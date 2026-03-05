export type BridgeStatusPayload = {
  state_dir: string;
  config_path: string;
  binary_path: string;
  config_exists: boolean;
  binary_exists: boolean;
  connected: boolean;
  connector_id: number | null;
  runtime_id: number | null;
  runtime_kind: string | null;
  scope: string | null;
  bootstrap_ready: boolean;
  bootstrap_runtime_id: number | null;
  bootstrap_fetched_at: string | null;
  bootstrap_error: string | null;
  bootstrap_applied: boolean;
  bootstrap_applied_at: string | null;
  bootstrap_apply_error: string | null;
  openclaw_data_dir: string | null;
  openclaw_config_path: string | null;
  openclaw_env_path: string | null;
  runtime_binding_present: boolean;
  runtime_binding_runtime_id: number | null;
  runtime_binding_runtime_kind: string | null;
  runtime_binding_flow_id: number | null;
  runtime_binding_updated_at: string | null;
  secrets_backend?: string | null;
  secrets_warning?: string | null;
  bridge_version?: string | null;
  bridge_commit?: string | null;
  bridge_build_date?: string | null;
  desktop_version?: string | null;
  error: string | null;
};

export type ConnectionSummary = {
  pillClassName: "pill-good" | "pill-warn" | "pill-neutral";
  pillLabel: string;
  message: string;
};

export type ConnectionDetails = {
  runtime: string;
  connector: string;
  scope: string;
  bootstrap: string;
  bootstrapApply: string;
  bootstrapFetchedAt: string;
  bootstrapError: string;
  bootstrapAppliedAt: string;
  bootstrapApplyError: string;
  openClawDataDir: string;
  openClawConfigPath: string;
  openClawEnvPath: string;
  boundRuntime: string;
  boundFlow: string;
  bindingUpdatedAt: string;
  secretsBackend: string;
  secretsWarning: string;
  bridgeVersion: string;
  desktopVersion: string;
  error: string;
  binaryPath: string;
  configPath: string;
};

export function getConnectionSummary(status: BridgeStatusPayload): ConnectionSummary {
  if (!status.binary_exists || !status.config_exists) {
    return {
      pillClassName: "pill-warn",
      pillLabel: "setup needed",
      message: "Install bridge binary and config before connecting.",
    };
  }

  if (status.connected) {
    if (!status.bootstrap_ready) {
      const bootstrapError =
        status.bootstrap_error && status.bootstrap_error.trim() !== ""
          ? ` ${status.bootstrap_error.trim()}`
          : "";

      if (status.runtime_id != null) {
        return {
          pillClassName: "pill-warn",
          pillLabel: "bootstrap pending",
          message: `Connected to runtime ${status.runtime_id}, but bootstrap has not completed.${bootstrapError}`,
        };
      }

      return {
        pillClassName: "pill-warn",
        pillLabel: "bootstrap pending",
        message: `Connected, but bootstrap has not completed.${bootstrapError}`,
      };
    }

    if (!status.bootstrap_applied) {
      const applyError =
        status.bootstrap_apply_error && status.bootstrap_apply_error.trim() !== ""
          ? ` ${status.bootstrap_apply_error.trim()}`
          : "";

      return {
        pillClassName: "pill-warn",
        pillLabel: "apply pending",
        message: `Connected, but local OpenClaw setup has not been applied.${applyError}`,
      };
    }

    if (status.runtime_id != null) {
      return {
        pillClassName: "pill-good",
        pillLabel: "connected",
        message: `Connected to runtime ${status.runtime_id}.`,
      };
    }

    return {
      pillClassName: "pill-good",
      pillLabel: "connected",
      message: "Connected.",
    };
  }

  const baseMessage = "Sign in to Agent Flows to connect your local runtime bridge.";
  if (status.runtime_binding_present && status.runtime_binding_runtime_id != null) {
    return {
      pillClassName: "pill-neutral",
      pillLabel: "not connected",
      message: `Saved runtime binding detected (runtime ${status.runtime_binding_runtime_id}). Sign in to reconnect.`,
    };
  }

  if (status.error && status.error.trim() !== "") {
    return {
      pillClassName: "pill-neutral",
      pillLabel: "not connected",
      message: `${baseMessage} ${status.error.trim()}`,
    };
  }

  return {
    pillClassName: "pill-neutral",
    pillLabel: "not connected",
    message: baseMessage,
  };
}

export function toConnectionDetails(status: BridgeStatusPayload): ConnectionDetails {
  let runtime = "-";
  if (status.runtime_id != null) {
    runtime = status.runtime_kind
      ? `${status.runtime_id} (${status.runtime_kind})`
      : `${status.runtime_id}`;
  }

  return {
    runtime,
    connector: status.connector_id != null ? `${status.connector_id}` : "-",
    scope: status.scope && status.scope.trim() !== "" ? status.scope.trim() : "-",
    bootstrap: status.bootstrap_ready ? "ready" : "pending",
    bootstrapApply: status.bootstrap_applied ? "applied" : "pending",
    bootstrapFetchedAt:
      status.bootstrap_fetched_at && status.bootstrap_fetched_at.trim() !== ""
        ? status.bootstrap_fetched_at.trim()
        : "-",
    bootstrapError:
      status.bootstrap_error && status.bootstrap_error.trim() !== ""
        ? status.bootstrap_error.trim()
        : "-",
    bootstrapAppliedAt:
      status.bootstrap_applied_at && status.bootstrap_applied_at.trim() !== ""
        ? status.bootstrap_applied_at.trim()
        : "-",
    bootstrapApplyError:
      status.bootstrap_apply_error && status.bootstrap_apply_error.trim() !== ""
        ? status.bootstrap_apply_error.trim()
        : "-",
    openClawDataDir:
      status.openclaw_data_dir && status.openclaw_data_dir.trim() !== ""
        ? status.openclaw_data_dir.trim()
        : "-",
    openClawConfigPath:
      status.openclaw_config_path && status.openclaw_config_path.trim() !== ""
        ? status.openclaw_config_path.trim()
        : "-",
    openClawEnvPath:
      status.openclaw_env_path && status.openclaw_env_path.trim() !== ""
        ? status.openclaw_env_path.trim()
        : "-",
    boundRuntime:
      status.runtime_binding_present && status.runtime_binding_runtime_id != null
        ? status.runtime_binding_runtime_kind
          ? `${status.runtime_binding_runtime_id} (${status.runtime_binding_runtime_kind})`
          : `${status.runtime_binding_runtime_id}`
        : "-",
    boundFlow:
      status.runtime_binding_present && status.runtime_binding_flow_id != null
        ? `${status.runtime_binding_flow_id}`
        : "-",
    bindingUpdatedAt:
      status.runtime_binding_updated_at && status.runtime_binding_updated_at.trim() !== ""
        ? status.runtime_binding_updated_at.trim()
        : "-",
    secretsBackend:
      status.secrets_backend && status.secrets_backend.trim() !== ""
        ? status.secrets_backend.trim()
        : "-",
    secretsWarning:
      status.secrets_warning && status.secrets_warning.trim() !== ""
        ? status.secrets_warning.trim()
        : "-",
    bridgeVersion:
      status.bridge_version && status.bridge_version.trim() !== ""
        ? status.bridge_version.trim()
        : "-",
    desktopVersion:
      status.desktop_version && status.desktop_version.trim() !== ""
        ? status.desktop_version.trim()
        : "-",
    error: status.error && status.error.trim() !== "" ? status.error.trim() : "-",
    binaryPath: status.binary_path,
    configPath: status.config_path,
  };
}

export function showConnectionDetailsPanel(status: BridgeStatusPayload): boolean {
  const hasBootstrapData = status.bootstrap_ready || status.bootstrap_applied;
  return status.connected || hasBootstrapData;
}
