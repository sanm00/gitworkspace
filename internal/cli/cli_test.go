package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func setGitIdentityEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
}

func mustGitOutput(t *testing.T, repo string, args ...string) []byte {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return out
}

func TestJSONErrorEnvelope(t *testing.T) {
	t.Setenv("GIT_WORKSPACE_HOME", t.TempDir())
	var out, stderr bytes.Buffer
	code := Run([]string{"show", "missing", "--json"}, bytes.NewReader(nil), &out, &stderr, "test")
	if code == 0 {
		t.Fatal("expected failure")
	}
	var value map[string]any
	if err := json.Unmarshal(out.Bytes(), &value); err != nil {
		t.Fatalf("invalid json: %v %s", err, out.String())
	}
	if value["schema_version"].(float64) != 2 || value["error"] == nil {
		t.Fatalf("unexpected envelope: %#v", value)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%s", stderr.String())
	}
}

func TestUnknownOptionIsRejected(t *testing.T) {
	var out, stderr bytes.Buffer
	code := Run([]string{"ls", "--porject", "demo"}, bytes.NewReader(nil), &out, &stderr, "test")
	if code == 0 || !bytes.Contains(stderr.Bytes(), []byte("Unknown option")) {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.HasPrefix(stderr.String(), "! ") || strings.Contains(stderr.String(), "Error:") {
		t.Fatalf("human error does not use the ! marker: %q", stderr.String())
	}
}

func TestErrorMarkerUsesRedWhenColorIsSupported(t *testing.T) {
	if got := formatErrorMarker(true); got != "\x1b[31m!\x1b[0m" {
		t.Fatalf("unexpected colored marker: %q", got)
	}
	if got := formatErrorMarker(false); got != "!" {
		t.Fatalf("unexpected plain marker: %q", got)
	}
}

func TestMissingArgumentsIncludeActionableUsage(t *testing.T) {
	t.Setenv("GIT_WORKSPACE_HOME", t.TempDir())
	for _, test := range []struct {
		args     []string
		expected string
	}{
		{args: []string{"finish"}, expected: "Usage: gws finish <workspace-id>"},
		{args: []string{"rm"}, expected: "Usage: gws rm <workspace-id>"},
		{args: []string{"dev"}, expected: "Usage: gws dev <branch>"},
		{args: []string{"project"}, expected: "Usage: gws project ls|show|rm"},
		{args: []string{"project", "rm"}, expected: "Usage: gws project rm <project-id>"},
	} {
		var out, stderr bytes.Buffer
		code := Run(test.args, bytes.NewReader(nil), &out, &stderr, "test")
		if code == 0 || !strings.Contains(stderr.String(), "Missing") || !strings.Contains(stderr.String(), test.expected) {
			t.Fatalf("args=%v code=%d stdout=%s stderr=%s", test.args, code, out.String(), stderr.String())
		}
	}
}

func TestRemovedProjectRootOptionsAreRejected(t *testing.T) {
	for _, option := range []string{"--use-parent", "--init-here"} {
		var out, stderr bytes.Buffer
		code := Run([]string{"init", option}, bytes.NewReader(nil), &out, &stderr, "test")
		if code == 0 || !strings.Contains(stderr.String(), "Unknown option") {
			t.Fatalf("option=%s code=%d stdout=%s stderr=%s", option, code, out.String(), stderr.String())
		}
	}
}

func TestCapabilitiesJSON(t *testing.T) {
	t.Setenv("GIT_WORKSPACE_HOME", t.TempDir())
	var out, stderr bytes.Buffer
	if code := Run([]string{"capabilities", "--json"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var value map[string]any
	if err := json.Unmarshal(out.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	data := value["data"].(map[string]any)
	if data["protocol_version"].(float64) != 1 || data["state_version"].(float64) != 1 || data["backend"] != "git-worktree" {
		t.Fatalf("capabilities=%#v", value)
	}
}

func TestHelpIncludesApplyAndFinish(t *testing.T) {
	var out, stderr bytes.Buffer
	if code := Run([]string{"--help"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(out.String(), "gws apply") || !strings.Contains(out.String(), "gws finish") {
		t.Fatalf("help=%s", out.String())
	}
	for _, removed := range []string{"gws agent", "gws code", "gws shell", "gws open"} {
		if strings.Contains(out.String(), removed) {
			t.Fatalf("help still contains removed command %q: %s", removed, out.String())
		}
	}
	if !strings.Contains(out.String(), "gws new [NAME]") || strings.Contains(out.String(), "gws new NAME") {
		t.Fatalf("help does not show the optional Workspace name: %s", out.String())
	}
	if !strings.Contains(out.String(), "gws project ls|show|rm") || strings.Contains(out.String(), "gws project ls|show|remove") {
		t.Fatalf("help does not use the project rm subcommand: %s", out.String())
	}
	if strings.Contains(out.String(), "--use-parent") || strings.Contains(out.String(), "--init-here") {
		t.Fatalf("help still advertises removed repository-selection flags: %s", out.String())
	}
}

func TestRemovedToolCommandsAreRejected(t *testing.T) {
	t.Setenv("GIT_WORKSPACE_HOME", t.TempDir())
	for _, command := range []string{"agent", "code", "shell", "open"} {
		var out, stderr bytes.Buffer
		if code := Run([]string{command}, bytes.NewReader(nil), &out, &stderr, "test"); code == 0 || !strings.Contains(stderr.String(), "Unknown command") {
			t.Fatalf("command=%s code=%d stdout=%s stderr=%s", command, code, out.String(), stderr.String())
		}
	}
}

func TestDoctorErrorUsesCanonicalCommandName(t *testing.T) {
	t.Setenv("GIT_WORKSPACE_HOME", t.TempDir())
	originalCWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(originalCWD) }()
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}

	var out, stderr bytes.Buffer
	if code := Run([]string{"doctor"}, bytes.NewReader(nil), &out, &stderr, "test"); code == 0 {
		t.Fatalf("expected doctor to fail outside a registered repository: stdout=%s", out.String())
	}
	if !strings.Contains(stderr.String(), "gws init") {
		t.Fatalf("doctor error does not use the canonical command name: %s", stderr.String())
	}
}

func TestDoctorJSONFailureUsesErrorEnvelope(t *testing.T) {
	repo := t.TempDir()
	if out, err := exec.Command("git", "init", "-b", "main", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	originalCWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(originalCWD) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_WORKSPACE_HOME", filepath.Join(t.TempDir(), "home"))

	var out, stderr bytes.Buffer
	if code := Run([]string{"init", "--name", "Demo"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("init code=%d stderr=%s", code, stderr.String())
	}
	if err := os.WriteFile(filepath.Join(repo, ".git-workspace.yaml"), []byte("version: invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	stderr.Reset()
	if code := Run([]string{"doctor", "--json"}, bytes.NewReader(nil), &out, &stderr, "test"); code == 0 {
		t.Fatalf("doctor unexpectedly succeeded: %s", out.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON error envelope: %v output=%s", err, out.String())
	}
	errorBody := envelope["error"].(map[string]any)
	if errorBody["code"] != "doctor_failed" || envelope["data"] != nil || stderr.Len() != 0 {
		t.Fatalf("unexpected doctor envelope=%#v stderr=%q", envelope, stderr.String())
	}
}

func TestNewWithoutNameUsesGeneratedWorkspaceName(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if out, err := exec.Command("git", "init", "-b", "main", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	for _, args := range [][]string{
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"commit", "--allow-empty", "-m", "initial"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}

	originalCWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(originalCWD) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GIT_WORKSPACE_HOME", filepath.Join(t.TempDir(), "home"))
	var out, stderr bytes.Buffer
	if code := Run([]string{"new"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("new code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
	if !strings.Contains(out.String(), "Work item: workspace") || !strings.Contains(out.String(), "Managed branch: gws/workspace-") {
		t.Fatalf("new did not generate a usable Workspace name: %s", out.String())
	}
}

func TestNewCreatesEmptyInitialCommitWithoutCommittingStagedChanges(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if out, err := exec.Command("git", "init", "-b", "main", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(repo, "staged.txt"), []byte("keep staged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "add", "staged.txt").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}

	originalCWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(originalCWD) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GIT_WORKSPACE_HOME", filepath.Join(t.TempDir(), "home"))
	setGitIdentityEnv(t)
	var out, stderr bytes.Buffer
	if code := Run([]string{"new", "test"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("new code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
	if !strings.Contains(out.String(), "Initial commit: created automatically (empty)") {
		t.Fatalf("new did not report the automatic initial commit: %s", out.String())
	}
	if tree := strings.TrimSpace(string(mustGitOutput(t, repo, "ls-tree", "-r", "--name-only", "HEAD"))); tree != "" {
		t.Fatalf("initial commit is not empty: %q", tree)
	}
	if status := string(mustGitOutput(t, repo, "status", "--porcelain")); !strings.Contains(status, "A  staged.txt") {
		t.Fatalf("staged changes were not preserved: %q", status)
	}
}

func TestInitInitializesNonGitDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".git-workspace.yaml"), []byte("version: 1\nproject:\n  name: Demo\n  default_branch: main\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	originalCWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(originalCWD) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GIT_WORKSPACE_HOME", filepath.Join(t.TempDir(), "home"))
	var out, stderr bytes.Buffer
	if code := Run([]string{"init"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("init code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(out.String(), "Registered Project Demo") {
		t.Fatalf("init output=%s", out.String())
	}
	if got, err := exec.Command("git", "-C", dir, "symbolic-ref", "--short", "HEAD").CombinedOutput(); err != nil || strings.TrimSpace(string(got)) != "main" {
		t.Fatalf("initialized branch=%q err=%v", got, err)
	}
}

func TestInitUsesCurrentDirectoryInsideParentRepository(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "parent")
	if out, err := exec.Command("git", "init", "-b", "main", parent).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	child := filepath.Join(parent, "child")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, ".git-workspace.yaml"), []byte("version: 1\nproject:\n  name: Child\n  default_branch: main\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	originalCWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(originalCWD) }()
	if err := os.Chdir(child); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GIT_WORKSPACE_HOME", filepath.Join(t.TempDir(), "home"))
	var out, stderr bytes.Buffer
	if code := Run([]string{"init", "--json"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 || stderr.Len() != 0 {
		t.Fatalf("init code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(child, ".git")); err != nil {
		t.Fatalf("init did not initialize the current directory: %v", err)
	}
	var value map[string]any
	if err := json.Unmarshal(out.Bytes(), &value); err != nil {
		t.Fatalf("invalid json: %v %s", err, out.String())
	}
	repoPath := value["data"].(map[string]any)["project"].(map[string]any)["repo_path"].(string)
	canonicalChild, err := filepath.EvalSymlinks(child)
	if err != nil {
		t.Fatal(err)
	}
	if repoPath != canonicalChild {
		t.Fatalf("registered repository=%q want %q", repoPath, canonicalChild)
	}
}

func TestNewAutomaticallyRegistersCurrentRepository(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if out, err := exec.Command("git", "init", "-b", "main", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	for _, args := range [][]string{
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"commit", "--allow-empty", "-m", "initial"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}

	originalCWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(originalCWD) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GIT_WORKSPACE_HOME", filepath.Join(t.TempDir(), "home"))
	var out, stderr bytes.Buffer
	if code := Run([]string{"new", "test"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("new code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(out.String(), "Created Workspace") {
		t.Fatalf("new output=%s", out.String())
	}

	out.Reset()
	stderr.Reset()
	if code := Run([]string{"project", "ls"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 || !strings.Contains(out.String(), repo) {
		t.Fatalf("project ls code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
}

func TestNewUsesCurrentDirectoryInsideRegisteredParentRepository(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "parent")
	if out, err := exec.Command("git", "init", "-b", "main", parent).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	child := filepath.Join(parent, "child")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}

	originalCWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(originalCWD) }()
	if err := os.Chdir(parent); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GIT_WORKSPACE_HOME", filepath.Join(t.TempDir(), "home"))
	setGitIdentityEnv(t)
	var out, stderr bytes.Buffer
	if code := Run([]string{"init"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("parent init code=%d stderr=%s", code, stderr.String())
	}
	if err := os.Chdir(child); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	stderr.Reset()
	if code := Run([]string{"new"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("new code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(child, ".git")); err != nil {
		t.Fatalf("new did not initialize the current directory: %v", err)
	}
	if !strings.Contains(out.String(), "Created Workspace") || !strings.Contains(out.String(), "Initial commit: created automatically (empty)") {
		t.Fatalf("new output=%s", out.String())
	}
}

func TestNewRejectsProjectSelector(t *testing.T) {
	t.Setenv("GIT_WORKSPACE_HOME", t.TempDir())
	var out, stderr bytes.Buffer
	code := Run([]string{"new", "feature/avatar", "--project", "demo"}, bytes.NewReader(nil), &out, &stderr, "test")
	if code == 0 || !strings.Contains(stderr.String(), "does not accept --project") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
}

func TestProjectRmReplacesProjectRemove(t *testing.T) {
	t.Setenv("GIT_WORKSPACE_HOME", t.TempDir())
	var out, stderr bytes.Buffer
	if code := Run([]string{"project", "remove", "demo"}, bytes.NewReader(nil), &out, &stderr, "test"); code == 0 || !strings.Contains(stderr.String(), "Unknown project subcommand") {
		t.Fatalf("legacy remove subcommand was not rejected: code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
	out.Reset()
	stderr.Reset()
	if code := Run([]string{"project", "rm", "missing"}, bytes.NewReader(nil), &out, &stderr, "test"); code == 0 || strings.Contains(stderr.String(), "Unknown project subcommand") {
		t.Fatalf("project rm was not recognized: code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
}

func TestNewCheckoutAndCurrentDirectoryCommands(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if out, err := exec.Command("git", "init", "-b", "main", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	for _, args := range [][]string{{"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("demo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "README.md"}, {"commit", "-m", "initial"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}

	originalCWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(originalCWD) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_WORKSPACE_HOME", filepath.Join(t.TempDir(), "home"))
	var out, stderr bytes.Buffer
	if code := Run([]string{"init", "--name", "Demo", "--json"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("init code=%d stderr=%s", code, stderr.String())
	}
	var initEnvelope map[string]any
	if err := json.Unmarshal(out.Bytes(), &initEnvelope); err != nil {
		t.Fatalf("json init: %v output=%s", err, out.String())
	}
	projectID := initEnvelope["data"].(map[string]any)["project"].(map[string]any)["id"].(string)
	if branchOut, branchErr := exec.Command("git", "-C", repo, "branch", "feature/cli").CombinedOutput(); branchErr != nil {
		t.Fatalf("create existing business branch: %v %s", branchErr, branchOut)
	}
	out.Reset()
	stderr.Reset()
	if code := Run([]string{"new", "feature/cli"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("new code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(out.String(), "Managed branch: gws/") {
		t.Fatalf("new did not report a managed branch: %s", out.String())
	}
	if !strings.Contains(out.String(), "Work item: feature-cli") {
		t.Fatalf("new lost the work-item prefix: %s", out.String())
	}
	fields := strings.Fields(out.String())
	if len(fields) < 3 {
		t.Fatalf("new output=%q", out.String())
	}
	workspaceID := fields[2]
	workspacePath := filepath.Join(os.Getenv("GIT_WORKSPACE_HOME"), "worktrees", projectID, workspaceID)
	out.Reset()
	stderr.Reset()
	if code := Run([]string{"new", "plugin/json", "--json"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("json new code=%d stderr=%s", code, stderr.String())
	}
	var newEnvelope map[string]any
	if err := json.Unmarshal(out.Bytes(), &newEnvelope); err != nil {
		t.Fatalf("json new: %v output=%s", err, out.String())
	}
	pluginWorkspaceID := newEnvelope["data"].(map[string]any)["id"].(string)
	out.Reset()
	stderr.Reset()
	if code := Run([]string{"rm", pluginWorkspaceID, "--json"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("json rm code=%d stderr=%s", code, stderr.String())
	}
	var removeEnvelope map[string]any
	if err := json.Unmarshal(out.Bytes(), &removeEnvelope); err != nil || removeEnvelope["data"].(map[string]any)["removed"] != true {
		t.Fatalf("json rm: err=%v output=%s", err, out.String())
	}
	out.Reset()
	stderr.Reset()
	if code := Run([]string{"ls"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("ls code=%d stderr=%s", code, stderr.String())
	}
	for _, expected := range []string{"NAME", "ID", "REPOSITORY", "BRANCH", "FROM", "STATUS", "feature-cli", workspaceID, "Demo", "gws/", "main", "ready"} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("ls missing %q: %s", expected, out.String())
		}
	}
	out.Reset()
	stderr.Reset()
	if code := Run([]string{"show", workspaceID}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("show code=%d stderr=%s", code, stderr.String())
	}
	canonicalRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Workspace: feature-cli", "ID: " + workspaceID, "Repository: Demo", "Repository path: " + canonicalRepo, "Branch: gws/", "From: main", "Managed branch: true", "Status: ready", "Worktree: " + workspacePath, "Task:"} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("show missing %q: %s", expected, out.String())
		}
	}

	if err := os.Chdir(workspacePath); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	stderr.Reset()
	if code := Run([]string{"status"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 || !strings.Contains(out.String(), workspaceID) {
		t.Fatalf("status code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
	out.Reset()
	stderr.Reset()
	if code := Run([]string{"status", "--json"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("json status code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
	var statusEnvelope map[string]any
	if err := json.Unmarshal(out.Bytes(), &statusEnvelope); err != nil {
		t.Fatal(err)
	}
	data := statusEnvelope["data"].(map[string]any)
	if statusEnvelope["schema_version"].(float64) != 2 || data["tools"] != nil {
		t.Fatalf("unexpected status schema: %#v", statusEnvelope)
	}
	gitStatus := data["git"].(map[string]any)
	if dirty, ok := gitStatus["dirty"].(bool); !ok || dirty {
		t.Fatalf("expected known clean JSON status, got %#v", gitStatus)
	}

	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", "/bin/pwd")
	out.Reset()
	stderr.Reset()
	if code := Run([]string{"checkout", workspaceID}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 || !strings.Contains(out.String(), workspacePath) {
		t.Fatalf("checkout code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
}
