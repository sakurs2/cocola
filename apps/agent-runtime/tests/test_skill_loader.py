"""Skill loader tests.

Hermetic: no socket, no admin-api. AdminSkillCatalog gets an injected fake
Fetcher returning canned JSON bytes; StaticSkillCatalog needs nothing. We
assert (a) the wire JSON maps to Skill, (b) only enabled-with-id entries
survive the defensive re-filter, and (c) transport/parse errors remain visible
to the runtime so it cannot expose stale sandbox skills.
"""

import json

import pytest
from cocola_agent_runtime.skill_loader import (
    AdminSkillCatalog,
    Skill,
    StaticSkillCatalog,
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
    assert s.native_id == "x"
    assert s.name == "" and s.version == "" and s.entrypoint == ""


def test_skill_separates_catalog_and_runtime_identity():
    s = Skill.from_json(
        {
            "id": "user-32970b55-frontend-design",
            "runtime_id": "frontend-design",
            "name": "Frontend Design",
        }
    )
    assert s.id == "user-32970b55-frontend-design"
    assert s.native_id == "frontend-design"


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


def test_admin_catalog_propagates_fetch_error():
    def boom(url, headers, timeout):
        raise RuntimeError("connection refused")

    cat = AdminSkillCatalog("http://admin", fetcher=boom)
    with pytest.raises(RuntimeError, match="connection refused"):
        cat.enabled_skills()


def test_admin_catalog_propagates_bad_json():
    def garbage(url, headers, timeout):
        return b"not json{"

    cat = AdminSkillCatalog("http://admin", fetcher=garbage)
    with pytest.raises(json.JSONDecodeError):
        cat.enabled_skills()


def test_static_catalog_roundtrip():
    s = Skill(id="a", name="A")
    cat = StaticSkillCatalog([s])
    assert cat.enabled_skills() == [s]
    assert cat.enabled_skills() is not cat.enabled_skills()
