package ansible

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGalaxyEchoesCommandAndHeartbeatsWhileSilent(t *testing.T) {
	bin := t.TempDir()
	// Silent for a while, like ansible-galaxy resolving and downloading.
	script := "#!/bin/sh\nsleep 0.5\necho done-output\n"
	if err := os.WriteFile(filepath.Join(bin, "ansible-galaxy"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	old := heartbeatEvery
	heartbeatEvery = 100 * time.Millisecond
	defer func() { heartbeatEvery = old }()

	var out strings.Builder
	if err := galaxy(t.Context(), t.TempDir(), &out, "collection", "install", "a.b"); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.HasPrefix(got, "$ ansible-galaxy collection install a.b\n") {
		t.Errorf("the command must be echoed first, got %q", got)
	}
	if !strings.Contains(got, "… still working (") {
		t.Errorf("a silent install must report that it is alive, got %q", got)
	}
	if !strings.HasSuffix(got, "done-output\n") {
		t.Errorf("installer output must follow, got %q", got)
	}
	time.Sleep(250 * time.Millisecond)
	if after := out.String(); after != got {
		t.Errorf("heartbeat kept writing after the install finished: %q", after[len(got):])
	}
}
