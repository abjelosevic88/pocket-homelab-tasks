<p align="center"><img src="web/public/favicon.svg" width="72" alt=""></p>
<h1 align="center">Pocket Tasks</h1>
<p align="center">Self-hosted tasks with a real CalDAV server, every dated task on any calendar, and the Nextcloud Tasks you already have imported or kept in sync.<br>One Go binary, one SQLite file, one volume.</p>

Pocket Tasks is what Nextcloud Tasks would be if it were an app of its own: the same open standard
underneath (every task a VTODO, every list a CalDAV collection, so Apple Reminders, BusyCal,
Thunderbird, Tasks.org and the Pocket Homelab app sync it) with the things the Nextcloud app
never got — sections inside lists, areas, Inbox / Today / Upcoming / Anytime / Someday / Logbook,
deadlines separate from "when", estimates, repeat *after I finish it*, saved filters, full-text
search, quick add in plain words, attachments, an activity log, a trash, reminders pushed from the
server, a morning digest, and a calendar every calendar app can show and move tasks on.

<p align="center"><img src="docs/screenshots/today.png" width="900" alt="Today: overdue tasks, a repeating household task and a scheduled server task, with areas, lists and tags in the sidebar"></p>

## Screenshots

<table>
  <tr>
    <td width="50%"><img src="docs/screenshots/list.png" alt="Server rebuild list with Plan and Build sections, priorities, tags, dates and subtask counts"></td>
    <td width="50%"><img src="docs/screenshots/task.png" alt="Task details beside the list: schedule, estimate, reminder and three subtasks"></td>
  </tr>
  <tr>
    <td align="center"><sub>Areas, lists and sections keep projects organised.</sub></td>
    <td align="center"><sub>When, deadline, estimate, reminders and subtasks in one panel.</sub></td>
  </tr>
  <tr>
    <td width="50%"><img src="docs/screenshots/calendar.png" alt="Month calendar with scheduled tasks, deadlines and a completed task, coloured by list"></td>
    <td width="50%"><img src="docs/screenshots/quick-add.png" alt="Quick add previews a plain-language task with tomorrow's time, list, tag, priority, monthly repeat and reminder"></td>
  </tr>
  <tr>
    <td align="center"><sub>Dated tasks on a calendar; drag to reschedule.</sub></td>
    <td align="center"><sub>Quick add in plain words, with a preview before you commit.</sub></td>
  </tr>
  <tr>
    <td width="50%"><img src="docs/screenshots/calendar-apps.png" alt="Calendar and apps settings with CalDAV addresses and instructions for connecting calendar clients"></td>
    <td width="50%"><img src="docs/screenshots/nextcloud.png" alt="Nextcloud Tasks setup with import and two-way sync modes, account fields and sync interval"></td>
  </tr>
  <tr>
    <td align="center"><sub>Connect your calendar and task apps over CalDAV.</sub></td>
    <td align="center"><sub>Import Nextcloud Tasks once, or keep both in sync.</sub></td>
  </tr>
</table>

<p align="center">
  <img src="docs/screenshots/mobile-today.png" width="260" alt="Today on a phone, with overdue and scheduled tasks, the menu button and the new-task button">&nbsp;&nbsp;
  <img src="docs/screenshots/mobile-task.png" width="260" alt="Task details on a phone, with scheduling, estimate, priority and reminder controls">
</p>

<p align="center"><img src="docs/screenshots/dark.png" width="900" alt="The Server rebuild list in dark mode, with Plan and Build sections"></p>

All screenshots use the built-in demo data. [How to refresh them](docs/screenshots/README.md).

## What it does

**Tasks the way you think about them**
- Areas → lists → sections → tasks → subtasks. Inbox for whatever comes in; Today, Upcoming,
  Anytime, Someday and the Logbook as smart views; tags; saved filters; archive for old lists.
- *When* (the day or moment you plan to do it) and *Deadline* are separate. Overdue is marked and
  rolls into Today.
- Priority, estimate, notes in Markdown, location, URL, attachments, alarms.
- Repeats on a fixed schedule (`every 2 weeks on Monday`, `last Friday of the month`) or
  *after completion* (`3 days after I water the plants`). Finishing one leaves it in the Logbook and
  makes the next.
- Quick add in plain words: `Call the bank tomorrow 9am !high #money /Home every month *30m`.
  The preview shows what was understood before you press Enter.
- Keyboard-driven: `n` new, `/` quick add, `⌘K` find, `t` today, `d` deadline, `1 2 3` priority,
  `s` someday, `m` move, Space done, `⌘⇧N` new task anywhere, `[` sidebar, `?` all shortcuts;
  multi-select with a batch bar, drag to reorder or move.
- Light and dark themes that follow the system (or set either from the avatar menu); a sidebar that
  becomes a drawer on phones, with a new-task button at the bottom.
- Full-text search (SQLite FTS5, accents folded), trash with restore, an activity log per task.

**A real CalDAV server**
- RFC 4791 + 6578 with the Apple and Nextcloud extensions clients rely on: `sync-collection`,
  `calendar-query`, `calendar-multiget`, `MKCALENDAR`, `PROPPATCH`, conditional `PUT`/`DELETE`,
  colours and order. Each task is kept as the client sent it; only the properties this app manages
  are rewritten, so `X-APPLE-*`, alarms with their ids and `VTIMEZONE` survive.
- `/dav/` and a `/remote.php/dav/` alias, so apps written for Nextcloud work by changing the address.
- Reminders on iPhone and Mac, BusyCal, Fantastical, Thunderbird, Tasks.org: an API token is the
  app password. See [docs/caldav.md](docs/caldav.md).

**Tasks on any calendar**
- A VEVENT calendar "Tasks" per person: timed tasks with their estimate, all-day otherwise,
  due-only tasks on their day, overdue ones rolled to today with ⚠︎, finished ones with ✓ on the day
  they were finished. Move or resize the event in iOS Calendar, BusyCal or Thunderbird and the task
  is rescheduled; drop a new event into that calendar and it becomes a task.
- The same calendar as a secret webcal/ICS feed for Nextcloud Calendar, Google Calendar and
  anything else that only subscribes.

**Nextcloud Tasks: import, or keep both**
- Point it at your Nextcloud (through Pocket Core, nothing to type, or an app password) and
  *import once* — lists, tasks, subtasks, alarms, repeats, done history — or run *two-way*: edits
  made here reach Nextcloud and the phones still synced to it within minutes, and theirs come back.
  By UID and etag; the newer edit wins on a clash. Deck boards are left to Deck.

**Reminders from the server**
- Alarms fire through Apprise, ntfy or a webhook even when no phone is synced; a morning digest
  lists today's and overdue tasks. Every change is a Pocket Core event
  (`tasks.created|updated|completed|deleted|reminder`).

**Household**
- Strictly per person: admins see nobody else's tasks. Sign in with Pocket Core (or any OpenID
  Connect provider), a password, or a trusted proxy header.
- Other Pocket apps act for a person through Core links (`tasks:read`, `tasks:write`): Pocket Day's
  journal, Pocket Notes' "make this line a task".

## Run it

```sh
docker run -d --name pocket-tasks -p 8080:8080 -v ./data:/data \
  -e TZ=Europe/Berlin -e BASE_URL=https://tasks.example.com -e SECURE_COOKIES=true \
  ghcr.io/abjelosevic88/pocket-homelab-tasks:latest
```

or `docker compose up -d` with the included file, or the single binary from the releases page.
Open it, create the admin, and:

1. **Settings → Calendar & apps** shows the CalDAV address, your name and per-app instructions;
   **Apps & API** makes the token that is the app password.
2. **Settings → Nextcloud sync** imports what you have (or keeps syncing).
3. **Settings → Notifications** for reminders on the server; **Calendar & apps** for the feed link.

Configuration: [docs/configuration.md](docs/configuration.md). API: [docs/api.md](docs/api.md).
Design: [docs/design.md](docs/design.md).

## Build

```sh
make web build      # React UI, then the Go binary with it embedded
make dev            # API on :8080 with demo data, Vite on :5173
make test
```

## License

MIT
