package kernel

import (
	"encoding/json"

	"github.com/bamanoz/tabula/internal/kernel/plugin"
)

// toolSource discriminates the dispatch route for a registered LLM tool
// in Hub.toolExec. Per creative `creative-plugin-runtime.md` §4 the kernel
// keeps a single name → dispatch table so name collisions between skills
// and plugin tools are detectable at registration time and handleDynamicTool
// stays a single switch.
type toolSource int

const (
	// toolSourceSkill routes to SkillExec.Run (per-call subprocess via the
	// `exec` field from a SKILL.md descriptor).
	toolSourceSkill toolSource = iota
	// toolSourcePlugin routes to a long-lived plugin process via
	// (*plugin.Handle).SendToolCall over NDJSON. Phase 2 D2.1 ships the
	// dispatch shell only; the live spawn path lands when runtime.go/
	// supervisor.go in internal/kernel/plugin land.
	toolSourcePlugin
)

// toolDispatch is the value stored in Hub.toolExec for each registered
// tool name. Exactly one of Command (skill) or Plugin (plugin handle) is
// populated, selected by Source.
//
// Schema is the tool's JSON schema as advertised in tools.json or in the
// plugin register-reply; today it is informational only — the LLM
// receives the schema via a separate path (toolsJSON marshalled at
// boot). Reserved for future per-tool validation / debug snapshots.
type toolDispatch struct {
	Source     toolSource
	Command    string          // toolSourceSkill: SKILL.md exec command
	Plugin     *plugin.Handle  // toolSourcePlugin: owning plugin handle
	Schema     json.RawMessage // optional JSON schema for the tool
	DeadlineMs int             // optional plugin tool deadline; defaulted by dispatcher
}

// skillDispatch is a small constructor used by NewHub when converting the
// legacy `skillExec map[string]string` argument into the unified
// dispatch table. Kept as a helper rather than inlined so future plugin
// tool registration (Hub.RegisterPlugin in D2.14) shares the obvious
// symmetric path.
func skillDispatch(command string) toolDispatch {
	return toolDispatch{Source: toolSourceSkill, Command: command}
}
