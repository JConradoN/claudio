package dream

import (
	"os"
	"testing"
	"time"
)

func TestDreamLockTimestampSurvivesCompletion(t *testing.T) {
	dir := t.TempDir()
	if err := acquireLock(dir); err != nil {
		t.Fatal(err)
	}
	touchLock(dir)

	if _, err := os.Stat(lockPath(dir)); err != nil {
		t.Fatalf("lock should remain as last-run marker: %v", err)
	}
	if lastDreamTime(dir).IsZero() {
		t.Fatal("lastDreamTime should read retained lock marker")
	}
	if time.Since(lastDreamTime(dir)) > time.Minute {
		t.Fatalf("lastDreamTime too old: %s", lastDreamTime(dir))
	}
}
