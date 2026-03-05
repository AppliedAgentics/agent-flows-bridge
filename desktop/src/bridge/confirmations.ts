export type ConfirmFn = (message: string) => boolean;
export type AsyncConfirmFn = (message: string) => Promise<boolean>;

export const AUTHORIZE_CONFIRMATION_MESSAGE =
  "Sign in and connect will apply Agent Flows bootstrap to your local OpenClaw instance. This overwrites local OpenClaw config/workspace files managed by the bridge. Continue?";

export const FORGET_CONFIRMATION_MESSAGE =
  "Forget Runtime revokes bridge runtime access and clears local bridge session state. Local OpenClaw files remain on disk. Continue?";

export async function confirmAuthorizeAndConnect(
  confirmFn: AsyncConfirmFn,
  fallbackConfirmFn: ConfirmFn,
): Promise<boolean> {
  return confirmWithFallback(AUTHORIZE_CONFIRMATION_MESSAGE, confirmFn, fallbackConfirmFn);
}

export async function confirmForgetRuntime(
  confirmFn: AsyncConfirmFn,
  fallbackConfirmFn: ConfirmFn,
): Promise<boolean> {
  return confirmWithFallback(FORGET_CONFIRMATION_MESSAGE, confirmFn, fallbackConfirmFn);
}

async function confirmWithFallback(
  message: string,
  confirmFn: AsyncConfirmFn,
  fallbackConfirmFn: ConfirmFn,
): Promise<boolean> {
  try {
    return await confirmFn(message);
  } catch (_error) {
    return fallbackConfirmFn(message);
  }
}
