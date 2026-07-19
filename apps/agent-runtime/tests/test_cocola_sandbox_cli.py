from __future__ import annotations

import importlib.util
import json
import re
import subprocess
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[3]
CLI_PATH = REPO_ROOT / "deploy" / "sandbox-runtime" / "cocola_sandbox.py"
MANIFEST_PATH = REPO_ROOT / "deploy" / "sandbox-runtime" / "runtime-manifest.json"
BUILTIN_SKILLS_PATH = REPO_ROOT / "deploy" / "sandbox-runtime" / "skills"


def load_cli():
    spec = importlib.util.spec_from_file_location("cocola_sandbox_cli", CLI_PATH)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


@pytest.fixture
def cli(monkeypatch: pytest.MonkeyPatch):
    module = load_cli()
    monkeypatch.setenv("COCOLA_RUNTIME_MANIFEST", str(MANIFEST_PATH))
    monkeypatch.delenv("COCOLA_SANDBOX_PROFILE", raising=False)
    monkeypatch.delenv("COCOLA_CODE_SERVER_ENABLED", raising=False)
    monkeypatch.delenv("COCOLA_BROWSER_ENABLED", raising=False)
    return module


def test_info_reports_coding_profile_and_ready_service(cli, monkeypatch, capsys):
    monkeypatch.setattr(
        cli.subprocess,
        "run",
        lambda *args, **kwargs: subprocess.CompletedProcess(
            args[0], 0, "code-server RUNNING pid 12, uptime 0:00:05\n", ""
        ),
    )

    assert cli.main(["info", "--json"]) == 0
    payload = json.loads(capsys.readouterr().out)
    assert payload["schema_version"] == 1
    assert payload["profile"] == "coding"
    assert payload["workspace"]["root"] == "/workspace"
    assert payload["services"][0]["state"] == "ready"
    assert payload["capabilities"][0]["name"] == "browser"
    assert payload["capabilities"][0]["enabled"] is True
    assert payload["capabilities"][1]["name"] == "artifacts"
    assert payload["capabilities"][1]["enabled"] is True


def test_minimal_profile_disables_code_server_without_supervisor(cli, monkeypatch, capsys):
    monkeypatch.setenv("COCOLA_SANDBOX_PROFILE", "minimal")

    def unexpected_run(*args, **kwargs):
        raise AssertionError("disabled service must not query supervisor")

    monkeypatch.setattr(cli.subprocess, "run", unexpected_run)
    assert cli.main(["service", "status", "--json"]) == 0
    payload = json.loads(capsys.readouterr().out)
    assert payload[0]["enabled"] is False
    assert payload[0]["state"] == "disabled"


def test_operator_override_takes_precedence_over_profile(cli, monkeypatch):
    manifest = cli.load_manifest()
    monkeypatch.setenv("COCOLA_CODE_SERVER_ENABLED", "true")
    assert cli.service_enabled(manifest, "minimal", "code-server") is True
    monkeypatch.setenv("COCOLA_CODE_SERVER_ENABLED", "false")
    assert cli.service_enabled(manifest, "coding", "code-server") is False
    monkeypatch.setenv("COCOLA_BROWSER_ENABLED", "true")
    assert cli.browser_enabled(manifest, "minimal") is True
    monkeypatch.setenv("COCOLA_BROWSER_ENABLED", "false")
    assert cli.browser_enabled(manifest, "coding") is False


def test_minimal_profile_disables_browser(cli, monkeypatch, capsys):
    monkeypatch.setenv("COCOLA_SANDBOX_PROFILE", "minimal")
    assert cli.main(["browser", "status", "--json"]) == 0
    payload = json.loads(capsys.readouterr().out)
    assert payload["enabled"] is False
    assert payload["state"] == "disabled"


def test_browser_inspect_runs_one_shot_runner(cli, monkeypatch, capsys):
    captured = {}
    monkeypatch.setattr(
        cli,
        "browser_status",
        lambda manifest, profile: {
            "enabled": True,
            "state": "ready",
            "detail": "ready",
        },
    )

    def fake_run(command, **kwargs):
        captured["command"] = command
        captured["request"] = json.loads(kwargs["input"])
        return subprocess.CompletedProcess(
            command,
            0,
            json.dumps(
                {
                    "ok": True,
                    "action": "inspect",
                    "url": "https://example.com/",
                    "title": "Example",
                    "text": "hello",
                    "links": [],
                }
            ),
            "",
        )

    monkeypatch.setattr(cli.subprocess, "run", fake_run)
    assert cli.main(["browser", "inspect", "https://example.com", "--json"]) == 0
    payload = json.loads(capsys.readouterr().out)
    assert payload["title"] == "Example"
    assert captured["request"] == {
        "action": "inspect",
        "url": "https://example.com",
        "timeout_ms": 30000,
        "viewport_width": 1440,
        "viewport_height": 900,
        "max_text_chars": 20000,
    }


def test_browser_output_is_scoped_to_workspace(cli, tmp_path):
    manifest = {
        "workspace": {"root": str(tmp_path)},
        "capabilities": {"browser": {"output_dir": str(tmp_path / "outputs" / "browser")}},
    }
    output = cli.browser_output_path(manifest, "screenshot", "page")
    assert output == str(tmp_path / "outputs" / "browser" / "page.png")
    with pytest.raises(ValueError, match="must stay under"):
        cli.browser_output_path(manifest, "pdf", str(tmp_path.parent / "escaped.pdf"))


def test_browser_output_keeps_logical_workspace_path(cli, tmp_path):
    session_workspace = tmp_path / "session" / "workspace"
    session_workspace.mkdir(parents=True)
    logical_workspace = tmp_path / "workspace"
    logical_workspace.symlink_to(session_workspace, target_is_directory=True)
    manifest = {
        "workspace": {"root": str(logical_workspace)},
        "capabilities": {"browser": {"output_dir": str(logical_workspace / "outputs" / "browser")}},
    }
    output = cli.browser_output_path(manifest, "screenshot", "page.png")
    assert output == str(logical_workspace / "outputs" / "browser" / "page.png")


def test_browser_rejects_non_http_navigation_before_launch(cli, monkeypatch, capsys):
    monkeypatch.setattr(
        cli.subprocess,
        "run",
        lambda *args, **kwargs: (_ for _ in ()).throw(
            AssertionError("invalid URL must not launch Browser")
        ),
    )
    assert cli.main(["browser", "inspect", "file:///workspace/index.html", "--json"]) == 1
    assert "must use http:// or https://" in capsys.readouterr().err


def test_root_browser_invocation_drops_to_cocola(cli, monkeypatch):
    monkeypatch.setattr(cli.os, "geteuid", lambda: 0)
    paths = {"node": "/usr/bin/node", "runuser": "/usr/sbin/runuser"}
    monkeypatch.setattr(cli.shutil, "which", lambda name: paths.get(name))
    command = cli.browser_command({"runner": "/opt/cocola/browser-runner.js"})
    assert command[:5] == ["/usr/sbin/runuser", "-u", "cocola", "--", "env"]
    assert command[-2:] == ["/usr/bin/node", "/opt/cocola/browser-runner.js"]


def test_workspace_info_uses_manifest_contract(cli, tmp_path):
    outputs = tmp_path / "outputs"
    outputs.mkdir()
    manifest = {
        "workspace": {
            "root": str(tmp_path),
            "outputs": str(outputs),
        }
    }
    payload = cli.workspace_info(manifest)
    assert payload["root"] == str(tmp_path)
    assert payload["paths"]["outputs"] == {
        "path": str(outputs),
        "exists": True,
        "writable": True,
    }


def test_artifact_list_reports_regular_files_and_skips_links(cli, tmp_path):
    outputs = tmp_path / "outputs"
    nested = outputs / "site"
    nested.mkdir(parents=True)
    (outputs / "report.md").write_text("# Report\n", encoding="utf-8")
    (nested / "index.html").write_text("<!doctype html><title>Site</title>", encoding="utf-8")
    (outputs / "linked.html").symlink_to(nested / "index.html")
    manifest = {
        "workspace": {"root": str(tmp_path)},
        "profiles": {"coding": {"capabilities": {"artifacts": {"enabled": True}}}},
        "capabilities": {
            "artifacts": {
                "kind": "workspace-output",
                "output_dir": str(outputs),
                "html_preview": "isolated-self-contained",
                "required": True,
                "commands": ["status", "list"],
            }
        },
    }

    payload = cli.artifact_files(manifest, "coding", 200)

    assert payload["root"] == str(outputs)
    assert payload["truncated"] is False
    assert [item["path"] for item in payload["files"]] == ["report.md", "site/index.html"]
    assert payload["files"][0]["mime_type"] == "text/markdown"
    assert payload["files"][1]["mime_type"] == "text/html"


def test_artifact_list_is_bounded(cli, tmp_path):
    outputs = tmp_path / "outputs"
    outputs.mkdir()
    for name in ("a.txt", "b.txt", "c.txt"):
        (outputs / name).write_text(name, encoding="utf-8")
    manifest = {
        "workspace": {"root": str(tmp_path)},
        "profiles": {"coding": {"capabilities": {"artifacts": {"enabled": True}}}},
        "capabilities": {"artifacts": {"output_dir": str(outputs)}},
    }

    payload = cli.artifact_files(manifest, "coding", 2)

    assert [item["path"] for item in payload["files"]] == ["a.txt", "b.txt"]
    assert payload["truncated"] is True


def test_artifact_output_must_be_workspace_scoped(cli, tmp_path):
    outside = tmp_path.parent / f"{tmp_path.name}-outside-artifacts"
    outside.mkdir(exist_ok=True)
    manifest = {
        "workspace": {"root": str(tmp_path)},
        "profiles": {"coding": {"capabilities": {"artifacts": {"enabled": True}}}},
        "capabilities": {"artifacts": {"output_dir": str(outside)}},
    }

    payload = cli.artifact_status(manifest, "coding")

    assert payload["state"] == "unavailable"
    assert payload["checks"]["workspace_scoped"] is False


def test_manifest_resource_defaults_match_sandbox_manager():
    manifest = json.loads(MANIFEST_PATH.read_text(encoding="utf-8"))
    source = (
        REPO_ROOT
        / "apps"
        / "sandbox-manager"
        / "internal"
        / "provider"
        / "opensandbox"
        / "opensandbox.go"
    ).read_text(encoding="utf-8")
    names = {
        ("coding", "cpu"): "defaultCodingCPU",
        ("coding", "memory"): "defaultCodingMemory",
        ("minimal", "cpu"): "defaultMinimalCPU",
        ("minimal", "memory"): "defaultMinimalMemory",
    }
    for (profile, resource), constant in names.items():
        value = manifest["profiles"][profile]["default_resources"][resource]
        assert re.search(rf'{constant}\s*=\s*"{re.escape(value)}"', source)


def test_supervised_launcher_cannot_be_replaced_or_raise_its_user():
    dockerfile = (REPO_ROOT / "deploy" / "sandbox-runtime" / "Dockerfile").read_text(
        encoding="utf-8"
    )
    launcher = (REPO_ROOT / "deploy" / "sandbox-runtime" / "code-server-launch.sh").read_text(
        encoding="utf-8"
    )
    manifest = json.loads(MANIFEST_PATH.read_text(encoding="utf-8"))
    browser_runner = (REPO_ROOT / "deploy" / "sandbox-runtime" / "browser-runner.js").read_text(
        encoding="utf-8"
    )
    supervisor = (REPO_ROOT / "deploy" / "sandbox-runtime" / "supervisord.conf").read_text(
        encoding="utf-8"
    )
    assert (
        "chown -R cocola:cocola /home/cocola /session /workspace /cache /opt/cocola"
        not in dockerfile
    )
    assert 'CODE_SERVER_USER="cocola"' in launcher
    assert "COCOLA_CODE_SERVER_USER" not in launcher
    assert "autorestart=false" in supervisor
    assert "autorestart=unexpected" not in supervisor
    assert "startretries=3" in supervisor
    assert manifest["capabilities"]["browser"]["state_dir"] in browser_runner


def test_builtin_browser_skill_matches_the_guest_cli_contract():
    platform_manifest = json.loads(
        (BUILTIN_SKILLS_PATH / "manifest.json").read_text(encoding="utf-8")
    )
    descriptor = platform_manifest["skills"][0]
    skill_md = (BUILTIN_SKILLS_PATH / descriptor["path"] / "SKILL.md").read_text(encoding="utf-8")
    dockerfile = (REPO_ROOT / "deploy" / "sandbox-runtime" / "Dockerfile").read_text(
        encoding="utf-8"
    )

    assert platform_manifest["schema_version"] == 1
    assert descriptor == {
        "id": "cocola-sandbox-browser",
        "name": "Cocola Sandbox Browser",
        "version": "1.0.0",
        "path": "cocola-sandbox-browser",
    }
    assert skill_md.startswith("---\nname: cocola-sandbox-browser\n")
    assert "cocola-sandbox browser status --json" in skill_md
    for command in ("inspect", "screenshot", "pdf"):
        assert f"cocola-sandbox browser {command}" in skill_md
    assert "COPY skills/ /opt/cocola/skills/" in dockerfile


def test_builtin_artifact_skill_matches_the_guest_cli_contract():
    platform_manifest = json.loads(
        (BUILTIN_SKILLS_PATH / "manifest.json").read_text(encoding="utf-8")
    )
    descriptors = {item["id"]: item for item in platform_manifest["skills"]}
    descriptor = descriptors["cocola-sandbox-artifacts"]
    skill_md = (BUILTIN_SKILLS_PATH / descriptor["path"] / "SKILL.md").read_text(encoding="utf-8")
    manifest = json.loads(MANIFEST_PATH.read_text(encoding="utf-8"))

    assert descriptor == {
        "id": "cocola-sandbox-artifacts",
        "name": "Cocola Sandbox Artifacts",
        "version": "1.0.0",
        "path": "cocola-sandbox-artifacts",
    }
    assert skill_md.startswith("---\nname: cocola-sandbox-artifacts\n")
    assert "cocola-sandbox artifact status --json" in skill_md
    assert "cocola-sandbox artifact list --json" in skill_md
    assert "self-contained `.html`" in skill_md
    assert manifest["capabilities"]["artifacts"] == {
        "kind": "workspace-output",
        "output_dir": "/workspace/outputs",
        "html_preview": "isolated-self-contained",
        "required": True,
        "commands": ["status", "list"],
    }
    assert all(
        profile["capabilities"]["artifacts"]["enabled"] for profile in manifest["profiles"].values()
    )
