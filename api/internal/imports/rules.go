package imports

import (
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

// matchRules tries the user's rules against the bank's category and the
// line's description, the same text ApplyCategoryRule reads for a line already
// imported.
func matchRules(l statement.Line, rules []store.CategoryRule) store.RuleMatch {
	return store.MatchRule(l.BankCategory+" "+l.Description, rules)
}
