package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const envKey = "AURELIA_HOME"
const defaultDir = ".aurelia"

// PathResolver resolves and exposes all instance-directory paths.
type PathResolver struct {
	root string
}

// New returns a PathResolver whose root is:
//   - $AURELIA_HOME if set and non-empty
//   - $HOME/.aurelia otherwise
//
// Returns a descriptive error if $AURELIA_HOME is unset and os.UserHomeDir() fails.
func New() (*PathResolver, error) {
	if override := os.Getenv(envKey); override != "" {
		return &PathResolver{root: override}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("runtime: cannot resolve instance root: os.UserHomeDir failed and %s is not set: %w", envKey, err)
	}
	return &PathResolver{root: filepath.Join(home, defaultDir)}, nil
}

// Root returns the instance root directory.
func (r *PathResolver) Root() string { return r.root }

// Config returns the path to the config/ subdirectory.
func (r *PathResolver) Config() string { return filepath.Join(r.root, "config") }

// AppConfig returns the path to the main app config JSON file.
func (r *PathResolver) AppConfig() string { return filepath.Join(r.Config(), "app.json") }

// Data returns the path to the data/ subdirectory.
func (r *PathResolver) Data() string { return filepath.Join(r.root, "data") }

// Memory returns the path to the memory/ subdirectory.
func (r *PathResolver) Memory() string { return filepath.Join(r.root, "memory") }

// MemoryPersonas returns the path to the memory/personas/ subdirectory.
func (r *PathResolver) MemoryPersonas() string { return filepath.Join(r.root, "memory", "personas") }

// Agents returns the path to the agents/ subdirectory.
func (r *PathResolver) Agents() string { return filepath.Join(r.root, "agents") }

// DBPath returns the path to a named database file inside the data/ subdirectory.
func (r *PathResolver) DBPath(name string) string { return filepath.Join(r.Data(), name) }

// SanitizeCwd converts an absolute path to a Claude Code-style sanitized key.
// Slashes become dashes, drive prefixes are stripped.
// Example: /media/rafael/projetos/app → -media-rafael-projetos-app
func SanitizeCwd(cwd string) string {
	return strings.ReplaceAll(filepath.ToSlash(cwd), "/", "-")
}

// ProjectMemoryDir returns the per-project private memory directory:
// ~/.aurelia/projects/<sanitized-cwd>/memory/
func (r *PathResolver) ProjectMemoryDir(cwd string) string {
	return filepath.Join(r.root, "projects", SanitizeCwd(cwd), "memory")
}

// ProjectTeamMemoryDir returns the per-project team (shared) memory directory:
// ~/.aurelia/projects/<sanitized-cwd>/memory/team/
func (r *PathResolver) ProjectTeamMemoryDir(cwd string) string {
	return filepath.Join(r.ProjectMemoryDir(cwd), "team")
}
