package store

import "github.com/abjelosevic88/pocket-homelab-tasks/internal/ical"

// User is an account that can sign in. The first user is the admin; admins
// manage other users. Every user has their own lists and tasks.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	Username     string `json:"username"` // CalDAV principal name
	PasswordHash string `json:"-"`
	Role         string `json:"role"` // admin | member
	FeedToken    string `json:"-"`
	CreatedAt    string `json:"created_at"`
}

// APIToken is a personal access token (hash stored, plaintext shown once).
// It doubles as the app password CalDAV clients sign in with.
type APIToken struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

// Area groups lists.
type Area struct {
	ID        int64   `json:"id"`
	UserID    int64   `json:"-"`
	Name      string  `json:"name"`
	Color     string  `json:"color"`
	SortOrder float64 `json:"sort_order"`
	CreatedAt string  `json:"created_at"`
}

// List is one CalDAV collection of tasks.
type List struct {
	ID          int64   `json:"id"`
	UserID      int64   `json:"-"`
	UID         string  `json:"uid"`
	Name        string  `json:"name"`
	Color       string  `json:"color"`
	Description string  `json:"description"`
	SortOrder   float64 `json:"sort_order"`
	AreaID      *int64  `json:"area_id"`
	IsInbox     bool    `json:"is_inbox"`
	Archived    bool    `json:"archived"`
	OpenCount   int     `json:"open_count"`
	Seq         int64   `json:"-"`
	NCHref      string  `json:"nc_href,omitempty"`
	NCToken     string  `json:"-"`
	NCCtag      string  `json:"-"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// Section is a heading inside a list.
type Section struct {
	ID        int64   `json:"id"`
	UserID    int64   `json:"-"`
	ListID    int64   `json:"list_id"`
	Name      string  `json:"name"`
	SortOrder float64 `json:"sort_order"`
}

// Attachment is a file kept with a task.
type Attachment struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"-"`
	TaskID    int64  `json:"task_id"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	Type      string `json:"type"`
	Path      string `json:"-"`
	URL       string `json:"url"`
	CreatedAt string `json:"created_at"`
}

// Repeat is the API shape of a recurrence.
type Repeat struct {
	RRule string `json:"rrule"`
	From  string `json:"from"` // due | completion
}

// ListRef names a task's list in answers.
type ListRef struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// Task is one task as the API shows it.
type Task struct {
	ID           int64        `json:"id"`
	UserID       int64        `json:"-"`
	UID          string       `json:"uid"`
	ListID       int64        `json:"list_id"`
	SectionID    *int64       `json:"section_id"`
	ParentID     *int64       `json:"parent_id"`
	Title        string       `json:"title"`
	Notes        string       `json:"notes"`
	Status       string       `json:"status"`
	CompletedAt  *string      `json:"completed_at"`
	Percent      int          `json:"percent"`
	Priority     int          `json:"priority"`
	Start        *string      `json:"start"`
	Due          *string      `json:"due"`
	EstimateMin  *int         `json:"estimate_min"`
	Repeat       *Repeat      `json:"repeat"`
	Someday      bool         `json:"someday"`
	Tags         []string     `json:"tags"`
	Alarms       []ical.Alarm `json:"alarms"`
	Location     string       `json:"location"`
	URL          string       `json:"url"`
	SortOrder    float64      `json:"sort_order"`
	Subtasks     []*Task      `json:"subtasks,omitempty"`
	SubtaskCount int          `json:"subtask_count"`
	SubtaskDone  int          `json:"subtask_done"`
	Attachments  []Attachment `json:"attachments"`
	List         *ListRef     `json:"list,omitempty"`
	Overdue      bool         `json:"overdue"`
	Snippet      string       `json:"snippet,omitempty"`
	ETag         string       `json:"etag"`
	Href         string       `json:"-"`
	ICS          string       `json:"-"`
	Seq          int64        `json:"-"`
	NCHref       string       `json:"-"`
	NCETag       string       `json:"-"`
	NCSeq        int64        `json:"-"`
	CreatedAt    string       `json:"created_at"`
	UpdatedAt    string       `json:"updated_at"`
	DeletedAt    string       `json:"deleted_at,omitempty"`
}

// StartStr / DueStr give the plain strings ("" when none).
func (t *Task) StartStr() string {
	if t.Start == nil {
		return ""
	}
	return *t.Start
}

// DueStr is the due as a plain string.
func (t *Task) DueStr() string {
	if t.Due == nil {
		return ""
	}
	return *t.Due
}

// Open reports whether the task is still to do.
func (t *Task) Open() bool { return t.Status == ical.StatusOpen || t.Status == ical.StatusInProgress }

// Filter is a saved query.
type Filter struct {
	ID        int64   `json:"id"`
	UserID    int64   `json:"-"`
	Name      string  `json:"name"`
	Icon      string  `json:"icon"`
	Query     Query   `json:"query"`
	SortOrder float64 `json:"sort_order"`
}

// Query narrows ListTasks. Zero values mean "no constraint".
type Query struct {
	View        string   `json:"view,omitempty"`
	ListID      int64    `json:"list_id,omitempty"`
	SectionID   *int64   `json:"section_id,omitempty"`
	Tag         string   `json:"tag,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Status      string   `json:"status,omitempty"` // open | done | all
	Priority    *int     `json:"priority,omitempty"`
	Q           string   `json:"q,omitempty"`
	DueFrom     string   `json:"due_from,omitempty"`
	DueTo       string   `json:"due_to,omitempty"`
	StartFrom   string   `json:"start_from,omitempty"`
	StartTo     string   `json:"start_to,omitempty"`
	Someday     *bool    `json:"someday,omitempty"`
	HasDate     *bool    `json:"has_date,omitempty"`
	CompletedOn string   `json:"completed_on,omitempty"`
	ModifiedAt  string   `json:"modified_since,omitempty"`
	ParentID    *int64   `json:"parent_id,omitempty"` // 0 = top level only
	Limit       int      `json:"limit,omitempty"`
	Offset      int      `json:"offset,omitempty"`
	Today       string   `json:"-"` // the person's today, YYYY-MM-DD
	Now         string   `json:"-"` // the person's now, floating
	Deleted     bool     `json:"-"` // the trash
	IDs         []int64  `json:"-"`
	Links       []string `json:"-"` // ItemKey of each url=: only tasks linking to one (mentions.go)
}

// Group is a heading over some ids in a list answer.
type Group struct {
	Key   string  `json:"key"`
	Title string  `json:"title"`
	IDs   []int64 `json:"ids"`
}

// Activity is one entry of the log.
type Activity struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"-"`
	TaskID    int64  `json:"task_id"`
	TaskTitle string `json:"task_title,omitempty"`
	Kind      string `json:"kind"`
	Detail    string `json:"detail"`
	At        string `json:"at"`
}

// TagCount is a tag with how many open tasks carry it.
type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// Alarm row with its task, for the notifier.
type DueAlarm struct {
	ID          int64
	UserID      int64
	TaskID      int64
	Trigger     string
	Description string
	FireAt      string
}

// Settings are the box's title plus one person's preferences.
type Settings struct {
	Title         string `json:"title"`
	SetupComplete bool   `json:"setup_complete"`

	WeekStart          int    `json:"week_start"`
	DefaultListID      int64  `json:"default_list_id"`
	DefaultEstimateMin int    `json:"default_estimate_min"`
	DayStart           string `json:"day_start"`
	DefaultAlarm       string `json:"default_alarm"`
	ShowDoneInLists    bool   `json:"show_done_in_lists"`
	LogbookGroup       string `json:"logbook_group"`
	Timezone           string `json:"timezone"`

	MirrorEnabled    bool `json:"mirror_enabled"`
	MirrorIncludeDue bool `json:"mirror_include_due"`
	MirrorRollover   bool `json:"mirror_rollover"`
	MirrorDone       bool `json:"mirror_done"`
	MirrorDoneCalDAV bool `json:"mirror_done_caldav"`

	NotifyKind   string `json:"notify_kind"`
	NotifyURL    string `json:"notify_url"`
	NotifyToken  string `json:"-"`
	NotifyAlarms bool   `json:"notify_alarms"`
	DigestTime   string `json:"digest_time"`
	DigestSentOn string `json:"-"`

	TrashDays int `json:"trash_days"`

	// Nextcloud sync
	NCMode        string `json:"-"` // off | import | two_way
	NCVia         string `json:"-"` // core | direct
	NCURL         string `json:"-"`
	NCUser        string `json:"-"`
	NCPassword    string `json:"-"`
	NCIntervalMin int    `json:"-"`
}

// Dashboard is the compact status used by widgets.
type Dashboard struct {
	Open         int     `json:"open"`
	Inbox        int     `json:"inbox"`
	Today        int     `json:"today"`
	Overdue      int     `json:"overdue"`
	UpcomingWeek int     `json:"upcoming_week"`
	Someday      int     `json:"someday"`
	DoneToday    int     `json:"done_today"`
	DoneWeek     int     `json:"done_week"`
	Lists        int     `json:"lists"`
	Next         []*Task `json:"next"`
}
