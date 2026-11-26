package gwupdate

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Installing a newer CLI (ADR 0081).
//
// Every upstream publishes the SHA-256 of its release artifacts in a manifest next to the download,
// so a download is verified before it is installed:
//
//	terraform  -> releases.hashicorp.com: <name>_SHA256SUMS next to <name>_<os>_<arch>.zip
//	opentofu   -> the GitHub release: tofu_<version>_SHA256SUMS next to tofu_<version>_<os>_<arch>.zip
//	ansible    -> PyPI publishes the sha256 of the ansible-core wheel in the project JSON
//
// The binaries are installed into a directory Pantau owns (ManagedDir), never over the system copy
// in /usr/bin: Pantau usually does not run as root, and the system copy belongs to the package
// manager. Pantau runs the managed binary when it exists, so the update takes effect immediately.
//
// ansible-core is a Python package rather than a single file, so it is installed into a versioned
// virtualenv and the "venv" symlink is flipped to the new one; the flip is atomic, so a failed
// install leaves the working version in place.

const (
	// downloadCap bounds one downloaded artifact.
	downloadCap = 200 << 20
	// manifestCap bounds the checksum manifest.
	manifestCap = 1 << 20
	// installTimeout bounds one install.
	installTimeout = 15 * time.Minute
)

// The download locations, as variables so the tests can point them at a local server.
var (
	// HashiCorpBaseURL is the base of the Terraform release downloads.
	HashiCorpBaseURL = "https://releases.hashicorp.com"
	// OpenTofuBaseURL is the base of the OpenTofu release downloads.
	OpenTofuBaseURL = "https://github.com/opentofu/opentofu/releases/download"
	// PyPIWheelURL is the PyPI JSON API of a specific ansible-core release.
	PyPIWheelURL = "https://pypi.org/pypi/ansible-core/%s/json"
)

// ManagedDir is the directory Pantau owns for the CLIs it installs.
//
//	<root>/bin/terraform            the managed terraform binary
//	<root>/bin/tofu                 the managed tofu binary
//	<root>/venv                     symlink to the managed ansible virtualenv
//	<root>/venvs/ansible-<version>  the virtualenv of one ansible-core version
type ManagedDir struct {
	Root string
}

// BinDir is the directory holding the managed standalone binaries.
func (m ManagedDir) BinDir() string { return filepath.Join(m.Root, "bin") }

// VenvLink is the symlink that points at the managed ansible virtualenv.
func (m ManagedDir) VenvLink() string { return filepath.Join(m.Root, "venv") }

// VenvsDir holds one virtualenv per installed ansible-core version.
func (m ManagedDir) VenvsDir() string { return filepath.Join(m.Root, "venvs") }

// ManagedBinary returns the path of a managed standalone binary, and whether it exists.
func (m ManagedDir) ManagedBinary(name string) (string, bool) {
	if m.Root == "" {
		return "", false
	}
	p := filepath.Join(m.BinDir(), name)
	if runtime.GOOS == "windows" {
		p += ".exe"
	}
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p, true
	}
	return "", false
}

// ManagedVenvBin returns the path of a command inside the managed ansible virtualenv, and whether
// it exists.
func (m ManagedDir) ManagedVenvBin(name string) (string, bool) {
	if m.Root == "" {
		return "", false
	}
	p := filepath.Join(m.VenvLink(), "bin", name)
	if runtime.GOOS == "windows" {
		p = filepath.Join(m.VenvLink(), "Scripts", name+".exe")
	}
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p, true
	}
	return "", false
}

// InstallError describes a failed install and says whether retrying could help.
type InstallError struct {
	Target  string
	Version string
	Reason  string
	Hint    string
	Err     error
}

func (e *InstallError) Error() string {
	msg := fmt.Sprintf("cannot install %s %s: %s", e.Target, e.Version, e.Reason)
	if e.Hint != "" {
		msg += " (" + e.Hint + ")"
	}
	return msg
}

func (e *InstallError) Unwrap() error { return e.Err }

// Install downloads, verifies and installs one release of one CLI.
//
// The version must be a plain release version (1.2.3); it is used to build the download URL, so
// anything else is refused before a request is made.
func Install(ctx context.Context, target, version, root string, log io.Writer) error {
	if _, ok := ParseVersion(strings.TrimPrefix(strings.TrimLeft(version, "vV"), "")); !ok {
		return &InstallError{Target: target, Version: version, Reason: "the version is not a release number"}
	}
	if runtime.GOOS == "windows" {
		return &InstallError{
			Target: target, Version: version,
			Reason: "this CLI is not supported on Windows",
			Hint:   "install it on a Linux or macOS Pantau server",
		}
	}

	ctx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()

	switch target {
	case TargetAnsible:
		return installAnsible(ctx, version, root, log)
	case TargetTerraform, TargetOpenTofu:
		return installStandalone(ctx, target, version, root, log)
	case TargetKubectl:
		return installKubectl(ctx, version, root, log)
	default:
		return &InstallError{Target: target, Version: version, Reason: "unknown target"}
	}
}

// installStandalone installs terraform or tofu: a zip holding one binary, verified against the
// checksum manifest published beside it.
func installStandalone(ctx context.Context, target, version, root string, log io.Writer) error {
	name := target // "terraform" or "tofu"
	version = strings.TrimPrefix(strings.TrimLeft(version, "vV"), "")
	asset := fmt.Sprintf("%s_%s_%s_%s.zip", name, version, runtime.GOOS, runtime.GOARCH)

	var manifestURL, assetURL string
	switch target {
	case TargetTerraform:
		manifestURL = fmt.Sprintf("%s/%s/%s/%s_%s_SHA256SUMS", HashiCorpBaseURL, name, version, name, version)
		assetURL = fmt.Sprintf("%s/%s/%s/%s", HashiCorpBaseURL, name, version, asset)
	case TargetOpenTofu:
		base := fmt.Sprintf("%s/v%s", OpenTofuBaseURL, version)
		manifestURL = fmt.Sprintf("%s/%s_%s_SHA256SUMS", base, name, version)
		assetURL = fmt.Sprintf("%s/%s", base, asset)
	}

	logf(log, "Downloading the checksum manifest of %s %s…", name, version)
	manifest, err := download(ctx, manifestURL, manifestCap)
	if err != nil {
		return &InstallError{Target: target, Version: version, Reason: "the release manifest could not be downloaded", Err: err}
	}
	want, err := parseManifest(manifest, asset)
	if err != nil {
		return &InstallError{Target: target, Version: version, Reason: "the release manifest has no checksum for " + asset, Err: err}
	}

	logf(log, "Downloading %s…", asset)
	data, err := download(ctx, assetURL, downloadCap)
	if err != nil {
		return &InstallError{Target: target, Version: version, Reason: "the release could not be downloaded", Err: err}
	}

	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, want) {
		return &InstallError{
			Target: target, Version: version,
			Reason: "the checksum of the download does not match the release manifest",
			Hint:   "expected " + want[:16] + "…, got " + got[:16] + "…",
		}
	}
	logf(log, "Checksum verified (%s…)", want[:16])

	binary, err := binaryFromZip(data, name)
	if err != nil {
		return &InstallError{Target: target, Version: version, Reason: "the release archive holds no " + name + " binary", Err: err}
	}

	m := ManagedDir{Root: root}
	if err := os.MkdirAll(m.BinDir(), 0755); err != nil {
		return &InstallError{Target: target, Version: version, Reason: "the install directory could not be created", Err: err}
	}

	// The candidate is written next to its final place under a unique name, checked, and only then
	// moved into place — so a concurrent install of the same version cannot collide with this one.
	candidate, err := writeCandidate(m.BinDir(), name, binary)
	if err != nil {
		return &InstallError{Target: target, Version: version, Reason: "the binary could not be written", Err: err}
	}
	defer os.Remove(candidate)

	logf(log, "Checking the new binary…")
	if err := smokeTest(ctx, candidate, version); err != nil {
		return &InstallError{Target: target, Version: version, Reason: "the new binary did not run", Err: err}
	}

	final := filepath.Join(m.BinDir(), name)
	if runtime.GOOS == "windows" {
		final += ".exe"
	}
	if err := os.Rename(candidate, final); err != nil {
		return &InstallError{Target: target, Version: version, Reason: "the binary could not be put in place", Err: err}
	}
	logf(log, "Installed %s %s into %s", name, version, m.BinDir())
	return nil
}

// writeCandidate writes the binary to a unique temporary file in dir and returns its path. The file
// is created exclusively, so two installs running at once never write the same file.
func writeCandidate(dir, name string, binary []byte) (string, error) {
	f, err := os.CreateTemp(dir, fmt.Sprintf(".%s-*.candidate", name))
	if err != nil {
		return "", err
	}
	path := f.Name()
	if _, err := f.Write(binary); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	if err := f.Chmod(0755); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

// installAnsible installs ansible-core into a versioned virtualenv and points the "venv" symlink at
// it. The symlink is only flipped after the new virtualenv has answered a version check, so a failed
// install leaves the working version in place.
func installAnsible(ctx context.Context, version, root string, log io.Writer) error {
	version = strings.TrimPrefix(strings.TrimLeft(version, "vV"), "")
	target := TargetAnsible

	logf(log, "Looking up the ansible-core %s release on PyPI…", version)
	wheel, err := pypiWheel(ctx, version)
	if err != nil {
		return &InstallError{Target: target, Version: version, Reason: "the ansible-core release could not be looked up on PyPI", Err: err}
	}

	logf(log, "Downloading %s…", wheel.Filename)
	data, err := download(ctx, wheel.URL, downloadCap)
	if err != nil {
		return &InstallError{Target: target, Version: version, Reason: "the ansible-core wheel could not be downloaded", Err: err}
	}

	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, wheel.SHA256) {
		return &InstallError{
			Target: target, Version: version,
			Reason: "the checksum of the download does not match the one PyPI publishes",
			Hint:   "expected " + wheel.SHA256[:16] + "…, got " + got[:16] + "…",
		}
	}
	logf(log, "Checksum verified (%s…)", wheel.SHA256[:16])

	python, err := exec.LookPath("python3")
	if err != nil {
		return &InstallError{
			Target: target, Version: version,
			Reason: "python3 was not found",
			Hint:   "ansible-core is a Python package, so a Python 3 interpreter is needed",
		}
	}

	m := ManagedDir{Root: root}
	if err := os.MkdirAll(m.VenvsDir(), 0755); err != nil {
		return &InstallError{Target: target, Version: version, Reason: "the install directory could not be created", Err: err}
	}
	venvDir := filepath.Join(m.VenvsDir(), "ansible-"+version)

	// A virtualenv of this version already there is reused: the install is then just the upgrade.
	if _, err := os.Stat(filepath.Join(venvDir, "bin", "python3")); err != nil {
		logf(log, "Creating the virtualenv…")
		if err := runCmd(ctx, python, "-m", "venv", venvDir); err != nil {
			return &InstallError{
				Target: target, Version: version,
				Reason: "the virtualenv could not be created",
				Hint:   "python3 needs the venv module (python3-venv on Debian)",
				Err:    err,
			}
		}
	}

	pip := filepath.Join(venvDir, "bin", "pip")
	if runtime.GOOS == "windows" {
		pip = filepath.Join(venvDir, "Scripts", "pip.exe")
	}
	if _, err := os.Stat(pip); err != nil {
		return &InstallError{
			Target: target, Version: version,
			Reason: "the virtualenv has no pip",
			Hint:   "python3 needs the ensurepip module (python3-venv on Debian)",
		}
	}

	// The wheel is written to disk and installed from there: pip verifies the file it is given the
	// same way, and the dependencies it pulls are hash-checked by pip against the PyPI index.
	wheelPath := filepath.Join(m.VenvsDir(), wheel.Filename)
	if err := os.WriteFile(wheelPath, data, 0644); err != nil {
		return &InstallError{Target: target, Version: version, Reason: "the wheel could not be written", Err: err}
	}
	defer os.Remove(wheelPath)

	logf(log, "Installing ansible-core %s (with its dependencies)…", version)
	installLog := &logWriter{log: log}
	cmd := exec.CommandContext(ctx, pip, "install", "--upgrade", "--no-cache-dir", wheelPath)
	cmd.Dir = m.VenvsDir()
	cmd.Env = append(os.Environ(), "PIP_DISABLE_PIP_VERSION_CHECK=1")
	cmd.Stdout = installLog
	cmd.Stderr = installLog
	if err := cmd.Run(); err != nil {
		return &InstallError{
			Target: target, Version: version,
			Reason: "pip could not install ansible-core",
			Hint:   "the server needs internet access to PyPI for the dependencies",
			Err:    err,
		}
	}

	playbook := filepath.Join(venvDir, "bin", "ansible-playbook")
	if runtime.GOOS == "windows" {
		playbook = filepath.Join(venvDir, "Scripts", "ansible-playbook.exe")
	}
	logf(log, "Checking the new installation…")
	if err := smokeTest(ctx, playbook, version); err != nil {
		return &InstallError{Target: target, Version: version, Reason: "the new installation did not run", Err: err}
	}

	// The symlink is flipped last: until this moment the working version stays in place.
	if err := pointSymlink(m.VenvLink(), venvDir); err != nil {
		return &InstallError{Target: target, Version: version, Reason: "the managed virtualenv could not be activated", Err: err}
	}
	logf(log, "Installed ansible-core %s into %s", version, m.VenvLink())
	return nil
}

// pypiWheel finds the wheel of one ansible-core release and the checksum PyPI publishes for it.
type pypiWheelInfo struct {
	Filename string
	URL      string
	SHA256   string
}

func pypiWheel(ctx context.Context, version string) (pypiWheelInfo, error) {
	url := fmt.Sprintf(PyPIWheelURL, version)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return pypiWheelInfo{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Pantau/"+UserAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return pypiWheelInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return pypiWheelInfo{}, fmt.Errorf("pypi: status %s", resp.Status)
	}

	var doc struct {
		Info struct {
			Version string `json:"version"`
		} `json:"info"`
		URLs []struct {
			Packagetype string `json:"packagetype"`
			Filename    string `json:"filename"`
			URL         string `json:"url"`
			Digests     struct {
				SHA256 string `json:"sha256"`
			} `json:"digests"`
		} `json:"urls"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&doc); err != nil {
		return pypiWheelInfo{}, err
	}
	if doc.Info.Version != version {
		return pypiWheelInfo{}, fmt.Errorf("pypi: asked for %s, got %s", version, doc.Info.Version)
	}
	for _, u := range doc.URLs {
		if u.Packagetype == "bdist_wheel" && u.Digests.SHA256 != "" {
			return pypiWheelInfo{Filename: u.Filename, URL: u.URL, SHA256: u.Digests.SHA256}, nil
		}
	}
	return pypiWheelInfo{}, errors.New("pypi: the release has no wheel with a checksum")
}

// installKubectl installs one kubectl release. kubectl is a bare binary with a SHA-256 file beside it
// (no archive), and each version is kept on its own, so switching to a cached version needs no
// download. The bin/kubectl symlink is the active version (ADR 0081).
func installKubectl(ctx context.Context, version, root string, log io.Writer) error {
	target := TargetKubectl
	version = strings.TrimLeft(version, "vV")
	if !kubectlRelease.MatchString(version) {
		return &InstallError{Target: target, Version: version, Reason: "the version is not a release number"}
	}
	if root == "" {
		return &InstallError{Target: target, Version: version, Reason: "the managed directory is not set"}
	}
	m := ManagedDir{Root: root}
	final := filepath.Join(m.VersionsDir(), kubectlPrefix+version)
	link := filepath.Join(m.BinDir(), "kubectl")

	// A cached version that still runs is switched to without touching the network.
	if st, err := os.Stat(final); err == nil && st.Mode().IsRegular() {
		err := smokeTestKubectl(ctx, final, version)
		if err == nil {
			logf(log, "kubectl %s is already downloaded, switching to it", version)
			return pointSymlink(link, final)
		}
		logf(log, "the cached kubectl %s did not pass its check, downloading it again: %v", version, err)
	}

	base := KubectlBaseURL + "/v" + version + "/bin/" + runtime.GOOS + "/" + runtime.GOARCH + "/kubectl"
	logf(log, "downloading kubectl %s", version)
	sumBody, err := download(ctx, base+".sha256", manifestCap)
	if err != nil {
		return &InstallError{Target: target, Version: version, Reason: "the checksum could not be downloaded", Err: err}
	}
	fields := strings.Fields(string(sumBody))
	if len(fields) == 0 || !isHex(fields[0]) {
		return &InstallError{Target: target, Version: version, Reason: "the checksum file holds no SHA-256 value"}
	}
	data, err := download(ctx, base, downloadCap)
	if err != nil {
		return &InstallError{Target: target, Version: version, Reason: "the release could not be downloaded", Err: err}
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != fields[0] {
		return &InstallError{Target: target, Version: version, Reason: "the checksum does not match the release", Hint: "nothing was installed"}
	}
	logf(log, "checksum verified")

	if err := os.MkdirAll(m.VersionsDir(), 0755); err != nil {
		return &InstallError{Target: target, Version: version, Reason: "the install directory could not be created", Err: err}
	}
	cand, err := writeCandidate(m.VersionsDir(), "kubectl", data)
	if err != nil {
		return &InstallError{Target: target, Version: version, Reason: "the binary could not be written", Err: err}
	}
	if err := smokeTestKubectl(ctx, cand, version); err != nil {
		os.Remove(cand)
		return &InstallError{Target: target, Version: version, Reason: "the new binary did not run", Err: err}
	}
	if err := os.Rename(cand, final); err != nil {
		os.Remove(cand)
		return &InstallError{Target: target, Version: version, Reason: "the binary could not be put in place", Err: err}
	}
	if err := pointSymlink(link, final); err != nil {
		return &InstallError{Target: target, Version: version, Reason: "the binary could not be made active", Err: err}
	}
	logf(log, "kubectl %s is active", version)
	return nil
}

// smokeTestKubectl runs "kubectl version --client", which works offline (without --client kubectl
// contacts the API server), and checks the version it reports.
func smokeTestKubectl(ctx context.Context, path, version string) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "version", "--client").Output()
	if err != nil {
		return fmt.Errorf("kubectl did not run: %v", err)
	}
	if got := ExtractVersion(string(out)); got != version {
		return fmt.Errorf("kubectl reported %q, expected version %s", got, version)
	}
	return nil
}

// smokeTest runs the installed binary and checks that it reports the expected version.
func smokeTest(ctx context.Context, path, version string) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "version").CombinedOutput()
	if err != nil {
		// terraform and tofu answer "version", ansible answers "--version".
		out2, err2 := exec.CommandContext(ctx, path, "--version").CombinedOutput()
		if err2 != nil {
			return fmt.Errorf("%s did not run: %v", filepath.Base(path), err)
		}
		out = out2
	}
	text := string(out)
	if !strings.Contains(text, version) {
		return fmt.Errorf("%s reported %q, expected version %s", filepath.Base(path), strings.TrimSpace(firstLine(text)), version)
	}
	return nil
}

// binaryFromZip takes one binary out of a release zip.
func binaryFromZip(data []byte, name string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	want := name
	if runtime.GOOS == "windows" {
		want = name + ".exe"
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		// Only the binary at the root of the archive is taken, so a crafted archive cannot write
		// anywhere else.
		if filepath.Base(f.Name) != want || strings.Contains(f.Name, "/") {
			continue
		}
		if f.UncompressedSize64 > downloadCap {
			return nil, fmt.Errorf("%s is too large", f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(io.LimitReader(rc, downloadCap))
	}
	return nil, fmt.Errorf("the archive holds no %s binary", want)
}

// parseManifest finds the checksum of one file in a "sha256  name" manifest.
func parseManifest(data []byte, filename string) (string, error) {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if filepath.Base(fields[len(fields)-1]) == filename {
			hash := strings.ToLower(fields[0])
			if !isHex(hash) {
				return "", fmt.Errorf("the manifest holds an invalid checksum for %s", filename)
			}
			return hash, nil
		}
	}
	return "", fmt.Errorf("no checksum entry for %s", filename)
}

// isHex reports whether s is a lowercase hex string of a SHA-256 checksum.
func isHex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// download fetches one file, refusing anything larger than cap.
func download(ctx context.Context, url string, cap int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Pantau/"+UserAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %s for %s", resp.Status, url)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, cap+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > cap {
		return nil, fmt.Errorf("the download is larger than %d bytes", cap)
	}
	return data, nil
}

// pointSymlink points link at target, replacing an existing link.
func pointSymlink(link, target string) error {
	if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
		return err
	}
	tmp := link + ".new"
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, link); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// logWriter sends a command's output to the install log, line by line.
type logWriter struct {
	log io.Writer
	buf []byte
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		logf(w.log, "%s", strings.TrimRight(string(w.buf[:i]), "\r"))
		w.buf = w.buf[i+1:]
	}
	return len(p), nil
}

// logf writes one line to the install log, if there is one.
func logf(log io.Writer, format string, args ...any) {
	if log == nil {
		return
	}
	fmt.Fprintf(log, format+"\n", args...)
}

// runCmd runs a command and reports its output on failure.
func runCmd(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// firstLine returns the first line of a text.
func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}
