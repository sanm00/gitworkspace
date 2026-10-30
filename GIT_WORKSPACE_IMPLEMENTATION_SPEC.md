# Git Workspace Implementation Specification

> Status: implementation baseline
> Product: Git Workspace
> Executable: `gws`
> Plugin backend ID: `git-worktree`
> Chinese version: `GIT_WORKSPACE_IMPLEMENTATION_SPEC.zh.md`

## 1. Product definition

Git Workspace is an opinionated Git worktree manager. It carves isolated
Workspaces out of the branch you are on and merges them back safely, so you can
triage a problem or develop in parallel without disturbing your main checkout.

Its value proposition is:

> One-step Workspace creation from the current branch, base tracking, guarded
> cleanup, precondition-checked merge-back, and a versioned machine interface
> on top of Git worktree.

Git Workspace is both:

1. a standalone CLI for developers using parallel working directories; and
2. a source-workspace plugin that a development-environment platform can invoke
   before it provisions runtimes, ports, containers, tools, or agents.

Git Workspace is not itself a complete development-environment isolation platform.

## 2. Product boundaries

Git Workspace owns:

- repository-scoped Workspace metadata and resolution;
- base ref/base commit recording for every Workspace type;
- Git worktree creation and removal;
- automatic, collision-free managed branches in the `gws/` namespace;
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

### 3.1 Repository scope

There is no registry. The repository containing `cwd` (identified by its Git
common directory) is the scope of every command. All Workspaces, metadata, and
locks live under `<git-common-dir>/gws/`, so every linked worktree of the same
repository shares one state, and unrelated repositories can never see each
other's Workspaces. Commands outside a Git repository fail with
`repository_not_found`.

### 3.2 Work item

A work item is an optional human-readable label for the user's intent, such as
`feature/avatar` or `issue-328`. Its name is not a Git branch name and must not
be coupled to branch existence. When omitted, the implementation derives a
label from the base branch slug, falling back to `workspace`; review and test
types prefix the label with `review-`/`test-`.

The normalized label is retained in Workspace metadata. Path components are
preserved by normalization: `feature/avatar` becomes `feature-avatar`, not just
`avatar`.

### 3.3 Workspace

A Workspace is a managed record linking:

- one work item and stable Workspace ID;
- one Git worktree path;
- one source ref plus recorded `base_ref` and `base_commit`;
- one task file;
- type, ownership, lifecycle state, error, and timestamps.

Lifecycle states are `creating`, `ready`, `error`, and `removing`.

Workspace types are `dev`, `fix`, `review`, and `test`. Review and test
Workspaces are detached and cannot be applied. `--detach` produces a detached
worktree of any type.

Managed branches carry the `gws/` namespace and an ownership flag. Only flagged
branches are ever deleted automatically, and `gc` never deletes branches.

## 4. Primary workflow

### 4.1 Create

```bash
gws new [NAME] [--base BRANCH|SHA] [--type dev|fix|review|test] [--detach] [--fetch] [--no-enter] [--json]
```

`new` creates a Workspace based on:

1. `--base <branch|sha>` when given; otherwise
2. the current branch when `cwd` is attached inside this repository; otherwise
3. `default_branch` from `.git/gws/config.json`; otherwise a
   `git_ref_not_found` error.

It records both the base ref and the resolved base commit for every type, so
inspection output and task files always show the exact baseline
(`Based on: <base> (<sha>)`).

For `dev`/`fix` it allocates a unique managed branch `gws/<name>` at the base
commit. For `review`/`test` (or `--detach`) the worktree is detached at the
resolved base commit. `--fetch` refreshes remote refs first and is rejected for
`dev`/`fix`.

After creation, `new` enters the user's shell in the worktree unless `--json`,
`--no-enter`, or non-TTY stdout is used; in those cases it prints
`Next: gws use <id>` instead. Multiple Workspaces may share a label; their
IDs, branches, and paths must remain unique, and ambiguous name selectors
require a Workspace ID.

Creating a Workspace directly on top of an existing user branch is not
supported: Git forbids attaching two worktrees to one branch, so every
development Workspace gets its own managed branch.

### 4.2 Enter

```bash
gws use [WORKSPACE]
```

`use` starts the user's shell with the worktree as its working directory. It
cannot change the parent shell's directory. The state lock must be released
before the child shell starts. Selectors accept exact ID, exact name, or an
unambiguous ID prefix; an omitted selector resolves from `cwd`. `use` is
interactive-only and rejects `--json`.

### 4.3 Inspect

```text
gws ls
gws show [WORKSPACE]
gws status [WORKSPACE]
gws path [WORKSPACE]
gws diff [WORKSPACE] [--staged] [--stat] [--uncommitted]
gws log [WORKSPACE] [--limit N] [--stat]
gws commit --message MESSAGE [-a|--all]
```

`diff`, `log`, and `commit` are thin wrappers that resolve the Workspace from
`cwd` (or an explicit selector) and run Git inside its worktree, so callers never
repeat the path or look up the base commit:

- `diff` prints `git diff <base_commit>`, i.e. everything the Workspace changed
  since its base, including committed work. `--uncommitted` switches the
  comparison to `HEAD`; `--staged` compares the index; `--stat` summarizes.
  Because Git never reports untracked files in a diff, they are listed
  separately.
- `log` prints `git log --oneline <base_commit>..HEAD`, limited by `--limit`.
- `commit` runs `git commit -m` inside the Workspace. `--all` (`-a`) stages
  every change first with `git add -A`, including untracked files, then commits;
  without it only pre-staged changes are committed and an empty index is reported
  as `workspace_nothing_staged` with a hint to use `--all`, instead of surfacing
  a raw Git failure. It refuses detached review/test Workspaces and reports an
  empty message as an error instead of prompting.

These commands only read Git state apart from `commit`, which never changes
Workspace metadata.

`show` presents persisted metadata including base ref and base commit. `status`
also verifies the filesystem, Git worktree registration, dirty state, and
task-file existence, and reports the base. When the worktree is missing, JSON
reports `git.dirty` as `null`; a failed Git status check is returned as an
error rather than being reported as clean.

Inside a managed worktree, an omitted Workspace selector resolves from `cwd`.

### 4.4 Apply and finish

```text
gws apply [--into BRANCH]
gws finish [WORKSPACE] [--into BRANCH]
```

`apply` must run from inside the Workspace; the selector is always resolved
from `cwd` and positional arguments are rejected. Its merge target is the
recorded base branch, overridden by `--into <branch>`.

`finish` may run from the main checkout or from inside the Workspace. Inside a
Workspace the positional argument is optional and defaults to that Workspace, and
the result reports `cwd_inside`.
A subshell launched by `new`/`use` carries `GWS_WORKSPACE_SHELL=<id>`; when it
also sets `GWS_WORKSPACE_WRAPPED=1` (zsh and bash), `LaunchShell` points the shell
at a generated startup file that sources the user's own rc and then defines a
`gws` wrapper function. A successful in-place `finish` returns the sentinel exit
code `42`; the wrapper turns it into `exit 0`, so the subshell closes and the
interactive parent resumes in its original directory. Signals cannot do this
because interactive shells ignore `SIGTERM`/`SIGHUP`, and a child process cannot
change its parent shell's directory. Without the wrapper (other shells, `--json`,
or invoking the binary by absolute path) the CLI prints the `cd` command instead. It applies with the
same target rules, and starts cleanup only after merge success. It removes the
worktree, task metadata, and the managed branch when ownership is recorded.

Both commands validate all preconditions before touching anything and stop
with a structured blocking reason (`apply_blocked` family):

- not inside a Workspace (apply); `finish` accepts either location;
- Workspace state is not `ready`;
- detached review/test Workspace;
- Workspace worktree dirty, missing, or unregistered;
- no target: base is a pure SHA and no `--into` was given;
- source equals target;
- main checkout detached, or not on the target branch (never auto-switched);
- main checkout dirty;
- merge conflict — the conflict is left in the main checkout for normal Git
  resolution or `git merge --abort`, and the Workspace is preserved.

Blocking errors include `workspace_id` and `source_branch` details and an
actionable message naming the exact Git command or `--into` alternative.

### 4.5 Remove and recover

```text
gws rm WORKSPACE [--force]
gws gc [--dry-run]
gws doctor
```

Release removes every artifact `gws` created: the worktree through
`git worktree remove`, its managed branch, the task file, the Workspace record,
and the worktree root once it is empty. A vanished but still registered
worktree is pruned instead of failing the release.

Normal removal refuses a dirty worktree and a managed branch with commits not
yet merged into its base branch (`workspace_unmerged`). `--force` skips only
those two checks. It never skips canonical-path and ownership validation.

Garbage collection acts only on resources whose Git Workspace ownership can be
proven. Unknown resources are reported and preserved; branches are never
deleted by `gc`. `--dry-run` is the recommended first step.

`doctor` self-checks the current repository: Git availability, config file,
state directory, and Workspace/worktree consistency.

## 5. Resolution rules

Repository resolution: `git rev-parse --show-toplevel` plus
`--git-common-dir` from `cwd`. No selector, no registry, no cross-repository
implicit search.

Workspace resolution accepts exact ID, exact name, and—where explicitly
enabled—an unambiguous ID prefix. Ambiguity is an error whose structured
details include the candidate Workspaces.

## 6. State and configuration

All state lives inside the repository:

```text
<repo>/.git/gws/
├── config.json          optional, {"version":1,"default_branch":"main"}
├── workspaces/<id>.json
├── tasks/<id>.md
└── locks/state.lock

<repo>/.git/gws/worktrees/<id>/    worktree checkout (inside the Git directory)
```

Because metadata hangs off the Git common directory, all linked worktrees share
one state; cloning the repository carries nothing over. `config.json` is
optional and never created automatically; its only field is the fallback
`default_branch` used when HEAD is detached and no `--base` is given.

Persisted Workspace records carry `state_version: 1`. Unversioned legacy
records are interpreted as version 1. A record with a newer unsupported version
is rejected with `state_version_unsupported` and must not be overwritten.

All state files use atomic same-directory replacement. Mutations hold the
repository-wide exclusive advisory lock; reads hold a shared lock. Lock
acquisition is bounded and cancellation-aware.

## 7. Safety invariants

Git Workspace must never:

- recursively delete the user home, repository root, repository parent, state
  root, or worktree root;
- remove a directory outside the canonical configured worktree root;
- remove a path whose Workspace ownership cannot be proven;
- delete an unowned user branch;
- auto-switch the main checkout's branch during apply/finish;
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

The response declares `protocol_version: 2`, backend `git-worktree`, features,
and interactive-only commands (`use`, and `new` without `--json`/`--no-enter`).
`capabilities` runs outside a repository.

### 8.2 JSON envelope

Every non-interactive lifecycle and inspection command accepts `--json`:

```json
{
  "schema_version": 2,
  "command": "path",
  "data": {
    "workspace_id": "avatar-8a10",
    "path": "/home/user/projects/example/.git/gws/worktrees/avatar-8a10"
  },
  "error": null
}
```

Failures write one JSON envelope to stdout, write nothing to stderr, and exit
non-zero. `data` is null and `error` contains stable `code`, `message`, and
`details` fields. Workspace objects expose `source.base_ref`,
`source.base_commit`, `detached`, `managed`, and `from_ref`.

Human-readable output is not a plugin API and may evolve.

`use` is interactive and rejects `--json`. Platforms call `path --json` and
launch their own process/container/agent in the returned directory. `new
--json` creates without entering a shell.

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
- Every Workspace records and displays its base ref and base commit.
- `new` is independent of same-named user branches.
- `apply` preserves the Workspace; `finish` cleans up only after success;
  neither auto-switches the main checkout.
- Dirty and unowned paths are protected from deletion.
- Human inspection output and JSON Schema v2 expose Workspace, branch, base,
  state, worktree, and task data.
- `capabilities --json` exposes plugin protocol version 2.
- Unit, integration, race, vet, and build checks pass.
