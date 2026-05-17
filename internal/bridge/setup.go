package bridge

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

const bridgePackageJSON = `{
  "name": "aurelia-bridge",
  "version": "1.0.0",
  "private": true,
  "type": "module",
  "scripts": {
    "build": "esbuild index.ts --bundle --platform=node --target=node18 --outfile=bundle.js --format=esm --banner:js=\"import { createRequire as __piCreateRequire } from 'module';const require = __piCreateRequire(import.meta.url);\""
  },
  "dependencies": {
    "@earendil-works/pi-coding-agent": "latest",
    "esbuild": "^0.28.0"
  }
}
`

// EnsureBridge checks if the bridge is set up at targetDir. If not,
// creates it with package.json, runs npm install, and builds bundle.js
// from TypeScript source. Returns the directory path.
// If bundleJS is non-nil, it is written as bundle.js (legacy embedded path).
func EnsureBridge(targetDir string, bundleJS []byte) (string, error) {
	bundlePath := filepath.Join(targetDir, "bundle.js")
	nodeModules := filepath.Join(targetDir, "node_modules")

	needsNpmInstall := false
	if _, err := os.Stat(nodeModules); os.IsNotExist(err) {
		needsNpmInstall = true
	}

	// Check if bundle.js exists and matches embedded (if provided).
	bundleExists := false
	if info, err := os.Stat(bundlePath); err == nil && info.Size() > 0 {
		if len(bundleJS) > 0 && info.Size() == int64(len(bundleJS)) {
			existing, readErr := os.ReadFile(bundlePath)
			bundleExists = readErr == nil && string(existing) == string(bundleJS)
		} else {
			bundleExists = true // already on disk, no embedded to compare
		}
	}

	if bundleExists && !needsNpmInstall {
		return targetDir, nil
	}

	if needsNpmInstall {
		slog.Info("Setting up Bridge for first time...")
	} else if !bundleExists {
		slog.Info("Building Bridge bundle...")
	}

	if err := os.MkdirAll(targetDir, 0700); err != nil {
		return "", fmt.Errorf("create bridge dir: %w", err)
	}

	// Create isolated PI agent directory for Aurelia to avoid credential
	// conflicts with PI CLI. If the user already has PI CLI configured,
	// copy its auth.json and models.json once for inheritance.
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Warn("cannot determine home directory for PI agent dir", "error", err)
	} else if home != "" {
		aureliaPiAgentDir := filepath.Join(home, ".aurelia", "pi-agent")
		if err := os.MkdirAll(aureliaPiAgentDir, 0700); err != nil {
			slog.Warn("failed to create isolated PI agent dir", "error", err)
		} else {
			// One-time inheritance: copy PI CLI config if it exists and
			// Aurelia's isolated dir is empty.
			piCliDir := filepath.Join(home, ".pi", "agent")
			if _, statErr := os.Stat(piCliDir); statErr == nil {
				if isEmptyDir(aureliaPiAgentDir) {
					if err := copyDirContents(piCliDir, aureliaPiAgentDir); err != nil {
						slog.Warn("failed to inherit PI CLI config", "error", err)
					} else {
						slog.Info("Inherited PI CLI config into isolated agent directory")
					}
				}
			}
		}
	}

	buildingFromSource := !bundleExists && len(bundleJS) == 0
	if buildingFromSource {
		if err := writeBridgeSource(targetDir); err != nil {
			return "", err
		}
		needsNpmInstall = true
	}

	// Write package.json and npm install first (needed for both embedded and TS build paths).
	if needsNpmInstall {
		pkgPath := filepath.Join(targetDir, "package.json")
		if err := os.WriteFile(pkgPath, []byte(bridgePackageJSON), 0600); err != nil {
			return "", fmt.Errorf("write package.json: %w", err)
		}

		slog.Info("Installing PI SDK bridge dependencies (npm install)...")
		cmd := exec.Command("npm", "install", "--production", "--no-optional")
		cmd.Dir = targetDir
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("npm install failed: %w", err)
		}
	}

	// Write or build bundle.js
	if !bundleExists {
		if len(bundleJS) > 0 {
			// Embedded bundle provided — write it directly.
			tmpPath := bundlePath + ".tmp"
			if err := os.WriteFile(tmpPath, bundleJS, 0600); err != nil {
				os.Remove(tmpPath)
				return "", fmt.Errorf("write bundle.js.tmp: %w", err)
			}
			if err := os.Rename(tmpPath, bundlePath); err != nil {
				os.Remove(tmpPath)
				return "", fmt.Errorf("rename bundle.js.tmp → bundle.js: %w", err)
			}
		} else {
			// No embedded bundle — build from TypeScript source.
			slog.Info("Building Bridge from TypeScript source (esbuild)...")
			cmd := exec.Command("npm", "run", "build")
			cmd.Dir = targetDir
			cmd.Stdout = os.Stderr
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				return "", fmt.Errorf("npm run build failed: %w", err)
			}
		}
	}

	slog.Info("Bridge setup complete.")
	return targetDir, nil
}

func writeBridgeSource(targetDir string) error {
	if len(EmbeddedBridgeTS) == 0 {
		return fmt.Errorf("bridge source is not embedded")
	}
	indexPath := filepath.Join(targetDir, "index.ts")
	if err := os.WriteFile(indexPath, EmbeddedBridgeTS, 0600); err != nil {
		return fmt.Errorf("write index.ts: %w", err)
	}
	return nil
}

// isEmptyDir returns true if the directory exists and contains no entries.
func isEmptyDir(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true
	}
	return len(entries) == 0
}

// copyDirContents copies all files from src to dst recursively.
func copyDirContents(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0600)
	})
}
