package gitworkspace

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Command struct {
	Path   string
	Args   []string
	Dir    string
	Env    []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

type Result struct {
	ExitCode       int
	Stdout, Stderr string
}

type Runner struct{}

func (Runner) Run(ctx context.Context, command Command) (Result, error) {
	cmd := exec.CommandContext(ctx, command.Path, command.Args...)
	cmd.Dir = command.Dir
	cmd.Env = append(os.Environ(), command.Env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	result := Result{ExitCode: 0, Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exit.ExitCode()
		} else {
			result.ExitCode = -1
		}
		return result, fmt.Errorf("%s %v exited with %d: %s", command.Path, command.Args, result.ExitCode, truncate(stderr.String(), 1000))
	}
	return result, nil
}

func (Runner) RunInteractive(ctx context.Context, command Command) error {
	cmd := exec.CommandContext(ctx, command.Path, command.Args...)
	cmd.Dir = command.Dir
	cmd.Env = append(os.Environ(), command.Env...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = command.Stdin, command.Stdout, command.Stderr
	return cmd.Run()
}

// ShellMarkerEnv carries the Workspace ID into the interactive subshell gws
// opens, so gws can tell whether a later command runs inside that subshell.
const ShellMarkerEnv = "GWS_WORKSPACE_SHELL"

// ShellWrappedEnv marks a subshell whose startup file installed the gws wrapper
// function. Only then can "gws finish" ask the shell to close itself.
const ShellWrappedEnv = "GWS_WORKSPACE_WRAPPED"

// ExitWorkspaceShell is the exit code the wrapper function watches for. The user
// runs "gws finish" inside the subshell; the binary reports this code, the
// wrapper turns it into "exit 0", and the interactive parent shell resumes in
// the directory it was in before gws opened the subshell. Interactive shells
// ignore SIGTERM/SIGHUP, so a signal cannot do this.
const ExitWorkspaceShell = 42

const workspaceShellFunction = `
gws() {
  command gws "$@"
  local gws_status=$?
  if [ "$gws_status" -eq %d ]; then
    exit 0
  fi
  return "$gws_status"
}
`

func (a *App) LaunchShell(ctx context.Context, workspace Workspace, stdin io.Reader, stdout, stderr io.Writer) error {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	fields := strings.Fields(shell)
	if len(fields) == 0 {
		fields = []string{"/bin/sh"}
	}
	shellPath, shellArgs := fields[0], fields[1:]
	if _, err := exec.LookPath(shellPath); err != nil {
		return NewError("shell_unavailable", fmt.Sprintf("Shell %q is not available.", shellPath))
	}
	env := append(os.Environ(), ShellMarkerEnv+"="+workspace.ID)
	cleanup, wrapped := prepareShellStartup(filepath.Base(shellPath), &env, &shellArgs)
	if cleanup != nil {
		defer cleanup()
	}
	if wrapped {
		env = append(env, ShellWrappedEnv+"=1")
	}
	err := (Runner{}).RunInteractive(ctx, Command{Path: shellPath, Args: shellArgs, Dir: workspace.WorktreePath, Env: env, Stdin: stdin, Stdout: stdout, Stderr: stderr})
	if err != nil {
		return WrapError("external_command_failed", "Workspace shell failed.", err)
	}
	return nil
}

// prepareShellStartup adds the gws wrapper function to an interactive shell by
// pointing it at a generated startup file that first sources the user's own
// configuration. Unsupported shells keep their plain startup and simply cannot
// close themselves, in which case finish prints the directory to cd into.
func prepareShellStartup(shellName string, env *[]string, args *[]string) (func(), bool) {
	source := fmt.Sprintf(workspaceShellFunction, ExitWorkspaceShell)
	switch shellName {
	case "zsh", "-zsh":
		zdotdir := os.Getenv("ZDOTDIR")
		if zdotdir == "" {
			zdotdir = os.Getenv("HOME")
		}
		dir, err := os.MkdirTemp("", "gws-zsh-")
		if err != nil {
			return nil, false
		}
		content := fmt.Sprintf("GWS_USER_ZDOTDIR=%q\nif [ -f \"$GWS_USER_ZDOTDIR/.zshrc\" ]; then source \"$GWS_USER_ZDOTDIR/.zshrc\"; fi\nunset GWS_USER_ZDOTDIR\n%s", zdotdir, source)
		if err := os.WriteFile(filepath.Join(dir, ".zshrc"), []byte(content), 0o600); err != nil {
			_ = os.RemoveAll(dir)
			return nil, false
		}
		*env = append(*env, "ZDOTDIR="+dir)
		return func() { _ = os.RemoveAll(dir) }, true
	case "bash", "-bash":
		rcfile := os.Getenv("GWS_USER_BASHRC")
		if rcfile == "" {
			rcfile = filepath.Join(os.Getenv("HOME"), ".bashrc")
		}
		dir, err := os.MkdirTemp("", "gws-bash-")
		if err != nil {
			return nil, false
		}
		path := filepath.Join(dir, "bashrc")
		content := fmt.Sprintf("if [ -f %q ]; then source %q; fi\n%s", rcfile, rcfile, source)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			_ = os.RemoveAll(dir)
			return nil, false
		}
		*args = append(*args, "--rcfile", path)
		return func() { _ = os.RemoveAll(dir) }, true
	default:
		return nil, false
	}
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max] + "..."
}
