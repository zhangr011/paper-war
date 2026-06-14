// client/src/input.js

export const TACTICAL_CHARGE = 0;
export const TACTICAL_RETREAT = 1;
export const TACTICAL_HOLD = 2;
export const TACTICAL_GATHER = 3;

export class InputHandler {
  constructor(canvas, camera, connection) {
    this.canvas = canvas;
    this.camera = camera;
    this.connection = connection;

    // Mouse state
    this.mouseX = 0;
    this.mouseY = 0;
    this.mouseWorldX = 0;
    this.mouseWorldY = 0;

    // Selection state
    this.selecting = false;
    this.selStartX = 0;
    this.selStartY = 0;
    this.selEndX = 0;
    this.selEndY = 0;
    this.selectedSquads = new Set(); // squadIDs

    // Drag pan state
    this.dragging = false;
    this.dragPanning = false;
    this.dragStartX = 0;
    this.dragStartY = 0;
    this.dragLastX = 0;
    this.dragLastY = 0;
    this.dragPanThreshold = 8;

    // Keys held
    this.keys = new Set();

    // Callbacks
    this.onSelect = null;       // callback(worldX, worldY)
    this.onBoxSelect = null;    // callback(x1, y1, x2, y2) in screen coords
    this.onHover = null;        // callback(worldX, worldY)
    this.onRightClick = null;   // callback(worldX, worldY, targetEntityID)
    this.onLeftClick = null;    // callback(worldX, worldY) -> true to consume

    // Bind events
    this._onMouseDown = this._onMouseDown.bind(this);
    this._onMouseMove = this._onMouseMove.bind(this);
    this._onMouseUp = this._onMouseUp.bind(this);
    this._onWheel = this._onWheel.bind(this);
    this._onKeyDown = this._onKeyDown.bind(this);
    this._onKeyUp = this._onKeyUp.bind(this);
    this._onContextMenu = this._onContextMenu.bind(this);

    this.attach();
  }

  attach() {
    this.canvas.addEventListener('mousedown', this._onMouseDown);
    window.addEventListener('mousemove', this._onMouseMove);
    window.addEventListener('mouseup', this._onMouseUp);
    this.canvas.addEventListener('wheel', this._onWheel, { passive: false });
    window.addEventListener('keydown', this._onKeyDown);
    window.addEventListener('keyup', this._onKeyUp);
    this.canvas.addEventListener('contextmenu', this._onContextMenu);
  }

  detach() {
    this.canvas.removeEventListener('mousedown', this._onMouseDown);
    window.removeEventListener('mousemove', this._onMouseMove);
    window.removeEventListener('mouseup', this._onMouseUp);
    this.canvas.removeEventListener('wheel', this._onWheel);
    window.removeEventListener('keydown', this._onKeyDown);
    window.removeEventListener('keyup', this._onKeyUp);
    this.canvas.removeEventListener('contextmenu', this._onContextMenu);
  }

  _onMouseDown(e) {
    const rect = this.canvas.getBoundingClientRect();
    const sx = e.clientX - rect.left;
    const sy = e.clientY - rect.top;

    if (e.button === 0) { // left click
      this.dragging = true;
      this.dragPanning = false;
      this.dragStartX = sx;
      this.dragStartY = sy;
      this.dragLastX = sx;
      this.dragLastY = sy;
      this.selecting = true;
      this.selStartX = sx;
      this.selStartY = sy;
      this.selEndX = sx;
      this.selEndY = sy;
    }
  }

  _onMouseMove(e) {
    const rect = this.canvas.getBoundingClientRect();
    this.mouseX = e.clientX - rect.left;
    this.mouseY = e.clientY - rect.top;

    // Update world coordinates via camera
    const [wx, wy] = this.camera.screenToWorld(this.mouseX, this.mouseY);
    this.mouseWorldX = wx;
    this.mouseWorldY = wy;

    if (this.selecting) {
      this.selEndX = this.mouseX;
      this.selEndY = this.mouseY;
    }

    if (this.dragging) {
      const totalDX = this.mouseX - this.dragStartX;
      const totalDY = this.mouseY - this.dragStartY;
      if (!this.dragPanning && Math.hypot(totalDX, totalDY) >= this.dragPanThreshold) {
        this.dragPanning = true;
        this.selecting = false;
      }

      if (this.dragPanning) {
        const dx = this.mouseX - this.dragLastX;
        const dy = this.mouseY - this.dragLastY;
        this.camera.pan(-dx, -dy);
      }

      this.dragLastX = this.mouseX;
      this.dragLastY = this.mouseY;
    }

    if (this.onHover) this.onHover(this.mouseWorldX, this.mouseWorldY);
  }

  _onMouseUp(e) {
    const rect = this.canvas.getBoundingClientRect();
    const sx = e.clientX - rect.left;
    const sy = e.clientY - rect.top;

    if (e.button === 0) {
      const wasSelecting = this.selecting;
      const wasPanning = this.dragPanning;
      this.dragging = false;
      this.dragPanning = false;
      this.selecting = false;
      if (!wasSelecting || wasPanning) {
        return;
      }

      const dx = Math.abs(sx - this.selStartX);
      const dy = Math.abs(sy - this.selStartY);

      if (dx > 5 || dy > 5) {
        // Box select
        if (this.onBoxSelect) {
          this.onBoxSelect(
            Math.min(this.selStartX, sx),
            Math.min(this.selStartY, sy),
            Math.max(this.selStartX, sx),
            Math.max(this.selStartY, sy)
          );
        }
      } else {
        // Single click select
        const [wx, wy] = this.camera.screenToWorld(sx, sy);
        // Build mode intercepts left-click
        if (this.onLeftClick && this.onLeftClick(wx, wy)) {
          return; // build mode consumed the click
        }
        if (this.onSelect) {
          this.onSelect(wx, wy);
        }
      }
    }
  }

  _onWheel(e) {
    e.preventDefault();
    this.camera.zoomAt(e.deltaY, this.mouseX, this.mouseY);
  }

  _onKeyDown(e) {
    this.keys.add(e.key.toLowerCase());

    // Tactical hotkeys
    const tacticalMap = { q: 0, w: 1, e: 2, r: 3 };
    const tac = tacticalMap[e.key.toLowerCase()];
    if (tac !== undefined && this.selectedSquads.size > 0) {
      for (const squadID of this.selectedSquads) {
        this.connection.sendTacticalOrder(squadID, tac);
      }
    }
  }

  _onKeyUp(e) {
    this.keys.delete(e.key.toLowerCase());
  }

  _onContextMenu(e) {
    e.preventDefault();
    // Right-click: move or attack command
    if (this.selectedSquads.size > 0) {
      const rect = this.canvas.getBoundingClientRect();
      const sx = e.clientX - rect.left;
      const sy = e.clientY - rect.top;
      const [wx, wy] = this.camera.screenToWorld(sx, sy);

      // Convert world tile coords to fixed-point for server
      // Server expects fixed-point int32 for TargetX/TargetY
      const fixedX = Math.round(wx * 4096);
      const fixedY = Math.round(wy * 4096);

      if (this.onRightClick) {
        this.onRightClick(wx, wy, 0); // 0 = no specific target
      }

      // Send move command for all selected squads
      for (const squadID of this.selectedSquads) {
        this.connection.sendMoveSquad(squadID, fixedX, fixedY, 0);
      }
    }
  }

  // Call each frame to handle keyboard panning.
  update(dt) {
    let panX = 0, panY = 0;
    const speed = 500 * dt; // pixels per second

    // WASD panning
    if (this.keys.has('w') || this.keys.has('arrowup')) panY -= speed;
    if (this.keys.has('s') || this.keys.has('arrowdown')) panY += speed;
    if (this.keys.has('a') || this.keys.has('arrowleft')) panX -= speed;
    if (this.keys.has('d') || this.keys.has('arrowright')) panX += speed;

    if (panX !== 0 || panY !== 0) {
      this.camera.pan(panX, panY);
    }
  }

  // Get selection box rectangle (for rendering) or null
  getSelectionBox() {
    if (!this.selecting) return null;
    return {
      x: Math.min(this.selStartX, this.selEndX),
      y: Math.min(this.selStartY, this.selEndY),
      w: Math.abs(this.selEndX - this.selStartX),
      h: Math.abs(this.selEndY - this.selStartY)
    };
  }
}
