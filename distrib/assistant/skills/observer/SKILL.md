---
name: observer
description: Collects kernel metrics from hooks and exposes them via HTTP endpoint
---

# Observer

Collects observability metrics from kernel hooks and serves them over HTTP.

## Metrics endpoint

`GET http://127.0.0.1:8091/metrics` returns:

```json
{
  "uptime_sec": 120.5,
  "sessions": {
    "main": {
      "message_count": 15,
      "avg_latency_ms": 340.2,
      "last_message_at": 1713200000.0,
      "clients": ["anthropic"]
    }
  },
  "tools": {
    "EXEC": {
      "calls": 8,
      "errors": 0,
      "total_ms": 1200.0,
      "avg_ms": 150.0
    }
  },
  "spawns": {
    "python3 skills/driver-anthropic/run.py --session main": {
      "count": 1,
      "alive": true
    }
  }
}
```

## Running

```bash
python3 skills/observer/run.py --port 8091
```
