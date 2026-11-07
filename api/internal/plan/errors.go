package plan

import (
	"errors"

	"github.com/JuanKsPty/chenchen/api/internal/money"
)

var (
	// ErrMonthPast means a write was for a month before the current one. A
	// past month keeps the plan it had.
	ErrMonthPast = errors.New("plan: that month has passed; its plan stays as it was")
	// ErrMonthTooFar means a write was for a month more than MonthsAhead
	// after the current one.
	ErrMonthTooFar = errors.New("plan: that month is too far ahead")
	// ErrItemNotFound means no such item belongs to the caller. One that is
	// someone else's is reported the same way, so an id cannot be probed.
	ErrItemNotFound = errors.New("plan: item not found")
	// ErrItemIDTaken means the id a caller chose for a new item is someone
	// else's item.
	ErrItemIDTaken = errors.New("plan: the id belongs to another item")
	// ErrKindFixed means an edit gave an item another kind. A paycheck never
	// becomes a payment: its months and links mean something else.
	ErrKindFixed = errors.New("plan: an item's kind never changes")
	// ErrAccountHasGoal means the account already backs another saving in
	// one of the months this one would hold for. Two goals on one account in
	// the same month would have to split what reaches it, and nothing says
	// how; one after the other, they never have to.
	ErrAccountHasGoal = errors.New("plan: the account already backs a goal")
	// ErrLinkNotFound means no such link belongs to the caller: it was
	// undone already, or went with its movement or the months of its item.
	ErrLinkNotFound = errors.New("plan: link not found")
	// ErrLinkIDTaken means the id a caller chose for a new link is someone
	// else's link.
	ErrLinkIDTaken = errors.New("plan: the id belongs to another link")
	// ErrMovementInPlan means the owner already said the movement paid
	// another month or another item. A movement pays one at a time; the
	// other link has to go first.
	ErrMovementInPlan = errors.New("plan: the movement already pays something in the plan")
	// ErrMovementTooFar means the movement is booked more than linkMonths
	// from the month it would pay. A payment a few days late lands in the
	// month after; one further away is something else.
	ErrMovementTooFar = errors.New("plan: the movement is booked too far from that month")
)

// The errors about one field of an item or a cap. Each is told to the owner
// with its message in fieldErrors, all of them at once when an edit has
// several wrong.
var (
	// ErrKind is a dismissal's: only a paycheck, a payment or a saving is
	// ever suggested. ErrItemKind is an item's, which may also be a purchase.
	ErrKind            = errors.New("plan: the kind is not income, fixed or saving")
	ErrItemKind        = errors.New("plan: the kind is not income, fixed, saving or purchase")
	ErrName            = errors.New("plan: a name has 1 to 60 characters")
	ErrNameInvalid     = errors.New("plan: the name has characters that are not allowed")
	ErrAmount          = errors.New("plan: the amount must be more than zero")
	ErrDay             = errors.New("plan: the day is 1 to 31, and a paycheck or payment needs one")
	ErrMatchText       = errors.New("plan: the text to recognise it by has up to 60 characters")
	ErrExpenseCategory = errors.New("plan: a payment or a cap needs an expense category")
	ErrIncomeCategory  = errors.New("plan: a paycheck needs an income category")
	ErrSavingCategory  = errors.New("plan: a saving has no category")
	ErrSavingMatchText = errors.New("plan: a saving is recognised by its account, not by text")
	ErrGoalAccount     = errors.New("plan: a saving's account is a savings or brokerage account")
	ErrAccountNotGoal  = errors.New("plan: only a saving is tied to an account")
	ErrTarget          = errors.New("plan: a goal must be more than zero")
	ErrTargetNotGoal   = errors.New("plan: only a saving has a goal")

	// A planned purchase's fields. Its price is the amount; the month it is
	// first planned for is when it starts being set aside, which the plan
	// keeps, so no one sends it.
	// ErrTargetMonth is a purchase with no month to buy it in, or one more
	// than MonthsAhead after the current month; ErrTargetMonthEarly one
	// before the month edited, which is not always the current one.
	ErrTargetMonth        = errors.New("plan: a purchase is bought by a month no more than MonthsAhead after the current one")
	ErrTargetMonthEarly   = errors.New("plan: a purchase is bought in the month edited or a later one")
	ErrTargetMonthNotGoal = errors.New("plan: only a saving or a purchase has a month to reach")
	ErrFunding            = errors.New("plan: a purchase is set aside monthly, in its month or not at all")
	ErrFundingNotPurchase = errors.New("plan: only a planned purchase says how it is set aside")
	ErrStartMonth         = errors.New("plan: a purchase starts being set aside in the month it is planned for")
	ErrPurchaseScope      = errors.New("plan: a purchase is set aside from a month on, never for one month alone")
	ErrPurchaseDay        = errors.New("plan: a purchase has no day")

	// The fields of a link.
	ErrDecision     = errors.New("plan: a link says assign, reject or paid")
	ErrLinkMovement = errors.New("plan: a link that assigns or rejects names a movement")
	ErrPaidMovement = errors.New("plan: a month paid outside names no movement")
	ErrLinkItemKind = errors.New("plan: only a paycheck, a payment or a purchase is paid by a movement")
	ErrPaidPurchase = errors.New("plan: a purchase is bought by a movement, never marked bought")
	ErrNotIncome    = errors.New("plan: a paycheck is paid by income")
	ErrNotSpending  = errors.New("plan: a payment is paid by spending")

	// ErrSuggestionKey is a dismissal's: it names a suggestion by its key, of
	// 1 to 120 characters, as the suggestions gave it.
	ErrSuggestionKey = errors.New("plan: a suggestion is named by its key, of 1 to 120 characters")
)

// fieldErrors are the errors about one request field, with what the owner is
// told about it. The messages never scold: they say what to do instead.
var fieldErrors = []struct {
	err            error
	field, message string
}{
	{ErrKind, "kind", "Elige si es un ingreso, un pago fijo o un ahorro."},
	{ErrItemKind, "kind", "Elige si es un ingreso, un pago fijo, un ahorro o una compra."},
	{ErrInvalidScope, "scope", "Elige si vale desde este mes o solo para este mes."},
	{ErrName, "name", "Ponle un nombre de hasta 60 caracteres."},
	{ErrNameInvalid, "name", "El nombre tiene caracteres que no se pueden mostrar."},
	{ErrAmount, "amount", "Indica un monto mayor que cero."},
	{ErrDay, "day", "Elige un día del 1 al 31."},
	{ErrMatchText, "match_text", "El texto para reconocerlo admite hasta 60 caracteres."},
	{ErrExpenseCategory, "category_id", "Elige una categoría de gastos."},
	{ErrIncomeCategory, "category_id", "Elige una categoría de ingresos."},
	{ErrSavingCategory, "category_id", "Un ahorro no lleva categoría."},
	{ErrSavingMatchText, "match_text", "Un ahorro se reconoce por su cuenta, no por un texto."},
	{ErrGoalAccount, "account_id", "Elige una cuenta de ahorro o de inversión."},
	{ErrAccountNotGoal, "account_id", "Solo un ahorro va ligado a una cuenta."},
	{ErrTarget, "target", "Indica una meta mayor que cero."},
	{ErrTargetNotGoal, "target", "Solo un ahorro tiene una meta."},
	{ErrTargetMonth, "target_month", "Elige para cuándo, de aquí a 24 meses como mucho."},
	{ErrTargetMonthEarly, "target_month", "Elige el mes que estás planeando o uno después."},
	{ErrTargetMonthNotGoal, "target_month", "Solo un ahorro o una compra tienen fecha."},
	{ErrFunding, "funding", "Elige cómo la apartas: un poco cada mes, todo ese mes o con tus ahorros."},
	{ErrFundingNotPurchase, "funding", "Solo una compra planeada dice cómo se aparta."},
	{ErrStartMonth, "start_month", "Una compra se empieza a apartar el mes en que la planeas."},
	{ErrPurchaseScope, "scope", "Una compra se aparta desde este mes hasta el de la compra."},
	{ErrPurchaseDay, "day", "Una compra planeada no lleva día."},
	{ErrDecision, "decision", "Elige si fue este movimiento, si no lo fue o si se pagó por otro lado."},
	{ErrLinkMovement, "movement_id", "Elige el movimiento."},
	{ErrPaidMovement, "movement_id", "Si se pagó por otro lado, no elijas un movimiento."},
	{ErrLinkItemKind, "item_id", "Solo un ingreso, un pago fijo o una compra se reconocen por un movimiento."},
	{ErrPaidPurchase, "decision", "Elige el movimiento con que la compraste."},
	{ErrNotIncome, "movement_id", "Elige un ingreso: ese movimiento no es un ingreso."},
	{ErrNotSpending, "movement_id", "Elige un gasto: una transferencia o un ingreso no es este pago."},
	{ErrSuggestionKey, "key", "Elige una de las sugerencias."},
}

// FieldProblems is what err says about each request field, every field at
// once when err joins several, for the handler and Claude's tools to tell the
// owner. It is empty when err is about no field.
func FieldProblems(err error) map[string]string {
	problems := map[string]string{}
	for _, f := range fieldErrors {
		if _, seen := problems[f.field]; !seen && errors.Is(err, f.err) {
			problems[f.field] = f.message
		}
	}
	return problems
}

// FieldProblemsWith is FieldProblems, with the caller's own word on the
// amounts it could not read, by field (AmountProblem). Such an amount reached
// the service as zero, so the caller's word replaces the service's only where
// the service refused it for not being more than zero; anything else it said
// about the field, such as that only a saving has a goal, is what the owner
// needs to hear.
func FieldProblemsWith(err error, unread map[string]string) map[string]string {
	problems := FieldProblems(err)
	if len(problems) == 0 {
		return problems
	}
	for field, message := range unread {
		if zero := notMoreThanZero[field]; zero != nil && errors.Is(err, zero) {
			problems[field] = message
		}
	}
	return problems
}

// notMoreThanZero is the error the service gives each amount field when it
// is zero, which is what an amount the caller could not read becomes.
var notMoreThanZero = map[string]error{"amount": ErrAmount, "target": ErrTarget}

// AmountProblem is what the owner is told about an amount that cannot be
// read; missing is what they are told when there is none, or it is not more
// than zero.
func AmountProblem(err error, missing string) string {
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
