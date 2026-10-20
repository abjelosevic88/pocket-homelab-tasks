// Mirrors docs/api.md.
export interface User { id: number; email: string; name: string; username: string; role: 'admin' | 'member'; created_at: string }

export type TaskStatus = 'open' | 'in_progress' | 'done' | 'cancelled'
export interface Repeat { rrule: string; from: 'due' | 'completion' }
export interface Alarm { trigger: string; description?: string }
export interface Attachment { id: number; name: string; size: number; type: string; url: string }

export interface Task {
  id: number; uid: string; list_id: number; section_id: number | null; parent_id: number | null
  title: string; notes: string
  status: TaskStatus; completed_at: string | null; percent: number
  priority: number
  start: string | null; due: string | null
  estimate_min: number | null
  repeat: Repeat | null
  someday: boolean
  tags: string[]
  alarms: Alarm[]
  location: string; url: string
  sort_order: number
  subtasks?: Task[]
  subtask_count: number; subtask_done: number
  attachments: Attachment[]
  list?: { id: number; name: string; color: string }
  overdue: boolean
  etag: string; created_at: string; updated_at: string
  // GET /tasks/{id} only
  activity?: Activity[]
  // GET /search only
  snippet?: string
}

export interface List { id: number; uid: string; name: string; color: string; description: string; sort_order: number; area_id: number | null; is_inbox: boolean; archived: boolean; open_count: number; nc_href?: string; created_at: string }
export interface Area { id: number; name: string; color: string; sort_order: number }
export interface Section { id: number; list_id: number; name: string; sort_order: number }
export interface Filter { id: number; name: string; icon: string; sort_order: number; query: TaskQuery }
export interface TaskQuery { view?: string; list_id?: number; section_id?: number; tag?: string; tags?: string[]; status?: 'open' | 'done' | 'all'; priority?: number; q?: string; due_from?: string; due_to?: string; start_from?: string; start_to?: string; someday?: boolean; has_date?: boolean }

export interface TaskGroup { key: string; title: string; ids: number[] }
export interface TaskList { tasks: Task[]; total: number; groups?: TaskGroup[] }

export interface Parsed { title: string; due: string | null; start: string | null; list_id: number | null; tags: string[]; priority: number; repeat: Repeat | null; alarm: string | null }
export interface QuickAnswer { task: Task; parsed: Parsed }

export interface Activity { id: number; task_id: number; task_title: string; kind: 'created' | 'updated' | 'completed' | 'reopened' | 'moved' | 'deleted' | 'restored' | 'synced' | 'reminded'; detail: Record<string, unknown> | string | null; at: string }

export interface TagCount { tag: string; count: number }

export interface Dashboard { open: number; inbox: number; today: number; overdue: number; upcoming_week: number; someday: number; done_today: number; done_week: number; lists: number; next: Task[] }

export interface Settings {
  title: string
  week_start: 0 | 1
  default_list_id: number
  default_estimate_min: number
  day_start: string
  default_alarm: string
  show_done_in_lists: boolean
  logbook_group: 'day' | 'list'
  timezone: string
  mirror_enabled: boolean
  mirror_include_due: boolean
  mirror_rollover: boolean
  mirror_done: boolean
  mirror_done_caldav: boolean
  notify_kind: 'apprise' | 'ntfy' | 'webhook'; notify_url: string; notify_token_set: boolean; notify_env: boolean
  notify_alarms: boolean
  digest_time: string
  trash_days: number
  core: boolean
}

export interface SystemInfo { version: string; go: string; db_size: number; data_dir: string; counts: Record<string, number>; core: { configured: boolean; url: string; registered: boolean; error: string }; caldav: { url: string; username: string }; feed_url: string; base_url: string }

export interface CalendarDays { days: Record<string, { start: Task[]; due: Task[]; done: Task[] }> }

export interface NcSyncStatus { running: boolean; last_run: string | null; last_error: string; lists: { name: string; href: string; tasks: number; mapped_list_id: number | null }[]; pulled: number; pushed: number }
export interface NcSync { mode: 'off' | 'import' | 'two_way'; via: 'core' | 'direct'; url: string; user: string; password_set: boolean; core_available: boolean; core_error?: string; status: NcSyncStatus; interval_min: number }
export interface NcTest { ok: boolean; calendars: { name: string; href: string; tasks: number }[] }

export interface Feeds { ics_url: string; webcal_url: string; caldav_url: string; caldav_nextcloud_url: string; username: string; apps: { name: string; how: string }[] }

export interface APIToken { id: number; name: string; prefix: string; last_used_at?: string; created_at: string }

export interface AuthStatus { needs_setup: boolean; auth_mode: string; authenticated: boolean; user: User | null; version: string; title: string; oidc?: boolean; oidc_name?: string }
