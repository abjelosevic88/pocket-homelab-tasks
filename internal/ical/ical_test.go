package ical

import (
	"strings"
	"testing"
	"time"
)

const iosTodo = "BEGIN:VCALENDAR\r\nCALSCALE:GREGORIAN\r\nPRODID:-//Apple Inc.//iOS 26.6.2//EN\r\nVERSION:2.0\r\nBEGIN:VTIMEZONE\r\nTZID:Europe/Sarajevo\r\nBEGIN:DAYLIGHT\r\nDTSTART:19830327T020000\r\nRRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU\r\nTZNAME:GMT+2\r\nTZOFFSETFROM:+0100\r\nTZOFFSETTO:+0200\r\nEND:DAYLIGHT\r\nEND:VTIMEZONE\r\nBEGIN:VTODO\r\nCREATED:20260831T144254Z\r\nUID:DD39B178-8E9D-44FD-9BAA-9C99AB8905BE\r\nX-BUSYMAC-LASTMODBY:Aleksandar Bjelosevic\r\nSUMMARY:HRM\\, the weekly one\r\nDESCRIPTION:Line one\\nLine two; with semicolon\r\nDTSTART:20261021T090000\r\nDUE:20261021T090000\r\nRRULE:FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR\r\nSTATUS:NEEDS-ACTION\r\nDTSTAMP:20261020T081928Z\r\nLAST-MODIFIED:20261020T081928Z\r\nSEQUENCE:27\r\nCATEGORIES:Work,Home\\, sweet\r\nBEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:Reminder\r\nTRIGGER:PT0S\r\nUID:0BF91AA9-AB7A-4441-9448-85293A1083FF\r\nX-WR-ALARMUID:0BF91AA9-AB7A-4441-9448-85293A1083FF\r\nEND:VALARM\r\nEND:VTODO\r\nEND:VCALENDAR\r\n"

func TestParseAndRoundTrip(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Sarajevo")
	cal, err := Parse(iosTodo)
	if err != nil {
		t.Fatal(err)
	}
	task, ok := FromVTODO(cal, loc)
	if !ok {
		t.Fatal("no VTODO")
	}
	if task.Title != "HRM, the weekly one" || task.Notes != "Line one\nLine two; with semicolon" {
		t.Errorf("text unescape: %q %q", task.Title, task.Notes)
	}
	if task.Start != "2026-10-21T09:00:00" || task.Due != "2026-10-21T09:00:00" {
		t.Errorf("dates: %q %q", task.Start, task.Due)
	}
	if task.RRule != "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR" || task.RepeatFrom != "due" {
		t.Errorf("rrule: %q %q", task.RRule, task.RepeatFrom)
	}
	if len(task.Tags) != 2 || task.Tags[1] != "Home, sweet" {
		t.Errorf("categories: %v", task.Tags)
	}
	if len(task.Alarms) != 1 || task.Alarms[0].Trigger != "PT0S" {
		t.Errorf("alarms: %v", task.Alarms)
	}
	// edit here and write back: unknown properties survive
	task.Title = "HRM edited"
	task.Status = StatusDone
	task.CompletedAt = time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	out := Apply(cal, task, time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)).String()
	for _, want := range []string{"X-BUSYMAC-LASTMODBY:Aleksandar Bjelosevic", "BEGIN:VTIMEZONE", "SUMMARY:HRM edited", "STATUS:COMPLETED", "COMPLETED:20261021T100000Z", "PERCENT-COMPLETE:100", "X-WR-ALARMUID:0BF91AA9", "CATEGORIES:Work,Home\\, sweet", "SEQUENCE:27", "PRODID:-//Apple Inc.//iOS 26.6.2//EN"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "\n\n") || !strings.HasSuffix(out, "END:VCALENDAR\r\n") {
		t.Errorf("bad line ends")
	}
	// and it parses again to the same thing
	cal2, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	t2, _ := FromVTODO(cal2, loc)
	if t2.Title != "HRM edited" || t2.Status != StatusDone || t2.Start != task.Start || len(t2.Tags) != 2 {
		t.Errorf("second parse: %+v", t2)
	}
}

func TestFolding(t *testing.T) {
	c := NewCalendar()
	todo := &Component{Name: "VTODO"}
	long := strings.Repeat("čćžšđ ", 40)
	todo.SetText("SUMMARY", long)
	c.Children = append(c.Children, todo)
	out := c.String()
	for _, line := range strings.Split(out, "\r\n") {
		if len(line) > 75 {
			t.Errorf("line longer than 75 octets: %d", len(line))
		}
	}
	back, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if back.Child("VTODO").Text("SUMMARY") != long {
		t.Errorf("unfold mismatch")
	}
}

func TestZonedAndUTCDates(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Sarajevo")
	cal, _ := Parse("BEGIN:VCALENDAR\r\nBEGIN:VTODO\r\nUID:x\r\nSUMMARY:x\r\nDUE;TZID=America/New_York:20261021T090000\r\nDTSTART:20261021T120000Z\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")
	task, _ := FromVTODO(cal, loc)
	if task.Due != "2026-10-21T15:00:00" { // 09:00 New York = 15:00 Sarajevo (both DST)
		t.Errorf("tzid due: %q", task.Due)
	}
	if task.Start != "2026-10-21T14:00:00" {
		t.Errorf("utc start: %q", task.Start)
	}
	cal2, _ := Parse("BEGIN:VCALENDAR\r\nBEGIN:VTODO\r\nUID:y\r\nSUMMARY:y\r\nDUE;VALUE=DATE:20261105\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")
	t2, _ := FromVTODO(cal2, loc)
	if t2.Due != "2026-11-05" || !IsDay(t2.Due) {
		t.Errorf("date due: %q", t2.Due)
	}
}

func TestRules(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Sarajevo")
	day := func(s string) time.Time { d, _ := time.ParseInLocation(DayLayout, s, loc); return d }
	cases := []struct{ rule, from, want string }{
		{"FREQ=DAILY", "2026-10-20", "2026-10-21"},
		{"FREQ=DAILY;INTERVAL=3", "2026-10-20", "2026-10-23"},
		{"FREQ=WEEKLY", "2026-10-20", "2026-10-27"},
		{"FREQ=WEEKLY;BYDAY=MO,WE,FR", "2026-10-20", "2026-10-21"},
		{"FREQ=WEEKLY;INTERVAL=2;BYDAY=TU", "2026-10-20", "2026-11-03"},
		{"FREQ=MONTHLY", "2026-01-31", "2026-02-28"},
		{"FREQ=MONTHLY;BYMONTHDAY=15", "2026-10-20", "2026-11-15"},
		{"FREQ=MONTHLY;BYDAY=-1FR", "2026-10-20", "2026-10-30"},
		{"FREQ=MONTHLY;BYDAY=2TU", "2026-10-20", "2026-11-10"},
		{"FREQ=YEARLY", "2024-02-29", "2025-02-28"},
		{"FREQ=YEARLY;BYMONTH=12;BYMONTHDAY=24", "2026-10-20", "2026-12-24"},
	}
	for _, c := range cases {
		r, ok := ParseRule(c.rule)
		if !ok {
			t.Fatalf("parse %s", c.rule)
		}
		got, ok := r.Next(day(c.from), day(c.from))
		if !ok || got.Format(DayLayout) != c.want {
			t.Errorf("%s from %s → %s (want %s)", c.rule, c.from, got.Format(DayLayout), c.want)
		}
	}
	r, _ := ParseRule("FREQ=DAILY;COUNT=1")
	if _, ok := r.Next(day("2026-10-20"), day("2026-10-20")); ok {
		t.Error("count=1 should end")
	}
	r, _ = ParseRule("FREQ=DAILY;UNTIL=20261021")
	if n, ok := r.Next(day("2026-10-21"), day("2026-10-21")); ok {
		t.Errorf("until passed, got %s", n)
	}
	r, _ = ParseRule("FREQ=WEEKLY;BYDAY=MO")
	n, _ := r.NextAfterCompletion(day("2026-10-13"), day("2026-10-20"))
	if n.Format(DayLayout) != "2026-10-26" {
		t.Errorf("after completion: %s", n.Format(DayLayout))
	}
	r, _ = ParseRule("FREQ=MONTHLY;INTERVAL=2")
	if r.Describe() != "every 2 months" {
		t.Errorf("describe: %q", r.Describe())
	}
}

func TestDurations(t *testing.T) {
	for in, want := range map[string]time.Duration{"-PT15M": -15 * time.Minute, "PT0S": 0, "P1DT2H": 26 * time.Hour, "P2W": 14 * 24 * time.Hour, "-P1D": -24 * time.Hour} {
		d, err := Duration(in)
		if err != nil || d != want {
			t.Errorf("%s → %v %v", in, d, err)
		}
		if FormatDuration(d) != in && !(in == "P2W") {
			t.Errorf("format %s → %s", in, FormatDuration(d))
		}
	}
}

func TestAlarmTime(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Sarajevo")
	task := &Task{Due: "2026-10-21"}
	at, ok := AlarmTime(Alarm{Trigger: "-PT1H"}, task, loc, "09:00")
	if !ok || at.Format(TimeLayout) != "2026-10-21T08:00:00" {
		t.Errorf("all-day alarm: %v", at)
	}
	task = &Task{Start: "2026-10-21T10:00:00", Due: "2026-10-21T12:00:00"}
	at, _ = AlarmTime(Alarm{Trigger: "-PT30M"}, task, loc, "09:00")
	if at.Format(TimeLayout) != "2026-10-21T11:30:00" {
		t.Errorf("due-relative alarm: %v", at)
	}
}
