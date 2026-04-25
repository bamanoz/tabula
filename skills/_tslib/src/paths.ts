/**
 * Filesystem path helpers for Tabula skills (TypeScript port of `skills/_pylib/paths.py`).
 */

import { homedir } from "node:os";
import path from "node:path";

export function tabulaHome(): string {
	const env = process.env.TABULA_HOME;
	if (env && env.length > 0) return env;
	return path.join(homedir(), ".tabula");
}

export function tabulaUrl(): string | undefined {
	return process.env.TABULA_URL;
}

export function tabulaSession(): string | undefined {
	return process.env.TABULA_SESSION;
}

export function tabulaSpawnToken(): string | undefined {
	return process.env.TABULA_SPAWN_TOKEN;
}

export function configDir(): string {
	return path.join(tabulaHome(), "config");
}

export function dataDir(): string {
	return path.join(tabulaHome(), "data");
}

export function stateDir(): string {
	return path.join(tabulaHome(), "state");
}

export function runDir(): string {
	return path.join(tabulaHome(), "run");
}

export function logsDir(): string {
	return path.join(tabulaHome(), "logs");
}

export function skillConfigDir(skillName: string): string {
	return path.join(configDir(), "skills", skillName);
}

export function skillDataDir(skillName: string): string {
	return path.join(dataDir(), "skills", skillName);
}

export function skillStateDir(skillName: string): string {
	return path.join(stateDir(), "skills", skillName);
}
