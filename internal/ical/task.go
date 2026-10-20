package ical

import (
	"strconv"
	"strings"
	"time"
)

// Alarm is one VALARM in the shape the API uses: a trigger relative to the
// due (or the start when the task has no due) such as "-PT15M", or an
// absolute UTC moment "2026-10-21T08:00:00Z".
type Alarm struct {
	Trigger     string `json:"trigger"`
	Description string `json:"description,omitempty"`
}

// Task is what the index keeps of a VTODO.
type Task struct {
	UID         string
	Title       string
	Notes       string
	Status      string // open | in_progress | done | cancelled
	CompletedAt time.Time
	Percent     int
	Priority    int
	Start       string // floating local
	Due         string
	EstimateMin int
	RRule       string
	RepeatFrom  string // due | completion
	Someday     bool
	SortOrder   float64
	HasOrder    bool
	Location    string
	URL         string
	Tags        []string
	Alarms      []Alarm
	ParentUID   string
	Section     string
	Created     time.Time
	Modified    time.Time
	Sequence    int
}

// Status values.
const (
	StatusOpen       = "open"
	StatusInProgress = "in_progress"
	StatusDone       = "done"
	StatusCancelled  = "cancelled"
)

// Managed are the VTODO properties this app rewrites; every other one is
// left as the client that wrote it sent it.
var managed = map[string]bool{
	"SUMMARY": true, "DESCRIPTION": true, "STATUS": true, "COMPLETED": true, "PERCENT-COMPLETE": true,
	"PRIORITY": true, "DTSTART": true, "DUE": true, "RRULE": true, "CATEGORIES": true, "LOCATION": true,
	"URL": true, "RELATED-TO": true, "LAST-MODIFIED": true, "DTSTAMP": true, "SEQUENCE": true, "CREATED": true,
	"X-POCKET-ESTIMATE": true, "X-POCKET-REPEAT-FROM": true, "X-POCKET-SOMEDAY": true, "X-POCKET-ORDER": true,
	"X-POCKET-SECTION": true,
}

// FromVTODO reads the first VTODO of a calendar. loc is the person's zone.
func FromVTODO(cal *Component, loc *time.Location) (*Task, bool) {
	var todo *Component
	if cal.Name == "VTODO" {
		todo = cal
	} else {
		todo = cal.Child("VTODO")
	}
	if todo == nil {
		return nil, false
	}
	t := &Task{UID: strings.TrimSpace(todo.Text("UID")), Title: strings.TrimSpace(todo.Text("SUMMARY")), Notes: todo.Text("DESCRIPTION")}
	t.Start, _ = ReadDateTime(todo.Get("DTSTART"), loc)
	t.Due, _ = ReadDateTime(todo.Get("DUE"), loc)
	if t.Due == "" {
		if p := todo.Get("DURATION"); p != nil && t.Start != "" {
			if d, err := Duration(p.Value); err == nil {
				if st, ok := ParseLocal(t.Start, loc); ok {
					t.Due = FormatLocal(st.Add(d), IsDay(t.Start))
				}
			}
		}
	}
	t.Percent, _ = strconv.Atoi(strings.TrimSpace(todo.Value("PERCENT-COMPLETE")))
	t.Priority, _ = strconv.Atoi(strings.TrimSpace(todo.Value("PRIORITY")))
	if t.Priority < 0 || t.Priority > 9 {
		t.Priority = 0
	}
	t.Sequence, _ = strconv.Atoi(strings.TrimSpace(todo.Value("SEQUENCE")))
	if c, ok := ReadUTC(todo.Get("COMPLETED"), loc); ok {
		t.CompletedAt = c
	}
	switch strings.ToUpper(strings.TrimSpace(todo.Value("STATUS"))) {
	case "COMPLETED":
		t.Status = StatusDone
	case "CANCELLED":
		t.Status = StatusCancelled
	case "IN-PROCESS":
		t.Status = StatusInProgress
	default:
		t.Status = StatusOpen
		if !t.CompletedAt.IsZero() || t.Percent == 100 {
			t.Status = StatusDone
		}
	}
	if t.Status == StatusDone && t.CompletedAt.IsZero() {
		if m, ok := ReadUTC(todo.Get("LAST-MODIFIED"), loc); ok {
			t.CompletedAt = m
		} else {
			t.CompletedAt = time.Now().UTC()
		}
	}
	if t.Status != StatusDone {
		t.CompletedAt = time.Time{}
	}
	if p := todo.Get("RRULE"); p != nil {
		t.RRule = strings.TrimSpace(p.Value)
	}
	for _, p := range todo.GetAll("CATEGORIES") {
		for _, c := range splitList(p.Value) {
			if c = strings.TrimSpace(c); c != "" {
				t.Tags = append(t.Tags, c)
			}
		}
	}
	t.Location = strings.TrimSpace(todo.Text("LOCATION"))
	t.URL = strings.TrimSpace(todo.Value("URL"))
	for _, p := range todo.GetAll("RELATED-TO") {
		rt := strings.ToUpper(p.Param("RELTYPE"))
		if rt == "" || rt == "PARENT" {
			t.ParentUID = strings.TrimSpace(p.Text())
			break
		}
	}
	t.EstimateMin, _ = strconv.Atoi(strings.TrimSpace(todo.Value("X-POCKET-ESTIMATE")))
	if todo.Value("X-POCKET-REPEAT-FROM") == "completion" {
		t.RepeatFrom = "completion"
	} else if t.RRule != "" {
		t.RepeatFrom = "due"
	}
	t.Someday = todo.Value("X-POCKET-SOMEDAY") == "1"
	t.Section = strings.TrimSpace(todo.Text("X-POCKET-SECTION"))
	if v := strings.TrimSpace(todo.Value("X-POCKET-ORDER")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			t.SortOrder, t.HasOrder = f, true
		}
	} else if v := strings.TrimSpace(todo.Value("X-APPLE-SORT-ORDER")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			t.SortOrder, t.HasOrder = f, true
		}
	}
	t.Created, _ = ReadUTC(todo.Get("CREATED"), loc)
	t.Modified, _ = ReadUTC(todo.Get("LAST-MODIFIED"), loc)
	if t.Modified.IsZero() {
		t.Modified, _ = ReadUTC(todo.Get("DTSTAMP"), loc)
	}
	for _, a := range todo.ChildrenNamed("VALARM") {
		if al, ok := readAlarm(a, t); ok {
			t.Alarms = append(t.Alarms, al)
		}
	}
	return t, true
}

func splitList(v string) []string {
	var out []string
	cur := strings.Builder{}
	for i := 0; i < len(v); i++ {
		if v[i] == '\\' && i+1 < len(v) {
			cur.WriteByte(v[i+1])
			i++
			continue
		}
		if v[i] == ',' {
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(v[i])
	}
	out = append(out, cur.String())
	return out
}

// readAlarm maps a VALARM to the API shape: relative triggers are expressed
// against the due (RELATED=END, or START when the task has no due).
func readAlarm(a *Component, t *Task) (Alarm, bool) {
	p := a.Get("TRIGGER")
	if p == nil {
		return Alarm{}, false
	}
	al := Alarm{Description: strings.TrimSpace(a.Text("DESCRIPTION"))}
	if al.Description == "Reminder" || al.Description == "Default" {
		al.Description = ""
	}
	v := strings.TrimSpace(p.Value)
	if strings.EqualFold(p.Param("VALUE"), "DATE-TIME") || strings.HasSuffix(v, "Z") {
		if at, ok := ReadUTC(p, time.UTC); ok {
			al.Trigger = at.UTC().Format(time.RFC3339)
			return al, true
		}
		return Alarm{}, false
	}
	d, err := Duration(v)
	if err != nil {
		return Alarm{}, false
	}
	// RFC: default relative to DTSTART; for a task with a due and no start,
	// treat it as relative to the due. A start-relative trigger on a task
	// with both is turned into a due-relative one for the API.
	rel := strings.ToUpper(p.Param("RELATED"))
	if rel != "END" && t.Start != "" && t.Due != "" && t.Start != t.Due {
		if st, ok1 := ParseLocal(t.Start, time.UTC); ok1 {
			if du, ok2 := ParseLocal(t.Due, time.UTC); ok2 {
				d -= du.Sub(st)
			}
		}
	}
	al.Trigger = FormatDuration(d)
	return al, true
}

// Apply writes the task's managed properties into the calendar's VTODO
// (creating VCALENDAR and VTODO when nil) and returns the calendar.
func Apply(cal *Component, t *Task, now time.Time) *Component {
	if cal == nil {
		cal = NewCalendar()
	}
	todo := cal.Child("VTODO")
	if todo == nil {
		todo = &Component{Name: "VTODO"}
		cal.Children = append(cal.Children, todo)
	}
	if todo.Get("UID") == nil {
		todo.SetText("UID", t.UID)
	}
	todo.SetText("SUMMARY", t.Title)
	if t.Notes != "" {
		todo.SetText("DESCRIPTION", t.Notes)
	} else {
		todo.Remove("DESCRIPTION")
	}
	switch t.Status {
	case StatusDone:
		todo.SetRaw("STATUS", "COMPLETED")
		c := t.CompletedAt
		if c.IsZero() {
			c = now
		}
		todo.Set(UTCProperty("COMPLETED", c))
		todo.SetRaw("PERCENT-COMPLETE", "100")
	case StatusCancelled:
		todo.SetRaw("STATUS", "CANCELLED")
		todo.Remove("COMPLETED")
		todo.Remove("PERCENT-COMPLETE")
	case StatusInProgress:
		todo.SetRaw("STATUS", "IN-PROCESS")
		todo.Remove("COMPLETED")
		if t.Percent > 0 && t.Percent < 100 {
			todo.SetRaw("PERCENT-COMPLETE", strconv.Itoa(t.Percent))
		} else {
			todo.Remove("PERCENT-COMPLETE")
		}
	default:
		todo.SetRaw("STATUS", "NEEDS-ACTION")
		todo.Remove("COMPLETED")
		if t.Percent > 0 && t.Percent < 100 {
			todo.SetRaw("PERCENT-COMPLETE", strconv.Itoa(t.Percent))
		} else {
			todo.Remove("PERCENT-COMPLETE")
		}
	}
	if t.Priority > 0 {
		todo.SetRaw("PRIORITY", strconv.Itoa(t.Priority))
	} else {
		todo.Remove("PRIORITY")
	}
	if t.Start != "" {
		todo.Set(DateProperty("DTSTART", t.Start))
	} else {
		todo.Remove("DTSTART")
	}
	todo.Remove("DURATION")
	if t.Due != "" {
		todo.Set(DateProperty("DUE", t.Due))
	} else {
		todo.Remove("DUE")
	}
	if t.RRule != "" {
		todo.SetRaw("RRULE", t.RRule)
		if t.RepeatFrom == "completion" {
			todo.SetRaw("X-POCKET-REPEAT-FROM", "completion")
		} else {
			todo.Remove("X-POCKET-REPEAT-FROM")
		}
	} else {
		todo.Remove("RRULE")
		todo.Remove("X-POCKET-REPEAT-FROM")
	}
	if len(t.Tags) > 0 {
		esc := make([]string, len(t.Tags))
		for i, c := range t.Tags {
			esc[i] = Escape(c)
		}
		todo.SetRaw("CATEGORIES", strings.Join(esc, ","))
	} else {
		todo.Remove("CATEGORIES")
	}
	if t.Location != "" {
		todo.SetText("LOCATION", t.Location)
	} else {
		todo.Remove("LOCATION")
	}
	if t.URL != "" {
		todo.SetRaw("URL", t.URL)
	} else {
		todo.Remove("URL")
	}
	// Keep other RELATED-TO kinds (SIBLING, CHILD), replace the parent.
	var kept []Property
	for _, p := range todo.GetAll("RELATED-TO") {
		rt := strings.ToUpper(p.Param("RELTYPE"))
		if rt != "" && rt != "PARENT" {
			kept = append(kept, p)
		}
	}
	todo.Remove("RELATED-TO")
	if t.ParentUID != "" {
		todo.Props = append(todo.Props, Property{Name: "RELATED-TO", Params: []Param{{Name: "RELTYPE", Values: []string{"PARENT"}}}, Value: Escape(t.ParentUID)})
	}
	todo.Props = append(todo.Props, kept...)
	if t.EstimateMin > 0 {
		todo.SetRaw("X-POCKET-ESTIMATE", strconv.Itoa(t.EstimateMin))
	} else {
		todo.Remove("X-POCKET-ESTIMATE")
	}
	if t.Someday {
		todo.SetRaw("X-POCKET-SOMEDAY", "1")
	} else {
		todo.Remove("X-POCKET-SOMEDAY")
	}
	if t.Section != "" {
		todo.SetText("X-POCKET-SECTION", t.Section)
	} else {
		todo.Remove("X-POCKET-SECTION")
	}
	if t.HasOrder {
		todo.SetRaw("X-POCKET-ORDER", strconv.FormatFloat(t.SortOrder, 'f', -1, 64))
	}
	// Alarms: when the set of triggers is unchanged the client's own VALARM
	// components stay as they were (Apple keeps ids in them); otherwise ours
	// replace the whole set.
	if !sameAlarms(todo, t) {
		todo.RemoveChildren("VALARM")
		for _, a := range t.Alarms {
			todo.Children = append(todo.Children, alarmComponent(a, t))
		}
	}
	created := t.Created
	if created.IsZero() {
		created = now
	}
	if todo.Get("CREATED") == nil {
		todo.Set(UTCProperty("CREATED", created))
	}
	todo.Set(UTCProperty("DTSTAMP", now))
	todo.Set(UTCProperty("LAST-MODIFIED", now))
	todo.SetRaw("SEQUENCE", strconv.Itoa(t.Sequence))
	return cal
}

func alarmComponent(a Alarm, t *Task) *Component {
	c := &Component{Name: "VALARM"}
	c.SetRaw("ACTION", "DISPLAY")
	desc := a.Description
	if desc == "" {
		desc = t.Title
	}
	c.SetText("DESCRIPTION", desc)
	if at, err := time.Parse(time.RFC3339, a.Trigger); err == nil {
		c.Set(Property{Name: "TRIGGER", Params: []Param{{Name: "VALUE", Values: []string{"DATE-TIME"}}}, Value: at.UTC().Format("20060102T150405Z")})
		return c
	}
	d, err := Duration(a.Trigger)
	if err != nil {
		d = 0
	}
	p := Property{Name: "TRIGGER", Value: FormatDuration(d)}
	if t.Due != "" && t.Start != "" {
		p.Params = []Param{{Name: "RELATED", Values: []string{"END"}}}
	}
	c.Set(p)
	return c
}

// AlarmTime computes when an alarm fires, in loc. allDayAt is the wall
// clock used for whole-day dates ("09:00").
func AlarmTime(a Alarm, t *Task, loc *time.Location, allDayAt string) (time.Time, bool) {
	if at, err := time.Parse(time.RFC3339, a.Trigger); err == nil {
		return at, true
	}
	d, err := Duration(a.Trigger)
	if err != nil {
		return time.Time{}, false
	}
	base := t.Due
	if base == "" {
		base = t.Start
	}
	if base == "" {
		return time.Time{}, false
	}
	bt, ok := ParseLocal(base, loc)
	if !ok {
		return time.Time{}, false
	}
	if IsDay(base) {
		h, m := 9, 0
		if len(allDayAt) >= 5 {
			h, _ = strconv.Atoi(allDayAt[:2])
			m, _ = strconv.Atoi(allDayAt[3:5])
		}
		bt = time.Date(bt.Year(), bt.Month(), bt.Day(), h, m, 0, 0, loc)
	}
	return bt.Add(d), true
}

// sameAlarms reports whether the VALARMs in todo say the same as t.Alarms.
func sameAlarms(todo *Component, t *Task) bool {
	existing := todo.ChildrenNamed("VALARM")
	if len(existing) != len(t.Alarms) {
		return false
	}
	have := map[string]int{}
	for _, a := range existing {
		al, ok := readAlarm(a, t)
		if !ok {
			return false
		}
		have[al.Trigger]++
	}
	for _, a := range t.Alarms {
		if have[a.Trigger] == 0 {
			return false
		}
		have[a.Trigger]--
	}
	return true
}
