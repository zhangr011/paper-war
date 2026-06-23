// match_result.js — pure helpers for the match-result overlay.
//
// Extracted from Game.showMatchResult (main.js) so the spectator-vs-player
// branching can be unit-tested without spinning up a Game instance (which
// pulls in WebGL, audio, and the canvas DOM tree).
//
// Bug #03 regression surface: spectator (playerID === 0) must see a neutral
// "Team Blue/Red Wins" message; non-spectators see Victory/Defeat based on
// their faction.

// Faction indices used on the wire.  winner === FactionPlayer (0) means the
// Blue team won; winner === FactionEnemy (1) means the Red team won.
export const FACTION_PLAYER = 0;
export const FACTION_ENEMY = 1;

// Team colour used in the overlay heading.
export const TEAM_BLUE_COLOR = '#4488FF';
export const TEAM_RED_COLOR = '#FF4444';
export const WIN_COLOR = '#4CAF50';
export const LOSE_COLOR = '#FF4444';

/**
 * Decide the heading text + colour for the match-result overlay.
 *
 * @param {number} playerID  The viewer's playerID.  0 = spectator.
 * @param {number} winner    The winning faction index (0 = Blue, 1 = Red).
 * @returns {{heading: string, color: string}}
 */
export function formatMatchResultHeading(playerID, winner) {
  if (playerID === 0) {
    // Spectator: neutral phrasing, team-coloured.
    const teamName = winner === FACTION_PLAYER ? 'Blue' : 'Red';
    return {
      heading: `Team ${teamName} Wins!`,
      color: winner === FACTION_PLAYER ? TEAM_BLUE_COLOR : TEAM_RED_COLOR,
    };
  }
  // Non-spectator: pid 1 ↔ faction 0 (Blue), pid 2 ↔ faction 1 (Red).
  const isWin = winner === (playerID - 1);
  return {
    heading: isWin ? 'Victory!' : 'Defeat',
    color: isWin ? WIN_COLOR : LOSE_COLOR,
  };
}
