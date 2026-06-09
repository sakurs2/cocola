"""Skill loader tests.

Hermetic: no socket, no admin-api. AdminSkillCatalog gets an injected fake
Fetcher returning canned JSON bytes; StaticSkillCatalog needs nothing. We
assert (a) the wire JSON maps to Skill, (b) only enabled-with-id entries
survive the defensive re-filter, (c) any transport/parse error degrades to an
empty list instead of breaking session startup, and (d) the system-prompt
preamble renders (and stays empty when there are no skills).
"""
import json

from cocola_agent_runtime.agent_provider import AgentOptions
from cocola_agent_runtime.skill_loader import (
    AdminSkillCatalog,
    Skill,
    StaticSkillCatalog,
    apply_skills_to_options,
    skills_system_preamble,
)


def _fetcher(payload, calls=None):
    raw = json.dumps(payload).encode()

    def fetch(url, headers, timeout):
        if calls is not None:
            calls.append((url, headers, timeout))
        return raw

    return fetch


def test_skill_from_json_defaults():
    s = Skill.from_json({"id": "x"})
    assert s.id == "x"
    assert s.name == "" and s.version == "" and s.entrypoint == ""


def test_admin_catalog_maps_and_filters():
    payload = {
        "skills": [
            {"id": "web", "name": "Web Search", "version": "1.2", "description": "search"},
            {"id": "", "name": "no-id-dropped"},
            "not-a-dict",
            {"name": "missing-id-dropped"},
        ]
    }
    cat = AdminSkillCatalog("http://admin/", fetcher=_fetcher(payload))
    skills = cat.enabled_skills()
    assert [s.id for s in skills] == ["web"]
    assert skills[0].name == "Web Search" and skills[0].version == "1.2"


def test_admin_catalog_sends_bearer_and_query():
    calls = []
    cat = AdminSkillCatalog(
        "http://admin/", admin_key="SECRET", fetcher=_fetcher({"skills": []}, calls=calls)
    )
    cat.enabled_skills()
    url, headers, _timeout = calls[0]
    assert url == "http://admin/admin/skills?enabled=true"
    assert headers["Authorization"] == "Bearer SECRET"


def test_admin_catalog_no_key_omits_auth_header():
    calls = []
    cat = AdminSkillCatalog("http://admin", fetcher=_fetcher({"skills": []}, calls=calls))
    cat.enabled_skills()
    _url, headers, _timeout = calls[0]
    assert "Authorization" not in headers


def test_admin_catalog_degrades_on_fetch_error():
    def boom(url, headers, timeout):
        raise RuntimeError("connection refused")

    cat = AdminSkillCatalog("http://admin", fetcher=boom)
    assert cat.enabled_skills() == []


def test_admin_catalog_degrades_on_bad_json():
    def garbage(url, headers, timeout):
        return b"not json{"

    cat = AdminSkillCatalog("http://admin", fetcher=garbage)
    assert cat.enabled_skills() == []


def test_static_catalog_roundtrip():
    s = Skill(id="a", name="A")
    cat = StaticSkillCatalog([s])
    assert cat.enabled_skills() == [s]
    assert cat.enabled_skills() is not cat.enabled_skills()


def test_preamble_empty_when_no_skills():
    assert skills_system_preamble([]) == ""
    assert skills_system_preamble([Skill(id="", name="")]) == ""


def test_preamble_renders_enabled():
    out = skills_system_preamble([
        Skill(id="web", name="Web Search", version="1.2", description="search the web"),
        Skill(id="calc", name=""),
    ])
    assert "Web Search v1.2" in out
    assert "search the web" in out
    assert "- calc" in out
    assert out.startswith("Available cocola skills")


def test_apply_skills_merges_into_empty_system_prompt():
    opts = AgentOptions(user_id="U", session_id="S")
    cat = StaticSkillCatalog([Skill(id="web", name="Web Search")])
    out = apply_skills_to_options(opts, cat)
    assert out is not opts
    assert out.system_prompt is not None
    assert out.system_prompt.startswith("Available cocola skills")
    assert "- Web Search" in out.system_prompt


def test_apply_skills_preserves_base_system_prompt():
    opts = AgentOptions(user_id="U", session_id="S", system_prompt="BASE RULES")
    cat = StaticSkillCatalog([Skill(id="web", name="Web Search")])
    out = apply_skills_to_options(opts, cat)
    assert out.system_prompt.startswith("Available cocola skills")
    assert out.system_prompt.endswith("BASE RULES")


def test_apply_skills_noop_when_no_skills():
    opts = AgentOptions(user_id="U", session_id="S", system_prompt="BASE")
    out = apply_skills_to_options(opts, StaticSkillCatalog([]))
    assert out is opts
