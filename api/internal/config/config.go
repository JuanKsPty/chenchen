// Package config turns the process environment into a validated struct.
//
// Two rules shape it. First, every setting has a working default, so a fresh
// clone boots with `cp .env.example .env && docker compose up` and nothing
// else. Second, loading either succeeds completely or reports *all* the
// problems at once — discovering a second bad setting only after fixing the
// first is a miserable way to start a container.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/JuanKsPty/chenchen/api/internal/money"
)

// Config is the whole configuration of the API process.
type Config struct {
	Env   string // "development" or "production"; affects log format and error detail
	HTTP  HTTPConfig
	DB    DBConfig
	Auth  AuthConfig
	AI    AIConfig
	IBKR  IBKRConfig
	OAuth OAuthConfig
	Log   LogConfig
}

type HTTPConfig struct {
	Addr string
	// ShutdownTimeout bounds how long in-flight requests get to finish after a
	// SIGTERM before the process exits anyway.
	ShutdownTimeout time.Duration
	// CORSOrigins are the browser origins allowed to call the API. In the
	// container image the frontend is served by nginx on the same origin, so
	// this is empty in production and only used for `npm run dev`.
	CORSOrigins []string
}

type DBConfig struct {
	URL         string
	MaxConns    int32
	ConnectWait time.Duration
}

type AuthConfig struct {
	// JWTSecret signs access tokens. See Load for what happens when it is unset.
	JWTSecret       []byte
	JWTSecretRandom bool
	AccessTTL       time.Duration
	RefreshTTL      time.Duration
	BcryptCost      int
	// LoginRateLimit is the number of login attempts allowed per IP per minute.
	LoginRateLimit int
	// PinLoginRateLimit is the same idea for /login-pin, kept separate from
	// LoginRateLimit because the two endpoints defend against different things:
	// a password's keyspace against an e-mail sweep, versus a six-digit PIN
	// against whoever is holding a stolen device cookie.
	PinLoginRateLimit int
	// DeviceTTL is how long a device stays trusted for PIN login after being
	// enabled from the Seguridad screen, absent any activity that would refresh
	// it. Long, because re-proving a device is exactly the friction PIN login
	// exists to remove.
	DeviceTTL time.Duration
	// CaptureIPRateLimit is how many purchases the phone's shortcut endpoint
	// takes per IP per minute. It runs before a key is looked up, so guessing
	// keys costs a request each; but someone with many addresses gets that
	// many allowances, so it is only a speed bump.
	CaptureIPRateLimit int
	// CaptureKeyRateLimit is how many purchases one shortcut key may send per
	// minute, and the limit that holds: far more than anyone taps, few enough
	// that a leaked key cannot bury an account in noise before it is revoked.
	CaptureKeyRateLimit int
}

type AIConfig struct {
	APIKey string
	Model  string
	// MaxToolTurns bounds the agentic loop so a confused model cannot spin.
	MaxToolTurns int

	// The three spend ceilings, in micro-dollars. Micro rather than cents because
	// one call can cost a fraction of a cent, and a ceiling that cannot represent
	// the thing it counts is not a ceiling.
	//
	// They exist because this deployment is public and registration is open:
	// without them, the budget is whatever a stranger decides to spend. Zero disables the assistant's use of the API
	// entirely, which is a supported state — the rule-based engine answers.
	//
	// BudgetMicros is the lifetime ceiling and the one that matters.
	BudgetMicros int64
	// DailyBudgetMicros keeps one bad day from consuming the lifetime budget.
	DailyBudgetMicros int64
	// UserDailyBudgetMicros keeps one account from consuming everyone else's share.
	UserDailyBudgetMicros int64
}

// Enabled reports whether the chat assistant can reach a model. When false the
// rest of the application is unaffected and the chat endpoint says so plainly.
func (c AIConfig) Enabled() bool { return c.APIKey != "" }

// IBKRConfig holds the key that encrypts an IBKR Flex Web Service token at
// rest.
//
// Unlike AuthConfig.JWTSecret, this key is never generated when unset: a
// random JWT secret only costs existing sessions on restart, but a random
// encryption key would make every previously stored token permanently
// undecryptable the moment the process restarted. So a missing key simply
// leaves the feature unavailable — SetLink refuses with a clear error —
// rather than silently corrupting data.
type IBKRConfig struct {
	// TokenEncryptionKey is 32 raw bytes (AES-256), decoded from base64. Empty
	// when unset.
	TokenEncryptionKey []byte
}

// Enabled reports whether IBKR tokens can be encrypted and, therefore, stored.
func (c IBKRConfig) Enabled() bool { return len(c.TokenEncryptionKey) > 0 }

// OAuthConfig is how Claude signs in to chenchén (ADR-0004): chenchén is both
// the authorization server Claude gets its tokens from and the resource those
// tokens are for, and both are named by the address the app is reached at.
//
// Like the AI key, it is optional. Without PUBLIC_URL there is no issuer to
// name, so every OAuth and MCP route is left unmounted and answers 404, and the
// rest of the application runs as it always did.
type OAuthConfig struct {
	// PublicURL is where the app is reached, in canonical form: lower-case
	// scheme and host, no default port, no path and no trailing slash. Empty
	// when unset.
	PublicURL string
	// RateLimit is how many requests per minute one address may make to the
	// authorize and token endpoints, each counted on its own. Generous: all of
	// claude.ai reaches them from a handful of addresses.
	RateLimit int
}

// Enabled reports whether Claude can connect at all.
func (c OAuthConfig) Enabled() bool { return c.PublicURL != "" }

// Issuer is the authorization server's identifier: the public URL itself,
// with no path, which is what RFC 8414 discovery derives the metadata address
// from.
func (c OAuthConfig) Issuer() string { return c.PublicURL }

// Resource is the protected resource Claude's tokens are for: the MCP
// endpoint, in exactly the form protected resource metadata announces it.
func (c OAuthConfig) Resource() string { return c.PublicURL + "/api/mcp" }

type LogConfig struct {
	Level  string // debug | info | warn | error
	Format string // json | text
}

// Load reads the environment and validates it.
func Load() (Config, error) {
	var errs []error
	fail := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	env := oneOf("ENV", "development", "development", "production")
	if env == "" {
		fail("ENV must be development or production, got %q", os.Getenv("ENV"))
		env = "development"
	}

	cfg := Config{
		Env: env,
		HTTP: HTTPConfig{
			Addr:            str("HTTP_ADDR", ":8080"),
			ShutdownTimeout: duration("HTTP_SHUTDOWN_TIMEOUT", 15*time.Second, fail),
			CORSOrigins:     list("CORS_ORIGINS", "http://localhost:5173"),
		},
		DB: DBConfig{
			URL:         str("DATABASE_URL", "postgres://chenchen:chenchen@localhost:5432/chenchen?sslmode=disable"),
			MaxConns:    int32(number("DB_MAX_CONNS", 10, fail)),
			ConnectWait: duration("DB_CONNECT_WAIT", 30*time.Second, fail),
		},
		Auth: AuthConfig{
			AccessTTL:         duration("ACCESS_TOKEN_TTL", 15*time.Minute, fail),
			RefreshTTL:        duration("REFRESH_TOKEN_TTL", 720*time.Hour, fail),
			BcryptCost:        number("BCRYPT_COST", 10, fail),
			LoginRateLimit:    number("LOGIN_RATE_LIMIT", 10, fail),
			PinLoginRateLimit: number("PIN_LOGIN_RATE_LIMIT", 15, fail),
			// 180 days: long enough that "trusted this device" means something,
			// short enough that a device nobody has touched in half a year is not
			// trusted forever.
			DeviceTTL:           duration("DEVICE_TOKEN_TTL", 4320*time.Hour, fail),
			CaptureIPRateLimit:  number("CAPTURE_IP_RATE_LIMIT", 60, fail),
			CaptureKeyRateLimit: number("CAPTURE_KEY_RATE_LIMIT", 12, fail),
		},
		OAuth: OAuthConfig{
			RateLimit: number("OAUTH_RATE_LIMIT", 30, fail),
		},
		AI: AIConfig{
			APIKey: str("ANTHROPIC_API_KEY", ""),
			Model:  str("ANTHROPIC_MODEL", "claude-sonnet-5"),
			// Four, not eight. A banking question needs one or two rounds of
			// tools; the rest of the allowance only ever bought a confused model
			// more chances to keep asking, and each round re-sends the whole
			// conversation.
			MaxToolTurns:          number("AI_MAX_TOOL_TURNS", 4, fail),
			BudgetMicros:          dollars("AI_BUDGET_USD", "4.00", fail),
			DailyBudgetMicros:     dollars("AI_DAILY_BUDGET_USD", "1.00", fail),
			UserDailyBudgetMicros: dollars("AI_USER_DAILY_BUDGET_USD", "0.30", fail),
		},
		Log: LogConfig{
			Level:  oneOf("LOG_LEVEL", "info", "debug", "info", "warn", "error"),
			Format: oneOf("LOG_FORMAT", defaultLogFormat(env), "json", "text"),
		},
	}

	// An unset signing secret must not stop the process from booting — a fresh
	// clone has no .env of its own — but a hardcoded fallback
	// committed to a public repository would be worse than no secret at all.
	// So one is generated per process: everything works, and the only cost is
	// that sessions do not survive a restart. Load's caller warns about it.
	if secret := os.Getenv("JWT_SECRET"); secret != "" {
		if len(secret) < 32 {
			fail("JWT_SECRET must be at least 32 characters, got %d", len(secret))
		}
		cfg.Auth.JWTSecret = []byte(secret)
	} else {
		secret, err := randomSecret(32)
		if err != nil {
			fail("generating an ephemeral JWT secret: %w", err)
		}
		cfg.Auth.JWTSecret = secret
		cfg.Auth.JWTSecretRandom = true
	}

	if cfg.Log.Level == "" {
		fail("LOG_LEVEL must be one of debug, info, warn, error; got %q", os.Getenv("LOG_LEVEL"))
	}
	if cfg.Log.Format == "" {
		fail("LOG_FORMAT must be json or text, got %q", os.Getenv("LOG_FORMAT"))
	}
	if cfg.DB.MaxConns < 1 {
		fail("DB_MAX_CONNS must be at least 1, got %d", cfg.DB.MaxConns)
	}
	// bcrypt itself rejects anything outside 4..31; below 10 is too cheap to be
	// worth shipping, and the ceiling keeps a typo from making login take
	// minutes instead of milliseconds.
	if cfg.Auth.BcryptCost < 10 || cfg.Auth.BcryptCost > 15 {
		fail("BCRYPT_COST must be between 10 and 15, got %d", cfg.Auth.BcryptCost)
	}
	if cfg.AI.MaxToolTurns < 1 {
		fail("AI_MAX_TOOL_TURNS must be at least 1, got %d", cfg.AI.MaxToolTurns)
	}
	// A limiter quietly treats anything below one as one, so a zero meant as
	// "off" would instead let one purchase a minute through: say so at boot.
	if cfg.Auth.CaptureIPRateLimit < 1 {
		fail("CAPTURE_IP_RATE_LIMIT must be at least 1, got %d", cfg.Auth.CaptureIPRateLimit)
	}
	if cfg.Auth.CaptureKeyRateLimit < 1 {
		fail("CAPTURE_KEY_RATE_LIMIT must be at least 1, got %d", cfg.Auth.CaptureKeyRateLimit)
	}

	if cfg.OAuth.RateLimit < 1 {
		fail("OAUTH_RATE_LIMIT must be at least 1, got %d", cfg.OAuth.RateLimit)
	}
	// Optional as well: without it Claude cannot connect, and nothing else
	// changes.
	if raw := strings.TrimSpace(os.Getenv("PUBLIC_URL")); raw != "" {
		publicURL, err := publicOrigin(raw)
		if err != nil {
			fail("PUBLIC_URL %w", err)
		} else {
			cfg.OAuth.PublicURL = publicURL
		}
	}

	// Optional, like the AI key: an empty value leaves the IBKR link feature
	// disabled rather than failing the boot, since a deployment that never links
	// a brokerage account has no reason to set it.
	if raw := strings.TrimSpace(os.Getenv("IBKR_TOKEN_ENCRYPTION_KEY")); raw != "" {
		key, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			fail("IBKR_TOKEN_ENCRYPTION_KEY must be base64-encoded: %w", err)
		} else if len(key) != 32 {
			fail("IBKR_TOKEN_ENCRYPTION_KEY must decode to 32 bytes (AES-256), got %d", len(key))
		} else {
			cfg.IBKR.TokenEncryptionKey = key
		}
	}

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("config: %w", errors.Join(errs...))
	}
	return cfg, nil
}

// publicOrigin checks that raw is an address the app can be reached at — an
// origin, with nothing after the host — and returns it canonical.
//
// https is required except on a loopback host: the tokens Claude receives are
// bearer credentials, and claude.ai will not connect to anything else anyway.
func publicOrigin(raw string) (string, error) {
	canonical, err := CanonicalURL(raw)
	if err != nil {
		return "", err
	}
	u, _ := url.Parse(canonical)
	if u.Path != "" {
		return "", fmt.Errorf("must be an origin with no path, got %q", raw)
	}
	if u.Scheme != "https" && !isLoopback(u.Hostname()) {
		return "", fmt.Errorf("must use https outside localhost, got %q", raw)
	}
	return canonical, nil
}

// CanonicalURL is the form an absolute http(s) URL is stored and compared in:
// scheme and host lower-cased, the scheme's default port dropped and any
// trailing slash trimmed. The path keeps its case, since a path is
// case-sensitive. A URL with credentials, a query or a fragment is refused —
// none of them belong in an origin or in a resource identifier.
func CanonicalURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("is not a URL: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	switch {
	case scheme != "https" && scheme != "http":
		return "", fmt.Errorf("must be an http or https URL, got %q", raw)
	case u.Host == "" || u.Opaque != "":
		return "", fmt.Errorf("must name a host, got %q", raw)
	case u.User != nil:
		return "", fmt.Errorf("must not carry credentials")
	case u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#"):
		return "", fmt.Errorf("must not have a query or a fragment, got %q", raw)
	}

	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", fmt.Errorf("must name a host, got %q", raw)
	}
	port := u.Port()
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	switch {
	case port != "":
		host = net.JoinHostPort(host, port)
	case strings.Contains(host, ":"):
		host = "[" + host + "]" // an IPv6 literal keeps its brackets
	}
	return scheme + "://" + host + strings.TrimRight(u.EscapedPath(), "/"), nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func randomSecret(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

func defaultLogFormat(env string) string {
	if env == "production" {
		return "json"
	}
	return "text"
}

func str(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// oneOf returns the value only if it is in allowed, and "" otherwise so the
// caller can report a usable error naming the variable.
func oneOf(key, def string, allowed ...string) string {
	v := strings.ToLower(str(key, def))
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	return ""
}

func list(key, def string) []string {
	var out []string
	for _, part := range strings.Split(str(key, def), ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func number(key string, def int, fail func(string, ...any)) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		fail("%s must be an integer, got %q", key, raw)
		return def
	}
	return n
}

// microsPerCent scales a cent into the micro-dollars the budget counts in.
const microsPerCent = 10_000

// dollars reads a spend ceiling written the way a person writes money ("4.00")
// and returns it in micro-dollars.
//
// It goes through money.Parse rather than strconv.ParseFloat for the same reason
// every other amount in this system does: parsing "0.30" as a float and scaling it
// gives 299999.99999999994 micro-dollars, and a ceiling that is a hair under what
// was asked for is a ceiling nobody can reason about. Parse works on the decimal
// text and refuses anything more precise than a cent.
func dollars(key, def string, fail func(string, ...any)) int64 {
	raw := str(key, def)

	cents, err := money.Parse(raw)
	if err != nil {
		fail("%s must be an amount in dollars such as 4.00, got %q: %v", key, raw, err)
		return 0
	}
	if cents < 0 {
		fail("%s must not be negative, got %q", key, raw)
		return 0
	}
	return int64(cents) * microsPerCent
}

func duration(key string, def time.Duration, fail func(string, ...any)) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		fail("%s must be a duration such as 15m or 30s, got %q", key, raw)
		return def
	}
	if d <= 0 {
		fail("%s must be positive, got %q", key, raw)
		return def
	}
	return d
}
