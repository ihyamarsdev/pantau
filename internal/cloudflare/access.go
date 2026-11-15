package cloudflare

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// AccessRule is a zone-level IP Access Rule (firewall/access_rules/rules), flattened from
// {configuration:{target,value}}.
type AccessRule struct {
	ID        string `json:"id"`
	Mode      string `json:"mode"`
	Target    string `json:"target"`
	Value     string `json:"value"`
	Notes     string `json:"notes,omitempty"`
	CreatedOn string `json:"created_on,omitempty"`
}

type AccessRuleInput struct {
	Mode   string `json:"mode"`
	Target string `json:"target"`
	Value  string `json:"value"`
	Notes  string `json:"notes,omitempty"`
}

// AccessRuleModes and AccessRuleTargets are what Pantau lets an operator create.
var (
	AccessRuleModes   = []string{"block", "challenge", "js_challenge", "managed_challenge", "whitelist"}
	AccessRuleTargets = []string{"ip", "ip_range", "asn", "country"}
)

var (
	asnRegex     = regexp.MustCompile(`^(?i:as)?(\d{1,10})$`)
	countryRegex = regexp.MustCompile(`^[A-Za-z]{2}$`)
)

// Validate normalizes the input in place (ASN -> "AS123", country -> upper case, trimmed fields) and checks that
// value matches target: ip = IP address, ip_range = CIDR, asn = AS number, country = ISO 3166-1 alpha-2 code.
func (in *AccessRuleInput) Validate() error {
	in.Mode = strings.TrimSpace(in.Mode)
	in.Target = strings.TrimSpace(in.Target)
	in.Value = strings.TrimSpace(in.Value)
	in.Notes = strings.TrimSpace(in.Notes)

	if !contains(AccessRuleModes, in.Mode) {
		return fmt.Errorf("invalid mode %q", in.Mode)
	}
	if len(in.Notes) > 200 {
		return errors.New("notes exceed 200 characters")
	}

	switch in.Target {
	case "ip":
		if net.ParseIP(in.Value) == nil {
			return fmt.Errorf("invalid IP address %q", in.Value)
		}
	case "ip_range":
		if _, _, err := net.ParseCIDR(in.Value); err != nil {
			return fmt.Errorf("invalid CIDR range %q", in.Value)
		}
	case "asn":
		m := asnRegex.FindStringSubmatch(in.Value)
		if m == nil {
			return fmt.Errorf("invalid ASN %q (use AS13335 or 13335)", in.Value)
		}
		n, err := strconv.ParseUint(m[1], 10, 32)
		if err != nil || n == 0 {
			return fmt.Errorf("invalid ASN %q", in.Value)
		}
		in.Value = "AS" + strconv.FormatUint(n, 10)
	case "country":
		if !countryRegex.MatchString(in.Value) {
			return fmt.Errorf("invalid country code %q (two letters, e.g. ID)", in.Value)
		}
		in.Value = strings.ToUpper(in.Value)
	default:
		return fmt.Errorf("invalid target %q", in.Target)
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

type accessRuleRaw struct {
	ID            string `json:"id"`
	Mode          string `json:"mode"`
	Notes         string `json:"notes"`
	CreatedOn     string `json:"created_on"`
	Configuration struct {
		Target string `json:"target"`
		Value  string `json:"value"`
	} `json:"configuration"`
}

func (r *accessRuleRaw) toRule() AccessRule {
	return AccessRule{
		ID:        r.ID,
		Mode:      r.Mode,
		Target:    r.Configuration.Target,
		Value:     r.Configuration.Value,
		Notes:     r.Notes,
		CreatedOn: r.CreatedOn,
	}
}

// ListAccessRules lists the zone's IP Access Rules. Pagination: per_page=100, max 20 pages.
func (c *Client) ListAccessRules(ctx context.Context, zoneID string) ([]AccessRule, error) {
	if !ValidID(zoneID) {
		return nil, errors.New("invalid zone id")
	}

	const perPage = 100
	const maxPages = 20

	var all []AccessRule
	for page := 1; page <= maxPages; page++ {
		q := make(url.Values)
		q.Set("per_page", strconv.Itoa(perPage))
		q.Set("page", strconv.Itoa(page))

		var raws []accessRuleRaw
		info, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/zones/%s/firewall/access_rules/rules", zoneID), q, nil, &raws)
		if err != nil {
			return nil, err
		}
		for i := range raws {
			all = append(all, raws[i].toRule())
		}

		if len(raws) == 0 {
			break
		}
		if info != nil && info.TotalPages > 0 && page >= info.TotalPages {
			break
		}
		if len(raws) < perPage && (info == nil || info.TotalPages == 0) {
			break
		}
	}
	return all, nil
}

type accessRuleBody struct {
	Mode          string            `json:"mode"`
	Notes         string            `json:"notes,omitempty"`
	Configuration map[string]string `json:"configuration"`
}

func (c *Client) CreateAccessRule(ctx context.Context, zoneID string, in AccessRuleInput) (*AccessRule, error) {
	if !ValidID(zoneID) {
		return nil, errors.New("invalid zone id")
	}
	if err := in.Validate(); err != nil {
		return nil, err
	}

	body := accessRuleBody{
		Mode:          in.Mode,
		Notes:         in.Notes,
		Configuration: map[string]string{"target": in.Target, "value": in.Value},
	}
	var raw accessRuleRaw
	if _, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/zones/%s/firewall/access_rules/rules", zoneID), nil, body, &raw); err != nil {
		return nil, err
	}
	rule := raw.toRule()
	return &rule, nil
}

func (c *Client) DeleteAccessRule(ctx context.Context, zoneID, ruleID string) error {
	if !ValidID(zoneID) {
		return errors.New("invalid zone id")
	}
	if !ValidID(ruleID) {
		return errors.New("invalid rule id")
	}
	_, err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/zones/%s/firewall/access_rules/rules/%s", zoneID, ruleID), nil, nil, nil)
	return err
}
