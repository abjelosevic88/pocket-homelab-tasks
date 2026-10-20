package ical

import (
	"strconv"
	"strings"
	"time"
)

// Rule is a parsed RRULE (the parts a task repeat needs).
type Rule struct {
	Freq       string // DAILY WEEKLY MONTHLY YEARLY
	Interval   int
	Count      int
	Until      string // floating local or day
	ByDay      []weekday
	ByMonthDay []int
	ByMonth    []int
	raw        map[string]string
}

type weekday struct {
	Day time.Weekday
	N   int // 0 = any, -1 last, 2 second…
}

var dayNames = map[string]time.Weekday{"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday, "TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday}

// ParseRule reads "FREQ=WEEKLY;BYDAY=MO,WE" (with or without "RRULE:").
func ParseRule(s string) (*Rule, bool) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(s)), "RRULE:"))
	if s == "" {
		return nil, false
	}
	r := &Rule{Interval: 1, raw: map[string]string{}}
	for _, part := range strings.Split(s, ";") {
		k, v, _ := strings.Cut(part, "=")
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		r.raw[k] = v
		switch k {
		case "FREQ":
			r.Freq = v
		case "INTERVAL":
			r.Interval, _ = strconv.Atoi(v)
			if r.Interval < 1 {
				r.Interval = 1
			}
		case "COUNT":
			r.Count, _ = strconv.Atoi(v)
		case "UNTIL":
			v = strings.TrimSuffix(v, "Z")
			if len(v) == 8 {
				if t, err := time.Parse("20060102", v); err == nil {
					r.Until = t.Format(DayLayout)
				}
			} else if t, err := time.Parse("20060102T150405", v); err == nil {
				r.Until = t.Format(TimeLayout)
			}
		case "BYDAY":
			for _, d := range strings.Split(v, ",") {
				d = strings.TrimSpace(d)
				if len(d) < 2 {
					continue
				}
				name := d[len(d)-2:]
				n, _ := strconv.Atoi(d[:len(d)-2])
				if wd, ok := dayNames[name]; ok {
					r.ByDay = append(r.ByDay, weekday{Day: wd, N: n})
				}
			}
		case "BYMONTHDAY":
			for _, d := range strings.Split(v, ",") {
				if n, err := strconv.Atoi(strings.TrimSpace(d)); err == nil {
					r.ByMonthDay = append(r.ByMonthDay, n)
				}
			}
		case "BYMONTH":
			for _, d := range strings.Split(v, ",") {
				if n, err := strconv.Atoi(strings.TrimSpace(d)); err == nil {
					r.ByMonth = append(r.ByMonth, n)
				}
			}
		}
	}
	switch r.Freq {
	case "DAILY", "WEEKLY", "MONTHLY", "YEARLY":
		return r, true
	}
	return nil, false
}

// String writes the rule back (COUNT adjusted).
func (r *Rule) String() string {
	parts := []string{"FREQ=" + r.Freq}
	if r.Interval > 1 {
		parts = append(parts, "INTERVAL="+strconv.Itoa(r.Interval))
	}
	for k, v := range r.raw {
		switch k {
		case "FREQ", "INTERVAL", "COUNT":
			continue
		}
		parts = append(parts, k+"="+v)
	}
	if r.Count > 0 {
		parts = append(parts, "COUNT="+strconv.Itoa(r.Count))
	}
	// stable order: FREQ, INTERVAL, then the rest alphabetically
	head, rest := parts[:1], parts[1:]
	sortStrings(rest)
	return strings.Join(append(head, rest...), ";")
}

func sortStrings(a []string) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

// Next returns the first occurrence strictly after `after` that matches the
// rule, given the series' anchor `from` (the task's current date). ok is
// false when the rule has ended (UNTIL passed or COUNT spent). The result
// keeps the anchor's time of day.
func (r *Rule) Next(from, after time.Time) (time.Time, bool) {
	if r.Count == 1 {
		return time.Time{}, false
	}
	loc := from.Location()
	cand := from
	// Walk forward in rule units, at most a few thousand steps.
	for i := 0; i < 5000; i++ {
		switch r.Freq {
		case "DAILY":
			cand = cand.AddDate(0, 0, 1)
			if len(r.ByDay) > 0 && !r.matchDay(cand) {
				continue
			}
			if !r.onInterval(from, cand) {
				continue
			}
		case "WEEKLY":
			cand = cand.AddDate(0, 0, 1)
			if len(r.ByDay) > 0 {
				if !r.matchDay(cand) {
					continue
				}
			} else if cand.Weekday() != from.Weekday() {
				continue
			}
			if !r.onInterval(from, cand) {
				continue
			}
		case "MONTHLY":
			if len(r.ByMonthDay) > 0 || len(r.ByDay) > 0 {
				cand = cand.AddDate(0, 0, 1)
				if len(r.ByMonthDay) > 0 && !r.matchMonthDay(cand) {
					continue
				}
				if len(r.ByDay) > 0 && !r.matchDayN(cand) {
					continue
				}
				if !r.onInterval(from, cand) {
					continue
				}
			} else {
				cand = addMonthsKeepDay(from, r.Interval*(i+1))
			}
		case "YEARLY":
			if len(r.ByMonth) > 0 && (len(r.ByMonthDay) > 0 || len(r.ByDay) > 0) {
				cand = cand.AddDate(0, 0, 1)
				if !r.matchMonth(cand) || (len(r.ByMonthDay) > 0 && !r.matchMonthDay(cand)) || (len(r.ByDay) > 0 && !r.matchDayN(cand)) {
					continue
				}
				if !r.onInterval(from, cand) {
					continue
				}
			} else {
				y := from.AddDate(r.Interval*(i+1), 0, 0)
				cand = time.Date(y.Year(), from.Month(), from.Day(), from.Hour(), from.Minute(), from.Second(), 0, loc)
				if cand.Month() != from.Month() { // 29 Feb
					cand = cand.AddDate(0, 0, -1)
				}
			}
		default:
			return time.Time{}, false
		}
		if !cand.After(after) {
			continue
		}
		if r.Until != "" {
			if u, ok := ParseLocal(r.Until, loc); ok {
				if IsDay(r.Until) {
					u = u.Add(24*time.Hour - time.Second)
				}
				if cand.After(u) {
					return time.Time{}, false
				}
			}
		}
		return cand, true
	}
	return time.Time{}, false
}

func (r *Rule) onInterval(from, cand time.Time) bool {
	if r.Interval <= 1 {
		return true
	}
	switch r.Freq {
	case "DAILY":
		days := int(cand.Sub(from).Hours() / 24)
		return days%r.Interval == 0
	case "WEEKLY":
		// weeks since the anchor's week (Monday-based)
		a := startOfWeek(from)
		b := startOfWeek(cand)
		weeks := int(b.Sub(a).Hours() / (24 * 7))
		return weeks%r.Interval == 0
	case "MONTHLY":
		months := (cand.Year()-from.Year())*12 + int(cand.Month()-from.Month())
		return months%r.Interval == 0
	case "YEARLY":
		return (cand.Year()-from.Year())%r.Interval == 0
	}
	return true
}

func startOfWeek(t time.Time) time.Time {
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	return d.AddDate(0, 0, -(wd - 1))
}

func (r *Rule) matchDay(t time.Time) bool {
	for _, d := range r.ByDay {
		if d.Day == t.Weekday() {
			return true
		}
	}
	return false
}

// matchDayN honours "2TU" (second Tuesday) and "-1FR" (last Friday).
func (r *Rule) matchDayN(t time.Time) bool {
	for _, d := range r.ByDay {
		if d.Day != t.Weekday() {
			continue
		}
		if d.N == 0 {
			return true
		}
		if d.N > 0 && (t.Day()-1)/7+1 == d.N {
			return true
		}
		if d.N < 0 {
			last := time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, t.Location()).Day()
			if (last-t.Day())/7+1 == -d.N {
				return true
			}
		}
	}
	return false
}

func (r *Rule) matchMonthDay(t time.Time) bool {
	last := time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, t.Location()).Day()
	for _, d := range r.ByMonthDay {
		if d == t.Day() || (d < 0 && last+d+1 == t.Day()) {
			return true
		}
	}
	return false
}

func (r *Rule) matchMonth(t time.Time) bool {
	for _, m := range r.ByMonth {
		if time.Month(m) == t.Month() {
			return true
		}
	}
	return false
}

// addMonthsKeepDay adds months without rolling over (31 Jan + 1 → 28/29 Feb).
func addMonthsKeepDay(t time.Time, months int) time.Time {
	y, m, d := t.Date()
	first := time.Date(y, m+time.Month(months), 1, t.Hour(), t.Minute(), t.Second(), 0, t.Location())
	last := time.Date(first.Year(), first.Month()+1, 0, 0, 0, 0, 0, t.Location()).Day()
	if d > last {
		d = last
	}
	return time.Date(first.Year(), first.Month(), d, t.Hour(), t.Minute(), t.Second(), 0, t.Location())
}

// NextAfterCompletion is the "repeat after I finish it" reading: one rule
// step from the completion day, keeping the anchor's time of day.
func (r *Rule) NextAfterCompletion(anchor, completed time.Time) (time.Time, bool) {
	if r.Count == 1 {
		return time.Time{}, false
	}
	loc := anchor.Location()
	base := time.Date(completed.Year(), completed.Month(), completed.Day(), anchor.Hour(), anchor.Minute(), anchor.Second(), 0, loc)
	var next time.Time
	switch r.Freq {
	case "DAILY":
		next = base.AddDate(0, 0, r.Interval)
		if len(r.ByDay) > 0 {
			for i := 0; i < 8 && !r.matchDay(next); i++ {
				next = next.AddDate(0, 0, 1)
			}
		}
	case "WEEKLY":
		if len(r.ByDay) > 0 {
			next = base
			for i := 0; i < 8; i++ {
				next = next.AddDate(0, 0, 1)
				if r.matchDay(next) {
					break
				}
			}
			if r.Interval > 1 {
				next = next.AddDate(0, 0, 7*(r.Interval-1))
			}
		} else {
			next = base.AddDate(0, 0, 7*r.Interval)
		}
	case "MONTHLY":
		next = addMonthsKeepDay(base, r.Interval)
		if len(r.ByMonthDay) > 0 {
			d := r.ByMonthDay[0]
			last := time.Date(next.Year(), next.Month()+1, 0, 0, 0, 0, 0, loc).Day()
			if d < 0 {
				d = last + d + 1
			}
			if d > last {
				d = last
			}
			next = time.Date(next.Year(), next.Month(), d, next.Hour(), next.Minute(), next.Second(), 0, loc)
		}
	case "YEARLY":
		next = base.AddDate(r.Interval, 0, 0)
	default:
		return time.Time{}, false
	}
	if r.Until != "" {
		if u, ok := ParseLocal(r.Until, loc); ok && next.After(u.Add(24*time.Hour)) {
			return time.Time{}, false
		}
	}
	return next, true
}

// Describe gives a short human reading ("every 2 weeks on Mon, Wed").
func (r *Rule) Describe() string {
	unit := map[string]string{"DAILY": "day", "WEEKLY": "week", "MONTHLY": "month", "YEARLY": "year"}[r.Freq]
	s := "every "
	if r.Interval > 1 {
		s += strconv.Itoa(r.Interval) + " " + unit + "s"
	} else {
		s += unit
	}
	if len(r.ByDay) > 0 {
		names := []string{}
		for _, d := range r.ByDay {
			n := d.Day.String()[:3]
			if d.N > 0 {
				n = ordinal(d.N) + " " + n
			} else if d.N < 0 {
				n = "last " + n
			}
			names = append(names, n)
		}
		s += " on " + strings.Join(names, ", ")
	}
	if len(r.ByMonthDay) > 0 {
		names := []string{}
		for _, d := range r.ByMonthDay {
			if d < 0 {
				names = append(names, "last day")
			} else {
				names = append(names, ordinal(d))
			}
		}
		s += " on the " + strings.Join(names, ", ")
	}
	if r.Count > 0 {
		s += ", " + strconv.Itoa(r.Count) + " times"
	}
	if r.Until != "" {
		s += " until " + Day(r.Until)
	}
	return s
}

func ordinal(n int) string {
	suffix := "th"
	switch {
	case n%100 >= 11 && n%100 <= 13:
	case n%10 == 1:
		suffix = "st"
	case n%10 == 2:
		suffix = "nd"
	case n%10 == 3:
		suffix = "rd"
	}
	return strconv.Itoa(n) + suffix
}
