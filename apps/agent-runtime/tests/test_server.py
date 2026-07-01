"""AgentRuntimeServicer tests.

Hermetic: no gRPC server, no socket. We call the servicer\'s Query coroutine
directly with a fake provider, a fake streaming context that records written
proto events, and a plain request object. We assert (a) generic AgentEvents map
onto proto AgentEvents with non-string data flattened, (b) a provider error
becomes a terminal proto `error` event instead of propagating, and (c) enabled
skills are folded into the AgentOptions the provider receives.
"""

import json
from dataclasses import dataclass, field

from cocola_agent_runtime.agent_provider import AgentEvent, AgentOptions
from cocola_agent_runtime.server import AgentRuntimeServicer, event_to_proto
from cocola_agent_runtime.skill_loader import Skill, StaticSkillCatalog


@dataclass
class FakeRequest:
    user_id: str = "U1"
    session_id: str = "S1"
    prompt: str = "hi"
    sandbox_id: str = ""
    max_turns: int = 0
    attachments: list = field(default_factory=list)


class FakeContext:
    """Records proto events the servicer streams via context.write()."""

    def __init__(self):
        self.written = []

    async def write(self, event):
        self.written.append(event)


class ListProvider:
    """AgentProvider yielding a fixed list; records the options it was given."""

    def __init__(self, events):
        self._events = events
        self.seen_options: AgentOptions | None = None

    async def query(self, prompt, options):
        self.seen_options = options
        for e in self._events:
            yield e


class BoomProvider:
    def __init__(self):
        self.seen_options = None

    async def query(self, prompt, options):
        self.seen_options = options
        yield AgentEvent(kind="text", data={"text": "partial"})
        raise RuntimeError("provider exploded")


def test_event_to_proto_flattens_non_strings():
    proto = event_to_proto(
        AgentEvent(
            kind="tool_use",
            data={
                "name": "bash",
                "input": {"cmd": "ls"},
                "n": 3,
                "nothing": None,
            },
        )
    )
    assert proto.kind == "tool_use"
    assert proto.data["name"] == "bash"
    assert json.loads(proto.data["input"]) == {"cmd": "ls"}
    assert proto.data["n"] == "3"
    assert proto.data["nothing"] == ""


async def test_query_streams_mapped_events():
    prov = ListProvider(
        [
            AgentEvent(kind="text", data={"text": "hello"}),
            AgentEvent(kind="done", data={}),
        ]
    )
    ctx = FakeContext()
    await AgentRuntimeServicer(prov).Query(FakeRequest(), ctx)
    kinds = [e.kind for e in ctx.written]
    assert kinds == ["text", "done"]
    assert ctx.written[0].data["text"] == "hello"


async def test_query_error_becomes_terminal_event():
    ctx = FakeContext()
    await AgentRuntimeServicer(BoomProvider()).Query(FakeRequest(), ctx)
    kinds = [e.kind for e in ctx.written]
    assert kinds == ["text", "error"]
    assert "provider exploded" in ctx.written[-1].data["error"]


async def test_query_folds_enabled_skills_into_options():
    prov = ListProvider([AgentEvent(kind="done", data={})])
    cat = StaticSkillCatalog([Skill(id="web", name="Web Search")])
    await AgentRuntimeServicer(prov, skills=cat).Query(FakeRequest(), FakeContext())
    assert prov.seen_options.system_prompt is not None
    assert "Web Search" in prov.seen_options.system_prompt


async def test_query_maps_request_fields_to_options():
    prov = ListProvider([AgentEvent(kind="done", data={})])
    req = FakeRequest(user_id="emp-9", session_id="sess-7", sandbox_id="box-1", max_turns=5)
    await AgentRuntimeServicer(prov).Query(req, FakeContext())
    o = prov.seen_options
    assert o.user_id == "emp-9" and o.session_id == "sess-7"
    assert o.sandbox_id == "box-1" and o.max_turns == 5
