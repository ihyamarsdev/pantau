/* Configuration tab: "Registry" browser button and module/provider version check with one-click constraint rewrite. */
(function () {
  'use strict';
  const C = (window.TfUI = window.TfUI || {}).cfg;
  const R = window.TfUI.reg;
  let busy = false, items = [];
  const CHIP = { up_to_date: 'badge-applied', outdated: 'badge-drifted', newer_outside_constraint: 'badge-errored', unpinned: 'badge-never', unknown: 'badge-never' };

  function rowHTML(it, i) {
    const fix = it.status === 'outdated' || it.status === 'newer_outside_constraint';
    const loc = it.file ? `<span class="mono tf-regc-loc">${tfE(it.file)}${it.line ? ':' + Number(it.line) : ''}</span>` : '';
    return `<li class="tf-regc-row" data-regc="${tfE(it.kind + ':' + it.name)}">
      <span class="badge ${CHIP[it.status] || 'badge-never'}">${tfE(tx('tfregc_status_' + it.status))}</span>
      <span class="mono tf-regc-name">${tfE(it.name)}</span><span class="tf-regc-kind">${tfE(it.kind)}</span>
      <span class="mono tf-regc-con">${tfE(it.constraint || '—')}</span>
      <span class="mono tf-regc-latest">${it.latest ? tfE(tx('tfregc_latest', { v: it.latest })) : ''}</span>${loc}
      ${fix && it.suggested ? `<button type="button" class="btn btn-sm" data-regc-apply="${i}">${tfE(tx('tfregc_apply', { v: it.suggested }))}</button>` : ''}</li>`;
  }
  function show(inner) {
    const el = tfEl('tfCfgRegStatus'); if (!el) return;
    el.innerHTML = `<button type="button" class="tf-cfg-diag-x" data-regc-close aria-label="${tfE(tx('tfregc_close'))}">${svgIcon('x', 'icon-xs')}</button>${inner}`;
    el.hidden = false;
  }

  // Rewrites only the version string literal on the item's line; refuses when the line no longer holds the checked constraint.
  function apply(it) {
    const doc = C.docs[it.file], ln = Number(it.line) - 1;
    if (!doc || ln < 0 || ln >= doc.lineCount()) return false;
    const text = doc.getLine(ln);
    const m = /(\bversion\s*=\s*")([^"]*)"/.exec(text) || /^(\s*[A-Za-z0-9_-]+\s*=\s*")([^"]*)"/.exec(text);
    if (!m || m[2] !== it.constraint) return false;
    doc.replaceRange(it.suggested, { line: ln, ch: m.index + m[1].length }, { line: ln, ch: m.index + m[1].length + m[2].length });
    return true;
  }

  async function check() {
    if (busy || !tfWs) return;
    const root = C.root, id = tfWs.id, btn = tfEl('tfCfgRegCheck');
    busy = true;
    const restore = SP.busy(btn, tx('tfregc_checking'));
    let r;
    try {
      r = await tfApi('GET', `/api/terraform/workspaces/${id}/registry/check`);
    } finally {
      busy = false;
      restore();
    }
    if (C.root !== root || !tfWs || tfWs.id !== id) return; // editor re-rendered meanwhile
    if (!r.ok) { tfErr(r); return; }
    items = r.data.items || [];
    const errs = (r.data.errors || []).map(e => `<li class="tf-regc-err">${tfE(typeof e === 'string' ? e : JSON.stringify(e))}</li>`).join('');
    const note = C.isDirty() ? `<div class="tf-regc-note">${tfE(tx('tfregc_unsaved'))}</div>` : '';
    if (r.data.supported === false) { show(`<div class="tf-regc-note">${tfE(tx('tfregc_unsupported'))}</div>`); return; }
    show(`<div class="tf-regc-head">${tfE(tx('tfcfg_regcheck'))}</div>${note}`
      + (items.length ? `<ul class="tf-regc-list">${items.map(rowHTML).join('')}</ul>` : `<div class="tf-regc-note">${tfE(tx('tfregc_none'))}</div>`)
      + (errs ? `<ul class="tf-regc-errs">${errs}</ul>` : ''));
  }

  C.regInit = function () {
    tfEl('tfCfgRegistry').addEventListener('click', () => R.openDialog());
    tfEl('tfCfgRegCheck').addEventListener('click', check);
    tfEl('tfCfgRegStatus').addEventListener('click', (e) => {
      if (e.target.closest('[data-regc-close]')) { tfEl('tfCfgRegStatus').hidden = true; return; }
      const b = e.target.closest('[data-regc-apply]'); if (!b) return;
      const it = items[Number(b.dataset.regcApply)];
      if (!it || !apply(it)) { showToast(tx('tfregc_stale'), 'error'); return; }
      b.disabled = true; b.textContent = tx('tfregc_applied');
      C.fileUI();
    });
  };
})();
