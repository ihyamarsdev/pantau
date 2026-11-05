package terraform

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"pantau/internal/ansible"
)

type ModuleInput struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Default     string `json:"default"`
	Required    bool   `json:"required"`
	Sensitive   bool   `json:"sensitive"`
}

type ModuleOutput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Sensitive   bool   `json:"sensitive"`
}

type ModuleDoc struct {
	Readme          string         `json:"readme"`
	Inputs          []ModuleInput  `json:"inputs"`
	Outputs         []ModuleOutput `json:"outputs"`
	Resources       []string       `json:"resources"` // "type.name"
	Providers       []string       `json:"providers"`
	RequiredVersion string         `json:"required_version"`
}

// ParseModule scans HCL files for variables, outputs, resources, providers, required_version and README.md.
func ParseModule(files map[string]string) ModuleDoc {
	doc := ModuleDoc{
		Inputs:    make([]ModuleInput, 0),
		Outputs:   make([]ModuleOutput, 0),
		Resources: make([]string, 0),
		Providers: make([]string, 0),
	}

	for name, content := range files {
		lower := strings.ToLower(name)
		if lower == "readme.md" || lower == "readme" {
			doc.Readme = content
			break
		}
	}

	var allBlocks []blockDef
	for name, content := range files {
		if strings.HasSuffix(name, ".tf") {
			blocks := scanHCLBlocks(content)
			allBlocks = append(allBlocks, blocks...)
		}
	}

	resourceSet := make(map[string]bool)
	providerSet := make(map[string]bool)

	for _, b := range allBlocks {
		switch b.Kind {
		case "variable":
			if len(b.Labels) > 0 {
				varName := b.Labels[0]
				inp := parseVariableBlock(varName, b.Body)
				doc.Inputs = append(doc.Inputs, inp)
			}
		case "output":
			if len(b.Labels) > 0 {
				outName := b.Labels[0]
				out := parseOutputBlock(outName, b.Body)
				doc.Outputs = append(doc.Outputs, out)
			}
		case "resource":
			if len(b.Labels) >= 2 {
				rName := fmt.Sprintf("%s.%s", b.Labels[0], b.Labels[1])
				resourceSet[rName] = true
				// Provider from resource type prefix (e.g. aws_s3_bucket -> aws)
				parts := strings.SplitN(b.Labels[0], "_", 2)
				if len(parts) > 0 && parts[0] != "" {
					providerSet[parts[0]] = true
				}
			}
		case "provider":
			if len(b.Labels) > 0 {
				providerSet[b.Labels[0]] = true
			}
		case "terraform":
			ver := parseRequiredVersion(b.Body)
			if ver != "" && doc.RequiredVersion == "" {
				doc.RequiredVersion = ver
			}
		}
	}

	for r := range resourceSet {
		doc.Resources = append(doc.Resources, r)
	}
	sort.Strings(doc.Resources)

	for p := range providerSet {
		doc.Providers = append(doc.Providers, p)
	}
	sort.Strings(doc.Providers)

	sort.Slice(doc.Inputs, func(i, j int) bool {
		return doc.Inputs[i].Name < doc.Inputs[j].Name
	})
	sort.Slice(doc.Outputs, func(i, j int) bool {
		return doc.Outputs[i].Name < doc.Outputs[j].Name
	})

	return doc
}

type blockDef struct {
	Kind   string
	Labels []string
	Body   string
}

// scanHCLBlocks parses top-level HCL blocks taking into account comments, strings, and braces.
func scanHCLBlocks(src string) []blockDef {
	var blocks []blockDef
	n := len(src)
	i := 0

	for i < n {
		i = skipWhitespaceAndComments(src, i)
		if i >= n {
			break
		}

		// Read identifier
		start := i
		for i < n && (isIdentChar(src[i])) {
			i++
		}
		if start == i {
			i++
			continue
		}
		kind := src[start:i]

		// Read labels until '{'
		var labels []string
		for {
			i = skipWhitespaceAndComments(src, i)
			if i >= n || src[i] == '{' {
				break
			}
			if src[i] == '"' {
				strVal, nextI := scanQuotedString(src, i)
				labels = append(labels, strVal)
				i = nextI
			} else if isIdentChar(src[i]) {
				lblStart := i
				for i < n && isIdentChar(src[i]) {
					i++
				}
				labels = append(labels, src[lblStart:i])
			} else {
				i++
			}
		}

		if i < n && src[i] == '{' {
			braceStart := i + 1
			braceEnd := scanBalancedBrace(src, i)
			body := ""
			if braceEnd > braceStart {
				body = src[braceStart:braceEnd]
			}
			blocks = append(blocks, blockDef{
				Kind:   kind,
				Labels: labels,
				Body:   body,
			})
			i = braceEnd + 1
		}
	}

	return blocks
}

func isIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-'
}

func skipWhitespaceAndComments(src string, i int) int {
	n := len(src)
	for i < n {
		c := src[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		if c == '#' {
			for i < n && src[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < n {
			if src[i+1] == '/' {
				for i < n && src[i] != '\n' {
					i++
				}
				continue
			}
			if src[i+1] == '*' {
				i += 2
				for i+1 < n && !(src[i] == '*' && src[i+1] == '/') {
					i++
				}
				i += 2
				continue
			}
		}
		break
	}
	return i
}

func scanQuotedString(src string, start int) (string, int) {
	n := len(src)
	i := start + 1
	var sb strings.Builder
	for i < n {
		if src[i] == '\\' && i+1 < n {
			sb.WriteByte(src[i+1])
			i += 2
			continue
		}
		if src[i] == '"' {
			return sb.String(), i + 1
		}
		sb.WriteByte(src[i])
		i++
	}
	return sb.String(), i
}

func scanBalancedBrace(src string, openIdx int) int {
	n := len(src)
	depth := 0
	i := openIdx

	for i < n {
		c := src[i]
		if c == '"' {
			_, nextI := scanQuotedString(src, i)
			i = nextI
			continue
		}
		if c == '#' {
			for i < n && src[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < n {
			if src[i+1] == '/' {
				for i < n && src[i] != '\n' {
					i++
				}
				continue
			}
			if src[i+1] == '*' {
				i += 2
				for i+1 < n && !(src[i] == '*' && src[i+1] == '/') {
					i++
				}
				i += 2
				continue
			}
		}
		// Heredoc <<EOF or <<-EOF
		if c == '<' && i+2 < n && src[i+1] == '<' {
			hdStart := i
			i += 2
			if i < n && src[i] == '-' {
				i++
			}
			tagStart := i
			for i < n && isIdentChar(src[i]) {
				i++
			}
			tag := src[tagStart:i]
			if tag != "" {
				// find \ntag
				endPattern := "\n" + tag
				idx := strings.Index(src[i:], endPattern)
				if idx != -1 {
					i += idx + len(endPattern)
					continue
				}
			}
			i = hdStart + 2
			continue
		}

		if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				return i
			}
		}
		i++
	}
	return n
}

var (
	typeRe       = regexp.MustCompile(`(?m)^\s*type\s*=\s*([^\n]+)`)
	descRe       = regexp.MustCompile(`(?m)^\s*description\s*=\s*"([^"]*)"`)
	defaultRe    = regexp.MustCompile(`(?m)^\s*default\s*=\s*([^\n]+)`)
	sensitiveRe  = regexp.MustCompile(`(?m)^\s*sensitive\s*=\s*(true|false)`)
	reqVersionRe = regexp.MustCompile(`(?m)^\s*required_version\s*=\s*"([^"]*)"`)
)

func parseVariableBlock(name, body string) ModuleInput {
	inp := ModuleInput{Name: name, Required: true}

	if m := typeRe.FindStringSubmatch(body); len(m) > 1 {
		inp.Type = strings.TrimSpace(m[1])
	}
	if m := descRe.FindStringSubmatch(body); len(m) > 1 {
		inp.Description = m[1]
	}
	if m := defaultRe.FindStringSubmatch(body); len(m) > 1 {
		inp.Default = strings.TrimSpace(m[1])
		inp.Required = false
	}
	if m := sensitiveRe.FindStringSubmatch(body); len(m) > 1 {
		inp.Sensitive = m[1] == "true"
	}
	return inp
}

func parseOutputBlock(name, body string) ModuleOutput {
	out := ModuleOutput{Name: name}

	if m := descRe.FindStringSubmatch(body); len(m) > 1 {
		out.Description = m[1]
	}
	if m := sensitiveRe.FindStringSubmatch(body); len(m) > 1 {
		out.Sensitive = m[1] == "true"
	}
	return out
}

func parseRequiredVersion(body string) string {
	if m := reqVersionRe.FindStringSubmatch(body); len(m) > 1 {
		return m[1]
	}
	return ""
}

// CloneModuleFiles shallow-clones the module repository and collects eligible files (max 50 files, <= 256KiB each).
func CloneModuleFiles(ctx context.Context, g GitSource, ref, subdir string) (map[string]string, error) {
	tempDir, err := os.MkdirTemp("", "pantau-module-clone-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	branch := ref
	if branch == "" {
		branch = g.Branch
	}

	repoDir, _, err := ansible.CloneGitRepo(ctx, g.URL, branch, g.Token, g.PrivateKey, tempDir, "https:ssh", nil)
	if err != nil {
		return nil, fmt.Errorf("git clone failed: %w", err)
	}

	targetDir := repoDir
	if subdir != "" {
		targetDir = filepath.Join(repoDir, filepath.FromSlash(subdir))
	}

	files := make(map[string]string)
	err = filepath.Walk(targetDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(targetDir, p)
		if err != nil {
			return nil
		}
		// Flat or relative name
		name := filepath.ToSlash(rel)
		baseName := filepath.Base(name)

		if !fileNameRe.MatchString(baseName) {
			return nil
		}

		matchedExt := false
		for ext := range allowedExtensions {
			if strings.HasSuffix(baseName, ext) {
				matchedExt = true
				break
			}
		}
		if !matchedExt {
			return nil
		}

		if info.Size() > 256*1024 {
			return nil
		}

		if len(files) >= 50 {
			return nil
		}

		b, err := os.ReadFile(p)
		if err == nil {
			files[name] = string(b)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return files, nil
}
