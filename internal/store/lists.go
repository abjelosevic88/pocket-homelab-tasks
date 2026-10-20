package store

import (
	"context"
	"database/sql"
	"strings"
)

// ---- areas ----

const areaCols = `id, user_id, name, color, sort_order, created_at`

func scanArea(row interface{ Scan(...any) error }) (*Area, error) {
	var a Area
	if err := row.Scan(&a.ID, &a.UserID, &a.Name, &a.Color, &a.SortOrder, &a.CreatedAt); err != nil {
		return nil, notFound(err)
	}
	return &a, nil
}

// ListAreas returns a person's areas in order.
func (s *Store) ListAreas(ctx context.Context, uid int64) ([]Area, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+areaCols+` FROM areas WHERE user_id = ? ORDER BY sort_order, id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Area{}
	for rows.Next() {
		a, err := scanArea(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// GetArea returns one area of the person.
func (s *Store) GetArea(ctx context.Context, uid, id int64) (*Area, error) {
	return scanArea(s.DB.QueryRowContext(ctx, `SELECT `+areaCols+` FROM areas WHERE id = ? AND user_id = ?`, id, uid))
}

// CreateArea adds an area at the end.
func (s *Store) CreateArea(ctx context.Context, a *Area) error {
	var max sql.NullFloat64
	_ = s.DB.QueryRowContext(ctx, `SELECT MAX(sort_order) FROM areas WHERE user_id = ?`, a.UserID).Scan(&max)
	a.SortOrder = max.Float64 + 1
	res, err := s.DB.ExecContext(ctx, `INSERT INTO areas (user_id, name, color, sort_order) VALUES (?, ?, ?, ?)`, a.UserID, a.Name, a.Color, a.SortOrder)
	if err != nil {
		return err
	}
	a.ID, _ = res.LastInsertId()
	a.CreatedAt = Now()
	return nil
}

// UpdateArea saves name and colour.
func (s *Store) UpdateArea(ctx context.Context, a *Area) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE areas SET name = ?, color = ?, sort_order = ? WHERE id = ? AND user_id = ?`, a.Name, a.Color, a.SortOrder, a.ID, a.UserID)
	return err
}

// DeleteArea removes an area; its lists stay.
func (s *Store) DeleteArea(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM areas WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Reorder sets sort_order 1..n for the given ids of a table the person owns.
func (s *Store) Reorder(ctx context.Context, table string, uid int64, ids []int64) error {
	switch table {
	case "areas", "lists", "sections", "filters", "tasks":
	default:
		return ErrNotFound
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE `+table+` SET sort_order = ? WHERE id = ? AND user_id = ?`, float64(i+1), id, uid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---- lists ----

const listCols = `l.id, l.user_id, l.uid, l.name, l.color, l.description, l.sort_order, l.area_id, l.is_inbox, l.archived, l.seq, l.nc_href, l.nc_token, l.nc_ctag, l.created_at, l.updated_at,
	(SELECT COUNT(1) FROM tasks t WHERE t.list_id = l.id AND t.deleted_at = '' AND t.status IN ('open','in_progress'))`

func scanList(row interface{ Scan(...any) error }) (*List, error) {
	var l List
	var area sql.NullInt64
	var inbox, archived int
	if err := row.Scan(&l.ID, &l.UserID, &l.UID, &l.Name, &l.Color, &l.Description, &l.SortOrder, &area, &inbox, &archived, &l.Seq, &l.NCHref, &l.NCToken, &l.NCCtag, &l.CreatedAt, &l.UpdatedAt, &l.OpenCount); err != nil {
		return nil, notFound(err)
	}
	if area.Valid {
		v := area.Int64
		l.AreaID = &v
	}
	l.IsInbox, l.Archived = inbox == 1, archived == 1
	return &l, nil
}

// ListLists returns a person's lists in order, the inbox first.
func (s *Store) ListLists(ctx context.Context, uid int64) ([]List, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+listCols+` FROM lists l WHERE l.user_id = ? ORDER BY l.is_inbox DESC, l.sort_order, l.id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []List{}
	for rows.Next() {
		l, err := scanList(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

// GetList returns one list of the person.
func (s *Store) GetList(ctx context.Context, uid, id int64) (*List, error) {
	return scanList(s.DB.QueryRowContext(ctx, `SELECT `+listCols+` FROM lists l WHERE l.id = ? AND l.user_id = ?`, id, uid))
}

// GetListByUID returns the list with that CalDAV path segment.
func (s *Store) GetListByUID(ctx context.Context, uid int64, luid string) (*List, error) {
	return scanList(s.DB.QueryRowContext(ctx, `SELECT `+listCols+` FROM lists l WHERE l.user_id = ? AND l.uid = ?`, uid, luid))
}

// GetListByNCHref returns the list mirrored from a Nextcloud calendar.
func (s *Store) GetListByNCHref(ctx context.Context, uid int64, href string) (*List, error) {
	return scanList(s.DB.QueryRowContext(ctx, `SELECT `+listCols+` FROM lists l WHERE l.user_id = ? AND l.nc_href = ?`, uid, href))
}

// Inbox returns the person's inbox, creating it on first sight.
func (s *Store) Inbox(ctx context.Context, uid int64) (*List, error) {
	if l, err := scanList(s.DB.QueryRowContext(ctx, `SELECT `+listCols+` FROM lists l WHERE l.user_id = ? AND l.is_inbox = 1`, uid)); err == nil {
		return l, nil
	}
	l := &List{UserID: uid, UID: "inbox", Name: "Inbox", IsInbox: true, Color: "#2563eb"}
	if err := s.CreateList(ctx, l); err != nil {
		return nil, err
	}
	return l, nil
}

// CreateList adds a list at the end.
func (s *Store) CreateList(ctx context.Context, l *List) error {
	if l.UID == "" {
		l.UID = strings.ToUpper(RandomToken(8))
	}
	var max sql.NullFloat64
	_ = s.DB.QueryRowContext(ctx, `SELECT MAX(sort_order) FROM lists WHERE user_id = ?`, l.UserID).Scan(&max)
	l.SortOrder = max.Float64 + 1
	l.Seq = s.NextSeq(ctx)
	res, err := s.DB.ExecContext(ctx, `INSERT INTO lists (user_id, uid, name, color, description, sort_order, area_id, is_inbox, archived, seq, nc_href) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.UserID, l.UID, l.Name, l.Color, l.Description, l.SortOrder, nullInt(l.AreaID), boolInt(l.IsInbox), boolInt(l.Archived), l.Seq, l.NCHref)
	if err != nil {
		return conflict(err)
	}
	l.ID, _ = res.LastInsertId()
	l.CreatedAt, l.UpdatedAt = Now(), Now()
	return nil
}

// UpdateList saves the editable columns and bumps the list's change number.
func (s *Store) UpdateList(ctx context.Context, l *List) error {
	l.Seq = s.NextSeq(ctx)
	_, err := s.DB.ExecContext(ctx, `UPDATE lists SET name = ?, color = ?, description = ?, area_id = ?, archived = ?, seq = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		l.Name, l.Color, l.Description, nullInt(l.AreaID), boolInt(l.Archived), l.Seq, Now(), l.ID, l.UserID)
	return err
}

// SetListNC stores the Nextcloud calendar a list mirrors and its sync state.
func (s *Store) SetListNC(ctx context.Context, id int64, href, token, ctag string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE lists SET nc_href = ?, nc_token = ?, nc_ctag = ? WHERE id = ?`, href, token, ctag, id)
	return err
}

// TouchList bumps a list's change number (the collection's ctag) after a task changed in it.
func (s *Store) TouchList(ctx context.Context, id, seq int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE lists SET seq = MAX(seq, ?), updated_at = ? WHERE id = ?`, seq, Now(), id)
	return err
}

// DeleteList removes a list. Its tasks move to moveTo when given, else they
// are deleted for good (the trash would show tasks of a list that is gone).
func (s *Store) DeleteList(ctx context.Context, uid, id, moveTo int64) error {
	l, err := s.GetList(ctx, uid, id)
	if err != nil {
		return err
	}
	if l.IsInbox {
		return ErrConflict
	}
	seq := s.NextSeq(ctx)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if moveTo > 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO tombstones (user_id, list_id, href, seq) SELECT user_id, list_id, href, ? FROM tasks WHERE list_id = ? AND user_id = ?`, seq, id, uid); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET list_id = ?, section_id = NULL, seq = ?, updated_at = ? WHERE list_id = ? AND user_id = ?`, moveTo, seq, Now(), id, uid); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE lists SET seq = ? WHERE id = ?`, seq, moveTo); err != nil {
			return err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `DELETE FROM tasks_fts WHERE rowid IN (SELECT id FROM tasks WHERE list_id = ? AND user_id = ?)`, id, uid); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO tombstones (user_id, list_id, href, seq) SELECT user_id, list_id, href, ? FROM tasks WHERE list_id = ? AND user_id = ?`, seq, id, uid); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM lists WHERE id = ? AND user_id = ?`, id, uid); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE user_settings SET value = '0' WHERE user_id = ? AND key = 'default_list_id' AND value = ?`, uid, id); err != nil {
		return err
	}
	return tx.Commit()
}

// ---- sections ----

const sectionCols = `id, user_id, list_id, name, sort_order`

func scanSection(row interface{ Scan(...any) error }) (*Section, error) {
	var x Section
	if err := row.Scan(&x.ID, &x.UserID, &x.ListID, &x.Name, &x.SortOrder); err != nil {
		return nil, notFound(err)
	}
	return &x, nil
}

// ListSections returns a list's sections in order (listID 0 = every list).
func (s *Store) ListSections(ctx context.Context, uid, listID int64) ([]Section, error) {
	q := `SELECT ` + sectionCols + ` FROM sections WHERE user_id = ?`
	args := []any{uid}
	if listID > 0 {
		q += ` AND list_id = ?`
		args = append(args, listID)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY list_id, sort_order, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Section{}
	for rows.Next() {
		x, err := scanSection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *x)
	}
	return out, rows.Err()
}

// GetSection returns one section of the person.
func (s *Store) GetSection(ctx context.Context, uid, id int64) (*Section, error) {
	return scanSection(s.DB.QueryRowContext(ctx, `SELECT `+sectionCols+` FROM sections WHERE id = ? AND user_id = ?`, id, uid))
}

// SectionByName finds (or makes) a section with that name in a list.
func (s *Store) SectionByName(ctx context.Context, uid, listID int64, name string) (*Section, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrNotFound
	}
	if x, err := scanSection(s.DB.QueryRowContext(ctx, `SELECT `+sectionCols+` FROM sections WHERE user_id = ? AND list_id = ? AND name = ? COLLATE NOCASE`, uid, listID, name)); err == nil {
		return x, nil
	}
	x := &Section{UserID: uid, ListID: listID, Name: name}
	if err := s.CreateSection(ctx, x); err != nil {
		return nil, err
	}
	return x, nil
}

// CreateSection adds a section at the end of its list.
func (s *Store) CreateSection(ctx context.Context, x *Section) error {
	var max sql.NullFloat64
	_ = s.DB.QueryRowContext(ctx, `SELECT MAX(sort_order) FROM sections WHERE list_id = ?`, x.ListID).Scan(&max)
	x.SortOrder = max.Float64 + 1
	res, err := s.DB.ExecContext(ctx, `INSERT INTO sections (user_id, list_id, name, sort_order) VALUES (?, ?, ?, ?)`, x.UserID, x.ListID, x.Name, x.SortOrder)
	if err != nil {
		return err
	}
	x.ID, _ = res.LastInsertId()
	return nil
}

// UpdateSection renames a section.
func (s *Store) UpdateSection(ctx context.Context, x *Section) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE sections SET name = ?, sort_order = ? WHERE id = ? AND user_id = ?`, x.Name, x.SortOrder, x.ID, x.UserID)
	return err
}

// DeleteSection removes a section; its tasks stay in the list.
func (s *Store) DeleteSection(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM sections WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- filters ----

// ListFilters returns the saved filters in order.
func (s *Store) ListFilters(ctx context.Context, uid int64) ([]Filter, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, user_id, name, icon, query, sort_order FROM filters WHERE user_id = ? ORDER BY sort_order, id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Filter{}
	for rows.Next() {
		var f Filter
		var q string
		if err := rows.Scan(&f.ID, &f.UserID, &f.Name, &f.Icon, &q, &f.SortOrder); err != nil {
			return nil, err
		}
		_ = jsonUnmarshal(q, &f.Query)
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetFilter returns one saved filter.
func (s *Store) GetFilter(ctx context.Context, uid, id int64) (*Filter, error) {
	var f Filter
	var q string
	if err := s.DB.QueryRowContext(ctx, `SELECT id, user_id, name, icon, query, sort_order FROM filters WHERE id = ? AND user_id = ?`, id, uid).Scan(&f.ID, &f.UserID, &f.Name, &f.Icon, &q, &f.SortOrder); err != nil {
		return nil, notFound(err)
	}
	_ = jsonUnmarshal(q, &f.Query)
	return &f, nil
}

// SaveFilter inserts or updates a filter.
func (s *Store) SaveFilter(ctx context.Context, f *Filter) error {
	q := jsonMarshal(f.Query)
	if f.ID == 0 {
		var max sql.NullFloat64
		_ = s.DB.QueryRowContext(ctx, `SELECT MAX(sort_order) FROM filters WHERE user_id = ?`, f.UserID).Scan(&max)
		f.SortOrder = max.Float64 + 1
		res, err := s.DB.ExecContext(ctx, `INSERT INTO filters (user_id, name, icon, query, sort_order) VALUES (?, ?, ?, ?, ?)`, f.UserID, f.Name, f.Icon, q, f.SortOrder)
		if err != nil {
			return err
		}
		f.ID, _ = res.LastInsertId()
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE filters SET name = ?, icon = ?, query = ? WHERE id = ? AND user_id = ?`, f.Name, f.Icon, q, f.ID, f.UserID)
	return err
}

// DeleteFilter removes a saved filter.
func (s *Store) DeleteFilter(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM filters WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
