// One-shot real multiplayer playtest harness.
// Two browser contexts queue → match_found → drive both players' squads
// toward each other via direct sendMoveSquad calls → wait for match result.
const { test, expect } = require('@playwright/test');

const PORT = 9091;

test.describe.serial('Multiplayer playtest', () => {
  test('queue → match → combat → result + AAR', async ({ browser }) => {
    const logs = [];
    const ts = () => new Date().toISOString().slice(11, 19);
    const log = (s) => { logs.push(`[${ts()}] ${s}`); };

    const ctxA = await browser.newContext();
    const ctxB = await browser.newContext();
    const pageA = await ctxA.newPage();
    const pageB = await ctxB.newPage();

    const errors = [];
    for (const [p, who] of [[pageA, 'A'], [pageB, 'B']]) {
      p.on('pageerror', (e) => errors.push(`${who} pageerror: ${e.message}`));
      p.on('console', (m) => {
        const t = m.text();
        if (m.type() === 'error' && !/favicon|404|net::ERR|Preflight|CORS/.test(t)) {
          errors.push(`${who} console.error: ${t}`);
        }
      });
    }

    log('Loading app');
    await Promise.all([
      pageA.goto(`http://localhost:${PORT}/`, { waitUntil: 'domcontentloaded' }),
      pageB.goto(`http://localhost:${PORT}/`, { waitUntil: 'domcontentloaded' }),
    ]);

    log('Login');
    await Promise.all([
      pageA.fill('#login-username', 'alice'),
      pageB.fill('#login-username', 'bob'),
    ]);
    await Promise.all([
      pageA.click('#login-form button[type="submit"]'),
      pageB.click('#login-form button[type="submit"]'),
    ]);
    await Promise.all([
      pageA.waitForSelector('#lobby-screen.active', { timeout: 8000 }),
      pageB.waitForSelector('#lobby-screen.active', { timeout: 8000 }),
    ]);
    log('Both in lobby');

    await Promise.all([
      pageA.click('#find-match-btn'),
      pageB.click('#find-match-btn'),
    ]);
    log('Both queued; waiting match_found');

    await Promise.all([
      pageA.waitForFunction(() => window.__paperWarGame && window.__paperWarGame.running, { timeout: 15000 })
        .catch(e => log(`A game start failed: ${e.message}`)),
      pageB.waitForFunction(() => window.__paperWarGame && window.__paperWarGame.running, { timeout: 15000 })
        .catch(e => log(`B game start failed: ${e.message}`)),
    ]);

    const pidA = await pageA.evaluate(() => window.__paperWarGame?.playerID);
    const pidB = await pageB.evaluate(() => window.__paperWarGame?.playerID);
    log(`In game. playerIDs: A=${pidA} B=${pidB}`);
    expect(pidA).not.toBe(pidB);
    expect(pidA > 0 && pidB > 0).toBe(true);

    // Wait for snapshots to populate state on both clients (avoid racing the
    // first snapshot — state.units is empty until it lands).
    await Promise.all([
      pageA.waitForFunction(() => window.__paperWarGame?.state?.units?.size > 0, { timeout: 10000 })
        .catch(e => log(`A units never arrived: ${e.message}`)),
      pageB.waitForFunction(() => window.__paperWarGame?.state?.units?.size > 0, { timeout: 10000 })
        .catch(e => log(`B units never arrived: ${e.message}`)),
    ]);
    log('Snapshots populated on both clients');

    // Helper: drive one player's squads toward the opposite spawn point.
    // The client doesn't see enemy units until they enter vision (fog of war),
    // so we use the map's spawn points as the known destination.
    const drive = async (page, who, myPid) => {
      const start = Date.now();
      const targetSpawnIdx = myPid === 1 ? 1 : 0;  // p1 → spawns[1], p2 → spawns[0]
      let loggedOnce = false;
      while (Date.now() - start < 240000) { // 4 min cap
        const done = await page.evaluate((args) => {
          const g = window.__paperWarGame;
          if (!g || !g.state || !g.connection) return { ok: false };
          // Check for match-result overlay first
          const ov = document.getElementById('match-result-overlay');
          if (ov && ov.style.display !== 'none') return { ok: true, text: ov.innerText.slice(0, 500) };

          const spawns = g.mapData?.spawns;
          if (!spawns || !spawns[args.targetSpawnIdx]) return { ok: false, reason: 'no spawn', tick: g.state.tick };

          const [tx, ty] = spawns[args.targetSpawnIdx];
          const targetX = Math.round(tx * 4096);
          const targetY = Math.round(ty * 4096);

          // Collect my squads
          const units = g.state.getRenderUnits ? g.state.getRenderUnits() : [...g.state.units.values()];
          const mine = units.filter(u => u.team === args.myPid && u.alive !== false);
          const squads = [...new Set(mine.map(u => u.squadID || u.boidSquadID).filter(Boolean))];

          // Send CmdMove for each
          let sent = 0;
          for (const sid of squads) {
            try { g.connection.sendMoveSquad(sid, targetX, targetY, 0); sent++; } catch (e) {}
          }
          return {
            ok: false,
            tick: g.state.tick,
            squads: squads.length,
            mine: mine.length,
            target: [tx, ty],
            sent,
          };
        }, { targetSpawnIdx, myPid });

        if (done.ok) { log(`${who}: result — ${done.text?.replace(/\s+/g, ' ').slice(0, 250)}`); return done; }
        if (!loggedOnce || done.tick % 50 === 0) {
          log(`${who} tick=${done.tick} squads=${done.squads} mine=${done.mine} target=${JSON.stringify(done.target)} sent=${done.sent}`);
          loggedOnce = true;
        }
        await page.waitForTimeout(3000);
      }
      return { ok: false };
    };

    log('Driving both players toward opposite spawns');
    const [rA, rB] = await Promise.all([
      drive(pageA, 'A', pidA).catch(e => ({ ok: false, err: e.message })),
      drive(pageB, 'B', pidB).catch(e => ({ ok: false, err: e.message })),
    ]);
    log(`A result: ${JSON.stringify(rA).slice(0, 300)}`);
    log(`B result: ${JSON.stringify(rB).slice(0, 300)}`);

    // Give both a moment to render the overlay
    await pageA.waitForTimeout(2000).catch(() => {});

    const grabOverlay = async (page, who) => {
      return await page.evaluate(() => {
        const el = document.getElementById('match-result-overlay');
        return el ? el.innerText.slice(0, 800) : null;
      });
    };
    const overlayA = await grabOverlay(pageA, 'A');
    const overlayB = await grabOverlay(pageB, 'B');
    log(`A overlay:\n${overlayA || '(none)'}`);
    log(`B overlay:\n${overlayB || '(none)'}`);

    console.log('\n========= FINAL LOG =========');
    logs.forEach(l => console.log(l));
    console.log('========= PAGE ERRORS =========');
    errors.forEach(e => console.log(e));
    console.log('==============================');

    expect(overlayA || overlayB).not.toBeNull();
    expect(errors).toEqual([]);

    await ctxA.close();
    await ctxB.close();
  });
});
