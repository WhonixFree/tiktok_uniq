package pixel

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"time"

	"videobatch/internal/ffprobe"
)

const (
	DefaultSmartSamples             = 7
	DefaultSmartConfidenceThreshold = 0.55
)

var (
	ErrNoFrames      = errors.New("no frames available for smart analysis")
	ErrInvalidFrame  = errors.New("invalid frame for smart analysis")
	ErrLowConfidence = errors.New("smart analysis confidence below threshold")
)

type Frame struct {
	Width  int
	Height int
	Luma   []byte
}

type Area struct {
	X          int
	Y          int
	Width      int
	Height     int
	Confidence float64
	Score      float64
}

type Analyzer interface {
	Analyze(ctx context.Context, inputPath string, probe *ffprobe.ProbeData, grid int) (Area, error)
}

type FFmpegAnalyzer struct {
	Samples int
	Timeout time.Duration
}

func (a FFmpegAnalyzer) Analyze(ctx context.Context, inputPath string, probe *ffprobe.ProbeData, grid int) (Area, error) {
	if probe == nil || probe.Video == nil {
		return Area{}, fmt.Errorf("smart analysis requires video probe data: %w", ErrInvalidFrame)
	}
	if inputPath == "" {
		return Area{}, errors.New("smart analysis input path is empty")
	}

	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return Area{}, fmt.Errorf("ffmpeg not found for smart analysis: %w", err)
	}

	samples := a.Samples
	if samples <= 0 {
		samples = DefaultSmartSamples
	}
	frameTimeout := a.Timeout
	if frameTimeout <= 0 {
		frameTimeout = 5 * time.Second
	}

	width := int(probe.Video.Width)
	height := int(probe.Video.Height)
	if width <= 0 || height <= 0 {
		return Area{}, fmt.Errorf("invalid probe dimensions %dx%d: %w", width, height, ErrInvalidFrame)
	}

	frames := make([]Frame, 0, samples)
	for _, ts := range sampleTimestamps(probe.Duration, samples) {
		frameCtx, cancel := context.WithTimeout(ctx, frameTimeout)
		frame, err := extractGrayFrame(frameCtx, ffmpegPath, inputPath, ts, width, height)
		cancel()
		if err != nil {
			return Area{}, err
		}
		frames = append(frames, frame)
	}
	return AnalyzeFrames(frames, grid)
}

func AnalyzeFrames(frames []Frame, grid int) (Area, error) {
	if len(frames) == 0 {
		return Area{}, ErrNoFrames
	}
	if grid < 2 {
		return Area{}, fmt.Errorf("smart grid must be >= 2: %w", ErrInvalidFrame)
	}

	width, height := frames[0].Width, frames[0].Height
	if width <= 0 || height <= 0 || len(frames[0].Luma) != width*height {
		return Area{}, ErrInvalidFrame
	}
	for _, frame := range frames[1:] {
		if frame.Width != width || frame.Height != height || len(frame.Luma) != width*height {
			return Area{}, ErrInvalidFrame
		}
	}

	cellW := width / grid
	cellH := height / grid
	if cellW <= 0 || cellH <= 0 {
		return Area{}, fmt.Errorf("smart grid %d is too dense for %dx%d frame: %w", grid, width, height, ErrInvalidFrame)
	}

	best := Area{Score: math.Inf(1)}
	for gy := 0; gy < grid; gy++ {
		for gx := 0; gx < grid; gx++ {
			x0 := gx * cellW
			y0 := gy * cellH
			x1 := x0 + cellW
			y1 := y0 + cellH
			if gx == grid-1 {
				x1 = width
			}
			if gy == grid-1 {
				y1 = height
			}

			motion := averageInterframeDelta(frames, x0, y0, x1, y1)
			variance := averageSpatialVariance(frames, x0, y0, x1, y1)
			score := 0.70*(motion/255.0) + 0.30*(math.Sqrt(variance)/255.0)
			if score < best.Score {
				best = Area{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0, Score: score, Confidence: clamp01(1 - score)}
			}
		}
	}
	return best, nil
}

func sampleTimestamps(duration float64, samples int) []float64 {
	if samples <= 1 || duration <= 0 {
		return []float64{0}
	}
	out := make([]float64, 0, samples)
	for i := 0; i < samples; i++ {
		fraction := (float64(i) + 0.5) / float64(samples)
		out = append(out, math.Max(0, duration*fraction))
	}
	return out
}

func extractGrayFrame(ctx context.Context, ffmpegPath, inputPath string, ts float64, width, height int) (Frame, error) {
	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-v", "error",
		"-ss", fmt.Sprintf("%.3f", ts),
		"-i", inputPath,
		"-frames:v", "1",
		"-vf", "format=gray",
		"-f", "rawvideo",
		"pipe:1",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return Frame{}, fmt.Errorf("smart frame extraction timed out: %w", ctx.Err())
		}
		return Frame{}, fmt.Errorf("smart frame extraction failed: %w: %s", err, stderr.String())
	}
	if len(out) != width*height {
		return Frame{}, fmt.Errorf("unexpected smart frame size %d, want %d: %w", len(out), width*height, ErrInvalidFrame)
	}
	return Frame{Width: width, Height: height, Luma: out}, nil
}

func averageInterframeDelta(frames []Frame, x0, y0, x1, y1 int) float64 {
	if len(frames) < 2 {
		return 0
	}
	var total float64
	var count int
	width := frames[0].Width
	for i := 1; i < len(frames); i++ {
		prev := frames[i-1].Luma
		cur := frames[i].Luma
		for y := y0; y < y1; y++ {
			row := y * width
			for x := x0; x < x1; x++ {
				delta := int(cur[row+x]) - int(prev[row+x])
				if delta < 0 {
					delta = -delta
				}
				total += float64(delta)
				count++
			}
		}
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func averageSpatialVariance(frames []Frame, x0, y0, x1, y1 int) float64 {
	var totalVariance float64
	var frameCount int
	width := frames[0].Width
	pixels := (x1 - x0) * (y1 - y0)
	if pixels <= 0 {
		return 0
	}
	for _, frame := range frames {
		var sum float64
		for y := y0; y < y1; y++ {
			row := y * width
			for x := x0; x < x1; x++ {
				sum += float64(frame.Luma[row+x])
			}
		}
		mean := sum / float64(pixels)

		var variance float64
		for y := y0; y < y1; y++ {
			row := y * width
			for x := x0; x < x1; x++ {
				diff := float64(frame.Luma[row+x]) - mean
				variance += diff * diff
			}
		}
		totalVariance += variance / float64(pixels)
		frameCount++
	}
	return totalVariance / float64(frameCount)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
