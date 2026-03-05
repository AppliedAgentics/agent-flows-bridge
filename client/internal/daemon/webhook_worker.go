package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/agentflows/agent-flows-bridge/client/internal/oauth"
	"github.com/agentflows/agent-flows-bridge/client/internal/receipt"
	"github.com/agentflows/agent-flows-bridge/client/internal/timing"
)

// OAuthClient defines claim/report behavior required by the webhook worker.
type OAuthClient interface {
	ClaimWebhookEvent(ctx context.Context) (*oauth.WebhookEvent, error)
	ReportWebhookResult(
		ctx context.Context,
		eventID string,
		outcome string,
		runID string,
		detail string,
	) (oauth.WebhookResultAck, error)
}

// Deliverer defines local OpenClaw webhook delivery behavior.
type Deliverer interface {
	Deliver(ctx context.Context, input receipt.DeliverInput) (receipt.Result, error)
}

// SleepFunc pauses worker execution between iterations.
type SleepFunc func(ctx context.Context, duration time.Duration) error

// IterationEvent captures one worker loop outcome for logging/observability.
type IterationEvent struct {
	Kind        string
	EventID     string
	Outcome     string
	RunID       string
	Detail      string
	ClaimError  error
	ReportError error
}

// WebhookWorkerOptions configure local webhook worker loop dependencies.
type WebhookWorkerOptions struct {
	OAuthClient        OAuthClient
	Deliverer          Deliverer
	RuntimeURL         string
	OpenClawConfigPath string
	PollInterval       time.Duration
	ErrorBackoff       time.Duration
	MaxErrorBackoff    time.Duration
	MaxTransientErrors int
	Sleep              SleepFunc
	OnIteration        func(event IterationEvent)
}

// WebhookWorker continuously claims and forwards queued webhook events.
type WebhookWorker struct {
	oauthClient        OAuthClient
	deliverer          Deliverer
	runtimeURL         string
	openClawConfigPath string
	pollInterval       time.Duration
	errorBackoff       time.Duration
	maxErrorBackoff    time.Duration
	maxTransientErrors int
	sleep              SleepFunc
	onIteration        func(event IterationEvent)
}

// NewWebhookWorker creates a configured worker with safe defaults.
//
// Requires OAuth client, deliverer, runtime URL, and OpenClaw config path.
// Poll interval defaults to 1s and error backoff defaults to 3s.
//
// Returns a worker or an error when required options are missing.
func NewWebhookWorker(opts WebhookWorkerOptions) (*WebhookWorker, error) {
	if opts.OAuthClient == nil {
		return nil, fmt.Errorf("oauth client is required")
	}
	if opts.Deliverer == nil {
		return nil, fmt.Errorf("deliverer is required")
	}

	runtimeURL := strings.TrimSpace(opts.RuntimeURL)
	if runtimeURL == "" {
		return nil, fmt.Errorf("runtime url is required")
	}

	openClawConfigPath := strings.TrimSpace(opts.OpenClawConfigPath)
	if openClawConfigPath == "" {
		return nil, fmt.Errorf("openclaw config path is required")
	}

	pollInterval := opts.PollInterval
	if pollInterval <= 0 {
		pollInterval = 1 * time.Second
	}

	errorBackoff := opts.ErrorBackoff
	if errorBackoff <= 0 {
		errorBackoff = 3 * time.Second
	}

	maxErrorBackoff := opts.MaxErrorBackoff
	if maxErrorBackoff <= 0 {
		maxErrorBackoff = 60 * time.Second
	}

	maxTransientErrors := opts.MaxTransientErrors
	if maxTransientErrors <= 0 {
		maxTransientErrors = 20
	}

	sleep := opts.Sleep
	if sleep == nil {
		sleep = defaultSleep
	}

	return &WebhookWorker{
		oauthClient:        opts.OAuthClient,
		deliverer:          opts.Deliverer,
		runtimeURL:         runtimeURL,
		openClawConfigPath: openClawConfigPath,
		pollInterval:       pollInterval,
		errorBackoff:       errorBackoff,
		maxErrorBackoff:    maxErrorBackoff,
		maxTransientErrors: maxTransientErrors,
		sleep:              sleep,
		onIteration:        opts.OnIteration,
	}, nil
}

// Run executes the claim-deliver-report loop until context cancellation.
//
// Each iteration claims at most one event. No-event iterations sleep poll
// interval; claim/report errors sleep error backoff.
//
// Returns nil on normal cancellation or an error for non-cancellation failures.
func (w *WebhookWorker) Run(ctx context.Context) error {
	consecutiveTransientErrors := 0

	for {
		if ctx.Err() != nil {
			return nil
		}

		claimedEvent, err := w.oauthClient.ClaimWebhookEvent(ctx)
		if err != nil {
			w.emit(IterationEvent{Kind: "claim_error", ClaimError: err})

			if isTerminalAuthError(err) {
				return err
			}

			consecutiveTransientErrors++
			if consecutiveTransientErrors >= w.maxTransientErrors {
				return fmt.Errorf("max consecutive transient claim errors reached: %w", err)
			}

			if pauseErr := w.pause(ctx, w.transientBackoff(consecutiveTransientErrors)); pauseErr != nil {
				return pauseErr
			}
			continue
		}

		if claimedEvent == nil {
			consecutiveTransientErrors = 0
			w.emit(IterationEvent{Kind: "no_event"})
			if pauseErr := w.pause(ctx, w.pollInterval); pauseErr != nil {
				return pauseErr
			}
			continue
		}

		deliveryResult, deliveryErr := w.deliverer.Deliver(ctx, receipt.DeliverInput{
			RuntimeURL:         w.runtimeURL,
			OpenClawConfigPath: w.openClawConfigPath,
			Payload:            claimedEvent.Payload,
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

		_, reportErr :=
			w.oauthClient.ReportWebhookResult(ctx, claimedEvent.EventID, outcome, runID, detail)
		if reportErr != nil {
			w.emit(IterationEvent{
				Kind:        "report_error",
				EventID:     claimedEvent.EventID,
				Outcome:     outcome,
				RunID:       runID,
				Detail:      detail,
				ReportError: reportErr,
			})

			if isTerminalAuthError(reportErr) {
				return reportErr
			}

			consecutiveTransientErrors++
			if consecutiveTransientErrors >= w.maxTransientErrors {
				return fmt.Errorf("max consecutive transient report errors reached: %w", reportErr)
			}

			if pauseErr := w.pause(ctx, w.transientBackoff(consecutiveTransientErrors)); pauseErr != nil {
				return pauseErr
			}
			continue
		}

		consecutiveTransientErrors = 0

		eventKind := "delivery_failed"
		if outcome == "delivered" {
			eventKind = "delivered"
		}
		w.emit(IterationEvent{
			Kind:    eventKind,
			EventID: claimedEvent.EventID,
			Outcome: outcome,
			RunID:   runID,
			Detail:  detail,
		})
	}
}

func (w *WebhookWorker) emit(event IterationEvent) {
	if w.onIteration != nil {
		w.onIteration(event)
	}
}

func (w *WebhookWorker) pause(ctx context.Context, duration time.Duration) error {
	err := w.sleep(ctx, duration)
	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
		return nil
	}

	return err
}

func (w *WebhookWorker) transientBackoff(consecutiveErrors int) time.Duration {
	backoff := w.errorBackoff
	for i := 1; i < consecutiveErrors; i++ {
		backoff *= 2
		if backoff >= w.maxErrorBackoff {
			return w.maxErrorBackoff
		}
	}

	return backoff
}

func defaultSleep(ctx context.Context, duration time.Duration) error {
	return timing.Sleep(ctx, duration)
}

func isTerminalAuthError(err error) bool {
	var apiError *oauth.APIError
	if !errors.As(err, &apiError) {
		return false
	}

	switch apiError.StatusCode {
	case 401, 403, 409:
		return true
	}

	switch strings.ToUpper(strings.TrimSpace(apiError.Code)) {
	case "INVALID_CONNECTOR_TOKEN", "CONNECTOR_REVOKED":
		return true
	default:
		return false
	}
}
