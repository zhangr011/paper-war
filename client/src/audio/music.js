// audio/music.js
// Match stingers: ascending major chord for victory, descending minor for defeat.

export class Music {
  constructor(engine) {
    this.engine = engine;
  }

  /**
   * Play a chord (array of frequencies) with optional arpeggio delay.
   */
  _playChord(freqs, duration, arpeggioDelay = 0, oscType = 'triangle') {
    if (!this.engine.ctx) return;
    const t = this.engine.now;
    const ctx = this.engine.ctx;

    freqs.forEach((f, i) => {
      const start = t + i * arpeggioDelay;
      const gain = ctx.createGain();
      gain.connect(this.engine.musicGain);
      gain.gain.setValueAtTime(0, start);
      gain.gain.linearRampToValueAtTime(0.3, start + 0.02);
      gain.gain.setValueAtTime(0.3, start + duration * 0.5);
      gain.gain.exponentialRampToValueAtTime(0.001, start + duration);

      const osc = ctx.createOscillator();
      osc.type = oscType;
      osc.frequency.setValueAtTime(f, start);
      osc.start(start);
      osc.stop(start + duration + 0.1);
      osc.connect(gain);

      // Add a soft fifth above for warmth
      const osc2 = ctx.createOscillator();
      osc2.type = 'sine';
      osc2.frequency.setValueAtTime(f * 1.5, start);
      const gain2 = ctx.createGain();
      gain2.connect(this.engine.musicGain);
      gain2.gain.setValueAtTime(0, start);
      gain2.gain.linearRampToValueAtTime(0.1, start + 0.05);
      gain2.gain.exponentialRampToValueAtTime(0.001, start + duration);
      osc2.connect(gain2);
      osc2.start(start);
      osc2.stop(start + duration + 0.1);
    });
  }

  /** Victory: ascending C major arpeggio (C5-E5-G5-C6) then sustained chord */
  victory() {
    if (!this.engine.ctx) return;
    // C5=523, E5=659, G5=784, C6=1047
    this._playChord([523, 659, 784, 1047], 2.5, 0.12, 'triangle');
    // Follow up with full sustained chord after arpeggio
    setTimeout(() => {
      this._playChord([523, 659, 784, 1047], 3.0, 0, 'sawtooth');
    }, 600);
  }

  /** Defeat: descending A minor (A4-F4-E4-A3) then sustained minor chord */
  defeat() {
    if (!this.engine.ctx) return;
    // A4=440, F4=349, E4=330, A3=220
    this._playChord([440, 349, 330, 220], 2.5, 0.15, 'triangle');
    setTimeout(() => {
      this._playChord([220, 262, 330, 415], 3.0, 0, 'sawtooth'); // Am chord
    }, 800);
  }
}
