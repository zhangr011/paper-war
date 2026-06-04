---
Title: Terrain select in clash test
Status: done
Priority: medium
Component: client+server
Type: enhancement
---

## Description
The clash test currently uses a random map. Add a terrain/map selector to the clash config panel so testers can pick terrain type before starting a clash.

## Scope
- Add terrain preset buttons to clash config panel (e.g. Plains, River, Fortress, Island)
- Send terrain selection to server in `start_clash` message
- Server applies the terrain preset when generating the map for clash mode
- Each preset sets map generator parameters (water ratio, bridge count, wall density, stronghold placement)

## Notes
- Map generator already supports seed-based generation with constraints
- Terrain presets can be implemented as named parameter bundles passed to the map generator
- Clash config panel already has team size buttons — add terrain row below
