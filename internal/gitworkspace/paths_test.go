package gitworkspace

import (
	"path/filepath"
	"testing"
)

func TestNewPathsLayout(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "demo")
	common := filepath.Join(repo, ".git")
	paths, err := NewPaths(Repository{Root: repo, GitCommonDir: common})
	if err != nil {
		t.Fatal(err)
	}
	if paths.StateDir != filepath.Join(common, "gws") {
		t.Fatalf("state dir=%s", paths.StateDir)
	}
	if paths.WorktreesDir != filepath.Join(common, "gws", "worktrees") {
		t.Fatalf("worktrees dir=%s", paths.WorktreesDir)
	}
	if got := paths.WorktreePath("abc"); got != filepath.Join(paths.WorktreesDir, "abc") {
		t.Fatalf("worktree path=%s", got)
	}
	if got := paths.WorkspaceFile("abc"); got != filepath.Join(common, "gws", "workspaces", "abc.json") {
		t.Fatalf("workspace file=%s", got)
	}
	if got := paths.TaskFile("abc"); got != filepath.Join(common, "gws", "tasks", "abc.md") {
		t.Fatalf("task file=%s", got)
	}
	if got := paths.LockFile(); got != filepath.Join(common, "gws", "locks", "state.lock") {
		t.Fatalf("lock file=%s", got)
	}
}

func TestNewPathsEnsureCreatesStateDirs(t *testing.T) {
	repo := t.TempDir()
	paths, err := NewPaths(Repository{Root: repo, GitCommonDir: filepath.Join(repo, ".git")})
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{paths.StateDir, paths.WorkspacesDir(), paths.TasksDir(), paths.LocksDir()} {
		if !pathIsDir(dir) {
			t.Fatalf("missing directory %s", dir)
		}
	}
	if pathIsDir(paths.WorktreesDir) {
		t.Fatalf("worktrees root should not be created eagerly: %s", paths.WorktreesDir)
	}
}
