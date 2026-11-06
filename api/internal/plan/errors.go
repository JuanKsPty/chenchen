package plan

import "errors"

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
)

// The errors about one field of an item or a cap. Each is told to the owner
// with its message in fieldErrors, all of them at once when an edit has
// several wrong.
var (
	ErrKind            = errors.New("plan: the kind is not income, fixed or saving")
	ErrPurchasesNotYet = errors.New("plan: planned purchases are not kept yet")
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
	// ErrStartMonth and ErrFunding are a planned purchase's fields, which no
	// item can have while purchases are not kept.
	ErrStartMonth = errors.New("plan: only a planned purchase starts being set aside in a month")
	ErrFunding    = errors.New("plan: only a planned purchase says how it is paid")
)

// fieldErrors are the errors about one request field, with what the owner is
// told about it. The messages never scold: they say what to do instead.
var fieldErrors = []struct {
	err            error
	field, message string
}{
	{ErrKind, "kind", "Elige si es un ingreso, un pago fijo o un ahorro."},
	{ErrPurchasesNotYet, "kind", "Las compras planeadas todavía no se pueden guardar."},
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
	{ErrStartMonth, "start_month", "Las compras planeadas todavía no se pueden guardar."},
	{ErrFunding, "funding", "Las compras planeadas todavía no se pueden guardar."},
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
