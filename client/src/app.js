// client/src/app.js — Screen flow controller: Login -> Lobby -> Game.
// Manages UI screens, server communication for login/matchmaking,
// and delegates to Game when a match is found.

import { Connection } from './connection.js?v=v8';
import { Game } from './main.js?v=v8';

const LAST_USERNAME_KEY = 'paper-war:last-username';
// v1.1: opaque player token persisted across sessions. Generated on first
// login via crypto.getRandomValues, used by the server to resolve a real
// DB playerID and accumulate career stats. Stored in localStorage so the
// same browser = same player account. No real auth — anyone with the
// token can act as that player. Acceptable for v1.x (private game).
const PLAYER_TOKEN_KEY = 'paper-war:player-token';

function loadOrCreatePlayerToken() {
  try {
    let tok = window.localStorage.getItem(PLAYER_TOKEN_KEY);
    if (tok) return tok;
    // Generate 16 bytes hex = 32 chars. Same shape as server-side
    // MatchRegistry tokens.
    const buf = new Uint8Array(16);
    crypto.getRandomValues(buf);
    tok = Array.from(buf, b => b.toString(16).padStart(2, '0')).join('');
    window.localStorage.setItem(PLAYER_TOKEN_KEY, tok);
    return tok;
  } catch (_) {
    // localStorage unavailable (private mode, sandbox). Return empty —
    // server treats empty token as ephemeral (no career stats).
    return '';
  }
}

export class App {
  constructor() {
    this.username = this.loadLastUsername();
    this.connection = new Connection();
    this.game = null; // created when match starts

    // Screen elements
    this.loginScreen = document.getElementById('login-screen');
    this.lobbyScreen = document.getElementById('lobby-screen');
    this.gameScreen = document.getElementById('game-screen');
    this.clashScreen = document.getElementById('clash-screen');
    this.careerScreen = document.getElementById('career-screen');
    this.leaderboardScreen = document.getElementById('leaderboard-screen');

    // Login elements
    this.loginForm = document.getElementById('login-form');
    this.loginInput = document.getElementById('login-username');
    if (this.loginInput && this.username) {
      this.loginInput.value = this.username;
    }

    // Lobby elements
    this.lobbyPlayerName = document.getElementById('lobby-player-name');
    this.soloBtn = document.getElementById('solo-btn');
    this.findMatchBtn = document.getElementById('find-match-btn');
    this.cancelQueueBtn = document.getElementById('cancel-queue-btn');
    this.lobbyStatus = document.getElementById('lobby-status');
    this.lobbySpinner = document.getElementById('lobby-spinner');

    // Roster data (from server roster_update messages)
    this.roster = null;

    this.wireUI();
  }

  // -----------------------------------------------------------------------
  // UI event wiring
  // -----------------------------------------------------------------------

  wireUI() {
    // Login form submission
    this.loginForm.addEventListener('submit', (e) => {
      e.preventDefault();
      const name = this.loginInput.value.trim();
      if (!name) return;
      this.username = name;
      this.saveLastUsername(name);
      this.handleLogin(name);
    });

    // Solo / Start Match button
    this.soloBtn.addEventListener('click', () => {
      this.lobbyStatus.textContent = 'Starting game...';
      this.soloBtn.disabled = true;
      this.findMatchBtn.disabled = true;
      this.connection.sendJSON({
        type: 'start_solo',
        commander_type: this.selectedCmdType,
      });
    });

    // Clash Test — config screen + AI vs AI spectator mode
    this.clashBtn = document.getElementById('clash-btn');
    this.careerBtn = document.getElementById('career-btn');
    this.leaderboardBtn = document.getElementById('leaderboard-btn');
    if (this.careerBtn) {
      this.careerBtn.addEventListener('click', () => {
        this.showCareerScreen();
      });
    }
    if (this.leaderboardBtn) {
      this.leaderboardBtn.addEventListener('click', () => {
        this.showLeaderboardScreen();
      });
    }
    // Leaderboard refresh button.
    const lbRefresh = document.getElementById('leaderboard-refresh-btn');
    if (lbRefresh) {
      lbRefresh.addEventListener('click', () => {
        this.refreshLeaderboard();
      });
    }
    // Back button on leaderboard screen → lobby.
    const lbBack = document.getElementById('leaderboard-back-btn');
    if (lbBack) {
      lbBack.addEventListener('click', () => {
        this.showScreen('lobby');
      });
    }
    // Back button on career screen → lobby.
    const careerBack = document.getElementById('career-back-btn');
    if (careerBack) {
      careerBack.addEventListener('click', () => {
        this.showScreen('lobby');
      });
    }
    this.clashTeam1Size = 5;
    this.clashTeam2Size = 5;
    this.clashTeam1Cmd = 0;
    this.clashTeam2Cmd = 0;
    this.clashTerrain = 'random';
    if (this.clashBtn) {
      // "Clash Test" in lobby -> show config screen
      this.clashBtn.addEventListener('click', () => {
        this.showScreen('clash');
      });

      // Wire clash config size buttons
      document.querySelectorAll('.clash-size-btn').forEach(btn => {
        btn.addEventListener('click', () => {
          const team = parseInt(btn.dataset.team);
          const size = parseInt(btn.dataset.size);
          document.querySelectorAll(`.clash-size-btn[data-team="${team}"]`).forEach(b => b.classList.remove('selected'));
          btn.classList.add('selected');
          if (team === 1) this.clashTeam1Size = size;
          else this.clashTeam2Size = size;
        });
      });

      // Wire clash config commander buttons
      document.querySelectorAll('.clash-cmd-btn').forEach(btn => {
        btn.addEventListener('click', () => {
          const team = parseInt(btn.dataset.team);
          const cmdType = parseInt(btn.dataset.cmdType);
          document.querySelectorAll(`.clash-cmd-btn[data-team="${team}"]`).forEach(b => b.classList.remove('selected'));
          btn.classList.add('selected');
          if (team === 1) this.clashTeam1Cmd = cmdType;
          else this.clashTeam2Cmd = cmdType;
        });
      });

      // Wire terrain preset buttons
      document.querySelectorAll('.clash-terrain-btn').forEach(btn => {
        btn.addEventListener('click', () => {
          document.querySelectorAll('.clash-terrain-btn').forEach(b => b.classList.remove('selected'));
          btn.classList.add('selected');
          this.clashTerrain = btn.dataset.terrain;
        });
      });

      // Back button -> return to lobby
      document.getElementById('clash-back-btn').addEventListener('click', () => {
        this.showScreen('lobby');
      });

      // Start Battle button -> send config and start match
      document.getElementById('clash-start-btn').addEventListener('click', () => {
        this.lobbyStatus.textContent = 'Starting clash test...';
        this.connection.sendJSON({
          type: 'start_clash',
          team1_units: this.clashTeam1Size,
          team2_units: this.clashTeam2Size,
          team1_commander: this.clashTeam1Cmd,
          team2_commander: this.clashTeam2Cmd,
          terrain: this.clashTerrain,
        });
      });
    }

    // Find match button
    this.findMatchBtn.addEventListener('click', () => {
      this.connection.sendJSON({ type: 'join_queue' });
      this.findMatchBtn.style.display = 'none';
      this.cancelQueueBtn.style.display = 'block';
      this.lobbyStatus.textContent = 'Searching for opponents...';
      this.lobbySpinner.style.display = 'flex';
    });

    // Cancel queue button
    this.cancelQueueBtn.addEventListener('click', () => {
      this.connection.sendJSON({ type: 'leave_queue' });
      this.findMatchBtn.style.display = 'block';
      this.cancelQueueBtn.style.display = 'none';
      this.lobbyStatus.textContent = 'Ready for battle';
      this.lobbySpinner.style.display = 'none';
    });

    // Commander type selection
    this.selectedCmdType = 0; // default: LI (Gun)
    document.querySelectorAll('.cmd-type-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        document.querySelectorAll('.cmd-type-btn').forEach(b => b.classList.remove('selected'));
        btn.classList.add('selected');
        this.selectedCmdType = parseInt(btn.dataset.cmdType, 10);
        this.updateRosterDisplay();
      });
    });
  }

  // -----------------------------------------------------------------------
  // Login flow
  // -----------------------------------------------------------------------

  loadLastUsername() {
    try {
      return window.localStorage.getItem(LAST_USERNAME_KEY) || '';
    } catch (_) {
      return '';
    }
  }

  saveLastUsername(name) {
    try {
      window.localStorage.setItem(LAST_USERNAME_KEY, name);
    } catch (_) {
      // Ignore storage failures; login should still work in private contexts.
    }
  }

  handleLogin(name) {
    this.username = name;
    // Connect WebSocket — save callbacks so they can be restored after a
    // Game instance overrides them (Game sets its own onConnect/onDisconnect
    // on the shared connection object).
    this._loginOnConnect = () => {
      this.connection.sendJSON({
        type: 'login',
        name: this.username,
        token: loadOrCreatePlayerToken(),
      });
    };
    this._loginOnDisconnect = () => {
      // Mid-match disconnect with a valid token — stay on game screen and
      // let the reconnect overlay do its job. Only clean up if there's no
      // pending reconnect (true disconnect / no match / clash spectator).
      if (this.connection.reconnectToken) return;
      // No reconnect token — clean up any active game and return to lobby.
      // The connection layer will auto-reconnect; when it does, onConnect
      // (restored by cleanupGame) re-sends login so the server recognises us.
      this.cleanupGame();
      this.lobbyStatus.textContent = 'Connection lost — match no longer available.';
    };

    this.connection.onConnect = this._loginOnConnect;
    this.connection.onDisconnect = this._loginOnDisconnect;

    // Mid-match disconnect — show reconnect overlay instead of booting to login.
    // The connection layer auto-retries; the overlay is dismissed on reconnect_ok.
    this.connection.onReconnecting = () => {
      if (this.game) this.game.showReconnectOverlay();
    };

    // Handle text messages from server
    this.connection.onTextMessage = (msg) => {
      this.handleServerMessage(msg);
    };

    // Override snapshot handler — only used during game
    this.connection.onSnapshot = null;

    this.connection.connect();
  }

  // -----------------------------------------------------------------------
  // Server message handling
  // -----------------------------------------------------------------------

  handleServerMessage(msg) {
    switch (msg.type) {
      case 'login_ok':
        // Show lobby
        this.lobbyPlayerName.textContent = this.username;
        this.showScreen('lobby');
        break;

      case 'queue_joined':
        this.lobbyStatus.textContent = `Searching... ${msg.count}/2 players`;
        break;

      case 'queue_left':
        this.findMatchBtn.style.display = 'block';
        this.cancelQueueBtn.style.display = 'none';
        this.lobbyStatus.textContent = 'Ready for battle';
        this.lobbySpinner.style.display = 'none';
        break;

      case 'match_found':
        this.startGame(msg);
        break;

      case 'stronghold_state':
        // Live stronghold entities (positions/level/faction/HP/garrison) for
        // rendering. Sent on match start and whenever state changes (#54).
        // New fields (hp/max_hp/garrison) default to 0 for older senders;
        // the client falls back to its strongholdCapacity helper.
        if (this.game) this.game.strongholds = msg.strongholds || [];
        break;

      case 'reconnect_ok':
        // Server validated our token and re-bound our playerID. The map data
        // binary message follows immediately after this text message.
        if (this.game) {
          this.game.playerID = msg.player_id;
          // Spectator mode (playerID 0 = clash/crash test): hide recruit/build/gold UI.
          document.body.classList.toggle('spectator-mode', msg.player_id === 0);
          this.game.mapWidth = msg.map_w || this.game.mapWidth;
          this.game.mapHeight = msg.map_h || this.game.mapHeight;
          // Clear stale entity state — the next snapshot will repopulate.
          if (this.game.state) this.game.state.clearEntities();
          this.game.hideReconnectOverlay();
          // Update token in case server refreshed it
          if (msg.reconnect_token) {
            this.connection.reconnectToken = msg.reconnect_token;
          }
        }
        break;

      case 'reconnect_failed':
        // Token invalid/expired or match ended — give up and return to lobby.
        this.connection.reconnectToken = null;
        if (this.game) this.game.hideReconnectOverlay();
        this.showReconnectFailed(msg.reason || 'unknown');
        // Return to lobby after short delay
        setTimeout(() => {
          this.cleanupGame();
          // The server doesn't know who we are on this socket — we only sent
          // a reconnect attempt, not a login. Re-send login so the server
          // associates this connection with our name and accepts start_solo /
          // start_clash / join_queue messages.
          this.connection.sendJSON({
            type: 'login',
            name: this.username,
            token: loadOrCreatePlayerToken(),
          });
          this.lobbyStatus.textContent = 'Reconnect failed — match no longer available.';
        }, 3000);
        break;

      case 'roster_update':
        this.roster = msg.roster;
        this.updateRosterDisplay();
        break;

      case 'career_stats':
        // v1.1: cumulative cross-match totals. May arrive at two times:
        // (1) right after login_ok (initial state, possibly all zeros),
        // (2) right after match end (updated totals post-AAR).
        this.careerStats = {
          matches_played:    msg.matches_played    || 0,
          matches_won:       msg.matches_won       || 0,
          matches_lost:      msg.matches_lost      || 0,
          total_kills:       msg.total_kills       || 0,
          total_deaths:      msg.total_deaths      || 0,
          commander_kills:   msg.commander_kills   || 0,
          commanders_lost:   msg.commanders_lost   || 0,
          total_gold_earned: msg.total_gold_earned || 0,
          total_gold_spent:  msg.total_gold_spent  || 0,
          total_recruits:    msg.total_recruits    || 0,
        };
        this.updateCareerDisplay();
        break;

      case 'leaderboard':
        // v1.2: leaderboard response. entries is an array of
        // { rank, player_id, name, matches_played, matches_won, matches_lost,
        //   total_kills, total_deaths } sorted by total_kills desc.
        // May include an "error" field if the store is unavailable.
        this.leaderboard = msg.entries || [];
        this.leaderboardError = msg.error || null;
        this.updateLeaderboardDisplay();
        break;
    }
  }

  // -----------------------------------------------------------------------
  // Career display
  // -----------------------------------------------------------------------

  updateCareerDisplay() {
    if (!this.careerStats) return;
    const cs = this.careerStats;
    // Update lobby career summary if present.
    const summary = document.getElementById('lobby-career-summary');
    if (summary) {
      const winRate = cs.matches_played > 0
        ? ((cs.matches_won / cs.matches_played) * 100).toFixed(0)
        : '—';
      const kd = cs.total_deaths > 0
        ? (cs.total_kills / cs.total_deaths).toFixed(2)
        : cs.total_kills.toFixed(0);
      summary.textContent =
        `${cs.matches_won}W–${cs.matches_lost}L (${winRate}%) · ` +
        `${cs.total_kills} kills / ${cs.total_deaths} deaths (K/D ${kd}) · ` +
        `${cs.commander_kills} cmd kills · ${cs.commanders_lost} cmd lost`;
    }
    // Update dedicated career screen if present.
    const screen = document.getElementById('career-screen');
    if (!screen || !screen.classList.contains('active')) return;
    const set = (id, val) => {
      const el = document.getElementById(id);
      if (el) el.textContent = val;
    };
    set('career-matches', cs.matches_played);
    set('career-wins', cs.matches_won);
    set('career-losses', cs.matches_lost);
    set('career-kills', cs.total_kills);
    set('career-deaths', cs.total_deaths);
    set('career-cmd-kills', cs.commander_kills);
    set('career-cmd-lost', cs.commanders_lost);
    set('career-gold-earned', cs.total_gold_earned);
    set('career-gold-spent', cs.total_gold_spent);
    set('career-recruits', cs.total_recruits);
  }

  showCareerScreen() {
    this.showScreen('career');
    this.updateCareerDisplay();
  }

  // -----------------------------------------------------------------------
  // Leaderboard display (v1.2)
  // -----------------------------------------------------------------------

  refreshLeaderboard() {
    // Ask the server for the current top-N. Response arrives as
    // 'leaderboard' message → updateLeaderboardDisplay().
    this.connection.sendJSON({ type: 'get_leaderboard' });
  }

  updateLeaderboardDisplay() {
    const tbody = document.getElementById('leaderboard-body');
    if (!tbody) return;
    // Clear any existing rows.
    tbody.innerHTML = '';

    if (this.leaderboardError) {
      const row = document.createElement('tr');
      const cell = document.createElement('td');
      cell.colSpan = 6;
      cell.textContent = 'Leaderboard unavailable: ' + this.leaderboardError;
      cell.style.color = '#EF5350';
      row.appendChild(cell);
      tbody.appendChild(row);
      return;
    }

    if (!this.leaderboard || this.leaderboard.length === 0) {
      const row = document.createElement('tr');
      const cell = document.createElement('td');
      cell.colSpan = 6;
      cell.textContent = 'No ranked players yet. Play a match to claim rank #1.';
      cell.style.color = 'var(--text-muted)';
      cell.style.textAlign = 'center';
      cell.style.padding = '20px';
      row.appendChild(cell);
      tbody.appendChild(row);
      return;
    }

    // Render one row per entry. Highlight the current player's row.
    const myPid = window.__paperWarGame?.playerID || 0;
    // Server uses match-local playerID for snapshots; the leaderboard uses
    // DB playerID. To highlight self we'd need the DB playerID. For v1.2
    // we use name match as a heuristic (login names are unique per session).
    const myName = this.username || '';
    for (const e of this.leaderboard) {
      const tr = document.createElement('tr');
      if (e.name && e.name === myName) {
        tr.classList.add('leaderboard-self');
      }
      const cells = [
        e.rank,
        e.name || '(unnamed)',
        `${e.matches_won}-${e.matches_lost}`,
        e.matches_played,
        e.total_kills,
        e.total_deaths,
      ];
      for (const val of cells) {
        const td = document.createElement('td');
        td.textContent = val;
        tr.appendChild(td);
      }
      tbody.appendChild(tr);
    }
  }

  showLeaderboardScreen() {
    this.showScreen('leaderboard');
    // Always refresh on entry so the view is fresh.
    this.refreshLeaderboard();
  }

  // -----------------------------------------------------------------------
  // Roster display
  // -----------------------------------------------------------------------

  updateRosterDisplay() {
    const cmdNames = ['Light Infantry (Gun)', 'Heavy Infantry (Cannon)', 'Sniper', 'AAI (Missile)'];
    const cmdNameEl = document.getElementById('roster-cmd-name');
    if (cmdNameEl) {
      cmdNameEl.textContent = cmdNames[this.selectedCmdType] || cmdNames[0];
    }

    const cmdLevelEl = document.getElementById('roster-cmd-level');
    if (cmdLevelEl && this.roster && this.roster.cmd_level) {
      cmdLevelEl.textContent = 'Lv ' + this.roster.cmd_level;
    } else if (cmdLevelEl) {
      cmdLevelEl.textContent = 'Lv 1';
    }

    // Update unit list if roster data available
    const unitsEl = document.getElementById('roster-units');
    if (unitsEl && this.roster && this.roster.units) {
      const unitNames = ['Light Infantry', 'Heavy Infantry', 'Sniper', 'AAI', 'MG Team', 'Mobile Artillery', 'Missile Launcher'];
      const unitColors = ['#8BC34A', '#42A5F5', '#00BCD4', '#FF9800', '#AB47BC', '#EF5350', '#E040FB'];
      unitsEl.innerHTML = '';
      for (const entry of this.roster.units) {
        const row = document.createElement('div');
        row.className = 'roster-unit-row';
        const icon = document.createElement('span');
        icon.className = 'roster-unit-icon';
        icon.style.color = unitColors[entry.type] || '#fff';
        icon.textContent = '\u25A0'; // ■
        const label = document.createElement('span');
        label.textContent = `${unitNames[entry.type] || 'Unknown'} x${entry.count}`;
        row.appendChild(icon);
        row.appendChild(label);
        unitsEl.appendChild(row);
      }
    }
  }

  // -----------------------------------------------------------------------
  // Game cleanup — called when a match ends, reconnect fails, or the
  // connection drops without a reconnect token (clash spectator mode).
  // Restores the connection callbacks that the Game instance overrode,
  // stops the game loop, and returns the player to the lobby.
  // -----------------------------------------------------------------------

  cleanupGame() {
    if (this.game) {
      // Stop the game loop and audio WITHOUT calling game.stop(), which
      // would call connection.disconnect() and kill the WebSocket.
      this.game.running = false;
      if (this.game.ambient) this.game.ambient.stop();
      this.game = null;
    }
    // Restore login callbacks — the Game instance replaced these with its
    // own handlers that don't send login. Without restoring, the next
    // WS reconnect won't send login and the server won't know who we are.
    if (this._loginOnConnect) this.connection.onConnect = this._loginOnConnect;
    if (this._loginOnDisconnect) this.connection.onDisconnect = this._loginOnDisconnect;
    // Re-enable lobby buttons
    this.soloBtn.disabled = false;
    this.findMatchBtn.disabled = false;
    if (this.clashBtn) this.clashBtn.disabled = false;
    this.lobbySpinner.style.display = 'none';
    // Clear spectator mode so recruit/build/gold UI shows again in solo/PvP.
    document.body.classList.remove('spectator-mode');
    this.showScreen('lobby');
  }

  // -----------------------------------------------------------------------
  // Screen management
  // -----------------------------------------------------------------------

  showScreen(name) {
    this.loginScreen.classList.remove('active');
    this.lobbyScreen.classList.remove('active');
    this.gameScreen.classList.remove('active');
    if (this.clashScreen) this.clashScreen.classList.remove('active');
    if (this.careerScreen) this.careerScreen.classList.remove('active');
    if (this.leaderboardScreen) this.leaderboardScreen.classList.remove('active');

    switch (name) {
      case 'login':
        this.loginScreen.classList.add('active');
        break;
      case 'lobby':
        this.lobbyScreen.classList.add('active');
        break;
      case 'clash':
        if (this.clashScreen) this.clashScreen.classList.add('active');
        break;
      case 'career':
        if (this.careerScreen) this.careerScreen.classList.add('active');
        break;
      case 'leaderboard':
        if (this.leaderboardScreen) this.leaderboardScreen.classList.add('active');
        break;
      case 'game':
        this.gameScreen.classList.add('active');
        break;
    }
  }

  // -----------------------------------------------------------------------
  // Reconnect failure notice — briefly shown before returning to lobby
  // -----------------------------------------------------------------------

  showReconnectFailed(reason) {
    const existing = document.getElementById('reconnect-failed-toast');
    if (existing) existing.remove();
    const toast = document.createElement('div');
    toast.id = 'reconnect-failed-toast';
    toast.style.cssText = [
      'position:fixed', 'top:50%', 'left:50%', 'transform:translate(-50%,-50%)',
      'background:#b3261e', 'color:#fff', 'padding:20px 32px', 'border-radius:8px',
      'font-family:sans-serif', 'font-size:16px', 'z-index:3000',
      'box-shadow:0 4px 20px rgba(0,0,0,0.5)', 'text-align:center',
    ].join(';');
    toast.textContent = `Reconnect failed (${reason}). Returning to lobby…`;
    document.body.appendChild(toast);
    setTimeout(() => toast.remove(), 3000);
  }

  // -----------------------------------------------------------------------
  // Game start
  // -----------------------------------------------------------------------

  startGame(matchInfo) {
    // Update player name in top bar
    const nameEl = document.getElementById('player-name');
    if (nameEl) nameEl.textContent = this.username;
    const avatarEl = document.getElementById('player-avatar');
    if (avatarEl) avatarEl.textContent = this.username.charAt(0).toUpperCase();

    // Show game screen
    this.showScreen('game');

    // Create Game with the existing connection
    this.game = new Game(this.connection);
    window.__paperWarGame = this.game;
    this.game.playerID = matchInfo.player_id;
    // Spectator mode (playerID 0 = clash/crash test): hide recruit/build/gold UI.
    document.body.classList.toggle('spectator-mode', matchInfo.player_id === 0);
    this.game.mapWidth = matchInfo.map_w || 48;
    this.game.mapHeight = matchInfo.map_h || 96;

    // Store server-provided spawn positions (used for build placement,
    // minimap flags, and camera centering).  Falls back to hardcoded
    // positions if the server didn't include them.
    this.game.serverSpawns = matchInfo.spawns || null;

    // Store reconnect token so connection.js can auto-rejoin on disconnect
    this.connection.reconnectToken = matchInfo.reconnect_token || null;

    // Set up map data handler
    this.connection.onMapData = (terrainData) => {
      this.game.setMapTerrain(terrainData);
    };

    // Set up creep overlay handler (Phase 4). Raw w*h bytes of CreepOwner
    // (0/1/2). Game stores it for the terrain render tint pass.
    this.connection.onCreepData = (creepData) => {
      this.game.setCreepData(creepData);
    };

    this.game.start();
  }
}

// ---------------------------------------------------------------------------
// Bootstrap
// ---------------------------------------------------------------------------

const app = new App();
window.__paperWarApp = app;
