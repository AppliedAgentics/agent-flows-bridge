# Release Automation

This directory contains release packaging assets for Agent Flows Bridge.

Agent Flows Bridge uses calendar versions in the format `YYYY.MM.DD.XX`. Git tags are published as `vYYYY.MM.DD.XX`.

The release script derives a semver-compatible internal version for Tauri and Cargo because those toolchains reject zero-padded calendar segments. The GitHub release tag, changelog, and Homebrew cask continue to use the canonical `YYYY.MM.DD.XX` version.

## Homebrew Cask

The Homebrew cask template lives at:

- `homebrew/agent-flows-bridge.rb`

## Local Release Preparation

Prepare the next calendar-versioned release from the repository root:

```bash
python3 scripts/release_macos.py \
  --prepare-release \
  --change "Summarize the first user-visible change" \
  --change "Summarize the second user-visible change"
```

This step will:

- compute the next `YYYY.MM.DD.XX` version from the changelog and existing release tags
- update the desktop package, Tauri config, Cargo metadata, and Cargo lock file
- prepend a matching changelog entry

Review the diff, commit it, and push `main`. Then create the release tag:

```bash
git tag v2026.03.05.03
git push origin main --tags
```

The tag must point at `origin/main`. The GitHub Actions release workflow enforces that.

## Automated macOS Publish

The publish workflow runs on tag pushes matching `vYYYY.MM.DD.XX`.

It will:

- build the macOS Tauri app bundle
- package the `.app` into a release zip
- compute the new SHA256
- create or update the GitHub release asset
- update the Homebrew tap cask and tap README
- push the tap update automatically

### Required GitHub Secret

The workflow needs a repository or organization secret named `HOMEBREW_TAP_PUSH_TOKEN`.

That token must have write access to:

- `AppliedAgentics/homebrew-tap`

## Safe Preview

Dry-run the publish path without making changes:

```bash
python3 scripts/release_macos.py \
  --tap-dir /Users/sidneyl/code/homebrew-tap \
  --version 2026.03.05.03 \
  --skip-build \
  --dry-run
```

Dry-run the next prepared version without writing files:

```bash
python3 scripts/release_macos.py \
  --prepare-release \
  --dry-run
```

## Requirements

- clean git state in both `agent-flows-bridge` and the tap repo
- `gh` authenticated for GitHub release creation
- `npm` and Rust toolchain available for Tauri builds
- local clone of the Homebrew tap repo
