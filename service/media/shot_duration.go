package media

import (
	"sort"

	"toonflow/service/internal/duration"
)

const (
	DefaultShotDurationSec = duration.DefaultShotDurationSec
	MinShotDurationSec     = duration.MinShotDurationSec
	MaxShotDurationSec     = duration.MaxShotDurationSec
)

// ResolveShotVideoDuration normalizes per-shot video length for I2V.
func ResolveShotVideoDuration(d float64) float64 {
	return duration.ResolveShotVideoDuration(d)
}

// SortShotNumbers returns a copy sorted ascending.
func SortShotNumbers(shots []int) []int {
	out := append([]int(nil), shots...)
	sort.Ints(out)
	return out
}
