package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/agentflows/agent-flows-bridge/client/internal/binding"
	"github.com/agentflows/agent-flows-bridge/client/internal/bootstrap"
	"github.com/agentflows/agent-flows-bridge/client/internal/config"
	"github.com/agentflows/agent-flows-bridge/client/internal/daemon"
	"github.com/agentflows/agent-flows-bridge/client/internal/diagnostics"
	"github.com/agentflows/agent-flows-bridge/client/internal/oauth"
	"github.com/agentflows/agent-flows-bridge/client/internal/packaging"
	"github.com/agentflows/agent-flows-bridge/client/internal/receipt"
	"github.com/agentflows/agent-flows-bridge/client/internal/secrets"
	"github.com/agentflows/agent-flows-bridge/client/internal/session"
	"github.com/agentflows/agent-flows-bridge/client/internal/ui"
	"github.com/agentflows/agent-flows-bridge/client/internal/wss"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	exitCode := run(os.Args[1:], os.Stdout, os.Stderr)
	os.Exit(exitCode)
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	flagSet := flag.NewFlagSet("agent-flows-bridge", flag.ContinueOnError)
	flagSet.SetOutput(stderr)

	configPath := flagSet.String("config", "", "Path to JSON config file")
	printVersion := flagSet.Bool("version", false, "Print build version metadata and exit")
	printConfig := flagSet.Bool("print-config", false, "Print resolved config and exit")
	oauthStart := flagSet.Bool("oauth-start", false, "Start OAuth login and select runtime in browser")
	oauthStartRuntimeID := flagSet.Int("oauth-start-runtime-id", 0, "Start OAuth login for runtime_id")
	oauthRedirectPort := flagSet.Int("oauth-redirect-port", 0, "Override OAuth localhost callback port")
	oauthCompleteCallbackURL := flagSet.String("oauth-complete-callback-url", "", "Complete OAuth login using callback URL")
	oauthSessionStatus := flagSet.Bool("oauth-session-status", false, "Print stored OAuth session status and exit")
	oauthClearLocalState := flagSet.Bool("oauth-clear-local-state", false, "Delete stored OAuth session/bootstrap data and pending state")
	disconnectRuntime := flagSet.Bool("disconnect-runtime", false, "Revoke connector runtime access using the current OAuth session")
	runtimeBindingStatus := flagSet.Bool("runtime-binding-status", false, "Print stored runtime binding status and exit")
	runtimeBindingClear := flagSet.Bool("runtime-binding-clear", false, "Delete stored runtime binding data and exit")
	verifyOpenClawReceipt := flagSet.Bool("verify-openclaw-receipt", false, "Probe OpenClaw hook endpoint and verify receipt evidence")
	processWebhookOnce := flagSet.Bool("process-webhook-once", false, "Claim one queued webhook event, deliver to local OpenClaw, and report result")
	runDaemon := flagSet.Bool("run-daemon", false, "Run continuous webhook claim/deliver/report worker")
	uiServe := flagSet.Bool("ui-serve", false, "Run minimal desktop shell UI server")
	uiListen := flagSet.String("ui-listen", "127.0.0.1:49300", "UI listen address")
	uiRuntimeID := flagSet.Int("ui-runtime-id", 0, "Runtime id used by UI authorize action")
	uiServeDurationRaw := flagSet.String("ui-serve-duration", "", "Optional UI runtime duration, e.g. 30s")
	installUserService := flagSet.Bool("install-user-service", false, "Install binary, config, and user startup service")
	installSourceBinary := flagSet.String("install-source-binary", "", "Path to source binary for install")
	installHomeDir := flagSet.String("install-home-dir", "", "Override install home dir")
	installGOOS := flagSet.String("install-goos", "", "Override install target GOOS")
	uninstallUserService := flagSet.Bool("uninstall-user-service", false, "Remove binary and user startup service")
	uninstallHomeDir := flagSet.String("uninstall-home-dir", "", "Override uninstall home dir")
	uninstallGOOS := flagSet.String("uninstall-goos", "", "Override uninstall target GOOS")

	if err := flagSet.Parse(args); err != nil {
		fmt.Fprintf(stderr, "parse args: %v\n", err)
		return 2
	}

	if modeCount(
		*printVersion,
		*printConfig,
		*uiServe,
		*installUserService,
		*uninstallUserService,
		*oauthSessionStatus,
		*oauthClearLocalState,
		*disconnectRuntime,
		*runtimeBindingStatus,
		*runtimeBindingClear,
		*verifyOpenClawReceipt,
		*processWebhookOnce,
		*runDaemon,
		*oauthStart,
		*oauthStartRuntimeID > 0,
		strings.TrimSpace(*oauthCompleteCallbackURL) != "",
	) > 1 {
		fmt.Fprintln(stderr, "print-config, install, uninstall, ui, daemon, oauth, runtime-binding, and disconnect modes are mutually exclusive")
		return 2
	}

	uiServeDuration := time.Duration(0)
	if strings.TrimSpace(*uiServeDurationRaw) != "" {
		parsedDuration, err := time.ParseDuration(*uiServeDurationRaw)
		if err != nil {
			fmt.Fprintf(stderr, "parse ui-serve-duration: %v\n", err)
			return 2
		}
		uiServeDuration = parsedDuration
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "load config: %v\n", err)
		return 1
	}

	if *printVersion {
		if err := printJSON(stdout, versionPayload()); err != nil {
			fmt.Fprintf(stderr, "encode version payload: %v\n", err)
			return 1
		}
		return 0
	}

	if *printConfig {
		if err := printJSON(stdout, cfg); err != nil {
			fmt.Fprintf(stderr, "encode config: %v\n", err)
			return 1
		}
		return 0
	}

	if *uiServe {
		return runUIServe(cfg, *uiListen, *uiRuntimeID, uiServeDuration, stdout, stderr)
	}

	if *installUserService {
		return runInstallUserService(
			cfg,
			installUserServiceOptions{
				SourceBinaryPath: *installSourceBinary,
				HomeDir:          *installHomeDir,
				GOOS:             *installGOOS,
			},
			stdout,
			stderr,
		)
	}

	if *uninstallUserService {
		return runUninstallUserService(
			cfg,
			uninstallUserServiceOptions{
				HomeDir: *uninstallHomeDir,
				GOOS:    *uninstallGOOS,
			},
			stdout,
			stderr,
		)
	}

	if *oauthSessionStatus {
		return runOAuthSessionStatus(context.Background(), cfg, stdout, stderr)
	}

	if *oauthClearLocalState {
		return runOAuthClearLocalState(context.Background(), cfg, stdout, stderr)
	}

	if *disconnectRuntime {
		return runDisconnectRuntime(context.Background(), cfg, stdout, stderr)
	}

	if *runtimeBindingStatus {
		return runRuntimeBindingStatus(cfg, stdout, stderr)
	}

	if *runtimeBindingClear {
		return runRuntimeBindingClear(cfg, stdout, stderr)
	}

	if *verifyOpenClawReceipt {
		return runVerifyOpenClawReceipt(context.Background(), cfg, stdout, stderr)
	}

	if *processWebhookOnce {
		return runProcessWebhookOnce(context.Background(), cfg, stdout, stderr)
	}

	if *runDaemon {
		return runWebhookDaemon(context.Background(), cfg, stdout, stderr)
	}

	if *oauthStart {
		return runOAuthStart(context.Background(), cfg, 0, *oauthRedirectPort, stdout, stderr)
	}

	if *oauthStartRuntimeID > 0 {
		return runOAuthStart(context.Background(), cfg, *oauthStartRuntimeID, *oauthRedirectPort, stdout, stderr)
	}

	if strings.TrimSpace(*oauthCompleteCallbackURL) != "" {
		return runOAuthComplete(context.Background(), cfg, *oauthCompleteCallbackURL, stdout, stderr)
	}

	fmt.Fprintf(stdout, "agent-flows-bridge config loaded\n")
	return 0
}

func runOAuthStart(
	ctx context.Context,
	cfg config.Config,
	runtimeID int,
	redirectPort int,
	stdout io.Writer,
	stderr io.Writer,
) int {
	oauthClient, err := newOAuthClientWithRedirectPort(cfg, redirectPort)
	if err != nil {
		fmt.Fprintf(stderr, "create oauth client: %v\n", err)
		return 1
	}

	resolvedRuntimeID := runtimeID
	startIntent := ""
	if resolvedRuntimeID == 0 {
		runtimeBinding, bindingErr := binding.Load(cfg.StateDir)
		if bindingErr == nil {
			resolvedRuntimeID = runtimeBinding.RuntimeID
			startIntent = "reconnect"
		} else if !errors.Is(bindingErr, binding.ErrNotFound) {
			fmt.Fprintf(stderr, "load runtime binding: %v\n", bindingErr)
			return 1
		}
	}

	start, err := oauthClient.StartLoginWithIntent(resolvedRuntimeID, startIntent)
	if err != nil {
		fmt.Fprintf(stderr, "start oauth login: %v\n", err)
		return 1
	}

	if err := oauthClient.SavePendingStart(ctx, cfg.StateDir, start); err != nil {
		fmt.Fprintf(stderr, "persist pending oauth start: %v\n", err)
		return 1
	}

	payload := map[string]any{
		"authorize_url": start.AuthorizeURL,
		"redirect_uri":  start.RedirectURI,
		"state":         start.State,
	}
	if start.RuntimeID > 0 {
		payload["runtime_id"] = start.RuntimeID
	} else {
		payload["runtime_id"] = nil
	}
	if strings.TrimSpace(start.Intent) != "" {
		payload["intent"] = start.Intent
	} else {
		payload["intent"] = nil
	}

	if err := printJSON(stdout, payload); err != nil {
		fmt.Fprintf(stderr, "encode oauth start payload: %v\n", err)
		return 1
	}

	_ = ctx
	return 0
}

func runOAuthComplete(
	ctx context.Context,
	cfg config.Config,
	callbackURL string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	oauthClient, _, err := newOAuthClientAndStore(cfg, 0)
	if err != nil {
		fmt.Fprintf(stderr, "create oauth client: %v\n", err)
		return 1
	}

	pendingStart, err := oauthClient.LoadPendingStart(ctx, cfg.StateDir)
	if err != nil {
		fmt.Fprintf(stderr, "load pending oauth start: %v\n", err)
		return 1
	}

	session, err := oauthClient.CompleteLoginFromCallbackURL(ctx, pendingStart, callbackURL)
	if err != nil {
		fmt.Fprintf(stderr, "complete oauth login: %v\n", err)
		return 1
	}

	bootstrapPayload, err := oauthClient.SyncBootstrap(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "sync bootstrap payload: %v\n", err)
		return 1
	}

	if err := oauthClient.ClearPendingStart(ctx, cfg.StateDir); err != nil {
		fmt.Fprintf(stderr, "clear pending oauth start: %v\n", err)
		return 1
	}

	bootstrapApplyResult, err := bootstrap.Apply(ctx, cfg.OpenClawDataDir, bootstrapPayload)
	if err != nil {
		fmt.Fprintf(stderr, "apply bootstrap payload locally: %v\n", err)
		return 1
	}

	runtimeBinding := binding.RuntimeBinding{
		RuntimeID:   session.RuntimeID,
		RuntimeKind: session.RuntimeKind,
		FlowID:      bootstrapPayload.Runtime.FlowID,
		ConnectorID: session.ConnectorID,
	}

	if err := binding.Save(cfg.StateDir, runtimeBinding); err != nil {
		fmt.Fprintf(stderr, "persist runtime binding: %v\n", err)
		return 1
	}

	payload := map[string]any{
		"connector_id":         session.ConnectorID,
		"runtime_id":           session.RuntimeID,
		"runtime_kind":         session.RuntimeKind,
		"scope":                session.Scope,
		"bootstrap_ready":      true,
		"bootstrap_runtime_id": bootstrapPayload.Runtime.ID,
		"bootstrap_fetched_at": bootstrapPayload.FetchedAt,
		"bootstrap_applied":    true,
		"openclaw_data_dir":    bootstrapApplyResult.OpenClawDataDir,
		"openclaw_config_path": bootstrapApplyResult.ConfigPath,
		"openclaw_env_path":    bootstrapApplyResult.EnvPath,
	}

	if err := printJSON(stdout, payload); err != nil {
		fmt.Fprintf(stderr, "encode oauth complete payload: %v\n", err)
		return 1
	}

	return 0
}

func runOAuthSessionStatus(ctx context.Context, cfg config.Config, stdout io.Writer, stderr io.Writer) int {
	oauthClient, secretStore, err := newOAuthClientAndStore(cfg, 0)
	if err != nil {
		fmt.Fprintf(stderr, "create oauth client: %v\n", err)
		return 1
	}

	storeMetadata := secrets.Describe(secretStore)

	session, err := oauthClient.LoadStoredSession(ctx)
	if err != nil {
		payload := map[string]any{
			"connected":         false,
			"error":             err.Error(),
			"secrets_backend":   storeMetadata.Backend,
			"bridge_version":    version,
			"bridge_commit":     commit,
			"bridge_build_date": buildDate,
		}
		if strings.TrimSpace(storeMetadata.Warning) != "" {
			payload["secrets_warning"] = storeMetadata.Warning
		}
		if jsonErr := printJSON(stdout, payload); jsonErr != nil {
			fmt.Fprintf(stderr, "encode oauth status payload: %v\n", jsonErr)
			return 1
		}
		return 0
	}

	payload := map[string]any{
		"connected":         true,
		"connector_id":      session.ConnectorID,
		"runtime_id":        session.RuntimeID,
		"runtime_kind":      session.RuntimeKind,
		"scope":             session.Scope,
		"secrets_backend":   storeMetadata.Backend,
		"bridge_version":    version,
		"bridge_commit":     commit,
		"bridge_build_date": buildDate,
	}
	if strings.TrimSpace(storeMetadata.Warning) != "" {
		payload["secrets_warning"] = storeMetadata.Warning
	}

	bootstrapPayload, bootstrapErr := oauthClient.LoadStoredBootstrap(ctx)
	if bootstrapErr == nil {
		payload["bootstrap_ready"] = true
		payload["bootstrap_runtime_id"] = bootstrapPayload.Runtime.ID
		payload["bootstrap_fetched_at"] = bootstrapPayload.FetchedAt
	} else {
		payload["bootstrap_ready"] = false

		if !errors.Is(bootstrapErr, secrets.ErrNotFound) {
			payload["bootstrap_error"] = bootstrapErr.Error()
		}
	}

	applyMarker, applyErr := bootstrap.LoadMarker(cfg.OpenClawDataDir)
	if applyErr == nil {
		payload["bootstrap_applied"] = true
		payload["openclaw_data_dir"] = applyMarker.OpenClawDataDir
		payload["openclaw_config_path"] = applyMarker.ConfigPath
		payload["openclaw_env_path"] = applyMarker.EnvPath
		payload["bootstrap_applied_at"] = applyMarker.AppliedAt
	} else {
		payload["bootstrap_applied"] = false

		if !errors.Is(applyErr, os.ErrNotExist) {
			payload["bootstrap_apply_error"] = applyErr.Error()
		}
	}

	if err := printJSON(stdout, payload); err != nil {
		fmt.Fprintf(stderr, "encode oauth status payload: %v\n", err)
		return 1
	}

	return 0
}

func runRuntimeBindingStatus(cfg config.Config, stdout io.Writer, stderr io.Writer) int {
	runtimeBinding, err := binding.Load(cfg.StateDir)
	if err != nil {
		if errors.Is(err, binding.ErrNotFound) {
			if jsonErr := printJSON(stdout, map[string]any{"bound": false}); jsonErr != nil {
				fmt.Fprintf(stderr, "encode runtime binding status payload: %v\n", jsonErr)
				return 1
			}
			return 0
		}

		fmt.Fprintf(stderr, "load runtime binding: %v\n", err)
		return 1
	}

	payload := map[string]any{
		"bound":        true,
		"runtime_id":   runtimeBinding.RuntimeID,
		"runtime_kind": runtimeBinding.RuntimeKind,
		"runtime_name": runtimeBinding.RuntimeName,
		"flow_id":      runtimeBinding.FlowID,
		"connector_id": runtimeBinding.ConnectorID,
		"updated_at":   runtimeBinding.UpdatedAt,
	}

	if err := printJSON(stdout, payload); err != nil {
		fmt.Fprintf(stderr, "encode runtime binding status payload: %v\n", err)
		return 1
	}

	return 0
}

func runRuntimeBindingClear(cfg config.Config, stdout io.Writer, stderr io.Writer) int {
	if err := binding.Clear(cfg.StateDir); err != nil {
		fmt.Fprintf(stderr, "clear runtime binding: %v\n", err)
		return 1
	}

	if err := printJSON(stdout, map[string]any{"cleared": true}); err != nil {
		fmt.Fprintf(stderr, "encode runtime binding clear payload: %v\n", err)
		return 1
	}

	return 0
}

func runOAuthClearLocalState(ctx context.Context, cfg config.Config, stdout io.Writer, stderr io.Writer) int {
	oauthClient, _, err := newOAuthClientAndStore(cfg, 0)
	if err != nil {
		fmt.Fprintf(stderr, "create oauth client: %v\n", err)
		return 1
	}

	if err := oauthClient.ClearLocalState(ctx); err != nil {
		fmt.Fprintf(stderr, "clear oauth local state: %v\n", err)
		return 1
	}

	if err := oauthClient.ClearPendingStart(ctx, cfg.StateDir); err != nil {
		fmt.Fprintf(stderr, "clear pending oauth start: %v\n", err)
		return 1
	}

	applyMarkerPath := filepath.Join(cfg.OpenClawDataDir, ".agent-flows-bridge-bootstrap.json")
	if err := os.Remove(applyMarkerPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(stderr, "clear bootstrap apply marker: %v\n", err)
		return 1
	}

	if err := printJSON(stdout, map[string]any{"cleared": true}); err != nil {
		fmt.Fprintf(stderr, "encode oauth clear payload: %v\n", err)
		return 1
	}

	return 0
}

func runDisconnectRuntime(ctx context.Context, cfg config.Config, stdout io.Writer, stderr io.Writer) int {
	oauthClient, err := newOAuthClient(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "create oauth client: %v\n", err)
		return 1
	}

	result, err := oauthClient.DisconnectConnector(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "disconnect runtime: %v\n", err)
		return 1
	}

	payload := map[string]any{
		"revoked":      result.Revoked,
		"connector_id": result.ConnectorID,
	}

	if err := printJSON(stdout, payload); err != nil {
		fmt.Fprintf(stderr, "encode disconnect payload: %v\n", err)
		return 1
	}

	return 0
}

func runVerifyOpenClawReceipt(ctx context.Context, cfg config.Config, stdout io.Writer, stderr io.Writer) int {
	oauthClient, err := newOAuthClient(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "create oauth client: %v\n", err)
		return 1
	}

	openClawConfigPath := filepath.Join(cfg.OpenClawDataDir, "openclaw.json")
	payload := map[string]any{
		"runtime_url":          cfg.RuntimeURL,
		"openclaw_config_path": openClawConfigPath,
	}

	session, sessionErr := oauthClient.LoadStoredSession(ctx)
	if sessionErr == nil {
		payload["connected"] = true
		payload["connector_id"] = session.ConnectorID
		payload["runtime_id"] = session.RuntimeID
		payload["runtime_kind"] = session.RuntimeKind
		payload["scope"] = session.Scope
	} else {
		payload["connected"] = false

		if !errors.Is(sessionErr, secrets.ErrNotFound) {
			payload["session_error"] = sessionErr.Error()
		}
	}

	bootstrapPayload, bootstrapErr := oauthClient.LoadStoredBootstrap(ctx)
	bootstrapHookToken := ""
	if bootstrapErr == nil {
		payload["bootstrap_ready"] = true
		payload["bootstrap_runtime_id"] = bootstrapPayload.Runtime.ID
		payload["bootstrap_fetched_at"] = bootstrapPayload.FetchedAt
		bootstrapHookToken = extractHookTokenFromBootstrapConfig(bootstrapPayload.Config)
	} else {
		payload["bootstrap_ready"] = false

		if !errors.Is(bootstrapErr, secrets.ErrNotFound) {
			payload["bootstrap_error"] = bootstrapErr.Error()
		}
	}

	applyMarker, applyErr := bootstrap.LoadMarker(cfg.OpenClawDataDir)
	if applyErr == nil {
		payload["bootstrap_applied"] = true
		payload["bootstrap_applied_at"] = applyMarker.AppliedAt
	} else {
		payload["bootstrap_applied"] = false

		if !errors.Is(applyErr, os.ErrNotExist) {
			payload["bootstrap_apply_error"] = applyErr.Error()
		}
	}

	verifier := receipt.NewVerifier(receipt.Options{})
	result, verifyErr := verifier.Verify(ctx, receipt.VerifyInput{
		RuntimeURL:         cfg.RuntimeURL,
		OpenClawConfigPath: openClawConfigPath,
	})
	if verifyErr != nil {
		payload["accepted"] = false
		payload["verify_error"] = verifyErr.Error()

		if err := printJSON(stdout, payload); err != nil {
			fmt.Fprintf(stderr, "encode verify payload: %v\n", err)
		}
		return 1
	}

	payload["accepted"] = result.Accepted
	payload["http_status"] = result.HTTPStatus
	payload["hook_url"] = result.HookURL
	payload["hook_path"] = result.HookPath
	payload["agent_id"] = result.AgentID
	payload["session_key"] = result.SessionKey
	payload["verified_at"] = result.VerifiedAt
	payload["duration_ms"] = result.DurationMilliseconds
	if strings.TrimSpace(result.RunID) != "" {
		payload["run_id"] = result.RunID
	}
	if strings.TrimSpace(result.ResponseBody) != "" {
		payload["response_body"] = result.ResponseBody
	}

	tokenMatchesBootstrap := true
	if strings.TrimSpace(bootstrapHookToken) != "" {
		tokenMatchesBootstrap = subtle.ConstantTimeCompare(
			[]byte(strings.TrimSpace(result.HookToken)),
			[]byte(strings.TrimSpace(bootstrapHookToken)),
		) == 1
		payload["token_matches_bootstrap"] = tokenMatchesBootstrap
	}

	if err := printJSON(stdout, payload); err != nil {
		fmt.Fprintf(stderr, "encode verify payload: %v\n", err)
		return 1
	}

	if result.Accepted && tokenMatchesBootstrap {
		return 0
	}

	return 1
}

func runProcessWebhookOnce(ctx context.Context, cfg config.Config, stdout io.Writer, stderr io.Writer) int {
	oauthClient, err := newOAuthClient(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "create oauth client: %v\n", err)
		return 1
	}

	claimedEvent, err := oauthClient.ClaimWebhookEvent(ctx)
	if err != nil {
		payload := map[string]any{
			"processed": false,
			"error":     err.Error(),
		}

		if jsonErr := printJSON(stdout, payload); jsonErr != nil {
			fmt.Fprintf(stderr, "encode process payload: %v\n", jsonErr)
		}

		return 1
	}

	if claimedEvent == nil {
		payload := map[string]any{
			"processed": false,
			"reason":    "no_event",
		}

		if err := printJSON(stdout, payload); err != nil {
			fmt.Fprintf(stderr, "encode process payload: %v\n", err)
			return 1
		}

		return 0
	}

	openClawConfigPath := filepath.Join(cfg.OpenClawDataDir, "openclaw.json")
	verifier := receipt.NewVerifier(receipt.Options{})
	deliveryResult, deliveryErr := verifier.Deliver(ctx, receipt.DeliverInput{
		RuntimeURL:         cfg.RuntimeURL,
		OpenClawConfigPath: openClawConfigPath,
		Payload:            claimedEvent.Payload,
	})

	outcome := "failed"
	runID := ""
	detail := ""
	if deliveryErr == nil {
		runID = deliveryResult.RunID

		if deliveryResult.Accepted {
			outcome = "delivered"
		} else {
			detail = deliveryResult.ResponseBody
		}
	} else {
		detail = deliveryErr.Error()
	}

	resultAck, reportErr :=
		oauthClient.ReportWebhookResult(ctx, claimedEvent.EventID, outcome, runID, detail)

	payload := map[string]any{
		"processed": true,
		"event_id":  claimedEvent.EventID,
		"outcome":   outcome,
	}

	if deliveryErr == nil {
		payload["accepted"] = deliveryResult.Accepted
		payload["http_status"] = deliveryResult.HTTPStatus
		payload["hook_url"] = deliveryResult.HookURL
		payload["duration_ms"] = deliveryResult.DurationMilliseconds
		if strings.TrimSpace(deliveryResult.RunID) != "" {
			payload["run_id"] = deliveryResult.RunID
		}
		if strings.TrimSpace(deliveryResult.ResponseBody) != "" {
			payload["response_body"] = deliveryResult.ResponseBody
		}
	} else {
		payload["accepted"] = false
		payload["delivery_error"] = deliveryErr.Error()
	}

	if reportErr == nil {
		payload["result_status"] = resultAck.Status
		payload["result_event_id"] = resultAck.EventID
		if strings.TrimSpace(resultAck.RunID) != "" {
			payload["result_run_id"] = resultAck.RunID
		}
	} else {
		payload["result_error"] = reportErr.Error()
	}

	if err := printJSON(stdout, payload); err != nil {
		fmt.Fprintf(stderr, "encode process payload: %v\n", err)
		return 1
	}

	if deliveryErr != nil || reportErr != nil || outcome != "delivered" {
		return 1
	}

	return 0
}

func runWebhookDaemon(ctx context.Context, cfg config.Config, stdout io.Writer, stderr io.Writer) int {
	oauthClient, err := newOAuthClient(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "create oauth client: %v\n", err)
		return 1
	}

	openClawConfigPath := filepath.Join(cfg.OpenClawDataDir, "openclaw.json")
	verifier := receipt.NewVerifier(receipt.Options{})

	worker, err := daemon.NewWebhookWorker(daemon.WebhookWorkerOptions{
		OAuthClient:        oauthClient,
		Deliverer:          verifier,
		RuntimeURL:         cfg.RuntimeURL,
		OpenClawConfigPath: openClawConfigPath,
		OnIteration: func(event daemon.IterationEvent) {
			switch event.Kind {
			case "claim_error":
				fmt.Fprintf(stderr, "daemon claim error: %v\n", event.ClaimError)
			case "report_error":
				fmt.Fprintf(
					stderr,
					"daemon report error event_id=%s outcome=%s error=%v\n",
					event.EventID,
					event.Outcome,
					event.ReportError,
				)
			case "delivered":
				fmt.Fprintf(stdout, "daemon delivered event_id=%s run_id=%s\n", event.EventID, event.RunID)
			case "delivery_failed":
				fmt.Fprintf(
					stderr,
					"daemon delivery failed event_id=%s detail=%s\n",
					event.EventID,
					event.Detail,
				)
			}
		},
	})
	if err != nil {
		fmt.Fprintf(stderr, "create webhook daemon: %v\n", err)
		return 1
	}

	daemonContext, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	runPolling := func() error {
		return runPollingWebhookDaemon(worker, daemonContext, stdout, stderr)
	}

	transportMode := strings.TrimSpace(cfg.TransportMode)
	if transportMode == "" {
		transportMode = "auto"
	}

	switch transportMode {
	case "poll":
		startSessionRefreshWorker(daemonContext, oauthClient, stderr, nil)
		if err := runPolling(); err != nil {
			fmt.Fprintf(stderr, "run webhook daemon: %v\n", err)
			return 1
		}

		fmt.Fprintf(stdout, "webhook daemon stopped\n")
		return 0
	case "wss":
		if err := runWSSWebhookDaemon(daemonContext, cfg, oauthClient, verifier, stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "run webhook daemon: %v\n", err)
			return 1
		}

		fmt.Fprintf(stdout, "webhook daemon stopped\n")
		return 0
	case "auto":
		if err := runWSSWebhookDaemon(daemonContext, cfg, oauthClient, verifier, stdout, stderr); err == nil {
			fmt.Fprintf(stdout, "webhook daemon stopped\n")
			return 0
		} else {
			fmt.Fprintf(stderr, "wss transport failed, falling back to poll: %v\n", err)
		}

		startSessionRefreshWorker(daemonContext, oauthClient, stderr, nil)
		if err := runPolling(); err != nil {
			fmt.Fprintf(stderr, "run webhook daemon: %v\n", err)
			return 1
		}

		fmt.Fprintf(stdout, "webhook daemon stopped\n")
		return 0
	default:
		fmt.Fprintf(stderr, "invalid transport mode %q\n", transportMode)
		return 2
	}
}

func runPollingWebhookDaemon(
	worker *daemon.WebhookWorker,
	daemonContext context.Context,
	stdout io.Writer,
	_ io.Writer,
) error {
	fmt.Fprintf(stdout, "webhook daemon running transport=poll\n")
	return worker.Run(daemonContext)
}

func runWSSWebhookDaemon(
	daemonContext context.Context,
	cfg config.Config,
	oauthClient *oauth.Client,
	deliverer daemon.Deliverer,
	stdout io.Writer,
	stderr io.Writer,
) error {
	session, err := oauthClient.LoadStoredSession(daemonContext)
	if err != nil {
		return fmt.Errorf("load oauth session for wss transport: %w", err)
	}

	accessToken := strings.TrimSpace(session.AccessToken)
	if accessToken == "" {
		return fmt.Errorf("stored session missing access token")
	}

	socketURL, err := wss.BuildWebhookSocketURL(cfg.APIBaseURL)
	if err != nil {
		return fmt.Errorf("build webhook socket url: %w", err)
	}

	dialer, err := wss.NewWebhookDialer(wss.WebhookDialerOptions{
		Deliverer:          deliverer,
		RuntimeURL:         cfg.RuntimeURL,
		OpenClawConfigPath: filepath.Join(cfg.OpenClawDataDir, "openclaw.json"),
		OnEvent: func(event wss.WebhookDeliveryEvent) {
			if event.Outcome == "delivered" {
				fmt.Fprintf(stdout, "daemon delivered event_id=%s run_id=%s\n", event.EventID, event.RunID)
			} else {
				fmt.Fprintf(stderr, "daemon delivery failed event_id=%s detail=%s\n", event.EventID, event.Detail)
			}
		},
	})
	if err != nil {
		return fmt.Errorf("create wss dialer: %w", err)
	}

	refreshingDialer := &refreshingWSSDialer{
		baseDialer:       dialer,
		sessionRefresher: oauthClient,
		accessToken:      accessToken,
	}

	startSessionRefreshWorker(daemonContext, oauthClient, stderr, func(refreshedSession oauth.Session) {
		refreshingDialer.setAccessToken(strings.TrimSpace(refreshedSession.AccessToken))
	})

	manager := wss.NewManager(wss.Options{
		URL:    socketURL,
		Token:  accessToken,
		Dialer: refreshingDialer,
		OnConnected: func() {
			fmt.Fprintf(stdout, "webhook daemon connected transport=wss\n")
		},
		OnDropped: func(err error) {
			fmt.Fprintf(stderr, "webhook wss connection dropped: %v\n", err)
		},
		OnDialError: func(err error) {
			fmt.Fprintf(stderr, "webhook wss dial error: %v\n", err)
		},
	})

	fmt.Fprintf(stdout, "webhook daemon running transport=wss\n")
	return manager.Run(daemonContext)
}

func startSessionRefreshWorker(
	daemonContext context.Context,
	refresher session.SessionRefresher,
	stderr io.Writer,
	onRefreshed func(session oauth.Session),
) {
	refreshWorker := session.NewRefreshWorker(session.RefreshWorkerOptions{
		Refresher: refresher,
		OnRefreshed: func(refreshedSession oauth.Session) {
			if onRefreshed != nil {
				onRefreshed(refreshedSession)
			}
		},
		OnFailure: func(err error) {
			fmt.Fprintf(stderr, "session refresh worker failure: %v\n", err)
		},
	})

	go func() {
		if err := refreshWorker.Run(daemonContext); err != nil && daemonContext.Err() == nil {
			fmt.Fprintf(stderr, "session refresh worker exited: %v\n", err)
		}
	}()
}

type connectorSessionRefresher interface {
	RefreshSession(ctx context.Context) (oauth.Session, error)
}

type refreshingWSSDialer struct {
	baseDialer       wss.Dialer
	sessionRefresher connectorSessionRefresher
	accessToken      string
	mu               sync.Mutex
}

func (d *refreshingWSSDialer) Dial(
	ctx context.Context,
	socketURL string,
	_ string,
) (wss.Connection, error) {
	// The manager token is intentionally ignored here.
	//
	// This dialer owns the mutable access token so background refresh callbacks
	// and 401-triggered refreshes both converge on a single token source.
	token := d.currentAccessToken()

	connection, err := d.baseDialer.Dial(ctx, socketURL, token)
	if err == nil {
		return connection, nil
	}

	if !isUnauthorizedWSSDialError(err) {
		return nil, err
	}

	if d.sessionRefresher == nil {
		return nil, fmt.Errorf("websocket dial unauthorized and session refresher is not configured")
	}

	refreshedSession, refreshErr := d.sessionRefresher.RefreshSession(ctx)
	if refreshErr != nil {
		return nil, fmt.Errorf("refresh connector session after unauthorized dial: %w", refreshErr)
	}

	refreshedToken := strings.TrimSpace(refreshedSession.AccessToken)
	if refreshedToken == "" {
		return nil, fmt.Errorf("refresh connector session returned empty access token")
	}

	d.setAccessToken(refreshedToken)

	connection, err = d.baseDialer.Dial(ctx, socketURL, refreshedToken)
	if err != nil {
		return nil, err
	}

	return connection, nil
}

func (d *refreshingWSSDialer) currentAccessToken() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.accessToken
}

func (d *refreshingWSSDialer) setAccessToken(token string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.accessToken = token
}

func isUnauthorizedWSSDialError(err error) bool {
	var dialErr *wss.HTTPDialError
	if errors.As(err, &dialErr) {
		return dialErr.StatusCode == http.StatusUnauthorized
	}

	return false
}

func runUIServe(
	cfg config.Config,
	listenAddr string,
	runtimeID int,
	serveDuration time.Duration,
	stdout io.Writer,
	stderr io.Writer,
) int {
	oauthClient, secretStore, err := newOAuthClientAndStore(cfg, 0)
	if err != nil {
		fmt.Fprintf(stderr, "create oauth client: %v\n", err)
		return 1
	}

	initialState := ui.State{Status: ui.StateNotConfigured, RuntimeID: runtimeID}
	if session, err := oauthClient.LoadStoredSession(context.Background()); err == nil {
		initialState.Status = ui.StateConnected
		initialState.RuntimeID = session.RuntimeID
		initialState.InfoMessage = "session restored"
	}

	shell := ui.NewShell(ui.Options{InitialState: initialState})
	exporter := diagnostics.NewExporter(diagnostics.ExporterOptions{
		OutputDir: filepath.Join(cfg.StateDir, "diagnostics"),
	})
	secretMetadata := secrets.Describe(secretStore)
	diagnosticsMetadata := map[string]any{
		"bridge_version":    version,
		"bridge_commit":     commit,
		"bridge_build_date": buildDate,
		"secrets_backend":   secretMetadata.Backend,
	}
	if strings.TrimSpace(secretMetadata.Warning) != "" {
		diagnosticsMetadata["secrets_warning"] = secretMetadata.Warning
	}

	controller := ui.NewController(ui.ControllerOptions{
		Shell:               shell,
		RuntimeID:           runtimeID,
		AuthStarter:         oauthClient,
		PendingState:        oauthClient,
		PendingStateDir:     cfg.StateDir,
		SessionLoader:       oauthClient,
		Exporter:            exporter,
		DiagnosticsMetadata: diagnosticsMetadata,
		OnAuthorizeURL: func(authorizeURL string) {
			_, _ = fmt.Fprintf(stdout, "authorize_url=%s\n", authorizeURL)
		},
	})

	shell.SetOnAction(controller.HandleAction)

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		fmt.Fprintf(stderr, "listen ui: %v\n", err)
		return 1
	}

	server := &http.Server{Handler: shell.Handler()}
	serverErrCh := make(chan error, 1)

	go func() {
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrCh <- err
			return
		}
		serverErrCh <- nil
	}()

	_, _ = fmt.Fprintf(stdout, "ui server listening on http://%s\n", listener.Addr().String())

	serveCtx, cancelServe := buildServeContext(serveDuration)
	defer cancelServe()

	select {
	case err := <-serverErrCh:
		if err != nil {
			fmt.Fprintf(stderr, "ui server error: %v\n", err)
			return 1
		}
		return 0
	case <-serveCtx.Done():
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	_ = server.Shutdown(shutdownCtx)

	if err := <-serverErrCh; err != nil {
		fmt.Fprintf(stderr, "ui server shutdown error: %v\n", err)
		return 1
	}

	return 0
}

type installUserServiceOptions struct {
	SourceBinaryPath string
	HomeDir          string
	GOOS             string
}

type uninstallUserServiceOptions struct {
	HomeDir string
	GOOS    string
}

func runInstallUserService(
	cfg config.Config,
	options installUserServiceOptions,
	stdout io.Writer,
	stderr io.Writer,
) int {
	sourceBinaryPath := strings.TrimSpace(options.SourceBinaryPath)
	if sourceBinaryPath == "" {
		executablePath, err := os.Executable()
		if err != nil {
			fmt.Fprintf(stderr, "resolve executable path: %v\n", err)
			return 1
		}
		sourceBinaryPath = executablePath
	}

	homeDir := strings.TrimSpace(options.HomeDir)
	if homeDir == "" {
		resolvedHomeDir, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(stderr, "resolve user home dir: %v\n", err)
			return 1
		}
		homeDir = resolvedHomeDir
	}

	plan, err := packaging.BuildInstallPlan(packaging.BuildInstallPlanOptions{
		GOOS:             strings.TrimSpace(options.GOOS),
		HomeDir:          homeDir,
		StateDir:         cfg.StateDir,
		SourceBinaryPath: sourceBinaryPath,
	})
	if err != nil {
		fmt.Fprintf(stderr, "build install plan: %v\n", err)
		return 1
	}

	result, err := packaging.Install(packaging.InstallOptions{
		Plan:   plan,
		Config: cfg,
	})
	if err != nil {
		fmt.Fprintf(stderr, "install user service: %v\n", err)
		return 1
	}

	if err := printJSON(stdout, result); err != nil {
		fmt.Fprintf(stderr, "encode install payload: %v\n", err)
		return 1
	}

	return 0
}

func runUninstallUserService(
	cfg config.Config,
	options uninstallUserServiceOptions,
	stdout io.Writer,
	stderr io.Writer,
) int {
	homeDir := strings.TrimSpace(options.HomeDir)
	if homeDir == "" {
		resolvedHomeDir, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(stderr, "resolve user home dir: %v\n", err)
			return 1
		}
		homeDir = resolvedHomeDir
	}

	plan, err := packaging.BuildInstallPlan(packaging.BuildInstallPlanOptions{
		GOOS:             strings.TrimSpace(options.GOOS),
		HomeDir:          homeDir,
		StateDir:         cfg.StateDir,
		SourceBinaryPath: filepath.Join(cfg.StateDir, "bin", "agent-flows-bridge"),
		BinaryName:       "agent-flows-bridge",
	})
	if err != nil {
		fmt.Fprintf(stderr, "build uninstall plan: %v\n", err)
		return 1
	}

	result, err := packaging.Uninstall(packaging.UninstallOptions{Plan: plan})
	if err != nil {
		fmt.Fprintf(stderr, "uninstall user service: %v\n", err)
		return 1
	}

	if err := printJSON(stdout, result); err != nil {
		fmt.Fprintf(stderr, "encode uninstall payload: %v\n", err)
		return 1
	}

	return 0
}

func buildServeContext(serveDuration time.Duration) (context.Context, context.CancelFunc) {
	if serveDuration > 0 {
		return context.WithTimeout(context.Background(), serveDuration)
	}

	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func newSecretStore(cfg config.Config) (secrets.Store, error) {
	return secrets.NewStore(secrets.Options{StateDir: cfg.StateDir})
}

func newOAuthClientWithRedirectPort(cfg config.Config, redirectPort int) (*oauth.Client, error) {
	client, _, err := newOAuthClientAndStore(cfg, redirectPort)
	return client, err
}

func newOAuthClient(cfg config.Config) (*oauth.Client, error) {
	return newOAuthClientWithRedirectPort(cfg, 0)
}

func newOAuthClientAndStore(cfg config.Config, redirectPort int) (*oauth.Client, secrets.Store, error) {
	secretStore, err := newSecretStore(cfg)
	if err != nil {
		return nil, nil, err
	}

	deviceName, err := os.Hostname()
	if err != nil || strings.TrimSpace(deviceName) == "" {
		deviceName = "Agent Flows Bridge"
	}

	resolvedRedirectPort := redirectPort
	if resolvedRedirectPort <= 0 {
		resolvedRedirectPort, err = selectOAuthRedirectPort()
		if err != nil {
			return nil, nil, err
		}
	}

	client := oauth.NewClient(oauth.Options{
		APIBaseURL:    cfg.APIBaseURL,
		OAuthClientID: cfg.OAuthClientID,
		DeviceName:    deviceName,
		Platform:      runtime.GOOS,
		RedirectPort:  resolvedRedirectPort,
		SecretStore:   secretStore,
	})

	return client, secretStore, nil
}

func printJSON(writer io.Writer, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(writer, "%s\n", string(encoded)); err != nil {
		return err
	}

	return nil
}

func versionPayload() map[string]any {
	return map[string]any{
		"version":    version,
		"commit":     commit,
		"build_date": buildDate,
		"goos":       runtime.GOOS,
		"goarch":     runtime.GOARCH,
	}
}

func selectOAuthRedirectPort() (int, error) {
	for port := 49200; port <= 49210; port++ {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}

		_ = listener.Close()
		return port, nil
	}

	return 0, fmt.Errorf("no available oauth redirect ports in range 49200-49210")
}

func modeCount(modes ...bool) int {
	total := 0
	for _, mode := range modes {
		if mode {
			total++
		}
	}
	return total
}

func extractHookTokenFromBootstrapConfig(config map[string]any) string {
	hooksObject, ok := config["hooks"]
	if !ok {
		return ""
	}

	hooks, ok := hooksObject.(map[string]any)
	if !ok {
		return ""
	}

	hookToken, _ := hooks["token"].(string)
	return strings.TrimSpace(hookToken)
}
