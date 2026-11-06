package oauth

import (
	"slices"
	"strings"
	"testing"
)

func TestClaudeCodeMayComeBackOnAnyLoopbackPort(t *testing.T) {
	for _, uri := range []string{
		"http://localhost/callback",
		"http://localhost:1/callback",
		"http://localhost:53682/callback",
		"http://127.0.0.1:65535/callback",
		"http://127.0.0.1/callback",
	} {
		if !loopbackCallback(uri) {
			t.Errorf("%s refused", uri)
		}
	}
	for _, uri := range []string{
		"",
		"https://localhost:53682/callback",
		"http://localhost:53682/callback/",
		"http://localhost:53682/Callback",
		"http://localhost:53682/callback?x=1",
		"http://localhost:53682/callback#x",
		"http://localhost:53682/callback#",
		"http://localhost:/callback",
		"http://localhost:99999/callback",
		"http://LOCALHOST:53682/callback",
		"http://localhost.example.com/callback",
		"http://127.0.0.2/callback",
		"http://[::1]:53682/callback",
		"http://user@localhost:53682/callback",
		"http://localhost:53682/%63allback",
		"http://localhost:53682/x/../callback",
		"https://claude.ai/api/mcp/auth_callback",
	} {
		if loopbackCallback(uri) {
			t.Errorf("%s taken for Claude Code's callback", uri)
		}
	}
}

func TestClaudeComesBackOnlyToItsOwnCallback(t *testing.T) {
	for _, uri := range []string{"https://claude.ai/api/mcp/auth_callback", "https://claude.com/api/mcp/auth_callback"} {
		if !claudeCallback(uri) {
			t.Errorf("%s refused", uri)
		}
	}
	for _, uri := range []string{
		"https://claude.ai/api/mcp/auth_callback/",
		"https://claude.ai/api/mcp/auth_callback?x=1",
		"https://CLAUDE.ai/api/mcp/auth_callback",
		"http://claude.ai/api/mcp/auth_callback",
		"https://claude.ai.example.com/api/mcp/auth_callback",
		"https://claude.ai:443/api/mcp/auth_callback",
		"http://localhost:53682/callback",
	} {
		if claudeCallback(uri) {
			t.Errorf("%s taken for claude.ai's callback", uri)
		}
	}

	// Each client comes back only to its own.
	web, _ := lookupClient(ClaudeClientID)
	code, _ := lookupClient(ClaudeCodeClientID)
	if web.redirects("http://localhost:53682/callback") || code.redirects("https://claude.ai/api/mcp/auth_callback") {
		t.Error("a client may be sent back to the other one's callback")
	}
	if _, ok := lookupClient("https://claude.ai/oauth/another-client"); ok {
		t.Error("an unlisted client is known")
	}
}

func TestReadIsAlwaysAskedForAndNothingUnknownIs(t *testing.T) {
	for raw, want := range map[string][]string{
		"":                           {ScopeRead},
		"read":                       {ScopeRead},
		"offline_access":             {ScopeRead},
		"write":                      {ScopeRead, ScopeWrite},
		"read  write offline_access": {ScopeRead, ScopeWrite},
		"write read write":           {ScopeRead, ScopeWrite},
	} {
		if got, ok := parseScope(raw); !ok || !slices.Equal(got, want) {
			t.Errorf("parseScope(%q) = %v, %v; want %v", raw, got, ok, want)
		}
	}
	for _, raw := range []string{"admin", "read admin", "READ", "openid"} {
		if got, ok := parseScope(raw); ok {
			t.Errorf("parseScope(%q) = %v, want refused", raw, got)
		}
	}
}

func TestPKCEMatchesOnlyAnS256Verifier(t *testing.T) {
	// RFC 7636, appendix B.
	const (
		verifier  = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
		challenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	)
	if !pkceMatches(verifier, challenge) {
		t.Fatal("the RFC's own example does not match")
	}
	for _, v := range []string{
		"",
		challenge, // "plain": the challenge sent back as the verifier
		verifier[:42],
		verifier + "x",
		strings.Repeat("a", 129),
		verifier[:42] + "+",
	} {
		if pkceMatches(v, challenge) {
			t.Errorf("verifier %q matches", v)
		}
	}
}
