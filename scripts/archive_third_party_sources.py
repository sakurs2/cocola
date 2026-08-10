#!/usr/bin/env python3
"""Download and verify the source bundle required by mirrored OCI images."""

from __future__ import annotations

import argparse
import hashlib
import shutil
import subprocess
import sys
from pathlib import Path

REPOSITORY_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(REPOSITORY_ROOT))

from scripts.generate_third_party_images import (  # noqa: E402
    DEFAULT_LOCK_PATH,
    load_and_validate_lock,
)


def download(url: str, destination: Path, attempts: int = 4) -> None:
    curl = shutil.which("curl")
    if curl is None:
        raise RuntimeError("curl is required to build the third-party source bundle")
    completed = subprocess.run(  # noqa: S603
        [
            curl,
            "--fail",
            "--location",
            "--silent",
            "--show-error",
            "--proto",
            "=https",
            "--proto-redir",
            "=https",
            "--retry",
            str(max(0, attempts - 1)),
            "--retry-all-errors",
            "--connect-timeout",
            "20",
            "--max-time",
            "300",
            "--user-agent",
            "cocola-source-archive/1",
            "--output",
            str(destination),
            url,
        ],
        check=False,
        capture_output=True,
        text=True,
    )
    if completed.returncode != 0:
        destination.unlink(missing_ok=True)
        diagnostic = completed.stderr.strip() or f"curl exited with {completed.returncode}"
        raise RuntimeError(f"download {url}: {diagnostic}")


def file_sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def build_source_bundle(lock_path: Path, output_directory: Path) -> None:
    lock = load_and_validate_lock(lock_path)
    output_directory.mkdir(parents=True, exist_ok=True)
    if any(output_directory.iterdir()):
        raise ValueError(f"output directory must be empty: {output_directory}")

    shutil.copyfile(lock_path, output_directory / lock_path.name)
    notice = [
        "# Cocola third-party container image sources",
        "",
        f"Lock revision: `{lock['revision']}`",
        "",
        "The mirrored images are unmodified copies of their upstream multi-platform OCI images.",
        "The archives and license texts below correspond to the exact revisions "
        "recorded in the lock file.",
        "",
        "| Dependency | Distributed image | Upstream source | License |",
        "| --- | --- | --- | --- |",
    ]
    for image in lock["images"]:
        if not image["mirror"]:
            continue
        distributed = f"{image['target_image']}:{image['tag']}@{image['manifest_digest']}"
        licenses = ", ".join(image["licenses"])
        notice.append(
            f"| {image['id']} | `{distributed}` | "
            f"[{image['source_ref']}]({image['source_repository']}) | {licenses} |"
        )
        for archive in image["source_archives"]:
            destination = output_directory / archive["name"]
            download(archive["url"], destination)
            actual = file_sha256(destination)
            if actual != archive["sha256"]:
                raise ValueError(
                    f"{archive['name']} checksum mismatch: expected "
                    f"{archive['sha256']}, got {actual}"
                )
        for index, url in enumerate(image["license_urls"], start=1):
            download(url, output_directory / f"{image['id']}-LICENSE-{index}.txt")

    (output_directory / "THIRD_PARTY_SOURCES.md").write_text(
        "\n".join(notice) + "\n", encoding="utf-8"
    )
    checksum_files = sorted(path for path in output_directory.iterdir() if path.is_file())
    checksums = "".join(f"{file_sha256(path)}  {path.name}\n" for path in checksum_files)
    (output_directory / "SHA256SUMS").write_text(checksums, encoding="utf-8")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--lock", type=Path, default=DEFAULT_LOCK_PATH)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    try:
        build_source_bundle(args.lock, args.output)
    except (OSError, RuntimeError, ValueError) as error:
        parser.error(str(error))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
