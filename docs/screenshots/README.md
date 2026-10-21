# README screenshots

These are browser captures of the built app with its built-in demo data. Desktop
views are 1280 × 860; phone views are 390 × 844 at 2× scale. Dates follow the day
the demo database is created. No live account or external service is used.

To refresh them, run from the repository root (Go, npm, Node 22+ and Docker):

```sh
make web build
shots_data=$(mktemp -d)
HOST=127.0.0.1 PORT=18197 BASE_URL=https://tasks.example.com \
  DATA_DIR="$shots_data" AUTH_MODE=none DEMO_DATA=true TZ=Europe/Sarajevo \
  ./pocket-tasks
```

Leave that running. In another terminal at the repository root:

```sh
docker run -d --rm --name pocket-tasks-screenshot-chrome --network host --shm-size=1g \
  zenika/alpine-chrome:124 --no-sandbox --headless --disable-gpu \
  --disable-dev-shm-usage --hide-scrollbars \
  --remote-debugging-port=19227 --remote-debugging-address=127.0.0.1 \
  --remote-allow-origins='*' about:blank
node scripts/screenshots.mjs
docker stop pocket-tasks-screenshot-chrome
```

Then stop the demo server with Ctrl-C. The temporary database can be discarded.
`BASE` and `CHROME` override the demo and DevTools URLs for the capture script.
An existing local Chrome with remote debugging enabled also works.

The script waits for each view's content and fonts, uses light and dark themes,
opens a task with subtasks, previews a quick-add phrase, expands the CalDAV client
instructions, and selects the Nextcloud two-way setup form without saving it.
It does not submit tasks, connect to Nextcloud or change the app's settings.
