/* Gate tab extra: Terraform notification events card. */
const TF_NF_EVENTS = ['awaiting_approval', 'errored', 'applied', 'drift'];

async function tfGateExtras(el) {
  if (!el) return;
  el.innerHTML = `<div class="card tf-nf-card" id="tfNotifyCard">
    <div class="card-title">${tfE(tx('tfnf_title'))}</div>
    <div class="tf-nf-hint">${tfE(tx('tfnf_hint'))}</div>
    <div class="tf-nf-list">${TF_NF_EVENTS.map(k => `<label class="tf-nf-item"><input type="checkbox" data-nf="${k}" /> <span>${tfE(tx('tfnf_ev_' + k))}</span></label>`).join('')}</div>
    <div class="tf-set-actions">
      <button type="button" class="btn btn-sm btn-primary" id="tfNfSave">${tfE(t('save', 'Save'))}</button>
      <button type="button" class="btn btn-sm" id="tfNfTest">${tfE(tx('tfnf_test'))}</button>
      <span id="tfNfMsg" class="tf-nf-msg" role="status"></span>
    </div></div>`;
  const msg = (s, bad) => { const m = tfEl('tfNfMsg'); if (m) { m.textContent = s; m.style.color = bad ? 'var(--danger)' : 'var(--text-muted)'; } };
  const boxes = () => Array.from(el.querySelectorAll('[data-nf]'));
  const r = await tfApi('GET', '/api/terraform/notifications');
  if (!r.ok) { msg(r.data.error || 'Error', true); return; }
  const ev = r.data.events || {};
  boxes().forEach(b => { b.checked = !!ev[b.dataset.nf]; });

  tfEl('tfNfSave').addEventListener('click', async () => {
    const events = {};
    boxes().forEach(b => { events[b.dataset.nf] = b.checked; });
    const s = await tfApi('PUT', '/api/terraform/notifications', { events });
    if (!s.ok) { msg(s.data.error || 'Error', true); return; }
    msg('');
    showToast(tx('tfnf_saved'), 'success');
  });
  tfEl('tfNfTest').addEventListener('click', async () => {
    const btn = tfEl('tfNfTest');
    const restore = SP.busy(btn, tx('tfnf_testing'));
    const m = tfEl('tfNfMsg');
    if (m) { m.textContent = ''; m.style.color = ''; m.innerHTML = SP.html(tx('tfnf_testing')); }
    let s;
    try {
      s = await tfApi('POST', '/api/terraform/notifications/test');
    } finally {
      restore();
    }
    if (s.ok && s.data.ok !== false) { msg(''); showToast(tx('tfnf_test_ok'), 'success'); return; }
    msg(tx('tfnf_test_fail', { err: s.data.error || 'Error' }), true);
  });
}
