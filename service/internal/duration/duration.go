package duration

const (
	DefaultShotDurationSec = 12.0
	// MinShotDurationSec: 5-minute short-drama single-shot floor (8–15s standard).
	MinShotDurationSec = 8.0
	// MaxShotDurationSec: per-shot ceiling for 5-minute packs (Agnes still allows ~18s).
	MaxShotDurationSec = 15.0
	// MinBeatsPerShot: Agnes keyframe I2V needs ≥2 images; static dialogue still gets 2.
	MinBeatsPerShot = 2
	// MaxBeatsPerShot is Agnes mode=keyframes hard limit (at most 3 images per call).
	MaxBeatsPerShot = 3

	// TargetShotsMin/Max: 5-minute episode storyboard density (not overcut).
	TargetShotsMin = 18
	TargetShotsMax = 25
)

// ResolveShotVideoDuration normalizes per-shot video length for keyframe I2V (8–15s).
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
