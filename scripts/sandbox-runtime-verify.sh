#!/usr/bin/env bash
# Verify the Route-A sandbox runtime image end-to-end on a LOCAL Docker (runc)
# engine. This is the "prove it on Docker bind-mount first" gate from ADR-0008
# sec.7; the SAME script body is the future runsc (gVisor) acceptance spike --
# just point DOCKER_RUNTIME=runsc once a Linux+gVisor host is available.
#
# What it proves (ADR-0009 / ADR-0008):
#   1. build   : the image builds; CLI is pre-baked (offline tgz if vendored).
#   2. selfcheck: Node + Claude/Codex CLIs and SDKs present; runtime state dirs,
#                 workspace and common browser/document tooling wired. (no network)
#   3. query   : a real turn through the in-sandbox stdio shim -> reaches the
#                llm-gateway (egress) AND exercises native bash/file IO inside
#                the container. This is also the live TOOL-USE round-trip
#                (ADR-0010): the model can only write proof.txt by emitting a
#                tool_use that the gateway forwarded -- if tools were dropped the
#                file never appears. Requires ANTHROPIC_BASE_URL + ANTHROPIC_AUTH_TOKEN.
#   4. persist : session storage keeps /home/cocola/.claude and
#                /home/cocola/.codex across a container destroy + recreate;
#                Claude `--resume <session_id>` is checked after a live turn.
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
mkdir -p "$SESS_VOL/workspace" "$SESS_VOL/claude" "$SESS_VOL/codex" "$SESS_VOL/browser"

CTR="cocola-verify-$$"
PASS=0; FAIL=0
ok()   { echo "  PASS: $*"; PASS=$((PASS+1)); }
bad()  { echo "  FAIL: $*"; FAIL=$((FAIL+1)); }
note() { echo "==> $*"; }

python_string_constant() {
  python3 -c 'import ast, pathlib, sys; tree = ast.parse(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8")); print(next(node.value.value for node in tree.body if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == sys.argv[2] for target in node.targets) and isinstance(node.value, ast.Constant) and isinstance(node.value.value, str)), end="")' "$1" "$2"
}

cleanup() {
  docker rm -f "$CTR" >/dev/null 2>&1 || true
  rm -rf "$WORK" || true
}
trap cleanup EXIT

# Mounts shared by every container we spin up: the session workspace and both
# runtimes' native state directories.
run_args=(
  --rm -d --name "$CTR"
  --runtime "$DOCKER_RUNTIME"
  -v "$SESS_VOL/workspace:/workspace"
  -v "$SESS_VOL/claude:/home/cocola/.claude"
  -v "$SESS_VOL/codex:/home/cocola/.codex"
  -v "$SESS_VOL/browser:/session/runtime/browser"
  -e "CLAUDE_CONFIG_DIR=/home/cocola/.claude"
  -e "ANTHROPIC_CONFIG_DIR=/home/cocola/.claude"
  -e "CODEX_HOME=/home/cocola/.codex"
  -e "COCOLA_WORKSPACE=/workspace"
  -e "CLAUDE_CODE_MAX_RETRIES=${CLAUDE_CODE_MAX_RETRIES:-3}"
)
[ -n "${ANTHROPIC_BASE_URL:-}" ]   && run_args+=( -e "ANTHROPIC_BASE_URL=$ANTHROPIC_BASE_URL" )
[ -n "${ANTHROPIC_AUTH_TOKEN:-}" ] && run_args+=( -e "ANTHROPIC_AUTH_TOKEN=$ANTHROPIC_AUTH_TOKEN" )
[ -n "${MODEL:-}" ]                && run_args+=( -e "ANTHROPIC_MODEL=$MODEL" )
[ -n "${COCOLA_LLM_BASE_URL:-}" ]  && run_args+=( -e "COCOLA_LLM_BASE_URL=$COCOLA_LLM_BASE_URL" )
[ -n "${CODEX_API_KEY:-}" ]         && run_args+=( -e "CODEX_API_KEY=$CODEX_API_KEY" )
[ -n "${MODEL:-}" ]                 && run_args+=( -e "CODEX_MODEL=$MODEL" )

start_ctr() { docker run "${run_args[@]}" "$IMAGE" >/dev/null; }

# ---- 1. build ------------------------------------------------------------
if [ "${SKIP_BUILD:-0}" != "1" ]; then
  note "building $IMAGE (runtime target: $DOCKER_RUNTIME)"
  docker build -t "$IMAGE" "$CTX"
  ok "image built"
else
  note "SKIP_BUILD=1, reusing $IMAGE"
fi

IMAGE_WORKDIR="$(docker image inspect --format '{{.Config.WorkingDir}}' "$IMAGE")"
[ "$IMAGE_WORKDIR" = "/" ] \
  && ok "image uses stable / working directory for OpenSandbox execd" \
  || bad "image WorkingDir is $IMAGE_WORKDIR (must be /; /workspace is replaced at startup)"

# ---- 2. selfcheck (no network) ------------------------------------------
note "selfcheck: runtime components baked into the image"
start_ctr
SELF="$(docker exec -i "$CTR" /opt/cocola/shim/entrypoint.sh --selfcheck || true)"
echo "$SELF"
echo "$SELF" | grep -q '"node":"v2' && ok "Node 22+ present" || bad "Node missing / wrong major"
echo "$SELF" | grep -q '"claude_cli":"[0-9]' && ok "claude CLI pre-baked" || bad "claude CLI missing"
echo "$SELF" | grep -qv '"claude_agent_sdk":"missing' && ok "claude-agent-sdk importable" || bad "claude-agent-sdk missing"
echo "$SELF" | grep -q '"codex_cli":"codex-cli [0-9]' && ok "codex CLI pre-baked" || bad "codex CLI missing"
echo "$SELF" | grep -q '"codex_sdk":"0.144.1"' && ok "codex SDK pinned" || bad "codex SDK missing / wrong version"
if docker exec -i -u cocola "$CTR" sh -c \
  'test "$GOBIN" = /home/cocola/.local/bin &&
   test -w /home/cocola/.local/bin &&
   test -w /home/cocola/.local/lib/node_modules &&
   test -w /home/cocola/.local/share/pnpm &&
   test -w /home/cocola/.local/share/man'; then
  ok "guest package-manager prefix is initialized and writable"
else
  bad "guest package-manager prefix or GOBIN is not initialized"
fi
for tool in pnpm yarn playwright chromium fd jq yq tree file make imagemagick pdftotext rsvg_convert \
  gopls clangd shellcheck shfmt java; do
  if echo "$SELF" | grep -Eq "\"$tool\":\"(missing|error:)"; then
    bad "$tool missing from sandbox runtime"
  else
    ok "$tool present"
  fi
done

RUNTIME_INFO="$(docker exec -i "$CTR" cocola-sandbox info --json || true)"
echo "$RUNTIME_INFO"
echo "$RUNTIME_INFO" | grep -q '"schema_version": 1' \
  && ok "versioned runtime manifest is readable through cocola-sandbox" \
  || bad "cocola-sandbox runtime manifest probe failed"
echo "$RUNTIME_INFO" | grep -q '"profile": "coding"' \
  && ok "coding runtime profile is active by default" \
  || bad "unexpected default runtime profile"

EDITOR_EXTENSIONS_DIR="$(echo "$RUNTIME_INFO" | jq -r '.editor.extensions_dir // empty')"
EDITOR_EXTENSIONS_LOCK="$(echo "$RUNTIME_INFO" | jq -r '.editor.extensions_lock // empty')"
if [ "$EDITOR_EXTENSIONS_DIR" = "/opt/cocola/code-server/extensions" ] \
  && [ "$EDITOR_EXTENSIONS_LOCK" = "/opt/cocola/code-server-extensions.lock.json" ]; then
  EXPECTED_EXTENSIONS="$(docker exec -i "$CTR" jq -r \
    '.extensions[] | ((.id | ascii_downcase) + "@" + .version)' \
    "$EDITOR_EXTENSIONS_LOCK" | sort)"
  ACTUAL_EXTENSIONS="$(docker exec -i -u cocola \
    -e XDG_CONFIG_HOME=/tmp/cocola-code-server-verify-config \
    "$CTR" code-server \
    --user-data-dir /tmp/cocola-code-server-verify-data \
    --extensions-dir "$EDITOR_EXTENSIONS_DIR" --list-extensions --show-versions \
    | tr '[:upper:]' '[:lower:]' | sed '/^\[/d; /^$/d' | sort)"
  [ "$ACTUAL_EXTENSIONS" = "$EXPECTED_EXTENSIONS" ] \
    && ok "Code Server extension inventory matches the image lock" \
    || bad "Code Server extensions differ from lock: expected=[$EXPECTED_EXTENSIONS] actual=[$ACTUAL_EXTENSIONS]"
  docker exec -i -u cocola "$CTR" test ! -w "$EDITOR_EXTENSIONS_DIR" \
    && [ "$(docker exec -i "$CTR" stat -c '%U:%G' "$EDITOR_EXTENSIONS_DIR")" = "root:root" ] \
    && ok "platform Code Server extensions are root-owned and guest read-only" \
    || bad "platform Code Server extension directory is guest-writable or not root-owned"
else
  bad "runtime editor contract does not expose the platform extension lock"
fi

BUILTIN_SKILL_OWNER="$(docker exec -i "$CTR" stat -c '%U:%G' \
  /opt/cocola/skills/cocola-sandbox-browser/SKILL.md 2>/dev/null || true)"
BUILTIN_ARTIFACT_SKILL_OWNER="$(docker exec -i "$CTR" stat -c '%U:%G' \
  /opt/cocola/skills/cocola-sandbox-artifacts/SKILL.md 2>/dev/null || true)"
docker exec -i "$CTR" test -f /opt/cocola/skills/manifest.json \
  && docker exec -i "$CTR" test -s /opt/cocola/skills/cocola-sandbox-browser/SKILL.md \
  && docker exec -i "$CTR" test -s /opt/cocola/skills/cocola-sandbox-artifacts/SKILL.md \
  && docker exec -i "$CTR" test -s /opt/cocola/skills/cocola-github/SKILL.md \
  && ok "built-in Browser, Artifact, and GitHub Skills are baked into the runtime" \
  || bad "one or more built-in Sandbox Skills are missing"
[ "$BUILTIN_SKILL_OWNER" = "root:root" ] \
  && [ "$BUILTIN_ARTIFACT_SKILL_OWNER" = "root:root" ] \
  && ok "built-in Skills remain root-owned runtime assets" \
  || bad "built-in Skill owners are ${BUILTIN_SKILL_OWNER:-unknown}/${BUILTIN_ARTIFACT_SKILL_OWNER:-unknown} (must be root:root)"

GH_VERSION_OUTPUT="$(docker exec -i "$CTR" gh --version 2>/dev/null | head -1 || true)"
echo "$GH_VERSION_OUTPUT" | grep -q 'gh version 2.94.0' \
  && ok "GitHub CLI 2.94.0 is pinned in the runtime" \
  || bad "pinned GitHub CLI is unavailable: $GH_VERSION_OUTPUT"
if docker exec -i "$CTR" gh repo view >/dev/null 2>&1; then
  bad "gh authenticated without a Project Broker credential"
else
  ok "gh fails closed outside an authenticated GitHub Project run"
fi

WORKSPACE_INFO="$(docker exec -i "$CTR" cocola-sandbox workspace info --json || true)"
echo "$WORKSPACE_INFO" | grep -q '"outputs".*"exists": true' \
  && ok "workspace outputs contract is prepared" \
  || bad "workspace outputs contract missing"

ARTIFACT_STATUS="$(docker exec -u cocola "$CTR" cocola-sandbox artifact status --json || true)"
echo "$ARTIFACT_STATUS" | grep -q '"state": "ready"' \
  && ok "Artifact output capability is ready" \
  || bad "Artifact output capability is not ready: $ARTIFACT_STATUS"
docker exec -u cocola "$CTR" sh -c \
  'printf "%s" "<!doctype html><title>Cocola Artifact Probe</title><main>COCOLA_ARTIFACT_OK</main>" > /workspace/outputs/runtime-verify.html && ln -s /etc/hosts /workspace/outputs/ignored-link.txt'
ARTIFACT_LIST="$(docker exec -u cocola "$CTR" cocola-sandbox artifact list --json || true)"
echo "$ARTIFACT_LIST" | grep -q '"path": "runtime-verify.html"' \
  && ! echo "$ARTIFACT_LIST" | grep -q 'ignored-link.txt' \
  && ok "Artifact inventory lists regular outputs and ignores symbolic links" \
  || bad "Artifact inventory contract failed: $ARTIFACT_LIST"

CODE_SERVER_READY=0
for _ in {1..10}; do
  SERVICE_INFO="$(docker exec -i "$CTR" cocola-sandbox service status --json || true)"
  if echo "$SERVICE_INFO" | grep -q '"state": "ready"'; then
    CODE_SERVER_READY=1
    break
  fi
  sleep 1
done
[ "$CODE_SERVER_READY" = "1" ] \
  && ok "supervisor reports resident code-server ready" \
  || bad "resident code-server did not become ready: ${SERVICE_INFO:-no status}"

# ---- 2b. on-demand Browser capability (local HTTP only; no external network) -
note "browser: one-shot inspect, screenshot, and PDF without a resident port"
docker exec -i "$CTR" sh -c \
  'printf "%s" "<!doctype html><title>Cocola Browser Probe</title><main>COCOLA_BROWSER_OK <span id=state></span></main><a href=\"/next\">Next</a><script>const key=\"cocola-browser-probe\";document.getElementById(\"state\").textContent=localStorage.getItem(key)||\"COCOLA_BROWSER_FIRST\";localStorage.setItem(key,\"COCOLA_BROWSER_PERSISTED\")</script>" > /workspace/browser-probe.html'
docker exec -d -u cocola "$CTR" \
  python3 -m http.server 39400 --bind 127.0.0.1 --directory /workspace
for _ in {1..10}; do
  if docker exec -u cocola "$CTR" curl -fsS http://127.0.0.1:39400/browser-probe.html >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

BROWSER_STATUS="$(docker exec -u cocola "$CTR" cocola-sandbox browser status --json || true)"
echo "$BROWSER_STATUS" | grep -q '"state": "ready"' \
  && ok "coding profile exposes the on-demand Browser capability" \
  || bad "Browser capability is not ready: $BROWSER_STATUS"

BROWSER_INSPECT="$(docker exec -u cocola "$CTR" cocola-sandbox browser inspect \
  http://127.0.0.1:39400/browser-probe.html --json || true)"
echo "$BROWSER_INSPECT" | grep -q 'COCOLA_BROWSER_OK' \
  && ok "Browser inspect returns rendered page text" \
  || bad "Browser inspect failed: $BROWSER_INSPECT"

BROWSER_SCREENSHOT="$(docker exec -u cocola "$CTR" cocola-sandbox browser screenshot \
  http://127.0.0.1:39400/browser-probe.html --output verify.png --json || true)"
echo "$BROWSER_SCREENSHOT" | grep -q '"mime_type": "image/png"' \
  && docker exec -u cocola "$CTR" test -s /workspace/outputs/browser/verify.png \
  && ok "Browser screenshot writes a non-empty Workspace PNG" \
  || bad "Browser screenshot failed: $BROWSER_SCREENSHOT"

BROWSER_PDF="$(docker exec -u cocola "$CTR" cocola-sandbox browser pdf \
  http://127.0.0.1:39400/browser-probe.html --output verify.pdf --json || true)"
echo "$BROWSER_PDF" | grep -q '"mime_type": "application/pdf"' \
  && docker exec -u cocola "$CTR" test -s /workspace/outputs/browser/verify.pdf \
  && ok "Browser PDF writes a non-empty Workspace document" \
  || bad "Browser PDF failed: $BROWSER_PDF"

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
# Markers that should outlive the container in both native state directories.
docker exec -i "$CTR" bash -lc 'echo cocola-persist-marker > /home/cocola/.claude/persist_probe.txt'
docker exec -i "$CTR" bash -lc 'echo cocola-persist-marker > /home/cocola/.codex/persist_probe.txt'
docker rm -f "$CTR" >/dev/null
ls "$SESS_VOL/claude/persist_probe.txt" >/dev/null 2>&1 && ok "session storage retained .claude file on host after destroy" || bad "session storage lost .claude data"
ls "$SESS_VOL/codex/persist_probe.txt" >/dev/null 2>&1 && ok "session storage retained .codex file on host after destroy" || bad "session storage lost .codex data"
find "$SESS_VOL/browser/profile" -mindepth 1 -print -quit 2>/dev/null | grep -q . \
  && ok "session storage retained Browser profile on host after destroy" \
  || bad "session storage lost Browser profile data"

start_ctr
docker exec -i "$CTR" cat /home/cocola/.claude/persist_probe.txt 2>/dev/null | grep -q cocola-persist-marker \
  && ok "re-created container re-mounts the same /home/cocola/.claude" || bad "remount did not restore /home/cocola/.claude"
docker exec -i "$CTR" cat /home/cocola/.codex/persist_probe.txt 2>/dev/null | grep -q cocola-persist-marker \
  && ok "re-created container re-mounts the same /home/cocola/.codex" || bad "remount did not restore /home/cocola/.codex"
docker exec -d -u cocola "$CTR" \
  python3 -m http.server 39400 --bind 127.0.0.1 --directory /workspace
for _ in {1..10}; do
  if docker exec -u cocola "$CTR" curl -fsS http://127.0.0.1:39400/browser-probe.html >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
BROWSER_RESTORED="$(docker exec -u cocola "$CTR" cocola-sandbox browser inspect \
  http://127.0.0.1:39400/browser-probe.html --json || true)"
echo "$BROWSER_RESTORED" | grep -q 'COCOLA_BROWSER_PERSISTED' \
  && ok "re-created container restores Browser local state" \
  || bad "Browser profile state was not restored: $BROWSER_RESTORED"

# Exercise the exact Agent Runtime scripts against the built image. An empty
# Admin/Personal catalog must still reconcile the image-baked platform Skill
# into the shared Claude/Codex Session Skill Set.
note "skills: reconcile image-baked platform Skill into both Agent runtimes"
SKILL_RECONCILER="$ROOT/apps/agent-runtime/cocola_agent_runtime/skill_reconciler.py"
SKILLS_INSPECT_SCRIPT="$(python_string_constant "$SKILL_RECONCILER" SKILLS_INSPECT_SCRIPT)"
SKILLS_RECONCILE_SCRIPT="$(python_string_constant "$SKILL_RECONCILER" SKILLS_RECONCILE_SCRIPT)"
SKILL_DIGEST="runtime-verify-empty-catalog"
SKILL_ARCHIVE="/tmp/cocola-runtime-verify-skills.zip"
SKILL_AVAILABLE="$(docker exec -i -u cocola "$CTR" python3 -c "$SKILLS_INSPECT_SCRIPT" || true)"
echo "$SKILL_AVAILABLE" | grep -q '"id":"cocola-sandbox-browser"' \
  && echo "$SKILL_AVAILABLE" | grep -q '"id":"cocola-sandbox-artifacts"' \
  && ok "Agent Runtime discovers both image platform Skills" \
  || bad "Agent Runtime did not discover both platform Skills: $SKILL_AVAILABLE"
docker exec -i -u cocola "$CTR" python3 -c \
  'import json, sys, zipfile; archive = zipfile.ZipFile(sys.argv[1], "w"); archive.writestr("manifest.json", json.dumps({"format": "session-bundle-v2", "digest": sys.argv[2], "skills": []})); archive.close()' \
  "$SKILL_ARCHIVE" "$SKILL_DIGEST"
docker exec -i -u cocola "$CTR" python3 -c "$SKILLS_RECONCILE_SCRIPT" \
  "$SKILL_ARCHIVE" "$SKILL_DIGEST"
docker exec -i -u cocola "$CTR" test -s \
  /home/cocola/.claude/skills/cocola-sandbox-browser/SKILL.md \
  && docker exec -i -u cocola "$CTR" test -s \
    /home/cocola/.agents/skills/cocola-sandbox-browser/SKILL.md \
  && docker exec -i -u cocola "$CTR" test -s \
    /home/cocola/.claude/skills/cocola-sandbox-artifacts/SKILL.md \
  && docker exec -i -u cocola "$CTR" test -s \
    /home/cocola/.agents/skills/cocola-sandbox-artifacts/SKILL.md \
  && ok "empty catalog still loads both built-in Skills for Claude and Codex" \
  || bad "built-in Skills were not loaded into both Agent runtime directories"

# resume only if we got a session id from step 3
if [ -n "$SESSION_ID" ]; then
  note "resume: rebuild the brain from the on-disk session ($SESSION_ID)"
  REQ2="$(printf '{"prompt":"What filename did you just create in the previous turn?","resume":"%s","max_turns":4}' "$SESSION_ID")"
  OUT2="$(printf '%s' "$REQ2" | docker exec -i "$CTR" /opt/cocola/shim/entrypoint.sh || true)"
  echo "$OUT2" | tail -10
  echo "$OUT2" | grep -qi 'proof.txt' && ok "--resume restored prior session context" || bad "resume lost prior context"
fi

# A resident editor crash after RUNNING must remain visible as EXITED. Initial
# startup retries are bounded separately by supervisor's startretries=3.
note "lifecycle: a running Code Server crash does not enter an unbounded restart loop"
CODE_SERVER_RUNNING=0
for _ in {1..10}; do
  CODE_SERVER_BEFORE_CRASH="$(docker exec -i "$CTR" supervisorctl \
    -c /opt/cocola/supervisord.conf status code-server 2>/dev/null || true)"
  if echo "$CODE_SERVER_BEFORE_CRASH" | grep -q 'RUNNING'; then
    CODE_SERVER_RUNNING=1
    break
  fi
  sleep 1
done
CODE_SERVER_PID="$(docker exec -i "$CTR" supervisorctl -c /opt/cocola/supervisord.conf \
  pid code-server 2>/dev/null || true)"
if [ "$CODE_SERVER_RUNNING" = "1" ] && [[ "$CODE_SERVER_PID" =~ ^[1-9][0-9]*$ ]]; then
  docker exec -i "$CTR" kill -KILL "$CODE_SERVER_PID"
  sleep 3
  CODE_SERVER_AFTER_CRASH="$(docker exec -i "$CTR" supervisorctl \
    -c /opt/cocola/supervisord.conf status code-server 2>/dev/null || true)"
  echo "$CODE_SERVER_AFTER_CRASH" | grep -q 'EXITED' \
    && ok "Code Server remains EXITED after an unexpected running-state crash" \
    || bad "Code Server restarted after crash: $CODE_SERVER_AFTER_CRASH"
else
  bad "could not resolve RUNNING Code Server pid: ${CODE_SERVER_PID:-empty}; status=${CODE_SERVER_BEFORE_CRASH:-empty}"
fi

# ---- summary -------------------------------------------------------------
echo
note "RESULT: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
