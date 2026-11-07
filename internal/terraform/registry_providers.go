package terraform

import (
	"context"
	"net/url"
	"strconv"
)

// SearchProviders uses the registry UI's v2 JSON:API (undocumented, 20 per page).
func (c *RegistryClient) SearchProviders(ctx context.Context, q string, page int) (RegistryProviderSearch, error) {
	res := RegistryProviderSearch{Providers: []RegistryProvider{}}
	if page < 1 {
		page = 1
	}
	v := url.Values{"filter[name]": {q}, "page[size]": {"20"}, "page[number]": {strconv.Itoa(page)}, "sort": {"-downloads"}}
	var raw struct {
		Data []struct {
			Attributes struct {
				Name        string `json:"name"`
				Namespace   string `json:"namespace"`
				FullName    string `json:"full-name"`
				Description string `json:"description"`
				Downloads   int64  `json:"downloads"`
				Tier        string `json:"tier"`
				Source      string `json:"source"`
			} `json:"attributes"`
		} `json:"data"`
		Meta struct {
			Pagination struct {
				NextPage *int `json:"next-page"`
			} `json:"pagination"`
		} `json:"meta"`
	}
	if err := c.get(ctx, "/v2/providers", v, &raw, true); err != nil {
		return res, err
	}
	for _, d := range raw.Data {
		a := d.Attributes
		res.Providers = append(res.Providers, RegistryProvider{Namespace: a.Namespace, Name: a.Name, FullName: a.FullName,
			Description: a.Description, Downloads: a.Downloads, Tier: a.Tier, Source: a.Source})
	}
	if raw.Meta.Pagination.NextPage != nil {
		res.NextPage = *raw.Meta.Pagination.NextPage
	}
	return res, nil
}

func (c *RegistryClient) ProviderDetail(ctx context.Context, ns, typ string) (RegistryProviderDetail, error) {
	var res RegistryProviderDetail
	if err := regSeg(ns, typ); err != nil {
		return res, err
	}
	var raw struct {
		RegistryProvider
		Versions []string `json:"versions"`
	}
	if err := c.get(ctx, "/v1/providers/"+ns+"/"+typ, nil, &raw, true); err != nil {
		return res, err
	}
	res.Provider = raw.RegistryProvider
	res.Provider.FullName = ns + "/" + typ
	res.Versions = reversed(raw.Versions)
	res.Latest = raw.Version
	res.DocsURL = c.BaseURL + "/providers/" + ns + "/" + typ + "/latest/docs"
	return res, nil
}

func (c *RegistryClient) LatestModuleVersion(ctx context.Context, ns, name, provider string) (string, error) {
	d, err := c.ModuleDetail(ctx, ns, name, provider, "")
	return d.Module.Version, err
}

func (c *RegistryClient) LatestProviderVersion(ctx context.Context, ns, typ string) (string, error) {
	d, err := c.ProviderDetail(ctx, ns, typ)
	return d.Latest, err
}
