package orchestrator

import (
	"context"
	"testing"

	"github.com/igormaneschy/aurelia/internal/bridge"
)

func TestValidate_FailedWorker(t *testing.T) {
	o := NewOrchestrator(newFakeBridge(), OrchestratorConfig{})

	vr, err := o.Validate(
		context.Background(),
		Task{ID: "1", Description: "test"},
		TaskResult{TaskID: "1", Success: false, Error: "timeout"},
		"validate prompt",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vr.Approved {
		t.Error("should not approve failed worker")
	}
	if !vr.ShouldRetry {
		t.Error("should suggest retry for failed worker")
	}
}

func TestValidate_ApprovedByBridge(t *testing.T) {
	fb := newFakeBridge()
	fb.SetDefault(&bridge.Event{
		Type:    "result",
		Content: `{"approved": true, "issues": [], "should_retry": false}`,
	})
	o := NewOrchestrator(fb, OrchestratorConfig{})

	vr, err := o.Validate(
		context.Background(),
		Task{ID: "1", Description: "test", Prompt: "implement X"},
		TaskResult{TaskID: "1", Success: true, Content: "implemented X"},
		"validate prompt",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !vr.Approved {
		t.Error("expected approved")
	}
}

func TestValidate_RejectedByBridge(t *testing.T) {
	fb := newFakeBridge()
	fb.SetDefault(&bridge.Event{
		Type:    "result",
		Content: `{"approved": false, "issues": ["missing error handling", "no tests"], "should_retry": true}`,
	})
	o := NewOrchestrator(fb, OrchestratorConfig{})

	vr, err := o.Validate(
		context.Background(),
		Task{ID: "1"},
		TaskResult{TaskID: "1", Success: true, Content: "did stuff"},
		"validate",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vr.Approved {
		t.Error("should not approve")
	}
	if len(vr.Issues) != 2 {
		t.Errorf("expected 2 issues, got %d", len(vr.Issues))
	}
	if !vr.ShouldRetry {
		t.Error("should suggest retry")
	}
}

func TestParseValidationResponse_JSON(t *testing.T) {
	input := `{"approved": true, "issues": [], "should_retry": false}`
	vr := parseValidationResponse(input)
	if !vr.Approved {
		t.Error("expected approved")
	}
}

func TestParseValidationResponse_JSONInText(t *testing.T) {
	input := "Here is the result:\n\n" + `{"approved": false, "issues": ["bad code"], "should_retry": true}` + "\n\nEnd."
	vr := parseValidationResponse(input)
	if vr.Approved {
		t.Error("expected not approved")
	}
	if len(vr.Issues) != 1 {
		t.Errorf("expected 1 issue, got %d", len(vr.Issues))
	}
}

func TestParseValidationResponse_HeuristicApproved(t *testing.T) {
	input := "The work looks great. Approved!"
	vr := parseValidationResponse(input)
	if !vr.Approved {
		t.Error("expected approved via heuristic")
	}
}

func TestParseValidationResponse_Unparseable(t *testing.T) {
	input := "Some random text without clear signal."
	vr := parseValidationResponse(input)
	if vr.Approved {
		t.Error("expected not approved for unparseable response")
	}
	if !vr.ShouldRetry {
		t.Error("should suggest retry for unparseable")
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`{"a": 1}`, `{"a": 1}`},
		{`text {"a": {"b": 2}} more`, `{"a": {"b": 2}}`},
		{`no json here`, ""},
		{`{unclosed`, ""},
	}
	for _, tt := range tests {
		got := extractJSON(tt.input)
		if got != tt.want {
			t.Errorf("extractJSON(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
