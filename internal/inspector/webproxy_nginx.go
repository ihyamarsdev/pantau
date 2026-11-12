package inspector

import (
	"strconv"
	"strings"

	"pantau/internal/store"
)

// nginx parser: reads the output of `nginx -T` (or the readable config files
// dumped with the same "# configuration file <path>:" markers) and returns one
// site row per server_name.
//
// The tokenizer understands comments, single/double quotes, escapes, braces and
// semicolons, so a `;` or `{` inside a string never breaks the structure.

const (
	ngTokWord = iota
	ngTokLBrace
	ngTokRBrace
	ngTokSemi
)

type nginxToken struct {
	kind  int
	value string
	line  int
}

type nginxNode struct {
	name string
	args []string
	kids []*nginxNode
	line int
}

// nginxFile is one configuration file of the dump.
type nginxFile struct {
	path string
	body string
}

// nginxServerRef is one server block with the file it came from.
type nginxServerRef struct {
	node *nginxNode
	file string
}

// splitNginxFiles splits an nginx dump into its files, following the
// "# configuration file <path>:" markers.
func splitNginxFiles(out string) []nginxFile {
	var files []nginxFile
	var cur *nginxFile
	var body strings.Builder
	var all strings.Builder
	flush := func() {
		if cur != nil {
			cur.body = body.String()
			files = append(files, *cur)
			cur = nil
		}
		body.Reset()
	}
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "METHOD ") || strings.HasPrefix(trimmed, "ERROR ") {
			continue // the section header, not a directive
		}
		if strings.HasPrefix(trimmed, "# configuration file ") {
			flush()
			path := strings.TrimSpace(strings.TrimPrefix(trimmed, "# configuration file "))
			path = strings.TrimSuffix(path, ":")
			cur = &nginxFile{path: path}
			continue
		}
		body.WriteString(line)
		body.WriteString("\n")
		all.WriteString(line)
		all.WriteString("\n")
	}
	flush()
	if len(files) == 0 {
		// No "# configuration file" marker: the whole dump is one config.
		files = append(files, nginxFile{path: "", body: all.String()})
	}
	return files
}

// tokenizeNginx turns a config file into tokens, dropping comments.
func tokenizeNginx(body string) []nginxToken {
	var tokens []nginxToken
	line := 1
	runes := []rune(body)
	for i := 0; i < len(runes); {
		c := runes[i]
		switch {
		case c == '\n':
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case c == '#':
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
		case c == '"' || c == '\'':
			quote := c
			i++
			var sb strings.Builder
			for i < len(runes) {
				if runes[i] == '\\' && i+1 < len(runes) {
					sb.WriteRune(runes[i+1])
					i += 2
					continue
				}
				if runes[i] == quote {
					i++
					break
				}
				if runes[i] == '\n' {
					line++
				}
				sb.WriteRune(runes[i])
				i++
			}
			tokens = append(tokens, nginxToken{kind: ngTokWord, value: sb.String(), line: line})
		case c == '{':
			tokens = append(tokens, nginxToken{kind: ngTokLBrace, line: line})
			i++
		case c == '}':
			tokens = append(tokens, nginxToken{kind: ngTokRBrace, line: line})
			i++
		case c == ';':
			tokens = append(tokens, nginxToken{kind: ngTokSemi, line: line})
			i++
		default:
			start := i
			for i < len(runes) {
				r := runes[i]
				if r == ' ' || r == '\t' || r == '\r' || r == '\n' || r == ';' || r == '{' || r == '}' || r == '#' || r == '"' || r == '\'' {
					break
				}
				i++
			}
			tokens = append(tokens, nginxToken{kind: ngTokWord, value: string(runes[start:i]), line: line})
		}
	}
	return tokens
}

// parseNginxNodes builds the directive tree of a token stream.
func parseNginxNodes(tokens []nginxToken) []*nginxNode {
	var roots []*nginxNode
	var stack []*nginxNode
	add := func(n *nginxNode) {
		if len(stack) == 0 {
			roots = append(roots, n)
			return
		}
		top := stack[len(stack)-1]
		top.kids = append(top.kids, n)
	}
	i := 0
	for i < len(tokens) {
		t := tokens[i]
		switch t.kind {
		case ngTokWord:
			n := &nginxNode{name: t.value, line: t.line}
			i++
			for i < len(tokens) && tokens[i].kind == ngTokWord {
				n.args = append(n.args, tokens[i].value)
				i++
			}
			if i < len(tokens) && tokens[i].kind == ngTokLBrace {
				add(n)
				i++
				stack = append(stack, n)
				continue
			}
			// A simple directive: skip the terminating semicolon.
			if i < len(tokens) && tokens[i].kind == ngTokSemi {
				i++
			}
			add(n)
		case ngTokLBrace:
			i++ // stray brace
		case ngTokRBrace:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			i++
		case ngTokSemi:
			i++
		}
	}
	return roots
}

// parseNginxConfig returns one site row per server_name of every http server
// block. where is "host" or "container", container the container name.
func parseNginxConfig(out, where, container string) []store.WebSite {
	files := splitNginxFiles(out)
	upstreams := map[string][]string{}
	var servers []nginxServerRef
	for _, f := range files {
		nodes := parseNginxNodes(tokenizeNginx(f.body))
		collectNginxUpstreams(nodes, upstreams)
		collectNginxServers(nodes, f.path, &servers)
	}

	var sites []store.WebSite
	for _, s := range servers {
		sites = append(sites, nginxServerSites(s.node, s.file, where, container, upstreams)...)
	}
	return sites
}

// collectNginxUpstreams records "upstream NAME { server addr; }" targets.
func collectNginxUpstreams(nodes []*nginxNode, upstreams map[string][]string) {
	for _, n := range nodes {
		if n.name == "upstream" && len(n.args) > 0 {
			targets := []string{}
			for _, k := range n.kids {
				if k.name == "server" && len(k.args) > 0 {
					targets = append(targets, k.args[0])
				}
			}
			upstreams[n.args[0]] = targets
			continue
		}
		collectNginxUpstreams(n.kids, upstreams)
	}
}

// collectNginxServers finds the http server blocks: a "server" block that is
// not inside an "upstream" and not inside a "stream" (stream is TCP proxying).
func collectNginxServers(nodes []*nginxNode, file string, out *[]nginxServerRef) {
	for _, n := range nodes {
		switch n.name {
		case "stream", "upstream":
			continue
		case "server":
			*out = append(*out, nginxServerRef{node: n, file: file})
			continue
		}
		collectNginxServers(n.kids, file, out)
	}
}

// nginxServerSites turns one server block into its site rows.
func nginxServerSites(node *nginxNode, file, where, container string, upstreams map[string][]string) []store.WebSite {
	var (
		names   []string
		listens []string
		tls     bool
		root    string
		passes  []string
		sslCert bool
		deflt   bool
	)

	var walk func(n *nginxNode)
	walk = func(n *nginxNode) {
		for _, k := range n.kids {
			switch k.name {
			case "server_name":
				names = append(names, k.args...)
			case "listen":
				if len(k.args) == 0 {
					continue
				}
				listens = append(listens, strings.Join(k.args, " "))
				for _, a := range k.args {
					switch {
					case a == "ssl" || strings.HasPrefix(a, "ssl="):
						tls = true
					case a == "default_server" || a == "default":
						deflt = true
					}
				}
			case "ssl_certificate":
				sslCert = true
			case "ssl":
				if len(k.args) > 0 && k.args[0] == "on" {
					tls = true
				}
			case "root":
				if len(k.args) > 0 && root == "" {
					root = k.args[0]
				}
			case "proxy_pass", "fastcgi_pass", "uwsgi_pass", "grpc_pass":
				if len(k.args) > 0 {
					passes = append(passes, k.args[0])
				}
			}
			walk(k)
		}
	}
	walk(node)

	if sslCert {
		tls = true
	}
	// A block that redirects (return 3xx / rewrite ... permanent) serves no content of its own;
	// its root is only there for ACME challenges, so it must not make it differ from the TLS block.
	if len(passes) == 0 && nginxRedirects(node) {
		root = ""
	}

	source := strconv.Itoa(node.line)
	if file != "" {
		source = file + ":" + source
	}
	var sites []store.WebSite
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || name == "_" {
			continue
		}
		var ups []string
		for _, p := range passes {
			ups = append(ups, resolveNginxUpstream(p, upstreams)...)
		}
		sites = append(sites, store.WebSite{
			Domain:        name,
			Proxy:         "nginx",
			Where:         where,
			Container:     container,
			TLS:           tls,
			Listen:        listens,
			Upstreams:     dedupeStrings(ups),
			Root:          root,
			Source:        source,
			Wildcard:      isWildcardDomain(name),
			Regex:         strings.HasPrefix(name, "~"),
			DefaultServer: deflt,
		})
	}
	return sites
}

// nginxRedirects reports whether a server block redirects its traffic: a
// return 3xx or a permanent/redirect rewrite directly in the block or in "location /".
func nginxRedirects(server *nginxNode) bool {
	isRedirect := func(k *nginxNode) bool {
		switch k.name {
		case "return":
			if len(k.args) > 0 {
				switch k.args[0] {
				case "301", "302", "303", "307", "308":
					return true
				}
			}
		case "rewrite":
			if n := len(k.args); n >= 3 && (k.args[n-1] == "permanent" || k.args[n-1] == "redirect") {
				return true
			}
		}
		return false
	}
	for _, k := range server.kids {
		if isRedirect(k) {
			return true
		}
		if k.name == "location" && len(k.args) == 1 && k.args[0] == "/" {
			for _, kk := range k.kids {
				if isRedirect(kk) {
					return true
				}
			}
		}
	}
	return false
}

// resolveNginxUpstream expands "proxy_pass http://backend" with the servers of
// the matching upstream block.
func resolveNginxUpstream(target string, upstreams map[string][]string) []string {
	scheme := ""
	name := target
	if idx := strings.Index(target, "://"); idx >= 0 {
		scheme = target[:idx+3]
		name = target[idx+3:]
	}
	if idx := strings.IndexAny(name, "/"); idx >= 0 {
		name = name[:idx]
	}
	if strings.Contains(name, "$") {
		return []string{target}
	}
	targets, ok := upstreams[name]
	if !ok {
		return []string{target}
	}
	out := make([]string, 0, len(targets))
	for _, t := range targets {
		out = append(out, scheme+t)
	}
	return out
}

func isWildcardDomain(name string) bool {
	return strings.HasPrefix(name, "*.") || strings.HasPrefix(name, ".")
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
