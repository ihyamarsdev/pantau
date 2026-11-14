/* Terraform Registry browser: module/provider detail (versions, inputs, outputs, README, snippet) and snippet actions. */
(function () {
  'use strict';
  const R = (window.TfUI = window.TfUI || {}).reg;
  const MOD_NAME_RE = /^[A-Za-z_][A-Za-z0-9_-]*$/;
  const defName = (n) => String(n || 'module').replace(/[^A-Za-z0-9_-]/g, '_').replace(/^[^A-Za-z_]/, '_$&');
  const cell = (v) => (v == null ? '' : typeof v === 'string' ? v : JSON.stringify(v));
  let dseq = 0, tmr = 0;

  const snipOf = (s) => (typeof s === 'string' ? { main: s, variables: '' } : { main: (s && s.main) || '', variables: (s && s.variables) || '' });
  function table(id, cols, rows, empty) {
    if (!rows.length) return `<div id="${id}" class="tf-reg-pane" hidden>${tfEmpty(empty)}</div>`;
    return `<div id="${id}" class="tf-reg-pane" hidden><table class="tf-reg-table"><thead><tr>${cols.map(c => `<th>${tfE(tx(c))}</th>`).join('')}</tr></thead>
      <tbody>${rows.join('')}</tbody></table></div>`;
  }
  function inputRow(i) {
    const type = cell(i.type).replace(/\s+/g, ' ');
    const dflt = cell(i.default).replace(/\s+/g, ' ');
    const clamp = (txt) => `<div class="tf-reg-type mono" title="${tfE(txt)}" onclick="this.classList.toggle('open')">${tfE(txt)}</div>`;
    return `<tr><td class="mono tf-reg-name">${tfE(i.name)}${i.required ? ` <span class="badge badge-errored">${tfE(tx('tfreg_required'))}</span>` : ''}</td>
      <td>${clamp(type)}</td><td>${tfE(i.description || '')}</td><td>${clamp(dflt)}</td></tr>`;
  }
  function paneHTML(d, isMod) {
    const snip = `<div id="tfRegSnipPane" class="tf-reg-pane" hidden><pre id="tfRegSnippet" class="tf-reg-pre"></pre></div>`;
    if (!isMod) return snip;
    const ins = (d.inputs || []).map(inputRow);
    const outs = (d.outputs || []).map(o => `<tr><td class="mono">${tfE(o.name)}</td><td>${tfE(o.description || '')}</td></tr>`);
    return table('tfRegInputs', ['tfreg_col_name', 'tfreg_col_type', 'tfreg_col_desc', 'tfreg_col_default'], ins, tx('tfreg_no_inputs'))
      + table('tfRegOutputs', ['tfreg_col_name', 'tfreg_col_desc'], outs, tx('tfreg_no_outputs'))
      + `<div id="tfRegReadmePane" class="tf-reg-pane" hidden>${d.readme ? `<div class="tf-md-bar"><button type="button" id="tfRegReadmeToggle" class="btn btn-sm" onclick="TfUI.reg.toggleRaw()">${tfE(tx('tfreg_readme_raw'))}</button></div>
        <div id="tfRegReadme" class="tf-md">${R.md(d.readme)}</div><pre id="tfRegReadmeRaw" class="tf-reg-pre" hidden>${tfE(d.readme)}</pre>` : tfEmpty(tx('tfreg_no_readme'))}
        ${d.readme_truncated ? `<div class="tf-reg-note">${tfE(tx('tfreg_truncated'))}</div>` : ''}</div>` + snip;
  }
  function showPane(name) {
    const m = { inputs: 'tfRegInputs', outputs: 'tfRegOutputs', readme: 'tfRegReadmePane', snippet: 'tfRegSnipPane' };
    for (const [k, id] of Object.entries(m)) { const el = R.q('#' + id); if (el) el.hidden = k !== name; }
    R.root.querySelectorAll('[data-reg-tab]').forEach(b => b.classList.toggle('active', b.dataset.regTab === name));
  }
  function snippetText() {
    const s = R.d.snippet;
    return s.main + (R.d.isMod && tfChk('tfRegDeclareVars') && s.variables ? '\n' + s.variables : '');
  }
  function snippetUI() {
    const pre = R.q('#tfRegSnippet'); if (pre) pre.textContent = snippetText();
    const bad = R.d.isMod && !MOD_NAME_RE.test(tfV('tfRegModName'));
    const nm = R.q('#tfRegModName'); if (nm) nm.setAttribute('aria-invalid', String(bad));
    for (const id of ['tfRegInsert', 'tfRegAddFile', 'tfRegCopy']) { const b = R.q('#' + id); if (b) b.disabled = bad; }
  }

  R.openItem = async function (kind, id, o) {
    o = o || {};
    const det = R.q('#tfRegDetail'); if (!det) return;
    const seq = ++dseq, isMod = kind === 'modules', root = R.root;
    det.innerHTML = SP.loading(tx('tfreg_loading'), Date.now());
    const p = id.split('/').map(encodeURIComponent).join('/');
    const qs = new URLSearchParams();
    if (o.version) qs.set('version', o.version);
    if (isMod) { qs.set('module_name', o.modName || defName(id.split('/')[1])); qs.set('declare_vars', '1'); } // variables are always fetched; the checkbox only decides use
    const [r, base] = await Promise.all([R.get(`${kind}/${p}?${qs}`), R.loadBase()]);
    if (seq !== dseq || R.root !== root) return;
    if (!r.ok) { det.innerHTML = tfEmpty(r.data.error || 'Error'); return; }
    const d = r.data, it = (isMod ? d.module : d.provider) || {};
    const ver = o.version || (isMod ? it.version : d.latest || it.version) || '';
    R.d = { kind, id, isMod, snippet: snipOf(d.snippet) };
    const link = R.safeUrl(isMod ? `${base}/modules/${p}` : d.docs_url) || R.safeUrl(`${base}/providers/${p}`);
    const versions = (d.versions || []).map(R.vstr);
    if (ver && !versions.includes(ver)) versions.unshift(ver);
    const deps = [].concat(d.providers || [], d.dependencies || []).map(x => (x && x.name) || R.vstr(x)).filter(Boolean);
    const tabs = (isMod ? ['inputs', 'outputs', 'readme'] : []).concat('snippet');
    det.innerHTML = `<div class="tf-reg-dhead"><span class="tf-reg-dname mono">${tfE(id)}</span>${link ? `<a href="${tfE(link)}" target="_blank" rel="noopener noreferrer" class="tf-reg-link">${tfE(tx(isMod ? 'tfreg_open' : 'tfreg_docs'))} ${svgIcon('external-link', 'icon-xs')}</a>` : ''}</div>
      <div class="tf-reg-ddesc">${tfE(it.description || '')}</div>${deps.length ? `<div class="tf-reg-note">${tfE(tx('tfreg_deps'))}: ${tfE(deps.join(', '))}</div>` : ''}
      <div class="tf-reg-ctl"><label>${tfE(tx('tfreg_version'))}<select id="tfRegVersion" class="form-input">${versions.map(v => `<option value="${tfE(v)}" ${v === ver ? 'selected' : ''}>${tfE(v)}</option>`).join('')}</select></label>
        ${isMod ? `<label>${tfE(tx('tfreg_mod_name'))}<input type="text" id="tfRegModName" class="form-input mono" maxlength="64" value="${tfE(o.modName || defName(id.split('/')[1]))}" /></label>
        <label class="tf-reg-chk"><input type="checkbox" id="tfRegDeclareVars" ${o.declare ? 'checked' : ''} /> ${tfE(tx('tfreg_declare_vars'))}</label>` : ''}</div>
      <div class="tf-reg-tabs" role="tablist">${tabs.map(k => `<button type="button" class="tf-reg-tab" role="tab" data-reg-tab="${k}">${tfE(tx('tfreg_' + k))}</button>`).join('')}</div>
      ${paneHTML(d, isMod)}
      <div class="tf-reg-actions">${R.hasEditor() ? `<button type="button" id="tfRegInsert" class="btn btn-sm btn-primary">${tfE(tx('tfreg_insert'))}</button>
        <button type="button" id="tfRegAddFile" class="btn btn-sm">${tfE(tx('tfreg_add_file'))}</button>` : ''}
        <button type="button" id="tfRegCopy" class="btn btn-sm">${svgIcon('copy', 'icon-xs')} ${tfE(tx('tfreg_copy'))}</button></div>`;
    showPane(tabs[0]); snippetUI();
    const opts = () => ({ version: tfV('tfRegVersion'), modName: tfV('tfRegModName'), declare: tfChk('tfRegDeclareVars') });
    const el = det;
    el.onclick = (e) => {
      const tb = e.target.closest('[data-reg-tab]'); if (tb) { showPane(tb.dataset.regTab); return; }
      if (e.target.closest('#tfRegCopy')) tfCopy(snippetText());
      else if (e.target.closest('#tfRegInsert')) R.insert();
      else if (e.target.closest('#tfRegAddFile')) R.addFile();
    };
    el.onchange = (e) => {
      if (e.target.id === 'tfRegVersion') R.openItem(kind, id, opts());
      else if (e.target.id === 'tfRegDeclareVars') { snippetUI(); }
    };
    el.oninput = (e) => { // module name changes the snippet: refetch it (debounced), keep the rest of the view
      if (e.target.id !== 'tfRegModName') return;
      snippetUI(); clearTimeout(tmr);
      if (MOD_NAME_RE.test(e.target.value)) tmr = setTimeout(() => R.refreshSnippet(opts()), 300);
    };
  };
  R.refreshSnippet = async function (o) {
    const d = R.d, seq = ++dseq; if (!d) return;
    const qs = new URLSearchParams({ version: o.version, module_name: o.modName, declare_vars: '1' });
    const r = await R.get(`modules/${d.id.split('/').map(encodeURIComponent).join('/')}?${qs}`);
    if (seq === dseq && R.d === d && r.ok) { d.snippet = snipOf(r.data.snippet); snippetUI(); }
  };
  R.fileName = () => (R.d.isMod ? `module-${tfV('tfRegModName')}.tf` : `provider-${R.d.id.split('/')[1]}.tf`);
  const wantVars = () => (R.d.isMod && tfChk('tfRegDeclareVars') ? R.d.snippet.variables : '');
  const end = (msg) => { tfDlgClose(1); showToast(msg, 'success'); if (TfUI.cfg.cm) TfUI.cfg.cm.focus(); };

  // Appends text to a file of the Configuration editor (created when missing), separated by a blank line.
  function appendTo(C, name, text) {
    if (!(name in C.files) && !C.add(name)) return false;
    const d = C.docFor(name), cur = d.getValue(), last = d.lastLine();
    d.replaceRange((cur.trim() ? (cur.endsWith('\n') ? '\n' : '\n\n') : '') + text.replace(/\n*$/, '\n'), { line: last, ch: d.getLine(last).length });
    return true;
  }
  R.insert = function () {
    const C = TfUI.cfg;
    if (!R.hasEditor()) { showToast(tx('tfreg_no_editor'), 'error'); return; }
    const cur = C.cur, doc = C.docs[cur], c = doc.getCursor(), line = doc.getLine(c.line);
    doc.replaceRange((line.slice(0, c.ch).trim() ? '\n\n' : '') + R.d.snippet.main.replace(/\n*$/, '\n') + (line.slice(c.ch).trim() ? '\n' : ''), c);
    const v = wantVars();
    if (v && appendTo(C, 'variables.tf', v)) C.open(cur);
    end(tx('tfreg_inserted'));
  };
  R.addFile = function () {
    const C = TfUI.cfg, name = R.fileName(), v = wantVars();
    if (!R.hasEditor()) { showToast(tx('tfreg_no_editor'), 'error'); return; }
    if (!appendTo(C, name, R.d.snippet.main) || (v && !appendTo(C, 'variables.tf', v))) return;
    C.open(name);
    end(tx('tfreg_file_added', { name }));
  };
})();
