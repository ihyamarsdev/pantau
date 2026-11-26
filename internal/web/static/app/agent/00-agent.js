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
      ag_q_security: 'Review the latest security scan of host "{host}" (id {id}): pending security updates and failing hardening checks. Rank what matters and propose safe fixes. Do not change anything until I confirm the plan.'
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
      ag_q_security: 'Tinjau scan keamanan terakhir host "{host}" (id {id}): update keamanan tertunda dan hardening yang gagal. Urutkan yang penting dan usulkan perbaikan aman. Jangan ubah apa pun sebelum saya setujui rencananya.'
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

  /* ---------- minimal markdown: everything is escaped first, then fences, inline code and bold ---------- */
  function md(text) {
    const parts = String(text).split(/```/);
    return parts.map((p, i) => {
      if (i % 2 === 1) return '<pre><code>' + esc(p.replace(/^[a-zA-Z0-9_+-]*\n/, '')) + '</code></pre>';
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

  /** Open the chat and send a message (used by the "Ask agent" buttons). */
  AG.ask = function (text) {
    if (!S.enabled) { showToast(t('ag_disabled'), 'info'); return; }
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
      await readSSE(res.body, (ev, data) => {
        if (ev === 'agent.tool') {
          const label = (data.tool || 'tool') + (data.label && data.label !== data.tool ? ': ' + data.label : '');
          const line = { role: 'tool', text: label, status: data.status, id: data.toolCallId, domain: data.domain, risk_tier: data.risk_tier, duration_ms: data.duration_ms };
          const prev = S.msgs.findIndex((m) => m.role === 'tool' && m.id === data.toolCallId);
          if (prev >= 0 && data.status !== 'running') S.msgs[prev] = line;
          else S.msgs.splice(S.msgs.indexOf(bot) < 0 ? S.msgs.length : S.msgs.indexOf(bot), 0, line);
          // Tools that add, change or regroup hosts: the host list reloads when one finishes, not at the next 30 s poll.
          if (data.status !== 'running' && ['fleet_host_action', 'fleet_host_groups', 'fleet_host_import'].includes(data.tool) && typeof loadHosts === 'function') loadHosts();
        } else if (ev === 'agent.error') {
          S.msgs.push({ role: 'error', text: (data && data.error) || 'agent error' });
        } else if (data && data.choices && data.choices[0]) {
          const c = data.choices[0];
          if (c.delta && c.delta.content) bot.text += c.delta.content;
          if (c.finish_reason === 'error') S.msgs.push({ role: 'error', text: (data.error && data.error.message) || 'agent error' });
        }
        scheduleLog();
      });
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
})();
