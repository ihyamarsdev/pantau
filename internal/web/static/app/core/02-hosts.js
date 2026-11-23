/* Host rendering, filters, groups, alerts, VPN, and host CRUD.
 * Native ES module fragment of the former monolithic inline script.
 * Air-gapped: no bundler, no CDN. Shared state lives in the mutable S object
 * from 00-state.js; cross-domain functions are imported explicitly. */
import { S } from './00-state.js';
import { svgIcon, batchAppend, rafBatch, debounce } from './00-foundation.js';
import { t, showConfirm, getUIPrefs, openEditNoteModal, clearSearch, hostStateClass, getFilteredAndSortedHosts, handleDropReorder, formatRelativeTime } from './01-i18n-auth.js';
import { openHostDetail, openHostTerminal } from './03-workspace-files-editor.js';
import { hostIpCopyHtml, openTerminalForHost, restoreSavedTerminalTabs } from './04-terminal.js';
import { formatBytes } from './05-settings-transfer.js';
import { openModal, closeModal, escapeHtml, jsArg } from './06-modal-docs-activity.js';
import { isHostStale, fleetCounts, recordHostTrafficSample, getHostTrafficHistory, renderSparklineSVG, setKpiFilter, renderKpiStrip, hostSecurityChip, getHostTopIssues, updateSideNavData } from './08-metrics-systemd-docker.js';
import { tfDlg, tfDlgClose, tfFoot, tfPrimary } from './12-terraform.js';

function renderHosts() {
  // Drop selections of hosts that no longer exist (deleted elsewhere or filtered by reload).
  for (const id of S.selectedHostIds) {
    if (!S.allRawHosts.some(h => h.id === id)) S.selectedHostIds.delete(id);
  }
  const prefs = getUIPrefs();
  const viewMode = prefs.viewMode || 'globe';
  const sortVal = document.getElementById('sortSelect')?.value || 'manual';
  const hasSearch = !!(document.getElementById('hostSearchInput')?.value.trim());
  const hasStatusFilter = (document.getElementById('statusFilterSelect')?.value || 'all') !== 'all';
  const isManualSort = sortVal === 'manual' && !hasSearch && !hasStatusFilter;

  const grid = document.getElementById('hostGrid');
  const listContainer = document.getElementById('hostList');
  const globe = document.getElementById('hostGlobe');

  if (viewMode === 'globe') {
    // Hidden grid/list cards would duplicate the detail card's element ids (dropdown-N, inspectBtn-N).
    grid.style.display = 'none';
    listContainer.style.display = 'none';
    grid.innerHTML = '';
    listContainer.innerHTML = '';
    globe.style.display = ''; // .host-globe-view CSS sets the grid layout
    if (window.GlobeRadar && S.currentSideNav === 'hosts') window.GlobeRadar.mount();
  } else {
    globe.style.display = 'none';
    if (window.GlobeRadar) window.GlobeRadar.unmount();
    if (viewMode === 'list') {
      grid.style.display = 'none';
      listContainer.style.display = 'block';
      renderListView(listContainer, isManualSort);
    } else {
      listContainer.style.display = 'none';
      grid.style.display = 'grid';
      renderGridView(grid, isManualSort);
    }
  }
  renderHostSelectBar();
}

function openHostGroupRename(name, ev) {
  if (ev) ev.stopPropagation();
  S.renamingHostGroup = name;
  const n = S.allRawHosts.filter(h => (h.group_name || '').trim() === name).length;
  const input = document.getElementById('hostGroupNewName');
  input.value = name;
  document.getElementById('hostGroupRenameHint').textContent = t('host_group_rename_hint', 'Renames the group on all {n} hosts. Ansible schedules that target this group follow the new name; playbooks that name the group in `hosts:` do not and must be edited.').replace('{n}', n);
  openModal('hostGroupModal');
  input.focus();
  input.select();
}

async function saveHostGroupRename() {
  const to = document.getElementById('hostGroupNewName').value.trim();
  if (!to) { showToast(t('host_group_rename_empty', 'Enter a group name'), 'warning'); return; }
  if (to === S.renamingHostGroup) { closeModal('hostGroupModal'); return; }
  const btn = document.getElementById('hostGroupSaveBtn');
  btn.disabled = true;
  try {
    const res = await fetch('/api/hosts/groups/rename', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ from: S.renamingHostGroup, to })
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
      showToast(t('error_prefix', 'Error: ') + (data.error || res.status), 'error');
      return;
    }
    closeModal('hostGroupModal');
    showToast(t('host_group_renamed', 'Group renamed to "{name}" ({n} hosts)').replace('{name}', data.name).replace('{n}', data.hosts), 'success');
    await loadHosts();
  } catch (e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error');
  } finally {
    btn.disabled = false;
  }
}

function deleteHostGroup(name, ev) {
  if (ev) ev.stopPropagation();
  // Every host in the group, not only the ones the current filter shows.
  const hosts = S.allRawHosts.filter(h => (h.group_name || '').trim() === name);
  deleteSelectedHosts(hosts, t('confirm_delete_group_title', 'Delete group "{name}" and its {n} hosts?').replace('{name}', name).replace('{n}', hosts.length));
}

function groupHosts(hosts) {
  const groupsMap = new Map();
  const ungrouped = [];
  hosts.forEach(h => {
    const g = (h.group_name || '').trim();
    if (!g) ungrouped.push(h);
    else {
      if (!groupsMap.has(g)) groupsMap.set(g, []);
      groupsMap.get(g).push(h);
    }
  });
  const result = [];
  const sortedNames = Array.from(groupsMap.keys()).sort((a, b) => a.localeCompare(b));
  for (const name of sortedNames) {
    result.push({ key: 'g:' + name, name, hosts: groupsMap.get(name), isUngrouped: false });
  }
  if (ungrouped.length > 0) {
    result.push({ key: '__ungrouped__', name: t('group_ungrouped', 'Ungrouped'), hosts: ungrouped, isUngrouped: true });
  }
  return result;
}

function getHostGroupsCollapsed() {
  try {
    const raw = localStorage.getItem('pantau_host_groups_collapsed');
    return new Set(raw ? JSON.parse(raw) : []);
  } catch (e) {
    return new Set();
  }
}

function toggleHostGroupCollapsed(key) {
  const set = getHostGroupsCollapsed();
  if (set.has(key)) set.delete(key);
  else set.add(key);
  try {
    localStorage.setItem('pantau_host_groups_collapsed', JSON.stringify(Array.from(set)));
  } catch (e) {}
  renderHosts();
}

function renderGridView(grid, isManualSort) {
  grid.innerHTML = '';
  const displayHosts = getFilteredAndSortedHosts();

  if (S.allRawHosts.length === 0) {
    grid.innerHTML = `
      <div class="empty-hosts-card">
        <svg class="empty-hosts-icon"><use href="#i-server"/></svg>
        <div style="font-weight: 600; font-size: var(--fs-md);">${t('empty_hosts_title', 'No hosts monitored yet')}</div>
        <div style="font-size: var(--fs-sm); color: var(--text-muted); max-width: 420px;">${t('empty_hosts_desc', 'Add your first server via SSH to start agentless monitoring and desired state checks.')}</div>
        <button class="btn btn-primary" onclick="openAddHostModal()" style="margin-top: 8px;"><svg class="icon icon-sm"><use href="#i-plus"/></svg> <span>${t('add_host_cta', '+ Add Host')}</span></button>
      </div>
    `;
    return;
  }

  if (displayHosts.length === 0) {
    grid.innerHTML = `
      <div class="empty-hosts-card">
        <svg class="empty-hosts-icon"><use href="#i-search"/></svg>
        <div style="font-weight: 600; font-size: var(--fs-md);">${t('empty_search_title', 'No hosts match your filter')}</div>
        <div style="font-size: var(--fs-sm); color: var(--text-muted); max-width: 420px;">${t('empty_search_desc', 'Try adjusting your search terms or clearing status and group filters.')}</div>
        <button class="btn btn-ghost" onclick="clearSearch(); setKpiFilter('all');" style="margin-top: 8px;"><span>${t('clear_filters', 'Clear Filters')}</span></button>
      </div>
    `;
    return;
  }

  const groups = groupHosts(displayHosts);
  const showDividers = groups.length > 1 || (groups.length === 1 && !groups[0].isUngrouped);
  const collapsed = getHostGroupsCollapsed();
  const nodes = [];
  groups.forEach(grp => {
    // A collapsed group keeps its header and hides its cards. Without dividers there is no header to click, so nothing collapses.
    const isCol = showDividers && collapsed.has(grp.key);
    if (showDividers) {
      const divider = document.createElement('div');
      divider.className = 'host-group-divider collapsible';
      divider.onclick = () => toggleHostGroupCollapsed(grp.key);
      divider.innerHTML = `
        <span class="host-group-title">
          <svg class="icon icon-xs grp-caret ${isCol ? 'collapsed' : ''}"><use href="#i-chevron-down"/></svg>
          <svg class="icon icon-sm"><use href="#i-folder"/></svg>
          ${escapeHtml(grp.name)}
        </span>
        <span style="display: inline-flex; align-items: center; gap: 6px;">
          <span class="badge" style="font-size: var(--fs-xs); background: var(--surface-2); color: var(--text-muted); border: 1px solid var(--card-border);">${grp.hosts.length}</span>
          ${grp.isUngrouped ? '' : `<button type="button" class="btn btn-icon btn-sm btn-ghost host-group-edit" onclick="openHostGroupRename(${jsArg(grp.name)}, event)" title="${escapeHtml(t('host_group_rename', 'Rename group'))}" aria-label="${escapeHtml(t('host_group_rename', 'Rename group'))}">${svgIcon('pencil', 'icon-xs')}</button>
          <button type="button" class="btn btn-icon btn-sm btn-ghost host-group-delete" onclick="deleteHostGroup(${jsArg(grp.name)}, event)" title="${escapeHtml(t('host_group_delete', 'Delete all hosts in group'))}" aria-label="${escapeHtml(t('host_group_delete', 'Delete all hosts in group'))}">${svgIcon('trash-2', 'icon-xs')}</button>`}
        </span>
      `;
      nodes.push(divider);
    }
    if (isCol) return;

    grp.hosts.forEach(h => nodes.push(buildHostCard(h, isManualSort)));
  });
  batchAppend(grid, nodes);
}

// One Grid-view host card; also the host detail beside the globe view.
function buildHostCard(h, isManualSort) {
  const card = document.createElement('div');
  card.dataset.hostId = h.id;

  const hist = getHostTrafficHistory(h);
  const sparklineSvg = renderSparklineSVG(hist, 80, 24);
  const isStale = isHostStale(h);
  const recency = formatRelativeTime(h.last_inspected);
  const issues = getHostTopIssues(h);
  const hasIssues = issues.length > 0;

  const ramPct = h.ram_total_bytes > 0 ? Math.round((h.ram_used_bytes / h.ram_total_bytes) * 100) : 0;
  const ramUsedStr = formatBytes(h.ram_used_bytes || 0);
  const ramTotStr = formatBytes(h.ram_total_bytes || 0);
  const ramClass = ramPct > 90 ? 'crit' : (ramPct > 80 ? 'warn' : 'normal');

  const diskPct = h.disk_total_bytes > 0 ? Math.round((h.disk_used_bytes / h.disk_total_bytes) * 100) : 0;
  const diskUsedStr = formatBytes(h.disk_used_bytes || 0);
  const diskTotStr = formatBytes(h.disk_total_bytes || 0);
  const diskClass = diskPct > 85 ? 'crit' : (diskPct > 80 ? 'warn' : 'normal');

  let load1m = 0;
  if (h.cpu_load) {
    const parts = h.cpu_load.split(',');
    if (parts.length > 0) load1m = parseFloat(parts[0].trim()) || 0;
  }
  const cores = h.cpu_cores > 0 ? h.cpu_cores : 1;
  const cpuClass = load1m > cores * 1.5 ? 'crit' : (load1m > cores ? 'warn' : 'normal');
  const cpuBarPct = Math.min(100, Math.round((load1m / cores) * 100));

  let scoreColor = 'var(--success)';
  if (h.lifecycle_score < 50) scoreColor = 'var(--danger)';
  else if (h.lifecycle_score < 80) scoreColor = 'var(--warning)';

  let problemClass = '';
  if (h.status === 'degraded' || h.drift) problemClass = 'problem-degraded';
  else if (h.status === 'down') problemClass = 'problem-down';
  else if (h.status === 'healthy' && (ramPct > 90 || diskPct > 85)) problemClass = 'problem-critical';
  else if (isStale) problemClass = 'problem-stale';

  card.className = `card host-card ${problemClass}${S.hostSelectMode && S.selectedHostIds.has(h.id) ? ' host-selected' : ''}`;

  if (isManualSort) {
    card.draggable = true;
    card.addEventListener('dragstart', (e) => {
      S.draggedHostId = h.id;
      card.classList.add('dragging');
      e.dataTransfer.effectAllowed = 'move';
      e.dataTransfer.setData('text/plain', String(h.id));
    });
    card.addEventListener('dragend', () => {
      card.classList.remove('dragging');
      document.querySelectorAll('.card.drag-target').forEach(el => el.classList.remove('drag-target'));
    });
    card.addEventListener('dragover', (e) => {
      e.preventDefault();
      e.dataTransfer.dropEffect = 'move';
      if (S.draggedHostId && S.draggedHostId !== h.id) {
        card.classList.add('drag-target');
      }
    });
    card.addEventListener('dragleave', () => {
      card.classList.remove('drag-target');
    });
    card.addEventListener('drop', (e) => {
      e.preventDefault();
      card.classList.remove('drag-target');
      const srcId = parseInt(e.dataTransfer.getData('text/plain')) || S.draggedHostId;
      if (srcId && srcId !== h.id) {
        handleDropReorder(srcId, h.id);
      }
    });
  }

  const noteSnippet = (h.notes || '').trim();
  const noteHtml = noteSnippet ? `
    <div class="host-card-note" onclick="openEditNoteModal(${h.id}, ${jsArg(h.notes || '')}, ${jsArg(h.name || '')})" title="${t('edit_host_note_title')}">
      <svg class="icon icon-sm text-muted"><use href="#i-pencil"/></svg>
      <span class="host-card-note-text">${escapeHtml(noteSnippet)}</span>
    </div>
  ` : '';

  const hostTabsCount = S.terminalTabs.filter(t => t.hostID === h.id).length;
  const hasActiveSession = S.workspaceSessions.some(s => s.hostId === h.id);

  card.innerHTML = `
    <div class="host-card-header">
      <div class="host-card-identity">
        <div class="host-card-title-row">
          ${S.hostSelectMode ? `<input type="checkbox" class="host-select-cb" data-host-id="${h.id}" ${S.selectedHostIds.has(h.id) ? 'checked' : ''} aria-label="${escapeHtml(h.name)}" />` : ''}
          ${isManualSort ? `<span class="drag-handle" title="${t('drag_handle_title')}">${svgIcon('grip-vertical', 'icon-sm')}</span>` : ''}
          <span class="status-dot ${hostStateClass(h.status)}" title="${h.status || 'unknown'}"></span>
          <span class="host-card-name">${escapeHtml(h.name)}</span>
          ${h.group_name ? `<span class="badge" style="font-size: var(--fs-xs); background: var(--surface-2); color: var(--text-muted); border: 1px solid var(--card-border);">${escapeHtml(h.group_name)}</span>` : ''}
          ${hostNetworkBadgeHtml(h)}
        </div>
        <div class="host-card-sub">${hostIpCopyHtml(h.host)}:${h.port} <span style="opacity: 0.7;">(${escapeHtml(h.user)})</span></div>
      </div>
      <div class="host-card-header-actions">
        <span class="badge badge-${h.status}">${h.status}</span>
        <button class="btn-inspect-quick" id="inspectBtn-${h.id}" onclick="inspectHost(${h.id}, event)" title="${t('btn_inspect_title', 'Run Inspection (Refresh)')}">
          <svg class="icon icon-sm"><use href="#i-rotate-ccw"/></svg>
        </button>
      </div>
    </div>

    ${hasIssues ? `
      <div class="card-issue-box">
        ${issues.map(iss => `
          <div class="card-issue-row">
            <span class="card-issue-badge ${iss.level}">${iss.badge}</span>
            <span style="overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">${escapeHtml(iss.text)}</span>
          </div>
        `).join('')}
      </div>
    ` : ''}

    <div class="host-gauge-grid">
      <div class="host-gauge-item">
        <div class="host-gauge-label">
          <span>CPU</span>
          <span class="host-gauge-val num">${load1m.toFixed(2)}</span>
        </div>
        <div class="host-gauge-bar"><div class="host-gauge-fill ${cpuClass}" style="width: ${cpuBarPct}%"></div></div>
      </div>
      <div class="host-gauge-item">
        <div class="host-gauge-label">
          <span>RAM</span>
          <span class="host-gauge-val num">${ramPct}%</span>
        </div>
        <div class="host-gauge-bar"><div class="host-gauge-fill ${ramClass}" style="width: ${ramPct}%"></div></div>
      </div>
      <div class="host-gauge-item">
        <div class="host-gauge-label">
          <span>Disk</span>
          <span class="host-gauge-val num">${diskPct}%</span>
        </div>
        <div class="host-gauge-bar"><div class="host-gauge-fill ${diskClass}" style="width: ${diskPct}%"></div></div>
      </div>
    </div>

    <div class="host-traffic-strip">
      ${sparklineSvg}
      <div class="host-traffic-rates">
        <div class="host-traffic-rate-row rate-in">
          <span>↓</span> <span class="num">${formatBytes(h.net_rx_speed_bps)}/s</span>
        </div>
        <div class="host-traffic-rate-row rate-out">
          <span>↑</span> <span class="num">${formatBytes(h.net_tx_speed_bps)}/s</span>
        </div>
      </div>
    </div>

    ${noteHtml}

    <div class="host-meta-row">
      <div class="host-meta-left">
        <span>${escapeHtml(h.os_info || 'Unknown')}</span>
        <span>•</span>
        <span>${escapeHtml(h.uptime || '-')}</span>
        <span>•</span>
        <span title="${recency.title}" style="${recency.isStale ? 'color: var(--warning);' : ''}">${recency.relative}</span>
      </div>
      <span style="display: inline-flex; gap: 4px;">
        ${hostSecurityChip(h)}
        <span class="host-score-chip" onclick="openHostDetail(${h.id}, 'overview')" title="${t('lifecycle_score')}" style="color: ${scoreColor};">
          <svg class="icon icon-sm"><use href="#i-gauge"/></svg>
          <span class="num">${h.lifecycle_score}</span>
        </span>
      </span>
    </div>

    <div class="host-actions-row">
      <button class="btn btn-primary btn-sm btn-host-view" data-host-id="${h.id}" onclick="openHostDetail(${h.id})" style="display: inline-flex; align-items: center; gap: 6px;">
        <svg class="icon icon-sm"><use href="#i-eye"/></svg>
        <span>${t('btn_view')}</span>
        <span class="badge host-session-badge" style="${hasActiveSession ? '' : 'display: none;'} font-size: 0.68rem; padding: 1px 4px; background: var(--accent-soft); color: var(--accent-text); border: 1px solid var(--accent-ring);" title="${t('active_session_badge')}"><svg class="icon icon-sm"><use href="#i-layers"/></svg></span>
      </button>
      <button class="btn btn-sm btn-ghost btn-host-terminal" data-host-id="${h.id}" onclick="openHostTerminal(${h.id})" title="${t('btn_terminal')}" style="display: inline-flex; align-items: center; gap: 4px;">
        <svg class="icon icon-sm"><use href="#i-terminal"/></svg>
        <span class="badge badge-healthy host-terminal-badge" style="${hostTabsCount > 0 ? '' : 'display: none;'} margin-left: 2px; font-size: 0.72rem; padding: 1px 5px;">${hostTabsCount}</span>
      </button>
      <div class="dropdown" style="margin-left: auto;">
        <button class="btn btn-sm btn-icon btn-ghost" onclick="toggleDropdown(event, ${h.id})" title="${t('more_actions', 'More Actions')}"><svg class="icon icon-sm"><use href="#i-ellipsis-vertical"/></svg></button>
        <div class="dropdown-menu" id="dropdown-${h.id}">
          <button class="dropdown-item" onclick="openEditHostModal(${h.id})"><svg class="icon icon-sm"><use href="#i-pencil"/></svg> <span>${t('edit_host', 'Edit Host')}</span></button>
          <button class="dropdown-item" onclick="testHostSSH(${h.id})"><svg class="icon icon-sm"><use href="#i-zap"/></svg> <span>${t('test_connection', 'Test Connection')}</span></button>
          <button class="dropdown-item" onclick="openInjectKeyModal(${h.id}, ${jsArg(h.name)})"><svg class="icon icon-sm"><use href="#i-key-round"/></svg> <span>${t('btn_inject_key', 'Inject Key')}</span></button>
          <button class="dropdown-item" onclick="window.PortFwd && PortFwd.open(${h.id})"><svg class="icon icon-sm"><use href="#i-plug"/></svg> <span>${t('pf_menu', 'Port Forward')}</span></button>
          ${!h.notes ? `<button class="dropdown-item" onclick="openEditNoteModal(${h.id}, '', ${jsArg(h.name || '')})"><svg class="icon icon-sm"><use href="#i-sticky-note"/></svg> <span>${t('add_note', 'Add Note')}</span></button>` : ''}
          <div class="dropdown-divider"></div>
          <button class="dropdown-item danger" onclick="deleteHost(${h.id})"><svg class="icon icon-sm"><use href="#i-trash-2"/></svg> <span>${t('delete_host', 'Delete Host')}</span></button>
        </div>
      </div>
    </div>
  `;
  return card;
}

function renderListView(listContainer, isManualSort) {
  listContainer.innerHTML = '';
  const displayHosts = getFilteredAndSortedHosts();

  if (S.allRawHosts.length === 0) {
    listContainer.innerHTML = `
      <div class="empty-hosts-card">
        <svg class="empty-hosts-icon"><use href="#i-server"/></svg>
        <div style="font-weight: 600; font-size: var(--fs-md);">${t('empty_hosts_title', 'No hosts monitored yet')}</div>
        <div style="font-size: var(--fs-sm); color: var(--text-muted); max-width: 420px;">${t('empty_hosts_desc', 'Add your first server via SSH to start agentless monitoring and desired state checks.')}</div>
        <button class="btn btn-primary" onclick="openAddHostModal()" style="margin-top: 8px;"><svg class="icon icon-sm"><use href="#i-plus"/></svg> <span>${t('add_host_cta', '+ Add Host')}</span></button>
      </div>
    `;
    return;
  }

  if (displayHosts.length === 0) {
    listContainer.innerHTML = `
      <div class="empty-hosts-card">
        <svg class="empty-hosts-icon"><use href="#i-search"/></svg>
        <div style="font-weight: 600; font-size: var(--fs-md);">${t('empty_search_title', 'No hosts match your filter')}</div>
        <div style="font-size: var(--fs-sm); color: var(--text-muted); max-width: 420px;">${t('empty_search_desc', 'Try adjusting your search terms or clearing status and group filters.')}</div>
        <button class="btn btn-ghost" onclick="clearSearch(); setKpiFilter('all');" style="margin-top: 8px;"><span>${t('clear_filters', 'Clear Filters')}</span></button>
      </div>
    `;
    return;
  }

  const table = document.createElement('table');
  table.className = 'host-list-table';
  table.innerHTML = `
    <thead>
      <tr>
        ${isManualSort ? '<th style="width: 32px;"></th>' : ''}
        <th style="width: 80px;">${t('col_status')}</th>
        <th>${t('col_name')}</th>
        <th>${t('group')}</th>
        <th>${t('col_os')}</th>
        <th style="width: 100px;">CPU</th>
        <th style="width: 110px;">${t('col_ram')}</th>
        <th style="width: 110px;">${t('col_disk')}</th>
        <th style="width: 140px;">${t('col_traffic')}</th>
        <th style="width: 70px;">${t('col_score')}</th>
        <th style="width: 70px;">${t('tab_security', 'Security')}</th>
        <th style="width: 70px;">${t('card_issues', 'Issues')}</th>
        <th style="width: 110px;">${t('col_last_inspected', 'Inspected')}</th>
        <th style="width: 140px;">${t('col_actions')}</th>
      </tr>
    </thead>
    <tbody></tbody>
  `;
  const tbody = table.querySelector('tbody');
  const rows = [];

  const groups = groupHosts(displayHosts);
  const showDividers = groups.length > 1 || (groups.length === 1 && !groups[0].isUngrouped);
  const collapsed = getHostGroupsCollapsed();

  groups.forEach(grp => {
    const isCol = showDividers && collapsed.has(grp.key);
    if (showDividers) {
      const headerRow = document.createElement('tr');
      headerRow.className = 'host-list-group-row collapsible';
      headerRow.onclick = () => toggleHostGroupCollapsed(grp.key);
      headerRow.innerHTML = `
        <td colspan="${isManualSort ? 14 : 13}">
          <div style="display: flex; align-items: center; gap: 8px;">
            <svg class="icon icon-xs grp-caret ${isCol ? 'collapsed' : ''}"><use href="#i-chevron-down"/></svg>
            <svg class="icon icon-sm"><use href="#i-folder"/></svg>
            <span>${escapeHtml(grp.name)}</span>
            <span class="badge" style="font-size: var(--fs-xs); background: var(--card-bg); color: var(--text-muted); border: 1px solid var(--card-border);">${grp.hosts.length}</span>
            ${grp.isUngrouped ? '' : `<button type="button" class="btn btn-icon btn-sm btn-ghost host-group-edit" onclick="openHostGroupRename(${jsArg(grp.name)}, event)" title="${escapeHtml(t('host_group_rename', 'Rename group'))}" aria-label="${escapeHtml(t('host_group_rename', 'Rename group'))}">${svgIcon('pencil', 'icon-xs')}</button>
            <button type="button" class="btn btn-icon btn-sm btn-ghost host-group-delete" onclick="deleteHostGroup(${jsArg(grp.name)}, event)" title="${escapeHtml(t('host_group_delete', 'Delete all hosts in group'))}" aria-label="${escapeHtml(t('host_group_delete', 'Delete all hosts in group'))}">${svgIcon('trash-2', 'icon-xs')}</button>`}
          </div>
        </td>
      `;
      rows.push(headerRow);
    }
    if (isCol) return;

    grp.hosts.forEach(h => {
      const tr = document.createElement('tr');
      tr.dataset.hostId = h.id;

      const hist = getHostTrafficHistory(h);
      const sparklineSvg = renderSparklineSVG(hist, 60, 20);
      const isStale = isHostStale(h);
      const recency = formatRelativeTime(h.last_inspected);
      const issues = getHostTopIssues(h);
      const hasIssues = issues.length > 0;

      const ramPct = h.ram_total_bytes > 0 ? Math.round((h.ram_used_bytes / h.ram_total_bytes) * 100) : 0;
      const ramClass = ramPct > 90 ? 'crit' : (ramPct > 80 ? 'warn' : 'normal');

      const diskPct = h.disk_total_bytes > 0 ? Math.round((h.disk_used_bytes / h.disk_total_bytes) * 100) : 0;
      const diskClass = diskPct > 85 ? 'crit' : (diskPct > 80 ? 'warn' : 'normal');

      let load1m = 0;
      if (h.cpu_load) {
        const parts = h.cpu_load.split(',');
        if (parts.length > 0) load1m = parseFloat(parts[0].trim()) || 0;
      }
      const cores = h.cpu_cores > 0 ? h.cpu_cores : 1;
      const cpuClass = load1m > cores * 1.5 ? 'crit' : (load1m > cores ? 'warn' : 'normal');
      const cpuBarPct = Math.min(100, Math.round((load1m / cores) * 100));

      let scoreColor = 'var(--success)';
      if (h.lifecycle_score < 50) scoreColor = 'var(--danger)';
      else if (h.lifecycle_score < 80) scoreColor = 'var(--warning)';

      let problemClass = '';
      if (h.status === 'degraded' || h.drift) problemClass = 'problem-degraded';
      else if (h.status === 'down') problemClass = 'problem-down';
      else if (isStale) problemClass = 'problem-stale';

      tr.className = problemClass + (S.hostSelectMode && S.selectedHostIds.has(h.id) ? ' host-selected' : '');

      if (isManualSort) {
        tr.draggable = true;
        tr.addEventListener('dragstart', (e) => {
          S.draggedHostId = h.id;
          tr.classList.add('dragging');
          e.dataTransfer.effectAllowed = 'move';
          e.dataTransfer.setData('text/plain', String(h.id));
        });
        tr.addEventListener('dragend', () => {
          tr.classList.remove('dragging');
          document.querySelectorAll('.host-list-table tr.drag-target').forEach(el => el.classList.remove('drag-target'));
        });
        tr.addEventListener('dragover', (e) => {
          e.preventDefault();
          e.dataTransfer.dropEffect = 'move';
          if (S.draggedHostId && S.draggedHostId !== h.id) {
            tr.classList.add('drag-target');
          }
        });
        tr.addEventListener('dragleave', () => {
          tr.classList.remove('drag-target');
        });
        tr.addEventListener('drop', (e) => {
          e.preventDefault();
          tr.classList.remove('drag-target');
          const srcId = parseInt(e.dataTransfer.getData('text/plain')) || S.draggedHostId;
          if (srcId && srcId !== h.id) {
            handleDropReorder(srcId, h.id);
          }
        });
      }

      const hostTabsCount = S.terminalTabs.filter(t => t.hostID === h.id).length;
      const hasActiveSession = S.workspaceSessions.some(s => s.hostId === h.id);

      tr.onclick = (e) => {
        if (e.target.closest('button, .dropdown, a, input, .dropdown-menu, [data-copy-ip]')) return;
        if (S.hostSelectMode) toggleHostSelected(h.id, !S.selectedHostIds.has(h.id));
        else openHostDetail(h.id);
      };

      tr.innerHTML = `
        ${isManualSort ? `<td style="text-align: center;"><span class="drag-handle" title="${t('drag_handle_title')}">${svgIcon('grip-vertical', 'icon-sm')}</span></td>` : ''}
        <td>
          <div style="display: inline-flex; align-items: center; gap: 6px;">
            ${S.hostSelectMode ? `<input type="checkbox" class="host-select-cb" data-host-id="${h.id}" ${S.selectedHostIds.has(h.id) ? 'checked' : ''} aria-label="${escapeHtml(h.name)}" />` : ''}
            <span class="status-dot ${hostStateClass(h.status)}" title="${h.status || 'unknown'}"></span>
            <span class="badge badge-${h.status}">${h.status}</span>
          </div>
        </td>
        <td>
          <div style="display: flex; flex-direction: column; gap: 2px;">
            <b style="color: var(--text);">${escapeHtml(h.name)}</b>
            <span class="mono text-muted" style="font-size: var(--fs-xs);">${hostIpCopyHtml(h.host)}:${h.port}</span>
            <span style="display: inline-flex; gap: 4px; align-items: center;">${hostNetworkBadgeHtml(h)}</span>
          </div>
        </td>
        <td>
          ${h.group_name ? `<span class="badge" style="font-size: var(--fs-xs); background: var(--surface-2); color: var(--text-muted); border: 1px solid var(--card-border);">${escapeHtml(h.group_name)}</span>` : '<span class="text-faint">-</span>'}
        </td>
        <td style="color: var(--text-muted); font-size: var(--fs-xs); max-width: 140px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">${escapeHtml(h.os_info || '-')}</td>
        <td>
          <div style="font-size: var(--fs-xs); margin-bottom: 3px;" class="num">${load1m.toFixed(2)}</div>
          <div class="host-gauge-bar" style="width: 70px;"><div class="host-gauge-fill ${cpuClass}" style="width: ${cpuBarPct}%"></div></div>
        </td>
        <td>
          <div style="font-size: var(--fs-xs); margin-bottom: 3px;" class="num">${ramPct}% <span class="text-faint">(${formatBytes(h.ram_total_bytes || 0)})</span></div>
          <div class="host-gauge-bar" style="width: 75px;"><div class="host-gauge-fill ${ramClass}" style="width: ${ramPct}%"></div></div>
        </td>
        <td>
          <div style="font-size: var(--fs-xs); margin-bottom: 3px;" class="num">${diskPct}% <span class="text-faint">(${formatBytes(h.disk_total_bytes || 0)})</span></div>
          <div class="host-gauge-bar" style="width: 75px;"><div class="host-gauge-fill ${diskClass}" style="width: ${diskPct}%"></div></div>
        </td>
        <td>
          <div style="display: flex; align-items: center; gap: 6px;">
            ${sparklineSvg}
            <div class="mono" style="font-size: 0.7rem; color: var(--text-muted); line-height: 1.2;">
              <div class="rate-in">↓ ${formatBytes(h.net_rx_speed_bps)}/s</div>
              <div class="rate-out">↑ ${formatBytes(h.net_tx_speed_bps)}/s</div>
            </div>
          </div>
        </td>
        <td>
          <span class="host-score-chip" style="color: ${scoreColor}; font-size: var(--fs-xs);">${h.lifecycle_score}</span>
        </td>
        <td>
          ${hostSecurityChip(h) || '<span class="text-faint" style="font-size: var(--fs-xs);">-</span>'}
        </td>
        <td>
          ${hasIssues ? `<span class="card-issue-badge ${issues[0].level}" title="${escapeHtml(issues[0].text)}">${issues.length}</span>` : '<span class="text-faint" style="font-size: var(--fs-xs);">0</span>'}
        </td>
        <td title="${recency.title}" style="font-size: var(--fs-xs); white-space: nowrap; ${recency.isStale ? 'color: var(--warning); font-weight: 500;' : 'color: var(--text-muted);'}">
          ${recency.relative}${h.last_duration_ms > 0 ? `<div style="font-size: 0.7rem; color: var(--success);">${h.last_duration_ms}ms</div>` : ''}
        </td>
        <td>
          <div style="display: inline-flex; gap: 4px; align-items: center;">
            <button class="btn btn-inspect-quick" id="inspectBtnList-${h.id}" onclick="inspectHost(${h.id}, event)" title="${t('btn_inspect_title', 'Run Inspection (Refresh)')}">
              <svg class="icon icon-sm"><use href="#i-rotate-ccw"/></svg>
            </button>
            <button class="btn btn-primary btn-sm btn-host-view" data-host-id="${h.id}" onclick="openHostDetail(${h.id})" style="display: inline-flex; align-items: center; gap: 4px;">
              <svg class="icon icon-sm"><use href="#i-eye"/></svg>
              <span>${t('btn_view')}</span>
              <span class="badge host-session-badge" style="${hasActiveSession ? '' : 'display: none;'} font-size: 0.68rem; padding: 1px 4px; background: var(--accent-soft); color: var(--accent-text); border: 1px solid var(--accent-ring);" title="${t('active_session_badge')}"><svg class="icon icon-sm"><use href="#i-layers"/></svg></span>
            </button>
            <button class="btn btn-sm btn-ghost btn-host-terminal" data-host-id="${h.id}" onclick="openHostTerminal(${h.id})" title="${t('btn_terminal')}" style="display: inline-flex; align-items: center; gap: 4px;">
              <svg class="icon icon-sm"><use href="#i-terminal"/></svg>
              <span class="badge badge-healthy host-terminal-badge" style="${hostTabsCount > 0 ? '' : 'display: none;'} margin-left: 2px; font-size: 0.72rem; padding: 1px 5px;">${hostTabsCount}</span>
            </button>
            <div class="dropdown">
              <button class="btn btn-sm btn-icon btn-ghost" onclick="toggleDropdown(event, 'list-${h.id}')" title="${t('more_actions', 'More Actions')}"><svg class="icon icon-sm"><use href="#i-ellipsis-vertical"/></svg></button>
              <div class="dropdown-menu" id="dropdown-list-${h.id}">
                <button class="dropdown-item" onclick="openEditHostModal(${h.id})"><svg class="icon icon-sm"><use href="#i-pencil"/></svg> <span>${t('edit_host', 'Edit Host')}</span></button>
                <button class="dropdown-item" onclick="testHostSSH(${h.id})"><svg class="icon icon-sm"><use href="#i-zap"/></svg> <span>${t('test_connection', 'Test Connection')}</span></button>
                <button class="dropdown-item" onclick="openInjectKeyModal(${h.id}, ${jsArg(h.name)})"><svg class="icon icon-sm"><use href="#i-key-round"/></svg> <span>${t('btn_inject_key', 'Inject Key')}</span></button>
                <button class="dropdown-item" onclick="window.PortFwd && PortFwd.open(${h.id})"><svg class="icon icon-sm"><use href="#i-plug"/></svg> <span>${t('pf_menu', 'Port Forward')}</span></button>
                <div class="dropdown-divider"></div>
                <button class="dropdown-item danger" onclick="deleteHost(${h.id})"><svg class="icon icon-sm"><use href="#i-trash-2"/></svg> <span>${t('delete_host', 'Delete Host')}</span></button>
              </div>
            </div>
          </div>
        </td>
      `;
      rows.push(tr);
    });
  });
  batchAppend(tbody, rows);
  listContainer.appendChild(table);
  fitHostList(listContainer);
  if (!listContainer._fitObserver) {
    listContainer.setAttribute('data-fit-observer', '');
    listContainer._fitObserver = new ResizeObserver(() => fitHostList(listContainer));
    listContainer._fitObserver.observe(listContainer);
  }
}

function fitHostList(view) {
  view.classList.remove('hlv-scroll');
  const table = view.querySelector('.host-list-table');
  if (table && table.offsetWidth > view.clientWidth) view.classList.add('hlv-scroll');
}

function setEngineStatus(online) {
  S.lastEngineStatus = online;
  const dot = document.getElementById('footerEngineDot');
  const txt = document.getElementById('footerEngineText');
  if (!dot || !txt) return;
  if (online) {
    dot.className = 'status-dot online';
    if (S.currentPollInterval === 0) {
      txt.innerHTML = `${t('footer_engine_active', 'Engine Active')} <span style="color: var(--warning); font-size: 0.72rem; margin-left: 4px;">(${t('inspection_paused_footer', 'Inspection Paused')})</span>`;
    } else {
      txt.innerText = t('footer_engine_active', 'Engine Active');
    }
    txt.style.color = 'var(--text-muted)';
  } else {
    dot.className = 'status-dot offline';
    txt.innerText = t('footer_engine_disconnected', 'Disconnected');
    txt.style.color = 'var(--danger)';
  }
}

function updateFooterMetrics() {
  const el = document.getElementById('footerMetricsText');
  if (el && S.allRawHosts) {
    const { total, healthy, drift, down } = fleetCounts(S.allRawHosts);
    const driftColor = drift > 0 ? 'var(--warning)' : 'var(--text-muted)';
    const downPart = down > 0 ? ` • <span style="color: var(--danger); font-weight: 600;">${down} ${t('kpi_offline', 'Offline')}</span>` : '';
    el.innerHTML = `${total} ${t('footer_hosts', 'Hosts')} • <span style="color: var(--success);">${healthy} ${t('footer_healthy', 'Healthy')}</span> • <span style="color: ${driftColor}; font-weight: ${drift > 0 ? '600' : 'normal'};">${drift} ${t('footer_drift', 'Drift')}</span>${downPart}`;
  }
  if (typeof renderKpiStrip === 'function') {
    renderKpiStrip(S.allRawHosts);
  }
}

async function loadVpnNetworkIndex(force) {
  if (!force && Date.now() - S.vpnIndexAt < 30000) return S.vpnNetworks;
  try {
    const res = await fetch('/api/vpn/networks');
    if (res.ok) { S.vpnNetworks = await res.json() || []; S.vpnIndexAt = Date.now(); }
  } catch (e) { /* cards just render without a network badge */ }
  return S.vpnNetworks;
}

function populateHostNetworkSelect(selected) {
  const sel = document.getElementById('hostNetwork');
  if (!sel) return;
  sel.innerHTML = `<option value="0">${escapeHtml(t('host_network_direct', 'Direct (no VPN)'))}</option>` +
    (S.vpnNetworks || []).map(n => `<option value="${Number(n.id)}"${Number(n.id) === Number(selected) ? ' selected' : ''}>${escapeHtml(n.name)}${n.last_state && n.last_state !== 'up' ? ' (' + escapeHtml(n.last_state) + ')' : ''}</option>`).join('');
  sel.value = String(selected || 0);
}

async function loadHosts() {
  try {
    const res = await fetch('/api/hosts');
    if (!res.ok) {
      setEngineStatus(false);
      return;
    }
    S.allRawHosts = await res.json() || [];
    if (S.allRawHosts.some(h => h.network_id)) await loadVpnNetworkIndex(false);
    S.allRawHosts.forEach(recordHostTrafficSample);
    updateGroupSuggestions();
    setEngineStatus(true);
    updateFooterMetrics();
    renderHosts();
    if (typeof updateSideNavData === 'function') updateSideNavData();
    if (!window._tabsRestoredOnce) {
      window._tabsRestoredOnce = true;
      restoreSavedTerminalTabs();
    }
  } catch(e) {
    setEngineStatus(false);
  }
}

function toggleAlertOverlay(e) {
  if (e) e.stopPropagation();
  S.alertOverlayOpen = !S.alertOverlayOpen;
  renderAlertOverlayState();
  if (S.alertOverlayOpen) {
    (S.latestAlerts || []).forEach(a => { loadAlertTriage(a.id).then(changed => { if (changed) loadAlerts(); }); });
  }
}

function closeAlertOverlay() {
  if (!S.alertOverlayOpen) return;
  S.alertOverlayOpen = false;
  renderAlertOverlayState();
}

function renderAlertOverlayState() {
  const panel = document.getElementById('alertOverlayPanel');
  const btn = document.getElementById('btnToggleAlertPanel');
  if (panel) {
    panel.style.display = S.alertOverlayOpen ? 'flex' : 'none';
  }
  if (btn) {
    btn.innerHTML = `<span>${t('view_details', 'View Details')}</span> ${svgIcon(S.alertOverlayOpen ? 'chevron-up' : 'chevron-down', 'icon-sm')}`;
  }
}


// Delta-only Daily Briefing card. Rendered from the persisted briefing; zero external libraries.
async function loadDailyBriefing() {
  const card = document.getElementById('dailyBriefingCard');
  if (!card) return;
  try {
    const res = await fetch('/api/agent/briefing');
    if (!res.ok) return;
    const b = await res.json();
    if (!b || !b.summary) return;
    const anomalies = Number(b.anomaly_count) || 0;
    card.style.display = 'flex';
    card.innerHTML = `
      <span class="briefing-icon">${svgIcon(anomalies ? 'triangle-alert' : 'circle-check', 'icon-md')}</span>
      <div class="briefing-body">
        <div class="briefing-title">${t('briefing_title', 'Daily Briefing')} <span class="briefing-count">${anomalies} ${t('briefing_anomalies', 'anomalies')}</span></div>
        <div class="briefing-summary">${escapeHtml(b.summary)}</div>
      </div>
      <div class="briefing-actions">
        <button class="btn btn-sm btn-ghost" onclick="generateDailyBriefing()" title="${t('briefing_refresh', 'Regenerate')}">${svgIcon('refresh-cw', 'icon-sm')}</button>
      </div>`;
  } catch (_) { /* briefing is optional */ }
}

async function generateDailyBriefing() {
  const btn = document.querySelector('#dailyBriefingCard .briefing-actions button');
  if (btn) btn.disabled = true;
  try {
    await fetch('/api/agent/briefing/generate', { method: 'POST' });
    await loadDailyBriefing();
  } finally {
    if (btn) btn.disabled = false;
  }
}
// AI auto-diagnosis results, keyed by alert id. Fetched lazily when the alert panel opens.
S.alertTriage = S.alertTriage || {};

async function loadAlertTriage(alertId) {
  if (S.alertTriage[alertId]) return false;
  try {
    const res = await fetch('/api/agent/triage?alert_id=' + encodeURIComponent(alertId));
    if (!res.ok) return false;
    const d = await res.json();
    if (d && d.root_cause) { S.alertTriage[alertId] = d; return true; }
  } catch (_) { /* triage is best-effort */ }
  return false;
}

// Human-in-the-loop remediation: mutating actions always ask for confirmation.
async function applyTriageRemediation(alertId) {
  const d = S.alertTriage[alertId];
  if (!d || !d.remediation_action) return;
  const isDocker = d.remediation_action === 'docker_restart';
  const ok = await showConfirm({
    title: t('triage_remediate_title', 'Apply AI remediation?'),
    message: (isDocker ? t('triage_remediate_docker', 'Restart this container') : t('triage_remediate_service', 'Restart this service')) + ' — ' + (d.remediation || ''),
    confirmText: t('triage_remediate_btn', 'Restart'),
    danger: true
  });
  if (!ok) return;
  const alert = (S.latestAlerts || []).find(a => a.id === alertId);
  if (!alert || !alert.host_id) return;
  const url = isDocker
    ? '/api/hosts/' + alert.host_id + '/docker/action'
    : '/api/hosts/' + alert.host_id + '/systemd/action';
  const body = isDocker
    ? JSON.stringify({ container: alert.target || '', action: 'restart' })
    : JSON.stringify({ unit: d.unit || alert.target || '', action: 'restart' });
  const res = await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body });
  if (res.ok) {
    showToast(t('triage_remediate_done', 'Remediation applied'), 'success');
    ackAlert(alertId);
  } else {
    showToast(t('triage_remediate_failed', 'Remediation failed'), 'error');
  }
}
async function loadAlerts() {
  const res = await fetch('/api/alerts');
  if (!res.ok) return;
  const alerts = await res.json();
  S.latestAlerts = alerts || [];
  const badge = document.getElementById('activeAlertBadge');
  const banner = document.getElementById('alertBannerArea');
  if (alerts && alerts.length > 0) {
    badge.innerText = alerts.length + ' ' + t('alerts_badge', 'ALERTS');
    badge.style.display = 'inline-block';

    const hostSet = new Set();
    alerts.forEach(a => { if (a.host_name) hostSet.add(a.host_name); });
    const hostCount = hostSet.size;

    banner.innerHTML = `
      <div class="alert-summary-bar">
        <div class="alert-summary-left">
          ${svgIcon('triangle-alert', 'icon-sm')}
          <span>
            ${alerts.length} ${t('active_alerts_title', 'Active Alerts')}
            <span class="alert-host-count">(${hostCount} ${t('hosts', 'Hosts')})</span>
          </span>
        </div>
        <div class="alert-summary-actions">
          <button class="btn btn-sm btn-ghost" id="btnToggleAlertPanel" onclick="toggleAlertOverlay(event)" style="display: inline-flex; align-items: center; gap: 4px;">
            <span>${t('view_details', 'View Details')}</span> ${svgIcon(S.alertOverlayOpen ? 'chevron-up' : 'chevron-down', 'icon-sm')}
          </button>
          <button class="btn btn-sm btn-danger" onclick="ackAllAlerts()">${t('dismiss_all', 'Dismiss All')}</button>
        </div>
      </div>
      <div id="alertOverlayPanel" class="alert-overlay-panel" style="display: ${S.alertOverlayOpen ? 'flex' : 'none'};">
        <div class="alert-overlay-header">
          <span style="font-weight: 600; display: inline-flex; align-items: center; gap: 6px; color: var(--danger);">${svgIcon('triangle-alert', 'icon-sm')} ${t('active_alerts_title', 'Active Alerts')} (${alerts.length})</span>
          <div style="display: flex; gap: 8px; align-items: center;">
            <button class="btn btn-sm btn-danger" onclick="ackAllAlerts()">${t('dismiss_all', 'Dismiss All')}</button>
            <button class="btn btn-sm btn-ghost" onclick="closeAlertOverlay()" style="padding: 2px 6px;" title="${t('close', 'Close')}">${svgIcon('x', 'icon-sm')}</button>
          </div>
        </div>
        <div class="alert-overlay-body">
          ${alerts.map(a => {
            const relTime = formatRelativeTime(a.created_at);
            return `
              <div class="alert-overlay-item">
                <div class="alert-overlay-main">
                  <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 4px; gap: 8px;">
                    <span class="alert-host-tag">[${escapeHtml(a.host_name)}]</span>
                    <span style="font-size: var(--fs-xs); color: var(--text-muted); display: inline-flex; align-items: center; gap: 4px;" title="${relTime.title}">${svgIcon('clock', 'icon-sm')} ${relTime.relative}</span>
                  </div>
                  <div class="alert-msg">${escapeHtml(a.message)}</div>
                  ${a.root_cause ? `
                    <details class="alert-root-cause">
                      <summary>${svgIcon('search', 'icon-sm')} ${t('root_cause_excerpt', 'Root Cause')}</summary>
                      <pre>${escapeHtml(a.root_cause)}</pre>
                    </details>
                  ` : ''}
                  ${(() => {
                    const d = S.alertTriage[a.id];
                    if (!d) return `<details class="alert-ai-diag"><summary>${svgIcon('bot', 'icon-sm')} ${t('alert_ai_diag', 'AI Auto-Diagnosis')}</summary><div class="alert-ai-pending">${t('alert_ai_pending', 'Diagnosis pending…')}</div></details>`;
                    return `<details class="alert-ai-diag">
                      <summary>${svgIcon('bot', 'icon-sm')} ${t('alert_ai_diag', 'AI Auto-Diagnosis')} <span class="alert-ai-source">${escapeHtml(d.source || '')}</span></summary>
                      <div class="alert-ai-body">
                        <div><b>${t('alert_ai_root', 'Root cause')}:</b> ${escapeHtml(d.root_cause || '')}</div>
                        <div><b>${t('alert_ai_impact', 'Impact')}:</b> ${escapeHtml(d.impact || '')}</div>
                        <div><b>${t('alert_ai_fix', 'Remediation')}:</b> ${escapeHtml(d.remediation || '')}</div>
                        ${d.remediation_action ? `<button class="btn btn-sm btn-warning" style="margin-top:6px;" onclick="applyTriageRemediation(${a.id})">${svgIcon('wrench', 'icon-sm')} ${t('alert_ai_apply', 'Apply remediation')}</button>` : ''}
                      </div>
                    </details>`;
                  })()}
                </div>
                <div class="alert-overlay-item-actions">
                  ${a.host_id ? `<button class="btn btn-sm btn-ghost" style="font-size: var(--fs-xs); padding: 3px 8px; color: var(--accent-text);" onclick="openHostDetailFromAlert(${a.host_id})">${t('open_host', 'Open Host')}</button>` : ''}
                  ${window.AG ? AG.btnNum('askAlert', a.id) : ''}
                  <button class="btn btn-sm btn-ghost" style="font-size: var(--fs-xs); padding: 3px 8px;" onclick="ackAlert(${a.id})">${t('dismiss', 'Dismiss')}</button>
                </div>
              </div>
            `;
          }).join('')}
        </div>
      </div>
    `;
  } else {
    badge.style.display = 'none';
    banner.innerHTML = '';
    S.alertOverlayOpen = false;
  }
}

async function ackAlert(id) {
  await fetch('/api/alerts/' + id + '/ack', { method: 'POST' });
  loadAlerts();
}

async function ackAllAlerts() {
  await fetch('/api/alerts/ack-all', { method: 'POST' });
  S.alertOverlayOpen = false;
  loadAlerts();
}

function openHostDetailFromAlert(hostID) {
  closeAlertOverlay();
  openHostDetail(hostID);
}

document.addEventListener('click', function(e) {
  const panel = document.getElementById('alertOverlayPanel');
  const bar = document.querySelector('.alert-summary-bar');
  const badge = document.getElementById('activeAlertBadge');
  if (!panel || !S.alertOverlayOpen) return;
  if (!panel.contains(e.target) && (!bar || !bar.contains(e.target)) && (!badge || !badge.contains(e.target))) {
    closeAlertOverlay();
  }
});

async function populateHostKeySelect(selectedKeyId = 0, isCustom = false) {
  const sel = document.getElementById('hostKeyId');
  if (!sel) return;

  const renderOptions = (keys) => {
    let html = `<option value="0">${t('ssh_key_global', 'Global key (default)')}</option>`;
    (keys || []).forEach(k => {
      html += `<option value="${k.id}">${escapeHtml(k.name)}</option>`;
    });
    html += `<option value="custom">${t('ssh_key_custom_opt', 'Custom key (paste manually)')}</option>`;
    sel.innerHTML = html;

    if (isCustom) {
      sel.value = 'custom';
    } else if (selectedKeyId && (keys || []).some(k => k.id === selectedKeyId)) {
      sel.value = String(selectedKeyId);
    } else if (selectedKeyId) {
      const opt = document.createElement('option');
      opt.value = String(selectedKeyId);
      opt.textContent = `Key #${selectedKeyId}`;
      sel.insertBefore(opt, sel.lastElementChild);
      sel.value = String(selectedKeyId);
    } else {
      sel.value = '0';
    }
    onHostKeySelectChange();
  };

  renderOptions(S.cachedSSHKeys);

  try {
    const res = await fetch('/api/ssh-keys');
    if (res.ok) {
      const keys = await res.json() || [];
      S.cachedSSHKeys = keys;
      renderOptions(keys);
    }
  } catch (err) {
    console.error('Failed to load ssh keys for select:', err);
  }
}

function vpnBadge(state) {
  const cls = state === 'up' ? 'badge-healthy' : (state === 'error' ? 'badge-danger' : (state === 'starting' ? 'badge-warning' : 'badge-degraded'));
  return `<span class="badge ${cls}">${escapeHtml(state || 'down')}</span>`;
}

// A network-path failure is not a dead host: show a distinct, non-red badge
// that names the network instead of implying the server itself is down.
function hostNetworkBadgeHtml(h) {
  const net = (S.vpnNetworks || []).find(n => Number(n.id) === Number(h.network_id));
  if (!net) return '';
  const down = !!(net.last_state && net.last_state !== 'up');
  if (down) {
    const label = t('host_network_down', 'Network down');
    return `<span class="badge badge-warning" style="font-size: var(--fs-xs);" title="${escapeHtml(label + ': ' + (net.name || ''))}">${svgIcon('shield', 'icon-xs')} ${escapeHtml(label)}: ${escapeHtml(net.name)}</span>`;
  }
  return `<span class="badge" style="font-size: var(--fs-xs);" title="${escapeHtml(t('host_network', 'Network (VPN / Tunnel)'))}">${svgIcon('shield', 'icon-xs')} ${escapeHtml(net.name)}</span>`;
}

async function loadVpnNetworks() {
  const box = document.getElementById('vpnNetworkList');
  if (!box) return;
  const gate = await fetch('/api/vpn/status').then(r => r.ok ? r.json() : null).catch(() => null);
  S.vpnGatewayEnabled = !!(gate && gate.enabled);
  const toggle = document.getElementById('vpnGateToggle');
  const state = document.getElementById('vpnGateState');
  if (toggle) toggle.checked = S.vpnGatewayEnabled;
  if (state) {
    state.textContent = S.vpnGatewayEnabled
      ? t('vpn_gate_on_hint', 'Enabled: vendors run with master privileges on this server.')
      : t('vpn_gate_off_hint', 'Disabled: hosts on a network cannot be reached.');
  }
  const drvBox = document.getElementById('vpnDriverStatus');
  if (drvBox && gate) {
    drvBox.innerHTML = (gate.drivers || []).map(d => {
      const ok = !d.error;
      return `<span class="badge ${ok ? 'badge-healthy' : 'badge-degraded'}" title="${escapeHtml(d.error || d.binary || '')}">${escapeHtml(d.driver)}${ok ? ': ' + escapeHtml((d.version || '').slice(0, 28)) : ': ' + escapeHtml(t('vpn_driver_missing', 'not installed'))}</span>`;
    }).join('');
  }
  const res = await fetch('/api/vpn/networks');
  S.vpnNetworks = res.ok ? (await res.json() || []) : [];
  S.vpnIndexAt = Date.now();
  if (!S.vpnNetworks.length) {
    box.innerHTML = `<div class="empty-state"><div class="empty-state-desc">${escapeHtml(t('vpn_no_networks', 'No networks yet. Add one per VPN or tunnel your private hosts live in.'))}</div></div>`;
    return;
  }
  const breakerLabels = {
    open: t('vpn_breaker_open', 'Circuit open'),
    closed: t('vpn_breaker_closed', 'Closed'),
    'half-open': t('vpn_breaker_half_open', 'Half-open'),
  };
  box.innerHTML = S.vpnNetworks.map(n => {
    const latency = (n.probe_ms === -1 || n.probe_ms === undefined || n.probe_ms === null)
      ? '—'
      : `${n.probe_ms} ms`;
    const breaker = breakerLabels[n.breaker] || escapeHtml(n.breaker || '');
    return `
    <div class="card" style="flex-direction:row;align-items:center;justify-content:space-between;gap:12px;${n.enabled ? '' : 'opacity:.6;'}">
      <div style="min-width:0;">
        <b>${escapeHtml(n.name)}</b>
        <span class="badge mono">${escapeHtml(n.driver)}${n.driver === 'netbird' && n.config && n.config.mode === 'instance' ? ':instance' : ''}</span>
        ${vpnBadge(n.last_state)}
        ${n.degraded ? `<span class="badge badge-degraded">${escapeHtml(t('vpn_degraded', 'Degraded'))}</span>` : ''}
        <span class="badge">${n.hosts_count} host${n.hosts_count === 1 ? '' : 's'}</span>
        ${n.iface ? `<span class="badge mono">${escapeHtml(n.iface)}</span>` : ''}
        <span class="badge mono" title="${escapeHtml(t('vpn_virtual_ip', 'Virtual IP'))}">${escapeHtml(t('vpn_virtual_ip', 'Virtual IP'))}: ${n.virtual_ip ? escapeHtml(n.virtual_ip) : '—'}</span>
        <span class="badge mono" title="${escapeHtml(t('vpn_latency', 'Latency'))}">${escapeHtml(t('vpn_latency', 'Latency'))}: ${latency}</span>
        <span class="badge" title="${escapeHtml(t('vpn_breaker_closed', 'Breaker'))}">${svgIcon('shield', 'icon-xs')} ${breaker}</span>
        <div style="font-size:var(--fs-xs);color:var(--text-muted);">${escapeHtml(n.last_error || '')}</div>
      </div>
      <div style="display:flex;gap:6px;flex-shrink:0;">
        <button type="button" class="btn btn-sm" onclick="vpnNetworkProbe(${n.id})">${escapeHtml(t('vpn_probe', 'Probe'))}</button>
        <button type="button" class="btn btn-sm" onclick="vpnNetworkUp(${n.id})">${escapeHtml(t('vpn_reconnect', 'Reconnect'))}</button>
        <button type="button" class="btn btn-sm" onclick="vpnNetworkDown(${n.id})">${escapeHtml(t('vpn_down', 'Down'))}</button>
        <button type="button" class="btn btn-sm" onclick="openVpnNetworkLog(${n.id})">${escapeHtml(t('vpn_log', 'Log'))}</button>
        <button type="button" class="btn btn-sm" onclick="openVpnNetworkModal(${n.id})">${svgIcon('pencil', 'icon-xs')} ${escapeHtml(t('edit', 'Edit'))}</button>
        <button type="button" class="btn btn-icon btn-sm" onclick="deleteVpnNetwork(${n.id})">${svgIcon('trash-2', 'icon-xs')}</button>
      </div>
    </div>`;
  }).join('');
}

async function saveVpnGate(on) {
  const res = await fetch('/api/vpn/gate', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ enabled: on }) });
  if (!res.ok) { showToast(t('vpn_gate_failed', 'Could not change the VPN gate'), 'error'); }
  loadVpnNetworks();
}

function vpnDriverField(field, cfg) {
  const val = cfg[field.key] || '';
  const set = field.secret && cfg[field.key + '_set'];
  const label = `<label class="form-label">${escapeHtml(field.label)}${set ? ' <span class="badge">' + escapeHtml(t('vpn_saved_value', 'stored')) + '</span>' : ''}</label>`;
  if (field.select) {
    return `<div class="form-group">${label}<select id="vpnCfg_${field.key}">${field.select.map(([v, l]) => `<option value="${escapeHtml(v)}"${val === v ? ' selected' : ''}>${escapeHtml(l)}</option>`).join('')}</select></div>`;
  }
  if (field.textarea) {
    return `<div class="form-group">${label}<textarea id="vpnCfg_${field.key}" rows="6" spellcheck="false" style="font-family:var(--font-mono);font-size:var(--fs-xs);" placeholder="${set ? escapeHtml(t('vpn_keep_secret', 'stored - leave blank to keep')) : ''}">${escapeHtml(val)}</textarea></div>`;
  }
  const type = field.secret ? 'password' : 'text';
  return `<div class="form-group">${label}<input type="${type}" id="vpnCfg_${field.key}" value="${escapeHtml(val)}" placeholder="${escapeHtml(field.placeholder || (set ? t('vpn_keep_secret', 'stored - leave blank to keep') : ''))}" /></div>`;
}

function openVpnNetworkModal(id) {
  const n = id ? S.vpnNetworks.find(x => x.id === id) : null;
  const drivers = Object.keys(S.VPN_DRIVER_META);
  const opts = drivers.map(d => `<option value="${d}"${n && n.driver === d ? ' selected' : (!n && d === 'netbird' ? ' selected' : '')}>${d}</option>`).join('');
  const body = `
    <div class="form-group"><label class="form-label">${escapeHtml(t('vpn_name', 'Name'))} *</label><input type="text" id="vpnName" value="${escapeHtml(n ? n.name : '')}" /></div>
    <div class="form-group"><label class="form-label">${escapeHtml(t('vpn_driver', 'Driver'))}</label><select id="vpnDriver" onchange="renderVpnDriverFields()">${opts}</select><span class="form-hint" id="vpnDriverHint"></span></div>
    <div class="form-group"><label class="form-label">${escapeHtml(t('vpn_iface', 'Interface (empty = driver default)'))}</label><input type="text" id="vpnIface" value="${escapeHtml(n ? n.iface : '')}" list="vpnIfaceList" /><datalist id="vpnIfaceList"></datalist></div>
    <div id="vpnDriverFields"></div>
    <div style="display:flex;gap:16px;flex-wrap:wrap;">
      <label style="display:flex;gap:6px;align-items:center;"><input type="checkbox" id="vpnEnabled" ${!n || n.enabled ? 'checked' : ''} /> ${escapeHtml(t('vpn_enabled', 'Enabled'))}</label>
      <label style="display:flex;gap:6px;align-items:center;"><input type="checkbox" id="vpnAutoUp" ${!n || n.auto_up ? 'checked' : ''} /> ${escapeHtml(t('vpn_auto_up', 'Bring up automatically when a host needs it'))}</label>
    </div>
    <div id="vpnModalError" class="form-hint" style="color:var(--danger);"></div>`;
  S.vpnDriverFields = n && n.config ? n.config : {};
  tfDlg(2, id ? t('vpn_edit_title', 'Edit Network') : t('vpn_new_title', 'New Network'), body, tfFoot(2, tfPrimary(t('save', 'Save'), `saveVpnNetwork(${id || 0})`)), 620);
  renderVpnDriverFields();
  loadVpnInterfaces();
}

async function loadVpnInterfaces() {
  const dl = document.getElementById('vpnIfaceList');
  if (!dl) return;
  try {
    const res = await fetch('/api/vpn/interfaces');
    if (!res.ok) return;
    const list = await res.json();
    dl.innerHTML = (list || []).map(i => `<option value="${escapeHtml(i.name)}">${escapeHtml((i.addrs || []).join(', '))}</option>`).join('');
  } catch (e) { /* the dropdown stays empty; the field still accepts a name */ }
}

function renderVpnDriverFields() {
  const driver = document.getElementById('vpnDriver')?.value || 'netbird';
  const meta = S.VPN_DRIVER_META[driver] || { fields: [] };
  const box = document.getElementById('vpnDriverFields');
  const hint = document.getElementById('vpnDriverHint');
  if (hint) hint.textContent = t('vpn_driver_hint_' + driver, meta.hint || '');
  if (!box) return;
  const values = {};
  (meta.fields || []).forEach(f => {
    const el = document.getElementById('vpnCfg_' + f.key);
    if (el) values[f.key] = el.value;
  });
  box.innerHTML = (meta.fields || []).map(f => vpnDriverField(f, Object.assign({}, S.vpnDriverFields, values))).join('');
}

function collectVpnConfig() {
  const driver = document.getElementById('vpnDriver')?.value || 'netbird';
  const meta = S.VPN_DRIVER_META[driver] || { fields: [] };
  const cfg = {};
  (meta.fields || []).forEach(f => {
    const el = document.getElementById('vpnCfg_' + f.key);
    if (!el) return;
    let v = el.value;
    if (f.textarea) v = v; else v = v.trim();
    if (f.secret && !v) return; // blank keeps the stored value
    if (v !== '') cfg[f.key] = v;
  });
  return cfg;
}

async function saveVpnNetwork(id) {
  const errEl = document.getElementById('vpnModalError');
  const name = (document.getElementById('vpnName')?.value || '').trim();
  if (!name) { if (errEl) errEl.textContent = t('vpn_name_required', 'Name is required'); return; }
  const payload = {
    name,
    driver: document.getElementById('vpnDriver').value,
    iface: (document.getElementById('vpnIface')?.value || '').trim(),
    enabled: document.getElementById('vpnEnabled').checked,
    auto_up: document.getElementById('vpnAutoUp').checked,
    config: collectVpnConfig(),
  };
  const res = await fetch(id ? '/api/vpn/networks/' + id : '/api/vpn/networks', {
    method: id ? 'PUT' : 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    if (errEl) errEl.textContent = data.error || ('HTTP ' + res.status);
    return;
  }
  showToast(t('vpn_saved', 'Network saved'), 'success');
  tfDlgClose(2);
  loadVpnNetworks();
  loadHosts();
}

async function deleteVpnNetwork(id) {
  const n = S.vpnNetworks.find(x => x.id === id);
  const ok = await showConfirm({ title: t('delete', 'Delete'), message: t('vpn_delete_confirm', 'Delete network') + ' ' + (n ? n.name : '#' + id) + '?', confirmText: t('delete', 'Delete'), danger: true });
  if (!ok) return;
  let res = await fetch('/api/vpn/networks/' + id, { method: 'DELETE' });
  if (res.status === 409) {
    const data = await res.json().catch(() => ({}));
    const force = await showConfirm({ title: t('vpn_delete_used_title', 'Network in use'), message: importFill(t('vpn_delete_used', '{n} host(s) use this network and will fall back to direct SSH.'), { n: data.hosts || 0 }), confirmText: t('vpn_detach', 'Detach and delete'), danger: true });
    if (!force) return;
    res = await fetch('/api/vpn/networks/' + id + '?force=1', { method: 'DELETE' });
  }
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    showToast(t('vpn_delete_failed', 'Delete failed: ') + (data.error || res.status), 'error');
    return;
  }
  showToast(t('vpn_deleted', 'Network deleted'), 'success');
  loadVpnNetworks();
  loadHosts();
}

async function vpnNetworkUp(id) {
  const res = await fetch('/api/vpn/networks/' + id + '/up', { method: 'POST' });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) { showToast(t('vpn_up_failed', 'Bring-up failed: ') + (data.error || res.status), 'error'); }
  else { showToast(t('vpn_up_ok', 'Tunnel up'), 'success'); }
  loadVpnNetworks();
}

async function vpnNetworkProbe(id) {
  const res = await fetch('/api/vpn/networks/' + id + '/probe', { method: 'POST' });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    showToast(t('vpn_probe_failed', 'Probe failed: ') + (data.error || res.status), 'error');
  } else {
    const latency = (data.probe_ms === -1 || data.probe_ms === undefined || data.probe_ms === null) ? '—' : data.probe_ms + ' ms';
    showToast(t('vpn_probe_ok', 'Probe complete') + ' · ' + latency, 'success');
  }
  loadVpnNetworks();
}

async function vpnNetworkDown(id) {
  const res = await fetch('/api/vpn/networks/' + id + '/down', { method: 'POST' });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) showToast(t('vpn_down_failed', 'Stop failed: ') + (data.error || res.status), 'error');
  loadVpnNetworks();
}

async function openVpnNetworkLog(id) {
  const res = await fetch('/api/vpn/networks/' + id + '/log');
  const data = res.ok ? await res.json() : { lines: [] };
  const n = S.vpnNetworks.find(x => x.id === id) || {};
  tfDlg(1, t('vpn_log_title', 'Tunnel log') + ': ' + (n.name || '#' + id), `<pre class="mono" style="font-size:var(--fs-xs);white-space:pre-wrap;max-height:60vh;overflow:auto;margin:0;">${escapeHtml((data.lines || []).join('\n') || t('vpn_log_empty', 'No output yet.'))}</pre>`, tfFoot(1), 720);
}

function onHostKeySelectChange() {
  const sel = document.getElementById('hostKeyId');
  const group = document.getElementById('hostCustomKeyGroup');
  if (!sel || !group) return;
  group.style.display = sel.value === 'custom' ? 'block' : 'none';
}

function openAddHostModal() {
  document.getElementById('hostModalTitle').innerText = t('add_host_title');
  document.getElementById('editHostId').value = '';
  document.getElementById('hostName').value = '';
  document.getElementById('hostAddr').value = '';
  document.getElementById('hostPort').value = 22;
  document.getElementById('hostUser').value = 'root';
  document.getElementById('hostPassword').value = '';
  document.getElementById('hostCustomKey').value = '';
  document.getElementById('hostCustomKey').placeholder = '-----BEGIN OPENSSH PRIVATE KEY-----';
  document.getElementById('hostCommissionDate').value = '';
  document.getElementById('hostNote').value = '';
  document.getElementById('hostGroup').value = '';
  const kmAdd = document.getElementById('hostKubeMode');
  if (kmAdd) kmAdd.value = 'full';
  populateHostNetworkSelect(0);
  const testBtn = document.getElementById('btnTestHostInModal');
  if (testBtn) testBtn.style.display = 'none';
  updateGroupSuggestions();
  document.getElementById('passGroup').style.display = 'flex';
  populateHostKeySelect(0, false);
  openModal('hostModal');
}

function openEditHostModal(hostId) {
  const h = S.allRawHosts.find(item => item.id === hostId);
  if (!h) return;
  document.getElementById('hostModalTitle').innerText = t('edit_host_title', 'Edit Host');
  document.getElementById('editHostId').value = h.id;
  document.getElementById('hostName').value = h.name || '';
  document.getElementById('hostAddr').value = h.host || '';
  document.getElementById('hostPort').value = h.port || 22;
  document.getElementById('hostUser').value = h.user || 'root';
  document.getElementById('hostGroup').value = h.group_name || '';
  document.getElementById('hostPassword').value = '';
  const ckEl = document.getElementById('hostCustomKey');
  ckEl.value = '';
  ckEl.placeholder = h.has_custom_key ? '(stored key hidden - leave blank to keep it)' : '-----BEGIN OPENSSH PRIVATE KEY-----';
  document.getElementById('hostCommissionDate').value = h.commission_date || '';
  document.getElementById('hostNote').value = h.notes || '';
  const kmEdit = document.getElementById('hostKubeMode');
  if (kmEdit) kmEdit.value = h.kube_mode || 'full';
  populateHostNetworkSelect(h.network_id || 0);
  document.getElementById('passGroup').style.display = 'none';
  const testBtn = document.getElementById('btnTestHostInModal');
  if (testBtn) testBtn.style.display = 'inline-flex';
  updateGroupSuggestions();
  const hasCustom = !!h.has_custom_key;
  populateHostKeySelect(h.key_id || 0, hasCustom);
  openModal('hostModal');
}

function updateGroupSuggestions() {
  const datalist = document.getElementById('groupSuggestions');
  const groupSelect = document.getElementById('groupFilterSelect');
  const groups = Array.from(new Set(S.allRawHosts.map(h => (h.group_name || '').trim()).filter(Boolean))).sort((a, b) => a.localeCompare(b));
  const hasUngrouped = S.allRawHosts.some(h => !(h.group_name || '').trim());
  if (datalist) {
    datalist.innerHTML = groups.map(g => `<option value="${escapeHtml(g)}"></option>`).join('');
  }
  if (groupSelect) {
    const currentVal = groupSelect.value || 'all';
    let options = `<option value="all" data-i18n="filter_all_groups">${t('filter_all_groups', 'All Groups')}</option>`;
    groups.forEach(g => {
      options += `<option value="${escapeHtml(g)}">${escapeHtml(g)}</option>`;
    });
    if (hasUngrouped && groups.length > 0) {
      options += `<option value="__ungrouped__" data-i18n="filter_ungrouped">${t('filter_ungrouped', 'Ungrouped')}</option>`;
    }
    groupSelect.innerHTML = options;
    if (groups.includes(currentVal) || (currentVal === '__ungrouped__' && hasUngrouped)) {
      groupSelect.value = currentVal;
    } else {
      groupSelect.value = 'all';
    }
  }
}

async function saveHost() {
  const id = document.getElementById('editHostId').value;
  const selectedKey = document.getElementById('hostKeyId')?.value || '0';
  let keyId = 0;
  let customKey = '';
  if (selectedKey === 'custom') {
    customKey = document.getElementById('hostCustomKey')?.value || '';
  } else {
    keyId = parseInt(selectedKey, 10) || 0;
  }
  const payload = {
    name: document.getElementById('hostName').value,
    host: document.getElementById('hostAddr').value,
    port: parseInt(document.getElementById('hostPort').value) || 22,
    user: document.getElementById('hostUser').value,
    password: document.getElementById('hostPassword').value,
    key_id: keyId,
    custom_key: customKey,
    commission_date: (document.getElementById('hostCommissionDate')?.value || '').trim(),
    notes: document.getElementById('hostNote').value,
    group_name: (document.getElementById('hostGroup')?.value || '').trim(),
    kube_mode: document.getElementById('hostKubeMode')?.value || 'full',
    network_id: parseInt(document.getElementById('hostNetwork')?.value, 10) || 0,
  };
  if (id && selectedKey === 'custom' && !customKey.trim()) {
    // the API never returns the stored key; blank + keep flag leaves it unchanged
    const cur = S.allRawHosts.find(x => String(x.id) === String(id));
    if (cur && cur.has_custom_key) payload.keep_custom_key = true;
  }
  const method = id ? 'PUT' : 'POST';
  const url = id ? '/api/hosts/' + id : '/api/hosts';
  const res = await fetch(url, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  });
  if (res.ok) {
    closeModal('hostModal');
    showToast(t('save_host_success', 'Host saved successfully'), 'success');
    loadHosts();
    if (id && S.currentDetailHostId === parseInt(id)) {
      openHostDetail(id);
    }
  } else {
    const txt = await res.text();
    let errMsg = txt;
    try {
      const errObj = JSON.parse(txt);
      if (errObj.error) errMsg = errObj.error;
    } catch(e) {}
    showToast(t('save_host_failed', 'Failed to save host: ') + errMsg, 'error');
  }
}

function openCommissionDateModal(hostId, currentCommDate, biosDate) {
  document.getElementById('quickCommHostId').value = hostId;
  document.getElementById('quickCommBiosDate').value = biosDate || '';
  document.getElementById('quickCommissionDate').value = currentCommDate || '';
  const biosInfoEl = document.getElementById('quickCommBiosInfo');
  if (biosDate) {
    biosInfoEl.style.display = 'block';
    biosInfoEl.innerHTML = `<span style="display: inline-flex; align-items: center; gap: 4px;">${svgIcon('cpu', 'icon-sm')} <strong>Motherboard BIOS:</strong> ${escapeHtml(biosDate)}</span>`;
  } else {
    biosInfoEl.style.display = 'none';
  }
  openModal('commissionDateModal');
}

function importFill(tpl, vars) {
  return String(tpl).replace(/\{(\w+)\}/g, (m, k) => (k in vars ? vars[k] : m));
}

function importReasonText(reason, sameAs) {
  if (reason === 'exists') return importFill(t('import_dup_exists', 'Already in Pantau as "{name}"'), { name: sameAs || '' });
  if (reason === 'duplicate_in_file') return importFill(t('import_dup_file', 'Same server as "{name}" above'), { name: sameAs || '' });
  return t('import_invalid', 'Invalid entry');
}

function openImportHostsModal() {
  S.importEntries = [];
  S.importedHosts = [];
  document.getElementById('importConfigText').value = '';
  document.getElementById('importConfigFile').value = '';
  document.getElementById('importWarnings').style.display = 'none';
  document.getElementById('importPreview').style.display = 'none';
  document.getElementById('importStepSelect').style.display = 'flex';
  document.getElementById('importStepGroups').style.display = 'none';
  openModal('importHostsModal');
}

function importLoadFile(input) {
  const file = input.files && input.files[0];
  if (!file) return;
  if (file.size > 1024 * 1024) {
    showToast(t('import_failed', 'Import failed: ') + 'file too large (max 1 MiB)', 'error');
    input.value = '';
    return;
  }
  const reader = new FileReader();
  reader.onload = () => {
    document.getElementById('importConfigText').value = String(reader.result || '');
    importParse();
  };
  reader.readAsText(file);
}

async function importRequestPreview(payload) {
  let res;
  try {
    res = await fetch('/api/hosts/import/preview', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
  } catch (e) {
    showToast(t('import_failed', 'Import failed: ') + e.message, 'error');
    return;
  }
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    showToast(t('import_failed', 'Import failed: ') + (data.error || res.status), 'error');
    return;
  }
  renderImportPreview(data);
}

function importParse() {
  const content = document.getElementById('importConfigText').value;
  if (!content.trim()) {
    showToast(t('import_empty', 'Paste or load an SSH config first'), 'warning');
    return;
  }
  importRequestPreview({ content });
}

function importReadServer() {
  importRequestPreview({ source: 'server' });
}

async function populateImportKeySelect() {
  const sel = document.getElementById('importKeyId');
  if (!sel) return;
  const render = (keys) => {
    const cur = sel.value || '0';
    sel.innerHTML = `<option value="0">${escapeHtml(t('ssh_key_global', 'Global key (default)'))}</option>` +
      (keys || []).map(k => `<option value="${Number(k.id)}">${escapeHtml(k.name)}</option>`).join('');
    sel.value = Array.from(sel.options).some(o => o.value === cur) ? cur : '0';
  };
  render(S.cachedSSHKeys);
  try {
    const res = await fetch('/api/ssh-keys');
    if (res.ok) { S.cachedSSHKeys = await res.json() || []; render(S.cachedSSHKeys); }
  } catch (err) {
    console.error('Failed to load ssh keys for import:', err);
  }
}

function importBulkNetworkChanged(value) {
  const bulk = parseInt(value, 10) || 0;
  document.querySelectorAll('#importPreviewBody .import-net').forEach(sel => { sel.value = String(bulk); });
  S.importRowNetworks = {};
}

async function populateImportNetworkSelect() {
  const sel = document.getElementById('importNetworkId');
  if (!sel) return;
  await loadVpnNetworkIndex(false);
  const cur = sel.value || '0';
  sel.innerHTML = `<option value="0">${escapeHtml(t('host_network_direct', 'Direct (no VPN)'))}</option>` +
    (S.vpnNetworks || []).map(n => `<option value="${Number(n.id)}">${escapeHtml(n.name)}</option>`).join('');
  sel.value = Array.from(sel.options).some(o => o.value === cur) ? cur : '0';
}

async function importInspectCreated(hosts) {
  const queue = hosts.map(h => h.id);
  const worker = async () => {
    while (queue.length) {
      const id = queue.shift();
      try { await fetch('/api/hosts/' + id + '/inspect', { method: 'POST' }); } catch (e) { /* the inspection run records the failure */ }
    }
  };
  await Promise.all([worker(), worker(), worker()]);
  loadHosts();
}

function renderImportPreview(data) {
  S.importRowNetworks = {};
  populateImportKeySelect();
  populateImportNetworkSelect();
  S.importEntries = data.entries || [];
  const warns = data.warnings || [];
  const warnEl = document.getElementById('importWarnings');
  warnEl.innerHTML = warns.map(w => `<div style="display: flex; align-items: center; gap: 6px;">${svgIcon('triangle-alert', 'icon-sm')} <span>${escapeHtml(w)}</span></div>`).join('');
  warnEl.style.display = warns.length ? 'flex' : 'none';
  warnEl.style.flexDirection = 'column';
  warnEl.style.gap = '4px';

  const preview = document.getElementById('importPreview');
  if (!S.importEntries.length) {
    preview.style.display = 'none';
    showToast(t('import_none_found', 'No importable hosts found'), 'warning');
    return;
  }
  const netOptions = `<option value="0">${escapeHtml(t('host_network_direct', 'Direct (no VPN)'))}</option>` +
    (S.vpnNetworks || []).map(n => `<option value="${Number(n.id)}">${escapeHtml(n.name)}</option>`).join('');
  document.getElementById('importPreviewBody').innerHTML = S.importEntries.map((e, i) => {
    let note = '';
    if (e.duplicate) {
      note = `<span class="badge badge-warning">${escapeHtml(importReasonText(e.reason, e.same_as))}</span>`;
    } else if (e.identity_file) {
      note = `<span class="badge badge-info" title="${escapeHtml(e.identity_file)}">${svgIcon('key-round', 'icon-sm')} ${escapeHtml(t('import_identity_note', 'Custom key in ssh config'))}</span>`;
    } else {
      note = `<span class="badge badge-success">${svgIcon('check', 'icon-sm')} Ready</span>`;
    }
    return `<tr${e.duplicate ? ' style="opacity: 0.55;"' : ''}>
      <td><input type="checkbox" class="import-cb" data-idx="${i}" ${e.duplicate ? 'disabled' : 'checked'} onchange="updateImportCount()" /></td>
      <td><strong>${escapeHtml(e.alias)}</strong></td>
      <td><code class="mono" style="font-size: var(--fs-xs);">${escapeHtml(e.host)}</code></td>
      <td><span class="num">${escapeHtml(String(e.port))}</span></td>
      <td>${escapeHtml(e.user || 'root')}</td>
      <td><select class="import-net" data-idx="${i}" onchange="importNetworkPicked(this)">${netOptions}</select></td>
      <td>${note}</td>
    </tr>`;
  }).join('');
  preview.style.display = 'flex';
  updateImportCount();
}

function importNetworkPicked(sel) {
  S.importRowNetworks[sel.dataset.idx] = parseInt(sel.value, 10) || 0;
}

function importableBoxes() {
  return Array.from(document.querySelectorAll('#importPreviewBody .import-cb')).filter(b => !b.disabled);
}

function updateImportCount() {
  const boxes = importableBoxes();
  const sel = boxes.filter(b => b.checked).length;
  document.getElementById('importSelectedCount').textContent = importFill(t('import_selected_count', '{n} of {m} selected'), { n: sel, m: boxes.length });
  const all = document.getElementById('importSelectAll');
  all.checked = boxes.length > 0 && sel === boxes.length;
  all.indeterminate = sel > 0 && sel < boxes.length;
  document.getElementById('importSubmitBtn').disabled = sel === 0;
}

function importToggleAll(cb) {
  importableBoxes().forEach(b => { b.checked = cb.checked; });
  updateImportCount();
}

async function importSubmit() {
  const hosts = importableBoxes().filter(b => b.checked).map(b => {
    const e = S.importEntries[parseInt(b.dataset.idx, 10)];
    const row = document.querySelector(`#importPreviewBody .import-net[data-idx="${b.dataset.idx}"]`);
    return { name: e.alias, host: e.host, port: e.port, user: e.user, network_id: row ? (parseInt(row.value, 10) || 0) : 0 };
  });
  if (!hosts.length) {
    showToast(t('import_nothing_selected', 'Select at least one host'), 'warning');
    return;
  }
  const btn = document.getElementById('importSubmitBtn');
  btn.disabled = true;
  try {
    const res = await fetch('/api/hosts/import', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        hosts,
        key_id: parseInt(document.getElementById('importKeyId').value, 10) || 0,
        network_id: parseInt(document.getElementById('importNetworkId')?.value, 10) || 0,
      })
    });
    const data = await res.json().catch(() => ({}));
    S.importedHosts = data.created || [];
    if (S.importedHosts.length) await loadHosts();
    if (S.importedHosts.length) importInspectCreated(S.importedHosts);
    if (!res.ok) {
      showToast(t('import_failed', 'Import failed: ') + (data.error || res.status), 'error');
      // Hosts created before the failure still need their groups.
      if (S.importedHosts.length) showImportGroupsStep();
      return;
    }
    if (!S.importedHosts.length) {
      showToast(t('import_none_created', 'No hosts were imported'), 'warning');
      return;
    }
    const skipped = (data.skipped || []).length;
    let msg = importFill(t('import_done', '{n} host(s) imported'), { n: S.importedHosts.length });
    if (skipped) msg += ', ' + importFill(t('import_skipped', '{n} skipped'), { n: skipped });
    showToast(msg, 'success');
    showImportGroupsStep();
  } catch (e) {
    showToast(t('import_failed', 'Import failed: ') + e.message, 'error');
  } finally {
    btn.disabled = false;
  }
}

function showImportGroupsStep() {
  document.getElementById('importStepSelect').style.display = 'none';
  document.getElementById('importStepGroups').style.display = 'flex';
  document.getElementById('importGroupsBody').innerHTML = S.importedHosts.map(h => `<tr>
    <td>
      <strong>${escapeHtml(h.name)}</strong>
      <div style="font-size: 0.75rem; color: var(--text-muted); font-family: var(--font-mono);">${escapeHtml(h.user)}@${escapeHtml(h.host)}:${escapeHtml(String(h.port))}</div>
    </td>
    <td style="width: 50%;"><input type="text" class="import-group-input" data-id="${h.id}" list="importGroupSuggestions" placeholder="${escapeHtml(t('host_group_ph', 'e.g. Primary Server, Testing, DB Cluster'))}" onchange="refreshImportGroupSuggestions()" /></td>
  </tr>`).join('');
  refreshImportGroupSuggestions();
}

function refreshImportGroupSuggestions() {
  const groups = new Set(S.allRawHosts.map(h => (h.group_name || '').trim()).filter(Boolean));
  document.querySelectorAll('.import-group-input').forEach(i => {
    const v = i.value.trim();
    if (v) groups.add(v);
  });
  document.getElementById('importGroupSuggestions').innerHTML = Array.from(groups)
    .sort((a, b) => a.localeCompare(b))
    .map(g => `<option value="${escapeHtml(g)}"></option>`).join('');
}

async function importSaveGroups() {
  const assignments = Array.from(document.querySelectorAll('.import-group-input'))
    .map(i => ({ id: parseInt(i.dataset.id, 10), group_name: i.value.trim() }))
    .filter(a => a.group_name);
  if (!assignments.length) {
    closeModal('importHostsModal');
    return;
  }
  try {
    const res = await fetch('/api/hosts/groups', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ assignments })
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
      showToast(t('save_host_failed', 'Failed to save host: ') + (data.error || res.status), 'error');
      return;
    }
    showToast(t('import_groups_saved', 'Groups saved'), 'success');
    closeModal('importHostsModal');
    loadHosts();
  } catch (e) {
    showToast(t('save_host_failed', 'Failed to save host: ') + e.message, 'error');
  }
}

async function submitQuickCommissionDate() {
  const hostId = document.getElementById('quickCommHostId').value;
  const dateVal = document.getElementById('quickCommissionDate').value.trim();
  const res = await fetch(`/api/hosts/${hostId}/commission-date`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ commission_date: dateVal })
  });
  if (res.ok) {
    closeModal('commissionDateModal');
    showToast(t('save_host_success', 'Saved successfully'), 'success');
    loadHosts();
    if (S.currentDetailHostId === parseInt(hostId)) {
      openHostDetail(hostId);
    }
  } else {
    const txt = await res.text();
    let errMsg = txt;
    try {
      const errObj = JSON.parse(txt);
      if (errObj.error) errMsg = errObj.error;
    } catch(e) {}
    showToast(errMsg || 'Failed to update commission date', 'error');
  }
}

async function clearCommissionDate() {
  document.getElementById('quickCommissionDate').value = '';
  await submitQuickCommissionDate();
}

function showToast(message, type = 'info', duration = 4000) {
  const validTypes = ['success', 'error', 'warning', 'info'];
  if (validTypes.includes(message) && typeof type === 'string' && !validTypes.includes(type)) {
    const tmp = message;
    message = type;
    type = tmp;
  }
  const container = document.getElementById('toastContainer');
  if (!container) return;
  const toast = document.createElement('div');
  const safeType = validTypes.includes(type) ? type : 'info';
  toast.className = 'toast toast-' + safeType;
  const icon = safeType === 'success' ? svgIcon('circle-check', 'text-success') :
               safeType === 'error' ? svgIcon('circle-x', 'text-danger') :
               safeType === 'warning' ? svgIcon('triangle-alert', 'text-warning') :
               svgIcon('info', 'text-info');
  toast.innerHTML = `<span class="toast-icon-wrap">${icon}</span><span class="toast-msg">${escapeHtml(message)}</span><button type="button" class="toast-close-btn" onclick="this.closest('.toast').remove()" aria-label="Dismiss">${svgIcon('x', 'icon-sm')}</button>`;
  container.appendChild(toast);
  setTimeout(() => {
    toast.style.opacity = '0';
    setTimeout(() => toast.remove(), 300);
  }, duration);
}

function showBusyToast(text, t0) {
  const container = document.getElementById('toastContainer');
  if (!container) return { close() {} };
  const toast = document.createElement('div');
  toast.className = 'toast toast-info';
  toast.setAttribute('role', 'status');
  toast.innerHTML = `<span class="toast-icon-wrap">${svgIcon('loader-circle', 'icon-sm')}</span><span class="toast-msg">${SP.html(text, t0)}</span>`;
  container.appendChild(toast);
  return {
    close() {
      if (!toast.isConnected) return;
      toast.style.opacity = '0';
      setTimeout(() => toast.remove(), 300);
    }
  };
}

function toggleDropdown(e, id) {
  if (e) e.stopPropagation();
  const menu = document.getElementById('dropdown-' + id);
  const isOpen = menu && menu.parentElement.classList.contains('open');
  closeAllDropdowns();
  if (menu && !isOpen) {
    menu.parentElement.classList.add('open');
    // The list view clips/scrolls its content, so its menu is pinned to the viewport next to the button.
    if (menu.closest('.host-list-table')) {
      const b = menu.parentElement.getBoundingClientRect();
      const h = menu.offsetHeight;
      Object.assign(menu.style, {
        position: 'fixed', margin: '0', bottom: 'auto', left: 'auto',
        right: (innerWidth - b.right) + 'px',
        top: (b.bottom + 6 + h > innerHeight ? Math.max(4, b.top - 6 - h) : b.bottom + 6) + 'px'
      });
    }
  }
}

function closeAllDropdowns() {
  document.querySelectorAll('.dropdown.open').forEach(el => el.classList.remove('open'));
}

window.addEventListener('click', closeAllDropdowns);

window.addEventListener('scroll', () => { if (document.querySelector('.host-list-table .dropdown.open')) closeAllDropdowns(); }, true);

function openInjectKeyModal(id, name) {
  closeAllDropdowns();
  document.getElementById('injectKeyHostId').value = id;
  document.getElementById('injectKeyModalTitle').innerText = t('inject_key_title', 'Key Provisioning') + ': ' + name;
  const input = document.getElementById('injectKeyPasswordInput');
  input.value = '';
  openModal('injectKeyModal');
  setTimeout(() => input.focus(), 50);
}

async function submitInjectKey() {
  const id = document.getElementById('injectKeyHostId').value;
  const pass = document.getElementById('injectKeyPasswordInput').value;
  if (!pass) return;
  const btn = document.getElementById('btnSubmitInjectKey');
  const restore = SP.busy(btn, t('upd_injecting'));
  showToast(t('injecting_key', 'Injecting Pantau public key...'), 'info', 2500);
  try {
    const res = await fetch('/api/hosts/' + id + '/inject-key', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ password: pass })
    });
    const data = await res.json();
    if (data.ok) {
      closeModal('injectKeyModal');
      showToast(t('inject_key_success', 'SSH key injected successfully for ') + (S.currentHost ? S.currentHost.name : 'host'), 'success');
      inspectHost(id);
    } else {
      showToast(t('inject_key_failed', 'Key injection failed: ') + (data.error || 'Unknown error'), 'error', 6000);
    }
  } catch(err) {
    showToast(t('inject_key_failed', 'Key injection failed: ') + err.message, 'error', 6000);
  } finally {
    restore();
  }
}

async function deleteHost(id) {
  closeAllDropdowns();
  if (!await showConfirm({ message: t('confirm_delete_host'), confirmText: t('delete_host', 'Delete Host') })) return;
  const res = await fetch('/api/hosts/' + id, { method: 'DELETE' });
  if (res.ok) {
    showToast(t('host_deleted', 'Host deleted successfully'), 'success');
  } else {
    showToast(t('delete_host_failed', 'Failed to delete host'), 'error');
  }
  loadHosts();
}

function toggleHostSelectMode(on) {
  S.hostSelectMode = on === undefined ? !S.hostSelectMode : !!on;
  if (!S.hostSelectMode) S.selectedHostIds.clear();
  const btn = document.getElementById('hostSelectBtn');
  if (btn) btn.classList.toggle('btn-primary', S.hostSelectMode);
  renderHosts();
}

function toggleHostSelected(id, on) {
  if (on) S.selectedHostIds.add(id); else S.selectedHostIds.delete(id);
  // Update in place instead of re-rendering every card.
  document.querySelectorAll(`[data-host-id="${id}"]`).forEach(el => {
    if (el.classList.contains('host-select-cb')) el.checked = on;
    else if (el.matches('.host-card, tr')) el.classList.toggle('host-selected', on);
  });
  renderHostSelectBar();
}

function selectAllShownHosts() {
  getFilteredAndSortedHosts().forEach(h => S.selectedHostIds.add(h.id));
  renderHosts();
}

function renderHostSelectBar() {
  const bar = document.getElementById('hostSelectBar');
  if (!bar) return;
  document.body.classList.toggle('host-select-on', S.hostSelectMode);
  if (!S.hostSelectMode) { bar.style.display = 'none'; bar.innerHTML = ''; return; }
  const n = S.selectedHostIds.size;
  const busy = !!S.hostBulkDeleting;
  bar.style.display = 'flex';
  bar.innerHTML = `
    <span class="hsb-count">${busy
      ? escapeHtml(t('host_select_deleting', 'Deleting {done}/{n}…').replace('{done}', S.hostBulkDeleting.done).replace('{n}', S.hostBulkDeleting.total))
      : escapeHtml(t('host_select_count', '{n} selected').replace('{n}', n))}</span>
    <button class="btn btn-sm btn-ghost" onclick="selectAllShownHosts()" ${busy ? 'disabled' : ''}>${svgIcon('list-checks', 'icon-sm')} <span>${t('host_select_all', 'Select all shown')}</span></button>
    <button class="btn btn-sm btn-ghost" onclick="selectedHostIds.clear(); renderHosts();" ${busy || !n ? 'disabled' : ''}>${t('host_select_clear', 'Clear')}</button>
    <button class="btn btn-sm" onclick="openBulkGroupModal()" ${busy || !n ? 'disabled' : ''}>${svgIcon('folder', 'icon-sm')} <span>${t('host_select_group', 'Change group')}</span></button>
    <button class="btn btn-sm btn-danger" onclick="deleteSelectedHosts()" ${busy || !n ? 'disabled' : ''}>${svgIcon('trash-2', 'icon-sm')} <span>${t('host_select_delete', 'Delete selected')}</span></button>
    <button class="btn btn-sm" onclick="toggleHostSelectMode(false)" ${busy ? 'disabled' : ''}>${t('host_select_done', 'Done')}</button>`;
}

function openBulkGroupModal() {
  const n = S.selectedHostIds.size;
  if (!n || S.hostBulkDeleting) return;
  const groups = Array.from(new Set(S.allRawHosts.map(h => (h.group_name || '').trim()).filter(Boolean))).sort((a, b) => a.localeCompare(b));
  document.getElementById('bulkGroupSuggestions').innerHTML = groups.map(g => `<option value="${escapeHtml(g)}"></option>`).join('');
  document.getElementById('bulkGroupDesc').textContent = t('host_select_group_desc', 'Set the group of {n} selected hosts.').replace('{n}', n);
  document.getElementById('bulkGroupInput').value = '';
  openModal('bulkGroupModal');
  document.getElementById('bulkGroupInput').focus();
}

async function submitBulkGroup() {
  const group = document.getElementById('bulkGroupInput').value.trim();
  const assignments = Array.from(S.selectedHostIds).map(id => ({ id, group_name: group }));
  if (!assignments.length) return;
  const btn = document.getElementById('bulkGroupSaveBtn');
  btn.disabled = true;
  try {
    const res = await fetch('/api/hosts/groups', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ assignments })
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
      showToast(t('save_host_failed', 'Failed to save host: ') + (data.error || res.status), 'error');
      return;
    }
    showToast(t('hosts_group_changed', 'Group updated for {n} hosts').replace('{n}', assignments.length), 'success');
    closeModal('bulkGroupModal');
    toggleHostSelectMode(false);
    loadHosts();
  } catch (e) {
    showToast(t('save_host_failed', 'Failed to save host: ') + e.message, 'error');
  } finally {
    btn.disabled = false;
  }
}

async function deleteSelectedHosts(hosts = S.allRawHosts.filter(h => S.selectedHostIds.has(h.id)), title) {
  if (!hosts.length || S.hostBulkDeleting) return;
  const names = hosts.map(h => h.name);
  const shown = names.slice(0, 10).join(', ') + (names.length > 10 ? ' ' + t('and_n_more', 'and {n} more').replace('{n}', names.length - 10) : '');
  const ok = await showConfirm({
    title: title || t('confirm_delete_hosts_title', 'Delete {n} hosts?').replace('{n}', hosts.length),
    message: t('confirm_delete_hosts', 'These hosts will be removed from Pantau.') + '\n\n' + shown,
    confirmText: t('delete_n_hosts', 'Delete {n} hosts').replace('{n}', hosts.length)
  });
  if (!ok) return;

  S.hostBulkDeleting = { done: 0, total: hosts.length };
  renderHostSelectBar();
  const failed = [];
  let next = 0;
  const worker = async () => {
    while (next < hosts.length) {
      const h = hosts[next++];
      try {
        const res = await fetch('/api/hosts/' + h.id, { method: 'DELETE' });
        if (res.ok) S.selectedHostIds.delete(h.id); else failed.push(h.name);
      } catch (e) {
        failed.push(h.name);
      }
      S.hostBulkDeleting.done++;
      renderHostSelectBar();
    }
  };
  await Promise.all(Array.from({ length: Math.min(4, hosts.length) }, worker));
  S.hostBulkDeleting = null;

  const deleted = hosts.length - failed.length;
  if (deleted) showToast(t('hosts_deleted', '{n} hosts deleted').replace('{n}', deleted), 'success');
  if (failed.length) showToast(t('hosts_delete_failed', 'Could not delete: {names}').replace('{names}', failed.join(', ')), 'error', 8000);
  if (!failed.length) S.hostSelectMode = false; // failed hosts stay selected for a retry
  const btn = document.getElementById('hostSelectBtn');
  if (btn) btn.classList.toggle('btn-primary', S.hostSelectMode);
  await loadHosts();
  renderHostSelectBar();
}

document.addEventListener('change', (e) => {
  const cb = e.target.closest && e.target.closest('.host-select-cb');
  if (cb) toggleHostSelected(Number(cb.dataset.hostId), cb.checked);
});

document.addEventListener('click', (e) => {
  if (!S.hostSelectMode || !e.target.closest) return;
  const card = e.target.closest('#hostGrid .host-card[data-host-id]');
  if (!card || e.target.closest('button, a, input, .dropdown, .dropdown-menu, [data-copy-ip], .host-card-note')) return;
  const id = Number(card.dataset.hostId);
  toggleHostSelected(id, !S.selectedHostIds.has(id));
});

async function testHostSSH(id, btn) {
  closeAllDropdowns();
  // Inline onclick handlers expose the clicked element as window.event.currentTarget.
  if (!btn && typeof window !== 'undefined' && window.event && window.event.currentTarget && window.event.currentTarget.tagName === 'BUTTON') {
    btn = window.event.currentTarget;
  }
  const restore = SP.busy(btn, t('upd_testing_ssh'));
  // The menu item lives in a dropdown that closes on click, so its spinner is not visible:
  // a sticky toast carries the elapsed time instead.
  const busyToast = showBusyToast(t('upd_testing_ssh'), Date.now());
  try {
    const res = await fetch('/api/hosts/' + id + '/test', { method: 'POST' });
    const data = await res.json();
    if (data.ok) {
      showToast(t('ssh_test_success', 'SSH Connection Success: ') + (data.output || 'Authenticated'), 'success', 5000);
    } else {
      showToast(t('ssh_test_failed', 'SSH Test Failed: ') + (data.error || 'Connection refused'), 'error', 6000);
    }
  } catch(err) {
    showToast(t('ssh_test_failed', 'SSH Test Failed: ') + err.message, 'error', 6000);
  } finally {
    restore();
    busyToast.close();
  }
}

async function inspectHost(id, e, quietCooldown) {
  if (e) e.stopPropagation();
  const modalBtn = (S.currentHost && S.currentHost.id === id) ? document.getElementById('inspectBtnModal') : null;
  const btns = [document.getElementById('inspectBtn-' + id), document.getElementById('inspectBtnList-' + id), modalBtn].filter(Boolean);
  // The button is a small icon-only button, so only its icon spins; the elapsed time
  // is carried by a sticky toast (the inspection can take a minute over SSH).
  const saved = btns.map(b => b.innerHTML);
  btns.forEach(b => {
    b.innerHTML = svgIcon('loader-circle', 'icon-sm');
    b.disabled = true;
  });
  const busyToast = showBusyToast(t('upd_inspecting'), Date.now());
  try {
    const res = await fetch('/api/hosts/' + id + '/inspect', { method: 'POST' });
    const data = await res.json();
    if (data.ok) {
      showToast(t('inspect_success', 'Inspection completed for host'), 'success');
      loadHosts();
      if (S.currentHost && S.currentHost.id === id) {
        openHostDetail(id);
      }
    } else if (res.status === 409 || data.in_flight) {
      showToast(data.error || t('inspect_in_flight', 'Inspection is already in progress for this host'), 'warning');
    } else if (res.status === 429 || data.cooldown) {
      if (!quietCooldown) showToast(data.error || t('inspect_cooldown', 'Host was recently inspected. Please wait a moment.'), 'warning');
    } else if (typeof data.error === 'string' && data.error.includes('network down:')) {
      // A network-path failure is ONE infrastructure event shared by every Host
      // behind the tunnel. Emit no per-host alert/notification: the Network-down
      // badge and KPI already report it. Just refresh so they reflect the state.
      loadHosts();
    } else {
      showToast(t('inspect_failed', 'Inspection failed: ') + (data.error || 'Unknown error'), 'error');
    }
  } catch(err) {
    showToast(t('inspect_failed', 'Inspection failed: ') + err.message, 'error');
  } finally {
    btns.forEach((b, i) => {
      b.innerHTML = saved[i];
      b.disabled = false;
    });
    busyToast.close();
  }
}

try {
  S.isHostDetailMaximized = localStorage.getItem('pantau_host_detail_maximized') === 'true';
} catch(e) {}

function applyHostDetailMaximizeState() {
  const modal = document.getElementById('hostDetailModal');
  const btn = document.getElementById('btnHostDetailMax');
  if (!modal) return;
  const content = modal.querySelector('.modal-content');
  if (!content) return;
  if (S.isHostDetailMaximized) {
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
}

function toggleHostDetailMaximize(e) {
  if (e && e.type === 'dblclick' && e.target && (e.target.tagName === 'BUTTON' || e.target.closest('button'))) return;
  S.isHostDetailMaximized = !S.isHostDetailMaximized;
  try {
    localStorage.setItem('pantau_host_detail_maximized', S.isHostDetailMaximized ? 'true' : 'false');
  } catch(err) {}
  applyHostDetailMaximizeState();
}

function getHostActiveTab(hostId) {
  try {
    const saved = sessionStorage.getItem('pantau_host_tab_' + hostId);
    if (saved && saved !== 'terminal') return saved;
  } catch(e) {}
  return 'overview';
}

function setHostActiveTab(hostId, tab) {
  try {
    if (hostId && tab && tab !== 'terminal') {
      sessionStorage.setItem('pantau_host_tab_' + hostId, tab);
    }
  } catch(e) {}
}

function getHostFilesPath(hostId) {
  try {
    const saved = sessionStorage.getItem('pantau_host_files_path_' + hostId);
    if (saved) return saved;
  } catch(e) {}
  return '/';
}

function setHostFilesPath(hostId, path) {
  try {
    if (hostId && path) {
      sessionStorage.setItem('pantau_host_files_path_' + hostId, path);
    }
  } catch(e) {}
}

function openTerminalForPath(targetPath) {
  if (!S.currentHost) return;
  const hostName = S.currentHost.name || ('Host ' + S.currentHost.id);
  const folderName = (targetPath === '/' || !targetPath) ? '/' : targetPath.split('/').filter(Boolean).pop() || targetPath;
  const customTitle = `${hostName}: ${folderName}`;
  openTerminalForHost(S.currentHost.id, null, true, customTitle, targetPath);
}

export { renderHosts, openHostGroupRename, saveHostGroupRename, deleteHostGroup, groupHosts, getHostGroupsCollapsed, toggleHostGroupCollapsed, renderGridView, buildHostCard, renderListView, fitHostList, setEngineStatus, updateFooterMetrics, loadVpnNetworkIndex, populateHostNetworkSelect, loadHosts, toggleAlertOverlay, closeAlertOverlay, renderAlertOverlayState, loadAlerts, loadAlertTriage, applyTriageRemediation, loadDailyBriefing, generateDailyBriefing, ackAlert, ackAllAlerts, openHostDetailFromAlert, populateHostKeySelect, vpnBadge, hostNetworkBadgeHtml, loadVpnNetworks, saveVpnGate, vpnDriverField, openVpnNetworkModal, loadVpnInterfaces, renderVpnDriverFields, collectVpnConfig, saveVpnNetwork, deleteVpnNetwork, vpnNetworkUp, vpnNetworkProbe, vpnNetworkDown, openVpnNetworkLog, onHostKeySelectChange, openAddHostModal, openEditHostModal, updateGroupSuggestions, saveHost, openCommissionDateModal, importFill, importReasonText, openImportHostsModal, importLoadFile, importRequestPreview, importParse, importReadServer, populateImportKeySelect, importBulkNetworkChanged, populateImportNetworkSelect, importInspectCreated, renderImportPreview, importNetworkPicked, importableBoxes, updateImportCount, importToggleAll, importSubmit, showImportGroupsStep, refreshImportGroupSuggestions, importSaveGroups, submitQuickCommissionDate, clearCommissionDate, showToast, showBusyToast, toggleDropdown, closeAllDropdowns, openInjectKeyModal, submitInjectKey, deleteHost, toggleHostSelectMode, toggleHostSelected, selectAllShownHosts, renderHostSelectBar, openBulkGroupModal, submitBulkGroup, deleteSelectedHosts, testHostSSH, inspectHost, applyHostDetailMaximizeState, toggleHostDetailMaximize, getHostActiveTab, setHostActiveTab, getHostFilesPath, setHostFilesPath, openTerminalForPath };
