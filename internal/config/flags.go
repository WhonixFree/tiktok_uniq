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
	fs.StringVar(&cfg.CropPosition, "crop-position", "center", "crop position: random|center|top|bottom")

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

	fs.StringVar(&cfg.MusicDir, "music-dir", "./assets/music", "music directory")
	fs.Float64Var(&cfg.MusicVolume, "music-volume", 0.06, "background music volume")
	fs.BoolVar(&cfg.MusicDucking, "music-ducking", false, "enable music ducking")
	fs.Float64Var(&cfg.DuckRatio, "duck-ratio", 6, "ducking ratio")
	fs.IntVar(&cfg.DuckAttackMS, "duck-attack-ms", 20, "ducking attack in milliseconds")
	fs.IntVar(&cfg.DuckReleaseMS, "duck-release-ms", 300, "ducking release in milliseconds")

	fs.StringVar(&cfg.StreamOverlayDir, "stream-overlay-dir", "./assets/stream_overlays", "stream overlay directory")
	fs.Float64Var(&cfg.StreamOverlayOpacity, "stream-overlay-opacity", 0.02, "stream overlay opacity")
	fs.StringVar(&cfg.StreamOverlayMode, "stream-overlay-mode", "normal", "stream overlay mode: normal|stealth")
	fs.BoolVar(&cfg.StreamOverlayRandom, "stream-overlay-random", false, "choose stream overlay randomly")

	fs.StringVar(&cfg.Metadata, "metadata", "off", "metadata mode: off|read|clean|clean-and-diversify")
	fs.StringVar(&cfg.MetadataPolicy, "metadata-policy", "./configs/metadata_policy.json", "metadata policy path")

	fs.Float64Var(&cfg.TemporalShift, "temporal-shift", 0, "temporal shift multiplier delta")
	fs.Float64Var(&cfg.FPSTweak, "fps-tweak", 0, "fps override/tweak")
	fs.StringVar(&cfg.CodecProfile, "codec-profile", "balanced", "codec profile: fast|balanced|strong")

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
	if cfg.TmpDir == "" {
		errs = append(errs, errors.New("--tmp must not be empty"))
	}
	if cfg.LogsDir == "" {
		errs = append(errs, errors.New("--logs must not be empty"))
	}
	if cfg.Jobs < 1 {
		errs = append(errs, errors.New("--jobs must be >= 1"))
	}
	if cfg.ThreadsPerJob < 1 {
		errs = append(errs, errors.New("--threads-per-job must be >= 1"))
	}
	if cfg.TrimStartMin < 0 || cfg.TrimStartMax < 0 || cfg.TrimEndMin < 0 || cfg.TrimEndMax < 0 {
		errs = append(errs, errors.New("trim values must be >= 0"))
	}
	if cfg.TrimStartMax < cfg.TrimStartMin {
		errs = append(errs, errors.New("--trim-start-max must be >= --trim-start-min"))
	}
	if cfg.TrimEndMax < cfg.TrimEndMin {
		errs = append(errs, errors.New("--trim-end-max must be >= --trim-end-min"))
	}
	if cfg.CropMinPercent < 0 || cfg.CropMaxPercent < 0 {
		errs = append(errs, errors.New("crop percent values must be >= 0"))
	}
	if cfg.CropMaxPercent < cfg.CropMinPercent {
		errs = append(errs, errors.New("--crop-max-percent must be >= --crop-min-percent"))
	}
	if !oneOf(cfg.CropPosition, "random", "center", "top", "bottom") {
		errs = append(errs, errors.New("--crop-position must be one of random|center|top|bottom"))
	}
	if !oneOf(cfg.ColorStrength, "soft", "medium", "hard") {
		errs = append(errs, errors.New("--color-strength must be one of soft|medium|hard"))
	}
	if !oneOf(cfg.Captions, "off", "auto") {
		errs = append(errs, errors.New("--captions must be one of off|auto"))
	}
	if !oneOf(cfg.AudioEnvelope, "off", "python") {
		errs = append(errs, errors.New("--audio-envelope must be one of off|python"))
	}
	if cfg.MusicVolume < 0 {
		errs = append(errs, errors.New("--music-volume must be >= 0"))
	}
	if cfg.DuckRatio <= 0 {
		errs = append(errs, errors.New("--duck-ratio must be > 0"))
	}
	if cfg.DuckAttackMS < 0 || cfg.DuckReleaseMS < 0 {
		errs = append(errs, errors.New("ducking timings must be >= 0"))
	}
	if cfg.StreamOverlayOpacity < 0 || cfg.StreamOverlayOpacity > 1 {
		errs = append(errs, errors.New("--stream-overlay-opacity must be between 0 and 1"))
	}
	if !oneOf(cfg.StreamOverlayMode, "normal", "stealth") {
		errs = append(errs, errors.New("--stream-overlay-mode must be one of normal|stealth"))
	}
	if cfg.StreamOverlayMode == "stealth" && (cfg.StreamOverlayOpacity < 0.01 || cfg.StreamOverlayOpacity > 0.03) {
		errs = append(errs, errors.New("--stream-overlay-opacity must be 0.01-0.03 in stealth mode"))
	}
	if !oneOf(cfg.Metadata, "off", "read", "clean", "clean-and-diversify") {
		errs = append(errs, errors.New("--metadata must be one of off|read|clean|clean-and-diversify"))
	}
	if !oneOf(cfg.CodecProfile, "fast", "balanced", "strong") {
		errs = append(errs, errors.New("--codec-profile must be one of fast|balanced|strong"))
	}

	return errors.Join(errs...)
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
