#!/usr/bin/env bash
# llm-tooluse-probe.sh -- fire a single tool-enabled turn at a RUNNING cocola
# llm-gateway and show whether the model came back with a tool_use block.
#
# This is the live counterpart to the hermetic scripts/llm-tooluse-e2e.py: that
# one proves the gateway plumbing with a FakeUpstream; THIS one proves the real
# upstream (the proxy you pointed COCOLA_ANTHROPIC_BASE_URL at) actually emits
# tool_use through the gateway end to end (ADR-0010).
#
# Prereqs: a gateway already up, e.g.
#     COCOLA_LLM_PROVIDER=anthropic \
#     COCOLA_ANTHROPIC_BASE_URL=<your proxy> \
#     COCOLA_ANTHROPIC_API_KEY=<key> \
#     bash scripts/run-stack.sh --with-llm
#   (the banner prints a dev TOKEN -- export it as TOKEN below)
#
# Usage:
#   TOKEN=<dev-token> bash scripts/llm-tooluse-probe.sh
#
# Env knobs:
#   BASE_URL   gateway base url        (default http://127.0.0.1:8081)
#   MODEL      model alias to request  (default cocola-default)
#   TOKEN      bearer token            (required unless the gateway has auth off)
#   STREAM     1=SSE (default) 0=JSON
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:8081}"
MODEL="${MODEL:-cocola-default}"
STREAM="${STREAM:-1}"
TOKEN="${TOKEN:-}"

AUTH=()
[ -n "$TOKEN" ] && AUTH=(-H "authorization: Bearer $TOKEN")

# A prompt that all but forces a tool call, plus a tool the model can pick.
read -r -d '' BODY <<JSON || true
{
  "model": "$MODEL",
  "max_tokens": 256,
  "stream": $([ "$STREAM" = "1" ] && echo true || echo false),
  "tools": [
    {
      "name": "get_weather",
      "description": "Get the current weather for a city. Always use this for weather questions.",
      "input_schema": {
        "type": "object",
        "properties": {"city": {"type": "string", "description": "city name"}},
        "required": ["city"]
      }
    }
  ],
  "tool_choice": {"type": "auto"},
  "messages": [
    {"role": "user", "content": "What is the weather in Tokyo right now? Use the tool."}
  ]
}
JSON

echo "==> POST $BASE_URL/v1/messages  (model=$MODEL stream=$STREAM)"
echo "----------------------------------------------------------------------"

if [ "$STREAM" = "1" ]; then
  RAW="$(curl -sN -X POST "$BASE_URL/v1/messages" \
    "${AUTH[@]}" \
    -H "content-type: application/json" \
    -d "$BODY")"
  echo "$RAW"
  echo "----------------------------------------------------------------------"
  if echo "$RAW" | grep -q '"type":"tool_use"' || echo "$RAW" | grep -q '"type": "tool_use"'; then
    echo "PASS: upstream returned a tool_use block through the gateway"
    echo "$RAW" | grep -o '"name":[ ]*"[^"]*"' | head -1
    exit 0
  fi
  if echo "$RAW" | grep -q 'input_json_delta'; then
    echo "PASS: saw input_json_delta (tool args streaming) -- tool_use path live"
    exit 0
  fi
  echo "WARN: no tool_use in the stream. Either the model chose to answer in"
  echo "      text, or tools are still being dropped. Check the gateway logs"
  echo "      (.run-logs/llm-gateway.log) and that the upstream supports tools."
  exit 1
else
  RAW="$(curl -s -X POST "$BASE_URL/v1/messages" \
    "${AUTH[@]}" \
    -H "content-type: application/json" \
    -d "$BODY")"
  echo "$RAW" | python3 -m json.tool 2>/dev/null || echo "$RAW"
  echo "----------------------------------------------------------------------"
  if echo "$RAW" | grep -q '"type": "tool_use"' || echo "$RAW" | grep -q '"type":"tool_use"'; then
    echo "PASS: non-stream response contains a tool_use block"
    exit 0
  fi
  echo "WARN: no tool_use block in the JSON response (see above)."
  exit 1
fi
