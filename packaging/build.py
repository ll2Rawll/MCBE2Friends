#!/usr/bin/env python3
"""Build the portable Mcbe2Friends downloads with Go and Python."""
import argparse
from collections import defaultdict
import os
from pathlib import Path
import plistlib
import shutil
import stat
import struct
import subprocess
import sys
import tarfile
import tempfile
import zipfile


ROOT = Path(__file__).resolve().parent.parent
PACKAGE = ROOT / "packaging"
GUIDE = ROOT / "docs" / "guide.md"
NAME = "Mcbe2Friends"


def build(go, stage, system, arch):
    path = stage / (system + "-" + arch)
    flags = "-s -w" + (" -H windowsgui" if system == "windows" else "")
    env = dict(os.environ, CGO_ENABLED="0", GOOS=system, GOARCH=arch)
    subprocess.run(
        [go, "build", "-trimpath", "-buildvcs=false", "-ldflags", flags, "-o", str(path), "."],
        cwd=ROOT, env=env, check=True,
    )
    return path


def mac_universal(binaries, output):
    """Combine the two original signed-by-the-linker Mach-O slices."""
    align = 14
    alignment = 1 << align
    entries = []
    data = bytearray(8 + 20 * len(binaries))
    for binary in binaries:
        image = binary.read_bytes()
        magic, cpu, subtype = struct.unpack_from("<III", image)
        if magic != 0xFEEDFACF:
            raise ValueError("Expected a little-endian 64-bit Mach-O executable")
        offset = (len(data) + alignment - 1) & -alignment
        data.extend(bytes(offset - len(data)))
        entries.append((cpu, subtype, offset, len(image), align))
        data.extend(image)
    struct.pack_into(">II", data, 0, 0xCAFEBABE, len(entries))
    for index, entry in enumerate(entries):
        struct.pack_into(">IIIII", data, 8 + index * 20, *entry)
    output.write_bytes(data)
    output.chmod(0o755)


def put_zip(archive, source, name, executable=False):
    info = zipfile.ZipInfo(name, (2026, 1, 1, 0, 0, 0))
    info.create_system = 3
    info.external_attr = (stat.S_IFREG | (0o755 if executable else 0o644)) << 16
    info.compress_type = zipfile.ZIP_DEFLATED
    archive.writestr(info, source.read_bytes())


def collect_notices(go, stage):
    """Collect each linked module's original license and attribution once."""
    modules = set()
    template = "{{with .Module}}{{if not .Main}}{{.Path}}\t{{.Version}}\t{{.Dir}}{{end}}{{end}}"
    targets = (("windows", "amd64"), ("darwin", "amd64"), ("darwin", "arm64"),
               ("linux", "amd64"), ("linux", "arm64"))
    for system, arch in targets:
        env = dict(os.environ, CGO_ENABLED="0", GOOS=system, GOARCH=arch)
        result = subprocess.check_output([go, "list", "-deps", "-f", template, "."],
                                         cwd=ROOT, env=env, text=True)
        modules.update(line for line in result.splitlines() if line)
    texts = defaultdict(list)
    for module in sorted(modules):
        name, version, directory = module.split("\t")
        files = sorted(p for p in Path(directory).iterdir()
                       if p.is_file() and p.name.upper().startswith(("LICENSE", "LICENCE", "NOTICE", "COPYING")))
        if not files:
            raise ValueError("Missing license for " + name)
        for file in files:
            texts[file.read_text(encoding="utf-8").strip()].append(f"{name} {version} ({file.name})")
    output = stage / "Go-dependencies.txt"
    output.write_text("\n\n".join("\n".join(names) + "\n\n" + body for body, names in texts.items()) + "\n",
                      encoding="utf-8")
    return [*sorted((PACKAGE / "licenses").glob("*.txt")), output]


def windows_release(go, stage, out, notices):
    binary = build(go, stage, "windows", "amd64")
    output = out / (NAME + "-Windows-x64.zip")
    with zipfile.ZipFile(output, "w") as archive:
        put_zip(archive, binary, NAME + ".exe", True)
        put_zip(archive, GUIDE, "README.md")
        for notice in notices:
            put_zip(archive, notice, "licenses/" + notice.name)
    print(output.name, flush=True)


def mac_release(go, stage, out, notices):
    slices = [build(go, stage, "darwin", arch) for arch in ("amd64", "arm64")]
    binary = stage / "mac-universal"
    mac_universal(slices, binary)
    plist = stage / "Info.plist"
    plist.write_bytes(plistlib.dumps({
        "CFBundleName": NAME,
        "CFBundleDisplayName": NAME,
        "CFBundleExecutable": NAME,
        "CFBundleIdentifier": "app.mcbe2friends.launcher",
        "CFBundleIconFile": NAME + ".icns",
        "CFBundlePackageType": "APPL",
        "CFBundleInfoDictionaryVersion": "6.0",
        "CFBundleShortVersionString": "1.2.0",
        "CFBundleVersion": "3",
        "LSUIElement": True,
    }))
    output = out / (NAME + "-macOS.zip")
    contents = NAME + ".app/Contents/"
    with zipfile.ZipFile(output, "w") as archive:
        put_zip(archive, binary, contents + "MacOS/" + NAME, True)
        put_zip(archive, plist, contents + "Info.plist")
        put_zip(archive, PACKAGE / (NAME + ".icns"), contents + "Resources/" + NAME + ".icns")
        put_zip(archive, GUIDE, "README.md")
        for notice in notices:
            put_zip(archive, notice, contents + "Resources/licenses/" + notice.name)
    print(output.name, flush=True)


def linux_release(go, stage, out, arch, notices):
    binary = build(go, stage, "linux", arch)
    label = "x64" if arch == "amd64" else "ARM64"
    output = out / (NAME + "-Linux-" + label + ".tar.gz")
    with tarfile.open(output, "w:gz") as archive:
        def add(source, name, mode):
            info = archive.gettarinfo(str(source), NAME + "/" + name)
            info.mode = mode
            info.uid = info.gid = 0
            info.uname = info.gname = ""
            with source.open("rb") as file:
                archive.addfile(info, file)
        add(binary, NAME, 0o755)
        add(GUIDE, "README.md", 0o644)
        for notice in notices:
            add(notice, "licenses/" + notice.name, 0o644)
    print(output.name, flush=True)


def source_release(out):
    output = out / (NAME + "-Source.zip")
    files = [*ROOT.glob("*.go"), ROOT / "go.mod", ROOT / "go.sum", ROOT / "README.md", ROOT / "resource_windows_amd64.syso"]
    for folder in (ROOT / "web", ROOT / "docs", ROOT / "licensing", ROOT / "testdata", PACKAGE):
        files.extend(p for p in folder.rglob("*") if p.is_file() and "__pycache__" not in p.parts)
    with zipfile.ZipFile(output, "w") as archive:
        for file in sorted(files):
            put_zip(archive, file, NAME + "/" + file.relative_to(ROOT).as_posix())
    print(output.name, flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", default=shutil.which("go") or "go")
    parser.add_argument("--output", type=Path, default=ROOT / "dist")
    args = parser.parse_args()
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory() as temporary:
        stage = Path(temporary)
        notices = collect_notices(args.go, stage)
        windows_release(args.go, stage, out, notices)
        mac_release(args.go, stage, out, notices)
        for arch in ("amd64", "arm64"):
            linux_release(args.go, stage, out, arch, notices)
        source_release(out)


if __name__ == "__main__":
    main()
