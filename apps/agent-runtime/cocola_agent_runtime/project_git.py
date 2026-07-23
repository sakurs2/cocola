"""Project workspace bootstrap and read-only Git inspection.

All Git operations execute inside the bound sandbox. Local Projects initialize
Git directly in their persistent workspace. GitHub Projects receive a
short-lived, single-repository installation token for bootstrap; it is never
written into the repository, remote URL, marker or runtime state.
"""

from __future__ import annotations

import json
import posixpath
import urllib.parse
from contextlib import suppress
from dataclasses import dataclass
from typing import Any

from cocola_agent_runtime.sandbox_binder import SandboxExecutor

PLATFORM_WORKSPACE = "/workspace"
PROJECT_WORKTREE = "/workspace/project"
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
    base_ref: str
    base_sha: str
    task_branch: str
    git_author_name: str
    git_author_email: str
    repository_provider: str = "github"
    repository_full_name: str = ""
    credential_mode: str = "ephemeral"


def spec_from_proto(value: Any) -> ProjectSpec:
    return ProjectSpec(
        project_id=str(value.project_id or ""),
        repository_id=int(value.repository_id or 0),
        clone_url=str(value.clone_url or ""),
        default_branch=str(value.default_branch or ""),
        base_ref=str(getattr(value, "base_ref", "") or value.default_branch or ""),
        base_sha=str(value.base_sha or ""),
        task_branch=str(value.task_branch or ""),
        git_author_name=str(value.git_author_name or ""),
        git_author_email=str(value.git_author_email or ""),
        repository_provider=str(getattr(value, "repository_provider", "") or "github"),
        repository_full_name=str(getattr(value, "repository_full_name", "") or ""),
        credential_mode=str(getattr(value, "credential_mode", "") or "ephemeral"),
    )


def validate_spec(spec: ProjectSpec) -> None:
    clone = urllib.parse.urlsplit(spec.clone_url)
    remote_clone_valid = (
        clone.scheme in {"http", "https"}
        and bool(clone.hostname)
        and clone.username is None
        and clone.password is None
        and spec.clone_url.endswith(".git")
    )
    if spec.repository_provider == "github":
        remote_clone_valid = (
            remote_clone_valid and clone.scheme == "https" and clone.hostname == "github.com"
        )
    local = spec.repository_provider == "local"
    local_remote_valid = (
        spec.repository_id == 0 and spec.credential_mode == "none" and not spec.repository_full_name
    ) or (
        spec.repository_id > 0
        and spec.credential_mode == "broker"
        and bool(spec.repository_full_name)
        and "/" in spec.repository_full_name
    )
    base_sha_valid = (local and not spec.base_sha) or (
        len(spec.base_sha) == 40 and all(char in "0123456789abcdefABCDEF" for char in spec.base_sha)
    )
    if (
        not spec.project_id
        or spec.repository_provider not in {"local", "github"}
        or (not local and spec.repository_id <= 0)
        or (not local and not remote_clone_valid)
        or (local and (spec.clone_url or not local_remote_valid))
        or (not local and spec.credential_mode != "ephemeral")
        or not spec.default_branch
        or not spec.base_ref
        or not base_sha_valid
        or (local and (spec.default_branch != "main" or spec.task_branch != "main"))
        or (not local and not spec.task_branch.startswith("cocola/task-"))
        or not spec.git_author_name.strip()
        or len(spec.git_author_name) > 128
        or any(char in spec.git_author_name for char in "\x00\r\n")
        or not spec.git_author_email.strip()
        or len(spec.git_author_email) > 254
        or "@" not in spec.git_author_email
        or any(char in spec.git_author_email for char in "\x00\r\n")
    ):
        raise ProjectWorkspaceError("PROJECT_CONTEXT_INVALID", "Project context is invalid")


def project_egress_host(spec: ProjectSpec) -> str:
    validate_spec(spec)
    if spec.repository_provider == "local":
        return ""
    return str(urllib.parse.urlsplit(spec.clone_url).hostname or "")


def project_egress_hosts(spec: ProjectSpec, broker_url: str | None = None) -> list[str]:
    host = project_egress_host(spec)
    brokered_local = spec.repository_provider == "local" and spec.credential_mode == "broker"
    if not host and not (brokered_local and broker_url):
        return []
    hosts = [host] if host else ["github.com"]
    if (spec.repository_provider != "github" and not brokered_local) or not broker_url:
        return hosts
    for github_host in (
        "api.github.com",
        "uploads.github.com",
        "objects.githubusercontent.com",
        "github-releases.githubusercontent.com",
    ):
        if github_host not in hosts:
            hosts.append(github_host)
    broker = urllib.parse.urlsplit(broker_url)
    if broker.scheme not in {"http", "https"} or not broker.hostname:
        raise ProjectWorkspaceError(
            "PROJECT_BROKER_CONFIG_INVALID", "Project broker URL is invalid"
        )
    if broker.hostname not in hosts:
        hosts.append(broker.hostname)
    return hosts


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
    if spec.repository_provider != "local" and not token:
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
    commit_sha: str = "",
) -> dict[str, Any]:
    validate_spec(spec)
    if operation not in {"status", "diff", "commit"}:
        raise ProjectWorkspaceError("GIT_OPERATION_INVALID", "Git operation is invalid")
    if operation == "diff":
        path = validate_relative_path(path)
        if diff_target not in {"working", "staged"}:
            raise ProjectWorkspaceError("GIT_OPERATION_INVALID", "Git diff target is invalid")
    if operation == "commit":
        if len(commit_sha) != 40 or any(
            character not in "0123456789abcdefABCDEF" for character in commit_sha
        ):
            raise ProjectWorkspaceError("GIT_COMMIT_INVALID", "Git commit is invalid")
        if path:
            path = validate_relative_path(path)
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
                "commit_sha": commit_sha,
            }
        ),
        timeout_secs=60,
    )
    return _decode_result(result, "GIT_INSPECT_FAILED")


async def publish_project(
    executor: SandboxExecutor,
    sandbox_id: str,
    spec: ProjectSpec,
    token: str,
    remote_clone_url: str,
    expected_head_sha: str,
) -> dict[str, Any]:
    validate_spec(spec)
    remote = urllib.parse.urlsplit(remote_clone_url)
    if (
        spec.repository_provider != "local"
        or not token
        or remote.scheme != "https"
        or remote.hostname != "github.com"
        or remote.username is not None
        or remote.password is not None
        or not remote_clone_url.endswith(".git")
        or len(expected_head_sha) != 40
        or any(char not in "0123456789abcdefABCDEF" for char in expected_head_sha)
    ):
        raise ProjectWorkspaceError("PROJECT_PUBLISH_INVALID", "Publish request is invalid")
    result = await executor.exec(
        sandbox_id=sandbox_id,
        cmd=["python3", "-c", _PROJECT_GIT_SCRIPT],
        cwd="/",
        env={"COCOLA_SCM_TOKEN": token},
        stdin=json.dumps(
            {
                "operation": "publish",
                "spec": spec.__dict__,
                "remote_clone_url": remote_clone_url,
                "expected_head_sha": expected_head_sha,
            }
        ),
        timeout_secs=600,
    )
    return _decode_result(result, "PROJECT_PUBLISH_FAILED")


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
import urllib.parse
import uuid

PLATFORM_WORKSPACE = pathlib.Path("/workspace")
WORKSPACE = PLATFORM_WORKSPACE / "project"
PLATFORM_DIRECTORIES = {"outputs", "uploads", "downloads"}
MAX_PATHS = 500
MAX_DIFF = 512 * 1024
MAX_COMMITS = 50
MAX_COMMIT_MESSAGE = 32 * 1024

"""
    + _PORCELAIN_V2_PARSER_SCRIPT
    + r"""

request = json.loads(sys.stdin.read())
spec = request["spec"]
spec["base_ref"] = spec.get("base_ref") or spec.get("default_branch", "")
repository_provider = spec.get("repository_provider", "github")

def fail(code, message):
    print(json.dumps({"ok": False, "code": code, "error": message}))
    raise SystemExit(0)

def git(args, cwd=WORKSPACE, env=None, timeout=60, binary=False):
    command = [
        "git", "-c", "credential.helper=", "-c", "core.hooksPath=/dev/null",
        "-c", "core.fsmonitor=false", "-c", "safe.directory=" + str(cwd), *args,
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

def validate_marker(path):
    if not path.is_file():
        fail("PROJECT_WORKSPACE_MISMATCH", "Project workspace marker is missing")
    try:
        marker = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        fail("PROJECT_WORKSPACE_MISMATCH", "Project workspace marker is invalid")
    if repository_provider == "local" and not spec.get("base_sha"):
        spec["base_sha"] = marker.get("base_sha", "")
    workspace_repository_id = 0 if repository_provider == "local" else int(spec["repository_id"])
    if (
        marker.get("project_id") != spec["project_id"]
        or int(marker.get("repository_id", 0)) != workspace_repository_id
        or marker.get("repository_provider", "github") != repository_provider
        or marker.get("task_branch") != spec["task_branch"]
        or marker.get("base_sha", "").lower() != spec["base_sha"].lower()
    ):
        fail("PROJECT_WORKSPACE_MISMATCH", "Workspace belongs to a different project")

def validate_workspace():
    validate_marker(marker_path())
    branch = git(["rev-parse", "--abbrev-ref", "HEAD"])
    if branch.returncode != 0 or branch.stdout.strip() != spec["task_branch"]:
        fail("PROJECT_WORKSPACE_MISMATCH", "Workspace is on an unexpected branch")

def remove_entry(path):
    if path.is_symlink() or path.is_file():
        path.unlink()
    elif path.is_dir():
        shutil.rmtree(path)

def link_or_copy(source, target):
    source = pathlib.Path(source)
    target = pathlib.Path(target)
    target.parent.mkdir(parents=True, exist_ok=True)
    try:
        os.link(source, target)
    except OSError:
        shutil.copy2(source, target)

def copy_entry(source, target):
    if source.is_symlink():
        target.parent.mkdir(parents=True, exist_ok=True)
        target.symlink_to(os.readlink(source))
    elif source.is_dir():
        shutil.copytree(
            source, target, symlinks=True, copy_function=link_or_copy,
        )
    else:
        link_or_copy(source, target)

def legacy_marker_path():
    return PLATFORM_WORKSPACE / ".git" / "cocola-project.json"

def legacy_tracked_platform_paths():
    tracked = git(
        ["ls-files", "-z", "--", *sorted(PLATFORM_DIRECTORIES)],
        cwd=PLATFORM_WORKSPACE,
        binary=True,
    )
    if tracked.returncode != 0:
        fail("PROJECT_WORKSPACE_MIGRATION_FAILED", "Could not inspect the legacy workspace")
    paths = []
    for raw in tracked.stdout.split(b"\0"):
        if not raw:
            continue
        value = raw.decode("utf-8", "surrogateescape")
        parts = pathlib.PurePosixPath(value).parts
        if not parts or parts[0] not in PLATFORM_DIRECTORIES or ".." in parts:
            fail("PROJECT_WORKSPACE_MIGRATION_FAILED", "Legacy workspace path is invalid")
        paths.append(value)
    return paths

def cleanup_legacy_workspace(tracked_platform_paths):
    for entry in list(PLATFORM_WORKSPACE.iterdir()):
        if (
            entry.name in PLATFORM_DIRECTORIES
            or entry.name in {"project", "lost+found"}
            or entry.name.startswith(".cocola-project-migrate-")
            or entry.name.startswith(".cocola-clone-")
        ):
            continue
        remove_entry(entry)
    for value in tracked_platform_paths:
        source = PLATFORM_WORKSPACE / value
        if source.is_symlink() or source.is_file():
            source.unlink()
        parent = source.parent
        platform_root = PLATFORM_WORKSPACE / pathlib.PurePosixPath(value).parts[0]
        while parent != platform_root:
            try:
                parent.rmdir()
            except OSError:
                break
            parent = parent.parent

def migrate_legacy_workspace():
    legacy_marker = legacy_marker_path()
    if not legacy_marker.is_file():
        return
    if WORKSPACE.exists():
        if any(WORKSPACE.iterdir()):
            fail(
                "PROJECT_WORKSPACE_MIGRATION_CONFLICT",
                "The legacy Project cannot be migrated because /workspace/project is not empty",
            )
        WORKSPACE.rmdir()

    tracked_platform_paths = legacy_tracked_platform_paths()
    stage = PLATFORM_WORKSPACE / (".cocola-project-migrate-" + uuid.uuid4().hex)
    stage.mkdir()
    try:
        for entry in PLATFORM_WORKSPACE.iterdir():
            if (
                entry.name in PLATFORM_DIRECTORIES
                or entry.name in {"project", "lost+found", stage.name}
                or entry.name.startswith(".cocola-project-migrate-")
                or entry.name.startswith(".cocola-clone-")
            ):
                continue
            copy_entry(entry, stage / entry.name)
        for value in tracked_platform_paths:
            source = PLATFORM_WORKSPACE / value
            if source.is_symlink() or source.is_file():
                copy_entry(source, stage / value)
        validate_marker(stage / ".git" / "cocola-project.json")
        branch = git(["rev-parse", "--abbrev-ref", "HEAD"], cwd=stage)
        if branch.returncode != 0 or branch.stdout.strip() != spec["task_branch"]:
            fail("PROJECT_WORKSPACE_MIGRATION_FAILED", "Legacy workspace branch is invalid")
        os.replace(stage, WORKSPACE)
    finally:
        if stage.exists():
            shutil.rmtree(stage, ignore_errors=True)
    cleanup_legacy_workspace(tracked_platform_paths)

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
    if legacy_marker_path().is_file():
        migrate_legacy_workspace()
        validate_workspace()
        configure_author(WORKSPACE)
        return
    PLATFORM_WORKSPACE.mkdir(parents=True, exist_ok=True)
    if WORKSPACE.exists():
        if any(WORKSPACE.iterdir()):
            fail("PROJECT_WORKSPACE_MISMATCH", "Project worktree is not empty")
        WORKSPACE.rmdir()
    if repository_provider == "local":
        if spec.get("base_sha"):
            fail(
                "LOCAL_PROJECT_WORKSPACE_LOST",
                "The local Project volume is unavailable and cannot be reinitialized safely",
            )
        WORKSPACE.mkdir()
        initialized = git(["init", "-b", "main"], cwd=WORKSPACE)
        if initialized.returncode != 0:
            fail("PROJECT_GIT_INIT_FAILED", "Could not initialize the local project")
        configure_author(WORKSPACE)
        committed = git(
            ["commit", "--allow-empty", "-m", "Initialize empty Cocola project"],
            cwd=WORKSPACE,
        )
        if committed.returncode != 0:
            fail("PROJECT_GIT_INIT_FAILED", "Could not create the initial project commit")
        head = git(["rev-parse", "HEAD"], cwd=WORKSPACE)
        if head.returncode != 0:
            fail("PROJECT_GIT_INIT_FAILED", "Could not read the initial project revision")
        spec["base_sha"] = head.stdout.strip()
        marker = {
            "schema_version": 2,
            "project_id": spec["project_id"],
            "repository_id": 0,
            "repository_provider": "local",
            "task_branch": "main",
            "base_sha": spec["base_sha"],
        }
        marker_path().write_text(json.dumps(marker, sort_keys=True), encoding="utf-8")
        return
    stage = PLATFORM_WORKSPACE / (".cocola-clone-" + uuid.uuid4().hex)
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
            "schema_version": 2,
            "project_id": spec["project_id"],
            "repository_id": int(spec["repository_id"]),
            "repository_provider": repository_provider,
            "task_branch": spec["task_branch"],
            "base_sha": spec["base_sha"],
        }
        (stage / ".git" / "cocola-project.json").write_text(
            json.dumps(marker, sort_keys=True), encoding="utf-8"
        )
        # Publish the complete checkout atomically so /workspace/project never
        # exposes a partially initialized Git worktree.
        os.replace(stage, WORKSPACE)
    finally:
        os.environ.pop("COCOLA_SCM_TOKEN", None)
        shutil.rmtree(askpass_dir, ignore_errors=True)
        if stage.exists():
            shutil.rmtree(stage, ignore_errors=True)

def normalize_refs(value):
    refs = []
    for raw in value.split(", "):
        ref = raw.strip()
        if not ref:
            continue
        if ref.startswith("HEAD -> "):
            refs.append("HEAD")
            ref = ref[len("HEAD -> "):]
        if ref.startswith("tag: "):
            ref = ref[len("tag: "):]
        for prefix in ("refs/heads/", "refs/remotes/", "refs/tags/"):
            if ref.startswith(prefix):
                ref = ref[len(prefix):]
                break
        if ref and ref not in refs:
            refs.append(ref)
    return refs

def parse_commit_record(record):
    fields = record.strip("\r\n").split("\x1f")
    if len(fields) != 6 or len(fields[0]) != 40:
        return None
    return {
        "sha": fields[0],
        "parents": [value for value in fields[1].split() if value],
        "subject": fields[2][:500],
        "author_name": fields[3][:200],
        "authored_at": fields[4],
        "refs": normalize_refs(fields[5])[:20],
    }

def commit_history():
    result = git([
        "log", "HEAD", "--topo-order", "--decorate=full",
        "--max-count=" + str(MAX_COMMITS + 1),
        "--format=%H%x1f%P%x1f%s%x1f%an%x1f%aI%x1f%D%x1e",
    ])
    if result.returncode != 0:
        fail("GIT_HISTORY_FAILED", "Could not read Git commit history")
    commits = []
    for record in result.stdout.split("\x1e"):
        commit = parse_commit_record(record)
        if commit is not None:
            commits.append(commit)
    return commits[:MAX_COMMITS], len(commits) > MAX_COMMITS

def validate_commit(commit_sha):
    ancestor = git(["merge-base", "--is-ancestor", commit_sha, "HEAD"])
    if ancestor.returncode != 0:
        fail("GIT_COMMIT_INVALID", "Commit is not reachable from the current HEAD")

def commit_parent(commit_sha):
    result = git(["rev-list", "--parents", "--max-count=1", commit_sha])
    if result.returncode != 0:
        fail("GIT_COMMIT_INVALID", "Could not read Git commit parents")
    values = result.stdout.strip().split()
    return values[1] if len(values) > 1 else ""

def commit_metadata(commit_sha):
    result = git([
        "show", "-s", "--decorate=full",
        "--format=%H%x1f%P%x1f%s%x1f%an%x1f%aI%x1f%D%x1e%B",
        commit_sha,
    ])
    if result.returncode != 0:
        fail("GIT_COMMIT_INVALID", "Could not read Git commit details")
    metadata, separator, body = result.stdout.partition("\x1e")
    commit = parse_commit_record(metadata)
    if commit is None or not separator:
        fail("GIT_COMMIT_INVALID", "Git commit details are invalid")
    commit["body"] = body.strip()[:MAX_COMMIT_MESSAGE]
    return commit

def commit_name_status(commit_sha, parent):
    if parent:
        result = git(["diff", "--name-status", "-z", "-M", parent, commit_sha], binary=True)
    else:
        result = git([
            "diff-tree", "--root", "--no-commit-id", "--name-status", "-r", "-z", "-M",
            commit_sha,
        ], binary=True)
    if result.returncode != 0:
        fail("GIT_COMMIT_FAILED", "Could not read files changed by the commit")
    records = result.stdout.split(b"\0")
    files = []
    index = 0
    while index < len(records):
        status = records[index].decode("utf-8", "replace")
        index += 1
        if not status or index >= len(records):
            continue
        old_path = ""
        if status[:1] in {"R", "C"}:
            old_path = records[index].decode("utf-8", "surrogateescape")
            index += 1
            if index >= len(records):
                break
        path = records[index].decode("utf-8", "surrogateescape")
        index += 1
        files.append({
            "path": path,
            "old_path": old_path,
            "status": status[:1] or "M",
            "binary": False,
        })
    return files

def commit_numstat(commit_sha, parent):
    if parent:
        result = git(["diff", "--numstat", "-z", "--no-renames", parent, commit_sha], binary=True)
    else:
        result = git([
            "show", "--format=", "--numstat", "-z", "--no-renames", commit_sha,
        ], binary=True)
    if result.returncode != 0:
        fail("GIT_COMMIT_FAILED", "Could not read commit statistics")
    additions = 0
    deletions = 0
    binary_paths = set()
    for raw in result.stdout.split(b"\0"):
        if not raw:
            continue
        fields = raw.decode("utf-8", "surrogateescape").split("\t", 2)
        if len(fields) != 3:
            continue
        if fields[0] == "-" or fields[1] == "-":
            binary_paths.add(fields[2])
            continue
        try:
            additions += int(fields[0])
            deletions += int(fields[1])
        except ValueError:
            continue
    return additions, deletions, binary_paths

def commit_details(commit_sha):
    validate_commit(commit_sha)
    parent = commit_parent(commit_sha)
    commit = commit_metadata(commit_sha)
    files = commit_name_status(commit_sha, parent)
    additions, deletions, binary_paths = commit_numstat(commit_sha, parent)
    for value in files:
        value["binary"] = value["path"] in binary_paths
    commit.update({
        "files_changed": len(files),
        "additions": additions,
        "deletions": deletions,
    })
    return commit, files[:MAX_PATHS], len(files) > MAX_PATHS, parent

def commit_file_diff(commit_sha, parent, path):
    if parent:
        args = [
            "diff", "--no-ext-diff", "--no-textconv", "--find-renames",
            parent, commit_sha, "--", path,
        ]
    else:
        args = [
            "show", "--format=", "--no-ext-diff", "--no-textconv", "--find-renames",
            commit_sha, "--", path,
        ]
    result = git(args, binary=True)
    if result.returncode != 0:
        fail("GIT_DIFF_FAILED", "Could not read Git commit diff")
    binary = b"Binary files " in result.stdout or b"GIT binary patch" in result.stdout
    truncated = len(result.stdout) > MAX_DIFF
    return result.stdout[:MAX_DIFF].decode("utf-8", "replace"), binary, truncated

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
    commits, history_truncated = commit_history()
    return {
        "branch": branch,
        "base_ref": spec["base_ref"],
        "base_sha": spec["base_sha"],
        "head_sha": head.stdout.strip(),
        "ahead": ahead_count,
        "dirty": bool(changes),
        "changes": changes,
        "truncated": truncated,
        "commits": commits,
        "history_truncated": history_truncated,
    }

def publish():
    if repository_provider != "local":
        fail("PROJECT_PUBLISH_INVALID", "Only local Projects can be published")
    validate_workspace()
    snapshot = status_snapshot()
    if snapshot["dirty"]:
        fail("PROJECT_PUBLISH_DIRTY", "Commit local changes before publishing")
    expected_head = request.get("expected_head_sha", "")
    if snapshot["head_sha"].lower() != expected_head.lower():
        fail("PROJECT_PUBLISH_HEAD_CHANGED", "Project HEAD changed before publishing")
    remote_url = request.get("remote_clone_url", "")
    remote = urllib.parse.urlsplit(remote_url)
    if (
        remote.scheme != "https" or remote.hostname != "github.com"
        or remote.username is not None or remote.password is not None
        or not remote_url.endswith(".git")
    ):
        fail("PROJECT_PUBLISH_INVALID", "GitHub clone URL is invalid")
    existing = git(["remote", "get-url", "origin"])
    if existing.returncode == 0 and existing.stdout.strip() != remote_url:
        fail("PROJECT_REMOTE_MISMATCH", "Workspace already has a different origin")
    askpass_dir = pathlib.Path(tempfile.mkdtemp(prefix="cocola-publish-"))
    askpass = askpass_dir / "askpass.sh"
    askpass.write_text(
        '#!/bin/sh\ncase "$1" in *Username*) printf "%s" "x-access-token" ;; '
        '*) printf "%s" "$COCOLA_SCM_TOKEN" ;; esac\n',
        encoding="utf-8",
    )
    askpass.chmod(0o700)
    try:
        pushed = git(
            ["push", remote_url, expected_head + ":refs/heads/main"],
            env={"GIT_ASKPASS": str(askpass)}, timeout=600,
        )
        if pushed.returncode != 0:
            fail("PROJECT_PUBLISH_PUSH_FAILED", "Could not push the Project to GitHub")
        if existing.returncode != 0:
            added = git(["remote", "add", "origin", remote_url])
            if added.returncode != 0:
                fail("PROJECT_REMOTE_CONFIG_FAILED", "Could not save the GitHub origin")
    finally:
        os.environ.pop("COCOLA_SCM_TOKEN", None)
        shutil.rmtree(askpass_dir, ignore_errors=True)
    return snapshot["head_sha"]

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
elif operation == "commit":
    commit_sha = request.get("commit_sha", "")
    commit, commit_files, files_truncated, parent = commit_details(commit_sha)
    path = request.get("path", "")
    diff = ""
    binary = False
    diff_truncated = False
    if path:
        diff, binary, diff_truncated = commit_file_diff(commit_sha, parent, path)
    print(json.dumps({
        "ok": True,
        "snapshot": status_snapshot(),
        "commit": commit,
        "commit_files": commit_files,
        "diff": diff,
        "binary": binary,
        "truncated": diff_truncated if path else files_truncated,
    }))
elif operation == "publish":
    print(json.dumps({"ok": True, "head_sha": publish()}))
else:
    fail("GIT_OPERATION_INVALID", "Git operation is invalid")
"""
)
