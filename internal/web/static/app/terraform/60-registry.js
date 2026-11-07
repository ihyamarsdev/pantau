/* Terraform Registry browser: kind toggle, search, paged results. Detail view lives in 61-registry-detail.js. */
(function () {
  'use strict';
  const R = (window.TfUI = window.TfUI || {}).reg = { st: { kind: 'modules', q: '', provider: '' }, root: null, dlg: false, base: '', next: 0, seq: 0 };
  const PAGE = 20;

  R.get = (path) => tfApi('GET', '/api/terraform/registry/' + path);
  // Registry data is untrusted: only http(s) URLs may become links.
  R.safeUrl = (u) => (/^https?:\/\//i.test(u || '') ? u : '');
  R.num = (n) => (Number(n) || 0).toLocaleString();
  R.vstr = (v) => (v && typeof v === 'object' ? v.version || v.name || '' : String(v == null ? '' : v));
  R.q = (sel) => (R.root ? R.root.querySelector(sel) : null);
  R.loadBase = async function () {
    if (R.base) return R.base;
    const r = await R.get('settings');
    R.base = (r.ok && (R.safeUrl(r.data.base_url) || R.safeUrl(r.data.default_url))) || 'https://registry.terraform.io';
    return R.base;
  };

  function kindBtn(k) {
    return `<button type="button" class="btn btn-sm${R.st.kind === k ? ' btn-primary' : ''}" data-reg-kind="${k}" aria-pressed="${R.st.kind === k}">${tfE(tx('tfreg_' + k))}</button>`;
  }
  R.mount = function (root, dlg) {
    R.root = root; R.dlg = !!dlg; R.seq++;
    const s = R.st;
    root.innerHTML = `<div class="tf-reg"><div class="tf-reg-bar">
      <div class="tf-reg-kinds" role="group">${kindBtn('modules')}${kindBtn('providers')}</div>
      <input type="text" id="tfRegQuery" class="form-input" placeholder="${tfE(tx('tfreg_search_ph'))}" aria-label="${tfE(tx('tfreg_search'))}" value="${tfE(s.q)}" maxlength="100" />
      <input type="text" id="tfRegProvider" class="form-input" placeholder="${tfE(tx('tfreg_provider_ph'))}" value="${tfE(s.provider)}" maxlength="64" ${s.kind === 'modules' ? '' : 'hidden'} />
      <button type="button" id="tfRegSearch" class="btn btn-sm btn-primary">${svgIcon('search', 'icon-xs')} ${tfE(tx('tfreg_search'))}</button></div>
      <div class="tf-reg-body"><div class="tf-reg-list"><div id="tfRegResults" class="tf-reg-results" aria-live="polite">${tfEmpty(tx('tfreg_enter_query'))}</div>
        <button type="button" id="tfRegMore" class="btn btn-sm" hidden>${tfE(tx('tfreg_more'))}</button></div>
      <div id="tfRegDetail" class="tf-reg-detail">${tfEmpty(tx('tfreg_select_hint'))}</div></div></div>`;
    const el = root.firstElementChild; // fresh per mount: listeners never stack on the reused root
    el.addEventListener('click', (e) => {
      const k = e.target.closest('[data-reg-kind]');
      if (k) { if (k.dataset.regKind !== s.kind) { s.kind = k.dataset.regKind; R.mount(root, R.dlg); } return; }
      if (e.target.closest('#tfRegSearch')) { R.search(false); return; }
      if (e.target.closest('#tfRegMore')) { R.search(true); return; }
      const it = e.target.closest('[data-reg-item]');
      if (it) { root.querySelectorAll('[data-reg-item].active').forEach(x => x.classList.remove('active')); it.classList.add('active'); R.openItem(s.kind, it.dataset.regItem); }
    });
    el.addEventListener('keydown', (e) => { if (e.key === 'Enter' && e.target.matches('#tfRegQuery, #tfRegProvider')) { e.preventDefault(); R.search(false); } });
    if (s.q) R.search(false);
  };

  function rowHTML(kind, x) {
    const id = kind === 'modules' ? `${x.namespace}/${x.name}/${x.provider}` : `${x.namespace}/${x.name}`;
    const badge = kind === 'modules'
      ? (x.verified ? `<span class="badge badge-applied">${tfE(tx('tfreg_verified'))}</span>` : '')
      : (x.tier ? `<span class="badge badge-never">${tfE(x.tier)}</span>` : '');
    const meta = [kind === 'modules' && x.version ? 'v' + x.version : '', x.downloads ? tx('tfreg_downloads', { n: R.num(x.downloads) }) : ''].filter(Boolean).join(' · ');
    return `<button type="button" class="tf-reg-row" data-reg-item="${tfE(id)}"><span class="tf-reg-rname mono">${tfE(id)}</span>${badge}
      <span class="tf-reg-rdesc">${tfE(x.description || '')}</span><span class="tf-reg-rmeta">${tfE(meta)}</span></button>`;
  }
  R.search = async function (append) {
    const s = R.st, root = R.root;
    if (!append) { s.q = tfV('tfRegQuery').trim(); s.provider = tfV('tfRegProvider').trim(); R.next = 0; }
    const box = R.q('#tfRegResults'), more = R.q('#tfRegMore');
    if (!box) return;
    if (!s.q) { box.innerHTML = tfEmpty(tx('tfreg_enter_query')); more.hidden = true; return; }
    const seq = ++R.seq, kind = s.kind;
    if (!append) box.innerHTML = tfEmpty(tx('tfreg_searching'));
    more.disabled = true;
    const qs = 'q=' + encodeURIComponent(s.q);
    const r = kind === 'modules'
      ? await R.get(`modules?${qs}&provider=${encodeURIComponent(s.provider)}&offset=${R.next}&limit=${PAGE}`)
      : await R.get(`providers?${qs}&page=${R.next || 1}`);
    if (seq !== R.seq || R.root !== root) return; // superseded by a newer search / re-mount
    more.disabled = false;
    if (!r.ok) { box.innerHTML = tfEmpty(r.data.error || 'Error'); more.hidden = true; return; }
    const list = (kind === 'modules' ? r.data.modules : r.data.providers) || [];
    if (!append) box.innerHTML = '';
    box.insertAdjacentHTML('beforeend', list.map(x => rowHTML(kind, x)).join(''));
    if (!box.children.length) box.innerHTML = tfEmpty(tx('tfreg_none'));
    R.next = kind === 'modules' ? Number(r.data.next_offset) || 0 : Number(r.data.next_page) || 0;
    more.hidden = !R.next || !list.length;
  };

  // Wide dialog hosting the browser, for the Configuration tab. The tab panel root is emptied so ids stay unique.
  R.openDialog = function () {
    const tab = tfEl('tfRegistryRoot'); if (tab) tab.innerHTML = '';
    tfDlg(1, tx('tfreg_title'), '<div id="tfRegDlgRoot"></div>', `<button type="button" class="btn btn-sm" onclick="tfDlgClose(1)">${tfE(tx('tfreg_dlg_close'))}</button>`, 1280);
    R.mount(tfEl('tfRegDlgRoot'), true);
  };
  R.hasEditor = () => { const C = TfUI.cfg; return !!(R.dlg && C && C.root && C.root.isConnected && C.cm); };
})();
