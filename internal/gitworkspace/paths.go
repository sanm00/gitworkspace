package gitworkspace

import (
	"fmt"
	"os"
	"path/filepath"
)

type Paths struct{ Home string }

func DefaultPaths() (Paths, error) {
	if value := os.Getenv("GIT_WORKSPACE_HOME"); value != "" {
		absolute, err := filepath.Abs(value)
		return Paths{Home: absolute}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve user home: %w", err)
	}
	return Paths{Home: filepath.Join(home, ".git-workspace")}, nil
}

func (p Paths) ProjectsDir() string   { return filepath.Join(p.Home, "projects") }
func (p Paths) WorkspacesDir() string { return filepath.Join(p.Home, "workspaces") }
func (p Paths) WorktreesDir() string  { return filepath.Join(p.Home, "worktrees") }
func (p Paths) TasksDir() string      { return filepath.Join(p.Home, "tasks") }
func (p Paths) LocksDir() string      { return filepath.Join(p.Home, "locks") }
func (p Paths) LockFile() string      { return filepath.Join(p.LocksDir(), "state.lock") }
func (p Paths) ProjectFile(id string) string {
	return filepath.Join(p.ProjectsDir(), id+".json")
}
func (p Paths) WorkspaceFile(id string) string {
	return filepath.Join(p.WorkspacesDir(), id+".json")
}
func (p Paths) WorktreePath(projectID, workspaceID string) string {
	return filepath.Join(p.WorktreesDir(), projectID, workspaceID)
}
func (p Paths) TaskFile(id string) string { return filepath.Join(p.TasksDir(), id+".md") }

func (p Paths) Ensure() error {
	for _, dir := range []string{p.Home, p.ProjectsDir(), p.WorkspacesDir(), p.WorktreesDir(), p.TasksDir(), p.LocksDir()} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create Git Workspace directory %s: %w", dir, err)
		}
	}
	return nil
}
