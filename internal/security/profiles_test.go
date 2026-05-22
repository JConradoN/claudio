package security

import (
	"reflect"
	"testing"
)

func TestProfilePrivilegedDiffersFromExecuteSafe(t *testing.T) {
	executeSafeTools := ProfileTools(ProfileExecuteSafe)
	privilegedTools := ProfileTools(ProfilePrivileged)
	if reflect.DeepEqual(executeSafeTools, privilegedTools) {
		t.Fatalf("ProfilePrivileged tools must differ from ProfileExecuteSafe: %v", privilegedTools)
	}
	if !containsTool(privilegedTools, "List") {
		t.Fatalf("ProfilePrivileged tools = %v, want List alias", privilegedTools)
	}
}

func containsTool(tools []string, want string) bool {
	for _, tool := range tools {
		if tool == want {
			return true
		}
	}
	return false
}
