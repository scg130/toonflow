package duration

import "testing"

func TestSnapPreferredShotDuration(t *testing.T) {
	cases := []struct {
		in, want float64
	}{
		{0, 12},
		{9, 8},
		{9.5, 10},
		{11, 10},
		{11.5, 12},
		{13, 12},
		{14, 15},
		{16, 15},
	}
	for _, c := range cases {
		if got := SnapPreferredShotDuration(c.in); got != c.want {
			t.Fatalf("SnapPreferredShotDuration(%v)=%v want %v", c.in, got, c.want)
		}
	}
}
