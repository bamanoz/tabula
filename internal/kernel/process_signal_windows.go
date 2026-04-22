//go:build windows

package kernel

import "os"

var (
	interruptSignal os.Signal = os.Interrupt
	killSignal      os.Signal = os.Kill
)

// signalProcess sends “sig“ to “proc“. Windows does not have the unix
// process-group semantics we rely on elsewhere, so we just signal the root
// process directly — which is what the wrapping cmd.exe forwards on.
func signalProcess(proc *os.Process, sig os.Signal) error {
	if proc == nil {
		return nil
	}
	return proc.Signal(sig)
}
