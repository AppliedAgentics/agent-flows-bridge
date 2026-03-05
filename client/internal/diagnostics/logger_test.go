package diagnostics

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestLoggerWritesStructuredJSONLine(t *testing.T) {
	buffer := &bytes.Buffer{}
	logger := NewLogger(buffer)

	logger.Info("session connected", Fields{
		"component":      "wss_manager",
		"correlation_id": "corr-123",
		"session_id":     "sess-abc",
	})

	line := buffer.Bytes()
	if len(line) == 0 {
		t.Fatal("expected output log line")
	}

	var payload map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(line), &payload); err != nil {
		t.Fatalf("decode log line: %v", err)
	}

	if payload["level"] != "info" {
		t.Fatalf("expected level=info, got %v", payload["level"])
	}
	if payload["message"] != "session connected" {
		t.Fatalf("unexpected message: %v", payload["message"])
	}
	if payload["component"] != "wss_manager" {
		t.Fatalf("unexpected component: %v", payload["component"])
	}
	if payload["correlation_id"] != "corr-123" {
		t.Fatalf("unexpected correlation_id: %v", payload["correlation_id"])
	}
	if payload["session_id"] != "sess-abc" {
		t.Fatalf("unexpected session_id: %v", payload["session_id"])
	}
	if payload["timestamp"] == nil {
		t.Fatal("expected timestamp field")
	}
}

func TestLoggerWritesFallbackLineOnEncodingError(t *testing.T) {
	buffer := &bytes.Buffer{}
	logger := NewLogger(buffer)

	logger.Error("session connected", Fields{
		"bad_field": make(chan int),
	})

	line := buffer.Bytes()
	if len(line) == 0 {
		t.Fatal("expected fallback output log line")
	}

	var payload map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(line), &payload); err != nil {
		t.Fatalf("decode fallback log line: %v", err)
	}

	if payload["message"] != "session connected" {
		t.Fatalf("unexpected message: %v", payload["message"])
	}
	if payload["encoding_error"] == nil {
		t.Fatalf("expected encoding_error field, got %+v", payload)
	}
}
