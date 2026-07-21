"""Project workspace bootstrap and read-only Git inspection.

All Git operations execute inside the bound sandbox. The only credential is a
short-lived, single-repository installation token supplied for the bootstrap
process through its environment; it is never written into the repository,
remote URL, marker or runtime state.
"""

from __future__ import annotations

import json
import posixpath
from contextlib import suppress
from dataclasses import dataclass
from typing import Any

from cocola_agent_runtime.sandbox_binder import SandboxExecutor

WORKSPACE = "/workspace"
MAX_DIFF_BYTES = 512 * 1024


class ProjectWorkspaceError(RuntimeError):
    """A stable project workspace validation/bootstrap failure."""

    def __init__(self, code: str, message: str) -> None:
        super().__init__(message)
        self.code = code


@dataclass(frozen=True)
class ProjectSpec:
    project_id: str
    repository_id: int
    clone_url: str
    default_branch: str
    base_sha: str
    task_branch: str
    git_author_name: str
    git_author_email: str


def spec_from_proto(value: Any) -> ProjectSpec:
    return ProjectSpec(
        project_id=str(value.project_id or ""),
        repository_id=int(value.repository_id or 0),
        clone_url=str(value.clone_url or ""),
        default_branch=str(value.default_branch or ""),
        base_sha=str(value.base_sha or ""),
        task_branch=str(value.task_branch or ""),
        git_author_name=str(value.git_author_name or ""),
        git_author_email=str(value.git_author_email or ""),
    )


def validate_spec(spec: ProjectSpec) -> None:
    if (
        not spec.project_id
        or spec.repository_id <= 0
        or not spec.clone_url.startswith("https://github.com/")
        or "@" in spec.clone_url.removeprefix("https://")
        or not spec.clone_url.endswith(".git")
        or not spec.default_branch
        or len(spec.base_sha) != 40
        or any(char not in "0123456789abcdefABCDEF" for char in spec.base_sha)
        or not spec.task_branch.startswith("cocola/task-")
        or not spec.git_author_name.strip()
        or len(spec.git_author_name) > 128
        or any(char in spec.git_author_name for char in "\x00\r\n")
        or not spec.git_author_email.strip()
        or len(spec.git_author_email) > 254
        or "@" not in spec.git_author_email
        or any(char in spec.git_author_email for char in "\x00\r\n")
    ):
        raise ProjectWorkspaceError("PROJECT_CONTEXT_INVALID", "Project context is invalid")


def validate_relative_path(path: str) -> str:
    if not path or "\x00" in path or path.startswith("/") or "\\" in path:
        raise ProjectWorkspaceError("GIT_PATH_INVALID", "Git path is invalid")
    normalized = posixpath.normpath(path)
    if normalized in {"", ".", ".."} or normalized.startswith("../"):
        raise ProjectWorkspaceError("GIT_PATH_INVALID", "Git path is invalid")
    return normalized


async def bootstrap_project(
    executor: SandboxExecutor,
    sandbox_id: str,
    spec: ProjectSpec,
    token: str,
    *,
    timeout_secs: int = 600,
) -> dict[str, Any]:
    validate_spec(spec)
    if not token:
        raise ProjectWorkspaceError(
            "PROJECT_CREDENTIAL_UNAVAILABLE", "Project credential is unavailable"
        )
    result = await executor.exec(
        sandbox_id=sandbox_id,
        cmd=["python3", "-c", _PROJECT_GIT_SCRIPT],
        cwd="/",
        env={"COCOLA_SCM_TOKEN": token},
        stdin=json.dumps({"operation": "bootstrap", "spec": spec.__dict__}),
        timeout_secs=timeout_secs,
    )
    return _decode_result(result, "PROJECT_BOOTSTRAP_FAILED")


async def inspect_project(
    executor: SandboxExecutor,
    sandbox_id: str,
    spec: ProjectSpec,
    operation: str,
    *,
    path: str = "",
    diff_target: str = "",
) -> dict[str, Any]:
    validate_spec(spec)
    if operation not in {"status", "diff"}:
        raise ProjectWorkspaceError("GIT_OPERATION_INVALID", "Git operation is invalid")
    if operation == "diff":
        path = validate_relative_path(path)
        if diff_target not in {"working", "staged"}:
            raise ProjectWorkspaceError("GIT_OPERATION_INVALID", "Git diff target is invalid")
    result = await executor.exec(
        sandbox_id=sandbox_id,
        cmd=["python3", "-c", _PROJECT_GIT_SCRIPT],
        cwd="/",
        stdin=json.dumps(
            {
                "operation": operation,
                "spec": spec.__dict__,
                "path": path,
                "diff_target": diff_target,
            }
        ),
        timeout_secs=60,
    )
    return _decode_result(result, "GIT_INSPECT_FAILED")


def _decode_result(result: Any, fallback_code: str) -> dict[str, Any]:
    payload: dict[str, Any] = {}
    with suppress(TypeError, ValueError):
        payload = json.loads(result.stdout.strip() or "{}")
    if not result.ok or not payload.get("ok"):
        code = str(payload.get("code") or fallback_code)
        message = str(
            payload.get("error") or result.error or result.stderr or "Git operation failed"
        )
        raise ProjectWorkspaceError(code, message[:500])
    return payload


_PORCELAIN_V2_PARSER_SCRIPT = r"""
def parse_porcelain_v2(raw, default_branch, max_paths):
    records = raw.split(b"\0")
    changes = []
    index = 0
    branch = default_branch
    while index < len(records):
        record = records[index]
        index += 1
        if not record:
            continue
        text = record.decode("utf-8", "surrogateescape")
        if text.startswith("# branch.head "):
            branch = text[len("# branch.head "):]
            continue
        if text.startswith("#") or text.startswith("!"):
            continue
        old_path = ""
        if text.startswith("1 "):
            fields = text.split(" ", 8)
            if len(fields) < 9:
                continue
            xy, path = fields[1], fields[8]
        elif text.startswith("2 "):
            fields = text.split(" ", 9)
            if len(fields) < 10:
                continue
            xy, path = fields[1], fields[9]
            if index < len(records):
                old_path = records[index].decode("utf-8", "surrogateescape")
                index += 1
        elif text.startswith("u "):
            fields = text.split(" ", 10)
            if len(fields) < 11:
                continue
            xy, path = fields[1], fields[10]
        elif text.startswith("? "):
            xy, path = "??", text[2:]
        else:
            continue
        staged = xy[0] not in {".", "?"}
        working = xy[1] not in {".", "?"}
        area = (
            "both" if staged and working
            else "staged" if staged
            else "working" if working
            else "untracked"
        )
        changes.append({"path": path, "old_path": old_path, "status": xy, "area": area})
    truncated = len(changes) > max_paths
    return branch, changes[:max_paths], truncated
"""


_PROJECT_GIT_SCRIPT = (
    r"""
import json
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile
import uuid

WORKSPACE = pathlib.Path("/workspace")
MAX_PATHS = 500
MAX_DIFF = 512 * 1024

"""
    + _PORCELAIN_V2_PARSER_SCRIPT
    + r"""

request = json.loads(sys.stdin.read())
spec = request["spec"]

def fail(code, message):
    print(json.dumps({"ok": False, "code": code, "error": message}))
    raise SystemExit(0)

def git(args, cwd=WORKSPACE, env=None, timeout=60, binary=False):
    command = [
        "git", "-c", "credential.helper=", "-c", "core.hooksPath=/dev/null",
        "-c", "core.fsmonitor=false", "-c", "safe.directory=/workspace", *args,
    ]
    process_env = {
        **os.environ,
        "GIT_CONFIG_NOSYSTEM": "1",
        "GIT_CONFIG_GLOBAL": "/dev/null",
        "GIT_CONFIG_SYSTEM": "/dev/null",
        "GIT_ATTR_NOSYSTEM": "1",
        "GIT_PAGER": "cat",
        "PAGER": "cat",
        "GIT_OPTIONAL_LOCKS": "0",
        "GIT_TERMINAL_PROMPT": "0",
        "LC_ALL": "C",
        # Phase one deliberately keeps Git LFS pointer files. Downloading LFS
        # objects would bypass the repository size guard during bootstrap.
        "GIT_LFS_SKIP_SMUDGE": "1",
    }
    if env:
        process_env.update(env)
    try:
        return subprocess.run(
            command, cwd=cwd, env=process_env, stdout=subprocess.PIPE,
            stderr=subprocess.PIPE, check=False, timeout=timeout,
            text=not binary,
        )
    except subprocess.TimeoutExpired:
        fail("GIT_TIMEOUT", "Git operation timed out")

def marker_path():
    return WORKSPACE / ".git" / "cocola-project.json"

def validate_workspace():
    path = marker_path()
    if not path.is_file():
        fail("PROJECT_WORKSPACE_MISMATCH", "Project workspace marker is missing")
    try:
        marker = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        fail("PROJECT_WORKSPACE_MISMATCH", "Project workspace marker is invalid")
    if (
        marker.get("project_id") != spec["project_id"]
        or int(marker.get("repository_id", 0)) != int(spec["repository_id"])
        or marker.get("task_branch") != spec["task_branch"]
        or marker.get("base_sha", "").lower() != spec["base_sha"].lower()
    ):
        fail("PROJECT_WORKSPACE_MISMATCH", "Workspace belongs to a different project")
    branch = git(["rev-parse", "--abbrev-ref", "HEAD"])
    if branch.returncode != 0 or branch.stdout.strip() != spec["task_branch"]:
        fail("PROJECT_WORKSPACE_MISMATCH", "Workspace is on an unexpected branch")

def configure_author(cwd):
    for key, value in (
        ("user.name", spec["git_author_name"]),
        ("user.email", spec["git_author_email"]),
    ):
        configured = git(["config", "--local", key, value], cwd=cwd)
        if configured.returncode != 0:
            fail("PROJECT_GIT_IDENTITY_FAILED", "Could not configure Git author identity")

def bootstrap():
    if (WORKSPACE / ".git").exists():
        validate_workspace()
        configure_author(WORKSPACE)
        return
    WORKSPACE.mkdir(parents=True, exist_ok=True)
    existing = [entry for entry in WORKSPACE.iterdir() if entry.name not in {"lost+found"}]
    if existing:
        fail("PROJECT_WORKSPACE_MISMATCH", "Workspace is not empty and is not a Cocola project")
    stage = WORKSPACE / (".cocola-clone-" + uuid.uuid4().hex)
    askpass_dir = pathlib.Path(tempfile.mkdtemp(prefix="cocola-askpass-"))
    askpass = askpass_dir / "askpass.sh"
    askpass.write_text(
        '#!/bin/sh\ncase "$1" in *Username*) printf "%s" "x-access-token" ;; '
        '*) printf "%s" "$COCOLA_SCM_TOKEN" ;; esac\n',
        encoding="utf-8",
    )
    askpass.chmod(0o700)
    auth_env = {"GIT_ASKPASS": str(askpass)}
    try:
        clone = git(
            ["clone", "--no-checkout", "--origin", "origin", spec["clone_url"], str(stage)],
            cwd="/", env=auth_env, timeout=600,
        )
        if clone.returncode != 0:
            fail("PROJECT_CLONE_FAILED", "Could not clone the project repository")
        checkout = git(["checkout", "--detach", spec["base_sha"]], cwd=stage)
        if checkout.returncode != 0:
            fail("PROJECT_BASE_SHA_UNAVAILABLE", "The locked project revision is unavailable")
        branch = git(["switch", "-c", spec["task_branch"]], cwd=stage)
        if branch.returncode != 0:
            fail("PROJECT_BRANCH_FAILED", "Could not create the task branch")
        head = git(["rev-parse", "HEAD"], cwd=stage)
        if head.returncode != 0 or head.stdout.strip().lower() != spec["base_sha"].lower():
            fail("PROJECT_BASE_SHA_MISMATCH", "Cloned repository revision does not match")
        configure_author(stage)
        marker = {
            "schema_version": 1,
            "project_id": spec["project_id"],
            "repository_id": int(spec["repository_id"]),
            "task_branch": spec["task_branch"],
            "base_sha": spec["base_sha"],
        }
        (stage / ".git" / "cocola-project.json").write_text(
            json.dumps(marker, sort_keys=True), encoding="utf-8"
        )
        # Publish repository metadata last. A process interruption can leave a
        # visibly incomplete directory, but can never leave a valid marker on
        # a partially moved checkout that a later resume would accept.
        for entry in [entry for entry in stage.iterdir() if entry.name != ".git"]:
            os.replace(entry, WORKSPACE / entry.name)
        os.replace(stage / ".git", WORKSPACE / ".git")
        stage.rmdir()
    finally:
        os.environ.pop("COCOLA_SCM_TOKEN", None)
        shutil.rmtree(askpass_dir, ignore_errors=True)
        if stage.exists():
            shutil.rmtree(stage, ignore_errors=True)

def status_snapshot():
    validate_workspace()
    status = git(
        ["status", "--porcelain=v2", "-z", "--branch", "--untracked-files=all"],
        binary=True,
    )
    if status.returncode != 0:
        fail("GIT_STATUS_FAILED", "Could not inspect Git status")
    branch, changes, truncated = parse_porcelain_v2(
        status.stdout, spec["task_branch"], MAX_PATHS
    )
    head = git(["rev-parse", "HEAD"])
    if head.returncode != 0:
        fail("GIT_STATUS_FAILED", "Could not read Git HEAD")
    ahead = git(["rev-list", "--count", spec["base_sha"] + "..HEAD"])
    try:
        ahead_count = int(ahead.stdout.strip()) if ahead.returncode == 0 else 0
    except ValueError:
        ahead_count = 0
    return {
        "branch": branch,
        "base_ref": spec["default_branch"],
        "base_sha": spec["base_sha"],
        "head_sha": head.stdout.strip(),
        "ahead": ahead_count,
        "dirty": bool(changes),
        "changes": changes,
        "truncated": truncated,
    }

operation = request["operation"]
if operation == "bootstrap":
    bootstrap()
    print(json.dumps({"ok": True, "snapshot": status_snapshot()}))
elif operation == "status":
    print(json.dumps({"ok": True, "snapshot": status_snapshot()}))
elif operation == "diff":
    validate_workspace()
    args = ["diff", "--no-ext-diff", "--no-textconv"]
    expected_codes = {0}
    if request.get("diff_target") == "staged":
        args.append("--cached")
        args.extend(["--", request["path"]])
    else:
        tracked = git(["ls-files", "--error-unmatch", "--", request["path"]])
        if tracked.returncode == 0:
            args.extend(["--", request["path"]])
        else:
            args.extend(["--no-index", "--", "/dev/null", request["path"]])
            expected_codes.add(1)
    result = git(args, binary=True)
    if result.returncode not in expected_codes:
        fail("GIT_DIFF_FAILED", "Could not read Git diff")
    binary = b"Binary files " in result.stdout or b"GIT binary patch" in result.stdout
    truncated = len(result.stdout) > MAX_DIFF
    text = result.stdout[:MAX_DIFF].decode("utf-8", "replace")
    print(json.dumps({
        "ok": True, "snapshot": status_snapshot(), "diff": text,
        "binary": binary, "truncated": truncated,
    }))
else:
    fail("GIT_OPERATION_INVALID", "Git operation is invalid")
"""
)
