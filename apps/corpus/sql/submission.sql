-- name: ListClassificationSubmissions :many
SELECT s.*, r.country, r.rel FROM submission s JOIN run r ON r.id=s.run_id
WHERE s.stage='classify' ORDER BY s.created_at,s.id;

-- name: SaveClassificationSubmission :exec
INSERT INTO submission (id,run_id,stage,provider,project_id,screen_hash,recipe,families,item_list,item_list_hash,provider_job_id,state,created_at,submitted_at,deadline_at,budget,budget_micro_usd,reserved_micro_usd,cost_micro_usd,cost_settled,results,key_version,price_version)
VALUES ($1,$2,'classify',sqlc.narg(provider),sqlc.narg(project_id),$3,$4,$5,$6,$7,sqlc.narg(provider_job_id),$8,$9,sqlc.narg(submitted_at),sqlc.narg(deadline_at),$10,$11,$12,$13,$14,$15,$16,sqlc.narg(price_version))
ON CONFLICT (id) DO UPDATE SET provider_job_id=excluded.provider_job_id,state=excluded.state,submitted_at=excluded.submitted_at,deadline_at=excluded.deadline_at,cost_micro_usd=excluded.cost_micro_usd,cost_settled=excluded.cost_settled,results=excluded.results;
