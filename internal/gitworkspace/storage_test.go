package gitworkspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreRoundTrip(t *testing.T) {
	paths := Paths{Home: t.TempDir()}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	store := Store{Paths: paths}
	now := time.Now().UTC().Truncate(time.Second)
	project := Project{ID: "demo", Name: "Demo", RepoPath: "/repo", GitCommonDir: "/repo/.git", DefaultBranch: "main", ConfigPath: "/repo/.git-workspace.yaml", CreatedAt: now, UpdatedAt: now}
	if err := store.SaveProject(project); err != nil {
		t.Fatal(err)
	}
	got, err := store.Project("demo")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != project.ID || !got.CreatedAt.Equal(now) {
		t.Fatalf("round trip mismatch: %#v", got)
	}
	if got.StateVersion != CurrentStateVersion {
		t.Fatalf("state version=%d", got.StateVersion)
	}
	info, err := os.Stat(paths.ProjectFile("demo"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
}

func TestStoreAcceptsLegacyStateAndRejectsFutureState(t *testing.T) {
	paths := Paths{Home: t.TempDir()}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	store := Store{Paths: paths}
	legacy := []byte(`{"id":"legacy","name":"Legacy"}`)
	if err := os.WriteFile(paths.ProjectFile("legacy"), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	project, err := store.Project("legacy")
	if err != nil || project.StateVersion != CurrentStateVersion {
		t.Fatalf("legacy state was not upgraded in memory: project=%#v err=%v", project, err)
	}

	future := []byte(`{"state_version":2,"id":"future","name":"Future"}`)
	if err := os.WriteFile(paths.ProjectFile("future"), future, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Project("future"); ErrorCode(err) != "state_version_unsupported" {
		t.Fatalf("future state error=%v", err)
	}
	if err := os.WriteFile(paths.WorkspaceFile("future"), future, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Workspace("future"); ErrorCode(err) != "state_version_unsupported" {
		t.Fatalf("future Workspace state error=%v", err)
	}
}

func TestGuardWorktreePath(t *testing.T) {
	paths := Paths{Home: t.TempDir()}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	project := Project{ID: "p", RepoPath: filepath.Join(paths.Home, "repo")}
	if err := os.MkdirAll(project.RepoPath, 0o700); err != nil {
		t.Fatal(err)
	}
	workspace := Workspace{ID: "w", ProjectID: "p", WorktreePath: paths.WorktreePath("p", "w")}
	if err := os.MkdirAll(workspace.WorktreePath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := guardWorktreePath(paths, project, workspace); err != nil {
		t.Fatalf("safe path rejected: %v", err)
	}
	workspace.WorktreePath = project.RepoPath
	if err := guardWorktreePath(paths, project, workspace); ErrorCode(err) != "workspace_path_unsafe" {
		t.Fatalf("unsafe path error=%v", err)
	}
}
