/* Accounts & SSH keys inventory of the Security tab, plus the fleet-wide key inventory.
 * Data comes from GET /api/hosts/{id}/security/accounts, POST /api/hosts/{id}/security/accounts/scan
 * and GET /api/security/keys. Relies on index.html globals: svgIcon, escapeHtml, t, I18N, openModal,
 * closeModal, showToast, currentHost. */
(function () {
  'use strict';

  const AC = (window.AC = {});
  let fleet = null;
  let fleetFilter = 'all';
  const S = { hostID: 0, audit: null, loading: false, busy: false, error: '', loadInFlight: false, loadID: 0, loadHostID: 0 };
  const esc = (s) => escapeHtml(String(s === null || s === undefined ? '' : s));
  const icon = (n) => svgIcon(n, 'icon-sm');
  const fill = (s, vars) => Object.keys(vars).reduce((a, k) => a.split('{' + k + '}').join(vars[k]), s);

  /* ---------- i18n ---------- */
  const I = {
    en: {
      ac_title: 'Accounts & SSH keys',
      ac_desc: 'Read-only inventory of the local accounts, their authorized_keys files, sudo grants and password state. Nothing is installed on the host.',
      ac_scan: 'Audit accounts',
      ac_scanning: 'Auditing...',
      ac_never: 'This host has not been audited yet.',
      ac_unsupported: 'Not assessed on this host.',
      ac_status: 'Status', ac_users: 'Users', ac_keys: 'Keys', ac_unique: 'Unique keys',
      ac_unknown: 'Unknown keys', ac_weak: 'Weak keys', ac_invalid: 'Unparseable lines',
      ac_uid0: 'Extra UID 0', ac_findings: 'Findings', ac_notes: 'Coverage notes',
      ac_col_user: 'User', ac_col_uid: 'UID', ac_col_shell: 'Shell', ac_col_groups: 'Groups',
      ac_col_sudo: 'Sudo', ac_col_pw: 'Password', ac_col_keys: 'Keys', ac_col_last: 'Last login',
      ac_col_fp: 'Fingerprint', ac_col_type: 'Type', ac_col_comment: 'Comment', ac_col_lib: 'Library key',
      ac_col_opts: 'Options', ac_no_users: 'No login-capable account was found.',
      ac_no_keys: 'No authorized_keys entry was found.',
      ac_no_findings: 'No finding.',
      ac_unknown_badge: 'Unknown', ac_none: 'none', ac_copy: 'Copy fingerprint',
      ac_copied: 'Fingerprint copied',
      ac_fleet: 'Fleet keys', ac_fleet_title: 'Fleet SSH key inventory',
      ac_fleet_all: 'All', ac_fleet_unknown: 'Unknown only', ac_fleet_weak: 'Weak only',
      ac_fleet_empty: 'No audited host has authorized keys yet.',
      ac_fleet_not_audited: '{n} host(s) not audited yet',
      ac_fleet_count: '{n} keys',
      ac_fleet_hosts: 'Hosts',
      ac_unreadable: 'unreadable', ac_restricted: 'restricted', ac_no_source: 'no from= restriction',
      ac_scan_ok: 'Account audit complete', ac_scan_failed: 'Account audit failed',
      ac_scan_cooldown: 'Account audit cooldown active, please wait',
      ac_scan_progress: 'Account audit already in progress',
      ac_sev: { critical: 'Critical', high: 'High', medium: 'Medium', low: 'Low', info: 'Info' },
      ac_status_ok: 'Complete', ac_status_partial: 'Partial coverage', ac_status_unsupported: 'Not assessed', ac_status_error: 'Error'
    },
    id: {
      ac_title: 'Akun & kunci SSH',
      ac_desc: 'Inventaris read-only akun lokal, berkas authorized_keys, hak sudo, dan status kata sandi. Tidak ada yang dipasang di host.',
      ac_scan: 'Audit akun',
      ac_scanning: 'Mengaudit...',
      ac_never: 'Host ini belum pernah diaudit.',
      ac_unsupported: 'Tidak dinilai pada host ini.',
      ac_status: 'Status', ac_users: 'Pengguna', ac_keys: 'Kunci', ac_unique: 'Kunci unik',
      ac_unknown: 'Kunci tak dikenal', ac_weak: 'Kunci lemah', ac_invalid: 'Baris tak terbaca',
      ac_uid0: 'UID 0 tambahan', ac_findings: 'Temuan', ac_notes: 'Catatan cakupan',
      ac_col_user: 'Pengguna', ac_col_uid: 'UID', ac_col_shell: 'Shell', ac_col_groups: 'Grup',
      ac_col_sudo: 'Sudo', ac_col_pw: 'Kata sandi', ac_col_keys: 'Kunci', ac_col_last: 'Login terakhir',
      ac_col_fp: 'Sidik jari', ac_col_type: 'Tipe', ac_col_comment: 'Komentar', ac_col_lib: 'Kunci library',
      ac_col_opts: 'Opsi', ac_no_users: 'Tidak ditemukan akun dengan shell login.',
      ac_no_keys: 'Tidak ditemukan entri authorized_keys.',
      ac_no_findings: 'Tidak ada temuan.',
      ac_unknown_badge: 'Tak dikenal', ac_none: 'tidak ada', ac_copy: 'Salin sidik jari',
      ac_copied: 'Sidik jari disalin',
      ac_fleet: 'Kunci fleet', ac_fleet_title: 'Inventaris kunci SSH seluruh fleet',
      ac_fleet_all: 'Semua', ac_fleet_unknown: 'Hanya tak dikenal', ac_fleet_weak: 'Hanya lemah',
      ac_fleet_empty: 'Belum ada host yang diaudit memiliki authorized_keys.',
      ac_fleet_not_audited: '{n} host belum diaudit',
      ac_fleet_count: '{n} kunci',
      ac_fleet_hosts: 'Host',
      ac_unreadable: 'tak terbaca', ac_restricted: 'terbatas', ac_no_source: 'tanpa batas from=',
      ac_scan_ok: 'Audit akun selesai', ac_scan_failed: 'Audit akun gagal',
      ac_scan_cooldown: 'Jeda audit akun aktif, tunggu sebentar',
      ac_scan_progress: 'Audit akun sedang berjalan',
      ac_sev: { critical: 'Kritis', high: 'Tinggi', medium: 'Sedang', low: 'Rendah', info: 'Info' },
      ac_status_ok: 'Lengkap', ac_status_partial: 'Cakupan sebagian', ac_status_unsupported: 'Tidak dinilai', ac_status_error: 'Galat'
    }
  };
  ['en', 'id'].forEach((l) => { I18N[l] = Object.assign(I18N[l] || {}, I[l]); });

  const sevBadge = (sev) => {
    const cls = { critical: 'badge-danger', high: 'badge-warning', medium: 'badge-warning', low: 'badge-info', info: 'badge-unknown' }[sev] || 'badge-unknown';
    const label = (I.en.ac_sev[sev] || sev);
    const loc = (I.id.ac_sev[sev] || label);
    return `<span class="badge ${cls}">${esc(currentLang === 'id' ? loc : label)}</span>`;
  };

  const shortFp = (fp) => {
    const f = String(fp || '').replace(/^SHA256:/, '');
    return f.slice(0, 12);
  };

  const statusBadge = (st) => {
    const map = { ok: 'badge-success', partial: 'badge-warning', unsupported: 'badge-sec-unknown', error: 'badge-danger' };
    const key = 'ac_status_' + st;
    return `<span class="badge ${map[st] || 'badge-sec-unknown'}">${esc(t(key, st))}</span>`;
  };

  /* ---------- card ---------- */
  AC.html = () => '<div class="card" id="secAccountsCard" style="padding: 16px; margin-top: 16px;"></div>';

  AC.render = function () {
    const el = document.getElementById('secAccountsCard');
    const host = currentHost;
    if (!el || !host) return;
    if (S.hostID !== host.id) Object.assign(S, { hostID: host.id, audit: null, error: '', loading: false, busy: false, loadInFlight: false });
    const a = S.audit;
    const working = S.loading || S.busy;
    const scanBtn = `<button class="btn btn-sm btn-primary" id="btnAccountAudit" onclick="AC.scan()" ${working ? 'disabled' : ''}>${working ? SP.html(t('ac_scanning'), S.t0) : icon('refresh-cw') + ' <span>' + esc(t('ac_scan')) + '</span>'}</button>`;
    const fleetBtn = `<button class="btn btn-sm btn-ghost" onclick="AC.openFleet()">${icon('key-round')} <span>${esc(t('ac_fleet'))}</span></button>`;

    let body = '';
    if (S.error) body += `<div style="background: var(--danger-soft); border: 1px solid var(--danger-border); border-radius: var(--radius); padding: 10px 12px; color: var(--danger); font-size: 0.82rem; margin-bottom: 12px;">${esc(S.error)}</div>`;
    if (!a) {
      body += `<div style="color: var(--text-muted); font-size: 0.85rem;">${S.loading ? SP.html(t('ac_scanning'), S.t0) : esc(t('ac_never'))}</div>`;
    } else if (a.status === 'unsupported') {
      body += `<div style="color: var(--text-muted); font-size: 0.85rem;">${esc(t('ac_unsupported'))}</div>` + notesHtml(a);
    } else {
      body = summaryHtml(a) + usersHtml(a) + keysHtml(a) + findingsHtml(a) + notesHtml(a);
    }

    el.innerHTML = `
      <div style="display: flex; justify-content: space-between; align-items: center; gap: 8px; margin-bottom: 12px; flex-wrap: wrap;">
        <h4 style="margin: 0; display: flex; align-items: center; gap: 6px;">${icon('users')} <span>${esc(t('ac_title'))}</span>${a ? statusBadge(a.status) : ''}</h4>
        <div style="display: flex; gap: 6px; align-items: center;">${fleetBtn}${scanBtn}</div>
      </div>
      ${body}`;
  };

  function summaryHtml(a) {
    const s = a.summary || {};
    const chip = (label, value, cls) => `<span class="badge ${cls || 'badge-unknown'}">${esc(label)}: ${esc(value)}</span>`;
    return `<div style="display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 12px;">
      ${chip(t('ac_users'), s.users || 0)}
      ${chip(t('ac_keys'), s.keys || 0)}
      ${chip(t('ac_unique'), s.unique_keys || 0)}
      ${chip(t('ac_unknown'), s.unknown_keys || 0, s.unknown_keys ? 'badge-warning' : 'badge-unknown')}
      ${chip(t('ac_weak'), s.weak_keys || 0, s.weak_keys ? 'badge-danger' : 'badge-unknown')}
      ${chip(t('ac_invalid'), s.invalid_lines || 0)}
      ${chip(t('ac_uid0'), s.uid0_accounts || 0, s.uid0_accounts ? 'badge-danger' : 'badge-unknown')}
    </div>`;
  }

  function usersHtml(a) {
    const users = a.users || [];
    if (!users.length) return `<div style="color: var(--text-muted); font-size: 0.85rem; margin-bottom: 12px;">${esc(t('ac_no_users'))}</div>`;
    const rows = users.map((u) => {
      const pw = { set: 'badge-success', empty: 'badge-danger', locked: 'badge-info', unknown: 'badge-sec-unknown' }[u.password_state] || 'badge-sec-unknown';
      const pwLabel = { set: 'set', empty: 'empty', locked: 'locked', unknown: 'unknown' }[u.password_state] || 'unknown';
      const sudo = u.sudo_grant && u.sudo_grant !== 'none'
        ? `<span class="badge ${u.sudo_grant.indexOf('NOPASSWD') === 0 ? 'badge-danger' : 'badge-warning'}">${esc(u.sudo_grant)}</span>`
        : `<span style="color: var(--text-muted);">${esc(t('ac_none'))}</span>`;
      return `<tr style="border-bottom: 1px solid var(--card-border);">
        <td style="padding: 6px 12px; white-space: nowrap;">${esc(u.name)}</td>
        <td style="padding: 6px 12px; white-space: nowrap;">${esc(u.uid)}</td>
        <td style="padding: 6px 12px; font-family: var(--font-mono); font-size: 0.8rem;">${esc(u.shell)}</td>
        <td style="padding: 6px 12px; font-size: 0.8rem;">${esc((u.groups || []).join(', ') || '-')}</td>
        <td style="padding: 6px 12px; white-space: nowrap;">${sudo}</td>
        <td style="padding: 6px 12px; white-space: nowrap;"><span class="badge ${pw}">${esc(pwLabel)}</span></td>
        <td style="padding: 6px 12px; white-space: nowrap;">${esc(u.key_count || 0)}${(u.key_files_unreadable || []).length ? ` <span class="badge badge-sec-unknown" title="${esc(t('ac_unreadable'))}">${esc(t('ac_unreadable'))}</span>` : ''}</td>
        <td style="padding: 6px 12px; white-space: nowrap; font-size: 0.8rem;">${esc(u.last_login || '-')}</td>
      </tr>`;
    }).join('');
    const cols = [t('ac_col_user'), t('ac_col_uid'), t('ac_col_shell'), t('ac_col_groups'), t('ac_col_sudo'), t('ac_col_pw'), t('ac_col_keys'), t('ac_col_last')];
    return `<div style="overflow-x: auto; margin-bottom: 12px;"><table style="width: 100%; border-collapse: collapse; font-size: 0.85rem;">
      <thead><tr style="border-bottom: 1px solid var(--card-border); color: var(--text-muted); text-align: left;">${cols.map((c) => `<th style="padding: 6px 12px;">${esc(c)}</th>`).join('')}</tr></thead>
      <tbody>${rows}</tbody></table></div>`;
  }

  function keysHtml(a) {
    const keys = (a.keys || []).filter((k) => k.valid);
    if (!keys.length) return `<div style="color: var(--text-muted); font-size: 0.85rem; margin-bottom: 12px;">${esc(t('ac_no_keys'))}</div>`;
    const rows = keys.map((k) => {
      const opts = (k.options || []).map((o) => `<span class="badge badge-info">${esc(o)}</span>`).join(' ');
      const lib = k.library_key
        ? `<span class="badge badge-success">${esc(k.library_key)}</span>`
        : `<span class="badge badge-warning">${esc(t('ac_unknown_badge'))}</span>`;
      const bits = k.bits ? `${esc(k.type)} ${esc(k.bits)}` : esc(k.type);
      return `<tr style="border-bottom: 1px solid var(--card-border);">
        <td style="padding: 6px 12px; white-space: nowrap;">
          <span class="dk-mono" title="${esc(k.fingerprint)}">${esc(shortFp(k.fingerprint))}</span>
          <button class="btn btn-icon btn-sm btn-ghost" title="${esc(t('ac_copy'))}" onclick="AC.copyFp('${esc(k.fingerprint)}')">${icon('copy')}</button>
        </td>
        <td style="padding: 6px 12px; white-space: nowrap;">${esc(bits)}${k.weak ? ` <span class="badge badge-danger">${esc(t('ac_weak'))}</span>` : ''}</td>
        <td style="padding: 6px 12px; font-size: 0.8rem;">${esc(k.comment || '-')}</td>
        <td style="padding: 6px 12px; white-space: nowrap;">${lib}</td>
        <td style="padding: 6px 12px; white-space: nowrap;">${opts || '-'}</td>
        <td style="padding: 6px 12px; white-space: nowrap;">${esc(k.user)}</td>
      </tr>`;
    }).join('');
    const cols = [t('ac_col_fp'), t('ac_col_type'), t('ac_col_comment'), t('ac_col_lib'), t('ac_col_opts'), t('ac_col_user')];
    return `<div style="overflow-x: auto; margin-bottom: 12px;"><table style="width: 100%; border-collapse: collapse; font-size: 0.85rem;">
      <thead><tr style="border-bottom: 1px solid var(--card-border); color: var(--text-muted); text-align: left;">${cols.map((c) => `<th style="padding: 6px 12px;">${esc(c)}</th>`).join('')}</tr></thead>
      <tbody>${rows}</tbody></table></div>`;
  }

  function findingsHtml(a) {
    const fs = a.findings || [];
    if (!fs.length) return `<div style="color: var(--text-muted); font-size: 0.85rem; margin-bottom: 12px;">${esc(t('ac_no_findings'))}</div>`;
    const items = fs.map((f) => `<li style="margin-bottom: 6px;">${sevBadge(f.severity)} <strong>${esc(f.title)}</strong>${f.user ? ` <span style="color: var(--text-muted);">${esc(f.user)}</span>` : ''}<div style="color: var(--text-muted); font-size: 0.82rem;">${esc(f.detail)}</div></li>`).join('');
    return `<div style="margin-bottom: 12px;"><h5 style="margin: 0 0 6px 0; font-size: 0.85rem;">${esc(t('ac_findings'))}</h5><ul style="margin: 0; padding-left: 18px; font-size: 0.85rem;">${items}</ul></div>`;
  }

  function notesHtml(a) {
    const ns = a.notes || [];
    if (!ns.length) return '';
    return `<div style="color: var(--text-muted); font-size: 0.8rem; margin-bottom: 8px;"><strong>${esc(t('ac_notes'))}</strong><ul style="margin: 4px 0 0; padding-left: 18px;">${ns.map((n) => `<li>${esc(n)}</li>`).join('')}</ul></div>`;
  }

  /* ---------- actions ---------- */
  AC.load = async function () {
    const host = currentHost;
    if (!host) return;
    // The card is rendered from two places (tab render and footer append): one fetch is enough.
    // The guard is per host: a load for another host must not block this one.
    if (S.loadInFlight && S.loadHostID === host.id) return;
    const loadID = ++S.loadID;
    S.loadInFlight = true;
    S.loadHostID = host.id;
    S.loading = true;
    S.t0 = Date.now();
    S.error = '';
    AC.render();
    try {
      const res = await fetch('/api/hosts/' + host.id + '/security/accounts');
      if (!res.ok) throw new Error('HTTP ' + res.status);
      const data = await res.json();
      if (loadID !== S.loadID || S.hostID !== host.id) return; // switched host while waiting
      S.audit = data.audited ? data.audit : null;
    } catch (e) {
      if (loadID !== S.loadID || S.hostID !== host.id) return; // switched host while waiting
      S.error = e.message;
    }
    if (loadID !== S.loadID) return; // a newer load owns the state now
    S.loading = false;
    S.loadInFlight = false;
    AC.render();
  };

  AC.scan = async function () {
    const host = currentHost;
    if (!host) return;
    S.busy = true;
    S.t0 = Date.now();
    S.error = '';
    AC.render();
    try {
      const res = await fetch('/api/hosts/' + host.id + '/security/accounts/scan', { method: 'POST' });
      if (res.ok) {
        const data = await res.json();
        S.audit = data.audited ? data.audit : null;
        showToast(t('ac_scan_ok'), 'success');
      } else if (res.status === 429) {
        const errData = await res.json().catch(() => ({}));
        showToast(errData.error || t('ac_scan_cooldown'), 'warning');
      } else if (res.status === 409) {
        showToast(t('ac_scan_progress'), 'warning');
      } else {
        const errData = await res.json().catch(() => ({}));
        showToast(t('ac_scan_failed') + ': ' + (errData.error || res.statusText), 'error');
      }
    } catch (e) {
      showToast(t('ac_scan_failed') + ': ' + e.message, 'error');
    }
    S.busy = false;
    AC.render();
  };

  AC.copyFp = function (fp) {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(fp);
      showToast(t('ac_copied'), 'success', 2000);
    }
  };

  /* ---------- fleet modal ---------- */
  AC.openFleet = async function () {
    if (!document.getElementById('accountsFleetModal')) {
      const m = document.createElement('div');
      m.id = 'accountsFleetModal';
      m.className = 'modal';
      m.innerHTML = `
        <div class="modal-content modal-workspace">
          <div class="modal-header">
            <div class="sd-title">${icon('key-round')} <h3>${esc(t('ac_fleet_title'))}</h3></div>
            <button class="btn btn-icon btn-sm btn-ghost" onclick="closeModal('accountsFleetModal')">${icon('x')}</button>
          </div>
          <div class="modal-body" id="acFleetBody"></div>
        </div>`;
      document.body.appendChild(m);
    }
    openModal('accountsFleetModal');
    renderFleet();
    if (fleet) return;
    try {
      const res = await fetch('/api/security/keys');
      fleet = res.ok ? await res.json() : { keys: [], error: 'HTTP ' + res.status };
    } catch (e) {
      fleet = { keys: [], error: e.message };
    }
    renderFleet();
  };

  AC.setFleetFilter = function (f) {
    fleetFilter = f;
    renderFleet();
  };

  function renderFleet() {
    const body = document.getElementById('acFleetBody');
    if (!body) return;
    if (!fleet) { body.innerHTML = `<div style="color: var(--text-muted);">${esc(t('loading', 'Loading...'))}</div>`; return; }
    if (fleet.error) { body.innerHTML = `<div style="color: var(--danger);">${esc(fleet.error)}</div>`; return; }
    let keys = fleet.keys || [];
    if (fleetFilter === 'unknown') keys = keys.filter((k) => k.unknown);
    if (fleetFilter === 'weak') keys = keys.filter((k) => k.weak);
    const filterBtn = (id, label) => `<button class="btn btn-sm ${fleetFilter === id ? 'btn-primary' : 'btn-ghost'}" onclick="AC.setFleetFilter('${id}')">${esc(label)}</button>`;
    const rows = keys.map((k) => {
      const hosts = (k.hosts || []).map((h) => `${esc(h.name || h.host_id)}:${esc(h.user)}`).join(', ');
      const lib = k.library_key ? `<span class="badge badge-success">${esc(k.library_key)}</span>` : `<span class="badge badge-warning">${esc(t('ac_unknown_badge'))}</span>`;
      const bits = k.bits ? `${esc(k.type)} ${esc(k.bits)}` : esc(k.type);
      return `<tr style="border-bottom: 1px solid var(--card-border);">
        <td style="padding: 6px 12px; white-space: nowrap;"><span class="dk-mono" title="${esc(k.fingerprint)}">${esc(shortFp(k.fingerprint))}</span></td>
        <td style="padding: 6px 12px; white-space: nowrap;">${esc(bits)}${k.weak ? ` <span class="badge badge-danger">${esc(t('ac_weak'))}</span>` : ''}</td>
        <td style="padding: 6px 12px; white-space: nowrap;">${lib}</td>
        <td style="padding: 6px 12px; font-size: 0.8rem;">${hosts || '-'}</td>
      </tr>`;
    }).join('');
    const cols = [t('ac_col_fp'), t('ac_col_type'), t('ac_col_lib'), t('ac_fleet_hosts')];
    body.innerHTML = `
      <div style="display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 10px; align-items: center;">
        ${filterBtn('all', t('ac_fleet_all'))}${filterBtn('unknown', t('ac_fleet_unknown'))}${filterBtn('weak', t('ac_fleet_weak'))}
        <span style="color: var(--text-muted); font-size: var(--fs-xs);">${esc(fill(t('ac_fleet_count'), { n: keys.length }))}</span>
        <span style="color: var(--text-muted); font-size: var(--fs-xs);">${esc(fill(t('ac_fleet_not_audited'), { n: fleet.hosts_not_audited || 0 }))}</span>
      </div>
      ${keys.length ? `<div style="overflow-x: auto;"><table style="width: 100%; border-collapse: collapse; font-size: 0.85rem;">
        <thead><tr style="border-bottom: 1px solid var(--card-border); color: var(--text-muted); text-align: left;">${cols.map((c) => `<th style="padding: 6px 12px;">${esc(c)}</th>`).join('')}</tr></thead>
        <tbody>${rows}</tbody></table></div>` : `<div style="color: var(--text-muted); font-size: 0.85rem;">${esc(t('ac_fleet_empty'))}</div>`}`;
  }
})();
