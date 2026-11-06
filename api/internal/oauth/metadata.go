package oauth

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/JuanKsPty/chenchen/api/internal/httpx"
)

// protectedResourcePath is where RFC 9728 puts the metadata of a resource
// whose path is /api/mcp. The variant without the path is served too: some
// clients look there first.
const protectedResourcePath = "/oauth-protected-resource"

// WellKnownRoutes returns the discovery documents, mounted at /.well-known at
// the root of the router — not under /api, since RFC 8414 and RFC 9728 derive
// these addresses from the issuer and the resource themselves.
func (h *Handler) WellKnownRoutes() http.Handler {
	r := chi.NewRouter()
	prm := mcpauth.ProtectedResourceMetadataHandler(h.protectedResourceMetadata())
	r.Method(http.MethodGet, protectedResourcePath+"/api/mcp", prm)
	r.Method(http.MethodGet, protectedResourcePath, prm)
	r.Get("/oauth-authorization-server", h.authServerMetadata)
	return r
}

// protectedResourceMetadata says what the MCP endpoint is and who issues its
// tokens (RFC 9728).
func (h *Handler) protectedResourceMetadata() *oauthex.ProtectedResourceMetadata {
	return &oauthex.ProtectedResourceMetadata{
		Resource:               h.svc.Resource(),
		AuthorizationServers:   []string{h.svc.Issuer()},
		ScopesSupported:        []string{ScopeRead, ScopeWrite},
		BearerMethodsSupported: []string{"header"},
		ResourceName:           "chenchén",
	}
}

// authServerMetadata is RFC 8414's document, in a struct of its own rather
// than oauthex.AuthServerMeta: that one always writes jwks_uri, and an empty
// URL where a client expects an optional one is a validation failure in some
// of them. Nothing here signs anything, so there is no key set to point at.
type authServerMetadata struct {
	Issuer                                     string   `json:"issuer"`
	AuthorizationEndpoint                      string   `json:"authorization_endpoint"`
	TokenEndpoint                              string   `json:"token_endpoint"`
	ScopesSupported                            []string `json:"scopes_supported"`
	ResponseTypesSupported                     []string `json:"response_types_supported"`
	GrantTypesSupported                        []string `json:"grant_types_supported"`
	TokenEndpointAuthMethodsSupported          []string `json:"token_endpoint_auth_methods_supported"`
	CodeChallengeMethodsSupported              []string `json:"code_challenge_methods_supported"`
	ClientIDMetadataDocumentSupported          bool     `json:"client_id_metadata_document_supported"`
	AuthorizationResponseIssParameterSupported bool     `json:"authorization_response_iss_parameter_supported"`
}

func (h *Handler) authServerMetadata(w http.ResponseWriter, r *http.Request) {
	issuer := h.svc.Issuer()
	// Public by nature, like the resource metadata the go-sdk serves with the
	// same header: a browser-based client must be able to read it.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	// Never cached: a client holding a stale copy after an address change
	// would send people to the wrong place.
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, r, http.StatusOK, authServerMetadata{
		Issuer:                            issuer,
		AuthorizationEndpoint:             issuer + "/api/oauth/authorize",
		TokenEndpoint:                     issuer + "/api/oauth/token",
		ScopesSupported:                   []string{ScopeRead, ScopeWrite, scopeOfflineAccess},
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		// Claude's two clients are known by their metadata documents' URLs,
		// fixed in this package; see clients.
		ClientIDMetadataDocumentSupported: true,
		// Every redirect carries iss (RFC 9207).
		AuthorizationResponseIssParameterSupported: true,
	})
}
