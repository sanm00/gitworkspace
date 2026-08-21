package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
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
	rest := args[1:]
	parsed, parseErr := parse(rest)
	jsonMode := parsed.bool("json")
	if parseErr != nil {
		return fail(streams, command, jsonMode, parseErr)
	}
	paths, err := gitworkspace.DefaultPaths()
	if err != nil {
		return fail(streams, args[0], false, err)
	}
	app, err := gitworkspace.NewApp(paths)
	if err != nil {
		return fail(streams, args[0], false, err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fail(streams, args[0], false, err)
	}
	ctx := context.Background()
	var runErr error
	switch command {
	case "init":
		runErr = runInit(ctx, app, cwd, parsed, streams)
	case "project":
		runErr = runProject(ctx, app, cwd, parsed, streams)
	case "new":
		runErr = runNew(ctx, app, cwd, parsed, streams)
	case "checkout":
		runErr = runCheckout(ctx, app, cwd, parsed, streams)
	case "apply":
		runErr = runApply(ctx, app, cwd, parsed, streams)
	case "finish":
		runErr = runFinish(ctx, app, cwd, parsed, streams)
	case "dev", "review", "fix", "test":
		runErr = runCreate(ctx, app, cwd, command, parsed, streams)
	case "ls":
		runErr = runList(ctx, app, parsed, streams)
	case "show":
		runErr = runShow(ctx, app, cwd, parsed, streams)
	case "status":
		runErr = runStatus(ctx, app, cwd, parsed, streams)
	case "path":
		runErr = runPath(ctx, app, cwd, parsed, streams)
	case "rm":
		runErr = runRemove(ctx, app, cwd, parsed, streams)
	case "gc":
		runErr = runGC(ctx, app, parsed, streams)
	case "doctor":
		runErr = runDoctor(ctx, app, cwd, parsed, streams)
	case "capabilities":
		runErr = runCapabilities(parsed, streams)
	default:
		runErr = gitworkspace.NewError("config_invalid", fmt.Sprintf("Unknown command %q. Run gws --help.", command))
	}
	if runErr != nil {
		return fail(streams, command, jsonMode, runErr)
	}
	return 0
}

type parsedArgs struct {
	positional []string
	values     map[string]string
	flags      map[string]bool
}

func parse(args []string) (parsedArgs, error) {
	p := parsedArgs{values: map[string]string{}, flags: map[string]bool{}}
	bools := map[string]bool{"json": true, "new": true, "fetch": true, "force": true, "dry-run": true}
	values := map[string]bool{"name": true, "default-branch": true, "project": true, "from": true, "base": true}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			p.positional = append(p.positional, arg)
			continue
		}
		key := strings.TrimPrefix(arg, "--")
		if bools[key] {
			p.flags[key] = true
			continue
		}
		if !values[key] {
			return p, gitworkspace.NewError("config_invalid", fmt.Sprintf("Unknown option --%s.", key))
		}
		if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
			return p, gitworkspace.NewError("config_invalid", fmt.Sprintf("Option --%s requires a value.", key))
		}
		i++
		p.values[key] = args[i]
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
	if p.value("project") != "" {
		return gitworkspace.NewError("config_invalid", "gws new determines the Project from the current directory and does not accept --project.")
	}
	workItem := p.optional(0)
	if workItem == "" {
		workItem = "workspace"
	}
	if _, _, err := app.InitProject(ctx, gitworkspace.InitRequest{CWD: cwd}); err != nil {
		return err
	}
	initialCommitCreated, err := app.EnsureInitialCommit(ctx, cwd)
	if err != nil {
		return err
	}
	from := p.value("from")
	workspace, err := app.CreateWorkspace(ctx, gitworkspace.CreateRequest{
		ProjectSelector: p.value("project"),
		CWD:             cwd,
		Name:            p.value("name"),
		Ref:             workItem,
		FromRef:         from,
		Type:            gitworkspace.WorkspaceDev,
		New:             true,
		FromCurrent:     from == "",
		GenerateBranch:  true,
	})
	if err != nil {
		return err
	}
	if p.bool("json") {
		return writeJSON(s.out, "new", workspaceView(workspace), nil)
	}
	initialCommitLine := ""
	if initialCommitCreated {
		initialCommitLine = "Initial commit: created automatically (empty)\n"
	}
	fmt.Fprintf(s.out, "Created Workspace %s\nWork item: %s\nFrom: %s\n%sManaged branch: %s\nWorktree: %s\n\nNext:\n  gws checkout %s\n", workspace.ID, workspace.Name, workspace.Source.FromRef, initialCommitLine, workspace.Source.Ref, workspace.WorktreePath, workspace.ID)
	return nil
}

func runCheckout(ctx context.Context, app *gitworkspace.App, cwd string, p parsedArgs, s ioStreams) error {
	if p.bool("json") {
		return gitworkspace.NewError("config_invalid", "checkout is interactive and does not support --json. Use path --json for platform integration.")
	}
	selector := p.optional(0)
	workspace, _, err := app.ResolveWorkspace(ctx, selector, p.value("project"), cwd, true)
	if err != nil {
		return err
	}
	fmt.Fprintf(s.out, "Entering Workspace %s (%s)\nDirectory: %s\nExit the shell to return.\n", workspace.Name, workspace.ID, workspace.WorktreePath)
	return app.LaunchCheckoutShell(ctx, workspace, s.in, s.out, s.err)
}

func runApply(ctx context.Context, app *gitworkspace.App, cwd string, p parsedArgs, s ioStreams) error {
	result, err := app.ApplyWorkspace(ctx, p.optional(0), p.value("project"), cwd)
	if err != nil {
		return err
	}
	if p.bool("json") {
		return writeJSON(s.out, "apply", applyView(result), nil)
	}
	fmt.Fprintf(s.out, "Applied Workspace %s (%s)\nSource: %s\nTarget: %s\nRepository: %s\n", result.Workspace.Name, result.Workspace.ID, result.SourceBranch, result.TargetBranch, result.Project.RepoPath)
	return nil
}

func runFinish(ctx context.Context, app *gitworkspace.App, cwd string, p parsedArgs, s ioStreams) error {
	selector := p.optional(0)
	if selector == "" {
		return gitworkspace.NewError("config_invalid", "Missing a Workspace argument. Usage: gws finish <workspace-id>. Run gws ls to list candidates.")
	}
	result, err := app.FinishWorkspace(ctx, selector, p.value("project"), cwd)
	if err != nil {
		return err
	}
	if p.bool("json") {
		return writeJSON(s.out, "finish", applyView(result), nil)
	}
	branchAction := "preserved"
	if result.Workspace.Source.Managed {
		branchAction = "removed"
	}
	fmt.Fprintf(s.out, "Finished Workspace %s (%s)\nApplied: %s -> %s\nRemoved: %s\nBranch %s was %s.\n", result.Workspace.Name, result.Workspace.ID, result.SourceBranch, result.TargetBranch, result.Workspace.WorktreePath, result.SourceBranch, branchAction)
	return nil
}

func runInit(ctx context.Context, app *gitworkspace.App, cwd string, p parsedArgs, s ioStreams) error {
	project, updated, err := app.InitProject(ctx, gitworkspace.InitRequest{CWD: cwd, Name: p.value("name"), DefaultBranch: p.value("default-branch")})
	if err != nil {
		return err
	}
	if p.bool("json") {
		return writeJSON(s.out, "init", initData{Project: projectView(project), Updated: updated}, nil)
	}
	action := "Already registered"
	if updated {
		action = "Registered"
	}
	fmt.Fprintf(s.out, "%s Project %s (%s)\nRepository: %s\n", action, project.Name, project.ID, project.RepoPath)
	return nil
}

func runProject(ctx context.Context, app *gitworkspace.App, cwd string, p parsedArgs, s ioStreams) error {
	sub := p.optional(0)
	if sub == "" {
		return gitworkspace.NewError("config_invalid", "Missing a project subcommand. Usage: gws project ls|show|rm.")
	}
	switch sub {
	case "ls":
		projects, err := app.Projects(ctx)
		if err != nil {
			return err
		}
		if p.bool("json") {
			views := make([]projectJSON, len(projects))
			for i, project := range projects {
				views[i] = projectView(project)
			}
			return writeJSON(s.out, "project ls", projectsData{Projects: views}, nil)
		}
		w := tabwriter.NewWriter(s.out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tREPOSITORY\tDEFAULT")
		for _, v := range projects {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", v.ID, v.Name, v.RepoPath, v.DefaultBranch)
		}
		return w.Flush()
	case "show":
		selector := ""
		if len(p.positional) > 1 {
			selector = p.positional[1]
		}
		project, err := app.ResolveProject(ctx, selector, cwd)
		if err != nil {
			return err
		}
		if p.bool("json") {
			return writeJSON(s.out, "project show", projectView(project), nil)
		}
		fmt.Fprintf(s.out, "Project: %s\nID: %s\nRepository: %s\nGit common dir: %s\nDefault branch: %s\nConfig: %s\n", project.Name, project.ID, project.RepoPath, project.GitCommonDir, project.DefaultBranch, project.ConfigPath)
		return nil
	case "rm":
		selector := p.optional(1)
		if selector == "" {
			return gitworkspace.NewError("config_invalid", "Missing a Project argument. Usage: gws project rm <project-id>.")
		}
		if err := app.RemoveProject(ctx, selector); err != nil {
			return err
		}
		if p.bool("json") {
			return writeJSON(s.out, "project rm", removeData{Selector: selector, Removed: true}, nil)
		}
		fmt.Fprintf(s.out, "Removed Project registration %s. Original repository was not modified.\n", selector)
		return nil
	default:
		return gitworkspace.NewError("config_invalid", fmt.Sprintf("Unknown project subcommand %q.", sub))
	}
}

func runCreate(ctx context.Context, app *gitworkspace.App, cwd, command string, p parsedArgs, s ioStreams) error {
	ref := p.optional(0)
	if ref == "" {
		return gitworkspace.NewError("config_invalid", fmt.Sprintf("Missing a branch argument. Usage: gws %s <branch>.", command))
	}
	kind := map[string]gitworkspace.WorkspaceType{"dev": gitworkspace.WorkspaceDev, "review": gitworkspace.WorkspaceReview, "fix": gitworkspace.WorkspaceFix, "test": gitworkspace.WorkspaceTest}[command]
	workspace, err := app.CreateWorkspace(ctx, gitworkspace.CreateRequest{ProjectSelector: p.value("project"), CWD: cwd, Name: p.value("name"), Ref: ref, BaseRef: p.value("base"), FromRef: p.value("from"), Type: kind, New: p.bool("new"), Fetch: p.bool("fetch")})
	if err != nil {
		return err
	}
	if p.bool("json") {
		return writeJSON(s.out, command, workspaceView(workspace), nil)
	}
	fmt.Fprintf(s.out, "Created Workspace %s\nWorktree: %s\nTask: %s\n\nNext:\n  gws checkout %s\n", workspace.ID, workspace.WorktreePath, workspace.TaskFilePath, workspace.ID)
	return nil
}

func runList(ctx context.Context, app *gitworkspace.App, p parsedArgs, s ioStreams) error {
	values, err := app.Workspaces(ctx, p.value("project"))
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
	projects, _ := app.Projects(ctx)
	projectsByID := map[string]gitworkspace.Project{}
	for _, v := range projects {
		projectsByID[v.ID] = v
	}
	w := tabwriter.NewWriter(s.out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tID\tREPOSITORY\tTYPE\tBRANCH\tFROM\tSTATUS")
	for _, v := range values {
		repository := v.ProjectID
		if project, ok := projectsByID[v.ProjectID]; ok {
			repository = project.Name
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", v.Name, v.ID, repository, v.Type, v.Source.Ref, displayValue(v.Source.FromRef), v.State)
	}
	return w.Flush()
}

func runShow(ctx context.Context, app *gitworkspace.App, cwd string, p parsedArgs, s ioStreams) error {
	selector := p.optional(0)
	workspace, project, err := app.ResolveWorkspace(ctx, selector, p.value("project"), cwd, true)
	if err != nil {
		return err
	}
	if p.bool("json") {
		return writeJSON(s.out, "show", showData{Workspace: workspaceView(workspace), Project: projectView(project)}, nil)
	}
	fmt.Fprintf(s.out, "Workspace: %s\nID: %s\nRepository: %s\nRepository path: %s\nGit common dir: %s\nType: %s\nBranch: %s\nFrom: %s\nBase: %s\nManaged branch: %t\nStatus: %s\nWorktree: %s\nTask: %s\nCreated: %s\nUpdated: %s\n", workspace.Name, workspace.ID, project.Name, project.RepoPath, project.GitCommonDir, workspace.Type, workspace.Source.Ref, displayValue(workspace.Source.FromRef), displayValue(workspace.Source.BaseRef), workspace.Source.Managed, workspace.State, workspace.WorktreePath, workspace.TaskFilePath, workspace.CreatedAt.UTC().Format(time.RFC3339), workspace.UpdatedAt.UTC().Format(time.RFC3339))
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
	status, err := app.Status(ctx, selector, p.value("project"), cwd)
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
	fmt.Fprintf(s.out, "Workspace: %s\nGit: %s\nWorktree registered: %t\nTask: %t\nState: %s\n", status.Workspace.ID, gitState, status.WorktreeRegistered, status.TaskExists, status.Workspace.State)
	return nil
}

func runPath(ctx context.Context, app *gitworkspace.App, cwd string, p parsedArgs, s ioStreams) error {
	selector := p.optional(0)
	w, _, err := app.ResolveWorkspace(ctx, selector, p.value("project"), cwd, true)
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
	if err := app.RemoveWorkspace(ctx, selector, p.value("project"), cwd, p.bool("force")); err != nil {
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

func runDoctor(ctx context.Context, app *gitworkspace.App, cwd string, p parsedArgs, s ioStreams) error {
	checks, err := app.Doctor(ctx, p.value("project"), cwd)
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
			return gitworkspace.NewError("doctor_failed", "Doctor found invalid project configuration.")
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
		return gitworkspace.NewError("doctor_failed", "Doctor found invalid project configuration.")
	}
	return nil
}

func runCapabilities(p parsedArgs, s ioStreams) error {
	data := capabilitiesData{
		ProtocolVersion: 1,
		StateVersion:    gitworkspace.CurrentStateVersion,
		Backend:         "git-worktree",
		Features:        []string{"project-registry", "work-item", "managed-branch", "safe-cleanup", "apply", "finish", "json"},
		Interactive:     []string{"checkout"},
	}
	if p.bool("json") {
		return writeJSON(s.out, "capabilities", data, nil)
	}
	fmt.Fprintln(s.out, "Backend: git-worktree")
	fmt.Fprintln(s.out, "Plugin protocol: 1")
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
	Kind     string  `json:"kind"`
	Ref      string  `json:"ref"`
	BaseRef  *string `json:"base_ref"`
	Detached bool    `json:"detached"`
	New      bool    `json:"new"`
	Managed  bool    `json:"managed"`
	FromRef  *string `json:"from_ref"`
}
type workspaceJSON struct {
	ID           string                      `json:"id"`
	ProjectID    string                      `json:"project_id"`
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
	return workspaceJSON{ID: w.ID, ProjectID: w.ProjectID, Name: w.Name, Type: w.Type, Source: sourceJSON{Kind: w.Source.Kind, Ref: w.Source.Ref, BaseRef: pointer(w.Source.BaseRef), Detached: w.Source.Detached, New: w.Source.New, Managed: w.Source.Managed, FromRef: pointer(w.Source.FromRef)}, WorktreePath: w.WorktreePath, TaskFilePath: w.TaskFilePath, State: w.State, LastError: pointer(w.LastError), CreatedAt: w.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: w.UpdatedAt.UTC().Format(time.RFC3339)}
}
func workspaceViews(values []gitworkspace.Workspace) []workspaceJSON {
	result := make([]workspaceJSON, len(values))
	for i, v := range values {
		result[i] = workspaceView(v)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ProjectID != result[j].ProjectID {
			return result[i].ProjectID < result[j].ProjectID
		}
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return result[i].ID < result[j].ID
	})
	return result
}

type projectJSON struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	RepoPath      string `json:"repo_path"`
	GitCommonDir  string `json:"git_common_dir"`
	DefaultBranch string `json:"default_branch"`
	ConfigPath    string `json:"config_path"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type listData struct {
	Workspaces []workspaceJSON `json:"workspaces"`
}

type initData struct {
	Project projectJSON `json:"project"`
	Updated bool        `json:"updated"`
}

type projectsData struct {
	Projects []projectJSON `json:"projects"`
}

type applyData struct {
	Workspace    workspaceJSON `json:"workspace"`
	Project      projectJSON   `json:"project"`
	SourceBranch string        `json:"source_branch"`
	TargetBranch string        `json:"target_branch"`
}

type pathData struct {
	WorkspaceID string `json:"workspace_id"`
	Path        string `json:"path"`
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
	Project   projectJSON   `json:"project"`
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

func projectView(p gitworkspace.Project) projectJSON {
	return projectJSON{p.ID, p.Name, p.RepoPath, p.GitCommonDir, p.DefaultBranch, p.ConfigPath, p.CreatedAt.UTC().Format(time.RFC3339), p.UpdatedAt.UTC().Format(time.RFC3339)}
}
func applyView(v gitworkspace.ApplyResult) applyData {
	return applyData{Workspace: workspaceView(v.Workspace), Project: projectView(v.Project), SourceBranch: v.SourceBranch, TargetBranch: v.TargetBranch}
}
func statusView(v gitworkspace.Status) statusData {
	return statusData{
		Workspace: workspaceView(v.Workspace),
		Git:       gitStatusJSON{RepoExists: v.RepoExists, WorktreeExists: v.WorktreeExists, WorktreeRegistered: v.WorktreeRegistered, Dirty: v.Dirty},
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
	fmt.Fprint(out, `Git Workspace manages task-oriented Git worktrees and their lifecycle.

Usage:
  gws init [--name NAME] [--default-branch BRANCH]
  gws new [NAME] [--from REF]
  gws checkout [WORKSPACE]
  gws apply [WORKSPACE] [--project PROJECT]
  gws finish WORKSPACE [--project PROJECT]
  gws ls [--project PROJECT] [--json]
  gws show|status|path [WORKSPACE] [--project PROJECT]
  gws rm WORKSPACE [--project PROJECT] [--force]
  gws project ls|show|rm
  gws gc [--dry-run]
  gws doctor [--project PROJECT]
  gws capabilities [--json]

Machine interface:
  Add --json to non-interactive commands. checkout is always interactive.

Advanced workspace types:
  gws dev|review|fix|test BRANCH [options]
`)
}
