package config

import "testing"

func TestValidateNewRanges(t *testing.T) {
	cfg := Config{InputDir:"in",OutputDir:"out",TmpDir:"tmp",LogsDir:"logs",Jobs:1,ThreadsPerJob:1,AVSineMode:"bad",MetadataFullMode:"clean_diversify"}
	if err := cfg.Validate(); err == nil { t.Fatal("expected error") }
}

func TestValidatePasses(t *testing.T) {
	cfg := Config{InputDir:"in",OutputDir:"out",TmpDir:"tmp",LogsDir:"logs",Jobs:1,ThreadsPerJob:1,AVSineMode:"lock",MetadataFullMode:"clean_diversify",
		AudioBaseSpeed:SpeedRange{0.1,1}, VideoBaseSpeed:SpeedRange{0.1,1},
		AudioSine:SineParamsRange{0,1,0,1,0,1}, VideoSine:SineParamsRange{0,1,0,1,0,1},
		FreezeCount:EventCountRange{0,2}, ReplaceCount:EventCountRange{0,2}, PixelReplacePercent:PercentRange{0,1}, PixelBlurSigma:PercentRange{0.01,0.2}, PixelReplaceMode:"edge", PixelAreaEdgeInset:PercentRange{0,1}, NeighborOffsetMin:0, NeighborOffsetMax:1,
	}
	if err := cfg.Validate(); err != nil { t.Fatalf("unexpected: %v", err) }
}

func TestValidatePixelBlurRange(t *testing.T) {
	cfg := Config{InputDir:"in",OutputDir:"out",TmpDir:"tmp",LogsDir:"logs",Jobs:1,ThreadsPerJob:1,AVSineMode:"lock",MetadataFullMode:"clean_diversify",
		AudioBaseSpeed:SpeedRange{0.1,1}, VideoBaseSpeed:SpeedRange{0.1,1},
		AudioSine:SineParamsRange{0,1,0,1,0,1}, VideoSine:SineParamsRange{0,1,0,1,0,1},
		FreezeCount:EventCountRange{0,2}, ReplaceCount:EventCountRange{0,2}, PixelReplacePercent:PercentRange{0,1}, PixelBlurSigma:PercentRange{0.3,0.2}, PixelReplaceMode:"edge", PixelAreaEdgeInset:PercentRange{0,1}, NeighborOffsetMin:0, NeighborOffsetMax:1,
	}
	if err := cfg.Validate(); err == nil { t.Fatal("expected blur range validation error") }
}
