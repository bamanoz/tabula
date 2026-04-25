/**
 * Helper for tool subprocess skills.
 *
 * The kernel invokes tool exec commands with JSON params on stdin and
 * expects either plain text or a JSON envelope on stdout. Errors should
 * be emitted on stderr with a non-zero exit code.
 */

import { stdin } from "node:process";

export interface ToolContext {
	params: Record<string, unknown>;
	session?: string;
	tabulaHome: string;
}

export type ToolHandler = (
	ctx: ToolContext,
) => Promise<string | object> | string | object;

export async function readToolInput(): Promise<Record<string, unknown>> {
	const chunks: Buffer[] = [];
	for await (const chunk of stdin) {
		chunks.push(chunk as Buffer);
	}
	if (chunks.length === 0) return {};
	const text = Buffer.concat(chunks).toString("utf8").trim();
	if (text.length === 0) return {};
	try {
		const parsed = JSON.parse(text);
		if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
			return parsed as Record<string, unknown>;
		}
	} catch {
		// fall through
	}
	return {};
}

/**
 * Run a tool handler: read JSON params from stdin, execute, write result to stdout.
 * Exits process when finished. Use `await runTool(...)` from `skill tool <name>` entrypoints.
 */
export async function runTool(handler: ToolHandler): Promise<void> {
	try {
		const params = await readToolInput();
		const ctx: ToolContext = {
			params,
			session: process.env.TABULA_SESSION,
			tabulaHome: process.env.TABULA_HOME ?? "",
		};
		const result = await handler(ctx);
		if (typeof result === "string") {
			process.stdout.write(result);
		} else {
			process.stdout.write(JSON.stringify(result));
		}
		process.exit(0);
	} catch (err) {
		const msg = err instanceof Error ? err.message : String(err);
		process.stderr.write(msg + "\n");
		process.exit(1);
	}
}
