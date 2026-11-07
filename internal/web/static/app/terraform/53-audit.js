/* Audit tab: workspace filter + timeline of `terraform` log entries (read-only). */
let tfAuditWs = '';

function tfAuditLevelCls(l) {
  l = String(l || '').toLowerCase();
  if (l.startsWith('err')) return 'badge-danger';
  if (l.startsWith('warn')) return 'badge-warning';
  return 'badge-info';
}

function tfAuditRowHTML(e) {
  return `<div class="tf-audit-row" data-audit>
    <span class="tf-audit-time">${tfE(tfTime(e.time))}</span>
    <span class="badge ${tfAuditLevelCls(e.level)}">${tfE(String(e.level || 'info').toUpperCase())}</span>
    <span class="tf-audit-msg">${tfE(e.message || '')}</span></div>`;
}

async function tfAuditLoad() {
  const list = tfEl('tfAuditList');
  if (!list) return;
  list.innerHTML = tfEmpty(tx('tfaud_loading'));
  const q = tfAuditWs ? '&workspace_id=' + encodeURIComponent(tfAuditWs) : '';
  const r = await tfApi('GET', '/api/terraform/audit?limit=200' + q);
  if (!tfEl('tfAuditList')) return;
  if (!r.ok) { list.innerHTML = tfEmpty(r.data.error || 'Error'); return; }
  const ev = r.data.events || [];
  list.innerHTML = ev.length ? ev.map(tfAuditRowHTML).join('') : tfEmpty(tx('tfaud_empty'));
}

function tfAuditRefresh() {
  const root = tfEl('tfAuditRoot');
  if (!root) return;
  const opts = [['', tx('tfaud_all_ws')]].concat((tfWorkspaces || []).map(w => [String(w.id), w.name]));
  if (!opts.some(o => o[0] === tfAuditWs)) tfAuditWs = '';
  root.innerHTML = `<div class="tf-audit-bar">
      <label for="tfAuditWs">${tfE(tx('tfaud_workspace'))}</label>
      <select id="tfAuditWs" class="form-input">${opts.map(([v, l]) => `<option value="${tfE(v)}" ${v === tfAuditWs ? 'selected' : ''}>${tfE(l)}</option>`).join('')}</select>
      <button type="button" class="btn btn-sm" id="tfAuditReload">${svgIcon('refresh-cw', 'icon-xs')} ${tfE(t('refresh', 'Refresh'))}</button>
    </div>
    <div class="card tf-audit-list" id="tfAuditList"></div>`;
  tfEl('tfAuditWs').addEventListener('change', e => { tfAuditWs = e.target.value; tfAuditLoad(); });
  tfEl('tfAuditReload').addEventListener('click', tfAuditLoad);
  tfAuditLoad();
}
