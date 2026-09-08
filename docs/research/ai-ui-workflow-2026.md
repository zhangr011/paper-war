# AI-Driven UI Workflow — Deep Research Report (Aug 2026)

**Question:** What is the best AI-driven workflow for building UI (idea → design → working code), for a solo developer on a vanilla-JS + WebGL game client (DOM/HUD around a canvas, no React), ranked by (1) idea→visible speed, (2) visual quality, (3) code maintainability?

**Method:** 5-angle web sweep → 22 sources fetched → 108 claims extracted → top 25 adversarially verified (3 skeptic votes each; ≥2 refutes kills a claim). **16 confirmed, 4 refuted, 5 unverified** (verification agents hit a provider rate limit; see §6). Confidence is marked per claim as the verify vote, e.g. `[3-0]`.

---

## 1. Verdict (TL;DR)

For a non-React, in-repo client like paper-war's, **the prompt-to-UI generators (v0, Lovable, Bolt, Figma Make) are disqualified as code sources** — their output is React-ecosystem-bound and doesn't transfer `[3-0]`, and every generator hits the "70% problem" complexity wall after which you're hand-editing anyway `[3-0]`. The evidence-backed workflow is **agentic coding over the real repo, with (a) machine-readable design-system context injected into every generation, and (b) a closed visual-feedback loop (screenshots → name the defect → fix → re-screenshot)**. That combination is what separates "renders roughly right but hard-codes colors and violates the design system" from production-grade output. paper-war already owns most of this pipeline via its gstack skill suite; the report ends with the gap list.

---

## 2. Tool classes: what the evidence supports

### 2.1 Prompt-to-UI generators (v0, Lovable, Bolt, Figma Make)

- v0's output is tightly coupled to React/Next.js + Tailwind + shadcn/ui and the Vercel ecosystem; it does not transfer to a non-React codebase without rewriting. `[3-0]` ([Osmani](https://addyo.substack.com/p/ai-driven-prototyping-v0-bolt-and), [buildthisnow](https://www.buildthisnow.com/blog/tools/extensions/claude-code-vs-v0))
- All three major generators hit Osmani's **"70% problem"**: fine to a prototype, then a complexity threshold forces a shift to editing code locally with agentic tools. The end-state is a **hybrid two-stage workflow, not generator-only**. `[3-0]` ([Osmani](https://addyo.substack.com/p/ai-driven-prototyping-v0-bolt-and))
- Speed/quality trade-off: v0 wins **time-to-first-screen** (rendered before any local setup); agentic Claude Code is slower to a first visual but faster to **production-ready code**. `[2-1]` ([buildthisnow](https://www.buildthisnow.com/blog/tools/extensions/claude-code-vs-v0))
- Comparison sites rate agentic IDEs "slowest to v1, best long-term codebase quality." `[2-1]` ([makemyprd](https://www.makemyprd.com/compare/ai-app-builder-comparison-2026))

**Residual use for generators:** disposable *visual sketchpads* — generate 6 looks in a minute, pick a direction, then have the repo agent reimplement it in the house style. Speed criterion satisfied without importing framework-locked code.

### 2.2 Agentic coding tools (Claude Code, Cursor)

- The unverified-but-consistent picture (all 5 rate-limited claims, §6): Claude Code operates over an existing repo with no framework constraints; that is what makes it viable for a vanilla-JS codebase. No verified claim contradicts this, and it matches the tool's documented design.
- Structured workflow beats one-shot prompting at scale: "vibe coding" was inadequate for large projects; Cursor succeeded with a feature-driven human-in-the-loop framework (decompose → build feature-by-feature → continuous testing). `[3-0]` ([arXiv 2604.06373](https://arxiv.org/html/2604.06373v1))

---

## 3. Visual quality: design-system context is the causal lever

This is the strongest-verified section — a full production case study with primary-source confirmation on every vote:

- **Naive design-to-code fails systemically even when it renders fine**: pasting a Figma link into Cursor (via Figma MCP) produced UI that looked acceptable but skipped design-system components, hard-coded colors, overrode typography, and wrote manual CSS — "the model had no understanding of what the design system actually was." `[3-0]` ([monday.com engineering](https://engineering.monday.com/how-we-use-ai-to-turn-figma-designs-into-production-code/))
- **The fix: make the design system machine-readable.** monday.com built a design-system MCP (components, valid props, token rules, a11y rules, examples) whose data was **generated from engineering artifacts** (component code, TS types, config, token definitions) — no hand-maintained parallel docs to drift. `[3-0]` (same source)
- **Context-builder ≠ code-writer.** Their production pipeline is an 11-node LangGraph agent (Figma pull → layout inference → token resolution → component identification → a11y retrieval → implementation plan) exposed to Cursor as one MCP tool; it **returns a structured context object and never writes code** — the repo-local model does the final generation. `[3-0]` (same source)
- Corroborating (extracted, not adversarially verified): generic-looking AI UI is primarily a **missing-design-context** problem, not a model-capability limit ([jeangalea](https://jeangalea.com/ai-ui-looks-generic/)); a dedicated "design skill" document keeps agent output consistent ([timdeschryver](https://timdeschryver.dev/blog/keep-agentic-ai-simple-a-practical-workflow-for-software-development)); agents read the **semantic token layer** (intent), not raw color primitives ([uxdesign.cc](https://uxdesign.cc/agentic-ai-design-systems-figma-a-practical-guide-6ab0b681718d)); Figma-metadata access beat screenshot-based generation on fidelity in EPAM's test (Replit/Figma Make ~92-93% vs v0 72%) ([epam](https://www.epam.com/insights/ai/blogs/best-vibe-coding-tools-v0-lovable-bolt-replit-and-figma-make)).

**Transferable principle for a vanilla-JS repo:** you don't need an MCP — any *always-loaded, artifact-derived* design context works (tokens file + component inventory + do/don't examples). What matters is that every generation pass sees it.

---

## 4. The visual-feedback loop (speed + fidelity)

- Iterate by **feeding screenshots back** to the agent with the specific visual defect named — not prose descriptions. `[3-0]` ([Tweag agentic-coding handbook](https://tweag.github.io/agentic-coding-handbook/WORKFLOW_VISUAL_FEEDBACK/))
- It's a **loop, not a check**: fresh screenshot after every fix, before the next prompt. `[3-0]` (same)
- For deeper debugging, augment screenshots with a browser-context MCP (Chrome DevTools MCP named) piping console logs, DOM, network into context. `[3-0]` (same)
- Playwright now ships **`@playwright/cli`**, a CLI "designed for coding agents," token-efficient by keeping tool schemas and accessibility-tree dumps out of context; official docs recommend CLI over Playwright MCP for agents like Claude Code. `[3-0]`×2 ([playwright.dev](https://playwright.dev/docs/getting-started-cli))
- Extracted (not verified): Playwright-based visual access cuts UI debugging from 10+ prompt iterations to 2-3 ([luca-becker](https://luca-becker.me/blog/level-up-agentic-coding-mcp-2-playwright/)); Claude Code's preview pane runs an auto-verify loop (serve → screenshot → inspect → fix) on by default ([builder.io](https://www.builder.io/blog/claude-code-visual-editor)); multi-variant design exploration is cheap (~6 designs/minute on variant.com) ([banani.co](https://www.banani.co/blog/variant-ai-review)); critique loops can be their own multi-agent pipelines (UI Critic → Design Strategist → Implementer) ([theunwindai](https://www.theunwindai.com/p/build-an-ai-ui-ux-feedback-agent-team-with-nano-banana)).

**Caveat for this repo:** the screenshot loop works for DOM/HUD, **not the WebGL canvas** — headless capture can't read game-canvas pixels (known project constraint). Canvas-side changes stay verified by code + console errors + eyeballing a real display.

---

## 5. Maintainability: what the evidence says you must design for

- Empirical study, 10 Cursor-generated projects (~17K LoC avg): **91% functional correctness, yet 1,305 CodeScene + 3,193 SonarQube issues** — working AI code systematically carries design debt. `[3-0]` ([arXiv 2604.06373](https://arxiv.org/html/2604.06373v1))
- The prevalent categories: code duplication, high complexity, large methods, framework best-practice violations, exception-handling and **accessibility** issues — violating SRP/SoC/DRY. `[3-0]` (same)
- **Refuted** (1-2): the claim that this debt concentrates disproportionately in frontend code did not survive verification — debt is spread; don't assume the UI layer is uniquely safe or uniquely doomed.
- Extracted (not verified): Anthropic RCT — engineers using AI finished tasks at the same speed but scored 17% lower on follow-up comprehension; "comprehension debt" is the real cost ([addyosmani](https://addyosmani.com/blog/comprehension-debt/)); Osmani's 2026 workflow front-loads AI-assisted **spec → plan → generate** rather than prompt-first coding ([Osmani](https://medium.com/@addyosmani/my-llm-coding-workflow-going-into-2026-52fe1681325e)).

**Implication:** the workflow must include human review gates and a green test suite as a non-negotiable ratchet — exactly the discipline that just caught issue #76 (red test on master for 5 weeks).

---

## 6. Refuted & unverified

**Refuted (do not repeat these):**
- "Bolt produces higher-quality UI than v0/Lovable with fewer iterations" — 0-3.
- "v0 looks better than Lovable but is slower to first version" — 0-3.
- "All leading generators are React-locked with no vanilla path at all" — 0-3 (overstated; Bolt accepts broader JS scaffolds).
- "AI design debt concentrates in frontend code" — 1-2.

**Unverified (all 3 verify votes errored on a provider rate limit; plausible, not confirmed):**
- Claude Code imposes no framework constraints / operates in-repo over plain files.
- v0 = React/Tailwind only (uibakery variant — but the same fact is confirmed 3-0 from two other sources).
- Bolt.new is cloud-only, can't operate on a local repo.
- Flowstep exports React/TS/Tailwind with MCP bridge into Claude Code/Cursor.

These five can be re-verified after the provider quota resets.

---

## 7. Recommended workflow for paper-war

The research describes exactly the pipeline paper-war's gstack suite already assembles. **Adopted workflow** (nothing new to install):

1. **Design exploration — `/design-shotgun`** for multi-variant generation + structured feedback (validated pattern: cheap variant floods + critique beats single-shot prompting). Optionally use v0/Bolt as external sketchpads for look-and-feel only — never paste their code in.
2. **Design context — treat `docs/design-system.md` + ADR-0021 (ui-ux-pro-max) as the always-loaded design-system MCP.** Keep it artifact-derived (tokens, real component inventory, do/don't examples). This is the verified lever against generic output and hard-coded values. Update it when the visual language changes — it is load-bearing for every generation pass.
3. **Generation — Claude Code in-repo** (the only class of tool that writes vanilla JS into an existing codebase). Spec-first for anything non-trivial: `/spec` or a short plan doc before code.
4. **Visual verification loop — `/qa` / `/browse`** on the DOM/HUD layer: screenshot → name the defect → fix → re-screenshot. Console errors are the canvas-side signal (pixel capture from the WebGL canvas doesn't work headless); eyeball shaders on a real display.
5. **Critique gate — `/design-review`** before calling a UI change done (mirrors the multi-agent critique pattern).
6. **Maintainability ratchet — green client suite + `/review` before landing.** The arXiv evidence says the debt will accumulate by default; the tests and review gates are the countermeasure. Watch specifically for the confirmed debt categories: duplicated UI code, oversized render/update functions, a11y regressions.

**Gaps worth closing (optional, in priority order):**
- A short `client/src/README`-style UI conventions file (element patterns, token usage, where HUD code lives) so every generation pass sees the inventory without rediscovering it — the monday.com lesson in repo-local form.
- Re-run verification of the 5 rate-limited claims after quota reset if generator-vs-agent specifics matter for a future decision.

---

## Sources (22 fetched; 6 primary/secondary, 16 blog)

Verified against: [monday.com engineering](https://engineering.monday.com/how-we-use-ai-to-turn-figma-designs-into-production-code/) · [playwright.dev CLI docs](https://playwright.dev/docs/getting-started-cli) · [arXiv 2604.06373](https://arxiv.org/html/2604.06373v1) · [Tweag handbook](https://tweag.github.io/agentic-coding-handbook/WORKFLOW_VISUAL_FEEDBACK/) · [Osmani](https://addyo.substack.com/p/ai-driven-prototyping-v0-bolt-and) · [buildthisnow](https://www.buildthisnow.com/blog/tools/extensions/claude-code-vs-v0) · [makemyprd](https://www.makemyprd.com/compare/ai-app-builder-comparison-2026)
Extracted (not adversarially verified): jeangalea.com · timdeschryver.dev · uxdesign.cc · epam.com · luca-becker.me · builder.io · banani.co · theunwindai.com · medium.muz.li ×2 · medium.com ×2 · addyosmani.com · devdojo.com · uibakery.io
