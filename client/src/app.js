// client/src/app.js — Screen flow controller: Login -> Lobby -> Game.
// Manages UI screens, server communication for login/matchmaking,
// and delegates to Game when a match is found.

import { Connection } from './connection.js';
import { Game } from './main.js';

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

    // Solo game button
    this.soloBtn.addEventListener('click', () => {
      this.lobbyStatus.textContent = 'Starting game...';
      this.soloBtn.disabled = true;
      this.findMatchBtn.disabled = true;
      this.connection.sendJSON({ type: 'start_solo' });
    });

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
        break;
      case 'game':
        this.gameScreen.classList.add('active');
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
