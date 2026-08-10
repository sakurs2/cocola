import copy
import hashlib
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from scripts.archive_third_party_sources import build_source_bundle
from scripts.generate_third_party_images import (
    DEFAULT_LOCK_PATH,
    EXPECTED_IMAGES,
    load_and_validate_lock,
    render_go,
)


class ThirdPartyImageLockTest(unittest.TestCase):
    def setUp(self) -> None:
        self.lock = json.loads(DEFAULT_LOCK_PATH.read_text(encoding="utf-8"))

    def write_lock(self, lock: dict[str, object]) -> Path:
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        path = Path(directory.name) / "images.json"
        path.write_text(json.dumps(lock), encoding="utf-8")
        return path

    def test_repository_lock_is_complete_and_generates_every_runtime_constant(self) -> None:
        lock = load_and_validate_lock(DEFAULT_LOCK_PATH)
        self.assertEqual(tuple(image["id"] for image in lock["images"]), EXPECTED_IMAGES)
        generated = render_go(lock)
        for image in lock["images"]:
            self.assertIn(image["target_image"], generated)
            self.assertIn(image["tag"], generated)
            self.assertIn(image["manifest_digest"], generated)

    def test_target_repository_change_updates_generated_cli_contract(self) -> None:
        original = render_go(load_and_validate_lock(DEFAULT_LOCK_PATH))
        lock = copy.deepcopy(self.lock)
        lock["images"][0]["target_image"] = "ghcr.io/sakurs2/cocola-redis-renamed"
        changed = render_go(load_and_validate_lock(self.write_lock(lock)))
        self.assertNotEqual(original, changed)
        self.assertIn("ghcr.io/sakurs2/cocola-redis-renamed", changed)

    def test_rejects_digest_drift_and_missing_platform(self) -> None:
        for mutation in ("digest", "platform"):
            with self.subTest(mutation=mutation):
                lock = copy.deepcopy(self.lock)
                if mutation == "digest":
                    lock["images"][0]["manifest_digest"] = "sha256:bad"
                else:
                    lock["images"][0]["platforms"] = ["linux/amd64"]
                with self.assertRaises(ValueError):
                    load_and_validate_lock(self.write_lock(lock))

    def test_rejects_duplicate_target_tags(self) -> None:
        lock = copy.deepcopy(self.lock)
        lock["images"][1]["target_image"] = lock["images"][0]["target_image"]
        lock["images"][1]["tag"] = lock["images"][0]["tag"]
        with self.assertRaisesRegex(ValueError, "duplicate target tag"):
            load_and_validate_lock(self.write_lock(lock))

    def test_source_bundle_contains_only_redistributed_image_sources(self) -> None:
        lock = copy.deepcopy(self.lock)
        for image in lock["images"]:
            for archive in image["source_archives"]:
                archive["sha256"] = hashlib.sha256(archive["url"].encode()).hexdigest()
        lock_path = self.write_lock(lock)
        output_parent = tempfile.TemporaryDirectory()
        self.addCleanup(output_parent.cleanup)
        output = Path(output_parent.name) / "bundle"

        def fake_download(url: str, destination: Path, attempts: int = 4) -> None:
            del attempts
            destination.write_bytes(url.encode())

        with mock.patch("scripts.archive_third_party_sources.download", side_effect=fake_download):
            build_source_bundle(lock_path, output)

        self.assertTrue((output / "SHA256SUMS").is_file())
        self.assertTrue((output / "forgejo-16.0.1-source.tar.gz").is_file())
        self.assertFalse((output / "openviking-v0.4.12-source.tar.gz").exists())


if __name__ == "__main__":
    unittest.main()
