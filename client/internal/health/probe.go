package health

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Options define probe timeout and HTTP client overrides.
type Options struct {
	Timeout    time.Duration
	HTTPClient *http.Client
}

// Prober checks local runtime reachability.
type Prober struct {
	httpClient *http.Client
	timeout    time.Duration
}

// ProbeResult reports local runtime health and reason codes.
type ProbeResult struct {
	GatewayReachable bool      `json:"gateway_reachable"`
	GatewayURL       string    `json:"gateway_url"`
	ErrorCode        string    `json:"error_code,omitempty"`
	ErrorDetail      string    `json:"error_detail,omitempty"`
	CheckedAt        time.Time `json:"checked_at"`
}

// NewProber construct a local runtime health prober.
//
// Defaults timeout to 2 seconds and builds an internal HTTP client when none
// is provided.
//
// Returns a configured Prober.
func NewProber(opts Options) *Prober {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	return &Prober{httpClient: httpClient, timeout: timeout}
}

// Probe check runtime URL reachability and return actionable error codes.
//
// Sends a GET request to the runtime URL and considers 2xx as reachable.
// Non-2xx responses and transport errors map to specific error codes.
//
// Returns a ProbeResult with GatewayReachable and optional error metadata.
func (p *Prober) Probe(ctx context.Context, runtimeURL string) ProbeResult {
	checkedAt := time.Now().UTC().Truncate(time.Second)

	result := ProbeResult{
		GatewayReachable: false,
		GatewayURL:       runtimeURL,
		CheckedAt:        checkedAt,
	}

	parsedURL, err := url.Parse(runtimeURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		result.ErrorCode = "invalid_runtime_url"
		if err != nil {
			result.ErrorDetail = err.Error()
		}
		return result
	}

	probeCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, runtimeURL, nil)
	if err != nil {
		result.ErrorCode = "invalid_runtime_url"
		result.ErrorDetail = err.Error()
		return result
	}

	response, err := p.httpClient.Do(request)
	if err != nil {
		result.ErrorCode = classifyProbeError(err)
		result.ErrorDetail = err.Error()
		return result
	}
	defer response.Body.Close()

	if response.StatusCode >= 200 && response.StatusCode < 300 {
		result.GatewayReachable = true
		return result
	}

	result.ErrorCode = fmt.Sprintf("http_status_%d", response.StatusCode)
	result.ErrorDetail = response.Status
	return result
}

func classifyProbeError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}

	errString := strings.ToLower(err.Error())
	switch {
	case strings.Contains(errString, "connection refused"):
		return "connection_refused"
	case strings.Contains(errString, "no such host"):
		return "dns_error"
	default:
		return "network_error"
	}
}
