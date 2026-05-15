package telegram

import (
	"strings"
	"testing"
	"time"
)

func TestFormatProgressDuration(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		d    time.Duration
		want string
	}{
		{name: "seconds", d: 12 * time.Second, want: "12s"},
		{name: "minutes", d: 2*time.Minute + 34*time.Second, want: "2m 34s"},
		{name: "negative", d: -time.Second, want: "0s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := formatProgressDuration(tc.d); got != tc.want {
				t.Fatalf("formatProgressDuration(%s) = %q, want %q", tc.d, got, tc.want)
			}
		})
	}
}

func TestProgressTextShowsTimerAndLastEightTools(t *testing.T) {
	t.Parallel()

	tools := []string{"one", "two", "three", "four", "five", "six", "seven", "eight", "nine"}
	got := progressText(tools, 2*time.Minute+34*time.Second)

	if !strings.HasPrefix(got, "⏱️ 2m 34s\n") {
		t.Fatalf("progress text should start with timer, got %q", got)
	}
	if strings.Contains(got, "one") {
		t.Fatalf("progress text should keep only last 8 tools, got %q", got)
	}
	for _, want := range tools[1:] {
		if !strings.Contains(got, want) {
			t.Fatalf("progress text missing %q: %q", want, got)
		}
	}
}
