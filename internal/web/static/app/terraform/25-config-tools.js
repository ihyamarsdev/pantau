/* Terraform Configuration tab: Format (terraform fmt) and Validate (init + validate) on the editor's current files. */
(function () {
  'use strict';
  const C = (window.TfUI = window.TfUI || {}).cfg;
  let busy = false;

  const api = (tool, files) => tfApi('POST', `/api/terraform/workspaces/${tfWs.id}/tools/${tool}`, { files });
  // A response is stale when the editor was re-rendered (workspace change / tab switch) meanwhile.
  const stale = (root, id) => C.root !== root || !tfWs || tfWs.id !== id;

  function fileRef(name, line) {
    if (!(name in C.files)) return name ? `<span class="mono">${tfE(name)}${line ? ':' + Number(line) : ''}</span>` : '';
    return `<button type="button" class="tf-cfg-loc mono" data-goto="${tfE(name)}" data-line="${Number(line) || 0}">${tfE(name)}${line ? ':' + Number(line) : ''}</button>`;
  }
  function item(sev, summary, detail, name, line) {
    return `<li class="tf-cfg-dg"><div class="tf-cfg-dg-head"><span class="tf-cfg-sev tf-cfg-sev-${sev}">${tfE(tx('tfcfg_sev_' + sev))}</span>
      <span class="tf-cfg-dg-sum">${tfE(summary)}</span>${fileRef(name, line)}</div>${detail ? `<pre class="tf-cfg-dg-detail">${tfE(detail)}</pre>` : ''}</li>`;
  }
  function showDiag(title, cls, items, raw) {
    const el = tfEl('tfCfgDiag'); if (!el) return;
    el.innerHTML = `<button type="button" class="tf-cfg-diag-x" data-diag-close aria-label="${tfE(tx('tfcfg_diag_close'))}">${svgIcon('x', 'icon-xs')}</button>
      <details open><summary class="tf-cfg-diag-sum ${cls}">${tfE(title)}</summary>
      ${raw ? `<pre class="tf-cfg-dg-detail">${tfE(raw)}</pre>` : ''}${items ? `<ul class="tf-cfg-dg-list">${items}</ul>` : ''}</details>`;
    el.hidden = false;
  }

  async function doFormat() {
    if (busy) return;
    const root = C.root, id = tfWs.id, files = C.collect();
    busy = true; tfEl('tfCfgFmt').disabled = true;
    const r = await api('fmt', files);
    busy = false;
    if (stale(root, id)) return;
    tfEl('tfCfgFmt').disabled = false;
    if (!r.ok) { tfErr(r); return; }
    const out = r.data.files || {};
    let n = 0;
    C.cm.operation(() => {
      for (const name of r.data.changed || []) {
        const d = C.docs[name];
        // Skip files edited while the request was in flight; one replaceRange keeps undo a single step.
        if (!d || typeof out[name] !== 'string' || d.getValue() !== files[name]) continue;
        const last = d.lastLine();
        d.replaceRange(out[name], { line: 0, ch: 0 }, { line: last, ch: d.getLine(last).length });
        n++;
      }
    });
    C.fileUI();
    const errs = Object.entries(r.data.errors || {});
    if (errs.length) showDiag(tx('tfcfg_fmt_errs'), 'err', errs.map(([name, msg]) => item('error', msg, '', name, 0)).join(''));
    else if (n || (r.data.changed || []).length) { const el = tfEl('tfCfgDiag'); if (el) el.hidden = true; }
    showToast(n ? tx('tfcfg_formatted', { n }) : tx('tfcfg_already_fmt'), errs.length ? 'error' : 'success');
  }

  async function doValidate() {
    if (busy) return;
    const root = C.root, id = tfWs.id, btn = tfEl('tfCfgValidate'), html = btn.innerHTML;
    busy = true; btn.disabled = true; tfEl('tfCfgFmt').disabled = true;
    btn.innerHTML = `<span class="spinner">${svgIcon('loader-circle', 'icon-xs')}</span> ${tfE(tx('tfcfg_validating'))}`;
    const r = await api('validate', C.collect());
    busy = false;
    if (stale(root, id)) return;
    btn.disabled = false; btn.innerHTML = html; tfEl('tfCfgFmt').disabled = false;
    if (!r.ok) { tfErr(r); return; }
    const d = r.data;
    if (d.init_error) { showDiag(tx('tfcfg_init_err'), 'err', '', d.init_error); return; }
    const diags = d.diagnostics || [];
    const e = diags.filter(x => x.severity === 'error').length;
    const title = d.ok ? tx('tfcfg_valid_ok') + (diags.length ? ` — ${tx('tfcfg_diag_counts', { e, w: diags.length - e })}` : '')
      : tx('tfcfg_diag_counts', { e, w: diags.length - e });
    showDiag(title, d.ok ? 'ok' : 'err', diags.map(x => item(x.severity === 'warning' ? 'warning' : 'error', x.summary || '', x.detail || '', x.file, x.line)).join(''));
  }

  C.toolsInit = function () {
    tfEl('tfCfgFmt').addEventListener('click', doFormat);
    tfEl('tfCfgValidate').addEventListener('click', doValidate);
    tfEl('tfCfgDiag').addEventListener('click', (ev) => {
      if (ev.target.closest('[data-diag-close]')) { tfEl('tfCfgDiag').hidden = true; return; }
      const g = ev.target.closest('[data-goto]'); if (!g) return;
      C.open(g.dataset.goto);
      const line = Number(g.dataset.line);
      if (line > 0) C.cm.setCursor({ line: line - 1, ch: 0 });
      C.cm.scrollIntoView(null, 120);
      C.cm.focus();
    });
    C.cm.addKeyMap({ 'Shift-Ctrl-F': doFormat, 'Shift-Cmd-F': doFormat });
    if (C.regInit) C.regInit(); // 62-registry-check.js
  };
})();
