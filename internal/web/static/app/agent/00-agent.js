/* AI agent: floating chat bottom-right, operator approval cards, Settings → AI Agent panel and the
 * "Ask agent" entry points (AG.askUnit / askContainer / askAlert / askSecurity). Backend: /api/agent/* (agent.go).
 * Relies on index.html globals: svgIcon, escapeHtml, t, I18N, showToast, currentHost, latestAlerts, langRerenderHooks. */
(function () {
  'use strict';

  const AG = (window.AG = {});
  const S = { grants: [], enabled: false, open: false, expanded: false, busy: false, session: null, abort: null, approvals: [], timer: null, msgs: [], chatT0: 0, apprT0: 0, fab: null, drag: null, suppressToggle: false, wired: false };
  const SESSION_KEY = 'pantau_agent_session';
  const POS_KEY = 'pantau_agent_fab_pos';

  const I = {
    en: {
      ag_title: 'AI Agent', ag_ask: 'Ask agent', ag_new: 'New chat', ag_close: 'Close', ag_expand: 'Expand', ag_collapse: 'Collapse', ag_send: 'Send',
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
      ag_s_approval: 'Approval for changes', ag_s_ask: 'Ask me for every change (recommended)',
      ag_s_auto: 'Auto-approve service/container actions, scans and inspections (shell commands still ask)',
      ag_s_soul: 'SOUL (the agent\'s identity and rules, sent with every chat)', ag_s_soul_reset: 'Reset SOUL to default',
      ag_s_save: 'Save', ag_s_test: 'Test connection', ag_s_saved: 'Saved', ag_s_test_ok: 'The provider answered',
      ag_s_testing: 'Testing connection…',
      ag_q_unit: 'Service {unit} on host "{host}" (id {id}) has a problem. Find the root cause and propose a fix. Check its status and journal first. Do not change anything until I confirm the plan.',
      ag_q_container: 'Container {name} on host "{host}" (id {id}) has a problem. Read its logs and state, find the root cause and propose a fix. Do not change anything until I confirm the plan.',
      ag_q_alert: 'Alert on host "{host}" (id {id}): {msg}\nRoot cause excerpt:\n{rc}\nInvestigate and propose a fix. Do not change anything until I confirm the plan.',
      ag_m_title: 'Agent memory', ag_m_desc: 'Durable notes the agent saves about your infrastructure and re-reads at the start of every chat. Review them: delete anything wrong. Notes are sent to your LLM provider with every chat.',
      ag_m_empty: 'No notes yet. The agent adds them as it learns your setup; you can add your own below.', ag_m_fleet: 'Whole fleet', ag_m_ph: 'e.g. Backups run at 02:00 via /opt/backup.sh',
      ag_m_scope: 'Scope', ag_m_scope_fleet: 'Whole fleet', ag_m_scope_host: 'One host', ag_m_scope_cluster: 'One cluster', ag_m_scope_domain: 'One domain',
      ag_m_add: 'Add', ag_m_forget: 'Forget', ag_m_forget_confirm: 'Delete this note?', ag_m_by_agent: 'agent', ag_m_by_you: 'you',
      ag_q_security: 'Review the latest security scan of host "{host}" (id {id}): pending security updates and failing hardening checks. Rank what matters and propose safe fixes. Do not change anything until I confirm the plan.'
    },
    id: {
      ag_title: 'Agen AI', ag_ask: 'Tanya agent', ag_new: 'Chat baru', ag_close: 'Tutup', ag_expand: 'Perluas', ag_collapse: 'Perkecil', ag_send: 'Kirim',
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
      ag_s_approval: 'Persetujuan perubahan', ag_s_ask: 'Tanya saya untuk setiap perubahan (disarankan)',
      ag_s_auto: 'Setujui otomatis aksi service/container, scan, dan inspeksi (perintah shell tetap bertanya)',
      ag_s_soul: 'SOUL (identitas dan aturan agent, dikirim di setiap chat)', ag_s_soul_reset: 'Kembalikan SOUL ke default',
      ag_s_save: 'Simpan', ag_s_test: 'Tes koneksi', ag_s_saved: 'Tersimpan', ag_s_test_ok: 'Penyedia menjawab',
      ag_s_testing: 'Menguji koneksi…',
      ag_q_unit: 'Service {unit} di host "{host}" (id {id}) bermasalah. Cari akar masalahnya dan usulkan perbaikan. Cek status dan journal dulu. Jangan ubah apa pun sebelum saya setujui rencananya.',
      ag_q_container: 'Container {name} di host "{host}" (id {id}) bermasalah. Baca log dan statusnya, cari akar masalah, lalu usulkan perbaikan. Jangan ubah apa pun sebelum saya setujui rencananya.',
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
        <div class="ag-head">${svgIcon('bot', 'icon-sm')}<span>${esc(t('ag_title'))}</span><span class="ag-sp"></span>
          <button class="btn btn-sm btn-ghost" onclick="AG.newChat()">${esc(t('ag_new'))}</button>
          <button class="btn btn-sm btn-icon btn-ghost" id="agentExpand" onclick="AG.expand()"></button>
          <button class="btn btn-sm btn-icon btn-ghost" aria-label="${esc(t('ag_close'))}" onclick="AG.toggle(false)">${svgIcon('x', 'icon-sm')}</button></div>
        <div class="ag-log" id="agentLog"></div>
        <form class="ag-form" onsubmit="AG.submit(event)">
          <textarea id="agentInput" rows="1" placeholder="${esc(t('ag_ph'))}" onkeydown="AG.key(event)"></textarea>
          <button class="btn btn-primary btn-sm" id="agentSend" type="submit">${svgIcon('send', 'icon-sm')}</button>
        </form>
      </div>
      <div id="agentApprovals"></div>`;
    while (root.firstChild) document.body.appendChild(root.firstChild);
    S.fab = storedPos() || defaultPos();
    renderLog();
    paintPanel();
    wirePlacement();
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
        return `<div class="ag-tool ag-tool-${m.status || 'running'}">${mark} ${esc(m.text)}</div>`;
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

  AG.toggle = function (force) {
    // A drag ends with a click on the bubble/header; that click must not toggle the chat.
    if (S.suppressToggle && force === undefined) { S.suppressToggle = false; return; }
    S.open = force === undefined ? !S.open : !!force;
    paintPanel();
    if (S.open) setTimeout(() => $('agentInput') && $('agentInput').focus(), 30);
  };

  // Panel classes and the expand button follow S, so a panel rebuilt by a language switch keeps its state.
  function paintPanel() {
    const panel = $('agentPanel');
    if (!panel) return;
    panel.classList.toggle('open', S.open);
    panel.classList.toggle('ag-expanded', S.expanded);
    const label = t(S.expanded ? 'ag_collapse' : 'ag_expand');
    const btn = $('agentExpand');
    btn.innerHTML = svgIcon(S.expanded ? 'minimize-2' : 'maximize-2', 'icon-sm');
    btn.title = label;
    btn.setAttribute('aria-label', label);
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

  function wirePlacement() {
    const fab = $('agentFab'), head = document.querySelector('#agentPanel .ag-head');
    fab.addEventListener('pointerdown', dragFrom);
    if (head) head.addEventListener('pointerdown', (e) => {
      if (e.target.closest && e.target.closest('button, a, input, textarea, select')) return;
      dragFrom(e);
    });
    if (S.wired) return;
    S.wired = true;
    window.addEventListener('resize', applyLayout);
    // Click outside the widget closes the chat; Capture phase so page handlers that swallow events cannot keep it open.
    document.addEventListener('pointerdown', (e) => {
      if (!S.open || !e.target.closest) return;
      if (e.target.closest('#agentPanel, #agentFab, #agentApprovals')) return;
      AG.toggle(false);
    }, true);
    document.addEventListener('keydown', (e) => {
      if (e.key !== 'Escape' || !S.open) return;
      if (document.querySelector('.modal.open')) return; // the modal stack owns Escape while a modal is on top
      AG.toggle(false);
    });
  }

  AG.newChat = function () {
    if (S.abort) S.abort.abort();
    api('/api/agent/grants', { method: 'DELETE' }).then(pollApprovals);
    S.msgs = [];
    S.session = newSession();
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
    if (!btn) return;
    btn.innerHTML = b ? svgIcon('square', 'icon-sm') : svgIcon('send', 'icon-sm');
    btn.title = b ? t('ag_stop') : t('ag_send');
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
      const res = await fetch('/api/agent/chat', {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, signal: S.abort.signal,
        body: JSON.stringify({ message: text, session_id: S.session })
      });
      if (!res.ok) {
        const j = await res.json().catch(() => ({}));
        const err = new Error(j.error || 'HTTP ' + res.status);
        err.settings = res.status === 400; // no provider/model configured: point at Settings → AI Agent
        throw err;
      }
      await readSSE(res.body, (ev, data) => {
        if (ev === 'agent.tool') {
          const label = (data.tool || 'tool') + (data.label && data.label !== data.tool ? ': ' + data.label : '');
          const line = { role: 'tool', text: label, status: data.status, id: data.toolCallId };
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
    if (S.enabled) S.timer = setInterval(pollApprovals, 2500);
    if (!S.enabled) { AG.toggle(false); $('agentApprovals').innerHTML = ''; }
    renderLog();
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
    body.innerHTML = `
      <div class="set-card">
        <div class="set-header"><div><h4 class="set-title">${svgIcon('bot')} <span>${esc(t('ag_title'))}</span></h4></div></div>
        <label style="display:flex;gap:8px;align-items:center;cursor:pointer;margin-bottom:12px;"><input type="checkbox" id="agEnabled" style="width:auto" ${cfg.enabled ? 'checked' : ''}> <b>${esc(t('ag_s_enable'))}</b></label>
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
        <div class="form-group"><label class="form-label">${esc(t('ag_s_approval'))}</label>
          <label style="display:flex;gap:8px;cursor:pointer;font-size:var(--fs-sm);"><input type="radio" name="agAppr" value="ask" style="width:auto" ${cfg.approval !== 'auto' ? 'checked' : ''}> ${esc(t('ag_s_ask'))}</label>
          <label style="display:flex;gap:8px;cursor:pointer;font-size:var(--fs-sm);margin-top:4px;"><input type="radio" name="agAppr" value="auto" style="width:auto" ${cfg.approval === 'auto' ? 'checked' : ''}> ${esc(t('ag_s_auto'))}</label></div>
        <div class="form-group"><label class="form-label">${esc(t('ag_s_soul'))}</label><textarea class="ag-soul" id="agSoul">${esc(cfg.soul)}</textarea>
          <div><button class="btn btn-sm btn-ghost" onclick="AG.resetSoul()">${esc(t('ag_s_soul_reset'))}</button></div></div>
        <div style="display:flex;gap:8px;margin-top:8px;">
          <button class="btn btn-primary btn-sm" id="agSave" onclick="AG.saveSettings()">${svgIcon('save', 'icon-sm')} <span>${esc(t('ag_s_save'))}</span></button>
          <button class="btn btn-ghost btn-sm" id="agTest" onclick="AG.testConn()">${esc(t('ag_s_test'))}</button>
        </div>
        <div id="agTestResult" style="margin-top:8px;font-size:var(--fs-xs);"></div>
      </div>
      <div id="agMemory"></div>`;
    AG.providerChanged();
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
      approval: (document.querySelector('input[name="agAppr"]:checked') || {}).value || 'ask',
      soul: $('agSoul').value
    };
    const key = $('agKey').value;
    if (key !== '') payload.api_key = key; // empty keeps the stored key
    const r = await post('/api/agent/settings', payload);
    if (r.ok) { showToast(t('ag_s_saved'), 'success'); await refreshEnabled(); await AG.loadSettings(); return ''; }
    showToast(r.data.error || 'error', 'error');
    return r.data.error || 'error';
  };

  AG.testConn = async function () {
    const before = $('agTestResult');
    if (before) before.innerHTML = SP.html(t('ag_s_testing'));
    const err = await AG.saveSettings();
    const out = $('agTestResult'); // saving re-renders the panel
    if (!out) return;
    if (err) { out.innerHTML = `<span style="color:var(--danger)">${esc(err)}</span>`; return; }
    let r;
    try {
      r = await post('/api/agent/test');
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
