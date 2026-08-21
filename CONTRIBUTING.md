# Contributing

Thank you for improving Git Workspace.

## Development setup

Requirements:

- Go 1.25 or newer
- Git
- macOS or Linux

Run the local quality checks before opening a pull request:

```bash
gofmt -w .
go mod tidy
go vet ./...
go test -race ./...
go build ./cmd/gws
```

## Changes

- Keep the supported integration boundary in `gws --json`; packages under
  `internal/` are not a public Go API.
- Add regression tests for behavior and error-code changes.
- Preserve existing JSON fields within a schema version. Breaking machine
  interface changes require a schema-version update and migration notes.
- Keep destructive operations explicit and scoped to resources whose ownership
  can be proven.
- Update both English and Chinese user documentation for user-visible changes.

## Pull requests

Explain the problem, the chosen behavior, verification performed, and any
compatibility or cleanup implications. Keep unrelated changes in separate pull
requests.

By participating, you agree to follow the project's Code of Conduct.

Maintainer release steps are documented in [RELEASING.md](RELEASING.md).
