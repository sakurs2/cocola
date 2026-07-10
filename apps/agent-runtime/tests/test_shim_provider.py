"""InSandboxShimProvider tests (Route A, ADR-0009).

Hermetic: no Docker, no gRPC, no real shim. A StaticSandboxExecutor scripts the
in-sandbox shim's NDJSON stdout (including a tool_use turn and a line split
across two byte chunks), and we assert the provider:

  - maps shim NDJSON events to the generic AgentEvent taxonomy, in order;
  - reassembles a JSON line that was split mid-line across exec chunks;
  - captures the session_id from `done` and reuses it as `resume` next turn;
  - turns a shim error / non-zero exit into a clean terminal error event;
  - refuses to run Route A without a bound sandbox_id.
"""

from __future__ import annotations

import json

from cocola_agent_runtime.agent_provider import AgentOptions
from cocola_agent_runtime.sandbox_binder import ExecChunk, StaticSandboxExecutor
from cocola_agent_runtime.session_map import MemorySessionMap
from cocola_agent_runtime.shim_provider import InSandboxShimProvider


def _ndjson(*objs: dict) -> str:
    return "".join(json.dumps(o, separators=(",", ":")) + "\n" for o in objs)


async def _drain(provider, prompt, options):
    return [ev async for ev in provider.query(prompt, options)]


async def test_maps_tool_use_turn_and_reassembles_split_line():
    # A realistic shim turn: start (dropped), a text block, a tool_use, a
    # tool_result, then a done carrying the session_id. We deliberately chop the
    # stdout into chunks that split a JSON line in the middle to exercise the
    # line-reassembly buffer.
    full = _ndjson(
        {"type": "start", "ts": 1.0},
        {"type": "text", "text": "Let me check the weather."},
        {"type": "tool_use", "id": "tu_1", "name": "Bash", "input": {"cmd": "date"}},
        {"type": "tool_result", "tool_use_id": "tu_1", "is_error": False},
        {"type": "done", "session_id": "sess-abc", "ts": 2.0},
    )
    cut = len(full) // 2  # almost certainly lands inside a line

    def stream_handler(sandbox_id, cmd, stdin):
        yield ExecChunk(kind="stdout", data=full[:cut])
        yield ExecChunk(kind="stdout", data=full[cut:])
        yield ExecChunk(kind="exit", exit_code=0)

    execu = StaticSandboxExecutor(stream_handler=stream_handler)
    provider = InSandboxShimProvider(execu)
    opts = AgentOptions(user_id="U1", session_id="S1", sandbox_id="box-1")

    events = await _drain(provider, "weather?", opts)
    kinds = [e.kind for e in events]

    # start is dropped; we synthesize exactly one terminal done.
    assert kinds == ["text", "tool_use", "tool_result", "done"], kinds
    assert events[0].data["text"] == "Let me check the weather."
    assert events[1].data == {"id": "tu_1", "name": "Bash", "input": {"cmd": "date"}}
    assert events[2].data["tool_use_id"] == "tu_1"
    assert events[-1].data == {"session_id": "sess-abc"}

    # The shim was driven via the shim entrypoint with our Request JSON on stdin.
    assert execu.stream_calls[0]["cmd"] == ["/opt/cocola/shim/entrypoint.sh"]
    sent = json.loads(execu.stream_calls[0]["stdin"])
    assert sent["prompt"] == "weather?"
    assert "resume" not in sent  # first turn has nothing to resume


async def test_request_includes_mcp_servers_when_configured():
    def stream_handler(sandbox_id, cmd, stdin):
        yield ExecChunk(kind="stdout", data=_ndjson({"type": "done", "session_id": "sess-1"}))
        yield ExecChunk(kind="exit", exit_code=0)

    execu = StaticSandboxExecutor(stream_handler=stream_handler)
    provider = InSandboxShimProvider(execu)
    opts = AgentOptions(
        user_id="U1",
        session_id="S1",
        sandbox_id="box-1",
        mcp_servers={
            "github": {
                "type": "stdio",
                "command": "npx",
                "args": ["-y", "server"],
                "env": {"GITHUB_TOKEN": "secret"},
            }
        },
    )

    await _drain(provider, "hello", opts)

    sent = json.loads(execu.stream_calls[0]["stdin"])
    assert sent["mcp_servers"]["github"]["command"] == "npx"
    assert sent["mcp_servers"]["github"]["env"]["GITHUB_TOKEN"] == "secret"
    assert "environment_skills" not in sent


async def test_environment_status_is_forwarded_as_a_snapshot():
    components = [
        {
            "kind": "mcp",
            "id": "maps",
            "label": "Amap",
            "status": "failed",
            "tool_count": 0,
            "error": "Unable to connect",
        }
    ]

    def stream_handler(sandbox_id, cmd, stdin):
        yield ExecChunk(
            kind="stdout",
            data=_ndjson(
                {
                    "type": "environment_status",
                    "version": 1,
                    "phase": "degraded",
                    "components": components,
                },
                {"type": "done", "session_id": "sess-1"},
            ),
        )
        yield ExecChunk(kind="exit", exit_code=0)

    provider = InSandboxShimProvider(StaticSandboxExecutor(stream_handler=stream_handler))
    opts = AgentOptions(
        user_id="U1",
        session_id="S1",
        sandbox_id="box-1",
        environment_skills=[{"id": "web", "name": "Web Search", "version": "1.2"}],
    )

    events = await _drain(provider, "hello", opts)

    assert [event.kind for event in events] == ["environment_status", "done"]
    expected_components = [
        {
            "kind": "skill",
            "id": "web",
            "label": "Web Search",
            "status": "loaded",
            "tool_count": 0,
            "version": "1.2",
        },
        *components,
    ]
    assert events[0].data == {
        "version": "1",
        "phase": "degraded",
        "components": json.dumps(
            expected_components,
            ensure_ascii=False,
            separators=(",", ":"),
        ),
    }


async def test_loaded_skills_enrich_an_empty_ready_environment_snapshot():
    def stream_handler(sandbox_id, cmd, stdin):
        yield ExecChunk(
            kind="stdout",
            data=_ndjson(
                {
                    "type": "environment_status",
                    "version": 1,
                    "phase": "ready",
                    "components": [],
                },
                {"type": "done", "session_id": "sess-1"},
            ),
        )
        yield ExecChunk(kind="exit", exit_code=0)

    provider = InSandboxShimProvider(StaticSandboxExecutor(stream_handler=stream_handler))
    opts = AgentOptions(
        user_id="U1",
        session_id="S1",
        sandbox_id="box-1",
        environment_skills=[{"id": "pdf", "name": "PDF", "version": "1.0"}],
    )

    events = await _drain(provider, "hello", opts)

    components = json.loads(events[0].data["components"])
    assert components == [
        {
            "kind": "skill",
            "id": "pdf",
            "label": "PDF",
            "status": "loaded",
            "tool_count": 0,
            "version": "1.0",
        }
    ]


async def test_session_id_is_reused_as_resume_next_turn():
    def make_handler(session_id):
        def stream_handler(sandbox_id, cmd, stdin):
            yield ExecChunk(
                kind="stdout",
                data=_ndjson(
                    {"type": "text", "text": "ok"},
                    {"type": "done", "session_id": session_id},
                ),
            )
            yield ExecChunk(kind="exit", exit_code=0)

        return stream_handler

    execu = StaticSandboxExecutor(stream_handler=make_handler("sess-1"))
    provider = InSandboxShimProvider(execu)
    opts = AgentOptions(user_id="U1", session_id="S1", sandbox_id="box-1")

    await _drain(provider, "first", opts)
    # Second turn on the SAME session must carry resume=sess-1.
    execu._stream_handler = make_handler("sess-2")
    await _drain(provider, "second", opts)

    second_req = json.loads(execu.stream_calls[1]["stdin"])
    assert second_req["resume"] == "sess-1"


async def test_stored_resume_is_reused_when_sandbox_changes():
    def stream_handler(sandbox_id, cmd, stdin):
        req = json.loads(stdin)
        assert req["resume"] == "sess-old"
        yield ExecChunk(
            kind="stdout",
            data=_ndjson(
                {"type": "text", "text": "resumed sandbox"},
                {"type": "done", "session_id": "sess-new"},
            ),
        )
        yield ExecChunk(kind="exit", exit_code=0)

    smap = MemorySessionMap()
    await smap.put("S1", "sess-old", user_id="U1", sandbox_id="box-old")
    execu = StaticSandboxExecutor(stream_handler=stream_handler)
    provider = InSandboxShimProvider(execu, session_map=smap)
    opts = AgentOptions(user_id="U1", session_id="S1", sandbox_id="box-new")

    events = await _drain(provider, "continue", opts)

    assert [e.kind for e in events] == ["text", "done"]
    assert events[0].data["text"] == "resumed sandbox"
    binding = await smap.get_binding("S1")
    assert binding is not None
    assert binding.claude_session_id == "sess-new"
    assert binding.sandbox_id == "box-new"


async def test_model_alias_is_injected_into_exec_env():
    def stream_handler(sandbox_id, cmd, stdin):
        yield ExecChunk(
            kind="stdout",
            data=_ndjson({"type": "done", "session_id": "sess-1"}),
        )
        yield ExecChunk(kind="exit", exit_code=0)

    execu = StaticSandboxExecutor(stream_handler=stream_handler)
    provider = InSandboxShimProvider(execu)
    opts = AgentOptions(
        user_id="U1",
        session_id="S1",
        sandbox_id="box-1",
        model_alias="claude-sonnet",
    )

    await _drain(provider, "hello", opts)

    env = execu.stream_calls[0]["env"]
    assert env["ANTHROPIC_MODEL"] == "claude-sonnet"
    assert env["ANTHROPIC_SMALL_FAST_MODEL"] == "claude-sonnet"


async def test_nonzero_exit_becomes_terminal_error():
    def stream_handler(sandbox_id, cmd, stdin):
        yield ExecChunk(kind="stderr", data="boom: cli not found\n")
        yield ExecChunk(kind="exit", exit_code=2)

    execu = StaticSandboxExecutor(stream_handler=stream_handler)
    provider = InSandboxShimProvider(execu)
    opts = AgentOptions(user_id="U1", session_id="S1", sandbox_id="box-1")

    events = await _drain(provider, "hi", opts)
    kinds = [e.kind for e in events]
    assert "error" in kinds
    err = next(e for e in events if e.kind == "error")
    assert "shim exited 2" in err.data["error"]
    assert "boom" in err.data.get("stderr", "")
    assert kinds[-1] == "done"  # still terminated cleanly


async def test_sandbox_exec_timeout_gets_user_facing_error():
    def stream_handler(sandbox_id, cmd, stdin):
        yield ExecChunk(
            kind="error",
            error="opensandbox: sse read: context deadline exceeded",
        )

    execu = StaticSandboxExecutor(stream_handler=stream_handler)
    provider = InSandboxShimProvider(execu)
    opts = AgentOptions(user_id="U1", session_id="S1", sandbox_id="box-1")

    events = await _drain(provider, "screenshot", opts)
    kinds = [e.kind for e in events]

    assert kinds == ["error", "done"], kinds
    assert "工具执行超时" in events[0].data["error"]
    assert "opensandbox: sse read" not in events[0].data["error"]


async def test_shim_error_event_is_relayed():
    def stream_handler(sandbox_id, cmd, stdin):
        yield ExecChunk(
            kind="stdout",
            data=_ndjson(
                {"type": "error", "stage": "run", "error": "model refused"},
            ),
        )
        yield ExecChunk(kind="exit", exit_code=1)

    execu = StaticSandboxExecutor(stream_handler=stream_handler)
    provider = InSandboxShimProvider(execu)
    opts = AgentOptions(user_id="U1", session_id="S1", sandbox_id="box-1")

    events = await _drain(provider, "hi", opts)
    errs = [e for e in events if e.kind == "error"]
    assert any("model refused" in e.data.get("error", "") for e in errs)


async def test_requires_bound_sandbox():
    execu = StaticSandboxExecutor()
    provider = InSandboxShimProvider(execu)
    opts = AgentOptions(user_id="U1", session_id="S1", sandbox_id=None)

    events = await _drain(provider, "hi", opts)
    kinds = [e.kind for e in events]
    assert kinds == ["error", "done"], kinds
    assert "requires a bound sandbox" in events[0].data["error"]
    assert execu.stream_calls == []  # never touched the sandbox


async def test_exec_stream_default_echoes_command():
    # Guards the StaticSandboxExecutor.exec_stream default path used by other
    # tests/dev: echo the command on stdout, then exit 0.
    execu = StaticSandboxExecutor()
    chunks = [c async for c in execu.exec_stream(sandbox_id="box-1", cmd=["echo", "hi"])]
    assert chunks[0].kind == "stdout"
    assert chunks[0].data == "ran: echo hi"
    assert chunks[-1].kind == "exit"
    assert chunks[-1].exit_code == 0


async def test_dangling_resume_retries_fresh_and_reindexes():
    # A stored resume id that the sandbox no longer has: the first exec fails
    # with a "no conversation found" marker and NO content; the provider must
    # forget the stale id, retry the SAME turn WITHOUT resume, relay the fresh
    # answer, and re-index the new session id -- the user never sees the exit.
    calls = {"n": 0}

    def stream_handler(sandbox_id, cmd, stdin):
        calls["n"] += 1
        req = json.loads(stdin)
        if calls["n"] == 1:
            # Attempt 1 carried the (now dangling) resume id and died.
            assert req["resume"] == "sess-stale"
            yield ExecChunk(
                kind="stderr",
                data="Error: No conversation found with session ID: sess-stale\n",
            )
            yield ExecChunk(kind="exit", exit_code=1)
            return
        # Attempt 2 is a fresh conversation (no resume) and succeeds.
        assert "resume" not in req
        yield ExecChunk(
            kind="stdout",
            data=_ndjson(
                {"type": "text", "text": "fresh answer"},
                {"type": "done", "session_id": "sess-new"},
            ),
        )
        yield ExecChunk(kind="exit", exit_code=0)

    smap = MemorySessionMap()
    await smap.put("S1", "sess-stale")
    execu = StaticSandboxExecutor(stream_handler=stream_handler)
    provider = InSandboxShimProvider(execu, session_map=smap)
    opts = AgentOptions(user_id="U1", session_id="S1", sandbox_id="box-1")

    events = await _drain(provider, "hello", opts)
    kinds = [e.kind for e in events]

    # The dangling-resume failure is swallowed; the user sees only the retry.
    assert kinds == ["text", "done"], kinds
    assert events[0].data["text"] == "fresh answer"
    assert events[-1].data == {"session_id": "sess-new"}
    assert calls["n"] == 2  # exactly one retry
    # The stale id was forgotten and replaced by the fresh one.
    assert await smap.get("S1") == "sess-new"


async def test_unrelated_failure_is_not_retried():
    # A non-resume failure (e.g. the CLI is missing) must NOT trigger the fresh
    # retry, even with a stored resume id -- we only replay for a dangling
    # resume, and the real error must reach the user.
    calls = {"n": 0}

    def stream_handler(sandbox_id, cmd, stdin):
        calls["n"] += 1
        yield ExecChunk(kind="stderr", data="boom: cli not found\n")
        yield ExecChunk(kind="exit", exit_code=127)

    smap = MemorySessionMap()
    await smap.put("S1", "sess-x")
    execu = StaticSandboxExecutor(stream_handler=stream_handler)
    provider = InSandboxShimProvider(execu, session_map=smap)
    opts = AgentOptions(user_id="U1", session_id="S1", sandbox_id="box-1")

    events = await _drain(provider, "hi", opts)
    kinds = [e.kind for e in events]

    assert calls["n"] == 1  # no retry
    assert kinds == ["error", "done"], kinds
    assert "shim exited 127" in events[0].data["error"]
    # A non-dangling failure must NOT clobber the stored resume id.
    assert await smap.get("S1") == "sess-x"


async def test_dangling_resume_not_retried_when_content_already_streamed():
    # If the shim streamed real content before dying with a resume-ish marker,
    # replaying the turn would double the answer. We must NOT retry once any
    # content was emitted; the deferred error still surfaces.
    calls = {"n": 0}

    def stream_handler(sandbox_id, cmd, stdin):
        calls["n"] += 1
        yield ExecChunk(kind="stdout", data=_ndjson({"type": "text", "text": "partial"}))
        yield ExecChunk(kind="stderr", data="No conversation found with session ID: sess-stale\n")
        yield ExecChunk(kind="exit", exit_code=1)

    smap = MemorySessionMap()
    await smap.put("S1", "sess-stale")
    execu = StaticSandboxExecutor(stream_handler=stream_handler)
    provider = InSandboxShimProvider(execu, session_map=smap)
    opts = AgentOptions(user_id="U1", session_id="S1", sandbox_id="box-1")

    events = await _drain(provider, "hi", opts)
    kinds = [e.kind for e in events]

    assert calls["n"] == 1  # no retry once content was streamed
    assert kinds == ["text", "error", "done"], kinds
    assert events[0].data["text"] == "partial"
