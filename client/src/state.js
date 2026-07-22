// client/src/state.js
// Double-buffered state management with jitter-buffered interpolation from
// 10Hz server snapshots to 30fps rendering. Handles incremental diff
// updates, angle wrapping, velocity-based extrapolation with decay, and
// accelerated correction.
//
// Jitter buffer: snapshots are queued on arrival and only activated
// (shifting prev→curr) when the current interpolation reaches t≥1.0.
// This prevents visual backward-jumps from early packet arrivals and
// naturally absorbs network timing variance.

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

// ChangedMask bit flags — must match server/pkg/network/snapshot.go
const CHANGED_POSITION  = 1 << 0;
const CHANGED_VELOCITY  = 1 << 1;
const CHANGED_ANGLE     = 1 << 2;
const CHANGED_HP        = 1 << 3;
const CHANGED_TARGET_ID = 1 << 4;
const CHANGED_MORALE    = 1 << 5;
const CHANGED_STATE     = 1 << 6;
const CHANGED_SQUAD_ID  = 1 << 7;

// Event types — must match server/pkg/network/snapshot.go
const EVENT_DAMAGE        = 0;
const EVENT_DEATH         = 1;
const EVENT_TERRAIN_CHANGE = 2;
const EVENT_COMMANDER_DOWN = 3;
const EVENT_PROJECTILE    = 4;

// Fixed-point conversion (server uses int64 with 12-bit fraction)
const FRAC_BITS = 12;
const FIXED_ONE = 1 << FRAC_BITS;

// Server tick rate — must match game.ServerTicksPerSecond (10 Hz)
const SERVER_HZ = 10;
const TICK_DURATION_MS = 1000 / SERVER_HZ; // 100ms

// Extrapolation limits — tuned for 10Hz (1.5 ticks max)
const MAX_EXTRAPOLATION_MS = 150;
const MAX_EXTRAPOLATION_T = MAX_EXTRAPOLATION_MS / TICK_DURATION_MS;

// Velocity decay during extrapolation: after each tick past t=1.0, scale
// velocity by this factor. This makes predicted movement slow down
// naturally instead of continuing at full speed into infinity.
const EXTRAPOLATION_VELOCITY_DECAY = 0.7;

// Issue #28 — Death animation lifecycle.
// After a unit's HP hits 0, the renderer plays the die animation for
// this many milliseconds before the unit is removed from the render
// list.  Tuned to match the die state's 4 frames × ~150 ms.
const DEATH_FADE_MS = 600;

// Issue #28 — Facing dead-zone.
// Position deltas below this threshold don't update facing, so units
// that are essentially stationary don't flicker between directions.
// Issue #48 — bumped from 0.01² to 0.04².  The old threshold only
// filtered sub-step jitter; the new one also absorbs the small
// snapshot-delta noise that flips facing on slow diagonals.  0.04 tile
// per tick ≈ 0.4 tile/sec at 10Hz — still below visible movement speed.
const FACING_DEADZONE_SQ = 0.04 * 0.04;

// Issue #48 — Axis-lock hysteresis.  Once facing is on a given axis
// (horizontal E/W or vertical N/S), only switch to the other axis when
// its component magnitude exceeds the current axis's by this factor.
// Kills the per-tick diagonal-flip twitch (units walking NE used to
// alternate N/E every snapshot).  1.5 = "the new axis must be 50%
// larger before we commit to switching."
const FACING_AXIS_SWITCH_RATIO = 1.5;

// Issue #48 — Attack-swing duration; matches main.js ATTACK_DURATION_MS
// (3 frames @ 14 FPS).  Duplicated here because state.js owns the
// per-unit lifecycle flags and must know when to release the attack-
// induced facing lock without depending on the renderer module.
const ATTACK_DURATION_MS = (3 / 14) * 1000;

// Issue #52 — How long the unit stays planted after firing. Decoupled
// from ATTACK_DURATION_MS (the sprite animation) so the unit holds its
// position well past the swing — the animation plays once (~214ms) then
// the unit holds alert-idle while the freeze window continues. Tuned to
// the median server attack cooldown (5 ticks @ 10Hz = 500ms; see
// server/pkg/component/unit_type.go) so the unit reads as "set between
// shots" instead of sliding the moment the swing completes. Per-unit-
// type cooldown variation (2–12 ticks) is a follow-up — would need the
// server to ship Cooldown in the snapshot.
const ATTACK_FREEZE_MS = 500;

// Accelerated correction: when the interpolated position is far from the
// target, blend toward it faster to avoid a visible "slide".
const CORRECTION_THRESHOLD = 5.0; // world units (only genuine desyncs, not normal movement)
const CORRECTION_SPEED = 3.0;     // blend fraction per tick (0.3 = 30% correction)

// Issue #52 — post-freeze catch-up. While frozen the render position is
// held while curr/prev keep advancing from snapshots, so on release the
// delta to the live interpolated target can be anything from sub-pixel
// (a Guard squad that barely moved) to large (a unit that slid a lot).
// Without this window the small-delta case hits the `renderX = rx` snap
// branch below and the unit teleports. For ATTACK_CATCHUP_MS after the
// freeze ends, always blend (never snap) at a rate that covers a
// moderate delta over ~200ms.
const ATTACK_CATCHUP_MS = 250;
const ATTACK_CATCHUP_SPEED = 6.0; // ≈60%/tick at 10Hz → ~95% catch-up in 3 ticks

// Maximum pending snapshots in the jitter buffer queue
const MAX_PENDING_SNAPSHOTS = 3;

// ---------------------------------------------------------------------------
// Utility functions
// ---------------------------------------------------------------------------

/**
 * Convert a fixed-point int64 (12-bit fraction) to a floating-point value.
 * @param {number} fixed - The fixed-point integer value
 * @returns {number} Floating-point value
 */
function fixedToFloat(fixed) {
  return fixed / FIXED_ONE;
}

/**
 * Linear interpolation between two values.
 * @param {number} a - Start value
 * @param {number} b - End value
 * @param {number} t - Interpolation factor [0..1]
 * @returns {number} Interpolated value
 */
function lerp(a, b, t) {
  return a + (b - a) * t;
}

/**
 * Linear interpolation for angles in the 0-3599 range (0-359.9 degrees).
 * Takes the shortest path around the circle.
 * @param {number} a - Start angle (0-3599)
 * @param {number} b - End angle (0-3599)
 * @param {number} t - Interpolation factor [0..1]
 * @returns {number} Interpolated angle (0-3599)
 */
function lerpAngle(a, b, t) {
  let diff = b - a;
  if (diff > 1800) diff -= 3600;
  else if (diff < -1800) diff += 3600;
  let result = a + diff * t;
  if (result < 0) result += 3600;
  if (result >= 3600) result -= 3600;
  return result;
}

/**
 * Clamp a value between a minimum and maximum.
 * @param {number} v - Value
 * @param {number} lo - Minimum
 * @param {number} hi - Maximum
 * @returns {number} Clamped value
 */
function clamp(v, lo, hi) {
  return v < lo ? lo : v > hi ? hi : v;
}

// ---------------------------------------------------------------------------
// UnitState — per-unit interpolated state
// ---------------------------------------------------------------------------

class UnitState {
  constructor() {
    this.entityID = 0;
    this.squadID = 0;

    // Previous snapshot values (float, after fixed-to-float conversion)
    this.prevX = 0;
    this.prevY = 0;
    this.prevVx = 0;
    this.prevVy = 0;
    this.prevAngle = 0;
    this.prevHP = 0;
    this.prevMorale = 0;
    this.prevState = 0;

    // Current snapshot values (float, after fixed-to-float conversion)
    this.currX = 0;
    this.currY = 0;
    this.currVx = 0;
    this.currVy = 0;
    this.currAngle = 0;
    this.currHP = 0;
    this.currMorale = 0;
    this.currState = 0;

    // Rendered (interpolated) values — updated every frame
    this.renderX = 0;
    this.renderY = 0;
    this.renderAngle = 0;

    // Facing (cardinal direction) — issue #28.
    //   0=S, 1=E, 2=N, 3=W (see unit_atlas.js DIR_* constants)
    // Computed in update() from (prev→curr) position delta.  Defaults
    // to S so freshly-spawned stationary units have a valid atlas row.
    this.facing = 0;

    // Death lifecycle — issue #28.
    //   0          = alive (or not yet killed)
    //   <timestamp> = time HP first hit 0 (set once, never reset)
    // The renderer plays the die animation for ~600 ms after dyingAt,
    // then the unit is removed from getRenderUnits().
    this.dyingAt = 0;

    // Attack-fire lifecycle — issue #48.
    //   0          = not currently in an attack swing
    //   <timestamp> = render clock (performance.now()) the most recent
    //                 attack-fire event arrived.  The renderer plays the
    //                 attack animation once over ATTACK_DURATION_MS, then
    //                 returns to alert-idle until the next event arrives.
    //                 Facing is locked for the duration so the unit keeps
    //                 its weapon pointed at the target mid-swing.
    this.attackTriggeredAt = 0;

    // Non-interpolated fields (use curr directly)
    this.targetID = 0;

    // Unit classification (sent once at creation, never changes)
    this.unitType = 0; // CombatUnitType: 0=LI, 1=HI, 2=Sniper, 3=AAI, 4=MG, 5=MA, 6=MM
    this.team = 0;     // player/faction ID
    this.isCommander = false; // bit-7 flag from server (#54): render a rank marker

    // Lifecycle
    this.alive = true;
  }
}

// ---------------------------------------------------------------------------
// StateManager
// ---------------------------------------------------------------------------

/**
 * Manages the double-buffered game state and interpolates between server
 * snapshots to produce smooth rendering at 30fps from 5Hz server updates.
 *
 * Usage:
 *   const sm = new StateManager();
 *   // On receiving a decoded snapshot:
 *   sm.applySnapshot(tick, prevTick, unitUpdates, events);
 *   // Every render frame:
 *   sm.update(performance.now());
 *   // Get units for rendering:
 *   const units = sm.getRenderUnits();
 */
export class StateManager {
  constructor() {
    /** Entity ID → UnitState */
    this.units = new Map();

    /** Entity IDs the client has seen die. Prevents `_activateSnapshot`
     *  from resurrecting a unit the client already removed after the
     *  death-fade window when a trailing/out-of-order snapshot arrives.
     *  Bounded FIFO — long matches don't grow it unbounded. Issue #51. */
    this.deadIDs = new Set();
    this._deadIDOrder = []; // FIFO backing for the bound
    this._deadIDCap = 512;

    /** Clear all tracked entities. Used on reconnect to drop stale state
     *  before the first post-rejoin snapshot repopulates the world. */
    this.clearEntities = () => {
      this.units.clear();
      this.deadIDs.clear();
      this._deadIDOrder.length = 0;
      this.prevTick = 0;
      this.currTick = 0;
      this.nextTick = 0;
      this.pendingQueue = [];
      this.t = 0;
      this.lastSnapshotTime = 0;
    };

    // Double-buffer tick tracking
    this.prevTick = 0;
    this.currTick = 0;
    this.nextTick = 0; // tick of the latest queued snapshot

    // Jitter buffer: pending snapshots awaiting activation.
    // Snapshots are queued on arrival and activated (prev→curr shift)
    // only when the current interpolation reaches t≥1.0 in update().
    this.pendingQueue = [];

    // Interpolation parameter t ∈ [0, 1+]
    this.t = 0;

    // Timing
    this.tickDuration = TICK_DURATION_MS;
    this.lastSnapshotTime = 0;

    // Event callbacks — consumers can override these to handle events
    this.onDamage = null;        // (entityID, data) => {}
    this.onDeath = null;         // (entityID, data) => {}
    this.onTerrainChange = null; // (data) => {}
    this.onCommanderDown = null; // (data) => {}
    this.onProjectile = null;    // (data) => {}

    // Pending events from the latest snapshot (for external consumers that
    // poll instead of using callbacks)
    this.pendingEvents = [];

    // Fog of war grid
    this.fogWidth = 0;
    this.fogHeight = 0;
    this.fogVisible = null; // Uint8Array: 0=unexplored, 1=explored, 2=visible
  }

  // -----------------------------------------------------------------------
  // Snapshot application
  // -----------------------------------------------------------------------

  /**
   * Queue a decoded server snapshot for activation.
   *
   * The snapshot is NOT applied immediately — it goes into a pending
   * queue and is activated in update() when the current interpolation
   * reaches t≥1.0. This prevents visual backward-jumps from early
   * packet arrivals and naturally absorbs network jitter.
   *
   * Events and fog data are processed immediately (they are time-critical:
   * damage flashes, death sounds, fog visibility).
   *
   * @param {number} tick - Current snapshot tick
   * @param {number} prevTick - Previous snapshot tick this is a diff from
   * @param {Array} unitUpdates - Unit updates with changedMask bits
   * @param {Array} events - Snapshot events
   * @param {Object} fog - Fog of war data
   */
  applySnapshot(tick, prevTick, unitUpdates, events, fog) {
    // Update fog grid immediately
    if (fog && fog.visible) {
      this.fogWidth = fog.width;
      this.fogHeight = fog.height;
      this.fogVisible = fog.visible;
    }

    // Process events immediately (time-critical)
    if (events && events.length > 0) {
      this._processEvents(events);
    }

    // Reject out-of-order or duplicate snapshots
    if (tick <= this.nextTick && this.nextTick !== 0) {
      return;
    }

    // Queue for delayed activation
    this.pendingQueue.push({ tick, prevTick, unitUpdates });
    if (this.pendingQueue.length > MAX_PENDING_SNAPSHOTS) {
      // Overflow — drop oldest (shouldn't happen at 10Hz server / 30fps client)
      this.pendingQueue.shift();
    }
    this.nextTick = tick;
  }

  /**
   * Activate a pending snapshot: shift prev→curr and apply diffs.
   * Called from update() when the current interpolation reaches t≥1.0,
   * or immediately for the first snapshot.
   * @private
   */
  _activateSnapshot(snap) {
    const { tick, prevTick, unitUpdates } = snap;

    // Final out-of-order guard
    if (tick <= this.currTick && this.currTick !== 0) {
      return;
    }

    // Shift current → previous
    this.prevTick = this.currTick;
    this.currTick = tick;

    // Process each unit update
    for (let i = 0; i < unitUpdates.length; i++) {
      const u = unitUpdates[i];
      // Issue #51 — resurrect guard: skip updates for entities the
      // client has already seen die and removed.  Without this, a
      // trailing/out-of-order snapshot for a dead entity would hit the
      // new-unit branch below and silently re-create it alive.
      if (this.deadIDs.has(u.entityID)) continue;
      let unit = this.units.get(u.entityID);

      if (!unit) {
        // Issue #51 — don't materialise a unit that's arriving already
        // dead (server's steady-state filter should prevent this, but
        // late/out-of-order packets can still carry it).
        if ((u.changedMask & CHANGED_HP) && u.hp <= 0) continue;
        // New unit — create with both prev and curr set to the same values
        unit = new UnitState();
        unit.entityID = u.entityID;
        unit.squadID = u.squadID || 0;
        unit.alive = true;

        // Set both prev and curr to initial values
        if (u.changedMask & CHANGED_POSITION) {
          unit.prevX = u.x;
          unit.prevY = u.y;
          unit.currX = u.x;
          unit.currY = u.y;
          unit.renderX = u.x;
          unit.renderY = u.y;
        }
        if (u.changedMask & CHANGED_VELOCITY) {
          unit.prevVx = u.vx;
          unit.prevVy = u.vy;
          unit.currVx = u.vx;
          unit.currVy = u.vy;
        }
        if (u.changedMask & CHANGED_ANGLE) {
          unit.prevAngle = u.angle;
          unit.currAngle = u.angle;
          unit.renderAngle = u.angle;
        }
        if (u.changedMask & CHANGED_HP) {
          unit.prevHP = u.hp;
          unit.currHP = u.hp;
        }
        if (u.changedMask & CHANGED_TARGET_ID) {
          unit.targetID = u.targetID;
        }
        if (u.changedMask & CHANGED_MORALE) {
          unit.prevMorale = u.morale;
          unit.currMorale = u.morale;
        }
        if (u.changedMask & CHANGED_STATE) {
          unit.prevState = u.state;
          unit.currState = u.state;
        }
        if (u.changedMask & CHANGED_SQUAD_ID) {
          unit.squadID = u.squadID;
        }
        if (u.unitType !== undefined) {
          // Bit 7 flags commanders (set by the server); low 7 bits are the
          // CombatUnitType for the sprite atlas.
          unit.unitType = u.unitType & 0x7f;
          unit.isCommander = (u.unitType & 0x80) !== 0;
        }
        if (u.team !== undefined) {
          unit.team = u.team;
        }

        this.units.set(u.entityID, unit);
      } else {
        // Existing unit — shift curr → prev, then apply changes to curr
        unit.prevX = unit.currX;
        unit.prevY = unit.currY;
        unit.prevVx = unit.currVx;
        unit.prevVy = unit.currVy;
        unit.prevAngle = unit.currAngle;
        unit.prevHP = unit.currHP;
        unit.prevMorale = unit.currMorale;
        unit.prevState = unit.currState;

        // Only update fields indicated by the changed mask
        if (u.changedMask & CHANGED_POSITION) {
          unit.currX = u.x;
          unit.currY = u.y;
        }
        if (u.changedMask & CHANGED_VELOCITY) {
          unit.currVx = u.vx;
          unit.currVy = u.vy;
        }
        if (u.changedMask & CHANGED_ANGLE) {
          unit.currAngle = u.angle;
        }
        if (u.changedMask & CHANGED_HP) {
          unit.currHP = u.hp;
          // Issue #51 — defensive kill: if HP drops to 0 and the unit
          // isn't already dying, transition it into the death-fade
          // window now.  EventDeath is normally the trigger, but if it
          // is dropped / fog-culled / arrives in a later snapshot, the
          // unit would otherwise sit on the map at 0 HP forever.
          // `_markDying` is idempotent on `dyingAt`.
          if (unit.currHP <= 0 && unit.dyingAt === 0) {
            this._markDying(unit);
          }
        }
        if (u.changedMask & CHANGED_TARGET_ID) {
          unit.targetID = u.targetID;
        }
        if (u.changedMask & CHANGED_MORALE) {
          unit.currMorale = u.morale;
        }
        if (u.changedMask & CHANGED_STATE) {
          unit.currState = u.state;
        }
        if (u.changedMask & CHANGED_SQUAD_ID) {
          unit.squadID = u.squadID;
        }

        // If position did not change in this snapshot, prev = curr so the
        // interpolation holds the unit still (or continues extrapolating
        // using velocity from the last update).
        if (!(u.changedMask & CHANGED_POSITION)) {
          unit.prevX = unit.currX;
          unit.prevY = unit.currY;
        }
        if (!(u.changedMask & CHANGED_VELOCITY)) {
          unit.prevVx = unit.currVx;
          unit.prevVy = unit.currVy;
        }
        if (!(u.changedMask & CHANGED_ANGLE)) {
          unit.prevAngle = unit.currAngle;
        }
      }
    }

    // Record activation time and reset interpolation parameter
    this.lastSnapshotTime = performance.now();
    this.t = 0;
  }

  // -----------------------------------------------------------------------
  // Per-frame update (call at render rate, e.g. 30fps)
  // -----------------------------------------------------------------------

  /**
   * Advance the interpolation state. Call once per render frame.
   *
   * First, any pending snapshots whose activation time has arrived
   * (current interpolation reached t≥1.0) are activated. Then units
   * are interpolated/extrapolated for the current frame.
   *
   * @param {number} now - Current time in ms (e.g. performance.now())
   */
  update(now) {
    // Activate pending snapshots whose interpolation window has completed.
    // This is the jitter buffer: early-arriving snapshots wait here until
    // the current interpolation finishes, preventing visual jumps.
    while (this.pendingQueue.length > 0) {
      if (this.lastSnapshotTime === 0) {
        // First snapshot — activate immediately
        this._activateSnapshot(this.pendingQueue.shift());
      } else {
        const elapsed = now - this.lastSnapshotTime;
        if (elapsed >= this.tickDuration) {
          this._activateSnapshot(this.pendingQueue.shift());
        } else {
          break; // Current interpolation not yet complete
        }
      }
    }

    if (this.lastSnapshotTime === 0) {
      // No snapshot activated yet
      return;
    }

    // Calculate elapsed time since the last snapshot was activated
    const elapsed = now - this.lastSnapshotTime;

    // Normalized interpolation parameter
    let t = elapsed / this.tickDuration;

    const extrapolating = t > 1.0;

    // Clamp extrapolation
    if (t > 1.0 + MAX_EXTRAPOLATION_T) {
      t = 1.0 + MAX_EXTRAPOLATION_T;
    }
    this.t = t;

    // Interpolate each alive unit
    const units = this.units;
    for (const unit of units.values()) {
      if (!unit.alive) continue;

      // Issue #52 — freeze render position for the duration of an attack
      // swing so the unit "plants to fire" instead of sliding through the
      // animation. curr/prev still advance from snapshots; only the render
      // transform is held. On release the post-freeze catch-up window
      // (ATTACK_CATCHUP_MS below) blends render back toward the live
      // interpolated position instead of snapping, regardless of how
      // large the accumulated delta is. The die state has its own
      // anchored-position handling and never reaches here (units in the
      // fade window are skipped by the alive check).
      // Note: attack-freeze is now SERVER-SIDE (#52) — the server suppresses
      // movement during the swing (BoidComponent.FreezeUntilTick), so the
      // client's snapshots naturally show no position change during the freeze.
      // No client-side freeze/catch-up needed — eliminates the teleport.

      // Base interpolation
      let rx = lerp(unit.prevX, unit.currX, t);
      let ry = lerp(unit.prevY, unit.currY, t);
      let ra = lerpAngle(unit.prevAngle, unit.currAngle, t);

      // When extrapolating past t=1, use velocity to predict position.
      // Velocity decays linearly so predicted movement slows down
      // naturally instead of shooting off at full speed.
      if (extrapolating && (unit.currVx !== 0 || unit.currVy !== 0)) {
        const extraT = t - 1.0;
        const velocityScale = Math.max(
          0,
          1.0 - extraT * (1.0 - EXTRAPOLATION_VELOCITY_DECAY),
        );
        rx += unit.currVx * extraT * velocityScale;
        ry += unit.currVy * extraT * velocityScale;
      }

      // Accelerated correction: if the render position is far from the
      // interpolated target (e.g. after a snap following packet loss),
      // blend toward the correct position faster.
      const dx = rx - unit.renderX;
      const dy = ry - unit.renderY;
      const dist = Math.sqrt(dx * dx + dy * dy);
      // Issue #52 — post-freeze catch-up window. After the attack freeze
      // the render position lags curr/prev; without this guard a small
      // delta (dist ≤ CORRECTION_THRESHOLD) would hit the direct-snap
      // branch and the unit would teleport. Force the blend path for
      // ATTACK_CATCHUP_MS after the freeze ends, regardless of delta.
      const sinceAttack = unit.attackTriggeredAt > 0 ? now - unit.attackTriggeredAt : Infinity;
      const inCatchup =
        sinceAttack >= ATTACK_FREEZE_MS &&
        sinceAttack < ATTACK_FREEZE_MS + ATTACK_CATCHUP_MS;
      if (dist > CORRECTION_THRESHOLD || inCatchup) {
        const rate = inCatchup ? ATTACK_CATCHUP_SPEED : CORRECTION_SPEED;
        const correctionT = clamp(
          rate * (this.tickDuration / 1000),
          0,
          1,
        );
        unit.renderX = lerp(unit.renderX, rx, correctionT);
        unit.renderY = lerp(unit.renderY, ry, correctionT);
      } else {
        unit.renderX = rx;
        unit.renderY = ry;
      }

      // Angle does not use accelerated correction (it would cause spinning)
      unit.renderAngle = ra;

      // Issue #28 / #48 — update facing from the (prev → curr) position
      // delta.  We use the snapshot delta rather than the interpolated
      // render delta because the latter can briefly be zero at t≈0 even
      // when the unit is moving, causing flicker.  The deadzone prevents
      // jitter when the unit is essentially stationary.  Issue #48 adds
      // axis-lock hysteresis: once facing commits to an axis, the other
      // axis must win by FACING_AXIS_SWITCH_RATIO before we switch,
      // killing the diagonal-flip twitch.
      //
      // Issue #48 — facing is locked for the duration of an attack swing
      // so the unit keeps its weapon pointed at the target mid-animation
      // instead of turning its back if movement resumes mid-swing.
      const inAttackSwing =
        unit.attackTriggeredAt > 0 &&
        performance.now() - unit.attackTriggeredAt < ATTACK_FREEZE_MS;
      if (!inAttackSwing) {
        const dxSnap = unit.currX - unit.prevX;
        const dySnap = unit.currY - unit.prevY;
        const distSq = dxSnap * dxSnap + dySnap * dySnap;
        if (distSq > FACING_DEADZONE_SQ) {
          const ax = Math.abs(dxSnap);
          const ay = Math.abs(dySnap);
          // Current axis: 0 = horizontal (E/W), 1 = vertical (N/S).
          const currIsVertical = unit.facing === 0 /*S*/ || unit.facing === 2 /*N*/;
          let pickVertical;
          if (currIsVertical) {
            // Switch to horizontal only if dx wins by the ratio.
            pickVertical = !(ax > ay * FACING_AXIS_SWITCH_RATIO);
          } else {
            // Switch to vertical only if dy wins by the ratio.
            pickVertical = ay > ax * FACING_AXIS_SWITCH_RATIO;
          }
          if (!pickVertical) {
            unit.facing = dxSnap > 0 ? 1 /*DIR_E*/ : 3 /*DIR_W*/;
          } else {
            unit.facing = dySnap > 0 ? 0 /*DIR_S*/ : 2 /*DIR_N*/;
          }
        }
      }
    }
  }

  // -----------------------------------------------------------------------
  // Query methods
  // -----------------------------------------------------------------------

  /**
   * Get all alive units with their interpolated render positions.
   *
   * Issue #28 — units in the death-fade window (HP just hit 0) are
   * still included so the renderer can play the die animation.  They
   * are removed automatically once `now - dyingAt > DEATH_FADE_MS`.
   * @returns {UnitState[]}
   */
  getRenderUnits() {
    const now = performance.now();
    const result = [];
    for (const unit of this.units.values()) {
      if (unit.dyingAt > 0) {
        if (now - unit.dyingAt > DEATH_FADE_MS) {
          // Fade complete — remove from active roster.
          this.units.delete(unit.entityID);
          continue;
        }
        // Still inside the die-animation window — include it even if
        // the death event already flipped `alive` to false.  The die
        // sprite atlas cell plays the collapse animation; the renderer
        // fades alpha as `now - dyingAt → DEATH_FADE_MS`.
        result.push(unit);
      } else if (unit.alive) {
        result.push(unit);
      }
    }
    return result;
  }

  /**
   * Get a specific unit by entity ID.
   * @param {number} entityID
   * @returns {UnitState|undefined}
   */
  getUnit(entityID) {
    return this.units.get(entityID);
  }

  /**
   * Get the current interpolation parameter.
   * @returns {number} t value [0, ~4]
   */
  getInterpolationT() {
    return this.t;
  }

  /**
   * Get the current server tick.
   * @returns {number}
   */
  getCurrentTick() {
    return this.currTick;
  }

  /**
   * Whether we are currently extrapolating (no recent snapshot).
   * @returns {boolean}
   */
  isExtrapolating() {
    return this.t > 1.0;
  }

  // -----------------------------------------------------------------------
  // Cleanup
  // -----------------------------------------------------------------------

  /**
   * Remove dead units from the map. Call periodically (e.g. once per second).
   */
  cleanup() {
    for (const [id, unit] of this.units) {
      if (!unit.alive) {
        this.units.delete(id);
      }
    }
  }

  /**
   * Reset all state. Useful when reconnecting or loading a new game.
   */
  reset() {
    this.units.clear();
    this.prevTick = 0;
    this.currTick = 0;
    this.nextTick = 0;
    this.pendingQueue = [];
    this.t = 0;
    this.lastSnapshotTime = 0;
    this.pendingEvents = [];
  }

  // -----------------------------------------------------------------------
  // Internal helpers
  // -----------------------------------------------------------------------

  /**
   * Process events from a snapshot and dispatch to callbacks.
   * @param {Array<{type:number, data:Uint8Array}>} events
   * @private
   */
  _processEvents(events) {
    for (let i = 0; i < events.length; i++) {
      const ev = events[i];

      // Dispatch to callbacks. (Issue #30: we used to also push into
      // `pendingEvents` for "polling consumers", but no consumer ever
      // called drainEvents() — the array grew unbounded during combat
      // and leaked ~22 KB/min plus retained event payloads. The
      // dispatch path above already routes every event to its handler
      // (audio / renderer / fog), so the buffer was pure overhead.)
      switch (ev.type) {
        case EVENT_DAMAGE:
          if (this.onDamage) this.onDamage(ev.data);
          break;
        case EVENT_DEATH:
          this._handleDeath(ev);
          if (this.onDeath) this.onDeath(ev);
          break;
        case EVENT_TERRAIN_CHANGE:
          if (this.onTerrainChange) this.onTerrainChange(ev.data);
          break;
        case EVENT_COMMANDER_DOWN:
          if (this.onCommanderDown) this.onCommanderDown(ev.data);
          break;
        case EVENT_PROJECTILE:
          this._handleAttack(ev);
          if (this.onProjectile) this.onProjectile(ev);
          break;
      }
    }
  }

  /**
   * Handle a death event — lock the unit into the die state and anchor
   * its render position at the death location.
   *
   * Issue #28 — the server now includes the unit's fixed-point X/Y at
   * the moment of death (captured BEFORE component teardown) plus the
   * simulation tick.  We snap both prev and curr position to that
   * location so the renderer plays the collapse animation at the exact
   * tile the unit occupied, not at the extrapolated interpolated
   * position which may have drifted past it.
   *
   * Accepts either a parsed event {entityID, x, y, tick} or a raw
   * Uint8Array (legacy 4-byte payload — entityID only, no position).
   * @param {{entityID?:number, x?:number, y?:number, tick?:number, byteLength?:number, buffer?:ArrayBuffer, byteOffset?:number}} data
   * @private
   */
  _handleDeath(data) {
    let entityID;
    let deathX = null;
    let deathY = null;
    if (data && typeof data.entityID === 'number') {
      entityID = data.entityID;
      if (typeof data.x === 'number' && typeof data.y === 'number') {
        deathX = fixedToFloat(data.x);
        deathY = fixedToFloat(data.y);
      }
    } else if (data && data.byteLength >= 4) {
      const view = new DataView(data.buffer, data.byteOffset, data.byteLength);
      entityID = view.getUint32(0, true);
      // Enriched 24-byte payload (issue #28): X (int64) + Y (int64) + tick
      if (data.byteLength >= 24) {
        deathX = fixedToFloat(Number(view.getBigInt64(4, true)));
        deathY = fixedToFloat(Number(view.getBigInt64(12, true)));
      }
    }
    if (entityID !== undefined) {
      const unit = this.units.get(entityID);
      if (unit) {
        unit.alive = false;
        // Idempotent: only set dyingAt on the first death event for this
        // unit.  Subsequent events (or HP=0 snapshots) don't reset the
        // timer — the die animation plays exactly once.
        if (unit.dyingAt === 0) {
          unit.dyingAt = performance.now();
        }
        // Snap render position to the authoritative death location so
        // the collapse animation stays anchored even if interpolation
        // had been leading/extrapolating the unit past the death tile.
        if (deathX !== null && deathY !== null) {
          unit.prevX = unit.currX = deathX;
          unit.prevY = unit.currY = deathY;
        }
        this._rememberDead(entityID);
      }
    }
  }

  /**
   * Record an entityID as known-dead so a trailing/out-of-order snapshot
   * cannot resurrect it after the client removes the unit.  Bounded FIFO
   * so a very long session doesn't grow the set without limit.  Issue #51.
   * @param {number} entityID
   * @private
   */
  _rememberDead(entityID) {
    if (this.deadIDs.has(entityID)) return;
    this.deadIDs.add(entityID);
    this._deadIDOrder.push(entityID);
    while (this._deadIDOrder.length > this._deadIDCap) {
      const evicted = this._deadIDOrder.shift();
      this.deadIDs.delete(evicted);
    }
  }

  /**
   * Idempotent transition into the death-fade window.  Used by the
   * HP=0 defensive-kill path in `_activateSnapshot` (issue #51) so the
   * die animation fires whether or not `EventDeath` arrives.  Mirrors
   * the guard in `_handleDeath`: a unit that's already dying keeps its
   * original timestamp.
   * @param {UnitState} unit
   * @private
   */
  _markDying(unit) {
    unit.alive = false;
    if (unit.dyingAt === 0) {
      unit.dyingAt = performance.now();
    }
    this._rememberDead(unit.entityID);
  }

  /**
   * Handle an attack-fire event (EventProjectile, repurposed in issue #48
   * to mean "this unit resolved an attack at this tick").  Stamps the
   * render clock on the attacker so the renderer plays the attack
   * animation once as a one-shot.  No-op if the attacker isn't in our
   * unit table (e.g., fog-of-war entity we never knew about).
   *
   * Connection.js decodes the payload into {entityID, tick}; the tick is
   * unused client-side — animation timing is driven off the arrival
   * moment, which is close enough to the server tick at v1 latencies.
   * @param {{entityID?:number}} data
   * @private
   */
  _handleAttack(data) {
    if (!data || typeof data.entityID !== 'number') return;
    const unit = this.units.get(data.entityID);
    if (unit && unit.alive) {
      unit.attackTriggeredAt = performance.now();
    }
  }

  /**
   * Drain and return all pending events, clearing the queue.
   * @returns {Array<{type:number, data:Uint8Array}>}
   */
  drainEvents() {
    const events = this.pendingEvents;
    this.pendingEvents = [];
    return events;
  }
}
