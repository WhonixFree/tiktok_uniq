package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"runtime"
)

func ParseFlags(args []string, output io.Writer) (Config, error) {
	cfg := Config{}
	fs := flag.NewFlagSet("videobatch", flag.ContinueOnError)
	fs.SetOutput(output)

	fs.StringVar(&cfg.InputDir, "input", "./input", "input directory")
	fs.StringVar(&cfg.OutputDir, "output", "./output", "output directory")
	fs.StringVar(&cfg.TmpDir, "tmp", "./tmp", "temporary files directory")
	fs.StringVar(&cfg.LogsDir, "logs", "./logs", "logs directory")
	fs.BoolVar(&cfg.Recursive, "recursive", false, "scan input directory recursively")
	fs.IntVar(&cfg.Jobs, "jobs", 1, "number of parallel jobs")
	fs.IntVar(&cfg.ThreadsPerJob, "threads-per-job", runtime.NumCPU(), "threads available per job")
	fs.Int64Var(&cfg.Seed, "seed", 0, "deterministic seed for random choices")
	fs.BoolVar(&cfg.DryRun, "dry-run", false, "build a processing plan without rendering")
	fs.BoolVar(&cfg.Overwrite, "overwrite", false, "overwrite existing output files")
	fs.BoolVar(&cfg.Cleanup, "cleanup", true, "remove per-file tmp files after successful processing")

	fs.BoolVar(&cfg.TrimEnabled, "trim", false, "enable trim")
	fs.Float64Var(&cfg.TrimStartMin, "trim-start-min", 0, "minimum trim from start in seconds")
	fs.Float64Var(&cfg.TrimStartMax, "trim-start-max", 0, "maximum trim from start in seconds")
	fs.Float64Var(&cfg.TrimEndMin, "trim-end-min", 0, "minimum trim from end in seconds")
	fs.Float64Var(&cfg.TrimEndMax, "trim-end-max", 0, "maximum trim from end in seconds")

	fs.BoolVar(&cfg.CropEnabled, "crop", false, "enable crop")
	fs.Float64Var(&cfg.CropMinPercent, "crop-min-percent", 0, "minimum crop percent")
	fs.Float64Var(&cfg.CropMaxPercent, "crop-max-percent", 0, "maximum crop percent")

	fs.StringVar(&cfg.ColorPreset, "color-preset", "off", "color preset name or random")
	fs.StringVar(&cfg.ColorStrength, "color-strength", "soft", "color strength: soft|medium|hard")
	fs.StringVar(&cfg.ColorConfigDir, "color-config-dir", "./configs/color_presets", "color presets directory")
	fs.StringVar(&cfg.Captions, "captions", "off", "captions mode: off|auto")
	fs.StringVar(&cfg.CaptionTemplate, "caption-template", "default", "caption template name or random")
	fs.StringVar(&cfg.CaptionTemplateDir, "caption-template-dir", "./configs/caption_templates", "caption templates directory")
	fs.StringVar(&cfg.CaptionLanguage, "caption-language", "auto", "caption language")
	fs.StringVar(&cfg.CaptionModel, "caption-model", "base", "caption model")
	fs.StringVar(&cfg.AudioEnvelope, "audio-envelope", "off", "audio envelope mode: off|python")
	fs.StringVar(&cfg.AudioEnvelopeConfig, "audio-envelope-config", "", "audio envelope config path")
	fs.BoolVar(&cfg.MusicEnabled, "music", false, "enable background music")
	fs.StringVar(&cfg.MusicDir, "music-dir", "./assets/music", "music directory")
	fs.Float64Var(&cfg.MusicVolume, "music-volume", 0.06, "background music volume")
	fs.BoolVar(&cfg.MusicDucking, "music-ducking", false, "enable music ducking")
	fs.StringVar(&cfg.DuckingMod, "ducking-mod", "standard", "ducking mode: soft|standard|aggressive")
	fs.StringVar(&cfg.StreamOverlayDir, "stream-overlay-dir", "./assets/stream_overlays", "stream overlay directory")
	fs.Float64Var(&cfg.StreamOverlayOpacity, "stream-overlay-opacity", 0.02, "stream overlay opacity (recommended: 0.01-0.03)")
	fs.StringVar(&cfg.MetadataPolicy, "metadata-policy", "./configs/metadata_policy.json", "metadata policy path")
	fs.Float64Var(&cfg.TemporalShift, "temporal-shift", 0, "temporal shift multiplier delta")
	fs.Float64Var(&cfg.FPSTweak, "fps-tweak", 0, "fps override/tweak")
	fs.StringVar(&cfg.CodecProfile, "codec-profile", "balanced", "codec profile: fast|balanced|strong")

	fs.Float64Var(&cfg.AudioBaseSpeed.MinPercent, "audio-base-speed-min-percent", 0.2, "audio base speed min percent delta")
	fs.Float64Var(&cfg.AudioBaseSpeed.MaxPercent, "audio-base-speed-max-percent", 1.2, "audio base speed max percent delta")
	fs.Float64Var(&cfg.VideoBaseSpeed.MinPercent, "video-base-speed-min-percent", 0.2, "video base speed min percent delta")
	fs.Float64Var(&cfg.VideoBaseSpeed.MaxPercent, "video-base-speed-max-percent", 1.2, "video base speed max percent delta")
	fs.StringVar(&cfg.AVSineMode, "av-sine-mode", "lock", "sine mode: lock|independent")
	fs.Float64Var(&cfg.AudioSine.AmplitudeMin, "audio-sine-amp-min", 0.001, "audio sine amplitude min")
	fs.Float64Var(&cfg.AudioSine.AmplitudeMax, "audio-sine-amp-max", 0.01, "audio sine amplitude max")
	fs.Float64Var(&cfg.VideoSine.AmplitudeMin, "video-sine-amp-min", 0.001, "video sine amplitude min")
	fs.Float64Var(&cfg.VideoSine.AmplitudeMax, "video-sine-amp-max", 0.01, "video sine amplitude max")
	fs.Float64Var(&cfg.AudioSine.FrequencyMin, "audio-sine-freq-min", 0.05, "audio sine frequency min")
	fs.Float64Var(&cfg.AudioSine.FrequencyMax, "audio-sine-freq-max", 0.3, "audio sine frequency max")
	fs.Float64Var(&cfg.VideoSine.FrequencyMin, "video-sine-freq-min", 0.05, "video sine frequency min")
	fs.Float64Var(&cfg.VideoSine.FrequencyMax, "video-sine-freq-max", 0.3, "video sine frequency max")
	fs.IntVar(&cfg.FreezeCount.Min, "freeze-count-min", 1, "minimum freeze event count")
	fs.IntVar(&cfg.FreezeCount.Max, "freeze-count-max", 6, "maximum freeze event count")
	fs.IntVar(&cfg.ReplaceCount.Min, "replace-count-min", 1, "minimum replace event count")
	fs.IntVar(&cfg.ReplaceCount.Max, "replace-count-max", 6, "maximum replace event count")
	fs.Float64Var(&cfg.MinEventDistanceSec, "min-event-distance-sec", 0.4, "minimum distance between events in seconds")
	fs.Float64Var(&cfg.PixelReplacePercent.Min, "pixel-replace-percent-min", 0.05, "pixel replace percent min")
	fs.Float64Var(&cfg.PixelReplacePercent.Max, "pixel-replace-percent-max", 0.4, "pixel replace percent max")
	fs.Float64Var(&cfg.PixelBlurSigma.Min, "pixel-blur-sigma-min", 0.08, "weak gaussian blur sigma min")
	fs.Float64Var(&cfg.PixelBlurSigma.Max, "pixel-blur-sigma-max", 0.22, "weak gaussian blur sigma max")
	fs.StringVar(&cfg.PixelReplaceMode, "pixel-replace-mode", "edge", "pixel replacement mode: edge|smart")
	fs.Float64Var(&cfg.PixelAreaEdgeInset.Min, "pixel-edge-inset-min", 0.01, "edge mode inset min percent")
	fs.Float64Var(&cfg.PixelAreaEdgeInset.Max, "pixel-edge-inset-max", 0.08, "edge mode inset max percent")
	fs.IntVar(&cfg.PixelAreaSmartGrid, "pixel-smart-grid", 8, "smart mode analysis grid")
	fs.IntVar(&cfg.NeighborOffsetMin, "neighbor-offset-min", 1, "neighbor offset min")
	fs.IntVar(&cfg.NeighborOffsetMax, "neighbor-offset-max", 2, "neighbor offset max")
	fs.StringVar(&cfg.MetadataFullMode, "metadata-full-mode", "clean_diversify", "metadata full mode")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (cfg Config) Validate() error {
	var errs []error
	if cfg.InputDir == "" {
		errs = append(errs, errors.New("--input must not be empty"))
	}
	if cfg.OutputDir == "" {
		errs = append(errs, errors.New("--output must not be empty"))
	}
	if cfg.Jobs < 1 {
		errs = append(errs, errors.New("--jobs must be >= 1"))
	}
	if cfg.ThreadsPerJob < 1 {
		errs = append(errs, errors.New("--threads-per-job must be >= 1"))
	}
	if cfg.TrimStartMax < cfg.TrimStartMin || cfg.TrimEndMax < cfg.TrimEndMin {
		errs = append(errs, errors.New("trim max must be >= min"))
	}
	if cfg.CropEnabled {
		if cfg.CropMinPercent <= 0 || cfg.CropMaxPercent <= 0 || cfg.CropMinPercent >= 100 || cfg.CropMaxPercent >= 100 || cfg.CropMaxPercent < cfg.CropMinPercent {
			errs = append(errs, errors.New("invalid crop percent range"))
		}
	}
	if !oneOf(cfg.AVSineMode, "lock", "independent") {
		errs = append(errs, errors.New("--av-sine-mode must be one of lock|independent"))
	}
	if err := validatePercentRange(cfg.AudioBaseSpeed.MinPercent, cfg.AudioBaseSpeed.MaxPercent, "audio base speed"); err != nil {
		errs = append(errs, err)
	}
	if err := validatePercentRange(cfg.VideoBaseSpeed.MinPercent, cfg.VideoBaseSpeed.MaxPercent, "video base speed"); err != nil {
		errs = append(errs, err)
	}
	if err := validateMinMax(cfg.AudioSine.AmplitudeMin, cfg.AudioSine.AmplitudeMax, "audio sine amplitude"); err != nil {
		errs = append(errs, err)
	}
	if err := validateMinMax(cfg.VideoSine.AmplitudeMin, cfg.VideoSine.AmplitudeMax, "video sine amplitude"); err != nil {
		errs = append(errs, err)
	}
	if err := validateMinMax(cfg.AudioSine.FrequencyMin, cfg.AudioSine.FrequencyMax, "audio sine frequency"); err != nil {
		errs = append(errs, err)
	}
	if err := validateMinMax(cfg.VideoSine.FrequencyMin, cfg.VideoSine.FrequencyMax, "video sine frequency"); err != nil {
		errs = append(errs, err)
	}
	if cfg.FreezeCount.Min < 0 || cfg.FreezeCount.Max < cfg.FreezeCount.Min {
		errs = append(errs, errors.New("invalid freeze count range"))
	}
	if cfg.ReplaceCount.Min < 0 || cfg.ReplaceCount.Max < cfg.ReplaceCount.Min {
		errs = append(errs, errors.New("invalid replace count range"))
	}
	if cfg.MinEventDistanceSec < 0 {
		errs = append(errs, errors.New("--min-event-distance-sec must be >= 0"))
	}
	if err := validatePercentRange(cfg.PixelReplacePercent.Min, cfg.PixelReplacePercent.Max, "pixel replace percent"); err != nil {
		errs = append(errs, err)
	}
	if err := validateMinMax(cfg.PixelBlurSigma.Min, cfg.PixelBlurSigma.Max, "pixel blur sigma"); err != nil {
		errs = append(errs, err)
	}
	if !oneOf(cfg.PixelReplaceMode, "edge", "smart") {
		errs = append(errs, errors.New("--pixel-replace-mode must be edge|smart"))
	}
	if err := validatePercentRange(cfg.PixelAreaEdgeInset.Min, cfg.PixelAreaEdgeInset.Max, "pixel edge inset"); err != nil {
		errs = append(errs, err)
	}
	if cfg.NeighborOffsetMin < 0 || cfg.NeighborOffsetMax < cfg.NeighborOffsetMin {
		errs = append(errs, errors.New("invalid neighbor offset range"))
	}
	if cfg.MetadataFullMode != "clean_diversify" {
		errs = append(errs, errors.New("--metadata-full-mode must be clean_diversify"))
	}
	return errors.Join(errs...)
}

func validateMinMax(min, max float64, name string) error {
	if min < 0 || max < min {
		return fmt.Errorf("invalid %s range", name)
	}
	return nil
}
func validatePercentRange(min, max float64, name string) error {
	if min < 0 || max < min || max > 100 {
		return fmt.Errorf("invalid %s percent range", name)
	}
	return nil
}
func oneOf(value string, allowed ...string) bool {
	for _, c := range allowed {
		if value == c {
			return true
		}
	}
	return false
}
