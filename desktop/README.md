# Agent Flows Bridge Desktop App

This directory contains the Tauri desktop application for Agent Flows Bridge.

This is the app end users launch to sign in, authorize a local runtime, review connection state, and manage the local bridge experience without using terminal commands.

## What Users Experience

The desktop app is responsible for:

- opening the browser sign-in flow
- receiving the localhost OAuth callback
- showing connection and bootstrap state
- surfacing runtime disconnect and recovery actions
- acting as the desktop entry point for the bridge product

## User Flow

1. Launch the app.
2. Click `Sign In to Agent Flows`.
3. Complete GitHub sign-in in the browser.
4. Approve access for the local runtime you want to connect.
5. Return to the app and confirm that the runtime shows as connected.

## Local Development

Prerequisites:

- Rust toolchain
- Node.js and npm
- the bridge Go binary and config available locally when you want to test full desktop flows

Install dependencies:

```bash
cd desktop
npm ci
```

Run the desktop app in development:

```bash
cd desktop
npm run tauri dev
```

Build the desktop web assets:

```bash
cd desktop
npm run build
```

Build the packaged Tauri app:

```bash
cd desktop
npm run tauri build
```

## Test

Run frontend tests:

```bash
cd desktop
npm test
```

Run Rust tests:

```bash
cd desktop/src-tauri
cargo test
```

Run Rust compile validation:

```bash
cd desktop/src-tauri
cargo check
```

## Release Packaging Notes

- The desktop app is the user-facing shell shipped in macOS builds.
- Homebrew release packaging is tracked separately in `../release/homebrew/`.
- CI for the desktop app lives in `../.github/workflows/agent-flows-bridge-desktop.yml`.

## Support Notes

When debugging desktop sign-in issues, check:

- browser launch behavior
- localhost callback handling
- current stored session and runtime binding state
- the desktop status screen after authorization
