package gitworkspace

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaunchShellUsesWorkspaceCWDAndShellArguments(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SHELL", "/bin/sh -c pwd")
	var stdout, stderr bytes.Buffer
	if err := (&App{}).LaunchShell(context.Background(), Workspace{WorktreePath: dir}, bytes.NewReader(nil), &stdout, &stderr); err != nil {
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

func TestPrepareShellStartupInstallsWrapperForZsh(t *testing.T) {
	if _, err := os.Stat("/bin/zsh"); err != nil {
		t.Skip("zsh is not installed")
	}
	home := t.TempDir()
	userrc := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(userrc, []byte("export FROM_USER_RC=yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", "")
	env := os.Environ()
	args := []string{}
	cleanup, wrapped := prepareShellStartup("zsh", &env, &args)
	if !wrapped || cleanup == nil {
		t.Fatalf("zsh should be wrapped, got wrapped=%t cleanup=%t", wrapped, cleanup != nil)
	}
	defer cleanup()
	zdotdir := ""
	for _, entry := range env {
		if strings.HasPrefix(entry, "ZDOTDIR=") {
			zdotdir = strings.TrimPrefix(entry, "ZDOTDIR=")
		}
	}
	if zdotdir == "" {
		t.Fatal("ZDOTDIR was not set for the wrapped zsh")
	}

	fakeBin := t.TempDir()
	fake := filepath.Join(fakeBin, "gws")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 42\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	script := strings.Join([]string{
		`echo "user-rc=$FROM_USER_RC"`,
		`echo "type=$(whence -w gws)"`,
		`gws finish`,
		`echo unreachable`,
	}, "\n") + "\n"
	path := fakeBin + string(os.PathListSeparator) + os.Getenv("PATH")
	err := (Runner{}).RunInteractive(context.Background(), Command{
		Path:   "/bin/zsh",
		Args:   []string{"-i"},
		Env:    []string{"ZDOTDIR=" + zdotdir, "PATH=" + path},
		Stdin:  strings.NewReader(script),
		Stdout: &out,
		Stderr: io.Discard,
	})
	if err != nil {
		t.Fatalf("zsh run: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "user-rc=yes") {
		t.Fatalf("wrapper did not source the user's rc: %s", text)
	}
	if !strings.Contains(text, "type=gws: function") {
		t.Fatalf("wrapper function missing: %s", text)
	}
	if strings.Contains(text, "unreachable") {
		t.Fatalf("shell kept going after the sentinel exit code: %s", text)
	}
}

func TestPrepareShellStartupFallsBackForUnknownShell(t *testing.T) {
	env := os.Environ()
	args := []string{}
	cleanup, wrapped := prepareShellStartup("sh", &env, &args)
	if wrapped || cleanup != nil {
		t.Fatalf("sh must not be wrapped: wrapped=%t", wrapped)
	}
}
