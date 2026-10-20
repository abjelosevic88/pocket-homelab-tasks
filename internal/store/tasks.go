package store

import (
	"context"
	"database/sql"
	"fmt"
	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-tasks/internal/ical"
)

const taskCols = `t.id, t.user_id, t.list_id, t.section_id, t.parent_id, t.uid, t.href, t.etag, t.ics, t.title, t.notes, t.status, t.completed_at, t.percent, t.priority,
	t.start, t.due, t.estimate_min, t.rrule, t.repeat_from, t.someday, t.sort_order, t.location, t.url, t.seq, t.nc_href, t.nc_etag, t.nc_seq, t.created_at, t.updated_at, t.deleted_at,
	(SELECT COUNT(1) FROM tasks c WHERE c.parent_id = t.id AND c.deleted_at = ''),
	(SELECT COUNT(1) FROM tasks c WHERE c.parent_id = t.id AND c.deleted_at = '' AND c.status IN ('done','cancelled')),
	l.name, l.color`

func scanTask(row interface{ Scan(...any) error }) (*Task, error) {
	var t Task
	var section, parent sql.NullInt64
	var completed, start, due, repeatFrom, rrule string
	var someday, estimate int
	var listName, listColor string
	if err := row.Scan(&t.ID, &t.UserID, &t.ListID, &section, &parent, &t.UID, &t.Href, &t.ETag, &t.ICS, &t.Title, &t.Notes, &t.Status, &completed, &t.Percent, &t.Priority,
		&start, &due, &estimate, &rrule, &repeatFrom, &someday, &t.SortOrder, &t.Location, &t.URL, &t.Seq, &t.NCHref, &t.NCETag, &t.NCSeq, &t.CreatedAt, &t.UpdatedAt, &t.DeletedAt,
		&t.SubtaskCount, &t.SubtaskDone, &listName, &listColor); err != nil {
		return nil, notFound(err)
	}
	if section.Valid {
		v := section.Int64
		t.SectionID = &v
	}
	if parent.Valid {
		v := parent.Int64
		t.ParentID = &v
	}
	if completed != "" {
		t.CompletedAt = &completed
	}
	if start != "" {
		t.Start = &start
	}
	if due != "" {
		t.Due = &due
	}
	if estimate > 0 {
		t.EstimateMin = &estimate
	}
	if rrule != "" {
		from := repeatFrom
		if from == "" {
			from = "due"
		}
		t.Repeat = &Repeat{RRule: rrule, From: from}
	}
	t.Someday = someday == 1
	t.Tags, t.Alarms, t.Attachments = []string{}, []ical.Alarm{}, []Attachment{}
	t.List = &ListRef{ID: t.ListID, Name: listName, Color: listColor}
	return &t, nil
}

func (s *Store) queryTasks(ctx context.Context, where string, args ...any) ([]*Task, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+taskCols+` FROM tasks t JOIN lists l ON l.id = t.list_id `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, s.hydrate(ctx, out)
}

// hydrate fills tags, alarms and attachments for a batch.
func (s *Store) hydrate(ctx context.Context, tasks []*Task) error {
	if len(tasks) == 0 {
		return nil
	}
	byID := map[int64]*Task{}
	ids := make([]string, 0, len(tasks))
	for _, t := range tasks {
		byID[t.ID] = t
		ids = append(ids, fmt.Sprint(t.ID))
	}
	in := "(" + strings.Join(ids, ",") + ")"
	rows, err := s.DB.QueryContext(ctx, `SELECT task_id, tag FROM task_tags WHERE task_id IN `+in+` ORDER BY tag`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var tag string
		if rows.Scan(&id, &tag) == nil {
			byID[id].Tags = append(byID[id].Tags, tag)
		}
	}
	rows.Close()
	rows, err = s.DB.QueryContext(ctx, `SELECT task_id, trigger, description FROM alarms WHERE task_id IN `+in+` ORDER BY id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var a ical.Alarm
		if rows.Scan(&id, &a.Trigger, &a.Description) == nil {
			byID[id].Alarms = append(byID[id].Alarms, a)
		}
	}
	rows.Close()
	rows, err = s.DB.QueryContext(ctx, `SELECT id, user_id, task_id, name, size, type, path, created_at FROM attachments WHERE task_id IN `+in+` ORDER BY id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var a Attachment
		if rows.Scan(&a.ID, &a.UserID, &a.TaskID, &a.Name, &a.Size, &a.Type, &a.Path, &a.CreatedAt) == nil {
			a.URL = fmt.Sprintf("/api/v1/tasks/%d/attachments/%d", a.TaskID, a.ID)
			byID[a.TaskID].Attachments = append(byID[a.TaskID].Attachments, a)
		}
	}
	rows.Close()
	return nil
}

// GetTask returns one task of the person (deleted ones included).
func (s *Store) GetTask(ctx context.Context, uid, id int64) (*Task, error) {
	ts, err := s.queryTasks(ctx, `WHERE t.id = ? AND t.user_id = ?`, id, uid)
	if err != nil {
		return nil, err
	}
	if len(ts) == 0 {
		return nil, ErrNotFound
	}
	return ts[0], nil
}

// GetTaskByHref returns the task stored under a CalDAV resource name.
func (s *Store) GetTaskByHref(ctx context.Context, uid, listID int64, href string) (*Task, error) {
	ts, err := s.queryTasks(ctx, `WHERE t.user_id = ? AND t.list_id = ? AND t.href = ? AND t.deleted_at = ''`, uid, listID, href)
	if err != nil {
		return nil, err
	}
	if len(ts) == 0 {
		return nil, ErrNotFound
	}
	return ts[0], nil
}

// GetTaskByUID returns the live task with that iCalendar UID.
func (s *Store) GetTaskByUID(ctx context.Context, uid int64, tuid string) (*Task, error) {
	ts, err := s.queryTasks(ctx, `WHERE t.user_id = ? AND t.uid = ? AND t.deleted_at = '' ORDER BY t.id LIMIT 1`, uid, tuid)
	if err != nil {
		return nil, err
	}
	if len(ts) == 0 {
		return nil, ErrNotFound
	}
	return ts[0], nil
}

// GetTaskByNCHref returns the task mirrored from a Nextcloud resource.
func (s *Store) GetTaskByNCHref(ctx context.Context, uid int64, href string) (*Task, error) {
	ts, err := s.queryTasks(ctx, `WHERE t.user_id = ? AND t.nc_href = ? ORDER BY t.deleted_at = '' DESC, t.id LIMIT 1`, uid, href)
	if err != nil {
		return nil, err
	}
	if len(ts) == 0 {
		return nil, ErrNotFound
	}
	return ts[0], nil
}

// Subtasks returns the live children of a task in order.
func (s *Store) Subtasks(ctx context.Context, uid, parentID int64) ([]*Task, error) {
	return s.queryTasks(ctx, `WHERE t.user_id = ? AND t.parent_id = ? AND t.deleted_at = '' ORDER BY t.status IN ('done','cancelled'), t.sort_order, t.id`, uid, parentID)
}

// ListTasks runs a query. The caller sets q.Today / q.Now in the person's zone.
func (s *Store) ListTasks(ctx context.Context, uid int64, q Query, inboxID int64) ([]*Task, int, error) {
	where := []string{"t.user_id = ?"}
	args := []any{uid}
	if q.Deleted {
		where = append(where, "t.deleted_at <> ''")
	} else {
		where = append(where, "t.deleted_at = ''")
	}
	open := "t.status IN ('open','in_progress')"
	done := "t.status IN ('done','cancelled')"
	dated := "(t.start <> '' OR t.due <> '')"
	status := q.Status
	order := "t.sort_order, t.id"
	switch q.View {
	case "inbox":
		where = append(where, "t.list_id = ?", "t.parent_id IS NULL")
		args = append(args, inboxID)
		if status == "" {
			status = "open"
		}
	case "today":
		where = append(where, open, "t.someday = 0", "((t.start <> '' AND substr(t.start,1,10) <= ?) OR (t.due <> '' AND substr(t.due,1,10) <= ?))")
		args = append(args, q.Today, q.Today)
		status = "all"
		order = "CASE WHEN t.due <> '' AND substr(t.due,1,10) < ? THEN 0 ELSE 1 END, l.is_inbox DESC, l.sort_order, t.sort_order, t.id"
		args = append(args, q.Today)
	case "upcoming":
		where = append(where, open, "t.someday = 0", dated, "NOT ((t.start <> '' AND substr(t.start,1,10) <= ?) OR (t.due <> '' AND substr(t.due,1,10) <= ?))")
		args = append(args, q.Today, q.Today)
		status = "all"
		order = "CASE WHEN t.start <> '' AND t.due <> '' THEN MIN(t.start, t.due) WHEN t.start <> '' THEN t.start ELSE t.due END, t.sort_order, t.id"
	case "anytime":
		where = append(where, open, "t.someday = 0", "t.start = ''", "t.list_id <> ?", "t.parent_id IS NULL")
		args = append(args, inboxID)
		status = "all"
		order = "l.sort_order, l.id, t.sort_order, t.id"
	case "someday":
		where = append(where, open, "t.someday = 1")
		status = "all"
		order = "l.is_inbox DESC, l.sort_order, l.id, t.sort_order, t.id"
	case "logbook":
		where = append(where, done)
		status = "all"
		order = "t.completed_at DESC, t.id DESC"
	case "all", "":
	}
	switch status {
	case "open":
		where = append(where, open)
	case "done":
		where = append(where, done)
	}
	if q.ListID > 0 {
		where = append(where, "t.list_id = ?")
		args = append(args, q.ListID)
		if q.View == "" {
			order = "t.sort_order, t.id"
		}
	}
	if q.SectionID != nil {
		if *q.SectionID == 0 {
			where = append(where, "t.section_id IS NULL")
		} else {
			where = append(where, "t.section_id = ?")
			args = append(args, *q.SectionID)
		}
	}
	if q.ParentID != nil {
		if *q.ParentID == 0 {
			where = append(where, "t.parent_id IS NULL")
		} else {
			where = append(where, "t.parent_id = ?")
			args = append(args, *q.ParentID)
		}
	}
	tags := q.Tags
	if q.Tag != "" {
		tags = append(tags, q.Tag)
	}
	for _, tag := range tags {
		where = append(where, "EXISTS (SELECT 1 FROM task_tags x WHERE x.task_id = t.id AND x.tag = ? COLLATE NOCASE)")
		args = append(args, tag)
	}
	if q.Priority != nil {
		switch *q.Priority {
		case 1:
			where = append(where, "t.priority BETWEEN 1 AND 4")
		case 5:
			where = append(where, "t.priority = 5")
		case 9:
			where = append(where, "t.priority BETWEEN 6 AND 9")
		case 0:
			where = append(where, "t.priority = 0")
		}
	}
	if q.DueFrom != "" {
		where = append(where, "t.due <> '' AND substr(t.due,1,10) >= ?")
		args = append(args, q.DueFrom)
	}
	if q.DueTo != "" {
		where = append(where, "t.due <> '' AND substr(t.due,1,10) <= ?")
		args = append(args, q.DueTo)
	}
	if q.StartFrom != "" {
		where = append(where, "t.start <> '' AND substr(t.start,1,10) >= ?")
		args = append(args, q.StartFrom)
	}
	if q.StartTo != "" {
		where = append(where, "t.start <> '' AND substr(t.start,1,10) <= ?")
		args = append(args, q.StartTo)
	}
	if q.Someday != nil {
		where = append(where, "t.someday = ?")
		args = append(args, boolInt(*q.Someday))
	}
	if q.HasDate != nil {
		if *q.HasDate {
			where = append(where, dated)
		} else {
			where = append(where, "t.start = '' AND t.due = ''")
		}
	}
	if q.CompletedOn != "" {
		where = append(where, done, "t.completed_at <> ''")
		if q.Status == "" {
			status = "all"
		}
		// completed_at is UTC; the caller gives the local day's UTC bounds in CompletedOn as "from|to"
		from, to, ok := strings.Cut(q.CompletedOn, "|")
		if ok {
			where = append(where, "t.completed_at >= ? AND t.completed_at < ?")
			args = append(args, from, to)
		} else {
			where = append(where, "substr(t.completed_at,1,10) = ?")
			args = append(args, q.CompletedOn)
		}
	}
	if q.ModifiedAt != "" {
		where = append(where, "t.updated_at > ?")
		args = append(args, q.ModifiedAt)
	}
	if len(q.IDs) > 0 {
		ids := make([]string, len(q.IDs))
		for i, id := range q.IDs {
			ids[i] = fmt.Sprint(id)
		}
		where = append(where, "t.id IN ("+strings.Join(ids, ",")+")")
	}
	if q.Q != "" {
		m := ftsQuery(q.Q)
		if m != "" {
			where = append(where, "t.id IN (SELECT rowid FROM tasks_fts WHERE tasks_fts MATCH ?)")
			args = append(args, m)
		}
	}
	w := "WHERE " + strings.Join(where, " AND ")
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM tasks t JOIN lists l ON l.id = t.list_id `+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 500
	}
	tail := fmt.Sprintf(" ORDER BY %s LIMIT %d OFFSET %d", order, limit, q.Offset)
	ts, err := s.queryTasks(ctx, w+tail, args...)
	if err != nil {
		return nil, 0, err
	}
	if ts == nil {
		ts = []*Task{}
	}
	return ts, total, nil
}

// ftsQuery turns words into an FTS5 prefix query; phrases in quotes stay.
func ftsQuery(q string) string {
	var parts []string
	for _, w := range strings.Fields(q) {
		w = strings.Trim(w, `"'`)
		w = strings.ReplaceAll(w, `"`, "")
		if w == "" {
			continue
		}
		parts = append(parts, `"`+w+`"*`)
	}
	return strings.Join(parts, " ")
}

// Search is full-text search with a snippet.
func (s *Store) Search(ctx context.Context, uid int64, q string, limit int) ([]*Task, error) {
	m := ftsQuery(q)
	if m == "" {
		return []*Task{}, nil
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT f.rowid, snippet(tasks_fts, -1, '[[[', ']]]', '…', 14) FROM tasks_fts f JOIN tasks t ON t.id = f.rowid
		WHERE tasks_fts MATCH ? AND t.user_id = ? AND t.deleted_at = '' ORDER BY t.status IN ('done','cancelled'), bm25(tasks_fts) LIMIT ?`, m, uid, limit)
	if err != nil {
		return nil, err
	}
	var ids []int64
	snip := map[int64]string{}
	for rows.Next() {
		var id int64
		var sn string
		if rows.Scan(&id, &sn) == nil {
			ids = append(ids, id)
			snip[id] = sn
		}
	}
	rows.Close()
	if len(ids) == 0 {
		return []*Task{}, nil
	}
	ts, _, err := s.ListTasks(ctx, uid, Query{IDs: ids, Status: "all", Limit: limit}, 0)
	if err != nil {
		return nil, err
	}
	byID := map[int64]*Task{}
	for _, t := range ts {
		t.Snippet = snip[t.ID]
		byID[t.ID] = t
	}
	out := make([]*Task, 0, len(ids))
	for _, id := range ids {
		if t := byID[id]; t != nil {
			out = append(out, t)
		}
	}
	return out, nil
}

// InsertTask stores a new task (ics, etag, href, uid set by the caller).
func (s *Store) InsertTask(ctx context.Context, t *Task, after ...core.CommitHook) error {
	if t.SortOrder == 0 {
		var max sql.NullFloat64
		_ = s.DB.QueryRowContext(ctx, `SELECT MAX(sort_order) FROM tasks WHERE list_id = ? AND deleted_at = ''`, t.ListID).Scan(&max)
		t.SortOrder = max.Float64 + 1
	}
	t.Seq = s.NextSeq(ctx)
	now := Now()
	if t.CreatedAt == "" {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	res, err := tx.ExecContext(ctx, `INSERT INTO tasks (user_id, list_id, section_id, parent_id, uid, href, etag, ics, title, notes, status, completed_at, percent, priority, start, due, estimate_min, rrule, repeat_from, someday, sort_order, location, url, seq, nc_href, nc_etag, nc_seq, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.UserID, t.ListID, nullInt(t.SectionID), nullInt(t.ParentID), t.UID, t.Href, t.ETag, t.ICS, t.Title, t.Notes, t.Status, str(t.CompletedAt), t.Percent, t.Priority,
		t.StartStr(), t.DueStr(), intv(t.EstimateMin), rrule(t.Repeat), repeatFrom(t.Repeat), boolInt(t.Someday), t.SortOrder, t.Location, t.URL, t.Seq, t.NCHref, t.NCETag, t.NCSeq, t.CreatedAt, t.UpdatedAt)
	if err != nil {
		return conflict(err)
	}
	t.ID, _ = res.LastInsertId()
	if err := s.writeExtras(ctx, tx, t); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE lists SET seq = ?, updated_at = ? WHERE id = ?`, t.Seq, now, t.ListID); err != nil {
		return err
	}
	return core.Commit(tx, after...)
}

// UpdateTask saves every column of a task and bumps its change number. When
// the task moved lists, a tombstone marks where it used to be.
func (s *Store) UpdateTask(ctx context.Context, t *Task, after ...core.CommitHook) error {
	var oldList int64
	var oldHref string
	if err := s.DB.QueryRowContext(ctx, `SELECT list_id, href FROM tasks WHERE id = ? AND user_id = ?`, t.ID, t.UserID).Scan(&oldList, &oldHref); err != nil {
		return notFound(err)
	}
	t.Seq = s.NextSeq(ctx)
	now := Now()
	t.UpdatedAt = now
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET list_id = ?, section_id = ?, parent_id = ?, uid = ?, href = ?, etag = ?, ics = ?, title = ?, notes = ?, status = ?, completed_at = ?, percent = ?, priority = ?, start = ?, due = ?, estimate_min = ?, rrule = ?, repeat_from = ?, someday = ?, sort_order = ?, location = ?, url = ?, seq = ?, nc_href = ?, nc_etag = ?, nc_seq = ?, updated_at = ?, deleted_at = ? WHERE id = ? AND user_id = ?`,
		t.ListID, nullInt(t.SectionID), nullInt(t.ParentID), t.UID, t.Href, t.ETag, t.ICS, t.Title, t.Notes, t.Status, str(t.CompletedAt), t.Percent, t.Priority, t.StartStr(), t.DueStr(), intv(t.EstimateMin), rrule(t.Repeat), repeatFrom(t.Repeat), boolInt(t.Someday), t.SortOrder, t.Location, t.URL, t.Seq, t.NCHref, t.NCETag, t.NCSeq, now, t.DeletedAt, t.ID, t.UserID); err != nil {
		return conflict(err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM task_tags WHERE task_id = ?`, t.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM alarms WHERE task_id = ?`, t.ID); err != nil {
		return err
	}
	if err := s.writeExtras(ctx, tx, t); err != nil {
		return err
	}
	if oldList != t.ListID || oldHref != t.Href {
		if _, err := tx.ExecContext(ctx, `INSERT INTO tombstones (user_id, list_id, href, seq) VALUES (?, ?, ?, ?)`, t.UserID, oldList, oldHref, t.Seq); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE lists SET seq = ?, updated_at = ? WHERE id = ?`, t.Seq, now, oldList); err != nil {
			return err
		}
		// subtasks follow their parent into the new list
		if oldList != t.ListID {
			if _, err := tx.ExecContext(ctx, `INSERT INTO tombstones (user_id, list_id, href, seq) SELECT user_id, list_id, href, ? FROM tasks WHERE parent_id = ? AND list_id <> ?`, t.Seq, t.ID, t.ListID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE tasks SET list_id = ?, section_id = NULL, seq = ?, updated_at = ? WHERE parent_id = ? AND list_id <> ?`, t.ListID, t.Seq, now, t.ID, t.ListID); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE lists SET seq = ?, updated_at = ? WHERE id = ?`, t.Seq, now, t.ListID); err != nil {
		return err
	}
	return core.Commit(tx, after...)
}

func (s *Store) writeExtras(ctx context.Context, tx *sql.Tx, t *Task) error {
	seen := map[string]bool{}
	for _, tag := range t.Tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[strings.ToLower(tag)] {
			continue
		}
		seen[strings.ToLower(tag)] = true
		if _, err := tx.ExecContext(ctx, `INSERT INTO task_tags (task_id, user_id, tag) VALUES (?, ?, ?)`, t.ID, t.UserID, tag); err != nil {
			return err
		}
	}
	for _, a := range t.Alarms {
		if _, err := tx.ExecContext(ctx, `INSERT INTO alarms (user_id, task_id, trigger, description, fire_at) VALUES (?, ?, ?, ?, ?)`, t.UserID, t.ID, a.Trigger, a.Description, ""); err != nil {
			return err
		}
	}
	if t.DeletedAt == "" {
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO tasks_fts (rowid, title, notes, tags) VALUES (?, ?, ?, ?)`, t.ID, t.Title, t.Notes, strings.Join(t.Tags, " ")); err != nil {
			return err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `DELETE FROM tasks_fts WHERE rowid = ?`, t.ID); err != nil {
			return err
		}
	}
	return nil
}

// SetAlarmTimes stores when each alarm of a task fires (UTC RFC3339, "" = never).
func (s *Store) SetAlarmTimes(ctx context.Context, taskID int64, at map[string]string) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, trigger FROM alarms WHERE task_id = ?`, taskID)
	if err != nil {
		return err
	}
	type row struct {
		id  int64
		trg string
	}
	var rs []row
	for rows.Next() {
		var r row
		if rows.Scan(&r.id, &r.trg) == nil {
			rs = append(rs, r)
		}
	}
	rows.Close()
	for _, r := range rs {
		if _, err := s.DB.ExecContext(ctx, `UPDATE alarms SET fire_at = ?, fired_at = CASE WHEN fire_at <> ? THEN '' ELSE fired_at END WHERE id = ?`, at[r.trg], at[r.trg], r.id); err != nil {
			return err
		}
	}
	return nil
}

// DueAlarms returns alarms whose time has come and that were not sent.
func (s *Store) DueAlarms(ctx context.Context, before time.Time) ([]DueAlarm, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT a.id, a.user_id, a.task_id, a.trigger, a.description, a.fire_at FROM alarms a JOIN tasks t ON t.id = a.task_id
		WHERE a.fire_at <> '' AND a.fired_at = '' AND a.fire_at <= ? AND t.deleted_at = '' AND t.status IN ('open','in_progress') ORDER BY a.fire_at`, before.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DueAlarm
	for rows.Next() {
		var a DueAlarm
		if err := rows.Scan(&a.ID, &a.UserID, &a.TaskID, &a.Trigger, &a.Description, &a.FireAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// MarkAlarmFired records that an alarm was sent.
func (s *Store) MarkAlarmFired(ctx context.Context, id int64, after ...core.CommitHook) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE alarms SET fired_at = ? WHERE id = ?`, Now(), id)
	if err != nil {
		return err
	}
	return core.Commit(tx, after...)
}

// TasksWithAlarms lists live open tasks that have alarms (to recompute fire times).
func (s *Store) TasksWithAlarms(ctx context.Context, uid int64) ([]*Task, error) {
	return s.queryTasks(ctx, `WHERE t.user_id = ? AND t.deleted_at = '' AND t.status IN ('open','in_progress') AND EXISTS (SELECT 1 FROM alarms a WHERE a.task_id = t.id)`, uid)
}

// SoftDeleteTask moves a task and its subtasks to the trash.
func (s *Store) SoftDeleteTask(ctx context.Context, uid, id int64, after ...core.CommitHook) error {
	t, err := s.GetTask(ctx, uid, id)
	if err != nil {
		return err
	}
	seq := s.NextSeq(ctx)
	now := Now()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	ids := []int64{id}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM tasks WHERE parent_id = ? AND user_id = ? AND deleted_at = ''`, id, uid)
	if err != nil {
		return err
	}
	for rows.Next() {
		var c int64
		if rows.Scan(&c) == nil {
			ids = append(ids, c)
		}
	}
	rows.Close()
	for _, x := range ids {
		if _, err := tx.ExecContext(ctx, `INSERT INTO tombstones (user_id, list_id, href, seq) SELECT user_id, list_id, href, ? FROM tasks WHERE id = ?`, seq, x); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET deleted_at = ?, seq = ?, updated_at = ? WHERE id = ?`, now, seq, now, x); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM tasks_fts WHERE rowid = ?`, x); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE lists SET seq = ?, updated_at = ? WHERE id = ?`, seq, now, t.ListID); err != nil {
		return err
	}
	return core.Commit(tx, after...)
}

// RestoreTask brings a task (and its subtasks) back from the trash.
func (s *Store) RestoreTask(ctx context.Context, uid, id int64) error {
	t, err := s.GetTask(ctx, uid, id)
	if err != nil {
		return err
	}
	if t.DeletedAt == "" {
		return nil
	}
	seq := s.NextSeq(ctx)
	now := Now()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	// the parent may be gone: restore to the top level then
	if t.ParentID != nil {
		var pd string
		if err := tx.QueryRowContext(ctx, `SELECT deleted_at FROM tasks WHERE id = ?`, *t.ParentID).Scan(&pd); err != nil || pd != "" {
			if _, err := tx.ExecContext(ctx, `UPDATE tasks SET parent_id = NULL WHERE id = ?`, id); err != nil {
				return err
			}
		}
	}
	for _, x := range append([]int64{id}, s.childIDs(ctx, tx, id)...) {
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET deleted_at = '', seq = ?, updated_at = ? WHERE id = ? AND user_id = ?`, seq, now, x, uid); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO tasks_fts (rowid, title, notes, tags) SELECT id, title, notes, (SELECT COALESCE(GROUP_CONCAT(tag, ' '), '') FROM task_tags WHERE task_id = tasks.id) FROM tasks WHERE id = ?`, x); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE lists SET seq = ?, updated_at = ? WHERE id = ?`, seq, now, t.ListID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) childIDs(ctx context.Context, tx *sql.Tx, id int64) []int64 {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM tasks WHERE parent_id = ?`, id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var c int64
		if rows.Scan(&c) == nil {
			out = append(out, c)
		}
	}
	return out
}

// PurgeTask deletes a task for good (with subtasks, by cascade) and returns
// the attachment paths to remove.
func (s *Store) PurgeTask(ctx context.Context, uid, id int64) ([]string, error) {
	paths := s.attachmentPaths(ctx, uid, id)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, `DELETE FROM tasks_fts WHERE rowid = ? OR rowid IN (SELECT id FROM tasks WHERE parent_id = ?)`, id, id); err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM tasks WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return paths, tx.Commit()
}

func (s *Store) attachmentPaths(ctx context.Context, uid, id int64) []string {
	rows, err := s.DB.QueryContext(ctx, `SELECT path FROM attachments WHERE user_id = ? AND (task_id = ? OR task_id IN (SELECT id FROM tasks WHERE parent_id = ?))`, uid, id, id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if rows.Scan(&p) == nil {
			out = append(out, p)
		}
	}
	return out
}

// PurgeTrash removes tasks deleted more than days ago; returns attachment paths.
func (s *Store) PurgeTrash(ctx context.Context, uid int64, days int) ([]string, error) {
	cutoff := time.Now().AddDate(0, 0, -days).UTC().Format(time.RFC3339)
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM tasks WHERE user_id = ? AND deleted_at <> '' AND deleted_at < ? AND parent_id IS NULL`, uid, cutoff)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	var paths []string
	for _, id := range ids {
		p, err := s.PurgeTask(ctx, uid, id)
		if err == nil {
			paths = append(paths, p...)
		}
	}
	// orphans whose parent was purged
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM tasks WHERE user_id = ? AND deleted_at <> '' AND deleted_at < ?`, uid, cutoff)
	return paths, nil
}

// EmptyTrash purges everything in the trash and returns attachment paths.
func (s *Store) EmptyTrash(ctx context.Context, uid int64) ([]string, error) {
	return s.PurgeTrash(ctx, uid, -1)
}

// ---- sync helpers ----

// ChangedSince lists tasks of a list (deleted included) with seq > since, and the tombstones.
func (s *Store) ChangedSince(ctx context.Context, uid, listID, since int64) ([]*Task, []string, error) {
	ts, err := s.queryTasks(ctx, `WHERE t.user_id = ? AND t.list_id = ? AND t.seq > ? ORDER BY t.seq`, uid, listID, since)
	if err != nil {
		return nil, nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT href FROM tombstones WHERE user_id = ? AND list_id = ? AND seq > ?`, uid, listID, since)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var gone []string
	for rows.Next() {
		var h string
		if rows.Scan(&h) == nil {
			gone = append(gone, h)
		}
	}
	return ts, gone, nil
}

// ChangedSinceUser is ChangedSince over every list (for the calendar mirror).
func (s *Store) ChangedSinceUser(ctx context.Context, uid, since int64) ([]*Task, []string, error) {
	ts, err := s.queryTasks(ctx, `WHERE t.user_id = ? AND t.seq > ? ORDER BY t.seq`, uid, since)
	if err != nil {
		return nil, nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT href FROM tombstones WHERE user_id = ? AND seq > ?`, uid, since)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var gone []string
	for rows.Next() {
		var h string
		if rows.Scan(&h) == nil {
			gone = append(gone, h)
		}
	}
	return ts, gone, nil
}

// MaxSeq is the newest change number of a person (0 = nothing yet).
func (s *Store) MaxSeq(ctx context.Context, uid int64) int64 {
	var n sql.NullInt64
	_ = s.DB.QueryRowContext(ctx, `SELECT MAX(m) FROM (SELECT MAX(seq) m FROM tasks WHERE user_id = ? UNION ALL SELECT MAX(seq) FROM lists WHERE user_id = ? UNION ALL SELECT MAX(seq) FROM tombstones WHERE user_id = ?)`, uid, uid, uid).Scan(&n)
	return n.Int64
}

// ListTasksInList returns every live task of a list (CalDAV listing).
func (s *Store) ListTasksInList(ctx context.Context, uid, listID int64) ([]*Task, error) {
	return s.queryTasks(ctx, `WHERE t.user_id = ? AND t.list_id = ? AND t.deleted_at = '' ORDER BY t.id`, uid, listID)
}

// LiveTasks returns every live task of a person (the mirror, exports).
func (s *Store) LiveTasks(ctx context.Context, uid int64) ([]*Task, error) {
	return s.queryTasks(ctx, `WHERE t.user_id = ? AND t.deleted_at = '' ORDER BY t.id`, uid)
}

// OldestTombstoneSeq says how far back sync tokens are still valid.
func (s *Store) OldestTombstoneSeq(ctx context.Context, uid int64) int64 {
	var n sql.NullInt64
	_ = s.DB.QueryRowContext(ctx, `SELECT MIN(seq) FROM tombstones WHERE user_id = ?`, uid).Scan(&n)
	return n.Int64
}

// SetNC records what Nextcloud holds for a task after a push or pull.
func (s *Store) SetNC(ctx context.Context, id int64, href, etag string, seq int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE tasks SET nc_href = ?, nc_etag = ?, nc_seq = ? WHERE id = ?`, href, etag, seq, id)
	return err
}

// NeedPush lists the person's tasks changed since Nextcloud last saw them
// (deleted ones included, so the delete reaches there).
func (s *Store) NeedPush(ctx context.Context, uid, listID int64) ([]*Task, error) {
	return s.queryTasks(ctx, `WHERE t.user_id = ? AND t.list_id = ? AND t.seq > t.nc_seq ORDER BY t.parent_id IS NOT NULL, t.id`, uid, listID)
}

// ---- tags ----

// ListTags counts tags over live open tasks.
func (s *Store) ListTags(ctx context.Context, uid int64) ([]TagCount, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT x.tag, COUNT(1) FROM task_tags x JOIN tasks t ON t.id = x.task_id WHERE x.user_id = ? AND t.deleted_at = '' AND t.status IN ('open','in_progress') GROUP BY x.tag COLLATE NOCASE ORDER BY x.tag COLLATE NOCASE`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TagCount{}
	for rows.Next() {
		var t TagCount
		if err := rows.Scan(&t.Tag, &t.Count); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TaggedIDs lists live tasks carrying a tag.
func (s *Store) TaggedIDs(ctx context.Context, uid int64, tag string) []int64 {
	rows, err := s.DB.QueryContext(ctx, `SELECT task_id FROM task_tags WHERE user_id = ? AND tag = ? COLLATE NOCASE`, uid, tag)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

// ---- attachments ----

// AddAttachment records a stored file.
func (s *Store) AddAttachment(ctx context.Context, a *Attachment) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO attachments (user_id, task_id, name, size, type, path) VALUES (?, ?, ?, ?, ?, ?)`, a.UserID, a.TaskID, a.Name, a.Size, a.Type, a.Path)
	if err != nil {
		return err
	}
	a.ID, _ = res.LastInsertId()
	a.CreatedAt = Now()
	a.URL = fmt.Sprintf("/api/v1/tasks/%d/attachments/%d", a.TaskID, a.ID)
	return nil
}

// GetAttachment returns one attachment of a task.
func (s *Store) GetAttachment(ctx context.Context, uid, taskID, id int64) (*Attachment, error) {
	var a Attachment
	if err := s.DB.QueryRowContext(ctx, `SELECT id, user_id, task_id, name, size, type, path, created_at FROM attachments WHERE id = ? AND task_id = ? AND user_id = ?`, id, taskID, uid).Scan(&a.ID, &a.UserID, &a.TaskID, &a.Name, &a.Size, &a.Type, &a.Path, &a.CreatedAt); err != nil {
		return nil, notFound(err)
	}
	a.URL = fmt.Sprintf("/api/v1/tasks/%d/attachments/%d", a.TaskID, a.ID)
	return &a, nil
}

// DeleteAttachment removes the row and returns the file path.
func (s *Store) DeleteAttachment(ctx context.Context, uid, taskID, id int64) (string, error) {
	a, err := s.GetAttachment(ctx, uid, taskID, id)
	if err != nil {
		return "", err
	}
	_, err = s.DB.ExecContext(ctx, `DELETE FROM attachments WHERE id = ?`, id)
	return a.Path, err
}

// ---- activity ----

// AddActivity appends to the log.
func (s *Store) AddActivity(ctx context.Context, uid, taskID int64, kind, detail string) {
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO activity (user_id, task_id, kind, detail) VALUES (?, ?, ?, ?)`, uid, taskID, kind, detail)
}

// ListActivity returns the newest entries (taskID 0 = all).
func (s *Store) ListActivity(ctx context.Context, uid, taskID int64, limit int) ([]Activity, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT a.id, a.user_id, a.task_id, COALESCE(t.title, ''), a.kind, a.detail, a.at FROM activity a LEFT JOIN tasks t ON t.id = a.task_id WHERE a.user_id = ?`
	args := []any{uid}
	if taskID > 0 {
		q += ` AND a.task_id = ?`
		args = append(args, taskID)
	}
	rows, err := s.DB.QueryContext(ctx, q+fmt.Sprintf(` ORDER BY a.id DESC LIMIT %d`, limit), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Activity{}
	for rows.Next() {
		var a Activity
		if err := rows.Scan(&a.ID, &a.UserID, &a.TaskID, &a.TaskTitle, &a.Kind, &a.Detail, &a.At); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// PruneActivity keeps the newest n entries per person.
func (s *Store) PruneActivity(ctx context.Context, uid int64, keep int) {
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM activity WHERE user_id = ? AND id NOT IN (SELECT id FROM activity WHERE user_id = ? ORDER BY id DESC LIMIT ?)`, uid, uid, keep)
}

// ---- dashboard ----

// Dashboard counts what the widgets show. todayFrom/todayTo bound the local day in UTC.
func (s *Store) Dashboard(ctx context.Context, uid int64, today, weekEnd, todayFrom, weekFrom string) (*Dashboard, error) {
	d := &Dashboard{Next: []*Task{}}
	inbox, _ := s.Inbox(ctx, uid)
	q := func(dst *int, sqlq string, args ...any) {
		_ = s.DB.QueryRowContext(ctx, sqlq, args...).Scan(dst)
	}
	open := `SELECT COUNT(1) FROM tasks WHERE user_id = ? AND deleted_at = '' AND status IN ('open','in_progress')`
	q(&d.Open, open, uid)
	q(&d.Inbox, open+` AND list_id = ?`, uid, inbox.ID)
	q(&d.Today, open+` AND someday = 0 AND ((start <> '' AND substr(start,1,10) <= ?) OR (due <> '' AND substr(due,1,10) <= ?))`, uid, today, today)
	q(&d.Overdue, open+` AND due <> '' AND substr(due,1,10) < ?`, uid, today)
	q(&d.UpcomingWeek, open+` AND someday = 0 AND ((start <> '' AND substr(start,1,10) > ? AND substr(start,1,10) <= ?) OR (due <> '' AND substr(due,1,10) > ? AND substr(due,1,10) <= ?))`, uid, today, weekEnd, today, weekEnd)
	q(&d.Someday, open+` AND someday = 1`, uid)
	doneQ := `SELECT COUNT(1) FROM tasks WHERE user_id = ? AND deleted_at = '' AND status = 'done' AND completed_at >= ?`
	q(&d.DoneToday, doneQ, uid, todayFrom)
	q(&d.DoneWeek, doneQ, uid, weekFrom)
	q(&d.Lists, `SELECT COUNT(1) FROM lists WHERE user_id = ? AND archived = 0`, uid)
	next, _, err := s.ListTasks(ctx, uid, Query{View: "today", Today: today, Limit: 5}, inbox.ID)
	if err == nil {
		d.Next = next
	}
	return d, nil
}

// ---- small helpers ----

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func intv(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func rrule(r *Repeat) string {
	if r == nil {
		return ""
	}
	return r.RRule
}

func repeatFrom(r *Repeat) string {
	if r == nil || r.RRule == "" {
		return ""
	}
	if r.From == "completion" {
		return "completion"
	}
	return "due"
}
