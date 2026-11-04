package cloudflare

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

type PurgeRequest struct {
	Everything bool     `json:"purge_everything,omitempty"`
	Files      []string `json:"files,omitempty"`
	Hosts      []string `json:"hosts,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Prefixes   []string `json:"prefixes,omitempty"`
}

// Validate checks that exactly one purge mode is set, and Files has at most 30 entries.
func (r PurgeRequest) Validate() error {
	modes := 0
	if r.Everything {
		modes++
	}
	if len(r.Files) > 0 {
		modes++
	}
	if len(r.Hosts) > 0 {
		modes++
	}
	if len(r.Tags) > 0 {
		modes++
	}
	if len(r.Prefixes) > 0 {
		modes++
	}

	if modes != 1 {
		return errors.New("exactly one purge mode must be specified")
	}

	if len(r.Files) > 30 {
		return errors.New("files purge mode allows at most 30 entries")
	}

	return nil
}

// PurgeCache purges cache for a zone.
func (c *Client) PurgeCache(ctx context.Context, zoneID string, req PurgeRequest) error {
	if !ValidID(zoneID) {
		return errors.New("invalid zone id")
	}
	if err := req.Validate(); err != nil {
		return err
	}

	_, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/zones/%s/purge_cache", zoneID), nil, req, nil)
	return err
}
