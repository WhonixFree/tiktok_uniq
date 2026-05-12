package main

import (
	"bytes"
	"context"
	"encoding/json"
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
	"videobatch/internal/ffprobe"
	"videobatch/internal/logging"
	"videobatch/internal/metadata"
	"videobatch/internal/recipe"
	"videobatch/internal/render"
	"videobatch/internal/scanner"
	"videobatch/internal/workerpool"
)

const startupTimeout = 5 * time.Second

var (
	probeFunc          = ffprobe.Probe
	generateRecipeFunc = recipe.GenerateWithSmartAnalyzer
	renderFunc         = func(ctx context.Context, cfg config.Config, job workerpool.Job, probeData *ffprobe.ProbeData, rec *recipe.Recipe) error {
		return render.Runner{}.Render(ctx, cfg, job, probeData, rec)
	}
	metadataFunc = func(ctx context.Context, cfg config.Config, job workerpool.Job, rec *recipe.Recipe, renderedPath string) error {
		return metadata.Runner{}.Process(ctx, cfg, job, rec, renderedPath)
	}
)

type dependency struct {
	Name        string
	Command     string
	Args        []string
	Required    bool
	InstallHint string
}

var startupDependencyProvider = startupDependencies

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
	workerpool.Run(runCtx, cfg.Jobs, jobs, results, processingHandler(cfg))

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

func processingHandler(cfg config.Config) workerpool.Handler {
	return func(ctx context.Context, job workerpool.Job) workerpool.Job {
		jobLogPath := perJobLogPath(job.RecipePath)
		fail := func(stage string, err error) workerpool.Job {
			if logErr := writeJobLog(jobLogPath, stage, "failed", map[string]any{"error": err.Error()}); logErr != nil {
				err = errors.Join(err, logErr)
			}
			job.Status = workerpool.StatusFailed
			job.Error = err
			return job
		}

		if err := writeJobLog(jobLogPath, "job", "started", map[string]any{"job_id": job.ID, "input": job.InputPath, "output": job.OutputPath}); err != nil {
			job.Status = workerpool.StatusFailed
			job.Error = err
			return job
		}
		if err := ctx.Err(); err != nil {
			if logErr := writeJobLog(jobLogPath, "job", "skipped", map[string]any{"error": err.Error()}); logErr != nil {
				err = errors.Join(err, logErr)
			}
			job.Status = workerpool.StatusSkipped
			job.Error = err
			return job
		}

		jobTmpDir := filepath.Join(cfg.TmpDir, fmt.Sprintf("job-%d-%s", job.ID, strings.TrimSuffix(filepath.Base(job.InputPath), filepath.Ext(job.InputPath))))
		if err := os.MkdirAll(jobTmpDir, 0o755); err != nil {
			return fail("tmp", err)
		}
		if err := writeJobLog(jobLogPath, "tmp", "created", map[string]any{"path": jobTmpDir}); err != nil {
			return fail("tmp", err)
		}

		probeData, err := probeFunc(ctx, job.InputPath)
		if err != nil {
			return fail("probe", err)
		}
		if err := writeJobLog(jobLogPath, "probe", "done", map[string]any{"duration": probeData.Duration, "has_audio": probeData.Audio != nil}); err != nil {
			return fail("probe", err)
		}

		jobCfg := cfg
		jobCfg.InputDir = job.InputPath
		jobCfg.OutputDir = job.OutputPath
		jobCfg.TmpDir = jobTmpDir
		rec, err := generateRecipeFunc(ctx, jobCfg, probeData, nil)
		if err != nil {
			return fail("recipe", err)
		}
		rec.InputPath = job.InputPath
		rec.OutputPath = job.OutputPath

		if err := writeRecipe(job.RecipePath, rec); err != nil {
			return fail("recipe", err)
		}
		if err := writeJobLog(jobLogPath, "recipe", "written", map[string]any{"path": job.RecipePath}); err != nil {
			return fail("recipe", err)
		}

		renderedPath := filepath.Join(jobTmpDir, "rendered.mp4")
		renderJob := job
		renderJob.OutputPath = renderedPath
		if err := renderFunc(ctx, cfg, renderJob, probeData, rec); err != nil {
			return fail("render", err)
		}
		if err := writeJobLog(jobLogPath, "render", "done", map[string]any{"path": renderedPath}); err != nil {
			return fail("render", err)
		}

		if err := metadataFunc(ctx, cfg, job, rec, renderedPath); err != nil {
			return fail("metadata", err)
		}
		if err := writeJobLog(jobLogPath, "metadata", "done", map[string]any{"mode": rec.Metadata.Mode, "output": job.OutputPath}); err != nil {
			return fail("metadata", err)
		}

		if err := os.RemoveAll(jobTmpDir); err != nil {
			return fail("cleanup", err)
		}
		if err := writeJobLog(jobLogPath, "cleanup", "done", map[string]any{"path": jobTmpDir}); err != nil {
			return fail("cleanup", err)
		}
		job.Status = workerpool.StatusSuccess
		if err := writeJobLog(jobLogPath, "job", "success", map[string]any{"job_id": job.ID}); err != nil {
			return fail("job", err)
		}
		return job
	}
}

func perJobLogPath(recipePath string) string {
	if strings.HasSuffix(recipePath, ".recipe.json") {
		return strings.TrimSuffix(recipePath, ".recipe.json") + ".log"
	}
	return recipePath + ".log"
}

func writeJobLog(path, stage, status string, fields map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	entry := map[string]any{
		"time":   time.Now().UTC().Format(time.RFC3339Nano),
		"stage":  stage,
		"status": status,
	}
	for key, value := range fields {
		entry[key] = value
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(data)
	return err
}

func writeRecipe(path string, rec *recipe.Recipe) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
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

	for _, dep := range startupDependencyProvider(cfg) {
		if err := checkExecutable(ctx, dep); err != nil {
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
		if err := checkPythonPackages(ctx, []string{"numpy", "scipy", "soundfile"}); err != nil {
			errs = append(errs, fmt.Errorf("python audio envelope dependencies: %w", err))
		} else {
			logger.Info("python audio envelope dependencies available")
		}
	}
	if cfg.Captions == "auto" {
		if err := checkPythonPackages(ctx, []string{"faster-whisper", "pysubs2"}); err != nil {
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
