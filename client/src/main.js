// client/src/main.js
// Bootstrap and game loop entry point for Paper War RTS client.
// Wires together: Renderer, Camera, StateManager, Connection, InputHandler.

import { Renderer } from './gl.js?v=fix-view-2';
import { Camera } from './camera.js?v=fix-view-2';
import { StateManager } from './state.js?v=death-fix';
import { Connection } from './connection.js?v=msg-fix';
import { InputHandler } from './input.js?v=fix-view-2';
import { TILE_WIDTH, TILE_HEIGHT } from './iso.js?v=fix-view-2';

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

// Max HP per CombatUnitType (must match server CombatUnitTypeTable)
// Commanders get 3x these values
const UNIT_MAX_HP = [80, 60, 40, 60, 120, 150, 130];

// Default unit sprite sizes per CombatUnitType (0-6)
// LI=small, HI=medium, Sniper=small, AAI=small, MG=medium, MA=large, MM=large
const UNIT_SIZES = [
  { w: 18, h: 22 }, // 0 Light Infantry
  { w: 22, h: 26 }, // 1 Heavy Infantry
  { w: 16, h: 24 }, // 2 Sniper
  { w: 18, h: 22 }, // 3 Anti-Armor Infantry
  { w: 26, h: 22 }, // 4 Motor Gun
  { w: 30, h: 24 }, // 5 Motor Artillery
  { w: 28, h: 24 }, // 6 Motor Missile
];

// Unit type colors (team-tinted): each type has a base hue
const UNIT_TYPE_COLORS = [
  { r: 0.3, g: 0.6, b: 0.9 }, // 0 LI  — blue
  { r: 0.4, g: 0.5, b: 0.7 }, // 1 HI  — steel blue
  { r: 0.5, g: 0.8, b: 0.5 }, // 2 Sniper — green
  { r: 0.6, g: 0.6, b: 0.3 }, // 3 AAI — olive
  { r: 0.8, g: 0.5, b: 0.3 }, // 4 MG  — orange
  { r: 0.7, g: 0.3, b: 0.3 }, // 5 MA  — red
  { r: 0.5, g: 0.3, b: 0.7 }, // 6 MM  — purple
];

// Team tint: team 1 is blue, team 2 is red
function teamTint(base, team) {
  if (team === 1) {
    // Blue team: shift toward blue, reduce red
    return {
      r: base.r * 0.5,
      g: base.g * 0.7,
      b: Math.min(1.0, base.b * 1.4 + 0.2),
    };
  }
  if (team === 2) {
    // Red team: shift toward red, reduce blue
    return {
      r: Math.min(1.0, base.r * 1.4 + 0.2),
      g: base.g * 0.6,
      b: base.b * 0.4,
    };
  }
  return base; // team 0 or other: use base color
}

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
  { r: 0.42, g: 0.39, b: 0.34 }, // 11 Stronghold L1
  { r: 0.48, g: 0.43, b: 0.36 }, // 12 Stronghold L2
  { r: 0.55, g: 0.47, b: 0.38 }, // 13 Stronghold L3
  { r: 0.62, g: 0.50, b: 0.39 }, // 14 Stronghold L4
  { r: 0.70, g: 0.52, b: 0.38 }, // 15 Stronghold L5
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
    this.gold = 50; // v1 starting gold
    this.score = 0;

    // AttackGround mode
    this.attackGroundMode = false;

    // Unit costs (must match server CombatUnitTypeTable)
    this.unitCosts = [15, 25, 50, 30, 25, 50, 60];

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
    this.centerCameraOnPlayerStart();
  }

  centerCameraOnPlayerStart() {
    const startY = this.playerID === 2 ? this.mapHeight - 10 : 10;
    this.camera.offsetX = (this.mapWidth / 2) * TILE_WIDTH;
    this.camera.offsetY = startY * TILE_HEIGHT;
    this.camera._clamp();
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
        if (u.unitType !== undefined) converted.unitType = u.unitType;
        if (u.team !== undefined) converted.team = u.team;
        return converted;
      });

      this.state.applySnapshot(snap.tick, snap.prevTick, units, snap.events, snap.fog);
    };

    // --- Connection status ---
    this.connection.onConnect = () => {
      this.updateConnectionStatus(true);
      this.gameStartTime = performance.now();
    };

    this.connection.onDisconnect = () => {
      this.updateConnectionStatus(false);
    };

    // --- Server messages ---
    this.connection.onGoldUpdate = (gold) => {
      this.gold = gold;
    };

    this.connection.onMatchResult = ({ winner, reason }) => {
      this.showMatchResult(winner, reason);
    };

    this.connection.onRosterUpdate = (rosterData) => {
      // Parse roster binary: commanderLevel(uint8) + unitCount(uint8) + [unitType(uint8)+count(uint8)]*N
      try {
        const view = new DataView(rosterData.buffer, rosterData.byteOffset, rosterData.byteLength);
        let off = 0;
        const cmdLevel = view.getUint8(off); off += 1;
        const unitCount = view.getUint8(off); off += 1;
        const units = [];
        for (let i = 0; i < unitCount; i++) {
          units.push({ type: view.getUint8(off), count: view.getUint8(off + 1) });
          off += 2;
        }
      } catch (e) {
        console.warn('Failed to parse roster data:', e);
      }
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

    // --- Recruit buttons ---
    document.querySelectorAll('.recruit-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        const unitType = parseInt(btn.dataset.unitType, 10);
        this.handleRecruit(unitType);
      });
    });

    // --- Attack Ground toggle ---
    const agBtn = document.getElementById('attack-ground-btn');
    if (agBtn) {
      agBtn.addEventListener('click', () => this.toggleAttackGround());
    }

    // --- Keyboard shortcut: A for Attack Ground ---
    this.input.onKeyDown = (key) => {
      if (key === 'a' || key === 'A') {
        this.toggleAttackGround();
      } else if (key === 'q') {
        this.handleTactic('charge');
      } else if (key === 'w') {
        this.handleTactic('retreat');
      } else if (key === 'e') {
        this.handleTactic('defend');
      } else if (key === 'r') {
        this.handleTactic('rally');
      }
    };

    // --- Tactic buttons ---
    document.querySelectorAll('[data-tactic]').forEach(btn => {
      btn.addEventListener('click', () => {
        this.handleTactic(btn.dataset.tactic);
      });
    });
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
      this.selectSquads([this.getCommandSquadID(closest)]);
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
      }
    }

    this.syncSelectedTeamUnits();
    this.updateSelectionPanel();
  }

  getCommandSquadID(unit) {
    return unit.squadID || unit.entityID;
  }

  selectSquads(squadIDs) {
    this.input.selectedSquads.clear();
    for (const squadID of squadIDs) {
      this.input.selectedSquads.add(squadID);
    }
    this.syncSelectedTeamUnits();
  }

  syncSelectedTeamUnits() {
    this.selectedEntityIDs.clear();
    this.selectedUnits = [];

    if (this.input.selectedSquads.size === 0) return;

    for (const unit of this.state.getRenderUnits()) {
      if (!this.input.selectedSquads.has(this.getCommandSquadID(unit))) continue;
      this.selectedEntityIDs.add(unit.entityID);
      this.selectedUnits.push(unit);
    }
  }

  /**
   * Move selected squads to a random target within the currently visible canvas.
   */
  handleTestMove() {
    if (this.input.selectedSquads.size === 0) return;

    const selectedUnits = this.state.getRenderUnits()
      .filter((unit) => this.input.selectedSquads.has(this.getCommandSquadID(unit)));
    const center = selectedUnits.length > 0
      ? selectedUnits.reduce((acc, unit) => {
        acc.x += unit.renderX;
        acc.y += unit.renderY;
        return acc;
      }, { x: 0, y: 0 })
      : null;
    if (center) {
      center.x /= selectedUnits.length;
      center.y /= selectedUnits.length;
    }

    const margin = 24;
    const maxX = Math.max(margin, this.camera.viewW - margin);
    const maxY = Math.max(margin, this.camera.viewH - margin);
    let worldX = 0;
    let worldY = 0;

    for (let attempt = 0; attempt < 8; attempt++) {
      const sx = margin + Math.random() * Math.max(1, maxX - margin);
      const sy = margin + Math.random() * Math.max(1, maxY - margin);
      [worldX, worldY] = this.camera.screenToWorld(sx, sy);
      if (!center || Math.hypot(worldX - center.x, worldY - center.y) >= 8) {
        break;
      }
    }

    if (center && Math.hypot(worldX - center.x, worldY - center.y) < 8) {
      const [leftWorld] = this.camera.screenToWorld(margin, this.camera.viewH / 2);
      const [rightWorld] = this.camera.screenToWorld(maxX, this.camera.viewH / 2);
      worldX = center.x < (leftWorld + rightWorld) / 2 ? rightWorld : leftWorld;
      worldY = center.y;
    }

    worldX = Math.max(0, Math.min(this.mapWidth - 0.01, worldX));
    worldY = Math.max(0, Math.min(this.mapHeight - 0.01, worldY));

    const fixedX = Math.round(worldX * FIXED_ONE);
    const fixedY = Math.round(worldY * FIXED_ONE);
    for (const squadID of this.input.selectedSquads) {
      this.connection.sendMoveSquad(squadID, fixedX, fixedY, 0);
    }
  }

  // -----------------------------------------------------------------------
  // Tactic handling
  // -----------------------------------------------------------------------

  handleTactic(tactic) {
    if (this.input.selectedSquads.size === 0) return;

    const units = this.state.getRenderUnits();
    const myUnits = units.filter(u => u.team === 1); // player team

    switch (tactic) {
      case 'charge': {
        // Move selected squads toward center of mass of visible enemies, attack-ground style
        const enemies = units.filter(u => u.team !== 1);
        if (enemies.length === 0) return;
        let cx = 0, cy = 0;
        for (const e of enemies) { cx += e.renderX; cy += e.renderY; }
        cx /= enemies.length; cy /= enemies.length;
        const fixedX = Math.round(cx * FIXED_ONE);
        const fixedY = Math.round(cy * FIXED_ONE);
        for (const squadID of this.input.selectedSquads) {
          this.connection.sendAttackGround(squadID, fixedX, fixedY, 0);
        }
        break;
      }
      case 'retreat': {
        // Move selected squads away from center of enemies (opposite direction)
        const enemies = units.filter(u => u.team !== 1);
        let centerX = 0, centerY = 0;
        for (const u of myUnits) { centerX += u.renderX; centerY += u.renderY; }
        centerX /= myUnits.length; centerY /= myUnits.length;

        if (enemies.length > 0) {
          let ecx = 0, ecy = 0;
          for (const e of enemies) { ecx += e.renderX; ecy += e.renderY; }
          ecx /= enemies.length; ecy /= enemies.length;
          // Move 10 world units in opposite direction
          const dx = centerX - ecx;
          const dy = centerY - ecy;
          const dist = Math.sqrt(dx * dx + dy * dy) || 1;
          const targetX = Math.round((centerX + (dx / dist) * 10) * FIXED_ONE);
          const targetY = Math.round((centerY + (dy / dist) * 10) * FIXED_ONE);
          for (const squadID of this.input.selectedSquads) {
            this.connection.sendMoveSquad(squadID, targetX, targetY, 0);
          }
        }
        break;
      }
      case 'defend': {
        // Hold position: send move to current positions (effectively stops them)
        for (const squadID of this.input.selectedSquads) {
          const squadUnits = myUnits.filter(u => u.squadID === squadID);
          if (squadUnits.length > 0) {
            const fx = Math.round(squadUnits[0].renderX * FIXED_ONE);
            const fy = Math.round(squadUnits[0].renderY * FIXED_ONE);
            this.connection.sendMoveSquad(squadID, fx, fy, 0);
          }
        }
        break;
      }
      case 'rally': {
        // Move selected squads to the commander's position
        const commander = myUnits.find(u => u.unitType === 0 && u.hp > u.maxHP * 2);
        if (!commander) return;
        const fx = Math.round(commander.renderX * FIXED_ONE);
        const fy = Math.round(commander.renderY * FIXED_ONE);
        for (const squadID of this.input.selectedSquads) {
          if (squadID !== commander.squadID) {
            this.connection.sendMoveSquad(squadID, fx, fy, 0);
          }
        }
        break;
      }
    }
  }

  // -----------------------------------------------------------------------
  // Resize handling
  // -----------------------------------------------------------------------

  handleRecruit(unitType) {
    const cost = this.unitCosts[unitType] || 0;
    if (this.gold < cost) return; // not enough gold

    // Send recruit command: use first selected squad as the commander's squad
    let squadID = 0;
    if (this.input.selectedSquads.size > 0) {
      squadID = this.input.selectedSquads.values().next().value;
    }

    // Build binary command: Type(0x06) + Seq(uint32) + PredictedTick(uint32) + SquadID(uint32) + RecruitType(uint8) = 14
    const CMD_RECRUIT = 0x06;
    const buf = new ArrayBuffer(13 + 1);
    const view = new DataView(buf);
    let off = 0;
    view.setUint8(off, CMD_RECRUIT); off += 1;
    view.setUint32(off, this.connection.seq++, true); off += 4;
    view.setUint32(off, 0, true); off += 4; // predictedTick
    view.setUint32(off, squadID, true); off += 4;
    view.setUint8(off, unitType); off += 1;
    this.connection.send(buf);
  }

  // -----------------------------------------------------------------------
  // Attack Ground mode
  // -----------------------------------------------------------------------

  toggleAttackGround() {
    this.attackGroundMode = !this.attackGroundMode;
    const btn = document.getElementById('attack-ground-btn');
    if (btn) {
      btn.classList.toggle('active', this.attackGroundMode);
    }
    // Override right-click behavior: next right-click sends AttackGround
    if (this.attackGroundMode) {
      this.input.onRightClick = (worldX, worldY) => {
        const fixedX = Math.round(worldX * FIXED_ONE);
        const fixedY = Math.round(worldY * FIXED_ONE);
        for (const squadID of this.input.selectedSquads) {
          this.connection.sendAttackGround(squadID, fixedX, fixedY, 0);
        }
        this.attackGroundMode = false;
        const btn = document.getElementById('attack-ground-btn');
        if (btn) btn.classList.remove('active');
      };
    } else {
      this.input.onRightClick = () => {};
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
    if (this.input.selectedSquads.size > 0) {
      this.syncSelectedTeamUnits();
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

    // Pass 1.5: Fog overlay
    const fogTiles = this.buildFogTiles(visible);
    if (fogTiles.length > 0) {
      this.renderer.drawFog(fogTiles, cameraOffset);
    }

    // Pass 2: Terrain objects (none yet)

    // Pass 3: Units (already Y-sorted by buildUnitDescriptors)
    this.renderer.drawUnits(unitDescs, cameraOffset);

    // Pass 3.5: HP bars above units (uses effects batch)
    this.renderer.drawHPBars(unitDescs, cameraOffset);

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
   * Build fog overlay tile descriptors for fogged areas.
   * 3-state fog: 0=unexplored (fully black), 1=explored (dimmed), 2=visible (skip).
   * Returns dark semi-transparent quads for non-visible tiles.
   */
  buildFogTiles(visible) {
    const fog = this.state.fogVisible;
    if (!fog) return [];
    const fogW = this.state.fogWidth;
    const tiles = [];
    const { minTX, maxTX, minTY, maxTY } = visible;
    const startX = Math.max(0, minTX);
    const endX = Math.min(this.mapWidth, maxTX);
    const startY = Math.max(0, minTY);
    const endY = Math.min(this.mapHeight, maxTY);
    const zoom = this.camera.zoom;

    for (let ty = startY; ty < endY; ty++) {
      for (let tx = startX; tx < endX; tx++) {
        const state = fog[ty * fogW + tx];
        if (state === 2) continue; // visible — skip
        const sx = tx * TILE_WIDTH * zoom;
        const sy = ty * TILE_HEIGHT * zoom;
        const tw = TILE_WIDTH * zoom;
        const th = TILE_HEIGHT * zoom;
        if (state === 0) {
          // Unexplored — fully black
          tiles.push({ x: sx, y: sy, w: tw, h: th, r: 0.0, g: 0.0, b: 0.0, a: 0.92 });
        } else {
          // Explored but not currently visible — dimmed
          tiles.push({ x: sx, y: sy, w: tw, h: th, r: 0.0, g: 0.0, b: 0.0, a: 0.45 });
        }
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

      // Size based on unit type
      const sizeIdx = Math.min(unit.unitType || 0, UNIT_SIZES.length - 1);
      const size = UNIT_SIZES[sizeIdx];
      const w = size.w * zoom;
      const h = size.h * zoom;

      // Color: base type color, tinted by team
      const baseColor = UNIT_TYPE_COLORS[sizeIdx] || UNIT_TYPE_COLORS[0];
      let color = teamTint(baseColor, unit.team || 0);

      let r = color.r;
      let g = color.g;
      let b = color.b;

      // State overlay: darken idle, brighten moving, redden attacking
      if (unit.currState === 1) { r *= 1.2; g *= 1.1; b *= 1.0; }      // moving: brighter
      else if (unit.currState === 2) { r = Math.min(1.0, r + 0.3); g *= 0.7; b *= 0.5; } // attacking: redder
      else if (unit.currState === 3) { r *= 0.8; g *= 0.8; b *= 0.8; } // retreating: darker

      // Check if this unit is selected -> brighten
      const isSelected = this.selectedEntityIDs.has(unit.entityID);
      if (isSelected) {
        r = Math.min(1.0, r + 0.3);
        g = Math.min(1.0, g + 0.3);
        b = Math.min(1.0, b + 0.3);
      }

      // HP ratio for tinting damaged units
      const maxHP = UNIT_MAX_HP[sizeIdx] || 80;
      const hpRatio = Math.max(0, Math.min(1, unit.currHP / maxHP));
      if (hpRatio < 0.5) {
        // Blend toward red as HP decreases
        const dmg = 1 - hpRatio * 2;
        r = r + (1.0 - r) * dmg * 0.5;
        g = g * (1 - dmg * 0.3);
        b = b * (1 - dmg * 0.3);
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
        hpRatio: hpRatio,
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
      const sizeIdx = Math.min(unit.unitType || 0, UNIT_SIZES.length - 1);
      const size = UNIT_SIZES[sizeIdx];
      const w = size.w * zoom;
      const h = size.h * zoom;

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

      // Color based on team and state
      const sizeIdx = Math.min(unit.unitType || 0, UNIT_TYPE_COLORS.length - 1);
      const baseColor = UNIT_TYPE_COLORS[sizeIdx] || UNIT_TYPE_COLORS[0];
      const tc = teamTint(baseColor, unit.team || 0);
      let color = `rgb(${Math.round(tc.r*255)},${Math.round(tc.g*255)},${Math.round(tc.b*255)})`;

      if (unit.currState === 2) color = '#cc4444'; // attacking override

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

    // Spectator scoreboard for clash mode (playerID=0)
    if (this.connection.playerID === 0) {
      this.updateSpectatorScoreboard();
    }

    // Update resource displays
    const units = this.state.getRenderUnits();
    const unitCountEl = document.querySelector('#unit-count .resource-value');
    if (unitCountEl) unitCountEl.textContent = units.length;

    const goldEl = document.querySelector('#gold .resource-value');
    if (goldEl) goldEl.textContent = this.gold;

    const scoreEl = document.querySelector('#score .resource-value');
    if (scoreEl) scoreEl.textContent = this.score;

    // Update recruit button disabled state based on gold
    document.querySelectorAll('.recruit-btn').forEach(btn => {
      const unitType = parseInt(btn.dataset.unitType, 10);
      const cost = this.unitCosts[unitType] || 0;
      btn.classList.toggle('disabled', this.gold < cost);
    });
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

  updateSpectatorScoreboard() {
    let board = document.getElementById('spectator-scoreboard');
    if (!board) {
      board = document.createElement('div');
      board.id = 'spectator-scoreboard';
      board.style.cssText = 'position:fixed;top:8px;left:50%;transform:translateX(-50%);' +
        'z-index:100;font-family:sans-serif;display:flex;align-items:center;gap:12px;' +
        'background:rgba(0,0,0,0.6);padding:6px 16px;border-radius:6px;color:#fff;font-size:14px;';
      document.body.appendChild(board);
    }

    const units = this.state.getRenderUnits();
    let blue = 0, red = 0;
    for (const u of units) {
      if (!u.alive) continue;
      if (u.team === 1) blue++;
      else if (u.team === 2) red++;
    }

    board.innerHTML =
      `<span style="color:#4488FF;font-weight:bold">BLUE ${blue}</span>` +
      `<span style="color:#888">vs</span>` +
      `<span style="color:#FF4444;font-weight:bold">RED ${red}</span>`;
  }

  showMatchResult(winner, reason) {
    // Show a simple overlay with the result
    let overlay = document.getElementById('match-result-overlay');
    if (!overlay) {
      overlay = document.createElement('div');
      overlay.id = 'match-result-overlay';
      overlay.style.cssText = 'position:fixed;top:0;left:0;width:100%;height:100%;' +
        'background:rgba(0,0,0,0.7);display:flex;align-items:center;justify-content:center;' +
        'z-index:9999;color:#fff;font-family:sans-serif;';
      document.body.appendChild(overlay);
    }
    const pid = this.connection.playerID;
    let heading, headingColor;
    if (pid === 0) {
      // Spectator: show neutral result (winner=0 is FactionPlayer/Blue, winner=1 is FactionEnemy/Red)
      const teamName = winner === 0 ? 'Blue' : 'Red';
      heading = `Team ${teamName} Wins!`;
      headingColor = winner === 0 ? '#4488FF' : '#FF4444';
    } else {
      const isWin = winner === pid;
      heading = isWin ? 'Victory!' : 'Defeat';
      headingColor = isWin ? '#4CAF50' : '#FF4444';
    }
    overlay.innerHTML =
      '<div style="text-align:center">' +
      `<h1 style="font-size:48px;margin:0;color:${headingColor}">${heading}</h1>` +
      '<p style="font-size:20px;margin:16px 0">' + reason + '</p>' +
      '<button id="match-result-ok" ' +
      'style="padding:12px 32px;font-size:18px;cursor:pointer">OK</button>' +
      '</div>';
    document.getElementById('match-result-ok').addEventListener('click', () => {
      overlay.remove();
      const app = window.__paperWarApp;
      if (app) {
        // Mark old connection for clean disconnect
        if (app.connection) app.connection._intentionalClose = true;
        app.lobbyStatus.textContent = 'Ready for battle';
        app.lobbySpinner.style.display = 'none';
        app.soloBtn.disabled = false;
        app.clashBtn.disabled = false;
        app.findMatchBtn.disabled = false;
        app.showScreen('lobby');
      }
    });
  }

  updateConnectionStatus(connected) {
    // Could update a connection indicator in the UI.
    // For now, log to console.
    const status = connected ? 'Connected' : 'Disconnected';
  }
}

// ---------------------------------------------------------------------------
// Bootstrap is handled by app.js — Game is instantiated after match is found.
// ---------------------------------------------------------------------------
