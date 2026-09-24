-- SCIM provisioning. The identity provider owns these rows: users and
-- groups it created, and which users are in which group. Names are unique
-- ignoring case because the core schema says userName is not case-exact
-- and because a policy targets a group by name.
CREATE TABLE IF NOT EXISTS scim_users (
    id          TEXT        PRIMARY KEY,
    user_name   TEXT        NOT NULL,
    external_id TEXT        NOT NULL DEFAULT '',
    active      BOOLEAN     NOT NULL,
    created     TIMESTAMPTZ NOT NULL,
    modified    TIMESTAMPTZ NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS scim_users_user_name ON scim_users (lower(user_name));

CREATE TABLE IF NOT EXISTS scim_groups (
    id           TEXT        PRIMARY KEY,
    display_name TEXT        NOT NULL,
    external_id  TEXT        NOT NULL DEFAULT '',
    created      TIMESTAMPTZ NOT NULL,
    modified     TIMESTAMPTZ NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS scim_groups_display_name ON scim_groups (lower(display_name));

CREATE TABLE IF NOT EXISTS scim_members (
    group_id TEXT NOT NULL REFERENCES scim_groups (id) ON DELETE CASCADE,
    user_id  TEXT NOT NULL REFERENCES scim_users (id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX IF NOT EXISTS scim_members_user ON scim_members (user_id);
