package ui

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentflows/agent-flows-bridge/client/internal/diagnostics"
	"github.com/agentflows/agent-flows-bridge/client/internal/oauth"
)

// AuthStarter starts OAuth login and returns authorize URL metadata.
type AuthStarter interface {
	StartLogin(runtimeID int) (oauth.StartLoginResult, error)
}

// PendingStateManager persists pending OAuth PKCE state across callback hops.
type PendingStateManager interface {
	SavePendingStart(ctx context.Context, stateDir string, start oauth.StartLoginResult) error
}

// SessionLoader loads persisted OAuth session data.
type SessionLoader interface {
	LoadStoredSession(ctx context.Context) (oauth.Session, error)
}

// Exporter exports diagnostics bundles.
type Exporter interface {
	Export(input diagnostics.BundleInput) (string, error)
}

// ControllerOptions configure UI action wiring behavior.
type ControllerOptions struct {
	Shell               *Shell
	RuntimeID           int
	AuthStarter         AuthStarter
	PendingState        PendingStateManager
	PendingStateDir     string
	SessionLoader       SessionLoader
	Exporter            Exporter
	DiagnosticsMetadata map[string]any
	OnAuthorizeURL      func(authorizeURL string)
}

// Controller maps shell actions to runtime/auth/diagnostics services.
type Controller struct {
	shell               *Shell
	runtimeID           int
	authStarter         AuthStarter
	pendingState        PendingStateManager
	pendingStateDir     string
	sessionLoader       SessionLoader
	exporter            Exporter
	diagnosticsMetadata map[string]any
	onAuthorizeURL      func(authorizeURL string)
}

// NewController build UI action controller.
//
// Shell is required. Other dependencies can be nil when corresponding actions
// are intentionally disabled.
//
// Returns a configured controller.
func NewController(opts ControllerOptions) *Controller {
	return &Controller{
		shell:               opts.Shell,
		runtimeID:           opts.RuntimeID,
		authStarter:         opts.AuthStarter,
		pendingState:        opts.PendingState,
		pendingStateDir:     opts.PendingStateDir,
		sessionLoader:       opts.SessionLoader,
		exporter:            opts.Exporter,
		diagnosticsMetadata: opts.DiagnosticsMetadata,
		onAuthorizeURL:      opts.OnAuthorizeURL,
	}
}

// HandleAction execute side effects for a shell button action.
//
// Updates shell state according to service outcomes and never writes token
// values into the UI state.
//
// Returns nothing.
func (c *Controller) HandleAction(action Action) {
	switch action {
	case ActionAuthorize:
		c.handleAuthorize()
	case ActionReconnect:
		c.handleReconnect()
	case ActionExportDiagnostics:
		c.handleExportDiagnostics()
	}
}

func (c *Controller) handleAuthorize() {
	if c.authStarter == nil {
		c.fail(StateDegraded, "AUTHORIZE_UNAVAILABLE", "authorize action is not configured")
		return
	}

	if c.runtimeID <= 0 {
		c.fail(StateDegraded, "RUNTIME_ID_REQUIRED", "set runtime id before authorize")
		return
	}

	start, err := c.authStarter.StartLogin(c.runtimeID)
	if err != nil {
		c.fail(StateDegraded, "AUTHORIZE_FAILED", "failed to start authorize flow")
		return
	}

	if c.pendingStateDir != "" && c.pendingState != nil {
		if err := c.pendingState.SavePendingStart(context.Background(), c.pendingStateDir, start); err != nil {
			c.fail(StateDegraded, "PENDING_STATE_SAVE_FAILED", "failed to persist pending login state")
			return
		}
	}

	state := c.shell.State()
	state.Status = StateAuthorizing
	state.RuntimeID = c.runtimeID
	state.ErrorCode = ""
	state.InfoMessage = "authorization started in browser"
	state.UpdatedAt = nowUTC()
	c.shell.Update(state)

	if c.onAuthorizeURL != nil {
		c.onAuthorizeURL(start.AuthorizeURL)
	}
}

func (c *Controller) handleReconnect() {
	if c.sessionLoader == nil {
		c.fail(StateDisconnected, "RECONNECT_UNAVAILABLE", "reconnect action is not configured")
		return
	}

	session, err := c.sessionLoader.LoadStoredSession(context.Background())
	if err != nil {
		errorCode := "RECONNECT_FAILED"
		if errors.Is(err, oauth.ErrPendingStartNotFound) {
			errorCode = "NO_SESSION"
		}
		c.fail(StateDisconnected, errorCode, "unable to reconnect from stored session")
		return
	}

	state := c.shell.State()
	state.Status = StateConnected
	state.RuntimeID = session.RuntimeID
	state.ErrorCode = ""
	state.InfoMessage = "session restored"
	state.UpdatedAt = nowUTC()
	c.shell.Update(state)
}

func (c *Controller) handleExportDiagnostics() {
	if c.exporter == nil {
		c.fail(StateDegraded, "EXPORT_UNAVAILABLE", "diagnostics exporter is not configured")
		return
	}

	currentState := c.shell.State()
	bundleInput := diagnostics.BundleInput{
		Metadata: map[string]any{
			"status":     currentState.Status,
			"runtime_id": currentState.RuntimeID,
		},
	}
	for key, value := range c.diagnosticsMetadata {
		bundleInput.Metadata[key] = value
	}

	bundlePath, err := c.exporter.Export(bundleInput)
	if err != nil {
		c.fail(StateDegraded, "EXPORT_FAILED", "failed to export diagnostics")
		return
	}

	state := c.shell.State()
	state.ErrorCode = ""
	state.InfoMessage = fmt.Sprintf("diagnostics exported to %s", bundlePath)
	state.UpdatedAt = nowUTC()
	c.shell.Update(state)
}

func (c *Controller) fail(status StateStatus, errorCode string, infoMessage string) {
	state := c.shell.State()
	state.Status = status
	state.ErrorCode = errorCode
	state.InfoMessage = infoMessage
	state.UpdatedAt = nowUTC()
	c.shell.Update(state)
}

func nowUTC() time.Time {
	return time.Now().UTC().Truncate(time.Second)
}
