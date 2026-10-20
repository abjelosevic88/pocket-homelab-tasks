CREATE TABLE users (
  id            INTEGER PRIMARY KEY,
  email         TEXT NOT NULL UNIQUE,
  name          TEXT NOT NULL DEFAULT '',
  username      TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'admin',
  feed_token    TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT
);
CREATE UNIQUE INDEX users_username ON users(username) WHERE username <> '';
CREATE UNIQUE INDEX users_feed_token ON users(feed_token) WHERE feed_token <> '';

CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE api_tokens (
  id           INTEGER PRIMARY KEY,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL DEFAULT '',
  token_hash   TEXT NOT NULL UNIQUE,
  prefix       TEXT NOT NULL DEFAULT '',
  last_used_at TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Box-wide settings (title, setup_complete, seq). Everything a person
-- chooses is in user_settings under their id.
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);
CREATE TABLE user_settings (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key     TEXT NOT NULL,
  value   TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, key)
);

CREATE TABLE areas (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  color      TEXT NOT NULL DEFAULT '',
  sort_order REAL NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- A list is a CalDAV collection of VTODOs (uid = its path segment).
CREATE TABLE lists (
  id          INTEGER PRIMARY KEY,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  uid         TEXT NOT NULL,
  name        TEXT NOT NULL,
  color       TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  sort_order  REAL NOT NULL DEFAULT 0,
  area_id     INTEGER REFERENCES areas(id) ON DELETE SET NULL,
  is_inbox    INTEGER NOT NULL DEFAULT 0,
  archived    INTEGER NOT NULL DEFAULT 0,
  seq         INTEGER NOT NULL DEFAULT 0,
  nc_href     TEXT NOT NULL DEFAULT '',
  nc_token    TEXT NOT NULL DEFAULT '',
  nc_ctag     TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE (user_id, uid)
);

CREATE TABLE sections (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  list_id    INTEGER NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  sort_order REAL NOT NULL DEFAULT 0
);

-- A task is one .ics resource; ics is the source of truth for CalDAV, the
-- columns are the index the UI and the API use.
CREATE TABLE tasks (
  id           INTEGER PRIMARY KEY,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  list_id      INTEGER NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
  section_id   INTEGER REFERENCES sections(id) ON DELETE SET NULL,
  parent_id    INTEGER REFERENCES tasks(id) ON DELETE CASCADE,
  uid          TEXT NOT NULL,
  href         TEXT NOT NULL,
  etag         TEXT NOT NULL DEFAULT '',
  ics          TEXT NOT NULL DEFAULT '',
  title        TEXT NOT NULL DEFAULT '',
  notes        TEXT NOT NULL DEFAULT '',
  status       TEXT NOT NULL DEFAULT 'open',
  completed_at TEXT NOT NULL DEFAULT '',
  percent      INTEGER NOT NULL DEFAULT 0,
  priority     INTEGER NOT NULL DEFAULT 0,
  start        TEXT NOT NULL DEFAULT '',
  due          TEXT NOT NULL DEFAULT '',
  estimate_min INTEGER NOT NULL DEFAULT 0,
  rrule        TEXT NOT NULL DEFAULT '',
  repeat_from  TEXT NOT NULL DEFAULT '',
  someday      INTEGER NOT NULL DEFAULT 0,
  sort_order   REAL NOT NULL DEFAULT 0,
  location     TEXT NOT NULL DEFAULT '',
  url          TEXT NOT NULL DEFAULT '',
  seq          INTEGER NOT NULL DEFAULT 0,
  nc_href      TEXT NOT NULL DEFAULT '',
  nc_etag      TEXT NOT NULL DEFAULT '',
  nc_seq       INTEGER NOT NULL DEFAULT 0,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  deleted_at   TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX tasks_href ON tasks(list_id, href);
CREATE INDEX tasks_user ON tasks(user_id, deleted_at, status);
CREATE INDEX tasks_uid ON tasks(user_id, uid);
CREATE INDEX tasks_seq ON tasks(list_id, seq);
CREATE INDEX tasks_parent ON tasks(parent_id);
CREATE INDEX tasks_nc ON tasks(nc_href) WHERE nc_href <> '';

-- Where a resource used to be, for CalDAV sync-collection after a delete or a move.
CREATE TABLE tombstones (
  id      INTEGER PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  list_id INTEGER NOT NULL,
  href    TEXT NOT NULL,
  seq     INTEGER NOT NULL
);
CREATE INDEX tombstones_list ON tombstones(list_id, seq);

CREATE TABLE task_tags (
  task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL,
  tag     TEXT NOT NULL,
  PRIMARY KEY (task_id, tag)
);
CREATE INDEX task_tags_user ON task_tags(user_id, tag);

CREATE TABLE alarms (
  id          INTEGER PRIMARY KEY,
  user_id     INTEGER NOT NULL,
  task_id     INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  trigger     TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  fire_at     TEXT NOT NULL DEFAULT '',
  fired_at    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX alarms_fire ON alarms(fire_at) WHERE fire_at <> '' AND fired_at = '';

CREATE TABLE attachments (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL,
  task_id    INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  size       INTEGER NOT NULL DEFAULT 0,
  type       TEXT NOT NULL DEFAULT '',
  path       TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE filters (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  icon       TEXT NOT NULL DEFAULT '',
  query      TEXT NOT NULL DEFAULT '{}',
  sort_order REAL NOT NULL DEFAULT 0
);

CREATE TABLE activity (
  id      INTEGER PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  task_id INTEGER NOT NULL,
  kind    TEXT NOT NULL,
  detail  TEXT NOT NULL DEFAULT '',
  at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX activity_task ON activity(task_id, id);
CREATE INDEX activity_user ON activity(user_id, id);

CREATE VIRTUAL TABLE tasks_fts USING fts5(title, notes, tags, tokenize = 'unicode61 remove_diacritics 2');
