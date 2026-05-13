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
	markers := []string{"[0:v]format=rgba[gcolor]", "[gcolor]null[blurred]", "[blurred]null[pixel]", "[pixel]trim=", "[temporal]format=rgba[vout]"}
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
	if got := strings.Join(pipeline.AudioFilters, ","); strings.Contains(got, "atempo=") {
		t.Fatalf("audio temporal filter must not use atempo, got %q", got)
	}
}

func TestBuildPipelineUsesExternalPixelStagePlaceholder(t *testing.T) {
	pipeline, err := BuildPipeline(testConfig(), testProbe(true), testRecipe())
	if err != nil {
		t.Fatalf("BuildPipeline failed: %v", err)
	}
	want := []string{"[gcolor]null[blurred]", "[blurred]null[pixel]"}
	for _, part := range want {
		if !strings.Contains(pipeline.FilterGraph, part) {
			t.Fatalf("pixel replacement graph missing %q:\n%s", part, pipeline.FilterGraph)
		}
	}
	if strings.Contains(pipeline.FilterGraph, "geq=") || strings.Contains(pipeline.FilterGraph, "colorchannelmixer=aa=") {
		t.Fatalf("pixel replacement must run in external python stage, got: %s", pipeline.FilterGraph)
	}
}

func TestBuildPipelineWithOverlayInputUsesOverlayStream(t *testing.T) {
	pipeline, err := BuildPipelineWithDonors(testConfig(), testProbe(true), testRecipe(), -1, 1)
	if err != nil {
		t.Fatalf("BuildPipelineWithDonors failed: %v", err)
	}
	want := []string{
		"[1:v]scale=64:64,setsar=1,fps=fps=25.00000000,format=rgba,colorchannelmixer=aa=0.020000[streamoverlay]",
		"[temporal][streamoverlay]overlay=0:0:shortest=1[vout]",
	}
	for _, part := range want {
		if !strings.Contains(pipeline.FilterGraph, part) {
			t.Fatalf("overlay graph missing %q:\n%s", part, pipeline.FilterGraph)
		}
	}
	if strings.Contains(pipeline.FilterGraph, "color=c=white") || strings.Contains(pipeline.FilterGraph, "nullsrc") || strings.Contains(pipeline.FilterGraph, "testsrc") || strings.Contains(pipeline.FilterGraph, "noise") {
		t.Fatalf("synthetic overlay source must not be used: %s", pipeline.FilterGraph)
	}
}

func TestBuildPipelineSkipsCropWhenCropDisabled(t *testing.T) {
	cfg := testConfig()
	cfg.CropEnabled = false
	cfg.CropMaxPercent = 2

	pipeline, err := BuildPipeline(cfg, testProbe(true), testRecipe())
	if err != nil {
		t.Fatalf("BuildPipeline failed: %v", err)
	}
	if strings.Contains(pipeline.FilterGraph, "[0:v]crop=") {
		t.Fatalf("crop filter must not be emitted when crop is disabled: %s", pipeline.FilterGraph)
	}
}

func TestBuildVideoSpeedPlanIsDeterministicAndTracksPlannedDuration(t *testing.T) {
	speed := recipe.SpeedConfig{
		BasePercent: 0.5,
		Sine:        recipe.SineParams{Amplitude: 0.001, Frequency: 0.1, Phase: 0.2},
		MicroEvents: []recipe.SpeedEvent{{StartSec: 0.45, DurationSec: 0.10, Delta: -0.002}},
	}
	first := BuildVideoSpeedPlan(1.2, speed)
	second := BuildVideoSpeedPlan(1.2, speed)
	if len(first) != len(second) {
		t.Fatalf("deterministic plan length mismatch: %d != %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("plan segment %d mismatch: %+v != %+v", i, first[i], second[i])
		}
		if first[i].Speed < 0.95 || first[i].Speed > 1.05 {
			t.Fatalf("segment speed out of safe bounds: %+v", first[i])
		}
	}
	_, planned := videoTemporalFilter("[pixel]", "[temporal]", first)
	if planned <= 0 || planned >= 1.2 {
		t.Fatalf("unexpected planned duration %.6f", planned)
	}
}

func TestBuildPipelineExposesPlannedVideoDuration(t *testing.T) {
	pipeline, err := BuildPipeline(testConfig(), testProbe(true), testRecipe())
	if err != nil {
		t.Fatalf("BuildPipeline failed: %v", err)
	}
	if pipeline.PlannedVideoDuration <= 0 {
		t.Fatalf("expected planned duration, got %.6f", pipeline.PlannedVideoDuration)
	}
}

func TestCropRatioUsesPercentDividedByHundred(t *testing.T) {
	if got := cropRatio(2); got != 0.02 {
		t.Fatalf("cropRatio(2) = %v, want 0.02", got)
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

func TestVideoTemporalEventFilterUsesPiecewiseOneFrameReplaceConcat(t *testing.T) {
	events := []recipe.Event{{Frame: 2, DonorFrame: 7, DonorPath: "donor.mp4", DonorImagePath: "replace_donor_000.png"}}
	graph := videoTemporalEventFilter("[speeded]", "[temporal]", nil, events, 1, 25, 1, 64, 64)
	want := []string{
		"[speeded]trim=start=0.000000000:end=0.080000000,setpts=PTS-STARTPTS[rseg0]",
		"[1:v]scale=64:64,setsar=1,format=rgba,fps=fps=25.00000000,trim=duration=0.040000000,setpts=PTS-STARTPTS[rdonor0]",
		"[speeded]trim=start=0.120000000:end=1.000000000,setpts=PTS-STARTPTS[rseg1]",
		"[rseg0][rdonor0][rseg1]concat=n=3:v=1:a=0,fps=fps=25.00000000[temporal]",
	}
	for _, part := range want {
		if !strings.Contains(graph, part) {
			t.Fatalf("replace graph missing %q:\n%s", part, graph)
		}
	}
	if strings.Contains(graph, "enable=") || strings.Contains(graph, "overlay=") {
		t.Fatalf("replace must be piecewise concat, not overlay enable: %s", graph)
	}
}

func TestVideoTemporalEventFilterInjectsOneFrameFreeze(t *testing.T) {
	freeze := []recipe.Event{{Frame: 2}}
	graph := videoTemporalEventFilter("[speeded]", "[temporal]", freeze, nil, -1, 25, 1, 64, 64)
	want := []string{
		"[speeded]trim=start=0.000000000:end=0.080000000,setpts=PTS-STARTPTS[rseg0]",
		"[speeded]trim=start=0.080000000:end=0.120000000,setpts=PTS-STARTPTS[rfreeze1]",
		"[speeded]trim=start=0.120000000:end=1.000000000,setpts=PTS-STARTPTS[rseg1]",
		"[rseg0][rfreeze1][rseg1]concat=n=3:v=1:a=0,fps=fps=25.00000000[temporal]",
	}
	for _, part := range want {
		if !strings.Contains(graph, part) {
			t.Fatalf("freeze graph missing %q:\n%s", part, graph)
		}
	}
}

func TestPrepareReplaceDonorsExtractsOnePNGPerEvent(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "ffmpeg.args")
	ffmpeg := filepath.Join(dir, "ffmpeg")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + logPath + "\nfor arg do out=\"$arg\"; done\nprintf 'png:%s' \"$*\" > \"$out\"\n"
	if err := os.WriteFile(ffmpeg, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ffmpeg: %v", err)
	}
	img0 := filepath.Join(dir, "replace_donor_000.png")
	img1 := filepath.Join(dir, "replace_donor_001.png")
	rec := &recipe.Recipe{ReplaceEvents: []recipe.Event{
		{Frame: 1, DonorFrame: 3, DonorPath: "donor-a.mp4", DonorImagePath: img0},
		{Frame: 4, DonorFrame: 8, DonorPath: "donor-a.mp4", DonorImagePath: img1},
	}}
	runner := Runner{FFmpegPath: ffmpeg}
	got, err := runner.prepareReplaceDonors(context.Background(), workerpool.Job{OutputPath: filepath.Join(dir, "out.mp4")}, rec)
	if err != nil {
		t.Fatalf("prepareReplaceDonors failed: %v", err)
	}
	if len(got) != 2 || got[0] != img0 || got[1] != img1 {
		t.Fatalf("unexpected donor images: %#v", got)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fake ffmpeg log: %v", err)
	}
	log := string(logData)
	for _, marker := range []string{"select=eq(n\\,3) -vsync 0 -frames:v 1", "select=eq(n\\,8) -vsync 0 -frames:v 1"} {
		if !strings.Contains(log, marker) {
			t.Fatalf("missing extraction marker %q in:\n%s", marker, log)
		}
	}
	firstPNG, _ := os.ReadFile(img0)
	if len(firstPNG) == 0 {
		t.Fatal("expected fake donor PNG to be written")
	}
}
