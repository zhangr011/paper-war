# ADR-0016: Jitter-Buffered Interpolation at 10Hz

**Date:** 2026-06-14
**Status:** Accepted
**Issue:** [#16 — Client prediction / lag compensation](https://github.com/zhangr011/paper-war/issues/16)

## Context

The client `StateManager` interpolated unit positions between server
snapshots to achieve smooth 30fps rendering from 10Hz authoritative
updates. Three problems degraded visual quality:

### 1. Tick Rate Mismatch (Bug)

`state.js` hardcoded `SERVER_HZ = 5` (`TICK_DURATION_MS = 200`) while
the server actually ticks at **10Hz** (`ServerTicksPerSecond = 10`,
100ms intervals — see `session.go`). The interpolation parameter
`t = elapsed / 200ms` only reached ~0.5 by the time the next snapshot
arrived at 100ms. Units never visually reached their authoritative
positions — they were permanently stuck at the midpoint.

### 2. No Jitter Buffer

Snapshots were applied immediately on arrival. Under network jitter
(±20-50ms at typical RTTs), early-arriving packets caused `t` to jump
backward, producing visible stutter and position popping.

### 3. Correction Tuned for Wrong Tick Rate

`MAX_EXTRAPOLATION_MS = 200` was calibrated for 5Hz (1 tick of
extrapolation). At 10Hz it allowed 2 ticks of extrapolation — too much,
causing units to overshoot. `CORRECTION_SPEED = 10.0` yielded
`correctionT = 10 × 0.1 = 1.0`, a full snap rather than a smooth blend.

## Decision

Implement **queue-based jitter-buffered interpolation** calibrated for
the actual 10Hz server tick rate. The May 18 design session decided
"interpolation only (no client simulation)" — this work makes that
interpolation robust and latency-tolerant.

### Architecture

```
Server (10Hz)          Network              Client (30fps)
─────────────          ───────              ─────────────
  Tick N ──────────►  packet  ───► applySnapshot() ──► pendingQueue
  Tick N+1 ────────► packet  ───► applySnapshot() ──► pendingQueue
                                                        │
                                   update(frameTime) ◄──┤
                                   │                   │
                          ┌────────┴────────┐          │
                          │ elapsed >= tick? │          │
                          │ (t reached 1.0?) │          │
                          └────────┬────────┘          │
                             yes   │   no → break      │
                          ┌────────┴────────┐          │
                          │ _activateSnapshot│◄─────────┘
                          │ shift prev→curr  │
                          └────────┬────────┘
                          ┌────────┴────────┐
                          │ interpolate all  │
                          │ units at param t │
                          └─────────────────┘
```

### Key Changes

| Component             | Before           | After                                |
|-----------------------|------------------|--------------------------------------|
| `TICK_DURATION_MS`    | 200 (5Hz)        | **100 (10Hz)**                       |
| Snapshot application  | Immediate        | **Queued**, activated when `t ≥ 1.0` |
| `MAX_EXTRAPOLATION_MS`| 200 (2 ticks)    | **150 (1.5 ticks)**                  |
| `CORRECTION_THRESHOLD`| 4.0              | **5.0** (genuine desyncs only)       |
| `CORRECTION_SPEED`    | 10.0 (snap)      | **3.0** (30% blend per tick)         |
| Queue capacity        | N/A              | **3** (drops oldest on overflow)     |

### Jitter Buffer Mechanics

Snapshots arriving from the network are **not** applied immediately.
Instead they enter a `pendingQueue` (max 3 entries). The `update()`
method, called once per render frame, checks whether the current
interpolation window has completed (`elapsed >= tickDuration`). Only
then does the next queued snapshot activate (shifting prev→curr).

This naturally absorbs jitter:
- **Early arrival** (jitter < 100ms): snapshot waits in queue. No
  visual disruption.
- **Late arrival** (gap > 100ms): client extrapolates using velocity
  with linear decay until the packet arrives. No stutter.
- **Burst arrival** (multiple packets at once): queue processes them
  one per tick, maintaining smooth 100ms-spaced transitions.
- **Queue overflow** (severe lag, >3 snapshots buffered): oldest
  snapshot is dropped. Client catches up via accelerated correction.

### What Doesn't Use the Queue

Events (damage flashes, death sounds) and fog-of-war data are processed
**immediately** on arrival, bypassing the queue. These are time-critical
— a damage flash delayed by 100ms feels broken.

### Alternatives Considered

1. **Full client-side prediction** (CS:GO/Overwatch style): run a parallel
   simulation, reconcile on server correction. Rejected at the May 18
   design session — too complex for v1, and the game's slow unit speeds
   (0.5 world units/tick) make pure interpolation sufficient.

2. **Adaptive interpolation delay** (track recent jitter, adjust buffer
   size dynamically): considered but deferred. The fixed 1-tick buffer
   handles typical 10-50ms jitter well. Can revisit if real-world testing
   reveals persistent high-jitter connections.

3. **Snap-through for large deltas** (if position error > N units, snap
   instead of blend): rejected in favor of accelerated correction. Snapping
   is visually jarring and breaks immersion even more than a fast slide.

## Consequences

- **Positive:** Units now reach their authoritative positions (Hz bug fix).
  Visual smoothness improves significantly under jitter. No more
  midpoint-stuck units.
- **Positive:** Extrapolation is bounded (150ms max), preventing runaway
  units during packet loss.
- **Positive:** 12 unit tests cover interpolation, jitter buffering,
  extrapolation, correction, out-of-order rejection, and queue overflow.
- **Negative:** 100ms base latency added (1 tick of buffer). Acceptable
  for an RTS with slow unit movement; would be problematic for a twitch
  shooter.
- **Negative:** Client test suite now requires `node` to run (previously
  Go tests only). Added `state_test.mjs` with mock time control.

## References

- Issue #16: Client prediction / lag compensation
- ADR-0013: Match statistics / AAR (shares the snapshot event mechanism)
- Design session May 18, 2026: "interpolation only (no client simulation)"
- Valve Software, "Latency Compensating Methods in Client/Server In-game
  Protocol Design and Optimization" (reference for interpolation/extrapolation
  techniques)
