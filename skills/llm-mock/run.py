#!/usr/bin/env python3
"""Tabula deterministic mock LLM driver for testing subagent orchestration."""

from __future__ import annotations

import os
import sys

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

from skills.lib.driver_runtime import DriverConfig, DriverRuntime
from skills.lib.providers import MockConfig, MockProvider


TABULA_URL = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")
VERBOSE = os.environ.get("TABULA_VERBOSE", "") == "1"
SUBAGENT_COUNT = int(os.environ.get("TABULA_MOCK_SUBAGENTS", "3"))
MOCK_TURNS = int(os.environ.get("TABULA_MOCK_TURNS", "5"))
MOCK_SLEEP_MS = int(os.environ.get("TABULA_MOCK_SLEEP_MS", "25"))
DEFAULT_WAVES = int(os.environ.get("TABULA_MOCK_WAVES", "1"))
DEFAULT_FANOUTS = [
    max(1, int(part))
    for part in os.environ.get("TABULA_MOCK_FANOUTS", "").split(",")
    if part.strip()
] or None


def log(msg: str):
    if VERBOSE:
        sys.stderr.write(f"[driver:mock] {msg}\n")
        sys.stderr.flush()


def main():
    mock_config = MockConfig(
        subagent_count=SUBAGENT_COUNT,
        mock_turns=MOCK_TURNS,
        mock_sleep_ms=MOCK_SLEEP_MS,
        default_waves=DEFAULT_WAVES,
        default_fanouts=DEFAULT_FANOUTS,
    )

    runtime = DriverRuntime(
        DriverConfig(name="mock", url=TABULA_URL),
        provider_factory=lambda prompt, tools: MockProvider(
            system_prompt=prompt,
            tools=tools,
            config=mock_config,
        ),
        logger=log,
    )

    log(f"connecting to {TABULA_URL}")
    runtime.connect()
    runtime.run()


if __name__ == "__main__":
    main()
