package pool

const asyncFrameBufferSize = 128

type asyncPublisher struct {
	frames chan any
}

func newAsyncPublisher() *asyncPublisher {
	return &asyncPublisher{frames: make(chan any, asyncFrameBufferSize)}
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
	p.frames <- frame
}

func (p *asyncPublisher) PublishBestEffort(frame any) bool {
	if p == nil || p.frames == nil || frame == nil {
		return false
	}
	select {
	case p.frames <- frame:
		return true
	default:
		return false
	}
}
