/* AI agent (Hermes Agent): floating chat bottom-right, operator approval cards, Settings → AI Agent panel and the
 * "Ask agent" entry points (AG.askUnit / askContainer / askAlert / askSecurity). Backend: /api/agent/* (agent.go).
 * Relies on index.html globals: svgIcon, escapeHtml, t, I18N, showToast, currentHost, latestAlerts, langRerenderHooks. */
(function () {
  'use strict';

  const AG = (window.AG = {});
  const S = { grants: [], enabled: false, open: false, busy: false, session: null, abort: null, approvals: [], timer: null, msgs: [], chatT0: 0, apprT0: 0 };
  const SESSION_KEY = 'pantau_agent_session';

  const I = {
    en: {
      ag_title: 'AI Agent', ag_ask: 'Ask agent', ag_new: 'New chat', ag_close: 'Close', ag_send: 'Send',
      ag_ph: 'Ask about a service, container, alert or security finding…',
      ag_empty: 'Pantau Operator can read host state and, with your approval, restart services and containers or change settings on your hosts.',
      ag_disabled: 'The AI agent is off. Enable it in Settings → AI Agent.', ag_open_settings: 'Open AI Agent settings',
      ag_working: 'Working…', ag_stop: 'Stop',
      ag_waiting_approval: 'Waiting for your approval',
      ag_appr_title: 'Agent wants to run', ag_appr_exec: 'Shell command (always needs approval)', ag_appr_infra: 'Infrastructure change (always needs approval)', ag_approve: 'Approve', ag_deny: 'Deny', ag_approve_session: 'Approve for this session', ag_session_hint: 'Approves every later change on {host} without asking (shell commands too: {shell}). Ends on New chat, Revoke or after 2 hours.', ag_grants: 'Auto-approved this session', ag_revoke: 'Revoke', ag_g_shell: 'incl. shell', ag_g_tools: 'service/container actions',
      ag_s_enable: 'Enable AI agent', ag_s_url: 'Hermes API server URL', ag_s_key: 'Hermes API key (API_SERVER_KEY)',
      ag_s_key_saved: 'Saved. Leave empty to keep it.', ag_s_model: 'Model name sent to Hermes',
      ag_s_approval: 'Approval for changes', ag_s_ask: 'Ask me for every change (recommended)',
      ag_s_auto: 'Auto-approve service/container actions, scans and inspections (shell commands still ask)',
      ag_s_soul: 'SOUL (the agent\'s identity and rules, sent with every chat)', ag_s_soul_reset: 'Reset SOUL to default',
      ag_s_save: 'Save', ag_s_test: 'Test connection', ag_s_saved: 'Saved', ag_s_test_ok: 'Hermes answered',
      ag_s_connect: 'Connect Hermes to Pantau',
      ag_s_connect_desc: 'Add this to ~/.hermes/config.yaml, then restart the Hermes gateway (or run /reload-mcp). Hermes gets Pantau\'s tools as mcp__pantau__*.',
      ag_s_token: 'MCP token', ag_s_rotate: 'Rotate token', ag_s_rotate_confirm: 'Rotate the token? Hermes must be updated with the new one.',
      ag_s_hermes_env: 'Hermes side (~/.hermes/.env)',
      ag_q_unit: 'Service {unit} on host "{host}" (id {id}) has a problem. Find the root cause and propose a fix. Check its status and journal first. Do not change anything until I confirm the plan.',
      ag_q_container: 'Container {name} on host "{host}" (id {id}) has a problem. Read its logs and state, find the root cause and propose a fix. Do not change anything until I confirm the plan.',
      ag_q_alert: 'Alert on host "{host}" (id {id}): {msg}\nRoot cause excerpt:\n{rc}\nInvestigate and propose a fix. Do not change anything until I confirm the plan.',
      ag_m_title: 'Agent memory', ag_m_desc: 'Durable notes the agent saves about your infrastructure and re-reads at the start of every chat. Review them: delete anything wrong. Notes are sent to your LLM provider with every chat.',
      ag_m_empty: 'No notes yet. The agent adds them as it learns your setup; you can add your own below.', ag_m_fleet: 'Whole fleet', ag_m_ph: 'e.g. Backups run at 02:00 via /opt/backup.sh',
      ag_m_add: 'Add', ag_m_forget: 'Forget', ag_m_forget_confirm: 'Delete this note?', ag_m_by_agent: 'agent', ag_m_by_you: 'you',
      ag_q_security: 'Review the latest security scan of host "{host}" (id {id}): pending security updates and failing hardening checks. Rank what matters and propose safe fixes. Do not change anything until I confirm the plan.'
    },
    id: {
      ag_title: 'Agen AI', ag_ask: 'Tanya agent', ag_new: 'Chat baru', ag_close: 'Tutup', ag_send: 'Kirim',
      ag_ph: 'Tanyakan soal service, container, alert, atau temuan keamanan…',
      ag_empty: 'Pantau Operator bisa membaca kondisi host dan, dengan persetujuan Anda, me-restart service/container atau mengubah setting host.',
      ag_disabled: 'Agen AI nonaktif. Aktifkan di Pengaturan → AI Agent.', ag_open_settings: 'Buka pengaturan AI Agent',
      ag_working: 'Bekerja…', ag_stop: 'Berhenti',
      ag_waiting_approval: 'Menunggu persetujuan Anda',
      ag_appr_title: 'Agent ingin menjalankan', ag_appr_exec: 'Perintah shell (selalu butuh persetujuan)', ag_appr_infra: 'Perubahan infrastruktur (selalu butuh persetujuan)', ag_approve: 'Setujui', ag_deny: 'Tolak', ag_approve_session: 'Setujui untuk sesi ini', ag_session_hint: 'Menyetujui semua perubahan berikutnya di {host} tanpa bertanya (termasuk perintah shell: {shell}). Berakhir saat Chat baru, Cabut, atau setelah 2 jam.', ag_grants: 'Disetujui otomatis di sesi ini', ag_revoke: 'Cabut', ag_g_shell: 'termasuk shell', ag_g_tools: 'aksi service/container',
      ag_s_enable: 'Aktifkan agen AI', ag_s_url: 'URL API server Hermes', ag_s_key: 'API key Hermes (API_SERVER_KEY)',
      ag_s_key_saved: 'Tersimpan. Kosongkan untuk tetap memakainya.', ag_s_model: 'Nama model yang dikirim ke Hermes',
      ag_s_approval: 'Persetujuan perubahan', ag_s_ask: 'Tanya saya untuk setiap perubahan (disarankan)',
      ag_s_auto: 'Setujui otomatis aksi service/container, scan, dan inspeksi (perintah shell tetap bertanya)',
      ag_s_soul: 'SOUL (identitas dan aturan agent, dikirim di setiap chat)', ag_s_soul_reset: 'Kembalikan SOUL ke default',
      ag_s_save: 'Simpan', ag_s_test: 'Tes koneksi', ag_s_saved: 'Tersimpan', ag_s_test_ok: 'Hermes menjawab',
      ag_s_connect: 'Hubungkan Hermes ke Pantau',
      ag_s_connect_desc: 'Tambahkan ini ke ~/.hermes/config.yaml, lalu restart gateway Hermes (atau jalankan /reload-mcp). Hermes mendapat tool Pantau sebagai mcp__pantau__*.',
      ag_s_token: 'Token MCP', ag_s_rotate: 'Ganti token', ag_s_rotate_confirm: 'Ganti token? Hermes harus diperbarui dengan token baru.',
      ag_s_hermes_env: 'Sisi Hermes (~/.hermes/.env)',
      ag_q_unit: 'Service {unit} di host "{host}" (id {id}) bermasalah. Cari akar masalahnya dan usulkan perbaikan. Cek status dan journal dulu. Jangan ubah apa pun sebelum saya setujui rencananya.',
      ag_q_container: 'Container {name} di host "{host}" (id {id}) bermasalah. Baca log dan statusnya, cari akar masalah, lalu usulkan perbaikan. Jangan ubah apa pun sebelum saya setujui rencananya.',
      ag_q_alert: 'Alert di host "{host}" (id {id}): {msg}\nCuplikan root cause:\n{rc}\nSelidiki dan usulkan perbaikan. Jangan ubah apa pun sebelum saya setujui rencananya.',
      ag_m_title: 'Memori agent', ag_m_desc: 'Catatan permanen yang disimpan agent tentang infrastruktur Anda dan dibaca ulang di awal setiap chat. Tinjau: hapus yang salah. Catatan dikirim ke penyedia LLM Anda di setiap chat.',
      ag_m_empty: 'Belum ada catatan. Agent menambahkannya saat memahami setup Anda; Anda juga bisa menambah sendiri di bawah.', ag_m_fleet: 'Seluruh fleet', ag_m_ph: 'misal: Backup jalan jam 02:00 lewat /opt/backup.sh',
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
          <button class="btn btn-sm btn-icon btn-ghost" aria-label="${esc(t('ag_close'))}" onclick="AG.toggle(false)">${svgIcon('x', 'icon-sm')}</button></div>
        <div class="ag-log" id="agentLog"></div>
        <form class="ag-form" onsubmit="AG.submit(event)">
          <textarea id="agentInput" rows="1" placeholder="${esc(t('ag_ph'))}" onkeydown="AG.key(event)"></textarea>
          <button class="btn btn-primary btn-sm" id="agentSend" type="submit">${svgIcon('send', 'icon-sm')}</button>
        </form>
      </div>
      <div id="agentApprovals"></div>`;
    while (root.firstChild) document.body.appendChild(root.firstChild);
    renderLog();
  }

  function renderLog() {
    const log = $('agentLog');
    if (!log) return;
    if (!S.enabled) { log.innerHTML = `<div class="ag-empty">${esc(t('ag_disabled'))}<br><br><button class="btn btn-sm btn-primary" onclick="AG.toggle(false); openSettingsModal('agent')">${esc(t('ag_open_settings'))}</button></div>`; return; }
    if (S.msgs.length === 0) { log.innerHTML = `<div class="ag-empty">${esc(t('ag_empty'))}</div>`; return; }
    log.innerHTML = S.msgs.map((m) => {
      if (m.role === 'user') return `<div class="ag-msg ag-user">${esc(m.text)}</div>`;
      if (m.role === 'tool') return `<div class="ag-tool">${svgIcon('wrench', 'icon-sm')} ${esc(m.text)}</div>`;
      if (m.role === 'error') return `<div class="ag-err">${esc(m.text)}</div>`;
      if (!m.text) return `<div class="ag-msg ag-bot">${S.approvals.length ? SP.html(t('ag_waiting_approval'), S.apprT0) : SP.html(t('ag_working'), S.chatT0)}</div>`;
    }).join('');
    log.scrollTop = log.scrollHeight;
  }

  AG.toggle = function (force) {
    S.open = force === undefined ? !S.open : !!force;
    $('agentPanel').classList.toggle('open', S.open);
    if (S.open) setTimeout(() => $('agentInput') && $('agentInput').focus(), 30);
  };

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
        throw new Error(j.error || 'HTTP ' + res.status);
      }
      await readSSE(res.body, (ev, data) => {
        if (ev === 'hermes.tool.progress') {
          if (data.status === 'running') {
            const at = S.msgs.indexOf(bot);
            S.msgs.splice(at, 0, { role: 'tool', text: (data.tool || 'tool') + (data.label && data.label !== data.tool ? ': ' + data.label : '') });
          }
        } else if (data && data.choices && data.choices[0]) {
          const c = data.choices[0];
          if (c.delta && c.delta.content) bot.text += c.delta.content;
          if (c.finish_reason === 'error') S.msgs.push({ role: 'error', text: (data.error && data.error.message) || 'agent error' });
        }
        renderLog();
      });
    } catch (e) {
      if (e.name !== 'AbortError') S.msgs.push({ role: 'error', text: e.message });
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
    if (host.dataset.html !== html) { host.dataset.html = html; host.innerHTML = html; }
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
  let cfg = null;
  AG.loadSettings = async function () {
    const body = $('agentSettingsBody');
    if (!body) return;
    const r = await api('/api/agent/settings');
    if (!r.ok) { body.innerHTML = `<div class="ag-err">HTTP ${r.status}</div>`; return; }
    cfg = r.data;
    const mcpUrl = location.origin + '/mcp';
    const snippet = `mcp_servers:\n  pantau:\n    url: "${mcpUrl}"\n    headers:\n      Authorization: "Bearer ${cfg.mcp_token}"\n    timeout: 300\n    connect_timeout: 30`;
    const envSnip = `API_SERVER_ENABLED=true\nAPI_SERVER_KEY=<choose-a-secret>`;
    body.innerHTML = `
      <div class="set-card">
        <div class="set-header"><div><h4 class="set-title">${svgIcon('bot')} <span>${esc(t('ag_title'))} · Hermes Agent</span></h4>
          <p class="set-desc">https://github.com/NousResearch/hermes-agent</p></div></div>
        <label style="display:flex;gap:8px;align-items:center;cursor:pointer;margin-bottom:12px;"><input type="checkbox" id="agEnabled" style="width:auto" ${cfg.enabled ? 'checked' : ''}> <b>${esc(t('ag_s_enable'))}</b></label>
        <div class="form-group"><label class="form-label">${esc(t('ag_s_url'))}</label><input type="text" id="agUrl" value="${esc(cfg.url)}" placeholder="http://127.0.0.1:8642"></div>
        <div class="form-group"><label class="form-label">${esc(t('ag_s_key'))}</label><input type="password" id="agKey" placeholder="••••••••" autocomplete="new-password">
          ${cfg.key_set ? `<span class="badge badge-info" style="margin-top:4px;">${esc(t('ag_s_key_saved'))}</span>` : ''}</div>
        <div class="form-group"><label class="form-label">${esc(t('ag_s_model'))}</label><input type="text" id="agModel" value="${esc(cfg.model)}" placeholder="hermes-agent"></div>
        <div class="form-group"><label class="form-label">${esc(t('ag_s_approval'))}</label>
          <label style="display:flex;gap:8px;cursor:pointer;font-size:var(--fs-sm);"><input type="radio" name="agAppr" value="ask" style="width:auto" ${cfg.approval !== 'auto' ? 'checked' : ''}> ${esc(t('ag_s_ask'))}</label>
          <label style="display:flex;gap:8px;cursor:pointer;font-size:var(--fs-sm);margin-top:4px;"><input type="radio" name="agAppr" value="auto" style="width:auto" ${cfg.approval === 'auto' ? 'checked' : ''}> ${esc(t('ag_s_auto'))}</label></div>
        <div class="form-group"><label class="form-label">${esc(t('ag_s_soul'))}</label><textarea class="ag-soul" id="agSoul">${esc(cfg.soul)}</textarea>
          <div><button class="btn btn-sm btn-ghost" onclick="AG.resetSoul()">${esc(t('ag_s_soul_reset'))}</button></div></div>
        <div style="display:flex;gap:8px;margin-top:8px;">
          <button class="btn btn-primary btn-sm" onclick="AG.saveSettings()">${svgIcon('save', 'icon-sm')} <span>${esc(t('ag_s_save'))}</span></button>
          <button class="btn btn-ghost btn-sm" onclick="AG.testConn()">${esc(t('ag_s_test'))}</button>
        </div>
        <div id="agTestResult" style="margin-top:8px;font-size:var(--fs-xs);"></div>
      </div>
      <div class="set-card">
        <div class="set-header"><div><h4 class="set-title">${svgIcon('plug')} <span>${esc(t('ag_s_connect'))}</span></h4>
          <p class="set-desc">${esc(t('ag_s_connect_desc'))}</p></div></div>
        <div class="form-label">${esc(t('ag_s_hermes_env'))}</div><pre class="ag-snippet">${esc(envSnip)}</pre>
        <div class="form-label">~/.hermes/config.yaml</div><pre class="ag-snippet">${esc(snippet)}</pre>
        <div><button class="btn btn-sm btn-ghost" onclick="AG.rotateToken()">${esc(t('ag_s_rotate'))}</button></div>
      </div>
      <div id="agMemory"></div>`;
    AG.loadMemory();
  };

  AG.resetSoul = async function () {
    await post('/api/agent/settings', { soul: '' });
    AG.loadSettings();
  };

  AG.saveSettings = async function () {
    const payload = {
      enabled: $('agEnabled').checked, url: $('agUrl').value.trim(), model: $('agModel').value.trim(),
      approval: (document.querySelector('input[name="agAppr"]:checked') || {}).value || 'ask', soul: $('agSoul').value
    };
    const key = $('agKey').value;
    if (key !== '') payload.key = key;
    const r = await post('/api/agent/settings', payload);
    if (r.ok) { showToast(t('ag_s_saved'), 'success'); await refreshEnabled(); AG.loadSettings(); }
    else showToast(r.data.error || 'error', 'error');
  };

  AG.testConn = async function () {
    const out = $('agTestResult');
    await AG.saveSettings();
    out.innerHTML = SP.html(t('ag_s_testing', 'Testing connection…'));
    let r;
    try {
      r = await post('/api/agent/test');
    } catch (e) {
      out.innerHTML = `<span style="color:var(--danger)">${esc(e.message)}</span>`;
      return;
    }
    out.innerHTML = r.data.ok ? `<span style="color:var(--success)">${esc(t('ag_s_test_ok'))}</span>` : `<span style="color:var(--danger)">${esc(r.data.error || 'error')}</span>`;
  };

  AG.rotateToken = async function () {
    if (!(await showConfirm({ message: t('ag_s_rotate_confirm') }))) return;
    await post('/api/agent/rotate-token');
    AG.loadSettings();
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
        <span class="badge ${n.host_id ? 'badge-info' : ''}" style="flex:none;">${esc(n.host_id ? name(n.host_id) : t('ag_m_fleet'))}</span>
        <span style="flex:1;font-size:var(--fs-sm);white-space:pre-wrap;word-break:break-word;">${esc(n.content)}</span>
        <span style="flex:none;font-size:var(--fs-xs);color:var(--text-muted);">${esc(n.source === 'agent' ? t('ag_m_by_agent') : t('ag_m_by_you'))}</span>
        <button class="btn btn-sm btn-icon btn-ghost" aria-label="${esc(t('ag_m_forget'))}" title="${esc(t('ag_m_forget'))}" onclick="AG.delMemory(${n.id})">${svgIcon('trash-2', 'icon-sm')}</button></div>`).join('');
    box.innerHTML = `
      <div class="set-card">
        <div class="set-header"><div><h4 class="set-title">${svgIcon('bookmark')} <span>${esc(t('ag_m_title'))} (${notes.length}/${r.data.max})</span></h4>
          <p class="set-desc">${esc(t('ag_m_desc'))}</p></div></div>
        ${rows || `<div style="font-size:var(--fs-sm);color:var(--text-muted);">${esc(t('ag_m_empty'))}</div>`}
        <div style="display:flex;gap:8px;margin-top:10px;">
          <select id="agMemHost" style="width:auto;max-width:160px;"><option value="">${esc(t('ag_m_fleet'))}</option>${hosts.map((h) => `<option value="${h.id}">${esc(h.name)}</option>`).join('')}</select>
          <input type="text" id="agMemText" maxlength="${r.data.max_len}" placeholder="${esc(t('ag_m_ph'))}" style="flex:1;" onkeydown="if(event.key==='Enter'){event.preventDefault();AG.addMemory();}">
          <button class="btn btn-primary btn-sm" onclick="AG.addMemory()">${svgIcon('plus', 'icon-sm')} <span>${esc(t('ag_m_add'))}</span></button>
        </div>
      </div>`;
  };

  AG.addMemory = async function () {
    const text = $('agMemText').value.trim();
    if (!text) return;
    const host = $('agMemHost').value;
    const r = await post('/api/agent/memory', { content: text, host_id: host ? Number(host) : null });
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
