package gitworkspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testPaths(t *testing.T) Paths {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	paths, err := NewPaths(Repository{Root: repo, GitCommonDir: filepath.Join(repo, ".git")})
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	return paths
}

func TestStoreRoundTrip(t *testing.T) {
	paths := testPaths(t)
	store := Store{Paths: paths}
	now := time.Now().UTC().Truncate(time.Second)
	workspace := Workspace{ID: "demo", Name: "Demo", Type: WorkspaceDev, Source: SourceSpec{Kind: "branch", Ref: "gws/demo", BaseRef: "main", BaseCommit: "abc123"}, WorktreePath: paths.WorktreePath("demo"), TaskFilePath: paths.TaskFile("demo"), State: StateReady, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveWorkspace(workspace); err != nil {
		t.Fatal(err)
	}
	got, err := store.Workspace("demo")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != workspace.ID || got.Source.BaseCommit != "abc123" || !got.CreatedAt.Equal(now) {
		t.Fatalf("round trip mismatch: %#v", got)
	}
	if got.StateVersion != CurrentStateVersion {
		t.Fatalf("state version=%d", got.StateVersion)
	}
	info, err := os.Stat(paths.WorkspaceFile("demo"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
}

func TestStoreAcceptsLegacyStateAndRejectsFutureState(t *testing.T) {
	paths := testPaths(t)
	store := Store{Paths: paths}
	legacy := []byte(`{"id":"legacy","name":"Legacy"}`)
	if err := os.WriteFile(paths.WorkspaceFile("legacy"), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	workspace, err := store.Workspace("legacy")
	if err != nil || workspace.StateVersion != CurrentStateVersion {
		t.Fatalf("legacy state was not upgraded in memory: workspace=%#v err=%v", workspace, err)
	}

	future := []byte(`{"state_version":2,"id":"future","name":"Future"}`)
	if err := os.WriteFile(paths.WorkspaceFile("future"), future, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Workspace("future"); ErrorCode(err) != "state_version_unsupported" {
		t.Fatalf("future state error=%v", err)
	}
}

func TestGuardWorktreePath(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	paths, err := NewPaths(Repository{Root: repo, GitCommonDir: filepath.Join(repo, ".git")})
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	app := &App{Paths: paths, Repo: Repository{Root: repo, GitCommonDir: filepath.Join(repo, ".git")}}
	workspace := Workspace{ID: "w", WorktreePath: paths.WorktreePath("w")}
	if err := os.MkdirAll(workspace.WorktreePath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := app.guardWorktreePath(workspace); err != nil {
		t.Fatalf("safe path rejected: %v", err)
	}
	workspace.WorktreePath = repo
	if err := app.guardWorktreePath(workspace); ErrorCode(err) != "workspace_path_unsafe" {
		t.Fatalf("repo root error=%v", err)
	}
	workspace.WorktreePath = filepath.Dir(repo)
	if err := app.guardWorktreePath(workspace); ErrorCode(err) != "workspace_path_unsafe" {
		t.Fatalf("parent dir error=%v", err)
	}
	workspace.WorktreePath = paths.WorktreesDir
	if err := app.guardWorktreePath(workspace); ErrorCode(err) != "workspace_path_unsafe" {
		t.Fatalf("worktrees root error=%v", err)
	}
}
