-- ErrorWork fetches a single book.
-- name: ErrorWork :exec

UPDATE work_queue
SET status = 'done', error=@error, updated_at = CURRENT_TIMESTAMP
WHERE id = @id;
