package quick

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	now := time.Date(2026, 10, 20, 14, 0, 0, 0, time.UTC) // a Tuesday
	cases := []struct {
		in    string
		title string
		start string
		due   string
		check func(p Parsed) bool
	}{
		{"Call the bank tomorrow 9am !high #money /Home", "Call the bank", "2026-10-21T09:00:00", "", func(p Parsed) bool {
			return p.Priority == 1 && len(p.Tags) == 1 && p.Tags[0] == "money" && p.List == "Home"
		}},
		{"Pay rent due 1 nov every month", "Pay rent", "", "2026-11-01", func(p Parsed) bool { return p.RRule == "FREQ=MONTHLY" }},
		{"Water plants every 3 days after done", "Water plants", "", "", func(p Parsed) bool { return p.RRule == "FREQ=DAILY;INTERVAL=3" && p.RepeatFrom == "completion" }},
		{"Gym every monday ~45m", "Gym", "2026-10-26", "", func(p Parsed) bool { return p.RRule == "FREQ=WEEKLY;BYDAY=MO" && p.EstimateMin == 45 }},
		{"Write report by friday 17:00 *1h", "Write report", "", "2026-10-23T17:00:00", func(p Parsed) bool { return p.Alarm == "-PT1H" }},
		{"Read a book someday #reading", "Read a book", "", "", func(p Parsed) bool { return p.Someday }},
		{"Dentist 2026-11-03 at 10:30", "Dentist", "2026-11-03T10:30:00", "", nil},
		{"Taxes 21.4. // bring the folder", "Taxes", "2027-04-21", "", func(p Parsed) bool { return p.Notes == "bring the folder" }},
		{"Buy milk", "Buy milk", "", "", func(p Parsed) bool { return p.Start == "" && p.Due == "" }},
		{"Call at 9", "Call", "2026-10-21T09:00:00", "", nil},
		{"Meeting in 2 days !!", "Meeting", "2026-10-22", "", func(p Parsed) bool { return p.Priority == 5 }},
		{"Order 3 pizzas", "Order 3 pizzas", "", "", nil},
		{"Standup every weekday 9:15", "Standup", "2026-10-21T09:15:00", "", func(p Parsed) bool { return p.RRule == "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR" }},
	}
	for _, c := range cases {
		p := Parse(c.in, now)
		if p.Title != c.title || p.Start != c.start || p.Due != c.due || (c.check != nil && !c.check(p)) {
			t.Errorf("%q → %+v (want title %q start %q due %q)", c.in, p, c.title, c.start, c.due)
		}
	}
}
