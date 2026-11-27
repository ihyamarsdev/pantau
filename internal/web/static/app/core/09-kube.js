/* Kubernetes clusters, workloads, logs, and workspaces.
 * Native ES module fragment of the former monolithic inline script.
 * Air-gapped: no bundler, no CDN. Shared state lives in the mutable S object
 * from 00-state.js; cross-domain functions are imported explicitly. */
import { S } from './00-state.js';
import { svgIcon } from './00-foundation.js';
import { t, showConfirm, formatRelativeTime } from './01-i18n-auth.js';
import { loadHosts, showToast } from './02-hosts.js';
import { openHostDetail, dockerTableHtml, dockerBar } from './03-workspace-files-editor.js';
import { copyTextToClipboard, sendTabCommand, openTerminalForHost } from './04-terminal.js';
import { openModal, closeModal, escapeHtml } from './06-modal-docs-activity.js';
import { showDockerOutput, dockerFloatingMenu } from './08-metrics-systemd-docker.js';

function saveKubeWorkspaces() { localStorage.setItem('pantau_kube_workspaces', JSON.stringify(S.kubeWorkspaces)); }

function kubeApiBase(id = S.selectedKubeHostId) { return S.kubeIsCluster(id) ? `/api/kube/clusters/${-id}` : `/api/hosts/${id}/kube`; }

function kubeWsParam(id = S.selectedKubeHostId) { return S.kubeIsCluster(id) ? `cluster_id=${-id}` : `host_id=${id}`; }

function kubeHostClusterName(cid, fallback) {
  const c = (S.kubeHostClusters || []).find(x => x.cluster_id === cid);
  return c ? c.name : fallback;
}

function kubeTargetInfo(id) {
  if (S.kubeIsCluster(id)) {
    const c = (S.kubeClusters || []).find(x => x.id === -Number(id));
    return c ? { name: c.name, detail: kubeProviderLabel(c.provider) + (c.context ? ' · ' + c.context : ''), mode: c.kube_mode || 'full' } : null;
  }
  const hosts = S.allRawHosts || [];
  const h = hosts.find(x => String(x.id) === String(id));
  if (!h) return null;
  const cid = (h.kube_cluster_id || '').trim();
  if (!cid) return { name: h.name, detail: h.host, status: h.status || 'unknown', mode: h.kube_mode || 'full' };
  const members = hosts.filter(x => (x.kube_cluster_id || '').trim() === cid);
  const status = members.every(m => m.status === 'healthy') ? 'healthy' : (members.some(m => m.status === 'healthy' || m.status === 'degraded') ? 'degraded' : 'down');
  return { name: kubeHostClusterName(cid, members[0].name), detail: members.length + ' ' + (members.length === 1 ? t('kube_node_single', 'node') : t('kube_node_multi', 'nodes')), status, mode: h.kube_mode || 'full' };
}

function kubeSameTarget(a, b) {
  if (Number(a) === Number(b)) return true;
  if (S.kubeIsCluster(a) || S.kubeIsCluster(b)) return false;
  const cid = id => ((S.allRawHosts || []).find(x => String(x.id) === String(id))?.kube_cluster_id || '').trim();
  return cid(a) !== '' && cid(a) === cid(b);
}

function renderKubeWorkspaces() {
  const grid = document.getElementById('kubeWorkspaceGrid');
  const back = document.getElementById('btnKubeBack');
  const promptEl = document.getElementById('kubeSelectHostPrompt');
  if (!grid) return;
  const home = !S.selectedKubeHostId;
  const cards = home ? S.kubeWorkspaces.map(id => [id, kubeTargetInfo(id)]).filter(x => x[1]) : [];
  if (back) back.style.display = home ? 'none' : 'inline-flex';
  if (promptEl) promptEl.style.display = home && !cards.length ? 'block' : 'none';
  grid.style.display = cards.length ? 'grid' : 'none';
  grid.innerHTML = cards.map(([id, w]) => {
    const version = (S.kubeDetected[id] && S.kubeDetected[id].cluster.version) || '';
    return `<div class="card host-card kube-ws-card" style="cursor:pointer;" onclick="onSelectKubeHost(${id})">
      <div class="host-card-header">
        <div style="display:flex;align-items:center;gap:8px;min-width:0;">
          ${w.status ? `<span class="status-dot ${w.status}"></span>` : svgIcon('cloud', 'icon-sm')}
          <span style="font-weight:600;font-size:var(--fs-md);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;">${escapeHtml(w.name)}</span>
        </div>
        <button type="button" class="btn btn-icon btn-sm btn-ghost" onclick="event.stopPropagation();closeKubeWorkspace(${id})" title="${escapeHtml(t('kube_ws_remove', 'Remove from workspaces'))}" aria-label="${escapeHtml(t('kube_ws_remove', 'Remove from workspaces') + ': ' + w.name)}">${svgIcon('x', 'icon-sm')}</button>
      </div>
      <div class="mono text-muted" style="white-space:nowrap;overflow:hidden;text-overflow:ellipsis;">${escapeHtml(w.detail)}</div>
      <div style="display:flex;align-items:center;gap:6px;min-height:20px;">
        ${version ? `<span class="badge" style="font-family:var(--font-mono);background:var(--surface-2);border:1px solid var(--card-border);">${escapeHtml(version)}</span>` : ''}
        ${w.mode === 'full' ? '' : `<span class="badge badge-warning">${escapeHtml(w.mode === 'read-only' ? t('kube_mode_read_only_badge', 'Read-Only') : t('kube_mode_no_exec_badge', 'No-Exec'))}</span>`}
      </div>
      <div style="margin-top:auto;display:flex;justify-content:flex-end;">
        <button type="button" class="btn btn-sm btn-primary" onclick="event.stopPropagation();onSelectKubeHost(${id})" style="display:inline-flex;align-items:center;gap:6px;">${svgIcon('boxes', 'icon-sm')} <span>${t('kube_ws_open', 'Open')}</span></button>
      </div>
    </div>`;
  }).join('');
}

function closeKubeWorkspace(id) {
  const i = S.kubeWorkspaces.indexOf(id);
  if (i < 0) return;
  S.kubeWorkspaces.splice(i, 1);
  saveKubeWorkspaces();
  delete S.kubeWsState[id];
  delete S.kubeDetected[id];
  if (id !== S.selectedKubeHostId) return renderKubeWorkspaces();
  S.selectedKubeHostId = null; // so the removed one is not saved again
  onSelectKubeHost('');
}

function kubeProviderLabel(p) {
  const map = { eks: t('kube_ext_provider_eks', 'EKS'), gke: t('kube_ext_provider_gke', 'GKE'), aks: t('kube_ext_provider_aks', 'AKS'), generic: t('kube_ext_provider_generic', 'Generic') };
  return map[(p || '').toLowerCase()] || (p ? String(p).toUpperCase() : t('kube_ext_provider_generic', 'Generic'));
}

async function loadKubeClusters() {
  try {
    const res = await fetch('/api/kube/clusters');
    const data = await res.json();
    if (data && data.ok && Array.isArray(data.clusters)) {
      S.kubeClusters = data.clusters;
      S.kubeHostClusters = Array.isArray(data.host_clusters) ? data.host_clusters : [];
    } else {
      S.kubeClusters = [];
      S.kubeHostClusters = [];
    }
  } catch(e) {
    S.kubeClusters = [];
    S.kubeHostClusters = [];
  }
  populateKubeHostSelect();
  if (typeof window.renderTeleportSwitcher === 'function') window.renderTeleportSwitcher();
}

function handleKubeEnvelope(data) {
  if (!data) return;
  const gwChip = document.getElementById('kubeGatewayChip');
  if (gwChip) {
    if (data.external) {
      gwChip.style.display = 'inline-flex';
      gwChip.className = 'badge';
      gwChip.style.background = 'var(--surface-2)';
      gwChip.style.border = '1px solid var(--card-border)';
      const pLabel = kubeProviderLabel(data.provider);
      gwChip.textContent = t('kube_ext_external_chip', 'External') + ' · ' + pLabel;
      return;
    }
    if (data.served_by_host_name) {
      gwChip.style.display = 'inline-flex';
      if (data.failover) {
        const prim = (S.allRawHosts || []).find(x => String(x.id) === String(data.primary_host_id))?.name || ('host #' + data.primary_host_id);
        gwChip.className = 'badge badge-warning';
        gwChip.textContent = 'Gateway: ' + data.served_by_host_name + ' (' + t('kube_failover_from', 'failover from') + ' ' + prim + ')';
      } else {
        gwChip.className = 'badge';
        gwChip.style.background = 'var(--surface-2)';
        gwChip.style.border = '1px solid var(--card-border)';
        gwChip.textContent = 'Gateway: ' + data.served_by_host_name;
      }
    }
  }
  if (!data.external && data.failover) {
    const prim = (S.allRawHosts || []).find(x => String(x.id) === String(data.primary_host_id))?.name || ('host #' + data.primary_host_id);
    const err = data.primary_error ? (': ' + data.primary_error) : '';
    showToast(t('kube_failover_notice', 'Failover active: request served by') + ' ' + data.served_by_host_name + ' (' + prim + err + ')', 'warning', 6000);
  }
}

function getSelectedKubeMode() {
  if (S.kubeIsCluster(S.selectedKubeHostId)) {
    const c = (S.kubeClusters || []).find(x => x.id === -Number(S.selectedKubeHostId));
    return c?.kube_mode || 'full';
  }
  const h = (S.allRawHosts || []).find(x => String(x.id) === String(S.selectedKubeHostId));
  return h?.kube_mode || 'full';
}

function updateKubeModeChip() {
  const btnEdit = document.getElementById('btnKubeEditCluster');
  const btnDelete = document.getElementById('btnKubeDeleteCluster');
  const isExt = S.kubeIsCluster(S.selectedKubeHostId);
  if (btnEdit) btnEdit.style.display = (isExt || currentKubeHostClusterId()) ? 'inline-flex' : 'none';
  if (btnDelete) btnDelete.style.display = isExt ? 'inline-flex' : 'none';

  const chip = document.getElementById('kubeModeChip');
  if (!chip) return;
  const mode = getSelectedKubeMode();
  if (mode === 'read-only') {
    chip.style.display = 'inline-flex';
    chip.textContent = t('kube_mode_read_only_badge', 'Read-Only');
    chip.title = t('kube_mode_hint');
  } else if (mode === 'no-exec') {
    chip.style.display = 'inline-flex';
    chip.textContent = t('kube_mode_no_exec_badge', 'No-Exec');
    chip.title = t('kube_mode_hint');
  } else {
    chip.style.display = 'none';
  }
}

function findHostForKubeNode(n) {
  if (S.kubeIsCluster(S.selectedKubeHostId)) return null;
  if (!n) return null;
  const ip = (n.internalIP || '').trim();
  const name = (n.name || '').trim().toLowerCase();
  const shortName = name.split('.')[0];
  return (S.allRawHosts || []).find(h => {
    if (ip && h.host && h.host.trim() === ip) return true;
    const hn = (h.name || '').trim().toLowerCase();
    const hh = (h.host || '').trim().toLowerCase();
    if (name && (hn === name || hh === name)) return true;
    if (shortName && (hn === shortName || hh === shortName)) return true;
    if (hn && (shortName.startsWith(hn) || hn.startsWith(shortName))) return true;
    return false;
  });
}

async function detectKubeClusters() {
  const btn = document.getElementById('btnKubeDetectClusters');
  if (btn) btn.disabled = true;
  const hosts = (S.allRawHosts || []).slice();
  const statusArea = document.getElementById('kubeStatusArea');
  const t0 = Date.now();
  let done = 0;
  const render = () => {
    const text = t('kube_detecting_clusters', 'Detecting Kubernetes clusters...');
    const count = t('kube_detect_progress', '{done}/{total} hosts').replace('{done}', done).replace('{total}', hosts.length);
    if (statusArea) statusArea.innerHTML = SP.banner(text + ' — ' + count, t0, done, hosts.length);
  };
  render();
  try {
    let active = 0, idx = 0;
    await new Promise(resolve => {
      if (hosts.length === 0) return resolve();
      function next() {
        while (active < 5 && idx < hosts.length) {
          const h = hosts[idx++];
          active++;
          fetch('/api/hosts/' + h.id + '/kube/detect')
            .then(r => r.json())
            .then(data => {
              if (data && data.ok && data.cluster && data.cluster.cluster_id) {
                h.kube_cluster_id = data.cluster.cluster_id;
              }
            })
            .catch(() => {})
            .finally(() => {
              active--;
              done++;
              render();
              if (idx >= hosts.length && active === 0) resolve();
              else next();
            });
        }
      }
      next();
    });
    await loadHosts();
    await loadKubeClusters();
    showToast(t('kube_detect_complete', 'Cluster detection complete'), 'success');
  } finally {
    if (btn) btn.disabled = false;
    if (statusArea) statusArea.innerHTML = '';
  }
}

function formatKubeMemory(val) {
  if (!val) return '-';
  const s = String(val).trim();
  const m = s.match(/^([0-9]+(?:\.[0-9]+)?)\s*([A-Za-z]+)?$/);
  if (!m) return s;
  const num = parseFloat(m[1]);
  const unit = (m[2] || '').toLowerCase();
  let bytes = num;
  if (unit === 'ki' || unit === 'kib' || unit === 'k') bytes = num * 1024;
  else if (unit === 'mi' || unit === 'mib' || unit === 'm') bytes = num * 1024 * 1024;
  else if (unit === 'gi' || unit === 'gib' || unit === 'g') bytes = num * 1024 * 1024 * 1024;
  else if (unit === 'ti' || unit === 'tib' || unit === 't') bytes = num * 1024 * 1024 * 1024 * 1024;
  else if (unit === 'pi' || unit === 'pib' || unit === 'p') bytes = num * 1024 * 1024 * 1024 * 1024 * 1024;
  else if (unit === 'ei' || unit === 'eib' || unit === 'e') bytes = num * 1024 * 1024 * 1024 * 1024 * 1024 * 1024;
  else if (!unit) bytes = num;
  else return s;
  const gb = bytes / (1024 * 1024 * 1024);
  return gb.toFixed(gb < 1 ? 2 : 1) + ' GB';
}

function kubeAge(created) {
  if (!created) return '-';
  const r = formatRelativeTime(created);
  return r.relative || '-';
}

function kubeEmptyHtml(title, desc, isError) {
  return `<div class="empty-state" style="padding:40px 20px;">
    <div class="empty-state-icon" style="color:${isError ? 'var(--danger)' : 'var(--text-muted)'};">${svgIcon(isError ? 'triangle-alert' : 'boxes', 'icon-xl')}</div>
    <div class="empty-state-title" style="font-weight:600;margin-bottom:4px;">${escapeHtml(title)}</div>
    ${desc ? `<div class="empty-state-desc" style="color:var(--text-muted);font-size:var(--fs-sm);">${escapeHtml(desc)}</div>` : ''}
  </div>`;
}

function populateKubeHostSelect() {
  const sel = document.getElementById('kubeHostSelect');
  if (sel) {
    const currentVal = sel.value;
    let optHtml = `<option value="">${t('kube_select_host', 'Select a host...')}</option>`;
    if (S.kubeClusters && S.kubeClusters.length > 0) {
      optHtml += `<optgroup label="${t('kube_ext_group_external', 'External clusters')}">` +
        S.kubeClusters.map(c => `<option value="${-c.id}">${escapeHtml(c.name)} (${escapeHtml(kubeProviderLabel(c.provider))})</option>`).join('') +
        `</optgroup>`;
    }
    optHtml += (S.allRawHosts || []).map(h => `<option value="${h.id}">${escapeHtml(h.name)} (${escapeHtml(h.host)})</option>`).join('');
    sel.innerHTML = optHtml;
    const hasHost = hId => (S.allRawHosts || []).some(h => String(h.id) === String(hId)) || (S.kubeClusters || []).some(c => String(-c.id) === String(hId));
    if (S.selectedKubeHostId && hasHost(S.selectedKubeHostId)) {
      sel.value = String(S.selectedKubeHostId);
    } else if (currentVal && hasHost(currentVal)) {
      sel.value = currentVal;
    }
  }
  updateKubeHostPickerBtn();
  renderKubeHostPickerList();
  renderKubeWorkspaces();
}

function updateKubeHostPickerBtn() {
  const label = document.getElementById('kubeHostPickerSelected');
  if (!label) return;
  if (S.kubeIsCluster(S.selectedKubeHostId)) {
    const cluster = (S.kubeClusters || []).find(c => c.id === -Number(S.selectedKubeHostId));
    if (cluster) {
      const pLabel = kubeProviderLabel(cluster.provider);
      label.innerHTML = `<svg class="icon icon-xs text-muted" style="flex-shrink:0;"><use href="#i-cloud"/></svg> <span style="font-weight:600;">${escapeHtml(cluster.name)}</span> <span class="badge" style="font-size:10px;padding:1px 6px;">${escapeHtml(pLabel)}</span>`;
      return;
    }
  }
  const h = (S.allRawHosts || []).find(x => String(x.id) === String(S.selectedKubeHostId));
  if (h) {
    const status = h.status || 'unknown';
    const cid = (h.kube_cluster_id || '').trim();
    if (cid) {
      const members = (S.allRawHosts || []).filter(x => (x.kube_cluster_id || '').trim() === cid);
      const clusterName = kubeHostClusterName(cid, members[0]?.name || h.name);
      const combinedStatus = members.every(m => m.status === 'healthy') ? 'healthy' : (members.some(m => m.status === 'healthy' || m.status === 'degraded') ? 'degraded' : 'down');
      const countText = members.length + ' ' + (members.length === 1 ? t('kube_node_single', 'node') : t('kube_node_multi', 'nodes'));
      label.innerHTML = `<span class="status-dot ${combinedStatus}"></span> <span style="font-weight:600;">${escapeHtml(clusterName)}</span> <span class="badge" style="font-size:10px;padding:1px 6px;">${countText}</span>`;
      return;
    }
    label.innerHTML = `<span class="status-dot ${status}"></span> <span style="font-weight:600;">${escapeHtml(h.name)}</span> <span class="mono text-muted" style="font-size:var(--fs-xs);">(${escapeHtml(h.host)})</span>`;
  } else {
    label.innerHTML = `<span class="text-muted" data-i18n="kube_select_host">${t('kube_select_host', 'Select a host...')}</span>`;
  }
}

function toggleKubeHostPicker(e) {
  if (e) e.stopPropagation();
  const menu = document.getElementById('kubeHostPickerMenu');
  if (!menu) return;
  const isOpening = menu.style.display !== 'flex';
  if (isOpening) {
    menu.style.display = 'flex';
    const btn = document.getElementById('kubeHostPickerBtn');
    if (btn) btn.setAttribute('aria-expanded', 'true');
    const input = document.getElementById('kubeHostSearchInput');
    if (input) {
      input.value = '';
      renderKubeHostPickerList();
      input.focus();
    }
  } else {
    closeKubeHostPicker();
  }
}

function closeKubeHostPicker() {
  const menu = document.getElementById('kubeHostPickerMenu');
  if (menu) menu.style.display = 'none';
  const btn = document.getElementById('kubeHostPickerBtn');
  if (btn) btn.setAttribute('aria-expanded', 'false');
  S.kubePickerActiveIndex = -1;
}

function onKubeHostPickerBtnKeydown(e) {
  if (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ') {
    e.preventDefault();
    toggleKubeHostPicker();
  }
}

function onKubeHostPickerKeydown(e) {
  const list = document.getElementById('kubeHostPickerList');
  if (!list) return;
  const items = list.querySelectorAll('.kube-host-picker-item');
  if (e.key === 'ArrowDown') {
    e.preventDefault();
    if (items.length === 0) return;
    S.kubePickerActiveIndex = (S.kubePickerActiveIndex + 1) % items.length;
    updateKubePickerHighlight(items);
  } else if (e.key === 'ArrowUp') {
    e.preventDefault();
    if (items.length === 0) return;
    S.kubePickerActiveIndex = (S.kubePickerActiveIndex - 1 + items.length) % items.length;
    updateKubePickerHighlight(items);
  } else if (e.key === 'Enter') {
    e.preventDefault();
    if (S.kubePickerActiveIndex >= 0 && items[S.kubePickerActiveIndex]) {
      items[S.kubePickerActiveIndex].click();
    } else if (items.length > 0) {
      items[0].click();
    }
  } else if (e.key === 'Escape') {
    e.preventDefault();
    closeKubeHostPicker();
    const btn = document.getElementById('kubeHostPickerBtn');
    if (btn) btn.focus();
  } else if (e.key === 'Tab') {
    closeKubeHostPicker();
  }
}

function updateKubePickerHighlight(items) {
  items.forEach((item, idx) => {
    const isAct = idx === S.kubePickerActiveIndex;
    item.classList.toggle('active', isAct);
    if (isAct) item.scrollIntoView({ block: 'nearest' });
  });
}

function renderKubeHostPickerList() {
  const list = document.getElementById('kubeHostPickerList');
  const input = document.getElementById('kubeHostSearchInput');
  if (!list) return;
  const q = (input ? input.value : '').toLowerCase().trim();
  const hosts = S.allRawHosts || [];
  S.kubePickerActiveIndex = -1;

  const extItems = (S.kubeClusters || []).filter(c => {
    const pLabel = kubeProviderLabel(c.provider);
    return !q || (c.name || '').toLowerCase().includes(q) || (c.provider || '').toLowerCase().includes(q) || (c.context || '').toLowerCase().includes(q) || pLabel.toLowerCase().includes(q);
  });

  const clusterMap = {};
  const standalone = [];
  hosts.forEach(h => {
    const cid = (h.kube_cluster_id || '').trim();
    if (cid) {
      if (!clusterMap[cid]) clusterMap[cid] = [];
      clusterMap[cid].push(h);
    } else {
      standalone.push(h);
    }
  });

  const clusterItems = Object.keys(clusterMap).map(cid => {
    const members = clusterMap[cid];
    const clusterName = kubeHostClusterName(cid, members[0].name);
    const combinedStatus = members.every(m => m.status === 'healthy') ? 'healthy' : (members.some(m => m.status === 'healthy' || m.status === 'degraded') ? 'degraded' : 'down');
    const matches = !q || clusterName.toLowerCase().includes(q) || members.some(m => (m.name || '').toLowerCase().includes(q) || (m.host || '').toLowerCase().includes(q));
    const isSelected = S.selectedKubeHostId && members.some(m => String(m.id) === String(S.selectedKubeHostId));
    const targetHost = members.find(m => m.status === 'healthy') || members[0];
    const countText = members.length + ' ' + (members.length === 1 ? t('kube_node_single', 'node') : t('kube_node_multi', 'nodes'));
    return { cid, clusterName, combinedStatus, matches, isSelected, targetHostId: targetHost.id, countText };
  }).filter(c => c.matches);

  const standaloneItems = standalone.filter(h => {
    return !q || (h.name || '').toLowerCase().includes(q) || (h.host || '').toLowerCase().includes(q);
  });

  if (extItems.length === 0 && clusterItems.length === 0 && standaloneItems.length === 0) {
    list.innerHTML = `<div style="padding:10px;text-align:center;color:var(--text-muted);font-size:var(--fs-xs);">${t('kube_no_data', 'No items found')}</div>`;
    return;
  }

  let html = '';
  if (extItems.length > 0) {
    html += `<div style="font-size:10px;font-weight:600;color:var(--text-muted);text-transform:uppercase;padding:6px 10px 2px;letter-spacing:0.5px;">${t('kube_ext_group_external', 'External clusters')}</div>`;
    html += extItems.map(c => {
      const isSelected = S.selectedKubeHostId && String(S.selectedKubeHostId) === String(-c.id);
      const pLabel = kubeProviderLabel(c.provider);
      return `<div class="kube-host-picker-item" role="option" aria-selected="${isSelected}" onclick="pickKubeHost(${-c.id})" style="display:flex;align-items:center;gap:8px;padding:6px 10px;border-radius:var(--radius-sm);cursor:pointer;font-size:var(--fs-xs);transition:background var(--dur);background:${isSelected ? 'var(--accent-soft)' : 'transparent'};">
        <svg class="icon icon-xs text-muted" style="flex-shrink:0;"><use href="#i-cloud"/></svg>
        <span style="font-weight:600;color:var(--text);">${escapeHtml(c.name)}</span>
        <span class="badge" style="margin-left:auto;font-size:10px;padding:1px 6px;">${escapeHtml(pLabel)}</span>
      </div>`;
    }).join('');
  }

  if (clusterItems.length > 0) {
    html += clusterItems.map(c => `
      <div class="kube-host-picker-item" role="option" aria-selected="${c.isSelected}" onclick="pickKubeHost(${c.targetHostId})" style="display:flex;align-items:center;gap:8px;padding:6px 10px;border-radius:var(--radius-sm);cursor:pointer;font-size:var(--fs-xs);transition:background var(--dur);background:${c.isSelected ? 'var(--accent-soft)' : 'transparent'};">
        <span class="status-dot ${c.combinedStatus}"></span>
        <svg class="icon icon-xs text-muted" style="flex-shrink:0;"><use href="#i-boxes"/></svg>
        <span style="font-weight:600;color:var(--text);">${escapeHtml(c.clusterName)}</span>
        <span class="badge" style="margin-left:auto;font-size:10px;padding:1px 6px;">${c.countText}</span>
      </div>
    `).join('');
  }

  if (standaloneItems.length > 0) {
    if (extItems.length > 0 || Object.keys(clusterMap).length > 0) {
      html += `<div style="font-size:10px;font-weight:600;color:var(--text-muted);text-transform:uppercase;padding:6px 10px 2px;letter-spacing:0.5px;">${t('kube_standalone', 'Standalone')}</div>`;
    }
    html += standaloneItems.map(h => {
      const isSelected = S.selectedKubeHostId && String(h.id) === String(S.selectedKubeHostId);
      const status = h.status || 'unknown';
      return `<div class="kube-host-picker-item" role="option" aria-selected="${isSelected}" onclick="pickKubeHost(${h.id})" style="display:flex;align-items:center;gap:8px;padding:6px 10px;border-radius:var(--radius-sm);cursor:pointer;font-size:var(--fs-xs);transition:background var(--dur);background:${isSelected ? 'var(--accent-soft)' : 'transparent'};">
        <span class="status-dot ${status}"></span>
        <span style="font-weight:600;color:var(--text);">${escapeHtml(h.name)}</span>
        <span class="mono text-muted">(${escapeHtml(h.host)})</span>
        <span class="badge badge-${status}" style="margin-left:auto;font-size:10px;padding:1px 6px;">${escapeHtml(status)}</span>
      </div>`;
    }).join('');
  }

  list.innerHTML = html;
}

function pickKubeHost(hostId) {
  closeKubeHostPicker();
  onSelectKubeHost(hostId);
}

document.addEventListener('click', e => {
  if (!e.target.closest('#kubeHostPicker')) closeKubeHostPicker();
});

async function onSelectKubeHost(hostId) {
  let id = hostId ? parseInt(hostId, 10) : null;
  if (id) id = S.kubeWorkspaces.find(x => kubeSameTarget(x, id)) || id;
  if (S.selectedKubeHostId) S.kubeWsState[S.selectedKubeHostId] = { tab: S.currentKubeTab, ns: S.currentKubeNamespace };
  if (id && !S.kubeWorkspaces.includes(id)) { S.kubeWorkspaces.push(id); saveKubeWorkspaces(); }
  const st = S.kubeWsState[id] || { tab: S.currentKubeTab, ns: '' }; // a new workspace opens on the tab you are looking at
  S.selectedKubeHostId = id;
  S.currentKubeTab = st.tab;
  S.currentKubeNamespace = st.ns;
  S.kubeTabHasData = false;
  S.kubeTabStale = false;
  S.kubeTabRefreshBusy = false;
  renderKubeRefreshStatus();
  renderKubeWorkspaces();
  updateKubeHostPickerBtn();
  updateKubeModeChip();
  renderKubeHostPickerList();
  const hostSel = document.getElementById('kubeHostSelect');
  if (hostSel) hostSel.value = S.selectedKubeHostId ? String(S.selectedKubeHostId) : '';
  const noClusterEl = document.getElementById('kubeNoClusterState');
  const clusterBar = document.getElementById('kubeClusterBar');
  const viewsContainer = document.getElementById('kubeViewsContainer');
  const gwChip = document.getElementById('kubeGatewayChip');
  const detectChip = document.getElementById('kubeDetectChip');
  const chipsEl = document.getElementById('kubeClusterChips');
  const nsWrap = document.getElementById('kubeNamespaceSelectorWrapper');
  const content = document.getElementById('kubeTabContent');
  const tabsStrip = document.getElementById('kubeTabsStrip');
  if (gwChip) gwChip.style.display = 'none'; // the previous workspace's gateway
  if (!S.selectedKubeHostId) { // the card list (renderKubeWorkspaces) is on screen
    stopKubeAutorefresh();
    if (noClusterEl) noClusterEl.style.display = 'none';
    if (clusterBar) clusterBar.style.display = 'none';
    if (viewsContainer) viewsContainer.style.display = 'none';
    if (detectChip) detectChip.style.display = 'none';
    if (chipsEl) chipsEl.style.display = 'flex';
    if (nsWrap) nsWrap.style.display = 'flex';
    if (tabsStrip) tabsStrip.style.display = 'flex';
    return;
  }
  if (noClusterEl) noClusterEl.style.display = 'none';
  if (clusterBar) clusterBar.style.display = 'none';
  if (viewsContainer) viewsContainer.style.display = 'none';
  const detectT0 = Date.now();
  const detectText = t('kube_detecting_cluster', 'Detecting cluster...');
  let detected = false;
  // Progress in the cluster bar (the chips of the previous cluster are hidden until the answer arrives).
  if (detectChip) { detectChip.style.display = 'inline-flex'; detectChip.innerHTML = SP.html(detectText, detectT0); }
  if (clusterBar) clusterBar.style.display = 'flex';
  if (chipsEl) chipsEl.style.display = 'none';
  if (nsWrap) nsWrap.style.display = 'none';
  if (viewsContainer) viewsContainer.style.display = 'flex';
  if (tabsStrip) tabsStrip.style.display = 'none';
  if (content) content.innerHTML = SP.loading(detectText, detectT0);
  try {
    let data = S.kubeDetected[id];
    if (!data) {
      const res = await fetch(`${kubeApiBase()}/detect`);
      data = await res.json();
      if (id !== S.selectedKubeHostId) return; // another workspace was opened meanwhile
      if (data.ok && data.cluster) S.kubeDetected[id] = data; // a failed detect is tried again next time
    }
    handleKubeEnvelope(data);
    updateKubeModeChip();
    if (!S.kubeIsCluster(S.selectedKubeHostId) && data.cluster && data.cluster.cluster_id) {
      const curH = (S.allRawHosts || []).find(x => x.id === S.selectedKubeHostId);
      const isNewCid = curH && curH.kube_cluster_id !== data.cluster.cluster_id;
      if (curH) curH.kube_cluster_id = data.cluster.cluster_id;
      if (isNewCid) await loadKubeClusters();
      if (id !== S.selectedKubeHostId) return;
      updateKubeHostPickerBtn();
      updateKubeModeChip();
      renderKubeWorkspaces();
    }
    if (!data.ok || !data.cluster) {
      if (noClusterEl) {
        noClusterEl.style.display = 'block';
        const msg = document.getElementById('kubeNoClusterMsg');
        if (msg) msg.textContent = data.error || t('kube_no_cluster', 'No k3s or rke2 control plane found on this host.');
      }
      return;
    }
    S.currentKubeCluster = data.cluster;
    if (clusterBar) clusterBar.style.display = 'flex';
    document.getElementById('kubeDistroChip').textContent = (S.currentKubeCluster.distro || 'k3s').toUpperCase();
    document.getElementById('kubeVersionChip').textContent = S.currentKubeCluster.version || '-';
    document.getElementById('kubeRoleChip').textContent = S.currentKubeCluster.role || 'server';
    if (viewsContainer) viewsContainer.style.display = 'flex';
    detected = true;
    await loadKubeNamespaces();
    if (id !== S.selectedKubeHostId) return;
    switchKubeTab(S.currentKubeTab); // marks this workspace's tab and loads it
  } catch(e) {
    if (id !== S.selectedKubeHostId) return;
    if (noClusterEl) {
      noClusterEl.style.display = 'block';
      const msg = document.getElementById('kubeNoClusterMsg');
      if (msg) msg.textContent = e.message;
    }
  } finally {
    // The progress state is reset for the workspace that is still on screen; a stale answer is ignored.
    if (id === S.selectedKubeHostId) {
      if (detectChip) detectChip.style.display = 'none';
      if (detected) {
        if (chipsEl) chipsEl.style.display = 'flex';
        if (nsWrap) nsWrap.style.display = 'flex';
        if (tabsStrip) tabsStrip.style.display = 'flex';
      } else {
        if (clusterBar) clusterBar.style.display = 'none';
        if (tabsStrip) tabsStrip.style.display = 'flex';
        if (content) content.innerHTML = '';
      }
    }
  }
}

async function loadKubeNamespaces() {
  if (!S.selectedKubeHostId) return;
  const ws = S.selectedKubeHostId;
  const sel = document.getElementById('kubeNamespaceSelect');
  if (sel) {
    sel.disabled = true;
    sel.innerHTML = `<option value="">${escapeHtml(t('kube_namespaces_loading', 'Loading...'))}</option>`;
  }
  let list = [];
  try {
    const res = await fetch(`${kubeApiBase()}/namespaces`);
    const data = await res.json();
    if (data.ok && Array.isArray(data.namespaces)) list = data.namespaces;
  } catch(e) {}
  if (ws !== S.selectedKubeHostId) { // answer of a workspace that is no longer on screen
    if (sel) { sel.disabled = false; sel.innerHTML = `<option value="">${t('kube_all_namespaces', 'All Namespaces')}</option>`; }
    return;
  }
  S.kubeNamespacesList = list;
  if (sel) {
    sel.innerHTML = `<option value="">${t('kube_all_namespaces', 'All Namespaces')}</option>` +
      S.kubeNamespacesList.map(ns => `<option value="${escapeHtml(ns.name)}" ${ns.name === S.currentKubeNamespace ? 'selected' : ''}>${escapeHtml(ns.name)}</option>`).join('');
    sel.disabled = (S.currentKubeTab === 'pvs');
  }
}

function renderKubeRefreshStatus() {
  const el = document.getElementById('kubeRefreshStatus');
  if (!el) return;
  if (S.kubeTabStale) {
    el.style.display = 'block';
    el.innerHTML = `<div class="badge badge-warning" style="display:inline-flex;align-items:center;gap:6px;">${svgIcon('triangle-alert', 'icon-xs')} ${escapeHtml(t('kube_stale_data', 'Showing the last known data - the last refresh failed'))}</div>`;
    return;
  }
  if (S.kubeTabRefreshBusy) {
    el.style.display = 'block';
    el.innerHTML = `<div style="font-size:var(--fs-xs);color:var(--text-muted);display:flex;align-items:center;gap:6px;">${SP.html(t('kube_refreshing', 'Refreshing...'))}</div>`;
    return;
  }
  el.style.display = 'none';
  el.innerHTML = '';
}

function switchKubeTab(tab) {
  S.currentKubeTab = tab;
  S.kubeTabHasData = false; // a new tab starts empty: the loader shows until the first answer arrives
  S.kubeTabStale = false;
  S.kubeTabRefreshBusy = false;
  renderKubeRefreshStatus();
  document.querySelectorAll('#kubeViewsContainer .tabs .tab').forEach(el => {
    el.classList.toggle('active', el.id.toLowerCase() === ('tabkube' + tab.toLowerCase()));
  });
  const evWrap = document.getElementById('kubeEventsFilterWrap');
  const podWrap = document.getElementById('kubePodsFilterWrap');
  const roWrap = document.getElementById('kubeAutorefreshWrap');
  if (evWrap) evWrap.style.display = tab === 'events' ? 'inline-flex' : 'none';
  if (podWrap) podWrap.style.display = (tab === 'pods' || tab === 'workloads') ? 'inline-flex' : 'none';
  if (roWrap) roWrap.style.display = tab === 'apply' ? 'none' : 'inline-flex';
  restartKubeAutorefresh();
  const nsWrapper = document.getElementById('kubeNamespaceSelectorWrapper');
  const nsSelect = document.getElementById('kubeNamespaceSelect');
  if (nsWrapper) {
    nsWrapper.style.visibility = (tab === 'nodes' || tab === 'apply') ? 'hidden' : 'visible';
    if (nsSelect) {
      nsSelect.disabled = (tab === 'pvs');
      nsSelect.title = (tab === 'pvs') ? t('kube_cluster_scoped', 'Cluster-scoped resource') : '';
    }
  }
  loadKubeTabData(tab);
}

function onKubeNamespaceChange(ns) {
  S.currentKubeNamespace = ns;
  S.kubeTabHasData = false; // a different namespace: the loader shows until the first answer arrives
  S.kubeTabStale = false;
  renderKubeRefreshStatus();
  loadKubeTabData(S.currentKubeTab);
}

function refreshKubeCurrentTab() {
  if (!S.selectedKubeHostId) return;
  loadKubeNamespaces();
  loadKubeTabData(S.currentKubeTab);
}

function loadKubeTabData(tab) {
  if (tab === 'nodes') loadKubeNodes();
  else if (tab === 'workloads') loadKubeWorkloads();
  else if (tab === 'pods') loadKubePods();
  else if (tab === 'services') loadKubeServices();
  else if (tab === 'events') loadKubeEvents();
  else if (tab === 'apply') loadKubeApply();
  else if (['ingresses', 'configmaps', 'secrets', 'pvcs', 'pvs', 'jobs', 'cronjobs'].includes(tab)) {
    loadKubeGenericResource(tab);
  }
}

async function kubeFetchTab(url, key, heads, rowFn, cacheFn) {
  const content = document.getElementById('kubeTabContent');
  if (!content || !S.selectedKubeHostId) return;
  // A background tick keeps the last table on screen; only an empty tab shows the loader.
  if (!S.kubeTabHasData) content.innerHTML = SP.loading(t('kube_loading_resources', 'Loading resources...')) + SP.skeleton(6);
  else { S.kubeTabRefreshBusy = true; renderKubeRefreshStatus(); }
  const ws = S.selectedKubeHostId, tab = S.currentKubeTab;
  const stale = () => ws !== S.selectedKubeHostId || tab !== S.currentKubeTab; // answer of another workspace or tab
  try {
    const res = await fetch(url);
    const data = await res.json();
    if (stale()) return;
    handleKubeEnvelope(data);
    if (!data.ok) {
      if (S.kubeTabHasData) { S.kubeTabStale = true; return; } // keep the last data, mark it stale
      content.innerHTML = kubeEmptyHtml(t('error_prefix', 'Error: '), data.error || 'Failed to load', true);
      return;
    }
    const items = data[key] || [];
    if (cacheFn) cacheFn(items);
    if (key === 'nodes') S.kubeNodesCache = items;
    else if (key === 'workloads') S.kubeWorkloadsCache = items;
    else if (key === 'pods') S.kubePodsCache = items;
    else if (key === 'services') S.kubeServicesCache = items;
    S.kubeTabHasData = true;
    S.kubeTabStale = false;
    if (!rowFn) return;
    if (items.length === 0) { content.innerHTML = kubeEmptyHtml(t('kube_no_data', 'No items found'), ''); return; }
    content.innerHTML = dockerTableHtml(heads, items.map(rowFn).join(''));
  } catch(e) {
    if (stale()) return;
    if (S.kubeTabHasData) { S.kubeTabStale = true; return; } // keep the last data, mark it stale
    content.innerHTML = kubeEmptyHtml(t('error_prefix', 'Error: '), e.message, true);
  } finally {
    S.kubeTabRefreshBusy = false;
    renderKubeRefreshStatus();
  }
}

function loadKubeNodes() {
  kubeFetchTab(`${kubeApiBase()}/nodes`, 'nodes',
    [t('col_name', 'Name'), t('col_status', 'Status'), 'Roles', 'Version', t('col_ip', 'Internal IP'), t('docker_col_cpu', 'CPU'), t('docker_col_mem', 'Memory'), t('col_age', 'Age'), ''],
    (n, idx) => {
      const h = findHostForKubeNode(n);
      const hostChip = h ? `<a href="javascript:void(0)" onclick="openHostDetail(${h.id})" class="badge" style="font-size:10px;text-decoration:none;margin-left:6px;background:var(--surface-2);border:1px solid var(--card-border);display:inline-flex;align-items:center;gap:3px;" title="${escapeHtml(t('kube_node_open_host', 'Open Host Detail'))}: ${escapeHtml(h.name)}">${svgIcon('server', 'icon-xs')} ${escapeHtml(h.name)}</a><button type="button" class="btn btn-icon btn-xs btn-ghost" style="margin-left:2px;vertical-align:middle;" onclick="openHostTerminal(${h.id})" title="${escapeHtml(t('open_terminal', 'Terminal'))}">${svgIcon('terminal', 'icon-xs')}</button>` : '';
      const nodeLabel = h ? `<a href="javascript:void(0)" onclick="openHostDetail(${h.id})" style="color:inherit;text-decoration:none;" title="${escapeHtml(t('kube_node_open_host', 'Open Host Detail'))}"><b>${escapeHtml(n.name)}</b></a>` : `<b>${escapeHtml(n.name)}</b>`;
      let cpuHtml = escapeHtml(n.cpu || '-');
      if (n.usageCPU != null && n.usageCPU !== '') {
        const pct = n.cpuPercent != null ? Math.round(n.cpuPercent) : null;
        cpuHtml = `<div style="display:flex;align-items:center;gap:6px;"><span>${escapeHtml(n.usageCPU)}</span>${pct != null ? `<span style="font-size:var(--fs-xs);color:var(--text-muted);">(${pct}%)</span>${dockerBar(pct, 70, 90)}` : ''}</div>`;
      }
      let memHtml = escapeHtml(formatKubeMemory(n.memory));
      if (n.usageMemory != null && n.usageMemory !== '') {
        const pct = n.memoryPercent != null ? Math.round(n.memoryPercent) : null;
        memHtml = `<div style="display:flex;align-items:center;gap:6px;"><span>${escapeHtml(formatKubeMemory(n.usageMemory))}</span>${pct != null ? `<span style="font-size:var(--fs-xs);color:var(--text-muted);">(${pct}%)</span>${dockerBar(pct, 70, 90)}` : ''}</div>`;
      }
      return `<tr>
        <td>${nodeLabel}${hostChip}</td>
        <td><span class="badge ${n.status === 'Ready' ? 'badge-healthy' : 'badge-degraded'}">${escapeHtml(n.status)}</span>${n.unschedulable ? '<span class="badge badge-warning" style="margin-left:4px;">SchedulingDisabled</span>' : ''}</td>
        <td>${escapeHtml(Array.isArray(n.roles) ? n.roles.join(', ') : (n.roles || '<none>'))}</td>
        <td class="mono">${escapeHtml(n.version || '-')}</td>
        <td class="mono">${escapeHtml(n.internalIP || '-')}</td>
        <td>${cpuHtml}</td>
        <td>${memHtml}</td>
        <td style="color:var(--text-muted);font-size:var(--fs-xs);">${kubeAge(n.created)}</td>
        <td style="text-align:right;"><button class="btn btn-icon btn-sm btn-ghost" onclick="openKubeNodeMenu(event,${idx})" title="Actions">${svgIcon('ellipsis-vertical','icon-sm')}</button></td>
      </tr>`;
    }
  );
}

function kubeWorkloadIsProblem(w) {
  const desired = w.desired || 0;
  const ready = w.ready || 0;
  const available = w.available != null ? w.available : ready;
  return ready < desired || available < desired;
}

function renderKubeWorkloadsTable() {
  const content = document.getElementById('kubeTabContent');
  if (!content) return;
  let items = (S.kubeWorkloadsCache || []).slice();
  items.sort((a, b) => (kubeWorkloadIsProblem(b) ? 1 : 0) - (kubeWorkloadIsProblem(a) ? 1 : 0));
  if (S.kubePodsOnlyProblems) items = items.filter(kubeWorkloadIsProblem);
  if (items.length === 0) { content.innerHTML = kubeEmptyHtml(t('kube_no_data', 'No items found'), ''); return; }
  const heads = ['Kind', t('kube_namespace', 'Namespace'), t('col_name', 'Name'), 'Ready', 'Images', t('col_age', 'Age'), ''];
  content.innerHTML = dockerTableHtml(heads, items.map((w) => {
    const idx = S.kubeWorkloadsCache.indexOf(w);
    const problem = kubeWorkloadIsProblem(w);
    const imgs = Array.isArray(w.images) ? w.images.join(', ') : (w.images || '-');
    return `<tr${problem ? ' style="background:var(--warning-soft, rgba(234,179,8,0.06));"' : ''}>
      <td><span class="badge" style="background:var(--surface-2);border:1px solid var(--card-border);">${escapeHtml(w.kind)}</span></td>
      <td style="font-size:var(--fs-xs);color:var(--text-muted);">${escapeHtml(w.namespace)}</td>
      <td><b>${escapeHtml(w.name)}</b></td>
      <td><span class="badge ${!problem ? 'badge-healthy' : 'badge-degraded'}">${w.ready || 0}/${w.desired || 0}</span></td>
      <td class="mono" style="max-width:250px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;" title="${escapeHtml(imgs)}">${escapeHtml(imgs)}</td>
      <td style="color:var(--text-muted);font-size:var(--fs-xs);">${kubeAge(w.created)}</td>
      <td style="text-align:right;white-space:nowrap;">
        <button class="btn btn-icon btn-sm btn-ghost" onclick="openKubeTopology(${jsArg(w.kind)}, ${jsArg(w.namespace)}, ${jsArg(w.name)})" title="${escapeHtml(t('kube_topology', 'Topology'))}">${svgIcon('network', 'icon-sm')}</button>
        <button class="btn btn-icon btn-sm btn-ghost" onclick="openKubeWorkloadMenu(event,${idx})" title="Actions">${svgIcon('ellipsis-vertical', 'icon-sm')}</button>
      </td>
    </tr>`;
  }).join(''));
}

function loadKubeWorkloads() {
  kubeFetchTab(`${kubeApiBase()}/workloads?ns=${encodeURIComponent(S.currentKubeNamespace)}`, 'workloads',
    null, null,
    workloads => { S.kubeWorkloadsCache = workloads || []; renderKubeWorkloadsTable(); }
  );
}

const KUBE_UNHEALTHY_STATUSES = new Set([
  'CrashLoopBackOff', 'Pending', 'OOMKilled', 'ImagePullBackOff', 'ErrImagePull', 'Failed'
]);

function kubePodIsProblem(p) {
  const st = (p.status || '').trim();
  if (KUBE_UNHEALTHY_STATUSES.has(st)) return true;
  return st !== 'Running' && st !== 'Completed' && st !== 'Succeeded';
}

function onKubePodsFilterChange() {
  S.kubePodsOnlyProblems = document.getElementById('kubePodsOnlyProblemsFilter').checked;
  if (S.currentKubeTab === 'workloads') renderKubeWorkloadsTable();
  else renderKubePodsTable();
}

function renderKubePodsTable() {
  const content = document.getElementById('kubeTabContent');
  if (!content) return;
  let items = S.kubePodsCache.slice();
  items.sort((a, b) => {
    const ap = kubePodIsProblem(a) ? 1 : 0;
    const bp = kubePodIsProblem(b) ? 1 : 0;
    return bp - ap;
  });
  if (S.kubePodsOnlyProblems) items = items.filter(kubePodIsProblem);
  if (items.length === 0) { content.innerHTML = kubeEmptyHtml(t('kube_no_data', 'No items found'), ''); return; }
  const heads = [t('kube_namespace', 'Namespace'), t('col_name', 'Name'), 'Ready', t('col_status', 'Status'), 'Restarts', 'Node', 'IP', t('docker_col_cpu', 'CPU'), t('docker_col_mem', 'Memory'), t('col_age', 'Age'), ''];
  content.innerHTML = dockerTableHtml(heads, items.map((p) => {
    const idx = S.kubePodsCache.indexOf(p);
    const problem = kubePodIsProblem(p);
    const marker = problem ? `<span title="${escapeHtml(t('kube_pod_problem', 'Problem pod'))}" style="color:var(--warning,#eab308);margin-right:4px;">${svgIcon('triangle-alert', 'icon-xs')}</span>` : '';
    return `<tr${problem ? ' style="background:var(--warning-soft, rgba(234,179,8,0.06));"' : ''}>
      <td style="font-size:var(--fs-xs);color:var(--text-muted);">${escapeHtml(p.namespace)}</td>
      <td>${marker}<b>${escapeHtml(p.name)}</b></td>
      <td style="font-size:var(--fs-xs);">${escapeHtml(p.ready || '-')}</td>
      <td><span class="badge ${!problem ? 'badge-healthy' : 'badge-degraded'}">${escapeHtml(p.status)}</span></td>
      <td style="font-size:var(--fs-xs);">${p.restarts !== undefined ? p.restarts : 0}</td>
      <td style="font-size:var(--fs-xs);color:var(--text-muted);">${escapeHtml(p.node || '-')}</td>
      <td class="mono">${escapeHtml(p.ip || '-')}</td>
      <td>${escapeHtml(p.usageCPU || '-')}</td>
      <td>${p.usageMemory ? escapeHtml(formatKubeMemory(p.usageMemory)) : '-'}</td>
      <td style="color:var(--text-muted);font-size:var(--fs-xs);">${kubeAge(p.created)}</td>
      <td style="text-align:right;white-space:nowrap;">
        <button class="btn btn-icon btn-sm btn-ghost" onclick="openKubeTopology('Pod', ${jsArg(p.namespace)}, ${jsArg(p.name)})" title="${escapeHtml(t('kube_topology', 'Topology'))}">${svgIcon('network', 'icon-sm')}</button>
        <button class="btn btn-icon btn-sm btn-ghost" onclick="openKubePodMenu(event,${idx})" title="Actions">${svgIcon('ellipsis-vertical','icon-sm')}</button>
      </td>
    </tr>`;
  }).join(''));
}

function loadKubePods() {
  kubeFetchTab(`${kubeApiBase()}/pods?ns=${encodeURIComponent(S.currentKubeNamespace)}`, 'pods', null, null,
    pods => { S.kubePodsCache = pods || []; renderKubePodsTable(); }
  );
}

function loadKubeServices() {
  kubeFetchTab(`${kubeApiBase()}/services?ns=${encodeURIComponent(S.currentKubeNamespace)}`, 'services',
    [t('kube_namespace', 'Namespace'), t('col_name', 'Name'), 'Type', 'Cluster IP', 'External IP', 'Ports', t('col_age', 'Age'), ''],
    (s, idx) => {
      const ports = Array.isArray(s.ports) ? s.ports.join(', ') : (s.ports || '-');
      return `<tr>
        <td style="font-size:var(--fs-xs);color:var(--text-muted);">${escapeHtml(s.namespace)}</td>
        <td><b>${escapeHtml(s.name)}</b></td>
        <td><span class="badge" style="background:var(--surface-2);border:1px solid var(--card-border);">${escapeHtml(s.type || 'ClusterIP')}</span></td>
        <td class="mono">${escapeHtml(s.clusterIP || '-')}</td>
        <td class="mono">${escapeHtml(s.externalIP || '-')}</td>
        <td class="mono" style="max-width:200px;overflow:hidden;text-overflow:ellipsis;" title="${escapeHtml(ports)}">${escapeHtml(ports)}</td>
        <td style="color:var(--text-muted);font-size:var(--fs-xs);">${kubeAge(s.created)}</td>
        <td style="text-align:right;white-space:nowrap;">
          <button class="btn btn-icon btn-sm btn-ghost" onclick="openKubeTopology('Service', ${jsArg(s.namespace)}, ${jsArg(s.name)})" title="${escapeHtml(t('kube_topology', 'Topology'))}">${svgIcon('network', 'icon-sm')}</button>
          <button class="btn btn-icon btn-sm btn-ghost" onclick="openKubeServiceMenu(event,${idx})" title="Actions">${svgIcon('ellipsis-vertical','icon-sm')}</button>
        </td>
      </tr>`;
    }
  );
}

function onKubeEventsFilterChange() {
  S.kubeEventsWarningOnly = document.getElementById('kubeEventsWarningFilter').checked;
  renderKubeEventsTable();
}

function renderKubeEventsTable() {
  const content = document.getElementById('kubeTabContent');
  if (!content) return;
  const all = S.kubeEventsCache;
  const rows = S.kubeEventsWarningOnly ? all.filter(ev => ev.type === 'Warning') : all;
  const counter = t('kube_logs_matches', '{n} match(es)').replace('{n}', rows.length + '/' + all.length);
  if (rows.length === 0) { content.innerHTML = kubeEmptyHtml(t('kube_no_data', 'No items found'), counter); return; }
  content.innerHTML = dockerTableHtml(
    [t('kube_namespace', 'Namespace'), t('col_status', 'Type'), 'Reason', 'Object', 'Message', 'Count', 'Last Seen'],
    rows.map(ev => `<tr>
      <td style="font-size:var(--fs-xs);color:var(--text-muted);">${escapeHtml(ev.namespace || '-')}</td>
      <td><span class="badge ${ev.type === 'Warning' ? 'badge-warning' : 'badge-info'}">${escapeHtml(ev.type || 'Normal')}</span></td>
      <td><b>${escapeHtml(ev.reason || '-')}</b></td>
      <td class="mono text-muted">${escapeHtml(ev.object || '-')}</td>
      <td style="font-size:var(--fs-xs);word-break:break-word;">${escapeHtml(ev.message || '-')}</td>
      <td style="font-size:var(--fs-xs);">${ev.count || 1}</td>
      <td style="color:var(--text-muted);font-size:var(--fs-xs);">${kubeAge(ev.last)}</td>
    </tr>`).join(''));
}

function loadKubeEvents() {
  kubeFetchTab(`${kubeApiBase()}/events?ns=${encodeURIComponent(S.currentKubeNamespace)}`, 'events', null, null,
    evs => { S.kubeEventsCache = evs || []; renderKubeEventsTable(); });
}

async function loadKubeGenericResource(kind) {
  const content = document.getElementById('kubeTabContent');
  if (!content || !S.selectedKubeHostId) return;
  // A background tick keeps the last table on screen; only an empty tab shows the loader.
  if (!S.kubeTabHasData) content.innerHTML = SP.loading(t('kube_loading_resources', 'Loading resources...')) + SP.skeleton(6);
  else { S.kubeTabRefreshBusy = true; renderKubeRefreshStatus(); }
  const isClusterScoped = (kind === 'pvs');
  const nsParam = isClusterScoped ? '' : encodeURIComponent(S.currentKubeNamespace);
  const url = `${kubeApiBase()}/resources?kind=${encodeURIComponent(kind)}&ns=${nsParam}`;
  const ws = S.selectedKubeHostId;
  const stale = () => ws !== S.selectedKubeHostId || kind !== S.currentKubeTab; // answer of another workspace or tab
  try {
    const res = await fetch(url);
    const data = await res.json();
    if (stale()) return;
    handleKubeEnvelope(data);
    if (!data.ok) {
      if (S.kubeTabHasData) { S.kubeTabStale = true; return; } // keep the last data, mark it stale
      content.innerHTML = kubeEmptyHtml(t('error_prefix', 'Error: '), data.error || 'Failed to load', true);
      return;
    }
    S.kubeTabHasData = true;
    S.kubeTabStale = false;
    S.kubeGenericCache[kind] = data;
    const columns = data.columns || [];
    const rows = data.rows || [];
    const isSecrets = (kind === 'secrets');
    const noteHtml = isSecrets ? `<div class="card" style="padding:10px 14px;margin-bottom:12px;background:var(--surface-2);border:1px solid var(--card-border);font-size:var(--fs-xs);color:var(--text-muted);display:flex;align-items:center;gap:8px;"><svg class="icon icon-sm text-accent"><use href="#i-shield"/></svg> <span>${t('kube_secrets_note', 'Secret values are never transmitted or displayed.')}</span></div>` : '';

    if (rows.length === 0) {
      content.innerHTML = noteHtml + kubeEmptyHtml(t('kube_no_data', 'No items found'), '');
      return;
    }

    const heads = columns.map(c => t('col_' + c.toLowerCase(), c.charAt(0).toUpperCase() + c.slice(1)));
    if (!isSecrets) heads.push('');

    const bodyHtml = rows.map((r, rowIdx) => {
      const cells = columns.map((col, colIdx) => {
        const val = r[colIdx] != null ? String(r[colIdx]) : '-';
        if (col === 'created' || col === 'lastSchedule') {
          return `<td style="color:var(--text-muted);font-size:var(--fs-xs);">${kubeAge(val)}</td>`;
        }
        if (col === 'name') {
          return `<td><b>${escapeHtml(val)}</b></td>`;
        }
        if (col === 'namespace') {
          return `<td style="font-size:var(--fs-xs);color:var(--text-muted);">${escapeHtml(val)}</td>`;
        }
        if (col === 'status') {
          const isOk = val === 'Bound' || val === 'Complete' || val === 'Active';
          return `<td><span class="badge ${isOk ? 'badge-healthy' : 'badge-info'}">${escapeHtml(val)}</span></td>`;
        }
        return `<td>${escapeHtml(val)}</td>`;
      }).join('');
      const actionCell = isSecrets ? '' : `<td style="text-align:right;"><button class="btn btn-icon btn-sm btn-ghost" onclick="openKubeGenericMenu(event, '${kind}', ${rowIdx})" title="Actions">${svgIcon('ellipsis-vertical', 'icon-sm')}</button></td>`;
      return `<tr>${cells}${actionCell}</tr>`;
    }).join('');

    content.innerHTML = noteHtml + dockerTableHtml(heads, bodyHtml);
  } catch(e) {
    if (stale()) return;
    if (S.kubeTabHasData) { S.kubeTabStale = true; return; } // keep the last data, mark it stale
    content.innerHTML = kubeEmptyHtml(t('error_prefix', 'Error: '), e.message, true);
  } finally {
    S.kubeTabRefreshBusy = false;
    renderKubeRefreshStatus();
  }
}

function openKubeGenericMenu(e, kind, rowIdx) {
  const data = S.kubeGenericCache[kind];
  if (!data || !data.rows || !data.rows[rowIdx]) return;
  const row = data.rows[rowIdx];
  const columns = data.columns || [];
  const nsIdx = columns.indexOf('namespace');
  const nameIdx = columns.indexOf('name');
  const ns = nsIdx >= 0 ? row[nsIdx] : '';
  const name = nameIdx >= 0 ? row[nameIdx] : '';
  const singular = { ingresses: 'ingress', configmaps: 'configmap', pvcs: 'pvc', pvs: 'pv', jobs: 'job', cronjobs: 'cronjob' }[kind] || kind;
  const items = [];
  if (S.kubeYamlEditable && S.kubeYamlEditable[singular]) {
    items.push({ label: t('kube_edit_yaml', 'Edit YAML'), run: () => openKubeYamlEditor(singular, ns, name) });
  }
  if (singular === 'ingress' || singular === 'pvc') {
    items.push({ label: t('kube_topology', 'Topology'), run: () => openKubeTopology(singular === 'pvc' ? 'PVC' : 'Ingress', ns, name) });
  }
  items.push({ label: t('kube_describe', 'Describe'), run: () => kubeDescribe(singular, ns, name) });
  dockerFloatingMenu(e, 'kg_' + kind + '_' + rowIdx, items);
}

function loadKubeApply() {
  const content = document.getElementById('kubeTabContent');
  if (!content) return;
  const isRo = getSelectedKubeMode() === 'read-only';
  S.lastDiffedYaml = null;
  const roBadgeHtml = isRo ? `<div style="display:flex;align-items:center;gap:6px;padding:8px 12px;background:var(--warning-soft, rgba(234,179,8,0.1));border:1px solid var(--warning-border, #eab308);border-radius:var(--radius-sm);color:var(--text);font-size:var(--fs-xs);margin-bottom:8px;">
    ${svgIcon('shield-alert', 'icon-xs')}
    <span style="font-weight:600;">${t('kube_mode_read_only_badge', 'Read-Only')}</span>: ${t('kube_apply_ro_warning', 'Applying manifests is blocked in Read-Only mode.')}
  </div>` : '';
  content.innerHTML = `<div class="card" style="padding:16px;display:flex;flex-direction:column;gap:12px;">
    ${roBadgeHtml}
    <div style="font-size:var(--fs-sm);color:var(--text-muted);">${t('kube_apply_desc', 'Paste a Kubernetes manifest (YAML) to preview changes and apply to the cluster.')}</div>
    <textarea id="kubeApplyYaml" class="form-input font-mono" rows="14" oninput="onKubeApplyYamlInput()" style="width:100%;box-sizing:border-box;font-family:var(--font-mono);font-size:var(--fs-xs);resize:vertical;min-height:250px;" placeholder="${escapeHtml(t('kube_apply_placeholder', '# Paste Kubernetes YAML here...'))}"></textarea>
    <div style="display:flex;gap:8px;justify-content:flex-end;align-items:center;flex-wrap:wrap;">
      <button class="btn btn-ghost" onclick="clearKubeApply()">${t('clear', 'Clear')}</button>
      <button class="btn" id="btnKubePreviewDiff" onclick="submitKubeDiff()" style="display:inline-flex;align-items:center;gap:6px;">
        ${svgIcon('git-compare', 'icon-sm')} <span>${t('kube_btn_preview_diff', 'Preview Diff')}</span>
      </button>
      <button class="btn btn-primary" id="btnKubeApplySubmit" disabled onclick="submitKubeApply()" title="${isRo ? t('kube_mode_blocked_ro', 'Disabled in Read-Only mode') : t('kube_apply_locked_hint', 'Run Preview Diff first')}" style="display:inline-flex;align-items:center;gap:6px;">
        ${svgIcon('play', 'icon-sm')} <span>${t('kube_btn_apply', 'Apply YAML')}</span>
      </button>
    </div>
    <pre id="kubeApplyOutput" class="kube-apply-output"></pre>
  </div>`;
}

function onKubeApplyYamlInput() {
  const btn = document.getElementById('btnKubeApplySubmit');
  const yamlEl = document.getElementById('kubeApplyYaml');
  const currentYaml = yamlEl ? yamlEl.value : '';
  const isRo = getSelectedKubeMode() === 'read-only';
  if (isRo) {
    if (btn) { btn.disabled = true; btn.title = t('kube_mode_blocked_ro', 'Disabled in Read-Only mode'); }
    return;
  }
  const isCurrentDiffed = S.lastDiffedYaml !== null && S.lastDiffedYaml === currentYaml;
  if (btn) {
    btn.disabled = !isCurrentDiffed;
    btn.title = isCurrentDiffed ? '' : t('kube_apply_locked_hint', 'Run Preview Diff first');
  }
}

function clearKubeApply() {
  const y = document.getElementById('kubeApplyYaml');
  const o = document.getElementById('kubeApplyOutput');
  if (y) y.value = '';
  if (o) { o.style.display = 'none'; o.textContent = ''; }
  S.lastDiffedYaml = null;
  onKubeApplyYamlInput();
}

async function submitKubeDiff() {
  if (!S.selectedKubeHostId) return;
  const yamlEl = document.getElementById('kubeApplyYaml');
  const yaml = (yamlEl ? yamlEl.value : '').trim();
  if (!yaml) { showToast(t('kube_apply_empty', 'Please enter a YAML manifest'), 'warning'); return; }
  const btnDiff = document.getElementById('btnKubePreviewDiff');
  const btnApply = document.getElementById('btnKubeApplySubmit');
  const outEl = document.getElementById('kubeApplyOutput');
  const restore = SP.busy(btnDiff, t('kube_diffing', 'Running dry-run diff...'));
  if (outEl) {
    outEl.style.display = 'block';
    outEl.className = 'kube-apply-output';
    outEl.innerHTML = SP.html(t('kube_diffing', 'Running dry-run diff...'));
  }
  try {
    const res = await fetch(`${kubeApiBase()}/diff`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ yaml })
    });
    const data = await res.json();
    handleKubeEnvelope(data);
    if (outEl) {
      outEl.style.display = 'block';
      if (data.ok) {
        if (data.changed) {
          outEl.textContent = data.output || '(Differences detected)';
          outEl.className = 'kube-apply-output';
        } else {
          outEl.textContent = t('kube_diff_no_changes', 'No changes detected. Cluster state matches manifest.');
          outEl.className = 'kube-apply-output success';
        }
      } else {
        outEl.textContent = 'Error: ' + (data.error || 'Diff failed');
        outEl.className = 'kube-apply-output error';
      }
    }
    if (data.ok) {
      S.lastDiffedYaml = (yamlEl ? yamlEl.value : '');
      const isRo = getSelectedKubeMode() === 'read-only';
      if (btnApply && !isRo) {
        btnApply.disabled = false;
        btnApply.title = '';
      }
    } else {
      S.lastDiffedYaml = null;
      if (btnApply) {
        btnApply.disabled = true;
        btnApply.title = t('kube_apply_locked_hint', 'Run Preview Diff first');
      }
    }
  } catch(e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error', 9000);
    if (outEl) {
      outEl.style.display = 'block';
      outEl.className = 'kube-apply-output error';
      outEl.textContent = 'Error: ' + e.message;
    }
  } finally {
    restore();
  }
}

function kubeBusy(text) {
  const host = document.getElementById('kubeViewsContainer');
  if (!host) return function () {};
  const el = document.createElement('div');
  el.className = 'kube-busy-line';
  el.setAttribute('role', 'status');
  el.setAttribute('aria-live', 'polite');
  el.style.cssText = 'display:flex;align-items:center;gap:6px;padding:8px 12px;margin-bottom:12px;border:1px solid var(--card-border);border-radius:var(--radius-sm, 6px);background:var(--surface-2, var(--bg));font-size:var(--fs-sm);';
  el.innerHTML = SP.html(text, Date.now());
  host.insertBefore(el, host.firstChild);
  let done = false;
  return function () {
    if (done) return;
    done = true;
    if (el.parentNode) el.parentNode.removeChild(el);
  };
}

async function kubeAction(payload, okMsg, refreshTab) {
  if (!S.selectedKubeHostId) return;
  const done = kubeBusy(t('kube_action_running', 'Running {op} on {name}...')
    .replace('{op}', payload.op || 'action')
    .replace('{name}', payload.name || ''));
  try {
    const res = await fetch(`${kubeApiBase()}/action`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    const data = await res.json();
    handleKubeEnvelope(data);
    if (data.ok) {
      showToast(okMsg || t('dk_done', 'Done'), 'success');
      if (data.output) showDockerOutput((payload.op || '') + ' ' + (payload.name || ''), data.output);
      if (refreshTab) loadKubeTabData(refreshTab);
      return data;
    }
    showToast(t('error_prefix', 'Error: ') + (data.error || 'Action failed'), 'error', 9000);
  } catch(e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error', 9000);
  } finally {
    done();
  }
}

async function kubeGetOutput(url, title) {
  if (!S.selectedKubeHostId) return;
  const done = kubeBusy(t('kube_output_running', 'Fetching {title}...').replace('{title}', title || ''));
  try {
    const res = await fetch(url);
    const data = await res.json();
    handleKubeEnvelope(data);
    if (data.ok) showDockerOutput(title, data.output || '(No output)');
  } catch(e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error', 9000);
  } finally {
    done();
  }
}

async function submitKubeApply() {
  if (!S.selectedKubeHostId) return;
  const isRo = getSelectedKubeMode() === 'read-only';
  if (isRo) {
    showToast(t('kube_mode_blocked_ro', 'Disabled in Read-Only mode'), 'warning');
    return;
  }
  const yamlEl = document.getElementById('kubeApplyYaml');
  const currentYaml = yamlEl ? yamlEl.value : '';
  const yaml = currentYaml.trim();
  if (!yaml) { showToast(t('kube_apply_empty', 'Please enter a YAML manifest'), 'warning'); return; }
  if (S.lastDiffedYaml === null || S.lastDiffedYaml !== currentYaml) {
    showToast(t('kube_apply_diff_needed', 'Manifest has changed. Please preview diff before applying.'), 'warning');
    onKubeApplyYamlInput();
    return;
  }
  const confirmed = await showConfirm({
    title: t('kube_confirm_apply_title', 'Apply Manifest'),
    message: t('kube_confirm_apply_msg', 'Are you sure you want to apply this manifest to the cluster?'),
    danger: true,
    confirmText: t('kube_btn_apply', 'Apply YAML')
  });
  if (!confirmed) return;
  const btn = document.getElementById('btnKubeApplySubmit');
  const restore = SP.busy(btn, t('kube_applying', 'Applying manifest...'));
  try {
    const res = await fetch(`${kubeApiBase()}/apply`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ yaml })
    });
    const data = await res.json();
    handleKubeEnvelope(data);
    const outEl = document.getElementById('kubeApplyOutput');
    if (outEl) {
      outEl.style.display = 'block';
      outEl.textContent = data.output || (data.ok ? t('kube_apply_success') : ('Error: ' + (data.error || 'Apply failed')));
      outEl.className = 'kube-apply-output ' + (data.ok ? 'success' : 'error');
    }
    if (data.ok) showToast(t('kube_apply_success', 'Manifest applied successfully'), 'success');
    else showToast(t('error_prefix', 'Error: ') + (data.error || 'Apply failed'), 'error', 9000);
  } catch(e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error', 9000);
    const outEl = document.getElementById('kubeApplyOutput');
    if (outEl) {
      outEl.style.display = 'block';
      outEl.className = 'kube-apply-output error';
      outEl.textContent = 'Error: ' + e.message;
    }
  } finally {
    restore();
  }
}

function openKubeNodeMenu(e, idx) {
  const n = S.kubeNodesCache[idx];
  if (!n) return;
  const isCordoned = !!n.unschedulable;
  const isRo = getSelectedKubeMode() === 'read-only';
  const h = findHostForKubeNode(n);
  const items = [];
  if (h) {
    items.push({ label: t('kube_node_open_host', 'Open Host Detail') + ' (' + h.name + ')', run: () => openHostDetail(h.id) });
    items.push({ label: t('kube_node_open_term', 'Open Host Terminal'), run: () => openHostTerminal(h.id) });
  }
  items.push(
    {
      label: t('kube_drain', 'Drain'),
      disabled: isRo,
      title: isRo ? t('kube_mode_blocked_ro', 'Disabled in Read-Only mode') : '',
      run: () => openKubeDrain(n.name)
    },
    {
      label: isCordoned ? t('kube_uncordon', 'Uncordon Node') : t('kube_cordon', 'Cordon Node'),
      disabled: isRo,
      title: isRo ? t('kube_mode_blocked_ro', 'Disabled in Read-Only mode') : '',
      run: async () => {
        if (!isCordoned) {
          const confirmed = await showConfirm({
            title: t('kube_cordon', 'Cordon Node'),
            message: t('kube_confirm_cordon_msg', 'Cordoning node {name} will mark it unschedulable. Existing workloads continue running, but no new pods will be scheduled on this node.').replace('{name}', n.name),
            danger: true,
            confirmText: t('kube_cordon', 'Cordon Node')
          });
          if (!confirmed) return;
        }
        kubeAction({ op: isCordoned ? 'uncordon' : 'cordon', kind: 'node', ns: '', name: n.name }, null, 'nodes');
      }
    },
    { label: t('kube_describe', 'Describe'), run: () => kubeDescribe('node', '', n.name) }
  );
  dockerFloatingMenu(e, 'kn_' + idx, items);
}

function openKubeWorkloadMenu(e, idx) {
  const wl = S.kubeWorkloadsCache[idx];
  if (!wl) return;
  const isRo = getSelectedKubeMode() === 'read-only';
  const items = [];
  const yamlKind = (wl.kind || '').toLowerCase();
  if (S.kubeYamlEditable && S.kubeYamlEditable[yamlKind]) {
    items.push({ label: t('kube_edit_yaml', 'Edit YAML'), run: () => openKubeYamlEditor(yamlKind, wl.namespace, wl.name) });
  }
  items.push({ label: t('kube_logs_workload', 'Workload Logs'), run: () => openKubeWorkloadLogs(wl) });
  if (wl.kind === 'Deployment' || wl.kind === 'StatefulSet') {
    items.push({
      label: t('kube_btn_scale', 'Scale'),
      disabled: isRo,
      title: isRo ? t('kube_mode_blocked_ro', 'Disabled in Read-Only mode') : '',
      run: () => openKubeScaleModal(wl)
    });
  }
  items.push({
    label: t('kube_restart_workload', 'Restart Rollout'),
    disabled: isRo,
    title: isRo ? t('kube_mode_blocked_ro', 'Disabled in Read-Only mode') : '',
    run: async () => {
      const count = wl.desired || 1;
      const confirmed = await showConfirm({
        title: t('kube_restart_workload', 'Restart Rollout'),
        message: t('kube_confirm_restart_msg', 'Restarting {name} will recreate {count} replica(s) in a rolling update.').replace('{name}', wl.name).replace('{count}', count),
        danger: false,
        confirmText: t('kube_restart_workload', 'Restart Rollout')
      });
      if (!confirmed) return;
      kubeAction({ op: 'restart', kind: wl.kind.toLowerCase(), ns: wl.namespace, name: wl.name }, null, 'workloads');
    }
  });
  items.push({ label: t('kube_topology', 'Topology'), run: () => openKubeTopology(wl.kind, wl.namespace, wl.name) });
  items.push({ label: t('kube_describe', 'Describe'), run: () => kubeDescribe(wl.kind.toLowerCase(), wl.namespace, wl.name) });
  dockerFloatingMenu(e, 'kw_' + idx, items);
}

function openKubePodMenu(e, idx) {
  const p = S.kubePodsCache[idx];
  if (!p) return;
  const mode = getSelectedKubeMode();
  const isRo = mode === 'read-only';
  const noExec = isRo || mode === 'no-exec';
  const menuItems = [];
  if (!S.kubeIsCluster(S.selectedKubeHostId)) {
    menuItems.push({
      label: t('kube_exec', 'Exec Console'),
      disabled: noExec,
      title: noExec ? t('kube_mode_blocked_exec', 'Exec console is disabled in this mode') : '',
      run: () => promptKubeContainer(p, t('kube_exec', 'Exec Console'), c => kubePodExec(p, c))
    });
  }
  menuItems.push({ label: t('kube_logs', 'Pod Logs'), run: () => promptKubeContainer(p, t('kube_logs', 'Pod Logs'), c => openKubeLogs(p, c)) });
  menuItems.push({ label: t('kube_topology', 'Topology'), run: () => openKubeTopology('Pod', p.namespace, p.name) });
  menuItems.push({ label: t('kube_describe', 'Describe'), run: () => kubeDescribe('pod', p.namespace, p.name) });
  menuItems.push({
    label: t('kube_pf_action', 'Port Forward'),
    disabled: isRo,
    title: isRo ? t('kube_mode_blocked_ro', 'Disabled in Read-Only mode') : '',
    run: () => openKubePortForward('pod', p.namespace, p.name, '')
  });
  menuItems.push(null);
  menuItems.push({
    label: t('delete', 'Delete'),
    danger: true,
    disabled: isRo,
    title: isRo ? t('kube_mode_blocked_ro', 'Disabled in Read-Only mode') : '',
    run: () => kubeDeletePod(p)
  });
  dockerFloatingMenu(e, 'kp_' + idx, menuItems);
}

function openKubeServiceMenu(e, idx) {
  const s = S.kubeServicesCache[idx];
  if (!s) return;
  const ro = getSelectedKubeMode() === 'read-only';
  dockerFloatingMenu(e, 'ks_' + idx, [
    { label: t('kube_topology', 'Topology'), run: () => openKubeTopology('Service', s.namespace, s.name) },
    { label: t('kube_edit_yaml', 'Edit YAML'), run: () => openKubeYamlEditor('service', s.namespace, s.name) },
    { label: t('kube_describe', 'Describe'), run: () => kubeDescribe('service', s.namespace, s.name) },
    {
      label: t('kube_pf_action', 'Port Forward'),
      disabled: ro,
      title: ro ? t('kube_mode_blocked_ro', 'Disabled in Read-Only mode') : '',
      run: () => openKubePortForward('service', s.namespace, s.name, kubePortHint(s.ports))
    }
  ]);
}

// kubePortHint returns the Service port kubectl forwards first: "80/TCP" and "80:30080/TCP" both give 80.
function kubePortHint(ports) {
  const list = Array.isArray(ports) ? ports : String(ports || '').split(',');
  for (const p of list) {
    const m = /^\s*(\d+)/.exec(String(p));
    if (m) return m[1];
  }
  return '';
}

// openKubePortForward asks for the remote port of a Service or Pod, then starts a kubectl port-forward.
function openKubePortForward(kind, namespace, name, portHint) {
  const modal = ensureKubePortForwardModal();
  modal.dataset.kind = kind;
  modal.dataset.namespace = namespace || 'default';
  modal.dataset.name = name;
  document.getElementById('kubePfTitle').textContent = `${t('kube_pf_title', 'Port Forward')}: ${kind}/${name}`;
  document.getElementById('kubePfRemotePort').value = portHint || '';
  document.getElementById('kubePfLocalPort').value = '';
  openModal('kubePfModal');
}

function ensureKubePortForwardModal() {
  const existing = document.getElementById('kubePfModal');
  if (existing) return existing;
  const el = document.createElement('div');
  el.id = 'kubePfModal';
  el.className = 'modal';
  el.innerHTML = `
    <div class="modal-content" style="max-width: 460px;">
      <div class="modal-header">
        <h3 id="kubePfTitle" style="margin: 0; font-size: var(--fs-md); font-weight: 600;"></h3>
        <button class="btn btn-sm btn-icon btn-ghost" onclick="closeModal('kubePfModal')">${svgIcon('x')}</button>
      </div>
      <div class="modal-body" style="padding: 16px;">
        <div class="form-group">
          <label class="form-label" for="kubePfRemotePort">${escapeHtml(t('kube_pf_remote_port', 'Remote port'))}</label>
          <input type="number" id="kubePfRemotePort" class="input" min="1" max="65535" />
        </div>
        <div class="form-group">
          <label class="form-label" for="kubePfLocalPort">${escapeHtml(t('kube_pf_local_port', 'Local port'))}</label>
          <input type="number" id="kubePfLocalPort" class="input" min="0" max="65535" placeholder="${escapeHtml(t('kube_pf_local_ph', 'auto (first free port)'))}" />
        </div>
        <div class="form-hint">${escapeHtml(t('kube_pf_hint', 'Listens on the same address as Pantau. Read-only clusters refuse port forwarding.'))}</div>
        <div style="display: flex; justify-content: flex-end; margin-top: 12px;">
          <button class="btn btn-sm btn-primary" id="kubePfStartBtn" onclick="submitKubePortForward()">${svgIcon('plug', 'icon-sm')} <span>${escapeHtml(t('kube_pf_start', 'Start forward'))}</span></button>
        </div>
      </div>
    </div>`;
  document.body.appendChild(el);
  return el;
}

// submitKubePortForward starts the forward on the active host or cluster and lists it in Port Forwards.
async function submitKubePortForward() {
  const modal = document.getElementById('kubePfModal');
  if (!modal) return;
  const remotePort = parseInt(document.getElementById('kubePfRemotePort').value, 10);
  if (!remotePort || remotePort < 1 || remotePort > 65535) {
    showToast(t('kube_pf_bad_port', 'Remote port must be 1-65535'), 'error');
    return;
  }
  const localPort = parseInt(document.getElementById('kubePfLocalPort').value, 10) || 0;
  const btn = document.getElementById('kubePfStartBtn');
  if (btn) btn.disabled = true;
  try {
    const res = await fetch('/api/kube/forwards', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        target_id: S.selectedKubeHostId,
        kind: modal.dataset.kind,
        name: modal.dataset.name,
        namespace: modal.dataset.namespace,
        remote_port: remotePort,
        local_port: localPort
      })
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
      showToast(data.error || t('kube_pf_failed', 'Port forward failed'), 'error');
      return;
    }
    closeModal('kubePfModal');
    showToast(t('kube_pf_started', 'Forward started: 127.0.0.1:{port}').replace('{port}', data.local_port), 'success');
    if (window.PortFwd && typeof window.PortFwd.open === 'function') window.PortFwd.open(0);
  } catch (err) {
    showToast(t('kube_pf_failed', 'Port forward failed'), 'error');
  } finally {
    if (btn) btn.disabled = false;
  }
}

function openKubeScaleModal(wl) {
  S.currentScaleTarget = wl;
  document.getElementById('kubeScaleModalTitle').textContent = `${t('kube_scale_title')}: ${wl.name}`;
  document.getElementById('kubeScaleWorkloadDesc').textContent = `${wl.kind} • ${wl.namespace}/${wl.name} (Current desired: ${wl.desired || 1})`;
  document.getElementById('kubeScaleReplicas').value = wl.desired || 1;
  openModal('kubeScaleModal');
}

async function submitKubeScale() {
  if (!S.currentScaleTarget || !S.selectedKubeHostId) return;
  const repInput = document.getElementById('kubeScaleReplicas');
  const replicas = parseInt(repInput ? repInput.value : '1', 10);
  if (isNaN(replicas) || replicas < 0 || replicas > 1000) {
    showToast('Replicas must be between 0 and 1000', 'warning');
    return;
  }
  if (replicas === 0) {
    const isStatefulSet = (S.currentScaleTarget.kind || '').toLowerCase() === 'statefulset';
    const count = S.currentScaleTarget.desired || 1;
    const notePvc = isStatefulSet ? (' ' + t('kube_confirm_scale_0_pvc', 'PersistentVolumeClaims (PVCs) will be preserved.')) : '';
    const confirmed = await showConfirm({
      title: t('kube_confirm_scale_0_title', 'Scale to 0 Replicas'),
      message: t('kube_confirm_scale_0_msg', 'Scaling {name} to 0 will terminate {count} running pod(s).{pvc}')
        .replace('{name}', S.currentScaleTarget.name)
        .replace('{count}', count)
        .replace('{pvc}', notePvc),
      danger: true,
      confirmText: t('kube_btn_scale', 'Scale')
    });
    if (!confirmed) return;
  }
  const btn = document.getElementById('kubeScaleSubmitBtn');
  const restore = SP.busy(btn, t('kube_scaling', 'Scaling...'));
  try {
    const res = await kubeAction({ op: 'scale', kind: S.currentScaleTarget.kind.toLowerCase(), ns: S.currentScaleTarget.namespace, name: S.currentScaleTarget.name, replicas }, null, 'workloads');
    if (res && res.ok) closeModal('kubeScaleModal');
  } finally {
    restore();
  }
}

async function kubeDeletePod(pod) {
  if (!S.selectedKubeHostId) return;
  const warn = t('kube_confirm_delete_pod_unmanaged', 'If not managed by a workload controller, this pod will be permanently deleted.');
  const confirmed = await showConfirm({
    title: t('kube_delete_pod_title', 'Delete Pod'),
    message: t('kube_confirm_delete_pod_msg', 'Delete pod {name} on node {node} in namespace {ns}? {warn}')
      .replace('{name}', pod.name)
      .replace('{node}', pod.node || '-')
      .replace('{ns}', pod.namespace)
      .replace('{warn}', warn),
    danger: true,
    confirmText: t('delete', 'Delete')
  });
  if (!confirmed) return;
  kubeAction({ op: 'delete', kind: 'pod', ns: pod.namespace, name: pod.name }, null, 'pods');
}

function kubeDescribe(kind, ns, name) {
  kubeGetOutput(`${kubeApiBase()}/describe?kind=${encodeURIComponent(kind)}&ns=${encodeURIComponent(ns || '')}&name=${encodeURIComponent(name)}`, t('kube_describe', 'Describe') + ': ' + kind + '/' + name);
}

function promptKubeContainer(pod, title, onSelected) {
  const containers = Array.isArray(pod.containers) ? pod.containers : [];
  if (containers.length <= 1) {
    onSelected(containers[0] || '');
    return;
  }
  S.kubeContainerChoiceCallback = onSelected;
  document.getElementById('kubeContainerModalTitle').textContent = `${title}: ${pod.name}`;
  document.getElementById('kubeContainerModalDesc').textContent = `${pod.namespace}/${pod.name}`;
  const sel = document.getElementById('kubeContainerSelect');
  if (sel) {
    sel.innerHTML = containers.map((c, i) => `<option value="${escapeHtml(c)}" ${i === 0 ? 'selected' : ''}>${escapeHtml(c)}</option>`).join('');
  }
  openModal('kubeContainerModal');
}

function submitKubeContainerChoice() {
  const sel = document.getElementById('kubeContainerSelect');
  const val = sel ? sel.value : '';
  closeModal('kubeContainerModal');
  if (typeof S.kubeContainerChoiceCallback === 'function') {
    const cb = S.kubeContainerChoiceCallback;
    S.kubeContainerChoiceCallback = null;
    cb(val);
  }
}

async function kubePodExec(pod, container) {
  if (!S.selectedKubeHostId) return;
  const done = kubeBusy(t('kube_exec_running', 'Opening exec console on {name}...').replace('{name}', pod.name));
  try {
    const res = await fetch(`${kubeApiBase()}/exec-cmd?ns=${encodeURIComponent(pod.namespace)}&pod=${encodeURIComponent(pod.name)}&container=${encodeURIComponent(container || '')}`);
    const data = await res.json();
    handleKubeEnvelope(data);
    if (!data.ok || !data.cmd) {
      showToast(t('error_prefix', 'Error: ') + (data.error || 'Failed to get exec command'), 'error', 8000);
      return;
    }
    const targetHostId = data.host_id || S.selectedKubeHostId;
    const tab = openTerminalForHost(targetHostId, null, true, 'exec ' + pod.name);
    if (!tab) return;
    for (let i = 0; i < 60; i++) {
      if (tab.socketLeft && tab.socketLeft.readyState === WebSocket.OPEN) {
        setTimeout(() => sendTabCommand(tab.id, data.cmd), 400);
        return;
       }
      await new Promise(r => setTimeout(r, 200));
    }
    showToast(t('dk_console_timeout', 'Terminal connection timed out'), 'warning');
  } catch(e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error', 8000);
  } finally {
    done();
  }
}

function stopKubeLogs() {
  if (S.kubeLogsSocket) {
    try { S.kubeLogsSocket.close(); } catch(e) {}
    S.kubeLogsSocket = null;
  }
}

function openKubeLogs(pod, container) {
  stopKubeLogs();
  S.currentKubeLogsPod = pod;
  S.currentKubeLogsWorkload = null;
  S.currentKubeLogsContainer = container || ((pod.containers && pod.containers[0]) || '');
  const out = document.getElementById('kubeLogsOutput');
  const status = document.getElementById('kubeLogsStatus');
  const titleEl = document.getElementById('kubeLogsTitle');
  let hostName = S.selectedKubeHostId;
  if (S.kubeIsCluster(S.selectedKubeHostId)) {
    const c = (S.kubeClusters || []).find(x => x.id === -Number(S.selectedKubeHostId));
    hostName = c ? c.name : S.selectedKubeHostId;
  } else {
    const host = (S.allRawHosts || []).find(h => String(h.id) === String(S.selectedKubeHostId));
    hostName = host ? host.name : S.selectedKubeHostId;
  }
  if (titleEl) titleEl.textContent = `${t('kube_logs', 'Pod Logs')}: ${pod.name} @ ${hostName}`;
  if (out) out.textContent = '';
  if (status) {
    status.textContent = t('docker_logs_connecting', 'Connecting...');
    status.className = 'badge badge-info';
  }

  const cWrap = document.getElementById('kubeLogsContainerWrap');
  const cSel = document.getElementById('kubeLogsContainerSelect');
  const containers = Array.isArray(pod.containers) ? pod.containers : [];
  if (cWrap && cSel) {
    if (containers.length > 1) {
      cWrap.style.display = 'inline-flex';
      cSel.innerHTML = containers.map(c => `<option value="${escapeHtml(c)}" ${c === S.currentKubeLogsContainer ? 'selected' : ''}>${escapeHtml(c)}</option>`).join('');
    } else {
      cWrap.style.display = 'none';
      cSel.innerHTML = '';
    }
  }

  openModal('kubeLogsModal');
  connectKubeLogsStream();
}

function onKubeLogsParamChange() {
  if (!S.currentKubeLogsPod && !S.currentKubeLogsWorkload) return;
  const cSel = document.getElementById('kubeLogsContainerSelect');
  if (cSel && cSel.value) S.currentKubeLogsContainer = cSel.value;
  if (S.currentKubeLogsWorkload) {
    const prevToggle = document.getElementById('kubeLogsPreviousToggle');
    if (prevToggle && prevToggle.checked) {
      prevToggle.checked = false;
      showToast(t('kube_logs_previous_workload', 'Previous is not available for workload logs.'), 'warning');
      return;
    }
  }
  connectKubeLogsStream();
}

function connectKubeLogsStream() {
  stopKubeLogs();
  const pod = S.currentKubeLogsPod;
  const wl = S.currentKubeLogsWorkload;
  if ((!pod && !wl) || !S.selectedKubeHostId) return;
  // pod mode: ensure Previous toggle visible again
  const prevToggleEl = document.getElementById('kubeLogsPreviousToggle');
  if (pod && prevToggleEl && prevToggleEl.parentElement) prevToggleEl.parentElement.style.display = '';

  const out = document.getElementById('kubeLogsOutput');
  const status = document.getElementById('kubeLogsStatus');
  if (out) out.textContent = '';
  S.kubeLogsPartial = ''; S.kubeLogsMatches = 0;
  if (status) {
    status.textContent = t('docker_logs_connecting', 'Connecting...');
    status.className = 'badge badge-info';
  }

  const tailSel = document.getElementById('kubeLogsTailSelect');
  const tail = tailSel ? (tailSel.value || '200') : '200';
  const prevToggle = document.getElementById('kubeLogsPreviousToggle');
  const isPrevious = pod && prevToggle ? prevToggle.checked : false;
  const tsToggle = document.getElementById('kubeLogsTimestampsToggle');
  const isTimestamps = tsToggle ? tsToggle.checked : false;

  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  let url = `${protocol}//${location.host}/ws/kube/logs?${kubeWsParam()}&tail=${encodeURIComponent(tail)}`;
  if (pod) {
    url += `&ns=${encodeURIComponent(pod.namespace)}&pod=${encodeURIComponent(pod.name)}&container=${encodeURIComponent(S.currentKubeLogsContainer || '')}`;
  } else {
    url += `&ns=${encodeURIComponent(wl.namespace)}&workload=${encodeURIComponent(wl.kind + '/' + wl.name)}`;
  }
  if (isPrevious) url += '&previous=1';
  if (isTimestamps) url += '&timestamps=1';

  const ws = new WebSocket(url);
  ws.binaryType = 'arraybuffer';
  S.kubeLogsSocket = ws;
  const decoder = new TextDecoder('utf-8');
  let scrollQueued = false;

  ws.onopen = () => {
    if (S.kubeLogsSocket !== ws) return;
    status.textContent = isPrevious ? t('completed', 'Completed') : t('docker_logs_live', 'Live');
    status.className = isPrevious ? 'badge badge-info' : 'badge badge-healthy';
  };

  ws.onmessage = (ev) => {
    if (S.kubeLogsSocket !== ws) return;
    if (!document.getElementById('kubeLogsModal').classList.contains('open')) {
      stopKubeLogs();
      return;
    }
    let text = typeof ev.data === 'string' ? ev.data : decoder.decode(ev.data, { stream: true });
    text = text.replace(/\x1b\[[0-9;?]*[A-Za-z]/g, '');
    if (!text) return;
    appendKubeLogText(out, text);
    const autoScrollEl = document.getElementById('kubeLogsAutoScroll');
    if (autoScrollEl && autoScrollEl.checked && !scrollQueued) {
      scrollQueued = true;
      requestAnimationFrame(() => { scrollQueued = false; out.scrollTop = out.scrollHeight; });
    }
  };

  ws.onclose = () => {
    if (S.kubeLogsSocket !== ws) return;
    S.kubeLogsSocket = null;
    appendKubeLogText(out, '', true);
    status.textContent = t('docker_logs_ended', 'Ended');
    status.className = 'badge badge-unknown';
  };
}

function copyKubeLogs() {
  const out = document.getElementById('kubeLogsOutput');
  const txt = out ? out.textContent : '';
  if (!txt) return;
  copyTextToClipboard(txt);
  showToast(t('copied', 'Copied!'), 'success');
}

function kubeAutoRefreshIntervalSec() {
  const sel = document.getElementById('kubeAutorefreshSelect');
  const v = sel ? parseInt(sel.value, 10) : 0;
  return (!v || isNaN(v)) ? 0 : v;
}

function stopKubeAutorefresh() {
  if (S.kubeAutoRefreshTimer) { clearInterval(S.kubeAutoRefreshTimer); S.kubeAutoRefreshTimer = null; }
}

function restartKubeAutorefresh() {
  stopKubeAutorefresh();
  S.kubeAutoRefreshInFlight = false;
  if (!S.selectedKubeHostId || S.currentSideNav !== 'kube') return;
  const sec = kubeAutoRefreshIntervalSec();
  if (!sec || S.currentKubeTab === 'apply') return;
  S.kubeAutoRefreshTimer = setInterval(() => {
    if (document.hidden || S.kubeAutoRefreshInFlight || document.querySelector('.modal.open')) return;
    if (S.currentSideNav !== 'kube' || S.currentKubeTab === 'apply') { stopKubeAutorefresh(); return; }
    S.kubeAutoRefreshInFlight = true;
    Promise.resolve(loadKubeTabData(S.currentKubeTab)).catch(() => {}).finally(() => { S.kubeAutoRefreshInFlight = false; });
  }, sec * 1000);
}

document.addEventListener('visibilitychange', () => { if (!document.hidden) restartKubeAutorefresh(); });

function openKubeYamlEditor(kind, ns, name) {
  if (!S.kubeYamlEditable[kind]) return; // secret/pod/node/pv never offered
  S.kubeYamlTarget = { kind, ns, name };
  S.kubeYamlLastDiffed = null;
  document.getElementById('kubeYamlTitle').textContent = kind + '/' + (ns ? ns + '/' : '') + name;
  const isRo = getSelectedKubeMode() === 'read-only';
  const note = document.getElementById('kubeYamlRoNote');
  note.style.display = isRo ? 'flex' : 'none';
  if (isRo) document.getElementById('kubeYamlRoNoteText').textContent = t('kube_yaml_ro_note', 'Read-only mode: viewing is allowed but Apply is disabled.');
  const out = document.getElementById('kubeYamlOutput');
  out.style.display = 'none'; out.textContent = ''; out.className = 'kube-apply-output';
  const loading = document.getElementById('kubeYamlLoading');
  const editorWrap = document.getElementById('kubeYamlEditorWrap');
  loading.style.display = 'block';
  loading.innerHTML = SP.loading(t('kube_yaml_loading', 'Loading YAML...'));
  editorWrap.style.display = 'none';
  const btnDiff = document.getElementById('btnKubeYamlPreviewDiff');
  btnDiff.disabled = true;
  const btnApply = document.getElementById('btnKubeYamlApply');
  btnApply.disabled = true;
  btnApply.title = isRo ? t('kube_mode_blocked_ro', 'Disabled in Read-Only mode') : t('kube_apply_locked_hint', 'Run Preview Diff first');
  openModal('kubeYamlModal');
  fetch(`${kubeApiBase()}/yaml?kind=${encodeURIComponent(kind)}&ns=${encodeURIComponent(ns || '')}&name=${encodeURIComponent(name)}`)
    .then(r => r.json()).then(data => {
      handleKubeEnvelope(data);
      if (!data.ok) { showToast(t('error_prefix', 'Error: ') + (data.error || 'Failed to load YAML'), 'error', 9000); closeModal('kubeYamlModal'); return; }
      loading.style.display = 'none';
      editorWrap.style.display = '';
      btnDiff.disabled = false;
      initKubeYamlEditor(data.yaml || '');
    })
    .catch(e => { showToast(t('error_prefix', 'Error: ') + e.message, 'error', 9000); closeModal('kubeYamlModal'); });
}

function initKubeYamlEditor(content) {
  const ta = document.getElementById('kubeYamlEditorArea');
  if (!S.kubeYamlEditor) {
    S.kubeYamlEditor = CodeMirror.fromTextArea(ta, { lineNumbers: true, theme: 'vscode-dark', mode: 'yaml' });
    S.kubeYamlEditor.on('change', onKubeYamlEditorInput);
  }
  S.kubeYamlEditor.setValue(content || '');
  setTimeout(() => S.kubeYamlEditor.refresh(), 50);
  onKubeYamlEditorInput();
}

function onKubeYamlEditorInput() {
  const btn = document.getElementById('btnKubeYamlApply');
  if (!btn || !S.kubeYamlEditor) return;
  const isRo = getSelectedKubeMode() === 'read-only';
  const diffed = S.kubeYamlLastDiffed !== null && S.kubeYamlLastDiffed === S.kubeYamlEditor.getValue();
  btn.disabled = isRo || !diffed;
  btn.title = isRo ? t('kube_mode_blocked_ro', 'Disabled in Read-Only mode') : (diffed ? '' : t('kube_apply_locked_hint', 'Run Preview Diff first'));
}

async function submitKubeYamlDiff() {
  if (!S.kubeYamlEditor || !S.kubeYamlTarget || !S.selectedKubeHostId) return;
  const yaml = S.kubeYamlEditor.getValue().trim();
  if (!yaml) { showToast(t('kube_apply_empty', 'Please enter a YAML manifest'), 'warning'); return; }
  const btnDiff = document.getElementById('btnKubeYamlPreviewDiff');
  const out = document.getElementById('kubeYamlOutput');
  const restore = SP.busy(btnDiff, t('kube_diffing', 'Running dry-run diff...'));
  out.style.display = 'block';
  out.className = 'kube-apply-output';
  out.innerHTML = SP.html(t('kube_diffing', 'Running dry-run diff...'));
  try {
    const res = await fetch(`${kubeApiBase()}/diff`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ yaml }) });
    const data = await res.json();
    handleKubeEnvelope(data);
    if (data.ok) {
      if (data.changed) { out.textContent = data.output || '(Differences detected)'; }
      else { out.textContent = t('kube_diff_no_changes', 'No changes detected. Cluster state matches manifest.'); out.className = 'kube-apply-output success'; }
      S.kubeYamlLastDiffed = S.kubeYamlEditor.getValue();
    } else {
      out.textContent = 'Error: ' + (data.error || 'Diff failed');
      out.className = 'kube-apply-output error';
      S.kubeYamlLastDiffed = null;
    }
  } catch(e) {
    out.textContent = 'Error: ' + e.message;
    out.className = 'kube-apply-output error';
    S.kubeYamlLastDiffed = null;
  } finally {
    restore();
    onKubeYamlEditorInput();
  }
}

async function submitKubeYamlApply() {
  if (!S.kubeYamlEditor || !S.kubeYamlTarget || !S.selectedKubeHostId) return;
  const isRo = getSelectedKubeMode() === 'read-only';
  if (isRo) { showToast(t('kube_mode_blocked_ro', 'Disabled in Read-Only mode'), 'warning'); return; }
  const currentYaml = S.kubeYamlEditor.getValue();
  const yaml = currentYaml.trim();
  if (!yaml) return;
  if (S.kubeYamlLastDiffed === null || S.kubeYamlLastDiffed !== currentYaml) {
    showToast(t('kube_apply_diff_needed', 'Manifest has changed. Please preview diff before applying.'), 'warning');
    return;
  }
  const confirmed = await showConfirm({
    title: t('kube_confirm_apply_title', 'Apply Manifest'),
    message: t('kube_confirm_apply_msg', 'Are you sure you want to apply this manifest to the cluster?'),
    danger: true,
    confirmText: t('kube_btn_apply', 'Apply YAML')
  });
  if (!confirmed) return;
  const btn = document.getElementById('btnKubeYamlApply');
  const out = document.getElementById('kubeYamlOutput');
  const restore = SP.busy(btn, t('kube_applying', 'Applying manifest...'));
  try {
    const res = await fetch(`${kubeApiBase()}/apply`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ yaml }) });
    const data = await res.json();
    handleKubeEnvelope(data);
    out.style.display = 'block';
    out.className = 'kube-apply-output ' + (data.ok ? 'success' : 'error');
    out.textContent = data.output || (data.ok ? t('kube_apply_success', 'Manifest applied successfully') : ('Error: ' + (data.error || 'Apply failed')));
    if (data.ok) {
      showToast(t('kube_apply_success', 'Manifest applied successfully'), 'success');
      S.kubeYamlLastDiffed = null;
      loadKubeTabData(S.currentKubeTab);
    } else {
      showToast(t('error_prefix', 'Error: ') + (data.error || 'Apply failed'), 'error', 9000);
      if ((data.error || '').toLowerCase().includes('object has been modified')) {
        out.textContent += '\n\n' + t('kube_yaml_conflict_hint', 'The object changed since it was opened (resourceVersion conflict). Re-open the YAML to get the latest version.');
      }
    }
  } catch(e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error', 9000);
    out.style.display = 'block';
    out.className = 'kube-apply-output error';
    out.textContent = 'Error: ' + e.message;
  } finally {
    restore();
    onKubeYamlEditorInput();
  }
}

function setKubeLogsCount(q) {
  const cnt = document.getElementById('kubeLogsMatchCount');
  if (cnt) cnt.textContent = q ? t('kube_logs_matches', '{n} match(es)').replace('{n}', String(S.kubeLogsMatches)) : '';
}

function appendKubeLogText(out, text, flush) {
  const q = (document.getElementById('kubeLogsFilterInput').value || '').toLowerCase();
  const parts = (S.kubeLogsPartial + text).split('\n');
  S.kubeLogsPartial = parts.pop();
  if (flush && S.kubeLogsPartial) { parts.push(S.kubeLogsPartial); S.kubeLogsPartial = ''; }
  const frag = document.createDocumentFragment();
  for (const line of parts) {
    const s = document.createElement('span');
    s.textContent = line + '\n';
    if (q && !line.toLowerCase().includes(q)) s.hidden = true; else S.kubeLogsMatches++;
    frag.appendChild(s);
  }
  out.appendChild(frag);
  while (out.childElementCount > S.kubeLogsMaxLines) {
    const f = out.firstChild;
    if (!f.hidden) S.kubeLogsMatches--;
    out.removeChild(f);
  }
  setKubeLogsCount(q);
}

function onKubeLogsFilterInput() {
  const out = document.getElementById('kubeLogsOutput');
  if (!out) return;
  const q = (document.getElementById('kubeLogsFilterInput').value || '').toLowerCase();
  S.kubeLogsMatches = 0;
  for (const s of out.children) {
    s.hidden = !!q && !s.textContent.toLowerCase().includes(q);
    if (!s.hidden) S.kubeLogsMatches++;
  }
  setKubeLogsCount(q);
}

function openKubeWorkloadLogs(wl) {
  stopKubeLogs();
  S.currentKubeLogsPod = null;
  S.currentKubeLogsContainer = null;
  S.currentKubeLogsWorkload = { kind: (wl.kind || '').toLowerCase(), name: wl.name, namespace: wl.namespace };
  let hostName = S.selectedKubeHostId;
  if (S.kubeIsCluster(S.selectedKubeHostId)) {
    const c = (S.kubeClusters || []).find(x => x.id === -Number(S.selectedKubeHostId));
    hostName = c ? c.name : S.selectedKubeHostId;
  } else {
    const host = (S.allRawHosts || []).find(h => String(h.id) === String(S.selectedKubeHostId));
    hostName = host ? host.name : S.selectedKubeHostId;
  }
  const titleEl = document.getElementById('kubeLogsTitle');
  if (titleEl) titleEl.textContent = `${t('kube_logs_workload', 'Workload Logs')}: ${wl.kind}/${wl.name} @ ${hostName}`;
  const out = document.getElementById('kubeLogsOutput');
  if (out) out.textContent = '';
  const status = document.getElementById('kubeLogsStatus');
  if (status) { status.textContent = t('docker_logs_connecting', 'Connecting...'); status.className = 'badge badge-info'; }
  const cWrap = document.getElementById('kubeLogsContainerWrap');
  if (cWrap) { cWrap.style.display = 'none'; }
  const prevWrap = document.getElementById('kubeLogsPreviousToggle');
  if (prevWrap && prevWrap.parentElement) { prevWrap.checked = false; prevWrap.parentElement.style.display = 'none'; }
  openModal('kubeLogsModal');
  connectKubeLogsStream();
}

function openKubeDrain(nodeName) {
  if (!S.selectedKubeHostId) return;
  const isRo = getSelectedKubeMode() === 'read-only';
  if (isRo) { showToast(t('kube_mode_blocked_ro', 'Disabled in Read-Only mode'), 'warning'); return; }
  S.kubeDrainTarget = nodeName;
  S.kubeDrainPreviewData = null;
  const summary = document.getElementById('kubeDrainSummary');
  summary.innerHTML = SP.loading(t('kube_drain_previewing', 'Running drain preview (dry-run)...'));
  const podsEl = document.getElementById('kubeDrainPods');
  podsEl.style.display = 'none'; podsEl.textContent = '';
  const note = document.getElementById('kubeDrainBlockedNote');
  note.style.display = 'none'; note.textContent = '';
  document.getElementById('btnKubeDrainConfirm').disabled = true;
  document.getElementById('kubeDrainFlagIgnoreDS').checked = true;
  document.getElementById('kubeDrainFlagEmptyDir').checked = false;
  document.getElementById('kubeDrainFlagForce').checked = false;
  openModal('kubeDrainModal');
  fetch(`${kubeApiBase()}/drain-preview`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: nodeName }) })
    .then(r => r.json()).then(data => {
      handleKubeEnvelope(data);
      if (!data.ok) { summary.textContent = t('error_prefix', 'Error: ') + (data.error || 'Failed to load preview'); return; }
      S.kubeDrainPreviewData = data;
      renderKubeDrainPreview();
    })
    .catch(e => { summary.textContent = t('error_prefix', 'Error: ') + e.message; });
}

function renderKubeDrainPreview() {
  const d = S.kubeDrainPreviewData;
  if (!d) return;
  const pods = d.pods || [];
  const blocked = d.blocked || [];
  const ds = d.daemonset_skipped || [];
  const lines = pods.map(p => (p.ns ? p.ns + '/' : '') + p.name);
  if (ds.length) lines.push(t('kube_drain_daemonset_skipped', 'DaemonSet-managed pods (skipped): {list}').replace('{list}', ds.join(', ')));
  const podsEl = document.getElementById('kubeDrainPods');
  if (lines.length) { podsEl.style.display = 'block'; podsEl.textContent = lines.join('\n'); }
  else podsEl.style.display = 'none';
  document.getElementById('kubeDrainSummary').textContent = t('kube_drain_pods_header', '{n} pod(s) will be evicted and rescheduled:').replace('{n}', String(d.pod_count != null ? d.pod_count : pods.length));
  const note = document.getElementById('kubeDrainBlockedNote');
  if (blocked.length) {
    note.style.display = 'block';
    note.textContent = t('kube_drain_blocked_note', '{n} pod(s) blocked by PodDisruptionBudget; the drain may run until the timeout.').replace('{n}', String(blocked.length)) + '\n' + blocked.join('\n');
  }
  document.getElementById('btnKubeDrainConfirm').disabled = false;
}

async function confirmKubeDrain() {
  const nodeName = S.kubeDrainTarget;
  const preview = S.kubeDrainPreviewData;
  if (!nodeName || !S.selectedKubeHostId) return;
  const podCount = preview ? (preview.pod_count != null ? preview.pod_count : (preview.pods || []).length) : 0;
  const blockedCount = preview ? (preview.blocked || []).length : 0;
  const ignoreDS = document.getElementById('kubeDrainFlagIgnoreDS').checked;
  const emptyDir = document.getElementById('kubeDrainFlagEmptyDir').checked;
  const force = document.getElementById('kubeDrainFlagForce').checked;
  let msg = t('kube_drain_confirm_base', 'Drain node {name}? {count} pod(s) will be evicted and rescheduled; single-replica workloads will experience downtime.').replace('{name}', nodeName).replace('{count}', String(podCount));
  if (emptyDir) msg += '\n' + t('kube_drain_confirm_emptydir', 'EmptyDir data will be lost permanently.');
  if (force) msg += '\n' + t('kube_drain_confirm_force', 'Pods without a controller may not come back.');
  const node = (S.kubeNodesCache || []).find(n => n.name === nodeName);
  const roles = node && Array.isArray(node.roles) ? node.roles.join(',').toLowerCase() : String(node && node.roles || '').toLowerCase();
  if (roles.includes('control-plane') || roles.includes('master') || roles.includes('etcd')) {
    msg += '\n' + t('kube_drain_confirm_etcd', '{name} is a control-plane and etcd member of the HA cluster: draining removes one etcd vote; if one other node fails during the drain, quorum is lost and all workloads stop.').replace('{name}', nodeName);
  }
  if (blockedCount > 0) msg += '\n' + t('kube_drain_confirm_blocked', '{n} pod(s) are blocked by PDB; the drain may run until the timeout.').replace('{n}', String(blockedCount));
  const confirmed = await showConfirm({
    title: t('kube_drain_confirm_title', 'Drain Node'),
    message: msg,
    danger: true,
    confirmText: t('kube_drain', 'Drain')
  });
  if (!confirmed) return;
  const btn = document.getElementById('btnKubeDrainConfirm');
  const restore = SP.busy(btn, t('kube_drain_starting', 'Starting drain...'));
  closeModal('kubeDrainModal');
  const done = kubeBusy(t('kube_drain_running', 'Draining node {name}... this can take minutes').replace('{name}', nodeName));
  try {
    const res = await fetch(`${kubeApiBase()}/action`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ op: 'drain', kind: 'node', ns: '', name: nodeName, ignoreDaemonsets: ignoreDS, deleteEmptyDirData: emptyDir, force })
    });
    const data = await res.json();
    handleKubeEnvelope(data);
    if (data.ok) {
      showToastDrainUncordon(nodeName, t('kube_drain_done', 'Drain completed'), 'success');
      if (data.output) showDockerOutput('drain ' + nodeName, data.output);
    } else {
      const err = data.error || 'Drain failed';
      showToast(t('error_prefix', 'Error: ') + err, 'error', 9000);
      if (/timeout/i.test(err)) showToastDrainUncordon(nodeName, t('kube_drain_timeout', 'Drain timeout: the node stays cordoned and pods may be partially evicted. Run Uncordon to undo.'), 'warning');
      return;
    }
    loadKubeTabData('nodes');
  } catch(e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error', 9000);
  } finally {
    done();
    restore();
  }
}

function showToastDrainUncordon(nodeName, msg, type) {
  if (msg) showToast(msg, type || 'success', 8000);
  const container = document.getElementById('toastContainer');
  if (!container) return;
  const last = container.lastElementChild;
  if (!last || !last.classList.contains('toast')) return;
  const btn = document.createElement('button');
  btn.type = 'button';
  btn.className = 'btn btn-sm';
  btn.style.margin = '0 6px 0 10px';
  btn.textContent = t('kube_uncordon', 'Uncordon Node');
  btn.onclick = () => { last.remove(); kubeAction({ op: 'uncordon', kind: 'node', ns: '', name: nodeName }, null, 'nodes'); };
  const msgEl = last.querySelector('.toast-msg');
  if (msgEl) msgEl.appendChild(btn);
}

function setKubeClusterError(msg) {
  const errEl = document.getElementById('kubeClusterError');
  if (!errEl) return;
  if (msg) {
    errEl.textContent = msg;
    errEl.style.display = 'block';
  } else {
    errEl.textContent = '';
    errEl.style.display = 'none';
  }
}

function openKubeClusterModal(clusterHostId) {
  setKubeClusterError('');
  const titleEl = document.getElementById('kubeClusterModalTitle');
  const configArea = document.getElementById('kubeClusterConfig');
  const fileInput = document.getElementById('kubeClusterFileInput');
  const contextSel = document.getElementById('kubeClusterContext');
  const nameInput = document.getElementById('kubeClusterName');
  const modeSel = document.getElementById('kubeClusterMode');
  const reqStar = document.getElementById('kubeClusterConfigReqStar');
  const submitBtn = document.getElementById('btnKubeClusterSubmit');
  const submitText = document.getElementById('btnKubeClusterSubmitText');
  if (fileInput) fileInput.value = '';
  if (configArea) configArea.value = '';
  if (nameInput) { nameInput.value = ''; nameInput.oninput = () => { S.kubeClusterUserEditedName = true; }; }
  if (modeSel) modeSel.value = 'full';
  if (contextSel) { contextSel.innerHTML = ''; contextSel.disabled = true; }
  if (submitBtn) submitBtn.disabled = false;
  S.kubeClusterUserEditedName = false;

  const isEdit = clusterHostId != null && (Number(clusterHostId) < 0 || typeof clusterHostId === 'number');
  const rawId = isEdit ? Math.abs(Number(clusterHostId)) : null;
  const existing = rawId ? (S.kubeClusters || []).find(c => c.id === rawId) : null;

  if (isEdit && existing) {
    S.kubeClusterModalMode = 'edit';
    S.kubeClusterEditId = rawId;
    if (titleEl) titleEl.textContent = t('kube_ext_modal_title_edit', 'Edit External Cluster');
    if (reqStar) reqStar.style.display = 'none';
    if (submitText) submitText.textContent = t('kube_ext_btn_save', 'Save');
    if (nameInput) nameInput.value = existing.name || '';
    if (modeSel) modeSel.value = existing.kube_mode || 'full';
    if (contextSel) {
      const ctx = existing.context || '';
      contextSel.innerHTML = `<option value="${escapeHtml(ctx)}">${escapeHtml(ctx)} ${t('kube_ext_context_saved_note', '(saved context)')}</option>`;
      contextSel.disabled = false;
    }
  } else {
    S.kubeClusterModalMode = 'add';
    S.kubeClusterEditId = null;
    if (titleEl) titleEl.textContent = t('kube_ext_modal_title_add', 'Add External Cluster');
    if (reqStar) reqStar.style.display = 'inline';
    if (submitText) submitText.textContent = t('kube_ext_btn_test_save', 'Test & Save');
  }

  openModal('kubeClusterModal');
  setTimeout(() => { if (configArea) configArea.focus(); }, 50);
}

function loadKubeClusterFile(ev) {
  const file = ev && ev.target && ev.target.files && ev.target.files[0];
  if (!file) return;
  const reader = new FileReader();
  reader.onload = e => {
    const text = e.target.result;
    const configArea = document.getElementById('kubeClusterConfig');
    if (configArea) configArea.value = text;
    loadKubeClusterContexts();
  };
  reader.readAsText(file);
}

async function loadKubeClusterContexts() {
  const configArea = document.getElementById('kubeClusterConfig');
  const yaml = (configArea ? configArea.value : '').trim();
  if (!yaml) {
    setKubeClusterError(t('kube_ext_err_no_kubeconfig', 'Please paste or load a kubeconfig first'));
    return;
  }
  setKubeClusterError('');
  const btn = document.getElementById('btnKubeClusterLoadContexts');
  const origBtnHtml = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = svgIcon('refresh-cw', 'icon-xs') + SP.html(t('kube_ext_loading_contexts', 'Loading contexts...')); }
  try {
    const res = await fetch('/api/kube/clusters/contexts', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ kubeconfig: yaml })
    });
    const data = await res.json();
    if (!res.ok || !data.ok) {
      setKubeClusterError(data.error || 'Failed to parse kubeconfig contexts');
      return;
    }
    const contexts = Array.isArray(data.contexts) ? data.contexts : [];
    const contextSel = document.getElementById('kubeClusterContext');
    if (!contextSel) return;
    if (contexts.length === 0) {
      contextSel.innerHTML = `<option value="">${t('kube_ext_err_no_contexts', 'No contexts found in kubeconfig')}</option>`;
      contextSel.disabled = true;
      return;
    }
    contextSel.innerHTML = contexts.map(c => `<option value="${escapeHtml(c)}">${escapeHtml(c)}</option>`).join('');
    contextSel.disabled = false;
    if (data.current_context && contexts.includes(data.current_context)) {
      contextSel.value = data.current_context;
    } else {
      contextSel.value = contexts[0];
    }
    onKubeClusterContextChange();
  } catch (e) {
    setKubeClusterError(e.message);
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = origBtnHtml; }
  }
}

function onKubeClusterContextChange() {
  const contextSel = document.getElementById('kubeClusterContext');
  const nameInput = document.getElementById('kubeClusterName');
  if (!contextSel || !nameInput) return;
  const chosen = contextSel.value;
  if (chosen && (!nameInput.value.trim() || !S.kubeClusterUserEditedName)) {
    nameInput.value = chosen;
  }
}

async function submitKubeClusterModal() {
  setKubeClusterError('');
  const configArea = document.getElementById('kubeClusterConfig');
  const contextSel = document.getElementById('kubeClusterContext');
  const nameInput = document.getElementById('kubeClusterName');
  const modeSel = document.getElementById('kubeClusterMode');
  const submitBtn = document.getElementById('btnKubeClusterSubmit');

  const kubeconfig = (configArea ? configArea.value : '').trim();
  const name = (nameInput ? nameInput.value : '').trim();
  const context = (contextSel ? contextSel.value : '').trim();
  const kube_mode = (modeSel ? modeSel.value : 'full') || 'full';

  if (!name) {
    setKubeClusterError(t('kube_ext_err_no_name', 'Please enter a display name'));
    if (nameInput) nameInput.focus();
    return;
  }

  if (S.kubeClusterModalMode === 'add') {
    if (!kubeconfig) {
      setKubeClusterError(t('kube_ext_err_no_kubeconfig', 'Please paste or load a kubeconfig first'));
      if (configArea) configArea.focus();
      return;
    }
    if (!context) {
      setKubeClusterError(t('kube_ext_err_no_context_selected', 'Please select a context'));
      return;
    }
  }

  const restore = SP.busy(submitBtn, S.kubeClusterModalMode === 'add' ? t('kube_ext_verifying', 'Verifying...') : t('kube_ext_saving', 'Saving...'));

  try {
    let url = '/api/kube/clusters';
    let method = 'POST';
    let body = { name, kube_mode };

    if (S.kubeClusterModalMode === 'add') {
      body.kubeconfig = kubeconfig;
      body.context = context;
    } else {
      url = `/api/kube/clusters/${S.kubeClusterEditId}`;
      method = 'PUT';
      if (kubeconfig) {
        body.kubeconfig = kubeconfig;
        if (context) body.context = context;
      }
    }

    const res = await fetch(url, {
      method,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    });
    const data = await res.json();

    if (!res.ok || !data.ok) {
      setKubeClusterError(data.error || 'Server rejected cluster configuration');
      return;
    }

    closeModal('kubeClusterModal');
    showToast(t('kube_ext_saved', 'Cluster saved successfully'), 'success');

    await loadKubeClusters();
    const savedId = (data.cluster && data.cluster.id) || S.kubeClusterEditId;
    if (savedId) {
      delete S.kubeDetected[-savedId]; // the kubeconfig may have changed
      pickKubeHost(-savedId);
    }
  } catch (e) {
    setKubeClusterError(e.message);
  } finally {
    restore();
  }
}

function editKubeCurrentCluster() {
  if (S.kubeIsCluster(S.selectedKubeHostId)) openKubeClusterModal(S.selectedKubeHostId);
  else openKubeHostClusterModal();
}

function currentKubeHostClusterId() {
  if (S.kubeIsCluster(S.selectedKubeHostId)) return '';
  const h = (S.allRawHosts || []).find(x => String(x.id) === String(S.selectedKubeHostId));
  return ((h && h.kube_cluster_id) || '').trim();
}

function openKubeHostClusterModal() {
  const cid = currentKubeHostClusterId();
  if (!cid) return;
  S.kubeHostClusterEditId = cid;
  const c = (S.kubeHostClusters || []).find(x => x.cluster_id === cid);
  const input = document.getElementById('kubeHostClusterName');
  input.value = c && c.custom ? c.name : '';
  document.getElementById('kubeHostClusterError').style.display = 'none';
  openModal('kubeHostClusterModal');
  setTimeout(() => input.focus(), 50);
}

async function submitKubeHostClusterModal() {
  const errEl = document.getElementById('kubeHostClusterError');
  errEl.style.display = 'none';
  try {
    const res = await fetch(`/api/kube/host-clusters/${encodeURIComponent(S.kubeHostClusterEditId)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: document.getElementById('kubeHostClusterName').value.trim() })
    });
    const data = await res.json();
    if (!res.ok || !data.ok) {
      errEl.textContent = data.error || 'Failed to rename cluster';
      errEl.style.display = 'block';
      return;
    }
    closeModal('kubeHostClusterModal');
    showToast(t('kube_ext_saved', 'Cluster saved successfully'), 'success');
    await loadKubeClusters();
  } catch (e) {
    errEl.textContent = e.message;
    errEl.style.display = 'block';
  }
}

async function deleteKubeCluster(clusterHostId) {
  const rawId = clusterHostId != null ? Math.abs(Number(clusterHostId)) : null;
  if (!rawId) return;
  const cluster = (S.kubeClusters || []).find(c => c.id === rawId);
  const clusterName = cluster ? cluster.name : ('Cluster #' + rawId);

  const confirmed = await showConfirm({
    title: t('kube_ext_confirm_delete_title', 'Delete External Cluster'),
    message: t('kube_ext_confirm_delete_msg', 'Are you sure you want to delete cluster "{name}"? This removes stored credentials and access configuration.').replace('{name}', clusterName),
    confirmText: t('delete', 'Delete'),
    danger: true
  });
  if (!confirmed) return;

  try {
    const res = await fetch(`/api/kube/clusters/${rawId}`, { method: 'DELETE' });
    const data = await res.json();
    if (!res.ok || !data.ok) {
      showToast(t('error_prefix', 'Error: ') + (data.error || 'Failed to delete cluster'), 'error', 6000);
      return;
    }
    showToast(t('kube_ext_deleted', 'Cluster deleted'), 'info');
    await loadKubeClusters();
    closeKubeWorkspace(-Number(rawId));
    updateKubeModeChip();
  } catch (e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error', 6000);
  }
}

/* kubectl CLI manager: the kubectl External Cluster operations run with. Checking versions only reads.
 * Installing, switching, reverting and removing change the server's kubectl, so each one asks first. */
const kubeCli = { data: null, busy: false };
const KUBE_CLI_VERSION_RE = /^\d+\.\d+\.\d+$/;

function openKubeCliModal() {
  openModal('kubeCliModal');
  checkKubeCli();
}

async function kubeCliRequest(method, url, body) {
  const res = await fetch(url, {
    method,
    headers: body ? { 'Content-Type': 'application/json' } : {},
    body: body ? JSON.stringify(body) : undefined,
    credentials: 'same-origin',
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || ('HTTP ' + res.status));
  return data;
}

// checkKubeCli reads the update check. A failed request is a failed check, never "up to date".
async function checkKubeCli() {
  if (kubeCli.busy) return;
  kubeCli.busy = true;
  renderKubeCli();
  try {
    kubeCli.data = await kubeCliRequest('GET', '/api/kube/cli/update-check');
  } catch (e) {
    kubeCli.data = { status: 'error', error: e.message, managed_versions: [], channels: [] };
  } finally {
    kubeCli.busy = false;
    renderKubeCli();
  }
}

function renderKubeCli() {
  const busy = kubeCli.busy;
  const d = kubeCli.data;
  const chip = document.getElementById('kubeCliChip');
  if (chip) {
    const text = d && d.installed ? 'kubectl ' + d.installed : '';
    chip.textContent = text;
    chip.style.display = text ? '' : 'none';
  }
  const status = document.getElementById('kubeCliStatus');
  if (!status) return;

  const list = document.getElementById('kubeCliVersionList');
  if (list && !list.dataset.wired) {
    list.dataset.wired = '1';
    list.addEventListener('click', onKubeCliListClick);
  }
  const setDisabled = (id, off) => { const el = document.getElementById(id); if (el) el.disabled = off; };
  setDisabled('kubeCliCheckBtn', busy);
  setDisabled('kubeCliSwitchBtn', busy);

  if (!d) {
    status.innerHTML = busy ? '<span class="text-muted">' + escapeHtml(t('kube_cli_checking', 'Checking...')) + '</span>' : '';
    setDisabled('kubeCliSystemBtn', true);
    return;
  }

  const labels = {
    update: ['badge-warning', 'kube_cli_status_update', 'Update available'],
    current: ['badge-healthy', 'kube_cli_status_current', 'Up to date'],
    error: ['badge-danger', 'kube_cli_status_error', 'Check failed'],
  };
  const [cls, key, fallback] = labels[d.status] || ['badge-danger', 'kube_cli_status_unknown', 'Version unknown'];
  const source = { managed: t('kube_cli_source_managed', 'Managed'), system: t('kube_cli_source_system', 'System') }[d.source]
    || t('kube_cli_source_missing', 'Missing');
  const row = (label, value) => '<div style="display:flex;gap:8px;flex-wrap:wrap;"><span class="text-muted" style="min-width:110px;">' + escapeHtml(label) +
    '</span><span style="font-family:var(--font-mono, monospace);word-break:break-all;">' + escapeHtml(value || '—') + '</span></div>';
  const err = d.error ? '<div style="color:var(--danger);margin-top:4px;">' + escapeHtml(d.error) + '</div>' : '';
  const release = d.release_url ? '<div style="margin-top:4px;"><a href="' + escapeHtml(d.release_url) +
    '" target="_blank" rel="noopener noreferrer">' + escapeHtml(t('kube_cli_release', 'Release notes')) + '</a></div>' : '';
  status.innerHTML = '<div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;margin-bottom:6px;">' +
    '<b style="font-family:var(--font-mono, monospace);">kubectl ' + escapeHtml(d.installed || '—') + '</b>' +
    '<span class="badge ' + cls + '">' + escapeHtml(t(key, fallback)) + '</span>' +
    '<span class="badge">' + escapeHtml(source) + '</span></div>' +
    row(t('kube_cli_latest', 'Latest'), d.latest) +
    row(t('kube_cli_path', 'Path'), d.path) +
    row(t('kube_cli_system', 'System copy'), d.system_installed) + err + release;

  const up = document.getElementById('kubeCliUpdateBtn');
  if (up) {
    up.style.display = d.status === 'update' && d.latest ? 'inline-flex' : 'none';
    up.disabled = busy;
    const lbl = document.getElementById('kubeCliUpdateLabel');
    if (lbl) lbl.textContent = t('kube_cli_update_to', 'Update to {v}').replace('{v}', d.latest || '');
  }

  const sel = document.getElementById('kubeCliVersionSelect');
  if (sel) {
    const seen = new Set();
    const opts = [];
    (d.channels || []).forEach(c => {
      if (seen.has(c.latest)) return;
      seen.add(c.latest);
      opts.push('<option value="' + escapeHtml(c.latest) + '">' + escapeHtml(c.latest + ' (' + c.minor + '.x)') + '</option>');
    });
    (d.managed_versions || []).forEach(v => {
      if (seen.has(v)) return;
      seen.add(v);
      opts.push('<option value="' + escapeHtml(v) + '">' + escapeHtml(v + ' [' + t('kube_cli_cached_tag', 'cached') + ']') + '</option>');
    });
    sel.innerHTML = '<option value="">' + escapeHtml(t('kube_cli_pick', 'Pick a version...')) + '</option>' + opts.join('');
    sel.disabled = busy;
  }

  if (list) {
    const versions = d.managed_versions || [];
    list.innerHTML = versions.length
      ? versions.map(v => {
        const active = v === d.active_managed_version;
        return '<div style="display:flex;align-items:center;gap:8px;padding:6px 0;border-bottom:1px solid var(--card-border);">' +
          '<span style="font-family:var(--font-mono, monospace);">' + escapeHtml(v) + '</span>' +
          (active ? '<span class="badge badge-healthy">' + escapeHtml(t('kube_cli_active_badge', 'Active')) + '</span>' : '') +
          '<span style="flex:1;"></span>' +
          (active ? '' : '<button class="btn btn-sm" data-kube-cli-activate="' + escapeHtml(v) + '"' + (busy ? ' disabled' : '') + '>' +
            escapeHtml(t('kube_cli_activate', 'Activate')) + '</button>') +
          '<button class="btn btn-sm btn-ghost" data-kube-cli-remove="' + escapeHtml(v) + '" title="' + escapeHtml(t('kube_cli_remove', 'Remove')) +
          '" aria-label="' + escapeHtml(t('kube_cli_remove', 'Remove')) + '"' + (active || busy ? ' disabled' : '') + '>' +
          '<svg class="icon icon-sm"><use href="#i-trash-2"/></svg></button></div>';
      }).join('')
      : '<div class="text-muted">' + escapeHtml(t('kube_cli_none', 'No cached versions yet.')) + '</div>';
  }

  // The system copy is only a revert target while a managed version is in use.
  setDisabled('kubeCliSystemBtn', busy || !(d.source === 'managed' && d.system_installed));
}

function onKubeCliListClick(e) {
  const act = e.target.closest('[data-kube-cli-activate]');
  if (act) { switchKubeCli(act.dataset.kubeCliActivate); return; }
  const rm = e.target.closest('[data-kube-cli-remove]');
  if (rm) deleteKubeCli(rm.dataset.kubeCliRemove);
}

// kubeCliStream runs an install or a switch. The server streams its steps as NDJSON.
async function kubeCliStream(url, body) {
  const out = document.getElementById('kubeCliInstallLog');
  if (out) { out.hidden = false; out.textContent = ''; }
  kubeCli.busy = true;
  renderKubeCli();
  let error = '';
  try {
    const res = await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body), credentials: 'same-origin' });
    if (!(res.headers.get('content-type') || '').includes('application/x-ndjson')) {
      const d = await res.json().catch(() => ({}));
      error = d.error || t('kube_cli_failed', 'kubectl change failed');
    } else {
      const reader = res.body.getReader();
      const dec = new TextDecoder();
      let buf = '';
      const take = (line) => {
        let m;
        try { m = JSON.parse(line); } catch (e) { return; }
        if (m.log && out) { out.textContent += m.log; out.scrollTop = out.scrollHeight; }
        if (m.done) error = m.error || '';
      };
      for (;;) {
        const r = await reader.read();
        if (r.done) break;
        buf += dec.decode(r.value, { stream: true });
        let nl;
        while ((nl = buf.indexOf('\n')) >= 0) { take(buf.slice(0, nl)); buf = buf.slice(nl + 1); }
      }
      if (buf.trim()) take(buf);
    }
  } catch (e) { error = e.message; }

  if (error) {
    if (out) out.textContent += '\n' + error;
    showToast(t('kube_cli_failed', 'kubectl change failed') + ': ' + error, 'error', 6000);
  } else {
    showToast(t('kube_cli_done', 'kubectl updated'), 'success');
  }
  kubeCli.busy = false;
  await checkKubeCli();
  return !error;
}

// installKubeCli downloads and activates a release. Without a version it is the newest stable one.
async function installKubeCli(version) {
  if (kubeCli.busy) return;
  const target = version || (kubeCli.data && kubeCli.data.latest) || '';
  const ok = await showConfirm({
    title: t('kube_cli_confirm_update_title', 'Update kubectl?'),
    message: t('kube_cli_confirm_update_msg', 'Download kubectl {v}, verify its checksum, and make it the one External Cluster operations use. The system copy is not changed.').replace('{v}', target || 'latest'),
    confirmText: t('kube_cli_apply', 'Install / Switch'),
    danger: false,
  });
  if (!ok) return;
  await kubeCliStream('/api/kube/cli/install', target ? { version: target } : {});
}

// switchKubeCli pins a version typed in the field, picked in the list, or chosen from the cache.
async function switchKubeCli(explicit) {
  if (kubeCli.busy) return;
  const input = document.getElementById('kubeCliVersionInput');
  const sel = document.getElementById('kubeCliVersionSelect');
  const version = String(explicit || (input && input.value) || (sel && sel.value) || '').trim().replace(/^v/i, '');
  if (!KUBE_CLI_VERSION_RE.test(version)) {
    showToast(t('kube_cli_invalid', 'Enter a version such as 1.31.4.'), 'warning');
    return;
  }
  const ok = await showConfirm({
    title: t('kube_cli_confirm_switch_title', 'Switch kubectl?'),
    message: t('kube_cli_confirm_switch_msg', 'External Cluster operations will use kubectl {v} from now on. A version that is not cached is downloaded and verified first.').replace('{v}', version),
    confirmText: t('kube_cli_apply', 'Install / Switch'),
    danger: false,
  });
  if (!ok) return;
  await kubeCliStream('/api/kube/cli/switch', { version });
}

async function revertKubeCliToSystem() {
  if (kubeCli.busy) return;
  if (!(kubeCli.data && kubeCli.data.system_installed)) {
    showToast(t('kube_cli_no_system', 'No system kubectl is installed, so there is nothing to revert to.'), 'warning');
    return;
  }
  const ok = await showConfirm({
    title: t('kube_cli_confirm_system_title', 'Use system kubectl?'),
    message: t('kube_cli_confirm_system_msg', 'External Cluster operations will use the system copy of kubectl again. The cached versions are kept.'),
    confirmText: t('kube_cli_use_system', 'Use system kubectl'),
    danger: false,
  });
  if (!ok) return;
  kubeCli.busy = true;
  renderKubeCli();
  try {
    await kubeCliRequest('POST', '/api/kube/cli/switch', { mode: 'system' });
    showToast(t('kube_cli_reverted', 'Now using the system kubectl'), 'success');
  } catch (e) {
    showToast(t('kube_cli_failed', 'kubectl change failed') + ': ' + e.message, 'error', 6000);
  } finally {
    kubeCli.busy = false;
  }
  await checkKubeCli();
}

async function deleteKubeCli(version) {
  if (kubeCli.busy) return;
  const ok = await showConfirm({
    title: t('kube_cli_confirm_remove_title', 'Remove cached kubectl?'),
    message: t('kube_cli_confirm_remove_msg', 'Remove kubectl {v} from the cache. It can be downloaded again later.').replace('{v}', version),
    confirmText: t('kube_cli_remove', 'Remove'),
    danger: true,
  });
  if (!ok) return;
  kubeCli.busy = true;
  renderKubeCli();
  try {
    await kubeCliRequest('DELETE', '/api/kube/cli/versions/' + encodeURIComponent(version));
    showToast(t('kube_cli_removed', 'Cached version removed'), 'success');
  } catch (e) {
    showToast(t('kube_cli_failed', 'kubectl change failed') + ': ' + e.message, 'error', 6000);
  } finally {
    kubeCli.busy = false;
  }
  await checkKubeCli();
}

let kubeTopoSeq = 0;

function closeKubeTopology() {
  const drawer = document.getElementById('kubeTopoDrawer');
  const backdrop = document.getElementById('kubeTopoBackdrop');
  if (drawer) { drawer.classList.remove('open'); drawer.hidden = true; }
  if (backdrop) backdrop.hidden = true;
  if (window.GlobeRadar && window.GlobeRadar.syncPauseState) window.GlobeRadar.syncPauseState();
}

async function openKubeTopology(kind, ns, name) {
  const drawer = document.getElementById('kubeTopoDrawer');
  const backdrop = document.getElementById('kubeTopoBackdrop');
  const titleEl = document.getElementById('kubeTopoTitle');
  const bodyEl = document.getElementById('kubeTopoBody');
  if (!drawer || !bodyEl || !S.selectedKubeHostId) return;

  if (titleEl) titleEl.textContent = `${t('kube_topology_title', 'Resource Topology')} · ${kind}: ${ns}/${name}`;
  bodyEl.innerHTML = `<div class="kube-topo-empty">${SP.loading(t('kube_topology_loading', 'Loading topology...'))}</div>`;
  if (backdrop) backdrop.hidden = false;
  drawer.hidden = false;
  requestAnimationFrame(() => drawer.classList.add('open'));
  if (window.GlobeRadar && window.GlobeRadar.syncPauseState) window.GlobeRadar.syncPauseState();

  const seq = ++kubeTopoSeq;
  try {
    const url = `${kubeApiBase()}/topology?kind=${encodeURIComponent(kind)}&ns=${encodeURIComponent(ns || 'default')}&name=${encodeURIComponent(name)}`;
    const res = await fetch(url);
    const data = await res.json();
    if (seq !== kubeTopoSeq) return;
    handleKubeEnvelope(data);
    if (!data.ok) {
      bodyEl.innerHTML = `<div class="kube-topo-empty">${escapeHtml(t('error_prefix', 'Error: ') + (data.error || 'Failed to load topology'))}</div>`;
      return;
    }
    renderKubeTopology(data.topology);
  } catch (e) {
    if (seq !== kubeTopoSeq) return;
    bodyEl.innerHTML = `<div class="kube-topo-empty">${escapeHtml(t('error_prefix', 'Error: ') + e.message)}</div>`;
  }
}

function kubeTopoStatusClass(status) {
  const s = (status || '').toLowerCase();
  if (s === 'running' || s === 'healthy' || s === 'bound' || s === 'active' || s === 'succeeded' || s === 'completed') return 'badge-healthy';
  if (s === 'degraded' || s === 'pending' || s === 'terminating') return 'badge-warning';
  return 'badge-down';
}

function renderKubeTopology(topo) {
  const bodyEl = document.getElementById('kubeTopoBody');
  if (!bodyEl) return;
  if (!topo || !Array.isArray(topo.columns) || topo.columns.every(c => !c.items || c.items.length === 0)) {
    bodyEl.innerHTML = `<div class="kube-topo-empty">${escapeHtml(t('kube_topology_empty', 'No topology relations found for this resource.'))}</div>`;
    return;
  }
  const rootId = topo.root ? topo.root.id : '';
  const colsHtml = topo.columns.map(col => {
    const stageLabel = t('kube_topo_' + col.stage, col.stage);
    const cardsHtml = (col.items || []).length
      ? col.items.map(item => {
          const isRoot = item.id === rootId;
          return `<div class="kube-topo-card${isRoot ? ' root' : ''}" data-topo-id="${escapeHtml(item.id)}" onclick="openKubeTopology(${jsArg(item.kind)}, ${jsArg(item.namespace)}, ${jsArg(item.name)})">
            <div class="kube-topo-card-head">
              <span class="kube-topo-kind">${escapeHtml(item.kind)}</span>
              <span class="badge ${kubeTopoStatusClass(item.status)}" style="font-size:10px;">${escapeHtml(item.status || '-')}</span>
            </div>
            <div class="kube-topo-name" title="${escapeHtml(item.name)}">${escapeHtml(item.name)}</div>
            ${item.meta ? `<div class="kube-topo-meta" title="${escapeHtml(item.meta)}">${escapeHtml(item.meta)}</div>` : ''}
          </div>`;
        }).join('')
      : `<div class="kube-topo-none">—</div>`;
    return `<div class="kube-topo-col">
      <div class="kube-topo-col-title">${escapeHtml(stageLabel)}</div>
      <div class="kube-topo-col-items">${cardsHtml}</div>
    </div>`;
  }).join('');

  bodyEl.innerHTML = `<div class="kube-topo-canvas" id="kubeTopoCanvas">
    <svg class="kube-topo-svg" id="kubeTopoSvg" aria-hidden="true"></svg>
    <div class="kube-topo-grid" id="kubeTopoGrid">${colsHtml}</div>
  </div>`;

  requestAnimationFrame(() => drawKubeTopologyEdges(topo.edges || []));
}

function drawKubeTopologyEdges(edges) {
  const canvas = document.getElementById('kubeTopoCanvas');
  const svg = document.getElementById('kubeTopoSvg');
  if (!canvas || !svg) return;
  const cRect = canvas.getBoundingClientRect();
  svg.setAttribute('width', String(Math.max(1, Math.round(canvas.scrollWidth))));
  svg.setAttribute('height', String(Math.max(1, Math.round(canvas.scrollHeight))));
  svg.setAttribute('viewBox', `0 0 ${Math.max(1, Math.round(canvas.scrollWidth))} ${Math.max(1, Math.round(canvas.scrollHeight))}`);
  svg.innerHTML = '';

  const nodeMap = new Map();
  canvas.querySelectorAll('[data-topo-id]').forEach(el => {
    nodeMap.set(el.getAttribute('data-topo-id'), el);
  });

  edges.forEach(edge => {
    const fromEl = nodeMap.get(edge.from);
    const toEl = nodeMap.get(edge.to);
    if (!fromEl || !toEl) return;
    const fRect = fromEl.getBoundingClientRect();
    const tRect = toEl.getBoundingClientRect();
    const x1 = fRect.right - cRect.left + canvas.scrollLeft;
    const y1 = fRect.top + fRect.height / 2 - cRect.top + canvas.scrollTop;
    const x2 = tRect.left - cRect.left + canvas.scrollLeft;
    const y2 = tRect.top + tRect.height / 2 - cRect.top + canvas.scrollTop;
    const dx = Math.max(24, Math.abs(x2 - x1) * 0.45);
    const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
    path.setAttribute('d', `M ${x1} ${y1} C ${x1 + dx} ${y1}, ${x2 - dx} ${y2}, ${x2} ${y2}`);
    path.setAttribute('class', 'kube-topo-edge');
    svg.appendChild(path);
  });
}

export { saveKubeWorkspaces, kubeApiBase, kubeWsParam, kubeHostClusterName, kubeTargetInfo, kubeSameTarget, renderKubeWorkspaces, closeKubeWorkspace, kubeProviderLabel, loadKubeClusters, handleKubeEnvelope, getSelectedKubeMode, updateKubeModeChip, findHostForKubeNode, detectKubeClusters, formatKubeMemory, kubeAge, kubeEmptyHtml, populateKubeHostSelect, updateKubeHostPickerBtn, toggleKubeHostPicker, closeKubeHostPicker, onKubeHostPickerBtnKeydown, onKubeHostPickerKeydown, updateKubePickerHighlight, renderKubeHostPickerList, pickKubeHost, onSelectKubeHost, loadKubeNamespaces, renderKubeRefreshStatus, switchKubeTab, onKubeNamespaceChange, refreshKubeCurrentTab, loadKubeTabData, kubeFetchTab, loadKubeNodes, kubeWorkloadIsProblem, renderKubeWorkloadsTable, loadKubeWorkloads, kubePodIsProblem, onKubePodsFilterChange, renderKubePodsTable, loadKubePods, loadKubeServices, onKubeEventsFilterChange, renderKubeEventsTable, loadKubeEvents, loadKubeGenericResource, openKubeGenericMenu, loadKubeApply, onKubeApplyYamlInput, clearKubeApply, submitKubeDiff, kubeBusy, kubeAction, kubeGetOutput, submitKubeApply, openKubeNodeMenu, openKubeWorkloadMenu, openKubePodMenu, openKubeServiceMenu, openKubeScaleModal, submitKubeScale, kubeDeletePod, kubeDescribe, promptKubeContainer, submitKubeContainerChoice, kubePodExec, stopKubeLogs, openKubeLogs, onKubeLogsParamChange, connectKubeLogsStream, copyKubeLogs, kubeAutoRefreshIntervalSec, stopKubeAutorefresh, restartKubeAutorefresh, openKubeYamlEditor, initKubeYamlEditor, onKubeYamlEditorInput, submitKubeYamlDiff, submitKubeYamlApply, setKubeLogsCount, appendKubeLogText, onKubeLogsFilterInput, openKubeWorkloadLogs, openKubeDrain, renderKubeDrainPreview, confirmKubeDrain, showToastDrainUncordon, setKubeClusterError, openKubeClusterModal, loadKubeClusterFile, loadKubeClusterContexts, onKubeClusterContextChange, submitKubeClusterModal, editKubeCurrentCluster, currentKubeHostClusterId, openKubeHostClusterModal, submitKubeHostClusterModal, deleteKubeCluster, openKubeCliModal, checkKubeCli, installKubeCli, switchKubeCli, revertKubeCliToSystem, openKubeTopology, closeKubeTopology, renderKubeTopology, drawKubeTopologyEdges, openKubePortForward, submitKubePortForward };
