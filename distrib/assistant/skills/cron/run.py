#!/usr/bin/env python3
"""
Tabula Cron Skill — scheduled tasks.

Uses OS crontab when available, falls back to built-in daemon.
Jobs are always stored in jobs.json (source of truth).

Commands: add, list, remove, fire, daemon
"""

import argparse
import datetime
import json
import os
import re
import shlex
import signal
import subprocess
import sys
import time
import uuid
from pathlib import Path

TABULA_HOME = os.environ.get("TABULA_HOME", os.path.join(os.path.expanduser("~"), ".tabula"))
TABULA_URL = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")
if sys.platform == "win32":
    VENV_PYTHON = os.path.join(TABULA_HOME, ".venv", "Scripts", "python.exe")
else:
    VENV_PYTHON = os.path.join(TABULA_HOME, ".venv", "bin", "python3")
SKILL_DIR = os.path.dirname(os.path.abspath(__file__))
CRONTAB_MARKER = "# tabula:"

if TABULA_HOME not in sys.path:
    sys.path.insert(0, TABULA_HOME)

from skills.lib.filelock import lock_file, unlock_file
from skills.lib.paths import ensure_parent, skill_data_dir

DATA_DIR = str(skill_data_dir("cron"))
JOBS_PATH = str(skill_data_dir("cron") / "jobs.json")

CRON_FIELD_RE = re.compile(r"^[\d,\-\*/]+$")


# --- Cron matching (for daemon mode) ---

def field_matches(field: str, value: int) -> bool:
    for part in field.split(","):
        if "/" in part:
            base, step = part.split("/", 1)
            step = int(step)
            if base == "*":
                if value % step == 0:
                    return True
            elif "-" in base:
                lo, hi = base.split("-", 1)
                if int(lo) <= value <= int(hi) and (value - int(lo)) % step == 0:
                    return True
        elif "-" in part:
            lo, hi = part.split("-", 1)
            if int(lo) <= value <= int(hi):
                return True
        elif part == "*":
            return True
        else:
            if int(part) == value:
                return True
    return False


def cron_matches(expr: str, dt: datetime.datetime) -> bool:
    fields = expr.split()
    if len(fields) != 5:
        return False
    # minute, hour, dom, month, dow (0=Sunday, 1=Monday, ..., 6=Saturday)
    dow = dt.isoweekday() % 7  # isoweekday: Mon=1..Sun=7 -> Sun=0..Sat=6
    values = [dt.minute, dt.hour, dt.day, dt.month, dow]
    return all(field_matches(f, v) for f, v in zip(fields, values))


# --- Jobs file I/O ---

def load_jobs() -> list[dict]:
    os.makedirs(DATA_DIR, exist_ok=True)
    if not os.path.exists(JOBS_PATH):
        return []
    with open(JOBS_PATH) as f:
        return json.load(f).get("jobs", [])


def save_jobs(jobs: list[dict]):
    os.makedirs(DATA_DIR, exist_ok=True)
    tmp = JOBS_PATH + ".tmp"
    with open(tmp, "w") as f:
        lock_file(f)
        json.dump({"jobs": jobs}, f, indent=2, ensure_ascii=False)
        f.flush()
        os.fsync(f.fileno())
        unlock_file(f)
    os.replace(tmp, JOBS_PATH)


def find_job(jobs: list[dict], job_id: str) -> dict | None:
    for j in jobs:
        if j["id"] == job_id:
            return j
    return None


# --- Crontab sync ---

def has_crontab() -> bool:
    try:
        result = subprocess.run(["crontab", "-l"], capture_output=True, text=True)
        # crontab -l returns 0 even with "no crontab for user" on some systems
        return result.returncode == 0 or "no crontab" in result.stderr.lower()
    except FileNotFoundError:
        return False


def read_crontab() -> str:
    result = subprocess.run(["crontab", "-l"], capture_output=True, text=True)
    if result.returncode != 0:
        return ""
    return result.stdout


def write_crontab(content: str):
    subprocess.run(["crontab", "-"], input=content, text=True, check=True)


def sync_job_to_crontab(job: dict):
    """Add a job to crontab."""
    if not has_crontab():
        return
    crontab = read_crontab()
    marker = f"{CRONTAB_MARKER}{job['id']}"
    if marker in crontab:
        return  # already there

    task_escaped = shlex.quote(job["task"])
    entry = (
        f"{job['cron']} "
        f"TABULA_HOME={shlex.quote(TABULA_HOME)} "
        f"TABULA_URL={shlex.quote(TABULA_URL)} "
        f"{VENV_PYTHON} {SKILL_DIR}/run.py fire --id {shlex.quote(job['id'])} --task {task_escaped} "
        f"{marker}"
    )
    new_crontab = crontab.rstrip("\n") + "\n" + entry + "\n" if crontab.strip() else entry + "\n"
    write_crontab(new_crontab)


def remove_job_from_crontab(job_id: str):
    """Remove a job from crontab."""
    if not has_crontab():
        return
    crontab = read_crontab()
    marker = f"{CRONTAB_MARKER}{job_id}"
    lines = crontab.splitlines()
    new_lines = [l for l in lines if marker not in l]
    if len(new_lines) != len(lines):
        write_crontab("\n".join(new_lines) + "\n" if new_lines else "")


# --- Validation ---

def validate_cron(expr: str) -> bool:
    fields = expr.split()
    return len(fields) == 5 and all(CRON_FIELD_RE.match(f) for f in fields)


# --- Commands ---

def cmd_add(args):
    cron_expr = args.cron
    if not validate_cron(cron_expr):
        print(json.dumps({"error": f"invalid cron expression: {cron_expr}. Expected 5 fields (minute hour dom month dow)"}))
        sys.exit(1)

    job_id = args.id or str(uuid.uuid4())[:8]
    jobs = load_jobs()

    if find_job(jobs, job_id):
        print(json.dumps({"error": f"job id already exists: {job_id}"}))
        sys.exit(1)

    job = {
        "id": job_id,
        "cron": cron_expr,
        "task": args.task,
        "once": bool(args.once),
        "created": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    }
    jobs.append(job)
    save_jobs(jobs)
    sync_job_to_crontab(job)

    print(json.dumps({"ok": True, "id": job_id}))


def cmd_list(_args):
    jobs = load_jobs()
    output = [{"id": j["id"], "cron": j["cron"], "task": j["task"], "once": j.get("once", False)} for j in jobs]
    print(json.dumps({"jobs": output}, ensure_ascii=False))


def cmd_remove(args):
    job_id = args.id
    jobs = load_jobs()

    if not find_job(jobs, job_id):
        print(json.dumps({"error": f"not found: {job_id}"}))
        sys.exit(1)

    jobs = [j for j in jobs if j["id"] != job_id]
    save_jobs(jobs)
    remove_job_from_crontab(job_id)

    print(json.dumps({"ok": True, "removed": job_id}))


def cleanup_once_job(job_id: str):
    """Remove a one-shot job after it fires."""
    jobs = load_jobs()
    job = find_job(jobs, job_id)
    if job and job.get("once"):
        jobs = [j for j in jobs if j["id"] != job_id]
        save_jobs(jobs)
        remove_job_from_crontab(job_id)


def cmd_fire(args):
    from lib.kernel_client import KernelConnection
    from lib.protocol import MSG_CONNECT, MSG_JOIN, MSG_MESSAGE

    url = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")
    try:
        conn = KernelConnection(url)
        conn.send({
            "type": MSG_CONNECT,
            "name": "cron-fire",
            "sends": [MSG_MESSAGE],
            "receives": [],
        })
        conn.recv(timeout=5)
        conn.send({"type": MSG_JOIN, "session": "main"})
        conn.recv(timeout=5)
        conn.send({
            "type": MSG_MESSAGE,
            "session": "main",
            "id": args.id,
            "text": args.task,
        })
        conn.close()
    except Exception as e:
        print(f"cron fire failed: {e}", file=sys.stderr)
        sys.exit(1)

    cleanup_once_job(args.id)


def cmd_daemon(_args):
    """Built-in scheduler. Used when OS crontab is unavailable."""
    from lib.kernel_client import KernelConnection
    from lib.protocol import MSG_CONNECT, MSG_JOIN, MSG_MESSAGE

    url = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")
    conn = KernelConnection(url)
    conn.send({
        "type": MSG_CONNECT,
        "name": "cron-daemon",
        "sends": [MSG_MESSAGE],
        "receives": [],
    })
    conn.recv(timeout=5)
    conn.send({"type": MSG_JOIN, "session": "main"})
    conn.recv(timeout=5)

    running = True

    def handle_signal(sig, frame):
        nonlocal running
        running = False

    signal.signal(signal.SIGINT, handle_signal)
    signal.signal(signal.SIGTERM, handle_signal)

    last_fired = {}  # "job_id:YYYY-MM-DD HH:MM" -> True

    while running:
        time.sleep(30)
        if not running:
            break

        now = datetime.datetime.now()
        minute_key = now.strftime("%Y-%m-%d %H:%M")

        try:
            jobs = load_jobs()
        except Exception:
            continue

        once_to_remove = []
        for job in jobs:
            fire_key = f"{job['id']}:{minute_key}"
            if fire_key in last_fired:
                continue
            if cron_matches(job["cron"], now):
                try:
                    conn.send({
                        "type": MSG_MESSAGE,
                        "session": "main",
                        "id": job["id"],
                        "text": job["task"],
                    })
                    last_fired[fire_key] = True
                    if job.get("once"):
                        once_to_remove.append(job["id"])
                except Exception:
                    pass

        # Remove one-shot jobs that just fired
        if once_to_remove:
            current_jobs = load_jobs()
            current_jobs = [j for j in current_jobs if j["id"] not in once_to_remove]
            save_jobs(current_jobs)

        # Prune old entries (keep last 10 minutes)
        cutoff = (now - datetime.timedelta(minutes=10)).strftime("%Y-%m-%d %H:%M")
        last_fired = {k: v for k, v in last_fired.items() if k.split(":", 1)[1] >= cutoff}

    conn.close()


def main():
    parser = argparse.ArgumentParser(prog="cron", description="Tabula cron skill")
    sub = parser.add_subparsers(dest="command", required=True)

    p_add = sub.add_parser("add")
    p_add.add_argument("--cron", required=True, help="5-field cron expression")
    p_add.add_argument("--task", required=True, help="Task prompt for the LLM")
    p_add.add_argument("--id", help="Job ID (auto-generated if omitted)")
    p_add.add_argument("--once", action="store_true", help="Fire once then auto-remove")

    sub.add_parser("list")

    p_remove = sub.add_parser("remove")
    p_remove.add_argument("id", help="Job ID to remove")

    p_fire = sub.add_parser("fire")
    p_fire.add_argument("--id", required=True)
    p_fire.add_argument("--task", required=True)

    sub.add_parser("daemon")

    args = parser.parse_args()

    if args.command == "add":
        cmd_add(args)
    elif args.command == "list":
        cmd_list(args)
    elif args.command == "remove":
        cmd_remove(args)
    elif args.command == "fire":
        cmd_fire(args)
    elif args.command == "daemon":
        cmd_daemon(args)


if __name__ == "__main__":
    main()
