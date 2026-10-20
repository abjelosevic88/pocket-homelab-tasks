package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-tasks/internal/ical"
	"github.com/abjelosevic88/pocket-homelab-tasks/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-tasks/internal/store"
)

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetSettings(r.Context(), me(r))
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r, st))
}

func (s *Server) publicSettings(r *http.Request, st store.Settings) map[string]any {
	uid := me(r)
	b, _ := json.Marshal(st)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if st.Timezone == "" {
		m["timezone"] = s.cfg.Timezone
	}
	m["notify_token_set"] = st.NotifyToken != ""
	m["notify_env"] = st.NotifyURL == "" && s.cfg.NotifyURL != "" && s.isOwner(r.Context(), uid)
	m["core"] = s.core.Configured()
	if st.DefaultListID == 0 {
		if inbox, err := s.store.Inbox(r.Context(), uid); err == nil {
			m["default_list_id"] = inbox.ID
		}
	}
	return m
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	u := userFrom(ctx)
	cur, err := s.store.GetSettings(ctx, uid)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	var raw map[string]json.RawMessage
	if err := decode(r, &raw); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st := cur
	b, _ := json.Marshal(cur)
	var merged map[string]json.RawMessage
	_ = json.Unmarshal(b, &merged)
	for k, v := range raw {
		merged[k] = v
	}
	mb, _ := json.Marshal(merged)
	if err := json.Unmarshal(mb, &st); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid settings")
		return
	}
	if v, ok := raw["notify_token"]; ok {
		var tok string
		_ = json.Unmarshal(v, &tok)
		st.NotifyToken = tok
	}
	if st.WeekStart != 0 {
		st.WeekStart = 1
	}
	if st.DefaultEstimateMin <= 0 || st.DefaultEstimateMin > 24*60 {
		st.DefaultEstimateMin = 30
	}
	if _, err := time.Parse("15:04", st.DayStart); err != nil {
		writeErr(w, http.StatusBadRequest, "day_start must be HH:MM")
		return
	}
	if st.DigestTime != "" {
		if _, err := time.Parse("15:04", st.DigestTime); err != nil {
			writeErr(w, http.StatusBadRequest, "digest_time must be HH:MM or empty")
			return
		}
	}
	if st.DefaultAlarm != "" {
		if _, err := ical.Duration(st.DefaultAlarm); err != nil {
			writeErr(w, http.StatusBadRequest, "default_alarm must be a duration like -PT15M or PT0S")
			return
		}
	}
	if st.Timezone != "" {
		if _, err := time.LoadLocation(st.Timezone); err != nil {
			writeErr(w, http.StatusBadRequest, "unknown time zone")
			return
		}
	}
	if st.LogbookGroup != "list" {
		st.LogbookGroup = "day"
	}
	if st.TrashDays < 1 || st.TrashDays > 3650 {
		st.TrashDays = 30
	}
	switch st.NotifyKind {
	case "apprise", "ntfy", "webhook":
	default:
		st.NotifyKind = "apprise"
	}
	if st.DefaultListID > 0 {
		if _, err := s.store.GetList(ctx, uid, st.DefaultListID); err != nil {
			writeErr(w, http.StatusBadRequest, "no such list")
			return
		}
	}
	if err := s.store.SaveSettings(ctx, uid, st); err != nil {
		s.fail(w, err, "save settings")
		return
	}
	if _, ok := raw["title"]; ok && u.Role == "admin" {
		box, _ := s.store.GetSettings(ctx, 0)
		box.Title = strings.TrimSpace(st.Title)
		if box.Title == "" {
			box.Title = store.DefaultTitle
		}
		_ = s.store.SaveBoxSettings(ctx, box)
	}
	if _, ok := raw["timezone"]; ok {
		go s.rescheduleAll(context.Background(), uid)
	}
	st, _ = s.store.GetSettings(ctx, uid)
	s.touch()
	writeJSON(w, http.StatusOK, s.publicSettings(r, st))
}

// notifyRoute is where a person's reminders go: their own route, or the
// box's for the owner.
func (s *Server) notifyRoute(ctx context.Context, uid int64, st store.Settings) (kind, url, token string) {
	if st.NotifyURL != "" {
		return st.NotifyKind, st.NotifyURL, st.NotifyToken
	}
	if s.isOwner(ctx, uid) && s.cfg.NotifyURL != "" {
		return s.cfg.NotifyKind, s.cfg.NotifyURL, ""
	}
	return "", "", ""
}

func (s *Server) handleTestNotify(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	st, _ := s.store.GetSettings(ctx, uid)
	kind, url, token := s.notifyRoute(ctx, uid, st)
	if url == "" {
		writeErr(w, http.StatusBadRequest, "no notification address is set")
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := notify.Send(cctx, kind, url, token, "Pocket Tasks", "Reminders will arrive here.", s.cfg.BaseURL); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	u := userFrom(ctx)
	var size int64
	if fi, err := os.Stat(s.cfg.DBPath); err == nil {
		size = fi.Size()
	}
	out := map[string]any{
		"version": Version, "go": runtime.Version(), "db_size": size, "data_dir": s.cfg.DataDir, "base_url": s.cfg.BaseURL,
		"counts": s.store.UserCounts(ctx, uid), "core": s.coreStatus(), "timezone": s.cfg.Timezone,
		"caldav":   map[string]string{"url": s.cfg.BaseURL + "/dav/", "username": u.Username},
		"feed_url": s.cfg.BaseURL + "/feed/" + u.FeedToken + ".ics",
	}
	if u.Role == "admin" {
		out["box_counts"] = s.store.Counts(ctx)
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- feeds / CalDAV info ----

func (s *Server) handleFeeds(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	base := s.cfg.BaseURL
	if base == "" {
		base = "http://" + r.Host
	}
	webcal := strings.Replace(strings.Replace(base, "https://", "webcal://", 1), "http://", "webcal://", 1)
	writeJSON(w, http.StatusOK, map[string]any{
		"ics_url":              base + "/feed/" + u.FeedToken + ".ics",
		"webcal_url":           webcal + "/feed/" + u.FeedToken + ".ics",
		"caldav_url":           base + "/dav/",
		"caldav_home":          base + "/dav/calendars/" + u.Username + "/",
		"caldav_nextcloud_url": base + "/remote.php/dav/calendars/" + u.Username + "/",
		"username":             u.Username,
		"mirror_url":           base + "/dav/calendars/" + u.Username + "/" + mirrorUID + "/",
	})
}

func (s *Server) handleRotateFeed(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	u.FeedToken = store.RandomToken(16)
	if err := s.store.UpdateUser(ctx, u); err != nil {
		s.fail(w, err, "rotate feed")
		return
	}
	s.handleFeeds(w, r)
}

// ---- backup, export, import ----

func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	dst := fmt.Sprintf("%s/backups/pocket-tasks-%s.db", s.cfg.DataDir, time.Now().Format("20060102-150405"))
	if err := s.store.Backup(r.Context(), dst); err != nil {
		s.fail(w, err, "backup")
		return
	}
	defer os.Remove(dst)
	w.Header().Set("Content-Type", "application/x-sqlite3")
	w.Header().Set("Content-Disposition", "attachment; filename=\"pocket-tasks.db\"")
	http.ServeFile(w, r, dst)
}

type exportFile struct {
	App      string          `json:"app"`
	Version  string          `json:"version"`
	Exported string          `json:"exported_at"`
	Areas    []store.Area    `json:"areas"`
	Lists    []store.List    `json:"lists"`
	Sections []store.Section `json:"sections"`
	Tasks    []*store.Task   `json:"tasks"`
	Filters  []store.Filter  `json:"filters"`
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	areas, _ := s.store.ListAreas(ctx, uid)
	lists, _ := s.store.ListLists(ctx, uid)
	secs, _ := s.store.ListSections(ctx, uid, 0)
	tasks, err := s.store.LiveTasks(ctx, uid)
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	filters, _ := s.store.ListFilters(ctx, uid)
	w.Header().Set("Content-Disposition", "attachment; filename=\"pocket-tasks-export.json\"")
	writeJSON(w, http.StatusOK, exportFile{App: "pocket-tasks", Version: Version, Exported: store.Now(), Areas: areas, Lists: lists, Sections: secs, Tasks: tasks, Filters: filters})
}

func (s *Server) handleExportICS(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	listID := queryInt64(r, "list_id")
	var tasks []*store.Task
	var err error
	name := "Pocket Tasks"
	if listID > 0 {
		l, lerr := s.store.GetList(ctx, uid, listID)
		if lerr != nil {
			s.fail(w, lerr, "export")
			return
		}
		name = l.Name
		tasks, err = s.store.ListTasksInList(ctx, uid, listID)
	} else {
		tasks, err = s.store.LiveTasks(ctx, uid)
	}
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	cal := ical.NewCalendar()
	cal.SetText("X-WR-CALNAME", name)
	for _, t := range tasks {
		if c, err := ical.Parse(t.ICS); err == nil {
			for _, ch := range c.Children {
				if ch.Name == "VTODO" {
					cal.Children = append(cal.Children, ch)
				}
			}
		}
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", store.Slug(name)+".ics"))
	_, _ = w.Write([]byte(cal.String()))
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	r.Body = http.MaxBytesReader(w, r.Body, 200<<20)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "could not read the upload")
		return
	}
	fh := firstFile(r, "file", "files")
	if fh == nil {
		writeErr(w, http.StatusBadRequest, "no file")
		return
	}
	f, err := fh.Open()
	if err != nil {
		writeErr(w, http.StatusBadRequest, "could not read the file")
		return
	}
	defer f.Close()
	data, _ := io.ReadAll(f)
	listID := int64(0)
	fmt.Sscanf(r.FormValue("list_id"), "%d", &listID)
	text := string(data)
	if strings.HasPrefix(strings.TrimSpace(text), "{") {
		n, err := s.importJSON(ctx, uid, data)
		if err != nil {
			s.fail(w, err, "import")
			return
		}
		s.touch()
		writeJSON(w, http.StatusOK, map[string]any{"imported": n, "tasks": n})
		return
	}
	n, l, err := s.importICS(ctx, uid, text, listID, strings.TrimSuffix(fh.Filename, ".ics"))
	if err != nil {
		s.fail(w, err, "import")
		return
	}
	s.touch()
	writeJSON(w, http.StatusOK, map[string]any{"imported": n, "tasks": n, "list": l, "lists": 1})
}

// importICS adds the VTODOs of a calendar to a list (a new one named after
// the calendar when listID is 0). Tasks whose UID is already there are skipped.
func (s *Server) importICS(ctx context.Context, uid int64, text string, listID int64, fallbackName string) (int, *store.List, error) {
	cal, err := ical.Parse(text)
	if err != nil {
		return 0, nil, badRequest("not an iCalendar file")
	}
	var l *store.List
	if listID > 0 {
		l, err = s.store.GetList(ctx, uid, listID)
		if err != nil {
			return 0, nil, badRequest("no such list")
		}
	} else {
		name := strings.TrimSpace(cal.Text("X-WR-CALNAME"))
		if name == "" {
			name = fallbackName
		}
		if name == "" {
			name = "Imported"
		}
		l = &store.List{UserID: uid, Name: name}
		if c, err := cleanColor(cal.Value("X-APPLE-CALENDAR-COLOR")); err == nil {
			l.Color = c
		}
		if err := s.store.CreateList(ctx, l); err != nil {
			return 0, nil, err
		}
	}
	loc := s.loc(ctx, uid)
	n := 0
	// parents first so RELATED-TO resolves
	todos := cal.ChildrenNamed("VTODO")
	for pass := 0; pass < 2; pass++ {
		for _, todo := range todos {
			it, ok := ical.FromVTODO(todo, loc)
			if !ok || it.UID == "" {
				continue
			}
			if (pass == 0) != (it.ParentUID == "") {
				continue
			}
			if _, err := s.store.GetTaskByUID(ctx, uid, it.UID); err == nil {
				continue
			}
			one := ical.NewCalendar()
			one.Children = append(one.Children, todo)
			t := &store.Task{UserID: uid, ListID: l.ID, ICS: one.String()}
			s.fromICal(ctx, it, t)
			t.ETag = ical.ETag(t.ICS)
			t.Href = it.UID + ".ics"
			if err := s.store.InsertTask(ctx, t); err != nil {
				continue
			}
			s.scheduleAlarms(ctx, t)
			n++
		}
	}
	return n, l, nil
}

func (s *Server) importJSON(ctx context.Context, uid int64, data []byte) (int, error) {
	var in exportFile
	if err := json.Unmarshal(data, &in); err != nil || in.App != "pocket-tasks" {
		return 0, badRequest("not a Pocket Tasks export")
	}
	areaMap := map[int64]int64{}
	for _, a := range in.Areas {
		na := &store.Area{UserID: uid, Name: a.Name, Color: a.Color}
		if s.store.CreateArea(ctx, na) == nil {
			areaMap[a.ID] = na.ID
		}
	}
	listMap := map[int64]int64{}
	inbox, _ := s.store.Inbox(ctx, uid)
	for _, l := range in.Lists {
		if l.IsInbox {
			listMap[l.ID] = inbox.ID
			continue
		}
		nl := &store.List{UserID: uid, Name: l.Name, Color: l.Color, Description: l.Description, Archived: l.Archived}
		if l.AreaID != nil {
			if id, ok := areaMap[*l.AreaID]; ok {
				nl.AreaID = &id
			}
		}
		if s.store.CreateList(ctx, nl) == nil {
			listMap[l.ID] = nl.ID
		}
	}
	secMap := map[int64]int64{}
	for _, x := range in.Sections {
		lid, ok := listMap[x.ListID]
		if !ok {
			continue
		}
		ns := &store.Section{UserID: uid, ListID: lid, Name: x.Name}
		if s.store.CreateSection(ctx, ns) == nil {
			secMap[x.ID] = ns.ID
		}
	}
	n := 0
	taskMap := map[int64]int64{}
	for pass := 0; pass < 2; pass++ {
		for _, t := range in.Tasks {
			if (pass == 0) != (t.ParentID == nil) {
				continue
			}
			lid, ok := listMap[t.ListID]
			if !ok {
				lid = inbox.ID
			}
			nt := *t
			nt.ID, nt.UserID, nt.ListID, nt.ICS, nt.ETag, nt.Href, nt.Seq, nt.NCHref, nt.NCETag, nt.NCSeq = 0, uid, lid, "", "", "", 0, "", "", 0
			nt.SectionID, nt.ParentID, nt.Attachments, nt.Subtasks = nil, nil, nil, nil
			if t.SectionID != nil {
				if id, ok := secMap[*t.SectionID]; ok {
					nt.SectionID = &id
				}
			}
			if t.ParentID != nil {
				if id, ok := taskMap[*t.ParentID]; ok {
					nt.ParentID = &id
				}
			}
			if _, err := s.store.GetTaskByUID(ctx, uid, nt.UID); err == nil {
				nt.UID = ""
			}
			if err := s.saveTask(ctx, &nt, "import", "created"); err == nil {
				taskMap[t.ID] = nt.ID
				n++
			}
		}
	}
	for _, f := range in.Filters {
		nf := &store.Filter{UserID: uid, Name: f.Name, Icon: f.Icon, Query: f.Query}
		if id, ok := listMap[nf.Query.ListID]; ok {
			nf.Query.ListID = id
		}
		_ = s.store.SaveFilter(ctx, nf)
	}
	return n, nil
}

func firstFile(r *http.Request, names ...string) *multipartFile {
	for _, n := range names {
		if fs := r.MultipartForm.File[n]; len(fs) > 0 {
			return fs[0]
		}
	}
	return nil
}

type multipartFile = multipart.FileHeader
