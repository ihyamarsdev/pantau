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
  // zoom in or select a country. admin1 = every line (flat, for close-zoom viewport detail); admin1ByIso groups
  // the same lines by the country's ISO alpha-2 code so selecting a country reveals all of its provinces at once.
  // Both are decoded once, on the first import, and never rebuilt per frame.
  var admin1 = null, admin1ByIso = null, admin1Req = null;
  G.admin1 = function () {
    if (!admin1Req) {
      admin1Req = import('/app/globe-admin1.js').then(function () {
        var src = window.GLOBE_ADMIN1;
        var rows = src.c.map(function (row) {
          return row.split('.').map(function (s) {
            var pts = decodeRing(s, src.q, 1), bbox = [180, 90, -180, -90];
            grow(bbox, pts);
            return { z: LUT[s.charCodeAt(0)], pts: pts, bbox: bbox };
          });
        });
        admin1 = rows.reduce(function (all, row) { return all.concat(row); }, []);
        admin1ByIso = {};
        var byIso = src.iso || {};
        Object.keys(byIso).forEach(function (iso) {
          admin1ByIso[iso] = byIso[iso].reduce(function (all, i) {
            return all.concat(rows[i] || []);
          }, []);
        });
        delete window.GLOBE_ADMIN1;
      }).catch(function (err) {
        console.error('globe: province lines failed to load', err);
      });
    }
    return admin1;
  };
  // Every province/state line of one country (by ISO alpha-2), or [] until the lazy import resolves. The array is
  // built once at decode time, so repeated calls allocate nothing.
  G.admin1Country = function (iso) {
    G.admin1(); // start the import if it has not begun
    return (admin1ByIso && admin1ByIso[iso]) || [];
  };
  G.admin1Ready = function () { return admin1 !== null; };

  // Indonesian province (admin-1) and kabupaten/kota (admin-2) features with real named polygons, from the
  // embedded cahyadsn/wilayah_boundaries bundle (MIT, Kepmendagri 2025). Lazy-loaded on first use like admin1, so
  // the world view stays light. admin2 = {p:[{code,name,lat,lon,rings,bbox}], k:[{code,parent,name,lat,lon,rings,bbox}],
  // byParent: {code: [kabupaten...]}}; every array is built once at decode time, never per frame.
  var admin2 = null, admin2Req = null;
  function decodeAdmin2() {
    var src = window.GLOBE_ADMIN2, byParent = {};
    function decode(row, parent) {
      var bbox = [180, 90, -180, -90];
      var rings = row[parent ? 5 : 4].split('.').map(function (s) {
        var r = decodeRing(s, src.q);
        grow(bbox, r);
        return r;
      });
      return {
        code: row[0], parent: parent ? row[1] : '', name: row[parent ? 2 : 1],
        lat: row[parent ? 3 : 2], lon: row[parent ? 4 : 3],
        rings: rings, bbox: bbox
      };
    }
    var p = src.p.map(function (row) { return decode(row, false); });
    var k = src.k.map(function (row) { return decode(row, true); });
    k.forEach(function (f) { (byParent[f.parent] || (byParent[f.parent] = [])).push(f); });
    admin2 = { p: p, k: k, byParent: byParent };
    delete window.GLOBE_ADMIN2;
  }
  G.admin2Load = function () {
    if (!admin2Req) {
      admin2Req = import('/app/globe-admin2.js').then(decodeAdmin2).catch(function (err) {
        console.error('globe: admin2 boundaries failed to load', err);
      });
    }
    return admin2Req;
  };
  G.admin2 = function () { G.admin2Load(); return admin2; };
  G.admin2Ready = function () { return admin2 !== null; };
  // The complete kabupaten/kota set of one province (by its 2-digit code), or [] until the lazy import resolves.
  G.admin2Kab = function (code) {
    G.admin2();
    return (admin2 && admin2.byParent[code]) || [];
  };

  // High-resolution Indonesian admin-2 from geoBoundaries (CC BY 3.0 IGO), in per-province chunks of shared
  // TopoJSON arcs. Only provinces in view are imported. Arcs are decoded once into Float32Array rings; every
  // feature's rings are assembled from arcs by index (negative = reversed), so neighbouring kabupaten share
  // the exact same boundary vertices -- no cracks or overlaps. Attribution: (c) BPS / WFP / OCHA ROAP,
  // geoBoundaries, CC BY 3.0 IGO.
  var hires = {};       // province code -> {features: [{code,name,rings,bbox}]}
  var hiresReq = {};    // province code -> Promise
  function decodeHiresChunk(code, src) {
    var q = src.q, arcs = src.arcs, cache = {}, out = [];
    function arc(i) {
      if (cache[i]) return cache[i];
      var neg = i < 0, s = arcs[neg ? ~i : i], pts = [], x = 0, y = 0, p = 0;
      function next() {
        var v = 0, sh = 0, c;
        do { c = LUT[s.charCodeAt(p++)]; v |= (c & 31) << sh; sh += 5; } while (c & 32);
        return (v >>> 1) ^ -(v & 1);
      }
      while (p < s.length) { x += next(); y += next(); pts.push(x / q, y / q); }
      if (neg) { var r = new Float32Array(pts.length); for (var k = 0; k < pts.length; k += 2) { r[k] = pts[pts.length - 2 - k]; r[k + 1] = pts[pts.length - 1 - k]; } pts = r; }
      else pts = new Float32Array(pts);
      return (cache[i] = pts);
    }
    src.f.forEach(function (f) {
      if (!f[2]) return; // fallback feature: no high-res geometry, keep the coarse bundle's polygon
      var rings = [], bbox = [180, 90, -180, -90];
      // f[2] is the TopoJSON ring list: [[arcRefs,...],...]. Build ONE closed ring per entry so islands,
      // exclaves and holes stay separate (no connector chords, correct even-odd fill).
      f[2].forEach(function (ringRefs) {
        var pts = [], prevEnd = false;
        ringRefs.forEach(function (ai) {
          var a = arc(ai);
          for (var k = prevEnd ? 2 : 0; k < a.length; k += 2) {
            pts.push(a[k], a[k + 1]);
            bbox[0] = Math.min(bbox[0], a[k]); bbox[2] = Math.max(bbox[2], a[k]);
            bbox[1] = Math.min(bbox[1], a[k + 1]); bbox[3] = Math.max(bbox[3], a[k + 1]);
          }
          prevEnd = true;
        });
        if (pts.length < 6) return;
        // Close the ring: TopoJSON rings are implicitly closed, so append the first point if needed.
        if (pts[0] !== pts[pts.length - 2] || pts[1] !== pts[pts.length - 1]) pts.push(pts[0], pts[1]);
        rings.push(new Float32Array(pts));
      });
      out.push({ code: f[0], name: f[1], rings: rings, bbox: bbox });
    });
    hires[code] = out;
    delete window.GLOBE_ADMIN2_HIRES[code];
  }
  G.hiresLoad = function (code) {
    if (hires[code]) return Promise.resolve(hires[code]);
    if (!hiresReq[code]) {
      hiresReq[code] = import('/app/globe-admin2-hires/' + code + '.js')
        .then(function () { decodeHiresChunk(code, window.GLOBE_ADMIN2_HIRES[code]); })
        .catch(function (err) { console.error('globe: hires chunk ' + code + ' failed', err); });
    }
    return hiresReq[code];
  };
  G.hiresFor = function (code) { return hires[code] || null; };
  G.hiresReady = function (code) { return !!hires[code]; };
  // All 38 province bboxes (from the complete fallback bundle), for deciding which hi-res chunks to load
  // for a painted raster rect. Built once, never per frame.
  var provinceBoxes = null;
  G.provinceBoxes = function () {
    if (!provinceBoxes && admin2) {
      provinceBoxes = admin2.p.map(function (p) { return { code: p.code, bbox: p.bbox }; });
    }
    return provinceBoxes || [];
  };
  // Complete fallback kabupaten polygons for one province (always available once admin2 is loaded).
  G.admin2Fallback = function (code) { return G.admin2Kab(code); };
  G.hiresCount = function () { return Object.keys(hires).length; };


  // Natural Earth 1:10m land/coastline tier (public domain), lazy-loaded only when the coarse 1:50m map's
  // ~0.06 deg simplification starts to exceed ~1.5 CSS px (roughly zoom 3). Decoded once into the same
  // {rings, bbox, iso} shape as countries(), so paintDetail can use it as a drop-in land source.
  var fine = null, fineReq = null;
  G.admin0fineLoad = function () {
    if (!fineReq) {
      fineReq = import('/app/globe-admin0fine.js').then(function () {
        var src = window.GLOBE_ADMIN0FINE;
        fine = src.c.map(function (row) {
          var bbox = [180, 90, -180, -90];
          var rings = row[1].split('.').map(function (s) {
            var r = decodeRing(s, src.q);
            grow(bbox, r);
            return r;
          });
          return { iso: row[0], rings: rings, bbox: bbox };
        });
        delete window.GLOBE_ADMIN0FINE;
      }).catch(function (err) {
        console.error('globe: fine coastline tier failed to load', err);
      });
    }
    return fineReq;
  };
  G.admin0fine = function () { G.admin0fineLoad(); return fine; };
  G.admin0fineReady = function () { return fine !== null; };
  // Accurate country lookup: the fine 1:10m tier when loaded, else the coarse country polygons. The coarse
  // 1:50m simplification can misclassify points near small countries (e.g. Singapore), so prefer fine.
  G.isoAt = function (lat, lon) {
    if (fine) {
      for (var i = 0; i < fine.length; i++) if (contains(fine[i], lon, lat)) return fine[i].iso || '';
      return '';
    }
    var idx = G.countryAt(lat, lon);
    return idx >= 0 ? (G.countries()[idx] || {}).iso || '' : '';
  };

  // Natural Earth admin-1 lines EXCLUDING Indonesia. Where the official 38-province / 514-kabupaten
  // dataset is drawn, the old NE province set conflicts with it, so it is suppressed. Built once at decode.
  var admin1OutsideId = null;
  G.admin1OutsideIndonesia = function () {
    G.admin1();
    if (!admin1OutsideId && admin1) {
      var skip = new Set(G.admin1Country('ID'));
      admin1OutsideId = admin1.filter(function (l) { return !skip.has(l); });
    }
    return admin1OutsideId || [];
  };

  // Point-in-polygon over the official Indonesian features with a bbox prefilter. Returns a shared object
  // (no per-move allocation); callers must read it before the next call. Null when the point is outside ID.
  var areaHit = { province: null, kab: null };
  G.areaAt = function (lat, lon) {
    areaHit.province = areaHit.kab = null;
    if (!admin2) return areaHit;
    var i;
    for (i = 0; i < admin2.k.length; i++) {
      var k = admin2.k[i], b = k.bbox;
      if (lon >= b[0] && lon <= b[2] && lat >= b[1] && lat <= b[3] && contains(k, lon, lat)) {
        areaHit.kab = k;
        break;
      }
    }
    for (i = 0; i < admin2.p.length; i++) {
      var p = admin2.p[i], pb = p.bbox;
      if (lon >= pb[0] && lon <= pb[2] && lat >= pb[1] && lat <= pb[3] && contains(p, lon, lat)) {
        areaHit.province = p;
        break;
      }
    }
    return areaHit;
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
    // Land source: the fine 1:10m tier once loaded, else the coarse 1:50m countries. Over Indonesia the
    // official admin-2 polygons are the land source, so their boundaries line up with the coast and their
    // small islands exist. Bboxes are prefiltered per feature, so a tight view only traces what intersects it.
    var land = (v.land || v.fine || cs);
    var sx = W / (v.east - v.west), sy = H / (v.north - v.south), traced = 0, coarseTraced = 0;
    g.fillStyle = pal.ocean;
    g.fillRect(0, 0, W, H);
    g.lineJoin = 'round';

    function each(bbox, fn) {
      for (var oi = -1; oi <= 1; oi++) {
        var off = oi * 360;
        if (bbox[2] + off < v.west || bbox[0] + off > v.east || bbox[3] < v.south || bbox[1] > v.north) continue;
        fn({ lon0: v.west - off, lat0: v.north, sx: sx, sy: sy });
      }
    }

    // Land fill: even-odd so holes work. When the land source is the official admin-2 polygons, use ONE
    // uniform slate tone (a per-kabupaten tone would make the map look faceted). Country/fine tiers keep
    // their stable per-country tone so neighbours differ as in the world view.
    var uniform = !!v.land;
    land.forEach(function (c, i) {
      each(c.bbox, function (t) {
        g.beginPath();
        trace(g, c.rings, t, false);
        var toneIdx = uniform ? 0 : (c.name ? tone(c, pal.land.length) : (i % pal.land.length));
        g.fillStyle = g.strokeStyle = pal.land[toneIdx];
        g.lineWidth = 1;
        g.fill('evenodd');
        g.stroke();
      });
    });

    // Highlight items (hover/selection) over the base fill. At close zoom fill from the FINE land geometry
    // (official Indonesian polygons or the 1:10m tier), never the coarse 1:50m rings: a coarse tint leaks a
    // straight-edged light band over the sea north of e.g. Java. At world zoom (no fine tier) the coarse
    // country polygons are still the only source.
    var tintLand = v.land || v.fine || null;
    items.forEach(function (it) {
      var c = cs[it.idx];
      if (!c) return;
      var sources = tintLand ? tintLand.filter(function (f) {
        var b = f.bbox;
        return b[2] >= c.bbox[0] && b[0] <= c.bbox[2] && b[3] >= c.bbox[1] && b[1] <= c.bbox[3];
      }) : [c];
      sources.forEach(function (f) {
        each(f.bbox, function (t) {
          g.beginPath();
          trace(g, f.rings, t, false);
          g.fillStyle = it.fill;
          g.fill('evenodd');
        });
      });
    });

    // Province lines: one dashed path, filtered by Natural Earth web-map zoom (+1 head start in lineZoom).
    // v.lines is already the right set: outside Indonesia it is the NE admin-1 lines, over Indonesia it is
    // empty (the official province polygons below replace them). When a fine land tier is active, CLIP the
    // coarse 0.02-deg NE lines to the fine polygons so they cannot cut across the smooth coast into the sea.
    if (lines && lines.length) {
      var clipped = !!(v.fine && land === v.fine);
      if (clipped) {
        g.save();
        g.beginPath();
        v.fine.forEach(function (f) { each(f.bbox, function (t) { trace(g, f.rings, t, false); }); });
        g.clip('evenodd');
      }
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
      if (clipped) g.restore();
    }

    // Indonesian admin features at their LOD: v.admin = { provinces:[feature,...], kabs:[feature,...],
    // province: feature|null (accent outline), kab: feature|null (accent fill + outline)}. Provinces draw
    // from province zoom, kabupaten from kabupaten zoom; the selected feature always draws. Drawn over the
    // land fill and under country borders.
    if (v.admin) {
      var a = v.admin;
      if (a.provinces && a.provinces.length && !a.hiresActive) {
        g.beginPath();
        a.provinces.forEach(function (f) { each(f.bbox, function (t) { trace(g, f.rings, t, true); }); });
        g.strokeStyle = pal.admin1;
        g.lineWidth = pal.admin1Line * v.px;
        g.stroke();
        traced += a.provinces.length;
      }
      if (a.kabs && a.kabs.length) {
        g.beginPath();
        a.kabs.forEach(function (f) { each(f.bbox, function (t) { trace(g, f.rings, t, true); }); });
        g.strokeStyle = pal.kabOutline || 'rgba(150, 170, 235, 0.5)';
        g.lineWidth = (pal.kabOutlineLine || 0.85) * v.px;
        g.stroke();
      }
      var pOut = a.provinceOutline || a.province;
      if (pOut) {
        g.beginPath();
        pOut.rings.forEach(function (r) { each(pOut.bbox, function (t) { trace(g, [r], t, true); }); });
        g.strokeStyle = pal.adminSel;
        g.lineWidth = pal.adminSelLine * v.px;
        g.stroke();
      }
      if (a.kab) {
        each(a.kab.bbox, function (t) {
          g.beginPath();
          trace(g, a.kab.rings, t, false);
          g.fillStyle = pal.kabFill;
          g.fill('evenodd');
          trace(g, a.kab.rings, t, true);
          g.strokeStyle = pal.kabSel;
          g.lineWidth = pal.kabSelLine * v.px;
          g.stroke();
        });
      }
    }

    // Country borders on top. When a finer tier is active (fine 1:10m land outside Indonesia, or official
    // admin-2 inside), the coarse 0.05-deg outline would draw a big straight "diamond" over the smooth
    // coast, so suppress it -- the finer land's own stroke already provides the coast/border.
    if (!v.fine && !v.land) {
      g.beginPath();
      cs.forEach(function (c) {
        each(c.bbox, function (t) { trace(g, c.rings, t, true); coarseTraced++; });
      });
      g.strokeStyle = pal.border;
      g.lineWidth = pal.detailLine * v.px;
      g.stroke();
    }

    // Item strokes last so highlights stay visible over the borders. At close zoom stroke the FINE land
    // geometry (never the coarse 1:50m rings, which draw straight lines across sea/land); when the item's
    // land is not in the fine set, omit the stroke entirely rather than draw a coarse outline.
    if (!tintLand) {
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
    }

    G.paintDetail.coarseTraced = coarseTraced;
    return traced;
  };
})();
