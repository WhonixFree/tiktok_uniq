package recipe

import (
	"testing"
	"videobatch/internal/config"
	"videobatch/internal/ffprobe"
)

func baseCfg() config.Config {
	return config.Config{
		InputDir: "in", OutputDir: "out", Seed: 42, AVSineMode: "independent", MetadataFullMode: "clean_diversify",
		AudioBaseSpeed: config.SpeedRange{MinPercent: 0.2, MaxPercent: 0.5},
		VideoBaseSpeed: config.SpeedRange{MinPercent: 0.2, MaxPercent: 0.5},
		AudioSine: config.SineParamsRange{AmplitudeMin: 0.01, AmplitudeMax: 0.02, FrequencyMin: 0.1, FrequencyMax: 0.2, PhaseMin: 0.0, PhaseMax: 1.0},
		VideoSine: config.SineParamsRange{AmplitudeMin: 0.01, AmplitudeMax: 0.02, FrequencyMin: 0.1, FrequencyMax: 0.2, PhaseMin: 0.0, PhaseMax: 1.0},
		FreezeCount: config.EventCountRange{Min: 2, Max: 2},
		ReplaceCount: config.EventCountRange{Min: 2, Max: 2},
		MinEventDistanceSec: 1.0,
		PixelReplacePercent: config.PercentRange{Min: 0.1, Max: 0.2},
		PixelBlurSigma: config.PercentRange{Min: 0.08, Max: 0.22},
		PixelReplaceMode: "edge",
		PixelAreaEdgeInset: config.PercentRange{Min: 0.01, Max: 0.02},
		PixelAreaSmartGrid: 8,
		NeighborOffsetMin: 1,
		NeighborOffsetMax: 2,
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
