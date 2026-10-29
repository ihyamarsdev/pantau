package inspector

import (
	"strings"
	"testing"
)

func TestDarwinPathPreludeContract(t *testing.T) {
	expected := "case \"$(uname -s)\" in Darwin) PATH=\"$PATH:/usr/local/bin:/opt/homebrew/bin:/opt/homebrew/sbin:/Applications/Docker.app/Contents/Resources/bin\"; export PATH;; esac; "
	if DarwinPathPrelude != expected {
		t.Fatalf("DarwinPathPrelude does not match shared contract:\ngot:  %q\nwant: %q", DarwinPathPrelude, expected)
	}

	// Verify required paths are included
	requiredPaths := []string{
		"/usr/local/bin",
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		"/Applications/Docker.app/Contents/Resources/bin",
	}
	for _, p := range requiredPaths {
		if !strings.Contains(DarwinPathPrelude, p) {
			t.Errorf("expected DarwinPathPrelude to contain %q", p)
		}
	}
}
