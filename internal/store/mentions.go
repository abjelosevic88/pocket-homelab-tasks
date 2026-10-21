package store

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Tasks about things in other apps: a contact, a book, a trip is its page's
// address, and a task is about it when its url field or its notes link there.
// GET /api/v1/tasks?url=… (Core link scope tasks:read) answers those, so
// Pocket Contacts can show a person's open to-dos. The rule is the one Pocket
// Notes uses for its mentions (internal/server/mentions.go there).

var linkURLRe = regexp.MustCompile(`https?://[^\s<>()\[\]"'` + "`" + `]+`)

// ItemKey is an address as compared here: host in lower case, then path,
// then query; no scheme, fragment or trailing slash ("" if it is no web address).
func ItemKey(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	k := strings.ToLower(u.Host) + strings.TrimRight(u.EscapedPath(), "/")
	if u.RawQuery != "" {
		k += "?" + u.RawQuery
	}
	return k
}

// LinksTo reports whether text links to one of the items: the same address,
// or one below it or with a query of its own (/contacts/12/edit, /contacts/12?tab=notes),
// never a sibling that merely starts the same (/contacts/123).
func LinksTo(text string, keys []string) bool {
	for _, m := range linkURLRe.FindAllString(text, -1) {
		k := ItemKey(strings.TrimRight(m, ".,;:!?"))
		if k == "" {
			continue
		}
		for _, want := range keys {
			if k == want || strings.HasPrefix(k, want+"?") || strings.HasPrefix(k, want+"/") {
				return true
			}
		}
	}
	return false
}

// linkedIDs narrows a task query to the tasks that link to one of the keys:
// SQL finds the candidates whose url or notes contain the host and path (LIKE
// folds ASCII case, as hosts are compared), LinksTo makes the exact match.
func (s *Store) linkedIDs(ctx context.Context, where string, args []any, keys []string) ([]int64, error) {
	var likes []string
	for _, k := range keys {
		needle := "%" + likeEscape(strings.SplitN(k, "?", 2)[0]) + "%"
		likes = append(likes, `t.url LIKE ? ESCAPE '\'`, `t.notes LIKE ? ESCAPE '\'`)
		args = append(args, needle, needle)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT t.id, t.url, t.notes FROM tasks t JOIN lists l ON l.id = t.list_id `+
		where+` AND (`+strings.Join(likes, " OR ")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		var link, notes string
		if err := rows.Scan(&id, &link, &notes); err != nil {
			return nil, err
		}
		if LinksTo(link+"\n"+notes, keys) {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}

// idList renders ids for an IN (…) clause; "0" when there are none (no task has id 0).
func idList(ids []int64) string {
	if len(ids) == 0 {
		return "0"
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = fmt.Sprint(id)
	}
	return strings.Join(out, ",")
}
