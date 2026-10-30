# Changelog

All notable changes to this project will be documented in this file. The project
uses semantic versioning and keeps incompatible behavior under v0 releases
explicit in release notes.

## v0.1.0 - 2026-10-30

First tagged release. Pre-1.0: CLI, JSON, and persisted-state breaking changes
are expected between minor versions.

### Added

- Initial Git worktree lifecycle commands and versioned JSON machine interface.
- Cross-platform CI, security scanning, and tagged release automation.
- Versioned Workspace records with base branch and base commit tracking.
- Everyday Git wrappers that resolve the Workspace from `cwd`: `gws diff`
  (relative to base, `--uncommitted`, `--staged`, `--stat`, untracked file
  list), `gws log` (`-n N`, `--stat`), and `gws commit -m ... [-a]`.

### Changed (breaking, pre-1.0)

- State moved from `~/.git-workspace` into the repository: metadata, tasks,
  locks, and configuration now live under `<repo>/.git/gws/`, and worktrees
  are created in the sibling directory `<parent>/<repo>-gws/<id>/`. Old global
  state is not migrated; remove `~/.git-workspace` manually if present.
- Removed the Project registry: `gws init` and `gws project ls|show|rm` are
  gone. The repository you are in is the scope of every command.
- Command mapping for previous releases:
  - `gws init` → nothing; run `gws new` directly
  - `gws checkout X` → `gws use X`
  - `gws dev B` / `gws fix B` → `gws new --base B --type dev|fix`
  - `gws review B` / `gws test B` → `gws new --base B --type review|test`
  - `gws apply [WS] [--force TARGET]` → enter the Workspace (`gws use WS`) and
    run `gws apply [--into TARGET]`
- `gws new` now defaults its base to the current branch and enters a child
  shell unless `--json`, `--no-enter`, or a non-TTY stdout is used.
- `--force` is only a boolean on `gws rm`. Merge targets use `--into <branch>`.
- Plugin protocol version raised to 2 (command surface changed); JSON schema
  version stays 2 (`source.base_commit` added).
