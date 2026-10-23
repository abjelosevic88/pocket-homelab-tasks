package server

import "testing"

// Without CORE_URL the Nextcloud settings say there is no Core, so the UI can
// grey out "Pocket Core" instead of offering it.
func TestNCStatusWithoutCore(t *testing.T) {
	s, h := testServer(t)
	c := newPerson(t, s, h, "ana@example.com")
	w, out := c.do("GET", "/api/v1/sync/nextcloud", nil)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if out["core_available"] != false || out["core_registered"] != false {
		t.Fatalf("core flags = %v / %v, want false / false", out["core_available"], out["core_registered"])
	}
}
