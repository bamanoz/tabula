// Package plugin implements the kernel-side runtime for long-lived plugin
// processes (kernel ↔ plugin stdio JSON-RPC).
//
// This package is the home of the PluginRuntime described in creative
// `memory-bank/creative/creative-plugin-runtime.md` §11 and the wire-level
// freeze in `memory-bank/creative/creative-plugin-protocol.md`.
//
// It is intentionally separate from the parent `internal/kernel` package to
// keep supervision domains distinct from WebSocket Client lifecycle: a
// plugin is a kernel-level singleton process supervised by ProcessSupervisor
// (two-tier supervision per design doc §4.3), not a member of
// ClientRegistry.
//
// The HookSubscriber adapter that lets *Handle participate in HookEngine
// dispatch lives in the parent kernel package (see handle_hooksub.go) to
// avoid an import cycle.
//
// Phase 2 D2.1 scaffolding lands the static surface (manifest types,
// message schema, NDJSON framing, Handle skeleton) without spawning real
// processes; the runtime/supervisor wiring follows in subsequent BUILD
// passes.
package plugin
