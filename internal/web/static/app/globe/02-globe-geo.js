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
  function decodeRing(s, q) {
    var pts = [], x = 0, y = 0, p = 0;
    function next() {
      var v = 0, shift = 0, c;
      do { c = LUT[s.charCodeAt(p++)]; v |= (c & 31) << shift; shift += 5; } while (c & 32);
      return (v >>> 1) ^ -(v & 1);
    }
    while (p < s.length) { x += next(); y += next(); pts.push(x / q, y / q); }
    return new Float32Array(pts);
  }

  // Decoded once on first use: [{iso, name, rings: [Float32Array lon,lat,...], bbox: [w,s,e,n]}].
  G.countries = function () {
    if (countries) return countries;
    var src = window.GLOBE_COUNTRIES || { q: 1, c: [] };
    countries = src.c.map(function (row) {
      var bbox = [180, 90, -180, -90];
      var rings = row[2].split('.').map(function (s) {
        var r = decodeRing(s, src.q);
        for (var k = 0; k < r.length; k += 2) {
          bbox[0] = Math.min(bbox[0], r[k]); bbox[2] = Math.max(bbox[2], r[k]);
          bbox[1] = Math.min(bbox[1], r[k + 1]); bbox[3] = Math.max(bbox[3], r[k + 1]);
        }
        return r;
      });
      return { iso: row[0], name: row[1], rings: rings, bbox: bbox };
    });
    return countries;
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
  // open=true leaves gaps on the antimeridian cut and the poles, which are map edges, not borders.
  function trace(g, c, W, H, open) {
    c.rings.forEach(function (r) {
      var n = r.length / 2, pLon = 0, pLat = 0;
      for (var k = 0; k <= n; k++) {
        var j = k % n, lon = r[2 * j], lat = r[2 * j + 1];
        var x = (lon + 180) / 360 * W, y = (90 - lat) / 180 * H;
        var cut = open && ((lon === pLon && Math.abs(lon) === 180) || (lat === pLat && Math.abs(lat) === 90));
        if (!k || cut) g.moveTo(x, y); else g.lineTo(x, y);
        pLon = lon; pLat = lat;
      }
      if (!open) g.closePath();
    });
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
    g.fillStyle = pal.ocean;
    g.fillRect(0, 0, W, H);
    g.lineJoin = 'round';
    cs.forEach(function (c) {
      g.beginPath();
      trace(g, c, W, H, false);
      g.fillStyle = g.strokeStyle = pal.land[tone(c, pal.land.length)];
      g.lineWidth = W / 4096; // a half-texel of overlap hides the sliver where two simplified neighbours disagree, without fattening coasts
      g.fill('evenodd');
      g.stroke();
    });
    g.beginPath();
    cs.forEach(function (c) { trace(g, c, W, H, true); });
    g.strokeStyle = pal.border;
    g.lineWidth = pal.line * W / 4096;
    g.stroke();
  };

  // Highlight layer: items = [{idx, fill, stroke?, width?}] drawn over a transparent canvas.
  G.paintOverlay = function (cv, items) {
    var W = cv.width, H = cv.height, g = cv.getContext('2d'), cs = G.countries();
    g.clearRect(0, 0, W, H);
    g.lineJoin = 'round';
    items.forEach(function (it) {
      var c = cs[it.idx];
      if (!c) return;
      g.beginPath();
      trace(g, c, W, H, false);
      g.fillStyle = it.fill;
      g.fill('evenodd');
      if (it.stroke) {
        g.beginPath();
        trace(g, c, W, H, true);
        g.strokeStyle = it.stroke;
        g.lineWidth = (it.width || 1.5) * W / 2048;
        g.stroke();
      }
    });
  };
})();
