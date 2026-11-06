package categories

// defaultSeed is one of the categories every new customer starts with.
type defaultSeed struct {
	name string
	kind Kind
	// fallbackFor marks the category that receives what no rule placed:
	// "fee" for a bank's or a broker's own charges.
	fallbackFor string
}

// defaultSeeds is what SeedDefaults inserts for a newly registered customer.
//
// A flat, opinionated starting set rather than an empty list: a customer who
// has never used the app should be able to categorise their first
// transaction immediately, not be sent to a settings page first. Every entry
// here is a system category (deletable, but not the thing "Sin categorizar"
// falls back to being confused with).
var defaultSeeds = []defaultSeed{
	{"Salario", KindIncome, ""},
	{"Freelance", KindIncome, ""},
	{"Inversiones", KindIncome, ""},
	{"Otros ingresos", KindIncome, ""},

	{"Alimentación", KindExpense, ""},
	{"Transporte", KindExpense, ""},
	{"Vivienda", KindExpense, ""},
	{"Servicios", KindExpense, ""},
	{"Salud", KindExpense, ""},
	{"Entretenimiento", KindExpense, ""},
	{"Compras", KindExpense, ""},
	{"Educación", KindExpense, ""},
	{"Suscripciones", KindExpense, ""},
	// What a bank or a broker charges: the importer files a charge here when
	// no rule of the owner's says otherwise.
	{"Comisiones", KindExpense, "fee"},
	// Cash taken out at an ATM: spent, on something no statement will name.
	{"Efectivo", KindExpense, ""},
	{"Otros gastos", KindExpense, ""},
}
