package recipe

import (
	"errors"
	"math"
	"math/rand"
	"reflect"
	"videobatch/internal/config"
	"videobatch/internal/ffprobe"
)

type SineParams struct { Amplitude, Frequency, Phase float64 }
type SpeedConfig struct { BasePercent float64; Sine SineParams }
type Event struct { Frame int64; DonorFrame int64 }

type PixelReplacement struct {
	Percent float64
	Mode string
	AreaInsetPercent float64
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
	rec.FreezeEvents = buildEvents(rng, totalFrames, cfg.FreezeCount.Min, cfg.FreezeCount.Max, cfg.MinEventDistanceSec, probe.Video.Fps, false)
	rec.ReplaceEvents = buildEvents(rng, totalFrames, cfg.ReplaceCount.Min, cfg.ReplaceCount.Max, cfg.MinEventDistanceSec, probe.Video.Fps, true)

	rec.PixelReplacement = PixelReplacement{Percent: randPercent(rng, cfg.PixelReplacePercent.Min, cfg.PixelReplacePercent.Max), Mode: cfg.PixelReplaceMode, AreaInsetPercent: randPercent(rng, cfg.PixelAreaEdgeInset.Min, cfg.PixelAreaEdgeInset.Max), SmartGrid: cfg.PixelAreaSmartGrid, NeighborOffset: randInt(rng, cfg.NeighborOffsetMin, cfg.NeighborOffsetMax)}
	return rec, nil
}

func randSignedPercent(rng *rand.Rand, r config.SpeedRange) float64 { p := randPercent(rng, r.MinPercent, r.MaxPercent); if rng.Intn(2)==0 { return -p }; return p }
func randSine(rng *rand.Rand, r config.SineParamsRange) SineParams { return SineParams{Amplitude: randPercent(rng, r.AmplitudeMin, r.AmplitudeMax), Frequency: randPercent(rng, r.FrequencyMin, r.FrequencyMax), Phase: randPercent(rng, r.PhaseMin, r.PhaseMax)} }
func randPercent(rng *rand.Rand, min,max float64) float64 { return min + rng.Float64()*(max-min) }
func randInt(rng *rand.Rand, min,max int) int { if max<=min { return min }; return min+rng.Intn(max-min+1)}

func buildEvents(rng *rand.Rand, totalFrames int64, minCount,maxCount int, minDistanceSec,fps float64, donor bool) []Event {
	count := minCount
	if maxCount>minCount { count += rng.Intn(maxCount-minCount+1) }
	if count <= 0 { return nil }
	minDist := int64(minDistanceSec * fps)
	used := map[int64]bool{}
	out := make([]Event,0,count)
	for len(out) < count {
		candidate := int64(rng.Int63n(totalFrames))
		ok := true
		for _, ev := range out { if abs64(ev.Frame-candidate) < minDist { ok=false; break } }
		if !ok || used[candidate] { continue }
		used[candidate] = true
		ev := Event{Frame:candidate}
		if donor { for { d:=int64(rng.Int63n(totalFrames)); if !used[d] && d!=candidate { used[d]=true; ev.DonorFrame=d; break } } }
		out = append(out, ev)
	}
	return out
}
func abs64(v int64) int64 { if v<0 { return -v }; return v }
