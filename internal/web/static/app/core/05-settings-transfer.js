/* Settings, 2FA, snapshot restore, S3, and cross-host transfers.
 * Native ES module fragment of the former monolithic inline script.
 * Air-gapped: no bundler, no CDN. Shared state lives in the mutable S object
 * from 00-state.js; cross-domain functions are imported explicitly. */
import { S } from './00-state.js';
import { svgIcon } from './00-foundation.js';
import { t, showConfirm, getDefaultHostsView } from './01-i18n-auth.js';
import { loadVpnNetworks, showToast } from './02-hosts.js';
import { loadSettingsPresets } from './04-terminal.js';
import { openModal, closeModal, escapeHtml } from './06-modal-docs-activity.js';

function switchSettingsTab(tab) {
  S.currentSettingsTab = tab;
  document.querySelectorAll('#settingsTabs .tab').forEach(t => t.classList.remove('active'));
  if (tab === 'general') document.getElementById('tabSettingsGeneral')?.classList.add('active');
  else if (tab === 'security') document.getElementById('tabSettingsSecurity')?.classList.add('active');
  else if (tab === 'keys') document.getElementById('tabSettingsKeys')?.classList.add('active');
  else if (tab === 'notif') document.getElementById('tabSettingsNotif')?.classList.add('active');
  else if (tab === 'backup') document.getElementById('tabSettingsBackup')?.classList.add('active');
  else if (tab === 'presets') document.getElementById('tabSettingsPresets')?.classList.add('active');
  else if (tab === 'updates') document.getElementById('tabSettingsUpdates')?.classList.add('active');
  else if (tab === 's3') document.getElementById('tabSettingsS3')?.classList.add('active');
  else if (tab === 'agent') document.getElementById('tabSettingsAgent')?.classList.add('active');
  else if (tab === 'networks') document.getElementById('tabSettingsNetworks')?.classList.add('active');

  const panelGeneral = document.getElementById('settingsPanelGeneral');
  const panelSecurity = document.getElementById('settingsPanelSecurity');
  const panelKeys = document.getElementById('settingsPanelKeys');
  const panelNotif = document.getElementById('settingsPanelNotif');
  const panelBackup = document.getElementById('settingsPanelBackup');
  const panelPresets = document.getElementById('settingsPanelPresets');
  const panelUpdates = document.getElementById('settingsPanelUpdates');
  const panelS3 = document.getElementById('settingsPanelS3');
  const panelAgent = document.getElementById('settingsPanelAgent');
  const panelNetworks = document.getElementById('settingsPanelNetworks');

  if (panelGeneral) panelGeneral.style.display = tab === 'general' ? 'flex' : 'none';
  if (panelSecurity) panelSecurity.style.display = tab === 'security' ? 'flex' : 'none';
  if (panelKeys) panelKeys.style.display = tab === 'keys' ? 'flex' : 'none';
  if (panelNotif) panelNotif.style.display = tab === 'notif' ? 'flex' : 'none';
  if (panelBackup) panelBackup.style.display = tab === 'backup' ? 'flex' : 'none';
  if (panelPresets) panelPresets.style.display = tab === 'presets' ? 'flex' : 'none';
  if (panelUpdates) panelUpdates.style.display = tab === 'updates' ? 'flex' : 'none';
  if (panelS3) panelS3.style.display = tab === 's3' ? 'flex' : 'none';
  if (panelAgent) panelAgent.style.display = tab === 'agent' ? 'flex' : 'none';
  if (panelNetworks) panelNetworks.style.display = tab === 'networks' ? 'flex' : 'none';
  if (tab === 'networks') loadVpnNetworks();
  if (tab === 'agent' && window.AG) window.AG.loadSettings();

  if (tab === 'keys') {
    loadSSHKeys();
  } else if (tab === 'presets') {
    loadSettingsPresets();
  } else if (tab === 'updates') {
    checkForUpdates(false);
  } else if (tab === 'security') {
    setTimeout(() => {
      const pubKeyEl = document.getElementById('settingPubKey');
      if (pubKeyEl) {
        pubKeyEl.style.height = 'auto';
        pubKeyEl.style.height = (pubKeyEl.scrollHeight + 4) + 'px';
      }
    }, 30);
  }
}

function copyPubKey() {
  const el = document.getElementById('settingPubKey');
  navigator.clipboard.writeText(el.value);
  const btn = document.getElementById('btnCopyKey');
  if (btn) {
    const original = btn.innerText;
    btn.innerText = t('copied');
    btn.style.background = 'var(--success)';
    setTimeout(() => {
      btn.innerText = original;
      btn.style.background = '';
    }, 1500);
  }
}

async function loadSSHKeys() {
  const container = document.getElementById('sshKeysListContainer');
  if (!container) return;
  try {
    const res = await fetch('/api/ssh-keys');
    if (!res.ok) {
      container.innerHTML = `<div style="color: var(--danger); font-size: 0.85rem; padding: 12px;">Failed to load SSH keys</div>`;
      return;
    }
    const keys = await res.json() || [];
    S.cachedSSHKeys = keys;
    renderSSHKeys(keys);
  } catch (err) {
    container.innerHTML = `<div style="color: var(--danger); font-size: 0.85rem; padding: 12px;">${escapeHtml(err.message)}</div>`;
  }
}

function renderSSHKeys(keys) {
  const container = document.getElementById('sshKeysListContainer');
  if (!container) return;

  if (!keys || keys.length === 0) {
    container.innerHTML = `
      <div class="empty-state" style="padding: 24px;">
        <div class="empty-state-icon">${svgIcon('key-round', 'icon-xl text-faint')}</div>
        <div class="empty-state-title" data-i18n="no_keys_title">${t('no_keys_title', 'No SSH Keys in Library')}</div>
        <div class="empty-state-desc" data-i18n="no_keys_desc">${t('no_keys_desc', 'Add your first private key above to authenticate hosts across your infrastructure.')}</div>
      </div>
    `;
    return;
  }

  container.innerHTML = `
    <div style="overflow-x: auto;">
      <table style="width: 100%; border-collapse: collapse; font-size: var(--fs-sm);">
        <thead>
          <tr style="border-bottom: 1px solid var(--card-border); color: var(--text-muted); text-align: left;">
            <th style="padding: 8px 10px; white-space: nowrap;" data-i18n="key_col_name">${t('key_col_name', 'Name')}</th>
            <th style="padding: 8px 10px; min-width: 220px;" data-i18n="key_col_fingerprint">${t('key_col_fingerprint', 'Fingerprint')}</th>
            <th style="padding: 8px 10px; white-space: nowrap; text-align: center;" data-i18n="key_col_used_by">${t('key_col_used_by', 'Used By')}</th>
            <th style="padding: 8px 10px; white-space: nowrap;" data-i18n="key_col_created">${t('key_col_created', 'Created')}</th>
            <th style="padding: 8px 10px; white-space: nowrap; text-align: right;" data-i18n="actions">${t('actions', 'Action')}</th>
          </tr>
        </thead>
        <tbody>
          ${keys.map(k => {
            const count = k.used_by || 0;
            const countLabel = count === 1 ? t('host_singular', 'host') : t('host_plural', 'hosts');
            const createdStr = k.created_at ? new Date(k.created_at).toLocaleDateString() : '—';
            return `
              <tr style="border-bottom: 1px solid var(--card-border);">
                <td style="padding: 8px 10px; font-weight: 600; white-space: nowrap;">
                  <div style="display: flex; align-items: center; gap: 6px; color: var(--text);">
                    ${svgIcon('key-round', 'icon-sm text-accent')}
                    <span>${escapeHtml(k.name)}</span>
                  </div>
                </td>
                <td style="padding: 8px 10px;">
                  <span class="fingerprint-chip">${escapeHtml(k.fingerprint || '—')}</span>
                </td>
                <td style="padding: 8px 10px; text-align: center; white-space: nowrap;">
                  <span class="badge ${count > 0 ? 'badge-info' : 'badge-unknown'}">
                    ${count} ${countLabel}
                  </span>
                </td>
                <td style="padding: 8px 10px; color: var(--text-muted); font-size: var(--fs-xs); white-space: nowrap;">
                  <span class="num">${createdStr}</span>
                </td>
                <td style="padding: 8px 10px; text-align: right; white-space: nowrap;">
                  <button class="btn btn-danger btn-sm" onclick="deleteSSHKey(${k.id})" style="padding: 2px 8px; font-size: var(--fs-xs);">
                    ${svgIcon('trash-2', 'icon-sm')} <span data-i18n="delete">${t('delete', 'Delete')}</span>
                  </button>
                </td>
              </tr>
            `;
          }).join('')}
        </tbody>
      </table>
    </div>
  `;
}

async function addSSHKey() {
  const nameInput = document.getElementById('newKeyName');
  const privInput = document.getElementById('newKeyPrivate');
  const name = (nameInput?.value || '').trim();
  const privateKey = (privInput?.value || '').trim();

  if (!name) {
    showToast(t('key_name_required', 'Key name is required'), 'warning');
    nameInput?.focus();
    return;
  }
  if (!privateKey) {
    showToast(t('key_private_required', 'Private key content is required'), 'warning');
    privInput?.focus();
    return;
  }

  try {
    const res = await fetch('/api/ssh-keys', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, private_key: privateKey })
    });
    if (res.status === 201 || res.ok) {
      showToast(t('key_added_success', 'SSH key added to library successfully'), 'success');
      if (nameInput) nameInput.value = '';
      if (privInput) privInput.value = '';
      loadSSHKeys();
    } else {
      const txt = await res.text();
      let errMsg = txt;
      try {
        const errObj = JSON.parse(txt);
        if (errObj.error) errMsg = errObj.error;
      } catch(e) {}
      showToast(errMsg, 'error');
    }
  } catch (err) {
    showToast(t('key_add_failed', 'Failed to add SSH key: ') + err.message, 'error');
  }
}

async function deleteSSHKey(id) {
  const k = S.cachedSSHKeys.find(item => item.id === id);
  const name = k ? k.name : ('#' + id);
  const msg = t('confirm_delete_ssh_key', 'Are you sure you want to delete SSH key "{name}"?').replace('{name}', name);
  if (!await showConfirm({ message: msg, confirmText: t('delete', 'Delete'), danger: true })) return;
  try {
    const res = await fetch('/api/ssh-keys/' + id, { method: 'DELETE' });
    if (res.ok) {
      showToast(t('ssh_key_deleted', 'SSH key deleted successfully'), 'success');
      loadSSHKeys();
    } else {
      const txt = await res.text();
      let errMsg = txt;
      try {
        const errObj = JSON.parse(txt);
        if (errObj.error) errMsg = errObj.error;
      } catch(e) {}
      showToast(errMsg, 'error');
    }
  } catch (err) {
    showToast(t('delete_ssh_key_failed', 'Failed to delete SSH key: ') + err.message, 'error');
  }
}

function handleSSHKeyFileSelect(event) {
  const file = event.target.files && event.target.files[0];
  if (!file) return;
  const reader = new FileReader();
  reader.onload = function(e) {
    const content = e.target.result;
    const txtArea = document.getElementById('newKeyPrivate');
    if (txtArea) txtArea.value = content;
    const nameInput = document.getElementById('newKeyName');
    if (nameInput && !nameInput.value.trim()) {
      let baseName = file.name.replace(/\.(pem|key|priv|id_rsa|id_ed25519)$/i, '');
      if (baseName === 'id_rsa' || baseName === 'id_ed25519' || !baseName) {
        baseName = file.name;
      }
      nameInput.value = baseName;
    }
  };
  reader.readAsText(file);
  event.target.value = '';
}

async function checkForUpdates(force = false) {
  const btn = document.getElementById('btnCheckUpdatesManual');
  const originalText = btn ? btn.innerText : '';
  if (btn && force) {
    btn.disabled = true;
    btn.innerText = t('checking_updates') || 'Checking...';
  }

  try {
    const res = await fetch('/api/update/check' + (force ? '?force=true' : ''));
    if (!res.ok) return;
    const data = await res.json();
    S.lastUpdateCheckData = data;

    // Update footer badge
    const footerBadge = document.getElementById('footerUpdateBadge');
    if (footerBadge) {
      if (data.available && data.latest_version) {
        footerBadge.style.display = 'inline-block';
        footerBadge.innerHTML = svgIcon('rocket', 'icon-sm') + ' ' + escapeHtml(String(data.latest_version));
        footerBadge.title = (t('update_available') || 'Update Available') + ' (' + data.latest_version + ')';
      } else {
        footerBadge.style.display = 'none';
      }
    }

    // Update settings modal elements if present
    const curVerEl = document.getElementById('updateCurrentVersion');
    const latVerEl = document.getElementById('updateLatestVersion');
    const noticeEl = document.getElementById('updateStatusNotice');
    const dockerCard = document.getElementById('updateDockerCard');
    const actionCard = document.getElementById('updateActionCard');
    const notesBox = document.getElementById('updateReleaseNotesBox');
    const linkEl = document.getElementById('updateReleaseLink');

    if (curVerEl) curVerEl.innerText = data.current_version || 'dev';
    if (latVerEl) latVerEl.innerText = data.latest_version || '-';

    if (dockerCard) {
      dockerCard.style.display = data.is_docker ? 'block' : 'none';
    }

    if (actionCard) {
      if (data.available && !data.is_docker) {
        actionCard.style.display = 'block';
        if (notesBox) notesBox.innerText = data.release_notes || 'Official release available.';
        if (linkEl && data.release_url) linkEl.href = data.release_url;
      } else {
        actionCard.style.display = 'none';
      }
    }

    if (noticeEl) {
      noticeEl.style.display = 'block';
      if (data.error) {
        noticeEl.style.background = 'var(--danger-soft)';
        noticeEl.style.color = 'var(--danger)';
        noticeEl.innerHTML = svgIcon('triangle-alert', 'icon-sm') + ' ' + escapeHtml(String(data.error));
      } else if (data.available) {
        noticeEl.style.background = 'var(--accent-soft)';
        noticeEl.style.color = 'var(--accent-text)';
        noticeEl.innerHTML = svgIcon('rocket', 'icon-sm') + ' ' + escapeHtml((t('update_available') || 'Update Available') + ': ' + data.latest_version);
      } else {
        noticeEl.style.background = 'var(--success-soft)';
        noticeEl.style.color = 'var(--success)';
        noticeEl.innerHTML = svgIcon('circle-check', 'icon-sm') + ' ' + escapeHtml(t('up_to_date') || 'Pantau is up to date.');
      }
    }
  } catch (err) {
    console.error('Update check failed:', err);
  } finally {
    if (btn && force) {
      btn.disabled = false;
      btn.innerText = originalText;
    }
  }
}

async function applySelfUpdate() {
  const confirmed = await showConfirm({
    title: t('update_now', 'Update Now'),
    message: 'Download and install update now? Pantau will verify the SHA-256 manifest and run a pre-flight smoke test before replacing the binary.',
    confirmText: t('update_now', 'Update Now'),
    danger: false
  });
  if (!confirmed) return;

  const btn = document.getElementById('btnApplyUpdate');
  const restore = SP.busy(btn, t('updating') || 'Updating...');

  showToast('Downloading verified update from GitHub Releases...', 'info');

  try {
    const res = await fetch('/api/update/apply', { method: 'POST' });
    const data = await res.json();
    if (!res.ok || data.error) {
      showToast(data.error || 'Failed to apply update', 'error');
      return;
    }

    showToast(t('update_success_restart') || 'Update installed successfully!', 'success');
    const actionCard = document.getElementById('updateActionCard');
    if (actionCard) actionCard.style.display = 'none';
    const restartCard = document.getElementById('updateRestartCard');
    if (restartCard) restartCard.style.display = 'block';
  } catch (err) {
    showToast('Update error: ' + err.message, 'error');
  } finally {
    restore();
  }
}

async function restartPantau() {
  const btn = document.getElementById('btnRestartNow');
  const restore = SP.busy(btn, t('restarting') || 'Restarting...');

  showToast('Restarting Pantau process...', 'info');

  try {
    await fetch('/api/update/restart', { method: 'POST' });
  } catch (_) {
    // Process might exit before response completes, which is expected
  }

  // Poll for reconnection (capped so a server that never returns does not leave the button stuck).
  showToast('Waiting for server to restart...', 'warning');
  const MAX_ATTEMPTS = 90;
  let attempts = 0;
  const pollInterval = setInterval(async () => {
    attempts++;
    try {
      const res = await fetch('/api/auth/status');
      if (res.ok) {
        clearInterval(pollInterval);
        showToast('Server back online! Reloading...', 'success');
        setTimeout(() => {
          window.location.reload();
        }, 800);
        return;
      }
    } catch (_) {
      // server still rebooting
    }
    if (attempts >= MAX_ATTEMPTS) {
      clearInterval(pollInterval);
      restore();
      showToast(t('restart_timeout', 'Server did not come back online. Restart it manually and try again.'), 'error');
    }
  }, 1000);
}

async function openSettingsModal(tab) {
  if (tab) S.currentSettingsTab = tab;
  const res = await fetch('/api/settings');
  const data = await res.json();
  document.getElementById('settingPubKey').value = data.ssh_public_key || '';
  document.getElementById('settingTgToken').value = data.telegram_token || '';
  document.getElementById('settingTgChat').value = data.telegram_chat_id || '';
  document.getElementById('settingWebhook').value = data.webhook_url || '';
  document.getElementById('settingNtfyUrl').value = data.ntfy_url || '';
  document.getElementById('settingNtfyToken').value = '';
  const ntfySaved = document.getElementById('settingNtfyTokenSaved');
  if (ntfySaved) ntfySaved.style.display = data.ntfy_token_set ? 'inline-flex' : 'none';
  document.getElementById('settingSlackUrl').value = data.slack_webhook_url || '';
  document.getElementById('settingDiscordUrl').value = data.discord_webhook_url || '';
  document.getElementById('settingSmtpHost').value = data.smtp_host || '';
  document.getElementById('settingSmtpPort').value = data.smtp_port || '';
  document.getElementById('settingSmtpUser').value = data.smtp_user || '';
  document.getElementById('settingSmtpPass').value = '';
  const smtpSaved = document.getElementById('settingSmtpPassSaved');
  if (smtpSaved) smtpSaved.style.display = data.smtp_pass_set ? 'inline-flex' : 'none';
  document.getElementById('settingSmtpFrom').value = data.smtp_from || '';
  document.getElementById('settingSmtpSecurity').value = data.smtp_security || 'starttls';
  document.getElementById('settingSmtpTo').value = data.smtp_to || '';
  document.getElementById('settingAlertDebounce').value = data.alert_debounce_runs || '1';
  const testRes = document.getElementById('testNotifResults');
  if (testRes) { testRes.style.display = 'none'; testRes.innerHTML = ''; }
  const pollEl = document.getElementById('settingPollInterval');
  if (pollEl) pollEl.value = (data.poll_interval_sec !== undefined && data.poll_interval_sec !== '') ? data.poll_interval_sec : '300';
  const defaultHostsViewEl = document.getElementById('settingDefaultHostsView');
  if (defaultHostsViewEl && typeof getDefaultHostsView === 'function') {
    defaultHostsViewEl.value = getDefaultHostsView();
  }
  const secScanEl = document.getElementById('settingSecurityScanInterval');
  if (secScanEl) secScanEl.value = (data.security_scan_interval_hours !== undefined && data.security_scan_interval_hours !== '') ? data.security_scan_interval_hours : '24';
  const vulnIntelEl = document.getElementById('settingVulnIntel');
  if (vulnIntelEl) vulnIntelEl.checked = data.vuln_intel_enabled === '1';
  const osvEnabledEl = document.getElementById('settingOsvEnabled');
  if (osvEnabledEl) osvEnabledEl.checked = data.osv_enabled === '1';
  const iacGateEl = document.getElementById('settingIacGate');
  if (iacGateEl) iacGateEl.checked = data.iac_gate_enabled === '1';

  document.getElementById('settingGhEnabled').checked = data.github_backup_enabled === '1';
  document.getElementById('settingGhRepo').value = data.github_repo || '';
  document.getElementById('settingGhToken').value = data.github_token || '';
  document.getElementById('settingGhPassphrase').value = data.snapshot_passphrase || '';
  document.getElementById('settingGhInterval').value = data.backup_interval_hours || '24';
  document.getElementById('settingGhPath').value = data.github_file_path || 'pantau-state.enc';
  renderS3Accounts();

  let statusText = t('not_configured');
  if (data.github_repo) {
    statusText = (data.github_backup_enabled === '1' ? t('active_status') : t('inactive_status')) + ' | Repo: ' + data.github_repo;
    if (data.last_backup_time) {
      statusText += ' | ' + t('last_backup_time') + data.last_backup_time + ' (' + (data.last_backup_status || 'ok') + ')';
    }
  }
  document.getElementById('backupStatusText').innerText = statusText;

  openModal('settingsModal');
  switchSettingsTab(S.currentSettingsTab || 'general');
  load2FASettings();
  setTimeout(() => {
    const pubKeyEl = document.getElementById('settingPubKey');
    if (pubKeyEl) {
      pubKeyEl.style.height = 'auto';
      pubKeyEl.style.height = (pubKeyEl.scrollHeight + 4) + 'px';
    }
  }, 50);
}

async function load2FASettings() {
  try {
    const res = await fetch('/api/2fa/status');
    if (!res.ok) return;
    const data = await res.json();
    const badge = document.getElementById('setting2FAStatusBadge');
    const disabledView = document.getElementById('setting2FADisabledView');
    const enabledView = document.getElementById('setting2FAEnabledView');
    const setupView = document.getElementById('setting2FASetupView');
    const remainingCount = document.getElementById('setting2FARemainingCount');

    if (setupView) setupView.style.display = 'none';
    if (data.enabled) {
      if (badge) {
        badge.innerText = t('two_factor_enabled', 'Active');
        badge.style.background = 'color-mix(in srgb, var(--success) 20%, transparent)';
        badge.style.color = 'var(--success)';
      }
      if (disabledView) disabledView.style.display = 'none';
      if (enabledView) enabledView.style.display = 'flex';
      if (remainingCount) remainingCount.innerText = data.recovery_codes_count ?? 0;
    } else {
      if (badge) {
        badge.innerText = t('two_factor_disabled', 'Disabled');
        badge.style.background = 'color-mix(in srgb, var(--danger) 20%, transparent)';
        badge.style.color = 'var(--danger)';
      }
      if (disabledView) disabledView.style.display = 'block';
      if (enabledView) enabledView.style.display = 'none';
    }
  } catch (e) {
    console.error('Failed to load 2FA status:', e);
  }
}

async function start2FASetup() {
  const errEl = document.getElementById('setting2FASetupErr');
  if (errEl) errEl.innerText = '';
  try {
    const res = await fetch('/api/2fa/setup', { method: 'POST' });
    if (!res.ok) throw new Error('Failed to start setup');
    const data = await res.json();
    window.pending2FASetupData = data;

    document.getElementById('setting2FAQRImg').src = '/api/2fa/qr?t=' + Date.now();
    document.getElementById('setting2FASecretText').innerText = data.secret;

    const listEl = document.getElementById('setting2FARecoveryList');
    if (listEl) {
      listEl.innerHTML = '';
      (data.recovery_codes || []).forEach(code => {
        const item = document.createElement('div');
        item.style.padding = '4px 6px';
        item.style.background = 'var(--card-bg)';
        item.style.borderRadius = '4px';
        item.style.textAlign = 'center';
        item.style.border = '1px solid var(--card-border)';
        item.innerText = code;
        listEl.appendChild(item);
      });
    }

    document.getElementById('setting2FADisabledView').style.display = 'none';
    document.getElementById('setting2FASetupView').style.display = 'flex';
    document.getElementById('setting2FAConfirmCode').value = '';
    setTimeout(() => document.getElementById('setting2FAConfirmCode')?.focus(), 50);
  } catch (e) {
    showToast(e.message, 'error');
  }
}

function cancel2FASetup() {
  window.pending2FASetupData = null;
  const setupView = document.getElementById('setting2FASetupView');
  const disabledView = document.getElementById('setting2FADisabledView');
  if (setupView) setupView.style.display = 'none';
  if (disabledView) disabledView.style.display = 'block';
}

async function confirmEnable2FA() {
  const code = document.getElementById('setting2FAConfirmCode').value.trim();
  const errEl = document.getElementById('setting2FASetupErr');
  if (errEl) errEl.innerText = '';
  if (!code) return;

  try {
    const res = await fetch('/api/2fa/enable', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ code: code })
    });
    const data = await res.json();
    if (res.ok && data.status === 'enabled') {
      showToast(t('2fa_enabled_success', 'Two-Factor Authentication successfully activated!'), 'success');
      load2FASettings();
    } else {
      if (errEl) errEl.innerText = t('invalid_2fa_code', 'Invalid confirmation code');
    }
  } catch (e) {
    if (errEl) errEl.innerText = e.message;
  }
}

async function disable2FA() {
  const password = document.getElementById('disable2FAPassword').value;
  const code = document.getElementById('disable2FACode').value.trim();
  const errEl = document.getElementById('setting2FADisableErr');
  if (errEl) errEl.innerText = '';
  if (!password || !code) {
    if (errEl) errEl.innerText = 'Password and code are required';
    return;
  }

  try {
    const res = await fetch('/api/2fa/disable', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ password: password, code: code })
    });
    const data = await res.json();
    if (res.ok && data.status === 'disabled') {
      document.getElementById('disable2FAPassword').value = '';
      document.getElementById('disable2FACode').value = '';
      showToast(t('2fa_disabled_success', 'Two-Factor Authentication deactivated.'), 'success');
      load2FASettings();
    } else {
      if (errEl) errEl.innerText = data.error === 'invalid_password' ? t('invalid_password', 'Invalid password') : t('invalid_2fa_code', 'Invalid code');
    }
  } catch (e) {
    if (errEl) errEl.innerText = e.message;
  }
}

function copy2FASecret() {
  const secret = document.getElementById('setting2FASecretText').innerText;
  if (secret) {
    navigator.clipboard.writeText(secret).then(() => {
      showToast(t('copied', 'Copied to clipboard'), 'success');
    });
  }
}

function copy2FARecoveryCodes() {
  if (!window.pending2FASetupData || !window.pending2FASetupData.recovery_codes) return;
  const text = window.pending2FASetupData.recovery_codes.join('\n');
  navigator.clipboard.writeText(text).then(() => {
    showToast(t('copied', 'Copied to clipboard'), 'success');
  });
}

function download2FARecoveryCodes() {
  if (!window.pending2FASetupData || !window.pending2FASetupData.recovery_codes) return;
  const text = "Pantau 2FA Emergency Recovery Codes\nGenerated at: " + new Date().toISOString() + "\n\n" + window.pending2FASetupData.recovery_codes.join('\n') + "\n\nEach code can only be used once.\n";
  const blob = new Blob([text], { type: 'text/plain;charset=utf-8' });
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = 'pantau-2fa-recovery-codes.txt';
  a.click();
  URL.revokeObjectURL(a.href);
}

async function saveSettings() {
  const payload = {
    telegram_token: document.getElementById('settingTgToken').value,
    telegram_chat_id: document.getElementById('settingTgChat').value,
    webhook_url: document.getElementById('settingWebhook').value,
    ntfy_url: document.getElementById('settingNtfyUrl').value.trim(),
    slack_webhook_url: document.getElementById('settingSlackUrl').value.trim(),
    discord_webhook_url: document.getElementById('settingDiscordUrl').value.trim(),
    smtp_host: document.getElementById('settingSmtpHost').value.trim(),
    smtp_port: document.getElementById('settingSmtpPort').value.trim(),
    smtp_user: document.getElementById('settingSmtpUser').value.trim(),
    smtp_from: document.getElementById('settingSmtpFrom').value.trim(),
    smtp_security: document.getElementById('settingSmtpSecurity').value,
    smtp_to: document.getElementById('settingSmtpTo').value.trim(),
    alert_debounce_runs: document.getElementById('settingAlertDebounce').value.trim() || '1',
  };
  const ntfyTokenVal = document.getElementById('settingNtfyToken').value;
  if (ntfyTokenVal !== '') payload.ntfy_token = ntfyTokenVal;
  const smtpPassVal = document.getElementById('settingSmtpPass').value;
  if (smtpPassVal !== '') payload.smtp_pass = smtpPassVal;

  const res = await fetch('/api/settings', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  });
  if (res.ok) {
    showToast(t('settings_saved'), 'success');
    openSettingsModal('notif');
  } else {
    const err = await res.json().catch(() => ({}));
    showToast(err.error || 'Failed to save settings', 'error');
  }
}

async function testNotification() {
  const btn = document.getElementById('btnTestNotif');
  const resContainer = document.getElementById('testNotifResults');
  if (btn) btn.disabled = true;
  if (resContainer) {
    resContainer.style.display = 'block';
    resContainer.innerHTML = `<span class="spinner" style="display:inline-block; margin-right:6px;">${svgIcon('loader-circle', 'icon-sm')}</span> <span>Testing channels...</span>`;
  }
  try {
    const res = await fetch('/api/settings/test-notify', { method: 'POST' });
    const data = await res.json().catch(() => ({}));
    if (data.ok) {
      showToast(t('test_notif_success'), 'success');
    } else {
      showToast(t('test_notif_failed') + (data.error || 'Some or all channels failed'), 'error');
    }
    if (resContainer) {
      const results = Array.isArray(data.results) ? data.results : [];
      if (results.length === 0) {
        resContainer.innerHTML = `<div style="color: var(--danger);">${escapeHtml(data.error || 'No notification channels configured.')}</div>`;
      } else {
        resContainer.innerHTML = `
          <div style="display: flex; flex-direction: column; gap: 4px; padding: 8px 10px; background: var(--surface-2); border: 1px solid var(--card-border); border-radius: var(--radius);">
            ${results.map(r => `
              <div style="display: flex; align-items: center; justify-content: space-between; gap: 8px;">
                <span style="font-weight: 600; text-transform: uppercase;">${escapeHtml(r.channel)}</span>
                <span class="badge ${r.ok ? 'badge-healthy' : 'badge-danger'}">
                  ${r.ok ? 'OK' : escapeHtml(r.error || 'Failed')}
                </span>
              </div>
            `).join('')}
          </div>`;
      }
    }
  } catch (e) {
    if (resContainer) resContainer.innerHTML = `<div style="color: var(--danger);">${escapeHtml(e.message)}</div>`;
  } finally {
    if (btn) btn.disabled = false;
  }
}

async function changePassword() {
  const old_password = document.getElementById('oldPass').value;
  const new_password = document.getElementById('newPass').value;
  const res = await fetch('/api/password', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ old_password, new_password })
  });
  if (res.ok) {
    showToast(t('pass_updated'), 'success');
  } else {
    showToast(t('pass_update_failed'), 'error');
  }
}

async function saveGitHubBackupSettings() {
  const payload = {
    github_backup_enabled: document.getElementById('settingGhEnabled').checked ? '1' : '0',
    github_repo: document.getElementById('settingGhRepo').value.trim(),
    github_token: document.getElementById('settingGhToken').value.trim(),
    snapshot_passphrase: document.getElementById('settingGhPassphrase').value.trim(),
    backup_interval_hours: document.getElementById('settingGhInterval').value.trim() || '24',
    github_file_path: document.getElementById('settingGhPath').value.trim() || 'pantau-state.enc',
  };
  const btn = document.getElementById('btnSaveGhConfig');
  const restore = SP.busy(btn, t('saving', 'Saving...'));
  try {
    const res = await fetch('/api/settings', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    if (!res.ok) {
      let msg = '';
      try { msg = (await res.json()).error || ''; } catch (_) { msg = ''; }
      showToast(t('gh_config_save_failed') + (msg || (res.status + ' ' + res.statusText)), 'error');
      return;
    }
    showToast(t('gh_config_saved'), 'success');
    openSettingsModal();
  } catch (e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error');
  } finally {
    restore();
  }
}

async function syncGitHubNow() {
  const btn = document.getElementById('btnSyncGhNow');
  const restore = SP.busy(btn, t('syncing_gh', 'Syncing to GitHub...'));
  try {
    const res = await fetch('/api/snapshot/github/sync', { method: 'POST' });
    const data = await res.json().catch(() => ({}));
    if (!res.ok || !data.ok) {
      showToast(t('sync_gh_failed') + (data.error || 'Unknown error'), 'error');
      return;
    }
    showToast(data.message || t('gh_config_saved'), 'success');
    openSettingsModal();
  } catch (e) {
    showToast(t('sync_gh_failed') + e.message, 'error');
  } finally {
    restore();
  }
}

function exportSnapshotPrompt() {
  const input = document.getElementById('snapshotExportPassword');
  if (input) input.value = '';
  openModal('snapshotExportModal');
  setTimeout(() => input?.focus(), 50);
}

async function submitSnapshotExport() {
  const pass = document.getElementById('snapshotExportPassword').value;
  if (!pass) {
    showToast(t('passphrase_required', 'Passphrase is required!'), 'error');
    return;
  }
  const btn = document.getElementById('btnSubmitSnapshotExport');
  const restore = SP.busy(btn, t('exporting_snapshot', 'Exporting...'));
  try {
    const res = await fetch('/api/snapshot/export', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ passphrase: pass })
    });
    if (!res.ok) {
      const err = await res.text();
      showToast(t('export_snapshot_failed') + err, 'error');
      return;
    }
    const blob = await res.blob();
    const url = window.URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'pantau-state.enc';
    document.body.appendChild(a);
    a.click();
    a.remove();
    window.URL.revokeObjectURL(url);
    closeModal('snapshotExportModal');
    showToast(t('snapshot_exported', 'Snapshot exported successfully'), 'success');
  } catch (e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error');
  } finally {
    restore();
  }
}

function openRestoreModal() {
  document.getElementById('restoreGhMsg').innerText = '';
  document.getElementById('restoreFileMsg').innerText = '';
  openModal('restoreModal');
}

function switchRestoreTab(tab) {
  document.getElementById('tabRestoreGh').classList.remove('active');
  document.getElementById('tabRestoreFile').classList.remove('active');
  document.getElementById('tabRestoreFresh').classList.remove('active');
  document.getElementById('restoreViewGithub').style.display = 'none';
  document.getElementById('restoreViewFile').style.display = 'none';
  document.getElementById('restoreViewFresh').style.display = 'none';

  if (tab === 'github') {
    document.getElementById('tabRestoreGh').classList.add('active');
    document.getElementById('restoreViewGithub').style.display = 'flex';
  } else if (tab === 'file') {
    document.getElementById('tabRestoreFile').classList.add('active');
    document.getElementById('restoreViewFile').style.display = 'flex';
  } else if (tab === 'fresh') {
    document.getElementById('tabRestoreFresh').classList.add('active');
    document.getElementById('restoreViewFresh').style.display = 'flex';
  }
}

async function doRestoreFromGitHub() {
  const repo = document.getElementById('restoreGhRepo').value.trim();
  const token = document.getElementById('restoreGhToken').value.trim();
  const path = document.getElementById('restoreGhPath').value.trim();
  const passphrase = document.getElementById('restoreGhPass').value;
  const msgEl = document.getElementById('restoreGhMsg');

  if (!repo || !token || !passphrase) {
    msgEl.innerHTML = '<span style="color: var(--danger);">' + t('all_fields_required') + '</span>';
    return;
  }

  const btn = document.getElementById('btnRestoreFromGh');
  const restore = SP.busy(btn, t('downloading_decrypting'));
  const t0 = Date.now();
  msgEl.innerHTML = SP.html(t('downloading_decrypting'), t0);
  try {
    const res = await fetch('/api/snapshot/github/restore', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ repo, token, path, passphrase })
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok || !data.ok) {
      msgEl.innerHTML = '<span style="color: var(--danger);">' + t('failed_prefix') + escapeHtml(data.error || (res.status + ' ' + res.statusText)) + '</span>';
      return;
    }
    msgEl.innerHTML = '<span style="color: var(--success);">' + t('restored_refreshing') + '</span>';
    setTimeout(() => location.reload(), 1200);
  } catch (e) {
    msgEl.innerHTML = '<span style="color: var(--danger);">' + t('error_prefix') + escapeHtml(e.message) + '</span>';
  } finally {
    restore();
  }
}

async function doRestoreFromFile() {
  const fileInput = document.getElementById('restoreFileInput');
  const pass = document.getElementById('restoreFilePass').value;
  const msgEl = document.getElementById('restoreFileMsg');

  if (!fileInput.files || fileInput.files.length === 0) {
    msgEl.innerHTML = '<span style="color: var(--danger);">' + t('select_snapshot_first') + '</span>';
    return;
  }
  if (!pass) {
    msgEl.innerHTML = '<span style="color: var(--danger);">' + t('passphrase_required') + '</span>';
    return;
  }

  const formData = new FormData();
  formData.append('file', fileInput.files[0]);
  formData.append('passphrase', pass);

  const btn = document.getElementById('btnRestoreFromFile');
  const restore = SP.busy(btn, t('decrypting_restoring'));
  const t0 = Date.now();
  msgEl.innerHTML = SP.html(t('decrypting_restoring'), t0);
  try {
    const res = await fetch('/api/snapshot/import', {
      method: 'POST',
      body: formData
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok || !data.ok) {
      msgEl.innerHTML = '<span style="color: var(--danger);">' + t('failed_prefix') + escapeHtml(data.error || (res.status + ' ' + res.statusText)) + '</span>';
      return;
    }
    msgEl.innerHTML = '<span style="color: var(--success);">' + t('restored_hosts_refreshing').replace('{count}', data.hosts_imported) + '</span>';
    setTimeout(() => location.reload(), 1200);
  } catch (e) {
    msgEl.innerHTML = '<span style="color: var(--danger);">' + t('error_prefix') + escapeHtml(e.message) + '</span>';
  } finally {
    restore();
  }
}

async function doStartFresh() {
  await fetch('/api/snapshot/bootstrap/start-fresh', { method: 'POST' });
  closeModal('restoreModal');
}

async function checkFreshStatus() {
  try {
    const res = await fetch('/api/snapshot/status');
    const data = await res.json();
    if (data.fresh) {
      openRestoreModal();
    }
  } catch (e) {
    console.error('checkFreshStatus error:', e);
  }
}

function formatBytes(bytes) {
  if (!bytes || bytes <= 0 || isNaN(bytes)) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(sizes.length - 1, Math.floor(Math.log(bytes) / Math.log(k)));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

async function openTransferDialog(srcPath, isDir) {
  if (!S.currentHost) return;
  document.getElementById('transferSourceHost').innerText = 'From: ' + S.currentHost.name + ' (' + S.currentHost.host + ')';
  document.getElementById('transferSourcePath').value = srcPath;
  
  const res = await fetch('/api/hosts');
  if (!res.ok) return;
  const hosts = await res.json();
  const select = document.getElementById('transferDestHost');
  select.innerHTML = '';
  const availableHosts = hosts.filter(h => h.id !== S.currentHost.id);
  if (availableHosts.length === 0) {
    showToast(t('no_other_dest_host'), 'error');
    return;
  }
  availableHosts.forEach(h => {
    const opt = document.createElement('option');
    opt.value = h.id;
    opt.innerText = h.name + ' (' + h.host + ')';
    select.appendChild(opt);
  });
  
  document.getElementById('transferDestPath').value = '/tmp';
  openModal('transferModal');
}

async function submitTransfer() {
  const destHostID = parseInt(document.getElementById('transferDestHost').value, 10);
  const srcPath = document.getElementById('transferSourcePath').value;
  const destPath = document.getElementById('transferDestPath').value.trim();
  const mode = document.getElementById('transferMode').value;
  
  if (!destHostID || !srcPath || !destPath) {
    showToast(t('fill_all_fields'), 'error');
    return;
  }
  
  const btn = document.getElementById('btnStartTransfer');
  btn.disabled = true;
  btn.innerText = t('starting');
  
  try {
    const res = await fetch('/api/transfers', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        source_host_id: S.currentHost.id,
        source_path: srcPath,
        dest_host_id: destHostID,
        dest_path: destPath,
        mode: mode
      })
    });
    const data = await res.json();
    if (!res.ok) {
      showToast(t('start_transfer_failed') + (data.error || res.statusText), 'error');
      return;
    }
    closeModal('transferModal');
    openTransfersModal();
  } catch (err) {
    showToast(t('error_prefix', 'Error: ') + err.message, 'error');
  } finally {
    btn.disabled = false;
    btn.innerText = t('start_transfer');
  }
}

async function loadS3Accounts() {
  const res = await fetch('/api/s3/accounts');
  S.s3Accounts = res.ok ? ((await res.json()).accounts || []) : [];
  return S.s3Accounts;
}

async function renderS3Accounts() {
  const box = document.getElementById('s3AccountsList');
  if (!box) return;
  await loadS3Accounts();
  if (S.s3Accounts.length === 0) {
    box.innerHTML = `<div style="color: var(--text-muted); font-size: var(--fs-sm); padding: 12px;">${escapeHtml(t('s3_no_accounts'))}</div>`;
    return;
  }
  box.innerHTML = S.s3Accounts.map(a => `
    <div style="display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 10px 12px; border-bottom: 1px solid var(--card-border);">
      <div style="min-width: 0;">
        <div style="font-weight: 600;">${escapeHtml(a.name)}</div>
        <div class="mono" style="font-size: var(--fs-xs); color: var(--text-muted); overflow: hidden; text-overflow: ellipsis;">s3://${escapeHtml(a.bucket)}/${escapeHtml(a.prefix)} &middot; ${escapeHtml(a.endpoint || 'AWS')}</div>
        <div id="s3AccTest_${a.id}" style="font-size: var(--fs-xs); display: none;"></div>
      </div>
      <div style="display: flex; gap: 6px; flex-shrink: 0;">
        <button class="btn btn-sm btn-ghost" onclick="testS3Account(${a.id})">${escapeHtml(t('s3_test'))}</button>
        <button class="btn btn-sm btn-icon" title="${escapeHtml(t('edit'))}" onclick="editS3Account(${a.id})">${svgIcon('pencil', 'icon-sm')}</button>
        <button class="btn btn-sm btn-icon btn-danger" onclick="deleteS3Account(${a.id})">${svgIcon('trash-2', 'icon-sm')}</button>
      </div>
    </div>`).join('');
}

function editS3Account(id) {
  const a = S.s3Accounts.find(x => x.id === id) || { id: 0, name: '', endpoint: '', region: '', bucket: '', access_key: '', prefix: '', path_style: '', secret_key_set: false };
  S.s3EditingId = a.id;
  document.getElementById('s3FormTitle').innerText = t(a.id ? 's3_edit_account' : 's3_add_account');
  document.getElementById('settingS3Name').value = a.name;
  document.getElementById('settingS3Endpoint').value = a.endpoint;
  document.getElementById('settingS3Bucket').value = a.bucket;
  document.getElementById('settingS3Region').value = a.region;
  document.getElementById('settingS3AccessKey').value = a.access_key;
  document.getElementById('settingS3SecretKey').value = '';
  document.getElementById('settingS3SecretKeySaved').style.display = a.secret_key_set ? 'inline-flex' : 'none';
  document.getElementById('settingS3Prefix').value = a.prefix;
  document.getElementById('settingS3PathStyle').value = a.path_style;
  document.getElementById('s3AccountForm').style.display = 'flex';
}

function cancelS3Form() {
  document.getElementById('s3AccountForm').style.display = 'none';
}

async function saveS3Account() {
  const payload = {
    name: document.getElementById('settingS3Name').value.trim(),
    endpoint: document.getElementById('settingS3Endpoint').value.trim(),
    bucket: document.getElementById('settingS3Bucket').value.trim(),
    region: document.getElementById('settingS3Region').value.trim(),
    access_key: document.getElementById('settingS3AccessKey').value.trim(),
    secret_key: document.getElementById('settingS3SecretKey').value.trim(),
    prefix: document.getElementById('settingS3Prefix').value.trim(),
    path_style: document.getElementById('settingS3PathStyle').value
  };
  const btn = document.getElementById('btnSaveS3Account');
  const restore = SP.busy(btn, t('upd_saving'));
  try {
    const res = await fetch(S.s3EditingId ? '/api/s3/accounts/' + S.s3EditingId : '/api/s3/accounts', {
      method: S.s3EditingId ? 'PUT' : 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({}));
      showToast(err.error || 'Failed to save S3 account', 'error');
      return;
    }
    showToast(t('settings_saved'), 'success');
    cancelS3Form();
    renderS3Accounts();
  } catch (e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error');
  } finally {
    restore();
  }
}

async function deleteS3Account(id) {
  const a = S.s3Accounts.find(x => x.id === id);
  const ok = await showConfirm({
    title: t('s3_delete_title'),
    message: t('s3_delete_msg').replace('{name}', a ? a.name : '#' + id),
    confirmText: t('delete', 'Delete'),
    danger: true
  });
  if (!ok) return;
  const res = await fetch('/api/s3/accounts/' + id, { method: 'DELETE' });
  if (res.ok) renderS3Accounts();
  else showToast('Failed to delete S3 account', 'error');
}

async function testS3Account(id) {
  const box = document.getElementById('s3AccTest_' + id);
  box.style.display = 'block';
  box.style.color = 'var(--text-muted)';
  box.innerHTML = SP.html(t('s3_testing'));
  try {
    const res = await fetch('/api/s3/accounts/' + id + '/test', { method: 'POST' });
    const data = await res.json().catch(() => ({}));
    box.style.color = data.ok ? 'var(--success)' : 'var(--danger)';
    box.innerText = data.ok ? t('s3_test_ok') : (data.error || res.statusText);
  } catch (e) {
    box.style.color = 'var(--danger)';
    box.innerText = e.message;
  }
}

function updateS3UploadDest() {
  const a = S.s3Accounts.find(x => x.id === parseInt(document.getElementById('s3UploadAccount').value, 10));
  document.getElementById('s3UploadBucket').innerText = a ? 's3://' + a.bucket + '/' : '';
  document.getElementById('s3UploadPrefix').value = a ? a.prefix : '';
}

async function openS3UploadDialog(srcPath, isDir) {
  if (!S.currentHost) return;
  await loadS3Accounts();
  if (S.s3Accounts.length === 0) {
    showToast(t('s3_not_configured'), 'error');
    openSettingsModal('s3');
    return;
  }
  document.getElementById('s3UploadSourceHost').innerText = 'From: ' + S.currentHost.name + ' (' + S.currentHost.host + ')';
  document.getElementById('s3UploadSourcePath').value = srcPath;
  const sel = document.getElementById('s3UploadAccount');
  const last = localStorage.getItem('pantau_s3_account');
  sel.innerHTML = S.s3Accounts.map(a => `<option value="${a.id}" ${String(a.id) === last ? 'selected' : ''}>${escapeHtml(a.name)}</option>`).join('');
  updateS3UploadDest();
  document.getElementById('s3UploadNote').innerText = t(isDir ? 's3_upload_note_dir' : 's3_upload_note_file');
  openModal('s3UploadModal');
}

async function submitS3Upload() {
  const btn = document.getElementById('btnStartS3Upload');
  const accountId = parseInt(document.getElementById('s3UploadAccount').value, 10);
  const restore = SP.busy(btn, t('s3_starting_upload'));
  try {
    const res = await fetch('/api/transfers/s3', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        source_host_id: S.currentHost.id,
        source_path: document.getElementById('s3UploadSourcePath').value,
        account_id: accountId,
        prefix: document.getElementById('s3UploadPrefix').value.trim()
      })
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
      showToast(t('s3_upload_failed') + (data.error || res.statusText), 'error');
      return;
    }
    localStorage.setItem('pantau_s3_account', String(accountId));
    closeModal('s3UploadModal');
    openTransfersModal();
  } catch (err) {
    showToast(t('error_prefix', 'Error: ') + err.message, 'error');
  } finally {
    restore();
  }
}

function openTransfersModal() {
  openModal('transfersListModal');
  loadTransfers();
  if (S.transferPollInterval) clearInterval(S.transferPollInterval);
  S.transferPollInterval = setInterval(loadTransfers, 2000);
}

async function cancelTransfer(jobID) {
  if (!await showConfirm({ message: t('confirm_cancel_transfer'), confirmText: t('cancel_transfer_btn', 'Cancel'), danger: true })) return;
  const res = await fetch('/api/transfers/' + jobID + '/cancel', { method: 'POST' });
  if (res.ok) {
    loadTransfers();
  } else {
    showToast(t('cancel_transfer_failed'), 'error');
  }
}

function switchTransfersTab(tab) {
  S.currentTransfersTab = tab;
  document.querySelectorAll('#transfersTabs .tab').forEach(t => t.classList.remove('active'));
  if (tab === 'active') {
    document.getElementById('tabTransfersActive').classList.add('active');
  } else {
    document.getElementById('tabTransfersHistory').classList.add('active');
  }
  loadTransfers();
}

async function loadTransfers() {
  try {
    const res = await fetch('/api/transfers');
    if (!res.ok) return;
    const jobs = await res.json();
    const container = document.getElementById('transfersListBody');
    const badge = document.getElementById('activeTransferBadge');
    
    const activeJobs = (jobs || []).filter(j => j.status === 'transferring' || j.status === 'queued');
    const historyJobs = (jobs || []).filter(j => j.status !== 'transferring' && j.status !== 'queued');
    if (badge) {
      if (activeJobs.length > 0) {
        badge.innerText = activeJobs.length;
        badge.style.display = 'inline-block';
      } else {
        badge.style.display = 'none';
      }
    }
    
    const displayJobs = S.currentTransfersTab === 'active' ? activeJobs : historyJobs;
    if (displayJobs.length === 0) {
      container.innerHTML = `
        <div class="empty-state" style="padding: 40px 16px;">
          <div class="empty-state-icon">${S.currentTransfersTab === 'active' ? svgIcon('zap', 'icon-xl') : svgIcon('history', 'icon-xl')}</div>
          <div class="empty-state-title">${S.currentTransfersTab === 'active' ? t('no_active_transfers_title') : t('no_history_transfers_title')}</div>
          <div class="empty-state-desc">${S.currentTransfersTab === 'active' ? t('no_active_transfers_desc') : t('no_history_transfers_desc')}</div>
        </div>
      `;
      return;
    }
    
    container.innerHTML = displayJobs.map(j => {
      let statusBadgeCls = 'badge-info';
      let fillCls = 'bg-primary';
      if (j.status === 'completed') { statusBadgeCls = 'badge-success'; fillCls = 'bg-success'; }
      else if (j.status === 'failed') { statusBadgeCls = 'badge-danger'; fillCls = 'bg-danger'; }
      else if (j.status === 'cancelled') { statusBadgeCls = 'badge-muted'; fillCls = 'bg-muted'; }
      
      const speedStr = j.speed_bytes_per_sec > 0 ? formatBytes(j.speed_bytes_per_sec) + '/s' : '';
      const transferredStr = formatBytes(j.transferred_bytes);
      const totalStr = j.total_bytes > 0 ? formatBytes(j.total_bytes) : '';
      const etaStr = j.eta_seconds > 0 ? (j.eta_seconds > 60 ? Math.floor(j.eta_seconds / 60) + 'm ' + (j.eta_seconds % 60) + 's' : j.eta_seconds + 's') : '';
      
      const pct = j.percentage ? Math.min(100, Math.max(0, j.percentage)).toFixed(1) : (j.status === 'completed' ? '100' : '0');
      
      return `
        <div class="transfer-job-card">
          <div class="transfer-job-header">
            <div style="display: flex; align-items: center; gap: 8px;">
              <span style="font-weight: 600; font-size: var(--fs-sm);">${t('job')} #${j.id}</span>
              <span class="badge badge-subtle" style="display: inline-flex; align-items: center; gap: 4px;">${j.kind === 's3' ? svgIcon('cloud', 'icon-xs') + ' <span>S3</span>' : j.kind === 'repo' ? svgIcon('git-branch', 'icon-xs') + ' <span>Repository</span>' : j.mode === 'verified' ? svgIcon('shield-check', 'icon-xs') + ' <span>Verified</span>' : svgIcon('zap', 'icon-xs') + ' <span>Fast Stream</span>'}</span>
              <span class="badge ${statusBadgeCls}">${escapeHtml(j.status)}</span>
            </div>
            <div>
              ${(j.status === 'transferring' || j.status === 'queued') ? '<button class="btn btn-danger btn-sm" onclick="cancelTransfer(' + j.id + ')">' + t('cancel_transfer_btn') + '</button>' : ''}
            </div>
          </div>
          <div class="transfer-job-route">
            <b>${escapeHtml(j.source_host_name || 'Source')}</b>:<code class="mono">${escapeHtml(j.source_path)}</code>
            <svg class="icon icon-xs" style="color:var(--text-muted);"><use href="#i-arrow-left-right"/></svg>
            ${j.kind === 's3' ? '' : '<b>' + escapeHtml(j.dest_host_name || 'Dest') + '</b>:'}<code class="mono">${escapeHtml(j.dest_path)}</code>
          </div>
          <div class="progress" style="height: 6px; margin: 8px 0; background: var(--surface-2);">
            <div class="progress-fill ${fillCls}" style="width: ${j.status === 'completed' ? '100' : pct}%;"></div>
          </div>
          <div class="transfer-job-stats">
            <span>${t('transferred')} <b class="mono">${transferredStr}</b>${totalStr ? ' / <span class="mono">' + totalStr + '</span> (' + pct + '%)' : ''}</span>
            <span style="display:inline-flex;align-items:center;gap:6px;">${speedStr ? svgIcon('zap', 'icon-xs') + ' <span class="mono">' + speedStr + '</span>' : ''} ${etaStr ? '<span class="mono">ETA: ' + etaStr + '</span>' : ''}</span>
          </div>
          ${j.note ? '<div style="margin-top: 6px; font-size: var(--fs-xs); color: var(--text-muted);">' + escapeHtml(j.note) + '</div>' : ''}
          ${j.error ? '<div class="sec-banner sec-banner-danger" style="margin-top: 8px; padding: 6px 10px; font-size: var(--fs-xs); display: flex; align-items: center; gap: 6px;"><svg class="icon icon-sm" style="color:var(--danger);"><use href="#i-circle-alert"/></svg> <span>' + escapeHtml(j.error) + '</span></div>' : ''}
        </div>
      `;
    }).join('');
  } catch (e) {
    console.error('loadTransfers error:', e);
  }
}

export { switchSettingsTab, copyPubKey, loadSSHKeys, renderSSHKeys, addSSHKey, deleteSSHKey, handleSSHKeyFileSelect, checkForUpdates, applySelfUpdate, restartPantau, openSettingsModal, load2FASettings, start2FASetup, cancel2FASetup, confirmEnable2FA, disable2FA, copy2FASecret, copy2FARecoveryCodes, download2FARecoveryCodes, saveSettings, testNotification, changePassword, saveGitHubBackupSettings, syncGitHubNow, exportSnapshotPrompt, submitSnapshotExport, openRestoreModal, switchRestoreTab, doRestoreFromGitHub, doRestoreFromFile, doStartFresh, checkFreshStatus, formatBytes, openTransferDialog, submitTransfer, loadS3Accounts, renderS3Accounts, editS3Account, cancelS3Form, saveS3Account, deleteS3Account, testS3Account, updateS3UploadDest, openS3UploadDialog, submitS3Upload, openTransfersModal, cancelTransfer, switchTransfersTab, loadTransfers };
