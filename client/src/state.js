// client/src/state.js
// Double-buffered state management with interpolation from 15Hz server
// snapshots to 30fps rendering. Handles incremental diff updates, angle
// wrapping, velocity-based extrapolation, and accelerated correction.

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

// Event types — must match server/pkg/network/snapshot.go
const EVENT_DAMAGE        = 0;
const EVENT_DEATH         = 1;
const EVENT_TERRAIN_CHANGE = 2;
const EVENT_COMMANDER_DOWN = 3;
const EVENT_PROJECTILE    = 4;

// Fixed-point conversion (server uses int64 with 12-bit fraction)
const FRAC_BITS = 12;
const FIXED_ONE = 1 << FRAC_BITS;

// Server tick rate
const SERVER_HZ = 15;
const TICK_DURATION_MS = 1000 / SERVER_HZ; // ~66.7ms

// Extrapolation limits
const MAX_EXTRAPOLATION_MS = 200;
const MAX_EXTRAPOLATION_T = MAX_EXTRAPOLATION_MS / TICK_DURATION_MS;

// Accelerated correction: when the interpolated position is far from the
// target, blend toward it faster to avoid a visible "slide".
const CORRECTION_THRESHOLD = 4.0; // world units
const CORRECTION_SPEED = 8.0;     // multiplier on the blend

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

    // Non-interpolated fields (use curr directly)
    this.targetID = 0;

    // Lifecycle
    this.alive = true;
  }
}

// ---------------------------------------------------------------------------
// StateManager
// ---------------------------------------------------------------------------

/**
 * Manages the double-buffered game state and interpolates between server
 * snapshots to produce smooth rendering at 30fps from 15Hz server updates.
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

    // Double-buffer tick tracking
    this.prevTick = 0;
    this.currTick = 0;

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
  }

  // -----------------------------------------------------------------------
  // Snapshot application
  // -----------------------------------------------------------------------

  /**
   * Apply a decoded server snapshot.
   *
   * @param {number} tick - Current snapshot tick
   * @param {number} prevTick - Previous snapshot tick this is a diff from
   * @param {Array<{entityID:number, changedMask:number, x?:number, y?:number, vx?:number, vy?:number, angle?:number, hp?:number, targetID?:number, morale?:number, state?:number}>} unitUpdates
   *   Unit updates with changedMask bits indicating which fields are present.
   *   Position/velocity values should already be converted from fixed-point.
   * @param {Array<{type:number, data:Uint8Array}>} events - Snapshot events
   */
  applySnapshot(tick, prevTick, unitUpdates, events) {
    const now = performance.now();

    // If this is an out-of-order or duplicate snapshot, skip it
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

    // Process events
    if (events && events.length > 0) {
      this._processEvents(events);
    }

    // Record the time this snapshot arrived for interpolation timing.
    // Reset t so we start interpolating from the beginning of the new tick.
    this.lastSnapshotTime = now;
    this.t = 0;
  }

  // -----------------------------------------------------------------------
  // Per-frame update (call at render rate, e.g. 30fps)
  // -----------------------------------------------------------------------

  /**
   * Advance the interpolation state. Call once per render frame.
   * @param {number} now - Current time in ms (e.g. performance.now())
   */
  update(now) {
    if (this.lastSnapshotTime === 0) {
      // No snapshot received yet
      return;
    }

    // Calculate elapsed time since the last snapshot arrived
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
      // This gives one frame of smooth continuation before snapping.
      if (extrapolating && (unit.currVx !== 0 || unit.currVy !== 0)) {
        const extraT = t - 1.0;
        // Velocity is in world-units per tick
        rx += unit.currVx * extraT;
        ry += unit.currVy * extraT;
      }

      // Accelerated correction: if the render position is far from the
      // interpolated target (e.g. after a snap following packet loss),
      // blend toward the correct position faster.
      const dx = rx - unit.renderX;
      const dy = ry - unit.renderY;
      const dist = Math.sqrt(dx * dx + dy * dy);
      if (dist > CORRECTION_THRESHOLD) {
        // Correction factor: higher distance → faster convergence
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
    }
  }

  // -----------------------------------------------------------------------
  // Query methods
  // -----------------------------------------------------------------------

  /**
   * Get all alive units with their interpolated render positions.
   * @returns {UnitState[]}
   */
  getRenderUnits() {
    const result = [];
    for (const unit of this.units.values()) {
      if (unit.alive) result.push(unit);
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
          this._handleDeath(ev.data);
          if (this.onDeath) this.onDeath(ev.data);
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
   * The data format is expected to contain a uint32 entityID at offset 0.
   * @param {Uint8Array} data
   * @private
   */
  _handleDeath(data) {
    if (data && data.byteLength >= 4) {
      const view = new DataView(data.buffer, data.byteOffset, data.byteLength);
      const entityID = view.getUint32(0, true); // little-endian
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
