/* Terraform Configuration tab: file model (names, validation, dirty tracking, per-file CodeMirror docs). */
(function () {
  'use strict';
  const TFUI = window.TfUI = window.TfUI || {};
  const C = TFUI.cfg = {
    files: {},        // name -> saved content (as of load / last save)
    docs: {},         // name -> CodeMirror.Doc
    gens: {},         // name -> clean changeGeneration
    cur: '', cm: null, mounted: false, obs: null, root: null,
    NAME_RE: /^[A-Za-z0-9_][A-Za-z0-9_.-]{0,99}$/,
    EXTS: ['.tf', '.tf.json', '.tfvars', '.tftpl', '.json', '.yaml', '.yml', '.txt', '.md'],
    MAX_FILES: 50, MAX_KIB: 256,
  };
  C.names = function () {
    return Object.keys(C.files).sort((a, b) => (a === 'main.tf' ? -1 : b === 'main.tf' ? 1 : a.localeCompare(b)));
  };
  C.dirty = function (n) { const d = C.docs[n]; return !d || !d.isClean(C.gens[n]); };
  C.dirtyNames = function () { return C.names().filter(n => C.dirty(n)); };
  C.isDirty = function () { return C.dirtyNames().length > 0; };
  C.collect = function () { const o = {}; for (const n of C.names()) o[n] = C.docs[n] ? C.docs[n].getValue() : C.files[n]; return o; };
  C.markSaved = function () { for (const n of C.names()) C.gens[n] = C.docs[n].changeGeneration(true); };

  // Mirrors terraform.ValidateFiles (internal/terraform): name regex, no .. / \, allowed extension, 50 files.
  C.validateName = function (name) {
    if (Object.keys(C.files).length >= C.MAX_FILES) return tx('tfcfg_too_many');
    if (!C.NAME_RE.test(name) || name.includes('..') || !C.EXTS.some(e => name.endsWith(e))) return tx('tfcfg_bad_name');
    return '';
  };
  C.modeFor = function (name) {
    if (name.endsWith('.yaml') || name.endsWith('.yml')) return 'yaml';
    if (name.endsWith('.sh')) return 'shell';
    if (name.endsWith('.json')) return { name: 'javascript', json: true };
    return null;
  };
  C.kib = function (s) { return (new Blob([s || '']).size / 1024).toFixed(1); };

  C.load = function (ws) {
    C.files = Object.assign({}, (ws && ws.files) || {});
    if (!Object.keys(C.files).length) C.files = { 'main.tf': '' };
    C.docs = {}; C.gens = {};
    C.cur = C.names().includes('main.tf') ? 'main.tf' : C.names()[0];
    for (const n of C.names()) C.docFor(n);
  };
  C.docFor = function (n) {
    if (C.docs[n]) return C.docs[n];
    const d = CodeMirror.Doc(C.files[n] || '', C.modeFor(n));
    d.on('change', () => { if (C.dirtyUI) C.dirtyUI(); });
    C.docs[n] = d; C.gens[n] = d.changeGeneration(true);
    return d;
  };
  C.open = function (n) {
    if (!(n in C.files)) return;
    C.cur = n;
    if (C.cm) C.cm.swapDoc(C.docFor(n));
    if (C.fileUI) C.fileUI();
  };
  C.add = function (name) {
    name = (name || '').trim();
    if (name in C.files) { C.open(name); return true; } // duplicate: just select the existing file
    const err = C.validateName(name);
    if (err) { showToast(err, 'error'); return false; }
    C.files[name] = ''; C.docFor(name);
    C.open(name);
    return true;
  };
  C.remove = function (n) {
    if (C.names().length <= 1) { showToast(tx('tfcfg_last_file'), 'error'); return false; }
    delete C.files[n]; delete C.docs[n]; delete C.gens[n];
    if (C.cur === n) C.open(C.names()[0]);
    else if (C.fileUI) C.fileUI();
    return true;
  };
  C.discard = function () {
    for (const n of C.names()) {
      if (C.docs[n] && C.docs[n].getValue() !== C.files[n]) {
        C.docs[n].setValue(C.files[n]);
        C.gens[n] = C.docs[n].changeGeneration(true);
      }
    }
    if (C.fileUI) C.fileUI();
  };
  C.destroy = function () {
    if (C.obs) { C.obs.disconnect(); C.obs = null; }
    C.cm = null; C.docs = {}; C.gens = {}; C.files = {}; C.cur = ''; C.root = null; C.mounted = false;
    try { tfDirtyGuard = null; } catch (e) { /* hook not landed yet */ }
  };
})();
