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

Only `Enabled` entries are ever returned — the admin-api already filters via
`?enabled=true`, and we filter again defensively so a catalog change cannot leak
a disabled skill into a session.
"""

from __future__ import annotations

import dataclasses
import json
import urllib.parse
import urllib.request
from collections.abc import Callable, Sequence
from dataclasses import dataclass
from typing import Protocol

from cocola_common import get_logger

from cocola_agent_runtime.agent_provider import AgentOptions

log = get_logger("cocola.agent-runtime.skills")


@dataclass(frozen=True)
class Skill:
    """One enabled Skill-Market capability, as the runtime consumes it.

    Mirrors the admin-api store.Skill JSON. `entrypoint` is the module/path the
    runtime will hand to the agent; `id`/`name`/`description` describe it to the
    model and to operators.
    """

    id: str
    name: str
    description: str = ""
    version: str = ""
    entrypoint: str = ""
    scope: str = ""
    content_sha256: str = ""
    bundle_object_key: str = ""
    skill_md: str = ""

    @classmethod
    def from_json(cls, d: dict) -> Skill:
        return cls(
            id=str(d.get("id", "")),
            name=str(d.get("name", "")),
            description=str(d.get("description", "")),
            version=str(d.get("version", "")),
            entrypoint=str(d.get("entrypoint", "")),
            scope=str(d.get("scope", "")),
            content_sha256=str(d.get("content_sha256", "")),
            bundle_object_key=str(d.get("bundle_object_key", "")),
            skill_md=str(d.get("skill_md", "")),
        )


class SkillCatalog(Protocol):
    """The runtime depends on this Protocol only, never a concrete client."""

    def enabled_skills(self, user_id: str = "") -> list[Skill]:
        """Return the currently-enabled skills (may be empty; never None)."""
        ...


# Injectable HTTP transport: (url, headers, timeout) -> response body bytes.
# Production uses _urllib_fetch; tests inject a fake returning canned JSON.
Fetcher = Callable[[str, dict[str, str], float], bytes]


def _urllib_fetch(url: str, headers: dict[str, str], timeout: float) -> bytes:
    req = urllib.request.Request(url, headers=headers, method="GET")
    with urllib.request.urlopen(req, timeout=timeout) as resp:  # noqa: S310 - trusted internal URL
        return resp.read()


class AdminSkillCatalog:
    """SkillCatalog backed by the admin-api `GET /admin/skills?enabled=true`.

    The admin key (if the admin-api has auth enabled) is sent as a bearer token,
    matching the admin-api's static-key middleware. A fetch failure returns an
    empty list (logged): a control-plane blip must not break session startup —
    the agent simply runs with no market skills until the next refresh.
    """

    def __init__(
        self,
        base_url: str,
        *,
        admin_key: str = "",
        timeout_s: float = 3.0,
        fetcher: Fetcher | None = None,
    ) -> None:
        self._base = base_url.rstrip("/")
        self._admin_key = admin_key
        self._timeout = timeout_s
        self._fetch = fetcher or _urllib_fetch

    def enabled_skills(self, user_id: str = "") -> list[Skill]:
        if user_id:
            url = self._base + "/admin/skills/effective?user_id=" + urllib.parse.quote(user_id)
        else:
            url = self._base + "/admin/skills?enabled=true"
        headers = {"Accept": "application/json"}
        if self._admin_key:
            headers["Authorization"] = "Bearer " + self._admin_key
        try:
            raw = self._fetch(url, headers, self._timeout)
            payload = json.loads(raw)
        except Exception as exc:  # noqa: BLE001 - degrade gracefully on any transport/parse error
            log.warning("skill catalog fetch failed; running with no market skills", error=str(exc))
            return []
        items = payload.get("skills") or []
        skills = [Skill.from_json(d) for d in items if isinstance(d, dict)]
        # Defensive re-filter: only enabled entries, only ones with an id.
        return [s for s in skills if s.id]


class StaticSkillCatalog:
    """In-memory SkillCatalog for tests and dev (no admin-api needed)."""

    def __init__(self, skills: Sequence[Skill] | None = None) -> None:
        self._skills = list(skills or ())

    def enabled_skills(self, user_id: str = "") -> list[Skill]:
        return list(self._skills)


def skills_system_preamble(skills: Sequence[Skill]) -> str:
    """Render enabled skills as a system-prompt preamble for the agent.

    This is the transport-neutral consumption point: rather than coupling to a
    specific SDK tool-registration API (which varies by SDK version), we surface
    the enabled capabilities to the model in the system prompt. Mapping a skill's
    entrypoint to a concrete SDK tool/MCP server is a follow-up; listing them is
    what makes an enabled skill observable to the agent today.

    Returns an empty string when there are no skills, so callers can append
    unconditionally without introducing a blank section.
    """
    enabled = [s for s in skills if s.id]
    if not enabled:
        return ""
    lines = ["Available cocola skills (enabled by your administrator):"]
    for s in enabled:
        label = s.name or s.id
        ver = f" v{s.version}" if s.version else ""
        desc = f" — {s.description}" if s.description else ""
        lines.append(f"- {label}{ver}{desc}")
    return "\n".join(lines)


def apply_skills_to_options(options: AgentOptions, catalog: SkillCatalog) -> AgentOptions:
    """Return a copy of `options` with the enabled-skills preamble merged in.

    This is the runtime's consumption seam: the M2 gRPC server builds base
    `AgentOptions`, then calls this to fold the control-plane's enabled skills
    into the system prompt before handing the options to a provider. The base
    system prompt (if any) is preserved and appended after the preamble.

    A catalog failure degrades to no skills (the catalog itself logs and returns
    an empty list), so this never raises on a control-plane blip — `options` is
    returned effectively unchanged.
    """
    preamble = skills_system_preamble(catalog.enabled_skills(options.user_id or ""))
    if not preamble:
        return options
    base = options.system_prompt or ""
    merged = preamble + "\n\n" + base if base else preamble
    return dataclasses.replace(options, system_prompt=merged)
