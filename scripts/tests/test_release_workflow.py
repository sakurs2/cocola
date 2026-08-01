from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = (ROOT / ".github" / "workflows" / "release.yml").read_text()


def test_stable_release_explicitly_publishes_latest() -> None:
    assert "latest=false" in WORKFLOW
    assert "type=raw,value=latest,enable=${{ !contains(github.ref_name, '-') }}" in WORKFLOW


def test_cli_release_waits_for_anonymous_images() -> None:
    assert "Verify anonymous image access" in WORKFLOW
    assert "docker logout ghcr.io" in WORKFLOW
    assert 'docker buildx imagetools inspect "$IMAGE"' in WORKFLOW
    assert "Set the GHCR package visibility to Public" in WORKFLOW
