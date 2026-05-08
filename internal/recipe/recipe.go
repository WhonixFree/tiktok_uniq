package recipe

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"videobatch/internal/config"
	"videobatch/internal/ffprobe"
)

type Recipe struct {
	InputPath  string
	OutputPath string

	TrimStart float64
	TrimEnd   float64

	CropW int64
	CropH int64
	CropX int64
	CropY int64

	ColorPresetPath string
	ColorStrength   float64

	CaptionsEnabled     bool
	CaptionsOutputPath  string
	CaptionTemplatePath string
	CaptionLanguage     string
	CaptionModel        string

	AudioEnvelopeEnabled bool
	AudioEnvelope        string

	MusicPath      string
	MusicVolume    float64
	DuckingEnabled bool
	DuckingRatio   float64

	StreamOverlayPath string
	OverlayOpacity    float64

	TemporalShift float64
	FpsTweak      float64
	VideoCodec    string
	AudioCodec    string

	Profile string
}

func Generate(cfg config.Config, probe *ffprobe.ProbeData) (*Recipe, error) {
	rec := Recipe{}
	rng := rand.New(rand.NewSource(cfg.Seed))

	rec.InputPath = cfg.InputDir
	rec.OutputPath = cfg.OutputDir

	//Trim
	rec.TrimStart = cfg.TrimStartMin + rng.Float64()*(cfg.TrimStartMax-cfg.TrimStartMin)
	rec.TrimEnd = cfg.TrimEndMin + rng.Float64()*(cfg.TrimEndMax-cfg.TrimEndMin)

	//Crop
	CropWPercent := cfg.CropMinPercent + rng.Float64()*(cfg.CropMaxPercent-cfg.CropMinPercent)
	CropHPercent := cfg.CropMinPercent + rng.Float64()*(cfg.CropMaxPercent-cfg.CropMinPercent)
	finalW := int64(float64(probe.Video.Width) * CropWPercent)
	finalH := int64(float64(probe.Video.Height) * CropHPercent)
	maxX := max(probe.Video.Width-finalW, 0)
	maxY := max(probe.Video.Height-finalH, 0)
	cropX := int64(rng.Float64() * float64(maxX))
	cropY := int64(rng.Float64() * float64(maxY))
	rec.CropW = finalW
	rec.CropH = finalH
	rec.CropX = cropX
	rec.CropY = cropY

	//Color
	colorPresetPath := ""
	if cfg.ColorPreset != "random" {
		colorPresetPath = filepath.Join(cfg.ColorConfigDir, cfg.ColorPreset+".json")
		if _, err := os.Stat(colorPresetPath); err != nil {
			return nil, errors.New("color preset does not exist")
		}
	} else {
		entries, err := os.ReadDir(cfg.ColorConfigDir)
		if err != nil {
			return nil, err
		}

		var jsonFiles []string
		for _, file := range entries {
			if !file.IsDir() && strings.HasSuffix(strings.ToLower(file.Name()), ".json") {
				jsonFiles = append(jsonFiles, file.Name())
			}
		}
		if len(jsonFiles) == 0 {
			return nil, errors.New("no color config files found")
		}
		randomIndex := rng.Intn(len(jsonFiles))
		colorPresetPath = filepath.Join(cfg.ColorConfigDir, jsonFiles[randomIndex])
	}
	rec.ColorPresetPath = colorPresetPath

	if cfg.ColorStrength == "soft" {
		rec.ColorStrength = 0.9 + 0.1*rng.Float64()
	} else if cfg.ColorStrength == "medium" {
		rec.ColorStrength = 0.8 + 0.1*rng.Float64()
	} else if cfg.ColorStrength == "hard" {
		rec.ColorStrength = 0.7 + 0.1*rng.NormFloat64()
	} else {
		return nil, errors.New("invalid color strength")
	}

	//Captions
	if cfg.Captions == "off" {
		rec.CaptionsEnabled = false
	} else if cfg.Captions == "auto" {
		baseName := strings.TrimSuffix(filepath.Base(rec.InputPath), filepath.Ext(rec.InputPath))
		rec.CaptionsOutputPath = filepath.Join(cfg.TmpDir, baseName+"_captions.ass")

		rec.CaptionModel = cfg.CaptionModel
		rec.CaptionLanguage = cfg.CaptionLanguage

		if cfg.CaptionTemplate != "random" {
			rec.CaptionTemplatePath = filepath.Join(cfg.CaptionTemplateDir, cfg.CaptionTemplate+".json")
			if _, err := os.Stat(rec.CaptionTemplatePath); err != nil {
				return nil, errors.New("caption template does not exist")
			}
		} else {
			entries, err := os.ReadDir(cfg.CaptionTemplateDir)
			if err != nil {
				return nil, errors.New("failed to read caption template directory")
			}

			var jsonFiles []string
			for _, file := range entries {
				if !file.IsDir() && strings.HasSuffix(strings.ToLower(file.Name()), ".json") {
					jsonFiles = append(jsonFiles, file.Name())
				}
			}
			if len(jsonFiles) == 0 {
				return nil, errors.New("no caption template files found")
			}
			randomIndex := rng.Intn(len(jsonFiles))
			rec.CaptionTemplatePath = filepath.Join(cfg.CaptionTemplateDir, jsonFiles[randomIndex])
		}
	} else {
		return nil, fmt.Errorf("invalid captions mode: %s", cfg.Captions)
	}

	//Audio
	if cfg.AudioEnvelope == "off" {
		rec.AudioEnvelopeEnabled = false
	} else if cfg.AudioEnvelope == "python" {
		rec.AudioEnvelopeEnabled = true

		if cfg.AudioEnvelopeConfig == "" {
			return nil, errors.New("audio envelope config path is required")
		}
		if !strings.HasSuffix(strings.ToLower(cfg.AudioEnvelopeConfig), ".json") {
			return nil, errors.New("audio envelope config must be a .json file")
		}
		if _, err := os.Stat(cfg.AudioEnvelopeConfig); err != nil {
			return nil, errors.New("audio envelope does not exist")
		}

		rec.AudioEnvelope = cfg.AudioEnvelope
	} else {
		return nil, fmt.Errorf("invalid audio envelope mode: %s", cfg.AudioEnvelope)
	}

	//Music
	musicFiles, err := filepath.Glob(filepath.Join(cfg.MusicDir, "*"))
	if err != nil || len(musicFiles) == 0 {
		return nil, errors.New("no music files found")
	}
	rec.MusicPath = musicFiles[rng.Intn(len(musicFiles))]
	rec.MusicVolume = cfg.MusicVolume
	rec.DuckingEnabled = cfg.MusicDucking
	rec.DuckingRatio = cfg.DuckRatio

	//StreamOverlay
	overlayFiles, err := filepath.Glob(filepath.Join(cfg.StreamOverlayDir, "*"))
	if err != nil || len(overlayFiles) == 0 {
		return nil, errors.New("no stream overlay files found")
	}
	rec.StreamOverlayPath = overlayFiles[rng.Intn(len(overlayFiles))]
	rec.OverlayOpacity = cfg.StreamOverlayOpacity

	//Temporal/FPS/Codec
	rec.TemporalShift = cfg.TemporalShift
	rec.FpsTweak = cfg.FPSTweak
	rec.VideoCodec = "libx264"
	rec.AudioCodec = "aac"
	rec.Profile = cfg.CodecProfile

	return &rec, nil
}
