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
// In tile-space (state.js stores positions as tile floats), 0.01 tile
// per tick ≈ 0.1 tile/sec at 10Hz — well below visible movement.
const FACING_DEADZONE_SQ = 0.01 * 0.01;

// Accelerated correction: when the interpolated position is far from the
// target, blend toward it faster to avoid a visible "slide".
const CORRECTION_THRESHOLD = 5.0; // world units (only genuine desyncs, not normal movement)
const CORRECTION_SPEED = 3.0;     // blend fraction per tick (0.3 = 30% correction)

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

    // Non-interpolated fields (use curr directly)
    this.targetID = 0;

    // Unit classification (sent once at creation, never changes)
    this.unitType = 0; // CombatUnitType: 0=LI, 1=HI, 2=Sniper, 3=AAI, 4=MG, 5=MA, 6=MM
    this.team = 0;     // player/faction ID

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

    /** Clear all tracked entities. Used on reconnect to drop stale state
     *  before the first post-rejoin snapshot repopulates the world. */
    this.clearEntities = () => {
      this.units.clear();
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
      let unit = this.units.get(u.entityID);

      if (!unit) {
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
          unit.unitType = u.unitType;
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
          // Issue #28 — detect HP→0 transition to trigger the die animation.
          // We set dyingAt once (idempotent) so subsequent snapshots that
          // also report HP=0 don't reset the timer.
          if (u.hp <= 0 && unit.currHP > 0 && unit.dyingAt === 0) {
            unit.dyingAt = performance.now();
          }
          unit.currHP = u.hp;
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
      if (dist > CORRECTION_THRESHOLD) {
        const correctionT = clamp(
          CORRECTION_SPEED * (this.tickDuration / 1000),
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

      // Issue #28 — update facing from the (prev → curr) position delta.
      // We use the snapshot delta rather than the interpolated render
      // delta because the latter can briefly be zero at t≈0 even when
      // the unit is moving, causing flicker.  The deadzone prevents
      // jitter when the unit is essentially stationary.
      const dxSnap = unit.currX - unit.prevX;
      const dySnap = unit.currY - unit.prevY;
      const distSq = dxSnap * dxSnap + dySnap * dySnap;
      if (distSq > FACING_DEADZONE_SQ) {
        // Cardinal snap: pick the axis with the larger magnitude.
        // Ties go to the horizontal axis (E/W), which feels natural for
        // a side-view bias.  Coordinate system: +y is south on screen.
        if (Math.abs(dxSnap) >= Math.abs(dySnap)) {
          unit.facing = dxSnap > 0 ? 1 /*DIR_E*/ : 3 /*DIR_W*/;
        } else {
          unit.facing = dySnap > 0 ? 0 /*DIR_S*/ : 2 /*DIR_N*/;
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
      if (!unit.alive) continue;
      if (unit.dyingAt > 0) {
        if (now - unit.dyingAt > DEATH_FADE_MS) {
          // Fade complete — remove from active roster.
          unit.alive = false;
          this.units.delete(unit.entityID);
          continue;
        }
        // Still inside the die-animation window — include it.
        result.push(unit);
      } else {
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

      // Store for polling consumers
      this.pendingEvents.push(ev);

      // Dispatch to callbacks
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
          if (this.onProjectile) this.onProjectile(ev.data);
          break;
      }
    }
  }

  /**
   * Handle a death event — mark the unit as not alive.
   * Accepts either a parsed event {entityID} or a raw Uint8Array.
   * @param {{entityID?:number, byteLength?:number, buffer?:ArrayBuffer, byteOffset?:number}} data
   * @private
   */
  _handleDeath(data) {
    let entityID;
    if (data && typeof data.entityID === 'number') {
      entityID = data.entityID;
    } else if (data && data.byteLength >= 4) {
      const view = new DataView(data.buffer, data.byteOffset, data.byteLength);
      entityID = view.getUint32(0, true);
    }
    if (entityID !== undefined) {
      const unit = this.units.get(entityID);
      if (unit) {
        unit.alive = false;
      }
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
