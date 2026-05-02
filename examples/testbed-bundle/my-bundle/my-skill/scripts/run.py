#!/usr/bin/env python3
from __future__ import annotations

import json
import sys

params = json.loads(sys.stdin.read() or "{}")
print(json.dumps({"ok": True, "text": params.get("text", "")}))
