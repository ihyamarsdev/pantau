/* Host firewall (ufw / firewalld / iptables) card of the Security tab.
 * Rules are read live over SSH on demand (never on tab open) and cached per host until the host changes. */
(function () {
  'use strict';

  const Fw = (window.Fw = {});
  const S = { hostID: 0, data: null, tab: 'ufw', loading: false, busy: false, error: '' };
  let dlg = null; // { backend, old } - old is the listed rule being edited, null when adding

  const esc = (s) => escapeHtml(String(s === null || s === undefined ? '' : s));
  const icon = (n) => svgIcon(n, 'icon-sm');

  /* ---------- i18n ---------- */
  const I = {
    en: {
      fw_title: 'Firewall', fw_load: 'Load firewall',
      fw_desc: 'Read and manage ufw, firewalld and iptables on this host over SSH. Needs root or passwordless sudo.',
      fw_unsupported: 'Firewall management supports Linux hosts only.',
      fw_mode_title: 'How commands run on the host',
      fw_no_priv: 'No root and no passwordless sudo for this SSH user: reading rules and every change will most likely be refused.',
      fw_no_priv_short: 'no root or passwordless sudo on the host',
      fw_conflict: 'ufw and firewalld are both active. They manage the same kernel rules and can conflict; keep only one.',
      fw_not_installed: 'not installed', fw_active: 'active', fw_inactive: 'inactive',
      fw_install_setup: 'Install & set up', fw_setup: 'Set up', fw_enable: 'Enable', fw_disable: 'Disable',
      fw_save_disk: 'Save to disk', fw_add_rule: 'Add rule', fw_edit_rule: 'Edit rule', fw_no_rules: 'No rules.',
      fw_readonly: 'Uses options the edit form cannot express: it can be deleted but not edited here.',
      fw_iptables_note: 'iptables changes take effect immediately but are lost on reboot unless saved: use Save to disk.',
      fw_ufw_inactive_note: 'ufw lists its rules only while it is enabled.',
      fw_ufw_v6_note: 'IPv4 and IPv6 twins are separate rows: adding or editing a rule creates both, but the old (v6) row must be deleted on its own.',
      fw_col_rule: 'Rule', fw_any: 'any',
      fw_f_action: 'Action', fw_f_direction: 'Direction', fw_f_proto: 'Protocol', fw_f_port: 'Port', fw_f_source: 'Source',
      fw_f_dest: 'Destination', fw_f_in_iface: 'In interface', fw_f_out_iface: 'Out interface', fw_f_zone: 'Zone',
      fw_f_kind: 'Type', fw_f_value: 'Value', fw_f_chain: 'Chain', fw_f_position: 'Position', fw_pos_ph: 'end',
      fw_hint_position: 'empty = append; 1 = top of the chain',
      fw_hint_value: 'service name, port/protocol, or a full rich rule',
      fw_act_setup: 'Set up', fw_act_enable: 'Enable', fw_act_disable: 'Disable', fw_act_add: 'Add rule',
      fw_act_edit: 'Edit rule', fw_act_delete: 'Delete rule', fw_act_persist: 'Save to disk',
      fw_act_add_zone: 'Add zone',
      fw_add_zone: 'Add zone', fw_new_zone_opt: '+ New zone…',
      fw_zone_name: 'Zone name', fw_zone_name_ph: 'e.g. dmz (1-17 chars)',
      fw_f_family: 'Family', fw_f_element: 'Element', fw_f_elem_none: 'none (all traffic)',
      fw_f_elem_service: 'service', fw_f_elem_port: 'port', fw_f_service: 'Service',
      fw_f_log: 'Log', fw_f_log_label: 'Log packets',
      fw_mode_form: 'Form', fw_mode_raw: 'Raw rule',
      fw_hint_zone_name: '1-17 characters: alphanumeric, underscore, dot, hyphen',
      fw_err_zone_name: 'Zone name must be 1-17 characters (alphanumeric, underscore, dot, or hyphen).',
      fw_err_service_name: 'Service name is invalid.',
      fw_err_port: 'Port must be 1-65535 or a range like 8000-8100.',
      fw_err_ip: 'Must be a valid IPv4/IPv6 address or CIDR.',
      fw_err_rich_req: 'A rich rule needs an element, a source, or a destination.',
      fw_action_ok: '{backend}: {action} done', fw_action_failed: '{backend}: {action} failed',
      fw_confirm_setup_title: 'Set up {backend}',
      fw_confirm_setup_ufw: 'Pantau will install ufw if it is missing, set the default policy to deny incoming / allow outgoing, allow {port}/tcp (the SSH port Pantau uses for {host}) and enable the firewall.',
      fw_confirm_setup_firewalld: 'Pantau will install firewalld if it is missing, start and enable it, and permanently allow {port}/tcp (the SSH port Pantau uses for {host}).',
      fw_confirm_setup_iptables: 'Pantau will install iptables if it is missing, accept loopback, established connections and {port}/tcp (SSH) on {host}, set the INPUT policy to DROP, and try to save the rules so they survive a reboot.',
      fw_confirm_enable_title: 'Enable {backend}',
      fw_confirm_enable_msg: 'Enable {backend} on {host}? Port {port}/tcp (SSH) is allowed first so Pantau keeps access.',
      fw_confirm_disable_title: 'Disable {backend}',
      fw_confirm_disable_msg: 'Disable {backend} on {host}? Its rules stop filtering traffic.',
      fw_confirm_delete_title: 'Delete rule',
      fw_confirm_delete_msg: 'Delete this {backend} rule on {host}?'
    },
    id: {
      fw_title: 'Firewall', fw_load: 'Muat firewall',
      fw_desc: 'Baca dan kelola ufw, firewalld, dan iptables pada host ini melalui SSH. Memerlukan root atau sudo tanpa kata sandi.',
      fw_unsupported: 'Pengelolaan firewall hanya mendukung host Linux.',
      fw_mode_title: 'Cara perintah dijalankan di host',
      fw_no_priv: 'Pengguna SSH ini tidak punya root maupun sudo tanpa kata sandi: pembacaan aturan dan setiap perubahan kemungkinan besar ditolak.',
      fw_no_priv_short: 'tidak ada root atau sudo tanpa kata sandi di host',
      fw_conflict: 'ufw dan firewalld sama-sama aktif. Keduanya mengelola aturan kernel yang sama dan dapat bentrok; pertahankan satu saja.',
      fw_not_installed: 'belum terpasang', fw_active: 'aktif', fw_inactive: 'nonaktif',
      fw_install_setup: 'Pasang & siapkan', fw_setup: 'Siapkan', fw_enable: 'Aktifkan', fw_disable: 'Nonaktifkan',
      fw_save_disk: 'Simpan ke disk', fw_add_rule: 'Tambah aturan', fw_edit_rule: 'Ubah aturan', fw_no_rules: 'Tidak ada aturan.',
      fw_readonly: 'Memakai opsi yang tidak dapat diungkapkan formulir ubah: hanya dapat dihapus, tidak diubah di sini.',
      fw_iptables_note: 'Perubahan iptables berlaku seketika tetapi hilang saat reboot kecuali disimpan: gunakan Simpan ke disk.',
      fw_ufw_inactive_note: 'ufw hanya menampilkan aturannya saat sedang diaktifkan.',
      fw_ufw_v6_note: 'Pasangan IPv4 dan IPv6 adalah baris terpisah: menambah/mengubah aturan membuat keduanya, tetapi baris (v6) lama harus dihapus sendiri.',
      fw_col_rule: 'Aturan', fw_any: 'semua',
      fw_f_action: 'Aksi', fw_f_direction: 'Arah', fw_f_proto: 'Protokol', fw_f_port: 'Port', fw_f_source: 'Sumber',
      fw_f_dest: 'Tujuan', fw_f_in_iface: 'Antarmuka masuk', fw_f_out_iface: 'Antarmuka keluar', fw_f_zone: 'Zona',
      fw_f_kind: 'Tipe', fw_f_value: 'Nilai', fw_f_chain: 'Chain', fw_f_position: 'Posisi', fw_pos_ph: 'akhir',
      fw_hint_position: 'kosong = tambahkan di akhir; 1 = paling atas chain',
      fw_hint_value: 'nama service, port/protokol, atau rich rule lengkap',
      fw_act_setup: 'Siapkan', fw_act_enable: 'Aktifkan', fw_act_disable: 'Nonaktifkan', fw_act_add: 'Tambah aturan',
      fw_act_edit: 'Ubah aturan', fw_act_delete: 'Hapus aturan', fw_act_persist: 'Simpan ke disk',
      fw_act_add_zone: 'Tambah zona',
      fw_add_zone: 'Tambah zona', fw_new_zone_opt: '+ Zona baru…',
      fw_zone_name: 'Nama zona', fw_zone_name_ph: 'contoh dmz (1-17 karakter)',
      fw_f_family: 'Famili', fw_f_element: 'Elemen', fw_f_elem_none: 'tidak ada (semua lalu lintas)',
      fw_f_elem_service: 'service', fw_f_elem_port: 'port', fw_f_service: 'Service',
      fw_f_log: 'Log', fw_f_log_label: 'Catat paket',
      fw_mode_form: 'Formulir', fw_mode_raw: 'Aturan mentah',
      fw_hint_zone_name: '1-17 karakter: alfanumerik, garis bawah, titik, tanda hubung',
      fw_err_zone_name: 'Nama zona harus 1-17 karakter (alfanumerik, garis bawah, titik, atau tanda hubung).',
      fw_err_service_name: 'Nama service tidak valid.',
      fw_err_port: 'Port harus 1-65535 atau rentang seperti 8000-8100.',
      fw_err_ip: 'Harus berupa alamat IPv4/IPv6 atau CIDR yang valid.',
      fw_err_rich_req: 'Rich rule memerlukan elemen, sumber, atau tujuan.',
      fw_action_ok: '{backend}: {action} selesai', fw_action_failed: '{backend}: {action} gagal',
      fw_confirm_setup_title: 'Siapkan {backend}',
      fw_confirm_setup_ufw: 'Pantau akan memasang ufw bila belum ada, menetapkan kebijakan bawaan tolak masuk / izinkan keluar, mengizinkan {port}/tcp (port SSH yang dipakai Pantau untuk {host}), lalu mengaktifkan firewall.',
      fw_confirm_setup_firewalld: 'Pantau akan memasang firewalld bila belum ada, menjalankan dan mengaktifkannya, serta mengizinkan {port}/tcp (port SSH yang dipakai Pantau untuk {host}) secara permanen.',
      fw_confirm_setup_iptables: 'Pantau akan memasang iptables bila belum ada, menerima loopback, koneksi yang sudah terbentuk, dan {port}/tcp (SSH) pada {host}, menetapkan kebijakan INPUT ke DROP, lalu mencoba menyimpan aturan agar bertahan setelah reboot.',
      fw_confirm_enable_title: 'Aktifkan {backend}',
      fw_confirm_enable_msg: 'Aktifkan {backend} pada {host}? Port {port}/tcp (SSH) diizinkan lebih dulu agar Pantau tetap punya akses.',
      fw_confirm_disable_title: 'Nonaktifkan {backend}',
      fw_confirm_disable_msg: 'Nonaktifkan {backend} pada {host}? Aturannya berhenti menyaring lalu lintas.',
      fw_confirm_delete_title: 'Hapus aturan',
      fw_confirm_delete_msg: 'Hapus aturan {backend} ini pada {host}?'
    }
  };
  ['en', 'id'].forEach((l) => { I18N[l] = Object.assign(I18N[l] || {}, I[l]); });

  const box = (kind, html) => `<div style="background: var(--${kind}-soft); border: 1px solid var(--${kind}-border); border-radius: var(--radius); padding: 10px 12px; color: var(--${kind === 'danger' ? 'danger' : 'text'}); font-size: 0.82rem; margin-bottom: 12px; white-space: pre-wrap; overflow-wrap: anywhere;">${html}</div>`;
  const backend = (id) => S.data && S.data.backends.find((b) => b.id === id);

  /* ---------- card ---------- */
  Fw.html = () => '<div class="card" id="secFirewallCard" style="padding: 16px; margin-top: 16px;"></div>';

  Fw.render = function () {
    const el = document.getElementById('secFirewallCard');
    const host = currentHost;
    if (!el || !host) return;
    if (S.hostID !== host.id) Object.assign(S, { hostID: host.id, data: null, error: '', loading: false, busy: false });
    const d = S.data;
    const working = S.loading || S.busy;
    const loadBtn = `<button class="btn btn-sm ${d ? '' : 'btn-primary'}" onclick="Fw.load()" ${working ? 'disabled' : ''}>${icon(working ? 'loader-circle' : 'refresh-cw')} <span>${d ? t('refresh', 'Refresh') : t('fw_load')}</span></button>`;
    let body = '';
    if (S.error) body += box('danger', esc(S.error));
    if (!d) {
      body += `<div style="color: var(--text-muted); font-size: 0.85rem;">${esc(t('fw_desc'))}</div>`;
    } else if (!d.supported) {
      body += `<div style="color: var(--text-muted); font-size: 0.85rem;">${esc(t('fw_unsupported'))}</div>`;
    } else {
      if (d.mode === 'none') body += box('warning', esc(t('fw_no_priv')));
      if (d.backends.filter((b) => b.active && b.id !== 'iptables').length > 1) body += box('warning', esc(t('fw_conflict')));
      const tabs = d.backends.map((b) => {
        const state = !b.installed ? t('fw_not_installed') : (b.active ? t('fw_active') : t('fw_inactive'));
        const sel = S.tab === b.id;
        return `<button class="btn btn-sm ${sel ? '' : 'btn-ghost'}" ${sel ? 'style="border-color: var(--accent); color: var(--accent);"' : ''} onclick="Fw.tab(${jsArg(b.id)})">${esc(b.id)} <span class="badge ${b.active ? 'badge-success' : 'badge-sec-unknown'}" style="margin-left: 4px;">${esc(state)}</span></button>`;
      }).join('');
      body += `<div style="display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 12px;">${tabs}</div>${panel(d.backends.find((b) => b.id === S.tab) || d.backends[0])}`;
    }
    const modeBadge = d && d.supported ? `<span class="badge badge-sec-unknown" title="${esc(t('fw_mode_title'))}">${esc(d.mode)}</span>` : '';
    el.innerHTML = `
      <div style="display: flex; justify-content: space-between; align-items: center; gap: 8px; margin-bottom: 12px; flex-wrap: wrap;">
        <h4 style="margin: 0; display: flex; align-items: center; gap: 6px;">${icon('shield')} <span>${esc(t('fw_title'))}</span> ${modeBadge}</h4>
        ${loadBtn}
      </div>
      ${body}`;
  };

  Fw.tab = function (id) { S.tab = id; Fw.render(); };

  function panel(b) {
    const dis = S.busy ? 'disabled' : '';
    const btn = (onclick, ic, label, cls) => `<button class="btn btn-sm ${cls || ''}" onclick="${onclick}" ${dis}>${icon(ic)} <span>${esc(label)}</span></button>`;
    if (!b.installed) {
      return `<div style="color: var(--text-muted); font-size: 0.85rem; margin-bottom: 10px;">${esc(b.id)} ${esc(t('fw_not_installed'))}.</div>` +
        btn(`Fw.setup(${jsArg(b.id)})`, 'download', t('fw_install_setup'), 'btn-primary');
    }
    const buttons = [btn(`Fw.openRule(${jsArg(b.id)}, null)`, 'plus', t('fw_add_rule'), 'btn-primary')];
    if (b.id === 'firewalld') buttons.push(btn('Fw.openAddZone()', 'folder-plus', t('fw_add_zone')));
    if (b.id !== 'iptables') buttons.push(btn(`Fw.toggle(${jsArg(b.id)}, ${!b.active})`, 'power', b.active ? t('fw_disable') : t('fw_enable')));
    buttons.push(btn(`Fw.setup(${jsArg(b.id)})`, 'shield-check', t('fw_setup')));
    if (b.id === 'iptables') buttons.push(btn('Fw.persist()', 'save', t('fw_save_disk')));
    const meta = [b.detail, b.defaults].filter(Boolean).map(esc).join(' · ');
    let html = `<div style="display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-bottom: 10px;"><span class="badge ${b.active ? 'badge-success' : 'badge-sec-unknown'}">${esc(b.active ? t('fw_active') : t('fw_inactive'))}</span><span style="color: var(--text-muted); font-size: 0.82rem; font-family: var(--font-mono);">${meta}</span></div>
      <div style="display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 12px;">${buttons.join('')}</div>`;
    if (b.error) html += box('danger', esc(b.error));
    const hasV6 = b.rules.some((r) => (r.raw || '').includes('(v6)'));
    const note = (b.id === 'iptables' && t('fw_iptables_note')) || (b.id === 'ufw' && !b.active && !b.error && t('fw_ufw_inactive_note')) || (b.id === 'ufw' && hasV6 && t('fw_ufw_v6_note'));
    if (note) html += `<div style="color: var(--text-muted); font-size: 0.8rem; margin-bottom: 10px;">${esc(note)}</div>`;
    if (!b.rules.length) return html + (b.error ? '' : `<div style="color: var(--text-muted); font-size: 0.85rem;">${esc(t('fw_no_rules'))}</div>`);

    const cols = { ufw: ['#', t('fw_col_rule')], firewalld: [t('fw_f_zone'), t('fw_f_kind'), t('fw_f_value')], iptables: [t('fw_f_chain'), '#', t('fw_col_rule')] }[b.id];
    const mono = 'font-family: var(--font-mono); font-size: 0.82rem; overflow-wrap: anywhere;';
    const rows = b.rules.map((r, i) => {
      const cells = { ufw: [r.index, r.raw], firewalld: [r.zone, r.kind, r.value], iptables: [r.chain, r.index, r.raw] }[b.id];
      const edit = r.editable
        ? `<button class="btn btn-icon btn-sm btn-ghost" ${dis} onclick="Fw.openRule(${jsArg(b.id)}, ${i})" title="${esc(t('edit', 'Edit'))}">${icon('pencil')}</button>`
        : `<span title="${esc(t('fw_readonly'))}" style="color: var(--text-muted);">${icon('lock')}</span>`;
      return `<tr style="border-bottom: 1px solid var(--card-border);">
        ${cells.map((c, k) => `<td style="padding: 6px 12px; ${k === cells.length - 1 ? mono : 'white-space: nowrap;'}">${esc(c)}</td>`).join('')}
        <td style="padding: 6px 12px; white-space: nowrap; text-align: right;">${edit}<button class="btn btn-icon btn-sm btn-ghost" ${dis} onclick="Fw.del(${jsArg(b.id)}, ${i})" title="${esc(t('delete', 'Delete'))}">${icon('trash-2')}</button></td>
      </tr>`;
    }).join('');
    return html + `<div style="overflow-x: auto;"><table style="width: 100%; border-collapse: collapse; font-size: 0.85rem;">
      <thead><tr style="border-bottom: 1px solid var(--card-border); color: var(--text-muted); text-align: left;">${cols.map((c) => `<th style="padding: 6px 12px;">${esc(c)}</th>`).join('')}<th></th></tr></thead>
      <tbody>${rows}</tbody></table></div>`;
  }

  /* ---------- read / change ---------- */
  Fw.load = async function () {
    const host = currentHost;
    if (!host) return;
    S.loading = true;
    S.error = '';
    Fw.render();
    let res;
    try {
      res = await (await fetch('/api/hosts/' + host.id + '/security/firewall')).json();
    } catch (e) {
      res = { ok: false, error: e.message };
    }
    if (S.hostID !== host.id) return; // switched host while waiting
    const first = !S.data;
    S.loading = false;
    if (res.ok) {
      S.data = res.firewall;
      const bs = res.firewall.backends;
      if (first && bs.length) S.tab = (bs.find((b) => b.active) || bs.find((b) => b.installed) || bs[0]).id;
    } else {
      S.error = res.error || 'error';
    }
    Fw.render();
  };

  // Runs one change, then re-reads the state (a failed multi-step change may be partly applied).
  async function run(be, action, extra) {
    const host = currentHost;
    if (!host) return { ok: false, message: '' };
    S.busy = true;
    Fw.render();
    let res;
    try {
      const r = await fetch('/api/hosts/' + host.id + '/security/firewall/action', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(Object.assign({ backend: be, action }, extra))
      });
      res = await r.json();
    } catch (e) {
      res = { ok: false, error: e.message };
    }
    S.busy = false;
    const r = res.result || {};
    let message = res.error || r.output || '';
    if (!res.ok && r.mode === 'none') message += (message ? ' — ' : '') + t('fw_no_priv_short');
    if (res.ok && r.output) message = r.output;
    if (S.hostID === host.id) await Fw.load();
    return { ok: !!res.ok, message };
  }

  function toast(be, action, out) {
    const label = t('fw_act_' + action, action);
    if (out.ok) showToast(t('fw_action_ok').replace('{backend}', be).replace('{action}', label), 'success');
    else showToast(out.message || t('fw_action_failed').replace('{backend}', be).replace('{action}', label), 'error', 8000);
  }

  async function confirmRun(be, action, c) {
    const host = currentHost;
    if (!host || !S.data) return;
    const fill = (s) => s.replace(/\{backend\}/g, be).replace(/\{host\}/g, host.name).replace(/\{port\}/g, S.data.ssh_port);
    if (!await showConfirm({ title: fill(c.title), message: fill(c.message), confirmText: c.btn, danger: c.danger !== false })) return;
    toast(be, action, await run(be, action, c.extra || {}));
  }

  Fw.setup = (be) => confirmRun(be, 'setup', { title: t('fw_confirm_setup_title'), message: t('fw_confirm_setup_' + be), btn: t('fw_setup') });

  Fw.toggle = (be, enable) => confirmRun(be, enable ? 'enable' : 'disable', enable
    ? { title: t('fw_confirm_enable_title'), message: t('fw_confirm_enable_msg'), btn: t('fw_enable'), danger: false }
    : { title: t('fw_confirm_disable_title'), message: t('fw_confirm_disable_msg'), btn: t('fw_disable') });

  Fw.persist = async () => toast('iptables', 'persist', await run('iptables', 'persist', {}));

  Fw.del = function (be, idx) {
    const rule = backend(be).rules[idx];
    return confirmRun(be, 'delete', {
      title: t('fw_confirm_delete_title'),
      message: t('fw_confirm_delete_msg') + '\n\n' + (rule.raw || rule.value),
      btn: t('delete', 'Delete'),
      extra: { old: rule }
    });
  };

  /* ---------- pure helpers (exposed for testability) ---------- */
  const COMMON_SERVICES = [
    'ssh', 'http', 'https', 'dns', 'smtp', 'imap', 'imaps',
    'pop3', 'pop3s', 'mysql', 'postgresql', 'ftp', 'samba',
    'nfs', 'dhcpv6-client', 'cockpit'
  ];

  Fw._parse = function (kind, val) {
    val = (val || '').trim();
    if (kind === 'service') return { service: val };
    if (kind === 'port') {
      const m = val.match(/^(\d+(?:-\d+)?)\/(tcp|udp|sctp|dccp)$/);
      if (m) return { port: m[1], proto: m[2] };
      return { port: val, proto: 'tcp' };
    }
    if (kind === 'rich') {
      let s = val;
      if (!s.startsWith('rule')) return { raw: true, text: val };
      s = s.slice(4).trim();
      const res = { raw: false, family: '', source: '', dest: '', elemType: 'none', elemService: '', elemPort: '', elemProto: 'tcp', action: 'accept', log: false, text: val };
      const famM = s.match(/^family="([^"]+)"\s*/);
      if (famM) {
        if (famM[1] !== 'ipv4' && famM[1] !== 'ipv6') return { raw: true, text: val };
        res.family = famM[1];
        s = s.slice(famM[0].length).trim();
      }
      const srcM = s.match(/^source\s+address="([^"]+)"\s*/);
      if (srcM) {
        res.source = srcM[1];
        s = s.slice(srcM[0].length).trim();
      } else if (s.startsWith('source')) {
        return { raw: true, text: val };
      }
      const dstM = s.match(/^destination\s+address="([^"]+)"\s*/);
      if (dstM) {
        res.dest = dstM[1];
        s = s.slice(dstM[0].length).trim();
      } else if (s.startsWith('destination')) {
        return { raw: true, text: val };
      }
      const svcM = s.match(/^service\s+name="([^"]+)"\s*/);
      const prtM = s.match(/^port\s+port="([^"]+)"\s+protocol="([^"]+)"\s*/);
      if (svcM) {
        res.elemType = 'service';
        res.elemService = svcM[1];
        s = s.slice(svcM[0].length).trim();
      } else if (prtM) {
        res.elemType = 'port';
        res.elemPort = prtM[1];
        res.elemProto = prtM[2];
        s = s.slice(prtM[0].length).trim();
      }
      const logM = s.match(/^log(\s+prefix="[^"]+")?(\s+level="[^"]+")?(\s+limit\s+value="[^"]+")?\s*/);
      if (logM) {
        if (logM[0].trim() !== 'log') return { raw: true, text: val };
        res.log = true;
        s = s.slice(logM[0].length).trim();
      }
      const actM = s.match(/^(accept|reject|drop)(\s+type="[^"]+")?(\s+limit\s+value="[^"]+")?\s*$/);
      if (actM) {
        if (actM[0].trim() !== actM[1]) return { raw: true, text: val };
        res.action = actM[1];
        s = '';
      } else {
        return { raw: true, text: val };
      }
      return res;
    }
    return {};
  };

  Fw._compose = function (kind, f) {
    if (kind === 'service') return (f.service || '').trim();
    if (kind === 'port') {
      const port = (f.port || '').trim();
      const proto = (f.proto || 'tcp').trim();
      return port ? port + '/' + proto : '';
    }
    if (kind === 'rich') {
      if (f.raw) return (f.text || '').trim();
      const parts = ['rule'];
      if (f.family) parts.push(`family="${f.family}"`);
      if (f.source) parts.push(`source address="${f.source.trim()}"`);
      if (f.dest) parts.push(`destination address="${f.dest.trim()}"`);
      if (f.elemType === 'service' && f.elemService) {
        parts.push(`service name="${f.elemService.trim()}"`);
      } else if (f.elemType === 'port' && f.elemPort) {
        parts.push(`port port="${f.elemPort.trim()}" protocol="${(f.elemProto || 'tcp').trim()}"`);
      }
      if (f.log) parts.push('log');
      parts.push(f.action || 'accept');
      return parts.join(' ');
    }
    return '';
  };

  function validZoneName(s) { return /^[A-Za-z0-9_.-]{1,17}$/.test(s); }
  function validServiceName(s) { return /^[A-Za-z0-9_.-]{1,64}$/.test(s); }
  function validPort(s) {
    const m = String(s || '').match(/^(\d{1,5})(?:-(\d{1,5}))?$/);
    if (!m) return false;
    const a = parseInt(m[1], 10);
    if (a < 1 || a > 65535) return false;
    if (m[2]) {
      const b = parseInt(m[2], 10);
      if (b < a || b > 65535) return false;
    }
    return true;
  }
  function validIPOrCIDR(s) {
    if (!s) return true;
    const [ip, cidr] = s.split('/');
    if (cidr !== undefined && (!/^\d{1,3}$/.test(cidr) || parseInt(cidr, 10) > 128)) return false;
    const v4 = /^(\d{1,3}\.){3}\d{1,3}$/;
    if (v4.test(ip)) {
      if (cidr !== undefined && parseInt(cidr, 10) > 32) return false;
      return ip.split('.').every((n) => parseInt(n, 10) <= 255);
    }
    const v6 = /^[0-9a-fA-F:]+$/;
    return v6.test(ip) && ip.includes(':');
  }

  /* ---------- add zone dialog ---------- */
  Fw.openAddZone = function () {
    let modal = document.getElementById('firewallZoneModal');
    if (!modal) {
      modal = document.createElement('div');
      modal.id = 'firewallZoneModal';
      modal.className = 'modal';
      document.body.appendChild(modal);
    }
    modal.innerHTML = `
      <div class="modal-content modal-dialog-sm">
        <div class="modal-header">
          <h3>${esc(t('fw_add_zone') + ' · firewalld')}</h3>
          <button class="btn btn-icon btn-sm btn-ghost" onclick="closeModal('firewallZoneModal')">${icon('x')}</button>
        </div>
        <div class="modal-body">
          <div class="form-group">
            <label class="form-label">${esc(t('fw_zone_name'))}</label>
            <input type="text" id="fwZoneNameInput" maxlength="17" placeholder="${esc(t('fw_zone_name_ph'))}" autocomplete="off" spellcheck="false" />
            <span class="form-hint">${esc(t('fw_hint_zone_name'))}</span>
          </div>
          <div id="firewallZoneErr" style="display: none; color: var(--danger); font-size: 0.82rem; white-space: pre-wrap; overflow-wrap: anywhere;"></div>
        </div>
        <div class="modal-footer">
          <button class="btn" onclick="closeModal('firewallZoneModal')">${esc(t('cancel', 'Cancel'))}</button>
          <button class="btn btn-primary" id="firewallZoneSave" onclick="Fw.submitZone()">${esc(t('save', 'Save'))}</button>
        </div>
      </div>`;
    openModal('firewallZoneModal');
    setTimeout(() => { const i = document.getElementById('fwZoneNameInput'); if (i) i.focus(); }, 50);
  };

  Fw.submitZone = async function () {
    const el = document.getElementById('fwZoneNameInput');
    const name = el ? el.value.trim() : '';
    const errEl = document.getElementById('firewallZoneErr');
    if (!validZoneName(name)) {
      if (errEl) { errEl.textContent = t('fw_err_zone_name'); errEl.style.display = 'block'; }
      return;
    }
    const save = document.getElementById('firewallZoneSave');
    if (save) save.disabled = true;
    const out = await run('firewalld', 'add-zone', { rule: { zone: name } });
    if (save) save.disabled = false;
    if (out.ok) {
      closeModal('firewallZoneModal');
      toast('firewalld', 'add_zone', out);
    } else if (errEl) {
      errEl.textContent = out.message;
      errEl.style.display = 'block';
    }
  };

  /* ---------- add / edit rule dialog ---------- */
  function renderFwdFields(kind, parsed, isRaw) {
    const p = parsed || {};
    const txt = (id, label, val, ph, hint) => `<div class="form-group"><label class="form-label">${esc(label)}</label><input type="text" id="${id}" value="${esc(val)}" placeholder="${esc(ph)}" autocomplete="off" spellcheck="false" />${hint ? `<span class="form-hint">${esc(hint)}</span>` : ''}</div>`;
    const sel = (id, label, list, cur) => `<div class="form-group"><label class="form-label">${esc(label)}</label><select id="${id}">${list.map((o) => { const [val, lbl] = Array.isArray(o) ? o : [o, o]; return `<option value="${esc(val)}" ${String(cur || '') === val ? 'selected' : ''}>${esc(lbl)}</option>`; }).join('')}</select></div>`;
    const any = t('fw_any');

    if (kind === 'service') {
      const dl = `<datalist id="fwfServiceDatalist">${COMMON_SERVICES.map((s) => `<option value="${esc(s)}"></option>`).join('')}</datalist>`;
      return `<div style="grid-column: 1 / -1;" class="form-group">
        <label class="form-label">${esc(t('fw_f_service'))}</label>
        <input type="text" id="fwfService" list="fwfServiceDatalist" value="${esc(p.service || '')}" placeholder="http" autocomplete="off" spellcheck="false" />
        ${dl}
      </div>`;
    }
    if (kind === 'port') {
      return txt('fwfPortNum', t('fw_f_port'), p.port || '', '8080 or 8000-8100') +
        sel('fwfPortProto', t('fw_f_proto'), ['tcp', 'udp', 'sctp', 'dccp'], p.proto || 'tcp');
    }
    if (kind === 'rich') {
      const toggleBtn = `<div style="grid-column: 1 / -1; display: flex; justify-content: flex-end; margin-bottom: 6px;">
        <button type="button" class="btn btn-sm btn-ghost" onclick="Fw.toggleRichMode()">${icon('refresh-cw')} <span>${esc(isRaw ? t('fw_mode_form') : t('fw_mode_raw'))}</span></button>
      </div>`;
      if (isRaw) {
        return toggleBtn + `<div style="grid-column: 1 / -1;">${txt('fwfRichRaw', t('fw_f_value'), p.text || '', 'rule family="ipv4" source address="1.2.3.4" reject', t('fw_hint_value'))}</div>`;
      }
      const elem = p.elemType || 'service';
      return toggleBtn +
        sel('fwfRichFamily', t('fw_f_family'), [['', any], 'ipv4', 'ipv6'], p.family || '') +
        sel('fwfRichAction', t('fw_f_action'), ['accept', 'reject', 'drop'], p.action || 'accept') +
        txt('fwfRichSource', t('fw_f_source'), p.source || '', '10.0.0.0/8') +
        txt('fwfRichDest', t('fw_f_dest'), p.dest || '', '192.168.1.1') +
        sel('fwfRichElemType', t('fw_f_element'), [['service', t('fw_f_elem_service')], ['port', t('fw_f_elem_port')], ['none', t('fw_f_elem_none')]], elem) +
        `<div class="form-group" style="display: flex; align-items: center; gap: 8px; margin-top: 24px;">
          <label style="display: flex; align-items: center; gap: 6px; cursor: pointer; font-size: 0.85rem;">
            <input type="checkbox" id="fwfRichLog" ${p.log ? 'checked' : ''} /> <span>${esc(t('fw_f_log_label'))}</span>
          </label>
        </div>` +
        `<div id="fwfRichElemFields" style="grid-column: 1 / -1; display: grid; grid-template-columns: 1fr 1fr; gap: 0 12px;">
          ${renderRichElemFields(elem, p)}
        </div>`;
    }
    return '';
  }

  function renderRichElemFields(elem, p) {
    const txt = (id, label, val, ph) => `<div class="form-group"><label class="form-label">${esc(label)}</label><input type="text" id="${id}" value="${esc(val)}" placeholder="${esc(ph)}" autocomplete="off" spellcheck="false" /></div>`;
    const sel = (id, label, list, cur) => `<div class="form-group"><label class="form-label">${esc(label)}</label><select id="${id}">${list.map((o) => { const [val, lbl] = Array.isArray(o) ? o : [o, o]; return `<option value="${esc(val)}" ${String(cur || '') === val ? 'selected' : ''}>${esc(lbl)}</option>`; }).join('')}</select></div>`;
    if (elem === 'service') {
      const dl = `<datalist id="fwfRichServiceDatalist">${COMMON_SERVICES.map((s) => `<option value="${esc(s)}"></option>`).join('')}</datalist>`;
      return `<div style="grid-column: 1 / -1;" class="form-group">
        <label class="form-label">${esc(t('fw_f_service'))}</label>
        <input type="text" id="fwfRichService" list="fwfRichServiceDatalist" value="${esc(p.elemService || '')}" placeholder="ssh" autocomplete="off" spellcheck="false" />
        ${dl}
      </div>`;
    }
    if (elem === 'port') {
      return txt('fwfRichPort', t('fw_f_port'), p.elemPort || '', '8080 or 8000-8100') +
        sel('fwfRichProto', t('fw_f_proto'), ['tcp', 'udp', 'sctp', 'dccp'], p.elemProto || 'tcp');
    }
    return '';
  }

  Fw.onKindChange = function () {
    const kindEl = document.getElementById('fwfKind');
    if (!kindEl) return;
    const kind = kindEl.value;
    if (dlg) { dlg.kind = kind; dlg.isRaw = false; dlg.parsed = {}; }
    const container = document.getElementById('fwfDynamicFields');
    if (container) container.innerHTML = renderFwdFields(kind, {}, false);
    attachRichListeners();
  };

  Fw.onZoneChange = function () {
    const zoneEl = document.getElementById('fwfZone');
    const newZoneGroup = document.getElementById('fwfNewZoneGroup');
    if (!zoneEl || !newZoneGroup) return;
    const isNew = zoneEl.value === '__new__';
    newZoneGroup.style.display = isNew ? 'block' : 'none';
    if (isNew) {
      const inp = document.getElementById('fwfNewZoneName');
      if (inp) setTimeout(() => inp.focus(), 50);
    }
  };

  Fw.toggleRichMode = function () {
    if (!dlg) return;
    dlg.isRaw = !dlg.isRaw;
    const container = document.getElementById('fwfDynamicFields');
    if (container) container.innerHTML = renderFwdFields('rich', dlg.parsed, dlg.isRaw);
    attachRichListeners();
  };

  function attachRichListeners() {
    const elemSel = document.getElementById('fwfRichElemType');
    if (elemSel) {
      elemSel.onchange = function () {
        const sub = document.getElementById('fwfRichElemFields');
        if (sub) sub.innerHTML = renderRichElemFields(elemSel.value, dlg && dlg.parsed ? dlg.parsed : {});
      };
    }
  }

  Fw.openRule = function (be, idx) {
    const b = backend(be);
    const old = idx === null ? null : b.rules[idx];
    dlg = { backend: be, old, kind: old ? old.kind : 'service', isRaw: false, parsed: {} };
    const v = old || {};
    const opts = (list, cur) => list.map((o) => { const [val, label] = Array.isArray(o) ? o : [o, o]; return `<option value="${esc(val)}" ${String(cur || '') === val ? 'selected' : ''}>${esc(label)}</option>`; }).join('');
    const sel = (id, label, list, cur, disabled, onchange) => `<div class="form-group"><label class="form-label">${esc(label)}</label><select id="${id}" ${disabled ? 'disabled' : ''} ${onchange ? `onchange="${onchange}"` : ''}>${opts(list, cur)}</select></div>`;
    const txt = (id, label, val, ph, hint) => `<div class="form-group"><label class="form-label">${esc(label)}</label><input type="text" id="${id}" value="${esc(val)}" placeholder="${esc(ph)}" autocomplete="off" spellcheck="false" />${hint ? `<span class="form-hint">${esc(hint)}</span>` : ''}</div>`;
    const any = t('fw_any');
    let fields;
    if (be === 'ufw') {
      fields = sel('fwfAction', t('fw_f_action'), ['allow', 'deny', 'reject', 'limit'], v.action || 'allow') +
        sel('fwfDirection', t('fw_f_direction'), ['in', 'out'], v.direction || 'in') +
        sel('fwfProto', t('fw_f_proto'), [['', any], 'tcp', 'udp'], v.proto) +
        txt('fwfPort', t('fw_f_port'), v.port, '22, 8000:8100') +
        txt('fwfSource', t('fw_f_source'), v.source, any + ' / 10.0.0.0/8');
    } else if (be === 'firewalld') {
      const zones = b.zones && b.zones.length ? [...b.zones] : ['public'];
      const zoneOpts = zones.map((z) => [z, z]);
      if (!old) zoneOpts.push(['__new__', t('fw_new_zone_opt')]);
      const initialKind = v.kind || 'service';
      const parsed = Fw._parse(initialKind, v.value);
      dlg.kind = initialKind;
      dlg.parsed = parsed;
      dlg.isRaw = parsed.raw === true;
      fields = sel('fwfZone', t('fw_f_zone'), zoneOpts, v.zone || zones[0], false, 'Fw.onZoneChange()') +
        sel('fwfKind', t('fw_f_kind'), ['service', 'port', 'rich'], initialKind, false, 'Fw.onKindChange()') +
        `<div id="fwfNewZoneGroup" style="display: none; grid-column: 1 / -1;" class="form-group">
          <label class="form-label">${esc(t('fw_zone_name'))}</label>
          <input type="text" id="fwfNewZoneName" maxlength="17" placeholder="${esc(t('fw_zone_name_ph'))}" autocomplete="off" spellcheck="false" />
          <span class="form-hint">${esc(t('fw_hint_zone_name'))}</span>
        </div>` +
        `<div id="fwfDynamicFields" style="grid-column: 1 / -1; display: grid; grid-template-columns: 1fr 1fr; gap: 0 12px;">
          ${renderFwdFields(initialKind, parsed, dlg.isRaw)}
        </div>`;
    } else {
      fields = sel('fwfChain', t('fw_f_chain'), ['INPUT', 'OUTPUT', 'FORWARD'], v.chain || 'INPUT', !!old) +
        sel('fwfAction', t('fw_f_action'), ['ACCEPT', 'DROP', 'REJECT'], v.action || 'ACCEPT') +
        sel('fwfProto', t('fw_f_proto'), [['', any], 'tcp', 'udp', 'icmp'], v.proto) +
        txt('fwfPort', t('fw_f_port'), v.port, '22, 8000:8100') +
        txt('fwfSource', t('fw_f_source'), v.source, '10.0.0.0/8') +
        txt('fwfDest', t('fw_f_dest'), v.dest, '') +
        txt('fwfIn', t('fw_f_in_iface'), v.in_iface, 'eth0') +
        txt('fwfOut', t('fw_f_out_iface'), v.out_iface, 'eth0') +
        (old ? '' : txt('fwfPos', t('fw_f_position'), '', t('fw_pos_ph'), t('fw_hint_position')));
    }
    let modal = document.getElementById('firewallRuleModal');
    if (!modal) {
      modal = document.createElement('div');
      modal.id = 'firewallRuleModal';
      modal.className = 'modal';
      document.body.appendChild(modal);
    }
    modal.innerHTML = `
      <div class="modal-content modal-dialog-md">
        <div class="modal-header">
          <h3>${esc((old ? t('fw_edit_rule') : t('fw_add_rule')) + ' · ' + be)}</h3>
          <button class="btn btn-icon btn-sm btn-ghost" onclick="closeModal('firewallRuleModal')">${icon('x')}</button>
        </div>
        <div class="modal-body">
          <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 0 12px;">${fields}</div>
          <div id="firewallRuleErr" style="display: none; color: var(--danger); font-size: 0.82rem; white-space: pre-wrap; overflow-wrap: anywhere; margin-top: 8px;"></div>
        </div>
        <div class="modal-footer">
          <button class="btn" onclick="closeModal('firewallRuleModal')">${esc(t('cancel', 'Cancel'))}</button>
          <button class="btn btn-primary" id="firewallRuleSave" onclick="Fw.submitRule()">${esc(t('save', 'Save'))}</button>
        </div>
      </div>`;
    openModal('firewallRuleModal');
    if (be === 'firewalld') attachRichListeners();
  };

  Fw.submitRule = async function () {
    if (!dlg) return;
    const { backend: be, old } = dlg;
    const g = (id) => { const el = document.getElementById(id); return el ? el.value.trim() : ''; };
    const errEl = document.getElementById('firewallRuleErr');
    const showErr = (msg) => {
      if (errEl) { errEl.textContent = msg; errEl.style.display = 'block'; }
    };
    if (errEl) { errEl.style.display = 'none'; errEl.textContent = ''; }

    let rule;
    let zoneToCreate = '';

    if (be === 'ufw') {
      rule = { action: g('fwfAction'), direction: g('fwfDirection'), proto: g('fwfProto'), port: g('fwfPort'), source: g('fwfSource') };
    } else if (be === 'firewalld') {
      let zone = g('fwfZone');
      if (zone === '__new__') {
        const newZ = g('fwfNewZoneName');
        if (!validZoneName(newZ)) { showErr(t('fw_err_zone_name')); return; }
        zoneToCreate = newZ;
        zone = newZ;
      }
      const kind = g('fwfKind');
      let value = '';

      if (kind === 'service') {
        const svc = g('fwfService');
        if (!validServiceName(svc)) { showErr(t('fw_err_service_name')); return; }
        value = Fw._compose('service', { service: svc });
      } else if (kind === 'port') {
        const port = g('fwfPortNum');
        const proto = g('fwfPortProto') || 'tcp';
        if (!validPort(port)) { showErr(t('fw_err_port')); return; }
        value = Fw._compose('port', { port, proto });
      } else if (kind === 'rich') {
        if (dlg.isRaw) {
          const rawVal = g('fwfRichRaw');
          if (!rawVal.startsWith('rule ') || rawVal.length > 500 || /[\r\n\0]/.test(rawVal)) {
            showErr(t('fw_hint_value')); return;
          }
          value = rawVal;
        } else {
          const family = g('fwfRichFamily');
          const action = g('fwfRichAction') || 'accept';
          const source = g('fwfRichSource');
          const dest = g('fwfRichDest');
          const elemType = g('fwfRichElemType') || 'service';
          const log = !!(document.getElementById('fwfRichLog') && document.getElementById('fwfRichLog').checked);
          let elemService = '';
          let elemPort = '';
          let elemProto = 'tcp';

          if (source && !validIPOrCIDR(source)) { showErr(t('fw_err_ip')); return; }
          if (dest && !validIPOrCIDR(dest)) { showErr(t('fw_err_ip')); return; }

          if (elemType === 'service') {
            elemService = g('fwfRichService');
            if (elemService && !validServiceName(elemService)) { showErr(t('fw_err_service_name')); return; }
          } else if (elemType === 'port') {
            elemPort = g('fwfRichPort');
            elemProto = g('fwfRichProto') || 'tcp';
            if (elemPort && !validPort(elemPort)) { showErr(t('fw_err_port')); return; }
          }
          if (elemType === 'none' && !source && !dest) {
            showErr(t('fw_err_rich_req')); return;
          }
          value = Fw._compose('rich', { family, source, dest, elemType, elemService, elemPort, elemProto, log, action });
        }
      }
      rule = { zone, kind, value };
    } else {
      rule = { chain: g('fwfChain'), action: g('fwfAction'), proto: g('fwfProto'), port: g('fwfPort'), source: g('fwfSource'), dest: g('fwfDest'), in_iface: g('fwfIn'), out_iface: g('fwfOut'), index: parseInt(g('fwfPos'), 10) || 0 };
    }

    const save = document.getElementById('firewallRuleSave');
    if (save) save.disabled = true;

    // If new zone requested, create it first
    if (zoneToCreate) {
      const zOut = await run('firewalld', 'add-zone', { rule: { zone: zoneToCreate } });
      if (!zOut.ok) {
        if (save) save.disabled = false;
        showErr(zOut.message);
        return;
      }
      // New zone succeeded: update select options in dialog so zone is now regular option
      const zoneSel = document.getElementById('fwfZone');
      if (zoneSel) {
        const opt = document.createElement('option');
        opt.value = zoneToCreate;
        opt.textContent = zoneToCreate;
        opt.selected = true;
        zoneSel.insertBefore(opt, zoneSel.lastElementChild);
        zoneSel.value = zoneToCreate;
      }
      const newZoneGroup = document.getElementById('fwfNewZoneGroup');
      if (newZoneGroup) newZoneGroup.style.display = 'none';
    }

    const out = await run(be, old ? 'edit' : 'add', old ? { rule, old } : { rule });
    if (save) save.disabled = false;
    if (out.ok) {
      closeModal('firewallRuleModal');
      toast(be, old ? 'edit' : 'add', out);
    } else {
      showErr(out.message);
    }
  };
})();
