-- An enrollment token lets one machine enroll for one user, once. Only the
-- token's hash is stored; the plaintext is shown to the administrator once.
CREATE TABLE IF NOT EXISTS enrollment_tokens (
    hash       TEXT        PRIMARY KEY,
    "user"     TEXT        NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ
);

-- A machine holds one credential, stored as its hash, and fetches one user's
-- bundle with it. The credential hash is the lookup key on every fetch. Seq
-- is a store-assigned sequence, separate from the id, so enrollment order is
-- deterministic even when two machines share an enrolled_at timestamp.
CREATE TABLE IF NOT EXISTS machines (
    id                  TEXT        PRIMARY KEY,
    "user"              TEXT        NOT NULL,
    name                TEXT        NOT NULL DEFAULT '',
    os                  TEXT        NOT NULL DEFAULT '',
    credential_hash     TEXT        NOT NULL UNIQUE,
    enrolled_at         TIMESTAMPTZ NOT NULL,
    last_seen_at        TIMESTAMPTZ,
    last_bundle_version TEXT        NOT NULL DEFAULT '',
    seq                 BIGSERIAL   UNIQUE
);

CREATE INDEX IF NOT EXISTS machines_seq ON machines (seq);
