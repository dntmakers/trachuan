-- +goose Up
ALTER TABLE submission
    DROP CONSTRAINT submission_budget_check,
    DROP CONSTRAINT submission_budget_micro_usd_check,
    ADD CONSTRAINT submission_budget_pair_check CHECK ((budget, budget_micro_usd) IN (('pilot',15000000),('trial',200000000),('full',100000000),('cap',150000000)));

-- +goose Down
ALTER TABLE submission
    DROP CONSTRAINT submission_budget_pair_check,
    ADD CONSTRAINT submission_budget_check CHECK (budget IN ('pilot','full','cap')),
    ADD CONSTRAINT submission_budget_micro_usd_check CHECK (budget_micro_usd IN (15000000,100000000,150000000));
