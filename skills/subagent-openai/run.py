#!/usr/bin/env python3
"""Tabula subagent backed by OpenAI Responses API."""

from __future__ import annotations

import argparse
import os
import sys
import time

ROOT = os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula"))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

from skills.lib import load_env
from skills.lib.providers import OpenAISession
from skills.lib.subagent_runtime import SubagentConfig, SubagentRuntime

load_env()


BASE_URL = os.environ.get("OPENAI_BASE_URL", "https://api.openai.com")
API_KEY = os.environ.get("OPENAI_API_KEY", "")
TABULA_URL = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")
TABULA_SPAWN_TOKEN = os.environ.get("TABULA_SPAWN_TOKEN", "")
DEFAULT_IDLE_TIMEOUT = 0
VERBOSE = os.environ.get("TABULA_VERBOSE", "") == "1"
LOG_FILE = os.path.join(os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula")), "subagent.log")


def log(msg: str):
    if VERBOSE:
        sys.stderr.write(f"[subagent:openai] {msg}\n")
        sys.stderr.flush()
        try:
            with open(LOG_FILE, "a") as handle:
                handle.write(f"[{time.time():.1f}] {msg}\n")
        except Exception:
            pass


def main():
    parser = argparse.ArgumentParser(description="Tabula subagent (OpenAI)")
    parser.add_argument("--id", required=True)
    parser.add_argument("--parent-session", required=True)
    parser.add_argument("--task", required=True)
    parser.add_argument("--model", default=os.environ.get("OPENAI_MODEL", "gpt-5"))
    parser.add_argument("--timeout", type=int, default=DEFAULT_IDLE_TIMEOUT)
    parser.add_argument("--max-turns", type=int, default=20)
    args = parser.parse_args()

    if not API_KEY:
        log("ERROR: OPENAI_API_KEY not set")
        sys.exit(1)

    max_turns = min(args.max_turns, 50)
    session_name = f"subagent-{args.id}"

    runtime = SubagentRuntime(
        SubagentConfig(
            name=session_name,
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
            model=args.model,
            api_key=API_KEY,
            base_url=BASE_URL,
            tools=tools,
        ),
        logger=log,
    )

    runtime.connect()
    runtime.run()


if __name__ == "__main__":
    main()
