package web

import (
	"strings"
	"testing"
)

func TestTerminalClipboardBridgeInIndexHTML(t *testing.T) {
	js := coreJSSource(t)

	// 1. Must define clipboard helper with fallback for non-secure HTTP contexts
	if !strings.Contains(js, "copyTextToClipboard") {
		t.Errorf("expected core modules to contain copyTextToClipboard helper")
	}
	if !strings.Contains(js, "execCommand('copy')") && !strings.Contains(js, `execCommand("copy")`) {
		t.Errorf("expected core modules to have document.execCommand('copy') fallback for plain HTTP")
	}

	// 2. Must attach custom key event handler for context-aware Ctrl+C and Ctrl+V
	if !strings.Contains(js, "attachCustomKeyEventHandler") {
		t.Errorf("expected core modules to use term.attachCustomKeyEventHandler for terminal shortcuts")
	}
	if !strings.Contains(js, "hasSelection()") {
		t.Errorf("expected core modules to check term.hasSelection() for context-aware Ctrl+C")
	}

	// 3. Must handle Ctrl+Shift+C, Ctrl+V, and Ctrl+Shift+V
	if !strings.Contains(js, "handleTerminalPaste") {
		t.Errorf("expected core modules to have handleTerminalPaste function")
	}

	// 4. Must implement multiline paste safety check
	if !strings.Contains(js, "includes('\\n')") && !strings.Contains(js, `includes("\n")`) {
		t.Errorf("expected handleTerminalPaste to check for multiline newline characters")
	}
	if !strings.Contains(js, "confirm_multiline_paste") {
		t.Errorf("expected handleTerminalPaste to invoke confirmation dialog for multiline commands")
	}

	// 5. Must support copy-on-select
	if !strings.Contains(js, "onSelectionChange") {
		t.Errorf("expected core modules to support copy-on-select via onSelectionChange")
	}

	// 6. Must support right-click paste
	if !strings.Contains(js, "contextmenu") {
		t.Errorf("expected core modules to handle contextmenu / right-click paste on terminal")
	}

	// 7. Check i18n keys for multiline paste confirmation in both EN and ID
	requiredKeys := []string{
		`"multiline_paste_title":`,
		`"confirm_multiline_paste":`,
		`"paste_http_notice":`,
	}
	for _, k := range requiredKeys {
		if strings.Count(js, k) < 2 {
			t.Errorf("expected i18n key %q to exist in at least 2 dictionaries (EN and ID), found %d", k, strings.Count(js, k))
		}
	}
}

func TestCtrlVDoesNotSuppressNativePasteEvent(t *testing.T) {
	js := coreJSSource(t)

	// Check that container paste listener uses capture phase (true)
	if !strings.Contains(js, "container.addEventListener('paste',") && !strings.Contains(js, `container.addEventListener("paste",`) {
		t.Fatalf("expected container paste listener in core modules")
	}
	if !strings.Contains(js, ", true);") && !strings.Contains(js, ", true)") {
		t.Errorf("expected container paste event listener to use capture phase (true) to intercept native paste before xterm raw handler")
	}

	// Check that Ctrl+V does NOT unconditionally call e.preventDefault()
	if strings.Contains(js, "// 2. Paste: Ctrl+V or Ctrl+Shift+V\n    if ((e.ctrlKey || e.metaKey) && (e.key === 'v' || e.key === 'V')) {\n      if (e.type === 'keydown') {\n        handleTerminalPaste(term, ws);\n      }\n      e.preventDefault();\n      return false;\n    }") {
		t.Errorf("bug detected: Ctrl+V unconditionally calls e.preventDefault(), suppressing native DOM paste event and failing on HTTP")
	}
}

func TestMultilinePasteSingleLineFlattening(t *testing.T) {
	js := coreJSSource(t)
	html := string(embeddedHTML)

	// 1. Must define flattenToSingleLine function in core modules
	if !strings.Contains(js, "function flattenToSingleLine(") {
		t.Errorf("expected core modules to contain flattenToSingleLine helper function")
	}

	// 2. confirmModal must have extra button for secondary action (Single-Line Flatten)
	if !strings.Contains(html, `id="confirmModalExtraBtn"`) {
		t.Errorf("expected confirmModal in index.html to contain confirmModalExtraBtn")
	}

	// 3. handleTerminalPaste must offer extraBtnText and handle result === 'single'
	if !strings.Contains(js, "extraBtnText: t('paste_single_line'") && !strings.Contains(js, `extraBtnText: t("paste_single_line"`) {
		t.Errorf("expected handleTerminalPaste to specify extraBtnText for single line flatten")
	}
	if !strings.Contains(js, "flattenToSingleLine(") {
		t.Errorf("expected handleTerminalPaste to call flattenToSingleLine when single line option selected")
	}

	// 4. i18n keys for single line flatten and paste as is
	requiredKeys := []string{
		`"paste_single_line":`,
		`"paste_as_is":`,
	}
	for _, k := range requiredKeys {
		if strings.Count(js, k) < 2 {
			t.Errorf("expected i18n key %q to exist in at least 2 dictionaries (EN and ID), found %d", k, strings.Count(js, k))
		}
	}
}
