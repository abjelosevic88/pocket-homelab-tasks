package server

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
)

// moveCore answers introspection for Pocket Move's link tokens acting for alice.
func moveCore(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/auth/introspect" {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		var in struct{ Token string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		scope := map[string]string{"pcx_export": core.ScopeMoveExport, "pcx_import": core.ScopeMoveImport}[in.Token]
		if scope == "" {
			_, _ = w.Write([]byte(`{"active":false}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"active": true, "kind": "delegated", "iss": "http://core.test", "scope": scope,
			"audience": "tasks", "app": map[string]any{"id": 9, "slug": "move", "name": "Pocket Move"},
			"user": map[string]any{"id": 1, "sub": "alice-sub", "email": "alice@example.com", "name": "Alice"}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// upload posts a file as multipart field "file", like Pocket Move does.
func (c *client) upload(path, name, content string) (*httptest.ResponseRecorder, map[string]any) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", name)
	_, _ = fw.Write([]byte(content))
	_ = mw.Close()
	req := httptest.NewRequest("POST", path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w, out
}

// tasksOf lists a person's live tasks as "list/title" (subtasks "list/parent>title").
func tasksOf(t *testing.T, s *Server, email string) []string {
	t.Helper()
	ctx := context.Background()
	u, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := s.store.LiveTasks(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	lists, _ := s.store.ListLists(ctx, u.ID)
	names := map[int64]string{}
	for _, l := range lists {
		names[l.ID] = l.Name
	}
	byID := map[int64]string{}
	for _, x := range tasks {
		byID[x.ID] = x.Title
	}
	out := []string{}
	for _, x := range tasks {
		title := x.Title
		if x.ParentID != nil {
			title = byID[*x.ParentID] + ">" + title
		}
		out = append(out, names[x.ListID]+"/"+title)
	}
	sort.Strings(out)
	return out
}

func TestPortableLinkScopes(t *testing.T) {
	fakeCoreURL = moveCore(t).URL
	t.Cleanup(func() { fakeCoreURL = "" })
	s, h := testServer(t)
	alice := newPerson(t, s, h, "alice@example.com")
	newPerson(t, s, h, "bob@example.com")
	if w, _ := alice.do("POST", "/api/v1/tasks", map[string]any{"title": "Alice's task"}); w.Code != 201 {
		t.Fatalf("create: %d", w.Code)
	}
	exp := &client{t: t, h: h, token: "pcx_export"}
	imp := &client{t: t, h: h, token: "pcx_import"}

	w, _ := exp.do("GET", core.PortablePath, nil)
	var desc core.Portable
	_ = json.Unmarshal(w.Body.Bytes(), &desc)
	if w.Code != 200 || len(desc.Exports) != 2 || len(desc.Imports) != 2 || desc.Exports[0].Kind != core.KindPocket || desc.Imports[1].Kind != core.KindICalTodos || desc.Imports[0].Form != "file" {
		t.Fatalf("description: %d %s", w.Code, w.Body.String())
	}
	if w, _ := exp.do("GET", "/api/v1/export.json", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "Alice's task") {
		t.Errorf("export.json: %d %s", w.Code, w.Body.String())
	}
	if w, _ := exp.do("GET", "/api/v1/export.ics", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "Alice's task") {
		t.Errorf("export.ics: %d", w.Code)
	}
	// move:export can't bring anything in or reach anything else
	if w, _ := exp.upload("/api/v1/import", "x.ics", "BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n"); w.Code != 403 {
		t.Errorf("export token imports: %d", w.Code)
	}
	for _, p := range []string{"/api/v1/settings", "/api/v1/tasks", "/api/v1/users", "/api/v1/backup", "/api/v1/tokens"} {
		if w, _ := exp.do("GET", p, nil); w.Code != 403 {
			t.Errorf("export token GET %s: %d", p, w.Code)
		}
	}
	// move:import reads the description and posts imports, nothing else
	if w, _ := imp.do("GET", core.PortablePath, nil); w.Code != 200 {
		t.Errorf("import token description: %d", w.Code)
	}
	for _, p := range []string{"/api/v1/export.json", "/api/v1/export.ics", "/api/v1/settings", "/api/v1/tasks"} {
		if w, _ := imp.do("GET", p, nil); w.Code != 403 {
			t.Errorf("import token GET %s: %d", p, w.Code)
		}
	}
	if w, _ := imp.do("POST", "/api/v1/tasks", map[string]any{"title": "nope"}); w.Code != 403 {
		t.Errorf("import token creates a task: %d", w.Code)
	}
	if w, _ := imp.do("PUT", "/api/v1/settings", map[string]any{}); w.Code != 403 {
		t.Errorf("import token settings: %d", w.Code)
	}
	w, out := imp.upload("/api/v1/import", "reminders.ics", "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VTODO\r\nUID:m-1\r\nSUMMARY:Moved task\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")
	if w.Code != 200 || out["imported"] != float64(1) {
		t.Fatalf("link import: %d %s", w.Code, w.Body.String())
	}
	// it went to alice, not bob
	if got := tasksOf(t, s, "alice@example.com"); strings.Join(got, ",") != "Inbox/Alice's task,Moved in/Moved task" {
		t.Errorf("alice: %v", got)
	}
	if got := tasksOf(t, s, "bob@example.com"); len(got) != 0 {
		t.Errorf("bob: %v", got)
	}
}

const remindersICS = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Pocket Move//EN\r\n" +
	"BEGIN:VTODO\r\nUID:g-1\r\nSUMMARY:Buy milk\r\nDESCRIPTION:two litres\r\nDUE;VALUE=DATE:20261201\r\nPRIORITY:1\r\nCATEGORIES:home,errands\r\nX-WR-CALNAME:Shopping\r\nEND:VTODO\r\n" +
	// a subtask before its parent, two levels deep
	"BEGIN:VTODO\r\nUID:g-3\r\nSUMMARY:Pick paint\r\nRELATED-TO:g-2\r\nX-WR-CALNAME:House\r\nEND:VTODO\r\n" +
	"BEGIN:VTODO\r\nUID:g-4\r\nSUMMARY:Ask about white\r\nRELATED-TO;RELTYPE=PARENT:g-3\r\nX-WR-CALNAME:House\r\nEND:VTODO\r\n" +
	"BEGIN:VTODO\r\nUID:g-2\r\nSUMMARY:Paint the hall\r\nX-WR-CALNAME:House\r\nEND:VTODO\r\n" +
	"BEGIN:VTODO\r\nUID:g-5\r\nSUMMARY:Old one\r\nSTATUS:COMPLETED\r\nCOMPLETED:20260101T100000Z\r\nEND:VTODO\r\n" +
	// no UID: still not doubled on a second run
	"BEGIN:VTODO\r\nSUMMARY:No uid\r\nEND:VTODO\r\n" +
	"END:VCALENDAR\r\n"

func TestImportICalTodosTwice(t *testing.T) {
	s, h := testServer(t)
	alice := newPerson(t, s, h, "alice@example.com")
	bob := newPerson(t, s, h, "bob@example.com")
	if w, _ := alice.do("POST", "/api/v1/lists", map[string]any{"name": "shopping"}); w.Code != 201 {
		t.Fatalf("list: %d", w.Code)
	}
	w, out := alice.upload("/api/v1/import", "tasks.ics", remindersICS)
	if w.Code != 200 || out["imported"] != float64(6) || out["skipped"] != float64(0) {
		t.Fatalf("first: %d %s", w.Code, w.Body.String())
	}
	want := "House/Paint the hall,House/Paint the hall>Pick paint,House/Pick paint>Ask about white,Moved in/No uid,Moved in/Old one,shopping/Buy milk"
	if got := strings.Join(tasksOf(t, s, "alice@example.com"), ","); got != want {
		t.Errorf("after first:\n got %s\nwant %s", got, want)
	}
	ctx := context.Background()
	u, _ := s.store.GetUserByEmail(ctx, "alice@example.com")
	milk, err := s.store.GetTaskByUID(ctx, u.ID, "g-1")
	if err != nil || milk.Notes != "two litres" || milk.Due == nil || *milk.Due != "2026-12-01" || milk.Priority != 1 || strings.Join(milk.Tags, ",") != "errands,home" {
		t.Errorf("milk: %+v %v", milk, err)
	}
	if old, _ := s.store.GetTaskByUID(ctx, u.ID, "g-5"); old == nil || old.Status != "done" {
		t.Errorf("completed: %+v", old)
	}
	lists1, _ := s.store.ListLists(ctx, u.ID)

	w, out = alice.upload("/api/v1/import", "tasks.ics", remindersICS)
	if w.Code != 200 || out["imported"] != float64(0) || out["skipped"] != float64(6) {
		t.Fatalf("second: %d %s", w.Code, w.Body.String())
	}
	if got := strings.Join(tasksOf(t, s, "alice@example.com"), ","); got != want {
		t.Errorf("after second: %s", got)
	}
	if lists2, _ := s.store.ListLists(ctx, u.ID); len(lists2) != len(lists1) {
		t.Errorf("lists: %d then %d", len(lists1), len(lists2))
	}

	// bob has his own copy, the same UIDs don't clash across people
	if w, out := bob.upload("/api/v1/import", "tasks.ics", remindersICS); w.Code != 200 || out["imported"] != float64(6) {
		t.Fatalf("bob: %d %s", w.Code, w.Body.String())
	}
	if got := len(tasksOf(t, s, "alice@example.com")); got != 6 {
		t.Errorf("alice after bob: %d", got)
	}
}

func TestImportPocketExportTwice(t *testing.T) {
	s, h := testServer(t)
	alice := newPerson(t, s, h, "alice@example.com")
	bob := newPerson(t, s, h, "bob@example.com")
	w, list := alice.do("POST", "/api/v1/lists", map[string]any{"name": "Garden"})
	if w.Code != 201 {
		t.Fatalf("list: %d", w.Code)
	}
	w, parent := alice.do("POST", "/api/v1/tasks", map[string]any{"title": "Plant tomatoes", "list_id": list["id"], "due": "2026-12-05"})
	if w.Code != 201 {
		t.Fatalf("task: %d", w.Code)
	}
	if w, _ := alice.do("POST", "/api/v1/tasks", map[string]any{"title": "Buy seeds", "list_id": list["id"], "parent_id": parent["id"]}); w.Code != 201 {
		t.Fatalf("subtask: %d %s", w.Code, w.Body.String())
	}
	if w, _ := alice.do("POST", "/api/v1/tasks", map[string]any{"title": "Call mum"}); w.Code != 201 {
		t.Fatalf("task: %d", w.Code)
	}
	w, _ = alice.do("GET", "/api/v1/export.json", nil)
	if w.Code != 200 {
		t.Fatalf("export: %d", w.Code)
	}
	export := w.Body.String()
	want := "Garden/Plant tomatoes,Garden/Plant tomatoes>Buy seeds,Inbox/Call mum"

	// alice's own file back: nothing doubles
	if w, out := alice.upload("/api/v1/import", "tasks.json", export); w.Code != 200 || out["imported"] != float64(0) || out["skipped"] != float64(3) {
		t.Fatalf("alice again: %d %s", w.Code, w.Body.String())
	}
	if got := strings.Join(tasksOf(t, s, "alice@example.com"), ","); got != want {
		t.Errorf("alice: %s", got)
	}
	// into bob (another box, in effect), twice
	for i, wantN := range []float64{3, 0} {
		w, out := bob.upload("/api/v1/import", "tasks.json", export)
		if w.Code != 200 || out["imported"] != wantN {
			t.Fatalf("bob run %d: %d %s", i, w.Code, w.Body.String())
		}
		if got := strings.Join(tasksOf(t, s, "bob@example.com"), ","); got != want {
			t.Errorf("bob run %d: %s", i, got)
		}
	}
	ctx := context.Background()
	u, _ := s.store.GetUserByEmail(ctx, "bob@example.com")
	if lists, _ := s.store.ListLists(ctx, u.ID); len(lists) != 2 {
		t.Errorf("bob's lists: %d", len(lists))
	}

	// the ICS export comes back with its lists too
	w, _ = alice.do("GET", "/api/v1/export.ics", nil)
	carol := newPerson(t, s, h, "carol@example.com")
	if w, out := carol.upload("/api/v1/import", "tasks.ics", w.Body.String()); w.Code != 200 || out["imported"] != float64(3) {
		t.Fatalf("carol: %d %s", w.Code, w.Body.String())
	}
	if got := strings.Join(tasksOf(t, s, "carol@example.com"), ","); got != want {
		t.Errorf("carol: %s", got)
	}
}
