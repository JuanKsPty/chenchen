package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// OAuthRequest is an authorization request from Claude waiting for the
// owner's answer (ADR-0004). UserID is nil until someone signed in opens it.
type OAuthRequest struct {
	ID            uuid.UUID
	UserID        *uuid.UUID
	ClientID      string
	RedirectURI   string
	CodeChallenge string
	Scopes        []string
	Resource      string
	State         string
	ExpiresAt     time.Time
}

const oauthRequestColumns = `id, user_id, client_id, redirect_uri, code_challenge, scopes, resource, state, expires_at`

// CreateOAuthRequest stores a request Claude just made.
func (q *Queries) CreateOAuthRequest(ctx context.Context, r OAuthRequest) error {
	_, err := q.q.Exec(ctx, `
		INSERT INTO oauth_requests (id, client_id, redirect_uri, code_challenge, scopes, resource, state, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		r.ID, r.ClientID, r.RedirectURI, r.CodeChallenge, r.Scopes, r.Resource, r.State, r.ExpiresAt)
	return wrap("store.CreateOAuthRequest", err)
}

// ClaimOAuthRequest returns a live request to the user opening it, and makes
// it theirs if nobody had opened it yet. One already someone else's is
// ErrNotFound, the same as one that expired or never existed.
func (q *Queries) ClaimOAuthRequest(ctx context.Context, id, userID uuid.UUID, now time.Time) (OAuthRequest, error) {
	r, err := scanOAuthRequest(q.q.QueryRow(ctx, `
		UPDATE oauth_requests SET user_id = $2
		WHERE id = $1 AND (user_id IS NULL OR user_id = $2) AND expires_at > $3
		RETURNING `+oauthRequestColumns, id, userID, now))
	return r, wrap("store.ClaimOAuthRequest", err)
}

// TakeOAuthRequest removes a live request as the user answers it, and returns
// it. Deleting is what makes an answer single-use: a second approval, or a
// denial after an approval, finds nothing.
//
// Only a request this user already claimed (ClaimOAuthRequest) can be taken.
// An unclaimed one is ErrNotFound to everyone: an answer comes from someone
// who opened the request and saw what it asks, never from a bare POST with
// its ID, which a page could send on a signed-in person's behalf.
func (q *Queries) TakeOAuthRequest(ctx context.Context, id, userID uuid.UUID, now time.Time) (OAuthRequest, error) {
	r, err := scanOAuthRequest(q.q.QueryRow(ctx, `
		DELETE FROM oauth_requests
		WHERE id = $1 AND user_id = $2 AND expires_at > $3
		RETURNING `+oauthRequestColumns, id, userID, now))
	return r, wrap("store.TakeOAuthRequest", err)
}

func scanOAuthRequest(s scanner) (OAuthRequest, error) {
	var r OAuthRequest
	err := s.Scan(&r.ID, &r.UserID, &r.ClientID, &r.RedirectURI, &r.CodeChallenge, &r.Scopes, &r.Resource,
		&r.State, &r.ExpiresAt)
	return r, err
}

// OAuthCode is an authorization code, by its hash, and everything it is bound
// to. GrantID is the grant its exchange created, once it has been used.
type OAuthCode struct {
	Hash          string
	UserID        uuid.UUID
	ClientID      string
	RedirectURI   string
	CodeChallenge string
	Scopes        []string
	// RequestedScopes is what the client asked for; Scopes what was granted.
	RequestedScopes []string
	Resource        string
	ExpiresAt       time.Time
	UsedAt          *time.Time
	GrantID         *uuid.UUID
}

// CreateOAuthCode stores a code the owner's approval just issued.
func (q *Queries) CreateOAuthCode(ctx context.Context, c OAuthCode) error {
	_, err := q.q.Exec(ctx, `
		INSERT INTO oauth_codes (hash, user_id, client_id, redirect_uri, code_challenge, scopes, requested_scopes,
		                         resource, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		c.Hash, c.UserID, c.ClientID, c.RedirectURI, c.CodeChallenge, c.Scopes, c.RequestedScopes, c.Resource,
		c.ExpiresAt)
	return wrap("store.CreateOAuthCode", err)
}

// LockOAuthCode reads a code for its exchange, used, expired or not, and
// locks it until the caller's transaction ends: two exchanges of one code are
// serialised, and the second finds it used.
func (q *Queries) LockOAuthCode(ctx context.Context, hash string) (OAuthCode, error) {
	var c OAuthCode
	err := q.q.QueryRow(ctx, `
		SELECT hash, user_id, client_id, redirect_uri, code_challenge, scopes, requested_scopes, resource, expires_at,
		       used_at, grant_id
		FROM oauth_codes WHERE hash = $1 FOR UPDATE`, hash).Scan(
		&c.Hash, &c.UserID, &c.ClientID, &c.RedirectURI, &c.CodeChallenge, &c.Scopes, &c.RequestedScopes, &c.Resource,
		&c.ExpiresAt, &c.UsedAt, &c.GrantID)
	return c, wrap("store.LockOAuthCode", err)
}

// UseOAuthCode marks a code spent, naming the grant it created.
func (q *Queries) UseOAuthCode(ctx context.Context, hash string, grantID uuid.UUID, now time.Time) error {
	return q.execOne(ctx, "store.UseOAuthCode",
		`UPDATE oauth_codes SET used_at = $3, grant_id = $2 WHERE hash = $1 AND used_at IS NULL`, hash, grantID, now)
}

// OAuthGrant is one approval and the token family issued from it.
type OAuthGrant struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	ClientID string
	Scopes   []string
	// RequestedScopes is what the client asked for when it connected.
	RequestedScopes []string
	Resource        string
	CreatedAt       time.Time
	LastUsedAt      *time.Time
	RevokedAt       *time.Time
}

const oauthGrantColumns = `g.id, g.user_id, g.client_id, g.scopes, g.requested_scopes, g.resource, g.created_at,
	g.last_used_at, g.revoked_at`

// CreateOAuthGrant records an approval as its code is exchanged, at
// g.CreatedAt: the service's clock, which every other time on a grant is read
// against.
func (q *Queries) CreateOAuthGrant(ctx context.Context, g OAuthGrant) (OAuthGrant, error) {
	created, err := scanOAuthGrant(q.q.QueryRow(ctx, `
		INSERT INTO oauth_grants AS g (id, user_id, client_id, scopes, requested_scopes, resource, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+oauthGrantColumns, g.ID, g.UserID, g.ClientID, g.Scopes, g.RequestedScopes, g.Resource,
		g.CreatedAt))
	return created, wrap("store.CreateOAuthGrant", err)
}

// OAuthGrantByID reads one grant, revoked or not.
func (q *Queries) OAuthGrantByID(ctx context.Context, id uuid.UUID) (OAuthGrant, error) {
	g, err := scanOAuthGrant(q.q.QueryRow(ctx,
		`SELECT `+oauthGrantColumns+` FROM oauth_grants g WHERE g.id = $1`, id))
	return g, wrap("store.OAuthGrantByID", err)
}

// LiveOAuthGrants lists the user's connections that can still do something:
// not revoked, and holding a refresh token that has neither expired nor been
// used. Newest first.
func (q *Queries) LiveOAuthGrants(ctx context.Context, userID uuid.UUID, now time.Time) ([]OAuthGrant, error) {
	rows, err := q.q.Query(ctx, `
		SELECT `+oauthGrantColumns+` FROM oauth_grants g
		WHERE g.user_id = $1 AND g.revoked_at IS NULL AND EXISTS (
			SELECT 1 FROM oauth_tokens t
			WHERE t.grant_id = g.id AND t.kind = 'refresh' AND t.consumed_at IS NULL AND t.expires_at > $2)
		ORDER BY g.created_at DESC, g.id`, userID, now)
	if err != nil {
		return nil, wrap("store.LiveOAuthGrants", err)
	}
	defer rows.Close()
	var out []OAuthGrant
	for rows.Next() {
		g, err := scanOAuthGrant(rows)
		if err != nil {
			return nil, wrap("store.LiveOAuthGrants", err)
		}
		out = append(out, g)
	}
	return out, wrap("store.LiveOAuthGrants", rows.Err())
}

// RevokeOAuthGrant ends a grant and deletes every token it issued, so neither
// its access tokens nor its refresh tokens work from this statement on. It
// runs two statements and belongs in the caller's transaction. Idempotent and
// silent about whether there was such a grant, like RevokeCaptureKey: it only
// ever touches one of userID's own.
func (q *Queries) RevokeOAuthGrant(ctx context.Context, userID, id uuid.UUID, now time.Time) error {
	if _, err := q.q.Exec(ctx, `UPDATE oauth_grants SET revoked_at = $3
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, id, userID, now); err != nil {
		return wrap("store.RevokeOAuthGrant", err)
	}
	_, err := q.q.Exec(ctx, `DELETE FROM oauth_tokens WHERE grant_id = $1 AND user_id = $2`, id, userID)
	return wrap("store.RevokeOAuthGrant", err)
}

// TouchOAuthGrant records the grant being used, at most once per every: an
// MCP session makes many calls a minute, and "last used" needs none of that
// precision.
func (q *Queries) TouchOAuthGrant(ctx context.Context, id uuid.UUID, now time.Time, every time.Duration) error {
	_, err := q.q.Exec(ctx, `UPDATE oauth_grants SET last_used_at = $2
		WHERE id = $1 AND (last_used_at IS NULL OR last_used_at <= $3)`, id, now, now.Add(-every))
	return wrap("store.TouchOAuthGrant", err)
}

func scanOAuthGrant(s scanner) (OAuthGrant, error) {
	var g OAuthGrant
	err := s.Scan(&g.ID, &g.UserID, &g.ClientID, &g.Scopes, &g.RequestedScopes, &g.Resource, &g.CreatedAt,
		&g.LastUsedAt, &g.RevokedAt)
	return g, err
}

// OAuthTokenKind tells an access token from a refresh token.
type OAuthTokenKind string

const (
	OAuthAccess  OAuthTokenKind = "access"
	OAuthRefresh OAuthTokenKind = "refresh"
)

// OAuthToken is an access or refresh token, by its hash. Scopes are what it
// may do: its grant's, or fewer for an access token a refresh narrowed.
type OAuthToken struct {
	Hash       string
	GrantID    uuid.UUID
	UserID     uuid.UUID
	Kind       OAuthTokenKind
	Scopes     []string
	ExpiresAt  time.Time
	ConsumedAt *time.Time
}

const oauthTokenColumns = `t.hash, t.grant_id, t.user_id, t.kind, t.scopes, t.expires_at, t.consumed_at`

// CreateOAuthToken stores a token just issued.
func (q *Queries) CreateOAuthToken(ctx context.Context, t OAuthToken) error {
	_, err := q.q.Exec(ctx, `
		INSERT INTO oauth_tokens (hash, grant_id, user_id, kind, scopes, expires_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		t.Hash, t.GrantID, t.UserID, t.Kind, t.Scopes, t.ExpiresAt)
	return wrap("store.CreateOAuthToken", err)
}

// OAuthGrantByRefreshToken reads the grant a refresh token was issued from,
// whether the token is live, consumed or expired, and locks nothing: it is
// for holding a refresh request's parameters to the grant before the token
// is rotated. A token whose grant was revoked is gone with it, and
// ErrNotFound like one that never existed.
func (q *Queries) OAuthGrantByRefreshToken(ctx context.Context, hash string) (OAuthGrant, error) {
	g, err := scanOAuthGrant(q.q.QueryRow(ctx, `
		SELECT `+oauthGrantColumns+`
		FROM oauth_tokens t JOIN oauth_grants g ON g.id = t.grant_id
		WHERE t.hash = $1 AND t.kind = 'refresh'`, hash))
	return g, wrap("store.OAuthGrantByRefreshToken", err)
}

// LockOAuthRefreshToken reads a refresh token, expired or consumed or not,
// with its grant, and locks the token until the caller's transaction ends: two
// refreshes with one token are serialised, and the second finds it consumed.
func (q *Queries) LockOAuthRefreshToken(ctx context.Context, hash string) (OAuthToken, OAuthGrant, error) {
	var (
		t OAuthToken
		g OAuthGrant
	)
	err := q.q.QueryRow(ctx, `
		SELECT `+oauthTokenColumns+`, `+oauthGrantColumns+`
		FROM oauth_tokens t JOIN oauth_grants g ON g.id = t.grant_id
		WHERE t.hash = $1 AND t.kind = 'refresh'
		FOR UPDATE OF t`, hash).Scan(
		&t.Hash, &t.GrantID, &t.UserID, &t.Kind, &t.Scopes, &t.ExpiresAt, &t.ConsumedAt,
		&g.ID, &g.UserID, &g.ClientID, &g.Scopes, &g.RequestedScopes, &g.Resource, &g.CreatedAt, &g.LastUsedAt,
		&g.RevokedAt)
	return t, g, wrap("store.LockOAuthRefreshToken", err)
}

// ConsumeOAuthRefreshToken marks a refresh token rotated, naming the hash of
// the one that replaced it. A token already consumed is ErrNotFound.
func (q *Queries) ConsumeOAuthRefreshToken(ctx context.Context, hash, replacedBy string, now time.Time) error {
	return q.execOne(ctx, "store.ConsumeOAuthRefreshToken", `
		UPDATE oauth_tokens SET consumed_at = $3, replaced_by = $2
		WHERE hash = $1 AND kind = 'refresh' AND consumed_at IS NULL`, hash, replacedBy, now)
}

// LiveOAuthAccessToken looks up the access token a request presented, with
// its grant. Expired, revoked or unknown are all ErrNotFound: the three look
// identical to whoever is asking.
func (q *Queries) LiveOAuthAccessToken(ctx context.Context, hash string, now time.Time) (OAuthToken, OAuthGrant, error) {
	var (
		t OAuthToken
		g OAuthGrant
	)
	err := q.q.QueryRow(ctx, `
		SELECT `+oauthTokenColumns+`, `+oauthGrantColumns+`
		FROM oauth_tokens t JOIN oauth_grants g ON g.id = t.grant_id
		WHERE t.hash = $1 AND t.kind = 'access' AND t.expires_at > $2 AND g.revoked_at IS NULL`, hash, now).Scan(
		&t.Hash, &t.GrantID, &t.UserID, &t.Kind, &t.Scopes, &t.ExpiresAt, &t.ConsumedAt,
		&g.ID, &g.UserID, &g.ClientID, &g.Scopes, &g.RequestedScopes, &g.Resource, &g.CreatedAt, &g.LastUsedAt,
		&g.RevokedAt)
	return t, g, wrap("store.LiveOAuthAccessToken", err)
}

// DeleteExpiredOAuth prunes what can no longer do anything: requests and
// codes past their time, tokens past theirs, and then every grant left with
// no token at all — a revoked one, or one whose last refresh token expired.
// A consumed refresh token is kept until it would have expired, since it is
// what recognises a stolen copy presented later.
func (q *Queries) DeleteExpiredOAuth(ctx context.Context, now time.Time) (int64, error) {
	var removed int64
	for _, step := range []struct {
		op, sql string
		args    []any
	}{
		{"requests", `DELETE FROM oauth_requests WHERE expires_at <= $1`, []any{now}},
		{"codes", `DELETE FROM oauth_codes WHERE expires_at <= $1`, []any{now}},
		{"tokens", `DELETE FROM oauth_tokens WHERE expires_at <= $1`, []any{now}},
		{"grants", `DELETE FROM oauth_grants g
			WHERE NOT EXISTS (SELECT 1 FROM oauth_tokens t WHERE t.grant_id = g.id)`, nil},
	} {
		tag, err := q.q.Exec(ctx, step.sql, step.args...)
		if err != nil {
			return removed, wrap("store.DeleteExpiredOAuth "+step.op, err)
		}
		removed += tag.RowsAffected()
	}
	return removed, nil
}
