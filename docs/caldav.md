# CalDAV, the Tasks calendar and the feed

## Connecting an app

Address: `https://tasks.example.com/dav/` (or the Nextcloud-shaped `https://tasks.example.com/remote.php/dav/calendars/<name>/` for apps that assume Nextcloud). User name: your CalDAV name (Settings → Profile; the part of your email before `@` by default). Password: an API token from Settings → Apps & API (or your account password with local accounts). Make one token per device so it can be revoked alone.

| App | How |
|---|---|
| iPhone / iPad Reminders | Settings → Apps → Calendar → Accounts → Add Account → Other → Add CalDAV Account. Server `tasks.example.com`, your name and token. Lists appear in Reminders; a task with a time shows in Calendar too |
| macOS Reminders | System Settings → Internet Accounts → Add Other Account → CalDAV. Account type Manual |
| BusyCal / Fantastical | Add a CalDAV account with the address above |
| Thunderbird | New calendar → On the network → the `/dav/` address; pick the lists |
| Tasks.org (Android) | Settings → Synchronisation → CalDAV |
| Nextcloud Calendar | Calendar → + New calendar → New subscription from link → paste the **webcal** feed link from Settings → Calendar & apps |
| Google Calendar | Other calendars → From URL → the feed link (Google refreshes once a day or so) |

The Nextcloud Tasks web app cannot subscribe to another server; the feed covers that case.

## What the server speaks

RFC 4791 (calendar-access), RFC 6578 (`sync-collection`), `calendar-query` with component, time-range and property filters, `calendar-multiget`, `MKCALENDAR` and extended `MKCOL`, `PROPPATCH` of name, colour, order and description, Apple's `calendar-color` / `calendar-order`, `getctag`, `current-user-principal`, `calendar-home-set`. Conditional `PUT` and `DELETE` with `If-Match` / `If-None-Match`. Each task is one `.ics`; the server keeps the text a client sent and rewrites only the properties it manages, so `X-APPLE-*`, `VTIMEZONE` and the like survive.

Extra fields ride in `X-POCKET-*` properties: `X-POCKET-ESTIMATE` (minutes), `X-POCKET-REPEAT-FROM:completion`, `X-POCKET-SOMEDAY:1`, `X-POCKET-SECTION` (name), `X-POCKET-ORDER`.

## The Tasks calendar

`/dav/calendars/<name>/tasks-calendar/` is a VEVENT collection every calendar app can add. One event per dated task: timed tasks with their estimate (default 30 min), all-day otherwise; a due-only task on its due day (Settings); overdue open tasks roll to today with ⚠︎; finished ones on the day they were finished with ✓ (Settings, off by default for CalDAV, on for the feed).

Moving or resizing the event reschedules the task (start and estimate); renaming it renames the task; deleting it only removes the start date. A new event created in that calendar becomes a task in the default list.

## The feed

`/feed/<secret>.ics` is the same calendar for apps that only subscribe: read-only, no login, refreshed every 15 minutes by well-behaved clients. `?done=0` hides finished tasks, `?days=30` limits how far back finished ones go. Rotate the secret in Settings if it leaks.
