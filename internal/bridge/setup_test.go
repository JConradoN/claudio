package bridge

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEmbeddedBridgeSourcePresent(t *testing.T) {
	if len(EmbeddedBridgeTS) == 0 {
		t.Fatal("expected embedded TypeScript bridge source")
	}
	if !strings.Contains(string(EmbeddedBridgeTS), "createAgentSession") {
		t.Fatal("embedded bridge source does not look like the PI SDK bridge")
	}
}

func TestBridgePackageJSONCanBuildBundle(t *testing.T) {
	var pkg struct {
		Scripts      map[string]string `json:"scripts"`
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(bridgePackageJSON), &pkg); err != nil {
		t.Fatalf("bridgePackageJSON is invalid JSON: %v", err)
	}
	if !strings.Contains(pkg.Scripts["build"], "esbuild index.ts") {
		t.Fatalf("missing esbuild build script: %q", pkg.Scripts["build"])
	}
	if pkg.Dependencies["@earendil-works/pi-coding-agent"] == "" {
		t.Fatal("missing PI SDK dependency")
	}
	if pkg.Dependencies["esbuild"] == "" {
		t.Fatal("missing esbuild dependency")
	}
}
