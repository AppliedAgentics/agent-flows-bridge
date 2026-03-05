# Agent Flows Bridge Launch Checklist

Use this checklist before announcing a public desktop release.

## Product Readiness

- Confirm the desktop sign-in flow works from a clean macOS user account
- Confirm bridge bootstrap applies the expected OpenClaw files and default `MEMORY.md`
- Confirm local webhook delivery works end-to-end against production Agent Flows
- Confirm disconnect, reconnect, and forget-runtime flows behave correctly

## Release Readiness

- Confirm `main` is clean and the prepared release version is `YYYY.MM.DD.XX`
- Confirm the changelog entry for that version is complete and user-facing
- Confirm the release tag is created as `vYYYY.MM.DD.XX`
- Confirm the GitHub Actions release workflow completed successfully
- Confirm the GitHub Release includes the macOS zip asset and release notes

## Homebrew Readiness

- Confirm `AppliedAgentics/homebrew-tap` was updated to the same version
- Confirm the cask `sha256` matches the uploaded asset
- Run a fresh install test:
  - `brew tap AppliedAgentics/tap`
  - `brew install --cask agent-flows-bridge`
- Run an uninstall test:
  - `brew uninstall --cask agent-flows-bridge`
  - `brew untap AppliedAgentics/tap`

## Documentation Readiness

- Confirm the top-level README install instructions match the public tap
- Confirm release automation docs match the current workflow and secret names
- Confirm support and troubleshooting guidance is current for the shipped build

## Operational Readiness

- Confirm the repo release workflow secret `HOMEBREW_TAP_PUSH_TOKEN` is present
- Confirm `GITHUB_TOKEN` has contents write permission in the workflow
- Confirm the tag points at `origin/main` before publishing
- Confirm rollback instructions are known: pull the cask back to the prior good version if the release must be withdrawn
