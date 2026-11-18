/* Network diagnostics and host comparison.
 * Native ES module fragment of the former monolithic inline script.
 * Air-gapped: no bundler, no CDN. Shared state lives in the mutable S object
 * from 00-state.js; cross-domain functions are imported explicitly. */
import { S } from './00-state.js';
import { svgIcon } from './00-foundation.js';
import { t, setLanguage, checkAuth } from './01-i18n-auth.js';
import { showToast } from './02-hosts.js';
import { copyTextToClipboard } from './04-terminal.js';
import { formatBytes } from './05-settings-transfer.js';
import { openModal, closeModal, escapeHtml } from './06-modal-docs-activity.js';

function openNetDiagModal(initialSourceHostId) {
  openModal('netDiagModal');
  const container = document.getElementById('netDiagModalBody');
  renderNetDiagUI(container, 'modal', initialSourceHostId || (S.currentHost ? S.currentHost.id : null), false);
}

function renderNetDiagTab(host) {
  const container = document.getElementById('tabContent');
  renderNetDiagUI(container, 'tab', host ? host.id : null, true);
}

function renderNetDiagUI(container, contextPrefix, defaultSourceId, isSourceLocked) {
  if (!container) return;

  if (S.currentNetDiagSSE) {
    S.currentNetDiagSSE.close();
    S.currentNetDiagSSE = null;
  }
  S.currentNetDiagHops = [];
  S.currentNetDiagRawLines = [];

  const onlineHosts = (S.allRawHosts || []).filter(h => h.status !== 'down');
  const availableSourceHosts = isSourceLocked ? (S.allRawHosts || []).filter(h => h.id === defaultSourceId) : onlineHosts;

  let activeSourceId = defaultSourceId;
  if (!activeSourceId && availableSourceHosts.length > 0) {
    activeSourceId = availableSourceHosts[0].id;
  }

  const cid = `netdiag_${contextPrefix}`;

  container.innerHTML = `
    <div style="display: flex; flex-direction: column; gap: 14px;">
      <!-- Diagnostic Controls Header Card -->
      <div class="card" style="padding: 16px;">
        <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: 14px; align-items: start;">
          <!-- Source Host -->
          <div class="form-group" style="margin-bottom: 0;">
            <div style="height: 28px; display: flex; align-items: center; margin-bottom: 8px;">
              <label style="font-size: var(--fs-xs); font-weight: 600; color: var(--text-muted); display: flex; align-items: center; gap: 6px; margin: 0;">
                ${svgIcon('monitor', 'icon-sm')} <span>${t('netdiag_source_host')}</span>
              </label>
            </div>
            ${isSourceLocked ? `
              <div style="height: 38px; box-sizing: border-box; padding: 0 12px; background: var(--surface-2); border: 1px solid var(--card-border); border-radius: var(--radius-sm); font-size: var(--fs-sm); display: flex; align-items: center; justify-content: space-between;">
                <span style="font-weight: 600; color: var(--text);">${escapeHtml(S.currentHost ? S.currentHost.name : '-')}</span>
                <span class="badge badge-info" style="font-family: var(--font-mono);">${escapeHtml(S.currentHost ? S.currentHost.host : '-')}</span>
              </div>
              <input type="hidden" id="${cid}_sourceHost" value="${activeSourceId || ''}" />
            ` : `
              <select id="${cid}_sourceHost" class="input" style="height: 38px; box-sizing: border-box; width: 100%;" onchange="onNetDiagSourceChange('${cid}')">
                <option value="local" ${activeSourceId === 'local' ? 'selected' : ''}>${t('netdiag_source_pantau')}</option>
                ${availableSourceHosts.map(h => `
                  <option value="${h.id}" ${h.id === activeSourceId ? 'selected' : ''}>
                    ${escapeHtml(h.name)} (${escapeHtml(h.host)})
                  </option>
                `).join('')}
              </select>
            `}
          </div>

          <!-- Target Mode & Destination -->
          <div class="form-group" style="margin-bottom: 0;">
            <div style="height: 28px; display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px;">
              <label style="font-size: var(--fs-xs); font-weight: 600; color: var(--text-muted); display: flex; align-items: center; gap: 6px; margin: 0;">
                ${svgIcon('globe', 'icon-sm')} <span>${t('netdiag_target_mode')}</span>
              </label>
              <div style="display: inline-flex; background: var(--surface-2); border: 1px solid var(--card-border); border-radius: var(--radius-sm); padding: 2px;">
                <button type="button" class="btn btn-sm ${S.currentNetDiagTargetMode === 'inter_host' ? 'btn-primary' : 'btn-ghost'}" id="${cid}_targetModeInter" onclick="setNetDiagTargetMode('${cid}', 'inter_host')" style="padding: 2px 8px; font-size: var(--fs-xs);">
                  ${t('netdiag_target_inter_host')}
                </button>
                <button type="button" class="btn btn-sm ${S.currentNetDiagTargetMode === 'custom' ? 'btn-primary' : 'btn-ghost'}" id="${cid}_targetModeCustom" onclick="setNetDiagTargetMode('${cid}', 'custom')" style="padding: 2px 8px; font-size: var(--fs-xs);">
                  ${t('netdiag_target_custom')}
                </button>
              </div>
            </div>

            <!-- Inter-Host Target Dropdown -->
            <div id="${cid}_interHostContainer" style="${S.currentNetDiagTargetMode === 'inter_host' ? 'display: block;' : 'display: none;'}">
              <select id="${cid}_targetHost" class="input" style="height: 38px; box-sizing: border-box; width: 100%;" onchange="updateNetDiagPrivateNote('${cid}')">
                <!-- Populated by populateNetDiagTargetHosts -->
              </select>
              <label style="display: flex; align-items: center; gap: 6px; margin-top: 6px; font-size: var(--fs-xs); color: var(--text-muted); cursor: pointer;">
                <input type="checkbox" id="${cid}_viaPrivate" ${S.currentNetDiagViaPrivate ? 'checked' : ''} />
                <span>${t('netdiag_via_private')}</span>
                <span id="${cid}_viaPrivateNote" style="font-family: var(--font-mono);"></span>
              </label>
            </div>

            <!-- Custom / Internet Target Input & Presets -->
            <div id="${cid}_customTargetContainer" style="${S.currentNetDiagTargetMode === 'custom' ? 'display: block;' : 'display: none;'}">
              <input type="text" id="${cid}_targetCustomInput" class="input" style="height: 38px; box-sizing: border-box; width: 100%; margin-bottom: 6px; font-family: var(--font-mono);" value="1.1.1.1" placeholder="${t('netdiag_target_ph')}" />
              <div style="display: flex; gap: 6px; flex-wrap: wrap;">
                <button type="button" class="btn btn-sm btn-ghost" style="padding: 2px 6px; font-size: var(--fs-xs);" onclick="setNetDiagPresetTarget('${cid}', '1.1.1.1')">1.1.1.1 (Cloudflare)</button>
                <button type="button" class="btn btn-sm btn-ghost" style="padding: 2px 6px; font-size: var(--fs-xs);" onclick="setNetDiagPresetTarget('${cid}', '8.8.8.8')">8.8.8.8 (Google)</button>
                <button type="button" class="btn btn-sm btn-ghost" style="padding: 2px 6px; font-size: var(--fs-xs);" onclick="setNetDiagPresetTarget('${cid}', 'google.com')">google.com</button>
              </div>
            </div>
          </div>
        </div>

        <!-- Tool Selection & Port Selector -->
        <div style="margin-top: 14px; padding-top: 14px; border-top: 1px solid var(--card-border); display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 12px;">
          <div style="display: flex; flex-direction: column; gap: 6px;">
            <label style="font-size: var(--fs-xs); font-weight: 600; color: var(--text-muted); display: flex; align-items: center; gap: 6px;">
              ${svgIcon('wrench', 'icon-sm')} <span>${t('netdiag_tool')}</span>
            </label>
            <div style="display: inline-flex; gap: 6px; flex-wrap: wrap;">
              <button type="button" class="btn btn-sm ${S.currentNetDiagTool === 'ping' ? 'btn-primary' : 'btn-ghost'}" id="${cid}_tool_ping" onclick="setNetDiagTool('${cid}', 'ping')" style="display: inline-flex; align-items: center; gap: 4px;">
                ${svgIcon('radar', 'icon-sm')} <span>${t('netdiag_tool_ping')}</span>
              </button>
              <button type="button" class="btn btn-sm ${S.currentNetDiagTool === 'traceroute' ? 'btn-primary' : 'btn-ghost'}" id="${cid}_tool_traceroute" onclick="setNetDiagTool('${cid}', 'traceroute')" style="display: inline-flex; align-items: center; gap: 4px;">
                ${svgIcon('arrow-left-right', 'icon-sm')} <span>${t('netdiag_tool_traceroute')}</span>
              </button>
              <button type="button" class="btn btn-sm ${S.currentNetDiagTool === 'port_probe' ? 'btn-primary' : 'btn-ghost'}" id="${cid}_tool_port_probe" onclick="setNetDiagTool('${cid}', 'port_probe')" style="display: inline-flex; align-items: center; gap: 4px;">
                ${svgIcon('plug', 'icon-sm')} <span>${t('netdiag_tool_port')}</span>
              </button>
              <button type="button" class="btn btn-sm ${S.currentNetDiagTool === 'dns_resolve' ? 'btn-primary' : 'btn-ghost'}" id="${cid}_tool_dns_resolve" onclick="setNetDiagTool('${cid}', 'dns_resolve')" style="display: inline-flex; align-items: center; gap: 4px;">
                ${svgIcon('search', 'icon-sm')} <span>${t('netdiag_tool_dns')}</span>
              </button>
              <button type="button" class="btn btn-sm ${S.currentNetDiagTool === 'dig' ? 'btn-primary' : 'btn-ghost'}" id="${cid}_tool_dig" onclick="setNetDiagTool('${cid}', 'dig')" style="display: inline-flex; align-items: center; gap: 4px;">
                ${svgIcon('search', 'icon-sm')} <span>${t('netdiag_tool_dig')}</span>
              </button>
            </div>
          </div>

          <!-- Port Probe Extra Options -->
          <div id="${cid}_portOptions" style="${S.currentNetDiagTool === 'port_probe' ? 'display: flex;' : 'display: none;'} flex-direction: column; gap: 6px;">
            <label style="font-size: 0.8rem; font-weight: 600; color: var(--text-muted); display: flex; align-items: center; gap: 6px;">
              ${svgIcon('plug', 'icon-sm')} <span>${t('netdiag_port_label')}</span>
            </label>
            <div style="display: flex; align-items: center; gap: 6px; flex-wrap: wrap;">
              <input type="number" id="${cid}_portInput" class="input" style="width: 85px; font-family: var(--font-mono);" value="80" min="1" max="65535" />
              <div style="display: flex; gap: 4px; flex-wrap: wrap;">
                <button type="button" class="btn btn-sm" style="padding: 2px 6px; font-size: 0.72rem;" onclick="document.getElementById('${cid}_portInput').value=80">80</button>
                <button type="button" class="btn btn-sm" style="padding: 2px 6px; font-size: 0.72rem;" onclick="document.getElementById('${cid}_portInput').value=443">443</button>
                <button type="button" class="btn btn-sm" style="padding: 2px 6px; font-size: 0.72rem;" onclick="document.getElementById('${cid}_portInput').value=22">22</button>
                <button type="button" class="btn btn-sm" style="padding: 2px 6px; font-size: 0.72rem;" onclick="document.getElementById('${cid}_portInput').value=3306">3306</button>
                <button type="button" class="btn btn-sm" style="padding: 2px 6px; font-size: 0.72rem;" onclick="document.getElementById('${cid}_portInput').value=5432">5432</button>
                <button type="button" class="btn btn-sm" style="padding: 2px 6px; font-size: 0.72rem;" onclick="document.getElementById('${cid}_portInput').value=6379">6379</button>
              </div>
            </div>
          </div>

          <!-- Dig Extra Options -->
          <div id="${cid}_digOptions" style="${S.currentNetDiagTool === 'dig' ? 'display: flex;' : 'display: none;'} gap: 10px; flex-wrap: wrap;">
            <label style="font-size: var(--fs-xs); font-weight: 600; color: var(--text-muted); display: flex; flex-direction: column; gap: 6px;">
              <span>${t('netdiag_dig_record_type')}</span>
              <select id="${cid}_digRecordType" class="input" style="width: 100px; font-family: var(--font-mono);">
                ${['A', 'AAAA', 'CNAME', 'MX', 'NS', 'TXT', 'SOA', 'PTR'].map(rt => `<option value="${rt}">${rt}</option>`).join('')}
              </select>
            </label>
            <label style="font-size: var(--fs-xs); font-weight: 600; color: var(--text-muted); display: flex; flex-direction: column; gap: 6px;">
              <span>${t('netdiag_dig_server')}</span>
              <input type="text" id="${cid}_digServer" class="input" list="${cid}_digServerList" style="width: 150px; font-family: var(--font-mono);" placeholder="${t('netdiag_dig_server_ph')}" />
              <datalist id="${cid}_digServerList"><option value="1.1.1.1"></option><option value="8.8.8.8"></option><option value="9.9.9.9"></option></datalist>
            </label>
          </div>

          <!-- Submit Button -->
          <div style="display: flex; align-items: flex-end; margin-left: auto;">
            <button class="btn btn-primary" id="${cid}_runBtn" onclick="runNetDiag('${cid}')" style="min-width: 140px; height: 36px; font-weight: 600;">
              ${t('netdiag_run_btn')}
            </button>
          </div>
        </div>
      </div>

      <!-- Execution Status & Live Feedback -->
      <div id="${cid}_statusBanner" style="display: none; padding: 10px 14px; border-radius: 6px; font-size: 0.85rem; align-items: center; justify-content: space-between;">
        <div style="display: flex; align-items: center; gap: 8px;">
          <div class="spinner" style="width: 14px; height: 14px; border-width: 2px;"></div>
          <span id="${cid}_statusText" style="color: var(--accent-text); font-weight: 500;">${t('netdiag_running')}</span>
        </div>
        <button class="btn btn-sm btn-danger" onclick="abortNetDiag('${cid}')" style="padding: 2px 8px; font-size: 0.75rem;">Cancel</button>
      </div>

      <!-- Automated Diagnosis Banner (Auto-Diagnosis) -->
      <div id="${cid}_autoDiagBanner" style="display: none; padding: 14px 16px; border-radius: 8px; border: 1px solid var(--card-border); background: color-mix(in srgb, var(--bg) 60%, transparent);">
        <div style="display: flex; align-items: flex-start; gap: 12px;">
          <span id="${cid}_autoDiagIcon" class="text-accent">${svgIcon('zap', 'icon-lg')}</span>
          <div style="flex: 1;">
            <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 4px;">
              <span style="font-weight: 700; font-size: 0.95rem; color: var(--text);">${t('netdiag_auto_diag_title', 'Automated Diagnosis')}</span>
              <span id="${cid}_autoDiagBadge" class="badge"></span>
            </div>
            <div id="${cid}_autoDiagSummary" style="font-size: 0.88rem; line-height: 1.5; color: var(--text);"></div>
          </div>
        </div>
      </div>

      <!-- Structured Results Cards Container -->
      <div id="${cid}_structuredResults" style="display: none;">
        <!-- Populated dynamically based on tool -->
      </div>

      <!-- Raw CLI Output Accordion -->
      <div id="${cid}_rawSection" style="display: none; margin-top: 4px;">
        <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 6px;">
          <label style="font-size: 0.78rem; font-weight: 600; color: var(--text-muted); cursor: pointer;" onclick="toggleNetDiagRaw('${cid}')">
            ${svgIcon('clipboard', 'icon-sm')} <span>${t('netdiag_raw_output')}</span> <span id="${cid}_rawToggleIcon">${svgIcon('chevron-down', 'icon-sm')}</span>
          </label>
          <button class="btn btn-sm" onclick="copyNetDiagRaw('${cid}')" style="padding: 2px 8px; font-size: 0.72rem;">
            ${t('netdiag_copy_raw')}
          </button>
        </div>
        <pre id="${cid}_rawPre" style="background: var(--code-bg); border: 1px solid var(--card-border); padding: 12px; border-radius: 6px; font-family: var(--font-mono); font-size: 0.8rem; max-height: 220px; overflow-y: auto; color: var(--text-muted); white-space: pre-wrap; line-height: 1.45;"></pre>
      </div>
    </div>
  `;

  populateNetDiagTargetHosts(cid);
}

function onNetDiagSourceChange(cid) {
  populateNetDiagTargetHosts(cid);
}

function populateNetDiagTargetHosts(cid) {
  const sourceEl = document.getElementById(`${cid}_sourceHost`);
  const targetSelect = document.getElementById(`${cid}_targetHost`);
  if (!targetSelect) return;

  const currentSourceId = sourceEl ? parseInt(sourceEl.value, 10) : 0;
  const otherHosts = (S.allRawHosts || []).filter(h => h.id !== currentSourceId);

  if (otherHosts.length === 0) {
    targetSelect.innerHTML = `<option value="" disabled selected>${t('netdiag_no_other_hosts')}</option>`;
    return;
  }

  const label = h => {
    const priv = netDiagPrivateIPs(h);
    return priv.length ? `${escapeHtml(h.host)} / ${escapeHtml(priv[0])}` : escapeHtml(h.host);
  };
  targetSelect.innerHTML = otherHosts.map(h => {
    const isOffline = h.status === 'down';
    if (isOffline) {
      return `<option value="${h.id}" disabled>${escapeHtml(h.name)} [${t('netdiag_status_offline')}] (${label(h)})</option>`;
    }
    return `<option value="${h.id}">${escapeHtml(h.name)} (${label(h)})</option>`;
  }).join('');
  updateNetDiagPrivateNote(cid);
}

function netDiagPrivateIPs(h) {
  try { return JSON.parse(h.private_ips || '[]') || []; } catch (e) { return []; }
}

function updateNetDiagPrivateNote(cid) {
  const note = document.getElementById(`${cid}_viaPrivateNote`);
  const box = document.getElementById(`${cid}_viaPrivate`);
  const select = document.getElementById(`${cid}_targetHost`);
  if (!note) return;
  const host = (S.allRawHosts || []).find(x => x.id === parseInt(select ? select.value : '0', 10));
  const priv = host ? netDiagPrivateIPs(host) : [];
  note.innerText = priv.length ? `→ ${priv[0]}` : (box && box.checked ? t('netdiag_no_private_ip') : '');
}

function setNetDiagTargetMode(cid, mode) {
  S.currentNetDiagTargetMode = mode;
  const btnInter = document.getElementById(`${cid}_targetModeInter`);
  const btnCustom = document.getElementById(`${cid}_targetModeCustom`);
  const interContainer = document.getElementById(`${cid}_interHostContainer`);
  const customContainer = document.getElementById(`${cid}_customTargetContainer`);

  if (!btnInter || !btnCustom) return;

  const inter = mode === 'inter_host';
  btnInter.classList.toggle('btn-primary', inter);
  btnInter.classList.toggle('btn-ghost', !inter);
  btnCustom.classList.toggle('btn-primary', !inter);
  btnCustom.classList.toggle('btn-ghost', inter);
  if (interContainer) interContainer.style.display = inter ? 'block' : 'none';
  if (customContainer) customContainer.style.display = inter ? 'none' : 'block';
}

function setNetDiagPresetTarget(cid, address) {
  const input = document.getElementById(`${cid}_targetCustomInput`);
  if (input) input.value = address;
}

function setNetDiagTool(cid, tool) {
  S.currentNetDiagTool = tool;
  const tools = ['ping', 'traceroute', 'port_probe', 'dns_resolve', 'dig'];
  tools.forEach(tName => {
    const btn = document.getElementById(`${cid}_tool_${tName}`);
    if (btn) {
      btn.classList.toggle('btn-primary', tName === tool);
      btn.classList.toggle('btn-ghost', tName !== tool);
    }
  });

  const portOpt = document.getElementById(`${cid}_portOptions`);
  if (portOpt) {
    portOpt.style.display = (tool === 'port_probe') ? 'flex' : 'none';
  }
  const digOpt = document.getElementById(`${cid}_digOptions`);
  if (digOpt) digOpt.style.display = (tool === 'dig') ? 'flex' : 'none';
}

function runNetDiag(cid) {
  const sourceEl = document.getElementById(`${cid}_sourceHost`);
  const sourceVal = sourceEl ? sourceEl.value : '';
  const sourceLocal = sourceVal === 'local';
  const sourceId = sourceLocal ? 0 : parseInt(sourceVal, 10);
  if (!sourceLocal && !sourceId) {
    showToast('Please select a valid source host', 'error');
    return;
  }

  let targetAddress = '';
  let targetHostId = 0;

  if (S.currentNetDiagTargetMode === 'inter_host') {
    const targetSelect = document.getElementById(`${cid}_targetHost`);
    targetHostId = targetSelect ? parseInt(targetSelect.value, 10) : 0;
    if (!targetHostId) {
      showToast('Please select a target host', 'error');
      return;
    }
  } else {
    const targetInput = document.getElementById(`${cid}_targetCustomInput`);
    targetAddress = targetInput ? targetInput.value.trim() : '';
    if (!targetAddress) {
      showToast('Please enter a target address or domain', 'error');
      return;
    }
  }

  let port = 0;
  if (S.currentNetDiagTool === 'port_probe') {
    const portInput = document.getElementById(`${cid}_portInput`);
    port = portInput ? parseInt(portInput.value, 10) : 80;
    if (isNaN(port) || port < 1 || port > 65535) {
      showToast('Port must be between 1 and 65535', 'error');
      return;
    }
  }

  // Reset UI
  if (S.currentNetDiagSSE) {
    S.currentNetDiagSSE.close();
    S.currentNetDiagSSE = null;
  }
  S.currentNetDiagHops = [];
  S.currentNetDiagRawLines = [];

  const runBtn = document.getElementById(`${cid}_runBtn`);
  if (runBtn) {
    runBtn.disabled = true;
    runBtn.innerText = t('netdiag_running');
  }

  const statusBanner = document.getElementById(`${cid}_statusBanner`);
  const statusText = document.getElementById(`${cid}_statusText`);
  if (statusBanner) {
    statusBanner.style.display = 'flex';
    statusBanner.style.background = 'color-mix(in srgb, var(--primary) 12%, transparent)';
    statusBanner.style.border = '1px solid color-mix(in srgb, var(--primary) 30%, transparent)';
  }
  if (statusText) statusText.innerText = `${t('netdiag_running')} (${S.currentNetDiagTool.toUpperCase()})`;

  const autoDiag = document.getElementById(`${cid}_autoDiagBanner`);
  if (autoDiag) autoDiag.style.display = 'none';

  const results = document.getElementById(`${cid}_structuredResults`);
  if (results) {
    results.style.display = 'block';
    results.innerHTML = '';
  }

  const rawSection = document.getElementById(`${cid}_rawSection`);
  const rawPre = document.getElementById(`${cid}_rawPre`);
  if (rawSection) rawSection.style.display = 'block';
  if (rawPre) rawPre.innerText = '';

  if (S.currentNetDiagTool === 'traceroute' && results) {
    results.innerHTML = `
      <div class="card" style="padding: 12px; background: color-mix(in srgb, var(--bg) 40%, transparent); border: 1px solid var(--card-border); border-radius: 8px;">
        <div style="font-weight: 600; font-size: 0.85rem; color: var(--text-muted); margin-bottom: 8px; display: flex; align-items: center; justify-content: space-between;">
          <span>${svgIcon('network', 'icon-sm')} ${t('netdiag_hops_detected')}</span>
          <span id="${cid}_hopCounterBadge" class="badge">0 hops</span>
        </div>
        <div style="overflow-x: auto;">
          <table style="width: 100%; border-collapse: collapse; font-size: 0.85rem;">
            <thead>
              <tr style="border-bottom: 1px solid var(--card-border); text-align: left; color: var(--text-muted); font-size: 0.75rem; text-transform: uppercase;">
                <th style="padding: 6px 8px; width: 45px;">${t('netdiag_hop')}</th>
                <th style="padding: 6px 8px;">${t('netdiag_host_ip')}</th>
                <th style="padding: 6px 8px; width: 140px;">${t('netdiag_latency')}</th>
                <th style="padding: 6px 8px; width: 150px;">${t('netdiag_zone')}</th>
                <th style="padding: 6px 8px; width: 90px; text-align: right;">Status</th>
              </tr>
            </thead>
            <tbody id="${cid}_hopsTbody"></tbody>
          </table>
        </div>
      </div>
    `;
  }

  const queryParams = new URLSearchParams({
    ...(sourceLocal ? { source: 'local' } : { source_host_id: sourceId }),
    type: S.currentNetDiagTool,
    target_type: S.currentNetDiagTargetMode === 'inter_host' ? 'host' : 'custom',
    target_host_id: targetHostId,
    target: targetAddress,
    port: port
  });
  if (S.currentNetDiagTargetMode === 'inter_host') {
    const viaEl = document.getElementById(`${cid}_viaPrivate`);
    S.currentNetDiagViaPrivate = !!(viaEl && viaEl.checked);
    if (S.currentNetDiagViaPrivate) queryParams.set('via', 'private');
  }
  if (S.currentNetDiagTool === 'dig') {
    queryParams.set('record_type', document.getElementById(`${cid}_digRecordType`)?.value || 'A');
    queryParams.set('dns_server', (document.getElementById(`${cid}_digServer`)?.value || '').trim());
  }

  const streamURL = `/api/netdiag/stream?${queryParams.toString()}`;
  const evtSource = new EventSource(streamURL);
  S.currentNetDiagSSE = evtSource;

  evtSource.onmessage = function(e) {
    try {
      const data = JSON.parse(e.data);
      handleNetDiagEvent(cid, data);
    } catch (err) {
      console.error("Netdiag SSE parse error:", err);
    }
  };

  evtSource.onerror = function() {
    evtSource.close();
    finishNetDiag(cid);
  };
}

function abortNetDiag(cid) {
  if (S.currentNetDiagSSE) {
    S.currentNetDiagSSE.close();
    S.currentNetDiagSSE = null;
  }
  finishNetDiag(cid);
  const statusText = document.getElementById(`${cid}_statusText`);
  if (statusText) statusText.innerText = "Probe aborted by user.";
}

function finishNetDiag(cid) {
  const runBtn = document.getElementById(`${cid}_runBtn`);
  if (runBtn) {
    runBtn.disabled = false;
    runBtn.innerText = t('netdiag_run_btn');
  }
  const statusBanner = document.getElementById(`${cid}_statusBanner`);
  if (statusBanner) statusBanner.style.display = 'none';
}

function handleNetDiagEvent(cid, evt) {
  if (evt.type === 'raw' && evt.raw_line !== undefined) {
    S.currentNetDiagRawLines.push(evt.raw_line);
    const rawPre = document.getElementById(`${cid}_rawPre`);
    if (rawPre) {
      rawPre.innerText += evt.raw_line + "\n";
      rawPre.scrollTop = rawPre.scrollHeight;
    }
  }

  if (evt.type === 'hop' && evt.hop) {
    const hop = evt.hop;
    const existingIdx = S.currentNetDiagHops.findIndex(h => h.hop === hop.hop);
    if (existingIdx >= 0) {
      S.currentNetDiagHops[existingIdx] = hop;
    } else {
      S.currentNetDiagHops.push(hop);
    }

    const tbody = document.getElementById(`${cid}_hopsTbody`);
    const counterBadge = document.getElementById(`${cid}_hopCounterBadge`);
    if (counterBadge) counterBadge.innerText = `${S.currentNetDiagHops.length} hops`;

    if (tbody) {
      renderHopRow(tbody, hop);
    }
  }

  if (evt.type === 'hop_analysis' && evt.analysis) {
    renderHopAnalysisBanner(cid, evt.analysis);
  }

  if (evt.type === 'ping_stats' && evt.ping) {
    renderPingResults(cid, evt.ping);
  }

  if (evt.type === 'port_stats' && evt.port) {
    renderPortResults(cid, evt.port);
  }

  if (evt.type === 'dns_stats' && evt.dns) {
    renderDNSResults(cid, evt.dns);
  }

  if (evt.type === 'dig_stats' && evt.dig) {
    renderDigResults(cid, evt.dig);
  }

  if (evt.type === 'error') {
    showToast(evt.message || 'Diagnostic probe error', 'error');
    const autoDiag = document.getElementById(`${cid}_autoDiagBanner`);
    const autoDiagIcon = document.getElementById(`${cid}_autoDiagIcon`);
    const autoDiagBadge = document.getElementById(`${cid}_autoDiagBadge`);
    const autoDiagSummary = document.getElementById(`${cid}_autoDiagSummary`);
    if (autoDiag && autoDiagSummary) {
      autoDiag.style.display = 'block';
      autoDiag.style.background = 'color-mix(in srgb, var(--danger) 12%, transparent)';
      autoDiag.style.borderColor = 'color-mix(in srgb, var(--danger) 35%, transparent)';
      if (autoDiagIcon) autoDiagIcon.innerHTML = svgIcon('circle-x', 'icon-lg');
      if (autoDiagBadge) {
        autoDiagBadge.className = 'badge badge-down';
        autoDiagBadge.innerText = 'FAILED';
      }
      autoDiagSummary.innerHTML = `<span style="color: var(--danger); font-weight: 600;">${escapeHtml(evt.message)}</span>`;
    }
    finishNetDiag(cid);
  }

  if (evt.type === 'done') {
    if (S.currentNetDiagSSE) {
      S.currentNetDiagSSE.close();
      S.currentNetDiagSSE = null;
    }
    finishNetDiag(cid);
  }
}

function renderHopRow(tbody, hop) {
  let rowEl = tbody.querySelector(`tr[data-hop="${hop.hop}"]`);
  const isNew = !rowEl;
  if (isNew) {
    rowEl = document.createElement('tr');
    rowEl.dataset.hop = hop.hop;
    rowEl.style.borderBottom = '1px solid color-mix(in srgb, var(--text) 5%, transparent)';
    tbody.appendChild(rowEl);
  }

  let zoneBadge = '';
  switch (hop.zone) {
    case 'private':
      zoneBadge = `<span class="badge badge-info" style="font-size: 0.72rem; display: inline-flex; align-items: center; gap: 4px;">${svgIcon('house', 'icon-sm')} <span>${t('netdiag_zone_private')}</span></span>`;
      break;
    case 'destination':
      zoneBadge = `<span class="badge badge-healthy" style="font-size: 0.72rem; display: inline-flex; align-items: center; gap: 4px;">${svgIcon('crosshair', 'icon-sm')} <span>${t('netdiag_zone_destination')}</span></span>`;
      break;
    case 'timeout':
      zoneBadge = `<span class="badge badge-danger" style="font-size: 0.72rem; display: inline-flex; align-items: center; gap: 4px;">${svgIcon('triangle-alert', 'icon-sm')} <span>${t('netdiag_zone_timeout')}</span></span>`;
      break;
    default:
      zoneBadge = `<span class="badge badge-unknown" style="font-size: 0.72rem; display: inline-flex; align-items: center; gap: 4px;">${svgIcon('globe', 'icon-sm')} <span>${t('netdiag_zone_transit')}</span></span>`;
  }
  let statusBadge = '';
  if (hop.status === 'timeout') {
    statusBadge = `<span class="badge badge-down">DROP</span>`;
  } else if (hop.status === 'warning') {
    statusBadge = `<span class="badge badge-degraded">HIGH RTT</span>`;
  } else {
    statusBadge = `<span class="badge badge-healthy">OK</span>`;
  }

  const barWidth = Math.min(100, Math.max(8, hop.rtt_ms * 0.8));
  const barColor = hop.rtt_ms > 100 ? 'var(--warning)' : 'var(--success)';

  rowEl.innerHTML = `
    <td style="padding: 8px; font-weight: 700; color: var(--text-muted); font-family: var(--font-mono);">#${hop.hop}</td>
    <td style="padding: 8px;">
      <div style="font-weight: 600; font-family: var(--font-mono); color: var(--text);">${escapeHtml(hop.host || '* * *')}</div>
      ${hop.ip && hop.ip !== hop.host ? `<div style="font-size: 0.72rem; color: var(--text-muted); font-family: var(--font-mono);">(${escapeHtml(hop.ip)})</div>` : ''}
    </td>
    <td style="padding: 8px;">
      ${hop.rtt_ms > 0 ? `
        <div style="font-weight: 600; color: ${barColor}; font-family: var(--font-mono); font-size: var(--fs-xs);">${hop.rtt_ms.toFixed(1)} ms</div>
        <div style="width: 100%; height: 4px; background: var(--surface-3); border-radius: 2px; margin-top: 3px; overflow: hidden;">
          <div style="width: ${barWidth}%; height: 100%; background: ${barColor}; border-radius: 2px;"></div>
        </div>
      ` : `<span style="color: var(--text-muted); font-family: var(--font-mono);">* * *</span>`}
    </td>
    <td style="padding: 8px;">${zoneBadge}</td>
    <td style="padding: 8px; text-align: right;">${statusBadge}</td>
  `;
}

function renderHopAnalysisBanner(cid, analysis) {
  const autoDiag = document.getElementById(`${cid}_autoDiagBanner`);
  const autoDiagIcon = document.getElementById(`${cid}_autoDiagIcon`);
  const autoDiagBadge = document.getElementById(`${cid}_autoDiagBadge`);
  const autoDiagSummary = document.getElementById(`${cid}_autoDiagSummary`);
  if (!autoDiag || !autoDiagSummary) return;

  autoDiag.style.display = 'block';

  let bg = 'var(--success-soft)';
  let border = 'var(--success-border)';
  let icon = svgIcon('circle-check', 'icon-sm');
  let badgeClass = 'badge badge-healthy';
  let badgeText = 'OPTIMAL';

  if (analysis.status === 'degraded') {
    bg = 'var(--warning-soft)';
    border = 'var(--warning-border)';
    icon = svgIcon('triangle-alert', 'icon-sm');
    badgeClass = 'badge badge-degraded';
    badgeText = 'DEGRADED';
  } else if (analysis.status === 'failed') {
    bg = 'var(--danger-soft)';
    border = 'var(--danger-border)';
    icon = svgIcon('circle-x', 'icon-sm');
    badgeClass = 'badge badge-down';
    badgeText = 'FAILED';
  }

  autoDiag.style.background = bg;
  autoDiag.style.borderColor = border;
  if (autoDiagIcon) autoDiagIcon.innerHTML = icon;
  if (autoDiagBadge) {
    autoDiagBadge.className = badgeClass;
    autoDiagBadge.innerText = badgeText;
  }

  const summaryText = S.currentLang === 'id' && analysis.summary_id ? analysis.summary_id : analysis.summary;
  autoDiagSummary.innerHTML = `<span>${escapeHtml(summaryText)}</span>`;
}

function renderPingResults(cid, ping) {
  const results = document.getElementById(`${cid}_structuredResults`);
  if (!results) return;

  const isHealthy = ping.status === 'healthy';
  const isDegraded = ping.status === 'degraded';
  const lossColor = ping.packet_loss_pct === 0 ? 'var(--success)' : (ping.packet_loss_pct === 100 ? 'var(--danger)' : 'var(--warning)');

  results.innerHTML = `
    <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(130px, 1fr)); gap: 10px; margin-bottom: 12px;">
      <div class="card" style="padding: 12px; background: color-mix(in srgb, var(--bg) 40%, transparent); text-align: center;">
        <div style="font-size: 0.72rem; color: var(--text-muted); text-transform: uppercase;">${t('netdiag_packets_sent')}</div>
        <div style="font-size: 1.4rem; font-weight: 700; color: var(--text); font-family: var(--font-mono);">${ping.packets_transmitted}</div>
      </div>
      <div class="card" style="padding: 12px; background: color-mix(in srgb, var(--bg) 40%, transparent); text-align: center;">
        <div style="font-size: 0.72rem; color: var(--text-muted); text-transform: uppercase;">${t('netdiag_packets_recv')}</div>
        <div style="font-size: 1.4rem; font-weight: 700; color: var(--success); font-family: var(--font-mono);">${ping.packets_received}</div>
      </div>
      <div class="card" style="padding: 12px; background: color-mix(in srgb, var(--bg) 40%, transparent); text-align: center;">
        <div style="font-size: 0.72rem; color: var(--text-muted); text-transform: uppercase;">${t('netdiag_packet_loss')}</div>
        <div style="font-size: 1.4rem; font-weight: 700; color: ${lossColor}; font-family: var(--font-mono);">${ping.packet_loss_pct.toFixed(0)}%</div>
      </div>
      <div class="card" style="padding: 12px; background: color-mix(in srgb, var(--bg) 40%, transparent); text-align: center;">
        <div style="font-size: 0.72rem; color: var(--text-muted); text-transform: uppercase;">${t('netdiag_avg_latency')}</div>
        <div style="font-size: 1.4rem; font-weight: 700; color: var(--accent-text); font-family: var(--font-mono);">${ping.avg_ms > 0 ? ping.avg_ms.toFixed(1) + ' ms' : '-'}</div>
      </div>
    </div>
  `;

  renderHopAnalysisBanner(cid, {
    status: isHealthy ? 'optimal' : (isDegraded ? 'degraded' : 'failed'),
    summary: ping.summary,
    summary_id: ping.summary_id
  });
}

function renderPortResults(cid, portStats) {
  const results = document.getElementById(`${cid}_structuredResults`);
  if (!results) return;

  const isOpen = portStats.open;
  results.innerHTML = `
    <div class="card" style="padding: 20px; border: 1px solid var(--card-border); border-radius: var(--radius); text-align: center;">
      <div style="font-size: 2rem; margin-bottom: 8px; color: ${isOpen ? 'var(--success)' : 'var(--danger)'};">${isOpen ? svgIcon('circle-check', 'icon-xl') : svgIcon('circle-x', 'icon-xl')}</div>
      <div style="font-size: var(--fs-md); font-weight: 700; color: ${isOpen ? 'var(--success)' : 'var(--danger)'}; font-family: var(--font-mono);">
        PORT ${portStats.port} ${isOpen ? t('netdiag_status_open') : t('netdiag_status_closed')}
      </div>
      <div style="color: var(--text-muted); font-size: var(--fs-xs); margin-top: 6px;">
        ${S.currentLang === 'id' ? portStats.summary_id : portStats.summary}
      </div>
    </div>
  `;

  renderHopAnalysisBanner(cid, {
    status: isOpen ? 'optimal' : 'failed',
    summary: portStats.summary,
    summary_id: portStats.summary_id
  });
}

function renderDNSResults(cid, dnsStats) {
  const results = document.getElementById(`${cid}_structuredResults`);
  if (!results) return;

  const isHealthy = dnsStats.status === 'healthy';
  results.innerHTML = `
    <div class="card" style="padding: 16px; border: 1px solid var(--card-border); border-radius: var(--radius);">
      <div style="font-weight: 600; font-size: var(--fs-xs); color: var(--text-muted); margin-bottom: 10px; display: flex; align-items: center; gap: 6px;">
        ${svgIcon('search', 'icon-sm')} <span>${t('netdiag_resolved_ips')} (${escapeHtml(dnsStats.target)}):</span>
      </div>
      <div style="display: flex; gap: 8px; flex-wrap: wrap;">
        ${dnsStats.resolved_ips && dnsStats.resolved_ips.length > 0 ? dnsStats.resolved_ips.map(ip => `
          <span class="badge badge-healthy" style="font-size: var(--fs-sm); font-family: var(--font-mono); padding: 4px 10px;">
            ${escapeHtml(ip)}
          </span>
        `).join('') : `<span style="color: var(--danger); font-size: var(--fs-xs);">None</span>`}
      </div>
    </div>
  `;

  renderHopAnalysisBanner(cid, {
    status: isHealthy ? 'optimal' : 'failed',
    summary: dnsStats.summary,
    summary_id: dnsStats.summary_id
  });
}

function renderDigResults(cid, dig) {
  const results = document.getElementById(`${cid}_structuredResults`);
  if (!results) return;

  const rows = (dig.answers || []).map(r => `
    <tr style="border-bottom: 1px solid color-mix(in srgb, var(--text) 5%, transparent);">
      <td style="padding: 6px 8px; font-family: var(--font-mono);">${escapeHtml(r.name)}</td>
      <td style="padding: 6px 8px; font-family: var(--font-mono);">${r.ttl}</td>
      <td style="padding: 6px 8px;"><span class="badge">${escapeHtml(r.type)}</span></td>
      <td style="padding: 6px 8px; font-family: var(--font-mono); word-break: break-all;">${escapeHtml(r.value)}</td>
    </tr>`).join('');
  results.innerHTML = `
    <div class="card" style="padding: 16px; border: 1px solid var(--card-border); border-radius: var(--radius);">
      <div style="font-weight: 600; font-size: var(--fs-xs); color: var(--text-muted); margin-bottom: 10px; display: flex; flex-wrap: wrap; align-items: center; gap: 8px;">
        ${svgIcon('search', 'icon-sm')} <span>dig ${escapeHtml(dig.target)} ${escapeHtml(dig.record_type)}</span>
        ${dig.rcode ? `<span class="badge ${dig.rcode === 'NOERROR' ? 'badge-healthy' : 'badge-down'}">${escapeHtml(dig.rcode)}</span>` : ''}
        ${dig.server ? `<span class="badge">@${escapeHtml(dig.server)}</span>` : ''}
        ${dig.rcode ? `<span class="badge">${dig.query_time_ms} ms</span>` : ''}
      </div>
      ${rows ? `
      <div style="overflow-x: auto;">
        <table style="width: 100%; border-collapse: collapse; font-size: var(--fs-xs);">
          <thead>
            <tr style="border-bottom: 1px solid var(--card-border); text-align: left; color: var(--text-muted); text-transform: uppercase;">
              <th style="padding: 6px 8px;">${t('netdiag_dig_name')}</th>
              <th style="padding: 6px 8px; width: 70px;">TTL</th>
              <th style="padding: 6px 8px; width: 80px;">${t('netdiag_dig_type')}</th>
              <th style="padding: 6px 8px;">${t('netdiag_dig_value')}</th>
            </tr>
          </thead>
          <tbody>${rows}</tbody>
        </table>
      </div>` : `<span style="color: var(--danger); font-size: var(--fs-xs);">${t('netdiag_dig_no_answer')}</span>`}
    </div>
  `;

  renderHopAnalysisBanner(cid, {
    status: dig.status === 'healthy' ? 'optimal' : dig.status === 'degraded' ? 'degraded' : 'failed',
    summary: dig.summary,
    summary_id: dig.summary_id
  });
}

function toggleNetDiagRaw(cid) {
  const pre = document.getElementById(`${cid}_rawPre`);
  const icon = document.getElementById(`${cid}_rawToggleIcon`);
  if (pre && icon) {
    if (pre.style.display === 'none') {
      pre.style.display = 'block';
      icon.innerHTML = svgIcon('chevron-down', 'icon-sm');
    } else {
      pre.style.display = 'none';
      icon.innerHTML = svgIcon('chevron-right', 'icon-sm');
    }
  }
}

function copyNetDiagRaw(cid) {
  const pre = document.getElementById(`${cid}_rawPre`);
  if (pre && pre.innerText) {
    navigator.clipboard.writeText(pre.innerText).then(() => {
      showToast(t('netdiag_raw_copied'), 'success');
    }).catch(() => {
      showToast('Failed to copy', 'error');
    });
  }
}

function openHostCompareModal(targetHostId) {
  if (targetHostId) {
    if (!S.compareHostIDs.includes(targetHostId)) {
      if (S.compareHostIDs.length >= 4) {
        S.compareHostIDs.pop();
      }
      S.compareHostIDs.unshift(targetHostId);
    }
  }

  // If compareHostIDs has fewer than 2 hosts, auto-populate from allRawHosts
  if (S.compareHostIDs.length < 2 && S.allRawHosts && S.allRawHosts.length > 0) {
    const available = S.allRawHosts.map(h => h.id);
    for (const hid of available) {
      if (!S.compareHostIDs.includes(hid)) {
        S.compareHostIDs.push(hid);
        if (S.compareHostIDs.length >= 3) break;
      }
    }
  }

  openModal('hostCompareModal');
  renderHostCompareView();
}

function closeHostCompareModal() {
  closeModal('hostCompareModal');
}

function onAddCompareHost(selectEl) {
  const val = parseInt(selectEl.value, 10);
  if (!val) return;
  if (!S.compareHostIDs.includes(val)) {
    if (S.compareHostIDs.length >= 4) {
      showToast(t('compare_max_hosts_msg', 'Maximum 4 hosts can be compared side-by-side.'), 'warning');
      selectEl.value = '';
      return;
    }
    S.compareHostIDs.push(val);
    renderHostCompareView();
  }
  selectEl.value = '';
}

function removeCompareHost(hostId) {
  S.compareHostIDs = S.compareHostIDs.filter(id => id !== hostId);
  renderHostCompareView();
}

function resetCompareHosts() {
  S.compareHostIDs = [];
  if (S.allRawHosts && S.allRawHosts.length > 0) {
    S.compareHostIDs = S.allRawHosts.slice(0, Math.min(3, S.allRawHosts.length)).map(h => h.id);
  }
  renderHostCompareView();
}

function calculateHostComparison(hosts) {
  // Categories and metrics specification
  // Polarity: 1 = HIB (Higher is Better), -1 = LIB (Lower is Better)
  const categories = [
    {
      id: 'compute',
      name: t('compare_cat_compute', 'Compute Capacity & Load'),
      metrics: [
        {
          id: 'cpu_cores',
          label: t('cores', 'CPU Cores'),
          polarity: 1,
          extract: h => h.cpu_cores || 1,
          format: (v, h) => `${v} ${v > 1 ? t('cores', 'Cores') : t('core', 'Core')}`
        },
        {
          id: 'cpu_load_norm',
          label: t('cpu_load_per_core', 'CPU Load (1m) / Core'),
          polarity: -1,
          extract: h => {
            let load1m = 0;
            if (h.cpu_load) {
              const parts = h.cpu_load.replace(',', ' ').trim().split(/\s+/);
              if (parts.length > 0) load1m = parseFloat(parts[0]) || 0;
            }
            return load1m / Math.max(1, h.cpu_cores || 1);
          },
          format: (v, h) => `${v.toFixed(2)} / core`
        },
        {
          id: 'ram_total',
          label: t('total_ram', 'Total RAM'),
          polarity: 1,
          extract: h => h.ram_total_bytes || 0,
          format: (v, h) => formatBytes(v)
        },
        {
          id: 'ram_free_pct',
          label: t('free_ram_pct', 'Free RAM Headroom'),
          polarity: 1,
          extract: h => h.ram_total_bytes > 0 ? Math.max(0, ((h.ram_total_bytes - (h.ram_used_bytes || 0)) / h.ram_total_bytes) * 100) : 0,
          format: (v, h) => {
            const freeBytes = Math.max(0, (h.ram_total_bytes || 0) - (h.ram_used_bytes || 0));
            return `${v.toFixed(1)}% (${formatBytes(freeBytes)})`;
          }
        }
      ]
    },
    {
      id: 'storage',
      name: t('compare_cat_storage', 'Storage Capacity & I/O'),
      metrics: [
        {
          id: 'disk_total',
          label: t('total_disk', 'Total Root Storage'),
          polarity: 1,
          extract: h => h.disk_total_bytes || 0,
          format: (v, h) => formatBytes(v)
        },
        {
          id: 'disk_free_pct',
          label: t('free_disk_pct', 'Free Disk Headroom'),
          polarity: 1,
          extract: h => h.disk_total_bytes > 0 ? Math.max(0, ((h.disk_total_bytes - (h.disk_used_bytes || 0)) / h.disk_total_bytes) * 100) : 0,
          format: (v, h) => {
            const freeBytes = Math.max(0, (h.disk_total_bytes || 0) - (h.disk_used_bytes || 0));
            return `${v.toFixed(1)}% (${formatBytes(freeBytes)})`;
          }
        },
        {
          id: 'disk_io_bps',
          label: t('disk_io_throughput', 'Current Disk I/O Rate'),
          polarity: -1,
          extract: h => (h.disk_read_speed_bps || 0) + (h.disk_write_speed_bps || 0),
          format: (v, h) => `${formatBytes(v)}/s`
        }
      ]
    },
    {
      id: 'network',
      name: t('compare_cat_network', 'Network Health & Security'),
      metrics: [
        {
          id: 'net_latency',
          label: t('egress_latency', 'Internet Latency'),
          polarity: -1,
          extract: h => h.internet_online ? (h.internet_latency_ms || 999) : 99999,
          format: (v, h) => h.internet_online ? `${v} ms` : 'Offline'
        },
        {
          id: 'auth_safety',
          label: t('auth_fails', 'Failed Auth Attempts'),
          polarity: -1,
          // Unreadable logs (-1) must never win "fewest failures".
          extract: h => (h.failed_logins_count < 0 ? Number.MAX_SAFE_INTEGER : (h.failed_logins_count || 0)),
          format: (v, h) => (v === Number.MAX_SAFE_INTEGER ? 'N/A' : `${v} failed`)
        },
        {
          id: 'net_egress_status',
          label: t('egress_connectivity', 'Internet Egress Status'),
          polarity: 1,
          extract: h => h.internet_online ? 1 : 0,
          format: (v, h) => h.internet_online ? 'Connected' : 'Offline'
        }
      ]
    },
    {
      id: 'hygiene',
      name: t('compare_cat_hygiene', 'System Hygiene & Lifecycle'),
      metrics: [
        {
          id: 'lifecycle_score',
          label: t('lifecycle_score_title', 'Lifecycle Score'),
          polarity: 1,
          extract: h => h.lifecycle_score || 0,
          format: (v, h) => `${v} / 100`
        },
        {
          id: 'system_status',
          label: t('system_status_label', 'System Desired State'),
          polarity: 1,
          extract: h => h.status === 'healthy' ? 1 : 0,
          format: (v, h) => h.status === 'healthy' ? 'Healthy' : (h.status === 'degraded' ? 'Degraded' : 'Down')
        },
        {
          id: 'freshness_lag',
          label: t('inspection_freshness', 'Inspection Freshness'),
          polarity: -1,
          extract: h => {
            if (!h.last_inspected) return 999999999;
            return Math.max(0, Date.now() - new Date(h.last_inspected).getTime());
          },
          format: (v, h) => {
            if (!h.last_inspected) return 'Never';
            const secs = Math.round(v / 1000);
            if (secs < 60) return `${secs}s ago`;
            const mins = Math.round(secs / 60);
            if (mins < 60) return `${mins}m ago`;
            const hrs = Math.round(mins / 60);
            return `${hrs}h ago`;
          }
        }
      ]
    }
  ];

  const hostScores = {};
  hosts.forEach(h => {
    hostScores[h.id] = { total: 0, byCat: {} };
    categories.forEach(c => { hostScores[h.id].byCat[c.id] = 0; });
  });

  const matrixResults = [];

  categories.forEach(cat => {
    const metricRows = [];
    cat.metrics.forEach(m => {
      const rowValues = {};
      const candidateList = [];

      hosts.forEach(h => {
        const isStaleOrDown = h.status === 'down' || (!h.last_inspected);
        const rawVal = m.extract(h);
        const formatted = m.format(rawVal, h);
        rowValues[h.id] = { raw: rawVal, formatted, isWinner: false, isTie: false, isStale: isStaleOrDown };

        if (h.status !== 'down') {
          candidateList.push({ hostId: h.id, val: rawVal });
        }
      });

      if (candidateList.length > 0) {
        candidateList.sort((a, b) => {
          return m.polarity === 1 ? (b.val - a.val) : (a.val - b.val);
        });

        const best = candidateList[0];
        let runnerUp = candidateList.length > 1 ? candidateList[1] : null;

        let hasDistinctWinner = false;
        if (runnerUp) {
          const maxAbs = Math.max(Math.abs(best.val), Math.abs(runnerUp.val));
          const diff = Math.abs(best.val - runnerUp.val);
          if (maxAbs === 0 || (diff / maxAbs) >= 0.05) {
            hasDistinctWinner = true;
          }
        } else {
          hasDistinctWinner = true;
        }

        if (hasDistinctWinner && candidateList.length > 0) {
          rowValues[best.hostId].isWinner = true;
          hostScores[best.hostId].total += 1;
          hostScores[best.hostId].byCat[cat.id] += 1;
        } else {
          if (candidateList.length > 1) {
            rowValues[candidateList[0].hostId].isTie = true;
            rowValues[candidateList[1].hostId].isTie = true;
          }
        }
      }

      metricRows.push({ metric: m, values: rowValues });
    });

    let bestCatScore = -1;
    let catWinners = [];
    hosts.forEach(h => {
      const score = hostScores[h.id].byCat[cat.id];
      if (score > bestCatScore) {
        bestCatScore = score;
        catWinners = [h.id];
      } else if (score === bestCatScore && score > 0) {
        catWinners.push(h.id);
      }
    });

    matrixResults.push({
      category: cat,
      metricRows,
      categoryWinners: catWinners,
      bestCatScore
    });
  });

  const sortedHostsByScore = [...hosts].sort((a, b) => {
    return hostScores[b.id].total - hostScores[a.id].total;
  });

  const maxTotalScore = sortedHostsByScore.length > 0 ? hostScores[sortedHostsByScore[0].id].total : 0;
  const overallWinners = sortedHostsByScore.filter(h => hostScores[h.id].total === maxTotalScore && maxTotalScore > 0);

  return {
    categories,
    matrixResults,
    hostScores,
    sortedHostsByScore,
    overallWinners,
    maxTotalScore
  };
}

function generateDecisionNarrative(hosts, results) {
  if (!hosts || hosts.length < 2) return { main: '', risk: '' };
  const lang = S.currentLang;
  const { hostScores, overallWinners, matrixResults } = results;

  const hostStrengths = {};
  hosts.forEach(h => {
    hostStrengths[h.id] = [];
  });

  matrixResults.forEach(res => {
    if (res.categoryWinners && res.categoryWinners.length > 0) {
      res.categoryWinners.forEach(hid => {
        if (hostStrengths[hid]) {
          hostStrengths[hid].push(res.category.name);
        }
      });
    }
  });

  const riskHosts = [];
  hosts.forEach(h => {
    const ramPct = h.ram_total_bytes > 0 ? (h.ram_used_bytes / h.ram_total_bytes) * 100 : 0;
    const diskPct = h.disk_total_bytes > 0 ? (h.disk_used_bytes / h.disk_total_bytes) * 100 : 0;
    if (h.status === 'down') {
      riskHosts.push({ host: h, reason: lang === 'id' ? 'berstatus DOWN' : 'is DOWN' });
    } else if (h.status === 'degraded') {
      riskHosts.push({ host: h, reason: lang === 'id' ? 'terdeteksi deviasi Desired State (Degraded)' : 'has Desired State drift (Degraded)' });
    } else if (ramPct > 85) {
      riskHosts.push({ host: h, reason: lang === 'id' ? `saturasi memori tinggi (${ramPct.toFixed(0)}% terpakai)` : `high memory saturation (${ramPct.toFixed(0)}% used)` });
    } else if (diskPct > 85) {
      riskHosts.push({ host: h, reason: lang === 'id' ? `penyimpanan hampir penuh (${diskPct.toFixed(0)}% terpakai)` : `storage near capacity (${diskPct.toFixed(0)}% used)` });
    }
  });

  let p1 = '';
  if (overallWinners.length === 1) {
    const w = overallWinners[0];
    const pts = hostScores[w.id].total;
    if (lang === 'id') {
      p1 = `Berdasarkan perbandingan objektif ${hosts.length} Host, <strong>${escapeHtml(w.name)}</strong> menempati posisi unggul dengan perolehan <strong>${pts} Poin</strong>. Server ini mendominasi keunggulan di kategori ${hostStrengths[w.id].length > 0 ? hostStrengths[w.id].join(', ') : 'spesifikasi sistem'}.`;
    } else {
      p1 = `Based on an objective evaluation of ${hosts.length} hosts, <strong>${escapeHtml(w.name)}</strong> emerges as the top performer with <strong>${pts} Points</strong>, leading in ${hostStrengths[w.id].length > 0 ? hostStrengths[w.id].join(', ') : 'overall specifications'}.`;
    }
  } else if (overallWinners.length > 1) {
    const names = overallWinners.map(h => `<strong>${escapeHtml(h.name)}</strong>`).join(' & ');
    const pts = hostScores[overallWinners[0].id].total;
    if (lang === 'id') {
      p1 = `Komparasi menunjukkan posisi berimbang (${pts} Poin) antara ${names}, di mana masing-masing server memiliki spesialisasi keunggulan yang saling melengkapi.`;
    } else {
      p1 = `The comparison shows a shared lead (${pts} Points) between ${names}, with complementary strengths across evaluated categories.`;
    }
  }

  let p2 = '';
  if (riskHosts.length > 0) {
    const riskStr = riskHosts.map(r => `<strong>${escapeHtml(r.host.name)}</strong> (${r.reason})`).join(', ');
    if (lang === 'id') {
      p2 = `${svgIcon('triangle-alert', 'icon-sm')} <strong>Peringatan Risiko:</strong> ${riskStr} memerlukan perhatian atau pemeliharaan sebelum dialokasikan beban kerja baru.`;
    } else {
      p2 = `${svgIcon('triangle-alert', 'icon-sm')} <strong>Risk Advisory:</strong> ${riskStr} requires maintenance or remediation before allocating additional workloads.`;
    }
  }

  return { main: p1, risk: p2 };
}

async function copyComparisonMarkdown() {
  const hosts = (S.allRawHosts || []).filter(h => S.compareHostIDs.includes(h.id));
  if (hosts.length < 2) {
    showToast(t('compare_min_hosts_msg', 'Please select at least 2 hosts to compare.'), 'warning');
    return;
  }

  const results = calculateHostComparison(hosts);
  const narrative = generateDecisionNarrative(hosts, results);
  const nowStr = new Date().toLocaleString();

  let md = `# Pantau — Host Comparison Report\n`;
  md += `_Generated on: ${nowStr}_\n\n`;

  md += `### Scoreboard & Standing\n\n`;
  md += `| Host | Status | Compute | Storage | Network | Hygiene | Total Points | Standing |\n`;
  md += `| :--- | :---: | :---: | :---: | :---: | :---: | :---: | :--- |\n`;

  results.sortedHostsByScore.forEach(h => {
    const sc = results.hostScores[h.id];
    const isOverall = results.overallWinners.some(w => w.id === h.id);
    const standing = isOverall ? 'Overall Winner' : 'Runner-up';
    md += `| **${h.name}** (${h.host}) | ${h.status} | ${sc.byCat['compute']} | ${sc.byCat['storage']} | ${sc.byCat['network']} | ${sc.byCat['hygiene']} | **${sc.total}** | ${standing} |\n`;
  });

  md += `\n### Decision Takeaway\n\n`;
  const cleanMain = narrative.main ? narrative.main.replace(/<[^>]+>/g, '') : '';
  const cleanRisk = narrative.risk ? narrative.risk.replace(/<[^>]+>/g, '') : '';
  if (cleanMain) md += `- ${cleanMain}\n`;
  if (cleanRisk) md += `- ${cleanRisk}\n`;

  try {
    await copyTextToClipboard(md);
    showToast(t('compare_copied_toast', 'Comparison summary copied to clipboard!'), 'success');
  } catch (err) {
    showToast('Failed to copy to clipboard', 'error');
  }
}

function renderHostCompareView() {
  const container = document.getElementById('compareModalBody');
  const pillsContainer = document.getElementById('compareSelectedPills');
  const selectEl = document.getElementById('compareAddHostSelect');
  if (!container) return;

  const hosts = (S.allRawHosts || []).filter(h => S.compareHostIDs.includes(h.id));

  // Render host selection pills
  if (pillsContainer) {
    pillsContainer.innerHTML = hosts.map(h => `
      <div class="compare-pill">
        <span style="display: inline-flex; align-items: center; gap: 4px;">${svgIcon('server', 'icon-sm')} <span>${escapeHtml(h.name)}</span></span>
        <span class="remove-btn" onclick="removeCompareHost(${h.id})" title="Remove">${svgIcon('x', 'icon-sm')}</span>
      </div>
    `).join('');
  }

  // Populate Add Host dropdown
  if (selectEl) {
    const unselected = (S.allRawHosts || []).filter(h => !S.compareHostIDs.includes(h.id));
    selectEl.innerHTML = `<option value="">${t('compare_add_host_ph', '+ Add Host (Max 4)...')}</option>` +
      unselected.map(h => `<option value="${h.id}">${escapeHtml(h.name)} (${escapeHtml(h.host)})</option>`).join('');
    selectEl.disabled = S.compareHostIDs.length >= 4 || unselected.length === 0;
  }

  // Check minimum 2 hosts
  if (hosts.length < 2) {
    container.innerHTML = `
      <div class="empty-state" style="padding: 48px 20px;">
        <div class="empty-state-icon">${svgIcon('git-compare', 'icon-xl')}</div>
        <div class="empty-state-title" data-i18n="compare_min_hosts_msg">${t('compare_min_hosts_msg', 'Please select at least 2 hosts to compare.')}</div>
        <div class="empty-state-desc">Pilih 2 hingga 4 host melalui dropdown di atas untuk memulai evaluasi matriks komparatif.</div>
        <div style="margin-top: 14px;">
          <button class="btn btn-primary btn-sm" onclick="resetCompareHosts()">${svgIcon('refresh-cw', 'icon-sm')} <span>${t('compare_reset_btn', 'Reset')}</span></button>
        </div>
      </div>
    `;
    return;
  }

  const results = calculateHostComparison(hosts);
  const narrative = generateDecisionNarrative(hosts, results);
  const colCount = hosts.length;

  let html = `<div style="display: flex; flex-direction: column; gap: 16px;">`;

  // Top Host Summary Cards Grid
  html += `
    <div style="display: grid; grid-template-columns: 220px repeat(${colCount}, 1fr); gap: 12px; align-items: stretch;">
      <div style="display: flex; flex-direction: column; justify-content: center; padding: 8px 12px; font-size: var(--fs-xs); color: var(--text-muted);">
        <div style="font-weight: 600; color: var(--accent-text); font-size: var(--fs-sm); display: flex; align-items: center; gap: 6px; margin-bottom: 4px;">
          ${svgIcon('git-compare', 'icon-sm')} <span>Side-by-Side</span>
        </div>
        <div>${hosts.length} Host Matrix Comparison</div>
      </div>
  `;

  hosts.forEach(h => {
    const sc = results.hostScores[h.id];
    const isWinner = results.overallWinners.some(w => w.id === h.id);
    const isDegraded = h.status === 'degraded' || h.status === 'down';
    const cardClass = isWinner ? 'compare-host-card overall-winner' : 'compare-host-card';
    const borderStyle = isDegraded ? 'border-color: var(--danger-border);' : '';

    html += `
      <div class="${cardClass}" style="${borderStyle}">
        ${isWinner ? `<span class="badge-winner">${svgIcon('star', 'icon-sm')} ${t('compare_overall_winner', 'Overall Winner')} (${sc.total} pts)</span>` : `<span class="badge-runnerup">${sc.total} ${t('compare_points_label', 'Points')}</span>`}
        <h4 style="margin: 6px 0 2px 0; font-size: var(--fs-md); font-weight: 600; color: ${isDegraded ? 'var(--danger)' : 'var(--text)'}; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;" title="${escapeHtml(h.name)}">${escapeHtml(h.name)}</h4>
        <div style="font-size: var(--fs-xs); color: var(--text-muted); font-family: var(--font-mono);">${escapeHtml(h.host)}</div>
        <div style="font-size: var(--fs-xs); color: var(--text-muted); margin-top: 4px; font-family: var(--font-mono);">
          ${h.cpu_cores || 1}C | ${formatBytes(h.ram_total_bytes || 0)} | ${h.lifecycle_score || 0} pts
        </div>
      </div>
    `;
  });
  html += `</div>`;
  // Comparison Matrix Table
  html += `
    <div style="background: var(--card-bg); border: 1px solid var(--card-border); border-radius: var(--radius); overflow: hidden;">
      <table class="compare-matrix-table">`;
  results.matrixResults.forEach(catRes => {
    const cat = catRes.category;
    let winnerNames = '';
    if (catRes.categoryWinners && catRes.categoryWinners.length > 0) {
      winnerNames = hosts
        .filter(h => catRes.categoryWinners.includes(h.id))
        .map(h => h.name)
        .join(', ');
    }

    html += `
      <thead>
        <tr class="compare-category-header">
          <td colspan="${colCount + 1}" style="padding: 8px 12px;">
            <span>${cat.name}</span>
            ${winnerNames ? `<span class="text-success" style="float: right; font-weight: normal; font-size: 0.72rem;">${svgIcon('star', 'icon-sm')} Winner: ${escapeHtml(winnerNames)}</span>` : ''}
          </td>
        </tr>
      </thead>
      <tbody>
    `;

    catRes.metricRows.forEach(row => {
      html += `<tr>`;
      html += `<td class="metric-col">${escapeHtml(row.metric.label)}</td>`;

      hosts.forEach(h => {
        const valInfo = row.values[h.id];
        let cellClass = '';
        let iconPrefix = '';
        if (valInfo.isWinner) {
          cellClass = 'cell-winner';
          iconPrefix = svgIcon('star', 'icon-sm') + ' ';
        } else if (valInfo.isStale) {
          cellClass = 'cell-stale';
        }

        html += `<td class="${cellClass}">${iconPrefix}${escapeHtml(valInfo.formatted)}</td>`;
      });

      html += `</tr>`;
    });

    html += `</tbody>`;
  });

  html += `</table></div>`;

  // Executive Decision Recommendation Card
  if (narrative.main || narrative.risk) {
    html += `
      <div class="compare-takeaway-card">
        <div style="display: flex; align-items: center; gap: 8px; font-weight: 600; color: var(--accent-text); font-size: var(--fs-sm);">
          ${svgIcon('activity', 'icon-sm')}
          <span data-i18n="compare_decision_takeaway">${t('compare_decision_takeaway', 'Decision Recommendation')}</span>
        </div>
        ${narrative.main ? `<p style="font-size: var(--fs-xs); line-height: 1.55; color: var(--text); margin: 0;">${narrative.main}</p>` : ''}
        ${narrative.risk ? `<div style="font-size: var(--fs-xs); line-height: 1.5; color: var(--danger); background: var(--danger-soft); border-left: 3px solid var(--danger); padding: 8px 12px; border-radius: var(--radius-sm);">${narrative.risk}</div>` : ''}
      </div>
    `;
  }

  html += `</div>`;
  container.innerHTML = html;
}

setLanguage(S.currentLang);

checkAuth();

export { openNetDiagModal, renderNetDiagTab, renderNetDiagUI, onNetDiagSourceChange, populateNetDiagTargetHosts, netDiagPrivateIPs, updateNetDiagPrivateNote, setNetDiagTargetMode, setNetDiagPresetTarget, setNetDiagTool, runNetDiag, abortNetDiag, finishNetDiag, handleNetDiagEvent, renderHopRow, renderHopAnalysisBanner, renderPingResults, renderPortResults, renderDNSResults, renderDigResults, toggleNetDiagRaw, copyNetDiagRaw, openHostCompareModal, closeHostCompareModal, onAddCompareHost, removeCompareHost, resetCompareHosts, calculateHostComparison, generateDecisionNarrative, copyComparisonMarkdown, renderHostCompareView };
