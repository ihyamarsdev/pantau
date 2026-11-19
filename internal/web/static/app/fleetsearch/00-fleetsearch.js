/* Fleet Search: find any infrastructure entity Pantau has stored, across every
 * host, without SSH. Backed by GET /api/search?q=&kind=&limit=.
 * Results are grouped by category, ranked (exact > prefix > substring), carry
 * deep-link metadata, and support Up/Down/Enter keyboard navigation (ADR 0087).
 * Relies on index.html globals: svgIcon, escapeHtml, t, I18N, openModal,
 * closeModal, showToast, formatRelativeTime, openHostDetail, switchTab,
 * setHostActiveTab, switchSideNav, openSettingsModal, loadHosts, renderHosts. */
(function () {
  'use strict';

  const FS = (window.FleetSearch = {});
  const S = { query: '', kind: 'all', results: null, loading: false, error: '', timer: null, seq: 0, t0: 0, active: -1 };

  const esc = (s) => escapeHtml(String(s === null || s === undefined ? '' : s));
  const icon = (n) => svgIcon(n, 'icon-sm');
  const fill = (s, vars) => Object.keys(vars).reduce((a, k) => a.split('{' + k + '}').join(vars[k]), s);
  const g = (name) => (typeof window[name] === 'function' ? window[name].bind(window) : null);

  const I = {
    en: {
      fs_title: 'Fleet Search',
      fs_intro: 'Find any infrastructure entity across every host, using the data Pantau already stored. Nothing is scanned while you search.',
      fs_ph: 'Search anything — or use a prefix: folder:, host:, port:, domain:, container:, service:, kube:, vpn:, cf:, git:, ansible:, tf:, pkg:, cve:',
      fs_hint: 'Prefixes: folder: host: port: domain: container: service: kube: vpn: cf: git: ansible: tf: pkg: cve:',
      fs_all: 'All',
      fs_folders: 'Folders / Host Groups',
      fs_hosts: 'Hosts',
      fs_packages: 'Packages',
      fs_containers: 'Containers',
      fs_ports: 'Ports & Services',
      fs_services: 'Services',
      fs_domains: 'Domains',
      fs_kube: 'Kubernetes',
      fs_vpn: 'VPN Networks',
      fs_cf: 'Cloudflare',
      fs_git: 'Git Repos',
      fs_ansible: 'Ansible',
      fs_tf: 'Terraform',
      fs_pkg: 'Pending Updates',
      fs_cve: 'CVEs / Findings',
      fs_sshkey: 'SSH Keys',
      fs_accounts: 'Accounts & Keys',
      fs_searching: 'Searching…',
      fs_none: 'Nothing found for this search.',
      fs_none_hint: 'Try a shorter name, another prefix, or another kind of item.',
      fs_failed: 'Search failed',
      fs_host_count: '{n} hosts',
      fs_host_count_one: '{n} host',
      fs_truncated: 'More results were found. Narrow the search to see the rest.',
      fs_no_pkg_data: '{n} of {total} hosts have no package data yet. Open a host and use "Rescan Packages" to refresh it.',
      fs_no_docker_data: "{n} of {total} hosts have no container data yet. Container data refreshes on every inspection.",
      fs_no_domain_data: '{n} of {total} hosts have not been scanned for domains — open All domains → Scan all hosts.',
      fs_data_age: 'data from {age}',
      fs_never: 'no data age',
      fs_nav_hint: '↑↓ navigate · Enter open'
    },
    id: {
      fs_title: 'Pencarian Fleet',
      fs_intro: 'Temukan entitas infrastruktur apa pun di seluruh host, memakai data yang sudah disimpan Pantau. Tidak ada pemindaian saat mencari.',
      fs_ph: 'Cari apa saja — atau pakai prefiks: folder:, host:, port:, domain:, container:, service:, kube:, vpn:, cf:, git:, ansible:, tf:, pkg:, cve:',
      fs_hint: 'Prefiks: folder: host: port: domain: container: service: kube: vpn: cf: git: ansible: tf: pkg: cve:',
      fs_all: 'Semua',
      fs_folders: 'Folder / Grup Host',
      fs_hosts: 'Host',
      fs_packages: 'Paket',
      fs_containers: 'Container',
      fs_ports: 'Port & Layanan',
      fs_services: 'Layanan',
      fs_domains: 'Domain',
      fs_kube: 'Kubernetes',
      fs_vpn: 'Jaringan VPN',
      fs_cf: 'Cloudflare',
      fs_git: 'Repo Git',
      fs_ansible: 'Ansible',
      fs_tf: 'Terraform',
      fs_pkg: 'Pembaruan Tertunda',
      fs_cve: 'CVE / Temuan',
      fs_sshkey: 'Kunci SSH',
      fs_accounts: 'Akun & Kunci',
      fs_searching: 'Mencari…',
      fs_none: 'Tidak ada hasil untuk pencarian ini.',
      fs_none_hint: 'Coba nama yang lebih pendek, prefiks lain, atau jenis item lain.',
      fs_failed: 'Pencarian gagal',
      fs_host_count: '{n} host',
      fs_host_count_one: '{n} host',
      fs_truncated: 'Ada hasil lain. Persempit pencarian untuk melihat sisanya.',
      fs_no_pkg_data: '{n} dari {total} host belum punya data paket. Buka sebuah host lalu pakai "Pindai Ulang Paket" untuk memperbaruinya.',
      fs_no_docker_data: '{n} dari {total} host belum punya data container. Data container diperbarui pada setiap inspeksi.',
      fs_no_domain_data: '{n} dari {total} host belum dipindai domain — buka Semua domain → Pindai semua host.',
      fs_data_age: 'data dari {age}',
      fs_never: 'tanpa usia data',
      fs_nav_hint: '↑↓ navigasi · Enter buka'
    }
  };
  ['en', 'id'].forEach((l) => { I18N[l] = Object.assign(I18N[l] || {}, I[l]); });

  // Every category the backend supports, with its icon and i18n label.
  const KINDS = [
    { id: 'all', key: 'fs_all', icon: 'layers' },
    { id: 'folder', key: 'fs_folders', icon: 'folder' },
    { id: 'host', key: 'fs_hosts', icon: 'server' },
    { id: 'package', key: 'fs_packages', icon: 'package' },
    { id: 'container', key: 'fs_containers', icon: 'container' },
    { id: 'port', key: 'fs_ports', icon: 'plug' },
    { id: 'service', key: 'fs_services', icon: 'settings' },
    { id: 'domain', key: 'fs_domains', icon: 'globe' },
    { id: 'kube', key: 'fs_kube', icon: 'boxes' },
    { id: 'vpn', key: 'fs_vpn', icon: 'cable' },
    { id: 'cf', key: 'fs_cf', icon: 'cloud' },
    { id: 'git', key: 'fs_git', icon: 'git-branch' },
    { id: 'ansible', key: 'fs_ansible', icon: 'ansible' },
    { id: 'tf', key: 'fs_tf', icon: 'terraform' },
    { id: 'pkg', key: 'fs_pkg', icon: 'arrow-up' },
    { id: 'cve', key: 'fs_cve', icon: 'shield-alert' },
    { id: 'sshkey', key: 'fs_sshkey', icon: 'key-round' },
    { id: 'account', key: 'fs_accounts', icon: 'users' }
  ];
  const KIND_BY_ID = Object.fromEntries(KINDS.map((k) => [k.id, k]));

  function modalHtml() {
    return `
      <div class="modal-content modal-dialog-lg" style="max-width: 860px; display: flex; flex-direction: column; max-height: 88vh;">
        <div class="modal-header">
          <div style="display: flex; align-items: center; gap: 8px;">
            ${icon('search')}
            <h3 style="margin: 0; font-size: var(--fs-md); font-weight: 600;">${esc(t('fs_title'))}</h3>
          </div>
          <button class="btn btn-sm btn-icon btn-ghost" onclick="closeModal('fleetSearchModal')">${icon('x')}</button>
        </div>
        <div class="modal-body" style="padding: 16px; overflow-y: auto; display: flex; flex-direction: column; min-height: 0;">
          <div class="fs-note">${esc(t('fs_intro'))}</div>
          <div class="fs-searchrow">
            <input type="text" id="fsQuery" placeholder="${esc(t('fs_ph'))}" autocomplete="off" spellcheck="false" />
          </div>
          <div class="fs-hint">${icon('info')} <span>${esc(t('fs_hint'))}</span></div>
          <div class="fs-kinds">
            ${KINDS.map((k) => `<button class="btn btn-sm fs-kind" data-kind="${k.id}" title="${esc(t(k.key))}">${icon(k.icon)} ${esc(t(k.key))}</button>`).join('')}
          </div>
          <div id="fsResults" tabindex="-1"></div>
          <div class="fs-navhint">${esc(t('fs_nav_hint'))}</div>
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

  // Flat list of result rows in display order, used for arrow navigation.
  let rows = [];

  function resultRow(item, idx) {
    const isFolder = item.category === 'folder';
    const tab = item.targetTab || 'overview';
    const meta = [item.subtitle, item.version, item.statusBadge].filter(Boolean).map(esc).join(' · ');
    const count = item.hosts || [];
    const hostList = count.map((h) => {
      const hmeta = [h.group, h.version, h.detail].filter(Boolean).map(esc).join(' · ');
      const containers = (h.containers || []).map((c) => esc(c.name) + (c.state ? ' (' + esc(c.state) + ')' : '')).join(', ');
      return `<span class="fs-host" data-host-id="${esc(h.host_id)}" data-tab="${esc(tab)}">
          <span class="fs-host-name">${icon('server')} ${esc(h.name)}</span>
          ${hmeta || containers ? `<span class="fs-host-meta">${hmeta}${containers ? ' · ' + containers : ''}</span>` : ''}
          <span class="fs-host-age" title="${esc(h.data_age || '')}">${icon('clock')} ${esc(ageLabel(h.data_age))}</span>
        </span>`;
    }).join('');
    const badge = item.statusBadge ? `<span class="badge fs-badge">${esc(item.statusBadge)}</span>` : '';
    return `<div class="fs-group" data-result="${idx}">
        <button class="fs-result-head ${idx === S.active ? 'is-active' : ''}" data-result-head="${idx}" type="button">
          <span class="fs-group-name">${icon(KIND_BY_ID[item.category] ? KIND_BY_ID[item.category].icon : 'circle')} ${esc(item.name)}${item.version ? ` <span class="fs-group-version">${esc(item.version)}</span>` : ''}</span>
          ${badge}
          <span class="badge fs-count">${esc(fill(count.length === 1 ? t('fs_host_count_one') : t('fs_host_count'), { n: count.length }))}</span>
        </button>
        ${meta ? `<div class="fs-subtitle">${esc(meta)}</div>` : ''}
        <div class="fs-hosts">${isFolder ? '' : hostList}</div>
      </div>`;
  }

  function renderResults(data) {
    const groups = (data && data.groups) || [];
    rows = [];
    groups.forEach((g, i) => {
      rows.push({ category: g.category || g.kind, entityId: g.entity_id || '', groupId: g.group_id || '', targetTab: g.target_tab || '', subtitle: g.subtitle || '', name: g.name, hosts: g.hosts || [], version: g.version || '', statusBadge: g.status_badge || '' });
    });
    if (!rows.length) {
      return `<div class="empty-state">
        <div class="empty-state-icon">${icon('search')}</div>
        <div class="empty-state-title">${esc(t('fs_none'))}</div>
        <div class="empty-state-desc">${esc(t('fs_none_hint'))}</div>
      </div>`;
    }
    const parts = rows.map((r, i) => resultRow(r, i));
    if (data.truncated) {
      parts.push(`<div class="fs-warn">${icon('triangle-alert')} <span>${esc(t('fs_truncated'))}</span></div>`);
    }
    return parts.join('');
  }

  function render() {
    const box = document.getElementById('fsResults');
    if (!box) return;
    if (S.loading) {
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
      S.active = -1;
      render();
      return;
    }
    const seq = ++S.seq;
    S.loading = true;
    S.error = '';
    S.active = -1;
    S.t0 = Date.now();
    render();
    try {
      const res = await fetch('/api/search?q=' + encodeURIComponent(q) + '&kind=' + encodeURIComponent(S.kind) + '&limit=200');
      const data = await res.json().catch(() => ({}));
      if (seq !== S.seq) return;
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

  // Deterministic deep-link navigation from the structured metadata.
  async function openResult(r, host) {
    try {
      const nav = g('switchSideNav');
      const tab = (host && host.dataset && host.dataset.tab) || r.targetTab || 'overview';
      // Enter on a result head has no clicked host element: use the first host
      // of the group so host-backed results still open their workspace tab.
      const firstHost = (r.hosts && r.hosts[0]) || null;
      const hostID = host && host.dataset && host.dataset.hostId
        ? parseInt(host.dataset.hostId, 10)
        : (firstHost && firstHost.host_id ? parseInt(firstHost.host_id, 10) : 0);

      if (r.category === 'folder') {
        closeModal('fleetSearchModal');
        const select = document.getElementById('groupFilterSelect');
        if (select) {
          const value = r.groupId || r.name;
          if (!Array.from(select.options).some((o) => o.value === value)) {
            const opt = document.createElement('option');
            opt.value = value;
            opt.textContent = value;
            select.appendChild(opt);
          }
          select.value = value;
          select.dispatchEvent(new Event('change', { bubbles: true }));
        }
        if (nav) nav('hosts');
        const load = g('loadHosts'); if (load) await load();
        const renderHostsFn = g('renderHosts'); if (renderHostsFn) renderHostsFn();
        return;
      }

      if (hostID > 0) {
        closeModal('fleetSearchModal');
        const setActiveTab = g('setHostActiveTab');
        if (setActiveTab) setActiveTab(hostID, tab);
        const detail = g('openHostDetail');
        if (detail) await detail(hostID);
        const switchTabFn = g('switchTab');
        if (switchTabFn) switchTabFn(tab);
        return;
      }

      // Non-host entities: navigate to their workspace view.
      closeModal('fleetSearchModal');
      switch (r.category) {
        case 'kube':
          if (nav) nav('kube');
          break;
        case 'vpn':
          if (g('openSettingsModal')) g('openSettingsModal')('networks');
          break;
        case 'cf':
          if (nav) nav('cloudflare');
          break;
        case 'git':
          if (nav) nav('repos');
          break;
        case 'ansible':
          if (nav) nav('ansible');
          break;
        case 'tf':
          if (nav) nav('terraform');
          if (r.entityId && typeof openTfWorkspaceModal === 'function') openTfWorkspaceModal(parseInt(r.entityId, 10));
          break;
        case 'sshkey':
          if (g('openSettingsModal')) g('openSettingsModal')('keys');
          break;
        default:
          if (nav) nav('hosts');
      }
    } catch (e) {
      showToast(e.message || String(e), 'error');
    }
  }

  function setActive(idx) {
    if (!rows.length) return;
    S.active = Math.max(0, Math.min(rows.length - 1, idx));
    document.querySelectorAll('#fsResults [data-result-head]').forEach((el) => {
      el.classList.toggle('is-active', parseInt(el.dataset.resultHead, 10) === S.active);
    });
    const activeEl = document.querySelector('#fsResults [data-result-head="' + S.active + '"]');
    if (activeEl && activeEl.scrollIntoView) activeEl.scrollIntoView({ block: 'nearest' });
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
      const head = hostBtn.closest('[data-result]');
      const idx = head ? parseInt(head.dataset.result, 10) : -1;
      openResult(rows[idx] || {}, hostBtn);
      return;
    }
    const headBtn = e.target.closest('[data-result-head]');
    if (headBtn) {
      e.preventDefault();
      const idx = parseInt(headBtn.dataset.resultHead, 10);
      setActive(idx);
      openResult(rows[idx] || {});
    }
  });

  document.addEventListener('input', (e) => {
    if (!e.target || e.target.id !== 'fsQuery') return;
    S.query = e.target.value;
    scheduleSearch();
  });

  document.addEventListener('keydown', (e) => {
    const input = e.target;
    if (!input || input.id !== 'fsQuery') return;
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setActive(S.active + 1);
      return;
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault();
      setActive(S.active - 1);
      return;
    }
    if (e.key === 'Enter') {
      e.preventDefault();
      clearTimeout(S.timer);
      if (S.active >= 0 && rows[S.active]) {
        openResult(rows[S.active]);
      } else {
        S.query = input.value;
        search();
      }
    }
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
