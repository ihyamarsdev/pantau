#!/usr/bin/env python3
"""Regenerate the lazy per-province high-resolution Indonesian admin-2 chunks (ADR 0090).

Source: geoBoundaries Open IDN ADM2 (BPS / WFP / OCHA ROAP), CC BY 3.0 IGO,
pinned release commit 9469f09, represented year 2020. The TopoJSON shares arcs
between neighbours, so simplifying each arc ONCE yields topology-consistent
boundaries: no cracks or overlaps between adjacent kabupaten/kota.

The 514 current Kepmendagri-2025 codes/names are kept. geoBoundaries has 519
2020 units; features are matched to the current list by normalised name plus a
verified alias table, and the few post-2020 splits that have no counterpart
fall back to the existing embedded polygon. Unmatched count is reported and
must be zero for the alias-covered set.

Output: one file per province in internal/web/static/app/globe-admin2-hires/,
each holding the simplified shared arcs used by that province's kabupaten plus
the feature->arc references, so only provinces in view are downloaded.

Standard library only. Usage:
    python3 tools/globe-countries/admin2_hires.py <topojson> <dst-dir> <current-js>
    python3 tools/globe-countries/admin2_hires.py --fetch <dst-dir> <current-js>
"""
import json
import math
import os
import re
import shutil
import sys
import tempfile
import urllib.request

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from gen import douglas_peucker, varint  # noqa: E402

PIN = "9469f09"
API = "https://www.geoboundaries.org/api/current/gbOpen/IDN/ADM2/"
UA = {"User-Agent": "pantau-geodata-generator"}
TOL = 0.0001     # Douglas-Peucker tolerance, degrees (~11 m); median segment ~118 m
Q = 10000        # coordinate units per degree (~11 m)
MIN_SEG = 1      # snapped vertices are deduplicated

# Verified spelling aliases: current Kepmendagri-2025 name -> geoBoundaries 2020 name.
ALIAS = {
    "labuhanbatu": "labuhan batu",
    "labuhanbatu selatan": "labuhan batu selatan",
    "labuhanbatu utara": "labuhan batu utara",
    "toba": "toba samosir",
    "pematangsiantar": "pematang siantar",
    "tanjungbalai": "tanjung balai",
    "sawahlunto": "sawah lunto",
    "batanghari": "batang hari",
    "banyuasin": "banyu asin",
    "lubuk linggau": "lubuklinggau",
    "tulang bawang": "tulangbawang",
    "gunungkidul": "gunung kidul",
    "karangasem": "karang asem",
    "palangkaraya": "palangka raya",
    "kotabaru": "kotawaringin timur",  # 2022 split; approximate parent
    "banjarbaru": "banjar baru",
    "mahakam ulu": "mahakam hulu",
    "kep siau tagulandang biaro": "siau tagulandang biaro",
    "bau bau": "baubau",
    "pasangkayu": "mamuju utara",
    "fak fak": "fakfak",
    # Tanimbar (post-2020 split) has no geoBoundaries counterpart -> embedded fallback.
}


def rings_of(g):
    """Return [ring,...] where each ring is a list of arc refs. Polygon and MultiPolygon both flatten
    to a ring list; every ring stays a separate closed loop (islands/holes preserved)."""
    a = g["arcs"]
    if g["type"] == "Polygon":
        return a
    if g["type"] == "MultiPolygon":
        return [ring for poly in a for ring in poly]
    return []


def norm(name):
    x = name.lower()
    for w in ("administrasi ", "kota ", "kabupaten ", "kab. ", "kota adm. "):
        x = x.replace(w, "")
    x = x.replace(".", " ").replace("-", " ")
    return " ".join(x.split())


def fetch(dst):
    meta = json.load(urllib.request.urlopen(urllib.request.Request(API, headers=UA), timeout=60))
    url = meta["tjDownloadURL"]
    path = os.path.join(dst, "idn-adm2.topojson")
    with open(path, "wb") as f:
        f.write(urllib.request.urlopen(urllib.request.Request(url, headers=UA), timeout=180).read())
    return path


def decode_arcs(topo):
    sx, sy = topo["transform"]["scale"]
    tx, ty = topo["transform"]["translate"]
    out = []
    for a in topo["arcs"]:
        x = y = 0
        pts = []
        for dx, dy in a:
            x += dx
            y += dy
            pts.append((x * sx + tx, y * sy + ty))
        out.append(pts)
    return out


def walk_arcs(x):
    if isinstance(x, int):
        yield x
    elif isinstance(x, list):
        for y in x:
            yield from walk_arcs(y)


def encode_arc(pts):
    """Simplify once, snap to 1/Q degree, delta-zigzag-varint encode."""
    sm = douglas_peucker(pts, TOL)
    prev = None
    enc = ""
    for lon, lat in sm:
        p = (round(lon * Q), round(lat * Q))
        if p == prev:
            continue
        enc += varint(p[0] - (prev[0] if prev else 0)) + varint(p[1] - (prev[1] if prev else 0))
        prev = p
    return enc


def current_features(js):
    """Return {code: (name, rings-string)} from the existing embedded bundle (fallback geometry)."""
    txt = open(js, encoding="utf-8").read()
    out = {}
    for m in re.finditer(r'^ \["([0-9.]+)","([0-9.]+)","([^"]+)",[^,]+,[^,]+,"([^"]+)"\]', txt, re.M):
        code, parent, name, rings = m.groups()
        out[code] = (name, rings)
    return out


def main(topo_path, dst_dir, current_js):
    topo = json.load(open(topo_path, encoding="utf-8"))
    arcs = decode_arcs(topo)
    geoms = topo["objects"]["IDNADM2gbOpen"]["geometries"]
    # Index by a strict key that KEEPS the kota/kabupaten distinction. geoBoundaries prefixes kota with
    # "Kota "; the current list prefixes with "Kota " or "Kabupaten ". Normalising both away made
    # "Kota Bandung" and "Kabupaten Bandung" collide onto the same feature (wrong geometry).
    by_name = {}
    kota_by_name = {}
    for g in geoms:
        raw = g["properties"]["shapeName"]
        bare = norm(raw)
        if raw.lower().startswith("kota "):
            kota_by_name[bare] = g
        else:
            by_name[bare] = g

    cur = current_features(current_js)
    provs = {}
    for m in re.finditer(r'^ \["([0-9.]+)","([^"]+)",[^,]+,[^,]+,"([^"]+)"\]',
                         open(current_js, encoding="utf-8").read(), re.M):
        code, name, rings = m.groups()
        provs[code] = (name, rings)

    # Map each current kabupaten to a geoBoundaries geometry (or None for fallback), preferring a
    # kota/kabupaten-type-aware key so identically-named kota and kabupaten get different features.
    feature_map = {}
    unmatched = []
    for code, (name, rings) in cur.items():
        key = norm(name)
        is_kota = name.startswith("Kota")
        g = kota_by_name.get(key) if is_kota else by_name.get(key)
        alias = ALIAS.get(key, "")
        if g is None and alias:
            g = kota_by_name.get(alias) if is_kota else by_name.get(alias)
            if g is None:
                g = by_name.get(alias) or kota_by_name.get(alias)
        if g is None and is_kota:
            g = by_name.get(key)  # last resort: a kota whose source name has no "Kota " prefix
        if g is None:
            unmatched.append((code, name))
        feature_map[code] = g

    # Assign each feature to a province by its centroid against province bboxes.
    def centroid(g):
        pts = []
        for ai in list(walk_arcs(g["arcs"]))[:30]:
            a = arcs[ai if ai >= 0 else ~ai]
            pts.extend(a[::max(1, len(a) // 40)])
        if not pts:
            return 0, 0
        return sum(p[0] for p in pts) / len(pts), sum(p[1] for p in pts) / len(pts)

    def province_bbox(rings_str, q=10000):
        b = [180, 90, -180, -90]
        alpha = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
        lut = {c: i for i, c in enumerate(alpha)}
        for s in rings_str.split("."):
            x = y = 0
            p = 0
            while p < len(s):
                v = sh = 0
                while True:
                    c = lut[s[p]]; p += 1; v |= (c & 31) << sh; sh += 5
                    if not c & 32:
                        break
                dx = (v >> 1) ^ -(v & 1)
                v = sh = 0
                while True:
                    c = lut[s[p]]; p += 1; v |= (c & 31) << sh; sh += 5
                    if not c & 32:
                        break
                dy = (v >> 1) ^ -(v & 1)
                x += dx; y += dy
                lon, lat = x / q, y / q
                b[0] = min(b[0], lon); b[2] = max(b[2], lon)
        return b

    pb = {code: province_bbox(rings) for code, (name, rings) in provs.items()}

    def which_prov(lon, lat):
        cands = [c for c, b in pb.items() if b[0] - 1 <= lon <= b[2] + 1 and b[1] - 1 <= lat <= b[3] + 1]
        if not cands:
            return None
        if len(cands) == 1:
            return cands[0]
        return min(cands, key=lambda c: (lon - (pb[c][0] + pb[c][2]) / 2) ** 2 +
                   (lat - (pb[c][1] + pb[c][3]) / 2) ** 2)

    chunk = {c: [] for c in provs}
    for code, g in feature_map.items():
        pcode = code.split(".")[0]
        if g is not None:
            lon, lat = centroid(g)
            assigned = which_prov(lon, lat) or pcode
        else:
            assigned = pcode
        chunk[assigned].append(code)

    os.makedirs(dst_dir, exist_ok=True)
    stats = []
    for pcode, codes in sorted(chunk.items()):
        if not codes:
            continue
        used = set()
        feats = []
        for code in codes:
            g = feature_map[code]
            if g is None:
                feats.append([code, cur[code][0], None, cur[code][1]])
                continue
            # Preserve the TopoJSON ring structure: Polygon -> [ring,...], MultiPolygon -> [[ring,...],...].
            # Each ring is a list of arc refs; a feature is a list of rings. This keeps islands, exclaves
            # and holes as separate closed rings instead of concatenating them into one corrupted ring.
            rings = rings_of(g)
            for ring in rings:
                for ai in ring:
                    used.add(ai if ai >= 0 else ~ai)
            feats.append([code, cur[code][0], rings])
        arc_enc = {i: encode_arc(arcs[i]) for i in sorted(used)}
        header = (
            "/* High-resolution Indonesian kabupaten/kota boundaries for the Globe Radar (ADR 0090).\n"
            " * Generated by tools/globe-countries/admin2_hires.py from geoBoundaries Open IDN ADM2\n"
            " * (BPS/WFP/OCHA ROAP, CC BY 3.0 IGO), pinned release %s; shared TopoJSON arcs simplified\n"
            " * once at TOL %s deg, snapped to 1/%d deg. Air-gapped: no CDN (ADR 0013).\n"
            " * Attribution: © BPS / WFP / OCHA ROAP, geoBoundaries (CC BY 3.0 IGO). */\n" % (PIN, TOL, Q)
        )
        body = "window.GLOBE_ADMIN2_HIRES = window.GLOBE_ADMIN2_HIRES || {};\n"
        body += "window.GLOBE_ADMIN2_HIRES['%s'] = { q: %d, arcs: %s, f: %s };\n" % (
            pcode, Q, json.dumps(arc_enc, separators=(",", ":")), json.dumps(feats, separators=(",", ":")))
        path = os.path.join(dst_dir, "%s.js" % pcode)
        with open(path, "w", encoding="utf-8") as f:
            f.write(header + body)
        raw = os.path.getsize(path)
        stats.append((pcode, len(codes), len(used), raw))

    total_raw = sum(s[3] for s in stats)
    print("%d chunks, %d features, %d fallback, unmatched=%d, total raw=%d bytes" % (
        len(stats), sum(cur.keys().__len__() for _ in [0]) if False else sum(len(c) for c in chunk.values()),
        len(unmatched), len(unmatched), total_raw), file=sys.stderr)
    for pcode, nf, na, raw in stats:
        print("  %s: %d features, %d arcs, %d bytes" % (pcode, nf, na, raw), file=sys.stderr)
    if unmatched:
        print("  fallback (embedded) features:", unmatched, file=sys.stderr)


if __name__ == "__main__":
    if len(sys.argv) == 4 and sys.argv[1] != "--fetch":
        main(sys.argv[1], sys.argv[2], sys.argv[3])
    elif len(sys.argv) == 4 and sys.argv[1] == "--fetch":
        tmp = tempfile.mkdtemp(prefix="globe-admin2-hires-")
        try:
            main(fetch(tmp), sys.argv[2], sys.argv[3])
        finally:
            shutil.rmtree(tmp)
    else:
        sys.exit(__doc__)
