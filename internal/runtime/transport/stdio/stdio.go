// Package stdio adapts newline-delimited Runtime API JSON frames over pipes.
package stdio

import (
	"io"
	"sync"

	"github.com/bamanoz/tabula/internal/runtime/codec"
)

type readWriteCloser struct {
	r  io.ReadCloser
	w  io.WriteCloser
	mu sync.Mutex
}

func NewConn(r io.ReadCloser, w io.WriteCloser) *codec.Conn {
	return codec.NewReadWriteCloser(&readWriteCloser{r: r, w: w})
}

func (rw *readWriteCloser) Read(p []byte) (int, error) { return rw.r.Read(p) }

func (rw *readWriteCloser) Write(p []byte) (int, error) {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	return rw.w.Write(p)
}

func (rw *readWriteCloser) Close() error {
	err := rw.r.Close()
	if writeErr := rw.w.Close(); err == nil {
		err = writeErr
	}
	return err
}
