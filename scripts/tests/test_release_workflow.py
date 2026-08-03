from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = (ROOT / ".github" / "workflows" / "release.yml").read_text()
WEB_DOCKERFILE = (ROOT / "apps" / "web" / "Dockerfile").read_text()


def test_stable_release_explicitly_publishes_latest() -> None:
    assert "latest=false" in WORKFLOW
    assert "type=raw,value=latest,enable=${{ !contains(github.ref_name, '-') }}" in WORKFLOW


def test_cli_release_waits_for_anonymous_images() -> None:
    assert "Verify anonymous image access" in WORKFLOW
    assert "docker logout ghcr.io" in WORKFLOW
    assert 'docker buildx imagetools inspect "$IMAGE"' in WORKFLOW
    assert "Set the GHCR package visibility to Public" in WORKFLOW


def test_unchanged_sandbox_runtime_reuses_the_previous_digest() -> None:
    assert "Detect an unchanged sandbox runtime" in WORKFLOW
    assert 'git diff --quiet "${previous_tag}^{commit}"' in WORKFLOW
    assert "-- deploy/sandbox-runtime" in WORKFLOW
    assert 'docker buildx imagetools inspect "${IMAGE_NAME}:${previous_tag}"' in WORKFLOW
    assert 'docker buildx imagetools create "${tag_args[@]}" "$SOURCE_IMAGE"' in WORKFLOW
    assert (
        "if: matrix.name != 'sandbox-runtime' || steps.sandbox_runtime.outputs.reuse != 'true'"
    ) in WORKFLOW


def test_reused_sandbox_runtime_is_still_verified_by_release_tag() -> None:
    assert (
        "IMAGE: ghcr.io/${{ github.repository_owner }}/"
        "cocola-sandbox-runtime:${{ github.ref_name }}"
    ) in WORKFLOW
    assert 'docker run --rm --platform linux/amd64 "$IMAGE"' in WORKFLOW


def test_web_image_copies_pnpm_patches_before_install() -> None:
    copy_patches = WEB_DOCKERFILE.index("COPY patches ./patches")
    frozen_install = WEB_DOCKERFILE.index("RUN pnpm install --frozen-lockfile")
    assert copy_patches < frozen_install
