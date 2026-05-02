package mock

import (
	"context"
	"testing"
	"time"

	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func TestMockRuntimeConnInvokeRecordAndCancel(t *testing.T) {
	m := New()
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	m.OnInvoke("default", target, "read").Delay(time.Hour).Return([]byte(`{"ok":true}`))

	done := make(chan runtimeapi.InvokeResp, 1)
	go func() {
		resp, err := m.Invoke(context.Background(), runtimeapi.InvokeReq{CallID: "call-1", TenantID: "default", Target: target, Tool: "read"})
		if err != nil {
			t.Errorf("Invoke returned error: %v", err)
		}
		done <- resp
	}()

	waitCtx, cancelWait := context.WithTimeout(context.Background(), time.Second)
	defer cancelWait()
	if err := m.WaitForRecordedInvokes(waitCtx, 1); err != nil {
		t.Fatalf("waiting for recorded invoke: %v", err)
	}
	if err := m.Cancel(context.Background(), "call-1"); err != nil {
		t.Fatal(err)
	}
	select {
	case resp := <-done:
		if resp.Error == nil || resp.Error.Code != wire.ErrorCancelled {
			t.Fatalf("expected cancelled, got %#v", resp)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for cancelled invoke")
	}
}

func TestMockCloseRejectsFutureInvoke(t *testing.T) {
	m := New()
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	resp, err := m.Invoke(context.Background(), runtimeapi.InvokeReq{CallID: "call-1"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Error == nil || resp.Error.Code != wire.ErrorRuntimeUnavailable || !resp.Error.Retryable {
		t.Fatalf("expected retryable runtime_unavailable, got %#v", resp)
	}
}

func TestMockCloseDrainsPendingInvoke(t *testing.T) {
	m := New()
	target := wire.Target{Kind: wire.TargetKindSkill, ID: "timer"}
	m.OnInvoke("default", target, "run").Delay(time.Hour).Return([]byte(`{"ok":true}`))

	done := make(chan runtimeapi.InvokeResp, 1)
	go func() {
		resp, err := m.Invoke(context.Background(), runtimeapi.InvokeReq{CallID: "call-1", TenantID: "default", Target: target, Tool: "run"})
		if err != nil {
			t.Errorf("Invoke returned error: %v", err)
		}
		done <- resp
	}()
	waitCtx, cancelWait := context.WithTimeout(context.Background(), time.Second)
	defer cancelWait()
	if err := m.WaitForRecordedInvokes(waitCtx, 1); err != nil {
		t.Fatalf("waiting for recorded invoke: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case resp := <-done:
		if resp.Error == nil || resp.Error.Code != wire.ErrorRuntimeUnavailable || !resp.Error.Retryable {
			t.Fatalf("expected retryable runtime_unavailable, got %#v", resp)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for drained invoke")
	}
}
