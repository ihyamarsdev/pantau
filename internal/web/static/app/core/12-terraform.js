/* Terraform workspaces, runs, state, variables, policies, modules.
 * Native ES module fragment of the former monolithic inline script.
 * Air-gapped: no bundler, no CDN. Shared state lives in the mutable S object
 * from 00-state.js; cross-domain functions are imported explicitly. */
import { S } from './00-state.js';
import { svgIcon } from './00-foundation.js';
import { t, showConfirm } from './01-i18n-auth.js';
import { showToast } from './02-hosts.js';
import { copyTextToClipboard } from './04-terminal.js';
import { openModal, closeModal, escapeHtml, jsArg } from './06-modal-docs-activity.js';

function tx(key, vars) {
  let s = t(key, key);
  if (vars) for (const [k, v] of Object.entries(vars)) s = s.split('{' + k + '}').join(v);
  return s;
}

async function tfApi(method, url, body) {
  const opt = { method, headers: {} };
  if (body !== undefined) { opt.headers['Content-Type'] = 'application/json'; opt.body = JSON.stringify(body); }
  try {
    const res = await fetch(url, opt);
    const text = await res.text();
    let data;
    try { data = text ? JSON.parse(text) : {}; } catch (e) { data = { error: text.trim() }; }
    if (data === null || typeof data !== 'object') data = { value: data };
    if (!res.ok && !data.error) data.error = 'HTTP ' + res.status;
    return { ok: res.ok, status: res.status, data };
  } catch (e) {
    return { ok: false, status: 0, data: { error: e.message } };
  }
}

function tfErr(r) { showToast(t('error_prefix', 'Error: ') + (r.data.error || 'Request failed'), 'error'); }

function tfJ(v, d) {
  if (typeof v === 'string') { try { return JSON.parse(v); } catch (e) { return d; } }
  return v == null ? d : v;
}

function tfEl(id) { return document.getElementById(id); }

function tfTime(ts) { return ts ? new Date(ts).toLocaleString() : '—'; }

function tfCsv(s) { return String(s || '').split(',').map(x => x.trim()).filter(Boolean); }

function tfStatusCls(s) {
  if (['queued', 'planning', 'applying', 'running'].includes(s)) return 'badge-running';
  if (['errored', 'policy_failed'].includes(s)) return 'badge-errored';
  if (['applied', 'no_changes'].includes(s)) return 'badge-applied';
  if (['awaiting_approval', 'drifted', 'locked', 'never'].includes(s)) return 'badge-' + s;
  return 'badge-never';
}

function tfBadge(s) {
  return `<span class="badge ${tfStatusCls(s)}">${S.tfE(t('tf_status_' + s, String(s || '').replace(/_/g, ' ')))}</span>`;
}

function tfCopy(text) {
  copyTextToClipboard(text);
  showToast(tx('tfx_copied'), 'success', 1500);
}

function tfCopyData(btn) { tfCopy(btn.dataset.copy || ''); }

function tfDlg(n, title, body, footer, width, cls) {
  tfEl('tfDlg' + n + 'Title').textContent = title;
  tfEl('tfDlg' + n + 'Body').innerHTML = body;
  tfEl('tfDlg' + n + 'Footer').innerHTML = footer;
  const content = tfEl('tfDlg' + n + 'Content');
  content.className = 'modal-content modal-dialog' + (cls ? ' ' + cls : '');
  content.style.maxWidth = 'min(' + (width || 560) + 'px, 96vw)';
  tfDlgErr(n, '');
  openModal('tfDlg' + n);
}

function tfDlgClose(n) { closeModal('tfDlg' + n); }

function tfDlgErr(n, msg) {
  const el = tfEl('tfDlg' + n + 'Err');
  el.textContent = msg || '';
  el.style.display = msg ? 'block' : 'none';
}

function tfFoot(n, primary) {
  return `<button type="button" class="btn btn-sm" onclick="tfDlgClose(${n})">${S.tfE(t('cancel', 'Cancel'))}</button>${primary || ''}`;
}

function tfPrimary(label, onclick, danger) {
  return `<button type="button" class="btn btn-sm ${danger ? 'btn-danger' : 'btn-primary'}" onclick="${onclick}">${S.tfE(label)}</button>`;
}

function tfField(label, inner, hint, full) {
  return `<div class="dk-field${full ? ' dk-full' : ''}"><label>${S.tfE(label)}</label>${inner}${hint ? `<div class="dk-hint">${S.tfE(hint)}</div>` : ''}</div>`;
}

function tfInput(id, val, extra) {
  return `<input type="text" id="${id}" class="form-input" style="width:100%;box-sizing:border-box;" value="${S.tfE(val == null ? '' : val)}" ${extra || ''} />`;
}

function tfSelect(id, opts, cur, onchange) {
  return `<select id="${id}" class="form-input" style="width:100%;box-sizing:border-box;" ${onchange ? `onchange="${onchange}"` : ''}>${opts.map(([v, l]) => `<option value="${S.tfE(v)}" ${String(v) === String(cur) ? 'selected' : ''}>${S.tfE(l)}</option>`).join('')}</select>`;
}

function tfCheck(id, label, checked, onchange) {
  return `<label style="display:flex;align-items:center;gap:8px;font-size:var(--fs-xs);cursor:pointer;"><input type="checkbox" id="${id}" ${checked ? 'checked' : ''} ${onchange ? `onchange="${onchange}"` : ''} /> <span>${S.tfE(label)}</span></label>`;
}

function tfV(id) { const el = tfEl(id); return el ? el.value : ''; }

function tfChk(id) { const el = tfEl(id); return !!(el && el.checked); }

function tfEmpty(msg) { return `<div style="padding:20px;text-align:center;color:var(--text-muted);font-size:var(--fs-xs);">${S.tfE(msg)}</div>`; }

function tfFilesEditorHTML() {
  return `<div class="dk-field dk-full"><label>${S.tfE(tx('tfx_files'))}</label>
    <div style="display:flex;gap:6px;align-items:center;margin-bottom:6px;flex-wrap:wrap;">
      <select id="tfFeSel" class="form-input" style="width:auto;min-width:160px;" onchange="tfFeSelect(this.value)"></select>
      <button type="button" class="btn btn-sm" onclick="tfFeRemove()">${svgIcon('trash-2', 'icon-xs')} ${S.tfE(tx('tfx_remove_file'))}</button>
      <input type="text" id="tfFeNew" class="form-input" style="width:150px;" placeholder="${S.tfE(tx('tfx_file_name_ph'))}" />
      <button type="button" class="btn btn-sm" onclick="tfFeAdd()">${svgIcon('plus', 'icon-xs')} ${S.tfE(tx('tfx_add_file'))}</button>
    </div>
    <textarea id="tfFeText" class="form-input" rows="14" spellcheck="false" style="width:100%;box-sizing:border-box;resize:vertical;font-family:var(--font-mono);font-size:var(--fs-xs);" oninput="tfFeStore()"></textarea></div>`;
}

function tfFeInit(files) {
  S.tfFE = { files: Object.assign({}, files || {}), cur: '' };
  const names = Object.keys(S.tfFE.files).sort();
  S.tfFE.cur = names.includes('main.tf') ? 'main.tf' : (names[0] || '');
  tfFeRender();
}

function tfFeRender() {
  const sel = tfEl('tfFeSel');
  const ta = tfEl('tfFeText');
  if (!sel || !ta) return;
  const names = Object.keys(S.tfFE.files).sort();
  sel.innerHTML = names.map(n => `<option value="${S.tfE(n)}" ${n === S.tfFE.cur ? 'selected' : ''}>${S.tfE(n)}</option>`).join('');
  ta.value = S.tfFE.cur ? (S.tfFE.files[S.tfFE.cur] || '') : '';
  ta.disabled = !S.tfFE.cur;
}

function tfFeStore() { const ta = tfEl('tfFeText'); if (ta && S.tfFE.cur) S.tfFE.files[S.tfFE.cur] = ta.value; }

function tfFeSelect(name) { tfFeStore(); S.tfFE.cur = name; tfFeRender(); }

function tfFeAdd() {
  tfFeStore();
  const inp = tfEl('tfFeNew');
  const name = (inp.value || '').trim();
  if (!/^[A-Za-z0-9_][A-Za-z0-9_.-]{0,99}$/.test(name)) { showToast(tx('tfx_file_name_bad'), 'warning'); return; }
  if (!(name in S.tfFE.files)) S.tfFE.files[name] = '';
  S.tfFE.cur = name;
  inp.value = '';
  tfFeRender();
}

function tfFeRemove() {
  if (!S.tfFE.cur) return;
  delete S.tfFE.files[S.tfFE.cur];
  S.tfFE.cur = Object.keys(S.tfFE.files).sort()[0] || '';
  tfFeRender();
}

function tfFeCollect() { tfFeStore(); return S.tfFE.files; }

async function loadTfStatus() {
  const r = await tfApi('GET', '/api/terraform/status');
  S.tfStatus = r.ok ? r.data : { enabled: false, supported: false, binaries: [], infracost: { available: false }, error: r.data.error };
  renderTfGate();
  return S.tfStatus;
}

function tfBinText(b) { return b.err ? `${b.name} — ${b.err}` : String(b.version || b.name).replace(/^\S+\s+/, b.name + ' '); }

function renderTfGate() {
  const st = S.tfStatus || { enabled: false, supported: false, binaries: [], infracost: {} };
  const pill = tfEl('tfGateStatus');
  if (!pill) return;
  pill.textContent = !st.supported ? t('tf_gate_unavailable', 'Terraform unavailable') : st.enabled ? t('tf_gate_active', 'Terraform Gate is Active') : t('tf_gate_disabled_title', 'Terraform Gate is Disabled');
  pill.className = 'badge ' + (!st.supported ? 'badge-danger' : st.enabled ? 'badge-healthy' : 'badge-warning');
  // Details (security warning, binaries, toggle) live only in the Gate tab; the header keeps just the status pill.
  const warn = tfEl('tfGateWarn');
  if (warn) {
    warn.style.color = !st.supported ? 'var(--danger)' : 'var(--warning)';
    warn.textContent = !st.supported ? (st.error || tx('tfx_no_binary')) : t('tf_gate_warning', tx('tfx_gate_security'));
  }
  const list = tfEl('tfGatePanelStatusList');
  if (list) {
    list.innerHTML = (st.binaries || []).map(b => `<div>${b.err ? svgIcon('circle-x', 'icon-xs') : svgIcon('circle-check', 'icon-xs')} ${S.tfE(tfBinText(b))} ${b.path ? `<span style="color:var(--text-faint);">${S.tfE(b.path)}</span>` : ''}</div>`).join('')
      + `<div>${st.infracost && st.infracost.available ? svgIcon('circle-check', 'icon-xs') : svgIcon('info', 'icon-xs')} infracost ${S.tfE(st.infracost && st.infracost.available ? (st.infracost.version || '') : tx('tfx_not_installed'))}</div>`;
  }
  const tb = tfEl('btnTfToggleGate');
  if (tb) {
    tb.textContent = st.enabled ? t('tf_gate_btn_disable', 'Disable Gate') : t('tf_gate_btn_enable', 'Enable Terraform Gate');
    tb.disabled = !st.supported;
  }
  if (typeof tfGateExtras === 'function') tfGateExtras(document.getElementById('tfGateExtras'));
}

async function toggleTfGate(enable) {
  if (enable === undefined) enable = !(S.tfStatus && S.tfStatus.enabled);
  const ok = await showConfirm(enable ? {
    title: t('tf_gate_confirm_enable_title', 'Enable Terraform Gate'),
    message: t('tf_gate_confirm_enable_msg', tx('tfx_gate_security')),
    confirmText: t('tf_gate_btn_enable', 'Enable Terraform Gate'), danger: true,
  } : {
    title: t('tf_gate_confirm_disable_title', 'Disable Terraform Gate'),
    message: t('tf_gate_confirm_disable_msg', 'Disable the Terraform Gate?'),
    confirmText: t('tf_gate_btn_disable', 'Disable Gate'), danger: true,
  });
  if (!ok) return;
  const r = await tfApi('PUT', '/api/terraform/status', { enabled: !!enable });
  if (!r.ok) { tfErr(r); return; }
  showToast(tx(enable ? 'tfx_gate_enabled_ok' : 'tfx_gate_disabled_ok'), 'success');
  await refreshTfSection();
}

function switchTfTab(tab) {
  if (!['workspaces', 'varsets', 'cloud', 'policies', 'modules', 'registry', 'audit', 'gate'].includes(tab)) tab = 'workspaces';
  S.tfCurrentTab = tab;
  document.querySelectorAll('#tfTabs .tab').forEach(el => {
    const active = el.dataset.tab === tab;
    el.classList.toggle('active', active);
    el.setAttribute('aria-selected', String(active));
    el.tabIndex = active ? 0 : -1;
    const panel = tfEl(el.getAttribute('aria-controls'));
    if (panel) panel.style.display = active ? 'flex' : 'none';
  });
  loadTfTabData();
}

function onTfTabKeydown(event) {
  const tabs = Array.from(document.querySelectorAll('#tfTabs .tab'));
  const i = tabs.indexOf(event.target);
  if (i < 0) return;
  let n;
  if (event.key === 'ArrowRight') n = (i + 1) % tabs.length;
  else if (event.key === 'ArrowLeft') n = (i + tabs.length - 1) % tabs.length;
  else if (event.key === 'Home') n = 0;
  else if (event.key === 'End') n = tabs.length - 1;
  else return;
  event.preventDefault();
  tabs[n].focus();
  switchTfTab(tabs[n].dataset.tab);
}

async function loadTfTabData() {
  if (!S.tfStatus || !S.tfStatus.enabled) {
    const off = `<div class="empty-state"><div class="empty-state-desc">${S.tfE(tx('tfx_gate_off'))}</div></div>`;
    if (S.tfCurrentTab !== 'gate') {
      const id = { workspaces: 'tfWsEmpty', varsets: 'tfVarsetsList', cloud: 'tfCloudRoot', policies: 'tfPoliciesList', modules: 'tfModulesList', registry: 'tfRegistryRoot', audit: 'tfAuditRoot' }[S.tfCurrentTab];
      const el = tfEl(id);
      if (el) { el.innerHTML = off; el.style.display = 'block'; }
      if (S.tfCurrentTab === 'workspaces') { tfEl('tfWsGrid').innerHTML = ''; tfEl('tfWsTableWrap').style.display = 'none'; }
    }
    return;
  }
  if (S.tfCurrentTab === 'workspaces') await loadTfWorkspaces();
  else if (S.tfCurrentTab === 'varsets') { await loadTfWorkspaces(); await loadTfVarsets(); }
  else if (S.tfCurrentTab === 'cloud') { if (typeof tfCloudRefresh === 'function') tfCloudRefresh(); }
  else if (S.tfCurrentTab === 'policies') { await loadTfWorkspaces(); await loadTfPolicies(); }
  else if (S.tfCurrentTab === 'modules') await loadTfModules();
  else if (S.tfCurrentTab === 'registry') { if (typeof tfRegistryRefresh === 'function') tfRegistryRefresh(); }
  else if (S.tfCurrentTab === 'audit') { if (typeof tfAuditRefresh === 'function') tfAuditRefresh(); }
}

async function refreshTfSection() {
  await loadTfStatus();
  await loadTfTabData();
}

async function loadTfWorkspaces() {
  const r = await tfApi('GET', '/api/terraform/workspaces');
  if (!r.ok) { tfErr(r); S.tfWorkspaces = []; } else S.tfWorkspaces = r.data.workspaces || [];
  renderTfWorkspaces();
}

function getTfWsCollapsed() {
  try {
    const raw = localStorage.getItem('pantau_tf_ws_collapsed');
    return new Set(raw ? JSON.parse(raw) : []);
  } catch (e) {
    return new Set();
  }
}

function toggleTfWsGroup(groupKey) {
  const set = getTfWsCollapsed();
  if (set.has(groupKey)) set.delete(groupKey);
  else set.add(groupKey);
  try {
    localStorage.setItem('pantau_tf_ws_collapsed', JSON.stringify(Array.from(set)));
  } catch (e) {}
  renderTfWorkspaces();
}

function setTfWsViewMode(mode) {
  S.tfWsViewMode = mode === 'list' ? 'list' : 'grid';
  localStorage.setItem('pantau_tf_view', S.tfWsViewMode);
  renderTfWorkspaces();
}

function renderTfWorkspaces() {
  const envSel = tfEl('tfWsEnvFilter');
  const tagSel = tfEl('tfWsTagFilter');
  const grpSel = tfEl('tfWsGroupFilter');
  if (!envSel || !tagSel) return;
  const curEnv = envSel.value;
  const curTag = tagSel.value;
  const curGrp = grpSel ? grpSel.value : 'all';

  const envs = Array.from(new Set(['production', 'staging', 'development'].concat(S.tfWorkspaces.map(w => w.environment).filter(Boolean)))).sort();
  const tags = Array.from(new Set(S.tfWorkspaces.flatMap(w => w.tags || []))).sort();
  const groups = Array.from(new Set(S.tfWorkspaces.map(w => (w.group_name || '').trim()).filter(Boolean))).sort();
  const hasUngrouped = S.tfWorkspaces.some(w => !(w.group_name || '').trim());

  envSel.innerHTML = `<option value="">${S.tfE(t('tf_filter_all_env', 'All Environments'))}</option>` + envs.map(e => `<option value="${S.tfE(e)}" ${e === curEnv ? 'selected' : ''}>${S.tfE(e)}</option>`).join('');
  tagSel.innerHTML = `<option value="">${S.tfE(t('tf_filter_all_tags', 'All Tags'))}</option>` + tags.map(e => `<option value="${S.tfE(e)}" ${e === curTag ? 'selected' : ''}>${S.tfE(e)}</option>`).join('');

  if (grpSel) {
    let opts = `<option value="all" data-i18n="filter_all_groups">${S.tfE(t('filter_all_groups', 'All Groups'))}</option>`;
    groups.forEach(g => { opts += `<option value="${S.tfE(g)}">${S.tfE(g)}</option>`; });
    if (hasUngrouped && groups.length > 0) {
      opts += `<option value="__ungrouped__" data-i18n="filter_ungrouped">${S.tfE(t('filter_ungrouped', 'Ungrouped'))}</option>`;
    }
    grpSel.innerHTML = opts;
    if (groups.includes(curGrp) || (curGrp === '__ungrouped__' && hasUngrouped)) grpSel.value = curGrp;
    else grpSel.value = 'all';
  }
  const selGrp = grpSel ? grpSel.value : 'all';

  const q = (tfV('tfWsSearchInput')).toLowerCase().trim();
  const list = S.tfWorkspaces.filter(w => {
    const grp = (w.group_name || '').trim();
    if (selGrp !== 'all') {
      if (selGrp === '__ungrouped__' && grp !== '') return false;
      if (selGrp !== '__ungrouped__' && grp !== selGrp) return false;
    }
    if (envSel.value && w.environment !== envSel.value) return false;
    if (tagSel.value && !(w.tags || []).includes(tagSel.value)) return false;
    if (!q) return true;
    return (w.name + ' ' + (w.description || '')).toLowerCase().includes(q) || grp.toLowerCase().includes(q);
  });

  tfEl('tfWsCountBadge').textContent = list.length + ' workspaces';
  tfEl('tfWsViewGridBtn').classList.toggle('active', S.tfWsViewMode === 'grid');
  tfEl('tfWsViewListBtn').classList.toggle('active', S.tfWsViewMode === 'list');
  const empty = tfEl('tfWsEmpty');
  const grid = tfEl('tfWsGrid');
  const wrap = tfEl('tfWsTableWrap');
  empty.innerHTML = `<div class="empty-state-icon">${svgIcon('layers', 'icon-xl')}</div><div class="empty-state-desc">${S.tfE(t('tf_no_ws', 'No workspaces found'))}</div>`;
  if (!list.length) { empty.style.display = 'flex'; grid.style.display = 'none'; wrap.style.display = 'none'; return; }
  empty.style.display = 'none';
  const lastRun = w => w.last_run ? `${tfBadge(w.last_run.status)} <span style="color:var(--text-muted);">#${w.last_run.id}</span>` : '—';

  const anyHasGroup = S.tfWorkspaces.some(w => (w.group_name || '').trim() !== '');

  const renderGridCard = w => `<div class="card" style="cursor:pointer;" onclick="openTfWorkspace(${w.id})">
    <div class="card-title"><span style="min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;">${S.tfE(w.name)}</span>${tfBadge(w.status)}</div>
    <div style="font-size:var(--fs-xs);color:var(--text-muted);min-height:16px;">${S.tfE(w.description || '')}</div>
    <div style="display:flex;gap:6px;flex-wrap:wrap;align-items:center;">
      ${w.environment ? `<span class="badge">${S.tfE(w.environment)}</span>` : ''}
      <span class="badge mono">${S.tfE(w.binary)}${w.tf_version ? ' ' + S.tfE(w.tf_version) : ''}</span>
      ${w.drift_detected ? `<span class="badge badge-drifted">${S.tfE(t('tf_status_drifted', 'Drifted'))}</span>` : ''}
      ${w.locked ? `<span class="badge badge-locked">${svgIcon('lock', 'icon-xs')} ${S.tfE(t('tf_status_locked', 'Locked'))}</span>` : ''}
      ${(w.tags || []).map(x => `<span class="badge" style="background:var(--surface-2);">${S.tfE(x)}</span>`).join('')}
    </div>
    <div style="display:flex;justify-content:space-between;font-size:var(--fs-xs);color:var(--text-muted);"><span>${w.resource_count || 0} ${S.tfE(tx('tfx_tab_resources'))}</span><span>${lastRun(w)}</span></div>
  </div>`;

  const renderListRow = w => `<tr style="cursor:pointer;" onclick="openTfWorkspace(${w.id})">
    <td><b>${S.tfE(w.name)}</b><div style="color:var(--text-muted);">${S.tfE(w.description || '')}</div></td>
    <td>${tfBadge(w.status)}${w.drift_detected ? ` <span class="badge badge-drifted">${S.tfE(t('tf_status_drifted', 'Drifted'))}</span>` : ''}</td>
    <td>${S.tfE(w.environment || '—')}</td><td class="mono">${S.tfE(w.binary)}</td><td>${w.resource_count || 0}</td><td>${lastRun(w)}</td>
    <td style="text-align:right;white-space:nowrap;">
      <button type="button" class="btn btn-icon btn-sm" onclick="event.stopPropagation();openTfWorkspace(${w.id}, 'settings')" title="${S.tfE(t('edit', 'Edit'))}">${svgIcon('pencil', 'icon-xs')}</button>
      <button type="button" class="btn btn-icon btn-sm" onclick="event.stopPropagation();deleteTfWorkspace(${w.id})" title="${S.tfE(t('delete', 'Delete'))}">${svgIcon('trash-2', 'icon-xs')}</button>
    </td></tr>`;

  if (S.tfWsViewMode === 'grid') {
    wrap.style.display = 'none';
    grid.style.display = 'grid';
    if (!anyHasGroup) {
      grid.innerHTML = list.map(renderGridCard).join('');
      return;
    }
    const collapsed = getTfWsCollapsed();
    const groupsMap = new Map();
    const ungrouped = [];
    list.forEach(w => {
      const g = (w.group_name || '').trim();
      if (!g) ungrouped.push(w);
      else {
        if (!groupsMap.has(g)) groupsMap.set(g, []);
        groupsMap.get(g).push(w);
      }
    });
    const sections = [];
    Array.from(groupsMap.keys()).sort((a, b) => a.localeCompare(b)).forEach(name => {
      sections.push({ key: 'g:' + name, name, items: groupsMap.get(name) });
    });
    if (ungrouped.length > 0) {
      sections.push({ key: '__ungrouped__', name: t('group_ungrouped', 'Ungrouped'), items: ungrouped });
    }
    let html = '';
    sections.forEach(sec => {
      const isCol = collapsed.has(sec.key);
      html += `<div class="host-group-divider collapsible" onclick="toggleTfWsGroup(${jsArg(sec.key)})">
        <span class="host-group-title">
          <svg class="icon icon-xs grp-caret ${isCol ? 'collapsed' : ''}"><use href="#i-chevron-down"/></svg>
          <svg class="icon icon-sm"><use href="#i-folder"/></svg>
          ${escapeHtml(sec.name)}
        </span>
        <span class="badge" style="font-size: var(--fs-xs); background: var(--surface-2); color: var(--text-muted); border: 1px solid var(--card-border);">${sec.items.length}</span>
      </div>`;
      if (!isCol) {
        html += sec.items.map(renderGridCard).join('');
      }
    });
    grid.innerHTML = html;
  } else {
    grid.style.display = 'none';
    wrap.style.display = 'block';
    if (!anyHasGroup) {
      tfEl('tfWsTableBody').innerHTML = list.map(renderListRow).join('');
      return;
    }
    const collapsed = getTfWsCollapsed();
    const groupsMap = new Map();
    const ungrouped = [];
    list.forEach(w => {
      const g = (w.group_name || '').trim();
      if (!g) ungrouped.push(w);
      else {
        if (!groupsMap.has(g)) groupsMap.set(g, []);
        groupsMap.get(g).push(w);
      }
    });
    const sections = [];
    Array.from(groupsMap.keys()).sort((a, b) => a.localeCompare(b)).forEach(name => {
      sections.push({ key: 'g:' + name, name, items: groupsMap.get(name) });
    });
    if (ungrouped.length > 0) {
      sections.push({ key: '__ungrouped__', name: t('group_ungrouped', 'Ungrouped'), items: ungrouped });
    }
    let html = '';
    sections.forEach(sec => {
      const isCol = collapsed.has(sec.key);
      html += `<tr class="host-list-group-row collapsible" onclick="toggleTfWsGroup(${jsArg(sec.key)})">
        <td colspan="7">
          <div style="display: flex; align-items: center; gap: 8px;">
            <svg class="icon icon-xs grp-caret ${isCol ? 'collapsed' : ''}"><use href="#i-chevron-down"/></svg>
            <svg class="icon icon-sm"><use href="#i-folder"/></svg>
            <span>${escapeHtml(sec.name)}</span>
            <span class="badge" style="font-size: var(--fs-xs); background: var(--card-bg); color: var(--text-muted); border: 1px solid var(--card-border);">${sec.items.length}</span>
          </div>
        </td>
      </tr>`;
      if (!isCol) {
        html += sec.items.map(renderListRow).join('');
      }
    });
    tfEl('tfWsTableBody').innerHTML = html;
  }
}

async function openTfWorkspaceModal(id) {
  let ws = null;
  if (id) {
    const r = await tfApi('GET', '/api/terraform/workspaces/' + id);
    if (!r.ok) { tfErr(r); return; }
    ws = r.data.workspace;
  }
  const bins = (S.tfStatus && S.tfStatus.binaries) || [{ name: 'terraform' }, { name: 'tofu' }];
  const defMain = 'resource "terraform_data" "example" {\n  input = "hello"\n}\n\noutput "example" {\n  value = terraform_data.example.output\n}\n';
  const src = ws ? ws.source : 'inline';
  const srcSeg = (v, icon, label) => `<button type="button" data-src="${v}" aria-pressed="${src === v ? 'true' : 'false'}" onclick="tfWfSetSource('${v}')">${svgIcon(icon, 'icon-xs')} <span>${S.tfE(label)}</span></button>`;
  const body = `<div class="tf-ws">
    <div class="dk-form-grid tf-ws-grid">
    ${tfField(tx('tfx_name') + ' *', tfInput('tfWfName', ws && ws.name, 'maxlength="100"'))}
    ${tfField(tx('tfx_group'), tfInput('tfWfGroup', ws && ws.group_name, 'list="tfGroupList" placeholder="' + S.tfE(tx('tfx_group_ph')) + '"') + '<datalist id="tfGroupList">' + Array.from(new Set(S.tfWorkspaces.map(w => (w.group_name || '').trim()).filter(Boolean))).sort().map(g => `<option value="${S.tfE(g)}">`).join('') + '</datalist>')}
    ${tfField(tx('tfx_environment'), tfInput('tfWfEnv', ws && ws.environment, 'list="tfEnvList" placeholder="production"') + '<datalist id="tfEnvList"><option value="production"><option value="staging"><option value="development"></datalist>')}
    ${tfField(tx('tfx_description'), tfInput('tfWfDesc', ws && ws.description), '', true)}
    ${tfField(tx('tfx_tags'), tfInput('tfWfTags', ws ? (ws.tags || []).join(', ') : ''))}
    ${tfField(tx('tfx_binary'), tfSelect('tfWfBinary', bins.map(b => [b.name, b.name + (b.err ? ' (' + tx('tfx_not_installed') + ')' : '')]), ws ? ws.binary : (bins.find(b => !b.err) || bins[0]).name))}
    ${tfField(tx('tfx_drift_interval'), tfInput('tfWfDrift', ws ? ws.drift_interval_minutes : 0, 'type="number" min="0"'), tx('tfx_drift_off') + ': 0')}
    <div class="dk-field tf-ws-source"><label>${S.tfE(tx('tfx_source'))}</label>
      <select id="tfWfSource" hidden onchange="tfWfSourceChanged()">${[['inline', tx('tfx_source_inline')], ['git', tx('tfx_source_git')]].map(([v, l]) => `<option value="${v}" ${src === v ? 'selected' : ''}>${S.tfE(l)}</option>`).join('')}</select>
      <div class="tf-ws-seg" role="group" aria-label="${S.tfE(tx('tfx_source'))}">${srcSeg('inline', 'file-code', tx('tfx_source_inline'))}${srcSeg('git', 'git-branch', tx('tfx_source_git'))}</div>
    </div>
    </div>
    <div id="tfWfInline" class="tf-ws-editor">${tfFilesEditorHTML()}</div>
    <div id="tfWfGit" class="dk-form-grid tf-ws-grid" style="display:none;">
      ${tfField(tx('tfx_git_url'), tfInput('tfWfGitUrl', ws && ws.git_url, 'placeholder="https://github.com/org/infra.git"'), '', true)}
      ${tfField(tx('tfx_git_branch'), tfInput('tfWfGitBranch', ws && ws.git_branch, 'placeholder="main"'))}
      ${tfField(tx('tfx_git_path'), tfInput('tfWfGitPath', ws && ws.git_path, 'placeholder="envs/prod"'))}
      ${tfField(tx('tfx_git_auth'), tfSelect('tfWfGitAuth', [['none', tx('tfx_git_auth_none')], ['token', tx('tfx_git_auth_token')], ['ssh', tx('tfx_git_auth_ssh')]], ws ? ws.git_auth || 'none' : 'none', 'tfWfAuthChanged()'))}
      <div id="tfWfTokenWrap">${tfField(tx('tfx_git_token'), `<input type="password" id="tfWfGitToken" class="form-input" autocomplete="new-password" style="width:100%;box-sizing:border-box;" placeholder="${ws && ws.git_token_set ? S.tfE(tx('tfx_git_token_keep')) : ''}" />`)}</div>
      <div id="tfWfKeyWrap">${tfField('SSH Key', '<select id="tfWfGitKey" class="form-input" style="width:100%;box-sizing:border-box;"></select>')}</div>
    </div>
  </div>`;
  tfDlg(1, ws ? t('tf_ws_modal_edit', 'Edit Workspace') : t('tf_ws_modal_new', 'New Workspace'), body,
    tfFoot(1, tfPrimary(t('save', 'Save'), `saveTfWorkspace(${id || 0})`)), 1180, 'tf-ws-dlg');
  tfFeInit(ws && ws.files && Object.keys(ws.files).length ? ws.files : { 'main.tf': defMain });
  tfWfSourceChanged();
  tfWfAuthChanged();
  const keySel = tfEl('tfWfGitKey');
  const kr = await tfApi('GET', '/api/ssh-keys');
  const keys = kr.ok && Array.isArray(kr.data) ? kr.data : [];
  keySel.innerHTML = keys.map(k => `<option value="${Number(k.id)}" ${ws && Number(k.id) === Number(ws.git_key_id) ? 'selected' : ''}>${S.tfE(k.name)}</option>`).join('');
  if (!ws && typeof tfTplInject === 'function') tfTplInject();
}

function tfWfSetSource(v) {
  const sel = tfEl('tfWfSource');
  if (!sel) return;
  sel.value = v;
  tfWfSourceChanged();
}

function tfWfSourceChanged() {
  const src = tfV('tfWfSource');
  const git = src === 'git';
  tfEl('tfWfInline').style.display = git ? 'none' : 'flex';
  tfEl('tfWfGit').style.display = git ? 'grid' : 'none';
  document.querySelectorAll('#tfDlg1 .tf-ws-seg button').forEach(b => b.setAttribute('aria-pressed', String(b.dataset.src === src)));
}

function tfWfAuthChanged() {
  const a = tfV('tfWfGitAuth');
  if (!tfEl('tfWfTokenWrap')) return;
  tfEl('tfWfTokenWrap').style.display = a === 'token' ? 'block' : 'none';
  tfEl('tfWfKeyWrap').style.display = a === 'ssh' ? 'block' : 'none';
}

async function saveTfWorkspace(id) {
  const name = tfV('tfWfName').trim();
  if (!name) { tfDlgErr(1, tx('tfx_name_required')); return; }
  const source = tfV('tfWfSource');
  const body = {
    name, description: tfV('tfWfDesc'), group_name: tfV('tfWfGroup').trim(), environment: tfV('tfWfEnv').trim(), tags: tfCsv(tfV('tfWfTags')),

    binary: tfV('tfWfBinary'), source, drift_interval_minutes: parseInt(tfV('tfWfDrift'), 10) || 0,
  };
  if (source === 'inline') body.files = tfFeCollect();
  else {
    Object.assign(body, { git_url: tfV('tfWfGitUrl').trim(), git_branch: tfV('tfWfGitBranch').trim(), git_path: tfV('tfWfGitPath').trim(), git_auth: tfV('tfWfGitAuth') });
    if (body.git_auth === 'token' && tfV('tfWfGitToken')) body.git_token = tfV('tfWfGitToken');
    if (body.git_auth === 'ssh') body.git_key_id = parseInt(tfV('tfWfGitKey'), 10) || 0;
  }
  const r = await tfApi(id ? 'PUT' : 'POST', id ? '/api/terraform/workspaces/' + id : '/api/terraform/workspaces', body);
  if (!r.ok) { tfDlgErr(1, r.data.error || 'Error'); return; }
  tfDlgClose(1);
  showToast(tx('tfx_saved'), 'success');
  await loadTfWorkspaces();
  if (id && S.tfWs && S.tfWs.id === id) await tfReloadWs();
}

async function deleteTfWorkspace(id) {
  const w = S.tfWorkspaces.find(x => x.id === id) || S.tfWs;
  const ok = await showConfirm({
    title: t('tf_confirm_delete_ws_title', 'Delete Workspace'),
    message: (t('tf_confirm_delete_ws_msg', 'Delete workspace "{name}"?')).replace('{name}', w ? w.name : '#' + id),
    confirmText: t('delete', 'Delete'), danger: true,
  });
  if (!ok) return;
  const r = await tfApi('DELETE', '/api/terraform/workspaces/' + id);
  if (!r.ok) { tfErr(r); return; }
  showToast(tx('tfx_deleted'), 'success');
  if (S.tfWs && S.tfWs.id === id) closeTfWorkspace();
  await loadTfWorkspaces();
}

function tfDrawerOpen() { return tfEl('tfDrawerRoot').classList.contains('open'); }

async function tfConfirmLeave() {
  if (!S.tfDirtyGuard) return true;
  const ok = await S.tfDirtyGuard();
  if (ok) S.tfDirtyGuard = null;
  return ok;
}

async function tfSwitchTabGuarded(tab) { if (await tfConfirmLeave()) switchTfWsTab(tab); }

async function openTfWorkspace(id, tab) {
  if (tfDrawerOpen() && !(await tfConfirmLeave())) return;
  const r = await tfApi('GET', '/api/terraform/workspaces/' + id);
  if (!r.ok) { tfErr(r); return; }
  S.tfWs = r.data.workspace;
  S.tfRunId = 0;
  tfShowDrawer();
  switchTfWsTab(tab || 'overview');
}

function tfShowDrawer() {
  const root = tfEl('tfDrawerRoot');
  tfApplyDrawerWide();
  if (root.hidden) {
    root.hidden = false;
    // double rAF so the transition from the hidden state runs
    requestAnimationFrame(() => requestAnimationFrame(() => root.classList.add('open')));
  } else {
    root.classList.add('open');
  }
}

async function closeTfWorkspace() {
  if (!(await tfConfirmLeave())) return;
  tfStopRunPoll();
  const root = tfEl('tfDrawerRoot');
  root.classList.remove('open');
  if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) root.hidden = true;
  else setTimeout(() => { if (!root.classList.contains('open')) root.hidden = true; }, 200);
  S.tfWs = null;
  loadTfWorkspaces();
}

function tfToggleDrawerWide() {
  const drawer = tfEl('tfDrawer');
  const wide = !drawer.classList.contains('wide');
  drawer.classList.toggle('wide', wide);
  localStorage.setItem('pantau_tf_drawer_wide', wide ? '1' : '0');
  const btn = tfEl('tfDrawerWide');
  const key = wide ? 'tf_drawer_collapse' : 'tf_drawer_expand';
  btn.title = t(key, wide ? 'Collapse Drawer' : 'Expand Drawer');
  btn.setAttribute('aria-label', btn.title);
  btn.setAttribute('data-i18n-title', key);
}

function tfApplyDrawerWide() {
  if (localStorage.getItem('pantau_tf_drawer_wide') === '1') tfEl('tfDrawer').classList.add('wide');
}

async function tfReloadWs() {
  if (!S.tfWs) return;
  const r = await tfApi('GET', '/api/terraform/workspaces/' + S.tfWs.id);
  if (r.ok) S.tfWs = r.data.workspace;
  tfRenderWsHead();
}

function tfRenderWsHead() {
  if (!S.tfWs) return;
  tfEl('tfWsModalName').textContent = S.tfWs.name;
  tfEl('tfWsHeadBadges').innerHTML = tfBadge(S.tfWs.status)
    + (S.tfWs.drift_detected ? ` <span class="badge badge-drifted">${S.tfE(t('tf_status_drifted', 'Drifted'))}</span>` : '')
    + (S.tfWs.locked ? ` <span class="badge badge-locked">${svgIcon('lock', 'icon-xs')} ${S.tfE(S.tfWs.lock_reason || S.tfWs.locked_by || t('tf_status_locked', 'Locked'))}</span>` : '')
    + (S.tfWs.environment ? ` <span class="badge">${S.tfE(S.tfWs.environment)}</span>` : '')
    + (S.tfWs.group_name ? ` <span class="badge" style="background:var(--surface-2);border:1px solid var(--card-border);">${svgIcon('folder', 'icon-xs')} ${S.tfE(S.tfWs.group_name)}</span>` : '');

  tfEl('tfWsTabs').innerHTML = S.TF_WS_TABS.map(([k, key]) => `<button type="button" class="tab ${k === S.tfWsTab ? 'active' : ''}" role="tab" aria-selected="${k === S.tfWsTab}" onclick="tfSwitchTabGuarded('${k}')">${S.tfE(t(key, k))}</button>`).join('');
}

function switchTfWsTab(tab) {
  tfStopRunPoll();
  S.tfWsTab = tab;
  tfRenderWsHead();
  const body = tfEl('tfWsBody');
  body.innerHTML = '';
  const builtin = { overview: tfRenderOverview, runs: tfRenderRuns, state: tfRenderState, variables: tfRenderVariables, drift: tfRenderDrift };
  (S.tfTabRenderers[tab] || builtin[tab] || (b => { b.innerHTML = tfEmpty(t('tf_tab_config_missing', 'Configuration editor unavailable')); }))(body);
}

async function tfRenderOverview(body) {
  const w = S.tfWs;
  body.innerHTML = `<div class="tf-ov-grid">
    <div class="card tf-ov-card"><div class="card-title"><span>${S.tfE(t('tf_tab_overview', 'Overview'))}</span></div>
      <div style="display:grid;grid-template-columns:auto 1fr;gap:6px 14px;font-size:var(--fs-xs);">
        <span style="color:var(--text-muted);">${S.tfE(tx('tfx_environment'))}</span><span>${S.tfE(w.environment || '—')}</span>
        <span style="color:var(--text-muted);">${S.tfE(tx('tfx_binary'))}</span><span class="mono">${S.tfE(w.binary)} ${S.tfE(w.tf_version || '')}</span>
        <span style="color:var(--text-muted);">${S.tfE(tx('tfx_source'))}</span><span>${w.source === 'git' ? S.tfE(w.git_url + (w.git_branch ? '@' + w.git_branch : '')) : S.tfE(tx('tfx_source_inline'))}</span>
        <span style="color:var(--text-muted);">${S.tfE(t('tf_col_resources', 'Resources'))}</span><span>${w.resource_count || 0}</span>
        <span style="color:var(--text-muted);">${S.tfE(t('tf_col_last_run', 'Last Run'))}</span><span>${w.last_run ? tfBadge(w.last_run.status) + ' #' + w.last_run.id + ' · ' + S.tfE(tfTime(w.last_run.finished_at)) : '—'}</span>
        <span style="color:var(--text-muted);">${S.tfE(t('tf_tab_drift', 'Drift'))}</span><span>${w.drift_detected ? `<span class="badge badge-drifted">${S.tfE(t('tf_drift_detected_alert', 'Drift Detected'))}</span>` : S.tfE(t('tf_drift_none_alert', 'No drift'))}</span>
        <span style="color:var(--text-muted);">${S.tfE(t('tf_ov_cloud', 'Cloud accounts'))}</span><span style="display:flex;gap:4px;flex-wrap:wrap;">${(w.cloud_accounts || []).map(a => `<span class="badge">${S.tfE(a.name)} (${S.tfE(a.provider)})</span>`).join('') || '<span style="color:var(--text-muted);">—</span>'}</span>
        ${w.locked ? `<span style="color:var(--text-muted);">${S.tfE(tx('tfx_lock_card'))}</span><span>${svgIcon('lock', 'icon-xs')} ${S.tfE(tx('tfx_locked_by', { by: w.locked_by || '' }))} ${S.tfE(w.lock_reason || '')}</span>` : ''}
      </div>
      <div class="tf-act-row" style="margin-top:6px;">
        <button type="button" class="btn btn-sm btn-primary" onclick="openTfRunDialog('plan')">${svgIcon('play', 'icon-xs')} ${S.tfE(t('tf_run_type_plan', 'Plan'))}</button>
        <button type="button" class="btn btn-sm" onclick="openTfRunDialog('refresh')">${svgIcon('refresh-cw', 'icon-xs')} ${S.tfE(t('tf_run_type_refresh', 'Refresh-only'))}</button>
        <button type="button" class="btn btn-sm btn-danger" onclick="openTfRunDialog('destroy')">${svgIcon('trash-2', 'icon-xs')} ${S.tfE(t('tf_run_type_destroy', 'Destroy'))}</button>
        ${window.IacScan ? window.IacScan.btnHtml('terraform', w.id) : ''}
        <span id="tfIacChip"></span>
      </div></div>
    <div class="card tf-ov-card"><div class="card-title"><span>${S.tfE(tx('tfx_outputs'))}</span>
      <button type="button" class="btn btn-sm" id="tfOutRevealBtn" onclick="tfToggleReveal()">${svgIcon('eye', 'icon-xs')} ${S.tfE(t('tf_reveal_sensitive', 'Reveal Sensitive'))}</button></div>
      <div id="tfOutputs"></div></div>
    <div class="card tf-ov-card"><div class="card-title"><span>${S.tfE(t('tf_drawer_recent_runs', 'Recent Runs'))}</span></div><div id="tfRecentRuns"></div></div></div>`;
  S.tfOutputsRevealed = false;
  await tfLoadOutputs();
  const recent = tfEl('tfRecentRuns');
  if (recent) {
    const runs = await tfLoadRunList();
    recent.innerHTML = runs.slice(0, 5).map(x => `<div style="display:flex;justify-content:space-between;gap:8px;padding:6px 0;border-bottom:1px solid var(--card-border);font-size:var(--fs-xs);"><span><b>#${x.id}</b> ${S.tfE(x.type)}</span><span>${tfBadge(x.status)} <span style="color:var(--text-muted);">${S.tfE(tfTime(x.finished_at || x.created_at))}</span></span></div>`).join('') || tfEmpty(tx('tfx_no_runs'));
  }
  if (window.IacScan) {
    const scan = await window.IacScan.latest('terraform', w.id);
    const chip = tfEl('tfIacChip');
    if (chip) chip.innerHTML = scan ? window.IacScan.chipHtml('terraform', w.id, { summary: scan.summary }) : '';
  }
}

async function tfLoadOutputs() {
  const r = await tfApi('GET', `/api/terraform/workspaces/${S.tfWs.id}/outputs${S.tfOutputsRevealed ? '?reveal=1' : ''}`);
  S.tfOutputs = r.ok ? (r.data.outputs || []) : [];
  const box = tfEl('tfOutputs');
  if (!box) return;
  if (!r.ok) { box.innerHTML = tfEmpty(r.data.error || ''); return; }
  if (!S.tfOutputs.length) { box.innerHTML = tfEmpty(t('tf_no_outputs', 'No outputs')); return; }
  box.innerHTML = `<table class="ans-sched-tbl" style="width:100%;border-collapse:collapse;font-size:var(--fs-xs);"><tbody>${S.tfOutputs.map((o, i) => {
    const hidden = o.sensitive && (o.value_hidden || o.value == null);
    const val = hidden ? tx('tfx_sensitive_val') : (typeof o.value === 'string' ? o.value : JSON.stringify(o.value));
    return `<tr><td class="mono" style="white-space:nowrap;">${S.tfE(o.name)}${o.sensitive ? ' ' + svgIcon('lock', 'icon-xs') : ''}</td>
      <td class="mono" style="word-break:break-all;${hidden ? 'color:var(--text-muted);' : ''}">${S.tfE(val)}</td>
      <td style="text-align:right;"><button type="button" class="btn btn-icon btn-sm" title="${S.tfE(tx('tfx_copy'))}" onclick="tfCopyOutput(${i})">${svgIcon('copy', 'icon-xs')}</button></td></tr>`;
  }).join('')}</tbody></table>`;
  const btn = tfEl('tfOutRevealBtn');
  if (btn) {
    btn.innerHTML = `${svgIcon(S.tfOutputsRevealed ? 'eye-off' : 'eye', 'icon-xs')} ${S.tfE(S.tfOutputsRevealed ? t('tf_hide_sensitive', 'Hide Sensitive') : t('tf_reveal_sensitive', 'Reveal Sensitive'))}`;
    btn.style.display = S.tfOutputs.some(o => o.sensitive) ? '' : 'none';
  }
}

async function tfToggleReveal() { S.tfOutputsRevealed = !S.tfOutputsRevealed; await tfLoadOutputs(); }

async function tfCopyOutput(i) {
  let o = S.tfOutputs[i];
  if (o.sensitive && (o.value_hidden || o.value == null)) {
    const r = await tfApi('GET', `/api/terraform/workspaces/${S.tfWs.id}/outputs?reveal=1`);
    const full = r.ok && (r.data.outputs || []).find(x => x.name === o.name);
    if (!full) { showToast(tx('tfx_reveal_failed'), 'error'); return; }
    o = full;
  }
  tfCopy(typeof o.value === 'string' ? o.value : JSON.stringify(o.value));
}

function tfStopRunPoll() { if (S.tfRunTimer) { clearInterval(S.tfRunTimer); S.tfRunTimer = null; } }

function openTfRunDialog(type) {
  const body = `<div class="dk-form-grid">
    ${tfField(tx('tfx_run_type'), tfSelect('tfRunType', [['plan', t('tf_run_type_plan', 'Plan')], ['destroy', t('tf_run_type_destroy', 'Destroy')], ['refresh', t('tf_run_type_refresh', 'Refresh-only')]], type || 'plan', 'tfRunTypeChanged()'))}
    <div class="dk-field" style="display:flex;align-items:flex-end;">${tfCheck('tfRunSpec', t('tf_run_speculative', 'Speculative plan'), false)}</div>
    ${tfField(tx('tfx_message'), tfInput('tfRunMsg', '', `placeholder="${S.tfE(t('tf_run_message_ph', ''))}"`), '', true)}
    ${tfField(tx('tfx_targets'), tfInput('tfRunTargets', '', `placeholder="${S.tfE(t('tf_run_targets_ph', ''))}"`), '', true)}
    ${tfField(tx('tfx_replace'), tfInput('tfRunReplace', '', `placeholder="${S.tfE(t('tf_run_replace_ph', ''))}"`), '', true)}</div>`;
  tfDlg(1, t('tf_run_start_title', 'Start Terraform Run'), body, tfFoot(1, tfPrimary(tx('tfx_start'), 'startTfRun()')), 720);
  tfRunTypeChanged();
}

function tfRunTypeChanged() {
  const spec = tfEl('tfRunSpec');
  if (spec && tfV('tfRunType') !== 'plan') spec.checked = false;
  if (spec) spec.disabled = tfV('tfRunType') !== 'plan';
}

async function startTfRun() {
  const r = await tfApi('POST', `/api/terraform/workspaces/${S.tfWs.id}/runs`, {
    type: tfV('tfRunType'), speculative: tfChk('tfRunSpec'), message: tfV('tfRunMsg'),
    targets: tfCsv(tfV('tfRunTargets')), replace: tfCsv(tfV('tfRunReplace')),
  });
  if (!r.ok) { tfDlgErr(1, r.data.error || 'Error'); return; }
  tfDlgClose(1);
  showToast(tx('tfx_run_started'), 'success');
  S.tfRunId = r.data.run.id;
  await tfReloadWs();
  switchTfWsTab('runs');
}

async function tfRenderRuns(body) {
  body.innerHTML = `<div style="display:flex;gap:16px;align-items:flex-start;">
    <div style="flex:0 1 290px;min-width:220px;display:flex;flex-direction:column;gap:8px;">
      <div id="tfRunStartBar" style="display:flex;justify-content:center;">
        <button type="button" id="tfRunStartBtn" class="btn btn-sm btn-primary" onclick="openTfRunDialog('plan')">${svgIcon('play', 'icon-xs')} ${S.tfE(t('tf_btn_start_run', 'Start Run'))}</button>
      </div>
      <div id="tfRunList" style="display:flex;flex-direction:column;gap:6px;"></div></div>
    <div id="tfRunDetail" style="flex:1;min-width:0;display:flex;flex-direction:column;gap:14px;"></div></div>`;
  const runs = await tfLoadRunList();
  if (!runs.length) {
    S.tfRunId = 0;
    body.innerHTML = `<div class="tf-runs-empty"><div class="tf-runs-empty-icon">${svgIcon('play', 'icon-lg')}</div>
      <div class="tf-runs-empty-title">${S.tfE(tx('tfx_no_runs'))}</div>
      <div class="tf-runs-empty-hint">${S.tfE(tx('tfx_no_runs_hint'))}</div>
      <div id="tfRunStartBar"><button type="button" id="tfRunStartBtn" class="btn btn-sm btn-primary" onclick="openTfRunDialog('plan')">${svgIcon('play', 'icon-xs')} ${S.tfE(t('tf_btn_start_run', 'Start Run'))}</button></div></div>`;
    return;
  }
  if (S.tfRunId) await tfOpenRun(S.tfRunId);
  else await tfOpenRun(runs[0].id);
}

async function tfLoadRunList() {
  const r = await tfApi('GET', `/api/terraform/workspaces/${S.tfWs.id}/runs?limit=50`);
  const runs = r.ok ? (r.data.runs || []) : [];
  const box = tfEl('tfRunList');
  if (!box) return runs;
  box.innerHTML = runs.length ? runs.map(x => `<div class="card" style="padding:10px;gap:4px;cursor:pointer;${x.id === S.tfRunId ? 'border-color:var(--primary);' : ''}" onclick="tfOpenRun(${x.id})">
    <div style="display:flex;justify-content:space-between;align-items:center;gap:6px;"><b>#${x.id}</b>${tfBadge(x.status)}</div>
    <div style="font-size:var(--fs-xs);color:var(--text-muted);">${S.tfE(x.type)}${x.speculative ? ' · spec' : ''} · ${S.tfE(x.trigger_kind)} · ${S.tfE(tfTime(x.created_at))}</div>
    <div class="mono" style="font-size:var(--fs-xs);"><span style="color:var(--success);">+${x.counts.create}</span> <span style="color:var(--warning);">~${x.counts.update}</span> <span style="color:var(--danger);">-${x.counts.delete}</span>${x.counts.replace ? ` <span style="color:var(--danger);">-/+${x.counts.replace}</span>` : ''}</div></div>`).join('') : tfEmpty(tx('tfx_no_runs'));
  if (!runs.length && tfEl('tfRunDetail')) tfEl('tfRunDetail').innerHTML = '';
  return runs;
}

async function tfOpenRun(id) {
  tfStopRunPoll();
  S.tfRunId = id;
  S.tfLogOffset = 0;
  S.tfLogText = '';
  const r = await tfApi('GET', '/api/terraform/runs/' + id);
  if (!r.ok) { tfErr(r); return; }
  tfRenderRun(r.data.run);
  tfLoadRunList();
  await tfPollLog();
  if (S.TF_ACTIVE_RUN.includes(r.data.run.status)) {
    let tick = 0;
    S.tfRunTimer = setInterval(async () => {
      if (!tfDrawerOpen() || S.tfRunId !== id) { tfStopRunPoll(); return; }
      const done = await tfPollLog();
      if (++tick % 3 === 0) tfLoadRunList();
      if (done) { tfStopRunPoll(); await tfOpenRunFinal(id); }
    }, 1000);
  }
}

async function tfOpenRunFinal(id) {
  const r = await tfApi('GET', '/api/terraform/runs/' + id);
  if (r.ok && S.tfRunId === id) tfRenderRun(r.data.run);
  tfLoadRunList();
  tfReloadWs();
  const pre = tfEl('tfRunLog');
  if (pre) pre.textContent = S.tfLogText;
}

async function tfPollLog() {
  const r = await tfApi('GET', `/api/terraform/runs/${S.tfRunId}/log?offset=${S.tfLogOffset}`);
  if (!r.ok) {
    // A transient poll failure is not the end of the run: keep polling and only give up after several
    // consecutive failures, so a short network hiccup never stops the log of a running plan/apply.
    S.tfPollFails++;
    if (S.tfPollFails <= S.TF_POLL_MAX_FAILS) return false;
    S.tfLogText += '\n' + tx('tfx_poll_failed') + '\n';
    const pre = tfEl('tfRunLog');
    if (pre) pre.textContent = S.tfLogText;
    showToast(tx('tfx_poll_failed'), 'error');
    return true;
  }
  S.tfPollFails = 0;
  if (r.data.log) {
    S.tfLogText += r.data.log;
    const pre = tfEl('tfRunLog');
    if (pre) {
      const atEnd = pre.scrollTop + pre.clientHeight >= pre.scrollHeight - 30;
      pre.textContent = S.tfLogText;
      if (atEnd) pre.scrollTop = pre.scrollHeight;
    }
  }
  S.tfLogOffset = r.data.next_offset;
  return !!r.data.done;
}

function tfVal(v, d) {
  if (d.unknown) return tx('tfx_known_after_apply');
  if (d.sensitive) return tx('tfx_sensitive_val');
  const s = v === undefined ? 'null' : JSON.stringify(v);
  return s.length > 240 ? s.slice(0, 240) + '…' : s;
}

function tfDiffHTML(changes) {
  if (!changes || !changes.length) return tfEmpty(tx('tfx_no_changes'));
  const sym = { create: '+', update: '~', replace: '-/+', delete: '-', read: '<=' };
  return changes.map(c => {
    const rows = (c.diffs || []).map(d => {
      const p = S.tfE(d.path);
      if (c.action === 'create' || (d.before == null && d.after != null && c.action !== 'delete')) return `<div class="tf-diff-row-add">+ ${p} = ${S.tfE(tfVal(d.after, d))}</div>`;
      if (c.action === 'delete') return `<div class="tf-diff-row-del">- ${p} = ${S.tfE(tfVal(d.before, { sensitive: d.sensitive }))}</div>`;
      return `<div class="tf-diff-row-mod">~ ${p}: ${S.tfE(tfVal(d.before, { sensitive: d.sensitive }))} → ${S.tfE(tfVal(d.after, d))}</div>`;
    }).join('') || `<div style="color:var(--text-muted);">—</div>`;
    return `<div class="tf-diff-card tf-diff-${S.tfE(c.action)}">
      <div class="tf-diff-header" onclick="tfToggleDiff(this)"><span style="display:flex;gap:10px;align-items:center;min-width:0;"><b class="mono" style="min-width:26px;">${sym[c.action] || ''}</b><span class="mono" style="overflow:hidden;text-overflow:ellipsis;">${S.tfE(c.address)}</span></span>
        <span class="badge">${S.tfE(c.action)}${c.reason ? ' · ' + S.tfE(c.reason) : ''}</span></div>
      <div class="tf-diff-body" style="display:none;">${rows}</div></div>`;
  }).join('');
}

function tfToggleDiff(h) { const b = h.nextElementSibling; b.style.display = b.style.display === 'none' ? 'block' : 'none'; }

function tfDiffAll(open) { document.querySelectorAll('#tfRunDetail .tf-diff-body').forEach(b => { b.style.display = open ? 'block' : 'none'; }); }

function tfPolicyHTML(pol) {
  if (!pol || !pol.status) return '';
  const lbl = { passed: t('tf_policy_pass', 'Passed'), warning: t('tf_policy_warning', 'Warning'), failed: t('tf_policy_failed', 'Failed') }[pol.status] || pol.status;
  const color = { passed: 'success', warning: 'warning', failed: 'danger' }[pol.status] || 'text-muted';
  const results = (pol.results || []).map(r => `<div style="font-size:var(--fs-xs);margin-top:4px;">${r.passed ? svgIcon('circle-check', 'icon-xs') : svgIcon('triangle-alert', 'icon-xs')} <b>${S.tfE(r.name)}</b> <span class="badge">${S.tfE(r.enforcement)}</span>
    ${(r.violations || []).map(v => `<div class="mono" style="margin-left:20px;color:var(--${r.enforcement === 'hard' ? 'danger' : 'warning'});">${S.tfE(v.address ? v.address + ': ' : '')}${S.tfE(v.message)}</div>`).join('')}</div>`).join('');
  return `<div class="card" style="border-left:4px solid var(--${color});gap:6px;"><div style="font-weight:600;color:var(--${color});">${svgIcon('shield', 'icon-sm')} ${S.tfE(tx('tfx_policy_check'))}: ${S.tfE(lbl)}</div>${results}</div>`;
}

function tfCostHTML(cost) {
  let inner;
  if (!cost || !cost.available) inner = `<span style="color:var(--text-muted);">${S.tfE(cost && cost.error && !/not installed/i.test(cost.error) ? cost.error : t('tf_cost_not_installed', 'Infracost not installed'))}</span>`;
  else {
    const cur = cost.currency || 'USD';
    let fmt;
    try { const nf = new Intl.NumberFormat(undefined, { style: 'currency', currency: cur }); fmt = v => nf.format(Math.abs(v)); } catch (e) { fmt = v => Math.abs(v).toFixed(2) + ' ' + cur; }
    const sign = cost.delta >= 0 ? '+' : '-';
    inner = `<span style="font-size:var(--fs-lg);font-weight:600;color:var(--${cost.delta > 0 ? 'warning' : 'success'});">${sign}${S.tfE(fmt(cost.delta))}${S.tfE(tx('tfx_per_month'))} (${cost.delta_pct >= 0 ? '+' : ''}${Number(cost.delta_pct || 0).toFixed(0)}%)</span>
      <span style="color:var(--text-muted);font-size:var(--fs-xs);">${S.tfE(fmt(cost.monthly_before))} → ${S.tfE(fmt(cost.monthly_after))}</span>`;
  }
  return `<div class="card" style="gap:6px;"><div class="card-title"><span>${S.tfE(t('tf_cost_delta', 'Monthly Cost Delta'))}</span></div><div style="display:flex;gap:12px;align-items:baseline;flex-wrap:wrap;">${inner}</div></div>`;
}

function tfRenderRun(run) {
  const box = tfEl('tfRunDetail');
  if (!box) return;
  const plan = run.plan || {};
  const active = S.TF_ACTIVE_RUN.includes(run.status);
  const counts = tx('tfx_plan_counts', { c: plan.create || 0, u: plan.update || 0, r: plan.replace || 0, d: plan.delete || 0 });
  const changes = run.type === 'drift' ? (run.drift || []) : (plan.changes || []);
  const needDecision = run.status === 'awaiting_approval';
  const warn = run.policy && run.policy.status === 'warning';
  // While the run is pending/running the status badge gets a spinner and a live elapsed counter.
  // The counter starts at the run start (falling back to creation) and disappears when the run finishes.
  const runT0 = run.started_at ? new Date(run.started_at).getTime() : (run.created_at ? new Date(run.created_at).getTime() : 0);
  const elapsed = active && runT0 ? `<span class="badge" style="display:inline-flex;align-items:center;gap:4px;" role="status">${svgIcon('loader-circle', 'icon-xs')}<span data-sp-t0="${runT0}" style="font-variant-numeric:tabular-nums;">0s</span></span>` : '';
  const gate = needDecision ? `<div class="card" style="border-left:4px solid var(--primary);gap:8px;">
      <div class="card-title"><span>${svgIcon('shield-alert', 'icon-sm')} ${S.tfE(t('tf_approval_title', 'Approval Gate'))}</span></div>
      <textarea id="tfApprComment" class="form-input" rows="2" style="width:100%;box-sizing:border-box;" placeholder="${S.tfE(t('tf_approval_comment_ph', ''))}"></textarea>
      ${warn ? tfCheck('tfApprOverride', t('tf_approval_override_policy', 'Override policy warnings'), false) : ''}
      <div id="tfApprErr" role="alert" style="display:none;color:var(--danger);font-size:var(--fs-xs);"></div>
      <div class="tf-act-row"><button type="button" id="tfApprApply" class="btn btn-sm btn-primary" onclick="tfApplyRun(${run.id})">${svgIcon('check', 'icon-xs')} ${S.tfE(t('tf_btn_confirm_apply', 'Confirm & Apply'))}</button>
      <button type="button" class="btn btn-sm btn-danger" onclick="tfDiscardRun(${run.id})">${svgIcon('x', 'icon-xs')} ${S.tfE(t('tf_btn_discard_run', 'Discard Run'))}</button></div></div>` : '';
  const decided = run.decision ? `<div class="card" style="gap:4px;font-size:var(--fs-xs);"><b>${S.tfE(tx('tfx_decision'))}: ${S.tfE(run.decision)}</b><span>${S.tfE(run.decision_comment || '')}</span></div>` : '';
  box.innerHTML = `<div class="card" style="gap:8px;">
      <div style="display:flex;justify-content:space-between;align-items:center;gap:8px;flex-wrap:wrap;">
        <div style="display:flex;gap:8px;align-items:center;"><b style="font-size:var(--fs-md);">${S.tfE(tx('tfx_run_n', { n: run.id }))}</b>${tfBadge(run.status)}${elapsed}<span class="badge">${S.tfE(run.type)}${run.speculative ? ' · speculative' : ''}</span></div>
        ${active ? `<button type="button" class="btn btn-sm btn-danger" onclick="tfCancelRun(${run.id})">${svgIcon('square', 'icon-xs')} ${S.tfE(t('tf_btn_cancel_run', 'Cancel Run'))}</button>` : ''}</div>
      <div style="display:grid;grid-template-columns:auto 1fr;gap:4px 14px;font-size:var(--fs-xs);">
        <span style="color:var(--text-muted);">${S.tfE(tx('tfx_trigger'))}</span><span>${S.tfE(run.trigger_kind)}</span>
        ${run.message ? `<span style="color:var(--text-muted);">${S.tfE(tx('tfx_message'))}</span><span>${S.tfE(run.message)}</span>` : ''}
        ${run.commit_sha ? `<span style="color:var(--text-muted);">${S.tfE(tx('tfx_commit'))}</span><span class="mono">${S.tfE(run.commit_sha.slice(0, 12))}</span>` : ''}
        ${(run.targets || []).length ? `<span style="color:var(--text-muted);">${S.tfE(tx('tfx_targets'))}</span><span class="mono">${S.tfE(run.targets.join(', '))}</span>` : ''}
        <span style="color:var(--text-muted);">${S.tfE(tx('tfx_created'))}</span><span>${S.tfE(tfTime(run.created_at))}</span>
        <span style="color:var(--text-muted);">${S.tfE(tx('tfx_finished'))}</span><span>${S.tfE(tfTime(run.finished_at))}</span></div></div>
    ${run.has_changes || run.status === 'awaiting_approval' ? `<div class="mono" style="font-size:var(--fs-sm);"><span style="color:var(--success);">+${plan.create || 0}</span> · <span style="color:var(--warning);">~${plan.update || 0}</span> · <span style="color:var(--danger);">-${plan.delete || 0}</span> · <span style="color:var(--danger);">-/+${plan.replace || 0}</span> <span style="color:var(--text-muted);font-size:var(--fs-xs);">${S.tfE(counts)}</span></div>` : ''}
    ${tfPolicyHTML(run.policy)}
    ${run.cost && (run.cost.available || run.cost.error) || run.has_changes ? tfCostHTML(run.cost) : ''}
    ${gate}${decided}
    ${changes.length || run.has_changes ? `<div><div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:6px;"><b>${S.tfE(run.type === 'drift' ? t('tf_tab_drift', 'Drift') : tx('tfx_plan_diff'))}</b>
      <span><button type="button" class="btn btn-sm btn-ghost" onclick="tfDiffAll(true)">${S.tfE(tx('tfx_expand_all'))}</button><button type="button" class="btn btn-sm btn-ghost" onclick="tfDiffAll(false)">${S.tfE(tx('tfx_collapse_all'))}</button></span></div>${tfDiffHTML(changes)}</div>` : ''}
    <div><b>${S.tfE(tx('tfx_console'))}</b><pre id="tfRunLog" class="tf-log" style="margin-top:6px;"></pre></div>`;
  const pre = tfEl('tfRunLog');
  if (pre) pre.textContent = S.tfLogText;
}

async function tfApplyRun(id, force) {
  if (S.tfApplyBusy) return; // the gate scan can take minutes: no double apply
  const comment = tfV('tfApprComment').trim();
  const err = tfEl('tfApprErr');
  const showErr = (m) => { if (err) { err.textContent = m; err.style.display = 'block'; } };
  if (comment.length < 3) { showErr(tx('tfx_comment_required')); return; }
  const btn = tfEl('tfApprApply');
  const restore = SP.busy(btn, tx('tfx_applying'));
  S.tfApplyBusy = true;
  let r;
  try {
    r = await tfApi('POST', `/api/terraform/runs/${id}/apply`, { comment, override_policy: tfChk('tfApprOverride'), ignore_iac_gate: !!force });
  } finally {
    // The spinner must stop on success, on an HTTP error and on a network failure alike.
    S.tfApplyBusy = false;
    restore();
  }
  if (!r.ok) {
    if (r.status === 409 && r.data.findings_summary && window.IacScan) {
      window.IacScan.gateBlocked(r.data.findings_summary, () => tfApplyRun(id, true));
      return;
    }
    showErr(r.data.error || 'Error');
    return;
  }
  showToast(tx('tfx_apply_ok'), 'success');
  await tfOpenRun(id);
  tfReloadWs();
}

async function tfDiscardRun(id) {
  const comment = tfV('tfApprComment').trim();
  const err = tfEl('tfApprErr');
  if (comment.length < 3) { err.textContent = tx('tfx_comment_required'); err.style.display = 'block'; return; }
  const ok = await showConfirm({ title: t('tf_btn_discard_run', 'Discard Run'), message: tx('tfx_discard_confirm'), confirmText: t('tf_btn_discard_run', 'Discard Run'), danger: true });
  if (!ok) return;
  const r = await tfApi('POST', `/api/terraform/runs/${id}/discard`, { comment });
  if (!r.ok) { err.textContent = r.data.error || 'Error'; err.style.display = 'block'; return; }
  showToast(tx('tfx_discard_ok'), 'success');
  await tfOpenRun(id);
  tfReloadWs();
}

async function tfCancelRun(id) {
  const ok = await showConfirm({ title: t('tf_btn_cancel_run', 'Cancel Run'), message: t('tf_cancel_confirm', 'Cancel this run?'), confirmText: t('tf_btn_cancel_run', 'Cancel Run'), danger: true });
  if (!ok) return;
  const r = await tfApi('POST', `/api/terraform/runs/${id}/cancel`);
  if (!r.ok) tfErr(r);
}

function tfLockCardHTML() {
  const w = S.tfWs;
  const btns = !w.locked
    ? `<button type="button" class="btn btn-sm" onclick="openTfLockDialog()">${svgIcon('lock', 'icon-xs')} ${S.tfE(t('tf_btn_lock', 'Lock'))}</button>`
    : `${w.locked_by === 'manual' ? `<button type="button" class="btn btn-sm" onclick="tfUnlock()">${svgIcon('lock-open', 'icon-xs')} ${S.tfE(t('tf_btn_unlock', 'Unlock'))}</button>` : ''}
       <button type="button" class="btn btn-sm btn-danger" onclick="tfForceUnlock()">${svgIcon('triangle-alert', 'icon-xs')} ${S.tfE(t('tf_btn_force_unlock', 'Force Unlock'))}</button>`;
  return `<div class="card" style="flex-direction:row;align-items:center;justify-content:space-between;flex-wrap:wrap;">
    <div style="display:flex;gap:10px;align-items:center;">${svgIcon(w.locked ? 'lock' : 'lock-open', 'icon-sm')}<div><b>${S.tfE(tx('tfx_lock_card'))}</b>
      <div style="font-size:var(--fs-xs);color:var(--text-muted);">${w.locked ? S.tfE(tx('tfx_locked_by', { by: w.locked_by || '' }) + (w.lock_reason ? ' — ' + w.lock_reason : '')) : S.tfE(tx('tfx_unlocked'))}</div></div></div>
    <div style="display:flex;gap:8px;">${btns}</div></div>`;
}

function openTfLockDialog() {
  tfDlg(1, t('tf_btn_lock', 'Lock'), tfField(tx('tfx_lock_reason'), tfInput('tfLockReason', '', 'maxlength="200"')), tfFoot(1, tfPrimary(t('tf_btn_lock', 'Lock'), 'tfLock()')), 460);
}

async function tfLock() {
  const r = await tfApi('POST', `/api/terraform/workspaces/${S.tfWs.id}/lock`, { reason: tfV('tfLockReason') });
  if (!r.ok) { tfDlgErr(1, r.data.error || 'Error'); return; }
  tfDlgClose(1);
  showToast(tx('tfx_locked_ok'), 'success');
  await tfAfterLockChange();
}

async function tfUnlock() {
  const r = await tfApi('POST', `/api/terraform/workspaces/${S.tfWs.id}/unlock`);
  if (!r.ok) { tfErr(r); return; }
  showToast(tx('tfx_unlocked_ok'), 'success');
  await tfAfterLockChange();
}

async function tfForceUnlock() {
  const ok = await showConfirm({ title: t('tf_force_unlock_confirm_title', 'Force Unlock'), message: t('tf_force_unlock_confirm_msg', ''), confirmText: t('tf_btn_force_unlock', 'Force Unlock'), danger: true });
  if (!ok) return;
  const r = await tfApi('POST', `/api/terraform/workspaces/${S.tfWs.id}/force-unlock`, { comment: 'force unlock from dashboard' });
  if (!r.ok) { tfErr(r); return; }
  showToast(tx('tfx_unlocked_ok'), 'success');
  await tfAfterLockChange();
}

async function tfAfterLockChange() { await tfReloadWs(); switchTfWsTab(S.tfWsTab); }

async function tfRenderState(body) {
  body.innerHTML = tfLockCardHTML() + `<div id="tfStateWrap" style="display:flex;flex-direction:column;gap:16px;margin-top:16px;"></div>`;
  const id = S.tfWs.id;
  const [sr, vr] = await Promise.all([tfApi('GET', `/api/terraform/workspaces/${id}/state`), tfApi('GET', `/api/terraform/workspaces/${id}/state/versions`)]);
  S.tfStateVersions = vr.ok ? (vr.data.versions || []) : [];
  S.tfStateRes = sr.ok ? (sr.data.resources || []) : [];
  const resources = S.tfStateRes;
  const wrap = tfEl('tfStateWrap');
  if (!wrap) return;
  const info = sr.ok && sr.data.serial !== undefined ? `<span class="mono" style="font-size:var(--fs-xs);color:var(--text-muted);">${S.tfE(tx('tfx_serial'))} ${sr.data.serial} · ${S.tfE(sr.data.lineage || '')} · tf ${S.tfE(sr.data.tf_version || sr.data.terraform_version || '')}</span>` : '';
  wrap.innerHTML = `<div class="card"><div class="card-title"><span>${S.tfE(tx('tfx_tab_resources'))} (${resources.length})</span>${info}</div>
      <input type="text" id="tfResFilter" class="form-input" placeholder="${S.tfE(tx('tfx_filter_addr'))}" oninput="tfFilterResources()" style="width:100%;box-sizing:border-box;" />
      <div id="tfResList" style="display:flex;flex-direction:column;gap:6px;max-height:380px;overflow:auto;"></div></div>
    <div class="card"><div class="card-title"><span>${S.tfE(t('tf_state_versions_title', 'State Versions'))}</span>
      <span style="display:flex;gap:8px;"><button type="button" class="btn btn-sm" onclick="tfDiffVersions()">${svgIcon('git-compare', 'icon-xs')} ${S.tfE(t('tf_btn_diff_versions', 'Diff Versions'))}</button>
      <button type="button" class="btn btn-sm" onclick="openTfImportDialog()">${svgIcon('upload', 'icon-xs')} ${S.tfE(t('tf_btn_import_state', 'Import State'))}</button></span></div>
      <div style="font-size:var(--fs-xs);color:var(--text-muted);">${svgIcon('triangle-alert', 'icon-xs')} ${S.tfE(t('tf_download_state_warning', tx('tfx_state_warn_dl')))}</div>
      <div id="tfVersions"></div><div id="tfStateDiff"></div></div>`;
  tfFilterResources();
  tfRenderVersions();
}

function tfFilterResources() {
  const box = tfEl('tfResList');
  if (!box) return;
  const q = tfV('tfResFilter').toLowerCase().trim();
  const list = S.tfStateRes.filter(r => !q || r.address.toLowerCase().includes(q));
  if (!list.length) { box.innerHTML = tfEmpty(S.tfStateRes.length ? tx('tfx_no_diff') : (t('tf_state_no_resources', '') || tx('tfx_no_state'))); return; }
  box.innerHTML = list.map(r => `<details style="border:1px solid var(--card-border);border-radius:var(--radius-sm);padding:6px 10px;">
    <summary style="cursor:pointer;display:flex;gap:10px;align-items:center;justify-content:space-between;"><span class="mono" style="font-size:var(--fs-xs);">${S.tfE(r.address)}</span><span style="font-size:var(--fs-xs);color:var(--text-muted);">${S.tfE(r.mode === 'data' ? 'data · ' : '')}${S.tfE(r.provider || '')}</span></summary>
    <pre class="tf-log" style="margin-top:6px;max-height:240px;">${S.tfE(JSON.stringify(r.attributes || {}, null, 2))}</pre></details>`).join('');
}

function tfRenderVersions() {
  const box = tfEl('tfVersions');
  if (!box) return;
  if (!S.tfStateVersions.length) { box.innerHTML = tfEmpty(tx('tfx_no_state')); return; }
  S.tfDiffSel = [];
  box.innerHTML = `<table class="ans-sched-tbl" style="width:100%;border-collapse:collapse;font-size:var(--fs-xs);"><thead><tr><th style="width:28px;"></th><th>${S.tfE(tx('tfx_serial'))}</th><th>${S.tfE(tx('tfx_source'))}</th><th>${S.tfE(t('tf_col_resources', 'Resources'))}</th><th>${S.tfE(tx('tfx_size'))}</th><th>${S.tfE(tx('tfx_created'))}</th><th>Run</th><th style="text-align:right;">${S.tfE(t('cf_col_actions', 'Actions'))}</th></tr></thead><tbody>
    ${S.tfStateVersions.map((v, i) => `<tr><td><input type="checkbox" data-vid="${v.id}" onchange="tfToggleDiffSel(this)" /></td>
      <td class="mono">#${v.serial}${i === 0 ? ` <span class="badge badge-applied">current</span>` : ''}</td><td>${S.tfE(v.source)}</td><td>${v.resource_count}</td><td>${(v.size / 1024).toFixed(1)} KB</td><td>${S.tfE(tfTime(v.created_at))}</td><td>${v.run_id ? '#' + v.run_id : '—'}</td>
      <td style="text-align:right;white-space:nowrap;">
        <a class="btn btn-icon btn-sm" href="/api/terraform/workspaces/${S.tfWs.id}/state/versions/${v.id}" download title="${S.tfE(t('tf_btn_download_state', 'Download'))}">${svgIcon('download', 'icon-xs')}</a>
        ${i === 0 ? '' : `<button type="button" class="btn btn-sm" onclick="openTfRollbackDialog(${v.id}, ${v.serial})">${S.tfE(t('tf_btn_rollback', 'Rollback'))}</button>`}</td></tr>`).join('')}</tbody></table>`;
}

function tfToggleDiffSel(cb) {
  const id = Number(cb.dataset.vid);
  S.tfDiffSel = S.tfDiffSel.filter(x => x !== id);
  if (cb.checked) S.tfDiffSel.push(id);
  if (S.tfDiffSel.length > 2) {
    const drop = S.tfDiffSel.shift();
    const el = document.querySelector(`#tfVersions input[data-vid="${drop}"]`);
    if (el) el.checked = false;
  }
}

async function tfDiffVersions() {
  if (S.tfDiffSel.length !== 2) { showToast(tx('tfx_select_two'), 'warning'); return; }
  const [from, to] = S.tfDiffSel.slice().sort((a, b) => a - b);
  const r = await tfApi('GET', `/api/terraform/workspaces/${S.tfWs.id}/state/diff?from=${from}&to=${to}`);
  if (!r.ok) { tfErr(r); return; }
  const d = r.data;
  const added = d.added || [], removed = d.removed || [], changed = d.changed || [];
  const out = tfEl('tfStateDiff');
  if (!added.length && !removed.length && !changed.length) { out.innerHTML = tfEmpty(tx('tfx_no_diff')); return; }
  out.innerHTML = `<div style="display:flex;flex-direction:column;gap:6px;margin-top:10px;">
    ${added.map(a => `<div class="tf-diff-row-add mono">+ ${S.tfE(a)}</div>`).join('')}
    ${removed.map(a => `<div class="tf-diff-row-del mono">- ${S.tfE(a)}</div>`).join('')}
    ${changed.map(c => `<div class="tf-diff-card tf-diff-update"><div class="tf-diff-header" onclick="tfToggleDiff(this)"><span class="mono">~ ${S.tfE(c.address)}</span><span class="badge">${S.tfE(tx('tfx_changed'))}</span></div>
      <div class="tf-diff-body" style="display:none;">${(c.changes || []).map(x => `<div class="tf-diff-row-mod">~ ${S.tfE(x.path)}: ${S.tfE(tfVal(x.before, {}))} → ${S.tfE(tfVal(x.after, {}))}</div>`).join('')}</div></div>`).join('')}</div>`;
}

function openTfRollbackDialog(vid, serial) {
  tfDlg(1, t('tf_rollback_confirm_title', 'Rollback State Version'),
    `<div style="font-size:var(--fs-xs);color:var(--text-muted);margin-bottom:10px;">${S.tfE((t('tf_rollback_confirm_msg', 'Rollback to serial #{serial}?')).replace('{serial}', serial))}</div>${tfField(tx('tfx_message'), tfInput('tfRbComment', '', `placeholder="${S.tfE(t('tf_rollback_comment_ph', ''))}"`))}`,
    tfFoot(1, tfPrimary(t('tf_btn_rollback', 'Rollback'), `tfRollback(${vid})`, true)), 480);
}

async function tfRollback(vid) {
  const r = await tfApi('POST', `/api/terraform/workspaces/${S.tfWs.id}/state/versions/${vid}/rollback`, { comment: tfV('tfRbComment') });
  if (!r.ok) { tfDlgErr(1, r.data.error || 'Error'); return; }
  tfDlgClose(1);
  showToast(tx('tfx_rollback_ok'), 'success');
  switchTfWsTab('state');
  tfReloadWs();
}

function openTfImportDialog() {
  tfDlg(1, tx('tfx_import_title'), `<div style="font-size:var(--fs-xs);color:var(--text-muted);margin-bottom:8px;">${S.tfE(tx('tfx_import_hint'))}</div>
    <input type="file" id="tfImpFile" accept=".tfstate,.json,application/json" onchange="tfImportFile(this)" style="margin-bottom:8px;" />
    <textarea id="tfImpText" class="form-input" rows="12" spellcheck="false" style="width:100%;box-sizing:border-box;font-family:var(--font-mono);font-size:var(--fs-xs);"></textarea>`,
    tfFoot(1, tfPrimary(t('tf_btn_import_state', 'Import State'), 'tfImportState()')), 640);
}

function tfImportFile(inp) {
  const f = inp.files && inp.files[0];
  if (!f) return;
  const rd = new FileReader();
  rd.onload = () => { tfEl('tfImpText').value = String(rd.result || ''); };
  rd.readAsText(f);
}

async function tfImportState() {
  const state = tfV('tfImpText').trim();
  if (!state) return;
  const r = await tfApi('POST', `/api/terraform/workspaces/${S.tfWs.id}/state/import`, { state });
  if (!r.ok) { tfDlgErr(1, r.data.error || 'Error'); return; }
  tfDlgClose(1);
  showToast(tx('tfx_import_ok'), 'success');
  switchTfWsTab('state');
  tfReloadWs();
}

function tfVarBase(sc) { return sc.kind === 'ws' ? `/api/terraform/workspaces/${sc.id}/variables` : `/api/terraform/varsets/${sc.id}/variables`; }

async function tfRenderVariables(body, effective) {
  const eff = !!effective;
  body.innerHTML = `<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px;flex-wrap:wrap;gap:8px;">
    <div style="display:flex;gap:6px;"><button type="button" class="btn btn-sm ${eff ? '' : 'btn-primary'}" onclick="tfRenderVariables(tfEl('tfWsBody'), false)">${S.tfE(t('tf_vars_tab_direct', 'Workspace Variables'))}</button>
    <button type="button" class="btn btn-sm ${eff ? 'btn-primary' : ''}" onclick="tfRenderVariables(tfEl('tfWsBody'), true)">${S.tfE(t('tf_vars_tab_effective', 'Effective View'))}</button></div>
    <div style="display:flex;gap:6px;flex-wrap:wrap;"><button type="button" id="tfVarDetectBtn" class="btn btn-sm" onclick="tfDetectVars()">${svgIcon('search', 'icon-xs')} ${S.tfE(tx('tfx_vd_btn'))}</button>
    <button type="button" class="btn btn-sm btn-primary" onclick="openTfVarDialog('ws', ${S.tfWs.id}, 0, 1)">${svgIcon('plus', 'icon-xs')} ${S.tfE(t('tf_btn_add_var', 'Add Variable'))}</button></div></div><div id="tfVarsBox"></div>`;
  S.tfVarScope = { kind: 'ws', id: S.tfWs.id, lvl: 1, refresh: () => tfRenderVariables(tfEl('tfWsBody'), eff) };
  const r = await tfApi('GET', `/api/terraform/workspaces/${S.tfWs.id}/variables${eff ? '?effective=1' : ''}`);
  S.tfVars = r.ok ? (r.data.variables || []) : [];
  tfEl('tfVarsBox').innerHTML = tfVarsTable(S.tfVars, eff, 'ws', S.tfWs.id, 1);
}

function tfVarsTable(vars, eff, kind, scopeId, lvl) {
  if (!vars.length) return tfEmpty(tx('tfx_no_vars'));
  return `<div style="overflow-x:auto;border:1px solid var(--card-border);border-radius:var(--radius-md);"><table class="ans-sched-tbl" style="width:100%;border-collapse:collapse;font-size:var(--fs-xs);"><thead><tr>
    <th>${S.tfE(t('tf_var_key', 'Key'))}</th><th>${S.tfE(t('tf_var_val', 'Value'))}</th><th>${S.tfE(t('tf_var_category', 'Category'))}</th><th>${S.tfE(t('tf_var_hcl', 'HCL'))}</th><th>${S.tfE(t('tf_var_sensitive', 'Sensitive'))}</th>${eff ? `<th>${S.tfE(t('tf_var_origin', 'Origin'))}</th>` : ''}<th></th></tr></thead><tbody>
    ${vars.map(v => `<tr><td class="mono">${S.tfE(v.key)}</td><td class="mono" style="max-width:320px;overflow:hidden;text-overflow:ellipsis;">${v.sensitive ? `<span style="color:var(--text-muted);">${S.tfE(v.value_set ? '••••••••' : '')}</span>` : S.tfE(v.value)}</td>
      <td>${S.tfE(v.category)}</td><td>${v.hcl ? svgIcon('check', 'icon-xs') : ''}</td><td>${v.sensitive ? svgIcon('lock', 'icon-xs') : ''}</td>${eff ? `<td>${S.tfE(v.origin || '')}</td>` : ''}
      <td style="text-align:right;white-space:nowrap;">${eff && v.origin !== 'workspace' ? '' : `<button type="button" class="btn btn-icon btn-sm" onclick="openTfVarDialog('${kind}', ${scopeId}, ${v.id}, ${lvl})">${svgIcon('pencil', 'icon-xs')}</button>
        <button type="button" class="btn btn-icon btn-sm" onclick="deleteTfVar('${kind}', ${scopeId}, ${v.id}, ${lvl})">${svgIcon('trash-2', 'icon-xs')}</button>`}</td></tr>`).join('')}</tbody></table></div>`;
}

function openTfVarDialog(kind, scopeId, vid, lvl) {
  const v = vid ? S.tfVars.find(x => x.id === vid) : null;
  S.tfVarScope = Object.assign({}, S.tfVarScope || {}, { kind, id: scopeId, lvl, vid: vid || 0 });
  const body = `<div class="dk-form-grid">
    ${tfField(t('tf_var_key', 'Key') + ' *', tfInput('tfVarKey', v && v.key, 'maxlength="64"'))}
    ${tfField(t('tf_var_category', 'Category'), tfSelect('tfVarCat', [['terraform', 'terraform'], ['env', 'env']], v ? v.category : 'terraform', 'tfVarCatChanged()'))}
    ${tfField(t('tf_var_val', 'Value'), `<textarea id="tfVarVal" class="form-input" rows="4" spellcheck="false" style="width:100%;box-sizing:border-box;font-family:var(--font-mono);font-size:var(--fs-xs);${v && v.sensitive ? '-webkit-text-security:disc;' : ''}" placeholder="${v && v.sensitive ? S.tfE(t('tf_var_leave_blank_keep', '')) : ''}">${v && !v.sensitive ? S.tfE(v.value || '') : ''}</textarea>`, '', true)}
    <div class="dk-field" style="display:flex;gap:18px;align-items:center;">${tfCheck('tfVarHcl', t('tf_var_hcl', 'HCL'), v && v.hcl)}${tfCheck('tfVarSens', t('tf_var_sensitive', 'Sensitive'), v && v.sensitive, 'tfVarSensChanged()')}</div>
    ${tfField(tx('tfx_var_desc'), tfInput('tfVarDesc', v && v.description))}</div>`;
  tfDlg(lvl, v ? tx('tfx_var_title_edit') : tx('tfx_var_title_new'), body, tfFoot(lvl, tfPrimary(t('save', 'Save'), 'saveTfVar()')), 560);
  tfVarCatChanged();
}

function tfVarCatChanged() { const h = tfEl('tfVarHcl'); if (!h) return; const env = tfV('tfVarCat') === 'env'; h.disabled = env; if (env) h.checked = false; }

function tfVarSensChanged() { const ta = tfEl('tfVarVal'); if (ta) ta.style.webkitTextSecurity = tfChk('tfVarSens') ? 'disc' : 'none'; }

async function saveTfVar() {
  const sc = S.tfVarScope;
  const key = tfV('tfVarKey').trim();
  if (!key) { tfDlgErr(sc.lvl, tx('tfx_name_required')); return; }
  const body = { key, value: tfV('tfVarVal'), category: tfV('tfVarCat'), hcl: tfChk('tfVarHcl'), sensitive: tfChk('tfVarSens'), description: tfV('tfVarDesc') };
  const r = await tfApi(sc.vid ? 'PUT' : 'POST', tfVarBase(sc) + (sc.vid ? '/' + sc.vid : ''), body);
  if (!r.ok) { tfDlgErr(sc.lvl, r.data.error || 'Error'); return; }
  tfDlgClose(sc.lvl);
  showToast(tx('tfx_var_saved'), 'success');
  if (sc.refresh) await sc.refresh();
}

async function deleteTfVar(kind, scopeId, vid, lvl) {
  const v = S.tfVars.find(x => x.id === vid);
  const ok = await showConfirm({ title: t('delete', 'Delete'), message: tx('tfx_var_delete_confirm', { k: v ? v.key : '#' + vid }), confirmText: t('delete', 'Delete'), danger: true });
  if (!ok) return;
  const r = await tfApi('DELETE', tfVarBase({ kind, id: scopeId }) + '/' + vid);
  if (!r.ok) { tfErr(r); return; }
  showToast(tx('tfx_var_deleted'), 'success');
  if (S.tfVarScope && S.tfVarScope.refresh) await S.tfVarScope.refresh();
}

function tfVdValue(v) {
  const d = (v.default || '').trim();
  if (!v.has_default) return { val: '', hcl: /^(list|set|map|object|tuple)\b/.test((v.type || '').trim()) };
  const m = d.match(/^"([^"\\$%]*)"$/);
  if (m) return { val: m[1], hcl: false };
  if (/^(true|false|-?\d+(\.\d+)?)$/.test(d)) return { val: d, hcl: false };
  return { val: d, hcl: true };
}

function tfVdRow(v) {
  const x = tfVdValue(v);
  const on = !v.exists && !v.has_default;
  const dis = v.exists ? 'disabled' : '';
  const sm = 'font-size:var(--fs-xs);';
  return `<div data-vd="${S.tfE(v.name)}" style="display:grid;grid-template-columns:auto 1fr;gap:4px 10px;padding:10px 12px;border-bottom:1px solid var(--card-border);${v.exists ? 'opacity:.6;' : ''}">
    <input type="checkbox" data-vd-on ${on ? 'checked' : ''} ${dis} style="margin-top:3px;" />
    <div style="min-width:0;">
      <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap;"><b class="mono">${S.tfE(v.name)}</b>
        ${v.type ? `<code class="mono" style="${sm}color:var(--text-muted);">${S.tfE(v.type)}</code>` : ''}
        ${v.exists ? `<span class="badge">${S.tfE(tx('tfx_vd_exists'))}</span>` : ''}
        ${v.has_default ? '' : `<span style="${sm}color:var(--text-muted);">${S.tfE(tx('tfx_vd_no_default'))}</span>`}</div>
      ${v.description ? `<div style="${sm}color:var(--text-muted);margin:2px 0 6px;">${S.tfE(v.description)}</div>` : ''}
      <div style="display:flex;gap:10px;align-items:center;flex-wrap:wrap;">
        <input type="text" data-vd-val class="form-input mono" style="flex:1;min-width:200px;box-sizing:border-box;${v.sensitive ? '-webkit-text-security:disc;' : ''}" spellcheck="false" value="${S.tfE(x.val)}" ${dis} />
        <label style="${sm}display:flex;gap:4px;align-items:center;"><input type="checkbox" data-vd-hcl ${x.hcl ? 'checked' : ''} ${dis} /> ${S.tfE(t('tf_var_hcl', 'HCL'))}</label>
        <label style="${sm}display:flex;gap:4px;align-items:center;"><input type="checkbox" data-vd-sens ${v.sensitive ? 'checked' : ''} ${dis} onchange="this.closest('[data-vd]').querySelector('[data-vd-val]').style.webkitTextSecurity = this.checked ? 'disc' : 'none'" /> ${S.tfE(t('tf_var_sensitive', 'Sensitive'))}</label></div>
      <div data-vd-err style="display:none;${sm}color:var(--danger);margin-top:4px;"></div></div></div>`;
}

async function tfDetectVars() {
  const r = await tfApi('GET', `/api/terraform/workspaces/${S.tfWs.id}/variables/detect`);
  if (!r.ok) { tfErr(r); return; }
  if (r.data.supported === false) { showToast(tx('tfx_vd_git'), 'info'); return; }
  const vars = r.data.variables || [];
  const body = vars.length
    ? `<div class="dk-hint" style="margin-bottom:8px;">${S.tfE(tx('tfx_vd_hint'))}</div><div id="tfVarDetectList" style="border:1px solid var(--card-border);border-radius:var(--radius-md);max-height:60vh;overflow-y:auto;">${vars.map(tfVdRow).join('')}</div>`
    : `<div id="tfVarDetectList">${tfEmpty(tx('tfx_vd_empty'))}</div>`;
  tfDlg(1, tx('tfx_vd_title'), body, tfFoot(1, vars.length ? `<button type="button" id="tfVarDetectAdd" class="btn btn-sm btn-primary" onclick="tfDetectAdd()">${S.tfE(tx('tfx_vd_add'))}</button>` : ''), 720);
}

async function tfDetectAdd() {
  const rows = [...document.querySelectorAll('#tfVarDetectList [data-vd]')].filter(el => el.querySelector('[data-vd-on]').checked);
  if (!rows.length) { tfDlgErr(1, tx('tfx_vd_none')); return; }
  tfDlgErr(1, '');
  const btn = tfEl('tfVarDetectAdd');
  btn.disabled = true;
  let ok = 0, fail = 0;
  for (const el of rows) {
    const q = s => el.querySelector(s);
    const err = q('[data-vd-err]');
    err.style.display = 'none';
    const r = await tfApi('POST', tfVarBase({ kind: 'ws', id: S.tfWs.id }), { key: el.getAttribute('data-vd'), value: q('[data-vd-val]').value, category: 'terraform', hcl: q('[data-vd-hcl]').checked, sensitive: q('[data-vd-sens]').checked, description: '' });
    if (r.ok) {
      ok++;
      q('[data-vd-on]').checked = false;
      for (const s of ['[data-vd-on]', '[data-vd-val]', '[data-vd-hcl]', '[data-vd-sens]']) q(s).disabled = true;
      el.style.opacity = '.6';
    } else {
      fail++;
      err.textContent = r.data.error || 'Error';
      err.style.display = 'block';
    }
  }
  btn.disabled = false;
  showToast(tx('tfx_vd_added', { n: ok, f: fail }), fail ? 'error' : 'success');
  if (ok && S.tfVarScope && S.tfVarScope.refresh) S.tfVarScope.refresh();
  if (!fail) tfDlgClose(1);
}

async function tfRenderDrift(body) {
  const r = await tfApi('GET', `/api/terraform/workspaces/${S.tfWs.id}/drift`);
  if (!r.ok) { body.innerHTML = tfEmpty(r.data.error || ''); return; }
  const d = r.data;
  body.innerHTML = `<div class="card" style="flex-direction:row;align-items:center;justify-content:space-between;flex-wrap:wrap;border-left:4px solid var(--${d.detected ? 'warning' : 'success'});">
    <div><b>${d.detected ? svgIcon('triangle-alert', 'icon-sm') + ' ' + S.tfE(t('tf_drift_detected_alert', 'Drift Detected')) : svgIcon('circle-check', 'icon-sm') + ' ' + S.tfE(t('tf_drift_none_alert', 'No drift'))}</b>
      <div style="font-size:var(--fs-xs);color:var(--text-muted);">${S.tfE(tx('tfx_drift_checked'))}: ${S.tfE(tfTime(d.checked_at))} · ${S.tfE(tx('tfx_drift_interval'))}: ${d.interval_minutes ? d.interval_minutes : S.tfE(tx('tfx_drift_off'))}</div></div>
    <button type="button" class="btn btn-sm btn-primary" onclick="tfCheckDrift()">${svgIcon('refresh-cw', 'icon-xs')} ${S.tfE(t('tf_drift_check_now', 'Check Drift Now'))}</button></div>
    <div style="margin-top:16px;">${d.detected ? tfDiffHTML(d.resources || []) : ''}</div>`;
}

async function tfCheckDrift() {
  const r = await tfApi('POST', `/api/terraform/workspaces/${S.tfWs.id}/drift/check`);
  if (!r.ok) { tfErr(r); return; }
  showToast(tx('tfx_drift_started'), 'success');
  S.tfRunId = r.data.run.id;
  await tfReloadWs();
  switchTfWsTab('runs');
}

function tfWebhookURL(w) { return w.webhook_url ? location.origin + w.webhook_url : ''; }

async function tfVcsTest(btn) {
  if (btn && !(btn instanceof HTMLElement)) btn = null; // a click event was passed instead of the button
  const out = tfEl('tfVcsResult');
  if (!out) return;
  const restore = SP.busy(btn, tx('tfx_vcs_testing'));
  out.style.color = '';
  out.innerHTML = SP.html(tx('tfx_vcs_testing'));
  let r;
  try {
    r = await tfApi('POST', `/api/terraform/workspaces/${S.tfWs.id}/vcs/test`);
  } finally {
    restore();
  }
  const d = r.data || {};
  const ok = r.ok && d.ok;
  out.style.color = ok ? 'var(--success)' : 'var(--danger)';
  out.textContent = ok ? `${tx('tfx_vcs_ok')} · ${d.branch || ''} ${(d.commit || '').slice(0, 10)}` : (d.error || 'failed');
}

async function tfRotateWebhook() {
  const ok = await showConfirm({ title: t('tf_webhook_rotate_btn', 'Rotate Secret'), message: tx('tfx_rotate_confirm'), confirmText: t('tf_webhook_rotate_btn', 'Rotate Secret'), danger: true });
  if (!ok) return;
  const r = await tfApi('POST', `/api/terraform/workspaces/${S.tfWs.id}/webhook/rotate`);
  if (!r.ok) { tfErr(r); return; }
  showToast(tx('tfx_webhook_rotated'), 'success');
  S.tfWs.webhook_url = r.data.webhook_url;
  tfEl('tfWhUrl').value = tfWebhookURL(S.tfWs);
  tfEl('tfWhSecret').innerHTML = `<div style="font-size:var(--fs-xs);margin-top:6px;">${S.tfE(t('tf_webhook_secret_once', 'Webhook Secret (shown only once):'))}
    <div style="display:flex;gap:8px;align-items:center;margin-top:4px;"><code class="mono" style="word-break:break-all;">${S.tfE(r.data.webhook_secret)}</code>
    <button type="button" class="btn btn-icon btn-sm" data-copy="${S.tfE(r.data.webhook_secret)}" onclick="tfCopyData(this)">${svgIcon('copy', 'icon-xs')}</button></div></div>`;
}

async function loadTfVarsets() {
  const r = await tfApi('GET', '/api/terraform/varsets');
  S.tfVarsets = r.ok ? (r.data.varsets || []) : [];
  const box = tfEl('tfVarsetsList');
  if (!S.tfVarsets.length) { box.innerHTML = `<div class="empty-state"><div class="empty-state-icon">${svgIcon('sliders-horizontal', 'icon-xl')}</div><div class="empty-state-desc">${S.tfE(tx('tfx_no_varsets'))}</div></div>`; return; }
  box.innerHTML = S.tfVarsets.map(s => `<div class="card" style="flex-direction:row;align-items:center;justify-content:space-between;gap:12px;">
    <div style="min-width:0;"><b>${S.tfE(s.name)}</b> ${s.global ? `<span class="badge badge-healthy">${S.tfE(tx('tfx_global'))}</span>` : ''}
      <div style="font-size:var(--fs-xs);color:var(--text-muted);">${S.tfE(s.description || '')}</div>
      <div style="font-size:var(--fs-xs);color:var(--text-muted);margin-top:4px;">${S.tfE(tx('tfx_n_vars', { n: s.variable_count || 0 }))} · ${S.tfE(s.global ? tx('tfx_policy_scope_all') : tx('tfx_n_ws', { n: (s.workspace_ids || []).length }))}</div></div>
    <div style="display:flex;gap:6px;"><button type="button" class="btn btn-sm" onclick="openTfVarsetModal(${s.id})">${svgIcon('pencil', 'icon-xs')} ${S.tfE(tx('tfx_edit'))}</button>
      <button type="button" class="btn btn-icon btn-sm" onclick="deleteTfVarset(${s.id})">${svgIcon('trash-2', 'icon-xs')}</button></div></div>`).join('');
}

async function openTfVarsetModal(id) {
  let s = null;
  S.tfVars = [];
  if (id) {
    const r = await tfApi('GET', '/api/terraform/varsets/' + id);
    if (!r.ok) { tfErr(r); return; }
    s = r.data.varset || r.data;
    const vr = await tfApi('GET', `/api/terraform/varsets/${id}/variables`);
    S.tfVars = vr.ok ? (vr.data.variables || []) : [];
  }
  const wsIds = new Set((s && s.workspace_ids) || []);
  S.tfVarScope = { kind: 'varset', id: id || 0, lvl: 2, refresh: () => openTfVarsetModal(id) };
  const body = `<div class="dk-form-grid">
    ${tfField(tx('tfx_name') + ' *', tfInput('tfVsName', s && s.name))}
    <div class="dk-field" style="display:flex;align-items:flex-end;">${tfCheck('tfVsGlobal', tx('tfx_varset_global'), s && s.global)}</div>
    ${tfField(tx('tfx_description'), tfInput('tfVsDesc', s && s.description), '', true)}
    ${tfField(tx('tfx_varset_workspaces'), `<div style="max-height:130px;overflow:auto;display:flex;flex-direction:column;gap:4px;border:1px solid var(--card-border);border-radius:var(--radius-sm);padding:8px;">${S.tfWorkspaces.length ? S.tfWorkspaces.map(w => tfCheck('tfVsWs' + w.id, w.name, wsIds.has(w.id))).join('') : tfEmpty(tx('tfx_no_runs'))}</div>`, '', true)}
    <div class="dk-field dk-full"><label style="display:flex;justify-content:space-between;align-items:center;">${S.tfE(tx('tfx_varset_vars'))}
      ${id ? `<button type="button" class="btn btn-sm" onclick="openTfVarDialog('varset', ${id}, 0, 2)">${svgIcon('plus', 'icon-xs')} ${S.tfE(t('tf_btn_add_var', 'Add Variable'))}</button>` : ''}</label>
      ${id ? tfVarsTable(S.tfVars, false, 'varset', id, 2) : `<div class="dk-hint">${S.tfE(tx('tfx_varset_save_first'))}</div>`}</div></div>`;
  tfDlg(1, s ? tx('tfx_varset_title_edit') : tx('tfx_varset_title_new'), body, tfFoot(1, tfPrimary(t('save', 'Save'), `saveTfVarset(${id || 0})`)), 760);
}

async function saveTfVarset(id) {
  const name = tfV('tfVsName').trim();
  if (!name) { tfDlgErr(1, tx('tfx_name_required')); return; }
  const body = { name, description: tfV('tfVsDesc'), global: tfChk('tfVsGlobal') };
  const r = await tfApi(id ? 'PUT' : 'POST', id ? '/api/terraform/varsets/' + id : '/api/terraform/varsets', body);
  if (!r.ok) { tfDlgErr(1, r.data.error || 'Error'); return; }
  const sid = id || (r.data.varset || r.data).id;
  const ids = S.tfWorkspaces.filter(w => tfChk('tfVsWs' + w.id)).map(w => w.id);
  const wr = await tfApi('PUT', `/api/terraform/varsets/${sid}/workspaces`, { workspace_ids: ids });
  if (!wr.ok) { tfDlgErr(1, wr.data.error || 'Error'); return; }
  showToast(tx('tfx_saved'), 'success');
  await loadTfVarsets();
  if (id) tfDlgClose(1); else await openTfVarsetModal(sid);
}

async function deleteTfVarset(id) {
  const s = S.tfVarsets.find(x => x.id === id);
  const ok = await showConfirm({ title: t('delete', 'Delete'), message: tx('tfx_varset_delete_confirm', { n: s ? s.name : '#' + id }), confirmText: t('delete', 'Delete'), danger: true });
  if (!ok) return;
  const r = await tfApi('DELETE', '/api/terraform/varsets/' + id);
  if (!r.ok) { tfErr(r); return; }
  showToast(tx('tfx_deleted'), 'success');
  await loadTfVarsets();
}

async function loadTfPolicies() {
  const r = await tfApi('GET', '/api/terraform/policies');
  S.tfPolicies = r.ok ? (r.data.policies || []) : [];
  const box = tfEl('tfPoliciesList');
  if (!S.tfPolicies.length) {
    box.innerHTML = `<div class="empty-state"><div class="empty-state-icon">${svgIcon('shield', 'icon-xl')}</div><div class="empty-state-desc">${S.tfE(tx('tfx_no_policies'))}</div></div>`;
    return;
  }
  const enfCls = { advisory: 'badge-warning', soft: 'badge-degraded', hard: 'badge-danger' };
  box.innerHTML = S.tfPolicies.map(p => {
    const ws = p.workspace_id ? (S.tfWorkspaces.find(w => w.id === p.workspace_id) || {}).name || '#' + p.workspace_id : tx('tfx_policy_scope_all');
    return `<div class="card" style="flex-direction:row;align-items:center;justify-content:space-between;gap:12px;${p.enabled ? '' : 'opacity:.6;'}">
    <div style="min-width:0;"><b>${S.tfE(p.name)}</b> <span class="badge mono">${S.tfE(p.kind)}</span> <span class="badge ${enfCls[p.enforcement] || ''}">${S.tfE(p.enforcement)}</span> ${p.enabled ? '' : `<span class="badge">off</span>`}
      <div style="font-size:var(--fs-xs);color:var(--text-muted);">${S.tfE(p.description || '')}</div>
      <div class="mono" style="font-size:var(--fs-xs);color:var(--text-muted);margin-top:4px;">${S.tfE(ws)} · ${S.tfE(typeof p.params === 'string' ? p.params : JSON.stringify(p.params || {}))}</div></div>
    <div style="display:flex;gap:6px;"><button type="button" class="btn btn-sm" onclick="openTfPolicyModal(${p.id})">${svgIcon('pencil', 'icon-xs')} ${S.tfE(tx('tfx_edit'))}</button>
      <button type="button" class="btn btn-icon btn-sm" onclick="deleteTfPolicy(${p.id})">${svgIcon('trash-2', 'icon-xs')}</button></div></div>`;
  }).join('');
}

async function openTfPolicyModal(id) {
  if (!S.tfWorkspaces.length) await loadTfWorkspaces();
  const p = id ? S.tfPolicies.find(x => x.id === id) : null;
  const params = p ? tfJ(p.params, {}) : {};
  const body = `<div class="dk-form-grid">
    ${tfField(tx('tfx_name') + ' *', tfInput('tfPolName', p && p.name))}
    ${tfField(tx('tfx_policy_kind'), tfSelect('tfPolKind', S.TF_POLICY_KINDS.map(k => [k, k]), p ? p.kind : 'deny_destroy', 'tfPolKindChanged()'))}
    ${tfField(tx('tfx_description'), tfInput('tfPolDesc', p && p.description), '', true)}
    ${tfField(tx('tfx_policy_enf'), tfSelect('tfPolEnf', [['advisory', tx('tfx_policy_enf_advisory')], ['soft', tx('tfx_policy_enf_soft')], ['hard', tx('tfx_policy_enf_hard')]], p ? p.enforcement : 'hard'))}
    ${tfField(tx('tfx_policy_scope'), tfSelect('tfPolScope', [[0, tx('tfx_policy_scope_all')]].concat(S.tfWorkspaces.map(w => [w.id, w.name])), p ? p.workspace_id : 0))}
    <div id="tfPolMaxWrap">${tfField(tx('tfx_policy_max'), tfInput('tfPolMax', params.max != null ? params.max : 0, 'type="number" min="0"'))}</div>
    <div id="tfPolTypesWrap" class="dk-full" style="grid-column:1/-1;">${tfField(tx('tfx_policy_types'), tfInput('tfPolTypes', (params.types || []).join(', ')))}</div>
    <div id="tfPolTagsWrap" class="dk-full" style="grid-column:1/-1;">${tfField(tx('tfx_policy_tags'), tfInput('tfPolTags', (params.tags || []).join(', ')))}</div>
    <div class="dk-field">${tfCheck('tfPolEnabled', tx('tfx_policy_enabled'), p ? p.enabled : true)}</div></div>`;
  tfDlg(1, p ? tx('tfx_policy_title_edit') : tx('tfx_policy_title_new'), body, tfFoot(1, tfPrimary(t('save', 'Save'), `saveTfPolicy(${id || 0})`)), 620);
  tfPolKindChanged();
}

function tfPolKindChanged() {
  const k = tfV('tfPolKind');
  tfEl('tfPolMaxWrap').style.display = k === 'max_deletes' || k === 'max_changes' ? 'block' : 'none';
  tfEl('tfPolTypesWrap').style.display = k === 'deny_resource_types' || k === 'require_tags' ? 'block' : 'none';
  tfEl('tfPolTagsWrap').style.display = k === 'require_tags' ? 'block' : 'none';
  const lbl = tfEl('tfPolTypesWrap').querySelector('label');
  if (lbl) lbl.textContent = k === 'require_tags' ? tx('tfx_policy_types_opt') : tx('tfx_policy_types');
}

async function saveTfPolicy(id) {
  const name = tfV('tfPolName').trim();
  if (!name) { tfDlgErr(1, tx('tfx_name_required')); return; }
  const kind = tfV('tfPolKind');
  let params = {};
  if (kind === 'max_deletes' || kind === 'max_changes') params = { max: parseInt(tfV('tfPolMax'), 10) || 0 };
  else if (kind === 'deny_resource_types') params = { types: tfCsv(tfV('tfPolTypes')) };
  else if (kind === 'require_tags') { params = { tags: tfCsv(tfV('tfPolTags')) }; const ty = tfCsv(tfV('tfPolTypes')); if (ty.length) params.types = ty; }
  const body = { name, description: tfV('tfPolDesc'), kind, params, enforcement: tfV('tfPolEnf'), enabled: tfChk('tfPolEnabled'), workspace_id: parseInt(tfV('tfPolScope'), 10) || 0 };
  const r = await tfApi(id ? 'PUT' : 'POST', id ? '/api/terraform/policies/' + id : '/api/terraform/policies', body);
  if (!r.ok) { tfDlgErr(1, r.data.error || 'Error'); return; }
  tfDlgClose(1);
  showToast(tx('tfx_saved'), 'success');
  await loadTfPolicies();
}

async function deleteTfPolicy(id) {
  const p = S.tfPolicies.find(x => x.id === id);
  const ok = await showConfirm({ title: t('delete', 'Delete'), message: tx('tfx_policy_delete_confirm', { n: p ? p.name : '#' + id }), confirmText: t('delete', 'Delete'), danger: true });
  if (!ok) return;
  const r = await tfApi('DELETE', '/api/terraform/policies/' + id);
  if (!r.ok) { tfErr(r); return; }
  showToast(tx('tfx_deleted'), 'success');
  await loadTfPolicies();
}

async function loadTfModules() {
  const r = await tfApi('GET', '/api/terraform/modules');
  S.tfModules = r.ok ? (r.data.modules || []) : [];
  const box = tfEl('tfModulesList');
  if (!S.tfModules.length) { box.style.display = 'block'; box.innerHTML = tfEmpty(tx('tfx_no_modules')); return; }
  box.style.display = 'grid';
  box.innerHTML = S.tfModules.map(m => `<div class="card" style="cursor:pointer;" onclick="openTfModuleDetail(${m.id})">
    <div class="card-title"><span>${svgIcon('package', 'icon-sm')} ${S.tfE(m.name)}</span><span class="badge">${S.tfE(m.provider)}</span></div>
    <div style="font-size:var(--fs-xs);color:var(--text-muted);min-height:16px;">${S.tfE(m.description || '')}</div>
    <div style="font-size:var(--fs-xs);color:var(--text-muted);">${m.latest_version ? S.tfE(tx('tfx_module_latest', { v: m.latest_version })) + ' · ' : ''}${S.tfE(tx('tfx_module_n_versions', { n: m.version_count || 0 }))}</div></div>`).join('');
}

function openTfModuleEdit(id, lvl) {
  const m = id ? (S.tfModule && S.tfModule.id === id ? S.tfModule : S.tfModules.find(x => x.id === id)) : null;
  const body = `<div class="dk-form-grid">${tfField(tx('tfx_name') + ' *', tfInput('tfModName', m && m.name))}${tfField(tx('tfx_module_provider') + ' *', tfInput('tfModProv', m && m.provider, 'placeholder="aws"'))}
    ${tfField(tx('tfx_description'), tfInput('tfModDesc', m && m.description), '', true)}</div>`;
  tfDlg(lvl, m ? tx('tfx_module_title_edit') : tx('tfx_module_title_new'), body, tfFoot(lvl, tfPrimary(t('save', 'Save'), `saveTfModule(${id || 0}, ${lvl})`)), 520);
}

async function openTfModuleModal() { openTfModuleEdit(0, 1); }

async function saveTfModule(id, lvl) {
  const body = { name: tfV('tfModName').trim(), provider: tfV('tfModProv').trim(), description: tfV('tfModDesc') };
  if (!body.name || !body.provider) { tfDlgErr(lvl, tx('tfx_name_required')); return; }
  const r = await tfApi(id ? 'PUT' : 'POST', id ? '/api/terraform/modules/' + id : '/api/terraform/modules', body);
  if (!r.ok) { tfDlgErr(lvl, r.data.error || 'Error'); return; }
  tfDlgClose(lvl);
  showToast(tx('tfx_saved'), 'success');
  await loadTfModules();
  if (id) openTfModuleDetail(id, S.tfModuleVer); else openTfModuleDetail((r.data.module || r.data).id);
}

async function openTfModuleDetail(id, ver) {
  const r = await tfApi('GET', '/api/terraform/modules/' + id);
  if (!r.ok) { tfErr(r); return; }
  S.tfModule = r.data.module || r.data;
  const versions = S.tfModule.versions || [];
  S.tfModuleVer = ver || S.tfModule.latest_version || (versions[0] && versions[0].version) || '';
  const cur = versions.find(v => v.version === S.tfModuleVer) || versions[0];
  const doc = cur ? tfJ(cur.doc, {}) : {};
  const inputs = doc.inputs || [], outputs = doc.outputs || [];
  const req = inputs.filter(i => i.required);
  const usage = cur ? `module "${S.tfModule.name}" {\n  source  = "${cur.source_url || './modules/' + S.tfModule.name}"\n  version = "${cur.version}"\n${req.map(i => `  ${i.name} = "" # ${tx('tfx_required')}`).join('\n')}${req.length ? '\n' : ''}}\n` : '';
  const tbl = (head, rows) => `<div style="overflow-x:auto;border:1px solid var(--card-border);border-radius:var(--radius-md);"><table class="ans-sched-tbl" style="width:100%;border-collapse:collapse;font-size:var(--fs-xs);"><thead><tr>${head.map(h => `<th>${S.tfE(h)}</th>`).join('')}</tr></thead><tbody>${rows}</tbody></table></div>`;
  const body = `<div style="display:flex;flex-direction:column;gap:14px;">
    <div style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:8px;">
      <div style="font-size:var(--fs-xs);color:var(--text-muted);">${S.tfE(S.tfModule.description || '')}</div>
      <div style="display:flex;gap:6px;align-items:center;flex-wrap:wrap;">
        ${versions.length ? `<select class="form-input" style="width:auto;" onchange="openTfModuleDetail(${id}, this.value)">${versions.map(v => `<option value="${S.tfE(v.version)}" ${v.version === (cur && cur.version) ? 'selected' : ''}>${S.tfE(v.version)}</option>`).join('')}</select>` : ''}
        <button type="button" class="btn btn-sm btn-primary" onclick="openTfVersionDialog(${id})">${svgIcon('plus', 'icon-xs')} ${S.tfE(t('tf_btn_add_version', 'Add Version'))}</button>
        ${cur ? `<button type="button" class="btn btn-sm" onclick="deleteTfModuleVersion(${id}, ${jsArg(cur.version)})">${svgIcon('trash-2', 'icon-xs')} ${S.tfE(cur.version)}</button>` : ''}
        <button type="button" class="btn btn-sm" onclick="openTfModuleEdit(${id}, 2)">${svgIcon('pencil', 'icon-xs')}</button>
        <button type="button" class="btn btn-sm btn-danger" onclick="deleteTfModule(${id})">${svgIcon('trash-2', 'icon-xs')}</button></div></div>
    ${cur ? `<div><b>${S.tfE(t('tf_usage_snippet', 'Usage Snippet'))}</b> <button type="button" class="btn btn-icon btn-sm" data-copy="${S.tfE(usage)}" onclick="tfCopyData(this)">${svgIcon('copy', 'icon-xs')}</button><pre class="tf-log" style="margin-top:6px;max-height:200px;">${S.tfE(usage)}</pre></div>
      ${doc.readme ? `<div><b>${S.tfE(tx('tfx_readme'))}</b><pre class="tf-log" style="margin-top:6px;max-height:240px;">${S.tfE(doc.readme)}</pre></div>` : ''}
      <div><b>${S.tfE(tx('tfx_inputs'))} (${inputs.length})</b>${inputs.length ? tbl([t('tf_var_key', 'Key'), tx('tfx_type'), tx('tfx_default'), tx('tfx_description')], inputs.map(i => `<tr><td class="mono">${S.tfE(i.name)}${i.required ? ` <span class="badge badge-danger">${S.tfE(tx('tfx_required'))}</span>` : ''}${i.sensitive ? ' ' + svgIcon('lock', 'icon-xs') : ''}</td><td class="mono">${S.tfE(i.type || '')}</td><td class="mono">${S.tfE(i.default || '')}</td><td>${S.tfE(i.description || '')}</td></tr>`).join('')) : tfEmpty('—')}</div>
      <div><b>${S.tfE(tx('tfx_outputs'))} (${outputs.length})</b>${outputs.length ? tbl([t('tf_var_key', 'Key'), tx('tfx_description')], outputs.map(o => `<tr><td class="mono">${S.tfE(o.name)}${o.sensitive ? ' ' + svgIcon('lock', 'icon-xs') : ''}</td><td>${S.tfE(o.description || '')}</td></tr>`).join('')) : tfEmpty('—')}</div>
      <div><b>${S.tfE(tx('tfx_tab_resources'))} (${(doc.resources || []).length})</b><div class="mono" style="font-size:var(--fs-xs);margin-top:4px;">${S.tfE((doc.resources || []).join(', ') || '—')}</div></div>
      <div><b>${S.tfE(tx('tfx_providers'))}</b> <span class="mono" style="font-size:var(--fs-xs);">${S.tfE((doc.providers || []).join(', ') || '—')}${doc.required_version ? ' · terraform ' + S.tfE(doc.required_version) : ''}</span></div>` : tfEmpty(tx('tfx_module_no_versions'))}</div>`;
  tfDlg(1, `${S.tfModule.name} · ${S.tfModule.provider}`, body, `<button type="button" class="btn btn-sm" onclick="tfDlgClose(1)">${S.tfE(t('close', 'Close'))}</button>`, 900);
}

function openTfVersionDialog(id) {
  const body = `<div class="dk-form-grid">
    ${tfField(tx('tfx_module_version') + ' *', tfInput('tfVerNum', '', 'placeholder="1.0.0"'))}
    ${tfField(tx('tfx_source'), tfSelect('tfVerSrc', [['inline', tx('tfx_source_inline')], ['git', tx('tfx_source_git')]], 'inline', 'tfVerSrcChanged()'))}
    <div id="tfVerInline" class="dk-full" style="grid-column:1/-1;">${tfFilesEditorHTML()}</div>
    <div id="tfVerGit" class="dk-form-grid dk-full" style="grid-column:1/-1;display:none;">
      ${tfField(tx('tfx_git_url'), tfInput('tfVerUrl', '', 'placeholder="https://github.com/org/module.git"'), '', true)}
      ${tfField(tx('tfx_git_ref'), tfInput('tfVerRef', '', 'placeholder="v1.0.0"'))}
      ${tfField(tx('tfx_git_path'), tfInput('tfVerPath', ''))}
      ${tfField(tx('tfx_git_auth'), tfSelect('tfVerAuth', [['none', tx('tfx_git_auth_none')], ['token', tx('tfx_git_auth_token')], ['ssh', tx('tfx_git_auth_ssh')]], 'none', 'tfVerAuthChanged()'))}
      <div id="tfVerTokWrap" style="display:none;">${tfField(tx('tfx_git_token'), '<input type="password" id="tfVerTok" class="form-input" autocomplete="new-password" style="width:100%;box-sizing:border-box;" />')}</div>
      <div id="tfVerKeyWrap" style="display:none;">${tfField('SSH Key', '<select id="tfVerKey" class="form-input" style="width:100%;box-sizing:border-box;"></select>')}</div></div></div>`;
  tfDlg(2, t('tf_btn_add_version', 'Add Version'), body, tfFoot(2, tfPrimary(t('save', 'Save'), `saveTfVersion(${id})`)), 780);
  tfFeInit({ 'main.tf': '', 'variables.tf': '', 'outputs.tf': '', 'README.md': '' });
  tfApi('GET', '/api/ssh-keys').then(kr => { const keys = kr.ok && Array.isArray(kr.data) ? kr.data : []; const s = tfEl('tfVerKey'); if (s) s.innerHTML = keys.map(k => `<option value="${Number(k.id)}">${S.tfE(k.name)}</option>`).join(''); });
}

function tfVerSrcChanged() { const g = tfV('tfVerSrc') === 'git'; tfEl('tfVerInline').style.display = g ? 'none' : 'block'; tfEl('tfVerGit').style.display = g ? 'grid' : 'none'; }

function tfVerAuthChanged() { const a = tfV('tfVerAuth'); tfEl('tfVerTokWrap').style.display = a === 'token' ? 'block' : 'none'; tfEl('tfVerKeyWrap').style.display = a === 'ssh' ? 'block' : 'none'; }

async function saveTfVersion(id) {
  const version = tfV('tfVerNum').trim();
  if (!version) { tfDlgErr(2, tx('tfx_name_required')); return; }
  const body = { version };
  if (tfV('tfVerSrc') === 'git') {
    body.git = { url: tfV('tfVerUrl').trim(), ref: tfV('tfVerRef').trim(), path: tfV('tfVerPath').trim(), auth: tfV('tfVerAuth') };
    if (body.git.auth === 'token') body.git.token = tfV('tfVerTok');
    if (body.git.auth === 'ssh') body.git.key_id = parseInt(tfV('tfVerKey'), 10) || 0;
  } else {
    const files = tfFeCollect();
    body.files = Object.fromEntries(Object.entries(files).filter(([, c]) => c !== ''));
  }
  const btn = document.querySelector('#tfDlg2Footer .btn-primary');
  const restore = SP.busy(btn, tx('tfx_saving_module'));
  let r;
  try {
    r = await tfApi('POST', `/api/terraform/modules/${id}/versions`, body);
  } finally {
    restore();
  }
  if (!r.ok) { tfDlgErr(2, r.data.error || 'Error'); return; }
  tfDlgClose(2);
  showToast(tx('tfx_saved'), 'success');
  await loadTfModules();
  await openTfModuleDetail(id, version);
}

async function deleteTfModuleVersion(id, ver) {
  const ok = await showConfirm({ title: t('delete', 'Delete'), message: tx('tfx_module_vdelete_confirm', { v: ver }), confirmText: t('delete', 'Delete'), danger: true });
  if (!ok) return;
  const r = await tfApi('DELETE', `/api/terraform/modules/${id}/versions/${encodeURIComponent(ver)}`);
  if (!r.ok) { tfErr(r); return; }
  showToast(tx('tfx_deleted'), 'success');
  await loadTfModules();
  await openTfModuleDetail(id);
}

async function deleteTfModule(id) {
  const ok = await showConfirm({ title: t('delete', 'Delete'), message: tx('tfx_module_delete_confirm', { n: S.tfModule ? S.tfModule.name : '#' + id }), confirmText: t('delete', 'Delete'), danger: true });
  if (!ok) return;
  const r = await tfApi('DELETE', '/api/terraform/modules/' + id);
  if (!r.ok) { tfErr(r); return; }
  tfDlgClose(1);
  showToast(tx('tfx_deleted'), 'success');
  await loadTfModules();
}

S.langRerenderHooks.push(renderTfWorkspaces, tfRenderWsHead, () => {
  if (!tfDrawerOpen()) return;
  if (S.tfWsTab === 'config' && window.TfUI && window.TfUI.cfg && window.TfUI.cfg.isDirty && window.TfUI.cfg.isDirty()) return;
  if (S.tfWsTab === 'settings' && typeof tfSetDirty === 'function' && tfSetDirty()) return;
  switchTfWsTab(S.tfWsTab);
});

export { tx, tfApi, tfErr, tfJ, tfEl, tfTime, tfCsv, tfStatusCls, tfBadge, tfCopy, tfCopyData, tfDlg, tfDlgClose, tfDlgErr, tfFoot, tfPrimary, tfField, tfInput, tfSelect, tfCheck, tfV, tfChk, tfEmpty, tfFilesEditorHTML, tfFeInit, tfFeRender, tfFeStore, tfFeSelect, tfFeAdd, tfFeRemove, tfFeCollect, loadTfStatus, tfBinText, renderTfGate, toggleTfGate, switchTfTab, onTfTabKeydown, loadTfTabData, refreshTfSection, loadTfWorkspaces, getTfWsCollapsed, toggleTfWsGroup, setTfWsViewMode, renderTfWorkspaces, openTfWorkspaceModal, tfWfSetSource, tfWfSourceChanged, tfWfAuthChanged, saveTfWorkspace, deleteTfWorkspace, tfDrawerOpen, tfConfirmLeave, tfSwitchTabGuarded, openTfWorkspace, tfShowDrawer, closeTfWorkspace, tfToggleDrawerWide, tfApplyDrawerWide, tfReloadWs, tfRenderWsHead, switchTfWsTab, tfRenderOverview, tfLoadOutputs, tfToggleReveal, tfCopyOutput, tfStopRunPoll, openTfRunDialog, tfRunTypeChanged, startTfRun, tfRenderRuns, tfLoadRunList, tfOpenRun, tfOpenRunFinal, tfPollLog, tfVal, tfDiffHTML, tfToggleDiff, tfDiffAll, tfPolicyHTML, tfCostHTML, tfRenderRun, tfApplyRun, tfDiscardRun, tfCancelRun, tfLockCardHTML, openTfLockDialog, tfLock, tfUnlock, tfForceUnlock, tfAfterLockChange, tfRenderState, tfFilterResources, tfRenderVersions, tfToggleDiffSel, tfDiffVersions, openTfRollbackDialog, tfRollback, openTfImportDialog, tfImportFile, tfImportState, tfVarBase, tfRenderVariables, tfVarsTable, openTfVarDialog, tfVarCatChanged, tfVarSensChanged, saveTfVar, deleteTfVar, tfVdValue, tfVdRow, tfDetectVars, tfDetectAdd, tfRenderDrift, tfCheckDrift, tfWebhookURL, tfVcsTest, tfRotateWebhook, loadTfVarsets, openTfVarsetModal, saveTfVarset, deleteTfVarset, loadTfPolicies, openTfPolicyModal, tfPolKindChanged, saveTfPolicy, deleteTfPolicy, loadTfModules, openTfModuleEdit, openTfModuleModal, saveTfModule, openTfModuleDetail, openTfVersionDialog, tfVerSrcChanged, tfVerAuthChanged, saveTfVersion, deleteTfModuleVersion, deleteTfModule };
