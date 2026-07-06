package adapter

// DefaultVideoDurationSec is used when callers omit VideoParams.Duration.
const DefaultVideoDurationSec float32 = 4.0

// FramesForVideoDuration maps seconds to Agnes I2V frame count (8n+1, round up).
func FramesForVideoDuration(seconds float32, frameRate int) int {
	const minFrames = 17
	const maxFrames = 441
	if frameRate <= 0 {
		frameRate = 24
	}
	if seconds <= 0 {
		seconds = DefaultVideoDurationSec
	}
	target := int(seconds * float32(frameRate))
	if target < minFrames {
		target = minFrames
	}
	if target > maxFrames {
		return maxFrames
	}
	// smallest 8n+1 >= target
	n := (target - 1 + 7) / 8
	frames := n*8 + 1
	if frames > maxFrames {
		return maxFrames
	}
	if frames < minFrames {
		return minFrames
	}
	return frames
}

// VideoDurationFromFrames returns playback seconds for a frame count.
func VideoDurationFromFrames(frames, frameRate int) float64 {
	if frameRate <= 0 {
		frameRate = 24
	}
	if frames <= 0 {
		return 0
	}
	return float64(frames) / float64(frameRate)
}
