/* Terraform Modules & Registry: private module catalog (versions, README, inputs/outputs) and the public
   Terraform Registry browser. Registry text is untrusted: README goes through Tf.registry.md (escaped, http(s) links only). */
(function () {
  'use strict';
  const Tf = window.Tf;
  const PAGE = 20;
  const MOD_NAME_RE = /^[A-Za-z_][A-Za-z0-9_-]*$/;

  Tf.addI18n({
    en: {
      tf_tab_registry: 'Modules & Registry', tf_reg_seg_private: 'Private Catalog', tf_reg_seg_public: 'Public Registry',
      tf_reg_modules: 'Modules', tf_reg_providers: 'Providers', tf_reg_search_ph: 'Search modules or providers…',
      tf_reg_provider_ph: 'Provider (e.g. aws)', tf_reg_search: 'Search', tf_reg_more: 'Load more',
      tf_reg_enter_query: 'Enter a search term', tf_reg_none: 'No results', tf_reg_select_hint: 'Select a result to see its details',
      tf_reg_loading: 'Loading…', tf_reg_searching: 'Searching…', tf_reg_verified: 'Verified', tf_reg_downloads: '{n} downloads',
      tf_reg_version: 'Version', tf_reg_inputs: 'Inputs', tf_reg_outputs: 'Outputs', tf_reg_readme: 'README', tf_reg_snippet: 'Snippet',
      tf_reg_required: 'required', tf_reg_no_inputs: 'No inputs', tf_reg_no_outputs: 'No outputs', tf_reg_no_readme: 'No README',
      tf_reg_col_name: 'Name', tf_reg_col_type: 'Type', tf_reg_col_desc: 'Description', tf_reg_col_default: 'Default',
      tf_reg_mod_name: 'Module name', tf_reg_declare_vars: 'Declare variables in variables.tf',
      tf_reg_insert: 'Insert at cursor', tf_reg_add_file: 'Add as file', tf_reg_copy: 'Copy', tf_reg_copy_done: 'Copied',
      tf_reg_inserted: 'Snippet inserted', tf_reg_file_added: 'Added {name}', tf_reg_open: 'Open in registry', tf_reg_docs: 'Docs',
      tf_reg_deps: 'Requires', tf_reg_readme_raw: 'Raw', tf_reg_readme_rendered: 'Rendered', tf_reg_truncated: 'README was truncated.',
      tf_reg_pick_title: 'Insert from Terraform Registry', tf_reg_pick_hint: 'Pick a module or provider, then insert it into the configuration.',
      tf_cat_search_ph: 'Search private modules…', tf_cat_provider_ph: 'Provider', tf_cat_new: 'New Module', tf_cat_empty: 'No private modules yet',
      tf_cat_empty_desc: 'Create a module with one or more versions, then reuse it across workspaces.', tf_cat_latest: 'latest {v}',
      tf_cat_nversions: '{n} versions', tf_cat_title_new: 'New Module', tf_cat_title_edit: 'Edit Module', tf_cat_name: 'Name',
      tf_cat_provider: 'Provider', tf_cat_desc: 'Description', tf_cat_name_required: 'Name and provider are required',
      tf_cat_add_version: 'Add Version', tf_cat_title_version: 'Add Version', tf_cat_ver_num: 'Version', tf_cat_src: 'Source',
      tf_cat_src_inline: 'Inline files', tf_cat_src_git: 'Git repository', tf_cat_git_url: 'Git URL', tf_cat_git_ref: 'Ref (tag or branch)',
      tf_cat_git_path: 'Path in repository', tf_cat_git_auth: 'Authentication', tf_cat_auth_none: 'None', tf_cat_auth_token: 'Token',
      tf_cat_auth_ssh: 'SSH key', tf_cat_git_token: 'Token', tf_cat_ssh_key: 'SSH key', tf_cat_usage: 'Usage snippet', tf_cat_copy: 'Copy',
      tf_cat_inputs: 'Inputs', tf_cat_outputs: 'Outputs', tf_cat_resources: 'Resources', tf_cat_providers: 'Providers',
      tf_cat_no_versions: 'This module has no versions yet. Add one to see its inputs and outputs.', tf_cat_saving: 'Saving…',
      tf_cat_delete_version: 'Delete version {v}?', tf_cat_delete_module: 'Delete module {n} and all its versions?',
      tf_cat_required: 'required', tf_cat_sensitive: 'sensitive', tf_cat_edit: 'Edit', tf_cat_delete: 'Delete', tf_save: 'Save',
      tf_cat_version_req: 'Version is required', tf_cat_deleted: 'Deleted', tf_cat_saved: 'Saved',
    },
    id: {
      tf_tab_registry: 'Modul & Registry', tf_reg_seg_private: 'Katalog Privat', tf_reg_seg_public: 'Registry Publik',
      tf_reg_modules: 'Modul', tf_reg_providers: 'Provider', tf_reg_search_ph: 'Cari modul atau provider…',
      tf_reg_provider_ph: 'Provider (mis. aws)', tf_reg_search: 'Cari', tf_reg_more: 'Muat lebih banyak',
      tf_reg_enter_query: 'Masukkan kata kunci', tf_reg_none: 'Tidak ada hasil', tf_reg_select_hint: 'Pilih hasil untuk melihat detailnya',
      tf_reg_loading: 'Memuat…', tf_reg_searching: 'Mencari…', tf_reg_verified: 'Terverifikasi', tf_reg_downloads: '{n} unduhan',
      tf_reg_version: 'Versi', tf_reg_inputs: 'Input', tf_reg_outputs: 'Output', tf_reg_readme: 'README', tf_reg_snippet: 'Cuplikan',
      tf_reg_required: 'wajib', tf_reg_no_inputs: 'Tidak ada input', tf_reg_no_outputs: 'Tidak ada output', tf_reg_no_readme: 'Tidak ada README',
      tf_reg_col_name: 'Nama', tf_reg_col_type: 'Tipe', tf_reg_col_desc: 'Deskripsi', tf_reg_col_default: 'Default',
      tf_reg_mod_name: 'Nama modul', tf_reg_declare_vars: 'Deklarasikan variabel di variables.tf',
      tf_reg_insert: 'Sisipkan di kursor', tf_reg_add_file: 'Tambahkan sebagai file', tf_reg_copy: 'Salin', tf_reg_copy_done: 'Disalin',
      tf_reg_inserted: 'Cuplikan disisipkan', tf_reg_file_added: '{name} ditambahkan', tf_reg_open: 'Buka di registry', tf_reg_docs: 'Dokumentasi',
      tf_reg_deps: 'Membutuhkan', tf_reg_readme_raw: 'Mentah', tf_reg_readme_rendered: 'Tampilan', tf_reg_truncated: 'README dipotong.',
      tf_reg_pick_title: 'Sisipkan dari Terraform Registry', tf_reg_pick_hint: 'Pilih modul atau provider, lalu sisipkan ke konfigurasi.',
      tf_cat_search_ph: 'Cari modul privat…', tf_cat_provider_ph: 'Provider', tf_cat_new: 'Modul Baru', tf_cat_empty: 'Belum ada modul privat',
      tf_cat_empty_desc: 'Buat modul dengan satu atau lebih versi, lalu gunakan ulang di seluruh workspace.', tf_cat_latest: 'terbaru {v}',
      tf_cat_nversions: '{n} versi', tf_cat_title_new: 'Modul Baru', tf_cat_title_edit: 'Ubah Modul', tf_cat_name: 'Nama',
      tf_cat_provider: 'Provider', tf_cat_desc: 'Deskripsi', tf_cat_name_required: 'Nama dan provider wajib diisi',
      tf_cat_add_version: 'Tambah Versi', tf_cat_title_version: 'Tambah Versi', tf_cat_ver_num: 'Versi', tf_cat_src: 'Sumber',
      tf_cat_src_inline: 'File inline', tf_cat_src_git: 'Repositori Git', tf_cat_git_url: 'URL Git', tf_cat_git_ref: 'Ref (tag atau branch)',
      tf_cat_git_path: 'Path di repositori', tf_cat_git_auth: 'Autentikasi', tf_cat_auth_none: 'Tidak ada', tf_cat_auth_token: 'Token',
      tf_cat_auth_ssh: 'Kunci SSH', tf_cat_git_token: 'Token', tf_cat_ssh_key: 'Kunci SSH', tf_cat_usage: 'Cuplikan pemakaian', tf_cat_copy: 'Salin',
      tf_cat_inputs: 'Input', tf_cat_outputs: 'Output', tf_cat_resources: 'Resource', tf_cat_providers: 'Provider',
      tf_cat_no_versions: 'Modul ini belum punya versi. Tambahkan satu untuk melihat input dan output-nya.', tf_cat_saving: 'Menyimpan…',
      tf_cat_delete_version: 'Hapus versi {v}?', tf_cat_delete_module: 'Hapus modul {n} beserta semua versinya?',
      tf_cat_required: 'wajib', tf_cat_sensitive: 'sensitif', tf_cat_edit: 'Ubah', tf_cat_delete: 'Hapus', tf_save: 'Simpan',
      tf_cat_version_req: 'Versi wajib diisi', tf_cat_deleted: 'Dihapus', tf_cat_saved: 'Tersimpan',
    },
  });

  /* ---------- shared helpers ---------- */
  const enc = encodeURIComponent;
  const pathOf = (id) => String(id).split('/').map(enc).join('/');
  const reg = (p) => Tf.api.get('/api/terraform/registry/' + p);
  // Registry data is untrusted: only http(s) URLs may become links.
  const safeUrl = (u) => (/^https?:\/\//i.test(u || '') ? u : '');
  const num = (n) => (Number(n) || 0).toLocaleString();
  const vstr = (v) => (v && typeof v === 'object' ? v.version || v.name || '' : String(v == null ? '' : v));
  const cell = (v) => (v == null ? '' : typeof v === 'string' ? v : JSON.stringify(v));
  const defName = (n) => String(n || 'module').replace(/[^A-Za-z0-9_-]/g, '_').replace(/^[^A-Za-z_]/, '_$&');
  const parseDoc = (d) => { if (!d) return {}; if (typeof d === 'object') return d; try { return JSON.parse(d) || {}; } catch (e) { return {}; } };
  const esc = Tf.esc;
  const icon = Tf.icon;

  /* ---------- Markdown: safe renderer for untrusted README text ----------
     Everything is HTML-escaped first; only elements built here reach the DOM. Raw HTML is dropped, images are
     shown as alt text, links are limited to http(s). Ported from the legacy 64-registry-md.js. */
  const URL_RE = /^https?:\/\/[^\s"'<>]+$/i;
  function inline(s0) {
    const codes = [];
    let s = s0.replace(/`([^`]+)`/g, (m, c) => { codes.push(c); return '\u0000' + (codes.length - 1) + '\u0000'; });
    s = s.replace(/!\[([^\]]*)\]\(([^)\s]*)[^)]*\)/g, (m, alt) => (alt ? `<span class="tf-md-alt">${alt}</span>` : ''));
    s = s.replace(/\[([^\]]+)\]\(([^)\s]+)[^)]*\)/g, (m, text, url) => {
      const raw = url.replace(/&amp;/g, '&');
      return URL_RE.test(raw) ? `<a href="${url}" target="_blank" rel="noopener noreferrer">${text}</a>` : text;
    });
    s = s.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>').replace(/(^|[\s(])_([^_]+)_(?=[\s).,;:!?]|$)/g, '$1<em>$2</em>');
    return s.replace(/\u0000(\d+)\u0000/g, (m, i) => `<code>${codes[Number(i)]}</code>`);
  }
  // Drops HTML tags outside code spans; the odd split segments are the code spans themselves.
  const stripHtml = (line) => line.split(/(`[^`]*`)/).map((p, i) => (i % 2 ? p : p.replace(/<\/?[A-Za-z][^>]*>/g, '').replace(/<!--.*?-->/g, ''))).join('');
  function mdTable(rows) {
    const cells = (r) => r.replace(/^\||\|$/g, '').split('|').map(c => inline(esc(stripHtml(c.trim()))));
    const head = cells(rows[0]);
    const body = rows.slice(2).map(cells);
    return `<table class="tf-md-table"><thead><tr>${head.map(c => `<th>${c}</th>`).join('')}</tr></thead><tbody>`
      + body.map(r => `<tr>${r.map(c => `<td>${c}</td>`).join('')}</tr>`).join('') + '</tbody></table>';
  }
  function md(src) {
    const out = [];
    const lines = String(src || '').replace(/<!--[\s\S]*?-->/g, '').split('\n');
    let fence = null, list = null, para = [];
    const flushPara = () => { if (para.length) { out.push(`<p>${para.join(' ')}</p>`); para = []; } };
    const closeList = () => { if (list) { out.push(`</${list}>`); list = null; } };
    for (let i = 0; i < lines.length; i++) {
      const line = lines[i];
      if (fence !== null) {
        if (/^\s*```/.test(line)) { out.push(`<pre class="tf-reg-pre">${esc(fence.join('\n'))}</pre>`); fence = null; } else fence.push(line);
        continue;
      }
      if (/^\s*```/.test(line)) { flushPara(); closeList(); fence = []; continue; }
      if (/^\s*\|/.test(line) && /^\s*\|?[\s:|-]+\|[\s:|-]*$/.test(lines[i + 1] || '')) {
        flushPara(); closeList();
        const rows = [line]; while (i + 1 < lines.length && /^\s*\|/.test(lines[i + 1])) rows.push(lines[++i]);
        out.push(mdTable(rows)); continue;
      }
      const clean = stripHtml(line);
      const h = /^(#{1,6})\s+(.*)$/.exec(clean);
      const li = /^\s*([-*+]|\d+\.)\s+(.*)$/.exec(clean);
      if (h) { flushPara(); closeList(); out.push(`<div class="tf-md-h tf-md-h${h[1].length}">${inline(esc(h[2]))}</div>`); }
      else if (li) {
        flushPara(); const tag = /\d/.test(li[1]) ? 'ol' : 'ul';
        if (list !== tag) { closeList(); out.push(`<${tag}>`); list = tag; }
        out.push(`<li>${inline(esc(li[2]))}</li>`);
      } else if (/^\s*>/.test(clean)) { flushPara(); closeList(); out.push(`<blockquote>${inline(esc(clean.replace(/^\s*>\s?/, '')))}</blockquote>`); }
      else if (/^\s*([-*_])\1{2,}\s*$/.test(clean)) { flushPara(); closeList(); out.push('<hr>'); }
      else if (!clean.trim()) { flushPara(); closeList(); }
      else para.push(inline(esc(clean.trim())));
    }
    if (fence !== null) out.push(`<pre class="tf-reg-pre">${esc(fence.join('\n'))}</pre>`); // truncated README: unclosed fence
    flushPara(); closeList();
    return out.join('');
  }
  Tf.registry = { md: md };

  /* ---------- public registry browser (tab segment and the insert dialog) ---------- */
  const R = Tf.registry;
  R.st = { kind: 'modules', q: '', provider: '' };
  R.next = 0; R.seq = 0; R.dseq = 0; R.base = ''; R.wrap = null; R.pickCb = null; R.d = null;

  R.loadBase = async function () {
    if (R.base) return R.base;
    try {
      const s = await reg('settings');
      R.base = safeUrl(s.base_url) || safeUrl(s.default_url) || 'https://registry.terraform.io';
    } catch (e) { R.base = 'https://registry.terraform.io'; }
    return R.base;
  };
  const q = (sel) => (R.wrap ? R.wrap.querySelector(sel) : null);

  function kindBtn(k) {
    return `<button type="button" class="btn btn-sm${R.st.kind === k ? ' btn-primary' : ''}" data-reg-kind="${k}" aria-pressed="${R.st.kind === k}">${esc(Tf.t(k === 'modules' ? 'tf_reg_modules' : 'tf_reg_providers', k))}</button>`;
  }
  // Mounts the browser into host. A fresh wrapper per mount keeps listeners from stacking on reused hosts.
  R.mountPublic = function (host, pick) {
    host.innerHTML = '';
    const wrap = document.createElement('div');
    wrap.className = 'tf-reg';
    host.appendChild(wrap);
    R.wrap = wrap; R.pick = !!pick; R.dseq++;
    const s = R.st;
    wrap.innerHTML = `<div class="tf-reg-bar">
      <div class="tf-reg-kinds" role="group">${kindBtn('modules')}${kindBtn('providers')}</div>
      <input type="text" id="tfRegQuery" class="form-input" placeholder="${esc(Tf.t('tf_reg_search_ph', 'Search modules or providers…'))}" aria-label="${esc(Tf.t('tf_reg_search', 'Search'))}" value="${esc(s.q)}" maxlength="100" />
      <input type="text" id="tfRegProvider" class="form-input" placeholder="${esc(Tf.t('tf_reg_provider_ph', 'Provider (e.g. aws)'))}" value="${esc(s.provider)}" maxlength="64" ${s.kind === 'modules' ? '' : 'hidden'} />
      <button type="button" id="tfRegSearch" class="btn btn-sm btn-primary">${icon('search', 'icon-xs')} ${esc(Tf.t('tf_reg_search', 'Search'))}</button></div>
      <div class="tf-reg-body"><div class="tf-reg-list"><div id="tfRegResults" class="tf-reg-results" aria-live="polite"><div class="tf-reg-hint">${esc(Tf.t('tf_reg_enter_query', 'Enter a search term'))}</div></div>
        <button type="button" id="tfRegMore" class="btn btn-sm" hidden>${esc(Tf.t('tf_reg_more', 'Load more'))}</button></div>
      <div id="tfRegDetail" class="tf-reg-detail"><div class="tf-reg-hint">${esc(Tf.t('tf_reg_select_hint', 'Select a result to see its details'))}</div></div></div>`;
    wrap.addEventListener('click', onPublicClick);
    wrap.addEventListener('change', onPublicChange);
    wrap.addEventListener('input', onPublicInput);
    wrap.addEventListener('keydown', (e) => { if (e.key === 'Enter' && e.target.matches('#tfRegQuery, #tfRegProvider')) { e.preventDefault(); R.search(false); } });
    if (s.q) R.search(false);
  };

  function rowHTML(kind, x) {
    const id = kind === 'modules' ? `${x.namespace}/${x.name}/${x.provider}` : `${x.namespace}/${x.name}`;
    const badge = kind === 'modules'
      ? (x.verified ? `<span class="badge badge-healthy">${esc(Tf.t('tf_reg_verified', 'Verified'))}</span>` : '')
      : (x.tier ? `<span class="badge badge-unknown">${esc(x.tier)}</span>` : '');
    const meta = [kind === 'modules' && x.version ? 'v' + x.version : '', x.downloads ? Tf.t('tf_reg_downloads', '{n} downloads', { n: num(x.downloads) }) : ''].filter(Boolean).join(' · ');
    return `<button type="button" class="tf-reg-row" data-reg-item="${esc(id)}"><span class="tf-reg-rname tf-mono">${esc(id)}</span>${badge}
      <span class="tf-reg-rdesc">${esc(x.description || '')}</span><span class="tf-reg-rmeta">${esc(meta)}</span></button>`;
  }
  R.search = async function (append) {
    const st = R.st, box = q('#tfRegResults'), more = q('#tfRegMore');
    if (!box || !more) return;
    if (!append) { st.q = (q('#tfRegQuery').value || '').trim(); st.provider = (q('#tfRegProvider').value || '').trim(); R.next = 0; }
    if (!st.q) { box.innerHTML = `<div class="tf-reg-hint">${esc(Tf.t('tf_reg_enter_query', 'Enter a search term'))}</div>`; more.hidden = true; return; }
    const seq = ++R.seq, wrap = R.wrap, kind = st.kind;
    if (!append) box.innerHTML = Tf.ui.skeleton(3);
    const qs = 'q=' + enc(st.q);
    more.disabled = true;
    let r;
    try {
      r = kind === 'modules'
        ? await reg(`modules?${qs}&provider=${enc(st.provider)}&offset=${R.next}&limit=${PAGE}`)
        : await reg(`providers?${qs}&page=${R.next || 1}`);
    } catch (e) {
      if (seq === R.seq && R.wrap === wrap) { box.innerHTML = Tf.ui.error(Tf.errMsg(e)); more.hidden = true; }
      return;
    } finally { more.disabled = false; }
    if (seq !== R.seq || R.wrap !== wrap) return; // superseded by a newer search or a re-mount
    const list = (kind === 'modules' ? r.modules : r.providers) || [];
    if (!append) box.innerHTML = '';
    box.insertAdjacentHTML('beforeend', list.map(x => rowHTML(kind, x)).join(''));
    if (!box.children.length) box.innerHTML = Tf.ui.empty({ icon: 'search', title: Tf.t('tf_reg_none', 'No results') });
    R.next = kind === 'modules' ? Number(r.next_offset) || 0 : Number(r.next_page) || 0;
    more.hidden = !R.next || !list.length;
  };

  function onPublicClick(e) {
    const st = R.st;
    const k = e.target.closest('[data-reg-kind]');
    if (k) { if (k.dataset.regKind !== st.kind) { st.kind = k.dataset.regKind; R.mountPublic(R.wrap.parentElement, R.pick); } return; }
    if (e.target.closest('#tfRegSearch')) { R.search(false); return; }
    if (e.target.closest('#tfRegMore')) { R.search(true); return; }
    const it = e.target.closest('[data-reg-item]');
    if (it) {
      R.wrap.querySelectorAll('.tf-reg-row.active').forEach(x => x.classList.remove('active'));
      it.classList.add('active'); R.openItem(st.kind, it.dataset.regItem); return;
    }
    if (e.target.closest('[data-reg-back]')) { const body = q('.tf-reg-body'); if (body) body.classList.remove('show-detail'); return; }
    const tb = e.target.closest('[data-reg-tab]');
    if (tb) { showPane(tb.dataset.regTab); return; }
    const clamp = e.target.closest('.tf-reg-clamp');
    if (clamp) { clamp.classList.toggle('open'); return; }
    if (e.target.closest('#tfRegCopy')) { Tf.copy(snippetText()); return; }
    if (e.target.closest('#tfRegInsert')) { pickAct('insert'); return; }
    if (e.target.closest('#tfRegAddFile')) { pickAct('file'); return; }
    if (e.target.closest('#tfRegReadmeToggle')) { toggleRaw(); }
  }
  function onPublicChange(e) {
    if (e.target.id === 'tfRegVersion') R.openItem(R.d.kind, R.d.id, opts());
    else if (e.target.id === 'tfRegDeclareVars') snippetUI();
  }
  let tmr = 0;
  function onPublicInput(e) { // module name changes the snippet: refetch it (debounced), keep the rest of the view
    if (e.target.id !== 'tfRegModName') return;
    snippetUI(); clearTimeout(tmr);
    if (MOD_NAME_RE.test(e.target.value)) tmr = setTimeout(() => R.refreshSnippet(opts()), 300);
  }
  const opts = () => ({ version: (q('#tfRegVersion') || {}).value || '', modName: (q('#tfRegModName') || {}).value || '', declare: !!(q('#tfRegDeclareVars') || {}).checked });

  const snipOf = (s) => (typeof s === 'string' ? { main: s, variables: '' } : { main: (s && s.main) || '', variables: (s && s.variables) || '' });
  function table(id, cols, rows, empty) {
    if (!rows.length) return `<div id="${id}" class="tf-reg-pane" hidden>${Tf.ui.empty({ title: empty })}</div>`;
    return `<div id="${id}" class="tf-reg-pane" hidden><div class="tf-reg-tablewrap"><table class="tf-reg-table"><thead><tr>${cols.map(c => `<th>${esc(Tf.t(c, c))}</th>`).join('')}</tr></thead>
      <tbody>${rows.join('')}</tbody></table></div></div>`;
  }
  function inputRow(i) {
    const type = cell(i.type).replace(/\s+/g, ' ');
    const dflt = cell(i.default).replace(/\s+/g, ' ');
    const clamp = (txt) => `<div class="tf-reg-clamp tf-mono" title="${esc(txt)}">${esc(txt)}</div>`;
    return `<tr><td class="tf-mono tf-reg-name">${esc(i.name)}${i.required ? ` <span class="badge badge-errored">${esc(Tf.t('tf_reg_required', 'required'))}</span>` : ''}</td>
      <td>${clamp(type)}</td><td>${esc(i.description || '')}</td><td>${clamp(dflt)}</td></tr>`;
  }
  function paneHTML(d, isMod) {
    const snip = `<div id="tfRegSnipPane" class="tf-reg-pane" hidden><pre id="tfRegSnippet" class="tf-reg-pre"></pre></div>`;
    if (!isMod) return snip;
    const ins = (d.inputs || []).map(inputRow);
    const outs = (d.outputs || []).map(o => `<tr><td class="tf-mono">${esc(o.name)}</td><td>${esc(o.description || '')}</td></tr>`);
    const readme = d.readme
      ? `<div class="tf-reg-readmebar"><button type="button" id="tfRegReadmeToggle" class="btn btn-sm">${esc(Tf.t('tf_reg_readme_raw', 'Raw'))}</button></div>
         <div id="tfRegReadme" class="tf-md">${md(d.readme)}</div><pre id="tfRegReadmeRaw" class="tf-reg-pre" hidden>${esc(d.readme)}</pre>`
      : Tf.ui.empty({ title: Tf.t('tf_reg_no_readme', 'No README') });
    return table('tfRegInputs', ['tf_reg_col_name', 'tf_reg_col_type', 'tf_reg_col_desc', 'tf_reg_col_default'], ins, Tf.t('tf_reg_no_inputs', 'No inputs'))
      + table('tfRegOutputs', ['tf_reg_col_name', 'tf_reg_col_desc'], outs, Tf.t('tf_reg_no_outputs', 'No outputs'))
      + `<div id="tfRegReadmePane" class="tf-reg-pane" hidden>${readme}${d.readme_truncated ? `<div class="tf-reg-note">${esc(Tf.t('tf_reg_truncated', 'README was truncated.'))}</div>` : ''}</div>` + snip;
  }
  function showPane(name) {
    const m = { inputs: 'tfRegInputs', outputs: 'tfRegOutputs', readme: 'tfRegReadmePane', snippet: 'tfRegSnipPane' };
    for (const [k, id] of Object.entries(m)) { const el = q('#' + id); if (el) el.hidden = k !== name; }
    R.wrap.querySelectorAll('[data-reg-tab]').forEach(b => b.classList.toggle('active', b.dataset.regTab === name));
  }
  function toggleRaw() {
    const raw = q('#tfRegReadmeRaw'), view = q('#tfRegReadme'), btn = q('#tfRegReadmeToggle');
    if (!raw || !view || !btn) return;
    raw.hidden = !raw.hidden; view.hidden = !raw.hidden;
    btn.textContent = raw.hidden ? Tf.t('tf_reg_readme_raw', 'Raw') : Tf.t('tf_reg_readme_rendered', 'Rendered');
  }
  function snippetText() {
    const s = R.d.snippet;
    return s.main + (R.d.isMod && (q('#tfRegDeclareVars') || {}).checked && s.variables ? '\n' + s.variables : '');
  }
  function fileName() { return R.d.isMod ? `module-${(q('#tfRegModName') || {}).value || ''}.tf` : `provider-${R.d.id.split('/')[1]}.tf`; }
  function snippetUI() {
    const pre = q('#tfRegSnippet'); if (pre) pre.textContent = snippetText();
    const nm = q('#tfRegModName');
    const bad = !!R.d && R.d.isMod && !MOD_NAME_RE.test(nm ? nm.value : '');
    if (nm) nm.setAttribute('aria-invalid', String(bad));
    for (const id of ['tfRegInsert', 'tfRegAddFile', 'tfRegCopy']) { const b = q('#' + id); if (b) b.disabled = bad; }
  }

  R.openItem = async function (kind, id, o) {
    o = o || {};
    const det = q('#tfRegDetail'); if (!det) return;
    const seq = ++R.dseq, isMod = kind === 'modules', wrap = R.wrap;
    det.innerHTML = Tf.ui.skeleton(4);
    const qs = new URLSearchParams();
    if (o.version) qs.set('version', o.version);
    if (isMod) { qs.set('module_name', o.modName || defName(id.split('/')[1])); qs.set('declare_vars', '1'); } // variables are always fetched; the checkbox only decides use
    let d, base;
    try { [d, base] = await Promise.all([reg(`${kind}/${pathOf(id)}?${qs}`), R.loadBase()]); }
    catch (e) { if (seq === R.dseq && R.wrap === wrap) det.innerHTML = Tf.ui.error(Tf.errMsg(e)); return; }
    if (seq !== R.dseq || R.wrap !== wrap) return;
    const it = (isMod ? d.module : d.provider) || {};
    const ver = o.version || (isMod ? it.version : d.latest || it.version) || '';
    R.d = { kind, id, isMod, snippet: snipOf(d.snippet) };
    const link = safeUrl(isMod ? `${base}/modules/${pathOf(id)}` : d.docs_url) || safeUrl(`${base}/providers/${pathOf(id)}`);
    const versions = (d.versions || []).map(vstr);
    if (ver && !versions.includes(ver)) versions.unshift(ver);
    const deps = [].concat(d.providers || [], d.dependencies || []).map(x => (x && x.name) || vstr(x)).filter(Boolean);
    const tabs = (isMod ? ['inputs', 'outputs', 'readme'] : []).concat('snippet');
    const tabLabel = { inputs: 'tf_reg_inputs', outputs: 'tf_reg_outputs', readme: 'tf_reg_readme', snippet: 'tf_reg_snippet' };
    const modName = o.modName || defName(id.split('/')[1]);
    det.innerHTML = `<div class="tf-reg-dhead"><button type="button" class="btn btn-sm tf-reg-back" data-reg-back>${icon('chevron-left', 'icon-xs')} ${esc(Tf.t('tf_back', 'Back'))}</button><span class="tf-reg-dname tf-mono">${esc(id)}</span>${link ? `<a href="${esc(link)}" target="_blank" rel="noopener noreferrer" class="tf-reg-link">${esc(Tf.t(isMod ? 'tf_reg_open' : 'tf_reg_docs', 'Open'))} ${icon('external-link', 'icon-xs')}</a>` : ''}</div>
      <div class="tf-reg-ddesc">${esc(it.description || '')}</div>${deps.length ? `<div class="tf-reg-note">${esc(Tf.t('tf_reg_deps', 'Requires'))}: ${esc(deps.join(', '))}</div>` : ''}
      <div class="tf-reg-ctl"><label>${esc(Tf.t('tf_reg_version', 'Version'))}<select id="tfRegVersion" class="form-input">${versions.map(v => `<option value="${esc(v)}" ${v === ver ? 'selected' : ''}>${esc(v)}</option>`).join('')}</select></label>
        ${isMod ? `<label>${esc(Tf.t('tf_reg_mod_name', 'Module name'))}<input type="text" id="tfRegModName" class="form-input tf-mono" maxlength="64" value="${esc(modName)}" /></label>
        <label class="tf-reg-chk"><input type="checkbox" id="tfRegDeclareVars" ${o.declare ? 'checked' : ''} /> ${esc(Tf.t('tf_reg_declare_vars', 'Declare variables in variables.tf'))}</label>` : ''}</div>
      <div class="tf-reg-tabs" role="tablist">${tabs.map(k => `<button type="button" class="tf-reg-tab" role="tab" data-reg-tab="${k}">${esc(Tf.t(tabLabel[k], k))}</button>`).join('')}</div>
      ${paneHTML(d, isMod)}
      <div class="tf-reg-actions">${R.pick ? `<button type="button" id="tfRegInsert" class="btn btn-sm btn-primary">${esc(Tf.t('tf_reg_insert', 'Insert at cursor'))}</button>
        <button type="button" id="tfRegAddFile" class="btn btn-sm">${esc(Tf.t('tf_reg_add_file', 'Add as file'))}</button>` : ''}
        <button type="button" id="tfRegCopy" class="btn btn-sm">${icon('copy', 'icon-xs')} ${esc(Tf.t('tf_reg_copy', 'Copy'))}</button></div>`;
    showPane(tabs[0]); snippetUI();
    const body = q('.tf-reg-body'); if (body) body.classList.add('show-detail'); // phone: list -> detail
  };
  R.refreshSnippet = async function (o) {
    const d = R.d, seq = ++R.dseq; if (!d || !d.isMod) return;
    const qs = new URLSearchParams({ version: o.version, module_name: o.modName, declare_vars: '1' });
    try {
      const r = await reg(`modules/${pathOf(d.id)}?${qs}`);
      if (seq === R.dseq && R.d === d) { d.snippet = snipOf(r.snippet); snippetUI(); }
    } catch (e) { /* keep the previous snippet */ }
  };

  // Insert/add-file are delegated to the caller (config editor) through onPick(snippet, mode).
  function pickAct(mode) {
    const d = R.d, cb = R.pickCb;
    if (!d || !cb) return;
    const nm = (q('#tfRegModName') || {}).value || '';
    if (d.isMod && !MOD_NAME_RE.test(nm)) return;
    const variables = d.isMod && (q('#tfRegDeclareVars') || {}).checked ? d.snippet.variables : '';
    cb({ main: d.snippet.main, variables: variables, file: fileName(), name: d.isMod ? nm : d.id.split('/')[1] }, mode);
  }

  // Opens the public registry browser in a dialog. onPick(snippet, mode) gets mode 'insert' or 'file'.
  // snippet = { main, variables (only when declared), file, name }. The dialog closes after a pick.
  R.pick = function (o) {
    const tab = document.getElementById('tfPanel-registry'); if (tab) tab.innerHTML = ''; // ids must stay unique
    R.pickCb = (snippet, mode) => { R.pickCb = null; api.close(); o.onPick(snippet, mode); };
    const api = Tf.dlg.open({
      title: Tf.t('tf_reg_pick_title', 'Insert from Terraform Registry'), width: 1180,
      body: `<div class="tf-reg-picker"><div class="tf-reg-hint">${esc(Tf.t('tf_reg_pick_hint', ''))}</div><div id="tfRegPickHost"></div></div>`,
      footer: '',
      onMount: (el) => R.mountPublic(el.querySelector('#tfRegPickHost'), true),
      onClose: () => { R.pickCb = null; },
    });
    return api;
  };

  /* ---------- private module catalog ---------- */
  const C = { list: [], q: '', prov: '' };
  async function loadModules() {
    const r = await Tf.api.get('/api/terraform/modules');
    C.list = r.modules || [];
    Tf.state.modules = C.list;
    return C.list;
  }
  function cardHTML(m) {
    return `<button type="button" class="card tf-cat-card" data-cat-id="${m.id}">
      <div class="card-title"><span>${icon('package', 'icon-sm')} ${esc(m.name)}</span><span class="badge">${esc(m.provider)}</span></div>
      <div class="tf-cat-desc">${esc(m.description || '')}</div>
      <div class="tf-cat-meta">${m.latest_version ? esc(Tf.t('tf_cat_latest', 'latest {v}', { v: m.latest_version })) + ' · ' : ''}${esc(Tf.t('tf_cat_nversions', '{n} versions', { n: m.version_count || 0 }))}</div></button>`;
  }
  function renderGrid() {
    const grid = q('#tfCatGrid'); if (!grid) return;
    const needle = C.q.toLowerCase();
    const items = C.list.filter(m => (!C.prov || m.provider === C.prov) && (!needle || (m.name + ' ' + (m.description || '')).toLowerCase().includes(needle)));
    if (!C.list.length) { grid.innerHTML = Tf.ui.empty({ icon: 'package', title: Tf.t('tf_cat_empty', 'No private modules yet'), desc: Tf.t('tf_cat_empty_desc', '') }); return; }
    grid.innerHTML = items.length ? items.map(cardHTML).join('') : Tf.ui.empty({ icon: 'search', title: Tf.t('tf_reg_none', 'No results') });
  }
  function catalogHTML() {
    return `<div class="tf-reg-bar">
      <input type="text" id="tfCatQuery" class="form-input" placeholder="${esc(Tf.t('tf_cat_search_ph', ''))}" value="${esc(C.q)}" maxlength="100" aria-label="${esc(Tf.t('tf_cat_search_ph', ''))}" />
      <input type="text" id="tfCatProvider" class="form-input" placeholder="${esc(Tf.t('tf_cat_provider_ph', 'Provider'))}" value="${esc(C.prov)}" maxlength="64" />
      <button type="button" id="btnTfNewModule" class="btn btn-sm btn-primary">${icon('plus', 'icon-xs')} ${esc(Tf.t('tf_cat_new', 'New Module'))}</button></div>
      <div id="tfCatGrid" class="tf-cat-grid"></div>`;
  }

  /* ---------- module detail (drawer) ---------- */
  async function openModule(id, ver) {
    let mod;
    try { const r = await Tf.api.get('/api/terraform/modules/' + id); mod = r.module || r; }
    catch (e) { Tf.toast(Tf.errMsg(e), 'error'); return; }
    const versions = mod.versions || [];
    ver = ver || mod.latest_version || (versions[0] && versions[0].version) || '';
    const cur = versions.find(v => v.version === ver) || versions[0];
    const doc = parseDoc(cur && cur.doc);
    const inputs = doc.inputs || [], outputs = doc.outputs || [];
    const req = inputs.filter(i => i.required);
    const usage = cur ? `module "${mod.name}" {\n  source  = "${cur.source_url || './modules/' + mod.name}"\n  version = "${cur.version}"\n${req.map(i => `  ${i.name} = "" # ${Tf.t('tf_cat_required', 'required')}`).join('\n')}${req.length ? '\n' : ''}}\n` : '';
    const tbl = (head, rows) => `<div class="tf-reg-tablewrap"><table class="tf-reg-table"><thead><tr>${head.map(h => `<th>${esc(h)}</th>`).join('')}</tr></thead><tbody>${rows}</tbody></table></div>`;
    const sec = (title, n, inner) => `<section class="tf-cat-sec"><h4>${esc(title)}${n !== undefined ? ` (${n})` : ''}</h4>${inner}</section>`;
    const body = `<div class="tf-cat-detail" data-cat-module="${id}">
      <div class="tf-cat-toolbar">
        ${versions.length ? `<select class="form-input tf-cat-ver" data-cat-act="version" aria-label="${esc(Tf.t('tf_reg_version', 'Version'))}">${versions.map(v => `<option value="${esc(v.version)}" ${cur && v.version === cur.version ? 'selected' : ''}>${esc(v.version)}</option>`).join('')}</select>` : ''}
        <button type="button" class="btn btn-sm btn-primary" data-cat-act="add-version">${icon('plus', 'icon-xs')} ${esc(Tf.t('tf_cat_add_version', 'Add Version'))}</button>
        ${cur ? `<button type="button" class="btn btn-sm" data-cat-act="delete-version" data-ver="${esc(cur.version)}">${icon('trash-2', 'icon-xs')} ${esc(Tf.t('tf_cat_delete', 'Delete'))} ${esc(cur.version)}</button>` : ''}
        <button type="button" class="btn btn-sm" data-cat-act="edit-module">${icon('pencil', 'icon-xs')} ${esc(Tf.t('tf_cat_edit', 'Edit'))}</button>
        <button type="button" class="btn btn-sm btn-danger" data-cat-act="delete-module">${icon('trash-2', 'icon-xs')} ${esc(Tf.t('tf_cat_delete', 'Delete'))}</button></div>
      ${!cur ? Tf.ui.empty({ icon: 'package', desc: Tf.t('tf_cat_no_versions', '') }) : `
        ${sec(Tf.t('tf_cat_usage', 'Usage snippet'), undefined, `<div class="tf-cat-copyrow"><pre class="tf-reg-pre">${esc(usage)}</pre><button type="button" class="btn btn-icon btn-sm" data-cat-act="copy" data-copy="${esc(usage)}" title="${esc(Tf.t('tf_cat_copy', 'Copy'))}">${icon('copy', 'icon-xs')}</button></div>`)}
        ${doc.readme ? sec(Tf.t('tf_reg_readme', 'README'), undefined, `<div class="tf-md">${md(doc.readme)}</div>`) : ''}
        ${sec(Tf.t('tf_cat_inputs', 'Inputs'), inputs.length, inputs.length ? tbl([Tf.t('tf_reg_col_name', 'Name'), Tf.t('tf_reg_col_type', 'Type'), Tf.t('tf_reg_col_default', 'Default'), Tf.t('tf_reg_col_desc', 'Description')],
          inputs.map(i => `<tr><td class="tf-mono">${esc(i.name)}${i.required ? ` <span class="badge badge-errored">${esc(Tf.t('tf_cat_required', 'required'))}</span>` : ''}${i.sensitive ? ` <span class="badge badge-unknown">${esc(Tf.t('tf_cat_sensitive', 'sensitive'))}</span>` : ''}</td>
            <td><div class="tf-reg-clamp tf-mono" title="${esc(i.type || '')}">${esc(i.type || '')}</div></td>
            <td><div class="tf-reg-clamp tf-mono" title="${esc(i.default || '')}">${esc(i.default || '')}</div></td><td>${esc(i.description || '')}</td></tr>`).join('')) : Tf.ui.empty({ title: '—' }))}
        ${sec(Tf.t('tf_cat_outputs', 'Outputs'), outputs.length, outputs.length ? tbl([Tf.t('tf_reg_col_name', 'Name'), Tf.t('tf_reg_col_desc', 'Description')],
          outputs.map(o => `<tr><td class="tf-mono">${esc(o.name)}${o.sensitive ? ` <span class="badge badge-unknown">${esc(Tf.t('tf_cat_sensitive', 'sensitive'))}</span>` : ''}</td><td>${esc(o.description || '')}</td></tr>`).join('')) : Tf.ui.empty({ title: '—' }))}
        ${sec(Tf.t('tf_cat_resources', 'Resources'), (doc.resources || []).length, `<div class="tf-mono tf-cat-list">${esc((doc.resources || []).join(', ') || '—')}</div>`)}
        ${sec(Tf.t('tf_cat_providers', 'Providers'), undefined, `<div class="tf-mono tf-cat-list">${esc((doc.providers || []).join(', ') || '—')}${doc.required_version ? ' · terraform ' + esc(doc.required_version) : ''}</div>`)}`}
    </div>`;
    Tf.drawer.open({
      title: mod.name, subtitle: `${esc(mod.provider)}${mod.description ? ' · ' + esc(mod.description) : ''}`, wide: true,
      mount: (host) => {
        host.innerHTML = '';
        const wrap = document.createElement('div');
        wrap.innerHTML = body;
        host.appendChild(wrap);
        wrap.addEventListener('click', (e) => onDetailClick(e, id, mod, cur));
        wrap.addEventListener('change', (e) => { if (e.target.matches('[data-cat-act="version"]')) openModule(id, e.target.value); });
        wrap.addEventListener('click', (e) => { const c = e.target.closest('.tf-reg-clamp'); if (c) c.classList.toggle('open'); });
      },
    });
  }
  function onDetailClick(e, id, mod, cur) {
    const b = e.target.closest('[data-cat-act]');
    if (!b) return;
    const act = b.dataset.catAct;
    if (act === 'copy') Tf.copy(b.dataset.copy || '');
    else if (act === 'add-version') openVersionDialog(id);
    else if (act === 'edit-module') openModuleDialog(mod);
    else if (act === 'delete-version' && cur) deleteVersion(id, b.dataset.ver);
    else if (act === 'delete-module') deleteModule(id, mod.name);
  }

  /* ---------- module create / edit ---------- */
  function openModuleDialog(m) {
    const field = (label, id, val, ph) => `<label class="tf-field"><span class="tf-field-label">${esc(label)}</span><input type="text" id="${id}" class="form-input" value="${esc(val || '')}" placeholder="${esc(ph || '')}" maxlength="120" /></label>`;
    const api = Tf.dlg.open({
      title: m ? Tf.t('tf_cat_title_edit', 'Edit Module') : Tf.t('tf_cat_title_new', 'New Module'), width: 520,
      body: `<div class="tf-grid-2">${field(Tf.t('tf_cat_name', 'Name'), 'tfModName', m && m.name)}${field(Tf.t('tf_cat_provider', 'Provider'), 'tfModProv', m && m.provider, 'aws')}
        <label class="tf-field" style="grid-column:1/-1;"><span class="tf-field-label">${esc(Tf.t('tf_cat_desc', 'Description'))}</span><input type="text" id="tfModDesc" class="form-input" value="${esc(m ? m.description || '' : '')}" maxlength="500" /></label></div>`,
      footer: `<button type="button" class="btn btn-sm" data-dlg-close>${esc(Tf.t('tf_close', 'Close'))}</button><button type="button" class="btn btn-sm btn-primary" data-act="save">${esc(Tf.t('tf_save', 'Save'))}</button>`,
      onMount: (el, dlg) => el.addEventListener('click', async (e) => {
        if (!e.target.closest('[data-act="save"]')) return;
        const body = { name: el.querySelector('#tfModName').value.trim(), provider: el.querySelector('#tfModProv').value.trim(), description: el.querySelector('#tfModDesc').value };
        if (!body.name || !body.provider) { dlg.error(Tf.t('tf_cat_name_required', 'Name and provider are required')); return; }
        try {
          const r = m ? await Tf.api.put('/api/terraform/modules/' + m.id, body) : await Tf.api.post('/api/terraform/modules', body);
          dlg.close();
          Tf.toast(Tf.t('tf_cat_saved', 'Saved'), 'success');
          await loadModules();
          const id = m ? m.id : (r.module || r).id;
          renderGrid();
          openModule(id);
        } catch (err) { dlg.error(Tf.errMsg(err)); }
      }),
    });
    return api;
  }

  /* ---------- version add (inline files or git) ---------- */
  const FILES = ['main.tf', 'variables.tf', 'outputs.tf', 'README.md'];
  function openVersionDialog(id) {
    const fileBox = FILES.map(f => `<label class="tf-field"><span class="tf-field-label tf-mono">${esc(f)}</span><textarea class="form-input tf-mono tf-cat-file" data-file="${esc(f)}" rows="5" spellcheck="false"></textarea></label>`).join('');
    const sel = (opts, cur) => opts.map(([v, l]) => `<option value="${v}" ${v === cur ? 'selected' : ''}>${esc(l)}</option>`).join('');
    const api = Tf.dlg.open({
      title: Tf.t('tf_cat_title_version', 'Add Version'), width: 760,
      body: `<div class="tf-grid-2"><label class="tf-field"><span class="tf-field-label">${esc(Tf.t('tf_cat_ver_num', 'Version'))} *</span><input type="text" id="tfVerNum" class="form-input tf-mono" placeholder="1.0.0" maxlength="64" /></label>
        <label class="tf-field"><span class="tf-field-label">${esc(Tf.t('tf_cat_src', 'Source'))}</span><select id="tfVerSrc" class="form-input">${sel([['inline', Tf.t('tf_cat_src_inline', 'Inline files')], ['git', Tf.t('tf_cat_src_git', 'Git repository')]], 'inline')}</select></label>
        <div id="tfVerInline" class="tf-cat-files" style="grid-column:1/-1;">${fileBox}</div>
        <div id="tfVerGit" class="tf-grid-2" style="grid-column:1/-1;" hidden>
          <label class="tf-field" style="grid-column:1/-1;"><span class="tf-field-label">${esc(Tf.t('tf_cat_git_url', 'Git URL'))}</span><input type="text" id="tfVerUrl" class="form-input tf-mono" placeholder="https://github.com/org/module.git" /></label>
          <label class="tf-field"><span class="tf-field-label">${esc(Tf.t('tf_cat_git_ref', 'Ref'))}</span><input type="text" id="tfVerRef" class="form-input tf-mono" placeholder="v1.0.0" /></label>
          <label class="tf-field"><span class="tf-field-label">${esc(Tf.t('tf_cat_git_path', 'Path'))}</span><input type="text" id="tfVerPath" class="form-input tf-mono" /></label>
          <label class="tf-field"><span class="tf-field-label">${esc(Tf.t('tf_cat_git_auth', 'Authentication'))}</span><select id="tfVerAuth" class="form-input">${sel([['none', Tf.t('tf_cat_auth_none', 'None')], ['token', Tf.t('tf_cat_auth_token', 'Token')], ['ssh', Tf.t('tf_cat_auth_ssh', 'SSH key')]], 'none')}</select></label>
          <label class="tf-field" id="tfVerTokWrap" hidden><span class="tf-field-label">${esc(Tf.t('tf_cat_git_token', 'Token'))}</span><input type="password" id="tfVerTok" class="form-input" autocomplete="new-password" /></label>
          <label class="tf-field" id="tfVerKeyWrap" hidden><span class="tf-field-label">${esc(Tf.t('tf_cat_ssh_key', 'SSH key'))}</span><select id="tfVerKey" class="form-input"></select></label>
        </div></div>`,
      footer: `<button type="button" class="btn btn-sm" data-dlg-close>${esc(Tf.t('tf_close', 'Close'))}</button><button type="button" class="btn btn-sm btn-primary" data-act="save">${esc(Tf.t('tf_save', 'Save'))}</button>`,
      onMount: (el, dlg) => {
        const $ = (s) => el.querySelector(s);
        el.addEventListener('change', (e) => {
          if (e.target.id === 'tfVerSrc') { const g = e.target.value === 'git'; $('#tfVerInline').hidden = g; $('#tfVerGit').hidden = !g; }
          if (e.target.id === 'tfVerAuth') { const a = e.target.value; $('#tfVerTokWrap').hidden = a !== 'token'; $('#tfVerKeyWrap').hidden = a !== 'ssh'; }
        });
        Tf.api.get('/api/ssh-keys').then(keys => {
          const s = $('#tfVerKey'); if (s) s.innerHTML = (Array.isArray(keys) ? keys : []).map(k => `<option value="${Number(k.id)}">${esc(k.name)}</option>`).join('');
        }).catch(() => { /* no keys: the ssh option stays empty */ });
        el.addEventListener('click', async (e) => {
          if (!e.target.closest('[data-act="save"]')) return;
          const version = $('#tfVerNum').value.trim();
          if (!version) { dlg.error(Tf.t('tf_cat_version_req', 'Version is required')); return; }
          const body = { version };
          if ($('#tfVerSrc').value === 'git') {
            body.git = { url: $('#tfVerUrl').value.trim(), ref: $('#tfVerRef').value.trim(), path: $('#tfVerPath').value.trim(), auth: $('#tfVerAuth').value };
            if (body.git.auth === 'token') body.git.token = $('#tfVerTok').value;
            if (body.git.auth === 'ssh') body.git.key_id = parseInt($('#tfVerKey').value, 10) || 0;
          } else {
            body.files = {};
            el.querySelectorAll('.tf-cat-file').forEach(t => { if (t.value !== '') body.files[t.dataset.file] = t.value; });
          }
          const btn = el.querySelector('[data-act="save"]');
          btn.disabled = true; btn.textContent = Tf.t('tf_cat_saving', 'Saving…');
          try {
            await Tf.api.post(`/api/terraform/modules/${id}/versions`, body);
            dlg.close();
            Tf.toast(Tf.t('tf_cat_saved', 'Saved'), 'success');
            await loadModules();
            renderGrid();
            openModule(id, version);
          } catch (err) {
            dlg.error(Tf.errMsg(err));
            btn.disabled = false; btn.textContent = Tf.t('tf_save', 'Save');
          }
        });
      },
    });
    return api;
  }

  async function deleteVersion(id, ver) {
    const ok = await Tf.confirm({ title: Tf.t('tf_cat_delete', 'Delete'), message: Tf.t('tf_cat_delete_version', 'Delete version {v}?', { v: ver }), confirmText: Tf.t('tf_cat_delete', 'Delete'), danger: true });
    if (!ok) return;
    try { await Tf.api.del(`/api/terraform/modules/${id}/versions/${enc(ver)}`); }
    catch (e) { Tf.toast(Tf.errMsg(e), 'error'); return; }
    Tf.toast(Tf.t('tf_cat_deleted', 'Deleted'), 'success');
    await loadModules();
    renderGrid();
    openModule(id);
  }
  async function deleteModule(id, name) {
    const ok = await Tf.confirm({ title: Tf.t('tf_cat_delete', 'Delete'), message: Tf.t('tf_cat_delete_module', 'Delete module {n} and all its versions?', { n: name }), confirmText: Tf.t('tf_cat_delete', 'Delete'), danger: true });
    if (!ok) return;
    try { await Tf.api.del('/api/terraform/modules/' + id); }
    catch (e) { Tf.toast(Tf.errMsg(e), 'error'); return; }
    Tf.toast(Tf.t('tf_cat_deleted', 'Deleted'), 'success');
    Tf.drawer.close();
    await loadModules();
    renderGrid();
  }

  /* ---------- top-level tab: segments Private Catalog | Public Registry ---------- */
  let seg = 'private';
  function segBar() {
    return `<div class="tf-seg" role="group">` +
      [['private', 'tf_reg_seg_private', 'Private Catalog'], ['public', 'tf_reg_seg_public', 'Public Registry']].map(([k, key, fb]) =>
        `<button type="button" class="btn btn-sm${seg === k ? ' btn-primary' : ''}" data-reg-seg="${k}" aria-pressed="${seg === k}">${esc(Tf.t(key, fb))}</button>`).join('') + '</div>';
  }
  async function showSeg(wrap, refresh) {
    const body = wrap.querySelector('#tfRegSegBody');
    if (!body) return;
    wrap.querySelectorAll('[data-reg-seg]').forEach(b => { const on = b.dataset.regSeg === seg; b.classList.toggle('btn-primary', on); b.setAttribute('aria-pressed', String(on)); });
    if (seg === 'public') { R.mountPublic(body, false); return; }
    if (!body.querySelector('#tfCatGrid') || refresh) body.innerHTML = catalogHTML();
    try { await loadModules(); } catch (e) { body.innerHTML = Tf.ui.error(Tf.errMsg(e)); return; }
    renderGrid();
  }

  Tf.registerTab({
    id: 'registry', icon: 'globe', labelKey: 'tf_tab_registry', fallback: 'Modules & Registry', order: 4,
    render: function (panel) {
      const wrap = document.createElement('div');
      wrap.className = 'tf-reg-page';
      wrap.innerHTML = segBar() + '<div id="tfRegSegBody" class="tf-reg-segbody"></div>';
      panel.appendChild(wrap);
      wrap.addEventListener('click', (e) => {
        const b = e.target.closest('[data-reg-seg]');
        if (b && b.dataset.regSeg !== seg) { seg = b.dataset.regSeg; showSeg(wrap, true); return; }
        const card = e.target.closest('[data-cat-id]');
        if (card) { openModule(Number(card.dataset.catId)); return; }
        if (e.target.closest('#btnTfNewModule')) { openModuleDialog(null); }
      });
      wrap.addEventListener('input', (e) => {
        if (e.target.id === 'tfCatQuery') { C.q = e.target.value.trim(); renderGrid(); }
      });
      wrap.addEventListener('change', (e) => {
        if (e.target.id === 'tfCatProvider') { C.prov = e.target.value.trim(); renderGrid(); }
      });
      wrap.addEventListener('keydown', (e) => { if (e.key === 'Enter' && e.target.matches('#tfRegQuery, #tfRegProvider')) { e.preventDefault(); R.search(false); } });
    },
    onShow: function (panel, opts) {
      const wrap = panel.querySelector('.tf-reg-page');
      if (wrap) showSeg(wrap, !!(opts && opts.refresh));
    },
    onHide: function () { if (R.wrap) R.wrap = null; },
  });
})();
