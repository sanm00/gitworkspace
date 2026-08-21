package gitworkspace

import (
	"path/filepath"
	"testing"
)

func TestDefaultPathsUsesGitWorkspaceHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_WORKSPACE_HOME", "")
	paths, err := DefaultPaths()
	if err != nil || paths.Home != filepath.Join(home, ".git-workspace") {
		t.Fatalf("paths=%#v err=%v", paths, err)
	}
}

func TestDefaultPathsUsesEnvironmentOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "state")
	t.Setenv("GIT_WORKSPACE_HOME", override)
	paths, err := DefaultPaths()
	if err != nil || paths.Home != override {
		t.Fatalf("paths=%#v err=%v", paths, err)
	}
}
