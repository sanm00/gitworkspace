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

func TestWorkspaceLifecycle(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app, err := NewApp(Paths{Home: filepath.Join(t.TempDir(), "home")})
	if err != nil {
		t.Fatal(err)
	}
	project, _, err := app.InitProject(ctx, InitRequest{CWD: repo, Name: "Demo", DefaultBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if project.StateVersion != CurrentStateVersion {
		t.Fatalf("project state version=%d", project.StateVersion)
	}
	if !strings.HasPrefix(project.ID, "demo-") || len(project.ID) != len("demo-")+4 {
		t.Fatalf("project ID does not have a unique suffix: %q", project.ID)
	}
	again, updated, err := app.InitProject(ctx, InitRequest{CWD: repo})
	if err != nil {
		t.Fatal(err)
	}
	if updated || again.ID != project.ID {
		t.Fatalf("init not idempotent: updated=%t %#v", updated, again)
	}
	review, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceReview, Ref: "feature/b", BaseRef: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if review.StateVersion != CurrentStateVersion {
		t.Fatalf("workspace state version=%d", review.StateVersion)
	}
	if _, err := exec.Command("git", "-C", review.WorktreePath, "symbolic-ref", "--quiet", "HEAD").CombinedOutput(); err == nil {
		t.Fatal("review worktree is attached")
	}
	fromWorktree, _, err := app.InitProject(ctx, InitRequest{CWD: review.WorktreePath})
	if err != nil {
		t.Fatal(err)
	}
	canonicalRepo, _ := canonical(repo)
	if fromWorktree.RepoPath != canonicalRepo || fromWorktree.ConfigPath != filepath.Join(canonicalRepo, ".git-workspace.yaml") {
		t.Fatalf("original checkout changed: %#v", fromWorktree)
	}
	dev, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Ref: "feature/a"})
	if err != nil {
		t.Fatal(err)
	}
	if got := gitCommand(t, repo, "branch", "--show-current"); got != "main\n" {
		t.Fatalf("original branch changed: %q", got)
	}
	if err := os.WriteFile(filepath.Join(dev.WorktreePath, "dirty.txt"), []byte("dirty"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.RemoveWorkspace(ctx, dev.ID, "", repo, false); ErrorCode(err) != "workspace_dirty" {
		t.Fatalf("expected dirty error, got %v", err)
	}
	if err := app.RemoveWorkspace(ctx, dev.ID, "", repo, true); err != nil {
		t.Fatal(err)
	}
	if err := app.RemoveWorkspace(ctx, review.ID, "", repo, false); err != nil {
		t.Fatal(err)
	}
	if !pathIsDir(repo) {
		t.Fatal("original repository removed")
	}
}

func TestProjectResolutionFromWorkspaceCWD(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app, _ := NewApp(Paths{Home: filepath.Join(t.TempDir(), "home")})
	project, _, _ := app.InitProject(ctx, InitRequest{CWD: repo, Name: "Demo", DefaultBranch: "main"})
	workspace, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceReview, Ref: "feature/a"})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := app.ResolveProject(ctx, "", workspace.WorktreePath)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ID != project.ID {
		t.Fatalf("project=%s", resolved.ID)
	}
}

func TestProjectResolutionErrorUsesCanonicalCommandName(t *testing.T) {
	app, err := NewApp(Paths{Home: filepath.Join(t.TempDir(), "home")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.ResolveProject(context.Background(), "", t.TempDir())
	if err == nil {
		t.Fatal("expected project resolution error")
	}
	if !strings.Contains(err.Error(), "gws init") {
		t.Fatalf("unexpected command hint: %v", err)
	}
}

func TestCreateWorkspaceMissingStartRefExplainsHowToProceed(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app, err := NewApp(Paths{Home: filepath.Join(t.TempDir(), "home")})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := app.InitProject(ctx, InitRequest{CWD: repo, Name: "Demo", DefaultBranch: "missing"}); err != nil {
		t.Fatal(err)
	}

	_, err = app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Ref: "test", FromRef: "missing", New: true})
	if ErrorCode(err) != "git_ref_not_found" {
		t.Fatalf("expected missing ref error, got %v", err)
	}
	for _, expected := range []string{"--from <ref>", "fetch the ref from a remote"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("error does not explain how to choose a start ref; missing %q: %v", expected, err)
		}
	}
}

func TestNewWorkspaceStartsFromCurrentBranch(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app, _ := NewApp(Paths{Home: filepath.Join(t.TempDir(), "home")})
	if _, _, err := app.InitProject(ctx, InitRequest{CWD: repo, Name: "Demo", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, repo, "checkout", "feature/a")
	gitCommand(t, repo, "branch", "feature/new-command")
	workspace, err := app.CreateWorkspace(ctx, CreateRequest{
		CWD:            repo,
		Type:           WorkspaceDev,
		Ref:            "feature/new-command",
		New:            true,
		FromCurrent:    true,
		GenerateBranch: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if workspace.Source.FromRef != "feature/a" {
		t.Fatalf("from=%q", workspace.Source.FromRef)
	}
	if workspace.Name != "feature-new-command" || !strings.HasPrefix(workspace.ID, "feature-new-command-") {
		t.Fatalf("workspace name/id lost work-item prefix: name=%q id=%q", workspace.Name, workspace.ID)
	}
	if !workspace.Source.Managed || !strings.HasPrefix(workspace.Source.Ref, "gws/") {
		t.Fatalf("source=%#v", workspace.Source)
	}
	if got := strings.TrimSpace(gitCommand(t, workspace.WorktreePath, "branch", "--show-current")); got != workspace.Source.Ref {
		t.Fatalf("workspace branch=%q", got)
	}
	base := gitCommand(t, repo, "rev-parse", "feature/a")
	head := gitCommand(t, workspace.WorktreePath, "rev-parse", "HEAD")
	if base != head {
		t.Fatalf("workspace did not start from current branch: base=%q head=%q", base, head)
	}
}

func TestSameWorkItemCreatesIndependentWorkspaces(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app, _ := NewApp(Paths{Home: filepath.Join(t.TempDir(), "home")})
	if _, _, err := app.InitProject(ctx, InitRequest{CWD: repo, Name: "Demo", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	request := CreateRequest{CWD: repo, Type: WorkspaceDev, Ref: "feature/avatar", New: true, FromCurrent: true, GenerateBranch: true}
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
	if _, _, err := app.ResolveWorkspace(ctx, "feature-avatar", "", repo, true); ErrorCode(err) != "workspace_ambiguous" {
		t.Fatalf("duplicate name should require an ID: %v", err)
	}
	if resolved, _, err := app.ResolveWorkspace(ctx, first.ID, "", repo, true); err != nil || resolved.ID != first.ID {
		t.Fatalf("resolve by ID: workspace=%#v err=%v", resolved, err)
	}
}

func TestEmptyWorkspaceSelectorExplainsMissingArgument(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app, _ := NewApp(Paths{Home: filepath.Join(t.TempDir(), "home")})
	if _, _, err := app.InitProject(ctx, InitRequest{CWD: repo, Name: "Demo", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Ref: "test", FromRef: "main", New: true, GenerateBranch: true}); err != nil {
		t.Fatal(err)
	}

	_, _, err := app.ResolveWorkspace(ctx, "", "", repo, true)
	if ErrorCode(err) != "workspace_not_found" {
		t.Fatalf("expected missing Workspace argument, got %v", err)
	}
	if strings.Contains(err.Error(), `Workspace ""`) || !strings.Contains(err.Error(), "Missing a Workspace argument") || !strings.Contains(err.Error(), "gws ls") {
		t.Fatalf("empty selector error is not actionable: %v", err)
	}
}

func TestApplyWithoutSelectorPrefersWorkspaceContainingCWD(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app, _ := NewApp(Paths{Home: filepath.Join(t.TempDir(), "home")})
	if _, _, err := app.InitProject(ctx, InitRequest{CWD: repo, Name: "Demo", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	first, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Ref: "first", FromRef: "main", New: true, GenerateBranch: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Ref: "second", FromRef: "main", New: true, GenerateBranch: true}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first.WorktreePath, "first.txt"), []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, first.WorktreePath, "add", "first.txt")
	gitCommand(t, first.WorktreePath, "commit", "-m", "first change")

	result, err := app.ApplyWorkspace(ctx, "", "", first.WorktreePath)
	if err != nil {
		t.Fatal(err)
	}
	if result.Workspace.ID != first.ID || !pathIsFile(filepath.Join(repo, "first.txt")) {
		t.Fatalf("apply selected the wrong Workspace: %#v", result)
	}
}

func TestApplyWithoutSelectorUsesOnlyCurrentProjectsApplicableWorkspaces(t *testing.T) {
	ctx := context.Background()
	repoA := testRepository(t)
	repoB := testRepository(t)
	app, _ := NewApp(Paths{Home: filepath.Join(t.TempDir(), "home")})
	if _, _, err := app.InitProject(ctx, InitRequest{CWD: repoA, Name: "Project A", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := app.InitProject(ctx, InitRequest{CWD: repoB, Name: "Project B", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repoA, Type: WorkspaceDev, Ref: "other-project", FromRef: "main", New: true, GenerateBranch: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ApplyWorkspace(ctx, "", "", repoB); ErrorCode(err) != "workspace_not_found" {
		t.Fatalf("apply crossed Project boundaries: %v", err)
	}

	dev, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repoB, Type: WorkspaceDev, Ref: "applicable", FromRef: "main", New: true, GenerateBranch: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repoB, Type: WorkspaceReview, Ref: "feature/a", BaseRef: "main"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dev.WorktreePath, "applicable.txt"), []byte("applicable\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, dev.WorktreePath, "add", "applicable.txt")
	gitCommand(t, dev.WorktreePath, "commit", "-m", "applicable change")

	result, err := app.ApplyWorkspace(ctx, "", "", repoB)
	if err != nil {
		t.Fatal(err)
	}
	if result.Workspace.ID != dev.ID {
		t.Fatalf("apply did not select the sole applicable Workspace: %#v", result)
	}
}

func TestApplyWithoutSelectorReportsCurrentProjectCandidates(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app, _ := NewApp(Paths{Home: filepath.Join(t.TempDir(), "home")})
	if _, _, err := app.InitProject(ctx, InitRequest{CWD: repo, Name: "Demo", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	first, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Ref: "first", FromRef: "main", New: true, GenerateBranch: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Ref: "second", FromRef: "main", New: true, GenerateBranch: true})
	if err != nil {
		t.Fatal(err)
	}

	_, err = app.ApplyWorkspace(ctx, "", "", repo)
	if ErrorCode(err) != "workspace_ambiguous" {
		t.Fatalf("expected current-Project ambiguity, got %v", err)
	}
	if !strings.Contains(err.Error(), first.ID) || !strings.Contains(err.Error(), second.ID) || !strings.Contains(err.Error(), "gws apply <workspace-id>") {
		t.Fatalf("ambiguity error is not actionable: %v", err)
	}
	candidates, ok := ErrorDetails(err)["candidates"].([]Workspace)
	if !ok || len(candidates) != 2 {
		t.Fatalf("unexpected candidates: %#v", ErrorDetails(err)["candidates"])
	}
	ids := map[string]bool{candidates[0].ID: true, candidates[1].ID: true}
	if !ids[first.ID] || !ids[second.ID] {
		t.Fatalf("wrong candidates: %#v", candidates)
	}
}

func TestRemoveWorkspaceIsScopedToCurrentProjectUnlessExplicit(t *testing.T) {
	ctx := context.Background()
	repoA := testRepository(t)
	repoB := testRepository(t)
	app, _ := NewApp(Paths{Home: filepath.Join(t.TempDir(), "home")})
	if _, _, err := app.InitProject(ctx, InitRequest{CWD: repoA, Name: "Project A", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	projectB, _, err := app.InitProject(ctx, InitRequest{CWD: repoB, Name: "Project B", DefaultBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	workspaceB, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repoB, Type: WorkspaceDev, Ref: "foreign", FromCurrent: true, New: true, GenerateBranch: true})
	if err != nil {
		t.Fatal(err)
	}

	if err := app.RemoveWorkspace(ctx, workspaceB.ID, "", repoA, false); ErrorCode(err) != "workspace_not_found" {
		t.Fatalf("cross-project removal without --project should fail with workspace_not_found, got %v", err)
	}
	if !pathIsDir(workspaceB.WorktreePath) {
		t.Fatal("cross-project removal deleted the Workspace")
	}
	if err := app.RemoveWorkspace(ctx, workspaceB.ID, projectB.ID, repoA, false); err != nil {
		t.Fatalf("explicit project removal failed: %v", err)
	}
	if pathIsDir(workspaceB.WorktreePath) {
		t.Fatal("explicit project removal preserved the Workspace")
	}
}

func TestApplyAndFinishWorkspace(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app, _ := NewApp(Paths{Home: filepath.Join(t.TempDir(), "home")})
	if _, _, err := app.InitProject(ctx, InitRequest{CWD: repo, Name: "Demo", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}

	applyWorkspace, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Ref: "feature/apply", FromRef: "main", New: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(applyWorkspace.WorktreePath, "apply.txt"), []byte("applied\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, applyWorkspace.WorktreePath, "add", "apply.txt")
	gitCommand(t, applyWorkspace.WorktreePath, "commit", "-m", "apply change")

	// Create the second Workspace before applying the first one so both branches
	// share the same base and the second integration exercises a real merge.
	finishWorkspace, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Ref: "finish", FromRef: "main", New: true, GenerateBranch: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(finishWorkspace.WorktreePath, "finish.txt"), []byte("finished\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, finishWorkspace.WorktreePath, "add", "finish.txt")
	gitCommand(t, finishWorkspace.WorktreePath, "commit", "-m", "finish change")

	result, err := app.ApplyWorkspace(ctx, applyWorkspace.ID, "", repo)
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceBranch != "feature/apply" || result.TargetBranch != "main" {
		t.Fatalf("result=%#v", result)
	}
	if !pathIsFile(filepath.Join(repo, "apply.txt")) || !pathIsDir(applyWorkspace.WorktreePath) {
		t.Fatal("apply must merge changes without removing the Workspace")
	}

	finished, err := app.FinishWorkspace(ctx, finishWorkspace.ID, "", repo)
	if err != nil {
		t.Fatal(err)
	}
	if finished.TargetBranch != "main" || pathIsDir(finishWorkspace.WorktreePath) {
		t.Fatalf("unexpected finish result=%#v path_exists=%t", finished, pathIsDir(finishWorkspace.WorktreePath))
	}
	if !pathIsFile(filepath.Join(repo, "finish.txt")) || app.Git.RefExists(ctx, repo, finishWorkspace.Source.Ref) {
		t.Fatal("finish must merge the change and delete its managed source branch")
	}
	repeated, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Ref: "finish", FromRef: "main", New: true, GenerateBranch: true})
	if err != nil {
		t.Fatalf("reuse finished work-item name: %v", err)
	}
	if repeated.Name != finishWorkspace.Name || repeated.Source.Ref == finishWorkspace.Source.Ref {
		t.Fatalf("repeated workspace did not receive a fresh managed branch: old=%#v new=%#v", finishWorkspace, repeated)
	}
}

func TestApplySafetyFailuresPreserveWorkspace(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app, _ := NewApp(Paths{Home: filepath.Join(t.TempDir(), "home")})
	if _, _, err := app.InitProject(ctx, InitRequest{CWD: repo, Name: "Demo", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}

	dirty, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Ref: "feature/dirty", FromRef: "main", New: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirty.WorktreePath, "dirty.txt"), []byte("not committed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ApplyWorkspace(ctx, dirty.ID, "", repo); ErrorCode(err) != "workspace_dirty" {
		t.Fatalf("expected workspace_dirty, got %v", err)
	}
	if !pathIsDir(dirty.WorktreePath) {
		t.Fatal("failed apply removed Workspace")
	}
	if _, err := app.FinishWorkspace(ctx, dirty.ID, "", dirty.WorktreePath); ErrorCode(err) != "workspace_in_use" {
		t.Fatalf("expected workspace_in_use, got %v", err)
	}
	if !pathIsDir(dirty.WorktreePath) {
		t.Fatal("failed finish removed Workspace")
	}

	review, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceReview, Ref: "feature/b", BaseRef: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.ApplyWorkspace(ctx, review.ID, "", repo); ErrorCode(err) != "workspace_detached" {
		t.Fatalf("expected workspace_detached, got %v", err)
	}
	if !pathIsDir(review.WorktreePath) {
		t.Fatal("failed detached apply removed Workspace")
	}
}

func TestInitPropagatesRegistrationConfigChanges(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	configPath := filepath.Join(repo, ".git-workspace.yaml")
	if err := os.WriteFile(configPath, []byte("version: 1\nproject:\n  name: First\n  default_branch: main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	app, _ := NewApp(Paths{Home: filepath.Join(t.TempDir(), "home")})
	first, _, err := app.InitProject(ctx, InitRequest{CWD: repo})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("version: 1\nproject:\n  name: Second\n  default_branch: main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, updated, err := app.InitProject(ctx, InitRequest{CWD: repo})
	if err != nil {
		t.Fatal(err)
	}
	if !updated || second.ID != first.ID || second.Name != "Second" {
		t.Fatalf("unexpected update: %#v updated=%t", second, updated)
	}
}

func TestStatusReportsUnknownAndPropagatesGitErrors(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app, _ := NewApp(Paths{Home: filepath.Join(t.TempDir(), "home")})
	if _, _, err := app.InitProject(ctx, InitRequest{CWD: repo, Name: "Demo", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	workspace, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Ref: "status", FromRef: "main", New: true})
	if err != nil {
		t.Fatal(err)
	}

	status, err := app.Status(ctx, workspace.ID, "", repo)
	if err != nil || status.Dirty == nil || *status.Dirty {
		t.Fatalf("expected a known clean status, status=%#v err=%v", status, err)
	}

	gitFile := filepath.Join(workspace.WorktreePath, ".git")
	disabledGitFile := gitFile + ".disabled"
	if err := os.Rename(gitFile, disabledGitFile); err != nil {
		t.Fatal(err)
	}
	_, statusErr := app.Status(ctx, workspace.ID, "", repo)
	if err := os.Rename(disabledGitFile, gitFile); err != nil {
		t.Fatal(err)
	}
	if ErrorCode(statusErr) != "external_command_failed" {
		t.Fatalf("expected Git status error to propagate, got %v", statusErr)
	}

	gitCommand(t, repo, "worktree", "remove", "--force", workspace.WorktreePath)
	status, err = app.Status(ctx, workspace.ID, "", repo)
	if err != nil {
		t.Fatal(err)
	}
	if status.WorktreeExists || status.Dirty != nil {
		t.Fatalf("missing worktree must report unknown dirty state: %#v", status)
	}
}

func TestGCReportsDirectoryReadErrors(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name string
		path func(Paths) string
	}{
		{name: "tasks", path: Paths.TasksDir},
		{name: "worktrees", path: Paths.WorktreesDir},
	} {
		t.Run(test.name, func(t *testing.T) {
			paths := Paths{Home: filepath.Join(t.TempDir(), "home")}
			app, err := NewApp(paths)
			if err != nil {
				t.Fatal(err)
			}
			target := test.path(paths)
			if err := os.Remove(target); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, []byte("not a directory"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := app.GC(ctx, true); ErrorCode(err) != "gc_scan_failed" {
				t.Fatalf("expected gc_scan_failed, got %v", err)
			}
		})
	}
}

func TestApplyFromNonReadyWorkspaceReportsState(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	app, _ := NewApp(Paths{Home: filepath.Join(t.TempDir(), "home")})
	if _, _, err := app.InitProject(ctx, InitRequest{CWD: repo, Name: "Demo", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	workspace, err := app.CreateWorkspace(ctx, CreateRequest{CWD: repo, Type: WorkspaceDev, Ref: "not-ready", FromRef: "main", New: true})
	if err != nil {
		t.Fatal(err)
	}
	workspace.State = StateError
	if err := app.Store.SaveWorkspace(workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ApplyWorkspace(ctx, "", "", workspace.WorktreePath); ErrorCode(err) != "workspace_not_ready" {
		t.Fatalf("expected workspace_not_ready, got %v", err)
	}
}
