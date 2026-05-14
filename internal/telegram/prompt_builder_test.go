package telegram

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/igormaneschy/aurelia/internal/session"
)

func TestLoadMemoryContents_RespectsTotalCap(t *testing.T) {
	dir := t.TempDir()
	content := strings.Repeat("x", 5000)
	for i := 0; i < 20; i++ {
		path := filepath.Join(dir, fmt.Sprintf("memory-%02d.md", i))
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	bc := &BotController{memoryDir: dir, memoryCache: newMemoryCache(), sessions: session.NewStore()}
	got := bc.loadMemoryContents(1, 0, nil)

	if len(got) > maxMemoryTotalChars {
		t.Fatalf("memory content length = %d, want <= %d", len(got), maxMemoryTotalChars)
	}
	if !strings.Contains(got, "memória truncada") {
		t.Fatalf("expected total truncation notice in memory content")
	}
}
