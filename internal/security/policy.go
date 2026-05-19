package security

import (
	"fmt"
	"path/filepath"
	"strings"
)

// PolicyMode controls how strictly policy rules are enforced.
type PolicyMode string

const (
	// PolicyWarn evaluates all tool calls but only blocks extreme cases.
	// Violations are logged and allowed. Use during Phase 1 rollout.
	PolicyWarn PolicyMode = "warn"

	// PolicyBlock blocks all violations (sensitive paths, destructive commands,
	// exfiltration, outside-cwd writes). Normal build/test/lint remains allowed.
	PolicyBlock PolicyMode = "block"
)

// BashPolicy defines granular rules for bash command evaluation.
type BashPolicy struct {
	AllowBuild     bool `json:"allow_build"`      // go build, npm run, etc.
	AllowGitSafe   bool `json:"allow_git_safe"`   // git status, diff, log
	AllowTest      bool `json:"allow_test"`       // go test, npm test
	Destructive    bool `json:"destructive"`       // rm -rf, sudo, chmod
	AllowEnvAccess bool `json:"allow_env_access"` // env, printenv, $VAR
}

// SecurityConfig holds the complete security policy configuration.
type SecurityConfig struct {
	Mode                  PolicyMode       `json:"mode"`                   // "warn" | "block"
	DefaultProfile        CapabilityProfile `json:"default_profile"`       // default for coding contexts
	AllowPrivilegedAgents bool             `json:"allow_privileged"`      // allow privileged profile
	SensitivePathPatterns []string         `json:"sensitive_paths"`       // glob patterns for sensitive paths
	AllowedOutsideCWDPaths []string        `json:"allowed_outside_cwd"`   // exceptions to cwd boundary
	BashRules             BashPolicy      `json:"bash_rules"`
}

// ToolDecision represents the outcome of a policy evaluation.
type ToolDecision string

const (
	DecisionAllow            ToolDecision = "allow"
	DecisionBlock            ToolDecision = "block"
	DecisionRewrite          ToolDecision = "rewrite"
	DecisionApprovalRequired ToolDecision = "approval_required"
)

// PolicyDecision is the result of evaluating a tool call against the policy.
type PolicyDecision struct {
	Decision ToolDecision              `json:"decision"`
	Reason   string                    `json:"reason"`
	Redacted map[string]any            `json:"redacted,omitempty"`
}

// DefaultConfig returns safe defaults for Phase 2 Block mode.
func DefaultConfig() SecurityConfig {
	return SecurityConfig{
		Mode:           PolicyBlock,
		DefaultProfile: ProfileExecuteSafe,
		SensitivePathPatterns: []string{
			".env",
			".env.*",
			"*.pem",
			"*.key",
			"id_rsa",
			"id_ed25519",
			"config.json",
			"credentials.json",
			"*.credentials",
			"service-account*.json",
			".ssh/*",
			".pi/*",
			".aurelia/config/*",
			".git/config",
		},
		AllowedOutsideCWDPaths: nil,
		BashRules: BashPolicy{
			AllowBuild:     true,
			AllowGitSafe:   true,
			AllowTest:      true,
			Destructive:    false,
			AllowEnvAccess: false,
		},
	}
}

// EvaluateToolCall checks a single tool call against the security policy.
// It returns the decision (allow, block, rewrite) along with a human-readable
// reason. The redacted input map removes sensitive values for audit logging.
func EvaluateToolCall(toolName string, input map[string]any, cwd string, profile CapabilityProfile, cfg SecurityConfig) PolicyDecision {
	// No policy enforcement for observe/read_only tools that aren't in the
	// restricted set — Write, Edit, Bash are already blocked at profile level.
	// But we still guard Read against sensitive paths.

	switch toolName {
	case "Read", "Grep", "Glob", "LS":
		return evaluateReadTool(toolName, input, cwd, profile, cfg)
	case "Write", "Edit":
		return evaluateWriteTool(toolName, input, cwd, profile, cfg)
	case "Bash":
		return evaluateBashTool(toolName, input, cwd, profile, cfg)
	default:
		return PolicyDecision{Decision: DecisionAllow, Reason: "tool not restricted"}
	}
}

// IsSensitivePath checks if a path matches any sensitive pattern.
func IsSensitivePath(path string, patterns []string) bool {
	clean := filepath.Clean(path)
	base := filepath.Base(clean)
	for _, p := range patterns {
		matched, err := filepath.Match(p, base)
		if err == nil && matched {
			return true
		}
		// Also try matching the full relative path.
		if matched, err := filepath.Match(p, clean); err == nil && matched {
			return true
		}
		// Check for path components.
		if strings.Contains(clean, "/"+p) || strings.Contains(clean, "\\"+p) {
			return true
		}
	}
	// Hard-coded sensitive path checks.
	if strings.Contains(clean, "/.ssh/") || strings.HasPrefix(clean, ".ssh/") {
		return true
	}
	if strings.Contains(clean, "/.aurelia/config/") || strings.HasPrefix(clean, ".aurelia/config/") {
		return true
	}
	if strings.Contains(clean, "/.pi/") || strings.HasPrefix(clean, ".pi/") {
		return true
	}
	return false
}

// IsDestructiveCommand checks if a bash command uses destructive patterns.
func IsDestructiveCommand(command string) bool {
	lower := strings.TrimSpace(strings.ToLower(command))

	// rm -rf with absolute paths or recursive.
	if strings.HasPrefix(lower, "rm ") {
		if strings.Contains(lower, " -rf") || strings.Contains(lower, " -fr") {
			// Allow rm -rf relative to cwd for temp files, but block
			// absolute paths, /, and known dangerous targets.
			for _, bad := range []string{"/ ", "/*", "/.", "~", "/etc", "/usr", "/bin", "/lib", "/home", "/root", "/var"} {
				if strings.Contains(lower, bad) {
					return true
				}
			}
		}
		// rm with / as target
		if strings.Contains(lower, "rm /") || strings.Contains(lower, "rm -rf /") {
			return true
		}
	}

	// sudo (unless trivial).
	if strings.HasPrefix(lower, "sudo ") {
		return true
	}

	// chmod -R on system paths.
	if strings.Contains(lower, "chmod") && (strings.Contains(lower, " -r") || strings.Contains(lower, " --recursive")) {
		for _, bad := range []string{"/ ", "/etc", "/usr", "/bin", "/lib"} {
			if strings.Contains(lower, bad) {
				return true
			}
		}
	}

	// chown -R.
	if strings.Contains(lower, "chown") && (strings.Contains(lower, " -r") || strings.Contains(lower, " --recursive")) {
		return true
	}

	// dd (disk destroyer).
	if strings.HasPrefix(lower, "dd ") && strings.Contains(lower, "of=") {
		return true
	}

	// Fork bomb patterns.
	if strings.Contains(lower, ":(){") || strings.Contains(lower, ":()") {
		return true
	}

	// mkfs, fdisk, parted — dangerous disk operations.
	if strings.HasPrefix(lower, "mkfs") || strings.HasPrefix(lower, "fdisk") || strings.HasPrefix(lower, "parted") {
		return true
	}

	return false
}

// IsExfiltrationCommand checks if a command pattern suggests data exfiltration.
func IsExfiltrationCommand(command string, _ map[string]any) bool {
	lower := strings.TrimSpace(strings.ToLower(command))

	// Check for combination of network tool + local file reading.
	hasNetworkTool := false
	for _, tool := range []string{"curl ", "wget ", "nc ", "ncat ", "scp ", "rsync "} {
		if strings.Contains(lower, tool) {
			hasNetworkTool = true
			break
		}
	}

	if !hasNetworkTool {
		return false
	}

	// Check for exfiltration patterns: sending file contents, env, or stdin.
	suspiciousPatterns := []string{
		"$(cat ", "`cat ", "`env`", "$(env)",
		"< /", "<~", ".env", "id_rsa", "token", "secret", "password",
		"-d @", " --data @", "--data-raw ", "--data-binary ",
		"-F ", "--form ", "file=@",
		"| nc ", "| ncat ",
		"< /dev/stdin", "<&",
	}

	for _, pat := range suspiciousPatterns {
		if strings.Contains(lower, pat) {
			return true
		}
	}

	return false
}

// evaluateReadTool checks Read/Grep/Glob/LS against file system policy.
func evaluateReadTool(toolName string, input map[string]any, cwd string, profile CapabilityProfile, cfg SecurityConfig) PolicyDecision {
	if profile == ProfileObserve {
		return PolicyDecision{Decision: DecisionBlock, Reason: fmt.Sprintf("profile %s does not allow %s", profile, toolName)}
	}

	path, _ := input["path"].(string)
	if path == "" {
		return PolicyDecision{Decision: DecisionAllow, Reason: "no path to evaluate"}
	}

	// Check sensitive paths.
	if IsSensitivePath(path, cfg.SensitivePathPatterns) {
		reason := fmt.Sprintf("access to sensitive path blocked: %s", path)
		if cfg.Mode == PolicyWarn {
			return PolicyDecision{Decision: DecisionAllow, Reason: "[WARN] " + reason}
		}
		return PolicyDecision{Decision: DecisionBlock, Reason: reason}
	}

	// Check cwd boundary for tools that read file contents.
	if toolName == "Read" && cwd != "" && !isPathAllowed(path, cwd, cfg.AllowedOutsideCWDPaths) {
		reason := fmt.Sprintf("path outside working directory: %s", path)
		if cfg.Mode == PolicyWarn {
			return PolicyDecision{Decision: DecisionAllow, Reason: "[WARN] " + reason}
		}
		return PolicyDecision{Decision: DecisionBlock, Reason: reason}
	}

	return PolicyDecision{Decision: DecisionAllow, Reason: "path allowed"}
}

// evaluateWriteTool checks Write/Edit against file system policy.
func evaluateWriteTool(toolName string, input map[string]any, cwd string, _ CapabilityProfile, cfg SecurityConfig) PolicyDecision {
	path, _ := input["path"].(string)
	if path == "" {
		return PolicyDecision{Decision: DecisionAllow, Reason: "no path to evaluate"}
	}

	// Check sensitive paths.
	if IsSensitivePath(path, cfg.SensitivePathPatterns) {
		return PolicyDecision{Decision: DecisionBlock, Reason: fmt.Sprintf("write to sensitive path blocked: %s", path)}
	}

	// Check cwd boundary.
	if cwd != "" && !isPathAllowed(path, cwd, cfg.AllowedOutsideCWDPaths) {
		reason := fmt.Sprintf("write to path outside working directory: %s", path)
		if cfg.Mode == PolicyWarn {
			return PolicyDecision{Decision: DecisionAllow, Reason: "[WARN] " + reason}
		}
		return PolicyDecision{Decision: DecisionBlock, Reason: reason}
	}

	return PolicyDecision{Decision: DecisionAllow, Reason: "path allowed"}
}

// evaluateBashTool checks bash commands against the security policy.
func evaluateBashTool(toolName string, input map[string]any, cwd string, _ CapabilityProfile, cfg SecurityConfig) PolicyDecision {
	command, _ := input["command"].(string)
	if command == "" {
		return PolicyDecision{Decision: DecisionAllow, Reason: "no command to evaluate"}
	}

	lower := strings.TrimSpace(strings.ToLower(command))

	// Build and test commands are always allowed (if configured).
	if cfg.BashRules.AllowBuild {
		if matchesBuildPattern(lower) {
			return PolicyDecision{Decision: DecisionAllow, Reason: "build command allowed"}
		}
	}
	if cfg.BashRules.AllowTest {
		if matchesTestPattern(lower) {
			return PolicyDecision{Decision: DecisionAllow, Reason: "test command allowed"}
		}
	}
	if cfg.BashRules.AllowGitSafe {
		if matchesSafeGitPattern(lower) {
			return PolicyDecision{Decision: DecisionAllow, Reason: "safe git command allowed"}
		}
	}

	// Check destructive commands.
	if !cfg.BashRules.Destructive && IsDestructiveCommand(command) {
		reason := fmt.Sprintf("destructive command blocked: %s", truncate(command, 80))
		if cfg.Mode == PolicyWarn {
			return PolicyDecision{Decision: DecisionAllow, Reason: "[WARN] " + reason}
		}
		return PolicyDecision{Decision: DecisionBlock, Reason: reason}
	}

	// Check env access.
	if !cfg.BashRules.AllowEnvAccess {
		if matchesEnvAccess(lower) {
			reason := "environment access blocked: command reads env vars or secrets"
			if cfg.Mode == PolicyWarn {
				return PolicyDecision{Decision: DecisionAllow, Reason: "[WARN] " + reason}
			}
			return PolicyDecision{Decision: DecisionBlock, Reason: reason}
		}
	}

	// Check exfiltration.
	if IsExfiltrationCommand(command, input) {
		reason := fmt.Sprintf("exfiltration blocked: %s", truncate(command, 80))
		if cfg.Mode == PolicyWarn {
			return PolicyDecision{Decision: DecisionAllow, Reason: "[WARN] " + reason}
		}
		return PolicyDecision{Decision: DecisionBlock, Reason: reason}
	}

	// Git push --force / dangerous git operations.
	if isDangerousGit(lower) {
		reason := "dangerous git operation blocked"
		if cfg.Mode == PolicyWarn {
			return PolicyDecision{Decision: DecisionAllow, Reason: "[WARN] " + reason}
		}
		return PolicyDecision{Decision: DecisionBlock, Reason: reason}
	}

	return PolicyDecision{Decision: DecisionAllow, Reason: "command allowed"}
}

// isPathAllowed checks if the given path is within cwd or in the allowlist.
func isPathAllowed(path, cwd string, allowedOutsideCWDPaths []string) bool {
	if path == "" || cwd == "" {
		return true
	}

	cleaned := filepath.Clean(path)

	// Relative path — resolve against an imaginary cwd.
	// If it starts with ../ it's outside.
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || cleaned == "." {
		// "." and ".." without cwd context — outside.
		return false
	}

	// Absolute path: must be within cwd.
	if filepath.IsAbs(cleaned) {
		if strings.HasPrefix(cleaned, cwd) {
			rel, err := filepath.Rel(cwd, cleaned)
			if err == nil && !strings.HasPrefix(rel, "..") {
				return true
			}
		}
		// Check allowlist.
		for _, allowed := range allowedOutsideCWDPaths {
			if strings.HasPrefix(cleaned, allowed) {
				return true
			}
		}
		return false
	}

	// Relative path — assume it's relative to cwd, so it's allowed.
	return true
}

// matchesBuildPattern checks if a command looks like a build command.
func matchesBuildPattern(cmd string) bool {
	prefixes := []string{
		"go build", "go install", "go mod",
		"npm run build", "npm run prod", "npm run compile",
		"npx tsc", "npx esbuild", "npx webpack",
		"make", "make build", "make compile",
		"cargo build", "cargo check",
		"dotnet build", "dotnet publish",
		"gradle build", "mvn compile", "mvn package",
		"bun run build", "yarn build", "yarn run build",
		"tsc ", "tsc --",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(cmd, p) {
			return true
		}
	}
	return false
}

// matchesTestPattern checks if a command looks like a test command.
func matchesTestPattern(cmd string) bool {
	prefixes := []string{
		"go test", "go vet", "go fmt",
		"npm test", "npm run test", "npx jest", "npx mocha", "npx vitest",
		"yarn test", "yarn run test",
		"bun test",
		"cargo test",
		"dotnet test",
		"gradle test",
		"mvn test",
		"pytest", "python -m pytest",
		"rspec", "bundle exec rspec",
		"rails test",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(cmd, p) {
			return true
		}
	}
	return false
}

// matchesSafeGitPattern checks for safe read-only git commands.
func matchesSafeGitPattern(cmd string) bool {
	safePrefixes := []string{
		"git status", "git diff", "git log", "git show",
		"git branch", "git checkout", "git stash list",
		"git describe", "git rev-parse", "git rev-list",
		"git config", "git ls-files", "git ls-tree",
		"git tag", "git blame", "git shortlog",
		"git cherry", "git cherry-pick --abort",
	}
	for _, p := range safePrefixes {
		if strings.HasPrefix(cmd, p) {
			return true
		}
	}

	// git diff with specific file.
	if strings.HasPrefix(cmd, "git diff") {
		return true
	}

	return false
}

// matchesEnvAccess detects commands that read environment variables or secrets.
func matchesEnvAccess(cmd string) bool {
	// Direct env commands.
	if strings.HasPrefix(cmd, "env") || strings.HasPrefix(cmd, "printenv") || strings.HasPrefix(cmd, "export") {
		return true
	}

	// Accessing .aurelia config.
	if strings.Contains(cmd, ".aurelia/config") || strings.Contains(cmd, "aurelia/config") {
		return true
	}

	// Accessing env vars via echo or print.
	if strings.Contains(cmd, "echo $") || strings.Contains(cmd, "echo ${") || strings.Contains(cmd, "print $") {
		return true
	}

	// cat of config files.
	if strings.Contains(cmd, "cat ~/.aurelia") || strings.Contains(cmd, "cat ${") {
		return true
	}

	return false
}

// isDangerousGit detects dangerous git operations.
func isDangerousGit(cmd string) bool {
	if !strings.HasPrefix(cmd, "git ") {
		return false
	}

	dangerous := []string{
		"git push --force", "git push -f",
		"git remote add", "git remote set-url",
		"git reset --hard", "git clean -f",
		"git reflog delete", "git update-ref -d",
		"git credential", "git gc",
	}
	for _, d := range dangerous {
		if strings.Contains(cmd, d) {
			return true
		}
	}
	return false
}

// truncate truncates a string to maxLen, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
