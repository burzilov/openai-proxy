# Risks and limitations

> **Summary:** This is unofficial software. You are responsible for compliance with OpenAI's terms and for any consequences of use.

## Legal and product

### Unofficial API

`chatgpt.com/backend-api/codex` is an **internal** ChatGPT/Codex endpoint, not a documented public API.

- The OAuth `client_id` belongs to Codex CLI
- The `originator: codex_cli_rs` header mimics the official client
- OpenAI may change the protocol, revoke the client ID, or restrict your account

**Risk:** ChatGPT ToS violation, account restriction. Use for personal or internal purposes only, with full awareness of the risk.

### ChatGPT subscription vs API billing

OAuth grants access to **subscription** quota (Plus/Pro/Team), not OpenAI Platform API credits. These are separate billing systems.

## Technical

### Cloudflare on datacenter IPs

The Codex endpoint is behind Cloudflare. On VPS/datacenter IPs you may get **403** (`cf-mitigated: challenge`) even with valid tokens and correct headers.

Mitigations:

- Residential IP
- VPN egress with a home IP
- Run on a home server

Headers alone are necessary but not always sufficient.

### Refresh token rotation

OAuth refresh tokens are **single-use**. If these run concurrently with the same account:

- openai-proxy
- Codex CLI (`codex login`)
- VS Code Codex extension

…you may get `refresh_token_reused` and need `auth login` again.

**Rule:** one owner of the refresh token per account.

### Device code restrictions

Team/Enterprise admins can disable device code auth. Then `auth login` will not work.

### Rate limits

| Code | Meaning | Action |
|------|---------|--------|
| 429 on login | OpenAI throttle | Wait and retry |
| 429 on inference | Subscription usage limit | Wait for reset; do not re-auth |
| 401 | Dead token | `auth login` |

### Model catalog

The Codex model list changes. Use live discovery via `GET /v1/models` rather than hardcoding.

## Operational

### Secrets

| Secret | Location | Severity |
|--------|----------|----------|
| `refresh_token` | `/data/auth.json` | Critical |
| `PROXY_API_KEY` | `.env` | High |
| `LITELLM_MASTER_KEY` | `.env` | High |

Do not commit, log, or share. Encrypt volume backups.

### Availability

The proxy is a single point of failure for Codex auth. If it goes down, LiteLLM returns 502/401 and clients cannot reach Codex without re-authentication elsewhere.

### Monitoring (recommended)

- `readyz` — alert if `codex_auth: false` for > 5 minutes
- Upstream 429 rate
- P95 latency on `/v1/chat/completions`
- Stream disconnect rate

## Official alternative

The [OpenAI API](https://platform.openai.com/) with an API key and usage-based billing is the stable path for production SaaS. This proxy is for using an **existing ChatGPT subscription** through an OpenAI-compatible interface.
