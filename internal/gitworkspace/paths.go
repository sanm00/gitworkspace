package gitworkspace

import (
	"fmt"
	"os"
	"path/filepath"
)

type Paths struct {
	StateDir     string
	WorktreesDir string
}

func NewPaths(repo Repository) (Paths, error) {
	stateDir := filepath.Join(repo.GitCommonDir, "gws")
	parent := filepath.Dir(repo.Root)
	worktreesDir := filepath.Join(parent, filepath.Base(repo.Root)+"-gws")
	return Paths{StateDir: stateDir, WorktreesDir: worktreesDir}, nil
}

func (p Paths) ConfigFile() string    { return filepath.Join(p.StateDir, "config.json") }
func (p Paths) WorkspacesDir() string { return filepath.Join(p.StateDir, "workspaces") }
func (p Paths) TasksDir() string      { return filepath.Join(p.StateDir, "tasks") }
func (p Paths) LocksDir() string      { return filepath.Join(p.StateDir, "locks") }
func (p Paths) LockFile() string      { return filepath.Join(p.LocksDir(), "state.lock") }
func (p Paths) WorkspaceFile(id string) string {
	return filepath.Join(p.WorkspacesDir(), id+".json")
}
func (p Paths) WorktreePath(workspaceID string) string {
	return filepath.Join(p.WorktreesDir, workspaceID)
}
func (p Paths) TaskFile(id string) string { return filepath.Join(p.TasksDir(), id+".md") }

func (p Paths) Ensure() error {
	for _, dir := range []string{p.StateDir, p.WorkspacesDir(), p.TasksDir(), p.LocksDir()} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create Git Workspace directory %s: %w", dir, err)
		}
	}
	return nil
}
