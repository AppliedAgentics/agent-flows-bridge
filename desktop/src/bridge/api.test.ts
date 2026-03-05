import { beforeEach, describe, expect, it, vi } from "vitest";

const { invokeMock } = vi.hoisted(() => ({
  invokeMock: vi.fn(),
}));

vi.mock("@tauri-apps/api/core", () => ({
  invoke: invokeMock,
}));

import {
  authorizeAndConnect,
  fetchBridgeStatus,
  forgetRuntimeBinding,
} from "./api";

describe("fetchBridgeStatus", () => {
  beforeEach(() => {
    invokeMock.mockReset();
  });

  it("invokes bridge_status with the provided state dir", async () => {
    const expected = { connected: false };
    invokeMock.mockResolvedValue(expected);

    const result = await fetchBridgeStatus("/Users/sidneyl/.agent-flows-bridge");

    expect(result).toBe(expected);
    expect(invokeMock).toHaveBeenCalledWith("bridge_status", {
      stateDir: "/Users/sidneyl/.agent-flows-bridge",
    });
  });

  it("passes null state dir through without rewriting it", async () => {
    invokeMock.mockResolvedValue({ connected: true });

    await fetchBridgeStatus(null);

    expect(invokeMock).toHaveBeenCalledWith("bridge_status", {
      stateDir: null,
    });
  });
});

describe("authorizeAndConnect", () => {
  beforeEach(() => {
    invokeMock.mockReset();
  });

  it("invokes authorize_and_connect and returns the typed payload", async () => {
    const expected = {
      callback_url: "http://127.0.0.1:49200/oauth/callback?code=abc&state=xyz",
      connector_id: 2,
      runtime_id: 98,
      runtime_kind: "local_connector",
      scope: "connector:bootstrap connector:heartbeat connector:webhook",
    };
    invokeMock.mockResolvedValue(expected);

    const result = await authorizeAndConnect(null);

    expect(result).toEqual(expected);
    expect(invokeMock).toHaveBeenCalledWith("authorize_and_connect", {
      stateDir: null,
    });
  });

  it("propagates invoke failures", async () => {
    invokeMock.mockRejectedValue(new Error("bridge command failed"));

    await expect(authorizeAndConnect(null)).rejects.toThrow("bridge command failed");
  });
});

describe("forgetRuntimeBinding", () => {
  beforeEach(() => {
    invokeMock.mockReset();
  });

  it("invokes forget_runtime_binding and returns the clear status", async () => {
    invokeMock.mockResolvedValue({ cleared: true });

    const result = await forgetRuntimeBinding(null);

    expect(result).toEqual({ cleared: true });
    expect(invokeMock).toHaveBeenCalledWith("forget_runtime_binding", {
      stateDir: null,
    });
  });
});
