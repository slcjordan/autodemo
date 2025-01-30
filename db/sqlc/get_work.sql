-- GetWork fetches a single book.
-- name: GetWork :one

SELECT id, status
FROM work_queue
WHERE domain=@domain AND project=@project AND status!=@status;
