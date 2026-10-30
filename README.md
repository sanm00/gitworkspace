# Git Workspace

[![CI](https://github.com/sanm00/gitworkspace/actions/workflows/ci.yml/badge.svg)](https://github.com/sanm00/gitworkspace/actions/workflows/ci.yml)
[![Security](https://github.com/sanm00/gitworkspace/actions/workflows/security.yml/badge.svg)](https://github.com/sanm00/gitworkspace/actions/workflows/security.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

[简体中文](README.zh.md)

Git Workspace (`gws`) carves isolated worktrees out of the branch you are on and
merges them back safely. It exists for two moments: triaging a problem while you
keep developing, and working on several things at once in the same repository.

> **Project status:** pre-1.0. CLI and JSON compatibility are treated carefully,
> but incompatible changes may still be introduced with migration notes.

## Product boundary

Git Workspace owns the lifecycle of source workspaces:

```text
base branch -> managed branch (gws/<name>) -> Git worktree -> apply or finish
```

It does not launch coding agents, editors, containers, databases, host
processes, or network services. The filesystem isolation comes from Git
worktree; `gws` adds naming, base tracking, locking, rollback, ownership
checks, diagnostics, and lifecycle policy.

## Install

```bash
go install github.com/sanm00/gitworkspace/cmd/gws@latest
gws --version
```

Prebuilt macOS and Linux archives for amd64 and arm64 are attached to GitHub
Releases. Verify downloaded archives against `SHA256SUMS` before installing.

## Build

Requirements: Go 1.25+, Git, and macOS or Linux.

```bash
make test
make lint
make build
./bin/gws --help
```

Local builds read the version from the repository's `VERSION` file and inject
it into the binary. Override it only when testing a different build label:

```bash
make build VERSION=v0.1.0-test
```

## Quick start

You are developing on `feature-x` and need a second, isolated copy of the same
baseline — to triage a bug, or to try something without disturbing your work:

```bash
cd ~/projects/example
git checkout feature-x
gws new
```

`gws new` needs no arguments. It bases the Workspace on the current branch,
creates a managed branch `gws/<name>` at that exact commit, opens a worktree in
a sibling directory (`../example-gws/<id>/`), stores its metadata under
`.git/gws/`, and drops you into a shell there. When you are done, `exit`.

Every command reports the baseline it used:

```text
Created Workspace feature-x-9f2c
Type: dev
Based on: feature-x (1a2b3c4d)
Branch: gws/feature-x
Worktree: /Users/you/projects/example-gws/feature-x-9f2c
```

To enter an existing Workspace later:

```bash
gws use feature-x-9f2c     # by ID, prefix, or name
```

Inside the Workspace, commit your changes as usual. Then merge them back:

```bash
gws apply                  # merge into the base branch; keeps the Workspace
```

`apply` runs from inside the Workspace and targets its base branch by default
(`--into <branch>` overrides). Before merging it validates every precondition —
clean worktrees, target branch checked out in the main checkout, no conflicts —
and stops with an actionable reason instead of touching anything:

```text
! The main checkout ... is on branch "main", not the target "feature-x".
  Run: git -C ... checkout feature-x, or apply into another branch with --into main.
```

When you are finished for good, run from the main checkout:

```bash
gws finish feature-x-9f2c  # merge back, then remove worktree and managed branch
```

`finish` applies first and cleans up only after a successful merge.

## Other workspace types

```bash
gws new --base pull/123/head --type review   # detached, read-only inspection
gws new --base v0.9.0 --type test            # detached, reproduce against a tag
gws new hotfix --base main --type fix        # managed branch off main, like dev
```

`review` and `test` worktrees are detached: they cannot be applied, which is
the safety guarantee for read-only triage. `--fetch` refreshes remote refs
first and is accepted only with these two types.

## Everyday Git, wrapped

`gws` also wraps the Git commands you run most inside a Workspace, so you do not
have to remember the base commit or re-type its path:

```bash
gws commit -m "fix avatar upload" -a   # commit inside the Workspace
gws diff                              # everything this Workspace changed since its base
gws diff --uncommitted                # only what is not committed yet
gws diff --staged --stat              # staged changes as a summary
gws log -n 5                          # commits on top of the base
```

`gws diff` prints ordinary `git diff` output and lists untracked files
separately, because Git never includes them in a diff. `gws commit` refuses
detached review/test Workspaces, which are read-only by design.

## Core commands

```text
gws new [NAME] [--base BRANCH|SHA] [--type dev|fix|review|test] [--detach] [--fetch] [--no-enter] [--json]
gws use [WORKSPACE]
gws commit --message MESSAGE [-a] [--json]
gws diff [WORKSPACE] [--staged] [--stat] [--uncommitted] [--json]
gws log [WORKSPACE] [-n N] [--stat] [--json]
gws apply [--into BRANCH]
gws finish WORKSPACE [--into BRANCH]
gws ls | show | status | path [WORKSPACE]
gws rm WORKSPACE [--force]
gws gc [--dry-run]
gws doctor
gws capabilities [--json]
```

`--help`, `--version`, and `capabilities` work anywhere; every other command
must run inside a Git repository.

## Safety model

- State mutations are serialized with an inter-process lock.
- Workspace creation records `creating` state and rolls back partial resources.
- Managed branches live in the `gws/` namespace and carry an ownership flag;
  only owned branches are ever deleted automatically.
- `apply`/`finish` check all preconditions first and report blocking reasons
  instead of half-applying; `finish` never cleans up when the merge fails.
- Normal removal refuses dirty worktrees. `--force` bypasses only the dirty
  check, never path ownership checks.
- `gc` prunes metadata for vanished worktrees but never deletes user branches.

## Platform/plugin integration

All non-interactive commands accept `--json` and emit a versioned envelope
(schema version 2, protocol version 2):

```bash
gws new issue-328 --no-enter --json
gws show issue-328 --json
gws path issue-328 --json
gws finish issue-328 --json
```

`use` is intentionally interactive and rejects `--json`. A platform uses
`path --json`, then launches its own runtime with that path as the working
directory. With `--json` (or when stdout is not a terminal), `new` creates the
Workspace without entering a shell. See
[GIT_WORKSPACE_IMPLEMENTATION_SPEC.md](GIT_WORKSPACE_IMPLEMENTATION_SPEC.md)
for the contract.

Packages under `internal/` are not a public Go API. The supported integration
interface is the versioned `gws --json` subprocess protocol.

## AI agent integration

`gws` is built for coding agents: create a disposable worktree, let the agent
work there, then merge through a gate that refuses half-finished results.

### Install the skill

The bundled Agent Skill teaches any client (opencode, Claude Code, Codex, ...)
the workflow and the error-code routing. Copy it into your agent's skills
directory:

```bash
git clone https://github.com/sanm00/gitworkspace /tmp/gitworkspace
mkdir -p ~/.agents/skills
cp -R /tmp/gitworkspace/skills/git-workspace ~/.agents/skills/git-workspace
```

opencode reads `~/.agents/skills/`; other clients use their own location, for
example `~/.claude/skills/`. The skill triggers on requests like "work on this
in isolation", "open a workspace", "review this ref without touching my branch",
or their Chinese equivalents.

### Or paste this into your agent

If you would rather not install a skill, give the agent this prompt once; it
installs the tool and learns the loop:

```text
Install Git Workspace with `go install github.com/sanm00/gitworkspace/cmd/gws@latest`
and verify with `gws --version`. From now on, when I ask you to work on a task in
isolation, run `gws new <name> --no-enter --json`, do all edits inside the returned
`data.worktree_path`, inspect with `gws diff`/`gws log`, deliver with
`git -C <path> add -A && gws commit -m "..."` followed by `gws apply --json` from
inside the worktree, and route on `error.code` instead of retrying blindly. Never
run `gws use` (interactive) and never switch branches in my main checkout by hand.
For read-only review or repro, use `gws new <name> --base <ref> --type review
--no-enter --json`.
```

Cloning this repository at all also works: an agent dropped into the checkout
reads [`AGENTS.md`](AGENTS.md) and
[`skills/git-workspace/SKILL.md`](skills/git-workspace/SKILL.md) automatically.

### What the agent gets

- `gws new --no-enter --json` returns the worktree path, id, and base commit, so
  the agent never has to parse porcelain output.
- Errors are structured: `merge_conflict`, `target_dirty`,
  `apply_target_not_checked_out`, `workspace_dirty`, and so on, each with a
  stable `code` and `details`.
- Read-only `review`/`test` workspaces physically cannot be applied, which makes
  automated code review safe.
- Inter-process locking means several agents can each hold their own workspace
  without stomping on shared state.

No MCP server is required — agents use gws through their shell tool. A thin MCP
shim over the same `--json` protocol is a possible future addition, not a
prerequisite.

## Configuration and state

Everything `gws` persists lives inside the repository:

```text
<repo>/.git/gws/config.json          optional configuration
<repo>/.git/gws/workspaces/<id>.json workspace metadata
<repo>/.git/gws/tasks/<id>.md        task brief for coding agents
<repo>/.git/gws/locks/state.lock     inter-process lock
<parent>/<repo>-gws/<id>/            worktree checkout
```

Metadata sits under the Git common directory, so all worktrees of one
repository share the same state, and cloning the repository carries nothing
over. Optional configuration is `.git/gws/config.json`:

```json
{ "version": 1, "default_branch": "main" }
```

`default_branch` is the fallback base when HEAD is detached and no `--base` is
given. `gws` never creates this file automatically.

Persisted records carry a state format version. Legacy unversioned records are
read as version 1; records from newer unsupported formats are rejected rather
than overwritten.

## Contributing and security

See [CONTRIBUTING.md](CONTRIBUTING.md) for development and pull-request checks.
Report vulnerabilities privately according to [SECURITY.md](SECURITY.md). This
project is available under the [MIT License](LICENSE).
