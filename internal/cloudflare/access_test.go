package cloudflare_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pantau/internal/cloudflare"
)

func TestAccessRuleInputValidate(t *testing.T) {
	tests := []struct {
		name      string
		in        cloudflare.AccessRuleInput
		wantValue string
		wantErr   bool
	}{
		{"ipv4", cloudflare.AccessRuleInput{Mode: "block", Target: "ip", Value: " 203.0.113.7 "}, "203.0.113.7", false},
		{"ipv6", cloudflare.AccessRuleInput{Mode: "challenge", Target: "ip", Value: "2001:db8::1"}, "2001:db8::1", false},
		{"cidr", cloudflare.AccessRuleInput{Mode: "block", Target: "ip_range", Value: "203.0.113.0/24"}, "203.0.113.0/24", false},
		{"asn bare number", cloudflare.AccessRuleInput{Mode: "block", Target: "asn", Value: "13335"}, "AS13335", false},
		{"asn lower case prefix", cloudflare.AccessRuleInput{Mode: "block", Target: "asn", Value: "as13335"}, "AS13335", false},
		{"country upper-cased", cloudflare.AccessRuleInput{Mode: "managed_challenge", Target: "country", Value: "id"}, "ID", false},
		{"whitelist mode", cloudflare.AccessRuleInput{Mode: "whitelist", Target: "ip", Value: "198.51.100.1"}, "198.51.100.1", false},
		{"unknown mode", cloudflare.AccessRuleInput{Mode: "allow", Target: "ip", Value: "198.51.100.1"}, "", true},
		{"unknown target", cloudflare.AccessRuleInput{Mode: "block", Target: "user_agent", Value: "x"}, "", true},
		{"ip with cidr", cloudflare.AccessRuleInput{Mode: "block", Target: "ip", Value: "203.0.113.0/24"}, "", true},
		{"bad cidr", cloudflare.AccessRuleInput{Mode: "block", Target: "ip_range", Value: "203.0.113.0/99"}, "", true},
		{"asn zero", cloudflare.AccessRuleInput{Mode: "block", Target: "asn", Value: "AS0"}, "", true},
		{"asn text", cloudflare.AccessRuleInput{Mode: "block", Target: "asn", Value: "cloudflare"}, "", true},
		{"country three letters", cloudflare.AccessRuleInput{Mode: "block", Target: "country", Value: "IDN"}, "", true},
		{"notes too long", cloudflare.AccessRuleInput{Mode: "block", Target: "ip", Value: "198.51.100.1", Notes: strings.Repeat("n", 201)}, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.in.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() err = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil && tc.in.Value != tc.wantValue {
				t.Fatalf("normalized value = %q, want %q", tc.in.Value, tc.wantValue)
			}
		})
	}
}

func okEnvelope(w http.ResponseWriter, result interface{}, info map[string]interface{}) {
	env := map[string]interface{}{"success": true, "errors": []interface{}{}, "result": result}
	if info != nil {
		env["result_info"] = info
	}
	_ = json.NewEncoder(w).Encode(env)
}

func TestAccessRulesListCreateDelete(t *testing.T) {
	var created map[string]interface{}
	var deleted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "/zones/" + validZoneID + "/firewall/access_rules/rules"
		switch {
		case r.URL.Path == base && r.Method == http.MethodGet:
			okEnvelope(w, []map[string]interface{}{{
				"id": validRecID, "mode": "block", "notes": "scanner", "created_on": "2026-01-01T00:00:00Z",
				"configuration": map[string]interface{}{"target": "ip", "value": "203.0.113.7"},
			}}, map[string]interface{}{"page": 1, "total_pages": 1})
		case r.URL.Path == base && r.Method == http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&created)
			okEnvelope(w, map[string]interface{}{
				"id": validRecID, "mode": created["mode"], "notes": created["notes"],
				"configuration": created["configuration"],
			}, nil)
		case r.URL.Path == base+"/"+validRecID && r.Method == http.MethodDelete:
			deleted = r.URL.Path
			okEnvelope(w, map[string]interface{}{"id": validRecID}, nil)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := cloudflare.New(dummyToken)
	c.BaseURL = srv.URL
	ctx := context.Background()

	rules, err := c.ListAccessRules(ctx, validZoneID)
	if err != nil || len(rules) != 1 {
		t.Fatalf("ListAccessRules = %v, %v", rules, err)
	}
	if r := rules[0]; r.Target != "ip" || r.Value != "203.0.113.7" || r.Mode != "block" || r.Notes != "scanner" {
		t.Fatalf("configuration must be flattened: %+v", r)
	}

	rule, err := c.CreateAccessRule(ctx, validZoneID, cloudflare.AccessRuleInput{Mode: "block", Target: "asn", Value: "13335", Notes: "bad asn"})
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := created["configuration"].(map[string]interface{})
	if cfg["target"] != "asn" || cfg["value"] != "AS13335" || created["mode"] != "block" {
		t.Fatalf("request body = %v", created)
	}
	if rule.Target != "asn" || rule.Value != "AS13335" {
		t.Fatalf("created rule = %+v", rule)
	}

	if err := c.DeleteAccessRule(ctx, validZoneID, validRecID); err != nil || deleted == "" {
		t.Fatalf("DeleteAccessRule: %v (deleted %q)", err, deleted)
	}
}

func TestAccessRulesRejectBadInputBeforeRequest(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer srv.Close()
	c := cloudflare.New(dummyToken)
	c.BaseURL = srv.URL
	ctx := context.Background()

	if _, err := c.ListAccessRules(ctx, invalidIDHex); err == nil {
		t.Error("list: invalid zone id accepted")
	}
	if err := c.DeleteAccessRule(ctx, validZoneID, "../etc"); err == nil {
		t.Error("delete: invalid rule id accepted")
	}
	if _, err := c.CreateAccessRule(ctx, validZoneID, cloudflare.AccessRuleInput{Mode: "block", Target: "ip", Value: "nope"}); err == nil {
		t.Error("create: invalid ip accepted")
	}
	if called {
		t.Fatal("an invalid request reached Cloudflare")
	}
}

func TestListTunnelsFiltersDeletedAndPaginates(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/accounts/"+validAccID+"/cfd_tunnel" {
			http.NotFound(w, r)
			return
		}
		queries = append(queries, r.URL.RawQuery)
		page := r.URL.Query().Get("page")
		name := "tunnel-" + page
		okEnvelope(w, []map[string]interface{}{{"id": "f70ff985-a4ef-4643-bbbc-4a0ed4fc8415", "name": name, "status": "healthy", "tun_type": "cfd_tunnel"}},
			map[string]interface{}{"page": 1, "total_pages": 2})
	}))
	defer srv.Close()
	c := cloudflare.New(dummyToken)
	c.BaseURL = srv.URL

	tunnels, err := c.ListTunnels(context.Background(), validAccID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tunnels) != 2 || tunnels[0].Name != "tunnel-1" || tunnels[1].Name != "tunnel-2" || tunnels[0].Status != "healthy" {
		t.Fatalf("tunnels = %+v", tunnels)
	}
	for _, q := range queries {
		if !strings.Contains(q, "is_deleted=false") {
			t.Fatalf("deleted tunnels must be excluded, query %q", q)
		}
	}
	if _, err := c.ListTunnels(context.Background(), "../x"); err == nil {
		t.Fatal("invalid account id accepted")
	}
}

func TestExportDNSRecords(t *testing.T) {
	const bind = "example.com.\t1\tIN\tA\t1.2.3.4\n"
	fail := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/zones/"+validZoneID+"/dns_records/export" {
			http.NotFound(w, r)
			return
		}
		if fail {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "errors": []map[string]interface{}{{"code": 10000, "message": "Authentication error"}}})
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(bind))
	}))
	defer srv.Close()
	c := cloudflare.New(dummyToken)
	c.BaseURL = srv.URL

	got, err := c.ExportDNSRecords(context.Background(), validZoneID)
	if err != nil || got != bind {
		t.Fatalf("ExportDNSRecords = %q, %v", got, err)
	}

	fail = true
	_, err = c.ExportDNSRecords(context.Background(), validZoneID)
	var apiErr *cloudflare.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden || !strings.Contains(err.Error(), "Authentication error") {
		t.Fatalf("export failure = %v", err)
	}
	if _, err := c.ExportDNSRecords(context.Background(), invalidIDHex); err == nil {
		t.Fatal("invalid zone id accepted")
	}
}

func TestExtendedZoneSettingsAreEditable(t *testing.T) {
	for id, val := range map[string]string{"tls_1_3": "zrt", "http3": "on", "0rtt": "off", "ipv6": "on", "websockets": "on", "brotli": "on", "email_obfuscation": "off", "hotlink_protection": "on", "always_online": "off"} {
		if err := cloudflare.ValidateSetting(id, val); err != nil {
			t.Errorf("ValidateSetting(%s, %s): %v", id, val, err)
		}
	}
	if err := cloudflare.ValidateSetting("http3", "zrt"); err == nil {
		t.Error("zrt is only valid for tls_1_3")
	}
}
