#!/usr/bin/env python3
"""Validate the third-party image lock and generate Cocola CLI constants."""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path
from typing import Any

REPOSITORY_ROOT = Path(__file__).resolve().parents[1]
DEFAULT_LOCK_PATH = REPOSITORY_ROOT / "deploy" / "third-party-images.lock.json"
DEFAULT_OUTPUT_PATH = (
    REPOSITORY_ROOT / "apps" / "cli" / "internal" / "config" / "third_party_images_generated.go"
)
EXPECTED_IMAGES = (
    "redis",
    "postgres",
    "forgejo",
    "minio",
    "minio-mc",
    "openviking",
    "opensandbox-server",
    "opensandbox-egress",
)
GO_NAMES = {
    "redis": ("redisTargetImage", "redisVersion", "redisManifestDigest"),
    "postgres": ("postgresTargetImage", "postgresVersion", "postgresManifestDigest"),
    "forgejo": ("forgejoTargetImage", "forgejoVersion", "forgejoManifestDigest"),
    "minio": ("minioTargetImage", "minioVersion", "minioManifestDigest"),
    "minio-mc": (
        "minioClientTargetImage",
        "minioClientVersion",
        "minioClientManifestDigest",
    ),
    "openviking": ("openVikingTargetImage", "openVikingVersion", "openVikingDigest"),
    "opensandbox-server": (
        "openSandboxServerTargetImage",
        "openSandboxServerVersion",
        "openSandboxServerDigest",
    ),
    "opensandbox-egress": (
        "openSandboxEgressTargetImage",
        "openSandboxEgressVersion",
        "openSandboxEgressDigest",
    ),
}
SHA256_PATTERN = re.compile(r"^[0-9a-f]{64}$")
DIGEST_PATTERN = re.compile(r"^sha256:[0-9a-f]{64}$")
COMMIT_PATTERN = re.compile(r"^[0-9a-f]{40}$")


def load_and_validate_lock(path: Path) -> dict[str, Any]:
    try:
        lock = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise ValueError(f"read {path}: {error}") from error

    if lock.get("schema_version") != 1:
        raise ValueError("third-party image lock schema_version must be 1")
    revision = lock.get("revision")
    if not isinstance(revision, str) or not re.fullmatch(r"[0-9]{8}\.[1-9][0-9]*", revision):
        raise ValueError("third-party image lock revision must use YYYYMMDD.N format")
    images = lock.get("images")
    if not isinstance(images, list):
        raise ValueError("third-party image lock images must be a list")
    ids = tuple(image.get("id") for image in images if isinstance(image, dict))
    if ids != EXPECTED_IMAGES:
        raise ValueError(f"third-party images must be ordered exactly as {EXPECTED_IMAGES!r}")

    targets: set[tuple[str, str]] = set()
    archive_names: set[str] = set()
    for image in images:
        validate_image(image, targets, archive_names)
    return lock


def validate_image(
    image: dict[str, Any],
    targets: set[tuple[str, str]],
    archive_names: set[str],
) -> None:
    image_id = image["id"]
    for key in (
        "upstream_image",
        "tag",
        "manifest_digest",
        "target_image",
        "source_repository",
        "source_ref",
        "source_commit",
        "image_source_repository",
        "image_source_commit",
    ):
        if not isinstance(image.get(key), str) or not image[key].strip():
            raise ValueError(f"{image_id}: {key} must be a non-empty string")
    if not isinstance(image.get("mirror"), bool):
        raise ValueError(f"{image_id}: mirror must be a boolean")
    if not DIGEST_PATTERN.fullmatch(image["manifest_digest"]):
        raise ValueError(f"{image_id}: manifest_digest must be a sha256 digest")
    if not COMMIT_PATTERN.fullmatch(image["source_commit"]):
        raise ValueError(f"{image_id}: source_commit must be a full Git commit")
    if not COMMIT_PATTERN.fullmatch(image["image_source_commit"]):
        raise ValueError(f"{image_id}: image_source_commit must be a full Git commit")
    if image.get("platforms") != ["linux/amd64", "linux/arm64"]:
        raise ValueError(f"{image_id}: platforms must contain amd64 and arm64 in canonical order")
    if not image["target_image"].startswith("ghcr.io/"):
        raise ValueError(f"{image_id}: runtime targets must use GHCR")
    if image["mirror"] and not image["target_image"].startswith("ghcr.io/sakurs2/cocola-"):
        raise ValueError(f"{image_id}: mirrored targets must use Cocola's GHCR namespace")
    if not image["mirror"] and image["target_image"] != image["upstream_image"]:
        raise ValueError(f"{image_id}: non-mirrored target must equal its upstream image")
    target = (image["target_image"], image["tag"])
    if target in targets:
        raise ValueError(f"{image_id}: duplicate target tag {target[0]}:{target[1]}")
    targets.add(target)

    licenses = image.get("licenses")
    license_urls = image.get("license_urls")
    if not isinstance(licenses, list) or not licenses or not all(licenses):
        raise ValueError(f"{image_id}: licenses must be a non-empty list")
    if not isinstance(license_urls, list) or not license_urls:
        raise ValueError(f"{image_id}: license_urls must be a non-empty list")
    for url in license_urls:
        require_https_url(image_id, "license URL", url)

    archives = image.get("source_archives")
    if not isinstance(archives, list) or not archives:
        raise ValueError(f"{image_id}: source_archives must be a non-empty list")
    for archive in archives:
        if not isinstance(archive, dict):
            raise ValueError(f"{image_id}: each source archive must be an object")
        name = archive.get("name")
        if not isinstance(name, str) or Path(name).name != name or not name.endswith(".tar.gz"):
            raise ValueError(f"{image_id}: archive name must be a plain .tar.gz filename")
        if name in archive_names:
            raise ValueError(f"{image_id}: duplicate archive name {name}")
        archive_names.add(name)
        require_https_url(image_id, "archive URL", archive.get("url"))
        if not isinstance(archive.get("sha256"), str) or not SHA256_PATTERN.fullmatch(
            archive["sha256"]
        ):
            raise ValueError(f"{image_id}: archive sha256 must contain 64 lowercase hex digits")


def require_https_url(image_id: str, field: str, value: object) -> None:
    if not isinstance(value, str) or not value.startswith("https://"):
        raise ValueError(f"{image_id}: {field} must use https")


def render_go(lock: dict[str, Any]) -> str:
    values = {image["id"]: image for image in lock["images"]}
    entries: list[tuple[str, str]] = []
    for image_id in EXPECTED_IMAGES:
        target_name, version_name, digest_name = GO_NAMES[image_id]
        image = values[image_id]
        entries.extend(
            (
                (target_name, image["target_image"]),
                (version_name, image["tag"]),
                (digest_name, image["manifest_digest"]),
            )
        )
    name_width = max(len(name) for name, _ in entries)
    lines = [
        "// Code generated by scripts/generate_third_party_images.py; DO NOT EDIT.",
        "",
        "package config",
        "",
        "const (",
    ]
    lines.extend(f'\t{name:<{name_width}} = "{value}"' for name, value in entries)
    lines.extend((")", ""))
    return "\n".join(lines)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--check", action="store_true")
    mode.add_argument("--write", action="store_true")
    parser.add_argument("--lock", type=Path, default=DEFAULT_LOCK_PATH)
    parser.add_argument("--output", type=Path, default=DEFAULT_OUTPUT_PATH)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        generated = render_go(load_and_validate_lock(args.lock))
    except ValueError as error:
        print(f"third-party image lock validation failed: {error}", file=sys.stderr)
        return 1
    if args.write:
        args.output.write_text(generated, encoding="utf-8")
        return 0
    try:
        current = args.output.read_text(encoding="utf-8")
    except OSError as error:
        print(f"read generated Go constants: {error}", file=sys.stderr)
        return 1
    if current != generated:
        print(
            "third-party image constants are stale; run "
            "python3 scripts/generate_third_party_images.py --write",
            file=sys.stderr,
        )
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
