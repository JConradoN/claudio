package pipeline

import (
	"fmt"
	"strings"
)

// codebaseReadKeywords are phrases that suggest the user wants to read or
// analyze a project/codebase — requiring file tools and a working directory.
var codebaseReadKeywords = []string{
	// Portuguese
	"leia a code base",
	"leia o código",
	"leia o codigo",
	"leia a base de código",
	"leia a base de codigo",
	"leia o projeto",
	"leia o repositório",
	"leia o repositorio",
	"analise o código",
	"analise o codigo",
	"analisa o código",
	"analisa o codigo",
	"analise o projeto",
	"analisa o projeto",
	"analise o repositório",
	"analise o repositorio",
	"veja o código",
	"veja o codigo",
	"veja o projeto",
	// English
	"read the codebase",
	"read the code base",
	"read the code",
	"read the project",
	"read the repo",
	"read the repository",
	"analyze the codebase",
	"analyze the code base",
	"analyze the code",
	"analyze the project",
	"analyze the repo",
	"analyze the repository",
	"analyse the codebase",
	"analyse the code",
	"analyse the project",
	"go through the code",
	"browse the code",
	"browse the codebase",
	"browse the project",
}

// looksLikeCodebaseRead returns true when the user message suggests they want
// to read, analyze, or browse a codebase/project/repository — which requires
// file tools and a working directory.
func looksLikeCodebaseRead(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	for _, kw := range codebaseReadKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// codebaseReadChatModeGuidanceForKnownProjects returns a system prompt section
// injected when the user asks to read/analyze code but no cwd is set for this
// chat. It tells the model to guide the user to /cwd. When knownPaths is
// non-empty, it lists the user's known project bindings from other chats.
func codebaseReadChatModeGuidanceForKnownProjects(knownPaths []string) string {
	if len(knownPaths) == 0 {
		return `## Codebase / Project Analysis Request

The user is asking you to read or analyze a codebase/project, but no working directory (cwd) is set. You are in chat mode and CANNOT access files.

When this happens:
1. Tell the user they need to set a working directory first using the command: /cwd <path>
2. Explain that file operations (Read, Write, Edit, Bash, Glob, Grep) are only available after a cwd is configured.
3. Do NOT try to read files or run commands — you have no file access without a cwd.
4. If the user mentions a specific project name (like "aurelia") and you know its path, suggest it: e.g. "/cwd /home/user/projects/aurelia"`
	}

	var b strings.Builder
	b.WriteString(`## Codebase / Project Analysis Request

The user is asking you to read or analyze a codebase/project, but no working directory (cwd) is set for this chat. You are in chat mode and CANNOT access files.

When this happens:
1. Tell the user to set a working directory: /cwd <path>
2. File operations are only available after a cwd is configured.
3. Do NOT try to read files or run commands — you have no file access without a cwd.
4. The user has previously bound projects in other chats. Suggest the most relevant one:`)
	for _, p := range knownPaths {
		fmt.Fprintf(&b, "\n   `/cwd %s`", p)
	}
	b.WriteString("\n5. If none of these match, ask what project the user wants to work on.")
	return b.String()
}
