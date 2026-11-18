/* Bulk command execution and package upgrade logs.
 * Native ES module fragment of the former monolithic inline script.
 * Air-gapped: no bundler, no CDN. Shared state lives in the mutable S object
 * from 00-state.js; cross-domain functions are imported explicitly. */
import { S } from './00-state.js';
import { svgIcon } from './00-foundation.js';
import { t, showConfirm } from './01-i18n-auth.js';
import { showToast } from './02-hosts.js';
import { openModal, closeModal, escapeHtml } from './06-modal-docs-activity.js';

function openBulkExecModal() {
  bulkClearSelection();
  const cmdEl = document.getElementById('bulkCommandInput');
  if (cmdEl) cmdEl.value = '';
  const toEl = document.getElementById('bulkTimeoutInput');
  if (toEl) toEl.value = '30';
  const sudoEl = document.getElementById('bulkSudoInput');
  if (sudoEl) sudoEl.checked = false;

  // Populate Group Filter options
  const grpSelect = document.getElementById('bulkGroupFilter');
  if (grpSelect) {
    const groups = Array.from(new Set((S.allRawHosts || []).map(h => (h.group_name || '').trim()).filter(Boolean))).sort();
    grpSelect.innerHTML = `<option value="all">${t('bulk_all_groups', 'All Groups')}</option>` +
      groups.map(g => `<option value="${escapeHtml(g)}">${escapeHtml(g)}</option>`).join('') +
      `<option value="__ungrouped__">${t('ungrouped', 'Ungrouped')}</option>`;
    grpSelect.value = 'all';
  }
  const resCont = document.getElementById('bulkResultsContainer');
  if (resCont) resCont.style.display = 'none';
  const resBody = document.getElementById('bulkResultsBody');
  if (resBody) resBody.innerHTML = '';

  renderBulkHostPicker();
  openModal('bulkExecModal');
  setTimeout(() => document.getElementById('bulkHostSearch')?.focus(), 50);
}

function getFilteredBulkHosts() {
  const q = (document.getElementById('bulkHostSearch')?.value || '').toLowerCase().trim();
  const grp = document.getElementById('bulkGroupFilter')?.value || 'all';
  return (S.allRawHosts || []).filter(h => {
    if (grp !== 'all') {
      const hGrp = (h.group_name || '').trim();
      if (grp === '__ungrouped__' && hGrp !== '') return false;
      if (grp !== '__ungrouped__' && hGrp !== grp) return false;
    }
    if (!q) return true;
    return (h.name || '').toLowerCase().includes(q) || (h.host || '').toLowerCase().includes(q);
  });
}

function renderBulkHostPicker() {
  const container = document.getElementById('bulkHostPicker');
  if (!container) return;
  const hosts = getFilteredBulkHosts();
  const countEl = document.getElementById('bulkSelectedCount');
  if (countEl) countEl.innerText = `${S.bulkSelectedHostIds.size} / 20`;

  if (hosts.length === 0) {
    container.innerHTML = `<div style="grid-column: 1/-1; text-align: center; color: var(--text-muted); font-size: var(--fs-xs); padding: 12px;">No matching hosts</div>`;
    return;
  }

  container.innerHTML = hosts.map(h => {
    const checked = S.bulkSelectedHostIds.has(h.id);
    return `
      <label style="display: flex; align-items: center; gap: 8px; font-size: var(--fs-xs); cursor: pointer; padding: 4px 6px; border-radius: var(--radius-sm); background: ${checked ? 'var(--hover)' : 'transparent'};">
        <input type="checkbox" ${checked ? 'checked' : ''} onchange="toggleBulkHost(${h.id}, this.checked)" />
        <span style="font-weight: 500; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;" title="${escapeHtml(h.name)}">${escapeHtml(h.name)}</span>
        <span class="badge badge-${h.status || 'unknown'}" style="margin-left: auto; font-size: 0.68rem; padding: 0 4px;">${h.status || 'unknown'}</span>
      </label>
    `;
  }).join('');
}

function toggleBulkHost(id, checked) {
  if (checked) {
    if (S.bulkSelectedHostIds.size >= 20) {
      showToast(t('bulk_max_hosts_reached', 'Maximum 20 hosts can be selected.'), 'warning');
      renderBulkHostPicker();
      return;
    }
    S.bulkSelectedHostIds.add(id);
  } else {
    S.bulkSelectedHostIds.delete(id);
  }
  renderBulkHostPicker();
}

function bulkSelectAllFiltered() {
  const filtered = getFilteredBulkHosts();
  for (const h of filtered) {
    if (S.bulkSelectedHostIds.size >= 20) break;
    S.bulkSelectedHostIds.add(h.id);
  }
  renderBulkHostPicker();
}

function bulkClearSelection() {
  S.bulkSelectedHostIds.clear();
  renderBulkHostPicker();
}

async function runBulkExec() {
  const hostIds = Array.from(S.bulkSelectedHostIds);
  if (hostIds.length === 0) {
    showToast(t('bulk_no_hosts_selected', 'Please select at least 1 host.'), 'warning');
    return;
  }
  const cmd = document.getElementById('bulkCommandInput')?.value.trim() || '';
  if (!cmd) {
    showToast(t('command_required', 'Please enter a command to execute.'), 'error');
    return;
  }
  const timeoutSec = parseInt(document.getElementById('bulkTimeoutInput')?.value || '30', 10) || 30;
  const sudo = document.getElementById('bulkSudoInput')?.checked || false;

  const confirmed = await showConfirm({
    title: t('bulk_exec_title', 'Bulk Command Execution'),
    message: t('bulk_confirm_msg', 'Execute command across {count} host(s)? Command: {cmd}').replace('{count}', hostIds.length).replace('{cmd}', cmd.length > 80 ? cmd.slice(0, 80) + '…' : cmd),
    confirmText: t('bulk_run_btn', 'Run Command'),
    danger: true
  });
  if (!confirmed) return;

  const btn = document.getElementById('btnRunBulkExec');
  if (btn) btn.disabled = true;

  const resCont = document.getElementById('bulkResultsContainer');
  if (resCont) resCont.style.display = 'flex';
  const resBody = document.getElementById('bulkResultsBody');
  if (resBody) resBody.innerHTML = '';
  const statusSummary = document.getElementById('bulkStatusSummary');
  if (statusSummary) {
    statusSummary.innerText = `0 / ${hostIds.length}`;
    statusSummary.className = 'badge badge-info';
  }

  S.bulkExecutionResults = [];
  S.bulkExecAbortController = new AbortController();

  try {
    const res = await fetch('/api/exec/bulk', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ host_ids: hostIds, command: cmd, timeout_sec: timeoutSec, sudo: sudo }),
      signal: S.bulkExecAbortController.signal
    });

    if (!res.ok) {
      const errText = await res.text().catch(() => '');
      showToast(`Bulk execution failed: ${errText || res.statusText}`, 'error');
      return;
    }

    const reader = res.body.getReader();
    const decoder = new TextDecoder('utf-8');
    let buffer = '';
    let finishedCount = 0;
    let failedCount = 0;

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split('\n');
      buffer = lines.pop(); // keep partial chunk

      for (const line of lines) {
        const trimmed = line.trim();
        if (!trimmed) continue;
        try {
          const item = JSON.parse(trimmed);
          if (item.done) {
            if (statusSummary) {
              statusSummary.innerText = `Done: ${item.total} total, ${item.failed} failed`;
              statusSummary.className = item.failed > 0 ? 'badge badge-warning' : 'badge badge-healthy';
            }
          } else if (item.host_id) {
            S.bulkExecutionResults.push(item);
            finishedCount++;
            if (!item.ok) failedCount++;
            if (statusSummary) {
              statusSummary.innerText = `${finishedCount} / ${hostIds.length}`;
            }
            appendBulkResultRow(item);
          }
        } catch (e) {}
      }
    }

    // Process any remaining buffer byte
    if (buffer.trim()) {
      try {
        const item = JSON.parse(buffer.trim());
        if (item.host_id) {
          S.bulkExecutionResults.push(item);
          appendBulkResultRow(item);
        }
      } catch (e) {}
    }
  } catch (e) {
    if (e.name !== 'AbortError') {
      showToast(`Bulk execution error: ${e.message}`, 'error');
    }
  } finally {
    if (btn) btn.disabled = false;
    S.bulkExecAbortController = null;
  }
}

function appendBulkResultRow(item) {
  const resBody = document.getElementById('bulkResultsBody');
  if (!resBody) return;
  const tr = document.createElement('tr');
  tr.style.borderBottom = '1px solid var(--card-border)';
  const statusCls = item.ok ? 'badge-healthy' : 'badge-danger';
  const statusText = item.ok ? 'OK' : (item.error || 'Failed');
  const durationText = item.duration_ms !== undefined ? `${item.duration_ms}ms` : '-';
  const exitText = item.exit_code !== undefined ? item.exit_code : '-';
  const outText = item.output || (item.error ? `Error: ${item.error}` : '');

  tr.innerHTML = `
    <td style="padding: 6px 10px; font-weight: 600; white-space: nowrap;">${escapeHtml(item.host_name || ('Host #' + item.host_id))}</td>
    <td style="padding: 6px 10px; white-space: nowrap;"><span class="badge ${statusCls}">${escapeHtml(statusText)}</span></td>
    <td style="padding: 6px 10px; font-family: var(--font-mono);">${exitText}</td>
    <td style="padding: 6px 10px; color: var(--text-muted);">${durationText}</td>
    <td style="padding: 6px 10px;">
      <details>
        <summary style="cursor: pointer; color: var(--primary);">${outText.length > 50 ? escapeHtml(outText.slice(0, 50)) + '…' : (outText ? escapeHtml(outText) : '<no output>')}${item.truncated ? ' <span class="badge badge-warning" style="font-size: 0.65rem;">truncated</span>' : ''}</summary>
        <pre style="margin: 4px 0 0; padding: 6px; background: var(--code-bg); border-radius: var(--radius-sm); font-family: var(--font-mono); font-size: var(--fs-xs); white-space: pre-wrap; word-break: break-all; max-height: 120px; overflow-y: auto;">${escapeHtml(outText)}</pre>
      </details>
    </td>
  `;
  resBody.appendChild(tr);
}

function copyAllBulkResults() {
  if (S.bulkExecutionResults.length === 0) {
    showToast('No results to copy', 'info');
    return;
  }
  const text = S.bulkExecutionResults.map(r => {
    const name = r.host_name || ('Host #' + r.host_id);
    return `=== [${name}] ok=${r.ok} exit=${r.exit_code} duration=${r.duration_ms}ms ===\n${r.output || r.error || ''}\n`;
  }).join('\n');
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(text).then(() => showToast(t('copied', 'Copied to clipboard'), 'success'));
  }
}

async function openHostPackageUpgradePreview(hostId) {
  S.currentPkgUpgradeHostId = hostId;
  S.currentPkgUpgradePreviewData = null;
  const host = (S.allRawHosts || []).find(h => h.id === hostId) || S.currentHost;
  const hostName = host ? host.name : ('Host #' + hostId);

  const titleEl = document.getElementById('pkgUpgradePreviewTitle');
  if (titleEl) titleEl.innerText = `${t('pkg_upgrade_title', 'Upgrade Packages')}: ${hostName}`;

  const loadingEl = document.getElementById('pkgUpgradePreviewLoading');
  const contentEl = document.getElementById('pkgUpgradePreviewContent');
  // The preview can take a while on a host with many packages: show the elapsed time.
  if (loadingEl) loadingEl.innerHTML = SP.html(t('loading', 'Loading...'));
  if (loadingEl) loadingEl.style.display = 'block';
  if (contentEl) contentEl.style.display = 'none';

  openModal('pkgUpgradePreviewModal');

  try {
    const res = await fetch(`/api/hosts/${hostId}/packages/upgrade-preview`);
    const data = await res.json().catch(() => ({}));

    if (!res.ok || !data.ok) {
      if (loadingEl) loadingEl.innerHTML = `<div style="color: var(--danger);">Failed to load preview: ${escapeHtml(data.error || res.statusText)}</div>`;
      return;
    }
    if (!data.supported) {
      if (loadingEl) loadingEl.innerHTML = `<div style="color: var(--warning); padding: 16px; text-align: center;">${svgIcon('info', 'icon-sm')} <span>Package upgrade is unsupported on this host operating system (${escapeHtml(data.manager || 'unknown')}).</span></div>`;
      return;
    }

    S.currentPkgUpgradePreviewData = data;
    if (loadingEl) loadingEl.style.display = 'none';
    if (contentEl) contentEl.style.display = 'flex';

    document.getElementById('pkgPreviewPendingTotal').innerText = data.pending_total || 0;
    document.getElementById('pkgPreviewPendingSec').innerText = data.pending_security || 0;
    document.getElementById('pkgPreviewManager').innerText = (data.manager || '').toUpperCase();

    const rebootBanner = document.getElementById('pkgPreviewRebootBanner');
    if (rebootBanner) rebootBanner.style.display = data.reboot_required ? 'flex' : 'none';

    // Mode selection options: apt only supports "all", dnf/yum support "all" or "security"
    const secRadio = document.getElementById('pkgModeSecurityOption');
    const modeNote = document.getElementById('pkgModeNote');
    const allRadio = document.querySelector('input[name="pkgUpgradeMode"][value="all"]');
    if (allRadio) allRadio.checked = true;

    if (data.manager === 'apt') {
      if (secRadio) secRadio.style.display = 'none';
      if (modeNote) modeNote.innerText = 'Debian/Ubuntu (apt) applies all pending packages. Security-only mode is not reliably isolated by apt.';
    } else {
      if (secRadio) secRadio.style.display = 'inline-flex';
      if (modeNote) modeNote.innerText = 'RHEL/Fedora (dnf/yum) allows isolating upgrades to security advisories (--security).';
    }

    const tbody = document.getElementById('pkgPreviewTableBody');
    const pkgs = Array.isArray(data.packages) ? data.packages : [];
    if (tbody) {
      if (pkgs.length === 0) {
        tbody.innerHTML = `<tr><td colspan="4" style="text-align: center; color: var(--text-muted); padding: 12px;">No pending upgradable packages found. System is up to date.</td></tr>`;
      } else {
        tbody.innerHTML = pkgs.map(p => `
          <tr style="border-bottom: 1px solid var(--card-border);">
            <td style="padding: 6px 10px; font-weight: 600;">${escapeHtml(p.name)}</td>
            <td style="padding: 6px 10px; font-family: var(--font-mono); color: var(--text-muted);">${escapeHtml(p.from || '-')}</td>
            <td style="padding: 6px 10px; font-family: var(--font-mono); color: var(--primary);">${escapeHtml(p.to || '-')}</td>
            <td style="padding: 6px 10px;">
              <span class="badge ${p.security ? 'badge-danger' : 'badge-info'}" style="font-size: 0.68rem;">
                ${p.security ? 'Security' : 'Standard'}
              </span>
            </td>
          </tr>
        `).join('');
      }
    }
  } catch (e) {
    if (loadingEl) loadingEl.innerHTML = `<div style="color: var(--danger);">${escapeHtml(e.message)}</div>`;
  }
}

function onPkgUpgradeModeChange() {}

async function startHostPackageUpgrade() {
  if (!S.currentPkgUpgradeHostId) return;
  const hostId = S.currentPkgUpgradeHostId;
  const modeRadio = document.querySelector('input[name="pkgUpgradeMode"]:checked');
  const mode = modeRadio ? modeRadio.value : 'all';

  const confirmed = await showConfirm({
    title: t('pkg_upgrade_title', 'Upgrade Packages'),
    message: t('pkg_upgrade_confirm_msg', 'Apply package upgrades to this host? Remote services may restart automatically. There is NO automatic rollback mechanism. Consider taking a System Snapshot or disk snapshot beforehand.'),
    confirmText: t('pkg_confirm_upgrade_btn', 'Start Upgrade'),
    danger: true
  });
  if (!confirmed) return;

  closeModal('pkgUpgradePreviewModal');
  openPkgUpgradeLogs(hostId, mode);
}

function stopPkgUpgradeLogs() {
  const ws = S.pkgUpgradeSocket;
  S.pkgUpgradeSocket = null;
  if (ws) {
    try { ws.close(); } catch (e) {}
  }
}

function openPkgUpgradeLogs(hostId, mode, extra) {
  stopPkgUpgradeLogs();
  const host = (S.allRawHosts || []).find(h => h.id === hostId) || S.currentHost;
  const hostName = host ? host.name : ('Host #' + hostId);

  const titleEl = document.getElementById('pkgUpgradeLogsTitle');
  if (titleEl) titleEl.innerText = `${t('pkg_upgrade_title', 'Upgrade Packages')} [${mode}]: ${hostName}`;

  const out = document.getElementById('pkgUpgradeLogsOutput');
  const status = document.getElementById('pkgUpgradeLogsStatus');
  if (out) out.textContent = '';
  if (status) {
    status.textContent = 'Connecting...';
    status.className = 'badge badge-info';
  }

  openModal('pkgUpgradeLogsModal');

  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const query = extra || `mode=${encodeURIComponent(mode)}`;
  const ws = new WebSocket(`${protocol}//${location.host}/ws/pkg/upgrade?host_id=${hostId}&${query}`);
  ws.binaryType = 'arraybuffer';
  S.pkgUpgradeSocket = ws;

  const decoder = new TextDecoder('utf-8');
  let scrollQueued = false;

  ws.onopen = () => {
    if (S.pkgUpgradeSocket !== ws) return;
    if (status) {
      status.textContent = 'Running';
      status.className = 'badge badge-healthy';
    }
  };

  ws.onmessage = (ev) => {
    if (S.pkgUpgradeSocket !== ws) return;
    let text = typeof ev.data === 'string' ? ev.data : decoder.decode(ev.data, { stream: true });
    text = text.replace(/\x1b\[[0-9;?]*[A-Za-z]/g, '');
    if (!text) return;
    if (out) {
      out.appendChild(document.createTextNode(text));
      if (document.getElementById('pkgUpgradeLogsAutoScroll')?.checked && !scrollQueued) {
        scrollQueued = true;
        requestAnimationFrame(() => {
          scrollQueued = false;
          out.scrollTop = out.scrollHeight;
        });
      }
    }
    if (text.includes('[pantau] exit=')) {
      const exitMatch = text.match(/\[pantau\] exit=([0-9]+)/);
      const code = exitMatch ? exitMatch[1] : '0';
      if (status) {
        status.textContent = code === '0' ? 'Completed (exit 0)' : `Failed (exit ${code})`;
        status.className = code === '0' ? 'badge badge-healthy' : 'badge badge-danger';
      }
    }
  };

  ws.onclose = () => {
    if (S.pkgUpgradeSocket !== ws) return;
    S.pkgUpgradeSocket = null;
    if (status && !status.textContent.includes('exit')) {
      status.textContent = 'Disconnected';
      status.className = 'badge badge-unknown';
    }
  };
}

async function pkgAction(hostId, action, rawNames) {
  const names = String(rawNames || '').split(/[\s,]+/).filter(Boolean);
  if (!names.length) return;
  const list = names.join(' ');
  const label = { upgrade: 'Upgrade', install: 'Install', remove: 'Remove' }[action];
  const confirmed = await showConfirm({
    title: `${t('pkg_' + action + '_title', label)}: ${list}`,
    message: t('pkg_' + action + '_confirm_msg', label + ' ' + list + ' on this host? Remote services may restart automatically. There is NO automatic rollback.'),
    confirmText: t('pkg_' + action + '_btn', label),
    danger: action === 'remove'
  });
  if (!confirmed) return;
  openPkgUpgradeLogs(hostId, `${action} ${list}`, `action=${action}&packages=${encodeURIComponent(names.join(','))}`);
}

function openPkgInstallModal(hostId) {
  S.currentPkgUpgradeHostId = hostId;
  const input = document.getElementById('pkgInstallInput');
  if (input) input.value = '';
  openModal('pkgInstallModal');
  if (input) input.focus();
}

function submitPkgInstall() {
  const names = (document.getElementById('pkgInstallInput')?.value || '').trim();
  if (!names) return;
  closeModal('pkgInstallModal');
  pkgAction(S.currentPkgUpgradeHostId, 'install', names);
}

export { openBulkExecModal, getFilteredBulkHosts, renderBulkHostPicker, toggleBulkHost, bulkSelectAllFiltered, bulkClearSelection, runBulkExec, appendBulkResultRow, copyAllBulkResults, openHostPackageUpgradePreview, onPkgUpgradeModeChange, startHostPackageUpgrade, stopPkgUpgradeLogs, openPkgUpgradeLogs, pkgAction, openPkgInstallModal, submitPkgInstall };
