// client/src/camera.js
// Isometric camera with pan/zoom for the Paper War RTS client.
// The camera tracks (offsetX, offsetY) in screen-pixel space,
// representing the world-origin point relative to the viewport center.

import { HALF_W, HALF_H } from './iso.js';

export class Camera {
  /**
   * @param {number} viewWidth  - Canvas width in pixels
   * @param {number} viewHeight - Canvas height in pixels
   */
  constructor(viewWidth, viewHeight) {
    // Camera position: the screen-pixel offset applied to all worldToScreen
    // conversions. Think of it as "which world-pixel is at the viewport center".
    this.offsetX = 0;
    this.offsetY = 0;

    this.zoom = 1.0;     // 1.0 = normal, >1 = zoomed in, <1 = zoomed out
    this.viewW = viewWidth;
    this.viewH = viewHeight;

    // Smooth pan velocity (screen pixels per second)
    this.panVX = 0;
    this.panVY = 0;

    // Zoom limits
    this.minZoom = 0.5;
    this.maxZoom = 3.0;

    // Map bounds (tile count)
    this.mapWidth = 64;
    this.mapHeight = 64;

    // Center camera on map by default
    this.centerOnMap();
  }

  /**
   * Center the camera on the middle of the map.
   */
  centerOnMap() {
    const cx = this.mapWidth / 2;
    const cy = this.mapHeight / 2;
    // Tile (cx, cy) in pixel space:
    //   px = (cx - cy) * HALF_W = 0
    //   py = (cx + cy) * HALF_H = (mapWidth + mapHeight) * HALF_H / 2
    this.offsetX = (cx - cy) * HALF_W;
    this.offsetY = (cx + cy) * HALF_H;
    this._clamp();
  }

  /**
   * Pan the camera by a screen-pixel delta.
   * The delta is in screen space, so we scale by 1/zoom to get the
   * correct world-pixel offset change.
   * @param {number} dx - Horizontal screen-pixel delta
   * @param {number} dy - Vertical screen-pixel delta
   */
  pan(dx, dy) {
    this.offsetX += dx / this.zoom;
    this.offsetY += dy / this.zoom;
    this._clamp();
  }

  /**
   * Zoom towards a specific screen point (typically the mouse cursor).
   * Adjusts the camera offset so the world point under the cursor stays fixed.
   * @param {number} delta - Scroll delta (>0 = zoom out, <0 = zoom in)
   * @param {number} sx    - Screen X of the zoom target
   * @param {number} sy    - Screen Y of the zoom target
   */
  zoomAt(delta, sx, sy) {
    // Convert the screen point to world-pixel space before zoom changes
    const wpx = (sx - this.viewW / 2) / this.zoom + this.offsetX;
    const wpy = (sy - this.viewH / 2) / this.zoom + this.offsetY;

    // Apply zoom
    const oldZoom = this.zoom;
    this.zoom *= delta > 0 ? 0.9 : 1.1;
    this.zoom = Math.max(this.minZoom, Math.min(this.maxZoom, this.zoom));

    // Adjust offset so the same world-pixel point stays under the cursor
    this.offsetX = wpx - (sx - this.viewW / 2) / this.zoom;
    this.offsetY = wpy - (sy - this.viewH / 2) / this.zoom;

    this._clamp();
  }

  /**
   * Convert world tile coordinates to screen pixel coordinates using this camera.
   * Applies zoom scaling and viewport centering.
   * @param {number} wx - World tile X
   * @param {number} wy - World tile Y
   * @returns {[number, number]} [screenX, screenY] relative to canvas
   */
  worldToScreen(wx, wy) {
    const sx = (wx - wy) * HALF_W * this.zoom
      - this.offsetX * this.zoom
      + this.viewW / 2;
    const sy = (wx + wy) * HALF_H * this.zoom
      - this.offsetY * this.zoom
      + this.viewH / 2;
    return [sx, sy];
  }

  /**
   * Convert screen pixel coordinates to world tile coordinates using this camera.
   * Inverse of this.worldToScreen.
   * @param {number} sx - Screen pixel X
   * @param {number} sy - Screen pixel Y
   * @returns {[number, number]} [worldX, worldY] (fractional tile coords)
   */
  screenToWorld(sx, sy) {
    // Undo viewport centering and zoom to get world-pixel coordinates
    const wpx = (sx - this.viewW / 2) / this.zoom + this.offsetX;
    const wpy = (sy - this.viewH / 2) / this.zoom + this.offsetY;
    // Convert world-pixel to tile coordinates
    const wx = (wpx / HALF_W + wpy / HALF_H) / 2;
    const wy = (wpy / HALF_H - wpx / HALF_W) / 2;
    return [wx, wy];
  }

  /**
   * Update smooth pan velocity.
   * @param {number} dt - Delta time in seconds
   */
  update(dt) {
    if (this.panVX !== 0 || this.panVY !== 0) {
      this.offsetX += this.panVX * dt;
      this.offsetY += this.panVY * dt;
      this._clamp();
    }
  }

  /**
   * Clamp the camera so the viewport does not go past map boundaries.
   * The map occupies a diamond in pixel space from (0,0) top to
   * (mapWidth*HALF_W + mapHeight*HALF_W) wide and tall.
   * @private
   */
  _clamp() {
    // Calculate the world-pixel bounding box of the map
    // Top of diamond: tile (0,0) -> pixel (0, 0)
    // Right of diamond: tile (mapW, 0) -> pixel (mapW*HALF_W, mapW*HALF_H)
    // Bottom of diamond: tile (mapW, mapH) -> pixel ((mapW-mapH)*HALF_W, (mapW+mapM)*HALF_H)
    // Left of diamond: tile (0, mapH) -> pixel (-mapH*HALF_W, mapH*HALF_H)
    const mapW = this.mapWidth;
    const mapH = this.mapHeight;

    // Pixel extents of the map diamond
    const pxMin = -mapH * HALF_W;
    const pxMax = mapW * HALF_W;
    const pyMin = 0;
    const pyMax = (mapW + mapH) * HALF_H;

    // The viewport center in world-pixel space is (offsetX, offsetY).
    // The visible range is +/- viewW/(2*zoom) horizontally and +/- viewH/(2*zoom) vertically.
    const halfVisW = this.viewW / (2 * this.zoom);
    const halfVisH = this.viewH / (2 * this.zoom);

    // Clamp center so viewport stays within map bounds
    this.offsetX = Math.max(pxMin + halfVisW, Math.min(pxMax - halfVisW, this.offsetX));
    this.offsetY = Math.max(pyMin + halfVisH, Math.min(pyMax - halfVisH, this.offsetY));
  }

  /**
   * Get the visible tile range for culling. Returns the bounding box of
   * tiles that could be visible on screen, with a 1-tile margin.
   * @returns {{ minTX: number, maxTX: number, minTY: number, maxTY: number }}
   */
  getVisibleTiles() {
    const [tlx, tly] = this.screenToWorld(0, 0);
    const [trx, try_] = this.screenToWorld(this.viewW, 0);
    const [blx, bly] = this.screenToWorld(0, this.viewH);
    const [brx, bry] = this.screenToWorld(this.viewW, this.viewH);

    const minTX = Math.floor(Math.min(tlx, blx, trx, brx)) - 1;
    const maxTX = Math.ceil(Math.max(tlx, blx, trx, brx)) + 1;
    const minTY = Math.floor(Math.min(tly, try_, bly, bry)) - 1;
    const maxTY = Math.ceil(Math.max(tly, try_, bly, bry)) + 1;

    return { minTX, maxTX, minTY, maxTY };
  }

  /**
   * Handle viewport resize.
   * @param {number} w - New canvas width
   * @param {number} h - New canvas height
   */
  resize(w, h) {
    this.viewW = w;
    this.viewH = h;
  }
}
