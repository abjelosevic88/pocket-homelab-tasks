# Pocket Tasks — design

Pocket Tasks is the task backend of a Pocket Box. One Go binary, one SQLite file, one volume.
It replaces Nextcloud Tasks and keeps every client that worked with it working.

## What Nextcloud Tasks is, and what it lacks

Nextcloud Tasks is a thin web UI over Nextcloud's CalDAV server (SabreDAV): every task is a
VTODO in a calendar collection. That is its strength — Apple Reminders, BusyCal, Thunderbird,
Tasks.org and the Pocket Homelab app all sync it — and the reason it stops there:

- no sections inside a list, no areas grouping lists, no saved filters, no "someday" shelf;
- no full-text search, no quick-add with natural language, no keyboard-driven UI;
- recurring tasks only "fixed schedule"; no "repeat after I finish it";
- reminders fire on the device only; nothing on the server pushes a notification or a daily digest;
- no attachments, no activity log, no trash, no undo;
- tasks appear in the Nextcloud Calendar only (as VTODOs on their due day) — a Google Calendar,
  Outlook or most calendar apps never show them, and they cannot be moved from there;
- no API besides CalDAV; nothing for other Pocket apps.

## What Pocket Tasks does

1. **A real CalDAV server** (RFC 4791 + 6578 + the Apple and Nextcloud extensions clients
   rely on) at `/dav/` with an alias at `/remote.php/dav/` so clients that assume Nextcloud's path
   shape (the Pocket Homelab app) work by changing only the address. Each list is a VTODO
   collection; each task a `.ics` resource with the whole VCALENDAR kept as sent (unknown
   properties, VTIMEZONE, X-APPLE-* survive edits made here).
2. **Tasks on any calendar.** A read–write VEVENT collection "Tasks" per person mirrors every
   dated task as an event (timed with its estimate, or all day); overdue ones roll forward to
   today; finished ones can show on the day they were finished. Moving or resizing the event in a
   calendar app reschedules the task; a new event dropped into that calendar becomes a task. The
   same calendar is also a secret webcal/ICS feed for apps that only subscribe (Nextcloud
   Calendar, Google Calendar).
3. **Nextcloud import and two-way sync**, through the person's Nextcloud account in Pocket Core
   or typed in: lists ↔ calendars, tasks ↔ VTODOs by UID, etags both ways, last-modified wins on a
   clash. Mode *off*, *import once* or *two-way*, so nothing has to change on the phones on day one.
4. **The Things model on top of iCalendar**: Inbox, Today, Upcoming, Anytime, Someday, Logbook;
   areas → lists → sections → tasks → subtasks; *when* (DTSTART) separate from *deadline* (DUE);
   estimates; tags; priority; repeats fixed or after completion; alarms; attachments; notes in
   Markdown; activity log; trash. Extra fields ride in `X-POCKET-*` properties so they survive a
   round trip through any client.
5. **Quick add in plain words**: "Call the bank tomorrow 9am !high #money /Home every month".
6. **Server-side reminders**: alarms and a morning digest pushed through Apprise, ntfy or a
   webhook; Pocket Core events (`tasks.created|updated|completed|deleted|reminder`).
7. **Pocket Core**: registration, OIDC sign-in, offers `tasks:read` / `tasks:write` to other
   Pocket apps (Day's journal, Notes' "make this line a task"), per-person service connection to
   Nextcloud (Core | Direct | Off).
8. Strictly per person. Admins see nobody else's tasks. The only cross-person path is sharing a
   list with a member (v0.2).

## Data model

```
users, sessions, api_tokens, settings, user_settings      (as in every Pocket app)
areas       id user_id name color sort_order
lists       id user_id uid name color description sort_order area_id is_inbox archived
            sync_seq (per-list monotonic counter for CalDAV sync-token / ctag) nc_href nc_ctag nc_token
sections    id user_id list_id name sort_order
tasks       id user_id list_id section_id parent_id uid href etag ics
            title notes status(open|in_progress|done|cancelled) completed_at percent priority(0-9)
            start (text: 2026-10-21 or 2026-10-21T09:00:00 floating local) due (same) tz
            estimate_min rrule repeat_from(due|completion) someday sort_order location url
            seq (list-wide change counter) created_at updated_at deleted_at
            nc_href nc_etag nc_seq (what Nextcloud last had)
task_tags   user_id task_id tag
alarms      id task_id trigger (ISO-8601 duration relative to due/start, or absolute Z time) description fired_at
attachments id user_id task_id name size type path
filters     id user_id name icon query(json) sort_order
activity    id user_id task_id kind detail(json) at
tasks_fts   FTS5 over title, notes, tags
```

The ICS blob is the source of truth for CalDAV; the columns are the index the UI and the API work
on. A write from the UI rewrites the *managed* properties of the blob and leaves the rest. A write
from CalDAV parses the blob into the columns. `seq` is bumped on every change; a deleted task keeps
its row (deleted_at) as a tombstone so sync-collection can report it, and is purged after 30 days.

## Views

- Inbox: open tasks in the inbox list (every person has one; it cannot be deleted).
- Today: open, not someday, `start <= today` or `due <= today` (overdue included, marked).
- Upcoming: open with a start or due after today, grouped by day; repeats shown once.
- Anytime: open, not someday, no start date (any deadline), grouped by list.
- Someday: the someday flag, grouped by list.
- Logbook: done or cancelled, newest first, by day.
- Calendar: a month/week grid of starts, dues and completions; drag to reschedule.
- A list: its sections in order, open tasks, a collapsed "done" tail; subtasks nested.

## The calendar mirror, in detail

For each person, collection `/dav/calendars/<user>/tasks-calendar/` (VEVENT) and the feed
`/feed/<token>.ics`. One event per task that has a start or a due:

- timed start → `DTSTART` at that time, `DTEND` = start + estimate (default 30 min);
- all-day start → all-day event; due only → all-day event on the due day (timed due → timed);
- overdue and open → shown today (setting), `SUMMARY` prefixed "⚠︎";
- done → event on the completion day with "✓" (setting, default on for the feed, off for CalDAV);
- `UID` = task uid + `@tasks-calendar`, `X-POCKET-TASK` = task id, `URL` deep link to the task.

PUT on such an event: DTSTART/DTEND change → start (and estimate) of the task; SUMMARY change →
title; everything else is ignored. PUT of a *new* UID → a task in the inbox with that start.
DELETE → the task loses its start (it is not deleted).

## Nextcloud sync

Per person, `ncsync` keeps a map list ↔ calendar (`nc_href`), task ↔ resource (`nc_href`,
`nc_etag`). Every `sync_minutes` (default 2) and on demand:

1. PROPFIND the calendar home; new VTODO calendars become lists (Deck boards skipped: app-generated);
   lists made here get `MKCALENDAR`'d there (two-way mode).
2. Per calendar: `sync-collection` with the stored token (fallback: ctag + etag diff). Changed
   resources are fetched with `calendar-multiget`; new UIDs become tasks, known ones update when the
   remote etag changed and the local task was not changed since its last sync, else the newer
   LAST-MODIFIED wins. Gone resources delete the local task (unless changed locally since).
3. Local changes (`seq > nc_seq`) are PUT with `If-Match`; 412 → fetch, resolve, retry once. Local
   deletes DELETE there. New local tasks PUT with `If-None-Match: *`.

Import mode runs step 1–2 once and stops. Off mode does nothing and keeps the mapping.
