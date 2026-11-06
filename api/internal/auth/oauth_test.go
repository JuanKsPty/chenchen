package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestClaudesTokensAreNoOtherToken(t *testing.T) {
	access, accessHash, err := NewOAuthAccessToken()
	if err != nil {
		t.Fatal(err)
	}
	refresh, refreshHash, err := NewOAuthRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	if !LooksLikeOAuthAccessToken(access) || LooksLikeOAuthAccessToken(refresh) ||
		!LooksLikeOAuthRefreshToken(refresh) || LooksLikeOAuthRefreshToken(access) {
		t.Errorf("access %q and refresh %q are not told apart", access, refresh)
	}
	if accessHash != HashOAuthToken(access) || refreshHash != HashOAuthToken(refresh) ||
		strings.Contains(accessHash, strings.TrimPrefix(access, OAuthAccessTokenPrefix)) {
		t.Error("a token's hash is not the hash of the whole token")
	}

	key, _, err := NewCaptureKey()
	if err != nil {
		t.Fatal(err)
	}
	jwt, err := newTestIssuer(t).IssueAccessToken(uuid.New(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	code, _, err := NewOAuthCode()
	if err != nil {
		t.Fatal(err)
	}
	for _, other := range []string{key, jwt, code, access[:len(access)-1], access + "A", " " + access,
		OAuthAccessTokenPrefix + strings.TrimPrefix(key, CaptureKeyPrefix)[:42] + "="} {
		if LooksLikeOAuthAccessToken(other) {
			t.Errorf("%q passes for an access token", other)
		}
	}
	if looksLikeCaptureKey(access) || looksLikeCaptureKey(refresh) {
		t.Error("one of Claude's tokens passes for a capture key")
	}
}

func TestRequireNeverTakesOneOfClaudesTokens(t *testing.T) {
	issuer := newTestIssuer(t)
	reached := false
	h := Require(issuer)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))

	access, _, _ := NewOAuthAccessToken()
	refresh, _, _ := NewOAuthRefreshToken()
	for _, token := range []string{access, refresh} {
		req := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized || reached {
			t.Errorf("%s… = %d, reached %v", token[:5], rec.Code, reached)
		}
	}

	// A session still works, so the refusal above is about the token.
	jwt, err := issuer.IssueAccessToken(uuid.New(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
	req.Header.Set("Authorization", "Bearer "+jwt)
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !reached {
		t.Error("a session was refused")
	}
}
