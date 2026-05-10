package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"videobatch/internal/config"
	"videobatch/internal/logging"
	"videobatch/internal/scanner"
	"videobatch/internal/workerpool"
)

const startupTimeout = 5 * time.Second

var (
	checkExecutableFn     = checkExecutable
	checkPythonPackagesFn = checkPythonPackages
)

type dependency struct {
	Name        string
	Command     string
	Args        []string
	Required    bool
	InstallHint string
}

func main() {
	bootstrapLogger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})).With("app", "videobatch")

	cfg, err := config.ParseFlags(os.Args[1:], os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		bootstrapLogger.Error("configuration error", "error", err)
		os.Exit(2)
	}

	if err := ensureRuntimeDirs(cfg.OutputDir, cfg.TmpDir, cfg.LogsDir); err != nil {
		bootstrapLogger.Error("failed to create runtime directories", "error", err)
		os.Exit(1)
	}

	logger, closeLogger, err := logging.New(cfg.LogsDir)
	if err != nil {
		bootstrapLogger.Error("failed to initialize logger", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := closeLogger(); err != nil {
			bootstrapLogger.Error("failed to close logger", "error", err)
		}
	}()

	logger.Info("startup configuration loaded",
		"input", cfg.InputDir,
		"output", cfg.OutputDir,
		"tmp", cfg.TmpDir,
		"logs", cfg.LogsDir,
		"recursive", cfg.Recursive,
		"jobs", cfg.Jobs,
		"threads_per_job", cfg.ThreadsPerJob,
		"seed", cfg.Seed,
		"dry_run", cfg.DryRun,
		"cleanup", cfg.Cleanup,
		"cpu_count", runtime.NumCPU(),
	)

	ctx := context.Background()
	if err := runStartupChecks(ctx, cfg, logger); err != nil {
		logger.Error("startup check failed", "error", err)
		os.Exit(1)
	}

	logger.Info("startup checks passed")

	files, err := scanner.Scan(cfg.InputDir, cfg.Recursive)
	if err != nil {
		if errors.Is(err, scanner.ErrNoSupportedFiles) {
			logger.Warn("no supported files found in input directory", "status", workerpool.StatusSkipped, "input_dir", cfg.InputDir)
			return
		}
		logger.Error("scan failed", "error", err, "input_dir", cfg.InputDir)
		os.Exit(1)
	}

	plannedJobs := buildJobs(files, cfg.OutputDir, cfg.LogsDir)
	logger.Info("scan completed", "file_count", len(plannedJobs))

	if cfg.DryRun {
		logger.Info("dry-run plan ready", "message", fmt.Sprintf("Would process %d files", len(plannedJobs)), "file_count", len(plannedJobs))
		return
	}

	runCtx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	jobs := make(chan workerpool.Job)
	results := make(chan workerpool.Job)
	workerpool.Run(runCtx, cfg.Jobs, jobs, results, workerpool.DefaultHandler)

	go func() {
		defer close(jobs)
		for _, job := range plannedJobs {
			select {
			case <-runCtx.Done():
				logger.Warn("job dispatch canceled", "reason", runCtx.Err())
				return
			case jobs <- job:
			}
		}
	}()

	for result := range results {
		logJobResult(logger, result)
	}

	if err := runCtx.Err(); err != nil {
		logger.Warn("shutdown completed after interrupt", "reason", err)
	}
}

func ensureRuntimeDirs(paths ...string) error {
	for _, path := range paths {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	return nil
}

func runStartupChecks(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	var errs []error

	for _, dep := range startupDependencies(cfg) {
		if err := checkExecutableFn(ctx, dep); err != nil {
			if dep.Required {
				errs = append(errs, err)
				logger.Error("dependency missing", "name", dep.Name, "error", err)
			} else {
				logger.Warn("optional dependency missing", "name", dep.Name, "error", err)
			}
			continue
		}
		logger.Info("dependency available", "name", dep.Name, "command", dep.Command)
	}

	if cfg.AudioEnvelope == "python" {
		if err := checkPythonPackagesFn(ctx, []string{"numpy", "scipy", "soundfile"}); err != nil {
			errs = append(errs, fmt.Errorf("python audio envelope dependencies: %w", err))
		} else {
			logger.Info("python audio envelope dependencies available")
		}
	}
	if cfg.Captions == "auto" {
		if err := checkPythonPackagesFn(ctx, []string{"faster-whisper", "pysubs2"}); err != nil {
			errs = append(errs, fmt.Errorf("python caption dependencies: %w", err))
		} else {
			logger.Info("python caption dependencies available")
		}
	}

	return errors.Join(errs...)
}

func startupDependencies(cfg config.Config) []dependency {
	return []dependency{
		{
			Name:        "ffmpeg",
			Command:     "ffmpeg",
			Args:        []string{"-version"},
			Required:    true,
			InstallHint: "Install FFmpeg and make sure ffmpeg is in PATH.",
		},
		{
			Name:        "ffprobe",
			Command:     "ffprobe",
			Args:        []string{"-version"},
			Required:    true,
			InstallHint: "Install FFmpeg tools and make sure ffprobe is in PATH.",
		},
		{
			Name:        "exiftool",
			Command:     "exiftool",
			Args:        []string{"-ver"},
			Required:    true,
			InstallHint: "Install ExifTool for metadata processing.",
		},
		{
			Name:        "python3",
			Command:     "python3",
			Args:        []string{"--version"},
			Required:    true,
			InstallHint: "Install Python 3 and make sure python3 is in PATH.",
		},
	}
}

func checkExecutable(parent context.Context, dep dependency) error {
	path, err := exec.LookPath(dep.Command)
	if err != nil {
		return fmt.Errorf("%s not found: %s", dep.Name, dep.InstallHint)
	}

	ctx, cancel := context.WithTimeout(parent, startupTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, dep.Args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%s check timed out after %s", dep.Name, startupTimeout)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("%s check failed: %w: %s", dep.Name, err, msg)
		}
		return fmt.Errorf("%s check failed: %w", dep.Name, err)
	}
	return nil
}

func checkPythonPackages(parent context.Context, packages []string) error {
	ctx, cancel := context.WithTimeout(parent, startupTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "python3", append([]string{"-m", "pip", "show"}, packages...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("python package check timed out after %s", startupTimeout)
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("package check failed: %w: %s; install with python3 -m pip install %s", err, msg, strings.Join(packages, " "))
		}
		return fmt.Errorf("package check failed: %w; install with python3 -m pip install %s", err, strings.Join(packages, " "))
	}
	return nil
}

func buildJobs(files []string, outputDir string, logsDir string) []workerpool.Job {
	jobs := make([]workerpool.Job, 0, len(files))
	for i, inputPath := range files {
		baseName := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
		jobs = append(jobs, workerpool.Job{
			ID:         i + 1,
			InputPath:  inputPath,
			OutputPath: filepath.Join(outputDir, baseName+"_processed.mp4"),
			RecipePath: filepath.Join(logsDir, baseName+".recipe.json"),
			Status:     workerpool.StatusPending,
		})
	}
	return jobs
}

func logJobResult(logger *slog.Logger, job workerpool.Job) {
	attrs := []any{
		"job_id", job.ID,
		"input", job.InputPath,
		"output", job.OutputPath,
		"recipe", job.RecipePath,
		"status", job.Status,
	}
	if job.Error != nil {
		attrs = append(attrs, "error", job.Error)
	}

	switch job.Status {
	case workerpool.StatusSuccess:
		logger.Info("job completed", attrs...)
	case workerpool.StatusSkipped:
		logger.Warn("job skipped", attrs...)
	case workerpool.StatusFailed:
		logger.Error("job failed", attrs...)
	default:
		logger.Info("job status updated", attrs...)
	}
}
