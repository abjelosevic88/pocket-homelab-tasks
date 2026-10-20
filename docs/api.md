# Pocket Tasks — HTTP API (v1)

Base: `/api/v1`. JSON in and out. Errors: `{"error": "message"}` with the HTTP status.
Auth: session cookie (`pt_session`), `Authorization: Bearer pt_…` (API token), Pocket Core tokens
(`pcd_`/`pcu_`/`pco_`), link tokens (`pcx_`, scoped), or Basic auth (email + API token or password).
Everything is per person: another person's ids answer 404.

## Dates

Dates are **floating local** strings, never UTC: `"2026-10-21"` (all day) or `"2026-10-21T09:00:00"`
(timed). `null` clears a date. Timestamps (`created_at`, `completed_at`, `updated_at`) are RFC 3339 UTC.

## Auth

| Method | Path | Notes |
|---|---|---|
| GET | /auth/status | `{needs_setup, auth_mode, authenticated, user, version, title, oidc, oidc_name}` |
| POST | /auth/setup | `{email, password, name}` first account (admin) |
| POST | /auth/login | `{email, password}` |
| POST | /auth/logout | |
| GET | /auth/me | current user |
| PUT | /auth/me | `{name?, email?, username?, current_password?, new_password?}` |
| GET | /auth/oidc/start?next= | browser redirect to Pocket Core / OIDC |
| GET | /auth/oidc/callback | |

User: `{id, email, name, username, role: "admin"|"member", created_at}`. `username` is the CalDAV
principal name (default: the part of the email before `@`).

## Objects

```ts
interface Task {
  id: number; uid: string; list_id: number; section_id: number|null; parent_id: number|null
  title: string; notes: string                       // notes: Markdown
  status: 'open'|'in_progress'|'done'|'cancelled'; completed_at: string|null; percent: number
  priority: number                                   // 0 none, 1 high, 5 medium, 9 low (1-4 high, 6-9 low)
  start: string|null; due: string|null               // floating local, see Dates
  estimate_min: number|null
  repeat: { rrule: string; from: 'due'|'completion' } | null   // rrule without "RRULE:"
  someday: boolean
  tags: string[]
  alarms: { trigger: string; description?: string }[] // "-PT15M" (before due, or start when no due), "PT0S", or absolute "2026-10-21T08:00:00Z"
  location: string; url: string
  sort_order: number
  subtasks?: Task[]                                  // on GET /tasks/{id} and in list answers when nest=1 (one level)
  subtask_count: number; subtask_done: number
  attachments: { id: number; name: string; size: number; type: string; url: string }[]
  list?: { id: number; name: string; color: string }  // on view answers
  overdue: boolean                                   // computed: open and (due < today or timed due < now)
  etag: string; created_at: string; updated_at: string
}
interface List  { id: number; uid: string; name: string; color: string; description: string; sort_order: number; area_id: number|null; is_inbox: boolean; archived: boolean; open_count: number; nc_href?: string; created_at: string }
interface Area  { id: number; name: string; color: string; sort_order: number }
interface Section { id: number; list_id: number; name: string; sort_order: number }
interface Filter { id: number; name: string; icon: string; sort_order: number; query: TaskQuery }
interface TaskQuery { view?: string; list_id?: number; section_id?: number; tag?: string; tags?: string[]; status?: 'open'|'done'|'all'; priority?: number; q?: string; due_from?: string; due_to?: string; start_from?: string; start_to?: string; someday?: boolean; has_date?: boolean }
```

Colours are `#rrggbb`. Tags are plain lowercase words/phrases (no `#`).

## Dashboard and meta

| Method | Path | Notes |
|---|---|---|
| GET | /dashboard | `{open, inbox, today, overdue, upcoming_week, someday, done_today, done_week, lists, next: Task[] (up to 5 for today)}` — what the Homepage widget reads |
| GET | /changes | `{stamp: number}` changes whenever anything changed; the UI polls it every ~10 s |
| GET | /settings | Settings (below) |
| PUT | /settings | partial, returns Settings |
| GET | /settings/system | `{version, go, db_size, data_dir, counts, core: {configured, url, registered, error}, caldav: {url, username}, feed_url, base_url}` |

Settings:
```ts
interface Settings {
  title: string                      // box-wide, admins only
  week_start: 0|1                    // 0 Sunday, 1 Monday
  default_list_id: number            // where quick add puts tasks without /list (the inbox by default)
  default_estimate_min: number       // 30
  day_start: string                  // "09:00": when an all-day task's reminder and the mirror's timed-for-all-day use
  default_alarm: string              // "" | "PT0S" | "-PT15M" | … added to new timed tasks made here
  show_done_in_lists: boolean
  logbook_group: 'day'|'list'
  timezone: string                   // IANA, defaults to the server's TZ
  mirror_enabled: boolean            // the Tasks calendar / feed
  mirror_include_due: boolean        // due-only tasks appear (true)
  mirror_rollover: boolean           // overdue open tasks show today (true)
  mirror_done: boolean               // finished tasks on the day they were finished (feed: true)
  mirror_done_caldav: boolean        // same in the CalDAV VEVENT collection (false)
  notify_kind: 'apprise'|'ntfy'|'webhook'; notify_url: string; notify_token_set: boolean; notify_env: boolean
  notify_alarms: boolean             // push alarms from the server (true when notify_url set)
  digest_time: string                // "07:30" or "" — a morning summary
  trash_days: number                 // 30
  core: boolean                      // Pocket Core configured on this box
}
```

## Lists, areas, sections

| Method | Path | Notes |
|---|---|---|
| GET | /lists | `List[]` (archived included with `archived: true`) |
| POST | /lists | `{name, color?, description?, area_id?}` |
| PUT | /lists/{id} | partial: name, color, description, area_id (null to clear), archived |
| DELETE | /lists/{id}?move_to={list_id} | tasks go to `move_to` (default: trash). The inbox cannot be deleted |
| POST | /lists/reorder | `{ids: number[]}` |
| GET | /areas | |
| POST | /areas | `{name, color?}` |
| PUT | /areas/{id} | |
| DELETE | /areas/{id} | its lists keep existing with no area |
| POST | /areas/reorder | `{ids}` |
| GET | /lists/{id}/sections | |
| POST | /lists/{id}/sections | `{name}` |
| PUT | /sections/{id} | `{name}` |
| DELETE | /sections/{id} | its tasks stay in the list without a section |
| POST | /lists/{id}/sections/reorder | `{ids}` |

## Tasks

| Method | Path | Notes |
|---|---|---|
| GET | /tasks | query below. Answer `{tasks: Task[], total: number, groups?: {key, title, ids}[]}` |
| POST | /tasks | any Task fields (title required) + `tags`, `alarms`; `text` instead of title → quick-add parsing applies |
| POST | /tasks/quick | `{text, list_id?, commit: boolean}` → `{task: Task (draft when !commit), parsed: {title, due, start, list_id, tags, priority, repeat, alarm}}` |
| GET | /tasks/{id} | with `subtasks`, `attachments`, `activity` (last 50) |
| PUT | /tasks/{id} | partial merge of Task fields (explicit `null` clears nullable ones); `tags`/`alarms` replace whole |
| DELETE | /tasks/{id} | to trash (with its subtasks) |
| POST | /tasks/{id}/complete | `{done: boolean}` → `{task, next?: Task}` (`next` = the new occurrence of a repeating task) |
| POST | /tasks/{id}/move | `{list_id?, section_id?, parent_id?, before_id?, after_id?}` (sort within the destination; `section_id: null` = no section, `parent_id: null` = top level) |
| POST | /tasks/batch | `{ids, set?: {status?, list_id?, section_id?, due?, start?, priority?, someday?, add_tags?, remove_tags?}, action?: 'delete'|'complete'|'reopen'}` → `{updated: number}` |
| POST | /tasks/{id}/attachments | multipart `files` → Task |
| GET | /tasks/{id}/attachments/{aid} | the file |
| DELETE | /tasks/{id}/attachments/{aid} | |
| GET | /tasks/{id}/ics | the raw iCalendar |
| POST | /tasks/{id}/duplicate | |

GET /tasks query: `view=inbox|today|upcoming|anytime|someday|logbook|all`, `list_id`, `section_id`,
`tag`, `status=open|done|all` (view implies it), `priority`, `q` (full-text), `due_from`, `due_to`,
`start_from`, `start_to`, `completed_on=YYYY-MM-DD`, `modified_since=RFC3339`, `parent_id`
(`0` = top level only), `nest=1` (one level of subtasks inline), `limit` (default 500), `offset`.
Views sort: Today by overdue first then sort_order; Upcoming by date; Logbook by completed_at desc;
lists by section then sort_order. `groups` is filled for upcoming (per day), anytime/someday (per
list), logbook (per day or list), a list (per section).

Everything in `/tasks` answers also for a saved filter: `GET /tasks?filter_id=ID`.

## Tags, filters, search, calendar, trash, activity

| Method | Path | Notes |
|---|---|---|
| GET | /tags | `[{tag, count}]` |
| PUT | /tags/{tag} | `{name}` rename everywhere |
| DELETE | /tags/{tag} | remove from every task |
| GET | /filters | |
| POST | /filters | `{name, icon?, query}` |
| PUT | /filters/{id} | |
| DELETE | /filters/{id} | |
| POST | /filters/reorder | `{ids}` |
| GET | /search?q=&limit= | `Task[]` with `snippet` (FTS5, prefix match, accents folded) |
| GET | /calendar?from=YYYY-MM-DD&to=YYYY-MM-DD | `{days: {"2026-10-21": {start: Task[], due: Task[], done: Task[]}}}` |
| GET | /trash | `Task[]` (deleted_at set) |
| POST | /trash/{id}/restore | |
| DELETE | /trash/{id} | forever |
| DELETE | /trash | empty |
| GET | /activity?task_id=&limit= | `[{id, task_id, task_title, kind, detail, at}]` kinds: created, updated, completed, reopened, moved, deleted, restored, synced, reminded |

## Sync with Nextcloud

| Method | Path | Notes |
|---|---|---|
| GET | /sync/nextcloud | `{mode: 'off'|'import'|'two_way', via: 'core'|'direct', url, user, password_set, core_available: boolean, core_error?, status: {running, last_run, last_error, lists: [{name, href, tasks, mapped_list_id}], pulled, pushed}, interval_min}` |
| PUT | /sync/nextcloud | `{mode, via, url?, user?, password?, interval_min?}` password empty keeps |
| POST | /sync/nextcloud/test | tries the account → `{ok, calendars: [{name, href, tasks}]}` |
| POST | /sync/nextcloud/run | run now → status |

## Feeds and CalDAV info

| Method | Path | Notes |
|---|---|---|
| GET | /feeds | `{ics_url, webcal_url, caldav_url, caldav_nextcloud_url, username, apps: [{name, how}]}` |
| POST | /feeds/rotate | new feed token |
| GET | /tokens, POST /tokens `{name}` → `{token, info}`, DELETE /tokens/{id} | API tokens double as CalDAV app passwords |

Outside `/api`: `GET /feed/{token}.ics` (public by token, read-only, `?done=0|1&days=N`), CalDAV at
`/dav/…` and `/remote.php/dav/…`, `/.well-known/caldav`.

## Users (admin), notifications, data

| Method | Path | Notes |
|---|---|---|
| GET/POST | /users | members see only themselves |
| PUT/DELETE | /users/{id} | |
| POST | /notify/test | sends a test to the person's notification route |
| GET | /backup | SQLite snapshot (admin) |
| GET | /export.json | this person's lists, sections, tasks |
| GET | /export.ics?list_id= | one list (or all) as iCalendar |
| POST | /import | multipart `file`: `.ics` (VTODOs into `list_id` or a new list named after X-WR-CALNAME) or a Pocket Tasks `.json` export |
| POST | /client-error | the UI reports a crash |

## Link scopes (Pocket Core)

`tasks:read` → GET /tasks, /tasks/{id}, /lists, /tags, /calendar, /dashboard, /search.
`tasks:write` → POST /tasks, /tasks/quick, PUT /tasks/{id}, POST /tasks/{id}/complete.
