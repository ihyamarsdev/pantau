/* Terraform Registry: minimal, safe Markdown renderer for UNTRUSTED README text.
 * Everything is HTML-escaped first; only elements built here reach the DOM. Raw HTML in the source is dropped,
 * images are not loaded (shown as their alt text) and links are limited to http(s). */
(function () {
  'use strict';
  const R = (window.TfUI = window.TfUI || {}).reg;

  const URL_RE = /^https?:\/\/[^\s"'<>]+$/i;

  // Inline spans on already-escaped text; code spans are lifted out first so their content stays literal.
  function inline(esc) {
    const codes = [];
    let s = esc.replace(/`([^`]+)`/g, (m, c) => { codes.push(c); return '\u0000' + (codes.length - 1) + '\u0000'; });
    s = s.replace(/!\[([^\]]*)\]\(([^)\s]*)[^)]*\)/g, (m, alt) => (alt ? `<span class="tf-md-alt">${alt}</span>` : ''));
    s = s.replace(/\[([^\]]+)\]\(([^)\s]+)[^)]*\)/g, (m, text, url) => {
      const raw = url.replace(/&amp;/g, '&');
      return URL_RE.test(raw) ? `<a href="${url}" target="_blank" rel="noopener noreferrer">${text}</a>` : text;
    });
    s = s.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>').replace(/(^|[\s(])_([^_]+)_(?=[\s).,;:!?]|$)/g, '$1<em>$2</em>');
    return s.replace(/\u0000(\d+)\u0000/g, (m, i) => `<code>${codes[Number(i)]}</code>`);
  }

  // Drop HTML tags outside `code` spans; the odd split segments are the code spans themselves.
  const stripHtml = (line) => line.split(/(`[^`]*`)/).map((p, i) => (i % 2 ? p : p.replace(/<\/?[A-Za-z][^>]*>/g, '').replace(/<!--.*?-->/g, ''))).join('');

  function table(rows) {
    const cells = (r) => r.replace(/^\||\|$/g, '').split('|').map(c => inline(tfE(stripHtml(c.trim()))));
    const head = cells(rows[0]);
    const body = rows.slice(2).map(cells);
    return `<table class="tf-md-table"><thead><tr>${head.map(c => `<th>${c}</th>`).join('')}</tr></thead><tbody>`
      + body.map(r => `<tr>${r.map(c => `<td>${c}</td>`).join('')}</tr>`).join('') + '</tbody></table>';
  }

  R.md = function (src) {
    const out = [];
    const lines = String(src || '').replace(/<!--[\s\S]*?-->/g, '').split('\n');
    let fence = null, list = null, para = [];
    const flushPara = () => { if (para.length) { out.push(`<p>${para.join(' ')}</p>`); para = []; } };
    const closeList = () => { if (list) { out.push(`</${list}>`); list = null; } };
    for (let i = 0; i < lines.length; i++) {
      const line = lines[i];
      if (fence !== null) {
        if (/^\s*```/.test(line)) { out.push(`<pre class="tf-reg-pre">${tfE(fence.join('\n'))}</pre>`); fence = null; } else fence.push(line);
        continue;
      }
      if (/^\s*```/.test(line)) { flushPara(); closeList(); fence = []; continue; }
      if (/^\s*\|/.test(line) && /^\s*\|?[\s:|-]+\|[\s:|-]*$/.test(lines[i + 1] || '')) {
        flushPara(); closeList();
        const rows = [line]; while (i + 1 < lines.length && /^\s*\|/.test(lines[i + 1])) rows.push(lines[++i]);
        out.push(table(rows)); continue;
      }
      const clean = stripHtml(line);
      const h = /^(#{1,6})\s+(.*)$/.exec(clean);
      const li = /^\s*([-*+]|\d+\.)\s+(.*)$/.exec(clean);
      if (h) { flushPara(); closeList(); out.push(`<div class="tf-md-h tf-md-h${h[1].length}">${inline(tfE(h[2]))}</div>`); }
      else if (li) {
        flushPara(); const tag = /\d/.test(li[1]) ? 'ol' : 'ul';
        if (list !== tag) { closeList(); out.push(`<${tag}>`); list = tag; }
        out.push(`<li>${inline(tfE(li[2]))}</li>`);
      } else if (/^\s*>/.test(clean)) { flushPara(); closeList(); out.push(`<blockquote>${inline(tfE(clean.replace(/^\s*>\s?/, '')))}</blockquote>`); }
      else if (/^\s*([-*_])\1{2,}\s*$/.test(clean)) { flushPara(); closeList(); out.push('<hr>'); }
      else if (!clean.trim()) { flushPara(); closeList(); }
      else para.push(inline(tfE(clean.trim())));
    }
    if (fence !== null) out.push(`<pre class="tf-reg-pre">${tfE(fence.join('\n'))}</pre>`); // truncated README: unclosed fence
    flushPara(); closeList();
    return out.join('');
  };

  R.toggleRaw = function () {
    const raw = document.getElementById('tfRegReadmeRaw'), view = document.getElementById('tfRegReadme'), btn = document.getElementById('tfRegReadmeToggle');
    if (!raw || !view || !btn) return;
    raw.hidden = !raw.hidden; view.hidden = !raw.hidden;
    btn.textContent = tx(raw.hidden ? 'tfreg_readme_raw' : 'tfreg_readme_rendered');
  };
})();
