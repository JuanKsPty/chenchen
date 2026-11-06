package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	// tokenIssuer identifies this API in the `iss` claim, so a token minted by a
	// different service with the same secret is still rejected.
	tokenIssuer = "chenchen"
	// tokenAudience narrows a token to this API's own endpoints.
	tokenAudience = "chenchen-api"
)

var (
	ErrTokenInvalid = errors.New("auth: token is invalid")
	ErrTokenExpired = errors.New("auth: token has expired")
)

// TokenIssuer mints and verifies access tokens.
//
// Access tokens are short-lived JWTs so that authorising a request needs no
// database round trip. The price of that is that they cannot be revoked before
// they expire, which is exactly why they last minutes and the long-lived
// credential is the refresh token — that one *is* stored, and revoking it ends
// the session.
type TokenIssuer struct {
	secret    []byte
	accessTTL time.Duration
}

func NewTokenIssuer(secret []byte, accessTTL time.Duration) *TokenIssuer {
	return &TokenIssuer{secret: secret, accessTTL: accessTTL}
}

// AccessTTL is how long a freshly issued access token remains valid; the client
// needs it to schedule a refresh before expiry rather than after a failure.
func (t *TokenIssuer) AccessTTL() time.Duration { return t.accessTTL }

// IssueAccessToken mints a signed access token for a user.
func (t *TokenIssuer) IssueAccessToken(userID uuid.UUID, now time.Time) (string, error) {
	claims := jwt.RegisteredClaims{
		Subject:   userID.String(),
		Issuer:    tokenIssuer,
		Audience:  jwt.ClaimStrings{tokenAudience},
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(t.accessTTL)),
		ID:        uuid.NewString(),
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	if err != nil {
		return "", fmt.Errorf("auth: signing access token: %w", err)
	}
	return signed, nil
}

// ParseAccessToken verifies a token and returns the user it belongs to.
func (t *TokenIssuer) ParseAccessToken(raw string) (uuid.UUID, error) {
	claims := &jwt.RegisteredClaims{}

	_, err := jwt.ParseWithClaims(raw, claims,
		func(*jwt.Token) (any, error) { return t.secret, nil },
		// Pinning the algorithm is what closes the "alg: none" and
		// HS256-verified-as-RS256 confusion classes: the parser is told what to
		// expect instead of trusting the token's own header.
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(tokenIssuer),
		jwt.WithAudience(tokenAudience),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			// Told apart from a malformed token on purpose: the client's correct
			// response to an expired token is to refresh, not to log in again.
			return uuid.Nil, ErrTokenExpired
		}
		return uuid.Nil, fmt.Errorf("%w: %v", ErrTokenInvalid, err)
	}

	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: subject is not a uuid", ErrTokenInvalid)
	}
	return userID, nil
}

// opaqueTokenBytes is 32 bytes — 256 bits — of entropy, which puts guessing one
// out of reach regardless of how fast the check is. Both the refresh token and
// the device token are this shape: a random string with nothing about it to
// verify except "does the database have this hash".
const opaqueTokenBytes = 32

// newOpaqueToken returns a fresh random token and its stored hash. Refresh
// tokens and device tokens are both built from this: they need to be
// revocable, which means a server-side record either way, and once there is a
// record, a self-describing token (a JWT, say) buys nothing and only adds a
// second format to get wrong.
func newOpaqueToken() (token string, hash string, err error) {
	raw := make([]byte, opaqueTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("auth: generating token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, hashOpaqueToken(token), nil
}

// hashOpaqueToken is the value stored in the database for a refresh or device
// token.
//
// SHA-256 rather than bcrypt, deliberately. bcrypt's work factor exists to slow
// down guessing of low-entropy, human-chosen secrets; a 256-bit random value has
// nothing to guess, so the cost would buy no security and would be paid on every
// single refresh or PIN unlock. What matters here is that the database never
// holds the usable credential — a leaked dump cannot be replayed as a set of
// live sessions or trusted devices.
func hashOpaqueToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// NewRefreshToken returns a fresh opaque token and the hash to store for it.
func NewRefreshToken() (token string, hash string, err error) { return newOpaqueToken() }

// HashRefreshToken is the value stored in the database for a refresh token.
func HashRefreshToken(token string) string { return hashOpaqueToken(token) }

// NewDeviceToken returns a fresh opaque token and the hash to store for it.
//
// A separate name from NewRefreshToken even though the shape is identical: a
// device token proves "this browser may unlock a session with a PIN", which is
// a weaker claim than a refresh token's "this browser has a live session", and
// the two must never be interchangeable in code that reads either name.
func NewDeviceToken() (token string, hash string, err error) { return newOpaqueToken() }

// HashDeviceToken is the value stored in the database for a device token.
func HashDeviceToken(token string) string { return hashOpaqueToken(token) }

// CaptureKeyPrefix starts every capture key, so the shortcut endpoint can turn
// away anything else before it looks anything up, and a key pasted somewhere
// it does not belong can be recognised for what it is.
const CaptureKeyPrefix = "chn_"

// NewCaptureKey returns a fresh capture key and the hash to store for it.
//
// The random part is the same shape as a refresh or device token, and it gets
// names of its own for the same reason a device token does, with more at
// stake: a capture key lives in plain text inside a phone's shortcut, synced
// through iCloud, and all it proves is "this shortcut may note purchases in one
// account". It must never be read as a session, a device or a user — which is
// why it carries a prefix neither of the others has, and why nothing that
// accepts one of those ever accepts this.
func NewCaptureKey() (key string, hash string, err error) {
	token, _, err := newOpaqueToken()
	if err != nil {
		return "", "", err
	}
	key = CaptureKeyPrefix + token
	return key, HashCaptureKey(key), nil
}

// HashCaptureKey is the value stored in the database for a capture key: the
// hash of the whole key, prefix included.
func HashCaptureKey(key string) string { return hashOpaqueToken(key) }

// The prefixes of the two tokens Claude holds once the owner connects it
// (ADR-0004). They are opaque tokens like a refresh or device token, and get a
// prefix for the reason a capture key does: what they grant — chenchén's MCP
// tools, as that owner — is narrower than a session and different in kind, so
// nothing that takes a session, a device or a capture key may ever take one of
// these, and the one verifier that takes them takes nothing else. Require
// refuses both by their prefix before parsing anything.
const (
	OAuthAccessTokenPrefix  = "chno_"
	OAuthRefreshTokenPrefix = "chnr_"
)

// NewOAuthAccessToken returns a fresh access token for Claude and the hash to
// store for it.
func NewOAuthAccessToken() (token string, hash string, err error) {
	return newPrefixedToken(OAuthAccessTokenPrefix)
}

// NewOAuthRefreshToken returns a fresh refresh token for Claude and the hash
// to store for it. It lives only in oauth_tokens, never in refresh_tokens.
func NewOAuthRefreshToken() (token string, hash string, err error) {
	return newPrefixedToken(OAuthRefreshTokenPrefix)
}

// NewOAuthCode returns a fresh authorization code and the hash to store for
// it. A code is never a bearer credential on its own — it is worthless without
// the PKCE verifier — so it carries no prefix.
func NewOAuthCode() (code string, hash string, err error) { return newOpaqueToken() }

// HashOAuthToken is the value stored in the database for an OAuth access
// token, refresh token or code: the hash of the whole string, prefix included.
func HashOAuthToken(token string) string { return hashOpaqueToken(token) }

// LooksLikeOAuthAccessToken reports whether token has the exact shape
// NewOAuthAccessToken gives one, so a verifier can turn away anything else —
// a session's JWT, a capture key, a refresh token — before a lookup.
func LooksLikeOAuthAccessToken(token string) bool {
	return looksLikePrefixedToken(token, OAuthAccessTokenPrefix)
}

// LooksLikeOAuthRefreshToken is LooksLikeOAuthAccessToken for refresh tokens.
func LooksLikeOAuthRefreshToken(token string) bool {
	return looksLikePrefixedToken(token, OAuthRefreshTokenPrefix)
}

// isOAuthToken reports whether token claims to be one of Claude's, whatever
// follows the prefix: enough to refuse it where a session is expected.
func isOAuthToken(token string) bool {
	return strings.HasPrefix(token, OAuthAccessTokenPrefix) || strings.HasPrefix(token, OAuthRefreshTokenPrefix)
}

// newPrefixedToken is a fresh opaque token behind prefix, and the hash of the
// whole of it.
func newPrefixedToken(prefix string) (token string, hash string, err error) {
	raw, _, err := newOpaqueToken()
	if err != nil {
		return "", "", err
	}
	token = prefix + raw
	return token, hashOpaqueToken(token), nil
}

// opaqueTokenLen is the length of an opaque token's 32 bytes in unpadded
// base64url.
const opaqueTokenLen = 43

// looksLikePrefixedToken reports whether token is prefix followed by exactly
// an opaque token's 43 base64url characters.
func looksLikePrefixedToken(token, prefix string) bool {
	if len(token) != len(prefix)+opaqueTokenLen || !strings.HasPrefix(token, prefix) {
		return false
	}
	for _, r := range token[len(prefix):] {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}
