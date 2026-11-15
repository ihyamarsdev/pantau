package cloudflare

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// Tunnel is a Cloudflare Tunnel (cloudflared) of an account; read-only in Pantau.
type Tunnel struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Status          string `json:"status"` // healthy, degraded, down, inactive
	TunType         string `json:"tun_type,omitempty"`
	CreatedAt       string `json:"created_at,omitempty"`
	ConnsActiveAt   string `json:"conns_active_at,omitempty"`
	ConnsInactiveAt string `json:"conns_inactive_at,omitempty"`
}

// ListTunnels lists the account's non-deleted tunnels (GET /accounts/{id}/cfd_tunnel?is_deleted=false).
// Pagination: per_page=100, max 20 pages.
func (c *Client) ListTunnels(ctx context.Context, accountID string) ([]Tunnel, error) {
	if !ValidID(accountID) {
		return nil, errors.New("invalid account id")
	}

	const perPage = 100
	const maxPages = 20

	var all []Tunnel
	for page := 1; page <= maxPages; page++ {
		q := make(url.Values)
		q.Set("is_deleted", "false")
		q.Set("per_page", strconv.Itoa(perPage))
		q.Set("page", strconv.Itoa(page))

		var batch []Tunnel
		info, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/accounts/%s/cfd_tunnel", accountID), q, nil, &batch)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)

		if len(batch) == 0 {
			break
		}
		if info != nil && info.TotalPages > 0 && page >= info.TotalPages {
			break
		}
		if len(batch) < perPage && (info == nil || info.TotalPages == 0) {
			break
		}
	}
	return all, nil
}
