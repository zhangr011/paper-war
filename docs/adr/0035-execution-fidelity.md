# ADR-0035: Execution fidelity — the reference is authoritative at execution level

- Status: Accepted (2026-09-22); trial verdict appended below
- Context: UI kit work (commits 23c8b45..4db8b6a) extracted design/component.png's palette and components into `--kit-*` tokens correctly — and still shipped UI that reads as "an app wearing paper clothes." A grill session (2026-09-22) diagnosed why: the implementation matched the reference's *tokens* while sanitizing its *physicality* — sub-pixel edge jitter instead of visible rips, 3px shadows instead of weighted depth, invisible rotations instead of tilt, grid spacing instead of overlapping collage. The user's words: "you implement it like an app, not the source file."
- Decision: the UI look is judged by **execution level** fidelity (physicality: rips, folds, weighted shadows, dimensionality), not palette level. "Implemented" means indistinguishable from a crop of the reference at a glance — torn edges that look torn, tape that overhangs and folds, layered hard shadows, dimensional objects, dense overlapping composition. The acceptance test is a side-by-side screenshot against the reference crop (see CONTEXT.md, *Execution Fidelity*).
- Consequences:
  - New UI work is judged by side-by-side comparison first; WCAG/contrast and the ui-verify loop still apply on top.
  - The Figma-authoring proposal (design in Figma first, implement from the file) is **parked** pending a one-panel re-implementation trial at this standard. If agent execution cannot reach it, Figma human authoring is justified with evidence; if it can, the repo-resident loop stands.
  - Existing kit components were built to the old standard and will be re-rolled to this one opportunistically, Selection panel first.

## Trial verdict (2026-09-22): FAIL

One-panel re-implementation at the standard (Selection panel: overhanging
torn tape, layered shadows, rough-ink stamp, baked ink staining) was
judged against a side-by-side with the reference crop — **fail**. The
agent reached the structure and physicality but not indistinguishability.

Reading of the result: part generator-craft, part TRANSPORT — a PNG
reference forces the implementer to *interpret* weights, proportions,
and effects from pixels. A Figma source read through MCP carries exact
fills, strokes, shadows, and rotations (the monday.com/EPAM finding:
metadata access ~93% fidelity vs ~72% screenshot). Consequence: the
parked Figma-authoring decision (option (a)) is **un-parked and
justified**. First question on that branch: whether the original
Figma source of design/component.png exists — if it does, exact-value
implementation via Figma MCP is the immediate path and the trial
re-runs against the same bar.

## Correction (2026-09-22): the reference is an AI-generated concept

`design/component.png` is itself AI-generated; there is no layered
design source behind it (no Figma file — confirmed). It is a
**moodboard/concept, not an authoritative spec**: its "exact values"
exist only as pixels, and it cannot be sliced into assets (rejected —
it would bake image-gen artifacts into the codebase). Execution
fidelity therefore applies to whatever design source is ADOPTED, not
to this PNG. The open question becomes: where does a game-grade
design source come from, given no designer and no source file?

## Decision (2026-09-29): Figma becomes the design source

Grill resolved. The user authors the design source in Figma (option A):
rebuild the kit surfaces using the AI concept PNG as a moodboard, not a
spec. Implementation transport is **Figma MCP** — exact fills, strokes,
effects, and geometry, closing the interpretation gap that failed the
PNG trial. Acceptance stays the side-by-side execution-fidelity bar.

Rollout protocol: ONE panel first (Selection) — user authors (~an
hour), agent implements from the file's exact node values, same bar.
Pass → roll across the kit and this ADR reaches its final form. Fail →
the transport hypothesis is falsified too, and the remaining options
are human-authored CSS by the user or commissioning art.
