# ADR-0014: Reconnect Support

Status: Accepted
Date: 2026-06-13
Relates to: issue #14

## Context

Paper War is a real-time strategy game played over WebSockets. A player whose
connection drops — flaky WiFi, tab sleep, brief ISP outage — is currently
treated the same as a voluntary forfeit:

  1. The Hub deletes the ClientSession on socket close.
  2. The GameSession keeps ticking, but the player's units stop receiving
     commands (commands are key-bound to playerID via Hub state, which is gone).
  3. There is no mechanism to rejoin — the player must start a brand-new match.

For a 1-hour-traversal game, losing a match to a 5-second WiFi blip is the worst
remaining UX bug. We want dropped players to rejoin their in-progress match
automatically, without losing their army, map state, or match outcome.

## Decision

Add a **reconnect token** system: a short-lived, unguessable token is issued to
each player when a match starts. On reconnect, the client presents the token;
the server validates it, rebinds the new WebSocket to the existing GameSession
player, and re-sends match setup data.

### Components

**MatchRegistry** (`server/pkg/game/match_registry.go`)
  - Token issue/validate/clear with TTL (default 10 minutes)
  - Cryptographically random 16-byte tokens (hex-encoded)
  - Thread-safe (sync.RWMutex); passes `-race`
  - `SetNowFunc` / `SetTTL` for deterministic testing
  - Re-issue for the same player replaces the old token (no stale entries)
  - `Clear()` called on match end to free memory and invalidate stragglers

**Server reconnect handler** (`server/cmd/server/main.go`)
  - `reconnect` text message: `{ type: "reconnect", token: "..." }`
  - Validates token → rejects if missing/expired/match-ended
  - Re-binds new clientID's playerID + InGame via Hub helpers
  - Re-sends `reconnect_ok` JSON + binary map data (0xFF 0xFD prefix)
  - Token is refreshed on successful reconnect (chainable)

**Client disconnect detection + overlay** (`client/src/connection.js`, `main.js`, `app.js`)
  - Token stored on connection.reconnectToken, set on match_found
  - On non-intentional close: show overlay, retry with exponential back-off
    (1s → 30s cap), send `reconnect` on each new socket's `onopen`
  - Intentional `disconnect()` clears the token (no auto-retry)
  - `reconnect_ok`: clears stale entities, refreshes map/playerID, hides overlay
  - `reconnect_failed`: shows toast, returns to lobby after 3s

## Alternatives Considered

### 1. Pause game on disconnect (awaiting reconnect)
Rejected: In a 1v1 match, pausing for one player's flaky connection griefs the
opponent. We continue running; the disconnected player's units simply don't
move, same as if they'd walked away from the keyboard. This is the established
RTS convention (StarCraft, Age of Empires both keep simulating).

### 2. Issue tokens from the matchmaker rather than on match_found
Rejected: The matchmaker doesn't own the GameSession lifecycle. Issuing at
match_found (solo, matchmaking, clash) keeps the registry colocated with the
game loop that consumes it.

### 3. HTTP-based reconnect endpoint
Rejected: Adds a second transport just for one operation. WebSocket text
messages already give us bidirectional framing; no need for a REST endpoint.

### 4. Permanent session IDs (cross-match)
Out of scope for v1. Tokens are per-match and cleared on match end. A future
cross-match session system could layer on top, but it's not needed to solve
the immediate UX problem.

## Consequences

- **Positive**: Dropped players rejoin automatically; no forfeit on transient
  network failure. Reconnect works across the solo/multiplayer/clash paths
  uniformly because tokens are issued at the same code path.
- **Positive**: Token plumbing (issue/validate/refresh) sets up the foundation
  for future features like spectating, shareable replay links, or party
  invites that all need short-lived bearer credentials.
- **Negative**: 16 bytes of token state per active match + ~1 line of map data
  re-send per reconnect. Negligible at expected scale.
- **Negative**: The game runs during disconnect — a griefing opponent could
  intentionally DC to stall. Mitigated by TTL (10 min max before the slot is
  permanently lost) and by the fact that their own units are also idle.

## Open Questions

- Should we add a visible "opponent disconnected" indicator to the other
  player? Currently they see no difference. Trivial future addition.
- Should we send a full state snapshot immediately on reconnect_ok to
  eliminate the 1-tick delay before units reappear? Currently the next regular
  snapshot repopulates state. Acceptable for v1; revisit if perceived lag is
  bad.
