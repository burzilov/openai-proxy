# Deployment

## Stack topology

```
┌─────────────────────────────────────────────────────────┐
│ docker-compose network: codex-net                        │
│                                                          │
│  ┌──────────────┐         ┌──────────────────────────┐  │
│  │   litellm    │────────▶│     openai-proxy         │  │
│  │   :4000      │         │     :8080 (internal)     │  │
│  └──────┬───────┘         └───────────┬──────────────┘  │
│         │                             │                  │
│         │                      ┌──────▼──────┐          │
│         │                      │ codex-data  │          │
│         │                      │  (volume)   │          │
│         │                      └─────────────┘          │
└─────────┼─────────────────────────────────────────────┘
          │
    Clients :4000
```

## Services

### openai-proxy

| Setting | Value |
|---------|-------|
| Image | Built from `deploy/Dockerfile` |
| Port | `8080` (internal); optional `8080:8080` for host debugging |
| Volume | `codex-data:/data` — stores `auth.json` |
| Restart | `unless-stopped` |

Codex OAuth must complete before `readyz` returns healthy:

```bash
docker compose run --rm openai-proxy auth login
```

### litellm

| Setting | Value |
|---------|-------|
| Image | `docker.litellm.ai/berriai/litellm:main-stable` |
| Port | `4000:4000` |
| Config | `./docs/examples/litellm_config.yaml` → `/app/config.yaml` |
| Depends on | `openai-proxy` (healthy) |

## First-time setup

```bash
cp docs/examples/openai-proxy.env.example .env
# Edit PROXY_API_KEY and LITELLM_MASTER_KEY

docker compose run --rm openai-proxy auth login
docker compose up -d

curl -s http://localhost:4000/health/liveliness
```

Until `auth login` completes, `readyz` on the proxy returns `503` — expected.

## Authentication

The proxy uses **ChatGPT OAuth** (device code flow), the same mechanism as Codex CLI. An OpenAI Platform API key does **not** work for ChatGPT subscription quota.

### Device code login

1. Run `docker compose run --rm openai-proxy auth login`
2. Open the printed URL (`https://auth.openai.com/codex/device`) and enter the code
3. Tokens are saved to `/data/auth.json` in the `codex-data` volume

### Token refresh

Refresh is automatic. The proxy refreshes access tokens ~120 seconds before expiry and rotates `refresh_token` under a file lock.

If you see `refresh_token_reused`, run `auth login` again. **Do not** use the same ChatGPT account with Codex CLI on the same machine at the same time — refresh tokens are single-use.

### Credentials storage

| Path | Contents |
|------|----------|
| `/data/auth.json` | OAuth tokens (volume `codex-data`) |

File mode `0600`, directory `0700`. Treat backups as secrets.

## Environment variables

Only secrets in `.env`:

| Variable | Description |
|----------|-------------|
| `PROXY_API_KEY` | Key for proxy clients (LiteLLM, curl) |
| `LITELLM_MASTER_KEY` | LiteLLM master key |

All other settings are constants in `internal/config/config.go`.

## Network egress

The proxy needs outbound HTTPS to `chatgpt.com` and `auth.openai.com`.

On datacenter/VPS IPs, Cloudflare may return **403** even with valid tokens. Mitigations: residential IP, VPN egress, or run on a home server. See [risks.md](./risks.md).

## Logs

```bash
docker compose logs -f openai-proxy
docker compose logs -f litellm
```

## Production checklist

- [ ] Unique `PROXY_API_KEY` and `LITELLM_MASTER_KEY`
- [ ] Persistent volume for `codex-data`
- [ ] Do not expose port `8080` publicly (only LiteLLM `:4000`)
- [ ] TLS termination in front of LiteLLM (nginx, Caddy)
- [ ] Monitor `readyz` and upstream 429 rate
- [ ] Plan for re-auth if refresh fails

Example compose file: [examples/docker-compose.yml](./examples/docker-compose.yml).
