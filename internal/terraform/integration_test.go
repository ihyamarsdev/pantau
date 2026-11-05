package terraform

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestIntegrationPlanApply(t *testing.T) {
	binaries := []string{"terraform", "tofu"}

	for _, bin := range binaries {
		t.Run(bin, func(t *testing.T) {
			if _, err := exec.LookPath(bin); err != nil {
				t.Skipf("%s not in PATH, skipping integration test", bin)
			}

			tmpRoot := t.TempDir()
			dataRoot := filepath.Join(tmpRoot, "data-root")

			files := map[string]string{
				"main.tf": `
terraform {
  required_version = ">= 1.0"
}

resource "terraform_data" "example" {
  input = var.my_input
}

output "out_val" {
  value = terraform_data.example.output
}
`,
				"variables.tf": `
variable "my_input" {
  type    = string
  default = "default_val"
}
`,
			}

			cfg := Config{
				Binary:   bin,
				DataRoot: dataRoot,
				Root:     filepath.Join(dataRoot, "ws-1"),
				Files:    files,
				Vars: []Var{
					{Key: "my_input", Value: "custom_val"},
				},
			}

			// 1. Initial Plan (Run 1)
			var planLog bytes.Buffer
			planRes, err := Plan(context.Background(), PlanSpec{
				Config:  cfg,
				RunID:   1,
				Mode:    "plan",
				Timeout: 1 * time.Minute,
			}, &planLog)
			if err != nil {
				t.Fatalf("Plan failed: %v\nLog: %s", err, planLog.String())
			}
			if !planRes.HasChanges {
				t.Fatalf("expected HasChanges=true on initial plan\nLog: %s", planLog.String())
			}
			if planRes.Summary.Create != 1 {
				t.Errorf("expected 1 create, got %d", planRes.Summary.Create)
			}
			if len(planRes.PlanJSON) == 0 {
				t.Errorf("expected non-empty PlanJSON")
			}

			// 2. Apply (Run 1)
			var applyLog bytes.Buffer
			applyRes, err := Apply(context.Background(), ApplySpec{
				Config:  cfg,
				RunID:   1,
				Timeout: 1 * time.Minute,
			}, &applyLog)
			if err != nil {
				t.Fatalf("Apply failed: %v\nLog: %s", err, applyLog.String())
			}
			if applyRes.ExitCode != 0 {
				t.Fatalf("Apply exit code %d\nLog: %s", applyRes.ExitCode, applyLog.String())
			}
			if len(applyRes.State) == 0 {
				t.Fatalf("expected non-empty state after apply")
			}

			// Check state serial and outputs
			stInfo, err := ParseState(applyRes.State)
			if err != nil {
				t.Fatalf("ParseState failed: %v", err)
			}
			if stInfo.Serial < 1 {
				t.Errorf("expected state serial >= 1, got %d", stInfo.Serial)
			}
			foundOutput := false
			for _, o := range applyRes.Outputs {
				if o.Name == "out_val" {
					foundOutput = true
					if string(o.Value) != `"custom_val"` {
						t.Errorf("expected out_val=\"custom_val\", got %s", string(o.Value))
					}
				}
			}
			if !foundOutput {
				t.Errorf("output out_val not found in apply result")
			}

			// 3. Second Plan (Run 2) with State supplied -> expect no changes
			cfg.State = applyRes.State
			var plan2Log bytes.Buffer
			plan2Res, err := Plan(context.Background(), PlanSpec{
				Config:  cfg,
				RunID:   2,
				Mode:    "plan",
				Timeout: 1 * time.Minute,
			}, &plan2Log)
			if err != nil {
				t.Fatalf("Second Plan failed: %v\nLog: %s", err, plan2Log.String())
			}
			if plan2Res.HasChanges {
				t.Errorf("expected HasChanges=false on second plan, got true (create=%d, update=%d, delete=%d)\nLog: %s",
					plan2Res.Summary.Create, plan2Res.Summary.Update, plan2Res.Summary.Delete, plan2Log.String())
			}
			if plan2Res.ExitCode != 0 {
				t.Errorf("expected exit code 0 for no changes, got %d", plan2Res.ExitCode)
			}

			// 4. Refresh / drift mode run (Run 3)
			var plan3Log bytes.Buffer
			plan3Res, err := Plan(context.Background(), PlanSpec{
				Config:  cfg,
				RunID:   3,
				Mode:    "refresh",
				Timeout: 1 * time.Minute,
			}, &plan3Log)
			if err != nil {
				t.Fatalf("Refresh Plan failed: %v\nLog: %s", err, plan3Log.String())
			}
			if plan3Res.ExitCode != 0 {
				t.Errorf("expected refresh exit code 0 when in sync, got %d\nLog: %s", plan3Res.ExitCode, plan3Log.String())
			}
		})
	}
}
