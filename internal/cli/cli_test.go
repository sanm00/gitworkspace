package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanm00/gitworkspace/internal/gitworkspace"
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

func newCLITestRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if out, err := exec.Command("git", "init", "-b", "main", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	for _, args := range [][]string{
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
	} {
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
	return repo
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
}

func TestJSONErrorEnvelopeOutsideRepository(t *testing.T) {
	chdir(t, t.TempDir())
	var out, stderr bytes.Buffer
	code := Run([]string{"ls", "--json"}, bytes.NewReader(nil), &out, &stderr, "test")
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
	if value["error"].(map[string]any)["code"] != "repository_not_found" {
		t.Fatalf("unexpected error code: %#v", value["error"])
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%s", stderr.String())
	}
}

func TestUnknownOptionIsRejected(t *testing.T) {
	chdir(t, newCLITestRepo(t))
	var out, stderr bytes.Buffer
	code := Run([]string{"ls", "--porject", "demo"}, bytes.NewReader(nil), &out, &stderr, "test")
	if code == 0 || !bytes.Contains(stderr.Bytes(), []byte("Unknown option")) {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.HasPrefix(stderr.String(), "! ") || strings.Contains(stderr.String(), "Error:") {
		t.Fatalf("human error does not use the ! marker: %q", stderr.String())
	}
}

func TestForceIsOnlyAcceptedByRm(t *testing.T) {
	chdir(t, newCLITestRepo(t))
	for _, args := range [][]string{{"apply", "--force", "main"}, {"finish", "x", "--force", "main"}} {
		var out, stderr bytes.Buffer
		code := Run(args, bytes.NewReader(nil), &out, &stderr, "test")
		if code == 0 || !strings.Contains(stderr.String(), "--force is supported only by gws rm") {
			t.Fatalf("args=%v code=%d stderr=%s", args, code, stderr.String())
		}
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
	chdir(t, newCLITestRepo(t))
	for _, test := range []struct {
		args     []string
		expected string
	}{
		{args: []string{"rm"}, expected: "Usage: gws rm <workspace-id>"},
	} {
		var out, stderr bytes.Buffer
		code := Run(test.args, bytes.NewReader(nil), &out, &stderr, "test")
		if code == 0 || !strings.Contains(stderr.String(), "Missing") || !strings.Contains(stderr.String(), test.expected) {
			t.Fatalf("args=%v code=%d stdout=%s stderr=%s", test.args, code, out.String(), stderr.String())
		}
	}
}

func TestRemovedCommandsAreRejected(t *testing.T) {
	chdir(t, newCLITestRepo(t))
	for _, command := range []string{"init", "checkout", "dev", "review", "fix", "test", "project", "agent", "code", "shell", "open"} {
		var out, stderr bytes.Buffer
		if code := Run([]string{command}, bytes.NewReader(nil), &out, &stderr, "test"); code == 0 || !strings.Contains(stderr.String(), "Unknown command") {
			t.Fatalf("command=%s code=%d stdout=%s stderr=%s", command, code, out.String(), stderr.String())
		}
	}
}

func TestRemovedOptionsAreRejected(t *testing.T) {
	chdir(t, newCLITestRepo(t))
	for _, args := range [][]string{{"new", "--from", "main"}, {"new", "--project", "demo"}, {"ls", "--project", "demo"}} {
		var out, stderr bytes.Buffer
		if code := Run(args, bytes.NewReader(nil), &out, &stderr, "test"); code == 0 || !strings.Contains(stderr.String(), "Unknown option") {
			t.Fatalf("args=%v code=%d stdout=%s stderr=%s", args, code, out.String(), stderr.String())
		}
	}
}

func TestCapabilitiesJSON(t *testing.T) {
	var out, stderr bytes.Buffer
	if code := Run([]string{"capabilities", "--json"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var value map[string]any
	if err := json.Unmarshal(out.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	data := value["data"].(map[string]any)
	if data["protocol_version"].(float64) != 2 || data["state_version"].(float64) != 1 || data["backend"] != "git-worktree" {
		t.Fatalf("capabilities=%#v", value)
	}
	features := data["features"].([]any)
	for _, feature := range features {
		if feature == "project-registry" {
			t.Fatalf("project-registry is still advertised: %#v", features)
		}
	}
	interactive := data["interactive_commands"].([]any)
	if len(interactive) == 0 || interactive[0] != "use" {
		t.Fatalf("interactive commands=%#v", interactive)
	}
}

func TestCapabilitiesWorksWithoutRepository(t *testing.T) {
	chdir(t, t.TempDir())
	var out, stderr bytes.Buffer
	if code := Run([]string{"capabilities"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(out.String(), "Plugin protocol: 2") {
		t.Fatalf("output=%s", out.String())
	}
}

func TestHelpShowsFinalCommandSurface(t *testing.T) {
	var out, stderr bytes.Buffer
	if code := Run([]string{"--help"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	text := out.String()
	for _, expected := range []string{"gws new [NAME]", "gws use [WORKSPACE]", "gws apply [--into BRANCH]", "gws finish WORKSPACE [--into BRANCH]", "gws rm WORKSPACE [--force]", "gws diff [WORKSPACE]", "gws log [WORKSPACE]", "gws commit --message MESSAGE"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("help missing %q:\n%s", expected, text)
		}
	}
	for _, removed := range []string{"gws init", "gws checkout", "gws dev", "gws review", "gws fix", "gws test", "gws project", "--from", "--project", "--force BRANCH"} {
		if strings.Contains(text, removed) {
			t.Fatalf("help still advertises %q:\n%s", removed, text)
		}
	}
}

func TestDoctorOutsideRepository(t *testing.T) {
	chdir(t, t.TempDir())
	var out, stderr bytes.Buffer
	if code := Run([]string{"doctor"}, bytes.NewReader(nil), &out, &stderr, "test"); code == 0 || !strings.Contains(stderr.String(), "not inside a Git repository") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
}

func TestDoctorInsideRepository(t *testing.T) {
	chdir(t, newCLITestRepo(t))
	var out, stderr bytes.Buffer
	if code := Run([]string{"doctor"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
	for _, expected := range []string{"git:", "config:", "state:", "workspaces:"} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("doctor output missing %q: %s", expected, out.String())
		}
	}
}

func TestNewWithoutNameEntersNoShellInNonTTY(t *testing.T) {
	repo := newCLITestRepo(t)
	chdir(t, repo)
	setGitIdentityEnv(t)
	var out, stderr bytes.Buffer
	if code := Run([]string{"new"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("new code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
	text := out.String()
	if !strings.Contains(text, "Created Workspace") || !strings.Contains(text, "Based on: main") || !strings.Contains(text, "Branch: gws/main-") {
		t.Fatalf("new output=%s", text)
	}
	if !strings.Contains(text, "Next:\n  gws use ") {
		t.Fatalf("non-tty new did not print the use hint: %s", text)
	}
	id := workspaceIDFromOutput(t, text)
	if _, err := os.Stat(filepath.Join(repo, ".git", "gws", "worktrees", id)); err != nil {
		t.Fatalf("worktree was not created inside the Git directory: %v", err)
	}
}

func TestNewJSONDoesNotEnterShell(t *testing.T) {
	repo := newCLITestRepo(t)
	chdir(t, repo)
	mustGitOutput(t, repo, "branch", "feature/x")
	var out, stderr bytes.Buffer
	if code := Run([]string{"new", "--base", "feature/x", "--type", "review", "--json"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	data := envelope["data"].(map[string]any)
	source := data["source"].(map[string]any)
	if source["detached"] != true || source["base_ref"] != "feature/x" {
		t.Fatalf("source=%#v", source)
	}
	if source["base_commit"] == nil || source["base_commit"] == "" {
		t.Fatalf("base commit missing: %#v", source)
	}
	if data["project_id"] != nil {
		t.Fatalf("project_id must be gone: %#v", data)
	}
}

func TestNewRejectsUnknownType(t *testing.T) {
	chdir(t, newCLITestRepo(t))
	var out, stderr bytes.Buffer
	if code := Run([]string{"new", "--type", "nope"}, bytes.NewReader(nil), &out, &stderr, "test"); code == 0 || !strings.Contains(stderr.String(), "Unknown workspace type") {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}

func TestNewFetchRequiresReviewOrTestType(t *testing.T) {
	chdir(t, newCLITestRepo(t))
	var out, stderr bytes.Buffer
	if code := Run([]string{"new", "--fetch"}, bytes.NewReader(nil), &out, &stderr, "test"); code == 0 || !strings.Contains(stderr.String(), "--fetch is supported only") {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}

func TestNewFailsOutsideRepository(t *testing.T) {
	chdir(t, t.TempDir())
	var out, stderr bytes.Buffer
	if code := Run([]string{"new"}, bytes.NewReader(nil), &out, &stderr, "test"); code == 0 || !strings.Contains(stderr.String(), "not inside a Git repository") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
}

func TestUseRejectsJSON(t *testing.T) {
	chdir(t, newCLITestRepo(t))
	for _, args := range [][]string{{"use", "x", "--json"}, {"use", "--json"}} {
		var out, stderr bytes.Buffer
		if code := Run(args, bytes.NewReader(nil), &out, &stderr, "test"); code == 0 || !strings.Contains(stderr.String(), "use is interactive") {
			t.Fatalf("args=%v code=%d stdout=%s stderr=%s", args, code, out.String(), stderr.String())
		}
	}
}

func TestUseLaunchesShellInWorkspaceDirectory(t *testing.T) {
	repo := newCLITestRepo(t)
	chdir(t, repo)
	var out, stderr bytes.Buffer
	if code := Run([]string{"new", "demo", "--no-enter"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("new code=%d stderr=%s", code, stderr.String())
	}
	id := workspaceIDFromOutput(t, out.String())
	worktree := filepath.Join(repo, ".git", "gws", "worktrees", id)
	t.Setenv("SHELL", "/bin/pwd")
	out.Reset()
	stderr.Reset()
	if code := Run([]string{"use", id}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("use code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
	if !strings.Contains(out.String(), worktree) {
		t.Fatalf("use did not enter the worktree: %s", out.String())
	}
}

func TestFinishInsideWrappedShellReturnsExitSentinel(t *testing.T) {
	repo := newCLITestRepo(t)
	chdir(t, repo)
	setGitIdentityEnv(t)
	var out, stderr bytes.Buffer
	if code := Run([]string{"new", "wrapped", "--no-enter"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("new code=%d stderr=%s", code, stderr.String())
	}
	id := workspaceIDFromOutput(t, out.String())
	chdir(t, filepath.Join(repo, ".git", "gws", "worktrees", id))
	t.Setenv(gitworkspace.ShellMarkerEnv, id)
	t.Setenv(gitworkspace.ShellWrappedEnv, "1")
	out.Reset()
	stderr.Reset()
	code := Run([]string{"finish"}, bytes.NewReader(nil), &out, &stderr, "test")
	if code != gitworkspace.ExitWorkspaceShell {
		t.Fatalf("wrapped shell finish code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
	text := out.String()
	if !strings.Contains(text, "Closing the Workspace shell") || strings.Contains(text, "Run: cd ") {
		t.Fatalf("finish output=%s", text)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "gws", "worktrees", id)); !os.IsNotExist(err) {
		t.Fatalf("worktree survived: %v", err)
	}
	if got := strings.TrimSpace(string(mustGitOutput(t, repo, "branch", "--list", "gws/*"))); got != "" {
		t.Fatalf("managed branch survived: %q", got)
	}
}

func TestFinishInsidePlainShellPrintsCDHint(t *testing.T) {
	repo := newCLITestRepo(t)
	chdir(t, repo)
	setGitIdentityEnv(t)
	var out, stderr bytes.Buffer
	if code := Run([]string{"new", "hint2", "--no-enter"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("new code=%d stderr=%s", code, stderr.String())
	}
	id := workspaceIDFromOutput(t, out.String())
	worktree := filepath.Join(repo, ".git", "gws", "worktrees", id)
	chdir(t, worktree)
	t.Setenv(gitworkspace.ShellMarkerEnv, id)
	t.Setenv(gitworkspace.ShellWrappedEnv, "")
	out.Reset()
	stderr.Reset()
	if code := Run([]string{"finish"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("finish code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(out.String(), "Run: cd ") {
		t.Fatalf("unwraped shell must get a cd hint: %s", out.String())
	}
}

// buildGWS compiles the real binary so a launched subshell can invoke gws the
// way a user would.
func buildGWS(t *testing.T) string {
	t.Helper()
	here, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(filepath.Join(here, "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repository root not found from %s: %v", here, err)
	}
	binary := filepath.Join(t.TempDir(), "gws")
	cmd := exec.Command("go", "build", "-o", binary, "./cmd/gws")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v %s", err, out)
	}
	return binary
}

func TestApplyRejectsPositionalArgument(t *testing.T) {
	chdir(t, newCLITestRepo(t))
	var out, stderr bytes.Buffer
	if code := Run([]string{"apply", "ws"}, bytes.NewReader(nil), &out, &stderr, "test"); code == 0 || !strings.Contains(stderr.String(), "takes no Workspace argument") {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}

func TestApplyOutsideWorkspace(t *testing.T) {
	chdir(t, newCLITestRepo(t))
	var out, stderr bytes.Buffer
	if code := Run([]string{"apply"}, bytes.NewReader(nil), &out, &stderr, "test"); code == 0 || !strings.Contains(stderr.String(), "not inside a registered Workspace") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
}

func TestFinishWithoutArgumentUsesCurrentWorkspace(t *testing.T) {
	repo := newCLITestRepo(t)
	chdir(t, repo)
	setGitIdentityEnv(t)
	var out, stderr bytes.Buffer
	if code := Run([]string{"new", "inner", "--no-enter"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("new code=%d stderr=%s", code, stderr.String())
	}
	id := workspaceIDFromOutput(t, out.String())
	worktree := filepath.Join(repo, ".git", "gws", "worktrees", id)
	if err := os.WriteFile(filepath.Join(worktree, "inner.txt"), []byte("inner\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustGitOutput(t, worktree, "add", "inner.txt")
	mustGitOutput(t, worktree, "commit", "-m", "inner change")

	chdir(t, worktree)
	out.Reset()
	stderr.Reset()
	if code := Run([]string{"finish"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("finish code=%d stderr=%s", code, stderr.String())
	}
	text := out.String()
	repoCanonical, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Finished Workspace inner ("+id+")") || !strings.Contains(text, "cd "+repoCanonical) {
		t.Fatalf("finish output=%s", text)
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Fatalf("worktree survived finish: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "inner.txt")); err != nil {
		t.Fatalf("change was not applied to the main checkout: %v", err)
	}
	if !strings.Contains(string(mustGitOutput(t, repo, "log", "-1", "--pretty=%s")), "inner change") {
		t.Fatal("main branch did not receive the commit")
	}
}

func TestCommitOptionHintForUnknownShortFlags(t *testing.T) {
	repo := newCLITestRepo(t)
	chdir(t, repo)
	setGitIdentityEnv(t)
	var out, stderr bytes.Buffer
	if code := Run([]string{"new", "hint", "--no-enter"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("new code=%d stderr=%s", code, stderr.String())
	}
	id := workspaceIDFromOutput(t, out.String())
	chdir(t, filepath.Join(repo, ".git", "gws", "worktrees", id))
	out.Reset()
	stderr.Reset()
	if code := Run([]string{"commit", "-am", "combined"}, bytes.NewReader(nil), &out, &stderr, "test"); code == 0 {
		t.Fatal("-am is not a supported short flag and must be rejected")
	}
	if !strings.Contains(stderr.String(), "Unknown option -am") || !strings.Contains(stderr.String(), "gws commit -m") {
		t.Fatalf("stderr=%s", stderr.String())
	}
}

func TestEndToEndNewApplyFinish(t *testing.T) {
	repo := newCLITestRepo(t)
	chdir(t, repo)
	var out, stderr bytes.Buffer
	if code := Run([]string{"new", "cli", "--no-enter", "--json"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("new code=%d stderr=%s", code, stderr.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	id := envelope["data"].(map[string]any)["id"].(string)
	worktree := envelope["data"].(map[string]any)["worktree_path"].(string)

	if err := os.WriteFile(filepath.Join(worktree, "cli.txt"), []byte("cli\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	chdir(t, worktree)

	out.Reset()
	stderr.Reset()
	if code := Run([]string{"diff", "--stat", "--json"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("diff code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
	var diffEnvelope map[string]any
	if err := json.Unmarshal(out.Bytes(), &diffEnvelope); err != nil {
		t.Fatal(err)
	}
	untracked, _ := diffEnvelope["data"].(map[string]any)["untracked_files"].([]any)
	found := false
	for _, entry := range untracked {
		if entry == "cli.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("diff did not report the untracked file: %#v", diffEnvelope["data"])
	}

	out.Reset()
	stderr.Reset()
	if code := Run([]string{"commit"}, bytes.NewReader(nil), &out, &stderr, "test"); code == 0 || !strings.Contains(stderr.String(), "Commit message is empty") {
		t.Fatalf("commit without message: code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}

	mustGitOutput(t, worktree, "add", "cli.txt")
	out.Reset()
	stderr.Reset()
	if code := Run([]string{"commit", "-m", "cli change", "--json"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("commit code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
	var commitEnvelope map[string]any
	if err := json.Unmarshal(out.Bytes(), &commitEnvelope); err != nil {
		t.Fatal(err)
	}
	if subject, _ := commitEnvelope["data"].(map[string]any)["subject"].(string); subject != "cli change" {
		t.Fatalf("commit subject=%q", subject)
	}

	out.Reset()
	stderr.Reset()
	if code := Run([]string{"log", "-n", "5"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 || !strings.Contains(out.String(), "cli change") {
		t.Fatalf("log code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}

	out.Reset()
	stderr.Reset()
	if code := Run([]string{"diff", "--uncommitted"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 || !strings.Contains(out.String(), "No changes relative to") {
		t.Fatalf("uncommitted diff code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}

	out.Reset()
	stderr.Reset()
	if code := Run([]string{"apply", "--json"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("apply code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
	var applyEnvelope map[string]any
	if err := json.Unmarshal(out.Bytes(), &applyEnvelope); err != nil {
		t.Fatal(err)
	}
	applyData := applyEnvelope["data"].(map[string]any)
	if applyData["target_branch"] != "main" || applyData["repo_root"] == nil {
		t.Fatalf("apply data=%#v", applyData)
	}
	if _, err := os.Stat(filepath.Join(repo, "cli.txt")); err != nil {
		t.Fatalf("apply did not merge into the repository: %v", err)
	}

	chdir(t, repo)
	out.Reset()
	stderr.Reset()
	if code := Run([]string{"ls"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("ls code=%d stderr=%s", code, stderr.String())
	}
	for _, expected := range []string{"NAME", "ID", "TYPE", "BRANCH", "BASE", "STATUS", "cli", "gws/", "main", "ready"} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("ls missing %q: %s", expected, out.String())
		}
	}
	if strings.Contains(out.String(), "REPOSITORY") || strings.Contains(out.String(), "FROM") {
		t.Fatalf("ls still shows project columns: %s", out.String())
	}

	out.Reset()
	stderr.Reset()
	if code := Run([]string{"show", id}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("show code=%d stderr=%s", code, stderr.String())
	}
	for _, expected := range []string{"Workspace: cli", "ID: " + id, "Branch: gws/", "Base: main", "Base commit:", "Managed branch: true", "Status: ready", "Worktree: " + worktree} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("show missing %q: %s", expected, out.String())
		}
	}
	if strings.Contains(out.String(), "Repository: "+filepath.Base(repo)) || strings.Contains(out.String(), "From:") {
		t.Fatalf("show still prints project fields: %s", out.String())
	}

	out.Reset()
	stderr.Reset()
	if code := Run([]string{"status", id, "--json"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("status code=%d stderr=%s", code, stderr.String())
	}
	var statusEnvelope map[string]any
	if err := json.Unmarshal(out.Bytes(), &statusEnvelope); err != nil {
		t.Fatal(err)
	}
	statusData := statusEnvelope["data"].(map[string]any)
	if statusData["git"].(map[string]any)["dirty"] != false {
		t.Fatalf("status=%#v", statusData)
	}

	out.Reset()
	stderr.Reset()
	if code := Run([]string{"path", id}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 || strings.TrimSpace(out.String()) != worktree {
		t.Fatalf("path code=%d stdout=%q stderr=%s", code, out.String(), stderr.String())
	}

	out.Reset()
	stderr.Reset()
	if code := Run([]string{"finish", id, "--json"}, bytes.NewReader(nil), &out, &stderr, "test"); code != 0 {
		t.Fatalf("finish code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Fatalf("finish left the worktree behind: %v", err)
	}
	git := gitworkspace.Git{Runner: gitworkspace.Runner{}}
	if git.RefExists(context.Background(), repo, "gws/"+id) {
		t.Fatal("finish left the managed branch behind")
	}
}

func workspaceIDFromOutput(t *testing.T, text string) string {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if rest, ok := strings.CutPrefix(line, "Created Workspace "); ok {
			return strings.TrimSpace(rest)
		}
	}
	t.Fatalf("no workspace id in output: %q", text)
	return ""
}
