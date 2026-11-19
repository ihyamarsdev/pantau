/* Globe Radar — WebGL2 renderer (ADR 0090). Native GL, zero dependencies (ADR 0013).
 * One sphere textured with the country map (02-globe-geo.js), a highlight layer for
 * the countries that matter, an atmosphere halo, and flat-shaded pins and arcs.
 * The controller (04-06) owns state and calls setScene / setTints / setCamera / draw;
 * everything the renderer draws is rebuilt only when one of those changes, never per frame. */
(function () {
  'use strict';

  var G = window.WebGlobe;
  var DEG = Math.PI / 180;
  var PIN_ALT = 1.004;               // pins float just above the mesh so the sphere never clips them
  var DIST = 3.6, FOV_TAN = 0.3615;  // camera distance in globe radii; at zoom 1 the globe fills ~80% of the stage
  var ARC_SEGS = 48;
  var MAP_W = 4096;                  // base map width; the highlight layer is half that

  // Land/ocean palette: blue slate that sits on the neutral dark UI (ADR 0039). Pins, arcs and highlights take the
  // theme's own status and accent tokens at runtime.
  var PALETTE = {
    ocean: '#0d1730',
    land: ['#1f2d54', '#263663', '#2d4075', '#233160', '#2a3b6c'],
    border: 'rgba(165, 185, 245, 0.34)',
    line: 1.7,
  };

  // ---- Matrices (column-major, as GLSL) ---------------------------------------
  function mul(a, b) {
    var o = new Float32Array(16);
    for (var c = 0; c < 4; c++) {
      for (var r = 0; r < 4; r++) {
        o[c * 4 + r] = a[r] * b[c * 4] + a[4 + r] * b[c * 4 + 1] + a[8 + r] * b[c * 4 + 2] + a[12 + r] * b[c * 4 + 3];
      }
    }
    return o;
  }
  function perspective(fx, fy, n, f) {
    return new Float32Array([fx, 0, 0, 0, 0, fy, 0, 0, 0, 0, (f + n) / (n - f), -1, 0, 0, 2 * f * n / (n - f), 0]);
  }
  function rotX(a) {
    var c = Math.cos(a), s = Math.sin(a);
    return new Float32Array([1, 0, 0, 0, 0, c, s, 0, 0, -s, c, 0, 0, 0, 0, 1]);
  }
  function rotY(a) {
    var c = Math.cos(a), s = Math.sin(a);
    return new Float32Array([c, 0, -s, 0, 0, 1, 0, 0, s, 0, c, 0, 0, 0, 0, 1]);
  }
  function translateZ(z) {
    return new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, z, 1]);
  }

  // ---- Shaders (GLSL ES 3.00) -------------------------------------------------
  var SHADERS = {
    // Atmosphere halo: a full-screen triangle that measures how close each camera ray passes to the globe.
    haloVS: `#version 300 es
      out vec2 vNdc;
      void main() {
        vec2 p = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2));
        vNdc = p * 2.0 - 1.0;
        gl_Position = vec4(vNdc, 0.0, 1.0);
      }`,
    haloFS: `#version 300 es
      precision highp float;
      uniform vec2 uFocal;
      uniform float uDist;
      uniform vec3 uGlow;
      in vec2 vNdc;
      out vec4 o;
      void main() {
        vec3 rd = normalize(vec3(vNdc / uFocal, -1.0));
        float b = length(cross(rd, vec3(0.0, 0.0, -uDist)));
        float a = pow(1.0 - smoothstep(1.0, 1.2, b), 2.2) * 0.36;
        o = vec4(uGlow * a, a);
      }`,

    // Sphere: country map + highlight layer, soft light from the upper left, thin rim light.
    globeVS: `#version 300 es
      layout(location=0) in vec3 aPos;
      layout(location=1) in vec2 aUV;
      uniform mat4 uProj, uMV;
      out vec2 vUV;
      out vec3 vN;
      out vec3 vP;
      void main() {
        vec4 p = uMV * vec4(aPos, 1.0);
        vP = p.xyz;
        vN = mat3(uMV) * aPos;
        vUV = aUV;
        gl_Position = uProj * p;
      }`,
    globeFS: `#version 300 es
      precision highp float;
      uniform sampler2D uMap, uOver;
      uniform vec3 uLight, uRim;
      in vec2 vUV;
      in vec3 vN;
      in vec3 vP;
      out vec4 o;
      void main() {
        vec3 n = normalize(vN);
        vec3 c = texture(uMap, vUV).rgb;
        vec4 h = texture(uOver, vUV);
        c = c * (1.0 - h.a) + h.rgb;
        float l = max(dot(n, uLight), 0.0);
        c *= 0.68 + 0.5 * l;
        vec3 v = normalize(-vP);
        c += vec3(0.55, 0.65, 1.0) * pow(max(dot(reflect(-uLight, n), v), 0.0), 26.0) * 0.14;
        float rim = pow(1.0 - max(dot(n, normalize(-vP)), 0.0), 3.0);
        c += uRim * rim * (0.3 + 0.7 * l);
        o = vec4(c, 1.0);
      }`,

    // Arcs: ribbons widened in screen space; the traveling pulse is computed per fragment from time, so
    // the geometry is static.
    arcVS: `#version 300 es
      layout(location=0) in vec3 aPos;
      layout(location=1) in vec3 aPrev;
      layout(location=2) in vec3 aNext;
      layout(location=3) in vec2 aTS;
      layout(location=4) in vec4 aColor;
      layout(location=5) in vec3 aInfo;
      uniform mat4 uMVP;
      uniform vec2 uView;
      uniform float uWidth;
      out vec4 vColor;
      out vec3 vInfo;
      out vec2 vTS;
      void main() {
        vec4 c = uMVP * vec4(aPos, 1.0);
        vec4 p = uMVP * vec4(aPrev, 1.0);
        vec4 n = uMVP * vec4(aNext, 1.0);
        vec2 dir = (n.xy / n.w - p.xy / p.w) * uView;
        float len = length(dir);
        dir = len > 1e-5 ? dir / len : vec2(1.0, 0.0);
        vec2 off = vec2(-dir.y, dir.x) * aTS.y * uWidth / uView;
        gl_Position = vec4(c.xy + off * c.w, c.z, c.w);
        vColor = aColor;
        vInfo = aInfo;
        vTS = aTS;
      }`,
    arcFS: `#version 300 es
      precision highp float;
      uniform float uTime, uBurst;
      in vec4 vColor;
      in vec3 vInfo;
      in vec2 vTS;
      out vec4 o;
      float trail(float d) { return exp(-d * 12.0); }
      void main() {
        float t = vTS.x;
        float head = fract(uTime * vInfo.z + vInfo.x);
        float c = trail(fract(head - t));
        if (vInfo.y > 0.5) c = max(c, trail(fract(t - (1.0 - fract(head + 0.5)))));
        c = clamp(c * (1.0 + uBurst), 0.0, 1.0);
        float edge = 1.0 - smoothstep(0.45, 1.0, abs(vTS.y));
        float ends = smoothstep(0.0, 0.05, t) * smoothstep(1.0, 0.95, t);
        float a = vColor.a * mix(0.5, 1.0, c) * edge * ends;
        o = vec4(mix(vColor.rgb, vec3(1.0), 0.45 * c) * a, a);
      }`,

    // Pins: one point sprite per node; core, dark separation ring, soft glow, alert pulse, selection ring.
    pinVS: `#version 300 es
      layout(location=0) in vec3 aPos;
      layout(location=1) in vec3 aColor;
      layout(location=2) in vec4 aInfo;
      uniform mat4 uMVP;
      uniform float uScale, uMaxPoint;
      out vec3 vColor;
      out vec4 vInfo;
      void main() {
        gl_Position = uMVP * vec4(aPos, 1.0);
        gl_PointSize = min(aInfo.x * 6.0 * uScale, uMaxPoint);
        vColor = aColor;
        vInfo = aInfo;
      }`,
    pinFS: `#version 300 es
      precision highp float;
      uniform float uTime;
      in vec3 vColor;
      in vec4 vInfo;
      out vec4 o;
      vec4 over(vec4 top, vec4 under) { return top + under * (1.0 - top.a); }
      void main() {
        float r = length(gl_PointCoord * 2.0 - 1.0) * 3.0;
        float w = max(fwidth(r), 0.02) * 0.6;
        float core = 1.0 - smoothstep(1.0 - w, 1.0 + w, r);
        float sep = smoothstep(1.0 - w, 1.0 + w, r) * (1.0 - smoothstep(1.4 - w, 1.4 + w, r)) * 0.55;
        float glow = exp(-r * r * 0.3) * 0.32 * (1.0 - smoothstep(1.8, 3.0, r));
        float ph = fract(uTime * 0.8 + vInfo.w);
        float pulse = vInfo.y * (1.0 - ph) * (1.0 - smoothstep(0.0, 0.12 + 2.0 * w, abs(r - mix(1.0, 3.0, ph))));
        float sel = vInfo.z * (1.0 - smoothstep(0.0, 0.1 + 2.0 * w, abs(r - 1.9)));
        vec4 c = vec4(vColor * glow, glow);
        c = over(vec4(0.0, 0.0, 0.0, sep), c);
        c = over(vec4(vColor * pulse, pulse), c);
        c = over(vec4(vec3(sel), sel), c);
        c = over(vec4(vColor * (0.9 + 0.2 * (1.0 - r)) * core, core), c);
        o = c;
      }`,
  };

  // ---- Renderer ---------------------------------------------------------------
  function Renderer(canvas) {
    this.canvas = canvas;
    this.gl = canvas.getContext('webgl2', { antialias: true, alpha: true, premultipliedAlpha: true, powerPreference: 'low-power' });
    this.ok = !!this.gl;
    this.dpr = 1;
    this.pinCount = 0;
    this.arcCount = 0;
    this.m = null;
    this.tmp = [];   // GL objects to free in dispose()
    if (this.ok) this._init();
  }

  Renderer.prototype._own = function (obj, kind) {
    this.tmp.push([kind, obj]);
    return obj;
  };

  Renderer.prototype._init = function () {
    var gl = this.gl;
    this.prog = {
      halo: this._program(SHADERS.haloVS, SHADERS.haloFS, ['uFocal', 'uDist', 'uGlow']),
      globe: this._program(SHADERS.globeVS, SHADERS.globeFS, ['uProj', 'uMV', 'uMap', 'uOver', 'uLight', 'uRim']),
      arc: this._program(SHADERS.arcVS, SHADERS.arcFS, ['uMVP', 'uView', 'uWidth', 'uTime', 'uBurst']),
      pin: this._program(SHADERS.pinVS, SHADERS.pinFS, ['uMVP', 'uScale', 'uMaxPoint', 'uTime']),
    };
    if (!this.prog.halo || !this.prog.globe || !this.prog.arc || !this.prog.pin) { this.ok = false; return; }
    this.glow = this._rgb('--primary', '#5e6ad2');
    this.rim = this._rgb('--accent-text', '#9aa3f8');
    this.rimDim = this.rim.map(function (c) { return c * 0.45; });
    this.maxPoint = gl.getParameter(gl.ALIASED_POINT_SIZE_RANGE)[1];
    this._buildSphere();
    this.haloVao = this._own(gl.createVertexArray(), 'vao');
    this.mapTex = this._buildMap();
    this.overCv = document.createElement('canvas');
    this.overCv.width = Math.min(MAP_W / 2, gl.getParameter(gl.MAX_TEXTURE_SIZE));
    this.overCv.height = this.overCv.width / 2;
    this.overTex = this._texture(this.overCv, null);
    this.pins = this._dynamic(10, [[0, 3, 0], [1, 3, 3], [2, 4, 6]]);
    this.arcs = this._dynamic(18, [[0, 3, 0], [1, 3, 3], [2, 3, 6], [3, 2, 9], [4, 4, 11], [5, 3, 15]]);
    this.arcs.ibo = this._own(gl.createBuffer(), 'buffer');
    gl.bindVertexArray(this.arcs.vao);
    gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, this.arcs.ibo);
    gl.bindVertexArray(null);
    gl.enable(gl.BLEND);
    gl.blendFunc(gl.ONE, gl.ONE_MINUS_SRC_ALPHA); // every program outputs premultiplied alpha
    gl.depthFunc(gl.LEQUAL);
  };

  Renderer.prototype._shader = function (type, src) {
    var gl = this.gl, sh = gl.createShader(type);
    gl.shaderSource(sh, src);
    gl.compileShader(sh);
    if (!gl.getShaderParameter(sh, gl.COMPILE_STATUS)) {
      console.error('globe shader error:', gl.getShaderInfoLog(sh));
      gl.deleteShader(sh);
      return null;
    }
    return sh;
  };

  Renderer.prototype._program = function (vs, fs, uniforms) {
    var gl = this.gl;
    var v = this._shader(gl.VERTEX_SHADER, vs), f = this._shader(gl.FRAGMENT_SHADER, fs);
    if (!v || !f) return null;
    var p = this._own(gl.createProgram(), 'program');
    gl.attachShader(p, v);
    gl.attachShader(p, f);
    gl.linkProgram(p);
    gl.deleteShader(v);
    gl.deleteShader(f);
    if (!gl.getProgramParameter(p, gl.LINK_STATUS)) {
      console.error('globe link error:', gl.getProgramInfoLog(p));
      return null;
    }
    var u = {};
    uniforms.forEach(function (name) { u[name] = gl.getUniformLocation(p, name); });
    return { p: p, u: u };
  };

  // 128x64 lat/lon sphere. Each vertex carries its own UV so texture derivatives stay continuous across the
  // antimeridian (a UV computed per pixel from atan() would put a mip-level seam on the date line).
  Renderer.prototype._buildSphere = function () {
    var gl = this.gl, seg = 128, ring = 64, pos = [], uv = [], idx = [];
    for (var y = 0; y <= ring; y++) {
      var lat = 90 - 180 * y / ring;
      for (var x = 0; x <= seg; x++) {
        var lon = -180 + 360 * x / seg, p = G.latLonToVec(lat, lon, 1);
        pos.push(p[0], p[1], p[2]);
        uv.push(x / seg, y / ring);
      }
    }
    for (var j = 0; j < ring; j++) {
      for (var i = 0; i < seg; i++) {
        var a = j * (seg + 1) + i, b = a + seg + 1;
        idx.push(a, b, a + 1, b, b + 1, a + 1);
      }
    }
    var vao = this.sphereVao = this._own(gl.createVertexArray(), 'vao');
    gl.bindVertexArray(vao);
    this._attrib(new Float32Array(pos), 0, 3);
    this._attrib(new Float32Array(uv), 1, 2);
    var ibo = this._own(gl.createBuffer(), 'buffer');
    gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, ibo);
    gl.bufferData(gl.ELEMENT_ARRAY_BUFFER, new Uint16Array(idx), gl.STATIC_DRAW);
    gl.bindVertexArray(null);
    this.sphereCount = idx.length;
  };

  Renderer.prototype._attrib = function (data, loc, size) {
    var gl = this.gl, b = this._own(gl.createBuffer(), 'buffer');
    gl.bindBuffer(gl.ARRAY_BUFFER, b);
    gl.bufferData(gl.ARRAY_BUFFER, data, gl.STATIC_DRAW);
    gl.enableVertexAttribArray(loc);
    gl.vertexAttribPointer(loc, size, gl.FLOAT, false, 0, 0);
  };

  // An interleaved, re-uploadable vertex buffer. layout = [[location, floats, offsetInFloats], ...].
  Renderer.prototype._dynamic = function (stride, layout) {
    var gl = this.gl, d = { vao: this._own(gl.createVertexArray(), 'vao'), vbo: this._own(gl.createBuffer(), 'buffer') };
    gl.bindVertexArray(d.vao);
    gl.bindBuffer(gl.ARRAY_BUFFER, d.vbo);
    layout.forEach(function (l) {
      gl.enableVertexAttribArray(l[0]);
      gl.vertexAttribPointer(l[0], l[1], gl.FLOAT, false, stride * 4, l[2] * 4);
    });
    gl.bindVertexArray(null);
    return d;
  };

  // Uploads a canvas as a premultiplied RGBA texture with a mipmap chain (crisp borders at the limb). Pass the
  // texture again to replace its pixels.
  Renderer.prototype._texture = function (cv, tex) {
    var gl = this.gl, fresh = !tex;
    if (fresh) tex = this._own(gl.createTexture(), 'texture');
    gl.bindTexture(gl.TEXTURE_2D, tex);
    gl.pixelStorei(gl.UNPACK_PREMULTIPLY_ALPHA_WEBGL, true);
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, cv);
    gl.pixelStorei(gl.UNPACK_PREMULTIPLY_ALPHA_WEBGL, false);
    gl.generateMipmap(gl.TEXTURE_2D);
    if (fresh) {
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR_MIPMAP_LINEAR);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
      var aniso = gl.getExtension('EXT_texture_filter_anisotropic');
      if (aniso) gl.texParameterf(gl.TEXTURE_2D, aniso.TEXTURE_MAX_ANISOTROPY_EXT, Math.min(8, gl.getParameter(aniso.MAX_TEXTURE_MAX_ANISOTROPY_EXT)));
    }
    return tex;
  };

  Renderer.prototype._buildMap = function () {
    var cv = document.createElement('canvas');
    cv.width = Math.min(MAP_W, this.gl.getParameter(this.gl.MAX_TEXTURE_SIZE));
    cv.height = cv.width / 2;
    G.paintMap(cv, PALETTE);
    var tex = this._texture(cv, null);
    cv.width = cv.height = 0; // the GPU has it now; give the 32 MB back
    return tex;
  };

  // ---- Theme colours ----------------------------------------------------------
  Renderer.prototype._css = function (name, fallback) {
    return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || fallback;
  };
  Renderer.prototype._rgb = function (token, fallback) {
    var hex = this._css(token, fallback).replace('#', '');
    if (hex.length === 3) hex = hex[0] + hex[0] + hex[1] + hex[1] + hex[2] + hex[2];
    var n = parseInt(hex, 16);
    if (isNaN(n)) n = parseInt(fallback.replace('#', ''), 16);
    return [((n >> 16) & 255) / 255, ((n >> 8) & 255) / 255, (n & 255) / 255];
  };
  var STATUS = { healthy: '--success', degraded: '--warning', down: '--danger' };
  var LINK = { ssh: '--success', proxy: '--info', app: '--info', vpn: '--accent-text', k8s: '--warning', cloudflare: '--primary' };

  // ---- Scene (rebuilt on change, never per frame) -----------------------------
  // nodes/links: what the filters let through. Pins and arcs are static buffers; only time moves them.
  Renderer.prototype.setScene = function (nodes, links, selectedId) {
    if (!this.ok) return;
    var gl = this.gl, self = this, byId = {};
    var pins = [];
    nodes.forEach(function (n, i) {
      byId[n.id] = n;
      var col = n.kind === 'gateway' ? self._rgb('--accent-text', '#9aa3f8') : self._rgb(STATUS[n.status] || '--text-muted', '#9a9aa3');
      var p = G.latLonToVec(n.lat, n.lon, PIN_ALT);
      var alert = n.active_alerts > 0 || n.status === 'degraded' || n.status === 'down';
      var size = (n.kind === 'gateway' ? 5 : 4) + (n.id === selectedId ? 1 : 0);
      pins.push(p[0], p[1], p[2], col[0], col[1], col[2], size, alert ? 1 : 0, n.id === selectedId ? 1 : 0, (i * 0.37) % 1);
    });
    this.pinCount = nodes.length;
    gl.bindBuffer(gl.ARRAY_BUFFER, this.pins.vbo);
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array(pins), gl.DYNAMIC_DRAW);

    var verts = [], idx = [], base = 0, arcs = 0;
    links.forEach(function (l, li) {
      var a = byId[l.source_id], b = byId[l.target_id];
      if (!a || !b) return;
      var u = G.latLonToVec(a.lat, a.lon, 1), v = G.latLonToVec(b.lat, b.lon, 1);
      var cos = Math.max(-1, Math.min(1, u[0] * v[0] + u[1] * v[1] + u[2] * v[2]));
      var theta = Math.acos(cos), sinT = Math.sin(theta);
      if (theta < 1e-3 || sinT < 1e-4) return; // same place (or antipodal, where no single great circle exists)
      var chord = 2 * Math.sin(theta / 2), lift = Math.max(0.01, 0.12 * chord);
      var rgb = self._rgb(l.status === 'down' ? '--danger' : (LINK[l.type] || '--info'), '#5eb0ef');
      var speed = (l.type === 'vpn' ? 0.12 : l.type === 'k8s' ? 0.1 : 0.16) * (l.latency_ms > 80 ? 0.7 : 1);
      var pts = [];
      for (var i = 0; i <= ARC_SEGS; i++) {
        var t = i / ARC_SEGS, s0 = Math.sin((1 - t) * theta) / sinT, s1 = Math.sin(t * theta) / sinT;
        var alt = 1 + lift * Math.sin(Math.PI * t);
        pts.push([(u[0] * s0 + v[0] * s1) * alt, (u[1] * s0 + v[1] * s1) * alt, (u[2] * s0 + v[2] * s1) * alt]);
      }
      for (var k = 0; k <= ARC_SEGS; k++) {
        var cur = pts[k], prev = pts[Math.max(0, k - 1)], next = pts[Math.min(ARC_SEGS, k + 1)];
        for (var side = -1; side <= 1; side += 2) {
          verts.push(cur[0], cur[1], cur[2], prev[0], prev[1], prev[2], next[0], next[1], next[2],
            k / ARC_SEGS, side, rgb[0], rgb[1], rgb[2], 0.9, (li * 0.137) % 1, l.bidirectional ? 1 : 0, speed);
        }
        if (k < ARC_SEGS) {
          var q = base + k * 2;
          idx.push(q, q + 1, q + 2, q + 1, q + 3, q + 2);
        }
      }
      base += (ARC_SEGS + 1) * 2;
      arcs++;
    });
    this.arcCount = arcs;
    this.arcIndexCount = idx.length;
    gl.bindBuffer(gl.ARRAY_BUFFER, this.arcs.vbo);
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array(verts), gl.DYNAMIC_DRAW);
    gl.bindVertexArray(this.arcs.vao);
    gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, this.arcs.ibo);
    gl.bufferData(gl.ELEMENT_ARRAY_BUFFER, new Uint32Array(idx), gl.DYNAMIC_DRAW);
    gl.bindVertexArray(null);
  };

  // items: [{idx, fill, stroke?, width?}] from the controller; repainted and re-uploaded only when called.
  Renderer.prototype.setTints = function (items) {
    if (!this.ok) return;
    G.paintOverlay(this.overCv, items);
    this._texture(this.overCv, this.overTex);
  };

  // ---- Camera -----------------------------------------------------------------
  // cam: {lon, lat, zoom}, radians; the globe turns, the camera stays on +Z. Zoom narrows the lens
  // instead of moving closer, so perspective distortion never changes.
  Renderer.prototype.setCamera = function (cam) {
    var w = this.canvas.width || 1, h = this.canvas.height || 1, aspect = w / h;
    var fy = cam.zoom / FOV_TAN * Math.min(1, aspect), fx = fy / aspect;
    var model = mul(rotX(cam.lat), rotY(-cam.lon));
    var mv = mul(translateZ(-DIST), model);
    var proj = perspective(fx, fy, 0.5, 20);
    this.m = { model: model, mv: mv, proj: proj, mvp: mul(proj, mv), fx: fx, fy: fy };
  };

  Renderer.prototype.resize = function () {
    if (!this.ok) return;
    this.dpr = Math.min(window.devicePixelRatio || 1, 2);
    var pw = Math.max(1, Math.floor((this.canvas.clientWidth || 800) * this.dpr));
    var ph = Math.max(1, Math.floor((this.canvas.clientHeight || 600) * this.dpr));
    if (this.canvas.width !== pw || this.canvas.height !== ph) {
      this.canvas.width = pw;
      this.canvas.height = ph;
    }
  };

  // ---- Draw -------------------------------------------------------------------
  Renderer.prototype.draw = function (ts, burst) {
    if (!this.ok || !this.m) return;
    var gl = this.gl, m = this.m, t = ts * 0.001, W = this.canvas.width, H = this.canvas.height;
    gl.viewport(0, 0, W, H);
    gl.clearColor(0, 0, 0, 0);
    gl.clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT);

    // Halo behind everything.
    var hp = this.prog.halo;
    gl.disable(gl.DEPTH_TEST);
    gl.useProgram(hp.p);
    gl.uniform2f(hp.u.uFocal, m.fx, m.fy);
    gl.uniform1f(hp.u.uDist, DIST);
    gl.uniform3fv(hp.u.uGlow, this.glow);
    gl.bindVertexArray(this.haloVao);
    gl.drawArrays(gl.TRIANGLES, 0, 3);

    // Globe.
    var gp = this.prog.globe;
    gl.enable(gl.DEPTH_TEST);
    gl.useProgram(gp.p);
    gl.uniformMatrix4fv(gp.u.uProj, false, m.proj);
    gl.uniformMatrix4fv(gp.u.uMV, false, m.mv);
    gl.uniform3f(gp.u.uLight, -0.5, 0.55, 0.67);
    gl.uniform3fv(gp.u.uRim, this.rimDim);
    gl.activeTexture(gl.TEXTURE0);
    gl.bindTexture(gl.TEXTURE_2D, this.mapTex);
    gl.uniform1i(gp.u.uMap, 0);
    gl.activeTexture(gl.TEXTURE1);
    gl.bindTexture(gl.TEXTURE_2D, this.overTex);
    gl.uniform1i(gp.u.uOver, 1);
    gl.bindVertexArray(this.sphereVao);
    gl.drawElements(gl.TRIANGLES, this.sphereCount, gl.UNSIGNED_SHORT, 0);

    // Arcs and pins read the depth buffer (the sphere hides what is behind it) but never write it.
    gl.depthMask(false);
    if (this.arcIndexCount) {
      var ap = this.prog.arc;
      gl.useProgram(ap.p);
      gl.uniformMatrix4fv(ap.u.uMVP, false, m.mvp);
      gl.uniform2f(ap.u.uView, W, H);
      gl.uniform1f(ap.u.uWidth, 2.0 * this.dpr);
      gl.uniform1f(ap.u.uTime, t);
      gl.uniform1f(ap.u.uBurst, burst || 0);
      gl.bindVertexArray(this.arcs.vao);
      gl.drawElements(gl.TRIANGLES, this.arcIndexCount, gl.UNSIGNED_INT, 0);
    }
    if (this.pinCount) {
      var pp = this.prog.pin;
      gl.useProgram(pp.p);
      gl.uniformMatrix4fv(pp.u.uMVP, false, m.mvp);
      gl.uniform1f(pp.u.uScale, this.dpr);
      gl.uniform1f(pp.u.uMaxPoint, this.maxPoint);
      gl.uniform1f(pp.u.uTime, t);
      gl.bindVertexArray(this.pins.vao);
      gl.drawArrays(gl.POINTS, 0, this.pinCount);
    }
    gl.depthMask(true);
    gl.bindVertexArray(null);
  };

  // ---- Picking ----------------------------------------------------------------
  // Pin -> CSS pixels inside the canvas; visible=false when it is on the far side.
  Renderer.prototype.project = function (node) {
    if (!this.ok || !this.m) return { x: 0, y: 0, visible: false };
    var m = this.m, p = G.latLonToVec(node.lat, node.lon, PIN_ALT);
    var mv = m.mv, mvp = m.mvp;
    var cx = mvp[0] * p[0] + mvp[4] * p[1] + mvp[8] * p[2] + mvp[12];
    var cy = mvp[1] * p[0] + mvp[5] * p[1] + mvp[9] * p[2] + mvp[13];
    var cw = mvp[3] * p[0] + mvp[7] * p[1] + mvp[11] * p[2] + mvp[15];
    // Facing the camera: the pin's outward normal (rotated into view space) against the direction to the eye.
    var nx = mv[0] * p[0] + mv[4] * p[1] + mv[8] * p[2];
    var ny = mv[1] * p[0] + mv[5] * p[1] + mv[9] * p[2];
    var nz = mv[2] * p[0] + mv[6] * p[1] + mv[10] * p[2];
    var vx = nx + mv[12], vy = ny + mv[13], vz = nz + mv[14];
    var facing = nx * -vx + ny * -vy + nz * -vz > 0;
    return {
      x: (cx / cw * 0.5 + 0.5) * this.canvas.clientWidth,
      y: (1 - (cy / cw * 0.5 + 0.5)) * this.canvas.clientHeight,
      visible: facing && cw > 0,
    };
  };

  // CSS pixels inside the canvas -> {lat, lon} (degrees) under the cursor, or null when the ray misses the globe.
  Renderer.prototype.unproject = function (x, y) {
    if (!this.ok || !this.m) return null;
    var m = this.m;
    var nx = x / this.canvas.clientWidth * 2 - 1, ny = 1 - y / this.canvas.clientHeight * 2;
    var dx = nx / m.fx, dy = ny / m.fy, dz = -1, len = Math.hypot(dx, dy, dz);
    dx /= len; dy /= len; dz /= len;
    var b = -DIST * dz;                         // ray . centre, the globe sits at (0,0,-DIST)
    var disc = b * b - (DIST * DIST - 1);
    if (disc < 0) return null;
    var t = b - Math.sqrt(disc);
    var hx = t * dx, hy = t * dy, hz = t * dz + DIST;  // hit point relative to the globe centre, view space
    var r = m.model;                            // pure rotation, so its transpose undoes it
    var lx = r[0] * hx + r[1] * hy + r[2] * hz;
    var ly = r[4] * hx + r[5] * hy + r[6] * hz;
    var lz = r[8] * hx + r[9] * hy + r[10] * hz;
    return { lat: Math.asin(Math.max(-1, Math.min(1, ly))) / DEG, lon: Math.atan2(lx, lz) / DEG };
  };

  // Frees every GL object and the highlight canvas. releaseContext: the canvas is being thrown away, so give
  // the browser's limited pool of WebGL contexts one back; a canvas that is reused on the next open must keep it.
  Renderer.prototype.dispose = function (releaseContext) {
    var gl = this.gl;
    if (!gl) return;
    this.tmp.forEach(function (o) {
      if (o[0] === 'buffer') gl.deleteBuffer(o[1]);
      else if (o[0] === 'vao') gl.deleteVertexArray(o[1]);
      else if (o[0] === 'program') gl.deleteProgram(o[1]);
      else if (o[0] === 'texture') gl.deleteTexture(o[1]);
    });
    this.tmp = [];
    if (this.overCv) this.overCv.width = this.overCv.height = 0;
    var lose = releaseContext && gl.getExtension('WEBGL_lose_context');
    if (lose) lose.loseContext();
    this.ok = false;
  };

  G.Renderer = Renderer;
})();
