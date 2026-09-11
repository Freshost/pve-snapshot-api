import importlib.util
import pathlib
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("release_metadata", pathlib.Path(__file__).with_name("release-metadata.py"))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class ReleaseMetadataTests(unittest.TestCase):
    def check(self, debian, changelog):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            (root / "debian").mkdir()
            (root / "debian/changelog").write_text(debian)
            (root / "CHANGELOG.md").write_text(changelog)
            return module.metadata(root)

    def test_only_current_release_notes(self):
        result = self.check("pve-snapshot-api (0.2.0-1) stable; urgency=high",
                            "# Changelog\n## [0.2.0] - 2026-09-10\nNew notes\n## [0.1.0] - 2026-03-14\nOld notes")
        self.assertEqual(result, ("0.2.0", "0.2.0-1", "New notes"))

    def test_mismatch_is_rejected(self):
        with self.assertRaises(ValueError):
            self.check("pve-snapshot-api (0.2.0-1) stable; urgency=high",
                       "## [0.1.0] - 2026-09-10\nNotes")

    def test_development_version_is_not_a_release(self):
        with self.assertRaises(ValueError):
            self.check("pve-snapshot-api (0.2.0-1+dev12) stable; urgency=high", "")

    def test_missing_notes_are_rejected(self):
        with self.assertRaises(ValueError):
            self.check("pve-snapshot-api (0.2.0-1) stable; urgency=high",
                       "## [0.2.0] - 2026-09-10\n")
