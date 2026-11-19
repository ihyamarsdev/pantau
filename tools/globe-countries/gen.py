#!/usr/bin/env python3
"""Regenerate internal/web/static/app/globe/00-countries.js (ADR 0090).

The Globe Radar draws a political world map from an embedded country dataset, so it
works air-gapped (ADR 0013). Source: Natural Earth 1:50m admin-0 countries (public
domain, https://www.naturalearthdata.com). Standard library only.

Shrinking, in order: islands and lakes under MIN_AREA deg^2 are dropped (a country always
keeps its largest ring), each ring is simplified with Douglas-Peucker (TOL degrees,
longitude scaled by cos(lat) so the error is the same on the sphere at every latitude;
vertices on the antimeridian and at the poles are always kept, or the cut edges of
Antarctica, Russia and Fiji would drift off the map edge), then snapped to 1/Q degree and
delta-encoded as zigzag varints, 5 payload bits per character (A-Z a-z 0-9 - _), bit 5 =
"more follows". Rings of one country are joined with '.'; the browser fills them with the
even-odd rule, so holes (Lesotho in South Africa, the Vatican in Italy) need no winding
bookkeeping.
"""
import json
import math
import sys

TOL = 0.06        # Douglas-Peucker tolerance, degrees on the sphere
MIN_AREA = 0.004  # deg^2 (~50 km^2 at the equator)
Q = 20            # coordinate units per degree (0.05 deg)
ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"


def area(ring):
    a = 0.0
    for i in range(len(ring)):
        x1, y1 = ring[i]
        x2, y2 = ring[(i + 1) % len(ring)]
        a += x1 * y2 - x2 * y1
    return abs(a) / 2


def douglas_peucker(pts, tol):
    n = len(pts)
    if n < 3:
        return pts
    xs = [p[0] * math.cos(math.radians(p[1])) for p in pts]
    ys = [p[1] for p in pts]
    keep = [False] * n
    keep[0] = keep[-1] = True
    stack = [(0, n - 1)]
    while stack:
        a, b = stack.pop()
        if b <= a + 1:
            continue
        ax, ay, dx, dy = xs[a], ys[a], xs[b] - xs[a], ys[b] - ys[a]
        len2 = dx * dx + dy * dy
        far, far_d = -1, -1.0
        for i in range(a + 1, b):
            t = 0.0 if len2 == 0 else max(0.0, min(1.0, ((xs[i] - ax) * dx + (ys[i] - ay) * dy) / len2))
            d = math.hypot(xs[i] - (ax + t * dx), ys[i] - (ay + t * dy))
            if d > far_d:
                far, far_d = i, d
        if far_d > tol:
            keep[far] = True
            stack += [(a, far), (far, b)]
    # The cut along the antimeridian and the pole row are map edges: those vertices must survive.
    return [p for p, k in zip(pts, keep) if k or abs(p[1]) >= 89.99 or abs(p[0]) >= 179.99]


def simplify_ring(ring, tol):
    pts = ring[:-1] if ring[0] == ring[-1] else list(ring)
    if len(pts) < 4:
        return pts
    # Split the closed ring at the point farthest from the start so both halves have stable anchors.
    far = max(range(len(pts)), key=lambda i: (pts[i][0] - pts[0][0]) ** 2 + (pts[i][1] - pts[0][1]) ** 2)
    first = douglas_peucker(pts[: far + 1], tol)
    second = douglas_peucker(pts[far:] + [pts[0]], tol)
    return first[:-1] + second[:-1]


def varint(v):
    z = (v << 1) ^ (v >> 63)
    out = ""
    while z >= 32:
        out += ALPHABET[(z & 31) | 32]
        z >>= 5
    return out + ALPHABET[z]


def encode_ring(ring):
    snapped = []
    for lon, lat in simplify_ring(ring, TOL):
        pt = (round(lon * Q), round(lat * Q))
        if not snapped or snapped[-1] != pt:
            snapped.append(pt)
    if len(snapped) > 1 and snapped[0] == snapped[-1]:
        snapped.pop()
    if len(snapped) < 3:
        return None
    out, px, py = "", 0, 0
    for x, y in snapped:
        out += varint(x - px) + varint(y - py)
        px, py = x, y
    return out


def main(src, dst):
    with open(src, encoding="utf-8") as f:
        features = json.load(f)["features"]
    rows, verts = [], 0
    for feat in features:
        props, geom = feat["properties"], feat["geometry"]
        iso = props.get("ISO_A2_EH") or props.get("ISO_A2") or ""
        iso = "" if iso == "-99" else iso
        name = props.get("NAME_EN") or props["NAME"]
        polys = [geom["coordinates"]] if geom["type"] == "Polygon" else geom["coordinates"]
        largest = max(polys, key=lambda p: area(p[0]))
        rings = []
        for poly in polys:
            if poly is not largest and area(poly[0]) < MIN_AREA:
                continue
            for i, ring in enumerate(poly):
                if i and area(ring) < MIN_AREA:  # a hole this small is a micro-state; let the land fill it
                    continue
                if any(abs(a[0] - b[0]) > 180 for a, b in zip(ring, ring[1:])):
                    sys.exit("antimeridian jump in %s: split the ring first" % name)
                enc = encode_ring(ring)
                if enc:
                    rings.append(enc)
                    verts += len(enc)
        if rings:
            rows.append((name, iso, ".".join(rings)))
    rows.sort()
    with open(dst, "w", encoding="utf-8") as out:
        out.write(
            "/* Embedded world-country dataset for the Globe Radar (ADR 0090). Generated by\n"
            " * tools/globe-countries/gen.py from Natural Earth 1:50m admin-0 (public domain);\n"
            " * do not edit by hand. [ISO 3166-1 alpha-2 or '', English name, rings]; rings are\n"
            " * '.'-separated delta-encoded varints of (lon,lat) in 1/q degree, decoded by\n"
            " * 02-globe-geo.js. Air-gapped: no map tiles, no CDN (ADR 0013). */\n"
            "window.GLOBE_COUNTRIES = { q: %d, c: [\n" % Q
        )
        for name, iso, enc in rows:
            out.write(" %s,\n" % json.dumps([iso, name, enc], ensure_ascii=True, separators=(",", ":")))
        out.write("] };\n")
    print("%d countries, %d chars of ring data" % (len(rows), verts), file=sys.stderr)


if __name__ == "__main__":
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    main(sys.argv[1], sys.argv[2])
