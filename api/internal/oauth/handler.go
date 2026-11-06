package oauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/auth"
	"github.com/JuanKsPty/chenchen/api/internal/httpx"
	"github.com/JuanKsPty/chenchen/api/internal/identity"
	"github.com/JuanKsPty/chenchen/api/internal/logging"
)

// Handler serves /api/oauth and the discovery documents.
type Handler struct {
	svc    *Service
	tokens *auth.TokenIssuer
	// limiter caps, per address, the two endpoints anyone can reach without
	// a session: authorize writes a row and token checks a secret.
	limiter *httpx.RateLimiter
}

func NewHandler(svc *Service, tokens *auth.TokenIssuer, limiter *httpx.RateLimiter) *Handler {
	return &Handler{svc: svc, tokens: tokens, limiter: limiter}
}

// Routes returns the /api/oauth subrouter.
//
// Two halves. Claude's — authorize and token — has no session: the browser
// arrives from Claude, and Claude's servers exchange codes. The owner's — a
// request's details, the answer, and the list of connections — runs in their
// web session, the way the security endpoints run inside the auth router.
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/authorize", h.authorize)
	r.Post("/token", h.token)

	r.Group(func(r chi.Router) {
		r.Use(auth.Require(h.tokens))
		r.Get("/requests/{id}", h.request)
		r.Post("/requests/{id}/approve", h.approve)
		r.Post("/requests/{id}/deny", h.deny)
		r.Get("/grants", h.grants)
		r.Delete("/grants/{id}", h.revoke)
	})
	return r
}

// limited spends one of the address's requests to endpoint and reports
// whether none was left, and how long until one is; the caller refuses in its
// endpoint's own terms.
func (h *Handler) limited(r *http.Request, endpoint string) (bool, time.Duration) {
	ok, retryAfter := h.limiter.Allow(endpoint + " " + httpx.ClientIP(r))
	return !ok, retryAfter
}

// --- authorize ----------------------------------------------------------------

// authorize is where Claude sends the person's browser to ask for a
// connection.
//
// A refusal that can go back to the client goes there, by redirect (RFC 6749
// §4.1.2.1). Three cannot: an unknown client and a redirect URI that is not
// the client's — in meaning a 400, since nothing in the request can be trusted
// to send anyone anywhere — and too many requests from this address, in
// meaning a 429. The request is a top-level navigation, though, so a 400 or a
// 429 with the API's JSON envelope would be all the person sees. Instead the
// browser goes to the consent page with error=invalid_client,
// invalid_redirect or rate_limited, and the page explains it in Spanish. The
// redirect URI the request named is never used, and nothing is stored.
func (h *Handler) authorize(w http.ResponseWriter, r *http.Request) {
	// No Retry-After: on a redirect it would ask the browser to wait before
	// following it, not before asking again.
	if limited, _ := h.limited(r, "authorize"); limited {
		http.Redirect(w, r, h.svc.ConsentPage(url.Values{"error": {RefusedTooMany}}), http.StatusFound)
		return
	}

	consent, err := h.svc.Authorize(r.Context(), r.URL.Query())
	var (
		untrusted *UntrustedError
		refused   *RedirectError
	)
	switch {
	case errors.As(err, &untrusted):
		logging.FromContext(r.Context()).Debug("authorization request refused", "error", err)
		http.Redirect(w, r, h.svc.ConsentPage(url.Values{"error": {untrusted.Code}}), http.StatusFound)
		return
	case errors.As(err, &refused):
		to, err := h.svc.redirect(refused.RedirectURI, url.Values{
			"error": {refused.OAuth.Code}, "error_description": {refused.OAuth.Description}, "state": {refused.State},
		})
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		http.Redirect(w, r, to, http.StatusFound)
		return
	case err != nil:
		httpx.Fail(w, r, err)
		return
	}
	http.Redirect(w, r, consent, http.StatusFound)
}

// --- token --------------------------------------------------------------------

// maxTokenRequest caps a token request's body. It carries a handful of short
// parameters; anything bigger is not a token request.
const maxTokenRequest = 16 << 10

// tokenResponse is RFC 6749 §5.1's.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// tokenErrorResponse is RFC 6749 §5.2's, not the API's own envelope: the
// clients reading it are OAuth libraries.
type tokenErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

func (h *Handler) token(w http.ResponseWriter, r *http.Request) {
	if limited, retryAfter := h.limited(r, "token"); limited {
		httpx.SetRetryAfter(w, retryAfter)
		writeTokenJSON(w, http.StatusTooManyRequests, tokenErrorResponse{
			Error: "temporarily_unavailable", ErrorDescription: "too many requests; retry later"})
		return
	}

	form, err := readTokenForm(w, r)
	if err != nil {
		h.tokenError(w, r, err)
		return
	}

	var pair TokenPair
	switch form.Get("grant_type") {
	case "authorization_code":
		pair, err = h.svc.Exchange(r.Context(), ExchangeInput{
			Code:        form.Get("code"),
			Verifier:    form.Get("code_verifier"),
			ClientID:    form.Get("client_id"),
			RedirectURI: form.Get("redirect_uri"),
			Resource:    form.Get("resource"),
		})
	case "refresh_token":
		pair, err = h.svc.Refresh(r.Context(), RefreshInput{
			RefreshToken: form.Get("refresh_token"),
			ClientID:     form.Get("client_id"),
			Resource:     form.Get("resource"),
			Scope:        form.Get("scope"),
		})
	case "":
		err = invalidRequest("grant_type is required")
	default:
		err = &Error{Code: "unsupported_grant_type", Description: "supported grant types are authorization_code and refresh_token"}
	}
	if err != nil {
		h.tokenError(w, r, err)
		return
	}

	writeTokenJSON(w, http.StatusOK, tokenResponse{
		AccessToken:  pair.AccessToken,
		TokenType:    "Bearer",
		ExpiresIn:    max(1, int64(pair.ExpiresIn.Seconds())),
		RefreshToken: pair.RefreshToken,
		Scope:        strings.Join(pair.Scopes, " "),
	})
}

// readTokenForm reads a token request's form-encoded body, which is the only
// encoding RFC 6749 §3.2 allows; the query string is never read, so nothing
// secret is taken from a URL. A parameter sent twice is refused (§3.1).
func readTokenForm(w http.ResponseWriter, r *http.Request) (url.Values, error) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		return nil, invalidRequest("the body must be application/x-www-form-urlencoded")
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxTokenRequest))
	if err != nil {
		return nil, invalidRequest("the body could not be read")
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, invalidRequest("the body is not a valid form")
	}
	for name, values := range form {
		if len(values) > 1 {
			return nil, invalidRequest(name + " is repeated")
		}
	}
	return form, nil
}

// tokenError writes err the way RFC 6749 §5.2 says. Anything that is not one
// of this package's refusals is a fault of ours: logged in full, and reported
// to the client as server_error and nothing more.
func (h *Handler) tokenError(w http.ResponseWriter, r *http.Request, err error) {
	var oauthErr *Error
	if !errors.As(err, &oauthErr) {
		logging.FromContext(r.Context()).Error("token request failed", "error", err)
		writeTokenJSON(w, http.StatusInternalServerError, tokenErrorResponse{Error: "server_error"})
		return
	}
	logging.FromContext(r.Context()).Debug("token request refused", "code", oauthErr.Code,
		"description", oauthErr.Description)
	status := http.StatusBadRequest
	if oauthErr.Code == "invalid_client" {
		status = http.StatusUnauthorized
	}
	writeTokenJSON(w, status, tokenErrorResponse{Error: oauthErr.Code, ErrorDescription: oauthErr.Description})
}

// writeTokenJSON writes a token endpoint response, which must never be cached
// by anything between here and the client (RFC 6749 §5.1).
func writeTokenJSON(w http.ResponseWriter, status int, v any) {
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Pragma", "no-cache")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// --- the owner's side ------------------------------------------------------------

// requestView is what the consent page renders.
type requestView struct {
	ClientName   string    `json:"client_name"`
	RedirectHost string    `json:"redirect_host"`
	Scopes       []string  `json:"scopes"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func (h *Handler) request(w http.ResponseWriter, r *http.Request) {
	id, err := requestID(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	req, err := h.svc.Request(r.Context(), identity.MustFromContext(r.Context()), id)
	if err != nil {
		httpx.Fail(w, r, translate(err))
		return
	}
	httpx.JSON(w, r, http.StatusOK, requestView{
		ClientName: req.ClientName, RedirectHost: req.RedirectHost, Scopes: req.Scopes, ExpiresAt: req.ExpiresAt,
	})
}

// approveRequest is the owner's yes, and whether Claude may also write.
type approveRequest struct {
	Write bool `json:"write"`
}

// answerView tells the consent page where to send the browser next: back to
// Claude, with the answer.
type answerView struct {
	RedirectTo string `json:"redirect_to"`
}

func (h *Handler) approve(w http.ResponseWriter, r *http.Request) {
	var body approveRequest
	if err := httpx.Decode(w, r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	id, err := requestID(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	to, err := h.svc.Approve(r.Context(), identity.MustFromContext(r.Context()), id, body.Write)
	if err != nil {
		httpx.Fail(w, r, translate(err))
		return
	}
	httpx.JSON(w, r, http.StatusOK, answerView{RedirectTo: to})
}

func (h *Handler) deny(w http.ResponseWriter, r *http.Request) {
	id, err := requestID(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	to, err := h.svc.Deny(r.Context(), identity.MustFromContext(r.Context()), id)
	if err != nil {
		httpx.Fail(w, r, translate(err))
		return
	}
	httpx.JSON(w, r, http.StatusOK, answerView{RedirectTo: to})
}

// grantView is one connection in Conexiones.
type grantView struct {
	ID         uuid.UUID  `json:"id"`
	ClientName string     `json:"client_name"`
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

func (h *Handler) grants(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.Grants(r.Context(), identity.MustFromContext(r.Context()))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out := make([]grantView, 0, len(list))
	for _, g := range list {
		out = append(out, grantView{ID: g.ID, ClientName: g.ClientName, Scopes: g.Scopes, CreatedAt: g.CreatedAt,
			LastUsedAt: g.LastUsedAt})
	}
	httpx.JSON(w, r, http.StatusOK, map[string]any{"grants": out})
}

func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Fail(w, r, httpx.NotFound("not_found", "No encontramos lo que buscas.").WithCause(err))
		return
	}
	if err := h.svc.Revoke(r.Context(), identity.MustFromContext(r.Context()), id); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.NoContent(w)
}

// requestID reads the {id} URL parameter; a malformed one is missing, like an
// expired one.
func requestID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return uuid.Nil, translate(fmt.Errorf("%w: %q", ErrRequestNotFound, chi.URLParam(r, "id")))
	}
	return id, nil
}

// translate gives the owner's side its words.
func translate(err error) error {
	if errors.Is(err, ErrRequestNotFound) {
		return httpx.NotFound("oauth_request_not_found",
			"Esta solicitud ya no está vigente. Vuelve a conectar chenchén desde Claude.").WithCause(err)
	}
	return err
}
