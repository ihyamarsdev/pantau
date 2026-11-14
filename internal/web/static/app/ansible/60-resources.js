/* Resources tab (Templates | Files | Library) and Settings tab (Ansible Gate + ansible.cfg). */
(function () {
  'use strict';
  const Ans = window.Ans;
  const esc = Ans.esc, T = Ans.t, icon = Ans.icon;

  Ans.addI18n({
    en: {
      ans_rs_sub_templates: 'Templates', ans_rs_sub_files: 'Files', ans_rs_sub_library: 'Library',
      ans_rs_tpl_hint: 'Jinja templates are stored in Pantau (included in encrypted System Snapshots) and shared by all inline playbooks. Reference one with template: src=<name>. Git playbooks use the templates/ folder of their own repository.',
      ans_rs_new: 'New template', ans_rs_search: 'Search…', ans_rs_tpl_name: 'Template name', ans_rs_tpl_name_ph: 'e.g. nginx.conf.j2', ans_rs_tpl_content: 'Content', ans_rs_save: 'Save', ans_rs_delete: 'Delete',
      ans_rs_tpl_none: 'No templates yet', ans_rs_tpl_pick: 'Select a template or create a new one.', ans_rs_tpl_saved: 'Template saved', ans_rs_tpl_name_required: 'Template name is required.', ans_rs_size: '{n} / 256 KiB',
      ans_rs_files_hint: 'Files are copied next to inline playbooks (use them with copy: src=<path>). Up to 5 MiB per file and 32 MiB in total; stored in Pantau and included in encrypted System Snapshots.',
      ans_rs_drop: 'Drop files here or choose files', ans_rs_prefix: 'Folder prefix (optional)', ans_rs_prefix_ph: 'e.g. certs/', ans_rs_upload: 'Upload', ans_rs_choose: 'Choose files', ans_rs_files_none: 'No files yet',
      ans_rs_uploading: 'Uploading {i}/{n}: {name}', ans_rs_uploaded: '{n} file(s) uploaded', ans_rs_upload_failed: 'Upload failed', ans_rs_none_selected: 'Choose at least one file.',
      ans_rs_col_name: 'Name', ans_rs_col_size: 'Size', ans_rs_col_updated: 'Updated', ans_rs_col_version: 'Version',
      ans_rs_confirm_title: 'Delete item?', ans_rs_confirm_msg: 'Delete "{name}"? Playbooks that reference it will fail.', ans_rs_deleted: 'Deleted', ans_rs_delete_failed: 'Could not delete',
      ans_rs_load_failed: 'Could not load the data', ans_rs_total: 'Total: {n} / 32 MiB',
      ans_rs_lib_hint: 'The Ansible library lives on the Pantau server disk and is shared by all playbooks. It is not included in encrypted System Snapshots; reinstall required roles and collections after restoring.',
      ans_rs_lib_unavailable: 'ansible-galaxy was not found on the Pantau server, so the library cannot be managed.', ans_rs_collections: 'Collections', ans_rs_roles: 'Roles', ans_rs_lib_empty: 'None installed',
      ans_rs_galaxy: 'Install from Galaxy', ans_rs_kind: 'Kind', ans_rs_kind_collection: 'Collection', ans_rs_kind_role: 'Role', ans_rs_gname: 'Name', ans_rs_gname_ph: 'namespace.name (e.g. community.general)', ans_rs_gversion: 'Version (optional)',
      ans_rs_install: 'Install', ans_rs_installing: 'Installing…', ans_rs_reqs: 'Install from requirements.yml', ans_rs_reqs_ph: 'collections:\n  - name: community.general\nroles:\n  - name: geerlingguy.nginx',
      ans_rs_local: 'Import local', ans_rs_source: 'Source', ans_rs_src_folder: 'Folder', ans_rs_src_archive: 'Archive (.tar.gz)', ans_rs_role_name: 'Role name', ans_rs_import: 'Import', ans_rs_importing: 'Importing…',
      ans_rs_err_name: 'Item name is required.', ans_rs_err_reqs: 'requirements.yml content is required.', ans_rs_err_role: 'Role name is required.', ans_rs_err_archive: 'Select a .tar.gz archive file.', ans_rs_err_folder: 'Select a folder containing files to import.',
      ans_rs_err_path: 'Folder contains an invalid or duplicate relative path.', ans_rs_install_ok: 'Installed successfully', ans_rs_import_ok: 'Imported successfully', ans_rs_install_failed: 'Install failed',
      ans_rs_stream_cut: 'Connection lost before the install finished; check the list (the server finishes it anyway).',
      ans_rs_lib_del_title: 'Delete library item?', ans_rs_lib_del_msg: 'Delete {kind} "{name}" from the server library?',
      ans_rs_gate: 'Ansible Gate', ans_rs_gate_state: 'State', ans_rs_gate_on: 'Active', ans_rs_gate_off: 'Off', ans_rs_gate_missing: 'ansible-playbook not found', ans_rs_version: 'Version', ans_rs_tz: 'Server time zone', ans_rs_conc: 'Max concurrent runs',
      ans_rs_git: 'git', ans_rs_git_yes: 'available', ans_rs_git_no: 'not found', ans_rs_gate_warn: 'Ansible playbooks execute on the Pantau server with the privileges of the Pantau process. Tasks using connection: local, delegate_to: localhost or local plugins can read and write data on the master server. Only enable the Gate if you trust everyone who can sign in to Pantau.',
      ans_rs_cfg: 'ansible.cfg', ans_rs_cfg_hint: 'Global additions to ansible.cfg. Pantau-managed keys (inventory, SSH keys/args, host key checking, roles/collections paths) are rejected. Any ansible.cfg inside imported folders is ignored; configure settings here instead.',
      ans_rs_cfg_allowed: 'Allowed sections: [{s}]', ans_rs_cfg_saved: 'Configuration saved', ans_rs_cfg_failed: 'Could not save the configuration', ans_rs_cfg_off: 'Enable the Ansible Gate to edit the configuration.', ans_rs_cfg_managed: 'Managed keys',
    },
    id: {
      ans_rs_sub_templates: 'Template', ans_rs_sub_files: 'Berkas', ans_rs_sub_library: 'Pustaka',
      ans_rs_tpl_hint: 'Template Jinja disimpan di Pantau (ikut System Snapshot terenkripsi) dan dipakai bersama oleh semua playbook inline. Rujuk dengan template: src=<nama>. Playbook Git memakai folder templates/ dari repositorinya sendiri.',
      ans_rs_new: 'Template baru', ans_rs_search: 'Cari…', ans_rs_tpl_name: 'Nama template', ans_rs_tpl_name_ph: 'mis. nginx.conf.j2', ans_rs_tpl_content: 'Isi', ans_rs_save: 'Simpan', ans_rs_delete: 'Hapus',
      ans_rs_tpl_none: 'Belum ada template', ans_rs_tpl_pick: 'Pilih template atau buat yang baru.', ans_rs_tpl_saved: 'Template disimpan', ans_rs_tpl_name_required: 'Nama template wajib diisi.', ans_rs_size: '{n} / 256 KiB',
      ans_rs_files_hint: 'Berkas disalin di sebelah playbook inline (pakai dengan copy: src=<path>). Maksimal 5 MiB per berkas dan 32 MiB total; disimpan di Pantau dan ikut System Snapshot terenkripsi.',
      ans_rs_drop: 'Letakkan berkas di sini atau pilih berkas', ans_rs_prefix: 'Prefix folder (opsional)', ans_rs_prefix_ph: 'mis. certs/', ans_rs_upload: 'Unggah', ans_rs_choose: 'Pilih berkas', ans_rs_files_none: 'Belum ada berkas',
      ans_rs_uploading: 'Mengunggah {i}/{n}: {name}', ans_rs_uploaded: '{n} berkas diunggah', ans_rs_upload_failed: 'Unggah gagal', ans_rs_none_selected: 'Pilih setidaknya satu berkas.',
      ans_rs_col_name: 'Nama', ans_rs_col_size: 'Ukuran', ans_rs_col_updated: 'Diperbarui', ans_rs_col_version: 'Versi',
      ans_rs_confirm_title: 'Hapus item?', ans_rs_confirm_msg: 'Hapus "{name}"? Playbook yang merujuknya akan gagal.', ans_rs_deleted: 'Dihapus', ans_rs_delete_failed: 'Gagal menghapus',
      ans_rs_load_failed: 'Gagal memuat data', ans_rs_total: 'Total: {n} / 32 MiB',
      ans_rs_lib_hint: 'Pustaka Ansible berada di disk server Pantau dan dipakai bersama oleh semua playbook. Tidak ikut System Snapshot terenkripsi; pasang ulang role dan collection yang dibutuhkan setelah restore.',
      ans_rs_lib_unavailable: 'ansible-galaxy tidak ditemukan di server Pantau, sehingga pustaka tidak bisa dikelola.', ans_rs_collections: 'Collection', ans_rs_roles: 'Role', ans_rs_lib_empty: 'Belum ada yang terpasang',
      ans_rs_galaxy: 'Pasang dari Galaxy', ans_rs_kind: 'Jenis', ans_rs_kind_collection: 'Collection', ans_rs_kind_role: 'Role', ans_rs_gname: 'Nama', ans_rs_gname_ph: 'namespace.name (mis. community.general)', ans_rs_gversion: 'Versi (opsional)',
      ans_rs_install: 'Pasang', ans_rs_installing: 'Memasang…', ans_rs_reqs: 'Pasang dari requirements.yml', ans_rs_reqs_ph: 'collections:\n  - name: community.general\nroles:\n  - name: geerlingguy.nginx',
      ans_rs_local: 'Impor lokal', ans_rs_source: 'Sumber', ans_rs_src_folder: 'Folder', ans_rs_src_archive: 'Arsip (.tar.gz)', ans_rs_role_name: 'Nama role', ans_rs_import: 'Impor', ans_rs_importing: 'Mengimpor…',
      ans_rs_err_name: 'Nama item wajib diisi.', ans_rs_err_reqs: 'Isi requirements.yml wajib diisi.', ans_rs_err_role: 'Nama role wajib diisi.', ans_rs_err_archive: 'Pilih berkas arsip .tar.gz.', ans_rs_err_folder: 'Pilih folder yang berisi berkas untuk diimpor.',
      ans_rs_err_path: 'Folder berisi path relatif yang tidak valid atau ganda.', ans_rs_install_ok: 'Berhasil dipasang', ans_rs_import_ok: 'Berhasil diimpor', ans_rs_install_failed: 'Pemasangan gagal',
      ans_rs_stream_cut: 'Koneksi terputus sebelum pemasangan selesai; periksa daftar (server tetap menyelesaikannya).',
      ans_rs_lib_del_title: 'Hapus item pustaka?', ans_rs_lib_del_msg: 'Hapus {kind} "{name}" dari pustaka server?',
      ans_rs_gate: 'Ansible Gate', ans_rs_gate_state: 'Status', ans_rs_gate_on: 'Aktif', ans_rs_gate_off: 'Nonaktif', ans_rs_gate_missing: 'ansible-playbook tidak ditemukan', ans_rs_version: 'Versi', ans_rs_tz: 'Zona waktu server', ans_rs_conc: 'Maks run bersamaan',
      ans_rs_git: 'git', ans_rs_git_yes: 'tersedia', ans_rs_git_no: 'tidak ditemukan', ans_rs_gate_warn: 'Playbook Ansible dijalankan di server Pantau dengan hak akses proses Pantau. Task yang memakai connection: local, delegate_to: localhost, atau plugin lokal dapat membaca dan menulis data di server master. Aktifkan Gate hanya jika Anda memercayai semua orang yang dapat masuk ke Pantau.',
      ans_rs_cfg: 'ansible.cfg', ans_rs_cfg_hint: 'Tambahan global untuk ansible.cfg. Kunci yang dikelola Pantau (inventory, kunci/argumen SSH, pemeriksaan host key, path role/collection) ditolak. ansible.cfg di dalam folder impor diabaikan; atur di sini.',
      ans_rs_cfg_allowed: 'Section yang diizinkan: [{s}]', ans_rs_cfg_saved: 'Konfigurasi disimpan', ans_rs_cfg_failed: 'Gagal menyimpan konfigurasi', ans_rs_cfg_off: 'Aktifkan Ansible Gate untuk mengedit konfigurasi.', ans_rs_cfg_managed: 'Kunci yang dikelola',
    },
  });

  const sizeText = function (n) { return Ans.fmt.bytes(n); };
  const ndjsonRead = async function (res, onLog) {
    let error = T('ans_rs_stream_cut');
    const handle = function (line) { let m; try { m = JSON.parse(line); } catch (e) { return; } if (m.log) onLog(m.log); if (m.done) error = m.error || ''; };
    const reader = res.body.getReader(), dec = new TextDecoder();
    let buf = '';
    for (;;) {
      const r = await reader.read();
      if (r.done) break;
      buf += dec.decode(r.value, { stream: true });
      let nl;
      while ((nl = buf.indexOf('\n')) >= 0) { handle(buf.slice(0, nl)); buf = buf.slice(nl + 1); }
    }
    if (buf.trim()) handle(buf);
    return error;
  };
  // Install endpoints answer NDJSON ({log} chunks, then {done[,error]}); a plain JSON reply means nothing ran.
  async function streamInstall(url, body, out) {
    out.hidden = false; out.textContent = '';
    const t0 = Date.now();
    // The elapsed time sits above the log from the start: the install may take a while before the first line arrives.
    const head = document.createElement('div');
    head.className = 'ans-rs-out-head';
    head.innerHTML = SP.html(T('ans_rs_installing'), t0);
    out.parentNode.insertBefore(head, out);
    const done = function () { if (head.parentNode) head.parentNode.removeChild(head); };
    try {
      const init = { method: 'POST', body: body instanceof FormData ? body : JSON.stringify(body), headers: body instanceof FormData ? {} : { 'Content-Type': 'application/json' }, credentials: 'same-origin' };
      const res = await fetch(url, init);
      if (!(res.headers.get('content-type') || '').includes('application/x-ndjson')) {
        const d = await res.json().catch(function () { return {}; });
        out.hidden = true; return d.error || T('ans_rs_install_failed');
      }
      return await ndjsonRead(res, function (s) { out.textContent += s; out.scrollTop = out.scrollHeight; });
    } finally { done(); }
  }

  /* ===================== Resources tab ===================== */
  const R = { sub: Ans.store.get('rs.sub', 'templates'), root: null, assets: null, lib: null, error: '', tpl: { sel: null, name: '', content: '', isNew: false, q: '' } };

  async function loadAssets() {
    try { const d = await Ans.api.get('/api/ansible/assets'); R.assets = { template: [], file: [] }; (d.assets || []).forEach(function (a) { if (R.assets[a.kind]) R.assets[a.kind].push(a); }); R.error = ''; }
    catch (e) { R.error = T('ans_rs_load_failed') + ': ' + e.message; R.assets = R.assets || { template: [], file: [] }; }
  }
  async function loadLib() {
    try { R.lib = await Ans.api.get('/api/ansible/library'); R.error = ''; } catch (e) { R.error = T('ans_rs_load_failed') + ': ' + e.message; R.lib = R.lib || { available: true, collections: [], roles: [] }; }
  }

  function paintSub() {
    const body = R.root.querySelector('[data-rs="body"]');
    R.root.querySelectorAll('[data-sub]').forEach(function (b) { b.setAttribute('aria-pressed', String(b.getAttribute('data-sub') === R.sub)); });
    body.innerHTML = Ans.ui.skeleton(4);
    if (R.sub === 'library') loadLib().then(function () { if (R.sub === 'library') paintLibrary(body); });
    else loadAssets().then(function () { (R.sub === 'templates' ? paintTemplates : paintFiles)(body); });
  }

  /* ----- templates ----- */
  function paintTemplates(body) {
    const items = R.assets.template, q = R.tpl.q.trim().toLowerCase();
    const shown = items.filter(function (a) { return !q || a.name.toLowerCase().indexOf(q) >= 0; });
    body.innerHTML = '<div class="ans-hint">' + esc(T('ans_rs_tpl_hint')) + '</div>' + (R.error ? Ans.ui.error(R.error) : '') +
      '<div class="ans-rs-split"><div class="ans-rs-side"><div class="ans-row"><div class="search-box" style="flex:1;margin:0"><input class="form-input" data-tpl="q" placeholder="' + esc(T('ans_rs_search')) + '" aria-label="' + esc(T('ans_rs_search')) + '" value="' + esc(R.tpl.q) + '" style="height:30px;font-size:var(--fs-xs);width:100%" /></div><button type="button" class="btn btn-sm btn-primary" data-tpl="new">' + icon('plus', 'icon-sm') + '</button></div>' +
      '<div id="ansibleTemplatesList" class="ans-rs-list">' + (shown.length ? shown.map(function (a) { return '<button type="button" class="ans-rs-item' + (R.tpl.sel === a.id ? ' on' : '') + '" data-tpl-id="' + a.id + '" title="' + esc(a.name) + '">' + icon('file-code', 'icon-sm') + '<span>' + esc(a.name) + '</span><em class="ans-mono ans-muted">' + esc(sizeText(a.size)) + '</em></button>'; }).join('') : '<div class="ans-muted ans-hint" id="ansibleTemplatesEmpty" style="padding:8px">' + esc(T('ans_rs_tpl_none')) + '</div>') + '</div></div>' +
      '<div class="ans-rs-main" data-tpl="main"></div></div>';
    paintTplEditor();
  }
  function paintTplEditor() {
    const main = R.root.querySelector('[data-tpl="main"]'); if (!main) return;
    const t = R.tpl;
    if (t.sel === null && !t.isNew) { main.innerHTML = '<div class="ans-muted ans-hint ans-rs-pick">' + esc(T('ans_rs_tpl_pick')) + '</div>'; return; }
    main.innerHTML = '<div class="ans-field"><label for="ansibleTemplateName">' + esc(T('ans_rs_tpl_name')) + '</label><input id="ansibleTemplateName" class="form-input ans-mono" data-tpl="name" placeholder="' + esc(T('ans_rs_tpl_name_ph')) + '" value="' + esc(t.name) + '" /></div>' +
      '<div class="ans-field" style="flex:1;min-height:0"><label for="ansibleTemplateContent">' + esc(T('ans_rs_tpl_content')) + ' <span class="ans-muted ans-mono" data-tpl="size"></span></label><textarea id="ansibleTemplateContent" class="form-input ans-mono ans-rs-ta" data-tpl="content" spellcheck="false">' + esc(t.content) + '</textarea></div>' +
      '<div class="ans-row" style="justify-content:flex-end">' + (t.sel ? '<button type="button" class="btn btn-sm btn-danger" data-tpl="del">' + icon('trash-2', 'icon-sm') + ' ' + esc(T('ans_rs_delete')) + '</button>' : '') + '<button type="button" id="btnAnsibleTemplateSave" class="btn btn-sm btn-primary" data-tpl="save">' + icon('save', 'icon-sm') + ' ' + esc(T('ans_rs_save')) + '</button></div>';
    paintTplSize();
  }
  function paintTplSize() { const el = R.root.querySelector('[data-tpl="size"]'); if (!el) return; const n = new Blob([R.tpl.content]).size; el.textContent = T('ans_rs_size', '{n} / 256 KiB', { n: sizeText(n) }); el.style.color = n > 256 * 1024 ? 'var(--danger)' : ''; }
  async function openTpl(id) {
    try { const d = await Ans.api.get('/api/ansible/assets/' + id); R.tpl.sel = id; R.tpl.isNew = false; R.tpl.name = d.name; R.tpl.content = d.content; paintTemplates(R.root.querySelector('[data-rs="body"]')); }
    catch (e) { Ans.toast(e.message, 'error'); }
  }
  async function putAsset(kind, name, body) {
    const res = await fetch('/api/ansible/assets?kind=' + kind + '&name=' + encodeURIComponent(name), { method: 'PUT', body: body, credentials: 'same-origin' });
    if (!res.ok) { const d = await res.json().catch(function () { return {}; }); throw new Error(d.error || String(res.status)); }
  }
  async function saveTpl() {
    const name = R.tpl.name.trim();
    if (!name) { Ans.toast(T('ans_rs_tpl_name_required'), 'error'); return; }
    try { await putAsset('template', name, R.tpl.content); Ans.toast(T('ans_rs_tpl_saved'), 'success'); await loadAssets(); const hit = R.assets.template.find(function (a) { return a.name === name; }); R.tpl.sel = hit ? hit.id : null; R.tpl.isNew = !hit; R.tpl.name = name; paintTemplates(R.root.querySelector('[data-rs="body"]')); }
    catch (e) { Ans.toast(e.message, 'error'); }
  }
  async function deleteAsset(kind, id, name) {
    const ok = await Ans.confirm({ title: T('ans_rs_confirm_title'), message: T('ans_rs_confirm_msg', 'Delete "{name}"?', { name: name }), confirmText: T('ans_rs_delete'), danger: true });
    if (!ok) return false;
    try { await Ans.api.del('/api/ansible/assets/' + id); Ans.toast(T('ans_rs_deleted'), 'success'); return true; }
    catch (e) { Ans.toast(T('ans_rs_delete_failed') + ': ' + e.message, 'error'); return false; }
  }

  /* ----- files ----- */
  function paintFiles(body) {
    const items = R.assets.file, total = items.reduce(function (s, a) { return s + a.size; }, 0);
    body.innerHTML = '<div class="ans-hint">' + esc(T('ans_rs_files_hint')) + '</div>' + (R.error ? Ans.ui.error(R.error) : '') +
      '<div class="ans-rs-drop" data-fl="drop" tabindex="0" role="button">' + icon('upload', 'icon-lg') + '<span>' + esc(T('ans_rs_drop')) + '</span><input type="file" id="ansibleFileInput" data-fl="input" multiple hidden /></div>' +
      '<div class="ans-row"><div class="ans-field" style="flex:1;min-width:200px"><label for="ansibleFilePrefix">' + esc(T('ans_rs_prefix')) + '</label><input id="ansibleFilePrefix" class="form-input ans-mono" data-fl="prefix" placeholder="' + esc(T('ans_rs_prefix_ph')) + '" /></div>' +
      '<button type="button" id="btnAnsibleFilesUpload" class="btn btn-sm btn-primary" data-fl="upload" style="align-self:flex-end">' + icon('upload', 'icon-sm') + ' ' + esc(T('ans_rs_upload')) + '</button></div><div class="ans-hint" data-fl="status" role="status"></div>' +
      '<div id="ansibleFilesList">' + (items.length ? '<div class="ans-table-wrap"><table class="ans-table"><thead><tr><th scope="col">' + esc(T('ans_rs_col_name')) + '</th><th scope="col">' + esc(T('ans_rs_col_size')) + '</th><th scope="col" class="ans-hide-sm">' + esc(T('ans_rs_col_updated')) + '</th><th scope="col" class="ans-actions"></th></tr></thead><tbody>' +
        items.map(function (a) { return '<tr data-id="' + a.id + '" data-name="' + esc(a.name) + '"><td class="ans-mono" style="font-weight:600">' + esc(a.name) + '</td><td class="ans-mono">' + esc(sizeText(a.size)) + '</td><td class="ans-hide-sm ans-muted">' + esc(Ans.fmt.ago(a.updated_at)) + '</td><td class="ans-actions"><button type="button" class="btn btn-sm btn-ghost ans-btn-icon" data-fl="del" title="' + esc(T('ans_rs_delete')) + '" aria-label="' + esc(T('ans_rs_delete')) + '">' + icon('trash-2', 'icon-sm') + '</button></td></tr>'; }).join('') + '</tbody></table></div><div class="ans-hint">' + esc(T('ans_rs_total', 'Total: {n} / 32 MiB', { n: sizeText(total) })) + '</div>' : '<div id="ansibleFilesEmpty">' + Ans.ui.empty({ icon: 'files', title: T('ans_rs_files_none') }) + '</div>') + '</div>';
  }
  const pending = { files: [] };
  async function uploadFiles(files) {
    if (!files.length) { Ans.toast(T('ans_rs_none_selected'), 'error'); return; }
    let prefix = (R.root.querySelector('#ansibleFilePrefix').value || '').trim().replace(/^\/+/, '');
    if (prefix && prefix.slice(-1) !== '/') prefix += '/';
    const st = R.root.querySelector('[data-fl="status"]'), btn = R.root.querySelector('#btnAnsibleFilesUpload');
    btn.disabled = true;
    let done = 0;
    try {
      for (let i = 0; i < files.length; i++) {
        st.textContent = T('ans_rs_uploading', 'Uploading {i}/{n}: {name}', { i: i + 1, n: files.length, name: files[i].name });
        try { await putAsset('file', prefix + files[i].name, files[i]); done++; } catch (e) { throw new Error(files[i].name + ': ' + e.message); }
      }
      Ans.toast(T('ans_rs_uploaded', '{n} file(s) uploaded', { n: done }), 'success');
    } catch (e) { Ans.toast(T('ans_rs_upload_failed') + ': ' + e.message, 'error'); }
    pending.files = [];
    await loadAssets(); if (R.sub === 'files') paintFiles(R.root.querySelector('[data-rs="body"]'));
  }

  /* ----- library ----- */
  function libItems(kind, items) {
    return items.length ? items.map(function (it) { return '<div class="ans-rs-libitem" data-kind="' + kind + '" data-name="' + esc(it.name) + '"><span class="ans-mono" style="font-weight:600">' + esc(it.name) + '</span><span class="badge badge-unknown ans-mono">' + esc(it.version || '-') + '</span><span class="ans-spacer"></span><button type="button" class="btn btn-sm btn-ghost ans-btn-icon" data-lib="del" title="' + esc(T('ans_rs_delete')) + '" aria-label="' + esc(T('ans_rs_delete')) + '">' + icon('trash-2', 'icon-sm') + '</button></div>'; }).join('') :
      '<div class="ans-muted ans-hint" id="' + (kind === 'collection' ? 'ansibleLibCollectionsEmpty' : 'ansibleLibRolesEmpty') + '">' + esc(T('ans_rs_lib_empty')) + '</div>';
  }
  function libTable(kind, items) {
    return '<div class="ans-card"><div class="ans-card-title">' + icon(kind === 'collection' ? 'package' : 'boxes', 'icon-sm') + esc(T(kind === 'collection' ? 'ans_rs_collections' : 'ans_rs_roles')) + '</div><div id="' + (kind === 'collection' ? 'ansibleLibCollectionsList' : 'ansibleLibRolesList') + '" class="ans-rs-lib">' + libItems(kind, items) + '</div></div>';
  }
  function paintLibrary(body) {
    const lib = R.lib || { available: true, collections: [], roles: [] };
    body.innerHTML = '<div class="ans-hint">' + esc(T('ans_rs_lib_hint')) + '</div>' + (R.error ? Ans.ui.error(R.error) : '') +
      (lib.available === false ? '<div class="ans-error" id="ansibleLibUnavailableBanner">' + icon('triangle-alert') + '<span>' + esc(T('ans_rs_lib_unavailable')) + '</span></div>' : '') +
      '<div class="ans-grid-2">' + libTable('collection', lib.collections || []) + libTable('role', lib.roles || []) + '</div>' +
      '<fieldset class="ans-rs-fs" ' + (lib.available === false ? 'disabled' : '') + '>' +
      '<div class="ans-card" id="ansibleLibGalaxyFields"><div class="ans-card-title">' + esc(T('ans_rs_galaxy')) + '</div><div class="ans-row"><select id="ansibleLibGalaxyKind" class="form-input" style="width:auto" aria-label="' + esc(T('ans_rs_kind')) + '"><option value="collection">' + esc(T('ans_rs_kind_collection')) + '</option><option value="role">' + esc(T('ans_rs_kind_role')) + '</option></select>' +
      '<input id="ansibleLibGalaxyName" class="form-input ans-mono" style="flex:1;min-width:200px" placeholder="' + esc(T('ans_rs_gname_ph')) + '" aria-label="' + esc(T('ans_rs_gname')) + '" /><input id="ansibleLibGalaxyVersion" class="form-input ans-mono" style="width:160px" placeholder="' + esc(T('ans_rs_gversion')) + '" aria-label="' + esc(T('ans_rs_gversion')) + '" />' +
      '<button type="button" id="btnAnsibleLibGalaxyInstall" class="btn btn-sm btn-primary" data-lib="galaxy">' + esc(T('ans_rs_install')) + '</button></div><pre class="ans-rs-out" id="ansibleLibGalaxyOutput" hidden></pre></div>' +
      '<div class="ans-card" id="ansibleLibReqsFields"><div class="ans-card-title">' + esc(T('ans_rs_reqs')) + '</div><textarea id="ansibleLibReqsContent" class="form-input ans-mono" rows="5" placeholder="' + esc(T('ans_rs_reqs_ph')) + '" spellcheck="false"></textarea><div class="ans-row" style="justify-content:flex-end"><button type="button" id="btnAnsibleLibReqsInstall" class="btn btn-sm btn-primary" data-lib="reqs">' + esc(T('ans_rs_install')) + '</button></div><pre class="ans-rs-out" id="ansibleLibReqsOutput" hidden></pre></div>' +
      '<div class="ans-card" id="ansibleLibLocalFields"><div class="ans-card-title">' + esc(T('ans_rs_local')) + '</div><div class="ans-grid-2"><div class="ans-field"><label>' + esc(T('ans_rs_kind')) + '</label><select id="ansibleLibLocalKind" class="form-input" data-lib="lkind"><option value="collection">' + esc(T('ans_rs_kind_collection')) + '</option><option value="role">' + esc(T('ans_rs_kind_role')) + '</option></select></div>' +
      '<div class="ans-field"><label>' + esc(T('ans_rs_source')) + '</label><select id="ansibleLibLocalSource" class="form-input" data-lib="lsrc"><option value="folder">' + esc(T('ans_rs_src_folder')) + '</option><option value="archive">' + esc(T('ans_rs_src_archive')) + '</option></select></div>' +
      '<div class="ans-field" id="ansibleLibLocalRoleNameWrap" hidden><label for="ansibleLibLocalRoleName">' + esc(T('ans_rs_role_name')) + '</label><input id="ansibleLibLocalRoleName" class="form-input ans-mono" /></div>' +
      '<div class="ans-field" id="ansibleLibLocalFolderField"><input type="file" id="ansibleLibLocalFolder" class="form-input" webkitdirectory multiple /></div><div class="ans-field" id="ansibleLibLocalArchiveField" hidden><input type="file" id="ansibleLibLocalArchive" class="form-input" accept=".tar.gz,application/gzip,application/x-tar" /></div></div>' +
      '<div class="ans-error" id="ansibleLibLocalError" role="alert" hidden></div><div class="ans-row" style="justify-content:flex-end"><button type="button" id="btnAnsibleLibLocalImport" class="btn btn-sm btn-primary" data-lib="local">' + esc(T('ans_rs_import')) + '</button></div><pre class="ans-rs-out" id="ansibleLibLocalOutput" hidden></pre></div></fieldset>';
  }
  const gid = function (id) { return R.root.querySelector('#' + id); };
  async function withBusy(btn, busyText, fn) {
    if (!btn) { try { await fn(); } catch (e) { Ans.toast(T('ans_rs_install_failed') + ': ' + (e && e.message ? e.message : e), 'error'); } return; }
    const restore = SP.busy(btn, busyText); // SP.busy restores the whole button, so the icon survives
    try { await fn(); } catch (e) { Ans.toast(T('ans_rs_install_failed') + ': ' + (e && e.message ? e.message : e), 'error'); }
    finally { restore(); }
  }
  async function libInstall(kind) {
    if (kind === 'galaxy') {
      const name = gid('ansibleLibGalaxyName').value.trim();
      if (!name) { Ans.toast(T('ans_rs_err_name'), 'error'); return; }
      await withBusy(gid('btnAnsibleLibGalaxyInstall'), T('ans_rs_installing'), async function () {
        const err = await streamInstall('/api/ansible/library/galaxy', { kind: gid('ansibleLibGalaxyKind').value, name: name, version: gid('ansibleLibGalaxyVersion').value.trim() }, gid('ansibleLibGalaxyOutput'));
        await loadLib(); if (err) Ans.toast(err, 'error'); else Ans.toast(T('ans_rs_install_ok'), 'success'); refreshLibLists();
      });
    } else if (kind === 'reqs') {
      const content = gid('ansibleLibReqsContent').value.trim();
      if (!content) { Ans.toast(T('ans_rs_err_reqs'), 'error'); return; }
      await withBusy(gid('btnAnsibleLibReqsInstall'), T('ans_rs_installing'), async function () {
        const err = await streamInstall('/api/ansible/library/requirements', { content: content }, gid('ansibleLibReqsOutput'));
        await loadLib(); if (err) Ans.toast(err, 'error'); else Ans.toast(T('ans_rs_install_ok'), 'success'); refreshLibLists();
      });
    } else await libLocal();
  }
  function refreshLibLists() { const lib = R.lib || {}; const c = gid('ansibleLibCollectionsList'), r = gid('ansibleLibRolesList'); if (c) c.innerHTML = libItems('collection', lib.collections || []); if (r) r.innerHTML = libItems('role', lib.roles || []); }
  function prepareFolder(selection) {
    const sel = Array.from(selection), first = sel[0] && sel[0].webkitRelativePath, root = first ? first.split('/')[0] : '';
    const strip = root && sel.every(function (f) { return (f.webkitRelativePath || '').indexOf(root + '/') === 0; });
    const ok = function (p) { return !!p && !/[\\":\x00-\x1f\x7f-\x9f]/.test(p) && !p.split('/').some(function (x) { return !x || x === '.' || x === '..'; }); };
    const out = [], seen = new Set();
    for (const f of sel) {
      const raw = f.webkitRelativePath || f.name; if (!ok(raw)) throw new Error(T('ans_rs_err_path'));
      const p = strip ? raw.slice(root.length + 1) : raw;
      if (p.split('/').some(function (x) { return ['.git', '__pycache__', '.DS_Store'].indexOf(x) >= 0; })) continue;
      if (!ok(p) || seen.has(p)) throw new Error(T('ans_rs_err_path'));
      seen.add(p); out.push({ file: f, path: p });
    }
    return out;
  }
  async function libLocal() {
    const kind = gid('ansibleLibLocalKind').value, src = gid('ansibleLibLocalSource').value, role = gid('ansibleLibLocalRoleName').value.trim();
    const err = gid('ansibleLibLocalError'), out = gid('ansibleLibLocalOutput');
    const fail = function (m) { err.textContent = m; err.hidden = !m; return !!m; };
    fail(''); out.hidden = true;
    if (kind === 'role' && !role) { fail(T('ans_rs_err_role')); return; }
    const body = new FormData(); body.append('kind', kind); if (kind === 'role') body.append('name', role);
    if (src === 'archive') { const f = gid('ansibleLibLocalArchive').files; if (!f || !f.length) { fail(T('ans_rs_err_archive')); return; } body.append('archive', f[0]); }
    else {
      const f = gid('ansibleLibLocalFolder').files; if (!f || !f.length) { fail(T('ans_rs_err_folder')); return; }
      try { prepareFolder(f).forEach(function (x) { body.append('files', x.file, x.path); }); } catch (e) { fail(e.message); return; }
    }
    await withBusy(gid('btnAnsibleLibLocalImport'), T('ans_rs_importing'), async function () {
      const e2 = await streamInstall('/api/ansible/library/local', body, out);
      await loadLib(); if (e2) fail(e2); else Ans.toast(T('ans_rs_import_ok'), 'success'); refreshLibLists();
    });
  }

  function renderResources(panel) {
    R.root = panel;
    panel.innerHTML = '<div class="ans-toolbar"><div class="ans-seg" role="group">' + ['templates', 'files', 'library'].map(function (s) { return '<button type="button" data-sub="' + s + '" aria-pressed="' + (R.sub === s) + '">' + esc(T('ans_rs_sub_' + s)) + '</button>'; }).join('') + '</div></div><div class="ans-rs" data-rs="body"></div>';
    panel.addEventListener('click', onClick);
    panel.addEventListener('input', onInput);
    panel.addEventListener('change', onChange);
    panel.addEventListener('dragover', function (e) { if (e.target.closest('[data-fl="drop"]')) { e.preventDefault(); e.target.closest('[data-fl="drop"]').classList.add('over'); } });
    panel.addEventListener('dragleave', function (e) { const d = e.target.closest('[data-fl="drop"]'); if (d) d.classList.remove('over'); });
    panel.addEventListener('drop', function (e) { const d = e.target.closest('[data-fl="drop"]'); if (!d) return; e.preventDefault(); d.classList.remove('over'); pending.files = Array.from(e.dataTransfer.files || []); uploadFiles(pending.files); });
    paintSub();
  }
  function onClick(e) {
    const sub = e.target.closest('[data-sub]'); if (sub && R.root.contains(sub)) { R.sub = sub.getAttribute('data-sub'); Ans.store.set('rs.sub', R.sub); paintSub(); return; }
    const tid = e.target.closest('[data-tpl-id]'); if (tid) { openTpl(Number(tid.getAttribute('data-tpl-id'))); return; }
    const tp = e.target.closest('[data-tpl]');
    if (tp) {
      const a = tp.getAttribute('data-tpl');
      if (a === 'new') { R.tpl.sel = null; R.tpl.isNew = true; R.tpl.name = ''; R.tpl.content = ''; paintTemplates(R.root.querySelector('[data-rs="body"]')); const n = gid('ansibleTemplateName'); if (n) n.focus(); }
      else if (a === 'save') saveTpl();
      else if (a === 'del') { const it = R.assets.template.find(function (x) { return x.id === R.tpl.sel; }); if (it) deleteAsset('template', it.id, it.name).then(function (ok) { if (ok) { R.tpl = { sel: null, name: '', content: '', isNew: false, q: R.tpl.q }; loadAssets().then(function () { paintTemplates(R.root.querySelector('[data-rs="body"]')); }); } }); }
      return;
    }
    const fl = e.target.closest('[data-fl]');
    if (fl) {
      const a = fl.getAttribute('data-fl');
      if (a === 'drop') gid('ansibleFileInput').click();
      else if (a === 'upload') uploadFiles(pending.files.length ? pending.files : Array.from(gid('ansibleFileInput').files || []));
      else if (a === 'del') { const tr = fl.closest('tr'); deleteAsset('file', Number(tr.getAttribute('data-id')), tr.getAttribute('data-name')).then(function (ok) { if (ok) loadAssets().then(function () { paintFiles(R.root.querySelector('[data-rs="body"]')); }); }); }
      return;
    }
    const lb = e.target.closest('[data-lib]');
    if (lb) {
      const a = lb.getAttribute('data-lib');
      if (a === 'del') {
        const row = lb.closest('[data-kind]'), kind = row.getAttribute('data-kind'), name = row.getAttribute('data-name');
        Ans.confirm({ title: T('ans_rs_lib_del_title'), message: T('ans_rs_lib_del_msg', 'Delete {kind} "{name}" from the server library?', { kind: kind, name: name }), confirmText: T('ans_rs_delete'), danger: true }).then(async function (ok) {
          if (!ok) return;
          try { await Ans.api.del('/api/ansible/library/' + kind + '/' + encodeURIComponent(name)); Ans.toast(T('ans_rs_deleted'), 'success'); await loadLib(); paintLibrary(R.root.querySelector('[data-rs="body"]')); }
          catch (er) { Ans.toast(T('ans_rs_delete_failed') + ': ' + er.message, 'error'); }
        });
      } else if (a === 'galaxy' || a === 'reqs' || a === 'local') libInstall(a);
    }
  }
  function onInput(e) {
    const t = e.target, k = t.getAttribute && t.getAttribute('data-tpl');
    if (k === 'q') { R.tpl.q = t.value; paintTemplates(R.root.querySelector('[data-rs="body"]')); const q = R.root.querySelector('[data-tpl="q"]'); if (q) { q.focus(); q.setSelectionRange(q.value.length, q.value.length); } }
    else if (k === 'name') R.tpl.name = t.value; else if (k === 'content') { R.tpl.content = t.value; paintTplSize(); }
  }
  function onChange(e) {
    const t = e.target, a = t.getAttribute && t.getAttribute('data-lib');
    if (a === 'lkind') gid('ansibleLibLocalRoleNameWrap').hidden = t.value !== 'role';
    else if (a === 'lsrc') { gid('ansibleLibLocalFolderField').hidden = t.value !== 'folder'; gid('ansibleLibLocalArchiveField').hidden = t.value !== 'archive'; }
    else if (t.getAttribute && t.getAttribute('data-fl') === 'input') pending.files = Array.from(t.files || []);
  }

  /* ===================== Settings tab ===================== */
  const SETT = { root: null, cfg: null, cfgText: '', saved: '' };
  function gateCard() {
    const s = Ans.state.status;
    if (!s) return Ans.ui.skeleton(2);
    const label = !s.available ? T('ans_rs_gate_missing') : s.enabled ? T('ans_rs_gate_on') : T('ans_rs_gate_off');
    const cls = !s.available ? 'badge-danger' : s.enabled ? 'badge-healthy' : 'badge-warning';
    const row = function (k, v) { return '<div class="ans-rs-kv"><span class="ans-rn-mk">' + esc(k) + '</span><span>' + v + '</span></div>'; };
    return '<div class="ans-card" id="ansibleGateBannerCard"><div class="ans-row"><div class="ans-card-title">' + icon('shield', 'icon-sm') + esc(T('ans_rs_gate')) + '</div><span class="badge ' + cls + '">' + esc(label) + '</span><span class="ans-spacer"></span>' +
      (s.available ? '<button type="button" class="btn btn-sm ' + (s.enabled ? '' : 'btn-primary') + '" data-st="gate">' + icon('power', 'icon-sm') + ' ' + esc(s.enabled ? T('ans_gate_disable') : T('ans_gate_enable')) + '</button>' : '') + '</div>' +
      '<div class="ans-rs-kvs">' + row(T('ans_rs_version'), '<span class="ans-mono">' + esc(s.version || '-') + '</span>') + row(T('ans_rs_tz'), esc(s.timezone || '-')) + row(T('ans_rs_conc'), esc(s.max_concurrent || '-')) + row(T('ans_rs_git'), s.git_available === false ? '<span class="ans-err">' + esc(T('ans_rs_git_no')) + '</span>' : esc(T('ans_rs_git_yes'))) + '</div>' +
      (s.error && !s.available ? '<div class="ans-error">' + icon('triangle-alert') + '<span>' + esc(s.error) + '</span></div>' : '') +
      '<div class="ans-hint">' + icon('shield-alert', 'icon-sm') + ' ' + esc(T('ans_rs_gate_warn')) + '</div></div>';
  }
  function cfgCard() {
    const s = Ans.state.status, on = !!(s && s.enabled), c = SETT.cfg || {}, n = new Blob([SETT.cfgText]).size;
    return '<div class="ans-card" id="ansibleConfigCard"><div class="ans-card-title">' + icon('cog', 'icon-sm') + esc(T('ans_rs_cfg')) + '</div><div class="ans-hint">' + esc(T('ans_rs_cfg_hint')) + '</div>' +
      (on ? '' : '<div class="ans-error" role="status">' + icon('shield-alert') + '<span>' + esc(T('ans_rs_cfg_off')) + '</span></div>') +
      '<textarea id="ansibleConfigContent" class="form-input ans-mono ans-rs-cfg" rows="8" spellcheck="false" placeholder="[defaults]\nforks = 10" ' + (on ? '' : 'disabled') + '>' + esc(SETT.cfgText) + '</textarea>' +
      '<div class="ans-row"><span class="ans-mono ans-muted" id="ansibleConfigAllowedSections">' + esc(Array.isArray(c.allowed_sections) ? T('ans_rs_cfg_allowed', 'Allowed sections: [{s}]', { s: c.allowed_sections.join(', ') }) : '') + '</span><span class="ans-spacer"></span><span class="ans-mono ans-muted" data-st="size" style="' + (n > 16384 ? 'color:var(--danger)' : '') + '">' + esc(Ans.fmt.bytes(n)) + ' / 16 KiB</span></div>' +
      (Array.isArray(c.managed_keys) && c.managed_keys.length ? '<details class="ans-details"><summary>' + esc(T('ans_rs_cfg_managed')) + '</summary><div class="ans-mono ans-muted" style="font-size:var(--fs-xs)">' + esc(c.managed_keys.join(', ')) + '</div></details>' : '') +
      '<div class="ans-error" id="ansibleConfigError" role="alert" hidden></div><div class="ans-row" style="justify-content:flex-end"><button type="button" id="btnAnsibleConfigSave" class="btn btn-sm btn-primary" data-st="save" ' + (on && SETT.cfgText !== SETT.saved ? '' : 'disabled') + '>' + icon('save', 'icon-sm') + ' ' + esc(T('ans_rs_save')) + '</button></div></div>';
  }
  async function loadCfg() {
    const s = Ans.state.status;
    if (!s || !s.enabled) { SETT.cfg = SETT.cfg || {}; return; }
    try { SETT.cfg = await Ans.api.get('/api/ansible/config'); SETT.cfgText = SETT.cfg.content || ''; SETT.saved = SETT.cfgText; } catch (e) { SETT.cfg = SETT.cfg || {}; }
  }
  function paintSettings() {
    if (!SETT.root) return;
    const focus = document.activeElement && document.activeElement.id === 'ansibleConfigContent';
    SETT.root.innerHTML = '<div class="ans-rs-settings">' + gateCard() + cfgCard() + '</div>';
    if (focus) { const t = SETT.root.querySelector('#ansibleConfigContent'); if (t) { t.focus(); t.setSelectionRange(t.value.length, t.value.length); } }
  }
  function renderSettings(panel) {
    SETT.root = panel;
    panel.addEventListener('click', async function (e) {
      const b = e.target.closest('[data-st]'); if (!b) return;
      const a = b.getAttribute('data-st');
      if (a === 'gate') { await Ans.toggleGate(); await loadCfg(); paintSettings(); }
      else if (a === 'save') {
        const err = SETT.root.querySelector('#ansibleConfigError'); err.hidden = true; b.disabled = true;
        try { await Ans.api.put('/api/ansible/config', { content: SETT.cfgText }); SETT.saved = SETT.cfgText; Ans.toast(T('ans_rs_cfg_saved'), 'success'); paintSettings(); }
        catch (ex) { err.textContent = T('ans_rs_cfg_failed') + ': ' + ex.message; err.hidden = false; b.disabled = false; }
      }
    });
    panel.addEventListener('input', function (e) {
      if (e.target.id !== 'ansibleConfigContent') return;
      SETT.cfgText = e.target.value;
      const sv = SETT.root.querySelector('#btnAnsibleConfigSave'); if (sv) sv.disabled = SETT.cfgText === SETT.saved;
      const sz = SETT.root.querySelector('[data-st="size"]'); if (sz) { const n = new Blob([SETT.cfgText]).size; sz.textContent = Ans.fmt.bytes(n) + ' / 16 KiB'; sz.style.color = n > 16384 ? 'var(--danger)' : ''; }
    });
    paintSettings(); loadCfg().then(paintSettings);
  }
  Ans.on('status', function () { if (SETT.root && SETT.root.isConnected) { paintSettings(); if (Ans.state.status && Ans.state.status.enabled && !SETT.cfg.content && SETT.cfg.content !== '') loadCfg().then(paintSettings); } });

  Ans.registerTab({ id: 'resources', icon: 'package', labelKey: 'ans_tab_resources', order: 40, render: renderResources, onShow: function (p, o) { if (o && o.refresh && R.root) paintSub(); } });
  Ans.registerTab({ id: 'settings', icon: 'settings', labelKey: 'ans_tab_settings', order: 50, render: renderSettings, onShow: function () { if (SETT.root) loadCfg().then(paintSettings); } });
})();
