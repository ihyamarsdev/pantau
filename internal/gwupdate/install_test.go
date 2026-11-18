package gwupdate

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeRelease serves a release: the checksum manifest, the zip holding one binary, and (for
// ansible) the PyPI JSON with the wheel and its checksum.
type fakeRelease struct {
	product string // "terraform" or "tofu"
	version string
	corrupt bool // the archive does not match the checksum
	noEntry bool // the manifest has no entry for the asset
	srv     *httptest.Server
}

func newFakeRelease(t *testing.T, product, version string) *fakeRelease {
	t.Helper()
	f := &fakeRelease{product: product, version: version}

	mux := http.NewServeMux()
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "_SHA256SUMS"):
			if f.noEntry {
				w.Write([]byte(strings.Repeat("0", 64) + "  other.zip\n"))
				return
			}
			// The asset name is the one the installer asks for: <prefix>_<os>_<arch>.zip.
			prefix := strings.TrimSuffix(filepath.Base(path), "_SHA256SUMS")
			asset := fmt.Sprintf("%s_%s_%s.zip", prefix, runtime.GOOS, runtime.GOARCH)
			fmt.Fprintf(w, "%s  %s\n", f.checksum(), asset)
		case strings.HasSuffix(path, ".zip"):
			w.Write(f.zip())
		case strings.Contains(path, "/pypi/"):
			f.writePyPI(w)
		default:
			http.NotFound(w, r)
		}
	})
	return f
}

// checksum is the checksum of the archive the fake release serves.
func (f *fakeRelease) checksum() string {
	if f.corrupt {
		return strings.Repeat("0", 64)
	}
	sum := sha256.Sum256(f.zip())
	return hex.EncodeToString(sum[:])
}

// zip builds a release archive holding one binary that reports the release version.
func (f *fakeRelease) zip() []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(f.product)
	w.Write([]byte("#!/bin/sh\necho '" + f.product + " v" + f.version + "'\n"))
	zw.Close()
	return buf.Bytes()
}

// writePyPI serves the PyPI JSON of the ansible-core release.
func (f *fakeRelease) writePyPI(w http.ResponseWriter) {
	wheel := f.zip()
	sum := sha256.Sum256(wheel)
	doc := map[string]any{
		"info": map[string]any{"version": f.version},
		"urls": []map[string]any{{
			"packagetype": "bdist_wheel",
			"filename":    "ansible_core-" + f.version + "-py3-none-any.whl",
			"url":         f.srv.URL + "/wheel/ansible_core-" + f.version + "-py3-none-any.whl",
			"digests":     map[string]any{"sha256": hex.EncodeToString(sum[:])},
		}},
	}
	json.NewEncoder(w).Encode(doc)
}

// point the package at the fake release.
func (f *fakeRelease) point(t *testing.T) {
	t.Helper()
	oldHC, oldOT, oldPyPI := HashiCorpBaseURL, OpenTofuBaseURL, PyPIWheelURL
	HashiCorpBaseURL = f.srv.URL
	OpenTofuBaseURL = f.srv.URL
	PyPIWheelURL = f.srv.URL + "/pypi/%s/json"
	t.Cleanup(func() {
		HashiCorpBaseURL, OpenTofuBaseURL, PyPIWheelURL = oldHC, oldOT, oldPyPI
	})
}

// A release whose checksum does not match the manifest is refused: nothing is installed.
func TestInstallStandaloneRefusesChecksumMismatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the standalone install is Linux/macOS only")
	}
	f := newFakeRelease(t, "terraform", "1.2.3")
	f.corrupt = true
	f.point(t)

	root := t.TempDir()
	err := Install(context.Background(), TargetTerraform, "1.2.3", root, nil)
	if err == nil {
		t.Fatal("expected the install to fail on a checksum mismatch")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Errorf("the error must name the checksum: %v", err)
	}
	// Nothing was installed.
	if _, ok := (ManagedDir{Root: root}).ManagedBinary("terraform"); ok {
		t.Error("a binary was installed despite the checksum mismatch")
	}
}

// A release whose manifest has no entry for the asset is refused.
func TestInstallStandaloneRefusesMissingManifestEntry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the standalone install is Linux/macOS only")
	}
	f := newFakeRelease(t, "terraform", "1.2.3")
	f.noEntry = true
	f.point(t)

	err := Install(context.Background(), TargetTerraform, "1.2.3", t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected the install to fail when the manifest has no entry")
	}
	if !strings.Contains(err.Error(), "no checksum") {
		t.Errorf("the error must name the missing checksum: %v", err)
	}
}

// A version that is not a release number is refused before any request is made.
func TestInstallRefusesNonReleaseVersion(t *testing.T) {
	err := Install(context.Background(), TargetTerraform, "latest", t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected the install to refuse a non-release version")
	}
	if !strings.Contains(err.Error(), "release number") {
		t.Errorf("the error must name the bad version: %v", err)
	}
}

// An unknown target is refused.
func TestInstallRefusesUnknownTarget(t *testing.T) {
	err := Install(context.Background(), "nope", "1.2.3", t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected the install to refuse an unknown target")
	}
}

// The manifest parser finds the checksum of one file and refuses a malformed one.
func TestParseManifest(t *testing.T) {
	hash := strings.Repeat("a", 64)
	other := strings.Repeat("b", 64)
	manifest := []byte(hash + "  terraform_1.2.3_linux_amd64.zip\n" + other + "  other.zip\n")
	got, err := parseManifest(manifest, "terraform_1.2.3_linux_amd64.zip")
	if err != nil {
		t.Fatalf("parseManifest: %v", err)
	}
	if got != hash {
		t.Errorf("checksum = %q, want %q", got, hash)
	}
	if _, err := parseManifest(manifest, "missing.zip"); err == nil {
		t.Error("expected an error for a file that is not in the manifest")
	}
	if _, err := parseManifest([]byte("short  file.zip\n"), "file.zip"); err == nil {
		t.Error("expected an error for a malformed checksum")
	}
	if _, err := parseManifest([]byte(strings.Repeat("z", 64)+"  file.zip\n"), "file.zip"); err == nil {
		t.Error("expected an error for a checksum that is not hex")
	}
}

// The zip reader takes the binary at the root and refuses an archive that holds none.
func TestBinaryFromZip(t *testing.T) {
	f := newFakeRelease(t, "terraform", "1.2.3")
	got, err := binaryFromZip(f.zip(), "terraform")
	if err != nil {
		t.Fatalf("binaryFromZip: %v", err)
	}
	if !strings.Contains(string(got), "v1.2.3") {
		t.Errorf("unexpected binary content: %q", got)
	}
	if _, err := binaryFromZip(f.zip(), "tofu"); err == nil {
		t.Error("expected an error when the archive holds no such binary")
	}
	if _, err := binaryFromZip([]byte("not a zip"), "terraform"); err == nil {
		t.Error("expected an error for an archive that is not a zip")
	}
}

// A binary nested in a directory is not taken: only the one at the root of the archive counts.
func TestBinaryFromZipIgnoresNestedFiles(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("sub/terraform")
	w.Write([]byte("nested"))
	zw.Close()
	if _, err := binaryFromZip(buf.Bytes(), "terraform"); err == nil {
		t.Error("expected a nested binary to be ignored")
	}
}

// The managed directory reports the binaries and the virtualenv it holds.
func TestManagedDir(t *testing.T) {
	root := t.TempDir()
	m := ManagedDir{Root: root}

	if _, ok := m.ManagedBinary("terraform"); ok {
		t.Error("an empty directory must not report a binary")
	}
	if _, ok := m.ManagedVenvBin("ansible-playbook"); ok {
		t.Error("an empty directory must not report a virtualenv command")
	}

	binDir := m.BinDir()
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "terraform"), []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
	p, ok := m.ManagedBinary("terraform")
	if !ok {
		t.Fatal("the managed binary was not found")
	}
	if filepath.Base(p) != "terraform" {
		t.Errorf("path = %q, want it to end in terraform", p)
	}

	venvBin := filepath.Join(m.VenvLink(), "bin", "ansible-playbook")
	if err := os.MkdirAll(filepath.Dir(venvBin), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(venvBin, []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.ManagedVenvBin("ansible-playbook"); !ok {
		t.Error("the managed virtualenv command was not found")
	}
}

// The symlink is replaced, not appended to, and points at the new target.
func TestPointSymlink(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "venv")
	if err := pointSymlink(link, filepath.Join(root, "a")); err != nil {
		t.Fatalf("pointSymlink: %v", err)
	}
	if err := pointSymlink(link, filepath.Join(root, "b")); err != nil {
		t.Fatalf("pointSymlink (second): %v", err)
	}
	dest, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if dest != filepath.Join(root, "b") {
		t.Errorf("the symlink points at %q, want %q", dest, filepath.Join(root, "b"))
	}
}

// The install log receives the steps of the install.
func TestInstallLogsItsSteps(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the standalone install is Linux/macOS only")
	}
	f := newFakeRelease(t, "terraform", "1.2.3")
	f.point(t)

	var log bytes.Buffer
	err := Install(context.Background(), TargetTerraform, "1.2.3", t.TempDir(), &log)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	text := log.String()
	for _, want := range []string{"checksum manifest", "Downloading", "Checksum verified", "Installed terraform 1.2.3"} {
		if !strings.Contains(text, want) {
			t.Errorf("the install log must mention %q:\n%s", want, text)
		}
	}
}

// A verified release is installed and the managed binary is found afterwards.
func TestInstallStandaloneInstallsTheBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the standalone install is Linux/macOS only")
	}
	f := newFakeRelease(t, "tofu", "1.13.9")
	f.point(t)

	root := t.TempDir()
	if err := Install(context.Background(), TargetOpenTofu, "1.13.9", root, nil); err != nil {
		t.Fatalf("Install: %v", err)
	}
	p, ok := (ManagedDir{Root: root}).ManagedBinary("tofu")
	if !ok {
		t.Fatal("the managed tofu binary was not found")
	}
	if st, err := os.Stat(p); err != nil || st.Mode()&0111 == 0 {
		t.Errorf("the managed binary must be executable: %v", err)
	}
}

// The version of the installed binary is checked before it is put in place: a binary that reports
// another version is refused.
func TestInstallRefusesBinaryWithWrongVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the standalone install is Linux/macOS only")
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("terraform")
	w.Write([]byte("#!/bin/sh\necho 'Terraform v0.0.1'\n"))
	zw.Close()
	archive := buf.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "_SHA256SUMS"):
			sum := sha256.Sum256(archive)
			name := filepath.Base(r.URL.Path)
			asset := fmt.Sprintf("%s_%s_%s.zip", strings.TrimSuffix(name, "_SHA256SUMS"), runtime.GOOS, runtime.GOARCH)
			fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), asset)
		case strings.HasSuffix(r.URL.Path, ".zip"):
			w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	oldHC := HashiCorpBaseURL
	HashiCorpBaseURL = srv.URL
	defer func() { HashiCorpBaseURL = oldHC }()

	root := t.TempDir()
	err := Install(context.Background(), TargetTerraform, "1.2.3", root, nil)
	if err == nil {
		t.Fatal("expected the install to refuse a binary that reports another version")
	}
	if !strings.Contains(err.Error(), "did not run") {
		t.Errorf("the error must name the failed check: %v", err)
	}
	if _, ok := (ManagedDir{Root: root}).ManagedBinary("terraform"); ok {
		t.Error("a binary was installed despite the failed check")
	}
}

// The PyPI lookup finds the wheel of one ansible-core release and the checksum it publishes.
func TestPyPIWheel(t *testing.T) {
	f := newFakeRelease(t, "ansible", "2.22.0")
	f.point(t)

	w, err := pypiWheel(context.Background(), "2.22.0")
	if err != nil {
		t.Fatalf("pypiWheel: %v", err)
	}
	if !strings.HasSuffix(w.Filename, ".whl") {
		t.Errorf("filename = %q, want a wheel", w.Filename)
	}
	if len(w.SHA256) != 64 {
		t.Errorf("checksum = %q, want 64 hex characters", w.SHA256)
	}
	if w.URL == "" {
		t.Error("the wheel has no download URL")
	}
}

// A release that does not exist on PyPI is an error, not an empty wheel.
func TestPyPIWheelMissingRelease(t *testing.T) {
	f := newFakeRelease(t, "ansible", "2.22.0")
	f.point(t)

	if _, err := pypiWheel(context.Background(), "9.9.9"); err == nil {
		t.Fatal("expected an error for a release that is not on PyPI")
	}
}
