package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-tasks/internal/ical"
	"github.com/abjelosevic88/pocket-homelab-tasks/internal/quick"
	"github.com/abjelosevic88/pocket-homelab-tasks/internal/store"
)

// ---------- between the index and iCalendar ----------

// toICal builds the iCalendar view of a task from its index columns.
func (s *Server) toICal(ctx context.Context, t *store.Task) *ical.Task {
	it := &ical.Task{
		UID: t.UID, Title: t.Title, Notes: t.Notes, Status: t.Status, Percent: t.Percent, Priority: t.Priority,
		Start: t.StartStr(), Due: t.DueStr(), Someday: t.Someday, SortOrder: t.SortOrder, HasOrder: true,
		Location: t.Location, URL: t.URL, Tags: t.Tags, Alarms: t.Alarms,
	}
	if t.EstimateMin != nil {
		it.EstimateMin = *t.EstimateMin
	}
	if t.Repeat != nil {
		it.RRule, it.RepeatFrom = t.Repeat.RRule, t.Repeat.From
	}
	if t.CompletedAt != nil {
		it.CompletedAt = store.ParseTime(*t.CompletedAt)
	}
	if t.ParentID != nil {
		if p, err := s.store.GetTask(ctx, t.UserID, *t.ParentID); err == nil {
			it.ParentUID = p.UID
		}
	}
	if t.SectionID != nil {
		if sec, err := s.store.GetSection(ctx, t.UserID, *t.SectionID); err == nil {
			it.Section = sec.Name
		}
	}
	it.Created = store.ParseTime(t.CreatedAt)
	return it
}

// fromICal fills the index columns of t from a parsed VTODO (a client wrote
// it over CalDAV or it came from Nextcloud). Parent and section are resolved
// by UID and name within the task's list.
func (s *Server) fromICal(ctx context.Context, it *ical.Task, t *store.Task) {
	t.UID, t.Title, t.Notes, t.Status, t.Percent, t.Priority = it.UID, it.Title, it.Notes, it.Status, it.Percent, it.Priority
	t.Start, t.Due = optStr(it.Start), optStr(it.Due)
	if !it.CompletedAt.IsZero() && it.Status == ical.StatusDone {
		c := store.FormatTime(it.CompletedAt)
		t.CompletedAt = &c
	} else {
		t.CompletedAt = nil
	}
	t.EstimateMin = nil
	if it.EstimateMin > 0 {
		e := it.EstimateMin
		t.EstimateMin = &e
	}
	t.Repeat = nil
	if it.RRule != "" {
		t.Repeat = &store.Repeat{RRule: it.RRule, From: it.RepeatFrom}
	}
	t.Someday, t.Location, t.URL = it.Someday, it.Location, it.URL
	t.Tags = append([]string{}, it.Tags...)
	t.Alarms = append([]ical.Alarm{}, it.Alarms...)
	if it.HasOrder {
		t.SortOrder = it.SortOrder
	}
	t.ParentID = nil
	if it.ParentUID != "" && it.ParentUID != it.UID {
		if p, err := s.store.GetTaskByUID(ctx, t.UserID, it.ParentUID); err == nil && p.ID != t.ID {
			t.ParentID = &p.ID
			if p.ListID != t.ListID {
				t.ListID = p.ListID
			}
		}
	}
	t.SectionID = nil
	if it.Section != "" {
		if sec, err := s.store.SectionByName(ctx, t.UserID, t.ListID, it.Section); err == nil {
			t.SectionID = &sec.ID
		}
	}
	if t.CreatedAt == "" && !it.Created.IsZero() {
		t.CreatedAt = store.FormatTime(it.Created)
	}
}

func optStr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

// render writes the task's managed properties into its iCalendar text.
func (s *Server) render(ctx context.Context, t *store.Task) {
	var cal *ical.Component
	if t.ICS != "" {
		cal, _ = ical.Parse(t.ICS)
	}
	it := s.toICal(ctx, t)
	it.Sequence = sequenceOf(cal) + 1
	cal = ical.Apply(cal, it, time.Now())
	t.ICS = cal.String()
	t.ETag = ical.ETag(t.ICS)
	if t.Href == "" {
		t.Href = t.UID + ".ics"
	}
}

func sequenceOf(cal *ical.Component) int {
	if cal == nil {
		return 0
	}
	if todo := cal.Child("VTODO"); todo != nil {
		var n int
		fmt.Sscanf(todo.Value("SEQUENCE"), "%d", &n)
		return n
	}
	return 0
}

// saveTask stores a task (new or changed) from the index side: renders the
// iCalendar, writes the row, recomputes alarms and tells the world.
func (s *Server) saveTask(ctx context.Context, t *store.Task, source, kind string) error {
	if t.UID == "" {
		t.UID = strings.ToUpper(newUUID())
	}
	t.Title = strings.TrimSpace(t.Title)
	if t.Title == "" {
		t.Title = "New task"
	}
	s.render(ctx, t)
	isNew := t.ID == 0
	if kind == "" {
		if isNew {
			kind = "created"
		} else {
			kind = "updated"
		}
	}
	hook := s.taskEvent(ctx, t, kind, source, nil)
	var err error
	if isNew {
		err = s.store.InsertTask(ctx, t, hook)
	} else {
		err = s.store.UpdateTask(ctx, t, hook)
	}
	if err != nil {
		return err
	}
	s.scheduleAlarms(ctx, t)
	s.touch()
	go s.syncSoon(t.UserID)
	s.store.AddActivity(ctx, t.UserID, t.ID, kind, source)
	return nil
}

// scheduleAlarms computes when each alarm of a task fires.
func (s *Server) scheduleAlarms(ctx context.Context, t *store.Task) {
	if len(t.Alarms) == 0 {
		return
	}
	st, _ := s.store.GetSettings(ctx, t.UserID)
	loc := s.loc(ctx, t.UserID)
	it := &ical.Task{Start: t.StartStr(), Due: t.DueStr()}
	at := map[string]string{}
	for _, a := range t.Alarms {
		if !t.Open() {
			at[a.Trigger] = ""
			continue
		}
		if when, ok := ical.AlarmTime(a, it, loc, st.DayStart); ok {
			at[a.Trigger] = when.UTC().Format(time.RFC3339)
		} else {
			at[a.Trigger] = ""
		}
	}
	_ = s.store.SetAlarmTimes(ctx, t.ID, at)
}

func newUUID() string {
	b := store.RandomToken(16)
	return b[0:8] + "-" + b[8:12] + "-4" + b[13:16] + "-" + b[16:20] + "-" + b[20:32]
}

// ---------- input ----------

// patch applies a JSON object onto a task: keys present are changed, an
// explicit null clears. It returns what the body said about tags/alarms.
func (s *Server) patch(ctx context.Context, t *store.Task, raw map[string]json.RawMessage) error {
	uid := t.UserID
	has := func(k string) bool { _, ok := raw[k]; return ok }
	isNull := func(k string) bool { return strings.TrimSpace(string(raw[k])) == "null" }
	str := func(k string) (string, error) {
		var v string
		if isNull(k) {
			return "", nil
		}
		return v, json.Unmarshal(raw[k], &v)
	}
	num := func(k string) (int64, error) {
		var v float64
		if isNull(k) {
			return 0, nil
		}
		err := json.Unmarshal(raw[k], &v)
		return int64(v), err
	}
	if has("title") {
		v, err := str("title")
		if err != nil {
			return badRequest("title must be text")
		}
		t.Title = v
	}
	if has("notes") {
		v, err := str("notes")
		if err != nil {
			return badRequest("notes must be text")
		}
		t.Notes = v
	}
	if has("list_id") {
		id, err := num("list_id")
		if err != nil || id == 0 {
			return badRequest("list_id must be a list id")
		}
		if _, err := s.store.GetList(ctx, uid, id); err != nil {
			return badRequest("no such list")
		}
		if id != t.ListID {
			t.ListID = id
			t.SectionID = nil
			if t.ParentID != nil {
				t.ParentID = nil
			}
		}
	}
	if has("section_id") {
		id, err := num("section_id")
		if err != nil {
			return badRequest("section_id must be a section id")
		}
		if id == 0 {
			t.SectionID = nil
		} else {
			sec, err := s.store.GetSection(ctx, uid, id)
			if err != nil || sec.ListID != t.ListID {
				return badRequest("no such section in that list")
			}
			t.SectionID = &id
		}
	}
	if has("parent_id") {
		id, err := num("parent_id")
		if err != nil {
			return badRequest("parent_id must be a task id")
		}
		if id == 0 {
			t.ParentID = nil
		} else {
			p, err := s.store.GetTask(ctx, uid, id)
			if err != nil || p.DeletedAt != "" || id == t.ID {
				return badRequest("no such task to nest under")
			}
			if p.ParentID != nil && *p.ParentID == t.ID {
				return badRequest("a task cannot be nested under its own subtask")
			}
			t.ParentID = &id
			t.ListID = p.ListID
			t.SectionID = nil
		}
	}
	if has("status") {
		v, _ := str("status")
		switch v {
		case ical.StatusOpen, ical.StatusInProgress, ical.StatusDone, ical.StatusCancelled:
		default:
			return badRequest("status must be open, in_progress, done or cancelled")
		}
		if v == ical.StatusDone && t.Status != ical.StatusDone {
			now := store.Now()
			t.CompletedAt = &now
		}
		if v != ical.StatusDone {
			t.CompletedAt = nil
		}
		t.Status = v
	}
	if has("completed_at") {
		v, _ := str("completed_at")
		if v != "" {
			if ts := store.ParseTime(v); !ts.IsZero() {
				f := store.FormatTime(ts)
				t.CompletedAt = &f
			}
		}
	}
	if has("percent") {
		v, _ := num("percent")
		if v < 0 || v > 100 {
			return badRequest("percent must be 0–100")
		}
		t.Percent = int(v)
	}
	if has("priority") {
		v, _ := num("priority")
		if v < 0 || v > 9 {
			return badRequest("priority must be 0–9")
		}
		t.Priority = int(v)
	}
	for _, k := range []string{"start", "due"} {
		if !has(k) {
			continue
		}
		v, err := str(k)
		if err != nil || !ical.ValidLocal(v) {
			return badRequest(k + " must be YYYY-MM-DD or YYYY-MM-DDTHH:MM:SS")
		}
		if v != "" && len(v) == 16 {
			v += ":00"
		}
		if k == "start" {
			t.Start = optStr(v)
		} else {
			t.Due = optStr(v)
		}
	}
	if has("estimate_min") {
		v, _ := num("estimate_min")
		t.EstimateMin = nil
		if v > 0 {
			n := int(v)
			t.EstimateMin = &n
		}
	}
	if has("repeat") {
		if isNull("repeat") {
			t.Repeat = nil
		} else {
			var rp store.Repeat
			if err := json.Unmarshal(raw["repeat"], &rp); err != nil {
				return badRequest("repeat must be {rrule, from}")
			}
			rp.RRule = strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(rp.RRule)), "RRULE:")
			if rp.RRule == "" {
				t.Repeat = nil
			} else {
				if _, ok := ical.ParseRule(rp.RRule); !ok {
					return badRequest("the repeat rule is not understood (FREQ=DAILY|WEEKLY|MONTHLY|YEARLY…)")
				}
				if rp.From != "completion" {
					rp.From = "due"
				}
				t.Repeat = &rp
			}
		}
	}
	if has("someday") {
		var v bool
		_ = json.Unmarshal(raw["someday"], &v)
		t.Someday = v
	}
	if has("tags") {
		var v []string
		if !isNull("tags") {
			if err := json.Unmarshal(raw["tags"], &v); err != nil {
				return badRequest("tags must be a list of words")
			}
		}
		t.Tags = cleanTags(v)
	}
	if has("alarms") {
		var v []ical.Alarm
		if !isNull("alarms") {
			if err := json.Unmarshal(raw["alarms"], &v); err != nil {
				return badRequest("alarms must be a list of {trigger}")
			}
		}
		var out []ical.Alarm
		for _, a := range v {
			a.Trigger = strings.TrimSpace(a.Trigger)
			if _, err := time.Parse(time.RFC3339, a.Trigger); err != nil {
				if _, err := ical.Duration(a.Trigger); err != nil {
					return badRequest("an alarm trigger is -PT15M, PT0S, P1D… or a UTC time")
				}
			}
			out = append(out, a)
		}
		t.Alarms = out
	}
	if has("location") {
		t.Location, _ = str("location")
	}
	if has("url") {
		t.URL, _ = str("url")
	}
	if has("sort_order") {
		var v float64
		_ = json.Unmarshal(raw["sort_order"], &v)
		t.SortOrder = v
	}
	if t.Tags == nil {
		t.Tags = []string{}
	}
	if t.Alarms == nil {
		t.Alarms = []ical.Alarm{}
	}
	return nil
}

func cleanTags(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, tag := range in {
		tag = strings.TrimSpace(strings.TrimPrefix(tag, "#"))
		if tag == "" || seen[strings.ToLower(tag)] {
			continue
		}
		seen[strings.ToLower(tag)] = true
		out = append(out, tag)
	}
	return out
}

// ---------- handlers ----------

func (s *Server) today(ctx context.Context, uid int64) (today string, now string, loc *time.Location) {
	loc = s.loc(ctx, uid)
	n := time.Now().In(loc)
	return n.Format(ical.DayLayout), n.Format(ical.TimeLayout), loc
}

func (s *Server) decorate(t *store.Task, today, now string) {
	t.Overdue = false
	if t.Open() && t.Due != nil {
		d := *t.Due
		if ical.IsDay(d) {
			t.Overdue = d < today
		} else {
			t.Overdue = d < now
		}
	}
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	qv := r.URL.Query()
	var q store.Query
	if fid := queryInt64(r, "filter_id"); fid > 0 {
		f, err := s.store.GetFilter(ctx, uid, fid)
		if err != nil {
			s.fail(w, err, "filter")
			return
		}
		q = f.Query
	}
	if v := qv.Get("view"); v != "" {
		q.View = v
	}
	if v := queryInt64(r, "list_id"); v > 0 {
		q.ListID = v
	}
	if qv.Has("section_id") {
		v := queryInt64(r, "section_id")
		q.SectionID = &v
	}
	if qv.Has("parent_id") {
		v := queryInt64(r, "parent_id")
		q.ParentID = &v
	}
	if v := qv.Get("tag"); v != "" {
		q.Tag = v
	}
	if v := qv.Get("status"); v != "" {
		q.Status = v
	}
	if qv.Has("priority") {
		v := queryInt(r, "priority")
		q.Priority = &v
	}
	if v := qv.Get("q"); v != "" {
		q.Q = v
	}
	for k, dst := range map[string]*string{"due_from": &q.DueFrom, "due_to": &q.DueTo, "start_from": &q.StartFrom, "start_to": &q.StartTo, "modified_since": &q.ModifiedAt} {
		if v := qv.Get(k); v != "" {
			*dst = v
		}
	}
	if qv.Has("someday") {
		v := queryBool(r, "someday")
		q.Someday = &v
	}
	if qv.Has("has_date") {
		v := queryBool(r, "has_date")
		q.HasDate = &v
	}
	if v := qv.Get("completed_on"); v != "" {
		loc := s.loc(ctx, uid)
		if d, err := time.ParseInLocation(ical.DayLayout, v, loc); err == nil {
			q.CompletedOn = store.FormatTime(d) + "|" + store.FormatTime(d.AddDate(0, 0, 1))
		}
	}
	if raws := qv["url"]; len(raws) > 0 {
		for _, raw := range raws {
			if k := store.ItemKey(raw); k != "" {
				q.Links = append(q.Links, k)
			}
		}
		if len(q.Links) == 0 || len(q.Links) > 20 {
			writeErr(w, http.StatusBadRequest, "give one to 20 web addresses as url=")
			return
		}
	}
	q.Limit, q.Offset = queryInt(r, "limit"), queryInt(r, "offset")
	tasks, groups, total, err := s.listTasks(ctx, uid, q, queryBool(r, "nest"))
	if err != nil {
		s.fail(w, err, "list tasks")
		return
	}
	out := map[string]any{"tasks": tasks, "total": total}
	if groups != nil {
		out["groups"] = groups
	}
	writeJSON(w, http.StatusOK, out)
}

// listTasks runs a query and builds the groups the view wants.
func (s *Server) listTasks(ctx context.Context, uid int64, q store.Query, nest bool) ([]*store.Task, []store.Group, int, error) {
	today, now, loc := s.today(ctx, uid)
	q.Today, q.Now = today, now
	inbox, err := s.store.Inbox(ctx, uid)
	if err != nil {
		return nil, nil, 0, err
	}
	if q.View != "" && q.View != "all" && q.View != "logbook" && q.ListID == 0 && q.ParentID == nil && q.View != "inbox" {
		// smart views show subtasks only under their parent when the parent is in the view too
	}
	tasks, total, err := s.store.ListTasks(ctx, uid, q, inbox.ID)
	if err != nil {
		return nil, nil, 0, err
	}
	for _, t := range tasks {
		s.decorate(t, today, now)
	}
	if nest {
		tasks = nestSubtasks(tasks)
	}
	st, _ := s.store.GetSettings(ctx, uid)
	var groups []store.Group
	switch {
	case q.View == "today":
		groups = groupBy(tasks, func(t *store.Task) (string, string) {
			if t.Overdue {
				return "overdue", "Overdue"
			}
			return "today", "Today"
		})
	case q.View == "upcoming":
		groups = groupBy(tasks, func(t *store.Task) (string, string) {
			d := effectiveDay(t)
			return d, dayTitle(d, today, loc)
		})
	case q.View == "anytime" || q.View == "someday":
		groups = groupBy(tasks, func(t *store.Task) (string, string) { return fmt.Sprintf("%d", t.ListID), t.List.Name })
	case q.View == "logbook":
		if st.LogbookGroup == "list" {
			groups = groupBy(tasks, func(t *store.Task) (string, string) { return fmt.Sprintf("%d", t.ListID), t.List.Name })
		} else {
			groups = groupBy(tasks, func(t *store.Task) (string, string) {
				d := ""
				if t.CompletedAt != nil {
					d = store.ParseTime(*t.CompletedAt).In(loc).Format(ical.DayLayout)
				}
				return d, dayTitle(d, today, loc)
			})
		}
	case q.ListID > 0 && q.View == "":
		secs, _ := s.store.ListSections(ctx, uid, q.ListID)
		names := map[int64]string{}
		order := map[int64]int{}
		for i, sec := range secs {
			names[sec.ID], order[sec.ID] = sec.Name, i+1
		}
		groups = groupBy(tasks, func(t *store.Task) (string, string) {
			if t.SectionID == nil {
				return "none", ""
			}
			return fmt.Sprintf("section:%d", *t.SectionID), names[*t.SectionID]
		})
		sort.SliceStable(groups, func(i, j int) bool {
			oi, oj := 0, 0
			if groups[i].Key != "none" {
				fmt.Sscanf(groups[i].Key, "section:%d", &oi)
				oi = order[int64(oi)]
			}
			if groups[j].Key != "none" {
				fmt.Sscanf(groups[j].Key, "section:%d", &oj)
				oj = order[int64(oj)]
			}
			return oi < oj
		})
		// every section appears, even when empty, so the UI can add to it
		for _, sec := range secs {
			key := fmt.Sprintf("section:%d", sec.ID)
			found := false
			for _, g := range groups {
				if g.Key == key {
					found = true
					break
				}
			}
			if !found {
				groups = append(groups, store.Group{Key: key, Title: sec.Name, IDs: []int64{}})
			}
		}
	}
	return tasks, groups, total, nil
}

func effectiveDay(t *store.Task) string {
	st, du := t.StartStr(), t.DueStr()
	switch {
	case st != "" && du != "":
		if ical.Day(st) < ical.Day(du) {
			return ical.Day(st)
		}
		return ical.Day(du)
	case st != "":
		return ical.Day(st)
	default:
		return ical.Day(du)
	}
}

func dayTitle(day, today string, loc *time.Location) string {
	if day == "" {
		return "Undated"
	}
	d, err := time.ParseInLocation(ical.DayLayout, day, loc)
	if err != nil {
		return day
	}
	t, _ := time.ParseInLocation(ical.DayLayout, today, loc)
	diff := int(d.Sub(t).Hours() / 24)
	switch {
	case diff == 0:
		return "Today · " + d.Format("Mon 2 Jan")
	case diff == 1:
		return "Tomorrow · " + d.Format("Mon 2 Jan")
	case diff == -1:
		return "Yesterday · " + d.Format("Mon 2 Jan")
	case diff > 1 && diff < 7:
		return d.Format("Monday · 2 Jan")
	case d.Year() == t.Year():
		return d.Format("Mon 2 Jan")
	}
	return d.Format("Mon 2 Jan 2006")
}

func groupBy(tasks []*store.Task, key func(*store.Task) (string, string)) []store.Group {
	var out []store.Group
	idx := map[string]int{}
	for _, t := range tasks {
		if t.ParentID != nil && hasParentIn(tasks, *t.ParentID) {
			continue
		}
		k, title := key(t)
		i, ok := idx[k]
		if !ok {
			i = len(out)
			idx[k] = i
			out = append(out, store.Group{Key: k, Title: title, IDs: []int64{}})
		}
		out[i].IDs = append(out[i].IDs, t.ID)
	}
	if out == nil {
		out = []store.Group{}
	}
	return out
}

func hasParentIn(tasks []*store.Task, id int64) bool {
	for _, t := range tasks {
		if t.ID == id {
			return true
		}
	}
	return false
}

// nestSubtasks moves children under their parent when both are in the answer.
func nestSubtasks(tasks []*store.Task) []*store.Task {
	byID := map[int64]*store.Task{}
	for _, t := range tasks {
		byID[t.ID] = t
	}
	var top []*store.Task
	for _, t := range tasks {
		if t.ParentID != nil {
			if p := byID[*t.ParentID]; p != nil {
				p.Subtasks = append(p.Subtasks, t)
				continue
			}
		}
		top = append(top, t)
	}
	if top == nil {
		top = []*store.Task{}
	}
	return top
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	t, err := s.store.GetTask(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get task")
		return
	}
	today, now, _ := s.today(ctx, uid)
	s.decorate(t, today, now)
	subs, _ := s.store.Subtasks(ctx, uid, t.ID)
	for _, c := range subs {
		s.decorate(c, today, now)
	}
	if subs == nil {
		subs = []*store.Task{}
	}
	t.Subtasks = subs
	act, _ := s.store.ListActivity(ctx, uid, t.ID, 50)
	writeJSON(w, http.StatusOK, struct {
		*store.Task
		Activity []store.Activity `json:"activity"`
	}{t, act})
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	var raw map[string]json.RawMessage
	if err := decode(r, &raw); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	t, err := s.newTask(ctx, uid, raw)
	if err != nil {
		s.fail(w, err, "create task")
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

// newTask makes a task from a JSON object; a "text" field goes through the
// quick-add parser first and the explicit fields win over what it found.
func (s *Server) newTask(ctx context.Context, uid int64, raw map[string]json.RawMessage) (*store.Task, error) {
	st, _ := s.store.GetSettings(ctx, uid)
	t := &store.Task{UserID: uid, Status: ical.StatusOpen, Tags: []string{}, Alarms: []ical.Alarm{}}
	t.ListID = s.defaultList(ctx, uid, st)
	if v, ok := raw["text"]; ok {
		var text string
		_ = json.Unmarshal(v, &text)
		p := quick.Parse(text, time.Now().In(s.loc(ctx, uid)))
		s.applyParsed(ctx, t, p)
		delete(raw, "text")
	}
	if err := s.patch(ctx, t, raw); err != nil {
		return nil, err
	}
	if strings.TrimSpace(t.Title) == "" {
		return nil, badRequest("a task needs a title")
	}
	if len(t.Alarms) == 0 && st.DefaultAlarm != "" && !hasKey(raw, "alarms") && ((t.Due != nil && !ical.IsDay(*t.Due)) || (t.Start != nil && !ical.IsDay(*t.Start))) {
		t.Alarms = []ical.Alarm{{Trigger: st.DefaultAlarm}}
	}
	if err := s.saveTask(ctx, t, "api", "created"); err != nil {
		return nil, err
	}
	return t, nil
}

func hasKey(m map[string]json.RawMessage, k string) bool { _, ok := m[k]; return ok }

func (s *Server) defaultList(ctx context.Context, uid int64, st store.Settings) int64 {
	if st.DefaultListID > 0 {
		if l, err := s.store.GetList(ctx, uid, st.DefaultListID); err == nil && !l.Archived {
			return l.ID
		}
	}
	inbox, _ := s.store.Inbox(ctx, uid)
	return inbox.ID
}

// applyParsed puts what quick-add understood onto a task.
func (s *Server) applyParsed(ctx context.Context, t *store.Task, p quick.Parsed) {
	t.Title = p.Title
	if p.Notes != "" {
		t.Notes = p.Notes
	}
	t.Start, t.Due = optStr(p.Start), optStr(p.Due)
	if p.Priority > 0 {
		t.Priority = p.Priority
	}
	t.Someday = p.Someday
	t.Tags = cleanTags(p.Tags)
	if p.EstimateMin > 0 {
		e := p.EstimateMin
		t.EstimateMin = &e
	}
	if p.RRule != "" {
		t.Repeat = &store.Repeat{RRule: p.RRule, From: p.RepeatFrom}
		if t.Start == nil && t.Due == nil {
			today := time.Now().In(s.loc(ctx, t.UserID)).Format(ical.DayLayout)
			t.Start = &today
		}
	}
	if p.Alarm != "" {
		t.Alarms = []ical.Alarm{{Trigger: p.Alarm}}
	}
	if p.List != "" {
		if l := s.listByName(ctx, t.UserID, p.List); l != nil {
			t.ListID = l.ID
		}
	}
	if p.Section != "" {
		if sec, err := s.store.SectionByName(ctx, t.UserID, t.ListID, p.Section); err == nil {
			t.SectionID = &sec.ID
		}
	}
}

func (s *Server) listByName(ctx context.Context, uid int64, name string) *store.List {
	lists, _ := s.store.ListLists(ctx, uid)
	name = strings.ToLower(strings.TrimSpace(name))
	for _, l := range lists {
		if strings.ToLower(l.Name) == name {
			return &l
		}
	}
	for _, l := range lists {
		if strings.HasPrefix(strings.ToLower(l.Name), name) {
			return &l
		}
	}
	return nil
}

func (s *Server) handleQuick(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	var in struct {
		Text   string `json:"text"`
		ListID int64  `json:"list_id"`
		Commit bool   `json:"commit"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	p := quick.Parse(in.Text, time.Now().In(s.loc(ctx, uid)))
	t := &store.Task{UserID: uid, Status: ical.StatusOpen, Tags: []string{}, Alarms: []ical.Alarm{}}
	t.ListID = s.defaultList(ctx, uid, st)
	if in.ListID > 0 {
		if _, err := s.store.GetList(ctx, uid, in.ListID); err == nil {
			t.ListID = in.ListID
		}
	}
	s.applyParsed(ctx, t, p)
	if len(t.Alarms) == 0 && st.DefaultAlarm != "" && ((t.Due != nil && !ical.IsDay(*t.Due)) || (t.Start != nil && !ical.IsDay(*t.Start))) {
		t.Alarms = []ical.Alarm{{Trigger: st.DefaultAlarm}}
	}
	parsed := map[string]any{"title": p.Title, "start": p.Start, "due": p.Due, "list_id": t.ListID, "tags": t.Tags, "priority": t.Priority, "repeat": t.Repeat, "alarm": p.Alarm, "someday": p.Someday, "estimate_min": p.EstimateMin, "section": p.Section, "notes": p.Notes}
	if p.List != "" {
		parsed["list"] = p.List
	}
	if t.Repeat != nil {
		if rule, ok := ical.ParseRule(t.Repeat.RRule); ok {
			parsed["repeat_text"] = rule.Describe()
		}
	}
	if !in.Commit || strings.TrimSpace(p.Title) == "" {
		if l, err := s.store.GetList(ctx, uid, t.ListID); err == nil {
			t.List = &store.ListRef{ID: l.ID, Name: l.Name, Color: l.Color}
		}
		writeJSON(w, http.StatusOK, map[string]any{"task": t, "parsed": parsed})
		return
	}
	if err := s.saveTask(ctx, t, "quick", "created"); err != nil {
		s.fail(w, err, "create task")
		return
	}
	t, _ = s.store.GetTask(ctx, uid, t.ID)
	writeJSON(w, http.StatusCreated, map[string]any{"task": t, "parsed": parsed})
}

func (s *Server) handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	t, err := s.store.GetTask(ctx, uid, idParam(r, "id"))
	if err != nil || t.DeletedAt != "" {
		s.fail(w, store.ErrNotFound, "get task")
		return
	}
	var raw map[string]json.RawMessage
	if err := decode(r, &raw); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	wasDone := t.Status == ical.StatusDone
	if err := s.patch(ctx, t, raw); err != nil {
		s.fail(w, err, "update task")
		return
	}
	kind := "updated"
	if !wasDone && t.Status == ical.StatusDone {
		kind = "completed"
	} else if wasDone && t.Status != ical.StatusDone {
		kind = "reopened"
	}
	if err := s.saveTask(ctx, t, "api", kind); err != nil {
		s.fail(w, err, "update task")
		return
	}
	var next *store.Task
	if kind == "completed" && t.Repeat != nil {
		next, _ = s.spawnNext(ctx, t)
	}
	t, _ = s.store.GetTask(ctx, uid, t.ID)
	today, now, _ := s.today(ctx, uid)
	s.decorate(t, today, now)
	if next != nil {
		writeJSON(w, http.StatusOK, map[string]any{"task": t, "next": next, "id": t.ID})
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	id := idParam(r, "id")
	t, err := s.store.GetTask(ctx, uid, id)
	if err != nil {
		s.fail(w, err, "get task")
		return
	}
	if err := s.store.SoftDeleteTask(ctx, uid, id, s.taskEvent(ctx, t, "deleted", "api", nil)); err != nil {
		s.fail(w, err, "delete task")
		return
	}
	s.touch()
	go s.syncSoon(uid)
	s.store.AddActivity(ctx, uid, id, "deleted", "api")
	w.WriteHeader(http.StatusNoContent)
}

// complete marks a task done (or open again) and spawns the next occurrence
// of a repeating one.
func (s *Server) complete(ctx context.Context, t *store.Task, done bool, source string) (*store.Task, error) {
	if done == (t.Status == ical.StatusDone) {
		return nil, nil
	}
	if done {
		now := store.Now()
		t.Status, t.CompletedAt, t.Percent = ical.StatusDone, &now, 100
	} else {
		t.Status, t.CompletedAt, t.Percent = ical.StatusOpen, nil, 0
	}
	kind := "completed"
	if !done {
		kind = "reopened"
	}
	if err := s.saveTask(ctx, t, source, kind); err != nil {
		return nil, err
	}
	if done && t.Repeat != nil {
		return s.spawnNext(ctx, t)
	}
	return nil, nil
}

// spawnNext makes the next occurrence of a repeating task that was just
// completed: a fresh task with the dates moved on, while the finished one
// stays in the logbook without the rule.
func (s *Server) spawnNext(ctx context.Context, done *store.Task) (*store.Task, error) {
	rule, ok := ical.ParseRule(done.Repeat.RRule)
	if !ok {
		return nil, nil
	}
	loc := s.loc(ctx, done.UserID)
	anchorStr := done.DueStr()
	if anchorStr == "" {
		anchorStr = done.StartStr()
	}
	if anchorStr == "" {
		anchorStr = time.Now().In(loc).Format(ical.DayLayout)
	}
	anchor, _ := ical.ParseLocal(anchorStr, loc)
	var next time.Time
	if done.Repeat.From == "completion" {
		next, ok = rule.NextAfterCompletion(anchor, time.Now().In(loc))
	} else {
		next, ok = rule.Next(anchor, anchor)
		// a long-overdue repeat jumps to the first occurrence after today
		if ok && ical.IsDay(anchorStr) {
			today := time.Now().In(loc)
			for ok && next.Before(time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc)) {
				next, ok = rule.Next(anchor, next)
			}
		}
	}
	rp := *done.Repeat
	if rule.Count > 0 {
		rule.Count--
		rp.RRule = rule.String()
	}
	// the finished copy keeps no rule, so clients do not show it as a series
	done.Repeat = nil
	_ = s.saveTask(ctx, done, "repeat", "updated")
	if !ok {
		return nil, nil
	}
	delta := next.Sub(anchor)
	shift := func(v string) *string {
		if v == "" {
			return nil
		}
		t, _ := ical.ParseLocal(v, loc)
		return optStr(ical.FormatLocal(t.Add(delta), ical.IsDay(v)))
	}
	n := &store.Task{UserID: done.UserID, ListID: done.ListID, SectionID: done.SectionID, ParentID: done.ParentID, Title: done.Title, Notes: done.Notes, Status: ical.StatusOpen,
		Priority: done.Priority, Start: shift(done.StartStr()), Due: shift(done.DueStr()), EstimateMin: done.EstimateMin, Repeat: &rp, Someday: done.Someday,
		Tags: append([]string{}, done.Tags...), Alarms: append([]ical.Alarm{}, done.Alarms...), Location: done.Location, URL: done.URL, SortOrder: done.SortOrder}
	// absolute alarms move with the task
	for i, a := range n.Alarms {
		if at, err := time.Parse(time.RFC3339, a.Trigger); err == nil {
			n.Alarms[i].Trigger = at.Add(delta).UTC().Format(time.RFC3339)
		}
	}
	if err := s.saveTask(ctx, n, "repeat", "created"); err != nil {
		return nil, err
	}
	// subtasks come along, open again
	subs, _ := s.store.Subtasks(ctx, done.UserID, done.ID)
	for _, c := range subs {
		cc := &store.Task{UserID: c.UserID, ListID: n.ListID, ParentID: &n.ID, Title: c.Title, Notes: c.Notes, Status: ical.StatusOpen, Priority: c.Priority, Tags: append([]string{}, c.Tags...), Alarms: []ical.Alarm{}, SortOrder: c.SortOrder}
		_ = s.saveTask(ctx, cc, "repeat", "created")
	}
	return n, nil
}

func (s *Server) handleCompleteTask(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	t, err := s.store.GetTask(ctx, uid, idParam(r, "id"))
	if err != nil || t.DeletedAt != "" {
		s.fail(w, store.ErrNotFound, "get task")
		return
	}
	var in struct {
		Done *bool `json:"done"`
	}
	_ = decode(r, &in)
	done := true
	if in.Done != nil {
		done = *in.Done
	}
	next, err := s.complete(ctx, t, done, "api")
	if err != nil {
		s.fail(w, err, "complete task")
		return
	}
	t, _ = s.store.GetTask(ctx, uid, t.ID)
	today, now, _ := s.today(ctx, uid)
	s.decorate(t, today, now)
	out := map[string]any{"task": t}
	if next != nil {
		out["next"] = next
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleMoveTask(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	t, err := s.store.GetTask(ctx, uid, idParam(r, "id"))
	if err != nil || t.DeletedAt != "" {
		s.fail(w, store.ErrNotFound, "get task")
		return
	}
	var raw map[string]json.RawMessage
	if err := decode(r, &raw); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	sub := map[string]json.RawMessage{}
	for _, k := range []string{"list_id", "section_id", "parent_id"} {
		if v, ok := raw[k]; ok {
			sub[k] = v
		}
	}
	if err := s.patch(ctx, t, sub); err != nil {
		s.fail(w, err, "move task")
		return
	}
	var before, after int64
	if v, ok := raw["before_id"]; ok {
		_ = json.Unmarshal(v, &before)
	}
	if v, ok := raw["after_id"]; ok {
		_ = json.Unmarshal(v, &after)
	}
	if before > 0 || after > 0 {
		s.placeBetween(ctx, t, before, after)
	}
	if err := s.saveTask(ctx, t, "api", "moved"); err != nil {
		s.fail(w, err, "move task")
		return
	}
	t, _ = s.store.GetTask(ctx, uid, t.ID)
	writeJSON(w, http.StatusOK, t)
}

// placeBetween gives t a sort_order between its neighbours; renumbers the
// neighbourhood when the gap closed.
func (s *Server) placeBetween(ctx context.Context, t *store.Task, beforeID, afterID int64) {
	var lo, hi float64
	var haveLo, haveHi bool
	if afterID > 0 {
		if a, err := s.store.GetTask(ctx, t.UserID, afterID); err == nil {
			lo, haveLo = a.SortOrder, true
		}
	}
	if beforeID > 0 {
		if b, err := s.store.GetTask(ctx, t.UserID, beforeID); err == nil {
			hi, haveHi = b.SortOrder, true
		}
	}
	switch {
	case haveLo && haveHi:
		t.SortOrder = (lo + hi) / 2
		if hi-lo < 1e-6 {
			s.renumber(ctx, t, beforeID)
		}
	case haveLo:
		t.SortOrder = lo + 1
	case haveHi:
		t.SortOrder = hi - 1
	}
}

// renumber spaces out the siblings of t (same list, section and parent) and
// puts t right before beforeID.
func (s *Server) renumber(ctx context.Context, t *store.Task, beforeID int64) {
	q := store.Query{ListID: t.ListID, Status: "all", Limit: 5000}
	sid := int64(0)
	if t.SectionID != nil {
		sid = *t.SectionID
	}
	q.SectionID = &sid
	pid := int64(0)
	if t.ParentID != nil {
		pid = *t.ParentID
	}
	q.ParentID = &pid
	sibs, _, err := s.store.ListTasks(ctx, t.UserID, q, 0)
	if err != nil {
		return
	}
	var ids []int64
	for _, x := range sibs {
		if x.ID == t.ID {
			continue
		}
		if x.ID == beforeID {
			ids = append(ids, t.ID)
		}
		ids = append(ids, x.ID)
	}
	_ = s.store.Reorder(ctx, "tasks", t.UserID, ids)
	for i, id := range ids {
		if id == t.ID {
			t.SortOrder = float64(i + 1)
		}
	}
}

func (s *Server) handleDuplicateTask(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	t, err := s.store.GetTask(ctx, uid, idParam(r, "id"))
	if err != nil || t.DeletedAt != "" {
		s.fail(w, store.ErrNotFound, "get task")
		return
	}
	n := &store.Task{UserID: uid, ListID: t.ListID, SectionID: t.SectionID, ParentID: t.ParentID, Title: t.Title + " (copy)", Notes: t.Notes, Status: ical.StatusOpen, Priority: t.Priority,
		Start: t.Start, Due: t.Due, EstimateMin: t.EstimateMin, Repeat: t.Repeat, Someday: t.Someday, Tags: append([]string{}, t.Tags...), Alarms: append([]ical.Alarm{}, t.Alarms...), Location: t.Location, URL: t.URL, SortOrder: t.SortOrder + 0.5}
	if err := s.saveTask(ctx, n, "api", "created"); err != nil {
		s.fail(w, err, "duplicate task")
		return
	}
	subs, _ := s.store.Subtasks(ctx, uid, t.ID)
	for _, c := range subs {
		cc := &store.Task{UserID: uid, ListID: n.ListID, ParentID: &n.ID, Title: c.Title, Notes: c.Notes, Status: ical.StatusOpen, Priority: c.Priority, Tags: append([]string{}, c.Tags...), Alarms: []ical.Alarm{}, SortOrder: c.SortOrder}
		_ = s.saveTask(ctx, cc, "api", "created")
	}
	n, _ = s.store.GetTask(ctx, uid, n.ID)
	writeJSON(w, http.StatusCreated, n)
}

func (s *Server) handleTaskICS(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	t, err := s.store.GetTask(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get task")
		return
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", t.Href))
	_, _ = w.Write([]byte(t.ICS))
}

func (s *Server) handleBatch(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	var in struct {
		IDs    []int64                    `json:"ids"`
		Set    map[string]json.RawMessage `json:"set"`
		Action string                     `json:"action"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	var addTags, removeTags []string
	if v, ok := in.Set["add_tags"]; ok {
		_ = json.Unmarshal(v, &addTags)
		delete(in.Set, "add_tags")
	}
	if v, ok := in.Set["remove_tags"]; ok {
		_ = json.Unmarshal(v, &removeTags)
		delete(in.Set, "remove_tags")
	}
	n := 0
	for _, id := range in.IDs {
		t, err := s.store.GetTask(ctx, uid, id)
		if err != nil || t.DeletedAt != "" {
			continue
		}
		switch in.Action {
		case "delete":
			if s.store.SoftDeleteTask(ctx, uid, id, s.taskEvent(ctx, t, "deleted", "api", nil)) == nil {
				s.store.AddActivity(ctx, uid, id, "deleted", "batch")
				n++
			}
			continue
		case "complete":
			if _, err := s.complete(ctx, t, true, "batch"); err == nil {
				n++
			}
			continue
		case "reopen":
			if _, err := s.complete(ctx, t, false, "batch"); err == nil {
				n++
			}
			continue
		}
		if len(in.Set) == 0 && len(addTags) == 0 && len(removeTags) == 0 {
			continue
		}
		if err := s.patch(ctx, t, in.Set); err != nil {
			s.fail(w, err, "batch")
			return
		}
		if len(addTags) > 0 || len(removeTags) > 0 {
			tags := append([]string{}, t.Tags...)
			tags = append(tags, addTags...)
			var kept []string
			for _, tag := range cleanTags(tags) {
				drop := false
				for _, rm := range removeTags {
					if strings.EqualFold(rm, tag) {
						drop = true
					}
				}
				if !drop {
					kept = append(kept, tag)
				}
			}
			t.Tags = kept
		}
		if err := s.saveTask(ctx, t, "batch", "updated"); err == nil {
			n++
		}
	}
	s.touch()
	go s.syncSoon(uid)
	writeJSON(w, http.StatusOK, map[string]int{"updated": n})
}

// ---------- search, calendar, trash, activity ----------

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	ts, err := s.store.Search(ctx, uid, r.URL.Query().Get("q"), queryInt(r, "limit"))
	if err != nil {
		s.fail(w, err, "search")
		return
	}
	today, now, _ := s.today(ctx, uid)
	for _, t := range ts {
		s.decorate(t, today, now)
	}
	writeJSON(w, http.StatusOK, ts)
}

type calendarDay struct {
	Start []*store.Task `json:"start"`
	Due   []*store.Task `json:"due"`
	Done  []*store.Task `json:"done"`
}

func (s *Server) handleCalendar(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	loc := s.loc(ctx, uid)
	f, err1 := time.ParseInLocation(ical.DayLayout, from, loc)
	t, err2 := time.ParseInLocation(ical.DayLayout, to, loc)
	if err1 != nil || err2 != nil || t.Before(f) || t.Sub(f) > 100*24*time.Hour {
		writeErr(w, http.StatusBadRequest, "from and to must be days at most 100 days apart")
		return
	}
	today, now, _ := s.today(ctx, uid)
	days := map[string]*calendarDay{}
	day := func(d string) *calendarDay {
		if days[d] == nil {
			days[d] = &calendarDay{Start: []*store.Task{}, Due: []*store.Task{}, Done: []*store.Task{}}
		}
		return days[d]
	}
	starts, _, err := s.store.ListTasks(ctx, uid, store.Query{Status: "open", StartFrom: from, StartTo: to, Limit: 2000}, 0)
	if err != nil {
		s.fail(w, err, "calendar")
		return
	}
	for _, x := range starts {
		s.decorate(x, today, now)
		day(ical.Day(x.StartStr())).Start = append(day(ical.Day(x.StartStr())).Start, x)
	}
	dues, _, _ := s.store.ListTasks(ctx, uid, store.Query{Status: "open", DueFrom: from, DueTo: to, Limit: 2000}, 0)
	for _, x := range dues {
		s.decorate(x, today, now)
		day(ical.Day(x.DueStr())).Due = append(day(ical.Day(x.DueStr())).Due, x)
	}
	dones, _, _ := s.store.ListTasks(ctx, uid, store.Query{Status: "done", CompletedOn: store.FormatTime(f) + "|" + store.FormatTime(t.AddDate(0, 0, 1)), Limit: 2000}, 0)
	for _, x := range dones {
		if x.CompletedAt == nil {
			continue
		}
		d := store.ParseTime(*x.CompletedAt).In(loc).Format(ical.DayLayout)
		day(d).Done = append(day(d).Done, x)
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": days})
}

func (s *Server) handleTrash(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	ts, _, err := s.store.ListTasks(ctx, uid, store.Query{Deleted: true, Status: "all", Limit: 1000}, 0)
	if err != nil {
		s.fail(w, err, "trash")
		return
	}
	sort.SliceStable(ts, func(i, j int) bool { return ts[i].DeletedAt > ts[j].DeletedAt })
	writeJSON(w, http.StatusOK, ts)
}

func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	id := idParam(r, "id")
	if err := s.store.RestoreTask(ctx, uid, id); err != nil {
		s.fail(w, err, "restore")
		return
	}
	s.store.AddActivity(ctx, uid, id, "restored", "api")
	s.touch()
	t, _ := s.store.GetTask(ctx, uid, id)
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handlePurge(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	t, err := s.store.GetTask(ctx, uid, idParam(r, "id"))
	if err != nil || t.DeletedAt == "" {
		s.fail(w, store.ErrNotFound, "purge")
		return
	}
	s.flushDeletes(ctx, uid)
	paths, err := s.store.PurgeTask(ctx, uid, t.ID)
	if err != nil {
		s.fail(w, err, "purge")
		return
	}
	s.removeFiles(paths)
	s.touch()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleEmptyTrash(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	s.flushDeletes(ctx, uid)
	paths, err := s.store.EmptyTrash(ctx, uid)
	if err != nil {
		s.fail(w, err, "empty trash")
		return
	}
	s.removeFiles(paths)
	s.touch()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	act, err := s.store.ListActivity(ctx, uid, queryInt64(r, "task_id"), queryInt(r, "limit"))
	if err != nil {
		s.fail(w, err, "activity")
		return
	}
	writeJSON(w, http.StatusOK, act)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	loc := s.loc(ctx, uid)
	now := time.Now().In(loc)
	today := now.Format(ical.DayLayout)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	st, _ := s.store.GetSettings(ctx, uid)
	wd := int(dayStart.Weekday())
	if st.WeekStart == 1 {
		wd = (wd + 6) % 7
	}
	weekStart := dayStart.AddDate(0, 0, -wd)
	d, err := s.store.Dashboard(ctx, uid, today, dayStart.AddDate(0, 0, 7).Format(ical.DayLayout), store.FormatTime(dayStart), store.FormatTime(weekStart))
	if err != nil {
		s.fail(w, err, "dashboard")
		return
	}
	for _, t := range d.Next {
		s.decorate(t, today, now.Format(ical.TimeLayout))
	}
	writeJSON(w, http.StatusOK, d)
}

var errNoSuchList = errors.New("no such list")
