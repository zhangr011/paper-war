// audio/ambient.js
// Looping ambient generator: low wind + distant battle rumble.
// Plays during PhasePlaying, ducks when SFX fire.

export class Ambient {
  constructor(engine) {
    this.engine = engine;
    this.nodes = [];
    this.playing = false;
  }

  start() {
    if (!this.engine.ctx || this.playing) return;
    this.playing = true;
    const ctx = this.engine.ctx;
    const t = this.engine.now;

    // 1. Low-frequency wind rumble — filtered brown noise
    const windSrc = this.engine.noiseSource();
    const windFilter = ctx.createBiquadFilter();
    windFilter.type = 'lowpass';
    windFilter.frequency.value = 120;
    windFilter.Q.value = 0.5;

    // Slow LFO modulating wind filter for natural gusts
    const lfo = ctx.createOscillator();
    lfo.frequency.value = 0.08; // ~12 second cycle
    const lfoGain = ctx.createGain();
    lfoGain.gain.value = 40;
    lfo.connect(lfoGain);
    lfoGain.connect(windFilter.frequency);
    lfo.start(t);

    const windGain = ctx.createGain();
    windGain.gain.value = 0.6;
    windSrc.connect(windFilter);
    windFilter.connect(windGain);
    windGain.connect(this.engine.ambientGain);
    windSrc.start(t);

    this.nodes.push(windSrc, lfo, windFilter, windGain, lfoGain);

    // 2. Distant battle rumble — very low sine + occasional filtered noise
    const rumbleOsc = ctx.createOscillator();
    rumbleOsc.type = 'sine';
    rumbleOsc.frequency.value = 35;
    const rumbleGain = ctx.createGain();
    rumbleGain.gain.value = 0.15;
    rumbleOsc.connect(rumbleGain);
    rumbleGain.connect(this.engine.ambientGain);
    rumbleOsc.start(t);

    this.nodes.push(rumbleOsc, rumbleGain);

    // 3. Mid-range hiss (air/atmosphere) — very quiet high-pass noise
    const hissSrc = this.engine.noiseSource();
    const hissFilter = ctx.createBiquadFilter();
    hissFilter.type = 'highpass';
    hissFilter.frequency.value = 4000;
    const hissGain = ctx.createGain();
    hissGain.gain.value = 0.03;
    hissSrc.connect(hissFilter);
    hissFilter.connect(hissGain);
    hissGain.connect(this.engine.ambientGain);
    hissSrc.start(t);

    this.nodes.push(hissSrc, hissFilter, hissGain);
  }

  stop() {
    if (!this.playing) return;
    this.playing = false;
    const t = this.engine.now;
    // Fade out then stop
    if (this.engine.ambientGain) {
      this.engine.ambientGain.gain.cancelScheduledValues(t);
      this.engine.ambientGain.gain.setValueAtTime(this.engine.ambientGain.gain.value, t);
      this.engine.ambientGain.gain.linearRampToValueAtTime(0, t + 0.5);
    }

    setTimeout(() => {
      for (const n of this.nodes) {
        try { if (n.stop) n.stop(); } catch (e) {}
        try { n.disconnect(); } catch (e) {}
      }
      this.nodes = [];
      // Restore ambient gain for next start
      if (this.engine.ambientGain && this.engine.ctx) {
        this.engine.ambientGain.gain.value = 0.25;
      }
    }, 600);
  }
}
