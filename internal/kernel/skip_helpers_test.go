package kernel

import "testing"

// skipKernelBuiltinRemoved marks a test as skipped because it depends on
// the legacy kernel-builtin LLM tools (shell_exec, process_spawn,
// process_kill, process_list) that were removed in Phase 1 D1.2 of the
// skill/plugin architecture migration.
//
// Per memory-bank/creative/creative-plugin-runtime.md §7 (option b
// "dead-code-keep") and tasks.md D1.11 / D1.12, these tests are kept as
// skipped rather than deleted. Equivalent coverage will be re-introduced
// once:
//   1. Skill-exec hook integration is exercised via synthetic tools
//      (replacing the shell_exec-based tool_hook_test cases), and
//   2. The subagent plugin lands in tabula-bundles, restoring the
//      spawn-token / MaxChildren invariant via plugin-side dispatch
//      (then TestSpawn* / TestSecurityHookTimeoutBlocksSpawn move to
//      plugin integration tests outside this repo).
//
// TODO(skill-plugin-arch): remove this helper and its callers once
// equivalent coverage exists.
func skipKernelBuiltinRemoved(t *testing.T) {
	t.Helper()
	t.Skip("dead-code-keep until subagent plugin GA — see tasks.md D1.11(b) / creative-plugin-runtime.md §7")
}
