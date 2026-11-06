package statement

import (
	"bytes"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// cardSheetRow is one movement of an invented card sheet. date is written as
// text, the way the bank writes it, unless serial is set.
type cardSheetRow struct {
	account, card, date, posted string
	serial                      bool
	description, ref, category  string
	charge, payment             float64
}

// cardSheetFile builds a workbook laid out like the bank's card export: the
// title, the "Cuenta :" cell, and a header row whose cells carry accents,
// trailing spaces and a tab, with the description spanning three columns.
func cardSheetFile(t *testing.T, headers map[string]string, rows []cardSheetRow) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	sheet := f.GetSheetName(0)
	set := func(cell string, v any) {
		t.Helper()
		if err := f.SetCellValue(sheet, cell, v); err != nil {
			t.Fatal(err)
		}
	}
	set("A2", "Movimientos de tarjetas de crédito")
	set("A4", "Cuenta : TARJETA DE PRUEBA **** 1111")
	for col, name := range headers {
		set(col+"8", name)
	}
	if err := f.MergeCell(sheet, "F8", "H8"); err != nil {
		t.Fatal(err)
	}
	for i, r := range rows {
		n := strconv.Itoa(9 + i)
		set("B"+n, r.account)
		set("C"+n, r.card)
		if r.serial {
			d, err := time.Parse("02/01/2006", r.date)
			if err != nil {
				t.Fatal(err)
			}
			set("D"+n, float64(d.Sub(excelEpoch))/float64(24*time.Hour))
		} else {
			set("D"+n, r.date)
		}
		set("E"+n, r.posted)
		set("F"+n, r.description)
		set("I"+n, r.ref)
		set("J"+n, r.category)
		if r.charge != 0 {
			set("K"+n, r.charge)
		}
		if r.payment != 0 {
			set("L"+n, r.payment)
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// cardSheetHeaders are the bank's own header cells, tab and accents included.
var cardSheetHeaders = map[string]string{
	"B": "Cuenta", "C": "Tarjeta \t", "D": "Fecha transacción", "E": "Fecha proceso",
	"F": "Descripción ", "I": "Referencia", "J": "Categoría", "K": "Cargos (Db)", "L": "Pagos (Cr)",
}

// cardSheetRows are cardFile's movements, as the Excel export writes them.
var cardSheetRows = []cardSheetRow{
	{account: "**** 1111", card: "**** 1111", date: "15/01/2026", posted: "15/01/2026",
		description: "SEGURO DE DESGRAVAMEN", ref: "REF-SHARED-001", category: "Cargos", charge: -0.5},
	{account: "**** 1111", card: "**** 1111", date: "15/01/2026", posted: "15/01/2026",
		description: "ITBMS CARGO POR SEGURO", ref: "REF-SHARED-001", category: "Cargos", charge: -0.05},
	{account: "**** 1111", card: "**** 1111", date: "20/01/2026", posted: "20/01/2026", serial: true,
		description: "GRACIAS POR SU PAGO - BANCA MOVIL", ref: "REF-PAY-001", category: "Pagos", payment: 100},
	{account: "**** 1111", card: "**** 2222", date: "18/01/2026", posted: "19/01/2026",
		description: "CAFE DE PRUEBA        PANAMA        PA", ref: "REF-002", category: "Comida y Bebida", charge: -9.5},
	{account: "**** 1111", card: "**** 2222", date: "19/01/2026", posted: "20/01/2026",
		description: "FARMACIA DE PRUEBA    PANAMA        PA", ref: "REF-003", category: "Salud", charge: -4.25},
}

// Downloading the same movements as text and as Excel must give the same
// lines, or importing both would count every movement twice.
func TestTheCardsExcelAndTextExportsGiveTheSameLines(t *testing.T) {
	text := must(Parse("tarjeta.txt", []byte(cardFile)))
	sheet, err := Parse("tarjeta.xlsx", cardSheetFile(t, cardSheetHeaders, cardSheetRows))
	if err != nil {
		t.Fatal(err)
	}
	if sheet.Source != SourceBGCard {
		t.Errorf("Source = %s, want %s", sheet.Source, SourceBGCard)
	}
	a, b := only(t, text), only(t, sheet)
	if a.Identity != b.Identity || a.PeriodStart != b.PeriodStart || a.PeriodEnd != b.PeriodEnd {
		t.Errorf("statement = %+v %s..%s, want the text's %+v %s..%s",
			b.Identity, b.PeriodStart, b.PeriodEnd, a.Identity, a.PeriodStart, a.PeriodEnd)
	}
	if len(a.Lines) != len(b.Lines) {
		t.Fatalf("got %d lines, want the text's %d", len(b.Lines), len(a.Lines))
	}
	for i, want := range a.Lines {
		got := b.Lines[i]
		if DedupKey(b.Identity, got) != DedupKey(a.Identity, want) {
			t.Errorf("%q: the Excel line's key differs from the text's", want.Description)
		}
		if got.BookedOn != want.BookedOn || *got.PostedOn != *want.PostedOn || got.Amount != want.Amount ||
			got.Description != want.Description || got.Kind != want.Kind || got.BankCategory != want.BankCategory {
			t.Errorf("line %d = %+v, want the text's %+v", got.LineNo, got, want)
		}
		for name := range want.Raw {
			if _, ok := got.Raw[name]; !ok || len(got.Raw) != len(want.Raw) {
				t.Errorf("line %d: Raw has keys %v, want the text's", got.LineNo, got.Raw)
				break
			}
		}
	}
	// The sheet's row, not the line of a text file, is what an error names.
	if b.Lines[0].LineNo != 9 {
		t.Errorf("LineNo = %d, want the sheet's row 9", b.Lines[0].LineNo)
	}
}

func TestACardSheetMissingAColumnSaysWhich(t *testing.T) {
	headers := map[string]string{}
	for col, name := range cardSheetHeaders {
		if name != "Categoría" {
			headers[col] = name
		}
	}
	_, err := Parse("tarjeta.xlsx", cardSheetFile(t, headers, cardSheetRows))
	if err == nil || !strings.Contains(err.Error(), `"Categoria"`) {
		t.Errorf("err = %v, want the missing Categoria column named", err)
	}
}

// The sheet can end in totals. A row with no account and no day is not a
// movement and is left out; a row with a day and no account is a movement the
// file does not say where to file, and is an error.
func TestACardSheetsTotalRowsAreNotMovements(t *testing.T) {
	totals := append(slices.Clone(cardSheetRows),
		cardSheetRow{description: "Total cargos", charge: -14.3},
		cardSheetRow{description: "Total pagos", payment: 100})
	st := only(t, must(Parse("tarjeta.xlsx", cardSheetFile(t, cardSheetHeaders, totals))))
	if len(st.Lines) != len(cardSheetRows) {
		t.Errorf("got %d lines, want the %d movements and no total", len(st.Lines), len(cardSheetRows))
	}

	unfiled := append(slices.Clone(cardSheetRows),
		cardSheetRow{date: "21/01/2026", description: "COMERCIO DE PRUEBA", ref: "REF-004", charge: -1})
	_, err := Parse("tarjeta.xlsx", cardSheetFile(t, cardSheetHeaders, unfiled))
	if err == nil || !strings.Contains(err.Error(), "line 14 has no Cuenta") {
		t.Errorf("err = %v, want row 14 named for its missing Cuenta", err)
	}
}

// Every .xlsx went to the account's parser before the card's sheet had one;
// the account's exports must not notice.
func TestTheAccountsWorkbooksStillGoToTheAccountsParser(t *testing.T) {
	file := must(Parse("movimientos.xlsx", bgAccountFile(t, bgFixture)))
	if file.Source != SourceBGAccount {
		t.Errorf("Source = %s, want %s", file.Source, SourceBGAccount)
	}

	// The account's plain Excel export has one signed "Monto" column instead
	// of Débito and Crédito, and is refused for it, as before.
	f := excelize.NewFile()
	defer f.Close()
	sheet := f.GetSheetName(0)
	for cell, v := range map[string]any{
		"B2": "Últimos movimientos", "B4": "Cuenta:Prueba 00-00-00-000000-0",
		"B8": "Fecha", "C8": "Referencia", "D8": "Descripción", "E8": "Monto", "F8": "Saldo total",
		"B9": 46330.5, "C9": "0", "D9": "YAPPY BG A PERSONA DE PRUEBA", "E9": -12.5, "F9": 87.5,
	} {
		if err := f.SetCellValue(sheet, cell, v); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse("movimientos.xlsx", buf.Bytes()); err == nil || !strings.Contains(err.Error(), "Débito") {
		t.Errorf("err = %v, want the account's missing Débito column", err)
	}
}
