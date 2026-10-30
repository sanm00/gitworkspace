package gitworkspace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

type App struct {
	Paths  Paths
	Store  Store
	Git    Git
	Locker StateLocker
	Repo   Repository
	Now    func() time.Time
}

func NewApp(paths Paths, repo Repository) *App {
	return &App{
		Paths:  paths,
		Store:  Store{Paths: paths},
		Git:    Git{Runner: Runner{}},
		Locker: StateLocker{Path: paths.LockFile(), Timeout: 10 * time.Second},
		Repo:   repo,
		Now:    func() time.Time { return time.Now().UTC() },
	}
}

func (a *App) Config() (ProjectConfig, error) { return LoadConfig(a.Paths.ConfigFile()) }

type CreateRequest struct {
	CWD    string
	Name   string
	Base   string
	Type   WorkspaceType
	Detach bool
	Fetch  bool
}

var shaPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

func (a *App) CreateWorkspace(ctx context.Context, request CreateRequest) (workspace Workspace, err error) {
	err = a.Locker.WithExclusive(ctx, func(ctx context.Context) error {
		repoPath := a.Repo.Root
		if !a.Git.HasCommits(ctx, repoPath) {
			return NewError("git_ref_not_found", "This repository has no commits. Create an initial commit (for example: git commit --allow-empty -m \"Initial commit\"), then retry.")
		}
		if request.Fetch {
			if request.Type != WorkspaceReview && request.Type != WorkspaceTest {
				return NewError("config_invalid", "--fetch is supported only with --type review or --type test.")
			}
			if err := a.Git.Fetch(ctx, repoPath); err != nil {
				return err
			}
		}
		baseRef := request.Base
		detached := request.Type == WorkspaceReview || request.Type == WorkspaceTest || request.Detach
		if baseRef == "" {
			repo, detectErr := a.Git.DetectRepository(ctx, request.CWD)
			if detectErr == nil && repo.GitCommonDir == a.Repo.GitCommonDir && repo.CurrentBranch != "" {
				baseRef = repo.CurrentBranch
			} else {
				config, configErr := a.Config()
				if configErr != nil {
					return configErr
				}
				baseRef = config.DefaultBranch
				if baseRef == "" {
					if detectErr != nil || repo.CurrentBranch == "" {
						return NewError("git_ref_not_found", "HEAD is detached and no default branch is configured. Pass --base <branch|sha>, or set default_branch in "+a.Paths.ConfigFile()+".")
					}
					return NewError("git_ref_not_found", "Could not detect a base branch. Pass --base <branch|sha>.")
				}
			}
		}
		resolved := baseRef
		isBranch := a.Git.IsBranch(ctx, repoPath, resolved)
		if request.Type == WorkspaceReview || request.Type == WorkspaceTest {
			candidate := baseRef
			if !strings.HasPrefix(candidate, "origin/") && a.Git.RefExists(ctx, repoPath, candidate) {
				remoteRef := "origin/" + candidate
				if a.Git.RefExists(ctx, repoPath, remoteRef) {
					candidate = remoteRef
				}
			}
			if !a.Git.RefExists(ctx, repoPath, candidate) {
				return NewError("git_ref_not_found", fmt.Sprintf("Git ref %q does not exist locally. Retry with --fetch to obtain it from a remote.", baseRef))
			}
			resolved = candidate
		} else if !a.Git.RefExists(ctx, repoPath, resolved) {
			return NewError("git_ref_not_found", fmt.Sprintf("Git ref %q does not exist locally. Retry with --fetch, or choose an existing ref with --base.", baseRef))
		}
		baseCommit, err := a.Git.ResolveCommit(ctx, repoPath, resolved)
		if err != nil {
			return err
		}
		name := request.Name
		if name == "" {
			if isBranch {
				name = slug(strings.TrimPrefix(resolved, "origin/"))
			}
			if name == "" {
				name = "workspace"
			}
			if request.Type == WorkspaceReview {
				name = "review-" + name
			} else if request.Type == WorkspaceTest {
				name = "test-" + name
			}
		}
		name = slug(name)
		if name == "" {
			return NewError("config_invalid", "Workspace name is empty after normalization.")
		}
		workspaces, listErr := a.Store.Workspaces()
		if listErr != nil {
			return listErr
		}
		id, idErr := uniqueWorkspaceID(workspaces, name)
		if idErr != nil {
			return idErr
		}
		source := SourceSpec{Kind: "branch", Ref: resolved, BaseRef: baseRef, BaseCommit: baseCommit, Detached: detached}
		branch := ""
		if !detached {
			source.New = true
			source.Managed = true
			source.FromRef = resolved
			branch, idErr = a.uniqueManagedBranch(ctx, repoPath, id)
			if idErr != nil {
				return idErr
			}
			source.Ref = branch
		}
		now := a.Now()
		if request.Type == "" {
			request.Type = WorkspaceDev
		}
		workspace = Workspace{StateVersion: CurrentStateVersion, ID: id, Name: name, Type: request.Type, Source: source, WorktreePath: a.Paths.WorktreePath(id), TaskFilePath: a.Paths.TaskFile(id), State: StateCreating, CreatedAt: now, UpdatedAt: now}
		if err := a.Store.SaveWorkspace(workspace); err != nil {
			return err
		}
		worktreeCreated, taskCreated := false, false
		rollback := func(cause error) error {
			var rollbackErr error
			if taskCreated {
				if removeErr := removeIfExists(workspace.TaskFilePath); removeErr != nil {
					rollbackErr = removeErr
				}
			}
			if worktreeCreated {
				if removeErr := a.Git.RemoveWorktree(ctx, repoPath, workspace.WorktreePath, true); removeErr != nil {
					rollbackErr = removeErr
				} else {
					_ = a.Git.Prune(ctx, repoPath)
				}
			}
			if workspace.Source.Managed && a.Git.RefExists(ctx, repoPath, workspace.Source.Ref) {
				if branchErr := a.Git.DeleteBranch(ctx, repoPath, workspace.Source.Ref, true); branchErr != nil && rollbackErr == nil {
					rollbackErr = branchErr
				}
			}
			if rollbackErr != nil {
				workspace.State, workspace.LastError, workspace.UpdatedAt = StateError, fmt.Sprintf("creation failed: %v; rollback failed: %v", cause, rollbackErr), a.Now()
				_ = a.Store.SaveWorkspace(workspace)
				return WrapError("external_command_failed", "Workspace creation failed and rollback was incomplete. Run gws show and gws gc --dry-run.", cause)
			}
			_ = a.Store.DeleteWorkspace(workspace.ID)
			return cause
		}
		if err := os.MkdirAll(filepath.Dir(workspace.WorktreePath), 0o700); err != nil {
			return rollback(err)
		}
		if err := a.Git.AddWorktree(ctx, repoPath, workspace.WorktreePath, workspace.Source); err != nil {
			return rollback(err)
		}
		worktreeCreated = true
		if err := WriteTask(workspace.TaskFilePath, RenderTask(workspace)); err != nil {
			return rollback(err)
		}
		taskCreated = true
		workspace.State, workspace.UpdatedAt = StateReady, a.Now()
		if err := a.Store.SaveWorkspace(workspace); err != nil {
			return rollback(err)
		}
		return nil
	})
	return
}

func (a *App) uniqueManagedBranch(ctx context.Context, repoPath, workspaceID string) (string, error) {
	base := "gws/" + workspaceID
	if !a.Git.RefExists(ctx, repoPath, base) {
		return base, nil
	}
	for i := 0; i < 16; i++ {
		candidate, err := randomID(base)
		if err != nil {
			return "", err
		}
		if !a.Git.RefExists(ctx, repoPath, candidate) {
			return candidate, nil
		}
	}
	return "", errors.New("could not allocate unique managed Git branch")
}

func (a *App) Workspaces(ctx context.Context) (values []Workspace, err error) {
	err = a.Locker.WithShared(ctx, func(context.Context) error { var e error; values, e = a.Store.Workspaces(); return e })
	return
}

func (a *App) ResolveWorkspace(ctx context.Context, selector, cwd string, allowPrefix bool) (workspace Workspace, err error) {
	err = a.Locker.WithShared(ctx, func(ctx context.Context) error {
		var e error
		workspace, e = a.resolveWorkspaceUnlocked(ctx, selector, cwd, allowPrefix)
		return e
	})
	return
}

func (a *App) resolveWorkspaceUnlocked(ctx context.Context, selector, cwd string, allowPrefix bool) (Workspace, error) {
	workspaces, err := a.Store.Workspaces()
	if err != nil {
		return Workspace{}, err
	}
	matches := []Workspace{}
	for _, w := range workspaces {
		if w.ID == selector {
			matches = []Workspace{w}
			break
		}
		if w.Name == selector {
			matches = append(matches, w)
		}
	}
	if len(matches) == 0 && allowPrefix && selector != "" {
		for _, w := range workspaces {
			if strings.HasPrefix(w.ID, selector) {
				matches = append(matches, w)
			}
		}
	}
	if len(matches) == 0 && selector == "" && cwd != "" {
		canonicalCWD, _ := canonical(cwd)
		for _, w := range workspaces {
			worktreePath, pathErr := canonical(w.WorktreePath)
			if pathErr == nil && isWithin(canonicalCWD, worktreePath, false) {
				matches = append(matches, w)
			}
		}
	}
	if len(matches) == 0 {
		if selector == "" {
			return Workspace{}, NewError("workspace_not_found", "Missing a Workspace argument: the current directory is not inside a registered Workspace. Pass <workspace-id>; run gws ls to list candidates.")
		}
		return Workspace{}, NewError("workspace_not_found", fmt.Sprintf("Workspace %q was not found. Run gws ls to list available Workspace IDs.", selector))
	}
	if len(matches) > 1 {
		message := fmt.Sprintf("Workspace %q is ambiguous. Run gws ls and pass a Workspace ID.", selector)
		if selector == "" {
			message = fmt.Sprintf("The current directory matches multiple Workspaces: %s. Pass a Workspace ID; run gws ls to inspect candidates.", workspaceIDs(matches))
		}
		appErr := NewError("workspace_ambiguous", message)
		appErr.Details["candidates"] = matches
		return Workspace{}, appErr
	}
	return matches[0], nil
}

func workspaceIDs(workspaces []Workspace) string {
	ids := make([]string, len(workspaces))
	for i, workspace := range workspaces {
		ids[i] = workspace.ID
	}
	return strings.Join(ids, ", ")
}

func workspaceNotReadyError(workspace Workspace) error {
	return NewError("workspace_not_ready", fmt.Sprintf("Workspace %s is in %q state and cannot be applied. Inspect it with gws status %s, then repair or remove it.", workspace.ID, workspace.State, workspace.ID))
}

func (a *App) RemoveWorkspace(ctx context.Context, selector, cwd string, force bool) error {
	return a.Locker.WithExclusive(ctx, func(ctx context.Context) error {
		workspace, err := a.resolveWorkspaceUnlocked(ctx, selector, cwd, false)
		if err != nil {
			return err
		}
		return a.removeWorkspaceUnlocked(ctx, workspace, force)
	})
}

func (a *App) removeWorkspaceUnlocked(ctx context.Context, workspace Workspace, force bool) error {
	if err := a.guardWorktreePath(workspace); err != nil {
		return err
	}
	if pathIsDir(workspace.WorktreePath) {
		dirty, err := a.Git.IsDirty(ctx, workspace.WorktreePath)
		if err != nil {
			return err
		}
		if dirty && !force {
			return NewError("workspace_dirty", "Workspace has uncommitted changes. Inspect it with gws status or use --force to discard it.")
		}
	}
	workspace.State, workspace.UpdatedAt = StateRemoving, a.Now()
	if err := a.Store.SaveWorkspace(workspace); err != nil {
		return err
	}
	if err := a.Git.RemoveWorktree(ctx, a.Repo.Root, workspace.WorktreePath, force); err != nil {
		workspace.State, workspace.LastError = StateError, err.Error()
		_ = a.Store.SaveWorkspace(workspace)
		return err
	}
	_ = a.Git.Prune(ctx, a.Repo.Root)
	if err := removeIfExists(workspace.TaskFilePath); err != nil {
		return err
	}
	if workspace.Source.Managed {
		if a.Git.RefExists(ctx, a.Repo.Root, workspace.Source.Ref) {
			if err := a.Git.DeleteBranch(ctx, a.Repo.Root, workspace.Source.Ref, force); err != nil {
				return WrapError("cleanup_branch_failed", fmt.Sprintf("Workspace worktree was removed, but managed branch %s could not be deleted.", workspace.Source.Ref), err)
			}
		}
	}
	return a.Store.DeleteWorkspace(workspace.ID)
}

type ApplyRequest struct {
	CWD  string
	Into string
}

func (a *App) ApplyWorkspace(ctx context.Context, request ApplyRequest) (result ApplyResult, err error) {
	err = a.Locker.WithExclusive(ctx, func(ctx context.Context) error {
		workspace, resolveErr := a.resolveWorkspaceUnlocked(ctx, "", request.CWD, false)
		if resolveErr != nil {
			return resolveErr
		}
		result, resolveErr = a.applyWorkspaceUnlocked(ctx, workspace, request.Into)
		return resolveErr
	})
	return
}

func (a *App) FinishWorkspace(ctx context.Context, selector, into, cwd string) (result ApplyResult, err error) {
	err = a.Locker.WithExclusive(ctx, func(ctx context.Context) error {
		workspace, resolveErr := a.resolveWorkspaceUnlocked(ctx, selector, cwd, false)
		if resolveErr != nil {
			return resolveErr
		}
		canonicalCWD, canonicalErr := canonical(cwd)
		canonicalWorktree, worktreeErr := canonical(workspace.WorktreePath)
		if canonicalErr == nil && worktreeErr == nil && isWithin(canonicalCWD, canonicalWorktree, false) {
			return NewError("workspace_in_use", fmt.Sprintf("Exit Workspace %s first, then run gws finish %s from the original repository.", workspace.Name, workspace.ID))
		}
		result, resolveErr = a.applyWorkspaceUnlocked(ctx, workspace, into)
		if resolveErr != nil {
			return resolveErr
		}
		if removeErr := a.removeWorkspaceUnlocked(ctx, workspace, false); removeErr != nil {
			return WrapError("finish_cleanup_failed", fmt.Sprintf("Changes were applied to %s, but Workspace cleanup failed. The source branch was preserved.", result.TargetBranch), removeErr)
		}
		return nil
	})
	return
}

func (a *App) applyWorkspaceUnlocked(ctx context.Context, workspace Workspace, into string) (result ApplyResult, err error) {
	result = ApplyResult{Workspace: workspace, RepoRoot: a.Repo.Root, SourceBranch: workspace.Source.Ref}
	fail := func(code, message string) (ApplyResult, error) {
		err := NewError(code, message)
		err.Details["workspace_id"] = workspace.ID
		err.Details["source_branch"] = workspace.Source.Ref
		return result, err
	}
	if workspace.State != StateReady {
		return fail("workspace_not_ready", fmt.Sprintf("Workspace %s is in %q state and cannot be applied. Inspect it with gws status %s, then repair or remove it.", workspace.ID, workspace.State, workspace.ID))
	}
	if workspace.Source.Detached {
		return fail("workspace_detached", "Detached review/test Workspaces cannot be applied. They are read-only; create a development Workspace instead.")
	}
	if !pathIsDir(workspace.WorktreePath) || !a.Git.IsWorktree(ctx, workspace.WorktreePath) {
		return fail("workspace_missing", "Workspace worktree is missing or is no longer registered with Git.")
	}
	workspaceRepo, err := a.Git.DetectRepository(ctx, workspace.WorktreePath)
	if err != nil {
		return result, err
	}
	if workspaceRepo.CurrentBranch == "" || workspaceRepo.CurrentBranch != workspace.Source.Ref {
		return fail("workspace_branch_mismatch", fmt.Sprintf("Workspace is on branch %q, but its registered source is %q.", workspaceRepo.CurrentBranch, workspace.Source.Ref))
	}
	dirty, err := a.Git.IsDirty(ctx, workspace.WorktreePath)
	if err != nil {
		return result, err
	}
	if dirty {
		return fail("workspace_dirty", "Workspace has uncommitted changes. Commit them before running gws apply.")
	}

	target := into
	if target == "" {
		target = workspace.Source.BaseRef
	}
	explicit := into != ""
	if shaPattern.MatchString(target) && !a.Git.IsBranch(ctx, a.Repo.Root, target) {
		if !explicit {
			return fail("apply_sha_target", fmt.Sprintf("Base %q is a commit SHA, not a branch; it cannot receive a merge. Pass a branch with --into <branch>.", target))
		}
		return fail("git_ref_not_found", fmt.Sprintf("Target branch %q does not exist.", target))
	}
	if target == result.SourceBranch {
		return fail("apply_same_branch", "Source and target are the same branch; there is nothing to apply.")
	}
	if !a.Git.RefExists(ctx, a.Repo.Root, target) {
		return fail("git_ref_not_found", fmt.Sprintf("Target branch %q does not exist.", target))
	}

	mainTree, err := a.Git.MainWorktree(ctx, a.Repo.Root)
	if err != nil {
		return result, err
	}
	result.RepoRoot = mainTree
	mainRepo, err := a.Git.DetectRepository(ctx, mainTree)
	if err != nil {
		return result, err
	}
	if mainRepo.CurrentBranch == "" {
		return fail("target_detached", fmt.Sprintf("The main checkout %s is in detached HEAD state. Check out %s before applying.", mainTree, target))
	}
	if mainRepo.CurrentBranch != target {
		return fail("apply_target_not_checked_out", fmt.Sprintf("The main checkout %s is on branch %q, not the target %q. Run: git -C %s checkout %s, or apply into another branch with --into %s.", mainTree, mainRepo.CurrentBranch, target, mainTree, target, mainRepo.CurrentBranch))
	}
	targetDirty, err := a.Git.IsDirty(ctx, mainTree)
	if err != nil {
		return result, err
	}
	if targetDirty {
		return fail("target_dirty", fmt.Sprintf("Target branch %q has uncommitted changes in %s. Clean the main checkout before applying.", target, mainTree))
	}
	if err := a.Git.Merge(ctx, mainTree, result.SourceBranch); err != nil {
		var appErr *AppError
		if errors.As(err, &appErr) && appErr.Code == "merge_failed" {
			return result, NewError("merge_conflict", fmt.Sprintf("Merge of %s into %s conflicts. Resolve it in %s, or run: git -C %s merge --abort. The Workspace was not cleaned up.", result.SourceBranch, target, mainTree, mainTree))
		}
		return result, err
	}
	result.TargetBranch = target
	return result, nil
}

func (a *App) Status(ctx context.Context, selector, cwd string) (status Status, err error) {
	err = a.Locker.WithShared(ctx, func(ctx context.Context) error {
		workspace, resolveErr := a.resolveWorkspaceUnlocked(ctx, selector, cwd, true)
		if resolveErr != nil {
			return resolveErr
		}
		status.Workspace = workspace
		status.RepoRoot = a.Repo.Root
		status.WorktreeExists = pathIsDir(workspace.WorktreePath)
		status.TaskExists = pathIsFile(workspace.TaskFilePath)
		if status.WorktreeExists {
			status.WorktreeRegistered = a.Git.IsWorktree(ctx, workspace.WorktreePath)
			dirty, dirtyErr := a.Git.IsDirty(ctx, workspace.WorktreePath)
			if dirtyErr != nil {
				return dirtyErr
			}
			status.Dirty = &dirty
		}
		return nil
	})
	return
}

func (a *App) baseCommitFor(ctx context.Context, workspace Workspace) (string, error) {
	if workspace.Source.BaseCommit != "" {
		return workspace.Source.BaseCommit, nil
	}
	ref := firstNonEmpty(workspace.Source.BaseRef, workspace.Source.FromRef)
	if ref == "" {
		return "", NewError("git_ref_not_found", fmt.Sprintf("Workspace %s has no recorded base commit. Run gws show %s.", workspace.ID, workspace.ID))
	}
	return a.Git.ResolveCommit(ctx, a.Repo.Root, ref)
}

func (a *App) requireWorktree(workspace Workspace) error {
	if !pathIsDir(workspace.WorktreePath) {
		return NewError("workspace_missing", fmt.Sprintf("Workspace %s worktree is missing at %s. Run gws status %s.", workspace.ID, workspace.WorktreePath, workspace.ID))
	}
	return nil
}

func (a *App) DiffWorkspace(ctx context.Context, selector, cwd string, staged, stat, uncommitted bool) (result DiffResult, err error) {
	err = a.Locker.WithShared(ctx, func(ctx context.Context) error {
		workspace, resolveErr := a.resolveWorkspaceUnlocked(ctx, selector, cwd, true)
		if resolveErr != nil {
			return resolveErr
		}
		if missingErr := a.requireWorktree(workspace); missingErr != nil {
			return missingErr
		}
		base, baseErr := a.baseCommitFor(ctx, workspace)
		if baseErr != nil {
			return baseErr
		}
		if uncommitted {
			base = "HEAD"
		}
		content, diffErr := a.Git.Diff(ctx, workspace.WorktreePath, base, staged, stat)
		if diffErr != nil {
			return diffErr
		}
		untracked, untrackedErr := a.Git.UntrackedFiles(ctx, workspace.WorktreePath)
		if untrackedErr != nil {
			return untrackedErr
		}
		result = DiffResult{Workspace: workspace, Base: base, Content: content, Untracked: untracked}
		return nil
	})
	return
}

func (a *App) LogWorkspace(ctx context.Context, selector, cwd string, limit int, stat bool) (result LogResult, err error) {
	err = a.Locker.WithShared(ctx, func(ctx context.Context) error {
		workspace, resolveErr := a.resolveWorkspaceUnlocked(ctx, selector, cwd, true)
		if resolveErr != nil {
			return resolveErr
		}
		if missingErr := a.requireWorktree(workspace); missingErr != nil {
			return missingErr
		}
		base, baseErr := a.baseCommitFor(ctx, workspace)
		if baseErr != nil {
			return baseErr
		}
		content, logErr := a.Git.Log(ctx, workspace.WorktreePath, base, limit, stat)
		if logErr != nil {
			return logErr
		}
		result = LogResult{Workspace: workspace, Base: base, Content: content}
		return nil
	})
	return
}

func (a *App) CommitWorkspace(ctx context.Context, cwd, message string, all bool) (result CommitResult, err error) {
	err = a.Locker.WithExclusive(ctx, func(ctx context.Context) error {
		workspace, resolveErr := a.resolveWorkspaceUnlocked(ctx, "", cwd, false)
		if resolveErr != nil {
			return resolveErr
		}
		if strings.TrimSpace(message) == "" {
			return NewError("config_invalid", "Commit message is empty. Pass --message <text>.")
		}
		if workspace.Source.Detached {
			return NewError("workspace_detached", "Detached review/test Workspaces are read-only and cannot commit. Create a development Workspace instead.")
		}
		if missingErr := a.requireWorktree(workspace); missingErr != nil {
			return missingErr
		}
		dirty, dirtyErr := a.Git.IsDirty(ctx, workspace.WorktreePath)
		if dirtyErr != nil {
			return dirtyErr
		}
		if !dirty {
			return NewError("workspace_clean", fmt.Sprintf("Nothing to commit in Workspace %s.", workspace.ID))
		}
		if commitErr := a.Git.Commit(ctx, workspace.WorktreePath, message, all); commitErr != nil {
			return commitErr
		}
		commit, subject, summaryErr := a.Git.HeadSummary(ctx, workspace.WorktreePath)
		if summaryErr != nil {
			return summaryErr
		}
		result = CommitResult{Workspace: workspace, Commit: commit, Subject: subject}
		return nil
	})
	return
}

func (a *App) GC(ctx context.Context, dryRun bool) (actions []GCAction, err error) {
	err = a.Locker.WithExclusive(ctx, func(ctx context.Context) error {
		workspaces, listErr := a.Store.Workspaces()
		if listErr != nil {
			return listErr
		}
		known := map[string]Workspace{}
		for _, w := range workspaces {
			known[w.ID] = w
			if !pathIsDir(w.WorktreePath) {
				action := GCAction{Kind: "metadata", Path: a.Paths.WorkspaceFile(w.ID), WorkspaceID: w.ID, Action: "remove", Reason: "worktree is missing"}
				if !dryRun {
					_ = removeIfExists(w.TaskFilePath)
					if e := a.Store.DeleteWorkspace(w.ID); e != nil {
						return e
					}
					action.Applied = true
				}
				actions = append(actions, action)
			} else if w.State != StateReady {
				actions = append(actions, GCAction{Kind: "workspace", Path: w.WorktreePath, WorkspaceID: w.ID, Action: "manual", Reason: "workspace is in " + string(w.State) + " state"})
			}
		}
		tasks, readErr := os.ReadDir(a.Paths.TasksDir())
		if readErr != nil && !os.IsNotExist(readErr) {
			return WrapError("gc_scan_failed", fmt.Sprintf("Failed to scan task directory %s.", a.Paths.TasksDir()), readErr)
		}
		for _, entry := range tasks {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			id := strings.TrimSuffix(entry.Name(), ".md")
			if _, ok := known[id]; !ok {
				path := filepath.Join(a.Paths.TasksDir(), entry.Name())
				action := GCAction{Kind: "task", Path: path, Action: "remove", Reason: "workspace metadata is missing"}
				if !dryRun {
					if e := removeIfExists(path); e != nil {
						return e
					}
					action.Applied = true
				}
				actions = append(actions, action)
			}
		}
		entries, readErr := os.ReadDir(a.Paths.WorktreesDir)
		if readErr != nil && !os.IsNotExist(readErr) {
			return WrapError("gc_scan_failed", fmt.Sprintf("Failed to scan worktree directory %s.", a.Paths.WorktreesDir), readErr)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			if _, ok := known[entry.Name()]; !ok {
				actions = append(actions, GCAction{Kind: "worktree", Path: filepath.Join(a.Paths.WorktreesDir, entry.Name()), WorkspaceID: entry.Name(), Action: "manual", Reason: "ownership cannot be fully proven without workspace metadata"})
			}
		}
		sort.Slice(actions, func(i, j int) bool { return actions[i].Path < actions[j].Path })
		return nil
	})
	return
}

func (a *App) guardWorktreePath(workspace Workspace) error {
	target, err := canonical(workspace.WorktreePath)
	if err != nil {
		return WrapError("workspace_path_unsafe", "Could not canonicalize Workspace path.", err)
	}
	root, err := canonical(a.Paths.WorktreesDir)
	if err != nil {
		return err
	}
	expected, _ := canonical(a.Paths.WorktreePath(workspace.ID))
	repo, _ := canonical(a.Repo.Root)
	parent, _ := canonical(filepath.Dir(repo))
	if target != expected || target == root || target == repo || target == parent || !isWithin(target, root, true) {
		return NewError("workspace_path_unsafe", fmt.Sprintf("Refusing to remove unsafe Workspace path %s.", target))
	}
	if info, lerr := os.Lstat(workspace.WorktreePath); lerr == nil && info.Mode()&os.ModeSymlink != 0 {
		return NewError("workspace_path_unsafe", "Refusing to remove a symlinked Workspace root.")
	}
	return nil
}

func isWithin(path, root string, strict bool) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if strict && rel == "." {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel))
}
func pathIsDir(path string) bool  { info, err := os.Stat(path); return err == nil && info.IsDir() }
func pathIsFile(path string) bool { info, err := os.Stat(path); return err == nil && !info.IsDir() }
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
func slug(value string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	result := strings.Trim(b.String(), "-")
	runes := []rune(result)
	if len(runes) > 48 {
		result = strings.TrimRight(string(runes[:48]), "-")
	}
	return result
}
func uniqueWorkspaceID(workspaces []Workspace, base string) (string, error) {
	used := map[string]bool{}
	for _, w := range workspaces {
		used[w.ID] = true
	}
	for i := 0; i < 16; i++ {
		id, err := randomID(base)
		if err != nil {
			return "", err
		}
		if !used[id] {
			return id, nil
		}
	}
	return "", errors.New("could not allocate unique workspace id")
}
func randomID(base string) (string, error) {
	bytes := make([]byte, 2)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base + "-" + hex.EncodeToString(bytes), nil
}
