# AGENTS.md

Guidance for AI coding agents working with this repository.

## Using gws (install and setup)

`gws` is a single Go binary. If it is not installed:

```bash
command -v gws || go install github.com/sanm00/gitworkspace/cmd/gws@latest
gws --version        # expect gws vX.Y.Z
```

The binary lands in `$(go env GOPATH)/bin`. If that directory is not on PATH,
tell the user instead of searching the disk.

To make an agent automatically follow the gws workflow in any repository, install
the bundled skill into your agent's skills directory:

```bash
mkdir -p ~/.agents/skills
cp -R skills/git-workspace ~/.agents/skills/git-workspace
```

`~/.agents/skills/` is read by opencode; other clients use their own location
(for example `~/.claude/skills/`).

### Short version of the workflow

1. `gws new <name> --no-enter --json` → creates an isolated worktree, records the
   base branch and base commit, and returns `data.worktree_path`.
2. Do all edits inside that worktree path.
3. `gws diff` / `gws log` to inspect your own work.
4. `git add -A && gws commit -m "..."` inside the worktree.
5. `gws apply --json` (run inside the worktree) to merge back. Route on
   `error.code`; never switch branches in the main checkout by hand.
6. Cleanup only when asked: `gws rm <id>` or `gws finish <id>` from outside.

Full rules, triggers, and the error-code table live in
[`skills/git-workspace/SKILL.md`](skills/git-workspace/SKILL.md).

## Developing this repository

Requirements: Go 1.25+, Git, macOS or Linux.

```bash
make test     # go test ./...
make lint     # gofmt check + go vet
make race     # go test -race ./...
make check    # test + lint + race + build
make build    # writes bin/gws with VERSION injected
```

- Source layout: `cmd/gws` (main), `internal/cli` (argument parsing, JSON
  envelope, rendering), `internal/gitworkspace` (service, git adapter, storage,
  locks, task files).
- Business rules belong in `internal/gitworkspace`, not in `internal/cli`.
- `internal/` packages are not a public Go API; the supported integration
  surface is the versioned `gws --json` subprocess protocol
  (`GIT_WORKSPACE_IMPLEMENTATION_SPEC.md`).
- Keep documentation in sync: `README.md`, `README.zh.md`, `CHANGELOG.md`, and
  both spec files.
- Do not add comments that merely restate the code.
