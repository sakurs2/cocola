"""Skill-Market loader: the agent-runtime consumes the admin-api catalog.

The admin-api owns the Skill-Market catalog (ADR-0006): operators register and
toggle named, versioned capabilities, and the catalog is the single audited
surface for "what employees can use". The runtime is the CONSUMER: before a
session it asks the admin-api for the *enabled* entries and exposes them to the
agent, so flipping a skill on/off in the control plane changes what the agent
can do — no runtime redeploy.

Design mirrors the rest of the runtime (agent_provider, shim_provider):
a small Protocol the server depends on, a production HTTP implementation, and a
static implementation for hermetic tests. The HTTP transport is an injectable
callable (default: stdlib urllib) so unit tests never open a socket and the
package imports cleanly without httpx.

Default sessions consume the user's effective enabled set. Custom Agent sessions
resolve only their snapshotted catalog IDs: user enable preferences are ignored,
while administrator disablement, deletion, package validity, and ownership stay
hard gates.
"""

from __future__ import annotations

import json
import urllib.parse
import urllib.request
from collections.abc import Callable, Sequence
from dataclasses import dataclass
from typing import Protocol


@dataclass(frozen=True)
class Skill:
    """One enabled Skill-Market capability, as the runtime consumes it.

    Mirrors the admin-api store.Skill JSON. `entrypoint` is the module/path the
    runtime will hand to the agent; `id`/`name`/`description` describe it to the
    model and to operators.
    """

    id: str
    name: str
    runtime_id: str = ""
    description: str = ""
    version: str = ""
    entrypoint: str = ""
    scope: str = ""
    owner_user_id: str = ""
    content_sha256: str = ""
    bundle_object_key: str = ""
    skill_md: str = ""
    result_contract: dict | None = None

    @property
    def native_id(self) -> str:
        """Claude/Codex-visible identity; catalog ``id`` stays internal."""
        return self.runtime_id.strip() or self.id.strip()

    @classmethod
    def from_json(cls, d: dict) -> Skill:
        return cls(
            id=str(d.get("id", "")),
            name=str(d.get("name", "")),
            runtime_id=str(d.get("runtime_id", "")),
            description=str(d.get("description", "")),
            version=str(d.get("version", "")),
            entrypoint=str(d.get("entrypoint", "")),
            scope=str(d.get("scope", "")),
            owner_user_id=str(d.get("owner_user_id", "")),
            content_sha256=str(d.get("content_sha256", "")),
            bundle_object_key=str(d.get("bundle_object_key", "")),
            skill_md=str(d.get("skill_md", "")),
            result_contract=(
                dict(d["result_contract"]) if isinstance(d.get("result_contract"), dict) else None
            ),
        )


class SkillCatalog(Protocol):
    """The runtime depends on this Protocol only, never a concrete client."""

    def enabled_skills(self, user_id: str = "") -> list[Skill]:
        """Return the currently-enabled skills (may be empty; never None)."""
        ...

    def resolve_skills(self, user_id: str, catalog_ids: Sequence[str]) -> SkillResolution:
        """Resolve an Agent's explicit catalog IDs, failing closed per item."""
        ...


@dataclass(frozen=True)
class SkillResolution:
    skills: list[Skill]
    unavailable_ids: list[str]


# Injectable HTTP transport: (url, headers, timeout) -> response body bytes.
# Production uses _urllib_fetch; tests inject a fake returning canned JSON.
Fetcher = Callable[[str, dict[str, str], float], bytes]
Poster = Callable[[str, dict[str, str], bytes, float], bytes]


def _urllib_fetch(url: str, headers: dict[str, str], timeout: float) -> bytes:
    req = urllib.request.Request(url, headers=headers, method="GET")
    with urllib.request.urlopen(req, timeout=timeout) as resp:  # noqa: S310 - trusted internal URL
        return resp.read()


def _urllib_post(url: str, headers: dict[str, str], body: bytes, timeout: float) -> bytes:
    req = urllib.request.Request(url, headers=headers, data=body, method="POST")
    with urllib.request.urlopen(req, timeout=timeout) as resp:  # noqa: S310 - trusted internal URL
        return resp.read()


class AdminSkillCatalog:
    """SkillCatalog backed by the admin-api effective-skill endpoint.

    The admin key (if the admin-api has auth enabled) is sent as a bearer token,
    matching the admin-api's static-key middleware. Fetch and parse failures are
    propagated so the runtime cannot mistake an unavailable catalog for an
    authoritative empty set and expose stale sandbox skills.
    """

    def __init__(
        self,
        base_url: str,
        *,
        admin_key: str = "",
        timeout_s: float = 3.0,
        fetcher: Fetcher | None = None,
        poster: Poster | None = None,
    ) -> None:
        self._base = base_url.rstrip("/")
        self._admin_key = admin_key
        self._timeout = timeout_s
        self._fetch = fetcher or _urllib_fetch
        self._post = poster or _urllib_post

    def _headers(self) -> dict[str, str]:
        headers = {"Accept": "application/json"}
        if self._admin_key:
            headers["Authorization"] = "Bearer " + self._admin_key
        return headers

    def enabled_skills(self, user_id: str = "") -> list[Skill]:
        if user_id:
            url = self._base + "/admin/skills/effective?user_id=" + urllib.parse.quote(user_id)
        else:
            url = self._base + "/admin/skills?enabled=true"
        raw = self._fetch(url, self._headers(), self._timeout)
        payload = json.loads(raw)
        items = payload.get("skills") or []
        skills = [Skill.from_json(d) for d in items if isinstance(d, dict)]
        # Defensive re-filter: only enabled entries, only ones with an id.
        return [s for s in skills if s.id]

    def resolve_skills(self, user_id: str, catalog_ids: Sequence[str]) -> SkillResolution:
        requested = [str(value).strip() for value in catalog_ids]
        requested_ids = set(requested)
        if len(requested_ids) != len(requested) or any(not value for value in requested):
            raise ValueError("invalid requested Agent Skill catalog IDs")
        body = json.dumps(
            {"user_id": user_id, "catalog_ids": requested},
            separators=(",", ":"),
        ).encode()
        headers = self._headers()
        headers["Content-Type"] = "application/json"
        raw = self._post(self._base + "/admin/skills/resolve", headers, body, self._timeout)
        payload = json.loads(raw)
        if not isinstance(payload, dict):
            raise ValueError("invalid Agent Skill resolution response")
        items = payload.get("skills") or []
        unavailable = payload.get("unavailable_ids") or []
        if not isinstance(items, list) or not isinstance(unavailable, list):
            raise ValueError("invalid Agent Skill resolution response")
        skills = [Skill.from_json(d) for d in items if isinstance(d, dict)]
        resolved_ids = [skill.id for skill in skills]
        unavailable_ids = [
            value.strip() for value in unavailable if isinstance(value, str) and value.strip()
        ]
        if (
            len(skills) != len(items)
            or len(set(resolved_ids)) != len(resolved_ids)
            or len(set(unavailable_ids)) != len(unavailable_ids)
            or not set(resolved_ids).isdisjoint(unavailable_ids)
            or set(resolved_ids) | set(unavailable_ids) != requested_ids
        ):
            raise ValueError("invalid Agent Skill resolution response")
        return SkillResolution(
            skills=skills,
            unavailable_ids=unavailable_ids,
        )


class StaticSkillCatalog:
    """In-memory SkillCatalog for tests and dev (no admin-api needed)."""

    def __init__(self, skills: Sequence[Skill] | None = None) -> None:
        self._skills = list(skills or ())

    def enabled_skills(self, user_id: str = "") -> list[Skill]:
        return list(self._skills)

    def resolve_skills(self, user_id: str, catalog_ids: Sequence[str]) -> SkillResolution:
        del user_id
        by_id = {skill.id: skill for skill in self._skills}
        skills = [by_id[value] for value in catalog_ids if value in by_id]
        unavailable = [value for value in catalog_ids if value not in by_id]
        return SkillResolution(skills=skills, unavailable_ids=unavailable)
