package duration

const (
	DefaultShotDurationSec = 4.0
	MinShotDurationSec     = 3.0
	MaxShotDurationSec     = 5.0
)

// ResolveShotVideoDuration normalizes per-shot video length for I2V (Douyin short-drama rhythm).
func ResolveShotVideoDuration(d float64) float64 {
	if d <= 0 {
		return DefaultShotDurationSec
	}
	if d < MinShotDurationSec {
		return MinShotDurationSec
	}
	if d > MaxShotDurationSec {
		return MaxShotDurationSec
	}
	return float64(int(d*2+0.5)) / 2
}
