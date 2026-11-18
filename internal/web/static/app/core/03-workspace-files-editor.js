/* Host detail tabs, file manager, CodeMirror editor, workspace shelf.
 * Native ES module fragment of the former monolithic inline script.
 * Air-gapped: no bundler, no CDN. Shared state lives in the mutable S object
 * from 00-state.js; cross-domain functions are imported explicitly. */
import { S } from './00-state.js';
import { svgIcon } from './00-foundation.js';
import { t, showConfirm, openEditNoteModal, formatRelativeTime } from './01-i18n-auth.js';
import { loadHosts, openCommissionDateModal, showToast, inspectHost, applyHostDetailMaximizeState, getHostActiveTab, setHostActiveTab, getHostFilesPath, setHostFilesPath, openTerminalForPath } from './02-hosts.js';
import { copyTextToClipboard, hostIpCopyHtml, loadTerminalPins, openTerminalForHost } from './04-terminal.js';
import { formatBytes, openTransferDialog, openS3UploadDialog } from './05-settings-transfer.js';
import { openModal, closeModal, escapeHtml, jsArg } from './06-modal-docs-activity.js';
import { renderNetDiagTab, netDiagPrivateIPs } from './07-netdiag-compare.js';
import { loadSystemdTab, openDockerRunForm, openDockerRenameForm, dockerConsole, openDockerInspect, loadDockerInspect, renderDockerImages, openDockerPullForm, openDockerVolumeForm, dockerVolumeRemove, openDockerNetworkForm, openDockerConnectForm, dockerNetworkRemove, renderDockerStacks, dockerFloatingMenu, openDockerStackForm, renderDockerSystem, dockerPrune } from './08-metrics-systemd-docker.js';
import { openHostPackageUpgradePreview, pkgAction, openPkgInstallModal } from './10-bulk-pkg.js';

async function openHostDetail(id) {
  // Smart Resume: restore existing minimized session if present (ADR-0026)
  const existingSession = S.workspaceSessions.find(s => s.type === 'host' && s.hostId === id);
  if (existingSession) {
    restoreWorkspaceSession(existingSession.id);
    return;
  }
  const res = await fetch('/api/hosts/' + id);
  if (!res.ok) return;
  const data = await res.json();
  S.currentHost = data.host;
  S.currentDetailHostId = id;
  window.detailData = data;
  document.getElementById('detailHostName').innerText = S.currentHost.name;
  const sub = document.getElementById('detailHostSubtitle');
  if (sub) {
    const extra = S.currentHost.os_info ? ' • ' + escapeHtml(S.currentHost.os_info) : '';
    sub.innerHTML = hostIpCopyHtml(S.currentHost.host) + extra;
  }
  const badge = document.getElementById('detailHostBadge');
  badge.className = 'badge badge-' + S.currentHost.status;
  const statusStr = S.currentHost.status || 'unknown';
  badge.innerText = statusStr.charAt(0).toUpperCase() + statusStr.slice(1);
  openModal('hostDetailModal');
  applyHostDetailMaximizeState();
  const targetTab = getHostActiveTab(S.currentHost.id);
  switchTab(targetTab);
}

async function openHostTerminal(hostID, presetID) {
  openTerminalForHost(hostID, presetID);
}

function formatProcessCmd(cmd) {
  if (!cmd || typeof cmd !== 'string') return '-';
  const parts = cmd.trim().split(/\s+/);
  const exe = parts[0].split('/').pop();
  return exe || cmd;
}

function switchTab(tab, presetID) {
  S.currentTab = tab;
  if (S.currentHost && S.currentHost.id) {
    setHostActiveTab(S.currentHost.id, tab);
  }
  document.querySelectorAll('#hostDetailTabs .tab').forEach(t => {
    t.classList.toggle('active', t.getAttribute('data-tab') === tab);
  });

  const content = document.getElementById('tabContent');
  const d = window.detailData;
  if (!d) return;

  try {
    if (tab === 'overview') {
      const h = d.host;
      let scoreColor = 'var(--success)';
      let scoreLabel = t('optimal', 'Optimal');
      if (h.lifecycle_score < 50) {
        scoreColor = 'var(--danger)';
        scoreLabel = t('replacement_due', 'Replacement Due');
      } else if (h.lifecycle_score < 80) {
        scoreColor = 'var(--warning)';
        scoreLabel = t('review_recommended', 'Review Recommended');
      }

      let breakdownList = [];
      try {
        if (h.lifecycle_breakdown) {
          const parsed = JSON.parse(h.lifecycle_breakdown);
          if (Array.isArray(parsed)) breakdownList = parsed;
        }
      } catch(e) {}

      let topCPU = [];
      try {
        if (h.top_cpu_processes) {
          const parsed = JSON.parse(h.top_cpu_processes);
          if (Array.isArray(parsed)) topCPU = parsed;
        }
      } catch(e) {}

      let topRAM = [];
      try {
        if (h.top_ram_processes) {
          const parsed = JSON.parse(h.top_ram_processes);
          if (Array.isArray(parsed)) topRAM = parsed;
        }
      } catch(e) {}

      let topSwap = [];
      try {
        if (h.top_swap_processes) {
          const parsed = JSON.parse(h.top_swap_processes);
          if (Array.isArray(parsed)) topSwap = parsed;
        }
      } catch(e) {}

      topCPU = Array.isArray(topCPU) ? topCPU : [];
      topRAM = Array.isArray(topRAM) ? topRAM : [];
      topSwap = Array.isArray(topSwap) ? topSwap : [];
      breakdownList = Array.isArray(breakdownList) ? breakdownList : [];

      const detailRecency = formatRelativeTime(h.last_inspected);

      // Drift status
      const isDrifted = h.status === 'degraded' || (d.actual_items && d.actual_items.some(it => it.status === 'drift'));
      const violatedRules = (d.actual_items || []).filter(it => it.status === 'drift');
      const latestIncidentWithExcerpt = (d.incidents || []).find(it => it.root_cause_excerpt);

      // Resource metrics
      const cpuLoadStr = h.cpu_load || '-';
      let load1m = 0;
      if (h.cpu_load) {
        const parts = h.cpu_load.replace(',', ' ').trim().split(/\s+/);
        if (parts.length > 0) load1m = parseFloat(parts[0]) || 0;
      }
      const cores = Math.max(1, h.cpu_cores || 1);
      const cpuLoadPct = Math.min(100, Math.round((load1m / cores) * 100));
      const cpuCls = cpuLoadPct >= 90 ? 'danger' : (cpuLoadPct >= 70 ? 'warn' : '');

      const ramTotal = h.ram_total_bytes || 0;
      const ramUsed = h.ram_used_bytes || 0;
      const ramPct = ramTotal > 0 ? Math.min(100, Math.round((ramUsed / ramTotal) * 100)) : 0;
      const ramCls = ramPct >= 90 ? 'danger' : (ramPct >= 80 ? 'warn' : '');

      const swapTotal = h.swap_total_bytes || 0;
      const swapUsed = h.swap_used_bytes || 0;
      const swapPct = swapTotal > 0 ? Math.min(100, Math.round((swapUsed / swapTotal) * 100)) : 0;
      const swapCls = swapPct >= 90 ? 'danger' : (swapPct >= 70 ? 'warn' : '');

      const diskTotal = h.disk_total_bytes || 0;
      const diskUsed = h.disk_used_bytes || 0;
      const diskPct = diskTotal > 0 ? Math.min(100, Math.round((diskUsed / diskTotal) * 100)) : 0;
      const diskCls = diskPct >= 90 ? 'danger' : (diskPct >= 80 ? 'warn' : '');

      // Disk I/O status & speedometer
      const modalDiskBps = (h.disk_read_speed_bps || 0) + (h.disk_write_speed_bps || 0);
      let modalDiskStatusText = t('disk_io_optimal');
      let modalDiskStatusDot = 'active-optimal';
      let modalDiskBadgeCls = 'badge-healthy';
      if (modalDiskBps >= 80 * 1024 * 1024) {
        modalDiskStatusText = t('disk_io_busy');
        modalDiskStatusDot = 'active-busy';
        modalDiskBadgeCls = 'badge-down';
      } else if (modalDiskBps >= 10 * 1024 * 1024) {
        modalDiskStatusText = t('disk_io_active');
        modalDiskStatusDot = 'active-active';
        modalDiskBadgeCls = 'badge-warning';
      }

      content.innerHTML = `
        <div class="b-overview-grid">
          ${isDrifted ? `
            <div class="b-drift-callout">
              <div class="b-drift-header">
                <div class="b-drift-title">
                  ${svgIcon('triangle-alert', 'icon-sm')}
                  <span>${t('drift_callout_title', 'Desired State Drift Detected')}</span>
                  ${violatedRules.length > 0 ? `<span class="badge badge-down">${violatedRules.length} Violated</span>` : ''}
                </div>
                <button class="btn btn-sm btn-ghost" onclick="switchTab('desired')" style="color: var(--danger); font-size: var(--fs-xs); display: inline-flex; align-items: center; gap: 4px;">
                  <span>${t('drift_view_rules', 'View Desired State Rules')}</span>
                  ${svgIcon('chevron-right', 'icon-sm')}
                </button>
              </div>
              <div style="font-size: var(--fs-xs); color: var(--text-muted);">
                ${violatedRules.map(r => `<strong>${escapeHtml(r.kind)}/${escapeHtml(r.target)}</strong> (${escapeHtml(r.details || 'drift')})`).join(' • ')}
              </div>
              ${latestIncidentWithExcerpt && latestIncidentWithExcerpt.root_cause_excerpt ? `
                <div class="b-drift-code-well"><code>${escapeHtml(latestIncidentWithExcerpt.root_cause_excerpt)}</code></div>
              ` : ''}
            </div>
          ` : ''}

          <!-- Top Summary Stat Tiles -->
          <div class="b-stat-grid">
            <div class="b-stat-tile">
              <div class="b-stat-tile-label">${svgIcon('activity', 'icon-sm')} <span>Status & Uptime</span></div>
              <div class="b-stat-tile-val">
                <span class="badge badge-${h.status || 'unknown'}" style="text-transform: capitalize;">${escapeHtml(h.status || 'Unknown')}</span>
              </div>
              <div class="b-stat-tile-sub">${escapeHtml(h.uptime || 'N/A')}</div>
            </div>

            <div class="b-stat-tile">
              <div class="b-stat-tile-label">${svgIcon('gauge', 'icon-sm')} <span>Lifecycle Score</span></div>
              <div class="b-stat-tile-val" style="color: ${scoreColor};">
                ${h.lifecycle_score || 0}<span style="font-size: var(--fs-sm); font-weight: 500; color: var(--text-muted);">/100</span>
              </div>
              <div class="b-stat-tile-sub" style="color: ${scoreColor}; font-weight: 500;">${scoreLabel}</div>
            </div>

            <div class="b-stat-tile">
              <div class="b-stat-tile-label">${svgIcon('globe', 'icon-sm')} <span>Network Egress</span></div>
              <div class="b-stat-tile-val">
                <span style="color: ${h.internet_online ? 'var(--success)' : 'var(--danger)'};">
                  ${h.internet_online ? `${h.internet_latency_ms || 0} ms` : 'Offline'}
                </span>
              </div>
              <div class="b-stat-tile-sub" style="font-family: var(--font-mono);">${escapeHtml(h.public_ip || 'NAT / Private')}</div>
            </div>

            <div class="b-stat-tile">
              <div class="b-stat-tile-label">${svgIcon('clock', 'icon-sm')} <span>Last Inspected</span></div>
              <div class="b-stat-tile-val" style="font-size: var(--fs-md);">
                ${detailRecency.isStale ? `<span style="color: var(--warning); display: inline-flex; align-items: center; gap: 4px;">${svgIcon('triangle-alert', 'icon-sm')} ${detailRecency.relative}</span>` : `<span>${detailRecency.relative}</span>`}
              </div>
              <div class="b-stat-tile-sub">
                ${h.last_duration_ms > 0 ? `<span style="color: var(--success); font-family: var(--font-mono); font-weight: 600;">${h.last_duration_ms}ms</span> duration` : detailRecency.title}
              </div>
            </div>
          </div>

          <!-- Two Column Grid: Resource Metrics & Disk/Network Telemetry -->
          <div class="b-two-col-grid">
            <div class="card">
              <h4 style="margin: 0 0 14px 0; display: flex; align-items: center; gap: 6px;">
                ${svgIcon('cpu', 'icon-sm')} <span>Resource Utilization</span>
              </h4>

              <div class="b-metric-item">
                <div class="b-metric-header">
                  <span class="b-metric-name">${svgIcon('cpu', 'icon-sm')} CPU Load</span>
                  <span class="b-metric-val">${cores} Cores • Load: ${escapeHtml(cpuLoadStr)}</span>
                </div>
                <div class="b-meter">
                  <div class="b-meter-fill ${cpuCls}" style="width: ${cpuLoadPct}%;"></div>
                </div>
              </div>

              <div class="b-metric-item">
                <div class="b-metric-header">
                  <span class="b-metric-name">${svgIcon('memory-stick', 'icon-sm')} RAM</span>
                  <span class="b-metric-val">${formatBytes(ramUsed)} / ${formatBytes(ramTotal)} (${ramPct}%)</span>
                </div>
                <div class="b-meter">
                  <div class="b-meter-fill ${ramCls}" style="width: ${ramPct}%;"></div>
                </div>
              </div>

              <div class="b-metric-item">
                <div class="b-metric-header">
                  <span class="b-metric-name">${svgIcon('hard-drive', 'icon-sm')} Swap</span>
                  <span class="b-metric-val">${swapTotal > 0 ? `${formatBytes(swapUsed)} / ${formatBytes(swapTotal)} (${swapPct}%)` : t('swap_disabled', 'Disabled')}</span>
                </div>
                <div class="b-meter">
                  <div class="b-meter-fill ${swapCls}" style="width: ${swapPct}%;"></div>
                </div>
              </div>

              <div class="b-metric-item">
                <div class="b-metric-header">
                  <span class="b-metric-name">${svgIcon('database', 'icon-sm')} Root Storage (/)</span>
                  <span class="b-metric-val">${formatBytes(diskUsed)} / ${formatBytes(diskTotal)} (${diskPct}%)</span>
                </div>
                <div class="b-meter">
                  <div class="b-meter-fill ${diskCls}" style="width: ${diskPct}%;"></div>
                </div>
              </div>
            </div>

            <div class="card">
              <h4 style="margin: 0 0 14px 0; display: flex; align-items: center; gap: 6px;">
                ${svgIcon('activity', 'icon-sm')} <span>I/O & Network Throughput</span>
              </h4>

              <!-- Disk I/O Speedometer -->
              <div style="background: var(--surface-2); border: 1px solid var(--card-border); border-radius: var(--radius-sm); padding: 12px; margin-bottom: 12px;">
                <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px;">
                  <span style="font-size: var(--fs-xs); font-weight: 600; color: var(--text-muted); display: inline-flex; align-items: center; gap: 6px;">
                    ${svgIcon('hard-drive', 'icon-sm')} <span>Disk I/O Rate</span>
                  </span>
                  <div style="display: flex; align-items: center; gap: 6px;">
                    <div class="b-speed-dots" title="Load status: ${modalDiskStatusText}">
                      <div class="b-speed-dot ${modalDiskBps > 0 ? modalDiskStatusDot : ''}"></div>
                      <div class="b-speed-dot ${modalDiskBps >= 10*1024*1024 ? modalDiskStatusDot : ''}"></div>
                      <div class="b-speed-dot ${modalDiskBps >= 80*1024*1024 ? modalDiskStatusDot : ''}"></div>
                    </div>
                    <span class="badge ${modalDiskBadgeCls}" style="font-size: 0.7rem;">${modalDiskStatusText}</span>
                  </div>
                </div>
                <div style="display: flex; justify-content: space-between; font-size: var(--fs-sm); font-family: var(--font-mono);">
                  <div style="display: flex; align-items: center; gap: 4px; color: var(--accent-text);">
                    ${svgIcon('arrow-down', 'icon-sm')} <span>Read: <strong>${formatBytes(h.disk_read_speed_bps || 0)}/s</strong></span>
                  </div>
                  <div style="display: flex; align-items: center; gap: 4px; color: var(--text);">
                    ${svgIcon('arrow-up', 'icon-sm')} <span>Write: <strong>${formatBytes(h.disk_write_speed_bps || 0)}/s</strong></span>
                  </div>
                </div>
                <div style="font-size: var(--fs-xs); color: var(--text-muted); margin-top: 6px; text-align: right;">
                  Total Read: ${formatBytes(h.disk_read_bytes || 0)} • Total Write: ${formatBytes(h.disk_write_bytes || 0)}
                </div>
              </div>

              <!-- Live Bandwidth Rate -->
              <div style="background: var(--surface-2); border: 1px solid var(--card-border); border-radius: var(--radius-sm); padding: 12px;">
                <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px;">
                  <span style="font-size: var(--fs-xs); font-weight: 600; color: var(--text-muted); display: inline-flex; align-items: center; gap: 6px;">
                    ${svgIcon('network', 'icon-sm')} <span>Live Network Bandwidth</span>
                  </span>
                  <span class="badge badge-info" style="font-size: 0.7rem;">${h.active_conn_count || 0} Sockets</span>
                </div>
                <div style="display: flex; justify-content: space-between; font-size: var(--fs-sm); font-family: var(--font-mono);">
                  <div style="display: flex; align-items: center; gap: 4px; color: var(--success);">
                    ${svgIcon('arrow-down', 'icon-sm')} <span>In: <strong>${formatBytes(h.net_rx_speed_bps || 0)}/s</strong></span>
                  </div>
                  <div style="display: flex; align-items: center; gap: 4px; color: var(--accent-text);">
                    ${svgIcon('arrow-up', 'icon-sm')} <span>Out: <strong>${formatBytes(h.net_tx_speed_bps || 0)}/s</strong></span>
                  </div>
                </div>
                <div style="font-size: var(--fs-xs); color: var(--text-muted); margin-top: 6px; text-align: right;">
                  Total In: ${formatBytes(h.net_rx_bytes || 0)} • Total Out: ${formatBytes(h.net_tx_bytes || 0)}
                </div>
              </div>
            </div>
          </div>

          <!-- Two Column Grid: Hardware & Lifecycle Breakdown -->
          <div class="b-two-col-grid">
            <div class="card">
              <h4 style="margin: 0 0 14px 0; display: flex; align-items: center; gap: 6px;">
                ${svgIcon('server', 'icon-sm')} <span>${t('hardware_os_info', 'Hardware & OS Details')}</span>
              </h4>
              <div class="b-kv-grid">
                <span class="b-kv-label">${t('platform_model', 'Platform / Hardware Model')}:</span>
                <span class="b-kv-val">${escapeHtml(h.hardware_model || 'Standard / Generic')}</span>

                <span class="b-kv-label">${t('os')}</span>
                <span class="b-kv-val">${escapeHtml(h.os_info || '-')}</span>

                <span class="b-kv-label">${t('kernel_version', 'Kernel Version')}:</span>
                <span class="b-kv-val">${escapeHtml(h.kernel || '-')}</span>

                <span class="b-kv-label">${t('cpu_label')}</span>
                <span class="b-kv-val">${cores} ${cores > 1 ? t('cores', 'Cores') : t('core', 'Core')}</span>

                <span class="b-kv-label">Uptime:</span>
                <span class="b-kv-val">${escapeHtml(h.uptime || '-')}</span>

                <span class="b-kv-label">${t('public_ip', 'Public IP')}:</span>
                <span class="b-kv-val">${escapeHtml(h.public_ip || 'NAT / Private')}</span>

                <span class="b-kv-label">${t('private_ip', 'Private IP')}:</span>
                <span class="b-kv-val" style="font-family: var(--font-mono);">${escapeHtml(netDiagPrivateIPs(h).join(', ') || '-')}</span>
              </div>
            </div>

            <div class="card">
              <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px;">
                <h4 style="margin: 0; display: flex; align-items: center; gap: 6px;">
                  ${svgIcon('gauge', 'icon-sm')} <span>${t('lifecycle_assessment')}</span>
                </h4>
                <button class="btn btn-sm btn-ghost" onclick="openCommissionDateModal(${h.id}, ${jsArg(h.commission_date || '')}, ${jsArg(h.bios_date || '')})" style="font-size: var(--fs-xs); padding: 3px 8px; display: inline-flex; align-items: center; gap: 4px;" title="${t('edit_commission_date', 'Edit Commission Date')}">
                  ${svgIcon('calendar', 'icon-sm')}
                  <span>${h.commission_date ? escapeHtml(h.commission_date) : t('set_commission_date', 'Set Commission Date')}</span>
                </button>
              </div>

              ${breakdownList && breakdownList.length > 0 ? `
                <div class="b-lifecycle-list">
                  ${breakdownList.map(f => {
                    const isDed = f.score_deduction < 0;
                    return `
                      <div class="b-lifecycle-row">
                        <div class="b-lifecycle-title">
                          <span style="color: ${isDed ? 'var(--danger)' : 'var(--success)'};">${svgIcon(isDed ? 'triangle-alert' : 'circle-check', 'icon-sm')}</span>
                          <span>${escapeHtml(f.name)}:</span>
                          <span class="b-lifecycle-desc" title="${escapeHtml(f.detail)}">${escapeHtml(f.detail)}</span>
                        </div>
                        <span class="b-lifecycle-pill ${isDed ? 'deduction' : 'zero'}">
                          ${isDed ? f.score_deduction : '+0'}
                        </span>
                      </div>
                    `;
                  }).join('')}
                </div>
              ` : `
                <p style="font-size: var(--fs-sm); color: var(--text-muted);">${escapeHtml(h.lifecycle_notes || 'No lifecycle breakdown factors recorded.')}</p>
              `}

              ${(h.lifecycle_score < 60 || (breakdownList && breakdownList.some(f => f.name === 'Productive Lifespan' && f.score_deduction < 0))) ? `
                <div style="background: var(--danger-soft); border: 1px solid var(--danger-border); border-radius: var(--radius-sm); padding: 10px 12px; margin-top: 12px; font-size: var(--fs-xs);">
                  <div style="font-weight: 600; color: var(--danger); display: flex; align-items: center; gap: 6px; margin-bottom: 2px;">
                    ${svgIcon('triangle-alert', 'icon-sm')}
                    <span>${t('refresh_recommendation')}</span>
                  </div>
                  <div style="color: var(--text-muted);">${t('refresh_justification')}</div>
                </div>
              ` : ''}
            </div>
          </div>

          <!-- Top Resource Consumers (Process Telemetry) -->
          <div class="card top-resource-consumers">
            <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px;">
              <h4 style="margin: 0; display: flex; align-items: center; gap: 6px;">
                ${svgIcon('cpu', 'icon-sm')} <span>${t('top_resource_consumers', 'Top Resource Consumers (Process Telemetry)')}</span>
              </h4>
              <button class="btn btn-sm btn-ghost" onclick="openTerminalForHost(${h.id}, null, true)" style="display: inline-flex; align-items: center; gap: 4px; font-size: var(--fs-xs);">
                ${svgIcon('terminal', 'icon-sm')} <span>${t('open_terminal', 'Open Terminal')}</span>
              </button>
            </div>
            <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 12px;">
              <!-- CPU Processes -->
              <div style="background: var(--surface-2); border: 1px solid var(--card-border); border-radius: var(--radius-sm); padding: 10px;">
                <div style="font-weight: 600; font-size: var(--fs-xs); color: var(--accent-text); margin-bottom: 8px; display: flex; justify-content: space-between; align-items: center;">
                  <span style="display: inline-flex; align-items: center; gap: 4px;">${svgIcon('cpu', 'icon-sm')} Top CPU</span>
                  <span style="color: var(--text-muted);">${cores} Cores</span>
                </div>
                ${topCPU.length === 0 ? '<div style="color: var(--text-muted); font-size: var(--fs-xs); text-align: center; padding: 16px;">No active processes reported</div>' : `
                  <div style="overflow-x: auto;">
                    <table style="width: 100%; font-size: var(--fs-xs); margin: 0;">
                      <thead><tr><th>PID</th><th>Command</th><th style="text-align: right;">CPU%</th></tr></thead>
                      <tbody>
                        ${topCPU.map(p => {
                          const cpuVal = Number(p.cpu) || 0;
                          const cColor = cpuVal > 50 ? 'var(--danger)' : (cpuVal > 20 ? 'var(--warning)' : 'var(--success)');
                          return `
                            <tr>
                              <td style="font-family: var(--font-mono); color: var(--text-muted);">${escapeHtml(String(p.pid || '-'))}</td>
                              <td style="max-width: 140px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;" title="${escapeHtml(p.command || '')}"><b>${escapeHtml(formatProcessCmd(p.command))}</b> <span style="color: var(--text-muted); font-size: 0.7rem;">${escapeHtml(p.user || '-')}</span></td>
                              <td style="text-align: right; font-weight: 700; font-family: var(--font-mono); color: ${cColor};">${cpuVal.toFixed(1)}%</td>
                            </tr>
                          `;
                        }).join('')}
                      </tbody>
                    </table>
                  </div>
                `}
              </div>

              <!-- RAM Processes -->
              <div style="background: var(--surface-2); border: 1px solid var(--card-border); border-radius: var(--radius-sm); padding: 10px;">
                <div style="font-weight: 600; font-size: var(--fs-xs); color: var(--accent-text); margin-bottom: 8px; display: flex; justify-content: space-between; align-items: center;">
                  <span style="display: inline-flex; align-items: center; gap: 4px;">${svgIcon('memory-stick', 'icon-sm')} Top RAM (RSS)</span>
                  <span style="color: var(--text-muted); font-family: var(--font-mono);">${formatBytes(ramUsed)}</span>
                </div>
                ${topRAM.length === 0 ? '<div style="color: var(--text-muted); font-size: var(--fs-xs); text-align: center; padding: 16px;">No active processes reported</div>' : `
                  <div style="overflow-x: auto;">
                    <table style="width: 100%; font-size: var(--fs-xs); margin: 0;">
                      <thead><tr><th>PID</th><th>Command</th><th style="text-align: right;">Memory</th></tr></thead>
                      <tbody>
                        ${topRAM.map(p => `
                          <tr>
                            <td style="font-family: var(--font-mono); color: var(--text-muted);">${escapeHtml(String(p.pid || '-'))}</td>
                            <td style="max-width: 140px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;" title="${escapeHtml(p.command || '')}"><b>${escapeHtml(formatProcessCmd(p.command))}</b> <span style="color: var(--text-muted); font-size: 0.7rem;">${escapeHtml(p.user || '-')}</span></td>
                            <td style="text-align: right; font-weight: 700; font-family: var(--font-mono); color: var(--accent-text);">${formatBytes(Number(p.rss_bytes) || 0)}</td>
                          </tr>
                        `).join('')}
                      </tbody>
                    </table>
                  </div>
                `}
              </div>

              <!-- Swap Processes -->
              <div style="background: var(--surface-2); border: 1px solid var(--card-border); border-radius: var(--radius-sm); padding: 10px;">
                <div style="font-weight: 600; font-size: var(--fs-xs); color: var(--warning); margin-bottom: 8px; display: flex; justify-content: space-between; align-items: center;">
                  <span style="display: inline-flex; align-items: center; gap: 4px;">${svgIcon('hard-drive', 'icon-sm')} Top Swap</span>
                  <span style="color: var(--text-muted); font-family: var(--font-mono);">${swapTotal > 0 ? formatBytes(swapUsed) : 'Disabled'}</span>
                </div>
                ${topSwap.length === 0 ? `
                  <div style="color: var(--text-muted); font-size: var(--fs-xs); text-align: center; padding: 16px;">
                    ${swapTotal === 0 ? 'Swap is disabled' : (swapUsed === 0 ? 'No swap usage (Clean)' : 'No swap processes active')}
                  </div>
                ` : `
                  <div style="overflow-x: auto;">
                    <table style="width: 100%; font-size: var(--fs-xs); margin: 0;">
                      <thead><tr><th>PID</th><th>Command</th><th style="text-align: right;">Swap</th></tr></thead>
                      <tbody>
                        ${topSwap.map(p => `
                          <tr>
                            <td style="font-family: var(--font-mono); color: var(--text-muted);">${escapeHtml(String(p.pid || '-'))}</td>
                            <td style="max-width: 140px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;" title="${escapeHtml(p.command || '')}"><b>${escapeHtml(formatProcessCmd(p.command))}</b></td>
                            <td style="text-align: right; font-weight: 700; font-family: var(--font-mono); color: var(--warning);">${formatBytes(Number(p.swap_bytes) || 0)}</td>
                          </tr>
                        `).join('')}
                      </tbody>
                    </table>
                  </div>
                `}
              </div>
            </div>
          </div>

          <!-- Host Note Card -->
          <div class="card">
            <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px;">
              <h4 style="margin: 0; display: flex; align-items: center; gap: 6px;">
                ${svgIcon('sticky-note', 'icon-sm')} <span>${t('host_note')}</span>
              </h4>
              <button class="btn btn-sm btn-ghost" onclick="openEditNoteModal(${h.id}, ${jsArg(h.notes || '')}, ${jsArg(h.name || '')})" style="font-size: var(--fs-xs); display: inline-flex; align-items: center; gap: 4px;">
                ${svgIcon('pencil', 'icon-sm')} <span>${t('edit')}</span>
              </button>
            </div>
            <div style="font-size: var(--fs-sm); color: var(--text); white-space: pre-wrap; line-height: 1.5; background: var(--bg); padding: 10px 14px; border-radius: var(--radius-sm); border: 1px solid var(--card-border);">
              ${h.notes ? escapeHtml(h.notes) : '<span style="color: var(--text-muted); font-style: italic;">' + t('no_note_set') + '</span>'}
            </div>
          </div>
        </div>
      `;
    } else if (tab === 'network') {
      const h = d.host;
      let topConns = [];
      try {
        const parsed = JSON.parse(h.top_connections);
        if (Array.isArray(parsed)) topConns = parsed;
      } catch(e) {}


      const rxSpeed = formatBytes(h.net_rx_speed_bps || 0) + '/s';
      const txSpeed = formatBytes(h.net_tx_speed_bps || 0) + '/s';
      const totalRx = formatBytes(h.net_rx_bytes || 0);
      const totalTx = formatBytes(h.net_tx_bytes || 0);

      const diskReadSpeed = formatBytes(h.disk_read_speed_bps || 0) + '/s';
      const diskWriteSpeed = formatBytes(h.disk_write_speed_bps || 0) + '/s';
      const totalDiskRead = formatBytes(h.disk_read_bytes || 0);
      const totalDiskWrite = formatBytes(h.disk_write_bytes || 0);

      const modalDiskBps = (h.disk_read_speed_bps || 0) + (h.disk_write_speed_bps || 0);
      let modalDiskStatusText = t('disk_io_optimal');
      let modalDiskStatusColor = 'var(--success)';
      if (modalDiskBps >= 80 * 1024 * 1024) {
        modalDiskStatusText = t('disk_io_busy');
        modalDiskStatusColor = 'var(--danger)';
      } else if (modalDiskBps >= 10 * 1024 * 1024) {
        modalDiskStatusText = t('disk_io_active');
        modalDiskStatusColor = 'var(--warning)';
      }

      content.innerHTML = `
        <div class="b-stat-grid" style="margin-bottom: 16px;">
          <div class="b-stat-tile">
            <div class="b-stat-tile-label">${svgIcon('globe', 'icon-sm')} <span>${t('net_egress')}</span></div>
            <div class="b-stat-tile-val" style="color: ${h.internet_online ? 'var(--success)' : 'var(--danger)'};">
              ${h.internet_online ? `${t('connected')} (${h.internet_latency_ms || 0} ms)` : t('no_internet')}
            </div>
            <div class="b-stat-tile-sub" style="font-family: var(--font-mono);">${t('public_ip')} <b>${escapeHtml(h.public_ip || 'NAT / Private')}</b></div>
            <div class="b-stat-tile-sub" style="font-family: var(--font-mono);">${t('private_ip')} <b>${escapeHtml(netDiagPrivateIPs(h).join(', ') || '-')}</b></div>
          </div>

          <div class="b-stat-tile">
            <div class="b-stat-tile-label">${svgIcon('activity', 'icon-sm')} <span>${t('live_bandwidth')}</span></div>
            <div class="b-stat-tile-val" style="color: var(--accent-text); font-size: var(--fs-md); font-family: var(--font-mono);">
              ${svgIcon('arrow-down', 'icon-sm')} ${rxSpeed} • ${svgIcon('arrow-up', 'icon-sm')} ${txSpeed}
            </div>
            <div class="b-stat-tile-sub">${t('total')} In: ${totalRx} • Out: ${totalTx}</div>
          </div>

          <div class="b-stat-tile">
            <div class="b-stat-tile-label" style="justify-content: space-between;">
              <span style="display: inline-flex; align-items: center; gap: 4px;">${svgIcon('hard-drive', 'icon-sm')} ${t('live_disk_io')}</span>
              <span class="badge" style="background: var(--surface-3); color: ${modalDiskStatusColor}; font-size: 0.7rem; display: inline-flex; align-items: center; gap: 4px;"><span class="kpi-dot" style="background: ${modalDiskStatusColor}; width: 6px; height: 6px; border-radius: 50%;"></span> <span>${modalDiskStatusText}</span></span>
            </div>
            <div class="b-stat-tile-val" style="color: var(--text); font-size: var(--fs-md); font-family: var(--font-mono);">
              ${svgIcon('arrow-down', 'icon-sm')} ${diskReadSpeed} • ${svgIcon('arrow-up', 'icon-sm')} ${diskWriteSpeed}
            </div>
            <div class="b-stat-tile-sub">${t('total')} Read: ${totalDiskRead} • Write: ${totalDiskWrite}</div>
          </div>

          <div class="b-stat-tile">
            <div class="b-stat-tile-label">${svgIcon('network', 'icon-sm')} <span>${t('active_sockets')}</span></div>
            <div class="b-stat-tile-val" style="color: var(--accent-text);">
              ${h.active_conn_count || 0} <span style="font-size: var(--fs-xs); font-weight: 500; color: var(--text-muted);">${t('established')}</span>
            </div>
            <div class="b-stat-tile-sub">${topConns.length} ${t('ext_ips_active')}</div>
          </div>
        </div>

        <div>
          <!-- Socket Profile / Remote Connections -->
          <div class="card">
            <h4 style="margin: 0 0 12px 0; display: flex; align-items: center; gap: 6px;">
              ${svgIcon('network', 'icon-sm')} <span>${t('socket_profile', 'Socket Profile & Remote Connections')}</span>
            </h4>
            ${topConns.length === 0 ? `
              <div style="text-align: center; color: var(--text-muted); padding: 24px; font-size: var(--fs-xs);">${t('no_active_conn')}</div>
            ` : `
              <div style="overflow-x: auto;">
                <table style="width: 100%; font-size: var(--fs-xs);">
                  <thead>
                    <tr><th>${t('remote_ip')}</th><th>${t('connections')}</th><th style="text-align: right;">${t('status')}</th></tr>
                  </thead>
                  <tbody>
                    ${topConns.map(c => `
                      <tr>
                        <td style="font-family: var(--font-mono); font-weight: 600;">${escapeHtml(c.remote_ip)}</td>
                        <td style="font-family: var(--font-mono);">${c.count} ${t('conn_suffix')}</td>
                        <td style="text-align: right;"><span class="badge badge-info" style="font-size: 0.7rem;">${t('established')}</span></td>
                      </tr>
                    `).join('')}
                  </tbody>
                </table>
              </div>
            `}
          </div>
        </div>
      `;
    } else if (tab === 'desired') {
    const rules = d.rules || [];
    const kindIcons = {
      container: 'container',
      docker: 'container',
      service: 'cog',
      port: 'network',
      disk: 'hard-drive',
      cron: 'clock',
      backup: 'archive',
      process: 'cpu',
      security_updates: 'shield'
    };

    content.innerHTML = `
      <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 14px; flex-shrink: 0; flex-wrap: wrap; gap: 8px;">
        <h4 style="margin: 0; display: flex; align-items: center; gap: 6px;">
          ${svgIcon('list-checks', 'icon-sm')} <span>${t('desired_rules')}</span>
          ${rules.length > 0 ? `<span class="badge badge-info" style="font-size: var(--fs-xs);">${rules.length}</span>` : ''}
        </h4>
        <div style="display: flex; gap: 8px;">
          <button class="btn btn-warning btn-sm" id="btnGenerateBaseline" onclick="generateBaseline(${d.host.id}, this)" style="display: inline-flex; align-items: center; gap: 4px;">
            ${svgIcon('rotate-ccw', 'icon-sm')} <span>${t('auto_detect_baseline')}</span>
          </button>
          <button class="btn btn-primary btn-sm" onclick="addRulePrompt(${d.host.id})" style="display: inline-flex; align-items: center; gap: 4px;">
            ${svgIcon('plus', 'icon-sm')} <span>${t('add_rule')}</span>
          </button>
          ${rules.length > 0 ? `<button class="btn btn-danger btn-sm" id="btnDeleteSelectedRules" onclick="deleteSelectedRules()" disabled style="display: inline-flex; align-items: center; gap: 4px;">
            ${svgIcon('trash-2', 'icon-sm')} <span>${t('delete_selected', 'Delete selected')}<span id="ruleSelCount"></span></span>
          </button>` : ''}
        </div>
      </div>
      ${rules.length === 0 ? `
        <div class="empty-state">
          <div class="empty-state-icon">${svgIcon('list-checks', 'icon-xl')}</div>
          <div class="empty-state-title">${t('no_rules_title')}</div>
          <div class="empty-state-desc">${t('no_rules_desc')}</div>
          <div style="display: flex; gap: 8px; margin-top: 12px;">
            <button class="btn btn-warning" id="btnGenerateBaseline" onclick="generateBaseline(${d.host.id}, this)" style="display: inline-flex; align-items: center; gap: 4px;">
              ${svgIcon('rotate-ccw', 'icon-sm')} <span>${t('auto_detect_baseline')}</span>
            </button>
            <button class="btn btn-primary" onclick="addRulePrompt(${d.host.id})" style="display: inline-flex; align-items: center; gap: 4px;">
              ${svgIcon('plus', 'icon-sm')} <span>${t('add_rule')}</span>
            </button>
          </div>
        </div>
      ` : `
        <div class="card" style="padding: 0; overflow: hidden;">
          <div style="overflow-x: auto;">
            <table style="width: 100%; font-size: var(--fs-xs); margin: 0;">
              <thead>
                <tr>
                  <th style="width: 28px;"><input type="checkbox" id="ruleSelectAll" style="width: auto;" onchange="toggleAllRules(this.checked)" /></th>
                  <th>${t('kind')}</th>
                  <th>${t('target')}</th>
                  <th>${t('expected')}</th>
                  <th>${t('current_actual')}</th>
                  <th>${t('status')}</th>
                  <th style="text-align: right;">${t('action')}</th>
                </tr>
              </thead>
              <tbody>
                ${rules.map(r => {
                  const matched = (d.actual_items || []).find(it => it.kind === r.kind && it.target === r.target);
                  const currentVal = matched ? matched.current : 'not found';
                  const isOk = matched && matched.status === 'ok';
                  const iconName = kindIcons[r.kind] || 'circle-dot';
                  return `
                    <tr>
                      <td><input type="checkbox" class="rule-cb" value="${r.id}" style="width: auto;" onchange="updateRuleSelection()" /></td>
                      <td style="white-space: nowrap;">
                        <span style="display: inline-flex; align-items: center; gap: 6px; font-weight: 600;">
                          ${svgIcon(iconName, 'icon-sm')}
                          <span>${escapeHtml(r.kind)}</span>
                        </span>
                      </td>
                      <td style="font-family: var(--font-mono); font-weight: 600;">${escapeHtml(r.target)}</td>
                      <td style="font-family: var(--font-mono); color: var(--text-muted);">${escapeHtml(r.kind === 'file_integrity' && window.FIM ? FIM.expectedSummary(r.expected) : r.expected)}</td>
                      <td style="font-family: var(--font-mono);">
                        <span style="color: ${isOk ? 'var(--success)' : 'var(--danger)'}; font-weight: 600;">${escapeHtml(currentVal)}</span>
                        ${matched && matched.details ? `<div style="font-size: 0.7rem; color: var(--text-muted); font-family: var(--font-sans); margin-top: 2px; white-space: pre-wrap;">${escapeHtml(matched.details)}</div>` : ''}
                      </td>
                      <td>
                        <span class="badge badge-${matched && matched.status === 'not_assessed' ? 'unknown' : (isOk ? 'healthy' : 'down')}">
                          ${matched && matched.status === 'not_assessed' ? t('fim_not_assessed', 'Not assessed') : (isOk ? t('in_desired_state', 'In Desired State') : t('drift_detected', 'Drift Detected'))}
                        </span>
                      </td>
                      <td style="text-align: right; white-space: nowrap;">
                        <button class="btn btn-sm btn-ghost btn-icon" onclick="editRulePrompt(${r.id}, ${jsArg(r.kind)}, ${jsArg(r.target)}, ${jsArg(r.expected)})" title="${t('edit_rule', 'Edit Rule')}">
                          ${svgIcon('pencil', 'icon-sm')}
                        </button>
                        <button class="btn btn-sm btn-danger btn-icon" onclick="deleteRule(${r.id})" title="${t('delete', 'Delete')}">
                          ${svgIcon('trash-2', 'icon-sm')}
                        </button>
                      </td>
                    </tr>
                  `;
                }).join('')}
              </tbody>
            </table>
          </div>
        </div>
      `}
    `;
  } else if (tab === 'docker') {
    // loadDockerTab decides between the first load (spinner) and a refresh (keep the content).
    loadDockerTab();
  } else if (tab === 'domains') {
    if (window.Domains) Domains.renderTab(d.host);
  } else if (tab === 'databases') {
    if (window.DbBackup) DbBackup.renderTab(d.host);
  } else if (tab === 'systemd') {
    content.innerHTML = SP.loading(t('systemd_loading', 'Reading services...'));
    loadSystemdTab();
  } else if (tab === 'timeline') {
    const incidents = d.incidents || [];
    content.innerHTML = `
      <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; flex-shrink: 0;">
        <h4 style="margin: 0; display: flex; align-items: center; gap: 6px;">
          ${svgIcon('history', 'icon-sm')} <span>${t('timeline_title')}</span>
          ${incidents.length > 0 ? `<span class="badge badge-info" style="font-size: var(--fs-xs);">${incidents.length}</span>` : ''}
        </h4>
      </div>
      ${incidents.length === 0 ? `
        <div class="empty-state">
          <div class="empty-state-icon">${svgIcon('shield-check', 'icon-xl')}</div>
          <div class="empty-state-title">${t('all_systems_normal')}</div>
          <div class="empty-state-desc">${t('no_incidents_desc')}</div>
        </div>
      ` : `
        <div class="b-timeline">
          ${incidents.map(inc => {
            const isDrift = inc.event_type === 'drift_detected';
            const rel = formatRelativeTime(inc.created_at);
            return `
              <div class="b-timeline-item">
                <div class="b-timeline-dot ${isDrift ? '' : 'resolved'}"></div>
                <div class="b-timeline-card">
                  <div class="b-timeline-header">
                    <div style="display: flex; align-items: center; gap: 8px;">
                      <span class="badge ${isDrift ? 'badge-down' : 'badge-healthy'}" style="font-size: 0.7rem;">
                        ${isDrift ? 'DRIFT' : 'RESOLVED'}
                      </span>
                      <span class="b-timeline-summary">${escapeHtml(inc.summary)}</span>
                    </div>
                    <span class="b-timeline-time" title="${escapeHtml(rel.title)}">${escapeHtml(rel.relative)}</span>
                  </div>
                  ${inc.root_cause_excerpt ? `
                    <details style="margin-top: 6px;">
                      <summary style="cursor: pointer; font-size: var(--fs-xs); color: var(--text-muted); font-weight: 500;">
                        ${t('root_cause_excerpt')}
                      </summary>
                      <pre class="b-drift-code-well" style="margin-top: 6px;"><code>${escapeHtml(inc.root_cause_excerpt)}</code></pre>
                    </details>
                  ` : ''}
                </div>
              </div>
            `;
          }).join('')}
        </div>
      `}
    `;
  } else if (tab === 'runs') {
    renderInspectionRunsTab();
  } else if (tab === 'packages') {
    renderPackagesTab();
  } else if (tab === 'security') {
    loadSecurityTab();
  } else if (tab === 'netdiag') {
    renderNetDiagTab(d.host);
  } else if (tab === 'files') {
    const startPath = (S.currentHost && (S.currentHost._lastPath || (S.currentHost.id ? getHostFilesPath(S.currentHost.id) : null))) || '/';
    loadFiles(startPath);
  }
  } catch(err) {
    console.error("switchTab error:", err);
    if (content) {
      content.innerHTML = `<div class="card" style="padding: 20px; color: var(--danger);">Error displaying tab ${escapeHtml(tab)}: ${escapeHtml(err.message)}</div>`;
    }
  }
}

async function renderInspectionRunsTab() {
  const content = document.getElementById('tabContent');
  const d = window.detailData;
  if (!d || !d.host) return;

  S.activeRunsFilter = 'all';

  content.innerHTML = `
    <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; flex-wrap: wrap; gap: 8px; flex-shrink: 0;">
      <h4 style="margin: 0; display: flex; align-items: center; gap: 6px;">
        ${svgIcon('clock', 'icon-sm')} <span>${t('runs_title', 'Inspection Runs History')}</span>
      </h4>
      <div style="display: inline-flex; background: var(--surface-2); border: 1px solid var(--card-border); border-radius: var(--radius-sm); padding: 2px;">
        <button id="btnRunsFilterAll" class="btn btn-sm btn-primary" style="padding: 4px 12px; font-size: var(--fs-xs); display: inline-flex; align-items: center; gap: 4px;" onclick="switchRunsFilter('all')">
          ${svgIcon('list', 'icon-sm')} <span>${t('runs_filter_all', 'All History')}</span>
        </button>
        <button id="btnRunsFilterIssues" class="btn btn-sm btn-ghost" style="padding: 4px 12px; font-size: var(--fs-xs); display: inline-flex; align-items: center; gap: 4px;" onclick="switchRunsFilter('issues')">
          ${svgIcon('triangle-alert', 'icon-sm')} <span>${t('runs_filter_issues', 'Last 100 Issues (Audit Trail)')}</span>
        </button>
      </div>
    </div>
    <div id="runsContainer" style="overflow-y: auto; flex: 1;">
      <div style="text-align: center; padding: 24px; color: var(--text-muted); font-size: var(--fs-xs);">${t('loading', 'Loading...')}</div>
    </div>
  `;

  await loadAndDisplayInspectionRuns(d.host.id, 'all');
}

function switchRunsFilter(filter) {
  S.activeRunsFilter = filter;
  const d = window.detailData;
  if (!d || !d.host) return;
  const btnAll = document.getElementById('btnRunsFilterAll');
  const btnIssues = document.getElementById('btnRunsFilterIssues');
  if (btnAll && btnIssues) {
    if (filter === 'issues') {
      btnAll.className = 'btn btn-sm btn-ghost';
      btnIssues.className = 'btn btn-sm btn-danger';
    } else {
      btnAll.className = 'btn btn-sm btn-primary';
      btnIssues.className = 'btn btn-sm btn-ghost';
    }
  }
  loadAndDisplayInspectionRuns(d.host.id, filter);
}

async function loadAndDisplayInspectionRuns(hostID, filter = 'all') {
  const container = document.getElementById('runsContainer');
  if (!container) return;

  let runs = [];
  try {
    const res = await fetch('/api/hosts/' + hostID + '/runs?filter=' + filter + '&limit=100');
    if (res.ok) {
      runs = await res.json();
    }
  } catch (e) {
    console.error('Failed to load inspection runs', e);
  }

  if (!runs || runs.length === 0) {
    const isIssues = filter === 'issues';
    container.innerHTML = `
      <div class="empty-state">
        <div class="empty-state-icon">${svgIcon(isIssues ? 'shield-check' : 'list-checks', 'icon-xl')}</div>
        <div class="empty-state-title">${isIssues ? t('no_issues_title', 'No Issues Recorded') : t('runs_title', 'Inspection Runs History')}</div>
        <div class="empty-state-desc">${isIssues ? t('no_issues_desc', 'No drift or errors recorded in recent inspection runs.') : t('no_runs_desc', 'No inspection runs recorded yet for this host.')}</div>
      </div>
    `;
    return;
  }

  const isIssues = filter === 'issues';
  const rowsHtml = runs.map(run => {
    let badgeClass = 'badge-healthy';
    let badgeText = 'OK';
    let borderColor = 'var(--success)';
    if (run.status === 'drift') {
      badgeClass = 'badge-warning';
      badgeText = 'DRIFT';
      borderColor = 'var(--warning)';
    } else if (run.status === 'down' || run.status === 'error') {
      badgeClass = 'badge-down';
      badgeText = run.status.toUpperCase();
      borderColor = 'var(--danger)';
    } else if (run.status === 'degraded') {
      badgeClass = 'badge-warning';
      badgeText = run.status.toUpperCase();
      borderColor = 'var(--warning)';
    }

    const durationColor = run.duration_ms < 1000 ? 'var(--success)' : (run.duration_ms < 3000 ? 'var(--warning)' : 'var(--danger)');
    const runTime = formatRelativeTime(run.started_at);
    const startDate = new Date(run.started_at);
    const fullDateStr = isNaN(startDate.getTime()) ? run.started_at : startDate.toLocaleString();

    return `
      <div class="card" style="padding: 12px 14px; margin-bottom: 8px; border-left: 3px solid ${borderColor};">
        <div style="display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 8px;">
          <div style="display: flex; align-items: center; gap: 8px;">
            <span class="badge ${badgeClass}" style="font-size: 0.7rem;">${badgeText}</span>
            <span style="font-size: var(--fs-xs); font-weight: 600; color: var(--text);">${escapeHtml(run.summary)}</span>
          </div>
          <div style="display: flex; align-items: center; gap: 12px; font-size: var(--fs-xs);">
            <span title="${t('run_duration', 'Duration')}" style="display: inline-flex; align-items: center; gap: 4px;">
              ${svgIcon('zap', 'icon-sm')}
              <strong style="color: ${durationColor}; font-family: var(--font-mono);">${run.duration_ms}ms</strong>
            </span>
            <span style="color: var(--text-muted); display: inline-flex; align-items: center; gap: 4px;" title="${fullDateStr}">
              ${svgIcon('clock', 'icon-sm')}
              <span style="font-family: var(--font-mono);">${fullDateStr}</span>
              <span style="opacity: 0.8;">(${runTime.relative})</span>
            </span>
          </div>
        </div>
        ${run.details ? `
          <details ${isIssues ? 'open' : ''} style="margin-top: 8px; border-top: 1px solid var(--card-border); padding-top: 6px;">
            <summary style="cursor: pointer; font-size: var(--fs-xs); color: var(--text-muted); font-weight: 500; display: inline-flex; align-items: center; gap: 4px;">
              ${svgIcon('search', 'icon-sm')} <span>${t('view_diagnostics', 'View Diagnostic Details')}</span>
            </summary>
            <pre class="b-drift-code-well" style="margin-top: 6px;"><code>${escapeHtml(run.details)}</code></pre>
          </details>
        ` : ''}
      </div>
    `;
  }).join('');

  container.innerHTML = `<div style="display: flex; flex-direction: column; gap: 6px;">${rowsHtml}</div>`;
}

function updatePkgCooldownUI(seconds, lastScannedIso) {
  if (S.pkgCooldownTimer) {
    clearInterval(S.pkgCooldownTimer);
    S.pkgCooldownTimer = null;
  }
  const infoEl = document.getElementById('pkgLastScannedTime');
  if (infoEl) {
    if (lastScannedIso) {
      const rel = formatRelativeTime(lastScannedIso);
      infoEl.innerHTML = `<b>${escapeHtml(rel.title)}</b> (${escapeHtml(rel.relative)})`;
    } else if (S.rawPackagesList && S.rawPackagesList.length > 0 && S.rawPackagesList[0].last_scanned_at) {
      const rel = formatRelativeTime(S.rawPackagesList[0].last_scanned_at);
      infoEl.innerHTML = `<b>${escapeHtml(rel.title)}</b> (${escapeHtml(rel.relative)})`;
    } else {
      infoEl.innerHTML = '<span style="color: var(--text-muted);">' + t('never_scanned', 'Belum pernah dipindai') + '</span>';
    }
  }
  const btn = document.getElementById('btnRescanPkgs');
  if (!btn) return;
  let rem = Number(seconds) || 0;
  if (rem > 0) {
    btn.disabled = true;
    btn.innerHTML = `<span style="display: inline-flex; align-items: center; gap: 4px;">${svgIcon('loader-circle', 'icon-sm')} <span>${t('cooldown_wait', 'Wait')} (${rem}s)</span></span>`;
    S.pkgCooldownTimer = setInterval(() => {
      rem--;
      if (rem <= 0) {
        clearInterval(S.pkgCooldownTimer);
        S.pkgCooldownTimer = null;
        btn.disabled = false;
        btn.innerHTML = `<span style="display: inline-flex; align-items: center; gap: 4px;">${svgIcon('refresh-cw', 'icon-sm')} <span>${t('rescan_packages', 'Rescan Packages')}</span></span>`;
      } else {
        btn.innerHTML = `<span style="display: inline-flex; align-items: center; gap: 4px;">${svgIcon('loader-circle', 'icon-sm')} <span>${t('cooldown_wait', 'Wait')} (${rem}s)</span></span>`;
      }
    }, 1000);
  } else {
    btn.disabled = false;
    btn.innerHTML = `<span style="display: inline-flex; align-items: center; gap: 4px;">${svgIcon('refresh-cw', 'icon-sm')} <span>${t('rescan_packages', 'Rescan Packages')}</span></span>`;
  }
}

async function renderPackagesTab() {
  const content = document.getElementById('tabContent');
  const d = window.detailData;
  if (!d || !d.host) return;

  S.activePackagesFilter = 'manual';
  S.packagesSearchQuery = '';

  content.innerHTML = `
    <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; flex-wrap: wrap; gap: 8px; flex-shrink: 0;">
      <div style="display: flex; align-items: center; gap: 10px; flex-wrap: wrap;">
        <h4 style="margin: 0; display: flex; align-items: center; gap: 6px;">
          ${svgIcon('package', 'icon-sm')} <span>${t('package_inventory', 'Package Inventory')}</span>
        </h4>
        <span id="pkgLastScannedInfo" style="font-size: var(--fs-xs); color: var(--text-muted); display: inline-flex; align-items: center; gap: 4px;">
          ${svgIcon('clock', 'icon-sm')} <span>${t('last_scanned_label', 'Last Scanned:')}</span> <span id="pkgLastScannedTime">...</span>
        </span>
        <div style="display: inline-flex; background: var(--surface-2); border: 1px solid var(--card-border); border-radius: var(--radius-sm); padding: 2px;">
          <button id="btnPkgFilterManual" class="btn btn-sm btn-primary" style="padding: 3px 10px; font-size: var(--fs-xs); display: inline-flex; align-items: center; gap: 4px;" onclick="filterPackages('manual')">
            ${svgIcon('star', 'icon-sm')} <span>${t('pkg_filter_manual', 'Manual Install')}</span>
          </button>
          <button id="btnPkgFilterDormant" class="btn btn-sm btn-ghost" style="padding: 3px 10px; font-size: var(--fs-xs); display: inline-flex; align-items: center; gap: 4px;" onclick="filterPackages('dormant')">
            ${svgIcon('pause', 'icon-sm')} <span>${t('pkg_filter_dormant', 'Dormant Packages')}</span>
          </button>
          <button id="btnPkgFilterAll" class="btn btn-sm btn-ghost" style="padding: 3px 10px; font-size: var(--fs-xs); display: inline-flex; align-items: center; gap: 4px;" onclick="filterPackages('all')">
            ${svgIcon('list', 'icon-sm')} <span>${t('pkg_filter_all', 'All Packages')}</span>
          </button>
        </div>
      </div>
      <div style="display: flex; align-items: center; gap: 8px;">
        <input type="text" id="pkgSearchInput" class="input" placeholder="${t('search_packages', 'Search package...')}" oninput="searchPackages(this.value)" style="height: 30px; font-size: var(--fs-xs); width: 180px;">
        <button id="btnUpgradePkgs" class="btn btn-sm btn-primary" onclick="openHostPackageUpgradePreview(${d.host.id})" style="display: inline-flex; align-items: center; gap: 4px;" ${d.host.os_info && d.host.os_info.toLowerCase().includes('darwin') ? 'disabled title="Package upgrade unsupported on macOS"' : ''}>
          ${svgIcon('package', 'icon-sm')} <span>${t('pkg_upgrade_btn', 'Upgrade packages')}</span>
        </button>
        <button id="btnInstallPkg" class="btn btn-sm" onclick="openPkgInstallModal(${d.host.id})" style="display: inline-flex; align-items: center; gap: 4px;" ${d.host.os_info && d.host.os_info.toLowerCase().includes('darwin') ? 'disabled title="Package install unsupported on macOS"' : ''}>
          ${svgIcon('plus', 'icon-sm')} <span>${t('pkg_install_btn', 'Install')}</span>
        </button>
        <button id="btnRescanPkgs" class="btn btn-sm" onclick="rescanHostPackages(${d.host.id})" style="display: inline-flex; align-items: center; gap: 4px;">
          ${svgIcon('refresh-cw', 'icon-sm')} <span>${t('rescan_packages', 'Rescan Packages')}</span>
        </button>
        <button id="btnCheckPkgUpdates" class="btn btn-sm" onclick="checkHostPackageUpdates(${d.host.id})" style="display: inline-flex; align-items: center; gap: 4px;" ${d.host.os_info && d.host.os_info.toLowerCase().includes('darwin') ? 'disabled title="Package update check unsupported on macOS"' : ''}>
          ${svgIcon('download', 'icon-sm')} <span>${t('pkg_check_updates_btn', 'Check for updates')}</span>
        </button>
      </div>
    </div>
    <div id="pkgUpdatesPanel" style="flex: 0 1 auto; max-height: 45%; overflow-y: auto; margin-bottom: 12px;"></div>
    <div id="packagesContainer" style="overflow-y: auto; flex: 1; min-height: 120px;">
      <div style="text-align: center; padding: 24px; color: var(--text-muted); font-size: var(--fs-xs);">${SP.html(t('loading', 'Loading...'))}</div>
    </div>
  `;
  await loadHostPackages(d.host.id);
  await loadHostPackageUpdates(d.host.id);
}

async function loadHostPackages(hostID) {
  const container = document.getElementById('packagesContainer');
  if (!container) return;
  try {
    const res = await fetch('/api/hosts/' + hostID + '/packages');
    if (res.ok) {
      S.rawPackagesList = await res.json() || [];
      const lastScanned = res.headers.get('X-Last-Scanned') || (S.rawPackagesList[0] ? S.rawPackagesList[0].last_scanned_at : null);
      const cooldownRem = parseInt(res.headers.get('X-Cooldown-Remaining') || '0', 10);
      displayPackagesTable(hostID);
      updatePkgCooldownUI(cooldownRem, lastScanned);
    } else {
      container.innerHTML = '<div style="text-align: center; color: var(--danger); padding: 20px;">Failed to load packages</div>';
    }
  } catch(e) {
    container.innerHTML = '<div style="text-align: center; color: var(--danger); padding: 20px;">' + escapeHtml(e.message) + '</div>';
  }
}

async function rescanHostPackages(hostID) {
  if (S.rawPackagesList && S.rawPackagesList.length > 0) {
    const ok = await showConfirm({
      title: t('rescan_packages', 'Rescan Packages'),
      message: t('confirm_rescan_msg', 'Pindai ulang paket perangkat lunak di server ini? Pantau akan melakukan inspeksi SSH agentless.'),
      confirmText: t('start_scan', 'Mulai Pindai'),
      danger: false
    });
    if (!ok) return;
  }

  // SP.busy shows the spinner and the elapsed time; restore() puts the button back in the finally.
  const restore = SP.busy(document.getElementById('btnRescanPkgs'), t('upd_rescanning_pkgs'));
  let cooldownApplied = false;
  try {
    const res = await fetch('/api/hosts/' + hostID + '/packages/scan', { method: 'POST' });
    if (res.ok) {
      S.rawPackagesList = await res.json() || [];
      displayPackagesTable(hostID);
      showToast('Package scan complete', 'success');
      updatePkgCooldownUI(60, new Date().toISOString());
      cooldownApplied = true;
    } else if (res.status === 429) {
      const errData = await res.json().catch(() => ({}));
      showToast(errData.error || 'Cooldown scan aktif, mohon tunggu sebentar', 'warning');
      updatePkgCooldownUI(errData.cooldown_remaining || 60, null);
      cooldownApplied = true;
    } else if (res.status === 409) {
      showToast('Pemindaian paket sedang berlangsung di latar belakang', 'warning');
    } else {
      const errData = await res.json().catch(() => ({}));
      showToast('Scan failed: ' + (errData.error || res.statusText), 'error');
    }
  } catch(e) {
    showToast('Scan error: ' + e.message, 'error');
  } finally {
    // The cooldown repaints the button every second: restoring the old markup would wipe it.
    if (!cooldownApplied) restore();
    else {
      const b = document.getElementById('btnRescanPkgs');
      if (b) b.disabled = true;
    }
  }
}

async function loadHostPackageUpdates(hostID) {
  try {
    const res = await fetch('/api/hosts/' + hostID + '/packages/updates');
    if (!res.ok) return;
    const data = await res.json().catch(() => ({}));
    S.pkgUpdateCheckData = data.checked ? data.check : null;
    const rem = parseInt(res.headers.get('X-Cooldown-Remaining') || '0', 10);
    S.pkgUpdateCooldownRem = rem > 0 ? rem : 0;
    renderPkgUpdatesPanel(hostID);
  } catch(e) {
    // A failed read leaves the panel empty; the operator can still run a check.
  }
}

function pkgUpdateCooldownPaint(rem) {
  const btn = document.getElementById('btnCheckPkgUpdates');
  if (!btn) return;
  if (rem > 0) {
    btn.disabled = true;
    btn.innerHTML = `<span style="display: inline-flex; align-items: center; gap: 4px;">${svgIcon('loader-circle', 'icon-sm')} <span>${t('cooldown_wait', 'Wait')} (${rem}s)</span></span>`;
  } else {
    btn.disabled = false;
    btn.innerHTML = `${svgIcon('download', 'icon-sm')} <span>${t('pkg_check_updates_btn', 'Check for updates')}</span>`;
  }
}

async function checkHostPackageUpdates(hostID) {
  const restore = SP.busy(document.getElementById('btnCheckPkgUpdates'), t('pkg_checking_updates', 'Checking...'));
  try {
    const res = await fetch('/api/hosts/' + hostID + '/packages/updates/check', { method: 'POST' });
    const data = await res.json().catch(() => ({}));
    if (res.ok && data.ok) {
      S.pkgUpdateCheckData = data.check;
      S.pkgUpdateCooldownRem = 60;
      startPkgUpdateCooldown();
      renderPkgUpdatesPanel(hostID);
      const n = (data.check && data.check.pending_total) || 0;
      showToast(n ? t('pkg_updates_found', 'Update available').replace('{n}', n) : t('pkg_all_current', 'Everything is up to date'), n ? 'warning' : 'success');
      // The table shows the available version per package, so it needs the fresh check.
      displayPackagesTable(hostID);
    } else if (res.status === 429) {
      S.pkgUpdateCooldownRem = data.cooldown_remaining || 60;
      startPkgUpdateCooldown();
      showToast(data.error || t('pkg_update_cooldown', 'Please wait a moment before checking again'), 'warning');
    } else {
      showToast(t('pkg_check_failed', 'Update check failed') + ': ' + (data.error || res.statusText), 'error');
    }
  } catch(e) {
    showToast(t('pkg_check_failed', 'Update check failed') + ': ' + e.message, 'error');
  } finally {
    if (S.pkgUpdateCooldownRem <= 0) restore();
  }
}

function startPkgUpdateCooldown() {
  if (S.pkgUpdateCooldownTimer) {
    clearInterval(S.pkgUpdateCooldownTimer);
    S.pkgUpdateCooldownTimer = null;
  }
  pkgUpdateCooldownPaint(S.pkgUpdateCooldownRem);
  S.pkgUpdateCooldownTimer = setInterval(() => {
    S.pkgUpdateCooldownRem--;
    if (S.pkgUpdateCooldownRem <= 0) {
      S.pkgUpdateCooldownRem = 0;
      clearInterval(S.pkgUpdateCooldownTimer);
      S.pkgUpdateCooldownTimer = null;
    }
    pkgUpdateCooldownPaint(S.pkgUpdateCooldownRem);
  }, 1000);
}

function renderPkgUpdatesPanel(hostID) {
  const panel = document.getElementById('pkgUpdatesPanel');
  if (!panel) return;
  const c = S.pkgUpdateCheckData;
  if (!c) {
    panel.innerHTML = '';
    return;
  }

  const rel = c.checked_at ? formatRelativeTime(c.checked_at) : null;
  const when = rel ? `<b>${escapeHtml(rel.title)}</b> (${escapeHtml(rel.relative)})` : t('unknown', 'Unknown');

  // A failed check is never "up to date": the operator sees why it could not be read.
  if (c.error) {
    panel.innerHTML = `
      <div class="card" style="padding: 12px 14px; border-color: var(--danger);">
        <div style="display: flex; align-items: center; gap: 6px; font-size: var(--fs-xs); font-weight: 600; color: var(--danger);">
          ${svgIcon('circle-alert', 'icon-sm')} <span>${t('pkg_update_check_failed', 'Update check failed')}</span>
        </div>
        <div style="font-size: var(--fs-xs); color: var(--text-muted); margin-top: 6px;">${escapeHtml(c.error)}</div>
      </div>`;
    return;
  }

  if (!c.supported) {
    panel.innerHTML = `
      <div class="card" style="padding: 12px 14px;">
        <div style="display: flex; align-items: center; gap: 6px; font-size: var(--fs-xs); color: var(--text-muted);">
          ${svgIcon('info', 'icon-sm')} <span>${t('pkg_update_unsupported', 'The package manager of this host cannot report pending updates')}</span>
        </div>
        ${c.note ? `<div style="font-size: var(--fs-xs); color: var(--text-muted); margin-top: 6px;">${escapeHtml(c.note)}</div>` : ''}
      </div>`;
    return;
  }

  const stale = c.repo_cache_age_hours > 168;
  const noUpdates = (c.pending_total || 0) === 0;
  const head = `
    <div class="card" style="padding: 12px 14px;">
      <div style="display: flex; align-items: center; justify-content: space-between; gap: 8px; flex-wrap: wrap;">
        <div style="display: flex; align-items: center; gap: 8px; font-size: var(--fs-xs); font-weight: 600;">
          ${svgIcon('package', 'icon-sm')}
          <span title="${escapeHtml(t('pkg_check_readonly_note', 'This check only reads; it never installs anything. Use Upgrade packages to apply them.'))}">${t('pkg_updates_title', 'Pending package updates')}</span>
          <span class="badge ${noUpdates ? 'badge-healthy' : 'badge-warning'}">${noUpdates ? t('pkg_all_current', 'Everything is up to date') : (c.pending_total || 0)}</span>
          ${c.pending_security > 0 ? `<span class="badge badge-danger">${c.pending_security} ${t('pkg_security_badge', 'security')}</span>` : ''}
          ${c.reboot_required ? `<span class="badge badge-warning">${t('pkg_reboot_badge', 'reboot required')}</span>` : ''}
        </div>
        <div style="font-size: var(--fs-xs); color: var(--text-muted); display: inline-flex; align-items: center; gap: 4px;">
          ${svgIcon('clock', 'icon-sm')} <span>${t('pkg_checked_at', 'Checked:')}</span> <span>${when}</span>
        </div>
      </div>`;

  const notes = [];
  if (c.note) notes.push(c.note);
  if (stale) {
    notes.push(t('pkg_stale_cache', 'The package metadata on the host is older than 7 days, so newer releases may be missing.'));
  }
  const notesHtml = notes.length ? `
    <div style="font-size: var(--fs-xs); color: var(--text-muted); margin-top: 8px;">${notes.map(n => escapeHtml(n)).join(' ')}</div>` : '';

  // A chip list, not a nested table: the package inventory below is the main surface of this tab, and a
  // second table here would squeeze it down to a couple of visible rows.
  const listHtml = noUpdates ? '' : `
    <div style="display: flex; flex-wrap: wrap; gap: 4px; margin-top: 6px; max-height: 120px; overflow-y: auto;">
      ${(c.packages || []).map(p => `
        <span title="${escapeHtml(p.name)}: ${escapeHtml(p.from || '')} → ${escapeHtml(p.to || '')}" style="display: inline-flex; align-items: center; gap: 4px; font-size: var(--fs-xs); padding: 1px 6px; border: 1px solid var(--card-border); border-radius: var(--radius-sm); background: var(--surface-2);">
          <span style="font-weight: 600;">${escapeHtml(p.name)}</span>
          <span style="font-family: var(--font-mono); color: var(--primary);">${escapeHtml(p.to || '-')}</span>
          ${p.security ? `<span class="badge badge-danger" style="font-size: 0.62rem;">${t('pkg_type_security', 'Security')}</span>` : ''}
        </span>`).join('')}
    </div>`;

  panel.innerHTML = head + notesHtml + listHtml;
}

function filterPackages(type) {
  S.activePackagesFilter = type;
  const btnManual = document.getElementById('btnPkgFilterManual');
  const btnDormant = document.getElementById('btnPkgFilterDormant');
  const btnAll = document.getElementById('btnPkgFilterAll');
  if (btnManual && btnDormant && btnAll) {
    btnManual.className = type === 'manual' ? 'btn btn-sm btn-primary' : 'btn btn-sm';
    btnDormant.className = type === 'dormant' ? 'btn btn-sm btn-primary' : 'btn btn-sm';
    btnAll.className = type === 'all' ? 'btn btn-sm btn-primary' : 'btn btn-sm';
  }
  const d = window.detailData;
  if (d && d.host) displayPackagesTable(d.host.id);
}

function searchPackages(query) {
  S.packagesSearchQuery = (query || '').toLowerCase().trim();
  const d = window.detailData;
  if (d && d.host) displayPackagesTable(d.host.id);
}

function displayPackagesTable(hostID) {
  const container = document.getElementById('packagesContainer');
  if (!container) return;

  let filtered = S.rawPackagesList.slice();

  if (S.activePackagesFilter === 'manual') {
    filtered = filtered.filter(p => p.is_manual);
  } else if (S.activePackagesFilter === 'dormant') {
    filtered = filtered.filter(p => {
      if (!p.is_manual) return false;
      if (p.service_status) {
        const s = p.service_status.toLowerCase();
        return s.includes('inactive') || s.includes('dead') || s.includes('failed') || s.includes('disabled');
      }
      return false;
    });
  }

  if (S.packagesSearchQuery) {
    filtered = filtered.filter(p => (p.name && p.name.toLowerCase().includes(S.packagesSearchQuery)) || (p.version && p.version.toLowerCase().includes(S.packagesSearchQuery)));
  }

  if (filtered.length === 0) {
    container.innerHTML = `
      <div class="empty-state">
        <div class="empty-state-icon">${svgIcon('package', 'icon-xl')}</div>
        <div class="empty-state-title">${S.rawPackagesList.length === 0 ? 'No Packages Scanned Yet' : 'No Packages Match Filter'}</div>
        <div class="empty-state-desc">${S.rawPackagesList.length === 0 ? 'Click "Rescan Packages" to perform an agentless inventory scan on this host.' : 'Try changing your search keywords or filter category.'}</div>
        ${S.rawPackagesList.length === 0 ? `<button class="btn btn-primary" onclick="rescanHostPackages(${hostID})" style="margin-top: 10px; display: inline-flex; align-items: center; gap: 4px;">${svgIcon('refresh-cw', 'icon-sm')} <span>${t('rescan_packages', 'Rescan Packages')}</span></button>` : ''}
      </div>
    `;
    return;
  }

  container.innerHTML = `
    <div class="card" style="padding: 0; overflow: hidden;">
      <div style="overflow-x: auto;">
        <table style="width: 100%; font-size: var(--fs-xs); margin: 0;">
          <thead>
            <tr>
              <th>${t('pkg_name', 'Package Name')}</th>
              <th>${t('version', 'Version')}</th>
              <th>${t('pkg_available_version', 'Available')}</th>
              <th>${t('size', 'Disk Size')}</th>
              <th>${t('install_type', 'Type')}</th>
              <th>${t('service_status', 'Service Status')}</th>
              <th style="text-align: right;">${t('action', 'Action')}</th>
            </tr>
          </thead>
          <tbody>
            ${filtered.map(p => {
              let svcBadge = '<span style="color: var(--text-muted);">-</span>';
              if (p.service_status) {
                const s = p.service_status.toLowerCase();
                const isActive = s.includes('active') && !s.includes('inactive');
                const isDormant = s.includes('inactive') || s.includes('dead') || s.includes('failed') || s.includes('disabled');
                const cls = isActive ? 'badge-healthy' : (isDormant ? 'badge-warning' : 'badge-unknown');
                svcBadge = `<span class="badge ${cls}" title="${escapeHtml(p.service_name || '')}">${escapeHtml(p.service_status)}</span>`;
              }
              const typeBadge = p.is_manual 
                ? '<span class="badge badge-info">Manual</span>' 
                : '<span class="badge badge-unknown">Base</span>';
              
              // Security update highlight
              const isSecUpdate = p.security_update || (window.currentSecurityScan && window.currentSecurityScan.findings && window.currentSecurityScan.findings.some(f => f.package === p.name));

              // The update check knows which installed package has a newer version available.
              const avail = S.pkgUpdateCheckData && (S.pkgUpdateCheckData.packages || []).find(u => u.name === p.name);
              const availCell = avail
                ? `<span style="font-family: var(--font-mono); color: var(--primary);">${escapeHtml(avail.to || '-')}</span>${avail.security ? `<span class="badge badge-danger" style="margin-left: 4px; font-size: 0.68rem;">${t('pkg_type_security', 'Security')}</span>` : ''}`
                : '<span style="color: var(--text-muted);">-</span>';

              return `
                <tr>
                  <td>
                    <span style="font-weight: 600; color: var(--text);">${escapeHtml(p.name)}</span>
                    ${isSecUpdate ? `<span class="badge badge-danger" style="margin-left: 6px; font-size: 0.68rem;">Security Update</span>` : ''}
                  </td>
                  <td style="font-family: var(--font-mono); color: var(--text-muted);">${escapeHtml(p.version)}</td>
                  <td>${availCell}</td>
                  <td style="font-family: var(--font-mono); font-weight: 500;">${p.size_bytes > 0 ? formatBytes(p.size_bytes) : '-'}</td>
                  <td>${typeBadge}</td>
                  <td>${svcBadge}</td>
                  <td style="text-align: right;">
                    <button class="btn btn-sm btn-ghost btn-icon" data-pkg="${escapeHtml(p.name)}" onclick="pkgAction(${hostID}, 'upgrade', this.dataset.pkg)" title="${t('pkg_upgrade_one', 'Upgrade')}">
                      ${svgIcon('arrow-up', 'icon-sm')}
                    </button>
                    <button class="btn btn-sm btn-ghost btn-icon" data-pkg="${escapeHtml(p.name)}" onclick="pkgAction(${hostID}, 'remove', this.dataset.pkg)" title="${t('pkg_remove_one', 'Delete')}" style="color: var(--danger);">
                      ${svgIcon('trash-2', 'icon-sm')}
                    </button>
                  </td>
                </tr>
              `;
            }).join('')}
          </tbody>
        </table>
      </div>
    </div>
  `;
}

function secCooldownPaint(rem) {
  // The button is looked up on every paint: a re-render of the tab replaces the element.
  const btn = document.getElementById('btnScanSecurity');
  if (!btn) return;
  if (rem > 0) {
    btn.disabled = true;
    btn.innerHTML = `<span style="display: inline-flex; align-items: center; gap: 4px;">${svgIcon('loader-circle', 'icon-sm')} <span>${t('cooldown_wait', 'Wait')} (${rem}s)</span></span>`;
  } else {
    btn.disabled = false;
    btn.innerHTML = `<span style="display: inline-flex; align-items: center; gap: 4px;">${svgIcon('refresh-cw', 'icon-sm')} <span>${t('sec_scan_now', 'Scan Now')}</span></span>`;
  }
}

function updateSecCooldownUI(seconds) {
  if (S.secCooldownTimer) {
    clearInterval(S.secCooldownTimer);
    S.secCooldownTimer = null;
  }
  S.secCooldownRem = Number(seconds) || 0;
  if (S.secCooldownRem > 0) {
    secCooldownPaint(S.secCooldownRem);
    S.secCooldownTimer = setInterval(() => {
      S.secCooldownRem--;
      if (S.secCooldownRem <= 0) {
        clearInterval(S.secCooldownTimer);
        S.secCooldownTimer = null;
      }
      secCooldownPaint(S.secCooldownRem);
    }, 1000);
  } else {
    secCooldownPaint(0);
  }
}

async function loadSecurityTab() {
  const host = S.currentHost;
  const content = document.getElementById('tabContent');
  if (!host || !content) return;

  content.innerHTML = `<div style="text-align: center; padding: 40px; color: var(--text-muted);"><span class="spinner" style="display:inline-block; margin-right:8px;">${svgIcon('loader-circle', 'icon')}</span><span>${t('loading', 'Loading...')}</span></div>`;

  let res, data;
  try {
    res = await fetch('/api/hosts/' + host.id + '/security');
    data = await res.json();
  } catch (e) {
    if (S.currentTab !== 'security' || !S.currentHost || S.currentHost.id !== host.id) return;
    content.innerHTML = `
      <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; flex-shrink: 0;">
        <h4 style="margin: 0; display: inline-flex; align-items: center; gap: 6px;">${svgIcon('shield', 'icon')} <span>${t('tab_security', 'Security')}</span></h4>
        <button id="btnScanSecurity" class="btn btn-sm btn-primary" onclick="runHostSecurityScan(${host.id})" style="display: inline-flex; align-items: center; gap: 4px;">${svgIcon('refresh-cw', 'icon-sm')} <span>${t('sec_scan_now', 'Scan Now')}</span></button>
      </div>
      <div style="background: var(--danger-soft); border: 1px solid var(--danger-border); border-radius: var(--radius); padding: 16px; color: var(--danger);">
        <div style="font-weight: 600; margin-bottom: 4px; display: inline-flex; align-items: center; gap: 6px;">${svgIcon('alert-triangle', 'icon-sm')} <span>${t('sec_scan_failed', 'Security scan failed')}</span></div>
        <div style="font-size: 0.82rem; font-family: var(--font-mono);">${escapeHtml(e.message)}</div>
      </div>`;
    appendSecurityFooter(content);
    return;
  }

  if (S.currentTab !== 'security' || !S.currentHost || S.currentHost.id !== host.id) return;

  const cooldownHeader = res.headers ? res.headers.get('X-Scan-Cooldown-Remaining') : null;
  const cooldownRem = parseInt(cooldownHeader || '0', 10);

  if (!res.ok) {
    content.innerHTML = `
      <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; flex-shrink: 0;">
        <h4 style="margin: 0; display: inline-flex; align-items: center; gap: 6px;">${svgIcon('shield', 'icon')} <span>${t('tab_security', 'Security')}</span></h4>
        <button id="btnScanSecurity" class="btn btn-sm btn-primary" onclick="runHostSecurityScan(${host.id})" style="display: inline-flex; align-items: center; gap: 4px;">${svgIcon('refresh-cw', 'icon-sm')} <span>${t('sec_scan_now', 'Scan Now')}</span></button>
      </div>
      <div style="background: var(--danger-soft); border: 1px solid var(--danger-border); border-radius: var(--radius); padding: 16px; color: var(--danger);">
        <div style="font-weight: 600; margin-bottom: 4px; display: inline-flex; align-items: center; gap: 6px;">${svgIcon('alert-triangle', 'icon-sm')} <span>${t('sec_scan_failed', 'Security scan failed')}</span></div>
        <div style="font-size: 0.82rem;">${escapeHtml(data?.error || res.statusText)}</div>
      </div>`;
    appendSecurityFooter(content);
    if (cooldownRem > 0) updateSecCooldownUI(cooldownRem);
    return;
  }

  if (!data.scanned || !data.scan) {
    content.innerHTML = `
      <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; flex-shrink: 0;">
        <h4 style="margin: 0; display: inline-flex; align-items: center; gap: 6px;">${svgIcon('shield', 'icon')} <span>${t('tab_security', 'Security')}</span></h4>
        <button id="btnScanSecurity" class="btn btn-sm btn-primary" onclick="runHostSecurityScan(${host.id})" style="display: inline-flex; align-items: center; gap: 4px;">${svgIcon('refresh-cw', 'icon-sm')} <span>${t('sec_scan_now', 'Scan Now')}</span></button>
      </div>
      <div class="empty-state">
        <div class="empty-state-icon">${svgIcon('shield-alert', 'icon-xl')}</div>
        <div class="empty-state-title">${t('sec_never_scanned_title', 'No Security Scan Available')}</div>
        <div class="empty-state-desc">${t('sec_never_scanned_desc', 'This host has not been scanned for security updates and hardening checks yet.')}</div>
        <div style="margin-top: 16px;">
          <button class="btn btn-primary" onclick="runHostSecurityScan(${host.id})" style="display: inline-flex; align-items: center; gap: 4px;">
            ${svgIcon('search', 'icon-sm')} <span>${t('sec_scan_now', 'Scan Now')}</span>
          </button>
        </div>
      </div>`;
    appendSecurityFooter(content);
    if (cooldownRem > 0) updateSecCooldownUI(cooldownRem);
    return;
  }

  S.currentSecurityScan = data.scan;
  renderSecurityData(data.scan, host.id, data.intel);
  if (cooldownRem > 0) updateSecCooldownUI(cooldownRem);
}

function renderSecurityData(scan, hostID, intel) {
  const content = document.getElementById('tabContent');
  if (!content) return;
  S.currentSecurityScan = scan;
  S.currentSecurityIntel = intel || null;

  const isStaleCache = scan.repo_cache_age_hours > 168;

  let statusBadge = '<span class="badge badge-healthy">OK</span>';
  if (scan.status === 'partial') {
    statusBadge = '<span class="badge badge-warning">PARTIAL</span>';
  } else if (scan.status === 'unsupported') {
    statusBadge = '<span class="badge badge-unknown">UNSUPPORTED</span>';
  } else if (scan.status === 'error') {
    statusBadge = '<span class="badge badge-down">ERROR</span>';
  }

  let bannerHtml = '';
  if (scan.status === 'partial') {
    bannerHtml = `
      <div class="sec-banner sec-banner-warn">
        <div style="font-weight: 600; display: flex; align-items: center; gap: 6px; margin-bottom: ${scan.notes && scan.notes.length ? '6px' : '0'};">
          ${svgIcon('triangle-alert', 'icon-sm')} <span>${t('sec_banner_partial', 'Partial Scan Coverage')}</span>
        </div>
        ${scan.notes && scan.notes.length ? `<ul style="margin: 0; padding-left: 20px; font-size: var(--fs-xs); color: var(--warning);">${scan.notes.map(n => `<li>${escapeHtml(n)}</li>`).join('')}</ul>` : ''}
      </div>`;
  } else if (scan.status === 'unsupported') {
    bannerHtml = `
      <div class="sec-banner sec-banner-info">
        <div style="font-weight: 600; display: flex; align-items: center; gap: 6px;">
          ${svgIcon('info', 'icon-sm')} <span>${t('sec_banner_unsupported', 'Security Updates Unsupported on this OS')}</span>
        </div>
        <div style="font-size: var(--fs-xs); color: var(--accent-text); margin-top: 4px;">
          ${t('sec_banner_unsupported_desc', 'Automated package security update inspection is not available for this distribution ({os} / {pm}). Hardening checks below are still evaluated.').replace('{os}', escapeHtml(scan.os_family || 'unknown')).replace('{pm}', escapeHtml(scan.package_manager || 'unknown'))}
        </div>
        ${scan.notes && scan.notes.length ? `<ul style="margin: 6px 0 0 0; padding-left: 20px; font-size: var(--fs-xs); color: var(--accent-text);">${scan.notes.map(n => `<li>${escapeHtml(n)}</li>`).join('')}</ul>` : ''}
      </div>`;
  } else if (scan.status === 'error') {
    bannerHtml = `
      <div class="sec-banner sec-banner-danger">
        <div style="font-weight: 600; display: flex; align-items: center; gap: 6px;">
          ${svgIcon('circle-x', 'icon-sm')} <span>${t('sec_banner_error', 'Security Scan Error')}</span>
        </div>
        <div style="font-size: var(--fs-xs); color: var(--danger); margin-top: 4px; font-family: var(--font-mono);">${escapeHtml(scan.error || 'Unknown scan error')}</div>
      </div>`;
  }

  const notAssessed = scan.status === 'unsupported' || scan.status === 'error';
  const recency = scan.scanned_at ? formatRelativeTime(scan.scanned_at) : { title: '-', relative: '-' };
  const cardsHtml = `
    <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 12px; margin-bottom: 16px;">
      <div class="card" style="padding: 14px; gap: 4px;">
        <div class="sec-stat-label">${t('sec_pending_updates', 'Pending Security Updates')}</div>
        <div style="font-size: 1.6rem; font-weight: 700; color: ${notAssessed ? 'var(--text-muted)' : (scan.pending_security > 0 ? 'var(--danger)' : 'var(--success)')}; font-family: var(--font-mono);">
          ${notAssessed ? 'N/A' : (scan.pending_security || 0)}
        </div>
        <div style="font-size: var(--fs-xs); color: var(--text-muted);">
          ${notAssessed ? t('sec_not_assessed', 'Not assessed') : (scan.pending_total > 0 ? `${scan.pending_total} ${t('sec_total_packages', 'total upgradable')}` : t('sec_no_pending', 'Up to date'))}
        </div>
      </div>

      <div class="card" style="padding: 14px; gap: 4px;">
        <div class="sec-stat-label" style="margin-bottom: 4px;">${t('sec_severity_breakdown', 'Severity Breakdown')}</div>
        ${notAssessed ? `<div style="color: var(--text-muted); font-size: var(--fs-sm);">${t('sec_not_assessed', 'Not assessed')}</div>` : `<div style="display: flex; gap: 4px; flex-wrap: wrap;">
          <span class="badge badge-sec-critical" title="Critical">${t('sec_sev_crit_short', 'Crit')}: ${scan.severity?.critical || 0}</span>
          <span class="badge badge-sec-important" title="Important">${t('sec_sev_imp_short', 'Imp')}: ${scan.severity?.important || 0}</span>
          <span class="badge badge-sec-moderate" title="Moderate">${t('sec_sev_mod_short', 'Mod')}: ${scan.severity?.moderate || 0}</span>
          <span class="badge badge-sec-low" title="Low">${t('sec_sev_low_short', 'Low')}: ${scan.severity?.low || 0}</span>
          <span class="badge badge-sec-unknown" title="Unknown">${t('sec_sev_unk_short', 'Unk')}: ${scan.severity?.unknown || 0}</span>
        </div>`}
      </div>

      <div class="card" style="padding: 14px; gap: 4px;">
        <div class="sec-stat-label">${t('sec_reboot_status', 'Reboot Status')}</div>
        <div style="margin-top: 6px;">
          ${notAssessed
            ? `<span style="color: var(--text-muted); font-size: var(--fs-sm);">${t('unknown', 'Unknown')}</span>`
            : (scan.reboot_required
              ? `<span class="badge badge-danger" style="font-size: var(--fs-xs); padding: 4px 8px; display: inline-flex; align-items: center; gap: 4px;">${svgIcon('triangle-alert', 'icon-sm')} ${t('sec_reboot_yes', 'Reboot Required')}</span>`
              : `<span class="badge badge-healthy" style="font-size: var(--fs-xs); padding: 4px 8px; display: inline-flex; align-items: center; gap: 4px;">${svgIcon('check', 'icon-sm')} ${t('sec_reboot_no', 'No Reboot Required')}</span>`)
          }
        </div>
      </div>

      <div class="card" style="padding: 14px; gap: 4px;">
        <div class="sec-stat-label">${t('sec_repo_cache_age', 'Repo Cache Age')}</div>
        <div style="font-size: 1.15rem; font-weight: 700; margin-top: 4px;">
          ${scan.repo_cache_age_hours < 0 
            ? `<span style="color: var(--text-muted); font-size: var(--fs-sm);">${t('unknown', 'Unknown')}</span>`
            : (isStaleCache 
                ? `<span style="color: var(--warning); display: inline-flex; align-items: center; gap: 4px;" title="Cache is older than 7 days">${svgIcon('triangle-alert', 'icon-sm')} ${scan.repo_cache_age_hours}h (${Math.round(scan.repo_cache_age_hours / 24)}d)</span>`
                : `<span style="color: var(--success); display: inline-flex; align-items: center; gap: 4px;">${svgIcon('check', 'icon-sm')} ${scan.repo_cache_age_hours}h</span>`
              )
          }
        </div>
      </div>

      <div class="card" style="padding: 14px; gap: 4px;">
        <div class="sec-stat-label">${t('sec_system_info', 'System & Scan Time')}</div>
        <div style="font-size: var(--fs-xs); margin-top: 4px; color: var(--text);">
          <b>${escapeHtml(scan.os_family || 'unknown')}</b> / <span>${escapeHtml(scan.package_manager || 'unknown')}</span>
        </div>
        <div style="font-size: var(--fs-xs); color: var(--text-muted); margin-top: 2px;" title="${escapeHtml(recency.title)}">
          ${escapeHtml(recency.relative)}
        </div>
      </div>
    </div>
  `;

  const findings = scan.findings || [];
  const findingsHtml = `
    <div class="card" style="padding: 16px;">
      <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; flex-wrap: wrap; gap: 8px;">
        <h4 style="margin: 0; display: flex; align-items: center; gap: 6px;">${svgIcon('search', 'icon-sm')} <span>${t('sec_findings_title', 'Security Findings')}</span> (<span id="secFindingsCount">${findings.length}</span>)</h4>
        <div style="display: flex; align-items: center; gap: 8px; flex-wrap: wrap;">
          <input type="text" id="secFindingsFilter" class="input" placeholder="${t('search_findings_ph', 'Filter packages or advisories...')}" oninput="filterSecurityFindings()" style="height: 30px; font-size: var(--fs-xs); width: 180px;">
          <select id="secFindingsSort" class="input" onchange="filterSecurityFindings()" style="height: 30px; font-size: var(--fs-xs);">
            <option value="sev_desc">${t('sort_sev_desc', 'Severity: High to Low')}</option>
            <option value="sev_asc">${t('sort_sev_asc', 'Severity: Low to High')}</option>
            <option value="pkg_asc">${t('sort_pkg_asc', 'Package Name (A-Z)')}</option>
            ${window.VI ? `<option value="exploit">${t('vi_sort', 'Exploitation: KEV, then EPSS')}</option>` : ''}
          </select>
        </div>
      </div>
      <div id="secFindingsContainer"></div>
    </div>
  `;

  const hardeningList = scan.hardening || [];
  let hardeningContent = '';
  if (hardeningList.length === 0) {
    hardeningContent = `<div style="color: var(--text-muted); font-size: 0.85rem;">${t('sec_no_hardening', 'No hardening checks reported')}</div>`;
  } else {
    hardeningContent = `
      <div style="overflow-x: auto;">
        <table style="width: 100%; border-collapse: collapse; font-size: 0.85rem;">
          <thead>
            <tr style="border-bottom: 1px solid var(--card-border); color: var(--text-muted); text-align: left;">
              <th style="padding: 8px 12px;">${t('sec_check', 'Check')}</th>
              <th style="padding: 8px 12px; width: 110px;">${t('sec_status', 'Status')}</th>
              <th style="padding: 8px 12px;">${t('sec_detail', 'Detail')}</th>
            </tr>
          </thead>
          <tbody>
            ${hardeningList.map(h => {
              let badgeCls = 'badge-sec-unknown';
              let statusText = (h.status || 'unknown').toLowerCase();
              if (statusText === 'pass') badgeCls = 'badge-success';
              else if (statusText === 'warn') badgeCls = 'badge-warning';
              else if (statusText === 'fail') badgeCls = 'badge-danger';
              const title = t('hardening_' + h.id, h.title || h.id);
              return `
                <tr style="border-bottom: 1px solid var(--card-border);">
                  <td style="padding: 8px 12px; font-weight: 600; color: var(--text);">${escapeHtml(title)}</td>
                  <td style="padding: 8px 12px;"><span class="badge ${badgeCls}">${escapeHtml(statusText.toUpperCase())}</span></td>
                  <td style="padding: 8px 12px; color: var(--text-muted); font-size: 0.82rem; font-family: var(--font-mono);">${escapeHtml(h.detail || '-')}</td>
                </tr>
              `;
            }).join('')}
          </tbody>
        </table>
      </div>
    `;
  }
  const hardeningHtml = `
    <div class="card" style="padding: 16px; margin-top: 16px;">
      <h4 style="margin: 0 0 12px 0; display: flex; align-items: center; gap: 6px;">${svgIcon('shield-check', 'icon-sm')} <span>${t('sec_hardening_title', 'Hardening & Baseline Checks')}</span></h4>
      ${hardeningContent}
    </div>
  `;

  let notesHtml = '';
  if (scan.notes && scan.notes.length > 0) {
    notesHtml = `
      <div class="card" style="padding: 16px; margin-top: 16px;">
        <h4 style="margin: 0 0 10px 0; display: flex; align-items: center; gap: 6px;">${svgIcon('file-text', 'icon-sm')} <span>${t('sec_notes_title', 'Coverage & Operational Notes')}</span></h4>
        <ul style="margin: 0; padding-left: 20px; color: var(--text-muted); font-size: var(--fs-xs); line-height: 1.6;">
          ${scan.notes.map(n => `<li>${escapeHtml(n)}</li>`).join('')}
        </ul>
      </div>
    `;
  }

  content.innerHTML = `
    <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 14px; flex-wrap: wrap; gap: 8px; flex-shrink: 0;">
      <div style="display: flex; align-items: center; gap: 8px;">
        <h4 style="margin: 0; display: flex; align-items: center; gap: 6px;">${svgIcon('shield', 'icon-sm')} <span>${t('tab_security', 'Security')}</span></h4>
        ${statusBadge}
      </div>
      <div style="display: flex; gap: 8px; align-items: center;">
        <button id="btnUpgradePkgsSec" class="btn btn-sm btn-primary" onclick="openHostPackageUpgradePreview(${hostID})" style="display: inline-flex; align-items: center; gap: 4px;" ${(window.detailData && window.detailData.host && window.detailData.host.os_info && window.detailData.host.os_info.toLowerCase().includes('darwin')) ? 'disabled title="Package upgrade unsupported on macOS"' : ''}>
          ${svgIcon('package', 'icon-sm')} <span>${t('pkg_upgrade_btn', 'Upgrade packages')}</span>
        </button>
        ${window.AG ? AG.btnNum('askSecurity', hostID) : ''}
        ${window.AC ? `<button class="btn btn-sm btn-ghost" onclick="AC.openFleet()" style="display: inline-flex; align-items: center; gap: 4px;">${svgIcon('key-round', 'icon-sm')} <span>${t('ac_fleet', 'Fleet keys')}</span></button>` : ''}
        <button id="btnScanSecurity" class="btn btn-sm" onclick="runHostSecurityScan(${hostID})" style="display: inline-flex; align-items: center; gap: 4px;">
          ${svgIcon('refresh-cw', 'icon-sm')} <span>${t('sec_scan_now', 'Scan Now')}</span>
        </button>
      </div>
    </div>
    <div style="overflow-y: auto; flex: 1;">
      ${bannerHtml}
      ${cardsHtml}
      ${findingsHtml}${window.VI ? VI.cardHtml(scan, S.currentSecurityIntel) : ''}${window.OV ? OV.html() : ''}
      ${hardeningHtml}
      ${securityExposureHtml()}${window.Fw ? Fw.html() : ''}${window.AC ? AC.html() : ''}
      ${notesHtml}
    </div>
  `;

  filterSecurityFindings();
  if (window.Fw) Fw.render();
  if (window.OV) OV.render(hostID);
  if (window.AC) { AC.render(); AC.load(); }
  // The re-render above replaced the scan button, so the cooldown is re-applied here.
  if (S.secCooldownRem > 0) secCooldownPaint(S.secCooldownRem);
}

function appendSecurityFooter(content) {
  content.insertAdjacentHTML('beforeend', securityExposureHtml() + (window.Fw ? Fw.html() : '') + (window.AC ? AC.html() : '') + (window.VI ? VI.cardHtml(null, undefined) : '') + (window.OV ? OV.html() : ''));
  if (window.Fw) Fw.render();
  if (window.AC) { AC.render(); AC.load(); }
  if (window.OV) OV.render(S.currentHost ? S.currentHost.id : null);
}

function listenProcessName(p) {
  const raw = String((p && p.process) || '').trim();
  if (!raw || raw === 'LISTEN' || raw === '-') return '';
  if (raw.startsWith('users:')) {
    const names = [...raw.matchAll(/\("([^"]+)"/g)].map(m => m[1]);
    return [...new Set(names)].join(', ');
  }
  const slash = raw.indexOf('/');
  if (slash > 0 && /^\d+$/.test(raw.slice(0, slash))) return raw.slice(slash + 1);
  return raw;
}

function listenProcessCellHtml(p) {
  const n = listenProcessName(p);
  if (!n) return '<span class="text-faint" title="' + escapeHtml(t('service_unknown_hint')) + '">—</span>';
  if (p.process_source === 'port_hint') {
    return '<span class="mono text-muted" style="font-style: italic;" title="' + escapeHtml(t('port_hint_tooltip')) + '">' + escapeHtml(n) + ' <span style="font-size: 0.7rem;">(' + escapeHtml(t('port_hint_label')) + ')</span></span>';
  }
  return '<span class="mono">' + escapeHtml(n) + '</span>';
}

function securityExposureHtml() {
  const h = window.detailData && window.detailData.host;
  if (!h) return '';
  let ports = [];
  try {
    const parsed = JSON.parse(h.listening_ports);
    if (Array.isArray(parsed)) ports = parsed;
  } catch (e) {}
  const failed = h.failed_logins_count || 0;
  const authMac = /^macos/i.test(h.os_info || '');
  const authNA = authMac || failed < 0;
  const authColor = authNA ? 'var(--text-muted)' : (failed > 10 ? 'var(--danger)' : 'var(--success)');
  const authValue = authMac ? t('failed_logins_na') : (failed < 0 ? t('failed_logins_unreadable') : `${failed} ${t('failed_logins')}`);
  const authNote = authNA ? '' : (failed > 10 ? t('high_failed') : t('normal_auth'));
  const portRows = ports.map(p => {
    const riskBadge = p.risk === 'high'
      ? '<span class="badge badge-danger">' + t('high_risk') + '</span>'
      : '<span class="badge" style="background: var(--surface-3); color: var(--text-muted);">' + t('safe_low') + '</span>';
    const expBadge = p.public
      ? '<span class="badge badge-warning">' + t('public_internet') + '</span>'
      : '<span class="badge" style="background: var(--surface-3); color: var(--text-muted);">' + t('localhost_only') + '</span>';
    return `
      <tr>
        <td><b>${escapeHtml(p.port)}</b> <span style="font-size: 0.75rem; color: var(--text-muted);">(${escapeHtml(p.proto)})</span></td>
        <td>${listenProcessCellHtml(p)}</td>
        <td style="font-family: var(--font-mono); font-size: 0.8rem;">${escapeHtml(p.address)}</td>
        <td>${expBadge}</td>
        <td>${riskBadge}</td>
      </tr>`;
  }).join('');
  return `
    <div class="card" style="padding: 16px; margin-top: 16px;">
      <h4 style="margin: 0 0 12px 0; display: flex; align-items: center; gap: 6px;">${svgIcon('lock', 'icon-sm')} <span>${t('sec_exposure_title')}</span></h4>
      <div style="font-size: var(--fs-xs); color: var(--text-muted);">${t('security_bruteforce')}</div>
      <div style="font-size: 1.1rem; font-weight: 700; margin: 4px 0; color: ${authColor};">${authValue}</div>
      <div style="font-size: 0.8rem; color: var(--text-muted); margin-bottom: 16px;">${authNote}</div>
      <h4 style="margin: 0 0 10px 0;">${t('open_ports')}</h4>
      <div style="overflow-x: auto;">
        <table>
          <thead>
            <tr><th>${t('port_proto')}</th><th>${t('service_process')}</th><th>${t('binding')}</th><th>${t('exposure')}</th><th>${t('risk')}</th></tr>
          </thead>
          <tbody>
            ${ports.length === 0 ? '<tr><td colspan="5" style="text-align: center; color: var(--text-muted); padding: 20px;">' + t('no_listening_ports') + '</td></tr>' : portRows}
          </tbody>
        </table>
      </div>
    </div>`;
}

function getSecuritySeverityBadge(sev) {
  const s = String(sev || 'unknown').toLowerCase();
  let cls = 'badge-sec-unknown';
  if (s === 'critical') cls = 'badge-sec-critical';
  else if (s === 'important') cls = 'badge-sec-important';
  else if (s === 'moderate') cls = 'badge-sec-moderate';
  else if (s === 'low') cls = 'badge-sec-low';
  return `<span class="badge ${cls}">${escapeHtml(s)}</span>`;
}

function filterSecurityFindings() {
  const container = document.getElementById('secFindingsContainer');
  if (!container || !S.currentSecurityScan) return;

  const q = (document.getElementById('secFindingsFilter')?.value || '').toLowerCase().trim();
  const sort = document.getElementById('secFindingsSort')?.value || 'sev_desc';
  const scan = S.currentSecurityScan;
  const allFindings = scan.findings || [];
  const isStaleCache = scan.repo_cache_age_hours > 168;

  if (allFindings.length === 0) {
    if (scan.status === 'ok' && !isStaleCache) {
      container.innerHTML = `
        <div class="sec-zero-state sec-zero-ok">
          <span style="font-size: 1.25rem; display: block; margin-bottom: 4px;">${svgIcon('circle-check', 'icon-lg')}</span>
          <b>${t('sec_all_clear_title', 'All Clear - No Security Updates Pending')}</b>
          <div style="font-size: var(--fs-xs); color: var(--text-muted); margin-top: 4px;">${t('sec_all_clear_desc', 'All installed packages are up to date with available security patches.')}</div>
        </div>`;
    } else if (scan.status === 'unsupported') {
      container.innerHTML = `
        <div class="sec-zero-state sec-zero-info">
          <span style="font-size: 1.25rem; display: block; margin-bottom: 4px;">${svgIcon('info', 'icon-lg')}</span>
          <b>${t('sec_unsupported_title', 'Package Security Scanning Not Supported')}</b>
          <div style="font-size: var(--fs-xs); color: var(--text-muted); margin-top: 4px;">${t('sec_unsupported_desc', 'Package manager security updates are not available for this OS distribution. Hardening checks are shown below.')}</div>
        </div>`;
    } else if (isStaleCache) {
      container.innerHTML = `
        <div class="sec-zero-state sec-zero-warn">
          <span style="font-size: 1.25rem; display: block; margin-bottom: 4px;">${svgIcon('triangle-alert', 'icon-lg')}</span>
          <b>${t('sec_stale_zero_title', 'Package Cache Stale')}</b>
          <div style="font-size: var(--fs-xs); color: var(--text-muted); margin-top: 4px;">${t('sec_stale_zero_desc', 'Package repository metadata is over 7 days old. Results may miss recent security updates.')}</div>
        </div>`;
    } else {
      container.innerHTML = `
        <div class="sec-zero-state sec-zero-warn">
          <span style="font-size: 1.25rem; display: block; margin-bottom: 4px;">${svgIcon('triangle-alert', 'icon-lg')}</span>
          <b>${t('sec_partial_zero_title', 'No Security Updates Detected (Coverage Partial)')}</b>
          <div style="font-size: var(--fs-xs); color: var(--text-muted); margin-top: 4px;">${t('sec_partial_zero_desc', 'No updates were reported, but scan coverage is limited. Review the coverage notes above.')}</div>
        </div>`;
    }
    return;
  }

  const sevWeight = { critical: 5, important: 4, moderate: 3, low: 2, unknown: 1 };
  let list = allFindings.slice();
  if (q) {
    list = list.filter(f => 
      (f.package && f.package.toLowerCase().includes(q)) ||
      (f.advisory && f.advisory.toLowerCase().includes(q)) ||
      (f.cves || []).some(c => c.toLowerCase().includes(q)) ||
      (f.severity && f.severity.toLowerCase().includes(q))
    );
  }

  list.sort((a, b) => {
    if (sort === 'sev_desc') {
      const diff = (sevWeight[String(b.severity).toLowerCase()] || 0) - (sevWeight[String(a.severity).toLowerCase()] || 0);
      return diff !== 0 ? diff : (a.package || '').localeCompare(b.package || '');
    } else if (sort === 'sev_asc') {
      const diff = (sevWeight[String(a.severity).toLowerCase()] || 0) - (sevWeight[String(b.severity).toLowerCase()] || 0);
      return diff !== 0 ? diff : (a.package || '').localeCompare(b.package || '');
    } else if (sort === 'exploit' && window.VI) {
      const d = VI.score(b, S.currentSecurityIntel) - VI.score(a, S.currentSecurityIntel);
      return d !== 0 ? d : (sevWeight[String(b.severity).toLowerCase()] || 0) - (sevWeight[String(a.severity).toLowerCase()] || 0);
    } else {
      return (a.package || '').localeCompare(b.package || '');
    }
  });

  const countEl = document.getElementById('secFindingsCount');
  if (countEl) countEl.innerText = list.length;

  if (list.length === 0) {
    container.innerHTML = `<div style="text-align: center; padding: 24px; color: var(--text-muted); font-size: 0.85rem;">${t('no_matching_findings', 'No findings match your filter.')}</div>`;
    return;
  }

  container.innerHTML = `
    <div style="overflow-x: auto;">
      <table style="width: 100%; border-collapse: collapse; font-size: 0.85rem;">
        <thead>
          <tr style="border-bottom: 1px solid var(--card-border); color: var(--text-muted); text-align: left;">
            <th style="padding: 8px 12px;">${t('sec_th_pkg', 'Package')}</th>
            <th style="padding: 8px 12px;">${t('sec_th_installed', 'Installed Version')}</th>
            <th style="padding: 8px 12px;">${t('sec_th_fixed', 'Fixed Version')}</th>
            <th style="padding: 8px 12px; width: 110px;">${t('sec_th_severity', 'Severity')}</th>
            <th style="padding: 8px 12px;">${t('sec_th_advisory', 'Advisory')}</th>
            ${window.VI ? `<th style="padding: 8px 12px;">${t('vi_th', 'CVE / Exploitation')}</th>` : ''}
          </tr>
        </thead>
        <tbody>
          ${list.map(f => `
            <tr style="border-bottom: 1px solid var(--card-border);">
              <td style="padding: 8px 12px; font-weight: 600; color: var(--text);">${escapeHtml(f.package)}</td>
              <td style="padding: 8px 12px; font-family: var(--font-mono); color: var(--text-muted); font-size: 0.8rem;">${escapeHtml(f.installed_version || '-')}</td>
              <td style="padding: 8px 12px; font-family: var(--font-mono); color: var(--success); font-size: 0.8rem;">${escapeHtml(f.fixed_version || '-')}</td>
              <td style="padding: 8px 12px;">${getSecuritySeverityBadge(f.severity)}</td>
              <td style="padding: 8px 12px; font-family: var(--font-mono); font-size: 0.8rem; color: var(--accent-text);">${escapeHtml(f.advisory || '-')}</td>
              ${window.VI ? `<td style="padding: 8px 12px;">${VI.findingCell(f, S.currentSecurityIntel)}</td>` : ''}
            </tr>
          `).join('')}
        </tbody>
      </table>
    </div>
  `;
}

async function runHostSecurityScan(hostID) {
  const btn = document.getElementById('btnScanSecurity');
  if (btn) {
    btn.disabled = true;
    btn.innerHTML = window.SP ? SP.html(t('sec_scanning', 'Scanning...')) : `<span class="spinner" style="display:inline-block; margin-right:4px;">${svgIcon('loader-circle', 'icon-sm')}</span> <span>${t('sec_scanning', 'Scanning...')}</span>`;
  }
  // The button is looked up again: a re-render of the tab replaces the element, so the
  // captured node would be a detached one.
  const resetBtn = () => {
    const el = document.getElementById('btnScanSecurity');
    if (!el) return;
    el.disabled = false;
    el.innerHTML = `<span style="display: inline-flex; align-items: center; gap: 4px;">${svgIcon('refresh-cw', 'icon-sm')} <span>${t('sec_scan_now', 'Scan Now')}</span></span>`;
  };
  try {
    const res = await fetch('/api/hosts/' + hostID + '/security/scan', { method: 'POST' });
    if (res.ok) {
      const data = await res.json();
      showToast(t('sec_scan_complete', 'Security scan complete'), 'success');
      if (S.currentTab === 'security' && S.currentHost && S.currentHost.id === hostID) {
        renderSecurityData(data.scan, hostID, data.intel);
      }
      // The cooldown is applied after the re-render: renderSecurityData replaces the tab
      // content (and with it the button), so a cooldown set before is lost.
      updateSecCooldownUI(60);
    } else if (res.status === 429) {
      const errData = await res.json().catch(() => ({}));
      const rem = errData.cooldown_remaining || 60;
      showToast(errData.error || t('sec_scan_cooldown', 'Scan cooldown active, please wait'), 'warning');
      resetBtn();
      updateSecCooldownUI(rem);
    } else if (res.status === 409) {
      const errData = await res.json().catch(() => ({}));
      showToast(errData.error || t('sec_scan_in_progress', 'Security scan already in progress'), 'warning');
      resetBtn();
    } else {
      const errData = await res.json().catch(() => ({}));
      showToast(t('sec_scan_failed', 'Security scan failed') + ': ' + (errData.error || res.statusText), 'error');
      resetBtn();
    }
  } catch (e) {
    showToast(t('sec_scan_failed', 'Security scan failed') + ': ' + e.message, 'error');
    resetBtn();
  }
}

async function generateBaseline(hostID, btn) {
  // The button is passed explicitly; window.event is not reliable in every browser.
  if (!btn) btn = document.getElementById('btnGenerateBaseline');
  const restore = SP.busy(btn, t('upd_generating_baseline'));
  try {
    const res = await fetch('/api/hosts/' + hostID + '/baseline', { method: 'POST' });
    if (res.ok) {
      inspectHost(hostID);
    } else {
      const errData = await res.json().catch(() => ({}));
      showToast(t('failed_prefix', 'Failed: ') + (errData.error || res.statusText), 'error');
    }
  } catch (e) {
    showToast(t('failed_prefix', 'Failed: ') + e.message, 'error');
  } finally {
    restore();
  }
}

function onRuleKindChange() {
  const kind = document.getElementById('ruleKind')?.value || 'container';
  const hint = S.ruleKindHints[kind] || S.ruleKindHints.container;
  const targetEl = document.getElementById('ruleTarget');
  const expectedEl = document.getElementById('ruleExpected');
  if (targetEl) {
    targetEl.placeholder = hint.targetPh;
    if (hint.readOnlyTarget) {
      targetEl.value = hint.targetVal || 'security';
      targetEl.readOnly = true;
    } else {
      if (targetEl.readOnly) {
        targetEl.readOnly = false;
        targetEl.value = '';
      }
    }
  }
  if (window.FIM) {
    const fim = kind === 'file_integrity';
    FIM.showPanel(fim);
    if (fim) {
      // A baseline belongs to one target; switching kinds discards it.
      FIM.reset();
      FIM.syncPresetVisibility();
    }
    // The FIM panel owns the target and the baseline; the generic inputs stay read-only.
    if (targetEl) targetEl.readOnly = fim || !!hint.readOnlyTarget;
    if (expectedEl) expectedEl.readOnly = fim;
  }
  if (expectedEl) {
    expectedEl.placeholder = hint.expectedPh;
    expectedEl.value = hint.expected;
  }
}

function addRulePrompt(hostID) {
  openAddRuleModal(hostID);
}

function openAddRuleModal(hostID) {
  document.getElementById('ruleHostId').value = hostID;
  document.getElementById('ruleEditId').value = '';
  const titleEl = document.getElementById('ruleModalTitle');
  if (titleEl) titleEl.innerText = t('add_rule_title', 'Add Desired State Rule');
  document.getElementById('ruleKind').value = 'container';
  if (window.FIM) FIM.reset();
  const targetEl = document.getElementById('ruleTarget');
  if (targetEl) {
    targetEl.readOnly = false;
    targetEl.value = '';
  }
  onRuleKindChange();
  openModal('ruleModal');
  setTimeout(() => document.getElementById('ruleTarget')?.focus(), 50);
}

function editRulePrompt(id, kind, target, expected) {
  if (!S.currentHost) return;
  document.getElementById('ruleHostId').value = S.currentHost.id;
  document.getElementById('ruleEditId').value = id;
  const titleEl = document.getElementById('ruleModalTitle');
  if (titleEl) titleEl.innerText = t('edit_rule', 'Edit Rule');
  const kindEl = document.getElementById('ruleKind');
  if (kindEl) kindEl.value = kind;
  onRuleKindChange();
  const targetEl = document.getElementById('ruleTarget');
  if (targetEl) targetEl.value = target;
  const expectedEl = document.getElementById('ruleExpected');
  if (expectedEl) expectedEl.value = expected;
  if (window.FIM) FIM.loadRule(kind, target, expected);
  openModal('ruleModal');
  setTimeout(() => document.getElementById('ruleExpected')?.focus(), 50);
}

async function submitRuleForm() {
  const hostID = document.getElementById('ruleHostId').value;
  const editId = document.getElementById('ruleEditId').value;
  const kind = document.getElementById('ruleKind').value;
  let target = document.getElementById('ruleTarget').value.trim();
  let expected = document.getElementById('ruleExpected').value.trim();
  if (kind === 'file_integrity' && window.FIM) {
    const vals = FIM.ruleValues();
    if (!vals.ok) {
      showToast(vals.reason === 'empty'
        ? t('fill_all_fields', 'Please fill in all fields')
        : t('fim_need_capture', 'Capture a baseline before saving this rule.'), 'error');
      return;
    }
    target = vals.target;
    expected = vals.expected;
  }

  if (!target || !expected) {
    showToast(t('fill_all_fields', 'Please fill in all fields'), 'error');
    return;
  }

  const btn = document.getElementById('btnSaveRule');
  if (btn) btn.disabled = true;
  try {
    const res = await fetch('/api/hosts/' + hostID + '/rules', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ kind, target, expected, enabled: true })
    });
    if (res.ok) {
      if (editId) {
        const d = window.detailData;
        const oldRule = d && d.rules ? d.rules.find(r => String(r.id) === String(editId)) : null;
        if (oldRule && (oldRule.kind !== kind || oldRule.target !== target)) {
          await fetch('/api/rules/' + editId, { method: 'DELETE' });
        }
      }
      closeModal('ruleModal');
      showToast(editId ? t('rule_updated', 'Rule updated successfully') : t('rule_added', 'Rule added successfully'), 'success');
      refreshHostDetailAfterRuleChange(hostID);
      inspectHost(hostID, null, true); // evaluates the new rule; the cooldown may refuse it, the refresh above already shows the rule
    } else {
      const raw = (await res.text()).trim();
      let msg = raw;
      try { msg = JSON.parse(raw).error || raw; } catch (e) {}
      showToast(t('failed_prefix', 'Failed: ') + (msg || 'Server error'), 'error');
    }
  } catch(e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error');
  } finally {
    if (btn) btn.disabled = false;
  }
}

async function deleteRule(id) {
  if (!await showConfirm({ message: t('confirm_delete_rule'), confirmText: t('delete', 'Delete') })) return;
  const res = await fetch('/api/rules/' + id, { method: 'DELETE' });
  if (res.ok) {
    showToast(t('rule_deleted', 'Rule deleted successfully'), 'success');
    refreshHostDetailAfterRuleChange(S.currentHost.id);
  } else {
    showToast(t('failed_prefix', 'Failed to delete rule'), 'error');
  }
}

function refreshHostDetailAfterRuleChange(hostID) {
  loadHosts();
  if (S.currentHost && String(S.currentHost.id) === String(hostID)) openHostDetail(S.currentHost.id);
}

function updateRuleSelection() {
  const all = [...document.querySelectorAll('.rule-cb')];
  const n = all.filter(c => c.checked).length;
  const btn = document.getElementById('btnDeleteSelectedRules');
  if (btn) btn.disabled = n === 0;
  const cnt = document.getElementById('ruleSelCount');
  if (cnt) cnt.textContent = n ? ` (${n})` : '';
  const head = document.getElementById('ruleSelectAll');
  if (head) { head.checked = n > 0 && n === all.length; head.indeterminate = n > 0 && n < all.length; }
}

function toggleAllRules(checked) {
  document.querySelectorAll('.rule-cb').forEach(c => { c.checked = checked; });
  updateRuleSelection();
}

async function deleteSelectedRules() {
  const ids = [...document.querySelectorAll('.rule-cb')].filter(c => c.checked).map(c => c.value);
  if (!ids.length) return;
  const message = t('confirm_delete_rules', 'Delete {n} selected rules?').replace('{n}', ids.length);
  if (!await showConfirm({ message, confirmText: t('delete', 'Delete'), danger: true })) return;
  let failed = 0;
  for (const id of ids) {
    try {
      const res = await fetch('/api/rules/' + encodeURIComponent(id), { method: 'DELETE' });
      if (!res.ok) failed++;
    } catch (e) { failed++; }
  }
  if (failed) showToast(t('rules_delete_partial', '{ok} deleted, {fail} failed').replace('{ok}', ids.length - failed).replace('{fail}', failed), 'warning');
  else showToast(t('rules_deleted_n', '{n} rules deleted').replace('{n}', ids.length), 'success');
  refreshHostDetailAfterRuleChange(S.currentHost.id);
}

function stopDockerStatsTimer() {
  if (S.dockerStatsTimer) { clearInterval(S.dockerStatsTimer); S.dockerStatsTimer = null; }
}

function startDockerStatsTimer() {
  stopDockerStatsTimer();
  S.dockerStatsTimer = setInterval(() => {
    const modalOpen = document.getElementById('hostDetailModal').classList.contains('open');
    if (!modalOpen || S.currentTab !== 'docker' || S.dockerSubTab !== 'stats' || !S.currentHost) { stopDockerStatsTimer(); return; }
    refreshDockerStats();
  }, 10000);
}

function toggleDockerStatsAuto(on) {
  S.dockerStatsAuto = on;
  if (on) startDockerStatsTimer(); else stopDockerStatsTimer();
}

function setDockerSubTab(view) {
  S.dockerSubTab = view;
  switchTab('docker');
}

function dockerHeaderHtml() {
  const views = [['containers', 'docker_sub_containers'], ['images', 'docker_sub_images'], ['volumes', 'docker_sub_volumes'], ['networks', 'docker_sub_networks'], ['stacks', 'docker_sub_stacks'], ['stats', 'docker_sub_stats'], ['system', 'docker_sub_system'], ['security', 'ds_sub']];
  const auto = S.dockerSubTab === 'stats'
    ? `<label style="display: flex; align-items: center; gap: 4px; font-size: var(--fs-xs); cursor: pointer; color: var(--text-muted);"><input type="checkbox" style="width: auto;" ${S.dockerStatsAuto ? 'checked' : ''} onchange="toggleDockerStatsAuto(this.checked)" /> <span>${t('docker_auto_refresh')}</span></label>`
    : '';
  const primary = {
    containers: ['dk_add_container', 'openDockerRunForm()', 'plus'],
    images: ['dk_pull_image', 'openDockerPullForm()', 'download'],
    volumes: ['dk_create_volume', 'openDockerVolumeForm()', 'plus'],
    networks: ['dk_create_network', 'openDockerNetworkForm()', 'plus'],
    stacks: ['dk_new_stack', 'openDockerStackForm()', 'plus']
  }[S.dockerSubTab];
  const prune = {
    images: ['dk_prune_images', "dockerPrune('images')"],
    volumes: ['dk_prune_volumes', "dockerPrune('volumes')"],
    networks: ['dk_prune_networks', "dockerPrune('networks')"]
  }[S.dockerSubTab];
  return `
    <div style="display: flex; justify-content: space-between; align-items: center; gap: 8px; margin-bottom: 12px; flex-wrap: wrap; flex-shrink: 0;">
      <div style="display: inline-flex; flex-wrap: wrap; background: var(--surface-2); border: 1px solid var(--card-border); border-radius: var(--radius-sm); padding: 2px;">
        ${views.map(([v, k]) => `<button class="btn btn-sm ${S.dockerSubTab === v ? 'btn-primary' : 'btn-ghost'}" style="padding: 4px 10px; font-size: var(--fs-xs);" onclick="setDockerSubTab('${v}')">${t(k)}</button>`).join('')}
      </div>
      <div class="dk-toolbar">
        ${auto}
        ${S.dockerSubTab === 'images' && window.Hub ? Hub.toolbar() : ''}
        ${prune ? `<button class="btn btn-sm btn-ghost" onclick="${prune[1]}">${svgIcon('trash-2', 'icon-sm')} <span>${t(prune[0])}</span></button>` : ''}
        ${primary ? `<button class="btn btn-sm btn-primary" onclick="${primary[1]}">${svgIcon(primary[2], 'icon-sm')} <span>${t(primary[0])}</span></button>` : ''}
        <button class="btn btn-sm btn-ghost" onclick="switchTab('docker')" style="display: inline-flex; align-items: center; gap: 4px; font-size: var(--fs-xs);">
          ${svgIcon('refresh-cw', 'icon-sm')} <span>${t('refresh')}</span>
        </button>
      </div>
    </div>`;
}

function dockerEmptyHtml(title, desc, mono) {
  return `
    <div class="empty-state">
      <div class="empty-state-icon">${svgIcon('container', 'icon-xl')}</div>
      <div class="empty-state-title">${title}</div>
      <div class="empty-state-desc"${mono ? ' style="font-family: var(--font-mono); font-size: var(--fs-xs); white-space: pre-wrap;"' : ''}>${escapeHtml(desc)}</div>
    </div>`;
}

function dockerTableHtml(heads, rows, bodyId) {
  return `<div style="overflow-x: auto;"><table><thead><tr>${heads.map(h => `<th>${h}</th>`).join('')}</tr></thead><tbody${bodyId ? ` id="${bodyId}"` : ''}>${rows}</tbody></table></div>`;
}

function dockerBar(pct, warnAt, dangerAt) {
  const cls = pct >= dangerAt ? 'danger' : (pct >= warnAt ? 'warn' : '');
  return `<div class="progress" style="min-width: 70px;"><div class="progress-fill ${cls}" style="width: ${Math.max(0, Math.min(100, pct))}%"></div></div>`;
}

function dockerChips(items) {
  return items.length === 0
    ? '<span style="color: var(--text-muted);">—</span>'
    : `<div style="display: flex; flex-wrap: wrap; gap: 4px;">${items.map(([label, title]) => `<span class="badge badge-info" style="text-transform: none;" title="${escapeHtml(title || '')}">${escapeHtml(label)}</span>`).join('')}</div>`;
}

function renderDockerContainers(data) {
  const containers = data.containers || [];
  if (containers.length === 0) return dockerEmptyHtml(t('no_containers_title'), t('no_containers_desc'));
  S.dockerContainersCache = containers;
  return dockerTableHtml(
    [t('container_name'), t('container_image'), t('status'), t('container_ports'), t('action')],
    containers.map((c, idx) => {
      const running = c.state === 'running';
      return `
        <tr>
          <td><a href="javascript:void(0)" class="dk-name-link" data-name="${escapeHtml(c.name)}" onclick="openDockerInspect(this.dataset.name)">${escapeHtml(c.name)}</a></td>
          <td style="font-family: var(--font-mono); font-size: var(--fs-xs); color: var(--text-muted);">${escapeHtml(c.image)}</td>
          <td><span class="badge badge-${running ? 'healthy' : (c.state === 'exited' || c.state === 'dead' ? 'down' : 'warning')}">${escapeHtml(c.status)}</span></td>
          <td style="font-family: var(--font-mono); font-size: var(--fs-xs); word-break: break-all; color: var(--text-muted);">${escapeHtml(c.ports || '')}</td>
          <td class="dk-actions">
            <button class="btn btn-sm btn-ghost btn-icon" title="${t('dk_inspect')}" data-name="${escapeHtml(c.name)}" onclick="openDockerInspect(this.dataset.name)">${svgIcon('search', 'icon-sm')}</button>
            <button class="btn btn-sm btn-ghost btn-icon" title="${t('docker_logs')}" data-name="${escapeHtml(c.name)}" onclick="openDockerLogs(this.dataset.name)">${svgIcon('scroll-text', 'icon-sm')}</button>
            <button class="btn btn-sm btn-ghost btn-icon" title="${t('docker_menu_title')}" aria-label="${t('docker_menu_title')}" onclick="openDockerMenu(event, ${idx})">${svgIcon('ellipsis-vertical', 'icon-sm')}</button>
          </td>
        </tr>`;
    }).join(''));
}

function dockerStatsRowsHtml(stats) {
  return stats.map(s => `
    <tr>
      <td><b>${escapeHtml(s.name)}</b></td>
      <td style="min-width: 120px;"><div style="font-size: 0.82rem;">${s.cpu_percent.toFixed(2)}%</div>${dockerBar(s.cpu_percent, 70, 90)}</td>
      <td style="min-width: 160px;"><div style="font-size: 0.82rem;">${formatBytes(s.mem_used_bytes)} / ${formatBytes(s.mem_limit_bytes)} (${s.mem_percent.toFixed(1)}%)</div>${dockerBar(s.mem_percent, 70, 90)}</td>
      <td style="font-family: var(--font-mono); font-size: 0.78rem; white-space: nowrap;">${escapeHtml(s.net_io)}</td>
      <td style="font-family: var(--font-mono); font-size: 0.78rem; white-space: nowrap;">${escapeHtml(s.block_io)}</td>
      <td>${s.pids}</td>
    </tr>`).join('');
}

function dockerStatsTotalsText(stats) {
  const cpu = stats.reduce((a, s) => a + s.cpu_percent, 0);
  const mem = stats.reduce((a, s) => a + s.mem_used_bytes, 0);
  return t('docker_totals').replace('{n}', stats.length).replace('{cpu}', cpu.toFixed(1)).replace('{mem}', formatBytes(mem));
}

function renderDockerStats(data) {
  const stats = data.stats || [];
  if (stats.length === 0) return dockerEmptyHtml(t('docker_no_stats_title'), t('docker_no_stats_desc'));
  return `<div id="dockerStatsTotals" style="font-size: 0.85rem; color: var(--text-muted); margin-bottom: 8px;">${dockerStatsTotalsText(stats)}</div>` +
    dockerTableHtml([t('container_name'), t('docker_col_cpu'), t('docker_col_mem'), t('docker_col_netio'), t('docker_col_blockio'), t('docker_col_pids')], dockerStatsRowsHtml(stats), 'dockerStatsBody');
}

async function refreshDockerStats() {
  const host = S.currentHost;
  if (S.dockerStatsBusy || !host) return;
  S.dockerStatsBusy = true;
  try {
    const res = await fetch('/api/hosts/' + host.id + '/docker/stats');
    const data = await res.json();
    const body = document.getElementById('dockerStatsBody');
    if (!data.ok || !body || S.currentTab !== 'docker' || S.dockerSubTab !== 'stats' || !S.currentHost || S.currentHost.id !== host.id) return;
    const stats = data.stats || [];
    if (stats.length === 0) { loadDockerTab(); return; }
    body.innerHTML = dockerStatsRowsHtml(stats);
    document.getElementById('dockerStatsTotals').textContent = dockerStatsTotalsText(stats);
  } catch (e) {
    // keep the previous snapshot
  } finally {
    S.dockerStatsBusy = false;
  }
}

function renderDockerVolumes(data) {
  const volumes = data.volumes || [];
  if (volumes.length === 0) return dockerEmptyHtml(t('docker_no_volumes_title'), t('docker_no_volumes_desc'));
  const unused = volumes.filter(v => v.used_by.length === 0).length;
  return `<div style="font-size: 0.85rem; color: var(--text-muted); margin-bottom: 8px;">${t('docker_vol_summary').replace('{n}', volumes.length).replace('{u}', unused)}</div>` +
    dockerTableHtml([t('docker_col_volume'), t('docker_col_driver'), t('docker_col_size'), t('docker_col_used_by'), t('docker_col_mount'), ''], volumes.map(v => {
      const project = (v.labels || {})['com.docker.compose.project'];
      const inUse = v.used_by.length > 0;
      return `
        <tr>
          <td style="max-width: 280px; word-break: break-all;"><b title="${escapeHtml(v.name)}">${escapeHtml(v.anonymous ? v.name.slice(0, 12) + '…' : v.name)}</b>${v.anonymous ? ` <span class="badge badge-unknown" style="text-transform: none;">${t('docker_vol_anonymous')}</span>` : ''}${project ? `<div style="font-size: 0.72rem; color: var(--text-muted);">compose: ${escapeHtml(project)}</div>` : ''}</td>
          <td>${escapeHtml(v.driver)}</td>
          <td style="white-space: nowrap;">${v.size ? escapeHtml(v.size) : '—'}</td>
          <td>${v.used_by.length === 0 ? `<span class="badge badge-warning">${t('docker_vol_unused')}</span>` : dockerChips(v.used_by.map(n => [n]))}</td>
          <td style="font-family: var(--font-mono); font-size: 0.72rem; word-break: break-all; color: var(--text-muted);">${escapeHtml(v.mountpoint)}</td>
          <td class="dk-actions"><button class="btn btn-sm btn-danger btn-icon" ${inUse ? 'disabled' : ''} title="${escapeHtml(inUse ? t('dk_in_use_hint') : t('dk_remove'))}" data-name="${escapeHtml(v.name)}" onclick="dockerVolumeRemove(this.dataset.name)">${svgIcon('trash-2', 'icon-sm')}</button></td>
        </tr>`;
    }).join(''));
}

function renderDockerNetworks(data) {
  const networks = data.networks || [];
  if (networks.length === 0) return dockerEmptyHtml(t('docker_no_networks_title'), t('docker_no_networks_desc'));
  return dockerTableHtml([t('docker_col_network'), t('docker_col_driver'), t('docker_col_scope'), t('docker_col_subnet'), t('docker_col_gateway'), t('docker_col_attached'), ''], networks.map(n => {
    const flags = [n.built_in ? t('docker_net_builtin') : '', n.internal ? t('docker_net_internal') : '', n.ipv6 ? 'IPv6' : ''].filter(Boolean);
    const nm = escapeHtml(n.name);
    const removable = !n.built_in && n.containers.length === 0;
    return `
      <tr>
        <td><b>${nm}</b> <span style="font-family: var(--font-mono); font-size: 0.72rem; color: var(--text-muted);">${escapeHtml(n.id)}</span>${flags.length ? `<div style="margin-top: 2px;">${flags.map(f => `<span class="badge badge-unknown" style="text-transform: none; margin-right: 4px;">${escapeHtml(f)}</span>`).join('')}</div>` : ''}</td>
        <td>${escapeHtml(n.driver)}</td>
        <td>${escapeHtml(n.scope)}</td>
        <td style="font-family: var(--font-mono); font-size: 0.78rem;">${n.subnets.length ? n.subnets.map(escapeHtml).join('<br>') : '—'}</td>
        <td style="font-family: var(--font-mono); font-size: 0.78rem;">${n.gateways.length ? n.gateways.map(escapeHtml).join('<br>') : '—'}</td>
        <td>${dockerChips(n.containers.map(c => [c.name, [c.ipv4, c.ipv6].filter(Boolean).join('  ')]))}</td>
        <td class="dk-actions">
          ${n.built_in ? '' : `<button class="btn btn-sm btn-ghost btn-icon" title="${t('dk_connect')}" data-name="${nm}" onclick="openDockerConnectForm(this.dataset.name)">${svgIcon('plug', 'icon-sm')}</button>`}
          ${n.built_in ? '' : `<button class="btn btn-sm btn-danger btn-icon" ${removable ? '' : 'disabled'} title="${escapeHtml(removable ? t('dk_remove') : t('dk_in_use_hint'))}" data-name="${nm}" onclick="dockerNetworkRemove(this.dataset.name)">${svgIcon('trash-2', 'icon-sm')}</button>`}
        </td>
      </tr>`;
  }).join(''));
}

function dockerRefreshIndicator() {
  return `<div id="dockerRefreshBar" role="status" aria-live="polite" style="display: flex; align-items: center; gap: 6px; padding: 4px 0 10px; color: var(--text-muted); font-size: var(--fs-xs);">${SP.html(t('docker_refreshing'), Date.now())}</div>`;
}

async function loadDockerTab() {
  stopDockerStatsTimer();
  const host = S.currentHost;
  const view = S.dockerSubTab;
  const content = document.getElementById('tabContent');
  if (!host || !content) return;
  const key = view + ':' + host.id;
  const seq = ++S.dockerTabSeq;
  // A stale response (older request, other sub-tab or other host) never overwrites the newer view.
  const stale = () => seq !== S.dockerTabSeq || S.currentTab !== 'docker' || !S.currentHost || S.currentHost.id !== host.id || S.dockerSubTab !== view;
  const setContent = (html) => {
    content.innerHTML = html + `<div data-docker-view="${key}" style="display: none;"></div>`;
  };
  // The same sub-tab is loaded again (refresh, or after an action): keep the content, show an indicator.
  const same = !!content.querySelector('[data-docker-view="' + key + '"]');
  const old = document.getElementById('dockerRefreshBar');
  if (old) old.remove();
  if (same) {
    const hdr = content.firstElementChild;
    if (hdr) hdr.insertAdjacentHTML('afterend', dockerRefreshIndicator());
    else content.insertAdjacentHTML('afterbegin', dockerRefreshIndicator());
  } else {
    content.innerHTML = dockerHeaderHtml() + SP.loading(t('docker_loading_data'), Date.now()) + SP.skeleton(6);
  }
  // The security panel needs the stored results; the Images list only needs them for the chips.
  // DS.load starts here (it drops the cache of a previous host at once) but the tab is not blocked by it.
  const dsPromise = (window.DS && (view === 'security' || view === 'images')) ? DS.load(host.id) : null;
  if (view === 'security' && window.DS) {
    await dsPromise;
    if (stale()) return;
    setContent(dockerHeaderHtml() + DS.panelHtml());
    // The exploit intelligence is fetched after the panel is painted: it is a network lookup and must
    // never delay the panel. A failure only means no KEV/EPSS badges.
    DS.loadIntel(host.id);
    return;
  }
  let data;
  try {
    const res = await fetch('/api/hosts/' + host.id + '/docker' + (S.DOCKER_VIEW_PATH[view] || ''));
    data = await res.json();
  } catch (e) {
    data = { ok: false, error: e.message };
  }
  if (stale()) return;
  if (!data.ok) {
    setContent(dockerHeaderHtml() + dockerEmptyHtml(t('docker_unavailable'), data.error || '', true));
    return;
  }
  const render = { containers: renderDockerContainers, images: renderDockerImages, stats: renderDockerStats, volumes: renderDockerVolumes, networks: renderDockerNetworks, stacks: renderDockerStacks, system: renderDockerSystem }[view];
  setContent(dockerHeaderHtml() + render(data));
  if (view === 'stats' && S.dockerStatsAuto) startDockerStatsTimer();
  // The vulnerability chips of the Images list arrive with the (slow) scan data: fill them in later.
  if (view === 'images' && dsPromise) {
    dsPromise.then(() => {
      if (stale()) return;
      DS.paintChips();
    });
  }
}

function closeDockerMenu() {
  const m = document.getElementById('dockerActionMenu');
  if (m) m.remove();
}

window.addEventListener('click', closeDockerMenu);

window.addEventListener('scroll', closeDockerMenu, true);

function openDockerMenu(e, idx) {
  const c = S.dockerContainersCache[idx];
  if (!c) return;
  const running = c.state === 'running';
  const paused = c.state === 'paused';
  const n = c.name;
  const items = [
    { label: t('dk_inspect'), run: () => openDockerInspect(n) },
    { label: t('docker_logs'), run: () => openDockerLogs(n) },
    { label: t('dk_console'), run: () => dockerConsole(n), disabled: !running },
    null
  ];
  if (!running && !paused) items.push({ label: t('start'), run: () => dockerAction(n, 'start') });
  if (running || paused) {
    items.push({ label: t('restart'), run: () => dockerAction(n, 'restart') });
    items.push({ label: t('stop'), run: () => dockerAction(n, 'stop') });
  }
  if (running) items.push({ label: t('dk_pause'), run: () => dockerAction(n, 'pause') });
  if (paused) items.push({ label: t('dk_unpause'), run: () => dockerAction(n, 'unpause') });
  if (running) items.push({ label: t('dk_kill'), danger: true, run: () => dockerAction(n, 'kill') });
  items.push(null);
  items.push({ label: t('dk_rename'), run: () => openDockerRenameForm(n) });
  items.push({ label: t('dk_remove'), danger: true, run: () => dockerRemoveContainer(c) });
  dockerFloatingMenu(e, String(idx), items);
}

function stopDockerLogs() {
  const ws = S.dockerLogsSocket;
  S.dockerLogsSocket = null;
  if (ws) ws.close();
}

function openDockerLogs(container) {
  stopDockerLogs();
  const out = document.getElementById('dockerLogsOutput');
  const status = document.getElementById('dockerLogsStatus');
  document.getElementById('dockerLogsTitle').textContent = t('docker_logs_title') + ': ' + container + ' @ ' + S.currentHost.name;
  out.textContent = '';
  status.textContent = t('docker_logs_connecting');
  status.className = 'badge badge-info';
  openModal('dockerLogsModal');

  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const ws = new WebSocket(protocol + '//' + location.host + '/ws/docker/logs?host_id=' + S.currentHost.id + '&container=' + encodeURIComponent(container));
  ws.binaryType = 'arraybuffer';
  S.dockerLogsSocket = ws;
  // Streaming decoder keeps multi-byte UTF-8 sequences intact across chunk boundaries.
  const decoder = new TextDecoder('utf-8');
  let scrollQueued = false;
  const maxChars = 500000;
  let chars = 0;

  ws.onopen = () => {
    if (S.dockerLogsSocket !== ws) return;
    status.textContent = t('docker_logs_live');
    status.className = 'badge badge-healthy';
  };
  ws.onmessage = (ev) => {
    if (S.dockerLogsSocket !== ws) return;
    if (!document.getElementById('dockerLogsModal').classList.contains('open')) {
      stopDockerLogs();
      return;
    }
    let text = typeof ev.data === 'string' ? ev.data : decoder.decode(ev.data, { stream: true });
    text = text.replace(/\x1b\[[0-9;?]*[A-Za-z]/g, '');
    if (!text) return;
    out.appendChild(document.createTextNode(text));
    chars += text.length;
    while (chars > maxChars && out.firstChild && out.firstChild !== out.lastChild) {
      chars -= out.firstChild.textContent.length;
      out.removeChild(out.firstChild);
    }
    if (document.getElementById('dockerLogsAutoScroll').checked && !scrollQueued) {
      scrollQueued = true;
      requestAnimationFrame(() => { scrollQueued = false; out.scrollTop = out.scrollHeight; });
    }
  };
  ws.onclose = () => {
    if (S.dockerLogsSocket !== ws) return;
    S.dockerLogsSocket = null;
    status.textContent = t('docker_logs_ended');
    status.className = 'badge badge-unknown';
  };
}

async function dockerAction(container, action, opts) {
  if (action === 'kill') {
    const ok = await showConfirm({
      title: t('dk_confirm_kill_title').replace('{name}', container),
      message: t('dk_confirm_kill_msg'),
      confirmText: t('dk_kill'),
      danger: true
    });
    if (!ok) return;
  }
  const data = await DP.run({ sub: 'action', payload: Object.assign({ container, action }, opts || {}) });
  if (data.ok) {
    if (S.currentTab === 'docker') loadDockerTab();
    if (S.dkInspectState.name === container && document.getElementById('dockerInspectModal').classList.contains('open')) loadDockerInspect();
  }
  return data;
}

async function dockerRemoveContainer(c) {
  const running = c.state === 'running' || c.state === 'paused' || c.state === 'restarting';
  const ok = await showConfirm({
    title: t('dk_confirm_remove_title').replace('{name}', c.name),
    message: t('dk_confirm_remove_msg') + (running ? '\n\n' + t('dk_confirm_remove_running') : ''),
    confirmText: t('dk_remove'),
    danger: true
  });
  if (!ok) return;
  const data = await dockerAction(c.name, 'remove', { force: running });
  if (data && data.ok && document.getElementById('dockerInspectModal').classList.contains('open') && S.dkInspectState.name === c.name) closeModal('dockerInspectModal');
}

function getHostFileAccess(hostId) {
  if (!hostId) return '';
  if (S.hostFileAccessMap.has(hostId)) return S.hostFileAccessMap.get(hostId) || '';
  try {
    const saved = sessionStorage.getItem('pantau_host_file_access_' + hostId);
    if (saved && /^[A-Za-z_][A-Za-z0-9_.-]{0,31}$/.test(saved)) {
      S.hostFileAccessMap.set(hostId, saved);
      return saved;
    }
  } catch (e) {}
  S.hostFileAccessMap.set(hostId, '');
  return '';
}

function setHostFileAccess(hostId, user) {
  if (!hostId) return;
  const clean = (user || '').trim();
  const valid = /^[A-Za-z_][A-Za-z0-9_.-]{0,31}$/.test(clean) ? clean : '';
  S.hostFileAccessMap.set(hostId, valid);
  try {
    if (valid) sessionStorage.setItem('pantau_host_file_access_' + hostId, valid);
    else sessionStorage.removeItem('pantau_host_file_access_' + hostId);
  } catch (e) {}
}

function resetHostFileAccess(id) { if (getHostFileAccess(id)) setHostFileAccess(id, ''); }

function filesApiUrl(hostId, action, params) {
  const url = '/api/hosts/' + hostId + '/files/' + action;
  const q = new URLSearchParams();
  if (params) {
    for (const [k, v] of Object.entries(params)) {
      if (v !== undefined && v !== null) q.set(k, v);
    }
  }
  const as = getHostFileAccess(hostId);
  if (as) q.set('as', as);
  const qs = q.toString();
  return qs ? url + '?' + qs : url;
}

function renderFileAccessControls() {
  if (!S.currentHost) return '';
  const u = getHostFileAccess(S.currentHost.id), c = !!(u && u !== 'root');
  return `<div class="file-access-controls" style="display:inline-flex;align-items:center;gap:6px;">
    <label style="font-size:var(--fs-xs);color:var(--text-muted);white-space:nowrap;">${t('access_as')}:</label>
    <select id="fileAccessSelect" class="input input-sm" style="padding:2px 6px;font-size:var(--fs-xs);height:28px;" onchange="onFileAccessSelectChange(this.value)">
      <option value="" ${u === '' ? 'selected' : ''}>${t('access_default')}</option>
      <option value="root" ${u === 'root' ? 'selected' : ''}>${t('access_root')}</option>
      <option value="__custom__" ${c ? 'selected' : ''}>${t('access_custom')}</option>
    </select>
    <input type="text" id="fileAccessCustomUser" class="input input-sm mono" style="width:100px;padding:2px 6px;font-size:var(--fs-xs);height:28px;display:${c ? 'inline-block' : 'none'};" placeholder="${t('access_custom_ph')}" value="${c ? escapeHtml(u) : ''}" onkeydown="if(event.key==='Enter'){event.preventDefault();this.blur();}" onblur="onFileAccessCustomBlur(this.value)" />
    ${u ? `<span class="badge badge-warning" style="text-transform:none;white-space:nowrap;font-size:var(--fs-xs);padding:3px 8px;display:inline-flex;align-items:center;gap:4px;">${svgIcon('shield-alert', 'icon-xs')} <span>sudo: ${escapeHtml(u)}</span></span>` : ''}
  </div>`;
}

function onFileAccessSelectChange(val) {
  const inp = document.getElementById('fileAccessCustomUser');
  if (val === '__custom__') {
    if (inp) { inp.style.display = 'inline-block'; inp.focus(); }
  } else {
    if (inp) inp.style.display = 'none';
    if (S.currentHost && getHostFileAccess(S.currentHost.id) !== val) switchFileAccess(val);
  }
}

function onFileAccessCustomBlur(val) {
  if (!S.currentHost) return;
  const trimmed = (val || '').trim(), current = getHostFileAccess(S.currentHost.id);
  if (!trimmed) {
    const sel = document.getElementById('fileAccessSelect');
    if (sel) sel.value = (current === '' || current === 'root') ? current : '__custom__';
    const inp = document.getElementById('fileAccessCustomUser');
    if (inp && current !== '__custom__' && !current) inp.style.display = 'none';
    return;
  }
  if (!/^[A-Za-z_][A-Za-z0-9_.-]{0,31}$/.test(trimmed)) {
    showToast(t('invalid_username'), 'error');
    const inp = document.getElementById('fileAccessCustomUser');
    if (inp) inp.value = (current !== 'root' && current !== '') ? current : '';
    return;
  }
  if (trimmed !== current) switchFileAccess(trimmed);
}

async function switchFileAccess(user) {
  if (!S.currentHost) return;
  setHostFileAccess(S.currentHost.id, user);
  await loadFiles((S.currentFilesData && S.currentFilesData.path) || '/');
}

async function loadHostFilePins(hostId) {
  if (!hostId) return;
  try {
    const res = await fetch('/api/hosts/' + hostId + '/files/pins');
    if (res.ok) {
      S.currentHostFilePins = await res.json();
    } else {
      S.currentHostFilePins = [];
    }
  } catch(e) {
    S.currentHostFilePins = [];
  }
}

async function loadFiles(path, isFallback) {
  if (S.currentHost && S.currentHost.id && path) {
    setHostFilesPath(S.currentHost.id, path);
  }
  const content = document.getElementById('tabContent');
  if (content && !isFallback) {
    content.innerHTML = `<div class="files-loading-state"><svg class="icon icon-lg spinning" style="color:var(--primary);"><use href="#i-loader-circle"/></svg><span>${t('loading_files')}</span></div>`;
  }
  try {
    const [listRes] = await Promise.all([
      fetch(filesApiUrl(S.currentHost.id, 'list', { path })),
      loadHostFilePins(S.currentHost.id)
    ]);
    if (listRes.status === 403) {
      const errText = (await listRes.text()).trim();
      showToast(errText || t('elevation_unavailable'), 'error');
      setHostFileAccess(S.currentHost.id, '');
      if (path !== '/' && !isFallback) return loadFiles('/', true);
      return loadFiles(path, true);
    }
    if (!listRes.ok) {
      const errText = (await listRes.text()).trim();
      const isPermDenied = errText.toLowerCase().includes('permission denied');
      const isDefault = !getHostFileAccess(S.currentHost.id);
      if (isDefault && isPermDenied) {
        S.currentFilesData = { path: path, files: [], is_protected: false, permDenied: true, errorText: errText };
        S.fileFilterQuery = '';
        renderFilesView();
        return;
      }
      if (path !== '/' && !isFallback) return loadFiles('/', true);
      if (content) {
        content.innerHTML = '<p style="color: var(--danger);">' + t('failed_open_path') + escapeHtml(path) + (errText ? ': ' + escapeHtml(errText) : '') + '</p>';
      }
      return;
    }
    const data = await listRes.json();
    S.currentFilesData = data;
    if (S.currentHost) {
      S.currentHost._lastPath = data.path;
    }
    S.fileFilterQuery = '';
    renderFilesView();
  } catch(e) {
    if (path !== '/' && !isFallback) return loadFiles('/', true);
    if (content) {
      content.innerHTML = '<p style="color: var(--danger);">' + t('failed_open_path') + escapeHtml(path) + '</p>';
    }
  }
}

function renderQuickAccessBar() {
  if (!S.currentHostFilePins || S.currentHostFilePins.length === 0) return '';
  const chips = S.currentHostFilePins.map(pin => {
    const isDir = !!pin.is_dir;
    const icon = isDir ? svgIcon('folder', 'icon-sm folder-glyph') : getFileIcon(pin.path);
    const name = pin.path.split('/').filter(Boolean).pop() || '/';
    const note = (pin.note || '').trim();
    const action = isDir ? `loadFiles(${jsArg(pin.path)})` : `openEditor(${jsArg(pin.path)})`;
    return `
      <div class="file-pin-chip" onclick="${action}" title="${escapeHtml(pin.path)}${note ? ' — ' + escapeHtml(note) : ''}">
        <span class="pin-icon">${icon}</span>
        <span class="pin-name">${escapeHtml(name)}</span>
        ${note ? `<span class="pin-note">(${escapeHtml(note)})</span>` : ''}
        <span class="pin-chip-action" onclick="event.stopPropagation(); openPinModal(${jsArg(pin.path)}, ${isDir})" title="${t('edit_pin')}">${svgIcon('pencil', 'icon-xs')}</span>
      </div>
    `;
  }).join('');

  return `
    <div class="file-quick-access">
      <span class="file-quick-access-title">${svgIcon('pin', 'icon-sm')} <span>${t('quick_access')}:</span></span>
      <div style="display: flex; gap: 8px; align-items: center; flex-wrap: wrap; flex: 1;">
        ${chips}
      </div>
    </div>
  `;
}

function toggleBreadcrumbInput(force) {
  S.isBreadcrumbEditing = (typeof force === 'boolean') ? force : !S.isBreadcrumbEditing;
  const bar = document.getElementById('fileBreadcrumbBar');
  if (!bar || !S.currentFilesData) return;
  if (S.isBreadcrumbEditing) {
    bar.innerHTML = `
      <input type="text" id="breadcrumbPathInput" class="mono" value="${escapeHtml(S.currentFilesData.path)}" onkeydown="if(event.key==='Enter'){event.preventDefault();loadFiles(this.value);toggleBreadcrumbInput(false);}else if(event.key==='Escape'){event.preventDefault();toggleBreadcrumbInput(false);}" />
      <button type="button" class="btn btn-sm btn-icon" onclick="loadFiles(document.getElementById('breadcrumbPathInput').value);toggleBreadcrumbInput(false);" title="Navigate">${svgIcon('check', 'icon-xs')}</button>
      <button type="button" class="btn btn-sm btn-icon" onclick="toggleBreadcrumbInput(false)" title="Cancel">${svgIcon('x', 'icon-xs')}</button>
    `;
    const inp = document.getElementById('breadcrumbPathInput');
    if (inp) { inp.focus(); inp.select(); }
  } else {
    bar.innerHTML = renderBreadcrumbs(S.currentFilesData.path);
  }
}

function renderBreadcrumbs(currentPath) {
  if (!currentPath || currentPath === '/') {
    return `
      <span class="file-breadcrumb-item active">${svgIcon('folder', 'icon-sm folder-glyph')} <span>/</span></span>
      <div style="margin-left:auto;display:inline-flex;align-items:center;gap:4px;">
        <button type="button" class="btn btn-icon btn-sm" onclick="copyTextToClipboard('/')" title="${t('copy_path', 'Copy Path')}">${svgIcon('copy', 'icon-xs')}</button>
        <button type="button" class="btn btn-icon btn-sm" onclick="toggleBreadcrumbInput(true)" title="Edit path">${svgIcon('pencil', 'icon-xs')}</button>
      </div>
    `;
  }
  const parts = currentPath.split('/').filter(Boolean);
  let html = `<span class="file-breadcrumb-item" onclick="loadFiles('/')" title="Root">${svgIcon('folder', 'icon-sm folder-glyph')}</span>`;
  let accum = '';
  for (let i = 0; i < parts.length; i++) {
    accum += '/' + parts[i];
    const isLast = (i === parts.length - 1);
    html += `<span class="file-breadcrumb-separator">/</span>`;
    if (isLast) {
      html += `<span class="file-breadcrumb-item active">${escapeHtml(parts[i])}</span>`;
    } else {
      const clickPath = accum;
      html += `<span class="file-breadcrumb-item" onclick="loadFiles(${jsArg(clickPath)})">${escapeHtml(parts[i])}</span>`;
    }
  }
  html += `
    <div style="margin-left:auto;display:inline-flex;align-items:center;gap:4px;">
      <button type="button" class="btn btn-icon btn-sm" onclick="copyTextToClipboard(${jsArg(currentPath)})" title="${t('copy_path', 'Copy Path')}">${svgIcon('copy', 'icon-xs')}</button>
      <button type="button" class="btn btn-icon btn-sm" onclick="toggleBreadcrumbInput(true)" title="Edit path">${svgIcon('pencil', 'icon-xs')}</button>
    </div>
  `;
  return html;
}

function getFileIcon(fileName) {
  const ext = (fileName || '').split('.').pop().toLowerCase();
  switch (ext) {
    case 'sh': case 'bash': case 'zsh': case 'fish':
      return svgIcon('terminal', 'icon-sm file-icon-shell');
    case 'json': case 'yml': case 'yaml': case 'conf': case 'ini': case 'env': case 'toml': case 'cfg': case 'xml':
      return svgIcon('settings', 'icon-sm file-icon-config');
    case 'log': case 'txt': case 'md': case 'rtf': case 'pdf': case 'doc': case 'docx':
      return svgIcon('file-text', 'icon-sm file-icon-text');
    case 'zip': case 'tar': case 'gz': case 'tgz': case 'bz2': case 'xz': case '7z': case 'enc': case 'rar':
      return svgIcon('file-archive', 'icon-sm file-icon-archive');
    case 'jpg': case 'jpeg': case 'png': case 'gif': case 'svg': case 'ico': case 'webp':
      return svgIcon('image', 'icon-sm file-icon-image');
    case 'pem': case 'key': case 'pub': case 'crt': case 'cer':
      return svgIcon('key-round', 'icon-sm file-icon-key');
    case 'js': case 'ts': case 'jsx': case 'tsx': case 'php': case 'py': case 'go': case 'rs': case 'c': case 'cpp': case 'h': case 'html': case 'css': case 'scss': case 'sql': case 'java':
      return svgIcon('file-code', 'icon-sm file-icon-code');
    default:
      return svgIcon('file', 'icon-sm file-icon-default');
  }
}

function renderFilesView() {
  const content = document.getElementById('tabContent');
  if (!content || !S.currentFilesData) return;
  const data = S.currentFilesData;
  const isProtectedDir = !!data.is_protected;
  const parent = data.path === '/' ? '/' : data.path.substring(0, data.path.lastIndexOf('/')) || '/';

  content.innerHTML = `
    <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; gap: 10px; flex-wrap: wrap;">
      <div style="display: flex; align-items: center; gap: 8px; flex: 1; min-width: 260px;">
        <div class="file-breadcrumb-bar" id="fileBreadcrumbBar" style="flex: 1;">
          ${renderBreadcrumbs(data.path)}
        </div>
        ${isProtectedDir ? `<span class="badge badge-danger" style="display: inline-flex; align-items: center; gap: 4px; white-space: nowrap;">${svgIcon('lock', 'icon-xs')} <span>${t('protected_path')}</span></span>` : ''}
      </div>
      <div style="display: flex; gap: 8px; align-items: center; flex-wrap: wrap;">
        ${renderFileAccessControls()}
        <button class="btn btn-sm" onclick="openTerminalForPath(${jsArg(data.path)})" title="${t('open_terminal_here', 'Open Terminal Here')}">
          ${svgIcon('terminal', 'icon-sm')} <span>${t('open_terminal_here', 'Open Terminal Here')}</span>
        </button>
        ${window.DiskUsage ? DiskUsage.toolbarButton(S.currentHost.id, data.path) : ''}
        <button class="btn btn-sm btn-icon" onclick="loadFiles(${jsArg(parent)})" title="${t('up_dir')}">${svgIcon('corner-up-left', 'icon-sm')}</button>
        <button class="btn btn-sm btn-icon" onclick="loadFiles(${jsArg(data.path)})" title="${t('refresh')}">${svgIcon('refresh-cw', 'icon-sm')}</button>
        <button class="btn btn-sm" onclick="openNewEditor(${jsArg(data.path)})" title="${t('new_file', 'New File')}">
          ${svgIcon('file-plus', 'icon-sm')} <span>${t('new_file', 'New File')}</span>
        </button>
        <input type="file" id="fileUploadInput" style="display: none;" onchange="uploadFile(${jsArg(data.path)})" />
        <button class="btn btn-primary btn-sm" id="btnFileUpload" ${isProtectedDir ? 'disabled title="' + t('cannot_upload_protected') + '"' : ''} onclick="document.getElementById('fileUploadInput').click()">
          ${svgIcon('upload', 'icon-sm')} <span>${t('upload_file')}</span>
        </button>
        <button class="btn btn-sm" onclick="backupSelected()" title="${t('backup_title', 'Backup selected folders as .tar.gz')}">
          ${svgIcon('archive', 'icon-sm')} <span id="fileBackupLabel">${backupBtnLabel()}</span>
        </button>
      </div>
    </div>
    ${renderQuickAccessBar()}
    <div class="file-filter-toolbar">
      <div class="file-search-box">
        <svg class="icon icon-sm" style="color: var(--text-muted);"><use href="#i-search"/></svg>
        <input type="text" id="fileSearchInput" placeholder="${t('filter_placeholder')}" value="${escapeHtml(S.fileFilterQuery)}" oninput="onFileFilterInput(this.value)" onkeydown="if(event.key==='Escape'){clearFileFilter();}" />
        <button type="button" id="fileSearchClear" class="file-search-clear" onclick="clearFileFilter()" title="Clear filter">${svgIcon('x', 'icon-xs')}</button>
      </div>
      <div style="display: flex; gap: 10px; align-items: center; flex-wrap: wrap;">
        <div class="file-pill-group">
          <button type="button" class="file-pill-btn ${S.fileFilterType === 'all' ? 'active' : ''}" onclick="setFileTypeFilter('all')">${t('filter_all_types', 'All')}</button>
          <button type="button" class="file-pill-btn ${S.fileFilterType === 'dir' ? 'active' : ''}" onclick="setFileTypeFilter('dir')">${t('filter_folders')}</button>
          <button type="button" class="file-pill-btn ${S.fileFilterType === 'file' ? 'active' : ''}" onclick="setFileTypeFilter('file')">${t('filter_files')}</button>
        </div>
        <span id="fileCountLabel" class="mono" style="font-size: var(--fs-xs); color: var(--text-muted); white-space: nowrap;"></span>
      </div>
    </div>
    <div class="files-table-container" id="filesTableContainer">
      <div id="fileDropOverlay" class="file-drop-overlay">
        <svg class="icon icon-xl" style="color: var(--primary);"><use href="#i-upload"/></svg>
        <div class="file-drop-overlay-title">${t('drop_files_to_upload')}</div>
        <div class="file-drop-overlay-desc">${t('drop_files_target')} <code class="mono">${escapeHtml(data.path)}</code></div>
      </div>
      <table>
        <thead>
          <tr>
            <th style="min-width: 220px;"><span class="sortable-th" onclick="toggleFileSort('name')">${t('file_name')} <span id="sortArrow_name" class="sort-arrow"></span></span></th>
            <th style="width: 110px; text-align: right;"><span class="sortable-th" onclick="toggleFileSort('size')">${t('file_size')} <span id="sortArrow_size" class="sort-arrow"></span></span></th>
            <th style="width: 110px; text-align: center;">${t('file_perms')}</th>
            <th style="width: 170px; text-align: center;"><span class="sortable-th" onclick="toggleFileSort('mod_time')">${t('file_modified')} <span id="sortArrow_mod_time" class="sort-arrow"></span></span></th>
            <th style="width: 1%; white-space: nowrap; text-align: right; padding-right: 14px;">${t('file_actions')}</th>
          </tr>
        </thead>
        <tbody id="filesTableBody"></tbody>
      </table>
    </div>
  `;

  setupFileDragAndDrop();
  renderFilesTableBody();
}

function setupFileDragAndDrop() {
  const container = document.getElementById('filesTableContainer');
  const overlay = document.getElementById('fileDropOverlay');
  if (!container || !overlay) return;

  container.addEventListener('dragover', (e) => {
    e.preventDefault();
    e.stopPropagation();
    overlay.classList.add('drag-over');
  });

  container.addEventListener('dragleave', (e) => {
    e.preventDefault();
    e.stopPropagation();
    if (!container.contains(e.relatedTarget)) {
      overlay.classList.remove('drag-over');
    }
  });

  container.addEventListener('drop', async (e) => {
    e.preventDefault();
    e.stopPropagation();
    overlay.classList.remove('drag-over');
    if (!S.currentFilesData || S.currentFilesData.is_protected) {
      if (S.currentFilesData && S.currentFilesData.is_protected) {
        showToast(t('cannot_upload_protected'), 'error');
      }
      return;
    }
    const files = e.dataTransfer.files;
    if (files && files.length > 0) {
      const total = files.length;
      const t0 = Date.now();
      try {
        for (let i = 0; i < total; i++) {
          const label = t('upd_uploading_n_of_m').replace('{n}', i + 1).replace('{m}', total);
          fileOpBanner(label, t0, i, total, files[i].name);
          // The banner belongs to the drop handler here, so a failed file cannot leave it stuck.
          await uploadRawFile(S.currentFilesData.path, files[i], false, label, false);
        }
      } finally {
        fileOpBannerClear();
      }
      loadFiles(S.currentFilesData.path);
    }
  });
}

function renderFilesTableBody() {
  if (!S.currentFilesData) return;
  const tbody = document.getElementById('filesTableBody');
  const countLabel = document.getElementById('fileCountLabel');
  const clearBtn = document.getElementById('fileSearchClear');
  if (!tbody) return;
  if (S.currentFilesData.permDenied) {
    if (countLabel) countLabel.textContent = '';
    if (clearBtn) clearBtn.style.display = 'none';
    tbody.innerHTML = `<tr><td colspan="5" style="padding:20px 16px;">
      <div class="sec-banner sec-banner-warn" style="display:flex;align-items:center;justify-content:space-between;gap:12px;margin:0;flex-wrap:wrap;">
        <div style="display:flex;align-items:center;gap:10px;">
          <svg class="icon icon-lg" style="color:var(--warning);flex-shrink:0;"><use href="#i-triangle-alert"/></svg>
          <div>
            <span style="font-weight:600;">${t('permission_denied')}</span>
            <div style="font-size:0.82rem;margin-top:4px;opacity:0.9;">${escapeHtml(S.currentFilesData.errorText || t('permission_denied_desc'))}</div>
          </div>
        </div>
        <div style="display:flex;gap:8px;align-items:center;">
          <button class="btn btn-sm" onclick="loadFiles('/')">${svgIcon('folder', 'icon-sm folder-glyph')} <span>/</span></button>
          <button class="btn btn-warning btn-sm" onclick="switchFileAccess('root')" style="white-space:nowrap;font-weight:600;display:inline-flex;align-items:center;gap:4px;">${svgIcon('shield-alert', 'icon-xs')} <span>${t('open_as_root')}</span></button>
        </div>
      </div></td></tr>`;
    return;
  }

  if (clearBtn) {
    clearBtn.style.display = S.fileFilterQuery ? 'inline-flex' : 'none';
  }

  // Update header sort arrows
  ['name', 'size', 'mod_time'].forEach(col => {
    const el = document.getElementById('sortArrow_' + col);
    if (el) {
      if (S.fileSortColumn === col) {
        el.innerHTML = S.fileSortOrder === 'asc' ? svgIcon('chevron-up', 'icon-xs') : svgIcon('chevron-down', 'icon-xs');
      } else {
        el.innerHTML = '';
      }
    }
  });

  // Update pill active classes
  document.querySelectorAll('.file-pill-btn').forEach(btn => {
    btn.classList.remove('active');
  });
  const activePill = document.querySelector(`.file-pill-btn[onclick="setFileTypeFilter('${S.fileFilterType}')"]`);
  if (activePill) activePill.classList.add('active');

  const allFiles = S.currentFilesData.files || [];
  const q = S.fileFilterQuery.trim().toLowerCase();

  const filtered = allFiles.filter(f => {
    if (S.fileFilterType === 'dir' && !f.is_dir) return false;
    if (S.fileFilterType === 'file' && f.is_dir) return false;
    if (q && !f.name.toLowerCase().includes(q)) return false;
    return true;
  });

  // Sort: Folders always first, then by fileSortColumn
  filtered.sort((a, b) => {
    if (a.is_dir !== b.is_dir) {
      return a.is_dir ? -1 : 1;
    }
    let res = 0;
    if (S.fileSortColumn === 'name') {
      res = a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' });
    } else if (S.fileSortColumn === 'size') {
      res = (a.size || 0) - (b.size || 0);
    } else if (S.fileSortColumn === 'mod_time') {
      res = (a.mod_time || '').localeCompare(b.mod_time || '');
    }
    return S.fileSortOrder === 'asc' ? res : -res;
  });

  if (countLabel) {
    countLabel.textContent = t('showing_entries')
      .replace('{count}', filtered.length)
      .replace('{total}', allFiles.length);
  }

  if (filtered.length === 0) {
    const isEmptyDir = (allFiles.length === 0);
    tbody.innerHTML = `
      <tr>
        <td colspan="5" style="padding: 32px 16px;">
          <div class="empty-state" style="padding: 16px;">
            <div class="empty-state-icon">${isEmptyDir ? svgIcon('folder-open', 'icon-xl') : svgIcon('filter-x', 'icon-xl')}</div>
            <div class="empty-state-title">${isEmptyDir ? t('empty_folder') : t('no_matching_files')}</div>
            <div class="empty-state-desc">${isEmptyDir ? t('empty_folder_desc') : t('no_matching_files_desc')}</div>
          </div>
        </td>
      </tr>
    `;
    return;
  }

  const isProtectedDir = !!S.currentFilesData.is_protected;
  const pinMap = new Map();
  if (S.currentHostFilePins && Array.isArray(S.currentHostFilePins)) {
    S.currentHostFilePins.forEach(p => pinMap.set(p.path, p));
  }

  tbody.innerHTML = filtered.map(f => {
    const fullPath = S.currentFilesData.path === '/' ? '/' + f.name : S.currentFilesData.path + '/' + f.name;
    const isItemProtected = !!(f.is_protected || isProtectedDir);
    const targetTermPath = f.is_dir ? fullPath : S.currentFilesData.path;
    const icon = f.is_dir ? svgIcon('folder', 'icon-sm folder-glyph') : getFileIcon(f.name);
    const pin = pinMap.get(fullPath);
    const isPinned = !!pin;
    return `
      <tr>
        <td style="word-break: break-all;">
          <input type="checkbox" class="file-backup-check" title="${t('backup_select', 'Include in backup')}" ${backupSelFor(S.currentHost.id).has(fullPath) ? 'checked' : ''} onchange="toggleBackupSel(${jsArg(fullPath)}, this)" />
          <button type="button" class="btn-file-pin ${isPinned ? 'pinned' : ''}" onclick="openPinModal(${jsArg(fullPath)}, ${f.is_dir})" title="${isPinned ? escapeHtml(pin.note || t('pinned')) : t('pin_this_item')}">${svgIcon('pin', 'icon-sm')}</button>
          ${f.is_dir 
            ? '<a class="folder-link" href="javascript:void(0)" onclick="loadFiles(' + jsArg(fullPath) + ')">' + svgIcon('folder', 'icon-sm folder-glyph') + ' <b>' + escapeHtml(f.name) + '</b></a>' 
            : '<span class="file-link-wrap">' + icon + ' <span>' + escapeHtml(f.name) + '</span></span>'}
          ${isPinned && pin.note ? `<span class="file-pin-badge" title="${escapeHtml(pin.note)}">${svgIcon('sticky-note', 'icon-xs')} <span>${escapeHtml(pin.note)}</span></span>` : ''}
          ${isItemProtected ? '<span class="file-protected-icon" title="' + t('protected_path') + '">' + svgIcon('lock', 'icon-xs') + '</span>' : ''}
        </td>
        <td class="file-size-cell mono" style="text-align: right; white-space: nowrap;">${f.is_dir ? '—' : formatBytes(f.size)}</td>
        <td class="file-perms-cell mono" style="text-align: center; white-space: nowrap;">${f.mode}</td>
        <td class="file-time-cell mono" style="text-align: center; white-space: nowrap;">${f.mod_time}</td>
        <td style="text-align: right; white-space: nowrap; padding-right: 14px;">
          <div class="action-grid" style="margin-left: auto;">
            <button class="btn btn-sm btn-icon" title="${t('open_terminal_here', 'Open Terminal Here')}" onclick="openTerminalForPath(${jsArg(targetTermPath)})">${svgIcon('terminal', 'icon-sm')}</button>
            ${!f.is_dir 
              ? `<button class="btn btn-primary btn-sm" ${isItemProtected ? 'disabled title="' + t('cannot_modify_protected') + '"' : ''} onclick="openEditor(${jsArg(fullPath)})">${svgIcon('pencil', 'icon-sm')} <span>${t('edit')}</span></button>` 
              : '<span style="display:inline-block;"></span>'}
            <button class="btn btn-sm btn-icon action-btn-duplicate" title="${t('duplicate')}" ${isItemProtected ? 'disabled title="' + t('cannot_modify_protected') + '"' : ''} onclick="openDuplicateModal(${jsArg(fullPath)}, ${f.is_dir}, ${jsArg(f.name)})">${svgIcon('copy', 'icon-sm')}</button>
            ${f.is_dir 
              ? `<button class="btn btn-sm btn-icon action-btn-download" title="${t('download_folder', 'Download Folder Archive')}" onclick="downloadFolder(${jsArg(fullPath)})">${svgIcon('download', 'icon-sm')}</button>` 
              : `<a class="btn btn-sm btn-icon action-btn-download" title="${t('download_file', 'Download File')}" href="${filesApiUrl(S.currentHost.id, 'download', { path: fullPath })}" download>${svgIcon('download', 'icon-sm')}</a>`}
            <button class="btn btn-ghost btn-sm" onclick="openTransferDialog(${jsArg(fullPath)}, ${f.is_dir})">${svgIcon('arrow-left-right', 'icon-sm')} <span>${t('copy_to_host')}</span></button>
            <button class="btn btn-sm btn-icon" title="${t('s3_upload_to')}" onclick="openS3UploadDialog(${jsArg(fullPath)}, ${f.is_dir})">${svgIcon('cloud', 'icon-sm')}</button>
            <button class="btn btn-danger btn-sm btn-icon" ${isItemProtected ? 'disabled title="' + t('cannot_delete_protected') + '"' : ''} onclick="deleteRemoteFile(${jsArg(fullPath)}, ${jsArg(S.currentFilesData.path)})">${svgIcon('trash-2', 'icon-sm')}</button>
          </div>
        </td>
      </tr>
    `;
  }).join('');
}

async function uploadRawFile(targetDir, file, overwrite = false, label, manageBanner = true) {
  const formData = new FormData();
  formData.append('file', file);
  const url = filesApiUrl(S.currentHost.id, 'upload', { dir: targetDir, overwrite: overwrite ? 'true' : undefined });
  const t0 = Date.now();
  if (manageBanner) fileOpBanner(label || t('upd_uploading'), t0);
  let res;
  try {
    res = await fetch(url, {
      method: 'POST',
      body: formData
    });
  } catch (e) {
    if (manageBanner) fileOpBannerClear();
    showToast(t('failed_prefix', 'Failed: ') + e.message, 'error');
    return false;
  }
  if (res.status === 409) {
    if (manageBanner) fileOpBannerClear();
    if (await showConfirm({ message: t('file_exists_overwrite').replace('{name}', file.name), confirmText: t('overwrite', 'Overwrite'), danger: true })) {
      return uploadRawFile(targetDir, file, true, label, manageBanner);
    }
    return false;
  }
  if (res.status === 403) {
    if (manageBanner) fileOpBannerClear();
    const errTxt = (await res.text()).trim();
    showToast(errTxt || t('cannot_upload_protected'), 'error');
    resetHostFileAccess(S.currentHost.id);
    return false;
  }
  if (!res.ok) {
    if (manageBanner) fileOpBannerClear();
    const errTxt = (await res.text()).trim();
    showToast(t('failed_prefix', 'Failed: ') + (errTxt || 'Failed to upload file'), 'error');
    return false;
  }
  if (manageBanner) fileOpBannerClear();
  showToast(t('file_uploaded', 'File uploaded successfully'), 'success');
  return true;
}

function fileOpBanner(text, t0, done, total, current) {
  let box = document.getElementById('fileOpBanner');
  if (!box) {
    box = document.createElement('div');
    box.id = 'fileOpBanner';
    box.style.cssText = 'position:fixed;right:24px;bottom:24px;z-index:99999;max-width:420px;';
    document.body.appendChild(box);
  }
  S.fileOpBannerSeq++; // a new banner invalidates the auto-clear timer of the previous one
  box.innerHTML = SP.banner(text, t0, done, total, current);
  return box;
}

function fileOpBannerClear() {
  const box = document.getElementById('fileOpBanner');
  S.fileOpBannerSeq++;
  if (box) box.innerHTML = '';
}

function fileOpBannerAutoClear(ms) {
  const seq = S.fileOpBannerSeq;
  setTimeout(() => { if (seq === S.fileOpBannerSeq) fileOpBannerClear(); }, ms || 5000);
}

function fileOpBannerArchive(t0) {
  fileOpBanner(t('upd_archiving') + ' — ' + t('upd_archiving_hint'), t0);
}

function onFileFilterInput(val) {
  S.fileFilterQuery = val;
  renderFilesTableBody();
}

function clearFileFilter() {
  S.fileFilterQuery = '';
  const inp = document.getElementById('fileSearchInput');
  if (inp) {
    inp.value = '';
    inp.focus();
  }
  renderFilesTableBody();
}

function setFileTypeFilter(type) {
  S.fileFilterType = type;
  renderFilesTableBody();
}

function toggleFileSort(col) {
  if (S.fileSortColumn === col) {
    S.fileSortOrder = S.fileSortOrder === 'asc' ? 'desc' : 'asc';
  } else {
    S.fileSortColumn = col;
    S.fileSortOrder = 'asc';
  }
  renderFilesTableBody();
}

async function downloadFolder(path) {
  try {
    const res = await fetch(filesApiUrl(S.currentHost.id, 'size', { path }));
    if (res.ok) {
      const data = await res.json();
      if (data.size > 2 * 1024 * 1024 * 1024) {
        if (!await showConfirm({ message: t('large_folder_warn').replace('{size}', formatBytes(data.size)), confirmText: t('continue', 'Continue'), danger: false })) {
          return;
        }
      }
    }
  } catch (e) {
    console.warn('size check error:', e);
  }
  // The archive is built server side and streamed to the browser: no byte signal exists,
  // so the banner shows the elapsed time until the download navigation started.
  const t0 = Date.now();
  fileOpBannerArchive(t0);
  window.location.href = filesApiUrl(S.currentHost.id, 'download', { path });
  fileOpBannerAutoClear(8000);
}

function backupSelFor(hostId) {
  if (S.fileBackupHostId !== hostId) { S.fileBackupSel.clear(); S.fileBackupHostId = hostId; }
  return S.fileBackupSel;
}

function backupBtnLabel() {
  const n = backupSelFor(S.currentHost.id).size;
  return t('backup_btn', 'Backup') + (n ? ' (' + n + ')' : '');
}

function toggleBackupSel(p, input) {
  const sel = backupSelFor(S.currentHost.id);
  if (input.checked && sel.size >= S.BACKUP_MAX_ITEMS) {
    input.checked = false;
    showToast(t('backup_limit', 'At most {n} items per backup.').replace('{n}', S.BACKUP_MAX_ITEMS), 'error');
    return;
  }
  if (input.checked) sel.add(p); else sel.delete(p);
  const el = document.getElementById('fileBackupLabel');
  if (el) el.textContent = backupBtnLabel();
}

async function backupSelected() {
  const sel = backupSelFor(S.currentHost.id);
  if (!sel.size) { showToast(t('backup_none', 'Tick the checkbox of the folders to back up first.'), 'info'); return; }
  const ok = await showConfirm({
    title: t('backup_title', 'Backup selected folders as .tar.gz'),
    message: t('backup_confirm').replace('{n}', sel.size).replace('{list}', [...sel].join('\n')),
    confirmText: t('backup_download', 'Download .tar.gz'),
    danger: false,
    extraBtnText: t('backup_clear', 'Clear selection'),
    extraBtnClass: 'btn-ghost',
    extraBtnValue: 'clear'
  });
  if (ok === 'clear') {
    sel.clear();
    renderFilesView();
    return;
  }
  if (ok !== true) return;
  // The archive is streamed to the browser: the browser gives no progress signal, so the
  // banner shows the elapsed time until the download navigation started.
  const t0 = Date.now();
  fileOpBannerArchive(t0);
  const base = filesApiUrl(S.currentHost.id, 'backup');
  window.location.href = base + (base.includes('?') ? '&' : '?') + [...sel].map(p => 'path=' + encodeURIComponent(p)).join('&');
  fileOpBannerAutoClear(8000);
}

async function uploadFile(targetDir, overwrite = false, fileOverride = null) {
  const input = document.getElementById('fileUploadInput');
  const file = fileOverride || (input && input.files.length ? input.files[0] : null);
  if (!file) return;
  const formData = new FormData();
  formData.append('file', file);
  const url = filesApiUrl(S.currentHost.id, 'upload', { dir: targetDir, overwrite: overwrite ? 'true' : undefined });
  const t0 = Date.now();
  fileOpBanner(t('upd_uploading'), t0);
  // The upload button is disabled while the transfer runs, so a second click cannot start a parallel upload.
  const btn = document.getElementById('btnFileUpload');
  const restore = SP.busy(btn, t('upd_uploading'));
  try {
    let res;
    try {
      res = await fetch(url, {
        method: 'POST',
        body: formData
      });
    } catch (e) {
      showToast(t('failed_prefix', 'Failed: ') + e.message, 'error');
      return;
    }
    if (res.status === 409) {
      if (await showConfirm({ message: t('file_exists_overwrite').replace('{name}', file.name), confirmText: t('overwrite', 'Overwrite'), danger: true })) {
        await uploadFile(targetDir, true, file);
      }
      return;
    }
    if (res.status === 403) {
      const errTxt = (await res.text()).trim();
      showToast(errTxt || t('cannot_upload_protected'), 'error');
      resetHostFileAccess(S.currentHost.id);
      return;
    }
    if (!res.ok) {
      const errTxt = (await res.text()).trim();
      showToast(t('failed_prefix', 'Failed: ') + (errTxt || 'Failed to upload file'), 'error');
      return;
    }
    showToast(t('file_uploaded', 'File uploaded successfully'), 'success');
    loadFiles(targetDir);
  } finally {
    fileOpBannerClear();
    restore();
    if (input) input.value = '';
  }
}

async function deleteRemoteFile(path, parentDir) {
  if (!await showConfirm({ message: t('confirm_delete_file').replace('{path}', path), confirmText: t('delete', 'Delete') })) return;
  let recursive = false;
  let res;
  for (;;) {
    try {
      res = await fetch(filesApiUrl(S.currentHost.id, 'delete'), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path, recursive })
      });
    } catch (e) {
      showToast(t('failed_prefix', 'Failed: ') + e.message, 'error');
      return;
    }
    // A folder with content is only removed after a second, explicit confirmation.
    if (res.status === 409 && !recursive) {
      if (!await showConfirm({ message: t('confirm_delete_folder_recursive').replace('{path}', path), confirmText: t('delete', 'Delete'), danger: true })) return;
      recursive = true;
      continue;
    }
    break;
  }
  if (res.status === 403) {
    const errTxt = (await res.text()).trim();
    showToast(errTxt || t('cannot_delete_protected'), 'error');
    resetHostFileAccess(S.currentHost.id);
    return;
  }
  if (!res.ok) {
    const errTxt = (await res.text()).trim();
    showToast(t('failed_prefix', 'Failed: ') + (errTxt || 'Failed to delete'), 'error');
    return;
  }
  showToast(t('file_deleted', 'File deleted successfully'), 'success');
  loadFiles(parentDir);
}

function generateDuplicateName(origName, isDir) {
  const d = new Date();
  const pad = n => String(n).padStart(2, '0');
  const ts = `${d.getFullYear()}${pad(d.getMonth()+1)}${pad(d.getDate())}${pad(d.getHours())}${pad(d.getMinutes())}${pad(d.getSeconds())}`;
  if (isDir) {
    return `${origName}_${ts}`;
  }
  const lastDot = origName.lastIndexOf('.');
  if (lastDot > 0) {
    const base = origName.substring(0, lastDot);
    const ext = origName.substring(lastDot);
    return `${base}_${ts}${ext}`;
  }
  return `${origName}_${ts}`;
}

async function openDuplicateModal(fullPath, isDir, origName) {
  S.currentDuplicatePath = fullPath;
  S.currentDuplicateIsDir = isDir;
  document.getElementById('duplicateSourcePath').textContent = fullPath;
  const defName = generateDuplicateName(origName, isDir);
  const nameInp = document.getElementById('duplicateNameInput');
  if (nameInp) nameInp.value = defName;
  
  const sizeEl = document.getElementById('duplicateItemSize');
  const availEl = document.getElementById('duplicateDiskAvail');
  const warnBadge = document.getElementById('duplicateWarningBadge');
  const errBadge = document.getElementById('duplicateErrorBadge');
  const confirmBtn = document.getElementById('btnConfirmDuplicate');
  
  if (sizeEl) sizeEl.textContent = '...';
  if (availEl) availEl.textContent = '...';
  if (warnBadge) warnBadge.style.display = 'none';
  if (errBadge) errBadge.style.display = 'none';
  if (confirmBtn) {
    confirmBtn.disabled = false;
    confirmBtn.innerHTML = svgIcon('copy', 'icon-sm') + ' <span>' + t('duplicate_btn') + '</span>';
  }

  openModal('duplicateModal');
  setTimeout(() => { if (nameInp) nameInp.focus(); }, 100);

  try {
    const res = await fetch(filesApiUrl(S.currentHost.id, 'size', { path: fullPath }));
    if (res.ok) {
      const data = await res.json();
      const itemSize = data.size || 0;
      const availDisk = data.available || 0;
      if (sizeEl) sizeEl.textContent = formatBytes(itemSize);
      if (availEl) availEl.textContent = availDisk > 0 ? formatBytes(availDisk) : '-';

      const safetyBuffer = 100 * 1024 * 1024; // 100 MB
      if (availDisk > 0 && (itemSize + safetyBuffer) > availDisk) {
        if (errBadge) {
          document.getElementById('duplicateErrorText').textContent = t('duplicate_disk_insufficient');
          errBadge.style.display = 'flex';
        }
        if (confirmBtn) confirmBtn.disabled = true;
      } else if (itemSize >= 500 * 1024 * 1024) {
        if (warnBadge) {
          document.getElementById('duplicateWarningText').textContent = t('duplicate_large_warn');
          warnBadge.style.display = 'flex';
        }
      }
    }
  } catch (e) {
    console.warn('Failed to load item size/disk info:', e);
  }
}

async function executeDuplicate() {
  const confirmBtn = document.getElementById('btnConfirmDuplicate');
  const nameInp = document.getElementById('duplicateNameInput');
  const newName = nameInp ? nameInp.value.trim() : '';
  if (!newName) {
    showToast(t('all_fields_required'), 'error');
    return;
  }
  if (confirmBtn) {
    confirmBtn.disabled = true;
    confirmBtn.innerHTML = svgIcon('loader-circle', 'icon-sm spinning') + ' <span>' + t('duplicating') + '</span>';
  }
  try {
    const res = await fetch(filesApiUrl(S.currentHost.id, 'duplicate'), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: S.currentDuplicatePath, new_name: newName })
    });
    if (res.status === 409) {
      showToast(t('duplicate_conflict'), 'error');
      if (confirmBtn) {
        confirmBtn.disabled = false;
        confirmBtn.innerHTML = svgIcon('copy', 'icon-sm') + ' <span>' + t('duplicate_btn') + '</span>';
      }
      return;
    }
    if (res.status === 403) {
      const errTxt = (await res.text()).trim();
      showToast(errTxt || t('cannot_modify_protected'), 'error');
      resetHostFileAccess(S.currentHost.id);
      if (confirmBtn) {
        confirmBtn.disabled = false;
        confirmBtn.innerHTML = svgIcon('copy', 'icon-sm') + ' <span>' + t('duplicate_btn') + '</span>';
      }
      return;
    }
    if (res.status === 507 || res.status === 400) {
      const errTxt = await res.text();
      showToast(errTxt || t('duplicate_disk_insufficient'), 'error');
      if (confirmBtn) {
        confirmBtn.disabled = false;
        confirmBtn.innerHTML = svgIcon('copy', 'icon-sm') + ' <span>' + t('duplicate_btn') + '</span>';
      }
      return;
    }
    if (!res.ok) {
      const errTxt = await res.text();
      showToast(t('failed_prefix') + (errTxt || 'Duplication failed'), 'error');
      if (confirmBtn) {
        confirmBtn.disabled = false;
        confirmBtn.innerHTML = svgIcon('copy', 'icon-sm') + ' <span>' + t('duplicate_btn') + '</span>';
      }
      return;
    }
    showToast(t('duplicate_success'), 'success');
    closeModal('duplicateModal');
    if (S.currentFilesData && S.currentFilesData.path) {
      loadFiles(S.currentFilesData.path);
    }
  } catch (e) {
    showToast(t('failed_prefix') + e.message, 'error');
    if (confirmBtn) {
      confirmBtn.disabled = false;
      confirmBtn.innerHTML = svgIcon('copy', 'icon-sm') + ' <span>' + t('duplicate_btn') + '</span>';
    }
  }
}

function openPinModal(path, isDir, hostId = null, refreshTabId = null, isManualInput = false) {
  S.currentPinEditingPath = path || '';
  S.currentPinEditingIsDir = !!isDir;
  S.currentPinHostId = hostId || (S.currentHost ? S.currentHost.id : null);
  S.currentPinRefreshTabId = refreshTabId;

  const targetHost = (S.allRawHosts || []).find(x => x.id === S.currentPinHostId) || (S.currentHost && S.currentHost.id === S.currentPinHostId ? S.currentHost : null);
  const hostBadge = document.getElementById('pinModalHostBadge');
  if (hostBadge) {
    if (targetHost && targetHost.name) {
      hostBadge.innerText = targetHost.name;
      hostBadge.style.display = 'inline-block';
    } else {
      hostBadge.style.display = 'none';
    }
  }

  const pathEl = document.getElementById('pinModalPath');
  const pathInputWrapper = document.getElementById('pinModalPathInputWrapper');
  const pathInput = document.getElementById('pinModalPathInput');
  const noteInput = document.getElementById('pinModalNote');
  const btnUnpin = document.getElementById('btnPinModalUnpin');

  if (!isManualInput && path) {
    if (pathEl) { pathEl.innerText = path; pathEl.style.display = 'block'; }
    if (pathInputWrapper) pathInputWrapper.style.display = 'none';
    const pin = S.currentHostFilePins && S.currentHostFilePins.find(p => p.path === path);
    if (noteInput) noteInput.value = pin ? (pin.note || '') : '';
    if (btnUnpin) btnUnpin.style.display = pin ? 'inline-block' : 'none';
    openModal('filePinModal');
    if (noteInput) setTimeout(() => noteInput.focus(), 50);
  } else {
    // Manual path entry mode (e.g. from Terminal) with auto-filled active path
    if (pathEl) pathEl.style.display = 'none';
    if (pathInputWrapper) pathInputWrapper.style.display = 'flex';
    if (pathInput) {
      pathInput.value = path || '/';
    }
    if (noteInput) noteInput.value = '';
    if (btnUnpin) btnUnpin.style.display = 'none';
    openModal('filePinModal');
    if (pathInput) {
      setTimeout(() => {
        pathInput.focus();
        pathInput.select();
      }, 50);
    }
  }
}

async function submitSavePin() {
  const pathInput = document.getElementById('pinModalPathInput');
  const pathInputWrapper = document.getElementById('pinModalPathInputWrapper');
  const isManualMode = pathInputWrapper && pathInputWrapper.style.display !== 'none';
  const savePath = isManualMode ? pathInput.value.trim() : S.currentPinEditingPath;
  const saveIsDir = isManualMode ? true : S.currentPinEditingIsDir;
  const targetHostId = S.currentPinHostId || (S.currentHost ? S.currentHost.id : null);

  if (!targetHostId || !savePath) {
    showToast(t('fill_all_fields', 'Please enter a path'), 'error');
    return;
  }
  const noteInput = document.getElementById('pinModalNote');
  const note = noteInput ? noteInput.value.trim() : '';
  try {
    const res = await fetch('/api/hosts/' + targetHostId + '/files/pins', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        path: savePath,
        is_dir: saveIsDir,
        note: note
      })
    });
    if (res.ok) {
      closeModal('filePinModal');
      showToast(t('pin_saved'), 'success');
      if (S.currentPinRefreshTabId) {
        loadTerminalPins(S.currentPinRefreshTabId);
      }
      if (S.currentHost && S.currentHost.id === targetHostId) {
        await loadHostFilePins(S.currentHost.id);
        renderFilesView();
      }
    } else {
      const d = await res.json();
      showToast(t('error_prefix', 'Error: ') + (d.error || 'Failed to save pin'), 'error');
    }
  } catch(e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error');
  }
}

async function submitUnpin() {
  const targetHostId = S.currentPinHostId || (S.currentHost ? S.currentHost.id : null);
  if (!targetHostId || !S.currentPinEditingPath) return;
  const fileName = S.currentPinEditingPath.split('/').filter(Boolean).pop() || S.currentPinEditingPath;
  const promptMsg = t('confirm_unpin_item', 'Are you sure you want to unpin "{name}"?').replace('{name}', fileName);
  if (!await showConfirm({ message: promptMsg, confirmText: t('btn_unpin', 'Unpin'), danger: true })) {
    return;
  }
  try {
    const res = await fetch('/api/hosts/' + targetHostId + '/files/pins?path=' + encodeURIComponent(S.currentPinEditingPath), {
      method: 'DELETE'
    });
    if (res.ok) {
      closeModal('filePinModal');
      showToast(t('pin_removed'), 'info');
      if (S.currentPinRefreshTabId) {
        loadTerminalPins(S.currentPinRefreshTabId);
      }
      if (S.currentHost && S.currentHost.id === targetHostId) {
        await loadHostFilePins(S.currentHost.id);
        renderFilesView();
      }
    } else {
      showToast(t('error_prefix', 'Error: ') + 'Failed to unpin', 'error');
    }
  } catch(e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error');
  }
}

async function openGlobalPinsModal() {
  S.globalPinsSearchQuery = '';
  const searchInput = document.getElementById('globalPinsSearchInput');
  if (searchInput) searchInput.value = '';
  openModal('globalPinsModal');
  await loadGlobalPins();
}

async function loadGlobalPins() {
  const container = document.getElementById('globalPinsList');
  if (container) {
    container.innerHTML = SP.loading(t('loading', 'Loading...'));
  }
  try {
    const res = await fetch('/api/pins');
    if (res.ok) {
      S.allGlobalPinsData = await res.json();
    } else {
      S.allGlobalPinsData = [];
    }
  } catch(e) {
    S.allGlobalPinsData = [];
  }
  renderGlobalPinsList();
}

function onGlobalPinsSearch(query) {
  S.globalPinsSearchQuery = (query || '').toLowerCase().trim();
  renderGlobalPinsList();
}

function renderGlobalPinsList() {
  const container = document.getElementById('globalPinsList');
  if (!container) return;

  const q = S.globalPinsSearchQuery;
  const filtered = S.allGlobalPinsData.filter(p => {
    if (!q) return true;
    const matchPath = (p.path || '').toLowerCase().includes(q);
    const matchNote = (p.note || '').toLowerCase().includes(q);
    const matchHost = (p.host_name || '').toLowerCase().includes(q) || (p.host_ip || '').toLowerCase().includes(q);
    return matchPath || matchNote || matchHost;
  });

  if (filtered.length === 0) {
    container.innerHTML = `
      <div class="empty-state" style="padding: 40px 16px;">
        <div class="empty-state-icon">${svgIcon('bookmark', 'icon-xl')}</div>
        <div class="empty-state-title">${t('global_pins_empty')}</div>
        <div class="empty-state-desc">${t('global_pins_empty_sub')}</div>
      </div>
    `;
    return;
  }

  const groups = new Map();
  filtered.forEach(p => {
    if (!groups.has(p.host_id)) {
      groups.set(p.host_id, {
        hostId: p.host_id,
        hostName: p.host_name,
        hostIP: p.host_ip,
        pins: []
      });
    }
    groups.get(p.host_id).pins.push(p);
  });

  let html = '';
  groups.forEach(g => {
    html += `
      <div class="global-pins-host-group">
        <div class="global-pins-host-header">
          <div style="display: flex; align-items: center; gap: 8px;">
            ${svgIcon('server', 'icon-sm')}
            <span style="font-weight: 600;">${escapeHtml(g.hostName)}</span>
            <span class="mono" style="font-size: var(--fs-xs); color: var(--text-muted);">(${escapeHtml(g.hostIP)})</span>
          </div>
          <span class="badge badge-info">${g.pins.length}</span>
        </div>
        <div>
          ${g.pins.map(p => {
            const isDir = !!p.is_dir;
            const icon = isDir ? svgIcon('folder', 'icon-sm folder-glyph') : getFileIcon(p.path);
            const fileName = p.path.split('/').filter(Boolean).pop() || '/';
            return `
              <div class="global-pins-item">
                <div style="display: flex; align-items: center; gap: 10px; min-width: 0; flex: 1; cursor: pointer;" onclick="jumpToHostFile(${p.host_id}, ${jsArg(p.path)}, ${isDir})" title="${escapeHtml(p.path)}">
                  <span style="flex-shrink: 0; display: inline-flex; align-items: center;">${icon}</span>
                  <div style="min-width: 0; flex: 1;">
                    <div style="font-weight: 500; font-size: var(--fs-sm); color: var(--text); word-break: break-all;">
                      ${escapeHtml(fileName)}
                      <span class="mono" style="font-size: var(--fs-xs); color: var(--text-muted); font-weight: normal; margin-left: 6px;">${escapeHtml(p.path)}</span>
                    </div>
                    ${p.note ? `<div style="font-size: var(--fs-xs); color: var(--accent-text); margin-top: 2px; display: inline-flex; align-items: center; gap: 4px;">${svgIcon('sticky-note', 'icon-xs')} <span>${escapeHtml(p.note)}</span></div>` : ''}
                  </div>
                </div>
                <div style="display: flex; align-items: center; gap: 6px; flex-shrink: 0; margin-left: 12px;">
                  <button class="btn btn-primary btn-sm" onclick="jumpToHostFile(${p.host_id}, ${jsArg(p.path)}, ${isDir})">
                    ${isDir ? svgIcon('folder-open', 'icon-xs') : svgIcon('pencil', 'icon-xs')} <span>Open</span>
                  </button>
                  <button class="btn btn-sm" onclick="openTerminalFromGlobalPin(${p.host_id}, ${jsArg(p.path)}, ${isDir})" title="${t('open_terminal_here', 'Open Terminal Here')}" style="display: inline-flex; align-items: center; gap: 4px;">
                    ${svgIcon('terminal', 'icon-xs')} <span>Terminal</span>
                  </button>
                  <button class="btn btn-danger btn-sm btn-icon" onclick="deleteGlobalPin(${p.host_id}, ${jsArg(p.path)})" title="${t('btn_unpin')}">
                    ${svgIcon('trash-2', 'icon-xs')}
                  </button>
                </div>
              </div>
            `;
          }).join('')}
        </div>
      </div>
    `;
  });
  container.innerHTML = html;
}

function openTerminalFromGlobalPin(hostId, targetPath, isDir) {
  closeModal('globalPinsModal');
  const termDir = isDir ? targetPath : (targetPath.substring(0, targetPath.lastIndexOf('/')) || '/');
  const host = (S.allRawHosts || []).find(h => h.id === hostId) || (S.currentHost && S.currentHost.id === hostId ? S.currentHost : null);
  const hostName = host ? host.name : ('Host ' + hostId);
  const folderName = (termDir === '/' || !termDir) ? '/' : termDir.split('/').filter(Boolean).pop() || termDir;
  const customTitle = `${hostName}: ${folderName}`;
  openTerminalForHost(hostId, null, true, customTitle, termDir);
}

async function deleteGlobalPin(hostId, path) {
  const fileName = path.split('/').filter(Boolean).pop() || path;
  const promptMsg = t('confirm_unpin_item', 'Are you sure you want to unpin "{name}"?').replace('{name}', fileName);
  if (!await showConfirm({ message: promptMsg, confirmText: t('btn_unpin', 'Unpin'), danger: true })) {
    return;
  }
  try {
    const res = await fetch('/api/hosts/' + hostId + '/files/pins?path=' + encodeURIComponent(path), {
      method: 'DELETE'
    });
    if (res.ok) {
      showToast(t('pin_removed'), 'success');
      await loadGlobalPins();
      if (S.currentHost && S.currentHost.id === hostId) {
        await loadHostFilePins(hostId);
        if (S.currentTab === 'files') {
          renderFilesView();
        }
      }
    }
  } catch(e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error');
  }
}

async function jumpToHostFile(hostId, targetPath, isDir) {
  closeModal('globalPinsModal');
  await openHostDetail(hostId);
  switchTab('files');
  if (isDir) {
    loadFiles(targetPath);
  } else {
    const parentDir = targetPath.substring(0, targetPath.lastIndexOf('/')) || '/';
    await loadFiles(parentDir);
    setTimeout(() => {
      openEditor(targetPath);
    }, 400);
  }
}

function toggleEditorMaximize(event) {
  if (event) event.stopPropagation();
  S.isEditorMaximized = !S.isEditorMaximized;
  applyEditorMaximizeState();
}

function applyEditorMaximizeState() {
  const modal = document.getElementById('editorModal');
  const content = modal ? modal.querySelector('.modal-content') : null;
  const btn = document.getElementById('btnEditorMax');
  if (!modal || !content) return;
  if (S.isEditorMaximized) {
    modal.classList.add('workspace-maximized-wrapper');
    content.classList.add('workspace-maximized');
    if (btn) {
      btn.innerHTML = svgIcon('minimize-2', 'icon-sm');
      btn.title = t('restore_workspace', 'Restore Workspace');
    }
  } else {
    modal.classList.remove('workspace-maximized-wrapper');
    content.classList.remove('workspace-maximized');
    if (btn) {
      btn.innerHTML = svgIcon('maximize-2', 'icon-sm');
      btn.title = t('maximize_workspace', 'Maximize Workspace');
    }
  }
  if (S.codeEditor) {
    setTimeout(() => S.codeEditor.refresh(), 50);
  }
}

function getEditorModeForPath(filePath) {
  const ext = (filePath || '').split('.').pop().toLowerCase();
  const map = {
    'js': 'javascript', 'ts': 'javascript', 'json': 'javascript',
    'sh': 'shell', 'bash': 'shell', 'zsh': 'shell', 'env': 'shell',
    'php': 'php',
    'yml': 'yaml', 'yaml': 'yaml',
    'css': 'css', 'scss': 'css',
    'html': 'htmlmixed', 'htm': 'htmlmixed',
    'xml': 'xml', 'svg': 'xml',
    'c': 'clike', 'cpp': 'clike', 'h': 'clike', 'java': 'clike'
  };
  return map[ext] || 'shell';
}

function setupCodeEditorEvents() {
  if (!S.codeEditor) return;
  S.codeEditor.on('cursorActivity', () => {
    const cur = S.codeEditor.getCursor();
    const pos = document.getElementById('editorCursorPos');
    if (pos) pos.textContent = `Ln ${cur.line + 1}, Col ${cur.ch + 1}`;
  });
  S.codeEditor.on('change', () => {
    const dirty = document.getElementById('editorDirtyIndicator');
    const isDirty = S.currentEditorState && S.codeEditor.getValue() !== S.currentEditorState.savedContent;
    if (dirty) dirty.style.display = isDirty ? 'inline-flex' : 'none';
    const lines = document.getElementById('editorLineCount');
    if (lines) lines.textContent = `${S.codeEditor.lineCount()} lines`;
  });
}

function openNewEditor(dirPath) {
  const defaultName = 'untitled.txt';
  const fullPath = (dirPath === '/' ? '' : dirPath) + '/' + defaultName;
  S.currentEditorState = {
    hostId: S.currentHost.id,
    hostName: S.currentHost.name,
    hostObj: Object.assign({}, S.currentHost),
    path: fullPath,
    savedContent: ''
  };
  document.getElementById('editorFilePath').innerText = fullPath;
  const chip = document.getElementById('editorLanguageChip');
  if (chip) chip.innerText = 'text';
  const modeLbl = document.getElementById('editorModeLabel');
  if (modeLbl) modeLbl.innerText = 'text';
  const dirty = document.getElementById('editorDirtyIndicator');
  if (dirty) dirty.style.display = 'none';

  openModal('editorModal');
  const mode = getEditorModeForPath(fullPath);
  if (!S.codeEditor) {
    S.codeEditor = CodeMirror.fromTextArea(document.getElementById('codeEditorArea'), {
      lineNumbers: true,
      theme: 'vscode-dark',
      mode: mode,
      extraKeys: {
        'Ctrl-S': () => saveEditorFile(),
        'Cmd-S': () => saveEditorFile()
      }
    });
    setupCodeEditorEvents();
  } else {
    S.codeEditor.setOption('theme', 'vscode-dark');
    S.codeEditor.setOption('mode', mode);
  }
  S.codeEditor.setValue('');
  setTimeout(() => S.codeEditor.refresh(), 100);
}

async function openEditor(path) {
  const sessId = 'editor-' + S.currentHost.id + '-' + path;
  const existingSession = S.workspaceSessions.find(s => s.id === sessId);
  if (existingSession) {
    restoreWorkspaceSession(sessId);
    return;
  }
  // The modal opens right away with a spinner: reading a remote file can take a while.
  const pathEl = document.getElementById('editorFilePath');
  if (pathEl) pathEl.innerText = path;
  openModal('editorModal');
  const modalBody = document.querySelector('#editorModal .modal-body');
  const loader = document.createElement('div');
  loader.id = 'editorFileLoading';
  loader.style.cssText = 'position:absolute;inset:0;display:flex;align-items:center;justify-content:center;background:var(--card-bg);z-index:10;';
  loader.innerHTML = SP.loading(t('loading', 'Loading...'));
  if (modalBody) { modalBody.style.position = 'relative'; modalBody.appendChild(loader); }
  let res;
  try {
    res = await fetch(filesApiUrl(S.currentHost.id, 'read', { path }));
  } catch (e) {
    showToast(t('failed_read_file', 'Failed to read file') + ': ' + e.message, 'error');
    closeModal('editorModal');
    return;
  } finally {
    if (loader.parentNode) loader.parentNode.removeChild(loader);
  }
  if (!res.ok) {
    const errTxt = (await res.text()).trim();
    showToast(errTxt || t('failed_read_file'), 'error');
    if (res.status === 403) resetHostFileAccess(S.currentHost.id);
    closeModal('editorModal');
    return;
  }
  let data;
  try {
    data = await res.json();
  } catch (e) {
    showToast(t('failed_read_file', 'Failed to read file') + ': ' + e.message, 'error');
    closeModal('editorModal');
    return;
  }
  S.currentEditorState = {
    hostId: S.currentHost.id,
    hostName: S.currentHost.name,
    hostObj: Object.assign({}, S.currentHost),
    path: path,
    savedContent: data.content
  };
  document.getElementById('editorFilePath').innerText = path;

  const mode = getEditorModeForPath(path);
  const chip = document.getElementById('editorLanguageChip');
  if (chip) chip.innerText = mode;
  const modeLbl = document.getElementById('editorModeLabel');
  if (modeLbl) modeLbl.innerText = mode;
  const dirty = document.getElementById('editorDirtyIndicator');
  if (dirty) dirty.style.display = 'none';

  if (!S.codeEditor) {
    S.codeEditor = CodeMirror.fromTextArea(document.getElementById('codeEditorArea'), {
      lineNumbers: true,
      theme: 'vscode-dark',
      mode: mode,
      extraKeys: {
        'Ctrl-S': () => saveEditorFile(),
        'Cmd-S': () => saveEditorFile()
      }
    });
    setupCodeEditorEvents();
  } else {
    S.codeEditor.setOption('theme', 'vscode-dark');
    S.codeEditor.setOption('mode', mode);
  }
  S.codeEditor.setValue(data.content);
  const lines = document.getElementById('editorLineCount');
  if (lines) lines.textContent = `${S.codeEditor.lineCount()} lines`;
  setTimeout(() => S.codeEditor.refresh(), 100);
}

async function saveEditorFile() {
  if (S.editorSaveInFlight) return; // Ctrl-S must not start a second save while one runs
  const path = document.getElementById('editorFilePath').innerText;
  const content = S.codeEditor.getValue();
  const hostId = (S.currentEditorState && S.currentEditorState.hostId) ? S.currentEditorState.hostId : (S.currentHost ? S.currentHost.id : null);
  if (!hostId) return;
  // The save button is disabled while the write runs, so a double save cannot happen.
  const btn = document.getElementById('btnSaveEditorFile');
  const restore = SP.busy(btn, t('upd_saving_file'));
  S.editorSaveInFlight = true;
  try {
    const res = await fetch(filesApiUrl(hostId, 'write'), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path, content })
    });
    if (res.ok) {
      showToast(t('file_saved', 'File saved successfully'), 'success');
      if (S.currentEditorState) {
        S.currentEditorState.savedContent = content;
      }
      const sessId = 'editor-' + hostId + '-' + path;
      S.workspaceSessions = S.workspaceSessions.filter(s => s.id !== sessId);
      renderWorkspaceShelf();
      closeModal('editorModal');
    } else if (res.status === 403) {
      const errTxt = (await res.text()).trim();
      showToast(errTxt || t('cannot_modify_protected'), 'error');
      resetHostFileAccess(hostId);
    } else {
      const errTxt = (await res.text()).trim();
      showToast(t('failed_prefix', 'Failed: ') + (errTxt || 'Failed to save file'), 'error');
    }
  } catch (e) {
    showToast(t('failed_prefix', 'Failed: ') + e.message, 'error');
  } finally {
    S.editorSaveInFlight = false;
    restore();
  }
}

function minimizeHostDetail() {
  if (!S.currentHost) return;
  if (S.workspaceSessions.length >= 5 && !S.workspaceSessions.find(s => s.type === 'host' && s.hostId === S.currentHost.id)) {
    showToast(t('shelf_limit_reached'), 'warning');
    return;
  }
  const lastPath = (S.currentFilesData && S.currentFilesData.path) || S.currentHost._lastPath || '/';
  const existingIdx = S.workspaceSessions.findIndex(s => s.type === 'host' && s.hostId === S.currentHost.id);
  const session = {
    id: 'host-' + S.currentHost.id,
    type: 'host',
    hostId: S.currentHost.id,
    hostName: S.currentHost.name,
    hostObj: Object.assign({}, S.currentHost),
    detailData: window.detailData,
    tab: S.currentTab || 'overview',
    path: lastPath
  };
  if (existingIdx >= 0) {
    S.workspaceSessions[existingIdx] = session;
  } else {
    S.workspaceSessions.push(session);
  }
  const el = document.getElementById('hostDetailModal');
  if (el) {
    el.classList.remove('open');
    el.style.zIndex = '';
  }
  S.openModalStack = S.openModalStack.filter(mId => mId !== 'hostDetailModal');
  renderWorkspaceShelf();
}

function closeHostDetailModal() {
  if (S.currentHost) {
    S.workspaceSessions = S.workspaceSessions.filter(s => !(s.type === 'host' && s.hostId === S.currentHost.id));
    renderWorkspaceShelf();
  }
  closeModal('hostDetailModal');
}

function minimizeEditor() {
  if (!S.currentEditorState || !S.currentEditorState.path) {
    const el = document.getElementById('editorModal');
    if (el) {
      el.classList.remove('open');
      el.style.zIndex = '';
    }
    S.openModalStack = S.openModalStack.filter(mId => mId !== 'editorModal');
    return;
  }
  const sessId = 'editor-' + S.currentEditorState.hostId + '-' + S.currentEditorState.path;
  if (S.workspaceSessions.length >= 5 && !S.workspaceSessions.find(s => s.id === sessId)) {
    showToast(t('shelf_limit_reached'), 'warning');
    return;
  }
  const content = S.codeEditor ? S.codeEditor.getValue() : '';
  const isDirty = content !== S.currentEditorState.savedContent;
  const existingIdx = S.workspaceSessions.findIndex(s => s.id === sessId);
  const session = {
    id: sessId,
    type: 'editor',
    hostId: S.currentEditorState.hostId,
    hostName: S.currentEditorState.hostName,
    hostObj: S.currentEditorState.hostObj || S.currentHost,
    path: S.currentEditorState.path,
    content: content,
    savedContent: S.currentEditorState.savedContent,
    isDirty: isDirty
  };
  if (existingIdx >= 0) {
    S.workspaceSessions[existingIdx] = session;
  } else {
    S.workspaceSessions.push(session);
  }
  const el = document.getElementById('editorModal');
  if (el) {
    el.classList.remove('open');
    el.style.zIndex = '';
  }
  S.openModalStack = S.openModalStack.filter(mId => mId !== 'editorModal');
  renderWorkspaceShelf();
}

async function closeEditorModalWithConfirm() {
  if (S.codeEditor && S.currentEditorState && S.currentEditorState.path) {
    const isDirty = S.codeEditor.getValue() !== S.currentEditorState.savedContent;
    if (isDirty) {
      const ok = await showConfirm({
        title: t('confirm', 'Confirm'),
        message: t('discard_unsaved_changes'),
        confirmText: t('delete', 'Discard'),
        danger: true
      });
      if (!ok) return;
    }
    const sessId = 'editor-' + S.currentEditorState.hostId + '-' + S.currentEditorState.path;
    S.workspaceSessions = S.workspaceSessions.filter(s => s.id !== sessId);
    renderWorkspaceShelf();
  }
  closeModal('editorModal');
}

async function closeWorkspaceSession(sessionId, event) {
  if (event) event.stopPropagation();
  const session = S.workspaceSessions.find(s => s.id === sessionId);
  if (!session) return;
  if (session.type === 'editor' && session.isDirty) {
    const ok = await showConfirm({
      title: t('confirm', 'Confirm'),
      message: t('discard_unsaved_changes'),
      confirmText: t('delete', 'Discard'),
      danger: true
    });
    if (!ok) return;
  }
  S.workspaceSessions = S.workspaceSessions.filter(s => s.id !== sessionId);
  renderWorkspaceShelf();
}

async function restoreWorkspaceSession(sessionId) {
  const session = S.workspaceSessions.find(s => s.id === sessionId);
  if (!session) return;

  // Auto-minimize any currently open Workspace Modal safely
  if (S.openModalStack.includes('editorModal')) {
    minimizeEditor();
  }
  if (S.openModalStack.includes('hostDetailModal')) {
    minimizeHostDetail();
  }

  // Remove the session from minimized shelf since it will now be active in viewport
  S.workspaceSessions = S.workspaceSessions.filter(s => s.id !== sessionId);
  renderWorkspaceShelf();

  if (session.type === 'host') {
    S.currentHost = session.hostObj;
    window.detailData = session.detailData;
    document.getElementById('detailHostName').innerText = S.currentHost.name + ' (' + S.currentHost.host + ')';
    const badge = document.getElementById('detailHostBadge');
    badge.className = 'badge badge-' + S.currentHost.status;
    badge.innerText = S.currentHost.status;
    openModal('hostDetailModal');
    S.currentTab = session.tab || 'overview';
    switchTab(S.currentTab);
    if (session.tab === 'files' && session.path) {
      loadFiles(session.path);
    }
  } else if (session.type === 'editor') {
    S.currentHost = session.hostObj;
    S.currentEditorState = {
      hostId: session.hostId,
      hostName: session.hostName,
      hostObj: session.hostObj,
      path: session.path,
      savedContent: session.savedContent
    };
    document.getElementById('editorFilePath').innerText = session.path;
    openModal('editorModal');
    const mode = getEditorModeForPath(session.path);
    if (!S.codeEditor) {
      S.codeEditor = CodeMirror.fromTextArea(document.getElementById('codeEditorArea'), {
        lineNumbers: true,
        theme: 'vscode-dark',
        mode: mode
      });
    } else {
      S.codeEditor.setOption('theme', 'vscode-dark');
      S.codeEditor.setOption('mode', mode);
    }
    S.codeEditor.setValue(session.content);
    setTimeout(() => S.codeEditor.refresh(), 100);
  }
}

function updateHostSessionBadges() {
  document.querySelectorAll('.btn-host-view').forEach(btn => {
    const hostId = parseInt(btn.getAttribute('data-host-id'), 10);
    const badge = btn.querySelector('.host-session-badge');
    if (badge) {
      const hasActive = S.workspaceSessions.some(s => s.hostId === hostId);
      badge.style.display = hasActive ? 'inline-block' : 'none';
    }
  });
}

function renderWorkspaceShelf() {
  const shelf = document.getElementById('workspaceShelf');
  const chipsContainer = document.getElementById('workspaceShelfChips');
  updateHostSessionBadges();
  if (!shelf || !chipsContainer) return;

  if (S.workspaceSessions.length === 0) {
    shelf.style.display = 'none';
    document.body.classList.remove('has-workspace-shelf');
    return;
  }

  shelf.style.display = 'flex';
  document.body.classList.add('has-workspace-shelf');

  chipsContainer.innerHTML = S.workspaceSessions.map((s, idx) => {
    let title = '';
    let icon = s.type === 'editor' ? svgIcon('file-code', 'icon-sm') : svgIcon('server', 'icon-sm');
    if (s.type === 'host') {
      title = s.hostName + (s.tab === 'files' && s.path ? ': ' + s.path : ' (' + s.tab + ')');
    } else {
      const fileName = s.path.split('/').pop() || s.path;
      title = s.hostName + ': ' + fileName;
    }
    const tooltip = (s.type === 'editor' ? 'File: ' + s.path : 'Host: ' + s.hostName + ' (' + (s.tab === 'files' ? s.path : s.tab) + ')') + ' [Alt+' + (idx + 1) + ']';
    return `
      <div class="workspace-chip" onclick="restoreWorkspaceSession(${jsArg(s.id)})" title="${escapeHtml(tooltip)}">
        <span style="display: inline-flex; align-items: center;">${icon}</span>
        <span class="workspace-chip-title">${escapeHtml(title)}</span>
        ${s.isDirty ? '<span class="workspace-chip-dirty" title="Unsaved changes">•</span>' : ''}
        <button type="button" class="workspace-chip-close" onclick="closeWorkspaceSession(${jsArg(s.id)}, event)" title="${t('close_session', 'Close session')}">${svgIcon('x', 'icon-sm')}</button>
      </div>
    `;
  }).join('');
}

export { openHostDetail, openHostTerminal, formatProcessCmd, switchTab, renderInspectionRunsTab, switchRunsFilter, loadAndDisplayInspectionRuns, updatePkgCooldownUI, renderPackagesTab, loadHostPackages, rescanHostPackages, loadHostPackageUpdates, pkgUpdateCooldownPaint, checkHostPackageUpdates, startPkgUpdateCooldown, renderPkgUpdatesPanel, filterPackages, searchPackages, displayPackagesTable, secCooldownPaint, updateSecCooldownUI, loadSecurityTab, renderSecurityData, appendSecurityFooter, listenProcessName, listenProcessCellHtml, securityExposureHtml, getSecuritySeverityBadge, filterSecurityFindings, runHostSecurityScan, generateBaseline, onRuleKindChange, addRulePrompt, openAddRuleModal, editRulePrompt, submitRuleForm, deleteRule, refreshHostDetailAfterRuleChange, updateRuleSelection, toggleAllRules, deleteSelectedRules, stopDockerStatsTimer, startDockerStatsTimer, toggleDockerStatsAuto, setDockerSubTab, dockerHeaderHtml, dockerEmptyHtml, dockerTableHtml, dockerBar, dockerChips, renderDockerContainers, dockerStatsRowsHtml, dockerStatsTotalsText, renderDockerStats, refreshDockerStats, renderDockerVolumes, renderDockerNetworks, dockerRefreshIndicator, loadDockerTab, closeDockerMenu, openDockerMenu, stopDockerLogs, openDockerLogs, dockerAction, dockerRemoveContainer, getHostFileAccess, setHostFileAccess, resetHostFileAccess, filesApiUrl, renderFileAccessControls, onFileAccessSelectChange, onFileAccessCustomBlur, switchFileAccess, loadHostFilePins, loadFiles, renderQuickAccessBar, toggleBreadcrumbInput, renderBreadcrumbs, getFileIcon, renderFilesView, setupFileDragAndDrop, renderFilesTableBody, uploadRawFile, fileOpBanner, fileOpBannerClear, fileOpBannerAutoClear, fileOpBannerArchive, onFileFilterInput, clearFileFilter, setFileTypeFilter, toggleFileSort, downloadFolder, backupSelFor, backupBtnLabel, toggleBackupSel, backupSelected, uploadFile, deleteRemoteFile, generateDuplicateName, openDuplicateModal, executeDuplicate, openPinModal, submitSavePin, submitUnpin, openGlobalPinsModal, loadGlobalPins, onGlobalPinsSearch, renderGlobalPinsList, openTerminalFromGlobalPin, deleteGlobalPin, jumpToHostFile, toggleEditorMaximize, applyEditorMaximizeState, getEditorModeForPath, setupCodeEditorEvents, openNewEditor, openEditor, saveEditorFile, minimizeHostDetail, closeHostDetailModal, minimizeEditor, closeEditorModalWithConfirm, closeWorkspaceSession, restoreWorkspaceSession, updateHostSessionBadges, renderWorkspaceShelf };
