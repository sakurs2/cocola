"""InSandboxShimProvider -- the Route A agent provider (ADR-0009).

Where the legacy central-SDK path (Route B, decommissioned) spawned the `claude`
CLI *on the agent-runtime host* and forwarded only bash/file tools into a sandbox,
this provider runs the WHOLE Claude Code brain inside the user's own container. It
does that by driving the in-sandbox stdio shim
(`/opt/cocola/shim/entrypoint.sh`, see deploy/sandbox-runtime/shim/agent_shim.py):

    stdin   <- one JSON Request {prompt, system_prompt?, max_turns, resume?}
    stdout  -> a stream of NDJSON events, one compact JSON object per line,
               terminated by a final {"type":"done", "session_id": ...}

agent-runtime thus degrades to a control-plane router: it resolves the bound
sandbox (already done upstream in server.py via the binder), pushes the prompt
to the shim over a *streaming* exec, and relays each NDJSON line back as a
generic `AgentEvent`. It no longer owns the agent loop, the context, or the
tools -- all of that lives next to the user's files, isolated by construction.

The streaming exec is essential: the shim emits events live (text deltas, each
tool_use, tool_result, ...) and we must relay them as they arrive, not wait for
the turn to finish. We therefore consume `SandboxExecutor.exec_stream` and
reassemble whole NDJSON lines from byte chunks that may split mid-line.

Security note (ADR-0009 sec.2): credentials (ANTHROPIC_BASE_URL / AUTH_TOKEN /
CLAUDE_CONFIG_DIR) are injected into the sandbox ENV at creation time, never
through the prompt channel -- so this provider never puts secrets in the Request.
"""

from __future__ import annotations

import json
import re
import secrets
import time
from collections.abc import AsyncIterator
from dataclasses import dataclass, field

from cocola_common import get_logger

from cocola_agent_runtime.agent_provider import AgentEvent, AgentOptions
from cocola_agent_runtime.sandbox_binder import SandboxExecutor
from cocola_agent_runtime.session_map import MemorySessionMap, SessionMap

log = get_logger("cocola.agent-runtime.shim")

# Absolute path of the shim launcher baked into the Route A sandbox image
# (deploy/sandbox-runtime/Dockerfile). Driven over `docker exec -i` / `kubectl
# exec -i` STDIO -- never a listening port.
SHIM_ENTRYPOINT = "/opt/cocola/shim/entrypoint.sh"

# Substrings the claude CLI / Agent SDK emit when asked to `--resume` a session
# id that has no on-disk conversation. The session_map is a pure INDEX (it only
# records which id to reopen); the SUFFICIENT condition for resume is the
# on-disk ~/.claude session file (ADR-0008). When the sandbox is fresh/recycled
# or the file was GC'd, a stored id goes dangling and the shim exits non-zero
# with one of these markers, surfacing an opaque "shim exited 1" to the user.
# Matching one lets the provider degrade gracefully: forget the stale id and
# retry the SAME turn as a fresh conversation (no --resume) -- see query().
_RESUME_NOT_FOUND_MARKERS = (
    "no conversation found with session id",
    "no conversation found",
    "session id not found",
    "could not find session",
    "no session found",
    "session not found",
)


def _looks_like_resume_not_found(text: str) -> bool:
    """True when *text* (a shim error / stderr tail) signals a dangling resume id."""
    if not text:
        return False
    low = text.lower()
    return any(marker in low for marker in _RESUME_NOT_FOUND_MARKERS)


_SANDBOX_TIMEOUT_RE = re.compile(
    r"(context deadline exceeded|deadlineexceeded|timed out|timeout)",
    re.IGNORECASE,
)


def _user_facing_exec_error(raw: str) -> str:
    """Map low-level sandbox transport errors to text safe to show users."""
    text = (raw or "").strip()
    if _SANDBOX_TIMEOUT_RE.search(text):
        return (
            "工具执行超时：这次沙箱里的命令运行太久了，可能是网页加载、浏览器截图或脚本没有在"
            "限定时间内结束。请缩小任务范围，或让浏览器脚本设置较短 timeout、使用 "
            "waitUntil='domcontentloaded'，并确保最后关闭浏览器。"
        )
    return f"sandbox exec failed: {text or 'unknown error'}"


def _shim_event_to_agent_events(ev: dict) -> list[AgentEvent]:
    """Map one shim NDJSON event to zero or more AgentEvents.

    Taxonomy is kept identical to the sandbox shim protocol so the gateway/web SSE
    layer consumes both providers the same way. `start` is shim-internal framing
    and carries nothing the caller needs, so it is dropped.
    """
    t = ev.get("type")
    if t == "text":
        return [AgentEvent(kind="text", data={"text": ev.get("text", "")})]
    if t == "thinking":
        return [AgentEvent(kind="thinking", data={"thinking": ev.get("text", "")})]
    if t == "tool_use":
        return [
            AgentEvent(
                kind="tool_use",
                data={
                    "id": ev.get("id", ""),
                    "name": ev.get("name", ""),
                    "input": ev.get("input", {}),
                },
            )
        ]
    if t == "tool_result":
        return [
            AgentEvent(
                kind="tool_result",
                data={
                    "tool_use_id": ev.get("tool_use_id", ""),
                    "is_error": bool(ev.get("is_error", False)),
                    "content": ev.get("content", ""),
                },
            )
        ]
    if t == "result":
        return [
            AgentEvent(
                kind="result",
                data={
                    "is_error": bool(ev.get("is_error", False)),
                    "num_turns": ev.get("num_turns"),
                    "total_cost_usd": ev.get("total_cost_usd"),
                    "session_id": ev.get("session_id"),
                    "result": ev.get("result"),
                },
            )
        ]
    if t == "system":
        return [AgentEvent(kind="system", data={"subtype": ev.get("subtype", "")})]
    if t == "environment_status":
        components = ev.get("components") if isinstance(ev.get("components"), list) else []
        return [
            AgentEvent(
                kind="environment_status",
                data={
                    "version": str(ev.get("version") or 1),
                    "phase": str(ev.get("phase") or "preparing"),
                    "components": json.dumps(
                        components,
                        ensure_ascii=False,
                        separators=(",", ":"),
                    ),
                },
            )
        ]
    if t == "error":
        return [
            AgentEvent(
                kind="error",
                data={"stage": ev.get("stage", ""), "error": ev.get("error", "")},
            )
        ]
    # "start", "done", and unknown types are handled by the caller / dropped.
    return []


def _with_loaded_skills(
    event: AgentEvent,
    skills: list[dict[str, str]] | None,
) -> AgentEvent:
    """Fold runtime-owned Skill state into a shim-owned MCP snapshot."""
    if event.kind != "environment_status" or not skills:
        return event
    try:
        raw_components = json.loads(event.data.get("components", "[]"))
    except (TypeError, json.JSONDecodeError):
        raw_components = []
    components = [item for item in raw_components if isinstance(item, dict)]
    existing = {(str(item.get("kind") or ""), str(item.get("id") or "")) for item in components}
    loaded: list[dict[str, object]] = []
    for skill in sorted(skills, key=lambda item: str(item.get("id") or "")):
        skill_id = str(skill.get("id") or "").strip()
        if not skill_id or ("skill", skill_id) in existing:
            continue
        component: dict[str, object] = {
            "kind": "skill",
            "id": skill_id,
            "label": str(skill.get("name") or skill_id).strip() or skill_id,
            "status": "loaded",
            "tool_count": 0,
        }
        version = str(skill.get("version") or "").strip()
        if version:
            component["version"] = version
        loaded.append(component)
    if not loaded:
        return event
    return AgentEvent(
        kind=event.kind,
        data={
            **event.data,
            "components": json.dumps(
                [*loaded, *components],
                ensure_ascii=False,
                separators=(",", ":"),
            ),
        },
    )


@dataclass
class _AttemptState:
    """Mutable bookkeeping for ONE shim exec attempt.

    Lets `_stream_attempt` relay content live while deferring the terminal
    error/session decisions to `query`, which needs them to choose whether a
    dangling-resume retry is warranted.
    """

    saw_content: bool = False
    saw_error: bool = False
    error_text: str = ""  # concatenated error/stderr text, for resume-not-found detection
    last_session_id: str | None = None
    errors: list[AgentEvent] = field(default_factory=list)


def _provider_trace_event(
    name: str,
    span_id: str,
    started_at_ns: int,
    *,
    status: str,
    **attributes: object,
) -> AgentEvent:
    data: dict[str, object] = {
        "schema_version": 1,
        "span_id": span_id,
        "service": "sandbox-shim",
        "name": name,
        "category": "agent",
        "started_at_unix_ms": started_at_ns // 1_000_000,
        "duration_us": max((time.time_ns() - started_at_ns) // 1_000, 0),
        "status": status,
    }
    data.update(attributes)
    return AgentEvent(kind="trace", data=data)


class InSandboxShimProvider:
    """AgentProvider that runs the agent inside the bound sandbox via the shim.

    The resume binding (cocola session_id -> claude_session_id) lives in a
    `SessionMap`. With a Postgres-backed map it survives an agent-runtime
    restart, so a follow-up turn `--resume`s the on-disk Claude session (restored
    from a MinIO checkpoint when the sandbox was replaced). The map is a pure INDEX: the
    SUFFICIENT condition for resume is the on-disk `~/.claude` session file; the
    map only records which id to reopen. Production injects Postgres; the
    in-process default exists only for isolated provider tests.
    """

    def __init__(
        self,
        executor: SandboxExecutor,
        *,
        default_max_turns: int = 20,
        session_map: SessionMap | None = None,
    ) -> None:
        self._executor = executor
        self._default_max_turns = default_max_turns
        self._session_map: SessionMap = session_map or MemorySessionMap()

    def _build_request(self, prompt: str, options: AgentOptions, resume: str | None) -> str:
        req: dict = {
            "prompt": prompt,
            "max_turns": options.max_turns or self._default_max_turns,
        }
        if options.system_prompt:
            req["system_prompt"] = options.system_prompt
        if options.mcp_servers:
            req["mcp_servers"] = options.mcp_servers
        if resume:
            req["resume"] = resume
        return json.dumps(req, ensure_ascii=False, separators=(",", ":"))

    def _model_env(self, options: AgentOptions) -> dict[str, str]:
        """Per-turn exec env for the shim.

        Carries the model alias and, when the gateway minted a per-user token
        for this turn, ANTHROPIC_AUTH_TOKEN. This exec env is applied on EVERY
        turn's `exec_stream`, so cold, warm and reused sandboxes authenticate to
        the llm-gateway AS THE USER without a static provisioning credential --
        the warm-pool-safe injection point (ADR-0009 keeps creds in env, never
        the prompt channel).
        """
        env: dict[str, str] = {}
        alias = (options.model_alias or "").strip()
        if alias:
            env["ANTHROPIC_MODEL"] = alias
            env["ANTHROPIC_SMALL_FAST_MODEL"] = alias
        token = (options.auth_token or "").strip()
        if token:
            env["ANTHROPIC_AUTH_TOKEN"] = token
        traceparent = (options.traceparent or "").strip()
        if traceparent:
            # Claude/Anthropic parses this variable as newline-separated
            # ``Header-Name: value`` entries, not as JSON.
            env["ANTHROPIC_CUSTOM_HEADERS"] = f"traceparent: {traceparent}"
        return env

    async def query(
        self,
        prompt: str,
        options: AgentOptions,
    ) -> AsyncIterator[AgentEvent]:
        """Drive one shim turn and relay its NDJSON stream as AgentEvents.

        Route A requires a bound sandbox: without `options.sandbox_id` there is
        nowhere to run the brain, so we emit a terminal error + done rather than
        silently falling back (the composition root decides routing, not us).

        Graceful resume degradation: if a stored resume id is *dangling* (the
        sandbox's on-disk ~/.claude has no such session -- fresh/recycled box or
        a GC'd session file), the shim exits non-zero with a "no conversation
        found" marker BEFORE emitting any content. Rather than surfacing an
        opaque "shim exited 1", we forget the stale index entry and retry the
        SAME turn once as a fresh conversation (no --resume). The retry is only
        attempted when nothing was streamed yet, so the user never sees a
        half-turn replayed.
        """
        if not options.sandbox_id:
            yield AgentEvent(
                kind="error",
                data={"error": "InSandboxShimProvider requires a bound sandbox_id"},
            )
            yield AgentEvent(kind="done", data={})
            return

        binding = await self._session_map.get_binding(options.session_id, user_id=options.user_id)
        resume = binding.claude_session_id if binding else None
        if binding and binding.sandbox_id and binding.sandbox_id != options.sandbox_id:
            log.info(
                "session sandbox changed; trying stored resume id",
                session_id=options.session_id,
                resume=resume,
                previous_sandbox_id=binding.sandbox_id,
                sandbox_id=options.sandbox_id,
            )

        state = _AttemptState()
        async for ev in self._stream_attempt(
            self._build_request(prompt, options, resume), options, state
        ):
            yield ev

        # A dangling resume fails cleanly (no content) with a recognizable
        # marker -- the one case where replaying the turn is safe. Forget the
        # stale id and retry fresh so the user gets a real answer instead of an
        # opaque exit code.
        if (
            resume
            and state.saw_error
            and not state.saw_content
            and _looks_like_resume_not_found(state.error_text)
        ):
            log.info(
                "resume id dangling; forgetting it and retrying without resume",
                session_id=options.session_id,
                resume=resume,
            )
            try:
                await self._session_map.delete(options.session_id, user_id=options.user_id)
            except Exception as exc:  # noqa: BLE001 - index delete is best-effort
                log.warning(
                    "session-map delete failed",
                    session_id=options.session_id,
                    error=repr(exc),
                )
            state = _AttemptState()
            retry_span_id = secrets.token_hex(8)
            retry_started_ns = time.time_ns()
            async for ev in self._stream_attempt(
                self._build_request(prompt, options, None), options, state
            ):
                yield ev
            yield _provider_trace_event(
                "agent.resume_retry",
                retry_span_id,
                retry_started_ns,
                status="error" if state.saw_error else "success",
            )

        # Surface any deferred error(s) from the FINAL attempt (deferred so the
        # retry decision above can inspect them before the user sees them).
        for err in state.errors:
            yield err

        # Persist the session<->claude-session index for a later --resume. A
        # Postgres-backed map makes this survive an agent-runtime restart; the
        # write is best-effort so a transient index fault never fails the turn.
        if state.last_session_id:
            try:
                await self._session_map.put(
                    options.session_id,
                    state.last_session_id,
                    user_id=options.user_id,
                    sandbox_id=options.sandbox_id or "",
                )
            except Exception as exc:  # noqa: BLE001 - index write is best-effort
                log.warning(
                    "session-map put failed",
                    session_id=options.session_id,
                    error=repr(exc),
                )

        yield AgentEvent(
            kind="done",
            data={"session_id": state.last_session_id} if state.last_session_id else {},
        )
        if state.saw_error:
            log.info("shim turn completed with errors", session_id=options.session_id)

    async def _stream_attempt(
        self,
        request_json: str,
        options: AgentOptions,
        state: _AttemptState,
    ) -> AsyncIterator[AgentEvent]:
        """Run ONE shim exec, relaying content live and DEFERRING terminal errors.

        Content events (text/thinking/tool_use/tool_result/result/system) are
        yielded as they arrive. Error signals -- a shim `error` event, an exec
        transport error, or a non-zero exit -- are NOT yielded here; they are
        recorded on `state` so the caller can decide whether a dangling-resume
        retry is warranted before the user ever sees them. The terminal `done`
        is likewise synthesized by the caller, once, after all attempts.
        """
        buf = ""  # holds an incomplete trailing NDJSON line across chunks
        stderr_tail = ""

        def _record_error(ev: AgentEvent, text: str) -> None:
            state.saw_error = True
            state.errors.append(ev)
            if text:
                state.error_text = (state.error_text + "\n" + text)[-4000:]

        try:
            async for chunk in self._executor.exec_stream(
                sandbox_id=options.sandbox_id,
                cmd=[SHIM_ENTRYPOINT],
                env=self._model_env(options),
                stdin=request_json,
                timeout_secs=options.run_timeout_secs,
            ):
                if chunk.kind == "stderr":
                    # Shim diagnostics; keep a short tail for error context only.
                    stderr_tail = (stderr_tail + chunk.data)[-2000:]
                    continue
                if chunk.kind == "error":
                    message = _user_facing_exec_error(chunk.error)
                    log.warning(
                        "sandbox exec failed",
                        session_id=options.session_id,
                        sandbox_id=options.sandbox_id,
                        error=chunk.error,
                        user_message=message,
                    )
                    _record_error(
                        AgentEvent(
                            kind="error",
                            data={"error": message},
                        ),
                        chunk.error,
                    )
                    continue
                if chunk.kind == "exit":
                    if chunk.exit_code != 0:
                        _record_error(
                            AgentEvent(
                                kind="error",
                                data={
                                    "error": f"shim exited {chunk.exit_code}",
                                    "stderr": stderr_tail,
                                },
                            ),
                            stderr_tail,
                        )
                    continue

                # kind == "stdout": reassemble whole NDJSON lines.
                buf += chunk.data
                while "\n" in buf:
                    line, buf = buf.split("\n", 1)
                    line = line.strip()
                    if not line:
                        continue
                    try:
                        ev = json.loads(line)
                    except json.JSONDecodeError:
                        log.warning("shim emitted a non-JSON line", line=line[:200])
                        continue
                    if not isinstance(ev, dict):
                        continue
                    if ev.get("type") == "done":
                        if ev.get("session_id"):
                            state.last_session_id = ev["session_id"]
                        continue  # caller synthesizes the terminal `done`
                    if ev.get("type") == "result" and ev.get("session_id"):
                        state.last_session_id = ev["session_id"]
                    for out in _shim_event_to_agent_events(ev):
                        out = _with_loaded_skills(out, options.environment_skills)
                        if out.kind == "error":
                            _record_error(out, out.data.get("error", ""))
                        else:
                            if out.kind != "environment_status":
                                state.saw_content = True
                            yield out

            # Flush a final unterminated line, if any (defensive).
            tail = buf.strip()
            if tail:
                try:
                    ev = json.loads(tail)
                    if isinstance(ev, dict):
                        if ev.get("type") == "done" and ev.get("session_id"):
                            state.last_session_id = ev["session_id"]
                        elif ev.get("type") != "done":
                            for out in _shim_event_to_agent_events(ev):
                                out = _with_loaded_skills(out, options.environment_skills)
                                if out.kind == "error":
                                    _record_error(out, out.data.get("error", ""))
                                else:
                                    if out.kind != "environment_status":
                                        state.saw_content = True
                                    yield out
                except json.JSONDecodeError:
                    pass
        except Exception as exc:  # noqa: BLE001 - turn any transport fault into a clean error
            _record_error(
                AgentEvent(kind="error", data={"error": f"shim transport error: {exc}"}),
                str(exc),
            )
