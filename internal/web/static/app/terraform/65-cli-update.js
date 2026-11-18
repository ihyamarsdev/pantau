/* Gateway CLI Update Check for the Terraform Gate (ADR 0080, ADR 0081).
 *
 * Compares the terraform / tofu binaries installed on the Pantau server with the latest release
 * published upstream, and installs a newer one into the directory Pantau manages.
 */
(function () {
  'use strict';

  const D = {
    tfcli_title: ['CLI update check', 'Pemeriksaan pembaruan CLI'],
    tfcli_hint: ['Compares the installed terraform / tofu with the latest release published upstream. The check only reads; the update installs into the directory Pantau manages.', 'Membandingkan terraform / tofu yang terpasang dengan rilis terbaru yang dipublikasikan upstream. Pemeriksaan hanya membaca; pembaruan memasang ke direktori yang dikelola Pantau.'],
    tfcli_check: ['Check for updates', 'Periksa pembaruan'],
    tfcli_checking: ['Checking…', 'Memeriksa…'],
    tfcli_available: ['Update available', 'Pembaruan tersedia'],
    tfcli_current: ['Up to date', 'Terbaru'],
    tfcli_unknown: ['Version unknown', 'Versi tidak diketahui'],
    tfcli_error: ['Check failed', 'Pemeriksaan gagal'],
    tfcli_installed: ['Installed', 'Terpasang'],
    tfcli_latest: ['Latest', 'Terbaru'],
    tfcli_release: ['Release', 'Rilis'],
    tfcli_checked_at: ['Checked at', 'Diperiksa pada'],
    tfcli_update: ['Update now', 'Perbarui sekarang'],
    tfcli_install_failed: ['The update failed', 'Pembaruan gagal'],
    tfcli_install_hint: ['The update installs the new version into the directory Pantau manages and verifies it against the checksum the upstream publishes. The system copy of the CLI is left untouched.', 'Pembaruan memasang versi baru ke direktori yang dikelola Pantau dan memverifikasinya terhadap checksum yang dipublikasikan upstream. Salinan sistem CLI tidak disentuh.'],
  };
  for (const [k, v] of Object.entries(D)) { I18N.en[k] = v[0]; I18N.id[k] = v[1]; }
  // The result of the last check, one entry per binary found on the server.
  let results = [];
  let busy = false;

  function statusLabel(status) {
    if (status === 'update') return tx('tfcli_available');
    if (status === 'current') return tx('tfcli_current');
    if (status === 'error') return tx('tfcli_error');
    return tx('tfcli_unknown');
  }

  function statusClass(status) {
    if (status === 'update') return 'badge-warning';
    if (status === 'current') return 'badge-healthy';
    return 'badge-danger';
  }

  function resultHtml(r) {
    const badge = `<span class="badge ${statusClass(r.status)}">${tfE(statusLabel(r.status))}</span>`;
    const versions = `<div class="tf-cli-kv"><span>${tfE(tx('tfcli_installed'))}</span><span class="tf-mono">${tfE(r.installed || '—')}</span></span></div>` +
      `<div class="tf-cli-kv"><span>${tfE(tx('tfcli_latest'))}</span><span class="tf-mono">${tfE(r.latest || '—')}</span></div>`;
    const link = r.release_url
      ? `<div class="tf-cli-kv"><span>${tfE(tx('tfcli_release'))}</span><span><a href="${tfE(r.release_url)}" target="_blank" rel="noopener noreferrer">${tfE(r.release_url)}</a></span></div>`
      : '';
    const err = r.error ? `<div class="tf-cli-err">${svgIcon('triangle-alert', 'icon-xs')} ${tfE(r.error)}</div>` : '';
    const at = r.checked_at ? `<div class="tf-cli-kv"><span>${tfE(tx('tfcli_checked_at'))}</span><span class="tf-mono">${tfE(tfTime(r.checked_at))}</span></div>` : '';
    // A binary with a newer release upstream can be updated right here.
    const update = r.status === 'update'
      ? `<button type="button" class="btn btn-sm btn-primary tf-cli-update" data-target="${tfE(r.target)}">${tfE(tx('tfcli_update'))}</button>`
      : '';
    return `<div class="tf-cli-row"><div class="tf-cli-name">${tfE(r.target)}</div>${badge}${update}</div>${versions}${link}${at}${err}`;
  }

  // Installs one binary into the directory Pantau manages (ADR 0081). The install log is streamed,
  // so the steps show while they run.
  async function install(target, out) {
    out.hidden = false;
    out.textContent = '';
    const res = await fetch('/api/terraform/install', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ target: target }),
      credentials: 'same-origin',
    });
    if (!(res.headers.get('content-type') || '').includes('application/x-ndjson')) {
      const d = await res.json().catch(function () { return {}; });
      out.textContent = d.error || tx('tfcli_install_failed');
      return false;
    }
    const reader = res.body.getReader(), dec = new TextDecoder();
    let buf = '', error = '';
    for (;;) {
      const r = await reader.read();
      if (r.done) break;
      buf += dec.decode(r.value, { stream: true });
      let nl;
      while ((nl = buf.indexOf('\n')) >= 0) {
        let m;
        try { m = JSON.parse(buf.slice(0, nl)); } catch (e) { buf = buf.slice(nl + 1); continue; }
        buf = buf.slice(nl + 1);
        if (m.log) { out.textContent += m.log; out.scrollTop = out.scrollHeight; }
        if (m.done) error = m.error || '';
      }
    }
    if (buf.trim()) {
      try {
        const m = JSON.parse(buf);
        if (m.log) out.textContent += m.log;
        if (m.done) error = m.error || '';
      } catch (e) { /* a partial line carries nothing usable */ }
    }
    if (error) { out.textContent += '\n' + error; return false; }
    return true;
  }

  async function check() {
    if (busy) return;
    busy = true;
    paint();
    try {
      const r = await tfApi('GET', '/api/terraform/update-check');
      if (r.ok) {
        results = r.data.results || [];
      } else {
        // A failed request is reported as a failed check, never as "up to date".
        results = [{ target: 'terraform', status: 'error', error: r.data.error || 'Request failed' }];
      }
    } finally {
      busy = false;
      paint();
    }
  }

  function paint() {
    const el = tfEl('tfCliUpdateBody');
    if (!el) return;
    if (busy) {
      el.innerHTML = `<div class="tf-cli-msg"><span class="spinner">${svgIcon('loader-circle', 'icon-xs')}</span> ${tfE(tx('tfcli_checking'))}</div>`;
      return;
    }
    if (!results.length) { el.innerHTML = ''; return; }
    el.innerHTML = results.map(resultHtml).join('');
  }

  function mount(el) {
    if (!el) return;
    // The Gate tab extras replace the container's content on every render, so the card is added
    // back when it is gone and the stored result is painted into it again.
    if (!tfEl('tfCliUpdateCard')) {
      el.insertAdjacentHTML('beforeend', `<div class="card tf-cli-card" id="tfCliUpdateCard">
        <div class="card-title">${tfE(tx('tfcli_title'))}</div>
        <div class="tf-nf-hint">${tfE(tx('tfcli_hint'))}</div>
        <div class="tf-set-actions">
          <button type="button" class="btn btn-sm btn-primary" id="tfCliUpdateCheck">${tfE(tx('tfcli_check'))}</button>
        </div>
        <div id="tfCliUpdateBody"></div>
        <pre class="tf-cli-log" id="tfCliUpdateLog" hidden></pre></div>`);
      const btn = tfEl('tfCliUpdateCheck');
      if (btn) btn.addEventListener('click', check);
      const body = tfEl('tfCliUpdateBody');
      if (body) {
        body.addEventListener('click', async function (e) {
          const b = e.target.closest('[data-target]');
          if (!b) return;
          const target = b.getAttribute('data-target');
          const out = tfEl('tfCliUpdateLog');
          if (!out) return;
          b.disabled = true;
          try {
            await install(target, out);
            await check();
          } finally {
            b.disabled = false;
          }
        });
      }
    }
    paint();
  }

  // The Gate tab renders its extras through tfGateExtras; this card joins them.
  const tfGateExtrasBase = tfGateExtras;
  tfGateExtras = function (el) {
    const p = tfGateExtrasBase(el);
    if (el) mount(el);
    return p;
  };
})();
