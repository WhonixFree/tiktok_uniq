package metadata

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"videobatch/internal/config"
	"videobatch/internal/recipe"
	"videobatch/internal/workerpool"
)

// CommandRunner executes a metadata tool command. It is injectable so worker
// tests can assert the mandatory clean + diversify phases without shelling out.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Runner applies mandatory metadata processing in full mode.
type Runner struct {
	ExifToolPath string
	RunCommand   CommandRunner
}

// Process copies the rendered tmp artifact to the final output and applies the
// full metadata mode as two explicit ExifTool phases: clean, then diversify.
func (r Runner) Process(ctx context.Context, cfg config.Config, job workerpool.Job, rec *recipe.Recipe, renderedPath string) error {
	if rec == nil {
		return fmt.Errorf("metadata recipe is required")
	}
	if rec.Metadata.Mode != "clean_diversify" {
		return fmt.Errorf("unsupported metadata full mode %q", rec.Metadata.Mode)
	}
	if err := os.MkdirAll(filepath.Dir(job.OutputPath), 0o755); err != nil {
		return err
	}
	if err := copyFile(renderedPath, job.OutputPath); err != nil {
		return err
	}
	exiftoolPath, err := executable(r.ExifToolPath, "exiftool")
	if err != nil {
		return err
	}
	run := r.RunCommand
	if run == nil {
		run = defaultCommandRunner
	}
	cleanArgs := []string{"-overwrite_original", "-all=", job.OutputPath}
	if out, err := run(ctx, exiftoolPath, cleanArgs...); err != nil {
		return fmt.Errorf("metadata clean failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	diversifyArgs := []string{"-overwrite_original"}
	for _, tag := range rec.Metadata.DiversifyTags() {
		diversifyArgs = append(diversifyArgs, fmt.Sprintf("-%s=%s", tag.Name, tag.Value))
	}
	diversifyArgs = append(diversifyArgs, job.OutputPath)
	if out, err := run(ctx, exiftoolPath, diversifyArgs...); err != nil {
		return fmt.Errorf("metadata diversify failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	_ = cfg
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func executable(override, name string) (string, error) {
	if override != "" {
		return override, nil
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s executable not found: %w", name, err)
	}
	return path, nil
}

func defaultCommandRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
