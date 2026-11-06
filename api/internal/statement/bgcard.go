package statement

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strings"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
)

var bgCardColumns = []string{
	"Cuenta", "Tarjeta", "Fecha tran", "Fecha proceso", "Descripcion",
	"Referencia", "Categoria", "Cargos (Db)", "Pagos (Cr)",
}

// parseBGCard reads a Banco General credit card export: semicolon-separated,
// one line per movement, several physical cards possibly sharing one account.
//
// The file prints no balance at all — no previous balance, no closing, no
// credit limit — so its statements carry no Opening or Closing. A card's
// balance comes from its closed statement's PDF (parseBGCardStatement), or
// from the owner.
func parseBGCard(data []byte) (File, error) {
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
	r.Comma = ';'
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	records, err := r.ReadAll()
	if err != nil {
		return File{}, fmt.Errorf("statement: reading the BG card file: %w", err)
	}
	if len(records) == 0 {
		return File{}, fmt.Errorf("statement: the BG card file is empty")
	}
	t := bgCardTable{
		col:       map[string]int{},
		rows:      records[1:],
		firstLine: 2,
		date:      func(s string) (civil.Date, error) { return civil.ParseLayout("02/01/2006", s) },
		amount:    signedFromText,
	}
	for i, name := range records[0] {
		t.col[strings.TrimSpace(name)] = i
	}
	return t.file()
}

// bgCardTable is a card export read into cells, under the text export's
// column names, whichever file it came in. The text and Excel exports of one
// movement become the same Line through it, so its dedup key is the same and
// downloading it both ways never counts it twice.
type bgCardTable struct {
	// col maps each of bgCardColumns to its index in a row.
	col  map[string]int
	rows [][]string
	// firstLine is the file's own line or row number of rows[0], for
	// messages that point back at it.
	firstLine int
	// date and amount read a cell as the file writes it.
	date   func(string) (civil.Date, error)
	amount func(string) (money.Cents, error)
}

// file builds one statement per card account from the table's rows.
func (t bgCardTable) file() (File, error) {
	for _, want := range bgCardColumns {
		if _, ok := t.col[want]; !ok {
			return File{}, fmt.Errorf("statement: the BG card file is missing the %q column", want)
		}
	}

	byAccount := map[string]*Statement{}
	var order []string
	for i, record := range t.rows {
		lineNo := t.firstLine + i
		cell := func(name string) string {
			if c := t.col[name]; c < len(record) {
				return strings.TrimSpace(record[c])
			}
			return ""
		}
		account := cell("Cuenta")
		if account == "" && cell("Fecha tran") == "" {
			// No account and no day: a blank line, or a sheet's total or
			// footer ("Total cargos"), not a movement.
			continue
		}
		if account == "" {
			return File{}, fmt.Errorf("statement: BG card line %d has no Cuenta", lineNo)
		}

		bookedOn, err := t.date(cell("Fecha tran"))
		if err != nil {
			return File{}, fmt.Errorf("statement: BG card line %d: fecha tran %q", lineNo, cell("Fecha tran"))
		}
		var postedOn *civil.Date
		if s := cell("Fecha proceso"); s != "" {
			d, err := t.date(s)
			if err != nil {
				return File{}, fmt.Errorf("statement: BG card line %d: fecha proceso %q", lineNo, s)
			}
			postedOn = &d
		}

		var charge, payment money.Cents
		if s := cell("Cargos (Db)"); s != "" {
			if charge, err = t.amount(s); err != nil {
				return File{}, fmt.Errorf("statement: BG card line %d: cargos: %w", lineNo, err)
			}
		}
		if s := cell("Pagos (Cr)"); s != "" {
			if payment, err = t.amount(s); err != nil {
				return File{}, fmt.Errorf("statement: BG card line %d: pagos: %w", lineNo, err)
			}
		}
		amount, err := fromColumns(charge, payment)
		if err != nil {
			return File{}, fmt.Errorf("statement: BG card line %d: %w", lineNo, err)
		}
		if amount == 0 {
			continue
		}

		raw := make(map[string]string, len(bgCardColumns))
		for _, name := range bgCardColumns {
			raw[name] = cell(name)
		}
		description := cell("Descripcion")
		if card := cell("Tarjeta"); card != "" && card != account {
			description = "[Tarjeta " + card + "] " + description
		}

		st, ok := byAccount[account]
		if !ok {
			st = &Statement{Identity: bgCardIdentity(account)}
			byAccount[account] = st
			order = append(order, account)
		}
		st.Lines = append(st.Lines, Line{
			LineNo:       lineNo,
			BookedOn:     bookedOn,
			PostedOn:     postedOn,
			Amount:       amount,
			Description:  strings.Join(strings.Fields(description), " "),
			BankRef:      cell("Referencia"),
			BankCategory: cell("Categoria"),
			Kind:         cardKind(cell("Categoria"), description, amount),
			Raw:          raw,
		})
	}

	file := File{Source: SourceBGCard}
	for _, account := range order {
		st := byAccount[account]
		assignOccurrences(st.Lines)
		sortByBalanceDay(st.Lines)
		period(st)
		file.Statements = append(file.Statements, *st)
	}
	return file, nil
}

// bgCardIdentity is the card account a BG card file is for, account being its
// number as the bank prints it ("**** 1234"). Every format of the card builds
// it here, so they all land in the same account.
func bgCardIdentity(account string) Identity {
	return Identity{
		Institution:    InstitutionBancoGeneral,
		ExternalNumber: account,
		Currency:       "USD",
		DisplayName:    "Tarjeta " + account,
		Class:          ClassLiability,
		Type:           "credit_card",
	}
}

// cardKind classifies a card line from the bank's own category. "Pagos" is
// money arriving from the owner to pay the card: a transfer between their own
// accounts, never income. "Cargos" are the bank's fees and insurance, or
// interest when the description says so. Any other positive amount is a
// refund of a purchase.
func cardKind(category, description string, amount money.Cents) Kind {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "pagos":
		return KindTransfer
	case "cargos":
		if strings.Contains(strings.ToUpper(description), "INTERES") {
			return KindInterest
		}
		return KindFee
	}
	if amount > 0 {
		return KindRefund
	}
	return KindExpense
}

// sortByBalanceDay orders card lines by the day they reach the balance, keeping
// the file's order between lines of the same day.
func sortByBalanceDay(lines []Line) {
	for i := 1; i < len(lines); i++ {
		for j := i; j > 0 && lines[j].BalanceOn().Before(lines[j-1].BalanceOn()); j-- {
			lines[j], lines[j-1] = lines[j-1], lines[j]
		}
	}
}
