package pixel

import "testing"

func TestAnalyzeFramesChoosesStableHomogeneousArea(t *testing.T) {
	frames := []Frame{
		newSmartTestFrame(4, 4, 20, 80, 0),
		newSmartTestFrame(4, 4, 20, 80, 30),
		newSmartTestFrame(4, 4, 20, 80, 60),
	}

	area, err := AnalyzeFrames(frames, 2)
	if err != nil {
		t.Fatalf("AnalyzeFrames failed: %v", err)
	}
	if area.X != 0 || area.Y != 0 || area.Width != 2 || area.Height != 2 {
		t.Fatalf("expected stable homogeneous top-left cell, got %+v", area)
	}
	if area.Confidence < DefaultSmartConfidenceThreshold {
		t.Fatalf("expected high confidence, got %.3f", area.Confidence)
	}
}

func newSmartTestFrame(width, height int, stableValue, noisyBase byte, noisyDelta byte) Frame {
	luma := make([]byte, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			idx := y*width + x
			if x < 2 && y < 2 {
				luma[idx] = stableValue
				continue
			}
			luma[idx] = noisyBase + noisyDelta + byte((x+y)%2)*50
		}
	}
	return Frame{Width: width, Height: height, Luma: luma}
}
