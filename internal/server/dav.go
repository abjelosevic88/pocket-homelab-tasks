package server

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-tasks/internal/ical"
	"github.com/abjelosevic88/pocket-homelab-tasks/internal/store"
)

// A CalDAV server (RFC 4791, 6578, the Apple and Nextcloud extensions
// clients ask for). Paths, under /dav/ or /remote.php/dav/:
//
//	/                              root: current-user-principal
//	/principals/<user>/            the person
//	/calendars/<user>/             calendar home: every list + the Tasks calendar
//	/calendars/<user>/<list uid>/  a list (VTODO collection)
//	/calendars/<user>/<list uid>/<href>  a task
//	/calendars/<user>/tasks-calendar/    the mirror (VEVENT collection)
//
// <user> is the person's CalDAV name (or email, or "me"); another person's
// name answers 404, so nobody learns who else is on the box.

const (
	nsDAV    = "DAV:"
	nsCalDAV = "urn:ietf:params:xml:ns:caldav"
	nsCS     = "http://calendarserver.org/ns/"
	nsApple  = "http://apple.com/ns/ical/"
	nsOC     = "http://owncloud.org/ns"
	nsNC     = "http://nextcloud.com/ns"
	nsSabre  = "http://sabredav.org/ns"

	mirrorUID = "tasks-calendar"
	syncPfx   = "http://pocket-tasks/sync/"
)

type davReq struct {
	prefix string // "/dav" or "/remote.php/dav"
	user   *store.User
	parts  []string // path segments after the prefix
	depth  string
}

// handleDAV routes every request under the DAV prefixes.
func (s *Server) handleDAV(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	prefix := "/dav"
	switch {
	case strings.HasPrefix(p, "/remote.php/dav"):
		prefix = "/remote.php/dav"
	case strings.HasPrefix(p, "/principals/"):
		prefix = "" // /principals/<user>/ as Apple sometimes derives it
	}
	rest := strings.TrimPrefix(p, prefix)
	var parts []string
	for _, seg := range strings.Split(rest, "/") {
		if seg != "" {
			if u, err := url.PathUnescape(seg); err == nil {
				seg = u
			}
			parts = append(parts, seg)
		}
	}
	if r.Method == http.MethodOptions {
		s.davOptions(w)
		return
	}
	u, err := s.resolveUser(r)
	if err != nil || u == nil {
		w.Header().Set("WWW-Authenticate", `Basic realm="Pocket Tasks"`)
		w.Header().Set("DAV", "1, 3, calendar-access")
		http.Error(w, "sign in with your CalDAV name and an API token (Settings → Apps & API)", http.StatusUnauthorized)
		return
	}
	if prefix == "" {
		prefix = "/dav"
		parts = append([]string{"principals"}, parts...)
	}
	req := &davReq{prefix: prefix, user: u, parts: parts, depth: strings.ToLower(r.Header.Get("Depth"))}
	if req.depth == "" {
		req.depth = "infinity"
	}
	ctx := r.Context()
	w.Header().Set("DAV", "1, 3, extended-mkcol, calendar-access")
	switch len(parts) {
	case 0:
		s.davRoot(ctx, w, r, req)
	case 1:
		switch parts[0] {
		case "principals":
			s.davRoot(ctx, w, r, req) // a principals listing: answer like the root
		case "calendars":
			s.davRoot(ctx, w, r, req)
		default:
			s.davNotFound(w)
		}
	default:
		if !s.davUserMatches(parts[1], u) {
			s.davNotFound(w)
			return
		}
		switch {
		case parts[0] == "principals" && len(parts) == 2:
			s.davPrincipal(ctx, w, r, req)
		case parts[0] == "calendars" && len(parts) == 2:
			s.davHome(ctx, w, r, req)
		case parts[0] == "calendars" && len(parts) == 3:
			s.davCollection(ctx, w, r, req, parts[2])
		case parts[0] == "calendars" && len(parts) == 4:
			s.davResource(ctx, w, r, req, parts[2], parts[3])
		default:
			s.davNotFound(w)
		}
	}
}

func (s *Server) davUserMatches(seg string, u *store.User) bool {
	seg = strings.ToLower(seg)
	return seg == "me" || seg == strings.ToLower(u.Username) || seg == strings.ToLower(u.Email) || seg == strconv.FormatInt(u.ID, 10)
}

func (s *Server) davOptions(w http.ResponseWriter) {
	w.Header().Set("DAV", "1, 3, extended-mkcol, calendar-access")
	w.Header().Set("Allow", "OPTIONS, GET, HEAD, PUT, DELETE, PROPFIND, PROPPATCH, REPORT, MKCALENDAR, MKCOL")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) davNotFound(w http.ResponseWriter) {
	http.Error(w, "not found", http.StatusNotFound)
}

func (s *Server) davMethodNotAllowed(w http.ResponseWriter) {
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// ---------- XML in ----------

type xnode struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
	Text    string     `xml:",chardata"`
	Nodes   []xnode    `xml:",any"`
}

func (n *xnode) child(local string) *xnode {
	for i := range n.Nodes {
		if n.Nodes[i].XMLName.Local == local {
			return &n.Nodes[i]
		}
	}
	return nil
}

func (n *xnode) children(local string) []*xnode {
	var out []*xnode
	for i := range n.Nodes {
		if n.Nodes[i].XMLName.Local == local {
			out = append(out, &n.Nodes[i])
		}
	}
	return out
}

func (n *xnode) attr(name string) string {
	for _, a := range n.Attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func readXML(r *http.Request) (*xnode, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil, nil
	}
	var n xnode
	if err := xml.Unmarshal(body, &n); err != nil {
		return nil, err
	}
	return &n, nil
}

// propName is a requested property.
type propName struct{ Space, Local string }

func requestedProps(n *xnode) (props []propName, allprop bool) {
	if n == nil {
		return nil, true
	}
	if n.child("allprop") != nil || n.child("propname") != nil {
		return nil, true
	}
	p := n.child("prop")
	if p == nil {
		return nil, true
	}
	for _, c := range p.Nodes {
		props = append(props, propName{c.XMLName.Space, c.XMLName.Local})
	}
	return props, false
}

// ---------- XML out ----------

type propstat struct {
	found    []string // rendered <prop> children
	notFound []propName
}

func (ps *propstat) add(xmlFragment string) { ps.found = append(ps.found, xmlFragment) }

func xesc(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func hrefEsc(p string) string {
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		parts[i] = url.PathEscape(seg)
	}
	return strings.Join(parts, "/")
}

type msWriter struct {
	b strings.Builder
}

func newMultistatus() *msWriter {
	m := &msWriter{}
	m.b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	m.b.WriteString(`<d:multistatus xmlns:d="DAV:" xmlns:cal="urn:ietf:params:xml:ns:caldav" xmlns:cs="http://calendarserver.org/ns/" xmlns:ical="http://apple.com/ns/ical/" xmlns:oc="http://owncloud.org/ns" xmlns:nc="http://nextcloud.com/ns" xmlns:s="http://sabredav.org/ns">` + "\n")
	return m
}

func (m *msWriter) response(href string, ps *propstat) {
	m.b.WriteString("<d:response><d:href>" + xesc(hrefEsc(href)) + "</d:href>")
	if len(ps.found) > 0 {
		m.b.WriteString("<d:propstat><d:prop>" + strings.Join(ps.found, "") + "</d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat>")
	}
	if len(ps.notFound) > 0 {
		m.b.WriteString("<d:propstat><d:prop>")
		for _, p := range ps.notFound {
			m.b.WriteString(`<x:` + p.Local + ` xmlns:x="` + xesc(p.Space) + `"/>`)
		}
		m.b.WriteString("</d:prop><d:status>HTTP/1.1 404 Not Found</d:status></d:propstat>")
	}
	if len(ps.found) == 0 && len(ps.notFound) == 0 {
		m.b.WriteString("<d:propstat><d:prop/><d:status>HTTP/1.1 200 OK</d:status></d:propstat>")
	}
	m.b.WriteString("</d:response>\n")
}

func (m *msWriter) gone(href string) {
	m.b.WriteString("<d:response><d:href>" + xesc(hrefEsc(href)) + "</d:href><d:status>HTTP/1.1 404 Not Found</d:status></d:response>\n")
}

func (m *msWriter) write(w http.ResponseWriter, extra string) {
	m.b.WriteString(extra)
	m.b.WriteString("</d:multistatus>\n")
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	_, _ = w.Write([]byte(m.b.String()))
}

// ---------- property sets ----------

type davNode struct {
	kind     string // root | principal | home | collection | mirror | resource
	href     string
	list     *store.List
	task     *store.Task
	etag     string
	ics      string
	modified string
	req      *davReq
	seq      int64
}

func (s *Server) principalHref(req *davReq) string {
	return req.prefix + "/principals/" + req.user.Username + "/"
}

func (s *Server) homeHref(req *davReq) string {
	return req.prefix + "/calendars/" + req.user.Username + "/"
}

// props answers a property request for one node.
func (s *Server) props(ctx context.Context, n *davNode, want []propName, all bool) *propstat {
	ps := &propstat{}
	req := n.req
	u := req.user
	get := func(space, local string) (string, bool) {
		switch space + " " + local {
		case nsDAV + " resourcetype":
			switch n.kind {
			case "principal":
				return "<d:resourcetype><d:principal/><d:collection/></d:resourcetype>", true
			case "collection", "mirror":
				return "<d:resourcetype><d:collection/><cal:calendar/></d:resourcetype>", true
			case "resource":
				return "<d:resourcetype/>", true
			}
			return "<d:resourcetype><d:collection/></d:resourcetype>", true
		case nsDAV + " displayname":
			switch n.kind {
			case "principal":
				return "<d:displayname>" + xesc(firstNonEmpty(u.Name, u.Email)) + "</d:displayname>", true
			case "home":
				return "<d:displayname>" + xesc(firstNonEmpty(u.Name, u.Email)) + "</d:displayname>", true
			case "collection":
				return "<d:displayname>" + xesc(n.list.Name) + "</d:displayname>", true
			case "mirror":
				return "<d:displayname>Tasks</d:displayname>", true
			case "resource":
				return "<d:displayname>" + xesc(n.task.Title) + "</d:displayname>", true
			}
			return "<d:displayname>Pocket Tasks</d:displayname>", true
		case nsDAV + " current-user-principal":
			return "<d:current-user-principal><d:href>" + xesc(hrefEsc(s.principalHref(req))) + "</d:href></d:current-user-principal>", true
		case nsDAV + " principal-URL":
			if n.kind == "principal" {
				return "<d:principal-URL><d:href>" + xesc(hrefEsc(s.principalHref(req))) + "</d:href></d:principal-URL>", true
			}
		case nsDAV + " owner":
			if n.kind != "root" {
				return "<d:owner><d:href>" + xesc(hrefEsc(s.principalHref(req))) + "</d:href></d:owner>", true
			}
		case nsOC + " owner-principal":
			if n.kind == "collection" || n.kind == "mirror" {
				return "<oc:owner-principal>" + xesc(hrefEsc(strings.TrimSuffix(s.principalHref(req), "/"))) + "</oc:owner-principal>", true
			}
		case nsCalDAV + " calendar-home-set":
			if n.kind == "principal" {
				return "<cal:calendar-home-set><d:href>" + xesc(hrefEsc(s.homeHref(req))) + "</d:href></cal:calendar-home-set>", true
			}
		case nsCalDAV + " calendar-user-address-set":
			if n.kind == "principal" {
				return "<cal:calendar-user-address-set><d:href>mailto:" + xesc(u.Email) + "</d:href></cal:calendar-user-address-set>", true
			}
		case nsCS + " email-address-set":
			if n.kind == "principal" {
				return "<cs:email-address-set><cs:email-address>" + xesc(u.Email) + "</cs:email-address></cs:email-address-set>", true
			}
		case nsDAV + " current-user-privilege-set":
			priv := "<d:privilege><d:read/></d:privilege><d:privilege><d:write/></d:privilege><d:privilege><d:write-properties/></d:privilege><d:privilege><d:write-content/></d:privilege><d:privilege><d:bind/></d:privilege><d:privilege><d:unbind/></d:privilege><d:privilege><d:read-current-user-privilege-set/></d:privilege>"
			if n.kind == "collection" && n.list.IsInbox {
				priv = "<d:privilege><d:read/></d:privilege><d:privilege><d:write/></d:privilege><d:privilege><d:write-properties/></d:privilege><d:privilege><d:write-content/></d:privilege><d:privilege><d:bind/></d:privilege><d:privilege><d:read-current-user-privilege-set/></d:privilege>"
			}
			return "<d:current-user-privilege-set>" + priv + "</d:current-user-privilege-set>", true
		case nsDAV + " supported-report-set":
			if n.kind == "collection" || n.kind == "mirror" || n.kind == "home" {
				return "<d:supported-report-set><d:supported-report><d:report><cal:calendar-query/></d:report></d:supported-report><d:supported-report><d:report><cal:calendar-multiget/></d:report></d:supported-report><d:supported-report><d:report><d:sync-collection/></d:report></d:supported-report></d:supported-report-set>", true
			}
			return "<d:supported-report-set/>", true
		case nsCalDAV + " supported-calendar-component-set":
			if n.kind == "collection" {
				return `<cal:supported-calendar-component-set><cal:comp name="VTODO"/></cal:supported-calendar-component-set>`, true
			}
			if n.kind == "mirror" {
				return `<cal:supported-calendar-component-set><cal:comp name="VEVENT"/></cal:supported-calendar-component-set>`, true
			}
		case nsCalDAV + " supported-calendar-data":
			if n.kind == "collection" || n.kind == "mirror" {
				return `<cal:supported-calendar-data><cal:calendar-data content-type="text/calendar" version="2.0"/></cal:supported-calendar-data>`, true
			}
		case nsCalDAV + " calendar-description":
			if n.kind == "collection" {
				return "<cal:calendar-description>" + xesc(n.list.Description) + "</cal:calendar-description>", true
			}
			if n.kind == "mirror" {
				return "<cal:calendar-description>Your dated tasks as events. Move one to reschedule it.</cal:calendar-description>", true
			}
		case nsCalDAV + " max-resource-size":
			if n.kind == "collection" || n.kind == "mirror" {
				return "<cal:max-resource-size>10000000</cal:max-resource-size>", true
			}
		case nsApple + " calendar-color":
			if n.kind == "collection" {
				c := n.list.Color
				if c == "" {
					c = "#2563eb"
				}
				return "<ical:calendar-color>" + xesc(strings.ToUpper(c)) + "FF</ical:calendar-color>", true
			}
			if n.kind == "mirror" {
				return "<ical:calendar-color>#2563EBFF</ical:calendar-color>", true
			}
		case nsApple + " calendar-order":
			if n.kind == "collection" {
				return "<ical:calendar-order>" + strconv.Itoa(int(n.list.SortOrder)) + "</ical:calendar-order>", true
			}
			if n.kind == "mirror" {
				return "<ical:calendar-order>99</ical:calendar-order>", true
			}
		case nsOC + " calendar-enabled":
			if n.kind == "collection" || n.kind == "mirror" {
				return "<oc:calendar-enabled>1</oc:calendar-enabled>", true
			}
		case nsCS + " getctag":
			if n.kind == "collection" || n.kind == "mirror" {
				return "<cs:getctag>" + xesc(fmt.Sprintf("%d-%d", n.list.ID, n.seq)) + "</cs:getctag>", true
			}
		case nsDAV + " sync-token":
			if n.kind == "collection" || n.kind == "mirror" {
				return "<d:sync-token>" + xesc(syncPfx+strconv.FormatInt(n.seq, 10)) + "</d:sync-token>", true
			}
		case nsDAV + " getetag":
			if n.kind == "resource" {
				return "<d:getetag>" + xesc(n.etag) + "</d:getetag>", true
			}
		case nsDAV + " getcontenttype":
			if n.kind == "resource" {
				comp := "VTODO"
				if n.list.UID == mirrorUID {
					comp = "VEVENT"
				}
				return "<d:getcontenttype>text/calendar; charset=utf-8; component=" + comp + "</d:getcontenttype>", true
			}
		case nsDAV + " getcontentlength":
			if n.kind == "resource" {
				return "<d:getcontentlength>" + strconv.Itoa(len(n.ics)) + "</d:getcontentlength>", true
			}
		case nsDAV + " getlastmodified":
			if n.modified != "" {
				t := store.ParseTime(n.modified)
				if !t.IsZero() {
					return "<d:getlastmodified>" + t.UTC().Format(http.TimeFormat) + "</d:getlastmodified>", true
				}
			}
		case nsCalDAV + " calendar-data":
			if n.kind == "resource" {
				return "<cal:calendar-data>" + xesc(n.ics) + "</cal:calendar-data>", true
			}
		case nsSabre + " sync-token", nsDAV + " sync-level":
			if n.kind == "collection" || n.kind == "mirror" {
				return "<s:sync-token>" + strconv.FormatInt(n.seq, 10) + "</s:sync-token>", true
			}
		case nsCalDAV + " schedule-calendar-transp":
			if n.kind == "collection" || n.kind == "mirror" {
				return "<cal:schedule-calendar-transp><cal:transparent/></cal:schedule-calendar-transp>", true
			}
		}
		return "", false
	}
	if all {
		for _, p := range defaultProps(n.kind) {
			if v, ok := get(p.Space, p.Local); ok {
				ps.add(v)
			}
		}
		return ps
	}
	for _, p := range want {
		if v, ok := get(p.Space, p.Local); ok {
			ps.add(v)
		} else {
			ps.notFound = append(ps.notFound, p)
		}
	}
	return ps
}

func defaultProps(kind string) []propName {
	common := []propName{{nsDAV, "resourcetype"}, {nsDAV, "displayname"}, {nsDAV, "current-user-principal"}, {nsDAV, "owner"}, {nsDAV, "supported-report-set"}, {nsDAV, "current-user-privilege-set"}}
	switch kind {
	case "principal":
		return append(common, propName{nsCalDAV, "calendar-home-set"}, propName{nsDAV, "principal-URL"}, propName{nsCalDAV, "calendar-user-address-set"})
	case "collection", "mirror":
		return append(common, propName{nsCalDAV, "supported-calendar-component-set"}, propName{nsCalDAV, "calendar-description"}, propName{nsApple, "calendar-color"}, propName{nsApple, "calendar-order"}, propName{nsCS, "getctag"}, propName{nsDAV, "sync-token"})
	case "resource":
		return []propName{{nsDAV, "resourcetype"}, {nsDAV, "getetag"}, {nsDAV, "getcontenttype"}, {nsDAV, "getcontentlength"}, {nsDAV, "getlastmodified"}}
	}
	return common
}

// ---------- nodes ----------

func (s *Server) davRoot(ctx context.Context, w http.ResponseWriter, r *http.Request, req *davReq) {
	if r.Method != "PROPFIND" {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			fmt.Fprintf(w, "Pocket Tasks CalDAV. Calendar home: %s\n", s.homeHref(req))
			return
		}
		s.davMethodNotAllowed(w)
		return
	}
	body, err := readXML(r)
	if err != nil {
		http.Error(w, "bad xml", http.StatusBadRequest)
		return
	}
	want, all := requestedProps(body)
	ms := newMultistatus()
	self := req.prefix + "/" + strings.Join(req.parts, "/")
	if len(req.parts) > 0 {
		self += "/"
	}
	ms.response(self, s.props(ctx, &davNode{kind: "root", req: req}, want, all))
	if req.depth != "0" && len(req.parts) == 0 {
		ms.response(req.prefix+"/principals/", s.props(ctx, &davNode{kind: "root", req: req}, want, all))
		ms.response(req.prefix+"/calendars/", s.props(ctx, &davNode{kind: "root", req: req}, want, all))
	}
	if req.depth != "0" && len(req.parts) == 1 && req.parts[0] == "principals" {
		ms.response(s.principalHref(req), s.props(ctx, &davNode{kind: "principal", req: req}, want, all))
	}
	if req.depth != "0" && len(req.parts) == 1 && req.parts[0] == "calendars" {
		ms.response(s.homeHref(req), s.props(ctx, &davNode{kind: "home", req: req}, want, all))
	}
	ms.write(w, "")
}

func (s *Server) davPrincipal(ctx context.Context, w http.ResponseWriter, r *http.Request, req *davReq) {
	switch r.Method {
	case "PROPFIND", "REPORT":
		body, err := readXML(r)
		if err != nil {
			http.Error(w, "bad xml", http.StatusBadRequest)
			return
		}
		want, all := requestedProps(body)
		if r.Method == "REPORT" && body != nil {
			// principal-property-search / expand-property: answer with the one principal
			if p := body.child("prop"); p != nil {
				want, all = requestedProps(body)
			}
		}
		ms := newMultistatus()
		ms.response(s.principalHref(req), s.props(ctx, &davNode{kind: "principal", req: req}, want, all))
		ms.write(w, "")
	default:
		s.davMethodNotAllowed(w)
	}
}

func (s *Server) mirrorList(req *davReq) *store.List {
	return &store.List{ID: 0, UserID: req.user.ID, UID: mirrorUID, Name: "Tasks", Color: "#2563eb"}
}

func (s *Server) davHome(ctx context.Context, w http.ResponseWriter, r *http.Request, req *davReq) {
	switch r.Method {
	case "PROPFIND":
		body, err := readXML(r)
		if err != nil {
			http.Error(w, "bad xml", http.StatusBadRequest)
			return
		}
		want, all := requestedProps(body)
		ms := newMultistatus()
		ms.response(s.homeHref(req), s.props(ctx, &davNode{kind: "home", req: req}, want, all))
		if req.depth != "0" {
			lists, _ := s.store.ListLists(ctx, req.user.ID)
			for i := range lists {
				l := &lists[i]
				if l.Archived {
					continue
				}
				ms.response(s.homeHref(req)+l.UID+"/", s.props(ctx, &davNode{kind: "collection", list: l, seq: l.Seq, req: req, modified: l.UpdatedAt}, want, all))
			}
			st, _ := s.store.GetSettings(ctx, req.user.ID)
			if st.MirrorEnabled {
				ms.response(s.homeHref(req)+mirrorUID+"/", s.props(ctx, &davNode{kind: "mirror", list: s.mirrorList(req), seq: s.store.MaxSeq(ctx, req.user.ID), req: req}, want, all))
			}
		}
		ms.write(w, "")
	case "REPORT":
		// some clients REPORT the home; answer an empty multistatus
		newMultistatus().write(w, "")
	case http.MethodGet, http.MethodHead:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "Calendar home of %s\n", req.user.Email)
	default:
		s.davMethodNotAllowed(w)
	}
}

// resolveCollection finds a list (or the mirror) by its path segment.
func (s *Server) resolveCollection(ctx context.Context, req *davReq, seg string) (*store.List, bool, error) {
	if seg == mirrorUID {
		return s.mirrorList(req), true, nil
	}
	l, err := s.store.GetListByUID(ctx, req.user.ID, seg)
	if err != nil {
		return nil, false, err
	}
	return l, false, nil
}

func (s *Server) davCollection(ctx context.Context, w http.ResponseWriter, r *http.Request, req *davReq, seg string) {
	uid := req.user.ID
	l, mirror, err := s.resolveCollection(ctx, req, seg)
	if err != nil {
		switch r.Method {
		case "MKCALENDAR", "MKCOL":
			s.davMkCalendar(ctx, w, r, req, seg)
		default:
			s.davNotFound(w)
		}
		return
	}
	href := s.homeHref(req) + seg + "/"
	switch r.Method {
	case "MKCALENDAR", "MKCOL":
		http.Error(w, "already there", http.StatusMethodNotAllowed)
	case "PROPFIND":
		body, err := readXML(r)
		if err != nil {
			http.Error(w, "bad xml", http.StatusBadRequest)
			return
		}
		want, all := requestedProps(body)
		ms := newMultistatus()
		kind := "collection"
		seq := l.Seq
		if mirror {
			kind, seq = "mirror", s.store.MaxSeq(ctx, uid)
		}
		ms.response(href, s.props(ctx, &davNode{kind: kind, list: l, seq: seq, req: req, modified: l.UpdatedAt}, want, all))
		if req.depth != "0" {
			for _, n := range s.collectionNodes(ctx, req, l, mirror) {
				ms.response(n.href, s.props(ctx, n, want, all))
			}
		}
		ms.write(w, "")
	case "PROPPATCH":
		s.davProppatch(ctx, w, r, req, l, mirror, href)
	case "REPORT":
		s.davReport(ctx, w, r, req, l, mirror, href)
	case http.MethodDelete:
		if mirror || l.IsInbox {
			http.Error(w, "this calendar cannot be deleted", http.StatusForbidden)
			return
		}
		// the tasks go to the trash first, then the list goes
		ts, _ := s.store.ListTasksInList(ctx, uid, l.ID)
		for _, t := range ts {
			if t.ParentID == nil {
				_ = s.store.SoftDeleteTask(ctx, uid, t.ID)
			}
		}
		inbox, _ := s.store.Inbox(ctx, uid)
		if err := s.store.DeleteList(ctx, uid, l.ID, inbox.ID); err != nil {
			s.fail(w, err, "delete list")
			return
		}
		s.touch()
		w.WriteHeader(http.StatusNoContent)
	case http.MethodGet, http.MethodHead:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "Calendar %q\n", l.Name)
	default:
		s.davMethodNotAllowed(w)
	}
}

// collectionNodes lists the resources of a collection.
func (s *Server) collectionNodes(ctx context.Context, req *davReq, l *store.List, mirror bool) []*davNode {
	var out []*davNode
	if mirror {
		st, _ := s.store.GetSettings(ctx, req.user.ID)
		tasks, _ := s.store.LiveTasks(ctx, req.user.ID)
		for _, t := range tasks {
			if ev, ok := s.mirrorEvent(ctx, t, st, st.MirrorDoneCalDAV); ok {
				out = append(out, s.mirrorNode(req, l, t, ev))
			}
		}
		return out
	}
	tasks, _ := s.store.ListTasksInList(ctx, req.user.ID, l.ID)
	for _, t := range tasks {
		out = append(out, &davNode{kind: "resource", href: s.homeHref(req) + l.UID + "/" + t.Href, list: l, task: t, etag: t.ETag, ics: t.ICS, modified: t.UpdatedAt, req: req})
	}
	return out
}

func (s *Server) mirrorNode(req *davReq, l *store.List, t *store.Task, ev *ical.Component) *davNode {
	ics := ev.String()
	return &davNode{kind: "resource", href: s.homeHref(req) + mirrorUID + "/" + mirrorHref(t), list: l, task: t, etag: ical.ETag(ics), ics: ics, modified: t.UpdatedAt, req: req}
}

func (s *Server) davMkCalendar(ctx context.Context, w http.ResponseWriter, r *http.Request, req *davReq, seg string) {
	body, _ := readXML(r)
	l := &store.List{UserID: req.user.ID, UID: seg, Name: seg}
	if body != nil {
		if set := body.child("set"); set != nil {
			if prop := set.child("prop"); prop != nil {
				if dn := prop.child("displayname"); dn != nil && strings.TrimSpace(dn.Text) != "" {
					l.Name = strings.TrimSpace(dn.Text)
				}
				if c := prop.child("calendar-color"); c != nil {
					if col, err := cleanColor(strings.TrimSpace(c.Text)); err == nil {
						l.Color = col
					}
				}
				if d := prop.child("calendar-description"); d != nil {
					l.Description = strings.TrimSpace(d.Text)
				}
				if comp := prop.child("supported-calendar-component-set"); comp != nil {
					onlyEvents := len(comp.Nodes) > 0
					for _, c := range comp.Nodes {
						if strings.EqualFold(c.attr("name"), "VTODO") {
							onlyEvents = false
						}
					}
					if onlyEvents {
						http.Error(w, "this server keeps tasks (VTODO); events are the Tasks calendar", http.StatusForbidden)
						return
					}
				}
			}
		}
	}
	if err := s.store.CreateList(ctx, l); err != nil {
		s.fail(w, err, "create list")
		return
	}
	s.touch()
	go s.syncSoon(req.user.ID)
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) davProppatch(ctx context.Context, w http.ResponseWriter, r *http.Request, req *davReq, l *store.List, mirror bool, href string) {
	body, err := readXML(r)
	if err != nil || body == nil {
		http.Error(w, "bad xml", http.StatusBadRequest)
		return
	}
	var ok, failed []string
	changed := false
	for _, set := range body.children("set") {
		prop := set.child("prop")
		if prop == nil {
			continue
		}
		for _, p := range prop.Nodes {
			tag := `<x:` + p.XMLName.Local + ` xmlns:x="` + xesc(p.XMLName.Space) + `"/>`
			if mirror {
				failed = append(failed, tag)
				continue
			}
			switch p.XMLName.Space + " " + p.XMLName.Local {
			case nsDAV + " displayname":
				if v := strings.TrimSpace(p.Text); v != "" {
					l.Name, changed = v, true
				}
				ok = append(ok, tag)
			case nsApple + " calendar-color":
				if c, err := cleanColor(strings.TrimSpace(p.Text)); err == nil {
					l.Color, changed = c, true
				}
				ok = append(ok, tag)
			case nsApple + " calendar-order":
				if n, err := strconv.Atoi(strings.TrimSpace(p.Text)); err == nil {
					l.SortOrder = float64(n)
				}
				ok = append(ok, tag)
			case nsCalDAV + " calendar-description":
				l.Description, changed = strings.TrimSpace(p.Text), true
				ok = append(ok, tag)
			case nsOC + " calendar-enabled", nsCalDAV + " calendar-timezone", nsCalDAV + " schedule-calendar-transp":
				ok = append(ok, tag) // accepted, nothing kept
			default:
				failed = append(failed, tag)
			}
		}
	}
	if changed || len(ok) > 0 {
		if !mirror {
			if err := s.store.UpdateList(ctx, l); err != nil {
				s.fail(w, err, "update list")
				return
			}
			s.touch()
			go s.syncSoon(req.user.ID)
		}
	}
	ms := newMultistatus()
	ms.b.WriteString("<d:response><d:href>" + xesc(hrefEsc(href)) + "</d:href>")
	if len(ok) > 0 {
		ms.b.WriteString("<d:propstat><d:prop>" + strings.Join(ok, "") + "</d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat>")
	}
	if len(failed) > 0 {
		ms.b.WriteString("<d:propstat><d:prop>" + strings.Join(failed, "") + "</d:prop><d:status>HTTP/1.1 403 Forbidden</d:status></d:propstat>")
	}
	ms.b.WriteString("</d:response>\n")
	ms.write(w, "")
}

// ---------- REPORT ----------

func (s *Server) davReport(ctx context.Context, w http.ResponseWriter, r *http.Request, req *davReq, l *store.List, mirror bool, href string) {
	body, err := readXML(r)
	if err != nil || body == nil {
		http.Error(w, "bad xml", http.StatusBadRequest)
		return
	}
	want, all := requestedProps(body)
	uid := req.user.ID
	switch body.XMLName.Local {
	case "calendar-query":
		nodes := s.collectionNodes(ctx, req, l, mirror)
		filter := body.child("filter")
		ms := newMultistatus()
		loc := s.loc(ctx, uid)
		for _, n := range nodes {
			if filter != nil && !s.matchFilter(filter, n, mirror, loc) {
				continue
			}
			ms.response(n.href, s.props(ctx, n, want, all))
		}
		ms.write(w, "")
	case "calendar-multiget":
		nodes := s.collectionNodes(ctx, req, l, mirror)
		byHref := map[string]*davNode{}
		for _, n := range nodes {
			byHref[n.href] = n
		}
		ms := newMultistatus()
		for _, h := range body.children("href") {
			p := strings.TrimSpace(h.Text)
			if u, err := url.PathUnescape(p); err == nil {
				p = u
			}
			// hrefs may come under either prefix
			key := p
			for _, pf := range []string{"/remote.php/dav", "/dav"} {
				if strings.HasPrefix(p, pf+"/") {
					key = req.prefix + strings.TrimPrefix(p, pf)
					break
				}
			}
			if n := byHref[key]; n != nil {
				ms.response(p, s.props(ctx, n, want, all))
			} else {
				ms.gone(p)
			}
		}
		ms.write(w, "")
	case "sync-collection":
		token := strings.TrimSpace(body.child("sync-token").textOr(""))
		since := int64(0)
		if token != "" {
			n, err := strconv.ParseInt(strings.TrimPrefix(token, syncPfx), 10, 64)
			if err != nil {
				w.Header().Set("Content-Type", "application/xml; charset=utf-8")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?><d:error xmlns:d="DAV:"><d:valid-sync-token/></d:error>`))
				return
			}
			since = n
		}
		if since > 0 {
			if oldest := s.store.OldestTombstoneSeq(ctx, uid); oldest > 0 && since < oldest-1 {
				// older than what we remember: ask for a full sync
				since = 0
			}
		}
		var changed []*store.Task
		var gone []string
		if mirror {
			changed, gone, err = s.store.ChangedSinceUser(ctx, uid, since)
		} else {
			changed, gone, err = s.store.ChangedSince(ctx, uid, l.ID, since)
		}
		if err != nil {
			s.fail(w, err, "sync")
			return
		}
		ms := newMultistatus()
		seen := map[string]bool{}
		st, _ := s.store.GetSettings(ctx, uid)
		for _, t := range changed {
			if mirror {
				h := s.homeHref(req) + mirrorUID + "/" + mirrorHref(t)
				if t.DeletedAt != "" {
					ms.gone(h)
					continue
				}
				if ev, ok := s.mirrorEvent(ctx, t, st, st.MirrorDoneCalDAV); ok {
					n := s.mirrorNode(req, l, t, ev)
					ms.response(n.href, s.props(ctx, n, want, all))
				} else {
					ms.gone(h)
				}
				seen[mirrorHref(t)] = true
				continue
			}
			h := s.homeHref(req) + l.UID + "/" + t.Href
			if t.DeletedAt != "" {
				ms.gone(h)
			} else {
				ms.response(h, s.props(ctx, &davNode{kind: "resource", href: h, list: l, task: t, etag: t.ETag, ics: t.ICS, modified: t.UpdatedAt, req: req}, want, all))
			}
			seen[t.Href] = true
		}
		for _, h := range gone {
			if mirror {
				h = "mirror-" + strings.TrimSuffix(h, ".ics") + ".ics"
			}
			if seen[h] {
				continue
			}
			ms.gone(s.homeHref(req) + l.UID + "/" + h)
		}
		newSeq := l.Seq
		if mirror {
			newSeq = s.store.MaxSeq(ctx, uid)
		}
		if since > newSeq {
			newSeq = since
		}
		ms.write(w, "<d:sync-token>"+xesc(syncPfx+strconv.FormatInt(newSeq, 10))+"</d:sync-token>\n")
	default:
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?><d:error xmlns:d="DAV:"><d:supported-report/></d:error>`))
	}
}

func (n *xnode) textOr(def string) string {
	if n == nil {
		return def
	}
	return n.Text
}

// matchFilter evaluates a calendar-query filter against a node.
func (s *Server) matchFilter(filter *xnode, n *davNode, mirror bool, loc *time.Location) bool {
	cal := filter.child("comp-filter")
	if cal == nil {
		return true
	}
	inner := cal.children("comp-filter")
	if len(inner) == 0 {
		return true
	}
	comp := "VTODO"
	if mirror {
		comp = "VEVENT"
	}
	for _, cf := range inner {
		if !strings.EqualFold(cf.attr("name"), comp) {
			continue
		}
		if cf.child("is-not-defined") != nil {
			return false
		}
		if tr := cf.child("time-range"); tr != nil && !s.inRange(n, tr, loc) {
			return false
		}
		for _, pf := range cf.children("prop-filter") {
			if !s.matchProp(pf, n, loc) {
				return false
			}
		}
		return true
	}
	return false // asked for another component
}

func parseRangeTime(v string) (time.Time, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, false
	}
	t, err := time.Parse("20060102T150405Z", v)
	return t, err == nil
}

func (s *Server) inRange(n *davNode, tr *xnode, loc *time.Location) bool {
	start, hasStart := parseRangeTime(tr.attr("start"))
	end, hasEnd := parseRangeTime(tr.attr("end"))
	t := n.task
	st, du := t.StartStr(), t.DueStr()
	if st == "" && du == "" {
		// a task with no dates matches any range unless completed long ago
		return true
	}
	within := func(v string) bool {
		tm, ok := ical.ParseLocal(v, loc)
		if !ok {
			return false
		}
		if ical.IsDay(v) {
			if hasEnd && !tm.Before(end) {
				return false
			}
			if hasStart && tm.Add(24*time.Hour).Before(start) {
				return false
			}
			return true
		}
		if hasEnd && !tm.Before(end) {
			return false
		}
		if hasStart && tm.Before(start) {
			return false
		}
		return true
	}
	// overlap of [start, due]
	lo, hi := st, du
	if lo == "" {
		lo = hi
	}
	if hi == "" {
		hi = lo
	}
	loT, _ := ical.ParseLocal(lo, loc)
	hiT, _ := ical.ParseLocal(hi, loc)
	if ical.IsDay(hi) {
		hiT = hiT.Add(24 * time.Hour)
	}
	if hasStart && hiT.Before(start) {
		return false
	}
	if hasEnd && !loT.Before(end) {
		return false
	}
	_ = within
	return true
}

func (s *Server) matchProp(pf *xnode, n *davNode, loc *time.Location) bool {
	name := strings.ToUpper(pf.attr("name"))
	t := n.task
	var value string
	var present bool
	switch name {
	case "COMPLETED":
		present = t.CompletedAt != nil
		if present {
			value = *t.CompletedAt
		}
	case "STATUS":
		present = true
		value = map[string]string{ical.StatusOpen: "NEEDS-ACTION", ical.StatusInProgress: "IN-PROCESS", ical.StatusDone: "COMPLETED", ical.StatusCancelled: "CANCELLED"}[t.Status]
	case "DUE":
		present = t.Due != nil
		value = t.DueStr()
	case "DTSTART":
		present = t.Start != nil
		value = t.StartStr()
	case "SUMMARY":
		present = true
		value = t.Title
	case "UID":
		present = true
		value = t.UID
	case "CATEGORIES":
		present = len(t.Tags) > 0
		value = strings.Join(t.Tags, ",")
	default:
		present = true
	}
	if pf.child("is-not-defined") != nil {
		return !present
	}
	if !present {
		return false
	}
	if tr := pf.child("time-range"); tr != nil {
		start, hasStart := parseRangeTime(tr.attr("start"))
		end, hasEnd := parseRangeTime(tr.attr("end"))
		var tm time.Time
		if name == "COMPLETED" {
			tm = store.ParseTime(value)
		} else {
			tm, _ = ical.ParseLocal(value, loc)
		}
		if hasStart && tm.Before(start) {
			return false
		}
		if hasEnd && !tm.Before(end) {
			return false
		}
	}
	if tm := pf.child("text-match"); tm != nil {
		neg := strings.EqualFold(tm.attr("negate-condition"), "yes")
		hit := strings.Contains(strings.ToLower(value), strings.ToLower(strings.TrimSpace(tm.Text)))
		if hit == neg {
			return false
		}
	}
	return true
}

// ---------- resources ----------

func (s *Server) davResource(ctx context.Context, w http.ResponseWriter, r *http.Request, req *davReq, seg, name string) {
	uid := req.user.ID
	l, mirror, err := s.resolveCollection(ctx, req, seg)
	if err != nil {
		s.davNotFound(w)
		return
	}
	href := s.homeHref(req) + seg + "/" + name
	if mirror {
		s.davMirrorResource(ctx, w, r, req, l, name, href)
		return
	}
	t, err := s.store.GetTaskByHref(ctx, uid, l.ID, name)
	exists := err == nil
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		if !exists {
			s.davNotFound(w)
			return
		}
		w.Header().Set("ETag", t.ETag)
		w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(t.ICS)))
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write([]byte(t.ICS))
	case "PROPFIND":
		if !exists {
			s.davNotFound(w)
			return
		}
		body, err := readXML(r)
		if err != nil {
			http.Error(w, "bad xml", http.StatusBadRequest)
			return
		}
		want, all := requestedProps(body)
		ms := newMultistatus()
		ms.response(href, s.props(ctx, &davNode{kind: "resource", href: href, list: l, task: t, etag: t.ETag, ics: t.ICS, modified: t.UpdatedAt, req: req}, want, all))
		ms.write(w, "")
	case http.MethodPut:
		if r.Header.Get("If-None-Match") == "*" && exists {
			http.Error(w, "already there", http.StatusPreconditionFailed)
			return
		}
		if im := r.Header.Get("If-Match"); im != "" && im != "*" {
			if !exists || !etagMatches(im, t.ETag) {
				http.Error(w, "changed meanwhile", http.StatusPreconditionFailed)
				return
			}
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
		if err != nil {
			http.Error(w, "could not read", http.StatusBadRequest)
			return
		}
		cal, err := ical.Parse(string(body))
		if err != nil {
			http.Error(w, "not iCalendar", http.StatusBadRequest)
			return
		}
		loc := s.loc(ctx, uid)
		it, ok := ical.FromVTODO(cal, loc)
		if !ok {
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?><d:error xmlns:d="DAV:" xmlns:cal="urn:ietf:params:xml:ns:caldav"><cal:supported-calendar-component/></d:error>`))
			return
		}
		if it.UID == "" {
			it.UID = strings.TrimSuffix(name, ".ics")
			if todo := cal.Child("VTODO"); todo != nil {
				todo.SetText("UID", it.UID)
			}
		}
		if !exists {
			// the same UID may already live here under another name (a client renamed)
			if other, err := s.store.GetTaskByUID(ctx, uid, it.UID); err == nil && other.ListID == l.ID {
				t, exists = other, true
				t.Href = name
			} else {
				t = &store.Task{UserID: uid, ListID: l.ID, Href: name, Tags: []string{}, Alarms: []ical.Alarm{}}
			}
		}
		wasDone := exists && t.Status == ical.StatusDone
		t.ListID = l.ID
		t.ICS = cal.String()
		t.ETag = ical.ETag(t.ICS)
		s.fromICal(ctx, it, t)
		if t.Title == "" {
			t.Title = "Untitled"
		}
		kind := "updated"
		if !exists {
			kind = "created"
			err = s.store.InsertTask(ctx, t)
		} else {
			if !wasDone && t.Status == ical.StatusDone {
				kind = "completed"
			} else if wasDone && t.Status != ical.StatusDone {
				kind = "reopened"
			}
			err = s.store.UpdateTask(ctx, t)
		}
		if err != nil {
			s.fail(w, err, "store task")
			return
		}
		s.scheduleAlarms(ctx, t)
		s.touch()
		s.store.AddActivity(ctx, uid, t.ID, kind, "caldav")
		s.publish(uid, "tasks."+map[string]string{"created": "created", "updated": "updated", "completed": "completed", "reopened": "updated"}[kind], fmt.Sprintf("task:%d", t.ID), map[string]any{"id": t.ID, "title": t.Title, "list_id": t.ListID, "status": t.Status, "due": t.DueStr(), "start": t.StartStr(), "source": "caldav"})
		go s.syncSoon(uid)
		w.Header().Set("ETag", t.ETag)
		if exists && kind != "created" {
			w.WriteHeader(http.StatusNoContent)
		} else {
			w.WriteHeader(http.StatusCreated)
		}
	case http.MethodDelete:
		if !exists {
			s.davNotFound(w)
			return
		}
		if im := r.Header.Get("If-Match"); im != "" && im != "*" && !etagMatches(im, t.ETag) {
			http.Error(w, "changed meanwhile", http.StatusPreconditionFailed)
			return
		}
		if err := s.store.SoftDeleteTask(ctx, uid, t.ID); err != nil {
			s.fail(w, err, "delete task")
			return
		}
		s.touch()
		s.store.AddActivity(ctx, uid, t.ID, "deleted", "caldav")
		s.publish(uid, "tasks.deleted", fmt.Sprintf("task:%d", t.ID), map[string]any{"id": t.ID, "title": t.Title, "list_id": t.ListID})
		go s.syncSoon(uid)
		w.WriteHeader(http.StatusNoContent)
	default:
		s.davMethodNotAllowed(w)
	}
}

func etagMatches(header, etag string) bool {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		part = strings.TrimPrefix(part, "W/")
		if part == etag || part == strings.Trim(etag, `"`) || `"`+part+`"` == etag {
			return true
		}
	}
	return false
}

// ---------- the Tasks calendar (mirror) ----------

func mirrorHref(t *store.Task) string { return "mirror-" + t.UID + ".ics" }

func taskUIDFromMirror(name string) string {
	name = strings.TrimSuffix(name, ".ics")
	return strings.TrimPrefix(name, "mirror-")
}

// mirrorEvent builds the VEVENT of a task for the Tasks calendar, or false
// when the task has no place on a calendar.
func (s *Server) mirrorEvent(ctx context.Context, t *store.Task, st store.Settings, includeDone bool) (*ical.Component, bool) {
	if t.DeletedAt != "" {
		return nil, false
	}
	loc := s.loc(ctx, t.UserID)
	tz := loc.String()
	now := time.Now().In(loc)
	today := now.Format(ical.DayLayout)
	var when string
	summary := t.Title
	switch {
	case t.Status == ical.StatusDone:
		if !includeDone || t.CompletedAt == nil {
			return nil, false
		}
		when = store.ParseTime(*t.CompletedAt).In(loc).Format(ical.DayLayout)
		summary = "✓ " + summary
	case t.Status == ical.StatusCancelled:
		return nil, false
	case t.StartStr() != "":
		when = t.StartStr()
	case t.DueStr() != "" && st.MirrorIncludeDue:
		when = t.DueStr()
	default:
		return nil, false
	}
	if t.Open() && st.MirrorRollover && ical.Day(when) < today {
		// overdue: show today, keep the time of day when there was one
		if ical.IsDay(when) {
			when = today
		} else {
			when = today + when[10:]
		}
		summary = "⚠︎ " + summary
	}
	if t.Open() && t.DueStr() != "" && ical.Day(t.DueStr()) < today && !strings.HasPrefix(summary, "⚠︎") {
		summary = "⚠︎ " + summary
	}
	cal := ical.NewCalendar()
	ev := &ical.Component{Name: "VEVENT"}
	ev.SetText("UID", "mirror-"+t.UID)
	ev.Set(ical.UTCProperty("DTSTAMP", time.Now()))
	ev.Set(ical.UTCProperty("LAST-MODIFIED", store.ParseTime(t.UpdatedAt)))
	ev.Set(ical.UTCProperty("CREATED", store.ParseTime(t.CreatedAt)))
	ev.SetText("SUMMARY", summary)
	if ical.IsDay(when) {
		ev.Set(ical.DateProperty("DTSTART", when))
		d, _ := ical.ParseLocal(when, loc)
		ev.Set(ical.DateProperty("DTEND", d.AddDate(0, 0, 1).Format(ical.DayLayout)))
	} else {
		ev.Set(ical.ZonedProperty("DTSTART", when, tz))
		mins := st.DefaultEstimateMin
		if t.EstimateMin != nil && *t.EstimateMin > 0 {
			mins = *t.EstimateMin
		}
		if mins <= 0 {
			mins = 30
		}
		d, _ := ical.ParseLocal(when, loc)
		ev.Set(ical.ZonedProperty("DTEND", ical.FormatLocal(d.Add(time.Duration(mins)*time.Minute), false), tz))
	}
	desc := t.Notes
	if t.DueStr() != "" && t.StartStr() != "" && t.StartStr() != t.DueStr() {
		desc = "Deadline: " + humanWhen(t.DueStr(), loc) + "\n\n" + desc
	}
	if t.List != nil {
		desc = strings.TrimSpace(desc + "\n\nList: " + t.List.Name)
	}
	if desc != "" {
		ev.SetText("DESCRIPTION", strings.TrimSpace(desc))
	}
	if t.Location != "" {
		ev.SetText("LOCATION", t.Location)
	}
	if len(t.Tags) > 0 {
		ev.SetRaw("CATEGORIES", strings.Join(t.Tags, ","))
	}
	if t.Status == ical.StatusDone {
		ev.SetRaw("STATUS", "CONFIRMED")
		ev.SetRaw("TRANSP", "TRANSPARENT")
	} else {
		ev.SetRaw("TRANSP", "TRANSPARENT")
	}
	if s.cfg.BaseURL != "" {
		ev.SetRaw("URL", fmt.Sprintf("%s/task/%d", s.cfg.BaseURL, t.ID))
	}
	ev.SetRaw("X-POCKET-TASK", strconv.FormatInt(t.ID, 10))
	ev.SetRaw("SEQUENCE", strconv.FormatInt(t.Seq%100000, 10))
	cal.Children = append(cal.Children, ev)
	return cal, true
}

func (s *Server) davMirrorResource(ctx context.Context, w http.ResponseWriter, r *http.Request, req *davReq, l *store.List, name, href string) {
	uid := req.user.ID
	st, _ := s.store.GetSettings(ctx, uid)
	t, err := s.store.GetTaskByUID(ctx, uid, taskUIDFromMirror(name))
	var ev *ical.Component
	exists := false
	if err == nil {
		ev, exists = s.mirrorEvent(ctx, t, st, st.MirrorDoneCalDAV)
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		if !exists {
			s.davNotFound(w)
			return
		}
		ics := ev.String()
		w.Header().Set("ETag", ical.ETag(ics))
		w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write([]byte(ics))
	case "PROPFIND":
		if !exists {
			s.davNotFound(w)
			return
		}
		body, err := readXML(r)
		if err != nil {
			http.Error(w, "bad xml", http.StatusBadRequest)
			return
		}
		want, all := requestedProps(body)
		ms := newMultistatus()
		n := s.mirrorNode(req, l, t, ev)
		ms.response(n.href, s.props(ctx, n, want, all))
		ms.write(w, "")
	case http.MethodPut:
		body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		if err != nil {
			http.Error(w, "could not read", http.StatusBadRequest)
			return
		}
		cal, err := ical.Parse(string(body))
		if err != nil || cal.Child("VEVENT") == nil {
			http.Error(w, "not an event", http.StatusBadRequest)
			return
		}
		in := cal.Child("VEVENT")
		loc := s.loc(ctx, uid)
		start, allDay := ical.ReadDateTime(in.Get("DTSTART"), loc)
		end, _ := ical.ReadDateTime(in.Get("DTEND"), loc)
		if end == "" {
			if p := in.Get("DURATION"); p != nil && start != "" {
				if d, err := ical.Duration(p.Value); err == nil {
					if sT, ok := ical.ParseLocal(start, loc); ok {
						end = ical.FormatLocal(sT.Add(d), allDay)
					}
				}
			}
		}
		title := strings.TrimSpace(in.Text("SUMMARY"))
		for _, pfx := range []string{"⚠︎ ", "✓ "} {
			title = strings.TrimPrefix(title, pfx)
		}
		if t == nil {
			// a new event in the Tasks calendar becomes a task
			t = &store.Task{UserID: uid, ListID: s.defaultList(ctx, uid, st), Status: ical.StatusOpen, Tags: []string{}, Alarms: []ical.Alarm{}, UID: taskUIDFromMirror(name)}
			if strings.HasPrefix(t.UID, "mirror-") || t.UID == "" {
				t.UID = ""
			}
			t.Title = firstNonEmpty(title, "New task")
			t.Notes = strings.TrimSpace(in.Text("DESCRIPTION"))
			t.Location = strings.TrimSpace(in.Text("LOCATION"))
		} else if title != "" && title != t.Title && !strings.Contains(t.Title, title) {
			t.Title = title
		}
		if start != "" {
			t.Start = &start
			if t.Status == ical.StatusDone {
				// moving a finished task's event does nothing to it
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if !allDay && end != "" {
				sT, _ := ical.ParseLocal(start, loc)
				eT, _ := ical.ParseLocal(end, loc)
				if mins := int(eT.Sub(sT).Minutes()); mins > 0 && mins <= 24*60 {
					t.EstimateMin = &mins
				}
			}
			// a task that only had a deadline, moved on the calendar: keep the deadline, add a start
		}
		if err := s.saveTask(ctx, t, "calendar", "updated"); err != nil {
			s.fail(w, err, "store task")
			return
		}
		t, _ = s.store.GetTask(ctx, uid, t.ID)
		if ev, ok := s.mirrorEvent(ctx, t, st, st.MirrorDoneCalDAV); ok {
			w.Header().Set("ETag", ical.ETag(ev.String()))
		}
		if exists {
			w.WriteHeader(http.StatusNoContent)
		} else {
			w.WriteHeader(http.StatusCreated)
		}
	case http.MethodDelete:
		if !exists {
			s.davNotFound(w)
			return
		}
		// taking the event off the calendar unschedules the task, it does not delete it
		if t.Open() {
			t.Start = nil
			if err := s.saveTask(ctx, t, "calendar", "updated"); err != nil {
				s.fail(w, err, "store task")
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		s.davMethodNotAllowed(w)
	}
}

// ---------- the feed ----------

func (s *Server) handleFeed(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tok := strings.TrimSuffix(pathLast(r.URL.Path), ".ics")
	u, err := s.store.GetUserByFeedToken(ctx, tok)
	if err != nil {
		http.Error(w, "no such feed", http.StatusNotFound)
		return
	}
	st, _ := s.store.GetSettings(ctx, u.ID)
	includeDone := st.MirrorDone
	if r.URL.Query().Has("done") {
		includeDone = queryBool(r, "done")
	}
	days := queryInt(r, "days")
	if days <= 0 {
		days = 90
	}
	tasks, err := s.store.LiveTasks(ctx, u.ID)
	if err != nil {
		s.fail(w, err, "feed")
		return
	}
	cal := ical.NewCalendar()
	cal.SetText("X-WR-CALNAME", "Tasks")
	cal.SetRaw("X-WR-TIMEZONE", s.loc(ctx, u.ID).String())
	cal.SetRaw("REFRESH-INTERVAL;VALUE=DURATION", "PT15M")
	cal.SetRaw("X-PUBLISHED-TTL", "PT15M")
	cutoff := time.Now().AddDate(0, 0, -days)
	for _, t := range tasks {
		if t.Status == ical.StatusDone && t.CompletedAt != nil && store.ParseTime(*t.CompletedAt).Before(cutoff) {
			continue
		}
		if ev, ok := s.mirrorEvent(ctx, t, st, includeDone); ok {
			cal.Children = append(cal.Children, ev.Child("VEVENT"))
		}
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=300")
	_, _ = w.Write([]byte(cal.String()))
}

func pathLast(p string) string { return path.Base(p) }
