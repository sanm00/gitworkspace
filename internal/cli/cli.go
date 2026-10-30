package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/sanm00/gitworkspace/internal/gitworkspace"
)

type ioStreams struct {
	in       io.Reader
	out, err io.Writer
}

func Run(args []string, stdin io.Reader, stdout, stderr io.Writer, version string) int {
	streams := ioStreams{stdin, stdout, stderr}
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		printHelp(stdout)
		return 0
	}
	if args[0] == "--version" || args[0] == "version" {
		fmt.Fprintf(stdout, "gws %s\n", version)
		return 0
	}
	command := args[0]
	if command == "capabilities" {
		parsed, parseErr := parse("capabilities", args[1:])
		if parseErr != nil {
			return fail(streams, command, false, parseErr)
		}
		if err := runCapabilities(parsed, streams); err != nil {
			return fail(streams, command, parsed.bool("json"), err)
		}
		return 0
	}
	parsed, parseErr := parse(command, args[1:])
	useJSON := command == "use" && parsed.bool("json")
	jsonMode := parsed.bool("json") && !useJSON
	if useJSON {
		parseErr = gitworkspace.NewError("config_invalid", "use is interactive and does not support --json. Use path --json for platform integration.")
	}
	if parseErr != nil {
		return fail(streams, command, jsonMode, parseErr)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fail(streams, command, jsonMode, err)
	}
	git := gitworkspace.Git{Runner: gitworkspace.Runner{}}
	repo, err := git.DetectRepository(context.Background(), cwd)
	if err != nil {
		return fail(streams, command, jsonMode, gitworkspace.NewError("repository_not_found", "Current directory is not inside a Git repository. Run gws from a repository checkout."))
	}
	paths, err := gitworkspace.NewPaths(repo)
	if err != nil {
		return fail(streams, command, jsonMode, err)
	}
	if err := paths.Ensure(); err != nil {
		return fail(streams, command, jsonMode, err)
	}
	app := gitworkspace.NewApp(paths, repo)
	ctx := context.Background()
	var runErr error
	switch command {
	case "new":
		runErr = runNew(ctx, app, cwd, parsed, streams)
	case "use":
		runErr = runUse(ctx, app, cwd, parsed, streams)
	case "apply":
		runErr = runApply(ctx, app, cwd, parsed, streams)
	case "finish":
		runErr = runFinish(ctx, app, cwd, parsed, streams)
	case "ls":
		runErr = runList(ctx, app, parsed, streams)
	case "show":
		runErr = runShow(ctx, app, cwd, parsed, streams)
	case "status":
		runErr = runStatus(ctx, app, cwd, parsed, streams)
	case "diff":
		runErr = runDiff(ctx, app, cwd, parsed, streams)
	case "log":
		runErr = runLog(ctx, app, cwd, parsed, streams)
	case "commit":
		runErr = runCommit(ctx, app, cwd, parsed, streams)
	case "path":
		runErr = runPath(ctx, app, cwd, parsed, streams)
	case "rm":
		runErr = runRemove(ctx, app, cwd, parsed, streams)
	case "gc":
		runErr = runGC(ctx, app, parsed, streams)
	case "doctor":
		runErr = runDoctor(ctx, app, parsed, streams)
	default:
		runErr = gitworkspace.NewError("config_invalid", fmt.Sprintf("Unknown command %q. Run gws --help.", command))
	}
	if errors.Is(runErr, errExitShell) {
		return ExitWorkspace
	}
	if runErr != nil {
		return fail(streams, command, jsonMode, runErr)
	}
	return 0
}

// errExitShell signals that the Workspace subshell should close. The shell
// wrapper function installed by LaunchShell watches for ExitWorkspace and turns
// it into "exit 0", because a child process cannot move its parent shell.
var errExitShell = errors.New("exit workspace shell")

// ExitWorkspace is the exit code the wrapper function watches for.
const ExitWorkspace = gitworkspace.ExitWorkspaceShell

type parsedArgs struct {
	positional []string
	values     map[string]string
	flags      map[string]bool
}

var valueFlags = map[string]bool{"base": true, "type": true, "into": true, "limit": true, "message": true}
var boolFlags = map[string]bool{"json": true, "detach": true, "fetch": true, "no-enter": true, "force": true, "dry-run": true, "staged": true, "stat": true, "uncommitted": true, "all": true}

var shortFlags = map[string]string{"-m": "message", "-n": "limit", "-a": "all"}

func parse(command string, args []string) (parsedArgs, error) {
	p := parsedArgs{values: map[string]string{}, flags: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if long, ok := shortFlags[arg]; ok {
			if long == "all" {
				p.flags[long] = true
				continue
			}
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return p, gitworkspace.NewError("config_invalid", fmt.Sprintf("Option %s requires a value.", arg))
			}
			i++
			p.values[long] = args[i]
			continue
		}
		if !strings.HasPrefix(arg, "--") {
			p.positional = append(p.positional, arg)
			continue
		}
		key := strings.TrimPrefix(arg, "--")
		if boolFlags[key] {
			p.flags[key] = true
			continue
		}
		if !valueFlags[key] {
			return p, gitworkspace.NewError("config_invalid", fmt.Sprintf("Unknown option --%s.", key))
		}
		if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
			return p, gitworkspace.NewError("config_invalid", fmt.Sprintf("Option --%s requires a value.", key))
		}
		i++
		p.values[key] = args[i]
	}
	if command != "rm" && p.flags["force"] {
		return p, gitworkspace.NewError("config_invalid", "--force is supported only by gws rm. Use --into <branch> to choose a merge target.")
	}
	return p, nil
}
func (p parsedArgs) value(key string) string { return p.values[key] }
func (p parsedArgs) bool(key string) bool    { return p.flags[key] }
func (p parsedArgs) optional(index int) string {
	if len(p.positional) <= index {
		return ""
	}
	return p.positional[index]
}

func runNew(ctx context.Context, app *gitworkspace.App, cwd string, p parsedArgs, s ioStreams) error {
	kind := gitworkspace.WorkspaceDev
	if value := p.value("type"); value != "" {
		switch gitworkspace.WorkspaceType(value) {
		case gitworkspace.WorkspaceDev, gitworkspace.WorkspaceFix, gitworkspace.WorkspaceReview, gitworkspace.WorkspaceTest:
			kind = gitworkspace.WorkspaceType(value)
		default:
			return gitworkspace.NewError("config_invalid", fmt.Sprintf("Unknown workspace type %q. Use dev, fix, review or test.", value))
		}
	}
	created, err := app.CreateWorkspace(ctx, gitworkspace.CreateRequest{
		CWD:    cwd,
		Name:   p.optional(0),
		Base:   p.value("base"),
		Type:   kind,
		Detach: p.bool("detach"),
		Fetch:  p.bool("fetch"),
	})
	if err != nil {
		return err
	}
	baseDisplay := created.Source.BaseRef
	baseCommit := created.Source.BaseCommit
	if baseDisplay == "" {
		baseDisplay = created.Source.FromRef
	}
	if p.bool("json") {
		return writeJSON(s.out, "new", workspaceView(created), nil)
	}
	fmt.Fprintf(s.out, "Created Workspace %s\nType: %s\nBased on: %s (%s)\nBranch: %s\nWorktree: %s\nTask: %s\n", created.ID, created.Type, displayValue(baseDisplay), shortSHA(baseCommit), created.Source.Ref, created.WorktreePath, created.TaskFilePath)
	if !shouldEnter(p, s) {
		fmt.Fprintf(s.out, "\nNext:\n  gws use %s\n", created.ID)
		return nil
	}
	fmt.Fprintf(s.out, "\nEnterting Workspace %s. Exit the shell to return.\n", created.Name)
	return app.LaunchShell(ctx, created, s.in, s.out, s.err)
}

func isTerminal(out io.Writer) bool {
	file, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func shouldEnter(p parsedArgs, s ioStreams) bool {
	if p.bool("json") || p.bool("no-enter") {
		return false
	}
	return isTerminal(s.out)
}

func shortSHA(value string) string {
	if value == "" {
		return "-"
	}
	if len(value) > 8 {
		return value[:8]
	}
	return value
}

func runUse(ctx context.Context, app *gitworkspace.App, cwd string, p parsedArgs, s ioStreams) error {
	selector := p.optional(0)
	workspace, err := app.ResolveWorkspace(ctx, selector, cwd, true)
	if err != nil {
		return err
	}
	fmt.Fprintf(s.out, "Entering Workspace %s (%s)\nDirectory: %s\nExit the shell to return.\n", workspace.Name, workspace.ID, workspace.WorktreePath)
	return app.LaunchShell(ctx, workspace, s.in, s.out, s.err)
}

func runApply(ctx context.Context, app *gitworkspace.App, cwd string, p parsedArgs, s ioStreams) error {
	if len(p.positional) > 0 {
		return gitworkspace.NewError("config_invalid", "gws apply takes no Workspace argument. Run it from inside the Workspace, or use gws finish <workspace-id>.")
	}
	result, err := app.ApplyWorkspace(ctx, gitworkspace.ApplyRequest{CWD: cwd, Into: p.value("into")})
	if err != nil {
		return err
	}
	if p.bool("json") {
		return writeJSON(s.out, "apply", applyView(result), nil)
	}
	fmt.Fprintf(s.out, "Applied Workspace %s (%s)\nSource: %s\nTarget: %s\nRepository: %s\n", result.Workspace.Name, result.Workspace.ID, result.SourceBranch, result.TargetBranch, result.RepoRoot)
	return nil
}

func runFinish(ctx context.Context, app *gitworkspace.App, cwd string, p parsedArgs, s ioStreams) error {
	result, err := app.FinishWorkspace(ctx, p.optional(0), p.value("into"), cwd)
	if err != nil {
		return err
	}
	if p.bool("json") {
		return writeJSON(s.out, "finish", applyView(result), nil)
	}
	leavesShell := result.CWDInside &&
		os.Getenv(gitworkspace.ShellMarkerEnv) == result.Workspace.ID &&
		os.Getenv(gitworkspace.ShellWrappedEnv) == "1"
	branchAction := "preserved"
	if result.Workspace.Source.Managed {
		branchAction = "removed"
	}
	fmt.Fprintf(s.out, "Finished Workspace %s (%s)\nApplied: %s -> %s\nRemoved: %s\nBranch %s was %s.\n", result.Workspace.Name, result.Workspace.ID, result.SourceBranch, result.TargetBranch, result.Workspace.WorktreePath, result.SourceBranch, branchAction)
	if leavesShell {
		fmt.Fprintln(s.out, "\nClosing the Workspace shell; you are back where you started.")
		return errExitShell
	}
	if result.CWDInside {
		fmt.Fprintf(s.out, "\nYour shell was inside the removed directory. Run: cd %s\n", result.RepoRoot)
	}
	return nil
}

func runList(ctx context.Context, app *gitworkspace.App, p parsedArgs, s ioStreams) error {
	values, err := app.Workspaces(ctx)
	if err != nil {
		return err
	}
	if p.bool("json") {
		return writeJSON(s.out, "ls", listData{Workspaces: workspaceViews(values)}, nil)
	}
	if len(values) == 0 {
		fmt.Fprintln(s.out, "No Workspaces. Run gws new to create one.")
		return nil
	}
	w := tabwriter.NewWriter(s.out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tID\tTYPE\tBRANCH\tBASE\tSTATUS")
	for _, v := range values {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", v.Name, v.ID, v.Type, v.Source.Ref, displayValue(baseRef(v)), v.State)
	}
	return w.Flush()
}

func baseRef(w gitworkspace.Workspace) string {
	if w.Source.BaseRef != "" {
		return w.Source.BaseRef
	}
	return w.Source.FromRef
}

func runShow(ctx context.Context, app *gitworkspace.App, cwd string, p parsedArgs, s ioStreams) error {
	selector := p.optional(0)
	workspace, err := app.ResolveWorkspace(ctx, selector, cwd, true)
	if err != nil {
		return err
	}
	if p.bool("json") {
		return writeJSON(s.out, "show", showData{Workspace: workspaceView(workspace)}, nil)
	}
	fmt.Fprintf(s.out, "Workspace: %s\nID: %s\nRepository: %s\nGit common dir: %s\nType: %s\nBranch: %s\nBase: %s\nBase commit: %s\nManaged branch: %t\nStatus: %s\nWorktree: %s\nTask: %s\nCreated: %s\nUpdated: %s\n", workspace.Name, workspace.ID, app.Repo.RepoPath(), app.Repo.GitCommonDir, workspace.Type, workspace.Source.Ref, displayValue(baseRef(workspace)), displayValue(workspace.Source.BaseCommit), workspace.Source.Managed, workspace.State, workspace.WorktreePath, workspace.TaskFilePath, workspace.CreatedAt.UTC().Format(time.RFC3339), workspace.UpdatedAt.UTC().Format(time.RFC3339))
	return nil
}

func displayValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func runStatus(ctx context.Context, app *gitworkspace.App, cwd string, p parsedArgs, s ioStreams) error {
	selector := p.optional(0)
	status, err := app.Status(ctx, selector, cwd)
	if err != nil {
		return err
	}
	if p.bool("json") {
		return writeJSON(s.out, "status", statusView(status), nil)
	}
	gitState := "missing"
	if status.WorktreeExists {
		gitState = "unknown"
		if status.Dirty != nil && !*status.Dirty {
			gitState = "clean"
		}
		if status.Dirty != nil && *status.Dirty {
			gitState = "dirty"
		}
	}
	fmt.Fprintf(s.out, "Workspace: %s\nBase: %s (%s)\nGit: %s\nWorktree registered: %t\nTask: %t\nState: %s\n", status.Workspace.ID, displayValue(baseRef(status.Workspace)), shortSHA(status.Workspace.Source.BaseCommit), gitState, status.WorktreeRegistered, status.TaskExists, status.Workspace.State)
	return nil
}

func runDiff(ctx context.Context, app *gitworkspace.App, cwd string, p parsedArgs, s ioStreams) error {
	result, err := app.DiffWorkspace(ctx, p.optional(0), cwd, p.bool("staged"), p.bool("stat"), p.bool("uncommitted"))
	if err != nil {
		return err
	}
	if p.bool("json") {
		return writeJSON(s.out, "diff", diffData{WorkspaceID: result.Workspace.ID, Base: result.Base, Content: result.Content, Untracked: result.Untracked}, nil)
	}
	if strings.TrimSpace(result.Content) == "" && len(result.Untracked) == 0 {
		fmt.Fprintf(s.out, "No changes relative to %s.\n", shortSHA(result.Base))
		return nil
	}
	if strings.TrimSpace(result.Content) != "" {
		fmt.Fprint(s.out, result.Content)
		if !strings.HasSuffix(result.Content, "\n") {
			fmt.Fprintln(s.out)
		}
	}
	if len(result.Untracked) > 0 {
		fmt.Fprintf(s.out, "\nUntracked files (not in the diff, stage them with git add):\n")
		for _, file := range result.Untracked {
			fmt.Fprintf(s.out, "  %s\n", file)
		}
	}
	return nil
}

func runLog(ctx context.Context, app *gitworkspace.App, cwd string, p parsedArgs, s ioStreams) error {
	limit := 0
	if value := p.value("limit"); value != "" {
		parsedLimit, err := strconv.Atoi(value)
		if err != nil || parsedLimit <= 0 {
			return gitworkspace.NewError("config_invalid", fmt.Sprintf("Invalid --limit value %q. Use a positive integer.", value))
		}
		limit = parsedLimit
	}
	result, err := app.LogWorkspace(ctx, p.optional(0), cwd, limit, p.bool("stat"))
	if err != nil {
		return err
	}
	if p.bool("json") {
		return writeJSON(s.out, "log", logData{WorkspaceID: result.Workspace.ID, Base: result.Base, Content: result.Content}, nil)
	}
	if strings.TrimSpace(result.Content) == "" {
		fmt.Fprintf(s.out, "No commits since %s.\n", shortSHA(result.Base))
		return nil
	}
	fmt.Fprint(s.out, result.Content)
	if !strings.HasSuffix(result.Content, "\n") {
		fmt.Fprintln(s.out)
	}
	return nil
}

func runCommit(ctx context.Context, app *gitworkspace.App, cwd string, p parsedArgs, s ioStreams) error {
	if len(p.positional) > 0 {
		first := p.positional[0]
		message := "gws commit takes no positional arguments. Run it from inside the Workspace; message goes to --message."
		if strings.HasPrefix(first, "-") {
			message = fmt.Sprintf("Unknown option %s. Use -a and --message/-m separately, for example: gws commit -m \"summary\" -a", first)
		}
		return gitworkspace.NewError("config_invalid", message)
	}
	result, err := app.CommitWorkspace(ctx, cwd, p.value("message"), p.bool("all"))
	if err != nil {
		return err
	}
	if p.bool("json") {
		return writeJSON(s.out, "commit", commitData{WorkspaceID: result.Workspace.ID, Branch: result.Workspace.Source.Ref, Commit: result.Commit, Subject: result.Subject}, nil)
	}
	fmt.Fprintf(s.out, "Committed %s to Workspace %s (%s)\nBranch: %s\nSubject: %s\n", shortSHA(result.Commit), result.Workspace.Name, result.Workspace.ID, result.Workspace.Source.Ref, result.Subject)
	return nil
}

func runPath(ctx context.Context, app *gitworkspace.App, cwd string, p parsedArgs, s ioStreams) error {
	selector := p.optional(0)
	w, err := app.ResolveWorkspace(ctx, selector, cwd, true)
	if err != nil {
		return err
	}
	if p.bool("json") {
		return writeJSON(s.out, "path", pathData{WorkspaceID: w.ID, Path: w.WorktreePath}, nil)
	}
	fmt.Fprintln(s.out, w.WorktreePath)
	return nil
}
func runRemove(ctx context.Context, app *gitworkspace.App, cwd string, p parsedArgs, s ioStreams) error {
	selector := p.optional(0)
	if selector == "" {
		return gitworkspace.NewError("config_invalid", "Missing a Workspace argument. Usage: gws rm <workspace-id>. Run gws ls to list candidates.")
	}
	if err := app.RemoveWorkspace(ctx, selector, cwd, p.bool("force")); err != nil {
		return err
	}
	if p.bool("json") {
		return writeJSON(s.out, "rm", removeData{Selector: selector, Removed: true}, nil)
	}
	fmt.Fprintf(s.out, "Removed Workspace %s.\n", selector)
	return nil
}
func runGC(ctx context.Context, app *gitworkspace.App, p parsedArgs, s ioStreams) error {
	actions, err := app.GC(ctx, p.bool("dry-run"))
	if err != nil {
		return err
	}
	if p.bool("json") {
		return writeJSON(s.out, "gc", gcData{DryRun: p.bool("dry-run"), Actions: actions}, nil)
	}
	if len(actions) == 0 {
		fmt.Fprintln(s.out, "No orphaned Git Workspace resources found.")
		return nil
	}
	for _, a := range actions {
		state := "planned"
		if a.Applied {
			state = "applied"
		}
		fmt.Fprintf(s.out, "%s\t%s\t%s\t%s\n", state, a.Action, a.Path, a.Reason)
	}
	return nil
}

func runDoctor(ctx context.Context, app *gitworkspace.App, p parsedArgs, s ioStreams) error {
	checks, err := app.Doctor(ctx)
	if err != nil {
		return err
	}
	hasError := false
	for _, c := range checks {
		if c.Status == "error" {
			hasError = true
		}
	}
	if p.bool("json") {
		if hasError {
			return gitworkspace.NewError("doctor_failed", "Doctor found invalid repository configuration.")
		}
		return writeJSON(s.out, "doctor", doctorData{Checks: checks}, nil)
	}
	for _, c := range checks {
		mark := "✓"
		if c.Status == "optional" {
			mark = "-"
		}
		if c.Status == "error" {
			mark = "✗"
		}
		fmt.Fprintf(s.out, "%s %s: %s\n", mark, c.Name, c.Message)
	}
	if hasError {
		return gitworkspace.NewError("doctor_failed", "Doctor found invalid repository configuration.")
	}
	return nil
}

func runCapabilities(p parsedArgs, s ioStreams) error {
	data := capabilitiesData{
		ProtocolVersion: 2,
		StateVersion:    gitworkspace.CurrentStateVersion,
		Backend:         "git-worktree",
		Features:        []string{"work-item", "managed-branch", "safe-cleanup", "apply", "finish", "diff", "log", "commit", "json"},
		Interactive:     []string{"use", "new"},
	}
	if p.bool("json") {
		return writeJSON(s.out, "capabilities", data, nil)
	}
	fmt.Fprintln(s.out, "Backend: git-worktree")
	fmt.Fprintln(s.out, "Plugin protocol: 2")
	fmt.Fprintf(s.out, "State format: %d\n", gitworkspace.CurrentStateVersion)
	fmt.Fprintln(s.out, "Machine interface: add --json to non-interactive commands")
	return nil
}

type envelope struct {
	SchemaVersion int    `json:"schema_version"`
	Command       string `json:"command"`
	Data          any    `json:"data"`
	Error         any    `json:"error"`
}
type errorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

func writeJSON(out io.Writer, command string, data any, err error) error {
	env := envelope{SchemaVersion: 2, Command: command, Data: data, Error: nil}
	if err != nil {
		env.Data = nil
		env.Error = errorBody{Code: gitworkspace.ErrorCode(err), Message: err.Error(), Details: gitworkspace.ErrorDetails(err)}
	}
	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(env)
}

type sourceJSON struct {
	Kind       string  `json:"kind"`
	Ref        string  `json:"ref"`
	BaseRef    *string `json:"base_ref"`
	BaseCommit *string `json:"base_commit"`
	Detached   bool    `json:"detached"`
	New        bool    `json:"new"`
	Managed    bool    `json:"managed"`
	FromRef    *string `json:"from_ref"`
}
type workspaceJSON struct {
	ID           string                      `json:"id"`
	Name         string                      `json:"name"`
	Type         gitworkspace.WorkspaceType  `json:"type"`
	Source       sourceJSON                  `json:"source"`
	WorktreePath string                      `json:"worktree_path"`
	TaskFilePath string                      `json:"task_file_path"`
	State        gitworkspace.WorkspaceState `json:"state"`
	LastError    *string                     `json:"last_error"`
	CreatedAt    string                      `json:"created_at"`
	UpdatedAt    string                      `json:"updated_at"`
}

func pointer(value string) *string {
	if value == "" {
		return nil
	}
	v := value
	return &v
}
func workspaceView(w gitworkspace.Workspace) workspaceJSON {
	base := w.Source.BaseRef
	if base == "" {
		base = w.Source.FromRef
	}
	return workspaceJSON{ID: w.ID, Name: w.Name, Type: w.Type, Source: sourceJSON{Kind: w.Source.Kind, Ref: w.Source.Ref, BaseRef: pointer(base), BaseCommit: pointer(w.Source.BaseCommit), Detached: w.Source.Detached, New: w.Source.New, Managed: w.Source.Managed, FromRef: pointer(w.Source.FromRef)}, WorktreePath: w.WorktreePath, TaskFilePath: w.TaskFilePath, State: w.State, LastError: pointer(w.LastError), CreatedAt: w.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: w.UpdatedAt.UTC().Format(time.RFC3339)}
}
func workspaceViews(values []gitworkspace.Workspace) []workspaceJSON {
	result := make([]workspaceJSON, len(values))
	for i, v := range values {
		result[i] = workspaceView(v)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return result[i].ID < result[j].ID
	})
	return result
}

type listData struct {
	Workspaces []workspaceJSON `json:"workspaces"`
}

type applyData struct {
	Workspace    workspaceJSON `json:"workspace"`
	RepoRoot     string        `json:"repo_root"`
	SourceBranch string        `json:"source_branch"`
	TargetBranch string        `json:"target_branch"`
}

type pathData struct {
	WorkspaceID string `json:"workspace_id"`
	Path        string `json:"path"`
}

type diffData struct {
	WorkspaceID string   `json:"workspace_id"`
	Base        string   `json:"base"`
	Content     string   `json:"content"`
	Untracked   []string `json:"untracked_files"`
}

type logData struct {
	WorkspaceID string `json:"workspace_id"`
	Base        string `json:"base"`
	Content     string `json:"content"`
}

type commitData struct {
	WorkspaceID string `json:"workspace_id"`
	Branch      string `json:"branch"`
	Commit      string `json:"commit"`
	Subject     string `json:"subject"`
}

type removeData struct {
	Selector string `json:"selector"`
	Removed  bool   `json:"removed"`
}

type gcData struct {
	DryRun  bool                    `json:"dry_run"`
	Actions []gitworkspace.GCAction `json:"actions"`
}

type doctorData struct {
	Checks []gitworkspace.DoctorCheck `json:"checks"`
}

type capabilitiesData struct {
	ProtocolVersion int      `json:"protocol_version"`
	StateVersion    int      `json:"state_version"`
	Backend         string   `json:"backend"`
	Features        []string `json:"features"`
	Interactive     []string `json:"interactive_commands"`
}

type showData struct {
	Workspace workspaceJSON `json:"workspace"`
}

type gitStatusJSON struct {
	RepoExists         bool  `json:"repo_exists"`
	WorktreeExists     bool  `json:"worktree_exists"`
	WorktreeRegistered bool  `json:"worktree_registered"`
	Dirty              *bool `json:"dirty"`
}

type taskStatusJSON struct {
	Exists bool `json:"exists"`
}

type statusData struct {
	Workspace workspaceJSON  `json:"workspace"`
	Git       gitStatusJSON  `json:"git"`
	Task      taskStatusJSON `json:"task"`
}

func applyView(v gitworkspace.ApplyResult) applyData {
	return applyData{Workspace: workspaceView(v.Workspace), RepoRoot: v.RepoRoot, SourceBranch: v.SourceBranch, TargetBranch: v.TargetBranch}
}
func statusView(v gitworkspace.Status) statusData {
	return statusData{
		Workspace: workspaceView(v.Workspace),
		Git:       gitStatusJSON{RepoExists: v.RepoRoot != "", WorktreeExists: v.WorktreeExists, WorktreeRegistered: v.WorktreeRegistered, Dirty: v.Dirty},
		Task:      taskStatusJSON{Exists: v.TaskExists},
	}
}

func fail(s ioStreams, command string, jsonMode bool, err error) int {
	if jsonMode {
		_ = writeJSON(s.out, command, nil, err)
	} else {
		fmt.Fprintf(s.err, "%s %s\n", errorMarker(s.err), err)
	}
	return 1
}

func errorMarker(out io.Writer) string {
	return formatErrorMarker(supportsColor(out))
}

func formatErrorMarker(color bool) string {
	const marker = "!"
	if !color {
		return marker
	}
	return "\x1b[31m" + marker + "\x1b[0m"
}

func supportsColor(out io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func printHelp(out io.Writer) {
	fmt.Fprint(out, `Git Workspace creates isolated worktrees from the branch you are on and merges them back safely.

Usage:
  gws new [NAME] [--base BRANCH|SHA] [--type dev|fix|review|test] [--detach] [--fetch] [--no-enter] [--json]
  gws use [WORKSPACE]
  gws apply [--into BRANCH]
  gws finish WORKSPACE [--into BRANCH]
  gws diff [WORKSPACE] [--staged] [--stat] [--uncommitted] [--json]
  gws log [WORKSPACE] [--limit N] [--stat] [--json]
  gws commit --message MESSAGE [--all] [--json]
  gws ls [--json]
  gws show|status|path [WORKSPACE]
  gws rm WORKSPACE [--force]
  gws gc [--dry-run]
  gws doctor
  gws capabilities [--json]

Workflow:
  gws new                 create a Workspace based on the current branch and enter it
  gws use <id>            enter an existing Workspace
  gws commit -m ...       commit inside the Workspace (--all includes tracked changes)
  gws diff                show everything this Workspace changed since its base
  gws apply               merge the Workspace back into its base branch (run inside the Workspace)
  gws finish [id]         merge back and clean up (id defaults to the current Workspace)

Machine interface:
  Add --json to non-interactive commands. use is always interactive; new enters a shell unless --json/--no-enter is set.
`)
}
