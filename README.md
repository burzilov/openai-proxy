# openai-proxy

OpenAI-compatible HTTP proxy for **OpenAI Codex** (ChatGPT OAuth). Clients speak the standard Chat Completions API; the proxy authenticates with ChatGPT and forwards requests to the Codex backend.

Typical setup: run `openai-proxy` and [LiteLLM](https://docs.litellm.ai/) in Docker Compose. LiteLLM exposes port `:4000` to IDEs, agents, and scripts.

> **Disclaimer**
>
> - **Not affiliated with OpenAI.** This project is unofficial and not endorsed by OpenAI.
> - **Unofficial API.** It uses undocumented ChatGPT/Codex endpoints and the same OAuth client ID as Codex CLI. OpenAI may change or block access at any time.
> - **Personal / internal use.** Intended for convenience (e.g. routing your own ChatGPT subscription through LiteLLM), not as a commercial API replacement.
> - **Use at your own risk.** Possible ChatGPT ToS issues, account restrictions, or loss of access. See [docs/risks.md](docs/risks.md).
> - **No warranty.** Provided under the [MIT License](LICENSE) without guarantees of uptime, compatibility, or continued operation.

```
Clients (Cursor, SDKs)  →  LiteLLM :4000  →  openai-proxy :8080  →  chatgpt.com/backend-api/codex
```

## Quick start

```bash
cp docs/examples/openai-proxy.env.example .env
# Set PROXY_API_KEY and LITELLM_MASTER_KEY

docker compose build
docker compose run --rm openai-proxy auth login
docker compose up -d
```

Verify via LiteLLM (port `8080` is internal to the compose network):

```bash
curl http://localhost:4000/v1/chat/completions \
  -H "Authorization: Bearer $LITELLM_MASTER_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"codex-gpt-5.4","messages":[{"role":"user","content":"ping"}]}'
```

### Docker Hub image

Pre-built image (after publish):

```bash
docker pull burzilov/openai-proxy:latest
```

In `docker-compose.yml`, replace `build:` with:

```yaml
image: burzilov/openai-proxy:latest
```

## CLI

```bash
openai-proxy auth login    # device code OAuth
openai-proxy auth status   # token expiry, account, plan
openai-proxy auth logout   # clear stored credentials
openai-proxy serve         # HTTP server (default in Docker)
```

## Documentation

| Document | Description |
|----------|-------------|
| [docs/deployment.md](docs/deployment.md) | Docker Compose, auth, configuration |
| [docs/api.md](docs/api.md) | Public HTTP API |
| [docs/litellm.md](docs/litellm.md) | LiteLLM integration |
| [docs/risks.md](docs/risks.md) | Limitations and risks |

## Configuration

Only secrets go in `.env`:

| Variable | Description |
|----------|-------------|
| `PROXY_API_KEY` | API key for clients talking to the proxy (LiteLLM, curl) |
| `LITELLM_MASTER_KEY` | LiteLLM master key (docker-compose only) |

Everything else is hardcoded in `internal/config/config.go` (listen address, Codex URLs, OAuth client, timeouts).

## License

[MIT](LICENSE). Trademarks (OpenAI, ChatGPT, Codex, LiteLLM) belong to their respective owners.
