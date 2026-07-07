# LiteLLM integration

LiteLLM is the gateway: clients connect to `http://litellm:4000/v1`, LiteLLM routes to `openai-proxy`.

## Flow

```
Client (OpenAI SDK)
    │  api_base: http://litellm:4000/v1
    │  api_key: LITELLM_MASTER_KEY
    ▼
LiteLLM Proxy (:4000)
    │  model: codex-gpt-5.4
    │  api_base: http://openai-proxy:8080/v1
    │  api_key: PROXY_API_KEY
    ▼
openai-proxy (:8080)
    │  OAuth Bearer (Codex)
    ▼
chatgpt.com/backend-api/codex
```

## Configuration

File: [examples/litellm_config.yaml](./examples/litellm_config.yaml).

Rules ([LiteLLM docs](https://docs.litellm.ai/docs/providers/openai_compatible)):

1. Model prefix: `openai/<model-id>`
2. `api_base` must end with `/v1`
3. `api_key` is `PROXY_API_KEY`, not the Codex OAuth token

```yaml
model_list:
  - model_name: codex-gpt-5.4
    litellm_params:
      model: openai/gpt-5.4
      api_base: http://openai-proxy:8080/v1
      api_key: os.environ/PROXY_API_KEY

general_settings:
  master_key: os.environ/LITELLM_MASTER_KEY
```

### model_name vs model

| Field | Who sees it | Example |
|-------|-------------|---------|
| `model_name` | LiteLLM clients | `codex-gpt-5.4` |
| `litellm_params.model` | Upstream model ID | `openai/gpt-5.4` |

Clients request `codex-gpt-5.4`; LiteLLM sends `gpt-5.4` to the proxy.

## docker-compose environment

```yaml
services:
  litellm:
    environment:
      PROXY_API_KEY: ${PROXY_API_KEY}
      LITELLM_MASTER_KEY: ${LITELLM_MASTER_KEY}
```

## Verify

```bash
curl http://localhost:4000/v1/chat/completions \
  -H "Authorization: Bearer $LITELLM_MASTER_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "codex-gpt-5.4",
    "messages": [{"role": "user", "content": "ping"}]
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
    "messages": [{"role": "user", "content": "count to 3"}]
  }'
```

### Python SDK

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
print(resp.choices[0].message.content)
```

## Cursor

In Cursor settings:

- **Base URL:** `http://localhost:4000/v1`
- **API key:** `LITELLM_MASTER_KEY`
- **Model:** `codex-gpt-5.4` (or any name from `litellm_config.yaml`)

## Streaming through LiteLLM

- Set `X-Accel-Buffering: no` on reverse proxies
- `proxy_read_timeout` ≥ 300s for long generations

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| `Connection refused` to openai-proxy | Service down | `docker compose ps` |
| LiteLLM `Not Found Error` | `api_base` missing `/v1` | Add `/v1` suffix |
| 401 from proxy | Wrong `PROXY_API_KEY` in LiteLLM env | Check `.env` |
| 401 `authentication_error` | No Codex auth | `docker compose run --rm openai-proxy auth login` |
| 403 from upstream | Cloudflare | See [risks.md](./risks.md) |
| Empty stream | Proxy buffering | Disable buffering |

## Model list

LiteLLM does not auto-discover models from the proxy. Add models explicitly in `litellm_config.yaml`, or periodically refresh from `GET /v1/models`.
