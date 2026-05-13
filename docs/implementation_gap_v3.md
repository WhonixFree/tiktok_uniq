# Implementation Gap Audit for PROJECT_SPEC_FOR_CODEX_3

Audit date: 2026-05-13 (refreshed in full compliance run).

This document records the current implementation status against `PROJECT_SPEC_FOR_CODEX_3.md`. Status values are:

- `done` — implemented in runtime code and covered by focused tests or e2e coverage.
- `partial` — recipe/config/test support exists, but runtime behavior is incomplete or approximate.
- `missing` — no meaningful implementation was found.

## Verification commands (this audit run)

- `go test ./...` — passed.
- Static-analysis/lint config scan (`rg` for `.golangci*`, `staticcheck`, `revive`, `Makefile`/`Taskfile`/`justfile`, `.github` lint jobs) — no dedicated linter setup detected in repository tree; verification remains limited to Go test/compile coverage.

| Requirement | Status | Notes | Proposed implementation |
|---|---|---|---|
| 1. CLI batch processing from `input/` to `output/` | done | Scanner discovers supported media, jobs write `<basename>_processed.mp4`, worker e2e covers a two-file batch. | Maintain e2e coverage for audio+video and video-only inputs. |
| 2. Architecture roles (Go orchestrator, FFmpeg/ffprobe/ExifTool/Python) | done | Go orchestrates scanning, worker jobs, recipe generation, FFmpeg rendering, ffprobe probing/validation, and ExifTool metadata. Python is required at startup and used for mandatory audio speed processing (`python/audio_speed.py`). | Keep Python runtime checks aligned with mandatory subprocess dependencies. |
| 3.1.1 Startup check for `ffmpeg`/`ffprobe`/`exiftool`/`python3` | done | `runStartupChecks` iterates mandatory dependency definitions and fails startup on missing required tools; unit tests cover the required list and missing-tool diagnostics. | Keep fail-fast behavior and actionable install hints. |
| 3.1.2 Per-file recipe generation | done | Worker probing builds a per-job config, generates a recipe, writes `logs/<basename>.recipe.json`, and e2e asserts recipe artifacts. | Keep schema backward-readable while adding future mandatory fields. |
| 3.1.3 Tmp cleanup policy | done | Worker creates `tmp/job-<id>-<basename>/`, renders into it, deletes it after successful metadata finalization, and preserves it on render failure; tests cover both success and failure. | Keep all future intermediate artifacts inside the per-job tmp directory. |
| 3.1.4 Metadata full mode clean + diversify | done | Recipe always records `clean_diversify`; metadata runner copies rendered artifact to final output, runs ExifTool clean (`-all=`), then writes diversify tags. | Maintain command ordering and tag allowlist safety if policy support is expanded. |
| 3.1.5 Color transformation | done | Render pipeline always applies an `eq` color transform in the geometry/color stage. | Optional preset-file loading can be added later, but is not required for the v3 mandatory transform. |
| 3.1.6 Stream overlay | done | Overlay discovery/selection is planned in recipe and runtime renders stream overlay from the selected discovered media asset (`rec.StreamOverlay.Path`) as a dedicated FFmpeg input in the final overlay stage. | Maintain deterministic overlay selection and runtime mapping coverage. |
| 3.1.7 Audio speed: base offset + sine modulation | done | Python WAV processor applies base offset, sine modulation, micro perturbations, and short freeze repeats; render integrates extracted audio -> python processor -> mux flow. | Keep sync-safe target-duration enforcement and event smoothing tests. |
| 3.1.8 Video speed: base offset + sine modulation | done | Go builds piecewise speed segments from base + sine (+micro events), and FFmpeg renders via `trim` + `setpts` + `concat`. | Maintain deterministic plan coverage and bounds checks. |
| 3.1.9 Freeze events, exactly 1 frame | done | Recipe planner generates 1-frame freeze events and runtime applies them as one-frame temporal insertions using piecewise trim/concat with a cloned frame segment (`tpad=stop_mode=clone:stop=1`). | Keep frame-exact tests and concat timeline assertions. |
| 3.1.10 Replace events, exactly 1 frame + unique donors | done | Recipe planner selects donor stream from recursive `--stream-overlay-dir` assets distinct from overlay/main, assigns unique valid donor frames and tmp PNG paths, render extracts one PNG per event, and temporal stage applies one-frame piecewise replacements before overlay. | Maintain focused planner/render determinism coverage. |
| 3.1.11 Pixel replacement neighbor-duplication | done | Render applies weak blur and then area-limited sparse neighbor-duplication per frame using deterministic per-pixel selection and neighbor sampling in filtergraph (`geq`), honoring recipe percent/area/offset parameters. | Keep focused filtergraph tests for sparse replacement semantics. |
| 4. Startup check behavior on missing dependencies | done | Missing required tools produce clear errors with install hints and stop startup before scanning/processing. | No change required. |
| 5. Input formats `.mp4`, `.mov`, `.mkv`; `.webm` optional | done | Scanner supports required formats and keeps optional `.webm`. | No change required unless project scope later narrows formats. |
| 5. Output naming `*_processed.mp4` | done | Job output paths are built as `<basename>_processed.mp4`. | No change required. |
| 5. Runtime dirs `tmp/` and `logs/` | done | Startup creates output/tmp/log dirs; worker writes per-file recipes/logs under `logs/` and uses per-job tmp dirs. | No change required. |
| 6.1.1 Base speed offset | done | Recipe chooses signed base percentages for audio and video; runtime applies via python audio processor and piecewise video plan. | No change required. |
| 6.1.2 Sine mode `lock|independent` | done | Config validates mode; recipe lock mirrors audio/video sine params, independent draws separately; both are consumed by python audio + Go video planning. | Keep tests for both modes and deterministic seed behavior. |
| 6.2 Freeze random count and min distance | done | Count/random/min-distance are planned deterministically and accepted events are realized in runtime temporal concat rendering. | Keep coordination+render tests for spacing and frame accuracy. |
| 6.3 Replace random count, min distance, unique donor | done | Recipe-side count/random/min-distance generation uses external donor streams, enforces unique donor frames, records effective counts when constraints reduce/disable replace, and runtime applies donor frames via concat-based replacement. | Keep donor/overlay/main distinctness and determinism tests. |
| 7. Weak Gaussian blur before pixel replacement | done | FFmpeg filter graph places `gblur` immediately before pixel replacement stage; tests assert this order. | Preserve blur-before-pixel invariant. |
| 7.1 Area modes `edge` and `smart` | done | Edge mode chooses edge bands; smart mode uses analyzer interface/FFmpeg analyzer and falls back to edge on errors or low confidence; tests cover both paths. | Tune scoring only if future visual QA requires it. |
| 8. Randomization constraints: seed, min distances, range validation | done | Recipe generation uses deterministic seeded RNG; temporal planner enforces cross-family min distances; config validation checks ranges, modes, counts, percentages, metadata mode. | Consider warning on impossible count/min-distance combinations causing reductions. |
| 9. Expanded recipe schema | done | Recipe includes base+sine, `av_sine_mode`, micro/freeze/replace events, global temporal summary with hardness/min distance/seed lineage, requested/effective/dropped stats and drop reasons, replace donor instructions, pixel params, and metadata mode. | Keep backward-readable fields while extending future mandatory blocks. |
| 10.1 Crop validation only when crop enabled | done | Config validation gates crop-percent checks behind `CropEnabled`; tests cover disabled and enabled behavior. | No change required. |
| 10.2 Crop percent math `ratio = percent / 100.0` | done | Render crop math uses `percent / 100.0`; tests assert expected behavior. | No change required. |
| 10.3 Crop position flag removal | done | No `CropPosition` config field or `--crop-position` flag remains; tests assert old flag is rejected. | No change required. |
| 10.4 Stream overlay scanning recursive | done | `overlay.Discover` uses recursive walk with nested coverage tests. | Wire discovered overlay assets into render runtime to close stream-overlay partial status. |
| 10.5 ffprobe audio permissiveness | done | ffprobe parsing requires video but allows missing audio; render maps audio only when present; e2e covers video-only processing. | No change required. |
| 11. Recommended effect order | done | Pipeline and tests enforce geometry/color → weak blur → pixel replacement → temporal speed/freeze/replace → overlay → encode/mux. | Preserve stage ordering invariants in filtergraph tests. |
| 12. Worker pipeline target behavior | done | E2E covers scan → worker pool → probe → recipe JSON → render/validation → metadata full mode → per-file logs/recipes → success tmp cleanup. | Maintain e2e as worker contract. |
| 13. Backward compatibility: keep `PROJECT_SPEC_FOR_CODEX_2.md` | done | `PROJECT_SPEC_FOR_CODEX_2.md` remains in the repository. | Preserve this file for historical reference. |

## Critical gaps summary

No critical v3 mandatory gaps were identified in this audit run.