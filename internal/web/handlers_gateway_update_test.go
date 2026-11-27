package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pantau/internal/gwupdate"
)

// The Gateway CLI Update Check (ADR 0080) must answer for a signed-in operator whether or not the
// Gate is enabled, and must never report "up to date" when the upstream could not be reached.

// fakeUpstream serves one release version for every upstream the check reads.
func fakeUpstream(t *testing.T, version string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"info":{"version":"` + version + `"},"version":"` + version + `"}`))
	}))
	t.Cleanup(srv.Close)

	// OpenTofu's "latest" is read from a redirect to the release tag, so it gets its own fake.
	ot := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://github.com/opentofu/opentofu/releases/tag/v"+version, http.StatusFound)
	}))
	t.Cleanup(ot.Close)

	oldPyPI, oldHC, oldOT := gwupdate.PyPIURL, gwupdate.HashiCorpURL, gwupdate.OpenTofuURL
	gwupdate.PyPIURL, gwupdate.HashiCorpURL, gwupdate.OpenTofuURL = srv.URL, srv.URL, ot.URL
	t.Cleanup(func() { gwupdate.PyPIURL, gwupdate.HashiCorpURL, gwupdate.OpenTofuURL = oldPyPI, oldHC, oldOT })
}

// 1. The endpoint needs a session.
func TestGatewayUpdateCheckUnauthenticated(t *testing.T) {
	srv, _ := setupAnsibleTestServer(t)
	rec := doRequest(t, srv, nil, http.MethodGet, "/api/ansible/update-check", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", rec.Code)
	}
}

// 2. The Ansible check works while the Gate is off, and reports the installed version.
func TestAnsibleUpdateCheckWorksWithGateOff(t *testing.T) {
	fakeUpstream(t, "2.22.0")
	srv, auth := setupAnsibleTestServer(t)

	rec := doRequest(t, srv, auth, http.MethodGet, "/api/ansible/update-check", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var res map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res["installed"] != "2.15.0" {
		t.Errorf("installed = %v, want 2.15.0", res["installed"])
	}
	if res["latest"] != "2.22.0" {
		t.Errorf("latest = %v, want 2.22.0", res["latest"])
	}
	if res["status"] != gwupdate.StatusUpdate {
		t.Errorf("status = %v, want %q", res["status"], gwupdate.StatusUpdate)
	}
}

// 3. A newer upstream release is reported as an update; the same version is "current".
func TestAnsibleUpdateCheckStatuses(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)

	fakeUpstream(t, "2.15.0")
	rec := doRequest(t, srv, auth, http.MethodGet, "/api/ansible/update-check", nil)
	var res map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res["status"] != gwupdate.StatusCurrent {
		t.Errorf("status = %v, want %q", res["status"], gwupdate.StatusCurrent)
	}
}

// 4. An upstream that cannot be reached is an error in the payload, never a 5xx and never "current".
func TestAnsibleUpdateCheckUpstreamFailure(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)

	old := gwupdate.PyPIURL
	gwupdate.PyPIURL = "http://127.0.0.1:1/unreachable"
	defer func() { gwupdate.PyPIURL = old }()

	rec := doRequest(t, srv, auth, http.MethodGet, "/api/ansible/update-check", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for an unreachable upstream, got %d", rec.Code)
	}
	var res map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res["status"] != gwupdate.StatusError {
		t.Errorf("status = %v, want %q", res["status"], gwupdate.StatusError)
	}
	if res["error"] == nil || res["error"] == "" {
		t.Error("expected an error message for an unreachable upstream")
	}
}

// 5. A binary that is not installed is unknown, not an error and not an update.
func TestAnsibleUpdateCheckWithoutBinary(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	srv.ansibleAvail = func() (string, string, error) {
		return "", "", errors.New("ansible-playbook not found in PATH")
	}

	rec := doRequest(t, srv, auth, http.MethodGet, "/api/ansible/update-check", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var res map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res["status"] != gwupdate.StatusUnknown {
		t.Errorf("status = %v, want %q", res["status"], gwupdate.StatusUnknown)
	}
}

// 6. The Terraform endpoint answers with a result per installed binary.
func TestTerraformUpdateCheck(t *testing.T) {
	fakeUpstream(t, "1.17.2")
	srv, auth := setupAnsibleTestServer(t)

	rec := doRequest(t, srv, auth, http.MethodGet, "/api/terraform/update-check", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Results []struct {
			Target    string `json:"target"`
			Installed string `json:"installed"`
			Latest    string `json:"latest"`
			Status    string `json:"status"`
		} `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// The result carries every binary that is installed on this server; a binary that is missing
	// is not a finding and must not appear.
	for _, r := range res.Results {
		if r.Target != "terraform" && r.Target != "tofu" {
			t.Errorf("unexpected target %q", r.Target)
		}
		// OpenTofu tags carry a "v" prefix by design (see gwupdate); compare the bare version.
		if strings.TrimPrefix(r.Latest, "v") != "1.17.2" {
			t.Errorf("latest = %q, want 1.17.2", r.Latest)
		}
	}
}

// 7. Only GET is allowed on both endpoints.
func TestGatewayUpdateCheckRejectsOtherMethods(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	for _, url := range []string{"/api/ansible/update-check", "/api/terraform/update-check"} {
		rec := doRequest(t, srv, auth, http.MethodPost, url, map[string]string{})
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: expected 405, got %d", url, rec.Code)
		}
	}
}
