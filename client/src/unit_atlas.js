// client/src/unit_atlas.js
//
// Procedural pixel-art texture atlas for combat units.
//
// Generates a single canvas texture at startup containing every
// (unitType, state, frame) sprite.  Units sample from this atlas via
// the existing InstancedBatch pipeline (per-instance spriteOffset +
// spriteSize attributes); team colour and HP tints still apply because
// the fragment shader multiplies the sampled texel by the per-instance
// tint.
//
// Layout: 32×32 cells in a 16-column grid.  Row = unitType * 4 + state,
// column = frame index within that state.  This keeps related frames
// adjacent in memory and makes the (type, state, frame) → (x, y)
// lookup a pure function — no metadata table to maintain.
//
// Frame counts per state (the lookup clamps frame to frameCount-1):
//   idle (0):    2 frames  — subtle breathing bob
//   move (1):    4 frames  — walk / drive cycle
//   attack (2):  3 frames  — raise / fire / recoil
//   retreat (3): 2 frames  — turned away, slightly smaller
//
// The 7 unit types (0=LI, 1=HI, 2=Sniper, 3=AAI, 4=MG, 5=MA, 6=MM)
// each get a distinct silhouette so they're identifiable at a glance
// even before team tint is applied.

export const ATLAS_CELL = 32;            // px per sprite cell
export const ATLAS_COLS = 16;            // sprites per row
export const ATLAS_ROWS = 7 * 4;         // 7 types × 4 states = 28 rows
export const ATLAS_W = ATLAS_COLS * ATLAS_CELL;  // 512
export const ATLAS_H = ATLAS_ROWS * ATLAS_CELL;  // 896

// Frames per state — indexed by state 0..3
export const FRAMES_PER_STATE = [2, 4, 3, 2];

// Animation rate (frames per second) — indexed by state 0..3
export const ANIM_FPS = [6, 10, 14, 8];

/**
 * Atlas cell origin (top-left) in atlas pixels for a given sprite.
 *
 * @param {number} unitType  0..6
 * @param {number} state     0..3   (0=idle, 1=move, 2=attack, 3=retreat)
 * @param {number} frame     0..N-1 (clamped to frameCount-1)
 * @returns {{x:number, y:number, w:number, h:number}}
 */
export function atlasCell(unitType, state, frame) {
  const t = Math.max(0, Math.min(6, unitType | 0));
  const s = Math.max(0, Math.min(3, state | 0));
  const f = Math.max(0, Math.min(FRAMES_PER_STATE[s] - 1, frame | 0));
  return {
    x: f * ATLAS_CELL,
    y: (t * 4 + s) * ATLAS_CELL,
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
 * @param {number} state      0..3
 * @param {number} entityID   unique per-unit ID (for phase offset)
 * @param {number} timeMs     render clock (performance.now())
 * @returns {number}          frame index in [0, FRAMES_PER_STATE[state]-1]
 */
export function currentFrame(state, entityID, timeMs) {
  const s = Math.max(0, Math.min(3, state | 0));
  const fc = FRAMES_PER_STATE[s];
  if (fc <= 1) return 0;
  const fps = ANIM_FPS[s];
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
 * Dispatches by unitType to a type-specific painter.
 *
 * @param {CanvasRenderingContext2D} ctx
 * @param {number} ox   atlas origin x (top-left of cell)
 * @param {number} oy   atlas origin y (top-left of cell)
 * @param {number} unitType  0..6
 * @param {number} state     0..3
 * @param {number} frame     0..N-1
 */
function drawCell(ctx, ox, oy, unitType, state, frame) {
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
  switch (unitType) {
    case 0: drawLightInfantry(ctx, state, frame); break;
    case 1: drawHeavyInfantry(ctx, state, frame); break;
    case 2: drawSniper(ctx, state, frame); break;
    case 3: drawAntiArmor(ctx, state, frame); break;
    case 4: drawMotorGun(ctx, state, frame); break;
    case 5: drawMotorArtillery(ctx, state, frame); break;
    case 6: drawMotorMissile(ctx, state, frame); break;
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

// Attack recoil: shift backward on frame 2 (after firing).
function attackShift(frame) {
  return frame === 2 ? -1 : 0;
}

// Retreat: flip horizontally (we mirror by drawing reversed) and shrink.
// We achieve "turned away" by drawing a smaller silhouette darker.
function retreatScale(frame) {
  return frame === 0 ? 1.0 : 0.9;
}

// --- per-type painters -----------------------------------------------------
// All sprites drawn on a 32×32 cell with a virtual ~10×10 body region
// centred horizontally.  White = body, #888 = darker detail, #aaa =
// mid detail, #444 = deep shadow.

function drawLightInfantry(ctx, state, frame) {
  const cx = 16;
  let dy = 0;
  if (state === 0) dy = idleBob(frame);
  else if (state === 1) dy = walkBob(frame);
  else if (state === 2) dy = attackShift(frame);
  else if (state === 3) { const s = retreatScale(frame); dy = -1; }

  // Head
  px(ctx, cx - 2, 8 + dy, 4, 4, '#fff');
  // Body
  px(ctx, cx - 3, 12 + dy, 6, 8, '#ddd');
  // Legs (animate on move)
  if (state === 1) {
    const l0 = (frame === 0 || frame === 2) ? 0 : -2;
    const l1 = (frame === 1 || frame === 3) ? 0 : -2;
    px(ctx, cx - 3, 20 + dy + l0, 2, 4, '#aaa');
    px(ctx, cx + 1, 20 + dy + l1, 2, 4, '#aaa');
  } else {
    px(ctx, cx - 3, 20 + dy, 2, 4, '#bbb');
    px(ctx, cx + 1, 20 + dy, 2, 4, '#bbb');
  }
  // Rifle (horizontal, right side)
  const rifleColor = '#888';
  if (state === 2 && frame === 1) {
    // Muzzle flash
    px(ctx, cx + 7, 13 + dy, 3, 2, '#fff');
    px(ctx, cx + 9, 12 + dy, 2, 4, '#fff');
  }
  px(ctx, cx + 2, 14 + dy, 6, 2, rifleColor);
  // Shadow under feet
  px(ctx, cx - 4, 25, 8, 1, '#444');
}

function drawHeavyInfantry(ctx, state, frame) {
  const cx = 16;
  let dy = 0;
  if (state === 0) dy = idleBob(frame);
  else if (state === 1) dy = walkBob(frame);
  else if (state === 2) dy = attackShift(frame);

  // Head (with helmet — wider)
  px(ctx, cx - 3, 7 + dy, 6, 5, '#fff');
  px(ctx, cx - 3, 7 + dy, 6, 1, '#aaa'); // helmet brim
  // Armored body — wider than LI
  px(ctx, cx - 4, 12 + dy, 8, 8, '#ccc');
  px(ctx, cx - 4, 12 + dy, 8, 1, '#888'); // shoulder stripe
  // Legs (heavy boots)
  if (state === 1) {
    const l0 = (frame === 0 || frame === 2) ? 0 : -1;
    const l1 = (frame === 1 || frame === 3) ? 0 : -1;
    px(ctx, cx - 4, 20 + dy + l0, 3, 4, '#999');
    px(ctx, cx + 1, 20 + dy + l1, 3, 4, '#999');
  } else {
    px(ctx, cx - 4, 20 + dy, 3, 4, '#aaa');
    px(ctx, cx + 1, 20 + dy, 3, 4, '#aaa');
  }
  // Heavy gun (chunky)
  if (state === 2 && frame === 1) {
    px(ctx, cx + 8, 13 + dy, 4, 3, '#fff'); // flash
  }
  px(ctx, cx + 3, 13 + dy, 5, 3, '#777');
  px(ctx, cx - 4, 25, 8, 1, '#444');
}

function drawSniper(ctx, state, frame) {
  const cx = 16;
  let dy = 0;
  if (state === 0) dy = idleBob(frame);
  else if (state === 1) dy = walkBob(frame);
  else if (state === 2) dy = attackShift(frame);

  // Head — small, with cap
  px(ctx, cx - 2, 9 + dy, 4, 3, '#fff');
  px(ctx, cx - 2, 9 + dy, 4, 1, '#888');
  // Body — slim
  px(ctx, cx - 2, 12 + dy, 5, 7, '#ddd');
  // Legs
  if (state === 1) {
    const l0 = (frame === 0 || frame === 2) ? 0 : -1;
    const l1 = (frame === 1 || frame === 3) ? 0 : -1;
    px(ctx, cx - 2, 19 + dy + l0, 2, 4, '#aaa');
    px(ctx, cx + 1, 19 + dy + l1, 2, 4, '#aaa');
  } else {
    px(ctx, cx - 2, 19 + dy, 2, 4, '#bbb');
    px(ctx, cx + 1, 19 + dy, 2, 4, '#bbb');
  }
  // Long sniper rifle — extends far right
  if (state === 2 && frame === 1) {
    px(ctx, cx + 12, 14 + dy, 3, 2, '#fff'); // distant flash
  }
  px(ctx, cx + 2, 14 + dy, 12, 1, '#666');
  px(ctx, cx + 5, 14 + dy, 1, 2, '#888'); // scope
  px(ctx, cx - 3, 24, 7, 1, '#444');
}

function drawAntiArmor(ctx, state, frame) {
  const cx = 16;
  let dy = 0;
  if (state === 0) dy = idleBob(frame);
  else if (state === 1) dy = walkBob(frame);
  else if (state === 2) dy = attackShift(frame);

  // Head
  px(ctx, cx - 2, 8 + dy, 4, 4, '#fff');
  // Body
  px(ctx, cx - 3, 12 + dy, 6, 7, '#ccc');
  // Legs
  if (state === 1) {
    const l0 = (frame === 0 || frame === 2) ? 0 : -1;
    const l1 = (frame === 1 || frame === 3) ? 0 : -1;
    px(ctx, cx - 3, 19 + dy + l0, 2, 4, '#999');
    px(ctx, cx + 1, 19 + dy + l1, 2, 4, '#999');
  } else {
    px(ctx, cx - 3, 19 + dy, 2, 4, '#aaa');
    px(ctx, cx + 1, 19 + dy, 2, 4, '#aaa');
  }
  // Big rocket launcher tube — thick, sits on shoulder
  if (state === 2 && frame === 1) {
    px(ctx, cx + 9, 12 + dy, 5, 4, '#fff'); // big backblast
    px(ctx, cx - 7, 12 + dy, 4, 4, '#fff');
  }
  px(ctx, cx + 1, 11 + dy, 8, 4, '#777');
  px(ctx, cx + 1, 11 + dy, 8, 1, '#444'); // tube top
  px(ctx, cx - 4, 24, 8, 1, '#444');
}

function drawMotorGun(ctx, state, frame) {
  // Tracked vehicle with rotating turret + machine gun
  const cx = 16;
  let dy = 0;
  if (state === 0) dy = idleBob(frame) * 0.5;
  else if (state === 1) dy = (frame % 2 === 0) ? 0 : -1; // rumble
  else if (state === 2) dy = attackShift(frame) * 0.5;

  // Tracks (bottom)
  if (state === 1) {
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
  // Turret
  px(ctx, 11, 10 + dy, 10, 5, '#ddd');
  // Machine gun barrel
  if (state === 2 && frame === 1) {
    px(ctx, cx + 9, 11 + dy, 4, 3, '#fff'); // flash
  }
  px(ctx, cx + 2, 12 + dy, 8, 1, '#444');
  px(ctx, cx - 4, 27, 28, 1, '#333'); // ground shadow
}

function drawMotorArtillery(ctx, state, frame) {
  const cx = 16;
  let dy = 0;
  let barrelLift = 0;
  if (state === 0) dy = idleBob(frame) * 0.5;
  else if (state === 1) dy = (frame % 2 === 0) ? 0 : -1;
  else if (state === 2) {
    // Raise barrel across frames 0..2
    barrelLift = -frame;
    dy = frame === 2 ? -1 : 0;
  }

  // Tracks
  if (state === 1) {
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
  px(ctx, 10, 9 + dy, 12, 5, '#ccc');
  // Long artillery cannon — raises on attack
  if (state === 2 && frame === 2) {
    px(ctx, cx + 12, 6 + dy + barrelLift, 5, 3, '#fff'); // muzzle blast
  }
  // Barrel drawn at angle (simulated with steps)
  const by = 11 + dy + barrelLift;
  px(ctx, cx + 2, by, 6, 2, '#555');
  px(ctx, cx + 7, by - 1, 4, 2, '#555');
  px(ctx, cx + 10, by - 2, 3, 2, '#555');
  px(ctx, cx - 5, 27, 30, 1, '#333');
}

function drawMotorMissile(ctx, state, frame) {
  const cx = 16;
  let dy = 0;
  if (state === 0) dy = idleBob(frame) * 0.5;
  else if (state === 1) dy = (frame % 2 === 0) ? 0 : -1;
  else if (state === 2) dy = attackShift(frame) * 0.5;

  // Wheels (not tracks — distinguishes from MA/MG)
  if (state === 1) {
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
  if (state === 2 && frame === 1) {
    // Launch smoke puff
    px(ctx, cx + 4, 4 + dy, 6, 4, '#fff');
  }
  px(ctx, 9, 8 + dy, 14, 6, '#bbb');          // rack base
  px(ctx, 11, 6 + dy, 3, 3, '#999');          // missile 1
  px(ctx, 15, 5 + dy, 3, 4, '#999');          // missile 2
  px(ctx, 19, 6 + dy, 3, 3, '#999');          // missile 3
  px(ctx, cx - 5, 27, 30, 1, '#333');
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
    for (let s = 0; s < 4; s++) {
      const fc = FRAMES_PER_STATE[s];
      for (let f = 0; f < fc; f++) {
        const cell = atlasCell(t, s, f);
        drawCell(ctx, cell.x, cell.y, t, s, f);
      }
    }
  }

  return canvas;
}
