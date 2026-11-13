package ui

import (
	"testing"
	"time"
)

func TestFormatElapsed(t *testing.T) {
	cases := []struct {
		elapsed time.Duration
		want    string
	}{
		{0, ""},
		{-time.Second, ""},
		{500 * time.Millisecond, "0.5s"},
		{3240 * time.Millisecond, "3.2s"},
		{9960 * time.Millisecond, "10s"},
		{12 * time.Second, "12s"},
		{59600 * time.Millisecond, "1m 00s"},
		{83 * time.Second, "1m 23s"},
		{time.Hour + 2*time.Minute + 5*time.Second, "1h 02m"},
	}
	for _, tc := range cases {
		if got := formatElapsed(tc.elapsed); got != tc.want {
			t.Errorf("%v: format = %q, want %q", tc.elapsed, got, tc.want)
		}
	}
}
