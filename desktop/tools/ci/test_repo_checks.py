import contextlib
import importlib.util
import io
import pathlib
import shutil
import subprocess
import tempfile
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location("repo_checks", pathlib.Path(__file__).with_name("repo_checks.py"))
checks = importlib.util.module_from_spec(spec)
spec.loader.exec_module(checks)


@unittest.skipUnless(shutil.which("git"), "git is not installed")
class RepositoryInventoryTests(unittest.TestCase):
    def test_local_artifacts_are_ignored_but_tracked_violations_fail(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            subprocess.run(["git", "init", "-q", directory], check=True)
            (root / "source file.py").write_text("pass\n")
            subprocess.run(["git", "-C", directory, "add", "source file.py"], check=True)
            (root / "old-backup.zip").write_bytes(b"x" * 100)
            (root / "legacy.rs").write_text("local artifact")
            with mock.patch.object(checks, "REPO_ROOT", directory), mock.patch.object(checks, "TRACKED_FILES", checks.tracked_files(directory)), mock.patch.object(checks, "MAX_FILE_BYTES", 50), mock.patch.object(checks, "failures", []), contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(list(checks.walk(directory)), [str(root / "source file.py")])
                checks.check_file_sizes()
                checks.check_no_legacy_toolchains()
                self.assertEqual(checks.failures, [])
                subprocess.run(["git", "-C", directory, "add", "old-backup.zip", "legacy.rs"], check=True)
                checks.TRACKED_FILES = checks.tracked_files(directory)
                checks.check_file_sizes()
                checks.check_no_legacy_toolchains()
                self.assertTrue(any("old-backup.zip" in failure for failure in checks.failures))
                self.assertTrue(any("legacy.rs" in failure for failure in checks.failures))

    def test_source_archive_falls_back_to_filesystem(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            (root / "source.py").write_text("pass\n")
            (root / "target").mkdir()
            (root / "target" / "generated.rs").write_text("generated")
            self.assertIsNone(checks.tracked_files(directory))
            with mock.patch.object(checks, "TRACKED_FILES", None):
                self.assertEqual(list(checks.walk(directory)), [str(root / "source.py")])


if __name__ == "__main__":
    unittest.main()
