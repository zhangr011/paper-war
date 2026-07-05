// Motion enhancer — uses the vendored Motion library (window.Motion,
// from motiondivision/motion) to add spring-physics interactions that
// CSS easing can't produce. Loaded after vendor/motion.min.js (deferred
// UMD global), so window.Motion is available by the time this module
// runs (modules are deferred by default and execute in document order).
//
// Two responsibilities, both per the frontend-design skill's "Motion:
// use animations for micro-interactions" guidance:
//
// 1. Spring press-feedback on every button (all pages). A scale dip to
//    0.95 on pointerdown, springy return to 1 on pointerup/leave.
//    Showcases Motion's spring physics on every interactive surface;
//    doesn't conflict with the CSS entrance cascades (different trigger).
//
// 2. Spring entrance on the login card (replaces the CSS rise-in for
//    that screen to avoid double-animation — see the override below).
//    Spring overshoot gives a tactile "settle" CSS ease-out can't.
//
// Reduced-motion: Motion respects prefers-reduced-motion via its own
// internals, but we also short-circuit here so no spring work runs.

const MOTION = globalThis.Motion;
const prefersReduced = globalThis.matchMedia?.('(prefers-reduced-motion: reduce)').matches;
if (!MOTION) {
  console.warn('[motion_enhance] window.Motion unavailable — vendor/motion.min.js missing?');
}
if (MOTION && !prefersReduced) {
  installButtonPressSpring();
  installLoginSpringEntrance();
}

// --- 1. Button press spring (all pages) ----------------------------------

function installButtonPressSpring() {
  // Use event delegation: one listener per event, applies to every
  // button now and in the future (career/leaderboard rows are dynamic).
  const press = (el) => MOTION.animate(el, { scale: 0.95 }, { type: 'spring', stiffness: 500, damping: 30 });
  const release = (el) => MOTION.animate(el, { scale: 1 }, { type: 'spring', stiffness: 350, damping: 18 });

  const opts = { passive: true };
  document.addEventListener('pointerdown', (e) => {
    const b = e.target.closest('button, [role="button"]');
    if (b && !b.disabled) press(b);
  }, opts);
  document.addEventListener('pointerup', (e) => {
    const b = e.target.closest('button, [role="button"]');
    if (b) release(b);
  }, opts);
  document.addEventListener('pointerleave', (e) => {
    const b = e.target.closest('button, [role="button"]');
    if (b) release(b);
  }, opts);
  // pointerleave doesn't bubble; catch the case where the pointer
  // cancels mid-press (e.g. drag off + release elsewhere).
  document.addEventListener('pointercancel', (e) => {
    const b = e.target.closest('button, [role="button"]');
    if (b) release(b);
  }, opts);
}

// --- 2. Login spring entrance --------------------------------------------

function installLoginSpringEntrance() {
  const login = document.getElementById('login-screen');
  if (!login) return;

  // The CSS rise-in cascade (style.css) animates the same elements we
  // spring here. Disable it for login by stripping the animation-name
  // so only the Motion spring runs — no double-animation, no jitter.
  for (const sel of ['.login-title', '.login-subtitle', '#login-form']) {
    const el = login.querySelector(sel);
    if (el) el.style.animationName = 'none';
  }

  const run = () => {
    const items = [
      login.querySelector('.login-title'),
      login.querySelector('.login-subtitle'),
      login.querySelector('#login-form'),
    ].filter(Boolean);
    items.forEach((el, i) => {
      // Set initial state, then spring to natural with a per-item delay.
      el.style.opacity = '0';
      el.style.transform = 'translateY(8px)';
      MOTION.animate(el, { opacity: 1, y: 0 }, {
        type: 'spring',
        stiffness: 120,
        damping: 14,
        delay: i * 0.09,
      }).then(() => {
        // Clear the inline overrides once settled so the card reads as
        // normal DOM for subsequent interactions.
        el.style.opacity = '';
        el.style.transform = '';
      });
    });
  };

  if (login.classList.contains('active')) {
    run();
  }
  // Re-run if login becomes active again later (e.g. logout flow).
  new MutationObserver(() => {
    if (login.classList.contains('active')) run();
  }).observe(login, { attributes: true, attributeFilter: ['class'] });
}
