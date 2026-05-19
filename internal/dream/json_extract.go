package dream

import (
	"fmt"
	"strings"
)

// extractJSONObject extracts the first top-level JSON object from raw text.
// Accepts:
//   - direct JSON object: {"key": "val"}
//   - fenced JSON: ```json\n{"key": "val"}\n```
//   - generic fence: ```\n{"key": "val"}\n```
//   - extra text before/after a single JSON object: prose {"key": "val"} more
//
// Rejects empty output and unbalanced braces. Returns the exact JSON substring
// on success, or an error describing why extraction failed.
func extractJSONObject(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("empty output")
	}

	// Find first '{' — skip any leading text (prose, fences, etc.)
	braceStart := strings.IndexByte(trimmed, '{')
	if braceStart < 0 {
		return "", fmt.Errorf("no JSON object found")
	}

	sub := trimmed[braceStart:]

	// Walk rune-by-rune tracking brace depth, respecting strings and escapes.
	depth := 0
	inString := false
	escaped := false

	for i := 0; i < len(sub); i++ {
		ch := sub[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && inString {
			escaped = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return sub[:i+1], nil
			}
		}
	}

	return "", fmt.Errorf("unbalanced braces: depth=%d", depth)
}
