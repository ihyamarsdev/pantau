/* Settings tab: General + Source cards, PUT body builder, dirty check. */
const tfIsProd = env => String(env || '').trim().toLowerCase() === 'production';

function tfSetBody(w) {
  const bins = (tfStatus && tfStatus.binaries) || [{ name: 'terraform' }, { name: 'tofu' }];
  const groups = Array.from(new Set(tfWorkspaces.map(x => (x.group_name || '').trim()).filter(Boolean))).sort();
  const envs = Array.from(new Set(['production', 'staging', 'development'].concat(tfWorkspaces.map(x => (x.environment || '').trim()).filter(Boolean))));
  const field = (label, inner, hint, id) => `<div class="dk-field"${id ? ` id="${id}"` : ''}><label>${tfE(label)}</label>${inner}${hint ? `<div class="dk-hint">${tfE(hint)}</div>` : ''}</div>`;
  const inp = (id, val, extra) => `<input type="text" id="${id}" class="form-input" value="${tfE(val == null ? '' : val)}" ${extra || ''} />`;
  const sel = (id, opts, cur) => `<select id="${id}" class="form-input">${opts.map(([v, l]) => `<option value="${tfE(v)}" ${String(v) === String(cur) ? 'selected' : ''}>${tfE(l)}</option>`).join('')}</select>`;
  const dl = (id, items) => `<datalist id="${id}">${items.map(v => `<option value="${tfE(v)}">`).join('')}</datalist>`;

  const general = `<div class="card tf-set-card"><div class="card-title">${tfE(tx('tfset_general'))}</div>
    <div class="tf-set-grid2">
      ${field(tx('tfx_name') + ' *', inp('tfSetName', w.name, 'maxlength="100" required'))}
      ${field(tx('tfx_group'), inp('tfSetGroup', w.group_name, 'list="tfSetGroupList" placeholder="' + tfE(tx('tfx_group_ph')) + '"') + dl('tfSetGroupList', groups))}
      ${field(tx('tfx_environment'), inp('tfSetEnv', w.environment, 'list="tfSetEnvList" placeholder="production"') + dl('tfSetEnvList', envs))}
      ${field(tx('tfx_tags'), inp('tfSetTags', (w.tags || []).join(', '), 'placeholder="a, b, c"'))}
      ${field(tx('tfx_binary'), sel('tfSetBinary', bins.map(b => [b.name, b.name + (b.err ? ' (' + tx('tfx_not_installed') + ')' : '')]), w.binary))}
      ${field(tx('tfx_drift_interval'), `<input type="number" id="tfSetDrift" class="form-input" min="0" value="${Number(w.drift_interval_minutes) || 0}" />`, tx('tfx_drift_off') + ': 0')}
      ${field(tx('tfset_plan_interval'), `<input type="number" id="tfSetPlanInterval" class="form-input" min="0" value="${Number(w.plan_interval_minutes) || 0}" />`, tx('tfset_plan_interval_hint'))}
      ${field(tx('tfset_auto_apply'), `<label class="tf-nf-item"><input type="checkbox" id="tfSetAutoApply" ${w.auto_apply && !tfIsProd(w.environment) ? 'checked' : ''} ${tfIsProd(w.environment) ? 'disabled' : ''} /> <span>${tfE(tx('tfset_auto_apply_hint'))}</span></label>`, tfIsProd(w.environment) ? tx('tfset_auto_apply_prod') : '', 'tfSetAutoApplyWrap')}
      ${field(tx('tfx_description'), `<textarea id="tfSetDesc" class="form-input" rows="2">${tfE(w.description || '')}</textarea>`, '', true)}
    </div></div>`;

  let source;
  if (w.source === 'git') {
    source = `<div class="card tf-set-card"><div class="card-title">${tfE(tx('tfx_source'))} · ${tfE(tx('tfx_source_git'))}</div>
    <div class="tf-set-grid2">
      ${field(tx('tfx_git_url'), inp('tfSetGitUrl', w.git_url, 'placeholder="https://github.com/org/infra.git"'), '', true)}
      ${field(tx('tfx_git_branch'), inp('tfSetGitBranch', w.git_branch, 'placeholder="main"'))}
      ${field(tx('tfx_git_path'), inp('tfSetGitPath', w.git_path, 'placeholder="envs/prod"'))}
      ${field(tx('tfx_git_auth'), sel('tfSetGitAuth', [['none', tx('tfx_git_auth_none')], ['token', tx('tfx_git_auth_token')], ['ssh', tx('tfx_git_auth_ssh')]], w.git_auth || 'none'))}
      ${field(tx('tfx_git_token'), `<input type="password" id="tfSetGitToken" class="form-input" autocomplete="new-password" placeholder="${w.git_token_set ? tfE(tx('tfx_git_token_keep')) : ''}" />`, '', '', 'tfSetTokenWrap')}
      ${field(tx('tfset_ssh_key'), `<select id="tfSetGitKey" class="form-input"></select>`, '', '', 'tfSetKeyWrap')}
    </div>
    <div class="tf-set-actions"><button type="button" class="btn btn-sm" id="tfSetGitTest">${svgIcon('git-compare', 'icon-xs')} ${tfE(t('tf_vcs_test_btn', 'Test VCS Connection'))}</button> <span id="tfVcsResult" style="font-size:var(--fs-xs);"></span></div></div>`;
  } else {
    const n = w.files ? Object.keys(w.files).length : 0;
    source = `<div class="card tf-set-card"><div class="card-title">${tfE(tx('tfx_source'))} · ${tfE(tx('tfx_source_inline'))}</div>
    <div style="font-size:var(--fs-xs);color:var(--text-muted);">${tfE(tx('tfset_inline_files').replace('{n}', n))}</div>
    <div class="tf-set-actions"><button type="button" class="btn btn-sm" id="tfSetOpenConfig">${svgIcon('pencil', 'icon-xs')} ${tfE(tx('tfset_open_config'))}</button></div>
    <div style="font-size:var(--fs-xs);color:var(--text-muted);">${tfE(tx('tfset_inline_note'))}</div></div>`;
  }
  return general + source + tfCloudLinkHTML(w);
}

/* Full PUT body: tfWs values overridden by the form; files/source untouched. */
function tfSetCollect() {
  const w = tfWs;
  const body = {
    name: tfV('tfSetName').trim(), description: tfV('tfSetDesc'), group_name: tfV('tfSetGroup').trim(),
    environment: tfV('tfSetEnv').trim(), tags: tfCsv(tfV('tfSetTags')), binary: tfV('tfSetBinary'),
    source: w.source, drift_interval_minutes: parseInt(tfV('tfSetDrift'), 10) || 0,
    plan_interval_minutes: parseInt(tfV('tfSetPlanInterval'), 10) || 0, auto_apply: tfChk('tfSetAutoApply') && !tfIsProd(tfV('tfSetEnv')),
    cloud_account_ids: tfSetCloudIds(w),
  };
  if (w.source === 'inline') body.files = w.files || {};
  else {
    Object.assign(body, { git_url: tfV('tfSetGitUrl').trim(), git_branch: tfV('tfSetGitBranch').trim(), git_path: tfV('tfSetGitPath').trim(), git_auth: tfV('tfSetGitAuth') });
    if (body.git_auth === 'token' && tfV('tfSetGitToken')) body.git_token = tfV('tfSetGitToken');
    if (body.git_auth === 'ssh') body.git_key_id = parseInt(tfV('tfSetGitKey'), 10) || 0;
  }
  return body;
}

function tfSetBase(w) {
  const base = {
    name: w.name, description: w.description || '', group_name: w.group_name || '', environment: w.environment || '',
    tags: (w.tags || []).slice(), binary: w.binary || '', source: w.source, drift_interval_minutes: Number(w.drift_interval_minutes) || 0,
    plan_interval_minutes: Number(w.plan_interval_minutes) || 0, auto_apply: !!w.auto_apply && !tfIsProd(w.environment),
    cloud_account_ids: (w.cloud_account_ids || []).map(Number).sort((a, b) => a - b),
  };
  if (w.source === 'inline') base.files = w.files || {};
  else {
    Object.assign(base, { git_url: w.git_url || '', git_branch: w.git_branch || '', git_path: w.git_path || '', git_auth: w.git_auth || 'none' });
    if (base.git_auth === 'ssh') base.git_key_id = Number(w.git_key_id) || 0;
  }
  return base;
}

function tfSetDirty() {
  const b = tfEl('tfSetName');
  if (!b) return false;
  return JSON.stringify(tfSetCollect()) !== JSON.stringify(tfSetBase(tfWs));
}
