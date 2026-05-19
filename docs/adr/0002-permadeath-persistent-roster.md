# Permadeath with Persistent Roster

CombatUnits and Commanders are persistent across matches and permanently die when killed in-game. This was chosen over respawn-after-match or stats-only-persistence because it creates genuine strategic tension — every engagement risks irreversible loss. The roster is stored in PostgreSQL. Players recover through a slow trickle of rookie units (1-2 per hour per Commander) and a starter roster for new accounts. This decision drives significant infrastructure (persistence, auth, match flow) and fundamentally shapes the game's emotional stakes.

Considered options:
- Stats persist, units respawn after match (traditional RTS) — rejected: no emotional stakes
- Full permadeath with no recovery — rejected: too punishing, players quit
- Starter roster + slow trickle — accepted: balanced risk and recovery
