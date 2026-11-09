/* Docker Hub browser and image update check for the host Docker tab.
 * Search, repository README and tags come from /api/dockerhub/* (fetched by the Pantau server, no credentials);
 * Pull and Run act on the host open in the Workspace Modal through the existing Docker endpoints.
 * Relies on index.html globals: currentHost, currentTab, dockerSubTab, loadDockerTab, dockerExecute,
 * openDockerRunForm, openModal, closeModal, showToast, svgIcon, escapeHtml, formatBytes, formatRelativeTime, t. */
(function () {
  'use strict';

  const Hub = (window.Hub = {});
  const S = { q: '', page: 1, search: null, searching: false, error: '', view: 'search',
    repo: null, repoName: '', tags: null, tagFilter: '', tagsLoading: false, pane: 'tags', checking: false };
  const updates = {}; // hostID -> { at, byRef }
  let filterTimer = null;

  const esc = (s) => escapeHtml(String(s === null || s === undefined ? '' : s));
  const icon = (n) => svgIcon(n, 'icon-sm');
  const num = (n) => new Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: 1 }).format(n || 0);
  const ago = (iso) => (iso ? formatRelativeTime(iso).relative : '-');
  const fill = (s, vars) => Object.keys(vars).reduce((a, k) => a.split('{' + k + '}').join(vars[k]), s);

  /* ---------- i18n ---------- */
  const I = {
    en: {
      hub_title: 'Docker Hub', hub_browse: 'Docker Hub', hub_check: 'Check updates', hub_checking_btn: 'Checking…',
      hub_target: 'pull target: {host}',
      hub_search_ph: 'Search Docker Hub, e.g. nginx, postgres, bitnami/redis', hub_search: 'Search',
      hub_note: 'Docker Hub is queried from the Pantau server (public data, no login). Pull and Run happen on {host}.',
      hub_start: 'Search public images on Docker Hub, then pull a tag to this host or start a container from it.',
      hub_no_results: 'No repositories match "{q}".', hub_page: 'Page {p} of {n}', hub_prev: 'Previous', hub_next: 'Next',
      hub_official: 'Official', hub_automated: 'Automated', hub_pulls: '{n} pulls', hub_stars: '{n} stars',
      hub_updated: 'updated {t}', hub_details: 'Details', hub_pull_latest: 'Pull latest', hub_back: 'Back to results',
      hub_open_site: 'Open on Docker Hub', hub_tags: 'Tags', hub_overview: 'Overview', hub_filter_ph: 'Filter tags, e.g. alpine',
      hub_col_tag: 'Tag', hub_col_platforms: 'Platforms', hub_col_size: 'Size', hub_col_pushed: 'Pushed',
      hub_no_tags: 'No tags match.', hub_more: 'Load more tags', hub_tag_count: '{n} tags',
      hub_pull: 'Pull', hub_run: 'Run', hub_no_readme: 'This repository has no overview.',
      hub_readme_cut: 'The overview is long and was cut; open it on Docker Hub for the rest.',
      hub_loading: 'Loading…', hub_pulled_note: 'Pulled {ref}. Running containers keep the old image until they are recreated (for Compose: Stacks → Update).',
      hub_checking: 'Checking local images against Docker Hub…', hub_check_failed: 'Update check failed',
      hub_updates_found: '{n} image(s) have a newer version on Docker Hub', hub_all_current: 'All Docker Hub images are up to date',
      hub_no_hub_images: 'No images from Docker Hub on this host', hub_check_errors: '{n} image(s) could not be checked: {why}',
      hub_summary: ' · {n} update(s) available', hub_summary_checked: ' · checked against Docker Hub {t}',
      hub_st_update_available: 'Update available', hub_st_up_to_date: 'Up to date', hub_st_not_found: 'Not on Docker Hub',
      hub_st_unknown: 'Not comparable', hub_st_error: 'Check failed',
      hub_st_update_available_tip: 'Docker Hub serves a different image for this tag (pushed {t}).',
      hub_pull_update: 'Pull update', hub_view_repo: 'View on Docker Hub'
    },
    id: {
      hub_title: 'Docker Hub', hub_browse: 'Docker Hub', hub_check: 'Cek pembaruan', hub_checking_btn: 'Memeriksa…',
      hub_target: 'target pull: {host}',
      hub_search_ph: 'Cari di Docker Hub, contoh nginx, postgres, bitnami/redis', hub_search: 'Cari',
      hub_note: 'Docker Hub dihubungi dari server Pantau (data publik, tanpa login). Pull dan Run berjalan di {host}.',
      hub_start: 'Cari image publik di Docker Hub, lalu pull sebuah tag ke host ini atau jalankan container darinya.',
      hub_no_results: 'Tidak ada repositori yang cocok dengan "{q}".', hub_page: 'Halaman {p} dari {n}', hub_prev: 'Sebelumnya', hub_next: 'Berikutnya',
      hub_official: 'Resmi', hub_automated: 'Otomatis', hub_pulls: '{n} pull', hub_stars: '{n} bintang',
      hub_updated: 'diperbarui {t}', hub_details: 'Detail', hub_pull_latest: 'Pull latest', hub_back: 'Kembali ke hasil',
      hub_open_site: 'Buka di Docker Hub', hub_tags: 'Tag', hub_overview: 'Ringkasan', hub_filter_ph: 'Saring tag, contoh alpine',
      hub_col_tag: 'Tag', hub_col_platforms: 'Platform', hub_col_size: 'Ukuran', hub_col_pushed: 'Di-push',
      hub_no_tags: 'Tidak ada tag yang cocok.', hub_more: 'Muat tag lainnya', hub_tag_count: '{n} tag',
      hub_pull: 'Pull', hub_run: 'Jalankan', hub_no_readme: 'Repositori ini tidak punya ringkasan.',
      hub_readme_cut: 'Ringkasan terlalu panjang dan dipotong; buka di Docker Hub untuk sisanya.',
      hub_loading: 'Memuat…', hub_pulled_note: '{ref} sudah di-pull. Container yang berjalan tetap memakai image lama sampai dibuat ulang (untuk Compose: Stacks → Update).',
      hub_checking: 'Membandingkan image lokal dengan Docker Hub…', hub_check_failed: 'Cek pembaruan gagal',
      hub_updates_found: '{n} image punya versi lebih baru di Docker Hub', hub_all_current: 'Semua image dari Docker Hub sudah terbaru',
      hub_no_hub_images: 'Tidak ada image dari Docker Hub di host ini', hub_check_errors: '{n} image tidak dapat diperiksa: {why}',
      hub_summary: ' · {n} pembaruan tersedia', hub_summary_checked: ' · dicek ke Docker Hub {t}',
      hub_st_update_available: 'Ada pembaruan', hub_st_up_to_date: 'Terbaru', hub_st_not_found: 'Tidak ada di Docker Hub',
      hub_st_unknown: 'Tidak dapat dibandingkan', hub_st_error: 'Cek gagal',
      hub_st_update_available_tip: 'Docker Hub menyajikan image lain untuk tag ini (di-push {t}).',
      hub_pull_update: 'Pull pembaruan', hub_view_repo: 'Lihat di Docker Hub'
    }
  };
  ['en', 'id'].forEach((l) => { I18N[l] = Object.assign(I18N[l] || {}, I[l]); });

  async function api(url) {
    try {
      const res = await fetch(url);
      const data = await res.json().catch(() => ({}));
      return res.ok ? { ok: true, data } : { ok: false, error: data.error || ('HTTP ' + res.status) };
    } catch (e) {
      return { ok: false, error: e.message };
    }
  }

  // Docker Hub namespace/name of a local image name, or null for other registries and digest references.
  function hubRepo(ref) {
    if (!ref || ref.includes('@')) return null;
    let repo = ref;
    const i = ref.lastIndexOf(':');
    if (i > ref.lastIndexOf('/')) repo = ref.slice(0, i);
    let p = repo.split('/');
    if (p.length > 1 && (/[.:]/.test(p[0]) || p[0] === 'localhost')) {
      if (!['docker.io', 'index.docker.io', 'registry-1.docker.io'].includes(p[0])) return null;
      p = p.slice(1);
    }
    if (p.length === 1) p = ['library', p[0]];
    return p.length === 2 ? p.join('/') : null;
  }

  /* ---------- modal ---------- */
  function ensureModal() {
    if (document.getElementById('dockerHubModal')) return;
    const m = document.createElement('div');
    m.id = 'dockerHubModal';
    m.className = 'modal';
    m.innerHTML = `
      <div class="modal-content modal-workspace">
        <div class="modal-header">
          <div class="sd-title">
            <svg class="icon icon-sm text-accent"><use href="#i-container"/></svg>
            <h3>${esc(t('hub_title'))}</h3>
            <span id="dockerHubHost" class="badge badge-info" style="text-transform: none;"></span>
          </div>
          <button class="btn btn-icon btn-sm btn-ghost" onclick="closeModal('dockerHubModal')">${icon('x')}</button>
        </div>
        <div class="modal-body" id="dockerHubBody"></div>
      </div>`;
    document.body.appendChild(m);
  }

  function render() {
    const body = document.getElementById('dockerHubBody');
    if (!body) return;
    const host = document.getElementById('dockerHubHost');
    if (host) host.textContent = fill(t('hub_target'), { host: currentHost ? currentHost.name : '-' });
    body.innerHTML = S.view === 'repo' ? repoHtml() : searchHtml();
  }

  Hub.open = function (query) {
    if (!currentHost) return;
    ensureModal();
    if (query !== undefined) Object.assign(S, { q: query, view: 'search' });
    render();
    openModal('dockerHubModal');
    if (S.view === 'search') setTimeout(() => { const el = document.getElementById('hubQuery'); if (el) el.focus(); }, 50);
    if (S.view === 'search' && S.q && !S.search) Hub.search(1);
  };

  /* ---------- search ---------- */
  function resultHtml(r) {
    const repo = r.namespace + '/' + r.repository;
    return `
      <div class="hub-result" data-repo="${esc(repo)}" onclick="Hub.openRepo(this.dataset.repo)">
        <div class="hub-result-main">
          <div class="hub-result-title">${esc(r.name)}
            ${r.official ? `<span class="badge badge-success">${esc(t('hub_official'))}</span>` : ''}
            ${r.automated ? `<span class="badge badge-unknown">${esc(t('hub_automated'))}</span>` : ''}
          </div>
          ${r.description ? `<div class="hub-desc">${esc(r.description)}</div>` : ''}
          <div class="hub-meta"><span>${icon('star')} ${esc(fill(t('hub_stars'), { n: num(r.stars) }))}</span><span>${icon('download')} ${esc(fill(t('hub_pulls'), { n: num(r.pulls) }))}</span></div>
        </div>
        <div class="hub-actions">
          <button class="btn btn-sm" data-ref="${esc(r.name + ':latest')}" onclick="event.stopPropagation(); Hub.pull(this.dataset.ref)">${icon('download')} <span>${esc(t('hub_pull_latest'))}</span></button>
          <button class="btn btn-sm btn-primary" data-repo="${esc(repo)}" onclick="event.stopPropagation(); Hub.openRepo(this.dataset.repo)"><span>${esc(t('hub_details'))}</span></button>
        </div>
      </div>`;
  }

  function searchHtml() {
    const bar = `
      <div class="hub-bar">
        <input type="search" id="hubQuery" value="${esc(S.q)}" placeholder="${esc(t('hub_search_ph'))}" maxlength="100" autocomplete="off" spellcheck="false" onkeydown="if (event.key === 'Enter') Hub.search(1)" />
        <button class="btn btn-sm btn-primary" onclick="Hub.search(1)" ${S.searching ? 'disabled' : ''}>${icon('search')} <span>${esc(t('hub_search'))}</span></button>
      </div>
      <div class="hub-note">${esc(fill(t('hub_note'), { host: currentHost ? currentHost.name : '-' }))}</div>`;
    if (S.error) return bar + `<div class="hub-err">${esc(S.error)}</div>`;
    if (S.searching) return bar + `<div class="hub-empty">${esc(t('hub_loading'))}</div>`;
    const r = S.search;
    if (!r) return bar + `<div class="hub-empty">${esc(t('hub_start'))}</div>`;
    if (!r.results.length) return bar + `<div class="hub-empty">${esc(fill(t('hub_no_results'), { q: S.q }))}</div>`;
    const pages = Math.min(r.num_pages || 1, 100);
    const pager = pages > 1 ? `
      <div class="hub-pager">
        <button class="btn btn-sm btn-ghost" ${S.page <= 1 ? 'disabled' : ''} onclick="Hub.search(${S.page - 1})">${icon('chevron-left')} <span>${esc(t('hub_prev'))}</span></button>
        <span>${esc(fill(t('hub_page'), { p: S.page, n: pages }))}</span>
        <button class="btn btn-sm btn-ghost" ${S.page >= pages ? 'disabled' : ''} onclick="Hub.search(${S.page + 1})"><span>${esc(t('hub_next'))}</span> ${icon('chevron-right')}</button>
      </div>` : '';
    return bar + `<div class="hub-results">${r.results.map(resultHtml).join('')}</div>` + pager;
  }

  Hub.search = async function (page) {
    const el = document.getElementById('hubQuery');
    const q = (el ? el.value : S.q).trim();
    if (!q) return;
    Object.assign(S, { q, page, searching: true, error: '', view: 'search' });
    render();
    const r = await api('/api/dockerhub/search?q=' + encodeURIComponent(q) + '&page=' + page);
    if (S.q !== q || S.page !== page) return; // a newer search replaced this one
    S.searching = false;
    if (r.ok) S.search = r.data; else S.error = r.error;
    render();
  };

  /* ---------- repository ---------- */
  function tagRowHtml(tag) {
    const ref = S.repo.name + ':' + tag.name;
    const plats = tag.platforms || [];
    const shown = plats.slice(0, 4).map((p) => `<span class="badge badge-info">${esc(p)}</span>`).join('');
    const more = plats.length > 4 ? `<span class="badge badge-unknown" title="${esc(plats.slice(4).join(', '))}">+${plats.length - 4}</span>` : '';
    return `
      <tr>
        <td class="dk-mono" style="font-weight: 600; color: var(--text);" title="${esc(tag.digest)}">${esc(tag.name)}</td>
        <td><div class="hub-platforms">${shown}${more}</div></td>
        <td style="white-space: nowrap;">${tag.size ? formatBytes(tag.size) : '-'}</td>
        <td class="text-muted" style="white-space: nowrap;" title="${esc(tag.last_pushed)}">${esc(ago(tag.last_pushed))}</td>
        <td class="dk-actions">
          <button class="btn btn-sm" data-ref="${esc(ref)}" onclick="Hub.pull(this.dataset.ref)">${icon('download')} <span>${esc(t('hub_pull'))}</span></button>
          <button class="btn btn-sm btn-ghost" data-ref="${esc(ref)}" onclick="Hub.run(this.dataset.ref)">${icon('play')} <span>${esc(t('hub_run'))}</span></button>
        </td>
      </tr>`;
  }

  function tagsHtml() {
    const filter = `<div class="hub-bar"><input type="search" id="hubTagFilter" value="${esc(S.tagFilter)}" placeholder="${esc(t('hub_filter_ph'))}" maxlength="128" autocomplete="off" spellcheck="false" oninput="Hub.filterTags(this.value)" /></div>`;
    const tg = S.tags;
    if (!tg) return filter + `<div class="hub-empty">${esc(S.tagsLoading ? t('hub_loading') : '')}</div>`;
    if (tg.error) return filter + `<div class="hub-err">${esc(tg.error)}</div>`;
    if (!tg.tags.length) return filter + `<div class="hub-empty">${esc(t('hub_no_tags'))}</div>`;
    const heads = [t('hub_col_tag'), t('hub_col_platforms'), t('hub_col_size'), t('hub_col_pushed'), ''];
    const more = tg.has_next ? `<div class="hub-pager"><button class="btn btn-sm btn-ghost" ${S.tagsLoading ? 'disabled' : ''} onclick="Hub.loadTags(${tg.page + 1})">${esc(S.tagsLoading ? t('hub_loading') : t('hub_more'))}</button></div>` : '';
    return filter + `<div style="overflow-x: auto;"><table class="hub-tags"><thead><tr>${heads.map((h) => `<th>${esc(h)}</th>`).join('')}</tr></thead><tbody>${tg.tags.map(tagRowHtml).join('')}</tbody></table></div>` + more;
  }

  function readmeHtml() {
    const r = S.repo;
    if (!r.readme) return `<div class="hub-empty">${esc(t('hub_no_readme'))}</div>`;
    const md = window.TfUI && TfUI.reg && TfUI.reg.md; // the safe README renderer of the Terraform Registry
    const body = md ? `<div class="tf-md">${md(r.readme)}</div>` : `<pre class="tf-reg-pre">${esc(r.readme)}</pre>`;
    return body + (r.readme_truncated ? `<div class="hub-note">${esc(t('hub_readme_cut'))}</div>` : '');
  }

  function repoHtml() {
    const back = `<div><button class="btn btn-sm btn-ghost" onclick="Hub.back()">${icon('chevron-left')} <span>${esc(t('hub_back'))}</span></button></div>`;
    if (S.error) return back + `<div class="hub-err">${esc(S.error)}</div>`;
    const r = S.repo;
    if (!r) return back + `<div class="hub-empty">${esc(t('hub_loading'))}</div>`;
    const count = S.tags && !S.tags.error ? ` (${num(S.tags.count)})` : '';
    const pane = (id, label) => `<button class="btn btn-sm ${S.pane === id ? 'btn-primary' : 'btn-ghost'}" style="padding: 4px 10px; font-size: var(--fs-xs);" onclick="Hub.pane('${id}')">${esc(label)}</button>`;
    return back + `
      <div class="hub-head">
        <div class="hub-result-main">
          <div class="hub-result-title" style="font-size: var(--fs-md);">${esc(r.name)}
            ${r.official ? `<span class="badge badge-success">${esc(t('hub_official'))}</span>` : ''}
          </div>
          ${r.description ? `<div class="hub-desc">${esc(r.description)}</div>` : ''}
          <div class="hub-meta">
            <span>${icon('star')} ${esc(fill(t('hub_stars'), { n: num(r.stars) }))}</span>
            <span>${icon('download')} ${esc(fill(t('hub_pulls'), { n: num(r.pulls) }))}</span>
            <span>${icon('clock')} ${esc(fill(t('hub_updated'), { t: ago(r.last_updated) }))}</span>
          </div>
        </div>
        <a class="btn btn-sm btn-ghost" href="${esc(r.url)}" target="_blank" rel="noopener noreferrer">${icon('external-link')} <span>${esc(t('hub_open_site'))}</span></a>
      </div>
      <div><div class="hub-panes">${pane('tags', t('hub_tags') + count)}${pane('readme', t('hub_overview'))}</div></div>
      <div>${S.pane === 'tags' ? tagsHtml() : readmeHtml()}</div>`;
  }

  Hub.openRepo = async function (repo) {
    if (!repo || !currentHost) return;
    ensureModal();
    Object.assign(S, { view: 'repo', repoName: repo, repo: null, tags: null, tagFilter: '', pane: 'tags', error: '' });
    render();
    openModal('dockerHubModal');
    const [ns, name] = repo.split('/');
    const path = encodeURIComponent(ns) + '/' + encodeURIComponent(name);
    const [r] = await Promise.all([api('/api/dockerhub/repo/' + path), Hub.loadTags(1)]);
    if (S.repoName !== repo) return;
    if (r.ok) S.repo = r.data; else S.error = r.error;
    render();
  };

  Hub.loadTags = async function (page) {
    const repo = S.repoName, filter = S.tagFilter;
    const [ns, name] = repo.split('/');
    S.tagsLoading = true;
    if (page === 1) S.tags = null;
    if (S.repo) render();
    const r = await api('/api/dockerhub/tags/' + encodeURIComponent(ns) + '/' + encodeURIComponent(name) + '?page=' + page + '&name=' + encodeURIComponent(filter));
    if (S.repoName !== repo || S.tagFilter !== filter) return; // superseded
    S.tagsLoading = false;
    if (!r.ok) S.tags = { error: r.error, tags: [] };
    else if (page > 1 && S.tags && !S.tags.error) S.tags = Object.assign({}, r.data, { tags: S.tags.tags.concat(r.data.tags), count: S.tags.count || r.data.count });
    else S.tags = r.data;
    if (S.repo) render();
  };

  Hub.filterTags = function (value) {
    S.tagFilter = value.trim();
    clearTimeout(filterTimer);
    filterTimer = setTimeout(async () => {
      await Hub.loadTags(1);
      const el = document.getElementById('hubTagFilter');
      if (el) { el.focus(); el.setSelectionRange(el.value.length, el.value.length); }
    }, 350);
  };

  Hub.pane = function (id) { S.pane = id; render(); };
  Hub.back = function () { Object.assign(S, { view: 'search', repo: null, repoName: '', tags: null, error: '' }); render(); };

  /* ---------- actions on the host ---------- */
  Hub.pull = async function (ref) {
    const host = currentHost;
    if (!host || !ref) return;
    const data = await dockerExecute({ sub: 'image', long: true, payload: { action: 'pull', image: ref }, outputTitle: t('dk_pull_image') + ': ' + ref });
    if (!data.ok) return;
    const u = updates[host.id] && updates[host.id].byRef[ref];
    if (!u || u.status !== 'update_available') return;
    Object.assign(u, { status: 'up_to_date', detail: '' });
    showToast(fill(t('hub_pulled_note'), { ref }), 'info', 7000);
  };

  Hub.run = function (ref) {
    closeModal('dockerHubModal');
    openDockerRunForm(ref);
  };

  /* ---------- image update check (Images sub-tab) ---------- */
  const imagesTabOf = (hostID) => currentTab === 'docker' && dockerSubTab === 'images' && currentHost && currentHost.id === hostID;
  const checkBtnHtml = () => `${icon(S.checking ? 'loader-circle' : 'refresh-cw')} <span>${esc(t(S.checking ? 'hub_checking_btn' : 'hub_check'))}</span>`;
  function setChecking(on) {
    S.checking = on;
    const b = document.getElementById('hubCheckBtn');
    if (b) { b.disabled = on; b.innerHTML = checkBtnHtml(); }
  }

  Hub.checkUpdates = async function () {
    const host = currentHost;
    if (!host || S.checking) return;
    setChecking(true);
    showToast(t('hub_checking'), 'info', 2500);
    const r = await api('/api/hosts/' + host.id + '/docker/images/updates');
    setChecking(false);
    if (!r.ok || !r.data.ok) {
      showToast(t('hub_check_failed') + ': ' + (r.error || r.data.error), 'error', 9000);
      return;
    }
    const list = r.data.updates || [];
    const byRef = {};
    list.forEach((u) => { byRef[u.ref] = u; });
    updates[host.id] = { at: r.data.checked_at, byRef };
    const avail = list.filter((u) => u.status === 'update_available').length;
    const failed = list.filter((u) => u.status === 'error');
    if (!list.length) showToast(t('hub_no_hub_images'), 'info');
    else if (failed.length) showToast(fill(t('hub_check_errors'), { n: failed.length, why: failed[0].detail }), 'warning', 7000);
    else showToast(avail ? fill(t('hub_updates_found'), { n: avail }) : t('hub_all_current'), avail ? 'warning' : 'success');
    if (imagesTabOf(host.id)) loadDockerTab();
  };

  Hub.toolbar = function () {
    return `<button class="btn btn-sm btn-ghost" id="hubCheckBtn" onclick="Hub.checkUpdates()" ${S.checking ? 'disabled' : ''}>${checkBtnHtml()}</button>` +
      `<button class="btn btn-sm btn-ghost" onclick="Hub.open()">${icon('search')} <span>${esc(t('hub_browse'))}</span></button>`;
  };

  const updateOf = (ref) => (currentHost && updates[currentHost.id] ? updates[currentHost.id].byRef[ref] : null);

  Hub.imageBadge = function (ref) {
    const u = updateOf(ref);
    if (!u) return '';
    const cls = { update_available: 'badge-warning', up_to_date: 'badge-success', error: 'badge-danger' }[u.status] || 'badge-unknown';
    const tip = u.status === 'update_available' ? fill(t('hub_st_update_available_tip'), { t: ago(u.remote_pushed) }) : (u.detail || '');
    return `<span class="badge ${cls} hub-img-badge" title="${esc(tip)}">${esc(t('hub_st_' + u.status, u.status))}</span>`;
  };

  Hub.imageActions = function (ref) {
    const repo = hubRepo(ref);
    if (!repo) return '';
    const u = updateOf(ref);
    const pull = u && u.status === 'update_available'
      ? `<button class="btn btn-sm btn-ghost btn-icon" title="${esc(t('hub_pull_update'))}" data-ref="${esc(ref)}" onclick="Hub.pull(this.dataset.ref)">${icon('download')}</button>` : '';
    return pull + `<button class="btn btn-sm btn-ghost btn-icon" title="${esc(t('hub_view_repo'))}" data-repo="${esc(repo)}" onclick="Hub.openRepo(this.dataset.repo)">${icon('external-link')}</button>`;
  };

  Hub.summary = function () {
    const c = currentHost && updates[currentHost.id];
    if (!c) return '';
    const n = Object.values(c.byRef).filter((u) => u.status === 'update_available').length;
    return esc(n ? fill(t('hub_summary'), { n }) : fill(t('hub_summary_checked'), { t: ago(c.at) }));
  };
})();
