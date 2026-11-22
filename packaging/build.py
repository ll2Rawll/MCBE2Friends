#!/usr/bin/env python3
"""Build a platform's native window or assemble the five native builds into downloads."""
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


def build(go, stage, system, arch, desktop=False):
    path = stage / (system + "-" + arch)
    flags = "-s -w" + (" -H windowsgui" if system == "windows" else "")
    env = dict(os.environ, CGO_ENABLED="1" if desktop and system != "windows" else "0", GOOS=system, GOARCH=arch)
    tags = ["-tags", "desktop,production" + (",webkit2_41" if system == "linux" else "")] if desktop else []
    subprocess.run(
        [go, "build", *tags, "-trimpath", "-buildvcs=false", "-ldflags", flags, "-o", str(path), "."],
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
        env = dict(os.environ, CGO_ENABLED="0" if system == "windows" else "1", GOOS=system, GOARCH=arch)
        result = subprocess.check_output([go, "list", "-tags", "desktop,production,webkit2_41", "-deps", "-f", template, "."],
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


def windows_release(native, out, notices):
    binary = native / "windows-amd64"
    shutil.copyfile(binary, out / (NAME + ".exe"))
    output = out / (NAME + "-Windows-x64.zip")
    with zipfile.ZipFile(output, "w") as archive:
        put_zip(archive, binary, NAME + ".exe", True)
        put_zip(archive, GUIDE, "README.md")
        for notice in notices:
            put_zip(archive, notice, "licenses/" + notice.name)
    print(output.name, flush=True)


def mac_release(native, stage, out, notices):
    slices = [native / ("darwin-" + arch) for arch in ("amd64", "arm64")]
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
        "CFBundleShortVersionString": "1.6.0",
        "CFBundleVersion": "7",
        "NSHighResolutionCapable": True,
        "NSAppTransportSecurity": {"NSAllowsLocalNetworking": True},
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


def linux_release(go, stage, native, out, arch, notices):
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
        add(native / ("linux-" + arch), "libexec/" + NAME + "-desktop", 0o755)
        add(GUIDE, "README.md", 0o644)
        for notice in notices:
            add(notice, "licenses/" + notice.name, 0o644)
    print(output.name, flush=True)


def source_release(out):
    output = out / (NAME + "-Source.zip")
    files = [*ROOT.glob("*.go"), ROOT / "go.mod", ROOT / "go.sum", ROOT / "README.md", ROOT / "resource_windows_amd64.syso"]
    for folder in (ROOT / "catalog", ROOT / "web", ROOT / "desktopwindow", ROOT / "docs", ROOT / "licensing", ROOT / "testdata", PACKAGE):
        files.extend(p for p in folder.rglob("*") if p.is_file() and "__pycache__" not in p.parts)
    with zipfile.ZipFile(output, "w") as archive:
        for file in sorted(files):
            put_zip(archive, file, NAME + "/" + file.relative_to(ROOT).as_posix())
    print(output.name, flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", default=shutil.which("go") or "go")
    parser.add_argument("--output", type=Path, default=ROOT / "dist")
    parser.add_argument("--native-output", type=Path, help="Build the desktop window for this runner's OS and architecture")
    parser.add_argument("--native-binaries", type=Path, help="Assemble the five builds from the Desktop windows workflow")
    args = parser.parse_args()
    if args.native_output:
        output = args.native_output.resolve()
        output.mkdir(parents=True, exist_ok=True)
        system, arch = subprocess.check_output([args.go, "env", "GOOS", "GOARCH"], text=True).split()
        build(args.go, output, system, arch, desktop=True)
        return
    if not args.native_binaries:
        parser.error("Use --native-output to build a desktop window or --native-binaries to package all platforms.")
    native = args.native_binaries.resolve()
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory() as temporary:
        stage = Path(temporary)
        notices = collect_notices(args.go, stage)
        windows_release(native, out, notices)
        mac_release(native, stage, out, notices)
        for arch in ("amd64", "arm64"):
            linux_release(args.go, stage, native, out, arch, notices)
        source_release(out)


if __name__ == "__main__":
    main()
