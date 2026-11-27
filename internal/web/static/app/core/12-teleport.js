/* Teleport: profiles, interactive tsh login, the Kubernetes cluster switcher, the expiry banner and palette commands.
 * Every request goes to /api/teleport/* on this origin, with the session cookie. Events are delegated from data-tp
 * attributes, so there are no inline handlers and no CDN. The terminal is the vendored xterm.js. */
import { S } from './00-state.js';
import { t } from './01-i18n-auth.js';

const STATE_KEY = 'pantau_teleport_cluster_state';
const STATUS_REFRESH_MS = 60000;
const PILL_LIMIT = 5;
const PALETTE_CLUSTER_LIMIT = 60;

const TP = {
  profiles: [],
  tsh: null,
  status: {},
  inited: false,
  login: null,
  dropdownOpen: false,
  busyId: null,
  statusTimer: null,
  editingId: null,
  cli: null,        // GET /api/teleport/cli/status
  cliCheck: null,   // last POST /api/teleport/cli/check-update answer
  cliBusy: false,   // a check or an install is running
  cliLog: '',       // install log, kept so a repaint does not lose it
};

const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

// tt translates with named {placeholders}.
function tt(key, fallback, vars) {
  return t(key, fallback).replace(/\{(\w+)\}/g, (_, k) => (vars && vars[k] !== undefined ? String(vars[k]) : ''));
}

function notify(message, type) {
  if (typeof window.showToast === 'function') window.showToast(message, type || 'info');
}

// ---------- API ----------

async function apiJSON(method, url, body) {
  const init = { method, headers: {} };
  if (body !== undefined) {
    init.headers['Content-Type'] = 'application/json';
    init.body = JSON.stringify(body);
  }
  const res = await fetch(url, init);
  let data = {};
  try { data = await res.json(); } catch (_) { data = {}; }
  return { ok: res.ok, status: res.status, data };
}

async function tpLoadProfiles() {
  const r = await apiJSON('GET', '/api/teleport/profiles');
  if (r.ok) {
    TP.profiles = Array.isArray(r.data.profiles) ? r.data.profiles : [];
    TP.tsh = r.data.tsh || null;
  }
  return TP.profiles;
}

async function tpStatusFor(profileId) {
  if (!profileId) return null;
  const r = await apiJSON('GET', `/api/teleport/profiles/${profileId}/status`);
  if (r.ok) TP.status[profileId] = r.data;
  else if (r.status === 503) TP.status[profileId] = { active: false, error: r.data.error };
  renderSessionBadge();
  return TP.status[profileId] || null;
}

// handleFailure reports a Teleport API failure. An expired session shows the Re-Login banner for its profile.
function handleFailure(data, fallbackProfileId) {
  if (data && data.code === 'TELEPORT_SESSION_EXPIRED') {
    showExpiredBanner(data.teleport_profile_id || fallbackProfileId);
    return;
  }
  if (data && data.code === 'TSH_NOT_INSTALLED') {
    notify(tt('tp_tsh_missing', 'tsh is not installed on this server'), 'error');
    return;
  }
  notify((data && data.error) || tt('tp_failed', 'Teleport request failed'), 'error');
}

// Kube requests that fail with an expired Teleport session are answered 401 with a code. The interceptor only reads
// the response (a clone), so every kube call keeps its own handling.
const originalFetch = window.fetch;
window.fetch = function teleportAwareFetch(input, init) {
  const p = originalFetch.apply(this, arguments);
  const url = typeof input === 'string' ? input : (input && input.url) || '';
  if (url.indexOf('/api/kube/') !== -1) {
    p.then((res) => {
      if (res.status !== 401) return;
      res.clone().json().then((d) => {
        if (d && d.code === 'TELEPORT_SESSION_EXPIRED') showExpiredBanner(d.teleport_profile_id);
      }).catch(() => {});
    }).catch(() => {});
  }
  return p;
};

// ---------- cluster state (namespace and tab per cluster, kept across reloads) ----------

function rememberClusterState() {
  try {
    const out = {};
    for (const [k, v] of Object.entries(S.kubeWsState || {})) {
      if (Number(k) < 0 && v) out[k] = { tab: v.tab, ns: v.ns };
    }
    if (S.selectedKubeHostId && Number(S.selectedKubeHostId) < 0) {
      out[S.selectedKubeHostId] = { tab: S.currentKubeTab, ns: S.currentKubeNamespace };
    }
    localStorage.setItem(STATE_KEY, JSON.stringify(out));
  } catch (_) { /* storage can be unavailable in private windows: the state is then per session only */ }
}

function restoreClusterState() {
  try {
    const saved = JSON.parse(localStorage.getItem(STATE_KEY) || '{}');
    S.kubeWsState = S.kubeWsState || {};
    for (const [k, v] of Object.entries(saved)) {
      if (Number(k) < 0 && v && typeof v === 'object') S.kubeWsState[k] = { tab: v.tab, ns: v.ns || '' };
    }
  } catch (_) { /* a damaged value is ignored */ }
}

// ---------- switcher ----------

function clusterById(id) {
  return (S.kubeClusters || []).find((c) => Number(c.id) === Number(id)) || null;
}

function profileName(profileId) {
  const p = TP.profiles.find((x) => Number(x.id) === Number(profileId));
  return p ? p.name : '';
}

function envLabel(badge) {
  return badge ? tt('tp_env_' + badge, badge) : '';
}

function envBadge(badge) {
  if (!badge) return '';
  return `<span class="tp-env tp-env-${esc(badge)}">${esc(envLabel(badge))}</span>`;
}

function policyLabel(mode) {
  return tt('tp_policy_' + String(mode || 'full').replace('-', ''), mode || 'full');
}

function pinnedClusters() {
  const list = (S.kubeClusters || []).slice();
  list.sort((a, b) => {
    if (!!b.is_favorite !== !!a.is_favorite) return a.is_favorite ? -1 : 1;
    const ua = a.last_used_at ? Date.parse(a.last_used_at) : 0;
    const ub = b.last_used_at ? Date.parse(b.last_used_at) : 0;
    return ub - ua;
  });
  return list.filter((c) => c.is_favorite || c.last_used_at).slice(0, PILL_LIMIT);
}

function renderPills() {
  const el = document.getElementById('tpPills');
  if (!el) return;
  const selected = Number(S.selectedKubeHostId) || 0;
  const pills = pinnedClusters();
  if (!pills.length) {
    el.innerHTML = `<span class="tp-muted">${esc(tt('tp_pinned_empty', 'Star a cluster to pin it here'))}</span>`;
    return;
  }
  el.innerHTML = pills.map((c) => {
    const busy = TP.busyId === c.id ? ' tp-busy' : '';
    const on = selected === -c.id ? ' tp-on' : '';
    return `<span class="tp-pill${on}${busy}">`
      + `<button type="button" class="tp-pill-main" data-tp="switch" data-id="${esc(c.id)}" title="${esc(c.name)}">`
      + `${esc(c.name)} ${envBadge(c.env_badge)}</button>`
      + `<button type="button" class="tp-star" data-tp="fav" data-id="${esc(c.id)}" aria-pressed="${c.is_favorite ? 'true' : 'false'}"`
      + ` title="${esc(tt('tp_favorite', 'Favorite'))}">${c.is_favorite ? '★' : '☆'}</button>`
      + `</span>`;
  }).join('');
}

// groupedMatches builds the dropdown: teleport clusters by profile, external kubeconfig clusters, then SSH hosts.
function groupedMatches(query) {
  const q = String(query || '').trim().toLowerCase();
  const match = (name) => !q || String(name || '').toLowerCase().indexOf(q) !== -1;
  const groups = new Map();
  const add = (title, item) => {
    if (!groups.has(title)) groups.set(title, []);
    groups.get(title).push(item);
  };
  for (const c of S.kubeClusters || []) {
    if (!match(c.name)) continue;
    if (c.source_type === 'teleport') {
      add(tt('tp_group_teleport', 'Teleport: {name}', { name: profileName(c.teleport_profile_id) || '?' }), { kind: 'cluster', c });
    } else {
      add(tt('tp_group_external', 'External Kubeconfig'), { kind: 'cluster', c });
    }
  }
  for (const h of S.kubeHostClusters || []) {
    if (!match(h.name)) continue;
    add(tt('tp_group_ssh', 'SSH k3s/RKE2'), { kind: 'host', h });
  }
  return groups;
}

function renderDropdown() {
  const el = document.getElementById('tpDropdown');
  if (!el) return;
  const input = document.getElementById('tpSearch');
  const groups = groupedMatches(input ? input.value : '');
  if (!groups.size) {
    el.innerHTML = `<div class="tp-muted tp-pad">${esc(tt('tp_no_match', 'No cluster matches'))}</div>`;
    return;
  }
  let html = '';
  for (const [title, items] of groups) {
    html += `<div class="tp-group">${esc(title)}</div>`;
    for (const it of items) {
      if (it.kind === 'cluster') {
        const c = it.c;
        const sess = c.source_type === 'teleport' ? sessionChip(c.teleport_profile_id) : '';
        html += `<button type="button" class="tp-item" data-tp="switch" data-id="${esc(c.id)}">`
          + `<span>${esc(c.name)}</span> ${envBadge(c.env_badge)}`
          + `<span class="tp-meta">${esc(policyLabel(c.kube_mode))}</span>${sess}</button>`;
      } else {
        html += `<button type="button" class="tp-item" data-tp="switch-host" data-id="${esc(it.h.id)}">`
          + `<span>${esc(it.h.name)}</span></button>`;
      }
    }
  }
  el.innerHTML = html;
}

function sessionChip(profileId) {
  const st = TP.status[profileId];
  if (!st) return '<span class="tp-meta">—</span>';
  const cls = sessionClass(st);
  return `<span class="tp-meta tp-sess-${cls}">${esc(sessionText(st))}</span>`;
}

// sessionClass: green above one hour, yellow below, red when expired or absent.
function sessionClass(st) {
  if (!st || !st.active || st.expired) return 'red';
  return st.ttl_seconds > 3600 ? 'green' : 'yellow';
}

function sessionText(st) {
  if (!st || !st.active) return tt('tp_session_none', 'Not logged in');
  if (st.expired) return tt('tp_session_expired', 'Session expired');
  const mins = Math.floor((st.ttl_seconds || 0) / 60);
  const h = Math.floor(mins / 60);
  return tt('tp_session_ttl', 'Session {ttl}', { ttl: h > 0 ? `${h}h ${mins % 60}m` : `${mins}m` });
}

function renderSessionBadge() {
  const el = document.getElementById('tpSessionBadge');
  if (!el) return;
  const c = clusterById(-(Number(S.selectedKubeHostId) || 0));
  if (!c || c.source_type !== 'teleport') {
    el.hidden = true;
    return;
  }
  const st = TP.status[c.teleport_profile_id];
  el.hidden = false;
  el.className = `tp-badge tp-sess-${sessionClass(st)}`;
  el.innerHTML = `${esc(profileName(c.teleport_profile_id))}: ${esc(sessionText(st))}`
    + (st && st.expired || !st || !st.active
      ? ` <button type="button" class="btn btn-sm" data-tp="relogin" data-id="${esc(c.teleport_profile_id)}">${esc(tt('tp_relogin', 'Re-Login'))}</button>`
      : '');
}

function showExpiredBanner(profileId) {
  const el = document.getElementById('tpBanner');
  if (!el) return;
  el.hidden = false;
  el.innerHTML = `<span>${esc(tt('tp_banner_expired', 'Teleport session expired — click Re-Login'))}</span>`
    + ` <button type="button" class="btn btn-sm" data-tp="relogin" data-id="${esc(profileId || 0)}">${esc(tt('tp_relogin', 'Re-Login'))}</button>`;
  if (profileId) {
    TP.status[profileId] = { active: false, expired: true, ttl_seconds: 0, profile_id: profileId };
    renderSessionBadge();
  }
}

function hideExpiredBanner() {
  const el = document.getElementById('tpBanner');
  if (el) { el.hidden = true; el.innerHTML = ''; }
}

// renderTeleportSwitcher draws the sticky bar above the cluster bar of the Kubernetes tab. It runs after every
// cluster load, so the pills, the dropdown and the badge follow the current data.
function renderTeleportSwitcher() {
  const anchor = document.getElementById('kubeClusterBar');
  if (!anchor || !anchor.parentNode) return;
  init();
  if (!document.getElementById('tpSwitcher')) {
    const bar = document.createElement('div');
    bar.id = 'tpSwitcher';
    bar.className = 'tp-switcher';
    bar.innerHTML = `<div class="tp-row">`
      + `<div class="tp-pills" id="tpPills"></div>`
      + `<div class="tp-search-wrap">`
      + `<input id="tpSearch" class="tp-search" type="search" autocomplete="off" data-tp="search"`
      + ` placeholder="${esc(tt('tp_search', 'Search clusters…'))}" aria-label="${esc(tt('tp_search', 'Search clusters…'))}">`
      + `<div id="tpDropdown" class="tp-dropdown" hidden></div></div>`
      + `<span id="tpSessionBadge" class="tp-badge" hidden></span>`
      + `<button type="button" class="btn btn-sm btn-ghost" data-tp="profiles">${esc(tt('tp_profiles_btn', 'Teleport Profiles'))}</button>`
      + `</div><div id="tpBanner" class="tp-banner" hidden></div>`;
    anchor.parentNode.insertBefore(bar, anchor);
  }
  renderPills();
  renderSessionBadge();
  if (TP.dropdownOpen) renderDropdown();
}

function openDropdown() {
  const el = document.getElementById('tpDropdown');
  if (!el) return;
  TP.dropdownOpen = true;
  el.hidden = false;
  renderDropdown();
}

function closeDropdown() {
  const el = document.getElementById('tpDropdown');
  TP.dropdownOpen = false;
  if (el) el.hidden = true;
}

// switchToCluster selects a cluster. A Teleport cluster without a context is provisioned first (a spinner marks the
// pill), so the workloads load against a context that exists.
async function switchToCluster(c) {
  closeDropdown();
  if (c.source_type === 'teleport' && !c.kube_context_name) {
    TP.busyId = c.id;
    renderPills();
    const r = await apiJSON('POST', `/api/teleport/clusters/${c.id}/ensure`);
    TP.busyId = null;
    if (!r.ok) {
      handleFailure(r.data, c.teleport_profile_id);
      renderPills();
      return;
    }
    c.kube_context_name = r.data.kube_context_name || c.kube_context_name;
  }
  if (c.source_type === 'teleport') {
    apiJSON('POST', `/api/teleport/clusters/${c.id}/used`).catch(() => {});
    tpStatusFor(c.teleport_profile_id);
  }
  rememberClusterState();
  await window.onSelectKubeHost(-c.id);
  rememberClusterState();
  renderTeleportSwitcher();
}

// ---------- profiles drawer ----------

function profileFormHTML(p) {
  const v = (x) => esc(x || '');
  const pol = p && p.default_policy ? p.default_policy : 'read-only';
  const opt = (val, label) => `<option value="${val}"${pol === val ? ' selected' : ''}>${esc(label)}</option>`;
  return `<form class="tp-form" data-tp-form="${p ? esc(p.id) : 'new'}">`
    + `<label>${esc(tt('tp_name', 'Name'))}<input name="name" required maxlength="64" value="${v(p && p.name)}"></label>`
    + `<label>${esc(tt('tp_proxy', 'Proxy'))}<input name="proxy" required placeholder="teleport.example.com:443" value="${v(p && p.proxy)}"></label>`
    + `<label>${esc(tt('tp_auth', 'Auth connector'))}<input name="auth_connector" placeholder="local | oidc" value="${v(p && p.auth_connector)}"></label>`
    + `<label>${esc(tt('tp_user', 'Username'))}<input name="username" value="${v(p && p.username)}"></label>`
    + `<label>${esc(tt('tp_cluster', 'Teleport cluster'))}<input name="teleport_cluster" value="${v(p && p.teleport_cluster)}"></label>`
    + `<label>${esc(tt('tp_home', 'TELEPORT_HOME (optional)'))}<input name="teleport_home" placeholder="~/.tsh-prod" value="${v(p && p.teleport_home)}"></label>`
    + `<label>${esc(tt('tp_policy', 'Default policy'))}<select name="default_policy">`
    + opt('read-only', policyLabel('read-only')) + opt('no-exec', policyLabel('no-exec')) + opt('full', policyLabel('full'))
    + `</select></label>`
    + `<div class="tp-form-actions"><button type="submit" class="btn btn-sm">${esc(tt('tp_save', 'Save'))}</button>`
    + `<button type="button" class="btn btn-sm btn-ghost" data-tp="form-cancel">${esc(tt('tp_cancel', 'Cancel'))}</button></div>`
    + `</form>`;
}

function profilesListHTML() {
  const rows = TP.profiles.map((p) => {
    const st = TP.status[p.id];
    return `<tr><td>${esc(p.name)}</td><td>${esc(p.proxy)}</td><td>${esc(p.teleport_cluster)}</td>`
      + `<td>${esc(policyLabel(p.default_policy))}</td><td>${sessionChip(p.id)}</td><td class="tp-actions">`
      + `<button class="btn btn-sm" data-tp="relogin" data-id="${esc(p.id)}">${esc(st && st.active && !st.expired ? tt('tp_login_again', 'Login again') : tt('tp_login', 'Login'))}</button>`
      + `<button class="btn btn-sm btn-ghost" data-tp="sync" data-id="${esc(p.id)}">${esc(tt('tp_sync', 'Sync Clusters'))}</button>`
      + `<button class="btn btn-sm btn-ghost" data-tp="sync-all" data-id="${esc(p.id)}">${esc(tt('tp_sync_all', 'Sync All Contexts'))}</button>`
      + `<button class="btn btn-sm btn-ghost" data-tp="logout" data-id="${esc(p.id)}">${esc(tt('tp_logout', 'Logout'))}</button>`
      + `<button class="btn btn-sm btn-ghost" data-tp="edit" data-id="${esc(p.id)}">${esc(tt('tp_edit', 'Edit'))}</button>`
      + `<button class="btn btn-sm btn-ghost tp-danger" data-tp="delete" data-id="${esc(p.id)}">${esc(tt('tp_delete', 'Delete'))}</button>`
      + `</td></tr>`;
  }).join('');
  const table = TP.profiles.length
    ? `<table class="tp-table"><thead><tr><th>${esc(tt('tp_name', 'Name'))}</th><th>${esc(tt('tp_proxy', 'Proxy'))}</th>`
      + `<th>${esc(tt('tp_cluster', 'Teleport cluster'))}</th><th>${esc(tt('tp_policy', 'Default policy'))}</th>`
      + `<th>${esc(tt('tp_session', 'Session'))}</th><th></th></tr></thead><tbody>${rows}</tbody></table>`
    : `<p class="tp-muted">${esc(tt('tp_none', 'No Teleport profiles yet.'))}</p>`;
  return `<div id="tpCliCard" class="tp-cli-card">${cliCardInner()}</div>` + table
    + `<div class="tp-form-actions"><button class="btn btn-sm" data-tp="new">${esc(tt('tp_new', 'New profile'))}</button></div>`;
}

// ---------- Teleport CLI (tsh) card: ADR 0080 (check) and ADR 0081 (managed install) ----------

async function loadCLIStatus() {
  const r = await apiJSON('GET', '/api/teleport/cli/status');
  if (r.ok) TP.cli = r.data;
  return TP.cli;
}

// The profile whose proxy version the check reads: the one with a live session, otherwise the first one.
function profileForCLICheck() {
  const active = TP.profiles.find((p) => TP.status[p.id] && TP.status[p.id].active);
  return active || TP.profiles[0] || null;
}

function cliSourceLabel(source) {
  return { env: tt('tp_cli_src_env', 'Env'), managed: tt('tp_cli_src_managed', 'Managed'), system: tt('tp_cli_src_system', 'System') }[source] || '';
}

function cliResultHTML(c) {
  if (!c) return '';
  if (c.status === 'check_failed') {
    return `<p class="tp-warn" role="status">${esc(tt('tp_cli_check_failed', 'Check failed: {error}', { error: c.error || '' }))}</p>`;
  }
  let main = '';
  if (c.status === 'update_available') main = tt('tp_cli_update_available', 'Update available: {installed} → {latest}', { installed: c.installed || '-', latest: c.latest });
  else if (c.status === 'up_to_date') main = tt('tp_cli_up_to_date', 'Up to date (v{version})', { version: c.installed || c.latest });
  else if (c.status === 'not_installed') main = tt('tp_cli_status_not_installed', 'tsh is not installed on this server. Install it to use Teleport profiles.');
  else main = tt('tp_cli_status_unknown', 'The installed version cannot be compared with the latest release.');
  const parts = [`<p class="tp-cli-main">${esc(main)}</p>`];
  if (c.major_version_mismatch && c.proxy && c.proxy.version) {
    parts.push(`<p class="tp-warn">${esc(tt('tp_cli_major_mismatch', 'Major version differs from the proxy (v{proxy}). Pick a version that matches the proxy.', { proxy: c.proxy.version }))}</p>`);
  }
  if (c.proxy && c.proxy.error) {
    parts.push(`<p class="tp-muted">${esc(tt('tp_cli_proxy_error', 'The proxy version could not be read: {error}', { error: c.proxy.error }))}</p>`);
  }
  return parts.join('');
}

// cliCardInner renders the card body. The install log lives in TP.cliLog, so a repaint keeps it.
function cliCardInner() {
  const tsh = (TP.cli && TP.cli.tsh) || {};
  const installed = !!tsh.installed;
  const check = TP.cliCheck;
  const busy = TP.cliBusy;
  const proxyVersion = check && check.proxy && check.proxy.version ? check.proxy.version : '';
  const latest = check && check.latest ? check.latest : '';

  const badge = installed
    ? `<span class="badge badge-healthy">${esc(cliSourceLabel(tsh.source))}</span>`
    : `<span class="badge badge-danger">${esc(tt('tp_cli_not_installed', 'Not Installed'))}</span>`;
  const kv = (label, value) => `<div class="tp-cli-kv"><span>${esc(label)}</span><strong>${value}</strong></div>`;

  const rows = [
    kv(tt('tp_cli_version', 'Installed version'), esc(installed ? (tsh.version || tt('tp_cli_unknown_version', 'unknown')) : '-')),
  ];
  if (installed && tsh.path) rows.push(kv(tt('tp_cli_path', 'Path'), `<code>${esc(tsh.path)}</code>`));
  rows.push(kv(tt('tp_cli_proxy_version', 'Proxy version'), esc(proxyVersion || tt('tp_cli_no_profile', 'Add a Teleport profile to see its proxy version.'))));

  const override = tsh.override_active
    ? `<p class="tp-warn">${esc(tt('tp_cli_override', 'PANTAU_TSH_BIN is set on this server, so Pantau runs that binary and not the managed tsh. Installing here does not change what runs.'))}</p>`
    : '';

  // Install is offered when tsh is missing, when a newer release exists, or when the major version differs from the proxy.
  const needsInstall = !installed || (check && (check.status === 'update_available' || check.status === 'not_installed' || check.major_version_mismatch));
  // "Match Proxy" is offered when the proxy runs a release other than the latest and other than what is installed.
  const matchProxy = proxyVersion && proxyVersion !== latest && proxyVersion !== (tsh.version || '');

  const actions = [
    `<button type="button" class="btn btn-sm" data-tp="cli-check"${busy ? ' disabled' : ''}>${esc(busy ? tt('tp_cli_checking', 'Checking…') : tt('tp_cli_check', 'Check for updates'))}</button>`,
  ];
  if (needsInstall) {
    actions.push(`<button type="button" class="btn btn-sm btn-primary" data-tp="cli-install" data-version="${esc(latest)}"${busy ? ' disabled' : ''}>${esc(tt('tp_cli_install', 'Install / Update now'))}</button>`);
  }
  if (matchProxy) {
    actions.push(`<button type="button" class="btn btn-sm" data-tp="cli-install" data-version="${esc(proxyVersion)}"${busy ? ' disabled' : ''}>${esc(tt('tp_cli_match', 'Match Proxy v{version}', { version: proxyVersion }))}</button>`);
  }
  if (check && check.release_url) {
    actions.push(`<a class="tp-link" href="${esc(check.release_url)}" target="_blank" rel="noopener noreferrer">${esc(tt('tp_cli_release_notes', 'Release notes'))}</a>`);
  }

  return `<div class="tp-cli-head"><strong>${esc(tt('tp_cli_title', 'Teleport CLI (tsh)'))}</strong> ${badge}</div>`
    + `<div class="tp-cli-kvs">${rows.join('')}</div>`
    + override
    + `<div class="tp-cli-result">${cliResultHTML(check)}</div>`
    + `<div class="tp-cli-actions">${actions.join('')}</div>`
    + `<pre class="tp-cli-log" id="tpCliLog"${TP.cliLog ? '' : ' hidden'}>${esc(TP.cliLog)}</pre>`
    + `<p class="tp-muted">${esc(tt('tp_cli_hint', 'Check reads only: it contacts the Teleport release site and the proxy, and installs nothing. Install downloads the release, verifies its SHA-256 and only then replaces the managed tsh.'))}</p>`;
}

function renderCLICard() {
  const el = document.getElementById('tpCliCard');
  if (el) el.innerHTML = cliCardInner();
}

function appendCLILog(text) {
  TP.cliLog += text;
  const el = document.getElementById('tpCliLog');
  if (el) {
    el.hidden = false;
    el.textContent = TP.cliLog;
    el.scrollTop = el.scrollHeight;
  }
}

// checkCLIUpdate runs POST /api/teleport/cli/check-update. A failed request is shown as check_failed, never as up to date.
async function checkCLIUpdate() {
  if (TP.cliBusy) return;
  TP.cliBusy = true;
  renderCLICard();
  try {
    const profile = profileForCLICheck();
    const r = await apiJSON('POST', '/api/teleport/cli/check-update', profile ? { profile_id: profile.id } : {});
    TP.cliCheck = r.ok ? r.data : { status: 'check_failed', error: (r.data && r.data.error) || tt('tp_failed', 'Teleport request failed') };
  } catch (e) {
    TP.cliCheck = { status: 'check_failed', error: e.message };
  } finally {
    TP.cliBusy = false;
    renderCLICard();
  }
}

// readInstallStream reads the NDJSON answer of the install: {"log"} lines, then {"done":true[,"error"]}.
async function readInstallStream(body) {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buf = '';
  let failure = '';
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });
    let nl = buf.indexOf('\n');
    while (nl >= 0) {
      const line = buf.slice(0, nl).trim();
      buf = buf.slice(nl + 1);
      nl = buf.indexOf('\n');
      if (!line) continue;
      let msg = null;
      try { msg = JSON.parse(line); } catch (_) { continue; }
      if (msg.log) appendCLILog(msg.log);
      if (msg.done && msg.error) failure = msg.error;
    }
  }
  return failure;
}

// installCLI runs POST /api/teleport/cli/install. An empty version installs the newest release.
async function installCLI(version) {
  if (TP.cliBusy) return;
  TP.cliBusy = true;
  TP.cliLog = '';
  renderCLICard();
  let failure = '';
  try {
    const res = await fetch('/api/teleport/cli/install', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(version ? { version } : {}),
    });
    if (res.status === 409) {
      failure = tt('tp_cli_busy', 'An install is already running');
    } else if (!res.ok) {
      let d = {};
      try { d = await res.json(); } catch (_) { d = {}; }
      failure = d.error || tt('tp_failed', 'Teleport request failed');
    } else {
      failure = await readInstallStream(res.body);
    }
  } catch (e) {
    failure = e.message;
  }
  TP.cliBusy = false;
  await loadCLIStatus();
  if (failure) {
    appendCLILog(`${tt('tp_cli_install_failed', 'The install failed')}: ${failure}\n`);
    notify(failure, 'error');
    renderCLICard();
    return;
  }
  notify(tt('tp_cli_installed_ok', 'tsh {version} is installed', { version: (TP.cli && TP.cli.tsh && TP.cli.tsh.version) || '' }), 'success');
  await checkCLIUpdate();
}

function ensureOverlay(id, title) {
  let el = document.getElementById(id);
  if (!el) {
    el = document.createElement('div');
    el.id = id;
    el.className = 'modal';
    el.setAttribute('role', 'dialog');
    el.setAttribute('aria-modal', 'true');
    el.innerHTML = `<div class="modal-content modal-dialog tp-dialog">`
      + `<div class="modal-header"><h3 data-tp-title></h3>`
      + `<button type="button" class="btn btn-sm btn-ghost" data-tp="close" aria-label="${esc(tt('tp_close', 'Close'))}">×</button></div>`
      + `<div class="modal-body" data-tp-body></div></div>`;
    document.body.appendChild(el);
  }
  el.querySelector('[data-tp-title]').textContent = title;
  return el;
}

async function openProfiles() {
  await tpLoadProfiles();
  await loadCLIStatus();
  const el = ensureOverlay('tpProfilesModal', tt('tp_title', 'Teleport Profiles'));
  el.querySelector('[data-tp-body]').innerHTML = profilesListHTML();
  for (const p of TP.profiles) tpStatusFor(p.id);
  if (typeof window.openModal === 'function') window.openModal('tpProfilesModal');
  else el.classList.add('open');
}

function openProfileForm(id) {
  const p = id ? TP.profiles.find((x) => Number(x.id) === Number(id)) : null;
  TP.editingId = p ? p.id : null;
  const el = ensureOverlay('tpProfilesModal', tt('tp_title', 'Teleport Profiles'));
  el.querySelector('[data-tp-body]').innerHTML = profileFormHTML(p);
}

async function saveProfileForm(form) {
  const fd = new FormData(form);
  const body = {};
  for (const [k, v] of fd.entries()) body[k] = String(v).trim();
  const id = form.getAttribute('data-tp-form');
  const r = id && id !== 'new'
    ? await apiJSON('PUT', `/api/teleport/profiles/${id}`, body)
    : await apiJSON('POST', '/api/teleport/profiles', body);
  if (!r.ok) {
    notify((r.data && r.data.error) || tt('tp_failed', 'Teleport request failed'), 'error');
    return;
  }
  notify(tt('tp_saved', 'Profile saved'), 'success');
  await tpLoadProfiles();
  openProfiles();
}

async function syncProfile(profileId, withContexts) {
  const r = await apiJSON('POST', `/api/teleport/profiles/${profileId}/sync`, { sync_contexts: !!withContexts });
  if (!r.ok) {
    handleFailure(r.data, profileId);
    return null;
  }
  const added = (r.data.added || []).length;
  const removed = (r.data.removed || []).length;
  const warn = r.data.context_errors ? ' — ' + tt('tp_some_contexts_failed', 'some contexts failed') : '';
  notify(tt('tp_sync_done', 'Clusters synced: +{added} / −{removed}', { added, removed }) + warn, r.data.context_errors ? 'warning' : 'success');
  if (typeof window.loadKubeClusters === 'function') await window.loadKubeClusters();
  return r.data;
}

async function logoutProfile(profileId) {
  if (!window.confirm(tt('tp_confirm_logout', 'Log out of this profile and remove its isolated kubeconfig?'))) return;
  const r = await apiJSON('POST', `/api/teleport/profiles/${profileId}/logout`, { clean_kubeconfig: true });
  if (!r.ok) {
    handleFailure(r.data, profileId);
    return;
  }
  notify(tt('tp_logged_out', 'Logged out'), 'success');
  TP.status[profileId] = { active: false, profile_id: profileId };
  if (typeof window.loadKubeClusters === 'function') await window.loadKubeClusters();
  openProfiles();
}

async function deleteProfile(profileId) {
  if (!window.confirm(tt('tp_confirm_delete', 'Delete this profile and its clusters?'))) return;
  const r = await apiJSON('DELETE', `/api/teleport/profiles/${profileId}`);
  if (!r.ok) {
    notify((r.data && r.data.error) || tt('tp_failed', 'Teleport request failed'), 'error');
    return;
  }
  delete TP.status[profileId];
  await tpLoadProfiles();
  if (typeof window.loadKubeClusters === 'function') await window.loadKubeClusters();
  openProfiles();
}

async function syncAllProfiles() {
  await tpLoadProfiles();
  for (const p of TP.profiles) await syncProfile(p.id, false);
}

// ---------- interactive login (tsh login in a PTY) ----------

function closeLogin() {
  const L = TP.login;
  if (!L) return;
  TP.login = null;
  window.removeEventListener('resize', L.onResize);
  try { if (L.ws && L.ws.readyState <= 1) L.ws.close(); } catch (_) { /* already closing */ }
  try { L.term.dispose(); } catch (_) { /* already disposed */ }
  const el = document.getElementById('tpLoginModal');
  if (el) {
    if (typeof window.closeModal === 'function') window.closeModal('tpLoginModal');
    else el.classList.remove('open');
  }
}

function setLoginStatus(text, cls) {
  const el = document.getElementById('tpLoginStatus');
  if (!el) return;
  el.textContent = text;
  el.className = 'tp-login-status ' + (cls || '');
}

function handleLoginControl(m, profileId) {
  if (!m || typeof m !== 'object') return;
  if (m.type === 'exit') {
    if (m.success) {
      setLoginStatus(tt('tp_login_ok', 'Login succeeded. Syncing clusters…'), 'tp-ok');
      syncProfile(profileId, false).then(() => {
        tpStatusFor(profileId);
        hideExpiredBanner();
      });
    } else {
      setLoginStatus(tt('tp_login_fail', 'Login failed (exit {code}).', { code: m.code }), 'tp-bad');
    }
    return;
  }
  if (m.type === 'error') {
    const text = m.code === 'TELEPORT_LOGIN_IN_PROGRESS'
      ? tt('tp_login_busy', 'A login for this profile is already running.')
      : (m.error || tt('tp_failed', 'Teleport request failed'));
    setLoginStatus(text, 'tp-bad');
  }
}

// openLogin opens the login modal. The terminal streams the tsh prompts (password, MFA, SSO link). Keystrokes go
// as binary frames and the size as a text frame. Nothing typed or shown is logged or stored.
function openLogin(profileId) {
  closeLogin();
  const p = TP.profiles.find((x) => Number(x.id) === Number(profileId));
  const el = ensureOverlay('tpLoginModal', tt('tp_login_title', 'tsh login — {name}', { name: p ? p.name : '' }));
  el.querySelector('[data-tp-body]').innerHTML = `<div id="tpLoginTerm" class="tp-login-term"></div>`
    + `<p id="tpLoginStatus" class="tp-login-status"></p>`;
  if (typeof window.openModal === 'function') window.openModal('tpLoginModal');
  else el.classList.add('open');

  if (typeof window.Terminal !== 'function' || !window.FitAddon) {
    setLoginStatus(tt('tp_no_terminal', 'The terminal library did not load.'), 'tp-bad');
    return;
  }
  const term = new window.Terminal({ cursorBlink: true, convertEol: true, fontSize: 13, scrollback: 2000 });
  const fit = new window.FitAddon.FitAddon();
  term.loadAddon(fit);
  term.open(document.getElementById('tpLoginTerm'));
  fit.fit();

  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const url = `${proto}//${location.host}/api/teleport/profiles/${profileId}/login/ws?cols=${term.cols}&rows=${term.rows}`;
  const ws = new WebSocket(url);
  ws.binaryType = 'arraybuffer';
  const L = { ws, term, fit, profileId, onResize: null };
  TP.login = L;

  ws.onmessage = (ev) => {
    if (typeof ev.data === 'string') {
      let m = null;
      try { m = JSON.parse(ev.data); } catch (_) { return; }
      handleLoginControl(m, profileId);
      return;
    }
    term.write(new Uint8Array(ev.data));
  };
  ws.onclose = () => {
    if (TP.login === L) setLoginStatus(tt('tp_login_closed', 'Connection closed.'), '');
  };
  term.onData((d) => {
    if (ws.readyState === 1) ws.send(new TextEncoder().encode(d));
  });
  L.onResize = () => {
    fit.fit();
    if (ws.readyState === 1) ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
  };
  window.addEventListener('resize', L.onResize);
  setLoginStatus(tt('tp_login_hint', 'Follow the prompts in the terminal.'), '');
}

// ---------- events ----------

function onClick(e) {
  const btn = e.target.closest('[data-tp]');
  if (!btn) {
    if (TP.dropdownOpen && !e.target.closest('.tp-search-wrap')) closeDropdown();
    return;
  }
  const action = btn.getAttribute('data-tp');
  const id = Number(btn.getAttribute('data-id')) || 0;
  switch (action) {
    case 'switch': {
      const c = clusterById(id);
      if (c) switchToCluster(c);
      break;
    }
    case 'switch-host':
      closeDropdown();
      window.onSelectKubeHost(id);
      break;
    case 'fav':
      toggleFavorite(id);
      break;
    case 'profiles':
      openProfiles();
      break;
    case 'relogin':
      openLogin(id);
      break;
    case 'sync':
      syncProfile(id, false);
      break;
    case 'sync-all':
      syncProfile(id, true);
      break;
    case 'logout':
      logoutProfile(id);
      break;
    case 'edit':
      openProfileForm(id);
      break;
    case 'cli-check':
      checkCLIUpdate();
      break;
    case 'cli-install':
      installCLI(btn.getAttribute('data-version') || '');
      break;
    case 'new':
      openProfileForm(0);
      break;
    case 'delete':
      deleteProfile(id);
      break;
    case 'form-cancel':
      openProfiles();
      break;
    case 'close': {
      const modal = btn.closest('.modal');
      if (modal && modal.id === 'tpLoginModal') closeLogin();
      else if (modal && typeof window.closeModal === 'function') window.closeModal(modal.id);
      break;
    }
    default:
      break;
  }
}

async function toggleFavorite(id) {
  const c = clusterById(id);
  if (!c) return;
  const r = await apiJSON('PATCH', `/api/teleport/clusters/${id}`, { is_favorite: !c.is_favorite });
  if (r.ok) {
    c.is_favorite = !!r.data.is_favorite;
    renderTeleportSwitcher();
  } else {
    notify((r.data && r.data.error) || tt('tp_failed', 'Teleport request failed'), 'error');
  }
}

function onInput(e) {
  if (e.target && e.target.id === 'tpSearch') {
    if (!TP.dropdownOpen) openDropdown();
    else renderDropdown();
  }
}

function onSubmit(e) {
  const form = e.target.closest && e.target.closest('form[data-tp-form]');
  if (!form) return;
  e.preventDefault();
  saveProfileForm(form);
}

function onFocusSearch(e) {
  if (e.target && e.target.id === 'tpSearch') openDropdown();
}

function init() {
  if (TP.inited) return;
  TP.inited = true;
  restoreClusterState();
  document.addEventListener('click', onClick);
  document.addEventListener('input', onInput);
  document.addEventListener('submit', onSubmit);
  document.addEventListener('focusin', onFocusSearch);
  window.addEventListener('beforeunload', rememberClusterState);
  tpLoadProfiles().then(() => renderTeleportSwitcher());
  TP.statusTimer = setInterval(() => {
    const c = clusterById(-(Number(S.selectedKubeHostId) || 0));
    if (c && c.source_type === 'teleport') tpStatusFor(c.teleport_profile_id);
  }, STATUS_REFRESH_MS);
}

// ---------- palette ----------

// teleportPaletteCommands returns command-palette entries: re-login per profile, sync, and one entry per cluster, so
// that typing "k8s staging" finds that cluster.
function teleportPaletteCommands() {
  const cmds = [];
  const add = (title, sub, icon, run) => cmds.push({ kind: 'command', group: 'commands', title, sub, icon, run });
  for (const p of TP.profiles) {
    add(tt('tp_cmd_relogin', 'Teleport: Re-Login {name}', { name: p.name }), p.proxy, 'plug', () => openLogin(p.id));
  }
  if (TP.profiles.length) add(tt('tp_cmd_sync', 'Teleport: Sync Clusters'), '', 'server', () => syncAllProfiles());
  for (const c of (S.kubeClusters || []).slice(0, PALETTE_CLUSTER_LIMIT)) {
    const sub = c.source_type === 'teleport' ? profileName(c.teleport_profile_id) : 'Kubeconfig';
    const badge = c.env_badge ? ' ' + envLabel(c.env_badge) : '';
    add(tt('tp_cmd_switch', 'k8s {name}', { name: c.name }) + badge, sub, 'boxes', () => switchToCluster(c));
  }
  return cmds;
}

export {
  renderTeleportSwitcher,
  openProfiles,
  openLogin,
  teleportPaletteCommands,
};

// Exposed for the palette, which reads TP through teleportPaletteCommands only.
window.TeleportPalette = { commands: teleportPaletteCommands };
