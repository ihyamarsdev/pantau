/* Modal helpers, documentation reader, and live activity console (SSE).
 * Native ES module fragment of the former monolithic inline script.
 * Air-gapped: no bundler, no CDN. Shared state lives in the mutable S object
 * from 00-state.js; cross-domain functions are imported explicitly. */
import { S } from './00-state.js';
import { svgIcon, debounce } from './00-foundation.js';
import { t, resolveConfirm } from './01-i18n-auth.js';
import { showToast } from './02-hosts.js';
import { stopDockerLogs, applyEditorMaximizeState, minimizeHostDetail, minimizeEditor, restoreWorkspaceSession, renderWorkspaceShelf } from './03-workspace-files-editor.js';
import { activateTerminalTab, toggleTabSplit, closeTerminalTab, minimizeTerminalDock } from './04-terminal.js';
import { stopKubeLogs } from './09-kube.js';
import { stopPkgUpgradeLogs } from './10-bulk-pkg.js';
import { tfDrawerOpen, closeTfWorkspace } from './12-terraform.js';

function openModal(id) {
  const el = document.getElementById(id);
  if (!el) return;
  S.modalZIndexCounter += 10;
  el.style.zIndex = S.modalZIndexCounter;
  el.classList.add('open');
  if (!S.openModalStack.includes(id)) {
    S.openModalStack.push(id);
  }
}

function closeModal(id) {
  if (id === 'transfersListModal' && S.transferPollInterval) {
    clearInterval(S.transferPollInterval);
    S.transferPollInterval = null;
  }
  if (id === 'dockerLogsModal') {
    stopDockerLogs();
  }
  if (id === 'kubeLogsModal') {
    stopKubeLogs();
  }
  if (id === 'pkgUpgradeLogsModal') {
    stopPkgUpgradeLogs();
  }
  if (id === 'netDiagModal' && S.currentNetDiagSSE) {
    try { S.currentNetDiagSSE.close(); } catch (e) {}
    S.currentNetDiagSSE = null;
  }
  if (id === 'terminalDockModal') {
    minimizeTerminalDock();
    return;
  }
  if (id === 'confirmModal' && S.confirmResolver) {
    resolveConfirm(false);
    return;
  }
  if (id === 'hostDetailModal') {
    if (S.currentHost) {
      S.workspaceSessions = S.workspaceSessions.filter(s => !(s.type === 'host' && s.hostId === S.currentHost.id));
      renderWorkspaceShelf();
    }
  }
  if (id === 'editorModal') {
    if (S.isEditorMaximized) {
      S.isEditorMaximized = false;
      applyEditorMaximizeState();
    }
    if (S.currentEditorState && S.currentEditorState.path) {
      const sessId = 'editor-' + S.currentEditorState.hostId + '-' + S.currentEditorState.path;
      S.workspaceSessions = S.workspaceSessions.filter(s => s.id !== sessId);
      renderWorkspaceShelf();
    }
  }
  const el = document.getElementById(id);
  if (el) {
    el.classList.remove('open');
    el.style.zIndex = '';
  }
  S.openModalStack = S.openModalStack.filter(mId => mId !== id);
}

window.addEventListener('click', (e) => {
  const menu = document.getElementById('newTabDropdownMenu');
  const btn = document.getElementById('btnNewTerminalTab');
  if (menu && menu.style.display === 'block') {
    if (!menu.contains(e.target) && (!btn || !btn.contains(e.target))) {
      menu.style.display = 'none';
    }
  }

  document.querySelectorAll('.terminal-dropdown-menu').forEach(m => {
    if (m.style.display === 'block') {
      const wrap = m.closest('.terminal-pins-wrapper, .terminal-presets-wrapper, .terminal-ports-wrapper');
      if (!wrap || !wrap.contains(e.target)) {
        m.style.display = 'none';
      }
    }
  });

  if (e.target && e.target.classList && e.target.classList.contains('modal') && e.target.classList.contains('open')) {
    if (e.target.id === 'confirmModal') {
      resolveConfirm(false);
    } else if (e.target.id === 'editorModal') {
      minimizeEditor();
    } else if (e.target.id === 'hostDetailModal') {
      minimizeHostDetail();
    } else {
      closeModal(e.target.id);
    }
  }
});

window.addEventListener('keydown', (e) => {
  // Global Terminal Dock Keyboard Shortcuts
  const dock = document.getElementById('terminalDockModal');
  const isDockOpen = dock && dock.style.display !== 'none';

  if (isDockOpen && e.altKey && !e.ctrlKey && !e.shiftKey) {
    if (e.key >= '1' && e.key <= '9') {
      const idx = parseInt(e.key, 10) - 1;
      if (idx >= 0 && idx < S.terminalTabs.length) {
        e.preventDefault();
        activateTerminalTab(S.terminalTabs[idx].id);
        return;
      }
    } else if (e.key.toLowerCase() === 'w') {
      e.preventDefault();
      if (S.activeTerminalTabId) {
        closeTerminalTab(e, S.activeTerminalTabId);
      }
      return;
    } else if (e.key === '\\') {
      e.preventDefault();
      if (S.activeTerminalTabId) {
        toggleTabSplit(S.activeTerminalTabId);
      }
      return;
    } else if (e.key.toLowerCase() === 'm') {
      e.preventDefault();
      minimizeTerminalDock();
      return;
    }
  }

  // Workspace Shelf Alt+1..5 Shortcuts (ADR-0026)
  if (!isDockOpen && e.altKey && !e.ctrlKey && !e.shiftKey) {
    if (e.key >= '1' && e.key <= '5') {
      const sIdx = parseInt(e.key, 10) - 1;
      if (sIdx >= 0 && sIdx < S.workspaceSessions.length) {
        e.preventDefault();
        restoreWorkspaceSession(S.workspaceSessions[sIdx].id);
        return;
      }
    }
  }

  if (e.key === 'Escape') {
    // ponytail: never intercept Escape if Terminal Dock is open and no other modal is stacked above it (ADR-0011)
    if (isDockOpen && (S.openModalStack.length <= 1 && (S.openModalStack[0] === 'terminalDockModal' || S.openModalStack.length === 0))) return;
    if (S.openModalStack.length > 0) {
      const topModalId = S.openModalStack[S.openModalStack.length - 1];
      if (topModalId === 'confirmModal') {
        resolveConfirm(false);
      } else if (topModalId === 'editorModal') {
        minimizeEditor();
      } else if (topModalId === 'hostDetailModal') {
        minimizeHostDetail();
      } else {
        closeModal(topModalId);
      }
      return;
    }
    const openModals = Array.from(document.querySelectorAll('.modal.open'));
    if (openModals.length > 0) {
      const topModal = openModals[openModals.length - 1];
      if (topModal.id === 'confirmModal') {
        resolveConfirm(false);
      } else {
        closeModal(topModal.id);
      }
      return;
    }
    if (tfDrawerOpen()) closeTfWorkspace();
  }
});

function escapeHtml(s) {
  if (!s) return '';
  return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

function jsArg(v) {
  return escapeHtml(JSON.stringify(v == null ? '' : String(v)));
}

function updateDocsLangButton() {
  const btn = document.getElementById('docsLangToggleBtn');
  if (!btn) return;
  if (S.currentDoc === 'changelog') {
    btn.innerHTML = svgIcon('globe', 'icon-sm') + ' <span>EN</span>';
    btn.disabled = true;
    btn.style.opacity = '0.5';
    btn.style.cursor = 'not-allowed';
    btn.title = t('docs_lang_en_only', 'Available in English only');
  } else {
    btn.disabled = false;
    btn.style.opacity = '1';
    btn.style.cursor = 'pointer';
    btn.title = t('switch_language', 'Switch Language');
    btn.innerHTML = svgIcon('globe', 'icon-sm') + ' <span>' + (S.docsModalLang === 'en' ? 'EN' : 'ID') + '</span>';
  }
}

function openDocumentationModal(docName) {
  if (docName) {
    S.currentDoc = docName;
  }
  S.docsModalLang = S.currentLang || 'en';
  updateDocsLangButton();
  const tabReadme = document.getElementById('tabDocReadme');
  const tabGuide = document.getElementById('tabDocGuide');
  const tabChangelog = document.getElementById('tabDocChangelog');
  if (tabReadme) tabReadme.classList.toggle('active', S.currentDoc === 'readme');
  if (tabGuide) tabGuide.classList.toggle('active', S.currentDoc === 'guide');
  if (tabChangelog) tabChangelog.classList.toggle('active', S.currentDoc === 'changelog');
  openModal('documentationModal');
  loadDocContent(S.currentDoc, S.docsModalLang);
}

function toggleDocsLanguage() {
  if (S.currentDoc === 'changelog') return;
  S.docsModalLang = S.docsModalLang === 'en' ? 'id' : 'en';
  updateDocsLangButton();
  loadDocContent(S.currentDoc, S.docsModalLang);
}

function switchDoc(name, lang) {
  S.currentDoc = name;
  if (lang) {
    S.docsModalLang = lang;
  }
  updateDocsLangButton();
  const tabReadme = document.getElementById('tabDocReadme');
  const tabGuide = document.getElementById('tabDocGuide');
  const tabChangelog = document.getElementById('tabDocChangelog');
  if (tabReadme) tabReadme.classList.toggle('active', name === 'readme');
  if (tabGuide) tabGuide.classList.toggle('active', name === 'guide');
  if (tabChangelog) tabChangelog.classList.toggle('active', name === 'changelog');
  loadDocContent(S.currentDoc, S.docsModalLang);
}

function sanitizeHtml(htmlStr) {
  let clean = htmlStr.replace(/<\/?(script|style|iframe|object|embed)[^>]*>/gi, '');
  clean = clean.replace(/\s+on\w+\s*=\s*(["'][^"']*["']|[^\s>]+)/gi, '');
  clean = clean.replace(/<img\s+([^>]*?)(\/?>)/gi, (m, attrs, end) => {
    if (!attrs.includes('onerror')) {
      return `<img ${attrs} onerror="this.style.display='none'" ${end}`;
    }
    return m;
  });
  clean = clean.replace(/href=["']([^"']*(?:README|user-guide|CHANGELOG)[^"']*\.md)["']/gi, (m, target) => {
    if (target.includes('CHANGELOG')) {
      return `href="javascript:void(0)" onclick="switchDoc('changelog', 'en')"`;
    }
    const isGuide = target.includes('user-guide');
    const isId = target.includes('.id.');
    const docName = isGuide ? 'guide' : 'readme';
    const docLang = isId ? 'id' : 'en';
    return `href="javascript:void(0)" onclick="switchDoc('${docName}', '${docLang}')"`;
  });
  return clean;
}

function parseMicroMarkdown(md) {
  const toc = [];
  const lines = md.split('\n');
  const output = [];
  let inCodeBlock = false;
  let codeBlockLang = '';
  let codeBuffer = [];
  let inList = false;
  let listType = '';
  let inTable = false;
  let tableHeaderDone = false;

  function closeList() {
    if (inList) {
      output.push(listType === 'ul' ? '</ul>' : '</ol>');
      inList = false;
      listType = '';
    }
  }

  function closeTable() {
    if (inTable) {
      output.push('</tbody></table>');
      inTable = false;
      tableHeaderDone = false;
    }
  }

  function inlineFormat(text) {
    const codeTokens = [];
    let formatted = text.replace(/`([^`]+)`/g, (_, code) => {
      codeTokens.push(`<code>${escapeHtml(code)}</code>`);
      return `___CODE_${codeTokens.length - 1}___`;
    });

    formatted = sanitizeHtml(formatted);

    formatted = formatted.replace(/\*\*\*([^*]+)\*\*\*/g, '<strong><em>$1</em></strong>');
    formatted = formatted.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    formatted = formatted.replace(/\*([^*]+)\*/g, '<em>$1</em>');
    formatted = formatted.replace(/\[([^\]]+)\]\(([^)]+)\)/g, (match, label, href) => {
      if (href.startsWith('#')) {
        const anchor = href.substring(1).replace(/[^a-zA-Z0-9_-]/g, '');
        return `<a href="#${anchor}" onclick="scrollToDocAnchor('${anchor}'); return false;">${label}</a>`;
      }
      if (href.includes('README') || href.includes('user-guide')) {
        const isGuide = href.includes('user-guide');
        const isId = href.includes('.id.');
        const docName = isGuide ? 'guide' : 'readme';
        const docLang = isId ? 'id' : 'en';
        return `<a href="javascript:void(0)" onclick="switchDoc('${docName}', '${docLang}')">${label}</a>`;
      }
      return `<a href="${href}" target="_blank" rel="noopener noreferrer">${label}</a>`;
    });

    formatted = formatted.replace(/___CODE_(\d+)___/g, (_, idx) => codeTokens[Number(idx)]);
    return formatted;
  }

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];

    if (line.trim().startsWith('```')) {
      closeList();
      closeTable();
      if (!inCodeBlock) {
        inCodeBlock = true;
        codeBlockLang = line.trim().substring(3).trim();
        codeBuffer = [];
      } else {
        inCodeBlock = false;
        const langAttr = codeBlockLang ? ` class="language-${escapeHtml(codeBlockLang)}"` : '';
        output.push(`<pre><code${langAttr}>${escapeHtml(codeBuffer.join('\n'))}</code></pre>`);
        codeBuffer = [];
      }
      continue;
    }

    if (inCodeBlock) {
      codeBuffer.push(line);
      continue;
    }

    const trimmed = line.trim();

    if (!trimmed) {
      closeList();
      closeTable();
      continue;
    }

    // HTML Block detection (starts with HTML tag)
    if (/^<\/?([a-zA-Z0-9]+)(\s+[^>]*)?>/.test(trimmed)) {
      closeList();
      closeTable();
      const hMatch = trimmed.match(/^<h([1-6])[^>]*>(.*?)<\/h\1>/i);
      if (hMatch) {
        const level = parseInt(hMatch[1], 10);
        const textOnly = hMatch[2].replace(/<[^>]+>/g, '').trim();
        const slug = textOnly.toLowerCase().replace(/[^\w\s-]/g, '').replace(/\s+/g, '-');
        if (level === 2 || level === 3) {
          toc.push({ id: slug, text: textOnly, level: level });
        }
        output.push(sanitizeHtml(trimmed.replace(/^<h([1-6])([^>]*)>/i, `<h$1$2 id="${slug}">`)));
      } else {
        output.push(sanitizeHtml(trimmed));
      }
      continue;
    }

    if (/^(\*\*\*|---|___)$/.test(trimmed)) {
      closeList();
      closeTable();
      output.push('<hr />');
      continue;
    }

    const headingMatch = trimmed.match(/^(#{1,4})\s+(.+)$/);
    if (headingMatch) {
      closeList();
      closeTable();
      const level = headingMatch[1].length;
      const rawText = headingMatch[2].trim();
      const slug = rawText.toLowerCase().replace(/[^\w\s-]/g, '').replace(/\s+/g, '-');
      if (level === 2 || level === 3) {
        toc.push({ id: slug, text: rawText, level: level });
      }
      output.push(`<h${level} id="${slug}">${inlineFormat(rawText)}</h${level}>`);
      continue;
    }

    if (trimmed.startsWith('>')) {
      closeList();
      closeTable();
      const quoteText = trimmed.replace(/^>\s*/, '');
      output.push(`<blockquote><p>${inlineFormat(quoteText)}</p></blockquote>`);
      continue;
    }

    if (trimmed.startsWith('|') && trimmed.endsWith('|')) {
      closeList();
      const cells = trimmed.split('|').slice(1, -1).map(c => c.trim());
      if (cells.every(c => /^:?-+:?$/.test(c))) {
        tableHeaderDone = true;
        continue;
      }
      if (!inTable) {
        inTable = true;
        tableHeaderDone = false;
        output.push('<table><thead><tr>');
        cells.forEach(c => output.push(`<th>${inlineFormat(c)}</th>`));
        output.push('</tr></thead><tbody>');
        continue;
      } else {
        output.push('<tr>');
        cells.forEach(c => output.push(`<td>${inlineFormat(c)}</td>`));
        output.push('</tr>');
        continue;
      }
    } else {
      closeTable();
    }

    const ulMatch = trimmed.match(/^[-*]\s+(.+)$/);
    const olMatch = trimmed.match(/^(\d+)\.\s+(.+)$/);
    if (ulMatch || olMatch) {
      const type = ulMatch ? 'ul' : 'ol';
      const itemContent = ulMatch ? ulMatch[1] : olMatch[2];
      if (!inList || listType !== type) {
        closeList();
        inList = true;
        listType = type;
        output.push(type === 'ul' ? '<ul>' : '<ol>');
      }
      output.push(`<li>${inlineFormat(itemContent)}</li>`);
      continue;
    } else {
      closeList();
    }

    output.push(`<p>${inlineFormat(trimmed)}</p>`);
  }

  closeList();
  closeTable();
  return { html: output.join('\n'), toc };
}

async function loadDocContent(name, lang) {
  const cacheKey = `${name}_${lang}`;
  const bodyEl = document.getElementById('docsContentBody');
  const tocEl = document.getElementById('docsTocList');

  if (S.docsCache[cacheKey]) {
    renderDocData(S.docsCache[cacheKey]);
    return;
  }

  if (bodyEl) {
    bodyEl.innerHTML = `<div class="empty-state" style="padding: 30px;"><div class="spinning" style="font-size: 1.5rem;">⏳</div><div class="empty-state-desc">${t('doc_loading')}</div></div>`;
  }
  if (tocEl) tocEl.innerHTML = '';

  try {
    const res = await fetch(`/api/docs?name=${encodeURIComponent(name)}&lang=${encodeURIComponent(lang)}`);
    if (!res.ok) throw new Error('Status ' + res.status);
    const md = await res.text();
    const parsed = parseMicroMarkdown(md);
    S.docsCache[cacheKey] = parsed;
    renderDocData(parsed);
  } catch (err) {
    if (bodyEl) {
      bodyEl.innerHTML = `<div class="empty-state" style="padding: 30px;"><div class="text-danger">${svgIcon('triangle-alert', 'icon-xl')}</div><div class="empty-state-desc">${t('doc_error')}</div></div>`;
    }
  }
}

function renderDocData(parsed) {
  const bodyEl = document.getElementById('docsContentBody');
  const tocEl = document.getElementById('docsTocList');
  if (bodyEl) bodyEl.innerHTML = parsed.html;

  if (tocEl) {
    tocEl.innerHTML = '';
    if (parsed.toc.length === 0) {
      tocEl.innerHTML = '<span style="font-size: 0.75rem; color: var(--text-muted); padding: 4px 8px;">-</span>';
    } else {
      parsed.toc.forEach(item => {
        const a = document.createElement('a');
        a.className = `docs-toc-item level-${item.level}`;
        a.innerText = item.text;
        a.href = '#' + item.id;
        a.onclick = (e) => {
          e.preventDefault();
          scrollToDocAnchor(item.id);
        };
        tocEl.appendChild(a);
      });
    }
  }
  const pane = document.getElementById('docsContentPane');
  if (pane) pane.scrollTop = 0;
}

function scrollToDocAnchor(id) {
  const target = document.getElementById(id);
  if (target) {
    target.scrollIntoView({ behavior: 'smooth', block: 'start' });
    document.querySelectorAll('.docs-toc-item').forEach(el => {
      el.classList.toggle('active', el.getAttribute('href') === '#' + id);
    });
  }
}

function connectActivityEventSource() {
  // One delegated listener for all log rows instead of an onclick per row.
  const tbody = document.getElementById('activityTableBody');
  if (tbody && !tbody._rowDelegated) {
    tbody._rowDelegated = true;
    tbody.addEventListener('click', (e) => {
      const row = e.target.closest && e.target.closest('.activity-row[data-detail]');
      if (row) toggleActivityRowDetail(row.getAttribute('data-detail'));
    });
  }
  try {
    S.activityEventSource = new EventSource('/api/logs/stream');
    const dot = document.getElementById('footerActivityDot');
    const statusEl = document.getElementById('activityLiveStatus');

    S.activityEventSource.onopen = () => {
      if (dot) {
        dot.className = 'status-dot online';
        dot.title = 'Live Event Stream connected';
      }
      if (statusEl && !S.activityPaused) {
        statusEl.innerHTML = '<span class="activity-live-dot"></span><span>LIVE</span>';
        statusEl.className = 'activity-live-pill live';
      }
    };

    S.activityEventSource.onmessage = (e) => {
      if (!e.data) return;
      try {
        const evt = JSON.parse(e.data);
        handleIncomingActivityEvent(evt);
      } catch (err) {
        console.error('Failed to parse SSE log record:', err);
      }
    };

    S.activityEventSource.onerror = () => {
      if (dot) {
        dot.className = 'status-dot offline';
        dot.title = 'Reconnecting log stream...';
      }
      if (statusEl && !S.activityPaused) {
        statusEl.innerHTML = '<span class="activity-live-dot"></span><span>RECONNECTING</span>';
        statusEl.className = 'activity-live-pill reconnecting';
      }
    };
  } catch (err) {
    console.error('EventSource initialization error:', err);
  }
}

function handleIncomingActivityEvent(evt) {
  if (!evt) return;
  // FIFO ring buffer: keep max 500 items in browser memory
  if (S.activityLogs.length >= 500) {
    S.activityLogs.shift();
  }
  S.activityLogs.push(evt);

  // Track unread errors/warnings if drawer is closed
  const lvl = (evt.level || '').toUpperCase();
  if (!S.activityConsoleOpen && (lvl === 'ERROR' || lvl === 'WARN' || lvl === 'WARNING')) {
    S.activityUnreadErrors++;
    updateActivityErrorBadge();
  }

  // Do not waste CPU/memory rendering DOM when drawer is closed or paused
  if (S.activityPaused || !S.activityConsoleOpen) {
    return;
  }

  scheduleActivityRender();
}

function scheduleActivityRender() {
  if (S.activityRenderScheduled || !S.activityConsoleOpen || S.activityPaused) return;
  S.activityRenderScheduled = true;
  requestAnimationFrame(() => {
    S.activityRenderScheduled = false;
    renderActivityLogs();
  });
}

function updateActivityErrorBadge() {
  const badge = document.getElementById('footerActivityErrBadge');
  if (!badge) return;
  if (S.activityUnreadErrors > 0) {
    badge.textContent = S.activityUnreadErrors > 99 ? '99+' : S.activityUnreadErrors;
    badge.style.display = 'inline-block';
  } else {
    badge.style.display = 'none';
  }
}

function toggleActivityConsole(forceState) {
  const drawer = document.getElementById('activityConsoleDrawer');
  if (!drawer) return;
  S.activityConsoleOpen = typeof forceState === 'boolean' ? forceState : (drawer.style.display === 'none');

  if (S.activityConsoleOpen) {
    drawer.style.display = 'flex';
    S.activityUnreadErrors = 0;
    updateActivityErrorBadge();
    renderActivityLogs();
    if (S.activityAutoScroll) {
      scrollActivityToBottom();
    }
  } else {
    drawer.style.display = 'none';
  }
}

function toggleActivityMaximize() {
  const drawer = document.getElementById('activityConsoleDrawer');
  const btn = document.getElementById('activityMaximizeBtn');
  if (!drawer) return;
  const isMax = drawer.classList.toggle('maximized');
  if (btn) btn.innerHTML = isMax ? svgIcon('minimize-2', 'icon-xs') : svgIcon('maximize-2', 'icon-xs');
}

function toggleActivityPause() {
  S.activityPaused = !S.activityPaused;
  const btnIcon = document.getElementById('activityPauseIcon');
  const btnLabel = document.getElementById('activityPauseLabel');
  const statusEl = document.getElementById('activityLiveStatus');

  if (S.activityPaused) {
    if (btnIcon) btnIcon.innerHTML = svgIcon('play', 'icon-xs');
    if (btnLabel) btnLabel.textContent = 'Resume';
    if (statusEl) {
      statusEl.innerHTML = '<span class="activity-live-dot"></span><span>PAUSED</span>';
      statusEl.className = 'activity-live-pill paused';
    }
  } else {
    if (btnIcon) btnIcon.innerHTML = svgIcon('pause', 'icon-xs');
    if (btnLabel) btnLabel.textContent = 'Pause';
    if (statusEl) {
      statusEl.innerHTML = '<span class="activity-live-dot"></span><span>LIVE</span>';
      statusEl.className = 'activity-live-pill live';
    }
    renderActivityLogs();
    if (S.activityAutoScroll) {
      scrollActivityToBottom();
    }
  }
}

function setActivityLevelFilter(level) {
  S.activityLevelFilter = level;
  const levels = ['ALL', 'ERROR', 'WARN', 'INFO'];
  levels.forEach(lvl => {
    const btn = document.getElementById(`activityFilter${lvl.charAt(0) + lvl.slice(1).toLowerCase()}`);
    if (!btn) return;
    if (lvl === level) {
      btn.classList.add('active');
    } else {
      btn.classList.remove('active');
    }
  });
  renderActivityLogs();
}

function onActivitySearchInput(val) {
  S.activitySearchQuery = (val || '').toLowerCase().trim();
  renderActivityLogs();
}
const onActivitySearchInputDebounced = debounce(onActivitySearchInput, 120);

function clearActivityLogs() {
  S.activityLogs = [];
  S.activityUnreadErrors = 0;
  updateActivityErrorBadge();
  renderActivityLogs();
}

function handleActivityScroll() {
  const container = document.getElementById('activityTableContainer');
  if (!container) return;
  // Smart Auto-Scroll: if user scrolled away from bottom, pause auto-scroll
  const isAtBottom = container.scrollHeight - container.scrollTop - container.clientHeight < 30;
  S.activityAutoScroll = isAtBottom;
}

function scrollActivityToBottom() {
  const container = document.getElementById('activityTableContainer');
  if (container) {
    container.scrollTop = container.scrollHeight;
  }
}

function renderActivityLogs() {
  const tbody = document.getElementById('activityTableBody');
  const emptyState = document.getElementById('activityEmptyState');
  if (!tbody) return;

  const filtered = S.activityLogs.filter(item => {
    if (S.activityLevelFilter !== 'ALL') {
      const itemLvl = (item.level || '').toUpperCase();
      if (itemLvl !== S.activityLevelFilter) {
        if (!(S.activityLevelFilter === 'WARN' && itemLvl === 'WARNING')) {
          return false;
        }
      }
    }
    if (S.activitySearchQuery) {
      const msg = (item.message || '').toLowerCase();
      const mod = (item.module || '').toLowerCase();
      const host = (item.host || '').toLowerCase();
      const attrsStr = item.attrs ? JSON.stringify(item.attrs).toLowerCase() : '';
      if (!msg.includes(S.activitySearchQuery) && !mod.includes(S.activitySearchQuery) && !host.includes(S.activitySearchQuery) && !attrsStr.includes(S.activitySearchQuery)) {
        return false;
      }
    }
    return true;
  });

  if (filtered.length === 0) {
    tbody.innerHTML = '';
    if (emptyState) emptyState.style.display = 'block';
    return;
  }

  if (emptyState) emptyState.style.display = 'none';

  const rows = filtered.map(item => {
    const timeStr = formatActivityTime(item.timestamp);
    const lvlBadge = getActivityLevelBadge(item.level);
    const modBadge = getActivityModuleBadge(item.module);
    const hostStr = item.host ? escapeHtml(item.host) : '<span style="color:var(--text-faint);">-</span>';
    const metaStr = getActivityMetaString(item);
    const hasDetails = item.attrs && Object.keys(item.attrs).length > 0;
    const rowId = `act-row-${item.id}`;

    let detailRow = '';
    if (hasDetails) {
      detailRow = `
        <tr id="${rowId}-detail" style="display: none; background: var(--surface-1);">
          <td colspan="6" style="padding: 6px 12px;">
            <div class="activity-detail-box">${escapeHtml(JSON.stringify(item.attrs, null, 2))}</div>
          </td>
        </tr>
      `;
    }

    return `
      <tr class="activity-row" data-detail="${rowId}-detail">
        <td style="color: var(--text-muted); font-family: var(--font-mono); white-space: nowrap;">${escapeHtml(timeStr)}</td>
        <td style="white-space: nowrap;">${lvlBadge}</td>
        <td style="white-space: nowrap;">${modBadge}</td>
        <td style="white-space: nowrap; font-weight: 500; font-family: var(--font-mono); color: var(--text);">${hostStr}</td>
        <td style="word-break: break-all; color: var(--text); font-family: var(--font-mono);">${escapeHtml(item.message || '')}</td>
        <td style="text-align: right; color: var(--text-muted); font-family: var(--font-mono); white-space: nowrap;">${metaStr} ${hasDetails ? `<span style="font-size: 0.65rem; color: var(--text-muted);">${svgIcon('chevron-right', 'icon-xs')}</span>` : ''}</td>
      </tr>
      ${detailRow}
    `;
  }).join('');

  tbody.innerHTML = rows;

  if (S.activityAutoScroll) {
    scrollActivityToBottom();
  }
}

function toggleActivityRowDetail(id) {
  const el = document.getElementById(id);
  if (el) {
    el.style.display = el.style.display === 'none' ? 'table-row' : 'none';
  }
}

function formatActivityTime(ts) {
  if (!ts) return '--:--:--.---';
  const d = new Date(ts);
  if (isNaN(d.getTime())) return ts;
  const pad = (n, s=2) => String(n).padStart(s, '0');
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${pad(d.getMilliseconds(), 3)}`;
}

function getActivityLevelBadge(lvl) {
  const u = (lvl || '').toUpperCase();
  switch (u) {
    case 'ERROR':
      return '<span class="badge" style="background:var(--danger-soft); color:var(--danger); border:1px solid var(--danger-border); font-size:0.65rem; font-weight:600; padding:1px 5px;">ERROR</span>';
    case 'WARN':
    case 'WARNING':
      return '<span class="badge" style="background:var(--warning-soft); color:var(--warning); border:1px solid var(--warning-border); font-size:0.65rem; font-weight:600; padding:1px 5px;">WARN</span>';
    case 'DEBUG':
      return '<span class="badge" style="background:var(--info-soft); color:var(--info); border:1px solid var(--info-border); font-size:0.65rem; font-weight:600; padding:1px 5px;">DEBUG</span>';
    default:
      return '<span class="badge" style="background:var(--success-soft); color:var(--success); border:1px solid var(--success-border); font-size:0.65rem; font-weight:600; padding:1px 5px;">INFO</span>';
  }
}

function getActivityModuleBadge(mod) {
  const m = (mod || 'system').toLowerCase();
  let bg = 'var(--surface-2)', color = 'var(--text-muted)', border = 'var(--card-border)';
  if (m === 'inspect') { bg = 'var(--primary-soft)'; color = 'var(--accent-text)'; border = 'var(--card-border)'; }
  else if (m === 'agent') { bg = 'var(--info-soft)'; color = 'var(--info)'; border = 'var(--info-border)'; }
  else if (m === 'ssh') { bg = 'var(--primary-soft)'; color = 'var(--accent-text)'; border = 'var(--card-border)'; }
  else if (m === 'transfer') { bg = 'var(--success-soft)'; color = 'var(--success)'; border = 'var(--success-border)'; }
  else if (m === 'terminal') { bg = 'var(--warning-soft)'; color = 'var(--warning)'; border = 'var(--warning-border)'; }
  else if (m === 'snapshot') { bg = 'var(--info-soft)'; color = 'var(--info)'; border = 'var(--info-border)'; }
  else if (m === 'auth') { bg = 'var(--primary-soft)'; color = 'var(--accent-text)'; border = 'var(--card-border)'; }

  return `<span class="badge" style="background:${bg}; color:${color}; border:1px solid ${border}; font-size:0.65rem; font-weight:500; padding:1px 5px; text-transform:lowercase;">${escapeHtml(m)}</span>`;
}

function getActivityMetaString(item) {
  if (!item.attrs) return '';
  const parts = [];
  if (item.attrs.tool !== undefined) parts.push(item.attrs.tool);
  if (item.attrs.domain !== undefined) parts.push(item.attrs.domain);
  if (item.attrs.status !== undefined) parts.push(item.attrs.status);
  if (item.attrs.duration_ms !== undefined) parts.push(`${item.attrs.duration_ms}ms`);
  if (item.attrs.prompt_size !== undefined) parts.push(`in ${item.attrs.prompt_size}`);
  if (item.attrs.reply_size !== undefined) parts.push(`out ${item.attrs.reply_size}`);
  if (item.attrs.tools !== undefined) parts.push(`${item.attrs.tools} tools`);
  if (item.attrs.drift_rules !== undefined) parts.push(`${item.attrs.drift_rules} drift`);
  if (item.attrs.port !== undefined) parts.push(`port ${item.attrs.port}`);
  return parts.join(' • ');

}

function toggleActivityExportMenu() {
  const menu = document.getElementById('activityExportMenu');
  if (menu) {
    menu.style.display = menu.style.display === 'none' ? 'block' : 'none';
  }
}

function exportActivityLogs(format) {
  const menu = document.getElementById('activityExportMenu');
  if (menu) menu.style.display = 'none';

  if (S.activityLogs.length === 0) {
    showToast('No logs to export', 'warning');
    return;
  }

  let content = '';
  let filename = `pantau-activity-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-')}`;

  if (format === 'json') {
    content = JSON.stringify(S.activityLogs, null, 2);
    filename += '.json';
  } else {
    content = S.activityLogs.map(item => {
      const ts = formatActivityTime(item.timestamp);
      const lvl = (item.level || 'INFO').toUpperCase().padEnd(5, ' ');
      const mod = (item.module || 'system').padEnd(8, ' ');
      const host = (item.host || '-').padEnd(14, ' ');
      const attrsStr = item.attrs ? ' ' + JSON.stringify(item.attrs) : '';
      return `[${ts}] [${lvl}] [${mod}] [${host}] ${item.message || ''}${attrsStr}`;
    }).join('\n');
    filename += '.log';
  }

  const blob = new Blob([content], { type: format === 'json' ? 'application/json' : 'text/plain' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}

window.addEventListener('keydown', (e) => {
  if (e.ctrlKey && (e.key === '`' || e.code === 'Backquote')) {
    e.preventDefault();
    toggleActivityConsole();
  }
});

document.addEventListener('click', (e) => {
  const menu = document.getElementById('activityExportMenu');
  if (menu && menu.style.display !== 'none' && !e.target.closest('#activityExportMenu') && !e.target.closest('button[onclick*="toggleActivityExportMenu"]')) {
    menu.style.display = 'none';
  }
});

export { openModal, closeModal, escapeHtml, jsArg, updateDocsLangButton, openDocumentationModal, toggleDocsLanguage, switchDoc, sanitizeHtml, parseMicroMarkdown, loadDocContent, renderDocData, scrollToDocAnchor, connectActivityEventSource, handleIncomingActivityEvent, scheduleActivityRender, updateActivityErrorBadge, toggleActivityConsole, toggleActivityMaximize, toggleActivityPause, setActivityLevelFilter, onActivitySearchInput, onActivitySearchInputDebounced, clearActivityLogs, handleActivityScroll, scrollActivityToBottom, renderActivityLogs, toggleActivityRowDetail, formatActivityTime, getActivityLevelBadge, getActivityModuleBadge, getActivityMetaString, toggleActivityExportMenu, exportActivityLogs };
