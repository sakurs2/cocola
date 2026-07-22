#!/usr/bin/env python3
"""Guest-facing CLI for the Cocola sandbox runtime contract."""

from __future__ import annotations

import argparse
import json
import mimetypes
import os
import shutil
import signal
import socket
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
PREVIEW_SECRET_ENV_MARKERS = ("TOKEN", "API_KEY", "SECRET", "PASSWORD", "CREDENTIAL")
PREVIEW_LOG_MAX_BYTES = 1024 * 1024


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


def preview_enabled(manifest: dict[str, Any], profile: str) -> bool:
    profile_capability = manifest["profiles"][profile].get("capabilities", {}).get("preview", {})
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


def run_github_command(
    kind: str,
    arguments: list[str],
    permissions: list[str],
    working_directory: str = "/workspace",
) -> int:
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
                ["git", *argv], cwd=working_directory, env=env, check=False
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


def preview_manager_status(manifest: dict[str, Any], profile: str) -> dict[str, Any]:
    contract = manifest.get("capabilities", {}).get("preview", {})
    enabled = preview_enabled(manifest, profile)
    state_dir = Path(os.environ.get("COCOLA_PREVIEW_STATE_DIR", str(contract.get("state_dir", ""))))
    parent = state_dir if state_dir.is_dir() else state_dir.parent
    available = bool(str(state_dir)) and parent.is_dir() and os.access(parent, os.W_OK)
    if not enabled:
        state = "disabled"
        detail = "disabled by active Sandbox Profile"
    elif available:
        state = "ready"
        detail = "managed preview processes can outlive one Agent turn"
    else:
        state = "unavailable"
        detail = "preview process state directory is unavailable"
    return {
        "name": "preview",
        "enabled": enabled,
        "state": state,
        "kind": contract.get("kind", "managed-user-process"),
        "required": bool(contract.get("required", False)),
        "commands": list(contract.get("commands", [])),
        "state_dir": str(state_dir),
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
    if "preview" in manifest.get("capabilities", {}):
        capabilities.append(preview_manager_status(manifest, profile))
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


def preview_state_root(manifest: dict[str, Any]) -> Path:
    contract = manifest["capabilities"]["preview"]
    configured = os.environ.get("COCOLA_PREVIEW_STATE_DIR", "").strip()
    return Path(configured or contract["state_dir"])


def preview_state_path(manifest: dict[str, Any], port: int) -> Path:
    return preview_state_root(manifest) / f"{port}.json"


def preview_log_path(manifest: dict[str, Any], port: int) -> Path:
    return preview_state_root(manifest) / f"{port}.log"


def preview_rotated_log_path(path: Path) -> Path:
    return path.with_suffix(path.suffix + ".1")


def preview_process_identity(pid: int) -> tuple[str, str] | None:
    """Return Linux process state and start ticks, protecting against PID reuse."""

    try:
        raw = Path(f"/proc/{pid}/stat").read_text(encoding="utf-8")
    except OSError:
        return None
    close = raw.rfind(")")
    fields = raw[close + 2 :].split() if close >= 0 else []
    if len(fields) < 20:
        return None
    return fields[0], fields[19]


def preview_network_host() -> str:
    try:
        addresses = socket.getaddrinfo(socket.gethostname(), None, socket.AF_INET)
    except OSError:
        addresses = []
    for item in addresses:
        host = str(item[4][0])
        if host and not host.startswith("127.") and host != "0.0.0.0":
            return host
    return "127.0.0.1"


def preview_port_reachable(port: int, *, timeout: float = 0.25) -> bool:
    try:
        with socket.create_connection((preview_network_host(), port), timeout=timeout):
            return True
    except OSError:
        return False


def read_preview_state(manifest: dict[str, Any], port: int) -> dict[str, Any] | None:
    path = preview_state_path(manifest, port)
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError:
        return None
    except (OSError, ValueError) as error:
        raise RuntimeError(f"preview state for port {port} is invalid") from error
    if not isinstance(value, dict) or int(value.get("port", 0)) != port:
        raise RuntimeError(f"preview state for port {port} is invalid")
    return value


def write_preview_state(manifest: dict[str, Any], state: dict[str, Any]) -> None:
    root = preview_state_root(manifest)
    root.mkdir(mode=0o700, parents=True, exist_ok=True)
    path = preview_state_path(manifest, int(state["port"]))
    temporary = root / f".{path.name}.{uuid.uuid4().hex}.tmp"
    try:
        temporary.write_text(
            json.dumps(state, sort_keys=True, separators=(",", ":")), encoding="utf-8"
        )
        temporary.chmod(0o600)
        os.replace(temporary, path)
    finally:
        with suppress(FileNotFoundError):
            temporary.unlink()


def preview_state_is_alive(state: dict[str, Any]) -> bool:
    pid = int(state.get("pid", 0))
    identity = preview_process_identity(pid)
    return bool(
        pid > 1
        and identity is not None
        and identity[0] != "Z"
        and identity[1] == str(state.get("start_ticks", ""))
    )


def preview_status(manifest: dict[str, Any], port: int) -> dict[str, Any]:
    state = read_preview_state(manifest, port)
    if state is None:
        reachable = preview_port_reachable(port)
        return {
            "port": port,
            "state": "unmanaged" if reachable else "stopped",
            "reachable": reachable,
        }
    if not preview_state_is_alive(state):
        with suppress(FileNotFoundError):
            preview_state_path(manifest, port).unlink()
        return {
            "port": port,
            "state": "stopped",
            "reachable": False,
            "log_path": str(preview_log_path(manifest, port)),
        }
    reachable = preview_port_reachable(port)
    return {
        "port": port,
        "state": "ready" if reachable else "starting",
        "reachable": reachable,
        "pid": int(state["pid"]),
        "cwd": str(state["cwd"]),
        "log_path": str(state["log_path"]),
        "started_at": int(state["started_at"]),
    }


def preview_environment(port: int) -> dict[str, str]:
    environment = os.environ.copy()
    for key in list(environment):
        upper = key.upper()
        if upper.startswith(("ANTHROPIC_", "OPENAI_", "COCOLA_PROJECT_", "COCOLA_SCM_")) or any(
            marker in upper for marker in PREVIEW_SECRET_ENV_MARKERS
        ):
            environment.pop(key, None)
    environment.update({"HOST": "0.0.0.0", "HOSTNAME": "0.0.0.0", "PORT": str(port)})
    return environment


def preview_workspace_cwd(manifest: dict[str, Any], requested: str) -> Path:
    workspace = Path(manifest["workspace"]["root"]).resolve()
    candidate = Path(os.path.abspath(requested)).resolve()
    if not candidate.is_relative_to(workspace):
        raise ValueError(f"preview cwd must stay under {workspace}")
    if not candidate.is_dir():
        raise ValueError(f"preview cwd does not exist: {candidate}")
    return candidate


def run_preview_process(log_path: str, command: list[str]) -> int:
    """Run one command while keeping at most two bounded log segments."""

    if not command:
        return 2
    path = Path(log_path)
    rotated = preview_rotated_log_path(path)
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    with suppress(FileNotFoundError):
        rotated.unlink()
    process = subprocess.Popen(
        command,
        stdin=subprocess.DEVNULL,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        close_fds=True,
    )
    assert process.stdout is not None
    handle = path.open("wb")
    size = 0
    try:
        while chunk := process.stdout.read(64 * 1024):
            if len(chunk) > PREVIEW_LOG_MAX_BYTES:
                chunk = chunk[-PREVIEW_LOG_MAX_BYTES:]
            if size + len(chunk) > PREVIEW_LOG_MAX_BYTES:
                handle.close()
                os.replace(path, rotated)
                handle = path.open("wb")
                size = 0
            handle.write(chunk)
            handle.flush()
            size += len(chunk)
    finally:
        handle.close()
        process.stdout.close()
    return process.wait()


def stop_preview_process(manifest: dict[str, Any], state: dict[str, Any]) -> None:
    port = int(state["port"])
    pid = int(state.get("pid", 0))
    if preview_state_is_alive(state):
        with suppress(ProcessLookupError):
            os.killpg(pid, signal.SIGTERM)
        deadline = time.monotonic() + 3
        while time.monotonic() < deadline and preview_state_is_alive(state):
            time.sleep(0.1)
        if preview_state_is_alive(state):
            with suppress(ProcessLookupError):
                os.killpg(pid, signal.SIGKILL)
    with suppress(FileNotFoundError):
        preview_state_path(manifest, port).unlink()


def preview_stop(manifest: dict[str, Any], port: int) -> dict[str, Any]:
    state = read_preview_state(manifest, port)
    if state is None and preview_port_reachable(port):
        raise RuntimeError(f"preview port {port} is not managed; refusing to stop another process")
    if state is not None:
        stop_preview_process(manifest, state)
    return {
        "port": port,
        "state": "stopped",
        "reachable": False,
        "log_path": str(preview_log_path(manifest, port)),
    }


def preview_start(
    manifest: dict[str, Any],
    port: int,
    cwd: str,
    command: list[str],
    timeout_ms: int,
) -> dict[str, Any]:
    current = preview_status(manifest, port)
    if current["state"] in {"starting", "ready"}:
        raise RuntimeError(f"preview port {port} is already managed; stop it before restarting")
    if preview_port_reachable(port):
        raise RuntimeError(f"preview port {port} is already in use by an unmanaged process")

    argv = list(command)
    if argv and argv[0] == "--":
        argv = argv[1:]
    if not argv:
        raise ValueError("a preview command is required after --")

    working_directory = preview_workspace_cwd(manifest, cwd)
    root = preview_state_root(manifest)
    root.mkdir(mode=0o700, parents=True, exist_ok=True)
    log_path = preview_log_path(manifest, port)
    process = subprocess.Popen(
        [sys.executable, str(Path(__file__).resolve()), "__preview_runner__", str(log_path), *argv],
        cwd=working_directory,
        env=preview_environment(port),
        stdin=subprocess.DEVNULL,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        start_new_session=True,
        close_fds=True,
    )

    identity = preview_process_identity(process.pid)
    if identity is None:
        with suppress(ProcessLookupError):
            os.killpg(process.pid, signal.SIGTERM)
        raise RuntimeError("preview process exited before its identity could be recorded")
    state = {
        "schema_version": 1,
        "port": port,
        "pid": process.pid,
        "start_ticks": identity[1],
        "cwd": str(working_directory),
        "log_path": str(log_path),
        "started_at": int(time.time()),
    }
    write_preview_state(manifest, state)

    deadline = time.monotonic() + timeout_ms / 1000
    while time.monotonic() < deadline:
        if process.poll() is not None or not preview_state_is_alive(state):
            with suppress(FileNotFoundError):
                preview_state_path(manifest, port).unlink()
            raise RuntimeError(
                f"preview command exited before port {port} became ready; inspect {log_path}"
            )
        if preview_port_reachable(port):
            return preview_status(manifest, port)
        time.sleep(0.2)

    stop_preview_process(manifest, state)
    raise RuntimeError(
        f"preview port {port} did not become reachable from the container network; "
        f"bind the server to 0.0.0.0 and inspect {log_path}"
    )


def preview_logs(manifest: dict[str, Any], port: int, lines: int) -> dict[str, Any]:
    path = preview_log_path(manifest, port)
    segments = [
        candidate for candidate in (preview_rotated_log_path(path), path) if candidate.is_file()
    ]
    if not segments:
        raise RuntimeError(f"preview log for port {port} does not exist")
    content = "".join(
        candidate.read_text(encoding="utf-8", errors="replace") for candidate in segments
    )
    return {
        "action": "preview-logs",
        "port": port,
        "log_path": str(path),
        "lines": content.splitlines()[-lines:],
    }


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
    if isinstance(value, dict) and value.get("action") == "preview-logs":
        print("\n".join(value["lines"]))
        return
    if isinstance(value, dict) and "port" in value and "state" in value:
        print(f"Preview {value['port']}: {value['state']}")
        if value.get("log_path"):
            print(f"Log: {value['log_path']}")
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

    preview = commands.add_parser("preview", help="manage a user-facing local preview server")
    preview_commands = preview.add_subparsers(dest="preview_command")
    preview_start_parser = preview_commands.add_parser(
        "start", help="start a detached preview process"
    )
    preview_start_parser.add_argument("--port", required=True, type=int)
    preview_start_parser.add_argument("--cwd")
    preview_start_parser.add_argument("--timeout-ms", type=int, default=20000)
    preview_start_parser.add_argument("--json", action="store_true")
    preview_start_parser.add_argument("arguments", nargs=argparse.REMAINDER)
    preview_status_parser = preview_commands.add_parser(
        "status", help="show a managed preview process"
    )
    preview_status_parser.add_argument("--port", required=True, type=int)
    preview_status_parser.add_argument("--json", action="store_true")
    preview_stop_parser = preview_commands.add_parser("stop", help="stop a managed preview process")
    preview_stop_parser.add_argument("--port", required=True, type=int)
    preview_stop_parser.add_argument("--json", action="store_true")
    preview_logs_parser = preview_commands.add_parser("logs", help="show bounded preview logs")
    preview_logs_parser.add_argument("--port", required=True, type=int)
    preview_logs_parser.add_argument("--lines", type=int, default=100)
    preview_logs_parser.add_argument("--json", action="store_true")

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
        elif args.command == "preview" and args.preview_command in {
            "start",
            "status",
            "stop",
            "logs",
        }:
            if not preview_enabled(manifest, profile):
                raise RuntimeError("preview commands are disabled by the active sandbox profile")
            port = bounded(args.port, "port", 1024, 65535)
            if args.preview_command == "start":
                timeout_ms = bounded(args.timeout_ms, "timeout-ms", 1000, 60000)
                requested_cwd = (
                    args.cwd
                    or os.environ.get("COCOLA_AGENT_CWD", "").strip()
                    or manifest["workspace"]["root"]
                )
                emit(
                    preview_start(manifest, port, requested_cwd, args.arguments, timeout_ms),
                    args.json,
                )
            elif args.preview_command == "status":
                emit(preview_status(manifest, port), args.json)
            elif args.preview_command == "stop":
                emit(preview_stop(manifest, port), args.json)
            else:
                lines = bounded(args.lines, "lines", 1, 1000)
                emit(preview_logs(manifest, port, lines), args.json)
        elif args.command == "github" and args.github_command in {"gh", "git"}:
            if not github_enabled(manifest, profile):
                raise RuntimeError("GitHub commands are disabled by the active sandbox profile")
            working_directory = preview_workspace_cwd(
                manifest,
                os.environ.get("COCOLA_AGENT_CWD", "").strip()
                or manifest["workspace"]["root"],
            )
            return run_github_command(
                args.github_command,
                args.arguments,
                args.permissions,
                str(working_directory),
            )
        else:
            parser.print_help()
            return 2
    except (OSError, RuntimeError, ValueError, KeyError, json.JSONDecodeError) as error:
        print(f"cocola-sandbox: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    if len(sys.argv) >= 3 and sys.argv[1] == "__preview_runner__":
        raise SystemExit(run_preview_process(sys.argv[2], sys.argv[3:]))
    raise SystemExit(main())
