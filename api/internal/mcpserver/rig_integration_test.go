package mcpserver_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

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
	"github.com/JuanKsPty/chenchen/api/internal/plan"
	"github.com/JuanKsPty/chenchen/api/internal/proposals"
	"github.com/JuanKsPty/chenchen/api/internal/reports"
	"github.com/JuanKsPty/chenchen/api/internal/rules"
	"github.com/JuanKsPty/chenchen/api/internal/server"
	"github.com/JuanKsPty/chenchen/api/internal/store"
	"github.com/JuanKsPty/chenchen/api/internal/storetest"
	"github.com/JuanKsPty/chenchen/api/internal/transfers"
	"github.com/JuanKsPty/chenchen/api/internal/uploads"
)

// rig is the whole API, as main builds it with a public URL, served over a
// real socket: the go-sdk's client talks to it as Claude would.
type rig struct {
	db       *store.DB
	srv      *httptest.Server
	oauth    *oauth.Service
	accounts *accounts.Service
	captures *captures.Service
	imports  *imports.Service
	uploads  *uploads.Service
	tokens   *auth.TokenIssuer
	logs     *syncBuffer
	owner    uuid.UUID
	stranger uuid.UUID
}

// syncBuffer is a log the server's goroutines write to at once.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newRig(t *testing.T) *rig { return newRigWith(t, 1000) }

// newRigWith builds the rig with callsPerMinute as each connection's limit.
func newRigWith(t *testing.T, callsPerMinute int) *rig {
	t.Helper()
	db := storetest.New(t)
	r := &rig{db: db, logs: &syncBuffer{}}
	var router http.Handler
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		router.ServeHTTP(w, req)
	}))
	t.Cleanup(r.srv.Close)

	cfg := config.Config{Env: "development", Auth: config.AuthConfig{
		JWTSecret: []byte(strings.Repeat("s", 32)), AccessTTL: 15 * time.Minute, RefreshTTL: time.Hour,
		BcryptCost: 10, LoginRateLimit: 10, PinLoginRateLimit: 10, DeviceTTL: time.Hour,
		CaptureIPRateLimit: 60, CaptureKeyRateLimit: 12,
	}, OAuth: config.OAuthConfig{PublicURL: r.srv.URL, RateLimit: 1000}}
	logger := slog.New(slog.NewTextHandler(r.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	cats := categories.NewService(db)
	r.accounts = accounts.NewService(db)
	authSvc, err := auth.NewService(db, r.accounts, cats, cfg.Auth)
	if err != nil {
		t.Fatal(err)
	}
	movementsSvc := movements.NewService(db, cats)
	reportsSvc := reports.NewService(db)
	transfersSvc := transfers.NewService(db)
	rulesSvc := rules.NewService(db, cats)
	investmentsSvc := investments.NewService(db, nil)
	proposalsSvc := proposals.NewService(db, r.accounts, cats, movementsSvc, rulesSvc, transfersSvc)
	r.captures = captures.NewService(db, cats)
	r.imports = imports.NewService(db, cats)
	r.uploads = uploads.NewService(db, r.imports, r.accounts, cfg.OAuth.PublicURL)
	planSvc := plan.NewService(db)
	deps := mcpserver.Deps{Accounts: r.accounts, Movements: movementsSvc, Categories: cats, Reports: reportsSvc,
		Transfers: transfersSvc, Investments: investmentsSvc, Rules: rulesSvc, Proposals: proposalsSvc,
		Plan: planSvc, Captures: r.captures, Uploads: r.uploads}
	chatSvc := chat.NewService(db, deps, chat.NewUnavailable(), 1)
	limiter := func(n int) *httpx.RateLimiter { return httpx.NewRateLimiter(n) }

	r.oauth = oauth.NewService(db, cfg.OAuth)
	router = server.NewRouter(server.Deps{
		Config: cfg, Logger: logger, Health: map[string]httpx.Checker{"postgres": db},
		Auth:        auth.NewHandler(authSvc, false, limiter(10), limiter(10)),
		Tokens:      authSvc.Tokens(),
		Accounts:    accounts.NewHandler(r.accounts),
		Movements:   movements.NewHandler(movementsSvc),
		Categories:  categories.NewHandler(cats),
		Investments: investments.NewHandler(investmentsSvc),
		Imports:     imports.NewHandler(r.imports, db),
		Reports:     reports.NewHandler(reportsSvc),
		Transfers:   transfers.NewHandler(transfersSvc),
		Rules:       rules.NewHandler(rulesSvc),
		Proposals:   proposals.NewHandler(proposalsSvc),
		Chat:        chat.NewHandler(chatSvc, authSvc, limiter(6)),
		Captures:    captures.NewHandler(r.captures),
		Shortcut:    captures.NewShortcutHandler(r.captures, auth.NewCaptureKeys(db), limiter(60), limiter(12)),
		OAuth:       oauth.NewHandler(r.oauth, authSvc.Tokens(), limiter(1000)),
		MCP: mcpserver.NewHTTPHandler(deps, mcpserver.HTTPOptions{
			Verifier:            r.oauth.Verifier(),
			ResourceMetadataURL: r.srv.URL + "/.well-known/oauth-protected-resource/api/mcp",
			Limiter:             limiter(callsPerMinute),
		}),
		Uploads: uploads.NewHandler(r.uploads),
		Plan:    plan.NewHandler(planSvc),
	})
	r.tokens = authSvc.Tokens()
	r.owner = r.newUser(t, "owner@example.com", cats)
	r.stranger = r.newUser(t, "stranger@example.com", cats)
	return r
}

func (r *rig) newUser(t *testing.T, email string, cats *categories.Service) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	u, err := r.db.Q().CreateUser(ctx, store.User{ID: uuid.New(), Email: email, PasswordHash: "x", FullName: "Persona De Prueba"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.db.InTx(ctx, func(q *store.Queries) error { return cats.SeedDefaults(ctx, q, u.ID) }); err != nil {
		t.Fatal(err)
	}
	return u.ID
}

// connect gives user a connection from Claude with these scopes, as the
// consent page would, and returns its access token and grant.
func (r *rig) connect(t *testing.T, user uuid.UUID, scopes ...string) (string, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	g, err := r.db.Q().CreateOAuthGrant(ctx, store.OAuthGrant{ID: uuid.New(), UserID: user, ClientID: oauth.ClaudeClientID,
		Scopes: scopes, RequestedScopes: []string{oauth.ScopeRead, oauth.ScopeWrite}, Resource: r.oauth.Resource(),
		CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	token, hash, err := auth.NewOAuthAccessToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.db.Q().CreateOAuthToken(ctx, store.OAuthToken{Hash: hash, GrantID: g.ID, UserID: user,
		Kind: store.OAuthAccess, Scopes: scopes, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	return token, g.ID
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

// bearer adds a token to every request, as Claude's client does.
type bearer struct {
	token string
	next  http.RoundTripper
}

func (b bearer) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+b.token)
	return b.next.RoundTrip(req)
}

// client connects to the MCP endpoint with token, as Claude would.
func (r *rig) client(t *testing.T, token string) *mcp.ClientSession {
	t.Helper()
	transport := &mcp.StreamableClientTransport{Endpoint: r.srv.URL + "/api/mcp", MaxRetries: -1,
		DisableStandaloneSSE: true,
		HTTPClient:           &http.Client{Transport: bearer{token: token, next: http.DefaultTransport}}}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "claude-test", Version: "1.0.0"}, nil).
		Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatalf("connecting to the MCP endpoint: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// call invokes a tool and decodes its structured result into out, when out
// is not nil. It returns whether the tool failed and its text.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any, out any) (bool, string) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return true, err.Error()
	}
	var text string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	if !res.IsError && out != nil {
		raw, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s returned %s: %v", name, raw, err)
		}
	}
	return res.IsError, text
}

// mustCall is call for a tool that must succeed.
func mustCall(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any, out any) {
	t.Helper()
	if failed, text := call(t, cs, name, args, out); failed {
		t.Fatalf("%s(%v) failed: %s", name, args, text)
	}
}

// do sends a request straight to the API.
func (r *rig) do(t *testing.T, method, path, authorization, contentType string, body io.Reader) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, r.srv.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, raw
}

// put sends data to an upload address, as curl --data-binary would.
func (r *rig) put(t *testing.T, url string, data []byte) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(raw)
}

// prepared is what prepare_import answers.
type prepared struct {
	UploadID  string `json:"upload_id"`
	UploadURL string `json:"upload_url"`
	Method    string `json:"method"`
	ExpiresAt string `json:"expires_at"`
}

// prepare asks for an address for data, declared as it is.
func prepare(t *testing.T, cs *mcp.ClientSession, filename string, data []byte) prepared {
	t.Helper()
	var p prepared
	mustCall(t, cs, mcpserver.ToolPrepareImport, map[string]any{"filename": filename, "sha256": digest(data),
		"size": len(data)}, &p)
	return p
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// status is what import_status answers.
type status struct {
	UploadID string `json:"upload_id"`
	Status   string `json:"status"`
	Reason   string `json:"reason"`
	Filename string `json:"filename"`
	Account  *struct {
		ID    string `json:"id"`
		Last4 string `json:"last4"`
		Name  string `json:"name"`
	} `json:"account"`
	Lines *struct {
		New        int `json:"new"`
		Duplicates int `json:"duplicates"`
	} `json:"lines"`
	MatchesBank *bool  `json:"matches_bank"`
	Difference  string `json:"difference"`
}

// send uploads data through prepare_import and a PUT, and returns what
// import_status says of it. The PUT must answer with the upload's id alone.
func (r *rig) send(t *testing.T, cs *mcp.ClientSession, filename string, data []byte) status {
	t.Helper()
	p := prepare(t, cs, filename, data)
	code, body := r.put(t, p.UploadURL, data)
	if want := fmt.Sprintf(`{"upload_id":%q}`, p.UploadID); code != http.StatusOK || body != want {
		t.Fatalf("PUT %s = %d %s; want 200 %s", filename, code, body, want)
	}
	var st status
	mustCall(t, cs, mcpserver.ToolImportStatus, map[string]any{"upload_id": p.UploadID}, &st)
	return st
}

// importAsOwner imports a file through the web, as the owner choosing it by
// hand.
func (r *rig) importAsOwner(t *testing.T, user uuid.UUID, filename string, data []byte) map[string]any {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	res, raw := r.do(t, http.MethodPost, "/api/imports", r.session(t, user), w.FormDataContentType(), &body)
	var out map[string]any
	if res.StatusCode != http.StatusOK || json.Unmarshal(raw, &out) != nil {
		t.Fatalf("POST /api/imports %s = %d %s", filename, res.StatusCode, raw)
	}
	return out
}

func (r *rig) count(t *testing.T, from string, args ...any) int {
	t.Helper()
	return storetest.Count(t, r.db, from, args...)
}

// --- fixtures -------------------------------------------------------------------
//
// Invented statements in the formats the parsers read. No real person,
// account or movement.

func latin1(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		out = append(out, byte(r))
	}
	return out
}

// bacFile is a bank account's statement for product, from opening to
// closing as of asOf (DD/MM/YYYY), with detail lines "DD/MM/YYYY, ref, code,
// description, debit, credit, balance".
func bacFile(product, opening, closing, asOf string, detail ...string) []byte {
	lines := []string{
		"Número de Clientes, Nombre, Producto, Moneda, Saldo Inicial, Saldo en Libros, Retenidos y Diferidos, Saldo Disponible, Fecha",
		"900000001, PERSONA DE PRUEBA, " + product + ", USD, " + opening + ", " + closing + ", 0.00, " + closing + ", " + asOf,
		"Detalle de Estado Bancario",
		"Fecha de Transacción, Referencia de Transacción, Código de Transacción, Descripción de Transacción, Débito de Transacción, Crédito de Transacción, Balance de Transacción",
	}
	return latin1(strings.Join(append(lines, detail...), "\r\n"))
}

const product = "100000001"

// june opens the account at 500.00 and closes it at 750.75.
var june = bacFile(product, "500.00", "750.75", "30/06/2026",
	"15/06/2026, 1, 4A, TRANSFERENCIA DE PRUEBA, 25.00, 0.00, 475.00",
	"20/06/2026, 2, 4C, DEPOSITO DE PRUEBA, 0.00, 275.75, 750.75")

// july continues june where it closed.
var july = bacFile(product, "750.75", "700.75", "31/07/2026",
	"03/07/2026, 3, CP, SUPERMERCADO DE PRUEBA, 50.00, 0.00, 700.75")

// cardFile is a card's movements export, one line per card account given.
func cardFile(accounts ...string) []byte {
	out := "Cuenta;Tarjeta;Fecha tran;Fecha proceso;Descripcion;Referencia;Categoria;Cargos (Db);Pagos (Cr)\r\n"
	for i, a := range accounts {
		out += fmt.Sprintf("**** %s;**** %s;1%d/01/2026;1%d/01/2026;CAFE DE PRUEBA %d;R%d;Comida y Bebida;-9.5;\r\n",
			a, a, i, i, i, i)
	}
	return []byte(out)
}
