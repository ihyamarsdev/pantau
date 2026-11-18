package web

import (
	"strings"
	"testing"
)

func TestTerminalLinkAndTunnelUIInIndexHTML(t *testing.T) {
	html := string(embeddedHTML)
	js := coreJSSource(t)
	ui := html + js

	// 1. Must define link provider setup and activation handler
	if !strings.Contains(js, "setupTerminalLinkProvider(term, tab,") {
		t.Errorf("expected core modules to call setupTerminalLinkProvider for terminals")
	}
	if !strings.Contains(js, "function setupTerminalLinkProvider(") {
		t.Errorf("expected core modules to define setupTerminalLinkProvider")
	}
	if !strings.Contains(js, "registerLinkProvider") {
		t.Errorf("expected core modules to use term.registerLinkProvider")
	}
	if !strings.Contains(js, "handleTerminalLinkActivation(") {
		t.Errorf("expected core modules to define handleTerminalLinkActivation")
	}

	// 2. Modifier key enforcement (Ctrl+Click / Cmd+Click)
	if !strings.Contains(js, "event.ctrlKey || event.metaKey") {
		t.Errorf("expected link activation to require Ctrl or Cmd modifier key")
	}

	// 3. Loopback port detection and tunnel confirmation modal
	if !strings.Contains(js, "showTunnelConfirmationModal(") {
		t.Errorf("expected core modules to have showTunnelConfirmationModal")
	}
	if !strings.Contains(html, `id="tunnelModal"`) {
		t.Errorf("expected index.html to contain tunnelModal")
	}
	if !strings.Contains(js, "openTunnelInNewTab()") {
		t.Errorf("expected core modules to contain openTunnelInNewTab handler")
	}

	// 4. Ports dropdown menu in terminal toolbar
	if !strings.Contains(ui, "terminal-ports-wrapper") {
		t.Errorf("expected terminal toolbar to contain terminal-ports-wrapper")
	}
	if !strings.Contains(ui, "portsDropdownMenu_") {
		t.Errorf("expected terminal toolbar to contain portsDropdownMenu_")
	}
	if !strings.Contains(js, "openCustomTunnelForTab(") {
		t.Errorf("expected core modules to define openCustomTunnelForTab")
	}

	// 5. Check closing dropdown on outside click
	if !strings.Contains(ui, ".terminal-ports-wrapper") {
		t.Errorf("expected window click listener to include .terminal-ports-wrapper")
	}
}
