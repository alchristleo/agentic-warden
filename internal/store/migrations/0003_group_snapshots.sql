-- A group snapshot is one export of identity-provider memberships. The
-- newest is current and replaces the previous one whole; older rows stay as
-- the audit trail. Members is the user -> groups map as JSON.
CREATE TABLE IF NOT EXISTS group_snapshots (
    seq        BIGSERIAL   PRIMARY KEY,
    source     TEXT        NOT NULL DEFAULT '',
    applied_by TEXT        NOT NULL DEFAULT '',
    synced_at  TIMESTAMPTZ NOT NULL,
    members    JSONB       NOT NULL
);
