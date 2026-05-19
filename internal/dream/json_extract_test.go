package dream

import (
	"strings"
	"testing"
)

func TestExtractJSONObject_DirectJSON(t *testing.T) {
	result, err := extractJSONObject(`{"updates":[{"layer":"global"}]}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != `{"updates":[{"layer":"global"}]}` {
		t.Fatalf("expected full JSON, got %q", result)
	}
}

func TestExtractJSONObject_WithLeadingText(t *testing.T) {
	raw := `Here is the JSON: {"key": "value"} and more text`
	result, err := extractJSONObject(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != `{"key": "value"}` {
		t.Fatalf("expected exact JSON object, got %q", result)
	}
}

func TestExtractJSONObject_FencedJSON(t *testing.T) {
	raw := "```json\n{\"key\": \"value\"}\n```"
	result, err := extractJSONObject(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != `{"key": "value"}` {
		t.Fatalf("expected JSON without fences, got %q", result)
	}
}

func TestExtractJSONObject_FencedNoLang(t *testing.T) {
	raw := "```\n{\"key\": \"value\"}\n```"
	result, err := extractJSONObject(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != `{"key": "value"}` {
		t.Fatalf("expected JSON without fences, got %q", result)
	}
}

func TestExtractJSONObject_EmptyString(t *testing.T) {
	_, err := extractJSONObject("")
	if err == nil {
		t.Fatal("expected error for empty string")
	}
}

func TestExtractJSONObject_NoBrace(t *testing.T) {
	_, err := extractJSONObject("just text without braces")
	if err == nil {
		t.Fatal("expected error when no brace found")
	}
}

func TestExtractJSONObject_UnbalancedBraces(t *testing.T) {
	_, err := extractJSONObject(`{"key": "value"`)
	if err == nil {
		t.Fatal("expected error for unbalanced braces")
	}
	if !strings.Contains(err.Error(), "unbalanced") {
		t.Fatalf("expected unbalanced error, got: %v", err)
	}
}

func TestExtractJSONObject_BracesInsideString(t *testing.T) {
	// Braces inside a string should not affect depth tracking
	raw := `{"key": "a{b}c", "nested": {"inner": true}}`
	result, err := extractJSONObject(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"nested"`) {
		t.Fatalf("expected full nested JSON, got %q", result)
	}
}

func TestExtractJSONObject_EscapedQuotesInsideString(t *testing.T) {
	raw := `{"key": "value with \"escaped\" quotes", "other": 1}`
	result, err := extractJSONObject(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"other"`) {
		t.Fatalf("expected both keys, got %q", result)
	}
}

func TestExtractJSONObject_ProseBeforeAndAfter(t *testing.T) {
	raw := `Based on the conversation, I extracted these facts:
{"updates": [{"layer": "global", "filename": "test.md", "facts": ["fact one"]}]}
Let me know if you need more.`
	result, err := extractJSONObject(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"updates"`) {
		t.Fatalf("expected JSON with updates, got %q", result)
	}
}

func TestExtractJSONObject_ProseAfterOnly(t *testing.T) {
	raw := `{"result": "done"} and this is extra text after`
	result, err := extractJSONObject(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != `{"result": "done"}` {
		t.Fatalf("expected exact JSON, got %q", result)
	}
}

func TestExtractJSONObject_WhitespaceOnlyBeforeBrace(t *testing.T) {
	raw := "  \n  \t  {\"key\": \"val\"}"
	result, err := extractJSONObject(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != `{"key": "val"}` {
		t.Fatalf("expected trimmed JSON, got %q", result)
	}
}

func TestExtractJSONObject_FirstValidObject(t *testing.T) {
	// When multiple JSON objects exist, should return the first valid top-level one
	raw := `{"first": true} and then {"second": true}`
	result, err := extractJSONObject(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"first"`) {
		t.Fatalf("expected first JSON object, got %q", result)
	}
	if strings.Contains(result, `"second"`) {
		t.Fatalf("should not include second object, got %q", result)
	}
}
