package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"videobatch/internal/config"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestStartupDependenciesRequiredSet(t *testing.T) {

	deps := startupDependencies(config.Config{})
	if len(deps) != 4 {
		t.Fatalf("expected 4 dependencies, got %d", len(deps))
	}

	want := map[string]string{
		"ffmpeg":   "Install FFmpeg and make sure ffmpeg is in PATH.",
		"ffprobe":  "Install FFmpeg tools and make sure ffprobe is in PATH.",
		"exiftool": "Install ExifTool for metadata processing.",
		"python3":  "Install Python 3 and make sure python3 is in PATH.",
	}

	for _, dep := range deps {
		if !dep.Required {
			t.Fatalf("dependency %s should be required", dep.Name)
		}
		hint, ok := want[dep.Name]
		if !ok {
			t.Fatalf("unexpected dependency %s", dep.Name)
		}
		if dep.InstallHint != hint {
			t.Fatalf("dependency %s hint mismatch: got %q want %q", dep.Name, dep.InstallHint, hint)
		}
	}
}

func TestRunStartupChecksFailsWhenRequiredDependencyMissing(t *testing.T) {

	cfg := config.Config{}
	logger := testLogger()

	origExec := checkExecutableFn
	origPy := checkPythonPackagesFn
	t.Cleanup(func() {
		checkExecutableFn = origExec
		checkPythonPackagesFn = origPy
	})

	checkExecutableFn = func(_ context.Context, dep dependency) error {
		if dep.Name == "ffprobe" {
			return errors.New("ffprobe not found: install ffprobe")
		}
		return nil
	}
	checkPythonPackagesFn = func(_ context.Context, _ []string) error { return nil }

	err := runStartupChecks(context.Background(), cfg, logger)
	if err == nil {
		t.Fatal("expected startup check error")
	}
	if !strings.Contains(err.Error(), "ffprobe not found") {
		t.Fatalf("expected ffprobe error, got: %v", err)
	}
}

func TestRunStartupChecksValidatesPythonPackagesForRequiredModes(t *testing.T) {

	cfg := config.Config{AudioEnvelope: "python", Captions: "auto"}
	logger := testLogger()

	origExec := checkExecutableFn
	origPy := checkPythonPackagesFn
	t.Cleanup(func() {
		checkExecutableFn = origExec
		checkPythonPackagesFn = origPy
	})

	checkExecutableFn = func(_ context.Context, _ dependency) error { return nil }
	var seen [][]string
	checkPythonPackagesFn = func(_ context.Context, pkgs []string) error {
		clone := append([]string(nil), pkgs...)
		seen = append(seen, clone)
		return nil
	}

	if err := runStartupChecks(context.Background(), cfg, logger); err != nil {
		t.Fatalf("unexpected startup check error: %v", err)
	}

	if len(seen) != 2 {
		t.Fatalf("expected two python package checks, got %d", len(seen))
	}

	got := fmt.Sprintf("%v", seen)
	if !strings.Contains(got, "numpy") || !strings.Contains(got, "scipy") || !strings.Contains(got, "soundfile") {
		t.Fatalf("missing audio envelope python deps in checks: %v", seen)
	}
	if !strings.Contains(got, "faster-whisper") || !strings.Contains(got, "pysubs2") {
		t.Fatalf("missing captions python deps in checks: %v", seen)
	}
}
