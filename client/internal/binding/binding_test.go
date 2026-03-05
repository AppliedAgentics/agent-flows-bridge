package binding

import (
	"errors"
	"testing"
)

func TestSaveAndLoadRoundTrip(t *testing.T) {
	stateDir := t.TempDir()

	input := RuntimeBinding{
		RuntimeID:   98,
		RuntimeKind: "local_connector",
		ConnectorID: 2,
		FlowID:      44,
	}

	if err := Save(stateDir, input); err != nil {
		t.Fatalf("save runtime binding: %v", err)
	}

	got, err := Load(stateDir)
	if err != nil {
		t.Fatalf("load runtime binding: %v", err)
	}

	if got.RuntimeID != input.RuntimeID {
		t.Fatalf("expected runtime id %d, got %d", input.RuntimeID, got.RuntimeID)
	}

	if got.RuntimeKind != input.RuntimeKind {
		t.Fatalf("expected runtime kind %q, got %q", input.RuntimeKind, got.RuntimeKind)
	}

	if got.ConnectorID != input.ConnectorID {
		t.Fatalf("expected connector id %d, got %d", input.ConnectorID, got.ConnectorID)
	}

	if got.FlowID != input.FlowID {
		t.Fatalf("expected flow id %d, got %d", input.FlowID, got.FlowID)
	}

	if got.UpdatedAt.IsZero() {
		t.Fatal("expected updated_at to be set")
	}
}

func TestLoadReturnsErrNotFoundWhenMissing(t *testing.T) {
	stateDir := t.TempDir()

	_, err := Load(stateDir)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestClearRemovesBindingFile(t *testing.T) {
	stateDir := t.TempDir()

	if err := Save(stateDir, RuntimeBinding{RuntimeID: 98, RuntimeKind: "local_connector"}); err != nil {
		t.Fatalf("save runtime binding: %v", err)
	}

	if err := Clear(stateDir); err != nil {
		t.Fatalf("clear runtime binding: %v", err)
	}

	_, err := Load(stateDir)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after clear, got %v", err)
	}
}
