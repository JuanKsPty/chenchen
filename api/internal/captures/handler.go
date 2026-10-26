package captures

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/categories"
	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/httpx"
	"github.com/JuanKsPty/chenchen/api/internal/identity"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/movements"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// Handler serves /api/captures: the owner noting, correcting, pairing and
// reviewing their purchases, and making or revoking their shortcuts' keys, in
// their own session.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes returns the /api/captures subrouter. /keys is a fixed segment, so chi
// matches it before the {id} routes and a key's URL is never read as a
// purchase's.
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Post("/", h.add)
	r.Get("/review", h.review)
	r.Post("/keys/{accountID}", h.createKey)
	r.Delete("/keys/{accountID}", h.revokeKey)
	r.Patch("/{id}", h.edit)
	r.Post("/{id}/link", h.link)
	r.Delete("/{id}/link", h.unlink)
	r.Post("/{id}/reject", h.reject)
	r.Put("/{id}/discard", h.discard)
	r.Delete("/{id}/discard", h.restore)
	return r
}

type addRequest struct {
	// ID is optional: a form sends the same one on every try so a repeat
	// never notes the purchase twice.
	ID        uuid.UUID `json:"id"`
	AccountID uuid.UUID `json:"account_id"`
	// Amount is what was paid, positive: "12.50".
	Amount     money.Input `json:"amount"`
	Merchant   string      `json:"merchant"`
	Day        civil.Date  `json:"day"`
	CategoryID *uuid.UUID  `json:"category_id"`
	Note       string      `json:"note"`
}

func (h *Handler) add(w http.ResponseWriter, r *http.Request) {
	var body addRequest
	if err := httpx.Decode(w, r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// Every field the owner typed is checked before anything is written, so
	// the form can mark them all at once.
	problems := map[string]string{}
	if body.AccountID == uuid.Nil {
		problems["account_id"] = "Elige una cuenta."
	}
	amount, err := body.Amount.Cents()
	if err == nil {
		err = checkAmount(amount)
	}
	if err != nil {
		problems["amount"] = amountProblem(err)
	}
	h.check(problems, &body.Merchant, &body.Note, &body.Day)
	if len(problems) > 0 {
		httpx.Fail(w, r, httpx.Invalid(problems))
		return
	}
	c, err := h.svc.Add(r.Context(), identity.MustFromContext(r.Context()), AddInput{
		ID: body.ID, AccountID: body.AccountID, Amount: amount, Merchant: body.Merchant, Day: body.Day,
		CategoryID: body.CategoryID, Note: body.Note,
	})
	if err != nil {
		httpx.Fail(w, r, TranslateError(err))
		return
	}
	httpx.JSON(w, r, http.StatusCreated, NewView(c))
}

// editRequest corrects a purchase; an absent or null field stays as it is.
type editRequest struct {
	Amount   *money.Input `json:"amount"`
	Merchant *string      `json:"merchant"`
	Day      *civil.Date  `json:"day"`
}

func (h *Handler) edit(w http.ResponseWriter, r *http.Request) {
	var body editRequest
	if err := httpx.Decode(w, r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	id, err := captureID(r)
	if err != nil {
		httpx.Fail(w, r, TranslateError(err))
		return
	}
	in := EditInput{Merchant: body.Merchant, Day: body.Day}
	problems := map[string]string{}
	if body.Amount != nil {
		amount, err := body.Amount.Cents()
		if err == nil {
			err = checkAmount(amount)
		}
		if err != nil {
			problems["amount"] = amountProblem(err)
		}
		in.Amount = &amount
	}
	h.check(problems, body.Merchant, nil, body.Day)
	if len(problems) > 0 {
		httpx.Fail(w, r, httpx.Invalid(problems))
		return
	}
	c, err := h.svc.Edit(r.Context(), identity.MustFromContext(r.Context()), id, in)
	if err != nil {
		httpx.Fail(w, r, TranslateError(err))
		return
	}
	httpx.JSON(w, r, http.StatusOK, NewView(c))
}

// check adds to problems what is wrong with whichever of a typed merchant,
// note and day are given, by the same rules the service applies.
func (h *Handler) check(problems map[string]string, merchant, note *string, day *civil.Date) {
	var errs []error
	if merchant != nil {
		_, err := cleanMerchant(*merchant)
		errs = append(errs, err)
	}
	if note != nil {
		_, err := cleanNote(*note)
		errs = append(errs, err)
	}
	if day != nil && !day.IsZero() {
		errs = append(errs, h.svc.checkDay(*day))
	}
	for _, err := range errs {
		if field, message, ok := fieldProblem(err); ok {
			problems[field] = message
		}
	}
}

// lineRequest names a printed line.
type lineRequest struct {
	EntryID uuid.UUID `json:"entry_id"`
}

func (h *Handler) link(w http.ResponseWriter, r *http.Request) {
	h.withLine(w, r, h.svc.Link)
}

func (h *Handler) reject(w http.ResponseWriter, r *http.Request) {
	h.withLine(w, r, h.svc.Reject)
}

func (h *Handler) withLine(w http.ResponseWriter, r *http.Request,
	act func(ctx context.Context, userID, id, entryID uuid.UUID) (Capture, error)) {
	var body lineRequest
	if err := httpx.Decode(w, r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if body.EntryID == uuid.Nil {
		httpx.Fail(w, r, httpx.Invalid(map[string]string{"entry_id": "Elige una línea de tu estado."}))
		return
	}
	id, err := captureID(r)
	if err != nil {
		httpx.Fail(w, r, TranslateError(err))
		return
	}
	c, err := act(r.Context(), identity.MustFromContext(r.Context()), id, body.EntryID)
	if err != nil {
		httpx.Fail(w, r, TranslateError(err))
		return
	}
	httpx.JSON(w, r, http.StatusOK, NewView(c))
}

func (h *Handler) unlink(w http.ResponseWriter, r *http.Request)  { h.act(w, r, h.svc.Unlink) }
func (h *Handler) discard(w http.ResponseWriter, r *http.Request) { h.act(w, r, h.svc.Discard) }
func (h *Handler) restore(w http.ResponseWriter, r *http.Request) { h.act(w, r, h.svc.Restore) }

func (h *Handler) act(w http.ResponseWriter, r *http.Request,
	act func(ctx context.Context, userID, id uuid.UUID) (Capture, error)) {
	id, err := captureID(r)
	if err != nil {
		httpx.Fail(w, r, TranslateError(err))
		return
	}
	c, err := act(r.Context(), identity.MustFromContext(r.Context()), id)
	if err != nil {
		httpx.Fail(w, r, TranslateError(err))
		return
	}
	httpx.JSON(w, r, http.StatusOK, NewView(c))
}

func (h *Handler) review(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.Review(r.Context(), identity.MustFromContext(r.Context()))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out := make([]ReviewItemView, 0, len(list))
	for _, item := range list {
		out = append(out, NewReviewItemView(item))
	}
	httpx.JSON(w, r, http.StatusOK, map[string]any{"captures": out})
}

// captureID reads the {id} URL parameter; a malformed id is a 404 like a
// missing one.
func captureID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: %q", ErrNotFound, chi.URLParam(r, "id"))
	}
	return id, nil
}

func amountProblem(err error) string {
	switch {
	case errors.Is(err, money.ErrEmpty):
		return "Escribe el monto."
	case errors.Is(err, money.ErrNotPositive):
		return "El monto debe ser mayor que cero."
	case errors.Is(err, money.ErrTooPrecise):
		return "El monto admite como máximo dos decimales."
	case errors.Is(err, ErrAmountTooLarge), errors.Is(err, money.ErrOutOfRange):
		return fmt.Sprintf("El monto no puede pasar de %s.", maxAmount.Display())
	default:
		return "El monto no tiene un formato válido. Usa por ejemplo «12.50»."
	}
}

// fieldProblem names the field a validation error is about and what to tell
// the owner; ok is false for any other error.
func fieldProblem(err error) (field, message string, ok bool) {
	switch {
	case err == nil:
		return "", "", false
	case errors.Is(err, ErrMerchantRequired):
		return "merchant", "Escribe dónde fue la compra.", true
	case errors.Is(err, ErrMerchantTooLong):
		return "merchant", fmt.Sprintf("El comercio admite como máximo %d caracteres.", maxMerchantRunes), true
	case errors.Is(err, ErrNoteTooLong):
		return "note", fmt.Sprintf("La nota admite como máximo %d caracteres.", maxNoteRunes), true
	case errors.Is(err, ErrFutureDay):
		return "day", "El día no puede ser después de hoy.", true
	case errors.Is(err, money.ErrNotPositive), errors.Is(err, ErrAmountTooLarge):
		return "amount", amountProblem(err), true
	}
	return "", "", false
}

// TranslateError maps this package's errors to HTTP responses.
func TranslateError(err error) error {
	if field, message, ok := fieldProblem(err); ok {
		return httpx.Invalid(map[string]string{field: message}).WithCause(err)
	}
	switch {
	case errors.Is(err, ErrNotFound):
		return httpx.NotFound("capture_not_found", "Esa compra no existe.").WithCause(err)
	case errors.Is(err, ErrAccountNotFound):
		return httpx.NotFound("account_not_found", "La cuenta no existe.").WithCause(err)
	case errors.Is(err, ErrBrokerage):
		return httpx.Unprocessable("brokerage_account", msgBrokerage).WithCause(err)
	case errors.Is(err, ErrLinked):
		return httpx.Conflict("capture_linked",
			"Esta compra ya está unida a una línea de tu estado.").WithCause(err)
	case errors.Is(err, ErrDiscarded):
		return httpx.Conflict("capture_discarded",
			"Esta compra está descartada. Restáurala primero.").WithCause(err)
	case errors.Is(err, ErrNotLinked):
		return httpx.Conflict("capture_not_linked",
			"Esta compra no está unida a ninguna línea.").WithCause(err)
	case errors.Is(err, ErrLineNotFound):
		return httpx.NotFound("entry_not_found", "Esa línea no existe.").WithCause(err)
	case errors.Is(err, ErrOtherAccount):
		return httpx.Unprocessable("other_account", "Esa línea es de otra cuenta.").WithCause(err)
	case errors.Is(err, ErrNotAPurchase):
		return httpx.Unprocessable("not_a_purchase",
			"Esa línea no es un gasto: no puede ser esta compra.").WithCause(err)
	case errors.Is(err, ErrLineTaken):
		return httpx.Conflict("line_taken", "Esa línea ya está unida a otra compra.").WithCause(err)
	case errors.Is(err, ErrIDTaken):
		return httpx.Conflict("capture_id_taken", "Esa compra ya existe. Ábrela de nuevo y anótala otra vez.").WithCause(err)
	default:
		return categories.TranslateError(err)
	}
}

// --- response shapes --------------------------------------------------------

// View is a noted purchase on the wire: the movement it reads as, with origin
// "capture"; what it is now (pending, printed, missing, linked or discarded;
// only pending counts as spending); and, once linked, its line.
type View struct {
	movements.View
	State    string          `json:"state"`
	LinkedBy string          `json:"linked_by,omitempty"`
	Line     *movements.View `json:"line,omitempty"`
}

// NewView builds a noted purchase's wire shape.
func NewView(c Capture) View {
	v := View{View: movements.NewView(c.AsEntry()), State: c.State}
	if c.LinkedBy != nil {
		v.LinkedBy = *c.LinkedBy
	}
	if c.Line != nil {
		line := movements.NewView(*c.Line)
		v.Line = &line
	}
	return v
}

// ReviewItemView is a purchase that needs the owner, with the printed lines
// it could be (candidates, for a printed one) or that come close to it (near,
// for a missing one). Both are always present, empty when there are none.
type ReviewItemView struct {
	View
	Candidates []movements.View `json:"candidates"`
	Near       []movements.View `json:"near"`
}

// NewReviewItemView builds a review item's wire shape.
func NewReviewItemView(r store.CaptureReview) ReviewItemView {
	return ReviewItemView{View: NewView(Capture{Capture: r.Capture}),
		Candidates: movements.NewViews(r.Candidates), Near: movements.NewViews(r.Near)}
}
