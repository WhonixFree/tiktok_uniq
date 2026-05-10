package config

import (
	"io"
	"testing"
)

func TestValidateNewRanges(t *testing.T) {
	cfg := Config{InputDir: "in", OutputDir: "out", TmpDir: "tmp", LogsDir: "logs", Jobs: 1, ThreadsPerJob: 1, AVSineMode: "bad", MetadataFullMode: "clean_diversify"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidatePasses(t *testing.T) {
	cfg := Config{InputDir: "in", OutputDir: "out", TmpDir: "tmp", LogsDir: "logs", Jobs: 1, ThreadsPerJob: 1, AVSineMode: "lock", MetadataFullMode: "clean_diversify",
		AudioBaseSpeed: SpeedRange{0.1, 1}, VideoBaseSpeed: SpeedRange{0.1, 1},
		AudioSine: SineParamsRange{0, 1, 0, 1, 0, 1}, VideoSine: SineParamsRange{0, 1, 0, 1, 0, 1},
		FreezeCount: EventCountRange{0, 2}, ReplaceCount: EventCountRange{0, 2}, PixelReplacePercent: PercentRange{0, 1}, PixelBlurSigma: PercentRange{0.01, 0.2}, PixelReplaceMode: "edge", PixelAreaEdgeInset: PercentRange{0, 1}, NeighborOffsetMin: 0, NeighborOffsetMax: 1,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestValidateIgnoresCropRangeWhenCropDisabled(t *testing.T) {
	cfg := validTestConfig()
	cfg.CropEnabled = false
	cfg.CropMinPercent = 0
	cfg.CropMaxPercent = 0

	if err := cfg.Validate(); err != nil {
		t.Fatalf("crop-specific validation must be skipped when crop is disabled, got: %v", err)
	}
}

func TestValidateRejectsCropRangeWhenCropEnabled(t *testing.T) {
	cfg := validTestConfig()
	cfg.CropEnabled = true
	cfg.CropMinPercent = 0
	cfg.CropMaxPercent = 0

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected crop percent validation error when crop is enabled")
	}
}

func TestParseFlagsRejectsRemovedCropPositionFlag(t *testing.T) {
	if _, err := ParseFlags([]string{"--crop-position", "top"}, io.Discard); err == nil {
		t.Fatal("expected removed --crop-position flag to be rejected")
	}
}

func TestValidatePixelBlurRange(t *testing.T) {
	cfg := Config{InputDir: "in", OutputDir: "out", TmpDir: "tmp", LogsDir: "logs", Jobs: 1, ThreadsPerJob: 1, AVSineMode: "lock", MetadataFullMode: "clean_diversify",
		AudioBaseSpeed: SpeedRange{0.1, 1}, VideoBaseSpeed: SpeedRange{0.1, 1},
		AudioSine: SineParamsRange{0, 1, 0, 1, 0, 1}, VideoSine: SineParamsRange{0, 1, 0, 1, 0, 1},
		FreezeCount: EventCountRange{0, 2}, ReplaceCount: EventCountRange{0, 2}, PixelReplacePercent: PercentRange{0, 1}, PixelBlurSigma: PercentRange{0.3, 0.2}, PixelReplaceMode: "edge", PixelAreaEdgeInset: PercentRange{0, 1}, NeighborOffsetMin: 0, NeighborOffsetMax: 1,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected blur range validation error")
	}
}

func validTestConfig() Config {
	return Config{InputDir: "in", OutputDir: "out", TmpDir: "tmp", LogsDir: "logs", Jobs: 1, ThreadsPerJob: 1, AVSineMode: "lock", MetadataFullMode: "clean_diversify",
		AudioBaseSpeed: SpeedRange{0.1, 1}, VideoBaseSpeed: SpeedRange{0.1, 1},
		AudioSine: SineParamsRange{0, 1, 0, 1, 0, 1}, VideoSine: SineParamsRange{0, 1, 0, 1, 0, 1},
		FreezeCount: EventCountRange{0, 2}, ReplaceCount: EventCountRange{0, 2}, PixelReplacePercent: PercentRange{0, 1}, PixelBlurSigma: PercentRange{0.01, 0.2}, PixelReplaceMode: "edge", PixelAreaEdgeInset: PercentRange{0, 1}, NeighborOffsetMin: 0, NeighborOffsetMax: 1,
	}
}
