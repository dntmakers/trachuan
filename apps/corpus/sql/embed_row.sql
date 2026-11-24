-- name: InsertEmbedRow :execrows
INSERT INTO embed_row (recipe_hash, input_sha256, artifact_id, row, token_count)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (recipe_hash, input_sha256) DO NOTHING;

-- name: MatchEmbedArtifact :one
SELECT EXISTS (
    SELECT 1 FROM artifact
    WHERE id = $1 AND stage = 'embed' AND recipe_hash = $2
        AND input_hash = $3 AND path = $4 AND sha256 = $5
);

-- name: RepairEmbedRow :execrows
UPDATE embed_row
SET artifact_id = sqlc.arg(artifact_id), row = sqlc.arg(row), token_count = sqlc.arg(token_count)
WHERE recipe_hash = sqlc.arg(recipe_hash) AND input_sha256 = sqlc.arg(input_sha256)
    AND artifact_id = sqlc.arg(old_artifact_id) AND row = sqlc.arg(old_row) AND token_count = sqlc.arg(old_token_count);

-- name: LookupEmbedRows :many
SELECT e.input_sha256, e.row, e.token_count, a.id AS artifact_id, a.input_hash, a.path, a.sha256,
    a.stage AS artifact_stage, a.recipe_hash AS artifact_recipe_hash
FROM embed_row e JOIN artifact a ON a.id = e.artifact_id
WHERE e.recipe_hash = sqlc.arg(recipe_hash) AND e.input_sha256 = ANY(sqlc.arg(input_hashes)::text[]);

-- name: ListEmbedArtifacts :many
SELECT id, input_hash, path, sha256 FROM artifact
WHERE stage = 'embed' AND recipe_hash = $1 ORDER BY id;
