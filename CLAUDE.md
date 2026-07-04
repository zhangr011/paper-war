## gstack

Use the `/browse` skill from gstack for all web browsing. Never use `mcp__claude-in-chrome__*` tools.

Available skills: /office-hours, /plan-ceo-review, /plan-eng-review, /plan-design-review, /design-consultation, /design-shotgun, /design-html, /review, /ship, /land-and-deploy, /canary, /benchmark, /browse, /connect-chrome, /qa, /qa-only, /design-review, /setup-browser-cookies, /setup-deploy, /setup-gbrain, /retro, /investigate, /document-release, /codex, /cso, /autoplan, /plan-devex-review, /devex-review, /careful, /freeze, /guard, /unfreeze, /gstack-upgrade, /learn

## Agent skills

### Issue tracker

Issues live as GitHub issues (repo: `zhangr011/paper-war`). Uses `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Using default label vocabulary (needs-triage, needs-info, ready-for-agent, ready-for-human, wontfix). See `docs/agents/triage-labels.md`.

### Issue / PR conventions

Issues filed in this repo follow a consistent shape. Match it on new issues:

- **Title** — verb + object + qualifier, ≤ ~80 chars. Readable as a one-line summary.
- **Motivation** — what's broken or missing, with concrete file/line citations (`path/to/file.go:42`). Cite the consumer count and the cost. Establish "why now" if there's a triggering event (e.g., another issue multiplying the surface).
- **Proposal** — numbered phases. Each phase is independently shippable. Phase 1 is the smallest viable increment.
- **Scope** — files added/changed, named explicitly. Names the public API contract (e.g., "uses only existing exports of `unit_atlas.js`").
- **Out of scope** — explicitly enumerates deferred items (wire-format changes, gameplay effects, pixel editing). Often as important as Scope.
- **Acceptance** — bulleted checklist. Each item is testable ("opens with no console errors", "tests pass with updated assertions").
- **Pointers** — file:line citations for the agent/human picking it up. Include relevant ADRs by number (`ADR-0006`, `ADR-0016`).

**Labels:** one category (`bug` or `enhancement`) + one state (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). Refactor/simplification counts as `enhancement` even if it removes code. Visual glitch counts as `bug` even if no crash.

**Code citations:** always use `path:line` form (e.g., `client/src/unit_atlas.js:138`) so the reader can navigate in one click. Cite consumers of the symbol being changed, not just the definition.

**Triage flow:** before recommending `ready-for-agent`, explore the codebase enough to verify the issue's contract claims (exports exist, payload formats, server-replication status). Note any correction in the agent brief. See `.claude/skills/triage/` for the full skill.

### Domain docs

Single-context layout — one `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.
