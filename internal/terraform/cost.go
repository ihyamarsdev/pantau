package terraform

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

type CostEstimate struct {
	Available     bool    `json:"available"`
	Error         string  `json:"error,omitempty"`
	Currency      string  `json:"currency,omitempty"`
	MonthlyBefore float64 `json:"monthly_before"`
	MonthlyAfter  float64 `json:"monthly_after"`
	Delta         float64 `json:"delta"`
	DeltaPct      float64 `json:"delta_pct"`
}

// EstimateCost estimates monthly cost change using infracost breakdown --path <plan.json> --format json.
// If infracost is not installed, returns Available=false, Error="infracost not installed".
func EstimateCost(ctx context.Context, planJSON []byte, env map[string]string) CostEstimate {
	p, _, err := Infracost()
	if err != nil {
		return CostEstimate{
			Available: false,
			Error:     "infracost not installed",
		}
	}

	if len(planJSON) == 0 {
		return CostEstimate{
			Available: true,
			Currency:  "USD",
		}
	}

	tmpFile, err := os.CreateTemp("", "pantau-infracost-*.json")
	if err != nil {
		return CostEstimate{
			Available: false,
			Error:     fmt.Sprintf("failed to create temp file for infracost: %v", err),
		}
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(planJSON); err != nil {
		_ = tmpFile.Close()
		return CostEstimate{
			Available: false,
			Error:     fmt.Sprintf("failed to write temp file for infracost: %v", err),
		}
	}
	_ = tmpFile.Close()

	cmd := exec.CommandContext(ctx, p, "breakdown", "--path", tmpFile.Name(), "--format", "json")
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	out, err := cmd.Output()
	if err != nil {
		return CostEstimate{
			Available: false,
			Error:     fmt.Sprintf("infracost breakdown failed: %v", err),
		}
	}

	var raw struct {
		Currency             string `json:"currency"`
		TotalMonthlyCost     string `json:"totalMonthlyCost"`
		PastTotalMonthlyCost string `json:"pastTotalMonthlyCost"`
		DiffTotalMonthlyCost string `json:"diffTotalMonthlyCost"`
	}

	if err := json.Unmarshal(out, &raw); err != nil {
		return CostEstimate{
			Available: false,
			Error:     fmt.Sprintf("failed to parse infracost json: %v", err),
		}
	}

	after, _ := strconv.ParseFloat(raw.TotalMonthlyCost, 64)
	before, _ := strconv.ParseFloat(raw.PastTotalMonthlyCost, 64)
	delta, _ := strconv.ParseFloat(raw.DiffTotalMonthlyCost, 64)

	var deltaPct float64
	if before > 0 {
		deltaPct = (delta / before) * 100.0
	}

	currency := raw.Currency
	if currency == "" {
		currency = "USD"
	}

	return CostEstimate{
		Available:     true,
		Currency:      currency,
		MonthlyBefore: before,
		MonthlyAfter:  after,
		Delta:         delta,
		DeltaPct:      deltaPct,
	}
}
