package teleport

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// Installing tsh (ADR 0081). The release tarball is downloaded, its SHA-256 is checked against the .sha256 file
// the CDN publishes beside it, and only teleport/tsh is taken out of the archive. The binary must report the target
// version before it replaces <data>/cli/tsh, so a failed install leaves the working binary in place. Nothing needs
// root and nothing outside <data>/cli is written.

const (
	installTimeout  = 10 * time.Minute
	smokeTimeout    = 30 * time.Second
	maxTarballBytes = 400 << 20 // the release tarball
	maxTSHBytes     = 400 << 20 // tsh itself is about 150 MB
	maxArchiveBytes = 1 << 30   // bytes read from the decompressed stream, which bounds a gzip bomb
	maxSumBytes     = 4 << 10
	tshEntry        = "teleport/tsh"
)

// ErrInstallInProgress means another tsh install is running. The API answers 409 Conflict.
var ErrInstallInProgress = errors.New("a tsh install is already running")

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// InstallTSH downloads, verifies and installs one tsh release into <data>/cli/tsh. Progress lines go to log. Only one
// install runs at a time; a second call returns ErrInstallInProgress without doing anything.
func (m *Manager) InstallTSH(ctx context.Context, version string, log io.Writer) error {
	if !releaseVersion.MatchString(version) {
		return fmt.Errorf("%w: tsh version must be a release number such as 18.11.10", ErrInvalidInput)
	}
	if !m.installMu.TryLock() {
		return ErrInstallInProgress
	}
	defer m.installMu.Unlock()

	osName, arch, err := installPlatform()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()

	cli := m.cliDir()
	if err := os.MkdirAll(cli, 0o755); err != nil {
		return fmt.Errorf("create cli directory: %w", err)
	}
	if override := m.tshOverride; override != "" {
		logf(log, "PANTAU_TSH_BIN is set to %s, so Pantau runs that binary and not the managed tsh.", override)
	}

	name := fmt.Sprintf("teleport-v%s-%s-%s-bin.tar.gz", version, osName, arch)
	base := m.up.CDN + "/" + name

	logf(log, "Downloading the checksum of %s…", name)
	sumBody, err := m.fetchSmall(ctx, base+".sha256")
	if err != nil {
		return fmt.Errorf("the checksum could not be downloaded: %w", err)
	}
	want, err := parseSHA256File(sumBody, name)
	if err != nil {
		return err
	}

	logf(log, "Downloading %s…", name)
	tarball, got, err := m.downloadTarball(ctx, base, cli)
	if err != nil {
		return err
	}
	defer os.Remove(tarball)
	if got != want {
		return fmt.Errorf("the checksum of the download does not match the one Teleport publishes (expected %s…, got %s…); nothing was installed", want[:16], got[:16])
	}
	logf(log, "Checksum verified (%s…)", want[:16])

	candidate, err := extractTSH(tarball, cli)
	if err != nil {
		return err
	}
	defer os.Remove(candidate) // a no-op once the candidate has been renamed into place

	logf(log, "Checking the new tsh…")
	if err := m.smokeTSH(ctx, candidate, version); err != nil {
		return err
	}

	final := filepath.Join(cli, managedTSHName)
	if err := os.Rename(candidate, final); err != nil {
		return fmt.Errorf("the binary could not be put in place: %w", err)
	}
	logf(log, "Installed tsh %s into %s", version, final)
	return nil
}

// installPlatform returns the OS and architecture names used in the tarball file name.
func installPlatform() (string, string, error) {
	switch runtime.GOOS {
	case "linux", "darwin":
	default:
		return "", "", fmt.Errorf("%w: the tsh install is not supported on %s", ErrInvalidInput, runtime.GOOS)
	}
	switch runtime.GOARCH {
	case "amd64", "arm64":
	default:
		return "", "", fmt.Errorf("%w: the tsh install is not supported on %s", ErrInvalidInput, runtime.GOARCH)
	}
	return runtime.GOOS, runtime.GOARCH, nil
}

// fetchSmall reads a short file such as a checksum file, refusing anything larger than maxSumBytes.
func (m *Manager) fetchSmall(ctx context.Context, url string) ([]byte, error) {
	resp, err := m.get(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, maxSumBytes))
}

// get performs one GET against the configured upstream and requires HTTP 200.
func (m *Manager) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Pantau")
	resp, err := m.up.Client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("status %s for %s", resp.Status, url)
	}
	return resp, nil
}

// parseSHA256File finds the checksum of one file in a "<sha256>  <name>" file, as the Teleport CDN publishes it.
func parseSHA256File(body []byte, name string) (string, error) {
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || path.Base(strings.TrimPrefix(fields[len(fields)-1], "*")) != name {
			continue
		}
		sum := strings.ToLower(fields[0])
		if !sha256Hex.MatchString(sum) {
			return "", fmt.Errorf("the checksum file holds an invalid checksum for %s", name)
		}
		return sum, nil
	}
	return "", fmt.Errorf("the checksum file has no entry for %s", name)
}

// downloadTarball streams the tarball into a temporary file in dir and returns its path and SHA-256. The file is
// never held in memory.
func (m *Manager) downloadTarball(ctx context.Context, url, dir string) (string, string, error) {
	resp, err := m.get(ctx, url)
	if err != nil {
		return "", "", fmt.Errorf("the release could not be downloaded: %w", err)
	}
	defer resp.Body.Close()

	f, err := os.CreateTemp(dir, ".tsh-*.tar.gz")
	if err != nil {
		return "", "", fmt.Errorf("create download file: %w", err)
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxTarballBytes+1))
	closeErr := f.Close()
	if copyErr == nil && n > maxTarballBytes {
		copyErr = fmt.Errorf("the download is larger than %d bytes", maxTarballBytes)
	}
	if copyErr != nil || closeErr != nil {
		os.Remove(f.Name())
		return "", "", fmt.Errorf("the release could not be downloaded: %w", errors.Join(copyErr, closeErr))
	}
	return f.Name(), hex.EncodeToString(h.Sum(nil)), nil
}

// extractTSH takes teleport/tsh out of the tarball and writes it to a candidate file in dir, mode 0755. The whole
// archive is read: an unsafe entry anywhere (an absolute path, a ".." segment, a backslash, or any link) fails the
// install, not only one in front of tsh.
func extractTSH(tarball, dir string) (string, error) {
	f, err := os.Open(tarball)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("the release is not a gzip archive: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(io.LimitReader(gz, maxArchiveBytes))
	var candidate string
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			removeIfSet(candidate)
			return "", fmt.Errorf("the release archive is unreadable: %w", err)
		}
		if err := checkEntry(h); err != nil {
			removeIfSet(candidate)
			return "", err
		}
		if path.Clean(h.Name) != tshEntry {
			continue
		}
		if h.Typeflag != tar.TypeReg {
			removeIfSet(candidate)
			return "", fmt.Errorf("%s in the archive is not a regular file", h.Name)
		}
		if h.Size > maxTSHBytes {
			removeIfSet(candidate)
			return "", fmt.Errorf("%s is larger than %d bytes", h.Name, maxTSHBytes)
		}
		// A second tsh entry replaces the first one, which must not be left behind.
		removeIfSet(candidate)
		if candidate, err = writeCandidate(tr, dir); err != nil {
			return "", err
		}
	}
	if candidate == "" {
		return "", fmt.Errorf("the release archive holds no %s", tshEntry)
	}
	return candidate, nil
}

// checkEntry refuses any archive entry that could name a path outside the directory, and every link.
func checkEntry(h *tar.Header) error {
	name := h.Name
	if name == "" || path.IsAbs(name) || strings.ContainsAny(name, "\\\x00") {
		return fmt.Errorf("the release archive holds an unsafe path %q", name)
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == ".." {
			return fmt.Errorf("the release archive holds an unsafe path %q", name)
		}
	}
	if h.Typeflag == tar.TypeSymlink || h.Typeflag == tar.TypeLink {
		return fmt.Errorf("the release archive holds a link %q", name)
	}
	return nil
}

// writeCandidate writes the tsh entry to a uniquely named file in dir, so two installs never share a file.
func writeCandidate(r io.Reader, dir string) (string, error) {
	f, err := os.CreateTemp(dir, ".tsh-*.candidate")
	if err != nil {
		return "", err
	}
	n, copyErr := io.Copy(f, io.LimitReader(r, maxTSHBytes+1))
	if copyErr == nil && n > maxTSHBytes {
		copyErr = fmt.Errorf("tsh is larger than %d bytes", maxTSHBytes)
	}
	if copyErr == nil {
		copyErr = f.Chmod(0o755)
	}
	closeErr := f.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("the tsh binary could not be written: %w", err)
	}
	return f.Name(), nil
}

// smokeTSH runs the candidate and requires it to report the target version.
func (m *Manager) smokeTSH(ctx context.Context, bin, version string) error {
	ctx, cancel := context.WithTimeout(ctx, smokeTimeout)
	defer cancel()
	res, err := m.runner.Run(ctx, os.Environ(), bin, "version", "--client", "--format=json")
	if err != nil {
		return fmt.Errorf("the new tsh did not run: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("the new tsh exited with code %d", res.ExitCode)
	}
	got, ok := ParseTSHVersion(res.Stdout)
	if !ok || got != version {
		return fmt.Errorf("the new tsh reported %q, expected %s", got, version)
	}
	return nil
}

func removeIfSet(p string) {
	if p != "" {
		os.Remove(p)
	}
}

// logf writes one progress line when there is a log.
func logf(log io.Writer, format string, args ...any) {
	if log == nil {
		return
	}
	fmt.Fprintf(log, format+"\n", args...)
}
