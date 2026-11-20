#!/usr/bin/env python3
"""Regenerate internal/web/static/app/globe-admin2/00-admin2.js (ADR 0090).

The Globe Radar offers a cascading Country -> Province -> Kabupaten/Kota picker for
Indonesia from an embedded dataset, so it works air-gapped (ADR 0013). Source:
cahyadsn/wilayah_boundaries, MIT licensed, boundaries per Kepmendagri
No. 300.2.2-2430 Tahun 2025. Pinned upstream commit:
2c0d8c791af7ca55ee06d17b8f34813e8e6bf1b1 (2025-07 SQL dumps).

Upstream stores one SQL file per level in db/prov/ and db/kab/. Each row is
(kode, nama, lat, lng, path) where path is a GeoJSON-like nesting of polygons,
rings and [latitude, longitude] points (two nesting variants occur; both are
accepted). The province code is the first two digits of the kabupaten code, so
parent/child links come straight from the data -- no hand-written name lists.

Rings are simplified with Douglas-Peucker (TOL degrees, longitude scaled by
cos(lat)), snapped to 1/Q degree, then delta-encoded as zigzag varints, 5 payload
bits per character (A-Z a-z 0-9 - _), bit 5 = "more follows". Rings of one
feature are joined with '.'. The browser fills them with the even-odd rule.

Standard library only. Usage:

    python3 tools/globe-countries/admin2.py <src-dir> <dst>
    python3 tools/globe-countries/admin2.py --fetch <dst>

<src-dir> must contain prov/ and kab/ subdirectories with the upstream *.sql
files. --fetch downloads the pinned commit into a temporary directory first.
"""
import json
import os
import re
import shutil
import sys
import tempfile
import urllib.request

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from gen import ALPHABET, douglas_peucker, varint  # noqa: E402

PIN = "2c0d8c791af7ca55ee06d17b8f34813e8e6bf1b1"
REPO = "cahyadsn/wilayah_boundaries"
Q = 10000       # coordinate units per degree (~11 m); sharp at the globe's max zoom
TOL = 0.0005    # Douglas-Peucker tolerance, degrees on the sphere (~55 m)
ROW = re.compile(
    r"\(\s*'([0-9.]+)'\s*,\s*'([^']*)'\s*,\s*(-?[0-9.eE+]+)\s*,\s*"
    r"(-?[0-9.eE+]+)\s*,\s*'(\[\[\[.*?\]\]\])'\s*\)", re.S,
)
UA = {"User-Agent": "pantau-geodata-generator"}


def fetch(dst_dir):
    """Download the pinned upstream SQL dumps into dst_dir/{prov,kab}."""
    for level in ("prov", "kab"):
        os.makedirs(os.path.join(dst_dir, level), exist_ok=True)
        listing = "https://api.github.com/repos/%s/contents/db/%s?ref=%s" % (REPO, level, PIN)
        req = urllib.request.Request(listing, headers=UA)
        items = json.load(urllib.request.urlopen(req, timeout=60))
        for item in items:
            if not item["name"].endswith(".sql"):
                continue
            req = urllib.request.Request(item["download_url"], headers=UA)
            data = urllib.request.urlopen(req, timeout=60).read()
            with open(os.path.join(dst_dir, level, item["name"]), "wb") as out:
                out.write(data)


def rings_of(path):
    """Normalise the two upstream nesting variants into a list of rings."""
    if not path:
        return []
    if isinstance(path[0][0][0], list):       # [polygon][ring][point][2]
        return [ring for poly in path for ring in poly]
    return path                               # [ring][point][2]


def encode(path):
    """Encode one feature's rings as '.'-separated delta varints (lon,lat)."""
    out = []
    for ring in rings_of(path):
        pts = [(lon, lat) for lat, lon in ring]  # upstream is [lat, lon]
        if len(pts) < 3:
            continue
        snapped = []
        for lon, lat in douglas_peucker(pts, TOL):
            pt = (round(lon * Q), round(lat * Q))
            if not snapped or snapped[-1] != pt:
                snapped.append(pt)
        if len(snapped) > 1 and snapped[0] == snapped[-1]:
            snapped.pop()
        if len(snapped) < 3:
            continue
        s, px, py = "", 0, 0
        for x, y in snapped:
            s += varint(x - px) + varint(y - py)
            px, py = x, y
        out.append(s)
    return ".".join(out)


def load(src_dir, level):
    rows = []
    folder = os.path.join(src_dir, level)
    for name in sorted(os.listdir(folder)):
        if not name.endswith(".sql"):
            continue
        with open(os.path.join(folder, name), encoding="utf-8") as f:
            text = f.read()
        for m in ROW.finditer(text):
            code, nama, lat, lng, path = m.groups()
            rows.append((code, nama, float(lat), float(lng), json.loads(path)))
    return rows


def main(src, dst):
    prov = load(src, "prov")
    kab = load(src, "kab")
    if len(prov) != 38 or len(kab) != 514:
        sys.exit("unexpected upstream coverage: %d provinces, %d kabupaten/kota" % (len(prov), len(kab)))
    codes = set(p[0] for p in prov)
    for code, *_ in kab:
        if code.split(".")[0] not in codes:
            sys.exit("kabupaten %s has no parent province" % code)

    def row(r, with_parent):
        code, nama, lat, lng, path = r
        enc = encode(path)
        if not enc:
            sys.exit("feature %s encoded to no rings" % code)
        fields = [code, nama, round(lat, 4), round(lng, 4)]
        if with_parent:
            fields.insert(1, code.split(".")[0])
        fields.append(enc)
        return json.dumps(fields, ensure_ascii=False, separators=(",", ":"))

    prov_rows = [row(r, False) for r in prov]
    kab_rows = [row(r, True) for r in kab]
    with open(dst, "w", encoding="utf-8") as out:
        out.write(
            "/* Embedded Indonesia province / kabupaten-kota boundaries for the Globe Radar\n"
            " * (ADR 0090). Generated by tools/globe-countries/admin2.py from\n"
            " * cahyadsn/wilayah_boundaries (MIT, Kepmendagri 300.2.2-2430/2025), pinned at\n"
            " * %s; do not edit by hand.\n"
            " * p = [code, name, lat, lon, rings]; k = [code, province code, name, lat, lon, rings].\n"
            " * Rings are '.'-separated delta zigzag varints of (lon,lat) in 1/q degree,\n"
            " * decoded by 02-globe-geo.js. Air-gapped: no CDN (ADR 0013). */\n"
            "window.GLOBE_ADMIN2 = { q: %d, p: [\n" % (PIN, Q)
        )
        for r in prov_rows:
            out.write(" %s,\n" % r)
        out.write("], k: [\n")
        for r in kab_rows:
            out.write(" %s,\n" % r)
        out.write("] };\n")
    print("%d provinces, %d kabupaten/kota, %d chars" % (
        len(prov), len(kab), sum(len(r) for r in prov_rows + kab_rows)), file=sys.stderr)


if __name__ == "__main__":
    if len(sys.argv) == 3 and sys.argv[1] != "--fetch":
        main(sys.argv[1], sys.argv[2])
    elif len(sys.argv) == 3 and sys.argv[1] == "--fetch":
        tmp = tempfile.mkdtemp(prefix="globe-admin2-")
        try:
            fetch(tmp)
            main(tmp, sys.argv[2])
        finally:
            shutil.rmtree(tmp)
    else:
        sys.exit(__doc__)
