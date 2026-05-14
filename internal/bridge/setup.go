package bridge

import (
	"fmt"
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
  "dependencies": {
    "@earendil-works/pi-coding-agent": "latest"
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
