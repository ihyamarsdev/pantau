/* Audit, notifications and scheduling strings (EN+ID) — same dictionary pair style as 40-cloud-i18n.js. */
(function () {
  const D = {
    tfaud_workspace: ['Workspace', 'Workspace'],
    tfaud_all_ws: ['All workspaces', 'Semua workspace'],
    tfaud_loading: ['Loading…', 'Memuat…'],
    tfaud_empty: ['No audit events yet', 'Belum ada event audit'],
    tfnf_title: ['Notifications', 'Notifikasi'],
    tfnf_hint: ['Choose which Terraform events are sent to the notification channels configured in Pantau Settings.', 'Pilih event Terraform yang dikirim ke kanal notifikasi yang dikonfigurasi di Pengaturan Pantau.'],
    tfnf_ev_awaiting_approval: ['Run awaiting approval', 'Run menunggu persetujuan'],
    tfnf_ev_errored: ['Run errored', 'Run gagal'],
    tfnf_ev_applied: ['Run applied', 'Run diterapkan'],
    tfnf_ev_drift: ['Drift detected', 'Drift terdeteksi'],
    tfnf_test: ['Send test', 'Kirim tes'],
    tfnf_testing: ['Sending…', 'Mengirim…'],
    tfnf_saved: ['Notification settings saved', 'Pengaturan notifikasi disimpan'],
    tfnf_test_ok: ['Test notification sent', 'Notifikasi tes terkirim'],
    tfnf_test_fail: ['Test failed: {err}', 'Tes gagal: {err}'],
    tfset_plan_interval: ['Scheduled plan interval (minutes)', 'Interval plan terjadwal (menit)'],
    tfset_plan_interval_hint: ['0 = off. A plan run is created automatically when overdue.', '0 = nonaktif. Run plan dibuat otomatis saat sudah waktunya.'],
    tfset_auto_apply: ['Auto-apply', 'Auto-apply'],
    tfset_auto_apply_hint: ['Apply automatically when a plan has changes and passes policy checks.', 'Terapkan otomatis ketika plan memiliki perubahan dan lolos pemeriksaan policy.'],
    tfset_auto_apply_prod: ['Not available for production workspaces.', 'Tidak tersedia untuk workspace production.'],
  };
  for (const [k, v] of Object.entries(D)) { I18N.en[k] = v[0]; I18N.id[k] = v[1]; }
})();
