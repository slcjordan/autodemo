-- PostprocessWork fetches a single book.
-- name: PostprocessWork :one

UPDATE work_queue
SET status = 'postprocessing', updated_at = CURRENT_TIMESTAMP
WHERE id = @id
RETURNING data;
