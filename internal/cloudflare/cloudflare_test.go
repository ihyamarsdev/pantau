package cloudflare_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pantau/internal/cloudflare"
)

const (
	dummyToken   = "secret-cf-token-12345"
	validZoneID  = "023e105f4ecef8ad9ca31a8372d0c353"
	validRecID   = "01a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6"
	validAccID   = "1234567890abcdef1234567890abcdef"
	invalidIDHex = "not-a-valid-id"
)

func TestValidID(t *testing.T) {
	tests := []struct {
		id    string
		valid bool
	}{
		{validZoneID, true},
		{validRecID, true},
		{"0123456789abcdef0123456789abcdef", true},
		{"0123456789ABCDEF0123456789ABCDEF", false}, // uppercase rejected
		{"../", false},
		{"0123456789abcdef", false},                  // too short (16)
		{"0123456789abcdef0123456789abcdef0", false}, // too long (33)
		{"", false},
		{" 023e105f4ecef8ad9ca31a8372d0c353", false},
		{"023e105f4ecef8ad9ca31a8372d0c35g", false}, // 'g' non-hex
	}

	for _, tc := range tests {
		if got := cloudflare.ValidID(tc.id); got != tc.valid {
			t.Errorf("ValidID(%q) = %v; want %v", tc.id, got, tc.valid)
		}
	}
}

func TestClientRefusesInvalidIDsBeforeRequest(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := cloudflare.New(dummyToken)
	client.BaseURL = srv.URL

	ctx := context.Background()

	// VerifyToken invalid account ID
	if _, err := client.VerifyToken(ctx, "../"); err == nil {
		t.Error("expected error for invalid account id in VerifyToken")
	}
	// ListZones invalid account ID
	if _, err := client.ListZones(ctx, "INVALID_UPPERCASE"); err == nil {
		t.Error("expected error for invalid account id in ListZones")
	}
	// GetZone invalid zone ID
	if _, err := client.GetZone(ctx, "short"); err == nil {
		t.Error("expected error for invalid zone id in GetZone")
	}
	// ListDNSRecords invalid zone ID
	if _, err := client.ListDNSRecords(ctx, "../path"); err == nil {
		t.Error("expected error for invalid zone id in ListDNSRecords")
	}
	// CreateDNSRecord invalid zone ID
	if _, err := client.CreateDNSRecord(ctx, "bad", cloudflare.DNSRecordInput{}); err == nil {
		t.Error("expected error for invalid zone id in CreateDNSRecord")
	}
	// UpdateDNSRecord invalid IDs
	if _, err := client.UpdateDNSRecord(ctx, "bad", validRecID, cloudflare.DNSRecordInput{}); err == nil {
		t.Error("expected error for invalid zone id in UpdateDNSRecord")
	}
	if _, err := client.UpdateDNSRecord(ctx, validZoneID, "bad", cloudflare.DNSRecordInput{}); err == nil {
		t.Error("expected error for invalid rec id in UpdateDNSRecord")
	}
	// DeleteDNSRecord invalid IDs
	if err := client.DeleteDNSRecord(ctx, "bad", validRecID); err == nil {
		t.Error("expected error for invalid zone id in DeleteDNSRecord")
	}
	if err := client.DeleteDNSRecord(ctx, validZoneID, "bad"); err == nil {
		t.Error("expected error for invalid rec id in DeleteDNSRecord")
	}
	// PurgeCache invalid zone ID
	if err := client.PurgeCache(ctx, "bad", cloudflare.PurgeRequest{Everything: true}); err == nil {
		t.Error("expected error for invalid zone id in PurgeCache")
	}
	// GetZoneSettings invalid zone ID
	if _, err := client.GetZoneSettings(ctx, "bad"); err == nil {
		t.Error("expected error for invalid zone id in GetZoneSettings")
	}
	// SetZoneSetting invalid zone ID
	if _, err := client.SetZoneSetting(ctx, "bad", "ssl", "full"); err == nil {
		t.Error("expected error for invalid zone id in SetZoneSetting")
	}

	if called {
		t.Error("server was called despite invalid IDs")
	}
}

func TestBearerHeaderSent(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"result": map[string]interface{}{
				"id":     "abc",
				"status": "active",
			},
		})
	}))
	defer srv.Close()

	client := cloudflare.New(dummyToken)
	client.BaseURL = srv.URL

	info, err := client.VerifyToken(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer "+dummyToken {
		t.Errorf("got Authorization %q; want %q", gotAuth, "Bearer "+dummyToken)
	}
	if info.ID != "abc" || info.Status != "active" {
		t.Errorf("unexpected token info: %+v", info)
	}
}

func TestVerifyTokenEndpointSelection(t *testing.T) {
	var requestedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"result": map[string]interface{}{
				"id":     "tok1",
				"status": "active",
			},
		})
	}))
	defer srv.Close()

	client := cloudflare.New(dummyToken)
	client.BaseURL = srv.URL

	// 1. empty account ID -> /user/tokens/verify
	if _, err := client.VerifyToken(context.Background(), ""); err != nil {
		t.Fatalf("VerifyToken err: %v", err)
	}
	if requestedPath != "/user/tokens/verify" {
		t.Errorf("path was %s; want /user/tokens/verify", requestedPath)
	}

	// 2. account ID != "" -> /accounts/{id}/tokens/verify
	if _, err := client.VerifyToken(context.Background(), validAccID); err != nil {
		t.Fatalf("VerifyToken err: %v", err)
	}
	expectedPath := "/accounts/" + validAccID + "/tokens/verify"
	if requestedPath != expectedPath {
		t.Errorf("path was %s; want %s", requestedPath, expectedPath)
	}
}

func TestEnvelopeSuccessFalseReturnsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"errors": []map[string]interface{}{
				{"code": 1001, "message": "Invalid token format"},
				{"code": 1002, "message": "Token expired"},
			},
		})
	}))
	defer srv.Close()

	client := cloudflare.New(dummyToken)
	client.BaseURL = srv.URL

	_, err := client.VerifyToken(context.Background(), "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	apiErr, ok := err.(*cloudflare.APIError)
	if !ok {
		t.Fatalf("expected *cloudflare.APIError, got %T: %v", err, err)
	}

	if apiErr.Status != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", apiErr.Status)
	}

	errMsg := apiErr.Error()
	if !strings.Contains(errMsg, "1001: Invalid token format") || !strings.Contains(errMsg, "1002: Token expired") {
		t.Errorf("unexpected error message: %s", errMsg)
	}

	if strings.Contains(errMsg, dummyToken) {
		t.Errorf("token leaked in error string: %s", errMsg)
	}
}

func TestNonJSONAnd5xxReturnsAPIErrorWithStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html><body>502 Bad Gateway</body></html>"))
	}))
	defer srv.Close()

	client := cloudflare.New(dummyToken)
	client.BaseURL = srv.URL

	_, err := client.GetZone(context.Background(), validZoneID)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	apiErr, ok := err.(*cloudflare.APIError)
	if !ok {
		t.Fatalf("expected *cloudflare.APIError, got %T: %v", err, err)
	}

	if apiErr.Status != http.StatusBadGateway {
		t.Errorf("expected status 502, got %d", apiErr.Status)
	}

	if apiErr.Error() != "cloudflare: HTTP 502" {
		t.Errorf("expected 'cloudflare: HTTP 502', got %q", apiErr.Error())
	}
}

func TestTokenNeverAppearsInErrors(t *testing.T) {
	// 1. HTTP error with non-JSON
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer srv.Close()

	client := cloudflare.New(dummyToken)
	client.BaseURL = srv.URL

	_, err := client.VerifyToken(context.Background(), "")
	if err != nil && strings.Contains(err.Error(), dummyToken) {
		t.Errorf("token leaked in error: %v", err)
	}

	// 2. CF error response
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"errors": []map[string]interface{}{
				{"code": 1000, "message": "Invalid access token"},
			},
		})
	}))
	defer srv2.Close()

	client2 := cloudflare.New(dummyToken)
	client2.BaseURL = srv2.URL

	_, err = client2.VerifyToken(context.Background(), "")
	if err != nil && strings.Contains(err.Error(), dummyToken) {
		t.Errorf("token leaked in error: %v", err)
	}
}

func TestListZonesPagination(t *testing.T) {
	var requestedPages []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		var p int
		_, _ = fmt.Sscanf(page, "%d", &p)
		requestedPages = append(requestedPages, p)

		w.Header().Set("Content-Type", "application/json")

		var zones []map[string]interface{}
		for i := range 50 {
			zones = append(zones, map[string]interface{}{
				"id":      fmt.Sprintf("%032x", (p-1)*50+i+1),
				"name":    fmt.Sprintf("zone-%d-%d.com", p, i),
				"status":  "active",
				"plan":    map[string]interface{}{"name": "Free Plan"},
				"account": map[string]interface{}{"name": "My Account"},
			})
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"result":  zones,
			"result_info": map[string]interface{}{
				"page":        p,
				"per_page":    50,
				"total_pages": 3,
				"total_count": 150,
			},
		})
	}))
	defer srv.Close()

	client := cloudflare.New(dummyToken)
	client.BaseURL = srv.URL

	zones, err := client.ListZones(context.Background(), "")
	if err != nil {
		t.Fatalf("ListZones err: %v", err)
	}

	if len(zones) != 150 {
		t.Errorf("expected 150 zones, got %d", len(zones))
	}

	if len(requestedPages) != 3 {
		t.Errorf("expected 3 pages requested, got %v", requestedPages)
	}

	if zones[0].Plan != "Free Plan" || zones[0].AccountName != "My Account" {
		t.Errorf("nested plan or account fields not mapped properly: %+v", zones[0])
	}
}

func TestListDNSRecordsPagination(t *testing.T) {
	var requestedPages []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		var p int
		_, _ = fmt.Sscanf(page, "%d", &p)
		requestedPages = append(requestedPages, p)

		w.Header().Set("Content-Type", "application/json")

		var records []map[string]interface{}
		for i := range 100 {
			records = append(records, map[string]interface{}{
				"id":      fmt.Sprintf("%032x", (p-1)*100+i+1),
				"type":    "A",
				"name":    fmt.Sprintf("sub%d.example.com", (p-1)*100+i),
				"content": "1.2.3.4",
				"ttl":     1,
			})
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"result":  records,
			"result_info": map[string]interface{}{
				"page":        p,
				"per_page":    100,
				"total_pages": 2,
				"total_count": 200,
			},
		})
	}))
	defer srv.Close()

	client := cloudflare.New(dummyToken)
	client.BaseURL = srv.URL

	records, err := client.ListDNSRecords(context.Background(), validZoneID)
	if err != nil {
		t.Fatalf("ListDNSRecords err: %v", err)
	}

	if len(records) != 200 {
		t.Errorf("expected 200 records, got %d", len(records))
	}

	if len(requestedPages) != 2 {
		t.Errorf("expected 2 pages requested, got %v", requestedPages)
	}
}

func TestDNSRecordInputValidateTable(t *testing.T) {
	priority := 10
	tests := []struct {
		name    string
		input   cloudflare.DNSRecordInput
		wantErr bool
	}{
		{
			name: "valid A record",
			input: cloudflare.DNSRecordInput{
				Type:    "A",
				Name:    "example.com",
				Content: "1.2.3.4",
				TTL:     1,
			},
			wantErr: false,
		},
		{
			name: "valid TTL 0 defaults to 1",
			input: cloudflare.DNSRecordInput{
				Type:    "A",
				Name:    "example.com",
				Content: "1.2.3.4",
				TTL:     0,
			},
			wantErr: false,
		},
		{
			name: "valid MX with priority",
			input: cloudflare.DNSRecordInput{
				Type:     "MX",
				Name:     "example.com",
				Content:  "mail.example.com",
				TTL:      300,
				Priority: &priority,
			},
			wantErr: false,
		},
		{
			name: "invalid MX missing priority",
			input: cloudflare.DNSRecordInput{
				Type:    "MX",
				Name:    "example.com",
				Content: "mail.example.com",
				TTL:     300,
			},
			wantErr: true,
		},
		{
			name: "bad type",
			input: cloudflare.DNSRecordInput{
				Type:    "UNKNOWN",
				Name:    "example.com",
				Content: "1.2.3.4",
				TTL:     1,
			},
			wantErr: true,
		},
		{
			name: "empty name",
			input: cloudflare.DNSRecordInput{
				Type:    "A",
				Name:    "",
				Content: "1.2.3.4",
				TTL:     1,
			},
			wantErr: true,
		},
		{
			name: "name too long",
			input: cloudflare.DNSRecordInput{
				Type:    "A",
				Name:    strings.Repeat("a", 256),
				Content: "1.2.3.4",
				TTL:     1,
			},
			wantErr: true,
		},
		{
			name: "empty content",
			input: cloudflare.DNSRecordInput{
				Type:    "A",
				Name:    "example.com",
				Content: "",
				TTL:     1,
			},
			wantErr: true,
		},
		{
			name: "content too long",
			input: cloudflare.DNSRecordInput{
				Type:    "TXT",
				Name:    "example.com",
				Content: strings.Repeat("a", 2049),
				TTL:     1,
			},
			wantErr: true,
		},
		{
			name: "ttl below 60",
			input: cloudflare.DNSRecordInput{
				Type:    "A",
				Name:    "example.com",
				Content: "1.2.3.4",
				TTL:     59,
			},
			wantErr: true,
		},
		{
			name: "ttl above 86400",
			input: cloudflare.DNSRecordInput{
				Type:    "A",
				Name:    "example.com",
				Content: "1.2.3.4",
				TTL:     86401,
			},
			wantErr: true,
		},
		{
			name: "valid TTL boundaries",
			input: cloudflare.DNSRecordInput{
				Type:    "A",
				Name:    "example.com",
				Content: "1.2.3.4",
				TTL:     86400,
			},
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.input
			err := in.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() err = %v; wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestPurgeRequestValidate(t *testing.T) {
	// Exactly one mode
	tests := []struct {
		name    string
		req     cloudflare.PurgeRequest
		wantErr bool
	}{
		{
			name:    "none specified",
			req:     cloudflare.PurgeRequest{},
			wantErr: true,
		},
		{
			name: "everything only",
			req: cloudflare.PurgeRequest{
				Everything: true,
			},
			wantErr: false,
		},
		{
			name: "files only <= 30",
			req: cloudflare.PurgeRequest{
				Files: []string{"https://example.com/style.css"},
			},
			wantErr: false,
		},
		{
			name: "multiple modes: everything + files",
			req: cloudflare.PurgeRequest{
				Everything: true,
				Files:      []string{"https://example.com/style.css"},
			},
			wantErr: true,
		},
		{
			name: "multiple modes: files + hosts",
			req: cloudflare.PurgeRequest{
				Files: []string{"https://example.com/style.css"},
				Hosts: []string{"example.com"},
			},
			wantErr: true,
		},
		{
			name: "files > 30",
			req: func() cloudflare.PurgeRequest {
				files := make([]string, 31)
				for i := range files {
					files[i] = fmt.Sprintf("https://example.com/%d.js", i)
				}
				return cloudflare.PurgeRequest{Files: files}
			}(),
			wantErr: true,
		},
		{
			name: "files exactly 30",
			req: func() cloudflare.PurgeRequest {
				files := make([]string, 30)
				for i := range files {
					files[i] = fmt.Sprintf("https://example.com/%d.js", i)
				}
				return cloudflare.PurgeRequest{Files: files}
			}(),
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestValidateSetting(t *testing.T) {
	tests := []struct {
		id      string
		val     string
		wantErr bool
	}{
		{"ssl", "off", false},
		{"ssl", "flexible", false},
		{"ssl", "full", false},
		{"ssl", "strict", false},
		{"ssl", "invalid", true},
		{"always_use_https", "on", false},
		{"always_use_https", "off", false},
		{"always_use_https", "true", true},
		{"security_level", "under_attack", false},
		{"security_level", "high", false},
		{"security_level", "extreme", true},
		{"development_mode", "on", false},
		{"development_mode", "off", false},
		{"min_tls_version", "1.2", false},
		{"min_tls_version", "1.4", true},
		{"automatic_https_rewrites", "on", false},
		{"automatic_https_rewrites", "off", false},
		{"unknown_setting", "on", true},
	}

	for _, tc := range tests {
		t.Run(tc.id+"="+tc.val, func(t *testing.T) {
			err := cloudflare.ValidateSetting(tc.id, tc.val)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateSetting(%q, %q) err = %v; wantErr = %v", tc.id, tc.val, err, tc.wantErr)
			}
		})
	}
}

func TestGetZoneSettingsReturnsOnlyAllowlistedSorted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"result": []map[string]interface{}{
				{"id": "waf", "value": "on", "editable": true}, // not allowlisted
				{"id": "ssl", "value": "full", "editable": true},
				{"id": "browser_cache_ttl", "value": 14400, "editable": true}, // not allowlisted
				{"id": "always_use_https", "value": "on", "editable": true},
				{"id": "development_mode", "value": "off", "editable": true},
			},
		})
	}))
	defer srv.Close()

	client := cloudflare.New(dummyToken)
	client.BaseURL = srv.URL

	settings, err := client.GetZoneSettings(context.Background(), validZoneID)
	if err != nil {
		t.Fatalf("GetZoneSettings err: %v", err)
	}

	if len(settings) != 3 {
		t.Fatalf("expected 3 settings, got %d", len(settings))
	}

	// Should be sorted by ID: always_use_https, development_mode, ssl
	expectedIDs := []string{"always_use_https", "development_mode", "ssl"}
	for i, exp := range expectedIDs {
		if settings[i].ID != exp {
			t.Errorf("settings[%d].ID = %s; want %s", i, settings[i].ID, exp)
		}
	}
}

func TestDNSCRUDOperations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"result": map[string]interface{}{
					"id":      validRecID,
					"type":    "A",
					"name":    "example.com",
					"content": "1.2.3.4",
					"ttl":     1,
				},
			})
		case http.MethodPut:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"result": map[string]interface{}{
					"id":      validRecID,
					"type":    "A",
					"name":    "example.com",
					"content": "5.6.7.8",
					"ttl":     1,
				},
			})
		case http.MethodDelete:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"result":  map[string]interface{}{"id": validRecID},
			})
		}
	}))
	defer srv.Close()

	client := cloudflare.New(dummyToken)
	client.BaseURL = srv.URL
	ctx := context.Background()

	// Create
	rec, err := client.CreateDNSRecord(ctx, validZoneID, cloudflare.DNSRecordInput{
		Type:    "A",
		Name:    "example.com",
		Content: "1.2.3.4",
		TTL:     1,
	})
	if err != nil {
		t.Fatalf("CreateDNSRecord err: %v", err)
	}
	if rec.ID != validRecID || rec.Content != "1.2.3.4" {
		t.Errorf("unexpected created record: %+v", rec)
	}

	// Update
	rec, err = client.UpdateDNSRecord(ctx, validZoneID, validRecID, cloudflare.DNSRecordInput{
		Type:    "A",
		Name:    "example.com",
		Content: "5.6.7.8",
		TTL:     1,
	})
	if err != nil {
		t.Fatalf("UpdateDNSRecord err: %v", err)
	}
	if rec.Content != "5.6.7.8" {
		t.Errorf("unexpected updated record: %+v", rec)
	}

	// Delete
	if err := client.DeleteDNSRecord(ctx, validZoneID, validRecID); err != nil {
		t.Fatalf("DeleteDNSRecord err: %v", err)
	}
}

func TestSetZoneSettingAndPurgeCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "purge_cache") {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"result":  map[string]interface{}{"id": validZoneID},
			})
			return
		}
		if strings.Contains(r.URL.Path, "settings/ssl") {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"result": map[string]interface{}{
					"id":       "ssl",
					"value":    "strict",
					"editable": true,
				},
			})
			return
		}
	}))
	defer srv.Close()

	client := cloudflare.New(dummyToken)
	client.BaseURL = srv.URL
	ctx := context.Background()

	// PurgeCache
	err := client.PurgeCache(ctx, validZoneID, cloudflare.PurgeRequest{Everything: true})
	if err != nil {
		t.Fatalf("PurgeCache err: %v", err)
	}

	// SetZoneSetting
	s, err := client.SetZoneSetting(ctx, validZoneID, "ssl", "strict")
	if err != nil {
		t.Fatalf("SetZoneSetting err: %v", err)
	}
	if s.Value != "strict" {
		t.Errorf("unexpected setting value: %+v", s)
	}
}
