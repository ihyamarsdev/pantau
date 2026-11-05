package ansible

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestExtractBundleFileConfined(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlinks")
	}
	project, outside := t.TempDir(), t.TempDir()
	root, err := os.OpenRoot(project)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.WriteFile(filepath.Join(outside, "sentinel"), []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	// Simulate filesystem links appearing after archive validation, not uploaded symlink entries.
	if err := os.Symlink(outside, filepath.Join(project, "parent")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "sentinel"), filepath.Join(project, "leaf")); err != nil {
		t.Fatal(err)
	}
	archive := bundleZIP(t, bundleEntry{name: "payload", content: "must stay inside"})
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	for _, dest := range []string{"parent/escaped", "leaf", "../escaped"} {
		if err := extractBundleFile(root, zr.File[0], dest); err == nil {
			t.Errorf("escaping destination %q accepted", dest)
		}
	}
	if err := root.MkdirAll("parent/escaped-dir", 0700); err == nil {
		t.Error("directory creation followed escaping symlink")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 1 {
		t.Fatalf("outside directory modified: %v %v", entries, err)
	}
	data, err := os.ReadFile(filepath.Join(outside, "sentinel"))
	if err != nil || string(data) != "unchanged" {
		t.Fatalf("outside sentinel modified: %q %v", data, err)
	}
}
