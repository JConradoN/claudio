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

func TestHasNonEmptyAuth(t *testing.T) {
	tests := []struct {
		name     string
		content  string // empty string means file does not exist
		want     bool
	}{
		{
			name:    "file missing",
			content: "", // don't create file
			want:    false,
		},
		{
			name:    "file empty",
			content: "",
			want:    false,
		},
		{
			name:    "whitespace only",
			content: "   \n\t  ",
			want:    false,
		},
		{
			name:    "empty JSON object",
			content: "{}",
			want:    false,
		},
		{
			name:    "whitespace between braces",
			content: "{ }",
			want:    true, // not a compact "{}", so treated as non-empty
		},
		{
			name:    "valid credentials",
			content: `{"access_token":"abc","refresh_token":"def"}`,
			want:    true,
		},
		{
			name:    "minimal non-empty JSON",
			content: `{"key":"value"}`,
			want:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.content != "" || tt.name == "file empty" {
				// Write file for all cases except "file missing"
				if tt.name != "file missing" {
					path := filepath.Join(dir, "auth.json")
					if err := os.WriteFile(path, []byte(tt.content), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			got := hasNonEmptyAuth(dir)
			if got != tt.want {
				t.Errorf("hasNonEmptyAuth(%q) = %v, want %v", dir, got, tt.want)
			}
		})
	}
}

// Test that a dir with a sessions/ subdir but no auth.json is treated as empty.
func TestHasNonEmptyAuth_IgnoresNonAuthFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sessions"), 0700); err != nil {
		t.Fatal(err)
	}
	if hasNonEmptyAuth(dir) {
		t.Error("expected false when only sessions/ subdir exists, no auth.json")
	}
}
