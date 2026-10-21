package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"testing"
)

type taskList struct {
	Tasks []struct {
		ID     int64  `json:"id"`
		Title  string `json:"title"`
		URL    string `json:"url"`
		Notes  string `json:"notes"`
		Due    string `json:"due"`
		Status string `json:"status"`
	} `json:"tasks"`
	Total int `json:"total"`
}

func listOf(t *testing.T, c *client, path string) (int, taskList) {
	t.Helper()
	w, _ := c.do("GET", path, nil)
	var out taskList
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func titles(l taskList) []string {
	out := []string{}
	for _, t := range l.Tasks {
		out = append(out, t.Title)
	}
	sort.Strings(out)
	return out
}

func TestTasksByURL(t *testing.T) {
	s, h := testServer(t)
	alice := newPerson(t, s, h, "alice@example.com")
	bob := newPerson(t, s, h, "bob@example.com")
	page := "https://contacts.example/contacts/12"
	for _, body := range []map[string]any{
		{"title": "url field", "url": page},
		{"title": "notes link", "notes": "About [Ana](https://Contacts.Example/contacts/12)."},
		{"title": "below it", "notes": "see https://contacts.example/contacts/12/edit"},
		{"title": "with a query", "notes": "https://contacts.example/contacts/12?tab=notes"},
		{"title": "other contact", "url": "https://contacts.example/contacts/123", "notes": "https://contacts.example/contacts/123"},
		{"title": "other host", "notes": "https://elsewhere.example/contacts/12"},
		{"title": "no link", "notes": "contacts/12"},
		{"title": "finished", "url": page, "status": "done"},
	} {
		if w, _ := alice.do("POST", "/api/v1/tasks", body); w.Code != 201 {
			t.Fatalf("create %v: %d %s", body["title"], w.Code, w.Body.String())
		}
	}
	if w, _ := bob.do("POST", "/api/v1/tasks", map[string]any{"title": "bob's", "url": page}); w.Code != 201 {
		t.Fatalf("bob create: %d", w.Code)
	}

	q := "/api/v1/tasks?view=all&status=open&limit=500&url=" + url.QueryEscape(page)
	code, got := listOf(t, alice, q)
	want := []string{"below it", "notes link", "url field", "with a query"}
	if code != 200 || len(got.Tasks) != len(want) || got.Total != len(want) {
		t.Fatalf("open by url: %d %v total=%d, want %v", code, titles(got), got.Total, want)
	}
	for i, v := range titles(got) {
		if v != want[i] {
			t.Errorf("open by url: got %v, want %v", titles(got), want)
			break
		}
	}

	// /contacts/123 must not pull in /contacts/12, and the other way round
	_, got = listOf(t, alice, "/api/v1/tasks?view=all&url="+url.QueryEscape("https://contacts.example/contacts/123"))
	if len(got.Tasks) != 1 || got.Tasks[0].Title != "other contact" {
		t.Errorf("/123: %v", titles(got))
	}
	// scheme, host case and a trailing slash don't matter; status=all includes the finished one
	_, got = listOf(t, alice, "/api/v1/tasks?view=all&status=all&url="+url.QueryEscape("http://CONTACTS.example/contacts/12/"))
	if len(got.Tasks) != 5 {
		t.Errorf("status=all: %v", titles(got))
	}
	// limit pages through the matches; total counts them all
	_, got = listOf(t, alice, "/api/v1/tasks?status=open&limit=1&url="+url.QueryEscape(page))
	if len(got.Tasks) != 1 || got.Total != 4 {
		t.Errorf("limit=1: %d tasks, total %d", len(got.Tasks), got.Total)
	}
	// strictly per person
	_, got = listOf(t, bob, q)
	if len(got.Tasks) != 1 || got.Tasks[0].Title != "bob's" {
		t.Errorf("bob sees: %v", titles(got))
	}
	// no match is an empty list, not everything
	_, got = listOf(t, alice, "/api/v1/tasks?url="+url.QueryEscape("https://contacts.example/contacts/99"))
	if len(got.Tasks) != 0 || got.Total != 0 {
		t.Errorf("no match: %v", titles(got))
	}
	if code, _ := listOf(t, alice, "/api/v1/tasks?url=contacts/12"); code != 400 {
		t.Errorf("not a web address: %d", code)
	}
}

// fakeCore answers introspection for two link tokens acting for alice.
func fakeCore(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/auth/introspect" {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		var in struct{ Token string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		scope := map[string]string{"pcx_read": "tasks:read", "pcx_rw": "tasks:read tasks:write"}[in.Token]
		if scope == "" {
			_, _ = w.Write([]byte(`{"active":false}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"active": true, "kind": "delegated", "iss": "http://core.test", "scope": scope,
			"audience": "tasks", "app": map[string]any{"id": 7, "slug": "contacts", "name": "Pocket Contacts"},
			"user": map[string]any{"id": 1, "sub": "alice-sub", "email": "alice@example.com", "name": "Alice"}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestLinkTokenTasksByURL(t *testing.T) {
	t.Setenv("CORE_URL", fakeCore(t).URL)
	s, h := testServer(t)
	alice := newPerson(t, s, h, "alice@example.com")
	bob := newPerson(t, s, h, "bob@example.com")
	page := "https://contacts.example/contacts/12"
	if w, _ := bob.do("POST", "/api/v1/tasks", map[string]any{"title": "bob's", "url": page}); w.Code != 201 {
		t.Fatalf("bob create: %d", w.Code)
	}
	reader := &client{t: t, h: h, token: "pcx_read"}
	writer := &client{t: t, h: h, token: "pcx_rw"}

	// what Pocket Contacts sends for "Remind me"
	w, task := writer.do("POST", "/api/v1/tasks", map[string]any{"title": "Call Ana", "due": "2026-10-23", "url": page, "notes": "About [Ana](" + page + ")"})
	if w.Code != 201 {
		t.Fatalf("link create: %d %s", w.Code, w.Body.String())
	}
	if task["url"] != page || task["due"] != "2026-10-23" || task["status"] != "open" || task["id"] == nil {
		t.Errorf("created task: %v", task)
	}
	// it is alice's
	w, mine := alice.do("GET", "/api/v1/tasks/"+itoa(int(task["id"].(float64))), nil)
	if w.Code != 200 || mine["url"] != page {
		t.Errorf("alice's view: %d %v", w.Code, mine["url"])
	}

	// what Pocket Contacts asks for the person's page
	code, got := listOf(t, reader, "/api/v1/tasks?view=all&status=open&limit=500&url="+url.QueryEscape(page))
	if code != 200 || len(got.Tasks) != 1 || got.Tasks[0].Title != "Call Ana" || got.Tasks[0].URL != page {
		t.Errorf("link read: %d %+v", code, got)
	}

	// tasks:read alone can't write; an unknown token is no one
	if w, _ := reader.do("POST", "/api/v1/tasks", map[string]any{"title": "nope"}); w.Code != 403 {
		t.Errorf("read token writes: %d", w.Code)
	}
	if w, _ := (&client{t: t, h: h, token: "pcx_gone"}).do("GET", "/api/v1/tasks", nil); w.Code != 401 {
		t.Errorf("inactive token: %d", w.Code)
	}
}
