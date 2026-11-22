-- +goose Up
ALTER TABLE submission
    ALTER COLUMN provider DROP NOT NULL,
    DROP CONSTRAINT submission_provider_check,
    DROP CONSTRAINT submission_budget_pair_check,
    ADD COLUMN project_id text,
    ADD CONSTRAINT submission_provider_budget_project_check CHECK (
        ((provider IS NULL OR provider = 'anthropic') AND project_id IS NULL AND (budget, budget_micro_usd) IN (('pilot',15000000),('trial',200000000),('full',100000000),('cap',150000000)))
        OR (COALESCE(provider = 'openai', false) AND project_id IS NOT NULL AND project_id ~ '^[a-zA-Z0-9_-]{1,128}$' AND (budget, budget_micro_usd) IN (('openai-trial',40000000),('openai-full',175000000)))
    );

-- +goose Down
UPDATE submission SET provider = 'anthropic' WHERE provider IS NULL;
ALTER TABLE submission
    DROP CONSTRAINT submission_provider_budget_project_check,
    DROP COLUMN project_id,
    ALTER COLUMN provider SET NOT NULL,
    ADD CONSTRAINT submission_provider_check CHECK (provider = 'anthropic'),
    ADD CONSTRAINT submission_budget_pair_check CHECK ((budget, budget_micro_usd) IN (('pilot',15000000),('trial',200000000),('full',100000000),('cap',150000000)));
