import { invoke } from "@tauri-apps/api/core";
import type { BridgeStatusPayload } from "./view_model";

export type AuthorizeAndConnectResult = {
  callback_url: string;
  connector_id: number;
  runtime_id: number;
  runtime_kind: string;
  scope: string;
};

export async function fetchBridgeStatus(
  stateDir: string | null,
): Promise<BridgeStatusPayload> {
  return invoke<BridgeStatusPayload>("bridge_status", {
    stateDir,
  });
}

export async function authorizeAndConnect(
  stateDir: string | null,
): Promise<AuthorizeAndConnectResult> {
  return invoke<AuthorizeAndConnectResult>("authorize_and_connect", {
    stateDir,
  });
}

export async function forgetRuntimeBinding(
  stateDir: string | null,
): Promise<{ cleared: boolean }> {
  return invoke<{ cleared: boolean }>("forget_runtime_binding", {
    stateDir,
  });
}
