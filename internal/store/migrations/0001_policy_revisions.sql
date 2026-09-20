-- Policy is append-only: a revision is written once and never edited, so the
-- current policy is a lookup and a rollback is re-applying an earlier one.
CREATE TABLE IF NOT EXISTS policy_revisions (
    seq        BIGSERIAL PRIMARY KEY,
    version    TEXT        NOT NULL UNIQUE,
    rule_set   JSONB       NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    created_by TEXT        NOT NULL DEFAULT ''
);

-- The current revision is the highest sequence, read on every policy request.
CREATE INDEX IF NOT EXISTS policy_revisions_seq_desc ON policy_revisions (seq DESC);
