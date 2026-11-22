"""Rebuild the launcher's pinned server catalog from Mojang, Paper and Fabric."""
import hashlib
import json
import sys
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from urllib.parse import quote
from urllib.request import Request, urlopen

ROOT = Path(__file__).resolve().parents[1]
LATEST, OLDEST, LOADER = "26.3", "1.19", "0.20.1"
UA = "ll2Rawll/Mcbe2Friends (https://github.com/ll2Rawll/MCBE2Friends)"


def read(url):
    with urlopen(Request(url, headers={"User-Agent": UA}), timeout=100) as res:
        return res.read()


def api(url):
    return json.loads(read(url))


def release(entry):
    version = entry["id"]
    raw = read(entry["url"])
    assert hashlib.sha1(raw).hexdigest() == entry["sha1"]
    meta = json.loads(raw)
    upstream = meta["downloads"]["server"]
    jar = read(upstream["url"])
    assert len(jar) == upstream["size"] and hashlib.sha1(jar).hexdigest() == upstream["sha1"]
    record = {"id": version, "java": meta["javaVersion"]["majorVersion"],
              "vanilla": {"name": "Minecraft Java " + version, "url": upstream["url"],
                          "size": len(jar), "sha256": hashlib.sha256(jar).hexdigest()}}
    if version in paper_versions:
        builds = api("https://fill.papermc.io/v3/projects/paper/versions/" + version + "/builds")
        if version == "26.3":
            builds = [b for b in builds if b["id"] == 257]
        build = next((b for b in builds if b["channel"] == "STABLE"), builds[0])
        asset = build["downloads"]["server:default"]
        record.update(paper={"name": "Paper " + version + " build " + str(build["id"]), "url": asset["url"],
                             "size": asset["size"], "sha256": asset["checksums"]["sha256"]}, paperChannel=build["channel"])
    profile = api("https://meta.fabricmc.net/v2/versions/loader/" + version + "/" + LOADER + "/server/json")
    assert profile["mainClass"] == "net.fabricmc.loader.impl.launch.knot.KnotServer"
    record["fabric"] = [x["name"] for x in profile["libraries"]]
    print(version, file=sys.stderr, flush=True)
    return record


def library(coord):
    group, name, version = coord.split(":")
    path = group.replace(".", "/") + "/" + name + "/" + version + "/" + name + "-" + version + ".jar"
    url = "https://maven.fabricmc.net/" + quote(path, safe="/+")
    data = read(url)
    checksum = hashlib.sha256(data).hexdigest()
    # All Maven artifacts publish at least a SHA-1 sidecar.
    assert hashlib.sha1(data).hexdigest() == read(url + ".sha1").decode().strip().split()[0]
    return coord, {"name": name + " " + version, "size": len(data), "sha256": checksum, "url": url}


if __name__ == "__main__":
    manifest = api("https://piston-meta.mojang.com/mc/game/version_manifest_v2.json")
    versions = [v for v in manifest["versions"] if v["type"] == "release"]
    ids = [v["id"] for v in versions]
    supported = versions[ids.index(LATEST):ids.index(OLDEST) + 1]
    paper_versions = {v for series in api("https://fill.papermc.io/v3/projects/paper")["versions"].values() for v in series}
    with ThreadPoolExecutor(max_workers=5) as pool:
        records = list(pool.map(release, supported))
        coords = sorted({coord for record in records for coord in record["fabric"]})
        libraries = dict(pool.map(library, coords))
    out = ROOT / "catalog" / "java.json"
    out.parent.mkdir(exist_ok=True)
    out.write_text(json.dumps({"loader": LOADER, "releases": records, "libraries": libraries}, indent=2) + "\n")
