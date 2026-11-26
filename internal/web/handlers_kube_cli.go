package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"pantau/internal/gwupdate"
	"pantau/internal/inspector"
	"pantau/internal/logger"
)

// The kubectl CLI manager (Kubernetes section). The kubectl Pantau runs for External Clusters and
// kubeconfig validation can be checked, installed, pinned to a version, and reverted to the system
// copy. k3s and RKE2 hosts over SSH keep using the kubectl of each host; this is not that binary.
//
// Every version lives beside the others under <data>/cli/versions, so switching back to one that is
// already cached needs no network. The system copy in PATH is never written.

// kubeCliChannel is the newest patch release of one Kubernetes minor.
type kubeCliChannel struct {
	Minor  string `json:"minor"`
	Latest string `json:"latest"`
}

// kubeCliUpdateCheck is the answer of GET /api/kube/cli/update-check.
type kubeCliUpdateCheck struct {
	OK bool `json:"ok"`
	gwupdate.Result
	Source               string           `json:"source"`
	Path                 string           `json:"path"`
	SystemInstalled      string           `json:"system_installed"`
	ManagedVersions      []string         `json:"managed_versions"`
	ActiveManagedVersion string           `json:"active_managed_version"`
	Channels             []kubeCliChannel `json:"channels"`
}

// handleKubeCliUpdateCheck: GET /api/kube/cli/update-check. Read only: nothing is downloaded.
// An upstream that cannot be reached is reported as status "error" with HTTP 200.
func (s *Server) handleKubeCliUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	st := inspector.DetectLocalKubectl()
	res := gwupdate.Check(r.Context(), gwupdate.TargetKubectl, st.Version)
	if st.Version == "" && res.Status != gwupdate.StatusError {
		res.Error = "kubectl is not installed on the Pantau server"
	}

	versions, active, err := gwupdate.Dir().ListKubectlVersions()
	if err != nil {
		versions = nil
	}

	out := kubeCliUpdateCheck{
		OK:                   true,
		Result:               res,
		Source:               st.Source,
		Path:                 st.Path,
		ManagedVersions:      versions,
		ActiveManagedVersion: active,
		Channels:             kubeCliChannels(r, res),
	}
	if st.SystemAvailable {
		out.SystemInstalled = st.SystemVersion
	}
	if out.ManagedVersions == nil {
		out.ManagedVersions = []string{}
	}
	if out.Channels == nil {
		out.Channels = []kubeCliChannel{}
	}
	writeJSON(w, http.StatusOK, out)
}

// kubeCliChannels looks up the newest patch of the latest minor and the two minors before it, so the
// UI can offer a quick pin for each. The lookups run together, so an airgapped network waits out one
// timeout rather than three. Nothing is offered when the upstream is unreachable.
func kubeCliChannels(r *http.Request, res gwupdate.Result) []kubeCliChannel {
	if res.Status == gwupdate.StatusError || res.Latest == "" {
		return nil
	}
	parts := strings.Split(res.Latest, ".")
	if len(parts) < 2 {
		return nil
	}
	major, errMajor := strconv.Atoi(parts[0])
	minor, errMinor := strconv.Atoi(parts[1])
	if errMajor != nil || errMinor != nil {
		return nil
	}

	var minors []string
	for i := 0; i < 3 && minor-i >= 0; i++ {
		minors = append(minors, fmt.Sprintf("%d.%d", major, minor-i))
	}

	found := make([]*kubeCliChannel, len(minors))
	var wg sync.WaitGroup
	for i, m := range minors {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rel, err := gwupdate.LatestKubectlChannel(r.Context(), m)
			if err == nil {
				found[i] = &kubeCliChannel{Minor: m, Latest: rel.Version}
			}
		}()
	}
	wg.Wait()

	var out []kubeCliChannel
	for _, c := range found {
		if c != nil {
			out = append(out, *c)
		}
	}
	return out
}

// handleKubeCliInstall: POST /api/kube/cli/install with {"version": "1.31.4"}. Without a version the
// newest stable release is installed. The answer is the NDJSON install stream (see installStream).
func (s *Server) handleKubeCliInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var body struct {
		Version string `json:"version"`
	}
	if !decodeKubeCliBody(w, r, &body) {
		return
	}
	streamKubeCliInstall(w, r, strings.TrimSpace(body.Version))
}

// handleKubeCliSwitch: POST /api/kube/cli/switch. Either {"mode": "system"} reverts to the system
// kubectl, or {"version": "1.30.8"} pins a version: a cached one is switched to at once, any other is
// downloaded and verified first (the NDJSON stream of the install).
func (s *Server) handleKubeCliSwitch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var body struct {
		Mode    string `json:"mode"`
		Version string `json:"version"`
	}
	if !decodeKubeCliBody(w, r, &body) {
		return
	}
	mode, version := strings.TrimSpace(body.Mode), strings.TrimSpace(body.Version)

	switch {
	case mode != "" && version != "":
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "give either a mode or a version, not both"})
	case mode == "system":
		s.revertKubectlToSystem(w)
	case mode != "":
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown mode " + mode})
	case version == "":
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "a version or mode is required"})
	default:
		streamKubeCliInstall(w, r, version)
	}
}

// revertKubectlToSystem removes the managed link, so External Cluster operations use the system kubectl.
// It is refused when there is no system kubectl, because that would leave them with no kubectl at all.
func (s *Server) revertKubectlToSystem(w http.ResponseWriter) {
	root := gwupdate.ManagedRoot()
	if root == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Pantau does not manage the CLI binaries on this server",
		})
		return
	}
	inspector.InvalidateKubectlDetectCache()
	if st := inspector.DetectLocalKubectl(); !st.SystemAvailable {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "no system kubectl is installed, so there is nothing to revert to",
		})
		return
	}
	if err := (gwupdate.ManagedDir{Root: root}).UseSystemKubectl(); err != nil {
		logger.Warn("kube", "kubectl", "Could not revert to the system kubectl: "+err.Error())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	inspector.InvalidateKubectlDetectCache()
	logger.Info("kube", "kubectl", "External Cluster kubectl reverted to the system copy")
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "mode": "system"})
}

// handleKubeCliVersion: DELETE /api/kube/cli/versions/{version} removes one cached version. The active
// version is refused with 409.
func (s *Server) handleKubeCliVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	version := strings.TrimPrefix(r.URL.Path, "/api/kube/cli/versions/")
	if _, ok := gwupdate.ParseVersion(version); !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the version is not a release number"})
		return
	}
	root := gwupdate.ManagedRoot()
	if root == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Pantau does not manage the CLI binaries on this server",
		})
		return
	}

	err := (gwupdate.ManagedDir{Root: root}).DeleteKubectlVersion(version)
	switch {
	case errors.Is(err, gwupdate.ErrKubectlVersionActive):
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "kubectl " + version + " is in use; switch to another version or the system copy first",
		})
	case os.IsNotExist(err):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "kubectl " + version + " is not cached"})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	default:
		logger.Info("kube", "kubectl", "Removed cached kubectl "+version)
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
	}
}

// streamKubeCliInstall installs one kubectl (the newest when version is empty) and streams the steps.
func streamKubeCliInstall(w http.ResponseWriter, r *http.Request, version string) {
	root := gwupdate.ManagedRoot()
	if root == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Pantau does not manage the CLI binaries on this server",
		})
		return
	}
	if version == "" {
		rel, err := gwupdate.LatestKubectl(r.Context())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"error": "the latest release could not be looked up: " + err.Error(),
			})
			return
		}
		version = rel.Version
	}

	stream := &installStream{w: w}
	err := gwupdate.Install(r.Context(), gwupdate.TargetKubectl, version, root, stream)
	if err == nil {
		inspector.InvalidateKubectlDetectCache()
		logger.Info("kube", "kubectl", "External Cluster kubectl is now "+version)
	} else {
		logger.Warn("kube", "kubectl", "kubectl "+version+" was not installed: "+err.Error())
	}
	stream.finish(err)
}

// decodeKubeCliBody reads a small JSON body, answering 400 itself when it is not valid.
func decodeKubeCliBody(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the request body is not valid JSON"})
		return false
	}
	return true
}
