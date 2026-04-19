#!/usr/bin/env python3
"""Tabula deterministic mock LLM driver for testing subagent orchestration."""

from __future__ import annotations

import argparse
import os
import sys
from pathlib import Path

ROOT = os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula"))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

from skills.lib import SkillConfigError, load_skill_config
from skills.lib.driver_runtime import DriverConfig, DriverRuntime
from skills.lib.providers import MockConfig, MockProvider


TABULA_URL = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")
VERBOSE = os.environ.get("TABULA_VERBOSE", "") == "1"


def log(msg: str):
    if VERBOSE:
        sys.stderr.write(f"[driver:mock] {msg}\n")
        sys.stderr.flush()


def load_mock_settings() -> dict:
    settings = load_skill_config(Path(__file__).resolve().parent)
    fanouts = settings.get("default_fanouts")
    return {
        "subagent_count": settings["subagent_count"],
        "max_turns": settings["max_turns"],
        "sleep_ms": settings["sleep_ms"],
        "default_waves": settings["default_waves"],
        "default_fanouts": [max(1, int(item)) for item in fanouts] if fanouts else None,
    }


def main():
    parser = argparse.ArgumentParser(description="Tabula LLM driver (mock)")
    parser.add_argument("--session", default="main", help="Session to join")
    args = parser.parse_args()

    try:
        settings = load_mock_settings()
    except SkillConfigError as e:
        log(f"ERROR: {e}")
        sys.exit(1)

    mock_config = MockConfig(
        subagent_count=settings["subagent_count"],
        mock_turns=settings["max_turns"],
        mock_sleep_ms=settings["sleep_ms"],
        default_waves=settings["default_waves"],
        default_fanouts=settings["default_fanouts"],
    )

    runtime = DriverRuntime(
        DriverConfig(name="mock", url=TABULA_URL, session=args.session),
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
