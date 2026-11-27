package teleport

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLocateTSH(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	exe := filepath.Join(dir, "tsh")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	notExe := filepath.Join(dir, "tsh-data")
	if err := os.WriteFile(notExe, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	found := func(string) (string, error) { return "/usr/bin/tsh", nil }
	missing := func(string) (string, error) { return "", errors.New("not found") }

	tests := []struct {
		name      string
		override  string
		lookPath  func(string) (string, error)
		want      string
		wantMiss  bool
		wantInval bool
	}{
		{name: "PATH lookup finds tsh", lookPath: found, want: "/usr/bin/tsh"},
		{name: "PATH without tsh is a sentinel error", lookPath: missing, wantMiss: true},
		{name: "override executable is used", override: exe, lookPath: missing, want: exe},
		{name: "override not executable", override: notExe, lookPath: found, wantMiss: true},
		{name: "override missing file", override: filepath.Join(dir, "nope"), lookPath: found, wantMiss: true},
		{name: "override relative is rejected", override: "tsh", lookPath: found, wantInval: true},
		{name: "override flag-like is rejected", override: "-o", lookPath: found, wantInval: true},
		{name: "override directory is rejected", override: dir, lookPath: found, wantMiss: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := LocateTSH(tt.override, tt.lookPath)
			switch {
			case tt.wantMiss:
				if !errors.Is(err, ErrTSHNotInstalled) {
					t.Fatalf("error = %v, want ErrTSHNotInstalled", err)
				}
			case tt.wantInval:
				if !errors.Is(err, ErrInvalidInput) {
					t.Fatalf("error = %v, want ErrInvalidInput", err)
				}
			default:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != tt.want {
					t.Fatalf("LocateTSH() = %q, want %q", got, tt.want)
				}
			}
		})
	}
}
