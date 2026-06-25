// client/src/tactic_loadout.js
//
// Customizable tactic preset slots (Issue #43, per design/main.png).
//
// Each of the 4 slots can hold one tactic name:
//   'charge' | 'retreat' | 'defend' | 'rally' | 'attack-ground'
//
// Behavior:
//   - Click empty slot → opens #tactic-picker at slot position
//   - Click picker option → assigns tactic to that slot, persists to localStorage
//   - Click assigned slot → executes the bound tactic via game.handleTactic()
//   - Right-click assigned slot → clears it
//   - Persistence: localStorage key 'paper-war.tactic-loadout' holds JSON array
//
// The class is framework-agnostic: it manipulates DOM directly and
// takes a `game` reference for tactic execution.

const STORAGE_KEY = 'paper-war.tactic-loadout';
const VALID_TACTICS = new Set([
  'charge', 'retreat', 'defend', 'rally', 'attack-ground',
]);

// Short labels for assigned slot display
const TACTIC_LABELS = {
  'charge':       'Chg',
  'retreat':      'Rtrt',
  'defend':       'Def',
  'rally':        'Raly',
  'attack-ground': 'Atk',
};

export class TacticLoadout {
  /**
   * @param {object} game  Game instance with handleTactic(tacticName) method.
   *                       Used to execute the bound tactic on click.
   */
  constructor(game) {
    this.game = game;
    this.slots = this._loadSaved();
    this._pickerSlot = null;  // which slot is the picker open for

    this._pickerEl = document.getElementById('tactic-picker');
    this._slotEls = [...document.querySelectorAll('.tactic-slot[data-slot]')];

    // Defensive: if elements are missing (e.g. lobby screen), no-op.
    if (!this._pickerEl || this._slotEls.length === 0) return;

    this._bindEvents();
    this._render();
  }

  // -- Persistence --------------------------------------------------------

  _loadSaved() {
    try {
      const raw = localStorage.getItem(STORAGE_KEY);
      if (!raw) return [null, null, null, null];
      const parsed = JSON.parse(raw);
      if (!Array.isArray(parsed) || parsed.length !== 4) return [null, null, null, null];
      // Sanitize: each entry must be a known tactic name or null
      return parsed.map(t => (typeof t === 'string' && VALID_TACTICS.has(t)) ? t : null);
    } catch {
      return [null, null, null, null];
    }
  }

  _save() {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(this.slots));
    } catch {
      // localStorage may be unavailable (private mode, sandbox) — fail silently.
    }
  }

  // -- Events -------------------------------------------------------------

  _bindEvents() {
    for (const el of this._slotEls) {
      const slotIdx = parseInt(el.dataset.slot, 10);

      el.addEventListener('click', (e) => {
        e.preventDefault();
        e.stopPropagation();
        const tactic = this.slots[slotIdx];
        if (tactic) {
          // Assigned slot → execute
          this._executeTactic(tactic);
        } else {
          // Empty slot → open picker
          this._openPicker(slotIdx, el);
        }
      });

      // Right-click clears assigned slot
      el.addEventListener('contextmenu', (e) => {
        if (this.slots[slotIdx]) {
          e.preventDefault();
          this.slots[slotIdx] = null;
          this._save();
          this._render();
        }
      });
    }

    // Picker option clicks
    const options = this._pickerEl.querySelectorAll('.tactic-picker-option');
    for (const opt of options) {
      opt.addEventListener('click', (e) => {
        e.stopPropagation();
        const tactic = opt.dataset.tactic;
        if (this._pickerSlot !== null && VALID_TACTICS.has(tactic)) {
          this.slots[this._pickerSlot] = tactic;
          this._save();
          this._render();
        }
        this._closePicker();
      });
    }

    // Cancel button
    const cancel = this._pickerEl.querySelector('.tactic-picker-cancel');
    cancel?.addEventListener('click', (e) => {
      e.stopPropagation();
      this._closePicker();
    });

    // Click outside picker closes it
    document.addEventListener('click', (e) => {
      if (!this._pickerEl.hidden && !this._pickerEl.contains(e.target)) {
        this._closePicker();
      }
    });

    // Esc closes picker
    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape' && !this._pickerEl.hidden) {
        this._closePicker();
      }
    });
  }

  // -- Picker -------------------------------------------------------------

  _openPicker(slotIdx, anchorEl) {
    this._pickerSlot = slotIdx;
    const rect = anchorEl.getBoundingClientRect();
    // Position below the slot, slightly left-aligned
    this._pickerEl.style.left = Math.max(4, rect.left - 40) + 'px';
    this._pickerEl.style.top = (rect.bottom + 4) + 'px';
    this._pickerEl.hidden = false;
  }

  _closePicker() {
    this._pickerEl.hidden = true;
    this._pickerSlot = null;
  }

  // -- Render -------------------------------------------------------------

  _render() {
    for (const el of this._slotEls) {
      const slotIdx = parseInt(el.dataset.slot, 10);
      const tactic = this.slots[slotIdx];
      if (tactic) {
        el.classList.remove('empty');
        const label = TACTIC_LABELS[tactic] || tactic;
        el.innerHTML = `<span class="tactic-slot-icon">${label}</span>`;
        el.title = `${tactic} — click to execute, right-click to clear`;
      } else {
        el.classList.add('empty');
        el.innerHTML = '<span class="tactic-slot-icon">+</span>';
        el.title = 'Click to assign tactic, right-click to clear';
      }
    }
  }

  // -- Execution ----------------------------------------------------------

  _executeTactic(tactic) {
    if (!this.game) return;
    if (tactic === 'attack-ground') {
      // Toggle attack-ground mode (different code path than named tactics)
      if (typeof this.game.toggleAttackGround === 'function') {
        this.game.toggleAttackGround();
      }
    } else if (typeof this.game.handleTactic === 'function') {
      this.game.handleTactic(tactic);
    }
  }

  // -- API for testing ----------------------------------------------------

  getSlot(idx) { return this.slots[idx]; }
  setSlot(idx, tactic) {
    if (idx < 0 || idx >= 4) throw new Error('slot out of range');
    if (tactic !== null && !VALID_TACTICS.has(tactic)) throw new Error('invalid tactic');
    this.slots[idx] = tactic;
    this._save();
    this._render();
  }
}
