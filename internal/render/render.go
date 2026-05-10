package render

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"videobatch/internal/config"
	"videobatch/internal/ffprobe"
	"videobatch/internal/recipe"
	"videobatch/internal/workerpool"
)

const avSyncToleranceSec = 0.75

// Stage records the recommended V3 render order. Tests and job logs can assert
// this order independently from the generated FFmpeg command details.
type Stage string

const (
	StageGeometryColor Stage = "geometry/color"
	StageWeakBlur      Stage = "weak gaussian blur"
	StagePixelReplace  Stage = "pixel replacement"
	StageTemporal      Stage = "temporal effects"
	StageOverlay       Stage = "overlay"
	StageEncodeMux     Stage = "final encode/mux"
)

var RecommendedOrder = []Stage{
	StageGeometryColor,
	StageWeakBlur,
	StagePixelReplace,
	StageTemporal,
	StageOverlay,
	StageEncodeMux,
}

type Pipeline struct {
	Stages       []Stage
	FilterGraph  string
	VideoLabel   string
	AudioFilters []string
}

type Runner struct {
	FFmpegPath  string
	FFprobePath string
}

func (r Runner) Render(ctx context.Context, cfg config.Config, job workerpool.Job, probe *ffprobe.ProbeData, rec *recipe.Recipe) error {
	ffmpegPath, err := executable(r.FFmpegPath, "ffmpeg")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(job.OutputPath), 0o755); err != nil {
		return err
	}

	pipeline, err := BuildPipeline(cfg, probe, rec)
	if err != nil {
		return err
	}

	args := []string{"-hide_banner", "-loglevel", "error"}
	if cfg.Overwrite {
		args = append(args, "-y")
	} else {
		args = append(args, "-n")
	}
	args = append(args, "-i", job.InputPath, "-filter_complex", pipeline.FilterGraph, "-map", pipeline.VideoLabel)
	if probe.Audio != nil {
		args = append(args, "-map", "0:a:0")
		if len(pipeline.AudioFilters) > 0 {
			args = append(args, "-af", strings.Join(pipeline.AudioFilters, ","))
		}
		args = append(args, "-c:a", "aac", "-b:a", "128k")
	}
	args = append(args, codecArgs(cfg.CodecProfile)...)
	args = append(args, "-pix_fmt", "yuv420p", "-movflags", "+faststart", "-shortest", job.OutputPath)

	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg render failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return r.ValidateOutput(ctx, job.OutputPath)
}

func (r Runner) ValidateOutput(ctx context.Context, outputPath string) error {
	ffprobePath, err := executable(r.FFprobePath, "ffprobe")
	if err != nil {
		return err
	}
	info, err := probeRendered(ctx, ffprobePath, outputPath)
	if err != nil {
		return err
	}
	if info.Format.Duration <= 0 || info.VideoDuration <= 0 {
		return fmt.Errorf("invalid rendered media durations: format=%.3f video=%.3f", info.Format.Duration, info.VideoDuration)
	}
	if info.AudioDuration > 0 && math.Abs(info.VideoDuration-info.AudioDuration) > avSyncToleranceSec {
		return fmt.Errorf("a/v sync check failed: video=%.3fs audio=%.3fs tolerance=%.3fs", info.VideoDuration, info.AudioDuration, avSyncToleranceSec)
	}
	if stat, err := os.Stat(outputPath); err != nil {
		return err
	} else if stat.Size() == 0 {
		return errors.New("rendered output is empty")
	}
	return nil
}

func BuildPipeline(cfg config.Config, probe *ffprobe.ProbeData, rec *recipe.Recipe) (Pipeline, error) {
	if probe == nil || probe.Video == nil {
		return Pipeline{}, errors.New("video probe data is required")
	}
	if rec == nil {
		return Pipeline{}, errors.New("recipe is required")
	}

	width := evenPositive(probe.Video.Width)
	height := evenPositive(probe.Video.Height)
	area := pixelArea(rec.PixelReplacement, width, height)
	neighborX := clamp(area.x+rec.PixelReplacement.NeighborOffset, 0, width-area.w)
	neighborY := area.y
	if neighborX == area.x && area.x > 0 {
		neighborX = area.x - 1
	}
	replaceAlpha := clampFloat(rec.PixelReplacement.Percent/100, 0.0001, 1)
	blurSigma := math.Max(0, rec.PixelReplacement.BlurSigma)
	videoSpeed := speedFactor(rec.VideoSpeed.BasePercent)
	overlayOpacity := clampFloat(cfg.StreamOverlayOpacity, 0, 1)

	geometryInput := "[0:v]"
	geometrySteps := []string{}
	if cfg.CropEnabled {
		crop := cropGeometry(width, height, cfg.CropMaxPercent)
		geometrySteps = append(geometrySteps, fmt.Sprintf("crop=%d:%d:%d:%d", crop.w, crop.h, crop.x, crop.y))
	}
	geometrySteps = append(geometrySteps, "scale=trunc(iw/2)*2:trunc(ih/2)*2", "setsar=1", colorFilter(cfg.ColorStrength), "format=rgba")
	geometryColor := fmt.Sprintf("%s%s[gcolor]", geometryInput, strings.Join(geometrySteps, ","))
	blur := fmt.Sprintf("[gcolor]gblur=sigma=%.4f[blurred]", blurSigma)
	pixel := fmt.Sprintf("[blurred]split=2[pixelbase][pixelsrc];[pixelsrc]crop=%d:%d:%d:%d,format=rgba,colorchannelmixer=aa=%.6f[pixelpatch];[pixelbase][pixelpatch]overlay=%d:%d[pixel]", area.w, area.h, neighborX, neighborY, replaceAlpha, area.x, area.y)
	temporal := fmt.Sprintf("[pixel]setpts=PTS/%.8f[temporal]", videoSpeed)
	overlay := fmt.Sprintf("color=c=white@%.6f:s=%dx%d:r=30,format=rgba[streamoverlay];[temporal][streamoverlay]overlay=0:0:shortest=1[vout]", overlayOpacity, width, height)

	return Pipeline{
		Stages:      append([]Stage(nil), RecommendedOrder...),
		FilterGraph: strings.Join([]string{geometryColor, blur, pixel, temporal, overlay}, ";"),
		VideoLabel:  "[vout]",
		AudioFilters: []string{
			fmt.Sprintf("atempo=%.8f", clampFloat(speedFactor(rec.AudioSpeed.BasePercent), 0.5, 2.0)),
		},
	}, nil
}

type renderProbe struct {
	Format        renderFormat
	VideoDuration float64
	AudioDuration float64
}

type renderFormat struct {
	Duration float64
}

type rawProbe struct {
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
	Streams []struct {
		CodecType string `json:"codec_type"`
		Duration  string `json:"duration"`
	} `json:"streams"`
}

func probeRendered(ctx context.Context, ffprobePath, outputPath string) (renderProbe, error) {
	cmd := exec.CommandContext(ctx, ffprobePath, "-v", "error", "-print_format", "json", "-show_format", "-show_streams", outputPath)
	out, err := cmd.Output()
	if err != nil {
		return renderProbe{}, fmt.Errorf("ffprobe output validation failed: %w", err)
	}
	var raw rawProbe
	if err := json.Unmarshal(out, &raw); err != nil {
		return renderProbe{}, err
	}
	info := renderProbe{}
	info.Format.Duration = parseDuration(raw.Format.Duration)
	for _, stream := range raw.Streams {
		dur := parseDuration(stream.Duration)
		if dur == 0 {
			dur = info.Format.Duration
		}
		switch stream.CodecType {
		case "video":
			if info.VideoDuration == 0 {
				info.VideoDuration = dur
			}
		case "audio":
			if info.AudioDuration == 0 {
				info.AudioDuration = dur
			}
		}
	}
	return info, nil
}

func codecArgs(profile string) []string {
	switch profile {
	case "fast":
		return []string{"-c:v", "libx264", "-preset", "ultrafast", "-crf", "24"}
	case "strong":
		return []string{"-c:v", "libx264", "-preset", "medium", "-crf", "20"}
	default:
		return []string{"-c:v", "libx264", "-preset", "veryfast", "-crf", "22"}
	}
}

func colorFilter(strength string) string {
	switch strength {
	case "hard":
		return "eq=contrast=1.035:saturation=1.035:brightness=0.006"
	case "medium":
		return "eq=contrast=1.020:saturation=1.020:brightness=0.004"
	default:
		return "eq=contrast=1.010:saturation=1.010:brightness=0.002"
	}
}

type rect struct{ x, y, w, h int }

func cropGeometry(width, height int, percent float64) rect {
	ratio := cropRatio(percent)
	keepRatio := clampFloat(1-ratio, 0.01, 1)
	w := evenPositive(int64(math.Round(float64(width) * keepRatio)))
	h := evenPositive(int64(math.Round(float64(height) * keepRatio)))
	w = clamp(w, 2, width)
	h = clamp(h, 2, height)
	return rect{x: (width - w) / 2, y: (height - h) / 2, w: w, h: h}
}

func cropRatio(percent float64) float64 {
	return percent / 100.0
}

func pixelArea(pixel recipe.PixelReplacement, width, height int) rect {
	if pixel.Mode == "smart" && pixel.AreaWidth > 0 && pixel.AreaHeight > 0 {
		w := clamp(pixel.AreaWidth, 1, width)
		h := clamp(pixel.AreaHeight, 1, height)
		return rect{x: clamp(pixel.AreaX, 0, width-w), y: clamp(pixel.AreaY, 0, height-h), w: w, h: h}
	}
	bandW := clamp(int(math.Round(float64(width)*0.08)), 1, width)
	bandH := clamp(int(math.Round(float64(height)*0.08)), 1, height)
	insetX := clamp(int(math.Round(float64(width)*pixel.AreaInsetPercent/100)), 0, width-1)
	insetY := clamp(int(math.Round(float64(height)*pixel.AreaInsetPercent/100)), 0, height-1)
	switch pixel.AreaEdge {
	case "right":
		return rect{x: clamp(width-bandW-insetX, 0, width-bandW), y: 0, w: bandW, h: height}
	case "bottom":
		return rect{x: 0, y: clamp(height-bandH-insetY, 0, height-bandH), w: width, h: bandH}
	case "left":
		return rect{x: insetX, y: 0, w: bandW, h: height}
	default:
		return rect{x: 0, y: insetY, w: width, h: bandH}
	}
}

func speedFactor(basePercent float64) float64 {
	factor := 1 + basePercent/100
	if factor < 0.1 {
		return 0.1
	}
	return factor
}

func parseDuration(value string) float64 {
	var f float64
	_, _ = fmt.Sscanf(value, "%f", &f)
	return f
}

func evenPositive(v int64) int {
	if v < 2 {
		return 2
	}
	i := int(v)
	if i%2 != 0 {
		i--
	}
	return i
}

func executable(configured, fallback string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	path, err := exec.LookPath(fallback)
	if err != nil {
		return "", fmt.Errorf("%s executable not found: %w", fallback, err)
	}
	return path, nil
}

func clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func clampFloat(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
