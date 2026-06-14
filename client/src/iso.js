// client/src/iso.js
// Rectangular coordinate conversion utilities for top-down tile projection.

const TILE_WIDTH = 32;
const TILE_HEIGHT = 32;
const HALF_W = TILE_WIDTH / 2;
const HALF_H = TILE_HEIGHT / 2;

/**
 * Convert world tile coordinates to screen pixel coordinates.
 * @param {number} wx - World tile X
 * @param {number} wy - World tile Y
 * @param {number} offsetX - Camera X offset in screen pixels
 * @param {number} offsetY - Camera Y offset in screen pixels
 * @returns {[number, number]} [screenX, screenY]
 */
export function worldToScreen(wx, wy, offsetX, offsetY) {
  const sx = wx * TILE_WIDTH + offsetX;
  const sy = wy * TILE_HEIGHT + offsetY;
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
  const wx = ax / TILE_WIDTH;
  const wy = ay / TILE_HEIGHT;
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
  const sx = wx * TILE_WIDTH - camX + viewW / 2;
  const sy = wy * TILE_HEIGHT - camY + viewH / 2;
  return [sx, sy];
}

/**
 * Get depth sort key for top-down rendering order.
 * Lower values are drawn first (behind), higher values drawn later (in front).
 * Sort by Y ascending (depth), same Y sorts by X.
 * @param {number} wx - World tile X
 * @param {number} wy - World tile Y
 * @returns {number} Sort key
 */
export function depthKey(wx, wy) {
  return wy * 10000 + wx;
}

export { TILE_WIDTH, TILE_HEIGHT, HALF_W, HALF_H };
