#!/usr/bin/env python3
"""Tabula LLM driver backed by OpenAI Chat Completions API."""

from __future__ import annotations

import argparse
import os
import signal
import sys
from pathlib import Path

ROOT = os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula"))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

from skills.lib import SkillConfigError, load_skill_config
from skills.lib.driver_runtime import DriverConfig, DriverRuntime
from skills.lib.providers import OpenAIChatCompletionsSession

TABULA_URL = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")
VERBOSE = os.environ.get("TABULA_VERBOSE", "") == "1"


def log(msg: str):
    if VERBOSE:
        sys.stderr.write(f"[driver:openai] {msg}\n")
        sys.stderr.flush()


def load_driver_settings() -> dict:
    settings = load_skill_config(Path(__file__).resolve().parent)
    return {
        "api_key": settings["api_key"],
        "base_url": settings["base_url"],
        "model": settings["model"],
    }


def main():
    parser = argparse.ArgumentParser(description="Tabula LLM driver (OpenAI)")
    parser.add_argument("--session", default="main", help="Session to join")
    args = parser.parse_args()

    try:
        settings = load_driver_settings()
    except SkillConfigError as e:
        log(f"ERROR: {e}")
        sys.exit(1)

    runtime = DriverRuntime(
        DriverConfig(name="openai", url=TABULA_URL, session=args.session),
        provider_factory=lambda prompt, tools: OpenAIChatCompletionsSession(
            system_prompt=prompt,
            model=settings["model"],
            api_key=settings["api_key"],
            base_url=settings["base_url"],
            tools=tools,
        ),
        logger=log,
    )

    def handle_sigint(sig, frame):
        log("SIGINT received, shutting down")
        runtime.abort()
        runtime.conn.close()

    signal.signal(signal.SIGINT, handle_sigint)

    log(f"connecting to {TABULA_URL}")
    try:
        runtime.connect()
        runtime.run()
    finally:
        runtime.conn.close()


if __name__ == "__main__":
    main()
