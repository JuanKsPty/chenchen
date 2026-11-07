// Command api is the chenchén backend.
//
// Startup order is deliberate and fails loudly: configuration, then the
// database, then the schema, and only then the HTTP listener. Nothing accepts traffic until everything it depends on is
// known to be reachable, so a misconfigured deployment fails at boot with a
// specific message instead of serving 500s.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/JuanKsPty/chenchen/api/internal/accounts"
	"github.com/JuanKsPty/chenchen/api/internal/auth"
	"github.com/JuanKsPty/chenchen/api/internal/captures"
	"github.com/JuanKsPty/chenchen/api/internal/categories"
	"github.com/JuanKsPty/chenchen/api/internal/chat"
	"github.com/JuanKsPty/chenchen/api/internal/config"
	"github.com/JuanKsPty/chenchen/api/internal/httpx"
	"github.com/JuanKsPty/chenchen/api/internal/imports"
	"github.com/JuanKsPty/chenchen/api/internal/investments"
	"github.com/JuanKsPty/chenchen/api/internal/llm"
	"github.com/JuanKsPty/chenchen/api/internal/logging"
	"github.com/JuanKsPty/chenchen/api/internal/mcpserver"
	"github.com/JuanKsPty/chenchen/api/internal/movements"
	"github.com/JuanKsPty/chenchen/api/internal/oauth"
	"github.com/JuanKsPty/chenchen/api/internal/plan"
	"github.com/JuanKsPty/chenchen/api/internal/proposals"
	"github.com/JuanKsPty/chenchen/api/internal/reports"
	"github.com/JuanKsPty/chenchen/api/internal/rules"
	"github.com/JuanKsPty/chenchen/api/internal/server"
	"github.com/JuanKsPty/chenchen/api/internal/store"
	"github.com/JuanKsPty/chenchen/api/internal/transfers"
	"github.com/JuanKsPty/chenchen/api/internal/uploads"
)

func main() {
	if err := run(); err != nil {
		// The logger may not exist yet when configuration is what failed, so
		// this last-resort line goes straight to stderr.
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := logging.New(cfg.Log.Level, cfg.Log.Format)
	slog.SetDefault(logger)

	logger.Info("starting chenchen api",
		"version", server.Version(), "env", cfg.Env, "addr", cfg.HTTP.Addr)

	if cfg.Auth.JWTSecretRandom {
		logger.Warn("JWT_SECRET is not set; using a secret generated for this process only — " +
			"existing sessions will not survive a restart")
	}
	if !cfg.AI.Enabled() {
		logger.Warn("ANTHROPIC_API_KEY is not set; the assistant is unavailable and says so — " +
			"everything else works normally")
	}
	if !cfg.OAuth.Enabled() {
		logger.Info("PUBLIC_URL is not set; Claude cannot connect, and its OAuth routes are not mounted")
	}

	// SIGTERM is what Docker sends on `compose down`; SIGINT is Ctrl-C.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	db, err := store.Open(ctx, cfg.DB.URL, cfg.DB.MaxConns, cfg.DB.ConnectWait, logger)
	if err != nil {
		return err
	}
	defer db.Close()
	logger.Info("postgres connected")

	if err := store.Migrate(ctx, db, logger); err != nil {
		return err
	}
	logger.Info("schema up to date")

	accountsSvc := accounts.NewService(db)
	categoriesSvc := categories.NewService(db)
	movementsSvc := movements.NewService(db, categoriesSvc)
	importsSvc := imports.NewService(db, categoriesSvc)
	capturesSvc := captures.NewService(db, categoriesSvc)
	investmentsSvc := investments.NewService(db, cfg.IBKR.TokenEncryptionKey)

	authSvc, err := auth.NewService(db, accountsSvc, categoriesSvc, cfg.Auth)
	if err != nil {
		return err
	}

	// Without an API key the assistant reports itself unavailable, and the
	// interface says why.
	provider := selectProvider(db, cfg.AI, logger)
	reportsSvc := reports.NewService(db)
	transfersSvc := transfers.NewService(db)
	rulesSvc := rules.NewService(db, categoriesSvc)
	planSvc := plan.NewService(db)
	proposalsSvc := proposals.NewService(db, accountsSvc, categoriesSvc, movementsSvc, rulesSvc, transfersSvc)
	// The statements Claude sends. Without a public URL there is no address
	// to hand out, but the owner's list still answers, empty.
	uploadsSvc := uploads.NewService(db, importsSvc, accountsSvc, cfg.OAuth.PublicURL)
	toolDeps := mcpserver.Deps{
		Accounts:    accountsSvc,
		Movements:   movementsSvc,
		Categories:  categoriesSvc,
		Reports:     reportsSvc,
		Transfers:   transfersSvc,
		Investments: investmentsSvc,
		Rules:       rulesSvc,
		Proposals:   proposalsSvc,
		Plan:        planSvc,
		Captures:    capturesSvc,
		Uploads:     uploadsSvc,
	}
	chatSvc := chat.NewService(db, toolDeps, provider, cfg.AI.MaxToolTurns)

	loginLimiter := httpx.NewRateLimiter(cfg.Auth.LoginRateLimit)
	// Its own limiter, not a shared one with login: a six-digit PIN and a
	// password defend against different kinds of guessing, at different rates.
	pinLoginLimiter := httpx.NewRateLimiter(cfg.Auth.PinLoginRateLimit)
	// The chat gets its own budget: a message costs an upstream API call, which is
	// a different order of expense from reading a balance.
	chatLimiter := httpx.NewRateLimiter(chatMessagesPerMinute)
	// The phone's shortcut gets two: per address before its key is looked
	// up, and per key, which is the one a leaked key cannot get around.
	captureIPLimiter := httpx.NewRateLimiter(cfg.Auth.CaptureIPRateLimit)
	captureKeyLimiter := httpx.NewRateLimiter(cfg.Auth.CaptureKeyRateLimit)
	// Claude's authorize and token endpoints, counted apart per address.
	oauthLimiter := httpx.NewRateLimiter(cfg.OAuth.RateLimit)
	// Claude's tools, counted per connection: every claude.ai user shares a
	// handful of addresses.
	mcpLimiter := httpx.NewRateLimiter(mcpserver.GrantCallsPerMinute)

	// Without a public URL there is no issuer to name: the service is not
	// built, and the router leaves every OAuth route out.
	var (
		oauthSvc     *oauth.Service
		oauthHandler *oauth.Handler
		mcpHandler   http.Handler
	)
	if cfg.OAuth.Enabled() {
		oauthSvc = oauth.NewService(db, cfg.OAuth)
		oauthHandler = oauth.NewHandler(oauthSvc, authSvc.Tokens(), oauthLimiter)
		mcpHandler = mcpserver.NewHTTPHandler(toolDeps, mcpserver.HTTPOptions{
			Verifier:            oauthSvc.Verifier(),
			ResourceMetadataURL: cfg.OAuth.PublicURL + "/.well-known/oauth-protected-resource/api/mcp",
			Limiter:             mcpLimiter,
		})
	}

	router := server.NewRouter(server.Deps{
		Config: cfg,
		Logger: logger,
		Health: map[string]httpx.Checker{
			"postgres": db,
		},
		Auth:        auth.NewHandler(authSvc, cfg.Env == "production", loginLimiter, pinLoginLimiter),
		Tokens:      authSvc.Tokens(),
		Accounts:    accounts.NewHandler(accountsSvc),
		Movements:   movements.NewHandler(movementsSvc),
		Categories:  categories.NewHandler(categoriesSvc),
		Investments: investments.NewHandler(investmentsSvc),
		Imports:     imports.NewHandler(importsSvc, db),
		Reports:     reports.NewHandler(reportsSvc),
		Transfers:   transfers.NewHandler(transfersSvc),
		Rules:       rules.NewHandler(rulesSvc),
		Proposals:   proposals.NewHandler(proposalsSvc),
		Chat:        chat.NewHandler(chatSvc, authSvc, chatLimiter),
		Captures:    captures.NewHandler(capturesSvc),
		Shortcut: captures.NewShortcutHandler(capturesSvc, auth.NewCaptureKeys(db),
			captureIPLimiter, captureKeyLimiter),
		OAuth:   oauthHandler,
		MCP:     mcpHandler,
		Uploads: uploads.NewHandler(uploadsSvc),
		Plan:    plan.NewHandler(planSvc),
	})

	go housekeeping(ctx, authSvc, oauthSvc, uploadsSvc, []*httpx.RateLimiter{
		loginLimiter, pinLoginLimiter, chatLimiter, captureIPLimiter, captureKeyLimiter, oauthLimiter, mcpLimiter,
	}, logger)

	return server.Run(ctx, cfg.HTTP.Addr, router, cfg.HTTP.ShutdownTimeout, logger)
}

// housekeeping prunes the structures that would otherwise grow without bound:
// expired sessions and Claude's expired requests, codes and tokens in the
// database, the statements Claude sent that nobody will use, and idle
// rate-limit buckets in memory. All are cheap, so one modest ticker covers
// them. oauthSvc is nil when Claude cannot connect.
func housekeeping(ctx context.Context, authSvc *auth.Service, oauthSvc *oauth.Service, uploadsSvc *uploads.Service,
	limiters []*httpx.RateLimiter, logger *slog.Logger) {
	const every = 15 * time.Minute

	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// A bounded context of its own: this must not be cancelled by the
			// shutdown of an unrelated request, nor hang forever.
			sweepCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			removed, err := authSvc.PruneSessions(sweepCtx)
			if err != nil {
				logger.Error("pruning expired sessions failed", "error", err)
			}
			var oauthRemoved int64
			if oauthSvc != nil {
				if oauthRemoved, err = oauthSvc.Prune(sweepCtx); err != nil {
					logger.Error("pruning Claude's expired connections failed", "error", err)
				}
			}
			uploadsExpired, err := uploadsSvc.Expire(sweepCtx)
			if err != nil {
				logger.Error("expiring statement uploads failed", "error", err)
			}
			cancel()
			var dropped int
			for _, limiter := range limiters {
				dropped += limiter.Sweep()
			}
			logger.Debug("housekeeping", "sessions_pruned", removed, "oauth_pruned", oauthRemoved,
				"uploads_expired", uploadsExpired, "rate_limit_buckets_dropped", dropped)
		}
	}
}

// chatMessagesPerMinute is the per-address budget for chat messages.
//
// Six, not twenty. Twenty was chosen when a message cost nothing but database
// round trips; with a real model behind it, this number is the rate at which a
// stranger can spend the deployment's money, and each message can drive several
// API calls. Six per minute is faster than anybody types and slow enough that the
// spend ceiling is reached by usage rather than by a script.
const chatMessagesPerMinute = 6

// selectProvider picks the engine behind the assistant.
//
// A missing API key must never stop the process from booting or make the chat
// return an error: a fresh clone has no credentials. Without one, the assistant
// says it is unavailable and the interface says why.
//
// When a key *is* present, the model never gets called directly. It goes behind the
// spend ceilings, because this deployment is public and the key on it is somebody's
// actual money: registration is open, so without a ceiling the budget is whatever a
// stranger decides to spend. When the money or the key runs out, the assistant
// reports itself unavailable rather than telling people to "try again in a moment"
// forever.
func selectProvider(db *store.DB, cfg config.AIConfig, logger *slog.Logger) llm.Provider {
	unavailable := chat.NewUnavailable()

	if !cfg.Enabled() {
		return unavailable
	}

	provider, err := llm.NewAnthropic(cfg.APIKey, cfg.Model)
	if err != nil {
		// A key that is present but unusable — empty after trimming, say. The
		// rest of the application keeps working and the log says why.
		logger.Error("the AI provider could not be built; the assistant is unavailable", "error", err)
		return unavailable
	}

	caps := chat.Caps{
		Total:     cfg.BudgetMicros,
		Daily:     cfg.DailyBudgetMicros,
		UserDaily: cfg.UserDailyBudgetMicros,
	}
	if caps.Total == 0 {
		// A key with no allowance to spend it. Worth saying out loud, because the
		// symptom — an assistant that is unavailable despite a configured key —
		// otherwise looks like the key being broken.
		logger.Warn("an API key is configured but AI_BUDGET_USD is zero; the assistant is unavailable")
	}
	logger.Info("AI assistant enabled",
		"model", cfg.Model,
		"budget_micros", caps.Total,
		"daily_micros", caps.Daily,
		"user_daily_micros", caps.UserDaily,
		"max_tool_turns", cfg.MaxToolTurns)

	return chat.NewBudgeted(provider, unavailable, chat.NewKeeper(db), caps)
}
