package ansible

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type bundleEntry struct {
	name, content string
	mode          os.FileMode
}

func bundleZIP(t *testing.T, entries ...bundleEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, entry := range entries {
		h := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		if entry.mode != 0 {
			h.SetMode(entry.mode)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(entry.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestValidateBundleFilePath(t *testing.T) {
	for _, name := range []string{"site.yml", "playbooks/site.yaml", ".ansible/collections/ansible_collections/acme/demo/plugins/modules/test.py", ".env", "a folder/file"} {
		if err := ValidateBundleFilePath(name); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
	for _, name := range []string{"", ".", "..", "../escape", "/absolute", "a/../../escape", "a/../b", "./a", "a//b", "a/", `a\b`, `C:\x`, "C:x", "a:b", `a"b.yml`, ".git/config", "nested/.GiT/config", "a\x00b", "a\nb", "a\x7fb", "a\u0085b", "a/\xff"} {
		if err := ValidateBundleFilePath(name); err == nil {
			t.Errorf("unsafe path %q accepted", name)
		}
	}
}

func TestValidateArchive(t *testing.T) {
	site := bundleEntry{name: "site.yml", content: "- hosts: all\n"}
	t.Run("returns entries", func(t *testing.T) {
		entries, err := ValidateArchive(bundleZIP(t, site, bundleEntry{name: "roles/a/tasks/main.yml", content: "[]"}))
		if err != nil || len(entries) != 2 || entries["site.yml"] == nil {
			t.Fatalf("entries=%v err=%v", entries, err)
		}
	})
	t.Run("rejects unsafe entries", func(t *testing.T) {
		for _, name := range []string{"../escape", "/abs", ".git/config", "a\\b"} {
			if _, err := ValidateArchive(bundleZIP(t, site, bundleEntry{name: name, content: "x"})); err == nil {
				t.Errorf("entry %q accepted", name)
			}
		}
		if _, err := ValidateArchive(bundleZIP(t, site, site)); err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Errorf("duplicate entry: %v", err)
		}
		if _, err := ValidateArchive(bundleZIP(t, bundleEntry{name: "link", content: "/etc/passwd", mode: os.ModeSymlink | 0777})); err == nil {
			t.Error("symlink entry accepted")
		}
		if _, err := ValidateArchive([]byte("not a zip")); err == nil {
			t.Error("garbage accepted")
		}
	})
	t.Run("ZIP byte limit", func(t *testing.T) {
		if _, err := ValidateArchive(make([]byte, MaxBundleSize+1)); err == nil || !strings.Contains(err.Error(), "archive exceeds") {
			t.Fatalf("archive limit: %v", err)
		}
	})
	t.Run("expanded total limit", func(t *testing.T) {
		data := strings.Repeat("z", MaxBundleSize/2)
		if _, err := ValidateArchive(bundleZIP(t, site, bundleEntry{name: "one", content: data}, bundleEntry{name: "two", content: data})); err == nil || !strings.Contains(err.Error(), "expanded size") {
			t.Fatalf("expanded limit: %v", err)
		}
	})
	t.Run("entry count limit", func(t *testing.T) {
		entries := []bundleEntry{site}
		for i := 1; i < MaxBundleFiles; i++ {
			entries = append(entries, bundleEntry{name: fmt.Sprintf("files/%d", i)})
		}
		if _, err := ValidateArchive(bundleZIP(t, entries...)); err != nil {
			t.Fatalf("entry count boundary rejected: %v", err)
		}
		entries = append(entries, bundleEntry{name: "one-too-many"})
		if _, err := ValidateArchive(bundleZIP(t, entries...)); err == nil || !strings.Contains(err.Error(), "maximum of 10000") {
			t.Fatalf("entry limit: %v", err)
		}
	})
	t.Run("corrupt data", func(t *testing.T) {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		for _, e := range []bundleEntry{site, {name: "files/payload", content: "unique stored payload"}} {
			w, err := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: zip.Store})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write([]byte(e.content)); err != nil {
				t.Fatal(err)
			}
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		archive := buf.Bytes()
		index := bytes.Index(archive, []byte("unique stored payload"))
		if index < 0 {
			t.Fatal("payload missing")
		}
		archive[index] ^= 1
		if _, err := ValidateArchive(archive); err == nil || !strings.Contains(err.Error(), "checksum") {
			t.Fatalf("corrupt payload: %v", err)
		}
	})
}

func TestExtractArchivePrivatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions")
	}
	archive := bundleZIP(t,
		bundleEntry{name: "site.yml", content: "- hosts: all\n", mode: 0666},
		bundleEntry{name: "files/script", content: "#!/bin/sh\n", mode: 0777 | os.ModeSetuid},
	)
	if _, err := ValidateArchive(archive); err != nil {
		t.Fatal(err)
	}
	project, err := ExtractArchive(archive, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]os.FileMode{".": 0700, "site.yml": 0600, "files": 0700, "files/script": 0700} {
		st, err := os.Stat(filepath.Join(project, name))
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != want || st.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
			t.Errorf("%s: permissions %v, want %o with no special bits", name, st.Mode(), want)
		}
	}
}
