import { describe, expect, it } from "vitest";
import {
  getConnectionSummary,
  showConnectionDetailsPanel,
  toConnectionDetails,
  type BridgeStatusPayload,
} from "./view_model";

describe("getConnectionSummary", () => {
  it("shows install incomplete when bridge binary or config is missing", () => {
    const status: BridgeStatusPayload = {
      state_dir: "/Users/sidneyl/.agent-flows-bridge",
      config_path: "/Users/sidneyl/.agent-flows-bridge/config/bridge.json",
      binary_path: "/Users/sidneyl/.agent-flows-bridge/bin/agent-flows-bridge",
      config_exists: false,
      binary_exists: true,
      connected: false,
      connector_id: null,
      runtime_id: null,
      runtime_kind: null,
      scope: null,
      bootstrap_ready: false,
      bootstrap_runtime_id: null,
      bootstrap_fetched_at: null,
      bootstrap_error: null,
      bootstrap_applied: false,
      bootstrap_applied_at: null,
      bootstrap_apply_error: null,
      openclaw_data_dir: null,
      openclaw_config_path: null,
      openclaw_env_path: null,
      runtime_binding_present: false,
      runtime_binding_runtime_id: null,
      runtime_binding_runtime_kind: null,
      runtime_binding_flow_id: null,
      runtime_binding_updated_at: null,
      error: "bridge install incomplete",
    };

    const summary = getConnectionSummary(status);

    expect(summary.pillClassName).toBe("pill-warn");
    expect(summary.pillLabel).toBe("setup needed");
    expect(summary.message).toContain("Install");
  });

  it("shows connected state with runtime id", () => {
    const status: BridgeStatusPayload = {
      state_dir: "/Users/sidneyl/.agent-flows-bridge",
      config_path: "/Users/sidneyl/.agent-flows-bridge/config/bridge.json",
      binary_path: "/Users/sidneyl/.agent-flows-bridge/bin/agent-flows-bridge",
      config_exists: true,
      binary_exists: true,
      connected: true,
      connector_id: 44,
      runtime_id: 97,
      runtime_kind: "local_connector",
      scope: "connector:heartbeat connector:webhook",
      bootstrap_ready: true,
      bootstrap_runtime_id: 97,
      bootstrap_fetched_at: "2026-03-04T00:00:00Z",
      bootstrap_error: null,
      bootstrap_applied: true,
      bootstrap_applied_at: "2026-03-04T00:01:00Z",
      bootstrap_apply_error: null,
      openclaw_data_dir: "/Users/sidneyl/.openclaw",
      openclaw_config_path: "/Users/sidneyl/.openclaw/openclaw.json",
      openclaw_env_path: "/Users/sidneyl/.openclaw/agent-flows.env",
      runtime_binding_present: false,
      runtime_binding_runtime_id: null,
      runtime_binding_runtime_kind: null,
      runtime_binding_flow_id: null,
      runtime_binding_updated_at: null,
      error: null,
    };

    const summary = getConnectionSummary(status);

    expect(summary.pillClassName).toBe("pill-good");
    expect(summary.pillLabel).toBe("connected");
    expect(summary.message).toBe("Connected to runtime 97.");
  });

  it("shows disconnected state and includes backend error", () => {
    const status: BridgeStatusPayload = {
      state_dir: "/Users/sidneyl/.agent-flows-bridge",
      config_path: "/Users/sidneyl/.agent-flows-bridge/config/bridge.json",
      binary_path: "/Users/sidneyl/.agent-flows-bridge/bin/agent-flows-bridge",
      config_exists: true,
      binary_exists: true,
      connected: false,
      connector_id: null,
      runtime_id: null,
      runtime_kind: null,
      scope: null,
      bootstrap_ready: false,
      bootstrap_runtime_id: null,
      bootstrap_fetched_at: null,
      bootstrap_error: null,
      bootstrap_applied: false,
      bootstrap_applied_at: null,
      bootstrap_apply_error: null,
      openclaw_data_dir: null,
      openclaw_config_path: null,
      openclaw_env_path: null,
      runtime_binding_present: false,
      runtime_binding_runtime_id: null,
      runtime_binding_runtime_kind: null,
      runtime_binding_flow_id: null,
      runtime_binding_updated_at: null,
      error: "secret not found",
    };

    const summary = getConnectionSummary(status);

    expect(summary.pillClassName).toBe("pill-neutral");
    expect(summary.pillLabel).toBe("not connected");
    expect(summary.message).toContain("secret not found");
  });

  it("shows sign-in guidance when disconnected without backend errors", () => {
    const status: BridgeStatusPayload = {
      state_dir: "/Users/sidneyl/.agent-flows-bridge",
      config_path: "/Users/sidneyl/.agent-flows-bridge/config/bridge.json",
      binary_path: "/Users/sidneyl/.agent-flows-bridge/bin/agent-flows-bridge",
      config_exists: true,
      binary_exists: true,
      connected: false,
      connector_id: null,
      runtime_id: null,
      runtime_kind: null,
      scope: null,
      bootstrap_ready: false,
      bootstrap_runtime_id: null,
      bootstrap_fetched_at: null,
      bootstrap_error: null,
      bootstrap_applied: false,
      bootstrap_applied_at: null,
      bootstrap_apply_error: null,
      openclaw_data_dir: null,
      openclaw_config_path: null,
      openclaw_env_path: null,
      runtime_binding_present: false,
      runtime_binding_runtime_id: null,
      runtime_binding_runtime_kind: null,
      runtime_binding_flow_id: null,
      runtime_binding_updated_at: null,
      error: null,
    };

    const summary = getConnectionSummary(status);

    expect(summary.pillClassName).toBe("pill-neutral");
    expect(summary.pillLabel).toBe("not connected");
    expect(summary.message).toBe("Sign in to Agent Flows to connect your local runtime bridge.");
  });

  it("shows reconnect guidance when runtime binding exists but session is cleared", () => {
    const status: BridgeStatusPayload = {
      state_dir: "/Users/sidneyl/.agent-flows-bridge",
      config_path: "/Users/sidneyl/.agent-flows-bridge/config/bridge.json",
      binary_path: "/Users/sidneyl/.agent-flows-bridge/bin/agent-flows-bridge",
      config_exists: true,
      binary_exists: true,
      connected: false,
      connector_id: null,
      runtime_id: null,
      runtime_kind: null,
      scope: null,
      bootstrap_ready: false,
      bootstrap_runtime_id: null,
      bootstrap_fetched_at: null,
      bootstrap_error: null,
      bootstrap_applied: false,
      bootstrap_applied_at: null,
      bootstrap_apply_error: null,
      openclaw_data_dir: null,
      openclaw_config_path: null,
      openclaw_env_path: null,
      runtime_binding_present: true,
      runtime_binding_runtime_id: 98,
      runtime_binding_runtime_kind: "local_connector",
      runtime_binding_flow_id: 44,
      runtime_binding_updated_at: "2026-03-04T22:00:00Z",
      error: null,
    };

    const summary = getConnectionSummary(status);

    expect(summary.pillClassName).toBe("pill-neutral");
    expect(summary.pillLabel).toBe("not connected");
    expect(summary.message).toContain("runtime 98");
  });

  it("shows bootstrap pending when connected without bootstrap payload", () => {
    const status: BridgeStatusPayload = {
      state_dir: "/Users/sidneyl/.agent-flows-bridge",
      config_path: "/Users/sidneyl/.agent-flows-bridge/config/bridge.json",
      binary_path: "/Users/sidneyl/.agent-flows-bridge/bin/agent-flows-bridge",
      config_exists: true,
      binary_exists: true,
      connected: true,
      connector_id: 44,
      runtime_id: 97,
      runtime_kind: "local_connector",
      scope: "connector:heartbeat connector:webhook",
      bootstrap_ready: false,
      bootstrap_runtime_id: null,
      bootstrap_fetched_at: null,
      bootstrap_error: "bootstrap payload missing",
      bootstrap_applied: false,
      bootstrap_applied_at: null,
      bootstrap_apply_error: null,
      openclaw_data_dir: null,
      openclaw_config_path: null,
      openclaw_env_path: null,
      runtime_binding_present: false,
      runtime_binding_runtime_id: null,
      runtime_binding_runtime_kind: null,
      runtime_binding_flow_id: null,
      runtime_binding_updated_at: null,
      error: null,
    };

    const summary = getConnectionSummary(status);
    expect(summary.pillClassName).toBe("pill-warn");
    expect(summary.pillLabel).toBe("bootstrap pending");
    expect(summary.message).toContain("runtime 97");
  });

  it("shows apply pending when bootstrap is ready but local write is incomplete", () => {
    const status: BridgeStatusPayload = {
      state_dir: "/Users/sidneyl/.agent-flows-bridge",
      config_path: "/Users/sidneyl/.agent-flows-bridge/config/bridge.json",
      binary_path: "/Users/sidneyl/.agent-flows-bridge/bin/agent-flows-bridge",
      config_exists: true,
      binary_exists: true,
      connected: true,
      connector_id: 44,
      runtime_id: 97,
      runtime_kind: "local_connector",
      scope: "connector:heartbeat connector:webhook",
      bootstrap_ready: true,
      bootstrap_runtime_id: 97,
      bootstrap_fetched_at: "2026-03-04T00:00:00Z",
      bootstrap_error: null,
      bootstrap_applied: false,
      bootstrap_applied_at: null,
      bootstrap_apply_error: "permission denied",
      openclaw_data_dir: "/Users/sidneyl/.openclaw",
      openclaw_config_path: null,
      openclaw_env_path: null,
      runtime_binding_present: false,
      runtime_binding_runtime_id: null,
      runtime_binding_runtime_kind: null,
      runtime_binding_flow_id: null,
      runtime_binding_updated_at: null,
      error: null,
    };

    const summary = getConnectionSummary(status);
    expect(summary.pillClassName).toBe("pill-warn");
    expect(summary.pillLabel).toBe("apply pending");
    expect(summary.message).toContain("local OpenClaw");
  });
});

describe("toConnectionDetails", () => {
  it("formats runtime and connector ids for connected payloads", () => {
    const status: BridgeStatusPayload = {
      state_dir: "/Users/sidneyl/.agent-flows-bridge",
      config_path: "/Users/sidneyl/.agent-flows-bridge/config/bridge.json",
      binary_path: "/Users/sidneyl/.agent-flows-bridge/bin/agent-flows-bridge",
      config_exists: true,
      binary_exists: true,
      connected: true,
      connector_id: 44,
      runtime_id: 97,
      runtime_kind: "local_connector",
      scope: "connector:heartbeat connector:webhook",
      bootstrap_ready: true,
      bootstrap_runtime_id: 97,
      bootstrap_fetched_at: "2026-03-04T00:00:00Z",
      bootstrap_error: null,
      bootstrap_applied: true,
      bootstrap_applied_at: "2026-03-04T00:01:00Z",
      bootstrap_apply_error: null,
      openclaw_data_dir: "/Users/sidneyl/.openclaw",
      openclaw_config_path: "/Users/sidneyl/.openclaw/openclaw.json",
      openclaw_env_path: "/Users/sidneyl/.openclaw/agent-flows.env",
      runtime_binding_present: false,
      runtime_binding_runtime_id: null,
      runtime_binding_runtime_kind: null,
      runtime_binding_flow_id: null,
      runtime_binding_updated_at: null,
      error: null,
    };

    const details = toConnectionDetails(status);
    expect(details.runtime).toBe("97 (local_connector)");
    expect(details.connector).toBe("44");
    expect(details.scope).toBe("connector:heartbeat connector:webhook");
    expect(details.bootstrap).toBe("ready");
    expect(details.bootstrapApply).toBe("applied");
    expect(details.boundRuntime).toBe("-");
    expect(details.boundFlow).toBe("-");
    expect(details.bindingUpdatedAt).toBe("-");
    expect(details.error).toBe("-");
  });

  it("uses dashes for missing optional fields", () => {
    const status: BridgeStatusPayload = {
      state_dir: "/Users/sidneyl/.agent-flows-bridge",
      config_path: "/Users/sidneyl/.agent-flows-bridge/config/bridge.json",
      binary_path: "/Users/sidneyl/.agent-flows-bridge/bin/agent-flows-bridge",
      config_exists: true,
      binary_exists: true,
      connected: false,
      connector_id: null,
      runtime_id: null,
      runtime_kind: null,
      scope: null,
      bootstrap_ready: false,
      bootstrap_runtime_id: null,
      bootstrap_fetched_at: null,
      bootstrap_error: null,
      bootstrap_applied: false,
      bootstrap_applied_at: null,
      bootstrap_apply_error: null,
      openclaw_data_dir: null,
      openclaw_config_path: null,
      openclaw_env_path: null,
      runtime_binding_present: false,
      runtime_binding_runtime_id: null,
      runtime_binding_runtime_kind: null,
      runtime_binding_flow_id: null,
      runtime_binding_updated_at: null,
      error: null,
    };

    const details = toConnectionDetails(status);
    expect(details.runtime).toBe("-");
    expect(details.connector).toBe("-");
    expect(details.scope).toBe("-");
    expect(details.bootstrap).toBe("pending");
    expect(details.bootstrapApply).toBe("pending");
    expect(details.boundRuntime).toBe("-");
    expect(details.boundFlow).toBe("-");
    expect(details.bindingUpdatedAt).toBe("-");
    expect(details.error).toBe("-");
  });
});

describe("showConnectionDetailsPanel", () => {
  it("hides details when disconnected", () => {
    const status: BridgeStatusPayload = {
      state_dir: "/Users/sidneyl/.agent-flows-bridge",
      config_path: "/Users/sidneyl/.agent-flows-bridge/config/bridge.json",
      binary_path: "/Users/sidneyl/.agent-flows-bridge/bin/agent-flows-bridge",
      config_exists: true,
      binary_exists: true,
      connected: false,
      connector_id: null,
      runtime_id: null,
      runtime_kind: null,
      scope: null,
      bootstrap_ready: false,
      bootstrap_runtime_id: null,
      bootstrap_fetched_at: null,
      bootstrap_error: null,
      bootstrap_applied: false,
      bootstrap_applied_at: null,
      bootstrap_apply_error: null,
      openclaw_data_dir: null,
      openclaw_config_path: null,
      openclaw_env_path: null,
      runtime_binding_present: false,
      runtime_binding_runtime_id: null,
      runtime_binding_runtime_kind: null,
      runtime_binding_flow_id: null,
      runtime_binding_updated_at: null,
      error: null,
    };

    expect(showConnectionDetailsPanel(status)).toBe(false);
  });

  it("shows details when connected", () => {
    const status: BridgeStatusPayload = {
      state_dir: "/Users/sidneyl/.agent-flows-bridge",
      config_path: "/Users/sidneyl/.agent-flows-bridge/config/bridge.json",
      binary_path: "/Users/sidneyl/.agent-flows-bridge/bin/agent-flows-bridge",
      config_exists: true,
      binary_exists: true,
      connected: true,
      connector_id: 44,
      runtime_id: 97,
      runtime_kind: "local_connector",
      scope: "connector:heartbeat connector:webhook",
      bootstrap_ready: true,
      bootstrap_runtime_id: 97,
      bootstrap_fetched_at: "2026-03-04T00:00:00Z",
      bootstrap_error: null,
      bootstrap_applied: true,
      bootstrap_applied_at: "2026-03-04T00:01:00Z",
      bootstrap_apply_error: null,
      openclaw_data_dir: "/Users/sidneyl/.openclaw",
      openclaw_config_path: "/Users/sidneyl/.openclaw/openclaw.json",
      openclaw_env_path: "/Users/sidneyl/.openclaw/agent-flows.env",
      runtime_binding_present: false,
      runtime_binding_runtime_id: null,
      runtime_binding_runtime_kind: null,
      runtime_binding_flow_id: null,
      runtime_binding_updated_at: null,
      error: null,
    };

    expect(showConnectionDetailsPanel(status)).toBe(true);
  });
});
