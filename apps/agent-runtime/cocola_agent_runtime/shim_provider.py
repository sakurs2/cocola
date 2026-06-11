"""InSandboxShimProvider -- the Route A agent provider (ADR-0009).

Where `ClaudeAgentSDKProvider` (Route B) spawns the `claude` CLI *on the
agent-runtime host* and forwards only bash/file tools into a sandbox, this
provider runs the WHOLE Claude Code brain inside the user's own container. It
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
from collections.abc import AsyncIterator

from cocola_common import get_logger

from cocola_agent_runtime.agent_provider import AgentEvent, AgentOptions
from cocola_agent_runtime.sandbox_binder import SandboxExecutor
from cocola_agent_runtime.session_map import MemorySessionMap, SessionMap

log = get_logger("cocola.agent-runtime.shim")

# Absolute path of the shim launcher baked into the Route A sandbox image
# (deploy/sandbox-runtime/Dockerfile). Driven over `docker exec -i` / `kubectl
# exec -i` STDIO -- never a listening port.
SHIM_ENTRYPOINT = "/opt/cocola/shim/entrypoint.sh"


def _shim_event_to_agent_events(ev: dict) -> list[AgentEvent]:
    """Map one shim NDJSON event to zero or more AgentEvents.

    Taxonomy is kept identical to ClaudeAgentSDKProvider so the gateway/web SSE
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
    if t == "error":
        return [
            AgentEvent(
                kind="error",
                data={"stage": ev.get("stage", ""), "error": ev.get("error", "")},
            )
        ]
    # "start", "done", and unknown types are handled by the caller / dropped.
    return []


class InSandboxShimProvider:
    """AgentProvider that runs the agent inside the bound sandbox via the shim.

    The resume binding (cocola session_id -> claude_session_id) lives in a
    `SessionMap`. With a Postgres-backed map it survives an agent-runtime
    restart, so a follow-up turn `--resume`s the on-disk Claude session
    (persisted on the agent volume, ADR-0008). The map is a pure INDEX: the
    SUFFICIENT condition for resume is the on-disk `~/.claude` session file; the
    map only records which id to reopen. Defaults to an in-process map so a
    zero-dependency dev boot still resumes within one process lifetime.
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
        if resume:
            req["resume"] = resume
        return json.dumps(req, ensure_ascii=False, separators=(",", ":"))

    async def query(
        self,
        prompt: str,
        options: AgentOptions,
    ) -> AsyncIterator[AgentEvent]:
        """Drive one shim turn and relay its NDJSON stream as AgentEvents.

        Route A requires a bound sandbox: without `options.sandbox_id` there is
        nowhere to run the brain, so we emit a terminal error + done rather than
        silently falling back (the composition root decides routing, not us).
        """
        if not options.sandbox_id:
            yield AgentEvent(
                kind="error",
                data={"error": "InSandboxShimProvider requires a bound sandbox_id"},
            )
            yield AgentEvent(kind="done", data={})
            return

        resume = await self._session_map.get(options.session_id)
        request_json = self._build_request(prompt, options, resume)
        buf = ""  # holds an incomplete trailing NDJSON line across chunks
        stderr_tail = ""
        last_session_id: str | None = None
        saw_error = False

        try:
            async for chunk in self._executor.exec_stream(
                sandbox_id=options.sandbox_id,
                cmd=[SHIM_ENTRYPOINT],
                stdin=request_json,
            ):
                if chunk.kind == "stderr":
                    # Shim diagnostics; keep a short tail for error context only.
                    stderr_tail = (stderr_tail + chunk.data)[-2000:]
                    continue
                if chunk.kind == "error":
                    saw_error = True
                    yield AgentEvent(
                        kind="error",
                        data={"error": f"sandbox exec failed: {chunk.error}"},
                    )
                    continue
                if chunk.kind == "exit":
                    if chunk.exit_code != 0:
                        saw_error = True
                        yield AgentEvent(
                            kind="error",
                            data={
                                "error": f"shim exited {chunk.exit_code}",
                                "stderr": stderr_tail,
                            },
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
                            last_session_id = ev["session_id"]
                        continue  # we synthesize the terminal `done` below
                    if ev.get("type") == "result" and ev.get("session_id"):
                        last_session_id = ev["session_id"]
                    for out in _shim_event_to_agent_events(ev):
                        if out.kind == "error":
                            saw_error = True
                        yield out

            # Flush a final unterminated line, if any (defensive).
            tail = buf.strip()
            if tail:
                try:
                    ev = json.loads(tail)
                    if isinstance(ev, dict):
                        if ev.get("type") == "done" and ev.get("session_id"):
                            last_session_id = ev["session_id"]
                        elif ev.get("type") != "done":
                            for out in _shim_event_to_agent_events(ev):
                                if out.kind == "error":
                                    saw_error = True
                                yield out
                except json.JSONDecodeError:
                    pass
        except Exception as exc:  # noqa: BLE001 - turn any transport fault into a clean error
            saw_error = True
            yield AgentEvent(kind="error", data={"error": f"shim transport error: {exc}"})

        # Persist the session<->claude-session index for a later --resume. A
        # Postgres-backed map makes this survive an agent-runtime restart; the
        # write is best-effort so a transient index fault never fails the turn.
        if last_session_id:
            try:
                await self._session_map.put(
                    options.session_id,
                    last_session_id,
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
            data={"session_id": last_session_id} if last_session_id else {},
        )
        if saw_error:
            log.info("shim turn completed with errors", session_id=options.session_id)
