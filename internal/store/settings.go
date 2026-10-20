package store

import (
	"context"
	"database/sql"
	"strconv"
)

// DefaultTitle is the app title until someone changes it.
const DefaultTitle = "Pocket Tasks"

func defaults() Settings {
	return Settings{
		Title: DefaultTitle, WeekStart: 1, DefaultEstimateMin: 30, DayStart: "09:00", ShowDoneInLists: false, LogbookGroup: "day",
		MirrorEnabled: true, MirrorIncludeDue: true, MirrorRollover: true, MirrorDone: true, MirrorDoneCalDAV: false,
		NotifyKind: "apprise", NotifyAlarms: true, TrashDays: 30, NCMode: "off", NCVia: "core", NCIntervalMin: 2,
	}
}

// GetSettings loads the box settings plus the preferences of userID, with
// defaults applied. userID 0 gives the box settings with default preferences.
func (s *Store) GetSettings(ctx context.Context, userID int64) (Settings, error) {
	st := defaults()
	rows, err := s.DB.QueryContext(ctx, `SELECT key, value FROM settings WHERE key IN ('title', 'setup_complete')
		UNION ALL SELECT key, value FROM user_settings WHERE user_id = ?`, userID)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	atoi := func(v string, def int) int {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
		return def
	}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return st, err
		}
		switch k {
		case "title":
			if v != "" {
				st.Title = v
			}
		case "setup_complete":
			st.SetupComplete = v == "1"
		case "week_start":
			if v == "0" {
				st.WeekStart = 0
			}
		case "default_list_id":
			n, _ := strconv.ParseInt(v, 10, 64)
			st.DefaultListID = n
		case "default_estimate_min":
			st.DefaultEstimateMin = atoi(v, st.DefaultEstimateMin)
		case "day_start":
			if len(v) == 5 {
				st.DayStart = v
			}
		case "default_alarm":
			st.DefaultAlarm = v
		case "show_done_in_lists":
			st.ShowDoneInLists = v == "1"
		case "logbook_group":
			if v == "day" || v == "list" {
				st.LogbookGroup = v
			}
		case "timezone":
			st.Timezone = v
		case "mirror_enabled":
			st.MirrorEnabled = v != "0"
		case "mirror_include_due":
			st.MirrorIncludeDue = v != "0"
		case "mirror_rollover":
			st.MirrorRollover = v != "0"
		case "mirror_done":
			st.MirrorDone = v != "0"
		case "mirror_done_caldav":
			st.MirrorDoneCalDAV = v == "1"
		case "notify_kind":
			if v == "apprise" || v == "ntfy" || v == "webhook" {
				st.NotifyKind = v
			}
		case "notify_url":
			st.NotifyURL = v
		case "notify_token":
			st.NotifyToken = v
		case "notify_alarms":
			st.NotifyAlarms = v != "0"
		case "digest_time":
			st.DigestTime = v
		case "digest_sent_on":
			st.DigestSentOn = v
		case "trash_days":
			st.TrashDays = atoi(v, st.TrashDays)
		case "nc.mode":
			if v == "off" || v == "import" || v == "two_way" {
				st.NCMode = v
			}
		case "nc.via":
			if v == "core" || v == "direct" {
				st.NCVia = v
			}
		case "nc.url":
			st.NCURL = v
		case "nc.user":
			st.NCUser = v
		case "nc.password":
			st.NCPassword = v
		case "nc.interval_min":
			st.NCIntervalMin = atoi(v, st.NCIntervalMin)
		}
	}
	if st.NCIntervalMin < 1 {
		st.NCIntervalMin = 1
	}
	if st.TrashDays < 1 {
		st.TrashDays = 1
	}
	return st, rows.Err()
}

func b2s(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// SaveSettings stores the personal preferences of userID (not the box title).
func (s *Store) SaveSettings(ctx context.Context, userID int64, st Settings) error {
	kv := map[string]string{
		"week_start":           strconv.Itoa(st.WeekStart),
		"default_list_id":      strconv.FormatInt(st.DefaultListID, 10),
		"default_estimate_min": strconv.Itoa(st.DefaultEstimateMin),
		"day_start":            st.DayStart,
		"default_alarm":        st.DefaultAlarm,
		"show_done_in_lists":   b2s(st.ShowDoneInLists),
		"logbook_group":        st.LogbookGroup,
		"timezone":             st.Timezone,
		"mirror_enabled":       b2s(st.MirrorEnabled),
		"mirror_include_due":   b2s(st.MirrorIncludeDue),
		"mirror_rollover":      b2s(st.MirrorRollover),
		"mirror_done":          b2s(st.MirrorDone),
		"mirror_done_caldav":   b2s(st.MirrorDoneCalDAV),
		"notify_kind":          st.NotifyKind,
		"notify_url":           st.NotifyURL,
		"notify_alarms":        b2s(st.NotifyAlarms),
		"digest_time":          st.DigestTime,
		"trash_days":           strconv.Itoa(st.TrashDays),
	}
	if st.NotifyToken != "" {
		kv["notify_token"] = st.NotifyToken
	}
	return s.putUserSettings(ctx, userID, kv)
}

// SaveNextcloud stores the sync settings. An empty password keeps the one stored.
func (s *Store) SaveNextcloud(ctx context.Context, userID int64, st Settings) error {
	kv := map[string]string{"nc.mode": st.NCMode, "nc.via": st.NCVia, "nc.url": st.NCURL, "nc.user": st.NCUser, "nc.interval_min": strconv.Itoa(st.NCIntervalMin)}
	if st.NCPassword != "" {
		kv["nc.password"] = st.NCPassword
	}
	return s.putUserSettings(ctx, userID, kv)
}

// UserSetting reads one raw key of a person's settings ("" when missing).
func (s *Store) UserSetting(ctx context.Context, userID int64, key string) string {
	var v string
	_ = s.DB.QueryRowContext(ctx, `SELECT value FROM user_settings WHERE user_id = ? AND key = ?`, userID, key).Scan(&v)
	return v
}

// UserSettingsPrefix reads every key of a person's settings starting with prefix.
func (s *Store) UserSettingsPrefix(ctx context.Context, userID int64, prefix string) map[string]string {
	out := map[string]string{}
	rows, err := s.DB.QueryContext(ctx, `SELECT key, value FROM user_settings WHERE user_id = ? AND key LIKE ? ESCAPE '\'`, userID, likeEscape(prefix)+"%")
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) == nil {
			out[k] = v
		}
	}
	return out
}

// SetUserSetting stores one key for a person.
func (s *Store) SetUserSetting(ctx context.Context, userID int64, key, value string) error {
	return s.putUserSettings(ctx, userID, map[string]string{key: value})
}

// SaveBoxSettings stores the settings of the box itself: title and setup state.
func (s *Store) SaveBoxSettings(ctx context.Context, st Settings) error {
	kv := map[string]string{"title": st.Title, "setup_complete": b2s(st.SetupComplete)}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for k, v := range kv {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) putUserSettings(ctx context.Context, userID int64, kv map[string]string) error {
	if userID <= 0 {
		return ErrNotFound
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for k, v := range kv {
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_settings (user_id, key, value) VALUES (?, ?, ?)
			ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value`, userID, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// OwnerID is the account the box was set up with (the first user): keys
// given to the whole box through the environment are theirs.
func (s *Store) OwnerID(ctx context.Context) int64 {
	var id sql.NullInt64
	_ = s.DB.QueryRowContext(ctx, `SELECT MIN(id) FROM users`).Scan(&id)
	return id.Int64
}

// UserIDs lists every user id.
func (s *Store) UserIDs(ctx context.Context) []int64 {
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM users ORDER BY id`)
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
