// Package server wires the HTTP API, the CalDAV server, the calendar
// mirror, the Nextcloud sync, reminders, auth and the embedded SPA.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/crypto/bcrypt"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-tasks/internal/config"
	"github.com/abjelosevic88/pocket-homelab-tasks/internal/store"
)

// Version is injected at build time.
var Version = "dev"

// Server holds dependencies.
type Server struct {
	cfg   *config.Config
	store *store.Store
	log   *slog.Logger
	webFS fs.FS
	http  *http.Client

	started  time.Time
	requests atomic.Int64
	errors   atomic.Int64
	changed  atomic.Int64 // bumps on every write; GET /changes

	core    *core.Client
	coreMu  sync.Mutex
	coreOK  bool
	coreErr string

	oidc   *oidcProvider
	oidcMu sync.Mutex

	syncMu    sync.Mutex
	syncState map[int64]*ncState // per user
	syncNow   chan int64
}

// New creates a server.
func New(cfg *config.Config, st *store.Store, log *slog.Logger, webFS fs.FS) *Server {
	ua := "PocketTasks/" + Version + " (+https://github.com/abjelosevic88/pocket-homelab-tasks)"
	s := &Server{cfg: cfg, store: st, log: log, webFS: webFS, started: time.Now(), syncState: map[int64]*ncState{}, syncNow: make(chan int64, 16)}
	s.http = &http.Client{Timeout: 60 * time.Second}
	s.core = core.New(cfg.CoreURL, core.Options{TokenFile: cfg.DataDir + "/core.token", UserAgent: ua, ServiceCache: time.Minute, ServiceStale: 15 * time.Minute})
	s.oidc = &oidcProvider{issuerURL: cfg.OIDCIssuer}
	return s
}

// isOwner reports whether uid is the account the box was set up with.
func (s *Server) isOwner(ctx context.Context, uid int64) bool {
	return uid > 0 && uid == s.store.OwnerID(ctx)
}

// Bootstrap seeds the first admin (if configured), demo data and starts the workers.
func (s *Server) Bootstrap(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Join(s.cfg.DataDir, "attachments"), 0o755); err != nil {
		return fmt.Errorf("create attachments dir: %w", err)
	}
	n, err := s.store.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n == 0 && s.cfg.AuthMode == "local" && s.cfg.AdminEmail != "" && s.cfg.AdminPassword != "" {
		if len(s.cfg.AdminPassword) < 8 {
			return errors.New("ADMIN_PASSWORD must be at least 8 characters")
		}
		h, _ := bcrypt.GenerateFromPassword([]byte(s.cfg.AdminPassword), bcrypt.DefaultCost)
		u := &store.User{Email: s.cfg.AdminEmail, Name: s.cfg.AdminName, PasswordHash: string(h), Role: "admin"}
		if err := s.store.CreateUser(ctx, u); err != nil {
			return err
		}
		s.seedUser(ctx, u.ID)
		st, _ := s.store.GetSettings(ctx, 0)
		st.SetupComplete = true
		if err := s.store.SaveBoxSettings(ctx, st); err != nil {
			return err
		}
		s.log.Info("admin user created from environment", "email", u.Email)
	}
	s.seedOwnerNextcloud(ctx)
	if s.cfg.DemoData {
		if err := s.seedDemo(ctx); err != nil {
			return err
		}
	}
	_ = s.store.PurgeSessions(ctx)
	s.changed.Store(time.Now().UnixNano())
	go s.housekeeping(ctx)
	go s.reminderLoop(ctx)
	go s.syncLoop(ctx)
	if s.core.Configured() {
		go s.coreLoop(ctx)
	}
	return nil
}

// seedOwnerNextcloud copies NEXTCLOUD_* from the environment into the
// owner's sync settings once (import mode, so nothing is pushed until asked).
func (s *Server) seedOwnerNextcloud(ctx context.Context) {
	if s.cfg.NextcloudURL == "" || s.cfg.NextcloudUser == "" || s.cfg.NextcloudPassword == "" {
		return
	}
	owner := s.store.OwnerID(ctx)
	if owner == 0 || s.store.UserSetting(ctx, owner, "nc.seeded") == "1" {
		return
	}
	st, _ := s.store.GetSettings(ctx, owner)
	st.NCVia, st.NCURL, st.NCUser, st.NCPassword = "direct", s.cfg.NextcloudURL, s.cfg.NextcloudUser, s.cfg.NextcloudPassword
	if st.NCMode == "" {
		st.NCMode = "off"
	}
	_ = s.store.SaveNextcloud(ctx, owner, st)
	_ = s.store.SetUserSetting(ctx, owner, "nc.seeded", "1")
}

// housekeeping empties old trash, prunes the log and sessions once an hour.
func (s *Server) housekeeping(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		for _, uid := range s.store.UserIDs(ctx) {
			st, err := s.store.GetSettings(ctx, uid)
			if err != nil {
				continue
			}
			// nothing leaves the trash while Nextcloud has not heard of the delete
			if s.flushDeletes(ctx, uid) {
				if paths, err := s.store.PurgeTrash(ctx, uid, st.TrashDays); err == nil {
					s.removeFiles(paths)
				}
			}
			s.store.PruneActivity(ctx, uid, 5000)
		}
		_ = s.store.PurgeSessions(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Server) removeFiles(paths []string) {
	for _, p := range paths {
		if p != "" {
			_ = os.Remove(filepath.Join(s.cfg.DataDir, "attachments", filepath.Base(p)))
		}
	}
}

// touch records that something changed (the UI polls /changes).
func (s *Server) touch() { s.changed.Store(time.Now().UnixNano()) }

func init() {
	for _, m := range []string{"PROPFIND", "PROPPATCH", "REPORT", "MKCALENDAR", "MKCOL", "MOVE", "COPY", "LOCK", "UNLOCK"} {
		chi.RegisterMethod(m)
	}
}

// Router builds the chi router.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	if s.cfg.TrustProxy {
		r.Use(middleware.RealIP)
	}
	r.Use(middleware.RequestID)
	r.Use(s.logging)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5, "application/json", "text/html", "text/css", "application/javascript", "text/plain", "text/calendar", "application/xml", "text/xml"))

	r.Get("/healthz", s.handleHealth)
	r.Get("/readyz", s.handleReady)
	r.Get("/api/health", s.handleHealth)
	if s.cfg.MetricsEnabled {
		r.Get("/metrics", s.handleMetrics)
	}

	// CalDAV: /dav/… and the Nextcloud-shaped alias; the well-known redirect.
	r.Handle("/.well-known/caldav", http.RedirectHandler("/dav/", http.StatusMovedPermanently))
	r.Handle("/.well-known/caldav/", http.RedirectHandler("/dav/", http.StatusMovedPermanently))
	r.Handle("/dav", http.HandlerFunc(s.handleDAV))
	r.Handle("/dav/*", http.HandlerFunc(s.handleDAV))
	r.Handle("/remote.php/dav", http.HandlerFunc(s.handleDAV))
	r.Handle("/remote.php/dav/*", http.HandlerFunc(s.handleDAV))
	r.Handle("/principals/*", http.HandlerFunc(s.handleDAV))

	// The secret calendar feed.
	r.Get("/feed/{token}.ics", s.handleFeed)
	r.Get("/feed/{token}", s.handleFeed)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.Timeout(180 * time.Second))
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/setup", s.handleSetup)
		r.Get("/auth/status", s.handleAuthStatus)
		r.Get("/auth/oidc/start", s.handleOIDCStart)
		r.Get("/auth/oidc/callback", s.handleOIDCCallback)
		r.Post("/client-error", s.handleClientError)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Post("/auth/logout", s.handleLogout)
			r.Get("/auth/me", s.handleMe)
			r.Put("/auth/me", s.handleUpdateMe)

			r.Get("/dashboard", s.handleDashboard)
			r.Get("/changes", s.handleChanges)

			r.Get("/lists", s.handleListLists)
			r.Post("/lists", s.handleCreateList)
			r.Post("/lists/reorder", s.handleReorderLists)
			r.Put("/lists/{id}", s.handleUpdateList)
			r.Delete("/lists/{id}", s.handleDeleteList)
			r.Get("/lists/{id}/sections", s.handleListSections)
			r.Post("/lists/{id}/sections", s.handleCreateSection)
			r.Post("/lists/{id}/sections/reorder", s.handleReorderSections)
			r.Put("/sections/{id}", s.handleUpdateSection)
			r.Delete("/sections/{id}", s.handleDeleteSection)
			r.Get("/areas", s.handleListAreas)
			r.Post("/areas", s.handleCreateArea)
			r.Post("/areas/reorder", s.handleReorderAreas)
			r.Put("/areas/{id}", s.handleUpdateArea)
			r.Delete("/areas/{id}", s.handleDeleteArea)

			r.Get("/tasks", s.handleListTasks)
			r.Post("/tasks", s.handleCreateTask)
			r.Post("/tasks/quick", s.handleQuick)
			r.Post("/tasks/batch", s.handleBatch)
			r.Get("/tasks/{id}", s.handleGetTask)
			r.Put("/tasks/{id}", s.handleUpdateTask)
			r.Delete("/tasks/{id}", s.handleDeleteTask)
			r.Post("/tasks/{id}/complete", s.handleCompleteTask)
			r.Post("/tasks/{id}/move", s.handleMoveTask)
			r.Post("/tasks/{id}/duplicate", s.handleDuplicateTask)
			r.Get("/tasks/{id}/ics", s.handleTaskICS)
			r.Post("/tasks/{id}/attachments", s.handleUploadAttachment)
			r.Get("/tasks/{id}/attachments/{aid}", s.handleGetAttachment)
			r.Delete("/tasks/{id}/attachments/{aid}", s.handleDeleteAttachment)

			r.Get("/tags", s.handleTags)
			r.Put("/tags/{tag}", s.handleRenameTag)
			r.Delete("/tags/{tag}", s.handleDeleteTag)
			r.Get("/filters", s.handleListFilters)
			r.Post("/filters", s.handleCreateFilter)
			r.Post("/filters/reorder", s.handleReorderFilters)
			r.Put("/filters/{id}", s.handleUpdateFilter)
			r.Delete("/filters/{id}", s.handleDeleteFilter)
			r.Get("/search", s.handleSearch)
			r.Get("/calendar", s.handleCalendar)
			r.Get("/trash", s.handleTrash)
			r.Post("/trash/{id}/restore", s.handleRestore)
			r.Delete("/trash/{id}", s.handlePurge)
			r.Delete("/trash", s.handleEmptyTrash)
			r.Get("/activity", s.handleActivity)

			r.Get("/sync/nextcloud", s.handleNCStatus)
			r.Put("/sync/nextcloud", s.handleNCSave)
			r.Post("/sync/nextcloud/test", s.handleNCTest)
			r.Post("/sync/nextcloud/run", s.handleNCRun)

			r.Get("/feeds", s.handleFeeds)
			r.Post("/feeds/rotate", s.handleRotateFeed)

			r.Get("/users", s.handleListUsers)
			r.Post("/users", s.handleCreateUser)
			r.Put("/users/{id}", s.handleUpdateUser)
			r.Delete("/users/{id}", s.handleDeleteUser)

			r.Get("/settings", s.handleGetSettings)
			r.Put("/settings", s.handlePutSettings)
			r.Get("/settings/system", s.handleSystemInfo)
			r.Post("/notify/test", s.handleTestNotify)

			r.Get("/tokens", s.handleListTokens)
			r.Post("/tokens", s.handleCreateToken)
			r.Delete("/tokens/{id}", s.handleDeleteToken)

			r.Get("/backup", s.handleBackup)
			r.Get("/export.json", s.handleExportJSON)
			r.Get("/export.ics", s.handleExportICS)
			r.Post("/import", s.handleImport)
		})
	})

	r.NotFound(s.handleSPA)
	return r
}

// ---------- helpers ----------

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		s.requests.Add(1)
		if ww.Status() >= 500 {
			s.errors.Add(1)
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			return
		}
		s.log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "dur", time.Since(start).String(), "ip", r.RemoteAddr)
	})
}

type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

type userError struct{ msg string }

func (e userError) Error() string { return e.msg }

func badRequest(msg string) error { return userError{msg} }

func (s *Server) fail(w http.ResponseWriter, err error, context string) {
	var ue userError
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
		return
	case errors.Is(err, store.ErrConflict):
		writeErr(w, http.StatusConflict, "already there")
		return
	case errors.As(err, &ue):
		writeErr(w, http.StatusBadRequest, ue.msg)
		return
	}
	s.log.Error(context, "err", err)
	writeErr(w, http.StatusInternalServerError, context+": "+err.Error())
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 16<<20))
	return dec.Decode(v)
}

func idParam(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	return n
}

func queryInt(r *http.Request, name string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(name))
	return n
}

func queryInt64(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(r.URL.Query().Get(name), 10, 64)
	return n
}

func queryBool(r *http.Request, name string) bool {
	v := strings.ToLower(r.URL.Query().Get(name))
	return v == "1" || v == "true" || v == "yes"
}

func me(r *http.Request) int64 { return actorID(r.Context()) }

// ---------- Pocket Core ----------

// coreLoop registers with Core on start (retrying until it answers) and
// re-registers every 30 minutes so version and last-seen stay fresh.
func (s *Server) coreLoop(ctx context.Context) {
	wait := 5 * time.Second
	for {
		err := s.registerWithCore(ctx)
		s.coreMu.Lock()
		s.coreOK, s.coreErr = err == nil, ""
		if err != nil {
			s.coreErr = err.Error()
		}
		s.coreMu.Unlock()
		if err != nil {
			s.log.Warn("pocket core: registration failed, retrying", "err", err, "in", wait.String())
		} else {
			wait = 30 * time.Minute
			s.reportModes(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if err != nil && wait < 5*time.Minute {
			wait *= 2
		}
	}
}

func (s *Server) registerWithCore(ctx context.Context) error {
	m := core.Manifest{
		Slug: "tasks", Name: "Pocket Tasks",
		Description: "Tasks with a real CalDAV server (Apple Reminders, BusyCal, Thunderbird, Tasks.org), every dated task on any calendar as an event, Nextcloud Tasks import and two-way sync, natural-language capture, sections, repeats, reminders pushed from the server.",
		Icon:        "/favicon.svg", Version: Version,
		URL: s.cfg.CoreAppURL, PublicURL: s.cfg.BaseURL, HealthPath: "/healthz", EnvPrefix: "TASKS",
		Publishes: []string{"tasks.created", "tasks.updated", "tasks.completed", "tasks.deleted", "tasks.reminder"},
		Offers:    linkOffers,
		Services:  serviceUses,
	}
	if s.cfg.BaseURL != "" {
		m.OIDC = &core.OIDCRequest{RedirectURIs: []string{s.cfg.BaseURL + "/api/v1/auth/oidc/callback"}}
	}
	reg, err := s.core.Register(ctx, s.cfg.CoreRegistrationKey, m)
	if err != nil {
		return err
	}
	if reg.Token != "" {
		s.log.Info("pocket core: registered, app token stored", "core", s.cfg.CoreURL)
	} else {
		s.log.Debug("pocket core: re-registered", "core", s.cfg.CoreURL)
	}
	return nil
}

// publish sends an event to Core without blocking the request.
func (s *Server) publish(userID int64, topic, subject string, payload map[string]any) {
	if !s.core.Configured() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		payload["user_id"] = userID
		if u, err := s.store.GetUser(ctx, userID); err == nil {
			payload["user_email"] = u.Email
		}
		if err := s.core.Publish(ctx, topic, subject, payload); err != nil {
			s.log.Warn("pocket core: publish failed", "topic", topic, "err", err)
		}
	}()
}

// coreStatus summarises the Pocket Core link for the About page.
func (s *Server) coreStatus() map[string]any {
	s.coreMu.Lock()
	defer s.coreMu.Unlock()
	return map[string]any{"configured": s.core.Configured(), "url": s.cfg.CoreURL, "registered": s.coreOK, "error": s.coreErr}
}

// handleClientError writes a crash of the web app into the server's log.
func (s *Server) handleClientError(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Message, Stack, Component, URL, Where, Agent string
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	who := ""
	if u, err := s.resolveUser(r); err == nil && u != nil {
		who = u.Email
	}
	s.log.Warn("web app crashed", "user", who, "where", in.Where, "url", in.URL, "message", in.Message, "stack", in.Stack, "component", in.Component, "agent", in.Agent)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleChanges(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"stamp": s.changed.Load()})
}

// ---------- health / metrics ----------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": Version, "uptime": time.Since(s.started).Round(time.Second).String()})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DB.PingContext(r.Context()); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	counts := s.store.Counts(ctx)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP pocket_tasks_info Build info.\n# TYPE pocket_tasks_info gauge\npocket_tasks_info{version=%q} 1\n", Version)
	fmt.Fprintf(w, "# HELP pocket_tasks_uptime_seconds Process uptime.\n# TYPE pocket_tasks_uptime_seconds gauge\npocket_tasks_uptime_seconds %d\n", int(time.Since(s.started).Seconds()))
	fmt.Fprintf(w, "# HELP pocket_tasks_http_requests_total HTTP requests served.\n# TYPE pocket_tasks_http_requests_total counter\npocket_tasks_http_requests_total %d\n", s.requests.Load())
	fmt.Fprintf(w, "# HELP pocket_tasks_http_errors_total HTTP 5xx responses.\n# TYPE pocket_tasks_http_errors_total counter\npocket_tasks_http_errors_total %d\n", s.errors.Load())
	fmt.Fprintf(w, "# HELP pocket_tasks_rows Rows per table.\n# TYPE pocket_tasks_rows gauge\n")
	for k, v := range counts {
		fmt.Fprintf(w, "pocket_tasks_rows{table=%q} %d\n", k, v)
	}
}

// ---------- static / SPA ----------

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/dav") || strings.HasPrefix(r.URL.Path, "/remote.php/") || strings.HasPrefix(r.URL.Path, "/feed/") {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if s.webFS == nil {
		http.Error(w, "web UI not built (run `make web`)", http.StatusNotFound)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if f, err := s.webFS.Open(path); err == nil {
		f.Close()
		if strings.HasPrefix(path, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.FileServerFS(s.webFS).ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	r.URL.Path = "/"
	http.FileServerFS(s.webFS).ServeHTTP(w, r)
}

// loc returns the person's time zone (their choice, else the box's).
func (s *Server) loc(ctx context.Context, uid int64) *time.Location {
	if tz := s.store.UserSetting(ctx, uid, "timezone"); tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			return l
		}
	}
	l, err := time.LoadLocation(s.cfg.Timezone)
	if err != nil {
		return time.UTC
	}
	return l
}
