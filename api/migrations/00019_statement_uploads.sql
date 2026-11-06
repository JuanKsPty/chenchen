-- A statement Claude sends from the owner's computer (ADR-0004): the file the
-- bank itself produced, never a number Claude wrote. Claude asks for a place
-- to put it (prepare_import), sends the bytes to a one-use address, and asks
-- how it went (import_status).
--
-- The address is the capability: 32 random bytes, of which only the hash is
-- kept, good for ten minutes and for one file of exactly the size and SHA-256
-- declared. A file enters on its own only when it continues an account the
-- owner already has and agrees with it; anything else waits here, with its
-- bytes, for the owner to look at in the app. Nothing that arrives this way
-- ever creates an account.
--
-- Every row belongs to a user and goes with them, and with the account it
-- turned out to be for.

-- +goose Up

-- An upload names the run that imported it, together with its owner.
ALTER TABLE import_runs ADD CONSTRAINT import_runs_id_user_key UNIQUE (id, user_id);

CREATE TABLE statement_uploads (
    id            UUID        PRIMARY KEY,
    user_id       UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- The connection that asked for it; it outlives the connection.
    grant_id      UUID,
    token_hash    TEXT        NOT NULL UNIQUE,
    filename      TEXT        NOT NULL CHECK (char_length(filename) BETWEEN 1 AND 255),
    sha256        TEXT        NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    size          BIGINT      NOT NULL CHECK (size BETWEEN 1 AND 20971520),
    -- The file, kept only while the owner may still want it: from a review
    -- until it is imported or dismissed, or for thirty days.
    data          BYTEA,
    status        TEXT        NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'imported', 'unchanged', 'needs_review', 'unreadable',
                                    'dismissed', 'expired')),
    -- Why it waits, in the owner's words; empty otherwise.
    reason        TEXT        NOT NULL DEFAULT '' CHECK (char_length(reason) <= 500),
    account_id    UUID,
    import_run_id UUID,
    -- When its bytes arrived: the address is spent from then on.
    received_at   TIMESTAMPTZ,
    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (id, user_id),
    FOREIGN KEY (grant_id, user_id) REFERENCES oauth_grants (id, user_id) ON DELETE SET NULL (grant_id),
    FOREIGN KEY (account_id, user_id) REFERENCES accounts (id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (import_run_id, user_id) REFERENCES import_runs (id, user_id) ON DELETE CASCADE,
    CONSTRAINT statement_uploads_data_is_declared CHECK (data IS NULL OR octet_length(data) = size)
);
CREATE INDEX statement_uploads_user_idx ON statement_uploads (user_id, created_at DESC);
-- What housekeeping walks: the open ones, and the ones still holding a file.
CREATE INDEX statement_uploads_open_idx ON statement_uploads (status, created_at)
    WHERE status IN ('pending', 'needs_review', 'unreadable');

-- +goose Down
DROP TABLE IF EXISTS statement_uploads;
ALTER TABLE import_runs DROP CONSTRAINT IF EXISTS import_runs_id_user_key;
