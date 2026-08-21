# Releasing

Git Workspace uses semantic version tags. Before v1.0, call out incompatible
CLI, JSON, and persisted-state changes explicitly.

1. Update `VERSION`, `CHANGELOG.md`, and both READMEs. `VERSION` must exactly
   match the release tag, including the leading `v`.
2. Run:

   ```bash
   go mod tidy
   make check
   ```

3. Commit the release changes.
4. Create and push an annotated tag:

   ```bash
   git tag -a v0.1.0 -m "gws v0.1.0"
   git push origin main v0.1.0
   ```

5. Verify the GitHub Release contains four archives and `SHA256SUMS`.
6. Install a released build and exercise `init`, `new`, `status`, `apply`, and
   `finish` in a temporary repository.

Never move or overwrite a published tag. Publish a new patch version for a
release correction.
