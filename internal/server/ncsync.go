package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-tasks/internal/caldav"
	"github.com/abjelosevic88/pocket-homelab-tasks/internal/ical"
	"github.com/abjelosevic88/pocket-homelab-tasks/internal/store"
)

// Nextcloud sync: a person's Nextcloud Tasks (VTODO calendars) imported
// once or kept in step both ways. Lists map to calendars by href, tasks to
// resources by href with the etag Nextcloud last gave; see docs/design.md.

type ncListStatus struct {
	Name         string `json:"name"`
	Href         string `json:"href"`
	Tasks        int    `json:"tasks"`
	MappedListID int64  `json:"mapped_list_id"`
	ReadOnly     bool   `json:"read_only,omitempty"`
}

type ncState struct {
	Running   bool           `json:"running"`
	LastRun   string         `json:"last_run"`
	LastError string         `json:"last_error"`
	Pulled    int            `json:"pulled"`
	Pushed    int            `json:"pushed"`
	Lists     []ncListStatus `json:"lists"`
	lastAt    time.Time
}

func (s *Server) ncStateFor(uid int64) *ncState {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	st := s.syncState[uid]
	if st == nil {
		st = &ncState{Lists: []ncListStatus{}}
		s.syncState[uid] = st
	}
	return st
}

// ncClient builds the CalDAV client for a person from Core or typed-in settings.
func (s *Server) ncClient(ctx context.Context, uid int64, st store.Settings) (*caldav.Client, error) {
	switch st.NCVia {
	case "core":
		if !s.core.Configured() {
			return nil, errors.New("Pocket Core is not configured on this box")
		}
		cred, err := s.core.ServiceFor(ctx, s.userEmail(ctx, uid), "nextcloud")
		if err != nil {
			var le *core.LinkError
			if errors.As(err, &le) {
				return nil, fmt.Errorf("Nextcloud through Pocket Core: %s", le.Message)
			}
			return nil, fmt.Errorf("Nextcloud through Pocket Core: %w", err)
		}
		user := cred.Fields["USERNAME"]
		pass := firstNonEmpty(cred.Fields["APP_PASSWORD"], cred.Fields["PASSWORD"], cred.Key)
		if cred.URL == "" || user == "" || pass == "" {
			return nil, errors.New("Pocket Core has no Nextcloud login for you (address, username and app password)")
		}
		c := caldav.New(cred.URL, user, pass)
		c.HTTP, c.UA = s.http, "PocketTasks/"+Version
		return c, nil
	default:
		if st.NCURL == "" || st.NCUser == "" || st.NCPassword == "" {
			return nil, errors.New("the Nextcloud address, username and app password are needed")
		}
		c := caldav.New(st.NCURL, st.NCUser, st.NCPassword)
		c.HTTP, c.UA = s.http, "PocketTasks/"+Version
		return c, nil
	}
}

// syncLoop runs every person's sync on their interval, and at once when asked.
func (s *Server) syncLoop(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case uid := <-s.syncNow:
			s.runSync(ctx, uid, true)
		case <-t.C:
			for _, uid := range s.store.UserIDs(ctx) {
				st, err := s.store.GetSettings(ctx, uid)
				if err != nil || st.NCMode == "off" {
					continue
				}
				state := s.ncStateFor(uid)
				if time.Since(state.lastAt) < time.Duration(st.NCIntervalMin)*time.Minute {
					continue
				}
				s.runSync(ctx, uid, false)
			}
		}
	}
}

// syncSoon asks for a sync of a person after a local change (two-way mode only).
func (s *Server) syncSoon(uid int64) {
	st, err := s.store.GetSettings(context.Background(), uid)
	if err != nil || st.NCMode != "two_way" {
		return
	}
	select {
	case s.syncNow <- uid:
	default:
	}
}

// runSync does one round for a person.
func (s *Server) runSync(ctx context.Context, uid int64, forced bool) {
	st, err := s.store.GetSettings(ctx, uid)
	if err != nil || (st.NCMode == "off" && !forced) {
		return
	}
	state := s.ncStateFor(uid)
	s.syncMu.Lock()
	if state.Running {
		s.syncMu.Unlock()
		return
	}
	state.Running = true
	s.syncMu.Unlock()
	defer func() {
		s.syncMu.Lock()
		state.Running = false
		state.lastAt = time.Now()
		state.LastRun = store.Now()
		s.syncMu.Unlock()
	}()
	cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	pulled, pushed, lists, err := s.syncOnce(cctx, uid, st)
	s.syncMu.Lock()
	state.Pulled, state.Pushed, state.Lists, state.LastError = pulled, pushed, lists, ""
	if err != nil {
		state.LastError = err.Error()
		s.log.Warn("nextcloud sync", "user", uid, "err", err)
	} else {
		s.log.Info("nextcloud sync", "user", uid, "pulled", pulled, "pushed", pushed, "mode", st.NCMode)
	}
	s.syncMu.Unlock()
	if err == nil && st.NCMode == "import" {
		// import once: done, keep the mapping, stop
		st.NCMode = "off"
		_ = s.store.SaveNextcloud(ctx, uid, st)
		_ = s.store.SetUserSetting(ctx, uid, "nc.imported_at", store.Now())
		go s.reportModes(context.Background())
	}
	if pulled+pushed > 0 {
		s.touch()
	}
}

func (s *Server) syncOnce(ctx context.Context, uid int64, st store.Settings) (pulled, pushed int, lists []ncListStatus, err error) {
	c, err := s.ncClient(ctx, uid, st)
	if err != nil {
		return 0, 0, nil, err
	}
	home, err := c.Home(ctx)
	if err != nil {
		return 0, 0, nil, err
	}
	cals, err := c.Calendars(ctx, home)
	if err != nil {
		return 0, 0, nil, err
	}
	twoWay := st.NCMode == "two_way"
	remote := map[string]caldav.Calendar{}
	lists = []ncListStatus{}
	for _, cal := range cals {
		if !cal.HasComponent("VTODO") {
			continue
		}
		seg := lastSegment(cal.Href)
		if strings.HasPrefix(seg, "app-generated--") {
			continue // Deck boards: Deck owns them
		}
		remote[cal.Href] = cal
		l, err := s.store.GetListByNCHref(ctx, uid, cal.Href)
		if err != nil {
			// a calendar we have not seen: match a list by name first
			if byName := s.listByName(ctx, uid, cal.Name); byName != nil && byName.NCHref == "" && !byName.IsInbox {
				l = byName
				_ = s.store.SetListNC(ctx, l.ID, cal.Href, "", "")
			} else if strings.EqualFold(cal.Name, "Default") || strings.EqualFold(cal.Name, "Inbox") || strings.EqualFold(cal.Name, "Tasks") || strings.EqualFold(cal.Name, "Reminders") {
				inbox, _ := s.store.Inbox(ctx, uid)
				if inbox.NCHref == "" {
					l = inbox
					_ = s.store.SetListNC(ctx, l.ID, cal.Href, "", "")
				}
			}
			if l == nil {
				l = &store.List{UserID: uid, Name: firstNonEmpty(cal.Name, seg), NCHref: cal.Href}
				if col, cerr := cleanColor(cal.Color); cerr == nil {
					l.Color = col
				}
				if err := s.store.CreateList(ctx, l); err != nil {
					return pulled, pushed, lists, err
				}
			}
			l, _ = s.store.GetList(ctx, uid, l.ID)
		}
		n, err := s.pullCalendar(ctx, c, uid, l, cal)
		pulled += n
		status := ncListStatus{Name: cal.Name, Href: cal.Href, MappedListID: l.ID, ReadOnly: cal.ReadOnly}
		if ts, terr := s.store.ListTasksInList(ctx, uid, l.ID); terr == nil {
			status.Tasks = len(ts)
		}
		lists = append(lists, status)
		if err != nil {
			return pulled, pushed, lists, fmt.Errorf("%s: %w", cal.Name, err)
		}
		if twoWay && !cal.ReadOnly {
			n, err := s.pushList(ctx, c, uid, l)
			pushed += n
			if err != nil {
				return pulled, pushed, lists, fmt.Errorf("%s: %w", cal.Name, err)
			}
		}
	}
	// lists of ours Nextcloud does not have yet
	if twoWay {
		locals, _ := s.store.ListLists(ctx, uid)
		for i := range locals {
			l := &locals[i]
			if l.Archived {
				continue
			}
			if l.NCHref != "" {
				if _, ok := remote[l.NCHref]; !ok {
					// the calendar is gone over there: forget the mapping, keep the tasks
					_ = s.store.SetListNC(ctx, l.ID, "", "", "")
					s.log.Info("nextcloud calendar gone, list kept", "list", l.Name)
				}
				continue
			}
			href := home + url.PathEscape(strings.ToLower(l.UID)) + "/"
			if err := c.MkCalendar(ctx, href, l.Name, l.Color); err != nil {
				return pulled, pushed, lists, fmt.Errorf("creating %s in Nextcloud: %w", l.Name, err)
			}
			_ = s.store.SetListNC(ctx, l.ID, href, "", "")
			l.NCHref = href
			n, err := s.pushList(ctx, c, uid, l)
			pushed += n
			lists = append(lists, ncListStatus{Name: l.Name, Href: href, MappedListID: l.ID, Tasks: n})
			if err != nil {
				return pulled, pushed, lists, err
			}
		}
	}
	return pulled, pushed, lists, nil
}

func lastSegment(href string) string {
	href = strings.TrimSuffix(href, "/")
	if i := strings.LastIndex(href, "/"); i >= 0 {
		return href[i+1:]
	}
	return href
}

// pullCalendar brings what changed in a Nextcloud calendar into its list.
func (s *Server) pullCalendar(ctx context.Context, c *caldav.Client, uid int64, l *store.List, cal caldav.Calendar) (int, error) {
	if cal.CTag != "" && cal.CTag == l.NCCtag && l.NCToken != "" {
		return 0, nil // nothing changed over there
	}
	var changes []caldav.Change
	var token string
	var err error
	if l.NCToken != "" {
		changes, token, err = c.Sync(ctx, cal.Href, l.NCToken)
		if errors.Is(err, caldav.ErrInvalidToken) {
			changes, err = c.ListETags(ctx, cal.Href)
			token = cal.SyncToken
		}
	} else {
		changes, err = c.ListETags(ctx, cal.Href)
		token = cal.SyncToken
	}
	if err != nil {
		return 0, err
	}
	loc := s.loc(ctx, uid)
	var fetch []string
	n := 0
	for _, ch := range changes {
		if !strings.HasSuffix(strings.ToLower(ch.Href), ".ics") {
			continue
		}
		local, lerr := s.store.GetTaskByNCHref(ctx, uid, ch.Href)
		if ch.Deleted {
			if lerr == nil && local.DeletedAt == "" {
				if local.Seq > local.NCSeq {
					// changed here since: keep ours, it goes back as new
					_ = s.store.SetNC(ctx, local.ID, "", "", 0)
					continue
				}
				_ = s.store.SoftDeleteTask(ctx, uid, local.ID)
				s.store.AddActivity(ctx, uid, local.ID, "deleted", "nextcloud")
				n++
			}
			continue
		}
		if lerr == nil && local.NCETag == ch.ETag && ch.ETag != "" {
			continue
		}
		fetch = append(fetch, ch.Href)
	}
	if len(fetch) > 0 {
		res, err := c.Multiget(ctx, cal.Href, fetch)
		if err != nil {
			return n, err
		}
		// parents before children so RELATED-TO resolves
		for pass := 0; pass < 2; pass++ {
			for _, r := range res {
				calc, err := ical.Parse(r.Data)
				if err != nil {
					continue
				}
				it, ok := ical.FromVTODO(calc, loc)
				if !ok || it.UID == "" {
					continue
				}
				if (pass == 0) != (it.ParentUID == "") {
					continue
				}
				if s.applyRemote(ctx, uid, l, r, calc, it) {
					n++
				}
			}
		}
	}
	_ = s.store.SetListNC(ctx, l.ID, cal.Href, token, cal.CTag)
	return n, nil
}

// applyRemote stores one fetched VTODO; reports whether anything changed.
func (s *Server) applyRemote(ctx context.Context, uid int64, l *store.List, r caldav.Resource, calc *ical.Component, it *ical.Task) bool {
	local, err := s.store.GetTaskByNCHref(ctx, uid, r.Href)
	if err != nil {
		// maybe we pushed it under that UID before the mapping was stored
		local, err = s.store.GetTaskByUID(ctx, uid, it.UID)
	}
	ics := calc.String()
	if err != nil {
		t := &store.Task{UserID: uid, ListID: l.ID, Href: it.UID + ".ics", ICS: ics, ETag: ical.ETag(ics), NCHref: r.Href, NCETag: r.ETag, Tags: []string{}, Alarms: []ical.Alarm{}}
		s.fromICal(ctx, it, t)
		if t.Title == "" {
			t.Title = "Untitled"
		}
		if !it.Created.IsZero() {
			t.CreatedAt = store.FormatTime(it.Created)
		}
		if _, dup := s.store.GetTaskByHref(ctx, uid, l.ID, t.Href); dup == nil {
			t.Href = strings.ToUpper(newUUID()) + ".ics"
		}
		if err := s.store.InsertTask(ctx, t); err != nil {
			s.log.Warn("nextcloud: cannot store task", "href", r.Href, "err", err)
			return false
		}
		_ = s.store.SetNC(ctx, t.ID, r.Href, r.ETag, t.Seq)
		s.scheduleAlarms(ctx, t)
		s.store.AddActivity(ctx, uid, t.ID, "synced", "nextcloud")
		return true
	}
	if local.DeletedAt != "" {
		if local.Seq > local.NCSeq {
			return false // deleted here, the delete goes over on the push
		}
		// deleted here earlier but changed there since: bring it back
		_ = s.store.RestoreTask(ctx, uid, local.ID)
		local, _ = s.store.GetTask(ctx, uid, local.ID)
	}
	if local.Seq > local.NCSeq {
		// changed on both sides: the newer edit wins
		remoteMod := it.Modified
		localMod := store.ParseTime(local.UpdatedAt)
		if !remoteMod.After(localMod) {
			return false // ours is newer; the push will carry it
		}
		s.log.Info("nextcloud: remote edit wins", "task", local.ID, "title", it.Title)
	}
	wasDone := local.Status == ical.StatusDone
	local.ICS, local.ETag = ics, ical.ETag(ics)
	if local.ListID != l.ID {
		local.ListID = l.ID
		local.SectionID = nil
	}
	s.fromICal(ctx, it, local)
	if local.Title == "" {
		local.Title = "Untitled"
	}
	if err := s.store.UpdateTask(ctx, local); err != nil {
		s.log.Warn("nextcloud: cannot update task", "href", r.Href, "err", err)
		return false
	}
	_ = s.store.SetNC(ctx, local.ID, r.Href, r.ETag, local.Seq)
	s.scheduleAlarms(ctx, local)
	kind := "synced"
	if !wasDone && local.Status == ical.StatusDone {
		kind = "completed"
	}
	s.store.AddActivity(ctx, uid, local.ID, kind, "nextcloud")
	return true
}

// pushList sends local changes of a list to its Nextcloud calendar.
func (s *Server) pushList(ctx context.Context, c *caldav.Client, uid int64, l *store.List) (int, error) {
	if l.NCHref == "" {
		return 0, nil
	}
	todo, err := s.store.NeedPush(ctx, uid, l.ID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, t := range todo {
		if t.DeletedAt != "" {
			if t.NCHref != "" {
				if err := c.Delete(ctx, t.NCHref, ""); err != nil && !errors.Is(err, caldav.ErrNotFound) {
					return n, err
				}
				n++
			}
			_ = s.store.SetNC(ctx, t.ID, "", "", t.Seq)
			continue
		}
		href := t.NCHref
		create := href == ""
		if create {
			href = l.NCHref + url.PathEscape(t.UID) + ".ics"
		}
		etag, err := c.Put(ctx, href, t.ICS, t.NCETag, create)
		if errors.Is(err, caldav.ErrPrecondition) {
			// changed over there meanwhile: fetch, let the newer one win, retry once
			res, gerr := c.Get(ctx, href)
			if gerr != nil {
				return n, gerr
			}
			loc := s.loc(ctx, uid)
			if calc, perr := ical.Parse(res.Data); perr == nil {
				if it, ok := ical.FromVTODO(calc, loc); ok {
					if it.Modified.After(store.ParseTime(t.UpdatedAt)) {
						s.applyRemote(ctx, uid, l, *res, calc, it)
						continue
					}
				}
			}
			etag, err = c.Put(ctx, href, t.ICS, res.ETag, false)
		}
		if errors.Is(err, caldav.ErrNotFound) && !create {
			// gone over there: put it back as new
			etag, err = c.Put(ctx, href, t.ICS, "", true)
		}
		if err != nil {
			return n, err
		}
		_ = s.store.SetNC(ctx, t.ID, href, etag, t.Seq)
		n++
	}
	return n, nil
}

// ---------- handlers ----------

func (s *Server) ncStatusView(ctx context.Context, uid int64, st store.Settings) map[string]any {
	state := s.ncStateFor(uid)
	s.syncMu.Lock()
	cp := *state
	s.syncMu.Unlock()
	out := map[string]any{
		"mode": st.NCMode, "via": st.NCVia, "url": st.NCURL, "user": st.NCUser, "password_set": st.NCPassword != "",
		"core_available": s.core.Configured(), "interval_min": st.NCIntervalMin, "status": cp,
		"imported_at": s.store.UserSetting(ctx, uid, "nc.imported_at"),
	}
	if s.core.Configured() {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if _, err := s.core.ServiceFor(cctx, s.userEmail(ctx, uid), "nextcloud"); err != nil {
			var le *core.LinkError
			if errors.As(err, &le) {
				out["core_error"] = le.Message
			} else {
				out["core_error"] = err.Error()
			}
		}
	}
	return out
}

func (s *Server) handleNCStatus(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	st, _ := s.store.GetSettings(ctx, uid)
	writeJSON(w, http.StatusOK, s.ncStatusView(ctx, uid, st))
}

func (s *Server) handleNCSave(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	st, _ := s.store.GetSettings(ctx, uid)
	var in struct {
		Mode     *string `json:"mode"`
		Via      *string `json:"via"`
		URL      *string `json:"url"`
		User     *string `json:"user"`
		Password *string `json:"password"`
		Interval *int    `json:"interval_min"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Mode != nil {
		switch *in.Mode {
		case "off", "import", "two_way":
			st.NCMode = *in.Mode
		default:
			writeErr(w, http.StatusBadRequest, "mode must be off, import or two_way")
			return
		}
	}
	if in.Via != nil {
		switch *in.Via {
		case "core", "direct":
			st.NCVia = *in.Via
		default:
			writeErr(w, http.StatusBadRequest, "via must be core or direct")
			return
		}
	}
	if in.URL != nil {
		u := strings.TrimRight(strings.TrimSpace(*in.URL), "/")
		if u != "" {
			if p, err := url.Parse(u); err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
				writeErr(w, http.StatusBadRequest, "the address must start with http:// or https://")
				return
			}
		}
		st.NCURL = u
	}
	if in.User != nil {
		st.NCUser = strings.TrimSpace(*in.User)
	}
	if in.Password != nil && *in.Password != "" {
		st.NCPassword = *in.Password
	}
	if in.Interval != nil && *in.Interval >= 1 && *in.Interval <= 1440 {
		st.NCIntervalMin = *in.Interval
	}
	if st.NCVia == "core" && !s.core.Configured() && st.NCMode != "off" {
		writeErr(w, http.StatusBadRequest, "Pocket Core is not configured on this box; use Direct")
		return
	}
	if err := s.store.SaveNextcloud(ctx, uid, st); err != nil {
		s.fail(w, err, "save sync")
		return
	}
	go s.reportModes(context.Background())
	if st.NCMode != "off" {
		select {
		case s.syncNow <- uid:
		default:
		}
	}
	writeJSON(w, http.StatusOK, s.ncStatusView(ctx, uid, st))
}

func (s *Server) handleNCTest(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	st, _ := s.store.GetSettings(ctx, uid)
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	c, err := s.ncClient(cctx, uid, st)
	if err != nil {
		writeJSON(w, http.StatusPreconditionFailed, map[string]any{"error": err.Error()})
		return
	}
	home, err := c.Home(cctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	cals, err := c.Calendars(cctx, home)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	out := []map[string]any{}
	for _, cal := range cals {
		if !cal.HasComponent("VTODO") {
			continue
		}
		seg := lastSegment(cal.Href)
		if strings.HasPrefix(seg, "app-generated--") {
			continue
		}
		n := 0
		if et, err := c.ListETags(cctx, cal.Href); err == nil {
			n = len(et)
		}
		mapped := int64(0)
		if l, err := s.store.GetListByNCHref(ctx, uid, cal.Href); err == nil {
			mapped = l.ID
		}
		out = append(out, map[string]any{"name": cal.Name, "href": cal.Href, "tasks": n, "read_only": cal.ReadOnly, "mapped_list_id": mapped})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "home": home, "calendars": out, "via": st.NCVia})
}

func (s *Server) handleNCRun(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	st, _ := s.store.GetSettings(ctx, uid)
	if st.NCMode == "off" {
		writeErr(w, http.StatusBadRequest, "turn the sync on first (Import once or Two-way)")
		return
	}
	s.runSync(ctx, uid, true)
	st, _ = s.store.GetSettings(ctx, uid)
	writeJSON(w, http.StatusOK, s.ncStatusView(ctx, uid, st))
}
