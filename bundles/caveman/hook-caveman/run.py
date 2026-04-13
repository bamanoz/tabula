#!/usr/bin/env python3
"""Caveman mode hook — auto-activates caveman on session start,
tracks /caveman commands via before_message."""

from __future__ import annotations

import json
import os
import re
import sys
from pathlib import Path

ROOT = os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula"))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

from skills.lib.kernel_client import KernelConnection

TABULA_URL  = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")
TABULA_HOME = os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula"))
STATE_FILE  = Path(TABULA_HOME) / "caveman-mode.json"

VALID_MODES = {
    "off", "lite", "full", "ultra",
    "wenyan-lite", "wenyan", "wenyan-full", "wenyan-ultra",
}

HOOK_EVENTS = [
    {"event": "session_start", "priority": 10},
    {"event": "before_message", "priority": 10},
]


# -- Config resolution ---------------------------------------------------------

def get_default_mode() -> str:
    """Resolve default mode: env var > config file > 'full'."""
    env = os.environ.get("CAVEMAN_DEFAULT_MODE", "").strip().lower()
    if env in VALID_MODES:
        return env

    config_dirs = [
        os.environ.get("XDG_CONFIG_HOME", os.path.expanduser("~/.config")),
    ]
    for d in config_dirs:
        cfg = os.path.join(d, "caveman", "config.json")
        if os.path.isfile(cfg):
            try:
                data = json.loads(Path(cfg).read_text())
                mode = data.get("defaultMode", "").strip().lower()
                if mode in VALID_MODES:
                    return mode
            except Exception:
                pass
    return "full"


# -- State (per-session modes) -------------------------------------------------

def _load_state() -> dict:
    if STATE_FILE.exists():
        try:
            return json.loads(STATE_FILE.read_text())
        except Exception:
            pass
    return {}


def _save_state(state: dict):
    STATE_FILE.parent.mkdir(parents=True, exist_ok=True)
    STATE_FILE.write_text(json.dumps(state, ensure_ascii=False))


def get_mode(session: str) -> str:
    state = _load_state()
    return state.get(session, get_default_mode())


def set_mode(session: str, mode: str):
    state = _load_state()
    if mode == "off":
        state.pop(session, None)
    else:
        state[session] = mode
    _save_state(state)


# -- Caveman rules injection --------------------------------------------------

def caveman_rules(mode: str) -> str:
    """Return caveman rules text for the given mode."""
    if mode == "off":
        return ""

    rules = (
        "CAVEMAN MODE ACTIVE — level: {mode}\n\n"
        "Respond terse like smart caveman. All technical substance stay. Only fluff die.\n"
        "Drop: articles (a/an/the), filler (just/really/basically), "
        "pleasantries (sure/certainly/of course), hedging.\n"
        "Fragments OK. Technical terms exact. Code blocks unchanged.\n"
        "Pattern: [thing] [action] [reason]. [next step].\n"
    ).format(mode=mode)

    if mode == "lite":
        rules += "Lite: No filler/hedging. Keep articles + full sentences. Professional but tight.\n"
    elif mode == "ultra":
        rules += "Ultra: Abbreviate (DB/auth/config/req/res/fn), arrows (X → Y), telegraphic.\n"
    elif mode.startswith("wenyan"):
        rules += "Wenyan: Classical Chinese style. Maximum terseness.\n"

    rules += "\nAuto-Clarity: drop caveman for security warnings, irreversible actions, user confusion.\n"
    rules += "Off: 'stop caveman' or 'normal mode'.\n"
    return rules


# -- Main loop -----------------------------------------------------------------

def run():
    conn = KernelConnection(TABULA_URL)
    conn.send({
        "type": "connect",
        "name": "hook-caveman",
        "sends": ["hook_result"],
        "receives": ["hook"],
        "hooks": HOOK_EVENTS,
    })
    conn.recv()  # connected

    log(f"started, default mode: {get_default_mode()}")

    try:
        while True:
            msg = conn.recv()
            if msg is None:
                break
            if msg.get("type") != "hook":
                continue

            event = msg.get("name", "")
            hook_id = msg.get("id", "")
            payload = msg.get("payload")
            if isinstance(payload, str):
                try:
                    payload = json.loads(payload)
                except Exception:
                    payload = {}

            if event == "session_start":
                handle_session_start(conn, hook_id, payload)
            elif event == "before_message":
                handle_before_message(conn, hook_id, payload)

    except KeyboardInterrupt:
        pass
    finally:
        conn.close()


def handle_session_start(conn: KernelConnection, hook_id: str, payload: dict):
    """On session start: inject caveman rules if mode is active."""
    session = payload.get("session", "")
    mode = get_mode(session)

    if mode and mode != "off":
        context = caveman_rules(mode)
        log(f"session_start [{session}]: injecting caveman {mode}")
        conn.send({
            "type": "hook_result",
            "id": hook_id,
            "action": "modify",
            "payload": {"context": context},
        })
    else:
        conn.send({
            "type": "hook_result",
            "id": hook_id,
            "action": "pass",
        })


def handle_before_message(conn: KernelConnection, hook_id: str, payload: dict):
    """On message: detect /caveman commands, pass through everything else."""
    text = payload.get("text", "").strip()
    session = payload.get("session", "")

    # Detect stop commands
    if re.search(r'\b(stop caveman|normal mode)\b', text, re.I):
        set_mode(session, "off")
        log(f"before_message [{session}]: caveman OFF")
        conn.send({
            "type": "hook_result",
            "id": hook_id,
            "action": "pass",
        })
        return

    # Detect /caveman [level] command
    m = re.match(r'^/caveman(?:\s+(\S+))?$', text, re.I)
    if m:
        level = (m.group(1) or get_default_mode()).lower()
        if level not in VALID_MODES:
            level = "full"
        set_mode(session, level)
        log(f"before_message [{session}]: caveman → {level}")
        # Pass through — the gateway/driver will handle the slash command
        conn.send({
            "type": "hook_result",
            "id": hook_id,
            "action": "pass",
        })
        return

    # Everything else: pass through
    conn.send({
        "type": "hook_result",
        "id": hook_id,
        "action": "pass",
    })


def log(msg: str):
    sys.stderr.write(f"[hook-caveman] {msg}\n")
    sys.stderr.flush()


if __name__ == "__main__":
    run()
