from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = (ROOT / ".github" / "workflows" / "release.yml").read_text()
CI_WORKFLOW = (ROOT / ".github" / "workflows" / "ci.yml").read_text()
GORELEASER_CONFIG = (ROOT / ".goreleaser.yml").read_text()
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
    assert "needs: [promote-images, forgejo-image]" in cli_job


def test_cli_release_waits_for_anonymous_images() -> None:
    assert "Verify anonymous image access" in WORKFLOW
    assert "docker logout ghcr.io" in WORKFLOW
    assert 'docker buildx imagetools inspect "$IMAGE"' in WORKFLOW
    assert "Set the GHCR package visibility to Public" in WORKFLOW


def test_forgejo_mirror_is_immutable_multiarch_and_public() -> None:
    forgejo_job, _ = WORKFLOW.split("\n  images:\n", 1)
    assert "regclient/actions/regctl-installer@1b705e32" in forgejo_job
    assert "release: v0.9.2" in forgejo_job
    assert "sha256:3eb3107bc9de4e9d6d9e539044e6c802dc0b7be351919a145540d4cb5422bf07" in forgejo_job
    assert "Refusing to overwrite" in forgejo_job
    assert "regctl image copy --force-recursive" in forgejo_job
    assert 'index("linux/amd64")' in forgejo_job
    assert 'index("linux/arm64")' in forgejo_job
    assert "Verify anonymous mirror access" in forgejo_job


def test_forgejo_corresponding_source_is_archived_once_per_version() -> None:
    forgejo_job, _ = WORKFLOW.split("\n  images:\n", 1)

    assert "b3d7e4ac3cbccc220703097a51fa4c16bf302579" in WORKFLOW
    assert "727e46ee360f00679d66fb12aaf935513109e6988c5834394a1ccf2931bb8db7" in WORKFLOW
    assert "forgejo-source-v16.0.1" in WORKFLOW
    assert "group: forgejo-distribution-16.0.1" in forgejo_job
    assert "contents: write" in forgejo_job
    assert "${RUNNER_TEMP}/forgejo-source-${FORGEJO_VERSION}" in WORKFLOW
    assert "forgejo-${FORGEJO_VERSION}-source.tar.gz" in WORKFLOW
    assert "License: GPL-3.0-or-later" in WORKFLOW
    assert 'gh release create "$FORGEJO_SOURCE_RELEASE_TAG"' in WORKFLOW
    assert 'gh release upload "$FORGEJO_SOURCE_RELEASE_TAG"' in WORKFLOW
    assert '"$digest" != "$expected_digest"' in WORKFLOW
    assert "--latest=false" in WORKFLOW
    assert 'releases/latest" --jq' in WORKFLOW
    assert "--head" in WORKFLOW
    assert forgejo_job.index('if ! verify_asset "$release_json" "$archive_name"') < (
        forgejo_job.index("forgejo/archive/v${FORGEJO_VERSION}.tar.gz")
    )


def test_forgejo_release_commands_are_explicitly_repo_scoped() -> None:
    forgejo_job, _ = WORKFLOW.split("\n  images:\n", 1)
    release_commands = [
        line.strip() for line in forgejo_job.splitlines() if line.strip().startswith("gh release ")
    ]

    assert len(release_commands) == 4
    assert all('--repo "$GITHUB_REPOSITORY"' in command for command in release_commands)


def test_forgejo_release_metadata_validation_is_semantic() -> None:
    forgejo_job, _ = WORKFLOW.split("\n  images:\n", 1)

    assert '"$actual_title" != "$release_title"' in forgejo_job
    assert '"$actual_body" != *"$required_value"*' in forgejo_job
    assert '"$actual_body" != "$expected_body"' not in forgejo_job
    for required_value in (
        "https://codeberg.org/forgejo/forgejo",
        '"v${FORGEJO_VERSION}"',
        '"$FORGEJO_COMMIT"',
        "GPL-3.0-or-later",
        '"${TARGET_IMAGE}@${FORGEJO_MANIFEST_DIGEST}"',
        '"$FORGEJO_SOURCE_SHA256"',
    ):
        assert required_value in forgejo_job


def test_ghcr_writes_have_bounded_parallelism() -> None:
    _, images_and_remaining = WORKFLOW.split("\n  images:\n", 1)
    images_job, remaining = images_and_remaining.split("\n  promote-images:\n", 1)
    promote_job, _ = remaining.split("\n  cli:\n", 1)
    bounded_strategy = "strategy:\n      fail-fast: false\n      max-parallel: 4"

    assert bounded_strategy in images_job
    assert bounded_strategy in promote_job


def test_cli_release_keeps_the_goreleaser_checkout_clean() -> None:
    _, cli_job = WORKFLOW.split("\n  cli:\n", 1)

    assert "actions/download-artifact" not in cli_job
    assert "release-assets/" not in cli_job
    assert 'gh release upload "$GITHUB_REF_NAME"' not in cli_job
    assert 'ignore_tags:\n    - "forgejo-source-*"' in GORELEASER_CONFIG


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
