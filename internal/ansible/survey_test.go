package ansible

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateSurvey(t *testing.T) {
	good := map[string][]SurveyField{
		"empty": nil,
		"all types": {
			{Key: "name", Type: "text", Required: true},
			{Key: "note", Type: "textarea", Min: new(float64(2)), Max: new(float64(20))},
			{Key: "ratio", Type: "number", Min: new(0.5), Max: new(1.5), Default: 1.0},
			{Key: "replicas", Type: "integer", Min: new(float64(1)), Max: new(float64(9)), Default: 3.0},
			{Key: "env", Type: "select", Options: []string{"dev", "prod"}, Default: "dev"},
			{Key: "zones", Type: "multiselect", Options: []string{"a", "b"}, Default: []any{"a"}},
			{Key: "tls", Type: "boolean", Default: true},
			{Key: "_secret", Type: "password", Min: new(float64(8))},
		},
	}
	for name, fields := range good {
		if err := ValidateSurvey(fields); err != nil {
			t.Errorf("%s: ValidateSurvey = %v, want nil", name, err)
		}
	}

	tooMany := make([]SurveyField, maxSurveyFields+1)
	for i := range tooMany {
		tooMany[i] = SurveyField{Key: "k" + strings.Repeat("x", i), Type: "text"}
	}
	bad := map[string][]SurveyField{
		"too many fields":    tooMany,
		"bad key":            {{Key: "1abc", Type: "text"}},
		"empty key":          {{Key: "", Type: "text"}},
		"dash in key":        {{Key: "a-b", Type: "text"}},
		"key too long":       {{Key: "a" + strings.Repeat("b", 64), Type: "text"}},
		"duplicate key":      {{Key: "x", Type: "text"}, {Key: "x", Type: "boolean"}},
		"unknown type":       {{Key: "x", Type: "radio"}},
		"missing type":       {{Key: "x"}},
		"bad default number": {{Key: "x", Type: "number", Default: "abc"}},
		"fractional int":     {{Key: "x", Type: "integer", Default: 1.5}},
		"int min fractional": {{Key: "x", Type: "integer", Min: new(1.5)}},
		"min above max":      {{Key: "x", Type: "number", Min: new(float64(5)), Max: new(float64(1))}},
		"negative min":       {{Key: "x", Type: "text", Min: new(-1.0)}},
		"fractional chars":   {{Key: "x", Type: "text", Min: new(1.5)}},
		"select no options":  {{Key: "x", Type: "select"}},
		"select bad default": {{Key: "x", Type: "select", Options: []string{"a"}, Default: "b"}},
		"select with min":    {{Key: "x", Type: "select", Options: []string{"a"}, Min: new(float64(1))}},
		"select dupe option": {{Key: "x", Type: "select", Options: []string{"a", "a"}}},
		"multi bad default":  {{Key: "x", Type: "multiselect", Options: []string{"a"}, Default: []any{"b"}}},
		"bool with max":      {{Key: "x", Type: "boolean", Max: new(float64(1))}},
		"bool bad default":   {{Key: "x", Type: "boolean", Default: "yes"}},
		"default above max":  {{Key: "x", Type: "number", Max: new(float64(5)), Default: 9.0}},
		"label too long":     {{Key: "x", Type: "text", Label: strings.Repeat("l", maxSurveyLabelLength+1)}},
	}
	for name, fields := range bad {
		if err := ValidateSurvey(fields); err == nil {
			t.Errorf("%s: ValidateSurvey = nil, want error", name)
		}
	}
}

func TestApplySurvey(t *testing.T) {
	fields := []SurveyField{
		{Key: "host_name", Label: "Host", Type: "text", Required: true, Min: new(float64(3)), Max: new(float64(6))},
		{Key: "replicas", Type: "integer", Min: new(float64(1)), Max: new(float64(5)), Default: 2.0},
		{Key: "ratio", Type: "number", Min: new(0.1), Max: new(float64(1))},
		{Key: "env", Type: "select", Options: []string{"dev", "prod"}, Default: "dev"},
		{Key: "zones", Type: "multiselect", Options: []string{"a", "b", "c"}},
		{Key: "tls", Type: "boolean", Default: false},
		{Key: "token", Type: "password"},
		{Key: "note", Type: "textarea", Max: new(float64(5))},
	}

	t.Run("answers and defaults", func(t *testing.T) {
		out, err := ApplySurvey(fields, map[string]any{
			"host_name": "web-1",
			"ratio":     "0.75", // numeric string from an <input>
			"zones":     []any{"a", "c"},
			"token":     "s3cret",
		})
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]any{
			"host_name": "web-1",
			"replicas":  int64(2),
			"ratio":     0.75,
			"env":       "dev",
			"zones":     []string{"a", "c"},
			"tls":       false,
			"token":     "s3cret",
		}
		if len(out) != len(want) {
			t.Fatalf("got %d vars, want %d: %v", len(out), len(want), out)
		}
		for k, v := range want {
			if got := out[k]; !equalAny(got, v) {
				t.Errorf("%s = %#v (%T), want %#v (%T)", k, got, got, v, v)
			}
		}
	})

	t.Run("required missing", func(t *testing.T) {
		if _, err := ApplySurvey(fields, map[string]any{}); err == nil || !strings.Contains(err.Error(), "required") {
			t.Fatalf("want required error, got %v", err)
		}
	})

	t.Run("required satisfied by default", func(t *testing.T) {
		fs := []SurveyField{{Key: "env", Type: "select", Required: true, Options: []string{"dev"}, Default: "dev"}}
		out, err := ApplySurvey(fs, nil)
		if err != nil || out["env"] != "dev" {
			t.Fatalf("out=%v err=%v", out, err)
		}
	})

	t.Run("unknown key", func(t *testing.T) {
		if _, err := ApplySurvey(fields, map[string]any{"host_name": "web-1", "bogus": 1}); err == nil ||
			!strings.Contains(err.Error(), "unknown survey answer") {
			t.Fatalf("want unknown key error, got %v", err)
		}
	})

	errCases := map[string]map[string]any{
		"integer fraction":     {"host_name": "web-1", "replicas": 2.5},
		"integer bad string":   {"host_name": "web-1", "replicas": "two"},
		"integer above max":    {"host_name": "web-1", "replicas": 9},
		"integer below min":    {"host_name": "web-1", "replicas": 0},
		"number not numeric":   {"host_name": "web-1", "ratio": "abc"},
		"number below min":     {"host_name": "web-1", "ratio": 0.05},
		"number bool":          {"host_name": "web-1", "ratio": true},
		"select not an option": {"host_name": "web-1", "env": "staging"},
		"multiselect not list": {"host_name": "web-1", "zones": "a"},
		"multiselect bad item": {"host_name": "web-1", "zones": []any{"a", "z"}},
		"boolean as string":    {"host_name": "web-1", "tls": "true"},
		"text too short":       {"host_name": "ab"},
		"text too long":        {"host_name": "web-1", "note": "toolong"},
		"text wrong type":      {"host_name": "web-1", "note": []any{"x"}},
	}
	for name, answers := range errCases {
		if _, err := ApplySurvey(fields, answers); err == nil {
			t.Errorf("%s: ApplySurvey = nil, want error", name)
		}
	}

	if _, err := ApplySurvey(nil, map[string]any{"x": 1}); err == nil {
		t.Error("answers to a playbook without a survey must be rejected")
	}
}

func TestHideSurveySecrets(t *testing.T) {
	fields := []SurveyField{{Key: "token", Type: "password"}, {Key: "name", Type: "text"}}
	vars := map[string]any{"token": "s3cret", "name": "web"}
	hidden := HideSurveySecrets(fields, vars)
	if hidden["token"] != "(hidden)" || hidden["name"] != "web" {
		t.Fatalf("hidden=%v", hidden)
	}
	if vars["token"] != "s3cret" {
		t.Fatal("HideSurveySecrets must not mutate its input")
	}
	if got := HideSurveySecrets([]SurveyField{{Key: "name", Type: "text"}}, vars); got["token"] != "s3cret" {
		t.Fatalf("non-password keys must be untouched: %v", got)
	}
}

func TestParseSurvey(t *testing.T) {
	if ParseSurvey("") != nil {
		t.Error("empty survey must parse to nil")
	}
	if ParseSurvey("{not json") != nil {
		t.Error("corrupt survey must parse to nil instead of failing the playbook read")
	}
	raw := `[{"key":"env","label":"Environment","type":"select","options":["dev","prod"],"default":"dev"}]`
	fields := ParseSurvey(raw)
	if len(fields) != 1 || fields[0].Key != "env" || len(fields[0].Options) != 2 {
		t.Fatalf("fields=%+v", fields)
	}
	back, err := json.Marshal(fields)
	if err != nil || !strings.Contains(string(back), `"options":["dev","prod"]`) {
		t.Fatalf("round trip: %s err=%v", back, err)
	}
	if _, err := ValidSurveyJSON(`[{"key":"env","type":"nope"}]`); err == nil {
		t.Error("ValidSurveyJSON must reject an unknown type")
	}
	if fields, err := ValidSurveyJSON(""); err != nil || fields != nil {
		t.Fatalf("empty: fields=%v err=%v", fields, err)
	}
}

func equalAny(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}
