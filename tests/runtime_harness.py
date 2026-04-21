from __future__ import annotations

import json
import os
import shutil
import socket
import subprocess
import tempfile
import time
import urllib.request
from contextlib import closing
from pathlib import Path

import websocket as ws_client

from tests.flat_surface import reset_and_materialize_flat_surface


ROOT = Path(__file__).resolve().parents[1]
_NO_PROXY_OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def get_free_port() -> int:
    with closing(socket.socket()) as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def populate_installed_home(home_path: Path) -> None:
    shutil.copytree(ROOT / "distrib", home_path / "distrib", symlinks=True)
    shutil.copytree(ROOT / "skills", home_path / "skills", symlinks=True)
    shutil.copytree(ROOT / "testing", home_path / "testing", symlinks=True)
    shutil.copytree(ROOT / ".venv", home_path / ".venv", dirs_exist_ok=True)
    reset_and_materialize_flat_surface(home_path, source_root=home_path)


def write_boot_script(
    home_path: Path,
    *,
    tabula_port: int,
    spawn: list[str],
    tools: list[dict] | None = None,
) -> None:
    config = {
        "url": f"ws://127.0.0.1:{tabula_port}/ws",
        "spawn": spawn,
        "tools": tools or [],
    }
    (home_path / "boot.py").write_text(
        "import json, sys\n"
        f"json.dump({config!r}, sys.stdout)\n",
        encoding="utf-8",
    )


def create_test_home(
    tabula_port: int,
    *,
    spawn: list[str],
    prefix: str = "tabula-runtime-smoke-",
) -> str:
    home = tempfile.mkdtemp(prefix=prefix)
    home_path = Path(home)
    populate_installed_home(home_path)
    (home_path / "tabula.yaml").write_text("boot: python3 boot.py\n")
    write_boot_script(home_path, tabula_port=tabula_port, spawn=spawn)
    return home


def start_kernel(home: str, tabula_port: int, *, extra_env: dict[str, str] | None = None) -> subprocess.Popen:
    env = os.environ.copy()
    env["TABULA_HOME"] = home
    env["TABULA_URL"] = f"ws://127.0.0.1:{tabula_port}/ws"
    env["TABULA_BOOT"] = "python3 boot.py"
    env["TABULA_PROVIDER"] = env.get("TABULA_PROVIDER", "mock")
    if extra_env:
        env.update(extra_env)

    proc = subprocess.Popen(
        ["go", "run", "./cmd/tabula", "serve"],
        cwd=ROOT,
        env=env,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.PIPE,
    )

    deadline = time.time() + 10
    last_error = None
    while time.time() < deadline:
        try:
            conn = ws_client.create_connection(env["TABULA_URL"], timeout=1)
            conn.close()
            return proc
        except Exception as exc:  # pragma: no cover - depends on local runtime
            last_error = exc
            time.sleep(0.2)

    terminate_process(proc)
    stderr = ""
    if proc.stderr:
        stderr = proc.stderr.read().decode("utf-8", errors="replace")
    raise RuntimeError(f"kernel did not start: {last_error}\n{stderr}")


def connect_gateway(
    tabula_port: int,
    *,
    session: str = "main",
    name: str = "runtime-smoke",
    sends: list[str] | None = None,
    receives: list[str] | None = None,
):
    conn = ws_client.create_connection(f"ws://127.0.0.1:{tabula_port}/ws", timeout=5)
    conn.send(
        json.dumps(
            {
                "type": "connect",
                "name": name,
                "sends": sends or ["message"],
                "receives": receives or ["stream_start", "stream_delta", "stream_end", "done", "error", "init"],
            }
        )
    )
    connected = json.loads(conn.recv())
    conn.send(json.dumps({"type": "join", "session": session}))
    joined = json.loads(conn.recv())
    init_msg = json.loads(conn.recv())
    return conn, connected, joined, init_msg


def wait_for_session_client(
    tabula_port: int,
    *,
    session: str,
    client_name: str,
    timeout: float = 5.0,
) -> dict:
    deadline = time.time() + timeout
    url = f"http://127.0.0.1:{tabula_port}/sessions"
    last_error = None
    while time.time() < deadline:
        try:
            with _NO_PROXY_OPENER.open(url, timeout=1) as response:
                snapshot = json.loads(response.read().decode("utf-8"))
            clients = snapshot.get(session, {}).get("clients", [])
            if client_name in clients:
                return snapshot
        except Exception as exc:  # pragma: no cover - diagnostic path
            last_error = exc
        time.sleep(0.1)

    raise RuntimeError(
        f"session client {client_name!r} did not join {session!r} before timeout; "
        f"last_error={last_error}"
    )


def collect_turn_text(conn, *, timeout: float = 10.0) -> str:
    messages = collect_until_done(conn, timeout=timeout)
    chunks: list[str] = []
    for msg in messages:
        msg_type = msg.get("type")
        if msg_type == "stream_delta":
            chunks.append(msg.get("text", ""))
        elif msg_type == "error":
            raise AssertionError(msg)
    return "".join(chunks)


def collect_until_done(conn, *, timeout: float = 10.0) -> list[dict]:
    conn.settimeout(timeout)
    messages: list[dict] = []
    while True:
        msg = json.loads(conn.recv())
        messages.append(msg)
        if msg.get("type") == "done":
            return messages


def terminate_process(proc: subprocess.Popen | None):
    if proc is None:
        return
    proc.terminate()
    proc.wait(timeout=10)
