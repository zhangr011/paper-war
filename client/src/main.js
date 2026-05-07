// client/src/main.js
// Bootstrap and game loop entry point for Paper War RTS client.
// Wires together: Renderer, Camera, StateManager, Connection, InputHandler.

import { Renderer } from './gl.js';
import { Camera } from './camera.js';
import { StateManager } from './state.js';
import { Connection } from './connection.js';
import { InputHandler } from './input.js';
import { TILE_WIDTH, TILE_HEIGHT } from './iso.js';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const MAP_WIDTH = 48;
const MAP_HEIGHT = 96;

// Fixed-point conversion (server uses int64 with 12-bit fraction)
const FRAC_BITS = 12;
const FIXED_ONE = 1 << FRAC_BITS;

// Tile colors for placeholder terrain (checkerboard grass)
const GRASS_A = { r: 0.18, g: 0.38, b: 0.14 };
const GRASS_B = { r: 0.20, g: 0.42, b: 0.16 };

// Default unit sprite size in screen pixels (placeholder)
const UNIT_SPRITE_W = 20;
const UNIT_SPRITE_H = 24;

// Selection highlight color
const SELECTION_COLOR = { r: 0.2, g: 0.8, b: 1.0, a: 0.25 };
const SELECTION_BORDER_COLOR = { r: 0.3, g: 0.9, b: 1.0, a: 0.7 };

// Cleanup dead units every N frames
const CLEANUP_INTERVAL = 30; // frames (roughly once per second at 30fps)

// Terrain type colors (matching server component.TerrainType)
const TERRAIN_COLORS = [
  { r: 0.18, g: 0.38, b: 0.14 }, // 0 Plain (grass)
  { r: 0.35, g: 0.30, b: 0.22 }, // 1 Road (brown)
  { r: 0.20, g: 0.38, b: 0.50 }, // 2 Shallow (light blue)
  { r: 0.10, g: 0.20, b: 0.40 }, // 3 Deep (dark blue)
  { r: 0.10, g: 0.28, b: 0.10 }, // 4 Forest (dark green)
  { r: 0.40, g: 0.36, b: 0.28 }, // 5 Hill (tan)
  { r: 0.25, g: 0.30, b: 0.18 }, // 6 Swamp (murky green)
  { r: 0.45, g: 0.35, b: 0.20 }, // 7 Bridge (wood brown)
  { r: 0.45, g: 0.45, b: 0.42 }, // 8 Wall (stone gray)
  { r: 0.80, g: 0.85, b: 0.90 }, // 9 Snow (white)
  { r: 0.70, g: 0.60, b: 0.35 }, // 10 Desert (sand)
];

// ---------------------------------------------------------------------------
// Game class
// ---------------------------------------------------------------------------

export class Game {
  constructor(existingConnection) {
    // Canvas elements
    this.canvas = document.getElementById('game-canvas');
    this.minimapCanvas = document.getElementById('minimap-canvas');

    if (!this.canvas) {
      throw new Error('Canvas #game-canvas not found');
    }

    // Initialize modules
    this.renderer = new Renderer(this.canvas);
    this.camera = new Camera(this.canvas.clientWidth, this.canvas.clientHeight);
    this.camera.mapWidth = MAP_WIDTH;
    this.camera.mapHeight = MAP_HEIGHT;
    this.camera.centerOnMap();

    this.state = new StateManager();

    // Use provided connection or create a new one
    if (existingConnection) {
      this.connection = existingConnection;
    } else {
      this.connection = new Connection();
    }

    this.input = new InputHandler(this.canvas, this.camera, this.connection);

    // Timing
    this.lastTime = 0;
    this.frameInterval = 1000 / 30; // 30 fps target
    this.running = false;
    this.frameCount = 0;

    // Map data
    this.mapWidth = MAP_WIDTH;
    this.mapHeight = MAP_HEIGHT;
    this.terrainData = null; // Uint8Array from server

    // Game time for timer display
    this.gameStartTime = 0;
    this.gameTimeSeconds = 0;

    // Player resources (updated from server or placeholder)
    this.gold = 0;
    this.score = 0;

    // Currently selected units for the selection panel
    this.selectedUnits = [];
    this.selectedEntityIDs = new Set();

    // Minimap 2D context
    this.minimapCtx = this.minimapCanvas
      ? this.minimapCanvas.getContext('2d')
      : null;

    // Wire all callbacks between modules
    this.wireCallbacks();

    // Initial resize
    this.handleResize();

    // Clean up dead units periodically
    this.framesSinceCleanup = 0;
  }

  // Set terrain data received from server
  setMapTerrain(data) {
    this.terrainData = data;
    this.camera.mapWidth = this.mapWidth;
    this.camera.mapHeight = this.mapHeight;
    this.camera.centerOnMap();
  }

  // -----------------------------------------------------------------------
  // Callback wiring
  // -----------------------------------------------------------------------

  wireCallbacks() {
    // --- Connection -> State ---
    this.connection.onSnapshot = (snap) => {
      // The connection decodes raw binary, but positions are still fixed-point.
      // Convert them to floating-point before passing to StateManager.
      const units = snap.units.map((u) => {
        const converted = {
          entityID: u.entityID,
          changedMask: u.changedMask,
        };
        if (u.x !== undefined) converted.x = u.x / FIXED_ONE;
        if (u.y !== undefined) converted.y = u.y / FIXED_ONE;
        if (u.vx !== undefined) converted.vx = u.vx / FIXED_ONE;
        if (u.vy !== undefined) converted.vy = u.vy / FIXED_ONE;
        if (u.angle !== undefined) converted.angle = u.angle;
        if (u.hp !== undefined) converted.hp = u.hp;
        if (u.targetID !== undefined) converted.targetID = u.targetID;
        if (u.morale !== undefined) converted.morale = u.morale;
        if (u.state !== undefined) converted.state = u.state;
        if (u.squadID !== undefined) converted.squadID = u.squadID;
        return converted;
      });

      this.state.applySnapshot(snap.tick, snap.prevTick, units, snap.events);
    };

    // --- Connection status ---
    this.connection.onConnect = () => {
      console.log('Connected to server');
      this.updateConnectionStatus(true);
      this.gameStartTime = performance.now();
    };

    this.connection.onDisconnect = () => {
      console.log('Disconnected from server');
      this.updateConnectionStatus(false);
    };

    // --- Input: single-click selection ---
    this.input.onSelect = (worldX, worldY) => {
      this.handleSelect(worldX, worldY);
    };

    // --- Input: box selection ---
    this.input.onBoxSelect = (x1, y1, x2, y2) => {
      this.handleBoxSelect(x1, y1, x2, y2);
    };

    // --- Input: right-click (move/attack) ---
    this.input.onRightClick = (worldX, worldY, targetEntityID) => {
      // InputHandler already sends move commands for selected squads.
      // Here we handle attack-logic if an enemy was clicked.
      // For now the input handler's context menu handler already dispatches
      // move commands via connection.sendMoveSquad for all selected squads.
    };

    // --- Resize ---
    window.addEventListener('resize', () => this.handleResize());

    const testMoveBtn = document.getElementById('team-test-move-btn');
    if (testMoveBtn) {
      testMoveBtn.addEventListener('click', () => this.handleTestMove());
    }
  }

  // -----------------------------------------------------------------------
  // Selection logic
  // -----------------------------------------------------------------------

  /**
   * Handle a single click at the given world coordinates.
   * Find the nearest unit within a tolerance radius and select its squad.
   */
  handleSelect(worldX, worldY) {
    const allUnits = this.state.getRenderUnits();
    const tolerance = 1.5; // world units
    let closest = null;
    let closestDist = tolerance * tolerance;

    for (const unit of allUnits) {
      const dx = unit.renderX - worldX;
      const dy = unit.renderY - worldY;
      const distSq = dx * dx + dy * dy;
      if (distSq < closestDist) {
        closestDist = distSq;
        closest = unit;
      }
    }

    // Clear previous selection
    this.input.selectedSquads.clear();
    this.selectedEntityIDs.clear();
    this.selectedUnits = [];

    if (closest) {
      this.input.selectedSquads.add(this.getCommandSquadID(closest));
      this.selectedEntityIDs.add(closest.entityID);
      this.selectedUnits = [closest];
    }

    this.updateSelectionPanel();
  }

  /**
   * Handle a box selection in screen coordinates.
   * Select all units whose screen positions fall within the box.
   */
  handleBoxSelect(x1, y1, x2, y2) {
    const allUnits = this.state.getRenderUnits();

    this.input.selectedSquads.clear();
    this.selectedEntityIDs.clear();
    this.selectedUnits = [];

    for (const unit of allUnits) {
      const [sx, sy] = this.camera.worldToScreen(unit.renderX, unit.renderY);
      if (sx >= x1 && sx <= x2 && sy >= y1 && sy <= y2) {
        this.input.selectedSquads.add(this.getCommandSquadID(unit));
        this.selectedEntityIDs.add(unit.entityID);
        this.selectedUnits.push(unit);
      }
    }

    this.updateSelectionPanel();
  }

  getCommandSquadID(unit) {
    return unit.squadID || unit.entityID;
  }

  /**
   * Move selected squads to a random target within the currently visible canvas.
   */
  handleTestMove() {
    if (this.input.selectedSquads.size === 0) return;

    const margin = 24;
    const maxX = Math.max(margin, this.camera.viewW - margin);
    const maxY = Math.max(margin, this.camera.viewH - margin);
    const sx = margin + Math.random() * Math.max(1, maxX - margin);
    const sy = margin + Math.random() * Math.max(1, maxY - margin);
    let [worldX, worldY] = this.camera.screenToWorld(sx, sy);

    worldX = Math.max(0, Math.min(this.mapWidth - 0.01, worldX));
    worldY = Math.max(0, Math.min(this.mapHeight - 0.01, worldY));

    const fixedX = Math.round(worldX * FIXED_ONE);
    const fixedY = Math.round(worldY * FIXED_ONE);
    for (const squadID of this.input.selectedSquads) {
      this.connection.sendMoveSquad(squadID, fixedX, fixedY, 0);
    }
  }

  // -----------------------------------------------------------------------
  // Resize handling
  // -----------------------------------------------------------------------

  handleResize() {
    this.renderer.resize();
    const rect = this.canvas.getBoundingClientRect();
    this.camera.resize(rect.width, rect.height);
  }

  // -----------------------------------------------------------------------
  // Game loop
  // -----------------------------------------------------------------------

  start() {
    this.running = true;
    if (!this.connection.connected) {
      this.connection.connect();
    }
    this.lastTime = performance.now();
    this.handleResize();
    requestAnimationFrame((t) => this.loop(t));
  }

  stop() {
    this.running = false;
    this.connection.disconnect();
  }

  loop(now) {
    if (!this.running) return;
    requestAnimationFrame((t) => this.loop(t));

    const elapsed = now - this.lastTime;

    // Throttle to ~30fps: skip frame if called too soon
    if (elapsed < this.frameInterval) {
      return;
    }

    // Compute delta time in seconds, capped to prevent spiral-of-death
    const dt = Math.min(elapsed / 1000, 0.1);
    this.lastTime = now - (elapsed % this.frameInterval);
    this.frameCount++;

    // --- Update phase ---
    this.state.update(now);
    this.input.update(dt);
    this.camera.update(dt);

    // Periodic cleanup
    this.framesSinceCleanup++;
    if (this.framesSinceCleanup >= CLEANUP_INTERVAL) {
      this.state.cleanup();
      this.framesSinceCleanup = 0;
    }

    // --- Render phase ---
    this.render();

    // --- UI phase ---
    this.updateUI();
  }

  // -----------------------------------------------------------------------
  // Rendering
  // -----------------------------------------------------------------------

  render() {
    const visible = this.camera.getVisibleTiles();
    const allUnits = this.state.getRenderUnits();

    // Build terrain tile descriptors for the visible range
    const terrainTiles = this.buildTerrainTiles(visible);

    // Build unit descriptors with screen positions
    const unitDescs = this.buildUnitDescriptors(allUnits);

    // Build selection highlight descriptors
    const selectionHighlights = this.buildSelectionHighlights();

    // Camera offset for the renderer: world-pixel at viewport center converted
    // into the raw projected coordinate space used by descriptors below.
    const cameraOffset = {
      x: this.camera.offsetX * this.camera.zoom - this.camera.viewW / 2,
      y: this.camera.offsetY * this.camera.zoom - this.camera.viewH / 2,
    };

    this.renderer.beginFrame();

    // Pass 1: Terrain
    this.renderer.drawTerrain(terrainTiles, cameraOffset);

    // Pass 2: Terrain objects (none yet)

    // Pass 3: Units (already Y-sorted by buildUnitDescriptors)
    this.renderer.drawUnits(unitDescs, cameraOffset);

    // Pass 4: Selection highlights (drawn as effects)
    if (selectionHighlights.length > 0) {
      this.renderer.drawEffects(selectionHighlights, cameraOffset);
    }

    this.renderer.endFrame();

    // Selection box overlay (drawn on a 2D context or via CSS)
    this.drawSelectionBox();

    // Minimap
    this.drawMinimap(allUnits);
  }

  /**
   * Build placeholder terrain tile descriptors for the visible tile range.
   * Uses a simple checkerboard pattern with two grass colors.
   */
  buildTerrainTiles(visible) {
    const tiles = [];
    const { minTX, maxTX, minTY, maxTY } = visible;

    const mw = this.mapWidth;
    const mh = this.mapHeight;

    // Clamp to map bounds
    const startX = Math.max(0, minTX);
    const endX = Math.min(mw, maxTX);
    const startY = Math.max(0, minTY);
    const endY = Math.min(mh, maxTY);

    const zoom = this.camera.zoom;

    for (let ty = startY; ty < endY; ty++) {
      for (let tx = startX; tx < endX; tx++) {
        // Rectangular tile position in screen pixels.
        const sx = tx * TILE_WIDTH * zoom;
        const sy = ty * TILE_HEIGHT * zoom;

        const tw = TILE_WIDTH * zoom;
        const th = TILE_HEIGHT * zoom;

        // Get terrain color from map data
        let color;
        if (this.terrainData) {
          const idx = ty * mw + tx;
          const terrainType = this.terrainData[idx] || 0;
          color = TERRAIN_COLORS[terrainType] || TERRAIN_COLORS[0];
        } else {
          color = (tx + ty) % 2 === 0 ? GRASS_A : GRASS_B;
        }

        tiles.push({
          x: sx,
          y: sy,
          w: tw,
          h: th,
          r: color.r,
          g: color.g,
          b: color.b,
        });
      }
    }

    return tiles;
  }

  /**
   * Build unit descriptors for rendering from the state's render units.
   * Each unit is converted to raw world-pixel coordinates and Y-sorted.
   * Camera offset is applied by the renderer (same as terrain tiles).
   */
  buildUnitDescriptors(units) {
    const descs = [];
    const zoom = this.camera.zoom;

    for (const unit of units) {
      if (!unit.alive) continue;

      // Raw world-pixel position (same formula as terrain tiles).
      const sx = unit.renderX * TILE_WIDTH * zoom;
      const sy = unit.renderY * TILE_HEIGHT * zoom;

      // Scale sprite size by zoom
      const w = UNIT_SPRITE_W * zoom;
      const h = UNIT_SPRITE_H * zoom;

      // Color based on state: idle=blue, moving=green, attacking=red
      let r = 0.3, g = 0.5, b = 0.8;
      if (unit.currState === 1) { r = 0.2; g = 0.7; b = 0.3; }      // moving
      else if (unit.currState === 2) { r = 0.9; g = 0.2; b = 0.2; } // attacking
      else if (unit.currState === 3) { r = 0.6; g = 0.6; b = 0.2; } // retreating

      // Check if this unit is selected -> brighten
      const isSelected = this.selectedEntityIDs.has(unit.entityID);
      if (isSelected) {
        r = Math.min(1.0, r + 0.3);
        g = Math.min(1.0, g + 0.3);
        b = Math.min(1.0, b + 0.3);
      }

      // HP ratio for tinting damaged units
      if (unit.currHP > 0) {
        const hpRatio = Math.max(0, Math.min(1, unit.currHP / 100));
        if (hpRatio < 0.5) {
          // Blend toward red as HP decreases
          const dmg = 1 - hpRatio * 2;
          r = r + (1.0 - r) * dmg * 0.5;
          g = g * (1 - dmg * 0.3);
          b = b * (1 - dmg * 0.3);
        }
      }

      descs.push({
        x: sx,
        y: sy,
        w: w,
        h: h,
        r: r,
        g: g,
        b: b,
        sortY: sy, // for Y-sorting
      });
    }

    // Y-sort: draw far units first (painter's algorithm)
    descs.sort((a, b) => a.sortY - b.sortY);

    return descs;
  }

  /**
   * Build selection highlight rectangles for selected units.
   */
  buildSelectionHighlights() {
    const highlights = [];
    const zoom = this.camera.zoom;

    for (const unit of this.selectedUnits) {
      if (!unit.alive) continue;

      // Raw world-pixel position (same formula as terrain tiles).
      const sx = unit.renderX * TILE_WIDTH * zoom;
      const sy = unit.renderY * TILE_HEIGHT * zoom;
      const w = UNIT_SPRITE_W * zoom;
      const h = UNIT_SPRITE_H * zoom;

      // Selection circle/highlight below the unit
      highlights.push({
        x: sx - 2,
        y: sy + h - 4,
        w: w + 4,
        h: 4,
        r: SELECTION_COLOR.r,
        g: SELECTION_COLOR.g,
        b: SELECTION_COLOR.b,
        a: SELECTION_COLOR.a,
      });
    }

    return highlights;
  }

  /**
   * Draw the selection box overlay on the main canvas using a 2D context.
   * We use a separate overlay approach: since the WebGL renderer owns the
   * canvas context, we draw the selection box using a small canvas overlay
   * or by temporarily switching context. For simplicity, we use CSS positioning
   * or draw on the minimap canvas. For now, skip as the selection state is
   * already visible through the input handler's getSelectionBox().
   */
  drawSelectionBox() {
    // The selection box is tracked in InputHandler.getSelectionBox().
    // Drawing it requires a 2D canvas overlay, which would be a separate DOM
    // element. For the MVP, the selection box is rendered as a visual indicator
    // through selected unit highlights. A proper overlay can be added later.
  }

  // -----------------------------------------------------------------------
  // Minimap
  // -----------------------------------------------------------------------

  drawMinimap(units) {
    const ctx = this.minimapCtx;
    if (!ctx) return;

    const mw = this.minimapCanvas.width;
    const mh = this.minimapCanvas.height;

    // Clear
    ctx.fillStyle = '#0a0a0a';
    ctx.fillRect(0, 0, mw, mh);

    // Draw map outline as a top-down rectangle so portrait maps read correctly
    // on a phone-sized minimap.
    const pad = 8;
    const mapAspect = this.mapWidth / this.mapHeight;
    let mapDrawH = mh - pad * 2;
    let mapDrawW = mapDrawH * mapAspect;
    if (mapDrawW > mw - pad * 2) {
      mapDrawW = mw - pad * 2;
      mapDrawH = mapDrawW / mapAspect;
    }
    const mapX = (mw - mapDrawW) / 2;
    const mapY = (mh - mapDrawH) / 2;
    const projectToMinimap = (wx, wy) => [
      mapX + (wx / this.mapWidth) * mapDrawW,
      mapY + (wy / this.mapHeight) * mapDrawH,
    ];

    ctx.strokeStyle = '#333';
    ctx.lineWidth = 1;
    ctx.fillStyle = '#1a2a1a';
    ctx.fillRect(mapX, mapY, mapDrawW, mapDrawH);
    ctx.strokeRect(mapX, mapY, mapDrawW, mapDrawH);

    // Draw units as colored dots
    for (const unit of units) {
      if (!unit.alive) continue;

      const [px, py] = projectToMinimap(unit.renderX, unit.renderY);

      // Color based on state
      let color = '#4488cc';
      if (unit.currState === 2) color = '#cc4444';
      else if (unit.currState === 1) color = '#44cc44';

      // Highlight selected units
      if (this.selectedEntityIDs.has(unit.entityID)) {
        color = '#ffffff';
      }

      ctx.fillStyle = color;
      ctx.fillRect(px - 1, py - 1, 2, 2);
    }

    // Draw viewport rectangle on the minimap
    const corners = [
      this.camera.screenToWorld(0, 0),
      this.camera.screenToWorld(this.camera.viewW, 0),
      this.camera.screenToWorld(this.camera.viewW, this.camera.viewH),
      this.camera.screenToWorld(0, this.camera.viewH),
    ];

    ctx.strokeStyle = '#ffffff88';
    ctx.lineWidth = 1;
    ctx.beginPath();
    for (let i = 0; i < corners.length; i++) {
      const [wx, wy] = corners[i];
      const [px, py] = projectToMinimap(wx, wy);
      if (i === 0) ctx.moveTo(px, py);
      else ctx.lineTo(px, py);
    }
    ctx.closePath();
    ctx.stroke();
  }

  // -----------------------------------------------------------------------
  // UI updates
  // -----------------------------------------------------------------------

  updateUI() {
    // Update game timer
    if (this.gameStartTime > 0) {
      this.gameTimeSeconds = (performance.now() - this.gameStartTime) / 1000;
    }
    this.updateTimer();

    // Update resource displays
    const units = this.state.getRenderUnits();
    const unitCountEl = document.querySelector('#unit-count .resource-value');
    if (unitCountEl) unitCountEl.textContent = units.length;

    const goldEl = document.querySelector('#gold .resource-value');
    if (goldEl) goldEl.textContent = this.gold;

    const scoreEl = document.querySelector('#score .resource-value');
    if (scoreEl) scoreEl.textContent = this.score;
  }

  updateTimer() {
    const timerEl = document.getElementById('timer');
    if (!timerEl) return;

    const totalSec = Math.floor(this.gameTimeSeconds);
    const min = Math.floor(totalSec / 60);
    const sec = totalSec % 60;
    timerEl.textContent =
      String(min).padStart(2, '0') + ':' + String(sec).padStart(2, '0');
  }

  updateSelectionPanel() {
    const selUnitsEl = document.getElementById('sel-units');
    const selMoraleEl = document.getElementById('sel-morale');
    const moraleBarEl = document.getElementById('morale-bar');
    const selStatusEl = document.getElementById('sel-status');

    if (this.selectedUnits.length === 0) {
      if (selUnitsEl) selUnitsEl.textContent = '0';
      if (selMoraleEl) selMoraleEl.textContent = '--';
      if (moraleBarEl) moraleBarEl.style.width = '0%';
      if (selStatusEl) selStatusEl.textContent = 'Idle';
      return;
    }

    if (selUnitsEl) selUnitsEl.textContent = this.selectedUnits.length;

    // Compute average morale
    let totalMorale = 0;
    let moraleCount = 0;
    for (const u of this.selectedUnits) {
      if (u.currMorale !== undefined) {
        totalMorale += u.currMorale;
        moraleCount++;
      }
    }
    const avgMorale = moraleCount > 0 ? totalMorale / moraleCount : 75;
    const moralePct = Math.max(0, Math.min(100, avgMorale));
    if (selMoraleEl) selMoraleEl.textContent = Math.round(moralePct) + '%';
    if (moraleBarEl) moraleBarEl.style.width = moralePct + '%';

    // Status from first selected unit's state
    const stateNames = ['Idle', 'Moving', 'Attacking', 'Retreating', 'Defending'];
    const first = this.selectedUnits[0];
    if (selStatusEl) {
      selStatusEl.textContent =
        stateNames[first.currState] || 'Unknown';
    }
  }

  updateConnectionStatus(connected) {
    // Could update a connection indicator in the UI.
    // For now, log to console.
    const status = connected ? 'Connected' : 'Disconnected';
    console.log('Connection status:', status);
  }
}

// ---------------------------------------------------------------------------
// Bootstrap is handled by app.js — Game is instantiated after match is found.
// ---------------------------------------------------------------------------
