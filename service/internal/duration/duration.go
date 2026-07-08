package duration

const (
	DefaultShotDurationSec = 10.0
	MinShotDurationSec     = 10.0
	// MaxShotDurationSec matches Agnes single-video ceiling (18s ≈ 441 frames @24fps).
	MaxShotDurationSec = 18.0
)

// ResolveShotVideoDuration normalizes per-shot video length for keyframe I2V (10–18s).
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
