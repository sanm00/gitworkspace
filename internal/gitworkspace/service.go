package gitworkspace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	Now    func() time.Time
}

func NewApp(paths Paths) (*App, error) {
	if err := paths.Ensure(); err != nil {
		return nil, err
	}
	return &App{Paths: paths, Store: Store{Paths: paths}, Git: Git{Runner: Runner{}}, Locker: StateLocker{Path: paths.LockFile(), Timeout: 10 * time.Second}, Now: func() time.Time { return time.Now().UTC() }}, nil
}

type InitRequest struct{ CWD, Name, DefaultBranch string }

func (a *App) InitProject(ctx context.Context, request InitRequest) (project Project, updated bool, err error) {
	err = a.Locker.WithExclusive(ctx, func(ctx context.Context) error {
		repo, detectErr := a.Git.DetectRepository(ctx, request.CWD)
		cwd, canonicalErr := canonical(request.CWD)
		if canonicalErr != nil {
			return canonicalErr
		}
		initializeRepository := detectErr != nil || repo.Root != cwd
		if initializeRepository {
			config, configErr := LoadProjectConfig(filepath.Join(request.CWD, ".git-workspace.yaml"))
			if configErr != nil {
				return configErr
			}
			initialBranch := firstNonEmpty(request.DefaultBranch, config.DefaultBranch)
			if initErr := a.Git.InitRepository(ctx, request.CWD, initialBranch); initErr != nil {
				return initErr
			}
			repo, detectErr = a.Git.DetectRepository(ctx, request.CWD)
			if detectErr != nil {
				return detectErr
			}
		}
		projects, listErr := a.Store.Projects()
		if listErr != nil {
			return listErr
		}
		for _, candidate := range projects {
			if candidate.GitCommonDir != repo.GitCommonDir {
				continue
			}
			project = candidate
			configPath := filepath.Join(project.RepoPath, ".git-workspace.yaml")
			config, configErr := LoadProjectConfig(configPath)
			if configErr != nil {
				return configErr
			}
			name := firstNonEmpty(request.Name, config.Name, project.Name)
			branch := firstNonEmpty(request.DefaultBranch, config.DefaultBranch, project.DefaultBranch)
			if err := validateProjectName(projects, project.ID, name); err != nil {
				return err
			}
			if name != project.Name || branch != project.DefaultBranch || project.ConfigPath != configPath {
				project.Name, project.DefaultBranch, project.ConfigPath, project.UpdatedAt = name, branch, configPath, a.Now()
				if err := a.Store.SaveProject(project); err != nil {
					return err
				}
				updated = true
			}
			return nil
		}
		configPath := filepath.Join(repo.Root, ".git-workspace.yaml")
		config, configErr := LoadProjectConfig(configPath)
		if configErr != nil {
			return configErr
		}
		name := firstNonEmpty(request.Name, config.Name, filepath.Base(repo.Root))
		branch := firstNonEmpty(request.DefaultBranch, config.DefaultBranch, repo.CurrentBranch)
		if branch == "" {
			return NewError("config_invalid", "Could not detect a default branch. Use --default-branch.")
		}
		if err := validateProjectName(projects, "", name); err != nil {
			return err
		}
		id, idErr := uniqueProjectID(projects, slug(name))
		if idErr != nil {
			return idErr
		}
		now := a.Now()
		project = Project{StateVersion: CurrentStateVersion, ID: id, Name: name, RepoPath: repo.Root, GitCommonDir: repo.GitCommonDir, DefaultBranch: branch, ConfigPath: configPath, CreatedAt: now, UpdatedAt: now}
		if err := a.Store.SaveProject(project); err != nil {
			return err
		}
		updated = true
		return nil
	})
	return
}

func validateProjectName(projects []Project, excludeID, name string) error {
	if strings.TrimSpace(name) == "" {
		return NewError("config_invalid", "Project name cannot be empty.")
	}
	for _, p := range projects {
		if p.ID != excludeID && strings.EqualFold(p.Name, name) {
			return NewError("project_ambiguous", fmt.Sprintf("Project name %q is already registered.", name))
		}
	}
	return nil
}

func (a *App) Projects(ctx context.Context) (values []Project, err error) {
	err = a.Locker.WithShared(ctx, func(context.Context) error { var e error; values, e = a.Store.Projects(); return e })
	return
}

func (a *App) ResolveProject(ctx context.Context, selector, cwd string) (project Project, err error) {
	err = a.Locker.WithShared(ctx, func(ctx context.Context) error {
		var e error
		project, e = a.resolveProjectUnlocked(ctx, selector, cwd)
		return e
	})
	return
}

func (a *App) EnsureInitialCommit(ctx context.Context, cwd string) (created bool, err error) {
	err = a.Locker.WithExclusive(ctx, func(ctx context.Context) error {
		project, resolveErr := a.resolveProjectUnlocked(ctx, "", cwd)
		if resolveErr != nil {
			return resolveErr
		}
		if a.Git.HasCommits(ctx, project.RepoPath) {
			return nil
		}
		if createErr := a.Git.CreateEmptyInitialCommit(ctx, project.RepoPath); createErr != nil {
			return createErr
		}
		created = true
		return nil
	})
	return
}

func (a *App) resolveProjectUnlocked(ctx context.Context, selector, cwd string) (Project, error) {
	projects, err := a.Store.Projects()
	if err != nil {
		return Project{}, err
	}
	if selector != "" {
		return selectProject(projects, selector, true)
	}
	canonicalCWD, err := canonical(cwd)
	if err == nil {
		workspaces, _ := a.Store.Workspaces()
		best := ""
		projectID := ""
		for _, w := range workspaces {
			worktreePath, pathErr := canonical(w.WorktreePath)
			if pathErr == nil && isWithin(canonicalCWD, worktreePath, false) && len(worktreePath) > len(best) {
				best, projectID = worktreePath, w.ProjectID
			}
		}
		if projectID != "" {
			for _, p := range projects {
				if p.ID == projectID {
					return p, nil
				}
			}
		}
	}
	repo, detectErr := a.Git.DetectRepository(ctx, cwd)
	if detectErr == nil {
		for _, p := range projects {
			if p.GitCommonDir == repo.GitCommonDir {
				return p, nil
			}
		}
	}
	return Project{}, NewError("project_not_found", "Could not resolve a registered Project. Enter a registered repository, run gws init, or use --project.")
}

func selectProject(projects []Project, selector string, allowPrefix bool) (Project, error) {
	canonicalSelector, _ := canonical(selector)
	matches := []Project{}
	for _, p := range projects {
		if p.ID == selector || strings.EqualFold(p.Name, selector) || (canonicalSelector != "" && p.RepoPath == canonicalSelector) {
			matches = append(matches, p)
		}
	}
	if len(matches) == 0 && allowPrefix && selector != "" {
		for _, p := range projects {
			if strings.HasPrefix(p.ID, selector) {
				matches = append(matches, p)
			}
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return Project{}, NewError("project_not_found", fmt.Sprintf("Project %q was not found.", selector))
	}
	err := NewError("project_ambiguous", fmt.Sprintf("Project %q is ambiguous.", selector))
	err.Details["candidates"] = matches
	return Project{}, err
}

func (a *App) RemoveProject(ctx context.Context, selector string) error {
	return a.Locker.WithExclusive(ctx, func(ctx context.Context) error {
		projects, err := a.Store.Projects()
		if err != nil {
			return err
		}
		project, err := selectProject(projects, selector, false)
		if err != nil {
			return err
		}
		workspaces, err := a.Store.Workspaces()
		if err != nil {
			return err
		}
		for _, w := range workspaces {
			if w.ProjectID == project.ID {
				return NewError("project_has_workspaces", fmt.Sprintf("Project %s still has registered Workspaces.", project.Name))
			}
		}
		return a.Store.DeleteProject(project.ID)
	})
}

type CreateRequest struct {
	ProjectSelector, CWD, Name, Ref, BaseRef, FromRef string
	Type                                              WorkspaceType
	New, Fetch, FromCurrent, GenerateBranch           bool
}

func (a *App) CreateWorkspace(ctx context.Context, request CreateRequest) (workspace Workspace, err error) {
	err = a.Locker.WithExclusive(ctx, func(ctx context.Context) error {
		project, resolveErr := a.resolveProjectUnlocked(ctx, request.ProjectSelector, request.CWD)
		if resolveErr != nil {
			return resolveErr
		}
		if request.Ref == "" {
			return NewError("git_ref_not_found", "A source branch is required.")
		}
		if request.Fetch {
			if request.Type != WorkspaceReview {
				return NewError("config_invalid", "--fetch is supported only by gws review.")
			}
			if err := a.Git.Fetch(ctx, project.RepoPath); err != nil {
				return err
			}
		}
		resolvedRef := request.Ref
		if request.Fetch && !request.New && !a.Git.RefExists(ctx, project.RepoPath, resolvedRef) {
			remoteRef := "origin/" + strings.TrimPrefix(resolvedRef, "origin/")
			if a.Git.RefExists(ctx, project.RepoPath, remoteRef) {
				resolvedRef = remoteRef
			}
		}
		if !request.New && !a.Git.RefExists(ctx, project.RepoPath, resolvedRef) {
			return NewError("git_ref_not_found", fmt.Sprintf("Git ref %q does not exist locally. For review, retry with --fetch.", request.Ref))
		}
		from := request.FromRef
		if request.New {
			if !request.GenerateBranch && a.Git.RefExists(ctx, project.RepoPath, resolvedRef) {
				return NewError("git_ref_exists", fmt.Sprintf("Git branch %q already exists. Choose another name or create a Workspace for the existing branch.", request.Ref))
			}
			if from == "" {
				if request.FromCurrent {
					repo, detectErr := a.Git.DetectRepository(ctx, request.CWD)
					if detectErr != nil {
						return NewError("git_ref_not_found", "Could not detect the current branch. Run this command inside the registered repository or use --from.")
					}
					if repo.GitCommonDir != project.GitCommonDir {
						return NewError("project_not_found", "The current branch belongs to a different Git repository than the selected Project. Use --from explicitly.")
					}
					from = repo.CurrentBranch
					if from == "" {
						return NewError("git_ref_not_found", "HEAD is detached, so there is no current branch. Use --from <ref>.")
					}
				} else {
					from = project.DefaultBranch
				}
			}
			if !a.Git.RefExists(ctx, project.RepoPath, from) {
				if !a.Git.HasCommits(ctx, project.RepoPath) {
					return NewError("git_ref_not_found", fmt.Sprintf("Start ref %q does not exist because this repository has no commits. Create an initial commit (for example: git commit --allow-empty -m \"Initial commit\"), then retry.", from))
				}
				return NewError("git_ref_not_found", fmt.Sprintf("Start ref %q does not exist locally. Choose an existing ref with --from <ref>, or fetch the ref from a remote first.", from))
			}
		}
		base := request.BaseRef
		if (request.Type == WorkspaceReview || request.Type == WorkspaceTest) && base == "" {
			base = project.DefaultBranch
		}
		name := request.Name
		if name == "" {
			if request.GenerateBranch {
				name = request.Ref
			} else {
				name = defaultWorkspaceName(request.Type, request.Ref)
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
		if request.GenerateBranch {
			resolvedRef, idErr = a.uniqueManagedBranch(ctx, project.RepoPath, id)
			if idErr != nil {
				return idErr
			}
		}
		now := a.Now()
		workspace = Workspace{StateVersion: CurrentStateVersion, ID: id, ProjectID: project.ID, Name: name, Type: request.Type, Source: SourceSpec{Kind: "branch", Ref: resolvedRef, BaseRef: base, Detached: request.Type == WorkspaceReview || request.Type == WorkspaceTest, New: request.New, Managed: request.GenerateBranch, FromRef: from}, WorktreePath: a.Paths.WorktreePath(project.ID, id), TaskFilePath: a.Paths.TaskFile(id), State: StateCreating, CreatedAt: now, UpdatedAt: now}
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
				if removeErr := a.Git.RemoveWorktree(ctx, project.RepoPath, workspace.WorktreePath, true); removeErr != nil {
					rollbackErr = removeErr
				} else {
					_ = a.Git.Prune(ctx, project.RepoPath)
				}
			}
			if workspace.Source.Managed && a.Git.RefExists(ctx, project.RepoPath, workspace.Source.Ref) {
				if branchErr := a.Git.DeleteBranch(ctx, project.RepoPath, workspace.Source.Ref, true); branchErr != nil && rollbackErr == nil {
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
		if err := a.Git.AddWorktree(ctx, project.RepoPath, workspace.WorktreePath, workspace.Source); err != nil {
			return rollback(err)
		}
		worktreeCreated = true
		if err := WriteTask(workspace.TaskFilePath, RenderTask(project, workspace)); err != nil {
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

func (a *App) Workspaces(ctx context.Context, projectSelector string) (values []Workspace, err error) {
	err = a.Locker.WithShared(ctx, func(ctx context.Context) error {
		var e error
		values, e = a.Store.Workspaces()
		if e != nil {
			return e
		}
		if projectSelector != "" {
			p, resolveErr := a.resolveProjectUnlocked(ctx, projectSelector, "")
			if resolveErr != nil {
				return resolveErr
			}
			filtered := []Workspace{}
			for _, w := range values {
				if w.ProjectID == p.ID {
					filtered = append(filtered, w)
				}
			}
			values = filtered
		}
		return nil
	})
	return
}

func (a *App) ResolveWorkspace(ctx context.Context, selector, projectSelector, cwd string, allowPrefix bool) (workspace Workspace, project Project, err error) {
	err = a.Locker.WithShared(ctx, func(ctx context.Context) error {
		var e error
		workspace, project, e = a.resolveWorkspaceUnlocked(ctx, selector, projectSelector, cwd, allowPrefix)
		return e
	})
	return
}

func (a *App) resolveWorkspaceUnlocked(ctx context.Context, selector, projectSelector, cwd string, allowPrefix bool) (Workspace, Project, error) {
	workspaces, err := a.Store.Workspaces()
	if err != nil {
		return Workspace{}, Project{}, err
	}
	projects, err := a.Store.Projects()
	if err != nil {
		return Workspace{}, Project{}, err
	}
	projectID := ""
	if projectSelector != "" {
		p, e := selectProject(projects, projectSelector, true)
		if e != nil {
			return Workspace{}, Project{}, e
		}
		projectID = p.ID
	}
	matches := []Workspace{}
	for _, w := range workspaces {
		if projectID != "" && w.ProjectID != projectID {
			continue
		}
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
			if (projectID == "" || w.ProjectID == projectID) && strings.HasPrefix(w.ID, selector) {
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
			return Workspace{}, Project{}, NewError("workspace_not_found", "Missing a Workspace argument: the current directory is not inside a registered Workspace. Pass <workspace-id>; run gws ls to list candidates.")
		}
		return Workspace{}, Project{}, NewError("workspace_not_found", fmt.Sprintf("Workspace %q was not found. Run gws ls to list available Workspace IDs.", selector))
	}
	if len(matches) > 1 {
		message := fmt.Sprintf("Workspace %q is ambiguous. Run gws ls and pass a Workspace ID.", selector)
		if selector == "" {
			message = fmt.Sprintf("The current directory matches multiple Workspaces: %s. Pass a Workspace ID; run gws ls to inspect candidates.", workspaceIDs(matches))
		}
		appErr := NewError("workspace_ambiguous", message)
		appErr.Details["candidates"] = matches
		return Workspace{}, Project{}, appErr
	}
	for _, p := range projects {
		if p.ID == matches[0].ProjectID {
			return matches[0], p, nil
		}
	}
	return Workspace{}, Project{}, NewError("project_not_found", "Workspace references a missing Project.")
}

func (a *App) resolveApplyWorkspaceUnlocked(ctx context.Context, selector, projectSelector, cwd string) (Workspace, Project, error) {
	if selector != "" {
		return a.resolveWorkspaceUnlocked(ctx, selector, projectSelector, cwd, true)
	}
	workspaces, err := a.Store.Workspaces()
	if err != nil {
		return Workspace{}, Project{}, err
	}
	projects, err := a.Store.Projects()
	if err != nil {
		return Workspace{}, Project{}, err
	}
	projectID := ""
	if projectSelector != "" {
		project, selectErr := selectProject(projects, projectSelector, true)
		if selectErr != nil {
			return Workspace{}, Project{}, selectErr
		}
		projectID = project.ID
	}

	canonicalCWD, _ := canonical(cwd)
	currentMatches := []Workspace{}
	for _, workspace := range workspaces {
		if projectID != "" && workspace.ProjectID != projectID {
			continue
		}
		worktreePath, pathErr := canonical(workspace.WorktreePath)
		if pathErr == nil && isWithin(canonicalCWD, worktreePath, false) {
			currentMatches = append(currentMatches, workspace)
		}
	}
	if len(currentMatches) == 1 {
		if currentMatches[0].State != StateReady {
			return Workspace{}, Project{}, workspaceNotReadyError(currentMatches[0])
		}
		for _, project := range projects {
			if project.ID == currentMatches[0].ProjectID {
				return currentMatches[0], project, nil
			}
		}
		return Workspace{}, Project{}, NewError("project_not_found", "Workspace references a missing Project.")
	}
	if len(currentMatches) > 1 {
		appErr := NewError("workspace_ambiguous", fmt.Sprintf("Current directory belongs to multiple Workspaces: %s. Run gws apply <workspace-id>.", workspaceIDs(currentMatches)))
		appErr.Details["candidates"] = currentMatches
		return Workspace{}, Project{}, appErr
	}

	var project Project
	if projectID != "" {
		for _, candidate := range projects {
			if candidate.ID == projectID {
				project = candidate
				break
			}
		}
	} else {
		project, err = a.resolveProjectUnlocked(ctx, "", cwd)
		if err != nil {
			return Workspace{}, Project{}, err
		}
		projectID = project.ID
	}

	matches := []Workspace{}
	for _, workspace := range workspaces {
		if workspace.ProjectID == projectID && workspace.State == StateReady && !workspace.Source.Detached {
			matches = append(matches, workspace)
		}
	}
	if len(matches) == 0 {
		return Workspace{}, Project{}, NewError("workspace_not_found", fmt.Sprintf("No applicable Workspace was found for Project %q. Run gws new or specify a Workspace ID.", project.Name))
	}
	if len(matches) > 1 {
		appErr := NewError("workspace_ambiguous", fmt.Sprintf("Project %q has multiple applicable Workspaces: %s. Run gws apply <workspace-id>.", project.Name, workspaceIDs(matches)))
		appErr.Details["candidates"] = matches
		return Workspace{}, Project{}, appErr
	}
	return matches[0], project, nil
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

func (a *App) RemoveWorkspace(ctx context.Context, selector, projectSelector, cwd string, force bool) error {
	return a.Locker.WithExclusive(ctx, func(ctx context.Context) error {
		if projectSelector == "" {
			project, resolveErr := a.resolveProjectUnlocked(ctx, "", cwd)
			if resolveErr != nil {
				return resolveErr
			}
			projectSelector = project.ID
		} else {
			projects, listErr := a.Store.Projects()
			if listErr != nil {
				return listErr
			}
			project, selectErr := selectProject(projects, projectSelector, false)
			if selectErr != nil {
				return selectErr
			}
			projectSelector = project.ID
		}
		workspace, project, err := a.resolveWorkspaceUnlocked(ctx, selector, projectSelector, cwd, false)
		if err != nil {
			return err
		}
		return a.removeWorkspaceUnlocked(ctx, workspace, project, force)
	})
}

func (a *App) removeWorkspaceUnlocked(ctx context.Context, workspace Workspace, project Project, force bool) error {
	if err := guardWorktreePath(a.Paths, project, workspace); err != nil {
		return err
	}
	dirty, err := a.Git.IsDirty(ctx, workspace.WorktreePath)
	if err != nil {
		return err
	}
	if dirty && !force {
		return NewError("workspace_dirty", "Workspace has uncommitted changes. Inspect it with gws status or use --force to discard it.")
	}
	workspace.State, workspace.UpdatedAt = StateRemoving, a.Now()
	if err := a.Store.SaveWorkspace(workspace); err != nil {
		return err
	}
	if err := a.Git.RemoveWorktree(ctx, project.RepoPath, workspace.WorktreePath, force); err != nil {
		workspace.State, workspace.LastError = StateError, err.Error()
		_ = a.Store.SaveWorkspace(workspace)
		return err
	}
	_ = a.Git.Prune(ctx, project.RepoPath)
	if err := removeIfExists(workspace.TaskFilePath); err != nil {
		return err
	}
	return a.Store.DeleteWorkspace(workspace.ID)
}

func (a *App) ApplyWorkspace(ctx context.Context, selector, projectSelector, cwd string) (result ApplyResult, err error) {
	err = a.Locker.WithExclusive(ctx, func(ctx context.Context) error {
		workspace, project, resolveErr := a.resolveApplyWorkspaceUnlocked(ctx, selector, projectSelector, cwd)
		if resolveErr != nil {
			return resolveErr
		}
		result, resolveErr = a.applyWorkspaceUnlocked(ctx, workspace, project)
		return resolveErr
	})
	return
}

func (a *App) applyWorkspaceUnlocked(ctx context.Context, workspace Workspace, project Project) (ApplyResult, error) {
	result := ApplyResult{Workspace: workspace, Project: project, SourceBranch: workspace.Source.Ref}
	if workspace.State != StateReady {
		return result, workspaceNotReadyError(workspace)
	}
	if workspace.Source.Detached {
		return result, NewError("workspace_detached", "Detached review/test Workspaces cannot be applied directly. Create a branch and a development Workspace first.")
	}
	if !pathIsDir(workspace.WorktreePath) || !a.Git.IsWorktree(ctx, workspace.WorktreePath) {
		return result, NewError("workspace_missing", "Workspace worktree is missing or is no longer registered with Git.")
	}
	workspaceRepo, err := a.Git.DetectRepository(ctx, workspace.WorktreePath)
	if err != nil {
		return result, err
	}
	if workspaceRepo.CurrentBranch == "" || workspaceRepo.CurrentBranch != workspace.Source.Ref {
		return result, NewError("workspace_branch_mismatch", fmt.Sprintf("Workspace is on branch %q, but its registered source is %q.", workspaceRepo.CurrentBranch, workspace.Source.Ref))
	}
	dirty, err := a.Git.IsDirty(ctx, workspace.WorktreePath)
	if err != nil {
		return result, err
	}
	if dirty {
		return result, NewError("workspace_dirty", "Workspace has uncommitted changes. Commit them before running gws apply.")
	}
	targetRepo, err := a.Git.DetectRepository(ctx, project.RepoPath)
	if err != nil {
		return result, err
	}
	if targetRepo.GitCommonDir != project.GitCommonDir {
		return result, NewError("project_not_found", "The registered original checkout no longer belongs to this Project.")
	}
	if targetRepo.CurrentBranch == "" {
		return result, NewError("target_detached", "The original checkout is in detached HEAD state. Switch it to the destination branch before applying.")
	}
	result.TargetBranch = targetRepo.CurrentBranch
	if result.TargetBranch == result.SourceBranch {
		return result, NewError("apply_same_branch", "Source and target are the same branch; there is nothing to apply.")
	}
	targetDirty, err := a.Git.IsDirty(ctx, project.RepoPath)
	if err != nil {
		return result, err
	}
	if targetDirty {
		return result, NewError("target_dirty", fmt.Sprintf("Target branch %q has uncommitted changes. Clean the original checkout before applying.", result.TargetBranch))
	}
	if err := a.Git.Merge(ctx, project.RepoPath, result.SourceBranch); err != nil {
		return result, err
	}
	return result, nil
}

func (a *App) FinishWorkspace(ctx context.Context, selector, projectSelector, cwd string) (result ApplyResult, err error) {
	err = a.Locker.WithExclusive(ctx, func(ctx context.Context) error {
		workspace, project, resolveErr := a.resolveWorkspaceUnlocked(ctx, selector, projectSelector, cwd, false)
		if resolveErr != nil {
			return resolveErr
		}
		canonicalCWD, canonicalErr := canonical(cwd)
		canonicalWorktree, worktreeErr := canonical(workspace.WorktreePath)
		if canonicalErr == nil && worktreeErr == nil && isWithin(canonicalCWD, canonicalWorktree, false) {
			return NewError("workspace_in_use", fmt.Sprintf("Exit Workspace %s first, then run gws finish %s from the original repository.", workspace.Name, workspace.ID))
		}
		result, resolveErr = a.applyWorkspaceUnlocked(ctx, workspace, project)
		if resolveErr != nil {
			return resolveErr
		}
		if removeErr := a.removeWorkspaceUnlocked(ctx, workspace, project, false); removeErr != nil {
			return WrapError("finish_cleanup_failed", fmt.Sprintf("Changes were applied to %s, but Workspace cleanup failed. The source branch was preserved.", result.TargetBranch), removeErr)
		}
		if workspace.Source.Managed {
			if branchErr := a.Git.DeleteBranch(ctx, project.RepoPath, workspace.Source.Ref, false); branchErr != nil {
				return WrapError("finish_cleanup_failed", fmt.Sprintf("Workspace was applied to %s and removed, but its managed branch %s could not be deleted.", result.TargetBranch, workspace.Source.Ref), branchErr)
			}
		}
		return nil
	})
	return
}

func (a *App) Status(ctx context.Context, selector, projectSelector, cwd string) (status Status, err error) {
	err = a.Locker.WithShared(ctx, func(ctx context.Context) error {
		workspace, project, resolveErr := a.resolveWorkspaceUnlocked(ctx, selector, projectSelector, cwd, true)
		if resolveErr != nil {
			return resolveErr
		}
		status.Workspace = workspace
		status.RepoExists = pathIsDir(project.RepoPath)
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
		if readErr != nil {
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
		projectDirs, readErr := os.ReadDir(a.Paths.WorktreesDir())
		if readErr != nil {
			return WrapError("gc_scan_failed", fmt.Sprintf("Failed to scan worktree directory %s.", a.Paths.WorktreesDir()), readErr)
		}
		for _, projectDir := range projectDirs {
			if !projectDir.IsDir() {
				continue
			}
			projectWorktreesDir := filepath.Join(a.Paths.WorktreesDir(), projectDir.Name())
			entries, readErr := os.ReadDir(projectWorktreesDir)
			if readErr != nil {
				return WrapError("gc_scan_failed", fmt.Sprintf("Failed to scan Project worktree directory %s.", projectWorktreesDir), readErr)
			}
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				if _, ok := known[entry.Name()]; !ok {
					actions = append(actions, GCAction{Kind: "worktree", Path: filepath.Join(a.Paths.WorktreesDir(), projectDir.Name(), entry.Name()), WorkspaceID: entry.Name(), Action: "manual", Reason: "ownership cannot be fully proven without workspace metadata"})
				}
			}
		}
		sort.Slice(actions, func(i, j int) bool { return actions[i].Path < actions[j].Path })
		return nil
	})
	return
}

func guardWorktreePath(paths Paths, project Project, workspace Workspace) error {
	target, err := canonical(workspace.WorktreePath)
	if err != nil {
		return WrapError("workspace_path_unsafe", "Could not canonicalize Workspace path.", err)
	}
	root, err := canonical(paths.WorktreesDir())
	if err != nil {
		return err
	}
	expected, _ := canonical(paths.WorktreePath(project.ID, workspace.ID))
	repo, _ := canonical(project.RepoPath)
	if target != expected || target == root || target == repo || !isWithin(target, root, true) {
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
func defaultWorkspaceName(kind WorkspaceType, ref string) string {
	base := filepath.Base(ref)
	if kind == WorkspaceReview {
		return "review-" + base
	}
	if kind == WorkspaceTest {
		return "test-" + base
	}
	return base
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
func uniqueProjectID(projects []Project, base string) (string, error) {
	if base == "" {
		base = "project"
	}
	used := map[string]bool{}
	for _, p := range projects {
		used[p.ID] = true
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
	return "", errors.New("could not allocate unique project id")
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
