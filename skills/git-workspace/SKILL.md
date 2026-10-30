---
name: git-workspace
description: 用 gws 创建或管理隔离的 Git worktree 工作空间。当用户说"开个空间/开个 workspace"、"并行改/不影响当前分支"、"隔离修复这个 bug"、"只读排查这个 PR/版本"、"把改动合回去"、"清理工作空间"时使用。Also use when the user asks to work on a task in isolation, review a ref without touching the main checkout, or merge a workspace back.
---

# Git Workspace (gws)

`gws` gives you a disposable, named worktree for a task. You do the work there;
the user's main checkout never moves. Deliver through `gws apply`, which checks
every precondition first and refuses to half-apply.

## 0. Ensure the tool exists

```bash
command -v gws && gws --version
```

If it is missing, install it and re-check (requires Go 1.25+):

```bash
go install github.com/sanm00/gitworkspace/cmd/gws@latest
```

If `gws` is still not found afterwards, its directory is probably not on PATH:
`$(go env GOPATH)/bin`. Report that to the user instead of guessing.

Run everything in the repository the user is working in. `gws` stores state and
worktree checkouts together inside the repository: metadata in
`<repo>/.git/gws/`, checkouts in `<repo>/.git/gws/worktrees/<id>/`. Because they
live under `.git`, a repository-wide search from the main checkout skips them on
purpose; when searching inside the workspace, pass its absolute path to `rg`
(that works by default), and only add `--hidden --no-ignore` if you scan from a
parent directory.

## 1. Create

Always non-interactive, always JSON:

```bash
gws new <name> --no-enter --json
```

- Base defaults to the current branch. Override with `--base <branch|sha>`.
- `--type dev` (default) or `fix` → managed branch `gws/<name>`, mergeable.
- `--type review` or `test` → detached and read-only, for triage. Add
  `--fetch` if the ref only exists on a remote.
- Parse `data.worktree_path`, `data.id`, `data.source.base_commit` from the
  envelope. Do all edits inside `worktree_path`; pass it as the working
  directory of every subsequent command.

## 2. Work and inspect

Use the wrapped commands so you never need the base SHA or the path spelling:

```bash
gws diff --stat          # summary of everything changed since the base
gws diff                 # full diff; untracked files are listed separately
gws diff --uncommitted   # only what is not committed yet
gws log -n 10            # commits on top of the base
gws status               # workspace state, base, dirty flag
```

`git` itself is fine for anything read-only (`git log`, `git show`, `git grep`).

## 3. Commit and deliver

```bash
git -C <worktree_path> add -A
gws commit -m "<concise summary>" -a
gws apply --json          # must run with cwd inside the worktree
```

`apply` merges into the recorded base branch (or `--into <branch>`). If you get
an error envelope, do not retry blindly. Route on `error.code`:

| code | what it means | what you should do |
|---|---|---|
| `workspace_dirty` | uncommitted changes remain | commit them first |
| `workspace_nothing_staged` | `gws commit` without `-a` and nothing staged | `gws commit -m "msg" -a` stages everything first |
| `workspace_unmerged` | `rm` refused: branch commits are not in the base | use `gws apply`, or `gws rm <id> --force` to discard |
| `workspace_detached` | review/test workspace | report findings; it cannot merge |
| `workspace_missing` | worktree was deleted | tell the user to recreate it |
| `workspace_branch_mismatch` | worktree is on another branch | stop; report it |
| `apply_target_not_checked_out` | main checkout is on a different branch | **ask the user**; never switch branches yourself |
| `target_dirty` | main checkout has uncommitted changes | **ask the user** to clean it |
| `apply_sha_target` | base is a SHA | ask the user for a target branch, then `--into <branch>` |
| `merge_conflict` | merge stopped mid-way | stop, report the conflict, let a human resolve or `git merge --abort` |
| `workspace_not_ready` | state is `creating`/`error`/`removing` | run `gws status` and report |

Only report success after the envelope's `error` is `null`.

## 4. Clean up

- `gws rm <id>` removes a workspace (refuses dirty ones).
- `gws finish [id]` merges and then cleans up. Inside the workspace you can omit
  the id. In a `gws new`/`gws use` subshell (zsh/bash) it closes automatically and
  you are back in the main checkout; otherwise `cd` to the repository path it
  prints before running anything else.
- Only run `rm`/`finish` when the user asked for cleanup. Never delete a
  workspace that still holds unreported work.
- `gws gc --dry-run` lists orphans; `gws doctor` self-checks the repository.

## Guardrails

- Never run `gws use`: it starts an interactive shell and will hang.
- Never run `gws new` without `--no-enter --json`; the shell would block.
- Never switch branches or merge by hand in the main checkout. `gws` checks the
  target branch and refuses rather than reconfiguring the user's checkout.
- Treat `review`/`test` workspaces as read-only. If a review requires a fix,
  create a separate `dev`/`fix` workspace.
- Report file paths as absolute paths inside the worktree so the user can find
  them after cleanup.
- When a task is complete, summarize: workspace id, base, commit(s), and the
  apply result.
