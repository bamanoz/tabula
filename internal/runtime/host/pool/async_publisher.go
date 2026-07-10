package pool

import "sync"

const asyncFrameBufferSize = 128

type asyncPublisher struct {
	frames chan any

	mu       sync.Mutex
	queued   []any
	draining bool
	closed   chan struct{}
}

func newAsyncPublisher() *asyncPublisher {
	return &asyncPublisher{frames: make(chan any, asyncFrameBufferSize), closed: make(chan struct{})}
}

func (p *asyncPublisher) Frames() <-chan any {
	if p == nil {
		return nil
	}
	return p.frames
}

func (p *asyncPublisher) PublishCritical(frame any) {
	if p == nil || p.frames == nil || frame == nil {
		return
	}
	p.mu.Lock()
	select {
	case <-p.closed:
		p.mu.Unlock()
		return
	default:
	}
	if len(p.queued) == 0 && !p.draining {
		select {
		case p.frames <- frame:
			p.mu.Unlock()
			return
		default:
		}
	}
	p.queued = append(p.queued, frame)
	if !p.draining {
		p.draining = true
		go p.drainCritical()
	}
	p.mu.Unlock()
}

func (p *asyncPublisher) drainCritical() {
	for {
		p.mu.Lock()
		if len(p.queued) == 0 {
			p.draining = false
			p.mu.Unlock()
			return
		}
		frame := p.queued[0]
		copy(p.queued, p.queued[1:])
		p.queued[len(p.queued)-1] = nil
		p.queued = p.queued[:len(p.queued)-1]
		p.mu.Unlock()

		select {
		case p.frames <- frame:
		case <-p.closed:
			return
		}
	}
}

func (p *asyncPublisher) PublishBestEffort(frame any) bool {
	if p == nil || p.frames == nil || frame == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case <-p.closed:
		return false
	default:
	}
	select {
	case p.frames <- frame:
		return true
	default:
		return false
	}
}

func (p *asyncPublisher) Close() {
	if p == nil || p.closed == nil {
		return
	}
	select {
	case <-p.closed:
		return
	default:
		close(p.closed)
	}
}
