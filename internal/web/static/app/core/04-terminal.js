/* Global multi-tab terminal dock, WebSocket PTY sessions, presets, pins.
 * Native ES module fragment of the former monolithic inline script.
 * Air-gapped: no bundler, no CDN. Shared state lives in the mutable S object
 * from 00-state.js; cross-domain functions are imported explicitly. */
import { S } from './00-state.js';
import { svgIcon } from './00-foundation.js';
import { t, showConfirm } from './01-i18n-auth.js';
import { showToast, getHostFilesPath, setHostFilesPath } from './02-hosts.js';
import { openHostDetail, switchTab, openPinModal } from './03-workspace-files-editor.js';
import { openModal, closeModal, escapeHtml, jsArg } from './06-modal-docs-activity.js';

function getActiveTerminalTab() {
  return S.terminalTabs.find(t => t.id === S.activeTerminalTabId) || null;
}

function renderTerminalTabsList() {
  const container = document.getElementById('terminalTabsList');
  if (!container) return;
  if (S.terminalTabs.length === 0) {
    container.innerHTML = `<span style="font-size: var(--fs-xs); color: var(--text-muted); padding: 4px 8px;">No active tabs</span>`;
    return;
  }
  container.innerHTML = S.terminalTabs.map(tabItem => `
    <div class="terminal-dock-tab ${tabItem.id === S.activeTerminalTabId ? 'active-tab' : ''}" data-tab-id="${tabItem.id}" onclick="activateTerminalTab('${tabItem.id}')">
      <span class="terminal-tab-status-dot ${tabItem.connectedLeft ? '' : 'disconnected'}"></span>
      ${tabItem.hasActivity ? '<span class="terminal-tab-activity-dot" title="New output"></span>' : ''}
      <span id="tabTitleText_${tabItem.id}" class="terminal-tab-title" ondblclick="startRenameTab(event, '${tabItem.id}')" title="${t('rename_tab_hint')}">
        ${escapeHtml(tabItem.title)}
      </span>
      <button class="terminal-tab-close-btn" onclick="closeTerminalTab(event, '${tabItem.id}')" title="Close Tab (Alt+W)">
        ${svgIcon('x', 'icon-xs')}
      </button>
    </div>
  `).join('');
}

function startRenameTab(e, tabId) {
  if (e) e.stopPropagation();
  const tab = S.terminalTabs.find(t => t.id === tabId);
  if (!tab) return;
  const container = document.getElementById('tabTitleText_' + tabId);
  if (!container) return;

  const currentVal = tab.title || tab.hostName;
  const input = document.createElement('input');
  input.type = 'text';
  input.value = currentVal;
  input.className = 'tab-rename-input';

  let done = false;
  function save() {
    if (done) return;
    done = true;
    const val = input.value.trim();
    if (val) {
      tab.title = val;
    }
    saveTerminalTabsToStorage();
    renderTerminalTabsList();
  }

  input.addEventListener('blur', save);
  input.addEventListener('keydown', (ev) => {
    if (ev.key === 'Enter') {
      save();
    } else if (ev.key === 'Escape') {
      done = true;
      renderTerminalTabsList();
    }
  });

  container.innerHTML = '';
  container.appendChild(input);
  input.focus();
  input.select();
}

function activateTerminalTab(tabId) {
  S.activeTerminalTabId = tabId;
  const tab = S.terminalTabs.find(t => t.id === tabId);
  if (tab) {
    tab.hasActivity = false;
    S.currentPresetList = tab.presets || [];
    if (!tab.socketLeft || tab.socketLeft.readyState !== WebSocket.OPEN) {
      initTabLeftTerminal(tab, tab.presetID);
      loadTabPresets(tab);
    }
  }
  document.querySelectorAll('#terminalTabsList .terminal-dock-tab').forEach(el => {
    el.classList.toggle('active-tab', el.getAttribute('data-tab-id') === tabId);
  });
  const activeTabEl = document.querySelector(`#terminalTabsList .terminal-dock-tab[data-tab-id="${tabId}"]`);
  if (activeTabEl && typeof activeTabEl.scrollIntoView === 'function') {
    activeTabEl.scrollIntoView({ behavior: 'smooth', block: 'nearest', inline: 'nearest' });
  }
  document.querySelectorAll('#terminalTabsWorkspaces .terminal-tab-workspace').forEach(el => {
    el.classList.toggle('active-workspace', el.id === 'workspace_' + tabId);
  });
  updateTerminalSessionIndicators();
  refitActiveTab();
  focusActiveTerminal();
}

function focusActiveTerminal() {
  const tab = getActiveTerminalTab();
  if (!tab) return;
  const term = tab.focusedPane === 'right' && tab.isSplit ? tab.termRight : tab.termLeft;
  if (term) {
    setTimeout(() => {
      try {
        term.focus();
      } catch (e) {}
    }, 50);
  }
}

function refitActiveTab() {
  const tab = getActiveTerminalTab();
  if (!tab) return;
  setTimeout(() => {
    if (tab.fitLeft) tab.fitLeft.fit();
    if (tab.isSplit && tab.fitRight) tab.fitRight.fit();
  }, 80);
}

function createTabWorkspaceDOM(tab) {
  const container = document.getElementById('terminalTabsWorkspaces');
  if (!container) return;
  const ws = document.createElement('div');
  ws.id = 'workspace_' + tab.id;
  ws.className = 'terminal-tab-workspace';
  ws.innerHTML = `
    <div class="terminal-toolbar">
      <!-- Left side: Presets Dropdown -->
      <div class="terminal-toolbar-cluster">
        <div class="terminal-presets-wrapper" style="position: relative; display: inline-block;">
          <button class="btn btn-sm btn-ghost" onclick="toggleTerminalPresetsDropdown('${tab.id}', event)" title="${t('tab_settings_presets', 'Presets')}">
            ${svgIcon('zap', 'icon-sm')} <span id="lblPresets_${tab.id}">${t('tab_settings_presets', 'Presets')}</span> ${svgIcon('chevron-down', 'icon-xs')}
          </button>
          <div id="presetsDropdownMenu_${tab.id}" class="terminal-dropdown-menu" style="display: none; min-width: 280px; max-width: 380px; left: 0; right: auto;">
            <div class="terminal-dropdown-search">
              <input type="text" id="presetSearchInput_${tab.id}" class="terminal-dropdown-search-input" placeholder="${t('search_presets', 'Search presets...')}" oninput="filterTabPresets('${tab.id}')" onclick="event.stopPropagation()">
            </div>
            <div id="presetDropdownList_${tab.id}" class="terminal-dropdown-list">
              <div style="padding: 10px 12px; font-size: var(--fs-xs); color: var(--text-muted); text-align: center;">${t('loading', 'Loading...')}</div>
            </div>
            <div style="display: flex; align-items: center; justify-content: space-between; gap: 6px; padding: 6px 8px; border-top: 1px solid var(--card-border); background: var(--surface-1);">
              <button class="btn btn-sm btn-primary" onclick="openCreatePresetModal(null, ${tab.hostID}); toggleTerminalPresetsDropdown('${tab.id}', event)" style="font-size: var(--fs-xs); padding: 3px 8px; flex: 1;">
                ${svgIcon('plus', 'icon-xs')} ${t('add_preset', 'New Preset')}
              </button>
              <button class="btn btn-sm btn-ghost" onclick="openPresetManagerModal(${tab.hostID}); toggleTerminalPresetsDropdown('${tab.id}', event)" title="${t('manage_presets', 'Manage Presets')}" style="font-size: var(--fs-xs); padding: 3px 8px;">
                ${svgIcon('settings', 'icon-xs')} ${t('manage_presets', 'Manage')}
              </button>
            </div>
          </div>
        </div>
      </div>

      <!-- Right side: Cleanly grouped clusters -->
      <div class="terminal-toolbar-cluster" style="flex-wrap: wrap;">
        <!-- Cluster 1: Host Navigation & Shortcuts -->
        <div class="terminal-toolbar-cluster">
          <button class="btn btn-sm btn-ghost" onclick="openFileManagerForTab('${tab.id}')" title="${t('open_in_file_manager', 'Open in File Manager')}">
            ${svgIcon('folder', 'icon-sm')} <span id="lblFileManager_${tab.id}">${t('open_in_file_manager', 'File Manager')}</span>
          </button>
          <div class="terminal-pins-wrapper" style="position: relative; display: inline-block;">
            <button class="btn btn-sm btn-ghost" onclick="toggleTerminalPinsDropdown('${tab.id}', event)" title="${t('pinned_paths', 'Pinned Paths')}">
              ${svgIcon('pin', 'icon-sm')} <span id="lblPins_${tab.id}">${t('pinned_paths', 'Pins')}</span> ${svgIcon('chevron-down', 'icon-xs')}
            </button>
            <div id="pinsDropdownMenu_${tab.id}" class="terminal-dropdown-menu" style="display: none; min-width: 280px; max-width: 380px; right: 0;">
              <div class="terminal-dropdown-search">
                <input type="text" id="pinSearchInput_${tab.id}" class="terminal-dropdown-search-input" placeholder="${t('search_pins', 'Search pinned paths...')}" oninput="filterTerminalPins('${tab.id}')" onclick="event.stopPropagation()">
              </div>
              <div id="pinsDropdownList_${tab.id}" class="terminal-dropdown-list">
                <div style="padding: 10px 12px; font-size: var(--fs-xs); color: var(--text-muted); text-align: center;">${t('loading', 'Loading...')}</div>
              </div>
              <div style="padding: 6px 8px; border-top: 1px solid var(--card-border); background: var(--surface-1);">
                <button class="btn btn-sm btn-primary" onclick="openTerminalPinModal('${tab.id}')" style="width: 100%; font-size: var(--fs-xs); padding: 4px 8px;">
                  ${svgIcon('plus', 'icon-xs')} ${t('pin_new_path', 'Pin New Path')}
                </button>
              </div>
            </div>
          </div>
          <div class="terminal-ports-wrapper" style="position: relative; display: inline-block;">
            <button class="btn btn-sm btn-ghost" onclick="toggleTerminalPortsDropdown('${tab.id}', event)" title="Forwarded Ports / Remote Web Tunnel">
              ${svgIcon('globe', 'icon-sm')} <span id="lblPorts_${tab.id}">Ports</span> ${svgIcon('chevron-down', 'icon-xs')}
            </button>
            <div id="portsDropdownMenu_${tab.id}" class="terminal-dropdown-menu" style="display: none; min-width: 260px; max-width: 340px; right: 0;">
              <div style="padding: 8px 12px; border-bottom: 1px solid var(--card-border); font-size: var(--fs-xs); font-weight: 600; color: var(--text); display: flex; align-items: center; gap: 6px; background: var(--surface-1);">
                ${svgIcon('globe', 'icon-xs')} <span>Remote Web Tunnel</span>
              </div>
              <div style="padding: 10px 12px;">
                <label style="font-size: var(--fs-xs); color: var(--text-muted); display: block; margin-bottom: 6px;">${t('open_remote_port', 'Open Remote Port:')}</label>
                <div style="display: flex; gap: 6px;">
                  <input type="number" id="portInput_${tab.id}" placeholder="e.g. 8080, 3000" min="1" max="65535" style="flex: 1; padding: 4px 8px; font-size: var(--fs-xs); background: var(--surface-2); color: var(--text); border: 1px solid var(--card-border); border-radius: var(--radius-sm); font-family: var(--font-mono);" onclick="event.stopPropagation()" onkeydown="if(event.key==='Enter') openCustomTunnelForTab('${tab.id}')">
                  <button class="btn btn-sm btn-primary" onclick="openCustomTunnelForTab('${tab.id}')" style="padding: 4px 10px; font-size: var(--fs-xs); display: inline-flex; align-items: center; gap: 4px;">
                    ${svgIcon('external-link', 'icon-xs')} <span>${t('open_port_btn', 'Open')}</span>
                  </button>
                </div>
              </div>
              <div id="recentPortsSection_${tab.id}" style="border-top: 1px solid var(--card-border); padding: 8px 12px; font-size: var(--fs-xs);">
                <div style="color: var(--text-muted); margin-bottom: 6px; font-size: 0.7rem;">${t('recent_ports', 'Quick / Recent Ports:')}</div>
                <div id="recentPortsList_${tab.id}" style="display: flex; flex-wrap: wrap; gap: 6px;"></div>
              </div>
              ${window.PortFwd ? `<div style="border-top: 1px solid var(--card-border); padding: 8px 12px;"><button class="btn btn-sm" style="width: 100%; font-size: var(--fs-xs); display: inline-flex; align-items: center; justify-content: center; gap: 4px;" onclick="event.stopPropagation(); PortFwd.open(${tab.hostID}, document.getElementById('portInput_${tab.id}').value)">${svgIcon('plug', 'icon-xs')} <span>${t('pf_dropdown_btn', 'Forward port to this computer…')}</span></button></div>` : ''}
            </div>
          </div>
        </div>

        <div class="terminal-toolbar-divider"></div>

        <!-- Cluster 2: View Controls (Font Size) -->
        <div class="terminal-font-stepper" title="${t('terminal_font_size', 'Terminal Font Size')}">
          <button class="btn btn-sm" onclick="adjustTerminalFontSize(-1)" title="${t('font_size_decrease', 'Decrease font size')}">A-</button>
          <span class="term-font-size-val" style="font-size: 0.72rem; color: var(--text-muted); min-width: 26px; text-align: center; font-family: var(--font-mono);">13px</span>
          <button class="btn btn-sm" onclick="adjustTerminalFontSize(1)" title="${t('font_size_increase', 'Increase font size')}">A+</button>
        </div>

        <div class="terminal-toolbar-divider"></div>

        <!-- Cluster 3: Terminal Session Controls -->
        <div class="terminal-toolbar-cluster">
          <button class="btn btn-sm btn-ghost" onclick="toggleTabSplit('${tab.id}')" title="Split Terminal (Alt+\\)">
            ${svgIcon('columns-2', 'icon-sm')} <span id="lblSplit_${tab.id}">${t('split_terminal')}</span>
          </button>
          <button class="btn btn-sm btn-ghost" onclick="clearTabActiveTerminal('${tab.id}')" title="Clear screen">
            ${svgIcon('trash-2', 'icon-sm')} <span>Clear</span>
          </button>
          <button class="btn btn-sm btn-ghost" onclick="reconnectTabTerminal('${tab.id}')" title="Reconnect SSH">
            ${svgIcon('rotate-ccw', 'icon-sm')} <span>Reconnect</span>
          </button>
        </div>
      </div>
    </div>

    <div id="panesContainer_${tab.id}" class="terminal-panes-wrapper">
      <div id="paneLeft_${tab.id}" class="terminal-pane active-pane" onclick="focusTabPane('${tab.id}', 'left')" style="flex: 1; display: flex; flex-direction: column; min-width: 0; height: 100%; border-radius: var(--radius); overflow: hidden; background: var(--code-bg); position: relative;">
        <div class="terminal-pane-header">
          <div class="terminal-pane-title">${svgIcon('server', 'icon-xs')} <span>${escapeHtml(tab.hostName)}</span></div>
          <div id="statusLeft_${tab.id}" class="terminal-pane-status connecting"><span class="terminal-tab-status-dot"></span><span>${escapeHtml(t('terminal_connecting', 'Connecting…'))}</span></div>
        </div>
        <div id="termDivLeft_${tab.id}" class="terminal-container" style="flex: 1; height: 100%; padding: 4px;"></div>
      </div>

      <div id="paneRight_${tab.id}" class="terminal-pane" onclick="focusTabPane('${tab.id}', 'right')" style="display: none; flex: 1; flex-direction: column; min-width: 0; height: 100%; border-radius: var(--radius); overflow: hidden; background: var(--code-bg); position: relative;">
        <div class="terminal-pane-header">
          <div style="display: flex; align-items: center; gap: 6px;">
            <span class="terminal-pane-title">${svgIcon('server', 'icon-xs')} Split:</span>
            <select id="splitSelect_${tab.id}" class="input" style="padding: 2px 6px; font-size: 0.72rem; width: auto; max-width: 160px;" onchange="onTabSplitHostChange('${tab.id}')"></select>
          </div>
          <div style="display: flex; align-items: center; gap: 8px;">
            <div id="statusRight_${tab.id}" class="terminal-pane-status connecting"><span class="terminal-tab-status-dot"></span><span>${escapeHtml(t('terminal_connecting', 'Connecting…'))}</span></div>
            <button class="btn btn-sm btn-ghost" onclick="closeTabSplit('${tab.id}')" title="Close Split" style="padding: 2px;">${svgIcon('x', 'icon-xs')}</button>
          </div>
        </div>
        <div id="termDivRight_${tab.id}" class="terminal-container" style="flex: 1; height: 100%; padding: 4px;"></div>
      </div>
    </div>
  `;
  container.appendChild(ws);
  syncTerminalFontControls();
}

function getTerminalFontSize() {
  return parseInt(localStorage.getItem('pantau_term_font_size') || '13', 10);
}

function syncTerminalFontControls() {
  const size = getTerminalFontSize();
  document.querySelectorAll('.term-font-size-val').forEach(el => {
    el.textContent = size + 'px';
  });
}

function adjustTerminalFontSize(delta) {
  const current = getTerminalFontSize();
  const next = Math.max(10, Math.min(24, current + delta));
  localStorage.setItem('pantau_term_font_size', next);
  applyTerminalFontSize();
}

function applyTerminalFontSize() {
  const size = getTerminalFontSize();
  syncTerminalFontControls();
  S.terminalTabs.forEach(tab => {
    if (tab.termLeft) {
      tab.termLeft.options.fontSize = size;
      try { tab.termLeft.refresh(0, tab.termLeft.rows - 1); } catch(e) {}
      if (tab.fitLeft) {
        setTimeout(() => tab.fitLeft && tab.fitLeft.fit(), 30);
      }
    }
    if (tab.termRight) {
      tab.termRight.options.fontSize = size;
      try { tab.termRight.refresh(0, tab.termRight.rows - 1); } catch(e) {}
      if (tab.fitRight) {
        setTimeout(() => tab.fitRight && tab.fitRight.fit(), 30);
      }
    }
  });
}

async function copyTextToClipboard(text) {
  if (!text) return;
  if (navigator.clipboard && navigator.clipboard.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return;
    } catch (err) {}
  }
  const ta = document.createElement('textarea');
  ta.value = text;
  ta.style.position = 'fixed';
  ta.style.left = '-9999px';
  ta.style.top = '-9999px';
  ta.setAttribute('readonly', '');
  document.body.appendChild(ta);
  ta.focus();
  ta.select();
  try {
    document.execCommand('copy');
  } catch (e) {}
  document.body.removeChild(ta);
}

function hostIpCopyHtml(ip) {
  return `<span class="host-ip-copy" role="button" tabindex="0" data-copy-ip="${escapeHtml(ip)}" title="${escapeHtml(t('copy_ip', 'Click to copy IP'))}">${escapeHtml(ip)}${svgIcon('copy', 'icon-xs')}</span>`;
}

function onHostIpCopy(e) {
  const el = e.target.closest && e.target.closest('[data-copy-ip]');
  if (!el || (e.type === 'keydown' && e.key !== 'Enter' && e.key !== ' ')) return;
  e.preventDefault();
  e.stopPropagation();
  copyTextToClipboard(el.dataset.copyIp);
  showToast(t('ip_copied', 'IP copied') + ': ' + el.dataset.copyIp, 'success', 1500);
}

document.addEventListener('click', onHostIpCopy, true);

document.addEventListener('keydown', onHostIpCopy, true);

function flattenToSingleLine(text) {
  if (!text) return '';
  // Strip backslash line continuations: \ followed by optional spaces and newline
  let cleaned = text.replace(/\\\s*[\r\n]+/g, ' ');
  // Replace all remaining newlines with spaces
  cleaned = cleaned.replace(/[\r\n]+/g, ' ');
  // Collapse multiple spaces/tabs into a single space
  cleaned = cleaned.replace(/[ \t]+/g, ' ');
  // Strip leading and trailing whitespace/newlines so command doesn't auto-execute
  return cleaned.trim();
}

async function handleTerminalPaste(term, ws, explicitText = null) {
  let text = explicitText;
  if (text === null) {
    if (navigator.clipboard && navigator.clipboard.readText) {
      try {
        text = await navigator.clipboard.readText();
      } catch (e) {
        showToast(t('paste_http_notice', 'Use Ctrl+V or Ctrl+Shift+V to paste on HTTP connections'), 'warning');
        return;
      }
    } else {
      showToast(t('paste_http_notice', 'Use Ctrl+V or Ctrl+Shift+V to paste on HTTP connections'), 'warning');
      return;
    }
  }
  if (!text) return;

  // De-duplicate if clipboard API and native paste event both fire for the same paste
  const now = Date.now();
  if (text === S.lastTerminalPasteText && (now - S.lastTerminalPasteTime) < 200) {
    return;
  }
  S.lastTerminalPasteText = text;
  S.lastTerminalPasteTime = now;

  // Q6. B: Multiline paste confirmation protection & single-line flatten option
  if (text.includes('\n') || text.includes('\r')) {
    const rawLines = text.split(/\r\n|\r|\n/);
    const lineCount = rawLines.length;
    const preview = text.length > 300 ? text.substring(0, 300) + '...' : text;
    const promptMsg = t('confirm_multiline_paste', 'You are about to paste {lines} lines into the terminal:\n\n{preview}\n\nContinue paste?')
      .replace('{lines}', lineCount)
      .replace('{preview}', preview);
    const result = await showConfirm({
      title: t('multiline_paste_title', 'Paste Multiline Commands'),
      message: promptMsg,
      extraBtnText: t('paste_single_line', 'Flatten to Single Line'),
      extraBtnValue: 'single',
      extraBtnClass: 'btn-warning',
      confirmText: t('paste_as_is', 'Paste As Is'),
      danger: true
    });
    if (!result) {
      try { term.focus(); } catch (e) {}
      return;
    }
    if (result === 'single') {
      text = flattenToSingleLine(text);
    }
  }

  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(text);
  }
  try { term.focus(); } catch (e) {}
}

function setupTerminalClipboardBridge(term, ws, container) {
  if (!term) return;

  term.attachCustomKeyEventHandler((e) => {
    // 1. Copy: Ctrl+C (context aware) or Ctrl+Shift+C (always)
    if ((e.ctrlKey || e.metaKey) && (e.key === 'c' || e.key === 'C' || e.code === 'KeyC')) {
      if (e.shiftKey) {
        if (e.type === 'keydown') {
          const sel = term.getSelection();
          if (sel) copyTextToClipboard(sel);
        }
        e.preventDefault();
        return false;
      }
      if (term.hasSelection()) {
        if (e.type === 'keydown') {
          const sel = term.getSelection();
          if (sel) copyTextToClipboard(sel);
        }
        e.preventDefault();
        return false;
      }
      return true;
    }

    // 2. Paste: Ctrl+V or Ctrl+Shift+V
    if ((e.ctrlKey || e.metaKey) && (e.key === 'v' || e.key === 'V' || e.code === 'KeyV')) {
      if (e.type === 'keydown') {
        if (navigator.clipboard && navigator.clipboard.readText) {
          navigator.clipboard.readText().then(text => {
            if (text) handleTerminalPaste(term, ws, text);
          }).catch(() => {
            // Silently ignore if readText fails or is blocked on HTTP; native paste event will handle it!
          });
        }
      }
      // Return false to stop xterm.js from eating Ctrl+V as \x16 and calling preventDefault()!
      // Do NOT call e.preventDefault() here so the browser triggers the native DOM paste event on textarea!
      return false;
    }

    return true;
  });

  // 3. Copy-on-select: automatically copy selection to clipboard
  let selectionDebounce = null;
  term.onSelectionChange(() => {
    if (selectionDebounce) clearTimeout(selectionDebounce);
    selectionDebounce = setTimeout(() => {
      if (term.hasSelection()) {
        const sel = term.getSelection();
        if (sel && sel.trim().length > 0) {
          copyTextToClipboard(sel);
        }
      }
    }, 150);
  });

  // 4. Native DOM paste (capture phase) & Right-click paste on container and textarea
  const onPaste = (e) => {
    const pasted = e.clipboardData ? e.clipboardData.getData('text') : '';
    if (pasted) {
      e.preventDefault();
      e.stopPropagation();
      handleTerminalPaste(term, ws, pasted);
    }
  };

  if (container) {
    container.addEventListener('paste', onPaste, true);
    container.addEventListener('contextmenu', (e) => {
      e.preventDefault();
      handleTerminalPaste(term, ws);
    });
    container.addEventListener('click', () => {
      try { term.focus(); } catch (e) {}
    });
  }
  if (term.textarea) {
    term.textarea.addEventListener('paste', onPaste, true);
  }
  if (term.element) {
    term.element.addEventListener('paste', onPaste, true);
  }
}

function extractPathFromTerminalTitle(title) {
  if (!title || typeof title !== 'string') return null;
  let str = title.trim();
  str = str.replace(/[\x00\x07]/g, '').trim();
  const colonIdx = str.lastIndexOf(':');
  if (colonIdx !== -1) {
    str = str.substring(colonIdx + 1).trim();
  }
  str = str.replace(/[\$#]\s*$/, '').trim();
  if (!str) return null;
  if (str.startsWith('/') || str.startsWith('~')) {
    return str;
  }
  return null;
}

function setTabActivePath(tab, pane, path) {
  if (!tab || !path) return;
  if (pane === 'right') {
    tab.currentDirRight = path;
  } else {
    tab.currentDirLeft = path;
  }
}

function getTerminalActivePath(tabId) {
  const tab = S.terminalTabs.find(t => t.id === tabId);
  if (!tab) return '/';
  const isRight = (tab.isSplit && tab.focusedPane === 'right');
  const targetHostId = isRight ? (tab.hostIDRight || tab.hostID) : tab.hostID;
  const currentPath = isRight ? (tab.currentDirRight || tab.initialDir) : (tab.currentDirLeft || tab.initialDir);
  if (currentPath && currentPath !== '/') {
    return currentPath;
  }
  return tab.initialDir || getHostFilesPath(targetHostId) || '/';
}

function setupTerminalPathTracking(tab, pane, term) {
  if (!tab || !term) return;

  // 1. OSC 7 sequence handler (file://hostname/path)
  if (term.parser && term.parser.registerOscHandler) {
    term.parser.registerOscHandler(7, data => {
      try {
        let p = data;
        if (p.startsWith('file://')) {
          const match = p.match(/^file:\/\/[^\/]*(\/.*)$/);
          if (match) p = decodeURIComponent(match[1]);
        }
        if (p && (p.startsWith('/') || p.startsWith('~'))) {
          setTabActivePath(tab, pane, p);
        }
      } catch (e) {}
      return true;
    });
  }

  // 2. Shell Window Title changes (default bash/zsh prompt: \e]0;user@host: \w\a)
  if (term.onTitleChange) {
    term.onTitleChange(title => {
      const p = extractPathFromTerminalTitle(title);
      if (p) {
        setTabActivePath(tab, pane, p);
      }
    });
  }
}

function initTabLeftTerminal(tab, presetID) {
  if (tab.socketLeft) {
    try { tab.socketLeft.close(); } catch(e) {}
    tab.socketLeft = null;
  }
  if (tab.termLeft) {
    try { tab.termLeft.dispose(); } catch(e) {}
    tab.termLeft = null;
  }

  const container = document.getElementById('termDivLeft_' + tab.id);
  if (!container) return;
  container.innerHTML = '';

  const term = new Terminal({
    cursorBlink: true,
    fontFamily: S.TERMINAL_FONT_FAMILY,
    fontSize: getTerminalFontSize(),
    theme: S.XTERM_THEME
  });
  tab.termLeft = term;
  tab.fitLeft = new FitAddon.FitAddon();
  term.loadAddon(tab.fitLeft);
  term.open(container);
  if (term.element) term.element.style.fontFamily = S.TERMINAL_FONT_FAMILY;
  tab.fitLeft.fit();
  setTimeout(() => tab.fitLeft && tab.fitLeft.fit(), 60);
  if (document.fonts && document.fonts.ready) {
    document.fonts.ready.then(() => {
      if (tab.fitLeft) tab.fitLeft.fit();
    });
  }

  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  let wsUrl = protocol + '//' + location.host + '/ws/terminal?host_id=' + tab.hostID;
  if (presetID) {
    wsUrl += '&preset_id=' + presetID;
  }
  if (tab.initialDir) {
    wsUrl += '&dir=' + encodeURIComponent(tab.initialDir);
  }
  const ws = new WebSocket(wsUrl);
  tab.socketLeft = ws;
  ws.binaryType = 'arraybuffer';
  setupTerminalClipboardBridge(term, ws, container);
  setupTerminalPathTracking(tab, 'left', term);
  setupTerminalLinkProvider(term, tab, tab.hostID);

  // The pane starts as "Connecting…": the socket is not open yet.
  const setStatus = (cls, label) => {
    const el = document.getElementById('statusLeft_' + tab.id);
    if (!el) return;
    el.className = 'terminal-pane-status ' + cls;
    el.innerHTML = '<span class="terminal-tab-status-dot' + (cls === 'disconnected' ? ' disconnected' : '') + '"></span><span>' + escapeHtml(label) + '</span>';
  };
  setStatus('connecting', t('terminal_connecting', 'Connecting…'));

  ws.onopen = () => {
    tab.connectedLeft = true;
    setStatus('connected', t('connected', 'Connected'));
    renderTerminalTabsList();
    term.write('\r\n\x1b[32m[Pantau SSH Bridge Connected]\x1b[0m\r\n\r\n');
    if (S.activeTerminalTabId === tab.id && !S.isTerminalDockMinimized) {
      setTimeout(() => {
        try { term.focus(); } catch (e) {}
      }, 50);
    }
  };

  ws.onmessage = (ev) => {
    const data = typeof ev.data === 'string' ? ev.data : new Uint8Array(ev.data);
    term.write(data);
    if (S.activeTerminalTabId !== tab.id || S.isTerminalDockMinimized) {
      if (!tab.hasActivity) {
        tab.hasActivity = true;
        renderTerminalTabsList();
        updateTerminalSessionIndicators();
      }
    }
  };

  ws.onclose = () => {
    tab.connectedLeft = false;
    setStatus('disconnected', t('terminal_disconnected', 'Disconnected'));
    renderTerminalTabsList();
    term.write('\r\n\x1b[31m[Session Disconnected]\x1b[0m\r\n');
  };

  term.onData(data => {
    if (ws.readyState === WebSocket.OPEN) {
      ws.send(data);
    }
  });

  term.onResize(sz => {
    if (ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ cols: sz.cols, rows: sz.rows }));
    }
  });
}

function initTabRightTerminal(tab, hostID, presetID) {
  if (tab.socketRight) {
    try { tab.socketRight.close(); } catch(e) {}
    tab.socketRight = null;
  }
  if (tab.termRight) {
    try { tab.termRight.dispose(); } catch(e) {}
    tab.termRight = null;
  }

  const container = document.getElementById('termDivRight_' + tab.id);
  if (!container) return;
  container.innerHTML = '';

  const term = new Terminal({
    cursorBlink: true,
    fontFamily: S.TERMINAL_FONT_FAMILY,
    fontSize: getTerminalFontSize(),
    theme: S.XTERM_THEME
  });
  tab.termRight = term;
  tab.fitRight = new FitAddon.FitAddon();
  term.loadAddon(tab.fitRight);
  term.open(container);
  if (term.element) term.element.style.fontFamily = S.TERMINAL_FONT_FAMILY;
  tab.fitRight.fit();
  setTimeout(() => tab.fitRight && tab.fitRight.fit(), 60);
  if (document.fonts && document.fonts.ready) {
    document.fonts.ready.then(() => {
      if (tab.fitRight) tab.fitRight.fit();
    });
  }

  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  let wsUrl = protocol + '//' + location.host + '/ws/terminal?host_id=' + hostID;
  if (presetID) {
    wsUrl += '&preset_id=' + presetID;
  }
  const ws = new WebSocket(wsUrl);
  tab.socketRight = ws;
  ws.binaryType = 'arraybuffer';
  setupTerminalClipboardBridge(term, ws, container);
  setupTerminalPathTracking(tab, 'right', term);
  setupTerminalLinkProvider(term, tab, hostID);

  // The pane starts as "Connecting…": the socket is not open yet.
  const setStatus = (cls, label) => {
    const el = document.getElementById('statusRight_' + tab.id);
    if (!el) return;
    el.className = 'terminal-pane-status ' + cls;
    el.innerHTML = '<span class="terminal-tab-status-dot' + (cls === 'disconnected' ? ' disconnected' : '') + '"></span><span>' + escapeHtml(label) + '</span>';
  };
  setStatus('connecting', t('terminal_connecting', 'Connecting…'));

  ws.onopen = () => {
    tab.connectedRight = true;
    setStatus('connected', t('connected', 'Connected'));
    term.write('\r\n\x1b[32m[Pantau SSH Bridge Connected]\x1b[0m\r\n\r\n');
    if (S.activeTerminalTabId === tab.id && tab.focusedPane === 'right' && !S.isTerminalDockMinimized) {
      setTimeout(() => {
        try { term.focus(); } catch (e) {}
      }, 50);
    }
  };

  ws.onmessage = (ev) => {
    const data = typeof ev.data === 'string' ? ev.data : new Uint8Array(ev.data);
    term.write(data);
    if (S.activeTerminalTabId !== tab.id || S.isTerminalDockMinimized) {
      if (!tab.hasActivity) {
        tab.hasActivity = true;
        renderTerminalTabsList();
        updateTerminalSessionIndicators();
      }
    }
  };

  ws.onclose = () => {
    tab.connectedRight = false;
    setStatus('disconnected', t('terminal_disconnected', 'Disconnected'));
    renderTerminalTabsList();
    term.write('\r\n\x1b[31m[Session Disconnected]\x1b[0m\r\n');
  };

  term.onData(data => {
    if (ws.readyState === WebSocket.OPEN) {
      ws.send(data);
    }
  });

  term.onResize(sz => {
    if (ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ cols: sz.cols, rows: sz.rows }));
    }
  });
}

function toggleTabSplit(tabId) {
  const tab = S.terminalTabs.find(t => t.id === tabId);
  if (!tab) return;
  if (tab.isSplit) {
    closeTabSplit(tabId);
  } else {
    openTabSplit(tabId);
  }
}

function openTabSplit(tabId) {
  const tab = S.terminalTabs.find(t => t.id === tabId);
  if (!tab) return;
  tab.isSplit = true;
  const paneRight = document.getElementById('paneRight_' + tabId);
  const btn = document.getElementById('lblSplit_' + tabId);
  if (btn) btn.innerText = t('unsplit_terminal');
  if (paneRight) paneRight.style.display = 'flex';

  const select = document.getElementById('splitSelect_' + tabId);
  if (select) {
    select.innerHTML = (S.allRawHosts || []).map(h => `
      <option value="${h.id}" ${h.id === (tab.hostIDRight || tab.hostID) ? 'selected' : ''}>${escapeHtml(h.name)}</option>
    `).join('');
  }
  initTabRightTerminal(tab, tab.hostIDRight || tab.hostID);
  setTimeout(() => {
    if (tab.fitLeft) tab.fitLeft.fit();
    if (tab.fitRight) tab.fitRight.fit();
  }, 80);
}

function closeTabSplit(tabId) {
  const tab = S.terminalTabs.find(t => t.id === tabId);
  if (!tab) return;
  tab.isSplit = false;
  const paneRight = document.getElementById('paneRight_' + tabId);
  const btn = document.getElementById('lblSplit_' + tabId);
  if (btn) btn.innerText = t('split_terminal');
  if (tab.socketRight) {
    try { tab.socketRight.close(); } catch(e) {}
    tab.socketRight = null;
  }
  if (tab.termRight) {
    try { tab.termRight.dispose(); } catch(e) {}
    tab.termRight = null;
  }
  if (paneRight) paneRight.style.display = 'none';
  focusTabPane(tabId, 'left');
  setTimeout(() => {
    if (tab.fitLeft) tab.fitLeft.fit();
  }, 80);
}

function onTabSplitHostChange(tabId) {
  const tab = S.terminalTabs.find(t => t.id === tabId);
  if (!tab) return;
  const select = document.getElementById('splitSelect_' + tabId);
  if (!select) return;
  const newHostId = parseInt(select.value, 10);
  if (!newHostId) return;
  tab.hostIDRight = newHostId;
  initTabRightTerminal(tab, tab.hostIDRight);
}

function focusTabPane(tabId, pane) {
  const tab = S.terminalTabs.find(t => t.id === tabId);
  if (!tab) return;
  tab.focusedPane = pane;
  const pL = document.getElementById('paneLeft_' + tabId);
  const pR = document.getElementById('paneRight_' + tabId);
  if (pL) pL.classList.toggle('active-pane', pane === 'left');
  if (pR) pR.classList.toggle('active-pane', pane === 'right');
  const term = pane === 'right' && tab.isSplit ? tab.termRight : tab.termLeft;
  if (term) {
    setTimeout(() => {
      try {
        term.focus();
      } catch (e) {}
    }, 30);
  }
}

function clearTabActiveTerminal(tabId) {
  const tab = S.terminalTabs.find(t => t.id === tabId);
  if (!tab) return;
  const term = tab.focusedPane === 'right' && tab.isSplit ? tab.termRight : tab.termLeft;
  if (term) term.clear();
}

function reconnectTabTerminal(tabId) {
  const tab = S.terminalTabs.find(t => t.id === tabId);
  if (!tab) return;
  if (tab.focusedPane === 'right' && tab.isSplit) {
    initTabRightTerminal(tab, tab.hostIDRight || tab.hostID);
  } else {
    initTabLeftTerminal(tab, tab.presetID);
  }
}

function sendTabCommand(tabId, cmd) {
  const tab = S.terminalTabs.find(t => t.id === tabId);
  if (!tab) return;
  if (cmd && typeof cmd === 'string') {
    const trimmed = cmd.trim();
    const cdMatch = trimmed.match(/^cd\s+['"]?([^'";\s]+)['"]?/);
    if (cdMatch && cdMatch[1] && (cdMatch[1].startsWith('/') || cdMatch[1].startsWith('~'))) {
      setTabActivePath(tab, tab.focusedPane, cdMatch[1]);
    }
  }
  const ws = (tab.focusedPane === 'right' && tab.isSplit) ? tab.socketRight : tab.socketLeft;
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(cmd + '\r');
  }
}

async function loadTabPresets(tab) {
  const listContainer = document.getElementById('presetDropdownList_' + tab.id);
  if (!listContainer) return;
  try {
    const res = await fetch('/api/presets?host_id=' + tab.hostID);
    if (!res.ok) {
      tab.presets = [];
      renderTabPresetsList(tab.id, []);
      return;
    }
    const data = await res.json();
    tab.presets = Array.isArray(data) ? data : [];
    S.currentPresetList = tab.presets;
    renderTabPresetsList(tab.id, tab.presets);
  } catch (err) {
    tab.presets = [];
    renderTabPresetsList(tab.id, []);
  }
}

function renderTabPresetsList(tabId, presets) {
  const listEl = document.getElementById('presetDropdownList_' + tabId);
  if (!listEl) return;
  if (!presets || presets.length === 0) {
    const input = document.getElementById('presetSearchInput_' + tabId);
    const hasQuery = input && input.value.trim().length > 0;
    listEl.innerHTML = `<div style="padding: 12px 14px; font-size: var(--fs-xs); color: var(--text-muted); text-align: center;">${hasQuery ? t('no_matching_presets', 'No matching presets') : t('no_presets', 'No presets configured')}</div>`;
    return;
  }

  listEl.innerHTML = presets.map(p => `
    <div class="terminal-dropdown-item" style="display: flex; align-items: center; justify-content: space-between; padding: 6px 10px; cursor: pointer; border-bottom: 1px solid var(--card-border); gap: 8px;" onclick="triggerTabPreset('${tabId}', ${p.id})" title="${escapeHtml(p.command)} ${p.use_tmux ? '(tmux)' : ''}">
      <div style="display: flex; align-items: center; gap: 8px; min-width: 0; flex: 1;">
        <span style="color: var(--primary); display: inline-flex; align-items: center; flex-shrink: 0;">${svgIcon('play', 'icon-xs')}</span>
        <div style="min-width: 0; display: flex; flex-direction: column;">
          <span style="font-size: var(--fs-xs); font-weight: 500; color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">${escapeHtml(p.name)}</span>
          <span style="font-family: var(--font-mono); font-size: 0.68rem; color: var(--text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">${escapeHtml(p.command)}</span>
        </div>
      </div>
      ${p.use_tmux ? `<span class="badge" style="font-size: 0.65rem; background: var(--primary-soft); color: var(--accent-text); border: 1px solid var(--card-border); padding: 1px 4px; flex-shrink: 0;">tmux</span>` : ''}
    </div>
  `).join('');
}

function filterTabPresets(tabId) {
  const tab = S.terminalTabs.find(t => t.id === tabId);
  if (!tab) return;
  const input = document.getElementById('presetSearchInput_' + tabId);
  const q = (input ? input.value : '').trim().toLowerCase();
  const presets = tab.presets || [];
  if (!q) {
    renderTabPresetsList(tabId, presets);
    return;
  }
  const filtered = presets.filter(p => {
    return (p.name && p.name.toLowerCase().includes(q)) ||
           (p.command && p.command.toLowerCase().includes(q));
  });
  renderTabPresetsList(tabId, filtered);
}

function toggleTerminalPresetsDropdown(tabId, e) {
  if (e) e.stopPropagation();
  const menu = document.getElementById('presetsDropdownMenu_' + tabId);
  if (!menu) return;
  if (menu.style.display === 'block') {
    menu.style.display = 'none';
    return;
  }
  document.querySelectorAll('.terminal-dropdown-menu').forEach(m => {
    if (m !== menu) {
      m.style.display = 'none';
    }
  });
  menu.style.display = 'block';
  const input = document.getElementById('presetSearchInput_' + tabId);
  if (input) {
    input.value = '';
    filterTabPresets(tabId);
    setTimeout(() => input.focus(), 40);
  }
}

function triggerTabPreset(tabId, presetId) {
  const menu = document.getElementById('presetsDropdownMenu_' + tabId);
  if (menu) menu.style.display = 'none';
  const tab = S.terminalTabs.find(t => t.id === tabId);
  if (!tab) return;
  const p = (tab.presets || []).find(x => x.id === presetId) || (S.currentPresetList || []).find(x => x.id === presetId);
  if (!p) return;
  sendTabCommand(tabId, p.command);
}

async function openFileManagerForTab(tabId) {
  const tab = S.terminalTabs.find(t => t.id === tabId);
  if (!tab) return;
  const targetHostId = (tab.isSplit && tab.focusedPane === 'right') ? (tab.hostIDRight || tab.hostID) : tab.hostID;
  if (!targetHostId) return;

  let targetPath = getTerminalActivePath(tabId);

  minimizeTerminalDock();
  await openHostDetail(targetHostId);
  setHostFilesPath(targetHostId, targetPath);
  if (S.currentHost) S.currentHost._lastPath = targetPath;
  switchTab('files');
}

async function toggleTerminalPinsDropdown(tabId, e) {
  if (e) e.stopPropagation();
  const menu = document.getElementById('pinsDropdownMenu_' + tabId);
  if (!menu) return;
  if (menu.style.display === 'block') {
    menu.style.display = 'none';
    return;
  }
  document.querySelectorAll('.terminal-dropdown-menu').forEach(m => {
    if (m !== menu) {
      m.style.display = 'none';
    }
  });
  menu.style.display = 'block';
  const input = document.getElementById('pinSearchInput_' + tabId);
  if (input) {
    input.value = '';
    setTimeout(() => input.focus(), 40);
  }
  await loadTerminalPins(tabId);
}

async function loadTerminalPins(tabId) {
  const tab = S.terminalTabs.find(t => t.id === tabId);
  if (!tab) return;
  const listEl = document.getElementById('pinsDropdownList_' + tabId);
  if (!listEl) return;
  const targetHostId = (tab.isSplit && tab.focusedPane === 'right') ? (tab.hostIDRight || tab.hostID) : tab.hostID;
  listEl.innerHTML = `<div style="padding: 10px 12px; font-size: 0.75rem; color: var(--text-muted);">${t('loading', 'Loading...')}</div>`;
  try {
    const res = await fetch('/api/hosts/' + targetHostId + '/files/pins');
    if (!res.ok) throw new Error('Failed to load pins');
    const pins = await res.json() || [];
    tab.pins = pins;
    const input = document.getElementById('pinSearchInput_' + tabId);
    if (input && input.value.trim().length > 0) {
      filterTerminalPins(tabId);
    } else {
      renderTerminalPinsList(tabId, targetHostId, pins);
    }
  } catch(err) {
    tab.pins = [];
    listEl.innerHTML = `<div style="padding: 10px 12px; font-size: 0.75rem; color: var(--danger);">${escapeHtml(err.message)}</div>`;
  }
}

function filterTerminalPins(tabId) {
  const tab = S.terminalTabs.find(t => t.id === tabId);
  if (!tab) return;
  const input = document.getElementById('pinSearchInput_' + tabId);
  const q = (input ? input.value : '').trim().toLowerCase();
  const pins = tab.pins || [];
  const targetHostId = (tab.isSplit && tab.focusedPane === 'right') ? (tab.hostIDRight || tab.hostID) : tab.hostID;
  if (!q) {
    renderTerminalPinsList(tabId, targetHostId, pins);
    return;
  }
  const filtered = pins.filter(p => {
    return (p.path && p.path.toLowerCase().includes(q)) ||
           (p.note && p.note.toLowerCase().includes(q));
  });
  renderTerminalPinsList(tabId, targetHostId, filtered);
}

function renderTerminalPinsList(tabId, hostId, pins) {
  const listEl = document.getElementById('pinsDropdownList_' + tabId);
  if (!listEl) return;
  if (!pins || pins.length === 0) {
    const input = document.getElementById('pinSearchInput_' + tabId);
    const hasQuery = input && input.value.trim().length > 0;
    listEl.innerHTML = `<div style="padding: 12px 14px; font-size: var(--fs-xs); color: var(--text-muted); text-align: center;">${hasQuery ? t('no_matching_pins', 'No matching pinned paths') : t('no_pinned_paths', 'No pinned paths on this host')}</div>`;
    return;
  }
  listEl.innerHTML = pins.map(pin => {
    const icon = pin.is_dir ? svgIcon('folder', 'icon-sm') : svgIcon('file-text', 'icon-sm');
    const actionHint = pin.is_dir ? 'Click to cd' : 'Click to insert path';
    return `
      <div class="terminal-dropdown-item" style="display: flex; align-items: center; justify-content: space-between; padding: 6px 10px; cursor: pointer; border-bottom: 1px solid var(--card-border); gap: 8px;" onclick="triggerPinAction('${tabId}', ${jsArg(pin.path)}, ${pin.is_dir})" title="${escapeHtml(actionHint)}">
        <div style="display: flex; align-items: center; gap: 8px; min-width: 0; flex: 1;">
          <span style="color: var(--text-muted); display: inline-flex; align-items: center; flex-shrink: 0;">${icon}</span>
          <div style="min-width: 0; display: flex; flex-direction: column;">
            <span style="font-family: var(--font-mono); font-size: 0.75rem; color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">${escapeHtml(pin.path)}</span>
            ${pin.note ? `<span style="font-size: 0.68rem; color: var(--text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; display: inline-flex; align-items: center; gap: 4px;">${svgIcon('sticky-note', 'icon-xs')} ${escapeHtml(pin.note)}</span>` : ''}
          </div>
        </div>
        <button class="btn btn-sm btn-ghost" onclick="event.stopPropagation(); deleteTerminalPin('${tabId}', ${hostId}, ${jsArg(pin.path)})" title="Unpin" style="padding: 2px; color: var(--text-muted);">
          ${svgIcon('x', 'icon-xs')}
        </button>
      </div>
    `;
  }).join('');
}

function triggerPinAction(tabId, path, isDir) {
  const menu = document.getElementById('pinsDropdownMenu_' + tabId);
  if (menu) menu.style.display = 'none';
  if (isDir) {
    const tab = S.terminalTabs.find(t => t.id === tabId);
    if (tab) {
      setTabActivePath(tab, tab.focusedPane, path);
    }
    sendTabCommand(tabId, "cd '" + path.replace(/'/g, "'\\''") + "'");
  } else {
    // Inject path into active terminal session with quotes and trailing space without newline
    const escaped = "'" + path.replace(/'/g, "'\\''") + "' ";
    const tab = S.terminalTabs.find(t => t.id === tabId);
    if (!tab) return;
    const ws = (tab.focusedPane === 'right' && tab.isSplit) ? tab.socketRight : tab.socketLeft;
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(escaped);
    }
  }
}

async function deleteTerminalPin(tabId, hostId, path) {
  try {
    const res = await fetch('/api/hosts/' + hostId + '/files/pins?path=' + encodeURIComponent(path), {
      method: 'DELETE'
    });
    if (res.ok) {
      loadTerminalPins(tabId);
    }
  } catch(e) {}
}

function openTerminalPinModal(tabId) {
  const tab = S.terminalTabs.find(t => t.id === tabId);
  if (!tab) return;
  const targetHostId = (tab.isSplit && tab.focusedPane === 'right') ? (tab.hostIDRight || tab.hostID) : tab.hostID;
  const menu = document.getElementById('pinsDropdownMenu_' + tabId);
  if (menu) menu.style.display = 'none';
  const defaultPath = getTerminalActivePath(tabId);
  openPinModal(defaultPath, true, targetHostId, tabId, true);
}

function setupTerminalLinkProvider(term, tab, hostID) {
  if (!term || typeof term.registerLinkProvider !== 'function') return;

  const urlRegex = /(?:https?:\/\/[^\s"'<>()]+|(?:localhost|127\.0\.0\.1|0\.0\.0\.0):\d+[^\s"'<>()]*)/gi;

  term.registerLinkProvider({
    provideLinks(bufferLineNumber, callback) {
      try {
        const line = term.buffer.active.getLine(bufferLineNumber - 1);
        if (!line) {
          callback(undefined);
          return;
        }
        const lineText = line.translateToString(true);
        const links = [];
        let match;
        urlRegex.lastIndex = 0;

        while ((match = urlRegex.exec(lineText)) !== null) {
          const rawText = match[0];
          const startIndex = match.index;

          // Strip common trailing punctuation
          let cleanedText = rawText;
          while (/[.,;:)\]]$/.test(cleanedText)) {
            if (cleanedText.endsWith(')') && (cleanedText.match(/\(/g) || []).length >= (cleanedText.match(/\)/g) || []).length) {
              break;
            }
            cleanedText = cleanedText.slice(0, -1);
          }

          if (cleanedText.length > 0) {
            links.push({
              range: {
                start: { x: startIndex + 1, y: bufferLineNumber },
                end: { x: startIndex + cleanedText.length, y: bufferLineNumber }
              },
              text: cleanedText,
              activate(event, text) {
                handleTerminalLinkActivation(event, text, hostID);
              }
            });
          }
        }

        callback(links.length > 0 ? links : undefined);
      } catch (err) {
        callback(undefined);
      }
    }
  });

  // Standard OSC 1337 (OpenURL) & OSC 777 remote browser opener hook
  const isLoopbackHostname = (hostname) => {
    const h = String(hostname || '').toLowerCase().replace(/^\[/, '').replace(/\]$/, '');
    return h === 'localhost' || h === '::1' || h === '0.0.0.0' || /^127\./.test(h);
  };

  const handleRemoteBrowserOpen = async (rawUrl) => {
    let url = (rawUrl || '').trim();
    if (!url) return false;
    if (!/^https?:\/\//i.test(url)) {
      url = 'http://' + url;
    }
    try {
      const parsed = new URL(url);
      const hostname = parsed.hostname.toLowerCase();
      if (isLoopbackHostname(hostname) && parsed.port) {
        const subpath = parsed.pathname + (parsed.search || '') + (parsed.hash || '');
        const tunnelUrl = location.origin + '/api/tunnel/' + hostID + '/' + parsed.port + (subpath.startsWith('/') ? subpath : '/' + subpath);
        const host = (S.currentHost && String(S.currentHost.id) === String(hostID))
          ? S.currentHost
          : (Array.isArray(S.allRawHosts) ? S.allRawHosts.find(h => String(h.id) === String(hostID)) : null);
        const hostName = host ? (host.name || host.host) : ('Host #' + hostID);
        const confirmed = await showConfirm({
          title: t('tunnel_confirm_title', 'Open remote port?'),
          message: t('tunnel_confirm_msg', 'The remote host "{host}" asked to open port {port} in your browser through Pantau Tunnel. Only continue if you trust this host.')
            .replace('{host}', hostName).replace('{port}', parsed.port),
          confirmText: t('tunnel_confirm_open', 'Open'),
          danger: false
        });
        if (!confirmed) return true;
        saveRecentPort(parsed.port);
        window.open(tunnelUrl, '_blank');
        showToast(t('tunnel_opening_remote_port', 'Opening remote port ') + parsed.port + t('tunnel_via_pantau', ' via Pantau Tunnel'), 'info', 2500);
        return true;
      }
    } catch(e) {}
    window.open(url, '_blank', 'noopener,noreferrer');
    showToast(t('tunnel_opening_browser_link', 'Opening link in browser: ') + url, 'info', 2500);
    return true;
  };

  if (term.parser && typeof term.parser.registerOscHandler === 'function') {
    term.parser.registerOscHandler(1337, (data) => {
      if (data.startsWith('OpenURL=')) {
        handleRemoteBrowserOpen(data.slice(8));
        return true;
      }
      return false;
    });
    term.parser.registerOscHandler(777, (data) => {
      if (data.startsWith('open;')) {
        handleRemoteBrowserOpen(data.slice(5));
        return true;
      }
      return false;
    });
  }
}

function handleTerminalLinkActivation(event, text, hostID) {
  // Decision 01: Require Ctrl+Click (Win/Linux) or Cmd+Click (macOS)
  const isModifier = event && (event.ctrlKey || event.metaKey);
  if (!isModifier) {
    showToast(t('terminal_ctrl_click_link', 'Press Ctrl+Click (or Cmd+Click) to open link'), 'info', 2500);
    return;
  }

  let fullUrl = text;
  if (!/^https?:\/\//i.test(fullUrl)) {
    fullUrl = 'http://' + fullUrl;
  }

  let parsed;
  try {
    parsed = new URL(fullUrl);
  } catch (e) {
    window.open(fullUrl, '_blank', 'noopener,noreferrer');
    return;
  }

  const hostname = parsed.hostname.toLowerCase();
  const isLoopback = (hostname === 'localhost' || hostname === '127.0.0.1' || hostname === '0.0.0.0');

  if (isLoopback && parsed.port) {
    const subpath = parsed.pathname + (parsed.search || '') + (parsed.hash || '');
    showTunnelConfirmationModal(hostID, parsed.port, subpath, text);
  } else {
    window.open(fullUrl, '_blank', 'noopener,noreferrer');
  }
}

function showTunnelConfirmationModal(hostID, port, subpath, rawUrl) {
  const host = (typeof hosts !== 'undefined' && Array.isArray(hosts)) ? hosts.find(h => h.id === hostID) : null;
  const hostName = host ? host.name : (S.currentHost && S.currentHost.id === hostID ? S.currentHost.name : ('Host #' + hostID));
  const cleanSubpath = subpath ? (subpath.startsWith('/') ? subpath : '/' + subpath) : '/';
  const tunnelUrl = location.origin + '/api/tunnel/' + hostID + '/' + port + cleanSubpath;

  S.currentTunnelTarget = {
    hostID: hostID,
    hostName: hostName,
    port: port,
    subpath: cleanSubpath,
    rawUrl: rawUrl,
    tunnelUrl: tunnelUrl
  };

  const nameEl = document.getElementById('tunnelHostName');
  if (nameEl) nameEl.textContent = hostName;
  const badgeEl = document.getElementById('tunnelPortBadge');
  if (badgeEl) badgeEl.textContent = ':' + port;
  const rawEl = document.getElementById('tunnelRawUrl');
  if (rawEl) rawEl.textContent = rawUrl;
  const proxyEl = document.getElementById('tunnelProxyUrl');
  if (proxyEl) proxyEl.textContent = tunnelUrl;

  openModal('tunnelModal');
}

function openTunnelInNewTab() {
  if (S.currentTunnelTarget && S.currentTunnelTarget.tunnelUrl) {
    saveRecentPort(S.currentTunnelTarget.port);
    window.open(S.currentTunnelTarget.tunnelUrl, '_blank');
    closeModal('tunnelModal');
  }
}

function copyTunnelUrl() {
  if (S.currentTunnelTarget && S.currentTunnelTarget.tunnelUrl) {
    copyTextToClipboard(S.currentTunnelTarget.tunnelUrl);
    showToast(t('tunnel_copied_toast', 'Tunnel link copied to clipboard!'), 'success', 2000);
  }
}

function toggleTerminalPortsDropdown(tabId, e) {
  if (e) e.stopPropagation();
  const menu = document.getElementById('portsDropdownMenu_' + tabId);
  if (!menu) return;
  const isOpen = menu.style.display === 'block';
  document.querySelectorAll('.terminal-dropdown-menu').forEach(m => {
    m.style.display = 'none';
  });
  if (!isOpen) {
    menu.style.display = 'block';
    renderRecentPorts(tabId);
    const input = document.getElementById('portInput_' + tabId);
    if (input) {
      input.value = '';
      setTimeout(() => input.focus(), 40);
    }
  }
}

function getRecentPorts() {
  try {
    const raw = localStorage.getItem('pantau_recent_tunnel_ports');
    if (raw) {
      const arr = JSON.parse(raw);
      if (Array.isArray(arr) && arr.length > 0) return arr;
    }
  } catch(e) {}
  return [80, 443, 3000, 8080, 8000, 9000];
}

function saveRecentPort(port) {
  const p = parseInt(port, 10);
  if (!p || p < 1 || p > 65535) return;
  let list = getRecentPorts();
  list = [p, ...list.filter(x => x !== p)].slice(0, 8);
  try {
    localStorage.setItem('pantau_recent_tunnel_ports', JSON.stringify(list));
  } catch(e) {}
}

function renderRecentPorts(tabId) {
  const container = document.getElementById('recentPortsList_' + tabId);
  if (!container) return;
  const ports = getRecentPorts();
  container.innerHTML = ports.map(p => `
    <button class="terminal-port-chip" onclick="event.stopPropagation(); openTunnelForPort('${tabId}', ${p})">
      :${p}
    </button>
  `).join('');
}

function openCustomTunnelForTab(tabId) {
  const input = document.getElementById('portInput_' + tabId);
  if (!input) return;
  const port = parseInt(input.value.trim(), 10);
  if (!port || port < 1 || port > 65535) {
    showToast(t('invalid_port_warning', 'Please enter a valid port number (1-65535)'), 'warning', 2500);
    return;
  }
  openTunnelForPort(tabId, port);
}

function openTunnelForPort(tabId, port) {
  const tab = S.terminalTabs.find(t => t.id === tabId);
  if (!tab) return;
  const targetHostId = (tab.isSplit && tab.focusedPane === 'right') ? (tab.hostIDRight || tab.hostID) : tab.hostID;
  saveRecentPort(port);
  const url = location.origin + '/api/tunnel/' + targetHostId + '/' + port + '/';
  window.open(url, '_blank');
  const menu = document.getElementById('portsDropdownMenu_' + tabId);
  if (menu) menu.style.display = 'none';
}

function openTerminalForHost(hostID, presetID, forceNew = false, customTitle = null, initialDir = null) {
  if (!forceNew) {
    const existing = S.terminalTabs.find(t => t.hostID === hostID);
    if (existing) {
      showTerminalDock();
      activateTerminalTab(existing.id);
      if (!existing.socketLeft || existing.socketLeft.readyState !== WebSocket.OPEN) {
        reconnectTabTerminal(existing.id);
      }
      if (presetID) triggerTabPreset(existing.id, presetID);
      return existing;
    }
  }

  if (S.terminalTabs.length >= 8) {
    showToast(t('terminal_tab_limit_notice'), 'info');
  }

  const host = (S.allRawHosts || []).find(h => h.id === hostID) || (S.currentHost && S.currentHost.id === hostID ? S.currentHost : { id: hostID, name: 'Host ' + hostID, host: '' });
  const tabId = 'tab_' + Date.now() + '_' + Math.random().toString(36).substring(2, 6);

  const tab = {
    id: tabId,
    hostID: hostID,
    hostName: host.name || ('Host ' + hostID),
    title: customTitle || host.name || ('Host ' + hostID),
    presetID: presetID || null,
    initialDir: initialDir || null,
    socketLeft: null,
    termLeft: null,
    fitLeft: null,
    connectedLeft: false,
    isSplit: false,
    hostIDRight: hostID,
    socketRight: null,
    termRight: null,
    fitRight: null,
    connectedRight: false,
    focusedPane: 'left',
    presets: [],
    hasActivity: false
  };

  S.terminalTabs.push(tab);
  createTabWorkspaceDOM(tab);
  renderTerminalTabsList();
  showTerminalDock();
  activateTerminalTab(tab.id);
  saveTerminalTabsToStorage();
  return tab;
}

function closeTerminalTab(e, tabId) {
  if (e) e.stopPropagation();
  const idx = S.terminalTabs.findIndex(t => t.id === tabId);
  if (idx === -1) return;
  const tab = S.terminalTabs[idx];

  if (tab.socketLeft) {
    try { tab.socketLeft.close(); } catch(err) {}
    tab.socketLeft = null;
  }
  if (tab.termLeft) {
    try { tab.termLeft.dispose(); } catch(err) {}
    tab.termLeft = null;
  }
  if (tab.socketRight) {
    try { tab.socketRight.close(); } catch(err) {}
    tab.socketRight = null;
  }
  if (tab.termRight) {
    try { tab.termRight.dispose(); } catch(err) {}
    tab.termRight = null;
  }

  const ws = document.getElementById('workspace_' + tabId);
  if (ws) ws.remove();

  S.terminalTabs.splice(idx, 1);
  saveTerminalTabsToStorage();

  if (S.activeTerminalTabId === tabId) {
    if (S.terminalTabs.length > 0) {
      const nextIdx = Math.max(0, idx - 1);
      activateTerminalTab(S.terminalTabs[nextIdx].id);
    } else {
      S.activeTerminalTabId = null;
      hideTerminalDock();
    }
  }
  renderTerminalTabsList();
  updateTerminalSessionIndicators();
}

function closeAllTerminalTabs() {
  S.terminalTabs.forEach(tab => {
    if (tab.socketLeft) { try { tab.socketLeft.close(); } catch(err) {} }
    if (tab.termLeft) { try { tab.termLeft.dispose(); } catch(err) {} }
    if (tab.socketRight) { try { tab.socketRight.close(); } catch(err) {} }
    if (tab.termRight) { try { tab.termRight.dispose(); } catch(err) {} }
    const ws = document.getElementById('workspace_' + tab.id);
    if (ws) ws.remove();
  });
  S.terminalTabs = [];
  S.activeTerminalTabId = null;
  saveTerminalTabsToStorage();
  renderTerminalTabsList();
  hideTerminalDock();
  updateTerminalSessionIndicators();
}

async function closeTerminalDock() {
  if (S.terminalTabs.length > 0) {
    const ok = await showConfirm(t('close_all_terminals_confirm'));
    if (!ok) return;
  }
  closeAllTerminalTabs();
}

function toggleTerminalDock() {
  const modal = document.getElementById('terminalDockModal');
  const isOpen = modal && modal.classList.contains('open') && modal.style.display !== 'none';
  if (isOpen) {
    minimizeTerminalDock();
  } else {
    if (S.terminalTabs.length === 0) {
      showTerminalDock();
      setTimeout(() => {
        const btn = document.getElementById('btnNewTerminalTab');
        if (btn) btn.click();
      }, 100);
    } else {
      restoreTerminalDock();
    }
  }
}

function minimizeTerminalDock() {
  S.isTerminalDockMinimized = true;
  hideTerminalDock();
  updateTerminalSessionIndicators();
}

function restoreTerminalDock() {
  S.isTerminalDockMinimized = false;
  showTerminalDock();
  if (S.activeTerminalTabId) {
    activateTerminalTab(S.activeTerminalTabId);
  }
  refitActiveTab();
  updateTerminalSessionIndicators();
}

function showTerminalDock() {
  S.isTerminalDockMinimized = false;
  const modal = document.getElementById('terminalDockModal');
  if (modal) {
    openModal('terminalDockModal');
    modal.style.display = 'flex';
  }
  refitActiveTab();
  focusActiveTerminal();
  updateTerminalSessionIndicators();
}

function hideTerminalDock() {
  const modal = document.getElementById('terminalDockModal');
  if (modal) {
    modal.classList.remove('open');
    modal.style.display = 'none';
    modal.style.zIndex = '';
  }
  S.openModalStack = S.openModalStack.filter(mId => mId !== 'terminalDockModal');
  updateTerminalSessionIndicators();
}

function updateTerminalSessionIndicators() {
  // ponytail: reactive indicators for header launcher and individual host cards
  const headerBadge = document.getElementById('activeTerminalBadge');
  if (headerBadge) {
    const total = S.terminalTabs.length;
    if (total > 0) {
      headerBadge.innerText = total;
      headerBadge.style.display = 'inline-block';
    } else {
      headerBadge.style.display = 'none';
    }
  }
  document.querySelectorAll('.btn-host-terminal').forEach(btn => {
    const hid = parseInt(btn.getAttribute('data-host-id'), 10);
    const badge = btn.querySelector('.host-terminal-badge');
    if (badge) {
      const count = S.terminalTabs.filter(t => t.hostID === hid).length;
      if (count > 0) {
        badge.innerText = count;
        badge.style.display = 'inline-block';
      } else {
        badge.style.display = 'none';
      }
    }
  });
}

function toggleTerminalDockMaximize() {
  S.isTerminalDockMaximized = !S.isTerminalDockMaximized;
  const content = document.getElementById('terminalDockContent');
  const modal = document.getElementById('terminalDockModal');
  const btn = document.getElementById('btnTerminalDockMax');
  if (!content) return;
  if (S.isTerminalDockMaximized) {
    content.classList.add('dock-maximized');
    if (modal) modal.classList.add('dock-maximized-wrapper');
    if (btn) btn.innerHTML = svgIcon('minimize-2', 'icon-sm');
    if (btn) btn.title = t('restore_terminal', 'Restore Terminal');
  } else {
    content.classList.remove('dock-maximized');
    if (modal) modal.classList.remove('dock-maximized-wrapper');
    if (btn) btn.innerHTML = svgIcon('maximize-2', 'icon-sm');
    if (btn) btn.title = t('maximize_terminal', 'Maximize Terminal');
  }
  refitActiveTab();
}

function renderTerminalDropdownHosts(hosts, isSearch) {
  const list = document.getElementById('terminalDropdownHostList');
  if (!list) return;
  if (!hosts || hosts.length === 0) {
    const emptyMsg = isSearch 
      ? t('no_matching_hosts_short', 'No matching hosts') 
      : t('no_hosts', 'No hosts available');
    list.innerHTML = `<div class="terminal-dropdown-item" style="color: var(--text-muted); justify-content: center; cursor: default;">${escapeHtml(emptyMsg)}</div>`;
    return;
  }
  list.innerHTML = hosts.map((h, idx) => `
    <div class="terminal-dropdown-item${isSearch && idx === 0 ? ' selected' : ''}" data-host-id="${h.id}" onclick="onSelectNewTabHost(${h.id})">
      ${svgIcon('server', 'icon-sm')}
      <span style="font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 180px;">${escapeHtml(h.name)}</span>
      <span style="color: var(--text-muted); font-size: 0.72rem; margin-left: auto; padding-left: 12px; font-family: var(--font-mono); white-space: nowrap;">${escapeHtml(h.host)}</span>
    </div>
  `).join('');
}

function filterTerminalDropdownHosts() {
  const input = document.getElementById('terminalHostSearchInput');
  const q = (input ? input.value : '').toLowerCase().trim();
  const hosts = S.allRawHosts || [];
  if (!q) {
    renderTerminalDropdownHosts(hosts, false);
    return;
  }
  const filtered = hosts.filter(h => {
    const name = (h.name || '').toLowerCase();
    const host = (h.host || '').toLowerCase();
    return name.includes(q) || host.includes(q);
  });
  renderTerminalDropdownHosts(filtered, true);
}

function onTerminalHostSearchKeyDown(e) {
  if (e.key === 'Escape') {
    e.stopPropagation();
    const menu = document.getElementById('newTabDropdownMenu');
    if (menu) menu.style.display = 'none';
  } else if (e.key === 'Enter') {
    e.preventDefault();
    e.stopPropagation();
    const firstItem = document.querySelector('#terminalDropdownHostList .terminal-dropdown-item[data-host-id]');
    if (firstItem) {
      const hostId = parseInt(firstItem.getAttribute('data-host-id'), 10);
      if (!isNaN(hostId)) onSelectNewTabHost(hostId);
    }
  }
}

function toggleNewTabDropdown(e) {
  if (e) e.stopPropagation();
  const menu = document.getElementById('newTabDropdownMenu');
  if (!menu) return;
  if (menu.style.display === 'block') {
    menu.style.display = 'none';
    return;
  }
  const input = document.getElementById('terminalHostSearchInput');
  if (input) input.value = '';
  renderTerminalDropdownHosts(S.allRawHosts || [], false);
  menu.style.display = 'block';
  if (input) setTimeout(() => input.focus(), 20);
}

function onSelectNewTabHost(hostID) {
  const menu = document.getElementById('newTabDropdownMenu');
  if (menu) menu.style.display = 'none';
  openTerminalForHost(hostID, null, true);
}

function saveTerminalTabsToStorage() {
  try {
    const data = S.terminalTabs.map(tab => ({
      hostID: tab.hostID,
      hostName: tab.hostName,
      title: tab.title,
      isSplit: tab.isSplit,
      hostIDRight: tab.hostIDRight,
      initialDir: tab.initialDir
    }));
    localStorage.setItem('pantau_terminal_tabs_meta', JSON.stringify(data));
  } catch(e) {}
}

function restoreSavedTerminalTabs() {
  try {
    const raw = localStorage.getItem('pantau_terminal_tabs_meta');
    if (!raw) return;
    const meta = JSON.parse(raw);
    if (!Array.isArray(meta) || meta.length === 0) return;
    // ponytail: restore saved tab metadata into header launcher and host indicators without creating live WebSockets until dock is restored or tab activated
    meta.forEach(m => {
      if (m && m.hostID) {
        const host = (S.allRawHosts || []).find(h => h.id === m.hostID) || { id: m.hostID, name: m.hostName || ('Host ' + m.hostID), host: '' };
        const tabId = 'tab_' + Date.now() + '_' + Math.random().toString(36).substring(2, 6);
        const tab = {
          id: tabId,
          hostID: m.hostID,
          hostName: host.name || ('Host ' + m.hostID),
          title: m.title || host.name || ('Host ' + m.hostID),
          presetID: null,
          initialDir: m.initialDir || null,
          socketLeft: null,
          termLeft: null,
          fitLeft: null,
          connectedLeft: false,
          isSplit: m.isSplit || false,
          hostIDRight: m.hostIDRight || m.hostID,
          socketRight: null,
          termRight: null,
          fitRight: null,
          connectedRight: false,
          focusedPane: 'left',
          presets: [],
          hasActivity: false
        };
        S.terminalTabs.push(tab);
        createTabWorkspaceDOM(tab);
      }
    });
    if (S.terminalTabs.length > 0) {
      S.activeTerminalTabId = S.terminalTabs[0].id;
      renderTerminalTabsList();
      minimizeTerminalDock();
    }
  } catch(e) {}
}

function cleanUpTerminals() {
  // ponytail: legacy cleanup is no-op to preserve active terminal sessions across modal navigations
}

async function initTerminal(hostID, initialPresetID) {
  openTerminalForHost(hostID, initialPresetID);
}

function toggleTerminalMaximize() {
  toggleTerminalDockMaximize();
}

function toggleTerminalSplit() {
  if (S.activeTerminalTabId) toggleTabSplit(S.activeTerminalTabId);
}

function clearActiveTerminal() {
  if (S.activeTerminalTabId) clearTabActiveTerminal(S.activeTerminalTabId);
}

function reconnectActiveTerminal() {
  if (S.activeTerminalTabId) reconnectTabTerminal(S.activeTerminalTabId);
}

function sendCommandToActiveTerminal(cmd) {
  if (S.activeTerminalTabId) sendTabCommand(S.activeTerminalTabId, cmd);
}

function triggerPreset(presetId) {
  if (S.activeTerminalTabId) triggerTabPreset(S.activeTerminalTabId, presetId);
}

function openCreatePresetModal(editPreset = null, explicitHostId = null) {
  document.getElementById('editPresetId').value = editPreset ? editPreset.id : '';
  document.getElementById('presetModalTitle').innerText = editPreset ? t('edit_preset') : t('add_preset');
  document.getElementById('presetNameInput').value = editPreset ? editPreset.name : '';
  document.getElementById('presetCommandInput').value = editPreset ? editPreset.command : '';
  document.getElementById('presetTmuxCheckbox').checked = editPreset ? editPreset.use_tmux : false;

  // Resolve target host in order of priority:
  // 1. Explicit host ID argument
  // 2. Preset's host_id if editing
  // 3. Active terminal tab's hostID
  // 4. Preset manager modal context host ID
  // 5. Fallback to currentHost (dashboard host detail view)
  let targetHost = null;
  const targetId = explicitHostId || (editPreset ? editPreset.host_id : null) || (getActiveTerminalTab() ? getActiveTerminalTab().hostID : null) || S.currentPresetManagerHostId;
  if (targetId) {
    targetHost = (S.allRawHosts || []).find(h => h.id === targetId) || (S.currentHost && S.currentHost.id === targetId ? S.currentHost : null);
    if (!targetHost && getActiveTerminalTab() && getActiveTerminalTab().hostID === targetId) {
      targetHost = { id: targetId, name: getActiveTerminalTab().hostName };
    }
  }
  if (!targetHost && S.currentHost) {
    targetHost = S.currentHost;
  }

  S.currentPresetTargetHost = targetHost;

  const hostOpt = document.getElementById('presetScopeHostOption');
  if (targetHost) {
    hostOpt.style.display = 'block';
    // t('scope_host') already contains '🎯', do not prepend duplicate emoji
    hostOpt.innerText = `${t('scope_host')} (${targetHost.name})`;
  } else {
    hostOpt.style.display = 'none';
  }

  const scopeSelect = document.getElementById('presetScopeSelect');
  if (editPreset && editPreset.host_id) {
    scopeSelect.value = 'host';
  } else {
    scopeSelect.value = 'global';
  }

  openModal('presetModal');
}

async function submitPresetForm() {
  const id = document.getElementById('editPresetId').value;
  const name = document.getElementById('presetNameInput').value.trim();
  const command = document.getElementById('presetCommandInput').value.trim();
  const scope = document.getElementById('presetScopeSelect').value;
  const useTmux = document.getElementById('presetTmuxCheckbox').checked;

  if (!name || !command) {
    showToast(t('fill_all_fields', 'Name and command are required'), 'error');
    return;
  }

  let hostId = null;
  if (scope === 'host' && S.currentPresetTargetHost) {
    hostId = S.currentPresetTargetHost.id;
  }

  const payload = {
    name: name,
    command: command,
    host_id: hostId,
    use_tmux: useTmux,
    sort_order: 10
  };

  const url = id ? '/api/presets/' + id : '/api/presets';
  const method = id ? 'PUT' : 'POST';

  try {
    const res = await fetch(url, {
      method: method,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({}));
      showToast(t('failed_prefix', 'Failed: ') + (err.error || 'Server error'), 'error');
      return;
    }
    closeModal('presetModal');
    showToast(t('preset_saved', 'Preset saved successfully'), 'success');

    S.terminalTabs.forEach(tab => loadTabPresets(tab));
    const pm = document.getElementById('presetManagerModal');
    if (pm && pm.classList.contains('open')) {
      loadPresetManagerList();
    }
    loadSettingsPresets();
  } catch (e) {
    showToast(t('error_prefix', 'Network error: ') + e.message, 'error');
  }
}

async function openPresetManagerModal(hostId = null) {
  S.currentPresetManagerHostId = hostId || (getActiveTerminalTab() ? getActiveTerminalTab().hostID : null);
  openModal('presetManagerModal');
  loadPresetManagerList();
}

async function loadPresetManagerList() {
  const container = document.getElementById('presetManagerListContainer');
  if (!container) return;
  container.innerHTML = '<div style="text-align: center; color: var(--text-muted); padding: 20px;">' + t('loading', 'Loading...') + '</div>';

  try {
    const res = await fetch('/api/presets?host_id=all');
    if (!res.ok) return;
    const presets = await res.json() || [];

    if (presets.length === 0) {
      container.innerHTML = '<div style="text-align: center; color: var(--text-muted); padding: 30px;">' + t('no_presets', 'No presets defined yet.') + '</div>';
      return;
    }

    container.innerHTML = `
      <table style="width: 100%; border-collapse: collapse; font-size: var(--fs-xs);">
        <thead>
          <tr style="border-bottom: 1px solid var(--card-border); color: var(--text-muted); text-align: left;">
            <th style="padding: 8px 10px; white-space: nowrap;">${t('scope', 'Scope')}</th>
            <th style="padding: 8px 10px; white-space: nowrap;">${t('preset_name', 'Name')}</th>
            <th style="padding: 6px; width: 100%; min-width: 260px;">Command</th>
            <th style="padding: 8px 10px; white-space: nowrap; text-align: center;">Tmux</th>
            <th style="padding: 8px 10px; white-space: nowrap; text-align: right;">${t('actions', 'Action')}</th>
          </tr>
        </thead>
        <tbody>
          ${presets.map(p => {
            const hostObj = (S.allRawHosts || []).find(h => h.id === p.host_id);
            const scopeLabel = p.host_id ? (hostObj ? escapeHtml(hostObj.name) : 'Host #' + p.host_id) : `<span class="badge" style="background: var(--primary-soft); color: var(--accent-text); font-size: 0.7rem;">${svgIcon('globe', 'icon-xs')} Global</span>`;
            return `
              <tr style="border-bottom: 1px solid var(--card-border);">
                <td style="padding: 8px 10px; white-space: nowrap;">${scopeLabel}</td>
                <td style="padding: 8px 10px; font-weight: 500; white-space: nowrap; color: var(--text);">${escapeHtml(p.name)}</td>
                <td style="padding: 8px 10px; font-family: var(--font-mono); font-size: 0.72rem; word-break: break-all; white-space: pre-wrap;"><code style="background: var(--code-bg); padding: 2px 6px; border-radius: var(--radius-sm); border: 1px solid var(--card-border); word-break: break-all; white-space: pre-wrap; color: var(--text);">${escapeHtml(p.command)}</code></td>
                <td style="padding: 8px 10px; white-space: nowrap; text-align: center;">${p.use_tmux ? `<span style="color: var(--success);">${svgIcon('check', 'icon-xs')}</span>` : '<span style="color: var(--text-faint);">—</span>'}</td>
                <td style="padding: 8px 10px; text-align: right; white-space: nowrap;">
                  <button class="btn btn-sm btn-ghost" onclick="editPresetFromManager(${p.id})" title="${t('edit', 'Edit')}" style="padding: 4px;">
                    ${svgIcon('pencil', 'icon-xs')}
                  </button>
                  <button class="btn btn-danger btn-sm" onclick="deletePreset(${p.id})" title="${t('delete', 'Delete')}" style="padding: 4px;">
                    ${svgIcon('trash-2', 'icon-xs')}
                  </button>
                </td>
              </tr>
            `;
          }).join('')}
        </tbody>
      </table>
    `;
  } catch (e) {
    container.innerHTML = '<div style="color: var(--danger); padding: 12px;">Failed to load presets</div>';
  }
}

async function editPresetFromManager(id) {
  try {
    const res = await fetch('/api/presets/' + id);
    if (!res.ok) return;
    const preset = await res.json();
    openCreatePresetModal(preset);
  } catch (e) {
    showToast(t('error_prefix', 'Error: ') + e.message, 'error');
  }
}

async function deletePreset(id) {
  if (!await showConfirm({ message: t('confirm_delete_preset', 'Delete this preset?'), confirmText: t('delete', 'Delete') })) return;
  try {
    const res = await fetch('/api/presets/' + id, { method: 'DELETE' });
    if (res.ok) {
      showToast(t('preset_deleted', 'Preset deleted successfully'), 'success');
      loadPresetManagerList();
      loadSettingsPresets();
      S.terminalTabs.forEach(tab => loadTabPresets(tab));
    } else {
      showToast(t('failed_prefix', 'Delete failed'), 'error');
    }
  } catch (e) {
    showToast(t('error_prefix', 'Network error'), 'error');
  }
}

async function loadSettingsPresets() {
  const container = document.getElementById('settingsPresetsList');
  if (!container) return;
  container.innerHTML = '<div style="text-align: center; color: var(--text-muted); padding: 20px;">' + t('loading', 'Loading...') + '</div>';

  try {
    const res = await fetch('/api/presets?host_id=all');
    if (!res.ok) return;
    const presets = await res.json() || [];

    if (presets.length === 0) {
      container.innerHTML = '<div style="text-align: center; color: var(--text-muted); padding: 30px;">' + t('no_presets', 'No presets defined yet.') + '</div>';
      return;
    }

    container.innerHTML = `
      <table style="width: 100%; border-collapse: collapse; font-size: var(--fs-sm);">
        <thead>
          <tr style="border-bottom: 1px solid var(--card-border); color: var(--text-muted); text-align: left;">
            <th style="padding: 8px 10px; white-space: nowrap;">Scope</th>
            <th style="padding: 8px 10px; white-space: nowrap;">Name</th>
            <th style="padding: 8px 10px; width: 100%; min-width: 260px;">Command</th>
            <th style="padding: 8px 10px; white-space: nowrap; text-align: center;">Tmux</th>
            <th style="padding: 8px 10px; white-space: nowrap; text-align: right;">Action</th>
          </tr>
        </thead>
        <tbody>
          ${presets.map(p => {
            const hostObj = S.allRawHosts.find(h => h.id === p.host_id);
            const scopeLabel = p.host_id ? `<span class="badge badge-unknown">${escapeHtml(hostObj ? hostObj.name : 'Host #' + p.host_id)}</span>` : `<span class="badge badge-info" style="gap: 4px;">${svgIcon('globe', 'icon-sm')} Global</span>`;
            return `
              <tr style="border-bottom: 1px solid var(--card-border);">
                <td style="padding: 8px 10px; white-space: nowrap;">${scopeLabel}</td>
                <td style="padding: 8px 10px; font-weight: 600; white-space: nowrap; color: var(--text);">${escapeHtml(p.name)}</td>
                <td style="padding: 8px 10px; font-size: var(--fs-xs); word-break: break-all; white-space: pre-wrap;"><code class="mono" style="word-break: break-all; white-space: pre-wrap; background: var(--code-bg); padding: 2px 6px; border-radius: var(--radius-sm); border: 1px solid var(--card-border);">${escapeHtml(p.command)}</code></td>
                <td style="padding: 8px 10px; white-space: nowrap; text-align: center;">${p.use_tmux ? `<span class="text-success">${svgIcon('check', 'icon-sm')}</span>` : '<span class="text-faint">—</span>'}</td>
                <td style="padding: 8px 10px; text-align: right; white-space: nowrap;">
                  <button class="btn btn-sm btn-ghost" onclick="editPresetFromManager(${p.id})" style="padding: 2px 6px;" title="Edit">${svgIcon('pencil', 'icon-sm')}</button>
                  <button class="btn btn-danger btn-sm" onclick="deletePreset(${p.id})" style="padding: 2px 6px;" title="Delete">${svgIcon('trash-2', 'icon-sm')}</button>
                </td>
              </tr>
            `;
          }).join('')}
        </tbody>
      </table>
    `;
  } catch (e) {
    container.innerHTML = '<div style="color: var(--danger); padding: 12px;">Failed to load presets</div>';
  }
}

window.addEventListener('resize', () => {
  if (typeof fitAddonLeft !== 'undefined' && fitAddonLeft) fitAddonLeft.fit();
  if (typeof fitAddonRight !== 'undefined' && fitAddonRight) fitAddonRight.fit();
  if (typeof refitActiveTab === 'function') refitActiveTab();
});

window.addEventListener('keydown', (e) => {
  if (e.altKey && (e.key === '\\' || e.code === 'Backslash')) {
    const modal = document.getElementById('hostDetailModal');
    if (modal && modal.classList.contains('open') && S.currentTab === 'terminal') {
      e.preventDefault();
      toggleTerminalSplit();
    }
  }
});

export { getActiveTerminalTab, renderTerminalTabsList, startRenameTab, activateTerminalTab, focusActiveTerminal, refitActiveTab, createTabWorkspaceDOM, getTerminalFontSize, syncTerminalFontControls, adjustTerminalFontSize, applyTerminalFontSize, copyTextToClipboard, hostIpCopyHtml, onHostIpCopy, flattenToSingleLine, handleTerminalPaste, setupTerminalClipboardBridge, extractPathFromTerminalTitle, setTabActivePath, getTerminalActivePath, setupTerminalPathTracking, initTabLeftTerminal, initTabRightTerminal, toggleTabSplit, openTabSplit, closeTabSplit, onTabSplitHostChange, focusTabPane, clearTabActiveTerminal, reconnectTabTerminal, sendTabCommand, loadTabPresets, renderTabPresetsList, filterTabPresets, toggleTerminalPresetsDropdown, triggerTabPreset, openFileManagerForTab, toggleTerminalPinsDropdown, loadTerminalPins, filterTerminalPins, renderTerminalPinsList, triggerPinAction, deleteTerminalPin, openTerminalPinModal, setupTerminalLinkProvider, handleTerminalLinkActivation, showTunnelConfirmationModal, openTunnelInNewTab, copyTunnelUrl, toggleTerminalPortsDropdown, getRecentPorts, saveRecentPort, renderRecentPorts, openCustomTunnelForTab, openTunnelForPort, openTerminalForHost, closeTerminalTab, closeAllTerminalTabs, closeTerminalDock, toggleTerminalDock, minimizeTerminalDock, restoreTerminalDock, showTerminalDock, hideTerminalDock, updateTerminalSessionIndicators, toggleTerminalDockMaximize, renderTerminalDropdownHosts, filterTerminalDropdownHosts, onTerminalHostSearchKeyDown, toggleNewTabDropdown, onSelectNewTabHost, saveTerminalTabsToStorage, restoreSavedTerminalTabs, cleanUpTerminals, initTerminal, toggleTerminalMaximize, toggleTerminalSplit, clearActiveTerminal, reconnectActiveTerminal, sendCommandToActiveTerminal, triggerPreset, openCreatePresetModal, submitPresetForm, openPresetManagerModal, loadPresetManagerList, editPresetFromManager, deletePreset, loadSettingsPresets };
