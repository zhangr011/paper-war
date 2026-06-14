// audio/sfx.js
// Synthesized sound effects using oscillators, noise, and envelopes.
// All sounds route through the AudioEngine's sfxGain bus.

import { EVENT_DAMAGE, EVENT_DEATH, EVENT_COMMANDER_DOWN, EVENT_PROJECTILE }
  from '../connection.js?v=v6';

// CombatUnitType constants (must match server component/unit_type.go)
const UNIT_LI = 0;     // Light Infantry — rifle
const UNIT_HI = 1;     // Heavy Infantry — rifle (heavier)
const UNIT_SNIPER = 2; // Sniper — precision shot
const UNIT_AAI = 3;    // Anti-Armor Infantry — rocket
const UNIT_MG = 4;     // Machine Gun — burst fire
const UNIT_MA = 5;     // Motor Artillery — mortar shell
const UNIT_MM = 6;     // Motor Missile — missile

/**
 * SFX synthesizer. All methods are no-ops if the engine isn't initialized.
 */
export class SFX {
  constructor(engine) {
    this.engine = engine;
    // Track recently played sounds per type for throttling
    this._lastPlayed = {}; // type → timestamp
  }

  // --- Low-level synth helpers ---

  _env(gainNode, peak, attack, decay, startTime) {
    const g = gainNode.gain;
    g.setValueAtTime(0, startTime);
    g.linearRampToValueAtTime(peak, startTime + attack);
    g.exponentialRampToValueAtTime(0.001, startTime + attack + decay);
  }

  _osc(type, freq, startTime, duration) {
    const osc = this.engine.ctx.createOscillator();
    osc.type = type;
    osc.frequency.setValueAtTime(freq, startTime);
    osc.start(startTime);
    osc.stop(startTime + duration);
    return osc;
  }

  _noiseFilter(filterType, freq, q, startTime, duration) {
    const src = this.engine.noiseSource();
    const filter = this.engine.ctx.createBiquadFilter();
    filter.type = filterType;
    filter.frequency.setValueAtTime(freq, startTime);
    filter.Q.value = q || 1;
    src.connect(filter);
    src.start(startTime);
    src.stop(startTime + duration);
    return { src, filter };
  }

  // Throttle: don't replay same sound type within `minGap` ms
  _throttle(type, minGap) {
    const now = this.engine.now * 1000;
    const last = this._lastPlayed[type] || 0;
    if (now - last < minGap) return false;
    this._lastPlayed[type] = now;
    return true;
  }

  // --- Combat SFX ---

  /** Rifle/machine gun: short noise burst + click */
  gunfire(unitType = UNIT_LI) {
    if (!this.engine.ctx) return;
    if (!this.engine.allocVoice()) return;
    this.engine.duck();

    const t = this.engine.now;
    let peak = 0.15, attack = 0.001, decay = 0.06, filterFreq = 3000;

    // Sniper: sharper, higher crack
    if (unitType === UNIT_SNIPER) { peak = 0.3; decay = 0.12; filterFreq = 5000; }
    // MG: punchier
    else if (unitType === UNIT_MG) { peak = 0.2; decay = 0.04; filterFreq = 2500; }
    // HI: heavier
    else if (unitType === UNIT_HI) { peak = 0.18; decay = 0.08; filterFreq = 2000; }

    const gain = this.engine.ctx.createGain();
    gain.connect(this.engine.sfxGain);
    this._env(gain, peak, attack, decay, t);

    const { src } = this._noiseFilter('bandpass', filterFreq, 2, t, decay);
    src.connect(gain);

    // Add a sharp transient click
    const click = this._osc('square', 800, t, 0.01);
    const clickGain = this.engine.ctx.createGain();
    this._env(clickGain, peak * 0.5, 0.001, 0.008, t);
    click.connect(clickGain);
    clickGain.connect(this.engine.sfxGain);

    setTimeout(() => this.engine.releaseVoice(), (attack + decay) * 1000 + 50);
  }

  /** Explosion: low-passed noise burst with long decay */
  explosion() {
    if (!this.engine.ctx) return;
    if (!this._throttle('explosion', 40)) return;
    if (!this.engine.allocVoice()) return;
    this.engine.duck();

    const t = this.engine.now;
    const peak = 0.4, attack = 0.005, decay = 0.5;

    const gain = this.engine.ctx.createGain();
    gain.connect(this.engine.sfxGain);
    this._env(gain, peak, attack, decay, t);

    // Low rumble noise
    const { src, filter } = this._noiseFilter('lowpass', 200, 1, t, decay);
    filter.frequency.exponentialRampToValueAtTime(60, t + decay);
    src.connect(gain);

    // Sub-bass thump
    const sub = this._osc('sine', 60, t, decay);
    const subGain = this.engine.ctx.createGain();
    subGain.gain.setValueAtTime(0, t);
    subGain.gain.linearRampToValueAtTime(0.5, t + 0.01);
    subGain.gain.exponentialRampToValueAtTime(0.001, t + decay);
    sub.connect(subGain);
    subGain.connect(this.engine.sfxGain);

    setTimeout(() => this.engine.releaseVoice(), decay * 1000 + 100);
  }

  /** Cannon fire: punchy low-freq sine with fast attack */
  cannon() {
    if (!this.engine.ctx) return;
    if (!this._throttle('cannon', 50)) return;
    if (!this.engine.allocVoice()) return;
    this.engine.duck();

    const t = this.engine.now;
    const peak = 0.35, attack = 0.002, decay = 0.2;

    const gain = this.engine.ctx.createGain();
    gain.connect(this.engine.sfxGain);
    this._env(gain, peak, attack, decay, t);

    const osc = this._osc('sine', 120, t, decay);
    osc.frequency.exponentialRampToValueAtTime(40, t + decay);
    osc.connect(gain);

    // Add noise transient
    const { src } = this._noiseFilter('highpass', 1000, 1, t, 0.03);
    const ng = this.engine.ctx.createGain();
    this._env(ng, 0.2, 0.001, 0.025, t);
    src.connect(ng);
    ng.connect(this.engine.sfxGain);

    setTimeout(() => this.engine.releaseVoice(), decay * 1000 + 50);
  }

  /** Unit death: short descending tone */
  unitDeath() {
    if (!this.engine.ctx) return;
    if (!this._throttle('death', 30)) return;
    if (!this.engine.allocVoice()) return;

    const t = this.engine.now;
    const peak = 0.12, attack = 0.005, decay = 0.2;

    const gain = this.engine.ctx.createGain();
    gain.connect(this.engine.sfxGain);
    this._env(gain, peak, attack, decay, t);

    const osc = this._osc('triangle', 400, t, decay);
    osc.frequency.exponentialRampToValueAtTime(80, t + decay);
    osc.connect(gain);

    setTimeout(() => this.engine.releaseVoice(), decay * 1000 + 50);
  }

  /** Commander down: dramatic descending chord */
  commanderDown() {
    if (!this.engine.ctx) return;
    if (!this.engine.allocVoice()) return;
    this.engine.duck();

    const t = this.engine.now;
    const freqs = [220, 175, 130]; // A3-F3-C3 minor descent
    const decay = 1.2;

    freqs.forEach((f, i) => {
      const gain = this.engine.ctx.createGain();
      gain.connect(this.engine.sfxGain);
      this._env(gain, 0.25, 0.01 + i * 0.08, decay, t);

      const osc = this._osc('sawtooth', f, t, decay + 0.2);
      osc.frequency.exponentialRampToValueAtTime(f * 0.5, t + decay);
      osc.connect(gain);
    });

    setTimeout(() => this.engine.releaseVoice(), decay * 1000 + 200);
  }

  // --- UI SFX ---

  uiClick() {
    this._blip(800, 0.04, 0.08);
  }

  uiRecruit() {
    // Ascending two-tone
    this._blip(440, 0.04, 0.06);
    setTimeout(() => this._blip(660, 0.04, 0.08), 60);
  }

  uiBuild() {
    // Solid thunk
    if (!this.engine.ctx) return;
    const t = this.engine.now;
    const gain = this.engine.ctx.createGain();
    gain.connect(this.engine.sfxGain);
    this._env(gain, 0.3, 0.001, 0.12, t);
    const osc = this._osc('sine', 100, t, 0.15);
    osc.frequency.exponentialRampToValueAtTime(50, t + 0.12);
    osc.connect(gain);
  }

  uiError() {
    // Low buzz
    if (!this.engine.ctx) return;
    const t = this.engine.now;
    const gain = this.engine.ctx.createGain();
    gain.connect(this.engine.sfxGain);
    this._env(gain, 0.15, 0.005, 0.15, t);
    const osc = this._osc('square', 150, t, 0.16);
    osc.connect(gain);
  }

  uiTactic() {
    // Command chirp — quick rising sweep
    if (!this.engine.ctx) return;
    const t = this.engine.now;
    const gain = this.engine.ctx.createGain();
    gain.connect(this.engine.sfxGain);
    this._env(gain, 0.15, 0.002, 0.1, t);
    const osc = this._osc('sine', 500, t, 0.12);
    osc.frequency.linearRampToValueAtTime(900, t + 0.08);
    osc.connect(gain);
  }

  /** Base under attack siren — urgent two-tone alarm */
  baseAlert() {
    if (!this.engine.ctx) return;
    if (!this.engine.allocVoice()) return;

    const t = this.engine.now;
    const totalDur = 1.5;

    // Two alternating tones
    for (let cycle = 0; cycle < 3; cycle++) {
      const start = t + cycle * 0.5;
      for (const freq of [800, 600]) {
        const noteStart = start + (freq === 600 ? 0.25 : 0);
        const gain = this.engine.ctx.createGain();
        gain.connect(this.engine.sfxGain);
        gain.gain.setValueAtTime(0, noteStart);
        gain.gain.linearRampToValueAtTime(0.2, noteStart + 0.02);
        gain.gain.setValueAtTime(0.2, noteStart + 0.2);
        gain.gain.exponentialRampToValueAtTime(0.001, noteStart + 0.25);

        const osc = this._osc('square', freq, noteStart, 0.26);
        osc.connect(gain);
      }
    }

    setTimeout(() => this.engine.releaseVoice(), totalDur * 1000 + 100);
  }

  // --- Internal helpers ---

  _blip(freq, peak, decay) {
    if (!this.engine.ctx) return;
    const t = this.engine.now;
    const gain = this.engine.ctx.createGain();
    gain.connect(this.engine.sfxGain);
    this._env(gain, peak, 0.001, decay, t);
    const osc = this._osc('sine', freq, t, decay + 0.01);
    osc.connect(gain);
  }

  // --- Event-driven batch processor ---
  // Call once per snapshot with the events array + camera position for spatial audio.

  processEvents(events, cameraWorldX, cameraWorldY) {
    if (!this.engine.ctx || !events || events.length === 0) return;

    let hadCombat = false;
    for (const evt of events) {
      switch (evt.type) {
        case EVENT_DAMAGE: {
          hadCombat = true;
          // Use sourceX/sourceY for positional, but we don't have unit type
          // so play generic gunfire with slight randomization
          if (this._throttle('damage', 20)) {
            this.gunfire(Math.floor(Math.random() * 3)); // LI/HI/MG
          }
          break;
        }
        case EVENT_PROJECTILE: {
          // Artillery/missile projectiles → explosion on impact
          // The event has x/y/targetX/targetY — play launch + impact
          this.cannon();
          break;
        }
        case EVENT_DEATH: {
          this.unitDeath();
          break;
        }
        case EVENT_COMMANDER_DOWN: {
          this.commanderDown();
          break;
        }
      }
    }
    return hadCombat;
  }
}
