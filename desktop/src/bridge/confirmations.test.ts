import { describe, expect, it, vi } from "vitest";
import {
  AUTHORIZE_CONFIRMATION_MESSAGE,
  FORGET_CONFIRMATION_MESSAGE,
  confirmAuthorizeAndConnect,
  confirmForgetRuntime,
} from "./confirmations";

describe("confirmAuthorizeAndConnect", () => {
  it("passes overwrite warning message to the async confirm callback", async () => {
    const confirmMock = vi.fn(async () => true);
    const fallbackMock = vi.fn(() => false);

    const accepted = await confirmAuthorizeAndConnect(confirmMock, fallbackMock);

    expect(accepted).toBe(true);
    expect(confirmMock).toHaveBeenCalledTimes(1);
    expect(confirmMock).toHaveBeenCalledWith(AUTHORIZE_CONFIRMATION_MESSAGE);
    expect(fallbackMock).not.toHaveBeenCalled();
    expect(AUTHORIZE_CONFIRMATION_MESSAGE).toContain("overwrites local OpenClaw config/workspace files");
  });

  it("falls back to sync confirm when async confirm fails", async () => {
    const confirmMock = vi.fn(async () => {
      throw new Error("dialog unavailable");
    });
    const fallbackMock = vi.fn(() => true);

    const accepted = await confirmAuthorizeAndConnect(confirmMock, fallbackMock);

    expect(accepted).toBe(true);
    expect(confirmMock).toHaveBeenCalledWith(AUTHORIZE_CONFIRMATION_MESSAGE);
    expect(fallbackMock).toHaveBeenCalledWith(AUTHORIZE_CONFIRMATION_MESSAGE);
  });
});

describe("confirmForgetRuntime", () => {
  it("passes revoke/session warning message to the async confirm callback", async () => {
    const confirmMock = vi.fn(async () => false);
    const fallbackMock = vi.fn(() => true);

    const accepted = await confirmForgetRuntime(confirmMock, fallbackMock);

    expect(accepted).toBe(false);
    expect(confirmMock).toHaveBeenCalledTimes(1);
    expect(confirmMock).toHaveBeenCalledWith(FORGET_CONFIRMATION_MESSAGE);
    expect(fallbackMock).not.toHaveBeenCalled();
    expect(FORGET_CONFIRMATION_MESSAGE).toContain("Local OpenClaw files remain on disk");
  });

  it("falls back to sync confirm when async confirm fails", async () => {
    const confirmMock = vi.fn(async () => {
      throw new Error("dialog unavailable");
    });
    const fallbackMock = vi.fn(() => false);

    const accepted = await confirmForgetRuntime(confirmMock, fallbackMock);

    expect(accepted).toBe(false);
    expect(confirmMock).toHaveBeenCalledWith(FORGET_CONFIRMATION_MESSAGE);
    expect(fallbackMock).toHaveBeenCalledWith(FORGET_CONFIRMATION_MESSAGE);
  });
});
