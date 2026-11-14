/* State tab additions: state rm / mv / import via POST .../state/ops (always behind a confirm). */
let tfStBusy = false; // a state operation is running: no second submit
tfTabRenderers.state = async function (body) {
  await tfRenderState(body);
  const filter = tfEl('tfResFilter');
  if (!filter) return;
  const locked = !!(tfWs && tfWs.locked);
  filter.insertAdjacentHTML('afterend', `<div class="tf-st-bar"><button type="button" class="btn btn-sm" id="tfStImport" ${locked ? 'disabled' : ''}>${svgIcon('download', 'icon-xs')} ${tfE(tx('tfst_import'))}</button>
    <span class="tf-st-hint">${tfE(tx(locked ? 'tfst_locked_hint' : 'tfst_ops_hint'))}</span></div>`);
  tfEl('tfStImport').addEventListener('click', () => tfStDialog('import', ''));
  tfEl('tfResList').addEventListener('click', e => {
    const b = e.target.closest('[data-st-rm],[data-st-mv]');
    if (!b || b.disabled) return;
    e.preventDefault(); // the buttons live in <summary>: do not toggle the details
    if (b.dataset.stRm) tfStRun('rm', b.dataset.stRm, '', '');
    else tfStDialog('mv', b.dataset.stMv);
  });
  tfStDecorate();
};

// The resource list is re-rendered on every filter keystroke: wrap it to re-add the action buttons.
const tfFilterResourcesBase = tfFilterResources;
tfFilterResources = function () { tfFilterResourcesBase(); tfStDecorate(); };

function tfStDecorate() {
  const locked = !!(tfWs && tfWs.locked);
  document.querySelectorAll('#tfResList details').forEach(d => {
    const sum = d.querySelector('summary');
    if (sum.querySelector('.tf-st-act')) return;
    const addr = sum.querySelector('.mono').textContent;
    sum.insertAdjacentHTML('beforeend', `<span class="tf-st-act"><button type="button" class="btn btn-sm" data-st-mv="${tfE(addr)}" ${locked ? 'disabled' : ''}>${tfE(tx('tfst_move'))}</button>
      <button type="button" class="btn btn-sm btn-danger" data-st-rm="${tfE(addr)}" ${locked ? 'disabled' : ''}>${svgIcon('trash-2', 'icon-xs')} ${tfE(tx('tfst_remove'))}</button></span>`);
  });
}

// Client-side sanity only (module paths, ["key"] and [0] indexes allowed); the server validates authoritatively.
function tfStAddrOK(a) { return !!a && !a.startsWith('-') && /^[^\s'"`$;&|<>\\(){}*?!#~]+$/.test(a.replace(/\["[^"\\]*"\]/g, '[]')); }

function tfStDialog(op, addr) {
  const mv = op === 'mv';
  const fields = mv
    ? tfField(tx('tfst_address'), tfInput('tfStAddr', addr, 'readonly')) + tfField(tx('tfst_move_to'), tfInput('tfStTo', addr, 'autocomplete="off"'))
    : tfField(tx('tfst_address'), tfInput('tfStAddr', '', 'autocomplete="off" placeholder="aws_instance.web"')) + tfField(tx('tfst_import_id'), tfInput('tfStId', '', 'autocomplete="off"'));
  tfDlg(1, tx(mv ? 'tfst_move_title' : 'tfst_import_title'), `<div class="dk-form-grid">${fields}</div>`,
    tfFoot(1, tfPrimary(tx('tfst_run'), 'tfStSubmit(\'' + op + '\')')), 560);
}

function tfStSubmit(op) {
  const addr = tfV('tfStAddr').trim(), to = tfV('tfStTo').trim(), id = tfV('tfStId').trim();
  if (!tfStAddrOK(addr) || (op === 'mv' && !tfStAddrOK(to))) { tfDlgErr(1, tx('tfst_addr_bad')); return; }
  if (op === 'import' && !id) { tfDlgErr(1, tx('tfst_id_required')); return; }
  tfStRun(op, addr, to, id);
}

async function tfStRun(op, addr, to, id) {
  if (tfStBusy) return; // no double submit while an operation runs
  const bin = tfWs.binary || 'terraform';
  const cmd = op === 'rm' ? `${bin} state rm ${addr}` : op === 'mv' ? `${bin} state mv ${addr} ${to}` : `${bin} import ${addr} ${id}`;
  tfStBusy = true;
  let ok = false;
  try {
    ok = await showConfirm({ title: tx('tfst_confirm_title'), message: tx('tfst_confirm_msg', { cmd }), confirmText: tx('tfst_run'), danger: op !== 'import' });
  } finally {
    if (!ok) tfStBusy = false; // cancelled: allow another run
  }
  if (!ok) return;
  // The operation can run for minutes: the dialog stays open with a live elapsed counter and disabled buttons.
  const t0 = Date.now();
  tfDlg(1, tx('tfst_result_title'), SP.banner(tx('tfst_running', { cmd }), t0),
    `<button type="button" class="btn btn-sm" disabled>${tfE(tx('tfst_running_short'))}</button>`, 720);
  let r;
  try {
    r = await tfApi('POST', `/api/terraform/workspaces/${tfWs.id}/state/ops`, { op, address: addr, to, import_id: id });
  } catch (e) {
    r = { ok: false, data: { error: e.message } };
  } finally {
    tfStBusy = false;
  }
  const d = r.data || {};
  if (r.ok) {
    tfDlgClose(1);
    showToast(tx('tfst_done', { v: d.state_version }), 'success');
  } else {
    // Failure: the dialog stays open and shows the error inline.
    tfDlg(1, tx('tfst_result_title'), `<div class="tf-st-res err" role="alert">${tfE(tx('tfst_failed') + ': ' + (d.error || ''))}</div>
      <pre class="tf-log" style="margin-top:8px;">${tfE(d.output || '')}</pre>`,
      `<button type="button" class="btn btn-sm btn-primary" onclick="tfDlgClose(1)">${tfE(tx('tfst_close'))}</button>`, 720);
  }
  if (r.ok) switchTfWsTab('state');
  tfReloadWs();
}
