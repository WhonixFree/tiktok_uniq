package render

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"videobatch/internal/config"
	"videobatch/internal/ffprobe"
	"videobatch/internal/recipe"
	"videobatch/internal/workerpool"
)

func testConfig() config.Config {
	return config.Config{
		StreamOverlayOpacity: 0.02,
		ColorStrength:        "soft",
		CodecProfile:         "fast",
		Overwrite:            true,
	}
}

func testProbe(audio bool) *ffprobe.ProbeData {
	p := &ffprobe.ProbeData{Duration: 1, Video: &ffprobe.VideoStream{Width: 64, Height: 64, Fps: 25}}
	if audio {
		p.Audio = &ffprobe.AudioStream{Codec: "aac", SampleRate: 44100, Channels: 1}
	}
	return p
}

func testRecipe() *recipe.Recipe {
	return &recipe.Recipe{
		VideoSpeed: recipe.SpeedConfig{BasePercent: 0.2},
		AudioSpeed: recipe.SpeedConfig{BasePercent: 0.2},
		PixelReplacement: recipe.PixelReplacement{
			BlurSigma:        0.08,
			Percent:          0.2,
			Mode:             "edge",
			AreaEdge:         "top",
			AreaInsetPercent: 0.01,
			NeighborOffset:   1,
		},
	}
}

func TestBuildPipelineUsesRecommendedOrder(t *testing.T) {
	pipeline, err := BuildPipeline(testConfig(), testProbe(true), testRecipe())
	if err != nil {
		t.Fatalf("BuildPipeline failed: %v", err)
	}
	if len(pipeline.Stages) != len(RecommendedOrder) {
		t.Fatalf("unexpected stage count: %d", len(pipeline.Stages))
	}
	for i := range RecommendedOrder {
		if pipeline.Stages[i] != RecommendedOrder[i] {
			t.Fatalf("stage %d = %q, want %q", i, pipeline.Stages[i], RecommendedOrder[i])
		}
	}
}

func TestBuildPipelineFilterGraphKeepsEffectOrder(t *testing.T) {
	pipeline, err := BuildPipeline(testConfig(), testProbe(true), testRecipe())
	if err != nil {
		t.Fatalf("BuildPipeline failed: %v", err)
	}
	markers := []string{"scale=", "eq=", "gblur=", "crop=", "[pixel]setpts=", "color=c=white", "[temporal][streamoverlay]overlay=0:0"}
	last := -1
	for _, marker := range markers {
		idx := strings.Index(pipeline.FilterGraph, marker)
		if idx == -1 {
			t.Fatalf("missing %q in filter graph: %s", marker, pipeline.FilterGraph)
		}
		if idx <= last {
			t.Fatalf("marker %q is out of order in filter graph: %s", marker, pipeline.FilterGraph)
		}
		last = idx
	}
	if got := strings.Join(pipeline.AudioFilters, ","); !strings.Contains(got, "atempo=") {
		t.Fatalf("expected audio temporal filter, got %q", got)
	}
}

func TestRenderIntegrationValidatesOutputIntegrityAndAVSync(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not available in test environment")
	}
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is not available in test environment")
	}

	dir := t.TempDir()
	input := filepath.Join(dir, "input.mp4")
	output := filepath.Join(dir, "output.mp4")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=64x64:rate=25:duration=1.2",
		"-f", "lavfi", "-i", "sine=frequency=1000:sample_rate=44100:duration=1.2",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", input,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create fixture: %v: %s", err, strings.TrimSpace(string(out)))
	}

	runner := Runner{FFmpegPath: ffmpegPath, FFprobePath: ffprobePath}
	err = runner.Render(ctx, testConfig(), workerpool.Job{InputPath: input, OutputPath: output}, testProbe(true), testRecipe())
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if stat, err := os.Stat(output); err != nil {
		t.Fatalf("output missing: %v", err)
	} else if stat.Size() == 0 {
		t.Fatal("output is empty")
	}
	if err := runner.ValidateOutput(ctx, output); err != nil {
		t.Fatalf("ValidateOutput failed: %v", err)
	}
}
