package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNew_UsesAURELIA_HOME(t *testing.T) {
	want := t.TempDir()
	t.Setenv("AURELIA_HOME", want)

	r, err := New()
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}
	if r.Root() != want {
		t.Errorf("Root() = %q, want %q", r.Root(), want)
	}
}

func TestNew_DefaultsToUserHome(t *testing.T) {
	t.Setenv("AURELIA_HOME", "") // ensure env var is not set

	r, err := New()
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}

	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".aurelia")
	if r.Root() != want {
		t.Errorf("Root() = %q, want %q", r.Root(), want)
	}
}

func TestPathResolver_Root(t *testing.T) {
	base := t.TempDir()
	r := &PathResolver{root: base}

	if r.Root() != base {
		t.Errorf("Root() = %q, want %q", r.Root(), base)
	}
}

func TestPathResolver_Accessors(t *testing.T) {
	base := "/tmp/testinstance"
	r := &PathResolver{root: base}

	cases := []struct {
		name string
		got  string
		want string
	}{
		{"Config", r.Config(), filepath.Join(base, "config")},
		{"Data", r.Data(), filepath.Join(base, "data")},
		{"Memory", r.Memory(), filepath.Join(base, "memory")},
		{"MemoryPersonas", r.MemoryPersonas(), filepath.Join(base, "memory", "personas")},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestProjectHelpers(t *testing.T) {
	base := t.TempDir()

	if got := ProjectRoot(base); got != filepath.Join(base, ".aurelia") {
		t.Fatalf("ProjectRoot() = %q", got)
	}
	if got := ProjectSkills(base); got != filepath.Join(base, ".aurelia", "skills") {
		t.Fatalf("ProjectSkills() = %q", got)
	}
}

func TestSanitizeCwd(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"/home/user/code", "-home-user-code"},
		{"/home/user/code/", "-home-user-code"},
		{"/home/user/code", "-home-user-code"},
		{"/", "-"},
		{"", ""},
	}
	for _, c := range cases {
		if got := SanitizeCwd(c.input); got != c.want {
			t.Errorf("SanitizeCwd(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestResolveProjectCwd_CleansEquivalentPaths(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveProjectCwd(filepath.Join(dir, "."))
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(want) {
		t.Fatalf("ResolveProjectCwd() = %q, want %q", got, want)
	}
}

func TestResolveProjectCwd_RejectsNonProjectDirectory(t *testing.T) {
	if _, err := ResolveProjectCwd(t.TempDir()); err == nil {
		t.Fatal("expected non-project directory to be rejected")
	}
}

func TestResolveProjectCwd_RejectsHomeDirectory(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("home directory unavailable")
	}
	if _, err := ResolveProjectCwd(home); err == nil {
		t.Fatal("expected home directory to be rejected as project cwd")
	}
}

func TestProjectMemoryDirs(t *testing.T) {
	r := &PathResolver{root: "/tmp/aurelia"}
	cwd := "/home/user/myproject"

	gotPrivate := r.ProjectMemoryDir(cwd)
	wantPrivate := filepath.Join("/tmp/aurelia", "projects", "-home-user-myproject", "memory")
	if gotPrivate != wantPrivate {
		t.Errorf("ProjectMemoryDir() = %q, want %q", gotPrivate, wantPrivate)
	}

	gotTeam := r.ProjectTeamMemoryDir(cwd)
	wantTeam := filepath.Join(wantPrivate, "team")
	if gotTeam != wantTeam {
		t.Errorf("ProjectTeamMemoryDir() = %q, want %q", gotTeam, wantTeam)
	}

	gotConversation := r.ConversationProjectMemoryDir(cwd, 42, 99)
	wantConversation := filepath.Join(wantPrivate, "conversations", "chat_42", "thread_99")
	if gotConversation != wantConversation {
		t.Errorf("ConversationProjectMemoryDir() = %q, want %q", gotConversation, wantConversation)
	}
}
