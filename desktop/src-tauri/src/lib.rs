use serde::{Deserialize, Serialize};
use std::fs;
use std::io::{Read, Write};
use std::net::TcpListener;
use std::path::{Path, PathBuf};
use std::process::Command;
use std::thread;
use std::time::{Duration, Instant};
use tauri::menu::{Menu, MenuItem, PredefinedMenuItem};
use tauri::tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent};
use tauri::{AppHandle, Emitter, Manager, RunEvent, WindowEvent};
use url::Url;

#[derive(Debug, Clone)]
struct BridgePaths {
    state_dir: PathBuf,
    config_path: PathBuf,
    binary_path: PathBuf,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "snake_case")]
struct BridgeStatus {
    state_dir: String,
    config_path: String,
    binary_path: String,
    config_exists: bool,
    binary_exists: bool,
    connected: bool,
    connector_id: Option<i64>,
    runtime_id: Option<i64>,
    runtime_kind: Option<String>,
    scope: Option<String>,
    bootstrap_ready: bool,
    bootstrap_runtime_id: Option<i64>,
    bootstrap_fetched_at: Option<String>,
    bootstrap_error: Option<String>,
    bootstrap_applied: bool,
    bootstrap_applied_at: Option<String>,
    bootstrap_apply_error: Option<String>,
    openclaw_data_dir: Option<String>,
    openclaw_config_path: Option<String>,
    openclaw_env_path: Option<String>,
    runtime_binding_present: bool,
    runtime_binding_runtime_id: Option<i64>,
    runtime_binding_runtime_kind: Option<String>,
    runtime_binding_flow_id: Option<i64>,
    runtime_binding_updated_at: Option<String>,
    secrets_backend: Option<String>,
    secrets_warning: Option<String>,
    bridge_version: Option<String>,
    bridge_commit: Option<String>,
    bridge_build_date: Option<String>,
    desktop_version: String,
    error: Option<String>,
}

#[derive(Debug, Deserialize)]
struct SessionStatusPayload {
    connected: bool,
    connector_id: Option<i64>,
    runtime_id: Option<i64>,
    runtime_kind: Option<String>,
    scope: Option<String>,
    bootstrap_ready: Option<bool>,
    bootstrap_runtime_id: Option<i64>,
    bootstrap_fetched_at: Option<String>,
    bootstrap_error: Option<String>,
    bootstrap_applied: Option<bool>,
    bootstrap_applied_at: Option<String>,
    bootstrap_apply_error: Option<String>,
    openclaw_data_dir: Option<String>,
    openclaw_config_path: Option<String>,
    openclaw_env_path: Option<String>,
    secrets_backend: Option<String>,
    secrets_warning: Option<String>,
    bridge_version: Option<String>,
    bridge_commit: Option<String>,
    bridge_build_date: Option<String>,
    error: Option<String>,
}

#[derive(Debug, Deserialize)]
struct RuntimeBindingStatusPayload {
    bound: bool,
    runtime_id: Option<i64>,
    runtime_kind: Option<String>,
    flow_id: Option<i64>,
    updated_at: Option<String>,
}

#[derive(Debug, Deserialize)]
struct ClearPayload {
    cleared: bool,
}

#[derive(Debug, Deserialize)]
struct DisconnectPayload {
    revoked: bool,
}

#[derive(Debug, Deserialize)]
struct OAuthStartPayload {
    authorize_url: String,
    redirect_uri: String,
    state: String,
}

#[derive(Debug, Deserialize)]
struct OAuthCompletePayload {
    connector_id: i64,
    runtime_id: i64,
    runtime_kind: String,
    scope: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "snake_case")]
struct AuthorizeResult {
    callback_url: String,
    connector_id: i64,
    runtime_id: i64,
    runtime_kind: String,
    scope: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "snake_case")]
struct ForgetRuntimeResult {
    cleared: bool,
}

const TRAY_ID: &str = "agent-flows-bridge-tray";
const TRAY_MENU_OPEN_ID: &str = "open-main-window";
const TRAY_MENU_SIGN_IN_ID: &str = "start-sign-in";
const TRAY_MENU_REFRESH_ID: &str = "refresh-status";
const TRAY_MENU_FORGET_ID: &str = "forget-runtime";
const TRAY_MENU_QUIT_ID: &str = "quit-app";
const TRAY_EVENT_SIGN_IN: &str = "bridge://tray-sign-in";
const TRAY_EVENT_REFRESH_STATUS: &str = "bridge://tray-refresh-status";
const TRAY_EVENT_FORGET_RUNTIME: &str = "bridge://tray-forget-runtime";
const BUNDLED_BRIDGE_RESOURCE_DIR: &str = "bridge";

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum TrayMenuAction {
    OpenMainWindow,
    StartSignIn,
    RefreshStatus,
    ForgetRuntime,
    QuitApp,
}

#[tauri::command]
fn bridge_status(app_handle: AppHandle, state_dir: Option<String>) -> Result<BridgeStatus, String> {
    let paths = BridgePaths::resolve(state_dir)?;
    let sync_result = ensure_packaged_bridge_binary_ready(&app_handle, &paths);
    let config_exists = paths.config_path.exists();
    let binary_exists = paths.binary_path.exists();

    if !config_exists || !binary_exists || sync_result.is_err() {
        let error_message = match sync_result.err() {
            Some(sync_error) => sync_error,
            None => "bridge install incomplete".to_string(),
        };
        let status = incomplete_bridge_status(&paths, config_exists, binary_exists, error_message);
        return Ok(status);
    }

    let args = vec![
        "-config".to_string(),
        paths.config_path.to_string_lossy().into_owned(),
        "-oauth-session-status".to_string(),
    ];

    let payload = run_bridge_json::<SessionStatusPayload>(&paths, &args)?;
    let runtime_binding = load_runtime_binding_status(&paths)?;

    let status = BridgeStatus {
        state_dir: paths.state_dir.to_string_lossy().into_owned(),
        config_path: paths.config_path.to_string_lossy().into_owned(),
        binary_path: paths.binary_path.to_string_lossy().into_owned(),
        config_exists,
        binary_exists,
        connected: payload.connected,
        connector_id: payload.connector_id,
        runtime_id: payload.runtime_id,
        runtime_kind: payload.runtime_kind,
        scope: payload.scope,
        bootstrap_ready: payload.bootstrap_ready.unwrap_or(false),
        bootstrap_runtime_id: payload.bootstrap_runtime_id,
        bootstrap_fetched_at: payload.bootstrap_fetched_at,
        bootstrap_error: payload.bootstrap_error,
        bootstrap_applied: payload.bootstrap_applied.unwrap_or(false),
        bootstrap_applied_at: payload.bootstrap_applied_at,
        bootstrap_apply_error: payload.bootstrap_apply_error,
        openclaw_data_dir: payload.openclaw_data_dir,
        openclaw_config_path: payload.openclaw_config_path,
        openclaw_env_path: payload.openclaw_env_path,
        runtime_binding_present: runtime_binding.bound,
        runtime_binding_runtime_id: runtime_binding.runtime_id,
        runtime_binding_runtime_kind: runtime_binding.runtime_kind,
        runtime_binding_flow_id: runtime_binding.flow_id,
        runtime_binding_updated_at: runtime_binding.updated_at,
        secrets_backend: payload.secrets_backend,
        secrets_warning: payload.secrets_warning,
        bridge_version: payload.bridge_version,
        bridge_commit: payload.bridge_commit,
        bridge_build_date: payload.bridge_build_date,
        desktop_version: env!("CARGO_PKG_VERSION").to_string(),
        error: payload.error,
    };

    Ok(status)
}

#[tauri::command]
fn forget_runtime_binding(
    app_handle: AppHandle,
    state_dir: Option<String>,
) -> Result<ForgetRuntimeResult, String> {
    let paths = BridgePaths::resolve(state_dir.clone())?;
    ensure_packaged_bridge_binary_ready(&app_handle, &paths)?;
    forget_runtime_binding_with_state_dir(state_dir)?;
    Ok(ForgetRuntimeResult { cleared: true })
}

#[tauri::command]
async fn authorize_and_connect(
    app_handle: AppHandle,
    state_dir: Option<String>,
) -> Result<AuthorizeResult, String> {
    let handle = tauri::async_runtime::spawn_blocking(move || {
        let paths = BridgePaths::resolve(state_dir)?;
        let bundled_binary_path = resolve_packaged_bridge_binary_path(&app_handle)?;
        authorize_and_connect_with_paths_and_browser(&paths, &bundled_binary_path, |url| {
            webbrowser::open(url).map_err(|error| format!("open browser: {error}"))
        })
    });

    handle
        .await
        .map_err(|join_error| format!("authorization worker failed: {join_error}"))?
}

fn bind_oauth_callback_listener() -> Result<TcpListener, String> {
    for port in 49200..=49210 {
        let listener_address = format!("127.0.0.1:{port}");
        match TcpListener::bind(&listener_address) {
            Ok(listener) => {
                listener
                    .set_nonblocking(true)
                    .map_err(|error| format!("configure callback listener: {error}"))?;
                return Ok(listener);
            }
            Err(_) => continue,
        }
    }

    Err("bind callback listener on 127.0.0.1:49200-49210: no free port".to_string())
}

fn wait_for_callback_and_open_with_browser<F>(
    listener: TcpListener,
    authorize_url: &str,
    redirect_uri: &str,
    expected_state: &str,
    open_browser: F,
) -> Result<String, String>
where
    F: FnOnce(&str) -> Result<(), String>,
{
    let redirect_url =
        Url::parse(redirect_uri).map_err(|error| format!("parse redirect uri: {error}"))?;
    let scheme = redirect_url.scheme().to_string();
    let host = redirect_url
        .host_str()
        .ok_or_else(|| "redirect_uri missing host".to_string())?
        .to_string();
    let port = redirect_url
        .port()
        .ok_or_else(|| "redirect_uri missing port".to_string())?;
    let callback_path = redirect_url.path().to_string();
    let trimmed_state = expected_state.trim();

    open_browser(authorize_url)?;

    let timeout = Duration::from_secs(180);
    let start = Instant::now();

    while start.elapsed() < timeout {
        match listener.accept() {
            Ok((mut stream, _address)) => {
                let mut buffer = [0u8; 8192];
                let bytes_read = stream
                    .read(&mut buffer)
                    .map_err(|error| format!("read callback request: {error}"))?;
                let request_text = String::from_utf8_lossy(&buffer[..bytes_read]).to_string();
                let request_line = request_text.lines().next().unwrap_or_default();
                let request_path = match parse_request_path(request_line) {
                    Ok(request_path) => request_path,
                    Err(error) => {
                        let error_body = callback_error_body("Invalid callback request.");
                        let _ = write_http_response(&mut stream, 400, &error_body);
                        let _ = stream.flush();
                        if error.contains("unexpected callback method") {
                            continue;
                        }
                        continue;
                    }
                };

                let request_url = Url::parse(&format!("http://127.0.0.1{request_path}"))
                    .map_err(|error| format!("parse callback request url: {error}"))?;

                if request_url.path() != callback_path {
                    let error_body = callback_error_body("Unexpected callback path.");
                    let _ = write_http_response(&mut stream, 404, &error_body);
                    let _ = stream.flush();
                    continue;
                }

                let callback_state = request_url
                    .query_pairs()
                    .find(|(key, _)| key == "state")
                    .map(|(_, value)| value.into_owned())
                    .unwrap_or_default();
                if callback_state.trim() != trimmed_state {
                    let error_body = callback_error_body("Callback state mismatch.");
                    let _ = write_http_response(&mut stream, 400, &error_body);
                    let _ = stream.flush();
                    continue;
                }

                let _ = write_http_response(&mut stream, 200, callback_success_body());
                let _ = stream.flush();

                let callback_url = format!("{scheme}://{host}:{port}{request_path}");
                return Ok(callback_url);
            }
            Err(error) if error.kind() == std::io::ErrorKind::WouldBlock => {
                thread::sleep(Duration::from_millis(100));
            }
            Err(error) => {
                return Err(format!("accept callback connection: {error}"));
            }
        }
    }

    Err("timed out waiting for oauth callback on localhost".to_string())
}

// Build a disconnected status payload for missing or unsynced bridge state.
//
// Uses resolved state paths and file existence flags to populate the desktop
// shell with actionable error details before any bridge command can run.
//
// Returns a disconnected `BridgeStatus` with the provided error message.
fn incomplete_bridge_status(
    paths: &BridgePaths,
    config_exists: bool,
    binary_exists: bool,
    error_message: String,
) -> BridgeStatus {
    BridgeStatus {
        state_dir: paths.state_dir.to_string_lossy().into_owned(),
        config_path: paths.config_path.to_string_lossy().into_owned(),
        binary_path: paths.binary_path.to_string_lossy().into_owned(),
        config_exists,
        binary_exists,
        connected: false,
        connector_id: None,
        runtime_id: None,
        runtime_kind: None,
        scope: None,
        bootstrap_ready: false,
        bootstrap_runtime_id: None,
        bootstrap_fetched_at: None,
        bootstrap_error: None,
        bootstrap_applied: false,
        bootstrap_applied_at: None,
        bootstrap_apply_error: None,
        openclaw_data_dir: None,
        openclaw_config_path: None,
        openclaw_env_path: None,
        runtime_binding_present: false,
        runtime_binding_runtime_id: None,
        runtime_binding_runtime_kind: None,
        runtime_binding_flow_id: None,
        runtime_binding_updated_at: None,
        secrets_backend: None,
        secrets_warning: None,
        bridge_version: None,
        bridge_commit: None,
        bridge_build_date: None,
        desktop_version: env!("CARGO_PKG_VERSION").to_string(),
        error: Some(error_message),
    }
}

// Resolve the packaged bridge binary path from the app bundle or dev resources.
//
// Checks the bundled app resource directory first, then falls back to the repo
// `src-tauri/resources` directory used by local development and tests.
//
// Returns the packaged bridge binary path or an error with all attempted paths.
fn resolve_packaged_bridge_binary_path(app_handle: &AppHandle) -> Result<PathBuf, String> {
    let mut candidate_paths = Vec::new();

    if let Ok(resource_dir) = app_handle.path().resource_dir() {
        candidate_paths.push(
            resource_dir
                .join(BUNDLED_BRIDGE_RESOURCE_DIR)
                .join(binary_file_name()),
        );
        candidate_paths.push(
            resource_dir
                .join("resources")
                .join(BUNDLED_BRIDGE_RESOURCE_DIR)
                .join(binary_file_name()),
        );
    }

    candidate_paths.push(
        PathBuf::from(env!("CARGO_MANIFEST_DIR"))
            .parent()
            .expect("resolve desktop dir")
            .join("generated-resources")
            .join(BUNDLED_BRIDGE_RESOURCE_DIR)
            .join(binary_file_name()),
    );

    candidate_paths.push(
        PathBuf::from(env!("CARGO_MANIFEST_DIR"))
            .join("resources")
            .join(BUNDLED_BRIDGE_RESOURCE_DIR)
            .join(binary_file_name()),
    );

    for candidate_path in &candidate_paths {
        if candidate_path.exists() {
            return Ok(candidate_path.clone());
        }
    }

    let checked_paths = candidate_paths
        .iter()
        .map(|path| path.to_string_lossy().into_owned())
        .collect::<Vec<_>>()
        .join(", ");

    Err(format!(
        "bundled bridge binary not found; checked {checked_paths}"
    ))
}

// Sync the packaged bridge binary into the user state directory.
//
// Copies the bundled bridge binary into `~/.agent-flows-bridge/bin/` when the
// installed binary is missing or differs from the packaged bytes.
//
// Returns `Ok(())` when the binary is ready, or an error if the copy fails.
fn ensure_packaged_bridge_binary_ready(
    app_handle: &AppHandle,
    paths: &BridgePaths,
) -> Result<(), String> {
    let bundled_binary_path = resolve_packaged_bridge_binary_path(app_handle)?;
    sync_packaged_bridge_binary(paths, &bundled_binary_path)?;
    ensure_default_bridge_config_ready(paths)
}

// Copy the packaged bridge binary into the state directory when needed.
//
// Compares the installed and bundled file bytes to avoid unnecessary writes.
// When they differ, writes a temporary file, applies executable permissions,
// and atomically replaces the installed binary.
//
// Returns `Ok(())` when the installed bridge binary matches the bundled copy.
fn sync_packaged_bridge_binary(
    paths: &BridgePaths,
    bundled_binary_path: &Path,
) -> Result<(), String> {
    let bundled_contents = fs::read(bundled_binary_path).map_err(|error| {
        format!(
            "read bundled bridge binary at {}: {error}",
            bundled_binary_path.to_string_lossy()
        )
    })?;

    if let Ok(existing_contents) = fs::read(&paths.binary_path) {
        if existing_contents == bundled_contents {
            return Ok(());
        }
    }

    let binary_parent = paths
        .binary_path
        .parent()
        .ok_or_else(|| "resolve bridge binary parent directory".to_string())?;
    fs::create_dir_all(binary_parent).map_err(|error| {
        format!(
            "create bridge binary directory at {}: {error}",
            binary_parent.to_string_lossy()
        )
    })?;

    let temp_binary_path = binary_parent.join(format!(
        "{}.tmp",
        paths
            .binary_path
            .file_name()
            .unwrap_or_default()
            .to_string_lossy()
    ));
    fs::write(&temp_binary_path, &bundled_contents).map_err(|error| {
        format!(
            "write synced bridge binary at {}: {error}",
            temp_binary_path.to_string_lossy()
        )
    })?;
    apply_packaged_binary_permissions(bundled_binary_path, &temp_binary_path)?;

    if paths.binary_path.exists() {
        fs::remove_file(&paths.binary_path).map_err(|error| {
            format!(
                "remove stale bridge binary at {}: {error}",
                paths.binary_path.to_string_lossy()
            )
        })?;
    }

    fs::rename(&temp_binary_path, &paths.binary_path).map_err(|error| {
        format!(
            "replace bridge binary at {}: {error}",
            paths.binary_path.to_string_lossy()
        )
    })?;

    Ok(())
}

// Apply packaged executable permissions to the synced state-dir binary.
//
// Mirrors the bundled file mode on Unix so the copied bridge binary remains
// runnable after sync. Non-Unix platforms rely on default file semantics.
//
// Returns `Ok(())` when permissions are ready.
#[cfg(unix)]
fn apply_packaged_binary_permissions(source_path: &Path, target_path: &Path) -> Result<(), String> {
    use std::os::unix::fs::PermissionsExt;

    let source_permissions = fs::metadata(source_path)
        .map_err(|error| {
            format!(
                "read bundled bridge binary permissions at {}: {error}",
                source_path.to_string_lossy()
            )
        })?
        .permissions();

    let mut target_permissions = fs::metadata(target_path)
        .map_err(|error| {
            format!(
                "read synced bridge binary permissions at {}: {error}",
                target_path.to_string_lossy()
            )
        })?
        .permissions();
    target_permissions.set_mode(source_permissions.mode());

    fs::set_permissions(target_path, target_permissions).map_err(|error| {
        format!(
            "set synced bridge binary permissions at {}: {error}",
            target_path.to_string_lossy()
        )
    })
}

// Apply packaged executable permissions to the synced state-dir binary.
//
// Non-Unix platforms do not require explicit permission updates after copying.
//
// Returns `Ok(())`.
#[cfg(not(unix))]
fn apply_packaged_binary_permissions(
    _source_path: &Path,
    _target_path: &Path,
) -> Result<(), String> {
    Ok(())
}

// Run the full desktop OAuth flow after ensuring the packaged bridge binary is installed.
//
// Syncs the packaged bridge binary into the user state dir, starts OAuth with
// the dynamic callback port, waits for the localhost callback, and completes
// connector authorization.
//
// Returns `Ok(AuthorizeResult)` or an error string.
fn authorize_and_connect_with_paths_and_browser<F>(
    paths: &BridgePaths,
    bundled_binary_path: &Path,
    open_browser: F,
) -> Result<AuthorizeResult, String>
where
    F: FnOnce(&str) -> Result<(), String>,
{
    sync_packaged_bridge_binary(paths, bundled_binary_path)?;
    ensure_default_bridge_config_ready(paths)?;

    if !paths.binary_path.exists() {
        return Err(format!(
            "bridge binary not found at {}",
            paths.binary_path.to_string_lossy()
        ));
    }
    if !paths.config_path.exists() {
        return Err(format!(
            "bridge config not found at {}",
            paths.config_path.to_string_lossy()
        ));
    }

    let listener = bind_oauth_callback_listener()?;
    let callback_port = listener
        .local_addr()
        .map_err(|error| format!("read callback listener address: {error}"))?
        .port();

    let start_args = vec![
        "-config".to_string(),
        paths.config_path.to_string_lossy().into_owned(),
        "-oauth-start".to_string(),
        "-oauth-redirect-port".to_string(),
        callback_port.to_string(),
    ];
    let start_payload = run_bridge_json::<OAuthStartPayload>(paths, &start_args)?;

    let callback_url = wait_for_callback_and_open_with_browser(
        listener,
        &start_payload.authorize_url,
        &start_payload.redirect_uri,
        &start_payload.state,
        open_browser,
    )?;

    let complete_args = vec![
        "-config".to_string(),
        paths.config_path.to_string_lossy().into_owned(),
        "-oauth-complete-callback-url".to_string(),
        callback_url.clone(),
    ];
    let complete_payload = run_bridge_json::<OAuthCompletePayload>(paths, &complete_args)?;

    let result = AuthorizeResult {
        callback_url,
        connector_id: complete_payload.connector_id,
        runtime_id: complete_payload.runtime_id,
        runtime_kind: complete_payload.runtime_kind,
        scope: complete_payload.scope,
    };

    Ok(result)
}

// Write the default bridge config file on first launch.
//
// Fresh Homebrew installs do not have `~/.agent-flows-bridge/config/bridge.json`
// yet. The desktop shell owns that first-run provisioning step so sign-in can
// succeed without a prior CLI install flow.
//
// Returns `Ok(())` when the config exists or is written successfully.
fn ensure_default_bridge_config_ready(paths: &BridgePaths) -> Result<(), String> {
    if paths.config_path.exists() {
        return Ok(());
    }

    let config_parent = paths
        .config_path
        .parent()
        .ok_or_else(|| "resolve bridge config parent directory".to_string())?;
    fs::create_dir_all(config_parent).map_err(|error| {
        format!(
            "create bridge config directory at {}: {error}",
            config_parent.to_string_lossy()
        )
    })?;

    let encoded_config = render_default_bridge_config(paths)?;
    fs::write(&paths.config_path, encoded_config).map_err(|error| {
        format!(
            "write bridge config at {}: {error}",
            paths.config_path.to_string_lossy()
        )
    })
}

// Render the default JSON config used by the packaged bridge binary.
//
// The desktop shell mirrors the bridge CLI defaults so it can provision
// `bridge.json` before the first OAuth flow on a clean machine.
//
// Returns the pretty-printed config bytes with a trailing newline.
fn render_default_bridge_config(paths: &BridgePaths) -> Result<Vec<u8>, String> {
    let home_dir = dirs::home_dir().ok_or_else(|| "resolve home directory".to_string())?;
    let openclaw_data_dir = home_dir.join(".openclaw");

    let config = serde_json::json!({
        "api_base_url": "https://agentflows.appliedagentics.ai",
        "runtime_url": "http://127.0.0.1:18789",
        "state_dir": paths.state_dir.to_string_lossy().into_owned(),
        "openclaw_data_dir": openclaw_data_dir.to_string_lossy().into_owned(),
        "log_level": "info",
        "oauth_client_id": "agent-flows-bridge",
        "transport_mode": "auto"
    });

    let mut encoded = serde_json::to_vec_pretty(&config)
        .map_err(|error| format!("encode default bridge config: {error}"))?;
    encoded.push(b'\n');
    Ok(encoded)
}

fn write_http_response(
    stream: &mut impl Write,
    status_code: u16,
    body: &str,
) -> Result<(), String> {
    let status_text = match status_code {
        200 => "OK",
        400 => "Bad Request",
        404 => "Not Found",
        _ => "Error",
    };

    let response = format!(
        "HTTP/1.1 {status_code} {status_text}\r\nContent-Type: text/html; charset=utf-8\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
        body.len(),
        body
    );

    stream
        .write_all(response.as_bytes())
        .map_err(|error| format!("write callback response: {error}"))
}

fn callback_success_body() -> &'static str {
    "<html><body style=\"font-family: sans-serif; padding: 24px;\"><h2>Agent Flows Bridge</h2><p>Authorization received. You can close this tab and return to the desktop app.</p></body></html>"
}

fn callback_error_body(message: &str) -> String {
    format!(
        "<html><body style=\"font-family: sans-serif; padding: 24px;\"><h2>Agent Flows Bridge</h2><p>{message}</p></body></html>"
    )
}

fn parse_request_path(request_line: &str) -> Result<String, String> {
    let mut parts = request_line.split_whitespace();
    let method = parts.next().unwrap_or_default();
    let target = parts.next().unwrap_or_default();

    if method != "GET" {
        return Err(format!("unexpected callback method: {method}"));
    }
    if target.is_empty() {
        return Err("missing callback request path".to_string());
    }

    Ok(target.to_string())
}

// Map tray menu item ids to typed actions.
//
// Menu event handling uses stable string ids from tray menu items and converts
// them into an internal action enum for dispatch.
//
// Returns `Some(TrayMenuAction)` when recognized, otherwise `None`.
fn parse_tray_menu_action(menu_id: &str) -> Option<TrayMenuAction> {
    match menu_id {
        TRAY_MENU_OPEN_ID => Some(TrayMenuAction::OpenMainWindow),
        TRAY_MENU_SIGN_IN_ID => Some(TrayMenuAction::StartSignIn),
        TRAY_MENU_REFRESH_ID => Some(TrayMenuAction::RefreshStatus),
        TRAY_MENU_FORGET_ID => Some(TrayMenuAction::ForgetRuntime),
        TRAY_MENU_QUIT_ID => Some(TrayMenuAction::QuitApp),
        _ => None,
    }
}

fn embedded_tray_icon_asset_name() -> &'static str {
    #[cfg(target_os = "macos")]
    {
        "tray-icon.png"
    }

    #[cfg(not(target_os = "macos"))]
    {
        "icon.png"
    }
}

fn embedded_tray_icon_bytes() -> &'static [u8] {
    #[cfg(target_os = "macos")]
    {
        include_bytes!("../icons/tray-icon.png")
    }

    #[cfg(not(target_os = "macos"))]
    {
        include_bytes!("../icons/icon.png")
    }
}

fn tray_icon_uses_template_rendering() -> bool {
    cfg!(target_os = "macos")
}

// Build the macOS tray icon and menu actions for menu bar mode.
//
// The tray menu exposes open/sign-in/refresh/forget/quit actions and routes
// actions to either window visibility operations or frontend events.
//
// Returns `Ok(())` when tray setup succeeds.
fn setup_system_tray(app: &mut tauri::App) -> tauri::Result<()> {
    let open_item = MenuItem::with_id(
        app,
        TRAY_MENU_OPEN_ID,
        "Open Agent Flows Bridge",
        true,
        None::<&str>,
    )?;
    let sign_in_item = MenuItem::with_id(
        app,
        TRAY_MENU_SIGN_IN_ID,
        "Sign In / Reconnect",
        true,
        None::<&str>,
    )?;
    let refresh_item = MenuItem::with_id(
        app,
        TRAY_MENU_REFRESH_ID,
        "Refresh Status",
        true,
        None::<&str>,
    )?;
    let forget_item = MenuItem::with_id(
        app,
        TRAY_MENU_FORGET_ID,
        "Forget Runtime",
        true,
        None::<&str>,
    )?;
    let separator_item = PredefinedMenuItem::separator(app)?;
    let quit_item = MenuItem::with_id(
        app,
        TRAY_MENU_QUIT_ID,
        "Quit Agent Flows Bridge",
        true,
        None::<&str>,
    )?;

    let menu = Menu::with_items(
        app,
        &[
            &open_item,
            &sign_in_item,
            &refresh_item,
            &forget_item,
            &separator_item,
            &quit_item,
        ],
    )?;

    let mut tray_builder = TrayIconBuilder::with_id(TRAY_ID)
        .menu(&menu)
        .tooltip("Agent Flows Bridge")
        .show_menu_on_left_click(false)
        .on_menu_event(|app_handle, event| {
            let menu_id = event.id().as_ref();
            if let Some(action) = parse_tray_menu_action(menu_id) {
                if let Err(error) = handle_tray_menu_action(app_handle, action) {
                    eprintln!("tray menu action error: {error}");
                }
            }
        })
        .on_tray_icon_event(|tray, event| {
            if let TrayIconEvent::Click {
                button: MouseButton::Left,
                button_state: MouseButtonState::Up,
                ..
            } = event
            {
                let app_handle = tray.app_handle();
                if let Err(error) = show_main_window(&app_handle) {
                    eprintln!("show window from tray icon click failed: {error}");
                }
            }
        });

    match tauri::image::Image::from_bytes(embedded_tray_icon_bytes()) {
        Ok(icon) => {
            tray_builder = tray_builder.icon(icon);
        }
        Err(e) => {
            eprintln!(
                "failed to load embedded tray icon {}: {e}",
                embedded_tray_icon_asset_name()
            );
            if let Some(icon) = app.default_window_icon().cloned() {
                tray_builder = tray_builder.icon(icon);
            }
        }
    }

    #[cfg(target_os = "macos")]
    if tray_icon_uses_template_rendering() {
        tray_builder = tray_builder.icon_as_template(true);
    }

    tray_builder.build(app)?;

    Ok(())
}

// Handle a tray menu action.
//
// Opens the window, emits frontend events for sign-in/refresh/forget actions,
// or exits the app for explicit quit actions.
//
// Returns `Ok(())` or an error string.
fn handle_tray_menu_action(app_handle: &AppHandle, action: TrayMenuAction) -> Result<(), String> {
    match action {
        TrayMenuAction::OpenMainWindow => show_main_window(app_handle),
        TrayMenuAction::StartSignIn => {
            show_main_window(app_handle)?;
            emit_tray_event(app_handle, TRAY_EVENT_SIGN_IN)
        }
        TrayMenuAction::RefreshStatus => {
            show_main_window(app_handle)?;
            emit_tray_event(app_handle, TRAY_EVENT_REFRESH_STATUS)
        }
        TrayMenuAction::ForgetRuntime => {
            show_main_window(app_handle)?;
            emit_tray_event(app_handle, TRAY_EVENT_FORGET_RUNTIME)
        }
        TrayMenuAction::QuitApp => {
            app_handle.exit(0);
            Ok(())
        }
    }
}

// Show and focus the primary window.
//
// The menu bar app keeps the main window hidden when closed; this helper
// restores it for explicit open/auth actions.
//
// Returns `Ok(())` or an error string.
fn show_main_window(app_handle: &AppHandle) -> Result<(), String> {
    let window = app_handle
        .get_webview_window("main")
        .ok_or_else(|| "main window not found".to_string())?;

    window
        .show()
        .map_err(|error| format!("show main window: {error}"))?;
    window
        .set_focus()
        .map_err(|error| format!("focus main window: {error}"))?;
    Ok(())
}

// Hide the primary window while keeping the app process alive.
//
// Used for macOS menu bar behavior when users click the window close control.
//
// Returns `Ok(())` or an error string.
fn hide_main_window(app_handle: &AppHandle) -> Result<(), String> {
    let window = app_handle
        .get_webview_window("main")
        .ok_or_else(|| "main window not found".to_string())?;

    window
        .hide()
        .map_err(|error| format!("hide main window: {error}"))?;
    Ok(())
}

// Emit a tray command event to the frontend window.
//
// Frontend code listens for these events and executes the existing sign-in,
// refresh, and forget flows.
//
// Returns `Ok(())` or an error string.
fn emit_tray_event(app_handle: &AppHandle, event_name: &str) -> Result<(), String> {
    app_handle
        .emit(event_name, ())
        .map_err(|error| format!("emit {event_name}: {error}"))
}

fn run_bridge_json<T: for<'de> Deserialize<'de>>(
    paths: &BridgePaths,
    args: &[String],
) -> Result<T, String> {
    let output = Command::new(&paths.binary_path)
        .args(args)
        .output()
        .map_err(|error| format!("run bridge command: {error}"))?;

    let stdout = String::from_utf8_lossy(&output.stdout).trim().to_string();
    let stderr = sanitize_bridge_stderr(String::from_utf8_lossy(&output.stderr).trim());

    if !output.status.success() {
        let error_message = if stderr.is_empty() {
            "bridge command failed".to_string()
        } else {
            format!("bridge command failed: {stderr}")
        };
        return Err(error_message);
    }

    serde_json::from_str::<T>(&stdout).map_err(|error| {
        format!("decode bridge json output: {error}; stdout={stdout}; stderr={stderr}")
    })
}

fn load_runtime_binding_status(paths: &BridgePaths) -> Result<RuntimeBindingStatusPayload, String> {
    let args = vec![
        "-config".to_string(),
        paths.config_path.to_string_lossy().into_owned(),
        "-runtime-binding-status".to_string(),
    ];

    run_bridge_json::<RuntimeBindingStatusPayload>(paths, &args)
}

fn forget_runtime_binding_with_state_dir(state_dir_override: Option<String>) -> Result<(), String> {
    let paths = BridgePaths::resolve(state_dir_override)?;
    if !paths.binary_path.exists() {
        return Err(format!(
            "bridge binary not found at {}",
            paths.binary_path.to_string_lossy()
        ));
    }
    if !paths.config_path.exists() {
        return Err(format!(
            "bridge config not found at {}",
            paths.config_path.to_string_lossy()
        ));
    }

    if let Err(disconnect_error) = disconnect_runtime_with_paths(&paths) {
        if !disconnect_error_is_safe_to_ignore(&disconnect_error) {
            return Err(disconnect_error);
        }
    }

    clear_binding_with_paths(&paths)?;
    clear_oauth_local_state_with_paths(&paths)?;
    Ok(())
}

// Decide whether a disconnect failure can be treated as already revoked.
//
// The desktop "Forget Runtime" flow is best-effort for server-side revoke and
// must still clear local runtime/session state if the connector token is already
// invalid or previously revoked.
//
// Returns true when the disconnect error is safe to ignore.
fn disconnect_error_is_safe_to_ignore(error_message: &str) -> bool {
    let normalized = error_message.to_ascii_lowercase();
    normalized.contains("invalid_connector_token")
        || normalized.contains("connector_revoked")
        || normalized.contains("invalid_grant")
}

fn disconnect_runtime_with_paths(paths: &BridgePaths) -> Result<(), String> {
    let args = vec![
        "-config".to_string(),
        paths.config_path.to_string_lossy().into_owned(),
        "-disconnect-runtime".to_string(),
    ];

    let payload = run_bridge_json::<DisconnectPayload>(paths, &args)?;
    if !payload.revoked {
        return Err("disconnect command returned revoked=false".to_string());
    }

    Ok(())
}

fn clear_binding_with_paths(paths: &BridgePaths) -> Result<(), String> {
    let args = vec![
        "-config".to_string(),
        paths.config_path.to_string_lossy().into_owned(),
        "-runtime-binding-clear".to_string(),
    ];

    let payload = run_bridge_json::<ClearPayload>(paths, &args)?;
    if !payload.cleared {
        return Err("runtime binding clear command returned cleared=false".to_string());
    }

    Ok(())
}

fn clear_oauth_local_state_with_paths(paths: &BridgePaths) -> Result<(), String> {
    let args = vec![
        "-config".to_string(),
        paths.config_path.to_string_lossy().into_owned(),
        "-oauth-clear-local-state".to_string(),
    ];

    let payload = run_bridge_json::<ClearPayload>(paths, &args)?;
    if !payload.cleared {
        return Err("oauth clear command returned cleared=false".to_string());
    }

    Ok(())
}

#[cfg(test)]
fn clear_local_state_on_exit_with_state_dir(state_dir_override: Option<String>) {
    let paths = match BridgePaths::resolve(state_dir_override) {
        Ok(paths) => paths,
        Err(_) => return,
    };

    if !paths.binary_path.exists() || !paths.config_path.exists() {
        return;
    }

    let args = vec![
        "-config".to_string(),
        paths.config_path.to_string_lossy().into_owned(),
        "-oauth-clear-local-state".to_string(),
    ];

    let _ = Command::new(&paths.binary_path).args(args).output();
}

impl BridgePaths {
    fn resolve(state_dir_override: Option<String>) -> Result<BridgePaths, String> {
        let state_dir = match state_dir_override {
            Some(path) if !path.trim().is_empty() => PathBuf::from(path.trim()),
            _ => default_state_dir()?,
        };

        let config_path = state_dir.join("config").join("bridge.json");
        let binary_path = state_dir.join("bin").join(binary_file_name());
        Ok(BridgePaths {
            state_dir,
            config_path,
            binary_path,
        })
    }
}

fn default_state_dir() -> Result<PathBuf, String> {
    let home = dirs::home_dir().ok_or_else(|| "resolve home directory".to_string())?;
    Ok(home.join(".agent-flows-bridge"))
}

fn binary_file_name() -> String {
    format!("agent-flows-bridge{}", std::env::consts::EXE_SUFFIX)
}

fn sanitize_bridge_stderr(stderr: &str) -> String {
    let mut sanitized = stderr.to_string();
    for needle in [
        "access_token",
        "refresh_token",
        "Authorization",
        "code=",
        "state=",
    ] {
        sanitized = redact_token_like_values(&sanitized, needle);
    }

    let trimmed = sanitized.trim();
    if trimmed.chars().count() > 512 {
        let truncated: String = trimmed.chars().take(512).collect();
        return format!("{truncated}...");
    }

    trimmed.to_string()
}

fn redact_token_like_values(input: &str, needle: &str) -> String {
    if !input.contains(needle) {
        return input.to_string();
    }

    let mut redacted = String::with_capacity(input.len());
    let mut remainder = input;

    while let Some(index) = remainder.find(needle) {
        let (before, after_needle) = remainder.split_at(index);
        redacted.push_str(before);
        redacted.push_str(needle);

        let value = &after_needle[needle.len()..];
        if needle.ends_with('=') {
            let stop = value
                .find(|character: char| character == '&' || character == ' ' || character == '\n')
                .unwrap_or(value.len());
            redacted.push_str("[REDACTED]");
            remainder = &value[stop..];
            continue;
        }

        if value.starts_with('=') {
            redacted.push('=');
            let stripped = &value[1..];
            let stop = stripped
                .find(|character: char| character == '&' || character == ' ' || character == '\n')
                .unwrap_or(stripped.len());
            redacted.push_str("[REDACTED]");
            remainder = &stripped[stop..];
            continue;
        }

        if value.starts_with(':') {
            redacted.push(':');
            let stripped = value[1..].trim_start();
            let stop = stripped
                .find(|character: char| character == '\n' || character == ' ')
                .unwrap_or(stripped.len());
            redacted.push_str(" [REDACTED]");
            remainder = &stripped[stop..];
            continue;
        }

        remainder = value;
    }

    redacted.push_str(remainder);
    redacted
}

#[cfg(test)]
mod tests {
    use super::{
        authorize_and_connect_with_paths_and_browser, binary_file_name,
        clear_local_state_on_exit_with_state_dir, default_state_dir, embedded_tray_icon_asset_name,
        ensure_default_bridge_config_ready, forget_runtime_binding_with_state_dir,
        parse_request_path, parse_tray_menu_action, sanitize_bridge_stderr,
        sync_packaged_bridge_binary, tray_icon_uses_template_rendering,
        wait_for_callback_and_open_with_browser, BridgePaths, TrayMenuAction,
    };
    use std::fs;
    use std::io::{Read, Write};
    use std::net::{TcpListener, TcpStream};
    use std::path::PathBuf;
    use std::thread;
    use std::time::{SystemTime, UNIX_EPOCH};

    #[cfg(unix)]
    use std::os::unix::fs::PermissionsExt;

    #[test]
    fn parse_request_path_accepts_get_callback() {
        let result = parse_request_path("GET /oauth/callback?code=abc&state=xyz HTTP/1.1");
        let path = result.expect("expected callback path");
        assert_eq!(path, "/oauth/callback?code=abc&state=xyz");
    }

    #[test]
    fn parse_request_path_rejects_non_get() {
        let result = parse_request_path("POST /oauth/callback HTTP/1.1");
        assert!(result.is_err());
    }

    #[test]
    fn wait_for_callback_ignores_wrong_path_then_accepts_valid_callback() {
        let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
        let port = listener.local_addr().expect("listener addr").port();
        let redirect_uri = format!("http://127.0.0.1:{port}/oauth/callback");

        let callback_url = wait_for_callback_and_open_with_browser(
            listener,
            "https://agentflows.example.test/oauth/bridge/sign-in",
            &redirect_uri,
            "state-123",
            |_| {
                spawn_http_request(
                    port,
                    "GET /wrong-path?code=abc&state=state-123 HTTP/1.1\r\n\r\n",
                );
                spawn_http_request(
                    port,
                    "GET /oauth/callback?code=abc&state=state-123 HTTP/1.1\r\n\r\n",
                );
                Ok(())
            },
        )
        .expect("expected valid callback");

        assert!(callback_url.ends_with("/oauth/callback?code=abc&state=state-123"));
    }

    #[test]
    fn wait_for_callback_ignores_wrong_state_then_accepts_valid_callback() {
        let listener = TcpListener::bind("127.0.0.1:0").expect("bind listener");
        let port = listener.local_addr().expect("listener addr").port();
        let redirect_uri = format!("http://127.0.0.1:{port}/oauth/callback");

        let callback_url = wait_for_callback_and_open_with_browser(
            listener,
            "https://agentflows.example.test/oauth/bridge/sign-in",
            &redirect_uri,
            "state-123",
            |_| {
                spawn_http_request(
                    port,
                    "GET /oauth/callback?code=abc&state=wrong-state HTTP/1.1\r\n\r\n",
                );
                spawn_http_request(
                    port,
                    "GET /oauth/callback?code=abc&state=state-123 HTTP/1.1\r\n\r\n",
                );
                Ok(())
            },
        )
        .expect("expected valid callback");

        assert!(callback_url.ends_with("/oauth/callback?code=abc&state=state-123"));
    }

    #[test]
    fn sanitize_bridge_stderr_redacts_and_truncates_secret_values() {
        let raw = format!(
            "access_token=token123 refresh_token=token456 code=abc state=xyz {}",
            "x".repeat(600)
        );

        let sanitized = sanitize_bridge_stderr(&raw);

        assert!(sanitized.contains("access_token=[REDACTED]"));
        assert!(sanitized.contains("refresh_token=[REDACTED]"));
        assert!(sanitized.contains("code=[REDACTED]"));
        assert!(sanitized.contains("state=[REDACTED]"));
        assert!(!sanitized.contains("token123"));
        assert!(sanitized.ends_with("..."));
    }

    #[test]
    fn parse_tray_menu_action_maps_known_ids() {
        let open_action = parse_tray_menu_action("open-main-window");
        let sign_in_action = parse_tray_menu_action("start-sign-in");
        let refresh_action = parse_tray_menu_action("refresh-status");
        let forget_action = parse_tray_menu_action("forget-runtime");
        let quit_action = parse_tray_menu_action("quit-app");

        assert_eq!(open_action, Some(TrayMenuAction::OpenMainWindow));
        assert_eq!(sign_in_action, Some(TrayMenuAction::StartSignIn));
        assert_eq!(refresh_action, Some(TrayMenuAction::RefreshStatus));
        assert_eq!(forget_action, Some(TrayMenuAction::ForgetRuntime));
        assert_eq!(quit_action, Some(TrayMenuAction::QuitApp));
    }

    #[test]
    fn parse_tray_menu_action_rejects_unknown_id() {
        let action = parse_tray_menu_action("unsupported-id");
        assert_eq!(action, None);
    }

    #[test]
    fn embedded_tray_icon_asset_name_matches_platform_expectation() {
        let asset_name = embedded_tray_icon_asset_name();

        #[cfg(target_os = "macos")]
        assert_eq!(asset_name, "tray-icon.png");

        #[cfg(not(target_os = "macos"))]
        assert_eq!(asset_name, "icon.png");
    }

    #[test]
    fn tray_icon_template_rendering_matches_platform_expectation() {
        #[cfg(target_os = "macos")]
        assert!(tray_icon_uses_template_rendering());

        #[cfg(not(target_os = "macos"))]
        assert!(!tray_icon_uses_template_rendering());
    }

    #[test]
    fn default_state_dir_ends_with_hidden_bridge_dir() {
        let result = default_state_dir().expect("expected default state dir");
        let path = result.to_string_lossy();
        assert!(path.ends_with(".agent-flows-bridge"));
    }

    #[test]
    fn binary_file_name_matches_platform_suffix() {
        let binary_name = binary_file_name();
        assert!(binary_name.starts_with("agent-flows-bridge"));
        assert!(binary_name.ends_with(std::env::consts::EXE_SUFFIX));
    }

    #[test]
    fn ensure_default_bridge_config_ready_writes_first_run_config() {
        let temp_dir = unique_temp_dir("af-bridge-tauri-config-test");
        let state_dir = temp_dir.join(".agent-flows-bridge");
        let config_path = state_dir.join("config").join("bridge.json");
        let binary_path = state_dir.join("bin").join(binary_file_name());

        let paths = BridgePaths {
            state_dir: state_dir.clone(),
            config_path: config_path.clone(),
            binary_path,
        };

        ensure_default_bridge_config_ready(&paths).expect("write default bridge config");

        let config_raw = fs::read_to_string(&config_path).expect("read default bridge config");
        let payload: serde_json::Value =
            serde_json::from_str(&config_raw).expect("parse default bridge config");

        assert_eq!(
            payload.get("api_base_url").and_then(|value| value.as_str()),
            Some("https://agentflows.appliedagentics.ai")
        );
        assert_eq!(
            payload.get("runtime_url").and_then(|value| value.as_str()),
            Some("http://127.0.0.1:18789")
        );
        assert_eq!(
            payload.get("state_dir").and_then(|value| value.as_str()),
            Some(state_dir.to_string_lossy().as_ref())
        );
        assert_eq!(
            payload
                .get("oauth_client_id")
                .and_then(|value| value.as_str()),
            Some("agent-flows-bridge")
        );
        assert_eq!(
            payload
                .get("transport_mode")
                .and_then(|value| value.as_str()),
            Some("auto")
        );

        let _ = fs::remove_dir_all(&temp_dir);
    }

    #[test]
    #[cfg(unix)]
    fn clear_local_state_on_exit_runs_bridge_clear_command() {
        let temp_dir = unique_temp_dir("af-bridge-tauri-exit-test");
        let state_dir = temp_dir.join(".agent-flows-bridge");
        let config_dir = state_dir.join("config");
        let bin_dir = state_dir.join("bin");
        fs::create_dir_all(&config_dir).expect("create config dir");
        fs::create_dir_all(&bin_dir).expect("create bin dir");

        let config_path = config_dir.join("bridge.json");
        fs::write(&config_path, "{}").expect("write config");

        let invocation_path = temp_dir.join("invocation.args");
        let binary_path = bin_dir.join(binary_file_name());
        let script = format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"{}\"\n",
            invocation_path.to_string_lossy()
        );
        fs::write(&binary_path, script).expect("write fake bridge binary");

        let mut permissions = fs::metadata(&binary_path)
            .expect("read fake bridge binary metadata")
            .permissions();
        permissions.set_mode(0o755);
        fs::set_permissions(&binary_path, permissions).expect("chmod fake bridge binary");

        clear_local_state_on_exit_with_state_dir(Some(state_dir.to_string_lossy().into_owned()));

        let invocation_raw = fs::read_to_string(&invocation_path).expect("read invocation args");
        let invocation_lines: Vec<&str> = invocation_raw.lines().collect();

        assert_eq!(invocation_lines.first().copied(), Some("-config"));
        assert_eq!(
            invocation_lines.get(1).copied(),
            Some(config_path.to_string_lossy().as_ref())
        );
        assert_eq!(
            invocation_lines.get(2).copied(),
            Some("-oauth-clear-local-state")
        );

        let _ = fs::remove_dir_all(&temp_dir);
    }

    #[test]
    #[cfg(unix)]
    fn forget_runtime_binding_runs_binding_and_oauth_clear_commands() {
        let temp_dir = unique_temp_dir("af-bridge-tauri-forget-test");
        let state_dir = temp_dir.join(".agent-flows-bridge");
        let config_dir = state_dir.join("config");
        let bin_dir = state_dir.join("bin");
        fs::create_dir_all(&config_dir).expect("create config dir");
        fs::create_dir_all(&bin_dir).expect("create bin dir");

        let config_path = config_dir.join("bridge.json");
        fs::write(&config_path, "{}").expect("write config");

        let invocation_path = temp_dir.join("invocation.args");
        let binary_path = bin_dir.join(binary_file_name());
        let script = format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"{}\"\nprintf -- '--\\n' >> \"{}\"\nif [ \"$3\" = \"-disconnect-runtime\" ]; then\n  printf '{{\"revoked\":true}}\\n'\nelse\n  printf '{{\"cleared\":true}}\\n'\nfi\n",
            invocation_path.to_string_lossy(),
            invocation_path.to_string_lossy(),
        );
        fs::write(&binary_path, script).expect("write fake bridge binary");

        let mut permissions = fs::metadata(&binary_path)
            .expect("read fake bridge binary metadata")
            .permissions();
        permissions.set_mode(0o755);
        fs::set_permissions(&binary_path, permissions).expect("chmod fake bridge binary");

        forget_runtime_binding_with_state_dir(Some(state_dir.to_string_lossy().into_owned()))
            .expect("forget runtime binding");

        let invocation_raw = fs::read_to_string(&invocation_path).expect("read invocation args");
        let invocation_lines: Vec<&str> = invocation_raw.lines().collect();

        assert_eq!(invocation_lines.first().copied(), Some("-config"));
        assert_eq!(
            invocation_lines.get(1).copied(),
            Some(config_path.to_string_lossy().as_ref())
        );
        assert_eq!(
            invocation_lines.get(2).copied(),
            Some("-disconnect-runtime")
        );
        assert_eq!(invocation_lines.get(3).copied(), Some("--"));
        assert_eq!(invocation_lines.get(4).copied(), Some("-config"));
        assert_eq!(
            invocation_lines.get(5).copied(),
            Some(config_path.to_string_lossy().as_ref())
        );
        assert_eq!(
            invocation_lines.get(6).copied(),
            Some("-runtime-binding-clear")
        );
        assert_eq!(invocation_lines.get(7).copied(), Some("--"));
        assert_eq!(invocation_lines.get(8).copied(), Some("-config"));
        assert_eq!(
            invocation_lines.get(9).copied(),
            Some(config_path.to_string_lossy().as_ref())
        );
        assert_eq!(
            invocation_lines.get(10).copied(),
            Some("-oauth-clear-local-state")
        );
        assert_eq!(invocation_lines.get(11).copied(), Some("--"));

        let _ = fs::remove_dir_all(&temp_dir);
    }

    #[test]
    #[cfg(unix)]
    fn forget_runtime_binding_stops_when_disconnect_fails() {
        let temp_dir = unique_temp_dir("af-bridge-tauri-forget-fail-test");
        let state_dir = temp_dir.join(".agent-flows-bridge");
        let config_dir = state_dir.join("config");
        let bin_dir = state_dir.join("bin");
        fs::create_dir_all(&config_dir).expect("create config dir");
        fs::create_dir_all(&bin_dir).expect("create bin dir");

        let config_path = config_dir.join("bridge.json");
        fs::write(&config_path, "{}").expect("write config");

        let invocation_path = temp_dir.join("invocation.args");
        let binary_path = bin_dir.join(binary_file_name());
        let script = format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"{}\"\nprintf -- '--\\n' >> \"{}\"\nprintf '{{\"revoked\":false}}\\n'\n",
            invocation_path.to_string_lossy(),
            invocation_path.to_string_lossy(),
        );
        fs::write(&binary_path, script).expect("write fake bridge binary");

        let mut permissions = fs::metadata(&binary_path)
            .expect("read fake bridge binary metadata")
            .permissions();
        permissions.set_mode(0o755);
        fs::set_permissions(&binary_path, permissions).expect("chmod fake bridge binary");

        let result =
            forget_runtime_binding_with_state_dir(Some(state_dir.to_string_lossy().into_owned()));
        assert!(result.is_err());

        let invocation_raw = fs::read_to_string(&invocation_path).expect("read invocation args");
        let invocation_lines: Vec<&str> = invocation_raw.lines().collect();

        assert_eq!(invocation_lines.first().copied(), Some("-config"));
        assert_eq!(
            invocation_lines.get(1).copied(),
            Some(config_path.to_string_lossy().as_ref())
        );
        assert_eq!(
            invocation_lines.get(2).copied(),
            Some("-disconnect-runtime")
        );
        assert_eq!(invocation_lines.get(3).copied(), Some("--"));
        assert_eq!(invocation_lines.len(), 4);

        let _ = fs::remove_dir_all(&temp_dir);
    }

    #[test]
    #[cfg(unix)]
    fn forget_runtime_binding_continues_when_disconnect_token_is_invalid() {
        let temp_dir = unique_temp_dir("af-bridge-tauri-forget-invalid-token-test");
        let state_dir = temp_dir.join(".agent-flows-bridge");
        let config_dir = state_dir.join("config");
        let bin_dir = state_dir.join("bin");
        fs::create_dir_all(&config_dir).expect("create config dir");
        fs::create_dir_all(&bin_dir).expect("create bin dir");

        let config_path = config_dir.join("bridge.json");
        fs::write(&config_path, "{}").expect("write config");

        let invocation_path = temp_dir.join("invocation.args");
        let binary_path = bin_dir.join(binary_file_name());
        let script = format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"{}\"\nprintf -- '--\\n' >> \"{}\"\nif [ \"$3\" = \"-disconnect-runtime\" ]; then\n  printf '%s\\n' 'disconnect runtime: disconnect request failed: status=401 body={{\"error\":\"Invalid connector token\",\"code\":\"INVALID_CONNECTOR_TOKEN\"}}' >&2\n  exit 1\nelse\n  printf '{{\"cleared\":true}}\\n'\nfi\n",
            invocation_path.to_string_lossy(),
            invocation_path.to_string_lossy(),
        );
        fs::write(&binary_path, script).expect("write fake bridge binary");

        let mut permissions = fs::metadata(&binary_path)
            .expect("read fake bridge binary metadata")
            .permissions();
        permissions.set_mode(0o755);
        fs::set_permissions(&binary_path, permissions).expect("chmod fake bridge binary");

        forget_runtime_binding_with_state_dir(Some(state_dir.to_string_lossy().into_owned()))
            .expect("forget runtime binding should clear local state");

        let invocation_raw = fs::read_to_string(&invocation_path).expect("read invocation args");
        let invocation_lines: Vec<&str> = invocation_raw.lines().collect();

        assert_eq!(invocation_lines.first().copied(), Some("-config"));
        assert_eq!(
            invocation_lines.get(1).copied(),
            Some(config_path.to_string_lossy().as_ref())
        );
        assert_eq!(
            invocation_lines.get(2).copied(),
            Some("-disconnect-runtime")
        );
        assert_eq!(invocation_lines.get(3).copied(), Some("--"));
        assert_eq!(invocation_lines.get(4).copied(), Some("-config"));
        assert_eq!(
            invocation_lines.get(5).copied(),
            Some(config_path.to_string_lossy().as_ref())
        );
        assert_eq!(
            invocation_lines.get(6).copied(),
            Some("-runtime-binding-clear")
        );
        assert_eq!(invocation_lines.get(7).copied(), Some("--"));
        assert_eq!(invocation_lines.get(8).copied(), Some("-config"));
        assert_eq!(
            invocation_lines.get(9).copied(),
            Some(config_path.to_string_lossy().as_ref())
        );
        assert_eq!(
            invocation_lines.get(10).copied(),
            Some("-oauth-clear-local-state")
        );
        assert_eq!(invocation_lines.get(11).copied(), Some("--"));

        let _ = fs::remove_dir_all(&temp_dir);
    }

    #[test]
    #[cfg(unix)]
    fn forget_runtime_binding_continues_when_disconnect_refresh_grant_is_invalid() {
        let temp_dir = unique_temp_dir("af-bridge-tauri-forget-invalid-grant-test");
        let state_dir = temp_dir.join(".agent-flows-bridge");
        let config_dir = state_dir.join("config");
        let bin_dir = state_dir.join("bin");
        fs::create_dir_all(&config_dir).expect("create config dir");
        fs::create_dir_all(&bin_dir).expect("create bin dir");

        let config_path = config_dir.join("bridge.json");
        fs::write(&config_path, "{}").expect("write config");

        let invocation_path = temp_dir.join("invocation.args");
        let binary_path = bin_dir.join(binary_file_name());
        let script = format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"{}\"\nprintf -- '--\\n' >> \"{}\"\nif [ \"$3\" = \"-disconnect-runtime\" ]; then\n  printf '%s\\n' 'disconnect runtime: refresh exchange failed: status=400 code=invalid_grant body={{\"error\":\"invalid_grant\"}}' >&2\n  exit 1\nelse\n  printf '{{\"cleared\":true}}\\n'\nfi\n",
            invocation_path.to_string_lossy(),
            invocation_path.to_string_lossy(),
        );
        fs::write(&binary_path, script).expect("write fake bridge binary");

        let mut permissions = fs::metadata(&binary_path)
            .expect("read fake bridge binary metadata")
            .permissions();
        permissions.set_mode(0o755);
        fs::set_permissions(&binary_path, permissions).expect("chmod fake bridge binary");

        forget_runtime_binding_with_state_dir(Some(state_dir.to_string_lossy().into_owned()))
            .expect("forget runtime binding should clear local state");

        let invocation_raw = fs::read_to_string(&invocation_path).expect("read invocation args");
        let invocation_lines: Vec<&str> = invocation_raw.lines().collect();

        assert_eq!(invocation_lines.first().copied(), Some("-config"));
        assert_eq!(
            invocation_lines.get(1).copied(),
            Some(config_path.to_string_lossy().as_ref())
        );
        assert_eq!(
            invocation_lines.get(2).copied(),
            Some("-disconnect-runtime")
        );
        assert_eq!(invocation_lines.get(3).copied(), Some("--"));
        assert_eq!(invocation_lines.get(4).copied(), Some("-config"));
        assert_eq!(
            invocation_lines.get(5).copied(),
            Some(config_path.to_string_lossy().as_ref())
        );
        assert_eq!(
            invocation_lines.get(6).copied(),
            Some("-runtime-binding-clear")
        );
        assert_eq!(invocation_lines.get(7).copied(), Some("--"));
        assert_eq!(invocation_lines.get(8).copied(), Some("-config"));
        assert_eq!(
            invocation_lines.get(9).copied(),
            Some(config_path.to_string_lossy().as_ref())
        );
        assert_eq!(
            invocation_lines.get(10).copied(),
            Some("-oauth-clear-local-state")
        );
        assert_eq!(invocation_lines.get(11).copied(), Some("--"));

        let _ = fs::remove_dir_all(&temp_dir);
    }

    #[test]
    #[cfg(unix)]
    fn sync_packaged_bridge_binary_replaces_stale_installed_binary() {
        let temp_dir = unique_temp_dir("af-bridge-tauri-sync-test");
        let state_dir = temp_dir.join(".agent-flows-bridge");
        let config_dir = state_dir.join("config");
        let bin_dir = state_dir.join("bin");
        let resource_dir = temp_dir.join("resources").join("bridge");
        fs::create_dir_all(&config_dir).expect("create config dir");
        fs::create_dir_all(&bin_dir).expect("create bin dir");
        fs::create_dir_all(&resource_dir).expect("create resource dir");

        let config_path = config_dir.join("bridge.json");
        fs::write(&config_path, "{}").expect("write config");

        let installed_binary_path = bin_dir.join(binary_file_name());
        fs::write(&installed_binary_path, "#!/bin/sh\necho stale\n").expect("write stale binary");
        make_executable(&installed_binary_path);

        let packaged_binary_path = resource_dir.join(binary_file_name());
        let packaged_contents = "#!/bin/sh\necho packaged\n";
        fs::write(&packaged_binary_path, packaged_contents).expect("write packaged binary");
        make_executable(&packaged_binary_path);

        let paths = BridgePaths {
            state_dir,
            config_path,
            binary_path: installed_binary_path.clone(),
        };

        sync_packaged_bridge_binary(&paths, &packaged_binary_path)
            .expect("sync packaged bridge binary");

        let installed_contents =
            fs::read_to_string(&installed_binary_path).expect("read synced bridge binary");
        assert_eq!(installed_contents, packaged_contents);

        let _ = fs::remove_dir_all(&temp_dir);
    }

    #[test]
    #[cfg(unix)]
    fn authorize_and_connect_replaces_stale_binary_before_oauth_start() {
        let temp_dir = unique_temp_dir("af-bridge-tauri-authorize-sync-test");
        let state_dir = temp_dir.join(".agent-flows-bridge");
        let config_dir = state_dir.join("config");
        let bin_dir = state_dir.join("bin");
        let resource_dir = temp_dir.join("resources").join("bridge");
        fs::create_dir_all(&config_dir).expect("create config dir");
        fs::create_dir_all(&bin_dir).expect("create bin dir");
        fs::create_dir_all(&resource_dir).expect("create resource dir");

        let config_path = config_dir.join("bridge.json");
        fs::write(&config_path, "{}").expect("write config");

        let stale_invocation_path = temp_dir.join("stale.invocation");
        let installed_binary_path = bin_dir.join(binary_file_name());
        let stale_script = format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"{}\"\nprintf -- '--\\n' >> \"{}\"\nprintf '%s\\n' 'flag provided but not defined: -oauth-redirect-port' >&2\nexit 2\n",
            stale_invocation_path.to_string_lossy(),
            stale_invocation_path.to_string_lossy(),
        );
        fs::write(&installed_binary_path, stale_script).expect("write stale binary");
        make_executable(&installed_binary_path);

        let packaged_invocation_path = temp_dir.join("packaged.invocation");
        let packaged_binary_path = resource_dir.join(binary_file_name());
        let packaged_script = format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"{}\"\nprintf -- '--\\n' >> \"{}\"\nif [ \"$3\" = \"-oauth-start\" ]; then\n  printf '{{\"authorize_url\":\"https://agentflows.example.test/oauth/bridge/sign-in?callback_port=%s\",\"redirect_uri\":\"http://127.0.0.1:%s/oauth/callback\",\"state\":\"state-123\"}}\\n' \"$5\" \"$5\"\nelif [ \"$3\" = \"-oauth-complete-callback-url\" ]; then\n  printf '{{\"connector_id\":7,\"runtime_id\":98,\"runtime_kind\":\"local_connector\",\"scope\":\"connector:bootstrap connector:webhook\"}}\\n'\nelse\n  printf '{{}}\\n'\nfi\n",
            packaged_invocation_path.to_string_lossy(),
            packaged_invocation_path.to_string_lossy(),
        );
        fs::write(&packaged_binary_path, packaged_script).expect("write packaged binary");
        make_executable(&packaged_binary_path);

        let paths = BridgePaths {
            state_dir,
            config_path,
            binary_path: installed_binary_path.clone(),
        };

        let result = authorize_and_connect_with_paths_and_browser(
            &paths,
            &packaged_binary_path,
            |authorize_url| {
                let parsed_url = url::Url::parse(authorize_url).expect("parse authorize url");
                let callback_port = parsed_url
                    .query_pairs()
                    .find_map(|(key, value)| (key == "callback_port").then(|| value.into_owned()))
                    .expect("callback port query param");
                let port = callback_port.parse::<u16>().expect("parse callback port");
                spawn_http_request(
                    port,
                    "GET /oauth/callback?code=abc&state=state-123 HTTP/1.1\r\n\r\n",
                );
                Ok(())
            },
        )
        .expect("authorize and connect");

        assert_eq!(result.connector_id, 7);
        assert_eq!(result.runtime_id, 98);
        assert_eq!(result.runtime_kind, "local_connector");
        assert_eq!(result.scope, "connector:bootstrap connector:webhook");

        assert!(!stale_invocation_path.exists());

        let invocation_raw =
            fs::read_to_string(&packaged_invocation_path).expect("read packaged invocation args");
        let invocation_lines: Vec<&str> = invocation_raw.lines().collect();
        assert_eq!(invocation_lines.first().copied(), Some("-config"));
        assert_eq!(invocation_lines.get(2).copied(), Some("-oauth-start"));
        assert_eq!(
            invocation_lines.get(3).copied(),
            Some("-oauth-redirect-port")
        );

        let _ = fs::remove_dir_all(&temp_dir);
    }

    #[test]
    #[cfg(unix)]
    fn authorize_and_connect_creates_missing_config_before_oauth_start() {
        let temp_dir = unique_temp_dir("af-bridge-tauri-authorize-config-test");
        let state_dir = temp_dir.join(".agent-flows-bridge");
        let bin_dir = state_dir.join("bin");
        let resource_dir = temp_dir.join("resources").join("bridge");
        fs::create_dir_all(&bin_dir).expect("create bin dir");
        fs::create_dir_all(&resource_dir).expect("create resource dir");

        let installed_binary_path = bin_dir.join(binary_file_name());
        fs::write(&installed_binary_path, "#!/bin/sh\necho stale\n").expect("write stale binary");
        make_executable(&installed_binary_path);

        let packaged_invocation_path = temp_dir.join("packaged-config.invocation");
        let packaged_binary_path = resource_dir.join(binary_file_name());
        let packaged_script = format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"{}\"\nprintf -- '--\\n' >> \"{}\"\nif [ \"$3\" = \"-oauth-start\" ]; then\n  printf '{{\"authorize_url\":\"https://agentflows.example.test/oauth/bridge/sign-in?callback_port=%s\",\"redirect_uri\":\"http://127.0.0.1:%s/oauth/callback\",\"state\":\"state-123\"}}\\n' \"$5\" \"$5\"\nelif [ \"$3\" = \"-oauth-complete-callback-url\" ]; then\n  printf '{{\"connector_id\":7,\"runtime_id\":98,\"runtime_kind\":\"local_connector\",\"scope\":\"connector:bootstrap connector:webhook\"}}\\n'\nelse\n  printf '{{}}\\n'\nfi\n",
            packaged_invocation_path.to_string_lossy(),
            packaged_invocation_path.to_string_lossy(),
        );
        fs::write(&packaged_binary_path, packaged_script).expect("write packaged binary");
        make_executable(&packaged_binary_path);

        let config_path = state_dir.join("config").join("bridge.json");
        let paths = BridgePaths {
            state_dir: state_dir.clone(),
            config_path: config_path.clone(),
            binary_path: installed_binary_path.clone(),
        };

        let result = authorize_and_connect_with_paths_and_browser(
            &paths,
            &packaged_binary_path,
            |authorize_url| {
                let parsed_url = url::Url::parse(authorize_url).expect("parse authorize url");
                let callback_port = parsed_url
                    .query_pairs()
                    .find_map(|(key, value)| (key == "callback_port").then(|| value.into_owned()))
                    .expect("callback port query param");
                let port = callback_port.parse::<u16>().expect("parse callback port");
                spawn_http_request(
                    port,
                    "GET /oauth/callback?code=abc&state=state-123 HTTP/1.1\r\n\r\n",
                );
                Ok(())
            },
        )
        .expect("authorize and connect");

        assert_eq!(result.connector_id, 7);
        assert!(config_path.exists());

        let config_raw = fs::read_to_string(&config_path).expect("read created bridge config");
        let payload: serde_json::Value =
            serde_json::from_str(&config_raw).expect("parse created bridge config");
        assert_eq!(
            payload.get("state_dir").and_then(|value| value.as_str()),
            Some(state_dir.to_string_lossy().as_ref())
        );

        let invocation_raw =
            fs::read_to_string(&packaged_invocation_path).expect("read packaged invocation args");
        let invocation_lines: Vec<&str> = invocation_raw.lines().collect();
        assert_eq!(invocation_lines.first().copied(), Some("-config"));
        assert_eq!(
            invocation_lines.get(1).copied(),
            Some(config_path.to_string_lossy().as_ref())
        );
        assert_eq!(invocation_lines.get(2).copied(), Some("-oauth-start"));

        let _ = fs::remove_dir_all(&temp_dir);
    }

    #[cfg(unix)]
    fn make_executable(path: &PathBuf) {
        let mut permissions = fs::metadata(path)
            .expect("read fake bridge binary metadata")
            .permissions();
        permissions.set_mode(0o755);
        fs::set_permissions(path, permissions).expect("chmod fake bridge binary");
    }

    fn unique_temp_dir(prefix: &str) -> PathBuf {
        let nonce = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("clock drift")
            .as_nanos();
        std::env::temp_dir().join(format!("{prefix}-{nonce}"))
    }

    fn spawn_http_request(port: u16, request: &str) {
        let request = request.to_string();
        let _ = thread::spawn(move || {
            let mut stream =
                TcpStream::connect(("127.0.0.1", port)).expect("connect callback listener");
            stream
                .write_all(request.as_bytes())
                .expect("write callback request");
            let mut response = Vec::new();
            let _ = stream.read_to_end(&mut response);
        });
    }
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let app = tauri::Builder::default()
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_opener::init())
        .setup(|app| Ok(setup_system_tray(app)?))
        .invoke_handler(tauri::generate_handler![
            bridge_status,
            authorize_and_connect,
            forget_runtime_binding
        ])
        .build(tauri::generate_context!())
        .expect("error while building tauri application");

    app.run(|app_handle, event| {
        if let RunEvent::WindowEvent { label, event, .. } = event {
            if label == "main" {
                if let WindowEvent::CloseRequested { api, .. } = event {
                    api.prevent_close();
                    if let Err(error) = hide_main_window(app_handle) {
                        eprintln!("hide main window on close failed: {error}");
                    }
                }
            }
        }
    });
}
