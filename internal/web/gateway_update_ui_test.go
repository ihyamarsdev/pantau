package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The Gateway CLI Update Check (ADR 0080) must be reachable from the two gate panels: the Ansible
// Gate card and the Terraform Gate card. Both must show what was found and must state that the
// check only reads.
func TestGatewayUpdateCheckUIInBundles(t *testing.T) {
	t.Parallel()
	ansibleBundle := ansibleBundle(t)
	terraformBundle := terraformJSBundle(t)

	// 1. The Ansible Gate card: a button that runs the check and a block that shows the result.
	for _, want := range []string{
		`data-st="update-check"`,
		"function checkCLIUpdate(",
		"/api/ansible/update-check",
		"updateCheckBlock()",
	} {
		if !strings.Contains(ansibleBundle, want) {
			t.Errorf("ansible bundle missing %q", want)
		}
	}

	// 2. The Terraform Gate card: a button, the endpoint it reads and the card it renders.
	for _, want := range []string{
		"id=\"tfCliUpdateCheck\"",
		"/api/terraform/update-check",
		"function cliResultHtml(",
		"tfCliUpdateCard",
	} {
		if !strings.Contains(terraformBundle, want) {
			t.Errorf("terraform bundle missing %q", want)
		}
	}

	// 3. Honesty: the panel states that the check only reads, and a failed check is never shown as
	// "up to date".
	for _, want := range []string{
		"ans_rs_cli_hint",
		"tf_cli_hint",
	} {
		if !strings.Contains(ansibleBundle, want) && !strings.Contains(terraformBundle, want) {
			t.Errorf("neither bundle carries %q", want)
		}
	}
	if !strings.Contains(ansibleBundle, "ans_rs_cli_hint") {
		t.Error("ansible bundle missing the read-only note")
	}
	if !strings.Contains(terraformBundle, "tf_cli_hint") {
		t.Error("terraform bundle missing the read-only note")
	}

	// The failure label must exist next to the success labels, so a failed check cannot be read as
	// an up-to-date one.
	for _, want := range []string{
		"ans_rs_cli_available",
		"ans_rs_cli_current",
		"ans_rs_cli_failed",
		"ans_rs_cli_unknown",
	} {
		if !strings.Contains(ansibleBundle, want) {
			t.Errorf("ansible bundle missing the status label %q", want)
		}
	}
	for _, want := range []string{
		"tf_cli_available",
		"tf_cli_current",
		"tf_cli_error",
		"tf_cli_unknown",
	} {
		if !strings.Contains(terraformBundle, want) {
			t.Errorf("terraform bundle missing the status label %q", want)
		}
	}
}

// The i18n keys of the feature must exist in both languages.
func TestGatewayUpdateCheckI18nKeys(t *testing.T) {
	t.Parallel()
	ansibleBundle := ansibleBundle(t)
	terraformBundle := terraformJSBundle(t)

	// The Ansible keys live in the Ans.addI18n block: once in EN, once in ID.
	for _, key := range []string{
		"ans_rs_cli_check",
		"ans_rs_cli_available",
		"ans_rs_cli_current",
		"ans_rs_cli_unknown",
		"ans_rs_cli_failed",
		"ans_rs_cli_versions",
		"ans_rs_cli_hint",
	} {
		if n := strings.Count(ansibleBundle, key+":"); n < 2 {
			t.Errorf("expected the i18n key %q in both EN and ID, found %d", key, n)
		}
	}

	// The Terraform keys live in an EN/ID pair table.
	for _, key := range []string{
		"tf_cli_check",
		"tf_cli_available",
		"tf_cli_current",
		"tf_cli_unknown",
		"tf_cli_error",
		"tf_cli_hint",
	} {
		if !strings.Contains(terraformBundle, key+":") {
			t.Errorf("terraform bundle missing the i18n key %q", key)
		}
	}
}

// The endpoints must be registered on the server.
func TestGatewayUpdateCheckRoutesRegistered(t *testing.T) {
	t.Parallel()
	srv, _ := setupAnsibleTestServer(t)
	for _, url := range []string{"/api/ansible/update-check", "/api/terraform/update-check"} {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		rec := httptest.NewRecorder()
		srv.mux.ServeHTTP(rec, req)
		// Without a session the route answers 401; an unknown route answers 404. The route exists
		// when the answer is 401.
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: expected 401 for an anonymous request, got %d", url, rec.Code)
		}
	}
}

// The update action (ADR 0081) must be reachable from both gate panels: a button that installs the
// newer version into the directory Pantau manages, and a log that shows the steps.
func TestGatewayCLIUpdateControlsUI(t *testing.T) {
	t.Parallel()
	ansibleBundle := ansibleBundle(t)
	terraformBundle := terraformJSBundle(t)

	// The Ansible Gate card offers the update when a newer release was found, and streams the log.
	for _, want := range []string{
		`data-st="update-install"`,
		"function installCLIUpdate(",
		"/api/ansible/install",
		`id="ansibleCliUpdateLog"`,
		"ans_rs_cli_update",
		"ans_rs_cli_install_hint",
	} {
		if !strings.Contains(ansibleBundle, want) {
			t.Errorf("ansible bundle missing %q", want)
		}
	}

	// The Terraform Gate card offers the update per binary, and streams the log.
	for _, want := range []string{
		`data-act="cli-install"`,
		"/api/terraform/install",
		`id="tfCliUpdateLog"`,
		"tf_cli_update",
		"tf_cli_install_hint",
	} {
		if !strings.Contains(terraformBundle, want) {
			t.Errorf("terraform bundle missing %q", want)
		}
	}
}

// The install endpoints must be registered.
func TestGatewayCLIInstallRoutesRegistered(t *testing.T) {
	t.Parallel()
	srv, _ := setupAnsibleTestServer(t)
	for _, url := range []string{"/api/ansible/install", "/api/terraform/install"} {
		req := httptest.NewRequest(http.MethodPost, url, nil)
		rec := httptest.NewRecorder()
		srv.mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: expected 401 for an anonymous request, got %d", url, rec.Code)
		}
	}
}
