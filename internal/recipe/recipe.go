package recipe

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"sort"
	"videobatch/internal/config"
	"videobatch/internal/ffprobe"
	"videobatch/internal/pixel"
)

type SineParams struct{ Amplitude, Frequency, Phase float64 }
type SpeedEvent struct {
	StartSec    float64
	DurationSec float64
	Delta       float64
}
type AudioFreezeEvent struct {
	StartSec    float64
	DurationSec float64
	Repeats     int
}
type SpeedConfig struct {
	BasePercent  float64
	Sine         SineParams
	MicroEvents  []SpeedEvent
	FreezeEvents []AudioFreezeEvent
}
type Event struct {
	Frame      int64
	DonorFrame int64
}

type PixelReplacement struct {
	BlurSigma           float64
	Percent             float64
	RequestedMode       string
	Mode                string
	AreaInsetPercent    float64
	AreaEdge            string
	AreaX               int
	AreaY               int
	AreaWidth           int
	AreaHeight          int
	SmartGrid           int
	SmartConfidence     float64
	SmartFallbackReason string
	NeighborOffset      int
}

type MetadataTag struct {
	Name  string
	Value string
}

type Metadata struct {
	Mode      string
	Clean     bool
	Diversify []MetadataTag
}

func (m Metadata) DiversifyTags() []MetadataTag {
	return append([]MetadataTag(nil), m.Diversify...)
}

type Recipe struct {
	InputPath, OutputPath  string
	AudioSpeed, VideoSpeed SpeedConfig
	AVSineMode             string
	FreezeEvents           []Event
	ReplaceEvents          []Event
	MinEventDistanceSec    float64
	PixelReplacement       PixelReplacement
	Metadata               Metadata
}

func Generate(cfg config.Config, probe *ffprobe.ProbeData) (*Recipe, error) {
	return GenerateWithSmartAnalyzer(context.Background(), cfg, probe, nil)
}

func GenerateWithSmartAnalyzer(ctx context.Context, cfg config.Config, probe *ffprobe.ProbeData, analyzer pixel.Analyzer) (*Recipe, error) {
	if probe == nil {
		return nil, errors.New("recipe is nil")
	}
	if reflect.DeepEqual(cfg, config.Config{}) {
		return nil, errors.New("recipe config is nil")
	}
	rng := rand.New(rand.NewSource(cfg.Seed))
	rec := &Recipe{InputPath: cfg.InputDir, OutputPath: cfg.OutputDir, AVSineMode: cfg.AVSineMode, MinEventDistanceSec: cfg.MinEventDistanceSec, Metadata: metadataFull(rng, cfg.MetadataFullMode)}

	audioSine := randSine(rng, cfg.AudioSine)
	videoSine := randSine(rng, cfg.VideoSine)
	if cfg.AVSineMode == "lock" {
		videoSine = audioSine
	}
	rec.AudioSpeed = SpeedConfig{BasePercent: randSignedPercent(rng, cfg.AudioBaseSpeed), Sine: audioSine}
	rec.VideoSpeed = SpeedConfig{BasePercent: randSignedPercent(rng, cfg.VideoBaseSpeed), Sine: videoSine}
	rec.AudioSpeed.MicroEvents = buildSpeedEvents(rng, probe.Duration, speedMicroCount(probe.Duration), 0.04, 0.14, 0.0008, 0.0030, 0.35)
	rec.VideoSpeed.MicroEvents = buildSpeedEvents(rng, probe.Duration, speedMicroCount(probe.Duration), 0.05, 0.20, 0.0010, 0.0040, 0.35)
	rec.AudioSpeed.FreezeEvents = buildAudioFreezeEvents(rng, probe.Duration, audioFreezeCount(probe.Duration), 0.010, 0.040, 0.60)

	totalFrames := int64(math.Max(1, probe.Duration*probe.Video.Fps))
	minDistFrames := int64(cfg.MinEventDistanceSec * probe.Video.Fps)
	rec.FreezeEvents = buildEvents(rng, totalFrames, cfg.FreezeCount.Min, cfg.FreezeCount.Max, minDistFrames, nil)
	rec.ReplaceEvents = buildReplaceEvents(rng, totalFrames, cfg.ReplaceCount.Min, cfg.ReplaceCount.Max, minDistFrames, rec.FreezeEvents)

	rec.PixelReplacement = PixelReplacement{
		BlurSigma:        randPercent(rng, cfg.PixelBlurSigma.Min, cfg.PixelBlurSigma.Max),
		Percent:          randPercent(rng, cfg.PixelReplacePercent.Min, cfg.PixelReplacePercent.Max),
		RequestedMode:    cfg.PixelReplaceMode,
		Mode:             cfg.PixelReplaceMode,
		AreaInsetPercent: randPercent(rng, cfg.PixelAreaEdgeInset.Min, cfg.PixelAreaEdgeInset.Max),
		AreaEdge:         randEdge(rng),
		SmartGrid:        cfg.PixelAreaSmartGrid,
		NeighborOffset:   randInt(rng, cfg.NeighborOffsetMin, cfg.NeighborOffsetMax),
	}
	if cfg.PixelReplaceMode == "smart" {
		applySmartArea(ctx, rec, cfg, probe, analyzer)
	}
	return rec, nil
}

func metadataFull(rng *rand.Rand, mode string) Metadata {
	baseDay := 1 + rng.Intn(28)
	hour := rng.Intn(24)
	minute := rng.Intn(60)
	second := rng.Intn(60)
	date := fmt.Sprintf("2024:01:%02d %02d:%02d:%02d", baseDay, hour, minute, second)
	return Metadata{
		Mode:  mode,
		Clean: true,
		Diversify: []MetadataTag{
			{Name: "Software", Value: fmt.Sprintf("videobatch-%06d", rng.Intn(1000000))},
			{Name: "Comment", Value: fmt.Sprintf("recipe-%08x", rng.Uint32())},
			{Name: "CreateDate", Value: date},
			{Name: "ModifyDate", Value: date},
		},
	}
}

func applySmartArea(ctx context.Context, rec *Recipe, cfg config.Config, probe *ffprobe.ProbeData, analyzer pixel.Analyzer) {
	if analyzer == nil {
		analyzer = pixel.FFmpegAnalyzer{}
	}
	area, err := analyzer.Analyze(ctx, rec.InputPath, probe, cfg.PixelAreaSmartGrid)
	if err != nil {
		fallbackToEdge(&rec.PixelReplacement, fmt.Sprintf("smart analysis failed: %v", err))
		return
	}
	if area.Confidence < pixel.DefaultSmartConfidenceThreshold {
		fallbackToEdge(&rec.PixelReplacement, fmt.Sprintf("smart analysis low confidence %.3f", area.Confidence))
		return
	}
	rec.PixelReplacement.AreaX = area.X
	rec.PixelReplacement.AreaY = area.Y
	rec.PixelReplacement.AreaWidth = area.Width
	rec.PixelReplacement.AreaHeight = area.Height
	rec.PixelReplacement.SmartConfidence = area.Confidence
}

func fallbackToEdge(pixelReplacement *PixelReplacement, reason string) {
	pixelReplacement.Mode = "edge"
	pixelReplacement.SmartFallbackReason = reason
	pixelReplacement.SmartConfidence = 0
	pixelReplacement.AreaX = 0
	pixelReplacement.AreaY = 0
	pixelReplacement.AreaWidth = 0
	pixelReplacement.AreaHeight = 0
}

func randEdge(rng *rand.Rand) string {
	edges := []string{"top", "right", "bottom", "left"}
	return edges[rng.Intn(len(edges))]
}

func randSignedPercent(rng *rand.Rand, r config.SpeedRange) float64 {
	p := randPercent(rng, r.MinPercent, r.MaxPercent)
	if rng.Intn(2) == 0 {
		return -p
	}
	return p
}
func randSine(rng *rand.Rand, r config.SineParamsRange) SineParams {
	return SineParams{Amplitude: randPercent(rng, r.AmplitudeMin, r.AmplitudeMax), Frequency: randPercent(rng, r.FrequencyMin, r.FrequencyMax), Phase: randPercent(rng, r.PhaseMin, r.PhaseMax)}
}
func randPercent(rng *rand.Rand, min, max float64) float64 { return min + rng.Float64()*(max-min) }
func randInt(rng *rand.Rand, min, max int) int {
	if max <= min {
		return min
	}
	return min + rng.Intn(max-min+1)
}

func buildEvents(rng *rand.Rand, totalFrames int64, minCount, maxCount int, minDist int64, blocked []Event) []Event {
	count := minCount
	if maxCount > minCount {
		count += rng.Intn(maxCount - minCount + 1)
	}
	if count <= 0 {
		return nil
	}
	used := map[int64]bool{}
	for _, ev := range blocked {
		used[ev.Frame] = true
	}
	out := make([]Event, 0, count)
	maxAttempts := int(math.Max(float64(totalFrames*8), 64))
	attempts := 0
	for len(out) < count && attempts < maxAttempts {
		attempts++
		candidate := int64(rng.Int63n(totalFrames))
		ok := true
		for _, ev := range out {
			if abs64(ev.Frame-candidate) < minDist {
				ok = false
				break
			}
		}
		for _, ev := range blocked {
			if abs64(ev.Frame-candidate) < minDist {
				ok = false
				break
			}
		}
		if !ok || used[candidate] {
			continue
		}
		used[candidate] = true
		out = append(out, Event{Frame: candidate})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Frame < out[j].Frame })
	return out
}

func buildReplaceEvents(rng *rand.Rand, totalFrames int64, minCount, maxCount int, minDist int64, freezeEvents []Event) []Event {
	replace := buildEvents(rng, totalFrames, minCount, maxCount, minDist, freezeEvents)
	if len(replace) == 0 {
		return nil
	}
	usedDonors := map[int64]bool{}
	for i := range replace {
		for {
			donor := int64(rng.Int63n(totalFrames))
			if donor == replace[i].Frame || usedDonors[donor] {
				continue
			}
			usedDonors[donor] = true
			replace[i].DonorFrame = donor
			break
		}
	}
	return replace
}

func speedMicroCount(durationSec float64) int {
	if durationSec <= 0 {
		return 0
	}
	count := int(math.Ceil(durationSec / 12.0))
	if count < 1 {
		return 1
	}
	if count > 10 {
		return 10
	}
	return count
}

func audioFreezeCount(durationSec float64) int {
	if durationSec < 2 {
		return 0
	}
	count := int(math.Ceil(durationSec / 18.0))
	if count < 1 {
		return 1
	}
	if count > 6 {
		return 6
	}
	return count
}

func buildSpeedEvents(rng *rand.Rand, durationSec float64, count int, minDuration, maxDuration, minDelta, maxDelta, minSpacing float64) []SpeedEvent {
	if count <= 0 || durationSec <= minDuration {
		return nil
	}
	out := make([]SpeedEvent, 0, count)
	maxAttempts := int(math.Max(float64(count*64), 64))
	for attempts := 0; len(out) < count && attempts < maxAttempts; attempts++ {
		eventDuration := randPercent(rng, minDuration, maxDuration)
		latestStart := math.Max(0, durationSec-eventDuration)
		candidate := rng.Float64() * latestStart
		if !timeWindowAvailable(candidate, candidate+eventDuration, minSpacing, out, nil) {
			continue
		}
		delta := randPercent(rng, minDelta, maxDelta)
		if rng.Intn(2) == 0 {
			delta = -delta
		}
		out = append(out, SpeedEvent{StartSec: round6(candidate), DurationSec: round6(eventDuration), Delta: round8(delta)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartSec < out[j].StartSec })
	return out
}

func buildAudioFreezeEvents(rng *rand.Rand, durationSec float64, count int, minDuration, maxDuration, minSpacing float64) []AudioFreezeEvent {
	if count <= 0 || durationSec <= minDuration {
		return nil
	}
	out := make([]AudioFreezeEvent, 0, count)
	maxAttempts := int(math.Max(float64(count*64), 64))
	for attempts := 0; len(out) < count && attempts < maxAttempts; attempts++ {
		eventDuration := randPercent(rng, minDuration, maxDuration)
		latestStart := math.Max(0, durationSec-eventDuration)
		candidate := rng.Float64() * latestStart
		if !freezeWindowAvailable(candidate, candidate+eventDuration, minSpacing, out) {
			continue
		}
		out = append(out, AudioFreezeEvent{StartSec: round6(candidate), DurationSec: round6(eventDuration), Repeats: 2 + rng.Intn(3)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartSec < out[j].StartSec })
	return out
}

func timeWindowAvailable(start, end, spacing float64, events []SpeedEvent, blocked []SpeedEvent) bool {
	for _, ev := range events {
		if start < ev.StartSec+ev.DurationSec+spacing && end+spacing > ev.StartSec {
			return false
		}
	}
	for _, ev := range blocked {
		if start < ev.StartSec+ev.DurationSec+spacing && end+spacing > ev.StartSec {
			return false
		}
	}
	return true
}

func freezeWindowAvailable(start, end, spacing float64, events []AudioFreezeEvent) bool {
	for _, ev := range events {
		if start < ev.StartSec+ev.DurationSec+spacing && end+spacing > ev.StartSec {
			return false
		}
	}
	return true
}

func round6(v float64) float64 { return math.Round(v*1_000_000) / 1_000_000 }
func round8(v float64) float64 { return math.Round(v*100_000_000) / 100_000_000 }
func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
