// client/src/unit_atlas.js
//
// Procedural pixel-art texture atlas for combat units.
//
// Generates a single canvas texture at startup containing every
// (unitType, state, dir, frame) sprite.  Units sample from this atlas
// via the existing InstancedBatch pipeline (per-instance spriteOffset +
// spriteSize attributes); team colour and HP tints still apply because
// the fragment shader multiplies the sampled texel by the per-instance
// tint.
//
// Layout: 32×32 cells packed into a 32-column grid.  Each (unitType,
// state, dir) sprite reserves MAX_FRAMES_PER_SPRITE=4 contiguous cells
// (one per possible frame, even if a state uses fewer — keeps the math
// simple and frames stay together visually).  140 sprites × 4 cells =
// 560 cells, packed 32 per row → 18 rows.
//
// Issue #38: previously 16 cols × 140 rows = 4480 px tall, which exceeds
// the WebGL2 MAX_TEXTURE_SIZE floor of 4096 on older mobile GPUs.  The
// 32-col layout is 1024×576 — fits comfortably on every WebGL2 device.
//
// Cell math:
//   spriteSlot   = unitType * (STATES * DIRECTIONS) + state * DIRECTIONS + dir
//   linearCell   = spriteSlot * MAX_FRAMES_PER_SPRITE + frame
//   x            = (linearCell % ATLAS_COLS) * ATLAS_CELL
//   y            = floor(linearCell / ATLAS_COLS) * ATLAS_CELL
//
// Frame counts per state (the lookup clamps frame to frameCount-1):
//   idle   (0):  2 frames  — subtle breathing bob
//   idle2  (1):  2 frames  — rare alternate idle (head turn / shuffle)
//   move   (2):  4 frames  — walk / drive cycle
//   attack (3):  3 frames  — raise / fire / recoil
//   die    (4):  4 frames  — collapse + fade (locks on last frame)
//
// Directions per state (each unit type has 4 facings):
//   S (0)  front view  — primary art (existing silhouettes)
//   E (1)  right side  — same art, weapon already points right
//   N (2)  back view   — back-of-head/hull variant
//   W (3)  left side   — mirrored E via ctx.scale(-1, 1)
//
// The 7 unit types (0=LI, 1=HI, 2=Sniper, 3=AAI, 4=MG, 5=MA, 6=MM)
// each get a distinct silhouette so they're identifiable at a glance
// even before team tint is applied.

export const ATLAS_CELL = 32;            // px per sprite cell
export const ATLAS_COLS = 32;            // sprites' cells per row (8 sprites × 4 frames)
export const MAX_FRAMES_PER_SPRITE = 4;  // max(FRAMES_PER_STATE); reserved per sprite slot

// Direction enum — exported as both numeric constants and an array count.
export const DIR_S = 0;
export const DIR_E = 1;
export const DIR_N = 2;
export const DIR_W = 3;
export const DIRECTIONS = 4;

// State enum
export const STATE_IDLE   = 0;
export const STATE_IDLE2  = 1;
export const STATE_MOVE   = 2;
export const STATE_ATTACK = 3;
export const STATE_DIE    = 4;
export const STATES = 5;

// Legacy compat: old callers passed state 0..3 with the meaning
//   0=idle, 1=move, 2=attack, 3=retreat
// We've renumbered to insert idle2 at index 1 and drop retreat (visually
// equivalent to move-with-west-facing).  stateMapLegacy[] translates the
// old server state values to the new state indices.
export const STATE_MAP_LEGACY = [
  STATE_IDLE,    // 0 (idle)   → idle
  STATE_MOVE,    // 1 (move)   → move
  STATE_ATTACK,  // 2 (attack) → attack
  STATE_MOVE,    // 3 (retreat)→ move (facing handled separately)
];

// Total sprite slots: 7 types × 5 states × 4 dirs = 140.
// Each slot reserves MAX_FRAMES_PER_SPRITE cells (4) so frames for one
// sprite stay contiguous.  Total cells = 140 × 4 = 560, packed 32 per
// row → 18 rows.
export const ATLAS_ROWS = Math.ceil(7 * STATES * DIRECTIONS * MAX_FRAMES_PER_SPRITE / ATLAS_COLS);
export const ATLAS_W = ATLAS_COLS * ATLAS_CELL;     // 1024
export const ATLAS_H = ATLAS_ROWS * ATLAS_CELL;     // 576

// Frames per state — indexed by state 0..4
//   idle: 2, idle2: 2, move: 4, attack: 3, die: 4
export const FRAMES_PER_STATE = [2, 2, 4, 3, 4];

// Animation rate (frames per second) — indexed by state 0..4
//   idle2 plays slowly (4 fps) so the alternate pose reads as occasional.
//   die plays at 8 fps → 4 frames = 500 ms (close to the 600 ms spec).
export const ANIM_FPS = [6, 4, 10, 14, 8];

/**
 * Atlas cell origin (top-left) in atlas pixels for a given sprite frame.
 *
 * Each (unitType, state, dir) sprite reserves MAX_FRAMES_PER_SPRITE=4
 * contiguous cells (column-wrapped).  Sprites are packed 8 per row.
 *
 * @param {number} unitType  0..6
 * @param {number} state     0..4   (0=idle, 1=idle2, 2=move, 3=attack, 4=die)
 * @param {number} dir       0..3   (0=S, 1=E, 2=N, 3=W)
 * @param {number} frame     0..N-1 (clamped to frameCount-1)
 * @returns {{x:number, y:number, w:number, h:number}}
 */
export function atlasCell(unitType, state, dir, frame) {
  const t = Math.max(0, Math.min(6, unitType | 0));
  const s = Math.max(0, Math.min(STATES - 1, state | 0));
  const d = Math.max(0, Math.min(DIRECTIONS - 1, dir | 0));
  const f = Math.max(0, Math.min(FRAMES_PER_STATE[s] - 1, frame | 0));
  const spriteSlot = t * (STATES * DIRECTIONS) + s * DIRECTIONS + d;
  const linearCell = spriteSlot * MAX_FRAMES_PER_SPRITE + f;
  return {
    x: (linearCell % ATLAS_COLS) * ATLAS_CELL,
    y: Math.floor(linearCell / ATLAS_COLS) * ATLAS_CELL,
    w: ATLAS_CELL,
    h: ATLAS_CELL,
  };
}

/**
 * Pick the current animation frame for a unit.
 *
 * Frame advances from render time (NOT server ticks) so animation runs
 * smoothly regardless of network jitter.  Per-unit phase offset is
 * derived deterministically from entityID so a squad doesn't animate
 * in lockstep.
 *
 * For the die state, the frame is clamped to the last frame after the
 * full cycle has played once — the renderer holds the final pose.
 *
 * @param {number} state      0..4
 * @param {number} entityID   unique per-unit ID (for phase offset)
 * @param {number} timeMs     render clock (performance.now())
 * @param {number} [stateEnteredMs]  timestamp the unit entered this state.
 *        Required for STATE_DIE — the death animation plays once then locks.
 * @returns {number}          frame index in [0, FRAMES_PER_STATE[state]-1]
 */
export function currentFrame(state, entityID, timeMs, stateEnteredMs = 0) {
  const s = Math.max(0, Math.min(STATES - 1, state | 0));
  const fc = FRAMES_PER_STATE[s];
  if (fc <= 1) return 0;
  const fps = ANIM_FPS[s];

  // Die state: play once and lock on the last frame.
  if (s === STATE_DIE) {
    const elapsed = Math.max(0, timeMs - stateEnteredMs);
    const cycleMs = (fc / fps) * 1000;
    if (elapsed >= cycleMs) return fc - 1;
    return Math.min(fc - 1, Math.floor((elapsed / 1000) * fps));
  }

  // Phase offset: hash entityID into [0, 1) so each unit's cycle starts
  // at a different point.  Multiplied by the cycle duration (ms).
  const phase = ((entityID * 0.1375) % 1) * (1000 / fps);
  const t = ((timeMs + phase) / 1000) * fps;
  return Math.floor(t) % fc;
}

// ---------------------------------------------------------------------------
// Pixel-art drawing
// ---------------------------------------------------------------------------

// Each cell is drawn into a 32×32 region.  We work in a small virtual
// grid (typically 8×8 or 10×10 logical pixels per sprite) and scale up
// so the sprites read as chunky pixel art rather than smooth shapes.

/**
 * Draw a single sprite cell at the given atlas origin.
 * Dispatches by unitType to a type-specific painter, after applying
 * the direction transform (W is mirrored, N draws a back variant).
 *
 * @param {CanvasRenderingContext2D} ctx
 * @param {number} ox   atlas origin x (top-left of cell)
 * @param {number} oy   atlas origin y (top-left of cell)
 * @param {number} unitType  0..6
 * @param {number} state     0..4
 * @param {number} dir       0..3 (S, E, N, W)
 * @param {number} frame     0..N-1
 */
function drawCell(ctx, ox, oy, unitType, state, dir, frame) {
  ctx.save();
  ctx.translate(ox, oy);

  // Clip to cell so sprites don't bleed into neighbours (atlas uses
  // NEAREST filtering + CLAMP_TO_EDGE, but bleeding still happens with
  // sub-pixel positions during the instanced draw).
  ctx.beginPath();
  ctx.rect(0, 0, ATLAS_CELL, ATLAS_CELL);
  ctx.clip();

  // All sprites are drawn in white; team colour comes from the
  // per-instance tint (fragment shader multiplies texel × tint).
  // We use varying brightness levels of white so silhouettes have
  // internal detail (dark spots for eyes, weapon, shadow).

  // Direction handling.
  //   S (0) — front view (default art)
  //   E (1) — same as S; the weapon already points right, which reads
  //           as a 3/4 side view.  No transform.
  //   N (2) — back view; pass drawBack=true to the painter so it
  //           simplifies face/helmet detail (you see the back of the
  //           head / hull instead of the face / turret front).
  //   W (3) — mirror of E.  We translate to the right edge and flip
  //           horizontally so the silhouette faces left.
  const drawBack = (dir === DIR_N);
  if (dir === DIR_W) {
    ctx.translate(ATLAS_CELL, 0);
    ctx.scale(-1, 1);
  }

  switch (unitType) {
    case 0: drawLightInfantry(ctx, state, frame, drawBack); break;
    case 1: drawHeavyInfantry(ctx, state, frame, drawBack); break;
    case 2: drawSniper(ctx, state, frame, drawBack); break;
    case 3: drawAntiArmor(ctx, state, frame, drawBack); break;
    case 4: drawMotorGun(ctx, state, frame, drawBack); break;
    case 5: drawMotorArtillery(ctx, state, frame, drawBack); break;
    case 6: drawMotorMissile(ctx, state, frame, drawBack); break;
  }
  ctx.restore();
}

// --- helpers ---------------------------------------------------------------

function px(ctx, x, y, w, h, color) {
  ctx.fillStyle = color;
  ctx.fillRect(x, y, w, h);
}

// Walk-cycle vertical bob for leg animation.
// frame 0..3 → returns offset in px.
function walkBob(frame) {
  return [0, -1, 0, -1][frame % 4] || 0;
}

// Idle breathing: subtle y-shift between two frames.
function idleBob(frame) {
  return frame === 1 ? -1 : 0;
}

// idle2: rare alternate — small head turn / shuffle.  frame 0 = look
// right, frame 1 = look left (returns x-offset to apply to head only).
function idle2HeadOffset(frame) {
  return frame === 0 ? 1 : -1;
}

// Attack recoil: shift backward on frame 2 (after firing).
function attackShift(frame) {
  return frame === 2 ? -1 : 0;
}

// Die state: progressive collapse across 4 frames.
//   frame 0 — start of fall (slight tilt, dy=+1)
//   frame 1 — half-fallen (dy=+3, body rotates)
//   frame 2 — grounded (dy=+5, flattened)
//   frame 3 — fading out (still grounded, painter applies lower alpha)
// Returns the y-offset for the body.  Alpha is applied separately.
function dieOffset(frame) {
  return [1, 3, 5, 5][Math.min(3, frame | 0)];
}
function dieAlpha(frame) {
  // frame 3 fades to 0.35 — final pose is dimmed but still visible.
  return frame >= 3 ? 0.35 : 1.0;
}

// Apply die-state alpha to subsequent px() calls.  We do this by
// setting ctx.globalAlpha before drawing the body; the painter calls
// applyDieAlpha() at the top when state === STATE_DIE.
function applyDieAlpha(ctx, state, frame) {
  if (state === STATE_DIE) ctx.globalAlpha = dieAlpha(frame);
}

// --- per-type death overlays (issue #36) ----------------------------------
// Each drawX() painter calls its drawDeathOverlay_X() after drawing the
// collapsing body.  The overlay adds type-distinctive debris/details so
// players can tell at a glance what kind of unit died.  Overlays are
// drawn at full alpha (the body underneath already faded).
//
// All overlays are kept inside the 32×32 cell budget — silhouettes only.

// Light Infantry: helmet pops off and lands beside the body.
//   frame 0: helmet lifts slightly
//   frame 1: helmet in air, to the right
//   frame 2+: helmet on ground beside body
function drawDeathOverlay_LI(ctx, frame) {
  const cx = 16;
  // Only draw overlay when the helmet has actually separated (frame >= 1).
  // Helmet is a 4×3 white pixel cluster.
  if (frame === 1) {
    // Mid-air, just above and to the right
    px(ctx, cx + 4, 4, 4, 3, '#fff');
    px(ctx, cx + 4, 4, 4, 1, '#aaa'); // brim
  } else if (frame >= 2) {
    // Landed on the ground to the right of the body
    px(ctx, cx + 6, 26, 4, 2, '#ccc');
    px(ctx, cx + 6, 26, 4, 1, '#888'); // shadow underside
  }
}

// Heavy Infantry: helmet stays on, but a shield/arm-piece breaks off.
//   frame 0: nothing yet
//   frame 1+: broken armor piece lands to the left
function drawDeathOverlay_HI(ctx, frame) {
  const cx = 16;
  if (frame >= 1) {
    // Armor shard (3×2 dark gray)
    px(ctx, cx - 8, 26 + (frame >= 2 ? 0 : -2), 3, 2, '#888');
    px(ctx, cx - 8, 26 + (frame >= 2 ? 0 : -2), 3, 1, '#666');
  }
}

// Sniper: rifle goes vertical as the shooter crumples.
//   frame 0: rifle tilts
//   frame 1+: rifle vertical, lying beside body
function drawDeathOverlay_Sniper(ctx, frame) {
  const cx = 16;
  if (frame === 0) {
    // Tilted rifle (diagonal-ish — approximate with 2 segments)
    px(ctx, cx + 4, 18, 2, 4, '#666');
    px(ctx, cx + 5, 16, 2, 3, '#666');
  } else {
    // Vertical rifle lying on ground (rotated 90°)
    px(ctx, cx + 5, 26, 6, 2, '#666');
    px(ctx, cx + 4, 26, 2, 2, '#444'); // stock
  }
}

// Anti-Armor Infantry: missile rack detonates — small smoke puff drawn
// above the body, growing with frame.
function drawDeathOverlay_AAI(ctx, frame) {
  const cx = 16;
  if (frame >= 1) {
    // Smoke puff — gray, growing, alpha falls (use solid color; the body
    // fade handles overall dimming).
    const size = 2 + frame;
    px(ctx, cx - frame, 4 - frame, size + 2, size, '#999');
    px(ctx, cx - frame + 1, 5 - frame, size, size - 1, '#bbb');
  }
}

// Motor Gun (MG): tripod folds — one leg juts out at an angle as the
// gunner slumps.  Drawn as a diagonal pixel row beneath the body.
function drawDeathOverlay_MG(ctx, frame) {
  const cx = 16;
  if (frame >= 1) {
    // Splayed leg: 3 pixels going down-left from the body center
    for (let i = 0; i < 3; i++) {
      px(ctx, cx - 4 - i, 24 + i, 2, 1, '#555');
    }
  }
}

// Motor Artillery (MA): turret separates from hull and lands beside.
//   frame 0: turret lifts slightly
//   frame 1+: turret beside hull on ground
function drawDeathOverlay_MA(ctx, frame) {
  const cx = 16;
  if (frame === 1) {
    // Turret mid-flight
    px(ctx, cx + 6, 8, 6, 3, '#999');
    px(ctx, cx + 6, 8, 6, 1, '#666');
  } else if (frame >= 2) {
    // Turret landed on ground, tilted
    px(ctx, cx + 7, 27, 6, 2, '#888');
    px(ctx, cx + 6, 28, 2, 1, '#666'); // overhang
  }
}

// Motor Missile (MM): chassis tips sideways — draw a "tipped" silhouette
// element to the side of the main body.
function drawDeathOverlay_MM(ctx, frame) {
  const cx = 16;
  if (frame >= 2) {
    // Chassis edge visible to the right as the hull lays on its side
    px(ctx, cx + 8, 22, 4, 4, '#aaa');
    px(ctx, cx + 8, 22, 4, 1, '#777'); // top edge
  }
}

// --- per-type painters -----------------------------------------------------
// All sprites drawn on a 32×32 cell with a virtual ~10×10 body region
// centred horizontally.  White = body, #888 = darker detail, #aaa =
// mid detail, #444 = deep shadow.

function drawLightInfantry(ctx, state, frame, drawBack = false) {
  const cx = 16;
  let dy = 0;
  let headDx = 0;
  if (state === STATE_IDLE) dy = idleBob(frame);
  else if (state === STATE_IDLE2) { dy = idleBob(frame); headDx = idle2HeadOffset(frame); }
  else if (state === STATE_MOVE) dy = walkBob(frame);
  else if (state === STATE_ATTACK) dy = attackShift(frame);
  else if (state === STATE_DIE) { dy = dieOffset(frame); applyDieAlpha(ctx, state, frame); }

  // Head
  px(ctx, cx - 2 + headDx, 8 + dy, 4, 4, '#fff');
  if (!drawBack) {
    // Face detail — skip when viewing the back
    px(ctx, cx - 1 + headDx, 9 + dy, 1, 1, '#888'); // eye
  } else {
    // Back of head: a small cap line
    px(ctx, cx - 2 + headDx, 8 + dy, 4, 1, '#aaa');
  }
  // Body
  px(ctx, cx - 3, 12 + dy, 6, 8, '#ddd');
  // Legs (animate on move).  In die state legs are tucked under body.
  if (state === STATE_MOVE) {
    const l0 = (frame === 0 || frame === 2) ? 0 : -2;
    const l1 = (frame === 1 || frame === 3) ? 0 : -2;
    px(ctx, cx - 3, 20 + dy + l0, 2, 4, '#aaa');
    px(ctx, cx + 1, 20 + dy + l1, 2, 4, '#aaa');
  } else if (state !== STATE_DIE) {
    px(ctx, cx - 3, 20 + dy, 2, 4, '#bbb');
    px(ctx, cx + 1, 20 + dy, 2, 4, '#bbb');
  }
  // Rifle (horizontal, right side) — skip in die state
  if (state !== STATE_DIE) {
    const rifleColor = '#888';
    if (state === STATE_ATTACK && frame === 1) {
      // Muzzle flash
      px(ctx, cx + 7, 13 + dy, 3, 2, '#fff');
      px(ctx, cx + 9, 12 + dy, 2, 4, '#fff');
    }
    px(ctx, cx + 2, 14 + dy, 6, 2, rifleColor);
  }
  // Shadow under feet (smaller on die)
  if (state === STATE_DIE) {
    px(ctx, cx - 5, 26, 10, 1, '#444');
  } else {
    px(ctx, cx - 4, 25, 8, 1, '#444');
  }
  if (state === STATE_DIE) ctx.globalAlpha = 1.0;
  // Issue #36: type-specific death debris (helmet pops off).
  if (state === STATE_DIE) drawDeathOverlay_LI(ctx, frame);
}

function drawHeavyInfantry(ctx, state, frame, drawBack = false) {
  const cx = 16;
  let dy = 0;
  let headDx = 0;
  if (state === STATE_IDLE) dy = idleBob(frame);
  else if (state === STATE_IDLE2) { dy = idleBob(frame); headDx = idle2HeadOffset(frame); }
  else if (state === STATE_MOVE) dy = walkBob(frame);
  else if (state === STATE_ATTACK) dy = attackShift(frame);
  else if (state === STATE_DIE) { dy = dieOffset(frame); applyDieAlpha(ctx, state, frame); }

  // Head (with helmet — wider)
  px(ctx, cx - 3 + headDx, 7 + dy, 6, 5, '#fff');
  if (!drawBack) {
    px(ctx, cx - 3 + headDx, 7 + dy, 6, 1, '#aaa'); // helmet brim
  } else {
    // Back of helmet: no brim detail, just a stripe
    px(ctx, cx - 3 + headDx, 8 + dy, 6, 1, '#888');
  }
  // Armored body — wider than LI
  px(ctx, cx - 4, 12 + dy, 8, 8, '#ccc');
  px(ctx, cx - 4, 12 + dy, 8, 1, '#888'); // shoulder stripe
  // Legs (heavy boots)
  if (state === STATE_MOVE) {
    const l0 = (frame === 0 || frame === 2) ? 0 : -1;
    const l1 = (frame === 1 || frame === 3) ? 0 : -1;
    px(ctx, cx - 4, 20 + dy + l0, 3, 4, '#999');
    px(ctx, cx + 1, 20 + dy + l1, 3, 4, '#999');
  } else if (state !== STATE_DIE) {
    px(ctx, cx - 4, 20 + dy, 3, 4, '#aaa');
    px(ctx, cx + 1, 20 + dy, 3, 4, '#aaa');
  }
  // Heavy gun (chunky)
  if (state !== STATE_DIE) {
    if (state === STATE_ATTACK && frame === 1) {
      px(ctx, cx + 8, 13 + dy, 4, 3, '#fff'); // flash
    }
    px(ctx, cx + 3, 13 + dy, 5, 3, '#777');
  }
  px(ctx, cx - 4, 25, 8, 1, '#444');
  if (state === STATE_DIE) ctx.globalAlpha = 1.0;
  // Issue #36: armor shard breaks off.
  if (state === STATE_DIE) drawDeathOverlay_HI(ctx, frame);
}

function drawSniper(ctx, state, frame, drawBack = false) {
  const cx = 16;
  let dy = 0;
  let headDx = 0;
  if (state === STATE_IDLE) dy = idleBob(frame);
  else if (state === STATE_IDLE2) { dy = idleBob(frame); headDx = idle2HeadOffset(frame); }
  else if (state === STATE_MOVE) dy = walkBob(frame);
  else if (state === STATE_ATTACK) dy = attackShift(frame);
  else if (state === STATE_DIE) { dy = dieOffset(frame); applyDieAlpha(ctx, state, frame); }

  // Head — small, with cap
  px(ctx, cx - 2 + headDx, 9 + dy, 4, 3, '#fff');
  if (!drawBack) {
    px(ctx, cx - 2 + headDx, 9 + dy, 4, 1, '#888'); // cap brim
  } else {
    px(ctx, cx - 2 + headDx, 9 + dy, 4, 1, '#666'); // back of cap
  }
  // Body — slim
  px(ctx, cx - 2, 12 + dy, 5, 7, '#ddd');
  // Legs
  if (state === STATE_MOVE) {
    const l0 = (frame === 0 || frame === 2) ? 0 : -1;
    const l1 = (frame === 1 || frame === 3) ? 0 : -1;
    px(ctx, cx - 2, 19 + dy + l0, 2, 4, '#aaa');
    px(ctx, cx + 1, 19 + dy + l1, 2, 4, '#aaa');
  } else if (state !== STATE_DIE) {
    px(ctx, cx - 2, 19 + dy, 2, 4, '#bbb');
    px(ctx, cx + 1, 19 + dy, 2, 4, '#bbb');
  }
  // Long sniper rifle — extends far right
  if (state !== STATE_DIE) {
    if (state === STATE_ATTACK && frame === 1) {
      px(ctx, cx + 12, 14 + dy, 3, 2, '#fff'); // distant flash
    }
    px(ctx, cx + 2, 14 + dy, 12, 1, '#666');
    px(ctx, cx + 5, 14 + dy, 1, 2, '#888'); // scope
  }
  px(ctx, cx - 3, 24, 7, 1, '#444');
  if (state === STATE_DIE) ctx.globalAlpha = 1.0;
  // Issue #36: rifle goes vertical.
  if (state === STATE_DIE) drawDeathOverlay_Sniper(ctx, frame);
}

function drawAntiArmor(ctx, state, frame, drawBack = false) {
  const cx = 16;
  let dy = 0;
  let headDx = 0;
  if (state === STATE_IDLE) dy = idleBob(frame);
  else if (state === STATE_IDLE2) { dy = idleBob(frame); headDx = idle2HeadOffset(frame); }
  else if (state === STATE_MOVE) dy = walkBob(frame);
  else if (state === STATE_ATTACK) dy = attackShift(frame);
  else if (state === STATE_DIE) { dy = dieOffset(frame); applyDieAlpha(ctx, state, frame); }

  // Head
  px(ctx, cx - 2 + headDx, 8 + dy, 4, 4, '#fff');
  if (!drawBack) {
    px(ctx, cx + 1 + headDx, 9 + dy, 1, 1, '#666'); // eye
  }
  // Body
  px(ctx, cx - 3, 12 + dy, 6, 7, '#ccc');
  // Legs
  if (state === STATE_MOVE) {
    const l0 = (frame === 0 || frame === 2) ? 0 : -1;
    const l1 = (frame === 1 || frame === 3) ? 0 : -1;
    px(ctx, cx - 3, 19 + dy + l0, 2, 4, '#999');
    px(ctx, cx + 1, 19 + dy + l1, 2, 4, '#999');
  } else if (state !== STATE_DIE) {
    px(ctx, cx - 3, 19 + dy, 2, 4, '#aaa');
    px(ctx, cx + 1, 19 + dy, 2, 4, '#aaa');
  }
  // Big rocket launcher tube — thick, sits on shoulder
  if (state !== STATE_DIE) {
    if (state === STATE_ATTACK && frame === 1) {
      px(ctx, cx + 9, 12 + dy, 5, 4, '#fff'); // big backblast
      px(ctx, cx - 7, 12 + dy, 4, 4, '#fff');
    }
    px(ctx, cx + 1, 11 + dy, 8, 4, '#777');
    px(ctx, cx + 1, 11 + dy, 8, 1, '#444'); // tube top
  }
  px(ctx, cx - 4, 24, 8, 1, '#444');
  if (state === STATE_DIE) ctx.globalAlpha = 1.0;
  // Issue #36: missile rack detonates with smoke puff.
  if (state === STATE_DIE) drawDeathOverlay_AAI(ctx, frame);
}

function drawMotorGun(ctx, state, frame, drawBack = false) {
  // Tracked vehicle with rotating turret + machine gun
  const cx = 16;
  let dy = 0;
  if (state === STATE_IDLE) dy = idleBob(frame) * 0.5;
  else if (state === STATE_IDLE2) dy = idleBob(frame) * 0.5; // vehicles don't shuffle
  else if (state === STATE_MOVE) dy = (frame % 2 === 0) ? 0 : -1; // rumble
  else if (state === STATE_ATTACK) dy = attackShift(frame) * 0.5;
  else if (state === STATE_DIE) { dy = dieOffset(frame); applyDieAlpha(ctx, state, frame); }

  // Tracks (bottom)
  if (state === STATE_MOVE) {
    // Animate tread marks
    const o = (frame % 2) * 2;
    for (let i = 0; i < 5; i++) {
      px(ctx, 6 + i * 4 + o, 22 + dy, 2, 4, '#555');
    }
  } else {
    for (let i = 0; i < 5; i++) {
      px(ctx, 6 + i * 4, 22 + dy, 2, 4, '#666');
    }
  }
  px(ctx, 4, 22 + dy, 24, 1, '#333'); // track baseline
  // Hull
  px(ctx, 6, 14 + dy, 20, 8, '#ccc');
  px(ctx, 6, 14 + dy, 20, 1, '#888'); // hull top edge
  // Turret (skipped on back view — see back of hull instead)
  if (!drawBack) {
    px(ctx, 11, 10 + dy, 10, 5, '#ddd');
    // Machine gun barrel
    if (state !== STATE_DIE) {
      if (state === STATE_ATTACK && frame === 1) {
        px(ctx, cx + 9, 11 + dy, 4, 3, '#fff'); // flash
      }
      px(ctx, cx + 2, 12 + dy, 8, 1, '#444');
    }
  } else {
    // Back of hull: engine deck
    px(ctx, 11, 10 + dy, 10, 5, '#aaa');
    px(ctx, 13, 11 + dy, 6, 3, '#888');
  }
  px(ctx, cx - 4, 27, 28, 1, '#333'); // ground shadow
  if (state === STATE_DIE) ctx.globalAlpha = 1.0;
  // Issue #36: tripod leg splays out as gunner slumps.
  if (state === STATE_DIE) drawDeathOverlay_MG(ctx, frame);
}

function drawMotorArtillery(ctx, state, frame, drawBack = false) {
  const cx = 16;
  let dy = 0;
  let barrelLift = 0;
  if (state === STATE_IDLE) dy = idleBob(frame) * 0.5;
  else if (state === STATE_IDLE2) dy = idleBob(frame) * 0.5;
  else if (state === STATE_MOVE) dy = (frame % 2 === 0) ? 0 : -1;
  else if (state === STATE_ATTACK) {
    // Raise barrel across frames 0..2
    barrelLift = -frame;
    dy = frame === 2 ? -1 : 0;
  } else if (state === STATE_DIE) { dy = dieOffset(frame); applyDieAlpha(ctx, state, frame); }

  // Tracks
  if (state === STATE_MOVE) {
    const o = (frame % 2) * 2;
    for (let i = 0; i < 5; i++) {
      px(ctx, 5 + i * 4 + o, 22 + dy, 2, 4, '#555');
    }
  } else {
    for (let i = 0; i < 5; i++) {
      px(ctx, 5 + i * 4, 22 + dy, 2, 4, '#666');
    }
  }
  px(ctx, 3, 22 + dy, 26, 1, '#333');
  // Hull — large
  px(ctx, 5, 13 + dy, 22, 9, '#bbb');
  px(ctx, 5, 13 + dy, 22, 1, '#777');
  // Turret
  if (!drawBack) {
    px(ctx, 10, 9 + dy, 12, 5, '#ccc');
    // Long artillery cannon — raises on attack
    if (state !== STATE_DIE) {
      if (state === STATE_ATTACK && frame === 2) {
        px(ctx, cx + 12, 6 + dy + barrelLift, 5, 3, '#fff'); // muzzle blast
      }
      // Barrel drawn at angle (simulated with steps)
      const by = 11 + dy + barrelLift;
      px(ctx, cx + 2, by, 6, 2, '#555');
      px(ctx, cx + 7, by - 1, 4, 2, '#555');
      px(ctx, cx + 10, by - 2, 3, 2, '#555');
    }
  } else {
    // Back of hull: engine exhaust grates
    px(ctx, 10, 9 + dy, 12, 5, '#aaa');
    px(ctx, 13, 10 + dy, 2, 3, '#888');
    px(ctx, 17, 10 + dy, 2, 3, '#888');
  }
  px(ctx, cx - 5, 27, 30, 1, '#333');
  if (state === STATE_DIE) ctx.globalAlpha = 1.0;
  // Issue #36: turret separates from hull and lands beside.
  if (state === STATE_DIE) drawDeathOverlay_MA(ctx, frame);
}

function drawMotorMissile(ctx, state, frame, drawBack = false) {
  const cx = 16;
  let dy = 0;
  if (state === STATE_IDLE) dy = idleBob(frame) * 0.5;
  else if (state === STATE_IDLE2) dy = idleBob(frame) * 0.5;
  else if (state === STATE_MOVE) dy = (frame % 2 === 0) ? 0 : -1;
  else if (state === STATE_ATTACK) dy = attackShift(frame) * 0.5;
  else if (state === STATE_DIE) { dy = dieOffset(frame); applyDieAlpha(ctx, state, frame); }

  // Wheels (not tracks — distinguishes from MA/MG)
  if (state === STATE_MOVE) {
    const o = (frame % 2);
    for (let i = 0; i < 4; i++) {
      ctx.fillStyle = '#555';
      ctx.beginPath();
      ctx.arc(7 + i * 5 + o, 24 + dy, 2, 0, Math.PI * 2);
      ctx.fill();
    }
  } else {
    for (let i = 0; i < 4; i++) {
      ctx.fillStyle = '#666';
      ctx.beginPath();
      ctx.arc(7 + i * 5, 24 + dy, 2, 0, Math.PI * 2);
      ctx.fill();
    }
  }
  px(ctx, 4, 24 + dy, 24, 1, '#333');
  // Hull — lower profile than tracked variants
  px(ctx, 5, 16 + dy, 22, 7, '#ccc');
  px(ctx, 5, 16 + dy, 22, 1, '#888');
  // Missile rack — angled rectangles pointing up-right
  if (!drawBack) {
    if (state !== STATE_DIE) {
      if (state === STATE_ATTACK && frame === 1) {
        // Launch smoke puff
        px(ctx, cx + 4, 4 + dy, 6, 4, '#fff');
      }
      px(ctx, 9, 8 + dy, 14, 6, '#bbb');          // rack base
      px(ctx, 11, 6 + dy, 3, 3, '#999');          // missile 1
      px(ctx, 15, 5 + dy, 3, 4, '#999');          // missile 2
      px(ctx, 19, 6 + dy, 3, 3, '#999');          // missile 3
    }
  } else {
    // Back of hull: exhausts
    px(ctx, 9, 8 + dy, 14, 6, '#999');
    px(ctx, 11, 10 + dy, 2, 3, '#777');
    px(ctx, 18, 10 + dy, 2, 3, '#777');
  }
  px(ctx, cx - 5, 27, 30, 1, '#333');
  if (state === STATE_DIE) ctx.globalAlpha = 1.0;
  // Issue #36: chassis tips sideways as hull collapses.
  if (state === STATE_DIE) drawDeathOverlay_MM(ctx, frame);
}

// ---------------------------------------------------------------------------
// Atlas generation entry point
// ---------------------------------------------------------------------------

/**
 * Generate the unit atlas as an OffscreenCanvas-like 2D canvas.
 *
 * Returns a plain HTMLCanvasElement which can be passed directly to
 * gl.texImage2D (the GL binding code accepts canvas elements).
 *
 * @returns {HTMLCanvasElement}
 */
export function generateUnitAtlas() {
  const canvas = document.createElement('canvas');
  canvas.width = ATLAS_W;
  canvas.height = ATLAS_H;
  const ctx = canvas.getContext('2d');
  if (!ctx) throw new Error('2D canvas unavailable — cannot generate unit atlas');

  // Disable smoothing so pixel-art edges stay crisp at any draw scale.
  ctx.imageSmoothingEnabled = false;

  // Clear to transparent — only the sprite pixels should be sampled.
  // The fragment shader's tint multiply produces (0,0,0,0) for empty
  // texels, which the batch's alpha blend correctly discards.
  ctx.clearRect(0, 0, ATLAS_W, ATLAS_H);

  for (let t = 0; t < 7; t++) {
    for (let s = 0; s < STATES; s++) {
      for (let d = 0; d < DIRECTIONS; d++) {
        const fc = FRAMES_PER_STATE[s];
        for (let f = 0; f < fc; f++) {
          const cell = atlasCell(t, s, d, f);
          drawCell(ctx, cell.x, cell.y, t, s, d, f);
        }
      }
    }
  }

  return canvas;
}
