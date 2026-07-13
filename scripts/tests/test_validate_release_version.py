import unittest

from scripts.validate_release_version import parse_version, validate_release_tag


class ParseVersionTest(unittest.TestCase):
    def test_accepts_stable_and_prerelease(self) -> None:
        self.assertEqual(parse_version("v2.0.0").core, (2, 0, 0))
        self.assertEqual(parse_version("v2.0.0-rc.2").prerelease, ("rc", "2"))

    def test_rejects_noncanonical_tags(self) -> None:
        for tag in (
            "2.0.0",
            "v2.0",
            "v02.0.0",
            "v2.0.0-rc.01",
            "v2.0.0+build.1",
            "v1.0.0-" + "a" * 122,
        ):
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                parse_version(tag)


class ValidateReleaseTagTest(unittest.TestCase):
    def test_allows_major_version_jump(self) -> None:
        # A tag-triggered workflow sees the current tag in `git tag`; it is not a prior release.
        message = validate_release_tag("v2.0.0", ["v1.0.0", "v1.0.1", "v2.0.0"])
        self.assertIn("latest stable: v1.0.1", message)

    def test_rejects_stable_regression(self) -> None:
        with self.assertRaises(ValueError):
            validate_release_tag("v1.0.1", ["v1.0.2"])

    def test_allows_ordered_prereleases_for_next_version(self) -> None:
        validate_release_tag("v2.0.0-rc.10", ["v1.9.0", "v2.0.0-rc.2"])

    def test_rejects_old_or_out_of_order_prerelease(self) -> None:
        with self.assertRaises(ValueError):
            validate_release_tag("v2.0.0-rc.1", ["v2.0.0"])
        with self.assertRaises(ValueError):
            validate_release_tag("v2.0.0-rc.1", ["v1.9.0", "v2.0.0-rc.2"])

    def test_ignores_legacy_non_semver_tags(self) -> None:
        validate_release_tag("v1.0.0", ["vlegacy", "v0.9"])


if __name__ == "__main__":
    unittest.main()
