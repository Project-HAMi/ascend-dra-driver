# Copyright 2026 The HAMi Authors.
# SPDX-License-Identifier: Apache-2.0
"""Unit tests for versioned asset installation; no cluster or NPU required."""

import hashlib
import pathlib
import subprocess
import tempfile
import unittest


SCRIPT = pathlib.Path(__file__).with_name("install-core-assets.sh")
REVISION = "98ec21f47d6c42c9ef092e7e7e55f10e7f838072"


class InstallCoreAssetsTest(unittest.TestCase):
    def test_install_is_idempotent_and_never_overwrites_a_mapped_version(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            assets, host = root / "assets", root / "host"
            assets.mkdir()
            host.mkdir()
            legacy = host / "libvnpu.so"
            legacy.write_bytes(b"old running library")
            for name, content in {
                "libvnpu.so": "new library",
                "core-revision": REVISION + "\n",
                "core-layout": "per-device-v1\n",
                "ld.so.preload": "/hami-vnpu-core/libvnpu.so\n",
            }.items():
                (assets / name).write_text(content)

            def checksum():
                digest = hashlib.sha256((assets / "libvnpu.so").read_bytes()).hexdigest()
                (assets / "libvnpu.sha256").write_text(digest + "  libvnpu.so\n")

            def install():
                return subprocess.run(
                    ["sh", str(SCRIPT), str(assets), str(host)],
                    capture_output=True, text=True, check=False,
                )

            checksum()
            result = install()
            self.assertEqual(result.returncode, 0, result.stderr)
            target = host / "releases" / REVISION / "libvnpu.so"
            inode = target.stat().st_ino
            self.assertEqual(install().returncode, 0)
            self.assertEqual(target.stat().st_ino, inode)
            (assets / "libvnpu.so").write_bytes(b"different build")
            checksum()
            self.assertNotEqual(install().returncode, 0)
            self.assertEqual(target.read_bytes(), b"new library")
            self.assertEqual(legacy.read_bytes(), b"old running library")
            self.assertEqual(list((host / "releases").glob(".install.*")), [])

    def test_invalid_revision_does_not_create_a_release(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            assets = root / "assets"
            assets.mkdir()
            (assets / "core-revision").write_text("../../outside")
            (assets / "core-layout").write_text("per-device-v1")
            result = subprocess.run(["sh", str(SCRIPT), str(assets), str(root / "host")], capture_output=True, check=False)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse((root / "host").exists())


if __name__ == "__main__":
    unittest.main()
