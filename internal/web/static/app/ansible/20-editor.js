/* Playbook editor: metadata, YAML (CodeMirror) or Git source, variables analyzer, survey editor, git files (view/edit/commit). */
(function () {
  'use strict';
  const Ans = window.Ans;
  const esc = Ans.esc, T = Ans.t, icon = Ans.icon;

  Ans.addI18n({
    en: {
      ans_ed_name: 'Name', ans_ed_name_ph: 'e.g. nginx-deploy', ans_ed_desc: 'Description', ans_ed_group: 'Group', ans_ed_group_ph: 'optional label, e.g. Web',
      ans_ed_source: 'Source', ans_ed_src_inline: 'Inline YAML', ans_ed_src_git: 'Git repository',
      ans_ed_sub_playbook: 'Playbook', ans_ed_sub_vars: 'Variables', ans_ed_sub_survey: 'Survey', ans_ed_sub_files: 'Files',
      ans_ed_save: 'Save', ans_ed_discard: 'Discard', ans_ed_saving: 'Saving…', ans_ed_unsaved: 'Unsaved changes', ans_ed_saved: 'Playbook saved', ans_ed_close: 'Close', ans_ed_formatted: 'Playbook checked and formatted',
      ans_ed_err_name: 'A playbook name is required.', ans_ed_err_name_len: 'The name must be at most 64 characters.', ans_ed_err_content: 'The playbook YAML cannot be empty.',
      ans_ed_err_git: 'Repository URL and playbook path are required.', ans_ed_save_failed: 'Could not save the playbook',
      ans_ed_git_url: 'Repository URL', ans_ed_git_url_ph: 'https://github.com/org/repo.git or git@host:org/repo.git', ans_ed_git_branch: 'Branch or tag', ans_ed_git_branch_ph: 'default branch',
      ans_ed_git_path: 'Playbook path in the repository', ans_ed_git_path_ph: 'e.g. site.yml or playbooks/deploy.yml', ans_ed_git_auth: 'Authentication',
      ans_ed_auth_none: 'None (public repository)', ans_ed_auth_token: 'HTTPS access token', ans_ed_auth_key: 'SSH key from the library', ans_ed_git_token: 'Access token',
      ans_ed_token_set: 'A token is stored. Leave empty to keep it.', ans_ed_token_new: 'Token with access to the repository. Stored encrypted like SSH keys and never shown again.',
      ans_ed_git_key: 'SSH key', ans_ed_git_nokeys: 'No keys in the SSH Key Library yet.',
      ans_ed_git_notice: 'Pantau clones the repository (shallow) for every run, so the playbook is always taken from the configured branch. The write access needed for commits is explained under Files.',
      ans_ed_git_missing: 'The git binary was not found on the Pantau server.', ans_ed_sync: 'Sync now', ans_ed_sync_ok: 'Synced at commit {c}', ans_ed_sync_failed: 'Sync failed',
      ans_ed_last_sync: 'Last sync: {when} · {commit}', ans_ed_sync_need_save: 'Save the playbook before syncing.',
      ans_ed_vars_hint: 'Variables found in the playbook, grouped into the playbook\'s own and those of each role. Set a default value to pass it as an extra var on every run; runs can still override it.',
      ans_ed_vars_loading: 'Analyzing…', ans_ed_vars_empty: 'No variables detected.', ans_ed_vars_fail: 'Could not analyze the playbook',
      ans_ed_v_name: 'Variable', ans_ed_v_status: 'Status', ans_ed_v_value: 'Default value', ans_ed_v_source: 'Source', ans_ed_v_used: 'Used in',
      ans_ed_v_required: 'Required', ans_ed_v_optional: 'Optional', ans_ed_v_defined: 'Defined', ans_ed_v_overridden: '{s} (overridden)',
      ans_ed_v_del_override: 'Remove the default extra var', ans_ed_v_del_yaml: 'Remove from the playbook YAML', ans_ed_v_del_none: 'Only referenced, nothing to remove',
      ans_ed_v_yaml_missing: 'Could not find this variable in a plain vars: block; edit it in the Raw tab.',
      ans_ed_v_search: 'Search variables…', ans_ed_v_count: '{n} of {m}', ans_ed_v_nomatch: 'No variable matches “{q}”.', ans_ed_v_g_play: 'Playbook variables', ans_ed_v_g_role: 'Role {r}',
      ans_ed_v_add: 'Add variable', ans_ed_v_new_name: 'name', ans_ed_v_new_value: 'value (JSON or text)', ans_ed_v_err_name: 'The name must match ^[A-Za-z_][A-Za-z0-9_]*$.', ans_ed_v_err_dup: 'That variable already exists.',
      ans_ed_v_git_load: 'Analyze repository', ans_ed_v_git_hint: 'Git playbooks are analyzed from a clone of the repository.',
      ans_ed_sv_hint: 'A survey asks for typed answers when the playbook is launched. Answers become extra vars.', ans_ed_sv_add: 'Add field', ans_ed_sv_empty: 'No survey fields yet.',
      ans_ed_sv_key: 'Variable key', ans_ed_sv_label: 'Label', ans_ed_sv_type: 'Type', ans_ed_sv_desc: 'Help text', ans_ed_sv_req: 'Required', ans_ed_sv_default: 'Default',
      ans_ed_sv_options: 'Options (one per line)', ans_ed_sv_min: 'Min', ans_ed_sv_max: 'Max', ans_ed_sv_minlen: 'Min length', ans_ed_sv_maxlen: 'Max length',
      ans_ed_sv_up: 'Move up', ans_ed_sv_down: 'Move down', ans_ed_sv_del: 'Remove field', ans_ed_sv_none: '(none)', ans_ed_sv_true: 'Yes', ans_ed_sv_false: 'No',
      ans_ed_sv_err_key: 'Field {n}: the key must match [A-Za-z_][A-Za-z0-9_]{0,63}.', ans_ed_sv_err_dup: 'Duplicate survey key "{k}".', ans_ed_sv_err_opts: 'Field "{k}": options need 1-50 unique, non-empty entries.',
      ans_ed_sv_err_max: 'The survey cannot have more than 30 fields.', ans_ed_sv_err_bounds: 'Field "{k}": min must not exceed max.', ans_ed_sv_err_default: 'Field "{k}": the default is not valid.',
      ans_ed_sv_save_failed: 'The playbook was saved but the survey was rejected', ans_ed_sv_t_text: 'Text', ans_ed_sv_t_textarea: 'Long text', ans_ed_sv_t_number: 'Number', ans_ed_sv_t_integer: 'Integer',
      ans_ed_sv_t_select: 'Select', ans_ed_sv_t_multiselect: 'Multi-select', ans_ed_sv_t_boolean: 'Yes / No', ans_ed_sv_t_password: 'Password',
      ans_ed_f_need_save: 'Save the playbook first, then browse and edit the repository here.', ans_ed_f_inline: 'Files are available for Git playbooks only.',
      ans_ed_f_loading: 'Cloning repository…', ans_ed_f_failed: 'Could not read the repository', ans_ed_f_filter: 'Filter files…', ans_ed_f_new: 'New file', ans_ed_f_reload: 'Reload',
      ans_ed_f_commit: 'Commit & push', ans_ed_f_changes: '{n} pending changes', ans_ed_f_none: 'Select a file from the tree.', ans_ed_f_truncated: 'The repository has more files than are listed (limit 2,000).',
      ans_ed_f_path_ph: 'path/to/new-file.yml', ans_ed_f_delete: 'Delete file', ans_ed_f_undelete: 'Undo delete', ans_ed_f_revert: 'Revert', ans_ed_f_marked: 'marked for deletion', ans_ed_f_new_tag: 'new', ans_ed_f_mod_tag: 'edited', ans_ed_f_del_tag: 'deleted',
      ans_ed_f_binary: 'This file is not editable text.', ans_ed_f_too_big: 'This file is larger than 1 MiB and cannot be edited here.', ans_ed_f_bad_path: 'Invalid path (no empty parts, "..", or ".git").', ans_ed_f_exists: 'That file already exists.',
      ans_ed_c_title: 'Commit & push', ans_ed_c_msg: 'Commit message', ans_ed_c_msg_ph: 'Describe the change', ans_ed_c_branch: 'New branch (optional)', ans_ed_c_branch_ph: 'push to a new branch instead of the configured one',
      ans_ed_c_author: 'Author name (optional)', ans_ed_c_email: 'Author email (optional)', ans_ed_c_go: 'Commit & push', ans_ed_c_cancel: 'Cancel',
      ans_ed_c_note: 'Never force-pushes. The token or SSH key needs write permission on the remote (GitHub: Contents read and write; GitLab: write_repository).',
      ans_ed_c_ok: 'Pushed {c} to {b}', ans_ed_c_conflict: 'The repository changed since you opened it. Reload the files and apply your changes again.', ans_ed_c_failed: 'Commit failed',
      ans_ed_c_msg_required: 'A commit message is required.', ans_ed_c_nochanges: 'There are no changes to commit.',
    },
    id: {
      ans_ed_name: 'Nama', ans_ed_name_ph: 'mis. nginx-deploy', ans_ed_desc: 'Deskripsi', ans_ed_group: 'Grup', ans_ed_group_ph: 'label opsional, mis. Web',
      ans_ed_source: 'Sumber', ans_ed_src_inline: 'YAML inline', ans_ed_src_git: 'Repositori Git',
      ans_ed_sub_playbook: 'Playbook', ans_ed_sub_vars: 'Variabel', ans_ed_sub_survey: 'Survey', ans_ed_sub_files: 'Berkas',
      ans_ed_save: 'Simpan', ans_ed_discard: 'Buang', ans_ed_saving: 'Menyimpan…', ans_ed_unsaved: 'Perubahan belum disimpan', ans_ed_saved: 'Playbook disimpan', ans_ed_close: 'Tutup', ans_ed_formatted: 'Playbook diperiksa dan diformat',
      ans_ed_err_name: 'Nama playbook wajib diisi.', ans_ed_err_name_len: 'Nama maksimal 64 karakter.', ans_ed_err_content: 'YAML playbook tidak boleh kosong.',
      ans_ed_err_git: 'URL repositori dan path playbook wajib diisi.', ans_ed_save_failed: 'Gagal menyimpan playbook',
      ans_ed_git_url: 'URL repositori', ans_ed_git_url_ph: 'https://github.com/org/repo.git atau git@host:org/repo.git', ans_ed_git_branch: 'Branch atau tag', ans_ed_git_branch_ph: 'branch bawaan',
      ans_ed_git_path: 'Path playbook di repositori', ans_ed_git_path_ph: 'mis. site.yml atau playbooks/deploy.yml', ans_ed_git_auth: 'Autentikasi',
      ans_ed_auth_none: 'Tidak ada (repositori publik)', ans_ed_auth_token: 'Token akses HTTPS', ans_ed_auth_key: 'Kunci SSH dari pustaka', ans_ed_git_token: 'Token akses',
      ans_ed_token_set: 'Token sudah tersimpan. Kosongkan untuk mempertahankannya.', ans_ed_token_new: 'Token yang punya akses ke repositori. Disimpan terenkripsi seperti kunci SSH dan tidak pernah ditampilkan lagi.',
      ans_ed_git_key: 'Kunci SSH', ans_ed_git_nokeys: 'Belum ada kunci di Pustaka Kunci SSH.',
      ans_ed_git_notice: 'Pantau meng-clone repositori (dangkal) pada setiap run, sehingga playbook selalu diambil dari branch yang dikonfigurasi. Akses tulis untuk commit dijelaskan di bagian Berkas.',
      ans_ed_git_missing: 'Biner git tidak ditemukan di server Pantau.', ans_ed_sync: 'Sinkronkan', ans_ed_sync_ok: 'Disinkronkan pada commit {c}', ans_ed_sync_failed: 'Sinkronisasi gagal',
      ans_ed_last_sync: 'Sinkron terakhir: {when} · {commit}', ans_ed_sync_need_save: 'Simpan playbook sebelum sinkronisasi.',
      ans_ed_vars_hint: 'Variabel yang ditemukan di playbook, dikelompokkan menjadi variabel playbook dan variabel tiap role. Isi nilai bawaan untuk meneruskannya sebagai extra var pada setiap run; run tetap bisa menimpanya.',
      ans_ed_vars_loading: 'Menganalisis…', ans_ed_vars_empty: 'Tidak ada variabel terdeteksi.', ans_ed_vars_fail: 'Gagal menganalisis playbook',
      ans_ed_v_name: 'Variabel', ans_ed_v_status: 'Status', ans_ed_v_value: 'Nilai bawaan', ans_ed_v_source: 'Sumber', ans_ed_v_used: 'Dipakai di',
      ans_ed_v_required: 'Wajib', ans_ed_v_optional: 'Opsional', ans_ed_v_defined: 'Terdefinisi', ans_ed_v_overridden: '{s} (ditimpa)',
      ans_ed_v_del_override: 'Hapus extra var bawaan', ans_ed_v_del_yaml: 'Hapus dari YAML playbook', ans_ed_v_del_none: 'Hanya dirujuk, tidak ada yang dihapus',
      ans_ed_v_yaml_missing: 'Variabel ini tidak ditemukan di blok vars: biasa; edit di tab Raw.',
      ans_ed_v_search: 'Cari variabel…', ans_ed_v_count: '{n} dari {m}', ans_ed_v_nomatch: 'Tidak ada variabel yang cocok dengan “{q}”.', ans_ed_v_g_play: 'Variabel playbook', ans_ed_v_g_role: 'Role {r}',
      ans_ed_v_add: 'Tambah variabel', ans_ed_v_new_name: 'nama', ans_ed_v_new_value: 'nilai (JSON atau teks)', ans_ed_v_err_name: 'Nama harus cocok dengan ^[A-Za-z_][A-Za-z0-9_]*$.', ans_ed_v_err_dup: 'Variabel itu sudah ada.',
      ans_ed_v_git_load: 'Analisis repositori', ans_ed_v_git_hint: 'Playbook Git dianalisis dari clone repositori.',
      ans_ed_sv_hint: 'Survey meminta jawaban bertipe saat playbook dijalankan. Jawaban menjadi extra vars.', ans_ed_sv_add: 'Tambah field', ans_ed_sv_empty: 'Belum ada field survey.',
      ans_ed_sv_key: 'Kunci variabel', ans_ed_sv_label: 'Label', ans_ed_sv_type: 'Tipe', ans_ed_sv_desc: 'Teks bantuan', ans_ed_sv_req: 'Wajib', ans_ed_sv_default: 'Bawaan',
      ans_ed_sv_options: 'Opsi (satu per baris)', ans_ed_sv_min: 'Min', ans_ed_sv_max: 'Maks', ans_ed_sv_minlen: 'Panjang min', ans_ed_sv_maxlen: 'Panjang maks',
      ans_ed_sv_up: 'Naikkan', ans_ed_sv_down: 'Turunkan', ans_ed_sv_del: 'Hapus field', ans_ed_sv_none: '(tidak ada)', ans_ed_sv_true: 'Ya', ans_ed_sv_false: 'Tidak',
      ans_ed_sv_err_key: 'Field {n}: kunci harus cocok dengan [A-Za-z_][A-Za-z0-9_]{0,63}.', ans_ed_sv_err_dup: 'Kunci survey ganda "{k}".', ans_ed_sv_err_opts: 'Field "{k}": opsi butuh 1-50 entri unik yang tidak kosong.',
      ans_ed_sv_err_max: 'Survey tidak boleh lebih dari 30 field.', ans_ed_sv_err_bounds: 'Field "{k}": min tidak boleh melebihi maks.', ans_ed_sv_err_default: 'Field "{k}": nilai bawaan tidak valid.',
      ans_ed_sv_save_failed: 'Playbook tersimpan tetapi survey ditolak', ans_ed_sv_t_text: 'Teks', ans_ed_sv_t_textarea: 'Teks panjang', ans_ed_sv_t_number: 'Angka', ans_ed_sv_t_integer: 'Bilangan bulat',
      ans_ed_sv_t_select: 'Pilihan', ans_ed_sv_t_multiselect: 'Pilihan ganda', ans_ed_sv_t_boolean: 'Ya / Tidak', ans_ed_sv_t_password: 'Password',
      ans_ed_f_need_save: 'Simpan playbook dulu, lalu jelajahi dan edit repositori di sini.', ans_ed_f_inline: 'Berkas hanya tersedia untuk playbook Git.',
      ans_ed_f_loading: 'Meng-clone repositori…', ans_ed_f_failed: 'Gagal membaca repositori', ans_ed_f_filter: 'Filter berkas…', ans_ed_f_new: 'Berkas baru', ans_ed_f_reload: 'Muat ulang',
      ans_ed_f_commit: 'Commit & push', ans_ed_f_changes: '{n} perubahan tertunda', ans_ed_f_none: 'Pilih berkas dari pohon.', ans_ed_f_truncated: 'Repositori memiliki lebih banyak berkas dari yang ditampilkan (batas 2.000).',
      ans_ed_f_path_ph: 'path/ke/berkas-baru.yml', ans_ed_f_delete: 'Hapus berkas', ans_ed_f_undelete: 'Batalkan hapus', ans_ed_f_revert: 'Kembalikan', ans_ed_f_marked: 'ditandai untuk dihapus', ans_ed_f_new_tag: 'baru', ans_ed_f_mod_tag: 'diubah', ans_ed_f_del_tag: 'dihapus',
      ans_ed_f_binary: 'Berkas ini bukan teks yang bisa diedit.', ans_ed_f_too_big: 'Berkas lebih besar dari 1 MiB dan tidak bisa diedit di sini.', ans_ed_f_bad_path: 'Path tidak valid (tanpa bagian kosong, "..", atau ".git").', ans_ed_f_exists: 'Berkas itu sudah ada.',
      ans_ed_c_title: 'Commit & push', ans_ed_c_msg: 'Pesan commit', ans_ed_c_msg_ph: 'Jelaskan perubahan', ans_ed_c_branch: 'Branch baru (opsional)', ans_ed_c_branch_ph: 'push ke branch baru, bukan branch yang dikonfigurasi',
      ans_ed_c_author: 'Nama penulis (opsional)', ans_ed_c_email: 'Email penulis (opsional)', ans_ed_c_go: 'Commit & push', ans_ed_c_cancel: 'Batal',
      ans_ed_c_note: 'Tidak pernah force-push. Token atau kunci SSH perlu izin tulis di remote (GitHub: Contents read and write; GitLab: write_repository).',
      ans_ed_c_ok: '{c} di-push ke {b}', ans_ed_c_conflict: 'Repositori berubah sejak Anda membukanya. Muat ulang berkas lalu terapkan perubahan lagi.', ans_ed_c_failed: 'Commit gagal',
      ans_ed_c_msg_required: 'Pesan commit wajib diisi.', ans_ed_c_nochanges: 'Tidak ada perubahan untuk di-commit.',
    },
  });

  const SAMPLE = '---\n- name: Ping all hosts\n  hosts: all\n  gather_facts: false\n  tasks:\n    - name: Ping\n      ansible.builtin.ping:\n';
  const TYPES = ['text', 'textarea', 'number', 'integer', 'select', 'multiselect', 'boolean', 'password'];
  const KEY_RE = /^[A-Za-z_][A-Za-z0-9_]{0,63}$/;
  const VAR_RE = /^[A-Za-z_][A-Za-z0-9_]*$/;

  function parseVarValue(text) { const t = (text || '').trim(); if (!t) return ''; try { return JSON.parse(t); } catch (e) { return text; } }
  function formatVarValue(v) { if (v === undefined || v === null || v === '') return ''; return typeof v === 'string' ? v : JSON.stringify(v); }

  // Removes `name:` (and its indented value) from a `vars:` mapping; null when it is not found in a plain block.
  function removeYamlVar(text, name) {
    if (!/^\w+$/.test(name)) return null;
    const lines = text.split('\n');
    const indentOf = function (l) { return l.match(/^\s*/)[0].length; };
    const key = new RegExp('^\\s*' + name + '\\s*:');
    for (let i = 0; i < lines.length; i++) {
      const m = lines[i].match(/^(\s*)vars\s*:\s*$/);
      if (!m) continue;
      let entry = -1;
      for (let j = i + 1; j < lines.length; j++) {
        if (!lines[j].trim()) continue;
        const ind = indentOf(lines[j]);
        if (ind <= m[1].length) break;
        if (entry < 0) entry = ind;
        if (ind !== entry || !key.test(lines[j])) continue;
        let end = j + 1;
        while (end < lines.length && (!lines[end].trim() || indentOf(lines[end]) > entry)) end++;
        while (end > j + 1 && !lines[end - 1].trim()) end--;
        lines.splice(j, end - j);
        const rest = lines.slice(i + 1).find(function (l) { return l.trim(); });
        if (!rest || indentOf(rest) <= m[1].length) lines.splice(i, 1); // an empty `vars:` is rejected by Ansible
        return lines.join('\n');
      }
    }
    return null;
  }

  function cmMode(path) {
    const ext = (path.split('.').pop() || '').toLowerCase();
    if (/^ya?ml$/.test(ext)) return 'yaml'; if (ext === 'sh' || ext === 'bash') return 'shell';
    if (ext === 'json') return { name: 'javascript', json: true }; if (ext === 'j2' || ext === 'html' || ext === 'xml') return 'htmlmixed';
    return null;
  }
  function newCM(textarea, opts) {
    if (typeof CodeMirror === 'undefined') return null;
    return CodeMirror.fromTextArea(textarea, Object.assign({ lineNumbers: true, theme: 'vscode-dark', mode: 'yaml', indentUnit: 2, tabSize: 2, indentWithTabs: false, extraKeys: { Tab: function (cm) { cm.replaceSelection('  ', 'end'); } } }, opts || {}));
  }

  /* ===================== mount ===================== */
  function mount(container, o) {
    o = o || {};
    const orig = o.playbook || null;
    const isNew = !orig;
    let id = orig ? orig.id : null;
    const S = {
      name: orig ? orig.name : '', desc: orig ? (orig.description || '') : '', group: orig ? (orig.group_name || '') : '',
      source: orig && orig.source === 'git' ? 'git' : 'inline', content: orig ? (orig.content || '') : SAMPLE,
      git: { url: orig ? orig.git_url || '' : '', branch: orig ? orig.git_branch || '' : '', path: orig ? orig.git_path || '' : '', auth: orig && orig.git_auth ? orig.git_auth : 'none', token: '', keyId: orig ? orig.git_key_id || 0 : 0, tokenSet: !!(orig && orig.git_token_set) },
      extra: {}, survey: null, sub: orig && orig.source === 'git' ? 'playbook' : 'raw', vq: '', vcol: {}, detected: [], analysis: '', busy: false, error: '', keys: [], baseline: '',
    };
    if (orig && orig.extra_vars) { try { const p = JSON.parse(orig.extra_vars); if (p && typeof p === 'object' && !Array.isArray(p)) S.extra = p; } catch (e) { /* ignore a malformed stored value */ } }
    let cm = null, analyzeTimer = null, analyzeSeq = 0, destroyed = false, savedSurvey = null;
    const fs = { loaded: false, loading: false, tree: null, error: '', open: null, cm: null, edits: {}, filter: '', ro: false, msg: '' };

    const q = function (s) { return container.querySelector(s); };
    const serialize = function () {
      return JSON.stringify([S.name, S.desc, S.group, S.source, S.source === 'inline' ? (cm ? cm.getValue() : S.content) : '', S.git.url, S.git.branch, S.git.path, S.git.auth, S.git.token, S.git.keyId, S.extra, S.survey, P.token()]);
    };
    const dirty = function () { return !destroyed && serialize() !== S.baseline; };
    let lastDirty = false;
    const notifyDirty = function () { const d = dirty(); const el = q('[data-ed="dirty"]'); if (el) el.hidden = !d; const sv = q('[data-ed="save"]'); if (sv) sv.disabled = S.busy || (!d && !isNew); const dc = q('[data-ed="discard"]'); if (dc) dc.disabled = !d; if (d !== lastDirty) { lastDirty = d; if (o.onDirty) o.onDirty(d); } };
    const P = Ans.editorParts.attach({ S: S, q: q, notifyDirty: notifyDirty, showError: function (m) { showError(m); }, newCM: newCM, goto: function (s) { switchSub(s); } });
    if (o.sub && subs().indexOf(o.sub) >= 0) S.sub = o.sub; // initial sub-tab from the caller; invalid ones keep the default

    /* ----- shell ----- */
    function shell() {
      const groups = Array.from(new Set((Ans.state.playbooks || []).map(function (p) { return (p.group_name || '').trim(); }).filter(Boolean))).sort();
      container.classList.add('ans-ed');
      container.innerHTML =
        '<div class="ans-ed-head"><div class="ans-grid-2"><div class="ans-field"><label for="ansiblePlaybookName">' + esc(T('ans_ed_name')) + ' <span class="ans-ln-req">*</span></label><input id="ansiblePlaybookName" class="form-input" data-f="name" maxlength="64" placeholder="' + esc(T('ans_ed_name_ph')) + '" value="' + esc(S.name) + '" autocomplete="off" /></div>' +
        '<div class="ans-field"><label for="ansiblePlaybookGroup">' + esc(T('ans_ed_group')) + '</label><input id="ansiblePlaybookGroup" class="form-input" data-f="group" list="ansEdGroups" placeholder="' + esc(T('ans_ed_group_ph')) + '" value="' + esc(S.group) + '" maxlength="64" autocomplete="off" /><datalist id="ansEdGroups">' + groups.map(function (g) { return '<option value="' + esc(g) + '"></option>'; }).join('') + '</datalist></div></div>' +
        '<div class="ans-field"><label for="ansiblePlaybookDesc">' + esc(T('ans_ed_desc')) + '</label><input id="ansiblePlaybookDesc" class="form-input" data-f="desc" value="' + esc(S.desc) + '" autocomplete="off" /></div>' +
        '<div class="ans-row"><span class="ans-label">' + esc(T('ans_ed_source')) + '</span><div class="ans-seg" role="group"><button type="button" data-src="inline" aria-pressed="' + (S.source === 'inline') + '"' + (isNew || S.source === 'inline' ? '' : '') + '>' + esc(T('ans_ed_src_inline')) + '</button><button type="button" data-src="git" aria-pressed="' + (S.source === 'git') + '">' + esc(T('ans_ed_src_git')) + '</button></div></div></div>' +
        '<div class="ans-ed-nav" role="tablist"><div class="ans-seg" data-ed="nav"></div></div><div class="ans-ed-body" data-ed="body"></div>' +
        '<div class="ans-drawer-foot"><span class="ans-err ans-ed-err" role="alert" data-ed="err"></span><span class="ans-spacer"></span><span class="ans-ed-dirty" data-ed="dirty" hidden>' + icon('circle-dot', 'icon-sm') + ' ' + esc(T('ans_ed_unsaved')) + '</span>' +
        '<button type="button" class="btn btn-sm" data-ed="discard" disabled>' + esc(T('ans_ed_discard')) + '</button><button type="button" id="btnAnsiblePlaybookSubmit" class="btn btn-sm btn-primary" data-ed="save">' + icon('save', 'icon-sm') + ' <span>' + esc(T('ans_ed_save')) + '</span> <kbd class="ans-kbd">Ctrl+S</kbd></button></div>';
      paintNav();
      paintSub();
      notifyDirty();
    }
    function subs() { return (S.source === 'inline' ? P.order : ['playbook']).concat(['vars', 'survey']).concat(S.source === 'git' ? ['files'] : []); }
    function paintNav() {
      q('[data-ed="nav"]').innerHTML = subs().map(function (s) { return '<button type="button" role="tab" data-sub="' + s + '" aria-pressed="' + (S.sub === s) + '">' + esc(P.has(s) || s === 'raw' ? T('ans_pt_' + s) : T('ans_ed_sub_' + s)) + '</button>'; }).join('');
    }
    // Leaving a part joins it into the raw YAML; entering one splits the raw YAML. Either may fail on invalid YAML,
    // in which case the editor stays where it is and shows why.
    async function switchSub(next) {
      if (next === S.sub || S.switching) return;
      S.switching = true;
      try {
        if (cm) S.content = cm.getValue();
        if (!(await P.leave(S.sub, next)) || !(await P.enter(next))) { paintSub(); return; } // leave() tore the editor down before it failed
        showError('');
        S.sub = next; paintNav(); paintSub();
      } finally { S.switching = false; }
    }
    async function switchSource(v) {
      if (cm) S.content = cm.getValue();
      if (!(await P.flush())) return;
      S.source = v;
      container.querySelectorAll('[data-src]').forEach(function (b) { b.setAttribute('aria-pressed', String(b.getAttribute('data-src') === v)); });
      if (S.source === 'git' && S.git.auth === 'ssh_key') loadKeys();
      paintNav(); paintSub(); notifyDirty();
    }
    function showError(msg) { S.error = msg || ''; const e = q('[data-ed="err"]'); if (e) e.textContent = S.error; }

    function paintSub() {
      if (cm && S.sub !== 'raw' && S.sub !== 'playbook') { S.content = cm.getValue(); }
      const body = q('[data-ed="body"]');
      destroyCM();
      if (!subs().includes(S.sub)) S.sub = S.source === 'git' ? 'playbook' : 'raw';
      if (S.sub === 'raw' || S.sub === 'playbook') paintPlaybook(body);
      else if (P.has(S.sub)) P.paint(S.sub, body);
      else if (S.sub === 'vars') paintVars(body);
      else if (S.sub === 'survey') paintSurvey(body);
      else paintFiles(body);
    }
    function destroyCM() { P.destroy(); if (cm) { try { S.content = cm.getValue(); cm.toTextArea(); } catch (e) { /* detached */ } cm = null; } if (fs.cm) { try { fs.cm.toTextArea(); } catch (e) { /* detached */ } fs.cm = null; } }

    /* ----- playbook panel ----- */
    function paintPlaybook(body) {
      if (S.source === 'inline') {
        body.innerHTML = '<div class="ans-ed-cm"><textarea id="ansiblePlaybookContent" data-ed="content"></textarea></div>';
        const ta = body.querySelector('textarea'); ta.value = S.content;
        cm = newCM(ta);
        if (cm) {
          cm.on('change', function () { notifyDirty(); scheduleAnalyze(); });
          setTimeout(function () { if (cm) cm.refresh(); }, 30);
        } else ta.addEventListener('input', function () { S.content = ta.value; notifyDirty(); });
        return;
      }
      const g = S.git, gitMissing = Ans.state.status && Ans.state.status.git_available === false;
      body.innerHTML = '<div class="ans-ed-scroll"><div class="ans-ed-form">' + (gitMissing ? '<div class="ans-error">' + icon('triangle-alert') + '<span>' + esc(T('ans_ed_git_missing')) + '</span></div>' : '') +
        '<div class="ans-field"><label for="ansibleGitUrl">' + esc(T('ans_ed_git_url')) + ' <span class="ans-ln-req">*</span></label><input id="ansibleGitUrl" class="form-input ans-mono" data-g="url" placeholder="' + esc(T('ans_ed_git_url_ph')) + '" value="' + esc(g.url) + '" autocomplete="off" /></div>' +
        '<div class="ans-grid-2"><div class="ans-field"><label for="ansibleGitBranch">' + esc(T('ans_ed_git_branch')) + '</label><input id="ansibleGitBranch" class="form-input ans-mono" data-g="branch" placeholder="' + esc(T('ans_ed_git_branch_ph')) + '" value="' + esc(g.branch) + '" /></div>' +
        '<div class="ans-field"><label for="ansibleGitPath">' + esc(T('ans_ed_git_path')) + ' <span class="ans-ln-req">*</span></label><input id="ansibleGitPath" class="form-input ans-mono" data-g="path" placeholder="' + esc(T('ans_ed_git_path_ph')) + '" value="' + esc(g.path) + '" /></div></div>' +
        '<div class="ans-grid-2"><div class="ans-field"><label for="ansibleGitAuth">' + esc(T('ans_ed_git_auth')) + '</label><select id="ansibleGitAuth" class="form-input" data-g="auth">' +
        [['none', 'ans_ed_auth_none'], ['token', 'ans_ed_auth_token'], ['ssh_key', 'ans_ed_auth_key']].map(function (a) { return '<option value="' + a[0] + '"' + (g.auth === a[0] ? ' selected' : '') + '>' + esc(T(a[1])) + '</option>'; }).join('') + '</select></div>' +
        (g.auth === 'token' ? '<div class="ans-field"><label for="ansibleGitToken">' + esc(T('ans_ed_git_token')) + '</label><input id="ansibleGitToken" type="password" class="form-input ans-mono" data-g="token" autocomplete="new-password" value="' + esc(g.token) + '" /><div class="hint">' + esc(g.tokenSet ? T('ans_ed_token_set') : T('ans_ed_token_new')) + '</div></div>' : '') +
        (g.auth === 'ssh_key' ? '<div class="ans-field"><label for="ansibleGitKeyId">' + esc(T('ans_ed_git_key')) + '</label><select id="ansibleGitKeyId" class="form-input" data-g="keyId">' + (S.keys.length ? S.keys.map(function (k) { return '<option value="' + Number(k.id) + '"' + (Number(k.id) === Number(g.keyId) ? ' selected' : '') + '>' + esc(k.name) + (k.fingerprint ? ' (' + esc(k.fingerprint.slice(0, 18)) + '…)' : '') + '</option>'; }).join('') : '<option value="0">' + esc(T('ans_ed_git_nokeys')) + '</option>') + '</select></div>' : '') + '</div>' +
        '<div class="ans-hint">' + icon('info', 'icon-sm') + ' ' + esc(T('ans_ed_git_notice')) + '</div>' +
        '<div class="ans-row"><button type="button" class="btn btn-sm" data-ed="sync"' + (id ? '' : ' disabled title="' + esc(T('ans_ed_sync_need_save')) + '"') + '>' + icon('refresh-cw', 'icon-sm') + ' ' + esc(T('ans_ed_sync')) + '</button>' +
        (orig && orig.last_sync_at ? '<span class="ans-muted ans-hint" data-ed="syncinfo">' + esc(T('ans_ed_last_sync', 'Last sync: {when} · {commit}', { when: Ans.fmt.ago(orig.last_sync_at), commit: (orig.last_sync_commit || '').slice(0, 8) })) + '</span>' : '<span class="ans-muted ans-hint" data-ed="syncinfo"></span>') + '</div></div></div>';
      if (g.auth === 'ssh_key' && !S.keys.length) loadKeys();
    }
    async function loadKeys() {
      try { const d = await Ans.api.get('/api/ssh-keys'); S.keys = Array.isArray(d) ? d : []; if (!S.git.keyId && S.keys[0]) { S.git.keyId = S.keys[0].id; } if (!destroyed && S.sub === 'playbook' && S.source === 'git') { paintSub(); notifyDirty(); } } catch (e) { S.keys = []; }
    }
    async function syncNow() {
      const b = q('[data-ed="sync"]'); if (b) b.disabled = true;
      try { const d = await Ans.api.post('/api/ansible/playbooks/' + id + '/git/sync'); const info = q('[data-ed="syncinfo"]'); const c = ((d && (d.commit || (d.sync && d.sync.commit))) || '').slice(0, 8); if (info) info.textContent = T('ans_ed_sync_ok', 'Synced at commit {c}', { c: c }); Ans.toast(T('ans_ed_sync_ok', 'Synced at commit {c}', { c: c }), 'success'); }
      catch (e) { Ans.toast(T('ans_ed_sync_failed') + ': ' + e.message, 'error'); }
      finally { if (b && !destroyed) b.disabled = false; }
    }

    /* ----- variables panel ----- */
    function scheduleAnalyze(immediate) {
      if (S.source !== 'inline') return;
      clearTimeout(analyzeTimer);
      analyzeTimer = setTimeout(runAnalyze, immediate ? 0 : 600);
    }
    async function runAnalyze() {
      if (S.source !== 'inline' || destroyed) return;
      const content = cm ? cm.getValue() : S.content;
      const seq = ++analyzeSeq;
      S.analysis = 'loading'; paintVarsStatus();
      try {
        const d = await Ans.api.post('/api/ansible/playbooks/analyze', { content: content, extra_vars: Object.keys(S.extra).length ? JSON.stringify(S.extra) : '' });
        if (seq !== analyzeSeq || destroyed) return;
        S.detected = (d && d.vars) || []; S.analysis = '';
      } catch (e) { if (seq !== analyzeSeq) return; S.analysis = T('ans_ed_vars_fail') + ': ' + e.message; }
      paintVarsStatus(); if (S.sub === 'vars') paintVarsTable();
    }
    async function analyzeGit() {
      if (!id) return;
      S.analysis = 'loading'; paintVarsStatus();
      try { const d = await Ans.api.get('/api/ansible/playbooks/' + id + '/contents'); S.detected = (d && d.vars) || []; S.analysis = ''; }
      catch (e) { S.analysis = T('ans_ed_vars_fail') + ': ' + e.message; }
      paintVarsStatus(); paintVarsTable();
    }
    function paintVars(body) {
      body.innerHTML = '<div class="ans-ed-scroll ans-ed-vars"><div class="ans-ed-form ans-ed-wide"><div class="ans-hint">' + esc(T('ans_ed_vars_hint')) + '</div><div class="ans-row" data-ed="vstatus"></div>' +
        '<div class="ans-row"><div class="search-box ans-ed-search">' + icon('search', 'icon-sm text-muted') + '<input id="ansiblePbVarSearch" class="form-input" data-vq placeholder="' + esc(T('ans_ed_v_search')) + '" aria-label="' + esc(T('ans_ed_v_search')) + '" value="' + esc(S.vq) + '" autocomplete="off" /></div><span class="ans-muted ans-hint" data-ed="vcount"></span></div>' +
        (S.source === 'git' ? '<div class="ans-row"><button type="button" class="btn btn-sm" data-ed="agit"' + (id ? '' : ' disabled') + '>' + icon('git-compare', 'icon-sm') + ' ' + esc(T('ans_ed_v_git_load')) + '</button><span class="ans-hint">' + esc(T('ans_ed_v_git_hint')) + '</span></div>' : '') +
        '<div class="ans-table-wrap"><table class="ans-table ans-vtable"><thead><tr><th scope="col">' + esc(T('ans_ed_v_name')) + '</th><th scope="col">' + esc(T('ans_ed_v_status')) + '</th><th scope="col">' + esc(T('ans_ed_v_value')) + '</th><th scope="col" class="ans-vc-src">' + esc(T('ans_ed_v_source')) + '</th><th scope="col" class="ans-vc-used">' + esc(T('ans_ed_v_used')) + '</th><th scope="col"></th></tr></thead><tbody id="ansiblePbVarsTableBody"></tbody></table></div>' +
        '<div class="ans-row ans-ed-addvar"><input id="ansiblePbNewVarName" class="form-input ans-mono" data-nv="name" placeholder="' + esc(T('ans_ed_v_new_name')) + '" style="width:180px" /><input id="ansiblePbNewVarValue" class="form-input ans-mono" data-nv="value" placeholder="' + esc(T('ans_ed_v_new_value')) + '" style="flex:1;min-width:140px" /><button type="button" class="btn btn-sm" data-ed="addvar">' + icon('plus', 'icon-sm') + ' ' + esc(T('ans_ed_v_add')) + '</button></div><div class="ans-err" id="ansiblePbNewVarError"></div></div></div>';
      paintVarsStatus(); paintVarsTable();
      if (S.source === 'inline' && !S.detected.length && S.analysis !== 'loading') scheduleAnalyze(true);
    }
    function paintVarsStatus() {
      const el = q('[data-ed="vstatus"]'); if (!el) return;
      el.innerHTML = S.analysis === 'loading' ? '<span class="ans-muted">' + icon('loader-circle', 'icon-sm') + ' ' + esc(T('ans_ed_vars_loading')) + '</span>' : S.analysis ? '<span class="ans-err">' + esc(S.analysis) + '</span>' : '';
    }
    function varRows() {
      const rows = new Map();
      S.detected.forEach(function (v) { if (v && v.name) rows.set(v.name, Object.assign({}, v)); });
      Object.keys(S.extra).forEach(function (k) {
        if (rows.has(k)) { const r = rows.get(k); r.hasOverride = true; r.overrideValue = S.extra[k]; }
        else rows.set(k, { name: k, status: 'defined', default: '', source: 'extra_vars', used_in: [], hasOverride: true, overrideValue: S.extra[k] });
      });
      return Array.from(rows.values()).sort(function (a, b) { return a.name.localeCompare(b.name); });
    }
    function varRowHtml(r, label) {
      const miss = r.status === 'required' && !r.hasOverride;
      const inYaml = !r.hasOverride && r.status === 'defined' && / vars$/.test(r.source || '') && S.source === 'inline';
      const used = r.used_in || [];
      const ph = r.default !== undefined && r.default !== '' ? formatVarValue(r.default) : (r.status === 'required' ? T('ans_ed_v_required') : '');
      return '<tr' + (miss ? ' class="ans-rn-badrow"' : '') + '><td class="ans-mono" style="font-weight:600">' + esc(r.name) + '</td><td><span class="badge ' + (miss ? 'badge-danger' : r.status === 'defined' ? 'badge-healthy' : 'badge-unknown') + '">' + esc(label[r.status] || r.status) + '</span></td>' +
        '<td><input class="form-input ans-mono" data-var="' + esc(r.name) + '" value="' + esc(r.hasOverride ? formatVarValue(r.overrideValue) : '') + '" placeholder="' + esc(ph) + '" aria-label="' + esc(r.name) + '" style="width:100%;height:26px;font-size:var(--fs-xs);' + (miss ? 'border-color:var(--danger);' : '') + '" /></td>' +
        '<td class="ans-mono ans-muted ans-vc-src">' + esc(r.hasOverride ? (r.source ? T('ans_ed_v_overridden', '{s} (overridden)', { s: r.source }) : 'extra_vars') : (r.source || '')) + '</td>' +
        '<td class="ans-mono ans-muted ans-vc-used" title="' + esc(used.join('\n')) + '">' + esc(used.slice(0, 3).join(', ')) + (used.length > 3 ? ' +' + (used.length - 3) : '') + '</td>' +
        '<td style="text-align:right"><button type="button" class="btn btn-sm btn-ghost ans-btn-icon" data-delvar="' + esc(r.name) + '" title="' + esc(r.hasOverride ? T('ans_ed_v_del_override') : inYaml ? T('ans_ed_v_del_yaml') : T('ans_ed_v_del_none')) + '" aria-label="' + esc(T('ans_ed_v_del_override')) + '"' + (r.hasOverride || inYaml ? '' : ' disabled') + '>' + icon('trash-2', 'icon-sm') + '</button></td></tr>';
    }
    // Variables are listed under the playbook's own and one section per role; the search box filters all of them.
    function paintVarsTable() {
      const tb = q('#ansiblePbVarsTableBody'); if (!tb) return;
      const all = varRows();
      const needle = (S.vq || '').trim().toLowerCase();
      const rows = !needle ? all : all.filter(function (r) {
        return [r.name, r.role || '', r.source || '', formatVarValue(r.hasOverride ? r.overrideValue : r.default), (r.used_in || []).join(' ')].join('\n').toLowerCase().indexOf(needle) >= 0;
      });
      const cnt = q('[data-ed="vcount"]'); if (cnt) cnt.textContent = needle ? T('ans_ed_v_count', '{n} of {m}', { n: rows.length, m: all.length }) : '';
      if (!rows.length) { tb.innerHTML = '<tr><td colspan="6" class="ans-muted" style="text-align:center">' + esc(needle ? T('ans_ed_v_nomatch', 'No variable matches “{q}”.', { q: S.vq.trim() }) : T('ans_ed_vars_empty')) + '</td></tr>'; return; }
      const label = { required: T('ans_ed_v_required'), optional: T('ans_ed_v_optional'), defined: T('ans_ed_v_defined') };
      const groups = new Map([['', []]]);
      rows.forEach(function (r) { const k = r.role || ''; if (!groups.has(k)) groups.set(k, []); groups.get(k).push(r); });
      const keys = Array.from(groups.keys()).filter(function (k) { return groups.get(k).length; }).sort(function (a, b) { return a === '' ? -1 : b === '' ? 1 : a.localeCompare(b); });
      const headed = Array.from(groups.keys()).some(function (k) { return k !== '' && groups.get(k).length; });
      tb.innerHTML = keys.map(function (k) {
        const open = !headed || !!needle || !S.vcol['g:' + k]; // a search shows every match, so groups open while it is active
        const head = headed ? '<tr class="ans-var-group"><th colspan="6" scope="colgroup"><button type="button" class="ans-var-toggle" data-vg="' + esc(k) + '" aria-expanded="' + open + '"' + (needle ? ' disabled' : '') + '>' + icon(open ? 'chevron-down' : 'chevron-right', 'icon-sm') + ' ' +
          (k ? icon('package', 'icon-sm') + ' ' + esc(T('ans_ed_v_g_role', 'Role {r}', { r: k })) : esc(T('ans_ed_v_g_play', 'Playbook variables'))) + ' <span class="ans-muted">' + groups.get(k).length + '</span></button></th></tr>' : '';
        return head + (open ? groups.get(k).map(function (r) { return varRowHtml(r, label); }).join('') : '');
      }).join('');
    }
    function toggleVarGroup(k) { S.vcol['g:' + k] = !S.vcol['g:' + k]; paintVarsTable(); }
    function deleteVar(name) {
      if (Object.prototype.hasOwnProperty.call(S.extra, name)) delete S.extra[name];
      else {
        const text = cm ? cm.getValue() : S.content;
        const next = removeYamlVar(text, name);
        if (next === null) { Ans.toast(T('ans_ed_v_yaml_missing'), 'error'); return; }
        S.content = next; if (cm) cm.setValue(next);
      }
      paintVarsTable(); notifyDirty(); scheduleAnalyze();
    }
    function addVar() {
      const nm = q('#ansiblePbNewVarName'), vl = q('#ansiblePbNewVarValue'), er = q('#ansiblePbNewVarError');
      er.textContent = '';
      const name = (nm.value || '').trim();
      if (!VAR_RE.test(name)) { er.textContent = T('ans_ed_v_err_name'); return; }
      if (Object.prototype.hasOwnProperty.call(S.extra, name)) { er.textContent = T('ans_ed_v_err_dup'); return; }
      S.extra[name] = parseVarValue(vl.value); nm.value = ''; vl.value = '';
      paintVarsTable(); notifyDirty(); scheduleAnalyze();
    }

    /* ----- survey panel ----- */
    function ensureSurvey() {
      if (S.survey !== null) return Promise.resolve();
      if (isNew) { S.survey = []; savedSurvey = '[]'; S.baseline = serialize(); return Promise.resolve(); }
      return Ans.api.get('/api/ansible/playbooks/' + id + '/survey').then(function (d) {
        S.survey = ((d && d.survey) || []).map(normField); savedSurvey = JSON.stringify(S.survey); S.baseline = serialize();
      }).catch(function () { S.survey = []; savedSurvey = '[]'; S.baseline = serialize(); });
    }
    function normField(f) { return Object.assign({ key: '', label: '', type: 'text', description: '', required: false, options: [], default: null, min: null, max: null }, f, { options: f.options || [], min: f.min === undefined ? null : f.min, max: f.max === undefined ? null : f.max, default: f.default === undefined ? null : f.default }); }
    function paintSurvey(body) {
      body.innerHTML = '<div class="ans-ed-scroll"><div class="ans-ed-form"><div class="ans-hint">' + esc(T('ans_ed_sv_hint')) + '</div><div data-ed="svlist">' + Ans.ui.skeleton(2) + '</div></div></div>';
      ensureSurvey().then(function () { if (!destroyed && S.sub === 'survey') { paintSurveyList(); notifyDirty(); } });
    }
    function paintSurveyList() {
      const box = q('[data-ed="svlist"]'); if (!box) return;
      const list = S.survey || [];
      box.innerHTML = (list.length ? list.map(function (f, i) { return surveyCard(f, i, list.length); }).join('') : '<div class="ans-muted ans-ed-empty">' + esc(T('ans_ed_sv_empty')) + '</div>') +
        '<div><button type="button" class="btn btn-sm" data-sv="add"' + (list.length >= 30 ? ' disabled' : '') + '>' + icon('plus', 'icon-sm') + ' ' + esc(T('ans_ed_sv_add')) + '</button></div>';
    }
    function surveyCard(f, i, n) {
      const sel = f.type === 'select' || f.type === 'multiselect', txt = f.type === 'text' || f.type === 'textarea' || f.type === 'password', num = f.type === 'number' || f.type === 'integer';
      let def = '';
      if (f.type === 'boolean') def = '<select class="form-input" data-sf="default"><option value="">' + esc(T('ans_ed_sv_none')) + '</option><option value="true"' + (f.default === true ? ' selected' : '') + '>' + esc(T('ans_ed_sv_true')) + '</option><option value="false"' + (f.default === false ? ' selected' : '') + '>' + esc(T('ans_ed_sv_false')) + '</option></select>';
      else if (f.type === 'select') def = '<select class="form-input" data-sf="default"><option value="">' + esc(T('ans_ed_sv_none')) + '</option>' + (f.options || []).map(function (o) { return '<option value="' + esc(o) + '"' + (f.default === o ? ' selected' : '') + '>' + esc(o) + '</option>'; }).join('') + '</select>';
      else if (f.type === 'multiselect') def = '<div class="ans-ln-multi">' + (f.options || []).map(function (o) { return '<label class="ans-ln-opt"><input type="checkbox" data-sf-multi value="' + esc(o) + '"' + (Array.isArray(f.default) && f.default.indexOf(o) >= 0 ? ' checked' : '') + ' /> ' + esc(o) + '</label>'; }).join('') + '</div>';
      else def = '<input class="form-input" data-sf="default" type="' + (num ? 'number' : 'text') + '" value="' + esc(f.default === null || f.default === undefined ? '' : f.default) + '" />';
      return '<div class="ans-card ans-ed-sv" data-i="' + i + '"><div class="ans-row"><strong class="ans-mono">' + esc(f.key || '…') + '</strong><span class="ans-spacer"></span>' +
        '<button type="button" class="btn btn-sm btn-ghost ans-btn-icon" data-sv="up" title="' + esc(T('ans_ed_sv_up')) + '" aria-label="' + esc(T('ans_ed_sv_up')) + '"' + (i === 0 ? ' disabled' : '') + '>' + icon('arrow-up', 'icon-sm') + '</button>' +
        '<button type="button" class="btn btn-sm btn-ghost ans-btn-icon" data-sv="down" title="' + esc(T('ans_ed_sv_down')) + '" aria-label="' + esc(T('ans_ed_sv_down')) + '"' + (i === n - 1 ? ' disabled' : '') + '>' + icon('arrow-down', 'icon-sm') + '</button>' +
        '<button type="button" class="btn btn-sm btn-ghost ans-btn-icon" data-sv="del" title="' + esc(T('ans_ed_sv_del')) + '" aria-label="' + esc(T('ans_ed_sv_del')) + '">' + icon('trash-2', 'icon-sm') + '</button></div>' +
        '<div class="ans-grid-2"><div class="ans-field"><label>' + esc(T('ans_ed_sv_key')) + '</label><input class="form-input ans-mono" data-sf="key" value="' + esc(f.key) + '" maxlength="64" /></div>' +
        '<div class="ans-field"><label>' + esc(T('ans_ed_sv_label')) + '</label><input class="form-input" data-sf="label" value="' + esc(f.label) + '" maxlength="200" /></div>' +
        '<div class="ans-field"><label>' + esc(T('ans_ed_sv_type')) + '</label><select class="form-input" data-sf="type">' + TYPES.map(function (t) { return '<option value="' + t + '"' + (f.type === t ? ' selected' : '') + '>' + esc(T('ans_ed_sv_t_' + t)) + '</option>'; }).join('') + '</select></div>' +
        '<div class="ans-field"><label>' + esc(T('ans_ed_sv_default')) + '</label>' + def + '</div>' +
        (sel ? '<div class="ans-field ans-ln-wide"><label>' + esc(T('ans_ed_sv_options')) + '</label><textarea class="form-input ans-mono" rows="3" data-sf="options">' + esc((f.options || []).join('\n')) + '</textarea></div>' : '') +
        (txt || num ? '<div class="ans-field"><label>' + esc(txt ? T('ans_ed_sv_minlen') : T('ans_ed_sv_min')) + '</label><input class="form-input" type="number" data-sf="min" value="' + esc(f.min === null ? '' : f.min) + '" /></div><div class="ans-field"><label>' + esc(txt ? T('ans_ed_sv_maxlen') : T('ans_ed_sv_max')) + '</label><input class="form-input" type="number" data-sf="max" value="' + esc(f.max === null ? '' : f.max) + '" /></div>' : '') +
        '<div class="ans-field ans-ln-wide"><label>' + esc(T('ans_ed_sv_desc')) + '</label><input class="form-input" data-sf="description" value="' + esc(f.description) + '" maxlength="1000" /></div></div>' +
        '<label class="ans-row" style="cursor:pointer"><span class="switch"><input type="checkbox" data-sf="required"' + (f.required ? ' checked' : '') + ' /><span class="switch-track"><span class="switch-thumb"></span></span></span><span>' + esc(T('ans_ed_sv_req')) + '</span></label></div>';
    }
    function onSurveyInput(e) {
      const card = e.target.closest('.ans-ed-sv'); if (!card) return;
      const f = S.survey[Number(card.getAttribute('data-i'))]; if (!f) return;
      const k = e.target.getAttribute('data-sf');
      if (e.target.hasAttribute('data-sf-multi')) { f.default = Array.from(card.querySelectorAll('[data-sf-multi]:checked')).map(function (c) { return c.value; }); notifyDirty(); return; }
      if (!k) return;
      const v = e.target.type === 'checkbox' ? e.target.checked : e.target.value;
      if (k === 'required') f.required = v;
      else if (k === 'options') { f.options = String(v).split('\n').map(function (s) { return s.trim(); }).filter(Boolean); if (e.type === 'change') paintSurveyList(); }
      else if (k === 'min' || k === 'max') f[k] = v === '' ? null : Number(v);
      else if (k === 'default') f.default = f.type === 'boolean' ? (v === '' ? null : v === 'true') : (v === '' ? null : ((f.type === 'number' || f.type === 'integer') ? Number(v) : v));
      else if (k === 'type') { f.type = v; f.default = null; f.options = (v === 'select' || v === 'multiselect') ? f.options : []; if (!(v === 'text' || v === 'textarea' || v === 'password' || v === 'number' || v === 'integer')) { f.min = null; f.max = null; } paintSurveyList(); }
      else { f[k] = v; if (k === 'key') { const t = card.querySelector('strong'); if (t) t.textContent = v || '…'; } }
      notifyDirty();
    }
    function surveyClick(btn) {
      const a = btn.getAttribute('data-sv');
      if (a === 'add') { if (S.survey.length >= 30) return; S.survey.push(normField({ key: 'var_' + (S.survey.length + 1), label: '', type: 'text' })); }
      else {
        const i = Number(btn.closest('.ans-ed-sv').getAttribute('data-i'));
        if (a === 'del') S.survey.splice(i, 1);
        else if (a === 'up' && i > 0) S.survey.splice(i - 1, 0, S.survey.splice(i, 1)[0]);
        else if (a === 'down' && i < S.survey.length - 1) S.survey.splice(i + 1, 0, S.survey.splice(i, 1)[0]);
      }
      paintSurveyList(); notifyDirty();
    }
    function surveyPayload() {
      if (!S.survey) return { fields: null };
      if (S.survey.length > 30) return { error: T('ans_ed_sv_err_max') };
      const seen = new Set(), out = [];
      for (let i = 0; i < S.survey.length; i++) {
        const f = S.survey[i];
        if (!KEY_RE.test(f.key)) return { error: T('ans_ed_sv_err_key', 'Field {n}: invalid key.', { n: i + 1 }) };
        if (seen.has(f.key)) return { error: T('ans_ed_sv_err_dup', 'Duplicate survey key "{k}".', { k: f.key }) };
        seen.add(f.key);
        const sel = f.type === 'select' || f.type === 'multiselect';
        if (sel && (!f.options.length || f.options.length > 50 || new Set(f.options).size !== f.options.length)) return { error: T('ans_ed_sv_err_opts', 'Field "{k}": options need 1-50 unique entries.', { k: f.key }) };
        if (f.min !== null && f.max !== null && f.min > f.max) return { error: T('ans_ed_sv_err_bounds', 'Field "{k}": min must not exceed max.', { k: f.key }) };
        if (f.type === 'select' && f.default !== null && f.options.indexOf(f.default) < 0) return { error: T('ans_ed_sv_err_default', 'Field "{k}": invalid default.', { k: f.key }) };
        const o2 = { key: f.key, label: f.label, type: f.type, description: f.description, required: !!f.required };
        if (sel) o2.options = f.options;
        if (f.default !== null && f.default !== undefined && !(Array.isArray(f.default) && !f.default.length)) o2.default = f.default;
        if (f.min !== null && !sel && f.type !== 'boolean') o2.min = f.min;
        if (f.max !== null && !sel && f.type !== 'boolean') o2.max = f.max;
        out.push(o2);
      }
      return { fields: out };
    }

    /* ----- files panel (git) ----- */
    function validPath(p) { return !!p && p.length <= 400 && !/[\\\x00-\x1f]/.test(p) && !p.split('/').some(function (x) { return !x || x === '.' || x === '..' || x === '.git'; }); }
    function pendingCount() { return Object.keys(fs.edits).length; }
    function paintFiles(body) {
      if (!id) { body.innerHTML = Ans.ui.empty({ icon: 'git-compare', title: T('ans_ed_f_need_save') }); return; }
      body.innerHTML = '<div class="ans-ed-files"><div class="ans-ed-ftree"><div class="ans-row"><div class="search-box" style="flex:1;margin:0"><input class="form-input" data-fl="filter" placeholder="' + esc(T('ans_ed_f_filter')) + '" aria-label="' + esc(T('ans_ed_f_filter')) + '" value="' + esc(fs.filter) + '" style="height:28px;font-size:var(--fs-xs);width:100%" /></div>' +
        '<button type="button" class="btn btn-sm btn-ghost ans-btn-icon" data-fl="reload" title="' + esc(T('ans_ed_f_reload')) + '" aria-label="' + esc(T('ans_ed_f_reload')) + '">' + icon('refresh-cw', 'icon-sm') + '</button></div>' +
        '<div class="ans-row"><input class="form-input ans-mono" data-fl="newpath" placeholder="' + esc(T('ans_ed_f_path_ph')) + '" style="flex:1;height:28px;font-size:var(--fs-xs)" /><button type="button" class="btn btn-sm" data-fl="new">' + icon('file-plus', 'icon-sm') + '</button></div><div class="ans-err" data-fl="newerr"></div>' +
        '<div class="ans-ed-flist" data-fl="list"></div></div><div class="ans-ed-fview" data-fl="view"></div></div>' +
        '<div class="ans-ed-fbar" data-fl="bar"></div>';
      if (!fs.loaded && !fs.loading) loadTree(); else { paintTree(); paintFileView(); }
      paintBar();
    }
    async function loadTree() {
      fs.loading = true; fs.error = '';
      const list = q('[data-fl="list"]'); if (list) list.innerHTML = '<div class="ans-muted ans-ed-empty">' + icon('loader-circle', 'icon-sm') + ' ' + esc(T('ans_ed_f_loading')) + '</div>';
      try { fs.tree = await Ans.api.get('/api/ansible/playbooks/' + id + '/git/files'); fs.loaded = true; }
      catch (e) { fs.error = T('ans_ed_f_failed') + ': ' + e.message; }
      fs.loading = false;
      if (!destroyed && S.sub === 'files') { paintTree(); paintFileView(); paintBar(); }
    }
    function paintTree() {
      const list = q('[data-fl="list"]'); if (!list) return;
      if (fs.error) { list.innerHTML = Ans.ui.error(fs.error, 'data-fl="reload"'); return; }
      if (!fs.tree) return;
      const needle = fs.filter.trim().toLowerCase();
      const files = (fs.tree.files || []).map(function (f) { return f.path; });
      Object.keys(fs.edits).forEach(function (p) { if (files.indexOf(p) < 0) files.push(p); });
      files.sort();
      const shown = files.filter(function (p) { return !needle || p.toLowerCase().indexOf(needle) >= 0; });
      list.innerHTML = (fs.tree.truncated ? '<div class="ans-hint">' + esc(T('ans_ed_f_truncated')) + '</div>' : '') + shown.map(function (p) {
        const e = fs.edits[p], tag = e ? (e.del ? ['del', T('ans_ed_f_del_tag')] : e.isNew ? ['new', T('ans_ed_f_new_tag')] : ['mod', T('ans_ed_f_mod_tag')]) : null;
        return '<button type="button" class="ans-ed-fi' + (fs.open === p ? ' on' : '') + (tag && tag[0] === 'del' ? ' del' : '') + '" data-file="' + esc(p) + '" title="' + esc(p) + '">' + icon('file', 'icon-sm') + '<span>' + esc(p) + '</span>' + (tag ? '<i class="ans-ed-ft ' + tag[0] + '">' + esc(tag[1]) + '</i>' : '') + '</button>';
      }).join('');
    }
    async function openFile(path) {
      if (fs.cm) { commitCurrentEdit(); }
      fs.open = path; fs.ro = false; fs.msg = '';
      paintTree();
      const view = q('[data-fl="view"]'); if (!view) return;
      const e = fs.edits[path];
      if (e) { paintFileView(); return; }
      view.innerHTML = '<div class="ans-muted ans-ed-empty">' + icon('loader-circle', 'icon-sm') + ' …</div>';
      try { const d = await Ans.api.get('/api/ansible/playbooks/' + id + '/git/file?path=' + encodeURIComponent(path)); fs.files = fs.files || {}; fs.files[path] = d.content; }
      catch (er) { fs.ro = true; fs.msg = er.status === 415 ? T('ans_ed_f_binary') : er.status === 413 ? T('ans_ed_f_too_big') : er.message; }
      if (!destroyed && fs.open === path) paintFileView();
    }
    function currentText() { return fs.cm ? fs.cm.getValue() : null; }
    function commitCurrentEdit() {
      const p = fs.open, cur = currentText();
      if (!p || cur === null || fs.ro) return;
      const e = fs.edits[p];
      if (e && e.del) return;
      const base = e && e.isNew ? '' : ((fs.files || {})[p] || '');
      if (e && e.isNew) e.content = cur; else if (cur !== base) fs.edits[p] = { content: cur }; else delete fs.edits[p];
    }
    function paintFileView() {
      const view = q('[data-fl="view"]'); if (!view) return;
      if (fs.cm) { try { fs.cm.toTextArea(); } catch (e) { /* detached */ } fs.cm = null; }
      const p = fs.open;
      if (!p) { view.innerHTML = '<div class="ans-muted ans-ed-empty">' + esc(T('ans_ed_f_none')) + '</div>'; return; }
      const e = fs.edits[p];
      const text = e && !e.del ? e.content : ((fs.files || {})[p] !== undefined ? fs.files[p] : '');
      const del = e && e.del;
      view.innerHTML = '<div class="ans-ed-fhead"><span class="ans-mono ans-ed-fpath">' + esc(p) + '</span><span class="ans-spacer"></span>' +
        (e && !e.isNew ? '<button type="button" class="btn btn-sm btn-ghost" data-fl="revert">' + icon('rotate-ccw', 'icon-sm') + ' ' + esc(T('ans_ed_f_revert')) + '</button>' : '') +
        '<button type="button" class="btn btn-sm ' + (del ? '' : 'btn-ghost') + '" data-fl="del">' + icon(del ? 'rotate-ccw' : 'trash-2', 'icon-sm') + ' ' + esc(del ? T('ans_ed_f_undelete') : T('ans_ed_f_delete')) + '</button></div>' +
        (fs.ro ? '<div class="ans-error" style="margin:8px">' + icon('triangle-alert') + '<span>' + esc(fs.msg) + '</span></div>' : del ? '<div class="ans-muted ans-ed-empty">' + esc(T('ans_ed_f_marked')) + '</div>' : '<div class="ans-ed-cm"><textarea data-fl="area"></textarea></div>');
      const ta = view.querySelector('[data-fl="area"]');
      if (ta) {
        ta.value = text;
        fs.cm = newCM(ta, { mode: cmMode(p) });
        if (fs.cm) { fs.cm.on('change', function () { const st = fs.edits[p]; const cur = fs.cm.getValue(); const base = st && st.isNew ? '' : ((fs.files || {})[p] || ''); if (st && st.isNew) st.content = cur; else if (cur !== base) fs.edits[p] = { content: cur }; else delete fs.edits[p]; paintTreeLight(); paintBar(); }); setTimeout(function () { if (fs.cm) fs.cm.refresh(); }, 30); }
      }
    }
    function paintTreeLight() { paintTree(); }
    function paintBar() {
      const bar = q('[data-fl="bar"]'); if (!bar) return;
      const n = pendingCount();
      bar.innerHTML = '<span class="ans-muted ans-hint">' + (fs.tree ? esc((fs.tree.branch || '') + ' @ ' + (fs.tree.commit || '').slice(0, 8)) : '') + '</span><span class="ans-spacer"></span>' + (n ? '<span class="ans-hint">' + esc(T('ans_ed_f_changes', '{n} pending changes', { n: n })) + '</span>' : '') +
        '<button type="button" class="btn btn-sm btn-primary" data-fl="commit"' + (n ? '' : ' disabled') + '>' + icon('upload', 'icon-sm') + ' ' + esc(T('ans_ed_f_commit')) + '</button>';
    }
    function newFile() {
      const inp = q('[data-fl="newpath"]'), er = q('[data-fl="newerr"]');
      const p = (inp.value || '').trim().replace(/^\/+/, ''); er.textContent = '';
      if (!validPath(p)) { er.textContent = T('ans_ed_f_bad_path'); return; }
      if (((fs.tree && fs.tree.files) || []).some(function (f) { return f.path === p; }) || (fs.edits[p] && !fs.edits[p].del)) { er.textContent = T('ans_ed_f_exists'); return; }
      commitCurrentEdit();
      fs.edits[p] = { content: '', isNew: true }; inp.value = ''; fs.open = p; fs.ro = false;
      paintTree(); paintFileView(); paintBar();
    }
    function toggleDelete() {
      const p = fs.open; if (!p) return;
      commitCurrentEdit();
      const e = fs.edits[p];
      if (e && e.isNew) delete fs.edits[p]; else if (e && e.del) delete fs.edits[p]; else fs.edits[p] = { del: true };
      if (!fs.edits[p] && e && e.isNew) fs.open = null;
      paintTree(); paintFileView(); paintBar();
    }
    function commitDialog() {
      commitCurrentEdit();
      const n = pendingCount();
      if (!n) { Ans.toast(T('ans_ed_c_nochanges'), 'warning'); return; }
      const old = document.getElementById('ansEdCommitModal'); if (old) old.remove();
      const m = document.createElement('div'); m.id = 'ansEdCommitModal'; m.className = 'modal';
      m.innerHTML = '<div class="modal-content modal-dialog" style="max-width:560px"><div class="modal-header"><h3 style="font-size:var(--fs-md);margin:0">' + esc(T('ans_ed_c_title')) + '</h3><button type="button" class="btn btn-icon btn-sm btn-ghost" data-c="close" aria-label="' + esc(T('ans_close')) + '">' + icon('x', 'icon-sm') + '</button></div>' +
        '<div class="modal-body" style="display:flex;flex-direction:column;gap:10px"><div class="ans-hint">' + esc(T('ans_ed_f_changes', '{n} pending changes', { n: n })) + ': ' + Object.keys(fs.edits).map(function (p) { return '<code class="ans-mono">' + esc(p) + '</code>'; }).join(', ') + '</div>' +
        '<div class="ans-field"><label for="ansEdCMsg">' + esc(T('ans_ed_c_msg')) + ' <span class="ans-ln-req">*</span></label><input id="ansEdCMsg" class="form-input" placeholder="' + esc(T('ans_ed_c_msg_ph')) + '" maxlength="500" /></div>' +
        '<div class="ans-field"><label for="ansEdCBranch">' + esc(T('ans_ed_c_branch')) + '</label><input id="ansEdCBranch" class="form-input ans-mono" placeholder="' + esc(T('ans_ed_c_branch_ph')) + '" /></div>' +
        '<div class="ans-grid-2"><div class="ans-field"><label for="ansEdCAuthor">' + esc(T('ans_ed_c_author')) + '</label><input id="ansEdCAuthor" class="form-input" /></div><div class="ans-field"><label for="ansEdCEmail">' + esc(T('ans_ed_c_email')) + '</label><input id="ansEdCEmail" class="form-input" type="email" /></div></div>' +
        '<div class="ans-hint">' + icon('info', 'icon-sm') + ' ' + esc(T('ans_ed_c_note')) + '</div><div class="ans-error" data-c="err" hidden></div></div>' +
        '<div class="modal-footer" style="display:flex;gap:8px;justify-content:flex-end"><button type="button" class="btn btn-sm" data-c="close">' + esc(T('ans_ed_c_cancel')) + '</button><button type="button" class="btn btn-sm btn-primary" data-c="go">' + esc(T('ans_ed_c_go')) + '</button></div></div>';
      document.body.appendChild(m);
      const close = function () { closeModal('ansEdCommitModal'); setTimeout(function () { m.remove(); }, 200); };
      m.addEventListener('click', async function (e) {
        const b = e.target.closest('[data-c]');
        if (!b) { if (e.target === m) close(); return; }
        if (b.getAttribute('data-c') === 'close') { close(); return; }
        const err = m.querySelector('[data-c="err"]'); err.hidden = true;
        const msg = m.querySelector('#ansEdCMsg').value.trim();
        if (!msg) { err.textContent = T('ans_ed_c_msg_required'); err.hidden = false; return; }
        const body = { message: msg, base_commit: fs.tree ? fs.tree.commit : '', changes: Object.keys(fs.edits).map(function (p) { const x = fs.edits[p]; return x.del ? { path: p, content: '', delete: true } : { path: p, content: x.content }; }) };
        const br = m.querySelector('#ansEdCBranch').value.trim(), an = m.querySelector('#ansEdCAuthor').value.trim(), ae = m.querySelector('#ansEdCEmail').value.trim();
        if (br) body.new_branch = br; if (an) body.author_name = an; if (ae) body.author_email = ae;
        b.disabled = true;
        try {
          const d = await Ans.api.post('/api/ansible/playbooks/' + id + '/git/commit', body);
          Ans.toast(T('ans_ed_c_ok', '{c} pushed to {b}', { c: (d.commit || '').slice(0, 8), b: d.branch || '' }), 'success');
          fs.edits = {}; fs.files = {}; fs.loaded = false; fs.tree = null; fs.open = null; close();
          if (!destroyed && S.sub === 'files') paintSub();
        } catch (ex) {
          err.textContent = ex.status === 409 ? T('ans_ed_c_conflict') : T('ans_ed_c_failed') + ': ' + ex.message; err.hidden = false;
        } finally { b.disabled = false; }
      });
      openModal('ansEdCommitModal');
      setTimeout(function () { const i = m.querySelector('#ansEdCMsg'); if (i) i.focus(); }, 80);
    }

    /* ----- save ----- */
    async function save() {
      if (S.busy) return;
      if (!(await P.flush())) return;
      if (cm) S.content = cm.getValue();
      showError('');
      const name = S.name.trim();
      if (!name) { showError(T('ans_ed_err_name')); return; }
      if (name.length > 64) { showError(T('ans_ed_err_name_len')); return; }
      // Inline playbooks are checked and formatted server-side before saving; git ones are committed as authored.
      if (S.source === 'inline' && S.content.trim()) {
        try {
          const f = await Ans.api.post('/api/ansible/playbooks/format', { content: S.content });
          if (f && f.changed && typeof f.content === 'string') {
            S.content = f.content;
            if (cm) { // raw editor is showing: keep cursor and scroll as close as possible
              const cur = cm.getCursor(), sc = cm.getScrollInfo();
              cm.setValue(f.content);
              const ln = Math.min(cur.line, cm.lineCount() - 1);
              cm.setCursor({ line: ln, ch: Math.min(cur.ch, (cm.getLine(ln) || '').length) });
              cm.scrollTo(sc.left, sc.top);
            }
            if (P.has(S.sub)) { P.invalidate(); await P.enter(S.sub); paintSub(); } // part tab showing: re-derive it from the formatted text
            Ans.toast(T('ans_ed_formatted'), 'success');
          }
        } catch (e) {
          if (S.sub !== 'raw') await switchSub('raw'); // switchSub clears the error on success, so show it after
          showError(e.message);
          return; // abort the save, S.busy stays false
        }
      }
      const payload = { name: name, description: S.desc.trim(), source: S.source, group_name: S.group.trim(), extra_vars: Object.keys(S.extra).length ? JSON.stringify(S.extra) : '' };
      if (S.source === 'git') {
        Object.assign(payload, { git_url: S.git.url.trim(), git_branch: S.git.branch.trim(), git_path: S.git.path.trim(), git_auth: S.git.auth, git_token: S.git.token, git_key_id: Number(S.git.keyId) || 0 });
        if (!payload.git_url || !payload.git_path) { showError(T('ans_ed_err_git')); return; }
      } else {
        payload.content = S.content.trim();
        if (!payload.content) { showError(T('ans_ed_err_content')); return; }
      }
      const sp = surveyPayload();
      if (sp.error) { showError(sp.error); return; }
      S.busy = true; notifyDirty();
      const btn = q('[data-ed="save"] span'); if (btn) btn.textContent = T('ans_ed_saving');
      try {
        const d = id ? await Ans.api.put('/api/ansible/playbooks/' + id, payload) : await Ans.api.post('/api/ansible/playbooks', payload);
        const pb = d && d.playbook ? d.playbook : Object.assign({ id: id }, payload);
        id = pb.id;
        if (sp.fields && JSON.stringify(S.survey) !== savedSurvey) {
          try { await Ans.api.put('/api/ansible/playbooks/' + id + '/survey', { survey: sp.fields }); savedSurvey = JSON.stringify(S.survey); }
          catch (e) { showError(T('ans_ed_sv_save_failed') + ': ' + e.message); S.sub = 'survey'; S.busy = false; paintNav(); paintSub(); if (o.onSaved && isNew) { /* playbook exists now; keep the editor open on the survey */ } S.baseline = serialize(); notifyDirty(); Ans.load.playbooks().catch(function () { /* list refresh */ }); return; }
        }
        S.git.token = ''; S.git.tokenSet = S.git.auth === 'token' && (S.git.tokenSet || !!payload.git_token);
        S.baseline = serialize();
        Ans.toast(T('ans_ed_saved'), 'success');
        S.busy = false; notifyDirty();
        if (o.onSaved) o.onSaved(pb, { sub: S.sub });
      } catch (e) { showError(T('ans_ed_save_failed') + ': ' + e.message); S.busy = false; notifyDirty(); }
      finally { const b2 = q('[data-ed="save"] span'); if (b2) b2.textContent = T('ans_ed_save'); }
    }
    function discard() {
      if (!dirty()) return;
      if (o.onClose && isNew) { o.onClose(); return; }
      S.name = orig ? orig.name : ''; S.desc = orig ? orig.description || '' : ''; S.group = orig ? orig.group_name || '' : ''; S.source = orig && orig.source === 'git' ? 'git' : 'inline';
      S.content = orig ? orig.content || '' : SAMPLE; S.extra = {}; if (orig && orig.extra_vars) { try { S.extra = JSON.parse(orig.extra_vars) || {}; } catch (e) { S.extra = {}; } }
      S.git = { url: orig ? orig.git_url || '' : '', branch: orig ? orig.git_branch || '' : '', path: orig ? orig.git_path || '' : '', auth: orig && orig.git_auth ? orig.git_auth : 'none', token: '', keyId: orig ? orig.git_key_id || 0 : 0, tokenSet: !!(orig && orig.git_token_set) };
      S.survey = savedSurvey ? JSON.parse(savedSurvey) : null; showError('');
      P.reset(); S.sub = S.source === 'git' ? 'playbook' : 'raw'; S.vq = ''; destroyCM(); shell();
    }

    /* ----- events ----- */
    const onInput = function (e) {
      const t = e.target;
      if (P.input(e)) return;
      if (t.getAttribute && t.getAttribute('data-vq') !== null) { if (e.type === 'input') { S.vq = t.value; paintVarsTable(); } return; }
      const f = t.getAttribute && t.getAttribute('data-f');
      if (f) { S[f] = t.value; notifyDirty(); return; }
      const g = t.getAttribute && t.getAttribute('data-g');
      if (g) { S.git[g] = g === 'keyId' ? Number(t.value) : t.value; if (g === 'auth') { paintSub(); } notifyDirty(); return; }
      const v = t.getAttribute && t.getAttribute('data-var');
      if (v) { if (t.value === '') delete S.extra[v]; else S.extra[v] = parseVarValue(t.value); if (e.type === 'change') { paintVarsTable(); scheduleAnalyze(); } notifyDirty(); return; }
      if (t.hasAttribute && t.hasAttribute('data-fl') && t.getAttribute('data-fl') === 'filter') { fs.filter = t.value; paintTree(); return; }
      if (S.sub === 'survey') onSurveyInput(e);
    };
    const onClick = function (e) {
      if (P.click(e)) return;
      const src = e.target.closest('[data-src]');
      if (src) { const v = src.getAttribute('data-src'); if (v !== S.source) switchSource(v); return; }
      const sub = e.target.closest('[data-sub]');
      if (sub) { switchSub(sub.getAttribute('data-sub')); return; }
      const sv = e.target.closest('[data-sv]'); if (sv) { surveyClick(sv); return; }
      const vg = e.target.closest('[data-vg]'); if (vg) { toggleVarGroup(vg.getAttribute('data-vg')); return; }
      const dv = e.target.closest('[data-delvar]'); if (dv) { deleteVar(dv.getAttribute('data-delvar')); return; }
      const ed = e.target.closest('[data-ed]');
      if (ed) {
        const a = ed.getAttribute('data-ed');
        if (a === 'save') save(); else if (a === 'discard') discard(); else if (a === 'addvar') addVar(); else if (a === 'sync') syncNow(); else if (a === 'agit') analyzeGit();
        return;
      }
      const fi = e.target.closest('[data-file]'); if (fi) { openFile(fi.getAttribute('data-file')); return; }
      const fl = e.target.closest('[data-fl]');
      if (fl) {
        const a = fl.getAttribute('data-fl');
        if (a === 'reload') {
          const doReload = function () { fs.loaded = false; fs.files = {}; fs.edits = {}; fs.open = null; paintSub(); };
          if (!pendingCount()) doReload();
          else Ans.confirm({ title: T('ans_ed_f_reload'), message: T('ans_ed_f_changes', '{n} pending changes', { n: pendingCount() }), confirmText: T('ans_ed_f_reload'), danger: true }).then(function (ok) { if (ok) doReload(); });
        }
        else if (a === 'new') newFile(); else if (a === 'del') toggleDelete(); else if (a === 'commit') commitDialog();
        else if (a === 'revert') { delete fs.edits[fs.open]; paintTree(); paintFileView(); paintBar(); }
      }
    };
    const onKey = function (e) {
      if ((e.ctrlKey || e.metaKey) && (e.key === 's' || e.key === 'S')) { e.preventDefault(); save(); return; }
      if (e.key === 'Enter' && e.target.matches && e.target.matches('[data-nv]')) { e.preventDefault(); addVar(); }
      if (e.key === 'Enter' && e.target.matches && e.target.matches('[data-fl="newpath"]')) { e.preventDefault(); newFile(); }
    };
    const onBefore = function (e) { if (dirty()) { e.preventDefault(); e.returnValue = ''; } };
    container.addEventListener('input', onInput);
    container.addEventListener('change', onInput);
    container.addEventListener('click', onClick);
    container.addEventListener('keydown', onKey);
    window.addEventListener('beforeunload', onBefore);

    shell();
    S.baseline = serialize();
    if (!isNew) ensureSurvey().then(function () { S.baseline = serialize(); notifyDirty(); });
    if (isNew) { S.survey = []; savedSurvey = '[]'; S.baseline = serialize(); }
    notifyDirty();
    // A sub mount option pointing at a Parts tab needs its outline before the first meaningful paint; repaint once it lands.
    if (P.has(S.sub)) P.enter(S.sub).then(function () { if (!destroyed) paintSub(); });
    setTimeout(function () { const n = q('#ansiblePlaybookName'); if (n && isNew) n.focus(); }, 80);

    return {
      destroy: function () {
        destroyed = true; clearTimeout(analyzeTimer); destroyCM();
        window.removeEventListener('beforeunload', onBefore);
        container.removeEventListener('input', onInput); container.removeEventListener('change', onInput); container.removeEventListener('click', onClick); container.removeEventListener('keydown', onKey);
        container.classList.remove('ans-ed'); container.innerHTML = '';
        const cmod = document.getElementById('ansEdCommitModal'); if (cmod) cmod.remove();
      },
      isDirty: dirty, save: save, getSub: function () { return S.sub; },
    };
  }

  Ans.editor = { mount: mount, util: { removeYamlVar: removeYamlVar, parseVarValue: parseVarValue } };
})();
