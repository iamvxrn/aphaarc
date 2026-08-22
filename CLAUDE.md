# AlphaARC — context for Claude

A Go cognitive architecture that solves **ARC-AGI-3** (interactive games at three.arcprize.org) via an **intrinsic compression drive — no external reward**. The goal emerges as "the shortest description of the world." Built solo by a 14-year-old aiming at a *meta-algorithm for AGI* (brain-principles-in-silicon), not a per-game solver. Module: `alphaarc`.

## Golden rules
- **NEVER `git push`** (the user pushes). I **do commit when asked**; end commit messages with `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.
- **Go toolchain lives at `/run/host/usr/lib/go/bin/go`** (plain `go` is not on PATH). Always build/test/vet before committing.
- **The ARC-AGI-3 API is FREE** — iterate freely, a run is ~2 min. To run games, load the key inline and **never print it**:
  `export ARC_API_KEY=$(grep '^ARC_API_KEY=' .env | cut -d= -f2-)`
  Then e.g. `go run ./cmd/alphaarc-play -game vc33-5430563c -actions 150 -maxblobs 20 &> play.log`. `.env` is gitignored + claudeignored — never expose or commit the key.
- **Honest calibration over hype.** 100% ARC is the unsolved AGI frontier — do NOT stake the user's income on it; keep the ARC dream as science. Scaffold reasoning Socratically. When pronouns are unknown, use they/them.

## The compression stack
- **Primitives (`pkg/macro/mdl.go` + `residual.go`, `correspondence.go`)**: structural MDL over objects/topology (NOT bytes). Each returns *savings* = bits a regularity explains. `BestPrimitive` = max-saving per grid; **no goal is hand-picked — it wins by bits.** Primitives: `Reflect`, `Translate`, `Count`, `Correspondence` (relational: A = copy of B up to Identity/ColorSwap/Reflect + residual). Every primitive is guarded against its degenerate cheat (blank / flood / scatter / solid).
- **`pkg/macro/drive.go`** — `DrivePreference` / `DriveScore` = the intrinsic goal signal.
- **Residual = attention (`residual.go`)** — `ResidualTargets` returns the cells that BREAK the best regularity (the anomaly), which is usually the interactive control.
- **Live agent `cmd/alphaarc-play/main.go`** — perceive → residual/object click candidates → **MODEL-FREE reinforcement**: `driveGain[token]` = EMA of the *observed* compression delta per click (`driveGainWeight`). The affordance *model* can't learn big / absolute / same-colour-colliding button effects, so we reinforce on observed ΔL instead of predicting. Cortex pixel-repaint macros are **disabled** (a Floor-2 static-ARC tool, wrong for interactive games). On a cleared level, stagnation **persists** instead of rewinding the whole game.
- **`cmd/probe/main.go`** — free-API diagnostic (`GAME=<id>` env): renders a grid, click-sweeps to find interactive cells, reports per-primitive deltas (`STEP=4|8` sweep resolution) or exact-coordinate breakdowns (`POINTS=x,y;x,y`). This is how we reverse-engineer a game's mechanic. Throwaway/experimental.

## Status (2026-08-22, calibration correction same day — see below)
- **FIRST live ARC-AGI-3 wins by pure intrinsic drive** — L1 of **vc33, lp85, r11l** (3 of **6**, not 7 — su15/lf52 were miscategorized as pure-click; they have extra actions), competitive with or beating baseline.
- **Correction, same session**: the "no per-game tuning" claim overclaimed. Several core hyperparameters (`driveGainWeight=0.02`, `refractorySteps=4`, the maxChangeCells cascade guard, GAME_OVER/reset handling) were hand-calibrated by reading **vc33's own probe numbers** in an earlier session — not derived task-agnostically. A genuinely untouched 6th pure-click game, **ft09-0d8bbf25** (never probed/rendered before), was found and blind-tested cold with zero prior calibration: **0/150, L1 never completed**. So: the *primitives* (Reflect/Translate/Count/Correspondence) are honestly game-agnostic; the *action-selection hyperparameters* are currently fit to one game and have NOT been shown to transfer blind. Say "3 of 6, hyperparameters tuned against one of them" — not "generalizes."
- **Correspondence** primitive is built + offline-proven (6 tests) but does **not yet** crack the 4 hard games (**s5i5, tn36, su15, lf52**). Those are RELATIONAL (template/legend matching) — anti-compression to self-regularity.
- **Aggregation fix DONE (commits 606f770 + 4b2f6f3):** model-free reinforcement now scores each click by `macro.BestPrimitiveDelta` — the SIGNED delta of whichever primitive moved most by absolute value (not the old `DrivePreference`-of-`DrivePreference`, and not a plain max-of-deltas either — that has its own floor-at-zero bug, see `TestAggregation_WorseningClickIsPunishedNotFloored`). Regression-tested live: vc33 solves L1 in ~9 actions again, matching pre-fix speed. This wall is CLOSED for the general mechanism, even though it didn't (yet) crack the hard games itself.
- **tn36's wall is NOT aggregation** (new finding): a fine probe sweep (STEP=4, 256 points) found `bestCorrespondenceDelta=+0` everywhere, even the *unmasked* direct Correspondence delta. Correspondence (built for same-shape template *pairs*) likely just doesn't recognize tn36's legend-to-checkerboard relationship at all — a representational gap, not a masking one.
- **Scale (s5i5-specific, still open):** its two template boxes are different sizes; Correspondence V1 deliberately excludes scale.

## Immediate next task
Aggregation is fixed and verified; it was necessary but **not sufficient** for the 4 hard games — s5i5 needs scale, tn36 needs Correspondence (or a new primitive) to recognize legend/exemplar-style relations, not just paired-region matches. Pick one: (a) generalize Correspondence to legend matching (one small reference region taught once, many instances elsewhere) for tn36; (b) add scale-invariant matching for s5i5. Also worth doing before either: regression-check lp85/r11l (only vc33 was re-verified live after the reinforcement changes). See `project_protaxon_resume.md` for the full trace, including the exact live numbers and the mechanistic root cause of the sign bug found along the way.

## Deep running log
Full history, decisions, and the honest-stakes stance live in the auto-memory, now under the **AlphaARC** project path (auto-loads when the session's working dir is this repo):
`/home/aiden/.claude/projects/-home-aiden-extra-git-AlphaARC/memory/` — start with `project_protaxon_resume.md`. (A stale copy remains under the `-Protaxon` project path as backup.) Note: the *codebase* named Protaxon is the older line; **AlphaARC is canonical** — do not sync from it.
