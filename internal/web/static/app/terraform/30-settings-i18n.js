/* Settings tab strings (EN+ID) — same dictionary pair style as TF_TXT. */
(function () {
  const D = {
    tfset_general: ['General', 'Umum'],
    tfset_dirty: ['Unsaved changes', 'Perubahan belum disimpan'],
    tfset_unsaved_msg: ['You have unsaved changes in this tab. Leave anyway?', 'Ada perubahan yang belum disimpan di tab ini. Tetap keluar?'],
    tfset_reset: ['Reset', 'Atur ulang'],
    tfset_leave: ['Leave', 'Keluar'],
    tfset_inline_files: ['Inline files ({n} files)', 'File inline ({n} file)'],
    tfset_inline_note: ['Inline files are edited in the Configuration tab.', 'File inline diedit di tab Configuration.'],
    tfset_open_config: ['Edit files', 'Ubah file'],
    tfset_ssh_key: ['SSH key', 'Kunci SSH'],
    tfset_webhook: ['Webhook', 'Webhook'],
  };
  for (const [k, v] of Object.entries(D)) { I18N.en[k] = v[0]; I18N.id[k] = v[1]; }
})();
