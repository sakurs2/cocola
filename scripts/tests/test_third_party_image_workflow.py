import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = (ROOT / ".github" / "workflows" / "sync-third-party-images.yml").read_text()
LOCK = json.loads((ROOT / "deploy" / "third-party-images.lock.json").read_text())


def test_sync_is_manual_and_separate_from_normal_release() -> None:
    assert "workflow_dispatch:" in WORKFLOW
    assert "push:" not in WORKFLOW
    assert "pull_request:" not in WORKFLOW
    assert "third-party-images-${revision}" in WORKFLOW
    assert not str(LOCK["revision"]).startswith("v")


def test_workflow_uses_least_privilege_jobs_and_pinned_tool() -> None:
    assert "permissions: {}" in WORKFLOW
    assert "packages: write" in WORKFLOW
    assert "contents: write" in WORKFLOW
    assert "REGCTL_VERSION: v0.11.5" in WORKFLOW
    assert "c93aa7638749f5aaac1a8e01787321889c78f0101809bb2880343478d0ba0467" in WORKFLOW
    assert "sha256sum --check --strict" in WORKFLOW


def test_workflow_enforces_digest_platform_visibility_and_proxy_checks() -> None:
    for required in (
        "Upstream digest drift",
        "linux/amd64",
        "linux/arm64",
        "Immutable target conflict",
        "GHCR package is not public",
        "ghcr.nju.edu.cn/",
        "Immutable source asset conflict",
    ):
        assert required in WORKFLOW or required in json.dumps(LOCK)
    assert "select(.mirror)" not in WORKFLOW
    assert 'if [[ "$mirror" == "true" ]]' in WORKFLOW


def test_only_openviking_bypasses_cocola_mirroring() -> None:
    upstream = [image["id"] for image in LOCK["images"] if not image["mirror"]]
    assert upstream == ["openviking"]


def test_core_development_compose_no_longer_pulls_from_docker_hub_or_codeberg() -> None:
    files = (
        ROOT / "deploy" / "docker-compose" / "docker-compose.dev.yml",
        ROOT / "deploy" / "docker-compose" / "docker-compose.opensandbox.yml",
    )
    for path in files:
        contents = path.read_text()
        assert "docker.io/" not in contents
        assert "codeberg.org/" not in contents
        assert "image: postgres:" not in contents
        assert "image: redis:" not in contents
        assert "image: minio/" not in contents
        assert "image: opensandbox/" not in contents
