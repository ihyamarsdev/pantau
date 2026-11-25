/* Workspace drawer tab "Configuration": inline file editor (CodeMirror, fmt/validate, registry check)
   and Git repository browser/editor with commit & push. Exposes Tf.config. */
(function () {
  'use strict';
  const Tf = window.Tf;

  Tf.addI18n({
    en: {
      tf_ws_tab_config: 'Configuration',
      tf_cfg_add_ph: 'file name, e.g. outputs.tf', tf_cfg_add: 'Add file', tf_cfg_files_count: '{n} / 50 files',
      tf_cfg_delete: 'Delete', tf_cfg_unsaved: 'Unsaved', tf_cfg_unsaved_title: 'Unsaved changes',
      tf_cfg_unsaved_msg: 'Leave without saving? Your changes to the configuration will be lost.', tf_cfg_leave: 'Leave',
      tf_cfg_discard: 'Discard', tf_cfg_discard_msg: 'Discard all unsaved changes?', tf_cfg_discard_ok: 'Discard',
      tf_cfg_format: 'Format', tf_cfg_format_title: 'terraform fmt (Shift+Ctrl/Cmd+F)', tf_cfg_formatting: 'Formatting…',
      tf_cfg_formatted: 'Formatted {n} file(s)', tf_cfg_already_fmt: 'Already formatted',
      tf_cfg_validate: 'Validate', tf_cfg_validate_title: 'terraform init and validate on the current files', tf_cfg_validating: 'Validating…',
      tf_cfg_valid_ok: 'Configuration is valid', tf_cfg_diag_counts: '{e} error(s), {w} warning(s)', tf_cfg_init_err: 'terraform init failed',
      tf_cfg_fmt_errs: 'Format errors', tf_cfg_diag_title: 'Diagnostics', tf_cfg_diag_close: 'Close',
      tf_cfg_sev_error: 'Error', tf_cfg_sev_warning: 'Warning',
      tf_cfg_registry: 'Registry', tf_cfg_registry_title: 'Insert a module or provider snippet from the registry',
      tf_cfg_registry_missing: 'Registry browser is not available',
      tf_cfg_regcheck: 'Check versions', tf_cfg_regcheck_title: 'Compare module and provider versions with the registry',
      tf_cfg_checking: 'Checking…', tf_cfg_regcheck_none: 'No module or provider versions to check.',
      tf_cfg_regcheck_unsupported: 'Version checks need the Terraform Gate and a registry URL.',
      tf_cfg_regcheck_unsaved: 'Unsaved edits are not checked. Save first for exact results.',
      tf_cfg_regcheck_stale: 'The line changed since the check. Run Check versions again.',
      tf_cfg_regcheck_applied: 'Applied', tf_cfg_regcheck_close: 'Close', tf_cfg_regcheck_latest: 'latest {v}',
      tf_cfg_regcheck_apply: 'Use {v}', tf_cfg_st_up_to_date: 'up to date', tf_cfg_st_outdated: 'outdated',
      tf_cfg_st_newer: 'newer than constraint', tf_cfg_st_unpinned: 'unpinned', tf_cfg_st_unknown: 'unknown',
      tf_cfg_detect: 'Detect variables', tf_cfg_detect_title: 'Add variables declared in variables.tf to the Variables tab',
      tf_cfg_save: 'Save', tf_cfg_save_plan: 'Save & Plan', tf_cfg_save_plan_title: 'Save, then start a plan',
      tf_cfg_saved: 'Configuration saved', tf_cfg_save_err: 'Save failed', tf_cfg_locked: 'The workspace is locked. Saving is still allowed; runs wait for the lock.',
      tf_cfg_name_too_many: 'A workspace can hold at most 50 files', tf_cfg_name_bad: 'Use letters, digits, _, - or . with an allowed extension (.tf, .tfvars, .hcl, .json, .yaml, .md, .txt, .tftpl)',
      tf_cfg_name_dup: 'A file with that name already exists', tf_cfg_last_file: 'A workspace needs at least one file',
      tf_cfg_del_title: 'Delete file', tf_cfg_del_msg: 'Delete {name} from the configuration? The change takes effect on save.',
      tf_cfg_no_cm: 'Editor is not available',
      tf_cfg_git_repo: 'Repository', tf_cfg_git_branch: 'Branch', tf_cfg_git_path: 'Working dir', tf_cfg_git_commit: 'Commit',
      tf_cfg_git_test: 'Test VCS Connection', tf_cfg_git_testing: 'Testing…', tf_cfg_git_ok: 'Connection OK',
      tf_cfg_git_sync: 'Reload tree', tf_cfg_git_edit: 'Edit Git config', tf_cfg_git_filter_ph: 'Filter files…',
      tf_cfg_git_loading: 'Loading repository…', tf_cfg_git_empty: 'No files in this repository.',
      tf_cfg_git_truncated: 'The repository has more files than shown.', tf_cfg_git_active: 'working dir',
      tf_cfg_git_commit_ph: 'Commit message', tf_cfg_git_commit_btn: 'Commit & Push', tf_cfg_git_committing: 'Pushing…',
      tf_cfg_git_changed: '{n} file(s) changed', tf_cfg_git_none: 'No changes to commit',
      tf_cfg_git_msg_req: 'Enter a commit message', tf_cfg_git_committed: 'Committed and pushed {c}',
      tf_cfg_git_conflict: 'The repository changed since it was opened. Reload the tree and apply your edits again.',
      tf_cfg_git_readonly: 'Push failed. The Git credentials may be read-only; they need write access to the remote.',
      tf_cfg_git_pick: 'Select a file to view or edit', tf_cfg_git_unsaved_reload: 'Reloading discards unsaved edits. Continue?',
      tf_cfg_git_unsaved_hint: 'Unsaved edits are kept in this view until you commit or leave the drawer.',
    },
    id: {
      tf_ws_tab_config: 'Konfigurasi',
      tf_cfg_add_ph: 'nama file, mis. outputs.tf', tf_cfg_add: 'Tambah file', tf_cfg_files_count: '{n} / 50 file',
      tf_cfg_delete: 'Hapus', tf_cfg_unsaved: 'Belum disimpan', tf_cfg_unsaved_title: 'Perubahan belum disimpan',
      tf_cfg_unsaved_msg: 'Tinggalkan tanpa menyimpan? Perubahan konfigurasi Anda akan hilang.', tf_cfg_leave: 'Tinggalkan',
      tf_cfg_discard: 'Buang', tf_cfg_discard_msg: 'Buang semua perubahan yang belum disimpan?', tf_cfg_discard_ok: 'Buang',
      tf_cfg_format: 'Format', tf_cfg_format_title: 'terraform fmt (Shift+Ctrl/Cmd+F)', tf_cfg_formatting: 'Memformat…',
      tf_cfg_formatted: '{n} file diformat', tf_cfg_already_fmt: 'Sudah terformat',
      tf_cfg_validate: 'Validasi', tf_cfg_validate_title: 'terraform init dan validate pada file saat ini', tf_cfg_validating: 'Memvalidasi…',
      tf_cfg_valid_ok: 'Konfigurasi valid', tf_cfg_diag_counts: '{e} error, {w} peringatan', tf_cfg_init_err: 'terraform init gagal',
      tf_cfg_fmt_errs: 'Error format', tf_cfg_diag_title: 'Diagnostik', tf_cfg_diag_close: 'Tutup',
      tf_cfg_sev_error: 'Error', tf_cfg_sev_warning: 'Peringatan',
      tf_cfg_registry: 'Registry', tf_cfg_registry_title: 'Sisipkan snippet modul atau provider dari registry',
      tf_cfg_registry_missing: 'Browser registry tidak tersedia',
      tf_cfg_regcheck: 'Periksa versi', tf_cfg_regcheck_title: 'Bandingkan versi modul dan provider dengan registry',
      tf_cfg_checking: 'Memeriksa…', tf_cfg_regcheck_none: 'Tidak ada versi modul atau provider untuk diperiksa.',
      tf_cfg_regcheck_unsupported: 'Pemeriksaan versi memerlukan Terraform Gate dan URL registry.',
      tf_cfg_regcheck_unsaved: 'Perubahan yang belum disimpan tidak diperiksa. Simpan dulu untuk hasil yang akurat.',
      tf_cfg_regcheck_stale: 'Baris sudah berubah sejak pemeriksaan. Jalankan Periksa versi lagi.',
      tf_cfg_regcheck_applied: 'Diterapkan', tf_cfg_regcheck_close: 'Tutup', tf_cfg_regcheck_latest: 'terbaru {v}',
      tf_cfg_regcheck_apply: 'Pakai {v}', tf_cfg_st_up_to_date: 'sudah terbaru', tf_cfg_st_outdated: 'usang',
      tf_cfg_st_newer: 'lebih baru dari constraint', tf_cfg_st_unpinned: 'tanpa versi', tf_cfg_st_unknown: 'tidak diketahui',
      tf_cfg_detect: 'Deteksi variabel', tf_cfg_detect_title: 'Tambahkan variabel dari variables.tf ke tab Variabel',
      tf_cfg_save: 'Simpan', tf_cfg_save_plan: 'Simpan & Plan', tf_cfg_save_plan_title: 'Simpan, lalu mulai plan',
      tf_cfg_saved: 'Konfigurasi disimpan', tf_cfg_save_err: 'Gagal menyimpan', tf_cfg_locked: 'Workspace terkunci. Menyimpan tetap diizinkan; run menunggu kunci dilepas.',
      tf_cfg_name_too_many: 'Sebuah workspace maksimal berisi 50 file', tf_cfg_name_bad: 'Gunakan huruf, angka, _, - atau . dengan ekstensi yang diizinkan (.tf, .tfvars, .hcl, .json, .yaml, .md, .txt, .tftpl)',
      tf_cfg_name_dup: 'Sudah ada file dengan nama itu', tf_cfg_last_file: 'Workspace memerlukan minimal satu file',
      tf_cfg_del_title: 'Hapus file', tf_cfg_del_msg: 'Hapus {name} dari konfigurasi? Perubahan berlaku setelah disimpan.',
      tf_cfg_no_cm: 'Editor tidak tersedia',
      tf_cfg_git_repo: 'Repositori', tf_cfg_git_branch: 'Branch', tf_cfg_git_path: 'Direktori kerja', tf_cfg_git_commit: 'Commit',
      tf_cfg_git_test: 'Uji Koneksi VCS', tf_cfg_git_testing: 'Menguji…', tf_cfg_git_ok: 'Koneksi OK',
      tf_cfg_git_sync: 'Muat ulang pohon', tf_cfg_git_edit: 'Ubah konfigurasi Git', tf_cfg_git_filter_ph: 'Saring berkas…',
      tf_cfg_git_loading: 'Memuat repositori…', tf_cfg_git_empty: 'Tidak ada berkas di repositori ini.',
      tf_cfg_git_truncated: 'Repositori memiliki lebih banyak berkas daripada yang ditampilkan.', tf_cfg_git_active: 'direktori kerja',
      tf_cfg_git_commit_ph: 'Pesan commit', tf_cfg_git_commit_btn: 'Commit & Push', tf_cfg_git_committing: 'Mendorong…',
      tf_cfg_git_changed: '{n} berkas berubah', tf_cfg_git_none: 'Tidak ada perubahan untuk di-commit',
      tf_cfg_git_msg_req: 'Isi pesan commit', tf_cfg_git_committed: 'Di-commit dan di-push {c}',
      tf_cfg_git_conflict: 'Repositori berubah sejak dibuka. Muat ulang pohon lalu terapkan kembali perubahan Anda.',
      tf_cfg_git_readonly: 'Push gagal. Kredensial Git mungkin hanya baca; kredensial perlu izin tulis ke remote.',
      tf_cfg_git_pick: 'Pilih berkas untuk dilihat atau diubah', tf_cfg_git_unsaved_reload: 'Memuat ulang akan membuang perubahan yang belum di-commit. Lanjutkan?',
      tf_cfg_git_unsaved_hint: 'Perubahan yang belum di-commit tetap ada di tampilan ini sampai Anda commit atau meninggalkan drawer.',
    },
  });

  /* ---- CodeMirror 5 mode for HCL (.tf/.tfvars/.hcl), same as the legacy 25-config-hcl.js ---- */
  (function () {
    if (typeof CodeMirror === 'undefined') return;
    const KEYWORDS = new Set(['resource', 'data', 'variable', 'output', 'locals', 'module', 'provider', 'terraform', 'for_each', 'count', 'dynamic', 'for', 'in', 'if']);
    const REFS = new Set(['var', 'local', 'each', 'path', 'self']);
    const ATOMS = new Set(['true', 'false', 'null']);
    function strTok(stream, state) {
      const start = stream.pos;
      while (!stream.eol()) {
        const c = stream.peek();
        if (c === '"') {
          if (stream.pos > start) return 'string';
          stream.next(); state.ctx.pop(); return 'string';
        }
        if (c === '\\') { stream.next(); stream.next(); continue; }
        if (c === '$' || c === '%') {
          if (stream.match(/^[$%]{2}\{/)) continue;
          if (stream.match(/^[$%]\{/, false)) {
            if (stream.pos > start) return 'string';
            stream.pos += 2; state.ctx.push({ d: 0 }); return 'variable-2';
          }
        }
        stream.next();
      }
      return 'string';
    }
    function codeTok(stream, state) {
      if (stream.eatSpace()) return null;
      const first = state.first; state.first = false;
      const top = state.ctx[state.ctx.length - 1];
      if (stream.match(/^(#|\/\/)/)) { stream.skipToEnd(); return 'comment'; }
      if (stream.match('/*')) { state.block = true; return blockTok(stream, state); }
      if (stream.match(/^<<-?([A-Za-z_]\w*)/)) { state.heredoc = stream.current().replace(/^<<-?/, ''); return 'string-2'; }
      if (stream.peek() === '"') {
        if (state.hdr) { stream.match(/^"(?:[^"\\]|\\.)*"?/); return 'def'; }
        stream.next(); state.ctx.push({ str: true }); return 'string';
      }
      if (stream.match(/^\d+(\.\d+)?([eE][+-]?\d+)?/)) return 'number';
      if (stream.match(/^[A-Za-z_][\w-]*/)) {
        const w = stream.current();
        if (ATOMS.has(w)) return 'atom';
        if (!state.ctx.length && first && stream.match(/^\s*(?:"|[A-Za-z_][\w-]*\s*["{]|\{)/, false)) { state.hdr = true; return 'keyword'; }
        if (state.hdr) return 'def';
        if (KEYWORDS.has(w)) return 'keyword';
        if (stream.match(/^\s*=(?![=>])/, false)) return 'property';
        if (REFS.has(w)) return 'variable-2';
        if (stream.peek() === '(') return 'variable-3';
        return 'variable';
      }
      const ch = stream.next();
      if (ch === '{') { state.hdr = false; if (top) top.d++; return null; }
      if (ch === '}' && top) {
        if (top.d === 0) { state.ctx.pop(); return 'variable-2'; }
        top.d--; return null;
      }
      if ('=!<>+-*/%&|?:'.includes(ch)) { stream.match(/^[=>&|]/); return 'operator'; }
      return null;
    }
    function blockTok(stream, state) {
      while (!stream.eol()) if (stream.match('*/')) { state.block = false; break; } else stream.next();
      return 'comment';
    }
    CodeMirror.defineMode('hcl', function () {
      return {
        lineComment: '#', blockCommentStart: '/*', blockCommentEnd: '*/',
        startState: () => ({ ctx: [], block: false, heredoc: null, first: true, hdr: false }),
        copyState: s => Object.assign({}, s, { ctx: s.ctx.map(f => Object.assign({}, f)) }),
        token(stream, state) {
          if (state.heredoc) {
            if (stream.sol() && stream.match(new RegExp('^\\s*' + state.heredoc.replace(/\W/g, '\\$&') + '\\s*$'))) state.heredoc = null;
            else stream.skipToEnd();
            return 'string-2';
          }
          if (state.block) return blockTok(stream, state);
          // Strings are single-line: any unfinished state is reset at the line start.
          if (stream.sol()) { state.ctx = []; state.first = true; state.hdr = false; }
          const top = state.ctx[state.ctx.length - 1];
          return top && top.str ? strTok(stream, state) : codeTok(stream, state);
        },
      };
    });
    CodeMirror.defineMIME('text/x-hcl', 'hcl');
  })();

  /* ---- state ---- */
  const NAME_RE = /^[A-Za-z0-9_][A-Za-z0-9_.-]{0,99}$/;
  const EXTS = ['.tf', '.tf.json', '.tfvars', '.tftpl', '.json', '.yaml', '.yml', '.txt', '.md'];
  const MAX_FILES = 50, MAX_KIB = 256;
  const C = (Tf.config = {
    ws: null, ctx: null, root: null, cm: null, mode: 'inline', observer: null, guardFn: null,
    files: {}, docs: {}, cur: '', git: null, busy: false,
  });

  C.names = function () {
    return Object.keys(C.files).sort((a, b) => (a === 'main.tf' ? -1 : b === 'main.tf' ? 1 : a.localeCompare(b)));
  };
  // Dirty = editor text differs from the last saved text. Compared as strings, so undo back to saved reads clean.
  C.dirty = function (n) { const d = C.docs[n]; return !!d && d.getValue() !== C.files[n]; };
  C.dirtyNames = function () { return C.names().filter(C.dirty); };
  C.gitDirty = function () {
    const g = C.git; if (!g) return [];
    return Object.keys(g.docs).filter(p => g.docs[p].getValue() !== g.saved[p]);
  };
  function hasUnsaved() { return C.mode === 'git' ? C.gitDirty().length > 0 : C.dirtyNames().length > 0; }
  // Used both internally (no args -> current editor) and by other modules (wsId -> true if that workspace has unsaved edits).
  C.isDirty = function (wsId) {
    if (wsId === undefined) return hasUnsaved();
    return !!C.root && !!C.ws && C.ws.id === wsId && hasUnsaved();
  };

  C.modeFor = function (name) {
    if (name.endsWith('.yaml') || name.endsWith('.yml')) return 'yaml';
    if (name.endsWith('.sh')) return 'shell';
    if (/\.(tf|tfvars|hcl)$/.test(name)) return 'hcl';
    if (name.endsWith('.json')) return { name: 'javascript', json: true };
    return null;
  };
  C.kib = function (s) { return (new Blob([s || '']).size / 1024).toFixed(1); };

  C.load = function (ws) {
    C.files = Object.assign({}, (ws && ws.files) || {});
    if (!Object.keys(C.files).length) C.files = { 'main.tf': '' };
    C.docs = {};
    C.cur = C.names().includes('main.tf') ? 'main.tf' : C.names()[0];
    C.names().forEach(C.docFor);
  };
  C.docFor = function (n) {
    if (C.docs[n]) return C.docs[n];
    const d = CodeMirror.Doc(C.files[n] || '', C.modeFor(n));
    d.on('change', () => C.refreshUI());
    C.docs[n] = d;
    return d;
  };
  C.open = function (n) {
    if (!(n in C.files)) return;
    C.cur = n;
    if (C.cm) C.cm.swapDoc(C.docFor(n));
    C.refreshUI();
  };
  C.validateName = function (name) {
    if (Object.keys(C.files).length >= MAX_FILES) return Tf.t('tf_cfg_name_too_many', 'A workspace can hold at most 50 files');
    if (!NAME_RE.test(name) || name.includes('..') || !EXTS.some(e => name.endsWith(e))) return Tf.t('tf_cfg_name_bad', 'Invalid file name');
    return '';
  };
  C.add = function (name) {
    name = (name || '').trim();
    if (name in C.files) { C.open(name); return true; } // duplicate: select the existing file
    const err = C.validateName(name);
    if (err) { Tf.toast(err, 'error'); return false; }
    C.files[name] = ''; C.docFor(name);
    C.open(name);
    return true;
  };
  C.remove = function (n) {
    if (C.names().length <= 1) { Tf.toast(Tf.t('tf_cfg_last_file', 'A workspace needs at least one file'), 'error'); return false; }
    delete C.files[n]; delete C.docs[n];
    if (C.cur === n) C.open(C.names()[0]);
    else C.refreshUI();
    return true;
  };
  C.discard = function () {
    C.names().forEach(n => { if (C.dirty(n)) C.docs[n].setValue(C.files[n]); });
    C.refreshUI();
  };

  /* ---- dirty guard: the workspace drawer asks before leaving with unsaved edits ---- */
  async function canLeave() {
    if (!C.isDirty()) return true;
    return !!(await Tf.confirm({ title: Tf.t('tf_cfg_unsaved_title', 'Unsaved changes'), message: Tf.t('tf_cfg_unsaved_msg', ''), confirmText: Tf.t('tf_cfg_leave', 'Leave'), danger: true }));
  }
  function setGuard(fn) {
    if (Tf.workspaces) Tf.workspaces.guard = fn || null; // blocks tab switches
    if (C.ctx && C.ctx.setCanClose) { C.ctx.setCanClose(fn); return; }
    const top = Tf.drawer.current();
    if (top) top.canClose = fn; // blocks closing the drawer
  }
  function reset() {
    if (C.observer) { C.observer.disconnect(); C.observer = null; }
    setGuard(null);
    C.cm = null; C.root = null; C.docs = {}; C.files = {}; C.cur = ''; C.git = null; C.ws = null;
  }

  /* ---- small helpers ---- */
  function busy(btn, label) {
    if (!btn) return function () {};
    const html = btn.innerHTML;
    btn.disabled = true;
    btn.textContent = label;
    return function () { btn.disabled = false; btn.innerHTML = html; };
  }
  function errText(e) { return Tf.errMsg(e); }
  function showErr(id, msg) { const el = document.getElementById(id); if (el) { el.textContent = msg || ''; el.hidden = !msg; } }
  function stale(root, id) { return C.root !== root || !C.ws || C.ws.id !== id; }
  function wsPath(ws, suffix) { return '/api/terraform/workspaces/' + ws.id + (suffix || ''); }

  /* ---- inline mode: markup ---- */
  function fileListHTML() {
    return C.names().map(n => '<div class="tf-cfg-frow' + (n === C.cur ? ' active' : '') + (C.dirty(n) ? ' dirty' : '') + '">' +
      '<button type="button" class="tf-cfg-fbtn" data-file="' + Tf.esc(n) + '" aria-current="' + (n === C.cur) + '" title="' + Tf.esc(n) + '">' +
      '<i class="tf-cfg-dot"></i><span class="tf-cfg-fname">' + Tf.esc(n) + '</span></button>' +
      '<button type="button" class="tf-cfg-fdel" data-del="' + Tf.esc(n) + '" aria-label="' + Tf.esc(Tf.t('tf_cfg_delete', 'Delete')) + ' ' + Tf.esc(n) + '">' + Tf.icon('trash-2', 'icon-xs') + '</button></div>').join('');
  }
  function inlineShell() {
    return '<div class="tf-cfg tf-cfg-inline">' +
      '<div class="tf-cfg-main"><aside class="tf-cfg-side">' +
      '<div class="tf-cfg-add"><input type="text" id="tfCfgNewName" class="form-input" placeholder="' + Tf.esc(Tf.t('tf_cfg_add_ph', '')) + '" aria-label="' + Tf.esc(Tf.t('tf_cfg_add', 'Add file')) + '" />' +
      '<button type="button" id="tfCfgAddFile" class="btn btn-sm" title="' + Tf.esc(Tf.t('tf_cfg_add', 'Add file')) + '">' + Tf.icon('plus', 'icon-xs') + '</button></div>' +
      '<div id="tfCfgCount" class="tf-cfg-count"></div>' +
      '<div id="tfCfgFiles" class="tf-cfg-list">' + fileListHTML() + '</div></aside>' +
      '<section class="tf-cfg-ed">' +
      '<div class="tf-cfg-bar"><span id="tfCfgCurName" class="tf-cfg-name tf-mono"></span>' +
      '<span id="tfCfgSize" class="tf-cfg-size"></span>' +
      '<span id="tfCfgDirty" class="tf-cfg-dirty" hidden>' + Tf.esc(Tf.t('tf_cfg_unsaved', 'Unsaved')) + '</span>' +
      '<span class="tf-cfg-gap"></span>' +
      '<span class="tf-cfg-grp">' +
      '<button type="button" id="tfCfgFmt" class="btn btn-sm" title="' + Tf.esc(Tf.t('tf_cfg_format_title', '')) + '">' + Tf.t('tf_cfg_format', 'Format') + '</button>' +
      '<button type="button" id="tfCfgValidate" class="btn btn-sm" title="' + Tf.esc(Tf.t('tf_cfg_validate_title', '')) + '">' + Tf.icon('list-checks', 'icon-xs') + ' ' + Tf.t('tf_cfg_validate', 'Validate') + '</button>' +
      '<button type="button" id="tfCfgRegistry" class="btn btn-sm" title="' + Tf.esc(Tf.t('tf_cfg_registry_title', '')) + '">' + Tf.icon('package', 'icon-xs') + ' ' + Tf.t('tf_cfg_registry', 'Registry') + '</button>' +
      '<button type="button" id="tfCfgRegCheck" class="btn btn-sm" title="' + Tf.esc(Tf.t('tf_cfg_regcheck_title', '')) + '">' + Tf.icon('refresh-cw', 'icon-xs') + ' ' + Tf.t('tf_cfg_regcheck', 'Check versions') + '</button>' +
      '<button type="button" id="tfCfgDetect" class="btn btn-sm" hidden title="' + Tf.esc(Tf.t('tf_cfg_detect_title', '')) + '">' + Tf.t('tf_cfg_detect', 'Detect variables') + '</button></span>' +
      '<span class="tf-cfg-grp">' +
      '<button type="button" id="tfCfgDiscard" class="btn btn-sm">' + Tf.t('tf_cfg_discard', 'Discard') + '</button>' +
      '<button type="button" id="tfCfgSavePlan" class="btn btn-sm" title="' + Tf.esc(Tf.t('tf_cfg_save_plan_title', '')) + '">' + Tf.icon('zap', 'icon-xs') + ' ' + Tf.t('tf_cfg_save_plan', 'Save & Plan') + '</button>' +
      '<button type="button" id="tfCfgSave" class="btn btn-sm btn-primary">' + Tf.icon('save', 'icon-xs') + ' ' + Tf.t('tf_cfg_save', 'Save') + '</button></span></div>' +
      (C.ws && C.ws.locked ? '<div class="tf-cfg-hint">' + Tf.icon('lock', 'icon-xs') + ' ' + Tf.esc(Tf.t('tf_cfg_locked', '')) + '</div>' : '') +
      '<div id="tfCfgError" class="tf-cfg-err" role="alert" hidden></div>' +
      '<div id="tfCfgDiag" class="tf-cfg-diag" role="region" aria-label="' + Tf.esc(Tf.t('tf_cfg_diag_title', 'Diagnostics')) + '" hidden></div>' +
      '<div id="tfCfgRegStatus" class="tf-cfg-regc" role="region" hidden></div>' +
      '<div id="tfCfgEditor" class="tf-cfg-edbox"></div></section></div></div>';
  }

  /* ---- inline mode: UI refresh (cheap; runs on every edit) ---- */
  C.refreshUI = function () {
    if (!C.root || C.mode !== 'inline') return;
    const list = document.getElementById('tfCfgFiles');
    if (list) list.innerHTML = fileListHTML();
    const n = Object.keys(C.files).length;
    const cnt = document.getElementById('tfCfgCount');
    if (cnt) cnt.textContent = Tf.t('tf_cfg_files_count', '{n} / 50 files', { n: n });
    const name = document.getElementById('tfCfgCurName'); if (name) name.textContent = C.cur;
    const size = document.getElementById('tfCfgSize');
    if (size && C.docs[C.cur]) size.textContent = C.kib(C.docs[C.cur].getValue()) + ' / ' + MAX_KIB + ' KiB';
    const dirty = C.isDirty();
    const d = document.getElementById('tfCfgDirty'); if (d) d.hidden = !dirty;
    const dis = document.getElementById('tfCfgDiscard'); if (dis) dis.disabled = !dirty;
    const det = document.getElementById('tfCfgDetect'); if (det) det.hidden = !(C.files['variables.tf'] !== undefined);
    setGuard(canLeave);
  };

  /* ---- inline mode: actions ---- */
  C.inlineInit = function () {
    C.load(C.ws);
    if (typeof CodeMirror === 'undefined') { showErr('tfCfgError', Tf.t('tf_cfg_no_cm', 'Editor is not available')); return; }
    const fmt = () => C.format();
    C.cm = CodeMirror(document.getElementById('tfCfgEditor'), {
      lineNumbers: true, theme: 'vscode-dark', indentUnit: 2, tabSize: 2, indentWithTabs: false,
      extraKeys: {
        Tab: cm => cm.replaceSelection('  ', 'end'),
        'Shift-Ctrl-F': fmt, 'Shift-Cmd-F': fmt,
        'Ctrl-S': () => C.save(false), 'Cmd-S': () => C.save(false),
      },
    });
    C.cm.swapDoc(C.docFor(C.cur));
    C.refreshUI();
  };

  C.save = async function (thenPlan) {
    const root = C.root, ws = C.ws;
    showErr('tfCfgError', '');
    const body = {
      name: ws.name, description: ws.description || '', group_name: ws.group_name || '', environment: ws.environment || '',
      tags: ws.tags || [], binary: ws.binary || 'terraform', source: 'inline',
      drift_interval_minutes: ws.drift_interval_minutes || 0, plan_interval_minutes: ws.plan_interval_minutes || 0,
      auto_apply: !!ws.auto_apply, files: {},
    };
    C.names().forEach(n => { body.files[n] = C.docs[n] ? C.docs[n].getValue() : C.files[n]; });
    const btn = document.getElementById('tfCfgSave');
    const restore = busy(btn, Tf.t('tf_cfg_save', 'Save'));
    try {
      const saved = await Tf.api.put(wsPath(ws), body);
      if (stale(root, ws.id)) return;
      C.files = Object.assign({}, body.files);
      Object.keys(C.docs).forEach(n => { if (!(n in C.files)) delete C.docs[n]; });
      if (C.ctx && C.ctx.reload) { const fresh = await C.ctx.reload(); if (fresh && fresh.id === ws.id) C.ws = fresh; }
      else if (saved && saved.workspace) C.ws = saved.workspace;
      Tf.toast(Tf.t('tf_cfg_saved', 'Configuration saved'), 'success');
      C.refreshUI();
      if (thenPlan && Tf.runs && Tf.runs.startDialog) Tf.runs.startDialog(ws.id, 'plan');
    } catch (e) {
      if (!stale(root, ws.id)) showErr('tfCfgError', Tf.t('tf_cfg_save_err', 'Save failed') + ': ' + errText(e));
    } finally {
      restore();
    }
  };

  C.format = async function () {
    if (C.busy) return;
    const root = C.root, ws = C.ws, files = {};
    C.names().forEach(n => { files[n] = C.docs[n].getValue(); });
    C.busy = true;
    const btn = document.getElementById('tfCfgFmt');
    const restore = busy(btn, Tf.t('tf_cfg_formatting', 'Formatting…'));
    let r;
    try { r = await Tf.api.post(wsPath(ws, '/tools/fmt'), { files: files }); }
    catch (e) { if (!stale(root, ws.id)) Tf.toast(errText(e), 'error'); return; }
    finally { C.busy = false; restore(); }
    if (stale(root, ws.id)) return;
    const out = r.files || {};
    let n = 0;
    C.cm.operation(() => {
      (r.changed || []).forEach(name => {
        const d = C.docs[name];
        // Skip files edited while the request was in flight; one replaceRange keeps undo a single step.
        if (!d || typeof out[name] !== 'string' || d.getValue() !== files[name]) return;
        const last = d.lastLine();
        d.replaceRange(out[name], { line: 0, ch: 0 }, { line: last, ch: d.getLine(last).length });
        n++;
      });
    });
    C.refreshUI();
    const errs = Object.entries(r.errors || {});
    if (errs.length) showDiag(Tf.t('tf_cfg_fmt_errs', 'Format errors'), 'err', errs.map(([name, msg]) => diagItem('error', msg, '', name, 0)).join(''));
    else hideDiag();
    Tf.toast(n ? Tf.t('tf_cfg_formatted', '{n} file(s) formatted', { n: n }) : Tf.t('tf_cfg_already_fmt', 'Already formatted'), errs.length ? 'error' : 'success');
  };

  C.validate = async function () {
    if (C.busy) return;
    const root = C.root, ws = C.ws, files = {};
    C.names().forEach(n => { files[n] = C.docs[n].getValue(); });
    C.busy = true;
    const btn = document.getElementById('tfCfgValidate');
    const restore = busy(btn, Tf.t('tf_cfg_validating', 'Validating…'));
    let d;
    try { d = await Tf.api.post(wsPath(ws, '/tools/validate'), { files: files }); }
    catch (e) { if (!stale(root, ws.id)) Tf.toast(errText(e), 'error'); return; }
    finally { C.busy = false; restore(); }
    if (stale(root, ws.id)) return;
    if (d.init_error) { showDiag(Tf.t('tf_cfg_init_err', 'terraform init failed'), 'err', '', d.init_error); return; }
    const diags = d.diagnostics || [];
    const e = diags.filter(x => x.severity !== 'warning').length;
    const title = d.ok
      ? Tf.t('tf_cfg_valid_ok', 'Configuration is valid') + (diags.length ? ' — ' + Tf.t('tf_cfg_diag_counts', '{e} error(s), {w} warning(s)', { e: e, w: diags.length - e }) : '')
      : Tf.t('tf_cfg_diag_counts', '{e} error(s), {w} warning(s)', { e: e, w: diags.length - e });
    showDiag(title, d.ok ? 'ok' : 'err', diags.map(x => diagItem(x.severity === 'warning' ? 'warning' : 'error', x.summary || '', x.detail || '', x.file, x.line)).join(''));
  };

  function diagItem(sev, summary, detail, name, line) {
    const loc = name in (C.files || {})
      ? '<button type="button" class="tf-cfg-loc tf-mono" data-goto="' + Tf.esc(name) + '" data-line="' + (Number(line) || 0) + '">' + Tf.esc(name) + (line ? ':' + Number(line) : '') + '</button>'
      : (name ? '<span class="tf-mono">' + Tf.esc(name) + (line ? ':' + Number(line) : '') + '</span>' : '');
    return '<li class="tf-cfg-dg"><div class="tf-cfg-dg-head"><span class="tf-cfg-sev tf-cfg-sev-' + sev + '">' + Tf.esc(Tf.t('tf_cfg_sev_' + sev, sev)) + '</span>' +
      '<span class="tf-cfg-dg-sum">' + Tf.esc(summary) + '</span>' + loc + '</div>' +
      (detail ? '<pre class="tf-cfg-dg-detail">' + Tf.esc(detail) + '</pre>' : '') + '</li>';
  }
  function showDiag(title, cls, items, raw) {
    const el = document.getElementById('tfCfgDiag'); if (!el) return;
    el.innerHTML = '<button type="button" class="tf-cfg-x" data-diag-close aria-label="' + Tf.esc(Tf.t('tf_cfg_diag_close', 'Close')) + '">' + Tf.icon('x', 'icon-xs') + '</button>' +
      '<details open><summary class="tf-cfg-diag-sum ' + cls + '">' + Tf.esc(title) + '</summary>' +
      (raw ? '<pre class="tf-cfg-dg-detail">' + Tf.esc(raw) + '</pre>' : '') + (items ? '<ul class="tf-cfg-dg-list">' + items + '</ul>' : '') + '</details>';
    el.hidden = false;
  }
  function hideDiag() { const el = document.getElementById('tfCfgDiag'); if (el) el.hidden = true; }

  // Rewrites only the version literal on the item's line; refuses when the line no longer holds the checked constraint.
  function applyConstraint(it) {
    const doc = C.docs[it.file], ln = Number(it.line) - 1;
    if (!doc || ln < 0 || ln >= doc.lineCount()) return false;
    const text = doc.getLine(ln);
    const m = /(\bversion\s*=\s*")([^"]*)"/.exec(text) || /^(\s*[A-Za-z0-9_-]+\s*=\s*")([^"]*)"/.exec(text);
    if (!m || m[2] !== it.constraint) return false;
    doc.replaceRange(it.suggested, { line: ln, ch: m.index + m[1].length }, { line: ln, ch: m.index + m[1].length + m[2].length });
    return true;
  }
  const CHIP = { up_to_date: 'badge-healthy', outdated: 'badge-warning', newer_outside_constraint: 'badge-danger', unpinned: 'badge-unknown', unknown: 'badge-unknown' };
  const STATUS_KEY = { up_to_date: 'tf_cfg_st_up_to_date', outdated: 'tf_cfg_st_outdated', newer_outside_constraint: 'tf_cfg_st_newer', unpinned: 'tf_cfg_st_unpinned', unknown: 'tf_cfg_st_unknown' };
  let regItems = [];
  C.registryCheck = async function () {
    if (C.busy) return;
    const root = C.root, ws = C.ws;
    C.busy = true;
    const btn = document.getElementById('tfCfgRegCheck');
    const restore = busy(btn, Tf.t('tf_cfg_checking', 'Checking…'));
    let r;
    try { r = await Tf.api.get(wsPath(ws, '/registry/check')); }
    catch (e) { if (!stale(root, ws.id)) Tf.toast(errText(e), 'error'); return; }
    finally { C.busy = false; restore(); }
    if (stale(root, ws.id)) return;
    const el = document.getElementById('tfCfgRegStatus'); if (!el) return;
    regItems = r.items || [];
    let inner = '<button type="button" class="tf-cfg-x" data-regc-close aria-label="' + Tf.esc(Tf.t('tf_cfg_regcheck_close', 'Close')) + '">' + Tf.icon('x', 'icon-xs') + '</button>';
    if (r.supported === false) inner += '<div class="tf-cfg-note">' + Tf.esc(Tf.t('tf_cfg_regcheck_unsupported', '')) + '</div>';
    else {
      if (C.isDirty()) inner += '<div class="tf-cfg-note">' + Tf.esc(Tf.t('tf_cfg_regcheck_unsaved', '')) + '</div>';
      if (!regItems.length) inner += '<div class="tf-cfg-note">' + Tf.esc(Tf.t('tf_cfg_regcheck_none', '')) + '</div>';
      else inner += '<ul class="tf-cfg-regc-list">' + regItems.map(regRowHTML).join('') + '</ul>';
      (r.errors || []).forEach(x => { inner += '<div class="tf-cfg-err">' + Tf.esc(typeof x === 'string' ? x : JSON.stringify(x)) + '</div>'; });
    }
    el.innerHTML = inner;
    el.hidden = false;
  };
  function regRowHTML(it, i) {
    const fix = (it.status === 'outdated' || it.status === 'newer_outside_constraint') && it.suggested;
    const loc = it.file ? '<span class="tf-mono tf-cfg-regc-loc">' + Tf.esc(it.file) + (it.line ? ':' + Number(it.line) : '') + '</span>' : '';
    return '<li class="tf-cfg-regc-row"><span class="badge ' + (CHIP[it.status] || 'badge-unknown') + '">' + Tf.esc(Tf.t(STATUS_KEY[it.status] || 'tf_cfg_st_unknown', it.status)) + '</span>' +
      '<span class="tf-mono">' + Tf.esc(it.name) + '</span><span class="tf-muted">' + Tf.esc(it.kind || '') + '</span>' +
      '<span class="tf-mono">' + Tf.esc(it.constraint || '—') + '</span>' +
      (it.latest ? '<span class="tf-mono tf-muted">' + Tf.esc(Tf.t('tf_cfg_regcheck_latest', 'latest {v}', { v: it.latest })) + '</span>' : '') + loc +
      (fix ? '<button type="button" class="btn btn-sm" data-regc-apply="' + i + '">' + Tf.esc(Tf.t('tf_cfg_regcheck_apply', 'Use {v}', { v: it.suggested })) + '</button>' : '') + '</li>';
  }

  // Inserts a registry snippet at the cursor, or appends it to a new file when the pick names one.
  function insertSnippet(snippet) {
    if (!snippet || !snippet.text) return;
    if (snippet.file && !(snippet.file in C.files)) { if (!C.add(snippet.file)) return; C.open(snippet.file); }
    else if (snippet.file) C.open(snippet.file);
    C.cm.replaceSelection(snippet.text, 'end');
    C.cm.focus();
  }

  /* ---- git mode ---- */
  function gitShell(ws) {
    const row = (k, v) => '<div class="tf-cfg-gk">' + Tf.esc(Tf.t(k, k)) + '</div><div class="tf-mono">' + Tf.esc(v) + '</div>';
    return '<div class="tf-cfg tf-cfg-git">' +
      '<div class="tf-cfg-gitinfo"><div class="tf-cfg-gitgrid">' +
      row('tf_cfg_git_repo', ws.git_url || '—') + row('tf_cfg_git_branch', ws.git_branch || 'default') +
      row('tf_cfg_git_path', ws.git_path || '/') + '<div class="tf-cfg-gk">' + Tf.esc(Tf.t('tf_cfg_git_commit', 'Commit')) + '</div><div class="tf-mono" id="tfGitCommit">—</div></div>' +
      '<div class="tf-row"><button type="button" id="tfCfgGitTest" class="btn btn-sm">' + Tf.icon('git-compare', 'icon-xs') + ' ' + Tf.esc(Tf.t('tf_cfg_git_test', 'Test VCS Connection')) + '</button>' +
      '<button type="button" id="tfCfgGitSync" class="btn btn-sm">' + Tf.icon('refresh-cw', 'icon-xs') + ' ' + Tf.esc(Tf.t('tf_cfg_git_sync', 'Reload tree')) + '</button>' +
      '<button type="button" id="tfCfgGitEdit" class="btn btn-sm">' + Tf.icon('pencil', 'icon-xs') + ' ' + Tf.esc(Tf.t('tf_cfg_git_edit', 'Edit Git config')) + '</button>' +
      '<span id="tfVcsResult" class="tf-cfg-vcs"></span></div></div>' +
      '<div class="tf-cfg-main"><aside class="tf-cfg-side">' +
      '<input type="search" id="tfGitFilter" class="form-input" placeholder="' + Tf.esc(Tf.t('tf_cfg_git_filter_ph', '')) + '" aria-label="' + Tf.esc(Tf.t('tf_cfg_git_filter_ph', '')) + '" />' +
      '<div id="tfGitTree" class="tf-cfg-list tf-cfg-tree" aria-live="polite"><div class="tf-muted">' + Tf.esc(Tf.t('tf_cfg_git_loading', '')) + '</div></div></aside>' +
      '<section class="tf-cfg-ed"><div class="tf-cfg-bar"><span id="tfGitName" class="tf-cfg-name tf-mono">' + Tf.esc(Tf.t('tf_cfg_git_pick', '')) + '</span>' +
      '<span id="tfGitDirty" class="tf-cfg-dirty" hidden>' + Tf.esc(Tf.t('tf_cfg_unsaved', 'Unsaved')) + '</span></div>' +
      '<div id="tfGitError" class="tf-cfg-err" role="alert" hidden></div>' +
      '<div id="tfGitEditor" class="tf-cfg-edbox"></div>' +
      '<div id="tfGitCommitBar" class="tf-cfg-commit">' +
      '<input type="text" id="tfGitCommitMsg" class="form-input" placeholder="' + Tf.esc(Tf.t('tf_cfg_git_commit_ph', '')) + '" aria-label="' + Tf.esc(Tf.t('tf_cfg_git_commit_ph', '')) + '" />' +
      '<button type="button" id="tfGitCommitBtn" class="btn btn-sm btn-primary" disabled>' + Tf.esc(Tf.t('tf_cfg_git_commit_btn', 'Commit & Push')) + '</button>' +
      '<span id="tfGitChanged" class="tf-muted tf-cfg-changed"></span></div>' +
      '</section></div></div>';
  }
  function treeFiles() {
    const g = C.git, q = (document.getElementById('tfGitFilter') || {}).value || '';
    const needle = q.trim().toLowerCase();
    return (g.files || []).filter(f => !needle || f.path.toLowerCase().includes(needle));
  }
  function renderTree() {
    const g = C.git, el = document.getElementById('tfGitTree');
    if (!el || !g) return;
    const prefix = (C.ws.git_path || '').replace(/^\/+|\/+$/g, '');
    const dirtySet = new Set(C.gitDirty());
    if (!g.files.length) { el.innerHTML = '<div class="tf-muted">' + Tf.esc(Tf.t('tf_cfg_git_empty', '')) + '</div>'; return; }
    el.innerHTML = treeFiles().map(f => {
      const active = prefix && (f.path === prefix || f.path.startsWith(prefix + '/'));
      return '<div class="tf-cfg-frow' + (f.path === g.cur ? ' active' : '') + (dirtySet.has(f.path) ? ' dirty' : '') + (active ? ' tf-cfg-wd' : '') + '">' +
        '<button type="button" class="tf-cfg-fbtn" data-gfile="' + Tf.esc(f.path) + '" title="' + Tf.esc(f.path) + '"><i class="tf-cfg-dot"></i><span class="tf-cfg-fname">' + Tf.esc(f.path) + '</span></button></div>';
    }).join('') + (g.truncated ? '<div class="tf-cfg-note">' + Tf.esc(Tf.t('tf_cfg_git_truncated', '')) + '</div>' : '');
  }
  function renderGitBar() {
    const g = C.git, dirty = C.gitDirty();
    const commitEl = document.getElementById('tfGitCommit'); if (commitEl && g) commitEl.textContent = (g.commit || '').slice(0, 12) || '—';
    const d = document.getElementById('tfGitDirty'); if (d) d.hidden = !dirty.length;
    const ch = document.getElementById('tfGitChanged');
    if (ch) ch.textContent = dirty.length ? Tf.t('tf_cfg_git_changed', '{n} file(s) changed', { n: dirty.length }) : Tf.t('tf_cfg_git_none', 'No changes to commit');
    const btn = document.getElementById('tfGitCommitBtn'); if (btn) btn.disabled = !dirty.length;
    setGuard(canLeave);
  }
  C.gitLoadTree = async function () {
    const root = C.root, ws = C.ws;
    let r;
    try { r = await Tf.api.get(wsPath(ws, '/git/files')); }
    catch (e) { if (!stale(root, ws.id)) showErr('tfGitError', errText(e)); return; }
    if (stale(root, ws.id)) return;
    const prev = C.git;
    C.git = { commit: r.commit || '', branch: r.branch || '', files: r.files || [], truncated: !!r.truncated, cur: prev ? prev.cur : '', docs: prev ? prev.docs : {}, saved: prev ? prev.saved : {}, cm: prev ? prev.cm : null };
    renderTree();
    renderGitBar();
  };
  C.gitOpen = async function (path) {
    const root = C.root, ws = C.ws, g = C.git;
    if (!g) return;
    showErr('tfGitError', '');
    if (!g.docs[path]) {
      let r;
      try { r = await Tf.api.get(wsPath(ws, '/git/file?path=' + encodeURIComponent(path))); }
      catch (e) { if (!stale(root, ws.id)) showErr('tfGitError', errText(e)); return; }
      if (stale(root, ws.id)) return;
      g.saved[path] = r.content || '';
      g.docs[path] = CodeMirror.Doc(r.content || '', C.modeFor(path));
      g.docs[path].on('change', () => renderGitBar());
      if (r.commit) g.commit = r.commit;
    }
    g.cur = path;
    if (g.cm) g.cm.swapDoc(g.docs[path]);
    const name = document.getElementById('tfGitName'); if (name) name.textContent = path;
    renderTree();
    renderGitBar();
  };
  C.gitCommit = async function () {
    const root = C.root, ws = C.ws, g = C.git;
    const msgEl = document.getElementById('tfGitCommitMsg');
    const message = (msgEl && msgEl.value || '').trim();
    const dirty = C.gitDirty();
    if (!message) { showErr('tfGitError', Tf.t('tf_cfg_git_msg_req', 'Enter a commit message')); return; }
    if (!dirty.length) return;
    const changes = dirty.map(p => ({ path: p, content: g.docs[p].getValue() }));
    const btn = document.getElementById('tfGitCommitBtn');
    const restore = busy(btn, Tf.t('tf_cfg_git_committing', 'Pushing…'));
    showErr('tfGitError', '');
    try {
      const r = await Tf.api.post(wsPath(ws, '/git/commit'), { message: message, base_commit: g.commit, changes: changes });
      if (stale(root, ws.id)) return;
      changes.forEach(c => { g.saved[c.path] = c.content; });
      g.commit = r.commit || g.commit;
      if (r.branch) g.branch = r.branch;
      if (msgEl) msgEl.value = '';
      const c = (r.commit || '').slice(0, 10);
      Tf.toast(Tf.t('tf_cfg_git_committed', 'Committed and pushed {c}', { c: c }), 'success');
      const cEl = document.getElementById('tfGitCommit'); if (cEl) cEl.textContent = c || '—';
      renderTree();
      renderGitBar();
    } catch (e) {
      if (stale(root, ws.id)) return;
      let msg = errText(e);
      if (e.status === 409) msg = Tf.t('tf_cfg_git_conflict', 'The repository changed since it was opened.') + ' ' + msg;
      if (e.status === 502) msg = Tf.t('tf_cfg_git_readonly', 'Push failed.') + ' ' + msg;
      showErr('tfGitError', msg);
    } finally {
      restore();
      renderGitBar();
    }
  };
  C.gitTest = async function () {
    const root = C.root, ws = C.ws;
    const out = document.getElementById('tfVcsResult'); if (!out) return;
    const btn = document.getElementById('tfCfgGitTest');
    const restore = busy(btn, Tf.t('tf_cfg_git_testing', 'Testing…'));
    out.className = 'tf-cfg-vcs';
    out.textContent = Tf.t('tf_cfg_git_testing', 'Testing…');
    try {
      const d = await Tf.api.post(wsPath(ws, '/vcs/test'));
      if (stale(root, ws.id)) return;
      out.classList.add(d.ok ? 'ok' : 'err');
      out.textContent = d.ok ? Tf.t('tf_cfg_git_ok', 'Connection OK') + ' · ' + (d.branch || '') + ' ' + (d.commit || '').slice(0, 10) : (d.error || 'failed');
    } catch (e) {
      if (stale(root, ws.id)) return;
      out.classList.add('err');
      out.textContent = errText(e);
    } finally { restore(); }
  };
  C.gitSync = async function () {
    if (C.gitDirty().length && !(await Tf.confirm({ title: Tf.t('tf_cfg_git_sync', 'Reload tree'), message: Tf.t('tf_cfg_git_unsaved_reload', ''), confirmText: Tf.t('tf_cfg_git_sync', 'Reload tree'), danger: true }))) return;
    if (C.git) { C.git.docs = {}; C.git.saved = {}; C.git.cur = ''; }
    await C.gitLoadTree();
  };
  C.gitInit = function () {
    C.git = { commit: '', branch: '', files: [], truncated: false, cur: '', docs: {}, saved: {}, cm: null };
    if (typeof CodeMirror !== 'undefined') {
      C.git.cm = CodeMirror(document.getElementById('tfGitEditor'), {
        lineNumbers: true, theme: 'vscode-dark', indentUnit: 2, tabSize: 2, indentWithTabs: false,
        extraKeys: { Tab: cm => cm.replaceSelection('  ', 'end') },
      });
      C.git.cm.setOption('readOnly', false);
    }
    C.gitLoadTree();
  };

  /* ---- shared render + wiring ---- */
  function onClick(e) {
    const t = e.target;
    const act = (sel) => t.closest(sel);
    let el;
    if ((el = act('[data-file]'))) return C.open(el.dataset.file);
    if ((el = act('[data-del]'))) return deleteFile(el.dataset.del);
    if ((el = act('[data-gfile]'))) return C.gitOpen(el.dataset.gfile);
    if ((el = act('[data-goto]'))) {
      C.open(el.dataset.goto);
      const line = Number(el.dataset.line);
      if (line > 0) C.cm.setCursor({ line: line - 1, ch: 0 });
      C.cm.scrollIntoView(null, 120);
      C.cm.focus();
      return;
    }
    if ((el = act('[data-diag-close]'))) return hideDiag();
    if ((el = act('[data-regc-close]'))) { const s = document.getElementById('tfCfgRegStatus'); if (s) s.hidden = true; return; }
    if ((el = act('[data-regc-apply]'))) {
      const it = regItems[Number(el.dataset.regcApply)];
      if (!it || !applyConstraint(it)) { Tf.toast(Tf.t('tf_cfg_regcheck_stale', ''), 'error'); return; }
      el.disabled = true; el.textContent = Tf.t('tf_cfg_regcheck_applied', 'Applied');
      C.refreshUI();
      return;
    }
    const id = t.closest('[id]') ? t.closest('[id]').id : '';
    if (!id) return;
    const actions = {
      tfCfgAddFile: () => { const inp = document.getElementById('tfCfgNewName'); if (C.add(inp.value)) inp.value = ''; },
      tfCfgFmt: () => C.format(),
      tfCfgValidate: () => C.validate(),
      tfCfgRegCheck: () => C.registryCheck(),
      tfCfgRegistry: () => {
        if (Tf.registry && Tf.registry.pick) Tf.registry.pick({ onPick: function (sn, mode) { insertSnippet({ text: [sn.main, sn.variables].filter(Boolean).join('\n\n'), file: mode === 'file' ? sn.file : undefined }); } });
        else Tf.toast(Tf.t('tf_cfg_registry_missing', 'Registry browser is not available'), 'error');
      },
      tfCfgDetect: () => { if (Tf.variables && Tf.variables.detect) Tf.variables.detect(C.ws); },
      tfCfgDiscard: async () => { if (await Tf.confirm({ title: Tf.t('tf_cfg_discard', 'Discard'), message: Tf.t('tf_cfg_discard_msg', ''), confirmText: Tf.t('tf_cfg_discard_ok', 'Discard'), danger: true })) C.discard(); },
      tfCfgSave: () => C.save(false),
      tfCfgSavePlan: () => C.save(true),
      tfCfgGitTest: () => C.gitTest(),
      tfCfgGitSync: () => C.gitSync(),
      tfCfgGitEdit: () => { if (C.ctx && C.ctx.goto) C.ctx.goto('settings'); },
      tfGitCommitBtn: () => C.gitCommit(),
    };
    if (actions[id]) actions[id]();
  }
  async function deleteFile(n) {
    if (!(await Tf.confirm({ title: Tf.t('tf_cfg_del_title', 'Delete file'), message: Tf.t('tf_cfg_del_msg', '', { name: n }), confirmText: Tf.t('tf_cfg_delete', 'Delete'), danger: true }))) return;
    C.remove(n);
  }
  function onInput(e) {
    if (e.target.id === 'tfGitFilter') renderTree();
  }
  function onKey(e) {
    if (e.target.id === 'tfCfgNewName' && e.key === 'Enter') {
      e.preventDefault();
      const inp = e.target; if (C.add(inp.value)) inp.value = '';
      return;
    }
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's' && C.mode === 'git') {
      e.preventDefault();
      C.gitCommit();
    }
  }

  C.render = function (body, ws, ctx) {
    if (C.observer) C.observer.disconnect();
    C.ws = ws; C.ctx = ctx || {};
    C.mode = ws.source === 'git' ? 'git' : 'inline';
    body.innerHTML = C.mode === 'git' ? gitShell(ws) : inlineShell();
    C.root = body.firstElementChild;
    C.root.addEventListener('click', onClick);
    C.root.addEventListener('input', onInput);
    C.root.addEventListener('keydown', onKey);
    // Clears the guard and state when the drawer body is replaced or removed.
    C.observer = new MutationObserver(() => { if (C.root && !C.root.isConnected) reset(); });
    C.observer.observe(body, { childList: true });
    if (C.mode === 'git') C.gitInit(); else C.inlineInit();
  };

  Tf.registerWsTab({
    id: 'config', icon: 'file-code', labelKey: 'tf_ws_tab_config', fallback: 'Configuration', order: 2,
    render(body, ws, ctx) { C.render(body, ws, ctx); },
  });
})();
