package recipe

import (
	"fmt"
	"math"
	"sort"
)

type TemporalEffectType string

const (
	EffectAudioMicro   TemporalEffectType = "AudioMicro"
	EffectAudioFreeze  TemporalEffectType = "AudioFreeze"
	EffectVideoMicro   TemporalEffectType = "VideoMicro"
	EffectVideoFreeze  TemporalEffectType = "VideoFreeze"
	EffectVideoReplace TemporalEffectType = "VideoReplace"
)

type TemporalHardness string

const (
	HardnessHard TemporalHardness = "HARD"
	HardnessSoft TemporalHardness = "SOFT"
)

type TemporalEvent struct {
	ID             string
	EffectType     TemporalEffectType
	StartSec       float64
	EndSec         float64
	Hardness       TemporalHardness
	MinDistanceSec float64
	SeedLineage    string
	Frame          int64 `json:",omitempty"`
}

type TemporalDrop struct {
	ID             string
	EffectType     TemporalEffectType
	StartSec       float64
	EndSec         float64
	Reason         string
	ConflictingID  string
	ConflictEffect TemporalEffectType
}

type TemporalEffectStats struct {
	Requested   int
	Effective   int
	Dropped     int
	DropReasons []string
}

type temporalCandidate struct {
	event       TemporalEvent
	audioMicro  *SpeedEvent
	audioFreeze *AudioFreezeEvent
	videoMicro  *SpeedEvent
	videoEvent  *Event
}

type temporalCoordinationResult struct {
	AudioMicro   []SpeedEvent
	AudioFreeze  []AudioFreezeEvent
	VideoMicro   []SpeedEvent
	VideoFreeze  []Event
	VideoReplace []Event
	Accepted     []TemporalEvent
	Dropped      []TemporalDrop
	Stats        map[TemporalEffectType]TemporalEffectStats
}

func coordinateTemporalEvents(candidates []temporalCandidate) temporalCoordinationResult {
	stats := map[TemporalEffectType]TemporalEffectStats{}
	for _, candidate := range candidates {
		st := stats[candidate.event.EffectType]
		st.Requested++
		stats[candidate.event.EffectType] = st
	}

	result := temporalCoordinationResult{Stats: stats}
	acceptedCandidates := make([]temporalCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		conflict, ok := firstTemporalConflict(candidate.event, result.Accepted)
		if ok {
			drop := TemporalDrop{
				ID:             candidate.event.ID,
				EffectType:     candidate.event.EffectType,
				StartSec:       candidate.event.StartSec,
				EndSec:         candidate.event.EndSec,
				Reason:         conflictReason(candidate.event, conflict),
				ConflictingID:  conflict.ID,
				ConflictEffect: conflict.EffectType,
			}
			result.Dropped = append(result.Dropped, drop)
			st := result.Stats[candidate.event.EffectType]
			st.Dropped++
			st.DropReasons = append(st.DropReasons, fmt.Sprintf("%s conflicts with %s", drop.ID, drop.ConflictingID))
			result.Stats[candidate.event.EffectType] = st
			continue
		}
		result.Accepted = append(result.Accepted, candidate.event)
		acceptedCandidates = append(acceptedCandidates, candidate)
		st := result.Stats[candidate.event.EffectType]
		st.Effective++
		result.Stats[candidate.event.EffectType] = st
	}

	for _, candidate := range acceptedCandidates {
		switch candidate.event.EffectType {
		case EffectAudioMicro:
			if candidate.audioMicro != nil {
				result.AudioMicro = append(result.AudioMicro, *candidate.audioMicro)
			}
		case EffectAudioFreeze:
			if candidate.audioFreeze != nil {
				result.AudioFreeze = append(result.AudioFreeze, *candidate.audioFreeze)
			}
		case EffectVideoMicro:
			if candidate.videoMicro != nil {
				result.VideoMicro = append(result.VideoMicro, *candidate.videoMicro)
			}
		case EffectVideoFreeze:
			if candidate.videoEvent != nil {
				result.VideoFreeze = append(result.VideoFreeze, *candidate.videoEvent)
			}
		case EffectVideoReplace:
			if candidate.videoEvent != nil {
				result.VideoReplace = append(result.VideoReplace, *candidate.videoEvent)
			}
		}
	}
	sort.Slice(result.AudioMicro, func(i, j int) bool { return result.AudioMicro[i].StartSec < result.AudioMicro[j].StartSec })
	sort.Slice(result.AudioFreeze, func(i, j int) bool { return result.AudioFreeze[i].StartSec < result.AudioFreeze[j].StartSec })
	sort.Slice(result.VideoMicro, func(i, j int) bool { return result.VideoMicro[i].StartSec < result.VideoMicro[j].StartSec })
	sort.Slice(result.VideoFreeze, func(i, j int) bool { return result.VideoFreeze[i].Frame < result.VideoFreeze[j].Frame })
	sort.Slice(result.VideoReplace, func(i, j int) bool { return result.VideoReplace[i].Frame < result.VideoReplace[j].Frame })
	sort.Slice(result.Accepted, func(i, j int) bool {
		if result.Accepted[i].StartSec == result.Accepted[j].StartSec {
			return result.Accepted[i].ID < result.Accepted[j].ID
		}
		return result.Accepted[i].StartSec < result.Accepted[j].StartSec
	})
	return result
}

func firstTemporalConflict(candidate TemporalEvent, accepted []TemporalEvent) (TemporalEvent, bool) {
	for _, existing := range accepted {
		if temporalEventsConflict(candidate, existing) {
			return existing, true
		}
	}
	return TemporalEvent{}, false
}

func temporalEventsConflict(a, b TemporalEvent) bool {
	if a.StartSec < b.EndSec && b.StartSec < a.EndSec {
		return true
	}
	spacing := math.Max(a.MinDistanceSec, b.MinDistanceSec)
	if spacing <= 0 {
		return false
	}
	return temporalGap(a, b) < spacing
}

func temporalGap(a, b TemporalEvent) float64 {
	if a.EndSec <= b.StartSec {
		return b.StartSec - a.EndSec
	}
	if b.EndSec <= a.StartSec {
		return a.StartSec - b.EndSec
	}
	return 0
}

func conflictReason(a, b TemporalEvent) string {
	if a.Hardness == HardnessHard && b.Hardness == HardnessHard {
		return "hard_hard_temporal_conflict"
	}
	if a.Hardness == HardnessHard || b.Hardness == HardnessHard {
		return "hard_soft_temporal_conflict"
	}
	return "soft_soft_temporal_conflict"
}

func temporalMinDistance(primary, fallback float64) float64 {
	if primary > fallback {
		return primary
	}
	return fallback
}
