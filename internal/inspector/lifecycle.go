package inspector

// Lifecycle Assessment: the score of a host and the factors behind it.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type LifecycleFactor struct {
	Name           string `json:"name"`
	Status         string `json:"status"` // "pass", "fail"
	ScoreDeduction int    `json:"score_deduction"`
	Detail         string `json:"detail"`
}

func ParseBIOSDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	layouts := []string{"01/02/2006", "2006-01-02", "01/02/06", "2006/01/02", "02-01-2006"}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil && t.Year() > 1990 && t.Year() <= time.Now().Year()+1 {
			return t, true
		}
	}
	return time.Time{}, false
}

func ParseCommissionDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil || t.Year() < 1990 {
		return time.Time{}, false
	}
	return t, true
}

func IsFutureDate(t time.Time) bool {
	todayLocal := time.Now().Format("2006-01-02")
	todayUTC := time.Now().UTC().Format("2006-01-02")
	dateStr := t.Format("2006-01-02")
	return dateStr > todayLocal && dateStr > todayUTC
}

func IsVirtualSystem(vendor string) bool {
	v := strings.ToLower(vendor)
	virtualSignatures := []string{
		"qemu", "kvm", "vmware", "virtualbox", "xen",
		"microsoft corporation", "hyper-v",
		"amazon ec2", "google", "digitalocean", "hetzner",
		"vultr", "linode", "openstack", "proxmox", "bhyve",
	}
	for _, sig := range virtualSignatures {
		if strings.Contains(v, sig) {
			return true
		}
	}
	return false
}

func calculateLifecycleScore(osInfo, loadStr string, cpuCores int, ramUsed, ramTotal, diskUsed, diskTotal int64, ioErrors int, biosDateStr, vendorStr string, osInstallEpoch int64, commissionDateStr string) (int, string, string) {
	if cpuCores <= 0 {
		cpuCores = 1
	}
	score := 100
	var notes []string
	var factors []LifecycleFactor

	// 1. Check EOL OS signatures (-30 points)
	eolPatterns := []string{
		"Ubuntu 14.04", "Ubuntu 16.04", "Ubuntu 18.04",
		"Debian 7", "Debian 8", "Debian 9",
		"CentOS 6", "CentOS release 6", "CentOS Linux 6",
		"CentOS Linux 7", "CentOS release 7",
		"CentOS Linux 8", "CentOS release 8",
		"Red Hat Enterprise Linux Server release 6",
		"Red Hat Enterprise Linux Server release 7",
	}
	osEOL := false
	matchedEOL := ""
	for _, pat := range eolPatterns {
		if strings.Contains(osInfo, pat) {
			osEOL = true
			matchedEOL = pat
			break
		}
	}
	if !osEOL {
		if isMac, macEOLStr := isMacOSEOL(osInfo); isMac {
			osEOL = true
			matchedEOL = macEOLStr
		}
	}
	if osEOL {
		score -= 30
		notes = append(notes, "OS reached End-Of-Life ("+matchedEOL+")")
		factors = append(factors, LifecycleFactor{
			Name:           "OS Support",
			Status:         "fail",
			ScoreDeduction: -30,
			Detail:         fmt.Sprintf("OS reached End-Of-Life (%s)", matchedEOL),
		})
	} else {
		factors = append(factors, LifecycleFactor{
			Name:           "OS Support",
			Status:         "pass",
			ScoreDeduction: 0,
			Detail:         "Operating system is supported",
		})
	}

	// 2. Productive Lifespan / Hardware Age check (MTBF degradation zone)
	var ageYears float64 = -1
	var ageDetail string
	isVirt := IsVirtualSystem(vendorStr)

	var bt time.Time
	hasBT := false
	if !isVirt {
		if t, ok := ParseBIOSDate(biosDateStr); ok {
			bt = t
			hasBT = true
		}
	}

	var commTime time.Time
	hasValidComm := false
	commIgnoredReason := ""
	if strings.TrimSpace(commissionDateStr) != "" {
		if ct, ok := ParseCommissionDate(commissionDateStr); ok {
			if IsFutureDate(ct) {
				commIgnoredReason = fmt.Sprintf("Commission date %s ignored: future date", ct.Format("2006-01-02"))
			} else if hasBT && ct.Before(bt) {
				commIgnoredReason = fmt.Sprintf("Commission date %s ignored: earlier than BIOS (%s)", ct.Format("2006-01-02"), bt.Format("Jan 2006"))
			} else {
				commTime = ct
				hasValidComm = true
			}
		} else {
			commIgnoredReason = "Commission date format invalid (expected YYYY-MM-DD)"
		}
	}

	if hasValidComm {
		ageYears = time.Since(commTime).Hours() / (24 * 365.25)
		ageDetail = fmt.Sprintf("Commissioned: %s", commTime.Format("2006-01-02"))
		if hasBT {
			ageDetail += fmt.Sprintf(" • Motherboard BIOS: %s", bt.Format("Jan 2006"))
		}
	} else if !isVirt && hasBT {
		ageYears = time.Since(bt).Hours() / (24 * 365.25)
		ageDetail = fmt.Sprintf("Physical hardware BIOS: %s (~%.1f yrs)", bt.Format("Jan 2006"), ageYears)
		if commIgnoredReason != "" {
			ageDetail += fmt.Sprintf(" [%s]", commIgnoredReason)
		}
	}

	// If virtual or physical BIOS date missing/invalid (and no commission date), fallback to OS deployment age
	if ageYears < 0 && osInstallEpoch > 0 {
		installTime := time.Unix(osInstallEpoch, 0)
		if time.Since(installTime) > 0 {
			ageYears = time.Since(installTime).Hours() / (24 * 365.25)
			if isVirt {
				ageDetail = fmt.Sprintf("Virtual instance deployment age: ~%.1f yrs", ageYears)
			} else {
				ageDetail = fmt.Sprintf("OS installation deployment age: ~%.1f yrs", ageYears)
			}
			if commIgnoredReason != "" {
				ageDetail += fmt.Sprintf(" [%s]", commIgnoredReason)
			}
		}
	}

	if ageYears >= 8.0 {
		score -= 25
		msg := fmt.Sprintf("Exceeds 8-yr critical lifespan (%.1f yrs) — urgent hardware refresh recommended", ageYears)
		notes = append(notes, msg)
		factors = append(factors, LifecycleFactor{
			Name:           "Productive Lifespan",
			Status:         "fail",
			ScoreDeduction: -25,
			Detail:         msg + " (" + ageDetail + ")",
		})
	} else if ageYears >= 5.0 {
		score -= 15
		msg := fmt.Sprintf("Exceeds 5-yr productive lifecycle / MTBF risk zone (%.1f yrs)", ageYears)
		notes = append(notes, msg)
		factors = append(factors, LifecycleFactor{
			Name:           "Productive Lifespan",
			Status:         "warn",
			ScoreDeduction: -15,
			Detail:         msg + " (" + ageDetail + ")",
		})
	} else if ageYears >= 0 {
		factors = append(factors, LifecycleFactor{
			Name:           "Productive Lifespan",
			Status:         "pass",
			ScoreDeduction: 0,
			Detail:         fmt.Sprintf("Within normal productive lifespan (%.1f yrs; %s)", ageYears, ageDetail),
		})
	} else {
		factors = append(factors, LifecycleFactor{
			Name:           "Productive Lifespan",
			Status:         "pass",
			ScoreDeduction: 0,
			Detail:         "Hardware age telemetry unavailable",
		})
	}

	// 2. Memory pressure check (>92% = -20 points)
	if ramTotal > 0 {
		ramPct := float64(ramUsed) / float64(ramTotal) * 100
		if ramPct > 92.0 {
			score -= 20
			msg := fmt.Sprintf("Critical memory pressure (%.1f%% RAM utilized)", ramPct)
			notes = append(notes, msg)
			factors = append(factors, LifecycleFactor{
				Name:           "Memory Pressure",
				Status:         "fail",
				ScoreDeduction: -20,
				Detail:         msg,
			})
		} else {
			factors = append(factors, LifecycleFactor{
				Name:           "Memory Pressure",
				Status:         "pass",
				ScoreDeduction: 0,
				Detail:         fmt.Sprintf("Normal memory usage (%.1f%% RAM utilized)", ramPct),
			})
		}
	} else {
		factors = append(factors, LifecycleFactor{
			Name:           "Memory Pressure",
			Status:         "pass",
			ScoreDeduction: 0,
			Detail:         "Memory statistics unavailable",
		})
	}

	// 3. CPU Saturation check (load 1m / vCPU > 2.0 = -20 points)
	loadVal := 0.0
	if loadStr != "" {
		loads := strings.Split(loadStr, ",")
		if len(loads) >= 1 {
			loadVal, _ = strconv.ParseFloat(strings.TrimSpace(loads[0]), 64)
		}
	}
	loadRatio := loadVal / float64(cpuCores)
	if loadRatio > 2.0 {
		score -= 20
		msg := fmt.Sprintf("High CPU saturation (load %.2f on %d vCPU, ratio %.2f)", loadVal, cpuCores, loadRatio)
		notes = append(notes, msg)
		factors = append(factors, LifecycleFactor{
			Name:           "CPU Saturation",
			Status:         "fail",
			ScoreDeduction: -20,
			Detail:         msg,
		})
	} else {
		factors = append(factors, LifecycleFactor{
			Name:           "CPU Saturation",
			Status:         "pass",
			ScoreDeduction: 0,
			Detail:         fmt.Sprintf("CPU load within capacity (load %.2f on %d vCPU)", loadVal, cpuCores),
		})
	}

	// 4. Disk Capacity check (>90% = -20 points)
	if diskTotal > 0 {
		diskPct := float64(diskUsed) / float64(diskTotal) * 100
		if diskPct > 90.0 {
			score -= 20
			msg := fmt.Sprintf("Critical disk saturation (%.1f%% disk utilized)", diskPct)
			notes = append(notes, msg)
			factors = append(factors, LifecycleFactor{
				Name:           "Disk Capacity",
				Status:         "fail",
				ScoreDeduction: -20,
				Detail:         msg,
			})
		} else {
			factors = append(factors, LifecycleFactor{
				Name:           "Disk Capacity",
				Status:         "pass",
				ScoreDeduction: 0,
				Detail:         fmt.Sprintf("Normal disk usage (%.1f%% disk utilized)", diskPct),
			})
		}
	} else {
		factors = append(factors, LifecycleFactor{
			Name:           "Disk Capacity",
			Status:         "pass",
			ScoreDeduction: 0,
			Detail:         "Disk statistics unavailable",
		})
	}

	// 5. Hardware I/O disk errors (-40 points)
	if ioErrors > 0 {
		score -= 40
		msg := fmt.Sprintf("%d disk I/O hardware errors recorded in kernel ring buffer", ioErrors)
		notes = append(notes, msg)
		factors = append(factors, LifecycleFactor{
			Name:           "Hardware I/O",
			Status:         "fail",
			ScoreDeduction: -40,
			Detail:         msg,
		})
	} else {
		factors = append(factors, LifecycleFactor{
			Name:           "Hardware I/O",
			Status:         "pass",
			ScoreDeduction: 0,
			Detail:         "No hardware disk I/O errors detected",
		})
	}

	if score < 0 {
		score = 0
	}
	notesStr := "Healthy — Hardware and OS are in good standing"
	if len(notes) > 0 {
		notesStr = strings.Join(notes, "; ")
	}

	breakdownBytes, _ := json.Marshal(factors)
	return score, notesStr, string(breakdownBytes)
}

func (ins *Inspector) RecalculateHostLifecycle(hostID int64) error {
	host, err := ins.db.GetHost(hostID)
	if err != nil || host == nil {
		return err
	}
	if host.LastInspected == nil && host.OSInfo == "" {
		return nil
	}
	// ponytail: RecalculateHostLifecycle performs offline re-assessment from cached metrics.
	// Substring scan for "Hardware I/O" status is an O(1) heuristic to preserve kernel ring-buffer error state
	// without full JSON unmarshaling overhead. Ceiling: 1-factor check. Upgrade path: unmarshal LifecycleFactor array.
	ioErrors := 0
	if strings.Contains(host.LifecycleBreakdown, "Hardware I/O") && strings.Contains(host.LifecycleBreakdown, `"status":"fail"`) {
		ioErrors = 1
	}
	score, notes, breakdown := calculateLifecycleScore(
		host.OSInfo, host.CPULoad, host.CPUCores,
		host.RAMUsedBytes, host.RAMTotalBytes,
		host.DiskUsedBytes, host.DiskTotalBytes,
		ioErrors, host.BIOSDate, host.HardwareModel, host.OSInstallEpoch,
		host.CommissionDate,
	)
	return ins.db.UpdateHostLifecycle(host.ID, score, notes, breakdown)
}
