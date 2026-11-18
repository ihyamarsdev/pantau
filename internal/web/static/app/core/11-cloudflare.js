/* Cloudflare accounts, zones, DNS, settings, and tunnels.
 * Native ES module fragment of the former monolithic inline script.
 * Air-gapped: no bundler, no CDN. Shared state lives in the mutable S object
 * from 00-state.js; cross-domain functions are imported explicitly. */
import { S } from './00-state.js';
import { svgIcon } from './00-foundation.js';
import { t, showConfirm } from './01-i18n-auth.js';
import { showToast } from './02-hosts.js';
import { openModal, closeModal, escapeHtml, jsArg } from './06-modal-docs-activity.js';

async function loadCfAccounts() {
  try {
    const res = await fetch('/api/cloudflare/accounts');
    const data = await res.json();
    if (data && Array.isArray(data.accounts)) {
      S.cfAccounts = data.accounts;
    } else {
      S.cfAccounts = [];
    }
  } catch (e) {
    S.cfAccounts = [];
  }
  populateCfAccountSelect();
}

function populateCfAccountSelect() {
  const sel = document.getElementById('cfAccountSelect');
  const btnEdit = document.getElementById('btnCfEditAccount');
  const btnDel = document.getElementById('btnCfDeleteAccount');
  if (!sel) return;
  const prev = sel.value;
  sel.innerHTML = `<option value="">${t('cf_select_account', 'Select an account...')}</option>` +
    S.cfAccounts.map(a => `<option value="${a.id}">${escapeHtml(a.name)}</option>`).join('');
  if (S.selectedCfAccountId && S.cfAccounts.some(a => String(a.id) === String(S.selectedCfAccountId))) {
    sel.value = String(S.selectedCfAccountId);
  } else if (prev && S.cfAccounts.some(a => String(a.id) === String(prev))) {
    sel.value = prev;
    S.selectedCfAccountId = Number(prev);
  } else {
    S.selectedCfAccountId = null;
    sel.value = '';
  }
  const hasAcc = !!S.selectedCfAccountId;
  if (btnEdit) btnEdit.style.display = hasAcc ? 'inline-flex' : 'none';
  if (btnDel) btnDel.style.display = hasAcc ? 'inline-flex' : 'none';
  onSelectCfAccount(sel.value, true);
}

async function onSelectCfAccount(accId, keep) {
  S.selectedCfAccountId = accId ? Number(accId) : null;
  const btnEdit = document.getElementById('btnCfEditAccount');
  const btnDel = document.getElementById('btnCfDeleteAccount');
  if (btnEdit) btnEdit.style.display = S.selectedCfAccountId ? 'inline-flex' : 'none';
  if (btnDel) btnDel.style.display = S.selectedCfAccountId ? 'inline-flex' : 'none';
  const prompt = document.getElementById('cfSelectAccountPrompt');
  const container = document.getElementById('cfZonesContainer');
  if (!S.selectedCfAccountId) {
    if (prompt) prompt.style.display = 'block';
    if (container) container.style.display = 'none';
    S.cfZonesCache = [];
    return;
  }
  if (prompt) prompt.style.display = 'none';
  if (container) container.style.display = 'flex';
  cfTunnelsReset();
  if (!keep) {
    await loadCfZones(S.selectedCfAccountId);
  }
}

async function refreshCfSection() {
  await loadCfAccounts();
  if (S.selectedCfAccountId) {
    await loadCfZones(S.selectedCfAccountId);
    if (document.getElementById('cfTunnelsContainer')?.open) await loadCfTunnels();
  }
}

async function loadCfZones(accId) {
  const grid = document.getElementById('cfZonesGrid');
  const empty = document.getElementById('cfZonesEmpty');
  const countBadge = document.getElementById('cfZoneCountBadge');
  if (grid) grid.innerHTML = `<div style="grid-column:1/-1;">${SP.loading(t('loading', 'Loading...'))}</div>`;
  try {
    const res = await fetch(`/api/cloudflare/accounts/${accId}/zones`);
    const data = await res.json();
    if (!res.ok) {
      setCfPerm('cfZonesPerm', 'zones', data);
      if (!data.forbidden) showToast(t('error_prefix', 'Error: ') + (data.error || 'Failed to load zones'), 'error');
      S.cfZonesCache = [];
    } else {
      setCfPerm('cfZonesPerm', null);
      S.cfZonesCache = Array.isArray(data.zones) ? data.zones : [];
    }
  } catch (e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error');
    S.cfZonesCache = [];
  }
  if (countBadge) countBadge.textContent = `${S.cfZonesCache.length} zone${S.cfZonesCache.length === 1 ? '' : 's'}`;
  renderCfZones();
}

function filterCfZones() {
  renderCfZones();
}

function renderCfZones() {
  const grid = document.getElementById('cfZonesGrid');
  const empty = document.getElementById('cfZonesEmpty');
  if (!grid || !empty) return;
  const q = (document.getElementById('cfZoneSearchInput')?.value || '').toLowerCase().trim();
  const filtered = q ? S.cfZonesCache.filter(z => (z.name || '').toLowerCase().includes(q) || (z.plan || '').toLowerCase().includes(q)) : S.cfZonesCache;
  if (filtered.length === 0) {
    grid.style.display = 'none';
    empty.style.display = 'block';
    return;
  }
  grid.style.display = 'grid';
  empty.style.display = 'none';
  grid.innerHTML = filtered.map(z => {
    const status = z.status || 'unknown';
    const plan = z.plan || 'Free';
    const nameServers = Array.isArray(z.name_servers) ? z.name_servers.join(', ') : '';
    const isPaused = !!z.paused;
    return `<div class="card host-card" style="cursor:pointer;" onclick="openCfZoneModal(${jsArg(z.id)})">
      <div class="host-card-header">
        <div style="display:flex;align-items:center;gap:8px;min-width:0;">
          <span class="status-dot ${status === 'active' ? 'online' : (status === 'pending' ? 'warn' : 'offline')}"></span>
          <span style="font-weight:600;font-size:var(--fs-md);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;" title="${escapeHtml(z.name)}">${escapeHtml(z.name)}</span>
        </div>
        <div style="display:flex;align-items:center;gap:6px;">
          ${isPaused ? `<span class="badge badge-warning">${t('cf_zone_paused', 'Paused')}</span>` : ''}
          <span class="badge ${status === 'active' ? 'badge-healthy' : 'badge-warning'}">${escapeHtml(status)}</span>
        </div>
      </div>
      <div style="display:flex;align-items:center;gap:6px;font-size:var(--fs-xs);color:var(--text-muted);">
        <span class="badge" style="background:var(--surface-2);border:1px solid var(--card-border);">${escapeHtml(plan)}</span>
        <span class="mono" style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap;" title="${escapeHtml(z.id)}">${escapeHtml(z.id)}</span>
      </div>
      <div style="font-size:var(--fs-xs);color:var(--text-muted);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;" title="${escapeHtml(nameServers)}">
        NS: ${escapeHtml(nameServers || '-')}
      </div>
      <div style="margin-top:auto;display:flex;justify-content:flex-end;">
        <button type="button" class="btn btn-sm btn-primary" onclick="event.stopPropagation();openCfZoneModal(${jsArg(z.id)})" style="display:inline-flex;align-items:center;gap:6px;">
          ${svgIcon('cloud', 'icon-sm')} <span>${t('cf_manage_zone', 'Manage Zone')}</span>
        </button>
      </div>
    </div>`;
  }).join('');
}

function openCfAccountModal(accId) {
  const titleEl = document.getElementById('cfAccountModalTitle');
  const nameInput = document.getElementById('cfAccountName');
  const cfidInput = document.getElementById('cfAccountCFID');
  const tokenInput = document.getElementById('cfAccountToken');
  const tokenStar = document.getElementById('cfAccountTokenReqStar');
  const tokenHint = document.getElementById('cfAccountTokenHint');
  const errEl = document.getElementById('cfAccountError');
  const submitBtn = document.getElementById('btnCfAccountSubmit');
  const submitText = document.getElementById('btnCfAccountSubmitText');
  if (errEl) { errEl.style.display = 'none'; errEl.textContent = ''; }
  if (submitBtn) submitBtn.disabled = false;
  if (tokenInput) tokenInput.value = '';
  if (accId) {
    const existing = S.cfAccounts.find(a => String(a.id) === String(accId));
    if (!existing) return;
    S.cfAccountModalMode = 'edit';
    S.cfAccountEditId = existing.id;
    if (titleEl) titleEl.textContent = t('cf_account_modal_title_edit', 'Edit Cloudflare Account');
    if (nameInput) nameInput.value = existing.name || '';
    if (cfidInput) cfidInput.value = existing.account_id || '';
    if (tokenStar) tokenStar.style.display = 'none';
    if (tokenHint) tokenHint.style.display = 'block';
    if (submitText) submitText.textContent = t('cf_btn_save', 'Save');
  } else {
    S.cfAccountModalMode = 'add';
    S.cfAccountEditId = null;
    if (titleEl) titleEl.textContent = t('cf_account_modal_title_add', 'Add Cloudflare Account');
    if (nameInput) nameInput.value = '';
    if (cfidInput) cfidInput.value = '';
    if (tokenStar) tokenStar.style.display = 'inline';
    if (tokenHint) tokenHint.style.display = 'none';
    if (submitText) submitText.textContent = t('cf_btn_save', 'Verify & Save');
  }
  openModal('cfAccountModal');
  setTimeout(() => { if (nameInput) nameInput.focus(); }, 50);
}

async function submitCfAccountModal() {
  const name = (document.getElementById('cfAccountName')?.value || '').trim();
  const accountId = (document.getElementById('cfAccountCFID')?.value || '').trim();
  const apiToken = (document.getElementById('cfAccountToken')?.value || '').trim();
  const errEl = document.getElementById('cfAccountError');
  const submitBtn = document.getElementById('btnCfAccountSubmit');
  const setError = (msg) => {
    if (!errEl) return;
    errEl.textContent = msg;
    errEl.style.display = msg ? 'block' : 'none';
  };
  setError('');
  if (!name) {
    setError(t('cf_err_name_token_required', 'Account name and API token are required'));
    return;
  }
  if (S.cfAccountModalMode === 'add' && !apiToken) {
    setError(t('cf_err_name_token_required', 'Account name and API token are required'));
    return;
  }
  const payload = { name };
  if (accountId) payload.account_id = accountId;
  if (apiToken) payload.api_token = apiToken;

  const restore = SP.busy(submitBtn, t('working', 'Working...'));
  try {
    const url = S.cfAccountModalMode === 'edit' ? `/api/cloudflare/accounts/${S.cfAccountEditId}` : '/api/cloudflare/accounts';
    const method = S.cfAccountModalMode === 'edit' ? 'PUT' : 'POST';
    const res = await fetch(url, {
      method,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    const data = await res.json();
    if (!res.ok) {
      setError(data.error || 'Server error');
      return;
    }
    closeModal('cfAccountModal');
    showToast(t('cf_account_saved', 'Cloudflare Account saved successfully'), 'success');
    await loadCfAccounts();
    const savedId = (data.account && data.account.id) || S.cfAccountEditId;
    if (savedId) {
      S.selectedCfAccountId = Number(savedId);
      const sel = document.getElementById('cfAccountSelect');
      if (sel) sel.value = String(savedId);
      onSelectCfAccount(savedId);
    }
  } catch (e) {
    setError(e.message || 'Network error');
  } finally {
    restore();
  }
}

async function deleteCfAccount(accId, btn) {
  if (!accId) return;
  const acc = S.cfAccounts.find(a => Number(a.id) === Number(accId));
  const accName = acc ? acc.name : `#${accId}`;
  const confirmed = await showConfirm({
    title: t('cf_confirm_delete_account_title', 'Delete Cloudflare Account'),
    message: t('cf_confirm_delete_account_msg', 'Are you sure you want to delete Cloudflare Account "{name}"? This removes stored credentials and access configuration.').replace('{name}', accName),
    confirmText: t('delete', 'Delete'),
    danger: true
  });
  if (!confirmed) return;
  const restore = SP.busy(btn, t('cf_deleting_record', 'Deleting...'));
  try {
    const res = await fetch(`/api/cloudflare/accounts/${accId}`, { method: 'DELETE' });
    const data = await res.json().catch(() => ({}));
    if (!res.ok || !data.ok) {
      showToast(t('error_prefix', 'Error: ') + (data.error || 'Failed to delete account'), 'error');
      return;
    }
    showToast(t('cf_account_deleted', 'Cloudflare Account deleted'), 'info');
    S.selectedCfAccountId = null;
    await loadCfAccounts();
  } catch (e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error');
  } finally {
    restore();
  }
}

async function openCfZoneModal(zoneId) {
  const zone = S.cfZonesCache.find(z => z.id === zoneId);
  if (!zone) return;
  S.currentCfZone = zone;
  const nameEl = document.getElementById('cfZoneModalName');
  const statusBadge = document.getElementById('cfZoneStatusBadge');
  const planBadge = document.getElementById('cfZonePlanBadge');
  const pausedBadge = document.getElementById('cfZonePausedBadge');
  if (nameEl) nameEl.textContent = zone.name || '';
  if (statusBadge) {
    statusBadge.textContent = zone.status || 'unknown';
    statusBadge.className = `badge ${zone.status === 'active' ? 'badge-healthy' : 'badge-warning'}`;
  }
  if (planBadge) planBadge.textContent = zone.plan || 'Free';
  if (pausedBadge) pausedBadge.style.display = zone.paused ? 'inline-flex' : 'none';
  openModal('cfZoneModal');
  switchCfZoneTab('dns');
}

function switchCfZoneTab(tab) {
  S.currentCfZoneTab = tab;
  document.querySelectorAll('#cfZoneTabs .tab').forEach(el => {
    const isAct = el.id === ('tabCfZone' + tab.charAt(0).toUpperCase() + tab.slice(1));
    el.classList.toggle('active', isAct);
  });
  const pDns = document.getElementById('cfZonePanelDns');
  const pCache = document.getElementById('cfZonePanelCache');
  const pSettings = document.getElementById('cfZonePanelSettings');
  const pAccess = document.getElementById('cfZonePanelAccess');
  if (pDns) pDns.style.display = tab === 'dns' ? 'flex' : 'none';
  if (pCache) pCache.style.display = tab === 'cache' ? 'flex' : 'none';
  if (pSettings) pSettings.style.display = tab === 'settings' ? 'flex' : 'none';
  if (pAccess) pAccess.style.display = tab === 'access' ? 'flex' : 'none';
  setCfPerm('cfZonePerm', null);
  if (tab === 'dns') loadCfDnsRecords();
  else if (tab === 'settings') loadCfZoneSettings();
  else if (tab === 'access') loadCfAccessRules();
}

async function refreshCurrentCfZoneTab() {
  if (S.currentCfZoneTab === 'dns') await loadCfDnsRecords();
  else if (S.currentCfZoneTab === 'settings') await loadCfZoneSettings();
  else if (S.currentCfZoneTab === 'access') await loadCfAccessRules();
}

async function loadCfDnsRecords() {
  if (!S.selectedCfAccountId || !S.currentCfZone) return;
  const tbody = document.getElementById('cfDnsTableBody');
  const empty = document.getElementById('cfDnsEmptyState');
  if (tbody) tbody.innerHTML = `<tr><td colspan="7" style="text-align:center;padding:20px;color:var(--text-muted);">${t('loading', 'Loading...')}</td></tr>`;
  try {
    const res = await fetch(`/api/cloudflare/accounts/${S.selectedCfAccountId}/zones/${S.currentCfZone.id}/dns`);
    const data = await res.json();
    if (!res.ok) {
      setCfPerm('cfZonePerm', 'dns_read', data);
      if (!data.forbidden) showToast(t('error_prefix', 'Error: ') + (data.error || 'Failed to load DNS records'), 'error');
      S.cfDnsRecords = [];
    } else {
      setCfPerm('cfZonePerm', null);
      S.cfDnsRecords = Array.isArray(data.records) ? data.records : [];
    }
  } catch (e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error');
    S.cfDnsRecords = [];
  }
  renderCfDnsTable();
}

function renderCfDnsTable() {
  const tbody = document.getElementById('cfDnsTableBody');
  const empty = document.getElementById('cfDnsEmptyState');
  if (!tbody || !empty) return;
  const q = (document.getElementById('cfDnsSearchInput')?.value || '').toLowerCase().trim();
  const filtered = q ? S.cfDnsRecords.filter(r => (r.type || '').toLowerCase().includes(q) || (r.name || '').toLowerCase().includes(q) || (r.content || '').toLowerCase().includes(q) || (r.comment || '').toLowerCase().includes(q)) : S.cfDnsRecords;
  if (filtered.length === 0) {
    tbody.innerHTML = '';
    empty.style.display = 'block';
    return;
  }
  empty.style.display = 'none';
  tbody.innerHTML = filtered.map(r => {
    const ttlStr = r.ttl === 1 ? t('cf_ttl_auto', 'Auto') : (r.ttl >= 3600 ? `${r.ttl / 3600}h` : `${r.ttl}s`);
    const isProxied = r.proxied === true;
    const proxyBadge = r.proxiable
      ? `<button type="button" class="badge ${isProxied ? 'badge-warning' : 'badge-info'}" style="cursor:pointer;border:none;outline:none;" onclick="toggleCfRecordProxied(${jsArg(r.id)}, this)" title="Click to toggle proxy">
          ${svgIcon('cloud', 'icon-xs')} <span>${isProxied ? t('cf_proxied_badge', 'Proxied') : t('cf_dns_only_badge', 'DNS only')}</span>
        </button>`
      : `<span class="badge" style="background:var(--surface-2);color:var(--text-muted);border:1px solid var(--card-border);">${t('cf_dns_only_badge', 'DNS only')}</span>`;
    return `<tr>
      <td><span class="badge" style="font-family:var(--font-mono);background:var(--surface-2);border:1px solid var(--card-border);">${escapeHtml(r.type)}</span></td>
      <td class="mono" style="font-weight:600;word-break:break-all;">${escapeHtml(r.name)}</td>
      <td class="mono" style="word-break:break-all;color:var(--text-muted);">${escapeHtml(r.content)}${r.priority !== undefined && r.priority !== null ? ` <span class="badge" style="font-size:10px;">prio ${r.priority}</span>` : ''}</td>
      <td>${proxyBadge}</td>
      <td style="font-size:var(--fs-xs);">${escapeHtml(ttlStr)}</td>
      <td style="font-size:var(--fs-xs);color:var(--text-muted);">${escapeHtml(r.comment || '-')}</td>
      <td style="text-align:right;white-space:nowrap;">
        <button type="button" class="btn btn-sm btn-ghost btn-icon" title="Edit" onclick="openCfRecordModal(${jsArg(r.id)})">${svgIcon('pencil', 'icon-sm')}</button>
        <button type="button" class="btn btn-sm btn-ghost btn-icon text-danger" title="Delete" onclick="deleteCfRecord(${jsArg(r.id)}, this)">${svgIcon('trash-2', 'icon-sm')}</button>
      </td>
    </tr>`;
  }).join('');
}

async function toggleCfRecordProxied(recordId, btn) {
  if (!S.selectedCfAccountId || !S.currentCfZone) return;
  const rec = S.cfDnsRecords.find(r => r.id === recordId);
  if (!rec || !rec.proxiable) return;
  const nextProxied = !rec.proxied;
  const payload = {
    type: rec.type,
    name: rec.name,
    content: rec.content,
    ttl: rec.ttl,
    proxied: nextProxied
  };
  if (rec.priority !== undefined && rec.priority !== null) payload.priority = rec.priority;
  if (rec.comment) payload.comment = rec.comment;
  const restore = SP.busy(btn, t('cf_saving_record', 'Saving...'));
  try {
    const res = await fetch(`/api/cloudflare/accounts/${S.selectedCfAccountId}/zones/${S.currentCfZone.id}/dns/${recordId}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
      showToast(t('error_prefix', 'Error: ') + cfErrMsg(data, 'Failed to update record', 'dns'), 'error');
      return;
    }
    showToast(t('cf_record_saved', 'DNS record saved successfully'), 'success');
    await loadCfDnsRecords();
  } catch (e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error');
  } finally {
    restore();
  }
}

function onCfRecordTypeChange() {
  const type = document.getElementById('cfRecordType')?.value || 'A';
  const prioField = document.getElementById('cfRecordPriorityField');
  const proxyWrap = document.getElementById('cfRecordProxiedWrap');
  if (prioField) prioField.style.display = type === 'MX' ? 'block' : 'none';
  const canProxy = type === 'A' || type === 'AAAA' || type === 'CNAME';
  if (proxyWrap) proxyWrap.style.display = canProxy ? 'block' : 'none';
  if (!canProxy) {
    const cb = document.getElementById('cfRecordProxied');
    if (cb) cb.checked = false;
  }
}

function openCfRecordModal(recordId) {
  const titleEl = document.getElementById('cfRecordModalTitle');
  const typeEl = document.getElementById('cfRecordType');
  const nameEl = document.getElementById('cfRecordName');
  const contentEl = document.getElementById('cfRecordContent');
  const ttlEl = document.getElementById('cfRecordTTL');
  const prioEl = document.getElementById('cfRecordPriority');
  const proxiedEl = document.getElementById('cfRecordProxied');
  const commentEl = document.getElementById('cfRecordComment');
  const errEl = document.getElementById('cfRecordError');
  if (errEl) { errEl.style.display = 'none'; errEl.textContent = ''; }
  if (recordId) {
    const rec = S.cfDnsRecords.find(r => r.id === recordId);
    if (!rec) return;
    S.cfRecordModalMode = 'edit';
    S.cfRecordEditId = rec.id;
    if (titleEl) titleEl.textContent = t('cf_record_modal_edit', 'Edit DNS Record');
    if (typeEl) typeEl.value = rec.type || 'A';
    if (nameEl) nameEl.value = rec.name || '';
    if (contentEl) contentEl.value = rec.content || '';
    if (ttlEl) ttlEl.value = String(rec.ttl || 1);
    if (prioEl) prioEl.value = rec.priority !== undefined && rec.priority !== null ? rec.priority : '';
    if (proxiedEl) proxiedEl.checked = !!rec.proxied;
    if (commentEl) commentEl.value = rec.comment || '';
  } else {
    S.cfRecordModalMode = 'add';
    S.cfRecordEditId = null;
    if (titleEl) titleEl.textContent = t('cf_record_modal_add', 'Add DNS Record');
    if (typeEl) typeEl.value = 'A';
    if (nameEl) nameEl.value = '';
    if (contentEl) contentEl.value = '';
    if (ttlEl) ttlEl.value = '1';
    if (prioEl) prioEl.value = '';
    if (proxiedEl) proxiedEl.checked = true;
    if (commentEl) commentEl.value = '';
  }
  onCfRecordTypeChange();
  openModal('cfRecordModal');
  setTimeout(() => { if (nameEl) nameEl.focus(); }, 50);
}

async function submitCfRecordModal() {
  if (!S.selectedCfAccountId || !S.currentCfZone) return;
  const type = document.getElementById('cfRecordType')?.value || 'A';
  const name = (document.getElementById('cfRecordName')?.value || '').trim();
  const content = (document.getElementById('cfRecordContent')?.value || '').trim();
  const ttl = parseInt(document.getElementById('cfRecordTTL')?.value || '1', 10) || 1;
  const prioVal = document.getElementById('cfRecordPriority')?.value;
  const proxied = !!document.getElementById('cfRecordProxied')?.checked;
  const comment = (document.getElementById('cfRecordComment')?.value || '').trim();
  const errEl = document.getElementById('cfRecordError');
  const submitBtn = document.getElementById('btnCfRecordSubmit');
  const setError = (msg) => {
    if (!errEl) return;
    errEl.textContent = msg;
    errEl.style.display = msg ? 'block' : 'none';
  };
  setError('');
  if (!type || !name || !content) {
    setError(t('cf_err_record_validation', 'Record type, name, and content are required'));
    return;
  }
  const payload = { type, name, content, ttl };
  if (type === 'A' || type === 'AAAA' || type === 'CNAME') {
    payload.proxied = proxied;
  }
  if (type === 'MX') {
    if (prioVal === '' || isNaN(parseInt(prioVal, 10))) {
      setError('Priority is required for MX records');
      return;
    }
    payload.priority = parseInt(prioVal, 10);
  }
  if (comment) payload.comment = comment;

  const restore = SP.busy(submitBtn, t('working', 'Working...'));
  try {
    const url = S.cfRecordModalMode === 'edit'
      ? `/api/cloudflare/accounts/${S.selectedCfAccountId}/zones/${S.currentCfZone.id}/dns/${S.cfRecordEditId}`
      : `/api/cloudflare/accounts/${S.selectedCfAccountId}/zones/${S.currentCfZone.id}/dns`;
    const method = S.cfRecordModalMode === 'edit' ? 'PUT' : 'POST';
    const res = await fetch(url, {
      method,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
      setError(cfErrMsg(data, 'Server error', 'dns'));
      return;
    }
    closeModal('cfRecordModal');
    showToast(t('cf_record_saved', 'DNS record saved successfully'), 'success');
    await loadCfDnsRecords();
  } catch (e) {
    setError(e.message || 'Network error');
  } finally {
    restore();
  }
}

async function deleteCfRecord(recordId, btn) {
  if (!S.selectedCfAccountId || !S.currentCfZone) return;
  const rec = S.cfDnsRecords.find(r => r.id === recordId);
  const rName = rec ? `${rec.type} ${rec.name}` : `#${recordId}`;
  const confirmed = await showConfirm({
    title: t('cf_confirm_delete_record_title', 'Delete DNS Record'),
    message: t('cf_confirm_delete_record_msg', 'Are you sure you want to delete {type} record "{name}"? This will take effect on Cloudflare nameservers immediately.').replace('{type}', rec ? rec.type : '').replace('{name}', rec ? rec.name : rName),
    confirmText: t('delete', 'Delete'),
    danger: true
  });
  if (!confirmed) return;
  const restore = SP.busy(btn, t('cf_deleting_record', 'Deleting...'));
  try {
    const res = await fetch(`/api/cloudflare/accounts/${S.selectedCfAccountId}/zones/${S.currentCfZone.id}/dns/${recordId}`, { method: 'DELETE' });
    const data = await res.json().catch(() => ({}));
    if (!res.ok || !data.ok) {
      showToast(t('error_prefix', 'Error: ') + cfErrMsg(data, 'Failed to delete record', 'dns'), 'error');
      return;
    }
    showToast(t('cf_record_deleted', 'DNS record deleted'), 'info');
    await loadCfDnsRecords();
  } catch (e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error');
  } finally {
    restore();
  }
}

async function submitCfPurgeEverything() {
  if (!S.selectedCfAccountId || !S.currentCfZone) return;
  const confirmed = await showConfirm({
    title: t('cf_confirm_purge_everything_title', 'Purge Everything'),
    message: t('cf_confirm_purge_everything_msg', 'Purge all cached resources for zone "{name}"? Origin servers may experience higher load.').replace('{name}', S.currentCfZone.name || ''),
    confirmText: t('cf_btn_purge_everything', 'Purge Everything'),
    danger: true
  });
  if (!confirmed) return;
  const btn = document.getElementById('btnCfPurgeEverything');
  const restore = SP.busy(btn, t('cf_purging', 'Purging...'));
  try {
    const res = await fetch(`/api/cloudflare/accounts/${S.selectedCfAccountId}/zones/${S.currentCfZone.id}/purge`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ purge_everything: true })
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok || !data.ok) {
      showToast(t('error_prefix', 'Error: ') + cfErrMsg(data, 'Purge failed', 'cache'), 'error');
      return;
    }
    showToast(t('cf_cache_purged', 'Cache purged successfully'), 'success');
  } catch (e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error');
  } finally {
    restore();
  }
}

async function submitCfPurgeUrls() {
  if (!S.selectedCfAccountId || !S.currentCfZone) return;
  const raw = (document.getElementById('cfPurgeUrlsTextarea')?.value || '').trim();
  if (!raw) {
    showToast(t('cf_err_purge_urls_empty', 'Please enter at least one URL to purge'), 'warning');
    return;
  }
  const files = raw.split(/\r?\n/).map(s => s.trim()).filter(s => s.length > 0);
  if (files.length === 0) {
    showToast(t('cf_err_purge_urls_empty', 'Please enter at least one URL to purge'), 'warning');
    return;
  }
  if (files.length > 30) {
    showToast(t('cf_err_purge_urls_limit', 'Maximum 30 URLs can be purged per request'), 'warning');
    return;
  }
  const btn = document.getElementById('btnCfPurgeUrls');
  const restore = SP.busy(btn, t('cf_purging', 'Purging...'));
  try {
    const res = await fetch(`/api/cloudflare/accounts/${S.selectedCfAccountId}/zones/${S.currentCfZone.id}/purge`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ files })
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok || !data.ok) {
      showToast(t('error_prefix', 'Error: ') + cfErrMsg(data, 'Purge failed', 'cache'), 'error');
      return;
    }
    showToast(t('cf_cache_purged', 'Cache purged successfully'), 'success');
    const ta = document.getElementById('cfPurgeUrlsTextarea');
    if (ta) ta.value = '';
  } catch (e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error');
  } finally {
    restore();
  }
}

async function loadCfZoneSettings() {
  if (!S.selectedCfAccountId || !S.currentCfZone) return;
  const container = document.getElementById('cfSettingsContainer');
  if (container) container.innerHTML = `<div style="text-align:center;padding:20px;color:var(--text-muted);">${t('loading', 'Loading...')}</div>`;
  try {
    const res = await fetch(`/api/cloudflare/accounts/${S.selectedCfAccountId}/zones/${S.currentCfZone.id}/settings`);
    const data = await res.json();
    if (!res.ok) {
      setCfPerm('cfZonePerm', 'settings', data);
      if (!data.forbidden) showToast(t('error_prefix', 'Error: ') + (data.error || 'Failed to load settings'), 'error');
      S.cfZoneSettings = [];
      S.cfZoneSettingsOptions = {};
    } else {
      setCfPerm('cfZonePerm', null);
      S.cfZoneSettings = Array.isArray(data.settings) ? data.settings : [];
      S.cfZoneSettingsOptions = data.options || {};
    }
  } catch (e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error');
    S.cfZoneSettings = [];
    S.cfZoneSettingsOptions = {};
  }
  renderCfZoneSettings();
}

function cfOptionLabel(v) {
  return String(v).replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
}

function renderCfSettingRow(s) {
  const allowed = S.cfZoneSettingsOptions[s.id] || [];
  const cur = String(s.value !== undefined && s.value !== null ? s.value : '');
  const editable = s.editable !== false && allowed.length > 0;
  const isToggle = editable && allowed.length === 2 && allowed.includes('on') && allowed.includes('off');
  const id = jsArg(s.id);
  const attention = (s.id === 'development_mode' && cur === 'on') || (s.id === 'security_level' && cur === 'under_attack') || (s.id === 'ssl' && (cur === 'off' || cur === 'flexible'));
  let ctl;
  if (isToggle) {
    ctl = `<label class="switch" style="margin:0;">
      <input type="checkbox" ${cur === 'on' ? 'checked' : ''} onchange="updateCfZoneSetting(${id}, this.checked ? 'on' : 'off', this)" />
      <span class="switch-track"><span class="switch-thumb"></span></span>
      <span class="switch-label" style="min-width:64px;">${escapeHtml(cur === 'on' ? t('cf_set_on', 'On') : t('cf_set_off', 'Off'))}</span>
    </label>`;
  } else if (editable) {
    ctl = `<select class="form-input" onchange="updateCfZoneSetting(${id}, this.value, this)">
      ${allowed.map(opt => `<option value="${escapeHtml(opt)}" ${opt === cur ? 'selected' : ''}>${escapeHtml(cfOptionLabel(opt))}</option>`).join('')}
    </select>`;
  } else {
    ctl = `<span class="badge" style="background:var(--surface-2);border:1px solid var(--card-border);">${escapeHtml(cfOptionLabel(cur))}</span>`;
  }
  const desc = t('cf_set_' + s.id + '_desc', '');
  return `<div class="cf-set-row">
    <div class="cf-set-info">
      <div class="cf-set-title">${escapeHtml(t('cf_set_' + s.id + '_title', cfOptionLabel(s.id)))}${attention ? `<span class="cf-set-flag">${escapeHtml(t('cf_set_flag_attention', 'Attention'))}</span>` : ''}</div>
      ${desc ? `<div class="cf-set-desc">${escapeHtml(desc)}</div>` : ''}
    </div>
    <div class="cf-set-ctl">${ctl}</div>
  </div>`;
}

function renderCfZoneSettings() {
  const container = document.getElementById('cfSettingsContainer');
  if (!container) return;
  if (S.cfZoneSettings.length === 0) {
    container.innerHTML = `<div class="empty-state" style="padding:20px;"><div class="empty-state-desc">${escapeHtml(t('cf_set_empty', 'No settings available.'))}</div></div>`;
    return;
  }
  const known = new Set(S.CF_SETTING_GROUPS.flatMap(g => g.ids));
  const groups = S.CF_SETTING_GROUPS.map(g => ({ key: g.key, items: g.ids.map(id => S.cfZoneSettings.find(s => s.id === id)).filter(Boolean) }));
  groups.push({ key: 'other', items: S.cfZoneSettings.filter(s => !known.has(s.id)) });
  container.innerHTML = groups.filter(g => g.items.length).map(g => `<div class="cf-set-group">
    <div class="cf-set-group-head">${escapeHtml(t('cf_set_group_' + g.key, g.key))}</div>
    ${g.items.map(renderCfSettingRow).join('')}
  </div>`).join('');
}

async function updateCfZoneSetting(settingId, nextValue, el) {
  if (!S.selectedCfAccountId || !S.currentCfZone) return;
  const ctl = el || null;
  let spinner = null;
  if (ctl) {
    ctl.disabled = true;
    spinner = document.createElement('span');
    spinner.className = 'spinner';
    spinner.style.display = 'inline-block';
    spinner.style.marginLeft = '6px';
    spinner.innerHTML = svgIcon('loader-circle', 'icon-sm');
    if (ctl.parentElement) ctl.parentElement.appendChild(spinner);
  }
  try {
    const res = await fetch(`/api/cloudflare/accounts/${S.selectedCfAccountId}/zones/${S.currentCfZone.id}/settings`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id: settingId, value: nextValue })
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
      showToast(t('error_prefix', 'Error: ') + cfErrMsg(data, 'Failed to update setting', 'settings'), 'error');
      await loadCfZoneSettings();
      return;
    }
    showToast(t('cf_setting_updated', 'Zone Setting updated successfully'), 'success');
    const s = S.cfZoneSettings.find(item => item.id === settingId);
    if (s && data.setting) s.value = data.setting.value;
    renderCfZoneSettings();
  } catch (e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error');
    await loadCfZoneSettings();
  } finally {
    if (spinner && spinner.parentElement) spinner.parentElement.removeChild(spinner);
    if (ctl) ctl.disabled = false;
  }
}

function cfPermHint(kind) {
  const perm = S.CF_PERM[kind];
  if (!perm) return '';
  return t('cf_perm_missing', 'The API token lacks a permission: {perm}. In Cloudflare open My Profile → API Tokens → Edit on this token and add it; the token value stays the same.').replace('{perm}', perm);
}

function cfErrMsg(data, fallback, kind) {
  const msg = (data && data.error) || fallback;
  return data && data.forbidden && kind ? msg + ' — ' + cfPermHint(kind) : msg;
}

function setCfPerm(elId, kind, data, always) {
  const el = document.getElementById(elId);
  if (!el) return;
  const forbidden = !!(data && data.forbidden && kind);
  if (!data || !data.error || (!forbidden && !always)) {
    el.style.display = 'none';
    el.textContent = '';
    return;
  }
  const head = forbidden ? `<div>${escapeHtml(cfPermHint(kind))}</div>` : '';
  el.innerHTML = `${svgIcon('shield-alert', 'icon-sm')}<div>${head}<div class="mono" style="opacity:.85;${forbidden ? 'margin-top:2px;' : ''}">${escapeHtml(data.error)}</div></div>`;
  el.style.display = 'flex';
}

function cfFmtDate(iso) {
  if (!iso) return '-';
  const d = new Date(iso);
  return isNaN(d.getTime()) ? '-' : d.toLocaleString();
}

function fillCfAccessSelects(modes, targets) {
  const fill = (id, list, prefix) => {
    const sel = document.getElementById(id);
    if (!sel) return;
    const prev = sel.value;
    sel.innerHTML = list.map(v => `<option value="${escapeHtml(v)}">${escapeHtml(t(prefix + v, cfOptionLabel(v)))}</option>`).join('');
    if (prev && list.includes(prev)) sel.value = prev;
  };
  fill('cfAccessMode', modes, 'cf_access_mode_');
  fill('cfAccessTarget', targets, 'cf_access_target_');
  onCfAccessTargetChange();
}

function onCfAccessTargetChange() {
  const target = document.getElementById('cfAccessTarget')?.value || 'ip';
  const input = document.getElementById('cfAccessValue');
  if (input) input.placeholder = t('cf_access_ph_' + target, '');
}

async function loadCfAccessRules() {
  if (!S.selectedCfAccountId || !S.currentCfZone) return;
  const tbody = document.getElementById('cfAccessTableBody');
  if (tbody) tbody.innerHTML = `<tr><td colspan="6" style="text-align:center;padding:20px;color:var(--text-muted);">${t('loading', 'Loading...')}</td></tr>`;
  let data = {};
  try {
    const res = await fetch(`/api/cloudflare/accounts/${S.selectedCfAccountId}/zones/${S.currentCfZone.id}/access-rules`);
    data = await res.json().catch(() => ({}));
    if (!res.ok) {
      setCfPerm('cfZonePerm', 'access', data, true);
      S.cfAccessRules = [];
    } else {
      setCfPerm('cfZonePerm', null);
      S.cfAccessRules = Array.isArray(data.rules) ? data.rules : [];
    }
  } catch (e) {
    setCfPerm('cfZonePerm', 'access', { error: e.message }, true);
    S.cfAccessRules = [];
  }
  fillCfAccessSelects(Array.isArray(data.modes) && data.modes.length ? data.modes : S.CF_ACCESS_MODES, Array.isArray(data.targets) && data.targets.length ? data.targets : S.CF_ACCESS_TARGETS);
  renderCfAccessTable();
}

function renderCfAccessTable() {
  const tbody = document.getElementById('cfAccessTableBody');
  const empty = document.getElementById('cfAccessEmptyState');
  if (!tbody || !empty) return;
  const q = (document.getElementById('cfAccessSearchInput')?.value || '').toLowerCase().trim();
  const rows = q ? S.cfAccessRules.filter(r => (r.value || '').toLowerCase().includes(q) || (r.notes || '').toLowerCase().includes(q) || (r.mode || '').toLowerCase().includes(q) || (r.target || '').toLowerCase().includes(q)) : S.cfAccessRules;
  if (rows.length === 0) {
    tbody.innerHTML = '';
    empty.style.display = 'block';
    return;
  }
  empty.style.display = 'none';
  tbody.innerHTML = rows.map(r => {
    const badge = r.mode === 'block' ? 'badge-down' : (r.mode === 'whitelist' ? 'badge-healthy' : 'badge-warning');
    return `<tr>
      <td><span class="badge ${badge}">${escapeHtml(t('cf_access_mode_' + r.mode, cfOptionLabel(r.mode)))}</span></td>
      <td style="font-size:var(--fs-xs);">${escapeHtml(t('cf_access_target_' + r.target, r.target))}</td>
      <td class="mono" style="font-weight:600;word-break:break-all;">${escapeHtml(r.value)}</td>
      <td style="font-size:var(--fs-xs);color:var(--text-muted);">${escapeHtml(r.notes || '-')}</td>
      <td style="font-size:var(--fs-xs);color:var(--text-muted);">${escapeHtml(cfFmtDate(r.created_on))}</td>
      <td style="text-align:right;white-space:nowrap;"><button type="button" class="btn btn-sm btn-ghost btn-icon text-danger" title="Delete" onclick="deleteCfAccessRule(${jsArg(r.id)}, this)">${svgIcon('trash-2', 'icon-sm')}</button></td>
    </tr>`;
  }).join('');
}

async function submitCfAccessRule() {
  if (!S.selectedCfAccountId || !S.currentCfZone) return;
  const mode = document.getElementById('cfAccessMode')?.value || 'block';
  const target = document.getElementById('cfAccessTarget')?.value || 'ip';
  const value = (document.getElementById('cfAccessValue')?.value || '').trim();
  const notes = (document.getElementById('cfAccessNotes')?.value || '').trim();
  if (!value) {
    showToast(t('cf_err_access_value', 'Enter a value for the rule'), 'warning');
    return;
  }
  if (mode === 'block') {
    const confirmed = await showConfirm({
      title: t('cf_confirm_access_add_title', 'Block traffic'),
      message: t('cf_confirm_access_add_msg', 'Block {target} {value} on zone "{zone}"? It takes effect immediately and also applies to you if it matches your own address.')
        .replace('{target}', t('cf_access_target_' + target, target)).replace('{value}', value).replace('{zone}', S.currentCfZone.name || ''),
      confirmText: t('cf_access_mode_block', 'Block'),
      danger: true
    });
    if (!confirmed) return;
  }
  const btn = document.getElementById('btnCfAccessAdd');
  const restore = SP.busy(btn, t('working', 'Working...'));
  try {
    const res = await fetch(`/api/cloudflare/accounts/${S.selectedCfAccountId}/zones/${S.currentCfZone.id}/access-rules`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ mode, target, value, notes })
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
      showToast(t('error_prefix', 'Error: ') + cfErrMsg(data, 'Failed to add rule', 'access'), 'error');
      return;
    }
    const v = document.getElementById('cfAccessValue');
    const n = document.getElementById('cfAccessNotes');
    if (v) v.value = '';
    if (n) n.value = '';
    showToast(t('cf_access_added', 'Access rule added'), 'success');
    await loadCfAccessRules();
  } catch (e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error');
  } finally {
    restore();
  }
}

async function deleteCfAccessRule(ruleId, btn) {
  if (!S.selectedCfAccountId || !S.currentCfZone) return;
  const rule = S.cfAccessRules.find(r => r.id === ruleId);
  const label = rule ? `${t('cf_access_mode_' + rule.mode, rule.mode)} ${rule.value}` : `#${ruleId}`;
  const confirmed = await showConfirm({
    title: t('cf_confirm_access_delete_title', 'Delete access rule'),
    message: t('cf_confirm_access_delete_msg', 'Delete the access rule "{rule}" from zone "{zone}"? It takes effect immediately.').replace('{rule}', label).replace('{zone}', S.currentCfZone.name || ''),
    confirmText: t('delete', 'Delete'),
    danger: true
  });
  if (!confirmed) return;
  const restore = SP.busy(btn, t('cf_deleting_record', 'Deleting...'));
  try {
    const res = await fetch(`/api/cloudflare/accounts/${S.selectedCfAccountId}/zones/${S.currentCfZone.id}/access-rules/${ruleId}`, { method: 'DELETE' });
    const data = await res.json().catch(() => ({}));
    if (!res.ok || !data.ok) {
      showToast(t('error_prefix', 'Error: ') + cfErrMsg(data, 'Failed to delete rule', 'access'), 'error');
      return;
    }
    showToast(t('cf_access_deleted', 'Access rule deleted'), 'info');
    await loadCfAccessRules();
  } catch (e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error');
  } finally {
    restore();
  }
}

async function exportCfDns() {
  if (!S.selectedCfAccountId || !S.currentCfZone) return;
  const btn = document.getElementById('btnCfDnsExport');
  const restore = SP.busy(btn, t('cf_exporting', 'Exporting...'));
  try {
    const res = await fetch(`/api/cloudflare/accounts/${S.selectedCfAccountId}/zones/${S.currentCfZone.id}/dns/export`);
    if (!res.ok) {
      const data = await res.json().catch(() => ({}));
      setCfPerm('cfZonePerm', 'dns_read', data);
      showToast(t('error_prefix', 'Error: ') + cfErrMsg(data, 'Export failed', 'dns_read'), 'error');
      return;
    }
    const blob = await res.blob();
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = (S.currentCfZone.name || S.currentCfZone.id) + '.zone';
    document.body.appendChild(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(a.href), 1000);
    showToast(t('cf_dns_exported', 'DNS records exported'), 'success');
  } catch (e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error');
  } finally {
    restore();
  }
}

function cfTunnelsReset() {
  if (S.cfTunnelsAcc === S.selectedCfAccountId) return;
  S.cfTunnelsAcc = S.selectedCfAccountId;
  S.cfTunnels = [];
  const tbody = document.getElementById('cfTunnelsTableBody');
  if (tbody) tbody.innerHTML = '';
  const cnt = document.getElementById('cfTunnelsCount');
  if (cnt) cnt.textContent = '-';
  const empty = document.getElementById('cfTunnelsEmpty');
  if (empty) empty.style.display = 'none';
  setCfPerm('cfTunnelsPerm', null);
  const det = document.getElementById('cfTunnelsContainer');
  if (det && det.open && S.selectedCfAccountId) loadCfTunnels();
}

function onCfTunnelsToggle(el) {
  if (el.open && S.selectedCfAccountId && !S.cfTunnels.length && !document.getElementById('cfTunnelsTableBody')?.children.length) loadCfTunnels();
}

async function loadCfTunnels() {
  if (!S.selectedCfAccountId) return;
  const accId = S.selectedCfAccountId;
  const tbody = document.getElementById('cfTunnelsTableBody');
  const empty = document.getElementById('cfTunnelsEmpty');
  if (tbody) tbody.innerHTML = `<tr><td colspan="5" style="text-align:center;padding:20px;color:var(--text-muted);">${t('loading', 'Loading...')}</td></tr>`;
  if (empty) empty.style.display = 'none';
  try {
    const res = await fetch(`/api/cloudflare/accounts/${accId}/tunnels`);
    const data = await res.json().catch(() => ({}));
    if (accId !== S.selectedCfAccountId) return;
    if (!res.ok) {
      setCfPerm('cfTunnelsPerm', 'tunnels', data, true);
      S.cfTunnels = [];
    } else {
      setCfPerm('cfTunnelsPerm', null);
      S.cfTunnels = Array.isArray(data.tunnels) ? data.tunnels : [];
    }
  } catch (e) {
    if (accId !== S.selectedCfAccountId) return;
    setCfPerm('cfTunnelsPerm', 'tunnels', { error: e.message }, true);
    S.cfTunnels = [];
  }
  renderCfTunnels();
}

function renderCfTunnels() {
  const tbody = document.getElementById('cfTunnelsTableBody');
  const empty = document.getElementById('cfTunnelsEmpty');
  const cnt = document.getElementById('cfTunnelsCount');
  if (cnt) cnt.textContent = String(S.cfTunnels.length);
  if (!tbody || !empty) return;
  const failed = document.getElementById('cfTunnelsPerm')?.style.display === 'flex';
  if (S.cfTunnels.length === 0) {
    tbody.innerHTML = '';
    empty.style.display = failed ? 'none' : 'block';
    return;
  }
  empty.style.display = 'none';
  const cls = { healthy: 'badge-healthy', degraded: 'badge-degraded', down: 'badge-down', inactive: 'badge-unknown' };
  tbody.innerHTML = S.cfTunnels.map(tn => {
    const st = tn.status || 'inactive';
    const last = st === 'healthy' || st === 'degraded' ? tn.conns_active_at : tn.conns_inactive_at;
    return `<tr>
      <td style="font-weight:600;word-break:break-all;">${escapeHtml(tn.name)}</td>
      <td><span class="badge ${cls[st] || 'badge-unknown'}">${escapeHtml(t('cf_tunnel_status_' + st, st))}</span></td>
      <td class="mono" style="font-size:var(--fs-xs);">${escapeHtml(tn.tun_type || '-')}</td>
      <td style="font-size:var(--fs-xs);color:var(--text-muted);">${escapeHtml(cfFmtDate(tn.created_at))}</td>
      <td style="font-size:var(--fs-xs);color:var(--text-muted);">${escapeHtml(cfFmtDate(last))}</td>
    </tr>`;
  }).join('');
}

for (const [k, v] of Object.entries(S.TF_TXT)) { S.I18N.en[k] = v[0]; S.I18N.id[k] = v[1]; }

export { loadCfAccounts, populateCfAccountSelect, onSelectCfAccount, refreshCfSection, loadCfZones, filterCfZones, renderCfZones, openCfAccountModal, submitCfAccountModal, deleteCfAccount, openCfZoneModal, switchCfZoneTab, refreshCurrentCfZoneTab, loadCfDnsRecords, renderCfDnsTable, toggleCfRecordProxied, onCfRecordTypeChange, openCfRecordModal, submitCfRecordModal, deleteCfRecord, submitCfPurgeEverything, submitCfPurgeUrls, loadCfZoneSettings, cfOptionLabel, renderCfSettingRow, renderCfZoneSettings, updateCfZoneSetting, cfPermHint, cfErrMsg, setCfPerm, cfFmtDate, fillCfAccessSelects, onCfAccessTargetChange, loadCfAccessRules, renderCfAccessTable, submitCfAccessRule, deleteCfAccessRule, exportCfDns, cfTunnelsReset, onCfTunnelsToggle, loadCfTunnels, renderCfTunnels };
