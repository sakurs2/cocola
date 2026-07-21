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
    validate_relative_path,
    validate_spec,
)
from cocola_agent_runtime.sandbox_binder import ExecOutcome


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


def test_bootstrap_configures_repository_local_git_author(tmp_path):
    workspace = tmp_path / "workspace"
    workspace.mkdir()
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
    script = _PROJECT_GIT_SCRIPT.replace(
        'pathlib.Path("/workspace")', f"pathlib.Path({str(workspace)!r})", 1
    )

    result = subprocess.run(
        [sys.executable, "-c", script],
        input=json.dumps({"operation": "bootstrap", "spec": spec}),
        check=True,
        capture_output=True,
        text=True,
    )

    assert json.loads(result.stdout)["ok"] is True
    author_name = subprocess.run(
        ["git", "config", "--local", "--get", "user.name"],
        cwd=workspace,
        check=False,
        capture_output=True,
        text=True,
    ).stdout.strip()
    author_email = subprocess.run(
        ["git", "config", "--local", "--get", "user.email"],
        cwd=workspace,
        check=False,
        capture_output=True,
        text=True,
    ).stdout.strip()
    assert author_name == "Octo Cat"
    assert author_email == "octo@example.com"


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
    script = _PROJECT_GIT_SCRIPT.replace(
        'pathlib.Path("/workspace")', f"pathlib.Path({str(workspace)!r})", 1
    )

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
            cwd=workspace,
            check=True,
            capture_output=True,
            text=True,
        ).stdout.strip()
        == "Octo Cat"
    )
    assert (
        subprocess.run(
            ["git", "config", "--local", "--get", "user.email"],
            cwd=workspace,
            check=True,
            capture_output=True,
            text=True,
        ).stdout.strip()
        == "octo@example.com"
    )
    (workspace / "work.txt").write_text("local change\n", encoding="utf-8")
    subprocess.run(["git", "add", "work.txt"], cwd=workspace, check=True)
    commit = subprocess.run(
        ["git", "commit", "-m", "local change"],
        cwd=workspace,
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
