import { describe, expect, it } from "bun:test";
import {
	ALL_MESSAGE_TYPES,
	DEFAULT_KERNEL_TOOLS,
	HOOK_AFTER_SPAWN,
	HOOK_AFTER_TOOL_CALL,
	HOOK_BEFORE_MESSAGE,
	HOOK_BEFORE_SPAWN,
	HOOK_BEFORE_TOOL_CALL,
	HOOK_BLOCK,
	HOOK_CANCEL,
	HOOK_CLAIM,
	HOOK_MODIFY,
	HOOK_PASS,
	HOOK_SESSION_END,
	HOOK_SESSION_START,
	MSG_CONNECT,
	MSG_DONE,
	MSG_HOOK,
	MSG_INIT,
	MSG_MESSAGE,
	MSG_TOOL_USE,
	PROTOCOL_VERSION,
	TOOL_PROCESS_KILL,
	TOOL_PROCESS_LIST,
	TOOL_PROCESS_SPAWN,
	TOOL_SHELL_EXEC,
} from "../src/protocol.ts";

describe("protocol parity", () => {
	it("uses protocol version 1 (matches Python and Go kernel)", () => {
		expect(PROTOCOL_VERSION).toBe(1);
	});

	it("exposes the canonical client->kernel message types", () => {
		for (const t of [MSG_CONNECT, MSG_MESSAGE, MSG_TOOL_USE, MSG_DONE]) {
			expect(ALL_MESSAGE_TYPES.has(t)).toBe(true);
		}
	});

	it("exposes the canonical kernel->client message types", () => {
		for (const t of [MSG_INIT, MSG_HOOK]) {
			expect(ALL_MESSAGE_TYPES.has(t)).toBe(true);
		}
	});

	it("exposes the four built-in kernel tools", () => {
		expect(DEFAULT_KERNEL_TOOLS).toEqual([
			TOOL_SHELL_EXEC,
			TOOL_PROCESS_SPAWN,
			TOOL_PROCESS_KILL,
			TOOL_PROCESS_LIST,
		]);
	});

	it("exposes the canonical hook actions", () => {
		expect([HOOK_PASS, HOOK_MODIFY, HOOK_BLOCK, HOOK_CLAIM].sort()).toEqual(
			["block", "claim", "modify", "pass"],
		);
	});

	it("exposes the canonical hook events", () => {
		const events = [
			HOOK_BEFORE_MESSAGE,
			HOOK_BEFORE_TOOL_CALL,
			HOOK_AFTER_TOOL_CALL,
			HOOK_SESSION_START,
			HOOK_SESSION_END,
			HOOK_CANCEL,
			HOOK_BEFORE_SPAWN,
			HOOK_AFTER_SPAWN,
		];
		for (const e of events) expect(typeof e).toBe("string");
	});
});
