package security

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogAudit_WritesDedicatedFile(t *testing.T) {
	var stderr bytes.Buffer
	path := filepath.Join(t.TempDir(), "audit.log")
	SetAuditWriter(&stderr)
	SetAuditFile(path, 1024, 1)
	defer SetAuditFile("", 0, 0)
	defer SetAuditWriter(os.Stderr)

	LogAudit(AuditEvent{Decision: DecisionBlock, ToolName: "Bash", Reason: "blocked", Profile: ProfileExecuteSafe})

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(audit.log) error = %v", err)
	}
	if !strings.Contains(string(content), `"tool_name":"Bash"`) {
		t.Fatalf("audit file missing tool event: %s", content)
	}
	if !strings.Contains(stderr.String(), "[security]") {
		t.Fatalf("stderr writer missing security prefix: %s", stderr.String())
	}
}

func TestLogAudit_RotatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	SetAuditWriter(&bytes.Buffer{})
	SetAuditFile(path, 80, 2)
	defer SetAuditFile("", 0, 0)
	defer SetAuditWriter(os.Stderr)

	for i := 0; i < 3; i++ {
		LogAudit(AuditEvent{Decision: DecisionAllow, ToolName: "Read", Reason: strings.Repeat("x", 60), Profile: ProfileReadOnly})
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("expected rotated audit backup: %v", err)
	}
}
