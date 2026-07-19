"""Materialize effective Skills into a sandbox-owned, persistent Skill Set."""

from __future__ import annotations

import asyncio
import hashlib
import io
import json
import re
import zipfile
from typing import Any

from cocola_agent_runtime.objstore import Fetcher
from cocola_agent_runtime.skill_loader import Skill

SKILLS_INSPECT_SCRIPT = r"""
import hashlib
import json
import os
import shutil

state_root = "/home/cocola/.cocola/skillsets/agents-skill-v1"
current = os.path.join(state_root, "current")
platform_root = "/opt/cocola/skills"


def replace_link(path, target):
    if os.path.islink(path) and os.readlink(path) == target:
        return
    if os.path.lexists(path):
        if os.path.isdir(path) and not os.path.islink(path):
            shutil.rmtree(path)
        else:
            os.unlink(path)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    os.symlink(target, path)


def platform_skills():
    manifest_path = os.path.join(platform_root, "manifest.json")
    if not os.path.isfile(manifest_path):
        return [], ""
    try:
        with open(manifest_path, encoding="utf-8") as src:
            platform_manifest = json.load(src)
    except (OSError, ValueError) as exc:
        raise SystemExit(f"invalid platform Skill manifest: {exc}")
    if platform_manifest.get("schema_version") != 1 or not isinstance(
        platform_manifest.get("skills"), list
    ):
        raise SystemExit("invalid platform Skill manifest schema")
    root = os.path.realpath(platform_root)
    items = []
    seen = set()
    for raw in platform_manifest["skills"]:
        if not isinstance(raw, dict):
            raise SystemExit("invalid platform Skill descriptor")
        skill_id = str(raw.get("id") or "")
        if not skill_id or not all(ch.isalnum() or ch in "_.-" for ch in skill_id):
            raise SystemExit(f"invalid platform Skill id: {skill_id}")
        if skill_id in seen:
            raise SystemExit(f"duplicate platform Skill id: {skill_id}")
        seen.add(skill_id)
        relative = str(raw.get("path") or skill_id)
        source = os.path.realpath(os.path.join(root, relative))
        if os.path.commonpath((root, source)) != root:
            raise SystemExit(f"unsafe platform Skill path: {relative}")
        skill_md = os.path.join(source, "SKILL.md")
        if not os.path.isfile(skill_md):
            raise SystemExit(f"platform Skill missing SKILL.md: {skill_id}")
        with open(skill_md, "rb") as src:
            content_sha256 = hashlib.sha256(src.read()).hexdigest()
        items.append(
            {
                "id": skill_id,
                "name": str(raw.get("name") or skill_id),
                "version": str(raw.get("version") or ""),
                "path": relative,
                "content_sha256": content_sha256,
            }
        )
    items.sort(key=lambda item: item["id"])
    encoded = json.dumps(items, sort_keys=True, separators=(",", ":")).encode()
    return items, hashlib.sha256(encoded).hexdigest()


os.makedirs(state_root, exist_ok=True)
replace_link("/home/cocola/.claude/skills", current)
replace_link("/home/cocola/.agents/skills", current)
manifest = {}
try:
    with open(os.path.join(current, "manifest.json"), encoding="utf-8") as src:
        manifest = json.load(src)
except (OSError, ValueError):
    pass
available_platform_skills, available_platform_digest = platform_skills()
manifest["available_platform_skills"] = available_platform_skills
manifest["available_platform_digest"] = available_platform_digest
print(json.dumps(manifest, sort_keys=True, separators=(",", ":")))
"""

SKILLS_RECONCILE_SCRIPT = r"""
import fcntl
import hashlib
import io
import json
import os
import shutil
import sys
import tempfile
import zipfile

archive_path, digest = sys.argv[1:3]
state_root = "/home/cocola/.cocola/skillsets/agents-skill-v1"
sets_root = os.path.join(state_root, "sets")
current = os.path.join(state_root, "current")
lock_path = os.path.join(state_root, "reconcile.lock")
platform_root = "/opt/cocola/skills"
os.makedirs(sets_root, exist_ok=True)


def extract_bundle(data, target):
    os.makedirs(target, exist_ok=True)
    with zipfile.ZipFile(io.BytesIO(data)) as bundle:
        for info in bundle.infolist():
            name = info.filename.replace("\\", "/")
            if not name or name.startswith("/") or ".." in name.split("/"):
                raise SystemExit(f"unsafe skill archive path: {name}")
            if info.is_dir():
                continue
            dest = os.path.join(target, name)
            os.makedirs(os.path.dirname(dest), exist_ok=True)
            with bundle.open(info) as src, open(dest, "wb") as out:
                shutil.copyfileobj(src, out)


def platform_skills():
    manifest_path = os.path.join(platform_root, "manifest.json")
    if not os.path.isfile(manifest_path):
        return [], "", {}
    try:
        with open(manifest_path, encoding="utf-8") as src:
            platform_manifest = json.load(src)
    except (OSError, ValueError) as exc:
        raise SystemExit(f"invalid platform Skill manifest: {exc}")
    if platform_manifest.get("schema_version") != 1 or not isinstance(
        platform_manifest.get("skills"), list
    ):
        raise SystemExit("invalid platform Skill manifest schema")
    root = os.path.realpath(platform_root)
    items = []
    sources = {}
    seen = set()
    for raw in platform_manifest["skills"]:
        if not isinstance(raw, dict):
            raise SystemExit("invalid platform Skill descriptor")
        skill_id = str(raw.get("id") or "")
        if not skill_id or not all(ch.isalnum() or ch in "_.-" for ch in skill_id):
            raise SystemExit(f"invalid platform Skill id: {skill_id}")
        if skill_id in seen:
            raise SystemExit(f"duplicate platform Skill id: {skill_id}")
        seen.add(skill_id)
        relative = str(raw.get("path") or skill_id)
        source = os.path.realpath(os.path.join(root, relative))
        if os.path.commonpath((root, source)) != root:
            raise SystemExit(f"unsafe platform Skill path: {relative}")
        skill_md = os.path.join(source, "SKILL.md")
        if not os.path.isfile(skill_md):
            raise SystemExit(f"platform Skill missing SKILL.md: {skill_id}")
        with open(skill_md, "rb") as src:
            content_sha256 = hashlib.sha256(src.read()).hexdigest()
        items.append(
            {
                "id": skill_id,
                "name": str(raw.get("name") or skill_id),
                "version": str(raw.get("version") or ""),
                "path": relative,
                "content_sha256": content_sha256,
            }
        )
        sources[skill_id] = source
    items.sort(key=lambda item: item["id"])
    encoded = json.dumps(items, sort_keys=True, separators=(",", ":")).encode()
    return items, hashlib.sha256(encoded).hexdigest(), sources


with open(lock_path, "a+") as lock:
    fcntl.flock(lock, fcntl.LOCK_EX)
    stage = tempfile.mkdtemp(prefix=f"{digest}-", dir=sets_root)
    old_set_name = ""
    try:
        if os.path.islink(current):
            old_set_name = os.path.basename(os.path.realpath(current))
        old_set = os.path.realpath(current)
        with zipfile.ZipFile(archive_path) as batch:
            manifest = json.loads(batch.read("manifest.json"))
            if manifest.get("digest") != digest or not isinstance(manifest.get("skills"), list):
                raise SystemExit("invalid skill reconcile manifest")
            configured_ids = set()
            for item in manifest["skills"]:
                skill_id = item.get("id", "")
                if not skill_id or not all(ch.isalnum() or ch in "_.-" for ch in skill_id):
                    raise SystemExit(f"invalid skill id: {skill_id}")
                configured_ids.add(skill_id)
                target = os.path.join(stage, skill_id)
                kind = item.get("kind")
                if kind == "reuse":
                    source = os.path.join(old_set, skill_id)
                    if not os.path.isfile(os.path.join(source, "SKILL.md")):
                        raise SystemExit(f"reusable skill missing: {skill_id}")
                    shutil.copytree(source, target, symlinks=True)
                elif kind == "bundle":
                    extract_bundle(batch.read(item["member"]), target)
                elif kind == "markdown":
                    os.makedirs(target, exist_ok=True)
                    with open(os.path.join(target, "SKILL.md"), "wb") as out:
                        out.write(batch.read(item["member"]))
                else:
                    raise SystemExit(f"invalid skill payload kind: {kind}")
                if not os.path.isfile(os.path.join(target, "SKILL.md")):
                    raise SystemExit(f"skill archive missing SKILL.md: {skill_id}")
            platform_items, platform_digest, platform_sources = platform_skills()
            for item in platform_items:
                skill_id = item["id"]
                if skill_id in configured_ids:
                    raise SystemExit(f"configured Skill uses reserved platform id: {skill_id}")
                os.symlink(platform_sources[skill_id], os.path.join(stage, skill_id))
            manifest["platform_skills"] = platform_items
            manifest["platform_digest"] = platform_digest
            with open(os.path.join(stage, "manifest.json"), "w", encoding="utf-8") as out:
                json.dump(manifest, out, sort_keys=True, separators=(",", ":"))

        new_set_name = os.path.basename(stage)
        next_link = os.path.join(state_root, ".current-next")
        if os.path.lexists(next_link):
            os.unlink(next_link)
        os.symlink(os.path.join("sets", new_set_name), next_link)
        os.replace(next_link, current)
        stage = ""

        keep = {new_set_name, old_set_name}
        for name in os.listdir(sets_root):
            path = os.path.join(sets_root, name)
            if name not in keep and os.path.isdir(path):
                shutil.rmtree(path, ignore_errors=True)
    finally:
        if stage:
            shutil.rmtree(stage, ignore_errors=True)
        try:
            os.unlink(archive_path)
        except OSError:
            pass
"""

_NATIVE_SKILL_ID_RE = re.compile(r"^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$")
_SKILL_SET_FORMAT = "session-bundle-v2"


def platform_skill_descriptors(snapshot: dict[str, Any]) -> tuple[list[dict[str, str]], str]:
    """Return validated root-owned Skill metadata reported by the Runtime image."""

    raw_items = snapshot.get("available_platform_skills")
    if raw_items is None:
        raw_items = []
    if not isinstance(raw_items, list):
        raise RuntimeError("invalid platform Skill inventory")
    descriptors: list[dict[str, str]] = []
    seen_ids: set[str] = set()
    for raw in raw_items:
        if not isinstance(raw, dict):
            raise RuntimeError("invalid platform Skill descriptor")
        skill_id = str(raw.get("id") or "").strip()
        if not _NATIVE_SKILL_ID_RE.fullmatch(skill_id):
            raise RuntimeError(f"invalid platform Skill id: {skill_id}")
        if skill_id in seen_ids:
            raise RuntimeError(f"duplicate platform Skill id: {skill_id}")
        seen_ids.add(skill_id)
        descriptors.append(
            {
                "id": skill_id,
                "name": str(raw.get("name") or skill_id).strip() or skill_id,
                "version": str(raw.get("version") or "").strip(),
            }
        )
    descriptors.sort(key=lambda item: item["id"])
    return descriptors, str(snapshot.get("available_platform_digest") or "")


def loaded_skill_metadata(
    skills: list[Skill], platform_skills: list[dict[str, str]]
) -> list[dict[str, str]]:
    """Merge configured and image-baked Skills for runtime/UI status reporting."""

    configured_ids = {skill.native_id for skill in skills}
    platform_ids = {item["id"] for item in platform_skills}
    collisions = sorted(configured_ids & platform_ids)
    if collisions:
        raise RuntimeError(f"configured Skill uses reserved platform id: {collisions[0]}")
    loaded = [
        {"id": skill.native_id, "name": skill.name or skill.native_id, "version": skill.version}
        for skill in skills
    ]
    loaded.extend(platform_skills)
    loaded.sort(key=lambda item: item["id"])
    return loaded


def skill_descriptors(skills: list[Skill], user_id: str) -> tuple[list[dict[str, str]], str]:
    descriptors: list[dict[str, str]] = []
    seen_ids: set[str] = set()
    for skill in skills:
        skill_id = skill.native_id
        if not _NATIVE_SKILL_ID_RE.fullmatch(skill_id):
            raise RuntimeError(f"invalid Runtime skill id: {skill_id}")
        if skill_id in seen_ids:
            raise RuntimeError(f"duplicate normalized skill id: {skill_id}")
        seen_ids.add(skill_id)
        owner = skill.owner_user_id.strip()
        if not owner and skill.scope.strip().lower() in {"user", "personal"}:
            owner = user_id
        descriptors.append(
            {
                "id": skill_id,
                "catalog_id": skill.id,
                "native_id": skill_id,
                "version": skill.version,
                "content_sha256": skill.content_sha256,
                "scope": skill.scope,
                "owner_identity": owner,
            }
        )
    descriptors.sort(key=lambda item: item["id"])
    encoded = json.dumps(
        {"format": _SKILL_SET_FORMAT, "skills": descriptors},
        sort_keys=True,
        separators=(",", ":"),
    ).encode()
    return descriptors, hashlib.sha256(encoded).hexdigest()


async def build_skill_batch_archive(
    skills: list[Skill],
    objstore: Fetcher | None,
    descriptors: list[dict[str, str]],
    digest: str,
    previous: dict[str, Any],
) -> bytes:
    """Build the desired manifest while downloading only changed payloads."""

    async def load_payload(skill: Skill) -> tuple[str, bytes]:
        if skill.bundle_object_key and objstore is not None:
            data = await asyncio.to_thread(objstore.get, skill.bundle_object_key)
            if skill.content_sha256:
                actual = hashlib.sha256(data).hexdigest()
                if actual != skill.content_sha256.lower():
                    raise RuntimeError(f"skill bundle checksum mismatch: {skill.native_id}")
            return "bundle", data
        if skill.skill_md:
            return "markdown", skill.skill_md.encode("utf-8")
        return "empty", b""

    skills_by_id = {skill.native_id: skill for skill in skills}
    previous_items = previous.get("skills") if isinstance(previous, dict) else None
    previous_by_id = {
        item.get("id"): item
        for item in (previous_items if isinstance(previous_items, list) else [])
        if isinstance(item, dict) and isinstance(item.get("id"), str)
    }
    reusable: set[str] = set()
    changed: list[Skill] = []
    for descriptor in descriptors:
        old = previous_by_id.get(descriptor["id"])
        if old is not None and all(old.get(key) == value for key, value in descriptor.items()):
            reusable.add(descriptor["id"])
        else:
            changed.append(skills_by_id[descriptor["id"]])
    changed_payloads = await asyncio.gather(*(load_payload(skill) for skill in changed))
    payload_by_id = {
        skill.native_id: payload for skill, payload in zip(changed, changed_payloads, strict=True)
    }
    manifest: list[dict[str, str]] = []
    out = io.BytesIO()
    with zipfile.ZipFile(out, "w", compression=zipfile.ZIP_DEFLATED) as batch:
        for index, descriptor in enumerate(descriptors):
            skill_id = descriptor["id"]
            entry = dict(descriptor)
            if skill_id in reusable:
                entry["kind"] = "reuse"
            else:
                kind, data = payload_by_id[skill_id]
                if kind == "empty":
                    raise RuntimeError(f"skill has no SKILL.md or bundle: {skill_id}")
                entry["kind"] = kind
                suffix = "zip" if kind == "bundle" else "md"
                member = f"payloads/{index:04d}.{suffix}"
                batch.writestr(
                    member,
                    data,
                    compress_type=(
                        zipfile.ZIP_STORED if kind == "bundle" else zipfile.ZIP_DEFLATED
                    ),
                )
                entry["member"] = member
            manifest.append(entry)
        batch.writestr(
            "manifest.json",
            json.dumps(
                {"format": _SKILL_SET_FORMAT, "digest": digest, "skills": manifest},
                ensure_ascii=False,
                sort_keys=True,
                separators=(",", ":"),
            ),
        )
    return out.getvalue()
