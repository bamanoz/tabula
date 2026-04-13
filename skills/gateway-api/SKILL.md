---
name: gateway-api
description: "OpenAI-compatible HTTP API gateway"
---

# Gateway API

OpenAI-compatible HTTP API gateway for Tabula. Exposes `POST /v1/chat/completions` and `POST /v1/responses` endpoints
that any OpenAI SDK or compatible client can use to interact with Tabula.

## Usage

Set `TABULA_API_PORT` environment variable to enable:

```bash
TABULA_API_PORT=8090 tabula
```

## Endpoints

### POST /v1/chat/completions

Standard OpenAI Chat Completions format. Supports streaming (SSE) and non-streaming responses.

### POST /v1/responses

OpenAI Responses API format. Supports streaming (named SSE events) and non-streaming responses.
Input can be a string or array of message items.

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `TABULA_API_PORT` | unset | HTTP port (enables gateway-api when set) |
| `TABULA_API_AUTH` | unset | Bearer token for authentication |

## Session Management

Sessions are resolved in order:
1. `x-session-id` header
2. Deterministic hash of `user` field
3. Random new session
