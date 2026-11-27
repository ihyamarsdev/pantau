package teleport

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tarFile is one entry of a fake release archive. The zero typ is a regular file.
type tarFile struct {
	name string
	body string
	typ  byte
}

// makeTarball builds a gzip-compressed tar archive from files.
func makeTarball(t *testing.T, files ...tarFile) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range files {
		typ := f.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		hdr := &tar.Header{Name: f.name, Mode: 0o755, Typeflag: typ}
		if typ == tar.TypeReg {
			hdr.Size = int64(len(f.body))
		} else {
			hdr.Linkname = "/bin/sh"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write([]byte(f.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha256Of(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// tarballName is the release file name for this platform, as the CDN publishes it.
func tarballName(t *testing.T, version string) string {
	t.Helper()
	osName, arch, err := installPlatform()
	if err != nil {
		t.Skip(err)
	}
	return fmt.Sprintf("teleport-v%s-%s-%s-bin.tar.gz", version, osName, arch)
}

// newInstallManager returns a Manager with no PANTAU_TSH_BIN override, wired to up, plus its managed directory.
func newInstallManager(t *testing.T, up *fakeUpstream, runner Runner) (*Manager, string) {
	t.Helper()
	m, err := NewManager(t.TempDir(), "", runner)
	if err != nil {
		t.Fatal(err)
	}
	up.wire(t, m)
	return m, m.cliDir()
}

// leftoverTemps lists the temporary files an install may leave in dir.
func leftoverTemps(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tsh-") {
			out = append(out, e.Name())
		}
	}
	return out
}

func TestInstallTSH(t *testing.T) {
	t.Parallel()

	const target = "18.11.10"
	safe := []tarFile{{name: "teleport/tctl", body: "tctl"}, {name: "teleport/tsh", body: "new-tsh"}}

	tests := []struct {
		name         string
		files        []tarFile
		badSum       bool   // the .sha256 file names a different checksum
		reported     string // version the new binary reports; "" means the target
		oldInstalled bool   // a working tsh is already managed
		wantErr      string // substring of the error; "" means success
	}{
		{name: "verified tsh is installed", files: safe},
		{name: "checksum mismatch refuses the install", files: safe, badSum: true, wantErr: "does not match"},
		{name: "tar-slip entry is refused, previous tsh kept", files: append(safe, tarFile{name: "../evil", body: "pwned"}), oldInstalled: true, wantErr: "unsafe path"},
		{name: "absolute path entry is refused", files: append(safe, tarFile{name: "/etc/evil", body: "pwned"}), wantErr: "unsafe path"},
		{name: "symlink entry is refused", files: []tarFile{{name: "teleport/tsh", typ: tar.TypeSymlink}}, wantErr: "link"},
		{name: "archive without tsh is refused", files: []tarFile{{name: "teleport/tctl", body: "tctl"}}, wantErr: "holds no teleport/tsh"},
		{name: "smoke test failure keeps the previous tsh", files: safe, reported: "18.9.0", oldInstalled: true, wantErr: "reported"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			name := tarballName(t, target)
			data := makeTarball(t, tt.files...)
			sum := sha256Of(data)
			if tt.badSum {
				sum = strings.Repeat("0", 64)
			}
			up := newFakeUpstream(t)
			up.set(func(f *fakeUpstream) {
				f.files["/"+name] = data
				f.files["/"+name+".sha256"] = []byte(sum + "  " + name + "\n")
			})

			reported := tt.reported
			if reported == "" {
				reported = target
			}
			m, cli := newInstallManager(t, up, versionRunner(reported))
			final := filepath.Join(cli, managedTSHName)
			if tt.oldInstalled {
				if err := os.MkdirAll(cli, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(final, []byte("old-tsh"), 0o755); err != nil {
					t.Fatal(err)
				}
			}

			var log bytes.Buffer
			err := m.InstallTSH(context.Background(), target, &log)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("InstallTSH() error = %v", err)
				}
				if !strings.Contains(log.String(), "Checksum verified") {
					t.Fatalf("log does not show the checksum step:\n%s", log.String())
				}
			}

			if leftover := leftoverTemps(t, cli); len(leftover) != 0 {
				t.Fatalf("temporary files left behind: %v", leftover)
			}

			got, readErr := os.ReadFile(final)
			switch {
			case tt.wantErr == "" && tt.oldInstalled && readErr == nil && string(got) == "old-tsh":
				t.Fatal("a successful install must replace the previous tsh")
			case tt.wantErr == "" && readErr != nil:
				t.Fatalf("installed tsh missing: %v", readErr)
			case tt.wantErr == "" && string(got) != "new-tsh":
				t.Fatalf("installed tsh = %q, want the new binary", got)
			case tt.wantErr != "" && tt.oldInstalled && string(got) != "old-tsh":
				t.Fatalf("failed install changed the previous tsh to %q", got)
			case tt.wantErr != "" && !tt.oldInstalled && readErr == nil:
				t.Fatalf("failed install created %s", final)
			}

			if tt.wantErr == "" {
				// Resolution now picks the managed binary, which reports the new version.
				loc, err := m.Locate()
				if err != nil || loc.Source != SourceManaged || loc.Path != final {
					t.Fatalf("Locate() = (%+v, %v), want managed %s", loc, err, final)
				}
				if info := m.TSHInfo(context.Background()); info.Version != target {
					t.Fatalf("TSHInfo version = %q, want %q", info.Version, target)
				}
			}
		})
	}
}

func TestInstallTSHRejectsBadVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
	}{
		{name: "word", version: "latest"},
		{name: "two parts", version: "18.11"},
		{name: "pre-release", version: "18.11.10-rc1"},
		{name: "path traversal", version: "../18.11.10"},
		{name: "newline injection", version: "18.11.10\nrm"},
		{name: "empty", version: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			up := newFakeUpstream(t)
			m, cli := newInstallManager(t, up, versionRunner("18.11.10"))

			err := m.InstallTSH(context.Background(), tt.version, nil)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error = %v, want ErrInvalidInput", err)
			}
			if up.requests() != 0 {
				t.Fatal("an invalid version must not produce a request")
			}
			if _, err := os.Stat(cli); err == nil {
				t.Fatal("an invalid version must not create the cli directory")
			}
		})
	}
}

func TestInstallTSHRejectsConcurrentInstall(t *testing.T) {
	t.Parallel()

	const target = "18.11.10"
	name := tarballName(t, target)
	data := makeTarball(t, tarFile{name: "teleport/tsh", body: "new-tsh"})

	up := newFakeUpstream(t)
	gate := make(chan struct{})
	up.set(func(f *fakeUpstream) {
		f.files["/"+name] = data
		f.files["/"+name+".sha256"] = []byte(sha256Of(data) + "  " + name + "\n")
		f.gate = gate
	})
	m, _ := newInstallManager(t, up, versionRunner(target))

	first := make(chan error, 1)
	go func() { first <- m.InstallTSH(context.Background(), target, nil) }()

	// The first install is parked on the gated download. Wait for that, not for a timer.
	<-up.started

	if err := m.InstallTSH(context.Background(), target, nil); !errors.Is(err, ErrInstallInProgress) {
		t.Fatalf("second install error = %v, want ErrInstallInProgress", err)
	}

	close(gate)
	if err := <-first; err != nil {
		t.Fatalf("first install error = %v", err)
	}
}

func TestParseSHA256File(t *testing.T) {
	t.Parallel()

	good := strings.Repeat("ab", 32)
	tests := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{name: "two spaces, name matches", body: good + "  teleport-v18.11.10-linux-amd64-bin.tar.gz\n", want: good},
		{name: "upper-case hex is accepted", body: strings.ToUpper(good) + " teleport-v18.11.10-linux-amd64-bin.tar.gz\n", want: good},
		{name: "other file only", body: good + "  teleport-v18.11.10-linux-arm64-bin.tar.gz\n", wantErr: true},
		{name: "invalid checksum", body: "zz  teleport-v18.11.10-linux-amd64-bin.tar.gz\n", wantErr: true},
		{name: "empty file", body: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseSHA256File([]byte(tt.body), "teleport-v18.11.10-linux-amd64-bin.tar.gz")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %q", got)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("parseSHA256File() = (%q, %v), want (%q, nil)", got, err, tt.want)
			}
		})
	}
}
