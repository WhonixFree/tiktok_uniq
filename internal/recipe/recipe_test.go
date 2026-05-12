package recipe

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"videobatch/internal/config"
	"videobatch/internal/ffprobe"
	"videobatch/internal/pixel"
)

func baseCfg() config.Config {
	return config.Config{
		InputDir: "in", OutputDir: "out", Seed: 42, AVSineMode: "independent", MetadataFullMode: "clean_diversify",
		AudioBaseSpeed:      config.SpeedRange{MinPercent: 0.2, MaxPercent: 0.5},
		VideoBaseSpeed:      config.SpeedRange{MinPercent: 0.2, MaxPercent: 0.5},
		AudioSine:           config.SineParamsRange{AmplitudeMin: 0.01, AmplitudeMax: 0.02, FrequencyMin: 0.1, FrequencyMax: 0.2, PhaseMin: 0.0, PhaseMax: 1.0},
		VideoSine:           config.SineParamsRange{AmplitudeMin: 0.01, AmplitudeMax: 0.02, FrequencyMin: 0.1, FrequencyMax: 0.2, PhaseMin: 0.0, PhaseMax: 1.0},
		FreezeCount:         config.EventCountRange{Min: 2, Max: 2},
		ReplaceCount:        config.EventCountRange{Min: 2, Max: 2},
		MinEventDistanceSec: 1.0,
		PixelReplacePercent: config.PercentRange{Min: 0.1, Max: 0.2},
		PixelBlurSigma:      config.PercentRange{Min: 0.08, Max: 0.22},
		PixelReplaceMode:    "edge",
		PixelAreaEdgeInset:  config.PercentRange{Min: 0.01, Max: 0.02},
		PixelAreaSmartGrid:  8,
		NeighborOffsetMin:   1,
		NeighborOffsetMax:   2,
	}
}

func TestGenerateTemporalPlannerConstraints(t *testing.T) {
	cfg := baseCfg()
	probe := &ffprobe.ProbeData{Duration: 12, Video: &ffprobe.VideoStream{Fps: 10}}

	rec, err := Generate(cfg, probe)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(rec.FreezeEvents) != 2 || len(rec.ReplaceEvents) != 2 {
		t.Fatalf("unexpected counts freeze=%d replace=%d", len(rec.FreezeEvents), len(rec.ReplaceEvents))
	}

	minDist := int64(cfg.MinEventDistanceSec * probe.Video.Fps)
	all := make([]Event, 0, len(rec.FreezeEvents)+len(rec.ReplaceEvents))
	all = append(all, rec.FreezeEvents...)
	all = append(all, rec.ReplaceEvents...)
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if abs64(all[i].Frame-all[j].Frame) < minDist {
				t.Fatalf("events too close: %d and %d", all[i].Frame, all[j].Frame)
			}
		}
	}

	usedDonors := map[int64]bool{}
	for _, ev := range rec.ReplaceEvents {
		if ev.DonorFrame == ev.Frame {
			t.Fatalf("donor frame equals target frame: %d", ev.Frame)
		}
		if usedDonors[ev.DonorFrame] {
			t.Fatalf("duplicate donor frame found: %d", ev.DonorFrame)
		}
		usedDonors[ev.DonorFrame] = true
	}
}

func TestGenerateSineLockMode(t *testing.T) {
	cfg := baseCfg()
	cfg.AVSineMode = "lock"
	probe := &ffprobe.ProbeData{Duration: 5, Video: &ffprobe.VideoStream{Fps: 30}}
	rec, err := Generate(cfg, probe)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if rec.AudioSpeed.Sine != rec.VideoSpeed.Sine {
		t.Fatal("expected locked sine params for audio/video")
	}
}

func TestGenerateSpeedMicroEventsAreDeterministicAndDurationDependent(t *testing.T) {
	cfg := baseCfg()
	probeShort := &ffprobe.ProbeData{Duration: 10, Video: &ffprobe.VideoStream{Fps: 30}}
	probeLong := &ffprobe.ProbeData{Duration: 30, Video: &ffprobe.VideoStream{Fps: 30}}

	shortA, err := Generate(cfg, probeShort)
	if err != nil {
		t.Fatalf("Generate short A failed: %v", err)
	}
	shortB, err := Generate(cfg, probeShort)
	if err != nil {
		t.Fatalf("Generate short B failed: %v", err)
	}
	if !reflect.DeepEqual(shortA.AudioSpeed.MicroEvents, shortB.AudioSpeed.MicroEvents) || !reflect.DeepEqual(shortA.VideoSpeed.MicroEvents, shortB.VideoSpeed.MicroEvents) {
		t.Fatal("speed micro event planning must be deterministic for the same seed")
	}
	longRec, err := Generate(cfg, probeLong)
	if err != nil {
		t.Fatalf("Generate long failed: %v", err)
	}
	if len(shortA.AudioSpeed.MicroEvents) != speedMicroCount(probeShort.Duration) || len(longRec.AudioSpeed.MicroEvents) != speedMicroCount(probeLong.Duration) {
		t.Fatalf("unexpected audio micro counts short=%d long=%d", len(shortA.AudioSpeed.MicroEvents), len(longRec.AudioSpeed.MicroEvents))
	}
	if len(longRec.AudioSpeed.MicroEvents) <= len(shortA.AudioSpeed.MicroEvents) {
		t.Fatalf("expected longer media to receive more micro events, short=%d long=%d", len(shortA.AudioSpeed.MicroEvents), len(longRec.AudioSpeed.MicroEvents))
	}
	if len(shortA.AudioSpeed.FreezeEvents) != audioFreezeCount(probeShort.Duration) || len(longRec.AudioSpeed.FreezeEvents) != audioFreezeCount(probeLong.Duration) {
		t.Fatalf("unexpected audio freeze counts short=%d long=%d", len(shortA.AudioSpeed.FreezeEvents), len(longRec.AudioSpeed.FreezeEvents))
	}
}

func TestGeneratePixelMandatoryBlockEdgeMode(t *testing.T) {
	cfg := baseCfg()
	probe := &ffprobe.ProbeData{Duration: 5, Video: &ffprobe.VideoStream{Fps: 30}}
	rec, err := Generate(cfg, probe)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if rec.PixelReplacement.BlurSigma < cfg.PixelBlurSigma.Min || rec.PixelReplacement.BlurSigma > cfg.PixelBlurSigma.Max {
		t.Fatalf("blur sigma out of range: %f", rec.PixelReplacement.BlurSigma)
	}
	if rec.PixelReplacement.Percent < cfg.PixelReplacePercent.Min || rec.PixelReplacement.Percent > cfg.PixelReplacePercent.Max {
		t.Fatalf("replace percent out of range: %f", rec.PixelReplacement.Percent)
	}
	if rec.PixelReplacement.Mode != "edge" {
		t.Fatalf("expected edge mode, got %q", rec.PixelReplacement.Mode)
	}
	if rec.PixelReplacement.AreaInsetPercent < cfg.PixelAreaEdgeInset.Min || rec.PixelReplacement.AreaInsetPercent > cfg.PixelAreaEdgeInset.Max {
		t.Fatalf("edge inset out of range: %f", rec.PixelReplacement.AreaInsetPercent)
	}
	switch rec.PixelReplacement.AreaEdge {
	case "top", "right", "bottom", "left":
	default:
		t.Fatalf("unexpected edge selected: %q", rec.PixelReplacement.AreaEdge)
	}
}

type stubSmartAnalyzer struct {
	area pixel.Area
	err  error
}

func (s stubSmartAnalyzer) Analyze(context.Context, string, *ffprobe.ProbeData, int) (pixel.Area, error) {
	return s.area, s.err
}

func TestGeneratePixelSmartModeUsesAnalyzerArea(t *testing.T) {
	cfg := baseCfg()
	cfg.PixelReplaceMode = "smart"
	probe := &ffprobe.ProbeData{Duration: 5, Video: &ffprobe.VideoStream{Fps: 30, Width: 1920, Height: 1080}}

	rec, err := GenerateWithSmartAnalyzer(context.Background(), cfg, probe, stubSmartAnalyzer{area: pixel.Area{X: 100, Y: 200, Width: 320, Height: 180, Confidence: 0.90}})
	if err != nil {
		t.Fatalf("GenerateWithSmartAnalyzer failed: %v", err)
	}
	if rec.PixelReplacement.RequestedMode != "smart" || rec.PixelReplacement.Mode != "smart" {
		t.Fatalf("expected smart mode without fallback, got requested=%q mode=%q", rec.PixelReplacement.RequestedMode, rec.PixelReplacement.Mode)
	}
	if rec.PixelReplacement.AreaX != 100 || rec.PixelReplacement.AreaY != 200 || rec.PixelReplacement.AreaWidth != 320 || rec.PixelReplacement.AreaHeight != 180 {
		t.Fatalf("unexpected smart area: %+v", rec.PixelReplacement)
	}
	if rec.PixelReplacement.SmartFallbackReason != "" {
		t.Fatalf("unexpected fallback reason: %q", rec.PixelReplacement.SmartFallbackReason)
	}
}

func TestGeneratePixelSmartModeFallsBackOnAnalyzerError(t *testing.T) {
	cfg := baseCfg()
	cfg.PixelReplaceMode = "smart"
	probe := &ffprobe.ProbeData{Duration: 5, Video: &ffprobe.VideoStream{Fps: 30, Width: 1920, Height: 1080}}

	rec, err := GenerateWithSmartAnalyzer(context.Background(), cfg, probe, stubSmartAnalyzer{err: errors.New("opencv unavailable")})
	if err != nil {
		t.Fatalf("GenerateWithSmartAnalyzer failed: %v", err)
	}
	if rec.PixelReplacement.RequestedMode != "smart" || rec.PixelReplacement.Mode != "edge" {
		t.Fatalf("expected smart request to fallback to edge, got requested=%q mode=%q", rec.PixelReplacement.RequestedMode, rec.PixelReplacement.Mode)
	}
	if rec.PixelReplacement.SmartFallbackReason == "" {
		t.Fatal("expected fallback reason")
	}
	switch rec.PixelReplacement.AreaEdge {
	case "top", "right", "bottom", "left":
	default:
		t.Fatalf("fallback must keep a valid edge area, got %q", rec.PixelReplacement.AreaEdge)
	}
}

func TestGeneratePixelSmartModeFallsBackOnLowConfidence(t *testing.T) {
	cfg := baseCfg()
	cfg.PixelReplaceMode = "smart"
	probe := &ffprobe.ProbeData{Duration: 5, Video: &ffprobe.VideoStream{Fps: 30, Width: 1920, Height: 1080}}

	rec, err := GenerateWithSmartAnalyzer(context.Background(), cfg, probe, stubSmartAnalyzer{area: pixel.Area{X: 1, Y: 2, Width: 3, Height: 4, Confidence: 0.10}})
	if err != nil {
		t.Fatalf("GenerateWithSmartAnalyzer failed: %v", err)
	}
	if rec.PixelReplacement.Mode != "edge" {
		t.Fatalf("expected low-confidence fallback to edge, got %q", rec.PixelReplacement.Mode)
	}
	if rec.PixelReplacement.SmartFallbackReason == "" {
		t.Fatal("expected low-confidence fallback reason")
	}
}

func TestGenerateMetadataFullModeCleanAndDiversify(t *testing.T) {
	cfg := baseCfg()
	probe := &ffprobe.ProbeData{Duration: 5, Video: &ffprobe.VideoStream{Fps: 30}}
	rec, err := Generate(cfg, probe)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if rec.Metadata.Mode != "clean_diversify" || !rec.Metadata.Clean {
		t.Fatalf("expected clean_diversify metadata with clean phase, got %+v", rec.Metadata)
	}
	if len(rec.Metadata.Diversify) == 0 {
		t.Fatal("expected diversify tags")
	}
}
