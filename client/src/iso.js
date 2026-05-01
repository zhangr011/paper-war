// client/src/iso.js
// Isometric coordinate conversion utilities for 2:1 diamond tile projection.
// Tile dimensions: 64x32px (standard isometric)

const TILE_WIDTH = 64;
const TILE_HEIGHT = 32;
const HALF_W = TILE_WIDTH / 2;  // 32
const HALF_H = TILE_HEIGHT / 2; // 16

/**
 * Convert world tile coordinates to screen pixel coordinates.
 * @param {number} wx - World tile X
 * @param {number} wy - World tile Y
 * @param {number} offsetX - Camera X offset in screen pixels
 * @param {number} offsetY - Camera Y offset in screen pixels
 * @returns {[number, number]} [screenX, screenY]
 */
export function worldToScreen(wx, wy, offsetX, offsetY) {
  const sx = (wx - wy) * HALF_W + offsetX;
  const sy = (wx + wy) * HALF_H + offsetY;
  return [sx, sy];
}

/**
 * Convert screen pixel coordinates to world tile coordinates.
 * Inverse of worldToScreen.
 * @param {number} sx - Screen pixel X
 * @param {number} sy - Screen pixel Y
 * @param {number} offsetX - Camera X offset in screen pixels
 * @param {number} offsetY - Camera Y offset in screen pixels
 * @returns {[number, number]} [worldX, worldY] (fractional tile coordinates)
 */
export function screenToWorld(sx, sy, offsetX, offsetY) {
  const ax = sx - offsetX;
  const ay = sy - offsetY;
  const wx = (ax / HALF_W + ay / HALF_H) / 2;
  const wy = (ay / HALF_H - ax / HALF_W) / 2;
  return [wx, wy];
}

/**
 * Convert world tile coordinates to screen, centered in viewport.
 * @param {number} wx - World tile X
 * @param {number} wy - World tile Y
 * @param {number} camX - Camera center X in screen pixel space
 * @param {number} camY - Camera center Y in screen pixel space
 * @param {number} viewW - Viewport width in pixels
 * @param {number} viewH - Viewport height in pixels
 * @returns {[number, number]} [screenX, screenY] relative to canvas
 */
export function worldToScreenCentered(wx, wy, camX, camY, viewW, viewH) {
  const sx = (wx - wy) * HALF_W - camX + viewW / 2;
  const sy = (wx + wy) * HALF_H - camY + viewH / 2;
  return [sx, sy];
}

/**
 * Get depth sort key for isometric rendering order.
 * Lower values are drawn first (behind), higher values drawn later (in front).
 * Sort by Y ascending (depth), same Y sorts by X.
 * @param {number} wx - World tile X
 * @param {number} wy - World tile Y
 * @returns {number} Sort key
 */
export function depthKey(wx, wy) {
  return (wx + wy) * 10000 + wx;
}

export { TILE_WIDTH, TILE_HEIGHT, HALF_W, HALF_H };
