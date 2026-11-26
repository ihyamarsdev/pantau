package gwupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// The upstream release sources, one per CLI behind a Pantau gateway. Each is read with the lightest
// request that still carries the version:
//
//	ansible-core -> PyPI, the JSON API decoded as a stream so the release history is never fetched
//	terraform     -> the HashiCorp releases API (the same one `terraform version` itself uses)
//	opentofu      -> the GitHub "latest release" redirect, which names the release in its Location
//
// Nothing here installs or downloads a release; see ADR 0080.

var (
	// PyPIURL is the JSON API of the ansible-core package.
	PyPIURL = "https://pypi.org/pypi/ansible-core/json"
	// HashiCorpURL is the latest-release endpoint of the Terraform releases API.
	HashiCorpURL = "https://api.releases.hashicorp.com/v1/releases/terraform/latest"
	// OpenTofuURL is the GitHub latest-release page of OpenTofu; it redirects to the release tag.
	OpenTofuURL = "https://github.com/opentofu/opentofu/releases/latest"
	// KubectlBaseURL is the Kubernetes release host (dl.k8s.io); stable.txt and stable-<minor>.txt sit
	// beside the release directories.
	KubectlBaseURL = "https://dl.k8s.io/release"
)

// kubectlRelease and kubectlMinor are the only shapes accepted for a kubectl version and a minor
// channel. Anything else (a path, "latest", a pre-release) never reaches a URL.
var (
	kubectlRelease = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	kubectlMinor   = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)
)

// requestTimeout bounds one upstream request.
const requestTimeout = 15 * time.Second

// httpClient is the client used for the upstream requests. The redirect is not followed for OpenTofu:
// the release tag is what the Location header carries, and following it would download the whole
// release payload.
var httpClient = &http.Client{Timeout: requestTimeout}

// Release is one upstream release: the version and where it was published.
type Release struct {
	Version string
	URL     string
}

// LatestAnsible returns the newest ansible-core release published on PyPI.
func LatestAnsible(ctx context.Context) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, PyPIURL, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Pantau/"+UserAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("pypi: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("pypi: status %s", resp.Status)
	}

	// The PyPI document starts with "info", and "version" is its first field. Decoding just that
	// object and closing the body keeps the rest of the release history from ever being read.
	dec := json.NewDecoder(resp.Body)
	if _, err := dec.Token(); err != nil { // opening brace
		return Release{}, fmt.Errorf("pypi: %w", err)
	}
	if _, err := dec.Token(); err != nil { // "info"
		return Release{}, fmt.Errorf("pypi: %w", err)
	}
	var info struct {
		Version string `json:"version"`
	}
	if err := dec.Decode(&info); err != nil {
		return Release{}, fmt.Errorf("pypi: %w", err)
	}
	if info.Version == "" {
		return Release{}, fmt.Errorf("pypi: no version in response")
	}
	return Release{Version: info.Version, URL: "https://pypi.org/project/ansible-core/" + info.Version + "/"}, nil
}

// LatestKubectl returns the newest stable kubectl release.
func LatestKubectl(ctx context.Context) (Release, error) {
	return readKubectlRelease(ctx, KubectlBaseURL+"/stable.txt")
}

// LatestKubectlChannel returns the newest patch release of one Kubernetes minor, e.g. "1.31".
func LatestKubectlChannel(ctx context.Context, minor string) (Release, error) {
	if !kubectlMinor.MatchString(minor) {
		return Release{}, fmt.Errorf("kubernetes: %q is not a minor version", minor)
	}
	return readKubectlRelease(ctx, KubectlBaseURL+"/stable-"+minor+".txt")
}

// readKubectlRelease reads a stable.txt style file: one release such as "v1.31.4".
func readKubectlRelease(ctx context.Context, url string) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("User-Agent", "Pantau/"+UserAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("kubernetes: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("kubernetes: status %s", resp.Status)
	}
	// The file is about ten bytes; anything longer is not the file we asked for.
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return Release{}, fmt.Errorf("kubernetes: %w", err)
	}
	version := strings.TrimPrefix(strings.TrimSpace(string(data)), "v")
	if !kubectlRelease.MatchString(version) {
		return Release{}, fmt.Errorf("kubernetes: %q is not a release version", strings.TrimSpace(string(data)))
	}
	return Release{Version: version, URL: "https://github.com/kubernetes/kubernetes/releases/tag/v" + version}, nil
}

// LatestTerraform returns the newest Terraform release published by HashiCorp.
func LatestTerraform(ctx context.Context) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, HashiCorpURL, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Pantau/"+UserAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("hashicorp: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("hashicorp: status %s", resp.Status)
	}

	var doc struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return Release{}, fmt.Errorf("hashicorp: %w", err)
	}
	if doc.Version == "" {
		return Release{}, fmt.Errorf("hashicorp: no version in response")
	}
	return Release{
		Version: doc.Version,
		URL:     "https://github.com/hashicorp/terraform/releases/tag/v" + doc.Version,
	}, nil
}

// LatestOpenTofu returns the newest OpenTofu release. The GitHub latest-release page answers with a
// redirect to the release tag, so the version comes from the Location header and no release payload
// is downloaded.
func LatestOpenTofu(ctx context.Context) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, OpenTofuURL, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("User-Agent", "Pantau/"+UserAgent)

	client := &http.Client{
		Timeout: requestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse // the tag is in the Location header
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("opentofu: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))

	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusMovedPermanently &&
		resp.StatusCode != http.StatusPermanentRedirect && resp.StatusCode != http.StatusSeeOther {
		return Release{}, fmt.Errorf("opentofu: status %s", resp.Status)
	}

	loc := resp.Header.Get("Location")
	version := versionFromTagPath(loc)
	if version == "" {
		return Release{}, fmt.Errorf("opentofu: no version in %q", loc)
	}
	return Release{Version: version, URL: loc}, nil
}

// versionFromTagPath pulls the version out of a release tag URL such as
// "https://github.com/opentofu/opentofu/releases/tag/v1.13.9".
func versionFromTagPath(loc string) string {
	if loc == "" {
		return ""
	}
	i := strings.LastIndex(loc, "/")
	if i < 0 {
		return ""
	}
	tag := loc[i+1:]
	if tag == "" {
		return ""
	}
	if _, ok := ParseVersion(tag); !ok {
		return ""
	}
	return tag
}
