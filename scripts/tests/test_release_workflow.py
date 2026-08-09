from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = (ROOT / ".github" / "workflows" / "release.yml").read_text()
CI_WORKFLOW = (ROOT / ".github" / "workflows" / "ci.yml").read_text()
WEB_DOCKERFILE = (ROOT / "apps" / "web" / "Dockerfile").read_text()
MAKEFILE = (ROOT / "Makefile").read_text()


def test_stable_aliases_are_promoted_only_after_images_succeed() -> None:
    images_job, remaining = WORKFLOW.split("\n  promote-images:\n", 1)
    promote_job, cli_job = remaining.split("\n  cli:\n", 1)

    assert "type=ref,event=tag" in images_job
    assert "type=semver" not in images_job
    assert "type=raw,value=latest" not in images_job
    assert "needs: images" in promote_job
    assert "type=semver,pattern={{version}}" in promote_job
    assert "type=semver,pattern={{major}}.{{minor}}" in promote_job
    assert "type=raw,value=latest,enable=${{ !contains(github.ref_name, '-') }}" in promote_job
    assert "needs: promote-images" in cli_job


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


def test_web_image_reproduces_workspace_install_configuration() -> None:
    copy_manifests = WEB_DOCKERFILE.index(
        "COPY package.json pnpm-lock.yaml pnpm-workspace.yaml tsconfig.base.json .npmrc ./"
    )
    copy_ui_manifest = WEB_DOCKERFILE.index(
        "COPY packages/ui-compat/package.json ./packages/ui-compat/package.json"
    )
    frozen_install = WEB_DOCKERFILE.index("RUN pnpm install --frozen-lockfile")
    copy_ui_source = WEB_DOCKERFILE.index("COPY packages/ui-compat ./packages/ui-compat")
    web_build = WEB_DOCKERFILE.index("RUN pnpm --filter @cocola/web build")

    assert copy_manifests < frozen_install
    assert copy_ui_manifest < frozen_install
    assert copy_ui_source < web_build
    assert "corepack prepare pnpm@" not in WEB_DOCKERFILE


def test_service_modules_are_built_outside_the_go_workspace() -> None:
    assert "(cd apps/$$a && GOWORK=off go mod tidy)" in MAKEFILE
    assert "(cd apps/$$a && GOWORK=off go build" in MAKEFILE
    assert (
        '(cd "apps/$app" && GOWORK=off go build -o "/tmp/cocola-$app" "./cmd/$app")' in CI_WORKFLOW
    )
