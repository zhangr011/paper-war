// client/src/app.js — Screen flow controller: Login -> Lobby -> Game.
// Manages UI screens, server communication for login/matchmaking,
// and delegates to Game when a match is found.

import { Connection } from './connection.js?v=fix-view-2';
import { Game } from './main.js?v=fix-view-2';

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

    // Clash Test button — AI vs AI spectator mode
    this.clashBtn = document.getElementById('clash-btn');
    this.clashConfig = document.getElementById('clash-config');
    this.clashTeam1Size = 5;
    this.clashTeam2Size = 10;
    this.clashTerrain = 'random';
    if (this.clashBtn) {
      // Wire clash config size buttons
      document.querySelectorAll('.clash-size-btn').forEach(btn => {
        btn.addEventListener('click', () => {
          const team = parseInt(btn.dataset.team);
          const size = parseInt(btn.dataset.size);
          // Toggle selected within same team row
          document.querySelectorAll(`.clash-size-btn[data-team="${team}"]`).forEach(b => b.classList.remove('selected'));
          btn.classList.add('selected');
          if (team === 1) this.clashTeam1Size = size;
          else this.clashTeam2Size = size;
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

      this.clashBtn.addEventListener('click', () => {
        this.lobbyStatus.textContent = 'Starting clash test...';
        this.soloBtn.disabled = true;
        this.clashBtn.disabled = true;
        this.findMatchBtn.disabled = true;
        this.connection.sendJSON({
          type: 'start_clash',
          team1_units: this.clashTeam1Size,
          team2_units: this.clashTeam2Size,
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
      // Go back to login screen
      this.showScreen('login');
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

    switch (name) {
      case 'login':
        this.loginScreen.classList.add('active');
        break;
      case 'lobby':
        this.lobbyScreen.classList.add('active');
        if (this.clashConfig) this.clashConfig.style.display = 'block';
        break;
      case 'game':
        this.gameScreen.classList.add('active');
        if (this.clashConfig) this.clashConfig.style.display = 'none';
        break;
    }
  }

  // -----------------------------------------------------------------------
  // Game start
  // -----------------------------------------------------------------------

  startGame(matchInfo) {
    console.log('Starting game:', matchInfo);
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
