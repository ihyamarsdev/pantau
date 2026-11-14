/* Fleet Search: search packages and containers across every host using only the data
 * Pantau already stored (package scans, container listings of the last inspection,
 * container drift records, remembered Compose projects). No SSH happens while searching.
 * Backed by GET /api/search?q=&kind=&limit=.
 * Relies on index.html globals: svgIcon, escapeHtml, t, I18N, openModal, closeModal,
 * showToast, formatRelativeTime, openHostDetail, switchTab. */
(function () {
  'use strict';

  const FS = (window.FleetSearch = {});
  const S = { query: '', kind: 'all', results: null, loading: false, error: '', timer: null, seq: 0, t0: 0 };

  const esc = (s) => escapeHtml(String(s === null || s === undefined ? '' : s));
  const icon = (n) => svgIcon(n, 'icon-sm');
  const fill = (s, vars) => Object.keys(vars).reduce((a, k) => a.split('{' + k + '}').join(vars[k]), s);

  const I = {
    en: {
      fs_title: 'Fleet Search',
      fs_intro: 'Search packages, containers and domains across every host, using the data Pantau already stored. Nothing is scanned while you search.',
      fs_ph: 'Search a package, container or domain name (2+ characters)',
      fs_all: 'All',
      fs_packages: 'Packages',
      fs_containers: 'Containers',
      fs_searching: 'Searching…',
      fs_none: 'Nothing found for this search.',
      fs_none_hint: 'Try a shorter name, or another kind of item.',
      fs_failed: 'Search failed',
      fs_hosts: '{n} hosts',
      fs_host: '{n} host',
      fs_truncated: 'More results were found. Narrow the search to see the rest.',
      fs_no_pkg_data: '{n} of {total} hosts have no package data yet. Open a host and use "Rescan Packages" to refresh it.',
      fs_no_docker_data: "{n} of {total} hosts have no container data yet. Container data refreshes on every inspection.",
      fs_domains: 'Domains',
      fs_no_domain_data: '{n} of {total} hosts have not been scanned for domains — open All domains → Scan all hosts.',
      fs_data_age: 'data from {age}',
      fs_never: 'no data age'
    },
    id: {
      fs_title: 'Pencarian Fleet',
      fs_intro: 'Cari paket, container, dan domain di seluruh host, memakai data yang sudah disimpan Pantau. Tidak ada pemindaian saat mencari.',
      fs_ph: 'Cari nama paket, container, atau domain (minimal 2 karakter)',
      fs_all: 'Semua',
      fs_packages: 'Paket',
      fs_containers: 'Container',
      fs_searching: 'Mencari…',
      fs_none: 'Tidak ada hasil untuk pencarian ini.',
      fs_none_hint: 'Coba nama yang lebih pendek, atau jenis item lain.',
      fs_failed: 'Pencarian gagal',
      fs_hosts: '{n} host',
      fs_host: '{n} host',
      fs_truncated: 'Ada hasil lain. Persempit pencarian untuk melihat sisanya.',
      fs_no_pkg_data: '{n} dari {total} host belum punya data paket. Buka sebuah host lalu pakai "Pindai Ulang Paket" untuk memperbaruinya.',
      fs_no_docker_data: '{n} dari {total} host belum punya data container. Data container diperbarui pada setiap inspeksi.',
      fs_domains: 'Domain',
      fs_no_domain_data: '{n} dari {total} host belum dipindai domain — buka Semua domain → Pindai semua host.',
      fs_data_age: 'data dari {age}',
      fs_never: 'tanpa usia data'
    }
  };
  ['en', 'id'].forEach((l) => { I18N[l] = Object.assign(I18N[l] || {}, I[l]); });

  const KINDS = [
    { id: 'all', key: 'fs_all' },
    { id: 'package', key: 'fs_packages' },
    { id: 'container', key: 'fs_containers' },
    { id: 'domain', key: 'fs_domains' }
  ];

  function modalHtml() {
    return `
      <div class="modal-content modal-dialog-lg" style="max-width: 760px; display: flex; flex-direction: column; max-height: 88vh;">
        <div class="modal-header">
          <div style="display: flex; align-items: center; gap: 8px;">
            ${icon('search')}
            <h3 style="margin: 0; font-size: var(--fs-md); font-weight: 600;">${esc(t('fs_title'))}</h3>
          </div>
          <button class="btn btn-sm btn-icon btn-ghost" onclick="closeModal('fleetSearchModal')">${icon('x')}</button>
        </div>
        <div class="modal-body" style="padding: 16px; overflow-y: auto;">
          <div class="fs-note">${esc(t('fs_intro'))}</div>
          <div class="fs-searchrow">
            <input type="text" id="fsQuery" placeholder="${esc(t('fs_ph'))}" autocomplete="off" spellcheck="false" />
            <div class="fs-kinds">
              ${KINDS.map((k) => `<button class="btn btn-sm fs-kind" data-kind="${k.id}">${esc(t(k.key))}</button>`).join('')}
            </div>
          </div>
          <div id="fsResults"></div>
        </div>
      </div>`;
  }

  function ensureModal() {
    const m = document.getElementById('fleetSearchModal');
    if (m) return;
    const el = document.createElement('div');
    el.id = 'fleetSearchModal';
    el.className = 'modal';
    el.innerHTML = modalHtml();
    document.body.appendChild(el);
  }

  function rebuildModal() {
    const m = document.getElementById('fleetSearchModal');
    if (!m) return;
    const input = document.getElementById('fsQuery');
    if (input) S.query = input.value.trim();
    m.innerHTML = modalHtml();
    const inp = document.getElementById('fsQuery');
    if (inp) inp.value = S.query;
    renderKinds();
    render();
  }
  if (typeof langRerenderHooks !== 'undefined') langRerenderHooks.push(rebuildModal);

  function renderKinds() {
    KINDS.forEach((k) => {
      const btn = document.querySelector('#fleetSearchModal .fs-kind[data-kind="' + k.id + '"]');
      if (btn) btn.className = 'btn btn-sm fs-kind' + (S.kind === k.id ? ' btn-primary' : '');
    });
  }

  function ageLabel(iso) {
    if (!iso) return t('fs_never');
    const r = formatRelativeTime(iso);
    return fill(t('fs_data_age'), { age: r.relative || '-' });
  }

  function hostRow(h, kind) {
    const tab = kind === 'container' ? 'docker' : (kind === 'domain' ? 'domains' : 'packages');
    const group = h.group ? ` · ${esc(h.group)}` : '';
    const version = h.version ? ` · ${esc(h.version)}` : '';
    const containers = (h.containers || []).length
      ? ' · ' + h.containers.map((c) => `${esc(c.name)}${c.state ? ' (' + esc(c.state) + ')' : ''}`).join(', ')
      : '';
    const detail = h.detail ? `<span class="fs-host-detail">${esc(h.detail)}</span>` : '';
    return `
      <button class="fs-host" data-host-id="${esc(h.host_id)}" data-tab="${tab}">
        <span class="fs-host-name">${icon('server')} ${esc(h.name)}</span>
        <span class="fs-host-meta">${group}${version}${containers}${detail}</span>
        <span class="fs-host-age" title="${esc(h.data_age || '')}">${icon('clock')} ${esc(ageLabel(h.data_age))}</span>
      </button>`;
  }

  function renderResults(data) {
    const groups = (data && data.groups) || [];
    if (!groups.length) {
      return `<div class="empty-state">
        <div class="empty-state-icon">${icon('search')}</div>
        <div class="empty-state-title">${esc(t('fs_none'))}</div>
        <div class="empty-state-desc">${esc(t('fs_none_hint'))}</div>
      </div>`;
    }
    const parts = groups.map((g) => {
      const count = g.host_count || (g.hosts || []).length;
      const countLabel = count === 1 ? t('fs_host', '{n} host') : t('fs_hosts', '{n} hosts');
      const kindIcon = g.kind === 'container' ? 'container' : (g.kind === 'domain' ? 'globe' : 'package');
      const version = g.version ? ' <span class="fs-group-version">' + esc(g.version) + '</span>' : '';
      return `
        <div class="fs-group">
          <div class="fs-group-head">
            <span class="fs-group-name">${icon(kindIcon)} ${esc(g.name)}${version}</span>
            <span class="badge">${esc(fill(countLabel, { n: count }))}</span>
          </div>
          <div class="fs-hosts">${(g.hosts || []).map((h) => hostRow(h, g.kind)).join('')}</div>
        </div>`;
    });
    if (data.truncated) {
      parts.push(`<div class="fs-warn">${icon('triangle-alert')} <span>${esc(t('fs_truncated'))}</span></div>`);
    }
    return parts.join('');
  }

  function render() {
    const box = document.getElementById('fsResults');
    if (!box) return;
    if (S.loading) {
      // SP.html adds the rotating icon and the live elapsed counter.
      box.innerHTML = `<div class="fs-empty">${window.SP ? SP.html(t('fs_searching'), S.t0) : `${icon('loader-circle')} <span>${esc(t('fs_searching'))}</span>`}</div>`;
      return;
    }
    if (S.error) {
      box.innerHTML = `<div class="fs-empty" style="color: var(--danger);">${esc(S.error)}</div>`;
      return;
    }
    if (!S.results) {
      box.innerHTML = '';
      return;
    }
    const d = S.results;
    const notes = [];
    if (d.hosts_without_packages > 0) {
      notes.push(`<div class="fs-note-line">${icon('info')} <span>${esc(fill(t('fs_no_pkg_data'), { n: d.hosts_without_packages, total: d.total_hosts }))}</span></div>`);
    }
    if (d.hosts_without_docker > 0) {
      notes.push(`<div class="fs-note-line">${icon('info')} <span>${esc(fill(t('fs_no_docker_data'), { n: d.hosts_without_docker, total: d.total_hosts }))}</span></div>`);
    }
    if (d.hosts_without_domains > 0) {
      notes.push(`<div class="fs-note-line">${icon('info')} <span>${esc(fill(t('fs_no_domain_data'), { n: d.hosts_without_domains, total: d.total_hosts }))}</span></div>`);
    }
    box.innerHTML = renderResults(d) + notes.join('');
  }

  async function search() {
    const q = S.query.trim();
    if (q.length < 2) {
      S.results = null;
      S.error = '';
      render();
      return;
    }
    const seq = ++S.seq;
    S.loading = true;
    S.error = '';
    S.t0 = Date.now();
    render();
    try {
      const res = await fetch('/api/search?q=' + encodeURIComponent(q) + '&kind=' + encodeURIComponent(S.kind) + '&limit=200');
      const data = await res.json().catch(() => ({}));
      if (seq !== S.seq) return; // a newer search already answered
      if (!res.ok) {
        S.error = data.error || t('fs_failed');
        S.results = null;
      } else {
        S.results = data;
      }
    } catch (e) {
      if (seq !== S.seq) return;
      S.error = e.message || t('fs_failed');
      S.results = null;
    }
    S.loading = false;
    render();
  }

  function scheduleSearch() {
    clearTimeout(S.timer);
    S.timer = setTimeout(search, 250);
  }

  async function openHost(hostID, tab) {
    try {
      closeModal('fleetSearchModal');
      // openHostDetail restores the remembered tab, so set it first.
      if (typeof setHostActiveTab === 'function') setHostActiveTab(hostID, tab);
      if (typeof openHostDetail === 'function') await openHostDetail(hostID);
      if (typeof switchTab === 'function') switchTab(tab);
    } catch (e) {
      showToast(e.message || String(e), 'error');
    }
  }

  // Scoped to the dialog: the page reuses data-host-id for other features.
  document.addEventListener('click', (e) => {
    const modal = document.getElementById('fleetSearchModal');
    if (!modal || !modal.contains(e.target)) return;
    const kindBtn = e.target.closest('.fs-kind');
    if (kindBtn) {
      S.kind = kindBtn.dataset.kind;
      renderKinds();
      search();
      return;
    }
    const hostBtn = e.target.closest('.fs-host');
    if (hostBtn) {
      e.preventDefault();
      openHost(parseInt(hostBtn.dataset.hostId, 10), hostBtn.dataset.tab);
    }
  });

  document.addEventListener('input', (e) => {
    const input = e.target;
    if (!input || input.id !== 'fsQuery') return;
    S.query = input.value;
    scheduleSearch();
  });

  document.addEventListener('keydown', (e) => {
    const input = e.target;
    if (!input || input.id !== 'fsQuery') return;
    if (e.key !== 'Enter') return;
    e.preventDefault();
    clearTimeout(S.timer);
    S.query = input.value;
    search();
  });

  FS.open = function (query) {
    ensureModal();
    if (query !== undefined && query !== null) S.query = String(query);
    const input = document.getElementById('fsQuery');
    if (input) input.value = S.query;
    renderKinds();
    openModal('fleetSearchModal');
    search();
  };
})();
