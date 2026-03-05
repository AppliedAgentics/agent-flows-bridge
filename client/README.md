# Agent Flows Bridge Service

This directory contains the Go service that powers Agent Flows Bridge.

Most end users should use the desktop app and should not need to run these commands manually. This README is for support, QA, and developers working on the bridge runtime itself.

## Responsibilities

The service is responsible for:

- loading local configuration
- handling OAuth connector sign-in flows
- persisting connector state and secrets
- delivering webhook events to the local OpenClaw runtime
- managing background processing and service installation

## Local Paths

By default, the service uses:

- Bridge state: `~/.agent-flows-bridge`
- OpenClaw data: `~/.openclaw`
- Local runtime URL: `http://127.0.0.1:18789`

## Configuration

The service supports a JSON config file and environment variable overrides.

Example config:

```json
{
  "api_base_url": "https://agentflows.appliedagentics.ai",
  "runtime_url": "http://127.0.0.1:18789",
  "state_dir": "/Users/you/.agent-flows-bridge",
  "openclaw_data_dir": "/Users/you/.openclaw",
  "log_level": "info",
  "oauth_client_id": "agent-flows-bridge"
}
```

Supported environment overrides:

- `AFB_API_BASE_URL`
- `AFB_RUNTIME_URL`
- `AFB_STATE_DIR`
- `AFB_OPENCLAW_DATA_DIR`
- `AFB_LOG_LEVEL`
- `AFB_OAUTH_CLIENT_ID`

## Common Commands

Print resolved configuration:

```bash
cd client
go run ./cmd/agent-flows-bridge -print-config
```

Print build version metadata:

```bash
cd client
go run ./cmd/agent-flows-bridge -version
```

Start OAuth in the browser for runtime selection:

```bash
cd client
go run ./cmd/agent-flows-bridge -oauth-start
```

Start OAuth for a known runtime id:

```bash
cd client
go run ./cmd/agent-flows-bridge -oauth-start-runtime-id 77
```

Complete OAuth after receiving the localhost callback URL:

```bash
cd client
go run ./cmd/agent-flows-bridge -oauth-complete-callback-url "http://127.0.0.1:49200/oauth/callback?code=...&state=..."
```

Print stored session state:

```bash
cd client
go run ./cmd/agent-flows-bridge -oauth-session-status
```

Print stored runtime binding state:

```bash
cd client
go run ./cmd/agent-flows-bridge -runtime-binding-status
```

Clear local OAuth state:

```bash
cd client
go run ./cmd/agent-flows-bridge -oauth-clear-local-state
```

Clear local runtime binding state:

```bash
cd client
go run ./cmd/agent-flows-bridge -runtime-binding-clear
```

Disconnect the currently bound runtime from Agent Flows:

```bash
cd client
go run ./cmd/agent-flows-bridge -disconnect-runtime
```

Verify that OpenClaw accepts hook delivery:

```bash
cd client
go run ./cmd/agent-flows-bridge -verify-openclaw-receipt
```

Process one queued webhook:

```bash
cd client
go run ./cmd/agent-flows-bridge -process-webhook-once
```

Run continuous webhook delivery:

```bash
cd client
go run ./cmd/agent-flows-bridge -run-daemon
```

Run the local browser UI shell:

```bash
cd client
go run ./cmd/agent-flows-bridge -ui-serve
```

Install the background user service:

```bash
cd client
go run ./cmd/agent-flows-bridge -install-user-service
```

Uninstall the background user service:

```bash
cd client
go run ./cmd/agent-flows-bridge -uninstall-user-service
```

## Secret Storage

On macOS, the bridge uses Keychain by default.

If Keychain is unavailable, it falls back to encrypted file-backed secrets inside the bridge state directory and surfaces that condition in status and diagnostics output.

## Build

```bash
cd client
go build ./cmd/agent-flows-bridge
```

## Test

```bash
cd client
go test ./...
```

## Support Notes

If you are debugging a customer issue, the most useful starting points are:

- `-oauth-session-status`
- `-runtime-binding-status`
- `-verify-openclaw-receipt`
- `-run-daemon`
