package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProbeReportsGatewayReachableOnHTTP200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	prober := NewProber(Options{Timeout: 2 * time.Second})

	result := prober.Probe(context.Background(), server.URL)
	if !result.GatewayReachable {
		t.Fatalf("expected gateway reachable, got %+v", result)
	}
	if result.ErrorCode != "" {
		t.Fatalf("expected empty error code, got %s", result.ErrorCode)
	}
}

func TestProbeReportsConnectionRefused(t *testing.T) {
	prober := NewProber(Options{Timeout: 200 * time.Millisecond})

	result := prober.Probe(context.Background(), "http://127.0.0.1:1")
	if result.GatewayReachable {
		t.Fatalf("expected gateway unreachable, got %+v", result)
	}
	if result.ErrorCode == "" {
		t.Fatal("expected non-empty error code")
	}
}

func TestProbeRejectsInvalidRuntimeURL(t *testing.T) {
	prober := NewProber(Options{Timeout: 200 * time.Millisecond})

	result := prober.Probe(context.Background(), "not-a-url")
	if result.GatewayReachable {
		t.Fatalf("expected gateway unreachable, got %+v", result)
	}
	if result.ErrorCode != "invalid_runtime_url" {
		t.Fatalf("expected invalid_runtime_url, got %s", result.ErrorCode)
	}
}
