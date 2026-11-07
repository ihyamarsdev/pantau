/* Terraform Configuration tab strings (EN + ID), registered into the shared dictionaries. */
(function () {
  'use strict';
  const TFCFG_TXT = {
    tfcfg_title: ['Configuration', 'Konfigurasi'],
    tfcfg_save: ['Save', 'Simpan'],
    tfcfg_save_plan: ['Save & plan', 'Simpan & plan'],
    tfcfg_discard: ['Discard', 'Buang perubahan'],
    tfcfg_del: ['Delete file', 'Hapus file'],
    tfcfg_del_confirm: ['Delete file "{name}"?', 'Hapus file "{name}"?'],
    tfcfg_last_file: ['Cannot delete the last file', 'Tidak bisa menghapus file terakhir'],
    tfcfg_bad_name: ['Invalid file name: use letters, digits, _ . - and an allowed extension (.tf, .tfvars, .tftpl, .json, .yaml, .yml, .txt, .md)', 'Nama file tidak valid: gunakan huruf, angka, _ . - dan ekstensi yang diizinkan (.tf, .tfvars, .tftpl, .json, .yaml, .yml, .txt, .md)'],
    tfcfg_too_many: ['Too many files (max 50)', 'Terlalu banyak file (maks 50)'],
    tfcfg_unsaved_title: ['Unsaved changes', 'Perubahan belum disimpan'],
    tfcfg_unsaved_msg: ['You have unsaved configuration changes. Leave anyway?', 'Ada perubahan konfigurasi yang belum disimpan. Tetap keluar?'],
    tfcfg_leave: ['Leave', 'Keluar'],
    tfcfg_save_err: ['Save failed', 'Gagal menyimpan'],
    tfcfg_locked_hint: ['Workspace is locked — runs use the saved files as they were at start.', 'Workspace terkunci — run memakai file tersimpan saat dijalankan.'],
    tfcfg_git_hint: ['Git workspaces take their files from the repository.', 'Workspace Git mengambil filenya dari repositori.'],
    tfcfg_git_edit: ['Edit Repository', 'Ubah Repositori'],
    tfcfg_del_confirm_t: ['Delete', 'Hapus'],
    tfcfg_fmt: ['Format', 'Format'],
    tfcfg_fmt_title: ['terraform fmt on all files (Ctrl+Shift+F)', 'terraform fmt pada semua file (Ctrl+Shift+F)'],
    tfcfg_validate: ['Validate', 'Validasi'],
    tfcfg_validate_title: ['terraform init + validate on the current (unsaved) files', 'terraform init + validate pada file saat ini (belum disimpan)'],
    tfcfg_validating: ['Validating…', 'Memvalidasi…'],
    tfcfg_formatted: ['Formatted {n} file(s)', '{n} file diformat'],
    tfcfg_already_fmt: ['Already formatted', 'Sudah terformat'],
    tfcfg_diag_title: ['Validation results', 'Hasil validasi'],
    tfcfg_diag_close: ['Close', 'Tutup'],
    tfcfg_valid_ok: ['Configuration is valid', 'Konfigurasi valid'],
    tfcfg_diag_counts: ['{e} error(s), {w} warning(s)', '{e} error, {w} peringatan'],
    tfcfg_init_err: ['Initialization failed', 'Inisialisasi gagal'],
    tfcfg_fmt_errs: ['Format errors', 'Kesalahan format'],
    tfcfg_sev_error: ['error', 'error'],
    tfcfg_sev_warning: ['warning', 'peringatan'],
  };
  const I18Nd = window.I18N || (typeof I18N !== 'undefined' ? I18N : null);
  if (I18Nd) for (const [k, v] of Object.entries(TFCFG_TXT)) { I18Nd.en[k] = v[0]; I18Nd.id[k] = v[1]; }
})();
