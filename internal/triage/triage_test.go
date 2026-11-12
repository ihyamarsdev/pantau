package triage

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestClassify_DisabledReturnsFalse(t *testing.T) {
	c := New("", nil)
	res, ok := c.Classify(context.Background(), "Unit nginx.service is inactive")
	if ok {
		t.Fatalf("expected ok=false when disabled, got %+v", res)
	}
}

func TestClassify_UnreachableReturnsFalse(t *testing.T) {
	c := New("http://127.0.0.1:1", nil)
	res, ok := c.Classify(context.Background(), "boom")
	if ok {
		t.Fatalf("expected ok=false when unreachable, got %+v", res)
	}
}

func TestClassify_TimeoutReturnsFalse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	res, ok := New(ts.URL, nil).Classify(ctx, "boom")
	if ok {
		t.Fatalf("expected ok=false on timeout, got %+v", res)
	}
}

func TestClassify_ServerErrorReturnsFalse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ts.Close()

	res, ok := New(ts.URL, nil).Classify(context.Background(), "boom")
	if ok {
		t.Fatalf("expected ok=false on server error, got %+v", res)
	}
}

func TestClassify_MissingCategoryReturnsFalse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"answers":{"urgency":{"type":"score","score":2}}}`)
	}))
	defer ts.Close()

	res, ok := New(ts.URL, nil).Classify(context.Background(), "boom")
	if ok {
		t.Fatalf("expected ok=false without a category answer, got %+v", res)
	}
}

func TestClassify_Success(t *testing.T) {
	var body map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("invalid request json: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"answers":{
			"category":{"type":"choice","choice":"service_down","answer_confidence":0.91},
			"urgency":{"type":"score","score":2.4,"answer_confidence":0.7},
			"escalate":{"type":"noul","noul":0.8,"answer_confidence":0.8}}}`)
	}))
	defer ts.Close()

	res, ok := New(ts.URL, nil).Classify(context.Background(), "Unit nginx.service is inactive")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if res.Category != "service_down" {
		t.Errorf("category = %q, want service_down", res.Category)
	}
	if res.Urgency != 2 {
		t.Errorf("urgency = %d, want 2", res.Urgency)
	}
	if !res.Escalate {
		t.Error("expected escalate=true for noul 0.8")
	}
	if res.Confidence != 0.91 {
		t.Errorf("confidence = %v, want 0.91", res.Confidence)
	}

	questions, _ := body["questions"].(map[string]any)
	if _, ok := questions[QCategory]; !ok {
		t.Errorf("request is missing the %q question", QCategory)
	}
	if _, ok := questions[QUrgency]; !ok {
		t.Errorf("request is missing the %q question", QUrgency)
	}
	if _, ok := questions[QEscalate]; !ok {
		t.Errorf("request is missing the %q question", QEscalate)
	}
	if body["state"] != "Unit nginx.service is inactive" {
		t.Errorf("state = %v", body["state"])
	}
}

func TestClassify_ScoreClamped(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"answers":{"category":{"type":"choice","choice":"unknown"},"urgency":{"type":"score","score":9.7}}}`)
	}))
	defer ts.Close()

	res, ok := New(ts.URL, nil).Classify(context.Background(), "boom")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if res.Urgency != 3 {
		t.Errorf("urgency = %d, want clamped to 3", res.Urgency)
	}
}

func TestClassify_LongStateTruncated(t *testing.T) {
	var got string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			State string `json:"state"`
		}
		json.Unmarshal(raw, &req)
		got = req.State
		io.WriteString(w, `{"answers":{"category":{"type":"choice","choice":"unknown"}}}`)
	}))
	defer ts.Close()

	// Multi-byte runes must not be cut mid-character.
	long := strings.Repeat("é", 60000)
	if _, ok := New(ts.URL, nil).Classify(context.Background(), long); !ok {
		t.Fatal("expected ok=true")
	}
	if len(got) != 40000 {
		t.Errorf("state length %d, want 40000", len(got))
	}
	if !utf8.ValidString(got) {
		t.Error("state was truncated mid-rune")
	}
}
