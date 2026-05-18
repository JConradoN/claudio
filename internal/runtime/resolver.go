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
	if strings.TrimSpace(cwd) == "" {
		return ""
	}
	return strings.ReplaceAll(filepath.ToSlash(filepath.Clean(cwd)), "/", "-")
}

// ResolveProjectCwd validates and canonicalizes a user-provided project path.
// Project bindings use this so equivalent paths such as /repo/app and
// /repo/app/ map to the same persisted cwd and project memory slug.
func ResolveProjectCwd(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("empty cwd")
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("resolve absolute cwd %q: %w", path, err)
	}
	clean := filepath.Clean(abs)
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", fmt.Errorf("resolve cwd symlinks %q: %w", clean, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat cwd %q: %w", resolved, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("cwd %q is not a directory", resolved)
	}
	if err := rejectSensitiveProjectCwd(resolved); err != nil {
		return "", err
	}
	if !looksLikeProjectDir(resolved) {
		return "", fmt.Errorf("cwd %q is not recognized as a project directory", resolved)
	}
	return filepath.Clean(resolved), nil
}

func looksLikeProjectDir(dir string) bool {
	markers := []string{
		".git", "go.mod", "package.json", "pyproject.toml", "Cargo.toml",
		"pom.xml", "build.gradle", "Makefile", "AGENTS.md", "CLAUDE.md", "README.md",
	}
	for _, marker := range markers {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return true
		}
	}
	return false
}

func rejectSensitiveProjectCwd(cwd string) error {
	clean := filepath.Clean(cwd)
	if clean == string(filepath.Separator) {
		return fmt.Errorf("cwd %q is not an allowed project directory", clean)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	home = filepath.Clean(home)
	if clean == home {
		return fmt.Errorf("cwd %q is not an allowed project directory", clean)
	}
	blockedPrefixes := []string{
		filepath.Join(home, ".ssh"),
		filepath.Join(home, ".config"),
		filepath.Join(home, ".aurelia"),
	}
	for _, prefix := range blockedPrefixes {
		if clean == prefix || strings.HasPrefix(clean, prefix+string(filepath.Separator)) {
			return fmt.Errorf("cwd %q is not an allowed project directory", clean)
		}
	}
	return nil
}

// ProjectSlug returns the filesystem-safe key used for project-scoped state.
func ProjectSlug(cwd string) string { return SanitizeCwd(cwd) }

// ProjectMemoryDir returns the per-project private memory directory:
// ~/.aurelia/projects/<sanitized-cwd>/memory/
func (r *PathResolver) ProjectMemoryDir(cwd string) string {
	return filepath.Join(r.root, "projects", ProjectSlug(cwd), "memory")
}

// ConversationProjectMemoryDir returns project-private memory for one
// conversation. This prevents notes from one Telegram group/topic leaking into
// another conversation that happens to bind the same repository.
func (r *PathResolver) ConversationProjectMemoryDir(cwd string, chatID int64, threadID int) string {
	return filepath.Join(r.ProjectMemoryDir(cwd), "conversations", fmt.Sprintf("chat_%d", chatID), fmt.Sprintf("thread_%d", threadID))
}

// ProjectTeamMemoryDir returns the per-project team (shared) memory directory:
// ~/.aurelia/projects/<sanitized-cwd>/memory/team/
func (r *PathResolver) ProjectTeamMemoryDir(cwd string) string {
	return filepath.Join(r.ProjectMemoryDir(cwd), "team")
}
