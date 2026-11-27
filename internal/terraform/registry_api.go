package terraform

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"sync"
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

type RegistrySubmodule struct {
	Path         string                   `json:"path"`
	Readme       string                   `json:"readme,omitempty"`
	Inputs       []RegistryInput          `json:"inputs"`
	Outputs      []RegistryOutput         `json:"outputs"`
	Dependencies []RegistryModuleDep      `json:"dependencies,omitempty"`
	ProviderDeps []RegistryModuleProvider `json:"provider_dependencies,omitempty"`
}

type RegistryModuleDetail struct {
	Module          RegistryModule           `json:"module"`
	Versions        []string                 `json:"versions"` // newest first
	Inputs          []RegistryInput          `json:"inputs"`
	Outputs         []RegistryOutput         `json:"outputs"`
	Submodules      []RegistrySubmodule      `json:"submodules"`
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
		Root struct {
			Readme       string                   `json:"readme"`
			Inputs       []RegistryInput          `json:"inputs"`
			Outputs      []RegistryOutput         `json:"outputs"`
			Dependencies []RegistryModuleDep      `json:"dependencies"`
			ProviderDeps []RegistryModuleProvider `json:"provider_dependencies"`
		} `json:"root"`
		Submodules []RegistrySubmodule `json:"submodules"`
	}
	if err := c.get(ctx, path, nil, &raw, true); err != nil {
		return res, err
	}
	res.Module = raw.RegistryModule
	res.Versions = reversed(raw.Versions)
	res.Inputs, res.Outputs, res.Dependencies = raw.Root.Inputs, raw.Root.Outputs, raw.Root.Dependencies
	res.Submodules = raw.Submodules
	for i := range res.Submodules {
		if res.Submodules[i].Inputs == nil {
			res.Submodules[i].Inputs = []RegistryInput{}
		}
		if res.Submodules[i].Outputs == nil {
			res.Submodules[i].Outputs = []RegistryOutput{}
		}
	}
	if res.Submodules == nil {
		res.Submodules = []RegistrySubmodule{}
	}
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

// BatchSearchModules runs multiple module searches with bounded parallelism and deduplicates results by module ID.
func (c *RegistryClient) BatchSearchModules(ctx context.Context, queries []string, provider string, limit int) ([]RegistryModule, error) {
	if provider != "" {
		if err := regSeg(provider); err != nil {
			return nil, err
		}
	}
	if limit <= 0 {
		limit = 10
	} else if limit > 50 {
		limit = 50
	}
	clean := make([]string, 0, len(queries))
	seenQ := make(map[string]bool, len(queries))
	for _, q := range queries {
		q = strings.TrimSpace(q)
		if q == "" || seenQ[q] {
			continue
		}
		seenQ[q] = true
		clean = append(clean, q)
	}
	if len(clean) == 0 {
		return []RegistryModule{}, nil
	}

	type searchRes struct {
		mods []RegistryModule
		err  error
	}
	results := make([]searchRes, len(clean))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, q := range clean {
		wg.Add(1)
		go func(idx int, query string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			r, err := c.SearchModules(ctx, query, provider, 0, limit)
			results[idx] = searchRes{mods: r.Modules, err: err}
		}(i, q)
	}
	wg.Wait()

	out := make([]RegistryModule, 0)
	seenMod := make(map[string]bool)
	var firstErr error
	for _, r := range results {
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		for _, m := range r.mods {
			key := m.Namespace + "/" + m.Name + "/" + m.Provider
			if key == "//" {
				key = m.ID
			}
			if seenMod[key] {
				continue
			}
			seenMod[key] = true
			out = append(out, m)
		}
	}
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

// InspectedRequiredInput is a compact required module input for agent consumption.
type InspectedRequiredInput struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// InspectedOptionalInput is a compact optional module input for agent consumption.
type InspectedOptionalInput struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Default any    `json:"default,omitempty"`
}

// InspectedModuleSection holds inputs and outputs for root or a submodule.
type InspectedModuleSection struct {
	Path                string                   `json:"path,omitempty"`
	Source              string                   `json:"source,omitempty"`
	RequiredInputs      []InspectedRequiredInput `json:"required_inputs"`
	OptionalInputs      []InspectedOptionalInput `json:"optional_inputs"`
	OptionalInputsTotal int                      `json:"optional_inputs_total,omitempty"`
	Outputs             []string                 `json:"outputs,omitempty"`
}

// InspectedModuleSchema is the <4 KB summary of a registry module returned by tf_catalog inspect.
type InspectedModuleSchema struct {
	ModuleID    string                   `json:"module_id"`
	Source      string                   `json:"source"`
	Version     string                   `json:"version"`
	Description string                   `json:"description,omitempty"`
	Root        InspectedModuleSection   `json:"root"`
	Submodules  []InspectedModuleSection `json:"submodules,omitempty"`
}

func clipText(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

func compactDefault(v any) any {
	if v == nil {
		return nil
	}
	if s, ok := v.(string); ok {
		if len(s) > 60 {
			return clipText(s, 60)
		}
		return s
	}
	b, err := json.Marshal(v)
	if err == nil && len(b) > 60 {
		return clipText(string(b), 60)
	}
	return v
}

func buildSection(inputs []RegistryInput, outputs []RegistryOutput, optCap int, includeOutputs bool) InspectedModuleSection {
	sec := InspectedModuleSection{
		RequiredInputs: []InspectedRequiredInput{},
		OptionalInputs: []InspectedOptionalInput{},
	}
	var opts []InspectedOptionalInput
	for _, in := range inputs {
		if in.Required {
			sec.RequiredInputs = append(sec.RequiredInputs, InspectedRequiredInput{
				Name:        in.Name,
				Type:        in.Type,
				Description: clipText(in.Description, 90),
			})
		} else {
			opts = append(opts, InspectedOptionalInput{
				Name:    in.Name,
				Type:    in.Type,
				Default: compactDefault(in.Default),
			})
		}
	}
	if optCap >= 0 && len(opts) > optCap {
		sec.OptionalInputsTotal = len(opts)
		sec.OptionalInputs = opts[:optCap]
	} else {
		sec.OptionalInputs = opts
	}
	if includeOutputs && len(outputs) > 0 {
		sec.Outputs = make([]string, 0, len(outputs))
		for _, o := range outputs {
			sec.Outputs = append(sec.Outputs, o.Name)
		}
		if len(sec.Outputs) > 25 {
			sec.Outputs = sec.Outputs[:25]
		}
	}
	return sec
}

func matchSubmodule(path, want string) bool {
	if want == "" {
		return false
	}
	p := strings.Trim(path, "/")
	w := strings.Trim(want, "/")
	return p == w || strings.TrimPrefix(p, "modules/") == w || strings.HasSuffix(p, "/"+w)
}

// FormatModuleSchema builds a compact (<4 KB) schema summary for a module and its submodules.
// When submodule is non-empty, the matching submodule returns all of its optional inputs in full.
func FormatModuleSchema(d RegistryModuleDetail, submodule string) InspectedModuleSchema {
	baseSource := d.Module.Namespace + "/" + d.Module.Name + "/" + d.Module.Provider
	rootOptCap := 12
	if submodule != "" {
		rootOptCap = 5
	}
	schema := InspectedModuleSchema{
		ModuleID:    baseSource,
		Source:      baseSource,
		Version:     d.Module.Version,
		Description: clipText(d.Module.Description, 180),
		Root:        buildSection(d.Inputs, d.Outputs, rootOptCap, submodule == ""),
	}

	subOptCap := 8
	if len(d.Submodules) > 4 {
		subOptCap = 4
	}
	for _, sm := range d.Submodules {
		isMatch := matchSubmodule(sm.Path, submodule)
		if submodule != "" && !isMatch {
			continue
		}
		capForSub := subOptCap
		if isMatch {
			capForSub = -1 // return all optional inputs in full for the requested submodule
		}
		sec := buildSection(sm.Inputs, sm.Outputs, capForSub, isMatch)
		sec.Path = sm.Path
		sec.Source = baseSource + "//" + strings.TrimPrefix(sm.Path, "/")
		schema.Submodules = append(schema.Submodules, sec)
	}

	// Enforce < 4 KB per module by trimming optional_inputs if needed.
	const maxSchemaBytes = 3950
	if b, err := json.Marshal(schema); err == nil && len(b) > maxSchemaBytes {
		if schema.Root.OptionalInputsTotal == 0 && len(schema.Root.OptionalInputs) > 4 {
			schema.Root.OptionalInputsTotal = len(schema.Root.OptionalInputs)
		}
		if len(schema.Root.OptionalInputs) > 4 {
			schema.Root.OptionalInputs = schema.Root.OptionalInputs[:4]
		}
		for i := range schema.Submodules {
			if submodule != "" && matchSubmodule(schema.Submodules[i].Path, submodule) {
				continue
			}
			if len(schema.Submodules[i].OptionalInputs) > 3 {
				if schema.Submodules[i].OptionalInputsTotal == 0 {
					schema.Submodules[i].OptionalInputsTotal = len(schema.Submodules[i].OptionalInputs)
				}
				schema.Submodules[i].OptionalInputs = schema.Submodules[i].OptionalInputs[:3]
			}
		}
	}
	return schema
}
