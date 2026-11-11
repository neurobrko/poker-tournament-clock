# Releasing

GitHub Actions runs formatting, tests, vet, and a native Linux check on pushes and pull requests. It also builds the six supported platform targets for pull requests and `build-validation-*` branches. Pushing a `v*` tag builds those binaries with the tag in the clock footer and publishes a GitHub Release. Gitea Actions runs only a lightweight notice job. The workflow is [`ci-release.yml`](../.github/workflows/ci-release.yml).

## Validate a release candidate

Push a branch named `build-validation-<suffix>` (for example, `build-validation-v0.0.2` or `build-validation-dev`) to run the GitHub platform-build matrix without opening a pull request or publishing a release. The workflow matches this prefix with `startsWith`, not a regular expression. For this release, all validation branches embed `v0.0.2` in binaries; update the workflow version expression for later releases. Gitea receives only its notice job.

After all six builds pass, merge the validated tree into `main` on Gitea and push/sync that same tree to GitHub. Re-run validation if merge resolution changes source files.

## Before the first public release

- Verify the root `LICENSE` and confirm redistribution rights for bundled fonts and MP3 sounds. The font license is bundled beside the runtime font files; document sound provenance before redistribution.
- Confirm GitHub Actions is enabled and hosted runners are available for every matrix target: Linux amd64/arm64, macOS amd64/arm64, and Windows amd64/arm64.
- Review the README and configuration guide. Releases are unsigned raw executables; there are no installers, packages, or generated checksums.

## Publish

1. Choose the release version using `vMAJOR.MINOR.PATCH` and ensure the final release commit is on `main` at both remotes. For v0.0.2, update `release/CHANGELOG.md` and `release/v0.0.2.md` before tagging.
2. From a clean checkout, run the same checks as CI:

   ```sh
   test -z "$(gofmt -l .)"
   go test -tags ci ./...
   go vet -tags ci ./...
   ```

3. Check that the tag push destination is GitHub. This repository may use different fetch and push URLs:

   ```sh
   git remote -v
   git remote get-url --push origin
   ```

4. Confirm `v0.0.2` does not already exist on either remote. Create and push the annotated tag to the configured push remotes (shown here as `origin`; this repository pushes to Gitea and GitHub):

   ```sh
   git tag -a vMAJOR.MINOR.PATCH -m "Poker Clock vMAJOR.MINOR.PATCH"
   git push origin vMAJOR.MINOR.PATCH
   ```

5. Watch the **CI and release** workflow complete. Confirm all six binaries appear on the GitHub Release page and download/run the builds for platforms available to test.

A GitHub tag push starts the checks before builds; a failed check prevents release publication. GitHub Release notes come from `release/v0.0.2.md`; the workflow publishes six assets named `poker-clock-<os>-<arch>` (`.exe` on Windows). Opening a tournament database with schema migration 5 requires a backup first; older schema-4 binaries cannot open the migrated database. Do not move or reuse a published version tag; publish a new patch version to replace a bad release.
