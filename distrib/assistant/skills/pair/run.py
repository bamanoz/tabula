#!/usr/bin/env python3
"""Universal pairing CLI for Tabula gateways.

Auth state is stored per-gateway in ~/.tabula/auth/<gateway>.json

Usage:
  pair.py <gateway> list
  pair.py <gateway> approve <token>
  pair.py <gateway> revoke <user_id>
"""
from __future__ import annotations

import json
import os
import sys
import time
from pathlib import Path

ROOT = os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula"))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

from skills.lib.paths import skill_data_dir

AUTH_DIR = skill_data_dir("pair")


def auth_file(gateway: str) -> Path:
    return AUTH_DIR / f"{gateway}.json"


def load(gateway: str) -> dict:
    f = auth_file(gateway)
    if f.exists():
        try:
            return json.loads(f.read_text())
        except Exception:
            pass
    return {"authorized": [], "pending": []}


def save(gateway: str, data: dict):
    AUTH_DIR.mkdir(parents=True, exist_ok=True)
    auth_file(gateway).write_text(json.dumps(data, indent=2, ensure_ascii=False))


def cmd_list(gateway: str):
    data = load(gateway)
    authorized = data.get("authorized", [])
    pending    = data.get("pending", [])
    now        = int(time.time())

    print(f"[{gateway}] Authorized ({len(authorized)}):")
    if authorized:
        for uid in authorized:
            print(f"   {uid}")
    else:
        print("   (none)")

    print(f"\n[{gateway}] Pending ({len(pending)}):")
    if pending:
        for p in pending:
            exp     = p["expires"] - now
            expired = " [EXPIRED]" if exp < 0 else f" (expires in {exp//60}m)"
            name = p.get("username", p.get("user_id", "?"))
            print(f"   {p['token']}  @{name} ({p['user_id']}){expired}")
    else:
        print("   (none)")


def cmd_approve(gateway: str, token: str):
    data = load(gateway)
    now  = int(time.time())

    for entry in data.get("pending", []):
        if entry["token"] == token:
            if entry["expires"] < now:
                print(f"Token {token} has expired")
                sys.exit(1)
            data["pending"].remove(entry)
            data.setdefault("authorized", [])
            if entry["user_id"] not in data["authorized"]:
                data["authorized"].append(entry["user_id"])
            save(gateway, data)
            name = entry.get("username", entry.get("user_id", "?"))
            print(f"Approved @{name} (user_id: {entry['user_id']})")
            return

    print(f"Token not found: {token}")
    sys.exit(1)


def cmd_revoke(gateway: str, user_id_str: str):
    # Try int first (Telegram chat_id), fall back to string (Discord user id etc.)
    try:
        user_id: int | str = int(user_id_str)
    except ValueError:
        user_id = user_id_str

    data   = load(gateway)
    before = len(data.get("authorized", []))
    data["authorized"] = [u for u in data.get("authorized", []) if u != user_id]

    if len(data["authorized"]) < before:
        save(gateway, data)
        print(f"Revoked user_id: {user_id}")
    else:
        print(f"user_id {user_id} was not authorized")


# -- Library API for gateways to import ---------------------------------------

def is_authorized(gateway: str, user_id) -> bool:
    data = load(gateway)
    return user_id in data.get("authorized", [])


def create_token(gateway: str, user_id, username: str, ttl: int = 1800) -> str:
    """Generate a pairing token. Replaces any existing pending entry for user_id."""
    import secrets
    token = "PRX-" + secrets.token_hex(3).upper() + "-" + secrets.token_hex(3).upper()
    expires = int(time.time()) + ttl
    data = load(gateway)
    data.setdefault("pending", [])
    data["pending"] = [p for p in data["pending"] if p["user_id"] != user_id]
    data["pending"].append({
        "token":    token,
        "user_id":  user_id,
        "username": username,
        "expires":  expires,
    })
    save(gateway, data)
    return token


def approve(gateway: str, token: str):
    """Approve a pairing token. Returns entry dict or None."""
    data = load(gateway)
    now = int(time.time())
    for entry in data.get("pending", []):
        if entry["token"] == token:
            if entry["expires"] < now:
                return None
            data["pending"].remove(entry)
            data.setdefault("authorized", [])
            if entry["user_id"] not in data["authorized"]:
                data["authorized"].append(entry["user_id"])
            save(gateway, data)
            return entry
    return None


def revoke(gateway: str, user_id) -> bool:
    data = load(gateway)
    before = len(data.get("authorized", []))
    data["authorized"] = [u for u in data.get("authorized", []) if u != user_id]
    changed = len(data["authorized"]) < before
    save(gateway, data)
    return changed


# -- CLI entry point -----------------------------------------------------------

USAGE = """\
Usage:
  pair.py <gateway> list
  pair.py <gateway> approve <token>
  pair.py <gateway> revoke <user_id>

Examples:
  pair.py telegram list
  pair.py telegram approve PRX-A1B2C3-D4E5F6
  pair.py discord revoke 123456789
"""


def main():
    args = sys.argv[1:]
    if len(args) < 2:
        print(USAGE)
        sys.exit(1)

    gateway = args[0]
    cmd = args[1]

    if cmd == "list":
        cmd_list(gateway)
    elif cmd == "approve" and len(args) == 3:
        cmd_approve(gateway, args[2])
    elif cmd == "revoke" and len(args) == 3:
        cmd_revoke(gateway, args[2])
    else:
        print(USAGE)
        sys.exit(1)


if __name__ == "__main__":
    main()
