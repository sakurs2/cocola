from __future__ import annotations

import importlib.util
import json
import sys
import time
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[3]
CLI_PATH = REPO_ROOT / "deploy" / "sandbox-runtime" / "cocola_knowledge.py"
SKILLS_PATH = REPO_ROOT / "deploy" / "sandbox-runtime" / "skills"
SOURCE_ID = "a" * 64
WIKI_NODE_ID = "3d594650-e540-4fe5-a8b5-4ccfdbb5dcdc"


def load_cli():
    spec = importlib.util.spec_from_file_location("cocola_knowledge_cli", CLI_PATH)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


@pytest.fixture
def cli(monkeypatch: pytest.MonkeyPatch, tmp_path: Path):
    module = load_cli()
    root = tmp_path / "knowledge"
    current = root / "current"
    current.mkdir(parents=True)
    monkeypatch.setenv("COCOLA_KNOWLEDGE_ROOT", str(root))
    return module, root, current


def write_manifest(current: Path, sources: list[dict], *, revision: int = 3) -> None:
    (current / ".manifest.json").write_text(
        json.dumps(
            {
                "agent_id": "agent-1",
                "revision": revision,
                "sources": sources,
            }
        ),
        encoding="utf-8",
    )


def cocola_source(path: str = "cocola-wiki/source/handbook.md") -> dict:
    return {
        "id": SOURCE_ID,
        "type": "cocola_wiki",
        "label": "Employee handbook",
        "url": "",
        "node_id": WIKI_NODE_ID,
        "status": "ready",
        "version": "version-1",
        "mime": "text/markdown",
        "path": path,
    }


def remote_source(*, status: str = "ready") -> dict:
    return {
        "id": SOURCE_ID,
        "type": "feishu_doc",
        "label": "Sales playbook",
        "url": "https://example.larkoffice.com/docx/AbCd_123",
        "node_id": "",
        "status": status,
        "path": f"feishu/{SOURCE_ID}.md",
    }


def test_status_without_a_collection_is_a_stable_empty_result(
    cli, monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
):
    module, root, _current = cli
    monkeypatch.setenv("COCOLA_KNOWLEDGE_ROOT", str(root / "missing"))

    assert module.main(["status", "--json"]) == 0
    assert json.loads(capsys.readouterr().out) == {
        "ok": True,
        "configured": False,
        "revision": 0,
        "stale": False,
        "sources": [],
    }


def test_search_uses_fixed_strings_without_rga_cache(cli, monkeypatch: pytest.MonkeyPatch):
    module, _root, current = cli
    source_path = current / "cocola-wiki" / "source" / "handbook.md"
    source_path.parent.mkdir(parents=True)
    source_path.write_text("The retention policy is ninety days.\n", encoding="utf-8")
    write_manifest(current, [cocola_source()])
    captured: dict[str, list[str]] = {}

    def fake_run(command, **_kwargs):
        captured["command"] = command
        event = {
            "type": "match",
            "data": {
                "path": {"text": str(source_path)},
                "lines": {"text": "The retention policy is ninety days.\n"},
                "line_number": 1,
            },
        }
        return module.CommandResult(0, (json.dumps(event) + "\n").encode())

    monkeypatch.setattr(module, "_run_capped", fake_run)
    context = module.load_context()

    payload = module.search(context, "retention", 8)

    assert payload["results"] == [
        {
            "source_id": SOURCE_ID,
            "source_type": "cocola_wiki",
            "label": "Employee handbook",
            "kind": "content",
            "line": 1,
            "snippet": "The retention policy is ninety days.",
        }
    ]
    command = captured["command"]
    assert command[0] == "rga"
    assert "--rga-no-cache" in command
    assert "--fixed-strings" in command
    assert command[-2:] == ["retention", str(source_path)]


def test_manifest_cannot_escape_the_active_revision(cli, tmp_path: Path):
    module, _root, current = cli
    (tmp_path / "secret.md").write_text("secret", encoding="utf-8")
    write_manifest(current, [cocola_source("../../secret.md")])

    with pytest.raises(module.KnowledgeError) as error:
        module.load_context()

    assert error.value.code == "KNOWLEDGE_INVALID_STATE"


def test_remote_document_is_fetched_once_and_cached(cli, monkeypatch: pytest.MonkeyPatch):
    module, _root, current = cli
    write_manifest(current, [remote_source()])
    calls: list[list[str]] = []

    def fake_run(command, **_kwargs):
        calls.append(command)
        payload = {
            "ok": True,
            "data": {"document": {"revision_id": 7, "content": "# Sales\nUse discovery."}},
        }
        return module.CommandResult(0, json.dumps(payload).encode())

    monkeypatch.setattr(module, "_run_capped", fake_run)
    context = module.load_context()

    first = module.read_source(context, SOURCE_ID, 50_000)
    second = module.read_source(context, SOURCE_ID, 50_000)

    assert first["content"] == "# Sales\nUse discovery."
    assert second["content"] == first["content"]
    assert first["stale"] is False
    assert len(calls) == 1
    assert calls[0][:3] == ["lark-cli", "docs", "+fetch"]
    assert "--as" in calls[0] and "bot" in calls[0]
    assert (current / "feishu" / f"{SOURCE_ID}.md").is_file()


def test_explicit_remote_permission_failure_removes_cached_content(
    cli, monkeypatch: pytest.MonkeyPatch
):
    module, root, current = cli
    write_manifest(current, [remote_source()])
    cached = current / "feishu" / f"{SOURCE_ID}.md"
    cached.parent.mkdir(parents=True)
    cached.write_text("old content", encoding="utf-8")
    (root / ".remote-state.json").write_text(
        json.dumps(
            {
                "revision": 3,
                "sources": {
                    SOURCE_ID: {"status": "ready", "fetched_at": 1},
                },
            }
        ),
        encoding="utf-8",
    )
    response = {
        "ok": False,
        "error": {
            "code": "missing_scope",
            "missing_scopes": ["docx:document:readonly"],
            "console_url": "https://open.feishu.cn/app/cli-test",
        },
    }
    monkeypatch.setattr(
        module,
        "_run_capped",
        lambda *_args, **_kwargs: module.CommandResult(1, json.dumps(response).encode()),
    )
    context = module.load_context()

    with pytest.raises(module.KnowledgeError) as error:
        module.read_source(context, SOURCE_ID, 50_000)

    assert error.value.code == "KNOWLEDGE_PERMISSION_REQUIRED"
    assert error.value.details == {
        "missing_scopes": ["docx:document:readonly"],
        "authorization_url": "https://open.feishu.cn/app/cli-test",
    }
    assert not cached.exists()


def test_temporarily_unavailable_remote_source_uses_revision_cache(cli):
    module, _root, current = cli
    write_manifest(current, [remote_source(status="stale")])
    cached = current / "feishu" / f"{SOURCE_ID}.md"
    cached.parent.mkdir(parents=True)
    cached.write_text("last known good", encoding="utf-8")
    context = module.load_context()

    payload = module.read_source(context, SOURCE_ID, 50_000)

    assert payload["content"] == "last known good"
    assert payload["stale"] is True


def test_run_capped_kills_child_processes_after_timeout(cli, tmp_path: Path):
    module, _root, _current = cli
    marker = tmp_path / "child-survived"
    child_code = (
        "import pathlib, sys, time; "
        "time.sleep(0.4); "
        "pathlib.Path(sys.argv[1]).write_text('survived', encoding='utf-8')"
    )
    parent_code = (
        "import subprocess, sys, time; "
        "subprocess.Popen([sys.executable, '-c', sys.argv[1], sys.argv[2]]); "
        "time.sleep(10)"
    )

    result = module._run_capped(
        [sys.executable, "-c", parent_code, child_code, str(marker)],
        timeout=0.1,
        max_bytes=1024,
    )

    assert result.timed_out is True
    time.sleep(0.6)
    assert not marker.exists()


def test_xlsx_read_explains_how_to_request_cell_content(cli, monkeypatch: pytest.MonkeyPatch):
    module, _root, current = cli
    workbook = current / "cocola-wiki" / "source" / "report.xlsx"
    workbook.parent.mkdir(parents=True)
    workbook.touch()
    write_manifest(current, [cocola_source("cocola-wiki/source/report.xlsx")])
    monkeypatch.setattr(
        module,
        "_run_capped",
        lambda *_args, **_kwargs: module.CommandResult(
            0,
            json.dumps({"sheets": [{"name": "Summary", "max_row": 20, "max_column": 4}]}).encode(),
        ),
    )

    payload = module.read_source(module.load_context(), SOURCE_ID, 50_000)

    assert payload["kind"] == "workbook_info"
    assert payload["requires_range"] is True
    assert "--sheet" in payload["hint"]
    assert "--range" in payload["hint"]


def test_xlsx_read_forwards_a_bounded_sheet_range(cli, monkeypatch: pytest.MonkeyPatch):
    module, _root, current = cli
    workbook = current / "cocola-wiki" / "source" / "report.xlsx"
    workbook.parent.mkdir(parents=True)
    workbook.touch()
    write_manifest(current, [cocola_source("cocola-wiki/source/report.xlsx")])
    captured: dict[str, list[str]] = {}

    def fake_run(command, **_kwargs):
        captured["command"] = command
        return module.CommandResult(0, b"quarter,revenue\r\nQ1,120\r\n")

    monkeypatch.setattr(module, "_run_capped", fake_run)

    payload = module.read_source(
        module.load_context(),
        SOURCE_ID,
        50_000,
        sheet="Summary",
        cell_range="A1:B2",
        xlsx_mode="cached",
    )

    assert payload["kind"] == "workbook_range"
    assert payload["content"] == "quarter,revenue\r\nQ1,120\r\n"
    assert captured["command"] == [
        "cocola-wiki-read",
        "xlsx-range",
        str(workbook),
        "--sheet",
        "Summary",
        "--range",
        "A1:B2",
        "--mode",
        "cached",
    ]


def test_builtin_knowledge_skill_and_rga_match_the_image_contract():
    platform_manifest = json.loads((SKILLS_PATH / "manifest.json").read_text(encoding="utf-8"))
    descriptors = {item["id"]: item for item in platform_manifest["skills"]}
    descriptor = descriptors["cocola-knowledge"]
    skill_md = (SKILLS_PATH / descriptor["path"] / "SKILL.md").read_text(encoding="utf-8")
    dockerfile = (REPO_ROOT / "deploy" / "sandbox-runtime" / "Dockerfile").read_text(
        encoding="utf-8"
    )

    assert descriptor == {
        "id": "cocola-knowledge",
        "name": "Cocola Knowledge",
        "version": "1.0.0",
        "path": "cocola-knowledge",
    }
    assert skill_md.startswith("---\nname: cocola-knowledge\n")
    for command in ("status", "search", "read"):
        assert f"cocola-knowledge {command}" in skill_md
    assert "ARG RGA_VERSION=0.10.10" in dockerfile
    assert "a969c25b182ac84aa672518313b5f741091decf7d93d03a020bcfe517b9ff4e8" in dockerfile
    assert "2cd875ab6c78b27e4830b5bca92c570a8a8dcfb368bc71b189b65ac01fbc3020" in dockerfile
    assert "COPY cocola_knowledge.py /opt/cocola/cocola_knowledge.py" in dockerfile
    assert "ln -sf /opt/cocola/cocola_knowledge.py /usr/local/bin/cocola-knowledge" in dockerfile
