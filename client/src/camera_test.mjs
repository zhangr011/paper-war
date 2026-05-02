// camera_test.mjs — TDD test for camera pan direction
// Run with: node client/src/camera_test.mjs

import assert from 'node:assert/strict';

// ---- Minimal Camera simulation (mirrors camera.js logic) ----

const HALF_W = 32;
const HALF_H = 16;

class Camera {
  constructor(viewW, viewH) {
    this.offsetX = 0;
    this.offsetY = 0;
    this.zoom = 1.0;
    this.viewW = viewW;
    this.viewH = viewH;
  }

  pan(dx, dy) {
    this.offsetX += dx / this.zoom;
    this.offsetY += dy / this.zoom;
  }

  worldToScreen(wx, wy) {
    const sx = (wx - wy) * HALF_W * this.zoom - this.offsetX * this.zoom + this.viewW / 2;
    const sy = (wx + wy) * HALF_H * this.zoom - this.offsetY * this.zoom + this.viewH / 2;
    return [sx, sy];
  }
}

// ---- Simulate input handler pan computation (mirrors input.js) ----
function inputPan(keys, speed) {
  let panX = 0, panY = 0;
  if (keys.has('w') || keys.has('arrowup')) panY -= speed;
  if (keys.has('s') || keys.has('arrowdown')) panY += speed;
  if (keys.has('a') || keys.has('arrowleft')) panX -= speed;
  if (keys.has('d') || keys.has('arrowright')) panX += speed;
  return [panX, panY];
}

// ---- Simulate edge scroll computation (mirrors input.js) ----
function edgeScrollPan(mouseX, mouseY, canvasW, canvasH, zone, speed) {
  let panX = 0, panY = 0;
  if (mouseX >= 0 && mouseY >= 0) {
    if (mouseX < zone) panX -= speed;
    if (mouseX > canvasW - zone) panX += speed;
    if (mouseY < zone) panY -= speed;
    if (mouseY > canvasH - zone) panY += speed;
  }
  return [panX, panY];
}

// ---- Tests: verify that pressing a direction scrolls the viewport correctly ----

function testWKeyScrollsUp() {
  // Pressing W should scroll the viewport UP, meaning content moves DOWN.
  // Content moving DOWN = screen Y increases after pan.
  const cam = new Camera(800, 600);
  const [, syBefore] = cam.worldToScreen(32, 32);

  const speed = 100;
  const [panX, panY] = inputPan(new Set(['w']), speed);
  cam.pan(panX, panY);

  const [, syAfter] = cam.worldToScreen(32, 32);

  assert.ok(syAfter > syBefore,
    `W should scroll up (content moves down): before=${syBefore}, after=${syAfter}`);
}

function testSKeyScrollsDown() {
  const cam = new Camera(800, 600);
  const [, syBefore] = cam.worldToScreen(32, 32);

  const speed = 100;
  const [panX, panY] = inputPan(new Set(['s']), speed);
  cam.pan(panX, panY);

  const [, syAfter] = cam.worldToScreen(32, 32);

  assert.ok(syAfter < syBefore,
    `S should scroll down (content moves up): before=${syBefore}, after=${syAfter}`);
}

function testAKeyScrollsLeft() {
  const cam = new Camera(800, 600);
  const [sxBfore] = cam.worldToScreen(32, 32);

  const speed = 100;
  const [panX, panY] = inputPan(new Set(['a']), speed);
  cam.pan(panX, panY);

  const [sxAfter] = cam.worldToScreen(32, 32);

  assert.ok(sxAfter > sxBfore,
    `A should scroll left (content moves right): before=${sxBfore}, after=${sxAfter}`);
}

function testDKeyScrollsRight() {
  const cam = new Camera(800, 600);
  const [sxBfore] = cam.worldToScreen(32, 32);

  const speed = 100;
  const [panX, panY] = inputPan(new Set(['d']), speed);
  cam.pan(panX, panY);

  const [sxAfter] = cam.worldToScreen(32, 32);

  assert.ok(sxAfter < sxBfore,
    `D should scroll right (content moves left): before=${sxBfore}, after=${sxAfter}`);
}

// ---- Edge scroll tests ----

function testEdgeTopScrollsUp() {
  const cam = new Camera(800, 600);
  const [, syBefore] = cam.worldToScreen(32, 32);
  const [panX, panY] = edgeScrollPan(400, 5, 800, 600, 20, 100);
  cam.pan(panX, panY);
  const [, syAfter] = cam.worldToScreen(32, 32);

  assert.ok(syAfter > syBefore, 'Mouse at top edge should scroll up');
}

function testEdgeBottomScrollsDown() {
  const cam = new Camera(800, 600);
  const [, syBefore] = cam.worldToScreen(32, 32);
  const [panX, panY] = edgeScrollPan(400, 595, 800, 600, 20, 100);
  cam.pan(panX, panY);
  const [, syAfter] = cam.worldToScreen(32, 32);

  assert.ok(syAfter < syBefore, 'Mouse at bottom edge should scroll down');
}

function testEdgeLeftScrollsLeft() {
  const cam = new Camera(800, 600);
  const [sxBfore] = cam.worldToScreen(32, 32);
  const [panX, panY] = edgeScrollPan(5, 300, 800, 600, 20, 100);
  cam.pan(panX, panY);
  const [sxAfter] = cam.worldToScreen(32, 32);

  assert.ok(sxAfter > sxBfore, 'Mouse at left edge should scroll left');
}

function testEdgeRightScrollsRight() {
  const cam = new Camera(800, 600);
  const [sxBfore] = cam.worldToScreen(32, 32);
  const [panX, panY] = edgeScrollPan(795, 300, 800, 600, 20, 100);
  cam.pan(panX, panY);
  const [sxAfter] = cam.worldToScreen(32, 32);

  assert.ok(sxAfter < sxBfore, 'Mouse at right edge should scroll right');
}

// ---- Run tests ----
let passed = 0, failed = 0;

for (const [name, fn] of Object.entries({
  testWKeyScrollsUp,
  testSKeyScrollsDown,
  testAKeyScrollsLeft,
  testDKeyScrollsRight,
  testEdgeTopScrollsUp,
  testEdgeBottomScrollsDown,
  testEdgeLeftScrollsLeft,
  testEdgeRightScrollsRight,
})) {
  try {
    fn();
    console.log(`PASS: ${name}`);
    passed++;
  } catch (e) {
    console.log(`FAIL: ${name}`);
    console.log(`  ${e.message}`);
    failed++;
  }
}

console.log(`\n${passed} passed, ${failed} failed`);
process.exit(failed > 0 ? 1 : 0);
