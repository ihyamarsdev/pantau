package cloudflare

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type DNSRecord struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Name       string `json:"name"`
	Content    string `json:"content"`
	TTL        int    `json:"ttl"` // 1 = auto
	Proxied    *bool  `json:"proxied,omitempty"`
	Proxiable  bool   `json:"proxiable"`
	Priority   *int   `json:"priority,omitempty"`
	Comment    string `json:"comment,omitempty"`
	ModifiedOn string `json:"modified_on,omitempty"`
}

type DNSRecordInput struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl"` // 1 = auto
	Proxied  *bool  `json:"proxied,omitempty"`
	Priority *int   `json:"priority,omitempty"`
	Comment  string `json:"comment,omitempty"`
}

var validRecordTypes = map[string]bool{
	"A":     true,
	"AAAA":  true,
	"CNAME": true,
	"TXT":   true,
	"MX":    true,
	"NS":    true,
	"SRV":   true,
	"CAA":   true,
}

// Validate checks DNSRecordInput according to spec:
// type in {A,AAAA,CNAME,TXT,MX,NS,SRV,CAA}; name & content required, <= 255 / 2048;
// TTL 1 or 60..86400 (ttl 0 -> considered 1); priority required for MX.
func (in *DNSRecordInput) Validate() error {
	inType := strings.ToUpper(strings.TrimSpace(in.Type))
	if !validRecordTypes[inType] {
		return fmt.Errorf("invalid DNS record type: %q", in.Type)
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		return errors.New("record name is required")
	}
	if len(name) > 255 {
		return errors.New("record name exceeds 255 characters")
	}

	content := strings.TrimSpace(in.Content)
	if content == "" {
		return errors.New("record content is required")
	}
	if len(content) > 2048 {
		return errors.New("record content exceeds 2048 characters")
	}

	if in.TTL == 0 {
		in.TTL = 1
	} else if in.TTL != 1 && (in.TTL < 60 || in.TTL > 86400) {
		return errors.New("TTL must be 1 (auto) or between 60 and 86400")
	}

	if inType == "MX" && in.Priority == nil {
		return errors.New("priority is required for MX records")
	}

	return nil
}

// ListDNSRecords lists records for a zone. Pagination: per_page=100, max 50 pages.
func (c *Client) ListDNSRecords(ctx context.Context, zoneID string) ([]DNSRecord, error) {
	if !ValidID(zoneID) {
		return nil, errors.New("invalid zone id")
	}

	const perPage = 100
	const maxPages = 50

	var all []DNSRecord
	for page := 1; page <= maxPages; page++ {
		q := make(url.Values)
		q.Set("per_page", strconv.Itoa(perPage))
		q.Set("page", strconv.Itoa(page))

		var records []DNSRecord
		info, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/zones/%s/dns_records", zoneID), q, nil, &records)
		if err != nil {
			return nil, err
		}

		all = append(all, records...)

		if len(records) == 0 {
			break
		}
		if info != nil && info.TotalPages > 0 && page >= info.TotalPages {
			break
		}
		if len(records) < perPage && (info == nil || info.TotalPages == 0) {
			break
		}
	}

	return all, nil
}

func (c *Client) CreateDNSRecord(ctx context.Context, zoneID string, in DNSRecordInput) (*DNSRecord, error) {
	if !ValidID(zoneID) {
		return nil, errors.New("invalid zone id")
	}
	if err := in.Validate(); err != nil {
		return nil, err
	}

	var rec DNSRecord
	if _, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/zones/%s/dns_records", zoneID), nil, in, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

func (c *Client) UpdateDNSRecord(ctx context.Context, zoneID, recordID string, in DNSRecordInput) (*DNSRecord, error) {
	if !ValidID(zoneID) {
		return nil, errors.New("invalid zone id")
	}
	if !ValidID(recordID) {
		return nil, errors.New("invalid record id")
	}
	if err := in.Validate(); err != nil {
		return nil, err
	}

	var rec DNSRecord
	if _, err := c.do(ctx, http.MethodPut, fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, recordID), nil, in, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

func (c *Client) DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error {
	if !ValidID(zoneID) {
		return errors.New("invalid zone id")
	}
	if !ValidID(recordID) {
		return errors.New("invalid record id")
	}

	_, err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, recordID), nil, nil, nil)
	return err
}
