package uploads

import (
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/httpx"
	"github.com/JuanKsPty/chenchen/api/internal/identity"
	"github.com/JuanKsPty/chenchen/api/internal/imports"
	"github.com/JuanKsPty/chenchen/api/internal/statement"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// Handler serves both sides of an upload: the address a file is sent to, and
// the owner's list of the files that wait for them.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// AddressRoutes is the address files are sent to, mounted at
// /api/mcp/uploads with no session and no token: the address is the
// credential. Its path is a secret, so the access log records the route, never
// the path.
func (h *Handler) AddressRoutes() http.Handler {
	r := chi.NewRouter()
	r.Use(httpx.RedactPath)
	r.Put("/{token}", h.receive)
	return r
}

// Routes is the owner's side, mounted at /api/imports/uploads behind a
// session.
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/", h.list)
	r.Get("/{id}/dry-run", h.dryRun)
	r.Post("/{id}/import", h.importFile)
	r.Post("/{id}/dismiss", h.dismiss)
	return r
}

// received is the whole answer to a file sent to an address, whatever it
// turned out to be: whoever holds the address learns nothing else from it.
type received struct {
	UploadID uuid.UUID `json:"upload_id"`
}

// bodyTimeout is how long a file sent to an address may take to arrive. The
// server's own read timeout is sized for JSON; a 20 MB statement on a slow
// uplink needs longer.
const bodyTimeout = 5 * time.Minute

func (h *Handler) receive(w http.ResponseWriter, r *http.Request) {
	// Only a writer that cannot move its deadline refuses, and that is a test.
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(bodyTimeout))
	body := http.MaxBytesReader(w, r.Body, MaxSize)
	id, err := h.svc.Receive(r.Context(), chi.URLParam(r, "token"), body)
	if err != nil {
		httpx.Fail(w, r, translate(err))
		return
	}
	httpx.JSON(w, r, http.StatusOK, received{UploadID: id})
}

// View is an upload as the owner's list shows it.
type View struct {
	ID        uuid.UUID    `json:"id"`
	Filename  string       `json:"filename"`
	Status    string       `json:"status"`
	Reason    string       `json:"reason,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
	Account   *AccountView `json:"account,omitempty"`
}

// AccountView names the account an upload is for without naming its bank.
type AccountView struct {
	ID    uuid.UUID `json:"id"`
	Last4 string    `json:"last4,omitempty"`
	Name  string    `json:"name"`
}

// NewAccountView is a's AccountView.
func NewAccountView(a store.Account) *AccountView {
	return &AccountView{ID: a.ID, Last4: store.LastFour(a.ExternalNumber), Name: AccountLabel(a)}
}

var listable = map[string]bool{
	store.UploadPending: true, store.UploadImported: true, store.UploadUnchanged: true, store.UploadNeedsReview: true,
	store.UploadUnreadable: true, store.UploadDismissed: true, store.UploadExpired: true,
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = store.UploadNeedsReview
	}
	if !listable[status] {
		httpx.Fail(w, r, httpx.BadRequest("invalid_status", "Ese estado no existe."))
		return
	}
	list, err := h.svc.List(r.Context(), identity.MustFromContext(r.Context()), status)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out := make([]View, 0, len(list))
	for _, l := range list {
		v := View{ID: l.ID, Filename: l.Filename, Status: l.Status, Reason: l.Reason, CreatedAt: l.CreatedAt.UTC()}
		if l.Account != nil {
			v.Account = NewAccountView(*l.Account)
		}
		out = append(out, v)
	}
	httpx.JSON(w, r, http.StatusOK, map[string]any{"uploads": out})
}

func (h *Handler) dryRun(w http.ResponseWriter, r *http.Request) {
	id, ok := uploadID(w, r)
	if !ok {
		return
	}
	parsed, err := h.svc.DryRun(r.Context(), identity.MustFromContext(r.Context()), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.Fail(w, r, translate(err))
			return
		}
		// What the web's dry run says of the same file chosen by hand.
		statement.LogParseProblem(r, err)
		httpx.Fail(w, r, statement.ParseProblem(err))
		return
	}
	httpx.JSON(w, r, http.StatusOK, statement.NewFileView(parsed))
}

// importRequest optionally names the account the owner imports into, as the
// web's import does from an account's page.
type importRequest struct {
	AccountID *uuid.UUID `json:"account_id"`
}

func (h *Handler) importFile(w http.ResponseWriter, r *http.Request) {
	id, ok := uploadID(w, r)
	if !ok {
		return
	}
	var body importRequest
	if r.ContentLength != 0 {
		if err := httpx.Decode(w, r, &body); err != nil && !errors.Is(err, io.EOF) {
			httpx.Fail(w, r, err)
			return
		}
	}
	result, err := h.svc.Import(r.Context(), identity.MustFromContext(r.Context()), id, body.AccountID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.Fail(w, r, translate(err))
			return
		}
		httpx.Fail(w, r, imports.TranslateError(r, err))
		return
	}
	httpx.JSON(w, r, http.StatusOK, imports.NewResultView(result))
}

func (h *Handler) dismiss(w http.ResponseWriter, r *http.Request) {
	id, ok := uploadID(w, r)
	if !ok {
		return
	}
	if err := h.svc.Dismiss(r.Context(), identity.MustFromContext(r.Context()), id); err != nil {
		httpx.Fail(w, r, translate(err))
		return
	}
	httpx.NoContent(w)
}

func uploadID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Fail(w, r, translate(ErrNotFound))
		return uuid.Nil, false
	}
	return id, true
}

// translate maps this package's errors to responses. Each is in Spanish:
// the address's are read by Claude, the rest by the owner.
func translate(err error) error {
	var bad invalid
	switch {
	case errors.As(err, &bad):
		return httpx.BadRequest("invalid_upload", bad.Message()).WithCause(err)
	case errors.Is(err, ErrUnknownAddress):
		return httpx.NotFound("upload_address_unknown",
			"Esta dirección de subida no existe, ya se usó o venció. Pide otra con prepare_import.").WithCause(err)
	case errors.Is(err, ErrTooLarge):
		return (&httpx.Error{Status: http.StatusRequestEntityTooLarge, Code: "body_too_large",
			Message: "El archivo pesa más de 20 MB."}).WithCause(err)
	case errors.Is(err, ErrBodyUnreadable):
		return httpx.BadRequest("upload_unreadable",
			"El archivo no llegó completo. Mándalo otra vez a la misma dirección.").WithCause(err)
	case errors.Is(err, ErrSizeMismatch):
		return httpx.BadRequest("size_mismatch",
			"El archivo no pesa lo que se anunció en prepare_import. No se guardó nada.").WithCause(err)
	case errors.Is(err, ErrDigestMismatch):
		return httpx.BadRequest("sha256_mismatch",
			"El archivo no es el que se anunció en prepare_import: su SHA-256 es otro. No se guardó nada.").WithCause(err)
	case errors.Is(err, ErrNotFound):
		return httpx.NotFound("upload_not_found", "Ese archivo ya no está en chenchén.").WithCause(err)
	default:
		return err
	}
}
