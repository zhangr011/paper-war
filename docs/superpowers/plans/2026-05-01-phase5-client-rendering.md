# Phase 5: Client Rendering — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans.

**Goal:** Build the browser-based 2.5D isometric WebGL client with state interpolation, input handling, and UI.

**Architecture:** Plain JavaScript with ES modules (no build tools). WebGL renders terrain + units + effects. State layer manages double-buffered interpolation between server snapshots. HTML/CSS overlay for UI panels.

**Tech Stack:** JavaScript ES modules, WebGL, WebSocket API, CSS Grid

**Spec reference:** `docs/superpowers/specs/2026-05-01-paper-war-rts-design.md` Section 8

---

## File Structure

```
client/
  index.html            # Entry point with UI layout
  src/
    main.js             # Bootstrap + game loop
    gl.js               # WebGL context + sprite batch
    camera.js           # Isometric camera (pan/zoom)
    iso.js              # World↔Screen coordinate conversion
    state.js            # Double-buffer state + interpolation
    connection.js       # WebSocket client
    input.js            # Mouse/keyboard handling
  style.css             # UI styling
```

---

### Task 1: HTML + CSS (UI Layout)
### Task 2: WebGL Context + Sprite Batch Renderer
### Task 3: Isometric Camera + Coordinate Conversion
### Task 4: State Interpolation (15Hz→30fps)
### Task 5: WebSocket Connection
### Task 6: Input Handling
### Task 7: Main Game Loop Integration
