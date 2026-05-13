package e2e

import "github.com/igormaneschy/aurelia/internal/bridge"

// newBridgeForTest creates a bridge instance pointing at the given directory.
func newBridgeForTest(bridgeDir string) *bridge.Bridge {
	return bridge.New(bridgeDir, "")
}
