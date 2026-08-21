# Git Workspace basic example

From this directory, let `gws` initialize and register the example, then run:

```bash
gws init
gws new avatar
gws checkout avatar
```

Commit changes inside the Workspace. Run `gws apply avatar` to keep the
Workspace, or `gws finish avatar` to apply and safely remove it.

Git Workspace manages Git worktrees only. A development-environment platform may
use `gws path avatar --json` and launch its own runtime in that directory.
