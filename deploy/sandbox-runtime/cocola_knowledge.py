#!/opt/cocola/venv/bin/python
"""Controlled search and read access to the current Cocola Agent Knowledge revision."""

from __future__ import annotations

import argparse
import json
import os
import re
import signal
import subprocess
import tempfile
import time
import urllib.parse
from dataclasses import dataclass
from pathlib import Path, PurePosixPath
from typing import Any

DEFAULT_ROOT = "/workspace/knowledge"
MAX_MANIFEST_BYTES = 128 * 1024
MAX_STATE_BYTES = 128 * 1024
MAX_SOURCES = 10
MAX_QUERY_CHARS = 512
MAX_RESULTS = 50
MAX_SEARCH_OUTPUT_BYTES = 2 * 1024 * 1024
MAX_REMOTE_RESPONSE_BYTES = 12 * 1024 * 1024
MAX_REMOTE_CONTENT_BYTES = 8 * 1024 * 1024
MAX_READ_CHARS = 200_000
REMOTE_CACHE_TTL_SECONDS = 900
REMOTE_RETRY_BACKOFF_SECONDS = 60
REMOTE_FETCH_TIMEOUT_SECONDS = 15
SEARCH_TIMEOUT_SECONDS = 30
SOURCE_ID_RE = re.compile(r"[0-9a-f]{64}")
SAFE_SCOPE_RE = re.compile(r"[A-Za-z0-9:._/-]{1,160}")
REMOTE_TYPES = {"feishu_doc", "feishu_wiki", "feishu_sheet", "feishu_base"}
CONTENT_REMOTE_TYPES = {"feishu_doc", "feishu_wiki"}
SOURCE_TYPES = REMOTE_TYPES | {"cocola_wiki"}
SOURCE_STATUSES = {"ready", "stale", "temporarily_unavailable", "unavailable"}
TEXT_SUFFIXES = {
    ".csv",
    ".json",
    ".log",
    ".md",
    ".rst",
    ".text",
    ".tsv",
    ".txt",
    ".xml",
    ".yaml",
    ".yml",
}


class KnowledgeError(Exception):
    def __init__(self, code: str, message: str, **details: Any):
        super().__init__(message)
        self.code = code
        self.message = message
        self.details = details


@dataclass(frozen=True)
class KnowledgeContext:
    root: Path
    current: Path
    manifest: dict[str, Any]
    sources: list[dict[str, Any]]


@dataclass(frozen=True)
class CommandResult:
    returncode: int
    stdout: bytes
    timed_out: bool = False
    truncated: bool = False


def _knowledge_root() -> Path:
    root = Path(os.getenv("COCOLA_KNOWLEDGE_ROOT", DEFAULT_ROOT))
    if not root.is_absolute():
        raise KnowledgeError("KNOWLEDGE_INVALID_ROOT", "Knowledge root must be absolute.")
    return root


def _is_within(path: Path, parent: Path) -> bool:
    try:
        path.relative_to(parent)
        return True
    except ValueError:
        return False


def _load_json(path: Path, max_bytes: int) -> Any:
    try:
        size = path.stat().st_size
        if size < 0 or size > max_bytes:
            raise KnowledgeError("KNOWLEDGE_INVALID_STATE", "Knowledge state is too large.")
        return json.loads(path.read_text(encoding="utf-8"))
    except KnowledgeError:
        raise
    except (OSError, UnicodeError, ValueError, TypeError) as exc:
        raise KnowledgeError(
            "KNOWLEDGE_INVALID_STATE", "Knowledge state is missing or invalid."
        ) from exc


def _validate_remote_url(source_type: str, value: str) -> str:
    roots = {
        "feishu_doc": {"docx"},
        "feishu_wiki": {"wiki"},
        "feishu_sheet": {"sheets"},
        "feishu_base": {"base", "bitable"},
    }
    parsed = urllib.parse.urlsplit(value)
    host = (parsed.hostname or "").lower()
    parts = [part for part in parsed.path.split("/") if part]
    host_allowed = any(
        host == suffix or host.endswith("." + suffix)
        for suffix in ("feishu.cn", "larkoffice.com", "larksuite.com")
    )
    if (
        parsed.scheme != "https"
        or parsed.username is not None
        or parsed.password is not None
        or parsed.port is not None
        or not host_allowed
        or len(parts) != 2
        or parts[0].lower() not in roots[source_type]
        or not re.fullmatch(r"[A-Za-z0-9_-]{1,256}", parts[1])
        or parsed.query
        or parsed.fragment
    ):
        raise KnowledgeError("KNOWLEDGE_INVALID_STATE", "Knowledge source URL is invalid.")
    return value


def _safe_source_path(current: Path, relative_path: str, *, may_not_exist: bool) -> Path:
    logical = PurePosixPath(relative_path)
    if (
        not relative_path
        or logical.is_absolute()
        or ".." in logical.parts
        or "." in logical.parts
        or len(relative_path) > 512
    ):
        raise KnowledgeError("KNOWLEDGE_INVALID_STATE", "Knowledge source path is invalid.")
    candidate = current.joinpath(*logical.parts)
    try:
        resolved = candidate.resolve(strict=not may_not_exist)
    except OSError as exc:
        raise KnowledgeError(
            "KNOWLEDGE_SOURCE_UNAVAILABLE", "Knowledge source file is unavailable."
        ) from exc
    current_resolved = current.resolve(strict=True)
    if not _is_within(resolved, current_resolved):
        raise KnowledgeError("KNOWLEDGE_INVALID_STATE", "Knowledge source escapes its revision.")
    return candidate


def _validate_source(raw: Any, current: Path) -> dict[str, Any]:
    if not isinstance(raw, dict):
        raise KnowledgeError("KNOWLEDGE_INVALID_STATE", "Knowledge source is invalid.")
    source_id = str(raw.get("id") or "").strip().lower()
    source_type = str(raw.get("type") or "").strip()
    label = str(raw.get("label") or "").strip()
    status = str(raw.get("status") or "").strip()
    if (
        not SOURCE_ID_RE.fullmatch(source_id)
        or source_type not in SOURCE_TYPES
        or not label
        or len(label) > 100
        or any(ord(char) < 32 or ord(char) == 127 for char in label)
        or status not in SOURCE_STATUSES
    ):
        raise KnowledgeError("KNOWLEDGE_INVALID_STATE", "Knowledge source is invalid.")

    source: dict[str, Any] = {
        "id": source_id,
        "type": source_type,
        "label": label,
        "status": status,
    }
    if source_type in REMOTE_TYPES:
        source["url"] = _validate_remote_url(source_type, str(raw.get("url") or "").strip())
        if source_type in CONTENT_REMOTE_TYPES:
            expected = f"feishu/{source_id}.md"
            path_value = str(raw.get("path") or expected)
            if path_value != expected:
                raise KnowledgeError(
                    "KNOWLEDGE_INVALID_STATE", "Remote Knowledge cache path is invalid."
                )
            _safe_source_path(current, expected, may_not_exist=True)
            source["path"] = expected
        return source

    node_id = str(raw.get("node_id") or "").strip().lower()
    path_value = str(raw.get("path") or "").strip()
    if not re.fullmatch(
        r"[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}",
        node_id,
    ):
        raise KnowledgeError("KNOWLEDGE_INVALID_STATE", "Cocola Wiki source is invalid.")
    source.update(
        {
            "node_id": node_id,
            "path": path_value,
            "mime": str(raw.get("mime") or "").strip(),
            "version": str(raw.get("version") or "").strip(),
        }
    )
    if path_value:
        _safe_source_path(current, path_value, may_not_exist=status != "ready")
    elif status in {"ready", "stale"}:
        raise KnowledgeError("KNOWLEDGE_INVALID_STATE", "Cocola Wiki source has no file.")
    return source


def load_context(*, required: bool = True) -> KnowledgeContext | None:
    root = _knowledge_root()
    current_link = root / "current"
    if not current_link.exists():
        if required:
            raise KnowledgeError(
                "KNOWLEDGE_NOT_CONFIGURED", "No Agent Knowledge is configured for this session."
            )
        return None
    try:
        root_resolved = root.resolve(strict=True)
        current = current_link.resolve(strict=True)
    except OSError as exc:
        raise KnowledgeError(
            "KNOWLEDGE_INVALID_STATE", "Knowledge revision is unavailable."
        ) from exc
    if not current.is_dir() or not _is_within(current, root_resolved):
        raise KnowledgeError("KNOWLEDGE_INVALID_STATE", "Knowledge revision path is invalid.")
    manifest = _load_json(current / ".manifest.json", MAX_MANIFEST_BYTES)
    if (
        not isinstance(manifest, dict)
        or not isinstance(manifest.get("agent_id"), str)
        or not manifest["agent_id"].strip()
        or not isinstance(manifest.get("revision"), int)
        or manifest["revision"] <= 0
        or not isinstance(manifest.get("sources"), list)
        or len(manifest["sources"]) > MAX_SOURCES
    ):
        raise KnowledgeError("KNOWLEDGE_INVALID_STATE", "Knowledge manifest is invalid.")
    sources: list[dict[str, Any]] = []
    seen: set[str] = set()
    for raw in manifest["sources"]:
        source = _validate_source(raw, current)
        if source["id"] in seen:
            raise KnowledgeError("KNOWLEDGE_INVALID_STATE", "Knowledge source IDs are duplicated.")
        seen.add(source["id"])
        sources.append(source)
    return KnowledgeContext(root=root, current=current, manifest=manifest, sources=sources)


def _state_path(context: KnowledgeContext) -> Path:
    return context.root / ".remote-state.json"


def _read_remote_state(context: KnowledgeContext) -> dict[str, Any]:
    path = _state_path(context)
    if not path.is_file():
        return {"revision": context.manifest["revision"], "sources": {}}
    try:
        value = _load_json(path, MAX_STATE_BYTES)
    except KnowledgeError:
        return {"revision": context.manifest["revision"], "sources": {}}
    if not isinstance(value, dict) or not isinstance(value.get("sources"), dict):
        return {"revision": context.manifest["revision"], "sources": {}}
    active_ids = {source["id"] for source in context.sources}
    return {
        "revision": context.manifest["revision"],
        "sources": {
            source_id: state
            for source_id, state in value["sources"].items()
            if source_id in active_ids and isinstance(state, dict)
        },
    }


def _write_json_atomic(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    payload = json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode()
    with tempfile.NamedTemporaryFile(dir=path.parent, prefix=f".{path.name}.", delete=False) as tmp:
        tmp_path = Path(tmp.name)
        tmp.write(payload)
        tmp.flush()
        os.fsync(tmp.fileno())
    try:
        os.replace(tmp_path, path)
    finally:
        tmp_path.unlink(missing_ok=True)


def _update_remote_state(
    context: KnowledgeContext,
    source_id: str,
    *,
    status: str,
    fetched_at: float | None = None,
    code: str = "",
) -> None:
    value = _read_remote_state(context)
    entry: dict[str, Any] = {"status": status, "checked_at": int(time.time())}
    if fetched_at is not None:
        entry["fetched_at"] = int(fetched_at)
    if code:
        entry["code"] = code
    value["sources"][source_id] = entry
    _write_json_atomic(_state_path(context), value)


def _run_capped(command: list[str], *, timeout: float, max_bytes: int) -> CommandResult:
    with tempfile.TemporaryFile() as output:
        try:
            process = subprocess.Popen(
                command,
                stdin=subprocess.DEVNULL,
                stdout=output,
                stderr=subprocess.DEVNULL,
                start_new_session=True,
            )
        except OSError as exc:
            raise KnowledgeError(
                "KNOWLEDGE_TOOL_UNAVAILABLE", "A required Knowledge tool is unavailable."
            ) from exc
        deadline = time.monotonic() + max(timeout, 0.1)
        timed_out = False
        truncated = False
        while process.poll() is None:
            if time.monotonic() >= deadline:
                timed_out = True
                _kill_process_group(process)
                break
            if os.fstat(output.fileno()).st_size > max_bytes:
                truncated = True
                _kill_process_group(process)
                break
            time.sleep(0.02)
        process.wait()
        output.flush()
        output.seek(0)
        data = output.read(max_bytes)
        if output.read(1):
            truncated = True
        return CommandResult(
            returncode=process.returncode,
            stdout=data,
            timed_out=timed_out,
            truncated=truncated,
        )


def _kill_process_group(process: subprocess.Popen) -> None:
    if process.poll() is not None:
        return
    try:
        os.killpg(process.pid, signal.SIGKILL)
    except ProcessLookupError:
        return
    except OSError:
        process.kill()


def _parse_command_json(data: bytes) -> Any:
    try:
        text = data.decode("utf-8")
    except UnicodeError as exc:
        raise KnowledgeError(
            "KNOWLEDGE_REMOTE_INVALID_RESPONSE", "Remote Knowledge returned invalid data."
        ) from exc
    try:
        return json.loads(text)
    except ValueError:
        for line in reversed(text.splitlines()):
            try:
                return json.loads(line)
            except ValueError:
                continue
    raise KnowledgeError(
        "KNOWLEDGE_REMOTE_INVALID_RESPONSE", "Remote Knowledge returned invalid data."
    )


def _find_values(value: Any, keys: set[str]) -> list[Any]:
    found: list[Any] = []
    if isinstance(value, dict):
        for key, item in value.items():
            if str(key).lower() in keys:
                found.append(item)
            found.extend(_find_values(item, keys))
    elif isinstance(value, list):
        for item in value:
            found.extend(_find_values(item, keys))
    return found


def _safe_permission_details(payload: Any) -> dict[str, Any]:
    details: dict[str, Any] = {}
    scopes: list[str] = []
    for raw in _find_values(payload, {"missing_scopes", "missing_scope", "scopes"}):
        values = raw if isinstance(raw, list) else [raw]
        for value in values:
            scope = str(value).strip()
            if SAFE_SCOPE_RE.fullmatch(scope) and scope not in scopes:
                scopes.append(scope)
    if scopes:
        details["missing_scopes"] = scopes[:20]
    for raw in _find_values(payload, {"console_url", "authorization_url", "authorize_url"}):
        url = str(raw).strip()
        parsed = urllib.parse.urlsplit(url)
        if parsed.scheme == "https" and (parsed.hostname or "").lower() in {
            "open.feishu.cn",
            "open.larksuite.com",
        }:
            details["authorization_url"] = url
            break
    return details


def _classify_remote_error(payload: Any, returncode: int) -> tuple[str, str]:
    compact = json.dumps(payload, ensure_ascii=False).lower()
    if any(
        marker in compact
        for marker in (
            "missing_scope",
            "missing scopes",
            "permission denied",
            "access denied",
            "forbidden",
            "no permission",
        )
    ):
        return "KNOWLEDGE_PERMISSION_REQUIRED", "permission_required"
    if any(marker in compact for marker in ("not found", "not_found", "does not exist")):
        return "KNOWLEDGE_NOT_FOUND", "not_found"
    if any(
        marker in compact
        for marker in (
            "tenant_access_token",
            "connector",
            "not configured",
            "authentication required",
        )
    ):
        return "KNOWLEDGE_CONNECTOR_REQUIRED", "connector_required"
    if returncode != 0:
        return "KNOWLEDGE_REMOTE_UNAVAILABLE", "temporarily_unavailable"
    return "KNOWLEDGE_REMOTE_INVALID_RESPONSE", "temporarily_unavailable"


def _remote_cache_path(context: KnowledgeContext, source: dict[str, Any]) -> Path:
    return _safe_source_path(context.current, source["path"], may_not_exist=True)


def _remote_document(
    context: KnowledgeContext,
    source: dict[str, Any],
    *,
    deadline: float | None = None,
) -> tuple[Path, bool]:
    path = _remote_cache_path(context, source)
    state = _read_remote_state(context).get("sources", {}).get(source["id"], {})
    if source["status"] == "unavailable":
        raise KnowledgeError(
            "KNOWLEDGE_SOURCE_UNAVAILABLE", "This Knowledge source is unavailable."
        )
    if source["status"] in {"stale", "temporarily_unavailable"}:
        if path.is_file():
            return path, True
        raise KnowledgeError(
            "KNOWLEDGE_REMOTE_UNAVAILABLE", "Remote Knowledge is temporarily unavailable."
        )
    fetched_at = float(state.get("fetched_at") or 0)
    if path.is_file() and time.time() - fetched_at < REMOTE_CACHE_TTL_SECONDS:
        return path, False
    if (
        state.get("status") == "temporarily_unavailable"
        and path.is_file()
        and time.time() - float(state.get("checked_at") or 0) < REMOTE_RETRY_BACKOFF_SECONDS
    ):
        return path, True

    timeout = float(REMOTE_FETCH_TIMEOUT_SECONDS)
    if deadline is not None:
        timeout = min(timeout, deadline - time.monotonic())
        if timeout <= 0.1:
            if path.is_file():
                return path, True
            raise KnowledgeError(
                "KNOWLEDGE_REMOTE_UNAVAILABLE", "Remote Knowledge search timed out."
            )
    command = [
        "lark-cli",
        "docs",
        "+fetch",
        "--doc",
        source["url"],
        "--doc-format",
        "markdown",
        "--detail",
        "simple",
        "--as",
        "bot",
        "--format",
        "json",
    ]
    result = _run_capped(command, timeout=timeout, max_bytes=MAX_REMOTE_RESPONSE_BYTES)
    if result.timed_out or result.truncated:
        _update_remote_state(
            context,
            source["id"],
            status="temporarily_unavailable",
            code="KNOWLEDGE_REMOTE_UNAVAILABLE",
        )
        if path.is_file():
            return path, True
        raise KnowledgeError(
            "KNOWLEDGE_REMOTE_UNAVAILABLE", "Remote Knowledge is temporarily unavailable."
        )
    try:
        payload = _parse_command_json(result.stdout)
    except KnowledgeError:
        payload = {}
    document = (
        payload.get("data", {}).get("document", {})
        if isinstance(payload, dict) and isinstance(payload.get("data"), dict)
        else {}
    )
    content = document.get("content") if isinstance(document, dict) else None
    if (
        result.returncode == 0
        and isinstance(payload, dict)
        and payload.get("ok") is True
        and isinstance(content, str)
    ):
        encoded = content.encode("utf-8")
        if len(encoded) > MAX_REMOTE_CONTENT_BYTES:
            path.unlink(missing_ok=True)
            _update_remote_state(
                context,
                source["id"],
                status="unavailable",
                code="KNOWLEDGE_SOURCE_TOO_LARGE",
            )
            raise KnowledgeError(
                "KNOWLEDGE_SOURCE_TOO_LARGE", "Remote Knowledge exceeds the read limit."
            )
        path.parent.mkdir(parents=True, exist_ok=True)
        _write_bytes_atomic(path, encoded)
        fetched_at = time.time()
        _update_remote_state(context, source["id"], status="ready", fetched_at=fetched_at)
        return path, False

    code, status = _classify_remote_error(payload, result.returncode)
    _update_remote_state(context, source["id"], status=status, code=code)
    if status in {"permission_required", "not_found", "connector_required"}:
        path.unlink(missing_ok=True)
    if path.is_file():
        return path, True
    details = _safe_permission_details(payload) if status == "permission_required" else {}
    raise KnowledgeError(code, _remote_error_message(status), **details)


def _write_bytes_atomic(path: Path, content: bytes) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(dir=path.parent, prefix=f".{path.name}.", delete=False) as tmp:
        tmp_path = Path(tmp.name)
        tmp.write(content)
        tmp.flush()
        os.fsync(tmp.fileno())
    try:
        os.replace(tmp_path, path)
    finally:
        tmp_path.unlink(missing_ok=True)


def _remote_error_message(status: str) -> str:
    return {
        "permission_required": "The Feishu app needs permission to read this Knowledge source.",
        "not_found": "This remote Knowledge source was not found.",
        "connector_required": "A ready Feishu Connector is required for this Knowledge source.",
    }.get(status, "Remote Knowledge is temporarily unavailable.")


def _source_file(
    context: KnowledgeContext,
    source: dict[str, Any],
    *,
    deadline: float | None = None,
) -> tuple[Path | None, bool]:
    if source["status"] == "unavailable":
        return None, False
    if source["type"] in CONTENT_REMOTE_TYPES:
        return _remote_document(context, source, deadline=deadline)
    if source["type"] in {"feishu_sheet", "feishu_base"}:
        return None, False
    path_value = str(source.get("path") or "")
    if not path_value:
        return None, False
    path = _safe_source_path(
        context.current,
        path_value,
        may_not_exist=source["status"] != "ready",
    )
    return (path if path.is_file() else None), source["status"] == "stale"


def _query(value: str) -> str:
    query = value.strip()
    if (
        not query
        or len(query) > MAX_QUERY_CHARS
        or any(ord(char) < 32 and char not in "\t\n\r" for char in query)
    ):
        raise KnowledgeError(
            "KNOWLEDGE_INVALID_QUERY", f"Query must contain 1-{MAX_QUERY_CHARS} characters."
        )
    return query


def _rga_results(
    query: str,
    paths: dict[str, dict[str, Any]],
    *,
    limit: int,
    deadline: float,
) -> list[dict[str, Any]]:
    if not paths or limit <= 0:
        return []
    remaining = deadline - time.monotonic()
    if remaining <= 0.1:
        return []
    command = [
        "rga",
        "--json",
        "--rga-no-cache",
        "--fixed-strings",
        "--ignore-case",
        "--context",
        "1",
        "--max-count",
        "20",
        "--max-columns",
        "1000",
        "--max-columns-preview",
        "--",
        query,
        *paths,
    ]
    result = _run_capped(
        command,
        timeout=remaining,
        max_bytes=MAX_SEARCH_OUTPUT_BYTES,
    )
    if result.timed_out:
        raise KnowledgeError(
            "KNOWLEDGE_SEARCH_TIMEOUT", "Knowledge search exceeded its time limit."
        )
    if result.returncode not in {0, 1, -9}:
        raise KnowledgeError(
            "KNOWLEDGE_SEARCH_UNAVAILABLE", "Knowledge search is temporarily unavailable."
        )
    results: list[dict[str, Any]] = []
    for line in result.stdout.decode("utf-8", errors="replace").splitlines():
        try:
            event = json.loads(line)
        except ValueError:
            continue
        if not isinstance(event, dict) or event.get("type") != "match":
            continue
        data = event.get("data")
        if not isinstance(data, dict):
            continue
        raw_path = str((data.get("path") or {}).get("text") or "")
        source = paths.get(raw_path)
        if source is None:
            source = next(
                (item for path, item in paths.items() if raw_path.startswith(path + ":")),
                None,
            )
        if source is None:
            continue
        snippet = " ".join(str((data.get("lines") or {}).get("text") or "").split())
        if not snippet:
            continue
        results.append(
            {
                "source_id": source["id"],
                "source_type": source["type"],
                "label": source["label"],
                "kind": "content",
                "line": int(data.get("line_number") or 0),
                "snippet": snippet[:1000],
            }
        )
        if len(results) >= limit:
            break
    return results


def search(context: KnowledgeContext, query_value: str, limit: int) -> dict[str, Any]:
    query = _query(query_value)
    if limit < 1 or limit > MAX_RESULTS:
        raise KnowledgeError(
            "KNOWLEDGE_INVALID_LIMIT", f"Limit must be between 1 and {MAX_RESULTS}."
        )
    deadline = time.monotonic() + SEARCH_TIMEOUT_SECONDS
    results: list[dict[str, Any]] = []
    unavailable: list[dict[str, str]] = []
    paths: dict[str, dict[str, Any]] = {}
    stale_ids: set[str] = set()
    lowered = query.casefold()
    for source in context.sources:
        if source["status"] == "unavailable":
            unavailable.append(
                {
                    "source_id": source["id"],
                    "label": source["label"],
                    "status": "unavailable",
                }
            )
            continue
        if lowered in source["label"].casefold() and len(results) < limit:
            results.append(
                {
                    "source_id": source["id"],
                    "source_type": source["type"],
                    "label": source["label"],
                    "kind": "reference",
                    "snippet": source["label"],
                }
            )
        try:
            path, stale = _source_file(context, source, deadline=deadline)
        except KnowledgeError as exc:
            unavailable.append(
                {
                    "source_id": source["id"],
                    "label": source["label"],
                    "status": exc.code,
                }
            )
            continue
        if stale:
            stale_ids.add(source["id"])
        if path is not None:
            paths[str(path)] = source
    results.extend(
        _rga_results(
            query,
            paths,
            limit=max(0, limit - len(results)),
            deadline=deadline,
        )
    )
    return {
        "ok": True,
        "revision": context.manifest["revision"],
        "stale": bool(stale_ids),
        "results": results[:limit],
        "unavailable_sources": unavailable,
    }


def _read_text(path: Path, max_chars: int) -> tuple[str, bool]:
    with path.open("rb") as source:
        data = source.read(max_chars * 4 + 1)
    text = data.decode("utf-8", errors="replace")
    return text[:max_chars], len(text) > max_chars or len(data) > max_chars * 4


def _extract_file(
    path: Path,
    max_chars: int,
    *,
    sheet: str = "",
    cell_range: str = "",
    xlsx_mode: str = "formula",
) -> tuple[str, bool, str]:
    suffix = path.suffix.lower()
    if suffix in TEXT_SUFFIXES:
        content, truncated = _read_text(path, max_chars)
        return content, truncated, "text"
    command: list[str] | None = None
    kind = "file"
    if suffix == ".pdf":
        command = ["pdftotext", "-layout", str(path), "-"]
        kind = "pdf_text"
    elif suffix == ".docx":
        command = ["cocola-wiki-read", "docx", str(path)]
        kind = "document_text"
    elif suffix == ".pptx":
        command = ["cocola-wiki-read", "pptx", str(path)]
        kind = "presentation_text"
    elif suffix == ".xlsx":
        if sheet and cell_range:
            command = [
                "cocola-wiki-read",
                "xlsx-range",
                str(path),
                "--sheet",
                sheet,
                "--range",
                cell_range,
                "--mode",
                xlsx_mode,
            ]
            kind = "workbook_range"
        else:
            command = ["cocola-wiki-read", "xlsx-info", str(path)]
            kind = "workbook_info"
    if command is None:
        return "", False, kind
    result = _run_capped(command, timeout=15, max_bytes=max_chars * 4 + 1)
    if result.timed_out or result.returncode != 0:
        raise KnowledgeError("KNOWLEDGE_READ_UNAVAILABLE", "Knowledge source could not be read.")
    text = result.stdout.decode("utf-8", errors="replace")
    return text[:max_chars], result.truncated or len(text) > max_chars, kind


def read_source(
    context: KnowledgeContext,
    source_id: str,
    max_chars: int,
    *,
    sheet: str = "",
    cell_range: str = "",
    xlsx_mode: str = "formula",
) -> dict[str, Any]:
    if not SOURCE_ID_RE.fullmatch(source_id):
        raise KnowledgeError("KNOWLEDGE_INVALID_SOURCE", "Knowledge source ID is invalid.")
    if max_chars < 1 or max_chars > MAX_READ_CHARS:
        raise KnowledgeError(
            "KNOWLEDGE_INVALID_LIMIT",
            f"Read limit must be between 1 and {MAX_READ_CHARS} characters.",
        )
    source = next((item for item in context.sources if item["id"] == source_id), None)
    if source is None:
        raise KnowledgeError("KNOWLEDGE_SOURCE_NOT_FOUND", "Knowledge source was not found.")
    if source["status"] == "unavailable":
        raise KnowledgeError(
            "KNOWLEDGE_SOURCE_UNAVAILABLE", "This Knowledge source is unavailable."
        )
    if source["type"] in {"feishu_sheet", "feishu_base"}:
        return {
            "ok": True,
            "revision": context.manifest["revision"],
            "source_id": source["id"],
            "source_type": source["type"],
            "label": source["label"],
            "kind": "remote_reference",
            "url": source["url"],
            "required_skill": ("lark-sheets" if source["type"] == "feishu_sheet" else "lark-base"),
            "stale": source["status"] == "stale",
        }
    path, stale = _source_file(context, source)
    if path is None:
        raise KnowledgeError(
            "KNOWLEDGE_SOURCE_UNAVAILABLE", "Knowledge source content is unavailable."
        )
    if bool(sheet) != bool(cell_range):
        raise KnowledgeError(
            "KNOWLEDGE_INVALID_ARGUMENT",
            "XLSX reads require both a sheet name and a cell range.",
        )
    if (sheet or cell_range) and path.suffix.lower() != ".xlsx":
        raise KnowledgeError(
            "KNOWLEDGE_INVALID_ARGUMENT",
            "Sheet and range options are only supported for XLSX sources.",
        )
    content, truncated, kind = _extract_file(
        path,
        max_chars,
        sheet=sheet,
        cell_range=cell_range,
        xlsx_mode=xlsx_mode,
    )
    payload: dict[str, Any] = {
        "ok": True,
        "revision": context.manifest["revision"],
        "source_id": source["id"],
        "source_type": source["type"],
        "label": source["label"],
        "kind": kind,
        "stale": stale,
        "truncated": truncated,
    }
    if content:
        payload["content"] = content
        if kind == "workbook_info":
            payload["requires_range"] = True
            payload["hint"] = (
                "Run cocola-knowledge read again with "
                f'--source {source["id"]} --sheet "Sheet name" '
                '--range "A1:B20" --mode cached --json to read cell values.'
            )
    else:
        payload["path"] = str(path)
        payload["hint"] = "Use the matching built-in document Skill to inspect this file."
    return payload


def status(context: KnowledgeContext | None) -> dict[str, Any]:
    if context is None:
        return {"ok": True, "configured": False, "revision": 0, "stale": False, "sources": []}
    try:
        runtime_state = _load_json(context.root / ".state.json", MAX_STATE_BYTES)
    except KnowledgeError:
        runtime_state = {}
    remote_state = _read_remote_state(context).get("sources", {})
    sources: list[dict[str, Any]] = []
    for source in context.sources:
        remote = remote_state.get(source["id"], {})
        effective_status = str(remote.get("status") or source["status"])
        sources.append(
            {
                "source_id": source["id"],
                "source_type": source["type"],
                "label": source["label"],
                "status": effective_status,
            }
        )
    return {
        "ok": True,
        "configured": True,
        "agent_id": context.manifest["agent_id"],
        "revision": context.manifest["revision"],
        "stale": bool(runtime_state.get("stale"))
        or any(item["status"] == "stale" for item in sources),
        "sources": sources,
    }


def _emit(payload: dict[str, Any]) -> None:
    print(json.dumps(payload, ensure_ascii=False, separators=(",", ":")))


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Search the current Agent Knowledge revision.")
    commands = parser.add_subparsers(dest="command", required=True)
    status_parser = commands.add_parser("status")
    status_parser.add_argument("--json", action="store_true")
    search_parser = commands.add_parser("search")
    search_parser.add_argument("--query", required=True)
    search_parser.add_argument("--limit", type=int, default=8)
    search_parser.add_argument("--json", action="store_true")
    read_parser = commands.add_parser("read")
    read_parser.add_argument("--source", required=True)
    read_parser.add_argument("--max-chars", type=int, default=50_000)
    read_parser.add_argument("--sheet", default="")
    read_parser.add_argument("--range", dest="cell_range", default="")
    read_parser.add_argument("--mode", choices=("formula", "cached"), default="formula")
    read_parser.add_argument("--json", action="store_true")
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    try:
        context = load_context(required=args.command != "status")
        if args.command == "status":
            payload = status(context)
        elif args.command == "search":
            assert context is not None
            payload = search(context, args.query, args.limit)
        else:
            assert context is not None
            payload = read_source(
                context,
                args.source.strip().lower(),
                args.max_chars,
                sheet=args.sheet,
                cell_range=args.cell_range,
                xlsx_mode=args.mode,
            )
        _emit(payload)
        return 0
    except KnowledgeError as exc:
        _emit({"ok": False, "code": exc.code, "error": exc.message, **exc.details})
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
