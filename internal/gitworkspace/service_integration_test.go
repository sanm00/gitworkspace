package gitworkspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitCommand(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", full...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func testRepository(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "-b", "main", repo)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	gitCommand(t, repo, "config", "user.email", "test@example.com")
	gitCommand(t, repo, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("demo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, repo, "add", "README.md")
	gitCommand(t, repo, "commit", "-m", "initial")
	gitCommand(t, repo, "branch", "feature/a")
	gitCommand(t, repo, "branch", "feature/b")
	return repo
}

func newTestApp(t *testing.T, repo string) *App {
	t.Helper()
	git := Git{Runner: Runner{}}
	detected, err := git.DetectRepository(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := NewPaths(detected)
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	return NewApp(paths, detected)
}

func TestWorkspaceLifecycle(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app := newTestApp(t, repo)

	review, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceReview, Base: "feature/b"})
	if err != nil {
		t.Fatal(err)
	}
	if review.StateVersion != CurrentStateVersion {
		t.Fatalf("workspace state version=%d", review.StateVersion)
	}
	if _, err := exec.Command("git", "-C", review.WorktreePath, "symbolic-ref", "--quiet", "HEAD").CombinedOutput(); err == nil {
		t.Fatal("review worktree is attached")
	}
	if review.Source.BaseRef != "feature/b" || review.Source.BaseCommit == "" {
		t.Fatalf("review base was not recorded: %#v", review.Source)
	}
	if got := strings.TrimSpace(gitCommand(t, review.WorktreePath, "rev-parse", "HEAD")); got != review.Source.BaseCommit {
		t.Fatalf("review worktree HEAD=%q base commit=%q", got, review.Source.BaseCommit)
	}

	dev, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Base: "feature/a"})
	if err != nil {
		t.Fatal(err)
	}
	if !dev.Source.Managed || !strings.HasPrefix(dev.Source.Ref, "gws/") {
		t.Fatalf("dev source=%#v", dev.Source)
	}
	if got := gitCommand(t, repo, "branch", "--show-current"); got != "main\n" {
		t.Fatalf("original branch changed: %q", got)
	}
	if err := os.WriteFile(filepath.Join(dev.WorktreePath, "dirty.txt"), []byte("dirty"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.RemoveWorkspace(ctx, dev.ID, repo, false); ErrorCode(err) != "workspace_dirty" {
		t.Fatalf("expected dirty error, got %v", err)
	}
	if err := app.RemoveWorkspace(ctx, dev.ID, repo, true); err != nil {
		t.Fatal(err)
	}
	if app.Git.RefExists(ctx, repo, dev.Source.Ref) {
		t.Fatal("rm must delete the managed branch")
	}
	if err := app.RemoveWorkspace(ctx, review.ID, repo, false); err != nil {
		t.Fatal(err)
	}
	if !pathIsDir(repo) {
		t.Fatal("original repository removed")
	}
}

func TestCreateWorkspaceRecordsBaseForAllTypes(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app := newTestApp(t, repo)
	gitCommand(t, repo, "checkout", "feature/a")
	for _, kind := range []WorkspaceType{WorkspaceDev, WorkspaceFix, WorkspaceReview, WorkspaceTest} {
		workspace, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: kind})
		if err != nil {
			t.Fatalf("type=%s err=%v", kind, err)
		}
		if workspace.Source.BaseRef != "feature/a" {
			t.Fatalf("type=%s base ref=%q", kind, workspace.Source.BaseRef)
		}
		if workspace.Source.BaseCommit == "" {
			t.Fatalf("type=%s base commit is empty", kind)
		}
		if want := strings.TrimSpace(gitCommand(t, repo, "rev-parse", "feature/a")); workspace.Source.BaseCommit != want {
			t.Fatalf("type=%s base commit=%q want=%q", kind, workspace.Source.BaseCommit, want)
		}
	}
}

func TestCreateWorkspaceDetachedFlagAndAutoName(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app := newTestApp(t, repo)
	gitCommand(t, repo, "checkout", "feature/b")
	workspace, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Detach: true})
	if err != nil {
		t.Fatal(err)
	}
	if !workspace.Source.Detached {
		t.Fatalf("source=%#v", workspace.Source)
	}
	if workspace.Type != WorkspaceDev {
		t.Fatalf("type=%q", workspace.Type)
	}
	if workspace.Name != "feature-b" {
		t.Fatalf("auto name=%q", workspace.Name)
	}
	if _, err := exec.Command("git", "-C", workspace.WorktreePath, "symbolic-ref", "--quiet", "HEAD").CombinedOutput(); err == nil {
		t.Fatal("--detach produced an attached worktree")
	}
}

func TestCreateWorkspaceRejectsUnknownBase(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app := newTestApp(t, repo)
	_, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Base: "missing-ref"})
	if ErrorCode(err) != "git_ref_not_found" {
		t.Fatalf("error=%v", err)
	}
	if !strings.Contains(err.Error(), "--base") {
		t.Fatalf("error does not explain how to proceed: %v", err)
	}
}

func TestCreateWorkspaceDetachedHeadNeedsBaseOrDefault(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app := newTestApp(t, repo)
	commit := strings.TrimSpace(gitCommand(t, repo, "rev-parse", "main"))
	gitCommand(t, repo, "checkout", "--detach", commit)
	if _, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo}); ErrorCode(err) != "git_ref_not_found" {
		t.Fatalf("expected git_ref_not_found, got %v", err)
	}
	if err := SaveConfig(app.Paths.ConfigFile(), ProjectConfig{DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	workspace, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo})
	if err != nil {
		t.Fatal(err)
	}
	if workspace.Source.BaseRef != "main" {
		t.Fatalf("base=%q", workspace.Source.BaseRef)
	}
}

func TestSameNameCreatesIndependentWorkspaces(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app := newTestApp(t, repo)
	request := CreateRequest{CWD: repo, Type: WorkspaceDev, Name: "avatar", Base: "main"}
	first, err := app.CreateWorkspace(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := app.CreateWorkspace(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.Source.Ref == second.Source.Ref || first.WorktreePath == second.WorktreePath {
		t.Fatalf("workspaces are not independent: first=%#v second=%#v", first, second)
	}
	if _, err := app.ResolveWorkspace(ctx, "avatar", repo, true); ErrorCode(err) != "workspace_ambiguous" {
		t.Fatalf("duplicate name should require an ID: %v", err)
	}
	if resolved, err := app.ResolveWorkspace(ctx, first.ID, repo, true); err != nil || resolved.ID != first.ID {
		t.Fatalf("resolve by ID: workspace=%#v err=%v", resolved, err)
	}
}

func TestEmptyWorkspaceSelectorExplainsMissingArgument(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app := newTestApp(t, repo)
	if _, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Base: "main"}); err != nil {
		t.Fatal(err)
	}
	_, err := app.ResolveWorkspace(ctx, "", repo, true)
	if ErrorCode(err) != "workspace_not_found" {
		t.Fatalf("expected missing Workspace argument, got %v", err)
	}
	if strings.Contains(err.Error(), `Workspace ""`) || !strings.Contains(err.Error(), "Missing a Workspace argument") || !strings.Contains(err.Error(), "gws ls") {
		t.Fatalf("empty selector error is not actionable: %v", err)
	}
}

func TestApplyRequiresWorkspaceCWD(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app := newTestApp(t, repo)
	if _, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Base: "main"}); err != nil {
		t.Fatal(err)
	}
	_, err := app.ApplyWorkspace(ctx, ApplyRequest{CWD: repo})
	if ErrorCode(err) != "workspace_not_found" {
		t.Fatalf("apply outside a workspace must fail, got %v", err)
	}
}

func TestApplyMergesIntoBase(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app := newTestApp(t, repo)
	gitCommand(t, repo, "checkout", "feature/a")
	workspace, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Name: "first"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace.WorktreePath, "first.txt"), []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, workspace.WorktreePath, "add", "first.txt")
	gitCommand(t, workspace.WorktreePath, "commit", "-m", "first change")

	result, err := app.ApplyWorkspace(ctx, ApplyRequest{CWD: workspace.WorktreePath})
	if err != nil {
		t.Fatal(err)
	}
	if result.Workspace.ID != workspace.ID || result.TargetBranch != "feature/a" {
		t.Fatalf("result=%#v", result)
	}
	if !pathIsFile(filepath.Join(repo, "first.txt")) {
		t.Fatal("apply did not merge into the base branch checkout")
	}
	if !pathIsDir(workspace.WorktreePath) {
		t.Fatal("apply must not remove the Workspace")
	}
}

func TestDiffLogAndCommitInsideWorkspace(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app := newTestApp(t, repo)
	workspace, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.DiffWorkspace(ctx, workspace.ID, repo, false, false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := app.CommitWorkspace(ctx, workspace.WorktreePath, "empty", true); ErrorCode(err) != "workspace_clean" {
		t.Fatalf("commit of clean workspace error=%v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace.WorktreePath, "README.md"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	diff, err := app.DiffWorkspace(ctx, workspace.ID, repo, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff.Content, "changed") || diff.Base != workspace.Source.BaseCommit {
		t.Fatalf("diff=%#v", diff)
	}
	if log, err := app.LogWorkspace(ctx, workspace.ID, repo, 0, false); err != nil || strings.TrimSpace(log.Content) != "" {
		t.Fatalf("log before commit=%#v err=%v", log, err)
	}
	committed, err := app.CommitWorkspace(ctx, workspace.WorktreePath, "tweak readme", true)
	if err != nil {
		t.Fatal(err)
	}
	if committed.Subject != "tweak readme" || committed.Commit == "" {
		t.Fatalf("commit result=%#v", committed)
	}
	log, err := app.LogWorkspace(ctx, workspace.ID, repo, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.Content, "tweak readme") {
		t.Fatalf("log after commit=%q", log.Content)
	}
	uncommitted, err := app.DiffWorkspace(ctx, workspace.ID, repo, false, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(uncommitted.Content) != "" || uncommitted.Base != "HEAD" {
		t.Fatalf("uncommitted diff=%#v", uncommitted)
	}

	review, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceReview, Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(review.WorktreePath, "README.md"), []byte("review\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.CommitWorkspace(ctx, review.WorktreePath, "nope", true); ErrorCode(err) != "workspace_detached" {
		t.Fatalf("detached commit error=%v", err)
	}
}

func TestFinishFromInsideWorktreeRemovesManagedBranchFromMainCheckout(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app := newTestApp(t, repo)
	workspace, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Name: "inside-finish", Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace.WorktreePath, "inside.txt"), []byte("inside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, workspace.WorktreePath, "add", "inside.txt")
	gitCommand(t, workspace.WorktreePath, "commit", "-m", "inside change")

	insideApp := newTestApp(t, workspace.WorktreePath)
	if insideApp.Repo.Root == repo {
		t.Fatalf("test setup expected the app to be detected from the worktree, got %s", insideApp.Repo.Root)
	}
	result, err := insideApp.FinishWorkspace(ctx, "", "", workspace.WorktreePath)
	if err != nil {
		t.Fatal(err)
	}
	if !result.CWDInside {
		t.Fatal("finish from inside the worktree must report cwd_inside")
	}
	if app.Git.RefExists(ctx, repo, workspace.Source.Ref) {
		t.Fatalf("managed branch %s survived finish run from inside the worktree", workspace.Source.Ref)
	}
	if !strings.Contains(string(gitCommand(t, repo, "log", "-1", "--pretty=%s")), "inside change") {
		t.Fatal("main branch did not receive the applied commit")
	}
	assertNoGwsArtifacts(t, app, workspace)
}

func TestCommitAllStagesUntrackedAndBareCommitExplains(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app := newTestApp(t, repo)
	workspace, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace.WorktreePath, "brand-new.txt"), []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.CommitWorkspace(ctx, workspace.WorktreePath, "auto stage", false); ErrorCode(err) != "workspace_nothing_staged" {
		t.Fatalf("unstaged untracked file should report workspace_nothing_staged, got %v", err)
	}
	committed, err := app.CommitWorkspace(ctx, workspace.WorktreePath, "auto stage", true)
	if err != nil {
		t.Fatal(err)
	}
	if committed.Subject != "auto stage" {
		t.Fatalf("commit result=%#v", committed)
	}
	tracked := gitCommand(t, workspace.WorktreePath, "ls-files", "--error-unmatch", "brand-new.txt")
	if !strings.Contains(tracked, "brand-new.txt") {
		t.Fatalf("--all did not stage the untracked file: %q", tracked)
	}
	if _, err := app.CommitWorkspace(ctx, workspace.WorktreePath, "clean now", true); ErrorCode(err) != "workspace_clean" {
		t.Fatalf("clean workspace should report workspace_clean, got %v", err)
	}
}

func TestDiffAndCommitRequireWorkspaceCWD(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app := newTestApp(t, repo)
	if _, err := app.DiffWorkspace(ctx, "", repo, false, false, false); ErrorCode(err) != "workspace_not_found" {
		t.Fatalf("diff error=%v", err)
	}
	if _, err := app.CommitWorkspace(ctx, repo, "msg", false); ErrorCode(err) != "workspace_not_found" {
		t.Fatalf("commit error=%v", err)
	}
}

func TestApplyBlockedWhenMainCheckoutOnOtherBranch(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app := newTestApp(t, repo)
	gitCommand(t, repo, "checkout", "feature/a")
	workspace, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Name: "blocked"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace.WorktreePath, "x.txt"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, workspace.WorktreePath, "add", "x.txt")
	gitCommand(t, workspace.WorktreePath, "commit", "-m", "x")
	gitCommand(t, repo, "checkout", "main")

	_, err = app.ApplyWorkspace(ctx, ApplyRequest{CWD: workspace.WorktreePath})
	if ErrorCode(err) != "apply_target_not_checked_out" {
		t.Fatalf("expected apply_target_not_checked_out, got %v", err)
	}
	if !strings.Contains(err.Error(), "feature/a") || !strings.Contains(err.Error(), "--into") {
		t.Fatalf("blocking message is not actionable: %v", err)
	}
	if pathIsFile(filepath.Join(repo, "x.txt")) {
		t.Fatal("blocked apply merged changes")
	}

	result, err := app.ApplyWorkspace(ctx, ApplyRequest{CWD: workspace.WorktreePath, Into: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetBranch != "main" || !pathIsFile(filepath.Join(repo, "x.txt")) {
		t.Fatalf("--into apply result=%#v", result)
	}
}

func TestApplyBlockedOnSHABase(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app := newTestApp(t, repo)
	commit := strings.TrimSpace(gitCommand(t, repo, "rev-parse", "main"))
	workspace, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Name: "sha", Base: commit})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace.WorktreePath, "s.txt"), []byte("s\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, workspace.WorktreePath, "add", "s.txt")
	gitCommand(t, workspace.WorktreePath, "commit", "-m", "s")
	_, err = app.ApplyWorkspace(ctx, ApplyRequest{CWD: workspace.WorktreePath})
	if ErrorCode(err) != "apply_sha_target" {
		t.Fatalf("expected apply_sha_target, got %v", err)
	}
	if _, err := app.ApplyWorkspace(ctx, ApplyRequest{CWD: workspace.WorktreePath, Into: "nope"}); ErrorCode(err) != "git_ref_not_found" {
		t.Fatalf("expected git_ref_not_found for unknown --into, got %v", err)
	}
}

func TestApplyBlockedOnDirtyWorkspaceAndDirtyTarget(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app := newTestApp(t, repo)
	workspace, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Name: "dirty", Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace.WorktreePath, "d.txt"), []byte("d\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ApplyWorkspace(ctx, ApplyRequest{CWD: workspace.WorktreePath}); ErrorCode(err) != "workspace_dirty" {
		t.Fatalf("expected workspace_dirty, got %v", err)
	}
	gitCommand(t, workspace.WorktreePath, "add", "d.txt")
	gitCommand(t, workspace.WorktreePath, "commit", "-m", "d")
	if err := os.WriteFile(filepath.Join(repo, "local.txt"), []byte("local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ApplyWorkspace(ctx, ApplyRequest{CWD: workspace.WorktreePath}); ErrorCode(err) != "target_dirty" {
		t.Fatalf("expected target_dirty, got %v", err)
	}
	if err := os.Remove(filepath.Join(repo, "local.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ApplyWorkspace(ctx, ApplyRequest{CWD: workspace.WorktreePath}); err != nil {
		t.Fatalf("apply after cleaning target: %v", err)
	}
}

func TestApplyConflictPreservesWorkspace(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app := newTestApp(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "conflict.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, repo, "add", "conflict.txt")
	gitCommand(t, repo, "commit", "-m", "base conflict file")

	workspace, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Name: "conflict", Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace.WorktreePath, "conflict.txt"), []byte("side A\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, workspace.WorktreePath, "add", "conflict.txt")
	gitCommand(t, workspace.WorktreePath, "commit", "-m", "side A")
	if err := os.WriteFile(filepath.Join(repo, "conflict.txt"), []byte("side B\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, repo, "add", "conflict.txt")
	gitCommand(t, repo, "commit", "-m", "side B")

	_, err = app.ApplyWorkspace(ctx, ApplyRequest{CWD: workspace.WorktreePath})
	if ErrorCode(err) != "merge_conflict" {
		t.Fatalf("expected merge_conflict, got %v", err)
	}
	if !pathIsDir(workspace.WorktreePath) {
		t.Fatal("conflicting apply removed the Workspace")
	}
	gitCommand(t, repo, "merge", "--abort")
}

func assertNoGwsArtifacts(t *testing.T, app *App, workspace Workspace) {
	t.Helper()
	if pathIsDir(workspace.WorktreePath) {
		t.Fatalf("worktree directory survived release: %s", workspace.WorktreePath)
	}
	if pathIsDir(app.Paths.WorktreesDir) {
		t.Fatalf("empty worktree root survived release: %s", app.Paths.WorktreesDir)
	}
	if pathIsFile(workspace.TaskFilePath) {
		t.Fatalf("task file survived release: %s", workspace.TaskFilePath)
	}
	if pathIsFile(app.Paths.WorkspaceFile(workspace.ID)) {
		t.Fatalf("workspace metadata survived release: %s", app.Paths.WorkspaceFile(workspace.ID))
	}
	if workspace.Source.Managed && app.Git.RefExists(context.Background(), app.Repo.Root, workspace.Source.Ref) {
		t.Fatalf("managed branch survived release: %s", workspace.Source.Ref)
	}
}

func TestReleaseRemovesEveryGwsArtifact(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app := newTestApp(t, repo)
	workspace, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Name: "release", Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace.WorktreePath, "release.txt"), []byte("work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, workspace.WorktreePath, "add", "release.txt")
	gitCommand(t, workspace.WorktreePath, "commit", "-m", "unmerged work")

	if err := app.RemoveWorkspace(ctx, workspace.ID, repo, false); ErrorCode(err) != "workspace_unmerged" {
		t.Fatalf("rm without --force must refuse unmerged work: %v", err)
	}
	if err := app.RemoveWorkspace(ctx, workspace.ID, repo, true); err != nil {
		t.Fatal(err)
	}
	assertNoGwsArtifacts(t, app, workspace)
	if values, err := app.Workspaces(ctx); err != nil || len(values) != 0 {
		t.Fatalf("workspaces=%#v err=%v", values, err)
	}
	if got := strings.TrimSpace(gitCommand(t, repo, "status", "--porcelain")); got != "" {
		t.Fatalf("release dirtied the main checkout:\n%s", got)
	}
}

func TestReleaseCleansWhenWorktreeAlreadyVanished(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app := newTestApp(t, repo)
	workspace, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Name: "vanished", Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	gitCommand(t, repo, "worktree", "remove", "--force", workspace.WorktreePath)
	if err := app.RemoveWorkspace(ctx, workspace.ID, repo, false); err != nil {
		t.Fatal(err)
	}
	assertNoGwsArtifacts(t, app, workspace)
}

func TestFinishMergesAndCleansUp(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app := newTestApp(t, repo)
	workspace, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Name: "finish", Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace.WorktreePath, "finish.txt"), []byte("finished\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, workspace.WorktreePath, "add", "finish.txt")
	gitCommand(t, workspace.WorktreePath, "commit", "-m", "finish change")

	inside, err := app.FinishWorkspace(ctx, "", "", workspace.WorktreePath)
	if err != nil {
		t.Fatalf("finish from inside the Workspace: %v", err)
	}
	if !inside.CWDInside || inside.Workspace.ID != workspace.ID || inside.TargetBranch != "main" {
		t.Fatalf("finish from inside resolved incorrectly: %#v", inside)
	}
	assertNoGwsArtifacts(t, app, workspace)
	if !pathIsFile(filepath.Join(repo, "finish.txt")) {
		t.Fatal("finish from inside the Workspace did not apply the change")
	}
	recreated, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Name: "finish", Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	workspace = recreated
	if err := os.WriteFile(filepath.Join(workspace.WorktreePath, "finish.txt"), []byte("again\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, workspace.WorktreePath, "add", "finish.txt")
	gitCommand(t, workspace.WorktreePath, "commit", "-m", "finish change again")

	result, err := app.FinishWorkspace(ctx, workspace.ID, "", repo)
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetBranch != "main" || pathIsDir(workspace.WorktreePath) {
		t.Fatalf("unexpected finish result=%#v exists=%t", result, pathIsDir(workspace.WorktreePath))
	}
	if !pathIsFile(filepath.Join(repo, "finish.txt")) || app.Git.RefExists(ctx, repo, workspace.Source.Ref) {
		t.Fatal("finish must merge and delete its managed branch")
	}
	assertNoGwsArtifacts(t, app, workspace)
	repeated, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Name: "finish", Base: "main"})
	if err != nil {
		t.Fatalf("reuse finished name: %v", err)
	}
	if repeated.Name != workspace.Name || repeated.Source.Ref == workspace.Source.Ref {
		t.Fatalf("repeated workspace did not receive a fresh branch: old=%#v new=%#v", workspace, repeated)
	}
}

func TestFinishBlockedPreservesWorkspace(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app := newTestApp(t, repo)
	workspace, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceReview, Name: "ro", Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.FinishWorkspace(ctx, workspace.ID, "", repo); ErrorCode(err) != "workspace_detached" {
		t.Fatalf("expected workspace_detached, got %v", err)
	}
	if !pathIsDir(workspace.WorktreePath) {
		t.Fatal("blocked finish removed the Workspace")
	}
}

func TestStatusReportsUnknownAndPropagatesGitErrors(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app := newTestApp(t, repo)
	workspace, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Name: "status", Base: "main"})
	if err != nil {
		t.Fatal(err)
	}

	status, err := app.Status(ctx, workspace.ID, repo)
	if err != nil || status.Dirty == nil || *status.Dirty {
		t.Fatalf("expected a known clean status, status=%#v err=%v", status, err)
	}

	gitFile := filepath.Join(workspace.WorktreePath, ".git")
	disabledGitFile := gitFile + ".disabled"
	if err := os.Rename(gitFile, disabledGitFile); err != nil {
		t.Fatal(err)
	}
	_, statusErr := app.Status(ctx, workspace.ID, repo)
	if err := os.Rename(disabledGitFile, gitFile); err != nil {
		t.Fatal(err)
	}
	if ErrorCode(statusErr) != "external_command_failed" {
		t.Fatalf("expected Git status error to propagate, got %v", statusErr)
	}

	gitCommand(t, repo, "worktree", "remove", "--force", workspace.WorktreePath)
	status, err = app.Status(ctx, workspace.ID, repo)
	if err != nil {
		t.Fatal(err)
	}
	if status.WorktreeExists || status.Dirty != nil {
		t.Fatalf("missing worktree must report unknown dirty state: %#v", status)
	}
}

func TestGCReportsDirectoryReadErrors(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app := newTestApp(t, repo)
	for _, test := range []struct {
		name string
		path func(Paths) string
	}{
		{name: "tasks", path: Paths.TasksDir},
		{name: "worktrees", path: func(p Paths) string { return p.WorktreesDir }},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := test.path(app.Paths)
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				t.Fatal(err)
			}
			_ = os.RemoveAll(target)
			if err := os.WriteFile(target, []byte("not a directory"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := app.GC(ctx, true); ErrorCode(err) != "gc_scan_failed" {
				t.Fatalf("expected gc_scan_failed, got %v", err)
			}
			_ = os.Remove(target)
		})
	}
}

func TestApplyFromNonReadyWorkspaceReportsState(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app := newTestApp(t, repo)
	workspace, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Name: "not-ready", Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	workspace.State = StateError
	if err := app.Store.SaveWorkspace(workspace); err != nil {
		t.Fatal(err)
	}
	_, err = app.ApplyWorkspace(ctx, ApplyRequest{CWD: workspace.WorktreePath})
	if ErrorCode(err) != "workspace_not_ready" {
		t.Fatalf("expected workspace_not_ready, got %v", err)
	}
}

func TestWorktreeLayoutLivesInsideGitDir(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app := newTestApp(t, repo)
	workspace, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Name: "layout", Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	repoCanonical, _ := canonical(repo)
	expected := filepath.Join(repoCanonical, ".git", "gws", "worktrees", workspace.ID)
	got, err := canonical(workspace.WorktreePath)
	if err != nil {
		t.Fatal(err)
	}
	if got != expected {
		t.Fatalf("worktree path=%q expected=%q", got, expected)
	}
	if got := strings.TrimSpace(gitCommand(t, repo, "status", "--porcelain")); got != "" {
		t.Fatalf("creating a worktree dirtied the main checkout:\n%s", got)
	}
	if filepath.Dir(expected) == repoCanonical {
		t.Fatalf("worktree must not sit in the repository root: %s", expected)
	}
	stateFiles := []string{app.Paths.ConfigFile(), app.Paths.WorkspaceFile(workspace.ID), app.Paths.TaskFile(workspace.ID), app.Paths.LockFile()}
	for _, file := range stateFiles {
		if !strings.HasPrefix(file, filepath.Join(repoCanonical, ".git", "gws")) {
			t.Fatalf("state file escaped .git/gws: %s", file)
		}
	}
	if !pathIsFile(app.Paths.TaskFile(workspace.ID)) {
		t.Fatal("task file missing")
	}
	content, err := os.ReadFile(app.Paths.TaskFile(workspace.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "Base: main") || !strings.Contains(string(content), "Base commit: "+workspace.Source.BaseCommit) {
		t.Fatalf("task file lacks base info:\n%s", content)
	}
}

func TestDetectRepositoryWorksFromWorkspaceCWD(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app := newTestApp(t, repo)
	workspace, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Name: "shared", Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	fromWorkspace, err := app.Git.DetectRepository(ctx, workspace.WorktreePath)
	if err != nil {
		t.Fatal(err)
	}
	if fromWorkspace.GitCommonDir != app.Repo.GitCommonDir {
		t.Fatalf("common dir mismatch: %q vs %q", fromWorkspace.GitCommonDir, app.Repo.GitCommonDir)
	}
	other := newTestApp(t, workspace.WorktreePath)
	if other.Paths.StateDir != app.Paths.StateDir {
		t.Fatalf("state dir differs when run from the workspace: %q vs %q", other.Paths.StateDir, app.Paths.StateDir)
	}
	if _, err := other.ResolveWorkspace(ctx, workspace.ID, workspace.WorktreePath, true); err != nil {
		t.Fatalf("workspace metadata is not shared: %v", err)
	}
}
