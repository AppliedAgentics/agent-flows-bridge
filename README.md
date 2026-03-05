# Agent Flows Bridge

Agent Flows Bridge connects Agent Flows to a local OpenClaw runtime running on your machine.

It gives Agent Flows a secure way to deliver webhook events to your local runtime without exposing inbound ports or asking you to manage tunnels manually.

## What It Does

- Signs you in to Agent Flows from a desktop app
- Links one local OpenClaw runtime to one Agent Flows runtime record
- Maintains an authenticated outbound connection back to Agent Flows
- Delivers webhook events to your local OpenClaw hook endpoint on loopback
- Stores bridge secrets locally with macOS Keychain as the default secret backend

## Who This Is For

Use Agent Flows Bridge if:

- you run OpenClaw locally on your Mac
- you want Agent Flows to dispatch work into that local runtime
- you want a desktop onboarding flow instead of manual webhook or tunnel setup

## Supported Platform

The current release target is macOS.

## Requirements

- An Agent Flows account
- A local OpenClaw runtime reachable at `http://127.0.0.1:18789`
- macOS with permission to run desktop apps and open a local browser callback

## Install

### Option 1: Download From Releases

This is the recommended install path for end users.

1. Download the latest macOS release archive from GitHub Releases.
2. Extract `Agent Flows Bridge.app`.
3. Move the app into `/Applications`.
4. Launch the app and complete sign-in.

### Option 2: Homebrew

Homebrew distribution depends on the Agent Flows Bridge cask being published to a tap.

The cask template shipped with this repo lives at `release/homebrew/agent-flows-bridge.rb`.
Use the tap-specific install command provided by your release channel once that cask is published.

Typical uninstall commands for a Homebrew-based install are:

```bash
brew uninstall --cask agent-flows-bridge
```

If your install instructions also required tapping a custom repository, remove that tap when you are done:

```bash
brew untap <tap>
```

## First-Time Setup

1. Start your local OpenClaw runtime.
2. Open `Agent Flows Bridge.app`.
3. Click `Sign In to Agent Flows`.
4. Complete GitHub sign-in in the browser.
5. Approve bridge access for the runtime you want to connect.
6. Return to the desktop app and confirm the runtime shows as connected.

The bridge will then fetch bootstrap data from Agent Flows and write the local configuration files required for your OpenClaw runtime.

## What The Bridge Stores Locally

- Bridge state and service files under `~/.agent-flows-bridge/`
- OpenClaw bootstrap files under your configured OpenClaw data directory
- Connector secrets in macOS Keychain by default

## Uninstall

### Remove The App

- Delete `Agent Flows Bridge.app` from `/Applications`

### Remove Bridge State

If you want to remove local bridge state after uninstall:

```bash
rm -rf ~/.agent-flows-bridge
```

### Remove OpenClaw Bootstrap Files

Only remove your OpenClaw data directory if you are sure it is safe to do so for your local runtime setup.

## Security Model

- The bridge uses an outbound connection to Agent Flows
- Webhooks are delivered locally over loopback to OpenClaw
- Connector access is scoped to the authorized runtime
- Secrets are stored in macOS Keychain by default

## Troubleshooting

If sign-in succeeds but the runtime does not connect:

1. Confirm OpenClaw is running locally.
2. Confirm the bridge is targeting the correct OpenClaw data directory.
3. Re-open the desktop app and check connection status.
4. Reconnect the runtime if the stored binding is stale.

## Repository Layout

- `client/` - Go bridge service and CLI
- `desktop/` - Tauri desktop application
- `release/homebrew/` - Homebrew cask template and release packaging assets
- `.github/workflows/` - Standalone CI for the bridge repo
