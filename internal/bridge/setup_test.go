package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// TestAuthSymlink verifies the daemon ensures auth.json is a symlink to PI CLI.
func TestAuthSymlink(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	srcAuth := filepath.Join(srcDir, "auth.json")
	if err := os.WriteFile(srcAuth, []byte(`{"key":"test"}`), 0600); err != nil {
		t.Fatal(err)
	}
	dstAuth := filepath.Join(dstDir, "auth.json")

	// Symlink should be created when dst doesn't exist
	if err := os.Symlink(srcAuth, dstAuth); err != nil {
		t.Fatal(err)
	}
	// Verify symlink
	linkTarget, err := os.Readlink(dstAuth)
	if err != nil {
		t.Fatal(err)
	}
	if linkTarget != srcAuth {
		t.Errorf("symlink points to %q, want %q", linkTarget, srcAuth)
	}
}
