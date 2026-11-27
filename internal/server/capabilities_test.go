package server

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
)

// getInto calls a route and reads its JSON answer into out (a pointer,
// emptied first so nothing of an earlier answer stays).
func getInto(t *testing.T, c *client, method, path string, body, out any) int {
	t.Helper()
	w, _ := c.do(method, path, body)
	if out != nil {
		reflect.ValueOf(out).Elem().SetZero()
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
	}
	return w.Code
}

// mustCreate adds a task and returns its id.
func mustCreate(t *testing.T, c *client, body map[string]any) int {
	t.Helper()
	w, task := c.do("POST", "/api/v1/tasks", body)
	if w.Code != 201 {
		t.Fatalf("create %v: %d %s", body["title"], w.Code, w.Body.String())
	}
	return int(task["id"].(float64))
}

func listID(t *testing.T, c *client, name string) int {
	t.Helper()
	w, l := c.do("POST", "/api/v1/lists", map[string]any{"name": name})
	if w.Code != 201 {
		t.Fatalf("create list %s: %d %s", name, w.Code, w.Body.String())
	}
	return int(l["id"].(float64))
}

func inboxID(t *testing.T, c *client) int {
	t.Helper()
	w, _ := c.do("GET", "/api/v1/lists", nil)
	var lists []map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &lists)
	return int(lists[0]["id"].(float64))
}

func TestPocketSearch(t *testing.T) {
	s, h := testServer(t)
	alice := newPerson(t, s, h, "alice@example.com")
	bob := newPerson(t, s, h, "bob@example.com")
	kitchen := listID(t, alice, "Kitchen")
	done := mustCreate(t, alice, map[string]any{"title": "Bread knife", "status": "done"})
	noted := mustCreate(t, alice, map[string]any{"title": "Buy bread", "notes": "- [ ] From the **bakery** on [Main Street](https://maps.example/main)\n\n## Later\n`rye` too"})
	dated := mustCreate(t, alice, map[string]any{"title": "Bake bread", "list_id": kitchen, "due": "2026-12-24T09:00:00"})
	mustCreate(t, alice, map[string]any{"title": "Water the plants"})
	mustCreate(t, bob, map[string]any{"title": "Bob's bread"})

	var got core.SearchResults
	if code := getInto(t, alice, "GET", "/api/v1/pocket/search?q=bread", nil, &got); code != 200 || len(got.Items) != 3 {
		t.Fatalf("search: %d %+v", code, got)
	}
	by := map[string]core.SearchItem{}
	for _, it := range got.Items {
		by[it.ID] = it
		if it.Kind != "task" {
			t.Errorf("%q: kind %q", it.Title, it.Kind)
		}
	}
	// open tasks come before finished ones
	if got.Items[2].ID != itoa(done) {
		t.Errorf("finished task not last: %+v", got.Items)
	}
	n := by[itoa(noted)]
	if n.Title != "Buy bread" || n.Snippet != "From the bakery on Main Street Later rye too" || n.URL != "/list/"+itoa(inboxID(t, alice))+"?task="+itoa(noted) {
		t.Errorf("task with notes: %+v", n)
	}
	if _, err := time.Parse(time.RFC3339, n.Date); err != nil {
		t.Errorf("undated task: date %q is not when it last changed", n.Date)
	}
	d := by[itoa(dated)]
	if d.Snippet != "Kitchen" || d.Date != "2026-12-24" || d.URL != "/list/"+itoa(kitchen)+"?task="+itoa(dated) {
		t.Errorf("task with a due day and no notes: %+v", d)
	}

	if getInto(t, alice, "GET", "/api/v1/pocket/search?q=bread&limit=1", nil, &got); len(got.Items) != 1 {
		t.Errorf("limit=1: %d items", len(got.Items))
	}
	if code := getInto(t, alice, "GET", "/api/v1/pocket/search?q=", nil, &got); code != 200 || got.Items == nil || len(got.Items) != 0 {
		t.Errorf("no words: %d %+v", code, got)
	}
	// strictly per person
	if getInto(t, bob, "GET", "/api/v1/pocket/search?q=bread", nil, &got); len(got.Items) != 1 || got.Items[0].Title != "Bob's bread" {
		t.Errorf("bob finds: %+v", got.Items)
	}
	if w, _ := (&client{t: t, h: h}).do("GET", "/api/v1/pocket/search?q=bread", nil); w.Code != 401 {
		t.Errorf("signed out: %d", w.Code)
	}
}

// setStamp moves a task's created_at (nothing in the API can).
func setStamp(t *testing.T, s *Server, id int, created string) {
	t.Helper()
	if _, err := s.store.DB.Exec(`UPDATE tasks SET created_at = ? WHERE id = ?`, created, id); err != nil {
		t.Fatal(err)
	}
}

func finishAt(t *testing.T, c *client, id int, at string) {
	t.Helper()
	if w, _ := c.do("PUT", "/api/v1/tasks/"+itoa(id), map[string]any{"status": "done", "completed_at": at}); w.Code != 200 {
		t.Fatalf("finish %d: %d %s", id, w.Code, w.Body.String())
	}
}

func dayItems(d core.Day) map[string]core.DayItem {
	out := map[string]core.DayItem{}
	for _, it := range d.Items {
		out[it.Title] = it
	}
	return out
}

func TestPocketDay(t *testing.T) {
	s, h := testServer(t)
	alice := newPerson(t, s, h, "alice@example.com")
	bob := newPerson(t, s, h, "bob@example.com")
	chores := listID(t, alice, "Chores")

	// Honolulu is ten hours behind UTC: 23:30 there on the 20th is 09:30 UTC
	// on the 21st (and 10:30 on the 21st in the box's zone, Europe/Sarajevo).
	late := mustCreate(t, alice, map[string]any{"title": "Late night", "list_id": chores})
	setStamp(t, s, late, "2026-11-01T08:00:00.000Z")
	finishAt(t, alice, late, "2026-11-21T09:30:00Z")
	due := mustCreate(t, alice, map[string]any{"title": "Still open", "due": "2026-11-20"})
	setStamp(t, s, due, "2026-11-01T08:00:00.000Z")
	added := mustCreate(t, alice, map[string]any{"title": "Added then"})
	setStamp(t, s, added, "2026-11-20T20:00:00.000Z") // 10:00 in Honolulu
	both := mustCreate(t, alice, map[string]any{"title": "Added and done"})
	setStamp(t, s, both, "2026-11-20T21:00:00.000Z")
	finishAt(t, alice, both, "2026-11-20T22:00:00Z") // 12:00 in Honolulu
	gone := mustCreate(t, alice, map[string]any{"title": "Thrown away", "due": "2026-11-20"})
	if w, _ := alice.do("DELETE", "/api/v1/tasks/"+itoa(gone), nil); w.Code >= 300 {
		t.Fatalf("delete: %d", w.Code)
	}
	theirs := mustCreate(t, bob, map[string]any{"title": "Bob's", "due": "2026-11-20"})
	finishAt(t, bob, theirs, "2026-11-21T09:30:00Z")

	var day core.Day
	if code := getInto(t, alice, "GET", "/api/v1/pocket/day/2026-11-20?tz=Pacific/Honolulu", nil, &day); code != 200 {
		t.Fatalf("day: %d", code)
	}
	if day.Date != "2026-11-20" || day.Summary != "2 done, 1 still due, 2 added" || len(day.Items) != 4 {
		t.Fatalf("the 20th in Honolulu: %+v", day)
	}
	got := dayItems(day)
	if it := got["Late night"]; !it.Done || it.Kind != "done" || it.Time != "23:30" || it.Sub != "Chores" || it.URL != "/list/"+itoa(chores)+"?task="+itoa(late) {
		t.Errorf("finished at 23:30 local: %+v", it)
	}
	if it := got["Added and done"]; !it.Done || it.Kind != "done" || it.Time != "12:00" {
		t.Errorf("added and finished the same day shows once, as done: %+v", it)
	}
	if it := got["Still open"]; it.Done || it.Kind != "due" || it.Time != "" || it.Sub != "Inbox" {
		t.Errorf("due that day: %+v", it)
	}
	if it := got["Added then"]; it.Done || it.Kind != "added" || it.Time != "10:00" {
		t.Errorf("added that day: %+v", it)
	}
	want := []core.Metric{{Label: "Done", Value: "2"}, {Label: "Due", Value: "1"}, {Label: "Added", Value: "2"}}
	if len(day.Metrics) != len(want) {
		t.Fatalf("metrics: %+v", day.Metrics)
	}
	for i, m := range want {
		if day.Metrics[i] != m {
			t.Errorf("metrics: %+v, want %+v", day.Metrics, want)
			break
		}
	}

	// the next day there: 23:30 the evening before is not part of it
	if getInto(t, alice, "GET", "/api/v1/pocket/day/2026-11-21?tz=Pacific/Honolulu", nil, &day); day.Summary != "" || len(day.Items) != 0 {
		t.Errorf("the 21st in Honolulu: %+v", day)
	}
	// without tz the person's own zone decides: 10:30 on the 21st
	getInto(t, alice, "GET", "/api/v1/pocket/day/2026-11-21", nil, &day)
	if it := dayItems(day)["Late night"]; day.Summary != "1 done" || len(day.Items) != 1 || it.Time != "10:30" {
		t.Errorf("the 21st in the app's zone: %+v", day)
	}
	// far east of UTC the same moment is already the 22nd (Kiritimati, +14: 23:30 on the 21st)
	getInto(t, alice, "GET", "/api/v1/pocket/day/2026-11-21?tz=Pacific/Kiritimati", nil, &day)
	if it := dayItems(day)["Late night"]; it.Time != "23:30" {
		t.Errorf("the 21st in Kiritimati: %+v", day)
	}

	// a day still to come has nothing "still" due
	mustCreate(t, alice, map[string]any{"title": "One day", "due": "2999-01-02T08:15:00"})
	getInto(t, alice, "GET", "/api/v1/pocket/day/2999-01-02", nil, &day)
	if it := dayItems(day)["One day"]; day.Summary != "1 due" || it.Kind != "due" || it.Time != "08:15" {
		t.Errorf("a day to come: %+v", day)
	}

	// strictly per person
	getInto(t, bob, "GET", "/api/v1/pocket/day/2026-11-20?tz=Pacific/Honolulu", nil, &day)
	if len(day.Items) != 1 || day.Items[0].Title != "Bob's" || day.Summary != "1 done" {
		t.Errorf("bob's 20th: %+v", day)
	}
	if code := getInto(t, alice, "GET", "/api/v1/pocket/day/1999-01-01", nil, &day); code != 200 || day.Summary != "" || day.Items == nil || len(day.Items) != 0 {
		t.Errorf("a day with nothing: %d %+v", code, day)
	}
	if w, _ := alice.do("GET", "/api/v1/pocket/day/yesterday", nil); w.Code != 400 {
		t.Errorf("not a day: %d", w.Code)
	}
}

func TestPocketDayRepeatIsNotAdded(t *testing.T) {
	s, h := testServer(t)
	alice := newPerson(t, s, h, "alice@example.com")
	u, _ := s.store.GetUserByEmail(context.Background(), "alice@example.com")
	today, _, _ := s.today(context.Background(), u.ID)
	id := mustCreate(t, alice, map[string]any{"title": "Water the plants", "due": today, "repeat": map[string]any{"rrule": "FREQ=DAILY"}})
	setStamp(t, s, id, "2026-01-01T08:00:00.000Z")
	if w, out := alice.do("POST", "/api/v1/tasks/"+itoa(id)+"/complete", map[string]any{"done": true}); w.Code != 200 || out["next"] == nil {
		t.Fatalf("complete: %d %s", w.Code, w.Body.String())
	}
	var day core.Day
	getInto(t, alice, "GET", "/api/v1/pocket/day/"+today, nil, &day)
	if day.Summary != "1 done" || len(day.Items) != 1 || day.Items[0].Kind != "done" {
		t.Errorf("the next round of a repeating task counted as added: %+v", day)
	}
}

func TestPocketWidget(t *testing.T) {
	s, h := testServer(t)
	alice := newPerson(t, s, h, "alice@example.com")
	bob := newPerson(t, s, h, "bob@example.com")
	u, _ := s.store.GetUserByEmail(context.Background(), "alice@example.com")
	today, _, loc := s.today(context.Background(), u.ID)
	now, _ := time.ParseInLocation("2006-01-02", today, loc)
	day := func(n int) string { return now.AddDate(0, 0, n).Format("2006-01-02") }

	var got core.WidgetData
	if code := getInto(t, alice, "GET", "/api/v1/pocket/widget", nil, &got); code != 200 || got.Line != "Nothing due today" || got.URL != "/today" || got.Items == nil || len(got.Items) != 0 {
		t.Fatalf("nothing yet: %d %+v", code, got)
	}

	soon := mustCreate(t, alice, map[string]any{"title": "In three days", "due": day(3)})
	mustCreate(t, alice, map[string]any{"title": "Tomorrow", "start": day(1)})
	mustCreate(t, alice, map[string]any{"title": "Due today", "due": today})
	mustCreate(t, alice, map[string]any{"title": "Starts today"})
	overdue := mustCreate(t, alice, map[string]any{"title": "Overdue", "due": day(-1)})
	mustCreate(t, alice, map[string]any{"title": "Someday", "someday": true})
	finished := mustCreate(t, alice, map[string]any{"title": "Finished"})
	if w, _ := alice.do("POST", "/api/v1/tasks/"+itoa(finished)+"/complete", map[string]any{"done": true}); w.Code != 200 {
		t.Fatalf("complete: %d", w.Code)
	}
	mustCreate(t, bob, map[string]any{"title": "Bob's", "due": day(-2)})

	getInto(t, alice, "GET", "/api/v1/pocket/widget", nil, &got)
	if got.Line != "2 due today, 1 late" || got.URL != "/today" {
		t.Errorf("line: %+v", got)
	}
	want := []core.Metric{{Label: "Today", Value: "2"}, {Label: "Late", Value: "1"}, {Label: "Done today", Value: "1"}}
	for i, m := range want {
		if len(got.Metrics) != len(want) || got.Metrics[i] != m {
			t.Errorf("metrics: %+v, want %+v", got.Metrics, want)
			break
		}
	}
	// late first, then today, then what comes next
	rows := []string{}
	for _, it := range got.Items {
		rows = append(rows, it.Title+" ("+it.Detail+")")
	}
	wantRows := "Overdue (late) | Due today (today) | Starts today (today) | Tomorrow (tomorrow) | In three days (" + dayWord(day(3), today, loc) + ")"
	if strings.Join(rows, " | ") != wantRows {
		t.Errorf("rows: %s\n want: %s", strings.Join(rows, " | "), wantRows)
	}
	inbox := itoa(inboxID(t, alice))
	if got.Items[0].ID != itoa(overdue) || got.Items[0].URL != "/list/"+inbox+"?task="+itoa(overdue) || got.Items[4].URL != "/list/"+inbox+"?task="+itoa(soon) {
		t.Errorf("row addresses: %+v", got.Items)
	}
	// the numbers are the dashboard's
	_, dash := alice.do("GET", "/api/v1/dashboard", nil)
	if dash["today"] != float64(3) || dash["overdue"] != float64(1) || dash["done_today"] != float64(1) {
		t.Errorf("dashboard: %v", dash)
	}

	// never more than eight rows, however long today is
	for i := 0; i < 10; i++ {
		mustCreate(t, alice, map[string]any{"title": "More " + itoa(i)})
	}
	getInto(t, alice, "GET", "/api/v1/pocket/widget", nil, &got)
	if len(got.Items) != 8 || got.Items[0].Title != "Overdue" || got.Line != "12 due today, 1 late" {
		t.Errorf("a long day: %q, %d rows", got.Line, len(got.Items))
	}

	// strictly per person
	getInto(t, bob, "GET", "/api/v1/pocket/widget", nil, &got)
	if got.Line != "1 late" || len(got.Items) != 1 || got.Items[0].Title != "Bob's" {
		t.Errorf("bob's widget: %+v", got)
	}
}

func capture(t *testing.T, c *client, body map[string]any) (int, core.CaptureResult, string) {
	t.Helper()
	w, _ := c.do("POST", "/api/v1/pocket/capture", body)
	var res core.CaptureResult
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	return w.Code, res, w.Body.String()
}

func TestPocketCapture(t *testing.T) {
	s, h := testServer(t)
	alice := newPerson(t, s, h, "alice@example.com")
	bob := newPerson(t, s, h, "bob@example.com")
	u, _ := s.store.GetUserByEmail(context.Background(), "alice@example.com")
	today, _, _ := s.today(context.Background(), u.ID)
	inbox := inboxID(t, alice)

	// a line with no title: the line is the title and is not repeated as notes
	line := map[string]any{"request_id": "hub-0000000000000001", "kind": "task", "text": "  Call the plumber  "}
	code, first, body := capture(t, alice, line)
	if code != 201 || first.ID == "" || first.Title != "Call the plumber" || first.URL != "/list/"+itoa(inbox)+"?task="+first.ID {
		t.Fatalf("capture: %d %s", code, body)
	}
	_, task := alice.do("GET", "/api/v1/tasks/"+first.ID, nil)
	if task["title"] != "Call the plumber" || task["notes"] != "" || task["list_id"] != float64(inbox) || task["start"] != today || task["due"] != nil || task["status"] != "open" {
		t.Errorf("the task it made: %v", task)
	}
	// logged like a task made in the app, apart from where it came from
	act, _ := task["activity"].([]any)
	if len(act) != 1 || act[0].(map[string]any)["kind"] != "created" || act[0].(map[string]any)["detail"] != "capture" {
		t.Errorf("activity: %v", task["activity"])
	}

	// the same request again answers the same task and adds nothing
	code, again, body := capture(t, alice, line)
	if code != 201 || again != first {
		t.Errorf("same request_id: %d %s, want %+v", code, body, first)
	}
	if _, all := listOf(t, alice, "/api/v1/tasks?view=all&status=all"); all.Total != 1 {
		t.Errorf("same request_id twice made %d tasks", all.Total)
	}
	// …and it is not a way to keep something else
	if code, _, body := capture(t, alice, map[string]any{"request_id": "hub-0000000000000001", "text": "Something else"}); code != 400 {
		t.Errorf("same request_id, other words: %d %s", code, body)
	}
	// another person's same request_id is their own task
	code, his, _ := capture(t, bob, line)
	if code != 201 || his.ID == first.ID {
		t.Errorf("bob's capture: %d %+v", code, his)
	}
	if w, _ := bob.do("GET", "/api/v1/tasks/"+first.ID, nil); w.Code != 404 {
		t.Errorf("bob reads alice's captured task: %d", w.Code)
	}

	// title, text and a due day: the text becomes the notes, in the list new tasks go to
	errands := listID(t, alice, "Errands")
	if w, _ := alice.do("PUT", "/api/v1/settings", map[string]any{"default_list_id": errands}); w.Code != 200 {
		t.Fatalf("set default list: %d %s", w.Code, w.Body.String())
	}
	code, full, body := capture(t, alice, map[string]any{"request_id": "hub-0000000000000002", "title": "Buy paint", "text": "Two tins of white\nand a brush", "due": "2026-12-05"})
	if code != 201 || full.Title != "Buy paint" || full.URL != "/list/"+itoa(errands)+"?task="+full.ID {
		t.Fatalf("capture with a title: %d %s", code, body)
	}
	_, task = alice.do("GET", "/api/v1/tasks/"+full.ID, nil)
	if task["notes"] != "Two tins of white\nand a brush" || task["due"] != "2026-12-05" || task["start"] != nil || task["list_id"] != float64(errands) {
		t.Errorf("the task with a title: %v", task)
	}
	// several lines and no title: the first line is the title, the rest the notes
	_, lines, _ := capture(t, alice, map[string]any{"request_id": "hub-0000000000000003", "text": "Pack for the trip\npassport\ncharger"})
	_, task = alice.do("GET", "/api/v1/tasks/"+lines.ID, nil)
	if lines.Title != "Pack for the trip" || task["notes"] != "passport\ncharger" {
		t.Errorf("several lines: %+v %v", lines, task["notes"])
	}

	for name, bad := range map[string]map[string]any{
		"nothing to keep": {"request_id": "hub-0000000000000004", "kind": "task"},
		"only spaces":     {"request_id": "hub-0000000000000004", "text": "   "},
		"no request_id":   {"text": "Call the plumber"},
		"unknown kind":    {"request_id": "hub-0000000000000004", "kind": "note", "text": "Call the plumber"},
		"not a day":       {"request_id": "hub-0000000000000004", "text": "Call the plumber", "due": "tomorrow"},
	} {
		if code, _, body := capture(t, alice, bad); code != 400 {
			t.Errorf("%s: %d %s", name, code, body)
		}
	}
	if _, all := listOf(t, alice, "/api/v1/tasks?view=all&status=all"); all.Total != 3 {
		t.Errorf("alice has %d tasks, want 3", all.Total)
	}
}

// The same line sent several times at once (a retry racing the first try)
// still makes one task, and every answer names it.
func TestPocketCaptureAtOnce(t *testing.T) {
	s, h := testServer(t)
	alice := newPerson(t, s, h, "alice@example.com")
	line := map[string]any{"request_id": "hub-000000000000000a", "text": "Only once"}
	const n = 8
	ids := make(chan string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w, _ := alice.do("POST", "/api/v1/pocket/capture", line)
			var res core.CaptureResult
			_ = json.Unmarshal(w.Body.Bytes(), &res)
			if w.Code != 201 {
				res.ID = "HTTP " + itoa(w.Code) + " " + w.Body.String()
			}
			ids <- res.ID
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]int{}
	for id := range ids {
		seen[id]++
	}
	if len(seen) != 1 {
		t.Errorf("answers: %v", seen)
	}
	if _, all := listOf(t, alice, "/api/v1/tasks?view=all&status=all"); all.Total != 1 {
		t.Errorf("%d tasks, want 1", all.Total)
	}
}

func TestPocketLinkTokens(t *testing.T) {
	fakeCoreURL = fakeCore(t).URL
	t.Cleanup(func() { fakeCoreURL = "" })
	s, h := testServer(t)
	alice := newPerson(t, s, h, "alice@example.com")
	bob := newPerson(t, s, h, "bob@example.com")
	mustCreate(t, alice, map[string]any{"title": "Alice's bread", "due": "2026-11-20"})
	mustCreate(t, bob, map[string]any{"title": "Bob's bread", "due": "2026-11-20"})

	line := map[string]any{"request_id": "hub-0000000000000001", "text": "Kept through a link"}
	routes := []struct {
		scope, method, path string
		body                any
		ok                  int
	}{
		{"search", "GET", "/api/v1/pocket/search?q=bread", nil, 200},
		{"day", "GET", "/api/v1/pocket/day/2026-11-20?tz=Europe/Sarajevo", nil, 200},
		{"widget", "GET", "/api/v1/pocket/widget", nil, 200},
		{"capture", "POST", "/api/v1/pocket/capture", line, 201},
	}
	for _, tok := range []string{"search", "day", "widget", "capture", "read", "rw"} {
		c := &client{t: t, h: h, token: "pcx_" + tok}
		for _, rt := range routes {
			want := 403
			if rt.scope == tok {
				want = rt.ok
			}
			w, _ := c.do(rt.method, rt.path, rt.body)
			if w.Code != want {
				t.Errorf("pcx_%s %s %s: %d, want %d", tok, rt.method, rt.path, w.Code, want)
			}
			if strings.Contains(w.Body.String(), "Bob") {
				t.Errorf("pcx_%s %s: another person's task: %s", tok, rt.path, w.Body.String())
			}
		}
		if w, _ := c.do("GET", "/api/v1/auth/me", nil); w.Code != 200 {
			t.Errorf("pcx_%s /auth/me: %d", tok, w.Code)
		}
		if tok == "read" || tok == "rw" {
			continue
		}
		// a capability opens its one route with its one method, nothing else of the app
		for _, rt := range []struct{ method, path string }{
			{"GET", "/api/v1/tasks"}, {"GET", "/api/v1/search?q=bread"}, {"GET", "/api/v1/dashboard"}, {"POST", "/api/v1/tasks"},
			{"GET", "/api/v1/export.json"}, {"GET", "/api/v1/pocket/portable"},
		} {
			if w, _ := c.do(rt.method, rt.path, map[string]any{"title": "nope"}); w.Code != 403 {
				t.Errorf("pcx_%s %s %s: %d, want 403", tok, rt.method, rt.path, w.Code)
			}
		}
	}

	// the other method on a capability's own route is no way in either
	for _, rt := range []struct{ tok, method, path string }{{"search", "POST", "/api/v1/pocket/search?q=bread"}, {"capture", "GET", "/api/v1/pocket/capture"}, {"widget", "DELETE", "/api/v1/pocket/widget"}} {
		if w, _ := (&client{t: t, h: h, token: "pcx_" + rt.tok}).do(rt.method, rt.path, nil); w.Code != 403 && w.Code != 405 {
			t.Errorf("pcx_%s %s %s: %d", rt.tok, rt.method, rt.path, w.Code)
		}
	}

	// what came back is alice's, and only hers
	var found core.SearchResults
	getInto(t, &client{t: t, h: h, token: "pcx_search"}, "GET", "/api/v1/pocket/search?q=bread", nil, &found)
	if len(found.Items) != 1 || found.Items[0].Title != "Alice's bread" {
		t.Errorf("link search: %+v", found.Items)
	}
	var day core.Day
	getInto(t, &client{t: t, h: h, token: "pcx_day"}, "GET", "/api/v1/pocket/day/2026-11-20", nil, &day)
	if len(day.Items) != 1 || day.Items[0].Title != "Alice's bread" || day.Items[0].Kind != "due" {
		t.Errorf("link day: %+v", day)
	}
	var wd core.WidgetData
	getInto(t, &client{t: t, h: h, token: "pcx_widget"}, "GET", "/api/v1/pocket/widget", nil, &wd)
	if wd.Line != "1 due today, 1 late" || len(wd.Items) != 2 || wd.Items[0].Title != "Alice's bread" || wd.Items[1].Title != "Kept through a link" {
		t.Errorf("link widget: %+v", wd)
	}

	// the same capture again through the link: still one task, and it is alice's
	hub := &client{t: t, h: h, token: "pcx_capture"}
	code, first, body := capture(t, hub, line)
	code2, again, _ := capture(t, hub, line)
	if code != 201 || code2 != 201 || first.ID == "" || first != again {
		t.Fatalf("link capture twice: %d %d %s", code, code2, body)
	}
	_, kept := listOf(t, alice, "/api/v1/tasks?view=all&q=kept")
	if kept.Total != 1 || kept.Tasks[0].Title != "Kept through a link" {
		t.Errorf("alice's captured tasks: %+v", kept)
	}
	if _, his := listOf(t, bob, "/api/v1/tasks?view=all&q=kept"); his.Total != 0 {
		t.Errorf("bob got the captured task: %+v", his)
	}
	if code, _, body := capture(t, hub, map[string]any{"request_id": "hub-0000000000000009"}); code != 400 {
		t.Errorf("link capture with nothing: %d %s", code, body)
	}
}

// The manifest Core gets says what Tasks can do, so nobody has to list it.
func TestManifestOffersCapabilities(t *testing.T) {
	s, _ := testServer(t)
	b, err := json.Marshal(s.manifest())
	if err != nil {
		t.Fatal(err)
	}
	var sent map[string]any
	_ = json.Unmarshal(b, &sent)
	if sent["slug"] != "tasks" || sent["category"] != "Everyday" {
		t.Errorf("manifest: slug %v, category %v", sent["slug"], sent["category"])
	}
	for _, k := range []string{"search", "day", "widget", "capture"} {
		if sent[k] == nil {
			t.Errorf("manifest has no %q", k)
		}
	}
	kinds, _ := sent["capture"].(map[string]any)["kinds"].([]any)
	if len(kinds) != 1 || kinds[0].(map[string]any)["kind"] != "task" {
		t.Errorf("capture kinds: %v", sent["capture"])
	}
	// Register adds one offer per capability
	offered := map[string]bool{}
	for _, o := range capabilities.Offers() {
		offered[o.Scope] = true
	}
	for _, sc := range []string{core.ScopeSearch, core.ScopeDay, core.ScopeWidget, core.ScopeCapture} {
		if !offered[sc] {
			t.Errorf("no offer for %s: %v", sc, offered)
		}
	}
}
