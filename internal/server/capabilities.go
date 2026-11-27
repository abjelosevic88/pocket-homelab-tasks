package server

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-tasks/internal/ical"
	"github.com/abjelosevic88/pocket-homelab-tasks/internal/store"
)

// The four routes behind `capabilities` (links.go): /api/v1/pocket/search,
// /day/{date}, /widget and /capture. Each runs as the request's person and
// sees only their tasks; the SDK's handlers check and cap what goes in and out.

// taskURL opens a task in the app: its list, with the task's panel open
// (what the command palette links to).
func taskURL(t *store.Task) string {
	return fmt.Sprintf("/list/%d?task=%d", t.ListID, t.ID)
}

func listName(t *store.Task) string {
	if t.List != nil {
		return t.List.Name
	}
	return ""
}

// ---------- search ----------

// pocketSearch is the search behind GET /search: open tasks before finished
// ones, best match first.
func (s *Server) pocketSearch(r *http.Request, q string, limit int) ([]core.SearchItem, error) {
	ts, err := s.store.Search(r.Context(), me(r), q, limit)
	if err != nil {
		return nil, err
	}
	out := make([]core.SearchItem, 0, len(ts))
	for _, t := range ts {
		it := core.SearchItem{ID: strconv.FormatInt(t.ID, 10), Title: t.Title, Snippet: plainText(t.Notes), Kind: "task", Date: t.UpdatedAt, URL: taskURL(t)}
		if it.Snippet == "" {
			it.Snippet = listName(t)
		}
		if t.Due != nil {
			it.Date = ical.Day(*t.Due)
		}
		out = append(out, it)
	}
	return out, nil
}

var (
	mdLink   = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	mdPrefix = regexp.MustCompile(`(?m)^\s*(#{1,6}\s+|>\s?|[-*+]\s+(\[[ xX]\]\s+)?|\d+[.)]\s+)`)
	mdMarks  = regexp.MustCompile("\\*{1,3}|~~|`+")
)

// plainText takes the Markdown out of notes, for a line of context.
func plainText(notes string) string {
	if len(notes) > 4000 {
		notes = strings.ToValidUTF8(notes[:4000], "")
	}
	notes = mdLink.ReplaceAllString(notes, "$1")
	notes = mdPrefix.ReplaceAllString(notes, "")
	notes = mdMarks.ReplaceAllString(notes, "")
	return strings.Join(strings.Fields(notes), " ")
}

// ---------- day ----------

// dayBounds gives the UTC bounds of a local day as stored timestamps compare
// (no zone letter, so "…:00Z" and "…:00.123Z" both sort after the bound).
func dayBounds(date string, loc *time.Location) (from, to string, ok bool) {
	d, err := time.ParseInLocation(ical.DayLayout, date, loc)
	if err != nil {
		return "", "", false
	}
	const layout = "2006-01-02T15:04:05"
	return d.UTC().Format(layout), d.AddDate(0, 0, 1).UTC().Format(layout), true
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// pocketDay says what happened with the person's tasks on one day: the ones
// finished that day, the ones due that day and still open, the ones added.
// A task shows once, as the most telling of the three.
func (s *Server) pocketDay(r *http.Request, date string, tz *time.Location) (*core.Day, error) {
	ctx, uid := r.Context(), me(r)
	today, _, loc := s.today(ctx, uid)
	if tz != nil {
		loc = tz
		today = time.Now().In(loc).Format(ical.DayLayout)
	}
	from, to, ok := dayBounds(date, loc)
	if !ok {
		return nil, nil
	}
	clock := func(stamp string) string {
		if at := store.ParseTime(stamp); !at.IsZero() {
			return at.In(loc).Format("15:04")
		}
		return ""
	}
	done, nDone, err := s.store.ListTasks(ctx, uid, store.Query{Status: "done", CompletedOn: from + "|" + to, Limit: 200}, 0)
	if err != nil {
		return nil, err
	}
	due, nDue, err := s.store.ListTasks(ctx, uid, store.Query{Status: "open", DueFrom: date, DueTo: date, Limit: 200}, 0)
	if err != nil {
		return nil, err
	}
	added, nAdded, err := s.store.ListTasks(ctx, uid, store.Query{Status: "all", CreatedOn: from + "|" + to, Limit: 200}, 0)
	if err != nil {
		return nil, err
	}
	if nDone+nDue+nAdded == 0 {
		return nil, nil
	}
	sort.SliceStable(done, func(i, j int) bool { return *done[i].CompletedAt < *done[j].CompletedAt })
	sort.SliceStable(added, func(i, j int) bool { return added[i].CreatedAt < added[j].CreatedAt })

	day := &core.Day{Date: date}
	seen := map[int64]bool{}
	add := func(t *store.Task, kind, at string) {
		if seen[t.ID] {
			return
		}
		seen[t.ID] = true
		day.Items = append(day.Items, core.DayItem{ID: strconv.FormatInt(t.ID, 10), Time: at, Title: t.Title, Sub: listName(t), Kind: kind, Done: kind == "done", URL: taskURL(t)})
	}
	for _, t := range done {
		add(t, "done", clock(*t.CompletedAt))
	}
	for _, t := range due {
		at := ""
		if !ical.IsDay(*t.Due) && len(*t.Due) >= 16 {
			at = (*t.Due)[11:16]
		}
		add(t, "due", at)
	}
	for _, t := range added {
		add(t, "added", clock(t.CreatedAt))
	}

	var parts []string
	if nDone > 0 {
		parts = append(parts, strconv.Itoa(nDone)+" done")
		day.Metrics = append(day.Metrics, core.Metric{Label: "Done", Value: strconv.Itoa(nDone)})
	}
	if nDue > 0 {
		// on a day still to come nothing is "still" due
		if date > today {
			parts = append(parts, strconv.Itoa(nDue)+" due")
		} else {
			parts = append(parts, strconv.Itoa(nDue)+" still due")
		}
		day.Metrics = append(day.Metrics, core.Metric{Label: "Due", Value: strconv.Itoa(nDue)})
	}
	if nAdded > 0 {
		parts = append(parts, strconv.Itoa(nAdded)+" added")
		day.Metrics = append(day.Metrics, core.Metric{Label: "Added", Value: strconv.Itoa(nAdded)})
	}
	day.Summary = strings.Join(parts, ", ")
	return day, nil
}

// ---------- widget ----------

// dayWord names a day next to a task: "tomorrow", "Mon 30 Nov".
func dayWord(day, today string, loc *time.Location) string {
	d, err := time.ParseInLocation(ical.DayLayout, day, loc)
	if err != nil {
		return day
	}
	t, _ := time.ParseInLocation(ical.DayLayout, today, loc)
	switch {
	case d.Equal(t.AddDate(0, 0, 1)):
		return "tomorrow"
	case d.Year() == t.Year():
		return d.Format("Mon 2 Jan")
	}
	return d.Format("2 Jan 2006")
}

// pocketWidget is the dashboard in a few lines: its counts (today, overdue,
// done today), then the tasks of Today, late ones first, and what comes next
// when there is room.
func (s *Server) pocketWidget(r *http.Request) (*core.WidgetData, error) {
	ctx, uid := r.Context(), me(r)
	d, err := s.dashboard(ctx, uid)
	if err != nil {
		return nil, err
	}
	today, _, loc := s.today(ctx, uid)
	// the dashboard's "today" counts late tasks too
	late := d.Overdue
	dueToday := max(d.Today-late, 0)

	out := &core.WidgetData{URL: "/today", Metrics: []core.Metric{
		{Label: "Today", Value: strconv.Itoa(dueToday)},
		{Label: "Late", Value: strconv.Itoa(late)},
		{Label: "Done today", Value: strconv.Itoa(d.DoneToday)},
	}}
	var parts []string
	if dueToday > 0 {
		parts = append(parts, strconv.Itoa(dueToday)+" due today")
	}
	if late > 0 {
		parts = append(parts, strconv.Itoa(late)+" late")
	}
	switch {
	case len(parts) > 0:
		out.Line = strings.Join(parts, ", ")
	case d.DoneToday > 0:
		out.Line = strconv.Itoa(d.DoneToday) + " done today"
	default:
		out.Line = "Nothing due today"
	}

	const rows = 8
	now, _, err := s.store.ListTasks(ctx, uid, store.Query{View: "today", Today: today, Limit: rows}, 0)
	if err != nil {
		return nil, err
	}
	for _, t := range now {
		due, start := ical.Day(t.DueStr()), ical.Day(t.StartStr())
		detail := listName(t)
		switch {
		case due != "" && due < today:
			detail = "late"
		case due == today || start == today:
			detail = "today"
		case due != "":
			detail = "due " + dayWord(due, today, loc)
		}
		out.Items = append(out.Items, core.WidgetItem{ID: strconv.FormatInt(t.ID, 10), Title: t.Title, Detail: detail, URL: taskURL(t)})
	}
	if room := rows - len(out.Items); room > 0 {
		next, _, err := s.store.ListTasks(ctx, uid, store.Query{View: "upcoming", Today: today, Limit: room}, 0)
		if err != nil {
			return nil, err
		}
		for _, t := range next {
			out.Items = append(out.Items, core.WidgetItem{ID: strconv.FormatInt(t.ID, 10), Title: t.Title, Detail: dayWord(effectiveDay(t), today, loc), URL: taskURL(t)})
		}
	}
	return out, nil
}

// ---------- capture ----------

// captureTitle splits a quick line into a task's title and notes: the title
// when one was given (the text is then the notes), else the text's first line.
func captureTitle(in core.CaptureInput) (title, notes string) {
	if in.Title != "" {
		return strings.Join(strings.Fields(in.Title), " "), in.Text
	}
	title, notes, _ = strings.Cut(in.Text, "\n")
	title, notes = strings.Join(strings.Fields(title), " "), strings.TrimSpace(notes)
	if r := []rune(title); len(r) > 300 {
		// a long paragraph: keep all of it as the notes
		return strings.TrimSpace(string(r[:299])) + "…", in.Text
	}
	return title, notes
}

// pocketCapture keeps a quick line as a new task, where POST /tasks without a
// list puts one (the person's default list, in Today unless it has a due
// day). The same request_id again answers the same task and adds nothing: the
// receipt is written in the transaction that adds the task.
func (s *Server) pocketCapture(r *http.Request, in core.CaptureInput) (*core.CaptureResult, error) {
	ctx, uid := r.Context(), me(r)
	op, err := core.NewOperation(fmt.Sprintf("tasks:user:%d:%s", uid, core.CapturePath), in.RequestID, in)
	if err != nil {
		return nil, err
	}
	var res core.CaptureResult
	replay := func() (*core.CaptureResult, error) {
		found, err := op.Replay(ctx, s.store.DB, &res)
		switch {
		case errors.Is(err, core.ErrOperationConflict):
			return nil, fmt.Errorf("%w: this was already kept with other words", core.ErrCapture)
		case err != nil || !found:
			return nil, err
		}
		return &res, nil
	}
	if kept, err := replay(); kept != nil || err != nil {
		return kept, err
	}

	st, _ := s.store.GetSettings(ctx, uid)
	t := &store.Task{UserID: uid, Status: ical.StatusOpen, Due: optStr(in.Due), Tags: []string{}, Alarms: []ical.Alarm{}}
	t.Title, t.Notes = captureTitle(in)
	if t.Title == "" {
		return nil, fmt.Errorf("%w: a task needs a title", core.ErrCapture)
	}
	t.ListID = s.defaultList(ctx, uid, st)
	s.startToday(ctx, t)
	err = s.saveTask(ctx, t, "capture", "created", func(tx *sql.Tx) error {
		res = core.CaptureResult{ID: strconv.FormatInt(t.ID, 10), Title: t.Title, URL: taskURL(t)}
		return op.Save(ctx, tx, res)
	})
	if err != nil {
		// the same line sent twice at once: the other one kept it
		if kept, rerr := replay(); kept != nil || rerr != nil {
			return kept, rerr
		}
		return nil, err
	}
	return &res, nil
}
