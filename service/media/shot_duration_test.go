package media

import "testing"

func TestResolveShotVideoDuration(t *testing.T) {
	tests := []struct {
		in, want float64
	}{
		{0, DefaultShotDurationSec},
		{4, MinShotDurationSec},
		{8, 8.0},
		{12, 12.0},
		{15, 15.0},
		{20, MaxShotDurationSec},
	}
	for _, tc := range tests {
		if got := ResolveShotVideoDuration(tc.in); got != tc.want {
			t.Fatalf("ResolveShotVideoDuration(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestSortShotNumbers(t *testing.T) {
	got := SortShotNumbers([]int{5, 1, 3})
	want := []int{1, 3, 5}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SortShotNumbers = %v, want %v", got, want)
		}
	}
}
