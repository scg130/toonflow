package service

import "sort"

const (
	DefaultShotDurationSec = 4.0
	MinShotDurationSec     = 3.0
	MaxShotDurationSec     = 5.0
)

// ResolveShotVideoDuration normalizes per-shot video length for I2V (Douyin short-drama rhythm).
func ResolveShotVideoDuration(duration float64) float64 {
	if duration <= 0 {
		return DefaultShotDurationSec
	}
	if duration < MinShotDurationSec {
		return MinShotDurationSec
	}
	if duration > MaxShotDurationSec {
		return MaxShotDurationSec
	}
	return float64(int(duration*2+0.5)) / 2
}

// SortShotNumbers returns a copy sorted ascending.
func SortShotNumbers(shots []int) []int {
	out := append([]int(nil), shots...)
	sort.Ints(out)
	return out
}
