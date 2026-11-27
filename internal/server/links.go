package server

import (
	"context"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-core/sdk/appkit"
)

// Pocket Core app links (docs/links.md in pocket-homelab-core): another
// Pocket app may act here for a person with a pcx_ token, only within these
// scopes. Every route not listed in linkRoutes stays closed to it.

var linkOffers = []core.Offer{
	{Scope: "tasks:read", Name: "Read your tasks", Description: "List and search your tasks, lists, tags and what is due (what Pocket Day shows in the journal)."},
	{Scope: "tasks:write", Name: "Add and tick tasks", Description: "Create tasks, change them and mark them done (Pocket Notes' \"make this line a task\")."},
}

// portable is what Pocket Move may copy out and bring in for a person
// (docs/move.md in pocket-homelab-core): the routes run as the person the link
// acts for, on their own lists only.
var portable = &core.Portable{
	Exports: []core.PortableExport{
		{Path: "/api/v1/export.json", File: "tasks.json", Kind: core.KindPocket, Title: "Every list, section, task and saved filter (Pocket Tasks' own file)"},
		{Path: "/api/v1/export.ics", File: "tasks.ics", Kind: core.KindICalTodos, Title: "Every task as an iCalendar to-do, each with its list's name"},
	},
	Imports: []core.PortableImport{
		{Path: "/api/v1/import", Kind: core.KindPocket, Form: "file", MaxBytes: importMaxBytes, Title: "Adds the lists and tasks from another box; tasks already here are left alone"},
		{Path: "/api/v1/import", Kind: core.KindICalTodos, Form: "file", MaxBytes: importMaxBytes, Title: "Adds the to-dos into the list the file names, or \"Moved in\"; tasks already here are left alone"},
	},
}

// capabilities are what Pocket Search, Pocket Day and Pocket Hub may ask of
// Tasks without knowing anything about it (docs/capabilities.md in
// pocket-homelab-core): one fixed route each, in capabilities.go, run as the
// person the caller acts for.
var capabilities = core.Capabilities{
	Search:  &core.Searchable{What: "Your tasks, by title, notes and tags", Kinds: []string{"task"}, Bangs: []string{"tasks"}},
	Day:     &core.DayFeed{Label: "Tasks", One: "task", Many: "tasks", Unit: "count", What: "The tasks you finished or added that day, and the ones due that day that are still open"},
	Widget:  &core.Widget{Title: "Tasks today", What: "What is due today, what is late and what you finished today"},
	Capture: &core.QuickCapture{Kinds: []core.CaptureKind{{Kind: "task", Label: "Task", What: "A new task in the list your new tasks go to (your Inbox unless you chose another)", Fields: []string{"title", "due"}}}},
}

// serviceUses are the third-party services reached through Core for a person.
var serviceUses = []core.ServiceUse{
	{Type: "nextcloud", Reason: "Import your Nextcloud Tasks and keep them in sync both ways"},
}

// linkRoutes are the requests a linked app may make with its scopes; every
// other route stays closed to it (appkit.Routes, with the Portable and
// Capabilities routes added by linkAllows).
var linkRoutes = appkit.Routes{
	appkit.R("GET", `^/api/v1/auth/me$`, ""),
	appkit.R("GET", `^/api/v1/(tasks|lists|tags|calendar|dashboard|search|areas|filters)$`, "tasks:read"),
	appkit.R("GET", `^/api/v1/tasks/\d+(/ics)?$`, "tasks:read"),
	appkit.R("GET", `^/api/v1/lists/\d+/sections$`, "tasks:read"),
	appkit.R("POST", `^/api/v1/(tasks|tasks/quick)$`, "tasks:write"),
	appkit.R("POST", `^/api/v1/tasks/\d+/complete$`, "tasks:write"),
	appkit.R("PUT", `^/api/v1/tasks/\d+$`, "tasks:write"),
}

// linkAllows reports whether a link token with these scopes may make a
// request: linkRoutes plus the Portable and Capabilities routes.
var linkAllows = linkRoutes.Allow(portable, capabilities)

// connModes are how each person reaches Nextcloud (Core, Direct or off), for
// Core's map; appkit.ModeReporter (s.modes) sends them.
func (s *Server) connModes(ctx context.Context) ([]core.ConnMode, error) {
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
	return modes, nil
}

func (s *Server) userEmail(ctx context.Context, uid int64) string {
	if u, err := s.store.GetUser(ctx, uid); err == nil {
		return u.Email
	}
	return ""
}
