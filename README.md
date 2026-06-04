# Paper War

A real-time strategy game with a paper/pencil aesthetic. Go server, WebGL client, WebSocket binary protocol.

## Quick Start

```bash
cd server
go build -o paper-war-server ./cmd/server/
./paper-war-server
```

Open `http://localhost:9091` in a browser.

## Architecture

```
server/                          # Go backend
  cmd/server/main.go             # Entry point, WebSocket handler, game loop
  pkg/
    ai/                          # AI system (attack, retreat, formation)
    boid/                        # Boid forces (separation, cohesion, alignment)
    combat/                      # Combat, death, leveling, projectiles, recruiting
    commander/                   # Commander promotion and type system
    component/                   # ECS components (health, position, movement, etc.)
    ecs/                         # Entity-Component-System (world, pool, entity)
    fixed/                       # Fixed-point arithmetic (12.4, 4096 = 1.0)
    fog/                         # Fog of war grid
    formation/                   # Formation roles and offsets
    game/                        # Session, lifecycle, matchmaking
    movement/                    # Movement system with velocity
    network/                     # Binary protocol, snapshots, culling, server messages
    objective/                   # Match objectives (elimination, capture)
    pathfinding/                 # Flow field pathfinding
    persist/                     # Postgres roster persistence
    spatial/                     # Spatial hash for queries
    terrain/                     # Dynamic terrain system
    tilemap/                     # Map generation with presets

client/                          # Frontend (raw JS, no build step)
  index.html                     # Entry point
  src/
    app.js                       # App shell, lobby, UI
    camera.js                    # Pan/zoom camera
    connection.js                # WebSocket + binary protocol decoder
    gl.js                        # WebGL renderer
    input.js                     # Keyboard/mouse input
    iso.js                       # Isometric projection
    main.js                      # Game loop, renderer, match result overlay
    state.js                     # Client game state, unit tracking
```

## Binary Protocol

Messages use a magic prefix system over WebSocket binary frames:

| Prefix | Type |
|--------|------|
| `0xFF 0xFD` | Map terrain data |
| `0xFF 0xFE` | Server messages (gold, match result, roster) |
| `0xFF 0xFE 0xFD 0xFC` | Fog grid marker (within snapshot) |
| `[tick uint32 LE]` | Snapshot data (starts with tick, no prefix) |

## Game Features

- **ECS architecture** — Entity-Component-System with systems processed by priority
- **Binary WebSocket protocol** — Compact snapshots with change masks, event serialization
- **Fog of war** — Per-player visibility grid, spectator sees all
- **Combat system** — Attack, damage, death events, commander promotion
- **AI opponents** — Boid-based formation, attack/retreat decisions
- **Isometric WebGL renderer** — GPU-batched terrain and unit rendering
- **Match objectives** — Elimination mode, capture mode
- **Roster persistence** — PostgreSQL backend for player rosters

## Running Tests

```bash
cd server
go test -timeout 120s ./...
```

## License

Private repository.
