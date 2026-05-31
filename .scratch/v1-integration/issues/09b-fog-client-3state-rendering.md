Status: ready-for-agent

## Parent

`.scratch/v1-integration/PRD.md`

## Depends on

Issue 09a — server must send 3-state fog data before client can render it

## What to build

Fix the client fog system to handle 3-state visibility and fix the shared ArrayBuffer bug.

### Current state

`connection.js` extracts fog data from the binary snapshot by scanning backwards for marker `0xFF 0xFD`, then creates a `Uint8Array` view into the WebSocket's ArrayBuffer. `state.js` stores `fogVisible` as a flat array. `main.js:buildFogTiles()` draws a single black overlay (alpha 0.55) on any tile where `fog[ty * fogW + tx] == 0`.

### Changes required

**1. Fix shared ArrayBuffer bug**

`connection.js` line 260:
```js
visible: new Uint8Array(data, i + 5, fogSize),
```

This creates a view into the WebSocket message buffer. If the browser reuses or detaches that buffer, fog data becomes garbage. Must copy:

```js
visible: new Uint8Array(data.slice(i + 5, i + 5 + fogSize)),
```

**2. Handle 3-state fog rendering**

After issue 09a ships, `fogData.visible` will contain values 0 (unexplored), 1 (explored), 2 (visible). Update rendering:

| State | Value | Client rendering |
|-------|-------|-----------------|
| Unexplored | 0 | Fully black overlay (alpha 0.85) |
| Explored | 1 | Dimmed overlay (alpha 0.45) — terrain visible, no units |
| Visible | 2 | No overlay |

In `buildFogTiles()`, replace the single overlay pass with two passes (or a single pass with conditional alpha):

```js
const val = fog[ty * fogW + tx];
if (val === FogVisible) continue; // no overlay
const alpha = val === FogUnexplored ? 0.85 : 0.45;
tiles.push({ x: sx, y: sy, w: tw, h: th, r: 0.0, g: 0.0, b: 0.0, a: alpha });
```

Define constants at top of `main.js` or `state.js`:
```js
const FogUnexplored = 0;
const FogExplored = 1;
const FogVisible = 2;
```

**3. Explored tiles show terrain but hide enemy units**

This is already partially handled: `GenerateSnapshot` in the server filters enemy units based on `fogGrid.IsVisible()` (which after 09a returns true for both explored and visible). Update the server check to use `IsCurrentlyVisible()` instead (only true for `FogVisible` state) — but this is a server change in issue 09a. The client just needs to render correctly.

### Acceptance criteria

- [ ] Fog data is copied (not shared) from WebSocket buffer — no ArrayBuffer detachment bug
- [ ] Unexplored tiles render as dark overlay (alpha ~0.85)
- [ ] Explored tiles render as dimmed overlay (alpha ~0.45)
- [ ] Visible tiles have no overlay
- [ ] Constants `FogUnexplored`, `FogExplored`, `FogVisible` defined in one place
- [ ] No visual regression when server sends only 0/1 values (backward compat during rollout)

### Files to modify

- `client/src/connection.js` — copy fog data with `data.slice()`
- `client/src/main.js` — `buildFogTiles()` 3-state rendering with conditional alpha
- `client/src/state.js` — add fog state constants

### Out of scope

- RLE/delta decoding (server still sends raw grid in v1)
- Terrain-only rendering for explored tiles (just dim overlay is fine for v1)
- "Last seen" unit ghosts on explored tiles (show faded unit positions from last visible frame)
