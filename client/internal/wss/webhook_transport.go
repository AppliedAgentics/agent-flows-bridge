package wss

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/agentflows/agent-flows-bridge/client/internal/receipt"
	"nhooyr.io/websocket"
)

// WebhookDeliverer defines local webhook delivery behavior for pushed events.
type WebhookDeliverer interface {
	Deliver(ctx context.Context, input receipt.DeliverInput) (receipt.Result, error)
}

// WebhookDeliveryEvent captures one processed webhook push event.
type WebhookDeliveryEvent struct {
	EventID string
	Outcome string
	RunID   string
	Detail  string
}

// WebhookDialerOptions configure websocket connector dial behavior.
type WebhookDialerOptions struct {
	Deliverer          WebhookDeliverer
	RuntimeURL         string
	OpenClawConfigPath string
	DialContext        func(ctx context.Context, rawURL string, header http.Header) (websocketConn, error)
	OnEvent            func(event WebhookDeliveryEvent)
}

// WebhookDialer opens connector webhook websocket sessions.
type WebhookDialer struct {
	deliverer          WebhookDeliverer
	runtimeURL         string
	openClawConfigPath string
	dialContext        func(ctx context.Context, rawURL string, header http.Header) (websocketConn, error)
	onEvent            func(event WebhookDeliveryEvent)
}

type websocketConn interface {
	Read(ctx context.Context) (websocket.MessageType, []byte, error)
	Write(ctx context.Context, messageType websocket.MessageType, payload []byte) error
	Close(statusCode websocket.StatusCode, reason string) error
}

type websocketConnection struct {
	conn *websocket.Conn
}

type webhookConnectionOptions struct {
	websocketConn      websocketConn
	deliverer          WebhookDeliverer
	runtimeURL         string
	openClawConfigPath string
	onEvent            func(event WebhookDeliveryEvent)
}

type webhookConnection struct {
	websocketConn      websocketConn
	deliverer          WebhookDeliverer
	runtimeURL         string
	openClawConfigPath string
	onEvent            func(event WebhookDeliveryEvent)
}

// HTTPDialError captures non-upgrade HTTP responses from websocket dial attempts.
type HTTPDialError struct {
	StatusCode int
	Body       string
}

func (e *HTTPDialError) Error() string {
	trimmedBody := strings.TrimSpace(e.Body)
	if trimmedBody == "" {
		return fmt.Sprintf("websocket dial failed with status %d", e.StatusCode)
	}

	return fmt.Sprintf("websocket dial failed with status %d body=%s", e.StatusCode, trimmedBody)
}

type websocketEnvelope struct {
	Type string `json:"type"`
}

type websocketWebhookEvent struct {
	Type      string         `json:"type"`
	EventID   string         `json:"event_id"`
	Payload   map[string]any `json:"payload"`
	RuntimeID int            `json:"runtime_id"`
	TaskID    int            `json:"task_id"`
	AgentID   string         `json:"agent_id"`
	EventType string         `json:"event_type"`
}

// BuildWebhookSocketURL convert API base URL to connector websocket endpoint URL.
//
// Maps https->wss and http->ws and appends `/api/connectors/webhooks/socket`.
//
// Returns websocket URL or an error.
func BuildWebhookSocketURL(apiBaseURL string) (string, error) {
	trimmed := strings.TrimSpace(apiBaseURL)
	if trimmed == "" {
		return "", fmt.Errorf("api base url is required")
	}

	parsedURL, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse api base url: %w", err)
	}

	switch parsedURL.Scheme {
	case "https":
		parsedURL.Scheme = "wss"
	case "http":
		parsedURL.Scheme = "ws"
	default:
		return "", fmt.Errorf("unsupported api base url scheme %q", parsedURL.Scheme)
	}

	parsedURL.Path = path.Join(parsedURL.Path, "/api/connectors/webhooks/socket")
	parsedURL.RawQuery = ""
	parsedURL.Fragment = ""

	return parsedURL.String(), nil
}

// NewWebhookDialer create a dialer for connector webhook websocket sessions.
//
// Requires deliverer, runtime URL, and OpenClaw config path to process pushed events.
//
// Returns a configured dialer or an error.
func NewWebhookDialer(opts WebhookDialerOptions) (*WebhookDialer, error) {
	runtimeURL := strings.TrimSpace(opts.RuntimeURL)
	openClawConfigPath := strings.TrimSpace(opts.OpenClawConfigPath)

	if opts.Deliverer == nil {
		return nil, fmt.Errorf("deliverer is required")
	}
	if runtimeURL == "" {
		return nil, fmt.Errorf("runtime url is required")
	}
	if openClawConfigPath == "" {
		return nil, fmt.Errorf("openclaw config path is required")
	}

	dialContext := opts.DialContext
	if dialContext == nil {
		dialContext = defaultDialContext
	}

	return &WebhookDialer{
		deliverer:          opts.Deliverer,
		runtimeURL:         runtimeURL,
		openClawConfigPath: openClawConfigPath,
		dialContext:        dialContext,
		onEvent:            opts.OnEvent,
	}, nil
}

// Dial open one authenticated websocket session and wrap frame handling state.
//
// Adds Authorization bearer header and returns a manager-compatible connection.
//
// Returns a live connection or an error.
func (d *WebhookDialer) Dial(ctx context.Context, rawURL string, token string) (Connection, error) {
	trimmedURL := strings.TrimSpace(rawURL)
	trimmedToken := strings.TrimSpace(token)

	if trimmedURL == "" {
		return nil, fmt.Errorf("websocket url is required")
	}
	if trimmedToken == "" {
		return nil, fmt.Errorf("connector token is required")
	}

	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+trimmedToken)

	wsConn, err := d.dialContext(ctx, trimmedURL, headers)
	if err != nil {
		return nil, fmt.Errorf("dial websocket: %w", err)
	}

	connection, err := newWebhookConnection(webhookConnectionOptions{
		websocketConn:      wsConn,
		deliverer:          d.deliverer,
		runtimeURL:         d.runtimeURL,
		openClawConfigPath: d.openClawConfigPath,
		onEvent:            d.onEvent,
	})
	if err != nil {
		_ = wsConn.Close(websocket.StatusInternalError, "invalid bridge connection options")
		return nil, err
	}

	return connection, nil
}

// Read process one inbound websocket frame.
//
// Handles ping/pong and webhook event delivery/result reporting.
//
// Returns nil for handled frames, or an error to trigger reconnect.
func (c *webhookConnection) Read(ctx context.Context) error {
	messageType, payload, err := c.websocketConn.Read(ctx)
	if err != nil {
		return err
	}

	if messageType != websocket.MessageText {
		return nil
	}

	var envelope websocketEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil
	}

	switch envelope.Type {
	case "ping":
		return c.writeJSON(ctx, map[string]any{"type": "pong"})
	case "webhook_event":
		return c.handleWebhookEvent(ctx, payload)
	case "webhook_result_ack":
		return nil
	default:
		return nil
	}
}

// Close gracefully close the underlying websocket session.
//
// Returns close error from underlying websocket implementation, if any.
func (c *webhookConnection) Close() error {
	return c.websocketConn.Close(websocket.StatusNormalClosure, "agent-flows-bridge closing")
}

func newWebhookConnection(opts webhookConnectionOptions) (*webhookConnection, error) {
	runtimeURL := strings.TrimSpace(opts.runtimeURL)
	openClawConfigPath := strings.TrimSpace(opts.openClawConfigPath)

	if opts.websocketConn == nil {
		return nil, fmt.Errorf("websocket connection is required")
	}
	if opts.deliverer == nil {
		return nil, fmt.Errorf("deliverer is required")
	}
	if runtimeURL == "" {
		return nil, fmt.Errorf("runtime url is required")
	}
	if openClawConfigPath == "" {
		return nil, fmt.Errorf("openclaw config path is required")
	}

	return &webhookConnection{
		websocketConn:      opts.websocketConn,
		deliverer:          opts.deliverer,
		runtimeURL:         runtimeURL,
		openClawConfigPath: openClawConfigPath,
		onEvent:            opts.onEvent,
	}, nil
}

func (c *webhookConnection) handleWebhookEvent(ctx context.Context, payload []byte) error {
	var event websocketWebhookEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return nil
	}

	eventID := strings.TrimSpace(event.EventID)
	if eventID == "" {
		return nil
	}

	eventPayload := event.Payload
	if eventPayload == nil {
		eventPayload = map[string]any{}
	}

	deliveryResult, deliveryErr := c.deliverer.Deliver(ctx, receipt.DeliverInput{
		RuntimeURL:         c.runtimeURL,
		OpenClawConfigPath: c.openClawConfigPath,
		Payload:            eventPayload,
	})

	outcome := "failed"
	runID := ""
	detail := ""
	if deliveryErr == nil {
		runID = strings.TrimSpace(deliveryResult.RunID)
		if deliveryResult.Accepted {
			outcome = "delivered"
		} else {
			detail = strings.TrimSpace(deliveryResult.ResponseBody)
		}
	} else {
		detail = strings.TrimSpace(deliveryErr.Error())
	}

	resultFrame := map[string]any{
		"type":     "webhook_result",
		"event_id": eventID,
		"outcome":  outcome,
	}
	if runID != "" {
		resultFrame["run_id"] = runID
	}
	if detail != "" {
		resultFrame["detail"] = detail
	}

	if err := c.writeJSON(ctx, resultFrame); err != nil {
		return err
	}

	if c.onEvent != nil {
		c.onEvent(WebhookDeliveryEvent{
			EventID: eventID,
			Outcome: outcome,
			RunID:   runID,
			Detail:  detail,
		})
	}

	return nil
}

func (c *webhookConnection) writeJSON(ctx context.Context, payload any) error {
	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode websocket frame: %w", err)
	}

	if err := c.websocketConn.Write(ctx, websocket.MessageText, encodedPayload); err != nil {
		return fmt.Errorf("write websocket frame: %w", err)
	}

	return nil
}

func defaultDialContext(
	ctx context.Context,
	rawURL string,
	header http.Header,
) (websocketConn, error) {
	dialOptions := &websocket.DialOptions{HTTPHeader: header}

	conn, response, err := websocket.Dial(ctx, rawURL, dialOptions)
	responseBody := ""

	if response != nil && response.Body != nil {
		bodyBytes, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		responseBody = strings.TrimSpace(string(bodyBytes))
		_ = response.Body.Close()
	}

	if err != nil {
		if response != nil && response.StatusCode > 0 {
			return nil, &HTTPDialError{
				StatusCode: response.StatusCode,
				Body:       responseBody,
			}
		}

		return nil, err
	}

	return &websocketConnection{conn: conn}, nil
}

func (c *websocketConnection) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	return c.conn.Read(ctx)
}

func (c *websocketConnection) Write(
	ctx context.Context,
	messageType websocket.MessageType,
	payload []byte,
) error {
	return c.conn.Write(ctx, messageType, payload)
}

func (c *websocketConnection) Close(statusCode websocket.StatusCode, reason string) error {
	return c.conn.Close(statusCode, reason)
}
