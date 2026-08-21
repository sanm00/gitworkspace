# Git Workspace Implementation Specification

> Status: implementation baseline
> Product: Git Workspace
> Executable: `gws`
> Plugin backend ID: `git-worktree`
> Chinese version: `GIT_WORKSPACE_IMPLEMENTATION_SPEC.zh.md`

## 1. Product definition

Git Workspace is an opinionated Git worktree manager. It turns a development work
item into a named, inspectable, and safely disposable Workspace.

Its value proposition is:

> A clear work-item model, automatic branch policy, guarded cleanup, and an
> explicit apply/finish workflow on top of Git worktree.

Git Workspace is both:

1. a standalone CLI for developers using parallel working directories; and
2. a source-workspace plugin that a development-environment platform can invoke
   before it provisions runtimes, ports, containers, tools, or agents.

Git Workspace is not itself a complete development-environment isolation platform.

## 2. Product boundaries

Git Workspace owns:

- repository registration and current-project resolution;
- work-item identity and Workspace metadata;
- Git worktree creation and removal;
- automatic, collision-free managed branches;
- detached review/test worktrees;
- task context files;
- state locking, rollback, status verification, and garbage collection;
- applying committed work and finishing a Workspace safely;
- a versioned process-level JSON interface.

Git Workspace does not own:

- editor or coding-agent selection and launch;
- Docker Compose, devcontainers, VMs, or sandbox engines;
- host-process supervision;
- port, database, network, secret, or credential isolation;
- dependency installation or setup hooks;
- remote pull-request lifecycle or CI orchestration.

An upper-layer platform may implement these capabilities around the worktree
path returned by Git Workspace.

## 3. Domain model

### 3.1 Project

A Project registers one canonical Git common directory and its original
checkout. It contains a stable ID, display name, repository path, Git common
directory, default branch, configuration path, and timestamps.

Two paths belonging to the same Git common directory are the same Project.
`init` is idempotent and must not duplicate it.

### 3.2 Work item

A work item is an optional human-readable label for the user's intent, such as
`feature/avatar` or `issue-328`. Its name is not a Git branch name and must not
be coupled to branch existence. When omitted by `gws new`, the implementation
uses `workspace` as the generated label.

The normalized work-item name is retained in Workspace metadata. Path
components are preserved by normalization: `feature/avatar` becomes
`feature-avatar`, not just `avatar`.

### 3.3 Workspace

A Workspace is a managed record linking:

- one Project;
- one work item and stable Workspace ID;
- one Git worktree path;
- one source ref and optional base/from refs;
- one task file;
- type, ownership, lifecycle state, error, and timestamps.

Lifecycle states are `creating`, `ready`, `error`, and `removing`.

Workspace types are `dev`, `fix`, `review`, and `test`. Review and test
Workspaces are detached and cannot be applied.

## 4. Primary workflow

### 4.1 Register

```bash
gws init [--name NAME] [--default-branch BRANCH]
```

`init` treats `cwd` itself as the Project root. If that exact directory is not
already a Git repository root, it initializes one there first, including when
`cwd` is nested inside a parent repository. The configured or explicit default
branch is used as the initial branch when present. Re-running `init` for the
same Git common directory returns the same Project ID and updates only mutable
explicit/configured fields. Project names remain unchanged; new Project IDs
use the name slug plus a random suffix to avoid collisions and concurrent
registration conflicts.

Project registration can be inspected or removed with `gws project ls`,
`gws project show`, and `gws project rm`. Removing a Project removes only its
registration after verifying that it has no Workspaces.

### 4.2 Create

```bash
gws new [NAME] [--from REF]
```

`new` creates a dev Workspace from the current branch, unless `--from`
overrides it. It allocates a unique branch with the `gws/` prefix and
marks that branch as managed.

The Project is always resolved from `cwd`; `new` does not accept `--project`.
If `cwd` is not itself a repository root or is not registered, `new` initializes
and registers it automatically. If the repository has no commits, `new` creates
an empty root commit before creating the worktree. This operation must preserve
the existing index and must not include staged changes in the generated commit.

No same-named user branch may block `new` because `NAME` is a work-item label.
Detached `cwd` requires an explicit `--from`.

Multiple Workspaces may share a work-item name. Their IDs, branches, and paths
must remain unique; ambiguous name selectors require a Workspace ID.

Advanced explicit-ref entry points remain available:

```text
gws dev BRANCH [--new] [--from REF]
gws fix BRANCH [--new] [--from REF]
gws review BRANCH [--base REF] [--fetch]
gws test BRANCH [--base REF]
```

Only `review --fetch` performs an implicit network fetch.

### 4.3 Enter

```bash
gws checkout [WORKSPACE]
```

`checkout` starts the user's shell with the worktree as its working directory.
It cannot change the parent shell's directory. The state lock must be released
before the child shell starts.

### 4.4 Inspect

```text
gws ls [--project PROJECT]
gws show [WORKSPACE]
gws status [WORKSPACE]
gws path [WORKSPACE]
```

`show` presents persisted metadata. `status` also verifies the filesystem, Git
worktree registration, dirty state, and task-file existence. When the worktree is
missing, JSON reports `git.dirty` as `null`; a failed Git status check is returned as
an error rather than being reported as clean.

Inside a managed worktree, an omitted Workspace selector resolves from `cwd`.

### 4.5 Apply and finish

```text
gws apply [WORKSPACE]
gws finish WORKSPACE
```

`apply` merges the clean, committed, attached Workspace branch into the branch
currently checked out in the registered original checkout. It preserves the
Workspace.

Without a Workspace selector, `apply` first selects the Workspace containing
`cwd`. If `cwd` is not inside a Workspace, it scopes candidates to the Project
resolved from `cwd` and automatically selects only when exactly one ready,
attached Workspace exists. Multiple candidates are an ambiguity error whose
message and structured details include their IDs. Workspaces from other
Projects must never be selected implicitly.

`finish` performs the same apply operation and starts cleanup only after merge
success. It removes the worktree and task metadata, and deletes the source
branch only when metadata marks it as Git Workspace-managed.

Both commands reject:

- dirty Workspace worktrees;
- detached review/test Workspaces;
- a dirty or detached original checkout;
- execution of `finish` from inside the Workspace being removed.

A merge conflict remains in the original checkout for normal Git conflict
resolution or `git merge --abort`; the Workspace is preserved.

### 4.6 Remove and recover

```text
gws rm WORKSPACE [--project PROJECT] [--force]
gws gc [--dry-run]
gws doctor [--project PROJECT]
```

Normal removal refuses a dirty worktree. `--force` skips only the dirty check.
It never skips canonical-path and ownership validation.

Without `--project`, `rm` resolves the Project from `cwd` first and searches
only that Project's Workspaces. An explicit `--project` is required to remove a
Workspace belonging to another registered repository.

Garbage collection acts only on resources whose Git Workspace ownership can be
proven. Unknown resources are reported and preserved. `--dry-run` is the
recommended first step.

## 5. Resolution rules

Project resolution order:

1. explicit `--project` selector;
2. Workspace containing `cwd`;
3. registered Project with the same Git common directory as `cwd`;
4. otherwise return a resolution error suggesting `gws init` or
   `--project`.

Git Workspace must not select a Project merely because only one is registered.

`new` is intentionally stricter than the general rule: it always uses `cwd` and
rejects `--project`. `rm` uses `cwd` by default and permits `--project` only as
an explicit cross-repository operation.

Workspace resolution accepts exact ID, exact name, and—where explicitly
enabled—an unambiguous ID prefix. Ambiguity is an error.

## 6. State and configuration

Default state root:

```text
~/.git-workspace/
├── projects/
├── workspaces/
├── worktrees/
├── tasks/
└── locks/state.lock
```

`GIT_WORKSPACE_HOME` overrides the root.

Repository configuration is optional and read from the registered original
checkout:

```yaml
version: 1

project:
  name: example
  default_branch: main
```

No agent, editor, or runtime section is accepted. Project fields are imported
by `init`; commands do not continuously mutate registration from config.

Persisted Project and Workspace records carry `state_version: 1`. Unversioned
legacy records are interpreted as version 1. A record with a newer unsupported
version is rejected with `state_version_unsupported` and must not be overwritten.

All state files use atomic same-directory replacement. Mutations hold the
global exclusive advisory lock; reads hold a shared lock. Lock acquisition is
bounded and cancellation-aware.

## 7. Safety invariants

Git Workspace must never:

- recursively delete the user home, repository root, state root, or worktree
  root;
- remove a directory outside the canonical configured worktree root;
- remove a path whose Workspace ownership cannot be proven;
- delete an unowned user branch;
- hide a failed apply behind successful cleanup;
- trust persisted state without checking real Git/filesystem state where a
  destructive action depends on it.

Creation records metadata before external side effects and rolls back in
reverse order. Incomplete rollback leaves `error` metadata with a diagnostic.

## 8. Platform/plugin protocol

The plugin boundary is the executable process, which keeps the core independent
of platform language and runtime.

### 8.1 Discovery

```bash
gws capabilities --json
```

The response declares `protocol_version: 1`, backend `git-worktree`, features,
and interactive-only commands.

### 8.2 JSON envelope

Every non-interactive lifecycle and inspection command accepts `--json`:

```json
{
  "schema_version": 2,
  "command": "path",
  "data": {
    "workspace_id": "avatar-8a10",
    "path": "/home/user/.git-workspace/worktrees/example/avatar-8a10"
  },
  "error": null
}
```

Failures write one JSON envelope to stdout, write nothing to stderr, and exit
non-zero. `data` is null and `error` contains stable `code`, `message`, and
`details` fields.

Human-readable output is not a plugin API and may evolve.

`checkout` is interactive and rejects `--json`. Platforms call `path --json`
and launch their own process/container/agent in the returned directory.

### 8.3 Ownership split

```text
Platform task/runtime ID
        |
        v
Workspace ID ----> worktree path
        |                         |
        |                         +--> platform runtime/resources
        +--> branch + metadata
```

The platform stores the Workspace ID as its foreign key. Git Workspace does
not store platform runtime IDs. Runtime teardown must occur before the platform
requests `finish`, `rm`, or destructive GC.

## 9. Current completion criteria

- All help, prompts, task files, branches, config defaults, binary paths, and
  documentation use Git Workspace naming.
- `new` is independent of same-named user branches.
- `apply` preserves the Workspace; `finish` cleans up only after success.
- Dirty and unowned paths are protected from deletion.
- Human inspection output and JSON Schema v2 expose Project, Workspace, branch,
  base/from, state, worktree, and task data.
- `capabilities --json` exposes plugin protocol version 1.
- Unit, integration, race, vet, and build checks pass.
