package gwupdate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A check with a newer upstream release reports an update.
func TestCheckReportsUpdate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version":"1.17.2"}`))
	}))
	defer srv.Close()

	old := HashiCorpURL
	HashiCorpURL = srv.URL
	defer func() { HashiCorpURL = old }()

	res := Check(context.Background(), TargetTerraform, "1.15.9")
	if res.Status != StatusUpdate {
		t.Errorf("status = %q, want %q", res.Status, StatusUpdate)
	}
	if res.Latest != "1.17.2" {
		t.Errorf("latest = %q, want 1.17.2", res.Latest)
	}
	if res.Installed != "1.15.9" {
		t.Errorf("installed = %q, want 1.15.9", res.Installed)
	}
	if res.Error != "" {
		t.Errorf("error = %q, want empty", res.Error)
	}
}

// A check with the same version upstream reports "current", never "update".
func TestCheckReportsCurrent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version":"1.17.2"}`))
	}))
	defer srv.Close()

	old := HashiCorpURL
	HashiCorpURL = srv.URL
	defer func() { HashiCorpURL = old }()

	res := Check(context.Background(), TargetTerraform, "1.17.2")
	if res.Status != StatusCurrent {
		t.Errorf("status = %q, want %q", res.Status, StatusCurrent)
	}
}

// An installed version that is newer than upstream is still "current": upstream may lag behind a
// build that is not published yet.
func TestCheckReportsCurrentForNewerInstalled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version":"1.17.2"}`))
	}))
	defer srv.Close()

	old := HashiCorpURL
	HashiCorpURL = srv.URL
	defer func() { HashiCorpURL = old }()

	res := Check(context.Background(), TargetTerraform, "1.18.0")
	if res.Status != StatusCurrent {
		t.Errorf("status = %q, want %q", res.Status, StatusCurrent)
	}
}

// An upstream that cannot be reached is an error, never "up to date".
func TestCheckReportsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	old := HashiCorpURL
	HashiCorpURL = srv.URL
	defer func() { HashiCorpURL = old }()

	res := Check(context.Background(), TargetTerraform, "1.15.9")
	if res.Status != StatusError {
		t.Errorf("status = %q, want %q", res.Status, StatusError)
	}
	if res.Error == "" {
		t.Error("expected an error message")
	}
	if res.Latest != "" {
		t.Errorf("latest = %q, want empty", res.Latest)
	}
}

// An installed version that cannot be parsed is unknown, never an update: an unusual version string
// must not be presented as outdated.
func TestCheckReportsUnknownForUnparsableVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version":"1.17.2"}`))
	}))
	defer srv.Close()

	old := HashiCorpURL
	HashiCorpURL = srv.URL
	defer func() { HashiCorpURL = old }()

	res := Check(context.Background(), TargetTerraform, "dev-build")
	if res.Status != StatusUnknown {
		t.Errorf("status = %q, want %q", res.Status, StatusUnknown)
	}
	if res.Error == "" {
		t.Error("expected an error message")
	}
}

// A binary that is not installed is unknown and never reaches the upstream.
func TestCheckReportsUnknownWithoutInstalledVersion(t *testing.T) {
	res := Check(context.Background(), TargetTerraform, "")
	if res.Status != StatusUnknown {
		t.Errorf("status = %q, want %q", res.Status, StatusUnknown)
	}
	if res.Error == "" {
		t.Error("expected an error message")
	}
}
