# Implementation Gap Audit for PROJECT_SPEC_FOR_CODEX_3

| Requirement | Status | Notes | Proposed implementation |
|---|---|---|---|
| 1. CLI batch processing from `input/` to `output/` | partial | CLI, scanner, worker pool exist; processing handler is stub (`DefaultHandler`) and does not run FFmpeg pipeline. | Implement real render handler: probe → recipe → ffmpeg pipeline → metadata stage → logs/recipe output. |
| 2. Architecture roles (Go orchestrator, FFmpeg/ffprobe/ExifTool/Python) | partial | Go orchestrator + ffprobe checks are present; no actual FFmpeg render stage, ExifTool execution, or Python helper subprocess integration in runtime pipeline. | Add pipeline execution layers for FFmpeg, ExifTool, and optional/required Python helper calls. |
| 3.1.1 Startup check for dependencies is mandatory | done | `runStartupChecks` verifies `ffmpeg`, `ffprobe`, `exiftool`, `python3` before run; startup-check behavior is now covered by unit tests in `cmd/videobatch/main_test.go` (commit: this-step). | Keep mandatory checks and expand diagnostics with per-platform install hints if needed. |
| 3.1.2 Per-file recipe generation mandatory | partial | `recipe.Generate` exists but is not wired into worker execution; recipe artifacts are not written per file to `logs/`. | Call recipe generation per job and persist JSON recipe into `logs/` with deterministic naming. |
| 3.1.3 Tmp cleanup policy mandatory (success cleanup, failure preserve) | missing | No per-file tmp lifecycle in worker handler; `--cleanup` flag exists but pipeline does not use it. | Implement per-job tmp workspace and cleanup policy tied to job result status. |
| 3.1.4 Metadata full mode (clean + diversify) mandatory | missing | `metadata-policy` config exists but no ExifTool execution or diversify logic. | Add metadata module with two explicit phases: clean fields + randomized diversify fields. |
| 3.1.5 Color transform mandatory | partial | Color parameters are generated in recipe, but no FFmpeg filter application in runtime. | Apply selected color preset + strength in filter graph unconditionally. |
| 3.1.6 Stream overlay mandatory | partial | Overlay file is selected in recipe, but no render integration; current scan is non-recursive glob only. | Apply overlay filter in render stage and switch file discovery to recursive traversal. |
| 3.1.7 Audio speed: base + sine mandatory | missing | Only `TemporalShift` scalar exists; no audio base randomization/sine modulation model. | Extend recipe with audio base speed + sine params and generate time-varying audio tempo chain. |
| 3.1.8 Video speed: base + sine mandatory | missing | No video speed modulation model implemented. | Add video speed model with base offset + sine component and FFmpeg realization. |
| 3.1.9 Freeze micro-events (1 frame) mandatory | missing | No freeze events generated or applied. | Generate 1-frame freeze event timeline with spacing validation; apply via filter/segment assembly. |
| 3.1.10 Replace micro-events (1 frame, donor from other stream source) mandatory | missing | No replace events pipeline or donor frame sourcing. | Build donor frame extraction pool and unique one-frame replacement map per video. |
| 3.1.11 Pixel replacement neighbor-duplication mandatory | missing | No pixel replacement stage implemented. | Add per-frame area-limited pixel replacement stage (likely Python/OpenCV helper or FFmpeg+custom filter path). |
| 4. Startup check behavior on missing dependencies | done | Mandatory dependency failures stop startup with explicit error; test coverage added for missing required dependency failure path (commit: this-step). | Maintain fail-fast behavior; optionally aggregate actionable install commands. |
| 5. Input formats `.mp4/.mov/.mkv` supported; `.webm` optional | partial | Scanner supports required formats and `.webm`; acceptable but spec allows dropping webm. | Decide policy: keep `.webm` as optional support or remove to narrow scope. |
| 5. Output naming `*_processed.mp4` | done | Job output path built as `<basename>_processed.mp4`. | No change required. |
| 5. Tmp artifacts in `tmp/` | partial | `tmp` directory is created globally, but no per-file tmp artifacts flow yet. | Implement per-file temp workspace under `tmp/<job-id|basename>/`. |
| 5. Per-file logs and recipes in `logs/` | partial | Logger writes app logs; per-file recipe/log artifacts are not emitted. | Emit per-job structured log and recipe file. |
| 6.1 Two-layer speed model for audio/video | missing | Not represented in config/recipe/runtime. | Add explicit fields for base speed ranges and sine ranges for both streams. |
| 6.1.1 Base speed sign randomization and percent range | missing | No such randomization exists. | Add range flags + recipe randomizer (speed-up/slow-down sign + magnitude). |
| 6.1.2 Sine mode `lock|independent` | missing | Absent from config and recipe. | Add `av-sine-mode` enum and synchronized/independent parameter generation. |
| 6.2 Freeze events random+distance constraints | missing | No temporal event planner. | Implement event planner with min-distance validation and duration-proportional counts. |
| 6.3 Replace events random+distance+unique donor constraints | missing | No replace event planner or uniqueness guarantees. | Add donor frame catalog and uniqueness-enforced assignment. |
| 7. Weak Gaussian blur before pixel replacement | missing | No blur/pixel stage exists. | Add blur params (min/max) and enforce ordering before pixel replacement. |
| 7.1 Area modes `edge` and `smart` | missing | No area-selection subsystem. | Implement mode enum + smart analyzer helper (Python module) + edge heuristics. |
| 8. Randomization constraints: seed reproducibility, distances, range validation | partial | Seed exists and recipe uses RNG; many required ranges/distances are absent. | Expand config validation and central randomization planner for all temporal/pixel params. |
| 9. Expanded recipe schema for temporal/pixel/metadata full mode | missing | Current recipe lacks required V3 fields (base/sine mode/events/pixel block/metadata full). | Redesign recipe struct/schema; serialize full per-file recipe JSON into logs. |
| 10.1 Crop validation only when crop enabled | missing | Validation currently always requires crop percent >0 and crop-position enum even if `--crop=false`. | Make crop-specific validation conditional on `CropEnabled`. |
| 10.2 Crop percent math `ratio = percent / 100.0` | missing | Current crop uses raw percent as multiplier (`width * percent`) causing invalid sizes. | Fix crop math and clamp resulting geometry. |
| 10.3 Remove `--crop-position` if only random-area used | partial | Flag exists and currently unused in recipe crop placement (always random). | Either implement position modes or remove flag per final design. |
| 10.4 Stream overlay scanning recursive | missing | Overlay discovery uses non-recursive `filepath.Glob` only top-level patterns. | Replace with recursive walk and extension filtering. |
| 10.5 ffprobe audio permissive (video-only valid) | missing | `Probe` currently errors when audio stream absent (`res.Audio == nil`). | Accept files with video stream only; adapt downstream audio stages accordingly. |
| 11. Recommended effect order | missing | No actual render pipeline yet to enforce order. | Implement pipeline orchestration respecting geometry/color → blur → pixel → temporal → overlay → encode. |
| 12. Worker pipeline target behavior end-to-end | partial | Scan + worker pool exist; probe/generate/render/metadata/logging/cleanup stages are not integrated. | Build staged job handler with explicit step transitions and error handling. |
| 13. Keep `PROJECT_SPEC_FOR_CODEX_2.md` in repo | done | V2 spec file is present alongside V3. | No action required. |

