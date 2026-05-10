package recipe

import (
	"errors"
	"math"
	"math/rand"
	"sort"
	"reflect"
	"videobatch/internal/config"
	"videobatch/internal/ffprobe"
)

type SineParams struct { Amplitude, Frequency, Phase float64 }
type SpeedConfig struct { BasePercent float64; Sine SineParams }
type Event struct { Frame int64; DonorFrame int64 }

type PixelReplacement struct {
	BlurSigma float64
	Percent float64
	Mode string
	AreaInsetPercent float64
	AreaEdge string
	SmartGrid int
	NeighborOffset int
}

type Metadata struct { Mode string }

type Recipe struct {
	InputPath, OutputPath string
	AudioSpeed, VideoSpeed SpeedConfig
	AVSineMode string
	FreezeEvents []Event
	ReplaceEvents []Event
	MinEventDistanceSec float64
	PixelReplacement PixelReplacement
	Metadata Metadata
}

func Generate(cfg config.Config, probe *ffprobe.ProbeData) (*Recipe, error) {
	if probe == nil { return nil, errors.New("recipe is nil") }
	if reflect.DeepEqual(cfg, config.Config{}) { return nil, errors.New("recipe config is nil") }
	rng := rand.New(rand.NewSource(cfg.Seed))
	rec := &Recipe{InputPath: cfg.InputDir, OutputPath: cfg.OutputDir, AVSineMode: cfg.AVSineMode, MinEventDistanceSec: cfg.MinEventDistanceSec, Metadata: Metadata{Mode: cfg.MetadataFullMode}}

	audioSine := randSine(rng, cfg.AudioSine)
	videoSine := randSine(rng, cfg.VideoSine)
	if cfg.AVSineMode == "lock" { videoSine = audioSine }
	rec.AudioSpeed = SpeedConfig{BasePercent: randSignedPercent(rng, cfg.AudioBaseSpeed), Sine: audioSine}
	rec.VideoSpeed = SpeedConfig{BasePercent: randSignedPercent(rng, cfg.VideoBaseSpeed), Sine: videoSine}

	totalFrames := int64(math.Max(1, probe.Duration*probe.Video.Fps))
	minDistFrames := int64(cfg.MinEventDistanceSec * probe.Video.Fps)
	rec.FreezeEvents = buildEvents(rng, totalFrames, cfg.FreezeCount.Min, cfg.FreezeCount.Max, minDistFrames, nil)
	rec.ReplaceEvents = buildReplaceEvents(rng, totalFrames, cfg.ReplaceCount.Min, cfg.ReplaceCount.Max, minDistFrames, rec.FreezeEvents)

	rec.PixelReplacement = PixelReplacement{
		BlurSigma: randPercent(rng, cfg.PixelBlurSigma.Min, cfg.PixelBlurSigma.Max),
		Percent: randPercent(rng, cfg.PixelReplacePercent.Min, cfg.PixelReplacePercent.Max),
		Mode: cfg.PixelReplaceMode,
		AreaInsetPercent: randPercent(rng, cfg.PixelAreaEdgeInset.Min, cfg.PixelAreaEdgeInset.Max),
		AreaEdge: randEdge(rng),
		SmartGrid: cfg.PixelAreaSmartGrid,
		NeighborOffset: randInt(rng, cfg.NeighborOffsetMin, cfg.NeighborOffsetMax),
	}
	return rec, nil
}

func randEdge(rng *rand.Rand) string {
	edges := []string{"top", "right", "bottom", "left"}
	return edges[rng.Intn(len(edges))]
}

func randSignedPercent(rng *rand.Rand, r config.SpeedRange) float64 { p := randPercent(rng, r.MinPercent, r.MaxPercent); if rng.Intn(2)==0 { return -p }; return p }
func randSine(rng *rand.Rand, r config.SineParamsRange) SineParams { return SineParams{Amplitude: randPercent(rng, r.AmplitudeMin, r.AmplitudeMax), Frequency: randPercent(rng, r.FrequencyMin, r.FrequencyMax), Phase: randPercent(rng, r.PhaseMin, r.PhaseMax)} }
func randPercent(rng *rand.Rand, min,max float64) float64 { return min + rng.Float64()*(max-min) }
func randInt(rng *rand.Rand, min,max int) int { if max<=min { return min }; return min+rng.Intn(max-min+1)}

func buildEvents(rng *rand.Rand, totalFrames int64, minCount,maxCount int, minDist int64, blocked []Event) []Event {
	count := minCount
	if maxCount>minCount { count += rng.Intn(maxCount-minCount+1) }
	if count <= 0 { return nil }
	used := map[int64]bool{}
	for _, ev := range blocked { used[ev.Frame] = true }
	out := make([]Event,0,count)
	maxAttempts := int(math.Max(float64(totalFrames*8), 64))
	attempts := 0
	for len(out) < count && attempts < maxAttempts {
		attempts++
		candidate := int64(rng.Int63n(totalFrames))
		ok := true
		for _, ev := range out { if abs64(ev.Frame-candidate) < minDist { ok=false; break } }
		for _, ev := range blocked { if abs64(ev.Frame-candidate) < minDist { ok=false; break } }
		if !ok || used[candidate] { continue }
		used[candidate] = true
		out = append(out, Event{Frame:candidate})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Frame < out[j].Frame })
	return out
}

func buildReplaceEvents(rng *rand.Rand, totalFrames int64, minCount,maxCount int, minDist int64, freezeEvents []Event) []Event {
	replace := buildEvents(rng, totalFrames, minCount, maxCount, minDist, freezeEvents)
	if len(replace) == 0 { return nil }
	usedDonors := map[int64]bool{}
	for i := range replace {
		for {
			donor := int64(rng.Int63n(totalFrames))
			if donor == replace[i].Frame || usedDonors[donor] { continue }
			usedDonors[donor] = true
			replace[i].DonorFrame = donor
			break
		}
	}
	return replace
}
func abs64(v int64) int64 { if v<0 { return -v }; return v }
