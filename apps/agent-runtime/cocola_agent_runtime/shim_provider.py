"""InSandboxShimProvider -- runtime-neutral Sandbox Shim provider.

Where the legacy central-SDK path (Route B, decommissioned) spawned the `claude`
CLI *on the agent-runtime host* and forwarded only bash/file tools into a sandbox,
this provider runs the selected Agent Runtime inside the user's own container.
It does that by driving the in-sandbox stdio shim
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

Security note (ADR-0009 sec.2): runtime credentials are injected into the
per-turn exec environment, never through the prompt channel, so this provider
never puts secrets in the request or logs.
"""

from __future__ import annotations

import json
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


def _user_facing_exec_error(raw: str) -> str:
    """Return the actual sandbox error without guessing which tool caused it."""
    text = (raw or "").strip()
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
        result = AgentEvent(
            kind="result",
            data={
                "is_error": bool(ev.get("is_error", False)),
                "num_turns": ev.get("num_turns"),
                "total_cost_usd": ev.get("total_cost_usd"),
                "session_id": ev.get("session_id"),
                "result": ev.get("result"),
            },
        )
        if not result.data["is_error"]:
            return [result]
        message = str(result.data["result"] or "Agent execution failed")
        return [
            result,
            AgentEvent(
                kind="error",
                data={"error": message},
            ),
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
    if t == "progress":
        return [
            AgentEvent(
                kind="progress",
                data={
                    "id": str(ev.get("id") or "todo-list"),
                    "items": json.dumps(
                        ev.get("items") if isinstance(ev.get("items"), list) else [],
                        ensure_ascii=False,
                        separators=(",", ":"),
                    ),
                },
            )
        ]
    if t == "error":
        data = {"stage": ev.get("stage", ""), "error": ev.get("error", "")}
        if ev.get("code"):
            data["code"] = ev["code"]
        return [
            AgentEvent(
                kind="error",
                data=data,
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
    error_codes: set[str] = field(default_factory=set)
    last_session_id: str | None = None
    persisted_session_id: str | None = None
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

    The resume binding (conversation/runtime -> native session ID) lives in a
    `SessionMap`. With a Postgres-backed map it survives an agent-runtime
    restart, so a follow-up turn resumes the on-disk native session (restored
    from the remounted Session Volume when the sandbox was replaced). The map is a pure
    index: the on-disk `~/.claude` or `~/.codex` state is the source of truth;
    the map only records which ID to reopen. Production injects Postgres; the
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
            "runtime_id": options.runtime_id,
            "conversation_id": options.session_id,
            "max_turns": options.max_turns or self._default_max_turns,
            "cwd": options.working_directory,
        }
        if options.model_route_id:
            req["model"] = options.model_route_id
        if options.selected_skill_id:
            req["skill_id"] = options.selected_skill_id
        if options.traceparent:
            req["traceparent"] = options.traceparent
        if options.system_prompt:
            req["system_prompt"] = options.system_prompt
        if options.mcp_servers:
            req["mcp_servers"] = options.mcp_servers
        if resume:
            req["resume"] = resume
        return json.dumps(req, ensure_ascii=False, separators=(",", ":"))

    def _model_env(self, options: AgentOptions) -> dict[str, str]:
        """Per-turn exec env for the shim.

        Carries the model route id and, when the gateway minted a per-user token,
        the selected runtime's auth variable. This exec env is applied on every
        turn's `exec_stream`, so cold, warm and reused sandboxes authenticate to
        the llm-gateway AS THE USER without a static provisioning credential --
        the per-turn injection point (ADR-0009 keeps creds in env, never
        the prompt channel).
        """
        env: dict[str, str] = {}
        route_id = (options.model_route_id or "").strip()
        if route_id and options.runtime_id == "codex":
            env["CODEX_MODEL"] = route_id
        elif route_id:
            env["ANTHROPIC_MODEL"] = route_id
            env["ANTHROPIC_SMALL_FAST_MODEL"] = route_id
        token = (options.auth_token or "").strip()
        if token and options.runtime_id == "codex":
            env["CODEX_API_KEY"] = token
        elif token:
            env["ANTHROPIC_AUTH_TOKEN"] = token
        traceparent = (options.traceparent or "").strip()
        if traceparent and options.runtime_id != "codex":
            # Claude/Anthropic parses this variable as newline-separated
            # ``Header-Name: value`` entries, not as JSON.
            env["ANTHROPIC_CUSTOM_HEADERS"] = f"traceparent: {traceparent}"
        project_credential = (options.project_credential or "").strip()
        project_provider = (options.project_provider or "").strip()
        project_repository = (options.project_repository or "").strip()
        project_broker_url = (options.project_broker_url or "").strip()
        project_task_branch = (options.project_task_branch or "").strip()
        working_directory = (options.working_directory or "/workspace").strip()
        env["COCOLA_AGENT_CWD"] = working_directory
        if project_credential and project_provider:
            env["COCOLA_PROJECT_CREDENTIAL"] = project_credential
            env["COCOLA_PROJECT_PROVIDER"] = project_provider
            env["COCOLA_PROJECT_REPOSITORY"] = project_repository
            env["COCOLA_PROJECT_TASK_BRANCH"] = project_task_branch
            if project_broker_url:
                env["COCOLA_PROJECT_BROKER_URL"] = project_broker_url
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
        sandbox's restored runtime state has no such session), the shim exits
        non-zero with a "no conversation
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

        binding = await self._session_map.get_binding(
            options.session_id, user_id=options.user_id, runtime_id=options.runtime_id
        )
        resume = binding.runtime_session_id if binding else None
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
            and "SESSION_NOT_FOUND" in state.error_codes
        ):
            log.info(
                "resume id dangling; forgetting it and retrying without resume",
                session_id=options.session_id,
                resume=resume,
            )
            try:
                await self._session_map.delete(
                    options.session_id,
                    user_id=options.user_id,
                    runtime_id=options.runtime_id,
                )
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

        # Persist the conversation<->native-session index before reporting success.
        # Without this write the next turn cannot resume the conversation, so
        # it is part of the user-visible result rather than best-effort metadata.
        if state.last_session_id and state.persisted_session_id != state.last_session_id:
            try:
                await self._persist_session_id(options, state.last_session_id)
                state.persisted_session_id = state.last_session_id
            except Exception as exc:  # noqa: BLE001 - storage boundary
                state.saw_error = True
                log.error(
                    "session-map put failed",
                    session_id=options.session_id,
                    error=repr(exc),
                )
                yield AgentEvent(
                    kind="error",
                    data={
                        "code": "SESSION_INDEX_WRITE_FAILED",
                        "error": "Session continuity could not be saved",
                    },
                )

        yield AgentEvent(
            kind="done",
            data={"session_id": state.last_session_id} if state.last_session_id else {},
        )
        if state.saw_error:
            log.info("shim turn completed with errors", session_id=options.session_id)

    async def _persist_session_id(self, options: AgentOptions, runtime_session_id: str) -> None:
        await self._session_map.put(
            options.session_id,
            runtime_session_id,
            user_id=options.user_id,
            sandbox_id=options.sandbox_id or "",
            runtime_id=options.runtime_id,
        )

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
            code = str(ev.data.get("code") or "").strip()
            if code:
                state.error_codes.add(code)

        try:
            async for chunk in self._executor.exec_stream(
                sandbox_id=options.sandbox_id,
                cmd=[SHIM_ENTRYPOINT],
                cwd=options.working_directory or "/workspace",
                env=self._model_env(options),
                stdin=request_json,
                # A full Agent run has no aggregate deadline. Gateway enforces
                # each tool step independently and cancellation propagates here.
                timeout_secs=-1,
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
                    if chunk.exit_code != 0 and not state.saw_error:
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
                    if ev.get("type") == "start" and ev.get("session_id"):
                        state.last_session_id = str(ev["session_id"])
                        try:
                            await self._persist_session_id(options, state.last_session_id)
                            state.persisted_session_id = state.last_session_id
                        except Exception as exc:  # noqa: BLE001 - retry after the stream completes
                            log.warning(
                                "early session-map put failed; will retry at turn end",
                                session_id=options.session_id,
                                error=repr(exc),
                            )
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
                        if ev.get("type") in {"start", "done"} and ev.get("session_id"):
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
