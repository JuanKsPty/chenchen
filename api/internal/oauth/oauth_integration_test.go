package oauth_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"

	"github.com/JuanKsPty/chenchen/api/internal/accounts"
	"github.com/JuanKsPty/chenchen/api/internal/auth"
	"github.com/JuanKsPty/chenchen/api/internal/captures"
	"github.com/JuanKsPty/chenchen/api/internal/categories"
	"github.com/JuanKsPty/chenchen/api/internal/chat"
	"github.com/JuanKsPty/chenchen/api/internal/config"
	"github.com/JuanKsPty/chenchen/api/internal/httpx"
	"github.com/JuanKsPty/chenchen/api/internal/imports"
	"github.com/JuanKsPty/chenchen/api/internal/investments"
	"github.com/JuanKsPty/chenchen/api/internal/mcpserver"
	"github.com/JuanKsPty/chenchen/api/internal/movements"
	"github.com/JuanKsPty/chenchen/api/internal/oauth"
	"github.com/JuanKsPty/chenchen/api/internal/proposals"
	"github.com/JuanKsPty/chenchen/api/internal/reports"
	"github.com/JuanKsPty/chenchen/api/internal/rules"
	"github.com/JuanKsPty/chenchen/api/internal/server"
	"github.com/JuanKsPty/chenchen/api/internal/store"
	"github.com/JuanKsPty/chenchen/api/internal/storetest"
	"github.com/JuanKsPty/chenchen/api/internal/transfers"
)

const (
	publicURL      = "https://chenchen.example.com"
	resource       = publicURL + "/api/mcp"
	claudeCallback = "https://claude.ai/api/mcp/auth_callback"
	state          = "st4te-from-claude"
)

func TestDiscoveryNamesTheIssuerAndTheResource(t *testing.T) {
	r := newRig(t, true)

	for _, path := range []string{"/.well-known/oauth-protected-resource/api/mcp", "/.well-known/oauth-protected-resource"} {
		rec := r.serve(t, "", http.MethodGet, path, "", "")
		var prm map[string]any
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &prm) != nil {
			t.Fatalf("GET %s = %d %s", path, rec.Code, rec.Body)
		}
		if prm["resource"] != resource || !sameJSON(prm["authorization_servers"], []string{publicURL}) ||
			!sameJSON(prm["scopes_supported"], []string{"read", "write"}) ||
			!sameJSON(prm["bearer_methods_supported"], []string{"header"}) {
			t.Errorf("GET %s = %s", path, rec.Body)
		}
	}

	rec := r.serve(t, "", http.MethodGet, "/.well-known/oauth-authorization-server", "", "")
	var as map[string]any
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &as) != nil {
		t.Fatalf("GET authorization server = %d %s", rec.Code, rec.Body)
	}
	want := map[string]any{
		"issuer":                                         publicURL,
		"authorization_endpoint":                         publicURL + "/api/oauth/authorize",
		"token_endpoint":                                 publicURL + "/api/oauth/token",
		"response_types_supported":                       []string{"code"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"scopes_supported":                               []string{"read", "write", "offline_access"},
		"client_id_metadata_document_supported":          true,
		"authorization_response_iss_parameter_supported": true,
	}
	for key, value := range want {
		if !sameJSON(as[key], value) {
			t.Errorf("%s = %v, want %v", key, as[key], value)
		}
	}
	if _, ok := as["jwks_uri"]; ok {
		t.Error("an empty jwks_uri is announced")
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
	}
}

func TestWithoutAPublicURLClaudeFindsNothing(t *testing.T) {
	r := newRig(t, false)
	for _, tt := range []struct{ method, path string }{
		{http.MethodGet, "/.well-known/oauth-protected-resource/api/mcp"},
		{http.MethodGet, "/.well-known/oauth-protected-resource"},
		{http.MethodGet, "/.well-known/oauth-authorization-server"},
		{http.MethodGet, "/api/oauth/authorize?" + r.params(newPKCE(t).challenge).Encode()},
		{http.MethodPost, "/api/oauth/token"},
		{http.MethodGet, "/api/oauth/grants"},
	} {
		rec := r.serve(t, r.session(t, r.owner), tt.method, tt.path, "", "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404", tt.method, tt.path, rec.Code)
		}
	}
	// And the API is otherwise itself.
	if rec := r.serve(t, r.session(t, r.owner), http.MethodGet, "/api/accounts", "", ""); rec.Code != http.StatusOK {
		t.Errorf("GET /api/accounts = %d", rec.Code)
	}
}

func TestAnUnknownClientOrRedirectIsExplainedOnTheConsentPageAndNeverSentBack(t *testing.T) {
	r := newRig(t, true)
	challenge := newPKCE(t).challenge

	for _, tt := range []struct {
		name, code string
		change     func(url.Values)
	}{
		{"no client", "invalid_client", func(q url.Values) { q.Del("client_id") }},
		{"unknown client", "invalid_client", func(q url.Values) { q.Set("client_id", "https://claude.ai/oauth/someone-else") }},
		{"two clients", "invalid_client", func(q url.Values) { q.Add("client_id", oauth.ClaudeCodeClientID) }},
		{"no redirect", "invalid_redirect", func(q url.Values) { q.Del("redirect_uri") }},
		{"foreign redirect", "invalid_redirect", func(q url.Values) {
			q.Set("redirect_uri", "https://attacker.example.com/api/mcp/auth_callback")
		}},
		{"near redirect", "invalid_redirect", func(q url.Values) { q.Set("redirect_uri", claudeCallback+"/") }},
		{"loopback redirect", "invalid_redirect", func(q url.Values) { q.Set("redirect_uri", "http://localhost:53682/callback") }},
		{"two redirects", "invalid_redirect", func(q url.Values) { q.Add("redirect_uri", "https://attacker.example.com/cb") }},
		{"code's redirect", "invalid_redirect", func(q url.Values) {
			q.Set("client_id", oauth.ClaudeCodeClientID)
			q.Set("redirect_uri", "https://attacker.example.com/callback")
		}},
		// Even with everything else wrong too: the redirect is never trusted.
		{"foreign redirect and no PKCE", "invalid_redirect", func(q url.Values) {
			q.Set("redirect_uri", "https://attacker.example.com/cb")
			q.Del("code_challenge")
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			q := r.params(challenge)
			tt.change(q)
			// A browser navigation: a 400 would leave the person reading JSON.
			rec := r.authorize(t, q)
			if want := publicURL + "/conectar?error=" + tt.code; rec.Code != http.StatusFound ||
				rec.Header().Get("Location") != want {
				t.Errorf("= %d, Location %q, want a 302 to %s", rec.Code, rec.Header().Get("Location"), want)
			}
		})
	}
	if n := r.count(t, "oauth_requests"); n != 0 {
		t.Errorf("%d requests stored", n)
	}
}

func TestTooManyAuthorizeRequestsAreExplainedOnTheConsentPage(t *testing.T) {
	r := newRigWith(t, true, 2)
	for range 2 {
		r.requestID(t, r.authorize(t, r.params(newPKCE(t).challenge)))
	}

	rec := r.authorize(t, r.params(newPKCE(t).challenge))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != publicURL+"/conectar?error=rate_limited" {
		t.Errorf("third = %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}
	// Retry-After on a redirect would ask the browser to wait before following it.
	if got := rec.Header().Get("Retry-After"); got != "" {
		t.Errorf("Retry-After = %q", got)
	}
	if n := r.count(t, "oauth_requests"); n != 2 {
		t.Errorf("%d requests stored, want the first two", n)
	}
	// Token counts on its own.
	if status, body := r.token(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"chnr_x"}}); status != http.StatusBadRequest {
		t.Errorf("token = %d %v", status, body)
	}
}

func TestClaudeCodeSignsInOnAnyLoopbackPort(t *testing.T) {
	r := newRig(t, true)
	for _, callback := range []string{"http://localhost:53682/callback", "http://127.0.0.1:1/callback"} {
		p := newPKCE(t)
		q := r.params(p.challenge)
		q.Set("client_id", oauth.ClaudeCodeClientID)
		q.Set("redirect_uri", callback)
		id := r.requestID(t, r.authorize(t, q))

		view := r.view(t, r.owner, id)
		if view.ClientName != "Claude Code" || view.RedirectHost != strings.Split(strings.TrimPrefix(callback, "http://"), ":")[0] {
			t.Errorf("view = %+v", view)
		}
		code := codeFrom(t, r.approve(t, r.owner, id, false), callback)
		if _, status, body := r.exchange(t, code, p.verifier, oauth.ClaudeCodeClientID, callback); status != http.StatusOK {
			t.Errorf("exchange for %s = %d %v", callback, status, body)
		}
	}
}

func TestOnceTheClientIsKnownAuthorizeRefusesByRedirect(t *testing.T) {
	r := newRig(t, true)
	challenge := newPKCE(t).challenge

	for _, tt := range []struct {
		name, code string
		change     func(url.Values)
		wantState  bool
	}{
		{"no PKCE", "invalid_request", func(q url.Values) { q.Del("code_challenge"); q.Del("code_challenge_method") }, true},
		{"plain PKCE", "invalid_request", func(q url.Values) { q.Set("code_challenge_method", "plain") }, true},
		{"no method", "invalid_request", func(q url.Values) { q.Del("code_challenge_method") }, true},
		{"short challenge", "invalid_request", func(q url.Values) { q.Set("code_challenge", challenge[:42]) }, true},
		{"no state", "invalid_request", func(q url.Values) { q.Del("state") }, false},
		{"implicit", "unsupported_response_type", func(q url.Values) { q.Set("response_type", "token") }, true},
		{"unknown scope", "invalid_scope", func(q url.Values) { q.Set("scope", "read admin") }, true},
		{"an empty resource", "invalid_target", func(q url.Values) { q.Set("resource", "") }, true},
		{"another resource", "invalid_target", func(q url.Values) { q.Set("resource", publicURL+"/api/other") }, true},
		{"another host", "invalid_target", func(q url.Values) { q.Set("resource", "https://other.example.com/api/mcp") }, true},
		{"a repeated parameter", "invalid_request", func(q url.Values) { q.Add("scope", "write") }, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			q := r.params(challenge)
			tt.change(q)
			rec := r.authorize(t, q)
			loc, err := url.Parse(rec.Header().Get("Location"))
			if rec.Code != http.StatusFound || err != nil {
				t.Fatalf("= %d %s", rec.Code, rec.Body)
			}
			got := loc.Query()
			if loc.Scheme+"://"+loc.Host+loc.Path != claudeCallback || got.Get("error") != tt.code ||
				got.Get("iss") != publicURL || got.Get("code") != "" || (got.Get("state") == state) != tt.wantState {
				t.Errorf("Location = %s", loc)
			}
		})
	}
	if n := r.count(t, "oauth_requests"); n != 0 {
		t.Errorf("%d requests stored", n)
	}
}

func TestTheResourceIsForgivenItsCaseAndATrailingSlash(t *testing.T) {
	r := newRig(t, true)
	for _, res := range []string{resource, resource + "/", "HTTPS://Chenchen.EXAMPLE.com/api/mcp", "https://chenchen.example.com:443/api/mcp/"} {
		q := r.params(newPKCE(t).challenge)
		q.Set("resource", res)
		rec := r.authorize(t, q)
		if loc := rec.Header().Get("Location"); rec.Code != http.StatusFound ||
			!strings.HasPrefix(loc, publicURL+"/conectar?req=") {
			t.Errorf("resource %s: %d, Location %q", res, rec.Code, loc)
		}
	}
	// The case of the path is the path: another one is another resource.
	q := r.params(newPKCE(t).challenge)
	q.Set("resource", publicURL+"/API/mcp")
	if loc := r.authorize(t, q).Header().Get("Location"); !strings.Contains(loc, "error=invalid_target") {
		t.Errorf("Location = %q", loc)
	}
}

func TestAuthorizeWithoutAResourceIsForTheOneThisServerHas(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	p := newPKCE(t)
	q := r.params(p.challenge)
	q.Del("resource")

	id := r.requestID(t, r.authorize(t, q))
	r.view(t, r.owner, id)
	code := codeFrom(t, r.approve(t, r.owner, id, false), claudeCallback)
	// Nor does the exchange name one.
	tokens, status, body := r.tokenPair(t, url.Values{"grant_type": {"authorization_code"}, "code": {code},
		"code_verifier": {p.verifier}, "client_id": {oauth.ClaudeClientID}, "redirect_uri": {claudeCallback}})
	if status != http.StatusOK {
		t.Fatalf("exchange = %d %v", status, body)
	}
	// The verifier takes only tokens issued for this server's resource.
	if _, err := r.svc.Verify(ctx, tokens.AccessToken, nil); err != nil {
		t.Errorf("Verify = %v", err)
	}
}

func TestApprovingWithoutWriteGrantsOnlyRead(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()

	for _, write := range []bool{false, true} {
		p := newPKCE(t)
		id := r.requestID(t, r.authorize(t, r.params(p.challenge)))
		view := r.view(t, r.owner, id)
		if view.ClientName != "Claude" || view.RedirectHost != "claude.ai" ||
			!slices.Equal(view.Scopes, []string{"read", "write"}) || !view.ExpiresAt.Equal(r.now.Add(10*time.Minute)) {
			t.Errorf("view = %+v", view)
		}

		redirect := r.approve(t, r.owner, id, write)
		loc, _ := url.Parse(redirect)
		if q := loc.Query(); q.Get("state") != state || q.Get("iss") != publicURL || q.Get("code") == "" {
			t.Errorf("redirect_to = %s", redirect)
		}
		tokens, status, body := r.exchange(t, codeFrom(t, redirect, claudeCallback), p.verifier, oauth.ClaudeClientID, claudeCallback)
		want := "read"
		if write {
			want = "read write"
		}
		if status != http.StatusOK || tokens.Scope != want || tokens.TokenType != "Bearer" || tokens.ExpiresIn != 3600 ||
			!strings.HasPrefix(tokens.AccessToken, "chno_") || !strings.HasPrefix(tokens.RefreshToken, "chnr_") {
			t.Errorf("write %v: %d %v", write, status, body)
		}
		info, err := r.svc.Verify(ctx, tokens.AccessToken, nil)
		if err != nil || info.UserID != r.owner.String() || strings.Join(info.Scopes, " ") != want {
			t.Errorf("Verify = %+v, %v", info, err)
		}
	}

	// Claude's refresh tokens never become sessions.
	if n := r.count(t, "refresh_tokens"); n != 0 {
		t.Errorf("refresh_tokens has %d rows", n)
	}
	// Only hashes are stored.
	if n := r.count(t, "oauth_tokens WHERE hash LIKE 'chn%'"); n != 0 {
		t.Errorf("%d tokens stored in plain text", n)
	}
}

func TestDenyingSendsClaudeBackWithAccessDenied(t *testing.T) {
	r := newRig(t, true)
	id := r.requestID(t, r.authorize(t, r.params(newPKCE(t).challenge)))
	r.view(t, r.owner, id)

	rec := r.serve(t, r.session(t, r.owner), http.MethodPost, "/api/oauth/requests/"+id+"/deny", "", "")
	var answer struct {
		RedirectTo string `json:"redirect_to"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &answer) != nil {
		t.Fatalf("deny = %d %s", rec.Code, rec.Body)
	}
	loc, err := url.Parse(answer.RedirectTo)
	if q := loc.Query(); err != nil || !strings.HasPrefix(answer.RedirectTo, claudeCallback+"?") ||
		q.Get("error") != "access_denied" || q.Get("state") != state || q.Get("iss") != publicURL || q.Get("code") != "" {
		t.Errorf("redirect_to = %s", answer.RedirectTo)
	}

	// The answer was given: there is nothing left to approve.
	rec = r.serve(t, r.session(t, r.owner), http.MethodPost, "/api/oauth/requests/"+id+"/approve", "application/json", `{"write": true}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("approve after deny = %d %s", rec.Code, rec.Body)
	}
	if n := r.count(t, "oauth_codes"); n != 0 {
		t.Errorf("%d codes", n)
	}
}

func TestARequestIsAnsweredOnceAndOnlyByWhoeverOpenedIt(t *testing.T) {
	r := newRig(t, true)
	id := r.requestID(t, r.authorize(t, r.params(newPKCE(t).challenge)))

	r.view(t, r.owner, id)
	for _, path := range []string{"", "/approve", "/deny"} {
		method := http.MethodPost
		if path == "" {
			method = http.MethodGet
		}
		rec := r.serve(t, r.session(t, r.stranger), method, "/api/oauth/requests/"+id+path, "application/json", `{"write": false}`)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s by someone else = %d %s", path, rec.Code, rec.Body)
		}
	}
	r.approve(t, r.owner, id, false)
	rec := r.serve(t, r.session(t, r.owner), http.MethodPost, "/api/oauth/requests/"+id+"/approve", "application/json", `{"write": false}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("a second approval = %d %s", rec.Code, rec.Body)
	}

	// Ten minutes to answer.
	late := r.requestID(t, r.authorize(t, r.params(newPKCE(t).challenge)))
	r.now = r.now.Add(10 * time.Minute)
	if rec := r.serve(t, r.session(t, r.owner), http.MethodGet, "/api/oauth/requests/"+late, "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("an expired request = %d %s", rec.Code, rec.Body)
	}
	// Without a session, nobody answers anything.
	if rec := r.serve(t, "", http.MethodPost, "/api/oauth/requests/"+late+"/approve", "application/json", `{}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("approve without a session = %d", rec.Code)
	}
	for _, bad := range []string{"not-a-uuid", uuid.NewString()} {
		if rec := r.serve(t, r.session(t, r.owner), http.MethodGet, "/api/oauth/requests/"+bad, "", ""); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d", bad, rec.Code)
		}
	}
}

func TestARequestNobodyOpenedCannotBeAnswered(t *testing.T) {
	r := newRig(t, true)
	id := r.requestID(t, r.authorize(t, r.params(newPKCE(t).challenge)))

	// A POST with the request's ID, from a page the person never saw, finds
	// nothing to answer — whoever's session it carries.
	for _, user := range []uuid.UUID{r.owner, r.stranger} {
		for _, answer := range []string{"/approve", "/deny"} {
			rec := r.serve(t, r.session(t, user), http.MethodPost, "/api/oauth/requests/"+id+answer, "application/json", `{"write": true}`)
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s unopened = %d %s", answer, rec.Code, rec.Body)
			}
		}
	}
	if n := r.count(t, "oauth_codes"); n != 0 {
		t.Errorf("%d codes", n)
	}

	// Still there for the person who opens it.
	r.view(t, r.owner, id)
	codeFrom(t, r.approve(t, r.owner, id, false), claudeCallback)
}

func TestACodeIsExchangedOnlyWithItsVerifier(t *testing.T) {
	r := newRig(t, true)
	p := newPKCE(t)
	code := r.code(t, r.owner, p, false)

	for name, verifier := range map[string]string{
		"no verifier":    "",
		"plain verifier": p.challenge,
		"wrong verifier": newPKCE(t).verifier,
	} {
		if _, status, body := r.exchange(t, code, verifier, oauth.ClaudeClientID, claudeCallback); status != http.StatusBadRequest ||
			body["error"] != "invalid_grant" {
			t.Errorf("%s = %d %v", name, status, body)
		}
	}
	// Nor for another client, or to another redirect.
	if _, status, body := r.exchange(t, code, p.verifier, oauth.ClaudeCodeClientID, claudeCallback); status != http.StatusBadRequest ||
		body["error"] != "invalid_grant" {
		t.Errorf("another client = %d %v", status, body)
	}
	if _, status, body := r.exchange(t, code, p.verifier, oauth.ClaudeClientID, "https://claude.com/api/mcp/auth_callback"); status != http.StatusBadRequest ||
		body["error"] != "invalid_grant" {
		t.Errorf("another redirect = %d %v", status, body)
	}
	// None of that spent it.
	if _, status, body := r.exchange(t, code, p.verifier, oauth.ClaudeClientID, claudeCallback); status != http.StatusOK {
		t.Errorf("the right verifier = %d %v", status, body)
	}
}

func TestACodeWorksOnceAndForAMinute(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	p := newPKCE(t)
	code := r.code(t, r.owner, p, true)

	first, status, _ := r.exchange(t, code, p.verifier, oauth.ClaudeClientID, claudeCallback)
	if status != http.StatusOK {
		t.Fatalf("first exchange = %d", status)
	}
	_, status, body := r.exchange(t, code, p.verifier, oauth.ClaudeClientID, claudeCallback)
	if status != http.StatusBadRequest || body["error"] != "invalid_grant" {
		t.Errorf("second exchange = %d %v", status, body)
	}
	// A code used twice leaked: what its first use issued is revoked.
	if _, err := r.svc.Verify(ctx, first.AccessToken, nil); !errors.Is(err, mcpauth.ErrInvalidToken) {
		t.Errorf("the first exchange's token still works: %v", err)
	}
	if _, status, _ := r.refresh(t, first.RefreshToken); status != http.StatusBadRequest {
		t.Errorf("the first exchange's refresh still works: %d", status)
	}

	p = newPKCE(t)
	code = r.code(t, r.owner, p, true)
	r.now = r.now.Add(60 * time.Second)
	if _, status, body := r.exchange(t, code, p.verifier, oauth.ClaudeClientID, claudeCallback); status != http.StatusBadRequest ||
		body["error"] != "invalid_grant" {
		t.Errorf("a code a minute old = %d %v", status, body)
	}
}

func TestAReplayedCodeRevokesTheConnectionOnlyIfItsClientProvesIt(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	p := newPKCE(t)
	code := r.code(t, r.owner, p, true)
	first, status, body := r.exchange(t, code, p.verifier, oauth.ClaudeClientID, claudeCallback)
	if status != http.StatusOK {
		t.Fatalf("first exchange = %d %v", status, body)
	}

	// The code alone, or with anything but its own client's proof, is
	// refused and changes nothing: whoever read it in a log cannot
	// disconnect the owner.
	for _, tt := range []struct{ name, verifier, clientID, redirectURI string }{
		{"no verifier", "", oauth.ClaudeClientID, claudeCallback},
		{"another verifier", newPKCE(t).verifier, oauth.ClaudeClientID, claudeCallback},
		{"another client", p.verifier, oauth.ClaudeCodeClientID, claudeCallback},
		{"another redirect", p.verifier, oauth.ClaudeClientID, "https://claude.com/api/mcp/auth_callback"},
	} {
		if _, status, body := r.exchange(t, code, tt.verifier, tt.clientID, tt.redirectURI); status != http.StatusBadRequest ||
			body["error"] != "invalid_grant" {
			t.Errorf("%s = %d %v", tt.name, status, body)
		}
		if _, err := r.svc.Verify(ctx, first.AccessToken, nil); err != nil {
			t.Fatalf("after a replay with %s, the connection is gone: %v", tt.name, err)
		}
	}

	// With all three, the code leaked from the client itself: what it issued
	// is revoked.
	if _, status, body := r.exchange(t, code, p.verifier, oauth.ClaudeClientID, claudeCallback); status != http.StatusBadRequest ||
		body["error"] != "invalid_grant" {
		t.Errorf("the client's own replay = %d %v", status, body)
	}
	if _, err := r.svc.Verify(ctx, first.AccessToken, nil); !errors.Is(err, mcpauth.ErrInvalidToken) {
		t.Errorf("after the client's own replay, Verify = %v", err)
	}
	if grants := r.grants(t, r.owner); len(grants) != 0 {
		t.Errorf("grants = %+v", grants)
	}
}

func TestTheTokenEndpointHoldsTheResourceToTheCode(t *testing.T) {
	r := newRig(t, true)
	p := newPKCE(t)
	code := r.code(t, r.owner, p, false)
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {p.verifier},
		"client_id": {oauth.ClaudeClientID}, "redirect_uri": {claudeCallback}}

	form.Set("resource", "https://other.example.com/api/mcp")
	if status, body := r.token(t, form); status != http.StatusBadRequest || body["error"] != "invalid_target" {
		t.Errorf("another resource = %d %v", status, body)
	}
	form.Set("resource", "https://CHENCHEN.example.com/api/mcp/")
	if status, body := r.token(t, form); status != http.StatusOK {
		t.Errorf("the resource, spelled differently = %d %v", status, body)
	}
}

func TestRefreshRotatesThePair(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	first := r.connect(t, r.owner, true)

	r.now = r.now.Add(30 * time.Minute)
	second, status, body := r.refresh(t, first.RefreshToken)
	if status != http.StatusOK || second.AccessToken == first.AccessToken || second.RefreshToken == first.RefreshToken ||
		second.Scope != "read write" || second.ExpiresIn != 3600 {
		t.Fatalf("refresh = %d %v", status, body)
	}
	// The new pair works; the old access token lives out its hour.
	for _, token := range []string{second.AccessToken, first.AccessToken} {
		if _, err := r.svc.Verify(ctx, token, nil); err != nil {
			t.Errorf("Verify = %v", err)
		}
	}
	r.now = r.now.Add(31 * time.Minute)
	if _, err := r.svc.Verify(ctx, first.AccessToken, nil); !errors.Is(err, mcpauth.ErrInvalidToken) {
		t.Errorf("an hour-old access token = %v", err)
	}

	// Sixty days from the last refresh, not from the first.
	r.now = r.now.Add(59 * 24 * time.Hour)
	third, status, body := r.refresh(t, second.RefreshToken)
	if status != http.StatusOK {
		t.Fatalf("refresh 59 days on = %d %v", status, body)
	}
	r.now = r.now.Add(60*24*time.Hour - time.Second)
	if _, status, body := r.refresh(t, third.RefreshToken); status != http.StatusOK {
		t.Errorf("refresh just inside sixty days = %d %v", status, body)
	}

	// A refresh token is no access token, and an access token no refresh.
	if _, err := r.svc.Verify(ctx, third.RefreshToken, nil); !errors.Is(err, mcpauth.ErrInvalidToken) {
		t.Errorf("Verify(refresh) = %v", err)
	}
	if _, status, body := r.refresh(t, third.AccessToken); status != http.StatusBadRequest || body["error"] != "invalid_grant" {
		t.Errorf("refresh with an access token = %d %v", status, body)
	}
}

func TestARefreshAskingForLessGetsATokenWithExactlyThat(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	first := r.connect(t, r.owner, true)
	form := func(refreshToken, scope string) url.Values {
		f := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}}
		if scope != "" {
			f.Set("scope", scope)
		}
		return f
	}

	narrow, status, body := r.tokenPair(t, form(first.RefreshToken, "read"))
	if status != http.StatusOK || narrow.Scope != "read" {
		t.Fatalf("scope=read = %d %v", status, body)
	}
	if info, err := r.svc.Verify(ctx, narrow.AccessToken, nil); err != nil || !slices.Equal(info.Scopes, []string{"read"}) {
		t.Errorf("Verify(narrow) = %+v, %v", info, err)
	}
	// The same narrower refresh, repeated, gets the same pair.
	if again, status, body := r.tokenPair(t, form(first.RefreshToken, "read offline_access")); status != http.StatusOK ||
		again.AccessToken != narrow.AccessToken || again.RefreshToken != narrow.RefreshToken {
		t.Errorf("repeated = %d %v", status, body)
	}
	// Asking for something else with a token just used is refused, and
	// revokes nothing: one access token cannot carry two answers.
	if _, status, body := r.tokenPair(t, form(first.RefreshToken, "")); status != http.StatusBadRequest ||
		body["error"] != "invalid_grant" {
		t.Errorf("the same token for all scopes = %d %v", status, body)
	}

	// The refresh token kept everything the owner granted: asking for less
	// once did not narrow the connection.
	full, status, body := r.tokenPair(t, form(narrow.RefreshToken, ""))
	if status != http.StatusOK || full.Scope != "read write" {
		t.Fatalf("refresh of the narrow pair = %d %v", status, body)
	}
	if info, err := r.svc.Verify(ctx, full.AccessToken, nil); err != nil || !slices.Equal(info.Scopes, []string{"read", "write"}) {
		t.Errorf("Verify(full) = %+v, %v", info, err)
	}
	if grants := r.grants(t, r.owner); len(grants) != 1 || !slices.Equal(grants[0].Scopes, []string{"read", "write"}) {
		t.Errorf("grants = %+v", grants)
	}
}

// A client that repeats on refresh the scope it asked for when it connected
// is asking for its connection, not for less: it keeps write the owner gave
// though it asked only to read, and is not refused when the owner gave less
// than it asked for.
func TestARefreshRepeatingWhatTheClientFirstAskedForGetsTheGrant(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	connect := func(asked string, write bool) tokenResponse {
		t.Helper()
		p := newPKCE(t)
		q := r.params(p.challenge)
		q.Set("scope", asked)
		id := r.requestID(t, r.authorize(t, q))
		r.view(t, r.owner, id)
		code := codeFrom(t, r.approve(t, r.owner, id, write), claudeCallback)
		tokens, status, body := r.exchange(t, code, p.verifier, oauth.ClaudeClientID, claudeCallback)
		if status != http.StatusOK {
			t.Fatalf("exchange = %d %v", status, body)
		}
		return tokens
	}
	refresh := func(token, scope string) (tokenResponse, int, map[string]any) {
		t.Helper()
		return r.tokenPair(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {token}, "scope": {scope}})
	}

	// Asked to read; the owner also let it write.
	more := connect("read offline_access", true)
	got, status, body := refresh(more.RefreshToken, "read offline_access")
	if status != http.StatusOK || got.Scope != "read write" {
		t.Fatalf("refresh repeating 'read' = %d %v, want the grant's read write", status, body)
	}
	if info, err := r.svc.Verify(ctx, got.AccessToken, nil); err != nil || !slices.Equal(info.Scopes, []string{"read", "write"}) {
		t.Errorf("Verify = %+v, %v", info, err)
	}

	// Asked to read and write; the owner let it only read.
	less := connect("read write offline_access", false)
	got, status, body = refresh(less.RefreshToken, "read write offline_access")
	if status != http.StatusOK || got.Scope != "read" {
		t.Fatalf("refresh repeating 'read write' = %d %v, want the grant's read", status, body)
	}

	// Asked only to read and given only that: asking now for write as well is
	// asking for more than was granted, and is refused.
	read := connect("read offline_access", false)
	if _, status, body := refresh(read.RefreshToken, "read write"); status != http.StatusBadRequest || body["error"] != "invalid_scope" {
		t.Errorf("refresh asking for more = %d %v", status, body)
	}
}

func TestARefreshTokenExpiresSixtyDaysAfterItWasIssued(t *testing.T) {
	r := newRig(t, true)
	first := r.connect(t, r.owner, false)
	r.now = r.now.Add(60 * 24 * time.Hour)
	if _, status, body := r.refresh(t, first.RefreshToken); status != http.StatusBadRequest || body["error"] != "invalid_grant" {
		t.Errorf("refresh at sixty days = %d %v", status, body)
	}
}

func TestTheSameRefreshWithinAMinuteGetsTheSamePair(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	first := r.connect(t, r.owner, true)

	// claude.ai sends them together…
	const n = 8
	var (
		wg      sync.WaitGroup
		results [n]tokenResponse
		codes   [n]int
	)
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {first.RefreshToken}}.Encode()
	for i := range n {
		// Straight to the router: a goroutine must not fail the test itself.
		wg.Go(func() {
			rec := r.serve(t, "", http.MethodPost, "/api/oauth/token", "application/x-www-form-urlencoded", form)
			codes[i] = rec.Code
			_ = json.Unmarshal(rec.Body.Bytes(), &results[i])
		})
	}
	wg.Wait()
	for i := range n {
		if codes[i] != http.StatusOK || results[i].AccessToken != results[0].AccessToken ||
			results[i].RefreshToken != results[0].RefreshToken {
			t.Fatalf("refresh %d = %d %+v, want the same pair as the first %+v", i, codes[i], results[i], results[0])
		}
	}
	// …or a moment apart.
	r.now = r.now.Add(59 * time.Second)
	again, status, body := r.refresh(t, first.RefreshToken)
	if status != http.StatusOK || again.AccessToken != results[0].AccessToken || again.RefreshToken != results[0].RefreshToken ||
		again.ExpiresIn != 3600-59 {
		t.Errorf("at 59 s = %d %v", status, body)
	}
	// One rotation, one new pair, and it works.
	if n := r.count(t, "oauth_tokens"); n != 4 {
		t.Errorf("%d tokens, want the first pair and one more", n)
	}
	if _, err := r.svc.Verify(ctx, results[0].AccessToken, nil); err != nil {
		t.Errorf("Verify = %v", err)
	}
	if next, status, body := r.refresh(t, results[0].RefreshToken); status != http.StatusOK || next.RefreshToken == results[0].RefreshToken {
		t.Errorf("refresh of the new pair = %d %v", status, body)
	}
}

func TestARefreshWithAnotherClientsIDNeverFailsTheOneItArrivesWith(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	first := r.connect(t, r.owner, true)

	// The service reads its clock first thing in a rotation, so a clock that
	// waits holds a rotation open, letting a request that would share it
	// arrive before it ends.
	var (
		now     = r.now
		entered = make(chan struct{}, 1)
		release = make(chan struct{})
	)
	r.svc.SetNow(func() time.Time {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		return now
	})
	send := func(clientID string) <-chan *httptest.ResponseRecorder {
		done := make(chan *httptest.ResponseRecorder, 1)
		form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {first.RefreshToken}, "client_id": {clientID}}
		go func() {
			done <- r.serve(t, "", http.MethodPost, "/api/oauth/token", "application/x-www-form-urlencoded", form.Encode())
		}()
		return done
	}

	// The bad one first, so it would be the one leading a shared rotation.
	bad := send(oauth.ClaudeCodeClientID)
	var badRec *httptest.ResponseRecorder
	select {
	case badRec = <-bad: // refused on its own, as it should be
	case <-entered: // leading a rotation, with its bad client_id
	case <-time.After(5 * time.Second):
		t.Fatal("the first refresh neither answered nor rotated")
	}
	good := send(oauth.ClaudeClientID)
	select {
	case <-entered: // leading its own rotation
	case <-time.After(200 * time.Millisecond): // or waiting on the bad one's
	}
	close(release)
	if badRec == nil {
		badRec = <-bad
	}
	goodRec := <-good

	if badRec.Code != http.StatusBadRequest || !strings.Contains(badRec.Body.String(), `"error":"invalid_grant"`) {
		t.Errorf("the bad refresh = %d %s", badRec.Code, badRec.Body)
	}
	var pair tokenResponse
	if goodRec.Code != http.StatusOK || json.Unmarshal(goodRec.Body.Bytes(), &pair) != nil || pair.Scope != "read write" {
		t.Fatalf("the good refresh = %d %s", goodRec.Code, goodRec.Body)
	}
	if _, err := r.svc.Verify(ctx, pair.AccessToken, nil); err != nil {
		t.Errorf("Verify = %v", err)
	}
}

func TestARefreshTokenUsedAgainAfterTheGraceRevokesTheConnection(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	first := r.connect(t, r.owner, true)
	second, status, _ := r.refresh(t, first.RefreshToken)
	if status != http.StatusOK {
		t.Fatal(status)
	}

	r.now = r.now.Add(61 * time.Second)
	if _, status, body := r.refresh(t, first.RefreshToken); status != http.StatusBadRequest || body["error"] != "invalid_grant" {
		t.Errorf("the old token again = %d %v", status, body)
	}
	// Whoever held the new pair — the owner, or a thief — holds nothing now.
	if _, err := r.svc.Verify(ctx, second.AccessToken, nil); !errors.Is(err, mcpauth.ErrInvalidToken) {
		t.Errorf("the family's access token = %v", err)
	}
	if _, status, _ := r.refresh(t, second.RefreshToken); status != http.StatusBadRequest {
		t.Errorf("the family's refresh token = %d", status)
	}
	if grants := r.grants(t, r.owner); len(grants) != 0 {
		t.Errorf("grants = %+v", grants)
	}
}

func TestAConnectionIsRefusedEverywhereElse(t *testing.T) {
	r := newRig(t, true)
	tokens := r.connect(t, r.owner, true)
	for _, token := range []string{tokens.AccessToken, tokens.RefreshToken} {
		for _, path := range []string{"/api/accounts", "/api/me", "/api/oauth/grants", "/api/captures/review"} {
			if rec := r.serve(t, "Bearer "+token, http.MethodGet, path, "", ""); rec.Code != http.StatusUnauthorized {
				t.Errorf("GET %s with %s… = %d", path, token[:5], rec.Code)
			}
		}
		if rec := r.serve(t, "Bearer "+token, http.MethodPost, "/api/oauth/requests/"+uuid.NewString()+"/approve",
			"application/json", `{"write": true}`); rec.Code != http.StatusUnauthorized {
			t.Errorf("approve with %s… = %d", token[:5], rec.Code)
		}
	}
}

func TestTheVerifierTakesOnlyAnAccessToken(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	tokens := r.connect(t, r.owner, false)

	session, err := r.tokens.IssueAccessToken(r.owner, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	account, _, err := r.db.Q().EnsureAccount(ctx, store.Account{ID: uuid.New(), UserID: r.owner, Class: "asset",
		Type: "checking", Institution: "bac", ExternalNumber: "0000000001", Currency: "USD"})
	if err != nil {
		t.Fatal(err)
	}
	key, err := r.captures.CreateKey(ctx, r.owner, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	for name, token := range map[string]string{
		"session":     session,
		"capture key": key.Key,
		"refresh":     tokens.RefreshToken,
		"empty":       "",
		"made up":     "chno_" + strings.Repeat("A", 43),
	} {
		if info, err := r.svc.Verify(ctx, token, nil); !errors.Is(err, mcpauth.ErrInvalidToken) || info != nil {
			t.Errorf("Verify(%s) = %+v, %v", name, info, err)
		}
	}

	info, err := r.svc.Verify(ctx, tokens.AccessToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	grantID, ok := oauth.GrantID(info)
	grants := r.grants(t, r.owner)
	if info.UserID != r.owner.String() || !slices.Equal(info.Scopes, []string{"read"}) ||
		!info.Expiration.Equal(r.now.Add(time.Hour)) || !ok || len(grants) != 1 || grants[0].ID != grantID ||
		grants[0].LastUsedAt == nil || !grants[0].LastUsedAt.Equal(r.now) {
		t.Errorf("Verify = %+v; grants %+v", info, grants)
	}

	// Last use is written at most once a minute.
	r.now = r.now.Add(59 * time.Second)
	if _, err := r.svc.Verify(ctx, tokens.AccessToken, nil); err != nil {
		t.Fatal(err)
	}
	if got := r.grants(t, r.owner)[0].LastUsedAt; !got.Equal(r.now.Add(-59 * time.Second)) {
		t.Errorf("last used = %v, want unchanged", got)
	}
	r.now = r.now.Add(time.Second)
	if _, err := r.svc.Verify(ctx, tokens.AccessToken, nil); err != nil {
		t.Fatal(err)
	}
	if got := r.grants(t, r.owner)[0].LastUsedAt; !got.Equal(r.now) {
		t.Errorf("last used = %v, want %v", got, r.now)
	}
}

func TestDisconnectingEndsEveryToken(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	tokens := r.connect(t, r.owner, true)
	theirs := r.connect(t, r.stranger, false)

	grants := r.grants(t, r.owner)
	if len(grants) != 1 || grants[0].ClientName != "Claude" || !slices.Equal(grants[0].Scopes, []string{"read", "write"}) ||
		!grants[0].CreatedAt.Equal(r.now) {
		t.Fatalf("grants = %+v", grants)
	}
	id := grants[0].ID.String()

	// Nobody else can end it.
	if rec := r.serve(t, r.session(t, r.stranger), http.MethodDelete, "/api/oauth/grants/"+id, "", ""); rec.Code != http.StatusNoContent {
		t.Errorf("DELETE by someone else = %d", rec.Code)
	}
	if _, err := r.svc.Verify(ctx, tokens.AccessToken, nil); err != nil {
		t.Fatalf("someone else ended it: %v", err)
	}

	if rec := r.serve(t, r.session(t, r.owner), http.MethodDelete, "/api/oauth/grants/"+id, "", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d %s", rec.Code, rec.Body)
	}
	if _, err := r.svc.Verify(ctx, tokens.AccessToken, nil); !errors.Is(err, mcpauth.ErrInvalidToken) {
		t.Errorf("Verify after disconnecting = %v", err)
	}
	if _, status, _ := r.refresh(t, tokens.RefreshToken); status != http.StatusBadRequest {
		t.Errorf("refresh after disconnecting = %d", status)
	}
	if grants := r.grants(t, r.owner); len(grants) != 0 {
		t.Errorf("grants = %+v", grants)
	}
	// Theirs is untouched.
	if _, err := r.svc.Verify(ctx, theirs.AccessToken, nil); err != nil {
		t.Errorf("someone else's connection = %v", err)
	}
}

func TestTheTokenEndpointSpeaksOAuth(t *testing.T) {
	r := newRig(t, true)
	tokens := r.connect(t, r.owner, false)
	refresh := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tokens.RefreshToken}}

	for _, tt := range []struct {
		name, contentType, body, code string
		status                        int
	}{
		{"json", "application/json", `{"grant_type":"refresh_token"}`, "invalid_request", http.StatusBadRequest},
		{"no grant", "application/x-www-form-urlencoded", "", "invalid_request", http.StatusBadRequest},
		{"another grant", "application/x-www-form-urlencoded", "grant_type=password", "unsupported_grant_type", http.StatusBadRequest},
		{"repeated", "application/x-www-form-urlencoded", refresh.Encode() + "&grant_type=refresh_token", "invalid_request", http.StatusBadRequest},
		{"unknown client", "application/x-www-form-urlencoded", "grant_type=authorization_code&code=x&client_id=someone", "invalid_client", http.StatusUnauthorized},
		{"an unknown scope", "application/x-www-form-urlencoded", refresh.Encode() + "&scope=read+admin", "invalid_scope", http.StatusBadRequest},
		{"another client", "application/x-www-form-urlencoded", refresh.Encode() + "&client_id=" + url.QueryEscape(oauth.ClaudeCodeClientID), "invalid_grant", http.StatusBadRequest},
	} {
		rec := r.serve(t, "", http.MethodPost, "/api/oauth/token", tt.contentType, tt.body)
		var body map[string]any
		if rec.Code != tt.status || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body["error"] != tt.code ||
			rec.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			t.Errorf("%s = %d %s", tt.name, rec.Code, rec.Body)
		}
	}
	// None of those spent the refresh token.
	if _, status, body := r.refresh(t, tokens.RefreshToken); status != http.StatusOK {
		t.Errorf("refresh = %d %v", status, body)
	}
	// Nothing is read from the query string.
	rec := r.serve(t, "", http.MethodPost, "/api/oauth/token?"+refresh.Encode(), "application/x-www-form-urlencoded", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("parameters in the URL = %d %s", rec.Code, rec.Body)
	}
}

func TestTheTokenEndpointIsRateLimitedPerAddress(t *testing.T) {
	r := newRigWith(t, true, 2)
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"chnr_x"}}
	for i := range 2 {
		if status, _ := r.token(t, form); status != http.StatusBadRequest {
			t.Fatalf("request %d = %d", i, status)
		}
	}
	rec := r.serve(t, "", http.MethodPost, "/api/oauth/token", "application/x-www-form-urlencoded", form.Encode())
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" ||
		!strings.Contains(rec.Body.String(), `"error":"temporarily_unavailable"`) {
		t.Errorf("third = %d %s", rec.Code, rec.Body)
	}
	// Authorize counts on its own.
	if id := r.requestID(t, r.authorize(t, r.params(newPKCE(t).challenge))); id == "" {
		t.Error("authorize sent the browser to the consent page without a request")
	}
}

func TestPruningKeepsOnlyWhatCanStillWork(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	live := r.connect(t, r.owner, false)
	dead := r.connect(t, r.stranger, false)
	r.authorize(t, r.params(newPKCE(t).challenge))
	if err := r.svc.Revoke(ctx, r.stranger, r.grants(t, r.stranger)[0].ID); err != nil {
		t.Fatal(err)
	}

	r.now = r.now.Add(2 * time.Hour)
	if _, err := r.svc.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	for table, want := range map[string]int{"oauth_requests": 0, "oauth_codes": 0, "oauth_grants": 1, "oauth_tokens": 1} {
		if n := r.count(t, table); n != want {
			t.Errorf("%s has %d rows, want %d", table, n, want)
		}
	}
	if _, status, _ := r.refresh(t, live.RefreshToken); status != http.StatusOK {
		t.Errorf("the live connection's refresh = %d", status)
	}
	if _, status, _ := r.refresh(t, dead.RefreshToken); status != http.StatusBadRequest {
		t.Errorf("the revoked connection's refresh = %d", status)
	}

	// Deleting a user takes every trace of their connections with them.
	if err := storetest.Exec(t, r.db, `DELETE FROM users WHERE id = $1`, r.owner); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"oauth_grants", "oauth_tokens"} {
		if n := r.count(t, table); n != 0 {
			t.Errorf("%s has %d rows", table, n)
		}
	}
}

// --- rig ----------------------------------------------------------------------------

type rig struct {
	db       *store.DB
	router   http.Handler
	svc      *oauth.Service
	captures *captures.Service
	tokens   *auth.TokenIssuer
	owner    uuid.UUID
	stranger uuid.UUID
	now      time.Time
	mu       sync.Mutex
}

func newRig(t *testing.T, enabled bool) *rig { return newRigWith(t, enabled, 1000) }

// newRigWith builds the whole API, as main does, with Claude's sign-in on or
// off and the given limit per address on authorize and token.
func newRigWith(t *testing.T, enabled bool, oauthLimit int) *rig {
	t.Helper()
	db := storetest.New(t)
	cats := categories.NewService(db)
	r := &rig{db: db, now: time.Now().Truncate(time.Microsecond)}

	cfg := config.Config{Env: "development", Auth: config.AuthConfig{
		JWTSecret: []byte(strings.Repeat("s", 32)), AccessTTL: 15 * time.Minute, RefreshTTL: time.Hour,
		BcryptCost: 10, LoginRateLimit: 10, PinLoginRateLimit: 10, DeviceTTL: time.Hour,
		CaptureIPRateLimit: 60, CaptureKeyRateLimit: 12,
	}}
	if enabled {
		cfg.OAuth = config.OAuthConfig{PublicURL: publicURL, RateLimit: oauthLimit}
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	accountsSvc := accounts.NewService(db)
	authSvc, err := auth.NewService(db, accountsSvc, cats, cfg.Auth)
	if err != nil {
		t.Fatal(err)
	}
	movementsSvc := movements.NewService(db, cats)
	reportsSvc := reports.NewService(db)
	transfersSvc := transfers.NewService(db)
	rulesSvc := rules.NewService(db, cats)
	investmentsSvc := investments.NewService(db, nil)
	proposalsSvc := proposals.NewService(db, accountsSvc, cats, movementsSvc, rulesSvc, transfersSvc)
	chatSvc := chat.NewService(db, mcpserver.Deps{Accounts: accountsSvc, Movements: movementsSvc, Categories: cats,
		Reports: reportsSvc, Transfers: transfersSvc, Investments: investmentsSvc, Rules: rulesSvc,
		Proposals: proposalsSvc}, chat.NewUnavailable(), 1)
	r.captures = captures.NewService(db, cats)
	limiter := func(n int) *httpx.RateLimiter { return httpx.NewRateLimiter(n) }

	var oauthHandler *oauth.Handler
	if enabled {
		r.svc = oauth.NewService(db, cfg.OAuth)
		r.svc.SetNow(func() time.Time {
			r.mu.Lock()
			defer r.mu.Unlock()
			return r.now
		})
		oauthHandler = oauth.NewHandler(r.svc, authSvc.Tokens(), limiter(cfg.OAuth.RateLimit))
	}

	r.router = server.NewRouter(server.Deps{
		Config: cfg, Logger: logger, Health: map[string]httpx.Checker{"postgres": db},
		Auth:        auth.NewHandler(authSvc, false, limiter(10), limiter(10)),
		Tokens:      authSvc.Tokens(),
		Accounts:    accounts.NewHandler(accountsSvc),
		Movements:   movements.NewHandler(movementsSvc),
		Categories:  categories.NewHandler(cats),
		Investments: investments.NewHandler(investmentsSvc),
		Imports:     imports.NewHandler(imports.NewService(db, cats), db),
		Reports:     reports.NewHandler(reportsSvc),
		Transfers:   transfers.NewHandler(transfersSvc),
		Rules:       rules.NewHandler(rulesSvc),
		Proposals:   proposals.NewHandler(proposalsSvc),
		Chat:        chat.NewHandler(chatSvc, authSvc, limiter(6)),
		Captures:    captures.NewHandler(r.captures),
		Shortcut:    captures.NewShortcutHandler(r.captures, auth.NewCaptureKeys(db), limiter(60), limiter(12)),
		OAuth:       oauthHandler,
	})
	r.tokens = authSvc.Tokens()
	r.owner = r.newUser(t, "owner@example.com", cats)
	r.stranger = r.newUser(t, "stranger@example.com", cats)
	return r
}

func (r *rig) newUser(t *testing.T, email string, cats *categories.Service) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	u, err := r.db.Q().CreateUser(ctx, store.User{ID: uuid.New(), Email: email, PasswordHash: "x", FullName: email})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.db.InTx(ctx, func(q *store.Queries) error { return cats.SeedDefaults(ctx, q, u.ID) }); err != nil {
		t.Fatal(err)
	}
	return u.ID
}

// serve sends any request to the API.
func (r *rig) serve(t *testing.T, authorization, method, target, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	r.router.ServeHTTP(rec, req)
	return rec
}

// session is an Authorization header for user's web session.
func (r *rig) session(t *testing.T, user uuid.UUID) string {
	t.Helper()
	token, err := r.tokens.IssueAccessToken(user, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return "Bearer " + token
}

type pkce struct{ verifier, challenge string }

func newPKCE(t *testing.T) pkce {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	return pkce{verifier: verifier, challenge: base64.RawURLEncoding.EncodeToString(sum[:])}
}

// params is what claude.ai sends to /authorize.
func (r *rig) params(challenge string) url.Values {
	return url.Values{
		"response_type":         {"code"},
		"client_id":             {oauth.ClaudeClientID},
		"redirect_uri":          {claudeCallback},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
		"scope":                 {"read write offline_access"},
		"resource":              {resource},
	}
}

func (r *rig) authorize(t *testing.T, q url.Values) *httptest.ResponseRecorder {
	t.Helper()
	return r.serve(t, "", http.MethodGet, "/api/oauth/authorize?"+q.Encode(), "", "")
}

// requestID reads the request /authorize sent the browser to the consent page
// with.
func (r *rig) requestID(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	loc, err := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusFound || err != nil || loc.Scheme+"://"+loc.Host+loc.Path != publicURL+"/conectar" {
		t.Fatalf("authorize = %d, Location %q, %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	return loc.Query().Get("req")
}

type requestView struct {
	ClientName   string    `json:"client_name"`
	RedirectHost string    `json:"redirect_host"`
	Scopes       []string  `json:"scopes"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func (r *rig) view(t *testing.T, user uuid.UUID, id string) requestView {
	t.Helper()
	rec := r.serve(t, r.session(t, user), http.MethodGet, "/api/oauth/requests/"+id, "", "")
	var v requestView
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &v) != nil {
		t.Fatalf("GET request = %d %s", rec.Code, rec.Body)
	}
	return v
}

// approve answers yes as user and returns where the browser goes next.
func (r *rig) approve(t *testing.T, user uuid.UUID, id string, write bool) string {
	t.Helper()
	body := `{"write": false}`
	if write {
		body = `{"write": true}`
	}
	rec := r.serve(t, r.session(t, user), http.MethodPost, "/api/oauth/requests/"+id+"/approve", "application/json", body)
	var answer struct {
		RedirectTo string `json:"redirect_to"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &answer) != nil {
		t.Fatalf("approve = %d %s", rec.Code, rec.Body)
	}
	return answer.RedirectTo
}

// codeFrom reads the code from a redirect back to callback.
func codeFrom(t *testing.T, redirect, callback string) string {
	t.Helper()
	if !strings.HasPrefix(redirect, callback+"?") {
		t.Fatalf("redirect_to = %s, want %s", redirect, callback)
	}
	loc, _ := url.Parse(redirect)
	return loc.Query().Get("code")
}

// code runs claude.ai's sign-in as user up to the code.
func (r *rig) code(t *testing.T, user uuid.UUID, p pkce, write bool) string {
	t.Helper()
	id := r.requestID(t, r.authorize(t, r.params(p.challenge)))
	r.view(t, user, id)
	return codeFrom(t, r.approve(t, user, id, write), claudeCallback)
}

// connect runs claude.ai's whole sign-in as user.
func (r *rig) connect(t *testing.T, user uuid.UUID, write bool) tokenResponse {
	t.Helper()
	p := newPKCE(t)
	tokens, status, body := r.exchange(t, r.code(t, user, p, write), p.verifier, oauth.ClaudeClientID, claudeCallback)
	if status != http.StatusOK {
		t.Fatalf("exchange = %d %v", status, body)
	}
	return tokens
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

func (r *rig) token(t *testing.T, form url.Values) (int, map[string]any) {
	t.Helper()
	rec := r.serve(t, "", http.MethodPost, "/api/oauth/token", "application/x-www-form-urlencoded", form.Encode())
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("token = %d %s", rec.Code, rec.Body)
	}
	return rec.Code, body
}

func (r *rig) exchange(t *testing.T, code, verifier, clientID, redirectURI string) (tokenResponse, int, map[string]any) {
	t.Helper()
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID},
		"redirect_uri": {redirectURI}, "resource": {resource}}
	if verifier != "" {
		form.Set("code_verifier", verifier)
	}
	return r.tokenPair(t, form)
}

func (r *rig) refresh(t *testing.T, refreshToken string) (tokenResponse, int, map[string]any) {
	t.Helper()
	return r.tokenPair(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken},
		"client_id": {oauth.ClaudeClientID}, "resource": {resource}})
}

func (r *rig) tokenPair(t *testing.T, form url.Values) (tokenResponse, int, map[string]any) {
	t.Helper()
	status, body := r.token(t, form)
	var pair tokenResponse
	raw, _ := json.Marshal(body)
	_ = json.Unmarshal(raw, &pair)
	return pair, status, body
}

type grantView struct {
	ID         uuid.UUID  `json:"id"`
	ClientName string     `json:"client_name"`
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

func (r *rig) grants(t *testing.T, user uuid.UUID) []grantView {
	t.Helper()
	rec := r.serve(t, r.session(t, user), http.MethodGet, "/api/oauth/grants", "", "")
	var body struct {
		Grants []grantView `json:"grants"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Grants == nil {
		t.Fatalf("GET grants = %d %s", rec.Code, rec.Body)
	}
	return body.Grants
}

// count is the number of rows in a table, or in a table with a condition.
func (r *rig) count(t *testing.T, from string) int {
	t.Helper()
	return storetest.Count(t, r.db, from)
}

// sameJSON reports whether a decoded JSON value is want.
func sameJSON(got, want any) bool {
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(want)
	return string(a) == string(b)
}
