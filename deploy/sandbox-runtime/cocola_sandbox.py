#!/usr/bin/env python3
"""Guest-facing CLI for the Cocola sandbox runtime contract."""

from __future__ import annotations

import argparse
import json
import mimetypes
import os
import shutil
import stat
import subprocess
import sys
import tempfile
import time
import uuid
from contextlib import suppress
from pathlib import Path
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit
from urllib.request import Request, urlopen

DEFAULT_MANIFEST = "/opt/cocola/runtime-manifest.json"
DEFAULT_SUPERVISOR_CONFIG = "/opt/cocola/supervisord.conf"
VALID_PROFILES = {"coding", "minimal"}
TRUE_VALUES = {"1", "true", "yes", "on"}
FALSE_VALUES = {"0", "false", "no", "off"}


def load_manifest() -> dict[str, Any]:
    path = Path(os.environ.get("COCOLA_RUNTIME_MANIFEST", DEFAULT_MANIFEST))
    with path.open(encoding="utf-8") as handle:
        manifest = json.load(handle)
    if manifest.get("schema_version") != 1:
        raise ValueError(f"unsupported runtime manifest schema: {manifest.get('schema_version')!r}")
    return manifest


def active_profile(manifest: dict[str, Any]) -> str:
    profile = os.environ.get("COCOLA_SANDBOX_PROFILE", "coding").strip().lower()
    if profile not in VALID_PROFILES or profile not in manifest.get("profiles", {}):
        raise ValueError(f"unsupported sandbox profile: {profile!r}")
    return profile


def parse_bool(value: str) -> bool:
    normalized = value.strip().lower()
    if normalized in TRUE_VALUES:
        return True
    if normalized in FALSE_VALUES:
        return False
    raise ValueError(f"invalid boolean value: {value!r}")


def service_enabled(manifest: dict[str, Any], profile: str, service: str) -> bool:
    if service == "code-server" and os.environ.get("COCOLA_CODE_SERVER_ENABLED", ""):
        return parse_bool(os.environ["COCOLA_CODE_SERVER_ENABLED"])
    profile_service = manifest["profiles"][profile].get("services", {}).get(service, {})
    return bool(profile_service.get("enabled", False))


def browser_enabled(manifest: dict[str, Any], profile: str) -> bool:
    if os.environ.get("COCOLA_BROWSER_ENABLED", ""):
        return parse_bool(os.environ["COCOLA_BROWSER_ENABLED"])
    profile_capability = manifest["profiles"][profile].get("capabilities", {}).get("browser", {})
    return bool(profile_capability.get("enabled", False))


def artifact_enabled(manifest: dict[str, Any], profile: str) -> bool:
    profile_capability = manifest["profiles"][profile].get("capabilities", {}).get("artifacts", {})
    return bool(profile_capability.get("enabled", False))


def github_enabled(manifest: dict[str, Any], profile: str) -> bool:
    profile_capability = manifest["profiles"][profile].get("capabilities", {}).get("github", {})
    return bool(profile_capability.get("enabled", False))


def _broker_request(
    method: str, path: str, body: dict[str, Any] | None = None
) -> tuple[int, dict[str, Any]]:
    base_url = os.environ.get("COCOLA_PROJECT_BROKER_URL", "").strip().rstrip("/")
    credential = os.environ.get("COCOLA_PROJECT_CREDENTIAL", "").strip()
    if not base_url or not credential:
        raise RuntimeError("GitHub broker is unavailable outside an authenticated Project run")
    raw = None if body is None else json.dumps(body, separators=(",", ":")).encode()
    request = Request(base_url + path, data=raw, method=method)
    request.add_header("Authorization", "Bearer " + credential)
    request.add_header("Accept", "application/json")
    if raw is not None:
        request.add_header("Content-Type", "application/json")
    try:
        with urlopen(request, timeout=35) as response:
            payload = json.loads(response.read() or b"{}")
            return int(response.status), payload
    except HTTPError as error:
        payload: dict[str, Any] = {}
        with suppress(ValueError):
            payload = json.loads(error.read() or b"{}")
        if error.code == 202:
            return error.code, payload
        message = payload.get("error", {}).get("message") or f"broker request failed ({error.code})"
        raise RuntimeError(message) from error
    except URLError as error:
        raise RuntimeError("GitHub broker could not be reached") from error


def run_github_command(kind: str, arguments: list[str], permissions: list[str]) -> int:
    argv = list(arguments)
    if argv and argv[0] == "--":
        argv = argv[1:]
    if not argv:
        raise ValueError("a GitHub command is required after --")
    if kind == "git" and argv[0] != "push":
        raise ValueError("the brokered Git command only supports push")

    request_payload = {
        "operation": kind,
        "argv": argv,
        "permissions": permissions,
        "request_id": str(uuid.uuid4()),
    }
    status, lease = _broker_request("POST", "/internal/scm/github/token-leases", request_payload)
    if status == 202 or lease.get("status") == "pending":
        approval_id = str(lease.get("approval_id") or "")
        if not approval_id:
            raise RuntimeError("the broker returned an invalid approval request")
        deadline = time.monotonic() + 300
        while time.monotonic() < deadline:
            _, decision = _broker_request("GET", f"/internal/scm/approvals/{approval_id}/wait")
            decision_status = str(decision.get("status") or "")
            if decision_status == "approved":
                status, lease = _broker_request(
                    "POST", "/internal/scm/github/token-leases", request_payload
                )
                break
            if decision_status in {"denied", "expired"}:
                raise RuntimeError(f"the user {decision_status} this GitHub command")
        else:
            raise RuntimeError("GitHub command approval timed out")
    token = str(lease.get("token") or "")
    lease_id = str(lease.get("lease_id") or "")
    if not token or not lease_id:
        raise RuntimeError("GitHub broker returned an incomplete lease")

    command_result = "failed"
    try:
        with tempfile.TemporaryDirectory(prefix="cocola-github-") as temp_dir:
            env = os.environ.copy()
            env.update(
                {
                    "GH_TOKEN": token,
                    "GH_HOST": "github.com",
                    "GH_REPO": os.environ.get("COCOLA_PROJECT_REPOSITORY", ""),
                    "GH_PROMPT_DISABLED": "true",
                    "GH_CONFIG_DIR": temp_dir,
                    "GIT_TERMINAL_PROMPT": "0",
                }
            )
            if kind == "gh":
                executable = os.environ.get("COCOLA_REAL_GH", "/opt/cocola/gh/current/bin/gh")
                return_code = subprocess.run([executable, *argv], env=env, check=False).returncode
                command_result = "success" if return_code == 0 else "failed"
                return return_code

            askpass = Path(temp_dir) / "askpass.sh"
            askpass.write_text(
                '#!/bin/sh\ncase "$1" in *Username*) printf "%s" "x-access-token" ;; '
                '*) printf "%s" "$GH_TOKEN" ;; esac\n',
                encoding="utf-8",
            )
            askpass.chmod(0o700)
            env["GIT_ASKPASS"] = str(askpass)
            return_code = subprocess.run(
                ["git", *argv], cwd="/workspace", env=env, check=False
            ).returncode
            command_result = "success" if return_code == 0 else "failed"
            return return_code
    finally:
        with suppress(RuntimeError):
            _broker_request(
                "DELETE",
                f"/internal/scm/github/token-leases/{lease_id}",
                {"result": command_result},
            )


def supervisor_status(program: str) -> tuple[str, str]:
    config = os.environ.get("COCOLA_SUPERVISOR_CONFIG", DEFAULT_SUPERVISOR_CONFIG)
    try:
        result = subprocess.run(
            ["/usr/bin/supervisorctl", "-c", config, "status", program],
            check=False,
            capture_output=True,
            text=True,
            timeout=3,
        )
    except (OSError, subprocess.TimeoutExpired) as error:
        return "unavailable", str(error)
    detail = (result.stdout or result.stderr).strip()
    fields = detail.split()
    state = fields[1].upper() if len(fields) > 1 else ""
    mapped = {
        "RUNNING": "ready",
        "STARTING": "starting",
        "BACKOFF": "failed",
        "FATAL": "failed",
        "EXITED": "stopped",
        "STOPPED": "stopped",
        "STOPPING": "stopped",
    }.get(state, "unavailable")
    return mapped, detail


def service_info(manifest: dict[str, Any], profile: str) -> list[dict[str, Any]]:
    services = []
    for name, contract in manifest.get("services", {}).items():
        enabled = service_enabled(manifest, profile, name)
        if enabled:
            state, detail = supervisor_status(contract["supervisor_program"])
        else:
            state, detail = "disabled", "disabled by active profile or operator override"
        services.append(
            {
                "name": name,
                "enabled": enabled,
                "state": state,
                "kind": contract.get("kind"),
                "port": contract.get("port"),
                "required": bool(contract.get("required", False)),
                "detail": detail,
            }
        )
    return services


def browser_status(manifest: dict[str, Any], profile: str) -> dict[str, Any]:
    contract = manifest.get("capabilities", {}).get("browser", {})
    enabled = browser_enabled(manifest, profile)
    runner = str(contract.get("runner", ""))
    chromium = os.environ.get("PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH", "/usr/local/bin/chromium")
    state_dir = str(contract.get("state_dir", ""))
    output_dir = str(contract.get("output_dir", ""))
    checks = {
        "runner": bool(runner) and Path(runner).is_file(),
        "node": shutil.which("node") is not None,
        "chromium": Path(chromium).is_file(),
        "state_dir": bool(state_dir) and Path(state_dir).is_dir() and os.access(state_dir, os.W_OK),
        "output_dir": bool(output_dir)
        and Path(output_dir).is_dir()
        and os.access(output_dir, os.W_OK),
    }
    available = all(checks.values())
    if not enabled:
        state = "disabled"
        detail = "disabled by active profile or operator override"
    elif available:
        state = "ready"
        detail = "on-demand headless browser is available"
    else:
        state = "unavailable"
        missing = ", ".join(name for name, present in checks.items() if not present)
        detail = f"missing runtime components: {missing}"
    return {
        "name": "browser",
        "enabled": enabled,
        "state": state,
        "kind": contract.get("kind", "on-demand"),
        "required": bool(contract.get("required", False)),
        "commands": list(contract.get("commands", [])),
        "state_dir": state_dir,
        "output_dir": output_dir,
        "checks": checks,
        "detail": detail,
    }


def artifact_status(manifest: dict[str, Any], profile: str) -> dict[str, Any]:
    contract = manifest.get("capabilities", {}).get("artifacts", {})
    enabled = artifact_enabled(manifest, profile)
    workspace = Path(manifest["workspace"]["root"]).resolve()
    output_value = str(contract.get("output_dir", "")).strip()
    output_dir = Path(output_value) if output_value else Path("/")
    try:
        output_resolved = output_dir.resolve()
        scoped = output_resolved.is_relative_to(workspace)
    except (OSError, RuntimeError):
        scoped = False
    checks = {
        "output_dir": bool(output_value) and output_dir.is_dir(),
        "writable": bool(output_value) and os.access(output_dir, os.W_OK),
        "absolute": bool(output_value) and output_dir.is_absolute(),
        "workspace_scoped": scoped,
    }
    available = all(checks.values())
    if not enabled:
        state = "disabled"
        detail = "disabled by active Sandbox Profile"
    elif available:
        state = "ready"
        detail = "changed regular files are published after the Agent turn"
    else:
        state = "unavailable"
        missing = ", ".join(name for name, present in checks.items() if not present)
        detail = f"invalid artifact output contract: {missing}"
    return {
        "name": "artifacts",
        "enabled": enabled,
        "state": state,
        "kind": contract.get("kind", "workspace-output"),
        "required": bool(contract.get("required", True)),
        "commands": list(contract.get("commands", [])),
        "output_dir": output_value,
        "html_preview": contract.get("html_preview", "isolated-self-contained"),
        "checks": checks,
        "detail": detail,
    }


def artifact_files(manifest: dict[str, Any], profile: str, limit: int) -> dict[str, Any]:
    status_payload = artifact_status(manifest, profile)
    if not status_payload["enabled"]:
        raise RuntimeError("artifact capability is disabled by the active Sandbox Profile")
    if status_payload["state"] != "ready":
        raise RuntimeError(status_payload["detail"])

    output_dir = Path(status_payload["output_dir"])
    files: list[dict[str, Any]] = []
    truncated = False
    for dirpath, dirnames, filenames in os.walk(output_dir, followlinks=False):
        dirnames[:] = sorted(name for name in dirnames if not Path(dirpath, name).is_symlink())
        for name in sorted(filenames):
            path = Path(dirpath, name)
            try:
                file_stat = path.lstat()
            except OSError:
                continue
            if not stat.S_ISREG(file_stat.st_mode):
                continue
            if len(files) >= limit:
                truncated = True
                break
            relative = path.relative_to(output_dir).as_posix()
            files.append(
                {
                    "path": relative,
                    "size": file_stat.st_size,
                    "mime_type": mimetypes.guess_type(relative)[0] or "application/octet-stream",
                    "mtime_ns": file_stat.st_mtime_ns,
                }
            )
        if truncated:
            break
    return {
        "root": str(output_dir),
        "count": len(files),
        "limit": limit,
        "truncated": truncated,
        "files": files,
    }


def capability_info(manifest: dict[str, Any], profile: str) -> list[dict[str, Any]]:
    capabilities = []
    if "browser" in manifest.get("capabilities", {}):
        capabilities.append(browser_status(manifest, profile))
    if "artifacts" in manifest.get("capabilities", {}):
        capabilities.append(artifact_status(manifest, profile))
    return capabilities


def workspace_info(manifest: dict[str, Any]) -> dict[str, Any]:
    contract = manifest["workspace"]
    paths = {
        name: {
            "path": path,
            "exists": Path(path).exists(),
            "writable": os.access(path, os.W_OK),
        }
        for name, path in contract.items()
    }
    return {"root": contract["root"], "paths": paths}


def runtime_info(manifest: dict[str, Any], profile: str) -> dict[str, Any]:
    return {
        "schema_version": manifest["schema_version"],
        "runtime": manifest["runtime"],
        "profile": profile,
        "workspace": workspace_info(manifest),
        "services": service_info(manifest, profile),
        "editor": manifest.get("editor"),
        "capabilities": capability_info(manifest, profile),
    }


def bounded(value: int, name: str, minimum: int, maximum: int) -> int:
    if value < minimum or value > maximum:
        raise ValueError(f"{name} must be between {minimum} and {maximum}")
    return value


def browser_output_path(manifest: dict[str, Any], action: str, requested: str | None) -> str:
    contract = manifest["capabilities"]["browser"]
    workspace = Path(manifest["workspace"]["root"]).resolve()
    output_dir = Path(contract["output_dir"])
    extension = ".png" if action == "screenshot" else ".pdf"
    if requested:
        candidate = Path(requested)
        if not candidate.is_absolute():
            candidate = output_dir / candidate
        if candidate.suffix == "":
            candidate = candidate.with_suffix(extension)
        elif candidate.suffix.lower() != extension:
            raise ValueError(f"{action} output must use the {extension} extension")
    else:
        stamp = time.strftime("%Y%m%d-%H%M%S", time.gmtime())
        candidate = output_dir / f"{action}-{stamp}-{uuid.uuid4().hex[:8]}{extension}"
    logical = Path(os.path.abspath(candidate))
    resolved = logical.resolve()
    if not resolved.is_relative_to(workspace):
        raise ValueError(f"browser output must stay under {workspace}")
    return str(logical)


def browser_command(contract: dict[str, Any]) -> list[str]:
    node = shutil.which("node")
    if node is None:
        raise RuntimeError("node is unavailable")
    command = [node, contract["runner"]]
    if os.geteuid() != 0:
        return command
    runuser = shutil.which("runuser")
    if runuser is None:
        raise RuntimeError("cannot drop browser runner to the cocola user")
    return [
        runuser,
        "-u",
        "cocola",
        "--",
        "env",
        "HOME=/home/cocola",
        f"NODE_PATH={os.environ.get('NODE_PATH', '')}",
        *command,
    ]


def run_browser(manifest: dict[str, Any], profile: str, request: dict[str, Any]) -> dict[str, Any]:
    status = browser_status(manifest, profile)
    if not status["enabled"]:
        raise RuntimeError("browser capability is disabled by the active Sandbox Profile")
    if status["state"] != "ready":
        raise RuntimeError(status["detail"])
    contract = manifest["capabilities"]["browser"]
    timeout_seconds = request["timeout_ms"] / 1000 + 15
    try:
        result = subprocess.run(
            browser_command(contract),
            input=json.dumps(request),
            check=False,
            capture_output=True,
            text=True,
            timeout=timeout_seconds,
        )
    except subprocess.TimeoutExpired as error:
        raise RuntimeError("browser runner timed out") from error
    lines = [line for line in result.stdout.splitlines() if line.strip()]
    try:
        payload = json.loads(lines[-1]) if lines else {}
    except json.JSONDecodeError as error:
        raise RuntimeError("browser runner returned invalid JSON") from error
    if result.returncode != 0 or not payload.get("ok"):
        detail = payload.get("error") or result.stderr.strip() or "browser command failed"
        raise RuntimeError(str(detail))
    return payload


def browser_request(args: argparse.Namespace, manifest: dict[str, Any]) -> dict[str, Any]:
    parsed = urlsplit(args.url)
    if parsed.scheme not in {"http", "https"} or not parsed.hostname:
        raise ValueError("browser URL must use http:// or https:// and include a host")
    request: dict[str, Any] = {
        "action": args.browser_command,
        "url": args.url,
        "timeout_ms": bounded(args.timeout_ms, "timeout-ms", 1000, 120000),
        "viewport_width": bounded(args.viewport_width, "viewport-width", 320, 3840),
        "viewport_height": bounded(args.viewport_height, "viewport-height", 200, 2160),
    }
    if args.browser_command == "inspect":
        request["max_text_chars"] = bounded(args.max_text_chars, "max-text-chars", 1, 100000)
    else:
        request["output"] = browser_output_path(manifest, args.browser_command, args.output)
        if args.browser_command == "screenshot":
            request["full_page"] = args.full_page
    return request


def emit(value: Any, as_json: bool) -> None:
    if as_json:
        print(json.dumps(value, ensure_ascii=False, sort_keys=True))
        return
    if isinstance(value, dict) and "runtime" in value:
        print(f"Runtime: {value['runtime']} (schema {value['schema_version']})")
        print(f"Profile: {value['profile']}")
        print(f"Workspace: {value['workspace']['root']}")
        for service in value["services"]:
            print(f"Service {service['name']}: {service['state']}")
        for capability in value.get("capabilities", []):
            print(f"Capability {capability['name']}: {capability['state']}")
        return
    if isinstance(value, dict) and "paths" in value:
        print(f"Workspace: {value['root']}")
        for name, item in value["paths"].items():
            print(
                f"  {name}: {item['path']} (exists={item['exists']}, writable={item['writable']})"
            )
        return
    if isinstance(value, list):
        for service in value:
            print(f"{service['name']}: {service['state']} ({service['detail']})")
        return
    if isinstance(value, dict) and value.get("name") == "browser":
        print(f"Browser: {value['state']} ({value['detail']})")
        return
    if isinstance(value, dict) and value.get("name") == "artifacts":
        print(f"Artifacts: {value['state']} ({value['detail']})")
        return
    if isinstance(value, dict) and "files" in value and "root" in value:
        print(f"Artifacts: {value['count']} file(s) under {value['root']}")
        for item in value["files"]:
            print(f"  {item['path']} ({item['size']} bytes, {item['mime_type']})")
        if value.get("truncated"):
            print(f"  ... truncated at {value['limit']} files")
        return
    if isinstance(value, dict) and value.get("action") == "inspect":
        print(f"URL: {value['url']}")
        print(f"Title: {value['title']}")
        print(value["text"])
        return
    if isinstance(value, dict) and value.get("path"):
        print(f"Saved {value['action']} to {value['path']} ({value['bytes']} bytes)")
        return
    print(value)


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="cocola-sandbox")
    parser.add_argument("--version", action="store_true", help="print runtime identity")
    commands = parser.add_subparsers(dest="command")

    info = commands.add_parser("info", help="show the effective runtime contract")
    info.add_argument("--json", action="store_true")

    service = commands.add_parser("service", help="inspect resident services")
    service_commands = service.add_subparsers(dest="service_command")
    status = service_commands.add_parser("status", help="show service status")
    status.add_argument("--json", action="store_true")

    workspace = commands.add_parser("workspace", help="inspect workspace paths")
    workspace_commands = workspace.add_subparsers(dest="workspace_command")
    workspace_info_parser = workspace_commands.add_parser("info", help="show workspace contract")
    workspace_info_parser.add_argument("--json", action="store_true")

    browser = commands.add_parser("browser", help="run on-demand headless browser automation")
    browser_commands = browser.add_subparsers(dest="browser_command")
    browser_status_parser = browser_commands.add_parser(
        "status", help="show browser capability status"
    )
    browser_status_parser.add_argument("--json", action="store_true")

    def add_navigation_options(command: argparse.ArgumentParser) -> None:
        command.add_argument("url")
        command.add_argument("--timeout-ms", type=int, default=30000)
        command.add_argument("--viewport-width", type=int, default=1440)
        command.add_argument("--viewport-height", type=int, default=900)
        command.add_argument("--json", action="store_true")

    inspect = browser_commands.add_parser("inspect", help="extract page title, text, and links")
    add_navigation_options(inspect)
    inspect.add_argument("--max-text-chars", type=int, default=20000)

    screenshot = browser_commands.add_parser("screenshot", help="capture a PNG screenshot")
    add_navigation_options(screenshot)
    screenshot.add_argument("--output")
    screenshot.add_argument(
        "--full-page", action="store_true", help="capture the full scrollable page"
    )

    pdf = browser_commands.add_parser("pdf", help="render the page to PDF")
    add_navigation_options(pdf)
    pdf.add_argument("--output")

    artifact = commands.add_parser("artifact", help="inspect publishable output artifacts")
    artifact_commands = artifact.add_subparsers(dest="artifact_command")
    artifact_status_parser = artifact_commands.add_parser(
        "status", help="show the artifact output contract"
    )
    artifact_status_parser.add_argument("--json", action="store_true")
    artifact_list_parser = artifact_commands.add_parser(
        "list", help="list regular files waiting under the output directory"
    )
    artifact_list_parser.add_argument("--limit", type=int, default=200)
    artifact_list_parser.add_argument("--json", action="store_true")

    github = commands.add_parser("github", help="use run-scoped GitHub credentials")
    github_commands = github.add_subparsers(dest="github_command")
    for command_name in ("gh", "git"):
        command = github_commands.add_parser(command_name)
        command.add_argument("--permissions", action="append", default=[])
        command.add_argument("arguments", nargs=argparse.REMAINDER)

    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    try:
        manifest = load_manifest()
        profile = active_profile(manifest)
        if args.version:
            print(f"{manifest['runtime']} schema-{manifest['schema_version']}")
        elif args.command == "info":
            emit(runtime_info(manifest, profile), args.json)
        elif args.command == "service" and args.service_command == "status":
            emit(service_info(manifest, profile), args.json)
        elif args.command == "workspace" and args.workspace_command == "info":
            emit(workspace_info(manifest), args.json)
        elif args.command == "browser" and args.browser_command == "status":
            emit(browser_status(manifest, profile), args.json)
        elif args.command == "browser" and args.browser_command in {
            "inspect",
            "screenshot",
            "pdf",
        }:
            request = browser_request(args, manifest)
            emit(run_browser(manifest, profile, request), args.json)
        elif args.command == "artifact" and args.artifact_command == "status":
            emit(artifact_status(manifest, profile), args.json)
        elif args.command == "artifact" and args.artifact_command == "list":
            limit = bounded(args.limit, "limit", 1, 1000)
            emit(artifact_files(manifest, profile, limit), args.json)
        elif args.command == "github" and args.github_command in {"gh", "git"}:
            if not github_enabled(manifest, profile):
                raise RuntimeError("GitHub commands are disabled by the active sandbox profile")
            return run_github_command(args.github_command, args.arguments, args.permissions)
        else:
            parser.print_help()
            return 2
    except (OSError, RuntimeError, ValueError, KeyError, json.JSONDecodeError) as error:
        print(f"cocola-sandbox: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
