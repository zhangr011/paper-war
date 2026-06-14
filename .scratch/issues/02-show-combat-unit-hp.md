---
Title: Show combat unit HP
Status: done
Priority: medium
Component: client
Type: enhancement
---

## Description
Combat units have HP but it's not visible to the player. Need to render HP bars or HP numbers on units in the game view.

## Scope
- Display HP bar above each combat unit sprite
- Color: green (high) → yellow (mid) → red (low)
- Show on hover or always visible (recommend: always-on thin bar)
- Commander HP bar should be slightly larger / more prominent

## Notes
- HP component already exists server-side and is serialized in snapshots
- Client snapshot parsing already has HP data available
- Likely render in `main.js` draw loop after unit sprites, before fog pass
