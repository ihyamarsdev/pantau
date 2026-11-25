package web

import (
	"os"
	"strings"
	"testing"
)

// TestTerraformSectionIsMountedByBundle enforces the revamped shell contract from
// .scratch/terraform-ui-redesign/spec.md (mirrors TestAnsibleSectionIsMountedByBundle):
// #sectionTerraform in index.html is a clean mount point and the legacy core/12-terraform.js
// plus inline markup/dialogs are removed.
func TestTerraformSectionIsMountedByBundle(t *testing.T) {
	html := string(embeddedHTML)
	fullUI := uiSourceForTest(t)

	for _, want := range []string{
		`id="sectionTerraform"`,
		`/app/terraform.css`,
		`/app/terraform.js`,
		`id="navItemTerraform"`,
		`switchSideNav('terraform')`,
		`window.Tf`,
		`"nav_terraform": "Terraform"`,
	} {
		if !strings.Contains(fullUI, want) {
			t.Errorf("UI source missing %q", want)
		}
	}

	// Legacy inline markup, stacked modal dialogs, and core/12-terraform.js globals must be removed.
	for _, gone := range []string{
		`id="tfTabs"`,
		`id="tfPanelWorkspaces"`,
		`id="tfDlg1"`,
		`id="tfDlg2"`,
		`id="tfWsModal"`,
		`function openTfWorkspaceModal`,
		`function tfSwitchTabGuarded`,
		`"tfTabRenderers": {}`,
		`"tfDirtyGuard": null`,
		`TF_TXT`,
	} {
		if strings.Contains(html, gone) || strings.Contains(fullUI, gone) {
			t.Errorf("UI still contains removed legacy Terraform marker %q", gone)
		}
	}

	if _, err := os.Stat("static/app/core/12-terraform.js"); !os.IsNotExist(err) {
		t.Errorf("legacy static/app/core/12-terraform.js must be removed; all modules belong under static/app/terraform/")
	}
}
