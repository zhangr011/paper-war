# ADR-0035: Execution fidelity — the reference is authoritative at execution level

- Status: Accepted (2026-09-22)
- Context: UI kit work (commits 23c8b45..4db8b6a) extracted design/component.png's palette and components into `--kit-*` tokens correctly — and still shipped UI that reads as "an app wearing paper clothes." A grill session (2026-09-22) diagnosed why: the implementation matched the reference's *tokens* while sanitizing its *physicality* — sub-pixel edge jitter instead of visible rips, 3px shadows instead of weighted depth, invisible rotations instead of tilt, grid spacing instead of overlapping collage. The user's words: "you implement it like an app, not the source file."
- Decision: `design/component.png` is authoritative at **execution level**, not palette level. "Implemented" means indistinguishable from a crop of the reference at a glance — torn edges that look torn, tape that overhangs and folds, layered hard shadows, dimensional objects, dense overlapping composition. The acceptance test is a side-by-side screenshot against the reference crop (see CONTEXT.md, *Execution Fidelity*).
- Consequences:
  - New UI work is judged by side-by-side comparison first; WCAG/contrast and the ui-verify loop still apply on top.
  - The Figma-authoring proposal (design in Figma first, implement from the file) is **parked** pending a one-panel re-implementation trial at this standard. If agent execution cannot reach it, Figma human authoring is justified with evidence; if it can, the repo-resident loop stands.
  - Existing kit components were built to the old standard and will be re-rolled to this one opportunistically, Selection panel first.
