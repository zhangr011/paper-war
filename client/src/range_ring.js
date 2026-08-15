// range_ring.js — high-ground range ring geometry + effective-range math.
//
// Readability cue (terrain-readability-plan.md Phase 1 / ADR-0029): when a
// unit is selected, draw its effective attack range so the high-ground
// advantage is visible at a glance. Effective range vs any lower target is
// base + 1 — the server grants a flat +1 tile for ANY height advantage, not
// +1 per level — so the ring shows:
//   inner ring = base range (what the unit reaches on level ground),
//   outer ring = base + 1 (max reach vs a lower-ground target).
// On flat ground (elev 0) the two coincide, so only one ring is drawn.

// Base attack ranges per CombatUnitType (0-6), in tiles.
// ⚠ Hand-mirrored from server/pkg/component/unit_type.go CombatUnitTypeTable
// (LI=3, HI=4, Sniper=4, AAI=4, MG=3, MA=4, MM=5). Same mirroring pattern as
// UNIT_MAX_HP in main.js. range_ring_test.mjs drift-guards this against the
// Go source by regexing the literals.
export const UNIT_RANGES = [3, 4, 4, 4, 3, 4, 5];

// effectiveRange returns the ring radius in tiles for a selected unit:
// base range, or base + 1 when standing on ANY raised ground (the server
// grants a flat +1 tile for any height advantage over the target).
export function effectiveRange(unitType, attackerElev) {
  const base = UNIT_RANGES[unitType] ?? 3;
  const bonus = (attackerElev | 0) > 0 ? 1 : 0;
  return { base, max: base + bonus };
}

// elevationAt reads the CPU-side elevation grid (main.js elevationData,
// row-major ty*w+tx) with bounds clamping — off-map reads return 0.
export function elevationAt(elevationData, mapW, mapH, x, y) {
  const tx = Math.floor(x), ty = Math.floor(y);
  if (tx < 0 || ty < 0 || tx >= mapW || ty >= mapH) return 0;
  return elevationData[ty * mapW + tx];
}

// ringVertices emits a triangle strip as a flat vertex list for one ring of
// the given radius (px) around (cx, cy): N segments, thin annulus between
// radius and radius−thickness. Output is pairs of triangles in the same
// per-vertex layout SpriteBatch writes (x,y only — color/uv handled by the
// caller's primitive), returned as an array of [x1,y1, x2,y2, x3,y3] triples
// for direct consumption by a pushTriangle loop.
//
// Kept allocation-light: pushes into the provided `out` array.
export function ringTriangles(cx, cy, radius, thickness, segments, out) {
  const r0 = radius - thickness;
  const step = (Math.PI * 2) / segments;
  for (let i = 0; i < segments; i++) {
    const a0 = i * step, a1 = (i + 1) * step;
    const c0 = Math.cos(a0), s0 = Math.sin(a0);
    const c1 = Math.cos(a1), s1 = Math.sin(a1);
    // Outer arc points
    const ox0 = cx + c0 * radius, oy0 = cy + s0 * radius;
    const ox1 = cx + c1 * radius, oy1 = cy + s1 * radius;
    // Inner arc points
    const ix0 = cx + c0 * r0, iy0 = cy + s0 * r0;
    const ix1 = cx + c1 * r0, iy1 = cy + s1 * r0;
    // Two triangles per segment (quad).
    out.push(ox0, oy0, ix0, iy0, ix1, iy1);
    out.push(ox0, oy0, ix1, iy1, ox1, oy1);
  }
  return out;
}
