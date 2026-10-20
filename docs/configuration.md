# Configuration

Everything is an environment variable. Nothing is required: `docker run` with one volume works.

| Variable | Default | What it does |
|---|---|---|
| `PORT`, `HOST` | `8080`, `0.0.0.0` | Where to listen |
| `BASE_URL` | | The public address (`https://tasks.example.com`). Needed for links in reminders, the CalDAV and feed addresses shown in Settings, OIDC callbacks |
| `DATA_DIR` | `./data` | Database, attachments, backups |
| `DB_PATH` | `$DATA_DIR/pocket-tasks.db` | |
| `TZ` | `UTC` | The box's time zone; each person may pick their own in Settings |
| `LOG_LEVEL`, `LOG_FORMAT` | `info`, `text` | `debug` logs every request; `json` for log shippers |
| `TRUST_PROXY` | `true` | Read the client address from `X-Forwarded-For` |
| `METRICS_ENABLED` | `true` | `/metrics` for Prometheus |
| `AUTH_MODE` | `local` | `local` (accounts and passwords), `proxy` (trust `AUTH_PROXY_HEADER`), `none` (everyone is the admin; demos only) |
| `AUTH_PROXY_HEADER`, `AUTH_PROXY_EMAIL_HEADER` | `Remote-User`, `Remote-Email` | For `AUTH_MODE=proxy` |
| `ADMIN_EMAIL`, `ADMIN_PASSWORD`, `ADMIN_NAME` | | Create the admin on first start instead of the setup screen |
| `SESSION_TTL` | `720h` | |
| `SECURE_COOKIES` | `false` | Set behind HTTPS |
| `OIDC_ISSUER`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`, `OIDC_NAME`, `OIDC_AUTO_CREATE` | | Sign in with any OpenID Connect provider. Not needed with Pocket Core |
| `CORE_URL`, `CORE_REGISTRATION_KEY`, `CORE_APP_URL` | | Pocket Core: registration, sign-in, the person's Nextcloud account, events |
| `NOTIFY_URL`, `NOTIFY_KIND` | | The owner's default route for reminders and the digest: an Apprise route (`http://apprise:8000/notify/tasks`), an ntfy topic URL or a webhook; `apprise` / `ntfy` / `webhook` |
| `NEXTCLOUD_URL`, `NEXTCLOUD_USER`, `NEXTCLOUD_PASSWORD` | | Seeded once into the owner's Nextcloud sync settings (mode stays *off* until switched on) |
| `MAX_ATTACHMENT_MB` | `50` | |
| `DEMO_DATA` | `false` | Seed a few made-up tasks into an empty box |

## Commands

```
pocket-tasks                       start
pocket-tasks backup [-o FILE]      consistent SQLite copy (VACUUM INTO)
pocket-tasks reset-password [-email USER] [-password NEW]
pocket-tasks version
```

## Files in the volume

- `pocket-tasks.db` (+ `-wal`, `-shm`): everything. Back it up with `pocket-tasks backup` or `GET /api/v1/backup`, never by copying a live file.
- `attachments/`: files kept with tasks.
- `backups/`: snapshots made by the backup command.
- `core.token`, `core.token.oidc.json`: what Pocket Core issued on registration.
