import { authorizeAndConnect, fetchBridgeStatus, forgetRuntimeBinding } from "./bridge/api";
import { listen } from "@tauri-apps/api/event";
import { confirm as tauriConfirm } from "@tauri-apps/plugin-dialog";
import {
  hashForRoute,
  normalizeRouteForStatus,
  routeFromHash,
  type BridgeRoute,
} from "./bridge/navigation";
import { confirmAuthorizeAndConnect, confirmForgetRuntime } from "./bridge/confirmations";
import { getConnectionSummary, toConnectionDetails, type BridgeStatusPayload } from "./bridge/view_model";
import packageJson from "../package.json";

type UI = {
  signInPage: HTMLElement;
  detailsPage: HTMLElement;
  detailsPanel: HTMLElement;
  connectButton: HTMLButtonElement;
  toggleDetailsButton: HTMLButtonElement;
  refreshButton: HTMLButtonElement;
  forgetRuntimeButton: HTMLButtonElement;
  signInStatusPill: HTMLElement;
  signInStatusText: HTMLElement;
  detailsStatusPill: HTMLElement;
  detailsStatusText: HTMLElement;
  runtimeValue: HTMLElement;
  connectorValue: HTMLElement;
  binaryValue: HTMLElement;
  configValue: HTMLElement;
  scopeValue: HTMLElement;
  secretsBackendValue: HTMLElement;
  secretsWarningValue: HTMLElement;
  bridgeVersionValue: HTMLElement;
  desktopVersionValue: HTMLElement;
  bootstrapValue: HTMLElement;
  bootstrapApplyValue: HTMLElement;
  bootstrapFetchedValue: HTMLElement;
  bootstrapErrorValue: HTMLElement;
  bootstrapAppliedValue: HTMLElement;
  bootstrapApplyErrorValue: HTMLElement;
  openClawDataDirValue: HTMLElement;
  openClawConfigPathValue: HTMLElement;
  openClawEnvPathValue: HTMLElement;
  boundRuntimeValue: HTMLElement;
  boundFlowValue: HTMLElement;
  bindingUpdatedAtValue: HTMLElement;
  errorValue: HTMLElement;
};

function getUI(): UI {
  const signInPage = requireElement<HTMLElement>("#page-signin");
  const detailsPage = requireElement<HTMLElement>("#page-details");
  const detailsPanel = requireElement<HTMLElement>("#details-panel");
  const connectButton = requireElement<HTMLButtonElement>("#connect-button");
  const toggleDetailsButton = requireElement<HTMLButtonElement>("#toggle-details-button");
  const refreshButton = requireElement<HTMLButtonElement>("#refresh-button");
  const forgetRuntimeButton = requireElement<HTMLButtonElement>("#forget-runtime-button");
  const signInStatusPill = requireElement<HTMLElement>("#signin-status-pill");
  const signInStatusText = requireElement<HTMLElement>("#signin-status-text");
  const detailsStatusPill = requireElement<HTMLElement>("#details-status-pill");
  const detailsStatusText = requireElement<HTMLElement>("#details-status-text");
  const runtimeValue = requireElement<HTMLElement>("#runtime-value");
  const connectorValue = requireElement<HTMLElement>("#connector-value");
  const binaryValue = requireElement<HTMLElement>("#binary-value");
  const configValue = requireElement<HTMLElement>("#config-value");
  const scopeValue = requireElement<HTMLElement>("#scope-value");
  const secretsBackendValue = requireElement<HTMLElement>("#secrets-backend-value");
  const secretsWarningValue = requireElement<HTMLElement>("#secrets-warning-value");
  const bridgeVersionValue = requireElement<HTMLElement>("#bridge-version-value");
  const desktopVersionValue = requireElement<HTMLElement>("#desktop-version-value");
  const bootstrapValue = requireElement<HTMLElement>("#bootstrap-value");
  const bootstrapApplyValue = requireElement<HTMLElement>("#bootstrap-apply-value");
  const bootstrapFetchedValue = requireElement<HTMLElement>("#bootstrap-fetched-value");
  const bootstrapErrorValue = requireElement<HTMLElement>("#bootstrap-error-value");
  const bootstrapAppliedValue = requireElement<HTMLElement>("#bootstrap-applied-value");
  const bootstrapApplyErrorValue = requireElement<HTMLElement>("#bootstrap-apply-error-value");
  const openClawDataDirValue = requireElement<HTMLElement>("#openclaw-data-dir-value");
  const openClawConfigPathValue = requireElement<HTMLElement>("#openclaw-config-path-value");
  const openClawEnvPathValue = requireElement<HTMLElement>("#openclaw-env-path-value");
  const boundRuntimeValue = requireElement<HTMLElement>("#bound-runtime-value");
  const boundFlowValue = requireElement<HTMLElement>("#bound-flow-value");
  const bindingUpdatedAtValue = requireElement<HTMLElement>("#binding-updated-at-value");
  const errorValue = requireElement<HTMLElement>("#error-value");

  return {
    signInPage,
    detailsPage,
    detailsPanel,
    connectButton,
    toggleDetailsButton,
    refreshButton,
    forgetRuntimeButton,
    signInStatusPill,
    signInStatusText,
    detailsStatusPill,
    detailsStatusText,
    runtimeValue,
    connectorValue,
    binaryValue,
    configValue,
    scopeValue,
    secretsBackendValue,
    secretsWarningValue,
    bridgeVersionValue,
    desktopVersionValue,
    bootstrapValue,
    bootstrapApplyValue,
    bootstrapFetchedValue,
    bootstrapErrorValue,
    bootstrapAppliedValue,
    bootstrapApplyErrorValue,
    openClawDataDirValue,
    openClawConfigPathValue,
    openClawEnvPathValue,
    boundRuntimeValue,
    boundFlowValue,
    bindingUpdatedAtValue,
    errorValue,
  };
}

function requireElement<T extends Element>(selector: string): T {
  const element = document.querySelector<T>(selector);
  if (!element) {
    throw new Error(`missing required element: ${selector}`);
  }
  return element;
}

function applyRoute(ui: UI, route: BridgeRoute): void {
  const routeHash = hashForRoute(route);
  if (window.location.hash !== routeHash) {
    window.history.replaceState(null, "", routeHash);
  }

  ui.signInPage.classList.toggle("is-hidden", route !== "signin");
  ui.detailsPage.classList.toggle("is-hidden", route !== "details");
}

function setStatusUI(
  ui: UI,
  pillClassName: "pill-good" | "pill-warn" | "pill-neutral",
  pillLabel: string,
  text: string,
): void {
  const pills = [ui.signInStatusPill, ui.detailsStatusPill];
  const texts = [ui.signInStatusText, ui.detailsStatusText];

  for (const pill of pills) {
    pill.className = `pill ${pillClassName}`;
    pill.textContent = pillLabel;
  }

  for (const statusText of texts) {
    statusText.textContent = text;
  }
}

function applyStatus(ui: UI, status: BridgeStatusPayload): void {
  const summary = getConnectionSummary(status);
  const details = toConnectionDetails(status);

  setStatusUI(ui, summary.pillClassName, summary.pillLabel, summary.message);

  ui.runtimeValue.textContent = details.runtime;
  ui.connectorValue.textContent = details.connector;
  ui.binaryValue.textContent = details.binaryPath;
  ui.configValue.textContent = details.configPath;
  ui.scopeValue.textContent = details.scope;
  ui.secretsBackendValue.textContent = details.secretsBackend;
  ui.secretsWarningValue.textContent = details.secretsWarning;
  ui.bridgeVersionValue.textContent = details.bridgeVersion;
  ui.desktopVersionValue.textContent = details.desktopVersion;
  ui.bootstrapValue.textContent = details.bootstrap;
  ui.bootstrapApplyValue.textContent = details.bootstrapApply;
  ui.bootstrapFetchedValue.textContent = details.bootstrapFetchedAt;
  ui.bootstrapErrorValue.textContent = details.bootstrapError;
  ui.bootstrapAppliedValue.textContent = details.bootstrapAppliedAt;
  ui.bootstrapApplyErrorValue.textContent = details.bootstrapApplyError;
  ui.openClawDataDirValue.textContent = details.openClawDataDir;
  ui.openClawConfigPathValue.textContent = details.openClawConfigPath;
  ui.openClawEnvPathValue.textContent = details.openClawEnvPath;
  ui.boundRuntimeValue.textContent = details.boundRuntime;
  ui.boundFlowValue.textContent = details.boundFlow;
  ui.bindingUpdatedAtValue.textContent = details.bindingUpdatedAt;
  ui.errorValue.textContent = details.error;
}

function applyBusy(ui: UI, busy: boolean): void {
  ui.connectButton.disabled = busy;
  ui.toggleDetailsButton.disabled = busy;
  ui.refreshButton.disabled = busy;
  ui.forgetRuntimeButton.disabled = busy;
}

function setDetailsExpanded(ui: UI, expanded: boolean): void {
  ui.detailsPanel.classList.toggle("is-hidden", !expanded);
  ui.toggleDetailsButton.setAttribute("aria-expanded", expanded ? "true" : "false");
  ui.toggleDetailsButton.textContent = expanded ? "Hide" : "Details";
}

async function refreshStatus(ui: UI): Promise<void> {
  const status = await fetchBridgeStatus(null);
  applyStatus(ui, status);

  const currentRoute = routeFromHash(window.location.hash);
  const nextRoute = normalizeRouteForStatus(currentRoute, status);
  applyRoute(ui, nextRoute);
}

async function runRefreshFlow(ui: UI): Promise<void> {
  applyBusy(ui, true);
  setStatusUI(ui, "pill-neutral", "checking", "Refreshing bridge status...");

  try {
    await refreshStatus(ui);
  } catch (error) {
    setStatusUI(ui, "pill-warn", "error", `Failed to refresh status: ${String(error)}`);
  } finally {
    applyBusy(ui, false);
  }
}

async function runForgetFlow(ui: UI): Promise<void> {
  setStatusUI(ui, "pill-neutral", "confirm", "Waiting for disconnect confirmation...");

  let accepted = false;
  try {
    accepted = await confirmForgetRuntime(confirmInDesktop, (message) => window.confirm(message));
  } catch (error) {
    setStatusUI(ui, "pill-warn", "error", `Failed to open confirmation: ${String(error)}`);
    return;
  }

  if (!accepted) {
    setStatusUI(ui, "pill-neutral", "cancelled", "Forget runtime cancelled.");
    return;
  }

  applyBusy(ui, true);
  setStatusUI(
    ui,
    "pill-neutral",
    "clearing",
    "Disconnecting runtime and clearing local bridge session...",
  );

  try {
    await forgetRuntimeBinding(null);
    await refreshStatus(ui);
    applyRoute(ui, "signin");
    setStatusUI(
      ui,
      "pill-neutral",
      "cleared",
      "Runtime disconnected and local session cleared.",
    );
  } catch (error) {
    setStatusUI(
      ui,
      "pill-warn",
      "clear failed",
      `Failed to clear local runtime binding: ${String(error)}`,
    );
  } finally {
    applyBusy(ui, false);
  }
}

async function runAuthorizeFlow(ui: UI): Promise<void> {
  setStatusUI(ui, "pill-neutral", "confirm", "Waiting for sign in confirmation...");

  let accepted = false;
  try {
    accepted = await confirmAuthorizeAndConnect(confirmInDesktop, (message) => window.confirm(message));
  } catch (error) {
    setStatusUI(ui, "pill-warn", "error", `Failed to open confirmation: ${String(error)}`);
    return;
  }

  if (!accepted) {
    setStatusUI(ui, "pill-neutral", "cancelled", "Sign in cancelled.");
    return;
  }

  applyBusy(ui, true);
  setStatusUI(
    ui,
    "pill-neutral",
    "authorizing",
    "Opening browser for Agent Flows sign in. Complete auth and return here.",
  );

  try {
    await authorizeAndConnect(null);
    await refreshStatus(ui);
    applyRoute(ui, "details");
    setStatusUI(ui, "pill-good", "connected", "Bridge authorization complete.");
  } catch (error) {
    setStatusUI(ui, "pill-warn", "authorize failed", `Authorization failed: ${String(error)}`);
  } finally {
    applyBusy(ui, false);
  }
}

function wireSectionToggle(toggleId: string, bodyId: string): void {
  const toggle = document.getElementById(toggleId);
  const body = document.getElementById(bodyId);
  if (!toggle || !body) return;

  toggle.addEventListener("click", () => {
    const expanded = toggle.getAttribute("aria-expanded") === "true";
    const next = !expanded;
    toggle.setAttribute("aria-expanded", next ? "true" : "false");
    body.classList.toggle("is-hidden", !next);
  });
}

async function confirmInDesktop(message: string): Promise<boolean> {
  return tauriConfirm(message, {
    title: "Agent Flows Bridge",
    kind: "warning",
    okLabel: "Continue",
    cancelLabel: "Cancel",
  });
}

window.addEventListener("DOMContentLoaded", async () => {
  const ui = getUI();
  const versionElement = document.getElementById("app-version");
  if (versionElement) {
    versionElement.textContent = `v${packageJson.version}`;
  }
  const unlistenCallbacks: Array<() => void> = [];
  let detailsExpanded = false;

  applyRoute(ui, "signin");
  setDetailsExpanded(ui, detailsExpanded);
  setStatusUI(ui, "pill-neutral", "checking", "Checking bridge status...");

  window.addEventListener("hashchange", () => {
    const route = routeFromHash(window.location.hash);
    applyRoute(ui, route);
  });

  ui.refreshButton.addEventListener("click", async () => runRefreshFlow(ui));
  ui.forgetRuntimeButton.addEventListener("click", async () => runForgetFlow(ui));
  ui.connectButton.addEventListener("click", async () => runAuthorizeFlow(ui));
  ui.toggleDetailsButton.addEventListener("click", () => {
    detailsExpanded = !detailsExpanded;
    setDetailsExpanded(ui, detailsExpanded);
  });

  wireSectionToggle("section-connection-toggle", "section-connection-body");
  wireSectionToggle("section-bootstrap-toggle", "section-bootstrap-body");
  wireSectionToggle("section-binding-toggle", "section-binding-body");

  unlistenCallbacks.push(
    await listen("bridge://tray-sign-in", async () => {
      await runAuthorizeFlow(ui);
    }),
  );

  unlistenCallbacks.push(
    await listen("bridge://tray-refresh-status", async () => {
      await runRefreshFlow(ui);
    }),
  );

  unlistenCallbacks.push(
    await listen("bridge://tray-forget-runtime", async () => {
      await runForgetFlow(ui);
    }),
  );

  window.addEventListener("beforeunload", () => {
    for (const unlisten of unlistenCallbacks) {
      unlisten();
    }
  });

  applyBusy(ui, true);
  try {
    await refreshStatus(ui);
    const initialRoute = routeFromHash(window.location.hash);
    if (initialRoute === "signin") {
      detailsExpanded = false;
      setDetailsExpanded(ui, detailsExpanded);
    }
  } catch (error) {
    setStatusUI(ui, "pill-warn", "error", `Failed to read bridge status: ${String(error)}`);
  } finally {
    applyBusy(ui, false);
  }
});
