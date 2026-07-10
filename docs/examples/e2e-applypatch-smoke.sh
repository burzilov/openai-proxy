#!/usr/bin/env bash
# E2E smoke: ApplyPatch via openai-proxy (and optionally LiteLLM).
#
# Prerequisites:
#   - openai-proxy running with Codex OAuth (readyz=200)
#   - PROXY_API_KEY / LITELLM_MASTER_KEY in env
#
# Usage:
#   ./docs/examples/e2e-applypatch-smoke.sh              # direct :8080
#   ./docs/examples/e2e-applypatch-smoke.sh litellm      # via :4000
set -euo pipefail

MODE="${1:-direct}"
if [[ "$MODE" == "litellm" ]]; then
  BASE="${LITELLM_BASE:-http://localhost:4000/v1}"
  KEY="${LITELLM_MASTER_KEY:?LITELLM_MASTER_KEY required}"
  MODEL="${LITELLM_MODEL:-codex-gpt-5.4}"
else
  BASE="${PROXY_BASE:-http://localhost:8080/v1}"
  KEY="${PROXY_API_KEY:?PROXY_API_KEY required}"
  MODEL="${PROXY_MODEL:-gpt-5.4}"
fi

TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT

payload_stream() {
  cat <<EOF
{
  "model": "$MODEL",
  "stream": true,
  "messages": [
    {"role": "user", "content": "Create file /tmp/openai-proxy-e2e.txt with content hello-e2e using ApplyPatch only. No prose."}
  ],
  "tools": [
    {
      "type": "custom",
      "name": "ApplyPatch",
      "description": "Apply a patch",
      "format": {
        "type": "grammar",
        "syntax": "lark",
        "definition": "start: ANY\\nANY: /.+/s"
      }
    }
  ]
}
EOF
}

payload_nostream() {
  cat <<EOF
{
  "model": "$MODEL",
  "stream": false,
  "messages": [
    {"role": "user", "content": "Create file /tmp/openai-proxy-e2e2.txt with content hello using ApplyPatch only."}
  ],
  "tools": [
    {
      "type": "custom",
      "name": "ApplyPatch",
      "format": {
        "type": "grammar",
        "syntax": "lark",
        "definition": "start: ANY\\nANY: /.+/s"
      }
    }
  ]
}
EOF
}

echo "POST $BASE/chat/completions (stream) model=$MODEL"
curl -sS -N "$BASE/chat/completions" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d "$(payload_stream)" | tee "$TMP"

echo
echo "--- checks ---"
if ! grep -q 'tool_calls' "$TMP"; then
  echo "FAIL: no tool_calls in SSE"
  exit 1
fi
if ! grep -q 'ApplyPatch' "$TMP"; then
  echo "FAIL: ApplyPatch name missing"
  exit 1
fi
if grep -q 'Begin Patch' "$TMP" || grep -q 'arguments' "$TMP"; then
  echo "OK: saw tool call payload fragments"
else
  echo "FAIL: no arguments/Begin Patch in stream"
  exit 1
fi

if [[ "$MODE" == "litellm" ]]; then
  echo
  echo "Non-stream aggregate via LiteLLM:"
  AGG="$(curl -sS "$BASE/chat/completions" \
    -H "Authorization: Bearer $KEY" \
    -H "Content-Type: application/json" \
    -d "$(payload_nostream)")"
  echo "$AGG" | head -c 2000
  echo
  if echo "$AGG" | grep -qE '"tool_calls":[[:space:]]*null'; then
    echo "FAIL: aggregated tool_calls is null"
    exit 1
  fi
  if ! echo "$AGG" | grep -q 'ApplyPatch'; then
    echo "FAIL: aggregated response missing ApplyPatch"
    exit 1
  fi
  echo "OK: LiteLLM aggregate has ApplyPatch tool_calls"
fi

echo "PASS"
