package daemon

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/agentflows/agent-flows-bridge/client/internal/oauth"
	"github.com/agentflows/agent-flows-bridge/client/internal/receipt"
)

func TestNewWebhookWorkerRequiresOAuthClient(t *testing.T) {
	_, err := NewWebhookWorker(WebhookWorkerOptions{
		Deliverer:          &fakeDeliverer{},
		RuntimeURL:         "http://127.0.0.1:18789",
		OpenClawConfigPath: "/tmp/openclaw.json",
	})
	if err == nil {
		t.Fatal("expected oauth client required error")
	}
}

func TestNewWebhookWorkerRequiresDeliverer(t *testing.T) {
	_, err := NewWebhookWorker(WebhookWorkerOptions{
		OAuthClient:        &fakeOAuthClient{},
		RuntimeURL:         "http://127.0.0.1:18789",
		OpenClawConfigPath: "/tmp/openclaw.json",
	})
	if err == nil {
		t.Fatal("expected deliverer required error")
	}
}

func TestNewWebhookWorkerRequiresRuntimeURL(t *testing.T) {
	_, err := NewWebhookWorker(WebhookWorkerOptions{
		OAuthClient:        &fakeOAuthClient{},
		Deliverer:          &fakeDeliverer{},
		OpenClawConfigPath: "/tmp/openclaw.json",
	})
	if err == nil {
		t.Fatal("expected runtime url required error")
	}
}

func TestNewWebhookWorkerRequiresOpenClawConfigPath(t *testing.T) {
	_, err := NewWebhookWorker(WebhookWorkerOptions{
		OAuthClient: &fakeOAuthClient{},
		Deliverer:   &fakeDeliverer{},
		RuntimeURL:  "http://127.0.0.1:18789",
	})
	if err == nil {
		t.Fatal("expected openclaw config path required error")
	}
}

func TestRunNoEventSleepsPollIntervalAndEmitsEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	oauthClient := &fakeOAuthClient{
		claimSequence: []claimResult{{event: nil}},
	}

	sleepCalls := []time.Duration{}
	iterationKinds := []string{}

	worker, err := NewWebhookWorker(WebhookWorkerOptions{
		OAuthClient:        oauthClient,
		Deliverer:          &fakeDeliverer{},
		RuntimeURL:         "http://127.0.0.1:18789",
		OpenClawConfigPath: "/tmp/openclaw.json",
		PollInterval:       250 * time.Millisecond,
		Sleep: func(_ context.Context, duration time.Duration) error {
			sleepCalls = append(sleepCalls, duration)
			cancel()
			return context.Canceled
		},
		OnIteration: func(event IterationEvent) {
			iterationKinds = append(iterationKinds, event.Kind)
		},
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	if err := worker.Run(ctx); err != nil {
		t.Fatalf("run worker: %v", err)
	}

	if oauthClient.claimCalls != 1 {
		t.Fatalf("expected one claim call, got %d", oauthClient.claimCalls)
	}

	if len(sleepCalls) != 1 || sleepCalls[0] != 250*time.Millisecond {
		t.Fatalf("expected one poll sleep call, got %+v", sleepCalls)
	}

	if len(iterationKinds) != 1 || iterationKinds[0] != "no_event" {
		t.Fatalf("expected no_event iteration, got %+v", iterationKinds)
	}
}

func TestRunClaimErrorSleepsBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	oauthClient := &fakeOAuthClient{
		claimSequence: []claimResult{{err: errors.New("claim failed")}},
	}

	sleepCalls := []time.Duration{}
	iterationKinds := []string{}

	worker, err := NewWebhookWorker(WebhookWorkerOptions{
		OAuthClient:        oauthClient,
		Deliverer:          &fakeDeliverer{},
		RuntimeURL:         "http://127.0.0.1:18789",
		OpenClawConfigPath: "/tmp/openclaw.json",
		ErrorBackoff:       3 * time.Second,
		Sleep: func(_ context.Context, duration time.Duration) error {
			sleepCalls = append(sleepCalls, duration)
			cancel()
			return context.Canceled
		},
		OnIteration: func(event IterationEvent) {
			iterationKinds = append(iterationKinds, event.Kind)
		},
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	if err := worker.Run(ctx); err != nil {
		t.Fatalf("run worker: %v", err)
	}

	if len(sleepCalls) != 1 || sleepCalls[0] != 3*time.Second {
		t.Fatalf("expected one backoff sleep call, got %+v", sleepCalls)
	}
	if len(iterationKinds) != 1 || iterationKinds[0] != "claim_error" {
		t.Fatalf("expected claim_error iteration, got %+v", iterationKinds)
	}
}

func TestRunClaimErrorUsesExponentialBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	oauthClient := &fakeOAuthClient{
		claimSequence: []claimResult{
			{err: errors.New("claim failed 1")},
			{err: errors.New("claim failed 2")},
			{event: nil},
		},
	}

	sleepCalls := []time.Duration{}

	worker, err := NewWebhookWorker(WebhookWorkerOptions{
		OAuthClient:        oauthClient,
		Deliverer:          &fakeDeliverer{},
		RuntimeURL:         "http://127.0.0.1:18789",
		OpenClawConfigPath: "/tmp/openclaw.json",
		ErrorBackoff:       3 * time.Second,
		MaxErrorBackoff:    60 * time.Second,
		Sleep: func(_ context.Context, duration time.Duration) error {
			sleepCalls = append(sleepCalls, duration)
			if len(sleepCalls) == 3 {
				cancel()
			}
			return context.Canceled
		},
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	if err := worker.Run(ctx); err != nil {
		t.Fatalf("run worker: %v", err)
	}

	if len(sleepCalls) < 2 {
		t.Fatalf("expected at least two sleep calls, got %+v", sleepCalls)
	}
	if sleepCalls[0] != 3*time.Second || sleepCalls[1] != 6*time.Second {
		t.Fatalf("expected exponential backoff [3s, 6s], got %+v", sleepCalls)
	}
}

func TestRunReturnsImmediatelyOnTerminalClaimAuthError(t *testing.T) {
	ctx := context.Background()
	oauthClient := &fakeOAuthClient{
		claimSequence: []claimResult{
			{err: &oauth.APIError{StatusCode: 401, Code: "INVALID_CONNECTOR_TOKEN"}},
		},
	}

	sleepCalled := false
	worker, err := NewWebhookWorker(WebhookWorkerOptions{
		OAuthClient:        oauthClient,
		Deliverer:          &fakeDeliverer{},
		RuntimeURL:         "http://127.0.0.1:18789",
		OpenClawConfigPath: "/tmp/openclaw.json",
		Sleep: func(_ context.Context, _ time.Duration) error {
			sleepCalled = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	err = worker.Run(ctx)
	if err == nil {
		t.Fatal("expected terminal auth error")
	}
	if sleepCalled {
		t.Fatal("expected no sleep after terminal auth error")
	}
}

func TestRunStopsAfterMaxTransientClaimErrors(t *testing.T) {
	ctx := context.Background()
	oauthClient := &fakeOAuthClient{
		claimSequence: []claimResult{
			{err: errors.New("claim failed 1")},
			{err: errors.New("claim failed 2")},
		},
	}

	sleepCalls := 0
	worker, err := NewWebhookWorker(WebhookWorkerOptions{
		OAuthClient:        oauthClient,
		Deliverer:          &fakeDeliverer{},
		RuntimeURL:         "http://127.0.0.1:18789",
		OpenClawConfigPath: "/tmp/openclaw.json",
		ErrorBackoff:       3 * time.Second,
		MaxTransientErrors: 2,
		Sleep: func(_ context.Context, _ time.Duration) error {
			sleepCalls++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	err = worker.Run(ctx)
	if err == nil {
		t.Fatal("expected max transient error failure")
	}
	if sleepCalls != 1 {
		t.Fatalf("expected one sleep before terminal stop, got %d", sleepCalls)
	}
}

func TestRunDeliveredEventReportsDelivered(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	oauthClient := &fakeOAuthClient{
		claimSequence: []claimResult{
			{event: webhookEvent("wev_1")},
			{event: nil},
		},
	}

	deliverer := &fakeDeliverer{
		deliverSequence: []deliverResult{
			{result: receipt.Result{Accepted: true, RunID: "run-123"}},
		},
	}

	iterationKinds := []string{}

	worker, err := NewWebhookWorker(WebhookWorkerOptions{
		OAuthClient:        oauthClient,
		Deliverer:          deliverer,
		RuntimeURL:         "http://127.0.0.1:18789",
		OpenClawConfigPath: "/tmp/openclaw.json",
		Sleep: func(_ context.Context, _ time.Duration) error {
			cancel()
			return context.Canceled
		},
		OnIteration: func(event IterationEvent) {
			iterationKinds = append(iterationKinds, event.Kind)
		},
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	if err := worker.Run(ctx); err != nil {
		t.Fatalf("run worker: %v", err)
	}

	if deliverer.deliverCalls != 1 {
		t.Fatalf("expected one delivery call, got %d", deliverer.deliverCalls)
	}
	if len(oauthClient.reportCalls) != 1 {
		t.Fatalf("expected one report call, got %d", len(oauthClient.reportCalls))
	}

	reportCall := oauthClient.reportCalls[0]
	if reportCall.eventID != "wev_1" || reportCall.outcome != "delivered" || reportCall.runID != "run-123" {
		t.Fatalf("unexpected report call: %+v", reportCall)
	}
	if reportCall.detail != "" {
		t.Fatalf("expected empty detail for delivered event, got %q", reportCall.detail)
	}

	if len(iterationKinds) == 0 || iterationKinds[0] != "delivered" {
		t.Fatalf("expected first iteration kind delivered, got %+v", iterationKinds)
	}
}

func TestRunDeliveryErrorReportsFailedWithErrorDetail(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	oauthClient := &fakeOAuthClient{
		claimSequence: []claimResult{
			{event: webhookEvent("wev_2")},
			{event: nil},
		},
	}

	deliverer := &fakeDeliverer{
		deliverSequence: []deliverResult{
			{err: errors.New("gateway down")},
		},
	}

	iterationKinds := []string{}

	worker, err := NewWebhookWorker(WebhookWorkerOptions{
		OAuthClient:        oauthClient,
		Deliverer:          deliverer,
		RuntimeURL:         "http://127.0.0.1:18789",
		OpenClawConfigPath: "/tmp/openclaw.json",
		Sleep: func(_ context.Context, _ time.Duration) error {
			cancel()
			return context.Canceled
		},
		OnIteration: func(event IterationEvent) {
			iterationKinds = append(iterationKinds, event.Kind)
		},
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	if err := worker.Run(ctx); err != nil {
		t.Fatalf("run worker: %v", err)
	}

	if len(oauthClient.reportCalls) != 1 {
		t.Fatalf("expected one report call, got %d", len(oauthClient.reportCalls))
	}

	reportCall := oauthClient.reportCalls[0]
	if reportCall.outcome != "failed" {
		t.Fatalf("expected failed outcome, got %+v", reportCall)
	}
	if reportCall.detail != "gateway down" {
		t.Fatalf("expected delivery error detail, got %+v", reportCall)
	}

	if len(iterationKinds) == 0 || iterationKinds[0] != "delivery_failed" {
		t.Fatalf("expected first iteration kind delivery_failed, got %+v", iterationKinds)
	}
}

func TestRunDeliveryRejectedReportsFailedWithResponseBody(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	oauthClient := &fakeOAuthClient{
		claimSequence: []claimResult{
			{event: webhookEvent("wev_3")},
			{event: nil},
		},
	}

	deliverer := &fakeDeliverer{
		deliverSequence: []deliverResult{
			{result: receipt.Result{Accepted: false, ResponseBody: "invalid payload"}},
		},
	}

	worker, err := NewWebhookWorker(WebhookWorkerOptions{
		OAuthClient:        oauthClient,
		Deliverer:          deliverer,
		RuntimeURL:         "http://127.0.0.1:18789",
		OpenClawConfigPath: "/tmp/openclaw.json",
		Sleep: func(_ context.Context, _ time.Duration) error {
			cancel()
			return context.Canceled
		},
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	if err := worker.Run(ctx); err != nil {
		t.Fatalf("run worker: %v", err)
	}

	if len(oauthClient.reportCalls) != 1 {
		t.Fatalf("expected one report call, got %d", len(oauthClient.reportCalls))
	}

	reportCall := oauthClient.reportCalls[0]
	if reportCall.outcome != "failed" || reportCall.detail != "invalid payload" {
		t.Fatalf("unexpected report call: %+v", reportCall)
	}
}

func TestRunReportErrorSleepsBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	oauthClient := &fakeOAuthClient{
		claimSequence: []claimResult{{event: webhookEvent("wev_4")}},
		reportErrors:  []error{errors.New("report failed")},
	}

	deliverer := &fakeDeliverer{
		deliverSequence: []deliverResult{
			{result: receipt.Result{Accepted: true, RunID: "run-xyz"}},
		},
	}

	sleepCalls := []time.Duration{}
	iterationKinds := []string{}

	worker, err := NewWebhookWorker(WebhookWorkerOptions{
		OAuthClient:        oauthClient,
		Deliverer:          deliverer,
		RuntimeURL:         "http://127.0.0.1:18789",
		OpenClawConfigPath: "/tmp/openclaw.json",
		ErrorBackoff:       7 * time.Second,
		Sleep: func(_ context.Context, duration time.Duration) error {
			sleepCalls = append(sleepCalls, duration)
			cancel()
			return context.Canceled
		},
		OnIteration: func(event IterationEvent) {
			iterationKinds = append(iterationKinds, event.Kind)
		},
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	if err := worker.Run(ctx); err != nil {
		t.Fatalf("run worker: %v", err)
	}

	if len(sleepCalls) != 1 || sleepCalls[0] != 7*time.Second {
		t.Fatalf("expected one report backoff sleep call, got %+v", sleepCalls)
	}
	if len(iterationKinds) != 1 || iterationKinds[0] != "report_error" {
		t.Fatalf("expected report_error iteration, got %+v", iterationKinds)
	}
}

func TestRunReturnsSleepErrors(t *testing.T) {
	ctx := context.Background()
	expectedErr := errors.New("sleep failed")

	worker, err := NewWebhookWorker(WebhookWorkerOptions{
		OAuthClient: &fakeOAuthClient{
			claimSequence: []claimResult{{event: nil}},
		},
		Deliverer:          &fakeDeliverer{},
		RuntimeURL:         "http://127.0.0.1:18789",
		OpenClawConfigPath: "/tmp/openclaw.json",
		Sleep: func(_ context.Context, _ time.Duration) error {
			return expectedErr
		},
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	err = worker.Run(ctx)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected sleep error, got %v", err)
	}
}

func TestRunReturnsSleepErrorAfterClaimError(t *testing.T) {
	ctx := context.Background()
	expectedErr := errors.New("backoff sleep failed")

	worker, err := NewWebhookWorker(WebhookWorkerOptions{
		OAuthClient: &fakeOAuthClient{
			claimSequence: []claimResult{{err: errors.New("claim failed")}},
		},
		Deliverer:          &fakeDeliverer{},
		RuntimeURL:         "http://127.0.0.1:18789",
		OpenClawConfigPath: "/tmp/openclaw.json",
		Sleep: func(_ context.Context, _ time.Duration) error {
			return expectedErr
		},
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	err = worker.Run(ctx)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected backoff sleep error, got %v", err)
	}
}

func TestRunReturnsSleepErrorAfterReportError(t *testing.T) {
	ctx := context.Background()
	expectedErr := errors.New("report backoff sleep failed")

	worker, err := NewWebhookWorker(WebhookWorkerOptions{
		OAuthClient: &fakeOAuthClient{
			claimSequence: []claimResult{{event: webhookEvent("wev_report_sleep")}},
			reportErrors:  []error{errors.New("report failed")},
		},
		Deliverer: &fakeDeliverer{
			deliverSequence: []deliverResult{{result: receipt.Result{Accepted: true, RunID: "run-1"}}},
		},
		RuntimeURL:         "http://127.0.0.1:18789",
		OpenClawConfigPath: "/tmp/openclaw.json",
		Sleep: func(_ context.Context, _ time.Duration) error {
			return expectedErr
		},
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	err = worker.Run(ctx)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected report backoff sleep error, got %v", err)
	}
}

func TestRunReturnsNilWhenContextCanceledBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	oauthClient := &fakeOAuthClient{}

	worker, err := NewWebhookWorker(WebhookWorkerOptions{
		OAuthClient:        oauthClient,
		Deliverer:          &fakeDeliverer{},
		RuntimeURL:         "http://127.0.0.1:18789",
		OpenClawConfigPath: "/tmp/openclaw.json",
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	if err := worker.Run(ctx); err != nil {
		t.Fatalf("expected nil on canceled context, got %v", err)
	}

	if oauthClient.claimCalls != 0 {
		t.Fatalf("expected no claim calls, got %d", oauthClient.claimCalls)
	}
}

func TestDefaultSleepHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := defaultSleep(ctx, 5*time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestDefaultSleepReturnsNilAfterDuration(t *testing.T) {
	ctx := context.Background()
	err := defaultSleep(ctx, 0)
	if err != nil {
		t.Fatalf("expected nil sleep error, got %v", err)
	}
}

func TestPauseReturnsNilOnDeadlineExceeded(t *testing.T) {
	worker, err := NewWebhookWorker(WebhookWorkerOptions{
		OAuthClient:        &fakeOAuthClient{},
		Deliverer:          &fakeDeliverer{},
		RuntimeURL:         "http://127.0.0.1:18789",
		OpenClawConfigPath: "/tmp/openclaw.json",
		Sleep: func(_ context.Context, _ time.Duration) error {
			return context.DeadlineExceeded
		},
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	if pauseErr := worker.pause(context.Background(), 1*time.Millisecond); pauseErr != nil {
		t.Fatalf("expected nil pause error, got %v", pauseErr)
	}
}

func TestPauseReturnsNilWhenSleepSucceeds(t *testing.T) {
	worker, err := NewWebhookWorker(WebhookWorkerOptions{
		OAuthClient:        &fakeOAuthClient{},
		Deliverer:          &fakeDeliverer{},
		RuntimeURL:         "http://127.0.0.1:18789",
		OpenClawConfigPath: "/tmp/openclaw.json",
		Sleep: func(_ context.Context, _ time.Duration) error {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	if pauseErr := worker.pause(context.Background(), 1*time.Millisecond); pauseErr != nil {
		t.Fatalf("expected nil pause error, got %v", pauseErr)
	}
}

type claimResult struct {
	event *oauth.WebhookEvent
	err   error
}

type reportCall struct {
	eventID string
	outcome string
	runID   string
	detail  string
}

type fakeOAuthClient struct {
	claimSequence []claimResult
	reportErrors  []error
	claimCalls    int
	reportCalls   []reportCall
}

func (f *fakeOAuthClient) ClaimWebhookEvent(_ context.Context) (*oauth.WebhookEvent, error) {
	index := f.claimCalls
	f.claimCalls++

	if index >= len(f.claimSequence) {
		return nil, nil
	}

	result := f.claimSequence[index]
	return result.event, result.err
}

func (f *fakeOAuthClient) ReportWebhookResult(
	_ context.Context,
	eventID string,
	outcome string,
	runID string,
	detail string,
) (oauth.WebhookResultAck, error) {
	f.reportCalls = append(f.reportCalls, reportCall{
		eventID: eventID,
		outcome: outcome,
		runID:   runID,
		detail:  detail,
	})

	index := len(f.reportCalls) - 1
	if index < len(f.reportErrors) && f.reportErrors[index] != nil {
		return oauth.WebhookResultAck{}, f.reportErrors[index]
	}

	return oauth.WebhookResultAck{EventID: eventID, Status: "ok", RunID: runID}, nil
}

type deliverResult struct {
	result receipt.Result
	err    error
}

type fakeDeliverer struct {
	deliverSequence []deliverResult
	deliverCalls    int
}

func (f *fakeDeliverer) Deliver(_ context.Context, _ receipt.DeliverInput) (receipt.Result, error) {
	index := f.deliverCalls
	f.deliverCalls++

	if index >= len(f.deliverSequence) {
		return receipt.Result{}, fmt.Errorf("unexpected delivery call")
	}

	result := f.deliverSequence[index]
	return result.result, result.err
}

func webhookEvent(eventID string) *oauth.WebhookEvent {
	return &oauth.WebhookEvent{
		EventID: eventID,
		Payload: map[string]any{
			"agentId": "lead",
		},
	}
}
