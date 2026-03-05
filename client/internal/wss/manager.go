package wss

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentflows/agent-flows-bridge/client/internal/timing"
)

// Dialer opens outbound websocket-like connections.
type Dialer interface {
	Dial(ctx context.Context, url string, token string) (Connection, error)
}

// Connection represents a live websocket-like session.
type Connection interface {
	Read(ctx context.Context) error
	Close() error
}

// Options configure reconnect and backoff behavior.
type Options struct {
	URL         string
	Token       string
	Dialer      Dialer
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
	Jitter      func(duration time.Duration) time.Duration
	Sleep       func(ctx context.Context, duration time.Duration) error
	OnConnected func()
	OnDropped   func(err error)
	OnDialError func(err error)
}

// Manager maintains a persistent outbound session with reconnect behavior.
type Manager struct {
	url         string
	token       string
	dialer      Dialer
	baseBackoff time.Duration
	maxBackoff  time.Duration
	jitter      func(duration time.Duration) time.Duration
	sleep       func(ctx context.Context, duration time.Duration) error
	onConnected func()
	onDropped   func(err error)
	onDialError func(err error)
}

// NewManager create a session manager with default backoff settings.
//
// Defaults:
// - BaseBackoff: 1s
// - MaxBackoff: 30s
// - Jitter: identity
// - Sleep: timer-backed cancellable sleep
//
// Returns a configured Manager.
func NewManager(opts Options) *Manager {
	baseBackoff := opts.BaseBackoff
	maxBackoff := opts.MaxBackoff
	jitter := opts.Jitter
	sleep := opts.Sleep

	if baseBackoff <= 0 {
		baseBackoff = 1 * time.Second
	}
	if maxBackoff <= 0 {
		maxBackoff = 30 * time.Second
	}
	if maxBackoff < baseBackoff {
		maxBackoff = baseBackoff
	}
	if jitter == nil {
		jitter = func(duration time.Duration) time.Duration {
			return duration
		}
	}
	if sleep == nil {
		sleep = defaultSleep
	}

	return &Manager{
		url:         opts.URL,
		token:       opts.Token,
		dialer:      opts.Dialer,
		baseBackoff: baseBackoff,
		maxBackoff:  maxBackoff,
		jitter:      jitter,
		sleep:       sleep,
		onConnected: opts.OnConnected,
		onDropped:   opts.OnDropped,
		onDialError: opts.OnDialError,
	}
}

// Run keep a connection alive until context cancellation.
//
// Attempts initial dial and reconnects on failures using exponential backoff
// with optional jitter, capped at MaxBackoff.
//
// Returns nil on context cancellation or a terminal configuration error.
func (m *Manager) Run(ctx context.Context) error {
	if m.dialer == nil {
		return fmt.Errorf("dialer is required")
	}
	if m.url == "" {
		return fmt.Errorf("url is required")
	}
	if m.token == "" {
		return fmt.Errorf("token is required")
	}

	backoff := m.baseBackoff

	for {
		if ctx.Err() != nil {
			return nil
		}

		connection, err := m.dialer.Dial(ctx, m.url, m.token)
		if err != nil {
			if m.onDialError != nil {
				m.onDialError(err)
			}

			if err := m.sleepWithBackoff(ctx, backoff); err != nil {
				return nil
			}

			backoff = m.nextBackoff(backoff)
			continue
		}

		if m.onConnected != nil {
			m.onConnected()
		}

		backoff = m.baseBackoff
		readErr := m.readUntilDrop(ctx, connection)
		_ = connection.Close()

		if ctx.Err() != nil {
			return nil
		}

		if readErr != nil && !errors.Is(readErr, context.Canceled) && m.onDropped != nil {
			m.onDropped(readErr)
		}

		if readErr != nil {
			if err := m.sleepWithBackoff(ctx, backoff); err != nil {
				return nil
			}
			backoff = m.nextBackoff(backoff)
		}
	}
}

func (m *Manager) readUntilDrop(ctx context.Context, connection Connection) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		err := connection.Read(ctx)
		if err != nil {
			return err
		}
	}
}

func (m *Manager) sleepWithBackoff(ctx context.Context, backoff time.Duration) error {
	jitteredBackoff := m.jitter(backoff)
	if jitteredBackoff < 0 {
		jitteredBackoff = 0
	}

	if err := m.sleep(ctx, jitteredBackoff); err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return context.Canceled
		}
		return err
	}

	return nil
}

func (m *Manager) nextBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > m.maxBackoff {
		return m.maxBackoff
	}
	return next
}

func defaultSleep(ctx context.Context, duration time.Duration) error {
	return timing.Sleep(ctx, duration)
}
