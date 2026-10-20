package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-tasks/internal/store"
)

// Pocket Core app links (docs/links.md in pocket-homelab-core): another
// Pocket app may act here for a person with a pcx_ token, only within these
// scopes. Every route not listed in linkRoutes stays closed to it.

var linkOffers = []core.Offer{
	{Scope: "tasks:read", Name: "Read your tasks", Description: "List and search your tasks, lists, tags and what is due (what Pocket Day shows in the journal)."},
	{Scope: "tasks:write", Name: "Add and tick tasks", Description: "Create tasks, change them and mark them done (Pocket Notes' \"make this line a task\")."},
}

// serviceUses are the third-party services reached through Core for a person.
var serviceUses = []core.ServiceUse{
	{Type: "nextcloud", Reason: "Import your Nextcloud Tasks and keep them in sync both ways"},
}

type linkRoute struct {
	methods string
	path    *regexp.Regexp
	scope   string
}

var linkRoutes = []linkRoute{
	{"GET", regexp.MustCompile(`^/api/v1/auth/me$`), ""},
	{"GET", regexp.MustCompile(`^/api/v1/(tasks|lists|tags|calendar|dashboard|search|areas|filters)$`), "tasks:read"},
	{"GET", regexp.MustCompile(`^/api/v1/tasks/\d+(/ics)?$`), "tasks:read"},
	{"GET", regexp.MustCompile(`^/api/v1/lists/\d+/sections$`), "tasks:read"},
	{"POST", regexp.MustCompile(`^/api/v1/(tasks|tasks/quick)$`), "tasks:write"},
	{"POST", regexp.MustCompile(`^/api/v1/tasks/\d+/complete$`), "tasks:write"},
	{"PUT", regexp.MustCompile(`^/api/v1/tasks/\d+$`), "tasks:write"},
}

var errLinkScope = errors.New("outside the link's scope")

func linkAllows(scopes []string, r *http.Request) bool {
	for _, lr := range linkRoutes {
		if !strings.Contains(" "+lr.methods+" ", " "+r.Method+" ") || !lr.path.MatchString(r.URL.Path) {
			continue
		}
		if lr.scope == "" {
			return true
		}
		for _, sc := range scopes {
			if sc == lr.scope {
				return true
			}
		}
	}
	return false
}

// linkUser validates a pcx_ token and maps it to the person it acts for.
func (s *Server) linkUser(ctx context.Context, tok string, r *http.Request) (*store.User, error) {
	in, err := s.core.Introspect(ctx, tok)
	if err != nil {
		return nil, err
	}
	if !in.Active || !in.Delegated() || in.User == nil || in.User.Email == "" {
		return nil, http.ErrNoCookie
	}
	if !linkAllows(strings.Fields(in.Scope), r) {
		return nil, errLinkScope
	}
	return s.coreIdentityUser(ctx, in)
}

// reportModes tells Core how each person reaches Nextcloud (Core, Direct or off).
func (s *Server) reportModes(ctx context.Context) {
	if !s.core.Configured() {
		return
	}
	var modes []core.ConnMode
	for _, uid := range s.store.UserIDs(ctx) {
		email := s.userEmail(ctx, uid)
		if email == "" {
			continue
		}
		st, err := s.store.GetSettings(ctx, uid)
		if err != nil {
			continue
		}
		mode := "off"
		if st.NCMode != "off" {
			mode = st.NCVia
		}
		modes = append(modes, core.ConnMode{User: email, Target: "service:nextcloud", Mode: mode})
	}
	if err := s.core.ReportModes(ctx, modes); err != nil {
		s.log.Debug("pocket core: report modes", "err", err)
	}
}

func (s *Server) userEmail(ctx context.Context, uid int64) string {
	if u, err := s.store.GetUser(ctx, uid); err == nil {
		return u.Email
	}
	return ""
}
