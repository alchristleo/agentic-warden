-- The signing key a machine last presented, so an operator can tell when a
-- key rotation has reached every machine.
ALTER TABLE machines ADD COLUMN IF NOT EXISTS last_key_id text NOT NULL DEFAULT '';
