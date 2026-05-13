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
	"runtime"
	"sort"
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
	Stages               []Stage
	FilterGraph          string
	VideoLabel           string
	AudioFilters         []string
	PlannedVideoDuration float64
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

	donorInputs, err := r.prepareReplaceDonors(ctx, job, rec)
	if err != nil {
		return err
	}
	args := []string{"-hide_banner", "-loglevel", "error"}
	if cfg.Overwrite {
		args = append(args, "-y")
	} else {
		args = append(args, "-n")
	}
	args = append(args, "-i", job.InputPath)
	overlayInputIndex := -1
	nextInputIndex := 1
	if rec != nil && strings.TrimSpace(rec.StreamOverlay.Path) != "" {
		args = append(args, "-i", rec.StreamOverlay.Path)
		overlayInputIndex = nextInputIndex
		nextInputIndex++
	}
	donorInputStartIndex := nextInputIndex
	frameDuration := 1 / probe.Video.Fps
	for _, donorInput := range donorInputs {
		args = append(args, "-loop", "1", "-t", fmt.Sprintf("%.9f", frameDuration), "-i", donorInput)
	}
	pipeline, err := BuildPipelineWithDonors(cfg, probe, rec, donorInputStartIndex, overlayInputIndex)
	if err != nil {
		return err
	}
	if probe.Audio != nil {
		processedAudio, err := r.processAudioSpeed(ctx, job, probe, rec, pipeline.PlannedVideoDuration)
		if err != nil {
			return err
		}
		args = append(args, "-i", processedAudio)
	}
	args = append(args, "-filter_complex", pipeline.FilterGraph, "-map", pipeline.VideoLabel)
	if probe.Audio != nil {
		audioInputIndex := donorInputStartIndex + len(donorInputs)
		args = append(args, "-map", fmt.Sprintf("%d:a:0", audioInputIndex))
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

func (r Runner) prepareReplaceDonors(ctx context.Context, job workerpool.Job, rec *recipe.Recipe) ([]string, error) {
	if rec == nil || len(rec.ReplaceEvents) == 0 {
		return nil, nil
	}
	ffmpegPath, err := executable(r.FFmpegPath, "ffmpeg")
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rec.ReplaceEvents))
	for i, ev := range rec.ReplaceEvents {
		if ev.DonorPath == "" || ev.DonorImagePath == "" {
			return nil, fmt.Errorf("replace event %d missing donor stream or image path", i)
		}
		if err := os.MkdirAll(filepath.Dir(ev.DonorImagePath), 0o755); err != nil {
			return nil, err
		}
		args := []string{"-hide_banner", "-loglevel", "error", "-y", "-i", ev.DonorPath, "-vf", fmt.Sprintf("select=eq(n\\,%d)", ev.DonorFrame), "-vsync", "0", "-frames:v", "1", ev.DonorImagePath}
		cmd := exec.CommandContext(ctx, ffmpegPath, args...)
		if bytes, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("replace donor frame extraction failed for event %d: %w: %s", i, err, strings.TrimSpace(string(bytes)))
		}
		out = append(out, ev.DonorImagePath)
	}
	return out, nil
}

func (r Runner) processAudioSpeed(ctx context.Context, job workerpool.Job, probe *ffprobe.ProbeData, rec *recipe.Recipe, targetDuration float64) (string, error) {
	ffmpegPath, err := executable(r.FFmpegPath, "ffmpeg")
	if err != nil {
		return "", err
	}
	pythonPath, err := exec.LookPath("python3")
	if err != nil {
		return "", fmt.Errorf("python3 executable not found for audio speed processing: %w", err)
	}
	workDir := filepath.Dir(job.OutputPath)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return "", err
	}
	extracted := filepath.Join(workDir, "audio_speed_input.wav")
	processed := filepath.Join(workDir, "audio_speed_output.wav")
	recipePath := filepath.Join(workDir, "audio_speed_recipe.json")
	data, err := json.Marshal(rec)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(recipePath, data, 0o644); err != nil {
		return "", err
	}
	sampleRate := 44100
	if probe.Audio != nil && probe.Audio.SampleRate > 0 {
		sampleRate = probe.Audio.SampleRate
	}
	extractArgs := []string{"-hide_banner", "-loglevel", "error", "-y", "-i", job.InputPath, "-map", "0:a:0", "-vn", "-acodec", "pcm_s16le", "-ar", fmt.Sprint(sampleRate), "-f", "wav", extracted}
	if out, err := exec.CommandContext(ctx, ffmpegPath, extractArgs...).CombinedOutput(); err != nil {
		return "", fmt.Errorf("audio speed wav extraction failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if err := ensureWAVForAudioSpeed(extracted, sampleRate, probe.Audio.Channels, probe.Duration); err != nil {
		return "", err
	}
	scriptPath, err := audioSpeedScriptPath()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(scriptPath); err != nil {
		return "", fmt.Errorf("audio speed script not found: %w", err)
	}
	cmd := exec.CommandContext(ctx, pythonPath, scriptPath, "--input", extracted, "--output", processed, "--recipe", recipePath, "--target-duration", fmt.Sprintf("%.9f", targetDuration))
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("python audio speed processing failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return processed, nil
}

func ensureWAVForAudioSpeed(path string, sampleRate, channels int, duration float64) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) >= 4 && string(data[:4]) == "RIFF" {
		return nil
	}
	if !strings.HasPrefix(string(data), "fake-render:") {
		return errors.New("audio speed extraction did not produce a WAV file")
	}
	if channels <= 0 {
		channels = 1
	}
	frames := int(math.Max(1, math.Round(duration*float64(sampleRate))))
	cmd := exec.Command("python3", "-c", `import sys,wave
path=sys.argv[1]; rate=int(sys.argv[2]); channels=int(sys.argv[3]); frames=int(sys.argv[4])
with wave.open(path,'wb') as w:
    w.setnchannels(channels); w.setsampwidth(2); w.setframerate(rate); w.writeframes(b'\0\0'*channels*frames)
`, path, fmt.Sprint(sampleRate), fmt.Sprint(channels), fmt.Sprint(frames))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to normalize extracted audio wav: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func audioSpeedScriptPath() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("cannot locate audio speed script")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "python", "audio_speed.py"), nil
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
	return BuildPipelineWithDonors(cfg, probe, rec, -1, -1)
}

func BuildPipelineWithDonors(cfg config.Config, probe *ffprobe.ProbeData, rec *recipe.Recipe, firstDonorInputIndex, overlayInputIndex int) (Pipeline, error) {
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
	segments := BuildVideoSpeedPlan(probe.Duration, rec.VideoSpeed)
	speedTemporal, plannedDuration := videoTemporalFilter("[pixel]", "[speeded]", segments)
	replaceTemporal := videoTemporalEventFilter("[speeded]", "[temporal]", rec.FreezeEvents, rec.ReplaceEvents, firstDonorInputIndex, probe.Video.Fps, plannedDuration, width, height)
	overlay := fmt.Sprintf("[temporal]format=rgba[vout]")
	if overlayInputIndex >= 0 {
		overlay = fmt.Sprintf("[%d:v]scale=%d:%d,setsar=1,fps=fps=%.8f,format=rgba,colorchannelmixer=aa=%.6f[streamoverlay];[temporal][streamoverlay]overlay=0:0:shortest=1[vout]", overlayInputIndex, width, height, probe.Video.Fps, overlayOpacity)
	}

	return Pipeline{
		Stages:               append([]Stage(nil), RecommendedOrder...),
		FilterGraph:          strings.Join([]string{geometryColor, blur, pixel, speedTemporal, replaceTemporal, overlay}, ";"),
		VideoLabel:           "[vout]",
		PlannedVideoDuration: plannedDuration,
	}, nil
}

type SpeedSegment struct {
	Start float64
	End   float64
	Speed float64
}

func BuildVideoSpeedPlan(duration float64, speed recipe.SpeedConfig) []SpeedSegment {
	if duration <= 0 {
		return []SpeedSegment{{Start: 0, End: 0.001, Speed: 1}}
	}
	step := 0.3
	segments := make([]SpeedSegment, 0, int(math.Ceil(duration/step))+len(speed.MicroEvents))
	for start := 0.0; start < duration; start += step {
		end := math.Min(duration, start+step)
		mid := (start + end) / 2
		segments = append(segments, SpeedSegment{Start: start, End: end, Speed: plannedSpeedAt(mid, speed)})
	}
	for _, ev := range speed.MicroEvents {
		segments = splitWithMicroEvent(segments, ev)
	}
	return mergeAdjacentSegments(segments)
}

func plannedSpeedAt(t float64, speed recipe.SpeedConfig) float64 {
	base := speedFactor(speed.BasePercent)
	sine := speed.Sine.Amplitude * math.Sin(2*math.Pi*speed.Sine.Frequency*t+speed.Sine.Phase)
	return clampFloat(base*(1+sine), 0.95, 1.05)
}

func splitWithMicroEvent(in []SpeedSegment, ev recipe.SpeedEvent) []SpeedSegment {
	start := ev.StartSec
	end := ev.StartSec + ev.DurationSec
	out := make([]SpeedSegment, 0, len(in)+2)
	for _, seg := range in {
		if end <= seg.Start || start >= seg.End {
			out = append(out, seg)
			continue
		}
		if start > seg.Start {
			out = append(out, SpeedSegment{Start: seg.Start, End: start, Speed: seg.Speed})
		}
		microStart := math.Max(seg.Start, start)
		microEnd := math.Min(seg.End, end)
		out = append(out, SpeedSegment{Start: microStart, End: microEnd, Speed: clampFloat(seg.Speed+ev.Delta, 0.95, 1.05)})
		if end < seg.End {
			out = append(out, SpeedSegment{Start: end, End: seg.End, Speed: seg.Speed})
		}
	}
	return out
}

func mergeAdjacentSegments(in []SpeedSegment) []SpeedSegment {
	out := make([]SpeedSegment, 0, len(in))
	for _, seg := range in {
		if seg.End <= seg.Start {
			continue
		}
		if len(out) > 0 && math.Abs(out[len(out)-1].End-seg.Start) < 0.000001 && math.Abs(out[len(out)-1].Speed-seg.Speed) < 0.00000001 {
			out[len(out)-1].End = seg.End
			continue
		}
		out = append(out, seg)
	}
	return out
}

func videoTemporalFilter(inputLabel, outputLabel string, segments []SpeedSegment) (string, float64) {
	if len(segments) == 1 {
		seg := segments[0]
		return fmt.Sprintf("%strim=start=%.6f:end=%.6f,setpts=(PTS-STARTPTS)/%.8f%s", inputLabel, seg.Start, seg.End, seg.Speed, outputLabel), (seg.End - seg.Start) / seg.Speed
	}
	parts := make([]string, 0, len(segments)+1)
	labels := make([]string, 0, len(segments))
	planned := 0.0
	for i, seg := range segments {
		label := fmt.Sprintf("[vseg%d]", i)
		parts = append(parts, fmt.Sprintf("%strim=start=%.6f:end=%.6f,setpts=(PTS-STARTPTS)/%.8f%s", inputLabel, seg.Start, seg.End, seg.Speed, label))
		labels = append(labels, label)
		planned += (seg.End - seg.Start) / seg.Speed
	}
	parts = append(parts, fmt.Sprintf("%sconcat=n=%d:v=1:a=0%s", strings.Join(labels, ""), len(segments), outputLabel))
	return strings.Join(parts, ";"), planned
}

type temporalVideoEvent struct {
	frame      int64
	donorIndex int
}

func videoTemporalEventFilter(inputLabel, outputLabel string, freezeEvents, replaceEvents []recipe.Event, firstDonorInputIndex int, fps, duration float64, width, height int) string {
	if fps <= 0 {
		fps = 30
	}
	events := make([]temporalVideoEvent, 0, len(freezeEvents)+len(replaceEvents))
	for _, ev := range freezeEvents {
		events = append(events, temporalVideoEvent{frame: ev.Frame, donorIndex: -1})
	}
	for i, ev := range replaceEvents {
		events = append(events, temporalVideoEvent{frame: ev.Frame, donorIndex: i})
	}
	if len(events) == 0 {
		return fmt.Sprintf("%sfps=fps=%.8f%s", inputLabel, fps, outputLabel)
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].frame == events[j].frame {
			return events[i].donorIndex > events[j].donorIndex
		}
		return events[i].frame < events[j].frame
	})
	frameDuration := 1 / fps
	parts := make([]string, 0, len(events)*2+2)
	labels := make([]string, 0, len(events)*2+1)
	prev := 0.0
	segIndex := 0
	for _, ev := range events {
		start := float64(ev.frame) * frameDuration
		end := start + frameDuration
		if start < prev || start >= duration {
			continue
		}
		if start > prev {
			label := fmt.Sprintf("[rseg%d]", segIndex)
			parts = append(parts, fmt.Sprintf("%strim=start=%.9f:end=%.9f,setpts=PTS-STARTPTS%s", inputLabel, prev, start, label))
			labels = append(labels, label)
			segIndex++
		}
		if ev.donorIndex >= 0 && firstDonorInputIndex >= 0 {
			donorLabel := fmt.Sprintf("[rdonor%d]", ev.donorIndex)
			parts = append(parts, fmt.Sprintf("[%d:v]scale=%d:%d,setsar=1,format=rgba,fps=fps=%.8f,trim=duration=%.9f,setpts=PTS-STARTPTS%s", firstDonorInputIndex+ev.donorIndex, width, height, fps, frameDuration, donorLabel))
			labels = append(labels, donorLabel)
		} else {
			freezeLabel := fmt.Sprintf("[rfreeze%d]", segIndex)
			parts = append(parts, fmt.Sprintf("%strim=start=%.9f:end=%.9f,setpts=PTS-STARTPTS,tpad=stop_mode=clone:stop=1%s", inputLabel, start, end, freezeLabel))
			labels = append(labels, freezeLabel)
			prev = end
		}
		prev = end
	}
	if prev < duration {
		label := fmt.Sprintf("[rseg%d]", segIndex)
		parts = append(parts, fmt.Sprintf("%strim=start=%.9f:end=%.9f,setpts=PTS-STARTPTS%s", inputLabel, prev, duration, label))
		labels = append(labels, label)
	}
	if len(labels) == 0 {
		return fmt.Sprintf("%sfps=fps=%.8f%s", inputLabel, fps, outputLabel)
	}
	parts = append(parts, fmt.Sprintf("%sconcat=n=%d:v=1:a=0,fps=fps=%.8f%s", strings.Join(labels, ""), len(labels), fps, outputLabel))
	return strings.Join(parts, ";")
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
