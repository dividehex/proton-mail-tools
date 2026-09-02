#!/usr/bin/env bash
# Exercise the read-only tools against a running instance.
# Usage: scripts/smoke.sh [base_url] [api_key]
set -euo pipefail
BASE="${1:-http://127.0.0.1:8930}"
KEY="${2:-${PROTON_TOOLS_API_KEY:-}}"
AUTH=()
[[ -n "$KEY" ]] && AUTH=(-H "Authorization: Bearer $KEY")

echo "== health";    curl -sf "$BASE/health"; echo
echo "== openapi";   curl -sf "$BASE/openapi.json" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(sorted(op["operationId"] for p in d["paths"].values() for op in p.values()))'
echo "== mailboxes"; curl -sf "${AUTH[@]}" "$BASE/mailboxes" | python3 -m json.tool
echo "== search INBOX (limit 3)"
curl -sf "${AUTH[@]}" -H 'Content-Type: application/json' -d '{"limit":3}' "$BASE/messages/search" | python3 -m json.tool
