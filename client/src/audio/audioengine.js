// audio/audioengine.js
// Manages the Web Audio API AudioContext, master gain, mute, and ducking.
// All sound modules route through this single engine.

export class AudioEngine {
  constructor() {
    this.ctx = null;
    this.masterGain = null;
    this.sfxGain = null;
    this.ambientGain = null;
    this.musicGain = null;

    // Mute state persisted to localStorage
    this.muted = localStorage.getItem('pw_muted') === '1';

    // Noise buffer (reused for percussive sounds)
    this._noiseBuffer = null;

    // Active voice count for throttling
    this.activeVoices = 0;
    this.maxVoices = 12;

    // Ducking: when SFX fires, ambient dips briefly
    this._duckDepth = 0.4; // ambient drops to 40% during SFX
    this._duckTimer = null;
  }

  /**
   * Lazily create the AudioContext on first user interaction.
   * Browsers block audio until a user gesture occurs.
   */
  init() {
    if (this.ctx) {
      // Resume if suspended (tab switch, etc.)
      if (this.ctx.state === 'suspended') {
        this.ctx.resume();
      }
      return true;
    }

    try {
      const AC = window.AudioContext || window.webkitAudioContext;
      if (!AC) return false;
      this.ctx = new AC();

      // Master gain → destination
      this.masterGain = this.ctx.createGain();
      this.masterGain.gain.value = this.muted ? 0 : 0.8;
      this.masterGain.connect(this.ctx.destination);

      // Sub-busses
      this.sfxGain = this.ctx.createGain();
      this.sfxGain.gain.value = 0.7;
      this.sfxGain.connect(this.masterGain);

      this.ambientGain = this.ctx.createGain();
      this.ambientGain.gain.value = 0.25;
      this.ambientGain.connect(this.masterGain);

      this.musicGain = this.ctx.createGain();
      this.musicGain.gain.value = 0.5;
      this.musicGain.connect(this.masterGain);

      // Pre-render white noise buffer (2 seconds)
      this._noiseBuffer = this._createNoiseBuffer(2.0);

      return true;
    } catch (e) {
      console.warn('AudioEngine init failed:', e);
      return false;
    }
  }

  _createNoiseBuffer(duration) {
    const len = Math.floor(this.ctx.sampleRate * duration);
    const buf = this.ctx.createBuffer(1, len, this.ctx.sampleRate);
    const data = buf.getChannelData(0);
    for (let i = 0; i < len; i++) {
      data[i] = Math.random() * 2 - 1;
    }
    return buf;
  }

  /** Get a noise source node (consumes from the pre-rendered buffer). */
  noiseSource() {
    if (!this.ctx) return null;
    const src = this.ctx.createBufferSource();
    src.buffer = this._noiseBuffer;
    src.loop = true;
    return src;
  }

  /** Current audio time in seconds. */
  get now() {
    return this.ctx ? this.ctx.currentTime : 0;
  }

  /** Try to allocate a voice slot. Returns false if throttled. */
  allocVoice() {
    if (this.activeVoices >= this.maxVoices) return false;
    this.activeVoices++;
    return true;
  }

  /** Release a voice slot when a sound finishes. */
  releaseVoice() {
    if (this.activeVoices > 0) this.activeVoices--;
  }

  /** Briefly duck the ambient bus when SFX fires. */
  duck() {
    if (!this.ambientGain || !this.ctx) return;
    const t = this.now;
    // Ramp down then back up over 300ms
    this.ambientGain.gain.cancelScheduledValues(t);
    this.ambientGain.gain.setValueAtTime(this.ambientGain.gain.value, t);
    this.ambientGain.gain.linearRampToValueAtTime(0.25 * this._duckDepth, t + 0.05);
    this.ambientGain.gain.linearRampToValueAtTime(0.25, t + 0.35);

    clearTimeout(this._duckTimer);
    this._duckTimer = setTimeout(() => {}, 400);
  }

  toggleMute() {
    this.muted = !this.muted;
    localStorage.setItem('pw_muted', this.muted ? '1' : '0');
    if (this.masterGain && this.ctx) {
      const t = this.now;
      this.masterGain.gain.cancelScheduledValues(t);
      this.masterGain.gain.setValueAtTime(this.masterGain.gain.value, t);
      this.masterGain.gain.linearRampToValueAtTime(this.muted ? 0 : 0.8, t + 0.15);
    }
    return this.muted;
  }

  /** Tear down all audio (call on game stop). */
  destroy() {
    if (this.ctx) {
      this.ctx.close();
      this.ctx = null;
    }
  }
}
