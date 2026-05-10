# videobatch

`videobatch` is a local CLI pipeline for batch-processing your own video files from an input directory into processed MP4 outputs. It is designed as a Go orchestrator around FFmpeg, ffprobe, ExifTool, and Python startup checks, not as a GUI editor or a publishing/account automation tool.

The current implementation targets [`PROJECT_SPEC_FOR_CODEX_3.md`](PROJECT_SPEC_FOR_CODEX_3.md). The previous [`PROJECT_SPEC_FOR_CODEX_2.md`](PROJECT_SPEC_FOR_CODEX_2.md) is intentionally kept in the repository as migration/reference material.

## What this project does not do

`videobatch` does **not**:

- publish videos to TikTok or any other platform;
- manage accounts;
- automate browsers;
- bypass platform restrictions.

## Requirements

Install the runtime tools and make sure they are available in `PATH` before running the pipeline:

- `ffmpeg`
- `ffprobe`
- `exiftool`
- `python3`

Startup checks are mandatory. If any required executable is missing, the pipeline exits before scanning or processing input files. Python package checks are conditional for optional Python-backed modes such as automatic captions or Python audio envelopes.

## Input, output, and runtime folders

Default folders:

- `./input` — source videos.
- `./output` — processed files named `<basename>_processed.mp4`.
- `./tmp` — per-job temporary folders.
- `./logs` — JSON recipe artifacts and per-job logs.

Supported input formats currently include `.mp4`, `.mov`, `.mkv`, and optional `.webm` support.

Temporary cleanup policy:

- successful jobs remove their per-job `tmp/job-<id>-<basename>/` directory when `--cleanup=true`;
- failed jobs preserve their per-job temporary files for debugging.

## Build

```bash
go build ./cmd/videobatch
```

This produces a local `videobatch` binary when run from the repository root.

## Basic usage examples

### Dry-run the default input folder

```bash
go run ./cmd/videobatch --dry-run
```

### Process `./input` into `./output`

```bash
go run ./cmd/videobatch \
  --input ./input \
  --output ./output \
  --tmp ./tmp \
  --logs ./logs
```

### Recursively scan input and use two workers

```bash
go run ./cmd/videobatch \
  --input ./input \
  --output ./output \
  --recursive \
  --jobs 2 \
  --threads-per-job 4
```

### Reproducible run with a fixed seed

```bash
go run ./cmd/videobatch \
  --input ./input \
  --output ./output \
  --seed 12345 \
  --overwrite
```

### Smart pixel area mode

```bash
go run ./cmd/videobatch \
  --input ./input \
  --output ./output \
  --pixel-replace-mode smart \
  --pixel-smart-grid 8
```

Smart mode analyzes sampled frames to choose a low-motion, background-like replacement area. If analysis fails or confidence is too low, recipe generation falls back to edge mode.

### Optional crop example

```bash
go run ./cmd/videobatch \
  --input ./input \
  --output ./output \
  --crop \
  --crop-min-percent 1.5 \
  --crop-max-percent 3.0
```

Crop validation is active only when `--crop` is set.

### Optional Python-backed captions check

```bash
go run ./cmd/videobatch \
  --input ./input \
  --output ./output \
  --captions auto \
  --caption-language auto \
  --caption-model base
```

When `--captions auto` is selected, startup checks also verify the required Python packages for captions.

## Mandatory spec v3 blocks

Spec v3 treats these blocks as mandatory and enabled by default. They do not have boolean enable/disable flags; only parameter flags or internal defaults are allowed.

| Block | Current implementation status |
|---|---|
| Startup dependency check | Implemented: checks `ffmpeg`, `ffprobe`, `exiftool`, and `python3` before processing. |
| Per-file recipe generation | Implemented: each job writes `logs/<basename>.recipe.json`. |
| Per-file temporary cleanup policy | Implemented: successful jobs clean temporary files; failed jobs preserve them. |
| Metadata full mode | Implemented: ExifTool clean + deterministic diversify phases. |
| Color transform | Partially implemented: unconditional render color stage exists; external color preset loading is still pending. |
| Stream overlay | Partially implemented: final overlay render stage exists; asset-backed overlay selection is still pending. |
| Audio speed base + sine | Partially implemented: recipe has base+sine fields and runtime applies base speed; runtime sine modulation is pending. |
| Video speed base + sine | Partially implemented: recipe has base+sine fields and runtime applies base speed; runtime sine modulation is pending. |
| Freeze micro-events | Partially implemented across recipe planning: one-frame events are generated with spacing constraints; render-time realization is pending. |
| Replace micro-events | Partially implemented across recipe planning: one-frame events and donor constraints are generated; donor-frame render-time replacement is pending. |
| Pixel replacement | Partially implemented: area-limited neighbor-patch replacement runs after weak blur; exact sparse per-pixel density semantics can still be refined. |

## Optional blocks

These features remain optional under spec v3 and are controlled by explicit flags:

- trim: `--trim`, `--trim-start-min`, `--trim-start-max`, `--trim-end-min`, `--trim-end-max`;
- crop: `--crop`, `--crop-min-percent`, `--crop-max-percent`;
- captions: `--captions off|auto`, caption template/language/model flags;
- Python audio envelope: `--audio-envelope off|python`, `--audio-envelope-config`;
- background music and ducking: `--music`, `--music-dir`, `--music-volume`, `--music-ducking`, `--ducking-mod`.

## Frequently used parameters

| Flag | Default | Purpose |
|---|---:|---|
| `--input` | `./input` | Input directory. |
| `--output` | `./output` | Output directory. |
| `--tmp` | `./tmp` | Temporary files directory. |
| `--logs` | `./logs` | Logs and recipe directory. |
| `--recursive` | `false` | Recursively scan input. |
| `--jobs` | `1` | Number of parallel jobs. |
| `--threads-per-job` | CPU count | FFmpeg threads available per job. |
| `--seed` | `0` | Deterministic seed for recipe randomization. |
| `--dry-run` | `false` | Scan and plan without rendering. |
| `--overwrite` | `false` | Allow replacing existing outputs. |
| `--cleanup` | `true` | Remove successful per-job temporary directories. |
| `--av-sine-mode` | `lock` | `lock` mirrors audio sine params to video; `independent` draws separately. |
| `--pixel-replace-mode` | `edge` | Pixel area mode: `edge` or `smart`. |
| `--metadata-full-mode` | `clean_diversify` | Required metadata mode. |

Run `go run ./cmd/videobatch -h` for the complete flag list.

## Migration notes: spec v2 to spec v3

When moving from spec v2 assumptions to spec v3 behavior, keep these changes in mind:

1. Mandatory effects are no longer toggleable feature blocks. Startup checks, per-file recipes, tmp cleanup, full metadata processing, color, overlay, speed adjustments, freeze/replace planning, and pixel replacement are expected to be part of the default pipeline.
2. Metadata mode is full mode: clean first, then diversify. Code and recipes should assume `clean_diversify`, not clean-only metadata handling.
3. Recipes are the source of per-file randomized decisions. Seeded runs should remain reproducible for recipe-level randomization.
4. Freeze and replace events are one-frame micro-events with minimum-distance constraints in recipe planning.
5. Pixel replacement must occur after weak Gaussian blur and before temporal/overlay stages in the render order.
6. Crop is optional and validated only when enabled. The removed `--crop-position` flag should not be used by spec v3 commands.
7. Video-only inputs are valid when they contain a video stream; audio filters are conditional on source audio presence.
8. `PROJECT_SPEC_FOR_CODEX_2.md` remains in the repository for historical reference; do not delete it during v3 work.

## Known limitations and technical debt

- Runtime sine modulation for audio and video is still pending; only base speed adjustment is applied at render time.
- Freeze and replace recipe events are planned, but full render-time one-frame freeze/replace realization is pending.
- Replace events still need donor-frame extraction and application from another stream source.
- Color preset file loading is pending beyond the current unconditional color stage.
- Stream overlay discovery exists in scanner utilities, but the render path still needs asset-backed overlay selection instead of the current synthetic overlay source.
- Pixel replacement currently uses an area-limited alpha-blended neighbor patch; exact sparse replacement density matching `pixel_replace_percent` remains technical debt.
- Optional Python helper subprocess integration is limited to startup/package checks for selected modes; Python is not yet a broad mandatory processing component.
- `.webm` remains supported as an optional input format even though spec v3 only requires `.mp4`, `.mov`, and `.mkv`.

## Development checks

Recommended final checks before opening a PR:

```bash
go test ./...
```

```bash
go run ./cmd/videobatch --dry-run
```
