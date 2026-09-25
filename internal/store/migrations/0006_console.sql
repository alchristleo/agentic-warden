-- Console sessions: only the token's hash is stored, as for machine
-- credentials. The audit log records every admin change; its CHECKs make an
-- event without an actor or action fail inside the change's transaction.
CREATE TABLE IF NOT EXISTS console_sessions (
    token_hash   text PRIMARY KEY CHECK (token_hash <> ''),
    user_name    text NOT NULL CHECK (user_name <> ''),
    created_at   timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS console_sessions_user ON console_sessions (user_name);

CREATE TABLE IF NOT EXISTS audit_events (
    id     bigserial PRIMARY KEY,
    at     timestamptz NOT NULL,
    actor  text NOT NULL CHECK (actor <> ''),
    action text NOT NULL CHECK (action <> ''),
    target text NOT NULL DEFAULT '',
    detail jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS audit_events_at ON audit_events (at);
