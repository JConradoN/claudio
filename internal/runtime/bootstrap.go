package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// requiredDirs lists all directories Bootstrap must ensure exist.
// Order does not matter — MkdirAll creates parent paths as needed.
var requiredDirs = []func(*PathResolver) string{
	(*PathResolver).Config,
	(*PathResolver).Data,
	(*PathResolver).Memory,
	(*PathResolver).MemoryPersonas,
	(*PathResolver).Agents,
}

// Bootstrap creates the full instance directory tree with 0700 permissions.
// It is safe to call multiple times — existing directories and files are not modified.
// On Windows, the 0700 permission argument is accepted but has no effect (ACL-based permissions).
func Bootstrap(r *PathResolver) error {
	for _, dir := range requiredDirs {
		if err := os.MkdirAll(dir(r), 0700); err != nil {
			return fmt.Errorf("runtime: bootstrap failed to create %q: %w", dir(r), err)
		}
	}
	return nil
}

// BootstrapProjectMemory ensures the per-project memory directories exist
// and creates empty MEMORY.md index files if they don't exist yet.
func BootstrapProjectMemory(r *PathResolver, cwd string) error {
	if strings.TrimSpace(cwd) == "" {
		return nil
	}

	dirs := []string{
		r.ProjectMemoryDir(cwd),
		r.ProjectTeamMemoryDir(cwd),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("runtime: bootstrap project memory %q: %w", dir, err)
		}
		indexPath := filepath.Join(dir, "MEMORY.md")
		if _, err := os.Stat(indexPath); os.IsNotExist(err) {
			if err := os.WriteFile(indexPath, nil, 0600); err != nil {
				return fmt.Errorf("runtime: create memory index %q: %w", indexPath, err)
			}
		}
	}
	return nil
}
