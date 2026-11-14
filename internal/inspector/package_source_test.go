package inspector

import (
	"testing"
)

// The package inventory must record the source package (dpkg) / origin package (apk), because the
// vulnerability feeds track source packages, not binary packages.
func TestParseDpkgSourcePackage(t *testing.T) {
	// columns: name, version, installed-size, status, source package, source version
	out := "libssl3\t3.0.11-1~deb12u2\t5000\tinstalled\topenssl\t3.0.11-1~deb12u2\n" +
		"nginx\t1.22.1-9\t1000\tinstalled\tnginx\t1.22.1-9\n" +
		"libc6\t2.36-9\t10000\tinstalled\t\t\n"
	pkgs := parseDpkgOutput(out, map[string]bool{}, map[string]string{}, 7)
	if len(pkgs) != 3 {
		t.Fatalf("expected 3 packages, got %d", len(pkgs))
	}
	if pkgs[0].Source != "openssl" || pkgs[0].SourceVersion != "3.0.11-1~deb12u2" {
		t.Errorf("unexpected source of libssl3: %+v", pkgs[0])
	}
	if pkgs[1].Source != "nginx" {
		t.Errorf("unexpected source of nginx: %+v", pkgs[1])
	}
	// no source package recorded: fall back to the package name and version
	if pkgs[2].Source != "libc6" || pkgs[2].SourceVersion != "2.36-9" {
		t.Errorf("unexpected fallback source of libc6: %+v", pkgs[2])
	}
}

func TestParseApkOriginPackage(t *testing.T) {
	// columns: name, version, size, origin package
	out := "busybox\t1.36.1-r19\t966656\tbusybox\n" +
		"libcrypto3\t3.1.4-r5\t2000000\topenssl\n"
	pkgs := parseApkOutput(out, map[string]bool{"libcrypto3": true}, map[string]string{}, 9)
	if len(pkgs) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(pkgs))
	}
	if pkgs[0].Name != "busybox" || pkgs[0].Source != "busybox" {
		t.Errorf("unexpected busybox package: %+v", pkgs[0])
	}
	if pkgs[1].Name != "libcrypto3" || pkgs[1].Source != "openssl" {
		t.Errorf("unexpected libcrypto3 package: %+v", pkgs[1])
	}
	if !pkgs[1].IsManual {
		t.Error("libcrypto3 was installed manually and must be flagged as such")
	}
}
