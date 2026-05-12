package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"videobatch/internal/config"
	"videobatch/internal/ffprobe"
	"videobatch/internal/pixel"
	"videobatch/internal/recipe"
	"videobatch/internal/scanner"
	"videobatch/internal/workerpool"
)

func TestStartupDependenciesContainRequiredTools(t *testing.T) {
	t.Parallel()

	deps := startupDependencies(config.Config{})
	want := map[string]bool{
		"ffmpeg":   false,
		"ffprobe":  false,
		"exiftool": false,
		"python3":  false,
	}

	for _, dep := range deps {
		if _, ok := want[dep.Name]; ok {
			want[dep.Name] = true
			if !dep.Required {
				t.Fatalf("dependency %s must be required", dep.Name)
			}
			if strings.TrimSpace(dep.InstallHint) == "" {
				t.Fatalf("dependency %s must include install hint", dep.Name)
			}
		}
	}

	for name, present := range want {
		if !present {
			t.Fatalf("required startup dependency %s not found", name)
		}
	}
}

func TestCheckExecutableMissingDependencyIncludesInstallHint(t *testing.T) {
	t.Parallel()

	err := checkExecutable(context.Background(), dependency{
		Name:        "fake-tool",
		Command:     "tool-that-does-not-exist-12345",
		Args:        []string{"--version"},
		Required:    true,
		InstallHint: "Install fake-tool from vendor package manager.",
	})
	if err == nil {
		t.Fatal("expected missing dependency error")
	}

	msg := err.Error()
	if !strings.Contains(msg, "fake-tool not found") {
		t.Fatalf("expected missing-tool marker in error, got: %s", msg)
	}
	if !strings.Contains(msg, "Install fake-tool") {
		t.Fatalf("expected install hint in error, got: %s", msg)
	}
}

func TestRunStartupChecksReturnsErrorWhenRequiredDependencyMissing(t *testing.T) {
	t.Parallel()

	cfg := config.Config{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	original := startupDependencyProvider
	startupDependencyProvider = func(config.Config) []dependency {
		return []dependency{{
			Name:        "missing-tool",
			Command:     "tool-that-does-not-exist-12345",
			Args:        []string{"--version"},
			Required:    true,
			InstallHint: "Install missing-tool manually.",
		}}
	}
	t.Cleanup(func() {
		startupDependencyProvider = original
	})

	err := runStartupChecks(ctx, cfg, logger)
	if err == nil {
		t.Fatal("expected startup check to fail")
	}
	if !strings.Contains(err.Error(), "missing-tool not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWorkerPipelineE2EProcessesBatchIncludingVideoOnly(t *testing.T) {
	dir := t.TempDir()
	inputDir := filepath.Join(dir, "input")
	outputDir := filepath.Join(dir, "output")
	tmpDir := filepath.Join(dir, "tmp")
	logsDir := filepath.Join(dir, "logs")
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"clip_audio.mp4", "clip_silent.mp4", "ignore.txt"} {
		if err := os.WriteFile(filepath.Join(inputDir, name), []byte("input-"+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ffmpegLog := filepath.Join(dir, "ffmpeg.args")
	exiftoolLog := filepath.Join(dir, "exiftool.args")
	writeFakeTool(t, filepath.Join(binDir, "ffprobe"), `#!/usr/bin/env bash
set -euo pipefail
target="${@: -1}"
if [[ "$target" == *silent* ]]; then
  cat <<'JSON'
{"format":{"duration":"1.000000","bit_rate":"100000","filename":"silent"},"streams":[{"codec_type":"video","codec_name":"h264","profile":"High","width":64,"height":64,"r_frame_rate":"25/1","duration":"1.000000"}]}
JSON
else
  cat <<'JSON'
{"format":{"duration":"1.000000","bit_rate":"100000","filename":"audio"},"streams":[{"codec_type":"video","codec_name":"h264","profile":"High","width":64,"height":64,"r_frame_rate":"25/1","duration":"1.000000"},{"codec_type":"audio","codec_name":"aac","sample_rate":"44100","channels":2,"duration":"1.000000"}]}
JSON
fi
`)
	writeFakeTool(t, filepath.Join(binDir, "ffmpeg"), `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$FAKE_FFMPEG_LOG"
out="${@: -1}"
mkdir -p "$(dirname "$out")"
printf 'fake-render:%s\n' "$*" > "$out"
`)
	writeFakeTool(t, filepath.Join(binDir, "exiftool"), `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "-ver" ]]; then
  echo "12.00"
  exit 0
fi
printf '%s\n' "$*" >> "$FAKE_EXIFTOOL_LOG"
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_FFMPEG_LOG", ffmpegLog)
	t.Setenv("FAKE_EXIFTOOL_LOG", exiftoolLog)

	cfg, err := config.ParseFlags([]string{
		"--input", inputDir,
		"--output", outputDir,
		"--tmp", tmpDir,
		"--logs", logsDir,
		"--jobs", "2",
		"--threads-per-job", "1",
		"--overwrite",
		"--seed", "99",
		"--codec-profile", "fast",
	}, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureRuntimeDirs(cfg.OutputDir, cfg.TmpDir, cfg.LogsDir); err != nil {
		t.Fatal(err)
	}

	files, err := scanner.Scan(cfg.InputDir, cfg.Recursive)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("expected scanner to find 2 supported media files, got %d: %v", len(files), files)
	}

	jobsList := buildJobs(files, cfg.OutputDir, cfg.LogsDir)
	jobs := make(chan workerpool.Job)
	results := make(chan workerpool.Job)
	workerpool.Run(context.Background(), cfg.Jobs, jobs, results, processingHandler(cfg))
	go func() {
		defer close(jobs)
		for _, job := range jobsList {
			jobs <- job
		}
	}()

	seen := map[string]workerpool.Job{}
	for result := range results {
		if result.Status != workerpool.StatusSuccess {
			t.Fatalf("expected e2e job success for %s, got status=%s err=%v", result.InputPath, result.Status, result.Error)
		}
		seen[filepath.Base(result.InputPath)] = result
	}
	if len(seen) != 2 {
		t.Fatalf("expected two successful jobs, got %d", len(seen))
	}

	for _, job := range seen {
		assertFileExists(t, job.OutputPath)
		assertFileExists(t, job.RecipePath)
		assertFileExists(t, perJobLogPath(job.RecipePath))
		assertRecipeMetadataFullMode(t, job.RecipePath)
		assertJobLogStages(t, perJobLogPath(job.RecipePath), []string{"tmp", "probe", "recipe", "render", "metadata", "cleanup", "job"})
	}
	assertTmpCleaned(t, tmpDir)

	ffmpegArgs := readFile(t, ffmpegLog)
	for _, name := range []string{"clip_audio", "clip_silent"} {
		if !strings.Contains(ffmpegArgs, name) {
			t.Fatalf("expected ffmpeg invocation for %s, got:\n%s", name, ffmpegArgs)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(ffmpegArgs), "\n") {
		if strings.Contains(line, "clip_audio") && strings.Contains(line, "rendered.mp4") && !strings.Contains(line, "-map 1:a:0") {
			t.Fatalf("expected final render to map processed audio stream, got: %s", line)
		}
		if strings.Contains(line, "clip_silent") && strings.Contains(line, "rendered.mp4") && strings.Contains(line, "-map 1:a:0") {
			t.Fatalf("expected video-only input to omit audio mapping, got: %s", line)
		}
	}

	exiftoolArgs := readFile(t, exiftoolLog)
	if got := strings.Count(exiftoolArgs, "-all="); got != 2 {
		t.Fatalf("expected metadata clean phase once per job, got %d entries:\n%s", got, exiftoolArgs)
	}
	if got := strings.Count(exiftoolArgs, "-Software="); got != 2 {
		t.Fatalf("expected metadata diversify phase once per job, got %d entries:\n%s", got, exiftoolArgs)
	}
}

func writeFakeTool(t *testing.T, path, script string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected file %s: %v", path, err)
	}
	if info.Size() == 0 {
		t.Fatalf("expected non-empty file %s", path)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func assertRecipeMetadataFullMode(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rec recipe.Recipe
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Metadata.Mode != "clean_diversify" || !rec.Metadata.Clean || len(rec.Metadata.Diversify) == 0 {
		t.Fatalf("expected full metadata mode in recipe %s, got %#v", path, rec.Metadata)
	}
}

func assertJobLogStages(t *testing.T, path string, stages []string) {
	t.Helper()
	data := readFile(t, path)
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(data), "\n") {
		var entry struct {
			Stage  string `json:"stage"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("invalid JSON log line %q: %v", line, err)
		}
		if entry.Status == "done" || entry.Status == "written" || entry.Status == "success" || entry.Status == "created" {
			seen[entry.Stage] = true
		}
	}
	for _, stage := range stages {
		if !seen[stage] {
			t.Fatalf("expected stage %q in %s, got log:\n%s", stage, path, data)
		}
	}
}

func assertTmpCleaned(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("expected tmp directory to be empty after successful jobs, got %v", names)
	}
}

func TestProcessingHandlerCleansTmpAfterSuccessfulRenderAndMetadata(t *testing.T) {
	cfg, job := handlerTestConfig(t)
	restore := stubProcessingPipeline(t, nil)
	defer restore()

	result := processingHandler(cfg)(context.Background(), job)
	if result.Status != workerpool.StatusSuccess {
		t.Fatalf("expected success, got status=%s error=%v", result.Status, result.Error)
	}
	jobTmpDir := filepath.Join(cfg.TmpDir, "job-7-sample")
	if _, err := os.Stat(jobTmpDir); !os.IsNotExist(err) {
		t.Fatalf("expected tmp dir cleanup after successful render, stat err=%v", err)
	}
	if _, err := os.Stat(job.OutputPath); err != nil {
		t.Fatalf("expected metadata stage to create final output: %v", err)
	}
}

func TestProcessingHandlerPreservesTmpAfterRenderError(t *testing.T) {
	cfg, job := handlerTestConfig(t)
	renderErr := errors.New("render boom")
	restore := stubProcessingPipeline(t, renderErr)
	defer restore()

	result := processingHandler(cfg)(context.Background(), job)
	if result.Status != workerpool.StatusFailed {
		t.Fatalf("expected failure, got status=%s", result.Status)
	}
	if !errors.Is(result.Error, renderErr) {
		t.Fatalf("expected render error, got %v", result.Error)
	}
	jobTmpDir := filepath.Join(cfg.TmpDir, "job-7-sample")
	if _, err := os.Stat(jobTmpDir); err != nil {
		t.Fatalf("expected tmp dir to be preserved after render error: %v", err)
	}
}

func handlerTestConfig(t *testing.T) (config.Config, workerpool.Job) {
	t.Helper()
	dir := t.TempDir()
	input := filepath.Join(dir, "sample.mp4")
	if err := os.WriteFile(input, []byte("input"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{TmpDir: filepath.Join(dir, "tmp"), LogsDir: filepath.Join(dir, "logs"), OutputDir: filepath.Join(dir, "out"), MetadataFullMode: "clean_diversify"}
	job := workerpool.Job{ID: 7, InputPath: input, OutputPath: filepath.Join(cfg.OutputDir, "sample_processed.mp4"), RecipePath: filepath.Join(cfg.LogsDir, "sample.recipe.json")}
	return cfg, job
}

func stubProcessingPipeline(t *testing.T, renderErr error) func() {
	t.Helper()
	oldProbe := probeFunc
	oldGenerate := generateRecipeFunc
	oldRender := renderFunc
	oldMetadata := metadataFunc
	probeFunc = func(context.Context, string) (*ffprobe.ProbeData, error) {
		return &ffprobe.ProbeData{Duration: 1, Video: &ffprobe.VideoStream{Width: 64, Height: 64, Fps: 25}, Audio: &ffprobe.AudioStream{}}, nil
	}
	generateRecipeFunc = func(context.Context, config.Config, *ffprobe.ProbeData, pixel.Analyzer) (*recipe.Recipe, error) {
		return &recipe.Recipe{Metadata: recipe.Metadata{Mode: "clean_diversify", Clean: true, Diversify: []recipe.MetadataTag{{Name: "Software", Value: "test"}}}}, nil
	}
	renderFunc = func(_ context.Context, _ config.Config, job workerpool.Job, _ *ffprobe.ProbeData, _ *recipe.Recipe) error {
		if err := os.MkdirAll(filepath.Dir(job.OutputPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(filepath.Dir(job.OutputPath), "debug.tmp"), []byte("debug"), 0o644); err != nil {
			return err
		}
		if renderErr != nil {
			return renderErr
		}
		return os.WriteFile(job.OutputPath, []byte("rendered"), 0o644)
	}
	metadataFunc = func(_ context.Context, _ config.Config, job workerpool.Job, _ *recipe.Recipe, renderedPath string) error {
		data, err := os.ReadFile(renderedPath)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(job.OutputPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(job.OutputPath, data, 0o644)
	}
	return func() {
		probeFunc = oldProbe
		generateRecipeFunc = oldGenerate
		renderFunc = oldRender
		metadataFunc = oldMetadata
	}
}
