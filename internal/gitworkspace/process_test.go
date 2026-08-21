package gitworkspace

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaunchCheckoutShellUsesWorkspaceCWDAndShellArguments(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SHELL", "/bin/sh -c pwd")
	var stdout, stderr bytes.Buffer
	if err := (&App{}).LaunchCheckoutShell(context.Background(), Workspace{WorktreePath: dir}, bytes.NewReader(nil), &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	canonicalDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(stdout.String()) != canonicalDir {
		t.Fatalf("stdout=%q", stdout.String())
	}
}
