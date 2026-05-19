package orchestrator

import (
	"strings"
)

const planMarkerStart = "```aurelia-plan"
const planMarkerEnd = "```"

// ContainsPlanMarker reports whether the response appears to contain an
// Aurelia execution plan block, even if the block is malformed or incomplete.
func ContainsPlanMarker(response string) bool {
	return strings.Contains(response, planMarkerStart)
}

// ExtractPlan detects and parses an execution plan from Aurelia's response.
// The plan is expected inside a ```aurelia-plan ... ``` code block.
// Returns nil if no plan marker is found. Returns error if JSON is malformed.
func (o *Orchestrator) ExtractPlan(response string) (*Plan, error) {
	return ExtractPlanFromText(response)
}

// ExtractPlanFromText is the standalone version for testing without an Orchestrator.
func ExtractPlanFromText(response string) (*Plan, error) {
	startIdx := strings.Index(response, planMarkerStart)
	if startIdx == -1 {
		return nil, nil
	}

	// Move past the marker and any trailing newline
	jsonStart := startIdx + len(planMarkerStart)
	if jsonStart < len(response) && response[jsonStart] == '\n' {
		jsonStart++
	}

	// Find closing ```
	endIdx := strings.Index(response[jsonStart:], planMarkerEnd)
	if endIdx == -1 {
		return nil, nil
	}

	jsonStr := strings.TrimSpace(response[jsonStart : jsonStart+endIdx])
	if jsonStr == "" {
		return nil, nil
	}

	return ParsePlan([]byte(jsonStr))
}

// StripPlanBlock removes the ```aurelia-plan ... ``` block from the response,
// returning the remaining text for display in Telegram.
func StripPlanBlock(response string) string {
	startIdx := strings.Index(response, planMarkerStart)
	if startIdx == -1 {
		return response
	}

	// Find closing ```
	afterMarker := startIdx + len(planMarkerStart)
	endIdx := strings.Index(response[afterMarker:], planMarkerEnd)
	if endIdx == -1 {
		return strings.TrimSpace(response[:startIdx])
	}

	// Remove the entire block (marker + content + closing)
	blockEnd := afterMarker + endIdx + len(planMarkerEnd)
	before := strings.TrimRight(response[:startIdx], "\n")
	after := strings.TrimLeft(response[blockEnd:], "\n")
	if before != "" && after != "" {
		return before + "\n\n" + after
	}
	return strings.TrimSpace(before + after)
}
