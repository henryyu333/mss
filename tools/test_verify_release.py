"""Run with: python -m unittest discover -s tools -p 'test_verify_release.py'.

These unit fixtures do not claim to execute a native MSS binary; run the release
validator on real locally built or GoReleaser archives for that separate gate.
"""

import hashlib
import importlib.util
import io
from pathlib import Path
import struct
import tarfile
import tempfile
import unittest
import zipfile


spec = importlib.util.spec_from_file_location("verify_release", Path(__file__).with_name("verify-release.py"))
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


class ReleaseValidationTests(unittest.TestCase):
    def test_version_requires_explicit_semver_tag(self):
        self.assertEqual(release.release_version("v0.3.0"), "0.3.0")
        self.assertEqual(release.release_version("v0.3.0-rc.1"), "0.3.0-rc.1")
        for tag in ("0.3.0", "vlatest", "v0.3.0/evil", "v0.3"):
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                release.release_version(tag)

    def test_checksum_set_and_content(self):
        with tempfile.TemporaryDirectory() as temporary:
            dist = Path(temporary)
            (dist / "archive.zip").write_bytes(b"archive")
            digest = hashlib.sha256(b"archive").hexdigest()
            valid = f"{digest}  archive.zip\n"
            (dist / "checksums.txt").write_text(valid)
            release.read_checksums(dist, {"archive.zip"})
            for text, expected in ((valid, {"archive.zip", "missing.zip"}),
                                   (valid, set()), (valid + valid, {"archive.zip"}),
                                   (f"{digest}  ../archive.zip\n", {"archive.zip"}),
                                   ("bad checksum\n", {"archive.zip"})):
                (dist / "checksums.txt").write_text(text)
                with self.subTest(text=text, expected=expected), self.assertRaises(ValueError):
                    release.read_checksums(dist, expected)
            (dist / "checksums.txt").write_text(valid)
            (dist / "archive.zip").write_bytes(b"changed")
            with self.assertRaisesRegex(ValueError, "SHA256 mismatch"):
                release.read_checksums(dist, {"archive.zip"})

    def test_safe_paths(self):
        self.assertEqual(release.safe_name("skills/mss/agents/openai.yaml"),
                         "skills/mss/agents/openai.yaml")
        for name in ("../mss", "/mss", "skills/../mss", "./mss", "C:/mss",
                     "skills\\mss", "skills//mss", ""):
            with self.subTest(name=name), self.assertRaises(ValueError):
                release.safe_name(name)

    def test_tar_rejects_links_traversal_and_duplicate_files(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "archive.tar.gz"
            for kind in ("symlink", "hardlink", "traversal", "duplicate"):
                with tarfile.open(path, "w:gz") as archive:
                    member = tarfile.TarInfo("../mss" if kind == "traversal" else "mss")
                    if kind in ("symlink", "hardlink"):
                        member.type = tarfile.SYMTYPE if kind == "symlink" else tarfile.LNKTYPE
                        member.linkname = "/outside"
                        archive.addfile(member)
                    else:
                        member.size = 1
                        archive.addfile(member, io.BytesIO(b"x"))
                        if kind == "duplicate":
                            archive.addfile(member, io.BytesIO(b"y"))
                with self.subTest(kind=kind), self.assertRaises(ValueError):
                    release.archive_files(path)

    def test_zip_rejects_symlinks_and_unexpected_directories(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "archive.zip"
            with zipfile.ZipFile(path, "w") as archive:
                member = zipfile.ZipInfo("mss")
                member.create_system = 3
                member.external_attr = 0o120777 << 16
                archive.writestr(member, "/outside")
            with self.assertRaisesRegex(ValueError, "non-regular ZIP"):
                release.archive_files(path)
            with zipfile.ZipFile(path, "w") as archive:
                archive.writestr("mss.exe", b"binary")
                archive.writestr("unexpected/", b"")
            with self.assertRaisesRegex(ValueError, "unexpected archive directories"):
                release.archive_files(path)

    def test_archive_keeps_asset_paths(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "archive.tar.gz"
            with tarfile.open(path, "w:gz") as archive:
                for name in ("skills", "skills/mss", "skills/mss/agents"):
                    member = tarfile.TarInfo(name)
                    member.type = tarfile.DIRTYPE
                    archive.addfile(member)
                member = tarfile.TarInfo("skills/mss/agents/openai.yaml")
                member.size = 6
                archive.addfile(member, io.BytesIO(b"policy"))
            self.assertEqual(release.archive_files(path), {"skills/mss/agents/openai.yaml": b"policy"})

    def test_skill_version_and_invocation_policy(self):
        text = b'---\nname: mss\ndisable-model-invocation: true\nmetadata:\n  mss-version: "0.3.0"\n---\n'
        names = ("skills/mss/SKILL.md", "skills/mss/SKILL.zh-CN.md")
        assets = dict.fromkeys(names, text)
        policy_name = "skills/mss/agents/openai.yaml"
        assets[policy_name] = b"policy:\n  allow_implicit_invocation: false\n"
        release.check_skill_assets(assets, "0.3.0")
        with self.assertRaisesRegex(ValueError, "wrong skill version"):
            release.check_skill_assets(assets, "0.2.0")
        for name in names:
            changed = dict(assets)
            changed[name] = text.replace(b"invocation: true", b"invocation: false")
            with self.assertRaisesRegex(ValueError, "explicit-invocation"):
                release.check_skill_assets(changed, "0.3.0")
        assets[policy_name] = b"policy:\n  allow_implicit_invocation: true\n"
        with self.assertRaisesRegex(ValueError, "Codex companion policy"):
            release.check_skill_assets(assets, "0.3.0")

    def test_binary_headers_match_all_six_targets(self):
        for system, arch in release.TARGETS:
            data = bytearray(256)
            if system == "linux":
                data[:6] = b"\x7fELF\x02\x01"
                struct.pack_into("<H", data, 18, 62 if arch == "amd64" else 183)
            elif system == "darwin":
                data[:4] = b"\xcf\xfa\xed\xfe"
                struct.pack_into("<I", data, 4, 0x01000007 if arch == "amd64" else 0x0100000C)
            else:
                data[:2] = b"MZ"
                struct.pack_into("<I", data, 60, 128)
                data[128:132] = b"PE\x00\x00"
                struct.pack_into("<H", data, 132, 0x8664 if arch == "amd64" else 0xAA64)
            with self.subTest(system=system, arch=arch):
                release.check_binary(data, system, arch)
                other = "arm64" if arch == "amd64" else "amd64"
                with self.assertRaisesRegex(ValueError, "wrong binary architecture"):
                    release.check_binary(data, system, other)



if __name__ == "__main__":
    unittest.main()
