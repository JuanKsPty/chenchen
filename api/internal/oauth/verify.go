package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"

	"github.com/JuanKsPty/chenchen/api/internal/auth"
	"github.com/JuanKsPty/chenchen/api/internal/logging"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// ExtraGrantID is the key under which Verify puts the grant's ID, a
// uuid.UUID, in TokenInfo.Extra: what a per-connection limit or an audit line
// is keyed by.
const ExtraGrantID = "grant_id"

// Verify checks an access token Claude presents to the MCP endpoint. It
// matches the go-sdk's auth.TokenVerifier, for auth.RequireBearerToken.
//
// It takes only chno_ access tokens, and only ones whose grant is live and was
// issued for this server's resource. A web session's JWT, a capture key, a
// refresh token or anything else is refused by its shape, before any lookup,
// with auth.ErrInvalidToken — never with something that could read as a
// server fault. The user it returns is the grant's, which the owner approved
// in their own session: nothing in the request can name another.
func (s *Service) Verify(ctx context.Context, token string, _ *http.Request) (*mcpauth.TokenInfo, error) {
	if !auth.LooksLikeOAuthAccessToken(token) {
		return nil, fmt.Errorf("%w: not an access token", mcpauth.ErrInvalidToken)
	}
	now := s.now()
	t, g, err := s.db.Q().LiveOAuthAccessToken(ctx, auth.HashOAuthToken(token), now)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: unknown, expired or revoked", mcpauth.ErrInvalidToken)
	}
	if err != nil {
		return nil, err
	}
	// The token's audience: a grant made while the app answered at another
	// address is not for this resource.
	if !sameResource(g.Resource, s.resource) {
		return nil, fmt.Errorf("%w: issued for another resource", mcpauth.ErrInvalidToken)
	}

	if g.LastUsedAt == nil || now.Sub(*g.LastUsedAt) >= touchEvery {
		// Bookkeeping for Conexiones; a failure here is no reason to turn
		// the call away.
		if err := s.db.Q().TouchOAuthGrant(ctx, g.ID, now, touchEvery); err != nil {
			logging.FromContext(ctx).Warn("recording a connection's last use failed", "grant_id", g.ID, "error", err)
		}
	}

	// The scopes are the token's own, which a refresh may have narrowed from
	// the grant's.
	return &mcpauth.TokenInfo{
		UserID:     t.UserID.String(),
		Scopes:     t.Scopes,
		Expiration: t.ExpiresAt,
		Extra:      map[string]any{ExtraGrantID: g.ID},
	}, nil
}

// Verifier is Verify as the go-sdk's type, for wiring.
func (s *Service) Verifier() mcpauth.TokenVerifier { return s.Verify }

// GrantID reads the grant Verify put in info, if any.
func GrantID(info *mcpauth.TokenInfo) (uuid.UUID, bool) {
	if info == nil {
		return uuid.Nil, false
	}
	id, ok := info.Extra[ExtraGrantID].(uuid.UUID)
	return id, ok
}
