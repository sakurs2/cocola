import json
import subprocess
import sys

import pytest
from cocola_agent_runtime.project_git import (
    _PORCELAIN_V2_PARSER_SCRIPT,
    _PROJECT_GIT_SCRIPT,
    ProjectSpec,
    ProjectWorkspaceError,
    bootstrap_project,
    inspect_project,
    project_egress_hosts,
    publish_project,
    validate_relative_path,
    validate_spec,
)
from cocola_agent_runtime.sandbox_binder import ExecOutcome


def project_script(workspace) -> str:
    return _PROJECT_GIT_SCRIPT.replace(
        'pathlib.Path("/workspace")', f"pathlib.Path({str(workspace)!r})", 1
    )


def valid_spec() -> ProjectSpec:
    return ProjectSpec(
        project_id="9ad7d767-2f20-4d67-b8ff-b604d10dd03e",
        repository_id=123,
        clone_url="https://github.com/octocat/example.git",
        default_branch="main",
        base_sha="a" * 40,
        task_branch="cocola/task-9ad7d7672f20",
        git_author_name="Octo Cat",
        git_author_email="octo@example.com",
    )


class RecordingExecutor:
    def __init__(self, payload: dict):
        self.payload = payload
        self.calls: list[dict] = []

    async def exec(self, **kwargs):
        self.calls.append(kwargs)
        return ExecOutcome(stdout=json.dumps({"ok": True, **self.payload}))


def test_project_spec_rejects_credential_in_clone_url():
    spec = valid_spec()
    invalid = ProjectSpec(**{**spec.__dict__, "clone_url": "https://token@github.com/a/b.git"})
    with pytest.raises(ProjectWorkspaceError, match="context is invalid"):
        validate_spec(invalid)


def test_project_egress_hosts_include_run_broker_only_for_github():
    assert project_egress_hosts(valid_spec(), "http://host.docker.internal:8080") == [
        "github.com",
        "api.github.com",
        "uploads.github.com",
        "objects.githubusercontent.com",
        "github-releases.githubusercontent.com",
        "host.docker.internal",
    ]
    local = ProjectSpec(
        **{
            **valid_spec().__dict__,
            "repository_id": 0,
            "repository_provider": "local",
            "clone_url": "",
            "base_sha": "",
            "default_branch": "main",
            "task_branch": "main",
            "credential_mode": "none",
        }
    )
    assert project_egress_hosts(local, "http://host.docker.internal:8080") == []
    published = ProjectSpec(
        **{
            **local.__dict__,
            "repository_id": 456,
            "repository_full_name": "octocat/published",
            "credential_mode": "broker",
        }
    )
    assert project_egress_hosts(published, "http://host.docker.internal:8080") == [
        "github.com",
        "api.github.com",
        "uploads.github.com",
        "objects.githubusercontent.com",
        "github-releases.githubusercontent.com",
        "host.docker.internal",
    ]


def test_project_egress_hosts_reject_invalid_broker_url():
    with pytest.raises(ProjectWorkspaceError, match="broker URL is invalid"):
        project_egress_hosts(valid_spec(), "not-a-url")


@pytest.mark.parametrize(
    ("field", "value"),
    [("git_author_name", "bad\nname"), ("git_author_email", "not-an-email")],
)
def test_project_spec_rejects_invalid_git_author(field: str, value: str):
    spec = valid_spec()
    invalid = ProjectSpec(**{**spec.__dict__, field: value})
    with pytest.raises(ProjectWorkspaceError, match="context is invalid"):
        validate_spec(invalid)


@pytest.mark.parametrize("path", ["../secret", "/etc/passwd", "dir\\file", "", "."])
def test_diff_path_rejects_workspace_escape(path: str):
    with pytest.raises(ProjectWorkspaceError):
        validate_relative_path(path)


def test_diff_path_preserves_spaces_and_unicode():
    assert validate_relative_path("src/你好 world.ts") == "src/你好 world.ts"


def test_porcelain_v2_parser_marks_unmerged_record_dirty():
    namespace: dict[str, object] = {}
    exec(_PORCELAIN_V2_PARSER_SCRIPT, namespace)  # noqa: S102
    parse = namespace["parse_porcelain_v2"]
    raw = (
        b"# branch.head cocola/task-a\0"
        b"u UU N... 100644 100644 100644 100644 "
        + b"a" * 40
        + b" "
        + b"b" * 40
        + b" "
        + b"c" * 40
        + b" src/conflicted file.py\0"
    )

    branch, changes, truncated = parse(raw, "fallback", 500)

    assert branch == "cocola/task-a"
    assert changes == [
        {
            "path": "src/conflicted file.py",
            "old_path": "",
            "status": "UU",
            "area": "both",
        }
    ]
    assert truncated is False


def test_embedded_project_git_script_compiles():
    compile(_PROJECT_GIT_SCRIPT, "<project-git>", "exec")


def test_fresh_local_project_initializes_main_and_reuses_workspace(tmp_path):
    workspace = tmp_path / "workspace"
    (workspace / "outputs" / "browser").mkdir(parents=True)
    (workspace / "outputs" / "browser" / "preview.png").write_bytes(b"png")
    (workspace / "downloads").mkdir()
    worktree = workspace / "project"
    spec = {
        "project_id": "project-local",
        "repository_id": 0,
        "clone_url": "",
        "default_branch": "main",
        "base_sha": "",
        "task_branch": "main",
        "git_author_name": "Local User",
        "git_author_email": "local@example.com",
        "repository_provider": "local",
        "repository_full_name": "",
        "credential_mode": "none",
    }
    script = project_script(workspace)

    first = subprocess.run(
        [sys.executable, "-c", script],
        input=json.dumps({"operation": "bootstrap", "spec": spec}),
        check=True,
        capture_output=True,
        text=True,
    )
    first_payload = json.loads(first.stdout)
    assert first_payload["ok"] is True
    assert first_payload["snapshot"]["branch"] == "main"
    assert first_payload["snapshot"]["dirty"] is False
    base_sha = first_payload["snapshot"]["base_sha"]
    assert len(base_sha) == 40
    branch = subprocess.run(
        ["git", "rev-parse", "--abbrev-ref", "HEAD"],
        cwd=worktree,
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()
    assert branch == "main"
    assert (workspace / "outputs" / "browser").is_dir()
    assert (workspace / "outputs" / "browser" / "preview.png").is_file()
    assert (workspace / "downloads").is_dir()
    assert (worktree / ".git").is_dir()

    spec["base_sha"] = base_sha
    (worktree / "kept.txt").write_text("persisted\n", encoding="utf-8")
    second = subprocess.run(
        [sys.executable, "-c", script],
        input=json.dumps({"operation": "bootstrap", "spec": spec}),
        check=True,
        capture_output=True,
        text=True,
    )
    assert json.loads(second.stdout)["ok"] is True
    assert (worktree / "kept.txt").read_text(encoding="utf-8") == "persisted\n"


def test_git_history_and_commit_details_are_bounded_and_readable(tmp_path):
    workspace = tmp_path / "workspace"
    worktree = workspace / "project"
    spec = {
        "project_id": "project-local",
        "repository_id": 0,
        "clone_url": "",
        "default_branch": "main",
        "base_sha": "",
        "task_branch": "main",
        "git_author_name": "Local User",
        "git_author_email": "local@example.com",
        "repository_provider": "local",
        "repository_full_name": "",
        "credential_mode": "none",
    }
    script = project_script(workspace)

    bootstrap = subprocess.run(
        [sys.executable, "-c", script],
        input=json.dumps({"operation": "bootstrap", "spec": spec}),
        check=True,
        capture_output=True,
        text=True,
    )
    spec["base_sha"] = json.loads(bootstrap.stdout)["snapshot"]["base_sha"]
    (worktree / "src").mkdir()
    (worktree / "src" / "app.py").write_text('print("hello")\n', encoding="utf-8")
    subprocess.run(["git", "add", "src/app.py"], cwd=worktree, check=True)
    subprocess.run(
        ["git", "commit", "-m", "Add application", "-m", "Explain the first feature."],
        cwd=worktree,
        check=True,
        capture_output=True,
        text=True,
    )
    head_sha = subprocess.run(
        ["git", "rev-parse", "HEAD"],
        cwd=worktree,
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()

    status = subprocess.run(
        [sys.executable, "-c", script],
        input=json.dumps({"operation": "status", "spec": spec}),
        check=True,
        capture_output=True,
        text=True,
    )
    snapshot = json.loads(status.stdout)["snapshot"]
    assert snapshot["commits"][0] == {
        "sha": head_sha,
        "parents": [spec["base_sha"]],
        "subject": "Add application",
        "author_name": "Local User",
        "authored_at": snapshot["commits"][0]["authored_at"],
        "refs": ["HEAD", "main"],
    }
    assert snapshot["history_truncated"] is False

    detail = subprocess.run(
        [sys.executable, "-c", script],
        input=json.dumps({"operation": "commit", "spec": spec, "commit_sha": head_sha}),
        check=True,
        capture_output=True,
        text=True,
    )
    detail_payload = json.loads(detail.stdout)
    assert detail_payload["commit"]["body"] == ("Add application\n\nExplain the first feature.")
    assert detail_payload["commit"]["files_changed"] == 1
    assert detail_payload["commit"]["additions"] == 1
    assert detail_payload["commit"]["deletions"] == 0
    assert detail_payload["commit_files"] == [
        {"path": "src/app.py", "old_path": "", "status": "A", "binary": False}
    ]

    patch = subprocess.run(
        [sys.executable, "-c", script],
        input=json.dumps(
            {
                "operation": "commit",
                "spec": spec,
                "commit_sha": head_sha,
                "path": "src/app.py",
            }
        ),
        check=True,
        capture_output=True,
        text=True,
    )
    patch_payload = json.loads(patch.stdout)
    assert '+print("hello")' in patch_payload["diff"]
    assert patch_payload["binary"] is False


def test_git_history_is_limited_to_latest_fifty_commits(tmp_path):
    workspace = tmp_path / "workspace"
    worktree = workspace / "project"
    subprocess.run(
        ["git", "init", "-b", "main", str(worktree)],
        check=True,
        capture_output=True,
        text=True,
    )
    subprocess.run(
        [
            "git",
            "-c",
            "user.name=Local User",
            "-c",
            "user.email=local@example.com",
            "commit",
            "--allow-empty",
            "-m",
            "commit 0",
        ],
        cwd=worktree,
        check=True,
        capture_output=True,
        text=True,
    )
    base_sha = subprocess.run(
        ["git", "rev-parse", "HEAD"],
        cwd=worktree,
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()
    for index in range(1, 52):
        subprocess.run(
            [
                "git",
                "-c",
                "user.name=Local User",
                "-c",
                "user.email=local@example.com",
                "commit",
                "--allow-empty",
                "-m",
                f"commit {index}",
            ],
            cwd=worktree,
            check=True,
            capture_output=True,
            text=True,
        )
    spec = {
        "project_id": "project-local",
        "repository_id": 0,
        "clone_url": "",
        "default_branch": "main",
        "base_sha": base_sha,
        "task_branch": "main",
        "git_author_name": "Local User",
        "git_author_email": "local@example.com",
        "repository_provider": "local",
        "repository_full_name": "",
        "credential_mode": "none",
    }
    (worktree / ".git" / "cocola-project.json").write_text(
        json.dumps(
            {
                "schema_version": 1,
                "project_id": spec["project_id"],
                "repository_id": 0,
                "repository_provider": "local",
                "task_branch": "main",
                "base_sha": base_sha,
            }
        ),
        encoding="utf-8",
    )
    script = project_script(workspace)

    result = subprocess.run(
        [sys.executable, "-c", script],
        input=json.dumps({"operation": "status", "spec": spec}),
        check=True,
        capture_output=True,
        text=True,
    )
    snapshot = json.loads(result.stdout)["snapshot"]
    assert len(snapshot["commits"]) == 50
    assert snapshot["commits"][0]["subject"] == "commit 51"
    assert snapshot["history_truncated"] is True


def test_local_project_does_not_silently_reinitialize_a_lost_volume(tmp_path):
    workspace = tmp_path / "workspace"
    spec = {
        "project_id": "project-local",
        "repository_id": 0,
        "clone_url": "",
        "default_branch": "main",
        "base_sha": "a" * 40,
        "task_branch": "main",
        "git_author_name": "Local User",
        "git_author_email": "local@example.com",
        "repository_provider": "local",
        "repository_full_name": "",
        "credential_mode": "none",
    }
    script = project_script(workspace)

    result = subprocess.run(
        [sys.executable, "-c", script],
        input=json.dumps({"operation": "bootstrap", "spec": spec}),
        check=True,
        capture_output=True,
        text=True,
    )

    payload = json.loads(result.stdout)
    assert payload["ok"] is False
    assert payload["code"] == "LOCAL_PROJECT_WORKSPACE_LOST"
    assert not (workspace / "project" / ".git").exists()


def test_bootstrap_configures_repository_local_git_author(tmp_path):
    workspace = tmp_path / "workspace"
    workspace.mkdir()
    worktree = workspace / "project"
    subprocess.run(
        ["git", "init", "-b", "cocola/task-test", str(workspace)],
        check=True,
        capture_output=True,
        text=True,
    )
    (workspace / "README.md").write_text("# test\n", encoding="utf-8")
    subprocess.run(["git", "add", "README.md"], cwd=workspace, check=True)
    subprocess.run(
        [
            "git",
            "-c",
            "user.name=Seed",
            "-c",
            "user.email=seed@example.com",
            "commit",
            "-m",
            "seed",
        ],
        cwd=workspace,
        check=True,
        capture_output=True,
        text=True,
    )
    base_sha = subprocess.run(
        ["git", "rev-parse", "HEAD"],
        cwd=workspace,
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()
    spec = {
        "project_id": "project-1",
        "repository_id": 123,
        "clone_url": "https://github.com/octocat/example.git",
        "default_branch": "main",
        "base_sha": base_sha,
        "task_branch": "cocola/task-test",
        "git_author_name": "Octo Cat",
        "git_author_email": "octo@example.com",
    }
    (workspace / ".git" / "cocola-project.json").write_text(
        json.dumps(
            {
                "schema_version": 1,
                "project_id": spec["project_id"],
                "repository_id": spec["repository_id"],
                "task_branch": spec["task_branch"],
                "base_sha": spec["base_sha"],
            }
        ),
        encoding="utf-8",
    )
    (workspace / "outputs").mkdir()
    (workspace / "outputs" / "artifact.txt").write_text("preview\n", encoding="utf-8")
    script = project_script(workspace)

    result = subprocess.run(
        [sys.executable, "-c", script],
        input=json.dumps({"operation": "bootstrap", "spec": spec}),
        check=True,
        capture_output=True,
        text=True,
    )

    payload = json.loads(result.stdout)
    assert payload["ok"] is True
    assert payload["snapshot"]["dirty"] is False
    author_name = subprocess.run(
        ["git", "config", "--local", "--get", "user.name"],
        cwd=worktree,
        check=False,
        capture_output=True,
        text=True,
    ).stdout.strip()
    author_email = subprocess.run(
        ["git", "config", "--local", "--get", "user.email"],
        cwd=worktree,
        check=False,
        capture_output=True,
        text=True,
    ).stdout.strip()
    assert author_name == "Octo Cat"
    assert author_email == "octo@example.com"
    assert not (workspace / ".git").exists()
    assert (workspace / "outputs" / "artifact.txt").read_text(encoding="utf-8") == "preview\n"
    assert not (worktree / "outputs").exists()


def test_fresh_clone_configures_repository_local_git_author(tmp_path):
    source = tmp_path / "source"
    subprocess.run(
        ["git", "init", "-b", "main", str(source)],
        check=True,
        capture_output=True,
        text=True,
    )
    (source / "README.md").write_text("# test\n", encoding="utf-8")
    subprocess.run(["git", "add", "README.md"], cwd=source, check=True)
    subprocess.run(
        [
            "git",
            "-c",
            "user.name=Seed",
            "-c",
            "user.email=seed@example.com",
            "commit",
            "-m",
            "seed",
        ],
        cwd=source,
        check=True,
        capture_output=True,
        text=True,
    )
    base_sha = subprocess.run(
        ["git", "rev-parse", "HEAD"],
        cwd=source,
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()
    workspace = tmp_path / "workspace"
    worktree = workspace / "project"
    spec = {
        "project_id": "project-1",
        "repository_id": 123,
        "clone_url": str(source),
        "default_branch": "main",
        "base_sha": base_sha,
        "task_branch": "cocola/task-test",
        "git_author_name": "Octo Cat",
        "git_author_email": "octo@example.com",
    }
    script = project_script(workspace)

    result = subprocess.run(
        [sys.executable, "-c", script],
        input=json.dumps({"operation": "bootstrap", "spec": spec}),
        check=True,
        capture_output=True,
        text=True,
    )

    assert json.loads(result.stdout)["ok"] is True
    assert (
        subprocess.run(
            ["git", "config", "--local", "--get", "user.name"],
            cwd=worktree,
            check=True,
            capture_output=True,
            text=True,
        ).stdout.strip()
        == "Octo Cat"
    )
    assert (
        subprocess.run(
            ["git", "config", "--local", "--get", "user.email"],
            cwd=worktree,
            check=True,
            capture_output=True,
            text=True,
        ).stdout.strip()
        == "octo@example.com"
    )
    (worktree / "work.txt").write_text("local change\n", encoding="utf-8")
    subprocess.run(["git", "add", "work.txt"], cwd=worktree, check=True)
    commit = subprocess.run(
        ["git", "commit", "-m", "local change"],
        cwd=worktree,
        check=False,
        capture_output=True,
        text=True,
    )
    assert commit.returncode == 0, commit.stderr


@pytest.mark.asyncio
async def test_bootstrap_passes_token_only_in_process_environment():
    executor = RecordingExecutor({"snapshot": {"branch": valid_spec().task_branch}})
    result = await bootstrap_project(executor, "sandbox-1", valid_spec(), "short-lived-token")

    assert result["snapshot"]["branch"] == valid_spec().task_branch
    call = executor.calls[0]
    assert call["env"] == {"COCOLA_SCM_TOKEN": "short-lived-token"}
    assert "short-lived-token" not in " ".join(call["cmd"])
    assert "short-lived-token" not in call["stdin"]
    assert call["timeout_secs"] == 600


@pytest.mark.asyncio
async def test_publish_passes_short_lived_token_only_in_environment():
    executor = RecordingExecutor({"head_sha": "b" * 40})
    local = ProjectSpec(
        project_id="project-local",
        repository_id=0,
        clone_url="",
        default_branch="main",
        base_sha="a" * 40,
        task_branch="main",
        git_author_name="Local User",
        git_author_email="local@example.com",
        repository_provider="local",
        credential_mode="none",
    )

    result = await publish_project(
        executor,
        "sandbox-1",
        local,
        "publish-token",
        "https://github.com/octocat/published.git",
        "b" * 40,
    )

    assert result["head_sha"] == "b" * 40
    call = executor.calls[0]
    assert call["env"] == {"COCOLA_SCM_TOKEN": "publish-token"}
    assert "publish-token" not in call["stdin"]
    request = json.loads(call["stdin"])
    assert request["operation"] == "publish"
    assert request["expected_head_sha"] == "b" * 40


@pytest.mark.asyncio
async def test_inspect_diff_uses_bounded_read_only_operation():
    executor = RecordingExecutor({"snapshot": {"changes": []}, "diff": "patch", "truncated": False})
    result = await inspect_project(
        executor,
        "sandbox-1",
        valid_spec(),
        "diff",
        path="src/a file.py",
        diff_target="staged",
    )

    assert result["diff"] == "patch"
    request = json.loads(executor.calls[0]["stdin"])
    assert request["operation"] == "diff"
    assert request["path"] == "src/a file.py"
    assert request["diff_target"] == "staged"
    assert "env" not in executor.calls[0]


@pytest.mark.asyncio
async def test_inspect_commit_forwards_valid_sha_and_path():
    executor = RecordingExecutor(
        {"snapshot": {"commits": []}, "commit": {"sha": "b" * 40}, "diff": "patch"}
    )

    result = await inspect_project(
        executor,
        "sandbox-1",
        valid_spec(),
        "commit",
        path="src/app.py",
        commit_sha="b" * 40,
    )

    assert result["diff"] == "patch"
    request = json.loads(executor.calls[0]["stdin"])
    assert request["operation"] == "commit"
    assert request["commit_sha"] == "b" * 40
    assert request["path"] == "src/app.py"


@pytest.mark.asyncio
async def test_inspect_commit_rejects_short_sha_before_exec():
    executor = RecordingExecutor({})

    with pytest.raises(ProjectWorkspaceError, match="Git commit is invalid"):
        await inspect_project(
            executor,
            "sandbox-1",
            valid_spec(),
            "commit",
            commit_sha="abc123",
        )

    assert executor.calls == []
