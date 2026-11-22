-- +goose Up
ALTER TABLE submission
    ADD COLUMN key_version integer NOT NULL DEFAULT 0,
    ADD COLUMN price_version text,
    ADD CONSTRAINT submission_classification_versions CHECK (
        (key_version = 0 AND price_version IS NULL) OR
        (key_version = 1 AND price_version IS NOT NULL AND price_version <> ''));

CREATE TABLE classification_attempt (
    submission_id text COLLATE "C" NOT NULL REFERENCES submission(id),
    item_index integer NOT NULL CHECK (item_index BETWEEN 0 AND 99999),
    request_key text COLLATE "C" NOT NULL CHECK (request_key ~ '^[0-9a-f]{64}$'),
    outcome text NOT NULL CHECK (outcome IN ('final','failed')),
    cost_micro_usd bigint CHECK (cost_micro_usd >= 0),
    price_version text NOT NULL CHECK (price_version <> ''),
    PRIMARY KEY (submission_id, item_index)
);
CREATE INDEX classification_attempt_key ON classification_attempt (request_key);

-- +goose Down
DROP TABLE classification_attempt;
ALTER TABLE submission
    DROP CONSTRAINT submission_classification_versions,
    DROP COLUMN price_version,
    DROP COLUMN key_version;
