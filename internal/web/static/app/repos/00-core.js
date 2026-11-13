/* Repositories (top-level section): Git repositories cloned into a folder on the Pantau server, then browsed, edited,
 * committed and pushed. Backed by /api/repos. Independent of Terraform (no gate); relies on index.html globals:
 * svgIcon, escapeHtml, t, I18N, showToast, showConfirm, openModal, closeModal, formatRelativeTime, langRerenderHooks, CodeMirror. */
(function () {
  'use strict';
  const R = (window.Repos = { state: { list: [], folder: '', loaded: false } });

  /* ---------- i18n ---------- */
  const D = {
    rp_hint: ['Clone Git repositories (SSH or HTTPS) into a folder on this server, then browse, edit, commit and push them from here. The SSH key or token needs WRITE permission on the repository to push.', 'Clone repositori Git (SSH atau HTTPS) ke folder di server ini, lalu jelajahi, ubah, commit, dan push dari sini. Kunci SSH atau token harus punya izin TULIS pada repositori agar bisa push.'],
    rp_folder: ['Location', 'Lokasi'],
    rp_folder_change: ['Change location', 'Ubah lokasi'],
    rp_folder_title: ['Repositories location', 'Lokasi repositori'],
    rp_folder_label: ['Absolute path (empty = default)', 'Path absolut (kosong = bawaan)'],
    rp_folder_hint: ['Folders and repositories are created inside this path. Existing clones stay where they are.', 'Folder dan repositori dibuat di dalam path ini. Clone yang sudah ada tetap di tempatnya.'],
    rp_folder_saved: ['Location saved', 'Lokasi disimpan'],
    rp_clone: ['Clone Repository', 'Clone Repositori'],
    rp_clone_btn: ['Clone', 'Clone'],
    rp_cloning: ['Cloning…', 'Meng-clone…'],
    rp_cloned: ['Repository cloned', 'Repositori di-clone'],
    rp_copy: ['Copy to host', 'Salin ke host'],
    rp_copy_title: ['Copy to host', 'Salin ke host'],
    rp_copy_host: ['Host', 'Host'],
    rp_copy_path: ['Destination directory on the host', 'Direktori tujuan di host'],
    rp_copy_path_hint: ['Absolute path. It is created if missing; files with the same name are overwritten and nothing else is deleted. System directories (/etc, /usr, /root, …) are refused.', 'Path absolut. Dibuat jika belum ada; berkas bernama sama ditimpa dan tidak ada yang dihapus. Direktori sistem (/etc, /usr, /root, …) ditolak.'],
    rp_copy_git: ['Include the .git folder (full history)', 'Sertakan folder .git (riwayat lengkap)'],
    rp_copy_what: ['Copies the files as the editor shows them (ignored files and symlinks are left out) over SSH as the host\'s SSH user, with progress in the Transfer Queue.', 'Menyalin berkas seperti yang tampil di editor (berkas yang di-ignore dan symlink tidak ikut) lewat SSH sebagai user SSH host, dengan progres di Transfer Queue.'],
    rp_copy_dirty: ['{n} uncommitted change(s) are included.', '{n} perubahan yang belum di-commit ikut tersalin.'],
    rp_copy_started: ['Copying {n} files to {host}', 'Menyalin {n} berkas ke {host}'],
    rp_copy_nohosts: ['Add a host first', 'Tambahkan host terlebih dahulu'],
    rp_f_folder: ['Folder', 'Folder'],
    rp_f_folder_hint: ['Optional. Groups repositories like a workspace group and is the directory they are cloned into: <location>/<folder>/<name>.', 'Opsional. Mengelompokkan repositori seperti grup workspace dan menjadi direktori tempat repositori di-clone: <lokasi>/<folder>/<nama>.'],
    rp_move_title: ['Move to folder', 'Pindah ke folder'],
    rp_move_hint: ['The repository directory is moved on the server; leave empty for no folder. A folder name that does not exist yet is created.', 'Direktori repositori dipindahkan di server; kosongkan untuk tanpa folder. Nama folder yang belum ada akan dibuat.'],
    rp_moved: ['Repository moved', 'Repositori dipindahkan'],
    rp_all_folders: ['All folders', 'Semua folder'],
    rp_ungrouped: ['No folder', 'Tanpa folder'],
    rp_repos_1: ['{n} repo', '{n} repo'],
    rp_repos_n: ['{n} repos', '{n} repo'],
    rp_clone_progress: ['Cloning {name}…', 'Meng-clone {name}…'],
    rp_clone_bg: ['You can close this window: the clone continues on the server and the repository appears in the list when it finishes.', 'Anda dapat menutup jendela ini: clone tetap berjalan di server dan repositori muncul di daftar saat selesai.'],
    rp_clone_cancel: ['Cancel clone', 'Batalkan clone'],
    rp_clone_canceling: ['Canceling…', 'Membatalkan…'],
    rp_clone_canceled: ['Clone canceled', 'Clone dibatalkan'],
    rp_clone_failed: ['Clone failed', 'Clone gagal'],
    rp_clone_retry: ['Try again', 'Coba lagi'],
    rp_loading: ['Loading…', 'Memuat…'],
    rp_empty_title: ['No repositories yet', 'Belum ada repositori'],
    rp_empty_desc: ['Clone a repository over SSH or HTTPS to browse and edit its files, commit and push.', 'Clone repositori lewat SSH atau HTTPS untuk menjelajah dan mengubah file, commit, dan push.'],
    rp_f_url: ['Repository URL', 'URL repositori'],
    rp_f_name: ['Folder name (optional)', 'Nama folder (opsional)'],
    rp_f_name_ph: ['defaults to the repository name', 'bawaan: nama repositori'],
    rp_f_branch: ['Branch (optional)', 'Branch (opsional)'],
    rp_f_branch_ph: ['remote default branch', 'branch bawaan remote'],
    rp_f_auth: ['Authentication', 'Autentikasi'],
    rp_auth_none: ['None (public repository)', 'Tidak ada (repositori publik)'],
    rp_auth_ssh: ['SSH key (SSH Key Library)', 'Kunci SSH (SSH Key Library)'],
    rp_auth_token: ['HTTPS token', 'Token HTTPS'],
    rp_f_key: ['SSH key', 'Kunci SSH'],
    rp_f_token: ['Access token', 'Token akses'],
    rp_token_keep: ['leave empty to keep the saved token', 'kosongkan untuk mempertahankan token tersimpan'],
    rp_no_keys: ['No keys in the SSH Key Library yet — add one in Settings.', 'Belum ada kunci di SSH Key Library — tambahkan di Pengaturan.'],
    rp_creds: ['Credentials', 'Kredensial'],
    rp_creds_title: ['Repository credentials', 'Kredensial repositori'],
    rp_creds_saved: ['Credentials saved', 'Kredensial disimpan'],
    rp_open: ['Open', 'Buka'],
    rp_sync: ['Sync', 'Sinkron'],
    rp_sync_ok: ['Synced {name} ({commit})', '{name} disinkronkan ({commit})'],
    rp_remove: ['Remove', 'Hapus'],
    rp_remove_title: ['Remove repository', 'Hapus repositori'],
    rp_remove_msg: ['Remove "{name}" and DELETE its folder {dir} from this server? Uncommitted changes and unpushed commits are lost; the remote repository is not touched.', 'Hapus "{name}" dan HAPUS foldernya {dir} dari server ini? Perubahan yang belum di-commit dan commit yang belum di-push akan hilang; repositori remote tidak diubah.'],
    rp_removed: ['Repository removed', 'Repositori dihapus'],
    rp_synced: ['Synced {when}', 'Disinkron {when}'],
    rp_changes_n: ['{n} changes', '{n} perubahan'],
    rp_clean: ['clean', 'bersih'],
    rp_unavailable: ['Unavailable', 'Tidak tersedia'],
    rp_ed_files: ['Files', 'File'],
    rp_ed_filter: ['Search files in the repository…', 'Cari file di repositori…'],
    rp_ed_newfile: ['new/path/file.tf', 'path/baru/file.tf'],
    rp_ed_add: ['New file', 'File baru'],
    rp_ed_pick: ['Select a file to edit.', 'Pilih file untuk diedit.'],
    rp_ed_save: ['Save', 'Simpan'],
    rp_ed_saved: ['Saved {path}', '{path} disimpan'],
    rp_ed_delete: ['Delete', 'Hapus'],
    rp_ed_delete_msg: ['Delete {path} from the working tree? It stays recoverable with Discard until you commit.', 'Hapus {path} dari working tree? Masih bisa dipulihkan dengan Buang sampai Anda commit.'],
    rp_ed_discard: ['Discard', 'Buang'],
    rp_ed_discard_msg: ['Discard all uncommitted changes of {path}?', 'Buang semua perubahan {path} yang belum di-commit?'],
    rp_ed_deleted_note: ['This file is deleted in the working tree. Discard to restore it, or commit the deletion.', 'File ini dihapus di working tree. Buang untuk memulihkan, atau commit penghapusannya.'],
    rp_ed_binary: ['Binary or non-UTF-8 files cannot be edited here.', 'File biner atau non-UTF-8 tidak dapat diedit di sini.'],
    rp_ed_toolarge: ['Files over 1 MiB cannot be edited here.', 'File di atas 1 MiB tidak dapat diedit di sini.'],
    rp_ed_truncated: ['This folder has more entries than can be listed here; use the search above to find a file.', 'Folder ini punya lebih banyak entri daripada yang dapat ditampilkan; gunakan pencarian di atas untuk menemukan file.'],
    rp_ed_changes_truncated: ['Too many changes to list; commit some of them first.', 'Terlalu banyak perubahan untuk ditampilkan; commit sebagian dulu.'],
    rp_ed_searching: ['Searching…', 'Mencari…'],
    rp_ed_nomatch: ['No files match.', 'Tidak ada file yang cocok.'],
    rp_ed_search_cap: ['Showing the first {n} matches; type more of the path to narrow it down.', 'Menampilkan {n} kecocokan pertama; ketik lebih banyak path untuk mempersempit.'],
    rp_ed_unsaved: ['Unsaved edits', 'Edit belum disimpan'],
    rp_ed_close_msg: ['There are unsaved edits. Close and discard them?', 'Ada edit yang belum disimpan. Tutup dan buang?'],
    rp_ed_close: ['Close and discard', 'Tutup dan buang'],
    rp_ed_refresh: ['Refresh', 'Muat ulang'],
    rp_ed_push: ['Push', 'Push'],
    rp_ed_pushed: ['Pushed {commit} to {branch}', '{commit} di-push ke {branch}'],
    rp_ed_commit: ['Commit & Push', 'Commit & Push'],
    rp_ed_ahead: ['{n} commit(s) not pushed', '{n} commit belum di-push'],
    rp_ed_behind: ['{n} commit(s) behind remote — Sync to update', '{n} commit tertinggal dari remote — Sinkron untuk memperbarui'],
    rp_cm_title: ['Commit & Push', 'Commit & Push'],
    rp_cm_message: ['Commit message', 'Pesan commit'],
    rp_cm_author: ['Author name', 'Nama author'],
    rp_cm_email: ['Author email', 'Email author'],
    rp_cm_branch_cur: ['Push to branch {branch}', 'Push ke branch {branch}'],
    rp_cm_branch_new: ['Create a new branch', 'Buat branch baru'],
    rp_cm_branch_ph: ['new-branch-name', 'nama-branch-baru'],
    rp_cm_files: ['Files to commit ({n})', 'File yang di-commit ({n})'],
    rp_cm_none: ['Select at least one file.', 'Pilih minimal satu file.'],
    rp_cm_perm: ['The SSH key or token must have WRITE permission on the repository; a read-only credential makes the push fail. Unsaved edits are saved first. No force push.', 'Kunci SSH atau token harus punya izin TULIS pada repositori; kredensial baca-saja membuat push gagal. Edit yang belum disimpan disimpan dulu. Tanpa force push.'],
    rp_cm_kept: ['The commit was kept locally. Fix the problem (credentials, or Sync if the remote moved), then use Push.', 'Commit tetap tersimpan lokal. Perbaiki masalahnya (kredensial, atau Sinkron bila remote berubah), lalu gunakan Push.'],
    rp_cm_working: ['Pushing…', 'Mem-push…'],
    rp_st_new: ['new', 'baru'],
    rp_st_modified: ['modified', 'diubah'],
    rp_st_deleted: ['deleted', 'dihapus'],
  };
  for (const [k, v] of Object.entries(D)) { I18N.en[k] = v[0]; I18N.id[k] = v[1]; }

  /* ---------- helpers ---------- */
  R.esc = escapeHtml;
  R.el = (id) => document.getElementById(id);
  R.val = (id) => { const e = R.el(id); return e ? e.value : ''; };
  R.tx = (key, vars) => {
    let s = t(key, key);
    if (vars) for (const [k, v] of Object.entries(vars)) s = s.split('{' + k + '}').join(v);
    return s;
  };
  R.short = (sha) => String(sha || '').slice(0, 7);
  R.empty = (msg) => `<div class="rp-note">${R.esc(msg)}</div>`;
  R.err = (r) => showToast(t('error_prefix', 'Error: ') + (r.data.error || 'Request failed'), 'error');
  R.api = async (method, url, body) => {
    const opt = { method, headers: {} };
    if (body !== undefined) { opt.headers['Content-Type'] = 'application/json'; opt.body = JSON.stringify(body); }
    try {
      const res = await fetch(url, opt);
      const text = await res.text();
      let data;
      try { data = text ? JSON.parse(text) : {}; } catch (e) { data = { error: text.trim() }; }
      if (data === null || typeof data !== 'object') data = { value: data };
      if (!res.ok && !data.error) data.error = 'HTTP ' + res.status;
      return { ok: res.ok, status: res.status, data };
    } catch (e) {
      return { ok: false, status: 0, data: { error: e.message } };
    }
  };
  R.field = (label, inner, hint, full) => `<div class="dk-field${full ? ' dk-full' : ''}"><label>${R.esc(label)}</label>${inner}${hint ? `<div class="dk-hint">${R.esc(hint)}</div>` : ''}</div>`;
  R.input = (id, val, extra) => `<input type="text" id="${id}" class="form-input" style="width:100%;box-sizing:border-box;" value="${R.esc(val == null ? '' : val)}" ${extra || ''} />`;
  R.select = (id, opts, cur, onchange) => `<select id="${id}" class="form-input" style="width:100%;box-sizing:border-box;" ${onchange ? `onchange="${onchange}"` : ''}>${opts.map(([v, l]) => `<option value="${R.esc(v)}" ${String(v) === String(cur) ? 'selected' : ''}>${R.esc(l)}</option>`).join('')}</select>`;

  /* Action Dialog (#repoDlg) */
  R.dlg = (title, body, footer, width) => {
    R.el('repoDlgTitle').textContent = title;
    R.el('repoDlgBody').innerHTML = body;
    R.el('repoDlgFooter').innerHTML = footer;
    R.el('repoDlgContent').style.maxWidth = 'min(' + (width || 560) + 'px, 96vw)';
    R.dlgErr('');
    openModal('repoDlg');
  };
  R.dlgClose = () => closeModal('repoDlg');
  R.dlgErr = (msg) => {
    const el = R.el('repoDlgErr');
    el.textContent = msg || '';
    el.style.display = msg ? 'block' : 'none';
  };
  R.foot = (primary) => `<button type="button" class="btn btn-sm" onclick="Repos.dlgClose()">${R.esc(t('cancel', 'Cancel'))}</button>${primary || ''}`;
  R.primary = (label, onclick) => `<button type="button" class="btn btn-sm btn-primary" onclick="${onclick}">${R.esc(label)}</button>`;
  R.busy = (btn, label) => {
    if (!btn) return;
    btn.disabled = true;
    btn.innerHTML = `<span class="spinner">${svgIcon('loader-circle', 'icon-xs')}</span> ${R.esc(label)}`;
  };
  // Highlighting follows the file name: hcl comes from the Terraform bundle, the rest from the vendored CodeMirror modes
  // via its mode table (meta.js); a name without a loaded mode stays plain text.
  R.modeFor = (path) => {
    let name = String(path).slice(String(path).lastIndexOf('/') + 1);
    name = name.replace(/\.(j2|jinja2?|tpl|tftpl)$/i, '') || name; // nginx.conf.j2 reads as nginx.conf
    if (/\.(tf|tfvars|hcl)$/i.test(name)) return CodeMirror.modes.hcl ? 'hcl' : null;
    if (/^(\.gitignore|\.dockerignore|\.env(\..*)?|\.envrc|Makefile|Procfile|\.bashrc|\.profile)$/.test(name)) return 'shell';
    if (/\.(conf|cfg|service|timer|socket|mount)$/i.test(name) && !/nginx/i.test(name)) return 'properties';
    // The name match can point at a mode that is not loaded (README.md -> gfm): then try the extension (-> markdown).
    const ext = name.includes('.') ? name.slice(name.lastIndexOf('.') + 1) : '';
    for (const m of [CodeMirror.findModeByFileName(name), ext && CodeMirror.findModeByExtension(ext)]) {
      if (m && CodeMirror.modes[m.mode]) return m.mime || m.mode;
    }
    return null;
  };
})();
