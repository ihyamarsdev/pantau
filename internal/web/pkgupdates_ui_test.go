package web

import (
	"strings"
	"testing"
)

// The Package Update Check must be reachable from the Packages tab: a button that runs the check, a
// panel that shows the result, and the endpoints it talks to.
func TestPackageUpdateCheckUIInIndexHTML(t *testing.T) {
	ui := uiSourceForTest(t)
	js := coreJSSource(t)

	// 1. The button in the Packages tab toolbar.
	for _, want := range []string{
		`id="btnCheckPkgUpdates"`,
		`onclick="checkHostPackageUpdates(`,
		`"pkg_check_updates_btn"`,
	} {
		if !strings.Contains(ui, want) {
			t.Errorf("expected %q in index.html", want)
		}
	}

	// 2. The panel that holds the result.
	if !strings.Contains(ui, `id="pkgUpdatesPanel"`) {
		t.Errorf("expected pkgUpdatesPanel in index.html")
	}

	// 3. The JS: loading, running and rendering the check.
	for _, want := range []string{
		"function loadHostPackageUpdates(",
		"function checkHostPackageUpdates(",
		"function renderPkgUpdatesPanel(",
		"function pkgUpdateCooldownPaint(",
		"/packages/updates'",
		"/packages/updates/check'",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("expected %q in core modules", want)
		}
	}

	// 4. The check result must reach the packages table (the available version per package).
	if !strings.Contains(ui, `t('pkg_available_version', 'Available')`) {
		t.Errorf("expected the Available column header in the packages table")
	}
	if !strings.Contains(ui, "const avail = S.pkgUpdateCheckData") {
		t.Errorf("expected the packages table to read the available version from the update check")
	}

	// 5. Honesty: a failed check must not be shown as "up to date", and a stale cache must be said out loud.
	for _, want := range []string{
		"pkg_update_check_failed",
		"pkg_stale_cache",
		"pkg_check_readonly_note",
		"repo_cache_age_hours",
	} {
		if !strings.Contains(ui, want) {
			t.Errorf("expected %q in index.html or core modules", want)
		}
	}

	// 6. The i18n keys must exist in both languages.
	for _, key := range []string{
		"pkg_check_updates_btn",
		"pkg_updates_title",
		"pkg_available_version",
		"pkg_type_security",
		"pkg_all_current",
		"pkg_stale_cache",
		"pkg_check_readonly_note",
	} {
		if n := strings.Count(ui, `"`+key+`":`); n != 2 {
			t.Errorf("expected the i18n key %q in both EN and ID, found %d", key, n)
		}
	}
}
