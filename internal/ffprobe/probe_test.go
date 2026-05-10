package ffprobe

import (
	"errors"
	"testing"
)

func TestParseProbeDataAcceptsVideoWithoutAudio(t *testing.T) {
	data, err := parseProbeData(dtoRoot{
		Format:  dtoFormat{Duration: "1.5", BitRate: "100000", Filename: "video-only.mp4"},
		Streams: []dtoStream{{CodecType: "video", CodecName: "h264", RFrameRate: "25/1", Profile: "High", Width: 1920, Height: 1080}},
	})
	if err != nil {
		t.Fatalf("parseProbeData failed: %v", err)
	}
	if data.Video == nil {
		t.Fatal("expected video stream")
	}
	if data.Audio != nil {
		t.Fatalf("expected missing audio to be valid and represented as nil, got %#v", data.Audio)
	}
}

func TestParseProbeDataRejectsAudioOnlyMedia(t *testing.T) {
	_, err := parseProbeData(dtoRoot{
		Format:  dtoFormat{Duration: "1.5", BitRate: "100000", Filename: "audio-only.m4a"},
		Streams: []dtoStream{{CodecType: "audio", CodecName: "aac", SampleRate: "44100", Channels: 2}},
	})
	if !errors.Is(err, ErrInvalidMedia) {
		t.Fatalf("expected ErrInvalidMedia for media without video, got %v", err)
	}
}
