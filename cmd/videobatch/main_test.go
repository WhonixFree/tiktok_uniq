package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"videobatch/internal/config"
	"videobatch/internal/ffprobe"
	"videobatch/internal/pixel"
	"videobatch/internal/recipe"
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
	logger := slog.New(slog.DiscardHandler)
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
