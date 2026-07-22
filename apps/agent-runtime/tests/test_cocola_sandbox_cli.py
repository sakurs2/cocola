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
EXTENSION_LOCK_PATH = REPO_ROOT / "deploy" / "sandbox-runtime" / "code-server-extensions.lock.json"
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
    assert payload["editor"]["update_policy"] == "runtime-image-only"
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


def test_minimal_profile_disables_github_commands(cli, monkeypatch, capsys):
    monkeypatch.setenv("COCOLA_SANDBOX_PROFILE", "minimal")
    monkeypatch.setattr(
        cli,
        "run_github_command",
        lambda *args: (_ for _ in ()).throw(AssertionError("GitHub command must not run")),
    )
    assert cli.main(["github", "gh", "--", "repo", "view"]) == 1
    assert "disabled by the active sandbox profile" in capsys.readouterr().err


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
    monkeypatch.setattr(cli, "browser_command", lambda _contract: ["node", "browser-runner.js"])

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


def test_preview_start_detaches_scrubs_run_credentials_and_waits_for_network(
    cli, monkeypatch, tmp_path
):
    workspace = tmp_path / "workspace"
    application = workspace / "app"
    application.mkdir(parents=True)
    state_dir = tmp_path / "runtime" / "previews"
    manifest = {
        "workspace": {"root": str(workspace)},
        "capabilities": {"preview": {"state_dir": str(state_dir)}},
    }
    captured = {}

    class FakeProcess:
        pid = 4321

        @staticmethod
        def poll():
            return None

    def fake_popen(command, **kwargs):
        captured["command"] = command
        captured["kwargs"] = kwargs
        return FakeProcess()

    reachability = iter([False, False, True, True])
    monkeypatch.setattr(cli.subprocess, "Popen", fake_popen)
    monkeypatch.setattr(cli, "preview_process_identity", lambda _pid: ("S", "987"))
    monkeypatch.setattr(cli, "preview_port_reachable", lambda _port: next(reachability))
    monkeypatch.setattr(cli.time, "sleep", lambda _seconds: None)
    monkeypatch.setenv("ANTHROPIC_AUTH_TOKEN", "run-secret")
    monkeypatch.setenv("COCOLA_PROJECT_CREDENTIAL", "broker-secret")
    monkeypatch.setenv("APP_MODE", "development")

    payload = cli.preview_start(
        manifest,
        3000,
        str(application),
        ["--", "npm", "run", "dev", "--", "--hostname", "0.0.0.0"],
        5000,
    )

    assert payload["state"] == "ready"
    assert captured["command"][-7:] == [
        str(state_dir / "3000.log"),
        "npm",
        "run",
        "dev",
        "--",
        "--hostname",
        "0.0.0.0",
    ]
    assert captured["kwargs"]["cwd"] == application
    assert captured["kwargs"]["start_new_session"] is True
    assert captured["kwargs"]["stdin"] is subprocess.DEVNULL
    assert captured["kwargs"]["stdout"] is subprocess.DEVNULL
    assert "ANTHROPIC_AUTH_TOKEN" not in captured["kwargs"]["env"]
    assert "COCOLA_PROJECT_CREDENTIAL" not in captured["kwargs"]["env"]
    assert captured["kwargs"]["env"]["APP_MODE"] == "development"
    assert captured["kwargs"]["env"]["HOST"] == "0.0.0.0"
    assert captured["kwargs"]["env"]["PORT"] == "3000"
    state = json.loads((state_dir / "3000.json").read_text(encoding="utf-8"))
    assert state["pid"] == 4321
    assert state["start_ticks"] == "987"
    assert "command" not in state


def test_preview_runner_keeps_only_two_bounded_log_segments(cli, tmp_path):
    log_path = tmp_path / "preview.log"
    payload_bytes = cli.PREVIEW_LOG_MAX_BYTES * 3

    assert (
        cli.run_preview_process(
            str(log_path),
            [cli.sys.executable, "-c", f"import os; os.write(1, b'x' * {payload_bytes})"],
        )
        == 0
    )

    rotated = cli.preview_rotated_log_path(log_path)
    assert log_path.stat().st_size <= cli.PREVIEW_LOG_MAX_BYTES
    assert rotated.stat().st_size <= cli.PREVIEW_LOG_MAX_BYTES
    assert log_path.stat().st_size + rotated.stat().st_size <= cli.PREVIEW_LOG_MAX_BYTES * 2


def test_preview_stop_does_not_signal_a_reused_pid(cli, monkeypatch, tmp_path):
    workspace = tmp_path / "workspace"
    workspace.mkdir()
    state_dir = tmp_path / "previews"
    manifest = {
        "workspace": {"root": str(workspace)},
        "capabilities": {"preview": {"state_dir": str(state_dir)}},
    }
    cli.write_preview_state(
        manifest,
        {
            "schema_version": 1,
            "port": 3000,
            "pid": 4321,
            "start_ticks": "old-process",
            "cwd": str(workspace),
            "log_path": str(state_dir / "3000.log"),
            "started_at": 1,
        },
    )
    monkeypatch.setattr(cli, "preview_process_identity", lambda _pid: ("S", "new-process"))
    monkeypatch.setattr(
        cli.os,
        "killpg",
        lambda *_args: (_ for _ in ()).throw(AssertionError("must not signal a reused PID")),
    )

    assert cli.preview_stop(manifest, 3000)["state"] == "stopped"
    assert not (state_dir / "3000.json").exists()


def test_preview_cwd_must_remain_in_workspace(cli, tmp_path):
    workspace = tmp_path / "workspace"
    workspace.mkdir()
    manifest = {"workspace": {"root": str(workspace)}}

    assert cli.preview_workspace_cwd(manifest, str(workspace)) == workspace
    with pytest.raises(ValueError, match="must stay under"):
        cli.preview_workspace_cwd(manifest, str(tmp_path))


def test_github_command_reuses_one_request_across_approval(cli, monkeypatch):
    requests = []
    responses = iter(
        [
            (202, {"status": "pending", "approval_id": "approval-1"}),
            (200, {"status": "approved"}),
            (200, {"status": "ready", "lease_id": "lease-1", "token": "temporary"}),
            (204, {}),
        ]
    )

    def broker_request(method, path, body=None):
        requests.append((method, path, body))
        return next(responses)

    def run(command, **kwargs):
        assert kwargs["env"]["GH_TOKEN"] == "temporary"
        assert kwargs["env"]["GH_CONFIG_DIR"]
        return subprocess.CompletedProcess(command, 0)

    monkeypatch.setattr(cli, "_broker_request", broker_request)
    monkeypatch.setattr(cli.subprocess, "run", run)

    assert cli.run_github_command("gh", ["pr", "view", "42"], []) == 0
    first_payload = requests[0][2]
    approved_payload = requests[2][2]
    assert first_payload == approved_payload
    assert first_payload["request_id"]
    assert requests[-1][2] == {"result": "success"}


def test_github_command_reports_failed_result_without_persisting_token(cli, monkeypatch):
    requests = []

    def broker_request(method, path, body=None):
        requests.append((method, path, body))
        if method == "POST":
            return 200, {"status": "ready", "lease_id": "lease-2", "token": "temporary"}
        return 204, {}

    def run(command, **kwargs):
        assert kwargs["env"]["GH_TOKEN"] == "temporary"
        return subprocess.CompletedProcess(command, 1)

    monkeypatch.setattr(cli, "_broker_request", broker_request)
    monkeypatch.setattr(cli.subprocess, "run", run)

    assert cli.run_github_command("gh", ["issue", "create", "--title", "Bug"], []) == 1
    assert requests[-1][2] == {"result": "failed"}


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
    assert 'CODE_SERVER_EXTENSIONS_DIR="/opt/cocola/code-server/extensions"' in launcher
    assert "COCOLA_CODE_SERVER_EXTENSIONS_DIR" not in launcher
    assert 'CODE_SERVER_STATE_DIR="/session/runtime/cocola/code-server"' in launcher
    assert "COCOLA_CODE_SERVER_STATE_DIR" not in launcher
    assert 'XDG_CONFIG_HOME="$CODE_SERVER_CONFIG_DIR"' in launcher
    assert '--user-data-dir "$CODE_SERVER_USER_DATA_DIR"' in launcher
    assert '--extensions-dir "$CODE_SERVER_EXTENSIONS_DIR"' in launcher
    assert "autorestart=false" in supervisor
    assert "autorestart=unexpected" not in supervisor
    assert "startretries=3" in supervisor
    assert manifest["capabilities"]["browser"]["state_dir"] in browser_runner


def test_platform_code_server_extensions_and_language_tools_are_exactly_locked():
    lock = json.loads(EXTENSION_LOCK_PATH.read_text(encoding="utf-8"))
    manifest = json.loads(MANIFEST_PATH.read_text(encoding="utf-8"))
    dockerfile = (REPO_ROOT / "deploy" / "sandbox-runtime" / "Dockerfile").read_text(
        encoding="utf-8"
    )

    assert lock["schema_version"] == 1
    assert lock["code_server_version"] == "4.117.0"
    assert lock["code_version"] == "1.117.0"
    extensions = {item["id"]: item for item in lock["extensions"]}
    assert set(extensions) == {
        "ms-python.python",
        "detachhead.basedpyright",
        "charliermarsh.ruff",
        "golang.Go",
        "redhat.java",
        "llvm-vs-code-extensions.vscode-clangd",
        "mads-hartmann.bash-ide-vscode",
        "redhat.vscode-yaml",
        "DavidAnson.vscode-markdownlint",
        "yzhang.markdown-all-in-one",
    }
    assert "ms-python.vscode-pylance" not in extensions
    for extension in extensions.values():
        assert extension["version"] not in {"latest", "stable", "pre-release"}
        if extension["install"] in {"vsix", "vsix-unpacked"}:
            assert extension["version"] in extension["url"]
            assert re.fullmatch(r"[0-9a-f]{64}", extension["sha256"])
            if extension["install"] == "vsix-unpacked":
                assert extension["id"] == "ms-python.python"
        else:
            assert extension["id"] == "charliermarsh.ruff"
            assert extension["install"] == "platform-vsix"
            assert "{platform}" in extension["url"]
            assert "{platform}" in extension["sha256_url"]

    tools = {item["name"]: item for item in lock["language_tools"]}
    assert set(tools) == {"gopls", "clangd", "shellcheck", "shfmt", "java"}
    for tool in tools.values():
        assert tool["version"] not in {"latest", "stable"}

    assert manifest["editor"] == {
        "kind": "code-server",
        "extensions_dir": "/opt/cocola/code-server/extensions",
        "extensions_lock": "/opt/cocola/code-server-extensions.lock.json",
        "language_tools_dir": "/opt/cocola/toolchains/bin",
        "update_policy": "runtime-image-only",
    }
    assert (
        "COPY code-server-extensions.lock.json /opt/cocola/code-server-extensions.lock.json"
        in dockerfile
    )
    assert "COPY install-code-server-extensions.sh" in dockerfile
    assert "GOTOOLCHAIN=auto" in dockerfile


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


def test_builtin_preview_skill_matches_the_managed_process_contract():
    platform_manifest = json.loads(
        (BUILTIN_SKILLS_PATH / "manifest.json").read_text(encoding="utf-8")
    )
    descriptors = {item["id"]: item for item in platform_manifest["skills"]}
    descriptor = descriptors["cocola-sandbox-preview"]
    skill_md = (BUILTIN_SKILLS_PATH / descriptor["path"] / "SKILL.md").read_text(encoding="utf-8")
    manifest = json.loads(MANIFEST_PATH.read_text(encoding="utf-8"))

    assert descriptor == {
        "id": "cocola-sandbox-preview",
        "name": "Cocola Sandbox Preview",
        "version": "1.0.0",
        "path": "cocola-sandbox-preview",
    }
    assert "cocola-sandbox preview start" in skill_md
    assert "state: ready" in skill_md
    assert "run_in_background" in skill_md
    assert manifest["capabilities"]["preview"] == {
        "kind": "managed-user-process",
        "state_dir": "/session/runtime/cocola/previews",
        "required": False,
        "commands": ["start", "status", "stop", "logs"],
    }
    assert all(
        profile["capabilities"]["preview"]["enabled"] for profile in manifest["profiles"].values()
    )


def test_builtin_github_skill_and_wrapper_match_the_broker_contract():
    platform_manifest = json.loads(
        (BUILTIN_SKILLS_PATH / "manifest.json").read_text(encoding="utf-8")
    )
    descriptors = {item["id"]: item for item in platform_manifest["skills"]}
    descriptor = descriptors["cocola-github"]
    skill_md = (BUILTIN_SKILLS_PATH / descriptor["path"] / "SKILL.md").read_text(encoding="utf-8")
    runtime_manifest = json.loads(MANIFEST_PATH.read_text(encoding="utf-8"))
    wrapper = (REPO_ROOT / "deploy" / "sandbox-runtime" / "gh-wrapper.sh").read_text(
        encoding="utf-8"
    )
    dockerfile = (REPO_ROOT / "deploy" / "sandbox-runtime" / "Dockerfile").read_text(
        encoding="utf-8"
    )

    assert descriptor == {
        "id": "cocola-github",
        "name": "Cocola GitHub",
        "version": "1.0.0",
        "path": "cocola-github",
    }
    assert "github/awesome-copilot" in skill_md
    assert "cocola-sandbox github git" in skill_md
    assert "--permissions actions=write" in skill_md
    assert "auth)" in wrapper and "login|logout|refresh|setup-git|switch|token" in wrapper
    assert "exec cocola-sandbox github gh --" in wrapper
    assert "ARG GH_VERSION=2.94.0" in dockerfile
    assert runtime_manifest["capabilities"]["github"] == {
        "kind": "run-scoped-broker",
        "cli": "/usr/local/bin/gh",
        "real_cli": "/opt/cocola/gh/current/bin/gh",
        "persistent_auth": False,
        "required": False,
        "commands": ["gh", "git"],
    }
