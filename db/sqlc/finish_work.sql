-- FinishWork fetches a single book.
-- name: FinishWork :one

UPDATE work_queue
SET status = 'done', updated_at = CURRENT_TIMESTAMP
WHERE id = @id
RETURNING data;
