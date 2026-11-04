package cloudflare

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
)

type Setting struct {
	ID       string      `json:"id"`
	Value    interface{} `json:"value"`
	Editable bool        `json:"editable"`
}

// EditableSettings defines the allowlisted settings that can be modified via Pantau.
// ssl{off,flexible,full,strict}, always_use_https{on,off},
// security_level{off,essentially_off,low,medium,high,under_attack}, development_mode{on,off},
// min_tls_version{1.0,1.1,1.2,1.3}, automatic_https_rewrites{on,off}
var EditableSettings = map[string][]string{
	"ssl":                      {"off", "flexible", "full", "strict"},
	"always_use_https":         {"on", "off"},
	"security_level":           {"off", "essentially_off", "low", "medium", "high", "under_attack"},
	"development_mode":         {"on", "off"},
	"min_tls_version":          {"1.0", "1.1", "1.2", "1.3"},
	"automatic_https_rewrites": {"on", "off"},
}

// ValidateSetting validates id and value against EditableSettings allowlist.
func ValidateSetting(id, value string) error {
	allowedVals, ok := EditableSettings[id]
	if !ok {
		return fmt.Errorf("unknown or non-editable setting: %q", id)
	}

	for _, v := range allowedVals {
		if v == value {
			return nil
		}
	}

	return fmt.Errorf("invalid value %q for setting %q", value, id)
}

// GetZoneSettings returns ONLY settings present in EditableSettings (stable order sorted by id).
func (c *Client) GetZoneSettings(ctx context.Context, zoneID string) ([]Setting, error) {
	if !ValidID(zoneID) {
		return nil, errors.New("invalid zone id")
	}

	var rawSettings []Setting
	if _, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/zones/%s/settings", zoneID), nil, nil, &rawSettings); err != nil {
		return nil, err
	}

	var filtered []Setting
	for _, s := range rawSettings {
		if _, ok := EditableSettings[s.ID]; ok {
			filtered = append(filtered, s)
		}
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].ID < filtered[j].ID
	})

	return filtered, nil
}

type setSettingPayload struct {
	Value string `json:"value"`
}

// SetZoneSetting validates the id and value, then PATCHes /zones/{id}/settings/{id} {"value":...}.
func (c *Client) SetZoneSetting(ctx context.Context, zoneID, id, value string) (*Setting, error) {
	if !ValidID(zoneID) {
		return nil, errors.New("invalid zone id")
	}
	if err := ValidateSetting(id, value); err != nil {
		return nil, err
	}

	payload := setSettingPayload{Value: value}
	var res Setting
	if _, err := c.do(ctx, http.MethodPatch, fmt.Sprintf("/zones/%s/settings/%s", zoneID, id), nil, payload, &res); err != nil {
		return nil, err
	}

	return &res, nil
}
