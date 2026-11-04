package cloudflare

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

type Zone struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	Paused      bool     `json:"paused"`
	Type        string   `json:"type"`
	NameServers []string `json:"name_servers"`
	Plan        string   `json:"plan"`         // from plan.name
	AccountName string   `json:"account_name"` // from account.name
}

type cfZoneRaw struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	Paused      bool     `json:"paused"`
	Type        string   `json:"type"`
	NameServers []string `json:"name_servers"`
	Plan        struct {
		Name string `json:"name"`
	} `json:"plan"`
	Account struct {
		Name string `json:"name"`
	} `json:"account"`
}

func (raw *cfZoneRaw) toZone() Zone {
	return Zone{
		ID:          raw.ID,
		Name:        raw.Name,
		Status:      raw.Status,
		Paused:      raw.Paused,
		Type:        raw.Type,
		NameServers: raw.NameServers,
		Plan:        raw.Plan.Name,
		AccountName: raw.Account.Name,
	}
}

// ListZones lists zones. accountID != "" filters with ?account.id=.
// Pagination: per_page=50, max 20 pages.
func (c *Client) ListZones(ctx context.Context, accountID string) ([]Zone, error) {
	if accountID != "" && !ValidID(accountID) {
		return nil, errors.New("invalid account id")
	}

	const perPage = 50
	const maxPages = 20

	var all []Zone
	for page := 1; page <= maxPages; page++ {
		q := make(url.Values)
		q.Set("per_page", strconv.Itoa(perPage))
		q.Set("page", strconv.Itoa(page))
		if accountID != "" {
			q.Set("account.id", accountID)
		}

		var rawList []cfZoneRaw
		info, err := c.do(ctx, http.MethodGet, "/zones", q, nil, &rawList)
		if err != nil {
			return nil, err
		}

		for _, item := range rawList {
			all = append(all, item.toZone())
		}

		if len(rawList) == 0 {
			break
		}
		if info != nil && info.TotalPages > 0 && page >= info.TotalPages {
			break
		}
		if len(rawList) < perPage && (info == nil || info.TotalPages == 0) {
			break
		}
	}

	return all, nil
}

// GetZone gets a single zone by ID.
func (c *Client) GetZone(ctx context.Context, zoneID string) (*Zone, error) {
	if !ValidID(zoneID) {
		return nil, errors.New("invalid zone id")
	}

	var raw cfZoneRaw
	if _, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/zones/%s", zoneID), nil, nil, &raw); err != nil {
		return nil, err
	}
	z := raw.toZone()
	return &z, nil
}
