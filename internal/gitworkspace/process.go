package gitworkspace

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	if err := (Runner{}).RunInteractive(ctx, Command{Path: shellPath, Args: shellArgs, Dir: workspace.WorktreePath, Stdin: stdin, Stdout: stdout, Stderr: stderr}); err != nil {
		return WrapError("external_command_failed", "Workspace shell failed.", err)
	}
	return nil
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max] + "..."
}
