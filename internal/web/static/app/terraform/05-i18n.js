/* Core Terraform strings (EN + ID). Feature modules add their own keys in their own files. */
(function () {
  'use strict';
  const Tf = window.Tf;
  Tf.addI18n({
    en: {
      tf_copied: 'Copied', tf_err_generic: 'Request failed', tf_retry: 'Retry',
      tf_time_now: 'just now', tf_time_ago: '{t} ago', tf_time_in: 'in {t}',
      tf_desc: 'Plan, approve and apply infrastructure with Terraform or OpenTofu, with state, variables and policies kept in Pantau.',
      tf_refresh: 'Refresh', tf_back: 'Back', tf_close: 'Close', tf_expand: 'Expand', tf_collapse: 'Collapse',
      tf_gate_active: 'Terraform Gate is Active', tf_gate_disabled_title: 'Terraform Gate is Disabled',
      tf_gate_unavailable: 'Terraform unavailable', tf_gate_off_title: 'Terraform Gate is off',
      tf_gate_off_desc: 'Workspaces, runs and state need the Terraform Gate, which an administrator must enable first.',
      tf_gate_open_settings: 'Open Gate Settings',
      tf_rs_queued: 'Queued', tf_rs_planning: 'Planning', tf_rs_awaiting: 'Awaiting approval', tf_rs_applying: 'Applying',
      tf_rs_applied: 'Applied', tf_rs_nochanges: 'No changes', tf_rs_planned: 'Planned', tf_rs_policyfailed: 'Policy failed',
      tf_rs_errored: 'Errored', tf_rs_discarded: 'Discarded', tf_rs_canceled: 'Canceled', tf_rs_interrupted: 'Interrupted',
    },
    id: {
      tf_copied: 'Disalin', tf_err_generic: 'Permintaan gagal', tf_retry: 'Coba lagi',
      tf_time_now: 'baru saja', tf_time_ago: '{t} lalu', tf_time_in: 'dalam {t}',
      tf_desc: 'Rencanakan, setujui, dan terapkan infrastruktur dengan Terraform atau OpenTofu, dengan state, variabel, dan kebijakan yang disimpan di Pantau.',
      tf_refresh: 'Muat ulang', tf_back: 'Kembali', tf_close: 'Tutup', tf_expand: 'Perlebar', tf_collapse: 'Perkecil',
      tf_gate_active: 'Terraform Gate aktif', tf_gate_disabled_title: 'Terraform Gate nonaktif',
      tf_gate_unavailable: 'Terraform tidak tersedia', tf_gate_off_title: 'Terraform Gate nonaktif',
      tf_gate_off_desc: 'Workspace, run, dan state memerlukan Terraform Gate yang harus diaktifkan administrator terlebih dahulu.',
      tf_gate_open_settings: 'Buka Pengaturan Gate',
      tf_rs_queued: 'Antre', tf_rs_planning: 'Merencanakan', tf_rs_awaiting: 'Menunggu persetujuan', tf_rs_applying: 'Menerapkan',
      tf_rs_applied: 'Diterapkan', tf_rs_nochanges: 'Tanpa perubahan', tf_rs_planned: 'Direncanakan', tf_rs_policyfailed: 'Kebijakan gagal',
      tf_rs_errored: 'Error', tf_rs_discarded: 'Dibuang', tf_rs_canceled: 'Dibatalkan', tf_rs_interrupted: 'Terputus',
    },
  });
})();
