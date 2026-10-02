#!/usr/bin/env python3
"""Safety and packaging checks for the fresh-install boundary."""
import importlib.util
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).parent / filename)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


install = load("install", "install.py")
packager = load("packager", "package-installation.py")
ROOT = Path(__file__).resolve().parent.parent


class InstallationTests(unittest.TestCase):
    def test_existing_data_or_symlinks_are_never_changed(self):
        for name in (".env", "data", "music"):
            with tempfile.TemporaryDirectory() as tmp:
                directory = Path(tmp)
                target = directory / "preserve"
                target.write_text("private sentinel")
                (directory / name).symlink_to(target)
                with patch.object(install, "run") as run:
                    with self.assertRaises(RuntimeError):
                        install.prepare(directory, "docker", "1.0.0", 8080)
                    run.assert_not_called()
                self.assertEqual(target.read_text(), "private sentinel")

    def test_engine_mapping_private_environment_and_guard(self):
        for engine in ("docker", "podman"):
            with tempfile.TemporaryDirectory() as tmp:
                directory = Path(tmp)
                (directory / ".env.example").write_text((ROOT / ".env.example").read_text())
                with patch.object(install, "run", return_value="") as run:
                    args = install.prepare(directory, engine, "1.0.0", 8081)
                env = (directory / ".env").read_text()
                self.assertNotIn("change-me", env)
                self.assertIn("YTMDL_HOST_PORT=8081", env)
                self.assertEqual((directory / ".env").stat().st_mode & 0o777, 0o600)
                marker = (directory / "music/.ytmdl-storage-id").read_text().strip().split(":", 1)[1]
                self.assertIn("MUSICDL_STORAGE_GUARD_ID=" + marker, env)
                init = run.call_args_list[1].args[0]
                self.assertEqual("--userns" in init, engine == "podman")
                self.assertEqual("compose.ghcr.podman.yaml" in args, engine == "podman")
                self.assertNotIn("-R", init[-1])

    def test_archive_whitelist_excludes_private_files_and_is_repeatable(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "root"
            root.mkdir()
            for name in packager.FILES:
                target = root / name
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes((ROOT / name).read_bytes())
            (root / ".env").write_text("private sentinel")
            first, second = Path(tmp) / "one", Path(tmp) / "two"
            packager.package(root, first)
            packager.package(root, second)
            version = (root / ".release-version").read_text().strip()
            filename = "ytmdl-" + version + ".tar.gz"
            self.assertEqual((first / filename).read_bytes(), (second / filename).read_bytes())
            with tarfile.open(first / filename) as archive:
                self.assertEqual(set(archive.getnames()), {"ytmdl-" + version + "/" + name for name in packager.FILES})


class ReleaseNotesTests(unittest.TestCase):
    def test_stable_changelog_compares_previous_stable_not_the_release_candidate(self):
        with tempfile.TemporaryDirectory() as tmp:
            changelog = Path(tmp) / "CHANGELOG.md"
            changelog.write_text("""# Changelog

## 1.1.0
### Highlights
- New player and library functionality, preserving existing recordings on upgrade.
- **Database Schema:** Migration to schema 15 requires a verified backup.

## 1.1.0-rc.2
### Highlights
- Qualified release candidate with explicit development-channel update commands.
- **Database Schema:** Migration to schema 15 requires a verified backup.

## 1.1.0-rc.1
## 1.0.0
""")
            for version, previous in (("1.1.0", "1.0.0"), ("1.1.0-rc.2", "1.1.0-rc.1")):
                result = subprocess.run([str(ROOT / "scripts/generate-release-notes.sh"),
                                         "--version", version, "--changelog", str(changelog)],
                                        capture_output=True, text=True, check=True)
                self.assertIn("/compare/v" + previous + "...v" + version, result.stdout)
                self.assertIn("Migration to schema 15 requires a verified backup.", result.stdout)
                if "-rc." in version:
                    self.assertIn("update --channel development --target " + version, result.stdout)


if __name__ == "__main__":
    unittest.main()
