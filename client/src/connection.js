// client/src/connection.js — WebSocket client with binary protocol matching the server.
// Binary encoding: little-endian throughout.

// ---------------------------------------------------------------------------
// Command types (client -> server)
// ---------------------------------------------------------------------------
export const CMD_MOVE_SQUAD = 0x01;
export const CMD_ATTACK_TARGET = 0x02;
export const CMD_ATTACK_GROUND = 0x03;
export const CMD_TACTICAL_ORDER = 0x05;
export const CMD_BUILD = 0x08;

// ---------------------------------------------------------------------------
// ChangedMask bits (server -> client unit deltas)
// ---------------------------------------------------------------------------
export const CHANGED_POSITION = 1 << 0;
export const CHANGED_VELOCITY = 1 << 1;
export const CHANGED_ANGLE = 1 << 2;
export const CHANGED_HP = 1 << 3;
export const CHANGED_TARGET_ID = 1 << 4;
export const CHANGED_MORALE = 1 << 5;
export const CHANGED_STATE = 1 << 6;
export const CHANGED_SQUAD_ID = 1 << 7;

// ---------------------------------------------------------------------------
// Event types (server -> client)
// ---------------------------------------------------------------------------
export const EVENT_DAMAGE = 0;
export const EVENT_DEATH = 1;
export const EVENT_TERRAIN_CHANGE = 2;
export const EVENT_COMMANDER_DOWN = 3;
export const EVENT_PROJECTILE = 4;

// ---------------------------------------------------------------------------
// Command header size: Type(uint8) + ClientSeq(uint32) + PredictedTick(uint32) + SquadID(uint32) = 13
// ---------------------------------------------------------------------------
const CMD_HEADER_SIZE = 1 + 4 + 4 + 4;

export class Connection {
  /**
   * @param {string} [url] — WebSocket endpoint. Defaults to ws://{host}/ws.
   */
  constructor(url) {
    this.url = url || `ws://${window.location.host}/ws`;
    this.ws = null;
    this.connected = false;
    this.seq = 0;
    this.playerID = 0;

    // Public callbacks
    this.onSnapshot = null;   // (snapshot) => void
    this.onConnect = null;    // () => void
    this.onDisconnect = null; // () => void
    this.onTextMessage = null; // (msg: object) => void — JSON text messages
    this.onMapData = null;    // (terrainData: Uint8Array) => void
    this.onCreepData = null;  // (creepData: Uint8Array) => void — Phase 4 creep overlay

    // Reconnection state (exponential back-off)
    this.reconnectDelay = 1000;
    this.maxReconnectDelay = 30000;
    this.reconnectTimer = null;

    // Match reconnect token — set when a match starts, cleared on intentional
    // disconnect or when the server rejects the reconnect. When non-null and
    // the socket drops, we automatically try to rejoin the in-progress match.
    this.reconnectToken = null;

    // Heartbeat
    this.pingInterval = null;
    this.lastPong = 0;
  }

  // -----------------------------------------------------------------------
  // Connection lifecycle
  // -----------------------------------------------------------------------

  connect() {
    this.ws = new WebSocket(this.url);
    this.ws.binaryType = 'arraybuffer';

    this.ws.onopen = () => {
      this.connected = true;
      this.reconnectDelay = 1000;
      this.startHeartbeat();
      // If we have a reconnect token, try to rejoin the in-progress match
      // instead of presenting as a fresh connection.
      if (this.reconnectToken) {
        this.ws.send(JSON.stringify({ type: 'reconnect', token: this.reconnectToken }));
        // onConnect still fires — Game uses it for status display — but the
        // actual rejoin is completed when the server replies reconnect_ok.
      }
      if (this.onConnect) this.onConnect();
    };

    this.ws.onclose = () => {
      this.connected = false;
      this.stopHeartbeat();
      // Intentional disconnect (user quit) — don't auto-reconnect.
      if (this._intentionalClose) return;
      // Mid-match drop — show overlay, then retry with back-off.
      if (this.reconnectToken && this.onReconnecting) this.onReconnecting();
      if (this.onDisconnect) this.onDisconnect();
      this.scheduleReconnect();
    };

    this.ws.onerror = (err) => {
      // Silence expected errors during intentional disconnect
      if (!this._intentionalClose) {
        console.error('WebSocket error:', err);
      }
    };

    this.ws.onmessage = (event) => {
      if (typeof event.data === 'string') {
        // JSON text message
        try {
          const msg = JSON.parse(event.data);
          if (this.onTextMessage) {
            this.onTextMessage(msg);
          }
        } catch (e) {
          console.error('Failed to parse JSON:', e);
        }
      } else {
        // Binary message — dispatch on the 2-byte prefix.
        const view = new DataView(event.data);
        if (view.byteLength >= 2 && view.getUint8(0) === 0xFF && view.getUint8(1) === 0xFD) {
          // Map terrain data (prefix 0xFF 0xFD)
          const terrainData = new Uint8Array(event.data, 2);
          if (this.onMapData) {
            this.onMapData(terrainData);
          }
        } else if (view.byteLength >= 2 && view.getUint8(0) === 0xFF && view.getUint8(1) === 0xFC) {
          // Creep overlay (prefix 0xFF 0xFC) — raw w*h bytes, one CreepOwner
          // (0/1/2) per tile. Broadcast at ~2Hz. Phase 4.
          const creepData = new Uint8Array(event.data, 2);
          if (this.onCreepData) {
            this.onCreepData(creepData);
          }
        } else {
          // Snapshot data or server message — handleMessage handles both
          this.handleMessage(event.data);
        }
      }
    };
  }

  disconnect() {
    this._intentionalClose = true;
    this.reconnectToken = null; // intentional disconnect — don't auto-rejoin
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
    this.stopHeartbeat();
    clearTimeout(this.reconnectTimer);
  }

  // -----------------------------------------------------------------------
  // Send helpers
  // -----------------------------------------------------------------------

  /**
   * Write the common command header into buf starting at offset 0.
   * @returns {number} offset past the header (always CMD_HEADER_SIZE).
   */
  _writeHeader(buf, type, squadID, predictedTick) {
    const view = new DataView(buf);
    let off = 0;
    view.setUint8(off, type); off += 1;
    view.setUint32(off, this.seq++, true); off += 4;
    view.setUint32(off, predictedTick || 0, true); off += 4;
    view.setUint32(off, squadID, true); off += 4;
    return off;
  }

  /** Send a raw ArrayBuffer if the socket is open. */
  send(buf) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(buf);
    }
  }

  /** Send a JSON-encoded object as a text message. */
  sendJSON(obj) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(obj));
    }
  }

  // -----------------------------------------------------------------------
  // Command senders
  // -----------------------------------------------------------------------

  /**
   * CmdMoveSquad (0x01): header + TargetX(int32) + TargetY(int32) = 21 bytes.
   */
  sendMoveSquad(squadID, targetX, targetY, predictedTick) {
    const buf = new ArrayBuffer(CMD_HEADER_SIZE + 4 + 4);
    const view = new DataView(buf);
    let off = this._writeHeader(buf, CMD_MOVE_SQUAD, squadID, predictedTick);
    view.setInt32(off, targetX, true); off += 4;
    view.setInt32(off, targetY, true); off += 4;
    this.send(buf);
  }

  /**
   * CmdAttackTarget (0x02): header + TargetID(uint32) = 17 bytes.
   */
  sendAttackTarget(squadID, targetID, predictedTick) {
    const buf = new ArrayBuffer(CMD_HEADER_SIZE + 4);
    const view = new DataView(buf);
    let off = this._writeHeader(buf, CMD_ATTACK_TARGET, squadID, predictedTick);
    view.setUint32(off, targetID, true); off += 4;
    this.send(buf);
  }

  /**
   * CmdAttackGround (0x03): header + TargetX(int32) + TargetY(int32) = 21 bytes.
   */
  sendAttackGround(squadID, targetX, targetY, predictedTick) {
    const buf = new ArrayBuffer(CMD_HEADER_SIZE + 4 + 4);
    const view = new DataView(buf);
    let off = this._writeHeader(buf, CMD_ATTACK_GROUND, squadID, predictedTick);
    view.setInt32(off, targetX, true); off += 4;
    view.setInt32(off, targetY, true); off += 4;
    this.send(buf);
  }

  /**
   * CmdTacticalOrder (0x05): header + OrderType(uint8) = 14 bytes.
   */
  sendTacticalOrder(squadID, orderType) {
    const buf = new ArrayBuffer(CMD_HEADER_SIZE + 1);
    const view = new DataView(buf);
    const off = this._writeHeader(buf, CMD_TACTICAL_ORDER, squadID, 0);
    view.setUint8(off, orderType);
    this.send(buf);
  }

  /**
   * CmdBuild (0x08): header + StructureType(uint8) + TargetX(int32) + TargetY(int32) = 22 bytes.
   */
  sendBuild(structureType, targetX, targetY) {
    const buf = new ArrayBuffer(CMD_HEADER_SIZE + 1 + 4 + 4);
    const view = new DataView(buf);
    let off = this._writeHeader(buf, CMD_BUILD, 0, 0);
    view.setUint8(off, structureType); off += 1;
    view.setInt32(off, targetX, true); off += 4;
    view.setInt32(off, targetY, true); off += 4;
    this.send(buf);
  }

  // -----------------------------------------------------------------------
  // Snapshot & event decoding (server -> client)
  // -----------------------------------------------------------------------

  /**
   * Parse an incoming binary snapshot message.
   *
   * Wire format (all little-endian):
   *   Tick(uint32) PrevTick(uint32) UnitCount(uint16) EventCount(uint8)
   *   [UnitCount x unit deltas]
   *   [EventCount x events]
   */
  handleMessage(data) {
    const checkView = new DataView(data);

    // Server messages start with 0xFF 0xFE magic prefix; snapshots start with tick uint32
    if (checkView.byteLength >= 2 && checkView.getUint8(0) === 0xFF && checkView.getUint8(1) === 0xFE) {
      this.handleServerMessage(data);
      return;
    }

    // Map data starts with 0xFF 0xFD prefix — ignore (handled elsewhere or stale)
    if (checkView.byteLength >= 2 && checkView.getUint8(0) === 0xFF && checkView.getUint8(1) === 0xFD) {
      return;
    }

    // Snapshot must be at least the header size (4 tick + 4 prevTick + 2 unitCount
    // + 1 eventCount + 1 baseAlert = 12 bytes). Shorter buffers are malformed
    // (e.g. fragmented/truncated WS frames from a misbehaving upstream) and would
    // otherwise throw "Offset is outside the bounds of the DataView" at getUint32
    // below. See issue #33.
    if (checkView.byteLength < 12) {
      console.warn('snapshot too short:', checkView.byteLength);
      return;
    }

    // --- Snapshot handling (existing code) ---
    // Check for appended fog data (marker 0xFF 0xFD)
    let fogData = null;
    let snapshotEnd = data.byteLength;
    const scanView = new DataView(data);
    // Search backwards for fog marker 0xFF 0xFE 0xFD 0xFC
    for (let i = data.byteLength - 4; i >= 7; i--) {
      if (scanView.getUint8(i) === 0xFC && scanView.getUint8(i - 1) === 0xFD &&
          scanView.getUint8(i - 2) === 0xFE && scanView.getUint8(i - 3) === 0xFF) {
        const fogStart = i + 1;
        const fogW = scanView.getUint16(fogStart, true);
        const fogH = scanView.getUint16(fogStart + 2, true);
        const fogSize = fogW * fogH;
        if (fogStart + 4 + fogSize <= data.byteLength) {
          fogData = {
            width: fogW,
            height: fogH,
            visible: new Uint8Array(data.slice(fogStart + 4, fogStart + 4 + fogSize)),
          };
          snapshotEnd = i - 3; // exclude 4-byte marker
        }
        break;
      }
    }

    const view = new DataView(data, 0, snapshotEnd);
    let off = 0;

    // --- Snapshot header ---
    const tick = view.getUint32(off, true); off += 4;
    const prevTick = view.getUint32(off, true); off += 4;
    const unitCount = view.getUint16(off, true); off += 2;
    const eventCount = view.getUint8(off); off += 1;
    const baseAlert = view.getUint8(off); off += 1;

    // --- Unit deltas ---
    const units = [];
    for (let i = 0; i < unitCount && off < view.byteLength - 4; i++) {
      const entityID = view.getUint32(off, true); off += 4;
      const mask = view.getUint8(off); off += 1;

      const unit = { entityID, changedMask: mask };
      if (mask & CHANGED_POSITION) {
        unit.x = Number(view.getBigInt64(off, true)); off += 8;
        unit.y = Number(view.getBigInt64(off, true)); off += 8;
      }
      if (mask & CHANGED_VELOCITY) {
        unit.vx = Number(view.getBigInt64(off, true)); off += 8;
        unit.vy = Number(view.getBigInt64(off, true)); off += 8;
      }
      if (mask & CHANGED_ANGLE) {
        unit.angle = view.getInt16(off, true); off += 2;
      }
      if (mask & CHANGED_HP) {
        unit.hp = view.getInt32(off, true); off += 4;
      }
      if (mask & CHANGED_TARGET_ID) {
        unit.targetID = view.getUint32(off, true); off += 4;
      }
      if (mask & CHANGED_MORALE) {
        unit.morale = view.getInt32(off, true); off += 4;
      }
      if (mask & CHANGED_STATE) {
        unit.state = view.getUint8(off); off += 1;
      }
      if (mask & CHANGED_SQUAD_ID) {
        unit.squadID = view.getUint32(off, true); off += 4;
      }

      // New unit (mask 0xFF): UnitType + Team appended after all masked fields
      if (mask === 0xFF) {
        unit.unitType = view.getUint8(off); off += 1;
        unit.team = view.getUint8(off); off += 1;
      }

      units.push(unit);
    }

    // --- Events ---
    const events = [];
    for (let i = 0; i < eventCount && off < view.byteLength - 4; i++) {
      const eventType = view.getUint8(off); off += 1;
      const evt = { type: eventType };

      switch (eventType) {
        case EVENT_DAMAGE: {
          // targetID(uint32) + damage(int32) + sourceX(int32) + sourceY(int32) = 16
          evt.targetID = view.getUint32(off, true); off += 4;
          evt.damage = view.getInt32(off, true); off += 4;
          evt.sourceX = view.getInt32(off, true); off += 4;
          evt.sourceY = view.getInt32(off, true); off += 4;
          break;
        }
        case EVENT_DEATH: {
          // Issue #28 — enriched payload:
          //   entityID (uint32, 4B)
          //   X         (int64,   8B)  — fixed-point (FractionBits=12) position at death
          //   Y         (int64,   8B)  — fixed-point position at death
          //   tick      (uint32,  4B)  — simulation tick of death
          // Total: 24 bytes.  X/Y anchor the die animation at the exact
          // death tile; tick lets the client reconstruct when the unit
          // died even if the event is processed a few snapshots late.
          evt.entityID = view.getUint32(off, true); off += 4;
          evt.x = Number(view.getBigInt64(off, true)); off += 8;
          evt.y = Number(view.getBigInt64(off, true)); off += 8;
          evt.tick = view.getUint32(off, true); off += 4;
          break;
        }
        case EVENT_TERRAIN_CHANGE: {
          // tileX(int32) + tileY(int32) + newType(uint8) = 9
          evt.tileX = view.getInt32(off, true); off += 4;
          evt.tileY = view.getInt32(off, true); off += 4;
          evt.newType = view.getUint8(off); off += 1;
          break;
        }
        case EVENT_COMMANDER_DOWN: {
          // commanderID(uint32) = 4
          evt.commanderID = view.getUint32(off, true); off += 4;
          break;
        }
        case EVENT_PROJECTILE: {
          // Issue #48 — repurposed as an attack-fire event:
          //   entityID (uint32, 4B) — attacker that resolved a shot
          //   tick      (uint32, 4B) — simulation tick of the attack
          // Total: 8 bytes.  The client stamps its render clock on
          // receipt and plays the attacker's animation once.  (Was a
          // 36-byte projectile-pos payload, but the server never
          // spawned projectiles — see server/pkg/combat/projectile.go,
          // which is dormant.  Repurposed rather than adding a new
          // event type.)
          evt.entityID = view.getUint32(off, true); off += 4;
          evt.tick = view.getUint32(off, true); off += 4;
          break;
        }
        default:
          console.warn(`Unknown event type ${eventType} at offset ${off - 1}, stopping event decode`);
          // Cannot continue safely — remaining layout is unknown.
          i = eventCount; // break outer loop
          break;
      }

      events.push(evt);
    }

    if (this.onSnapshot) {
      this.onSnapshot({ tick, prevTick, units, events, fog: fogData, baseAlert });
    }
  }

  // -----------------------------------------------------------------------
  // Server messages (Gold, MatchResult, Roster)
  // -----------------------------------------------------------------------

  handleServerMessage(data) {
    const view = new DataView(data);
    // Skip 0xFF 0xFE magic prefix, then read type
    const type = view.getUint8(2);
    let off = 3;

    switch (type) {
      case 0x80: { // MsgGoldUpdate
        const gold = view.getInt32(off, true);
        if (this.onGoldUpdate) this.onGoldUpdate(gold);
        break;
      }
      case 0x81: { // MsgMatchResult
        const winner = view.getUint8(off); off += 1;
        const reasonLen = view.getUint16(off, true); off += 2;
        const reasonBytes = new Uint8Array(data, off, reasonLen);
        const reason = new TextDecoder().decode(reasonBytes);
        if (this.onMatchResult) this.onMatchResult({ winner, reason });
        break;
      }
      case 0x82: { // MsgRosterUpdate
        const dataLen = view.getUint16(off, true); off += 2;
        const rosterData = new Uint8Array(data, off, dataLen);
        if (this.onRosterUpdate) this.onRosterUpdate(rosterData);
        break;
      }
      case 0x83: { // MsgMatchStats (AAR)
        const stats = [null, null];
        for (let i = 0; i < 2; i++) {
          const kills = view.getUint16(off, true); off += 2;
          const deaths = view.getUint16(off, true); off += 2;
          const commanderKills = view.getUint16(off, true); off += 2;
          const unitsRecruited = view.getUint16(off, true); off += 2;
          const goldEarned = view.getInt32(off, true); off += 4;
          const goldSpent = view.getInt32(off, true); off += 4;
          stats[i] = { kills, deaths, commanderKills, unitsRecruited, goldEarned, goldSpent };
        }
        if (this.onMatchStats) this.onMatchStats(stats);
        break;
      }
      default:
        console.warn(`Unknown server message type 0x${type.toString(16)}`);
    }
  }

  // -----------------------------------------------------------------------
  // Heartbeat
  // -----------------------------------------------------------------------

  startHeartbeat() {
    this.lastPong = Date.now();
    this.pingInterval = setInterval(() => {
      if (this.ws && this.ws.readyState === WebSocket.OPEN) {
        // Browser WebSocket doesn't support .ping() — send text ping instead
        this.ws.send(JSON.stringify({ type: 'ping' }));
      }
    }, 15000);
  }

  stopHeartbeat() {
    clearInterval(this.pingInterval);
    this.pingInterval = null;
  }

  // -----------------------------------------------------------------------
  // Reconnection (exponential back-off)
  // -----------------------------------------------------------------------

  scheduleReconnect() {
    this.reconnectTimer = setTimeout(() => {
      this.connect();
    }, this.reconnectDelay);
    this.reconnectDelay = Math.min(this.reconnectDelay * 2, this.maxReconnectDelay);
  }
}
