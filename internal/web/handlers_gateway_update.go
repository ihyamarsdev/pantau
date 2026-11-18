package web

import (
	"net/http"
	"sync"

	"pantau/internal/ansible"
	"pantau/internal/gwupdate"
	"pantau/internal/terraform"
)

// The Gateway CLI Update Check (ADR 0080) compares the CLI versions installed on the Pantau server
// with the latest release published upstream, for the CLIs behind the Ansible Gate and the Terraform
// Gate. It only reads: nothing is installed, downloaded or changed.
//
// Both endpoints work whether or not the Gate is enabled: an operator wants to know the version
// before deciding to turn a Gate on. An upstream that cannot be reached is reported as an error in
// the payload (HTTP 200), never as "up to date" and never as a 5xx.

// handleAnsibleUpdateCheck: GET /api/ansible/update-check -> the update check for ansible-playbook.
func (s *Server) handleAnsibleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var (
		ver string
		err error
	)
	if s.ansibleAvail != nil {
		_, ver, err = s.ansibleAvail()
	} else {
		_, ver, err = ansible.Available()
	}

	installed := ""
	if err == nil {
		installed = gwupdate.ExtractVersion(ver)
	}

	res := gwupdate.Check(r.Context(), gwupdate.TargetAnsible, installed)
	if installed == "" && res.Status != gwupdate.StatusError {
		res.Error = "ansible-playbook is not installed on this server"
	}
	writeJSON(w, http.StatusOK, res)
}

// handleTerraformUpdateCheck: GET /api/terraform/update-check -> the update checks for the terraform
// and tofu binaries that are installed on the Pantau server.
func (s *Server) handleTerraformUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	type check struct {
		target    string
		installed string
	}
	checks := make([]check, 0, 2)
	for _, b := range terraform.Detect() {
		// A binary that is not installed is not a finding: there is nothing to update.
		if b.Err != "" {
			continue
		}
		checks = append(checks, check{target: b.Name, installed: gwupdate.ExtractVersion(b.Version)})
	}

	// The upstream lookups run together: in an airgapped network each one would otherwise wait out
	// its own timeout, so the check would take twice as long as it needs to.
	results := make([]gwupdate.Result, len(checks))
	var wg sync.WaitGroup
	for i, c := range checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = gwupdate.Check(r.Context(), c.target, c.installed)
		}()
	}
	wg.Wait()

	writeJSON(w, http.StatusOK, map[string]interface{}{"results": results})
}
