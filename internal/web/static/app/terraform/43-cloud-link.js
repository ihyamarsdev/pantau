/* Settings tab: "Cloud accounts" link card (checkbox per account). */
function tfCaLinkListHTML(linked) {
  const accs = TfUI.cloud.accounts;
  if (!accs.length) {
    return `<div class="tf-ca-link-empty">${tfE(tx('tfca_link_empty'))}</div>
      <div><button type="button" class="btn btn-sm" data-ca-manage>${svgIcon('cloud', 'icon-xs')} ${tfE(tx('tfca_link_manage'))}</button></div>`;
  }
  return accs.map(a => `<label class="tf-ca-linkrow"><input type="checkbox" data-ca-link="${Number(a.id)}" ${linked.includes(a.id) ? 'checked' : ''} />
    <span class="tf-ca-linkname" title="${tfE(a.name)}">${tfE(a.name)}</span>${tfCaBadge(a.provider)}${tfCaChip(a)}</label>`).join('');
}

function tfCloudLinkHTML(w) {
  const linked = (w.cloud_account_ids || []).map(Number);
  const loaded = TfUI.cloud.loaded;
  // Cache empty: fetch once after the card is inserted, then fill the (still untouched) list.
  if (!loaded) {
    setTimeout(async () => {
      const r = await tfCaLoad();
      const list = tfWs === w && tfEl('tfSetCloud') && tfEl('tfSetCloud').querySelector('.tf-ca-linklist');
      if (list && !list.querySelector('[data-ca-link]')) list.innerHTML = r.ok ? tfCaLinkListHTML(linked) : `<div class="tf-ca-link-empty">${tfE(r.data.error || 'Error')}</div>`;
    }, 0);
  }
  return `<div class="card tf-set-card tf-ca-link" id="tfSetCloud"><div class="card-title">${tfE(tx('tfca_link_title'))}</div>
    <div class="tf-ca-link-hint">${tfE(tx('tfca_link_hint'))}</div>
    <div class="tf-ca-linklist">${loaded ? tfCaLinkListHTML(linked) : `<div class="tf-ca-link-empty">${tfE(tx('tfca_loading'))}</div>`}</div></div>`;
}

// Linked ids as currently checked; stored value until the list is rendered (or when no account exists).
function tfSetCloudIds(w) {
  const box = tfEl('tfSetCloud');
  if (!box || !box.querySelector('[data-ca-link]')) return (w.cloud_account_ids || []).map(Number).sort((a, b) => a - b);
  return Array.from(box.querySelectorAll('[data-ca-link]:checked')).map(c => Number(c.dataset.caLink)).sort((a, b) => a - b);
}

async function tfCaLinkManage() {
  await closeTfWorkspace();
  if (!tfWs) switchTfTab('cloud');
}
