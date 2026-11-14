/* Shared "scan is running" feedback: a spinning icon, the step text and the seconds elapsed, so a slow scan
 * (trivy, OSV, SSH audits) never looks stuck. Elapsed time is derived from a start timestamp stored in the
 * markup (data-sp-t0), so it survives a re-render of the surrounding card. Relies on index.html globals:
 * svgIcon, escapeHtml. */
(function () {
  'use strict';

  const SP = (window.SP = {});
  const LONG_AFTER_S = 45; // after this the elapsed time is highlighted as "still running"

  function fmt(sec) {
    if (sec < 60) return sec + 's';
    const m = Math.floor(sec / 60);
    return m + 'm ' + String(sec % 60).padStart(2, '0') + 's';
  }

  function tick() {
    const now = Date.now();
    document.querySelectorAll('[data-sp-t0]').forEach((el) => {
      const sec = Math.max(0, Math.floor((now - Number(el.getAttribute('data-sp-t0'))) / 1000));
      el.textContent = fmt(sec);
      el.style.color = sec >= LONG_AFTER_S ? 'var(--warning)' : '';
    });
  }
  setInterval(tick, 1000);

  // SP.html returns a spinner, the text and a live elapsed counter. t0 = Date.now() when the work began.
  SP.html = function (text, t0) {
    t0 = t0 || Date.now();
    return '<span class="spinner" style="display:inline-block; margin-right:4px;">' + svgIcon('loader-circle', 'icon-sm') + '</span>' +
      '<span>' + escapeHtml(String(text)) + '</span> ' +
      '<span data-sp-t0="' + t0 + '" style="font-variant-numeric: tabular-nums; opacity: 0.8;">' + fmt(Math.max(0, Math.floor((Date.now() - t0) / 1000))) + '</span>';
  };

  // SP.busy disables a button and shows progress inside it; the returned function restores it.
  SP.busy = function (btn, text) {
    if (!btn) return function () {};
    const saved = btn.innerHTML;
    const wasDisabled = btn.disabled;
    btn.disabled = true;
    btn.innerHTML = SP.html(text);
    return function () {
      btn.disabled = wasDisabled;
      btn.innerHTML = saved;
    };
  };

  // SP.banner is a full-width progress line, optionally with a done/total bar and the current item.
  SP.banner = function (text, t0, done, total, current) {
    const bar = total > 0
      ? '<div style="height:4px; background: var(--card-border); border-radius: 2px; margin-top:8px; overflow:hidden;">' +
        '<div style="height:100%; width:' + Math.min(100, Math.round((done / total) * 100)) + '%; background: var(--primary); transition: width .3s;"></div></div>'
      : '';
    const cur = current ? '<div style="font-family: var(--font-mono); font-size: var(--fs-xs); color: var(--text-muted); margin-top:4px; word-break: break-all;">' + escapeHtml(String(current)) + '</div>' : '';
    return '<div class="card" role="status" aria-live="polite" style="padding:10px 14px; margin-bottom:12px;">' +
      '<div style="display:flex; align-items:center; gap:6px;">' + SP.html(text, t0) + '</div>' + bar + cur + '</div>';
  };

  // SP.loading is a centered placeholder for a slow GET view: spinner, text and elapsed seconds.
  SP.loading = function (text, t0) {
    return '<div role="status" aria-live="polite" style="display:flex; align-items:center; justify-content:center; gap:6px; padding:30px; color: var(--text-muted);">' + SP.html(text, t0) + '</div>';
  };

  // SP.skeleton returns shimmering table-like rows (use under SP.loading, or alone while a list reloads).
  SP.skeleton = function (rows) {
    let h = '<div class="sp-skel" aria-hidden="true">';
    for (let i = 0; i < (rows || 5); i++) h += '<div class="sp-skel-row"><i></i><i></i><i></i></div>';
    return h + '</div>';
  };
})();
