package bridge

import "testing"

func TestEventContent(t *testing.T) {
	tests := []struct {
		name string
		ev   Event
		want string
	}{
		{name: "both empty", ev: Event{}, want: ""},
		{name: "content only", ev: Event{Content: "c"}, want: "c"},
		{name: "text only", ev: Event{Text: "t"}, want: "t"},
		{name: "text preferred over content", ev: Event{Text: "text", Content: "content"}, want: "text"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := EventContent(tc.ev)
			if got != tc.want {
				t.Fatalf("EventContent(%+v) = %q, want %q", tc.ev, got, tc.want)
			}
		})
	}
}
