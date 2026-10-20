// Package quick reads a task typed in plain words:
//
//	Call the bank tomorrow 9am !high #money /Home every month ^Errands ~30m *15m
//
// Dates: today, tomorrow, tod/tom, yesterday, mon…sunday (next such day), next week,
// next month, in 3 days / weeks / months, 21 oct, oct 21, 21.10., 2026-10-21, 21/10.
// Times: 9am, 9:30, 17h, at 9, 9pm. "due …" / "by …" marks a deadline rather than a start;
// "on …"/"at …" a start. !high !medium !low or !!!/!!/!, #tag, /list, ^section,
// "every day|week|month|year|N days|weekday|monday|2 weeks", ~30m estimate, *15m alarm
// (before), "someday".
package quick

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Parsed is what the text said.
type Parsed struct {
	Title       string   `json:"title"`
	Start       string   `json:"start,omitempty"`
	Due         string   `json:"due,omitempty"`
	List        string   `json:"list,omitempty"`
	Section     string   `json:"section,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Priority    int      `json:"priority,omitempty"`
	RRule       string   `json:"rrule,omitempty"`
	RepeatFrom  string   `json:"repeat_from,omitempty"`
	EstimateMin int      `json:"estimate_min,omitempty"`
	Alarm       string   `json:"alarm,omitempty"`
	Someday     bool     `json:"someday,omitempty"`
	Notes       string   `json:"notes,omitempty"`
}

var (
	reTag      = regexp.MustCompile(`(?:^|\s)#([\p{L}\p{N}_][\p{L}\p{N}_\-]*)`)
	reList     = regexp.MustCompile(`(?:^|\s)/("[^"]+"|[\p{L}\p{N}_][\p{L}\p{N}_\-]*)`)
	reSection  = regexp.MustCompile(`(?:^|\s)\^("[^"]+"|[\p{L}\p{N}_][\p{L}\p{N}_\-]*)`)
	rePrioWord = regexp.MustCompile(`(?i)(?:^|\s)!(high|medium|med|low|none|[1-3])\b`)
	rePrioBang = regexp.MustCompile(`(?:^|\s)(!{1,3})(?:\s|$)`)
	reEstimate = regexp.MustCompile(`(?i)(?:^|\s)~(\d+)\s*(m|min|h|hr|hours?|minutes?)?\b`)
	reAlarm    = regexp.MustCompile(`(?i)(?:^|\s)\*(\d+)\s*(m|min|h|hr|d|day|days|hours?|minutes?)?\b`)
	reEvery    = regexp.MustCompile(`(?i)(?:^|\s)(every|each)\s+(other\s+)?(\d+\s+)?(day|days|weekday|weekdays|workday|workdays|week|weeks|month|months|year|years|mon|monday|tue|tuesday|wed|wednesday|thu|thursday|fri|friday|sat|saturday|sun|sunday|morning|evening)\b(?:\s+(after\s+(?:done|completion|finish(?:ing)?)))?`)
	reSomeday  = regexp.MustCompile(`(?i)(?:^|\s)someday\b`)
	reNotes    = regexp.MustCompile(`\s//\s*(.+)$`)
	reISO      = regexp.MustCompile(`(?i)(?:^|\s)(due|by|deadline|on|at|from)?\s*(\d{4}-\d{2}-\d{2})(?:[T ](\d{1,2}):(\d{2}))?\b`)
	reDMY      = regexp.MustCompile(`(?i)(?:^|\s)(due|by|deadline|on|at|from)?\s*(\d{1,2})[./](\d{1,2})(?:[./](\d{2,4})?)?(?:\s|$)`)
	reDayMon   = regexp.MustCompile(`(?i)(?:^|\s)(due|by|deadline|on|at|from)?\s*(\d{1,2})(?:st|nd|rd|th)?\s+(jan|feb|mar|apr|may|jun|jul|aug|sep|sept|oct|nov|dec)[a-z]*\b(?:\s+(\d{4}))?`)
	reMonDay   = regexp.MustCompile(`(?i)(?:^|\s)(due|by|deadline|on|at|from)?\s*(jan|feb|mar|apr|may|jun|jul|aug|sep|sept|oct|nov|dec)[a-z]*\s+(\d{1,2})(?:st|nd|rd|th)?\b(?:,?\s+(\d{4}))?`)
	reRel      = regexp.MustCompile(`(?i)(?:^|\s)(due|by|deadline|on|at|from)?\s*(today|tod|tomorrow|tom|tmr|yesterday|tonight|next\s+week|next\s+month|next\s+year|this\s+weekend|next\s+weekend|end\s+of\s+(?:the\s+)?(?:week|month)|in\s+(\d+)\s+(day|days|week|weeks|month|months|hour|hours)|(?:next\s+|this\s+)?(mon|monday|tue|tues|tuesday|wed|wednesday|thu|thur|thurs|thursday|fri|friday|sat|saturday|sun|sunday))\b`)
	reTime     = regexp.MustCompile(`(?i)(?:^|\s)(?:at\s+)?(\d{1,2})(?::(\d{2}))?\s*(am|pm|h)?\b`)
	reDueTime  = regexp.MustCompile(`(?i)(?:^|\s)(due|by|deadline)\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm|h)?\b`)
	reSpace    = regexp.MustCompile(`\s+`)
)

var weekdays = map[string]time.Weekday{"mon": time.Monday, "monday": time.Monday, "tue": time.Tuesday, "tues": time.Tuesday, "tuesday": time.Tuesday, "wed": time.Wednesday, "wednesday": time.Wednesday,
	"thu": time.Thursday, "thur": time.Thursday, "thurs": time.Thursday, "thursday": time.Thursday, "fri": time.Friday, "friday": time.Friday, "sat": time.Saturday, "saturday": time.Saturday, "sun": time.Sunday, "sunday": time.Sunday}
var months = map[string]time.Month{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "sept": 9, "oct": 10, "nov": 11, "dec": 12}
var byday = map[time.Weekday]string{time.Monday: "MO", time.Tuesday: "TU", time.Wednesday: "WE", time.Thursday: "TH", time.Friday: "FR", time.Saturday: "SA", time.Sunday: "SU"}

// Parse reads text with "now" as the reference.
func Parse(text string, now time.Time) Parsed {
	p := Parsed{}
	s := " " + strings.TrimSpace(text) + " "
	cut := func(re *regexp.Regexp, m []int) { s = s[:m[0]] + " " + s[m[1]:] }

	if m := reNotes.FindStringSubmatchIndex(s); m != nil {
		p.Notes = strings.TrimSpace(s[m[2]:m[3]])
		s = s[:m[0]] + " "
	}
	for {
		m := reTag.FindStringSubmatchIndex(s)
		if m == nil {
			break
		}
		p.Tags = append(p.Tags, strings.ToLower(s[m[2]:m[3]]))
		cut(reTag, m)
	}
	if m := reList.FindStringSubmatchIndex(s); m != nil {
		p.List = strings.Trim(s[m[2]:m[3]], `"`)
		cut(reList, m)
	}
	if m := reSection.FindStringSubmatchIndex(s); m != nil {
		p.Section = strings.Trim(s[m[2]:m[3]], `"`)
		cut(reSection, m)
	}
	if m := rePrioWord.FindStringSubmatchIndex(s); m != nil {
		switch strings.ToLower(s[m[2]:m[3]]) {
		case "high", "1":
			p.Priority = 1
		case "medium", "med", "2":
			p.Priority = 5
		case "low", "3":
			p.Priority = 9
		}
		cut(rePrioWord, m)
	} else if m := rePrioBang.FindStringSubmatchIndex(s); m != nil {
		switch m[3] - m[2] {
		case 3:
			p.Priority = 1
		case 2:
			p.Priority = 5
		case 1:
			p.Priority = 9
		}
		cut(rePrioBang, m)
	}
	if m := reEstimate.FindStringSubmatchIndex(s); m != nil {
		n, _ := strconv.Atoi(s[m[2]:m[3]])
		unit := ""
		if m[4] >= 0 {
			unit = strings.ToLower(s[m[4]:m[5]])
		}
		if strings.HasPrefix(unit, "h") {
			n *= 60
		}
		p.EstimateMin = n
		cut(reEstimate, m)
	}
	if m := reAlarm.FindStringSubmatchIndex(s); m != nil {
		n, _ := strconv.Atoi(s[m[2]:m[3]])
		unit := "m"
		if m[4] >= 0 {
			unit = strings.ToLower(s[m[4]:m[5]])
		}
		switch {
		case strings.HasPrefix(unit, "h"):
			p.Alarm = "-PT" + strconv.Itoa(n) + "H"
		case strings.HasPrefix(unit, "d"):
			p.Alarm = "-P" + strconv.Itoa(n) + "D"
		default:
			p.Alarm = "-PT" + strconv.Itoa(n) + "M"
		}
		if n == 0 {
			p.Alarm = "PT0S"
		}
		cut(reAlarm, m)
	}
	if m := reSomeday.FindStringSubmatchIndex(s); m != nil {
		p.Someday = true
		cut(reSomeday, m)
	}
	if m := reEvery.FindStringSubmatchIndex(s); m != nil {
		interval := 1
		if m[4] >= 0 {
			interval = 2
		}
		if m[6] >= 0 {
			interval, _ = strconv.Atoi(strings.TrimSpace(s[m[6]:m[7]]))
			if interval < 1 {
				interval = 1
			}
		}
		unit := strings.ToLower(s[m[8]:m[9]])
		rule := ""
		switch {
		case strings.HasPrefix(unit, "day"), unit == "morning", unit == "evening":
			rule = "FREQ=DAILY"
		case strings.HasPrefix(unit, "weekday"), strings.HasPrefix(unit, "workday"):
			rule = "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"
		case strings.HasPrefix(unit, "week"):
			rule = "FREQ=WEEKLY"
		case strings.HasPrefix(unit, "month"):
			rule = "FREQ=MONTHLY"
		case strings.HasPrefix(unit, "year"):
			rule = "FREQ=YEARLY"
		default:
			if wd, ok := weekdays[unit]; ok {
				rule = "FREQ=WEEKLY;BYDAY=" + byday[wd]
				if p.Start == "" && p.Due == "" {
					p.Start = nextWeekday(now, wd, false).Format("2006-01-02")
				}
			}
		}
		if interval > 1 && rule != "" {
			rule += ";INTERVAL=" + strconv.Itoa(interval)
		}
		p.RRule = rule
		p.RepeatFrom = "due"
		if m[10] >= 0 {
			p.RepeatFrom = "completion"
		}
		cut(reEvery, m)
	}

	// Dates, the most specific forms first. A "due/by" word before the date
	// makes it the deadline; otherwise it is the start ("when").
	var dateDay string
	var dateKind string
	var timeStr string
	setDate := func(kind string, t time.Time) {
		dateDay = t.Format("2006-01-02")
		dateKind = strings.ToLower(strings.TrimSpace(kind))
	}
	if m := reISO.FindStringSubmatchIndex(s); m != nil {
		if t, err := time.ParseInLocation("2006-01-02", s[m[4]:m[5]], now.Location()); err == nil {
			setDate(sub(s, m, 1), t)
			if m[6] >= 0 {
				timeStr = pad(s[m[6]:m[7]]) + ":" + s[m[8]:m[9]]
			}
			cut(reISO, m)
		}
	} else if m := reDayMon.FindStringSubmatchIndex(s); m != nil {
		d, _ := strconv.Atoi(s[m[4]:m[5]])
		mo := months[strings.ToLower(s[m[6]:m[7]])]
		y := now.Year()
		if m[8] >= 0 {
			y, _ = strconv.Atoi(s[m[8]:m[9]])
		}
		t := time.Date(y, mo, d, 0, 0, 0, 0, now.Location())
		if m[8] < 0 && t.Before(startOfDay(now).AddDate(0, 0, -1)) {
			t = t.AddDate(1, 0, 0)
		}
		setDate(sub(s, m, 1), t)
		cut(reDayMon, m)
	} else if m := reMonDay.FindStringSubmatchIndex(s); m != nil {
		mo := months[strings.ToLower(s[m[4]:m[5]])]
		d, _ := strconv.Atoi(s[m[6]:m[7]])
		y := now.Year()
		if m[8] >= 0 {
			y, _ = strconv.Atoi(s[m[8]:m[9]])
		}
		t := time.Date(y, mo, d, 0, 0, 0, 0, now.Location())
		if m[8] < 0 && t.Before(startOfDay(now).AddDate(0, 0, -1)) {
			t = t.AddDate(1, 0, 0)
		}
		setDate(sub(s, m, 1), t)
		cut(reMonDay, m)
	} else if m := reRel.FindStringSubmatchIndex(s); m != nil {
		word := strings.ToLower(reSpace.ReplaceAllString(s[m[4]:m[5]], " "))
		var t time.Time
		base := startOfDay(now)
		switch {
		case word == "today" || word == "tod" || word == "tonight":
			t = base
			if word == "tonight" {
				timeStr = "19:00"
			}
		case word == "tomorrow" || word == "tom" || word == "tmr":
			t = base.AddDate(0, 0, 1)
		case word == "yesterday":
			t = base.AddDate(0, 0, -1)
		case word == "next week":
			t = nextWeekday(now, time.Monday, true)
		case word == "next month":
			t = time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, now.Location())
		case word == "next year":
			t = time.Date(now.Year()+1, 1, 1, 0, 0, 0, 0, now.Location())
		case word == "this weekend":
			t = nextWeekday(now, time.Saturday, false)
		case word == "next weekend":
			t = nextWeekday(now, time.Saturday, true)
		case strings.HasPrefix(word, "end of"):
			if strings.HasSuffix(word, "week") {
				t = nextWeekday(now, time.Sunday, false)
			} else {
				t = time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, now.Location())
			}
		case strings.HasPrefix(word, "in "):
			n, _ := strconv.Atoi(s[m[6]:m[7]])
			unit := strings.ToLower(s[m[8]:m[9]])
			switch {
			case strings.HasPrefix(unit, "day"):
				t = base.AddDate(0, 0, n)
			case strings.HasPrefix(unit, "week"):
				t = base.AddDate(0, 0, 7*n)
			case strings.HasPrefix(unit, "month"):
				t = base.AddDate(0, n, 0)
			case strings.HasPrefix(unit, "hour"):
				at := now.Add(time.Duration(n) * time.Hour)
				t = startOfDay(at)
				timeStr = at.Format("15:04")
			}
		default:
			name := strings.ToLower(s[m[10]:m[11]])
			if wd, ok := weekdays[name]; ok {
				t = nextWeekday(now, wd, strings.HasPrefix(word, "next "))
			}
		}
		if !t.IsZero() {
			setDate(sub(s, m, 1), t)
			cut(reRel, m)
		}
	} else if m := reDMY.FindStringSubmatchIndex(s); m != nil {
		d, _ := strconv.Atoi(s[m[4]:m[5]])
		mo, _ := strconv.Atoi(s[m[6]:m[7]])
		y := now.Year()
		if m[8] >= 0 {
			y, _ = strconv.Atoi(s[m[8]:m[9]])
			if y < 100 {
				y += 2000
			}
		}
		if d >= 1 && d <= 31 && mo >= 1 && mo <= 12 {
			t := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, now.Location())
			if m[8] < 0 && t.Before(startOfDay(now).AddDate(0, 0, -1)) {
				t = t.AddDate(1, 0, 0)
			}
			setDate(sub(s, m, 1), t)
			cut(reDMY, m)
		}
	}
	// a deadline time on its own ("due 17:00")
	dueTime := ""
	if m := reDueTime.FindStringSubmatchIndex(s); m != nil {
		dueTime = clock(s[m[4]:m[5]], sub(s, m, 2), sub(s, m, 3))
		cut(reDueTime, m)
	}
	if timeStr == "" {
		if m := reTime.FindStringSubmatchIndex(s); m != nil {
			h, _ := strconv.Atoi(s[m[2]:m[3]])
			ampm := strings.ToLower(sub(s, m, 3))
			minutes := sub(s, m, 2)
			explicit := strings.Contains(strings.ToLower(s[max(0, m[0]-1):m[1]]), "at ") || minutes != "" || ampm != ""
			if explicit && h <= 24 {
				timeStr = clock(s[m[2]:m[3]], minutes, ampm)
				cut(reTime, m)
			}
		}
	}
	if dateDay == "" && (timeStr != "" || dueTime != "") {
		dateDay = startOfDay(now).Format("2006-01-02")
		if timeStr != "" && timeStr <= now.Format("15:04") {
			dateDay = startOfDay(now).AddDate(0, 0, 1).Format("2006-01-02")
		}
	}
	if dateDay != "" {
		v := dateDay
		if timeStr != "" {
			v += "T" + timeStr + ":00"
		}
		switch dateKind {
		case "due", "by", "deadline":
			p.Due = v
		default:
			p.Start = v
		}
	}
	if dueTime != "" {
		d := dateDay
		if p.Due != "" {
			d = p.Due[:10]
		}
		p.Due = d + "T" + dueTime + ":00"
	}
	p.Title = strings.TrimSpace(reSpace.ReplaceAllString(s, " "))
	p.Title = strings.TrimRight(p.Title, " ,;-")
	return p
}

func sub(s string, m []int, i int) string {
	if m[2*i] < 0 {
		return ""
	}
	return s[m[2*i]:m[2*i+1]]
}

func pad(h string) string {
	if len(h) == 1 {
		return "0" + h
	}
	return h
}

func clock(h, minutes, ampm string) string {
	hh, _ := strconv.Atoi(h)
	switch strings.ToLower(ampm) {
	case "pm":
		if hh < 12 {
			hh += 12
		}
	case "am":
		if hh == 12 {
			hh = 0
		}
	}
	if hh > 23 {
		hh = 23
	}
	if minutes == "" {
		minutes = "00"
	}
	return pad(strconv.Itoa(hh)) + ":" + minutes
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// nextWeekday is the next such day after today (next=true: in the following week).
func nextWeekday(now time.Time, wd time.Weekday, next bool) time.Time {
	d := startOfDay(now)
	days := (int(wd) - int(d.Weekday()) + 7) % 7
	if days == 0 {
		days = 7
	}
	if next && days < 7 {
		// "next monday" said on a Wednesday is the Monday after this coming one
		// only when this coming one is in the current week; keep it simple: +7 when today is after it
		if int(wd) > int(d.Weekday()) {
			days += 7
		}
	}
	return d.AddDate(0, 0, days)
}
