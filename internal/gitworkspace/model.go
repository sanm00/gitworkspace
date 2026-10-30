package gitworkspace

import "time"

const CurrentStateVersion = 1

type WorkspaceType string

const (
	WorkspaceDev    WorkspaceType = "dev"
	WorkspaceReview WorkspaceType = "review"
	WorkspaceFix    WorkspaceType = "fix"
	WorkspaceTest   WorkspaceType = "test"
)

type WorkspaceState string

const (
	StateCreating WorkspaceState = "creating"
	StateReady    WorkspaceState = "ready"
	StateError    WorkspaceState = "error"
	StateRemoving WorkspaceState = "removing"
)

type SourceSpec struct {
	Kind       string `json:"kind"`
	Ref        string `json:"ref"`
	BaseRef    string `json:"base_ref,omitempty"`
	BaseCommit string `json:"base_commit,omitempty"`
	Detached   bool   `json:"detached"`
	New        bool   `json:"new,omitempty"`
	Managed    bool   `json:"managed,omitempty"`
	FromRef    string `json:"from_ref,omitempty"`
}

type Workspace struct {
	StateVersion int            `json:"state_version"`
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Type         WorkspaceType  `json:"type"`
	Source       SourceSpec     `json:"source"`
	WorktreePath string         `json:"worktree_path"`
	TaskFilePath string         `json:"task_file_path"`
	State        WorkspaceState `json:"state"`
	LastError    string         `json:"last_error,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type Repository struct {
	Root          string
	MainRoot      string
	GitCommonDir  string
	CurrentBranch string
}

type Status struct {
	Workspace          Workspace
	RepoRoot           string
	WorktreeExists     bool
	WorktreeRegistered bool
	Dirty              *bool
	TaskExists         bool
}

type ApplyResult struct {
	Workspace    Workspace
	RepoRoot     string
	SourceBranch string
	TargetBranch string
	CWDInside    bool
}

type DiffResult struct {
	Workspace Workspace
	Base      string
	Content   string
	Untracked []string
}

type LogResult struct {
	Workspace Workspace
	Base      string
	Content   string
}

type CommitResult struct {
	Workspace Workspace
	Commit    string
	Subject   string
}

type GCAction struct {
	Kind        string `json:"kind"`
	Path        string `json:"path"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Action      string `json:"action"`
	Reason      string `json:"reason"`
	Applied     bool   `json:"applied"`
}
