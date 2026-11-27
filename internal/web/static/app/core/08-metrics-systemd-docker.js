/* KPI strip, systemd, and Docker host-detail views.
 * Native ES module fragment of the former monolithic inline script.
 * Air-gapped: no bundler, no CDN. Shared state lives in the mutable S object
 * from 00-state.js; cross-domain functions are imported explicitly. */
import { S } from './00-state.js';
import { svgIcon } from './00-foundation.js';
import { t, showConfirm, onSearchFilterChange, formatRelativeTime } from './01-i18n-auth.js';
import { showToast, showBusyToast, toggleDropdown, closeAllDropdowns, testHostSSH, setHostActiveTab, renderHosts } from './02-hosts.js';
import { openHostDetail, switchTab, dockerEmptyHtml, dockerTableHtml, dockerChips, loadDockerTab, closeDockerMenu, stopDockerLogs, openDockerLogs, dockerAction } from './03-workspace-files-editor.js';
import { copyTextToClipboard, sendTabCommand, openTerminalForHost } from './04-terminal.js';
import { formatBytes } from './05-settings-transfer.js';
import { openModal, closeModal, escapeHtml } from './06-modal-docs-activity.js';
import { loadKubeClusters, populateKubeHostSelect, stopKubeAutorefresh } from './09-kube.js';
import { loadCfAccounts } from './11-cloudflare.js';

function isHostStale(h) {
  if (S.currentPollInterval === 0) return false;
  if (!h || !h.last_inspected) return true;
  const d = new Date(h.last_inspected);
  if (isNaN(d.getTime())) return true;
  return (Date.now() - d.getTime()) / 1000 > 600;
}

function fleetCounts(list) {
  const hosts = list || [];
  const netById = new Map((S.vpnNetworks || []).map(n => [Number(n.id), n]));
  const networkDown = hosts.filter(h => {
    const net = netById.get(Number(h.network_id));
    if (!net) return false;
    return (net.last_state && net.last_state !== 'up') || !!net.degraded;
  }).length;
  return {
    total: hosts.length,
    healthy: hosts.filter(h => h.status === 'healthy').length,
    drift: hosts.filter(h => h.status === 'degraded' || h.drift).length,
    down: hosts.filter(h => h.status === 'down').length,
    stale: hosts.filter(h => isHostStale(h)).length,
    network_down: networkDown
  };
}

function recordHostTrafficSample(h) {
  if (!h || !h.id) return;
  let hist = S.hostTrafficHistory.get(h.id);
  if (!hist) {
    hist = [];
    S.hostTrafficHistory.set(h.id, hist);
  }
  hist.push({ rx: h.net_rx_speed_bps || 0, tx: h.net_tx_speed_bps || 0 });
  if (hist.length > 30) hist.shift();
}

function getHostTrafficHistory(h) {
  return (h && S.hostTrafficHistory.get(h.id)) || [];
}

function renderSparklineSVG(hist, w = 80, h = 24) {
  if (!hist || hist.length < 2) {
    return `<svg class="host-sparkline-svg" viewBox="0 0 ${w} ${h}" preserveAspectRatio="none" aria-hidden="true"><line x1="2" y1="${h - 3}" x2="${w - 2}" y2="${h - 3}" stroke="var(--card-border)" stroke-width="1.5" stroke-dasharray="3 3" stroke-linecap="round" /></svg>`;
  }
  let maxVal = 1000;
  for (const pt of hist) {
    if (pt.rx > maxVal) maxVal = pt.rx;
    if (pt.tx > maxVal) maxVal = pt.tx;
  }
  const len = hist.length;
  const stepX = len > 1 ? (w - 4) / (len - 1) : w;
  const padY = 2;
  const usableH = h - padY * 2;
  
  const rxPts = hist.map((pt, i) => {
    const x = (2 + i * stepX).toFixed(1);
    const y = (h - padY - (pt.rx / maxVal) * usableH).toFixed(1);
    return `${x},${y}`;
  }).join(' ');
  
  const txPts = hist.map((pt, i) => {
    const x = (2 + i * stepX).toFixed(1);
    const y = (h - padY - (pt.tx / maxVal) * usableH).toFixed(1);
    return `${x},${y}`;
  }).join(' ');

  return `<svg class="host-sparkline-svg" viewBox="0 0 ${w} ${h}" preserveAspectRatio="none" aria-hidden="true"><polyline points="${rxPts}" fill="none" stroke="var(--info)" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" opacity="0.85" /><polyline points="${txPts}" fill="none" stroke="var(--accent-text)" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" opacity="0.85" /></svg>`;
}

function setKpiFilter(status) {
  const select = document.getElementById('statusFilterSelect');
  if (!select) return;
  select.value = status;
  onSearchFilterChange();
}

function renderKpiStrip(hosts) {
  const container = document.getElementById('kpiStrip');
  if (!container) return;
  const list = hosts || S.allRawHosts || [];
  const { total, healthy, drift, down, stale, network_down } = fleetCounts(list);

  let totalRamPct = 0;
  let ramCount = 0;
  let maxDiskPct = 0;
  list.forEach(h => {
    if (h.ram_total_bytes > 0) {
      totalRamPct += (h.ram_used_bytes / h.ram_total_bytes) * 100;
      ramCount++;
    }
    if (h.disk_total_bytes > 0) {
      const dp = Math.round((h.disk_used_bytes / h.disk_total_bytes) * 100);
      if (dp > maxDiskPct) maxDiskPct = dp;
    }
  });
  const avgRam = ramCount > 0 ? Math.round(totalRamPct / ramCount) : 0;
  const currentFilter = document.getElementById('statusFilterSelect')?.value || 'all';

  container.innerHTML = `
    <div class="kpi-tile ${currentFilter === 'all' ? 'active' : ''}" onclick="setKpiFilter('all')" title="${t('filter_all', 'All Status')}">
      <div class="kpi-tile-header">
        <span>${t('kpi_total_hosts', 'Total Hosts')}</span>
        <svg class="icon icon-sm text-muted"><use href="#i-server"/></svg>
      </div>
      <div class="kpi-tile-value">${total}</div>
      <div class="kpi-tile-sub">${t('filter_all', 'All Status')}</div>
    </div>
    <div class="kpi-tile ${currentFilter === 'healthy' ? 'active' : ''}" onclick="setKpiFilter('healthy')" title="${t('filter_healthy', 'Healthy')}">
      <div class="kpi-tile-header">
        <span>${t('kpi_healthy', 'Healthy')}</span>
        <span class="kpi-dot healthy"></span>
      </div>
      <div class="kpi-tile-value" style="color: var(--success);">${healthy}</div>
      <div class="kpi-tile-sub">${total > 0 ? Math.round((healthy / total) * 100) : 0}% ${t('kpi_fleet', 'of fleet')}</div>
    </div>
    <div class="kpi-tile ${currentFilter === 'degraded' ? 'active' : ''}" onclick="setKpiFilter('degraded')" title="${t('filter_degraded', 'Degraded')}">
      <div class="kpi-tile-header">
        <span>${t('kpi_drift', 'Drift')}</span>
        <span class="kpi-dot drift"></span>
      </div>
      <div class="kpi-tile-value" style="color: ${drift > 0 ? 'var(--warning)' : 'var(--text-muted)'};">${drift}</div>
      <div class="kpi-tile-sub">${drift > 0 ? t('kpi_requires_attention', 'Requires attention') : t('kpi_no_drift', 'No drift')}</div>
    </div>
    <div class="kpi-tile ${currentFilter === 'down' ? 'active' : ''}" onclick="setKpiFilter('down')" title="${t('filter_down', 'Down')}">
      <div class="kpi-tile-header">
        <span>${t('kpi_offline', 'Offline')}</span>
        <span class="kpi-dot down"></span>
      </div>
      <div class="kpi-tile-value" style="color: ${down > 0 ? 'var(--danger)' : 'var(--text-muted)'};">${down}</div>
      <div class="kpi-tile-sub">${down > 0 ? t('kpi_unreachable', 'Unreachable') : t('kpi_all_connected', 'All connected')}</div>
    </div>
    <div class="kpi-tile ${currentFilter === 'stale' ? 'active' : ''}" onclick="setKpiFilter('stale')" title="${t('filter_stale', 'Stale')}">
      <div class="kpi-tile-header">
        <span>${t('kpi_stale', 'Stale')}</span>
        <span class="kpi-dot stale"></span>
      </div>
      <div class="kpi-tile-value" style="color: ${stale > 0 ? 'var(--warning)' : 'var(--text-muted)'};">${stale}</div>
      <div class="kpi-tile-sub">${t('kpi_stale_sub', 'Not inspected recently')}</div>
    </div>
    <div class="kpi-tile ${currentFilter === 'network_down' ? 'active' : ''}" onclick="setKpiFilter('network_down')" title="${t('host_network_down', 'Network down')}">
      <div class="kpi-tile-header">
        <span>${t('host_network_down', 'Network down')}</span>
        <span class="kpi-dot drift"></span>
      </div>
      <div class="kpi-tile-value" style="color: ${network_down > 0 ? 'var(--warning)' : 'var(--text-muted)'};">${network_down}</div>
      <div class="kpi-tile-sub">${network_down > 0 ? t('kpi_network_down_sub', 'Tunnel path unavailable') : t('kpi_network_paths_ok', 'All paths up')}</div>
    </div>
    <div class="kpi-tile" onclick="setKpiFilter('all')" title="${t('kpi_resources_title', 'Fleet resource usage')}">
      <div class="kpi-tile-header">
        <span>${t('kpi_avg_ram', 'Avg RAM')}</span>
        <span class="kpi-dot resource"></span>
      </div>
      <div class="kpi-tile-value">${avgRam}%</div>
      <div class="kpi-tile-sub">${t('kpi_max_disk', 'Peak Disk')}: ${maxDiskPct}%</div>
    </div>
  `;
}

function hostSecurityChip(h) {
  const s = h.security;
  if (!s) return '';
  const notAssessed = s.status === 'unsupported' || s.status === 'error';
  const parts = [];
  if (notAssessed) parts.push(t('sec_not_assessed', 'Not assessed'));
  else parts.push(t('sec_chip_pending', '{n} security update(s) pending').replace('{n}', s.pending_security));
  if (s.status === 'partial') parts.push(t('sec_banner_partial', 'Partial Scan Coverage'));
  if (s.reboot_required) parts.push(t('sec_reboot_yes', 'Reboot Required'));
  if (s.hardening_fail > 0) parts.push(t('sec_chip_fail', '{n} hardening check(s) failed').replace('{n}', s.hardening_fail));
  if (s.hardening_warn > 0) parts.push(t('sec_chip_warn', '{n} hardening warning(s)').replace('{n}', s.hardening_warn));
  parts.push(t('sec_chip_scanned', 'scanned {t}').replace('{t}', formatRelativeTime(s.scanned_at).relative));

  let color = 'var(--success)';
  if (notAssessed) color = 'var(--text-muted)';
  else if (s.pending_security > 0 || s.hardening_fail > 0) color = 'var(--danger)';
  else if (s.status === 'partial' || s.reboot_required || s.hardening_warn > 0) color = 'var(--warning)';
  const icon = color === 'var(--success)' ? 'shield-check' : (notAssessed ? 'shield' : 'shield-alert');
  const title = t('tab_security', 'Security') + ': ' + parts.join(' · ');
  const label = notAssessed ? 'N/A' : (s.pending_security > 0 ? String(s.pending_security) : '');
  return `<span class="host-score-chip" onclick="event.stopPropagation(); openHostSecurity(${h.id})" title="${escapeHtml(title)}" style="color: ${color};">${svgIcon(icon, 'icon-sm')}${label ? `<span class="num">${label}</span>` : ''}</span>`;
}

async function openHostSecurity(id) {
  setHostActiveTab(id, 'security');
  await openHostDetail(id);
  if (S.currentHost && S.currentHost.id === id && S.currentTab !== 'security') switchTab('security');
}

function getHostTopIssues(h) {
  const issues = [];
  if (h.status === 'down') {
    issues.push({ level: 'danger', badge: 'Offline', text: 'Host unreachable via SSH' });
  }
  if (Array.isArray(S.latestAlerts)) {
    const alert = S.latestAlerts.find(a => (a.host_id && a.host_id === h.id) || (a.host_name && a.host_name === h.name));
    if (alert) {
      issues.push({ level: 'danger', badge: 'Alert', text: alert.message });
    }
  }
  if (h.drift_rules_count > 0 && issues.length < 2) {
    issues.push({ level: 'warning', badge: 'Drift', text: `${h.drift_rules_count} desired rule(s) violated` });
  }
  if (h.lifecycle_breakdown && issues.length < 2) {
    try {
      const items = typeof h.lifecycle_breakdown === 'string' ? JSON.parse(h.lifecycle_breakdown) : h.lifecycle_breakdown;
      if (Array.isArray(items)) {
        const failed = items.filter(it => it.status === 'fail');
        failed.forEach(f => {
          if (issues.length < 2) {
            issues.push({ level: 'warning', badge: f.name || 'Check', text: f.detail || 'Assessment penalty' });
          }
        });
      }
    } catch(e) {}
  }
  return issues;
}

async function loadSystemdTab(quiet) {
  const host = S.currentHost;
  const content = document.getElementById('tabContent');
  if (!host || !content) return;
  if (!quiet) {
    content.innerHTML = SP.loading(t('systemd_loading', 'Reading services...'));
  }
  let data;
  try {
    const res = await fetch('/api/hosts/' + host.id + '/systemd');
    data = await res.json();
  } catch (e) {
    data = { ok: false, error: e.message };
  }
  if (S.currentTab !== 'systemd' || !S.currentHost || S.currentHost.id !== host.id) return;
  const sameHost = S.systemdState.hostId === host.id;
  S.systemdState = { hostId: host.id, data, filter: sameHost ? S.systemdState.filter : 'all', query: sameHost ? S.systemdState.query : '' };
  renderSystemdTab();
}

function systemdMatches(s, filter, query) {
  if (filter === 'running' && !(s.active === 'active' && s.sub === 'running')) return false;
  if (filter === 'failed' && s.active !== 'failed') return false;
  if (filter === 'inactive' && s.active !== 'inactive') return false;
  if (filter === 'enabled' && s.enabled !== 'enabled') return false;
  if (query) {
    const q = query.toLowerCase();
    return s.name.toLowerCase().includes(q) || (s.description || '').toLowerCase().includes(q);
  }
  return true;
}

function systemdActiveBadge(s) {
  const cls = s.active === 'failed' ? 'badge-danger'
    : (s.active === 'active' ? 'badge-success'
    : (s.active === 'inactive' ? 'badge-unknown' : 'badge-warning'));
  return `<span class="badge ${cls}">${escapeHtml(s.active)}</span>`;
}

function systemdEnabledBadge(s) {
  if (!s.enabled) return '<span class="text-faint">—</span>';
  const cls = s.enabled === 'enabled' ? 'badge-success'
    : (s.enabled === 'masked' ? 'badge-warning'
    : (s.enabled === 'disabled' ? 'badge-unknown' : 'badge-info'));
  return `<span class="badge ${cls}">${escapeHtml(s.enabled)}</span>`;
}

function systemdRowsHtml(services) {
  if (services.length === 0) {
    return `<tr><td colspan="5" style="text-align: center; color: var(--text-muted); padding: 24px;">${t('systemd_empty', 'No services match your filter')}</td></tr>`;
  }
  return services.map(s => {
    const u = escapeHtml(s.name);
    const up = s.active === 'active' || s.active === 'activating' || s.active === 'reloading';
    const canAct = s.load !== 'masked' && s.load !== 'not-found';
    const btn = (action, icon, extra) => `<button class="btn btn-icon btn-sm ${extra || 'btn-ghost'}" data-unit="${u}" onclick="systemdAction(this.dataset.unit, '${action}', this)" title="${escapeHtml(t(action))}">${svgIcon(icon, 'icon-sm')}</button>`;
    const acts = [`<button class="btn btn-icon btn-sm ${s.active === 'failed' ? 'btn-danger' : 'btn-ghost'}" data-unit="${u}" onclick="openSystemdDetail(this.dataset.unit)" title="${escapeHtml(t('systemd_btn_debug'))}">${svgIcon('stethoscope', 'icon-sm')}</button>`];
    if (s.active === 'failed' && window.AG && AG.enabled()) acts.push(`<button class="btn btn-icon btn-sm btn-ghost ag-ask-btn" data-arg="${u}" onclick="AG.askUnit(this.dataset.arg)" title="${escapeHtml(t('ag_ask'))}">${svgIcon('bot', 'icon-sm')}</button>`);
    if (canAct) {
      if (!up) acts.push(btn('start', 'play'));
      acts.push(btn('restart', 'rotate-ccw'));
      if (up) acts.push(btn('stop', 'square'));
    }
    return `
    <tr class="${s.active === 'failed' ? 'sd-row-failed' : ''}">
      <td>
        <a href="javascript:void(0)" class="sd-unit-link mono" data-unit="${u}" onclick="openSystemdDetail(this.dataset.unit)">${u}</a>
        ${s.description && s.description !== s.name ? `<div style="font-size: var(--fs-xs); color: var(--text-muted);">${escapeHtml(s.description)}</div>` : ''}
      </td>
      <td>${systemdActiveBadge(s)}</td>
      <td class="mono text-muted">${escapeHtml(s.sub)}</td>
      <td>${systemdEnabledBadge(s)}</td>
      <td class="sd-actions">${acts.join('')}</td>
    </tr>`;
  }).join('');
}

function setSystemdFilter(f) {
  S.systemdState.filter = f;
  renderSystemdTab();
}

function setSystemdQuery(q) {
  S.systemdState.query = q;
  const services = (S.systemdState.data && S.systemdState.data.services) || [];
  const shown = services.filter(s => systemdMatches(s, S.systemdState.filter, q));
  const body = document.getElementById('systemdRows');
  const count = document.getElementById('systemdCount');
  if (body) body.innerHTML = systemdRowsHtml(shown);
  if (count) count.textContent = t('systemd_showing', 'Showing {shown} of {total} services').replace('{shown}', shown.length).replace('{total}', services.length);
}

function renderSystemdTab() {
  const content = document.getElementById('tabContent');
  const data = S.systemdState.data;
  if (!content || !data) return;
  const refreshBtn = `<button class="btn btn-sm btn-ghost" onclick="loadSystemdTab()">${svgIcon('refresh-cw', 'icon-sm')} <span>${t('refresh')}</span></button>`;
  const reloadBtn = `<button class="btn btn-sm btn-ghost" onclick="systemdAction('', 'daemon-reload', this)" title="${escapeHtml(t('systemd_reload_title'))}">${svgIcon('rotate-ccw', 'icon-sm')} <span>${systemdVerb('daemon-reload')}</span></button>`;
  const header = `
    <div style="display: flex; justify-content: space-between; align-items: center; gap: 8px; margin-bottom: 12px; flex-wrap: wrap; flex-shrink: 0;">
      <h4 style="margin: 0; display: flex; align-items: center; gap: 6px;">${svgIcon('cog', 'icon-sm')} <span>${t('systemd_title', 'systemd Services')}</span></h4>
      <span style="display: flex; gap: 4px;">${data.ok && data.supported ? reloadBtn : ''}${refreshBtn}</span>
    </div>`;
  if (!data.ok) {
    content.innerHTML = header + `<div class="empty-state"><div class="empty-state-icon">${svgIcon('triangle-alert', 'icon-xl')}</div><div class="empty-state-title">${t('systemd_unavailable', 'Could not read services')}</div><div class="empty-state-desc" style="font-family: var(--font-mono); font-size: var(--fs-xs); white-space: pre-wrap;">${escapeHtml(data.error || '')}</div></div>`;
    return;
  }
  if (!data.supported) {
    content.innerHTML = header + `<div class="empty-state"><div class="empty-state-icon">${svgIcon('cog', 'icon-xl')}</div><div class="empty-state-title">${t('systemd_unsupported_title', 'systemd is not available on this host')}</div><div class="empty-state-desc">${t('systemd_unsupported_desc')}</div></div>`;
    return;
  }
  const services = data.services || [];
  const running = services.filter(s => s.active === 'active' && s.sub === 'running').length;
  const failed = services.filter(s => s.active === 'failed').length;
  const enabled = services.filter(s => s.enabled === 'enabled').length;
  const filters = ['all', 'running', 'failed', 'inactive', 'enabled'];
  const shown = services.filter(s => systemdMatches(s, S.systemdState.filter, S.systemdState.query));
  content.innerHTML = header + `
    <div class="b-stat-grid" style="margin-bottom: 16px;">
      <div class="b-stat-tile">
        <div class="b-stat-tile-label">${svgIcon('play', 'icon-sm')} <span>${t('systemd_stat_running', 'Running')}</span></div>
        <div class="b-stat-tile-val text-success">${running}</div>
      </div>
      <div class="b-stat-tile">
        <div class="b-stat-tile-label">${svgIcon('circle-alert', 'icon-sm')} <span>${t('systemd_stat_failed', 'Failed')}</span></div>
        <div class="b-stat-tile-val ${failed > 0 ? 'text-danger' : 'text-muted'}">${failed}</div>
      </div>
      <div class="b-stat-tile">
        <div class="b-stat-tile-label">${svgIcon('power', 'icon-sm')} <span>${t('systemd_stat_enabled', 'Enabled at boot')}</span></div>
        <div class="b-stat-tile-val">${enabled}</div>
      </div>
      <div class="b-stat-tile">
        <div class="b-stat-tile-label">${svgIcon('layers', 'icon-sm')} <span>${t('systemd_stat_total', 'Total units')}</span></div>
        <div class="b-stat-tile-val">${services.length}</div>
      </div>
    </div>
    <div style="display: flex; justify-content: space-between; align-items: center; gap: 8px; margin-bottom: 10px; flex-wrap: wrap; flex-shrink: 0;">
      <div class="file-pill-group">
        ${filters.map(f => `<button class="file-pill-btn ${S.systemdState.filter === f ? 'active' : ''}" onclick="setSystemdFilter('${f}')">${t('systemd_filter_' + f)}</button>`).join('')}
      </div>
      <div class="search-box" style="max-width: 280px;">
        ${svgIcon('search', 'icon-sm')}
        <input type="text" id="systemdSearch" placeholder="${escapeHtml(t('systemd_search_ph', 'Search services...'))}" value="${escapeHtml(S.systemdState.query)}" oninput="setSystemdQuery(this.value)" />
      </div>
    </div>
    <div style="overflow-x: auto;">
      <table>
        <thead><tr><th>${t('systemd_col_service', 'Service')}</th><th>${t('systemd_col_state', 'State')}</th><th>${t('systemd_col_sub', 'Sub-state')}</th><th>${t('systemd_col_enabled', 'Boot')}</th><th style="text-align: right;">${t('systemd_col_actions', 'Actions')}</th></tr></thead>
        <tbody id="systemdRows">${systemdRowsHtml(shown)}</tbody>
      </table>
    </div>
    <div id="systemdCount" style="font-size: var(--fs-xs); color: var(--text-muted); margin-top: 8px;">${t('systemd_showing', 'Showing {shown} of {total} services').replace('{shown}', shown.length).replace('{total}', services.length)}</div>`;
}

function openSystemdDetail(unit) {
  if (!unit) return;
  S.systemdDetailState = { unit, errorsOnly: false, data: null, error: null, loading: true, acting: false, forceSudo: false };
  document.getElementById('systemdDetailTitle').textContent = unit;
  const badge = document.getElementById('systemdDetailBadge');
  badge.className = 'badge badge-unknown';
  badge.textContent = '…';
  openModal('systemdDetailModal');
  loadSystemdDetail();
}

async function loadSystemdDetail() {
  const st = S.systemdDetailState;
  const host = S.currentHost;
  if (!st.unit || !host) return;
  st.loading = true;
  renderSystemdDetail();
  let res;
  try {
    const forceSudo = st.forceSudo;
    st.forceSudo = false;
    const r = await fetch('/api/hosts/' + host.id + '/systemd/detail?unit=' + encodeURIComponent(st.unit) + (st.errorsOnly ? '&errors=1' : '') + (forceSudo ? '&retry_sudo=1' : ''));
    res = await r.json();
  } catch (e) {
    res = { ok: false, error: e.message };
  }
  if (S.systemdDetailState !== st) return;
  st.loading = false;
  if (res.ok) { st.data = res.detail; st.error = null; } else { st.error = res.error || 'error'; }
  renderSystemdDetail();
}

function toggleSystemdErrorsOnly(on) {
  S.systemdDetailState.errorsOnly = on;
  loadSystemdDetail();
}

function retrySystemdSudo() {
  S.systemdDetailState.forceSudo = true;
  loadSystemdDetail();
}

function systemdMemoryText(v) {
  const n = Number(v);
  return Number.isFinite(n) && n > 0 && n < 1.8e19 ? formatBytes(n) : '—';
}

function systemdLogsHtml(text) {
  return text.split('\n').map(line => {
    const cls = /\b(error|failed|failure|fatal|emerg|crit|alert|denied|refused|killed)\b/i.test(line) ? 'sd-log-err'
      : (/\bwarn(ing)?\b/i.test(line) ? 'sd-log-warn' : '');
    return cls ? `<span class="${cls}">${escapeHtml(line)}</span>` : escapeHtml(line);
  }).join('\n');
}

function renderSystemdDetail() {
  const st = S.systemdDetailState;
  const body = document.getElementById('systemdDetailBody');
  const footer = document.getElementById('systemdDetailFooter');
  const badge = document.getElementById('systemdDetailBadge');
  if (!body || !footer || !st.unit) return;
  const closeBtn = `<button class="btn btn-sm" onclick="closeModal('systemdDetailModal')">${t('close', 'Close')}</button>`;
  const d = st.data;
  if (!d) {
    if (st.loading) {
      body.innerHTML = SP.loading(t('systemd_loading', 'Reading services...'));
      footer.innerHTML = `<span></span><div class="sd-footer-group">${closeBtn}</div>`;
    } else {
      body.innerHTML = `<div class="empty-state"><div class="empty-state-icon">${svgIcon('triangle-alert', 'icon-xl')}</div><div class="empty-state-title">${t('systemd_unavailable', 'Could not read services')}</div><div class="empty-state-desc" style="font-family: var(--font-mono); font-size: var(--fs-xs); white-space: pre-wrap;">${escapeHtml(st.error || '')}</div></div>`;
      footer.innerHTML = `<span></span><div class="sd-footer-group"><button class="btn btn-sm" onclick="loadSystemdDetail()">${svgIcon('refresh-cw', 'icon-sm')} <span>${t('refresh')}</span></button>${closeBtn}</div>`;
    }
    return;
  }

  const p = d.properties || {};
  const state = p.ActiveState || '';
  const up = state === 'active' || state === 'activating' || state === 'reloading';
  const canAct = p.LoadState !== 'masked' && p.LoadState !== 'not-found';
  badge.className = 'badge ' + (state === 'failed' ? 'badge-danger' : (state === 'active' ? 'badge-success' : (state === 'inactive' ? 'badge-unknown' : 'badge-warning')));
  badge.textContent = state + (p.SubState ? ' · ' + p.SubState : '');

  const since = state === 'active' ? p.ActiveEnterTimestamp : p.InactiveEnterTimestamp;
  const result = p.Result || '—';
  const exitCode = p.ExecMainStatus && p.ExecMainStatus !== '0' ? ` (exit ${escapeHtml(p.ExecMainStatus)})` : '';
  const tiles = `
    <div class="b-stat-grid">
      <div class="b-stat-tile">
        <div class="b-stat-tile-label">${svgIcon('activity', 'icon-sm')} <span>${t('systemd_sum_result', 'Last result')}</span></div>
        <div class="b-stat-tile-val ${result !== 'success' && result !== '—' ? 'text-danger' : ''}" style="font-size: var(--fs-md);">${escapeHtml(result)}${exitCode}</div>
      </div>
      <div class="b-stat-tile">
        <div class="b-stat-tile-label">${svgIcon('clock', 'icon-sm')} <span>${state === 'active' ? t('systemd_sum_since', 'Running since') : t('systemd_sum_stopped', 'Stopped since')}</span></div>
        <div class="b-stat-tile-val" style="font-size: var(--fs-sm);">${escapeHtml(since || '—')}</div>
      </div>
      <div class="b-stat-tile">
        <div class="b-stat-tile-label">${svgIcon('cpu', 'icon-sm')} <span>${t('systemd_sum_proc', 'Process')}</span></div>
        <div class="b-stat-tile-val mono" style="font-size: var(--fs-sm);">PID ${escapeHtml(p.MainPID && p.MainPID !== '0' ? p.MainPID : '—')} · ${escapeHtml(systemdMemoryText(p.MemoryCurrent))}</div>
        <div class="b-stat-tile-sub">${t('systemd_restarts', 'Restarts')}: ${escapeHtml(p.NRestarts || '0')}</div>
      </div>
    </div>`;

  const diags = d.diagnoses || [];
  const diagHtml = diags.length === 0 ? '' : `
    <div class="sd-diag">
      <div style="font-weight: 600; display: flex; align-items: center; gap: 6px;">${svgIcon('triangle-alert', 'icon-sm')} <span>${t('systemd_diag_title', 'Probable causes')}</span></div>
      ${diags.map(x => `
        <div class="sd-diag-item">
          <div class="sd-diag-title">${escapeHtml(t('systemd_diag_' + x.code, x.code))}</div>
          <div class="sd-diag-hint">${escapeHtml(t('systemd_diag_' + x.code + '_hint', ''))}</div>
          ${x.evidence ? `<div class="sd-diag-evidence">${escapeHtml(x.evidence)}</div>` : ''}
        </div>`).join('')}
    </div>`;

  const kv = `
    <div class="b-kv-grid">
      <span class="b-kv-label">${t('systemd_unit_file', 'Unit file')}:</span><span class="b-kv-val mono">${escapeHtml(p.FragmentPath || '—')}</span>
      <span class="b-kv-label">${t('systemd_col_enabled', 'Boot')}:</span><span class="b-kv-val">${escapeHtml(p.UnitFileState || '—')}</span>
      <span class="b-kv-label">${t('systemd_user', 'Runs as')}:</span><span class="b-kv-val mono">${escapeHtml(p.User || 'root')}</span>
      <span class="b-kv-label">${t('systemd_desc', 'Description')}:</span><span class="b-kv-val">${escapeHtml(p.Description || '—')}</span>
    </div>`;

  let note = '';
  if (d.logs_note === 'no_permission' && d.logs_sudo_skipped) {
    note = `<div class="sd-note sd-note-warn">${svgIcon('lock', 'icon-sm')} <span style="flex: 1;">${t('systemd_logs_sudo_skipped')}</span><button class="btn btn-sm" onclick="retrySystemdSudo()">${t('systemd_logs_retry_sudo', 'Retry with sudo')}</button></div>`;
  } else if (d.logs_note === 'no_permission') {
    note = `<div class="sd-note sd-note-warn">${svgIcon('lock', 'icon-sm')} <span style="flex: 1;">${t('systemd_logs_noperm')}</span><button class="btn btn-sm" onclick="retrySystemdSudo()">${t('systemd_logs_retry_sudo', 'Retry with sudo')}</button></div>`;
  } else if (d.logs_note === 'unavailable') {
    note = `<div class="sd-note sd-note-info">${svgIcon('info', 'icon-sm')} <span>${t('systemd_logs_unavailable')}</span></div>`;
  } else if (d.logs_elevated) {
    note = `<div class="sd-note sd-note-info">${svgIcon('shield-check', 'icon-sm')} <span>${t('systemd_logs_elevated')}</span></div>`;
  }
  const logsText = st.loading ? t('systemd_loading', 'Reading services...') : (d.logs ? '' : t('systemd_logs_empty', 'No journal entries.'));
  const logsInner = st.loading || !d.logs ? escapeHtml(logsText) : systemdLogsHtml(d.logs);

  body.innerHTML = `
    ${diagHtml}
    ${tiles}
    ${kv}
    <div>
      <div class="sd-section-title">
        <span>${svgIcon('scroll-text', 'icon-sm')} ${t('systemd_logs_title', 'Recent journal')}</span>
        <span style="display: flex; align-items: center; gap: 10px;">
          <label class="switch" style="margin: 0;">
            <input type="checkbox" ${st.errorsOnly ? 'checked' : ''} onchange="toggleSystemdErrorsOnly(this.checked)" />
            <span class="switch-track"><span class="switch-thumb"></span></span>
            <span class="switch-label">${t('systemd_logs_errors_only', 'Errors only')}</span>
          </label>
          <button class="btn btn-sm btn-ghost" onclick="copySystemdLogs()">${svgIcon('copy', 'icon-sm')} <span>${t('systemd_logs_copy', 'Copy')}</span></button>
        </span>
      </div>
      ${note}
      <pre class="sd-pre sd-logs" id="systemdLogsPre">${logsInner}</pre>
    </div>
    <details>
      <summary style="cursor: pointer; font-weight: 600; font-size: var(--fs-sm); margin-bottom: 8px;">${t('systemd_status_output', 'systemctl status')}</summary>
      <pre class="sd-pre">${escapeHtml(d.status || '—')}</pre>
    </details>`;

  const dis = st.acting ? 'disabled' : '';
  const u = escapeHtml(st.unit);
  const isSSH = S.SYSTEMD_SSH_RE.test(st.unit);
  const act = (action, cls, icon) => `<button class="btn btn-sm ${cls}" ${dis} data-unit="${u}" onclick="systemdAction(this.dataset.unit, '${action}', this)">${svgIcon(icon, 'icon-sm')} <span>${t(action)}</span></button>`;
  const buttons = [];
  if (canAct) {
    if (!up) buttons.push(act('start', 'btn-primary', 'play'));
    buttons.push(act('restart', up ? 'btn-primary' : '', 'rotate-ccw'));
    if (up && !isSSH) buttons.push(act('stop', 'btn-danger', 'square'));
  }
  // Secondary actions live in a "More" menu (stop/disable/mask are not offered for the SSH daemon).
  const unitFile = p.UnitFileState || '';
  const more = [];
  const item = (action, icon) => `<button class="dropdown-item" data-unit="${u}" onclick="systemdAction(this.dataset.unit, '${action}', this)">${svgIcon(icon, 'icon-sm')} <span>${systemdVerb(action)}</span></button>`;
  if (canAct && up) more.push(item('reload', 'refresh-cw'));
  if (state === 'failed') more.push(item('reset-failed', 'rotate-ccw'));
  if (canAct && unitFile === 'disabled') more.push(item('enable', 'power'));
  if (canAct && unitFile === 'enabled' && !isSSH) more.push(item('disable', 'ban'));
  if (p.LoadState === 'masked' || unitFile === 'masked') more.push(item('unmask', 'lock-open'));
  else if (p.LoadState !== 'not-found' && !isSSH) more.push(item('mask', 'lock'));
  const moreMenu = more.length === 0 ? '' : `
    <div class="dropdown">
      <button class="btn btn-sm btn-ghost" ${dis} onclick="toggleDropdown(event, 'sd-more')">${svgIcon('ellipsis', 'icon-sm')} <span>${t('systemd_btn_more', 'More')}</span></button>
      <div class="dropdown-menu" id="dropdown-sd-more" style="left: 0; right: auto;">${more.join('')}</div>
    </div>`;
  footer.innerHTML = `
    <div class="sd-footer-group">${buttons.join('')}${moreMenu}${window.AG ? AG.btn('askUnit', st.unit, state === 'failed' ? 'btn-primary' : 'btn-ghost') : ''}</div>
    <div class="sd-footer-group">
      <button class="btn btn-sm btn-ghost" ${dis} onclick="loadSystemdDetail()">${svgIcon('refresh-cw', 'icon-sm')} <span>${t('refresh')}</span></button>
      ${closeBtn}
    </div>`;
}

function copySystemdLogs() {
  const d = S.systemdDetailState.data;
  if (!d || !d.logs) return;
  copyTextToClipboard(d.logs);
  showToast(t('copied', 'Copied!'), 'success', 1500);
}

function systemdVerb(action) {
  const v = t('systemd_verb_' + action.replace(/-/g, '_'), '');
  return v || t(action);
}

async function systemdAction(unit, action, btn) {
  const host = S.currentHost;
  if (!host || (!unit && action !== 'daemon-reload')) return;
  if (action in S.SYSTEMD_CONFIRM) {
    let message = t('systemd_confirm_' + action + '_msg').replace('{host}', host.name);
    if (action === 'restart' && S.SYSTEMD_SSH_RE.test(unit)) message += '\n\n' + t('systemd_confirm_ssh_restart');
    const ok = await showConfirm({
      title: t('systemd_confirm_' + action + '_title').replace('{unit}', unit),
      message,
      confirmText: systemdVerb(action),
      danger: S.SYSTEMD_CONFIRM[action]
    });
    if (!ok) return;
  }
  const label = unit || host.name;
  const detailOpen = !!unit && S.systemdDetailState.unit === unit && document.getElementById('systemdDetailModal').classList.contains('open');
  // The clicked button spins with the elapsed time; the toast keeps the feedback visible
  // when the detail modal re-renders its footer (which replaces the button).
  const restore = btn ? SP.busy(btn, systemdVerb(action)) : null;
  const busyToast = showBusyToast(systemdVerb(action), Date.now());
  if (detailOpen) { S.systemdDetailState.acting = true; renderSystemdDetail(); }
  let res;
  try {
    const r = await fetch('/api/hosts/' + host.id + '/systemd/action', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ unit, action })
    });
    res = await r.json();
  } catch (e) {
    res = { ok: false, error: e.message };
  } finally {
    if (restore) restore();
    busyToast.close();
  }
  const r = res.result || {};
  const state = [r.active_state, r.sub_state].filter(Boolean).join('/') || r.enabled_state || '—';
  if (res.ok) {
    showToast(t('systemd_action_ok').replace('{unit}', label).replace('{action}', systemdVerb(action)).replace('{state}', state), 'success');
  } else if (r.exit_code === 0) {
    // systemctl accepted the request but the unit dropped straight into "failed": show why.
    showToast(t('systemd_action_failed_after').replace('{unit}', label).replace('{action}', systemdVerb(action)), 'error', 8000);
    if (unit && !detailOpen) openSystemdDetail(unit);
  } else {
    let msg = res.error || r.output || t('systemd_action_failed').replace('{unit}', label).replace('{action}', systemdVerb(action));
    if (r.mode === 'none') msg += ' — ' + t('systemd_action_nopriv');
    showToast(msg, 'error', 8000);
  }
  if (detailOpen && S.systemdDetailState.unit === unit) { S.systemdDetailState.acting = false; loadSystemdDetail(); }
  if (S.currentTab === 'systemd') loadSystemdTab(true);
}

async function dockerPost(sub, payload, onChunk) {
  const host = S.currentHost;
  if (!host) return { ok: false, error: 'no host' };
  try {
    const res = await fetch('/api/hosts/' + host.id + '/docker/' + sub, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    if (!res.ok || !(res.headers.get('Content-Type') || '').startsWith('text/plain')) return await res.json();
    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let text = '';
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      const delta = decoder.decode(value, { stream: true });
      text += delta;
      if (onChunk) onChunk(delta);
    }
    const m = text.match(/\n*\[pantau\] exit=(\d+)\s*$/);
    if (!m) return { ok: false, output: text, error: 'connection lost before the command finished' };
    const output = text.slice(0, m.index).trim();
    if (m[1] === '0') return { ok: true, output };
    return { ok: false, output, error: output.split('\n').pop() || 'failed' };
  } catch (e) {
    return { ok: false, error: e.message };
  }
}

function showDockerOutput(title, text) {
  stopDockerLogs();
  document.getElementById('dockerLogsTitle').textContent = title;
  document.getElementById('dockerLogsOutput').textContent = text;
  const status = document.getElementById('dockerLogsStatus');
  status.textContent = t('dk_done');
  status.className = 'badge badge-healthy';
  openModal('dockerLogsModal');
}

async function dockerExecute(o) {
  const data = await DP.run(o);
  if (data.ok && S.currentTab === 'docker') loadDockerTab();
  return data;
}

function dockerLines(s) {
  return String(s || '').split('\n').map(x => x.trim()).filter(Boolean);
}

function openDockerForm(cfg) {
  S.dkForm = cfg;
  document.getElementById('dockerFormTitle').textContent = cfg.title;
  const fieldHtml = f => {
    const id = 'dkf_' + f.id;
    const common = `id="${id}"${f.readonly ? ' readonly' : ''}${f.placeholder ? ` placeholder="${escapeHtml(f.placeholder)}"` : ''}`;
    let input;
    if (f.type === 'textarea') input = `<textarea ${common} rows="${f.rows || 3}">${escapeHtml(f.value || '')}</textarea>`;
    else if (f.type === 'select') input = `<select id="${id}">${(f.options || []).map(([v, l]) => `<option value="${escapeHtml(v)}" ${v === f.value ? 'selected' : ''}>${escapeHtml(l)}</option>`).join('')}</select>`;
    else if (f.type === 'checkbox') return `<div class="dk-field ${f.full ? 'dk-full' : ''}"><label style="display: flex; align-items: center; gap: 8px; cursor: pointer;"><input type="checkbox" id="${id}" style="width: auto;" ${f.value ? 'checked' : ''} /> <span>${escapeHtml(f.label)}</span></label>${f.hint ? `<div class="dk-hint">${escapeHtml(f.hint)}</div>` : ''}</div>`;
    else input = `<input type="text" ${common} value="${escapeHtml(f.value || '')}" ${f.mono ? 'class="mono"' : ''} />`;
    return `<div class="dk-field ${f.full ? 'dk-full' : ''}"><label for="${id}">${escapeHtml(f.label)}${f.required ? ' *' : ''}</label>${input}${f.hint ? `<div class="dk-hint">${escapeHtml(f.hint)}</div>` : ''}</div>`;
  };
  document.getElementById('dockerFormBody').innerHTML = (cfg.intro ? `<div style="font-size: var(--fs-sm); color: var(--text-muted);">${escapeHtml(cfg.intro)}</div>` : '') +
    `<div class="dk-form-grid">${cfg.fields.map(fieldHtml).join('')}</div>`;
  const submit = document.getElementById('dockerFormSubmit');
  submit.textContent = cfg.submit;
  submit.disabled = false;
  openModal('dockerFormModal');
  const first = cfg.fields.find(f => !f.readonly);
  if (first) setTimeout(() => { const el = document.getElementById('dkf_' + first.id); if (el) el.focus(); }, 50);
}

function dockerFormValues() {
  const v = {};
  (S.dkForm ? S.dkForm.fields : []).forEach(f => {
    const el = document.getElementById('dkf_' + f.id);
    if (!el) return;
    v[f.id] = f.type === 'checkbox' ? el.checked : el.value;
  });
  return v;
}

async function submitDockerForm() {
  const cfg = S.dkForm;
  if (!cfg) return;
  const values = dockerFormValues();
  const missing = cfg.fields.find(f => f.required && !String(values[f.id] || '').trim());
  if (missing) {
    showToast(t('dk_required').replace('{field}', missing.label), 'warning');
    const el = document.getElementById('dkf_' + missing.id);
    if (el) el.focus();
    return;
  }
  const btn = document.getElementById('dockerFormSubmit');
  btn.disabled = true;
  let ok = false;
  try {
    ok = await cfg.onSubmit(values);
  } finally {
    btn.disabled = false;
  }
  if (ok) closeModal('dockerFormModal');
}

function openDockerRunForm(image) {
  openDockerForm({
    title: t('dk_add_container'),
    submit: t('dk_deploy'),
    fields: [
      { id: 'image', label: t('dk_f_image'), value: image || '', placeholder: 'nginx:1.27-alpine', required: true, mono: true, full: true },
      { id: 'name', label: t('dk_f_name'), placeholder: 'web' },
      { id: 'restart', label: t('dk_f_restart'), type: 'select', value: '', options: [['', t('dk_restart_no')], ['unless-stopped', 'unless-stopped'], ['always', 'always'], ['on-failure', 'on-failure']] },
      { id: 'ports', label: t('dk_f_ports'), type: 'textarea', rows: 3, placeholder: '8080:80', hint: t('dk_h_ports') },
      { id: 'env', label: t('dk_f_env'), type: 'textarea', rows: 3, placeholder: 'KEY=value', hint: t('dk_h_env') },
      { id: 'volumes', label: t('dk_f_volumes'), type: 'textarea', rows: 3, placeholder: '/srv/data:/data', hint: t('dk_h_volumes'), full: true },
      { id: 'network', label: t('dk_f_network'), placeholder: 'bridge' },
      { id: 'command', label: t('dk_f_command'), placeholder: 'sleep 3600', hint: t('dk_h_command'), mono: true },
      { id: 'memory', label: t('dk_f_memory'), placeholder: '512m' },
      { id: 'cpus', label: t('dk_f_cpus'), placeholder: '1.5' }
    ],
    onSubmit: async v => {
      const data = await dockerExecute({
        sub: 'run',
        payload: { image: v.image.trim(), name: v.name.trim(), restart: v.restart, network: v.network.trim(), command: v.command, memory: v.memory.trim(), cpus: v.cpus.trim(), ports: dockerLines(v.ports), env: dockerLines(v.env), volumes: dockerLines(v.volumes) },
        okMsg: t('dk_container_created')
      });
      return data.ok;
    }
  });
}

function openDockerRenameForm(name) {
  openDockerForm({
    title: t('dk_rename') + ': ' + name,
    submit: t('dk_rename'),
    fields: [{ id: 'new_name', label: t('dk_f_new_name'), value: name, required: true, full: true }],
    onSubmit: async v => {
      const data = await dockerAction(name, 'rename', { new_name: v.new_name.trim() });
      return !!(data && data.ok);
    }
  });
}

async function dockerConsole(name) {
  const host = S.currentHost;
  if (!host) return;
  const tab = openTerminalForHost(host.id, null, true, 'exec ' + name);
  if (!tab) return;
  const shell = `command -v bash >/dev/null 2>&1 && exec bash || exec sh`;
  const cmd = `d=docker; docker ps -q >/dev/null 2>&1 || d="sudo docker"; $d exec -it ${dkShq(name)} sh -c ${dkShq(shell)}`;
  for (let i = 0; i < 60; i++) {
    if (tab.socketLeft && tab.socketLeft.readyState === WebSocket.OPEN) {
      setTimeout(() => sendTabCommand(tab.id, cmd), 400);
      return;
    }
    await new Promise(r => setTimeout(r, 200));
  }
  showToast(t('dk_console_timeout'), 'warning');
}

function dkShq(s) {
  return "'" + String(s).replace(/'/g, "'\\''") + "'";
}

function openDockerInspect(name) {
  if (!name) return;
  S.dkInspectState = { name, data: null, reveal: false, loading: true, error: null, t0: Date.now() };
  document.getElementById('dockerInspectTitle').textContent = name;
  const badge = document.getElementById('dockerInspectBadge');
  badge.className = 'badge badge-unknown';
  badge.textContent = '…';
  openModal('dockerInspectModal');
  loadDockerInspect();
}

async function loadDockerInspect() {
  const st = S.dkInspectState;
  const host = S.currentHost;
  if (!st.name || !host) return;
  st.loading = true;
  st.t0 = Date.now();
  renderDockerInspect();
  let res;
  try {
    const r = await fetch('/api/hosts/' + host.id + '/docker/inspect?name=' + encodeURIComponent(st.name));
    res = await r.json();
  } catch (e) {
    res = { ok: false, error: e.message };
  }
  if (S.dkInspectState !== st) return;
  st.loading = false;
  if (res.ok) { st.data = res.container; st.error = null; } else { st.error = res.error || 'error'; }
  renderDockerInspect();
}

function toggleDockerEnvReveal() {
  S.dkInspectState.reveal = !S.dkInspectState.reveal;
  renderDockerInspect();
}

async function dockerDisconnect(network, container) {
  const ok = await showConfirm({
    title: t('dk_confirm_disconnect_title').replace('{name}', network),
    message: t('dk_confirm_disconnect_msg').replace('{container}', container),
    confirmText: t('dk_disconnect'),
    danger: true
  });
  if (!ok) return;
  const data = await DP.run({ sub: 'network', payload: { action: 'disconnect', name: network, container } });
  if (data.ok) loadDockerInspect();
}

function dockerKv(rows) {
  return `<div class="b-kv-grid">${rows.filter(r => r[1] !== undefined && r[1] !== '').map(([k, v, mono]) => `<span class="b-kv-label">${escapeHtml(k)}:</span><span class="b-kv-val${mono ? ' dk-mono' : ''}">${v}</span>`).join('')}</div>`;
}

function dockerSection(title, inner, right) {
  return `<div class="dk-section"><div class="dk-section-title"><span>${title}</span>${right || ''}</div>${inner}</div>`;
}

function dockerMiniTable(heads, rows) {
  return `<div style="overflow-x: auto;"><table><thead><tr>${heads.map(h => `<th>${h}</th>`).join('')}</tr></thead><tbody>${rows}</tbody></table></div>`;
}

function renderDockerInspect() {
  const st = S.dkInspectState;
  const body = document.getElementById('dockerInspectBody');
  const footer = document.getElementById('dockerInspectFooter');
  const badge = document.getElementById('dockerInspectBadge');
  if (!body || !footer || !st.name) return;
  const closeBtn = `<button class="btn btn-sm" onclick="closeModal('dockerInspectModal')">${t('close', 'Close')}</button>`;
  const d = st.data;
  if (!d) {
    body.innerHTML = st.loading
      ? SP.loading(t('docker_loading_data'), st.t0) + SP.skeleton(4)
      : dockerEmptyHtml(t('docker_unavailable'), st.error || '', true);
    footer.innerHTML = `<span></span><div class="sd-footer-group">${st.loading ? '' : `<button class="btn btn-sm" onclick="loadDockerInspect()">${svgIcon('refresh-cw', 'icon-sm')} <span>${t('refresh')}</span></button>`}${closeBtn}</div>`;
    return;
  }
  const state = d.paused ? 'paused' : d.status;
  badge.className = 'badge ' + (d.running && !d.paused ? 'badge-success' : (d.status === 'exited' || d.status === 'dead' ? 'badge-danger' : 'badge-warning'));
  badge.textContent = state + (d.health ? ' · ' + d.health : '');

  const problems = [];
  if (d.oom_killed) problems.push(t('dk_oom_killed'));
  if (!d.running && d.exit_code !== 0) problems.push(t('dk_exit_code').replace('{code}', d.exit_code));
  if (d.error) problems.push(d.error);
  if (d.health === 'unhealthy' && d.health_log) problems.push(d.health_log);
  const alert = problems.length === 0 ? '' : `<div class="sd-diag"><div style="font-weight: 600; display: flex; align-items: center; gap: 6px;">${svgIcon('triangle-alert', 'icon-sm')} <span>${t('dk_attention')}</span></div>${problems.map(p => `<div class="sd-diag-evidence">${escapeHtml(p)}</div>`).join('')}</div>`;

  const tiles = `
    <div class="b-stat-grid">
      <div class="b-stat-tile"><div class="b-stat-tile-label">${svgIcon('clock', 'icon-sm')} <span>${d.running ? t('dk_started') : t('dk_finished')}</span></div><div class="b-stat-tile-val" style="font-size: var(--fs-sm);">${escapeHtml(((d.running ? d.started_at : d.finished_at) || '').replace('T', ' ').replace(/\.\d+Z$/, ' UTC') || '—')}</div></div>
      <div class="b-stat-tile"><div class="b-stat-tile-label">${svgIcon('rotate-ccw', 'icon-sm')} <span>${t('systemd_restarts')}</span></div><div class="b-stat-tile-val">${d.restart_count}</div><div class="b-stat-tile-sub">${t('dk_f_restart')}: ${escapeHtml(d.restart_policy)}</div></div>
      <div class="b-stat-tile"><div class="b-stat-tile-label">${svgIcon('cpu', 'icon-sm')} <span>${t('dk_limits')}</span></div><div class="b-stat-tile-val" style="font-size: var(--fs-sm);">${d.memory_limit > 0 ? formatBytes(d.memory_limit) : t('dk_unlimited')} · ${d.cpus > 0 ? d.cpus + ' CPU' : t('dk_unlimited')}</div></div>
    </div>`;

  const overview = dockerSection(t('dk_sec_config'), dockerKv([
    ['ID', escapeHtml(d.id.slice(0, 12)) + `<span class="text-faint" title="${escapeHtml(d.id)}"> …</span>`, true],
    [t('container_image'), escapeHtml(d.image), true],
    [t('dk_created'), escapeHtml((d.created || '').replace('T', ' ').replace(/\.\d+Z$/, ' UTC'))],
    [t('dk_command'), d.command ? escapeHtml(d.command) : '', true],
    [t('dk_entrypoint'), d.entrypoint ? escapeHtml(d.entrypoint) : '', true],
    [t('dk_workdir'), d.working_dir ? escapeHtml(d.working_dir) : '', true],
    [t('systemd_user'), d.user ? escapeHtml(d.user) : '', true],
    ['Hostname', escapeHtml(d.hostname), true],
    [t('dk_network_mode'), escapeHtml(d.network_mode), true],
    [t('dk_privileged'), d.privileged ? `<span class="badge badge-warning">${t('dk_yes')}</span>` : t('dk_no')]
  ]));

  const ports = d.ports.length === 0 ? '' : dockerSection(t('dk_sec_ports'), dockerMiniTable([t('dk_container_port'), t('dk_host_binding')], d.ports.map(p => `<tr><td class="dk-mono">${escapeHtml(p.container_port)}</td><td class="dk-mono">${p.host_port ? escapeHtml((p.host_ip || '0.0.0.0') + ':' + p.host_port) : `<span class="text-faint">${t('dk_not_published')}</span>`}</td></tr>`).join('')));
  const mounts = d.mounts.length === 0 ? '' : dockerSection(t('dk_sec_mounts'), dockerMiniTable([t('dk_type'), t('dk_source'), t('dk_destination'), 'RW'], d.mounts.map(m => `<tr><td>${escapeHtml(m.type)}</td><td class="dk-mono">${escapeHtml(m.name || m.source)}</td><td class="dk-mono">${escapeHtml(m.destination)}</td><td>${m.rw ? t('dk_yes') : '<span class="badge badge-info">ro</span>'}</td></tr>`).join('')));
  const nets = d.networks.length === 0 ? '' : dockerSection(t('dk_sec_networks'), dockerMiniTable([t('docker_col_network'), 'IP', t('docker_col_gateway'), 'MAC', ''], d.networks.map(n => `<tr><td><b>${escapeHtml(n.name)}</b>${(n.aliases || []).length ? `<div class="dk-mono text-faint">${escapeHtml(n.aliases.join(', '))}</div>` : ''}</td><td class="dk-mono">${escapeHtml(n.ip || '—')}</td><td class="dk-mono">${escapeHtml(n.gateway || '—')}</td><td class="dk-mono">${escapeHtml(n.mac || '—')}</td><td class="dk-actions">${['host', 'none'].includes(n.name) ? '' : `<button class="btn btn-sm btn-ghost btn-icon" title="${t('dk_disconnect')}" data-net="${escapeHtml(n.name)}" data-c="${escapeHtml(d.name)}" onclick="dockerDisconnect(this.dataset.net, this.dataset.c)">${svgIcon('unplug', 'icon-sm')}</button>`}</td></tr>`).join('')));
  const envRows = d.env.map(e => { const i = e.indexOf('='); const k = i < 0 ? e : e.slice(0, i); const v = i < 0 ? '' : e.slice(i + 1); return `<tr><td class="dk-mono" style="font-weight: 600;">${escapeHtml(k)}</td><td class="dk-mono">${st.reveal ? escapeHtml(v) : '••••••••'}</td></tr>`; }).join('');
  const env = d.env.length === 0 ? '' : dockerSection(t('dk_sec_env'), dockerMiniTable([t('dk_key'), t('dk_value')], envRows), `<button class="btn btn-sm btn-ghost" onclick="toggleDockerEnvReveal()">${svgIcon(st.reveal ? 'eye-off' : 'eye', 'icon-sm')} <span>${st.reveal ? t('dk_hide_values') : t('dk_show_values')}</span></button>`);
  const labelKeys = Object.keys(d.labels || {}).sort();
  const labels = labelKeys.length === 0 ? '' : `<details><summary style="cursor: pointer; font-weight: 600; font-size: var(--fs-sm);">${t('dk_sec_labels')} (${labelKeys.length})</summary>${dockerMiniTable([t('dk_key'), t('dk_value')], labelKeys.map(k => `<tr><td class="dk-mono">${escapeHtml(k)}</td><td class="dk-mono">${escapeHtml(d.labels[k])}</td></tr>`).join(''))}</details>`;

  body.innerHTML = [alert, tiles, overview, ports, mounts, nets, env, labels].filter(Boolean).join('');

  const n = escapeHtml(d.name);
  const act = (action, cls, icon, label) => `<button class="btn btn-sm ${cls}" data-name="${n}" onclick="dockerAction(this.dataset.name, '${action}')">${svgIcon(icon, 'icon-sm')} <span>${label}</span></button>`;
  const buttons = [];
  if (d.paused) buttons.push(act('unpause', 'btn-primary', 'play', t('dk_unpause')));
  else if (!d.running) buttons.push(act('start', 'btn-primary', 'play', t('start')));
  if (d.running || d.paused) {
    buttons.push(act('restart', d.paused ? '' : 'btn-primary', 'rotate-ccw', t('restart')));
    buttons.push(act('stop', 'btn-danger', 'square', t('stop')));
  }
  buttons.push(`<button class="btn btn-sm" data-name="${n}" onclick="openDockerLogs(this.dataset.name)">${svgIcon('scroll-text', 'icon-sm')} <span>${t('docker_logs')}</span></button>`);
  if (window.AG) buttons.push(AG.btn('askContainer', d.name, d.running ? 'btn-ghost' : 'btn-primary').replace('data-arg=', 'data-name="' + n + '" data-arg='));
  if (d.running) buttons.push(`<button class="btn btn-sm" data-name="${n}" onclick="dockerConsole(this.dataset.name)">${svgIcon('square-terminal', 'icon-sm')} <span>${t('dk_console')}</span></button>`);
  footer.innerHTML = `<div class="sd-footer-group">${buttons.join('')}</div><div class="sd-footer-group"><button class="btn btn-sm btn-ghost" onclick="loadDockerInspect()">${svgIcon('refresh-cw', 'icon-sm')} <span>${t('refresh')}</span></button>${closeBtn}</div>`;
}

function renderDockerImages(data) {
  const images = data.images || [];
  if (images.length === 0) return dockerEmptyHtml(t('dk_no_images_title'), t('dk_no_images_desc'));
  const total = images.reduce((a, i) => a + i.size, 0);
  const unused = images.filter(i => !i.in_use).length;
  return `<div style="font-size: 0.85rem; color: var(--text-muted); margin-bottom: 8px;">${t('dk_img_summary').replace('{n}', images.length).replace('{u}', unused).replace('{size}', formatBytes(total))}${window.Hub ? Hub.summary() : ''}</div>` +
    dockerTableHtml([t('dk_f_image'), t('dk_col_tag'), 'ID', t('docker_col_size'), t('dk_col_created'), t('status'), t('ds_col_counts'), ''], images.map(i => {
      const ref = i.dangling ? i.id : i.repository + ':' + i.tag;
      const status = i.in_use ? `<span class="badge badge-success">${t('dk_in_use')}</span>` : (i.dangling ? `<span class="badge badge-warning">${t('dk_dangling')}</span>` : `<span class="badge badge-unknown">${t('dk_unused')}</span>`);
      return `
        <tr>
          <td class="dk-mono" style="font-weight: 600; color: var(--text);">${escapeHtml(i.dangling ? '<none>' : i.repository)}</td>
          <td class="dk-mono">${escapeHtml(i.dangling ? '<none>' : i.tag)}</td>
          <td class="dk-mono text-muted" title="${escapeHtml(i.full_id)}">${escapeHtml(i.id)}</td>
          <td style="white-space: nowrap;">${formatBytes(i.size)}</td>
          <td class="text-muted" style="white-space: nowrap;" title="${escapeHtml(i.created)}">${escapeHtml(i.age)}</td>
          <td>${status}${!i.dangling && window.Hub ? Hub.imageBadge(ref) : ''}</td>
          <td>${!i.dangling && window.DS ? DS.chips(ref) : ''}</td>
          <td class="dk-actions">
            ${i.dangling ? '' : `${window.Hub ? Hub.imageActions(ref) : ''}${window.DS ? DS.scanButton(ref) : ''}<button class="btn btn-sm btn-ghost btn-icon" title="${t('dk_run_image')}" data-ref="${escapeHtml(ref)}" onclick="openDockerRunForm(this.dataset.ref)">${svgIcon('play', 'icon-sm')}</button>`}
            <button class="btn btn-sm btn-danger btn-icon" ${i.in_use ? 'disabled' : ''} title="${escapeHtml(i.in_use ? t('dk_in_use_hint') : t('dk_remove'))}" data-ref="${escapeHtml(ref)}" onclick="dockerImageRemove(this.dataset.ref)">${svgIcon('trash-2', 'icon-sm')}</button>
          </td>
        </tr>`;
    }).join(''));
}

function openDockerPullForm() {
  openDockerForm({
    title: t('dk_pull_image'),
    submit: t('dk_pull'),
    intro: t('dk_pull_intro'),
    fields: [{ id: 'image', label: t('dk_f_image'), placeholder: 'nginx:1.27-alpine', required: true, mono: true, full: true }],
    onSubmit: async v => {
      const data = await dockerExecute({ sub: 'image', payload: { action: 'pull', image: v.image.trim() } });
      return data.ok;
    }
  });
}

async function dockerImageRemove(ref) {
  const ok = await showConfirm({
    title: t('dk_confirm_image_remove_title').replace('{name}', ref),
    message: t('dk_confirm_image_remove_msg'),
    confirmText: t('dk_remove'),
    danger: true
  });
  if (!ok) return;
  await dockerExecute({ sub: 'image', payload: { action: 'remove', image: ref } });
}

function openDockerVolumeForm() {
  openDockerForm({
    title: t('dk_create_volume'),
    submit: t('dk_create'),
    fields: [{ id: 'name', label: t('dk_f_name'), placeholder: 'app-data', required: true, mono: true, full: true }],
    onSubmit: async v => (await dockerExecute({ sub: 'volume', payload: { action: 'create', name: v.name.trim() } })).ok
  });
}

async function dockerVolumeRemove(name) {
  const ok = await showConfirm({
    title: t('dk_confirm_volume_remove_title').replace('{name}', name),
    message: t('dk_confirm_volume_remove_msg'),
    confirmText: t('dk_remove'),
    danger: true
  });
  if (!ok) return;
  await dockerExecute({ sub: 'volume', payload: { action: 'remove', name } });
}

function openDockerNetworkForm() {
  openDockerForm({
    title: t('dk_create_network'),
    submit: t('dk_create'),
    fields: [
      { id: 'name', label: t('dk_f_name'), placeholder: 'app-net', required: true, mono: true },
      { id: 'driver', label: t('docker_col_driver'), type: 'select', value: 'bridge', options: [['bridge', 'bridge'], ['macvlan', 'macvlan'], ['ipvlan', 'ipvlan']] },
      { id: 'subnet', label: t('docker_col_subnet'), placeholder: '172.30.0.0/16', mono: true },
      { id: 'gateway', label: t('docker_col_gateway'), placeholder: '172.30.0.1', mono: true },
      { id: 'internal', label: t('dk_f_internal'), type: 'checkbox', hint: t('dk_h_internal'), full: true }
    ],
    onSubmit: async v => (await dockerExecute({ sub: 'network', payload: { action: 'create', name: v.name.trim(), driver: v.driver, subnet: v.subnet.trim(), gateway: v.gateway.trim(), internal: v.internal } })).ok
  });
}

async function openDockerConnectForm(network) {
  let names = [];
  try {
    const res = await fetch('/api/hosts/' + S.currentHost.id + '/docker');
    const data = await res.json();
    names = (data.containers || []).map(c => c.name);
  } catch (e) {}
  if (names.length === 0) { showToast(t('no_containers_title'), 'info'); return; }
  openDockerForm({
    title: t('dk_connect') + ': ' + network,
    submit: t('dk_connect'),
    fields: [{ id: 'container', label: t('dk_f_container'), type: 'select', value: names[0], options: names.map(n => [n, n]), full: true }],
    onSubmit: async v => (await dockerExecute({ sub: 'network', payload: { action: 'connect', name: network, container: v.container } })).ok
  });
}

async function dockerNetworkRemove(name) {
  const ok = await showConfirm({
    title: t('dk_confirm_network_remove_title').replace('{name}', name),
    message: t('dk_confirm_network_remove_msg'),
    confirmText: t('dk_remove'),
    danger: true
  });
  if (!ok) return;
  await dockerExecute({ sub: 'network', payload: { action: 'remove', name } });
}

function renderDockerStacks(data) {
  const stacks = data.stacks || [];
  S.dockerStacksCache = stacks;
  if (stacks.length === 0) return dockerEmptyHtml(t('dk_no_stacks_title'), t('dk_no_stacks_desc'));
  return dockerTableHtml([t('dk_col_stack'), t('status'), t('dk_col_services'), t('dk_col_file'), ''], stacks.map((s, idx) => {
    const cls = s.status === 'running' ? 'badge-success' : (s.status === 'partial' ? 'badge-warning' : 'badge-unknown');
    const n = escapeHtml(s.name);
    return `
      <tr>
        <td><b style="color: var(--text);">${n}</b></td>
        <td><span class="badge ${cls}">${t('dk_stack_' + s.status)}${s.status === 'down' ? '' : ` · ${s.running}/${s.total}`}</span></td>
        <td>${dockerChips(s.services.map(x => [x]))}</td>
        <td class="dk-mono text-muted">${s.config_files.length ? s.config_files.map(escapeHtml).join('<br>') : '—'}</td>
        <td class="dk-actions">
          <button class="btn btn-sm btn-ghost btn-icon" title="${t('dk_stack_edit')}" ${s.config_files.length === 1 ? '' : 'disabled'} data-name="${n}" onclick="openDockerStackForm(this.dataset.name, this)">${svgIcon('pencil', 'icon-sm')}</button>
          <button class="btn btn-sm btn-ghost btn-icon" title="${t('dk_stack_update')}" ${s.config_files.length ? '' : 'disabled'} data-name="${n}" onclick="dockerStackAction(this.dataset.name, 'update')">${svgIcon('download', 'icon-sm')}</button>
          <button class="btn btn-sm btn-ghost btn-icon" title="${t('docker_menu_title')}" ${s.config_files.length ? '' : 'disabled'} onclick="openDockerStackMenu(event, ${idx})">${svgIcon('ellipsis-vertical', 'icon-sm')}</button>
        </td>
      </tr>`;
  }).join(''));
}

function dockerFloatingMenu(e, id, items) {
  e.stopPropagation();
  const existing = document.getElementById('dockerActionMenu');
  const toggleOff = existing && existing.dataset.idx === id;
  closeDockerMenu();
  closeAllDropdowns();
  if (toggleOff) return;
  const menu = document.createElement('div');
  menu.id = 'dockerActionMenu';
  menu.className = 'dropdown-menu';
  menu.dataset.idx = id;
  menu.style.cssText = 'display: block; position: fixed; margin: 0; right: auto; bottom: auto; z-index: ' + (S.modalZIndexCounter + 100) + ';';
  items.forEach(it => {
    if (!it) {
      const hr = document.createElement('div');
      hr.className = 'dropdown-divider';
      menu.appendChild(hr);
      return;
    }
    const b = document.createElement('button');
    b.className = 'dropdown-item' + (it.danger ? ' danger' : '');
    b.textContent = it.label;
    if (it.title) b.title = it.title;
    if (it.disabled) {
      b.disabled = true;
      b.style.opacity = '0.5';
      b.style.cursor = 'not-allowed';
    } else {
      b.addEventListener('click', () => { closeDockerMenu(); it.run(); });
    }
    menu.appendChild(b);
  });
  document.body.appendChild(menu);
  const rect = e.currentTarget.getBoundingClientRect();
  let top = rect.bottom + 4;
  if (top + menu.offsetHeight > window.innerHeight) top = rect.top - menu.offsetHeight - 4;
  menu.style.left = Math.max(4, rect.right - menu.offsetWidth) + 'px';
  menu.style.top = Math.max(4, top) + 'px';
}

function openDockerStackMenu(e, idx) {
  const s = (S.dockerStacksCache || [])[idx];
  if (!s) return;
  const n = s.name;
  const items = s.status === 'down' ? [
    { label: t('dk_stack_up'), run: () => dockerStackAction(n, 'up') },
    null,
    { label: t('dk_stack_forget'), danger: true, run: () => dockerStackAction(n, 'forget') }
  ] : [
    { label: t('start'), run: () => dockerStackAction(n, 'start') },
    { label: t('restart'), run: () => dockerStackAction(n, 'restart') },
    { label: t('stop'), run: () => dockerStackAction(n, 'stop') },
    null,
    { label: t('dk_stack_pull'), run: () => dockerStackAction(n, 'pull') },
    { label: t('dk_stack_up'), run: () => dockerStackAction(n, 'up') },
    { label: t('dk_stack_recreate'), run: () => dockerStackAction(n, 'recreate') },
    null,
    { label: t('dk_stack_down'), danger: true, run: () => dockerStackAction(n, 'down') }
  ];
  dockerFloatingMenu(e, 'stack' + idx, items);
}

async function dockerStackAction(name, action) {
  if (action === 'down' || action === 'recreate' || action === 'update' || action === 'forget') {
    const ok = await showConfirm({
      title: t('dk_confirm_stack_' + action + '_title').replace('{name}', name),
      message: t('dk_confirm_stack_' + action + '_msg'),
      confirmText: t('dk_stack_' + action),
      danger: action === 'down' || action === 'forget'
    });
    if (!ok) return;
  }
  if (action === 'forget') {
    await dockerExecute({ sub: 'stack', payload: { project: name, action } });
    return;
  }
  await dockerExecute({ sub: 'stack', payload: { project: name, action } });
}

function dockerIconBusy(btn, text) {
  const saved = btn.innerHTML;
  const wasDisabled = btn.disabled;
  btn.disabled = true;
  const t0 = Date.now();
  btn.innerHTML = `<span class="spinner" style="display:inline-block; margin-right:4px;">${svgIcon('loader-circle', 'icon-sm')}</span>` +
    `<span data-sp-t0="${t0}" style="font-variant-numeric: tabular-nums; font-size: var(--fs-xs); color: var(--text-muted);">0s</span>`;
  const savedTitle = btn.title;
  btn.title = text;
  return function () {
    btn.disabled = wasDisabled;
    btn.innerHTML = saved;
    btn.title = savedTitle;
  };
}

async function openDockerStackForm(name, btn) {
  let yaml = S.DOCKER_STACK_SAMPLE;
  let path = '';
  if (name) {
    // The compose file is fetched from the host (can take a while): the button shows the progress.
    const restore = btn ? dockerIconBusy(btn, t('docker_stack_loading')) : function () {};
    let data;
    try {
      const res = await fetch('/api/hosts/' + S.currentHost.id + '/docker/stack/file?project=' + encodeURIComponent(name));
      data = await res.json();
    } catch (e) {
      data = { ok: false, error: e.message };
    } finally {
      restore();
    }
    if (!data.ok) { showToast(t('error_prefix', 'Error: ') + (data.error || 'failed'), 'error', 8000); return; }
    yaml = data.content;
    path = data.path;
  }
  openDockerForm({
    title: name ? t('dk_stack_edit') + ': ' + name : t('dk_new_stack'),
    submit: t('dk_deploy'),
    intro: name ? path : t('dk_stack_new_intro'),
    fields: [
      { id: 'name', label: t('dk_f_stack_name'), value: name || '', placeholder: 'myapp', readonly: !!name, required: true, mono: true, full: true, hint: name ? '' : t('dk_h_stack_name') },
      { id: 'yaml', label: 'docker-compose.yml', type: 'textarea', rows: 18, value: yaml, required: true, full: true, hint: t('dk_h_stack_yaml') }
    ],
    onSubmit: async v => (await dockerExecute({ sub: 'stack/deploy', payload: { name: v.name.trim(), yaml: v.yaml } })).ok
  });
}

function renderDockerSystem(data) {
  const s = data.system;
  const tile = (icon, label, val, sub) => `<div class="b-stat-tile"><div class="b-stat-tile-label">${svgIcon(icon, 'icon-sm')} <span>${label}</span></div><div class="b-stat-tile-val" style="font-size: var(--fs-md);">${val}</div>${sub ? `<div class="b-stat-tile-sub">${sub}</div>` : ''}</div>`;
  const tiles = `
    <div class="b-stat-grid" style="margin-bottom: 16px;">
      ${tile('container', t('dk_sys_engine'), escapeHtml(s.server_version || '—'), 'Client ' + escapeHtml(s.client_version || '—') + (s.compose_version ? ' · Compose ' + escapeHtml(s.compose_version) : ''))}
      ${tile('server', t('dk_sys_host'), escapeHtml(s.os || '—'), escapeHtml(s.architecture) + ' · ' + escapeHtml(s.kernel))}
      ${tile('cpu', t('dk_sys_resources'), s.cpus + ' CPU · ' + formatBytes(s.memory_bytes))}
      ${tile('layers', t('dk_sys_containers'), s.containers_running + ' / ' + s.containers, s.containers_paused + ' ' + t('dk_pause').toLowerCase() + ' · ' + s.containers_stopped + ' ' + t('dk_stopped'))}
      ${tile('boxes', t('dk_sub_images_count'), s.images)}
      ${tile('hard-drive', t('dk_sys_storage'), escapeHtml(s.storage_driver || '—'), `<span class="dk-mono">${escapeHtml(s.root_dir)}</span>`)}
    </div>`;
  const usage = (s.usage || []).length === 0 ? '' : dockerSection(t('dk_sys_df'), dockerMiniTable([t('dk_type'), t('dk_sys_total'), t('dk_sys_active'), t('docker_col_size'), t('dk_sys_reclaimable')], s.usage.map(u => `<tr><td><b>${escapeHtml(u.type)}</b></td><td>${escapeHtml(u.total)}</td><td>${escapeHtml(u.active)}</td><td>${escapeHtml(u.size)}</td><td>${escapeHtml(u.reclaimable)}</td></tr>`).join('')));
  const prune = [['containers', 'dk_prune_containers'], ['images', 'dk_prune_images'], ['images_all', 'dk_prune_images_all'], ['volumes', 'dk_prune_volumes'], ['networks', 'dk_prune_networks'], ['builder', 'dk_prune_builder']];
  const maintenance = dockerSection(t('dk_sys_maintenance'), prune.map(([target, key]) => `
    <div class="dk-prune-row">
      <div><div style="font-weight: 600; font-size: var(--fs-sm);">${t(key)}</div><div class="dk-prune-desc">${t('dk_prune_' + target + '_desc')}</div></div>
      <button class="btn btn-sm btn-danger" onclick="dockerPrune('${target}')">${svgIcon('trash-2', 'icon-sm')} <span>${t('dk_prune')}</span></button>
    </div>`).join(''));
  return `<div style="display: flex; flex-direction: column; gap: 16px;">${tiles}${usage}${maintenance}</div>`;
}

async function dockerPrune(target) {
  const ok = await showConfirm({
    title: t('dk_confirm_prune_' + target + '_title'),
    message: t('dk_confirm_prune_' + target + '_msg').replace('{host}', S.currentHost ? S.currentHost.name : ''),
    confirmText: t('dk_prune'),
    danger: true
  });
  if (!ok) return;
  await dockerExecute({ sub: 'prune', payload: { target } });
}

function updatePassStrengthMeter(pass) {
  const bar = document.getElementById('passStrengthBar');
  const label = document.getElementById('passStrengthLabel');
  if (!bar) return;
  if (!pass) {
    bar.style.width = '0%';
    bar.style.background = 'var(--surface-3)';
    if (label) label.textContent = '';
    return;
  }
  let score = 0;
  if (pass.length >= 8) score++;
  if (pass.length >= 12) score++;
  if (/[A-Z]/.test(pass) && /[a-z]/.test(pass)) score++;
  if (/[0-9]/.test(pass)) score++;
  if (/[^A-Za-z0-9]/.test(pass)) score++;

  if (score <= 2) {
    bar.style.width = '33%';
    bar.style.background = 'var(--danger)';
    if (label) {
      label.textContent = t('pass_strength_weak', 'Weak');
      label.style.color = 'var(--danger)';
    }
  } else if (score <= 3) {
    bar.style.width = '66%';
    bar.style.background = 'var(--warning)';
    if (label) {
      label.textContent = t('pass_strength_fair', 'Fair');
      label.style.color = 'var(--warning)';
    }
  } else {
    bar.style.width = '100%';
    bar.style.background = 'var(--success)';
    if (label) {
      label.textContent = t('pass_strength_strong', 'Strong');
      label.style.color = 'var(--success)';
    }
  }
}

async function testHostFromModal() {
  const id = document.getElementById('editHostId')?.value;
  if (!id) return;
  await testHostSSH(Number(id));
}

function initSidePanel() {
  if (localStorage.getItem('pantau_side_panel_collapsed') === 'true') {
    document.body.classList.add('side-panel-collapsed');
  }
}

initSidePanel();

function toggleSidePanelCollapse() {
  const isCollapsed = document.body.classList.toggle('side-panel-collapsed');
  localStorage.setItem('pantau_side_panel_collapsed', isCollapsed ? 'true' : 'false');
}

function toggleSidePanelMobile(force) {
  const isOpen = typeof force === 'boolean' ? force : !document.body.classList.contains('side-panel-mobile-open');
  document.body.classList.toggle('side-panel-mobile-open', isOpen);
  const backdrop = document.getElementById('sidePanelBackdrop');
  if (backdrop) backdrop.style.display = isOpen ? 'block' : 'none';
}

function switchSideNav(section) {
  S.currentSideNav = section;
  document.querySelectorAll('.side-panel-item').forEach(el => {
    const isAct = el.id === ('navItem' + section.charAt(0).toUpperCase() + section.slice(1));
    el.classList.toggle('active', isAct);
    if (isAct) { el.setAttribute('aria-current', 'page'); el.setAttribute('aria-selected', 'true'); }
    else { el.removeAttribute('aria-current'); el.setAttribute('aria-selected', 'false'); }
  });
  const secHosts = document.getElementById('sectionHosts');
  const secDocker = document.getElementById('sectionDocker');
  const secKube = document.getElementById('sectionKube');
  const secCf = document.getElementById('sectionCloudflare');
  if (secHosts) secHosts.style.display = section === 'hosts' ? 'block' : 'none';
  if (secDocker) secDocker.style.display = section === 'docker' ? 'block' : 'none';
  if (secKube) secKube.style.display = section === 'kube' ? 'block' : 'none';
  if (secCf) secCf.style.display = section === 'cloudflare' ? 'block' : 'none';
  const secAns = document.getElementById('sectionAnsible');
  if (secAns) secAns.style.display = section === 'ansible' ? 'block' : 'none';
  const secTf = document.getElementById('sectionTerraform');
  if (secTf) secTf.style.display = section === 'terraform' ? 'block' : 'none';
  const secRepos = document.getElementById('sectionRepos');
  if (secRepos) secRepos.style.display = section === 'repos' ? 'block' : 'none';
  const secShortcuts = document.getElementById('sectionShortcuts');
  if (secShortcuts) secShortcuts.style.display = section === 'shortcuts' ? 'block' : 'none';
  const secSecrets = document.getElementById('sectionSecrets');
  if (secSecrets) secSecrets.style.display = section === 'secrets' ? 'block' : 'none';
  toggleSidePanelMobile(false);
  if (section !== 'kube') stopKubeAutorefresh();
  if (section === 'hosts') renderHosts();
  else if (window.GlobeRadar) window.GlobeRadar.unmount();
  if (section === 'docker') { renderDockerSection(); loadDockerFleetOverview(); }
  else if (section === 'kube') { loadKubeClusters(); populateKubeHostSelect(); }
  else if (section === 'cloudflare') { loadCfAccounts(); }
  else if (section === 'ansible') { refreshAnsibleSection(); }
  else if (section === 'terraform') { window.refreshTfSection(); }
  else if (section === 'repos') { if (window.Repos) Repos.refresh(); }
  else if (section === 'shortcuts') { if (window.Shortcuts) Shortcuts.refresh(); }
  else if (section === 'secrets') { if (window.Vault) Vault.refresh(); }
}

function updateSideNavData() {
  if (S.currentSideNav === 'docker') { renderDockerSection(); loadDockerFleetOverview(); }
  if (S.currentSideNav === 'kube') { loadKubeClusters(); populateKubeHostSelect(); }
  if (S.currentSideNav === 'cloudflare') { loadCfAccounts(); }
  if (S.currentSideNav === 'ansible') { refreshAnsibleSection(); }
}

async function openHostDocker(hostId) {
  setHostActiveTab(hostId, 'docker');
  await openHostDetail(hostId);
  switchTab('docker');
}

async function loadDockerFleetOverview() {
  const strip = document.getElementById('dockerFleetStrip');
  if (!strip) return;
  try {
    const res = await fetch('/api/docker/overview');
    if (!res.ok) throw new Error(String(res.status));
    const d = await res.json();
    const stat = (label, value, cls) => `<div class="dk-fleet-stat"><div class="dk-fleet-num ${cls || ''}">${value}</div><div class="dk-fleet-label">${escapeHtml(label)}</div></div>`;
    strip.innerHTML = stat(t('dk_fleet_hosts', 'Hosts with Docker'), d.hosts_with_docker + ' / ' + d.host_count) +
      stat(t('dk_fleet_containers', 'Containers'), d.total_containers) +
      stat(t('dk_fleet_running', 'Running'), d.running_containers, 'ok') +
      stat(t('dk_fleet_stopped', 'Stopped'), d.stopped_containers) +
      stat(t('dk_fleet_stacks', 'Stacks'), d.total_stacks) +
      stat(t('dk_fleet_issues', 'Security issues'), d.total_security_issues, d.total_security_issues > 0 ? 'bad' : '');
  } catch (e) {
    strip.innerHTML = `<div class="dk-fleet-empty">${escapeHtml(t('dk_fleet_nodocker', 'No Docker data recorded yet'))}</div>`;
  }
}

function renderDockerSection() {
  const input = document.getElementById('dockerHostSearchInput');
  const q = (input ? input.value : '').toLowerCase().trim();
  const hosts = S.allRawHosts || [];
  const filtered = q ? hosts.filter(h => (h.name || '').toLowerCase().includes(q) || (h.host || '').toLowerCase().includes(q) || (h.os_info || '').toLowerCase().includes(q)) : hosts;
  const grid = document.getElementById('dockerHostsGrid');
  const empty = document.getElementById('dockerHostsEmpty');
  if (!grid || !empty) return;
  if (filtered.length === 0) { grid.style.display = 'none'; empty.style.display = 'block'; return; }
  grid.style.display = 'grid';
  empty.style.display = 'none';
  grid.innerHTML = filtered.map(h => {
    const status = h.status || 'unknown';
    return `<div class="card host-card" style="cursor:pointer;" onclick="openHostDocker(${h.id})">
      <div class="host-card-header">
        <div style="display:flex;align-items:center;gap:8px;min-width:0;">
          <span class="status-dot ${status}"></span>
          <span style="font-weight:600;font-size:var(--fs-md);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;">${escapeHtml(h.name)}</span>
        </div>
        <span class="badge badge-${status}">${escapeHtml(status)}</span>
      </div>
      <div class="mono text-muted">${escapeHtml(h.host)}</div>
      <div style="font-size:var(--fs-xs);color:var(--text-muted);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;">${escapeHtml(h.os_info || '-')}</div>
      <div style="margin-top:auto;display:flex;justify-content:flex-end;">
        <button class="btn btn-sm btn-primary" onclick="event.stopPropagation();openHostDocker(${h.id})" style="display:inline-flex;align-items:center;gap:6px;">
          ${svgIcon('container', 'icon-sm')} <span>${t('docker_open_btn', 'Manage Docker')}</span>
        </button>
      </div>
    </div>`;
  }).join('');
}

function filterDockerSectionHosts() {
  const input = document.getElementById('dockerHostSearchInput');
  const clearBtn = document.getElementById('clearDockerSearchBtn');
  if (clearBtn && input) clearBtn.style.display = input.value ? 'inline-flex' : 'none';
  renderDockerSection();
}

function clearDockerSectionSearch() {
  const input = document.getElementById('dockerHostSearchInput');
  if (input) input.value = '';
  filterDockerSectionHosts();
}

try { S.kubeWorkspaces = JSON.parse(localStorage.getItem('pantau_kube_workspaces') || '[]').filter(Number.isInteger); } catch (e) {}

export { isHostStale, fleetCounts, recordHostTrafficSample, getHostTrafficHistory, renderSparklineSVG, setKpiFilter, renderKpiStrip, hostSecurityChip, openHostSecurity, getHostTopIssues, loadSystemdTab, systemdMatches, systemdActiveBadge, systemdEnabledBadge, systemdRowsHtml, setSystemdFilter, setSystemdQuery, renderSystemdTab, openSystemdDetail, loadSystemdDetail, toggleSystemdErrorsOnly, retrySystemdSudo, systemdMemoryText, systemdLogsHtml, renderSystemdDetail, copySystemdLogs, systemdVerb, systemdAction, dockerPost, showDockerOutput, dockerExecute, dockerLines, openDockerForm, dockerFormValues, submitDockerForm, openDockerRunForm, openDockerRenameForm, dockerConsole, dkShq, openDockerInspect, loadDockerInspect, toggleDockerEnvReveal, dockerDisconnect, dockerKv, dockerSection, dockerMiniTable, renderDockerInspect, renderDockerImages, openDockerPullForm, dockerImageRemove, openDockerVolumeForm, dockerVolumeRemove, openDockerNetworkForm, openDockerConnectForm, dockerNetworkRemove, renderDockerStacks, dockerFloatingMenu, openDockerStackMenu, dockerStackAction, dockerIconBusy, openDockerStackForm, renderDockerSystem, dockerPrune, updatePassStrengthMeter, testHostFromModal, initSidePanel, toggleSidePanelCollapse, toggleSidePanelMobile, switchSideNav, updateSideNavData, openHostDocker, renderDockerSection, filterDockerSectionHosts, clearDockerSectionSearch, loadDockerFleetOverview };
