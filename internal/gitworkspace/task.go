package gitworkspace

import (
	"fmt"
	"os"
	"strings"
)

func RenderTask(workspace Workspace) string {
	typeName := string(workspace.Type)
	if len(typeName) > 0 {
		typeName = strings.ToUpper(typeName[:1]) + typeName[1:]
	}
	permission := "You may modify project files to complete this task."
	focus := "Implement the requested change, run relevant tests, and report the result."
	if workspace.Type == WorkspaceReview {
		permission = "Do not modify project files unless explicitly requested by the developer."
		focus = "Compare target against base. Prioritize concrete defects and report findings ordered by severity with file and line references."
	} else if workspace.Type == WorkspaceTest {
		permission = "Do not modify project files unless explicitly requested by the developer."
		focus = "Validate the target, run relevant tests, and report failures with reproducible details."
	}
	base := firstNonEmpty(workspace.Source.BaseRef, workspace.Source.FromRef)
	if base == "" {
		base = "(none)"
	}
	baseCommit := workspace.Source.BaseCommit
	if baseCommit == "" {
		baseCommit = "-"
	}
	return fmt.Sprintf("# Git Workspace Task\n\nType: %s\nWorkspace: %s\nTarget: %s\nBase: %s\nBase commit: %s\nWorktree: %s\n\n## Instructions\n\n%s\n%s\n", typeName, workspace.Name, workspace.Source.Ref, base, baseCommit, workspace.WorktreePath, focus, permission)
}

func WriteTask(path, content string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return WrapError("internal_error", "Failed to write Workspace task file.", err)
	}
	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		return WrapError("internal_error", "Failed to write Workspace task file.", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
