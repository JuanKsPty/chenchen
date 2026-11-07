package plan

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/accounts"
	"github.com/JuanKsPty/chenchen/api/internal/categories"
	"github.com/JuanKsPty/chenchen/api/internal/httpx"
	"github.com/JuanKsPty/chenchen/api/internal/identity"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/movements"
)

// Handler serves /api/plan.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes returns the /api/plan subrouter.
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/", h.month)
	r.Put("/items/{id}", h.saveItem)
	r.Delete("/items/{id}", h.deleteItem)
	r.Put("/caps/{category_id}", h.saveCap)
	r.Delete("/caps/{category_id}", h.deleteCap)
	r.Post("/links", h.link)
	r.Delete("/links/{id}", h.unlink)
	return r
}

// month answers GET /api/plan?month=YYYY-MM: that month's plan beside what
// happened in it. With no month, the current one in Panama.
func (h *Handler) month(w http.ResponseWriter, r *http.Request) {
	var m Month
	if raw := r.URL.Query().Get("month"); raw != "" {
		var err error
		if m, err = ParseMonth(raw); err != nil {
			httpx.Fail(w, r, translate(err))
			return
		}
	}
	p, err := h.svc.Month(r.Context(), identity.MustFromContext(r.Context()), m)
	if err != nil {
		httpx.Fail(w, r, translate(err))
		return
	}
	httpx.JSON(w, r, http.StatusOK, NewView(p))
}

// itemRequest is an item as a form saves it. Amounts arrive as text and
// months as YYYY-MM.
type itemRequest struct {
	Kind        string       `json:"kind"`
	Month       string       `json:"month"`
	Scope       string       `json:"scope"`
	Name        string       `json:"name"`
	Amount      money.Input  `json:"amount"`
	Day         int          `json:"day"`
	MatchText   string       `json:"match_text"`
	CategoryID  *uuid.UUID   `json:"category_id"`
	AccountID   *uuid.UUID   `json:"account_id"`
	Target      *money.Input `json:"target"`
	TargetMonth string       `json:"target_month"`
	StartMonth  string       `json:"start_month"`
	Funding     string       `json:"funding"`
}

// saveItem answers PUT /api/plan/items/{id}: it creates the item with the
// caller's id (201) or edits the owner's own (200), and answers with the
// month it was saved for. Sending the same thing again changes nothing, which
// is also how an edit is undone.
func (h *Handler) saveItem(w http.ResponseWriter, r *http.Request) {
	var body itemRequest
	if err := httpx.Decode(w, r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Fail(w, r, translate(ErrItemNotFound))
		return
	}
	in := ItemInput{Kind: body.Kind, Scope: Scope(body.Scope), Name: body.Name, Day: body.Day,
		MatchText: body.MatchText, CategoryID: body.CategoryID, AccountID: body.AccountID, Funding: body.Funding}
	in.Month, err = ParseMonth(body.Month)
	if err == nil {
		in.TargetMonth, err = optionalMonth(body.TargetMonth)
	}
	if err == nil {
		in.StartMonth, err = optionalMonth(body.StartMonth)
	}
	if err != nil {
		httpx.Fail(w, r, translate(err))
		return
	}
	// An amount that cannot be read reaches the service as zero, which it
	// refuses with everything else that is wrong, so nothing is written; what
	// the owner is told about the amount is then what the handler knows of it.
	unread := map[string]string{}
	if in.Amount, err = body.Amount.Cents(); err != nil {
		unread["amount"] = amountProblem(err, "Indica un monto mayor que cero.")
	}
	// A goal left blank is no goal, as a card's minimum payment left blank is
	// none: a saving may have no goal, and a form sends its empty field.
	if body.Target != nil && *body.Target != "" {
		target, err := body.Target.Cents()
		if err != nil {
			unread["target"] = amountProblem(err, "Indica una meta mayor que cero.")
		}
		in.Target = &target
	}
	user := identity.MustFromContext(r.Context())
	_, created, err := h.svc.SaveItem(r.Context(), user, id, in)
	if err != nil {
		httpx.Fail(w, r, translateWith(err, unread))
		return
	}
	saved, err := h.svc.Item(r.Context(), user, id, in.Month)
	if err != nil {
		httpx.Fail(w, r, translate(err))
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.JSON(w, r, status, NewItemView(saved))
}

// deleteItem answers DELETE /api/plan/items/{id}?month=YYYY-MM&scope=from|only.
func (h *Handler) deleteItem(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Fail(w, r, translate(ErrItemNotFound))
		return
	}
	m, scope, err := monthAndScope(r)
	if err == nil {
		err = h.svc.DeleteItem(r.Context(), identity.MustFromContext(r.Context()), id, m, scope)
	}
	if err != nil {
		httpx.Fail(w, r, translate(err))
		return
	}
	httpx.NoContent(w)
}

type capRequest struct {
	Month  string      `json:"month"`
	Scope  string      `json:"scope"`
	Amount money.Input `json:"amount"`
}

// saveCap answers PUT /api/plan/caps/{category_id} with the cap that holds
// for the month it was saved for.
func (h *Handler) saveCap(w http.ResponseWriter, r *http.Request) {
	var body capRequest
	if err := httpx.Decode(w, r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	categoryID, err := uuid.Parse(chi.URLParam(r, "category_id"))
	if err != nil {
		httpx.Fail(w, r, translate(categories.ErrNotFound))
		return
	}
	m, err := ParseMonth(body.Month)
	if err != nil {
		httpx.Fail(w, r, translate(err))
		return
	}
	in := CapInput{Month: m, Scope: Scope(body.Scope)}
	// As for an item, an amount that cannot be read reaches the service as
	// zero.
	unread := map[string]string{}
	if in.Amount, err = body.Amount.Cents(); err != nil {
		unread["amount"] = amountProblem(err, "Indica un monto mayor que cero.")
	}
	user := identity.MustFromContext(r.Context())
	if err := h.svc.SaveCap(r.Context(), user, categoryID, in); err != nil {
		httpx.Fail(w, r, translateWith(err, unread))
		return
	}
	saved, err := h.svc.Cap(r.Context(), user, categoryID, m)
	if err != nil {
		httpx.Fail(w, r, translate(err))
		return
	}
	httpx.JSON(w, r, http.StatusOK, NewCapView(saved))
}

// deleteCap answers DELETE /api/plan/caps/{category_id}?month=YYYY-MM&scope=from|only.
func (h *Handler) deleteCap(w http.ResponseWriter, r *http.Request) {
	categoryID, err := uuid.Parse(chi.URLParam(r, "category_id"))
	if err != nil {
		httpx.Fail(w, r, translate(categories.ErrNotFound))
		return
	}
	m, scope, err := monthAndScope(r)
	if err == nil {
		err = h.svc.DeleteCap(r.Context(), identity.MustFromContext(r.Context()), categoryID, m, scope)
	}
	if err != nil {
		httpx.Fail(w, r, translate(err))
		return
	}
	httpx.NoContent(w)
}

// linkRequest is what the owner says about one month of an item: that a
// movement paid it, that one did not, or that it was paid outside. The id is
// the caller's, so sending it again is safe.
type linkRequest struct {
	ID         uuid.UUID  `json:"id"`
	ItemID     uuid.UUID  `json:"item_id"`
	Month      string     `json:"month"`
	MovementID *uuid.UUID `json:"movement_id"`
	Decision   string     `json:"decision"`
}

// link answers POST /api/plan/links: 201 with the link it records, or 200
// with the one already there when it was said before, by its id or about the
// same movement and month.
func (h *Handler) link(w http.ResponseWriter, r *http.Request) {
	var body linkRequest
	if err := httpx.Decode(w, r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	m, err := ParseMonth(body.Month)
	if err != nil {
		httpx.Fail(w, r, translate(err))
		return
	}
	in := LinkInput{ID: body.ID, ItemID: body.ItemID, Month: m, Decision: Decision(body.Decision)}
	if body.MovementID != nil {
		in.MovementID = *body.MovementID
	}
	link, created, err := h.svc.Link(r.Context(), identity.MustFromContext(r.Context()), in)
	if err != nil {
		httpx.Fail(w, r, translate(err))
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.JSON(w, r, status, NewLinkView(link))
}

// unlink answers DELETE /api/plan/links/{id}: what the link said is undone.
func (h *Handler) unlink(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Fail(w, r, translate(ErrLinkNotFound))
		return
	}
	if err := h.svc.Unlink(r.Context(), identity.MustFromContext(r.Context()), id); err != nil {
		httpx.Fail(w, r, translate(err))
		return
	}
	httpx.NoContent(w)
}

// monthAndScope reads ?month=YYYY-MM&scope=from|only, which a removal needs
// both of.
func monthAndScope(r *http.Request) (Month, Scope, error) {
	m, err := ParseMonth(r.URL.Query().Get("month"))
	if err != nil {
		return Month{}, "", err
	}
	scope, err := ParseScope(r.URL.Query().Get("scope"))
	if err != nil {
		return Month{}, "", err
	}
	return m, scope, nil
}

// optionalMonth reads a month a request may leave out: "" is no month.
func optionalMonth(raw string) (Month, error) {
	if raw == "" {
		return Month{}, nil
	}
	return ParseMonth(raw)
}

// amountProblem is what the owner is told about an amount that cannot be
// read; missing is what they are told when there is none, or it is not more
// than zero.
func amountProblem(err error, missing string) string {
	switch {
	case errors.Is(err, money.ErrEmpty), errors.Is(err, money.ErrNotPositive):
		return missing
	case errors.Is(err, money.ErrTooPrecise):
		return "El monto admite como máximo dos decimales."
	case errors.Is(err, money.ErrOutOfRange):
		return "El monto está fuera del rango admitido."
	default:
		return "El monto no tiene un formato válido. Usa por ejemplo «12.50»."
	}
}

// translateWith is translate, with the handler's own word on the amounts it
// could not read. Such an amount reached the service as zero, so the
// handler's word replaces the service's only where the service refused it
// for not being more than zero; anything else it said about the field, such
// as that only a saving has a goal, is what the owner needs to hear.
func translateWith(err error, unread map[string]string) error {
	problems := FieldProblems(err)
	if len(problems) == 0 {
		return translate(err)
	}
	for field, message := range unread {
		if zero := notMoreThanZero[field]; zero != nil && errors.Is(err, zero) {
			problems[field] = message
		}
	}
	return httpx.Invalid(problems).WithCause(err)
}

// notMoreThanZero is the error the service gives each amount field when it
// is zero, which is what an amount the handler could not read becomes.
var notMoreThanZero = map[string]error{"amount": ErrAmount, "target": ErrTarget}

// translate maps the plan's errors to HTTP responses. Something of another
// owner's (an item, a link, a movement, a category, an account) is not
// found, never forbidden, so an id cannot be probed.
func translate(err error) error {
	if problems := FieldProblems(err); len(problems) > 0 {
		return httpx.Invalid(problems).WithCause(err)
	}
	switch {
	case errors.Is(err, ErrInvalidMonth):
		return httpx.BadRequest("invalid_month", "Revisa el mes.").WithCause(err)
	case errors.Is(err, ErrMonthPast):
		return httpx.Unprocessable("plan_month_past", "Ese mes ya pasó; su plan queda como estaba.").WithCause(err)
	case errors.Is(err, ErrMonthTooFar):
		return httpx.Unprocessable("plan_month_too_far", "Puedes planear hasta 24 meses adelante.").WithCause(err)
	case errors.Is(err, ErrItemNotFound):
		return httpx.NotFound("plan_item_not_found", "Eso ya no está en tu plan.").WithCause(err)
	case errors.Is(err, ErrItemIDTaken):
		return httpx.Conflict("plan_item_id_taken", "No pudimos guardarlo así. Intenta otra vez.").WithCause(err)
	case errors.Is(err, ErrKindFixed):
		return httpx.Unprocessable("plan_kind_fixed",
			"Lo que ya está en tu plan no cambia de tipo; agrégalo como algo nuevo.").WithCause(err)
	case errors.Is(err, ErrAccountHasGoal):
		return httpx.Conflict("account_has_goal", "Esa cuenta ya tiene una meta.").WithCause(err)
	case errors.Is(err, ErrLinkNotFound):
		return httpx.NotFound("plan_link_not_found", "Eso ya se había deshecho.").WithCause(err)
	case errors.Is(err, ErrLinkIDTaken):
		return httpx.Conflict("plan_link_id_taken", "No pudimos guardarlo así. Intenta otra vez.").WithCause(err)
	case errors.Is(err, ErrMovementInPlan):
		return httpx.Conflict("movement_in_plan", "Ese movimiento ya está en tu plan.").WithCause(err)
	case errors.Is(err, ErrMovementTooFar):
		return httpx.Unprocessable("movement_too_far",
			"Elige un movimiento de ese mes o de uno de al lado.").WithCause(err)
	case errors.Is(err, movements.ErrNotFound):
		return httpx.NotFound("entry_not_found", "El movimiento no existe.").WithCause(err)
	case errors.Is(err, accounts.ErrNotFound):
		return httpx.NotFound("account_not_found", "La cuenta no existe.").WithCause(err)
	case errors.Is(err, categories.ErrNotFound), errors.Is(err, categories.ErrNotOwned):
		return categories.TranslateError(err)
	default:
		return err
	}
}
