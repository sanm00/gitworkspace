package gitworkspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Git struct{ Runner Runner }

func (g Git) InitRepository(ctx context.Context, path, initialBranch string) error {
	args := []string{"init"}
	if initialBranch != "" {
		args = append(args, "-b", initialBranch)
	}
	_, err := g.Runner.Run(ctx, Command{Path: "git", Args: args, Dir: path})
	if err != nil {
		return WrapError("external_command_failed", "Failed to initialize a Git repository in the current directory.", err)
	}
	return nil
}

func (g Git) DetectRepository(ctx context.Context, path string) (Repository, error) {
	rootResult, err := g.Runner.Run(ctx, Command{Path: "git", Args: []string{"-C", path, "rev-parse", "--show-toplevel"}})
	if err != nil {
		return Repository{}, WrapError("project_not_found", "Current directory is not inside a Git repository.", err)
	}
	root, err := canonical(strings.TrimSpace(rootResult.Stdout))
	if err != nil {
		return Repository{}, err
	}
	commonResult, err := g.Runner.Run(ctx, Command{Path: "git", Args: []string{"-C", root, "rev-parse", "--git-common-dir"}})
	if err != nil {
		return Repository{}, WrapError("external_command_failed", "Failed to resolve Git common directory.", err)
	}
	common := strings.TrimSpace(commonResult.Stdout)
	if !filepath.IsAbs(common) {
		common = filepath.Join(root, common)
	}
	common, err = canonical(common)
	if err != nil {
		return Repository{}, err
	}
	branchResult, branchErr := g.Runner.Run(ctx, Command{Path: "git", Args: []string{"-C", root, "symbolic-ref", "--quiet", "--short", "HEAD"}})
	branch := ""
	if branchErr == nil {
		branch = strings.TrimSpace(branchResult.Stdout)
	}
	mainRoot := root
	if listResult, listErr := g.Runner.Run(ctx, Command{Path: "git", Args: []string{"-C", root, "worktree", "list", "--porcelain"}}); listErr == nil {
		for _, line := range strings.Split(listResult.Stdout, "\n") {
			if rest, ok := strings.CutPrefix(line, "worktree "); ok {
				if resolved, resolveErr := canonical(strings.TrimSpace(rest)); resolveErr == nil {
					mainRoot = resolved
				}
				break
			}
		}
	}
	return Repository{Root: root, MainRoot: mainRoot, GitCommonDir: common, CurrentBranch: branch}, nil
}

// RepoPath returns the main checkout. Repo-scoped operations must use it
// because a linked worktree can disappear (and its own root is not a valid
// context for branch or worktree administration).
func (r Repository) RepoPath() string {
	if r.MainRoot != "" {
		return r.MainRoot
	}
	return r.Root
}

func (g Git) ResolveCommit(ctx context.Context, repoPath, ref string) (string, error) {
	result, err := g.Runner.Run(ctx, Command{Path: "git", Args: []string{"-C", repoPath, "rev-parse", "--verify", "--quiet", ref + "^{commit}"}})
	if err != nil {
		return "", NewError("git_ref_not_found", fmt.Sprintf("Git ref %q does not exist.", ref))
	}
	commit := strings.TrimSpace(result.Stdout)
	if commit == "" {
		return "", NewError("git_ref_not_found", fmt.Sprintf("Git ref %q does not resolve to a commit.", ref))
	}
	return commit, nil
}

func (g Git) IsBranch(ctx context.Context, repoPath, ref string) bool {
	if ref == "" || strings.Contains(ref, "^") || strings.Contains(ref, "~") {
		return false
	}
	_, err := g.Runner.Run(ctx, Command{Path: "git", Args: []string{"-C", repoPath, "show-ref", "--verify", "--quiet", "refs/heads/" + ref}})
	return err == nil
}

func (g Git) MainWorktree(ctx context.Context, repoPath string) (string, error) {
	result, err := g.Runner.Run(ctx, Command{Path: "git", Args: []string{"-C", repoPath, "worktree", "list", "--porcelain"}})
	if err != nil {
		return "", WrapError("external_command_failed", "Failed to list Git worktrees.", err)
	}
	for _, line := range strings.Split(result.Stdout, "\n") {
		if rest, ok := strings.CutPrefix(line, "worktree "); ok {
			path := strings.TrimSpace(rest)
			if path != "" {
				return canonical(path)
			}
		}
	}
	return "", NewError("internal_error", "Git worktree list returned no entries.")
}

func (g Git) RefExists(ctx context.Context, repoPath, ref string) bool {
	_, err := g.Runner.Run(ctx, Command{Path: "git", Args: []string{"-C", repoPath, "rev-parse", "--verify", "--quiet", ref + "^{commit}"}})
	return err == nil
}

func (g Git) HasCommits(ctx context.Context, repoPath string) bool {
	result, err := g.Runner.Run(ctx, Command{Path: "git", Args: []string{"-C", repoPath, "rev-list", "--all", "--max-count=1"}})
	return err == nil && strings.TrimSpace(result.Stdout) != ""
}

func (g Git) CreateEmptyInitialCommit(ctx context.Context, repoPath string) error {
	treeResult, err := g.Runner.Run(ctx, Command{Path: "git", Args: []string{"-C", repoPath, "mktree"}})
	if err != nil {
		return WrapError("external_command_failed", "Failed to create the empty tree for the initial Git commit.", err)
	}
	tree := strings.TrimSpace(treeResult.Stdout)
	commitResult, err := g.Runner.Run(ctx, Command{Path: "git", Args: []string{"-C", repoPath, "commit-tree", tree, "-m", "Initial commit"}})
	if err != nil {
		return WrapError("external_command_failed", "Failed to create an empty initial Git commit automatically. Configure Git user.name and user.email, then retry.", err)
	}
	commit := strings.TrimSpace(commitResult.Stdout)
	if _, err := g.Runner.Run(ctx, Command{Path: "git", Args: []string{"-C", repoPath, "update-ref", "HEAD", commit}}); err != nil {
		return WrapError("external_command_failed", "Created an initial Git commit object but failed to update HEAD.", err)
	}
	return nil
}

func (g Git) Fetch(ctx context.Context, repoPath string) error {
	_, err := g.Runner.Run(ctx, Command{Path: "git", Args: []string{"-C", repoPath, "fetch", "--all", "--prune"}})
	if err != nil {
		return WrapError("external_command_failed", "Failed to fetch Git refs.", err)
	}
	return nil
}

func (g Git) AddWorktree(ctx context.Context, repoPath, path string, source SourceSpec) error {
	args := []string{"-C", repoPath, "worktree", "add"}
	if source.Detached {
		args = append(args, "--detach", path, source.Ref)
	} else if source.New {
		args = append(args, "-b", source.Ref, path, source.FromRef)
	} else {
		args = append(args, path, source.Ref)
	}
	_, err := g.Runner.Run(ctx, Command{Path: "git", Args: args})
	if err != nil {
		return WrapError("external_command_failed", fmt.Sprintf("Failed to create Git worktree for %s.", source.Ref), err)
	}
	return nil
}

func (g Git) IsDirty(ctx context.Context, path string) (bool, error) {
	result, err := g.Runner.Run(ctx, Command{Path: "git", Args: []string{"-C", path, "status", "--porcelain"}})
	if err != nil {
		return false, WrapError("external_command_failed", "Failed to inspect Workspace Git status.", err)
	}
	return strings.TrimSpace(result.Stdout) != "", nil
}

func (g Git) IsWorktree(ctx context.Context, path string) bool {
	result, err := g.Runner.Run(ctx, Command{Path: "git", Args: []string{"-C", path, "rev-parse", "--is-inside-work-tree"}})
	return err == nil && strings.TrimSpace(result.Stdout) == "true"
}

func (g Git) Merge(ctx context.Context, repoPath, ref string) error {
	_, err := g.Runner.Run(ctx, Command{Path: "git", Args: []string{"-C", repoPath, "merge", "--no-edit", ref}})
	if err != nil {
		return WrapError("merge_failed", fmt.Sprintf("Failed to merge %q. The target repository may contain conflicts; resolve them or run git merge --abort.", ref), err)
	}
	return nil
}

func (g Git) Diff(ctx context.Context, path, base string, staged, stat bool) (string, error) {
	args := []string{"-C", path, "diff", "--no-color"}
	if staged {
		args = append(args, "--staged")
	}
	if stat {
		args = append(args, "--stat")
	}
	if base != "" {
		args = append(args, base)
	}
	result, err := g.Runner.Run(ctx, Command{Path: "git", Args: args})
	if err != nil {
		return "", WrapError("external_command_failed", "Failed to compute the Workspace diff.", err)
	}
	return result.Stdout, nil
}

func (g Git) UntrackedFiles(ctx context.Context, path string) ([]string, error) {
	result, err := g.Runner.Run(ctx, Command{Path: "git", Args: []string{"-C", path, "ls-files", "--others", "--exclude-standard"}})
	if err != nil {
		return nil, WrapError("external_command_failed", "Failed to list untracked Workspace files.", err)
	}
	files := []string{}
	for _, line := range strings.Split(result.Stdout, "\n") {
		if file := strings.TrimSpace(line); file != "" {
			files = append(files, file)
		}
	}
	return files, nil
}

func (g Git) Log(ctx context.Context, path, base string, limit int, stat bool) (string, error) {
	args := []string{"-C", path, "log", "--no-color", "--oneline"}
	if stat {
		args = append(args, "--stat")
	}
	if limit > 0 {
		args = append(args, "--max-count", fmt.Sprintf("%d", limit))
	}
	args = append(args, base+"..HEAD")
	result, err := g.Runner.Run(ctx, Command{Path: "git", Args: args})
	if err != nil {
		return "", WrapError("external_command_failed", "Failed to list Workspace commits.", err)
	}
	return result.Stdout, nil
}

func (g Git) Commit(ctx context.Context, path, message string, all bool) error {
	if all {
		if _, err := g.Runner.Run(ctx, Command{Path: "git", Args: []string{"-C", path, "add", "-A"}}); err != nil {
			return WrapError("commit_failed", "Failed to stage the Workspace changes.", err)
		}
	}
	if _, err := g.Runner.Run(ctx, Command{Path: "git", Args: []string{"-C", path, "commit", "-m", message}}); err != nil {
		return WrapError("commit_failed", "Failed to commit the Workspace changes.", err)
	}
	return nil
}

func (g Git) HasStagedChanges(ctx context.Context, path string) (bool, error) {
	result, err := g.Runner.Run(ctx, Command{Path: "git", Args: []string{"-C", path, "diff", "--cached", "--quiet"}})
	if err != nil {
		if result.ExitCode == 1 {
			return true, nil
		}
		return false, WrapError("external_command_failed", "Failed to inspect staged changes.", err)
	}
	return false, nil
}

func (g Git) HeadSummary(ctx context.Context, path string) (string, string, error) {
	result, err := g.Runner.Run(ctx, Command{Path: "git", Args: []string{"-C", path, "log", "-1", "--pretty=%H%x1f%s"}})
	if err != nil {
		return "", "", WrapError("external_command_failed", "Failed to read the committed HEAD.", err)
	}
	parts := strings.SplitN(strings.TrimRight(result.Stdout, "\n"), "\x1f", 2)
	commit := parts[0]
	subject := ""
	if len(parts) > 1 {
		subject = parts[1]
	}
	return commit, subject, nil
}

func (g Git) CountCommits(ctx context.Context, repoPath, revisionRange string) (int, error) {
	result, err := g.Runner.Run(ctx, Command{Path: "git", Args: []string{"-C", repoPath, "rev-list", "--count", revisionRange}})
	if err != nil {
		return 0, WrapError("external_command_failed", fmt.Sprintf("Failed to count commits in %q.", revisionRange), err)
	}
	count := 0
	if _, err := fmt.Sscanf(strings.TrimSpace(result.Stdout), "%d", &count); err != nil {
		return 0, WrapError("external_command_failed", fmt.Sprintf("Unexpected commit count %q.", strings.TrimSpace(result.Stdout)), err)
	}
	return count, nil
}

func (g Git) DeleteBranch(ctx context.Context, repoPath, ref string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := g.Runner.Run(ctx, Command{Path: "git", Args: []string{"-C", repoPath, "branch", flag, "--", ref}})
	if err != nil {
		return WrapError("external_command_failed", fmt.Sprintf("Failed to delete managed Git branch %q.", ref), err)
	}
	return nil
}

func (g Git) RemoveWorktree(ctx context.Context, repoPath, path string, force bool) error {
	args := []string{"-C", repoPath, "worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	_, err := g.Runner.Run(ctx, Command{Path: "git", Args: args})
	if err != nil {
		return WrapError("external_command_failed", "Failed to remove Git worktree.", err)
	}
	return nil
}

func (g Git) Prune(ctx context.Context, repoPath string) error {
	_, err := g.Runner.Run(ctx, Command{Path: "git", Args: []string{"-C", repoPath, "worktree", "prune"}})
	return err
}

func canonical(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return filepath.Clean(resolved), nil
	}
	if os.IsNotExist(err) {
		return filepath.Clean(abs), nil
	}
	return "", err
}
