#!/usr/bin/env bash
# Verify the Route-A sandbox runtime image end-to-end on a LOCAL Docker (runc)
# engine. This is the "prove it on Docker bind-mount first" gate from ADR-0008
# sec.7; the SAME script body is the future runsc (gVisor) acceptance spike --
# just point DOCKER_RUNTIME=runsc once a Linux+gVisor host is available.
#
# What it proves (ADR-0009 / ADR-0008):
#   1. build   : the image builds; CLI is pre-baked (offline tgz if vendored).
#   2. selfcheck: Node + claude CLI + claude-agent-sdk present; config_dir /
#                 workspace / auth env wired, plus common browser/document
#                 tooling baked in. (no network)
#   3. query   : a real turn through the in-sandbox stdio shim -> reaches the
#                llm-gateway (egress) AND exercises native bash/file IO inside
#                the container. This is also the live TOOL-USE round-trip
#                (ADR-0010): the model can only write proof.txt by emitting a
#                tool_use that the gateway forwarded -- if tools were dropped the
#                file never appears. Requires ANTHROPIC_BASE_URL + ANTHROPIC_AUTH_TOKEN.
#   4. persist : the session storage keeps /home/cocola/.claude across a
#                container destroy + recreate, and `--resume <session_id>`
#                restores the session.
#
# The shim is driven over `docker exec -i` STDIO -- never a listening port.
#
# Usage:
#   scripts/sandbox-runtime-verify.sh                  # build + selfcheck + persist
#   ANTHROPIC_BASE_URL=... ANTHROPIC_AUTH_TOKEN=... scripts/sandbox-runtime-verify.sh
#
# Env knobs:
#   IMAGE           image tag to build/use      (default cocola/sandbox-runtime:dev)
#   DOCKER_RUNTIME  container runtime           (default runc; set runsc for gVisor)
#   MODEL           model alias for the query turn (default cocola-default)
#   SKIP_BUILD=1    reuse an existing image
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CTX="$ROOT/deploy/sandbox-runtime"
IMAGE="${IMAGE:-cocola/sandbox-runtime:dev}"
DOCKER_RUNTIME="${DOCKER_RUNTIME:-runc}"
MODEL="${MODEL:-cocola-default}"

# Per-test scratch dir that stands in for the session workspace volume.
WORK="$(mktemp -d)"
SESS_VOL="$WORK/session"   # session root; subdirs emulate session volume subPaths
mkdir -p "$SESS_VOL/workspace" "$SESS_VOL/claude"

CTR="cocola-verify-$$"
PASS=0; FAIL=0
ok()   { echo "  PASS: $*"; PASS=$((PASS+1)); }
bad()  { echo "  FAIL: $*"; FAIL=$((FAIL+1)); }
note() { echo "==> $*"; }

cleanup() {
  docker rm -f "$CTR" >/dev/null 2>&1 || true
  rm -rf "$WORK" || true
}
trap cleanup EXIT

# Mounts shared by every container we spin up: the session workspace volume,
# plus the env that redirects Claude Code at the cocola gateway (ADR-0009 sec.2).
run_args=(
  --rm -d --name "$CTR"
  --runtime "$DOCKER_RUNTIME"
  -v "$SESS_VOL/workspace:/workspace"
  -v "$SESS_VOL/claude:/home/cocola/.claude"
  -e "CLAUDE_CONFIG_DIR=/home/cocola/.claude"
  -e "ANTHROPIC_CONFIG_DIR=/home/cocola/.claude"
  -e "COCOLA_WORKSPACE=/workspace"
  -e "CLAUDE_CODE_MAX_RETRIES=${CLAUDE_CODE_MAX_RETRIES:-3}"
)
[ -n "${ANTHROPIC_BASE_URL:-}" ]   && run_args+=( -e "ANTHROPIC_BASE_URL=$ANTHROPIC_BASE_URL" )
[ -n "${ANTHROPIC_AUTH_TOKEN:-}" ] && run_args+=( -e "ANTHROPIC_AUTH_TOKEN=$ANTHROPIC_AUTH_TOKEN" )
[ -n "${MODEL:-}" ]                && run_args+=( -e "ANTHROPIC_MODEL=$MODEL" )

start_ctr() { docker run "${run_args[@]}" "$IMAGE" >/dev/null; }

# ---- 1. build ------------------------------------------------------------
if [ "${SKIP_BUILD:-0}" != "1" ]; then
  note "building $IMAGE (runtime target: $DOCKER_RUNTIME)"
  docker build -t "$IMAGE" "$CTX"
  ok "image built"
else
  note "SKIP_BUILD=1, reusing $IMAGE"
fi

# ---- 2. selfcheck (no network) ------------------------------------------
note "selfcheck: runtime components baked into the image"
start_ctr
SELF="$(docker exec -i "$CTR" /opt/cocola/shim/entrypoint.sh --selfcheck || true)"
echo "$SELF"
echo "$SELF" | grep -q '"node":"v2' && ok "Node 22+ present" || bad "Node missing / wrong major"
echo "$SELF" | grep -q '"claude_cli":"[0-9]' && ok "claude CLI pre-baked" || bad "claude CLI missing"
echo "$SELF" | grep -qv '"claude_agent_sdk":"missing' && ok "claude-agent-sdk importable" || bad "claude-agent-sdk missing"
for tool in pnpm yarn playwright chromium fd jq yq tree file make imagemagick pdftotext rsvg_convert; do
  if echo "$SELF" | grep -Eq "\"$tool\":\"(missing|error:)"; then
    bad "$tool missing from sandbox runtime"
  else
    ok "$tool present"
  fi
done

# ---- 3. real query: egress + native bash/file IO -------------------------
if [ -n "${ANTHROPIC_BASE_URL:-}" ] && [ -n "${ANTHROPIC_AUTH_TOKEN:-}" ]; then
  note "live turn: reach the gateway AND run native bash/file IO in-sandbox"
  note "  (this is the end-to-end TOOL-USE round-trip -- ADR-0010)"
  REQ='{"prompt":"Use the Bash tool to write the text COCOLA_OK into /workspace/proof.txt, then read it back and tell me its contents.","max_turns":8}'
  OUT="$(printf '%s' "$REQ" | docker exec -i "$CTR" /opt/cocola/shim/entrypoint.sh || true)"
  echo "$OUT" | tail -20
  SESSION_ID="$(echo "$OUT" | grep '"type":"done"' | sed -n 's/.*"session_id":"\([^"]*\)".*/\1/p' | head -1)"
  echo "$OUT" | grep -q '"type":"result"' && ok "model turn produced a result event" || bad "no result event (gateway/egress?)"
  # The shim surfaces tool activity as tool_use / tool events in its NDJSON. If
  # the gateway had dropped `tools` (the M3 regression), the model could never
  # call Bash and we'd see neither a tool_use event nor the file.
  if echo "$OUT" | grep -Eq '"(tool_use|tool)"|"name":[ ]*"Bash"'; then
    ok "shim stream shows a tool_use turn (tools survived the gateway)"
  else
    bad "no tool_use in the shim stream (tools dropped at the gateway?)"
  fi
  if docker exec -i "$CTR" cat /workspace/proof.txt 2>/dev/null | grep -q COCOLA_OK; then
    ok "native bash wrote proof.txt inside the sandbox (tool_use executed end to end)"
  else
    bad "proof.txt not written by native bash"
  fi
  [ -n "$SESSION_ID" ] && ok "captured session_id=$SESSION_ID" || note "no session_id captured (skipping resume test)"
else
  note "no gateway env: skipping live model turn + resume"
  SESSION_ID=""
fi

# ---- 4. persistence across container destroy + recreate ------------------
note "persistence: session storage survives container teardown"
# A marker that should outlive the container, written to the hidden session-local Claude config.
docker exec -i "$CTR" bash -lc 'echo cocola-persist-marker > /home/cocola/.claude/persist_probe.txt'
docker rm -f "$CTR" >/dev/null
ls "$SESS_VOL/claude/persist_probe.txt" >/dev/null 2>&1 && ok "session storage retained .claude file on host after destroy" || bad "session storage lost .claude data"

start_ctr
docker exec -i "$CTR" cat /home/cocola/.claude/persist_probe.txt 2>/dev/null | grep -q cocola-persist-marker \
  && ok "re-created container re-mounts the same /home/cocola/.claude" || bad "remount did not restore /home/cocola/.claude"

# resume only if we got a session id from step 3
if [ -n "$SESSION_ID" ]; then
  note "resume: rebuild the brain from the on-disk session ($SESSION_ID)"
  REQ2="$(printf '{"prompt":"What filename did you just create in the previous turn?","resume":"%s","max_turns":4}' "$SESSION_ID")"
  OUT2="$(printf '%s' "$REQ2" | docker exec -i "$CTR" /opt/cocola/shim/entrypoint.sh || true)"
  echo "$OUT2" | tail -10
  echo "$OUT2" | grep -qi 'proof.txt' && ok "--resume restored prior session context" || bad "resume lost prior context"
fi

# ---- summary -------------------------------------------------------------
echo
note "RESULT: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
