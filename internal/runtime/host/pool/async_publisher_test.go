package pool

import (
	"testing"
	"time"
)

func TestAsyncPublisherPublishesCriticalFrames(t *testing.T) {
	publisher := newAsyncPublisher()
	publisher.PublishCritical("critical")

	select {
	case frame := <-publisher.Frames():
		if frame != "critical" {
			t.Fatalf("frame = %#v, want critical", frame)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for critical frame")
	}
}

func TestAsyncPublisherBestEffortReportsDropWhenFull(t *testing.T) {
	publisher := newAsyncPublisher()
	for i := 0; i < asyncFrameBufferSize; i++ {
		if !publisher.PublishBestEffort(i) {
			t.Fatalf("best-effort frame %d unexpectedly dropped before buffer filled", i)
		}
	}
	if publisher.PublishBestEffort("dropped") {
		t.Fatal("best-effort publish should report drop when buffer is full")
	}

	select {
	case <-publisher.Frames():
	default:
		t.Fatal("expected a queued frame")
	}
	if !publisher.PublishBestEffort("after-drain") {
		t.Fatal("best-effort publish should succeed after one frame is drained")
	}
}

func TestAsyncPublisherIgnoresNilFrames(t *testing.T) {
	publisher := newAsyncPublisher()
	publisher.PublishCritical(nil)
	if publisher.PublishBestEffort(nil) {
		t.Fatal("nil best-effort frame should not publish")
	}

	select {
	case frame := <-publisher.Frames():
		t.Fatalf("unexpected frame: %#v", frame)
	default:
	}
}
