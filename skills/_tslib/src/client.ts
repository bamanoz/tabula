/**
 * WebSocket client for Tabula skills (TypeScript port of `skills/_pylib/kernel_client.py`).
 *
 * Provides:
 *  - thread-safe send via async queue
 *  - typed receive iteration
 *  - automatic protocol version injection
 */

import WebSocket from "ws";
import { type Envelope, PROTOCOL_VERSION } from "./protocol.ts";

export class KernelConnection {
	private ws: WebSocket;
	private opened: Promise<void>;
	private sendQueue: Promise<void> = Promise.resolve();
	private closed = false;
	private incoming: Envelope[] = [];
	private waiters: Array<{
		resolve: (m: Envelope | null) => void;
		reject: (e: unknown) => void;
		timer?: NodeJS.Timeout;
	}> = [];

	constructor(url: string) {
		this.ws = new WebSocket(url);

		this.opened = new Promise((resolve, reject) => {
			this.ws.once("open", () => resolve());
			this.ws.once("error", (err) => reject(err));
		});

		this.ws.on("message", (data) => {
			let raw: string;
			if (typeof data === "string") raw = data;
			else if (data instanceof Buffer) raw = data.toString("utf8");
			else if (Array.isArray(data)) raw = Buffer.concat(data).toString("utf8");
			else raw = Buffer.from(data as ArrayBuffer).toString("utf8");
			let parsed: Envelope;
			try {
				parsed = JSON.parse(raw) as Envelope;
			} catch {
				return;
			}
			this.deliver(parsed);
		});

		this.ws.on("close", () => {
			this.closed = true;
			this.deliver(null);
		});

		this.ws.on("error", () => {
			// errors during use surface to recv() via close
		});
	}

	private deliver(msg: Envelope | null): void {
		if (this.waiters.length > 0) {
			const w = this.waiters.shift()!;
			if (w.timer) clearTimeout(w.timer);
			w.resolve(msg);
			return;
		}
		if (msg !== null) {
			this.incoming.push(msg);
		}
	}

	async ready(): Promise<void> {
		await this.opened;
	}

	async send(msg: Envelope): Promise<void> {
		if (msg.version === undefined) msg.version = PROTOCOL_VERSION;
		const payload = JSON.stringify(msg);
		await this.opened;
		this.sendQueue = this.sendQueue.then(
			() =>
				new Promise<void>((resolve, reject) => {
					this.ws.send(payload, (err) => {
						if (err) reject(err);
						else resolve();
					});
				}),
		);
		await this.sendQueue;
	}

	/**
	 * Wait for the next message from the kernel.
	 * Returns null when the connection has been closed.
	 * Throws on timeout if `timeoutMs` is provided.
	 */
	recv(timeoutMs?: number): Promise<Envelope | null> {
		if (this.incoming.length > 0) {
			return Promise.resolve(this.incoming.shift()!);
		}
		if (this.closed) return Promise.resolve(null);
		return new Promise<Envelope | null>((resolve, reject) => {
			const w: (typeof this.waiters)[number] = { resolve, reject };
			if (timeoutMs && timeoutMs > 0) {
				w.timer = setTimeout(() => {
					const idx = this.waiters.indexOf(w);
					if (idx >= 0) this.waiters.splice(idx, 1);
					reject(new Error("recv timeout"));
				}, timeoutMs);
			}
			this.waiters.push(w);
		});
	}

	async *messages(): AsyncIterableIterator<Envelope> {
		while (!this.closed || this.incoming.length > 0) {
			const m = await this.recv();
			if (m === null) return;
			yield m;
		}
	}

	close(): void {
		try {
			this.ws.close();
		} catch {
			// ignore
		}
	}
}
