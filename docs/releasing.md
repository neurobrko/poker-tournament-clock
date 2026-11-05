# Releasing

GitHub Actions builds and publishes a GitHub Release when a `v*` tag is pushed to GitHub. The workflow is [`ci-release.yml`](../.github/workflows/ci-release.yml). It runs formatting, tests, and vet, then builds six standalone binaries and creates release notes automatically.

## Before the first public release

- Choose the project license and add a root-level `LICENSE`. This repository currently has none. Confirm redistribution terms for bundled fonts, sounds, and other assets before publishing.
- Confirm GitHub Actions is enabled and hosted runners are available for every matrix target: Linux amd64/arm64, macOS amd64/arm64, and Windows amd64/arm64.
- Review the README and configuration guide. Releases are unsigned raw executables; there are no installers, packages, or generated checksums.

## Publish

1. Choose the release version using `vMAJOR.MINOR.PATCH` (for example, `v0.1.0`). Ensure the release commit is on the GitHub branch you intend to publish.
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

4. Create and push the annotated tag to the GitHub push remote (shown here as `origin`):

   ```sh
   git tag -a vMAJOR.MINOR.PATCH -m "Poker Clock vMAJOR.MINOR.PATCH"
   git push origin vMAJOR.MINOR.PATCH
   ```

5. Watch the **CI and release** workflow complete. Confirm all six binaries appear on the GitHub Release page and download/run the builds for platforms available to test.

A tag push starts the checks before builds; a failed check prevents release publication. The generated assets are named `poker-clock-<os>-<arch>` (`.exe` on Windows). Do not move or reuse a published version tag; publish a new patch version to replace a bad release.
