package iacscan

import (
	"fmt"
	"regexp"
	"strings"
)

// tfBlock is a brace-aware block of a .tf file: its header, its body lines and its start line.
type tfBlock struct {
	header string
	lines  []string
	start  int // 1-based line of the header
}

// parseTFBlocks splits a Terraform file into top-level blocks. The parser is text based: it counts braces
// outside of strings and comments, which is enough for the rules below.
func parseTFBlocks(lines []string) []tfBlock {
	var blocks []tfBlock
	depth := 0
	var cur tfBlock
	inBlock := false
	for i, line := range lines {
		code := stripTFComment(line)
		if !inBlock && depth == 0 {
			if strings.Contains(code, "{") && !strings.Contains(code, "=") {
				cur = tfBlock{header: strings.TrimSpace(code), start: i + 1}
				inBlock = true
			}
		}
		if inBlock {
			cur.lines = append(cur.lines, line)
		}
		depth += strings.Count(code, "{") - strings.Count(code, "}")
		if inBlock && depth <= 0 {
			blocks = append(blocks, cur)
			inBlock = false
			cur = tfBlock{}
			depth = 0
		}
	}
	return blocks
}

// stripTFComment removes a trailing comment and the content of quoted strings from a line.
func stripTFComment(line string) string {
	var b strings.Builder
	inStr := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '"' && (i == 0 || line[i-1] != '\\'):
			inStr = !inStr
			b.WriteByte(c)
		case c == '#' && !inStr:
			return b.String()
		case c == '/' && i+1 < len(line) && line[i+1] == '/' && !inStr:
			return b.String()
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// tfBlockType returns the resource/data/module type of a block header ("aws_instance" of resource "aws_instance" "web").
func tfBlockType(header string) string {
	fields := strings.Fields(header)
	switch {
	case len(fields) >= 2 && (fields[0] == "resource" || fields[0] == "data"):
		return strings.Trim(fields[1], `"`)
	case len(fields) >= 1:
		return strings.Trim(fields[0], `"`)
	}
	return ""
}

// tfBlockName returns the label of a block ("web" of resource "aws_instance" "web").
func tfBlockName(header string) string {
	fields := strings.Fields(header)
	if len(fields) >= 3 && (fields[0] == "resource" || fields[0] == "data") {
		return strings.Trim(fields[2], `"`)
	}
	return ""
}

// tfAttr finds an attribute inside a block body: `name = value` on one line.
func tfAttr(b tfBlock, name string) (string, bool) {
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(name) + `\s*=\s*(.+?)\s*$`)
	for _, line := range b.lines {
		if m := re.FindStringSubmatch(stripTFComment(line)); m != nil {
			return strings.Trim(m[1], `"`), true
		}
	}
	return "", false
}

// tfAttrPresent reports whether the block has the attribute at all.
func tfAttrPresent(b tfBlock, name string) bool {
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(name) + `\s*=`)
	for _, line := range b.lines {
		if re.MatchString(stripTFComment(line)) {
			return true
		}
	}
	return false
}

var tfOpenCIDRs = []string{"0.0.0.0/0", "::/0"}

// tfSensitivePorts are the ports that must never be open to the world.
var tfSensitivePorts = map[string]string{
	"22": "SSH", "3389": "RDP", "3306": "MySQL", "5432": "PostgreSQL",
	"1433": "MSSQL", "27017": "MongoDB", "6379": "Redis", "9200": "Elasticsearch",
	"5672": "AMQP", "2379": "etcd",
}

// scanTerraformLine applies the per-line Terraform rules.
func scanTerraformLine(res *Result, rel string, lineNo int, line string, content string, lines []string) {
	code := stripTFComment(line)
	trimmed := strings.TrimSpace(code)

	// Provider credentials.
	if m := tfCredentialRe.FindStringSubmatch(code); m != nil {
		add(res, Finding{
			RuleID: "terraform.provider-credentials", Category: CategoryTerraform, Severity: SeverityCritical, File: rel, Line: lineNo,
			Title:       "Hard-coded provider credentials",
			Detail:      fmt.Sprintf("The provider block holds a literal %s.", m[1]),
			Remediation: "Remove the credential from the code and pass it with the provider's environment variable.",
			Snippet:     redactLine(trimmed),
		})
	}

	// Insecure settings.
	if tfInsecureRe.MatchString(code) {
		add(res, Finding{
			RuleID: "terraform.insecure-skip-tls", Category: CategoryTerraform, Severity: SeverityHigh, File: rel, Line: lineNo,
			Title:       "TLS verification disabled",
			Detail:      "The provider or resource disables the certificate check.",
			Remediation: "Remove the insecure setting and install the correct CA certificate.",
			Snippet:     trimmed,
		})
	}
	if tfInsecureBoolRe.MatchString(code) {
		add(res, Finding{
			RuleID: "terraform.insecure-flag", Category: CategoryTerraform, Severity: SeverityHigh, File: rel, Line: lineNo,
			Title:       "Insecure flag enabled",
			Detail:      "An insecure flag is set on the resource.",
			Remediation: "Remove the insecure flag and use the secure option.",
			Snippet:     trimmed,
		})
	}

	// IAM policy with Action "*" and Resource "*".
	if tfActionStarRe.MatchString(code) && tfResourceStarRe.MatchString(content) {
		add(res, Finding{
			RuleID: "terraform.iam-wildcard-policy", Category: CategoryTerraform, Severity: SeverityHigh, File: rel, Line: lineNo,
			Title:       "IAM policy allows every action on every resource",
			Detail:      "The policy grants Action \"*\" on Resource \"*\".",
			Remediation: "Restrict the policy to the actions and resources the workload really needs.",
			Snippet:     trimmed,
		})
	}

}

var (
	tfCredentialRe         = regexp.MustCompile(`(?i)^\s*(access_key|secret_key|token|password)\s*=\s*"[^"$]+"`)
	tfInsecureRe           = regexp.MustCompile(`(?i)skip_(tls|ssl)_verify|ignore_tls|verify_ssl\s*=\s*false`)
	tfInsecureBoolRe       = regexp.MustCompile(`(?i)^\s*insecure\s*=\s*true`)
	tfPubliclyAccessibleRe = regexp.MustCompile(`(?i)publicly_accessible\s*=\s*true`)
	tfActionStarRe         = regexp.MustCompile(`(?i)"?(action|actions)"?\s*[:=]\s*\[?\s*"?\*"?`)
	tfResourceStarRe       = regexp.MustCompile(`(?i)"?(resource|resources)"?\s*[:=]\s*\[?\s*"?\*"?`)
)

// tfIsDBResource reports whether the block starting at lineNo is a database resource.
func tfIsDBResource(content string, lineNo int) bool {
	lines := strings.Split(content, "\n")
	for _, b := range parseTFBlocks(lines) {
		if b.start != lineNo {
			continue
		}
		t := tfBlockType(b.header)
		return strings.Contains(t, "db_instance") || strings.Contains(t, "rds_cluster") ||
			strings.Contains(t, "database") || strings.Contains(t, "sql")
	}
	return false
}

// scanTerraformFile applies the block-level Terraform rules.
func scanTerraformFile(res *Result, rel string, content string) {
	lines := strings.Split(content, "\n")
	blocks := parseTFBlocks(lines)
	for _, b := range blocks {
		t := tfBlockType(b.header)
		name := tfBlockName(b.header)
		switch {
		case t == "aws_security_group" || t == "aws_security_group_rule" || t == "google_compute_firewall" ||
			t == "azurerm_network_security_group" || t == "openstack_compute_fw_rule_ingress":
			tfCheckSecurityGroup(res, rel, b, t)
		case t == "aws_s3_bucket":
			tfCheckS3Bucket(res, rel, b, blocks)
		case t == "aws_s3_bucket_public_access_block":
			tfCheckS3PublicAccessBlock(res, rel, b)
		case t == "aws_ebs_volume" || t == "aws_db_instance" || t == "aws_rds_cluster" || t == "google_compute_disk":
			tfCheckEncryption(res, rel, b, t)
			tfCheckPublicDatabase(res, rel, b, t)
		case t == "provider":
			tfCheckProviderVersion(res, rel, b)
		case t == "module":
			tfCheckModuleVersion(res, rel, b)
		}
		_ = name
	}
}

// tfCheckSecurityGroup reports an ingress open to the world.
func tfCheckSecurityGroup(res *Result, rel string, b tfBlock, t string) {
	body := strings.Join(b.lines, "\n")
	if !tfIngressRe.MatchString(body) {
		return
	}
	for _, cidr := range tfOpenCIDRs {
		if !strings.Contains(body, cidr) {
			continue
		}
		ports := tfPortsIn(body)
		severity := SeverityHigh
		detail := fmt.Sprintf("The security group accepts traffic from %s.", cidr)
		for _, p := range ports {
			if name, ok := tfSensitivePorts[p]; ok {
				severity = SeverityCritical
				detail = fmt.Sprintf("The security group exposes %s (%s) to %s.", p, name, cidr)
				break
			}
		}
		if len(ports) == 0 {
			detail = fmt.Sprintf("The security group accepts traffic on all ports from %s.", cidr)
			severity = SeverityCritical
		}
		add(res, Finding{
			RuleID: "terraform.open-security-group", Category: CategoryTerraform, Severity: severity, File: rel, Line: b.start,
			Title:       "Security group open to the internet",
			Detail:      detail,
			Remediation: "Restrict cidr_blocks to the addresses that really need the port.",
			Snippet:     "",
		})
	}
}

// tfPortsIn extracts the port numbers of a security group body.
func tfPortsIn(body string) []string {
	var ports []string
	re := regexp.MustCompile(`(?i)(?:from_port|port)\s*=\s*"?(\d+)"?`)
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		ports = append(ports, m[1])
	}
	return ports
}

var tfIngressRe = regexp.MustCompile(`(?i)ingress|type\s*=\s*"ingress"|direction\s*=\s*"INGRESS"`)

// tfCheckS3Bucket reports a public ACL and a missing versioning.
func tfCheckS3Bucket(res *Result, rel string, b tfBlock, blocks []tfBlock) {
	if acl, ok := tfAttr(b, "acl"); ok {
		switch strings.ToLower(acl) {
		case "public-read", "public-read-write":
			add(res, Finding{
				RuleID: "terraform.s3-public-acl", Category: CategoryTerraform, Severity: SeverityCritical, File: rel, Line: b.start,
				Title:       "S3 bucket is publicly readable",
				Detail:      fmt.Sprintf("The bucket ACL is %q.", acl),
				Remediation: "Use a private ACL and grant access through a bucket policy.",
				Snippet:     fmt.Sprintf("acl = %q", acl),
			})
		}
	}
	// Versioning lives in its own block; a bucket without one loses old object versions.
	for _, other := range blocks {
		if tfBlockType(other.header) != "aws_s3_bucket_versioning" {
			continue
		}
		if tfBlockName(other.header) == tfBlockName(b.header) {
			return
		}
	}
	add(res, Finding{
		RuleID: "terraform.s3-no-versioning", Category: CategoryTerraform, Severity: SeverityLow, File: rel, Line: b.start,
		Title:       "S3 bucket without versioning",
		Detail:      "The bucket has no aws_s3_bucket_versioning block, so deleted objects cannot be recovered.",
		Remediation: "Add an aws_s3_bucket_versioning block with status \"Enabled\".",
		Snippet:     "",
	})
}

// tfCheckS3PublicAccessBlock reports a disabled public access block.
func tfCheckS3PublicAccessBlock(res *Result, rel string, b tfBlock) {
	for _, attr := range []string{"block_public_acls", "block_public_policy", "ignore_public_acls", "restrict_public_buckets"} {
		if v, ok := tfAttr(b, attr); ok && strings.EqualFold(v, "false") {
			add(res, Finding{
				RuleID: "terraform.s3-public-access-disabled", Category: CategoryTerraform, Severity: SeverityHigh, File: rel, Line: b.start,
				Title:       "S3 public access block disabled",
				Detail:      fmt.Sprintf("%s is false, so the bucket can be exposed again.", attr),
				Remediation: "Set every attribute of the public access block to true.",
				Snippet:     fmt.Sprintf("%s = %s", attr, v),
			})
		}
	}
}

// tfCheckEncryption reports an unencrypted volume or database.
func tfCheckEncryption(res *Result, rel string, b tfBlock, t string) {
	if v, ok := tfAttr(b, "storage_encrypted"); ok {
		if strings.EqualFold(v, "false") {
			add(res, Finding{
				RuleID: "terraform.unencrypted-storage", Category: CategoryTerraform, Severity: SeverityHigh, File: rel, Line: b.start,
				Title:       "Storage is not encrypted",
				Detail:      fmt.Sprintf("%s has storage_encrypted = false.", t),
				Remediation: "Set storage_encrypted = true.",
				Snippet:     "storage_encrypted = false",
			})
		}
		return
	}
	if t == "aws_ebs_volume" || t == "google_compute_disk" {
		add(res, Finding{
			RuleID: "terraform.unencrypted-storage", Category: CategoryTerraform, Severity: SeverityMedium, File: rel, Line: b.start,
			Title:       "Storage without an explicit encryption setting",
			Detail:      fmt.Sprintf("%s does not set encrypted / storage_encrypted.", t),
			Remediation: "Set encrypted = true (or storage_encrypted = true) so the intent is explicit.",
			Snippet:     "",
		})
	}
}

// tfCheckPublicDatabase reports a database that is reachable from the internet.
func tfCheckPublicDatabase(res *Result, rel string, b tfBlock, t string) {
	if v, ok := tfAttr(b, "publicly_accessible"); ok && strings.EqualFold(v, "true") {
		add(res, Finding{
			RuleID: "terraform.public-database", Category: CategoryTerraform, Severity: SeverityHigh, File: rel, Line: b.start,
			Title:       "Database is publicly accessible",
			Detail:      fmt.Sprintf("%s is reachable from the internet.", t),
			Remediation: "Set publicly_accessible to false and reach the database through a private subnet.",
			Snippet:     "publicly_accessible = true",
		})
	}
}

// tfCheckProviderVersion reports an unpinned provider version.
func tfCheckProviderVersion(res *Result, rel string, b tfBlock) {
	source, ok := tfAttr(b, "source")
	if !ok {
		return
	}
	version, hasVersion := tfAttr(b, "version")
	if hasVersion && version != "" {
		return
	}
	add(res, Finding{
		RuleID: "terraform.unpinned-provider", Category: CategoryTerraform, Severity: SeverityLow, File: rel, Line: b.start,
		Title:       "Provider version is not pinned",
		Detail:      fmt.Sprintf("The provider %s has no version constraint.", source),
		Remediation: "Add a version constraint so a new provider release cannot change the plan unnoticed.",
		Snippet:     fmt.Sprintf("source = %q", source),
	})
}

// tfCheckModuleVersion reports a module source without a version, or one pinned to a branch.
func tfCheckModuleVersion(res *Result, rel string, b tfBlock) {
	source, ok := tfAttr(b, "source")
	if !ok {
		return
	}
	version, hasVersion := tfAttr(b, "version")
	detail := ""
	switch {
	case !hasVersion || version == "":
		detail = fmt.Sprintf("The module %s has no version constraint.", source)
	case strings.Contains(source, "?ref="):
		detail = fmt.Sprintf("The module %s is pinned to a branch or tag reference.", source)
	default:
		return
	}
	add(res, Finding{
		RuleID: "terraform.unpinned-module", Category: CategoryTerraform, Severity: SeverityLow, File: rel, Line: b.start,
		Title:       "Module version is not pinned",
		Detail:      detail,
		Remediation: "Pin the module to a released version.",
		Snippet:     fmt.Sprintf("source = %q", source),
	})
}
