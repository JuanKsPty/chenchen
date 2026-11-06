package oauth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"

	"github.com/JuanKsPty/chenchen/api/internal/auth"
	"github.com/JuanKsPty/chenchen/api/internal/config"
	"github.com/JuanKsPty/chenchen/api/internal/logging"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// How long each thing lives.
const (
	// requestTTL is how long the owner has to answer on the consent page,
	// signing in first if they must.
	requestTTL = 10 * time.Minute
	// codeTTL is how long a code is good for between the owner's approval and
	// Claude's exchange, which follows at once.
	codeTTL = 60 * time.Second
	// accessTTL is short because an access token is checked on every call
	// but revoking it means deleting a row; an hour bounds what a leaked one
	// is worth.
	accessTTL = time.Hour
	// refreshTTL slides: every refresh issues a new token good for this long,
	// so a connection used at least every two months never asks again.
	refreshTTL = 60 * 24 * time.Hour
	// refreshGrace is how long a consumed refresh token still returns the
	// pair it was rotated into; see graceCache.
	refreshGrace = 60 * time.Second
	// touchEvery bounds how often a grant's last use is written.
	touchEvery = time.Minute
	// rotateTimeout bounds a refresh shared by several requests, which runs
	// on none of their contexts: see Refresh.
	rotateTimeout = 10 * time.Second
)

// Service runs the authorization server: it takes Claude's requests, records
// the owner's answers, and issues, rotates, checks and revokes Claude's
// tokens.
type Service struct {
	db       *store.DB
	issuer   string
	resource string
	now      func() time.Time
	grace    *graceCache
	flight   singleflight.Group
}

// NewService builds the service for the public URL in cfg, which must be set:
// without it there is no issuer to name, and nothing here is mounted.
func NewService(db *store.DB, cfg config.OAuthConfig) *Service {
	return &Service{
		db:       db,
		issuer:   cfg.Issuer(),
		resource: cfg.Resource(),
		now:      time.Now,
		grace:    newGraceCache(),
	}
}

// Issuer is this authorization server's identifier.
func (s *Service) Issuer() string { return s.issuer }

// Resource is the protected resource every token is for.
func (s *Service) Resource() string { return s.resource }

// --- authorization ------------------------------------------------------------

// codeChallengeShape is an S256 challenge: BASE64URL(SHA-256(verifier)), 43
// characters with no padding.
var codeChallengeShape = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// maxStateLen bounds the state echoed back. Clients send a few dozen
// characters; the bound only keeps the table from holding whatever a stranger
// sends.
const maxStateLen = 512

// RedirectError is a refusal at the authorization endpoint once the redirect
// URI is known to be the client's: it goes back to the client there, as RFC
// 6749 §4.1.2.1 says, rather than being shown.
type RedirectError struct {
	OAuth       *Error
	RedirectURI string
	State       string
}

func (e *RedirectError) Error() string { return e.OAuth.Error() }

// UntrustedError is a refusal at the authorization endpoint before the client
// and its redirect URI are known: nothing may be sent to the client, so the
// person in the browser is told instead. Code is what the consent page
// explains it by — RefusedClient or RefusedRedirect — and Reason is for the
// log.
type UntrustedError struct {
	Code   string
	Reason string
}

func (e *UntrustedError) Error() string {
	return "oauth: untrusted authorization request: " + e.Code + ": " + e.Reason
}

// The refusals the consent page is sent to explain, as error=<code>, when an
// authorization request cannot go back to the client (see Handler.authorize).
const (
	RefusedClient   = "invalid_client"
	RefusedRedirect = "invalid_redirect"
	RefusedTooMany  = "rate_limited"
)

// Authorize takes what Claude sent to /authorize and, if it all holds, stores
// the request and returns where to send the person to answer it: the consent
// page, which signs them in first if it must. A refusal is an *UntrustedError
// or a *RedirectError.
func (s *Service) Authorize(ctx context.Context, q url.Values) (string, error) {
	req, err := s.checkAuthorize(q)
	if err != nil {
		return "", err
	}
	if err := s.db.Q().CreateOAuthRequest(ctx, req); err != nil {
		return "", err
	}
	return s.ConsentPage(url.Values{"req": {req.ID.String()}}), nil
}

// ConsentPage is the address of the web's consent page, /conectar, with
// params: the request to answer, or a refusal to explain.
func (s *Service) ConsentPage(params url.Values) string {
	return s.issuer + "/conectar?" + params.Encode()
}

// checkAuthorize validates an authorization request in the order RFC 6749
// §4.1.2.1 sets: the client and its redirect URI first, refused on the spot;
// everything else after, refused by redirect.
func (s *Service) checkAuthorize(q url.Values) (store.OAuthRequest, error) {
	if len(q["client_id"]) > 1 {
		return store.OAuthRequest{}, &UntrustedError{Code: RefusedClient, Reason: "client_id repeated"}
	}
	c, ok := lookupClient(q.Get("client_id"))
	if !ok {
		return store.OAuthRequest{}, &UntrustedError{Code: RefusedClient, Reason: "unknown client"}
	}
	if len(q["redirect_uri"]) > 1 {
		return store.OAuthRequest{}, &UntrustedError{Code: RefusedRedirect, Reason: "redirect_uri repeated"}
	}
	redirectURI := q.Get("redirect_uri")
	if !c.redirects(redirectURI) {
		return store.OAuthRequest{}, &UntrustedError{Code: RefusedRedirect, Reason: "redirect_uri is not the client's"}
	}

	state := q.Get("state")
	if len(state) > maxStateLen {
		state = "" // too long to echo; the request fails below anyway
	}
	refuse := func(e *Error) (store.OAuthRequest, error) {
		return store.OAuthRequest{}, &RedirectError{OAuth: e, RedirectURI: redirectURI, State: state}
	}
	for name, values := range q {
		if len(values) > 1 {
			return refuse(invalidRequest(name + " is repeated"))
		}
	}
	switch {
	case q.Get("state") == "":
		return refuse(invalidRequest("state is required"))
	case state == "":
		return refuse(invalidRequest("state is too long"))
	case q.Get("response_type") != "code":
		return refuse(&Error{Code: "unsupported_response_type", Description: "only response_type=code is supported"})
	case q.Get("code_challenge_method") != "S256":
		return refuse(invalidRequest("PKCE with code_challenge_method=S256 is required"))
	case !codeChallengeShape.MatchString(q.Get("code_challenge")):
		return refuse(invalidRequest("code_challenge must be an S256 challenge"))
	}
	scopes, ok := parseScope(q.Get("scope"))
	if !ok {
		return refuse(invalidScope("supported scopes are read, write and offline_access"))
	}
	// RFC 8707 makes resource optional, and this server has exactly one, so a
	// request that names none is for that one. One that names another is
	// refused: a token is never asked for anywhere else.
	if raw, sent := q["resource"]; sent && !s.isResource(raw[0]) {
		return refuse(invalidTarget("resource must be " + s.resource))
	}

	return store.OAuthRequest{
		ID:            uuid.New(),
		ClientID:      c.id,
		RedirectURI:   redirectURI,
		CodeChallenge: q.Get("code_challenge"),
		Scopes:        scopes,
		Resource:      s.resource,
		State:         state,
		ExpiresAt:     s.now().Add(requestTTL),
	}, nil
}

// PendingRequest is what the consent page shows of a request: who is asking,
// where the answer goes, and what was asked for.
type PendingRequest struct {
	ClientName   string
	RedirectHost string
	Scopes       []string
	ExpiresAt    time.Time
}

// Request returns a live request to the person opening it, and from then on it
// is theirs alone: anyone else gets ErrRequestNotFound. Opening it is also
// what lets them answer it; see take.
func (s *Service) Request(ctx context.Context, userID, id uuid.UUID) (PendingRequest, error) {
	r, err := s.db.Q().ClaimOAuthRequest(ctx, id, userID, s.now())
	if errors.Is(err, store.ErrNotFound) {
		return PendingRequest{}, fmt.Errorf("%w: %s", ErrRequestNotFound, id)
	}
	if err != nil {
		return PendingRequest{}, err
	}
	return PendingRequest{
		ClientName:   clientName(r.ClientID),
		RedirectHost: redirectHost(r.RedirectURI),
		Scopes:       r.Scopes,
		ExpiresAt:    r.ExpiresAt,
	}, nil
}

// Approve answers a request yes, as userID: it issues a code bound to them and
// to everything the request carried, and returns the client's redirect URI
// with the code, the state and the issuer.
//
// write is the owner's answer to «Anotar compras y ordenar tus movimientos».
// It is theirs to give whatever scope the client asked for: an MCP client asks
// for the scope the endpoint's challenge names, which is the minimum to read,
// so treating the request as a ceiling would leave writing impossible to
// grant. The token response says what was granted, as RFC 6749 §5.1 requires
// when it differs from what was asked.
func (s *Service) Approve(ctx context.Context, userID, id uuid.UUID, write bool) (string, error) {
	code, hash, err := auth.NewOAuthCode()
	if err != nil {
		return "", err
	}
	now := s.now()
	var req store.OAuthRequest
	err = s.db.InTx(ctx, func(q *store.Queries) error {
		if req, err = s.take(ctx, q, userID, id, now); err != nil {
			return err
		}
		return q.CreateOAuthCode(ctx, store.OAuthCode{
			Hash:            hash,
			UserID:          userID,
			ClientID:        req.ClientID,
			RedirectURI:     req.RedirectURI,
			CodeChallenge:   req.CodeChallenge,
			Scopes:          grantedScopes(write),
			RequestedScopes: req.Scopes,
			Resource:        req.Resource,
			ExpiresAt:       now.Add(codeTTL),
		})
	})
	if err != nil {
		return "", err
	}
	return s.redirect(req.RedirectURI, url.Values{"code": {code}, "state": {req.State}})
}

// Deny answers a request no, as userID, and returns the client's redirect URI
// with access_denied.
func (s *Service) Deny(ctx context.Context, userID, id uuid.UUID) (string, error) {
	req, err := s.take(ctx, s.db.Q(), userID, id, s.now())
	if err != nil {
		return "", err
	}
	return s.redirect(req.RedirectURI, url.Values{"error": {"access_denied"},
		"error_description": {"the owner declined the connection"}, "state": {req.State}})
}

// take removes a live request as userID answers it: one answer, by the one
// person who opened it with Request. A request nobody opened cannot be
// answered at all, so an approval only ever follows a person seeing what they
// approve — never a bare POST some other page sent with their session.
func (s *Service) take(ctx context.Context, q *store.Queries, userID, id uuid.UUID, now time.Time) (store.OAuthRequest, error) {
	req, err := q.TakeOAuthRequest(ctx, id, userID, now)
	if errors.Is(err, store.ErrNotFound) {
		return store.OAuthRequest{}, fmt.Errorf("%w: %s", ErrRequestNotFound, id)
	}
	return req, err
}

// redirect is uri with params and the issuer added to its query. The issuer
// goes on every response, success or error (RFC 9207), so a client talking to
// more than one server can tell whose answer it is holding.
func (s *Service) redirect(uri string, params url.Values) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("oauth: stored redirect URI: %w", err)
	}
	q := u.Query()
	for name, values := range params {
		if len(values) == 1 && values[0] == "" {
			continue // no empty state=
		}
		q[name] = values
	}
	q.Set("iss", s.issuer)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// --- tokens --------------------------------------------------------------------

// TokenPair is what the token endpoint hands Claude.
type TokenPair struct {
	AccessToken     string
	RefreshToken    string
	AccessExpiresAt time.Time
	// ExpiresIn is what is left of the access token as it is handed over:
	// all of accessTTL, or less when a repeated refresh gets a pair issued a
	// moment ago.
	ExpiresIn time.Duration
	Scopes    []string
}

// ExchangeInput is an authorization_code grant request.
type ExchangeInput struct {
	Code        string
	Verifier    string
	ClientID    string
	RedirectURI string
	// Resource is optional here: the code already carries it. Sent, it must
	// be the same.
	Resource string
}

// verifierShape is RFC 7636's code_verifier: 43 to 128 unreserved characters.
var verifierShape = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

// Exchange trades a code for the first pair of a new grant. Every way a code
// can be wrong is invalid_grant.
//
// A code used twice by its own client also revokes what its first use issued
// (RFC 6749 §4.1.2): a second exchange means it leaked. Its own client is one
// that proves it — the same client ID and redirect URI, and the verifier that
// matches the challenge. A code alone, read from a log or a browser's history,
// proves nothing, and must not be enough to disconnect the owner: that replay
// is refused and nothing else happens.
func (s *Service) Exchange(ctx context.Context, in ExchangeInput) (TokenPair, error) {
	if _, ok := lookupClient(in.ClientID); !ok {
		return TokenPair{}, invalidClient("unknown client_id")
	}
	if in.Code == "" {
		return TokenPair{}, invalidRequest("code is required")
	}
	now := s.now()
	var (
		pair          TokenPair
		used, revoked bool
	)
	err := s.db.InTx(ctx, func(q *store.Queries) error {
		c, err := q.LockOAuthCode(ctx, auth.HashOAuthToken(in.Code))
		if errors.Is(err, store.ErrNotFound) {
			return invalidGrant("unknown code")
		}
		if err != nil {
			return err
		}
		if c.UsedAt != nil {
			// Refused either way; returning nil lets the transaction
			// commit the revocation, when there is one.
			used = true
			if in.ClientID != c.ClientID || in.RedirectURI != c.RedirectURI ||
				!pkceMatches(in.Verifier, c.CodeChallenge) || c.GrantID == nil {
				return nil
			}
			revoked = true
			return q.RevokeOAuthGrant(ctx, c.UserID, *c.GrantID, now)
		}
		switch {
		case !now.Before(c.ExpiresAt):
			return invalidGrant("the code has expired")
		case in.ClientID != c.ClientID:
			return invalidGrant("the code was issued to another client")
		case in.RedirectURI != c.RedirectURI:
			return invalidGrant("redirect_uri does not match the authorization request")
		case !pkceMatches(in.Verifier, c.CodeChallenge):
			return invalidGrant("code_verifier does not match the code_challenge")
		case in.Resource != "" && !sameResource(in.Resource, c.Resource):
			return invalidTarget("resource must be " + c.Resource)
		}

		g, err := q.CreateOAuthGrant(ctx, store.OAuthGrant{
			ID: uuid.New(), UserID: c.UserID, ClientID: c.ClientID, Scopes: c.Scopes,
			RequestedScopes: c.RequestedScopes, Resource: c.Resource, CreatedAt: now,
		})
		if err != nil {
			return err
		}
		if pair, _, err = s.issue(ctx, q, g, g.Scopes, now); err != nil {
			return err
		}
		return q.UseOAuthCode(ctx, c.Hash, g.ID, now)
	})
	if err != nil {
		return TokenPair{}, err
	}
	if used {
		if revoked {
			logging.FromContext(ctx).Warn("an authorization code was presented twice by its client; its grant is revoked")
		}
		return TokenPair{}, invalidGrant("the code has already been used")
	}
	return pair, nil
}

// pkceMatches reports whether verifier is the one challenge was made from,
// by S256 — the only method ever accepted, so a verifier sent as the
// challenge itself ("plain") never matches.
func pkceMatches(verifier, challenge string) bool {
	if !verifierShape.MatchString(verifier) {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) == 1
}

// RefreshInput is a refresh_token grant request.
type RefreshInput struct {
	RefreshToken string
	// ClientID, Resource and Scope are optional. Sent, they must match the
	// grant, and Scope may ask for less than it holds but never for more.
	ClientID string
	Resource string
	Scope    string
}

// Refresh rotates a refresh token: the presented one is consumed and a new
// pair issued, good for refreshTTL from now.
//
// Each request's own parameters are held to the grant first, before it can
// join a rotation other requests share: one with another client's ID, another
// resource or more scope than the grant holds is refused on its own, and never
// fails the requests it arrived with. A Scope asking for less gets an access
// token that carries exactly that; the new refresh token keeps all of the
// grant's (RFC 6749 §6), so asking for less once does not narrow the
// connection.
//
// The same token presented again within refreshGrace, asking for the same
// scope, returns the same new pair (see graceCache), and requests that arrive
// together share one rotation rather than racing for the row. Presented after
// that, it can only be a copy someone kept, so the whole grant is revoked: the
// thief's pair and the owner's both stop working, and the owner connects
// again.
func (s *Service) Refresh(ctx context.Context, in RefreshInput) (TokenPair, error) {
	if !auth.LooksLikeOAuthRefreshToken(in.RefreshToken) {
		return TokenPair{}, invalidGrant("not a refresh token")
	}
	hash := auth.HashOAuthToken(in.RefreshToken)

	// A grant's client, resource and scopes never change, so what is read
	// here, without a lock, still holds when the token is rotated.
	g, err := s.db.Q().OAuthGrantByRefreshToken(ctx, hash)
	if errors.Is(err, store.ErrNotFound) {
		return TokenPair{}, invalidGrant("unknown refresh token")
	}
	if err != nil {
		return TokenPair{}, err
	}
	if g.RevokedAt != nil {
		return TokenPair{}, invalidGrant("the grant has been revoked")
	}
	scopes, err := checkRefresh(in, g)
	if err != nil {
		return TokenPair{}, err
	}

	// Requests share a rotation only if they asked for the same scopes: one
	// access token cannot carry two. It runs on a context of its own, since
	// requests sharing it must not all fail because the first one's client
	// hung up.
	key := rotationKey(hash, scopes)
	v, err, _ := s.flight.Do(key, func() (any, error) {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rotateTimeout)
		defer cancel()
		return s.rotate(rctx, hash, scopes)
	})
	if err != nil {
		return TokenPair{}, err
	}
	pair := v.(rotation).pair
	pair.ExpiresIn = pair.AccessExpiresAt.Sub(s.now())
	return pair, nil
}

// rotationKey names one rotation: the refresh token it consumes, and the
// scopes its access token carries, which are already in grantedScopes' order.
func rotationKey(hash string, scopes []string) string {
	return hash + " " + strings.Join(scopes, " ")
}

// rotate consumes the refresh token with this hash and issues the next pair,
// its access token carrying scopes, or returns the pair it was already rotated
// into for those scopes while that is still within the grace window.
func (s *Service) rotate(ctx context.Context, hash string, scopes []string) (rotation, error) {
	now := s.now()
	key := rotationKey(hash, scopes)
	if r, ok := s.grace.get(key, now); ok {
		// Disconnected meanwhile: the pair was deleted with the grant.
		g, err := s.db.Q().OAuthGrantByID(ctx, r.grantID)
		if errors.Is(err, store.ErrNotFound) || (err == nil && g.RevokedAt != nil) {
			return rotation{}, invalidGrant("the grant has been revoked")
		}
		if err != nil {
			return rotation{}, err
		}
		return r, nil
	}

	var (
		out     rotation
		revoked bool
	)
	err := s.db.InTx(ctx, func(q *store.Queries) error {
		t, g, err := q.LockOAuthRefreshToken(ctx, hash)
		if errors.Is(err, store.ErrNotFound) {
			return invalidGrant("unknown refresh token")
		}
		if err != nil {
			return err
		}
		if g.RevokedAt != nil {
			return invalidGrant("the grant has been revoked")
		}
		if t.ConsumedAt != nil {
			if now.Sub(*t.ConsumedAt) <= refreshGrace {
				// A repeat inside the window that this process did not
				// rotate, or that asks for other scopes than the rotation
				// did. The pair is not here to return, and nothing suggests
				// theft, so only this request fails.
				return invalidGrant("the refresh token has just been used")
			}
			revoked = true
			return q.RevokeOAuthGrant(ctx, g.UserID, g.ID, now)
		}
		if !now.Before(t.ExpiresAt) {
			return invalidGrant("the refresh token has expired")
		}

		pair, newHash, err := s.issue(ctx, q, g, scopes, now)
		if err != nil {
			return err
		}
		if err := q.ConsumeOAuthRefreshToken(ctx, hash, newHash, now); err != nil {
			return err
		}
		out = rotation{pair: pair, grantID: g.ID}
		return nil
	})
	if err != nil {
		return rotation{}, err
	}
	if revoked {
		logging.FromContext(ctx).Warn("a consumed refresh token was presented again; its grant is revoked")
		return rotation{}, invalidGrant("the refresh token has already been used")
	}
	s.grace.put(key, out, now.Add(refreshGrace), now)
	return out, nil
}

// checkRefresh holds a refresh request's optional parameters to the grant
// they would refresh, and returns the scopes its access token is to carry:
// the grant's, or fewer if the request asked for fewer.
//
// A client that repeats on refresh the scope it asked for when it connected
// is asking for its grant, not narrowing it: it gets the grant's scopes,
// whether the owner granted more than it asked (write, though it asked only
// for read) or less (read, though it asked for write). Read narrowly, the
// first would lose write an hour after connecting and the second would fail
// every refresh and never stay connected.
func checkRefresh(in RefreshInput, g store.OAuthGrant) ([]string, error) {
	if in.ClientID != "" && in.ClientID != g.ClientID {
		return nil, invalidGrant("the refresh token was issued to another client")
	}
	if in.Resource != "" && !sameResource(in.Resource, g.Resource) {
		return nil, invalidTarget("resource must be " + g.Resource)
	}
	if in.Scope == "" {
		return g.Scopes, nil
	}
	asked, ok := parseScope(in.Scope)
	if ok && slices.Equal(asked, g.RequestedScopes) {
		return g.Scopes, nil
	}
	if !ok || !within(asked, g.Scopes) {
		return nil, invalidScope("a refresh cannot ask for more than was granted")
	}
	return asked, nil
}

// issue mints an access token carrying scopes, which are the grant's or fewer,
// and a refresh token carrying all of the grant's, stores their hashes, and
// returns the pair and the refresh token's hash.
func (s *Service) issue(ctx context.Context, q *store.Queries, g store.OAuthGrant, scopes []string, now time.Time) (TokenPair, string, error) {
	access, accessHash, err := auth.NewOAuthAccessToken()
	if err != nil {
		return TokenPair{}, "", err
	}
	refresh, refreshHash, err := auth.NewOAuthRefreshToken()
	if err != nil {
		return TokenPair{}, "", err
	}
	for _, t := range []store.OAuthToken{
		{Hash: accessHash, GrantID: g.ID, UserID: g.UserID, Kind: store.OAuthAccess, Scopes: scopes,
			ExpiresAt: now.Add(accessTTL)},
		{Hash: refreshHash, GrantID: g.ID, UserID: g.UserID, Kind: store.OAuthRefresh, Scopes: g.Scopes,
			ExpiresAt: now.Add(refreshTTL)},
	} {
		if err := q.CreateOAuthToken(ctx, t); err != nil {
			return TokenPair{}, "", err
		}
	}
	return TokenPair{AccessToken: access, RefreshToken: refresh, AccessExpiresAt: now.Add(accessTTL),
		ExpiresIn: accessTTL, Scopes: scopes}, refreshHash, nil
}

// --- the owner's connections -----------------------------------------------------

// Grant is one of the owner's live connections, as Conexiones lists it.
type Grant struct {
	ID         uuid.UUID
	ClientName string
	Scopes     []string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// Grants lists userID's connections that still work, newest first.
func (s *Service) Grants(ctx context.Context, userID uuid.UUID) ([]Grant, error) {
	list, err := s.db.Q().LiveOAuthGrants(ctx, userID, s.now())
	if err != nil {
		return nil, err
	}
	out := make([]Grant, 0, len(list))
	for _, g := range list {
		out = append(out, Grant{ID: g.ID, ClientName: clientName(g.ClientID), Scopes: g.Scopes,
			CreatedAt: g.CreatedAt, LastUsedAt: g.LastUsedAt})
	}
	return out, nil
}

// Revoke ends one of userID's connections and every token it issued. It is
// silent about a grant that is not theirs or no longer exists.
func (s *Service) Revoke(ctx context.Context, userID, id uuid.UUID) error {
	return s.db.InTx(ctx, func(q *store.Queries) error {
		return q.RevokeOAuthGrant(ctx, userID, id, s.now())
	})
}

// Prune deletes requests, codes, tokens and grants that can no longer do
// anything. Called on the API's housekeeping timer.
func (s *Service) Prune(ctx context.Context) (int64, error) {
	now := s.now()
	s.grace.sweep(now)
	return s.db.Q().DeleteExpiredOAuth(ctx, now)
}

// --- resources -------------------------------------------------------------------

// isResource reports whether raw names this server's protected resource.
func (s *Service) isResource(raw string) bool { return sameResource(raw, s.resource) }

// sameResource reports whether raw names the resource want, which is already
// canonical. Scheme and host case, a default port and a trailing slash are
// forgiven — clients differ in all four and none changes the address — while
// everything else must be equal, so a token can never be asked for, or
// accepted at, another resource.
func sameResource(raw, want string) bool {
	if raw == "" {
		return false
	}
	canonical, err := config.CanonicalURL(raw)
	return err == nil && canonical == want
}
