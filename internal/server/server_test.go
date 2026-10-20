package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-tasks/internal/config"
	"github.com/abjelosevic88/pocket-homelab-tasks/internal/store"
)

func testServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	os.Setenv("DATA_DIR", dir)
	os.Setenv("AUTH_MODE", "local")
	os.Setenv("TZ", "Europe/Sarajevo")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(cfg.DBPath, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := New(cfg, st, log, nil)
	if err := s.core.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s, s.Router()
}

type client struct {
	t     *testing.T
	h     http.Handler
	token string
}

func (c *client) do(method, path string, body any, headers ...string) (*httptest.ResponseRecorder, map[string]any) {
	var rd io.Reader
	if body != nil {
		if s, ok := body.(string); ok {
			rd = strings.NewReader(s)
		} else {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
	}
	req := httptest.NewRequest(method, path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w, out
}

// newPerson makes an account and returns a client with an API token.
func newPerson(t *testing.T, s *Server, h http.Handler, email string) *client {
	t.Helper()
	ctx := context.Background()
	u, err := s.firstOrSystemUser(ctx, email, strings.Split(email, "@")[0])
	if err != nil {
		t.Fatal(err)
	}
	plain, _, err := s.store.CreateAPIToken(ctx, u.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	return &client{t: t, h: h, token: plain}
}

func TestPerPersonIsolation(t *testing.T) {
	s, h := testServer(t)
	alice := newPerson(t, s, h, "alice@example.com")
	bob := newPerson(t, s, h, "bob@example.com")

	w, task := alice.do("POST", "/api/v1/tasks", map[string]any{"title": "Alice's secret", "due": "2026-10-21"})
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	id := int(task["id"].(float64))
	w, lists := alice.do("GET", "/api/v1/lists", nil)
	_ = lists
	var aliceLists []map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &aliceLists)
	inbox := int(aliceLists[0]["id"].(float64))

	// Bob (an admin? no: the first account is admin — make Bob the admin to be strict)
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/v1/tasks/" + itoa(id), nil},
		{"PUT", "/api/v1/tasks/" + itoa(id), map[string]any{"title": "hacked"}},
		{"DELETE", "/api/v1/tasks/" + itoa(id), nil},
		{"POST", "/api/v1/tasks/" + itoa(id) + "/complete", map[string]any{"done": true}},
		{"GET", "/api/v1/tasks/" + itoa(id) + "/ics", nil},
		{"PUT", "/api/v1/lists/" + itoa(inbox), map[string]any{"name": "hacked"}},
		{"GET", "/api/v1/lists/" + itoa(inbox) + "/sections", nil},
		{"POST", "/api/v1/trash/" + itoa(id) + "/restore", nil},
	} {
		w, _ := bob.do(c.method, c.path, c.body)
		if w.Code != 404 {
			t.Errorf("%s %s by another person: got %d, want 404", c.method, c.path, w.Code)
		}
	}
	w, _ = bob.do("GET", "/api/v1/tasks?q=secret", nil)
	if strings.Contains(w.Body.String(), "secret") {
		t.Error("search leaked across people")
	}
	w, _ = bob.do("GET", "/api/v1/tasks?view=today", nil)
	if strings.Contains(w.Body.String(), "secret") {
		t.Error("today view leaked across people")
	}
	// posting into Alice's list by id must fail too
	w, _ = bob.do("POST", "/api/v1/tasks", map[string]any{"title": "into alice", "list_id": inbox})
	if w.Code != 400 {
		t.Errorf("create into another person's list: %d", w.Code)
	}
	// Alice still sees hers untouched
	w, got := alice.do("GET", "/api/v1/tasks/"+itoa(id), nil)
	if w.Code != 200 || got["title"] != "Alice's secret" {
		t.Errorf("alice's task changed: %d %v", w.Code, got["title"])
	}
}

func TestCalDAVIsolationAndBasics(t *testing.T) {
	s, h := testServer(t)
	alice := newPerson(t, s, h, "alice@example.com")
	bob := newPerson(t, s, h, "bob@example.com")
	// Bob asks for Alice's home → 404
	w, _ := bob.do("PROPFIND", "/dav/calendars/alice/", `<?xml version="1.0"?><d:propfind xmlns:d="DAV:"><d:prop><d:displayname/></d:prop></d:propfind>`, "Depth", "1")
	if w.Code != 404 {
		t.Errorf("bob at alice's home: %d", w.Code)
	}
	// no auth → 401 with a Basic challenge
	anon := &client{t: t, h: h}
	w, _ = anon.do("PROPFIND", "/dav/", nil, "Depth", "0")
	if w.Code != 401 || !strings.Contains(w.Header().Get("WWW-Authenticate"), "Basic") {
		t.Errorf("anonymous: %d %q", w.Code, w.Header().Get("WWW-Authenticate"))
	}
	// Alice: PUT a task over CalDAV, see it in the API, sync token moves
	w, _ = alice.do("PROPFIND", "/dav/calendars/me/", `<?xml version="1.0"?><d:propfind xmlns:d="DAV:"><d:prop><d:displayname/><d:resourcetype/></d:prop></d:propfind>`, "Depth", "1")
	if w.Code != 207 || !strings.Contains(w.Body.String(), "/inbox/") {
		t.Fatalf("home: %d %s", w.Code, w.Body.String())
	}
	ics := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\nBEGIN:VTODO\r\nUID:abc-1\r\nSUMMARY:Over DAV\r\nDUE;VALUE=DATE:20261101\r\nSTATUS:NEEDS-ACTION\r\nEND:VTODO\r\nEND:VCALENDAR\r\n"
	w, _ = alice.do("PUT", "/dav/calendars/alice/inbox/abc-1.ics", ics, "Content-Type", "text/calendar", "If-None-Match", "*")
	if w.Code != 201 || w.Header().Get("ETag") == "" {
		t.Fatalf("put: %d %s", w.Code, w.Body.String())
	}
	etag := w.Header().Get("ETag")
	w, _ = alice.do("GET", "/api/v1/tasks?view=upcoming", nil)
	if !strings.Contains(w.Body.String(), "Over DAV") {
		t.Errorf("task not in the API: %s", w.Body.String())
	}
	// stale If-Match → 412
	w, _ = alice.do("PUT", "/dav/calendars/alice/inbox/abc-1.ics", ics, "Content-Type", "text/calendar", "If-Match", `"nope"`)
	if w.Code != 412 {
		t.Errorf("stale if-match: %d", w.Code)
	}
	w, _ = alice.do("PUT", "/dav/calendars/alice/inbox/abc-1.ics", strings.Replace(ics, "Over DAV", "Over DAV 2", 1), "Content-Type", "text/calendar", "If-Match", etag)
	if w.Code != 204 {
		t.Errorf("good if-match: %d", w.Code)
	}
	// bob cannot read it even with the right path shape
	w, _ = bob.do("GET", "/dav/calendars/bob/inbox/abc-1.ics", nil)
	if w.Code != 404 {
		t.Errorf("bob reads alice's resource: %d", w.Code)
	}
	// the mirror shows it as an event
	w, _ = alice.do("REPORT", "/dav/calendars/alice/tasks-calendar/", `<?xml version="1.0"?><c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><c:calendar-data/></d:prop><c:filter><c:comp-filter name="VCALENDAR"><c:comp-filter name="VEVENT"/></c:comp-filter></c:filter></c:calendar-query>`, "Depth", "1")
	if w.Code != 207 || !strings.Contains(w.Body.String(), "Over DAV 2") || !strings.Contains(w.Body.String(), "BEGIN:VEVENT") {
		t.Errorf("mirror: %d %s", w.Code, w.Body.String())
	}
	// the feed needs the token, not a session
	ctx := context.Background()
	u, _ := s.store.GetUserByEmail(ctx, "alice@example.com")
	w, _ = anon.do("GET", "/feed/"+u.FeedToken+".ics", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Over DAV 2") {
		t.Errorf("feed: %d", w.Code)
	}
	w, _ = anon.do("GET", "/feed/wrong.ics", nil)
	if w.Code != 404 {
		t.Errorf("wrong feed token: %d", w.Code)
	}
}

func TestQuickAndRepeat(t *testing.T) {
	s, h := testServer(t)
	alice := newPerson(t, s, h, "alice@example.com")
	w, out := alice.do("POST", "/api/v1/tasks/quick", map[string]any{"text": "Pay rent due 1 nov every month !high", "commit": true})
	if w.Code != 201 {
		t.Fatalf("quick: %d %s", w.Code, w.Body.String())
	}
	task := out["task"].(map[string]any)
	if task["due"] != "2026-11-01" && !strings.HasSuffix(task["due"].(string), "-11-01") {
		t.Errorf("due: %v", task["due"])
	}
	id := itoa(int(task["id"].(float64)))
	w, out = alice.do("POST", "/api/v1/tasks/"+id+"/complete", map[string]any{"done": true})
	if w.Code != 200 || out["next"] == nil {
		t.Fatalf("complete: %d %s", w.Code, w.Body.String())
	}
	next := out["next"].(map[string]any)
	if !strings.HasSuffix(next["due"].(string), "-12-01") || next["repeat"] == nil {
		t.Errorf("next occurrence: %v", next)
	}
	if out["task"].(map[string]any)["repeat"] != nil {
		t.Error("the finished copy should carry no rule")
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
