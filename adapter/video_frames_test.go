package adapter

import "testing"

func TestFramesForVideoDuration(t *testing.T) {
	tests := []struct {
		sec  float32
		want int
		minS float64
	}{
		{3, 73, 3.0},
		{4, 97, 4.0},
		{5, 121, 5.0},
		{2.5, 65, 2.5},
		{0, FramesForVideoDuration(DefaultVideoDurationSec, 24), 3.9},
	}
	for _, tc := range tests {
		got := FramesForVideoDuration(tc.sec, 24)
		if got != tc.want {
			t.Fatalf("FramesForVideoDuration(%v)=%d want %d", tc.sec, got, tc.want)
		}
		gotSec := VideoDurationFromFrames(got, 24)
		if gotSec+0.05 < tc.minS {
			t.Fatalf("FramesForVideoDuration(%v)=%d => %.2fs < min %.2fs", tc.sec, got, gotSec, tc.minS)
		}
	}
}
