package config

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	// The promise this test pins down: a fresh clone with no environment at all
	// still produces a bootable configuration.
	clearEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load with an empty environment: %v", err)
	}

	if cfg.HTTP.Addr != ":8080" {
		t.Errorf("HTTP.Addr = %q, want :8080", cfg.HTTP.Addr)
	}
	if cfg.Auth.AccessTTL != 15*time.Minute {
		t.Errorf("Auth.AccessTTL = %v, want 15m", cfg.Auth.AccessTTL)
	}
	if cfg.Env != "development" || cfg.Log.Format != "text" {
		t.Errorf("env %q with log format %q, want development/text", cfg.Env, cfg.Log.Format)
	}
	if cfg.AI.Enabled() {
		t.Error("AI reports itself enabled without an API key")
	}
	if cfg.Auth.CaptureIPRateLimit != 60 || cfg.Auth.CaptureKeyRateLimit != 12 {
		t.Errorf("capture limits = %d per IP, %d per key; want 60 and 12",
			cfg.Auth.CaptureIPRateLimit, cfg.Auth.CaptureKeyRateLimit)
	}
}

func TestCaptureRateLimitsFromEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv("CAPTURE_IP_RATE_LIMIT", "30")
	t.Setenv("CAPTURE_KEY_RATE_LIMIT", "6")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Auth.CaptureIPRateLimit != 30 || cfg.Auth.CaptureKeyRateLimit != 6 {
		t.Errorf("capture limits = %d per IP, %d per key; want 30 and 6",
			cfg.Auth.CaptureIPRateLimit, cfg.Auth.CaptureKeyRateLimit)
	}
}

func TestJWTSecretIsGeneratedWhenUnset(t *testing.T) {
	// Booting without a secret must work, and the generated one must be random —
	// a fixed fallback compiled into a public binary would be worse than none.
	clearEnv(t)

	first, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	second, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !first.Auth.JWTSecretRandom {
		t.Error("JWTSecretRandom is false, so the caller cannot warn about it")
	}
	if len(first.Auth.JWTSecret) < 32 {
		t.Errorf("generated secret is %d bytes, want at least 32", len(first.Auth.JWTSecret))
	}
	if string(first.Auth.JWTSecret) == string(second.Auth.JWTSecret) {
		t.Error("two loads produced the same secret, so it is not random")
	}
}

func TestJWTSecretFromEnv(t *testing.T) {
	clearEnv(t)
	secret := strings.Repeat("k", 32)
	t.Setenv("JWT_SECRET", secret)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(cfg.Auth.JWTSecret) != secret {
		t.Error("the configured secret was not used")
	}
	if cfg.Auth.JWTSecretRandom {
		t.Error("JWTSecretRandom is true even though a secret was configured")
	}
}

func TestIBKRDisabledWhenUnset(t *testing.T) {
	clearEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.IBKR.Enabled() {
		t.Error("IBKR reports itself enabled without an encryption key")
	}
}

func TestIBKRKeyFromEnv(t *testing.T) {
	clearEnv(t)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	t.Setenv("IBKR_TOKEN_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.IBKR.Enabled() {
		t.Error("IBKR reports itself disabled despite a configured key")
	}
	if string(cfg.IBKR.TokenEncryptionKey) != string(key) {
		t.Error("the decoded key does not match what was configured")
	}
}

func TestClaudeCannotConnectWithoutAPublicURL(t *testing.T) {
	clearEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.OAuth.Enabled() || cfg.OAuth.RateLimit != 30 {
		t.Errorf("OAuth = %+v, want off with a limit of 30", cfg.OAuth)
	}
}

func TestThePublicURLNamesTheIssuerAndTheResource(t *testing.T) {
	for _, raw := range []string{
		"https://chenchen.example.com",
		"https://chenchen.example.com/",
		"HTTPS://Chenchen.Example.COM:443/",
	} {
		t.Run(raw, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("PUBLIC_URL", raw)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if !cfg.OAuth.Enabled() || cfg.OAuth.Issuer() != "https://chenchen.example.com" ||
				cfg.OAuth.Resource() != "https://chenchen.example.com/api/mcp" {
				t.Errorf("issuer %q, resource %q", cfg.OAuth.Issuer(), cfg.OAuth.Resource())
			}
		})
	}

	// Plain http only on the developer's own machine.
	clearEnv(t)
	t.Setenv("PUBLIC_URL", "http://localhost:8080")
	if cfg, err := Load(); err != nil || cfg.OAuth.Resource() != "http://localhost:8080/api/mcp" {
		t.Errorf("Load = %q, %v", cfg.OAuth.Resource(), err)
	}
}

func TestCanonicalURLFoldsWhatDoesNotChangeTheAddress(t *testing.T) {
	for raw, want := range map[string]string{
		"https://chenchen.example.com/api/mcp":       "https://chenchen.example.com/api/mcp",
		"https://chenchen.example.com/api/mcp/":      "https://chenchen.example.com/api/mcp",
		"HTTPS://CHENCHEN.example.com:443/api/mcp":   "https://chenchen.example.com/api/mcp",
		"http://LOCALHOST:80/api/mcp":                "http://localhost/api/mcp",
		"http://127.0.0.1:8080/api/mcp":              "http://127.0.0.1:8080/api/mcp",
		"https://chenchen.example.com:8443/api/mcp/": "https://chenchen.example.com:8443/api/mcp",
		"http://[::1]/api/mcp":                       "http://[::1]/api/mcp",
		// The path is case-sensitive, so it stays as it came.
		"https://chenchen.example.com/API/mcp": "https://chenchen.example.com/API/mcp",
	} {
		if got, err := CanonicalURL(raw); err != nil || got != want {
			t.Errorf("CanonicalURL(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{
		"", "chenchen.example.com", "/api/mcp", "ftp://chenchen.example.com", "https://",
		"https://user:pass@chenchen.example.com", "https://chenchen.example.com/api/mcp?x=1",
		"https://chenchen.example.com/api/mcp#frag", "https://chenchen.example.com/api/mcp#",
	} {
		if got, err := CanonicalURL(raw); err == nil {
			t.Errorf("CanonicalURL(%q) = %q, want an error", raw, got)
		}
	}
}

func TestLoadReportsEveryProblemAtOnce(t *testing.T) {
	// Fixing one bad variable only to be told about the next one is a miserable
	// way to start a container, so Load must not stop at the first failure.
	clearEnv(t)
	t.Setenv("JWT_SECRET", "too-short")
	t.Setenv("BCRYPT_COST", "3")
	t.Setenv("ACCESS_TOKEN_TTL", "fifteen minutes")
	t.Setenv("DB_MAX_CONNS", "not-a-number")

	_, err := Load()
	if err == nil {
		t.Fatal("Load accepted four invalid values")
	}

	msg := err.Error()
	for _, want := range []string{"JWT_SECRET", "BCRYPT_COST", "ACCESS_TOKEN_TTL", "DB_MAX_CONNS"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error does not mention %s:\n%s", want, msg)
		}
	}
}

func TestLoadRejects(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"ENV", "staging"},
		{"LOG_LEVEL", "verbose"},
		{"LOG_FORMAT", "logfmt"},
		{"BCRYPT_COST", "31"}, // valid for bcrypt, but login would take minutes
		{"BCRYPT_COST", "4"},  // valid for bcrypt, but far too cheap to ship
		{"DB_MAX_CONNS", "0"},
		{"HTTP_SHUTDOWN_TIMEOUT", "-5s"},
		{"AI_MAX_TOOL_TURNS", "0"},
		{"CAPTURE_IP_RATE_LIMIT", "0"},
		{"CAPTURE_KEY_RATE_LIMIT", "-1"},
		{"CAPTURE_KEY_RATE_LIMIT", "twelve"},
		{"IBKR_TOKEN_ENCRYPTION_KEY", "not valid base64!!"},
		{"IBKR_TOKEN_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte("too short"))},
		{"OAUTH_RATE_LIMIT", "0"},
		{"PUBLIC_URL", "chenchen.example.com"},
		{"PUBLIC_URL", "http://chenchen.example.com"}, // tokens in the clear
		{"PUBLIC_URL", "https://chenchen.example.com/app"},
		{"PUBLIC_URL", "https://chenchen.example.com/?x=1"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			clearEnv(t)
			t.Setenv(tc.key, tc.value)

			if _, err := Load(); err == nil {
				t.Errorf("Load accepted %s=%q", tc.key, tc.value)
			}
		})
	}
}

// clearEnv unsets every variable Load reads, so a test does not depend on the
// developer's shell or on .env being present. t.Setenv restores them after.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"ENV", "HTTP_ADDR", "HTTP_SHUTDOWN_TIMEOUT", "CORS_ORIGINS",
		"DATABASE_URL", "DB_MAX_CONNS", "DB_CONNECT_WAIT",
		"JWT_SECRET", "ACCESS_TOKEN_TTL", "REFRESH_TOKEN_TTL", "BCRYPT_COST", "LOGIN_RATE_LIMIT",
		"CAPTURE_IP_RATE_LIMIT", "CAPTURE_KEY_RATE_LIMIT",
		"ANTHROPIC_API_KEY", "ANTHROPIC_MODEL", "AI_MAX_TOOL_TURNS",
		"IBKR_TOKEN_ENCRYPTION_KEY", "PUBLIC_URL", "OAUTH_RATE_LIMIT",
		"LOG_LEVEL", "LOG_FORMAT",
	} {
		t.Setenv(key, "")
	}
}
