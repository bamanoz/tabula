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

func TestAsyncPublisherCriticalPublishDoesNotBlockWhenFull(t *testing.T) {
	publisher := newAsyncPublisher()
	t.Cleanup(publisher.Close)
	for i := 0; i < asyncFrameBufferSize; i++ {
		publisher.PublishCritical(i)
	}

	done := make(chan struct{})
	go func() {
		publisher.PublishCritical("queued")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("critical publish blocked behind a full frame buffer")
	}

	for i := 0; i < asyncFrameBufferSize; i++ {
		<-publisher.Frames()
	}
	select {
	case frame := <-publisher.Frames():
		if frame != "queued" {
			t.Fatalf("frame = %#v, want queued", frame)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for queued critical frame")
	}
}

func TestAsyncPublisherCriticalFramesRemainOrderedWhenFull(t *testing.T) {
	publisher := newAsyncPublisher()
	t.Cleanup(publisher.Close)
	for i := 0; i < asyncFrameBufferSize; i++ {
		publisher.PublishCritical(i)
	}
	publisher.PublishCritical(asyncFrameBufferSize)
	publisher.PublishCritical(asyncFrameBufferSize + 1)

	for want := 0; want < asyncFrameBufferSize+2; want++ {
		select {
		case got := <-publisher.Frames():
			if got != want {
				t.Fatalf("frame[%d] = %#v", want, got)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for frame %d", want)
		}
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

func TestAsyncPublisherBestEffortDropsAfterClose(t *testing.T) {
	publisher := newAsyncPublisher()
	publisher.Close()
	if publisher.PublishBestEffort("late") {
		t.Fatal("best-effort publish should report drop after close")
	}

	select {
	case frame := <-publisher.Frames():
		t.Fatalf("unexpected frame after close: %#v", frame)
	default:
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
