-- Schema creates the required sqlite schema.
-- name: Schema :exec

CREATE TABLE IF NOT EXISTS work_queue (
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  data BLOB NOT NULL,
  domain TEXT NOT NULL,
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending',
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  error TEXT NOT NULL DEFAULT '',
  UNIQUE (data, project)
);
