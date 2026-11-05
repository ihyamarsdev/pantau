package ansible

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Cron is a parsed standard 5-field cron expression (minute hour day-of-month month day-of-week).
// Supported: `*`, `a`, `a-b`, `*/n`, `a-b/n`, `a/n`, comma lists, and @hourly/@daily/@midnight/@weekly/@monthly/@yearly.
// shortcut: no month/weekday names and no `?`/`L`/`W`; add them if operators ask.
type Cron struct {
	min, hour, dom, mon, dow uint64
	domStar, dowStar         bool
}

var cronAliases = map[string]string{
	"@hourly":   "0 * * * *",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@weekly":   "0 0 * * 0",
	"@monthly":  "0 0 1 * *",
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
}

func ParseCron(expr string) (*Cron, error) {
	expr = strings.TrimSpace(expr)
	if a, ok := cronAliases[expr]; ok {
		expr = a
	}
	f := strings.Fields(expr)
	if len(f) != 5 {
		return nil, fmt.Errorf("cron expression must have 5 fields (minute hour day-of-month month day-of-week), got %d", len(f))
	}
	var c Cron
	var err error
	if c.min, _, err = parseCronField(f[0], 0, 59); err != nil {
		return nil, fmt.Errorf("minute: %w", err)
	}
	if c.hour, _, err = parseCronField(f[1], 0, 23); err != nil {
		return nil, fmt.Errorf("hour: %w", err)
	}
	if c.dom, c.domStar, err = parseCronField(f[2], 1, 31); err != nil {
		return nil, fmt.Errorf("day-of-month: %w", err)
	}
	if c.mon, _, err = parseCronField(f[3], 1, 12); err != nil {
		return nil, fmt.Errorf("month: %w", err)
	}
	if c.dow, c.dowStar, err = parseCronField(f[4], 0, 7); err != nil {
		return nil, fmt.Errorf("day-of-week: %w", err)
	}
	if c.dow&(1<<7) != 0 { // 7 == Sunday
		c.dow |= 1
	}
	return &c, nil
}

func parseCronField(s string, lo, hi int) (mask uint64, star bool, err error) {
	for _, item := range strings.Split(s, ",") {
		step, hasStep := 1, false
		if base, st, ok := strings.Cut(item, "/"); ok {
			n, e := strconv.Atoi(st)
			if e != nil || n <= 0 {
				return 0, false, fmt.Errorf("invalid step %q", st)
			}
			step, hasStep, item = n, true, base
		}
		if item == "*" {
			star = true
		}
		a, b := lo, hi
		switch {
		case item == "*":
		case strings.Contains(item, "-"):
			x, y, _ := strings.Cut(item, "-")
			var e1, e2 error
			a, e1 = strconv.Atoi(x)
			b, e2 = strconv.Atoi(y)
			if e1 != nil || e2 != nil {
				return 0, false, fmt.Errorf("invalid range %q", item)
			}
		default:
			v, e := strconv.Atoi(item)
			if e != nil {
				return 0, false, fmt.Errorf("invalid value %q", item)
			}
			a = v
			if !hasStep { // "a/n" runs from a to the field maximum
				b = v
			}
		}
		if a < lo || b > hi || a > b {
			return 0, false, fmt.Errorf("%q out of range %d-%d", item, lo, hi)
		}
		for v := a; v <= b; v += step {
			mask |= 1 << uint(v)
		}
	}
	return mask, star, nil
}

func (c *Cron) dayMatch(t time.Time) bool {
	domOK := c.dom&(1<<uint(t.Day())) != 0
	dowOK := c.dow&(1<<uint(t.Weekday())) != 0
	if c.domStar || c.dowStar { // Vixie cron: only when both are restricted do they OR
		return domOK && dowOK
	}
	return domOK || dowOK
}

// Next returns the first matching minute strictly after `after` in after's location, or the zero Time if none
// exists within 5 years (e.g. "0 0 31 2 *").
func (c *Cron) Next(after time.Time) time.Time {
	loc := after.Location()
	t := after.Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(5, 0, 0)
	for t.Before(limit) {
		switch {
		case c.mon&(1<<uint(t.Month())) == 0:
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, loc)
		case !c.dayMatch(t):
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, loc)
		case c.hour&(1<<uint(t.Hour())) == 0:
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, loc)
		case c.min&(1<<uint(t.Minute())) == 0:
			t = t.Add(time.Minute)
		default:
			return t
		}
	}
	return time.Time{}
}
