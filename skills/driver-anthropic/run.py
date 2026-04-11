#!/usr/bin/env python3
"""Tabula LLM driver backed by Anthropic."""

from __future__ import annotations

import argparse
import os
import signal
import sys

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

from skills.lib.driver_runtime import AbortError, DriverConfig, DriverRuntime
from skills.lib.providers import AnthropicSession


BASE_URL = os.environ.get("ANTHROPIC_BASE_URL", "https://api.anthropic.com")
API_KEY = os.environ.get("ANTHROPIC_API_KEY", "")
MODEL = os.environ.get("ANTHROPIC_MODEL", "claude-sonnet-4-6")
TABULA_URL = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")
VERBOSE = os.environ.get("TABULA_VERBOSE", "") == "1"


def log(msg: str):
    if VERBOSE:
        sys.stderr.write(f"[driver:anthropic] {msg}\n")
        sys.stderr.flush()


def main():
    parser = argparse.ArgumentParser(description="Tabula LLM driver (Anthropic)")
    parser.add_argument("--session", default="main", help="Session to join")
    args = parser.parse_args()

    if not API_KEY:
        log("ERROR: ANTHROPIC_API_KEY not set")
        sys.exit(1)

    runtime = DriverRuntime(
        DriverConfig(name="anthropic", url=TABULA_URL, session=args.session),
        provider_factory=lambda prompt, tools: AnthropicSession(
            system_prompt=prompt,
            model=MODEL,
            api_key=API_KEY,
            base_url=BASE_URL,
            tools=tools,
        ),
        logger=log,
    )

    def handle_sigint(sig, frame):
        log("SIGINT received, aborting active request")
        runtime.abort()
        raise AbortError("cancelled")

    signal.signal(signal.SIGINT, handle_sigint)

    log(f"connecting to {TABULA_URL}")
    runtime.connect()
    runtime.run()


if __name__ == "__main__":
    main()
