package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/JuanKsPty/chenchen/api/internal/httpx"
	"github.com/JuanKsPty/chenchen/api/internal/logging"
	"github.com/JuanKsPty/chenchen/api/internal/oauth"
)

// HTTPOptions is what the endpoint Claude reaches needs besides the services.
type HTTPOptions struct {
	// Verifier checks Claude's access tokens: oauth.Service.Verifier. It
	// takes nothing else, so a web session or a shortcut key is refused.
	Verifier mcpauth.TokenVerifier
	// ResourceMetadataURL is where the endpoint's protected resource
	// metadata is (RFC 9728), named in every refusal so a client can find
	// out how to sign in.
	ResourceMetadataURL string
	// Limiter counts the calls of each connection.
	Limiter *httpx.RateLimiter
}

// GrantCallsPerMinute is how many requests one connection may make a minute.
// Counted per grant, not per address: all of claude.ai reaches the endpoint
// from a handful of Anthropic's addresses, so a limit per address would be one
// limit for everybody.
const GrantCallsPerMinute = 60

// maxRequestBody bounds one JSON-RPC request. Every tool's arguments are a few
// fields; a file never travels this way.
const maxRequestBody = 1 << 20

// advertisedScopes is what a refusal tells a client to ask for when it signs
// in. Read is all the endpoint requires, but a client asks for what the
// refusal names, and a connection that never asked to write could never be
// granted it: the owner switches write off on the consent page instead.
var advertisedScopes = []string{oauth.ScopeRead, oauth.ScopeWrite}

// NewHTTPHandler is the MCP endpoint Claude reaches, /api/mcp: a bearer token
// checked before anything else, a limit per connection, and then a server
// built for the token's owner and scopes on each request (NewServer).
//
// It is stateless and answers in plain JSON: no session is kept between
// requests and no stream stays open, so the endpoint holds nothing in memory
// between calls.
func NewHTTPHandler(deps Deps, opts HTTPOptions) http.Handler {
	cache := mcp.NewSchemaCache()
	streamable := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		info := mcpauth.TokenInfoFromContext(r.Context())
		logger := logging.FromContext(r.Context())
		if info == nil {
			return nil
		}
		userID, err := uuid.Parse(info.UserID)
		if err != nil {
			logger.Error("an MCP token names no user", "error", err)
			return nil
		}
		grantID, _ := oauth.GrantID(info)
		server, err := NewServer(deps, userID, info.Scopes, RemoteOptions{GrantID: grantID, Logger: logger,
			SchemaCache: cache})
		if err != nil {
			logger.Error("building the MCP server failed", "error", err)
			return nil
		}
		return server
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, MaxRequestBodyBytes: maxRequestBody})

	limited := limitPerGrant(opts.Limiter, streamable)
	require := mcpauth.RequireBearerToken(quietVerifier(opts.Verifier), &mcpauth.RequireBearerTokenOptions{
		ResourceMetadataURL: opts.ResourceMetadataURL,
		Scopes:              []string{oauth.ScopeRead},
	})
	challenge := fmt.Sprintf("Bearer resource_metadata=%q, scope=%q", opts.ResourceMetadataURL,
		strings.Join(advertisedScopes, " "))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A refusal names every scope; the request that gets through reaches
		// the server with the response writer as it came.
		require(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			limited.ServeHTTP(w, r)
		})).ServeHTTP(&challengeWriter{ResponseWriter: w, challenge: challenge}, r)
	})
}

// challengeWriter replaces the WWW-Authenticate of a 401 with challenge.
type challengeWriter struct {
	http.ResponseWriter
	challenge string
}

func (c *challengeWriter) WriteHeader(status int) {
	if status == http.StatusUnauthorized {
		c.Header().Set("WWW-Authenticate", c.challenge)
	}
	c.ResponseWriter.WriteHeader(status)
}

// quietVerifier keeps a failure of the app's own — the database, say — out of
// the response: RequireBearerToken writes a verifier's error into its body.
func quietVerifier(verify mcpauth.TokenVerifier) mcpauth.TokenVerifier {
	return func(ctx context.Context, token string, r *http.Request) (*mcpauth.TokenInfo, error) {
		info, err := verify(ctx, token, r)
		if err != nil && !errors.Is(err, mcpauth.ErrInvalidToken) {
			logging.FromContext(r.Context()).Error("verifying an MCP token failed", "error", err)
			return nil, errors.New("internal error")
		}
		return info, err
	}
}

// limitPerGrant turns away a connection over GrantCallsPerMinute, with how
// long to wait.
func limitPerGrant(limiter *httpx.RateLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := mcpauth.TokenInfoFromContext(r.Context())
		key := "user:" + info.UserID
		if grantID, ok := oauth.GrantID(info); ok {
			key = "grant:" + grantID.String()
		}
		if allowed, retryAfter := limiter.Allow(key); !allowed {
			httpx.SetRetryAfter(w, retryAfter)
			httpx.Fail(w, r, httpx.TooManyRequests("Demasiadas llamadas a chenchén en un minuto. Espera un momento."))
			return
		}
		next.ServeHTTP(w, r)
	})
}
