/* Databases: back up the database servers of one host (Host Detail tab), installed on the host
 * or running in Docker containers. Each Backup streams one gzip dump to the browser.
 * Backed by GET /api/databases?host_id= and GET /api/databases/backup?host_id=&where=&engine=&container=.
 * Relies on index.html globals: svgIcon, escapeHtml, t, I18N, showToast, currentHost, currentTab. */
(function () {
  'use strict';

  const B = (window.DbBackup = {});
  const S = { hostId: 0, sources: null, loading: false, error: '', seq: 0 };

  const esc = (s) => escapeHtml(String(s === null || s === undefined ? '' : s));
  const icon = (n, cls) => svgIcon(n, cls || 'icon-sm');
  const fill = (s, vars) => Object.keys(vars).reduce((a, k) => a.split('{' + k + '}').join(vars[k]), s);

  /* ---------- i18n ---------- */
  const I = {
    en: {
      dbb_loading: 'Looking for databases…',
      dbb_rescan: 'Scan again',
      dbb_load_failed: 'Could not look for databases: {why}',
      dbb_none_title: 'No database found',
      dbb_none_desc: 'Pantau looks for MySQL/MariaDB, PostgreSQL and MongoDB servers on the host and in running Docker containers. Docker access may be required.',
      dbb_col_engine: 'Database',
      dbb_col_where: 'Location',
      dbb_host: 'Host',
      dbb_container: 'Docker',
      dbb_backup: 'Backup',
      dbb_missing: '{tool} is not installed on the host',
      dbb_started: 'Backup started. The download begins when the database starts sending its dump.',
      dbb_failed: 'Backup failed',
      dbb_note: 'Backs up every database of the server as one compressed dump (.sql.gz, MongoDB .archive.gz). Host servers use the local login of the SSH user (root, or sudo, over the unix socket; the postgres account for PostgreSQL). Containers use the root credentials of their environment variables.',
      dbb_login_title: 'MongoDB login',
      dbb_login_hint: 'Leave the username empty if the database needs no login. The login is sent once to start the dump and is never stored.',
      dbb_user: 'Username',
      dbb_password: 'Password',
      dbb_authdb: 'Authentication database'
    },
    id: {
      dbb_loading: 'Mencari database…',
      dbb_rescan: 'Pindai ulang',
      dbb_load_failed: 'Tidak dapat mencari database: {why}',
      dbb_none_title: 'Tidak ada database ditemukan',
      dbb_none_desc: 'Pantau mencari server MySQL/MariaDB, PostgreSQL, dan MongoDB di host dan di container Docker yang berjalan. Akses Docker mungkin diperlukan.',
      dbb_col_engine: 'Database',
      dbb_col_where: 'Lokasi',
      dbb_host: 'Host',
      dbb_container: 'Docker',
      dbb_backup: 'Backup',
      dbb_missing: '{tool} belum terpasang di host',
      dbb_started: 'Backup dimulai. Unduhan mulai saat database mengirim dump-nya.',
      dbb_failed: 'Backup gagal',
      dbb_note: 'Membackup semua database pada server sebagai satu dump terkompresi (.sql.gz, MongoDB .archive.gz). Server di host memakai login lokal user SSH (root, atau sudo, lewat unix socket; akun postgres untuk PostgreSQL). Container memakai kredensial root dari environment variable-nya.',
      dbb_login_title: 'Login MongoDB',
      dbb_login_hint: 'Kosongkan username jika database tidak memerlukan login. Login hanya dikirim sekali untuk memulai dump dan tidak disimpan.',
      dbb_user: 'Username',
      dbb_password: 'Password',
      dbb_authdb: 'Database autentikasi'
    }
  };
  ['en', 'id'].forEach((l) => { I18N[l] = Object.assign(I18N[l] || {}, I[l]); });

  const ENGINE = { mysql: 'MySQL / MariaDB', postgres: 'PostgreSQL', mongo: 'MongoDB' };

  const activeHostId = () => (typeof currentHost !== 'undefined' && currentHost ? currentHost.id : 0);
  const tabStillActive = () => typeof currentTab !== 'undefined' && currentTab === 'databases' && activeHostId() === S.hostId;

  function row(src, i) {
    const where = src.where === 'host'
      ? `${icon('server')} ${esc(t('dbb_host'))}`
      : `${icon('container')} ${esc(t('dbb_container'))}: <span class="mono">${esc(src.container)}</span> <span class="dbb-image">${esc(src.image)}</span>`;
    const btn = src.missing
      ? `<button class="btn btn-sm" disabled title="${esc(fill(t('dbb_missing'), { tool: src.missing }))}">${icon('download')} <span>${esc(t('dbb_backup'))}</span></button>`
      : `<button class="btn btn-sm btn-primary" data-dbb-backup="${i}">${icon('download')} <span>${esc(t('dbb_backup'))}</span></button>`;
    return `<tr>
      <td><span class="badge badge-info">${esc(ENGINE[src.engine] || src.engine)}</span></td>
      <td>${where}</td>
      <td class="dbb-actions">${src.missing ? `<span class="dbb-missing">${icon('triangle-alert')} ${esc(fill(t('dbb_missing'), { tool: src.missing }))}</span>` : ''}${btn}</td>
    </tr>`;
  }

  function renderTab() {
    const box = document.getElementById('tabContent');
    if (!box || !tabStillActive()) return;
    if (S.loading) {
      box.innerHTML = `<div class="dbb-loading"><span class="spinner">${svgIcon('loader-circle', 'icon-sm')}</span><span>${esc(t('dbb_loading'))}</span></div>`;
      return;
    }
    const head = `<div class="dbb-head">
      <span class="dbb-note">${esc(t('dbb_note'))}</span>
      <button class="btn btn-sm" data-dbb-scan>${icon('refresh-cw')} <span>${esc(t('dbb_rescan'))}</span></button>
    </div>`;
    if (S.error) {
      box.innerHTML = `${head}<div class="dbb-err">${icon('circle-alert')} <span>${esc(S.error)}</span></div>`;
      return;
    }
    const list = S.sources || [];
    if (!list.length) {
      box.innerHTML = `${head}<div class="empty-state">
        <div class="empty-state-icon">${icon('database', 'icon-xl')}</div>
        <div class="empty-state-title">${esc(t('dbb_none_title'))}</div>
        <div class="empty-state-desc">${esc(t('dbb_none_desc'))}</div>
      </div>`;
      return;
    }
    box.innerHTML = `${head}<table class="dbb-table">
      <thead><tr><th>${esc(t('dbb_col_engine'))}</th><th>${esc(t('dbb_col_where'))}</th><th></th></tr></thead>
      <tbody>${list.map(row).join('')}</tbody>
    </table>`;
  }

  async function scan() {
    const seq = ++S.seq;
    S.loading = true;
    S.error = '';
    renderTab();
    let res, data = {};
    try {
      res = await fetch('/api/databases?host_id=' + encodeURIComponent(S.hostId));
      data = await res.json().catch(() => ({}));
    } catch (e) {
      data = { error: e.message };
    }
    if (seq !== S.seq) return;
    S.loading = false;
    if (!res || !res.ok) {
      S.error = fill(t('dbb_load_failed'), { why: data.error || (res ? 'HTTP ' + res.status : '') });
    } else {
      S.sources = data.sources || [];
    }
    renderTab();
  }

  // The dump streams through a hidden frame, submitted as a POST form so that a MongoDB login stays
  // out of the URL. A dump that fails right away answers with plain text, which loads in the frame and
  // is shown as a toast; a real download never fires 'load'.
  function submit(src, login) {
    const old = document.getElementById('dbbFrame');
    if (old) old.remove();
    const f = document.createElement('iframe');
    f.id = f.name = 'dbbFrame';
    f.hidden = true;
    document.body.appendChild(f);
    const form = document.createElement('form');
    form.method = 'POST';
    form.action = '/api/databases/backup';
    form.target = 'dbbFrame';
    form.hidden = true;
    const fields = Object.assign({ host_id: S.hostId, where: src.where, engine: src.engine, container: src.container || '' }, login || {});
    Object.keys(fields).forEach((k) => {
      const input = document.createElement('input');
      input.type = 'hidden';
      input.name = k;
      input.value = fields[k];
      form.appendChild(input);
    });
    document.body.appendChild(form);
    form.submit();
    form.remove();
    f.addEventListener('load', () => {
      let msg = '';
      try {
        if (f.contentWindow.location.href === 'about:blank') return;
        msg = f.contentDocument.body.innerText.trim();
      } catch (e) { /* not readable: fall through to the generic message */ }
      showToast(msg || t('dbb_failed'), 'error');
      f.remove();
    });
    showToast(t('dbb_started'), 'info');
  }

  /* ---------- MongoDB login prompt ---------- */
  function ensureLoginModal() {
    let el = document.getElementById('dbbLoginModal');
    if (el) el.remove(); // rebuilt each time so the texts follow the current language
    el = document.createElement('div');
    el.id = 'dbbLoginModal';
    el.className = 'modal';
    el.innerHTML = `<div class="modal-content" style="max-width: 440px;">
      <div class="modal-header"><h3 style="margin: 0; font-size: var(--fs-md); font-weight: 600;">${esc(t('dbb_login_title'))}</h3></div>
      <div class="modal-body" style="display: flex; flex-direction: column; gap: 12px;">
        <div class="dbb-note">${esc(t('dbb_login_hint'))}</div>
        <div class="form-group"><label class="form-label">${esc(t('dbb_user'))}</label><input type="text" id="dbbUser" autocomplete="off" spellcheck="false" /></div>
        <div class="form-group"><label class="form-label">${esc(t('dbb_password'))}</label><input type="password" id="dbbPass" autocomplete="new-password" /></div>
        <div class="form-group"><label class="form-label">${esc(t('dbb_authdb'))}</label><input type="text" id="dbbAuthDb" value="admin" autocomplete="off" spellcheck="false" /></div>
      </div>
      <div class="modal-footer">
        <button class="btn btn-sm" data-dbb-login-cancel>${esc(t('cancel', 'Cancel'))}</button>
        <button class="btn btn-sm btn-primary" data-dbb-login-go>${icon('download')} <span>${esc(t('dbb_backup'))}</span></button>
      </div>
    </div>`;
    el.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') { e.preventDefault(); el.querySelector('[data-dbb-login-go]').click(); }
    });
    document.body.appendChild(el);
  }

  function download(src) {
    if (src.engine !== 'mongo') { submit(src); return; }
    S.pending = src;
    ensureLoginModal();
    openModal('dbbLoginModal');
    document.getElementById('dbbUser').focus();
  }

  document.addEventListener('click', (e) => {
    if (!tabStillActive()) return;
    if (e.target.closest('[data-dbb-scan]')) {
      e.preventDefault();
      scan();
      return;
    }
    if (e.target.closest('[data-dbb-login-cancel]')) {
      closeModal('dbbLoginModal');
      return;
    }
    if (e.target.closest('[data-dbb-login-go]')) {
      const login = {
        user: document.getElementById('dbbUser').value.trim(),
        password: document.getElementById('dbbPass').value,
        auth_db: document.getElementById('dbbAuthDb').value.trim()
      };
      closeModal('dbbLoginModal');
      document.getElementById('dbbLoginModal').remove(); // drops the typed password with the dialog
      if (S.pending) submit(S.pending, login);
      return;
    }
    const btn = e.target.closest('[data-dbb-backup]');
    if (btn) {
      e.preventDefault();
      const src = (S.sources || [])[Number(btn.getAttribute('data-dbb-backup'))];
      if (src) download(src);
    }
  });

  B.renderTab = function (host) {
    if (!host || !host.id) return;
    S.hostId = host.id;
    S.sources = null;
    scan();
  };
})();
