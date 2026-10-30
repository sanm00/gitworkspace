# Git Workspace basic example

This directory is a plain Git repository. From anywhere inside it, run:

```bash
gws new demo            # create a Workspace based on the current branch
gws use demo            # enter it later
```

Commit changes inside the Workspace. Run `gws apply` from within the Workspace
to merge into its base branch and keep it, or `gws finish <workspace-id>` from
the main checkout to merge and safely remove it.

Git Workspace manages Git worktrees only. A development-environment platform may
use `gws path demo --json` and launch its own runtime in that directory.
