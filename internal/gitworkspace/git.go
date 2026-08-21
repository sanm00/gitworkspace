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
	return Repository{Root: root, GitCommonDir: common, CurrentBranch: branch}, nil
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
