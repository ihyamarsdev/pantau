package ansible

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// SurveyField is one question of a playbook survey. Answers become extra vars, so Key is the variable name.
type SurveyField struct {
	Key         string   `json:"key"` // ^[A-Za-z_][A-Za-z0-9_]{0,63}$ (nama variabel extra var)
	Label       string   `json:"label"`
	Type        string   `json:"type"` // text | textarea | number | integer | select | multiselect | boolean | password
	Description string   `json:"description"`
	Default     any      `json:"default"`
	Required    bool     `json:"required"`
	Options     []string `json:"options"` // select/multiselect, 1..50
	Min         *float64 `json:"min,omitempty"`
	Max         *float64 `json:"max,omitempty"` // number/integer; untuk text/textarea/password = panjang
}

// SurveyTypes is the closed set of field types; anything else is a typo that would silently become a text box.
var SurveyTypes = map[string]bool{
	"text": true, "textarea": true, "number": true, "integer": true,
	"select": true, "multiselect": true, "boolean": true, "password": true,
}

var surveyKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

const (
	maxSurveyFields       = 30
	maxSurveyOptions      = 50
	maxSurveyLabelLength  = 200
	maxSurveyDescLength   = 1000
	maxSurveyTextLength   = 65535 // text/textarea/password length cap, default and answer
	maxSurveyOptionLength = 256
)

// ValidateSurvey checks a survey definition: at most 30 fields, unique keys, and the rules of each type.
func ValidateSurvey(fields []SurveyField) error {
	if len(fields) > maxSurveyFields {
		return fmt.Errorf("survey must not have more than %d fields", maxSurveyFields)
	}
	seen := make(map[string]bool, len(fields))
	for i, f := range fields {
		if !surveyKeyRe.MatchString(f.Key) {
			return fmt.Errorf("field %d: key must match [A-Za-z_][A-Za-z0-9_]{0,63}", i+1)
		}
		if seen[f.Key] {
			return fmt.Errorf("duplicate survey key %q", f.Key)
		}
		seen[f.Key] = true
		if err := validateSurveyField(f); err != nil {
			return fmt.Errorf("field %q: %w", f.Key, err)
		}
	}
	return nil
}

func validateSurveyField(f SurveyField) error {
	if !SurveyTypes[f.Type] {
		return fmt.Errorf("unknown type %q", f.Type)
	}
	if len(f.Label) > maxSurveyLabelLength {
		return fmt.Errorf("label must not exceed %d characters", maxSurveyLabelLength)
	}
	if len(f.Description) > maxSurveyDescLength {
		return fmt.Errorf("description must not exceed %d characters", maxSurveyDescLength)
	}
	if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
		return errors.New("min must not be greater than max")
	}

	switch f.Type {
	case "text", "textarea", "password":
		if err := validateFloatBounds(f, true); err != nil {
			return err
		}
		if s, ok := f.Default.(string); ok && len(s) > maxSurveyTextLength {
			return fmt.Errorf("default must not exceed %d characters", maxSurveyTextLength)
		}
	case "number":
		if err := validateFloatBounds(f, false); err != nil {
			return err
		}
		if f.Default != nil {
			n, ok := surveyNumber(f.Default)
			if !ok {
				return errors.New("default must be a number")
			}
			if err := checkBounds(f, n); err != nil {
				return fmt.Errorf("default %w", err)
			}
		}
	case "integer":
		if err := validateFloatBounds(f, false); err != nil {
			return err
		}
		if f.Min != nil && math.Trunc(*f.Min) != *f.Min {
			return errors.New("min must be a whole number for integer fields")
		}
		if f.Max != nil && math.Trunc(*f.Max) != *f.Max {
			return errors.New("max must be a whole number for integer fields")
		}
		if f.Default != nil {
			n, ok := surveyNumber(f.Default)
			if !ok || math.Trunc(n) != n {
				return errors.New("default must be a whole number")
			}
			if err := checkBounds(f, n); err != nil {
				return fmt.Errorf("default %w", err)
			}
		}
	case "select", "multiselect":
		if f.Min != nil || f.Max != nil {
			return errors.New("min/max are not allowed on select fields")
		}
		if len(f.Options) == 0 || len(f.Options) > maxSurveyOptions {
			return fmt.Errorf("options must have 1-%d entries", maxSurveyOptions)
		}
		opts := make(map[string]bool, len(f.Options))
		for _, o := range f.Options {
			if o == "" || len(o) > maxSurveyOptionLength {
				return fmt.Errorf("options must be 1-%d characters", maxSurveyOptionLength)
			}
			if opts[o] {
				return fmt.Errorf("duplicate option %q", o)
			}
			opts[o] = true
		}
		if f.Type == "select" {
			if f.Default != nil {
				s, ok := f.Default.(string)
				if !ok || !opts[s] {
					return errors.New("default must be one of the options")
				}
			}
			return nil
		}
		if f.Default != nil {
			list, ok := surveyStringList(f.Default)
			if !ok {
				return errors.New("default must be a list of options")
			}
			for _, s := range list {
				if !opts[s] {
					return fmt.Errorf("default option %q is not one of the options", s)
				}
			}
		}
	case "boolean":
		if f.Min != nil || f.Max != nil {
			return errors.New("min/max are not allowed on boolean fields")
		}
		if f.Default != nil {
			if _, ok := f.Default.(bool); !ok {
				return errors.New("default must be true or false")
			}
		}
	}
	return nil
}

// validateFloatBounds rejects fractional bounds where only whole lengths make sense.
func validateFloatBounds(f SurveyField, whole bool) error {
	for _, b := range []*float64{f.Min, f.Max} {
		if b == nil {
			continue
		}
		if *b < 0 {
			return errors.New("min/max must not be negative")
		}
		if whole && math.Trunc(*b) != *b {
			return errors.New("min/max must be whole numbers")
		}
	}
	return nil
}

// surveyNumber accepts JSON numbers and numeric strings (an <input> posts strings).
func surveyNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, !math.IsNaN(n) && !math.IsInf(n, 0)
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
	}
	return 0, false
}

func surveyStringList(v any) ([]string, bool) {
	switch l := v.(type) {
	case []string:
		return l, true
	case []any:
		out := make([]string, 0, len(l))
		for _, e := range l {
			s, ok := e.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	return nil, false
}

// ApplySurvey validates answers against the survey and returns the extra vars object: defaults fill unanswered
// optional fields, an answered password is kept so the runner receives it, and an unknown key is rejected
// (answers land in -e, so a typo would silently do nothing and a stray key could override something else).
func ApplySurvey(fields []SurveyField, answers map[string]any) (map[string]any, error) {
	if err := ValidateSurvey(fields); err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(fields))
	for _, f := range fields {
		known[f.Key] = true
		// A required field with neither an answer nor a default cannot be launched.
		if f.Required && f.Default == nil {
			if _, ok := answers[f.Key]; !ok {
				return nil, fmt.Errorf("%s is required", surveyFieldName(f))
			}
		}
	}
	for k := range answers {
		if !known[k] {
			return nil, fmt.Errorf("unknown survey answer %q", k)
		}
	}

	out := make(map[string]any, len(fields))
	for _, f := range fields {
		raw, answered := answers[f.Key]
		if !answered {
			if f.Default == nil {
				continue
			}
			raw = f.Default
		}
		v, err := surveyValue(f, raw)
		if err != nil {
			return nil, err
		}
		out[f.Key] = v
	}
	return out, nil
}

func surveyFieldName(f SurveyField) string {
	if strings.TrimSpace(f.Label) != "" {
		return fmt.Sprintf("%s (%s)", f.Label, f.Key)
	}
	return f.Key
}

// surveyValue converts one answer to the type the playbook expects.
func surveyValue(f SurveyField, raw any) (any, error) {
	name := surveyFieldName(f)
	switch f.Type {
	case "text", "textarea", "password":
		var s string
		switch v := raw.(type) {
		case string:
			s = v
		case float64, int, int64, json.Number, bool:
			s = fmt.Sprint(v)
		default:
			return nil, fmt.Errorf("%s must be text", name)
		}
		if len(s) > maxSurveyTextLength {
			return nil, fmt.Errorf("%s must not exceed %d characters", name, maxSurveyTextLength)
		}
		if f.Min != nil && float64(len(s)) < *f.Min {
			return nil, fmt.Errorf("%s must be at least %d characters", name, int(*f.Min))
		}
		if f.Max != nil && float64(len(s)) > *f.Max {
			return nil, fmt.Errorf("%s must not exceed %d characters", name, int(*f.Max))
		}
		return s, nil

	case "number", "integer":
		n, ok := surveyNumber(raw)
		if !ok {
			return nil, fmt.Errorf("%s must be a number", name)
		}
		if f.Type == "integer" {
			if math.Trunc(n) != n {
				return nil, fmt.Errorf("%s must be a whole number", name)
			}
			if err := checkBounds(f, n); err != nil {
				return nil, fmt.Errorf("%s %w", name, err)
			}
			return int64(n), nil
		}
		if err := checkBounds(f, n); err != nil {
			return nil, fmt.Errorf("%s %w", name, err)
		}
		return n, nil

	case "select":
		s, ok := raw.(string)
		if !ok || !containsString(f.Options, s) {
			return nil, fmt.Errorf("%s must be one of the options", name)
		}
		return s, nil

	case "multiselect":
		list, ok := surveyStringList(raw)
		if !ok {
			return nil, fmt.Errorf("%s must be a list of options", name)
		}
		for _, s := range list {
			if !containsString(f.Options, s) {
				return nil, fmt.Errorf("%s: %q is not one of the options", name, s)
			}
		}
		if list == nil {
			list = []string{}
		}
		return list, nil

	case "boolean":
		b, ok := raw.(bool)
		if !ok {
			return nil, fmt.Errorf("%s must be true or false", name)
		}
		return b, nil
	}
	return nil, fmt.Errorf("%s has unknown type %q", name, f.Type)
}

func checkBounds(f SurveyField, n float64) error {
	if f.Min != nil && n < *f.Min {
		return fmt.Errorf("must be at least %v", *f.Min)
	}
	if f.Max != nil && n > *f.Max {
		return fmt.Errorf("must not exceed %v", *f.Max)
	}
	return nil
}

func containsString(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

// HideSurveySecrets copies extra vars with every password-typed survey key replaced by "(hidden)". That copy is
// what the run record keeps; the runner still receives the real value.
func HideSurveySecrets(fields []SurveyField, vars map[string]any) map[string]any {
	secret := map[string]bool{}
	for _, f := range fields {
		if f.Type == "password" {
			secret[f.Key] = true
		}
	}
	out := make(map[string]any, len(vars))
	for k, v := range vars {
		if secret[k] {
			v = "(hidden)"
		}
		out[k] = v
	}
	return out
}

// ParseSurvey decodes the stored survey JSON; an empty or invalid value is no survey. It is deliberately
// lenient: a corrupt definition must not make the playbook unreadable.
func ParseSurvey(raw string) []SurveyField {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var fields []SurveyField
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return nil
	}
	return fields
}

// ValidSurveyJSON normalizes and validates a stored/request survey JSON value.
func ValidSurveyJSON(raw string) ([]SurveyField, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var fields []SurveyField
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return nil, errors.New("survey must be a JSON array of fields")
	}
	if err := ValidateSurvey(fields); err != nil {
		return nil, err
	}
	return fields, nil
}
