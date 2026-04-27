from __future__ import annotations

import os
import signal
import subprocess
import sys
from pathlib import Path

_SDK_SRC = Path(__file__).resolve().parents[1] / "plugin-sdk-python" / "src"
if _SDK_SRC.exists():
    sys.path.insert(0, str(_SDK_SRC))

from tabula_plugin_sdk import PluginAPI, run


def configure(api: PluginAPI) -> None:
    children: list[subprocess.Popen] = []

    @api.tool(
        "hello_ping",
        description="Return a greeting from the hello plugin",
        schema={"type": "object", "properties": {"name": {"type": "string"}}},
        deadline_ms=5000,
    )
    def hello_ping(args, ctx):
        name = args.get("name") or "world"
        api.log(
            "hello_ping_called",
            metric_name="hello_ping_total",
            metric_value=1,
            metric_kind="counter",
            tool="hello_ping",
        )
        api.send("hello_plugin_event", {"name": name}, session=ctx.get("session", ""))
        return f"{api.config.get('greeting', 'hello')}, {name}"

    @api.tool(
        "hello_spawn_child",
        description="Spawn a dummy child so the reference plugin can demonstrate child-PG cleanup",
        schema={"type": "object", "properties": {"seconds": {"type": "integer"}}},
        deadline_ms=5000,
    )
    def hello_spawn_child(args, _ctx):
        seconds = int(args.get("seconds") or 60)
        seconds = max(1, min(seconds, 300))
        child = subprocess.Popen(
            ["sleep", str(seconds)],
            start_new_session=(os.name != "nt"),
        )
        children.append(child)
        api.log("hello_child_spawned", child_pid=child.pid)
        return str(child.pid)

    @api.on("before_tool_call", priority=50)
    def before_tool_call(data, _ctx):
        if data.get("tool") == "hello_ping":
            original = data.get("input") or {}
            if isinstance(original, dict) and original.get("name") == "blocked":
                return {"action": "deny", "reason": "blocked by hello plugin"}
            if isinstance(original, dict) and original.get("name") == "rewrite":
                return {"action": "rewrite", "data": {**data, "input": {"name": "rewritten"}}}
        return {"action": "ok"}

    @api.on_shutdown
    def cleanup_children() -> None:
        while children:
            child = children.pop()
            if child.poll() is not None:
                continue
            if os.name != "nt":
                os.killpg(child.pid, signal.SIGTERM)
            else:
                child.terminate()
            try:
                child.wait(timeout=2)
            except subprocess.TimeoutExpired:
                if os.name != "nt":
                    os.killpg(child.pid, signal.SIGKILL)
                else:
                    child.kill()
                child.wait(timeout=2)


if __name__ == "__main__":
    run(configure)
