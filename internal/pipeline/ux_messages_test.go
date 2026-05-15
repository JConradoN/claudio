package pipeline

import (
	"strings"
	"testing"
	"time"
)

func TestBridgeErrorMessagesIncludeActionableHints(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		msg  string
		want string
	}{
		{name: "connect", msg: bridgeConnectErrorMessage, want: "/new"},
		{name: "retry", msg: bridgeRetryFailedMessage, want: "Dica"},
		{name: "timeout", msg: bridgeTimeoutMessage, want: "dividir em partes menores"},
		{name: "cooldown", msg: bridgeCooldownMessage(12 * time.Second), want: "~12 segundos"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(tc.msg, tc.want) {
				t.Fatalf("message %q missing %q", tc.msg, tc.want)
			}
		})
	}
}
