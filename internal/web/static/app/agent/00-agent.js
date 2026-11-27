/* AI agent: floating chat bottom-right, operator approval cards, Settings → AI Agent panel and the
 * "Ask agent" entry points (AG.askUnit / askContainer / askAlert / askSecurity). Backend: /api/agent/* (agent.go).
 * Relies on index.html globals: svgIcon, escapeHtml, t, I18N, showToast, currentHost, latestAlerts, langRerenderHooks. */
(function () {
  'use strict';

  const AG = (window.AG = {});
  const SESSION_KEY = 'pantau_agent_session';
  const POS_KEY = 'pantau_agent_fab_pos';
  const MODE_KEY = 'pantau_agent_mode';
  const DOCK_W_KEY = 'pantau_agent_dock_width';
  const DOCK_MIN_W = 300;
  const DOCK_DEFAULT_W = 400;
  function storedDockWidth() {
    const raw = Number(localStorage.getItem(DOCK_W_KEY));
    if (isFinite(raw) && raw >= DOCK_MIN_W) return Math.round(raw);
    return DOCK_DEFAULT_W;
  }
  const S = {
    grants: [], enabled: false, open: false, expanded: false, docked: localStorage.getItem(MODE_KEY) === 'docked',
    dockWidth: storedDockWidth(), busy: false, session: null, abort: null, approvals: [], timer: null,
    msgs: [], chatT0: 0, apprT0: 0, fab: null, drag: null, suppressToggle: false, wired: false,
    thread: 0, scope: '', hydrated: false, ctx: null, ctxTimer: null, skipAutoLoadForScope: '', reflowTimer: null
  };

  const I = {
    en: {
      ag_title: 'AI Agent', ag_ask: 'Ask agent', ag_new: 'New chat', ag_close: 'Close', ag_expand: 'Expand', ag_collapse: 'Collapse', ag_send: 'Send',
      ag_dock: 'Dock to side', ag_float: 'Float window', ag_resize_panel: 'Resize AI panel',
      ag_ctx_global: 'Fleet', ag_ctx_host: 'Host', ag_ctx_kube: 'K8s', ag_ctx_pod: 'Pod', ag_ctx_docker: 'Docker', ag_ctx_ansible: 'Ansible', ag_ctx_terraform: 'TF',
      ag_chip_explain: 'Explain Error', ag_chip_diagnose: 'Diagnose Resource', ag_chip_fix: 'Suggest Safe Fix', ag_chip_drift: 'Analyze Drift/Plan',
      ag_qp_host_explain: 'Check recent errors, failed systemd units, and active alerts on host "{target}" (id {id}). Explain the root cause.',
      ag_qp_host_diag: 'Diagnose CPU, memory, disk, and top processes on host "{target}" (id {id}). Summarize any bottlenecks.',
      ag_qp_host_fix: 'Review drift, alerts, and security findings on host "{target}" (id {id}) and suggest safe fixes. Do not change anything until I confirm.',
      ag_qp_kube_explain: 'Investigate failing pods, crash loops, or warning events in {target}. Explain the root cause.',
      ag_qp_kube_diag: 'Diagnose Kubernetes resource health, pod status, and node capacity for {target}.',
      ag_qp_kube_fix: 'Propose safe remediation steps for issues in {target}. Do not apply changes until I confirm.',
      ag_qp_docker_explain: 'Inspect logs and exit/health state of Docker {target} and explain any errors.',
      ag_qp_docker_diag: 'Diagnose Docker resource usage, container state, and configuration for {target}.',
      ag_qp_docker_fix: 'Suggest safe fixes for issues in Docker {target}. Do not change anything until I confirm.',
      ag_qp_ansible_explain: 'Analyze the latest failed tasks or errors in Ansible "{target}" and explain the root cause.',
      ag_qp_ansible_diag: 'Review Ansible playbook/inventory readiness and host connectivity for "{target}".',
      ag_qp_ansible_fix: 'Suggest safe playbook or task fixes for Ansible "{target}". Do not modify or run anything until I confirm.',
      ag_qp_tf_drift: 'Analyze the current Terraform plan, state, and drift for "{target}". Summarize proposed changes and risks.',
      ag_qp_tf_explain: 'Explain recent Terraform plan/apply or state errors in "{target}".',
      ag_qp_tf_fix: 'Suggest safe HCL or state remediation for Terraform "{target}". Do not apply anything until I confirm.',
      ag_qp_global_diag: 'Summarize overall fleet health, degraded hosts, and resource pressure.',
      ag_qp_global_explain: 'Summarize recent active alerts across the fleet and explain their likely root causes.',
      ag_qp_global_fix: 'Prioritize safe remediation steps for current fleet issues. Do not change anything until I confirm.',
      ag_ph: 'Ask about a service, container, alert or security finding…',
      ag_empty: 'Pantau Operator can read host state and, with your approval, restart services and containers or change settings on your hosts.',
      ag_disabled: 'The AI agent is off. Enable it in Settings → AI Agent.', ag_open_settings: 'Open AI Agent settings',
      ag_working: 'Working…', ag_stop: 'Stop',
      ag_waiting_approval: 'Waiting for your approval',
    ag_save_kb: 'Save as Runbook / Blueprint', ag_k_need_thread: 'Open a chat thread first: distill needs a conversation.',
    ag_k_distilling: 'Distilling this chat into a draft...', ag_k_review_title: 'Review runbook draft',
    ag_k_review_hint: 'Nothing is saved until you press Save. Check every step: credentials must stay as {{ vault.KEY }} placeholders.',
    ag_k_f_title: 'Title', ag_k_f_kind: 'Kind', ag_k_f_domain: 'Domain', ag_k_f_trigger: 'Trigger (symptom or goal)',
    ag_k_f_steps: 'Steps (one per line)', ag_k_f_verify: 'Verify (one per line)', ag_k_f_rollback: 'Rollback (one per line)',
    ag_k_f_tags: 'Tags (comma separated)', ag_k_save: 'Save to Knowledge Base', ag_k_cancel: 'Cancel', ag_k_saved: 'Saved to the knowledge base',
    ag_k_title: 'Knowledge & Skills', ag_k_hint: 'Runbooks and blueprints the agent retrieves for matching questions, and custom skills injected for their domain.',
    ag_k_search: 'Search runbooks and notes', ag_k_skills: 'Custom skills', ag_k_no_skills: 'No custom skills yet.', ag_k_none: 'Nothing found.',
    ag_k_enabled: 'Enabled', ag_k_delete: 'Delete', ag_k_used: 'used', ag_k_confirm_delete: 'Delete this entry? This cannot be undone.',
    ag_k_skill_name: 'Skill name', ag_k_skill_content: 'Instructions for this domain (Markdown)', ag_k_add_skill: 'Add skill',
    ag_plan_title: 'Plan', ag_plan_loading: 'Loading plan...', ag_plan_goal_label: 'Goal',
    ag_plan_hint: 'Nothing runs until you approve. One approval covers the steps of this plan. Destructive actions are never part of a plan.',
    ag_plan_approve: 'Approve & Execute Plan', ag_plan_reject: 'Reject', ag_plan_stop: 'Stop', ag_plan_rollback: 'Run Rollback',
    ag_plan_rollback_steps: 'Rollback steps (run only when you click Run Rollback)',
    ag_plan_st_proposed: 'Awaiting your approval', ag_plan_st_running: 'Running', ag_plan_st_done: 'Done',
    ag_plan_st_failed: 'Failed', ag_plan_st_verify_failed: 'Verification failed, rollback available',
    ag_plan_st_rolling_back: 'Rolling back', ag_plan_st_rolled_back: 'Rolled back', ag_plan_st_rollback_failed: 'Rollback failed',
    ag_plan_st_cancelled: 'Stopped',
    ag_plan_s_pending: 'pending', ag_plan_s_running: 'running', ag_plan_s_done: 'done', ag_plan_s_failed: 'failed', ag_plan_s_skipped: 'skipped',
      ag_appr_title: 'Agent wants to run', ag_appr_exec: 'Shell command (always needs approval)', ag_appr_infra: 'Infrastructure change (always needs approval)', ag_approve: 'Approve', ag_deny: 'Deny', ag_approve_session: 'Approve for this session', ag_session_hint: 'Approves every later change on {host} without asking (shell commands too: {shell}). Ends on New chat, Revoke or after 2 hours.', ag_grants: 'Auto-approved this session', ag_revoke: 'Revoke', ag_g_shell: 'incl. shell', ag_g_tools: 'service/container actions',
      ag_s_enable: 'Enable AI agent',
      ag_s_provider: 'LLM provider', ag_s_provider_none: 'Not configured', ag_s_provider_anthropic: 'Anthropic', ag_s_provider_openai: 'OpenAI-compatible',
      ag_s_base_url: 'Base URL', ag_s_base_url_hint: 'Empty = the provider default. OpenAI-compatible covers OpenRouter, LiteLLM, vLLM, Ollama and any other gateway.',
      ag_s_key: 'API key', ag_s_key_saved: 'Saved. Leave empty to keep it.',
      ag_s_key_hint: 'Required for Anthropic. Optional for OpenAI-compatible local servers (Ollama, vLLM).',
      ag_s_model: 'Model', ag_s_model_hint: 'Required: the agent needs a model to answer.',
      ag_s_reasoning_title: 'Reasoning Model (Primary)', ag_s_reasoning_hint: 'Used for complex root-cause analysis, YAML/HCL/Playbook authoring, and remediation.',
      ag_s_fast_title: 'Fast / Utility Model (Optional)', ag_s_fast_hint: 'Used for summarizing large outputs, intent routing, and auto-generating thread titles. Empty falls back to the Reasoning Model.',
      ag_s_fast_provider: 'Fast model provider', ag_s_fast_provider_same: 'Same as Reasoning Model',
      ag_s_fast_base_url: 'Fast model base URL', ag_s_fast_base_url_hint: 'Optional: e.g. http://localhost:11434/v1 for local Ollama.',
      ag_s_fast_key: 'Fast model API key', ag_s_fast_key_hint: 'Optional for local servers (Ollama, vLLM). Leave empty to keep the saved key or inherit primary.',
      ag_s_fast_model: 'Fast model name', ag_s_fast_model_hint: 'Optional: e.g. qwen2.5:7b, claude-haiku-4-5, or gpt-4o-mini. Empty falls back to Reasoning Model.',
      ag_s_test_fast: 'Test fast model',
      ag_s_triage_enable: 'Proactive AI Auto-Triage', ag_s_triage_hint: 'Diagnose obvious failures locally (0 tokens) and use one short LLM call for the rest',
      ag_s_triage_model: 'Lightweight triage model (optional)', ag_s_triage_model_hint: 'Empty uses the primary agent model',
      ag_s_triage_budget: 'Daily triage token budget', ag_s_triage_budget_hint: 'Global cap across all auto-triage calls per day',
      ag_s_briefing_time: 'Daily briefing time (HH:MM, optional)', ag_s_briefing_hint: 'Delta-only briefing of the last 24 hours; empty disables the schedule',
      ag_s_approval: 'Approval for changes', ag_s_ask: 'Ask me for every change (recommended)',
      ag_s_auto: 'Auto-approve service/container actions, scans and inspections (shell commands still ask)',
      ag_s_soul: 'SOUL (the agent\'s identity and rules, sent with every chat)', ag_s_soul_reset: 'Reset SOUL to default',
      ag_s_save: 'Save', ag_s_test: 'Test connection', ag_s_saved: 'Saved', ag_s_test_ok: 'The provider answered',
      ag_s_testing: 'Testing connection…',
      ag_q_unit: 'Service {unit} on host "{host}" (id {id}) has a problem. Find the root cause and propose a fix. Check its status and journal first. Do not change anything until I confirm the plan.',
      ag_q_container: 'Container {name} on host "{host}" (id {id}) has a problem. Read its logs and state, find the root cause and propose a fix. Do not change anything until I confirm the plan.',
      ag_q_stack: 'Docker Compose stack "{stack}" on host "{host}" (id {id}) needs inspection. Check its containers, status, and logs, find any issues and propose a fix. Do not change anything until I confirm the plan.',
      ag_q_alert: 'Alert on host "{host}" (id {id}): {msg}\nRoot cause excerpt:\n{rc}\nInvestigate and propose a fix. Do not change anything until I confirm the plan.',
      ag_m_title: 'Agent memory', ag_m_desc: 'Durable notes the agent saves about your infrastructure and re-reads at the start of every chat. Review them: delete anything wrong. Notes are sent to your LLM provider with every chat.',
      ag_m_empty: 'No notes yet. The agent adds them as it learns your setup; you can add your own below.', ag_m_fleet: 'Whole fleet', ag_m_ph: 'e.g. Backups run at 02:00 via /opt/backup.sh',
      ag_m_scope: 'Scope', ag_m_scope_fleet: 'Whole fleet', ag_m_scope_host: 'One host', ag_m_scope_cluster: 'One cluster', ag_m_scope_domain: 'One domain',
      ag_m_add: 'Add', ag_m_forget: 'Forget', ag_m_forget_confirm: 'Delete this note?', ag_m_by_agent: 'agent', ag_m_by_you: 'you',
      ag_q_security: 'Review the latest security scan of host "{host}" (id {id}): pending security updates and failing hardening checks. Rank what matters and propose safe fixes. Do not change anything until I confirm the plan.',
      ag_patch_preview: 'Preview Diff', ag_patch_dryrun: 'Validate / Dry-Run', ag_patch_dryrunning: 'Validating…',
      ag_patch_apply: 'Apply Changes', ag_patch_applying: 'Applying…', ag_patch_applied: 'Applied',
      ag_patch_view_split: 'Side-by-Side', ag_patch_view_unified: 'Unified Diff',
      ag_patch_before: 'Current (Before)', ag_patch_after: 'Proposed (After)',
      ag_patch_need_dryrun: 'Run Validate / Dry-Run first', ag_patch_valid_ok: 'Validation passed', ag_patch_valid_fail: 'Validation failed',
      ag_patch_apply_ok: 'Changes applied',
      ag_ask_remediate: 'Ask Agent to Remediate', ag_ask_fix: 'Ask Agent to Fix', ag_ask_explain_plan: 'Explain Plan & Risk',
      ag_q_remediate: 'Remediate this {source} finding:\n- Rule/ID: {rule}\n- Severity: {severity}\n- Target: {target}\n- Finding: {title}\n- Detail: {detail}\n- Suggested remediation: {remediation}\nInvestigate and propose a safe fix (emit a pantau-patch block if editing config/YAML/HCL). Do not apply changes until I confirm.',
      ag_q_ansible_fix: 'Ansible task failed in "{playbook}" (run #{run_id}) on host "{host}" (status: {status}):\nTask: {task}\nError:\n{error}\nFind the root cause and propose a safe fix (emit a pantau-patch block if editing the playbook). Do not run or modify anything until I confirm.',
      ag_q_tf_explain: 'Explain this Terraform plan and policy/risk summary for workspace "{workspace}" (run #{run_id}, status: {status}):\n- Plan: +{create} create, ~{update} update, ±{replace} replace, -{delete} delete\n- Policy: {policy}\n- Cost delta: {cost}\nHighlight breaking changes, deletions, and policy risks.'
    },
    id: {
      ag_title: 'Agen AI', ag_ask: 'Tanya agent', ag_new: 'Chat baru', ag_close: 'Tutup', ag_expand: 'Perluas', ag_collapse: 'Perkecil', ag_send: 'Kirim',
      ag_dock: 'Dok ke sisi kanan', ag_float: 'Mode melayang', ag_resize_panel: 'Ubah ukuran panel AI',
      ag_ctx_global: 'Fleet', ag_ctx_host: 'Host', ag_ctx_kube: 'K8s', ag_ctx_pod: 'Pod', ag_ctx_docker: 'Docker', ag_ctx_ansible: 'Ansible', ag_ctx_terraform: 'TF',
      ag_chip_explain: 'Jelaskan Error', ag_chip_diagnose: 'Diagnosa Resource', ag_chip_fix: 'Saran Perbaikan Aman', ag_chip_drift: 'Analisis Drift/Plan',
      ag_qp_host_explain: 'Periksa error terbaru, unit systemd yang gagal, dan alert aktif pada host "{target}" (id {id}). Jelaskan akar masalahnya.',
      ag_qp_host_diag: 'Diagnosa CPU, memori, disk, dan proses teratas pada host "{target}" (id {id}). Ringkas bottleneck yang ditemukan.',
      ag_qp_host_fix: 'Tinjau drift, alert, dan temuan keamanan pada host "{target}" (id {id}) lalu sarankan perbaikan aman. Jangan ubah apa pun sebelum saya setujui.',
      ag_qp_kube_explain: 'Selidiki pod yang gagal, crash loop, atau event peringatan pada {target}. Jelaskan akar masalahnya.',
      ag_qp_kube_diag: 'Diagnosa kesehatan resource Kubernetes, status pod, dan kapasitas node untuk {target}.',
      ag_qp_kube_fix: 'Usulkan langkah perbaikan aman untuk masalah pada {target}. Jangan terapkan perubahan sebelum saya setujui.',
      ag_qp_docker_explain: 'Periksa log dan status exit/health dari Docker {target} lalu jelaskan error yang ada.',
      ag_qp_docker_diag: 'Diagnosa penggunaan resource, status container, dan konfigurasi Docker untuk {target}.',
      ag_qp_docker_fix: 'Sarankan perbaikan aman untuk masalah pada Docker {target}. Jangan ubah apa pun sebelum saya setujui.',
      ag_qp_ansible_explain: 'Analisis task yang gagal atau error terbaru di Ansible "{target}" dan jelaskan akar masalahnya.',
      ag_qp_ansible_diag: 'Tinjau kesiapan playbook/inventory dan konektivitas host untuk Ansible "{target}".',
      ag_qp_ansible_fix: 'Sarankan perbaikan aman pada playbook atau task Ansible "{target}". Jangan ubah atau jalankan apa pun sebelum saya setujui.',
      ag_qp_tf_drift: 'Analisis Terraform plan, state, dan drift saat ini untuk "{target}". Ringkas perubahan yang diusulkan dan risikonya.',
      ag_qp_tf_explain: 'Jelaskan error plan/apply atau state Terraform terbaru pada "{target}".',
      ag_qp_tf_fix: 'Sarankan perbaikan aman untuk HCL atau state Terraform "{target}". Jangan apply apa pun sebelum saya setujui.',
      ag_qp_global_diag: 'Ringkas kesehatan fleet secara keseluruhan, host yang degraded, dan tekanan resource.',
      ag_qp_global_explain: 'Ringkas alert aktif terbaru di seluruh fleet dan jelaskan kemungkinan akar masalahnya.',
      ag_qp_global_fix: 'Urutkan langkah perbaikan aman untuk masalah fleet saat ini. Jangan ubah apa pun sebelum saya setujui.',
      ag_ph: 'Tanyakan soal service, container, alert, atau temuan keamanan…',
      ag_empty: 'Pantau Operator bisa membaca kondisi host dan, dengan persetujuan Anda, me-restart service/container atau mengubah setting host.',
      ag_disabled: 'Agen AI nonaktif. Aktifkan di Pengaturan → AI Agent.', ag_open_settings: 'Buka pengaturan AI Agent',
      ag_working: 'Bekerja…', ag_stop: 'Berhenti',
      ag_waiting_approval: 'Menunggu persetujuan Anda',
    ag_save_kb: 'Simpan sebagai Runbook / Blueprint', ag_k_need_thread: 'Buka thread chat dulu: penyulingan membutuhkan percakapan.',
    ag_k_distilling: 'Menyuling chat ini menjadi draf...', ag_k_review_title: 'Tinjau draf runbook',
    ag_k_review_hint: 'Tidak ada yang disimpan sebelum Anda menekan Simpan. Periksa setiap langkah: kredensial harus tetap berupa placeholder {{ vault.KEY }}.',
    ag_k_f_title: 'Judul', ag_k_f_kind: 'Jenis', ag_k_f_domain: 'Domain', ag_k_f_trigger: 'Pemicu (gejala atau tujuan)',
    ag_k_f_steps: 'Langkah (satu per baris)', ag_k_f_verify: 'Verifikasi (satu per baris)', ag_k_f_rollback: 'Rollback (satu per baris)',
    ag_k_f_tags: 'Tag (dipisah koma)', ag_k_save: 'Simpan ke Basis Pengetahuan', ag_k_cancel: 'Batal', ag_k_saved: 'Disimpan ke basis pengetahuan',
    ag_k_title: 'Pengetahuan & Skill', ag_k_hint: 'Runbook dan blueprint yang diambil agen untuk pertanyaan yang cocok, serta skill kustom yang disisipkan untuk domainnya.',
    ag_k_search: 'Cari runbook dan catatan', ag_k_skills: 'Skill kustom', ag_k_no_skills: 'Belum ada skill kustom.', ag_k_none: 'Tidak ditemukan.',
    ag_k_enabled: 'Aktif', ag_k_delete: 'Hapus', ag_k_used: 'dipakai', ag_k_confirm_delete: 'Hapus entri ini? Tindakan ini tidak bisa dibatalkan.',
    ag_k_skill_name: 'Nama skill', ag_k_skill_content: 'Instruksi untuk domain ini (Markdown)', ag_k_add_skill: 'Tambah skill',
    ag_plan_title: 'Rencana', ag_plan_loading: 'Memuat rencana...', ag_plan_goal_label: 'Tujuan',
    ag_plan_hint: 'Tidak ada yang berjalan sebelum Anda menyetujui. Satu persetujuan mencakup langkah-langkah rencana ini. Aksi destruktif tidak pernah menjadi bagian rencana.',
    ag_plan_approve: 'Setujui & Jalankan Rencana', ag_plan_reject: 'Tolak', ag_plan_stop: 'Hentikan', ag_plan_rollback: 'Jalankan Rollback',
    ag_plan_rollback_steps: 'Langkah rollback (hanya berjalan saat Anda klik Jalankan Rollback)',
    ag_plan_st_proposed: 'Menunggu persetujuan Anda', ag_plan_st_running: 'Sedang berjalan', ag_plan_st_done: 'Selesai',
    ag_plan_st_failed: 'Gagal', ag_plan_st_verify_failed: 'Verifikasi gagal, rollback tersedia',
    ag_plan_st_rolling_back: 'Sedang di-rollback', ag_plan_st_rolled_back: 'Sudah di-rollback', ag_plan_st_rollback_failed: 'Rollback gagal',
    ag_plan_st_cancelled: 'Dihentikan',
    ag_plan_s_pending: 'menunggu', ag_plan_s_running: 'berjalan', ag_plan_s_done: 'selesai', ag_plan_s_failed: 'gagal', ag_plan_s_skipped: 'dilewati',
      ag_appr_title: 'Agent ingin menjalankan', ag_appr_exec: 'Perintah shell (selalu butuh persetujuan)', ag_appr_infra: 'Perubahan infrastruktur (selalu butuh persetujuan)', ag_approve: 'Setujui', ag_deny: 'Tolak', ag_approve_session: 'Setujui untuk sesi ini', ag_session_hint: 'Menyetujui semua perubahan berikutnya di {host} tanpa bertanya (termasuk perintah shell: {shell}). Berakhir saat Chat baru, Cabut, atau setelah 2 jam.', ag_grants: 'Disetujui otomatis di sesi ini', ag_revoke: 'Cabut', ag_g_shell: 'termasuk shell', ag_g_tools: 'aksi service/container',
      ag_s_enable: 'Aktifkan agen AI',
      ag_s_provider: 'Penyedia LLM', ag_s_provider_none: 'Belum dikonfigurasi', ag_s_provider_anthropic: 'Anthropic', ag_s_provider_openai: 'OpenAI-compatible',
      ag_s_base_url: 'Base URL', ag_s_base_url_hint: 'Kosong = default penyedia. OpenAI-compatible mencakup OpenRouter, LiteLLM, vLLM, Ollama, dan gateway lainnya.',
      ag_s_key: 'API key', ag_s_key_saved: 'Tersimpan. Kosongkan untuk tetap memakainya.',
      ag_s_key_hint: 'Wajib untuk Anthropic. Opsional untuk server lokal OpenAI-compatible (Ollama, vLLM).',
      ag_s_model: 'Model', ag_s_model_hint: 'Wajib: agent butuh model untuk menjawab.',
      ag_s_reasoning_title: 'Model Penalaran (Utama)', ag_s_reasoning_hint: 'Dipakai untuk RCA kompleks, penulisan YAML/HCL/Playbook, dan remediasi.',
      ag_s_fast_title: 'Model Cepat / Utilitas (Opsional)', ag_s_fast_hint: 'Dipakai untuk meringkas output besar, klasifikasi intent/routing, dan membuat judul thread otomatis. Kosong berarti fallback ke Model Penalaran.',
      ag_s_fast_provider: 'Penyedia model cepat', ag_s_fast_provider_same: 'Sama dengan Model Penalaran',
      ag_s_fast_base_url: 'Base URL model cepat', ag_s_fast_base_url_hint: 'Opsional: misal http://localhost:11434/v1 untuk Ollama lokal.',
      ag_s_fast_key: 'API key model cepat', ag_s_fast_key_hint: 'Opsional untuk server lokal (Ollama, vLLM). Kosongkan untuk tetap memakai key tersimpan atau mewarisi model utama.',
      ag_s_fast_model: 'Nama model cepat', ag_s_fast_model_hint: 'Opsional: misal qwen2.5:7b, claude-haiku-4-5, atau gpt-4o-mini. Kosong = fallback ke Model Penalaran.',
      ag_s_test_fast: 'Tes model cepat',
      ag_s_triage_enable: 'Auto-Triage AI Proaktif', ag_s_triage_hint: 'Diagnosa kegagalan umum secara lokal (0 token) dan pakai satu panggilan LLM singkat untuk sisanya',
      ag_s_triage_model: 'Model triage ringan (opsional)', ag_s_triage_model_hint: 'Kosong = memakai model agent utama',
      ag_s_triage_budget: 'Batas token triage harian', ag_s_triage_budget_hint: 'Batas global seluruh panggilan auto-triage per hari',
      ag_s_briefing_time: 'Waktu briefing harian (HH:MM, opsional)', ag_s_briefing_hint: 'Ringkasan delta 24 jam terakhir; kosong menonaktifkan jadwal',
      ag_s_approval: 'Persetujuan perubahan', ag_s_ask: 'Tanya saya untuk setiap perubahan (disarankan)',
      ag_s_auto: 'Setujui otomatis aksi service/container, scan, dan inspeksi (perintah shell tetap bertanya)',
      ag_s_soul: 'SOUL (identitas dan aturan agent, dikirim di setiap chat)', ag_s_soul_reset: 'Kembalikan SOUL ke default',
      ag_s_save: 'Simpan', ag_s_test: 'Tes koneksi', ag_s_saved: 'Tersimpan', ag_s_test_ok: 'Penyedia menjawab',
      ag_s_testing: 'Menguji koneksi…',
      ag_q_unit: 'Service {unit} di host "{host}" (id {id}) bermasalah. Cari akar masalahnya dan usulkan perbaikan. Cek status dan journal dulu. Jangan ubah apa pun sebelum saya setujui rencananya.',
      ag_q_container: 'Container {name} di host "{host}" (id {id}) bermasalah. Baca log dan statusnya, cari akar masalah, lalu usulkan perbaikan. Jangan ubah apa pun sebelum saya setujui rencananya.',
      ag_q_stack: 'Stack Docker Compose "{stack}" di host "{host}" (id {id}) perlu diperiksa. Cek container, status, dan log-nya, temukan masalah jika ada, lalu usulkan perbaikan. Jangan ubah apa pun sebelum saya setujui rencananya.',
      ag_q_alert: 'Alert di host "{host}" (id {id}): {msg}\nCuplikan root cause:\n{rc}\nSelidiki dan usulkan perbaikan. Jangan ubah apa pun sebelum saya setujui rencananya.',
      ag_m_title: 'Memori agent', ag_m_desc: 'Catatan permanen yang disimpan agent tentang infrastruktur Anda dan dibaca ulang di awal setiap chat. Tinjau: hapus yang salah. Catatan dikirim ke penyedia LLM Anda di setiap chat.',
      ag_m_empty: 'Belum ada catatan. Agent menambahkannya saat memahami setup Anda; Anda juga bisa menambah sendiri di bawah.', ag_m_fleet: 'Seluruh fleet', ag_m_ph: 'misal: Backup jalan jam 02:00 lewat /opt/backup.sh',
      ag_m_scope: 'Lingkup', ag_m_scope_fleet: 'Seluruh fleet', ag_m_scope_host: 'Satu host', ag_m_scope_cluster: 'Satu cluster', ag_m_scope_domain: 'Satu domain',
      ag_m_add: 'Tambah', ag_m_forget: 'Lupakan', ag_m_forget_confirm: 'Hapus catatan ini?', ag_m_by_agent: 'agent', ag_m_by_you: 'Anda',
      ag_q_security: 'Tinjau scan keamanan terakhir host "{host}" (id {id}): update keamanan tertunda dan hardening yang gagal. Urutkan yang penting dan usulkan perbaikan aman. Jangan ubah apa pun sebelum saya setujui rencananya.',
      ag_patch_preview: 'Pratinjau Diff', ag_patch_dryrun: 'Validasi / Dry-Run', ag_patch_dryrunning: 'Memvalidasi…',
      ag_patch_apply: 'Terapkan Perubahan', ag_patch_applying: 'Menerapkan…', ag_patch_applied: 'Diterapkan',
      ag_patch_view_split: 'Berdampingan', ag_patch_view_unified: 'Diff Terpadu',
      ag_patch_before: 'Saat Ini (Sebelum)', ag_patch_after: 'Usulan (Sesudah)',
      ag_patch_need_dryrun: 'Jalankan Validasi / Dry-Run terlebih dahulu', ag_patch_valid_ok: 'Validasi berhasil', ag_patch_valid_fail: 'Validasi gagal',
      ag_patch_apply_ok: 'Perubahan diterapkan',
      ag_ask_remediate: 'Tanya Agent untuk Remediasi', ag_ask_fix: 'Tanya Agent untuk Perbaiki', ag_ask_explain_plan: 'Jelaskan Plan & Risiko',
      ag_q_remediate: 'Remediasi temuan {source} ini:\n- Aturan/ID: {rule}\n- Tingkat: {severity}\n- Target: {target}\n- Temuan: {title}\n- Detail: {detail}\n- Saran perbaikan: {remediation}\nSelidiki dan usulkan perbaikan aman (sertakan blok pantau-patch jika mengubah config/YAML/HCL). Jangan terapkan perubahan sebelum saya setujui.',
      ag_q_ansible_fix: 'Task Ansible gagal pada "{playbook}" (run #{run_id}) di host "{host}" (status: {status}):\nTask: {task}\nError:\n{error}\nCari akar masalahnya dan usulkan perbaikan aman (sertakan blok pantau-patch jika mengubah playbook). Jangan jalankan atau ubah apa pun sebelum saya setujui.',
      ag_q_tf_explain: 'Jelaskan Terraform plan serta ringkasan kebijakan/risiko untuk workspace "{workspace}" (run #{run_id}, status: {status}):\n- Plan: +{create} tambah, ~{update} ubah, ±{replace} ganti, -{delete} hapus\n- Kebijakan: {policy}\n- Delta biaya: {cost}\nSoroti perubahan yang berisiko, penghapusan resource, dan pelanggaran kebijakan.'
    }
  };
  ['en', 'id'].forEach((l) => { I18N[l] = Object.assign(I18N[l] || {}, I[l]); });

  const esc = (s) => escapeHtml(String(s === null || s === undefined ? '' : s));
  const fill = (s, v) => Object.keys(v).reduce((a, k) => a.split('{' + k + '}').join(v[k]), s);
  const $ = (id) => document.getElementById(id);
  const api = async (url, opts) => {
    const r = await fetch(url, opts);
    const j = await r.json().catch(() => ({}));
    return { ok: r.ok, status: r.status, data: j };
  };
  const post = (url, body) => api(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body || {}) });

  S.session = localStorage.getItem(SESSION_KEY) || newSession();
  function newSession() {
    const id = 'pantau-' + Math.random().toString(36).slice(2, 10) + Date.now().toString(36);
    localStorage.setItem(SESSION_KEY, id);
    return id;
  }

  /* ---------- context detection & thread binding ---------- */
  function isElOpen(id) {
    const el = $(id);
    return !!(el && !el.hidden && el.classList.contains('open'));
  }

  function cleanText(id) {
    const el = $(id);
    return el ? String(el.textContent || '').trim() : '';
  }

  function detectContext() {
    const h = typeof currentHost !== 'undefined' && currentHost && currentHost.id ? currentHost : null;
    const page = typeof currentSideNav === 'string' ? currentSideNav : '';
    let ctx = null;

    if (isElOpen('dockerInspectModal')) {
      const name = cleanText('dockerInspectTitle') || 'container';
      ctx = { type: 'docker', scope_type: h ? 'host' : 'docker', scope_id: h ? Number(h.id) : 0, icon: 'container', label: t('ag_ctx_docker') + ': ' + name, target: name };
    } else if (isElOpen('dockerLogsModal')) {
      const raw = cleanText('dockerLogsTitle');
      const m = raw.match(/:\s*([^@]+)/);
      const name = (m ? m[1] : raw).trim() || 'container';
      ctx = { type: 'docker', scope_type: h ? 'host' : 'docker', scope_id: h ? Number(h.id) : 0, icon: 'container', label: t('ag_ctx_docker') + ': ' + name, target: name };
    } else if (isElOpen('kubeLogsModal')) {
      const pod = cleanText('kubeLogsTitle') || 'pod';
      const kid = typeof selectedKubeHostId !== 'undefined' && selectedKubeHostId ? Number(selectedKubeHostId) : 0;
      const st = kid < 0 ? 'cluster' : (kid > 0 ? 'host' : 'cluster');
      const sid = kid < 0 ? -kid : (kid > 0 ? kid : 0);
      ctx = { type: 'kube_pod', scope_type: st, scope_id: sid, icon: 'boxes', label: t('ag_ctx_pod') + ': ' + pod, target: pod };
    } else if (isElOpen('hostDetailModal') && h) {
      const name = h.name || ('#' + h.id);
      if (typeof window.S !== 'undefined' && window.S && window.S.currentTab === 'docker') {
        ctx = { type: 'docker', scope_type: 'host', scope_id: Number(h.id), icon: 'container', label: t('ag_ctx_docker') + ': ' + name, target: name };
      } else {
        ctx = { type: 'host', scope_type: 'host', scope_id: Number(h.id), icon: 'server', label: t('ag_ctx_host') + ': ' + name, target: name };
      }
    } else if (page === 'kube') {
      const kid = typeof selectedKubeHostId !== 'undefined' && selectedKubeHostId ? Number(selectedKubeHostId) : 0;
      let label = t('ag_ctx_kube') + ': Fleet';
      let target = 'Kubernetes Fleet';
      let st = 'cluster';
      let sid = 0;
      if (kid < 0) {
        sid = -kid;
        const cl = (typeof kubeClusters !== 'undefined' ? kubeClusters : []).find((c) => c.id === sid);
        target = cl ? cl.name : ('Cluster #' + sid);
        label = t('ag_ctx_kube') + ': ' + target;
      } else if (kid > 0) {
        st = 'host';
        sid = kid;
        const kh = (typeof allRawHosts !== 'undefined' ? allRawHosts : []).find((x) => Number(x.id) === sid);
        target = kh ? kh.name : ('Host #' + sid);
        label = t('ag_ctx_kube') + ': ' + target;
      }
      ctx = { type: 'kube', scope_type: st, scope_id: sid, icon: 'boxes', label, target };
    } else if (page === 'docker') {
      ctx = { type: 'docker', scope_type: 'docker', scope_id: 0, icon: 'container', label: t('ag_ctx_docker') + ': Fleet', target: 'Docker Fleet' };
    } else if (page === 'ansible') {
      const open = isElOpen('ansDrawerRoot');
      const title = open ? cleanText('ansDrawerTitle') : '';
      const target = title || 'Ansible';
      ctx = { type: 'ansible', scope_type: 'ansible', scope_id: 0, icon: 'ansible', label: title ? t('ag_ctx_ansible') + ': ' + title : t('ag_ctx_ansible'), target };
    } else if (page === 'terraform') {
      const open = isElOpen('tfDrawerRoot');
      const title = open ? cleanText('tfDrawerTitle') : '';
      const target = title || 'Terraform';
      ctx = { type: 'terraform', scope_type: 'terraform', scope_id: 0, icon: 'terraform', label: title ? t('ag_ctx_terraform') + ': ' + title : t('ag_ctx_terraform'), target };
    } else {
      ctx = { type: 'global', scope_type: 'global', scope_id: 0, icon: 'globe', label: t('ag_ctx_global'), target: 'Fleet' };
    }
    ctx.sig = [ctx.type, ctx.scope_type, ctx.scope_id, ctx.label].join('|');
    return ctx;
  }

  function scope() {
    const ctx = S.ctx || detectContext();
    return { scope_type: ctx.scope_type, scope_id: ctx.scope_id };
  }

  function chipsForContext(ctx) {
    const vars = { target: ctx.target, id: ctx.scope_id };
    if (ctx.type === 'host') {
      return [
        { label: t('ag_chip_explain'), prompt: fill(t('ag_qp_host_explain'), vars) },
        { label: t('ag_chip_diagnose'), prompt: fill(t('ag_qp_host_diag'), vars) },
        { label: t('ag_chip_fix'), prompt: fill(t('ag_qp_host_fix'), vars) }
      ];
    }
    if (ctx.type === 'kube' || ctx.type === 'kube_pod') {
      return [
        { label: t('ag_chip_explain'), prompt: fill(t('ag_qp_kube_explain'), vars) },
        { label: t('ag_chip_diagnose'), prompt: fill(t('ag_qp_kube_diag'), vars) },
        { label: t('ag_chip_fix'), prompt: fill(t('ag_qp_kube_fix'), vars) }
      ];
    }
    if (ctx.type === 'docker') {
      return [
        { label: t('ag_chip_explain'), prompt: fill(t('ag_qp_docker_explain'), vars) },
        { label: t('ag_chip_diagnose'), prompt: fill(t('ag_qp_docker_diag'), vars) },
        { label: t('ag_chip_fix'), prompt: fill(t('ag_qp_docker_fix'), vars) }
      ];
    }
    if (ctx.type === 'ansible') {
      return [
        { label: t('ag_chip_explain'), prompt: fill(t('ag_qp_ansible_explain'), vars) },
        { label: t('ag_chip_diagnose'), prompt: fill(t('ag_qp_ansible_diag'), vars) },
        { label: t('ag_chip_fix'), prompt: fill(t('ag_qp_ansible_fix'), vars) }
      ];
    }
    if (ctx.type === 'terraform') {
      return [
        { label: t('ag_chip_drift'), prompt: fill(t('ag_qp_tf_drift'), vars) },
        { label: t('ag_chip_explain'), prompt: fill(t('ag_qp_tf_explain'), vars) },
        { label: t('ag_chip_fix'), prompt: fill(t('ag_qp_tf_fix'), vars) }
      ];
    }
    return [
      { label: t('ag_chip_diagnose'), prompt: fill(t('ag_qp_global_diag'), vars) },
      { label: t('ag_chip_explain'), prompt: fill(t('ag_qp_global_explain'), vars) },
      { label: t('ag_chip_fix'), prompt: fill(t('ag_qp_global_fix'), vars) }
    ];
  }

  function renderContextBar() {
    const bar = $('agentCtxBar'), pill = $('agentCtxPill'), chipsEl = $('agentChips');
    if (!bar || !pill || !chipsEl) return;
    const ctx = S.ctx || detectContext();
    pill.innerHTML = `${svgIcon(ctx.icon || 'globe', 'icon-sm')} <span>${esc(ctx.label)}</span>`;
    pill.title = ctx.label;
    bar.style.display = S.enabled ? 'flex' : 'none';
    const chips = chipsForContext(ctx);
    chipsEl.innerHTML = chips.map((c, idx) =>
      `<button type="button" class="ag-chip" data-chip-idx="${idx}" ${S.busy ? 'disabled' : ''}>${esc(c.label)}</button>`
    ).join('');
    chipsEl.querySelectorAll('.ag-chip').forEach((btn) => {
      btn.addEventListener('click', () => {
        const item = chips[Number(btn.dataset.chipIdx)];
        if (item) AG.ask(item.prompt);
      });
    });
  }

  async function bindThreadForScope(scopeType, scopeID) {
    const key = scopeType + ':' + scopeID;
    S.scope = key;
    const r = await api('/api/agent/threads?scope_type=' + encodeURIComponent(scopeType) + '&scope_id=' + Number(scopeID || 0)).catch(() => null);
    if ((S.ctx ? S.ctx.scope_type + ':' + S.ctx.scope_id : '') !== key) return;
    const th = r && r.ok ? (r.data.threads || [])[0] : null;
    if (!th) {
      S.thread = 0;
      S.msgs = [];
      renderLog();
      return;
    }
    S.thread = th.id;
    const d = await api('/api/agent/threads/' + th.id).catch(() => null);
    if ((S.ctx ? S.ctx.scope_type + ':' + S.ctx.scope_id : '') !== key) return;
    if (!d || !d.ok) return;
    const restored = [];
    for (const m of d.data.messages || []) {
      if (m.role === 'user') restored.push({ role: 'user', text: m.content });
      else if (m.role === 'assistant' && m.content) restored.push({ role: 'bot', text: m.content });
      else if (m.role === 'tool') restored.push({ role: 'tool', text: (m.tool_name || 'tool'), status: m.is_error ? 'error' : 'done', id: m.tool_call_id });
    }
    S.msgs = restored;
    renderLog();
  }

  function syncContext() {
    const next = detectContext();
    const prevScope = S.ctx ? (S.ctx.scope_type + ':' + S.ctx.scope_id) : '';
    const changed = !S.ctx || S.ctx.sig !== next.sig;
    if (!changed) return;
    S.ctx = next;
    renderContextBar();
    const newScope = next.scope_type + ':' + next.scope_id;
    if (S.enabled && !S.busy && prevScope && newScope !== prevScope) {
      if (S.skipAutoLoadForScope !== newScope) {
        S.skipAutoLoadForScope = '';
        bindThreadForScope(next.scope_type, next.scope_id);
      }
    }
  }

  // Hydrate paints the stored conversation once per page load; a new chat skips it.
  async function hydrate() {
    if (S.hydrated) return;
    S.hydrated = true;
    S.ctx = detectContext();
    renderContextBar();
    const r = await api('/api/agent/threads?session=' + encodeURIComponent(S.session)).catch(() => null);
    const th = r && r.ok ? (r.data.threads || [])[0] : null;
    if (!th) {
      await bindThreadForScope(S.ctx.scope_type, S.ctx.scope_id);
      return;
    }
    S.thread = th.id;
    S.scope = th.scope_type + ':' + th.scope_id;
    const d = await api('/api/agent/threads/' + th.id).catch(() => null);
    if (!d || !d.ok) return;
    const restored = [];
    for (const m of d.data.messages || []) {
      if (m.role === 'user') restored.push({ role: 'user', text: m.content });
      else if (m.role === 'assistant' && m.content) restored.push({ role: 'bot', text: m.content });
      else if (m.role === 'tool') restored.push({ role: 'tool', text: (m.tool_name || 'tool'), status: m.is_error ? 'error' : 'done', id: m.tool_call_id });
    }
    if (restored.length) { S.msgs = restored; renderLog(); }
  }

  /* ---------- minimal markdown + structured pantau-patch cards ---------- */
  const PATCH_KIND_LABEL = {
    k8s_yaml: 'K8s YAML',
    sftp_file: 'SFTP Config',
    ansible_playbook: 'Ansible Playbook',
    terraform_hcl: 'Terraform HCL',
    docker_compose: 'Docker Compose'
  };
  S.patches = S.patches || {};
  let patchSeq = 0;

  function renderPatchCard(rawJson) {
    let p = null;
    try { p = JSON.parse(rawJson); } catch (_) { return null; }
    if (!p || typeof p !== 'object' || !p.kind || typeof p.after !== 'string') return null;
    const id = 'p_' + (++patchSeq);
    S.patches[id] = Object.assign({ id }, p);
    const kindLabel = PATCH_KIND_LABEL[p.kind] || p.kind;
    const title = p.title || p.target || p.stack_name || kindLabel;
    const target = p.target || p.stack_name || '';
    return `<div class="ag-patch-card" data-patch-id="${esc(id)}">
      <div class="ag-patch-head">
        <span class="badge badge-info">${esc(kindLabel)}</span>
        <span class="ag-patch-title">${esc(title)}</span>
      </div>
      ${target ? `<div class="ag-patch-target">${esc(target)}</div>` : ''}
      <div class="ag-patch-actions">
        <button type="button" class="btn btn-sm btn-primary" onclick="AG.openPatchDiff('${esc(id)}')">${svgIcon('git-compare', 'icon-sm')} <span>${esc(t('ag_patch_preview'))}</span></button>
      </div>
    </div>`;
  }

  function md(text) {
    const parts = String(text).split(/```/);
    return parts.map((p, i) => {
      if (i % 2 === 1) {
        if (/^pantau-patch\s*\n/i.test(p)) {
          const card = renderPatchCard(p.replace(/^pantau-patch\s*\n/i, '').trim());
          if (card) return card;
        }
        return '<pre><code>' + esc(p.replace(/^[a-zA-Z0-9_+-]*\n/, '')) + '</code></pre>';
      }
      return esc(p).replace(/`([^`\n]+)`/g, '<code>$1</code>').replace(/\*\*([^*\n]+)\*\*/g, '<b>$1</b>').replace(/\n/g, '<br>');
    }).join('');
  }

  /* ---------- UI shell ---------- */
  function build() {
    if ($('agentFab')) return;
    const root = document.createElement('div');
    root.innerHTML = `
      <button id="agentFab" type="button" title="${esc(t('ag_title'))}" aria-label="${esc(t('ag_title'))}" onclick="AG.toggle()">${svgIcon('bot')}<span class="ag-badge" id="agentBadge"></span></button>
      <div id="agentPanel" role="dialog" aria-label="${esc(t('ag_title'))}">
        <div class="ag-resizer" id="agentResizer" role="separator" aria-orientation="vertical" aria-label="${esc(t('ag_resize_panel'))}"></div>
        <div class="ag-head">${svgIcon('bot', 'icon-sm')}<span>${esc(t('ag_title'))}</span><span class="ag-sp"></span>
          <button class="btn btn-sm btn-ghost" onclick="AG.distill()" title="${esc(t('ag_save_kb'))}">${esc(t('ag_save_kb'))}</button>
          <button class="btn btn-sm btn-ghost" onclick="AG.newChat()">${esc(t('ag_new'))}</button>
          <button class="btn btn-sm btn-icon btn-ghost" id="agentDockToggle" onclick="AG.toggleDock()"></button>
          <button class="btn btn-sm btn-icon btn-ghost" id="agentExpand" onclick="AG.expand()"></button>
          <button class="btn btn-sm btn-icon btn-ghost" aria-label="${esc(t('ag_close'))}" onclick="AG.toggle(false)">${svgIcon('x', 'icon-sm')}</button></div>
        <div class="ag-log" id="agentLog"></div>
        <div class="ag-ctx-bar" id="agentCtxBar" style="display:none;">
          <div class="ag-ctx-row"><span class="ag-ctx-pill" id="agentCtxPill"></span></div>
          <div class="ag-chips" id="agentChips"></div>
        </div>
        <form class="ag-form" onsubmit="AG.submit(event)">
          <textarea id="agentInput" rows="1" placeholder="${esc(t('ag_ph'))}" onkeydown="AG.key(event)"></textarea>
          <button class="btn btn-primary btn-sm" id="agentSend" type="submit">${svgIcon('send', 'icon-sm')}</button>
        </form>
      </div>
      <div id="agentApprovals"></div>`;
    while (root.firstChild) document.body.appendChild(root.firstChild);
    S.fab = storedPos() || defaultPos();
    S.ctx = detectContext();
    renderLog();
    renderContextBar();
    paintPanel();
    wirePlacement();
    wireResizer();
  }

  function renderLog() {
    const log = $('agentLog');
    if (!log) return;
    if (!S.enabled) { log.innerHTML = `<div class="ag-empty">${esc(t('ag_disabled'))}<br><br><button class="btn btn-sm btn-primary" onclick="AG.toggle(false); openSettingsModal('agent')">${esc(t('ag_open_settings'))}</button></div>`; return; }
    if (S.msgs.length === 0) { log.innerHTML = `<div class="ag-empty">${esc(t('ag_empty'))}</div>`; return; }
    log.innerHTML = S.msgs.map((m) => {
      if (m.role === 'user') return `<div class="ag-msg ag-user">${esc(m.text)}</div>`;
      if (m.role === 'tool') {
        const mark = m.status === 'error' ? svgIcon('x', 'icon-sm') : m.status === 'done' ? svgIcon('check', 'icon-sm') : svgIcon('wrench', 'icon-sm');
        const meta = [m.domain, m.risk_tier, m.duration_ms !== undefined ? m.duration_ms + 'ms' : ''].filter(Boolean).join(' · ');
        return `<div class="ag-tool ag-tool-${m.status || 'running'}${m.risk_tier === 'destructive' ? ' ag-tool-danger' : ''}">${mark} ${esc(m.text)}${meta ? ` <span style="opacity:.6;font-size:.85em;">${esc(meta)}</span>` : ''}</div>`;
      }
      if (m.role === 'plan') return renderPlanCard(m.planId);
      if (m.role === 'error') return `<div class="ag-err">${esc(m.text)}${m.settings ? `<div style="margin-top:8px;"><button class="btn btn-sm btn-primary" onclick="AG.toggle(false); openSettingsModal('agent')">${esc(t('ag_open_settings'))}</button></div>` : ''}</div>`;
      if (!m.text) return `<div class="ag-msg ag-bot">${S.approvals.length ? SP.html(t('ag_waiting_approval'), S.apprT0) : SP.html(t('ag_working'), S.chatT0)}</div>`;
      return `<div class="ag-msg ag-bot">${md(m.text)}</div>`;
    }).join('');
    log.scrollTop = log.scrollHeight;
  }

  // Streaming deltas arrive many times per frame; re-parsing the markdown of the whole chat for each is what made long answers lag.
  let logFrame = 0;
  function scheduleLog() {
    if (!logFrame) logFrame = requestAnimationFrame(() => { logFrame = 0; renderLog(); });
  }

  function triggerLayoutReflow() {
    try { window.dispatchEvent(new Event('resize')); } catch (_) {}
    if (typeof window.refitActiveTab === 'function') {
      try { window.refitActiveTab(); } catch (_) {}
    }
    if (window.codeEditor && typeof window.codeEditor.refresh === 'function') {
      try { window.codeEditor.refresh(); } catch (_) {}
    }
    document.querySelectorAll('.CodeMirror').forEach((el) => {
      if (el.CodeMirror && typeof el.CodeMirror.refresh === 'function') {
        try { el.CodeMirror.refresh(); } catch (_) {}
      }
    });
  }

  function scheduleLayoutReflow() {
    triggerLayoutReflow();
    if (S.reflowTimer) clearTimeout(S.reflowTimer);
    S.reflowTimer = setTimeout(() => {
      S.reflowTimer = null;
      triggerLayoutReflow();
    }, 220);
  }

  AG.toggle = function (force) {
    // A drag ends with a click on the bubble/header; that click must not toggle the chat.
    if (S.suppressToggle && force === undefined) { S.suppressToggle = false; return; }
    S.open = force === undefined ? !S.open : !!force;
    if (S.open) syncContext();
    paintPanel();
    scheduleLayoutReflow();
    if (S.open) setTimeout(() => $('agentInput') && $('agentInput').focus(), 30);
  };

  AG.toggleDock = function (force) {
    S.docked = force === undefined ? !S.docked : !!force;
    localStorage.setItem(MODE_KEY, S.docked ? 'docked' : 'floating');
    paintPanel();
    scheduleLayoutReflow();
  };

  // Panel classes and the expand/dock buttons follow S, so a panel rebuilt by a language switch keeps its state.
  function paintPanel() {
    const panel = $('agentPanel');
    if (!panel) return;
    const vw = document.documentElement.clientWidth || window.innerWidth;
    const dockW = clampNum(S.dockWidth || DOCK_DEFAULT_W, DOCK_MIN_W, Math.max(DOCK_MIN_W, Math.floor(vw * 0.65)));
    document.documentElement.style.setProperty('--agent-dock-width', dockW + 'px');
    panel.classList.toggle('open', S.open);
    panel.classList.toggle('ag-docked', S.docked);
    panel.classList.toggle('ag-expanded', !S.docked && S.expanded);
    document.body.classList.toggle('ag-docked-open', !!(S.open && S.docked));
    const dockBtn = $('agentDockToggle');
    if (dockBtn) {
      const dockLabel = t(S.docked ? 'ag_float' : 'ag_dock');
      dockBtn.innerHTML = svgIcon(S.docked ? 'layers' : 'columns-2', 'icon-sm');
      dockBtn.title = dockLabel;
      dockBtn.setAttribute('aria-label', dockLabel);
      dockBtn.setAttribute('aria-pressed', S.docked ? 'true' : 'false');
    }
    const label = t(S.expanded ? 'ag_collapse' : 'ag_expand');
    const btn = $('agentExpand');
    if (btn) {
      btn.style.display = S.docked ? 'none' : '';
      btn.innerHTML = svgIcon(S.expanded ? 'minimize-2' : 'maximize-2', 'icon-sm');
      btn.title = label;
      btn.setAttribute('aria-label', label);
    }
    applyLayout();
  }

  AG.expand = function (force) {
    S.expanded = force === undefined ? !S.expanded : !!force;
    paintPanel();
  };

  /* ---------- placement: the bubble is the only anchor, the chat docks above it (below when there is no room) ---------- */
  const FAB = 48, GAP = 12, PAD = 8, PANEL_H = 560;

  function clampNum(v, lo, hi) { return Math.min(Math.max(v, lo), Math.max(lo, hi)); }

  function defaultPos() { return { x: window.innerWidth - 24 - FAB, y: window.innerHeight - 52 - FAB }; }

  function storedPos() {
    try {
      const p = JSON.parse(localStorage.getItem(POS_KEY) || 'null');
      if (p && isFinite(p.x) && isFinite(p.y)) return { x: Number(p.x), y: Number(p.y) };
    } catch (e) { /* a corrupt entry just means the default corner */ }
    return null;
  }

  // Always opaque, always on screen: the bubble stays in the viewport, and the chat docks above or below
  // it without ever covering it, shrinking (max-height, log scrolls) when neither side has room.
  function applyLayout() {
    const fab = $('agentFab'), panel = $('agentPanel'), appr = $('agentApprovals');
    if (!fab || !panel) return;
    const vw = document.documentElement.clientWidth || window.innerWidth;
    const vh = document.documentElement.clientHeight || window.innerHeight;
    const p = S.fab || defaultPos();
    S.fab = { x: clampNum(p.x, PAD, vw - FAB - PAD), y: clampNum(p.y, PAD, vh - FAB - PAD) };
    fab.style.left = Math.round(S.fab.x) + 'px';
    fab.style.top = Math.round(S.fab.y) + 'px';
    const aw = appr ? appr.offsetWidth || Math.min(420, vw - PAD * 2) : 0;
    // Approval cards and the grants bar float on the far side of the chat; the chat yields them that space,
    // so the two never overlap while the chat is open.
    const band = appr && S.open ? appr.offsetHeight : 0;
    const reserve = band ? band + GAP : 0;
    if (S.docked) {
      panel.style.maxHeight = '';
      panel.style.left = '';
      panel.style.top = '';
      if (appr) {
        const dockW = clampNum(S.dockWidth || DOCK_DEFAULT_W, DOCK_MIN_W, Math.max(DOCK_MIN_W, Math.floor(vw * 0.65)));
        const ax = S.open ? clampNum(vw - dockW - GAP - aw, PAD, vw - aw - PAD) : clampNum(S.fab.x + FAB - aw, PAD, vw - aw - PAD);
        const ay = S.open ? clampNum(vh - 36 - band - GAP, PAD, vh - band - PAD) : clampNum(S.fab.y - GAP - band, PAD, vh - band - PAD);
        appr.style.left = Math.round(ax) + 'px';
        appr.style.top = Math.round(ay) + 'px';
      }
      return;
    }
    // Width/height from the target state (see #agentPanel in 00-agent.css), not the box: the box is mid-transition.
    const w = Math.min(S.expanded ? 960 : 420, vw - PAD * 2);
    const above = S.fab.y - GAP - PAD, below = vh - (S.fab.y + FAB) - GAP - PAD;
    const useAbove = above >= below;
    const h = Math.max(120, Math.min(PANEL_H, vh - 120, Math.max(above, below) - reserve));
    panel.style.maxHeight = h + 'px';
    const x = clampNum(S.fab.x + FAB - w, PAD, vw - w - PAD);
    const y = clampNum(useAbove ? S.fab.y - GAP - h : S.fab.y + FAB + GAP, PAD, vh - h - PAD);
    panel.style.left = Math.round(x) + 'px';
    panel.style.top = Math.round(y) + 'px';
    if (!appr) return;
    const base = S.open ? (useAbove ? y : y + h) : S.fab.y;
    const up = S.open ? useAbove : true;
    const ay = clampNum(up ? base - GAP - band : base + GAP, PAD, vh - band - PAD);
    appr.style.left = Math.round(clampNum(S.fab.x + FAB - aw, PAD, vw - aw - PAD)) + 'px';
    appr.style.top = Math.round(ay) + 'px';
  }

  function dragFrom(e) {
    if (e.button > 0 || S.drag) return;
    const handle = e.currentTarget;
    const from = { x: S.fab.x, y: S.fab.y, px: e.clientX, py: e.clientY };
    S.drag = { moved: false };
    if (handle.setPointerCapture) handle.setPointerCapture(e.pointerId);
    const move = (ev) => {
      const dx = ev.clientX - from.px, dy = ev.clientY - from.py;
      if (!S.drag.moved) {
        if (Math.abs(dx) + Math.abs(dy) < 4) return;
        S.drag.moved = true;
        $('agentFab').classList.add('ag-dragging');
        $('agentPanel').classList.add('ag-dragging');
      }
      // applyLayout clamps, so the widget can never be dragged off screen.
      S.fab = { x: from.x + dx, y: from.y + dy };
      applyLayout();
    };
    const up = () => {
      handle.removeEventListener('pointermove', move);
      handle.removeEventListener('pointerup', up);
      handle.removeEventListener('pointercancel', up);
      const moved = !!S.drag && S.drag.moved;
      S.drag = null;
      ['agentFab', 'agentPanel'].forEach((id) => { const n = $(id); if (n) n.classList.remove('ag-dragging'); });
      if (!moved) return;
      localStorage.setItem(POS_KEY, JSON.stringify({ x: Math.round(S.fab.x), y: Math.round(S.fab.y) }));
      S.suppressToggle = true;
      setTimeout(() => { S.suppressToggle = false; }, 0);
    };
    handle.addEventListener('pointermove', move);
    handle.addEventListener('pointerup', up);
    handle.addEventListener('pointercancel', up);
  }

  function wireResizer() {
    const resizer = $('agentResizer');
    const panel = $('agentPanel');
    if (!resizer || !panel) return;
    resizer.addEventListener('pointerdown', (e) => {
      if (e.button > 0 || !S.docked) return;
      e.preventDefault();
      e.stopPropagation();
      const startX = e.clientX;
      const startW = panel.getBoundingClientRect().width || S.dockWidth || DOCK_DEFAULT_W;
      panel.classList.add('ag-resizing');
      document.body.classList.add('ag-docked-resizing');
      if (resizer.setPointerCapture) resizer.setPointerCapture(e.pointerId);
      const move = (ev) => {
        const vw = document.documentElement.clientWidth || window.innerWidth;
        const maxW = Math.max(DOCK_MIN_W, Math.floor(vw * 0.65));
        const w = clampNum(Math.round(startW + (startX - ev.clientX)), DOCK_MIN_W, maxW);
        S.dockWidth = w;
        document.documentElement.style.setProperty('--agent-dock-width', w + 'px');
        applyLayout();
        triggerLayoutReflow();
      };
      const up = () => {
        resizer.removeEventListener('pointermove', move);
        resizer.removeEventListener('pointerup', up);
        resizer.removeEventListener('pointercancel', up);
        panel.classList.remove('ag-resizing');
        document.body.classList.remove('ag-docked-resizing');
        localStorage.setItem(DOCK_W_KEY, String(S.dockWidth));
        scheduleLayoutReflow();
      };
      resizer.addEventListener('pointermove', move);
      resizer.addEventListener('pointerup', up);
      resizer.addEventListener('pointercancel', up);
    });
  }

  function wirePlacement() {
    const fab = $('agentFab'), head = document.querySelector('#agentPanel .ag-head');
    fab.addEventListener('pointerdown', dragFrom);
    if (head) head.addEventListener('pointerdown', (e) => {
      if (S.docked) return;
      if (e.target.closest && e.target.closest('button, a, input, textarea, select')) return;
      dragFrom(e);
    });
    if (S.wired) return;
    S.wired = true;
    window.addEventListener('resize', applyLayout);
    // Click outside the widget closes the floating chat; docked side-panel stays open for split-view workflow.
    document.addEventListener('pointerdown', (e) => {
      if (!S.open || S.docked || !e.target.closest) return;
      if (e.target.closest('#agentPanel, #agentFab, #agentApprovals')) return;
      AG.toggle(false);
    }, true);
    document.addEventListener('click', () => { setTimeout(syncContext, 60); }, true);
    document.addEventListener('keydown', (e) => {
      if (e.key !== 'Escape' || !S.open || S.docked) return;
      if (document.querySelector('.modal.open')) return; // the modal stack owns Escape while a modal is on top
      AG.toggle(false);
    });
  }

  AG.newChat = function () {
    if (S.abort) S.abort.abort();
    api('/api/agent/grants', { method: 'DELETE' }).then(pollApprovals);
    S.msgs = [];
    S.session = newSession();
    S.thread = 0;
    const sc = scope();
    S.scope = sc.scope_type + ':' + sc.scope_id;
    S.skipAutoLoadForScope = S.scope;
    S.busy = false;
    setBusy(false);
    renderLog();
  };

  AG.key = function (e) {
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) { e.preventDefault(); AG.submit(e); }
  };

  function setBusy(b) {
    S.busy = b;
    const btn = $('agentSend');
    if (btn) {
      btn.innerHTML = b ? svgIcon('square', 'icon-sm') : svgIcon('send', 'icon-sm');
      btn.title = b ? t('ag_stop') : t('ag_send');
    }
    document.querySelectorAll('#agentChips .ag-chip').forEach((c) => { c.disabled = !!b; });
  }

  AG.submit = function (e) {
    if (e && e.preventDefault) e.preventDefault();
    if (S.busy) { if (S.abort) S.abort.abort(); return; }
    const inp = $('agentInput');
    const text = inp.value.trim();
    if (!text) return;
    inp.value = '';
    send(text);
  };

  AG.getContext = function () { return S.ctx || detectContext(); };

  /** Open the chat and send a message (used by the "Ask agent" buttons and Command Palette). */
  AG.ask = function (text, opts) {
    if (!S.enabled) { showToast(t('ag_disabled'), 'info'); return; }
    if (opts && opts.dock && !S.docked) AG.toggleDock(true);
    AG.toggle(true);
    if (S.busy) { showToast(t('ag_working'), 'info'); return; }
    send(text);
  };

  async function send(text) {
    S.msgs.push({ role: 'user', text });
    const bot = { role: 'bot', text: '' };
    S.msgs.push(bot);
    S.chatT0 = Date.now();
    renderLog();
    setBusy(true);
    S.abort = new AbortController();
    try {
      S.skipAutoLoadForScope = '';
      const sc = scope(), key = sc.scope_type + ':' + sc.scope_id;
      const res = await fetch('/api/agent/chat', {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, signal: S.abort.signal,
        body: JSON.stringify({ message: text, session_id: S.session, thread_id: S.scope === key ? S.thread : 0,
          page: typeof currentSideNav === 'string' ? currentSideNav : '',
          host_id: sc.scope_type === 'host' ? sc.scope_id : 0,
          cluster_id: sc.scope_type === 'cluster' ? sc.scope_id : 0 })
      });
      // The server names the thread it used; the panel binds to it so the next turn continues it.
      const bound = Number(res.headers.get('X-Agent-Thread')) || 0;
      if (bound) { S.thread = bound; S.scope = key; }
      if (!res.ok) {
        const j = await res.json().catch(() => ({}));
        const err = new Error(j.error || 'HTTP ' + res.status);
        err.settings = res.status === 400; // no provider/model configured: point at Settings → AI Agent
        throw err;
      }
      const turn = { mutated: false, planned: false };
      await readSSE(res.body, (ev, data) => {
        if (ev === 'agent.tool') {
          const label = (data.tool || 'tool') + (data.label && data.label !== data.tool ? ': ' + data.label : '');
          const line = { role: 'tool', text: label, status: data.status, id: data.toolCallId, domain: data.domain, risk_tier: data.risk_tier, duration_ms: data.duration_ms };
          const prev = S.msgs.findIndex((m) => m.role === 'tool' && m.id === data.toolCallId);
          if (prev >= 0 && data.status !== 'running') S.msgs[prev] = line;
          else S.msgs.splice(S.msgs.indexOf(bot) < 0 ? S.msgs.length : S.msgs.indexOf(bot), 0, line);
          // A proposed plan shows its card right after the tool line; the card keeps itself in sync with the server.
          if (data.plan_id && data.status !== 'running') {
            S.msgs.splice(S.msgs.indexOf(line) + 1, 0, { role: 'plan', planId: data.plan_id });
            refreshPlan(data.plan_id);
          }
          // Tools that add, change or regroup hosts: the host list reloads when one finishes, not at the next 30 s poll.
          // An action by the agent changes what its view shows: that view reloads now, not at the next manual refresh.
          if (data.status !== 'running' && data.risk_tier !== 'read_only') { turn.mutated = true; refreshForTool(data.tool); }
          if (data.plan_id) turn.planned = true;
        } else if (ev === 'agent.error') {
          S.msgs.push({ role: 'error', text: (data && data.error) || 'agent error' });
        } else if (data && data.choices && data.choices[0]) {
          const c = data.choices[0];
          if (c.delta && c.delta.content) bot.text += c.delta.content;
          if (c.finish_reason === 'error') S.msgs.push({ role: 'error', text: (data.error && data.error.message) || 'agent error' });
        }
        scheduleLog();
      });
      // Any view the action changed reloads with the page, once the turn is done: the chat comes back from the server.
      // A proposed plan keeps the page, because its card (approve, stop, rollback) lives only in memory.
      if (turn.mutated && !turn.planned) setTimeout(() => location.reload(), 1200);
    } catch (e) {
      if (e.name !== 'AbortError') S.msgs.push({ role: 'error', text: e.message, settings: !!e.settings });
    } finally {
      if (!bot.text) S.msgs.splice(S.msgs.indexOf(bot), 1);
      S.abort = null;
      setBusy(false);
      renderLog();
      pollApprovals();
    }
  }

  /** Parses an SSE byte stream into (event, json) callbacks; comment lines (": keepalive") are skipped. */
  async function readSSE(body, cb) {
    const reader = body.getReader();
    const dec = new TextDecoder();
    let buf = '';
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      buf += dec.decode(value, { stream: true });
      let i;
      while ((i = buf.indexOf('\n\n')) >= 0) {
        const frame = buf.slice(0, i);
        buf = buf.slice(i + 2);
        let ev = '', data = '';
        frame.split('\n').forEach((l) => {
          if (l.startsWith('event:')) ev = l.slice(6).trim();
          else if (l.startsWith('data:')) data += l.slice(5).trim();
        });
        if (!data || data === '[DONE]') continue;
        try { cb(ev, JSON.parse(data)); } catch (_) { /* ignore malformed frame */ }
      }
    }
  }

  /* ---------- plans ---------- */
  // A plan card shows the steps and the buttons its state allows. The server owns the state: the card only asks
  // for it, and polls while the plan runs. Every action is an explicit click; nothing runs on its own.
  S.plans = S.plans || {};

  function renderPlanCard(id) {
    const p = S.plans[id];
    if (!p) return `<div class="ag-plan"><span class="ag-plan-hint">${esc(t('ag_plan_loading'))}</span></div>`;
    const steps = (list) => list.map((s) => `<li class="ag-plan-step ag-plan-${esc(s.status)}">
        <span class="badge badge-unknown">${esc(t('ag_plan_s_' + s.status))}</span> <span class="ag-plan-type">${esc(s.type)}</span>
        ${esc(s.description)}${s.target ? ` <span class="ag-plan-target">${esc(s.target)}</span>` : ''}
        ${s.result ? `<div class="ag-plan-result">${esc(s.result)}</div>` : ''}</li>`).join('');
    const btn = (action, label, cls) => `<button type="button" class="btn btn-sm ${cls}" onclick="AG.planAction('${esc(id)}', '${action}')">${esc(label)}</button>`;
    const buttons = [];
    if (p.state === 'proposed') buttons.push(btn('approve', t('ag_plan_approve'), 'btn-primary'), btn('cancel', t('ag_plan_reject'), ''));
    if (p.state === 'running' || p.state === 'rolling_back') buttons.push(btn('cancel', t('ag_plan_stop'), ''));
    if ((p.state === 'verify_failed' || p.state === 'failed') && (p.rollback_plan || []).length) buttons.push(btn('rollback', t('ag_plan_rollback'), 'btn-primary'));
    const rollback = (p.rollback_plan || []).length ? `<div class="ag-plan-sub">${esc(t('ag_plan_rollback_steps'))}</div><ul class="ag-plan-steps">${steps(p.rollback_plan)}</ul>` : '';
    return `<div class="ag-plan" data-plan-id="${esc(id)}">
      <div class="ag-plan-head"><span class="badge badge-info">${esc(t('ag_plan_title'))}</span> <span class="ag-plan-goal">${esc(p.goal)}</span></div>
      <div class="ag-plan-state">${esc(t('ag_plan_st_' + p.state))}${p.message ? ` — <span class="ag-plan-msg">${esc(p.message)}</span>` : ''}</div>
      ${p.state === 'proposed' ? `<div class="ag-plan-hint">${esc(t('ag_plan_hint'))}</div>` : ''}
      <ul class="ag-plan-steps">${steps(p.steps || [])}</ul>
      ${rollback}
      ${buttons.length ? `<div class="ag-plan-actions">${buttons.join('')}</div>` : ''}
    </div>`;
  }

  function refreshTerraformList() {
    const tf = window.Tf;
    if (tf && tf.load && typeof tf.load.workspaces === 'function') tf.load.workspaces().catch(() => {});
  }

  // Reload the open host detail in place: fetch the host again and redraw the tab that is showing.
  async function refreshOpenHost() {
    const id = window.detailData && window.detailData.host && window.detailData.host.id;
    if (!id) return;
    const res = await fetch('/api/hosts/' + id);
    if (!res.ok) return;
    window.detailData = await res.json();
    const tab = document.querySelector('#hostDetailTabs .tab.active');
    if (tab && typeof switchTab === 'function') switchTab(tab.getAttribute('data-tab'));
  }

  // Each agent action reloads the view it changed, through the loader that view already has.
  function refreshForTool(tool) {
    const name = String(tool || '');
    const run = (f) => { if (typeof f === 'function') { try { const p = f(); if (p && p.catch) p.catch(() => {}); } catch (_) {} } };
    if (/^(fleet_|alert_)/.test(name)) run(window.loadHosts);
    if (name.startsWith('tf_')) refreshTerraformList();
    if (/^(kube_|k8s_)/.test(name)) run(window.refreshKubeCurrentTab);
    if (name.startsWith('docker_')) { run(window.loadDockerFleetOverview); if (window.detailData) run(window.loadDockerTab); }
    if (/^(systemd_|pkg_|file_|firewall_|transfer_|domain_|host_|vpn_)/.test(name)) refreshOpenHost().catch(() => {});
  }

  async function refreshPlan(id) {
    const r = await api('/api/agent/plans/' + encodeURIComponent(id)).catch(() => null);
    if (!r || !r.ok || !r.data) return;
    const before = S.plans[id] && S.plans[id].state;
    S.plans[id] = r.data;
    // Plan steps run outside the chat stream: their views reload when the plan moves on from running.
    if (r.data.state !== 'running' && r.data.state !== before) (r.data.steps || []).filter((st) => st.type === 'mutate').forEach((st) => refreshForTool(st.tool));
    renderLog();
    if (r.data.state === 'running' || r.data.state === 'rolling_back') setTimeout(() => refreshPlan(id), 1500);
  }

  AG.planAction = async function (id, action) {
    const r = await post('/api/agent/plans/' + encodeURIComponent(id) + '/' + action, {}).catch(() => null);
    if (r && !r.ok) { S.msgs.push({ role: 'error', text: (r.data && r.data.error) || 'HTTP ' + r.status }); renderLog(); }
    refreshPlan(id);
  };

  /* ---------- approvals ---------- */
  async function pollApprovals() {
    if (!S.enabled) return;
    const r = await api('/api/agent/approvals').catch(() => null);
    if (!r || !r.ok) return;
    const prev = S.approvals.length;
    S.approvals = r.data || [];
    // The first pending approval starts the "waiting for your approval" clock.
    if (S.approvals.length && !prev) S.apprT0 = Date.now();
    const gr = await api('/api/agent/grants').catch(() => null);
    S.grants = gr && gr.ok ? gr.data || [] : [];
    const badge = $('agentBadge');
    badge.style.display = S.approvals.length ? 'flex' : 'none';
    badge.textContent = S.approvals.length;
    const cards = S.approvals.map((a) => `
      <div class="ag-appr ${a.gate >= 2 ? 'ag-appr-exec' : ''}">
        <div class="ag-appr-title">${svgIcon('triangle-alert', 'icon-sm')} ${esc(a.gate === 3 ? t('ag_appr_infra') : a.tool === 'exec_command' ? t('ag_appr_exec') : t('ag_appr_title'))} <span class="badge badge-unknown">${esc(a.host_name)}</span></div>
        <pre>${esc(a.summary)}</pre>
        <div class="ag-appr-btns">
          <button class="btn btn-sm" data-id="${esc(a.id)}" onclick="AG.decide(this.dataset.id, false)">${esc(t('ag_deny'))}</button>
          ${a.gate === 3 || !a.host_id ? '' : `<button class="btn btn-sm" data-id="${esc(a.id)}" title="${esc(fill(t('ag_session_hint'), { host: a.host_name, shell: a.gate === 2 ? '✓' : '✗' }))}" onclick="AG.decide(this.dataset.id, true, true)">${esc(t('ag_approve_session'))}</button>`}
          <button class="btn btn-sm btn-primary" data-id="${esc(a.id)}" onclick="AG.decide(this.dataset.id, true)">${esc(t('ag_approve'))}</button>
        </div>
      </div>`).join('');
    const host = $('agentApprovals');
    const html = grantsHtml() + cards; // grants stay visible even while the chat panel is closed
    if (host.dataset.html !== html) { host.dataset.html = html; host.innerHTML = html; applyLayout(); }
    // The chat bubble shows the approval wait, so it must follow the approval state.
    if (S.approvals.length !== prev) renderLog();
  }

  AG.decide = async function (id, approve, session) {
    await post('/api/agent/approvals/' + encodeURIComponent(id), { approve, session: !!session });
    pollApprovals();
  };

  AG.revoke = async function (hostId) {
    await api('/api/agent/grants' + (hostId ? '?host=' + Number(hostId) : ''), { method: 'DELETE' });
    pollApprovals();
  };

  function grantsHtml() {
    return S.grants.length === 0 ? '' : `<div class="ag-grants"><b>${esc(t('ag_grants'))}</b>` + S.grants.map((g) =>
      `<span class="ag-grant">${esc(g.host_name)} <i>(${esc(g.gate === 2 ? t('ag_g_shell') : t('ag_g_tools'))})</i> <a href="javascript:void(0)" onclick="AG.revoke(${Number(g.host_id)})">${esc(t('ag_revoke'))}</a></span>`).join('') + '</div>';
  }

  /* ---------- init ---------- */
  async function refreshEnabled() {
    const r = await api('/api/agent/settings').catch(() => null);
    S.enabled = !!(r && r.ok && r.data.enabled);
    document.querySelectorAll('.ag-ask-btn').forEach((b) => { b.style.display = S.enabled ? '' : 'none'; });
    if (S.timer) { clearInterval(S.timer); S.timer = null; }
    if (S.ctxTimer) { clearInterval(S.ctxTimer); S.ctxTimer = null; }
    if (S.enabled) {
      S.timer = setInterval(pollApprovals, 2500);
      S.ctxTimer = setInterval(syncContext, 1000);
    }
    if (!S.enabled) { AG.toggle(false); $('agentApprovals').innerHTML = ''; }
    S.ctx = detectContext();
    renderContextBar();
    renderLog();
    if (S.enabled) hydrate();
  }

  AG.init = function () { build(); refreshEnabled(); };
  AG.enabled = () => S.enabled;
  // index.html calls checkAuth() before this module loads, so a reload with a live session never reaches
  // AG.init() and the bubble would be missing until the next login. Boot here in that case.
  api('/api/me').then((r) => { if (r.ok && r.data.authenticated && !$('agentFab')) AG.init(); }).catch(() => {});
  if (typeof langRerenderHooks !== 'undefined') langRerenderHooks.push(() => { if ($('agentFab')) { document.getElementById('agentFab').remove(); $('agentPanel').remove(); $('agentApprovals').remove(); build(); refreshEnabled(); } });

  /* ---------- "Ask agent" entry points ---------- */
  const hostCtx = () => (typeof currentHost !== 'undefined' && currentHost ? { id: currentHost.id, host: currentHost.name } : null);
  AG.askUnit = function (unit) { const h = hostCtx(); if (h) AG.ask(fill(t('ag_q_unit'), { unit, host: h.host, id: h.id })); };
  AG.askContainer = function (name) { const h = hostCtx(); if (h) AG.ask(fill(t('ag_q_container'), { name, host: h.host, id: h.id })); };
  AG.askStack = function (stack) { const h = hostCtx(); if (h) AG.ask(fill(t('ag_q_stack'), { stack, host: h.host, id: h.id })); };
  AG.askSecurity = function (hostId) { const h = hostCtx(); AG.ask(fill(t('ag_q_security'), { host: h ? h.host : '', id: hostId })); };
  AG.askAlert = function (alertId) {
    const a = (typeof latestAlerts !== 'undefined' ? latestAlerts : []).find((x) => x.id === alertId);
    if (a) AG.ask(fill(t('ag_q_alert'), { host: a.host_name, id: a.host_id, msg: a.message, rc: a.root_cause || '-' }));
  };
  AG.askRemediation = function (info) {
    const d = info || {};
    AG.ask(fill(t('ag_q_remediate'), {
      source: d.source || 'DevSecOps',
      rule: d.rule || '-',
      severity: d.severity || '-',
      target: d.target || '-',
      title: d.title || '-',
      detail: d.detail || '-',
      remediation: d.remediation || '-'
    }), { dock: true });
  };
  AG.askAnsibleFix = function (info) {
    const d = info || {};
    AG.ask(fill(t('ag_q_ansible_fix'), {
      playbook: d.playbook || 'Playbook',
      run_id: d.run_id || 0,
      host: d.host || '-',
      status: d.status || 'failed',
      task: d.task || '-',
      error: (d.error || '-').slice(0, 2000)
    }), { dock: true });
  };
  AG.askTerraformExplain = function (info) {
    const d = info || {};
    const p = d.plan || {};
    const pol = d.policy || {};
    const cost = d.cost && d.cost.available ? ((d.cost.delta >= 0 ? '+' : '') + Number(d.cost.delta || 0).toFixed(2) + ' ' + (d.cost.currency || 'USD') + '/mo') : 'N/A';
    const polDesc = pol.status ? (pol.status + ((pol.results || []).length ? ' (' + pol.results.map((r) => r.name + ': ' + (r.passed ? 'pass' : 'fail')).join(', ') + ')' : '')) : 'none';
    AG.ask(fill(t('ag_q_tf_explain'), {
      workspace: d.workspace || 'Workspace',
      run_id: d.run_id || 0,
      status: d.status || '-',
      create: Number(p.create || 0),
      update: Number(p.update || 0),
      replace: Number(p.replace || 0),
      delete: Number(p.delete || 0),
      policy: polDesc,
      cost
    }), { dock: true });
  };

  /* ---------- Interactive Patch Diff (CodeMirror 5.65.16 local) & Dry-Run ---------- */
  const DS = { patch: null, before: '', after: '', baseSha: '', mode: 'split', dryRunOk: false, applied: false, cmBefore: null, cmAfter: null, cmUnified: null };

  function cmModeForPatch(p) {
    if (!p) return 'yaml';
    if (p.kind === 'k8s_yaml' || p.kind === 'ansible_playbook' || p.kind === 'docker_compose') return 'yaml';
    if (p.kind === 'terraform_hcl') return 'hcl';
    const tgt = String(p.target || '').toLowerCase();
    if (tgt.endsWith('.json')) return 'application/json';
    if (tgt.endsWith('.yml') || tgt.endsWith('.yaml')) return 'yaml';
    if (tgt.endsWith('.conf') || tgt.includes('nginx')) return 'nginx';
    if (tgt.endsWith('.sh')) return 'shell';
    if (tgt.endsWith('.py')) return 'python';
    if (tgt.endsWith('.toml')) return 'toml';
    return 'text/plain';
  }

  function computeLineOps(aText, bText) {
    const a = String(aText || '').split('\n');
    const b = String(bText || '').split('\n');
    const n = a.length, m = b.length;
    if (n * m > 250000) {
      return { ops: a.map((l) => ({ t: '-', l })).concat(b.map((l) => ({ t: '+', l }))), delLines: a.map((_, i) => i), addLines: b.map((_, i) => i) };
    }
    const dp = Array.from({ length: n + 1 }, () => new Int32Array(m + 1));
    for (let i = n - 1; i >= 0; i--) {
      for (let j = m - 1; j >= 0; j--) {
        dp[i][j] = a[i] === b[j] ? 1 + dp[i + 1][j + 1] : Math.max(dp[i + 1][j], dp[i][j + 1]);
      }
    }
    const ops = [], delLines = [], addLines = [];
    let i = 0, j = 0;
    while (i < n && j < m) {
      if (a[i] === b[j]) { ops.push({ t: ' ', l: a[i] }); i++; j++; }
      else if (dp[i + 1][j] >= dp[i][j + 1]) { delLines.push(i); ops.push({ t: '-', l: a[i++] }); }
      else { addLines.push(j); ops.push({ t: '+', l: b[j++] }); }
    }
    while (i < n) { delLines.push(i); ops.push({ t: '-', l: a[i++] }); }
    while (j < m) { addLines.push(j); ops.push({ t: '+', l: b[j++] }); }
    return { ops, delLines, addLines };
  }

  function buildUnifiedDiff(target, before, after) {
    const { ops } = computeLineOps(before, after);
    const hdr = `--- a/${target || 'current'}\n+++ b/${target || 'proposed'}\n@@ -1,${String(before || '').split('\n').length} +1,${String(after || '').split('\n').length} @@\n`;
    return hdr + ops.map((o) => o.t + o.l).join('\n');
  }

  function ensureDiffModal() {
    if ($('agentDiffModal')) return;
    const el = document.createElement('div');
    el.id = 'agentDiffModal';
    el.className = 'modal';
    el.innerHTML = `
      <div class="modal-content modal-workspace">
        <div class="modal-header">
          <div class="sd-title">${svgIcon('git-compare', 'icon-sm')} <h3 id="agDiffTitle"></h3></div>
          <button type="button" class="btn btn-icon btn-sm btn-ghost" onclick="closeModal('agentDiffModal')">${svgIcon('x')}</button>
        </div>
        <div class="modal-body" id="agDiffBody">
          <div class="ag-diff-toolbar">
            <span class="ag-diff-meta" id="agDiffMeta"></span>
            <div class="btn-group" role="group">
              <button type="button" class="btn btn-sm" id="agDiffModeSplit" onclick="AG.setDiffViewMode('split')">${esc(t('ag_patch_view_split'))}</button>
              <button type="button" class="btn btn-sm" id="agDiffModeUnified" onclick="AG.setDiffViewMode('unified')">${esc(t('ag_patch_view_unified'))}</button>
            </div>
            <button type="button" class="btn btn-sm" id="agDiffDryRunBtn" onclick="AG.runPatchDryRun()">${svgIcon('flask-conical', 'icon-sm')} <span>${esc(t('ag_patch_dryrun'))}</span></button>
            <button type="button" class="btn btn-sm btn-primary" id="agDiffApplyBtn" disabled title="${esc(t('ag_patch_need_dryrun'))}" onclick="AG.applyPatch()">${svgIcon('check', 'icon-sm')} <span>${esc(t('ag_patch_apply'))}</span></button>
          </div>
          <div id="agDiffEditors"></div>
          <div id="agDryRunBox" class="ag-dryrun-box" style="display:none;"></div>
        </div>
      </div>`;
    document.body.appendChild(el);
  }

  function renderDiffEditors() {
    const host = $('agDiffEditors');
    if (!host || !DS.patch) return;
    DS.cmBefore = null; DS.cmAfter = null; DS.cmUnified = null;
    const splitBtn = $('agDiffModeSplit'), uniBtn = $('agDiffModeUnified');
    if (splitBtn) splitBtn.classList.toggle('btn-primary', DS.mode === 'split');
    if (uniBtn) uniBtn.classList.toggle('btn-primary', DS.mode === 'unified');

    const theme = 'nord';
    if (DS.mode === 'unified') {
      host.innerHTML = `<div class="ag-diff-unified"><textarea id="agDiffUnifiedTa"></textarea></div>`;
      if (typeof CodeMirror !== 'undefined') {
        const cm = CodeMirror.fromTextArea($('agDiffUnifiedTa'), {
          value: buildUnifiedDiff(DS.patch.target || DS.patch.stack_name, DS.before, DS.after),
          mode: 'diff', theme, readOnly: true, lineNumbers: true, lineWrapping: true
        });
        cm.setValue(buildUnifiedDiff(DS.patch.target || DS.patch.stack_name, DS.before, DS.after));
        DS.cmUnified = cm;
        setTimeout(() => cm.refresh(), 20);
      }
      return;
    }

    host.innerHTML = `
      <div class="ag-diff-split">
        <div class="ag-diff-pane"><div class="ag-diff-pane-hdr">${esc(t('ag_patch_before'))}</div><textarea id="agDiffBeforeTa"></textarea></div>
        <div class="ag-diff-pane"><div class="ag-diff-pane-hdr">${esc(t('ag_patch_after'))}</div><textarea id="agDiffAfterTa"></textarea></div>
      </div>`;
    if (typeof CodeMirror !== 'undefined') {
      const mode = cmModeForPatch(DS.patch);
      const cmB = CodeMirror.fromTextArea($('agDiffBeforeTa'), { mode, theme, readOnly: true, lineNumbers: true, lineWrapping: true });
      const cmA = CodeMirror.fromTextArea($('agDiffAfterTa'), { mode, theme, readOnly: false, lineNumbers: true, lineWrapping: true });
      cmB.setValue(DS.before || '');
      cmA.setValue(DS.after || '');
      const paintHighlights = () => {
        const { delLines, addLines } = computeLineOps(cmB.getValue(), cmA.getValue());
        for (let i = 0; i < cmB.lineCount(); i++) cmB.removeLineClass(i, 'background', 'ag-diff-line-del');
        for (let i = 0; i < cmA.lineCount(); i++) cmA.removeLineClass(i, 'background', 'ag-diff-line-add');
        delLines.forEach((ln) => cmB.addLineClass(ln, 'background', 'ag-diff-line-del'));
        addLines.forEach((ln) => cmA.addLineClass(ln, 'background', 'ag-diff-line-add'));
      };
      paintHighlights();
      let syncing = false;
      cmB.on('scroll', () => { if (syncing) return; syncing = true; cmA.scrollTo(cmB.getScrollInfo().left, cmB.getScrollInfo().top); syncing = false; });
      cmA.on('scroll', () => { if (syncing) return; syncing = true; cmB.scrollTo(cmA.getScrollInfo().left, cmA.getScrollInfo().top); syncing = false; });
      cmA.on('change', () => {
        DS.after = cmA.getValue();
        DS.dryRunOk = false;
        const applyBtn = $('agDiffApplyBtn');
        if (applyBtn) { applyBtn.disabled = true; applyBtn.title = t('ag_patch_need_dryrun'); }
        paintHighlights();
      });
      DS.cmBefore = cmB; DS.cmAfter = cmA;
      setTimeout(() => { cmB.refresh(); cmA.refresh(); }, 20);
    }
  }

  AG.setDiffViewMode = function (mode) {
    DS.mode = mode === 'unified' ? 'unified' : 'split';
    renderDiffEditors();
  };

  AG.openPatchDiff = async function (patchId) {
    const p = S.patches && S.patches[patchId];
    if (!p) return;
    ensureDiffModal();
    DS.patch = Object.assign({}, p);
    DS.before = p.before || '';
    DS.after = p.after || '';
    DS.dryRunOk = false;
    DS.applied = false;
    DS.baseSha = ''; // fail closed: a failed prepare leaves no base, so dry-run and apply are refused
    $('agDiffTitle').textContent = (PATCH_KIND_LABEL[p.kind] || p.kind) + ' — ' + (p.title || p.target || p.stack_name || '');
    $('agDiffMeta').textContent = p.target || p.stack_name || '';
    const box = $('agDryRunBox');
    if (box) { box.style.display = 'none'; box.className = 'ag-dryrun-box'; box.innerHTML = ''; }
    const applyBtn = $('agDiffApplyBtn');
    if (applyBtn) { applyBtn.disabled = true; applyBtn.title = t('ag_patch_need_dryrun'); applyBtn.innerHTML = `${svgIcon('check', 'icon-sm')} <span>${esc(t('ag_patch_apply'))}</span>`; }
    openModal('agentDiffModal');
    renderDiffEditors();
    // Always re-read the live base: the proposal's "before" can be stale, and apply checks base_sha256 against the host.
    const r = await post('/api/agent/patch/prepare', p).catch(() => null);
    if (r && r.ok && r.data) {
      if (typeof r.data.before === 'string' && r.data.before !== DS.before) {
        DS.before = r.data.before;
        renderDiffEditors();
      }
      DS.baseSha = r.data.base_sha256 || '';
    }
  };

  AG.runPatchDryRun = async function () {
    if (!DS.patch) return;
    const btn = $('agDiffDryRunBtn'), applyBtn = $('agDiffApplyBtn'), box = $('agDryRunBox');
    const restore = typeof SP !== 'undefined' && SP.busy ? SP.busy(btn, t('ag_patch_dryrunning')) : () => {};
    if (box) { box.style.display = 'block'; box.className = 'ag-dryrun-box'; box.innerHTML = esc(t('ag_patch_dryrunning')); }
    try {
      const payload = Object.assign({}, DS.patch, { after: DS.after, base_sha256: DS.baseSha });
      const r = await post('/api/agent/patch/dry-run', payload);
      const ok = !!(r && r.ok && r.data && r.data.valid);
      DS.dryRunOk = ok;
      if (applyBtn) {
        applyBtn.disabled = !ok || DS.applied;
        applyBtn.title = ok ? '' : t('ag_patch_need_dryrun');
      }
      if (box) {
        const d = (r && r.data) || {};
        box.className = 'ag-dryrun-box ' + (ok ? 'is-ok' : 'is-err');
        box.innerHTML = `<div><b>${esc(ok ? t('ag_patch_valid_ok') : t('ag_patch_valid_fail'))}</b>${d.command ? ` <span class="badge">${esc(d.command)}</span>` : ''}</div>` +
          (d.error ? `<div style="color:var(--danger);margin-top:4px;">${esc(d.error)}</div>` : '') +
          (d.output ? `<pre class="ag-dryrun-out">${esc(d.output)}</pre>` : '');
      }
    } finally { restore(); }
  };

  AG.applyPatch = async function () {
    if (!DS.patch || !DS.dryRunOk || DS.applied) return;
    const btn = $('agDiffApplyBtn'), box = $('agDryRunBox');
    const restore = typeof SP !== 'undefined' && SP.busy ? SP.busy(btn, t('ag_patch_applying')) : () => {};
    try {
      const payload = Object.assign({}, DS.patch, { after: DS.after, base_sha256: DS.baseSha });
      const r = await post('/api/agent/patch/apply', payload);
      if (!r || !r.ok) {
        showToast((r && r.data && r.data.error) || 'error', 'error');
        return;
      }
      DS.applied = true;
      if (btn) { btn.disabled = true; btn.innerHTML = `${svgIcon('check', 'icon-sm')} <span>${esc(t('ag_patch_applied'))}</span>`; }
      if (box) {
        box.style.display = 'block';
        box.className = 'ag-dryrun-box is-ok';
        box.innerHTML = `<div><b>${esc(t('ag_patch_apply_ok'))}</b></div>` + (r.data && r.data.output ? `<pre class="ag-dryrun-out">${esc(r.data.output)}</pre>` : '');
      }
      showToast(t('ag_patch_apply_ok'), 'success');
    } finally { restore(); }
  };
  /** HTML of an "Ask agent" button; hidden until the agent is enabled. onclick is a JS expression using data-arg. */
  AG.btn = function (call, arg, cls) {
    return `<button class="btn btn-sm ${cls || 'btn-ghost'} ag-ask-btn" style="${S.enabled ? '' : 'display:none'}" data-arg="${esc(arg)}" onclick="AG.${call}(this.dataset.arg)">${svgIcon('bot', 'icon-sm')} <span>${esc(t('ag_ask'))}</span></button>`;
  };
  AG.btnNum = function (call, arg, cls) {
    return `<button class="btn btn-sm ${cls || 'btn-ghost'} ag-ask-btn" style="${S.enabled ? '' : 'display:none'}" onclick="AG.${call}(${Number(arg)})">${svgIcon('bot', 'icon-sm')} <span>${esc(t('ag_ask'))}</span></button>`;
  };

  /* ---------- Settings → AI Agent ---------- */
  AG.loadSettings = async function () {
    const body = $('agentSettingsBody');
    if (!body) return;
    const r = await api('/api/agent/settings');
    if (!r.ok) { body.innerHTML = `<div class="ag-err">HTTP ${r.status}</div>`; return; }
    const cfg = r.data;
    const provider = cfg.provider || '';
    const fastProvider = cfg.fast_provider || '';
    body.innerHTML = `
      <div class="set-card">
        <div class="set-header"><div><h4 class="set-title">${svgIcon('bot')} <span>${esc(t('ag_title'))}</span></h4></div></div>
        <label style="display:flex;gap:8px;align-items:center;cursor:pointer;margin-bottom:12px;"><input type="checkbox" id="agEnabled" style="width:auto" ${cfg.enabled ? 'checked' : ''}> <b>${esc(t('ag_s_enable'))}</b></label>
        <div style="margin:12px 0 8px;padding-top:10px;border-top:1px solid var(--card-border);">
          <div style="font-weight:600;font-size:var(--fs-sm);margin-bottom:2px;">${esc(t('ag_s_reasoning_title'))}</div>
          <div class="form-hint" style="margin-bottom:8px;">${esc(t('ag_s_reasoning_hint'))}</div>
        </div>
        <div class="form-group"><label class="form-label" for="agProvider">${esc(t('ag_s_provider'))}</label>
          <select id="agProvider" onchange="AG.providerChanged()">
            <option value="" ${provider === '' ? 'selected' : ''}>${esc(t('ag_s_provider_none'))}</option>
            <option value="anthropic" ${provider === 'anthropic' ? 'selected' : ''}>${esc(t('ag_s_provider_anthropic'))}</option>
            <option value="openai" ${provider === 'openai' ? 'selected' : ''}>${esc(t('ag_s_provider_openai'))}</option>
          </select></div>
        <div class="form-group"><label class="form-label" for="agBaseUrl">${esc(t('ag_s_base_url'))}</label>
          <input type="text" id="agBaseUrl" value="${esc(cfg.base_url || '')}" placeholder="https://api.openai.com/v1">
          <span class="form-hint">${esc(t('ag_s_base_url_hint'))}</span></div>
        <div class="form-group"><label class="form-label" for="agKey">${esc(t('ag_s_key'))}</label>
          <input type="password" id="agKey" placeholder="••••••••" autocomplete="new-password">
          ${cfg.api_key_set ? `<span class="badge badge-info" id="agKeySaved" style="margin-top:4px;">${esc(t('ag_s_key_saved'))}</span>` : ''}
          <span class="form-hint">${esc(t('ag_s_key_hint'))}</span></div>
        <div class="form-group"><label class="form-label" for="agModel">${esc(t('ag_s_model'))}</label>
          <input type="text" id="agModel" value="${esc(cfg.model || '')}" placeholder="gpt-4o">
          <span class="form-hint">${esc(t('ag_s_model_hint'))}</span></div>
        <div style="margin:14px 0 8px;padding-top:10px;border-top:1px solid var(--card-border);">
          <div style="font-weight:600;font-size:var(--fs-sm);margin-bottom:2px;">${esc(t('ag_s_fast_title'))}</div>
          <div class="form-hint" style="margin-bottom:8px;">${esc(t('ag_s_fast_hint'))}</div>
        </div>
        <div class="form-group"><label class="form-label" for="agFastProvider">${esc(t('ag_s_fast_provider'))}</label>
          <select id="agFastProvider" onchange="AG.fastProviderChanged()">
            <option value="" ${fastProvider === '' ? 'selected' : ''}>${esc(t('ag_s_fast_provider_same'))}</option>
            <option value="anthropic" ${fastProvider === 'anthropic' ? 'selected' : ''}>${esc(t('ag_s_provider_anthropic'))}</option>
            <option value="openai" ${fastProvider === 'openai' ? 'selected' : ''}>${esc(t('ag_s_provider_openai'))}</option>
          </select></div>
        <div class="form-group"><label class="form-label" for="agFastBaseUrl">${esc(t('ag_s_fast_base_url'))}</label>
          <input type="text" id="agFastBaseUrl" value="${esc(cfg.fast_base_url || '')}" placeholder="http://localhost:11434/v1">
          <span class="form-hint">${esc(t('ag_s_fast_base_url_hint'))}</span></div>
        <div class="form-group"><label class="form-label" for="agFastKey">${esc(t('ag_s_fast_key'))}</label>
          <input type="password" id="agFastKey" placeholder="••••••••" autocomplete="new-password">
          ${cfg.fast_api_key_set ? `<span class="badge badge-info" id="agFastKeySaved" style="margin-top:4px;">${esc(t('ag_s_key_saved'))}</span>` : ''}
          <span class="form-hint">${esc(t('ag_s_fast_key_hint'))}</span></div>
        <div class="form-group"><label class="form-label" for="agFastModel">${esc(t('ag_s_fast_model'))}</label>
          <input type="text" id="agFastModel" value="${esc(cfg.fast_model || '')}" placeholder="${esc(cfg.model || 'qwen2.5:7b')}">
          <span class="form-hint">${esc(t('ag_s_fast_model_hint'))}</span></div>
        <div class="form-group"><label class="form-label">${esc(t('ag_s_triage_enable', 'Proactive AI Auto-Triage'))}</label>
          <label style="display:flex;gap:8px;cursor:pointer;font-size:var(--fs-sm);"><input type="checkbox" id="agTriageEnabled" style="width:auto" ${cfg.triage_enabled ? 'checked' : ''}> ${esc(t('ag_s_triage_hint', 'Diagnose obvious failures locally (0 tokens) and use one short LLM call for the rest'))}</label></div>
        <div class="form-group"><label class="form-label" for="agTriageModel">${esc(t('ag_s_triage_model', 'Lightweight triage model (optional)'))}</label>
          <input type="text" id="agTriageModel" value="${esc(cfg.triage_model || '')}" placeholder="${esc(cfg.model || 'uses primary model')}">
          <span class="form-hint">${esc(t('ag_s_triage_model_hint', 'Empty uses the primary agent model'))}</span></div>
        <div class="form-group"><label class="form-label" for="agTriageBudget">${esc(t('ag_s_triage_budget', 'Daily triage token budget'))}</label>
          <input type="number" id="agTriageBudget" min="0" value="${Number(cfg.triage_daily_budget) || 0}">
          <span class="form-hint">${esc(t('ag_s_triage_budget_hint', 'Global cap across all auto-triage calls per day'))}</span></div>
        <div class="form-group"><label class="form-label" for="agBriefingTime">${esc(t('ag_s_briefing_time', 'Daily briefing time (HH:MM, optional)'))}</label>
          <input type="text" id="agBriefingTime" value="${esc(cfg.briefing_time || '')}" placeholder="08:00">
          <span class="form-hint">${esc(t('ag_s_briefing_hint', 'Delta-only briefing of the last 24 hours; empty disables the schedule'))}</span></div>
        <div class="form-group"><label class="form-label">${esc(t('ag_s_approval'))}</label>
          <label style="display:flex;gap:8px;cursor:pointer;font-size:var(--fs-sm);"><input type="radio" name="agAppr" value="ask" style="width:auto" ${cfg.approval !== 'auto' ? 'checked' : ''}> ${esc(t('ag_s_ask'))}</label>
          <label style="display:flex;gap:8px;cursor:pointer;font-size:var(--fs-sm);margin-top:4px;"><input type="radio" name="agAppr" value="auto" style="width:auto" ${cfg.approval === 'auto' ? 'checked' : ''}> ${esc(t('ag_s_auto'))}</label></div>
        <div class="form-group"><label class="form-label">${esc(t('ag_s_soul'))}</label><textarea class="ag-soul" id="agSoul">${esc(cfg.soul)}</textarea>
          <div><button class="btn btn-sm btn-ghost" onclick="AG.resetSoul()">${esc(t('ag_s_soul_reset'))}</button></div></div>
        <div style="display:flex;gap:8px;margin-top:8px;flex-wrap:wrap;">
          <button class="btn btn-primary btn-sm" id="agSave" onclick="AG.saveSettings()">${svgIcon('save', 'icon-sm')} <span>${esc(t('ag_s_save'))}</span></button>
          <button class="btn btn-ghost btn-sm" id="agTest" onclick="AG.testConn()">${esc(t('ag_s_test'))}</button>
          <button class="btn btn-ghost btn-sm" id="agTestFast" onclick="AG.testConn('fast')">${esc(t('ag_s_test_fast'))}</button>
        </div>
        <div id="agTestResult" style="margin-top:8px;font-size:var(--fs-xs);"></div>
      </div>
      <div id="agMemory"></div>`;
    AG.providerChanged();
    AG.fastProviderChanged();
    AG.loadMemory();
  };

  /** The placeholders show the provider default; an empty base URL means the default. */
  AG.providerChanged = function () {
    const p = ($('agProvider') || {}).value || '';
    const url = $('agBaseUrl');
    const model = $('agModel');
    if (url) url.placeholder = p === 'anthropic' ? 'https://api.anthropic.com/v1' : p === 'openai' ? 'https://api.openai.com/v1' : '';
    if (model) model.placeholder = p === 'anthropic' ? 'claude-sonnet-4-6' : p === 'openai' ? 'gpt-4o' : '';
  };

  AG.fastProviderChanged = function () {
    const fp = ($('agFastProvider') || {}).value || ($('agProvider') || {}).value || '';
    const url = $('agFastBaseUrl');
    const model = $('agFastModel');
    if (url) url.placeholder = fp === 'anthropic' ? 'https://api.anthropic.com/v1' : fp === 'openai' ? 'http://localhost:11434/v1' : '';
    if (model) model.placeholder = fp === 'anthropic' ? 'claude-haiku-4-5' : fp === 'openai' ? 'qwen2.5:7b' : ($('agModel') || {}).value || '';
  };

  AG.resetSoul = async function () {
    await post('/api/agent/settings', { soul: '' });
    AG.loadSettings();
  };

  AG.saveSettings = async function () {
    const payload = {
      enabled: $('agEnabled').checked,
      provider: $('agProvider').value,
      base_url: $('agBaseUrl').value.trim(),
      model: $('agModel').value.trim(),
      fast_provider: $('agFastProvider').value,
      fast_base_url: $('agFastBaseUrl').value.trim(),
      fast_model: $('agFastModel').value.trim(),
      approval: (document.querySelector('input[name="agAppr"]:checked') || {}).value || 'ask',
      soul: $('agSoul').value,
      triage_enabled: $('agTriageEnabled').checked,
      triage_model: $('agTriageModel').value.trim(),
      triage_daily_budget: Number($('agTriageBudget').value) || 0,
      briefing_time: $('agBriefingTime').value.trim()
    };
    const key = $('agKey').value;
    if (key !== '') payload.api_key = key; // empty keeps the stored key
    const fastKey = $('agFastKey').value;
    if (fastKey !== '') payload.fast_api_key = fastKey; // empty keeps the stored key
    const r = await post('/api/agent/settings', payload);
    if (r.ok) { showToast(t('ag_s_saved'), 'success'); await refreshEnabled(); await AG.loadSettings(); return ''; }
    showToast(r.data.error || 'error', 'error');
    return r.data.error || 'error';
  };

  AG.testConn = async function (target) {
    const before = $('agTestResult');
    if (before) before.innerHTML = SP.html(t('ag_s_testing'));
    const err = await AG.saveSettings();
    const out = $('agTestResult'); // saving re-renders the panel
    if (!out) return;
    if (err) { out.innerHTML = `<span style="color:var(--danger)">${esc(err)}</span>`; return; }
    let r;
    try {
      r = await post('/api/agent/test', target ? { target } : {});
    } catch (e) {
      out.innerHTML = `<span style="color:var(--danger)">${esc(e.message)}</span>`;
      return;
    }
    out.innerHTML = r.data.ok ? `<span style="color:var(--success)">${esc(t('ag_s_test_ok'))}</span>` : `<span style="color:var(--danger)">${esc(r.data.error || 'error')}</span>`;
  };


  /* ---------- Settings → AI Agent → Memory ---------- */
  AG.loadMemory = async function () {
    const box = $('agMemory');
    if (!box) return;
    const r = await api('/api/agent/memory');
    if (!r.ok) { box.innerHTML = `<div class="ag-err">HTTP ${r.status}</div>`; return; }
    const hosts = typeof allRawHosts !== 'undefined' ? allRawHosts : [];
    const name = (id) => (hosts.find((h) => h.id === id) || {}).name || '#' + id;
    const notes = r.data.notes || [];
    const rows = notes.map((n) => `<div style="display:flex;gap:8px;align-items:flex-start;padding:6px 0;border-top:1px solid var(--card-border);">
        <span class="badge ${n.host_id || n.cluster_id || n.domain ? 'badge-info' : ''}" style="flex:none;">${esc(n.host_id ? name(n.host_id) : n.cluster_id ? '#' + n.cluster_id : n.domain || t('ag_m_fleet'))}</span>
        <span style="flex:1;font-size:var(--fs-sm);white-space:pre-wrap;word-break:break-word;">${esc(n.content)}</span>
        <span style="flex:none;font-size:var(--fs-xs);color:var(--text-muted);">${esc(n.source === 'agent' ? t('ag_m_by_agent') : t('ag_m_by_you'))}</span>
        <button class="btn btn-sm btn-icon btn-ghost" aria-label="${esc(t('ag_m_forget'))}" title="${esc(t('ag_m_forget'))}" onclick="AG.delMemory(${n.id})">${svgIcon('trash-2', 'icon-sm')}</button></div>`).join('');
    // The clusters are only needed for the scope picker, so they are fetched once per panel load.
    const kc = await api('/api/kube/clusters');
    const clusters = kc.ok ? (kc.data.clusters || []) : [];
    const domains = ['security', 'network', 'docker', 'ansible', 'terraform', 'kubernetes', 'database', 'storage'];
    box.innerHTML = `
      <div class="set-card">
        <div class="set-header"><div><h4 class="set-title">${svgIcon('bookmark')} <span>${esc(t('ag_m_title'))} (${notes.length}/${r.data.max})</span></h4>
          <p class="set-desc">${esc(t('ag_m_desc'))}</p></div></div>
        ${rows || `<div style="font-size:var(--fs-sm);color:var(--text-muted);">${esc(t('ag_m_empty'))}</div>`}
        <div style="display:flex;gap:8px;margin-top:10px;flex-wrap:wrap;">
          <select id="agMemScope" style="width:auto;max-width:170px;" onchange="AG.memScopeChanged()">
            <option value="fleet">${esc(t('ag_m_scope_fleet'))}</option>
            <option value="host">${esc(t('ag_m_scope_host'))}</option>
            <option value="cluster">${esc(t('ag_m_scope_cluster'))}</option>
            <option value="domain">${esc(t('ag_m_scope_domain'))}</option>
          </select>
          <select id="agMemHost" style="width:auto;max-width:160px;display:none;">${hosts.map((h) => `<option value="${h.id}">${esc(h.name)}</option>`).join('')}</select>
          <select id="agMemCluster" style="width:auto;max-width:160px;display:none;">${clusters.map((c) => `<option value="${c.id}">${esc(c.name)}</option>`).join('')}</select>
          <select id="agMemDomain" style="width:auto;max-width:160px;display:none;">${domains.map((d) => `<option value="${d}">${esc(d)}</option>`).join('')}</select>
          <input type="text" id="agMemText" maxlength="${r.data.max_len}" placeholder="${esc(t('ag_m_ph'))}" style="flex:1;min-width:200px;" onkeydown="if(event.key==='Enter'){event.preventDefault();AG.addMemory();}">
          <button class="btn btn-primary btn-sm" id="agMemAdd" onclick="AG.addMemory()">${svgIcon('plus', 'icon-sm')} <span>${esc(t('ag_m_add'))}</span></button>
        </div>
      </div>`;
  };

  // Only the picker of the chosen scope is shown: one note carries exactly one scope.
  AG.memScopeChanged = function () {
    const kind = $('agMemScope').value;
    for (const [id, on] of [['agMemHost', 'host'], ['agMemCluster', 'cluster'], ['agMemDomain', 'domain']]) {
      const el = $(id);
      if (el) el.style.display = kind === on ? '' : 'none';
    }
  };

  AG.addMemory = async function () {
    const text = $('agMemText').value.trim();
    if (!text) return;
    const kind = $('agMemScope').value;
    const body = { content: text };
    if (kind === 'host') body.host_id = Number($('agMemHost').value);
    else if (kind === 'cluster') body.cluster_id = Number($('agMemCluster').value);
    else if (kind === 'domain') body.domain = $('agMemDomain').value;
    const r = await post('/api/agent/memory', body);
    if (r.ok) AG.loadMemory();
    else showToast(r.data.error || 'error', 'error');
  };

  AG.delMemory = async function (id) {
    if (!(await showConfirm({ message: t('ag_m_forget_confirm') }))) return;
    const r = await api('/api/agent/memory?id=' + id, { method: 'DELETE' });
    if (r.ok) AG.loadMemory();
    else showToast(r.data.error || 'error', 'error');
  };
  /* ---------- Procedural memory: distill a thread, Knowledge & Skills settings ---------- */
  const KNOWLEDGE_DOMAINS = ['global', 'host', 'kubernetes', 'docker', 'ansible', 'terraform', 'security'];
  S.skills = S.skills || [];

  // Distill: the fast model drafts a runbook from this chat; the operator reviews the draft before anything is saved.
  AG.distill = async function () {
    if (!S.thread) { showToast(t('ag_k_need_thread'), 'error'); return; }
    showToast(t('ag_k_distilling'), 'info');
    const r = await post('/api/agent/threads/' + S.thread + '/distill', {});
    if (!r.ok) { showToast((r.data && r.data.error) || 'HTTP ' + r.status, 'error'); return; }
    openDistillReview(r.data.draft);
  };

  function domainOptions(selected) {
    return KNOWLEDGE_DOMAINS.map((d) => `<option value="${d}" ${d === selected ? 'selected' : ''}>${esc(d)}</option>`).join('');
  }

  function openDistillReview(d) {
    const ov = document.createElement('div');
    ov.className = 'ag-modal-backdrop';
    ov.innerHTML = `<div class="ag-modal" role="dialog" aria-modal="true" aria-label="${esc(t('ag_k_review_title'))}">
      <h4>${esc(t('ag_k_review_title'))}</h4>
      <div class="form-hint">${esc(t('ag_k_review_hint'))}</div>
      <div class="form-group"><label class="form-label" for="agDTitle">${esc(t('ag_k_f_title'))}</label><input id="agDTitle" value="${esc(d.title || '')}"></div>
      <div class="form-group"><label class="form-label" for="agDKind">${esc(t('ag_k_f_kind'))}</label>
        <select id="agDKind"><option value="runbook" ${d.kind !== 'blueprint' ? 'selected' : ''}>runbook</option><option value="blueprint" ${d.kind === 'blueprint' ? 'selected' : ''}>blueprint</option></select></div>
      <div class="form-group"><label class="form-label" for="agDDomain">${esc(t('ag_k_f_domain'))}</label><select id="agDDomain">${domainOptions(d.domain || 'global')}</select></div>
      <div class="form-group"><label class="form-label" for="agDTrigger">${esc(t('ag_k_f_trigger'))}</label><input id="agDTrigger" value="${esc(d.trigger_summary || '')}"></div>
      <div class="form-group"><label class="form-label" for="agDSteps">${esc(t('ag_k_f_steps'))}</label><textarea id="agDSteps" rows="4">${esc((d.steps || []).join('\n'))}</textarea></div>
      <div class="form-group"><label class="form-label" for="agDVerify">${esc(t('ag_k_f_verify'))}</label><textarea id="agDVerify" rows="2">${esc((d.verify || []).join('\n'))}</textarea></div>
      <div class="form-group"><label class="form-label" for="agDRollback">${esc(t('ag_k_f_rollback'))}</label><textarea id="agDRollback" rows="2">${esc((d.rollback || []).join('\n'))}</textarea></div>
      <div class="form-group"><label class="form-label" for="agDTags">${esc(t('ag_k_f_tags'))}</label><input id="agDTags" value="${esc((d.tags || []).join(', '))}"></div>
      <div class="ag-modal-actions">
        <button class="btn btn-sm btn-ghost" id="agDCancel" type="button">${esc(t('ag_k_cancel'))}</button>
        <button class="btn btn-sm btn-primary" id="agDSave" type="button">${esc(t('ag_k_save'))}</button>
      </div></div>`;
    document.body.appendChild(ov);
    const lines = (id) => $(id).value.split('\n').map((x) => x.trim()).filter(Boolean);
    ov.querySelector('#agDCancel').onclick = () => ov.remove();
    ov.querySelector('#agDSave').onclick = async () => {
      const body = {
        kind: $('agDKind').value, title: $('agDTitle').value, domain: $('agDDomain').value,
        trigger_summary: $('agDTrigger').value, steps: lines('agDSteps'), verify: lines('agDVerify'), rollback: lines('agDRollback'),
        tags: $('agDTags').value.split(',').map((x) => x.trim()).filter(Boolean), source_thread_id: d.source_thread_id || 0,
      };
      const r = await post('/api/agent/knowledge', body);
      if (r.ok) { ov.remove(); showToast(t('ag_k_saved'), 'success'); }
      else showToast((r.data && r.data.error) || 'HTTP ' + r.status, 'error');
    };
  }

  function runbookRow(rb) {
    return `<div class="ag-k-row"><b>${esc(rb.title)}</b> <span class="badge badge-unknown">${esc(rb.kind)}</span> <span class="badge badge-unknown">${esc(rb.domain)}</span>
      <span class="ag-k-meta">${esc(t('ag_k_used'))} ${Number(rb.usage_count) || 0}</span>
      <button class="btn btn-sm btn-ghost" onclick="AG.deleteRunbook(${Number(rb.id)})">${esc(t('ag_k_delete'))}</button></div>`;
  }

  function skillRow(sk) {
    return `<div class="ag-k-row"><b>${esc(sk.name)}</b> <span class="badge badge-unknown">${esc(sk.domain)}</span>
      <label class="ag-k-meta"><input type="checkbox" ${sk.enabled ? 'checked' : ''} onchange="AG.toggleSkill(${Number(sk.id)}, this.checked)"> ${esc(t('ag_k_enabled'))}</label>
      <button class="btn btn-sm btn-ghost" onclick="AG.deleteSkill(${Number(sk.id)})">${esc(t('ag_k_delete'))}</button></div>`;
  }

  async function renderKnowledgeCard() {
    const body = $('agentSettingsBody');
    if (!body) return;
    let card = $('agKnowledgeCard');
    if (!card) { card = document.createElement('div'); card.id = 'agKnowledgeCard'; body.appendChild(card); }
    const [rb, sk] = await Promise.all([api('/api/agent/knowledge'), api('/api/agent/skills/custom')]);
    const runbooks = rb.ok ? (rb.data.runbooks || []) : [];
    S.skills = sk.ok ? (sk.data.skills || []) : [];
    card.innerHTML = `<div class="set-card">
      <div class="set-header"><div><h4 class="set-title">${svgIcon('bot')} <span>${esc(t('ag_k_title'))}</span></h4></div></div>
      <div class="form-hint" style="margin-bottom:8px;">${esc(t('ag_k_hint'))}</div>
      <div class="form-group"><label class="form-label" for="agKSearch">${esc(t('ag_k_search'))}</label>
        <input id="agKSearch" type="search" oninput="AG.knowledgeSearch(this.value)"></div>
      <div id="agKList">${runbooks.map(runbookRow).join('') || `<div class="form-hint">${esc(t('ag_k_none'))}</div>`}</div>
      <div style="margin:14px 0 6px;font-weight:600;">${esc(t('ag_k_skills'))}</div>
      <div id="agSkillList">${S.skills.map(skillRow).join('') || `<div class="form-hint">${esc(t('ag_k_no_skills'))}</div>`}</div>
      <div class="ag-k-form">
        <input id="agSkName" placeholder="${esc(t('ag_k_skill_name'))}">
        <select id="agSkDomain">${domainOptions('docker')}</select>
        <textarea id="agSkContent" rows="3" placeholder="${esc(t('ag_k_skill_content'))}"></textarea>
        <label class="ag-k-meta"><input type="checkbox" id="agSkEnabled" checked> ${esc(t('ag_k_enabled'))}</label>
        <button class="btn btn-sm btn-primary" type="button" onclick="AG.addSkill()">${esc(t('ag_k_add_skill'))}</button>
      </div></div>`;
  }

  const loadSettingsBase = AG.loadSettings;
  AG.loadSettings = async function () {
    await loadSettingsBase();
    await renderKnowledgeCard();
  };

  AG.knowledgeSearch = async function (q) {
    const list = $('agKList');
    if (!list) return;
    q = (q || '').trim();
    if (!q) { renderKnowledgeCard(); return; }
    const r = await api('/api/agent/knowledge?q=' + encodeURIComponent(q));
    const hits = r.ok ? (r.data.hits || []) : [];
    list.innerHTML = hits.map((h) => `<div class="ag-k-row"><b>${esc(h.title)}</b> <span class="badge badge-unknown">${esc(h.kind)}</span>
      <span class="ag-k-meta">${esc(h.snippet || '')}</span>${h.kind === 'runbook' ? `<button class="btn btn-sm btn-ghost" onclick="AG.deleteRunbook(${Number(h.id)})">${esc(t('ag_k_delete'))}</button>` : ''}</div>`).join('')
      || `<div class="form-hint">${esc(t('ag_k_none'))}</div>`;
  };

  AG.deleteRunbook = async function (id) {
    if (!confirm(t('ag_k_confirm_delete'))) return;
    const r = await api('/api/agent/knowledge/runbooks/' + id, { method: 'DELETE' });
    if (!r.ok) showToast((r.data && r.data.error) || 'HTTP ' + r.status, 'error');
    renderKnowledgeCard();
  };

  AG.addSkill = async function () {
    const name = $('agSkName').value.trim();
    if (!name) { showToast(t('ag_k_skill_name'), 'error'); return; }
    const r = await post('/api/agent/skills/custom', { name, domain: $('agSkDomain').value, enabled: $('agSkEnabled').checked, content_md: $('agSkContent').value });
    if (!r.ok) { showToast((r.data && r.data.error) || 'HTTP ' + r.status, 'error'); return; }
    renderKnowledgeCard();
  };

  AG.toggleSkill = async function (id, enabled) {
    const sk = S.skills.find((x) => x.id === id);
    if (!sk) return;
    const r = await api('/api/agent/skills/custom/' + id, { method: 'PUT', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: sk.name, domain: sk.domain, enabled, content_md: sk.content_md }) });
    if (!r.ok) showToast((r.data && r.data.error) || 'HTTP ' + r.status, 'error');
    renderKnowledgeCard();
  };

  AG.deleteSkill = async function (id) {
    if (!confirm(t('ag_k_confirm_delete'))) return;
    const r = await api('/api/agent/skills/custom/' + id, { method: 'DELETE' });
    if (!r.ok) showToast((r.data && r.data.error) || 'HTTP ' + r.status, 'error');
    renderKnowledgeCard();
  };
})();
