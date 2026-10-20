package server

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-tasks/internal/store"
)

var reColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func cleanColor(c string) (string, error) {
	c = strings.TrimSpace(c)
	if c == "" {
		return "", nil
	}
	if len(c) == 9 && strings.HasPrefix(c, "#") { // #rrggbbaa from CalDAV clients
		c = c[:7]
	}
	if !reColor.MatchString(c) {
		return "", badRequest("colour must be #rrggbb")
	}
	return strings.ToLower(c), nil
}

// ---- lists ----

func (s *Server) handleListLists(w http.ResponseWriter, r *http.Request) {
	lists, err := s.store.ListLists(r.Context(), me(r))
	if err != nil {
		s.fail(w, err, "list lists")
		return
	}
	writeJSON(w, http.StatusOK, lists)
}

type listInput struct {
	Name        *string `json:"name"`
	Color       *string `json:"color"`
	Description *string `json:"description"`
	AreaID      *int64  `json:"area_id"`
	Archived    *bool   `json:"archived"`
}

func (s *Server) handleCreateList(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	var in listInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	l := &store.List{UserID: uid}
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		writeErr(w, http.StatusBadRequest, "a list needs a name")
		return
	}
	l.Name = strings.TrimSpace(*in.Name)
	if in.Color != nil {
		c, err := cleanColor(*in.Color)
		if err != nil {
			s.fail(w, err, "create list")
			return
		}
		l.Color = c
	}
	if in.Description != nil {
		l.Description = strings.TrimSpace(*in.Description)
	}
	if in.AreaID != nil && *in.AreaID > 0 {
		if _, err := s.store.GetArea(ctx, uid, *in.AreaID); err != nil {
			writeErr(w, http.StatusBadRequest, "no such area")
			return
		}
		l.AreaID = in.AreaID
	}
	if err := s.store.CreateList(ctx, l); err != nil {
		s.fail(w, err, "create list")
		return
	}
	s.touch()
	go s.syncSoon(uid)
	writeJSON(w, http.StatusCreated, l)
}

func (s *Server) handleUpdateList(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	l, err := s.store.GetList(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get list")
		return
	}
	var raw map[string]any
	if err := decode(r, &raw); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if v, ok := raw["name"].(string); ok && strings.TrimSpace(v) != "" {
		l.Name = strings.TrimSpace(v)
	}
	if v, ok := raw["color"].(string); ok {
		c, err := cleanColor(v)
		if err != nil {
			s.fail(w, err, "update list")
			return
		}
		l.Color = c
	}
	if v, ok := raw["description"].(string); ok {
		l.Description = strings.TrimSpace(v)
	}
	if v, ok := raw["area_id"]; ok {
		switch a := v.(type) {
		case nil:
			l.AreaID = nil
		case float64:
			id := int64(a)
			if id == 0 {
				l.AreaID = nil
			} else {
				if _, err := s.store.GetArea(ctx, uid, id); err != nil {
					writeErr(w, http.StatusBadRequest, "no such area")
					return
				}
				l.AreaID = &id
			}
		}
	}
	if v, ok := raw["archived"].(bool); ok && !l.IsInbox {
		l.Archived = v
	}
	if err := s.store.UpdateList(ctx, l); err != nil {
		s.fail(w, err, "update list")
		return
	}
	s.touch()
	go s.syncSoon(uid)
	l, _ = s.store.GetList(ctx, uid, l.ID)
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) handleDeleteList(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	id := idParam(r, "id")
	moveTo := queryInt64(r, "move_to")
	if moveTo > 0 {
		if _, err := s.store.GetList(ctx, uid, moveTo); err != nil {
			writeErr(w, http.StatusBadRequest, "no such list to move the tasks to")
			return
		}
	}
	if moveTo == 0 {
		// the tasks go to the trash, not away for good
		ts, _ := s.store.ListTasksInList(ctx, uid, id)
		inbox, _ := s.store.Inbox(ctx, uid)
		for _, t := range ts {
			if t.ParentID == nil {
				_ = s.store.SoftDeleteTask(ctx, uid, t.ID)
			}
		}
		moveTo = inbox.ID
		if moveTo == id {
			writeErr(w, http.StatusBadRequest, "the inbox cannot be deleted")
			return
		}
	}
	if err := s.store.DeleteList(ctx, uid, id, moveTo); err != nil {
		if err == store.ErrConflict {
			writeErr(w, http.StatusBadRequest, "the inbox cannot be deleted")
			return
		}
		s.fail(w, err, "delete list")
		return
	}
	s.touch()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleReorderLists(w http.ResponseWriter, r *http.Request) {
	s.reorder(w, r, "lists")
}

func (s *Server) reorder(w http.ResponseWriter, r *http.Request, table string) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.store.Reorder(r.Context(), table, me(r), in.IDs); err != nil {
		s.fail(w, err, "reorder")
		return
	}
	s.touch()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- areas ----

func (s *Server) handleListAreas(w http.ResponseWriter, r *http.Request) {
	areas, err := s.store.ListAreas(r.Context(), me(r))
	if err != nil {
		s.fail(w, err, "list areas")
		return
	}
	writeJSON(w, http.StatusOK, areas)
}

func (s *Server) handleCreateArea(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Color string }
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Name) == "" {
		writeErr(w, http.StatusBadRequest, "an area needs a name")
		return
	}
	c, err := cleanColor(in.Color)
	if err != nil {
		s.fail(w, err, "create area")
		return
	}
	a := &store.Area{UserID: me(r), Name: strings.TrimSpace(in.Name), Color: c}
	if err := s.store.CreateArea(r.Context(), a); err != nil {
		s.fail(w, err, "create area")
		return
	}
	s.touch()
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) handleUpdateArea(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	a, err := s.store.GetArea(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get area")
		return
	}
	var in struct {
		Name  *string `json:"name"`
		Color *string `json:"color"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
		a.Name = strings.TrimSpace(*in.Name)
	}
	if in.Color != nil {
		c, err := cleanColor(*in.Color)
		if err != nil {
			s.fail(w, err, "update area")
			return
		}
		a.Color = c
	}
	if err := s.store.UpdateArea(ctx, a); err != nil {
		s.fail(w, err, "update area")
		return
	}
	s.touch()
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleDeleteArea(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteArea(r.Context(), me(r), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete area")
		return
	}
	s.touch()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleReorderAreas(w http.ResponseWriter, r *http.Request) { s.reorder(w, r, "areas") }

// ---- sections ----

func (s *Server) handleListSections(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	if _, err := s.store.GetList(ctx, uid, idParam(r, "id")); err != nil {
		s.fail(w, err, "get list")
		return
	}
	secs, err := s.store.ListSections(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "list sections")
		return
	}
	writeJSON(w, http.StatusOK, secs)
}

func (s *Server) handleCreateSection(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	l, err := s.store.GetList(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get list")
		return
	}
	var in struct{ Name string }
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Name) == "" {
		writeErr(w, http.StatusBadRequest, "a section needs a name")
		return
	}
	x := &store.Section{UserID: uid, ListID: l.ID, Name: strings.TrimSpace(in.Name)}
	if err := s.store.CreateSection(ctx, x); err != nil {
		s.fail(w, err, "create section")
		return
	}
	s.touch()
	writeJSON(w, http.StatusCreated, x)
}

func (s *Server) handleUpdateSection(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	x, err := s.store.GetSection(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get section")
		return
	}
	var in struct{ Name string }
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Name) == "" {
		writeErr(w, http.StatusBadRequest, "a section needs a name")
		return
	}
	x.Name = strings.TrimSpace(in.Name)
	if err := s.store.UpdateSection(ctx, x); err != nil {
		s.fail(w, err, "update section")
		return
	}
	// the name rides in every task's X-POCKET-SECTION
	sid := x.ID
	ts, _, _ := s.store.ListTasks(ctx, uid, store.Query{ListID: x.ListID, SectionID: &sid, Status: "all", Limit: 5000}, 0)
	for _, t := range ts {
		_ = s.saveTask(ctx, t, "section", "updated")
	}
	s.touch()
	writeJSON(w, http.StatusOK, x)
}

func (s *Server) handleDeleteSection(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	x, err := s.store.GetSection(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get section")
		return
	}
	sid := x.ID
	ts, _, _ := s.store.ListTasks(ctx, uid, store.Query{ListID: x.ListID, SectionID: &sid, Status: "all", Limit: 5000}, 0)
	if err := s.store.DeleteSection(ctx, uid, x.ID); err != nil {
		s.fail(w, err, "delete section")
		return
	}
	for _, t := range ts {
		t.SectionID = nil
		_ = s.saveTask(ctx, t, "section", "updated")
	}
	s.touch()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleReorderSections(w http.ResponseWriter, r *http.Request) {
	s.reorder(w, r, "sections")
}

// ---- filters ----

func (s *Server) handleListFilters(w http.ResponseWriter, r *http.Request) {
	f, err := s.store.ListFilters(r.Context(), me(r))
	if err != nil {
		s.fail(w, err, "list filters")
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (s *Server) handleCreateFilter(w http.ResponseWriter, r *http.Request) {
	var in store.Filter
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Name) == "" {
		writeErr(w, http.StatusBadRequest, "a filter needs a name and a query")
		return
	}
	f := &store.Filter{UserID: me(r), Name: strings.TrimSpace(in.Name), Icon: in.Icon, Query: in.Query}
	if err := s.store.SaveFilter(r.Context(), f); err != nil {
		s.fail(w, err, "save filter")
		return
	}
	s.touch()
	writeJSON(w, http.StatusCreated, f)
}

func (s *Server) handleUpdateFilter(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	f, err := s.store.GetFilter(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get filter")
		return
	}
	var in struct {
		Name  *string      `json:"name"`
		Icon  *string      `json:"icon"`
		Query *store.Query `json:"query"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
		f.Name = strings.TrimSpace(*in.Name)
	}
	if in.Icon != nil {
		f.Icon = *in.Icon
	}
	if in.Query != nil {
		f.Query = *in.Query
	}
	if err := s.store.SaveFilter(ctx, f); err != nil {
		s.fail(w, err, "save filter")
		return
	}
	s.touch()
	writeJSON(w, http.StatusOK, f)
}

func (s *Server) handleDeleteFilter(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteFilter(r.Context(), me(r), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete filter")
		return
	}
	s.touch()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleReorderFilters(w http.ResponseWriter, r *http.Request) {
	s.reorder(w, r, "filters")
}

// ---- tags ----

func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.store.ListTags(r.Context(), me(r))
	if err != nil {
		s.fail(w, err, "tags")
		return
	}
	writeJSON(w, http.StatusOK, tags)
}

func (s *Server) handleRenameTag(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	from := chi.URLParam(r, "tag")
	var in struct{ Name string }
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Name) == "" {
		writeErr(w, http.StatusBadRequest, "the new name is needed")
		return
	}
	to := cleanTags([]string{in.Name})
	if len(to) == 0 {
		writeErr(w, http.StatusBadRequest, "the new name is needed")
		return
	}
	n := 0
	for _, id := range s.store.TaggedIDs(ctx, uid, from) {
		t, err := s.store.GetTask(ctx, uid, id)
		if err != nil {
			continue
		}
		var tags []string
		for _, tag := range t.Tags {
			if strings.EqualFold(tag, from) {
				tag = to[0]
			}
			tags = append(tags, tag)
		}
		t.Tags = cleanTags(tags)
		if s.saveTask(ctx, t, "tags", "updated") == nil {
			n++
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"updated": n})
}

func (s *Server) handleDeleteTag(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	tag := chi.URLParam(r, "tag")
	n := 0
	for _, id := range s.store.TaggedIDs(ctx, uid, tag) {
		t, err := s.store.GetTask(ctx, uid, id)
		if err != nil {
			continue
		}
		var tags []string
		for _, x := range t.Tags {
			if !strings.EqualFold(x, tag) {
				tags = append(tags, x)
			}
		}
		t.Tags = tags
		if s.saveTask(ctx, t, "tags", "updated") == nil {
			n++
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"updated": n})
}

// ---- attachments ----

func (s *Server) handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	t, err := s.store.GetTask(ctx, uid, idParam(r, "id"))
	if err != nil || t.DeletedAt != "" {
		s.fail(w, store.ErrNotFound, "get task")
		return
	}
	max := int64(s.cfg.MaxAttachmentMB) << 20
	r.Body = http.MaxBytesReader(w, r.Body, max*4)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "could not read the upload")
		return
	}
	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		files = r.MultipartForm.File["file"]
	}
	if len(files) == 0 {
		writeErr(w, http.StatusBadRequest, "no file")
		return
	}
	dir := filepath.Join(s.cfg.DataDir, "attachments")
	for _, fh := range files {
		if fh.Size > max {
			writeErr(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("%s is larger than %d MB", fh.Filename, s.cfg.MaxAttachmentMB))
			return
		}
		src, err := fh.Open()
		if err != nil {
			continue
		}
		name := fmt.Sprintf("%d-%s%s", t.ID, store.RandomToken(6), strings.ToLower(filepath.Ext(fh.Filename)))
		dst, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			src.Close()
			s.fail(w, err, "store attachment")
			return
		}
		n, _ := io.Copy(dst, src)
		dst.Close()
		src.Close()
		typ := fh.Header.Get("Content-Type")
		if typ == "" {
			typ = mime.TypeByExtension(filepath.Ext(fh.Filename))
		}
		a := &store.Attachment{UserID: uid, TaskID: t.ID, Name: filepath.Base(fh.Filename), Size: n, Type: typ, Path: name}
		if err := s.store.AddAttachment(ctx, a); err != nil {
			s.fail(w, err, "store attachment")
			return
		}
	}
	s.store.AddActivity(ctx, uid, t.ID, "updated", "attachment")
	s.touch()
	t, _ = s.store.GetTask(ctx, uid, t.ID)
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) handleGetAttachment(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	a, err := s.store.GetAttachment(ctx, uid, idParam(r, "id"), idParam(r, "aid"))
	if err != nil {
		s.fail(w, err, "attachment")
		return
	}
	if a.Type != "" {
		w.Header().Set("Content-Type", a.Type)
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", a.Name))
	http.ServeFile(w, r, filepath.Join(s.cfg.DataDir, "attachments", filepath.Base(a.Path)))
}

func (s *Server) handleDeleteAttachment(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	p, err := s.store.DeleteAttachment(ctx, uid, idParam(r, "id"), idParam(r, "aid"))
	if err != nil {
		s.fail(w, err, "attachment")
		return
	}
	s.removeFiles([]string{p})
	s.touch()
	w.WriteHeader(http.StatusNoContent)
}
