/* Settings tab: Webhook + Danger zone (same behaviour as old tfRenderSettings). */
function tfSetSideHTML(w) {
  return `<div class="card tf-set-card"><div class="card-title">${tfE(t('tf_webhook_url', 'Webhook URL'))}</div>
    <div style="font-size:var(--fs-xs);color:var(--text-muted);">${tfE(tx('tfx_webhook_hint'))}</div>
    <div class="tf-set-actions"><input type="text" id="tfWhUrl" class="form-input mono" readonly value="${tfE(tfWebhookURL(w))}" />
      <button type="button" class="btn btn-icon btn-sm" onclick="tfCopy(tfV('tfWhUrl'))" title="${tfE(tx('tfx_copy'))}">${svgIcon('copy', 'icon-xs')}</button>
      <button type="button" class="btn btn-sm" onclick="tfRotateWebhook()">${svgIcon('refresh-cw', 'icon-xs')} ${tfE(t('tf_webhook_rotate_btn', 'Rotate Secret'))}</button></div>
    <div id="tfWhSecret"></div></div>
  <div class="card tf-set-card tf-set-danger"><div class="card-title"><span style="color:var(--danger);">${tfE(tx('tfx_danger_zone'))}</span></div>
    <div><button type="button" class="btn btn-sm btn-danger" onclick="deleteTfWorkspace(${w.id})">${svgIcon('trash-2', 'icon-xs')} ${tfE(tx('tfx_delete_ws'))}</button></div></div>`;
}
