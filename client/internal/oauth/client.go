package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/agentflows/agent-flows-bridge/client/internal/secrets"
)

const (
	sessionSecretKey    = "oauth_session"
	bootstrapSecretKey  = "connector_bootstrap_payload"
	oauthScope          = "connector:bootstrap connector:heartbeat connector:webhook"
	maxAPIResponseBytes = 1 << 20
)

// Options define OAuth client behavior and defaults.
type Options struct {
	APIBaseURL    string
	OAuthClientID string
	DeviceName    string
	Platform      string
	RedirectPort  int
	HTTPClient    *http.Client
	SecretStore   secrets.Store
}

// Client execute OAuth PKCE login flows for the bridge runtime.
type Client struct {
	apiBaseURL    string
	oauthClientID string
	deviceName    string
	platform      string
	redirectPort  int
	httpClient    *http.Client
	secretStore   secrets.Store
}

// StartLoginResult contains PKCE/session data for login completion.
type StartLoginResult struct {
	AuthorizeURL  string
	State         string
	CodeVerifier  string
	RedirectURI   string
	RuntimeID     int
	Intent        string
	OAuthClientID string
}

// Session stores connector OAuth tokens and identity metadata.
type Session struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	Scope        string    `json:"scope"`
	ConnectorID  int       `json:"connector_id"`
	RuntimeID    int       `json:"runtime_id"`
	RuntimeKind  string    `json:"runtime_kind"`
	IssuedAt     time.Time `json:"issued_at"`
}

// ExpiresAt return the absolute access token expiry time.
//
// Uses IssuedAt plus ExpiresIn seconds when both fields are populated.
//
// Returns a zero time when expiry metadata is incomplete.
func (s Session) ExpiresAt() time.Time {
	if s.IssuedAt.IsZero() || s.ExpiresIn <= 0 {
		return time.Time{}
	}

	return s.IssuedAt.UTC().Add(time.Duration(s.ExpiresIn) * time.Second)
}

// ShouldRefresh report whether the access token should be refreshed now.
//
// RefreshAhead defaults to five minutes when zero or negative. Invalid expiry
// metadata is treated as needing refresh.
//
// Returns true when the token is at or within the refresh window.
func (s Session) ShouldRefresh(now time.Time, refreshAhead time.Duration) bool {
	expiresAt := s.ExpiresAt()
	if expiresAt.IsZero() {
		return true
	}

	if refreshAhead <= 0 {
		refreshAhead = 5 * time.Minute
	}

	return !now.UTC().Before(expiresAt.Add(-refreshAhead))
}

// BootstrapRuntime identifies the runtime bound to bootstrap payload data.
type BootstrapRuntime struct {
	ID          int    `json:"id"`
	RuntimeKind string `json:"runtime_kind"`
	FlowID      int    `json:"flow_id"`
}

// BootstrapPayload stores runtime bootstrap configuration and credentials.
type BootstrapPayload struct {
	Runtime        BootstrapRuntime             `json:"runtime"`
	Env            map[string]string            `json:"env"`
	Config         map[string]any               `json:"config"`
	WorkspaceFiles map[string]map[string]string `json:"workspace_files"`
	FetchedAt      time.Time                    `json:"fetched_at"`
}

// WebhookEvent represents one claimed delivery event for the local runtime.
type WebhookEvent struct {
	EventID   string         `json:"event_id"`
	RuntimeID int            `json:"runtime_id"`
	TaskID    int            `json:"task_id"`
	AgentID   string         `json:"agent_id"`
	EventType string         `json:"event_type"`
	Payload   map[string]any `json:"payload"`
	Attempts  int            `json:"attempts"`
}

// WebhookResultAck captures server acknowledgement after event result submission.
type WebhookResultAck struct {
	EventID string `json:"event_id"`
	Status  string `json:"status"`
	RunID   string `json:"run_id"`
}

// DisconnectResult captures connector revocation acknowledgement payload.
type DisconnectResult struct {
	Revoked     bool `json:"revoked"`
	ConnectorID int  `json:"connector_id"`
}

// APIError capture structured connector API error responses.
//
// Includes HTTP status plus optional parsed error code and raw response body.
//
// Returns a formatted error string via Error().
type APIError struct {
	StatusCode int
	Code       string
	Body       string
}

func (e *APIError) Error() string {
	parts := []string{fmt.Sprintf("status=%d", e.StatusCode)}
	if trimmedCode := strings.TrimSpace(e.Code); trimmedCode != "" {
		parts = append(parts, "code="+trimmedCode)
	}
	if trimmedBody := strings.TrimSpace(e.Body); trimmedBody != "" {
		parts = append(parts, "body="+trimmedBody)
	}

	return strings.Join(parts, " ")
}

// NewClient construct a PKCE OAuth client with validated options.
//
// Missing options are defaulted where possible. SecretStore is required.
//
// Returns a ready-to-use client.
func NewClient(opts Options) *Client {
	apiBaseURL := strings.TrimSpace(opts.APIBaseURL)
	oauthClientID := strings.TrimSpace(opts.OAuthClientID)
	deviceName := strings.TrimSpace(opts.DeviceName)
	platform := strings.TrimSpace(opts.Platform)
	redirectPort := opts.RedirectPort
	httpClient := opts.HTTPClient

	if oauthClientID == "" {
		oauthClientID = "agent-flows-bridge"
	}
	if deviceName == "" {
		deviceName = "Agent Flows Bridge"
	}
	if platform == "" {
		platform = runtime.GOOS
	}
	if redirectPort <= 0 {
		redirectPort = 49200
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}

	return &Client{
		apiBaseURL:    strings.TrimRight(apiBaseURL, "/"),
		oauthClientID: oauthClientID,
		deviceName:    deviceName,
		platform:      platform,
		redirectPort:  redirectPort,
		httpClient:    httpClient,
		secretStore:   opts.SecretStore,
	}
}

// StartLogin create authorize URL and PKCE data for a bridge login.
//
// Generates verifier/challenge and state values and returns a fully formed
// OAuth authorize URL for browser launch. Runtime selection can be deferred
// to browser consent by passing runtimeID as 0.
//
// Returns StartLoginResult or an error.
func (c *Client) StartLogin(runtimeID int) (StartLoginResult, error) {
	return c.StartLoginWithIntent(runtimeID, "")
}

// StartLoginWithIntent create authorize URL and PKCE data with optional login intent.
//
// Includes `intent` and `runtime_id` query values when runtimeID is provided.
// Pass `intent=reconnect` to force reconnect-mode consent behavior on the server.
//
// Returns StartLoginResult or an error.
func (c *Client) StartLoginWithIntent(runtimeID int, intent string) (StartLoginResult, error) {
	if runtimeID < 0 {
		return StartLoginResult{}, fmt.Errorf("runtime_id must be zero or positive")
	}
	if strings.TrimSpace(c.apiBaseURL) == "" {
		return StartLoginResult{}, fmt.Errorf("api base url is required")
	}

	trimmedIntent := strings.TrimSpace(intent)
	if runtimeID == 0 {
		trimmedIntent = ""
	}

	verifier, err := randomToken(48)
	if err != nil {
		return StartLoginResult{}, fmt.Errorf("generate code verifier: %w", err)
	}

	state, err := randomToken(24)
	if err != nil {
		return StartLoginResult{}, fmt.Errorf("generate state: %w", err)
	}

	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/oauth/callback", c.redirectPort)
	baseURL, err := url.Parse(c.apiBaseURL)
	if err != nil {
		return StartLoginResult{}, fmt.Errorf("parse api base url: %w", err)
	}

	baseURL.Path = path.Join(baseURL.Path, "/oauth/bridge/sign-in")

	query := url.Values{}
	query.Set("response_type", "code")
	query.Set("client_id", c.oauthClientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("scope", oauthScope)
	query.Set("state", state)
	query.Set("code_challenge", pkceChallenge(verifier))
	query.Set("code_challenge_method", "S256")
	if runtimeID > 0 {
		query.Set("runtime_id", strconv.Itoa(runtimeID))
		if trimmedIntent != "" {
			query.Set("intent", trimmedIntent)
		}
	}
	query.Set("device_name", c.deviceName)
	query.Set("platform", c.platform)

	baseURL.RawQuery = query.Encode()

	result := StartLoginResult{
		AuthorizeURL:  baseURL.String(),
		State:         state,
		CodeVerifier:  verifier,
		RedirectURI:   redirectURI,
		RuntimeID:     runtimeID,
		Intent:        trimmedIntent,
		OAuthClientID: c.oauthClientID,
	}

	return result, nil
}

// CompleteLoginFromCallbackURL validate callback state, exchange code, and persist session.
//
// Parses callback URL query params, validates state, performs token exchange,
// and writes resulting session into secret storage.
//
// Returns a Session or an error.
func (c *Client) CompleteLoginFromCallbackURL(
	ctx context.Context,
	start StartLoginResult,
	callbackURL string,
) (Session, error) {
	callbackCode, callbackState, err := parseCallback(callbackURL)
	if err != nil {
		return Session{}, err
	}

	if callbackState != start.State {
		return Session{}, fmt.Errorf("callback state mismatch")
	}

	session, err := c.exchangeAuthorizationCode(ctx, start, callbackCode)
	if err != nil {
		return Session{}, err
	}

	if err := c.persistSession(ctx, session); err != nil {
		return Session{}, err
	}

	return session, nil
}

// LoadStoredSession read the stored OAuth connector session.
//
// Session values are loaded from secure secret storage and decoded from JSON.
//
// Returns a Session or an error.
func (c *Client) LoadStoredSession(ctx context.Context) (Session, error) {
	if c.secretStore == nil {
		return Session{}, fmt.Errorf("secret store is not configured")
	}

	raw, err := c.secretStore.Load(ctx, sessionSecretKey)
	if err != nil {
		return Session{}, err
	}

	var session Session
	if err := json.Unmarshal(raw, &session); err != nil {
		return Session{}, fmt.Errorf("decode stored oauth session: %w", err)
	}

	return session, nil
}

// LoadFreshSession load a stored session and refresh it when expiry is near.
//
// RefreshAhead defaults to five minutes when zero or negative. Refreshed
// sessions are persisted by RefreshSession before being returned.
//
// Returns a usable Session or an error.
func (c *Client) LoadFreshSession(ctx context.Context, refreshAhead time.Duration) (Session, error) {
	session, err := c.LoadStoredSession(ctx)
	if err != nil {
		return Session{}, err
	}

	if !session.ShouldRefresh(time.Now().UTC(), refreshAhead) {
		return session, nil
	}

	return c.RefreshSession(ctx)
}

// ClearLocalState delete stored OAuth session and bootstrap payload secrets.
//
// Missing secrets are treated as success so callers can safely invoke this
// during app shutdown cleanup.
//
// Returns nil on success or an error.
func (c *Client) ClearLocalState(ctx context.Context) error {
	if c.secretStore == nil {
		return fmt.Errorf("secret store is not configured")
	}

	if err := c.secretStore.Delete(ctx, sessionSecretKey); err != nil && !errors.Is(err, secrets.ErrNotFound) {
		return fmt.Errorf("delete oauth session secret: %w", err)
	}

	if err := c.secretStore.Delete(ctx, bootstrapSecretKey); err != nil && !errors.Is(err, secrets.ErrNotFound) {
		return fmt.Errorf("delete bootstrap payload secret: %w", err)
	}

	return nil
}

// SavePendingStart persist pending PKCE state using the configured secret store.
//
// Uses stateDir for legacy file migration compatibility and validation only.
//
// Returns nil on success or an error.
func (c *Client) SavePendingStart(ctx context.Context, stateDir string, start StartLoginResult) error {
	return SavePendingStart(ctx, stateDir, c.secretStore, start)
}

// LoadPendingStart read pending PKCE state from secret storage or migrate it.
//
// Uses stateDir to check and delete the legacy plaintext file path when needed.
//
// Returns StartLoginResult or an error.
func (c *Client) LoadPendingStart(ctx context.Context, stateDir string) (StartLoginResult, error) {
	return LoadPendingStart(ctx, stateDir, c.secretStore)
}

// ClearPendingStart delete pending PKCE state from all known storage paths.
//
// Uses stateDir to remove the legacy plaintext file path during cleanup.
//
// Returns nil on success or an error.
func (c *Client) ClearPendingStart(ctx context.Context, stateDir string) error {
	return ClearPendingStart(ctx, stateDir, c.secretStore)
}

// RefreshSession rotate connector tokens using the stored refresh token.
//
// Reads the current stored session, calls the refresh-token grant endpoint,
// preserves runtime identity fields, and persists the rotated session.
//
// Returns the updated Session or an error.
func (c *Client) RefreshSession(ctx context.Context) (Session, error) {
	currentSession, err := c.LoadStoredSession(ctx)
	if err != nil {
		return Session{}, err
	}

	refreshedSession, err := c.exchangeRefreshToken(ctx, currentSession.RefreshToken)
	if err != nil {
		return Session{}, err
	}

	// Refresh responses do not include runtime details; keep prior identity.
	refreshedSession.RuntimeID = currentSession.RuntimeID
	refreshedSession.RuntimeKind = currentSession.RuntimeKind

	if refreshedSession.ConnectorID <= 0 {
		refreshedSession.ConnectorID = currentSession.ConnectorID
	}
	if strings.TrimSpace(refreshedSession.Scope) == "" {
		refreshedSession.Scope = currentSession.Scope
	}

	if err := validateSession(refreshedSession); err != nil {
		return Session{}, err
	}

	if err := c.persistSession(ctx, refreshedSession); err != nil {
		return Session{}, err
	}

	return refreshedSession, nil
}

// SyncBootstrap fetch runtime-scoped bootstrap payload and persist it securely.
//
// Reads the stored OAuth session, calls connector bootstrap endpoint with the
// session access token, validates runtime identity, and stores the payload.
//
// Returns BootstrapPayload or an error.
func (c *Client) SyncBootstrap(ctx context.Context) (BootstrapPayload, error) {
	session, err := c.LoadFreshSession(ctx, 5*time.Minute)
	if err != nil {
		return BootstrapPayload{}, err
	}

	if strings.TrimSpace(session.AccessToken) == "" {
		return BootstrapPayload{}, fmt.Errorf("stored session missing access token")
	}

	bootstrapPayload, err := c.fetchBootstrapPayload(ctx, session.AccessToken)
	if err != nil {
		return BootstrapPayload{}, err
	}

	if session.RuntimeID > 0 && bootstrapPayload.Runtime.ID != session.RuntimeID {
		return BootstrapPayload{}, fmt.Errorf(
			"bootstrap runtime mismatch: session=%d payload=%d",
			session.RuntimeID,
			bootstrapPayload.Runtime.ID,
		)
	}

	bootstrapPayload.FetchedAt = time.Now().UTC().Truncate(time.Second)

	if err := c.persistBootstrap(ctx, bootstrapPayload); err != nil {
		return BootstrapPayload{}, err
	}

	return bootstrapPayload, nil
}

// LoadStoredBootstrap read the most recent persisted bootstrap payload.
//
// Payload values are loaded from secure secret storage and decoded from JSON.
//
// Returns BootstrapPayload or an error.
func (c *Client) LoadStoredBootstrap(ctx context.Context) (BootstrapPayload, error) {
	if c.secretStore == nil {
		return BootstrapPayload{}, fmt.Errorf("secret store is not configured")
	}

	raw, err := c.secretStore.Load(ctx, bootstrapSecretKey)
	if err != nil {
		return BootstrapPayload{}, err
	}

	var bootstrapPayload BootstrapPayload
	if err := json.Unmarshal(raw, &bootstrapPayload); err != nil {
		return BootstrapPayload{}, fmt.Errorf("decode stored bootstrap payload: %w", err)
	}

	if err := validateBootstrapPayload(bootstrapPayload); err != nil {
		return BootstrapPayload{}, err
	}

	return bootstrapPayload, nil
}

// ClaimWebhookEvent claim the next queued webhook delivery event for this connector.
//
// Uses the stored OAuth access token to call the connector claim endpoint.
//
// Returns `(*WebhookEvent, nil)` when an event is claimed, `(nil, nil)` when no
// event is available, or an error.
func (c *Client) ClaimWebhookEvent(ctx context.Context) (*WebhookEvent, error) {
	session, err := c.LoadFreshSession(ctx, 5*time.Minute)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(session.AccessToken) == "" {
		return nil, fmt.Errorf("stored session missing access token")
	}

	claimURL := c.apiBaseURL + "/api/connectors/webhooks/claim"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, claimURL, strings.NewReader("{}"))
	if err != nil {
		return nil, fmt.Errorf("build webhook claim request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+session.AccessToken)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("execute webhook claim request: %w", err)
	}
	defer response.Body.Close()

	body, err := readBodyLimited(response.Body, maxAPIResponseBytes)
	if err != nil {
		return nil, fmt.Errorf("read webhook claim response: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("webhook claim failed: %w", newAPIError(response.StatusCode, body))
	}

	var envelope webhookClaimResponseEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode webhook claim response: %w", err)
	}

	if envelope.Data.Event == nil {
		return nil, nil
	}

	event := envelope.Data.Event
	if strings.TrimSpace(event.EventID) == "" {
		return nil, fmt.Errorf("webhook claim response missing event_id")
	}
	if event.Payload == nil {
		return nil, fmt.Errorf("webhook claim response missing payload")
	}

	return event, nil
}

// ReportWebhookResult submit webhook delivery outcome for a claimed event.
//
// Uses the stored OAuth access token and sends an outcome payload to the
// connector result endpoint.
//
// Returns WebhookResultAck or an error.
func (c *Client) ReportWebhookResult(
	ctx context.Context,
	eventID string,
	outcome string,
	runID string,
	detail string,
) (WebhookResultAck, error) {
	session, err := c.LoadFreshSession(ctx, 5*time.Minute)
	if err != nil {
		return WebhookResultAck{}, err
	}

	trimmedEventID := strings.TrimSpace(eventID)
	if trimmedEventID == "" {
		return WebhookResultAck{}, fmt.Errorf("event id is required")
	}

	trimmedOutcome := strings.TrimSpace(outcome)
	if trimmedOutcome == "" {
		return WebhookResultAck{}, fmt.Errorf("outcome is required")
	}

	resultPayload := map[string]any{
		"event_id": trimmedEventID,
		"outcome":  trimmedOutcome,
	}

	trimmedRunID := strings.TrimSpace(runID)
	if trimmedRunID != "" {
		resultPayload["run_id"] = trimmedRunID
	}

	trimmedDetail := strings.TrimSpace(detail)
	if trimmedDetail != "" {
		resultPayload["detail"] = trimmedDetail
	}

	encodedPayload, err := json.Marshal(resultPayload)
	if err != nil {
		return WebhookResultAck{}, fmt.Errorf("encode webhook result payload: %w", err)
	}

	resultURL := c.apiBaseURL + "/api/connectors/webhooks/result"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, resultURL, strings.NewReader(string(encodedPayload)))
	if err != nil {
		return WebhookResultAck{}, fmt.Errorf("build webhook result request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+session.AccessToken)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return WebhookResultAck{}, fmt.Errorf("execute webhook result request: %w", err)
	}
	defer response.Body.Close()

	body, err := readBodyLimited(response.Body, maxAPIResponseBytes)
	if err != nil {
		return WebhookResultAck{}, fmt.Errorf("read webhook result response: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		return WebhookResultAck{}, fmt.Errorf("webhook result failed: %w", newAPIError(response.StatusCode, body))
	}

	var envelope webhookResultResponseEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return WebhookResultAck{}, fmt.Errorf("decode webhook result response: %w", err)
	}

	ack := envelope.Data
	if strings.TrimSpace(ack.EventID) == "" {
		return WebhookResultAck{}, fmt.Errorf("webhook result response missing event_id")
	}
	if strings.TrimSpace(ack.Status) == "" {
		return WebhookResultAck{}, fmt.Errorf("webhook result response missing status")
	}

	return ack, nil
}

// DisconnectConnector revoke active connector access for the stored session.
//
// Uses the stored OAuth access token to call the connector disconnect endpoint,
// which revokes the connector and all active connector sessions server-side.
//
// Returns DisconnectResult or an error.
func (c *Client) DisconnectConnector(ctx context.Context) (DisconnectResult, error) {
	session, err := c.LoadFreshSession(ctx, 5*time.Minute)
	if err != nil {
		return DisconnectResult{}, err
	}

	if strings.TrimSpace(session.AccessToken) == "" {
		return DisconnectResult{}, fmt.Errorf("stored session missing access token")
	}

	disconnectURL := c.apiBaseURL + "/api/connectors/disconnect"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, disconnectURL, strings.NewReader("{}"))
	if err != nil {
		return DisconnectResult{}, fmt.Errorf("build disconnect request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+session.AccessToken)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return DisconnectResult{}, fmt.Errorf("execute disconnect request: %w", err)
	}
	defer response.Body.Close()

	body, err := readBodyLimited(response.Body, maxAPIResponseBytes)
	if err != nil {
		return DisconnectResult{}, fmt.Errorf("read disconnect response: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		return DisconnectResult{}, fmt.Errorf("disconnect request failed: %w", newAPIError(response.StatusCode, body))
	}

	var envelope disconnectResponseEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return DisconnectResult{}, fmt.Errorf("decode disconnect response: %w", err)
	}

	result := envelope.Data
	if !result.Revoked {
		return DisconnectResult{}, fmt.Errorf("disconnect response missing revoked=true")
	}
	if result.ConnectorID <= 0 {
		return DisconnectResult{}, fmt.Errorf("disconnect response missing connector_id")
	}

	return result, nil
}

func (c *Client) exchangeAuthorizationCode(
	ctx context.Context,
	start StartLoginResult,
	code string,
) (Session, error) {
	if strings.TrimSpace(c.apiBaseURL) == "" {
		return Session{}, fmt.Errorf("api base url is required")
	}

	tokenURL := c.apiBaseURL + "/oauth/token"
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", start.RedirectURI)
	form.Set("client_id", start.OAuthClientID)
	form.Set("code_verifier", start.CodeVerifier)

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Session{}, fmt.Errorf("build token request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return Session{}, fmt.Errorf("execute token request: %w", err)
	}
	defer response.Body.Close()

	body, err := readBodyLimited(response.Body, maxAPIResponseBytes)
	if err != nil {
		return Session{}, fmt.Errorf("read token response: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		return Session{}, fmt.Errorf("token exchange failed: %w", newAPIError(response.StatusCode, body))
	}

	var payload tokenResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return Session{}, fmt.Errorf("decode token response: %w", err)
	}

	session := Session{
		AccessToken:  payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		TokenType:    payload.TokenType,
		ExpiresIn:    payload.ExpiresIn,
		Scope:        payload.Scope,
		ConnectorID:  payload.ConnectorID,
		RuntimeID:    payload.Runtime.ID,
		RuntimeKind:  payload.Runtime.RuntimeKind,
		IssuedAt:     time.Now().UTC().Truncate(time.Second),
	}

	if err := validateSession(session); err != nil {
		return Session{}, err
	}

	return session, nil
}

func (c *Client) persistSession(ctx context.Context, session Session) error {
	if c.secretStore == nil {
		return fmt.Errorf("secret store is not configured")
	}

	encoded, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("encode oauth session: %w", err)
	}

	if err := c.secretStore.Save(ctx, sessionSecretKey, encoded); err != nil {
		return fmt.Errorf("save oauth session: %w", err)
	}

	return nil
}

func (c *Client) exchangeRefreshToken(ctx context.Context, refreshToken string) (Session, error) {
	if strings.TrimSpace(c.apiBaseURL) == "" {
		return Session{}, fmt.Errorf("api base url is required")
	}

	tokenURL := c.apiBaseURL + "/oauth/token"
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", c.oauthClientID)

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Session{}, fmt.Errorf("build refresh request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return Session{}, fmt.Errorf("execute refresh request: %w", err)
	}
	defer response.Body.Close()

	body, err := readBodyLimited(response.Body, maxAPIResponseBytes)
	if err != nil {
		return Session{}, fmt.Errorf("read refresh response: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		return Session{}, fmt.Errorf("refresh exchange failed: %w", newAPIError(response.StatusCode, body))
	}

	var payload tokenResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return Session{}, fmt.Errorf("decode refresh response: %w", err)
	}

	session := Session{
		AccessToken:  payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		TokenType:    payload.TokenType,
		ExpiresIn:    payload.ExpiresIn,
		Scope:        payload.Scope,
		ConnectorID:  payload.ConnectorID,
		IssuedAt:     time.Now().UTC().Truncate(time.Second),
	}

	if strings.TrimSpace(session.AccessToken) == "" {
		return Session{}, fmt.Errorf("refresh response missing access_token")
	}
	if strings.TrimSpace(session.RefreshToken) == "" {
		return Session{}, fmt.Errorf("refresh response missing refresh_token")
	}
	if strings.TrimSpace(session.TokenType) == "" {
		return Session{}, fmt.Errorf("refresh response missing token_type")
	}
	if session.ExpiresIn <= 0 {
		return Session{}, fmt.Errorf("refresh response missing expires_in")
	}

	return session, nil
}

func (c *Client) fetchBootstrapPayload(ctx context.Context, accessToken string) (BootstrapPayload, error) {
	if strings.TrimSpace(c.apiBaseURL) == "" {
		return BootstrapPayload{}, fmt.Errorf("api base url is required")
	}

	bootstrapURL := c.apiBaseURL + "/api/connectors/bootstrap"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, bootstrapURL, strings.NewReader("{}"))
	if err != nil {
		return BootstrapPayload{}, fmt.Errorf("build bootstrap request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+accessToken)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return BootstrapPayload{}, fmt.Errorf("execute bootstrap request: %w", err)
	}
	defer response.Body.Close()

	body, err := readBodyLimited(response.Body, maxAPIResponseBytes)
	if err != nil {
		return BootstrapPayload{}, fmt.Errorf("read bootstrap response: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		return BootstrapPayload{}, fmt.Errorf("bootstrap fetch failed: %w", newAPIError(response.StatusCode, body))
	}

	var envelope bootstrapResponseEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return BootstrapPayload{}, fmt.Errorf("decode bootstrap response: %w", err)
	}

	bootstrapPayload := BootstrapPayload{
		Runtime:        envelope.Data.Runtime,
		Env:            envelope.Data.Env,
		Config:         envelope.Data.Config,
		WorkspaceFiles: envelope.Data.WorkspaceFiles,
	}

	if err := validateBootstrapPayload(bootstrapPayload); err != nil {
		return BootstrapPayload{}, err
	}

	return bootstrapPayload, nil
}

func (c *Client) persistBootstrap(ctx context.Context, bootstrapPayload BootstrapPayload) error {
	if c.secretStore == nil {
		return fmt.Errorf("secret store is not configured")
	}

	encoded, err := json.Marshal(bootstrapPayload)
	if err != nil {
		return fmt.Errorf("encode bootstrap payload: %w", err)
	}

	if err := c.secretStore.Save(ctx, bootstrapSecretKey, encoded); err != nil {
		return fmt.Errorf("save bootstrap payload: %w", err)
	}

	return nil
}

func parseCallback(callbackURL string) (string, string, error) {
	parsedURL, err := url.Parse(callbackURL)
	if err != nil {
		return "", "", fmt.Errorf("parse callback url: %w", err)
	}

	query := parsedURL.Query()
	if errCode := strings.TrimSpace(query.Get("error")); errCode != "" {
		return "", "", fmt.Errorf("oauth callback error: %s", errCode)
	}

	code := strings.TrimSpace(query.Get("code"))
	state := strings.TrimSpace(query.Get("state"))
	if code == "" || state == "" {
		return "", "", fmt.Errorf("callback must include code and state")
	}

	return code, state, nil
}

func validateSession(session Session) error {
	if strings.TrimSpace(session.AccessToken) == "" {
		return fmt.Errorf("token response missing access_token")
	}
	if strings.TrimSpace(session.RefreshToken) == "" {
		return fmt.Errorf("token response missing refresh_token")
	}
	if strings.TrimSpace(session.TokenType) == "" {
		return fmt.Errorf("token response missing token_type")
	}
	if session.ExpiresIn <= 0 {
		return fmt.Errorf("token response missing expires_in")
	}
	if session.ConnectorID <= 0 {
		return fmt.Errorf("token response missing connector_id")
	}
	if session.RuntimeID <= 0 {
		return fmt.Errorf("token response missing runtime.id")
	}
	if strings.TrimSpace(session.RuntimeKind) == "" {
		return fmt.Errorf("token response missing runtime.runtime_kind")
	}
	if session.IssuedAt.IsZero() {
		return fmt.Errorf("token response missing issued_at")
	}

	return nil
}

func validateBootstrapPayload(bootstrapPayload BootstrapPayload) error {
	if bootstrapPayload.Runtime.ID <= 0 {
		return fmt.Errorf("bootstrap payload missing runtime.id")
	}
	if strings.TrimSpace(bootstrapPayload.Runtime.RuntimeKind) == "" {
		return fmt.Errorf("bootstrap payload missing runtime.runtime_kind")
	}
	if strings.TrimSpace(bootstrapPayload.Env["AGENT_FLOWS_API_URL"]) == "" {
		return fmt.Errorf("bootstrap payload missing env.AGENT_FLOWS_API_URL")
	}
	if strings.TrimSpace(bootstrapPayload.Env["AGENT_FLOWS_API_KEY"]) == "" {
		return fmt.Errorf("bootstrap payload missing env.AGENT_FLOWS_API_KEY")
	}
	if bootstrapPayload.Config == nil {
		return fmt.Errorf("bootstrap payload missing config")
	}
	if bootstrapPayload.WorkspaceFiles == nil {
		return fmt.Errorf("bootstrap payload missing workspace_files")
	}

	return nil
}

func readBodyLimited(body io.Reader, maxBytes int64) ([]byte, error) {
	limited, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if err != nil {
		return nil, err
	}

	if int64(len(limited)) > maxBytes {
		return nil, fmt.Errorf("response body exceeds %d bytes", maxBytes)
	}

	return limited, nil
}

func newAPIError(statusCode int, body []byte) *APIError {
	trimmedBody := strings.TrimSpace(string(body))
	apiError := &APIError{StatusCode: statusCode, Body: trimmedBody}

	if trimmedBody == "" {
		return apiError
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return apiError
	}

	if rawCode, ok := payload["code"].(string); ok {
		apiError.Code = strings.TrimSpace(rawCode)
	}
	if apiError.Code == "" {
		if rawError, ok := payload["error"].(string); ok {
			apiError.Code = strings.TrimSpace(rawError)
		}
	}

	return apiError
}

func randomToken(byteLength int) (string, error) {
	buffer := make([]byte, byteLength)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	ConnectorID  int    `json:"connector_id"`
	Runtime      struct {
		ID          int    `json:"id"`
		RuntimeKind string `json:"runtime_kind"`
	} `json:"runtime"`
}

type bootstrapResponseEnvelope struct {
	Data bootstrapResponseData `json:"data"`
}

type bootstrapResponseData struct {
	Runtime        BootstrapRuntime             `json:"runtime"`
	Env            map[string]string            `json:"env"`
	Config         map[string]any               `json:"config"`
	WorkspaceFiles map[string]map[string]string `json:"workspace_files"`
}

type webhookClaimResponseEnvelope struct {
	Data webhookClaimResponseData `json:"data"`
}

type webhookClaimResponseData struct {
	Event *WebhookEvent `json:"event"`
}

type webhookResultResponseEnvelope struct {
	Data WebhookResultAck `json:"data"`
}

type disconnectResponseEnvelope struct {
	Data DisconnectResult `json:"data"`
}
