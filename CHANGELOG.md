# Changelog

All notable changes to Agent Flows Bridge are documented in this file.

Versioned as `YYYY.MM.DD.XX` where `XX` is a zero-padded integer increment each of n times pushed to the repository per day.

---


## 2026.03.05.05

### Changes

- Bundle and sync the packaged bridge binary before desktop sign-in and status actions
- Stop Tauri dev rebuild loops caused by generated bridge resources
- Allow disconnect to clear local state when the refresh grant is already invalid
- Fallback oversized bootstrap payload secrets to encrypted file storage when Keychain rejects large values
## 2026.03.05.04

### Changes

- Add Apple signing and notarization enforcement to macOS releases
- Verify signed app bundles before publishing Homebrew artifacts
## 2026.03.05.03

### Changes

- Switch Agent Flows Bridge to calendar versioning for shipped releases
- Add tag-driven GitHub Actions publishing and launch checklist automation
## 2026.03.05.02

### Changes

- Bump the desktop package, Tauri bundle, and Rust crate version metadata to `0.1.1`
- Add the automated macOS release workflow used to build the app, publish the GitHub release asset, and update the Homebrew tap
- Add release packaging documentation for the standalone bridge repository

## 2026.03.05.01 — Initial Standalone Bridge Release

### Changes

- Create the standalone `agent-flows-bridge` repository under `AppliedAgentics`
- Ship the Go bridge service used to authenticate, persist runtime binding state, and deliver local webhook traffic to OpenClaw
- Ship the Tauri desktop app used for sign-in, runtime authorization, status display, and recovery actions
- Add standalone desktop CI covering frontend tests plus Rust test and compile validation
- Add Homebrew cask packaging template for macOS distribution
- Exclude local-only planning artifacts, tickets, templates, and release zip artifacts from Git tracking in the standalone repo
