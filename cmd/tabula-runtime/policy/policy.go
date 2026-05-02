// Package policy defines runtime-side worker lifecycle contracts.
//
// Worker lifecycle:
//
//	spawned -> initialized -> ready -> calling -> idle -> shutting_down -> exited
//
// Cold workers usually move from ready to calling to exited after a single call;
// warm workers may move between ready/calling/idle repeatedly until shutdown.
package policy

import (
	"context"
	"errors"

	workerwire "github.com/bamanoz/tabula/internal/runtime/worker/wire"
)

// ErrNotImplemented is returned by placeholder policies before M2 spawning
// lands.
var ErrNotImplemented = errors.New("runtime policy: not implemented")

// PluginExecPolicy governs how runtime workers are spawned.
type PluginExecPolicy interface {
	// Spawn creates a worker process according to req and returns after the process
	// exists but before any WorkerInit handshake is necessarily complete.
	Spawn(ctx context.Context, req SpawnReq) (Worker, error)
}

// Worker is the runtime-side handle for one spawned skill/plugin worker.
type Worker interface {
	// Init sends WorkerInit and blocks until WorkerInitAck or ctx/process failure.
	Init(ctx context.Context, init workerwire.WorkerInit) error
	// Call performs one synchronous WorkerCall. Cold workers must reject reused or
	// overlapping calls; warm workers accept sequential calls.
	Call(ctx context.Context, call workerwire.WorkerCall) (workerwire.WorkerResult, error)
	// Shutdown requests cooperative shutdown and escalates according to policy.
	Shutdown(ctx context.Context) error
	// Wait blocks until process exit and returns exit details.
	Wait() (ExitInfo, error)
	// IsAlive reports whether the worker process is believed to still be running.
	IsAlive() bool
}

// SpawnMode identifies whether a worker is single-call cold or reusable warm.
type SpawnMode string

const (
	// SpawnModeCold means a worker should handle at most one call.
	SpawnModeCold SpawnMode = "cold"
	// SpawnModeWarm means a worker may handle multiple sequential calls.
	SpawnModeWarm SpawnMode = "warm"
)

// SpawnReq contains all policy input needed to create a worker.
type SpawnReq struct {
	KernelID   string
	TenantID   string
	TargetID   string
	Runtime    string
	Entry      string
	Manifest   []byte
	Env        map[string]string
	WorkingDir string
	Mode       SpawnMode
}

// ExitInfo describes the observed worker process exit.
type ExitInfo struct {
	Code     int
	Signaled bool
	Message  string
}
