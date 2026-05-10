package ffprobe

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type dtoRoot struct {
	Format  dtoFormat   `json:"format"`
	Streams []dtoStream `json:"streams"`
}

type dtoFormat struct {
	Duration string `json:"duration"`
	BitRate  string `json:"bit_rate"`
	Filename string `json:"filename"`
}

type dtoStream struct {
	CodecType  string `json:"codec_type"`
	CodecName  string `json:"codec_name"`
	SampleRate string `json:"sample_rate"`
	RFrameRate string `json:"r_frame_rate"`
	Profile    string `json:"profile"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Channels   int    `json:"channels"`
}
type ProbeData struct {
	Duration float64
	Bitrate  int64
	Video    *VideoStream
	Audio    *AudioStream
}

type VideoStream struct {
	Codec   string
	Profile string
	Width   int64
	Height  int64
	Fps     float64
}

type AudioStream struct {
	Codec      string
	SampleRate int
	Channels   int
}

var (
	ErrFfprobeNotFound = errors.New("ffprobe executable not found")
	ErrFileNotFound    = errors.New("file not found")
	ErrInvalidMedia    = errors.New("invalid media")
	ErrTimeout         = errors.New("timeout")
	ErrParseError      = errors.New("parse error")
)

func parseFPS(fpsStr string) (float64, error) {
	parts := strings.Split(fpsStr, "/")
	if len(parts) != 2 {
		return strconv.ParseFloat(fpsStr, 64)
	}

	num, err1 := strconv.ParseFloat(parts[0], 64)
	den, err2 := strconv.ParseFloat(parts[1], 64)

	if err1 != nil || err2 != nil || den == 0 {
		return 0, ErrParseError
	}

	return num / den, nil
}

func Probe(ctx context.Context, filePath string) (*ProbeData, error) {
	path, err := exec.LookPath("ffprobe")
	if err != nil {
		return nil, ErrFfprobeNotFound
	}
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, ErrFileNotFound
	}
	cmd := exec.CommandContext(ctx, path, "-print_format", "json", "-show_format", "-show_streams", "-loglevel", "quiet", filePath)

	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ErrTimeout
		}

		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, ErrInvalidMedia
		}

		return nil, ErrInvalidMedia
	}

	var raw dtoRoot
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, ErrParseError
	}

	return parseProbeData(raw)
}

func parseProbeData(raw dtoRoot) (*ProbeData, error) {
	res := &ProbeData{}

	dur, err := strconv.ParseFloat(raw.Format.Duration, 64)
	if err != nil {
		return nil, ErrParseError
	}
	res.Duration = dur

	br, err := strconv.ParseInt(raw.Format.BitRate, 10, 64)
	if err != nil {
		return nil, ErrParseError
	}
	res.Bitrate = br

	for _, stream := range raw.Streams {
		if stream.CodecType == "video" && res.Video == nil {
			fps, err := parseFPS(stream.RFrameRate)
			if err != nil {
				return nil, ErrParseError
			}

			res.Video = &VideoStream{
				Codec:   stream.CodecName,
				Profile: stream.Profile,
				Width:   int64(stream.Width),
				Height:  int64(stream.Height),
				Fps:     fps,
			}
		} else if stream.CodecType == "audio" && res.Audio == nil {
			sr, err := strconv.Atoi(stream.SampleRate)
			if err != nil {
				return nil, ErrParseError
			}

			res.Audio = &AudioStream{
				Codec:      stream.CodecName,
				SampleRate: sr,
				Channels:   stream.Channels,
			}
		}
	}
	if res.Video == nil {
		return nil, ErrInvalidMedia
	}

	return res, nil
}
