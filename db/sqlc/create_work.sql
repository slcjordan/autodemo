-- MaybeCreateWork inserts work into a work queue.
-- name: MaybeCreateWork :exec

INSERT OR IGNORE INTO work_queue (domain, project, data) VALUES (@domain, @project, @data);
