package runtime

import (
	"context"
	"errors"
	"testing"
)

func TestNotImplementedConnReturnsStubError(t *testing.T) {
	conn := NewNotImplementedConn()
	ctx := context.Background()
	if _, err := conn.Invoke(ctx, InvokeReq{}); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Invoke err = %v", err)
	}
	if err := conn.Cancel(ctx, "call-1"); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Cancel err = %v", err)
	}
	if _, err := conn.Health(ctx); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Health err = %v", err)
	}
	if _, err := conn.ListCapabilities(ctx); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("ListCapabilities err = %v", err)
	}
	if _, err := conn.Reload(ctx, ReloadReq{}); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Reload err = %v", err)
	}
	if err := conn.Close(); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Close err = %v", err)
	}
}
