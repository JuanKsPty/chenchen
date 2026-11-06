-- Claude signs in to chenchén with OAuth 2.1 (ADR-0004): claude.ai, its phone
-- app and Claude Code reach the MCP endpoint with a token of their own, which
-- the owner grants from the app and can revoke from it.
--
-- Everything here is apart from the web's sessions. A web refresh token lives
-- in refresh_tokens and an OAuth one only in oauth_tokens; the two prefixes
-- (chno_, chnr_) and the two tables are what keep a token Claude holds from
-- ever reaching an endpoint a session reaches, and a session from ever being
-- read as Claude's. As everywhere else, only hashes are stored: a database
-- dump cannot be replayed as a connected Claude.
--
-- Every row belongs to a user and goes with them.

-- +goose Up

-- An authorization request waiting for the owner, between Claude sending
-- them to /api/oauth/authorize and their answer on the consent page. It holds
-- what the code will be bound to. Nobody owns it until someone signed in
-- opens it; from then on only they can answer it, once.
CREATE TABLE oauth_requests (
    id             UUID        PRIMARY KEY,
    user_id        UUID        REFERENCES users (id) ON DELETE CASCADE,
    client_id      TEXT        NOT NULL,
    redirect_uri   TEXT        NOT NULL,
    -- S256 only: BASE64URL(SHA-256(verifier)) is always 43 characters.
    code_challenge TEXT        NOT NULL CHECK (code_challenge ~ '^[A-Za-z0-9_-]{43}$'),
    scopes         TEXT[]      NOT NULL CHECK ('read' = ANY (scopes) AND scopes <@ ARRAY['read', 'write']),
    resource       TEXT        NOT NULL,
    state          TEXT        NOT NULL CHECK (char_length(state) BETWEEN 1 AND 512),
    expires_at     TIMESTAMPTZ NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One approval, and everything issued from it: the family every refresh
-- rotates within. Revoking it, from Conexiones or because a consumed refresh
-- token came back, ends all of its tokens at once.
CREATE TABLE oauth_grants (
    id           UUID        PRIMARY KEY,
    user_id      UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    client_id    TEXT        NOT NULL,
    scopes       TEXT[]      NOT NULL CHECK ('read' = ANY (scopes) AND scopes <@ ARRAY['read', 'write']),
    -- What the client asked for when it connected, which may be more or less
    -- than the owner granted. A refresh that repeats it asks for the grant,
    -- whatever it is; only another, smaller set narrows the access token.
    requested_scopes TEXT[]  NOT NULL CHECK ('read' = ANY (requested_scopes)
                                            AND requested_scopes <@ ARRAY['read', 'write']),
    resource     TEXT        NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ,
    UNIQUE (id, user_id)
);
CREATE INDEX oauth_grants_user_idx ON oauth_grants (user_id);

-- An authorization code: a minute to live, one use, bound to everything the
-- request carried and to the owner who approved it. grant_id is the grant its
-- exchange created, so a code presented twice can revoke what the first use
-- issued.
CREATE TABLE oauth_codes (
    hash           TEXT        PRIMARY KEY,
    user_id        UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    client_id      TEXT        NOT NULL,
    redirect_uri   TEXT        NOT NULL,
    code_challenge TEXT        NOT NULL CHECK (code_challenge ~ '^[A-Za-z0-9_-]{43}$'),
    scopes         TEXT[]      NOT NULL CHECK ('read' = ANY (scopes) AND scopes <@ ARRAY['read', 'write']),
    requested_scopes TEXT[]    NOT NULL CHECK ('read' = ANY (requested_scopes)
                                              AND requested_scopes <@ ARRAY['read', 'write']),
    resource       TEXT        NOT NULL,
    expires_at     TIMESTAMPTZ NOT NULL,
    used_at        TIMESTAMPTZ,
    grant_id       UUID,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (grant_id, user_id) REFERENCES oauth_grants (id, user_id) ON DELETE SET NULL (grant_id),
    CONSTRAINT oauth_codes_used_has_when CHECK (grant_id IS NULL OR used_at IS NOT NULL)
);

-- Access (chno_, an hour) and refresh (chnr_, sixty days from its last use)
-- tokens. A refresh token is consumed when it rotates, and the row stays until
-- it would have expired: it is how a stolen one, presented after the client
-- already moved on, is recognised and its whole grant revoked.
CREATE TABLE oauth_tokens (
    hash        TEXT        PRIMARY KEY,
    grant_id    UUID        NOT NULL,
    user_id     UUID        NOT NULL,
    kind        TEXT        NOT NULL CHECK (kind IN ('access', 'refresh')),
    -- What the token may do: its grant's scopes, or fewer for an access token
    -- whose refresh asked for less (RFC 6749 §6). A refresh token always holds
    -- all of its grant's, so asking for less once never loses the rest.
    scopes      TEXT[]      NOT NULL CHECK ('read' = ANY (scopes) AND scopes <@ ARRAY['read', 'write']),
    expires_at  TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    -- The hash of the refresh token that replaced this one.
    replaced_by TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (grant_id, user_id) REFERENCES oauth_grants (id, user_id) ON DELETE CASCADE,
    CONSTRAINT oauth_tokens_only_refresh_rotates CHECK (kind = 'refresh' OR consumed_at IS NULL),
    CONSTRAINT oauth_tokens_consumed_is_replaced CHECK ((consumed_at IS NULL) = (replaced_by IS NULL))
);
CREATE INDEX oauth_tokens_grant_idx ON oauth_tokens (grant_id);

-- +goose Down
DROP TABLE IF EXISTS oauth_tokens;
DROP TABLE IF EXISTS oauth_codes;
DROP TABLE IF EXISTS oauth_grants;
DROP TABLE IF EXISTS oauth_requests;
