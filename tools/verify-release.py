#!/usr/bin/env python3
"""Prepare/verify MSS release bundles using only Python's standard library.

prepare preserves and checks GoReleaser's six archive checksums, adds the exact
`git archive` source and standalone Homebrew formula, and checksums both.
verify never downloads files; it executes only this host's extracted binary in
an isolated temporary HOME. SHA256 integrity is not a publisher signature.
"""

import argparse
import hashlib
import os
from pathlib import Path, PurePosixPath
import platform
import re
import stat
import struct
import subprocess
import sys
import tarfile
import tempfile
import zipfile


ASSETS = (
    "LICENSE", "README.md", "README.zh-CN.md", "docs/install.md",
    "skills/mss/SKILL.md", "skills/mss/SKILL.zh-CN.md",
    "skills/mss/agents/openai.yaml",
)
TARGETS = tuple((system, arch) for system in ("darwin", "linux", "windows")
                for arch in ("amd64", "arm64"))


def require(condition, message):
    if not condition:
        raise ValueError(message)


def release_version(tag):
    require(re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?", tag),
            "tag must be vMAJOR.MINOR.PATCH (optionally with a prerelease suffix)")
    return tag[1:]


def archive_name(version, system, arch):
    suffix = "zip" if system == "windows" else "tar.gz"
    return f"mss_{version}_{system}_{arch}.{suffix}"


def source_name(version):
    return f"mss_{version}_source.tar.gz"


def sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def read_checksums(dist, expected):
    checksums = {}
    for line in (dist / "checksums.txt").read_text(encoding="utf-8").splitlines():
        match = re.fullmatch(r"([0-9a-fA-F]{64})  ([^/\\]+)", line)
        require(match is not None, f"invalid checksum line: {line!r}")
        digest, name = match.groups()
        require(name not in checksums, f"duplicate checksum: {name}")
        checksums[name] = digest.lower()
    require(set(checksums) == expected,
            f"checksum artifact set differs: missing={sorted(expected - set(checksums))}, "
            f"unexpected={sorted(set(checksums) - expected)}")
    for name, digest in checksums.items():
        path = dist / name
        require(path.is_file() and not path.is_symlink(), f"missing/unsafe artifact: {name}")
        require(sha256(path) == digest, f"SHA256 mismatch: {name}")


def safe_name(name):
    path = PurePosixPath(name)
    require(name and "\\" not in name and ":" not in name and not path.is_absolute()
            and all(part not in ("", ".", "..") for part in name.rstrip("/").split("/")),
            f"unsafe archive path: {name!r}")
    return str(path)


def archive_files(path):
    """Read regular files only; never extract archive-supplied paths or links."""
    files, directories, seen = {}, set(), set()
    if path.suffix == ".zip":
        with zipfile.ZipFile(path) as archive:
            for member in archive.infolist():
                name = safe_name(member.filename)
                require(name not in seen, f"duplicate archive path: {name}")
                seen.add(name)
                mode = member.external_attr >> 16
                require(stat.S_IFMT(mode) in (0, stat.S_IFREG, stat.S_IFDIR),
                        f"non-regular ZIP member: {name}")
                if member.is_dir():
                    directories.add(name)
                else:
                    files[name] = archive.read(member)
    else:
        with tarfile.open(path, "r:gz") as archive:
            for member in archive:
                name = safe_name(member.name)
                require(name not in seen, f"duplicate archive path: {name}")
                seen.add(name)
                require(member.isfile() or member.isdir(), f"non-regular tar member: {name}")
                if member.isdir():
                    directories.add(name)
                else:
                    with archive.extractfile(member) as stream:
                        files[name] = stream.read()
    parents = {str(parent) for name in files for parent in PurePosixPath(name).parents
               if str(parent) != "."}
    require(directories <= parents, f"unexpected archive directories: {sorted(directories - parents)}")
    return files


def check_skill_assets(files, version):
    # This repository's deliberately small YAML schema needs no YAML dependency.
    for name in ("skills/mss/SKILL.md", "skills/mss/SKILL.zh-CN.md"):
        text = files[name].decode("utf-8")
        require(text.startswith("---\n") and "\n---\n" in text[4:], f"missing frontmatter: {name}")
        front = text.split("---\n", 2)[1]
        require(re.search(r"^name: mss$", front, re.M), f"wrong skill name: {name}")
        require(re.search(r"^disable-model-invocation: true$", front, re.M),
                f"skill is not explicit-invocation only: {name}")
        require(re.search(r'^  mss-version: "' + re.escape(version) + r'"$', front, re.M),
                f"wrong skill version: {name} (expected {version})")
    policy = files["skills/mss/agents/openai.yaml"].decode("utf-8")
    require(re.fullmatch(r"policy:\n  allow_implicit_invocation: false\n?", policy),
            "Codex companion policy must prohibit implicit invocation")


def check_source(dist, root, version):
    files = archive_files(dist / source_name(version))
    prefix = f"mss-{version}/"
    require(all(name.startswith(prefix) for name in files), "source archive has wrong prefix")
    relative = {name[len(prefix):]: data for name, data in files.items()}
    for name in (*ASSETS, "go.mod", "cmd/mss/main.go", "skills/bundle.go"):
        require(name in relative, f"source archive missing: {name}")
    for name, data in relative.items():
        path = root / name
        require(path.is_file() and not path.is_symlink(), f"source member missing in checkout: {name}")
        require(path.read_bytes() == data, f"source differs from checkout: {name}")
    check_skill_assets(relative, version)


def formula(version, source_hash):
    return f'''# Generated from the exact tagged source; publish to the external tap separately.
class Mss < Formula
  desc "Recall and summarize past AI coding sessions, no memory system needed"
  homepage "https://github.com/henryyu333/mss"
  url "https://github.com/henryyu333/mss/releases/download/v{version}/{source_name(version)}"
  version "{version}"
  sha256 "{source_hash}"
  license "MIT"

  depends_on "go" => :build

  def install
    ENV["CGO_ENABLED"] = "0"
    system "go", "build", *std_go_args(ldflags: "-s -w -X main.version={version}"), "./cmd/mss"
    pkgshare.install "skills/mss"
  end

  def caveats
    <<~EOS
      Install the skill explicitly: mss install-skill <claude|codex|pi|omp>
      sqlite3 and zstd are optional runtime tools for SQLite/compressed stores.
      No hooks, agent settings, or existing skills are changed automatically.
    EOS
  end

  test do
    assert_equal "mss {version}", shell_output("#{{bin}}/mss version").strip
    ENV["HOME"] = testpath.to_s
    system bin/"mss", "install-skill", "codex"
    assert_equal (pkgshare/"mss/SKILL.md").read, (testpath/".agents/skills/mss/SKILL.md").read
    assert_equal (pkgshare/"mss/agents/openai.yaml").read,
                 (testpath/".agents/skills/mss/agents/openai.yaml").read
  end
end
'''


def check_binary(data, system, arch):
    require(len(data) >= 256, f"truncated binary: {system}/{arch}")
    if system == "linux":
        require(data[:6] == b"\x7fELF\x02\x01", "expected little-endian ELF64")
        machine = struct.unpack_from("<H", data, 18)[0]
        expected = {"amd64": 62, "arm64": 183}[arch]
    elif system == "darwin":
        require(data[:4] == b"\xcf\xfa\xed\xfe", "expected little-endian Mach-O64")
        machine = struct.unpack_from("<I", data, 4)[0]
        expected = {"amd64": 0x01000007, "arm64": 0x0100000C}[arch]
    else:
        require(data[:2] == b"MZ", "expected Windows executable")
        offset = struct.unpack_from("<I", data, 60)[0]
        require(offset + 6 <= len(data) and data[offset:offset + 4] == b"PE\x00\x00", "invalid PE header")
        machine = struct.unpack_from("<H", data, offset + 4)[0]
        expected = {"amd64": 0x8664, "arm64": 0xAA64}[arch]
    require(machine == expected, f"wrong binary architecture: {system}/{arch}")


def host_target():
    system = {"Darwin": "darwin", "Linux": "linux", "Windows": "windows"}.get(platform.system())
    arch = {"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}.get(platform.machine().lower())
    require((system, arch) in TARGETS, "no supported native host target")
    return system, arch


def check_native(data, assets, version, target):
    with tempfile.TemporaryDirectory(prefix="mss-release-") as temporary:
        base = Path(temporary)
        binary = base / ("mss.exe" if target[0] == "windows" else "mss")
        binary.write_bytes(data)
        binary.chmod(0o700)
        environment = {key: value for key, value in os.environ.items() if not key.startswith("MSS_")}
        environment.update(HOME=str(base), USERPROFILE=str(base), XDG_CONFIG_HOME=str(base / "config"))

        def run(arguments, env):
            result = subprocess.run([str(binary), *arguments], env=env, cwd=base,
                                    capture_output=True, text=True, timeout=30, check=True)
            return result.stdout.strip()

        require(run(["version"], environment) == f"mss {version}", "native binary version mismatch")
        destinations = {"claude": ".claude/skills/mss", "codex": ".agents/skills/mss",
                        "pi": ".pi/agent/skills/mss", "omp": ".omp/agent/skills/mss"}
        for harness, destination in destinations.items():
            for language, asset in (("en", "SKILL.md"), ("zh-CN", "SKILL.zh-CN.md")):
                home = base / f"{harness}-{language}"
                home.mkdir()
                env = dict(environment, HOME=str(home), USERPROFILE=str(home),
                           XDG_CONFIG_HOME=str(home / "config"))
                arguments = ["install-skill", harness, "--language", language]
                run(arguments, env)
                run(arguments, env)  # Identical reinstall must remain safe.
                expected = {f"{destination}/SKILL.md": assets[f"skills/mss/{asset}"]}
                if harness == "codex":
                    expected[f"{destination}/agents/openai.yaml"] = assets["skills/mss/agents/openai.yaml"]
                actual = {path.relative_to(home).as_posix(): path.read_bytes()
                          for path in home.rglob("*") if path.is_file()}
                require(actual == expected, f"installed embedded assets differ: {harness}/{language}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("prepare", "verify"))
    parser.add_argument("--dist", type=Path, required=True)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    args = parser.parse_args()
    version = release_version(args.tag)
    names = {archive_name(version, *target) for target in TARGETS}
    dist, root = args.dist.resolve(), args.root.resolve()
    if args.command == "prepare":
        read_checksums(dist, names)
        check_source(dist, root, version)
        (dist / "mss.rb").write_text(formula(version, sha256(dist / source_name(version))), encoding="utf-8")
        names.update((source_name(version), "mss.rb"))
        (dist / "checksums.txt").write_text("".join(f"{sha256(dist / name)}  {name}\n" for name in sorted(names)), encoding="utf-8")
        print("Prepared source/formula checksums; native archive validation still required")
        return
    names.update((source_name(version), "mss.rb"))
    read_checksums(dist, names)
    actual = {path.name for path in dist.iterdir()
              if path.name.endswith((".tar.gz", ".zip", ".rb"))}
    require(actual == names, f"unexpected/missing release artifacts: {sorted(actual ^ names)}")
    check_source(dist, root, version)
    require((dist / "mss.rb").read_text(encoding="utf-8") == formula(version, sha256(dist / source_name(version))),
            "Homebrew formula differs from exact-source template")
    native = host_target()
    for target in TARGETS:
        files = archive_files(dist / archive_name(version, *target))
        binary = "mss.exe" if target[0] == "windows" else "mss"
        require(set(files) == {*ASSETS, binary}, f"archive member set differs: {target}")
        for name in ASSETS:
            require(files[name] == (root / name).read_bytes(), f"archive asset differs from source: {target}/{name}")
        check_skill_assets(files, version)
        check_binary(files[binary], *target)
        if target == native:
            check_native(files[binary], files, version, target)
    print(f"PASS: eight artifacts, SHA256, safe paths, source/assets/metadata, six binary headers; "
          f"native {native[0]}/{native[1]} version and all embedded skill installs")
    print("Other target binaries were inspected, not executed; checksums are not signatures")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, UnicodeError, tarfile.TarError, zipfile.BadZipFile,
            subprocess.SubprocessError) as error:
        print(f"FAIL: {error}", file=sys.stderr)
        sys.exit(1)
