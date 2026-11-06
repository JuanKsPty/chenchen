package imports

import (
	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/statement"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// defaultCardRules maps the card statement's own categories onto the default
// categories every user starts with, so a first card import is already mostly
// categorised. "Pagos" and "Cargos" are absent on purpose: a payment is a
// transfer and a charge is the bank's fee, which the importer's kind already
// says; neither is a spending category. "Viajes" has no honest default either.
var defaultCardRules = map[string]string{
	"Comida y Bebida":     "Alimentación",
	"Supermercados":       "Alimentación",
	"Salud":               "Salud",
	"Transporte":          "Transporte",
	"Entretenimiento":     "Entretenimiento",
	"Tiendas y almacenes": "Compras",
}

// filing is how a new line finds its category: the user's rules first, then,
// for a charge no rule placed, the category charges fall to (fees, when the
// user has one).
type filing struct {
	rules []store.CategoryRule
	fees  *uuid.UUID
}

// match tries the user's rules against the bank's category and the line's
// description, the same text ApplyCategoryRule reads for a line already
// imported. kind is what the line is before any rule: a rule that makes a
// charge something else (a transfer) also keeps it out of fees.
func (f filing) match(l statement.Line, kind statement.Kind) store.RuleMatch {
	m := store.MatchRule(l.BankCategory+" "+l.Description, f.rules)
	if m.SetKind != nil {
		kind = statement.Kind(*m.SetKind)
	}
	if m.CategoryID == nil && f.fees != nil && l.Amount < 0 &&
		(kind == statement.KindFee || kind == statement.KindInterest) {
		m.CategoryID = f.fees
	}
	return m
}
