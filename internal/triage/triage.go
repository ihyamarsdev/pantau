// Package triage classifies a free-text incident excerpt with an optional
// Laya decision engine (https://github.com/NandhaKishorM/laya) served as a
// sidecar over HTTP.
//
// Laya only suggests labels: a category, an urgency level and whether a human
// is needed. It never replaces a Desired State rule, and a failure here never
// fails an inspection or suppresses an alert.
package triage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// Question ids sent to Laya.
const (
	QCategory = "category"
	QUrgency  = "urgency"
	QEscalate = "escalate"
)

// DefaultTimeout bounds one classification request. A warm laya-serve on CPU
// answers in roughly 10 seconds, so the default leaves room for it.
const DefaultTimeout = 15000 * time.Millisecond

// escalateThreshold turns the noul probability into a decision.
const escalateThreshold = 0.5

// maxStateBytes caps the excerpt sent to Laya.
const maxStateBytes = 40000

// Result is one classification of an incident excerpt.
type Result struct {
	Category   string  `json:"category"`
	Urgency    int     `json:"urgency"`
	Escalate   bool    `json:"escalate"`
	Confidence float64 `json:"confidence"`
}

// questions is the question set sent with every request. Labels are semantic,
// never boolean words: Laya's choice head follows the label wording, so a
// "true"/"false" pair would answer the label instead of the excerpt.
var questions = map[string]any{
	QCategory: map[string]any{
		"type":         "choice",
		"instructions": "Which incident category best describes this infrastructure drift alert?",
		"criteria": map[string]string{
			"service_down":         "a service or container is stopped, failed or missing",
			"resource_exhaustion":  "disk, memory, CPU or inode pressure",
			"network_connectivity": "a host cannot reach a peer, a name server or a port",
			"security_exposure":    "a vulnerability or a weak configuration is exposed",
			"package_drift":        "a package version differs from the desired one",
			"backup_failure":       "a backup, verification or checksum step failed",
			"unknown":              "none of the other categories fit",
		},
	},
	QUrgency: map[string]any{
		"type":         "score",
		"instructions": "How urgent is this incident?",
		"criteria":     []string{"low", "medium", "high", "critical"},
	},
	QEscalate: map[string]any{
		"type":         "noul",
		"instructions": "Does this incident need a human to act before the next scheduled inspection?",
	},
}

// Client talks to a laya-serve instance.
type Client struct {
	// BaseURL is the laya-serve root, e.g. http://127.0.0.1:8000.
	BaseURL string
	// HTTP is the shared client; the request deadline comes from the context.
	HTTP *http.Client
}

// New returns a client for baseURL. httpClient may be nil.
func New(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: httpClient}
}

type request struct {
	State     string         `json:"state"`
	Questions map[string]any `json:"questions"`
	Model     string         `json:"model,omitempty"`
}

type answer struct {
	Type             string  `json:"type"`
	Choice           string  `json:"choice"`
	Score            float64 `json:"score"`
	Noul             float64 `json:"noul"`
	AnswerConfidence float64 `json:"answer_confidence"`
}

type response struct {
	Answers map[string]answer `json:"answers"`
}

// Classify sends state to Laya and returns the suggested labels.
// The second result is false when Laya is disabled, unreachable, slow, or
// answers with something Pantau cannot use; the caller then keeps its own
// path.
func (c *Client) Classify(ctx context.Context, state string) (Result, bool) {
	if c == nil || c.BaseURL == "" {
		return Result{}, false
	}
	if len(state) > maxStateBytes {
		cut := maxStateBytes
		for cut > 0 && !utf8.RuneStart(state[cut]) {
			cut--
		}
		state = state[:cut]
	}
	body, err := json.Marshal(request{
		State:     state,
		Questions: questions,
		Model:     "english",
	})
	if err != nil {
		return Result{}, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/systemone", bytes.NewReader(body))
	if err != nil {
		return Result{}, false
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Result{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return Result{}, false
	}
	var out response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Result{}, false
	}

	cat, ok := out.Answers[QCategory]
	if !ok || cat.Type != "choice" || strings.TrimSpace(cat.Choice) == "" {
		return Result{}, false
	}
	res := Result{
		Category:   cat.Choice,
		Confidence: cat.AnswerConfidence,
	}
	if urg, ok := out.Answers[QUrgency]; ok && urg.Type == "score" {
		level := int(urg.Score + 0.5)
		if level < 0 {
			level = 0
		}
		if level > 3 {
			level = 3
		}
		res.Urgency = level
	}
	if esc, ok := out.Answers[QEscalate]; ok && esc.Type == "noul" {
		res.Escalate = esc.Noul >= escalateThreshold
	}
	return res, true
}

// String renders a result for a notification body.
func (r Result) String() string {
	return fmt.Sprintf("%s (urgency %d/3)", r.Category, r.Urgency)
}
