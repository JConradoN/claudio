// Package security implements capability-based security guard-rails for the
// Aurelia agent runtime. It provides CapabilityProfiles that define the set of
// tools available in a given execution context, a policy engine that validates
// individual tool calls against configurable rules, and structured audit logging.
//
// Architecture
//
//	Agent/Context → CapabilityProfile → RequestOptions.tools
//	                                           ↓
//	                                 ┌─────────────────┐
//	                                 │  PI tool_call    │ ← Security Hook (TS)
//	                                 │  hook no bridge  │
//	                                 └────────┬────────┘
//	                                          ↓
//	                                 ┌─────────────────┐
//	                                 │  evaluateTool    │ ← Policy Engine (TS)
//	                                 │  Policy()        │
//	                                 └────────┬────────┘
//	                               allow / block / rewrite
//	                                          ↓
//	                                 ┌─────────────────┐
//	                                 │  Audit Log      │
//	                                 └─────────────────┘
package security

// CapabilityProfile represents the level of tool access an execution context
// is granted. Each profile maps to a definitive allowlist of tools that may
// be used in that context.
type CapabilityProfile string

const (
	// ProfileObserve grants no tools. Used for classification, routing, and
	// generation that does not interact with the filesystem or shell.
	ProfileObserve CapabilityProfile = "observe"

	// ProfileReadOnly grants read-only filesystem tools: Read, Grep, Glob, LS.
	// Used for reviewers, validators, and discovery.
	ProfileReadOnly CapabilityProfile = "read_only"

	// ProfileEditProject adds Write and Edit to the read-only set, enabling
	// materialisation of specs, designs, and documentation. No Bash.
	ProfileEditProject CapabilityProfile = "edit_project"

	// ProfileExecuteSafe adds governed Bash to the edit set, enabling normal
	// coding workflows (build, test, lint) while blocking destructive or
	// exfiltrative commands via the policy hook.
	ProfileExecuteSafe CapabilityProfile = "execute_safe"

	// ProfilePrivileged grants wide tool access including ungoverned Bash.
	// Requires explicit opt-in via configuration.
	ProfilePrivileged CapabilityProfile = "privileged"
)

// ProfileTools returns the definitive tool allowlist for a profile.
// Tools not in this list are always blocked regardless of agent overrides.
// This is the SOURCE OF TRUTH for what tools each profile may use.
func ProfileTools(p CapabilityProfile) []string {
	switch p {
	case ProfileObserve:
		return nil
	case ProfileReadOnly:
		return []string{"Read", "Grep", "Glob", "LS"}
	case ProfileEditProject:
		return []string{"Read", "Grep", "Glob", "LS", "Write", "Edit"}
	case ProfileExecuteSafe:
		return []string{"Read", "Write", "Edit", "Bash", "Grep", "Glob", "LS",
			"WebSearch", "WebSearchPremium", "WebFetch"}
	case ProfilePrivileged:
		return []string{"Read", "Write", "Edit", "Bash", "Grep", "Glob", "LS",
			"WebSearch", "WebSearchPremium", "WebFetch"}
	default:
		return nil
	}
}

// ResolveProfile intersects the agent-level tool configuration with profile
// limits and returns the effective profile and tool list.
//
// Parameters:
//   - profile: the base profile for the execution context
//   - agentAllowed: tools explicitly allowed by the agent definition (may be nil)
//   - agentDisallowed: tools explicitly disallowed by the agent definition
//   - hasCWD: whether a working directory is set
//
// The effective tool list is:
//  1. Start with ProfileTools(profile)
//  2. If agentAllowed is non-empty, intersect with it (only tools in both)
//  3. Remove any tools in agentDisallowed
func ResolveProfile(profile CapabilityProfile, agentAllowed, agentDisallowed []string, hasCWD bool) (CapabilityProfile, []string) {
	tools := ProfileTools(profile)
	if tools == nil {
		return profile, nil
	}

	// Intersect with agent-level allowed_tools if set.
	if len(agentAllowed) > 0 {
		allowedSet := make(map[string]bool, len(agentAllowed))
		for _, t := range agentAllowed {
			allowedSet[t] = true
		}
		intersected := make([]string, 0, len(tools))
		for _, t := range tools {
			if allowedSet[t] {
				intersected = append(intersected, t)
			}
		}
		tools = intersected
	}

	// Remove agent-level disallowed_tools.
	if len(agentDisallowed) > 0 {
		deniedSet := make(map[string]bool, len(agentDisallowed))
		for _, t := range agentDisallowed {
			deniedSet[t] = true
		}
		filtered := make([]string, 0, len(tools))
		for _, t := range tools {
			if !deniedSet[t] {
				filtered = append(filtered, t)
			}
		}
		tools = filtered
	}

	// Downgrade profile if no cwd is set and a write/bash tool is present.
	if !hasCWD {
		for _, t := range tools {
			if t == "Write" || t == "Edit" || t == "Bash" {
				downgraded := ProfileReadOnly
				return downgraded, ProfileTools(downgraded)
			}
		}
	}

	return profile, tools
}

// DefaultProfileForContext returns the recommended capability profile for a
// given execution context.
//
//   - Without a working directory: read_only (no write/bash tools).
//   - Internal system processes (dream, nudge, validator): edit_project.
//   - Normal coding with cwd and write needed: execute_safe.
//   - Normal coding with cwd, read-only: read_only.
func DefaultProfileForContext(hasCWD bool, isInternal bool, needsWrite bool) CapabilityProfile {
	if isInternal {
		return ProfileEditProject
	}
	if !hasCWD {
		return ProfileReadOnly
	}
	if needsWrite {
		return ProfileExecuteSafe
	}
	return ProfileReadOnly
}

// ValidateProfile returns an error if the profile name is unknown.
func ValidateProfile(p CapabilityProfile) error {
	switch p {
	case ProfileObserve, ProfileReadOnly, ProfileEditProject, ProfileExecuteSafe, ProfilePrivileged:
		return nil
	default:
		return &ErrUnknownProfile{Profile: p}
	}
}

// ErrUnknownProfile is returned when an agent declares an unknown profile.
type ErrUnknownProfile struct {
	Profile CapabilityProfile
}

func (e *ErrUnknownProfile) Error() string {
	return "unknown capability profile: " + string(e.Profile)
}
