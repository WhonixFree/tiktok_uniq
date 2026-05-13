package recipe

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"path/filepath"
	"reflect"
	"sort"

	"videobatch/internal/config"
	"videobatch/internal/ffprobe"
	"videobatch/internal/overlay"
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
	ID                string
	Kind              string
	Frame             int64
	DonorFrame        int64
	DonorPath         string
	DonorImagePath    string
	Applied           bool
	DegradationReason string
	EffectiveFrames   int
}

type StreamOverlay struct {
	Path string
}

type PixelReplacement struct {
	BlurSigma           float64
	Percent             float64
	PercentUnits        string
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
	ReplaceRequested       int
	ReplaceEffective       int
	ReplaceDisabledReason  string
	StreamOverlay          StreamOverlay
	MinEventDistanceSec    float64
	TemporalEvents         []TemporalEvent
	TemporalDroppedEvents  []TemporalDrop
	TemporalStats          map[TemporalEffectType]TemporalEffectStats
	RuntimeTemporalLog     map[string]any
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
	rec := &Recipe{InputPath: cfg.InputDir, OutputPath: cfg.OutputDir, AVSineMode: cfg.AVSineMode, MinEventDistanceSec: cfg.MinEventDistanceSec, Metadata: metadataFull(rng, cfg.MetadataFullMode), RuntimeTemporalLog: map[string]any{}}

	audioSine := randSine(rng, cfg.AudioSine)
	videoSine := randSine(rng, cfg.VideoSine)
	if cfg.AVSineMode == "lock" {
		videoSine = audioSine
	}
	rec.AudioSpeed = SpeedConfig{BasePercent: randSignedPercent(rng, cfg.AudioBaseSpeed), Sine: audioSine}
	rec.VideoSpeed = SpeedConfig{BasePercent: randSignedPercent(rng, cfg.VideoBaseSpeed), Sine: videoSine}

	audioMicroCandidates := buildSpeedEvents(rng, probe.Duration, speedMicroCount(probe.Duration), 0.04, 0.14, 0.0008, 0.0030, 0.35)
	videoMicroCandidates := buildSpeedEvents(rng, probe.Duration, speedMicroCount(probe.Duration), 0.05, 0.20, 0.0010, 0.0040, 0.35)
	audioFreezeCandidates := buildAudioFreezeEvents(rng, probe.Duration, audioFreezeCount(probe.Duration), 0.010, 0.040, 0.60)

	totalFrames := int64(math.Max(1, probe.Duration*probe.Video.Fps))
	minDistFrames := int64(cfg.MinEventDistanceSec * probe.Video.Fps)
	freezeMin, freezeMax := cfg.FreezeCount.Min, cfg.FreezeCount.Max
	if probe.Duration < 1.0 {
		freezeMin, freezeMax = 0, 0
	}
	freezeCandidates := buildEvents(rng, totalFrames, freezeMin, freezeMax, minDistFrames, nil)
	for i := range freezeCandidates {
		freezeCandidates[i].Kind = "freeze_1f"
		freezeCandidates[i].ID = fmt.Sprintf("freeze_%03d", i)
		freezeCandidates[i].EffectiveFrames = 1
	}
	replaceCandidates := planReplaceCandidateEvents(ctx, rng, cfg, rec, totalFrames, minDistFrames)

	coordination := coordinateTemporalEvents(buildTemporalCandidates(cfg.Seed, probe.Video.Fps, cfg.MinEventDistanceSec, audioMicroCandidates, audioFreezeCandidates, videoMicroCandidates, freezeCandidates, replaceCandidates))
	rec.AudioSpeed.MicroEvents = coordination.AudioMicro
	rec.AudioSpeed.FreezeEvents = coordination.AudioFreeze
	rec.VideoSpeed.MicroEvents = coordination.VideoMicro
	rec.FreezeEvents = coordination.VideoFreeze
	rec.ReplaceEvents = coordination.VideoReplace
	rec.ReplaceEffective = len(rec.ReplaceEvents)
	rec.TemporalEvents = coordination.Accepted
	rec.TemporalDroppedEvents = coordination.Dropped
	rec.TemporalStats = coordination.Stats
	applyReplaceCoordinationStatus(rec)
	finalizeTemporalStats(rec)

	rec.PixelReplacement = PixelReplacement{
		BlurSigma:        randPercent(rng, cfg.PixelBlurSigma.Min, cfg.PixelBlurSigma.Max),
		Percent:          randPercent(rng, cfg.PixelReplacePercent.Min, cfg.PixelReplacePercent.Max),
		PercentUnits:     "percent_0_100",
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

var discoverOverlayFiles = overlay.Discover
var probeMedia = ffprobe.Probe

func planReplaceCandidateEvents(ctx context.Context, rng *rand.Rand, cfg config.Config, rec *Recipe, totalFrames, minDistFrames int64) []Event {
	requested := cfg.ReplaceCount.Min
	if cfg.ReplaceCount.Max > cfg.ReplaceCount.Min {
		requested += rng.Intn(cfg.ReplaceCount.Max - cfg.ReplaceCount.Min + 1)
	}
	rec.ReplaceRequested = requested
	if requested <= 0 {
		rec.ReplaceEffective = 0
		return nil
	}

	overlayPath, donorPath, donorFrames, reason := selectOverlayAndDonor(ctx, rng, cfg)
	if reason != "" {
		rec.ReplaceDisabledReason = reason
		rec.ReplaceEffective = 0
		return nil
	}
	rec.StreamOverlay = StreamOverlay{Path: overlayPath}
	if donorFrames < int64(requested) {
		rec.ReplaceDisabledReason = fmt.Sprintf("replace events reduced: donor frame count %d is below requested %d", donorFrames, requested)
		requested = int(donorFrames)
	}
	if requested <= 0 {
		rec.ReplaceDisabledReason = "donor stream has no available frames"
		rec.ReplaceEffective = 0
		return nil
	}

	replace := buildEvents(rng, totalFrames, requested, requested, minDistFrames, nil)
	if len(replace) == 0 {
		rec.ReplaceDisabledReason = "no valid replace target frames after spacing constraints"
		rec.ReplaceEffective = 0
		return nil
	}
	for i := range replace {
		replace[i].Kind = "replace_1f"
		replace[i].ID = fmt.Sprintf("replace_%03d", i)
		replace[i].EffectiveFrames = 1
	}
	assignDonorFrames(rng, replace, donorPath, donorFrames, cfg.TmpDir)
	return replace
}

func buildTemporalCandidates(seed int64, fps, minDistanceSec float64, audioMicro []SpeedEvent, audioFreeze []AudioFreezeEvent, videoMicro []SpeedEvent, videoFreeze []Event, videoReplace []Event) []temporalCandidate {
	frameDuration := 1.0 / fps
	if fps <= 0 {
		frameDuration = 1.0 / 30.0
	}
	minMicro := temporalMinDistance(minDistanceSec, 0.35)
	minAudioFreeze := temporalMinDistance(minDistanceSec, 0.60)
	out := make([]temporalCandidate, 0, len(videoFreeze)+len(videoReplace)+len(audioMicro)+len(audioFreeze)+len(videoMicro))

	// Register hard frame-exact events first, then soft events. This preserves the
	// one-frame visual guarantees while still applying global spacing to all later
	// accepted temporal events deterministically.
	for i := range videoFreeze {
		ev := videoFreeze[i]
		start := float64(ev.Frame) * frameDuration
		out = append(out, temporalCandidate{event: TemporalEvent{ID: temporalID(EffectVideoFreeze, i), EffectType: EffectVideoFreeze, StartSec: round6(start), EndSec: round6(start + frameDuration), Hardness: HardnessHard, MinDistanceSec: minDistanceSec, SeedLineage: temporalSeedLineage(seed, EffectVideoFreeze, i), Frame: ev.Frame}, videoEvent: &videoFreeze[i]})
	}
	for i := range videoReplace {
		ev := videoReplace[i]
		start := float64(ev.Frame) * frameDuration
		out = append(out, temporalCandidate{event: TemporalEvent{ID: temporalID(EffectVideoReplace, i), EffectType: EffectVideoReplace, StartSec: round6(start), EndSec: round6(start + frameDuration), Hardness: HardnessHard, MinDistanceSec: minDistanceSec, SeedLineage: temporalSeedLineage(seed, EffectVideoReplace, i), Frame: ev.Frame}, videoEvent: &videoReplace[i]})
	}
	for i := range audioMicro {
		ev := audioMicro[i]
		out = append(out, temporalCandidate{event: TemporalEvent{ID: temporalID(EffectAudioMicro, i), EffectType: EffectAudioMicro, StartSec: ev.StartSec, EndSec: round6(ev.StartSec + ev.DurationSec), Hardness: HardnessSoft, MinDistanceSec: minMicro, SeedLineage: temporalSeedLineage(seed, EffectAudioMicro, i)}, audioMicro: &audioMicro[i]})
	}
	for i := range audioFreeze {
		ev := audioFreeze[i]
		out = append(out, temporalCandidate{event: TemporalEvent{ID: temporalID(EffectAudioFreeze, i), EffectType: EffectAudioFreeze, StartSec: ev.StartSec, EndSec: round6(ev.StartSec + ev.DurationSec), Hardness: HardnessSoft, MinDistanceSec: minAudioFreeze, SeedLineage: temporalSeedLineage(seed, EffectAudioFreeze, i)}, audioFreeze: &audioFreeze[i]})
	}
	for i := range videoMicro {
		ev := videoMicro[i]
		out = append(out, temporalCandidate{event: TemporalEvent{ID: temporalID(EffectVideoMicro, i), EffectType: EffectVideoMicro, StartSec: ev.StartSec, EndSec: round6(ev.StartSec + ev.DurationSec), Hardness: HardnessSoft, MinDistanceSec: minMicro, SeedLineage: temporalSeedLineage(seed, EffectVideoMicro, i)}, videoMicro: &videoMicro[i]})
	}
	return out
}

func temporalID(effect TemporalEffectType, index int) string {
	return fmt.Sprintf("%s_%03d", effect, index)
}

func temporalSeedLineage(seed int64, effect TemporalEffectType, index int) string {
	return fmt.Sprintf("seed:%d:%s:%03d", seed, effect, index)
}

func applyReplaceCoordinationStatus(rec *Recipe) {
	if rec.ReplaceRequested == 0 || rec.ReplaceDisabledReason != "" {
		return
	}
	if rec.ReplaceEffective < rec.ReplaceRequested {
		rec.ReplaceDisabledReason = fmt.Sprintf("replace events reduced: temporal coordination allowed %d of %d requested", rec.ReplaceEffective, rec.ReplaceRequested)
	}
}

func finalizeTemporalStats(rec *Recipe) {
	if rec.TemporalStats == nil {
		rec.TemporalStats = map[TemporalEffectType]TemporalEffectStats{}
	}
	for _, effect := range []TemporalEffectType{EffectAudioMicro, EffectAudioFreeze, EffectVideoMicro, EffectVideoFreeze, EffectVideoReplace} {
		if _, ok := rec.TemporalStats[effect]; !ok {
			rec.TemporalStats[effect] = TemporalEffectStats{}
		}
	}
	replaceStats := rec.TemporalStats[EffectVideoReplace]
	if rec.ReplaceRequested > replaceStats.Requested {
		replaceStats.Requested = rec.ReplaceRequested
	}
	replaceStats.Effective = rec.ReplaceEffective
	if replaceStats.Requested > replaceStats.Effective {
		replaceStats.Dropped = replaceStats.Requested - replaceStats.Effective
	}
	if rec.ReplaceDisabledReason != "" && replaceStats.Dropped > 0 {
		replaceStats.DropReasons = append(replaceStats.DropReasons, rec.ReplaceDisabledReason)
	}
	rec.TemporalStats[EffectVideoReplace] = replaceStats
}

func selectOverlayAndDonor(ctx context.Context, rng *rand.Rand, cfg config.Config) (string, string, int64, string) {
	files, err := discoverOverlayFiles(cfg.StreamOverlayDir)
	if err != nil {
		return "", "", 0, fmt.Sprintf("replace disabled: %v", err)
	}
	mainPath := canonicalPath(cfg.InputDir)
	validOverlay := make([]string, 0, len(files))
	for _, file := range files {
		if canonicalPath(file) != mainPath {
			validOverlay = append(validOverlay, file)
		}
	}
	if len(validOverlay) == 0 {
		return "", "", 0, "replace disabled: no overlay stream distinct from main input"
	}
	overlayPath := validOverlay[rng.Intn(len(validOverlay))]
	validDonors := make([]string, 0, len(validOverlay))
	for _, file := range validOverlay {
		if canonicalPath(file) != canonicalPath(overlayPath) {
			validDonors = append(validDonors, file)
		}
	}
	if len(validDonors) == 0 {
		return overlayPath, "", 0, "replace disabled: no donor stream distinct from overlay and main input"
	}
	donorPath := validDonors[rng.Intn(len(validDonors))]
	donorProbe, err := probeMedia(ctx, donorPath)
	if err != nil {
		return overlayPath, donorPath, 0, fmt.Sprintf("replace disabled: donor probe failed: %v", err)
	}
	if donorProbe.Video == nil || donorProbe.Video.Fps <= 0 || donorProbe.Duration <= 0 {
		return overlayPath, donorPath, 0, "replace disabled: donor stream has invalid video timing"
	}
	donorFrames := int64(math.Floor(donorProbe.Duration * donorProbe.Video.Fps))
	if donorFrames <= 0 {
		return overlayPath, donorPath, 0, "replace disabled: donor stream has no frames"
	}
	return overlayPath, donorPath, donorFrames, ""
}

func assignDonorFrames(rng *rand.Rand, events []Event, donorPath string, donorFrames int64, tmpDir string) {
	used := map[int64]bool{}
	for i := range events {
		for {
			donor := int64(rng.Int63n(donorFrames))
			if used[donor] {
				continue
			}
			used[donor] = true
			events[i].DonorFrame = donor
			events[i].DonorPath = donorPath
			events[i].DonorImagePath = filepath.Join(tmpDir, fmt.Sprintf("replace_donor_%03d.png", i))
			break
		}
	}
}

func canonicalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
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
