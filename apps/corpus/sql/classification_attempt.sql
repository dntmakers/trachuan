-- name: DeleteClassificationAttemptIndex :exec
DELETE FROM classification_attempt;

-- name: IndexClassificationAttempt :exec
INSERT INTO classification_attempt (submission_id,item_index,request_key,outcome,cost_micro_usd,price_version)
VALUES ($1,$2,$3,$4,sqlc.narg(cost_micro_usd),$5)
ON CONFLICT (submission_id,item_index) DO NOTHING;
