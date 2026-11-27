package teleport

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeExe creates an executable file at dir/name and returns its path.
func writeExe(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestResolveTSH(t *testing.T) {
	t.Parallel()

	found := func(string) (string, error) { return "/usr/bin/tsh", nil }
	missing := func(string) (string, error) { return "", errors.New("not found") }

	tests := []struct {
		name        string
		override    func(t *testing.T) string // "" means none
		managed     bool                      // install an executable <cli>/tsh
		managedData bool                      // install a non-executable <cli>/tsh
		lookPath    func(string) (string, error)
		wantSource  string
		wantPath    string // "<managed>" and "<override>" are replaced with real paths
		wantErr     error
		wantInval   bool
	}{
		{
			name:       "env override beats managed and system",
			override:   func(t *testing.T) string { return writeExe(t, t.TempDir(), "tsh-env") },
			managed:    true,
			lookPath:   found,
			wantSource: SourceEnv,
			wantPath:   "<override>",
		},
		{
			name:       "managed beats system",
			managed:    true,
			lookPath:   found,
			wantSource: SourceManaged,
			wantPath:   "<managed>",
		},
		{
			name:       "system PATH when nothing is managed",
			lookPath:   found,
			wantSource: SourceSystem,
			wantPath:   "/usr/bin/tsh",
		},
		{
			name:       "nothing anywhere is missing",
			lookPath:   missing,
			wantSource: SourceMissing,
			wantErr:    ErrTSHNotInstalled,
		},
		{
			name:        "non-executable managed file is skipped",
			managedData: true,
			lookPath:    found,
			wantSource:  SourceSystem,
			wantPath:    "/usr/bin/tsh",
		},
		{
			name: "env pointing at a missing file is an error, not a fallback",
			override: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "nope")
			},
			managed:    true,
			lookPath:   found,
			wantSource: SourceMissing,
			wantErr:    ErrTSHNotInstalled,
		},
		{
			name:       "relative override is rejected",
			override:   func(*testing.T) string { return "tsh" },
			lookPath:   found,
			wantSource: SourceMissing,
			wantInval:  true,
		},
		{
			name:       "flag-like override is rejected",
			override:   func(*testing.T) string { return "-o" },
			lookPath:   found,
			wantSource: SourceMissing,
			wantInval:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cli := t.TempDir()
			var managed string
			if tt.managed {
				managed = writeExe(t, cli, managedTSHName)
			}
			if tt.managedData {
				// A managed tsh without the execute bit must not be picked up.
				if err := os.WriteFile(filepath.Join(cli, managedTSHName), []byte("data"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			override := ""
			if tt.override != nil {
				override = tt.override(t)
			}

			got, err := ResolveTSH(override, cli, tt.lookPath)
			switch {
			case tt.wantInval:
				if !errors.Is(err, ErrInvalidInput) {
					t.Fatalf("error = %v, want ErrInvalidInput", err)
				}
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
			default:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			if got.Source != tt.wantSource {
				t.Fatalf("source = %q, want %q", got.Source, tt.wantSource)
			}
			if tt.wantErr == nil && !tt.wantInval {
				want := tt.wantPath
				switch want {
				case "<managed>":
					want = managed
				case "<override>":
					want = override
				}
				if got.Path != want {
					t.Fatalf("path = %q, want %q", got.Path, want)
				}
			}
		})
	}
}

func TestParseTSHVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		raw    string
		want   string
		wantOK bool
	}{
		{name: "json from tsh version --format=json", raw: `{"version":"18.11.10","gitref":"v18.11.10-0-g3e50964c"}`, want: "18.11.10", wantOK: true},
		{name: "json with a non-release version is not trusted", raw: `{"version":"latest"}`, wantOK: false},
		{name: "text form falls back to the first version", raw: "Teleport v18.11.10 git:v18.11.10-0-g3e50964c go1.26.10\n", want: "18.11.10", wantOK: true},
		{name: "text form without a newline", raw: "Teleport v17.2.1", want: "17.2.1", wantOK: true},
		{name: "no version at all", raw: "tsh: error: unknown flag", wantOK: false},
		{name: "empty output", raw: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ParseTSHVersion([]byte(tt.raw))
			if ok != tt.wantOK || got != tt.want {
				t.Fatalf("ParseTSHVersion() = (%q, %v), want (%q, %v)", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
