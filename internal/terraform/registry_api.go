package terraform

import (
	"context"
	"net/url"
	"strconv"
	"unicode/utf8"
)

const regReadmeCap = 40 << 10

type RegistryModule struct {
	ID          string `json:"id"`
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	Provider    string `json:"provider"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Source      string `json:"source"`
	PublishedAt string `json:"published_at"`
	Downloads   int64  `json:"downloads"`
	Verified    bool   `json:"verified"`
}

type RegistryModuleSearch struct {
	Modules    []RegistryModule `json:"modules"`
	NextOffset int              `json:"next_offset"` // 0 = no further page
	TotalHint  int              `json:"total_hint"`  // lower bound: the registry gives no total
}

type RegistryInput struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Default     any    `json:"default"`
	Required    bool   `json:"required"`
}

type RegistryOutput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type RegistryModuleProvider struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Source    string `json:"source"`
	Version   string `json:"version"`
}

type RegistryModuleDep struct {
	Name    string `json:"name"`
	Source  string `json:"source"`
	Version string `json:"version"`
}

type RegistryModuleDetail struct {
	Module          RegistryModule           `json:"module"`
	Versions        []string                 `json:"versions"` // newest first
	Inputs          []RegistryInput          `json:"inputs"`
	Outputs         []RegistryOutput         `json:"outputs"`
	Providers       []RegistryModuleProvider `json:"providers"`
	Dependencies    []RegistryModuleDep      `json:"dependencies"`
	Readme          string                   `json:"readme"`
	ReadmeTruncated bool                     `json:"readme_truncated"`
}

type RegistryProvider struct {
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	FullName    string `json:"full_name"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description"`
	Downloads   int64  `json:"downloads"`
	Tier        string `json:"tier"`
	Source      string `json:"source"`
	PublishedAt string `json:"published_at,omitempty"`
}

type RegistryProviderSearch struct {
	Providers []RegistryProvider `json:"providers"`
	NextPage  int                `json:"next_page"` // 0 = no further page
}

type RegistryProviderDetail struct {
	Provider RegistryProvider `json:"provider"`
	Versions []string         `json:"versions"` // newest first
	Latest   string           `json:"latest"`
	DocsURL  string           `json:"docs_url"`
}

func reversed(s []string) []string {
	out := make([]string, len(s))
	for i, v := range s {
		out[len(s)-1-i] = v
	}
	return out
}

func (c *RegistryClient) SearchModules(ctx context.Context, q, provider string, offset, limit int) (RegistryModuleSearch, error) {
	var res RegistryModuleSearch
	if provider != "" {
		if err := regSeg(provider); err != nil {
			return res, err
		}
	}
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 20
	} else if limit > 50 {
		limit = 50
	}
	v := url.Values{"q": {q}, "offset": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(limit)}}
	if provider != "" {
		v.Set("provider", provider)
	}
	var raw struct {
		Meta struct {
			NextOffset *int `json:"next_offset"`
		} `json:"meta"`
		Modules []RegistryModule `json:"modules"`
	}
	if err := c.get(ctx, "/v1/modules/search", v, &raw, true); err != nil {
		return res, err
	}
	res.Modules = raw.Modules
	if res.Modules == nil {
		res.Modules = []RegistryModule{}
	}
	if raw.Meta.NextOffset != nil {
		res.NextOffset = *raw.Meta.NextOffset
	}
	res.TotalHint = offset + len(res.Modules)
	return res, nil
}

// ModuleDetail returns the latest version when version is empty.
func (c *RegistryClient) ModuleDetail(ctx context.Context, ns, name, provider, version string) (RegistryModuleDetail, error) {
	var res RegistryModuleDetail
	if err := regSeg(ns, name, provider); err != nil {
		return res, err
	}
	path := "/v1/modules/" + ns + "/" + name + "/" + provider
	if version != "" {
		if err := regSeg(version); err != nil {
			return res, err
		}
		path += "/" + version
	}
	var raw struct {
		RegistryModule
		Providers []string `json:"providers"`
		Versions  []string `json:"versions"`
		Root      struct {
			Readme       string                   `json:"readme"`
			Inputs       []RegistryInput          `json:"inputs"`
			Outputs      []RegistryOutput         `json:"outputs"`
			Dependencies []RegistryModuleDep      `json:"dependencies"`
			ProviderDeps []RegistryModuleProvider `json:"provider_dependencies"`
		} `json:"root"`
	}
	if err := c.get(ctx, path, nil, &raw, true); err != nil {
		return res, err
	}
	res.Module = raw.RegistryModule
	res.Versions = reversed(raw.Versions)
	res.Inputs, res.Outputs, res.Dependencies = raw.Root.Inputs, raw.Root.Outputs, raw.Root.Dependencies
	res.Providers = raw.Root.ProviderDeps
	if len(res.Providers) == 0 {
		for _, p := range raw.Providers {
			res.Providers = append(res.Providers, RegistryModuleProvider{Name: p})
		}
	}
	res.Readme = raw.Root.Readme
	if res.Inputs == nil {
		res.Inputs = []RegistryInput{}
	}
	if res.Outputs == nil {
		res.Outputs = []RegistryOutput{}
	}
	if res.Dependencies == nil {
		res.Dependencies = []RegistryModuleDep{}
	}
	if res.Providers == nil {
		res.Providers = []RegistryModuleProvider{}
	}
	if len(res.Readme) > regReadmeCap {
		cut := regReadmeCap
		for cut > 0 && !utf8.RuneStart(res.Readme[cut]) {
			cut--
		}
		res.Readme, res.ReadmeTruncated = res.Readme[:cut], true
	}
	return res, nil
}
