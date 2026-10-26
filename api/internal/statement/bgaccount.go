package statement

import (
	"bytes"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
)

// excelEpoch is Excel's day zero. A BG export's date cell is a serial number of
// days since it. Its fractional part is the time the file was downloaded — the
// same on every row — so only the day is the movement's.
var excelEpoch = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

// parseBGAccount reads a Banco General account movement export (.xlsx):
// a "Cuenta:<alias> <number>" cell, a header row found by its "Fecha" cell,
// and one row per movement with Débito, Crédito and the running "Saldo total".
func parseBGAccount(data []byte) (File, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{RawCellValue: true})
	if err != nil {
		return File{}, fmt.Errorf("statement: opening the BG account file: %w", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return File{}, fmt.Errorf("statement: the BG account file has no sheets")
	}
	rows, err := f.GetRows(sheets[0], excelize.Options{RawCellValue: true})
	if err != nil {
		return File{}, fmt.Errorf("statement: reading the BG account file: %w", err)
	}

	id := Identity{
		Institution: InstitutionBancoGeneral,
		Currency:    "USD", // the export states no currency; BG accounts here are USD
		Class:       ClassAsset,
		Type:        "checking",
	}
	header, index := -1, map[string]int{}
	for i, row := range rows {
		for _, cell := range row {
			if alias, number, ok := bgAccountLabel(cell); ok && id.ExternalNumber == "" {
				id.ExternalNumber, id.DisplayName = number, alias
			}
		}
		if header < 0 && hasCell(row, "Fecha") {
			header = i
			for c, name := range row {
				index[strings.TrimSpace(name)] = c
			}
		}
	}
	if id.ExternalNumber == "" {
		return File{}, fmt.Errorf("statement: the BG account file does not say which account it is (no \"Cuenta:\" cell)")
	}
	if header < 0 {
		return File{}, fmt.Errorf("statement: the BG account file has no header row with \"Fecha\"")
	}
	for _, want := range []string{"Fecha", "Descripción", "Débito", "Crédito"} {
		if _, ok := index[want]; !ok {
			return File{}, fmt.Errorf("statement: the BG account file is missing the %q column", want)
		}
	}
	_, hasSaldo := index["Saldo total"]

	var lines []Line
	for i, row := range rows[header+1:] {
		lineNo := header + i + 2
		cell := func(name string) string {
			if c, ok := index[name]; ok && c < len(row) {
				return strings.TrimSpace(row[c])
			}
			return ""
		}
		if cell("Fecha") == "" {
			continue
		}
		bookedOn, err := excelDate(cell("Fecha"))
		if err != nil {
			return File{}, fmt.Errorf("statement: BG account line %d: fecha %q: %w", lineNo, cell("Fecha"), err)
		}

		var debit, credit money.Cents
		if t := cell("Débito"); t != "" {
			if debit, err = centsFromFloat(t); err != nil {
				return File{}, fmt.Errorf("statement: BG account line %d: débito: %w", lineNo, err)
			}
		}
		if t := cell("Crédito"); t != "" {
			if credit, err = centsFromFloat(t); err != nil {
				return File{}, fmt.Errorf("statement: BG account line %d: crédito: %w", lineNo, err)
			}
		}
		amount, err := fromColumns(debit, credit)
		if err != nil {
			return File{}, fmt.Errorf("statement: BG account line %d: %w", lineNo, err)
		}
		if amount == 0 {
			continue
		}

		raw := map[string]string{}
		for name, c := range index {
			if c < len(row) && name != "" {
				raw[name] = strings.TrimSpace(row[c])
			}
		}
		l := Line{
			LineNo:      lineNo,
			BookedOn:    bookedOn,
			Amount:      amount,
			Description: cell("Descripción"),
			// BG's own Referencia is always "0"; the transaction code is the
			// one reference-like field that carries information, and it is
			// the same in every export of the same movement.
			BankRef:      cell("Transacción"),
			BankCategory: cell("Transacción"),
			Kind:         bgAccountKind(cell("Descripción"), amount),
			BankTransfer: bgAccountTransfer(cell("Descripción")),
			Raw:          raw,
		}
		if hasSaldo && cell("Saldo total") != "" {
			bal, err := centsFromFloat(cell("Saldo total"))
			if err != nil {
				return File{}, fmt.Errorf("statement: BG account line %d: saldo total: %w", lineNo, err)
			}
			l.RunningBalance = &bal
		}
		lines = append(lines, l)
	}

	assignOccurrences(lines)
	st := Statement{Identity: id, Lines: chronological(lines)}
	period(&st)
	if len(st.Lines) > 0 {
		first, last := st.Lines[0], st.Lines[len(st.Lines)-1]
		if first.RunningBalance != nil {
			st.Opening = cents(*first.RunningBalance - first.Amount)
		}
		if last.RunningBalance != nil {
			st.Closing = cents(*last.RunningBalance)
		}
	}
	checkChain(&st)
	return File{Source: SourceBGAccount, Statements: []Statement{st}}, nil
}

// chronological orders lines oldest first. A BG export can list newest first,
// and lines of the same day carry no time; the file's own order between those
// is kept only if it is the one the running balances agree with.
func chronological(lines []Line) []Line {
	out := append([]Line(nil), lines...)
	// File order reversed is the other candidate for same-day lines: an
	// export that lists newest first lists them newest first too.
	if newestFirst(lines) {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].BookedOn.Before(out[j].BookedOn) })
	return out
}

// newestFirst reports whether the running balances link each line to the one
// after it (a newest-first listing) better than to the one before it.
func newestFirst(lines []Line) bool {
	forward, backward := 0, 0
	for i := 1; i < len(lines); i++ {
		a, b := lines[i-1], lines[i]
		if a.RunningBalance == nil || b.RunningBalance == nil {
			continue
		}
		if *b.RunningBalance == *a.RunningBalance+b.Amount {
			forward++
		}
		if *a.RunningBalance == *b.RunningBalance+a.Amount {
			backward++
		}
	}
	return backward > forward
}

// excelDate reads the day of a date cell's serial, rounded to the second
// first so float noise cannot push a midnight into the day before.
func excelDate(raw string) (civil.Date, error) {
	serial, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return civil.Date{}, fmt.Errorf("no es una fecha de Excel")
	}
	seconds := math.Round(serial * 24 * 60 * 60)
	return civil.Of(excelEpoch.Add(time.Duration(seconds) * time.Second)), nil
}

// bgAccountLabel splits "Cuenta:<alias> <number>". The number is the last
// token; it has no spaces of its own ("04-98-97-958835-1").
func bgAccountLabel(cell string) (alias, number string, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(cell), "Cuenta:")
	if !found {
		return "", "", false
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", "", false
	}
	number = fields[len(fields)-1]
	alias = strings.Join(fields[:len(fields)-1], " ")
	if alias == "" {
		alias = "Cuenta " + number
	}
	return alias, number, true
}

// bankKind is the importer's first guess for a bank-account line. Transfers
// between the owner's own accounts cannot be told from the line alone; rules
// and transfer matching refine it later.
func bankKind(amount money.Cents) Kind {
	if amount > 0 {
		return KindIncome
	}
	return KindExpense
}

// bgAccountKind refines bankKind with what the BG account's description
// already says:
//
//   - "… ENTRE CUENTAS" is the bank's own label for a transfer between two of
//     the holder's accounts there.
//   - "COMISION …" is the bank's fee.
//   - "YAPPY BG DE <persona>" is someone sending money, which for a person is
//     mostly their share of something the owner paid: a refund that lowers
//     spending, not income. A real payment is the exception, and the owner
//     can say so.
//
// A card payment ("PAGO VISA …") is left as spending: transfer matching pairs
// it with the card's own "Pagos" line when the card is imported, and until
// then it is the best record of what the card bought.
func bgAccountKind(description string, amount money.Cents) Kind {
	d := strings.ToUpper(description)
	switch {
	case strings.Contains(d, "ENTRE CUENTAS"):
		return KindTransfer
	case amount < 0 && strings.HasPrefix(d, "COMISION"):
		return KindFee
	case amount > 0 && strings.HasPrefix(d, "YAPPY BG DE "):
		return KindRefund
	}
	return bankKind(amount)
}

// bgAccountTransfer reports whether a BG account line is money moved between
// banks or accounts — an ACH, or a mobile-banking transfer — as opposed to a
// Yappy, a card payment or a top-up, which can name the holder without being
// a transfer between their accounts.
func bgAccountTransfer(description string) bool {
	d := strings.ToUpper(description)
	if strings.HasPrefix(d, "ACH") {
		return true
	}
	rest, ok := strings.CutPrefix(d, "BANCA MOVIL ")
	return ok && !strings.HasPrefix(rest, "PAGO") && !strings.HasPrefix(rest, "RECARGA")
}

func hasCell(row []string, want string) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell) == want {
			return true
		}
	}
	return false
}
