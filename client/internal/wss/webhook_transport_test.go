package wss

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/agentflows/agent-flows-bridge/client/internal/receipt"
	"nhooyr.io/websocket"
)

func TestBuildWebhookSocketURLConvertsHTTPS(t *testing.T) {
	socketURL, err := BuildWebhookSocketURL("https://agentflows.appliedagentics.ai/")
	if err != nil {
		t.Fatalf("build socket url: %v", err)
	}

	want := "wss://agentflows.appliedagentics.ai/api/connectors/webhooks/socket"
	if socketURL != want {
		t.Fatalf("unexpected socket url: got=%q want=%q", socketURL, want)
	}
}

func TestBuildWebhookSocketURLConvertsHTTPAndKeepsBasePath(t *testing.T) {
	socketURL, err := BuildWebhookSocketURL("http://localhost:4000/base")
	if err != nil {
		t.Fatalf("build socket url: %v", err)
	}

	want := "ws://localhost:4000/base/api/connectors/webhooks/socket"
	if socketURL != want {
		t.Fatalf("unexpected socket url: got=%q want=%q", socketURL, want)
	}
}

func TestWebhookConnectionReadProcessesWebhookEventAndWritesDeliveredResult(t *testing.T) {
	ws := &fakeWebSocketConn{
		reads: []fakeRead{
			{
				messageType: websocket.MessageText,
				payload: []byte(`{
					"type":"webhook_event",
					"event_id":"wev_123",
					"runtime_id":98,
					"task_id":42,
					"agent_id":"lead",
					"event_type":"mentioned",
					"payload":{"message":"hello"}
				}`),
			},
		},
	}

	deliverer := &fakeWebhookDeliverer{
		result: receipt.Result{Accepted: true, RunID: "run-local-123"},
	}

	connection, err := newWebhookConnection(webhookConnectionOptions{
		websocketConn:      ws,
		deliverer:          deliverer,
		runtimeURL:         "http://127.0.0.1:18789",
		openClawConfigPath: "/tmp/openclaw.json",
	})
	if err != nil {
		t.Fatalf("new connection: %v", err)
	}

	if err := connection.Read(context.Background()); err != nil {
		t.Fatalf("read frame: %v", err)
	}

	if deliverer.calls != 1 {
		t.Fatalf("expected one delivery call, got %d", deliverer.calls)
	}

	if got := deliverer.lastInput.RuntimeURL; got != "http://127.0.0.1:18789" {
		t.Fatalf("unexpected runtime url: %q", got)
	}

	if got := deliverer.lastInput.OpenClawConfigPath; got != "/tmp/openclaw.json" {
		t.Fatalf("unexpected openclaw config path: %q", got)
	}

	if len(ws.writes) != 1 {
		t.Fatalf("expected one write, got %d", len(ws.writes))
	}

	if ws.writes[0].messageType != websocket.MessageText {
		t.Fatalf("expected text frame write, got %d", ws.writes[0].messageType)
	}

	var frame map[string]any
	if err := json.Unmarshal(ws.writes[0].payload, &frame); err != nil {
		t.Fatalf("decode written frame: %v", err)
	}

	if frame["type"] != "webhook_result" {
		t.Fatalf("unexpected type: %+v", frame)
	}
	if frame["event_id"] != "wev_123" {
		t.Fatalf("unexpected event_id: %+v", frame)
	}
	if frame["outcome"] != "delivered" {
		t.Fatalf("unexpected outcome: %+v", frame)
	}
	if frame["run_id"] != "run-local-123" {
		t.Fatalf("unexpected run_id: %+v", frame)
	}
}

func TestWebhookConnectionReadProcessesWebhookEventAndWritesFailedResultOnDeliverError(t *testing.T) {
	ws := &fakeWebSocketConn{
		reads: []fakeRead{
			{
				messageType: websocket.MessageText,
				payload: []byte(`{
					"type":"webhook_event",
					"event_id":"wev_456",
					"runtime_id":98,
					"task_id":77,
					"agent_id":"writer",
					"event_type":"mentioned",
					"payload":{"message":"hello"}
				}`),
			},
		},
	}

	deliverer := &fakeWebhookDeliverer{
		err: errors.New("gateway down"),
	}

	connection, err := newWebhookConnection(webhookConnectionOptions{
		websocketConn:      ws,
		deliverer:          deliverer,
		runtimeURL:         "http://127.0.0.1:18789",
		openClawConfigPath: "/tmp/openclaw.json",
	})
	if err != nil {
		t.Fatalf("new connection: %v", err)
	}

	if err := connection.Read(context.Background()); err != nil {
		t.Fatalf("read frame: %v", err)
	}

	if len(ws.writes) != 1 {
		t.Fatalf("expected one write, got %d", len(ws.writes))
	}

	var frame map[string]any
	if err := json.Unmarshal(ws.writes[0].payload, &frame); err != nil {
		t.Fatalf("decode written frame: %v", err)
	}

	if frame["outcome"] != "failed" {
		t.Fatalf("unexpected outcome: %+v", frame)
	}
	if frame["detail"] != "gateway down" {
		t.Fatalf("unexpected detail: %+v", frame)
	}
}

func TestWebhookConnectionReadRespondsToPing(t *testing.T) {
	ws := &fakeWebSocketConn{
		reads: []fakeRead{
			{messageType: websocket.MessageText, payload: []byte(`{"type":"ping"}`)},
		},
	}

	connection, err := newWebhookConnection(webhookConnectionOptions{
		websocketConn:      ws,
		deliverer:          &fakeWebhookDeliverer{},
		runtimeURL:         "http://127.0.0.1:18789",
		openClawConfigPath: "/tmp/openclaw.json",
	})
	if err != nil {
		t.Fatalf("new connection: %v", err)
	}

	if err := connection.Read(context.Background()); err != nil {
		t.Fatalf("read frame: %v", err)
	}

	if len(ws.writes) != 1 {
		t.Fatalf("expected one write, got %d", len(ws.writes))
	}

	var frame map[string]any
	if err := json.Unmarshal(ws.writes[0].payload, &frame); err != nil {
		t.Fatalf("decode written frame: %v", err)
	}

	if frame["type"] != "pong" {
		t.Fatalf("expected pong frame, got %+v", frame)
	}
}

func TestWebhookConnectionReadReturnsUnderlyingReadError(t *testing.T) {
	expectedErr := errors.New("socket closed")
	ws := &fakeWebSocketConn{
		reads: []fakeRead{
			{err: expectedErr},
		},
	}

	connection, err := newWebhookConnection(webhookConnectionOptions{
		websocketConn:      ws,
		deliverer:          &fakeWebhookDeliverer{},
		runtimeURL:         "http://127.0.0.1:18789",
		openClawConfigPath: "/tmp/openclaw.json",
	})
	if err != nil {
		t.Fatalf("new connection: %v", err)
	}

	err = connection.Read(context.Background())
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected read error %v, got %v", expectedErr, err)
	}
}

func TestWebhookDialerDialPassesBearerHeader(t *testing.T) {
	var gotURL string
	var gotAuthHeader string

	dialer, err := NewWebhookDialer(WebhookDialerOptions{
		Deliverer:          &fakeWebhookDeliverer{},
		RuntimeURL:         "http://127.0.0.1:18789",
		OpenClawConfigPath: "/tmp/openclaw.json",
		DialContext: func(_ context.Context, rawURL string, header http.Header) (websocketConn, error) {
			gotURL = rawURL
			gotAuthHeader = header.Get("Authorization")
			return &fakeWebSocketConn{}, nil
		},
	})
	if err != nil {
		t.Fatalf("new dialer: %v", err)
	}

	conn, err := dialer.Dial(context.Background(), "wss://agentflows.example.test/api/connectors/webhooks/socket", "access-token-123")
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}

	parsedURL, err := url.Parse(gotURL)
	if err != nil {
		t.Fatalf("parse dial url: %v", err)
	}

	if parsedURL.Scheme != "wss" || parsedURL.Host != "agentflows.example.test" ||
		parsedURL.Path != "/api/connectors/webhooks/socket" {
		t.Fatalf("unexpected dial url: %q", gotURL)
	}

	if gotAuthHeader != "Bearer access-token-123" {
		t.Fatalf("unexpected auth header: %q", gotAuthHeader)
	}

	if parsedURL.Query().Get("token") != "" {
		t.Fatalf("expected no token query parameter, got %q", parsedURL.Query().Get("token"))
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("close connection: %v", err)
	}
}

type fakeWebhookDeliverer struct {
	calls     int
	lastInput receipt.DeliverInput
	result    receipt.Result
	err       error
}

func (f *fakeWebhookDeliverer) Deliver(_ context.Context, input receipt.DeliverInput) (receipt.Result, error) {
	f.calls++
	f.lastInput = input
	if f.err != nil {
		return receipt.Result{}, f.err
	}
	return f.result, nil
}

type fakeRead struct {
	messageType websocket.MessageType
	payload     []byte
	err         error
}

type fakeWrite struct {
	messageType websocket.MessageType
	payload     []byte
}

type fakeWebSocketConn struct {
	reads       []fakeRead
	readIndex   int
	writes      []fakeWrite
	closeCalled bool
}

func (f *fakeWebSocketConn) Read(_ context.Context) (websocket.MessageType, []byte, error) {
	if f.readIndex >= len(f.reads) {
		return websocket.MessageText, nil, errors.New("no more reads")
	}

	item := f.reads[f.readIndex]
	f.readIndex++
	return item.messageType, item.payload, item.err
}

func (f *fakeWebSocketConn) Write(_ context.Context, messageType websocket.MessageType, payload []byte) error {
	f.writes = append(f.writes, fakeWrite{
		messageType: messageType,
		payload:     append([]byte(nil), payload...),
	})
	return nil
}

func (f *fakeWebSocketConn) Close(_ websocket.StatusCode, _ string) error {
	f.closeCalled = true
	return nil
}
