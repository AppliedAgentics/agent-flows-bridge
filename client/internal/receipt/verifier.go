package receipt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Options configure hook probe verification behavior.
type Options struct {
	HTTPClient     *http.Client
	RequestTimeout time.Duration
}

// Verifier probes OpenClaw hook endpoints with a signed payload.
type Verifier struct {
	httpClient     *http.Client
	requestTimeout time.Duration
}

// VerifyInput defines runtime and OpenClaw config paths for a probe.
type VerifyInput struct {
	RuntimeURL         string
	OpenClawConfigPath string
	AgentID            string
}

// DeliverInput defines a concrete hook payload delivery to local OpenClaw.
type DeliverInput struct {
	RuntimeURL         string
	OpenClawConfigPath string
	Payload            map[string]any
}

// Result captures probe request/response evidence.
type Result struct {
	VerifiedAt           time.Time `json:"verified_at"`
	HookURL              string    `json:"hook_url"`
	HookPath             string    `json:"hook_path"`
	AgentID              string    `json:"agent_id"`
	SessionKey           string    `json:"session_key"`
	HTTPStatus           int       `json:"http_status"`
	Accepted             bool      `json:"accepted"`
	RunID                string    `json:"run_id,omitempty"`
	ResponseBody         string    `json:"response_body,omitempty"`
	DurationMilliseconds int64     `json:"duration_ms"`
	HookToken            string    `json:"-"`
}

// NewVerifier creates a hook receipt verifier with safe defaults.
func NewVerifier(opts Options) *Verifier {
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	requestTimeout := opts.RequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = 8 * time.Second
	}

	return &Verifier{
		httpClient:     httpClient,
		requestTimeout: requestTimeout,
	}
}

// Verify sends a signed hook probe to OpenClaw and returns evidence.
func (v *Verifier) Verify(ctx context.Context, input VerifyInput) (Result, error) {
	runtimeURL := strings.TrimSpace(input.RuntimeURL)
	openClawConfigPath := strings.TrimSpace(input.OpenClawConfigPath)
	_, _, allowedAgentIDs, err := loadHooksConfig(openClawConfigPath)
	if err != nil {
		return Result{}, err
	}

	agentID := resolveProbeAgentID(strings.TrimSpace(input.AgentID), allowedAgentIDs)
	sessionKey := "hook:probe:" + strconv.FormatInt(time.Now().UTC().UnixMilli(), 10)

	payload := map[string]any{
		"message":                    "[AgentFlows:receipt_probe] task_id=0",
		"name":                       "AgentFlows",
		"agentId":                    agentID,
		"sessionKey":                 sessionKey,
		"wakeMode":                   "now",
		"deliver":                    false,
		"timeoutSeconds":             10,
		"allowUnsafeExternalContent": true,
	}

	return v.Deliver(ctx, DeliverInput{
		RuntimeURL:         runtimeURL,
		OpenClawConfigPath: openClawConfigPath,
		Payload:            payload,
	})
}

// Deliver sends a concrete webhook payload to local OpenClaw and captures evidence.
func (v *Verifier) Deliver(ctx context.Context, input DeliverInput) (Result, error) {
	runtimeURL := strings.TrimSpace(input.RuntimeURL)
	if runtimeURL == "" {
		return Result{}, fmt.Errorf("runtime url is required")
	}

	openClawConfigPath := strings.TrimSpace(input.OpenClawConfigPath)
	if openClawConfigPath == "" {
		return Result{}, fmt.Errorf("openclaw config path is required")
	}

	payload := input.Payload
	if payload == nil {
		return Result{}, fmt.Errorf("payload is required")
	}

	hookToken, hookPath, _, err := loadHooksConfig(openClawConfigPath)
	if err != nil {
		return Result{}, err
	}

	hookURL, err := buildHookURL(runtimeURL, hookPath)
	if err != nil {
		return Result{}, err
	}

	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		return Result{}, fmt.Errorf("encode hook payload: %w", err)
	}

	probeCtx, cancel := context.WithTimeout(ctx, v.requestTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(probeCtx, http.MethodPost, hookURL, bytes.NewReader(encodedPayload))
	if err != nil {
		return Result{}, fmt.Errorf("build probe request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+hookToken)
	request.Header.Set("Content-Type", "application/json")

	startedAt := time.Now()
	response, err := v.httpClient.Do(request)
	if err != nil {
		return Result{}, fmt.Errorf("send probe request: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 8192))
	if err != nil {
		return Result{}, fmt.Errorf("read probe response body: %w", err)
	}

	verifiedAt := time.Now().UTC().Truncate(time.Second)
	durationMilliseconds := time.Since(startedAt).Milliseconds()

	result := Result{
		VerifiedAt:           verifiedAt,
		HookURL:              hookURL,
		HookPath:             hookPath,
		AgentID:              stringFromPayload(payload, "agentId"),
		SessionKey:           stringFromPayload(payload, "sessionKey"),
		HTTPStatus:           response.StatusCode,
		Accepted:             response.StatusCode == http.StatusOK || response.StatusCode == http.StatusAccepted,
		ResponseBody:         strings.TrimSpace(string(responseBody)),
		DurationMilliseconds: durationMilliseconds,
		HookToken:            hookToken,
	}

	if runID := decodeRunID(responseBody); runID != "" {
		result.RunID = runID
	}

	return result, nil
}

func loadHooksConfig(openClawConfigPath string) (string, string, []string, error) {
	raw, err := os.ReadFile(openClawConfigPath)
	if err != nil {
		return "", "", nil, fmt.Errorf("read openclaw config: %w", err)
	}

	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return "", "", nil, fmt.Errorf("parse openclaw config: %w", err)
	}

	hooks, ok := config["hooks"].(map[string]any)
	if !ok {
		return "", "", nil, fmt.Errorf("openclaw config missing hooks object")
	}

	hookToken, _ := hooks["token"].(string)
	hookToken = strings.TrimSpace(hookToken)
	if hookToken == "" {
		return "", "", nil, fmt.Errorf("openclaw config missing hooks.token")
	}

	hookPath, _ := hooks["path"].(string)
	hookPath = strings.TrimSpace(hookPath)
	if hookPath == "" {
		hookPath = "/hooks"
	}

	allowedAgentIDs := []string{}
	if rawAllowed, ok := hooks["allowedAgentIds"].([]any); ok {
		for _, value := range rawAllowed {
			if agentID, ok := value.(string); ok {
				agentID = strings.TrimSpace(agentID)
				if agentID != "" {
					allowedAgentIDs = append(allowedAgentIDs, agentID)
				}
			}
		}
	}

	return hookToken, hookPath, allowedAgentIDs, nil
}

func resolveProbeAgentID(preferredAgentID string, allowedAgentIDs []string) string {
	if preferredAgentID != "" {
		return preferredAgentID
	}

	if len(allowedAgentIDs) > 0 {
		return allowedAgentIDs[0]
	}

	return "lead"
}

func buildHookURL(runtimeURL string, hookPath string) (string, error) {
	parsedRuntimeURL, err := url.Parse(runtimeURL)
	if err != nil {
		return "", fmt.Errorf("parse runtime url: %w", err)
	}
	if parsedRuntimeURL.Scheme == "" || parsedRuntimeURL.Host == "" {
		return "", fmt.Errorf("runtime url must include scheme and host")
	}

	trimmedHookPath := strings.TrimSpace(hookPath)
	if trimmedHookPath == "" {
		trimmedHookPath = "/hooks"
	}
	if !strings.HasPrefix(trimmedHookPath, "/") {
		trimmedHookPath = "/" + trimmedHookPath
	}
	trimmedHookPath = strings.TrimSuffix(trimmedHookPath, "/")
	fullHookPath := trimmedHookPath + "/agent"

	hookURL := parsedRuntimeURL.ResolveReference(&url.URL{Path: fullHookPath})
	return hookURL.String(), nil
}

func decodeRunID(responseBody []byte) string {
	var payload map[string]any
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return ""
	}

	runID, _ := payload["runId"].(string)
	return strings.TrimSpace(runID)
}

func stringFromPayload(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}
