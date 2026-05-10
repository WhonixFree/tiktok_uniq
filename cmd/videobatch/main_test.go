package main

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"videobatch/internal/config"
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
