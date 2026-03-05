package ui

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"sync"
	"time"
)

// StateStatus identifies current connector UI state.
type StateStatus string

const (
	StateNotConfigured StateStatus = "not_configured"
	StateAuthorizing   StateStatus = "authorizing"
	StateConnected     StateStatus = "connected"
	StateDegraded      StateStatus = "degraded"
	StateDisconnected  StateStatus = "disconnected"
)

// Action identifies a user-triggered UI action.
type Action string

const (
	ActionAuthorize         Action = "authorize"
	ActionReconnect         Action = "reconnect"
	ActionExportDiagnostics Action = "export_diagnostics"
)

// State contains UI-safe shell status data.
type State struct {
	Status        StateStatus `json:"status"`
	RuntimeID     int         `json:"runtime_id"`
	ErrorCode     string      `json:"error_code,omitempty"`
	InfoMessage   string      `json:"info_message,omitempty"`
	UpdatedAt     time.Time   `json:"updated_at"`
	LastHeartbeat *time.Time  `json:"last_heartbeat,omitempty"`
}

// Options configure shell behavior.
type Options struct {
	OnAction     func(action Action)
	InitialState State
}

// Shell hosts minimal desktop status UI and action endpoints.
type Shell struct {
	mu       sync.RWMutex
	state    State
	onAction func(action Action)
	tmpl     *template.Template
}

// NewShell construct minimal shell state and handlers.
//
// Initial state defaults to `not_configured` when not provided.
//
// Returns a new Shell.
func NewShell(opts Options) *Shell {
	state := opts.InitialState
	if state.Status == "" {
		state.Status = StateNotConfigured
	}
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	}

	shell := &Shell{
		state:    state,
		onAction: opts.OnAction,
		tmpl:     template.Must(template.New("shell").Parse(indexTemplate)),
	}

	return shell
}

// Update replace current state with a new UI-safe snapshot.
//
// UpdatedAt is auto-filled with current UTC time when omitted.
//
// Returns nothing.
func (s *Shell) Update(state State) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if state.Status == "" {
		state.Status = StateDisconnected
	}
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	}

	s.state = state
}

// State return current shell state snapshot.
//
// Returns a copy safe for external use.
//
// Returns State.
func (s *Shell) State() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// SetOnAction replace the shell action callback handler.
//
// Use this to attach orchestration logic after shell construction.
//
// Returns nothing.
func (s *Shell) SetOnAction(onAction func(action Action)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onAction = onAction
}

// Handler expose the UI and action API HTTP handlers.
//
// Routes:
// - GET /                 => shell page
// - GET /api/state        => state JSON
// - POST /api/action/*    => action trigger
//
// Returns an http.Handler.
func (s *Shell) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleRoot)
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("GET /api/actions", s.handleActions)
	mux.HandleFunc("POST /api/action/authorize", s.handleAuthorize)
	mux.HandleFunc("POST /api/action/reconnect", s.handleReconnect)
	mux.HandleFunc("POST /api/action/export-diagnostics", s.handleExportDiagnostics)
	mux.HandleFunc("POST /api/action/", s.handleActionNotFound)
	return mux
}

func (s *Shell) handleRoot(writer http.ResponseWriter, _ *http.Request) {
	state := s.State()

	view := map[string]any{
		"Status":        state.Status,
		"RuntimeID":     state.RuntimeID,
		"ErrorCode":     state.ErrorCode,
		"InfoMessage":   state.InfoMessage,
		"UpdatedAt":     state.UpdatedAt.Format(time.RFC3339),
		"LastHeartbeat": formatTimePtr(state.LastHeartbeat),
	}

	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.Execute(writer, view); err != nil {
		http.Error(writer, "template render failed", http.StatusInternalServerError)
	}
}

func (s *Shell) handleState(writer http.ResponseWriter, _ *http.Request) {
	state := s.State()
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(state)
}

func (s *Shell) handleActions(writer http.ResponseWriter, _ *http.Request) {
	payload := map[string][]string{
		"actions": {
			string(ActionAuthorize),
			string(ActionReconnect),
			string(ActionExportDiagnostics),
		},
	}
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(payload)
}

func (s *Shell) handleAuthorize(writer http.ResponseWriter, _ *http.Request) {
	s.fireAction(ActionAuthorize)
	writer.WriteHeader(http.StatusAccepted)
	_, _ = writer.Write([]byte(`{"ok":true}`))
}

func (s *Shell) handleReconnect(writer http.ResponseWriter, _ *http.Request) {
	s.fireAction(ActionReconnect)
	writer.WriteHeader(http.StatusAccepted)
	_, _ = writer.Write([]byte(`{"ok":true}`))
}

func (s *Shell) handleExportDiagnostics(writer http.ResponseWriter, _ *http.Request) {
	s.fireAction(ActionExportDiagnostics)
	writer.WriteHeader(http.StatusAccepted)
	_, _ = writer.Write([]byte(`{"ok":true}`))
}

func (s *Shell) handleActionNotFound(writer http.ResponseWriter, request *http.Request) {
	http.NotFound(writer, request)
}

func (s *Shell) fireAction(action Action) {
	if s.onAction != nil {
		s.onAction(action)
	}
}

func formatTimePtr(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

const indexTemplate = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Agent Flows Bridge</title>
    <style>
      :root {
        --bg-1: #0b132b;
        --bg-2: #1c2541;
        --panel: rgba(255,255,255,0.9);
        --ink: #14213d;
        --accent: #fca311;
        --good: #2a9d8f;
        --warn: #e76f51;
      }
      * { box-sizing: border-box; }
      body {
        margin: 0;
        min-height: 100vh;
        font-family: "Avenir Next", "Segoe UI", "Helvetica Neue", sans-serif;
        background: radial-gradient(circle at top left, var(--bg-2), var(--bg-1));
        color: var(--ink);
        display: grid;
        place-items: center;
      }
      .card {
        width: min(560px, 92vw);
        border-radius: 18px;
        padding: 1.5rem;
        background: var(--panel);
        box-shadow: 0 30px 80px rgba(0,0,0,0.28);
        animation: rise 240ms ease-out;
      }
      @keyframes rise {
        from { transform: translateY(10px); opacity: 0; }
        to { transform: translateY(0); opacity: 1; }
      }
      .title {
        margin: 0;
        font-size: 1.35rem;
        letter-spacing: 0.03em;
      }
      .status {
        margin-top: 0.6rem;
        display: inline-flex;
        align-items: center;
        gap: 0.45rem;
        font-size: 0.95rem;
        font-weight: 700;
        text-transform: lowercase;
      }
      .dot {
        width: 10px;
        height: 10px;
        border-radius: 999px;
        background: var(--warn);
      }
      .status.connected .dot { background: var(--good); }
      .status.authorizing .dot,
      .status.degraded .dot { background: var(--accent); }
      .meta {
        margin-top: 1rem;
        padding: 0.8rem;
        background: rgba(20,33,61,0.06);
        border-radius: 10px;
        font-size: 0.9rem;
        line-height: 1.5;
      }
      .meta code {
        font-family: "SF Mono", Menlo, monospace;
      }
      .actions {
        margin-top: 1rem;
        display: grid;
        grid-template-columns: repeat(3, 1fr);
        gap: 0.6rem;
      }
      button {
        border: 0;
        border-radius: 10px;
        padding: 0.65rem 0.5rem;
        font-weight: 700;
        cursor: pointer;
        background: #14213d;
        color: #fff;
        transition: transform 120ms ease, opacity 120ms ease;
      }
      button:hover { transform: translateY(-1px); opacity: 0.95; }
      @media (max-width: 640px) {
        .actions { grid-template-columns: 1fr; }
      }
    </style>
  </head>
  <body>
    <section class="card" aria-label="agent-flows-bridge-shell">
      <h1 class="title">Agent Flows Bridge</h1>
      <div class="status {{.Status}}"><span class="dot"></span> {{.Status}}</div>
      <div class="meta">
        <div>runtime_id: <code>{{.RuntimeID}}</code></div>
        <div>error_code: <code>{{.ErrorCode}}</code></div>
        <div>info: <code>{{.InfoMessage}}</code></div>
        <div>updated_at: <code>{{.UpdatedAt}}</code></div>
        <div>last_heartbeat: <code>{{.LastHeartbeat}}</code></div>
      </div>
      <form class="actions" method="post">
        <button formaction="/api/action/authorize">Authorize</button>
        <button formaction="/api/action/reconnect">Reconnect</button>
        <button formaction="/api/action/export-diagnostics">Export Diagnostics</button>
      </form>
    </section>
  </body>
</html>
`

func (status StateStatus) String() string {
	return strings.TrimSpace(string(status))
}

func (state State) String() string {
	return fmt.Sprintf("status=%s runtime_id=%d", state.Status, state.RuntimeID)
}
