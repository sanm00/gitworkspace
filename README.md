# Git Workspace

[![CI](https://github.com/sanm00/gitworkspace/actions/workflows/ci.yml/badge.svg)](https://github.com/sanm00/gitworkspace/actions/workflows/ci.yml)
[![Security](https://github.com/sanm00/gitworkspace/actions/workflows/security.yml/badge.svg)](https://github.com/sanm00/gitworkspace/actions/workflows/security.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

[简体中文](README.zh.md)

Git Workspace is a Git worktree manager with a work-item model, automatic branch
policy, guarded cleanup, and an explicit apply/finish workflow.

It is useful on its own and can also serve as the source-workspace plugin of a
larger development-environment isolation platform.

> **Project status:** pre-1.0. CLI and JSON compatibility are treated carefully,
> but incompatible changes may still be introduced with migration notes.

## Product boundary

Git Workspace owns the lifecycle of source workspaces:

```text
work item -> managed branch -> Git worktree -> apply or finish
```

Git Workspace does not launch coding agents, editors, containers, databases, host
processes, or network services. A platform may resolve a managed Workspace and
provision those resources around its worktree path.

The filesystem isolation comes from Git worktree. Git Workspace adds naming,
metadata, locking, rollback, ownership checks, diagnostics, and lifecycle
policy; it does not claim process, port, dependency, credential, or runtime
isolation.

## Install

Install the latest tagged version with Go:

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

Initialize Git when needed and register the current directory as a Project:

```bash
cd ~/projects/example
gws init --name example --default-branch main
```

`init` always treats the current directory as the Project root. If that exact
directory is not already a Git repository root, it runs `git init` there first,
including when the directory is nested inside a parent repository. The
configured or explicitly requested default branch is used as the initial branch.

Create a Workspace from the current branch and enter its child shell. The work
item name is optional:

```bash
gws new feature/avatar
gws checkout feature-avatar
```

Running `gws new` without a name uses `workspace` as its generated label. The
randomized Workspace ID and managed branch remain unique.

`new` creates a globally unique branch managed by Git Workspace. When supplied,
the work-item name is only a human-readable label, not a Git branch name, so an
existing `feature/avatar` branch does not conflict.
The same work-item name may have multiple Workspaces; use the displayed ID when
a name is ambiguous.

`new` always selects the repository from the current directory and does not accept
`--project`. It automatically registers the current repository when needed. Use
`--from REF` when the Workspace should start from a ref other than the current
branch. If the repository has no commits, `new` creates an empty initial commit
automatically without committing or clearing staged changes. If automatic
registration encounters a parent/current-directory relationship, `new` still
initializes and registers the current directory.

Commit changes inside the Workspace, then choose one of these outcomes:

```bash
gws apply
```

`apply` merges the committed Workspace branch into the branch currently checked
out in the registered original repository and keeps the Workspace. Without a
Workspace argument, it uses the Workspace containing the current directory;
otherwise it selects the sole applicable Workspace belonging to the current
Project. Multiple candidates produce an error listing their IDs.

```bash
exit
gws finish feature-avatar
```

`finish` applies first and removes the worktree and managed branch only after a
successful merge.

## Core commands

```text
gws init
gws new
gws checkout
gws apply
gws finish
gws ls|show|status|path
gws rm
gws project ls|show|rm
gws gc
gws doctor
gws capabilities
```

Advanced `dev`, `review`, `fix`, and `test` commands attach explicit branches or
create detached inspection worktrees.

## Safety model

- State mutations are serialized with an inter-process lock.
- Workspace creation records `creating` state and rolls back partial resources.
- Normal removal refuses dirty worktrees.
- `gws rm` is scoped to the current repository unless `--project` is explicit.
- `--force` bypasses only the dirty check, never path ownership checks.
- `finish` does not clean up when apply fails.
- Only branches explicitly marked as owned by Git Workspace are automatically deleted.
- `gc --dry-run` reports cleanup actions before they are applied.

## Platform/plugin integration

The plugin boundary is the `gws` process. Platforms should discover it
instead of assuming features:

```bash
gws capabilities --json
```

All non-interactive lifecycle and inspection commands accept `--json` and emit a
versioned envelope. For example:

```bash
gws new issue-328 --json
gws show issue-328 --json
gws path issue-328 --json
gws finish issue-328 --json
```

`checkout` is intentionally interactive and does not support JSON. A platform
uses `path --json`, then launches its own runtime with that path as the working
directory. See [GIT_WORKSPACE_IMPLEMENTATION_SPEC.md](GIT_WORKSPACE_IMPLEMENTATION_SPEC.md)
for the contract.

Packages under `internal/` are not a public Go API. The supported integration
interface is the versioned `gws --json` subprocess protocol.

## Configuration and state

Optional repository configuration lives at `.git-workspace.yaml`:

```yaml
version: 1

project:
  name: example
  default_branch: main
```

This file is not required and `gws` does not create it automatically. Without
it, the Project name comes from the current directory and the default branch
comes from Git.

Global state defaults to `~/.git-workspace` and can be overridden with
`GIT_WORKSPACE_HOME`. Persisted Project and Workspace records include a state
format version. Legacy unversioned records are read as version 1; records from
newer unsupported formats are rejected rather than overwritten.

## Contributing and security

See [CONTRIBUTING.md](CONTRIBUTING.md) for development and pull-request checks.
Report vulnerabilities privately according to [SECURITY.md](SECURITY.md). This
project is available under the [MIT License](LICENSE).
