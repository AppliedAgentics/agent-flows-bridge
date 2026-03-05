import { describe, expect, it } from "vitest";
import {
  hashForRoute,
  normalizeRouteForStatus,
  routeFromHash,
  targetRouteForStatus,
} from "./navigation";
import type { BridgeStatusPayload } from "./view_model";

describe("routeFromHash", () => {
  it("maps details hash to details route", () => {
    expect(routeFromHash("#/details")).toBe("details");
  });

  it("defaults unknown hash to signin route", () => {
    expect(routeFromHash("#/unknown")).toBe("signin");
    expect(routeFromHash("")).toBe("signin");
  });
});

describe("hashForRoute", () => {
  it("returns canonical route hashes", () => {
    expect(hashForRoute("signin")).toBe("#/signin");
    expect(hashForRoute("details")).toBe("#/details");
  });
});

describe("targetRouteForStatus", () => {
  it("routes disconnected status to signin", () => {
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

    expect(targetRouteForStatus(status)).toBe("signin");
  });

  it("routes connected status to details", () => {
    const status: BridgeStatusPayload = {
      state_dir: "/Users/sidneyl/.agent-flows-bridge",
      config_path: "/Users/sidneyl/.agent-flows-bridge/config/bridge.json",
      binary_path: "/Users/sidneyl/.agent-flows-bridge/bin/agent-flows-bridge",
      config_exists: true,
      binary_exists: true,
      connected: true,
      connector_id: 2,
      runtime_id: 98,
      runtime_kind: "local_connector",
      scope: "connector:heartbeat connector:webhook",
      bootstrap_ready: true,
      bootstrap_runtime_id: 98,
      bootstrap_fetched_at: "2026-03-04T19:10:00Z",
      bootstrap_error: null,
      bootstrap_applied: true,
      bootstrap_applied_at: "2026-03-04T19:10:15Z",
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

    expect(targetRouteForStatus(status)).toBe("details");
  });
});

describe("normalizeRouteForStatus", () => {
  it("moves sign in route to details when already connected", () => {
    const status: BridgeStatusPayload = {
      state_dir: "/Users/sidneyl/.agent-flows-bridge",
      config_path: "/Users/sidneyl/.agent-flows-bridge/config/bridge.json",
      binary_path: "/Users/sidneyl/.agent-flows-bridge/bin/agent-flows-bridge",
      config_exists: true,
      binary_exists: true,
      connected: true,
      connector_id: 2,
      runtime_id: 98,
      runtime_kind: "local_connector",
      scope: "connector:heartbeat connector:webhook",
      bootstrap_ready: true,
      bootstrap_runtime_id: 98,
      bootstrap_fetched_at: "2026-03-04T19:10:00Z",
      bootstrap_error: null,
      bootstrap_applied: true,
      bootstrap_applied_at: "2026-03-04T19:10:15Z",
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

    expect(normalizeRouteForStatus("signin", status)).toBe("details");
  });

  it("moves details route back to sign in when disconnected", () => {
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

    expect(normalizeRouteForStatus("details", status)).toBe("signin");
  });
});
