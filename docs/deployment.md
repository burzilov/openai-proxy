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

All other Codex-serve settings are constants in `internal/config/config.go`.

## FreeLLMAPI gateway

Cursor’s **Override OpenAI Base URL** may send `tools` with `type: "custom"` and `reasoning` as a string. FreeLLMAPI validates those fields strictly and returns HTTP 400 before any custom endpoint (including this proxy on `:8080`) runs.

`openai-proxy gateway` is a separate listener that:

1. Rewrites `reasoning: "medium"` → `reasoning: {"effort":"medium"}`
2. Rewrites `type: "custom"` tools into `type: "function"` wrappers (by `name` / `custom.name`)
3. Reverse-proxies all other traffic to FreeLLMAPI unchanged (including `/v1/models` and streams)

Defaults:

| Variable | Default | Meaning |
|----------|---------|---------|
| `GATEWAY_LISTEN` | `:3002` | Listen address on the FreeLLMAPI host |
| `GATEWAY_UPSTREAM` | `http://127.0.0.1:3001` | FreeLLMAPI origin |

### Install without sources on the host

The host does not need a Go toolchain or a git checkout. Build a static Linux binary elsewhere, copy it, enable systemd:

```bash
# on a build machine (linux/amd64):
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags='-s -w' -o openai-proxy ./cmd/openai-proxy

# copy to the FreeLLMAPI host:
scp openai-proxy root@llm:/usr/local/bin/openai-proxy
ssh root@llm 'chmod +x /usr/local/bin/openai-proxy'

# on the FreeLLMAPI host — autostart:
curl -fsSL -o /etc/systemd/system/openai-proxy-gateway.service \
  https://raw.githubusercontent.com/burzilov/openai-proxy/main/docs/examples/openai-proxy-gateway.service
systemctl daemon-reload
systemctl enable --now openai-proxy-gateway.service
systemctl status openai-proxy-gateway.service --no-pager
```

Or run once without systemd:

```bash
GATEWAY_LISTEN=:3002 GATEWAY_UPSTREAM=http://127.0.0.1:3001 /usr/local/bin/openai-proxy gateway
```

Then point **Nginx Proxy Manager** at `http://<fla-host>:3002` instead of `:3001`. Keep Cursor Override URL as `https://llm.example/v1` — only the NPM upstream port changes.

If you already run `openai-proxy serve` on `:8080`, replace that same binary with a build that includes `gateway`, then add the second unit above (both commands share one binary).

`gateway` does **not** use `PROXY_API_KEY` / Codex OAuth; FreeLLMAPI still authenticates with its own key. The Codex `serve` process on `:8080` stays as FreeLLMAPI’s custom endpoint.

Systemd unit example: [examples/openai-proxy-gateway.service](./examples/openai-proxy-gateway.service).

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
