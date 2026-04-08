#!/usr/bin/env python3
"""Shared WebSocket client for Tabula skills."""

from __future__ import annotations

import json
import threading

import websocket as ws_client


class KernelConnection:
    """Thread-safe WebSocket wrapper used by skills."""

    def __init__(self, url: str):
        self.ws = ws_client.create_connection(url)
        self._lock = threading.Lock()

    def send(self, msg: dict):
        data = json.dumps(msg, ensure_ascii=False)
        with self._lock:
            self.ws.send(data)

    def recv(self, timeout: float | None = None) -> dict | None:
        if timeout is not None:
            self.ws.settimeout(timeout)
        else:
            self.ws.settimeout(None)
        try:
            data = self.ws.recv()
            if not data:
                return None
            return json.loads(data)
        except ws_client.WebSocketTimeoutException as exc:
            raise TimeoutError() from exc
        except (ws_client.WebSocketConnectionClosedException, ConnectionError, OSError):
            return None

    def close(self):
        try:
            self.ws.close()
        except Exception:
            pass
