/* i18n, authentication, confirmation dialogs, notes, dashboard toolbar.
 * Native ES module fragment of the former monolithic inline script.
 * Air-gapped: no bundler, no CDN. Shared state lives in the mutable S object
 * from 00-state.js; cross-domain functions are imported explicitly. */
import { S } from './00-state.js';
import { svgIcon, debounce, rafBatch } from './00-foundation.js';
import { renderHosts, loadHosts, loadAlerts, showToast } from './02-hosts.js';
import { switchTab } from './03-workspace-files-editor.js';
import { checkForUpdates, checkFreshStatus, loadTransfers } from './05-settings-transfer.js';
import { openModal, closeModal, updateDocsLangButton, loadDocContent, connectActivityEventSource } from './06-modal-docs-activity.js';
import { isHostStale, setKpiFilter } from './08-metrics-systemd-docker.js';

function t(key, fallback) {
  if (S.I18N[S.currentLang] && S.I18N[S.currentLang][key]) return S.I18N[S.currentLang][key];
  if (S.I18N['en'] && S.I18N['en'][key]) return S.I18N['en'][key];
  return fallback !== undefined ? fallback : key;
}

function setLanguage(lang) {
  S.currentLang = lang === 'id' ? 'id' : 'en';
  localStorage.setItem('pantau_lang', S.currentLang);
  document.documentElement.lang = S.currentLang;

  const btn = document.getElementById('langToggleBtn');
  if (btn) btn.innerHTML = `${svgIcon('globe', 'icon-sm')} <span>${S.currentLang === 'en' ? 'EN' : 'ID'}</span>`;
  const loginBtn = document.getElementById('loginLangBtn');
  if (loginBtn) loginBtn.innerHTML = `${svgIcon('globe', 'icon-sm')} <span>${S.currentLang === 'en' ? 'EN' : 'ID'}</span>`;

  document.querySelectorAll('[data-i18n]').forEach(el => {
    const k = el.getAttribute('data-i18n');
    const v = t(k);
    if (!v) return;
    // Keep a leading sprite icon (and its sibling label span) instead of wiping it with plain text.
    const lead = el.firstElementChild;
    if (lead && lead.tagName.toLowerCase() === 'svg') {
      let label = el.querySelector(':scope > span');
      if (!label) {
        label = document.createElement('span');
        el.appendChild(label);
      }
      label.innerHTML = v;
    } else {
      el.innerHTML = v;
    }
  });
  document.querySelectorAll('[data-i18n-placeholder]').forEach(el => {
    const k = el.getAttribute('data-i18n-placeholder');
    const v = t(k);
    if (v) el.placeholder = v;
  });
  document.querySelectorAll('[data-i18n-title]').forEach(el => {
    const k = el.getAttribute('data-i18n-title');
    const v = t(k);
    if (v) {
      el.title = v;
      el.setAttribute('aria-label', v);
    }
  });

  if (S.currentHost && window.detailData) {
    switchTab(S.currentTab);
  }
  if (document.getElementById('globalNotesModal') && document.getElementById('globalNotesModal').classList.contains('open')) {
    showGlobalNotesViewMode();
  }
  loadHosts();
  if (typeof loadAlerts === 'function') loadAlerts();
  if (typeof loadDailyBriefing === 'function') loadDailyBriefing();
  if (document.getElementById('transfersListModal') && document.getElementById('transfersListModal').classList.contains('open')) {
    loadTransfers();
  }
  if (document.getElementById('documentationModal') && document.getElementById('documentationModal').classList.contains('open')) {
    S.docsModalLang = S.currentLang;
    updateDocsLangButton();
    loadDocContent(S.currentDoc, S.docsModalLang);
  }
  document.querySelectorAll('.footer-badge').forEach(el => {
    if (el.id !== 'footerUpdateBadge') {
      el.title = t('view_changelog', 'View Changelog');
    }
  });
  // Lists that live in later script sections register here once their state exists (setLanguage also runs at boot).
  S.langRerenderHooks.forEach(fn => fn());
}

function toggleLanguage() {
  setLanguage(S.currentLang === 'en' ? 'id' : 'en');
}

async function checkAuth() {
  const res = await fetch('/api/me');
  const data = await res.json();
  if (data.authenticated) {
    document.getElementById('loginView').style.display = 'none';
    document.getElementById('appView').style.display = 'flex';
    initDashboardToolbar();
    loadGlobalNotes();
    loadInspectionSettings();
    loadHosts();
    loadAlerts();
    if (typeof loadDailyBriefing === 'function') loadDailyBriefing();
    loadTransfers();
    setInterval(loadAlerts, 15000);
    setInterval(loadTransfers, 15000);
    setInterval(loadHosts, 30000);
    checkFreshStatus();
    checkInspectAllStatus();
    setTimeout(() => checkForUpdates(false), 2000);
    connectActivityEventSource();
    if (window.AG) window.AG.init();
  } else {
    document.getElementById('loginView').style.display = 'flex';
    document.getElementById('appView').style.display = 'none';
    setTimeout(() => document.getElementById('loginPassword')?.focus(), 50);
  }
}

async function doLogin() {
  const pass = document.getElementById('loginPassword').value;
  document.getElementById('loginErr').innerText = '';
  const res = await fetch('/api/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password: pass })
  });
  if (res.ok) {
    const data = await res.json();
    if (data.status === 'require_2fa') {
      window.loginTempToken = data.temp_token;
      document.getElementById('loginPassGroup').style.display = 'none';
      document.getElementById('loginSubmitBtn').style.display = 'none';
      document.getElementById('login2FAGroup').style.display = 'flex';
      document.getElementById('loginErr').innerText = '';
      setTimeout(() => document.getElementById('login2FACode')?.focus(), 50);
      return;
    }
    checkAuth();
  } else {
    document.getElementById('loginErr').innerText = t('invalid_password', 'Invalid password');
  }
}

async function doLogin2FA() {
  const code = document.getElementById('login2FACode').value.trim();
  if (!code) return;
  document.getElementById('loginErr').innerText = '';
  const res = await fetch('/api/login/2fa', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ temp_token: window.loginTempToken, code: code })
  });
  const data = await res.json();
  if (res.ok && data.status === 'ok') {
    cancel2FALogin();
    checkAuth();
  } else {
    if (data.error === 'too_many_attempts' || data.error === 'invalid_or_expired_token') {
      cancel2FALogin();
      document.getElementById('loginErr').innerText = t('session_expired_retry', 'Session expired or too many attempts. Please enter password again.');
    } else {
      const remaining = data.remaining_attempts !== undefined ? ` (${data.remaining_attempts} ${t('attempts_left', 'attempts remaining')})` : '';
      document.getElementById('loginErr').innerText = (t('invalid_2fa_code', 'Invalid 2FA code or recovery code') + remaining);
    }
  }
}

function cancel2FALogin() {
  window.loginTempToken = null;
  const passGroup = document.getElementById('loginPassGroup');
  const submitBtn = document.getElementById('loginSubmitBtn');
  const group2fa = document.getElementById('login2FAGroup');
  if (passGroup) passGroup.style.display = 'flex';
  if (submitBtn) submitBtn.style.display = 'block';
  if (group2fa) group2fa.style.display = 'none';
  const codeInput = document.getElementById('login2FACode');
  if (codeInput) codeInput.value = '';
  const errEl = document.getElementById('loginErr');
  if (errEl) errEl.innerText = '';
  setTimeout(() => document.getElementById('loginPassword')?.focus(), 50);
}

function showConfirm(options) {
  const opts = typeof options === 'string' ? { message: options } : (options || {});
  return new Promise((resolve) => {
    S.confirmResolver = resolve;
    const titleEl = document.getElementById('confirmModalTitle');
    if (titleEl) titleEl.innerText = opts.title || t('confirm', 'Confirm');
    const msgEl = document.getElementById('confirmModalMessage');
    if (msgEl) msgEl.innerText = opts.message || '';
    const isDanger = opts.danger !== false;
    const btn = document.getElementById('confirmModalBtn');
    if (btn) {
      btn.innerText = opts.confirmText || t('confirm', 'Confirm');
      btn.className = 'btn ' + (isDanger ? 'btn-danger' : 'btn-primary');
    }
    const iconWell = document.getElementById('confirmModalIconWell');
    if (iconWell) {
      iconWell.className = 'confirm-icon-well ' + (isDanger ? 'destructive' : 'default');
      iconWell.innerHTML = isDanger ? svgIcon('triangle-alert', 'icon-lg') : svgIcon('info', 'icon-lg');
    }
    const extraBtn = document.getElementById('confirmModalExtraBtn');
    if (extraBtn) {
      if (opts.extraBtnText) {
        extraBtn.innerText = opts.extraBtnText;
        extraBtn.className = 'btn ' + (opts.extraBtnClass || 'btn-warning');
        extraBtn.style.display = 'inline-block';
        extraBtn.onclick = () => resolveConfirm(opts.extraBtnValue !== undefined ? opts.extraBtnValue : 'extra');
      } else {
        extraBtn.style.display = 'none';
        extraBtn.onclick = null;
      }
    }
    openModal('confirmModal');
  });
}

function resolveConfirm(val) {
  const el = document.getElementById('confirmModal');
  if (el) {
    el.classList.remove('open');
    el.style.zIndex = '';
  }
  S.openModalStack = S.openModalStack.filter(mId => mId !== 'confirmModal');
  const extraBtn = document.getElementById('confirmModalExtraBtn');
  if (extraBtn) extraBtn.style.display = 'none';
  if (S.confirmResolver) {
    const res = S.confirmResolver;
    S.confirmResolver = null;
    res(val);
  }
}

async function doLogout() {
  if (!await showConfirm({ message: t('logout_confirm', 'Are you sure you want to log out?'), confirmText: t('logout', 'Logout') })) return;
  await fetch('/api/logout', { method: 'POST' });
  location.reload();
}

function getUIPrefs() {
  try {
    const raw = localStorage.getItem(S.UI_PREFS_KEY);
    return raw ? JSON.parse(raw) : {};
  } catch(e) {
    return {};
  }
}

function setUIPrefs(updates) {
  try {
    const current = getUIPrefs();
    const next = Object.assign({}, current, updates);
    localStorage.setItem(S.UI_PREFS_KEY, JSON.stringify(next));
  } catch(e) {}
}

async function loadGlobalNotes() {
  try {
    const res = await fetch('/api/notes');
    if (!res.ok) return;
    const data = await res.json();
    S.globalNotesData = data.notes || '';
  } catch(e) {}
}

async function openGlobalNotesModal() {
  await loadGlobalNotes();
  showGlobalNotesViewMode();
  openModal('globalNotesModal');
}

function showGlobalNotesViewMode() {
  const viewEl = document.getElementById('globalNotesModalView');
  const editEl = document.getElementById('globalNotesModalEdit');
  const viewActions = document.getElementById('globalNotesModalViewActions');
  const editActions = document.getElementById('globalNotesModalEditActions');
  const textEl = document.getElementById('globalNotesModalText');

  if (viewEl) viewEl.style.display = 'block';
  if (editEl) editEl.style.display = 'none';
  if (viewActions) viewActions.style.display = 'flex';
  if (editActions) editActions.style.display = 'none';

  if (textEl) {
    if (!S.globalNotesData.trim()) {
      textEl.innerHTML = `<span style="color: var(--text-muted); font-style: italic;">${t('global_note_empty')}</span>`;
    } else {
      textEl.textContent = S.globalNotesData;
    }
  }
}

function startEditGlobalNotesModal() {
  const viewEl = document.getElementById('globalNotesModalView');
  const editEl = document.getElementById('globalNotesModalEdit');
  const viewActions = document.getElementById('globalNotesModalViewActions');
  const editActions = document.getElementById('globalNotesModalEditActions');
  const input = document.getElementById('globalNotesModalInput');

  if (viewEl) viewEl.style.display = 'none';
  if (editEl) editEl.style.display = 'flex';
  if (viewActions) viewActions.style.display = 'none';
  if (editActions) editActions.style.display = 'flex';

  if (input) {
    input.value = S.globalNotesData;
    setTimeout(() => input.focus(), 50);
  }
}

function cancelEditGlobalNotesModal() {
  showGlobalNotesViewMode();
}

async function saveGlobalNotesModal() {
  const input = document.getElementById('globalNotesModalInput');
  const notes = input ? input.value : '';
  try {
    const res = await fetch('/api/notes', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ notes })
    });
    if (res.ok) {
      S.globalNotesData = notes;
      showGlobalNotesViewMode();
      showToast(t('global_note_saved', 'Global note saved successfully'), 'success');
    } else {
      showToast(t('failed_save_global_note', 'Failed to save global note'), 'error');
    }
  } catch(e) {
    showToast(t('failed_save_global_note', 'Failed to save global note') + ': ' + e.message, 'error');
  }
}

function openEditNoteModal(hostId, currentNote, hostName) {
  document.getElementById('editNoteHostId').value = hostId;
  document.getElementById('editNoteHostNameLabel').innerText = hostName ? `${t('col_name')}: ${hostName}` : t('host_note');
  document.getElementById('editNoteContent').value = currentNote || '';
  openModal('hostNoteModal');
  setTimeout(() => document.getElementById('editNoteContent').focus(), 50);
}

async function saveHostNote() {
  const hostId = document.getElementById('editNoteHostId').value;
  const notes = document.getElementById('editNoteContent').value;
  if (!hostId) return;

  try {
    const res = await fetch(`/api/hosts/${hostId}/notes`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ notes })
    });
    if (res.ok) {
      closeModal('hostNoteModal');
      if (window.detailData && window.detailData.host && window.detailData.host.id == hostId) {
        window.detailData.host.notes = notes;
        if (S.currentTab === 'overview') switchTab('overview');
      }
      showToast(t('host_note_saved', 'Host note saved successfully'), 'success');
      loadHosts();
    } else {
      showToast(t('failed_save_host_note', 'Failed to save host note'), 'error');
    }
  } catch(e) {
    showToast(t('failed_save_host_note', 'Failed to save host note') + ': ' + e.message, 'error');
  }
}

function initDashboardToolbar() {
  const prefs = getUIPrefs();
  const searchInput = document.getElementById('hostSearchInput');
  const statusSelect = document.getElementById('statusFilterSelect');
  const sortSelect = document.getElementById('sortSelect');
  const clearBtn = document.getElementById('clearSearchBtn');

  if (statusSelect && prefs.statusFilter) statusSelect.value = prefs.statusFilter;
  if (sortSelect && prefs.sortBy) sortSelect.value = prefs.sortBy;
  updateViewModeButtons(prefs.viewMode || 'list');
}

function setViewMode(mode) {
  setUIPrefs({ viewMode: mode });
  updateViewModeButtons(mode);
  renderHosts();
}

function updateViewModeButtons(mode) {
  const gridBtn = document.getElementById('viewGridBtn');
  const listBtn = document.getElementById('viewListBtn');
  if (gridBtn && listBtn) {
    if (mode === 'list') {
      gridBtn.classList.remove('active');
      listBtn.classList.add('active');
    } else {
      gridBtn.classList.add('active');
      listBtn.classList.remove('active');
    }
  }
}

function onSearchFilterChange() {
  const searchVal = document.getElementById('hostSearchInput').value;
  const clearBtn = document.getElementById('clearSearchBtn');
  if (clearBtn) clearBtn.style.display = searchVal ? 'inline-flex' : 'none';

  const statusVal = document.getElementById('statusFilterSelect').value;
  const sortVal = document.getElementById('sortSelect').value;
  setUIPrefs({ statusFilter: statusVal, sortBy: sortVal });

  document.querySelectorAll('.kpi-tile').forEach(tile => tile.classList.remove('active'));
  const activeTile = document.querySelector(`.kpi-tile[onclick="setKpiFilter('${statusVal}')"]`);
  if (activeTile) activeTile.classList.add('active');

  rafBatch(renderHosts);
}

// Debounced wrapper for the text input; select/filter changes call onSearchFilterChange directly.
const onSearchInput = debounce(onSearchFilterChange, 120);

function clearSearch() {
  const searchInput = document.getElementById('hostSearchInput');
  if (searchInput) searchInput.value = '';
  onSearchFilterChange();
}

function hostStateClass(status) {
  const s = (status || 'unknown').toLowerCase();
  if (s === 'healthy') return 'healthy';
  if (s === 'down') return 'down';
  if (s === 'degraded' || s === 'drift') return 'degraded';
  return 'unknown';
}

function getFilteredAndSortedHosts() {
  const searchVal = (document.getElementById('hostSearchInput')?.value || '').toLowerCase().trim();
  const groupVal = document.getElementById('groupFilterSelect')?.value || 'all';
  const statusVal = document.getElementById('statusFilterSelect')?.value || 'all';
  const sortVal = document.getElementById('sortSelect')?.value || 'manual';

  let list = [...S.allRawHosts];

  if (groupVal && groupVal !== 'all') {
    if (groupVal === '__ungrouped__') {
      list = list.filter(h => !(h.group_name || '').trim());
    } else {
      list = list.filter(h => (h.group_name || '').trim() === groupVal);
    }
  }

  if (statusVal && statusVal !== 'all') {
    if (statusVal.toLowerCase() === 'stale') {
      list = list.filter(h => isHostStale(h));
    } else {
      list = list.filter(h => (h.status || '').toLowerCase() === statusVal.toLowerCase());
    }
  }
  if (searchVal) {
    list = list.filter(h => {
      const name = (h.name || '').toLowerCase();
      const host = (h.host || '').toLowerCase();
      const os = (h.os_info || '').toLowerCase();
      const notes = (h.notes || '').toLowerCase();
      const group = (h.group_name || '').toLowerCase();
      return name.includes(searchVal) || host.includes(searchVal) || os.includes(searchVal) || notes.includes(searchVal) || group.includes(searchVal);
    });
  }

  if (sortVal === 'name_asc') {
    list.sort((a, b) => (a.name || '').localeCompare(b.name || ''));
  } else if (sortVal === 'name_desc') {
    list.sort((a, b) => (b.name || '').localeCompare(a.name || ''));
  } else if (sortVal === 'score_desc') {
    list.sort((a, b) => (b.lifecycle_score || 0) - (a.lifecycle_score || 0));
  } else if (sortVal === 'score_asc') {
    list.sort((a, b) => (a.lifecycle_score || 0) - (b.lifecycle_score || 0));
  } else if (sortVal === 'status') {
    const statusWeight = { down: 1, degraded: 2, unknown: 3, healthy: 4 };
    list.sort((a, b) => (statusWeight[a.status] || 5) - (statusWeight[b.status] || 5));
  } else {
    list.sort((a, b) => (a.sort_order || 0) - (b.sort_order || 0) || (a.id - b.id));
  }

  return list;
}

async function reorderHostsAPI(ids) {
  try {
    const res = await fetch('/api/hosts/reorder', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ids })
    });
    if (!res.ok) {
      console.error('Failed to persist host order');
    }
  } catch(e) {
    console.error('Error persisting host order:', e);
  }
}

function handleDropReorder(srcId, targetId) {
  const srcIdx = S.allRawHosts.findIndex(item => item.id == srcId);
  const targetIdx = S.allRawHosts.findIndex(item => item.id == targetId);
  if (srcIdx === -1 || targetIdx === -1) return;

  const [moved] = S.allRawHosts.splice(srcIdx, 1);
  S.allRawHosts.splice(targetIdx, 0, moved);

  S.allRawHosts.forEach((item, idx) => {
    item.sort_order = idx + 1;
  });

  renderHosts();

  const orderedIds = S.allRawHosts.map(item => item.id);
  reorderHostsAPI(orderedIds);
}

function updateInspectionStatusUI() {
  const isPaused = S.currentPollInterval === 0;
  const badge = document.getElementById('inspectionPausedBadge');
  if (badge) {
    badge.style.display = isPaused ? 'inline-flex' : 'none';
  }
  const engineTxt = document.getElementById('footerEngineText');
  if (engineTxt && S.lastEngineStatus) {
    if (isPaused) {
      engineTxt.innerHTML = `${t('footer_engine_active', 'Engine Active')} <span style="color: var(--warning); font-size: 0.72rem; margin-left: 4px;">(${t('inspection_paused_footer', 'Inspection Paused')})</span>`;
    } else {
      engineTxt.innerText = t('footer_engine_active', 'Engine Active');
    }
  }
}

async function loadInspectionSettings() {
  try {
    const res = await fetch('/api/settings');
    if (res.ok) {
      const data = await res.json();
      const val = parseInt(data.poll_interval_sec !== undefined ? data.poll_interval_sec : '300', 10);
      S.currentPollInterval = isNaN(val) ? 300 : val;
      updateInspectionStatusUI();
    }
  } catch (e) {}
}

function formatRelativeTime(isoStr) {
  if (!isoStr) return { relative: t('never_inspected', 'Never'), title: t('never_inspected', 'Never'), isStale: (S.currentPollInterval !== 0) };
  const d = new Date(isoStr);
  if (isNaN(d.getTime())) return { relative: '-', title: '-', isStale: false };
  const diffSec = Math.floor((Date.now() - d.getTime()) / 1000);
  const title = d.toLocaleString();
  // ponytail: 600s (10 minutes) threshold marks telemetry as stale when background inspection is active.
  // Suppress stale warning if background inspection is intentionally disabled (interval = 0).
  const isStale = (S.currentPollInterval !== 0) && (diffSec > 600);

  if (diffSec < 10) return { relative: t('just_now', 'just now'), title, isStale: false };
  if (diffSec < 60) return { relative: `${diffSec}s ` + t('ago', 'ago'), title, isStale: false };
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return { relative: `${diffMin}m ` + t('ago', 'ago'), title, isStale };
  const diffHours = Math.floor(diffMin / 60);
  if (diffHours < 24) return { relative: `${diffHours}h ` + t('ago', 'ago'), title, isStale };
  const diffDays = Math.floor(diffHours / 24);
  return { relative: `${diffDays}d ` + t('ago', 'ago'), title, isStale };
}

function setInspectAllBanner(on) {
  const grid = document.getElementById('hostGrid');
  if (!grid) return;
  let el = document.getElementById('inspectAllBanner');
  if (on) {
    if (!el) {
      el = document.createElement('div');
      el.id = 'inspectAllBanner';
      grid.parentNode.insertBefore(el, grid);
    }
    el.innerHTML = SP.banner(t('inspect_all_banner', 'Inspecting all hosts…'), S.inspectAllStartT0);
  } else if (el) {
    el.remove();
  }
}

function setInspectAllRunning(running) {
  const btn = document.getElementById('btnInspectAll');
  const icon = document.getElementById('inspectAllIcon');
  if (btn) btn.disabled = running;
  if (icon) {
    if (running) icon.classList.add('spinning');
    else icon.classList.remove('spinning');
  }
  if (running) {
    if (!S.inspectAllStartT0) S.inspectAllStartT0 = Date.now();
    setInspectAllBanner(true);
  } else {
    S.inspectAllStartT0 = 0;
    setInspectAllBanner(false);
  }
}

async function checkInspectAllStatus() {
  try {
    const res = await fetch('/api/hosts/inspect-all');
    if (!res.ok) return;
    const data = await res.json();
    if (data.in_flight) {
      setInspectAllRunning(true);
      if (!S.inspectAllPollingInterval) {
        startInspectAllPolling();
      }
    }
  } catch(e) {}
}

function startInspectAllPolling() {
  if (S.inspectAllPollingInterval) clearInterval(S.inspectAllPollingInterval);
  const startTime = Date.now();
  S.inspectAllPollingInterval = setInterval(async () => {
    try {
      const sRes = await fetch('/api/hosts/inspect-all');
      if (sRes.ok) {
        const sData = await sRes.json();
        if (!sData.in_flight || Date.now() - startTime > 60000) {
          clearInterval(S.inspectAllPollingInterval);
          S.inspectAllPollingInterval = null;
          setInspectAllRunning(false);
          loadHosts();
          showToast(t('inspect_all_done', 'All hosts inspection completed'), 'success');
        }
      }
    } catch (err) {
      clearInterval(S.inspectAllPollingInterval);
      S.inspectAllPollingInterval = null;
      setInspectAllRunning(false);
    }
  }, 1500);
}

async function inspectAllHosts() {
  const count = (S.allRawHosts && S.allRawHosts.length) || 0;
  const msg = t('inspect_all_confirm_msg', 'Pantau will trigger parallel SSH inspections on {count} registered hosts to update Actual State and detect Drift. Continue?').replace('{count}', count);
  const confirmed = await showConfirm({
    title: t('inspect_all_confirm_title', 'Inspect All Hosts'),
    message: msg,
    confirmText: t('inspect_all_confirm_btn', 'Start Inspection'),
    danger: false
  });
  if (!confirmed) return;

  setInspectAllRunning(true);
  showToast(t('inspect_all_running', 'Starting inspection for all hosts...'), 'info');
  let polling = false;
  try {
    const res = await fetch('/api/hosts/inspect-all', { method: 'POST' });
    const data = await res.json();
    if (res.status === 409 || (data && data.in_flight)) {
      showToast(data.error || t('inspect_in_flight', 'Inspection is already in progress for one or more hosts'), 'warning');
      setInspectAllRunning(true);
      polling = true;
      startInspectAllPolling();
      return;
    }
    if (res.status === 429 || (data && data.cooldown)) {
      showToast(data.error || t('inspect_cooldown', 'Hosts were recently inspected. Please wait a moment.'), 'warning');
      setInspectAllRunning(false);
      return;
    }
    if (!res.ok || !data.ok) {
      showToast((data && data.error) || t('inspect_all_failed', 'Failed to trigger inspection: '), 'error');
      setInspectAllRunning(false);
      return;
    }

    showToast(t('inspect_all_success', `Inspection triggered for ${data.count || ''} hosts`), 'success');
    polling = true;
    startInspectAllPolling();
  } catch (e) {
    showToast(t('inspect_all_failed', 'Failed to trigger inspection: ') + e.message, 'error');
  } finally {
    // The polling owns the spinner and the banner once it started; otherwise reset them here.
    if (!polling) setInspectAllRunning(false);
  }
}

async function savePollInterval() {
  const input = document.getElementById('settingPollInterval');
  if (!input) return;
  const sec = parseInt(input.value, 10);
  if (isNaN(sec) || (sec !== 0 && sec < 30)) {
    showToast(t('min_interval_error', 'Minimum inspection interval is 30 seconds (or 0 to disable)'), 'error');
    return;
  }
  try {
    const res = await fetch('/api/settings', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ poll_interval_sec: String(sec) })
    });
    if (res.ok) {
      S.currentPollInterval = sec;
      updateInspectionStatusUI();
      renderHosts();
      showToast(t('settings_saved', 'Settings saved successfully'), 'success');
    } else {
      showToast(t('save_failed', 'Failed to save settings'), 'error');
    }
  } catch (e) {
    showToast(t('save_failed', 'Failed to save settings') + ': ' + e.message, 'error');
  }
}

async function saveSecurityScanInterval() {
  const input = document.getElementById('settingSecurityScanInterval');
  if (!input) return;
  const hours = parseInt(input.value, 10);
  if (isNaN(hours) || hours < 0 || hours > 720) {
    showToast(t('sec_scan_interval_error', 'Security scan interval must be between 0 and 720 hours (0 = disabled)'), 'error');
    return;
  }
  try {
    const res = await fetch('/api/settings', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ security_scan_interval_hours: String(hours) })
    });
    if (res.ok) {
      showToast(t('settings_saved', 'Settings saved successfully'), 'success');
    } else {
      showToast(t('save_failed', 'Failed to save settings'), 'error');
    }
  } catch (e) {
    showToast(t('save_failed', 'Failed to save settings') + ': ' + e.message, 'error');
  }
}

async function saveVulnIntelSetting(on) {
  try {
    const res = await fetch('/api/settings', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ vuln_intel_enabled: on ? '1' : '0' })
    });
    showToast(res.ok ? t('settings_saved', 'Settings saved successfully') : t('save_failed', 'Failed to save settings'), res.ok ? 'success' : 'error');
  } catch (e) {
    showToast(t('save_failed', 'Failed to save settings') + ': ' + e.message, 'error');
  }
}

async function saveOsvEnabledSetting(on) {
  try {
    const res = await fetch('/api/settings', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ osv_enabled: on ? '1' : '0' })
    });
    showToast(res.ok ? t('settings_saved', 'Settings saved successfully') : t('save_failed', 'Failed to save settings'), res.ok ? 'success' : 'error');
  } catch (e) {
    showToast(t('save_failed', 'Failed to save settings') + ': ' + e.message, 'error');
  }
}

async function saveIacGateSetting(on) {
  try {
    const res = await fetch('/api/settings', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ iac_gate_enabled: on ? '1' : '0' })
    });
    showToast(res.ok ? t('settings_saved', 'Settings saved successfully') : t('save_failed', 'Failed to save settings'), res.ok ? 'success' : 'error');
  } catch (e) {
    showToast(t('save_failed', 'Failed to save settings') + ': ' + e.message, 'error');
  }
}

export { t, setLanguage, toggleLanguage, checkAuth, doLogin, doLogin2FA, cancel2FALogin, showConfirm, resolveConfirm, doLogout, getUIPrefs, setUIPrefs, loadGlobalNotes, openGlobalNotesModal, showGlobalNotesViewMode, startEditGlobalNotesModal, cancelEditGlobalNotesModal, saveGlobalNotesModal, openEditNoteModal, saveHostNote, initDashboardToolbar, setViewMode, updateViewModeButtons, onSearchFilterChange, onSearchInput, clearSearch, hostStateClass, getFilteredAndSortedHosts, reorderHostsAPI, handleDropReorder, updateInspectionStatusUI, loadInspectionSettings, formatRelativeTime, setInspectAllBanner, setInspectAllRunning, checkInspectAllStatus, startInspectAllPolling, inspectAllHosts, savePollInterval, saveSecurityScanInterval, saveVulnIntelSetting, saveOsvEnabledSetting, saveIacGateSetting };
