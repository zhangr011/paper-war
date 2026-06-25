// client/src/particles.js
//
// Lightweight particle system for combat juice (issue #37).
// Spawns short-lived visual effects from server combat events:
//   - MUZZLE_FLASH:  bright additive yellow, 60ms
//   - IMPACT_SPARK:  3 small orange quads with gravity, 100ms
//   - DUST_PUFF:     expanding brown quad, 300ms, alpha fade
//   - DEATH_SMOKE:   12 rising dark-gray quads, 500ms
//   - DEATH_FLASH:   one large white quad, 80ms
//
// Particles live in a pre-allocated pool (MAX_PARTICLES=500). Spawning
// when full overwrites the oldest particle (LRU via ring cursor). No
// per-frame allocation after warmup — particle state mutates in place,
// and getRenderDescs() reuses a fixed-size result buffer.
//
// Integration:
//   - main.js Game constructor: this.particles = new ParticleSystem();
//   - On snapshot: this.particles.processEvents(events, camWX, camWY);
//   - Per frame:   this.particles.update(dt);
//                  const descs = this.particles.getRenderDescs(zoom);
//                  this.renderer.drawEffects(descs, cameraOffset);
//   - On cleanup:  this.particles.reset();

// Event type constants — mirror state.js/connection.js (re-defined locally
// to avoid a circular import).
const EVENT_DAMAGE = 0;
const EVENT_DEATH = 1;

// Fixed-point format used by server positions (FractionBits=12).
const FIXED_ONE = 4096;

// Cull events whose source is more than this many tiles from the camera
// center — invisible to the player, no point spawning particles.
const VIEW_CULL_RADIUS = 30;

export const MAX_PARTICLES = 500;

export const PARTICLE_TYPES = {
  MUZZLE_FLASH: 0,
  IMPACT_SPARK: 1,
  DUST_PUFF: 2,
  DEATH_SMOKE: 3,
  DEATH_FLASH: 4,
};

// Per-type defaults. Callers can override via spawn() options.
const PARTICLE_CONFIG = {
  [PARTICLE_TYPES.MUZZLE_FLASH]: { life: 0.060, size: 4,  color: [1.0,  0.85, 0.40], alpha: 1.0 },
  [PARTICLE_TYPES.IMPACT_SPARK]: { life: 0.100, size: 2,  color: [1.0,  0.60, 0.20], alpha: 1.0 },
  [PARTICLE_TYPES.DUST_PUFF]:    { life: 0.300, size: 6,  color: [0.73, 0.66, 0.53], alpha: 0.6 },
  [PARTICLE_TYPES.DEATH_SMOKE]:  { life: 0.500, size: 8,  color: [0.30, 0.30, 0.30], alpha: 0.7 },
  [PARTICLE_TYPES.DEATH_FLASH]:  { life: 0.080, size: 16, color: [1.0,  1.0,  1.0 ], alpha: 0.9 },
};

/**
 * ParticleSystem manages a fixed-size pool of combat-juice particles.
 * Use spawn() for individual particles, processEvents() to batch-spawn
 * from a server snapshot, update() per frame, and getRenderDescs() to
 * feed the renderer.
 */
export class ParticleSystem {
  constructor(maxParticles = MAX_PARTICLES) {
    this.maxParticles = maxParticles;
    // Particle pool: each entry is a plain object we mutate in place.
    // `active=false` slots are inert; the cursor finds the next slot
    // to write via a ring buffer index.
    this.pool = new Array(maxParticles);
    for (let i = 0; i < maxParticles; i++) {
      this.pool[i] = {
        active: false,
        type: 0,
        x: 0, y: 0, vx: 0, vy: 0,
        age: 0, life: 0,
        baseSize: 0, size: 0,
        r: 0, g: 0, b: 0, baseAlpha: 0,
      };
    }
    // Ring buffer cursor: next slot to write. LRU when full.
    this.cursor = 0;
    this.activeCount = 0;
  }

  /**
   * Spawn a single particle. Options override defaults. When the pool
   * is full, the oldest active particle is overwritten (LRU).
   */
  spawn(type, x, y, opts = {}) {
    const p = this.pool[this.cursor];
    if (!p.active) this.activeCount++;
    p.active = true;
    p.type = type;
    p.x = x;
    p.y = y;
    p.vx = opts.vx ?? 0;
    p.vy = opts.vy ?? 0;
    p.age = 0;
    const cfg = PARTICLE_CONFIG[type] || PARTICLE_CONFIG[PARTICLE_TYPES.DUST_PUFF];
    p.life = opts.life ?? cfg.life;
    p.baseSize = opts.size ?? cfg.size;
    p.size = p.baseSize;
    p.r = opts.r ?? cfg.color[0];
    p.g = opts.g ?? cfg.color[1];
    p.b = opts.b ?? cfg.color[2];
    p.baseAlpha = opts.alpha ?? cfg.alpha;
    // Advance cursor — wrap around to spawn over the oldest particle.
    this.cursor = (this.cursor + 1) % this.maxParticles;
  }

  /**
   * Batch-spawn particles from a server snapshot's events array.
   * Camera world position (in tiles) is used to cull off-screen events.
   */
  processEvents(events, cameraWorldX, cameraWorldY) {
    if (!events || events.length === 0) return;
    for (const evt of events) {
      switch (evt.type) {
        case EVENT_DAMAGE: {
          // sourceX/sourceY are fixed-point (fraction bits = 12).
          const ex = evt.sourceX !== undefined ? evt.sourceX / FIXED_ONE : cameraWorldX;
          const ey = evt.sourceY !== undefined ? evt.sourceY / FIXED_ONE : cameraWorldY;
          if (Math.abs(ex - cameraWorldX) > VIEW_CULL_RADIUS ||
              Math.abs(ey - cameraWorldY) > VIEW_CULL_RADIUS) break;
          // Brief muzzle flash at the shooter position + 3 impact sparks
          // spraying outward and upward (gravity pulls them down in update).
          this.spawn(PARTICLE_TYPES.MUZZLE_FLASH, ex, ey);
          for (let i = 0; i < 3; i++) {
            const angle = Math.random() * Math.PI * 2;
            const speed = 2 + Math.random() * 3;
            this.spawn(PARTICLE_TYPES.IMPACT_SPARK, ex, ey, {
              vx: Math.cos(angle) * speed,
              vy: Math.sin(angle) * speed - 2, // upward bias
            });
          }
          break;
        }
        case EVENT_DEATH: {
          // x/y are fixed-point.
          const ex = evt.x !== undefined ? evt.x / FIXED_ONE : cameraWorldX;
          const ey = evt.y !== undefined ? evt.y / FIXED_ONE : cameraWorldY;
          if (Math.abs(ex - cameraWorldX) > VIEW_CULL_RADIUS ||
              Math.abs(ey - cameraWorldY) > VIEW_CULL_RADIUS) break;
          // Big white flash + 12 smoke puffs expanding outward and rising.
          this.spawn(PARTICLE_TYPES.DEATH_FLASH, ex, ey);
          for (let i = 0; i < 12; i++) {
            const angle = Math.random() * Math.PI * 2;
            const speed = 1 + Math.random() * 2;
            this.spawn(PARTICLE_TYPES.DEATH_SMOKE, ex, ey, {
              vx: Math.cos(angle) * speed,
              vy: Math.sin(angle) * speed - 1 - Math.random() * 2, // rise
              size: 6 + Math.random() * 6,
            });
          }
          break;
        }
        // EVENT_PROJECTILE (4): skip — impact fires as EVENT_DAMAGE at the
        //   target on the next tick.
        // EVENT_COMMANDER_DOWN (3): skip — base-alert overlay handles UI.
        // EVENT_TERRAIN_CHANGE (2): skip — no particle expression needed.
      }
    }
  }

  /**
   * Per-frame integration step. dt in seconds.
   * Advances age, integrates position, applies gravity to sparks,
   * fades/expands size, deactivates particles past their life.
   */
  update(dt) {
    let active = 0;
    const gAccel = 9.8; // tiles/sec^2 — scaled for top-down readability
    for (let i = 0; i < this.maxParticles; i++) {
      const p = this.pool[i];
      if (!p.active) continue;
      p.age += dt;
      if (p.age >= p.life) {
        p.active = false;
        continue;
      }
      // Integrate position.
      p.x += p.vx * dt;
      p.y += p.vy * dt;
      // Gravity on impact sparks only (others naturally fade).
      if (p.type === PARTICLE_TYPES.IMPACT_SPARK) {
        p.vy += gAccel * dt;
      }
      // Smoke/dust expand as they age; sparks/flash shrink slightly.
      const t = p.age / p.life;
      if (p.type === PARTICLE_TYPES.DEATH_SMOKE || p.type === PARTICLE_TYPES.DUST_PUFF) {
        p.size = p.baseSize * (1 + t * 0.5);
      } else {
        p.size = p.baseSize * (1 - t * 0.3);
      }
      active++;
    }
    this.activeCount = active;
  }

  /**
   * Iterate over all active particles. The callback receives each
   * particle object; the caller reads its state (x, y, size, r, g, b,
   * baseAlpha, age, life, type) and renders as desired.
   *
   * Used by main.js / gl.js to render particles in the effects pass
   * without per-frame descriptor allocation.
   */
  forEachActive(cb) {
    for (let i = 0; i < this.maxParticles; i++) {
      const p = this.pool[i];
      if (!p.active) continue;
      cb(p);
    }
  }

  /**
   * Deactivate all particles. Call on match end / cleanup.
   */
  reset() {
    for (let i = 0; i < this.maxParticles; i++) {
      this.pool[i].active = false;
    }
    this.activeCount = 0;
    this.cursor = 0;
  }
}
