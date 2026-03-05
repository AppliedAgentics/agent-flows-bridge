# Release Automation

This directory contains release packaging assets for Agent Flows Bridge.

## Homebrew Cask

The Homebrew cask template lives at:

- `homebrew/agent-flows-bridge.rb`

## Automated macOS Release

Use the release helper from the repository root:

```bash
python3 scripts/release_macos.py \
  --tap-dir /Users/sidneyl/code/homebrew-tap
```

The helper will:

- build the macOS Tauri app bundle
- package the `.app` into a release zip
- compute the new SHA256
- update the cask in this repo
- create or upload the GitHub release asset
- update the cask and README in the Homebrew tap
- commit and push the tap changes

## Safe Preview

Dry-run the next release without making changes:

```bash
python3 scripts/release_macos.py \
  --tap-dir /Users/sidneyl/code/homebrew-tap \
  --version 0.1.1 \
  --skip-build \
  --dry-run
```

## Requirements

- clean git state in both `agent-flows-bridge` and the tap repo
- `gh` authenticated for GitHub release creation
- `npm` and Rust toolchain available for Tauri builds
- local clone of the Homebrew tap repo
