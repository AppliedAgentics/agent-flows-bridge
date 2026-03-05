package wss

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRunRetriesDialWithExponentialJitterBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dialer := &fakeDialer{results: []dialResult{
		{err: errors.New("dial failed 1")},
		{err: errors.New("dial failed 2")},
		{conn: &blockingConn{}},
	}}

	sleepDurations := make([]time.Duration, 0, 2)
	var mu sync.Mutex

	connectedCount := 0
	manager := NewManager(Options{
		URL:         "wss://example.test/connect",
		Token:       "access-token",
		Dialer:      dialer,
		BaseBackoff: 1 * time.Second,
		MaxBackoff:  4 * time.Second,
		Jitter: func(duration time.Duration) time.Duration {
			return duration + 100*time.Millisecond
		},
		Sleep: func(_ context.Context, duration time.Duration) error {
			mu.Lock()
			sleepDurations = append(sleepDurations, duration)
			mu.Unlock()
			return nil
		},
		OnConnected: func() {
			connectedCount++
			cancel()
		},
	})

	err := manager.Run(ctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if dialer.calls != 3 {
		t.Fatalf("expected 3 dial attempts, got %d", dialer.calls)
	}
	if connectedCount != 1 {
		t.Fatalf("expected one successful connection, got %d", connectedCount)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(sleepDurations) != 2 {
		t.Fatalf("expected 2 backoff sleeps, got %d", len(sleepDurations))
	}
	if sleepDurations[0] != 1100*time.Millisecond {
		t.Fatalf("unexpected first backoff: %s", sleepDurations[0])
	}
	if sleepDurations[1] != 2100*time.Millisecond {
		t.Fatalf("unexpected second backoff: %s", sleepDurations[1])
	}
}

func TestRunReconnectsAfterConnectionDrop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	firstConn := &sequenceConn{readErrors: []error{errors.New("socket dropped")}}
	secondConn := &blockingConn{}
	dialer := &fakeDialer{results: []dialResult{
		{conn: firstConn},
		{conn: secondConn},
	}}

	sleepDurations := make([]time.Duration, 0, 1)
	connectedCount := 0
	manager := NewManager(Options{
		URL:         "wss://example.test/connect",
		Token:       "access-token",
		Dialer:      dialer,
		BaseBackoff: 1 * time.Second,
		MaxBackoff:  4 * time.Second,
		Jitter: func(duration time.Duration) time.Duration {
			return duration
		},
		Sleep: func(_ context.Context, duration time.Duration) error {
			sleepDurations = append(sleepDurations, duration)
			return nil
		},
		OnConnected: func() {
			connectedCount++
			if connectedCount == 2 {
				cancel()
			}
		},
	})

	err := manager.Run(ctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if dialer.calls != 2 {
		t.Fatalf("expected 2 dial attempts, got %d", dialer.calls)
	}
	if connectedCount != 2 {
		t.Fatalf("expected two successful connections, got %d", connectedCount)
	}
	if len(sleepDurations) != 1 || sleepDurations[0] != 1*time.Second {
		t.Fatalf("unexpected sleep durations: %+v", sleepDurations)
	}
}

func TestRunCapsBackoffAtMaxDuration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dialer := &fakeDialer{results: []dialResult{
		{err: errors.New("fail 1")},
		{err: errors.New("fail 2")},
		{err: errors.New("fail 3")},
		{err: errors.New("fail 4")},
	}}

	sleeps := make([]time.Duration, 0, 4)
	manager := NewManager(Options{
		URL:         "wss://example.test/connect",
		Token:       "access-token",
		Dialer:      dialer,
		BaseBackoff: 1 * time.Second,
		MaxBackoff:  3 * time.Second,
		Jitter: func(duration time.Duration) time.Duration {
			return duration
		},
		Sleep: func(_ context.Context, duration time.Duration) error {
			sleeps = append(sleeps, duration)
			if len(sleeps) == 4 {
				cancel()
			}
			return nil
		},
	})

	err := manager.Run(ctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	want := []time.Duration{1 * time.Second, 2 * time.Second, 3 * time.Second, 3 * time.Second}
	if len(sleeps) != len(want) {
		t.Fatalf("unexpected sleep count: got=%d want=%d", len(sleeps), len(want))
	}
	for index := range want {
		if sleeps[index] != want[index] {
			t.Fatalf("unexpected backoff at index %d: got=%s want=%s", index, sleeps[index], want[index])
		}
	}
}

type fakeDialer struct {
	results []dialResult
	calls   int
}

type dialResult struct {
	conn Connection
	err  error
}

func (d *fakeDialer) Dial(_ context.Context, _ string, _ string) (Connection, error) {
	index := d.calls
	d.calls++

	if index >= len(d.results) {
		return nil, errors.New("unexpected dial")
	}

	result := d.results[index]
	if result.err != nil {
		return nil, result.err
	}

	return result.conn, nil
}

type sequenceConn struct {
	readErrors []error
	readCalls  int
}

func (c *sequenceConn) Read(_ context.Context) error {
	if c.readCalls >= len(c.readErrors) {
		return nil
	}

	err := c.readErrors[c.readCalls]
	c.readCalls++
	return err
}

func (c *sequenceConn) Close() error {
	return nil
}

type blockingConn struct{}

func (c *blockingConn) Read(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func (c *blockingConn) Close() error {
	return nil
}
