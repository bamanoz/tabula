#!/usr/bin/env python3
"""Tabula subagent backed by OpenAI Responses API."""

from __future__ import annotations

import argparse
import os
import sys
import time
from pathlib import Path

ROOT = os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula"))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

from skills.lib import SkillConfigError, load_skill_config
from skills.lib.paths import ensure_parent, skill_logs_dir
from skills.lib.providers import OpenAISession
from skills.lib.subagent_runtime import SubagentConfig, SubagentRuntime

TABULA_URL = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")
TABULA_SPAWN_TOKEN = os.environ.get("TABULA_SPAWN_TOKEN", "")
DEFAULT_IDLE_TIMEOUT = 0
VERBOSE = os.environ.get("TABULA_VERBOSE", "") == "1"
LOG_FILE = str(skill_logs_dir("subagent-openai") / "subagent.log")


def log(msg: str):
    if VERBOSE:
        sys.stderr.write(f"[subagent:openai] {msg}\n")
        sys.stderr.flush()
        try:
            with open(ensure_parent(Path(LOG_FILE)), "a") as handle:
                handle.write(f"[{time.time():.1f}] {msg}\n")
        except Exception:
            pass


def load_subagent_settings() -> dict:
    settings = load_skill_config(Path(__file__).resolve().parent)
    return {
        "api_key": settings["api_key"],
        "base_url": settings["base_url"],
        "model": settings["model"],
    }


def main():
    parser = argparse.ArgumentParser(description="Tabula subagent (OpenAI)")
    parser.add_argument("--id", required=True)
    parser.add_argument("--parent-session", required=True)
    parser.add_argument("--task", required=True)
    parser.add_argument("--model")
    parser.add_argument("--timeout", type=int, default=DEFAULT_IDLE_TIMEOUT)
    parser.add_argument("--max-turns", type=int, default=20)
    args = parser.parse_args()

    try:
        settings = load_subagent_settings()
    except SkillConfigError as e:
        log(f"ERROR: {e}")
        sys.exit(1)

    max_turns = min(args.max_turns, 50)
    session_name = f"subagent-{args.id}"
    model = args.model or settings["model"]

    runtime = SubagentRuntime(
        SubagentConfig(
            name=session_name,
            provider="openai",
            url=TABULA_URL,
            session_name=session_name,
            parent_session=args.parent_session,
            agent_id=args.id,
            initial_task=args.task,
            idle_timeout=args.timeout,
            max_turns=max_turns,
            spawn_token=TABULA_SPAWN_TOKEN,
        ),
        provider_factory=lambda prompt, tools: OpenAISession(
            system_prompt=prompt + f"\n\nYou have a budget of {max_turns} llm turns. Plan your work to finish within this limit.",
            model=model,
            api_key=settings["api_key"],
            base_url=settings["base_url"],
            tools=tools,
        ),
        logger=log,
    )

    runtime.connect()
    runtime.run()


if __name__ == "__main__":
    main()
