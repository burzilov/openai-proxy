# HTTP API

The proxy implements a subset of the OpenAI API v1, enough for LiteLLM and standard SDKs.

**Inside Docker Compose:** `http://openai-proxy:8080/v1`  
**Via LiteLLM:** `http://localhost:4000/v1`

## Client authentication

```http
Authorization: Bearer <PROXY_API_KEY>
```

The proxy does **not** forward this key to Codex — upstream uses the OAuth access token from `auth.json`.

If `PROXY_API_KEY` is empty, client auth is disabled (local use only).

## Endpoints

### `GET /healthz`

Liveness probe. No authentication.

### `GET /readyz`

Readiness: checks for a valid Codex OAuth token.

Returns `200` when authenticated, `503` when not.

### `GET /v1/models`

Lists Codex models (proxied from upstream).

### `GET /v1/models/{id}`

Returns a single model or `404`.

### `POST /v1/chat/completions`

Main endpoint. Accepts standard OpenAI Chat Completions requests.

| Field | Support |
|-------|---------|
| `model` | Required |
| `messages` | Required |
| `stream` | Yes |
| `tools` | Yes — `function` and `custom` (e.g. Cursor `ApplyPatch`) |
| `tool_choice` | Yes |
| `max_tokens` | Mapped to `max_output_tokens` when supported |
| `temperature` | Ignored on Codex backend |
| `n` | Only `n=1` |
| `user` | Session key when `X-Session-Id` is not set |

**Non-stream response:** standard `chat.completion` object.  
**Stream response:** `text/event-stream`, `data: {...}\n\n`, ends with `data: [DONE]\n\n`.

#### Multi-turn agents (Cursor)

| Mechanism | Description |
|-----------|-------------|
| `codex_reasoning_items` / `codex_message_items` | Optional fields on assistant messages for clients that echo them back |
| `X-Session-Id` | Request header; proxy stores reasoning artifacts server-side |
| `user` | Fallback session key |
| Auto `conv:…` | If no header/`user`: hash of `model` + first user message |
| Continuation | On `incomplete` / reasoning-only responses, the proxy continues the turn automatically |

### Not supported

- `/v1/embeddings`
- `/v1/images/*`
- `/v1/audio/*`
- `/v1/responses` (use `/v1/chat/completions`)

## Examples

### Non-stream

```bash
curl http://localhost:4000/v1/chat/completions \
  -H "Authorization: Bearer $LITELLM_MASTER_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "codex-gpt-5.4",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

### Stream

```bash
curl http://localhost:4000/v1/chat/completions \
  -H "Authorization: Bearer $LITELLM_MASTER_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "codex-gpt-5.4",
    "stream": true,
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

## Error codes

OpenAI-compatible format:

```json
{
  "error": {
    "message": "...",
    "type": "invalid_request_error",
    "code": "..."
  }
}
```

| HTTP | type | When |
|------|------|------|
| 401 | `invalid_request_error` | Wrong `PROXY_API_KEY` |
| 401 | `authentication_error` | Missing or expired Codex OAuth |
| 400 | `invalid_request_error` | Invalid JSON, unknown model |
| 429 | `rate_limit_error` | Upstream quota / rate limit |
| 502 | `api_error` | Upstream failure |
| 504 | `timeout_error` | Upstream timeout |

## OpenAI SDK

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:4000/v1",
    api_key="sk-litellm-master",
)

resp = client.chat.completions.create(
    model="codex-gpt-5.4",
    messages=[{"role": "user", "content": "ping"}],
)
```

When talking to the proxy directly (inside compose), use `http://openai-proxy:8080/v1`. See [litellm.md](./litellm.md).

## Streaming headers

For SSE through reverse proxies, disable buffering:

```http
X-Accel-Buffering: no
Cache-Control: no-cache
```
