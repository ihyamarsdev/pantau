/* Globe Radar — geography (ADR 0090): lat/lon -> sphere, the embedded country
 * dataset (00-countries.js), country hit-testing and the Canvas 2D painters that
 * turn the polygons into textures. No GL here; 03-globe-render.js uploads the result.
 * Air-gapped: nothing is fetched (ADR 0013). */
(function () {
  'use strict';

  var G = (window.WebGlobe = window.WebGlobe || {});
  var DEG = Math.PI / 180;

  // lat/lon (degrees) -> point on a sphere. +Z faces the camera at lon 0, +X is east, +Y is north.
  G.latLonToVec = function (lat, lon, radius) {
    var la = lat * DEG, lo = lon * DEG, c = Math.cos(la);
    return [radius * c * Math.sin(lo), radius * Math.sin(la), radius * c * Math.cos(lo)];
  };

  // ---- Country dataset ------------------------------------------------------
  var ALPHABET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_';
  var LUT = new Int8Array(128);
  for (var i = 0; i < ALPHABET.length; i++) LUT[ALPHABET.charCodeAt(i)] = i;
  var countries = null;

  // One ring: zigzag varints (5 payload bits per char, bit 5 = more follows) of (lon,lat) deltas in 1/q degree.
  // start skips a leading prefix character (admin-1 lines carry their web-map zoom there).
  function decodeRing(s, q, start) {
    var pts = [], x = 0, y = 0, p = start || 0;
    function next() {
      var v = 0, shift = 0, c;
      do { c = LUT[s.charCodeAt(p++)]; v |= (c & 31) << shift; shift += 5; } while (c & 32);
      return (v >>> 1) ^ -(v & 1);
    }
    while (p < s.length) { x += next(); y += next(); pts.push(x / q, y / q); }
    return new Float32Array(pts);
  }

  // Expand bbox to include every point of a decoded ring/line (lon,lat pairs).
  function grow(bbox, r) {
    for (var k = 0; k < r.length; k += 2) {
      bbox[0] = Math.min(bbox[0], r[k]); bbox[2] = Math.max(bbox[2], r[k]);
      bbox[1] = Math.min(bbox[1], r[k + 1]); bbox[3] = Math.max(bbox[3], r[k + 1]);
    }
  }

  // Decoded once on first use: [{iso, name, rings: [Float32Array lon,lat,...], bbox: [w,s,e,n]}].
  G.countries = function () {
    if (countries) return countries;
    var src = window.GLOBE_COUNTRIES || { q: 1, c: [] };
    countries = src.c.map(function (row) {
      var bbox = [180, 90, -180, -90];
      var rings = row[2].split('.').map(function (s) {
        var r = decodeRing(s, src.q);
        grow(bbox, r);
        return r;
      });
      return { iso: row[0], name: row[1], rings: rings, bbox: bbox };
    });
    return countries;
  };

  // Natural Earth 1:10m admin-1 province/state lines, lazy-loaded from a same-origin bundle the first time we
  // zoom in. [{z, pts:Float32Array, bbox:[w,s,e,n]}] or null until the import resolves.
  var admin1 = null, admin1Req = null;
  G.admin1 = function () {
    if (!admin1Req) {
      admin1Req = import('/app/globe-admin1.js').then(function () {
        var src = window.GLOBE_ADMIN1;
        admin1 = src.c.map(function (row) {
          return row.split('.').map(function (s) {
            var pts = decodeRing(s, src.q, 1), bbox = [180, 90, -180, -90];
            grow(bbox, pts);
            return { z: LUT[s.charCodeAt(0)], pts: pts, bbox: bbox };
          });
        }).reduce(function (all, rows) { return all.concat(rows); }, []);
        delete window.GLOBE_ADMIN1;
      }).catch(function (err) {
        console.error('globe: province lines failed to load', err);
      });
    }
    return admin1;
  };

  // Even-odd point-in-polygon over every ring of the country, so holes (Lesotho in South Africa) work.
  function contains(c, lon, lat) {
    var b = c.bbox;
    if (lon < b[0] || lon > b[2] || lat < b[1] || lat > b[3]) return false;
    var odd = false;
    for (var k = 0; k < c.rings.length; k++) {
      var r = c.rings[k];
      for (var i = 0, j = r.length - 2; i < r.length; j = i, i += 2) {
        var yi = r[i + 1], yj = r[j + 1];
        if ((yi > lat) !== (yj > lat) && lon < (r[j] - r[i]) * (lat - yi) / (yj - yi) + r[i]) odd = !odd;
      }
    }
    return odd;
  }

  // Index into countries() of the country at lat/lon, or -1 over open sea.
  G.countryAt = function (lat, lon) {
    var cs = G.countries();
    for (var i = 0; i < cs.length; i++) if (contains(cs[i], lon, lat)) return i;
    return -1;
  };

  // Degrees from lon/lat to the nearest edge of the country (longitude scaled by cos(lat), so a degree is a degree).
  function edgeDistance(c, lon, lat, k) {
    var best = Infinity, px = lon * k;
    for (var n = 0; n < c.rings.length; n++) {
      var r = c.rings[n];
      for (var i = 0, j = r.length - 2; i < r.length; j = i, i += 2) {
        var ax = r[j] * k, ay = r[j + 1], dx = r[i] * k - ax, dy = r[i + 1] - ay, len2 = dx * dx + dy * dy;
        var t = len2 ? Math.max(0, Math.min(1, ((px - ax) * dx + (lat - ay) * dy) / len2)) : 0;
        best = Math.min(best, Math.hypot(px - ax - t * dx, lat - ay - t * dy));
      }
    }
    return best;
  }

  // Like countryAt, but a point just off the coast still belongs to the nearest country within maxDeg. A Host is
  // always on land, yet the simplified coastline can put a port city (New York, Lisbon, Rio) a kilometre or two out to sea.
  G.countryNear = function (lat, lon, maxDeg) {
    var hit = G.countryAt(lat, lon);
    if (hit >= 0) return hit;
    var cs = G.countries(), k = Math.max(0.05, Math.cos(lat * DEG)), best = -1, bestD = maxDeg;
    for (var i = 0; i < cs.length; i++) {
      var b = cs[i].bbox;
      if (lat < b[1] - maxDeg || lat > b[3] + maxDeg || lon < b[0] - maxDeg / k || lon > b[2] + maxDeg / k) continue;
      var d = edgeDistance(cs[i], lon, lat, k);
      if (d < bestD) { bestD = d; best = i; }
    }
    return best;
  };

  // Localised name via the browser's own region names; the dataset's English name is the fallback.
  var displayNames = {};
  G.countryName = function (c, lang) {
    if (!c) return '';
    try {
      var dn = displayNames[lang] || (displayNames[lang] = new Intl.DisplayNames([lang], { type: 'region' }));
      var n = c.iso && dn.of(c.iso);
      if (n && n !== c.iso) return n;
    } catch (e) { /* no Intl.DisplayNames, or a code it does not know */ }
    return c.name;
  };

  // ---- Painters (equirectangular canvas: x = lon, y = lat) -------------------
  // t = {lon0, lat0, sx, sy}: x = (lon - lon0) * sx, y = (lat0 - lat) * sy. The open/cut test stays on raw
  // lon/lat so map edges (antimeridian, poles) stay clean at any zoom.
  function trace(g, rings, t, open) {
    rings.forEach(function (r) {
      var n = r.length / 2, pLon = 0, pLat = 0;
      for (var k = 0; k <= n; k++) {
        var j = k % n, lon = r[2 * j], lat = r[2 * j + 1];
        var x = (lon - t.lon0) * t.sx, y = (t.lat0 - lat) * t.sy;
        var cut = open && ((lon === pLon && Math.abs(lon) === 180) || (lat === pLat && Math.abs(lat) === 90));
        if (!k || cut) g.moveTo(x, y); else g.lineTo(x, y);
        pLon = lon; pLat = lat;
      }
      if (!open) g.closePath();
    });
  }

  // Open polyline through a decoded admin-1 line's points (no closePath).
  function traceLine(g, pts, t) {
    for (var k = 0; k < pts.length; k += 2) {
      var x = (pts[k] - t.lon0) * t.sx, y = (t.lat0 - pts[k + 1]) * t.sy;
      if (!k) g.moveTo(x, y); else g.lineTo(x, y);
    }
  }

  // Stable pseudo-random land tone per country, so neighbours differ a little.
  function tone(c, n) {
    var h = 0;
    for (var k = 0; k < c.name.length; k++) h = (h * 31 + c.name.charCodeAt(k)) | 0;
    return Math.abs(h) % n;
  }

  // Base map: ocean, every country filled in one of pal.land, then all borders in a single stroke so a shared
  // border is painted once and has the same weight as a coastline.
  G.paintMap = function (cv, pal) {
    var W = cv.width, H = cv.height, g = cv.getContext('2d'), cs = G.countries();
    var ident = { lon0: -180, lat0: 90, sx: W / 360, sy: H / 180 };
    g.fillStyle = pal.ocean;
    g.fillRect(0, 0, W, H);
    g.lineJoin = 'round';
    cs.forEach(function (c) {
      g.beginPath();
      trace(g, c.rings, ident, false);
      g.fillStyle = g.strokeStyle = pal.land[tone(c, pal.land.length)];
      g.lineWidth = W / 4096; // a half-texel of overlap hides the sliver where two simplified neighbours disagree, without fattening coasts
      g.fill('evenodd');
      g.stroke();
    });
    g.beginPath();
    cs.forEach(function (c) { trace(g, c.rings, ident, true); });
    g.strokeStyle = pal.border;
    g.lineWidth = pal.line * W / 4096;
    g.stroke();
  };

  // Highlight layer: items = [{idx, fill, stroke?, width?}] drawn over a transparent canvas.
  G.paintOverlay = function (cv, items) {
    var W = cv.width, H = cv.height, g = cv.getContext('2d'), cs = G.countries();
    var ident = { lon0: -180, lat0: 90, sx: W / 360, sy: H / 180 };
    g.clearRect(0, 0, W, H);
    g.lineJoin = 'round';
    items.forEach(function (it) {
      var c = cs[it.idx];
      if (!c) return;
      g.beginPath();
      trace(g, c.rings, ident, false);
      g.fillStyle = it.fill;
      g.fill('evenodd');
      if (it.stroke) {
        g.beginPath();
        trace(g, c.rings, ident, true);
        g.strokeStyle = it.stroke;
        g.lineWidth = (it.width || 1.5) * W / 2048;
        g.stroke();
      }
    });
  };

  // Close-zoom detail texture: country borders plus Natural Earth admin-1 province lines, sampled at the exact
  // lat/lon of each fragment. Returns the number of province lines actually traced. v = {west, east, south,
  // north, px, lineZoom}; px is texels per CSS pixel.
  G.paintDetail = function (cv, v, pal, items, lines) {
    var W = cv.width, H = cv.height, g = cv.getContext('2d'), cs = G.countries();
    var sx = W / (v.east - v.west), sy = H / (v.north - v.south), traced = 0;
    g.fillStyle = pal.ocean;
    g.fillRect(0, 0, W, H);
    g.lineJoin = 'round';

    // A feature near the antimeridian may need drawing at -360/0/+360 to cover a window that crosses the cut.
    function each(bbox, fn) {
      for (var oi = -1; oi <= 1; oi++) {
        var off = oi * 360;
        if (bbox[2] + off < v.west || bbox[0] + off > v.east || bbox[3] < v.south || bbox[1] > v.north) continue;
        fn({ lon0: v.west - off, lat0: v.north, sx: sx, sy: sy });
      }
    }

    // Countries: fill evenodd and stroke in the tone colour, the base map's half-texel seam trick.
    cs.forEach(function (c) {
      each(c.bbox, function (t) {
        g.beginPath();
        trace(g, c.rings, t, false);
        g.fillStyle = g.strokeStyle = pal.land[tone(c, pal.land.length)];
        g.lineWidth = 1;
        g.fill('evenodd');
        g.stroke();
      });
    });

    // Highlight items (hover/selection) over the base fill.
    items.forEach(function (it) {
      var c = cs[it.idx];
      if (!c) return;
      each(c.bbox, function (t) {
        g.beginPath();
        trace(g, c.rings, t, false);
        g.fillStyle = it.fill;
        g.fill('evenodd');
      });
    });

    // Province lines: one dashed path, filtered by Natural Earth web-map zoom (+1 head start in lineZoom).
    if (lines) {
      g.beginPath();
      lines.forEach(function (l) {
        if (l.z > v.lineZoom) return;
        var hit = false;
        each(l.bbox, function (t) { traceLine(g, l.pts, t); hit = true; });
        if (hit) traced++;
      });
      g.setLineDash([4 * v.px, 3 * v.px]);
      g.strokeStyle = pal.admin1;
      g.lineWidth = pal.admin1Line * v.px;
      g.stroke();
      g.setLineDash([]);
    }

    // Country borders on top, open-traced so map edges stay clean.
    g.beginPath();
    cs.forEach(function (c) {
      each(c.bbox, function (t) { trace(g, c.rings, t, true); });
    });
    g.strokeStyle = pal.border;
    g.lineWidth = pal.detailLine * v.px;
    g.stroke();

    // Item strokes last so highlights stay visible over the borders.
    items.forEach(function (it) {
      var c = cs[it.idx];
      if (!c || !it.stroke) return;
      each(c.bbox, function (t) {
        g.beginPath();
        trace(g, c.rings, t, true);
        g.strokeStyle = it.stroke;
        g.lineWidth = (it.width || 1.5) * v.px;
        g.stroke();
      });
    });

    return traced;
  };
})();
