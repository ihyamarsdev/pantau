#!/usr/bin/env python3
"""Regenerate internal/web/static/app/globe-admin1/00-admin1.js (ADR 0090).

The Globe Radar draws province/state boundary lines on its close-zoom detail
texture from an embedded Natural Earth 1:10m admin-1 dataset, so it works
air-gapped (ADR 0013). Source: Natural Earth 1:10m admin-1 states/provinces
lines (public domain, https://www.naturalearthdata.com). Standard library only.

Each line is simplified with Douglas-Peucker (TOL degrees), snapped to 1/Q
degree, then delta-encoded as zigzag varints, 5 payload bits per character
(A-Z a-z 0-9 - _), bit 5 = "more follows". A leading zoom character (the
Natural Earth MIN_ZOOM at which the line should appear) lets the browser reveal
lines progressively. Lines of one country (ADM0_A3) are joined with '.' into one
row; a parallel iso index maps ISO alpha-2 codes to those row indices so the
browser can select every province line of one country at once. Input lines are
split at any antimeridian jump before encoding (the current data has none).
"""
import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from gen import ALPHABET, douglas_peucker, varint  # noqa: E402

TOL = 0.02       # Douglas-Peucker tolerance, degrees on the sphere
Q = 50           # coordinate units per degree (0.02 deg)
SKIP = {"Admin-1 statistical meta bounds", "Admin-1 boundary indicator"}
# Natural Earth admin-1 lines carry a 3-letter ADM0_A3; the country dataset uses
# ISO 3166-1 alpha-2. This table is derived from Natural Earth admin-0's own
# ISO_A2_EH / ISO_A2 properties (see gen.py), so the browser can group province
# lines by the same iso code a country is keyed by. Not a hand-maintained list.
A3_TO_A2 = {
    "": "", "AFG": "AF", "AGO": "AO", "AIA": "AI", "ALB": "AL", "ALD": "AX", "AND": "AD",
    "ARE": "AE", "ARG": "AR", "ARM": "AM", "ASM": "AS", "ATG": "AG", "AUS": "AU",
    "AUT": "AT", "AZE": "AZ", "BDI": "BI", "BEL": "BE", "BEN": "BJ", "BFA": "BF",
    "BGD": "BD", "BGR": "BG", "BHR": "BH", "BHS": "BS", "BIH": "BA", "BLR": "BY",
    "BLZ": "BZ", "BMU": "BM", "BOL": "BO", "BRA": "BR", "BRB": "BB", "BRN": "BN",
    "BTN": "BT", "BWA": "BW", "CAF": "CF", "CAN": "CA", "CHE": "CH", "CHL": "CL",
    "CHN": "CN", "CIV": "CI", "CMR": "CM", "COD": "CD", "COG": "CG", "COL": "CO",
    "CPV": "CV", "CRI": "CR", "CUB": "CU", "CYP": "CY", "CZE": "CZ", "DEU": "DE",
    "DJI": "DJ", "DMA": "DM", "DNK": "DK", "DOM": "DO", "DZA": "DZ", "ECU": "EC",
    "EGY": "EG", "ERI": "ER", "ESP": "ES", "EST": "EE", "ETH": "ET", "FIN": "FI",
    "FJI": "FJ", "FRA": "FR", "GAB": "GA", "GBR": "GB", "GEO": "GE", "GHA": "GH",
    "GIN": "GN", "GMB": "GM", "GNB": "GW", "GNQ": "GQ", "GRC": "GR", "GRD": "GD",
    "GRL": "GL", "GTM": "GT", "GUY": "GY", "HKG": "HK", "HND": "HN", "HRV": "HR",
    "HTI": "HT", "HUN": "HU", "IDN": "ID", "IND": "IN", "IRL": "IE", "IRN": "IR",
    "IRQ": "IQ", "ISL": "IS", "ISR": "IL", "ITA": "IT", "JAM": "JM", "JOR": "JO",
    "JPN": "JP", "KAZ": "KZ", "KEN": "KE", "KGZ": "KG", "KHM": "KH", "KNA": "KN",
    "KOR": "KR", "KOS": "XK", "KWT": "KW", "LAO": "LA", "LBN": "LB", "LBR": "LR",
    "LBY": "LY", "LCA": "LC", "LIE": "LI", "LKA": "LK", "LSO": "LS", "LTU": "LT",
    "LUX": "LU", "LVA": "LV", "MAR": "MA", "MDA": "MD", "MDG": "MG", "MEX": "MX",
    "MKD": "MK", "MLI": "ML", "MLT": "MT", "MMR": "MM", "MNE": "ME", "MNG": "MN",
    "MOZ": "MZ", "MRT": "MR", "MSR": "MS", "MUS": "MU", "MWI": "MW", "MYS": "MY",
    "NAM": "NA", "NCL": "NC", "NER": "NE", "NGA": "NG", "NIC": "NI", "NLD": "NL",
    "NOR": "NO", "NPL": "NP", "NRU": "NR", "NZL": "NZ", "OMN": "OM", "PAK": "PK",
    "PER": "PE", "PHL": "PH", "PLW": "PW", "PNG": "PG", "POL": "PL", "PRK": "KP",
    "PRT": "PT", "PRY": "PY", "QAT": "QA", "ROU": "RO", "RUS": "RU", "RWA": "RW",
    "SAH": "EH", "SAU": "SA", "SDN": "SD", "SEN": "SN", "SGP": "SG", "SLB": "SB",
    "SLE": "SL", "SLV": "SV", "SMR": "SM", "SOM": "SO", "SRB": "RS", "SUR": "SR",
    "SVK": "SK", "SVN": "SI", "SWE": "SE", "SWZ": "SZ", "SYC": "SC", "SYR": "SY",
    "TCD": "TD", "TGO": "TG", "THA": "TH", "TJK": "TJ", "TKM": "TM", "TLS": "TL",
    "TTO": "TT", "TUN": "TN", "TUR": "TR", "TWN": "TW", "TZA": "TZ", "UGA": "UG",
    "UKR": "UA", "URY": "UY", "USA": "US", "UZB": "UZ", "VCT": "VC", "VEN": "VE",
    "VNM": "VN", "WLF": "WF", "WSM": "WS", "YEM": "YE", "ZAF": "ZA", "ZMB": "ZM",
    "ZWE": "ZW",
}


def split_antimeridian(line):
    """Split a line wherever consecutive longitudes differ by more than 180 deg."""
    if not line:
        return []
    parts, cur = [], [line[0]]
    for a, b in zip(line, line[1:]):
        if abs(a[0] - b[0]) > 180:
            parts.append(cur)
            cur = [b]
        else:
            cur.append(b)
    parts.append(cur)
    return parts


def encode_line(part, zoom):
    """Encode one line as ALPHABET[zoom] + zigzag varint deltas from (0,0)."""
    snapped = []
    for lon, lat in douglas_peucker(part, TOL):
        pt = (round(lon * Q), round(lat * Q))
        if not snapped or snapped[-1] != pt:
            snapped.append(pt)
    if len(snapped) < 2:
        return None
    out, px, py = ALPHABET[zoom], 0, 0
    for x, y in snapped:
        out += varint(x - px) + varint(y - py)
        px, py = x, y
    return out


def main(src, dst):
    with open(src, encoding="utf-8") as f:
        features = json.load(f)["features"]
    groups = {}
    count = 0
    for feat in features:
        props, geom = feat["properties"], feat["geometry"]
        if geom is None or props.get("FEATURECLA") in SKIP:
            continue
        zoom = max(0, min(63, round(props.get("MIN_ZOOM") or 0)))
        parts = [geom["coordinates"]] if geom["type"] == "LineString" else geom["coordinates"]
        encs = []
        for part in parts:
            for piece in split_antimeridian(part):
                enc = encode_line(piece, zoom)
                if enc:
                    encs.append(enc)
        if encs:
            key = props.get("ADM0_A3") or ""
            groups.setdefault(key, []).extend(encs)
            count += len(encs)
    keys = sorted(groups)
    rows = [".".join(groups[k]) for k in keys]
    # ISO alpha-2 -> list of row indices. Several A3 codes can share one ISO code
    # (e.g. disputed territories), so collect all of them: selecting a country
    # reveals every associated line group, never a viewport-dependent subset.
    iso = {}
    for idx, key in enumerate(keys):
        iso.setdefault(A3_TO_A2.get(key, key), []).append(idx)
    with open(dst, "w", encoding="utf-8") as out:
        out.write(
            "/* Embedded Natural Earth 1:10m admin-1 province/state lines for the Globe Radar\n"
            " * (ADR 0090). Generated by tools/globe-countries/admin1.py from Natural Earth\n"
            " * 1:10m admin-1 (public domain); do not edit by hand. Lines are '.'-separated,\n"
            " * each prefixed by its web-map zoom character, then delta-encoded zigzag varints\n"
            " * of (lon,lat) in 1/q degree, decoded by 02-globe-geo.js. iso maps ISO alpha-2\n"
            " * codes to row indices so a whole country's provinces can be shown at once.\n"
            " * Air-gapped: no CDN (ADR 0013). */\n"
            "window.GLOBE_ADMIN1 = { q: %d, iso: %s, c: [\n" % (Q, json.dumps(iso, sort_keys=True, separators=(",", ":")))
        )
        for row in rows:
            out.write(" %s,\n" % json.dumps(row))
        out.write("] };\n")
    print("%d lines in %d countries, %d chars" % (
        count, len(rows), sum(len(r) for r in rows)), file=sys.stderr)


if __name__ == "__main__":
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    main(sys.argv[1], sys.argv[2])
