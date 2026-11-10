package inspector

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"pantau/internal/store"
)

// Web domains: the reverse proxies a host serves (nginx, Caddy, Traefik),
// collected agentless over SSH in one session per scan.
//
// The remote script prints marked sections:
//
//	__PANTAU_SECTION__ <kind> <where> <container>
//
// so the Go side can split the output per source. Every section is capped
// remotely (head -c) so a huge config cannot flood the SSH channel.

const (
	webSectionMarker = "__PANTAU_SECTION__"
	webScanTimeout   = 60 * time.Second
	// ponytail: 10 containers per scan is enough to cover a proxy stack; raise it when a host runs more proxies than that.
	webMaxProxiedContainers = 10
	webMaxSectionBytes      = 2000000
)

var (
	// ErrWebScanInProgress is returned when a scan of the same host is already running.
	ErrWebScanInProgress = errors.New("web domain scan already in progress for this host")
	// ErrWebScanSSH marks a failure to reach the host (the handler answers 502).
	ErrWebScanSSH = errors.New("ssh connection failed")
)

// webScanInFlight guards concurrent scans of the same host. The key carries the
// inspector pointer so two inspectors (tests, tools) do not block each other.
var webScanInFlight sync.Map

type webScanKey struct {
	ins    *Inspector
	hostID int64
}

// webDomainScript collects everything in one remote shell run: the host nginx
// and Caddy configs plus the Docker containers that run a proxy.
const webDomainScriptTemplate = `
PATH="$PATH:/usr/sbin:/sbin:/usr/local/sbin"; export PATH

if command -v nginx >/dev/null 2>&1; then
  printf '%s\n' "__PANTAU_SECTION__ nginxversion host"
  nginx -v 2>&1 | head -c 200
  printf '\n'
  printf '%s\n' "__PANTAU_SECTION__ nginx host"
  if out=$(nginx -T 2>&1); then
    printf 'METHOD nginx -T\n'
    printf '%s' "$out" | head -c 2000000
  elif out=$(sudo -n nginx -T 2>&1); then
    printf 'METHOD sudo nginx -T\n'
    printf '%s' "$out" | head -c 2000000
  else
    printf 'METHOD config files\n'
    found=0
    for f in /etc/nginx/nginx.conf /etc/nginx/conf.d/*.conf /etc/nginx/http.d/*.conf /etc/nginx/sites-enabled/* /usr/local/etc/nginx/nginx.conf /usr/local/etc/nginx/conf.d/*.conf /usr/local/etc/nginx/http.d/*.conf /usr/local/etc/nginx/servers-enabled/* /opt/homebrew/etc/nginx/nginx.conf /opt/homebrew/etc/nginx/conf.d/*.conf /opt/homebrew/etc/nginx/servers-enabled/*; do
      [ -f "$f" ] || continue
      found=1
      printf '# configuration file %s:\n' "$f"
      cat "$f" 2>/dev/null
    done | head -c 2000000
    [ "$found" = "1" ] || printf 'ERROR no readable nginx configuration found\n'
  fi
  printf '\n'
fi

if command -v caddy >/dev/null 2>&1; then
  printf '%s\n' "__PANTAU_SECTION__ caddyversion host"
  caddy version 2>&1 | head -c 200
  printf '\n'
  printf '%s\n' "__PANTAU_SECTION__ caddy host"
  if out=$(curl -fsS --max-time 3 http://127.0.0.1:2019/config/ 2>/dev/null); then
    printf 'METHOD caddy admin API\n'
    printf '%s' "$out" | head -c 2000000
  elif command -v wget >/dev/null 2>&1 && out=$(wget -qO- -T 3 http://127.0.0.1:2019/config/ 2>/dev/null); then
    printf 'METHOD caddy admin API\n'
    printf '%s' "$out" | head -c 2000000
  elif out=$(caddy adapt --config /etc/caddy/Caddyfile --adapter caddyfile 2>&1); then
    printf 'METHOD caddy adapt\n'
    printf '%s' "$out" | head -c 2000000
  elif [ -f /etc/caddy/Caddyfile ]; then
    printf 'METHOD Caddyfile\n'
    cat /etc/caddy/Caddyfile 2>/dev/null | head -c 2000000
  else
    printf 'ERROR no caddy configuration found\n'
  fi
  printf '\n'
fi

if command -v docker >/dev/null 2>&1; then
  ids=$(docker ps -q 2>/dev/null | head -n 40)
  if [ -n "$ids" ]; then
    printf '%s\n' "__PANTAU_SECTION__ docker host"
    for id in $ids; do
      name=$(docker inspect --format '{{.Name}}' "$id" 2>/dev/null | sed 's|^/||')
      img=$(docker inspect --format '{{.Config.Image}}' "$id" 2>/dev/null)
      labels=$(docker inspect --format '{{json .Config.Labels}}' "$id" 2>/dev/null)
      printf 'CONTAINER_NAME %s\n' "$name"
      printf 'CONTAINER_IMAGE %s\n' "$img"
      printf 'CONTAINER_LABELS %s\n' "$labels"
      docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$id" 2>/dev/null | grep -E '^(VIRTUAL_HOST|VIRTUAL_PORT|LETSENCRYPT_HOST)=' | sed 's|^|ENV |'
    done | head -c 2000000
    printf '\n'
    n=0
    for id in $ids; do
      name=$(docker inspect --format '{{.Name}}' "$id" 2>/dev/null | sed 's|^/||')
      img=$(docker inspect --format '{{.Config.Image}}' "$id" 2>/dev/null)
      # only a plain container name is used in a docker exec
      case "$name" in
        ""|*[!a-zA-Z0-9_.-]*) continue;;
      esac
      case "$img" in
        *nginx*|*openresty*)
          if [ "$n" -lt 10 ]; then
            printf '%s\n' "__PANTAU_SECTION__ nginx container $name"
            printf 'METHOD nginx -T\n'
            docker exec "$name" nginx -T 2>&1 | head -c 2000000
            printf '\n'
            n=$((n+1))
          fi;;
        *caddy*)
          if [ "$n" -lt 10 ]; then
            printf '%s\n' "__PANTAU_SECTION__ caddy container $name"
            printf 'METHOD caddy admin API\n'
            docker exec "$name" sh -c 'wget -qO- -T 3 http://127.0.0.1:2019/config/ 2>/dev/null || caddy adapt --config /etc/caddy/Caddyfile --adapter caddyfile 2>/dev/null || cat /etc/caddy/Caddyfile 2>/dev/null' 2>&1 | head -c 2000000
            printf '\n'
            n=$((n+1))
          fi;;
      esac
    done
  fi
fi

printf '%s\n' "__PANTAU_SECTION__ traefik host"
if command -v curl >/dev/null 2>&1; then
  curl -fsS --max-time 3 http://127.0.0.1:8080/api/http/routers 2>/dev/null | head -c 2000000
fi
printf '\n'
`

// webDomainScript is the remote script with the output cap and the container
// limit applied from the constants above.
var webDomainScript = strings.NewReplacer(
	"2000000", strconv.Itoa(webMaxSectionBytes),
	"n -lt 10", "n -lt "+strconv.Itoa(webMaxProxiedContainers),
).Replace(webDomainScriptTemplate)

// webSection is one marked chunk of the remote output.
type webSection struct {
	Kind      string
	Where     string
	Container string
	Body      string
}

// splitWebSections splits the remote output into its marked sections. Lines
// before the first marker are ignored.
func splitWebSections(out string) []webSection {
	var sections []webSection
	var cur *webSection
	var body strings.Builder
	flush := func() {
		if cur != nil {
			cur.Body = body.String()
			sections = append(sections, *cur)
			cur = nil
		}
		body.Reset()
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, webSectionMarker) {
			flush()
			fields := strings.Fields(strings.TrimPrefix(line, webSectionMarker))
			s := webSection{}
			if len(fields) > 0 {
				s.Kind = fields[0]
			}
			if len(fields) > 1 {
				s.Where = fields[1]
			}
			if len(fields) > 2 {
				s.Container = strings.Join(fields[2:], " ")
			}
			cur = &s
			continue
		}
		if cur != nil {
			body.WriteString(line)
			body.WriteString("\n")
		}
	}
	flush()
	return sections
}

// ScanWebDomains collects the domains served by the reverse proxies of a host.
// The result is returned, not stored: the caller decides to persist it.
func (ins *Inspector) ScanWebDomains(hostID int64) (*store.WebScan, error) {
	key := webScanKey{ins: ins, hostID: hostID}
	if _, loaded := webScanInFlight.LoadOrStore(key, struct{}{}); loaded {
		return nil, ErrWebScanInProgress
	}
	defer webScanInFlight.Delete(key)

	host, err := ins.db.GetHost(hostID)
	if err != nil {
		return nil, err
	}
	if host == nil {
		return nil, fmt.Errorf("host %d not found", hostID)
	}
	runner, err := ins.GetRunnerForHost(host)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWebScanSSH, err)
	}
	defer runner.Close()

	stdout, _, _, err := runner.ExecTimeout(dockerShell(webDomainScript), webScanTimeout)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWebScanSSH, err)
	}
	return parseWebScan(hostID, stdout), nil
}

// parseWebScan turns the remote output into the stored scan.
func parseWebScan(hostID int64, out string) *store.WebScan {
	scan := &store.WebScan{
		HostID:    hostID,
		ScannedAt: time.Now().UTC(),
		Proxies:   []store.WebProxy{},
		Sites:     []store.WebSite{},
		Warnings:  []string{},
	}
	sections := splitWebSections(out)

	versions := map[string]string{}
	for _, s := range sections {
		switch s.Kind {
		case "nginxversion":
			mergeWebVersions(versions, "nginx", s.Body)
		case "caddyversion":
			mergeWebVersions(versions, "caddy", s.Body)
		}
	}

	seenProxy := map[string]bool{}
	addProxy := func(p store.WebProxy) {
		k := p.Kind + "|" + p.Where + "|" + p.Container
		if seenProxy[k] {
			return
		}
		seenProxy[k] = true
		scan.Proxies = append(scan.Proxies, p)
	}
	// Versions are read from the host binaries only; a container's version is unknown.
	hostVersion := func(kind, where string) string {
		if where == "host" {
			return versions[kind]
		}
		return ""
	}

	// containers first: the label based sources (Traefik, caddy-docker-proxy,
	// nginx-proxy) need the container inventory.
	containers := []dockerContainerInfo{}
	for _, s := range sections {
		if s.Kind == "docker" && s.Where == "host" {
			containers = parseDockerContainers(s.Body)
		}
	}

	for _, s := range sections {
		switch s.Kind {
		case "nginx":
			method, errText := webSectionMethod(s.Body)
			sites := parseNginxConfig(s.Body, s.Where, s.Container)
			p := store.WebProxy{Kind: "nginx", Where: s.Where, Container: s.Container, Version: hostVersion("nginx", s.Where), Method: method, Error: errText}
			addProxy(p)
			scan.Sites = append(scan.Sites, sites...)
			if errText != "" {
				scan.Warnings = append(scan.Warnings, "nginx ("+s.Where+containerLabel(s.Container)+"): "+errText)
			}
		case "caddy":
			method, errText := webSectionMethod(s.Body)
			sites := parseCaddyConfig(s.Body, s.Where, s.Container, method)
			p := store.WebProxy{Kind: "caddy", Where: s.Where, Container: s.Container, Version: hostVersion("caddy", s.Where), Method: method, Error: errText}
			addProxy(p)
			scan.Sites = append(scan.Sites, sites...)
			if errText != "" {
				scan.Warnings = append(scan.Warnings, "caddy ("+s.Where+containerLabel(s.Container)+"): "+errText)
			}
		case "traefik":
			sites, errText := parseTraefikAPI(s.Body, s.Where)
			if len(sites) > 0 {
				addProxy(store.WebProxy{Kind: "traefik", Where: s.Where, Method: "traefik API", Version: versions["traefik"]})
				scan.Sites = append(scan.Sites, sites...)
			}
			if errText != "" {
				scan.Warnings = append(scan.Warnings, "traefik: "+errText)
			}
		}
	}

	// label based sources (Traefik, caddy-docker-proxy, nginx-proxy)
	labelSites, labelProxies, labelWarnings := parseContainerLabels(containers)
	scan.Sites = append(scan.Sites, labelSites...)
	for _, p := range labelProxies {
		addProxy(p)
	}
	scan.Warnings = append(scan.Warnings, labelWarnings...)

	scan.Sites = dedupeWebSites(scan.Sites)
	sortWebSites(scan.Sites)
	return scan
}

// webSectionMethod reads the METHOD/ERROR header lines of a section.
func webSectionMethod(body string) (method string, errText string) {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "METHOD "):
			if method == "" {
				method = strings.TrimSpace(strings.TrimPrefix(line, "METHOD "))
			}
		case strings.HasPrefix(line, "ERROR "):
			if errText == "" {
				errText = strings.TrimSpace(strings.TrimPrefix(line, "ERROR "))
			}
		}
	}
	return method, errText
}

func containerLabel(container string) string {
	if container == "" {
		return "host"
	}
	return "container " + container
}

// dedupeWebSites removes the identical rows (a domain can be listed twice when
// it comes from several sources of the same proxy).
func dedupeWebSites(sites []store.WebSite) []store.WebSite {
	seen := map[string]bool{}
	out := make([]store.WebSite, 0, len(sites))
	for _, s := range sites {
		k := s.Domain + "|" + s.Proxy + "|" + s.Where + "|" + s.Container + "|" + s.Source
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	return out
}

func sortWebSites(sites []store.WebSite) {
	sort.SliceStable(sites, func(i, j int) bool {
		a, b := sites[i], sites[j]
		if a.Domain != b.Domain {
			return a.Domain < b.Domain
		}
		if a.Proxy != b.Proxy {
			return a.Proxy < b.Proxy
		}
		return a.Container < b.Container
	})
}
