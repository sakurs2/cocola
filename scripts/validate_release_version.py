#!/usr/bin/env python3
"""Validate a Cocola release tag against existing Git tags."""

from __future__ import annotations

import re
import sys
from collections.abc import Iterable
from dataclasses import dataclass
from functools import total_ordering

TAG_PATTERN = re.compile(
    r"^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"
    r"(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$"
)


@total_ordering
@dataclass(frozen=True)
class Version:
    tag: str
    core: tuple[int, int, int]
    prerelease: tuple[str, ...] | None

    def __eq__(self, other: object) -> bool:
        if not isinstance(other, Version):
            return NotImplemented
        return self.core == other.core and self.prerelease == other.prerelease

    def __lt__(self, other: object) -> bool:
        if not isinstance(other, Version):
            return NotImplemented
        if self.core != other.core:
            return self.core < other.core
        if self.prerelease is None:
            return False
        if other.prerelease is None:
            return True
        return compare_prerelease(self.prerelease, other.prerelease) < 0


def compare_prerelease(left: tuple[str, ...], right: tuple[str, ...]) -> int:
    for index in range(min(len(left), len(right))):
        left_part = left[index]
        right_part = right[index]
        if left_part == right_part:
            continue
        left_numeric = left_part.isdigit()
        right_numeric = right_part.isdigit()
        if left_numeric and right_numeric:
            return -1 if int(left_part) < int(right_part) else 1
        if left_numeric != right_numeric:
            return -1 if left_numeric else 1
        return -1 if left_part < right_part else 1
    if len(left) == len(right):
        return 0
    return -1 if len(left) < len(right) else 1


def parse_version(tag: str) -> Version:
    if len(tag) > 128:
        raise ValueError(f"invalid release tag {tag!r}; Docker tags cannot exceed 128 characters")
    match = TAG_PATTERN.fullmatch(tag)
    if match is None:
        raise ValueError(
            f"invalid release tag {tag!r}; expected vMAJOR.MINOR.PATCH or "
            "vMAJOR.MINOR.PATCH-prerelease"
        )
    prerelease = tuple(match.group(4).split(".")) if match.group(4) else None
    has_leading_zero = prerelease and any(
        part.isdigit() and len(part) > 1 and part[0] == "0" for part in prerelease
    )
    if has_leading_zero:
        raise ValueError(
            f"invalid release tag {tag!r}; numeric prerelease identifiers cannot have leading zeros"
        )
    return Version(
        tag=tag,
        core=(int(match.group(1)), int(match.group(2)), int(match.group(3))),
        prerelease=prerelease,
    )


def validate_release_tag(current_tag: str, existing_tags: Iterable[str]) -> str:
    current = parse_version(current_tag)
    existing: list[Version] = []
    for raw_tag in existing_tags:
        tag = raw_tag.strip()
        if not tag or tag == current_tag:
            continue
        try:
            existing.append(parse_version(tag))
        except ValueError:
            # Old non-SemVer tags do not define ordering for new releases.
            continue

    stable_versions = [version for version in existing if version.prerelease is None]
    latest_stable = max(stable_versions, default=None)

    if current.prerelease is None:
        if latest_stable is not None and current <= latest_stable:
            raise ValueError(
                f"release {current.tag} must be newer than the latest stable release "
                f"{latest_stable.tag}"
            )
    else:
        if latest_stable is not None and current.core <= latest_stable.core:
            raise ValueError(
                f"prerelease {current.tag} must target a version newer than the latest "
                f"stable release {latest_stable.tag}"
            )
        same_core = [
            version
            for version in existing
            if version.core == current.core and version.prerelease is not None
        ]
        latest_prerelease = max(same_core, default=None)
        if latest_prerelease is not None and current <= latest_prerelease:
            raise ValueError(f"prerelease {current.tag} must be newer than {latest_prerelease.tag}")

    latest = latest_stable.tag if latest_stable is not None else "none"
    return f"release tag {current.tag} is valid (latest stable: {latest})"


def main() -> int:
    if len(sys.argv) != 2:
        print(f"usage: {sys.argv[0]} vMAJOR.MINOR.PATCH", file=sys.stderr)
        return 2
    try:
        message = validate_release_tag(sys.argv[1], sys.stdin)
    except ValueError as error:
        print(f"release version validation failed: {error}", file=sys.stderr)
        return 1
    print(message)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
