// client/src/app.js — Screen flow controller: Login -> Lobby -> Game.
// Manages UI screens, server communication for login/matchmaking,
// and delegates to Game when a match is found.

import { Connection } from './connection.js?v=v1';
import { Game } from './main.js?v=v1';

const LAST_USERNAME_KEY = 'paper-war:last-username';

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
    // Connect WebSocket
    this.connection.onConnect = () => {
      // Send login message
      this.connection.sendJSON({ type: 'login', name: name });
    };

    this.connection.onDisconnect = () => {
      // Mid-match disconnect with a valid token — stay on game screen and
      // let the reconnect overlay do its job. Only boot to login if there's
      // no pending reconnect (true disconnect / no match in progress).
      if (this.connection.reconnectToken) return;
      this.showScreen('login');
    };

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

      case 'reconnect_ok':
        // Server validated our token and re-bound our playerID. The map data
        // binary message follows immediately after this text message.
        if (this.game) {
          this.game.playerID = msg.player_id;
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
          if (this.game) {
            this.game.stop();
            this.game = null;
          }
          this.showScreen('lobby');
          this.lobbyStatus.textContent = 'Reconnect failed — match no longer available.';
        }, 3000);
        break;

      case 'roster_update':
        this.roster = msg.roster;
        this.updateRosterDisplay();
        break;
    }
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
  // Screen management
  // -----------------------------------------------------------------------

  showScreen(name) {
    this.loginScreen.classList.remove('active');
    this.lobbyScreen.classList.remove('active');
    this.gameScreen.classList.remove('active');
    if (this.clashScreen) this.clashScreen.classList.remove('active');

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
    this.game.mapWidth = matchInfo.map_w || 48;
    this.game.mapHeight = matchInfo.map_h || 96;

    // Store reconnect token so connection.js can auto-rejoin on disconnect
    this.connection.reconnectToken = matchInfo.reconnect_token || null;

    // Set up map data handler
    this.connection.onMapData = (terrainData) => {
      this.game.setMapTerrain(terrainData);
    };

    this.game.start();
  }
}

// ---------------------------------------------------------------------------
// Bootstrap
// ---------------------------------------------------------------------------

const app = new App();
window.__paperWarApp = app;
