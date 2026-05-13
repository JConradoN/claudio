// Package dream provides background memory consolidation and nudge review.
package dream

import (
	"context"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/runtime"
)

// DreamConfig holds tuning parameters for the dreamer.
type DreamConfig struct {
	Enabled      bool
	MinInterval  time.Duration // minimum time between dreams
	MinTurns     int           // minimum user turns before a dream can fire
	Provider     string        // PI provider to use for consolidation/extraction
	Model        string        // model to use for consolidation (dream)
	ExtractModel string        // model to use for memory extraction (legacy, unused with nudge)
	NudgeEnabled bool          // enable periodic nudge review
	NudgeTurns   int           // turns between nudge reviews
	NudgeModel   string        // model for nudge review
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() DreamConfig {
	return DreamConfig{
		Enabled:      true,
		MinInterval:  24 * time.Hour,
		MinTurns:     5,
		Model:        "claude-sonnet-4-6",
		ExtractModel: "claude-haiku-4-5",
		NudgeEnabled: true,
		NudgeTurns:   10,
		NudgeModel:   "claude-haiku-4-5",
	}
}

// Dreamer runs background memory consolidation and periodic nudge reviews.
type Dreamer struct {
	memoryDir string // global memory dir (~/.aurelia/memory)
	resolver  *runtime.PathResolver
	bridge    *bridge.Bridge
	config    DreamConfig

	turns        atomic.Int32
	running      atomic.Bool
	nudgeRunning atomic.Bool
}

// New creates a Dreamer.
func New(memoryDir string, resolver *runtime.PathResolver, br *bridge.Bridge, cfg DreamConfig) *Dreamer {
	return &Dreamer{
		memoryDir: memoryDir,
		resolver:  resolver,
		bridge:    br,
		config:    cfg,
	}
}

// AfterTurn is called after every successful user turn.
// It checks gates and fires a background dream if conditions are met.
func (d *Dreamer) AfterTurn() {
	if !d.config.Enabled {
		return
	}

	turns := int(d.turns.Add(1))

	// Gate: enough turns?
	if turns < d.config.MinTurns {
		return
	}

	// Gate: enough time since last dream?
	last := lastDreamTime(d.memoryDir)
	if !last.IsZero() && time.Since(last) < d.config.MinInterval {
		return
	}

	// Gate: not already running?
	if !d.running.CompareAndSwap(false, true) {
		return
	}

	go d.run()
}

// hasMemoryFiles checks if the memory directory contains any .md files
// besides MEMORY.md itself. Without real memories, consolidation would
// hallucinate content.
func hasMemoryFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() || e.Name() == "MEMORY.md" || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		return true
	}
	return false
}

func (d *Dreamer) run() {
	defer d.running.Store(false)

	// Skip consolidation if there are no memory files to consolidate.
	// Running on an empty directory causes the model to hallucinate content.
	if !hasMemoryFiles(d.memoryDir) {
		log.Println("[dream] skipped: no memory files to consolidate")
		return
	}

	log.Println("[dream] starting memory consolidation...")
	start := time.Now()

	if err := acquireLock(d.memoryDir); err != nil {
		log.Printf("[dream] skipped: %v", err)
		return
	}
	defer releaseLock(d.memoryDir)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	req := bridge.Request{
		Command: "query",
		Prompt:  "Consolidate memories now.",
		Options: bridge.RequestOptions{
			Provider:       d.config.Provider,
			Model:          d.config.Model,
			SystemPrompt:   consolidationPrompt,
			Cwd:            d.memoryDir,
			MaxTurns:       25,
			PermissionMode: "bypassPermissions",
			AllowedTools:   []string{"Read", "Glob", "Grep", "Write", "Edit", "Bash"},
			NoUserSettings: true,
			PersistSession: boolPtr(false),
		},
	}

	ev, err := d.bridge.ExecuteSync(ctx, req)
	if err != nil {
		log.Printf("[dream] failed: %v", err)
		return
	}
	if ev.Type == "error" {
		log.Printf("[dream] bridge error: %s", ev.Message)
		return
	}

	d.turns.Store(0)
	touchLock(d.memoryDir)

	log.Printf("[dream] completed in %s — cost=$%.4f turns=%d",
		time.Since(start).Round(time.Second), ev.CostUSD, ev.NumTurns)
}
