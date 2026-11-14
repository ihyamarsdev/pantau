/* Animated progress cards for every Docker management call (start/stop/restart, pull, stack up/down, prune, ...).
 * The raw command output is never shown while the call runs: the stream is only parsed for a stage line and a
 * done/total count (image layers, compose resources). A failed call keeps its card with the error and a collapsed
 * "Details" tail of the output. Relies on index.html globals: dockerPost, svgIcon, escapeHtml, t, I18N, SP
 * (scanprogress bundle, for the elapsed counter). */
(function () {
  'use strict';

  const DP = (window.DP = {});

  // key (`sub:action`, or `sub` when the payload has no action) -> icon, animation, [ing EN, done EN, ing ID, done ID]
  const KINDS = {
    'action:start': ['play', 'pulse', 'Starting', 'Started', 'Memulai', 'Dimulai'],
    'action:stop': ['square', 'shake', 'Stopping', 'Stopped', 'Menghentikan', 'Dihentikan'],
    'action:restart': ['refresh-cw', 'spin', 'Restarting', 'Restarted', 'Memulai ulang', 'Dimulai ulang'],
    'action:pause': ['pause', 'pulse', 'Pausing', 'Paused', 'Menjeda', 'Dijeda'],
    'action:unpause': ['play', 'pulse', 'Resuming', 'Resumed', 'Melanjutkan', 'Dilanjutkan'],
    'action:kill': ['zap', 'shake', 'Killing', 'Killed', 'Mematikan paksa', 'Dimatikan paksa'],
    'action:remove': ['trash-2', 'shake', 'Removing', 'Removed', 'Menghapus', 'Dihapus'],
    'action:rename': ['pencil', 'pulse', 'Renaming', 'Renamed', 'Mengganti nama', 'Diganti nama'],
    'image:pull': ['download', 'down', 'Pulling', 'Pulled', 'Menarik', 'Ditarik'],
    'image:remove': ['trash-2', 'shake', 'Removing image', 'Removed image', 'Menghapus image', 'Image dihapus'],
    'run': ['rocket', 'up', 'Creating container from', 'Container created from', 'Membuat container dari', 'Container dibuat dari'],
    'stack:up': ['rocket', 'up', 'Starting stack', 'Stack started', 'Menjalankan stack', 'Stack berjalan'],
    'stack:down': ['square', 'shake', 'Taking down stack', 'Stack down', 'Menurunkan stack', 'Stack diturunkan'],
    'stack:start': ['play', 'pulse', 'Starting stack', 'Stack started', 'Memulai stack', 'Stack dimulai'],
    'stack:stop': ['square', 'shake', 'Stopping stack', 'Stack stopped', 'Menghentikan stack', 'Stack dihentikan'],
    'stack:restart': ['refresh-cw', 'spin', 'Restarting stack', 'Stack restarted', 'Memulai ulang stack', 'Stack dimulai ulang'],
    'stack:pull': ['download', 'down', 'Pulling images of', 'Pulled images of', 'Menarik image', 'Image ditarik'],
    'stack:recreate': ['refresh-cw', 'spin', 'Recreating stack', 'Stack recreated', 'Membuat ulang stack', 'Stack dibuat ulang'],
    'stack:update': ['refresh-cw', 'spin', 'Updating stack', 'Stack updated', 'Memperbarui stack', 'Stack diperbarui'],
    'stack:forget': ['trash-2', 'shake', 'Forgetting stack', 'Stack forgotten', 'Melupakan stack', 'Stack dilupakan'],
    'stack/deploy': ['rocket', 'up', 'Deploying stack', 'Stack deployed', 'Men-deploy stack', 'Stack ter-deploy'],
    'prune': ['trash-2', 'shake', 'Pruning', 'Pruned', 'Membersihkan', 'Dibersihkan'],
    'volume:create': ['hard-drive', 'up', 'Creating volume', 'Volume created', 'Membuat volume', 'Volume dibuat'],
    'volume:remove': ['trash-2', 'shake', 'Removing volume', 'Removed volume', 'Menghapus volume', 'Volume dihapus'],
    'network:create': ['network', 'up', 'Creating network', 'Network created', 'Membuat network', 'Network dibuat'],
    'network:remove': ['trash-2', 'shake', 'Removing network', 'Removed network', 'Menghapus network', 'Network dihapus'],
    'network:connect': ['plug', 'pulse', 'Connecting to', 'Connected to', 'Menghubungkan ke', 'Terhubung ke'],
    'network:disconnect': ['plug', 'shake', 'Disconnecting from', 'Disconnected from', 'Memutus dari', 'Terputus dari']
  };
  const GENERIC = ['loader-circle', 'spin', 'Working on', 'Finished', 'Memproses', 'Selesai'];
  const key = (k) => 'dp_' + k.replace(/[:/]/g, '_');
  const kindOf = (o) => {
    const p = o.payload || {};
    const k = p.action ? o.sub + ':' + p.action : o.sub;
    return [k, KINDS[k] || GENERIC];
  };

  const L = {
    en: { dp_failed: 'Failed', dp_details: 'Details', dp_hide: 'Hide details', dp_layers: '{d}/{n} layers', dp_items: '{d}/{n} resources', dp_close: 'Close', dp_took: 'took {s}' },
    id: { dp_failed: 'Gagal', dp_details: 'Detail', dp_hide: 'Sembunyikan detail', dp_layers: '{d}/{n} layer', dp_items: '{d}/{n} resource', dp_close: 'Tutup', dp_took: 'selama {s}' }
  };
  Object.keys(KINDS).concat(['_generic']).forEach((k) => {
    const v = k === '_generic' ? GENERIC : KINDS[k];
    L.en[key(k) + '_ing'] = v[2]; L.en[key(k) + '_done'] = v[3];
    L.id[key(k) + '_ing'] = v[4]; L.id[key(k) + '_done'] = v[5];
  });
  ['en', 'id'].forEach((l) => { I18N[l] = Object.assign(I18N[l] || {}, L[l]); });

  const esc = (s) => escapeHtml(String(s === null || s === undefined ? '' : s));
  const fill = (s, vars) => Object.keys(vars).reduce((a, k) => a.split('{' + k + '}').join(vars[k]), s);
  const icon = (n) => svgIcon(n, 'icon-sm');
  const verb = (k, part) => t(key(KINDS[k] ? k : '_generic') + '_' + part);

  const PULL_DONE = /^(Pull complete|Already exists)/;
  const COMPOSE_DONE = /^(Created|Started|Stopped|Removed|Recreated|Pulled|Running|Healthy|Built|Restarted|Killed|Exited)$/;
  const COMPOSE_V2 = /^\s*(Container|Network|Volume|Image|Service)\s+(\S+)\s+([A-Z][A-Za-z ]*?)\s*$/;
  const COMPOSE_V1 = /^(Creating|Starting|Stopping|Removing|Recreating|Pulling)\s+(\S+)\s+\.\.\.\s*(done)?\s*$/;

  // Turns the streamed output into a stage line and a done/total count. Unknown lines are ignored.
  function makeParser() {
    const layers = {}; const items = {};
    let buf = ''; let stage = ''; const tail = [];
    function line(l) {
      if (!l.trim()) return;
      tail.push(l); if (tail.length > 40) tail.shift();
      let m = l.match(/^([0-9a-f]{12}): (.+)$/);
      if (m) {
        layers[m[1]] = m[2];
        stage = /^Extract/.test(m[2]) ? 'extract' : /^Download/.test(m[2]) ? 'download' : stage || 'prepare';
        return;
      }
      if (/^(Digest|Status):/.test(l)) { stage = 'finish'; return; }
      m = l.match(COMPOSE_V2);
      if (m) { items[m[1] + ' ' + m[2]] = COMPOSE_DONE.test(m[3]); stage = m[3] + ' ' + m[2]; return; }
      m = l.match(COMPOSE_V1);
      if (m) { items[m[2]] = !!m[3]; stage = m[1] + ' ' + m[2]; }
    }
    return {
      tail: () => tail.join('\n'),
      feed(delta) {
        buf += delta;
        const parts = buf.split('\n'); buf = parts.pop();
        parts.forEach(line);
        const ids = Object.keys(layers);
        if (ids.length) {
          const d = ids.filter((i) => PULL_DONE.test(layers[i])).length;
          const text = { prepare: 'Preparing', download: 'Downloading', extract: 'Extracting', finish: 'Finishing' }[stage] || 'Preparing';
          return { text, count: fill(t('dp_layers'), { d, n: ids.length }), pct: ids.length ? d / ids.length : 0 };
        }
        const ks = Object.keys(items);
        if (ks.length) {
          const d = ks.filter((i) => items[i]).length;
          return { text: stage, count: fill(t('dp_items'), { d, n: ks.length }), pct: d / ks.length };
        }
        return null;
      }
    };
  }

  function stackEl() {
    let el = document.getElementById('dpStack');
    if (!el) {
      el = document.createElement('div');
      el.id = 'dpStack';
      el.setAttribute('aria-live', 'polite');
      document.body.appendChild(el);
    }
    return el;
  }

  // DP.start shows a running card and returns {onChunk, finish}.
  DP.start = function (o) {
    const [k, kind] = kindOf(o);
    const p = o.payload || {};
    const subject = p.container || p.image || p.project || p.name || p.target || '';
    const card = document.createElement('div');
    card.className = 'dp-card dp-running';
    card.setAttribute('role', 'status');
    const t0 = Date.now();
    const title = (part) => `${esc(verb(k, part))}${subject ? ' <b>' + esc(subject) + '</b>' : ''}`;
    card.innerHTML = `
      <div class="dp-icon dp-a-${kind[1]}">${icon(kind[0])}</div>
      <div class="dp-main">
        <div class="dp-title">${title('ing')}…</div>
        <div class="dp-stage"></div>
        <div class="dp-bar dp-indeterminate"><i></i></div>
        <div class="dp-meta"><span data-sp-t0="${t0}">0s</span><span class="dp-count"></span></div>
        <pre class="dp-details" hidden></pre>
      </div>
      <button class="dp-x" type="button" aria-label="${esc(t('dp_close'))}">${icon('x')}</button>`;
    stackEl().appendChild(card);
    const q = (s) => card.querySelector(s);
    const remove = () => { card.classList.add('dp-leaving'); setTimeout(() => card.remove(), 250); };
    q('.dp-x').onclick = remove;
    const parser = makeParser();

    return {
      onChunk(delta) {
        const pr = parser.feed(delta);
        if (!pr) return;
        q('.dp-stage').textContent = pr.text;
        q('.dp-count').textContent = pr.count;
        const bar = q('.dp-bar');
        bar.classList.remove('dp-indeterminate');
        bar.firstElementChild.style.width = Math.max(4, Math.round(pr.pct * 100)) + '%';
      },
      finish(res) {
        const took = fmtSec(Math.round((Date.now() - t0) / 1000));
        const meta = q('.dp-meta');
        card.classList.remove('dp-running');
        const bar = q('.dp-bar');
        bar.classList.remove('dp-indeterminate');
        if (res && res.ok) {
          card.classList.add('dp-ok');
          q('.dp-icon').className = 'dp-icon dp-a-done';
          q('.dp-icon').innerHTML = icon('circle-check');
          q('.dp-title').innerHTML = (o.okMsg ? esc(o.okMsg) : title('done'));
          q('.dp-stage').textContent = '';
          bar.firstElementChild.style.width = '100%';
          meta.innerHTML = `<span>${esc(fill(t('dp_took'), { s: took }))}</span>`;
          setTimeout(remove, 3500);
          return;
        }
        card.classList.add('dp-err');
        q('.dp-icon').className = 'dp-icon dp-a-fail';
        q('.dp-icon').innerHTML = icon('circle-alert');
        q('.dp-title').innerHTML = `${esc(t('dp_failed'))}: ${title('ing')}`;
        q('.dp-stage').textContent = String((res && res.error) || '').slice(0, 300);
        bar.firstElementChild.style.width = '100%';
        meta.innerHTML = `<span>${esc(took)}</span>`;
        const out = ((res && res.output) || parser.tail() || '').split('\n').slice(-40).join('\n');
        if (out) {
          const btn = document.createElement('button');
          btn.type = 'button'; btn.className = 'dp-link'; btn.textContent = t('dp_details');
          const pre = q('.dp-details'); pre.textContent = out;
          btn.onclick = () => { pre.hidden = !pre.hidden; btn.textContent = t(pre.hidden ? 'dp_details' : 'dp_hide'); };
          meta.appendChild(btn);
        }
      }
    };
  };

  function fmtSec(s) { return s < 60 ? s + 's' : Math.floor(s / 60) + 'm ' + String(s % 60).padStart(2, '0') + 's'; }

  // DP.run executes one Docker management call with its card and returns the result of dockerPost.
  DP.run = async function (o) {
    const job = DP.start(o);
    let data;
    try {
      data = await dockerPost(o.sub, o.payload, job.onChunk);
    } catch (e) {
      data = { ok: false, error: e.message };
    }
    job.finish(data);
    return data;
  };
})();
