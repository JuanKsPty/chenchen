package imports_test

import (
	"bytes"
	"context"
	"strconv"
	"testing"

	"github.com/go-pdf/fpdf"
	"github.com/xuri/excelize/v2"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/statement"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// cardSheet is cardFile as the card's Excel export writes it: a title, the
// account cell, headers with accents and a tab, dates as text and amounts as
// numbers.
func cardSheet(t *testing.T) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	s := f.GetSheetName(0)
	set := func(c string, v any) {
		if err := f.SetCellValue(s, c, v); err != nil {
			t.Fatal(err)
		}
	}
	set("A2", "Movimientos de tarjetas de crédito")
	set("A4", "Cuenta : TARJETA DE PRUEBA **** 1111")
	for col, name := range map[string]string{"B": "Cuenta", "C": "Tarjeta \t", "D": "Fecha transacción",
		"E": "Fecha proceso", "F": "Descripción ", "I": "Referencia", "J": "Categoría", "K": "Cargos (Db)", "L": "Pagos (Cr)"} {
		set(col+"8", name)
	}
	for i, r := range []struct {
		card, date, posted, desc, ref, category string
		charge, payment                         float64
	}{
		{"**** 1111", "15/01/2026", "15/01/2026", "SEGURO DE DESGRAVAMEN", "R1", "Cargos", -0.5, 0},
		{"**** 1111", "15/01/2026", "15/01/2026", "ITBMS CARGO POR SEGURO", "R1", "Cargos", -0.05, 0},
		{"**** 1111", "20/01/2026", "20/01/2026", "GRACIAS POR SU PAGO - BANCA MOVIL", "R2", "Pagos", 0, 100},
		{"**** 2222", "18/01/2026", "19/01/2026", "CAFE DE PRUEBA PANAMA", "R3", "Comida y Bebida", -9.5, 0},
		{"**** 2222", "19/01/2026", "20/01/2026", "FARMACIA DE PRUEBA PANAMA", "R4", "Salud", -4.25, 0},
	} {
		n := strconv.Itoa(9 + i)
		set("B"+n, "**** 1111")
		set("C"+n, r.card)
		set("D"+n, r.date)
		set("E"+n, r.posted)
		set("F"+n, r.desc)
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

// cardStatementPDF draws the closed statement of cardFile's card, laid out
// like the bank's: the account, the cut date and the summary row under its
// labels. It owed 100.00 at the previous cut and 14.30 at this one: -100.00 -
// 14.30 + 100.00, exactly what cardFile's lines add up to.
func cardStatementPDF(t *testing.T) []byte {
	t.Helper()
	return cardStatementPDFAt(t, "21-ene-2026 • 10:15 AM", "20-ene-2026")
}

// cardStatementPDFAt is cardStatementPDF downloaded at another moment, which
// the bank prints on it, or for another cut day.
func cardStatementPDFAt(t *testing.T, downloaded, cut string) []byte {
	t.Helper()
	doc := fpdf.New("P", "pt", "Letter", "")
	doc.AddPage()
	tr := doc.UnicodeTranslatorFromDescriptor("")
	text := func(x, y float64, s string) {
		doc.SetFont("Helvetica", "", 10)
		doc.Text(x, y, tr(s))
	}
	text(450, 30, downloaded)
	text(20, 65, "TARJETA DE PRUEBA")
	text(20, 84, "**** 1111 null")
	text(20, 125, "Estado de cuenta de "+cut)
	for i, cell := range [][2]string{{"Deuda anterior", "$100.00"}, {"Compras y cargos", "-$14.30"},
		{"Pagos", "$100.00"}, {"Deuda al corte", "$14.30"}, {"Pago mínimo", "$14.30"}, {"Fecha de pago", "15-feb-2026"}} {
		text(20+95*float64(i), 146, cell[0])
		text(20+95*float64(i), 166, cell[1])
	}
	text(20, 348, "Movimientos PERSONA DE PRUEBA • **** 2222")
	text(20, 386, "18-ene-2026")
	text(120, 386, "CAFE DE PRUEBA PANAMA")
	text(563, 386, "-$9.50")
	var buf bytes.Buffer
	if err := doc.Output(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

var cutDay = civil.Date{Year: 2026, Month: 1, Day: 20}

// The card's text and Excel exports bring its lines and no balance; its closed
// statement's PDF brings the balance and no lines. Together the card owes what
// the bank says, every movement counted once.
func TestACardStatementPDFAnchorsTheCardAtItsCut(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	first := e.importFile(t, "tarjeta.txt", []byte(cardFile)).Accounts[0]
	if !first.NeedsOpening {
		t.Fatal("the fixture is not what this test assumes: the card should lack its balance")
	}
	card := first.AccountID

	// The same movements downloaded as Excel are the same movements.
	sheet := e.importFile(t, "tarjeta.xlsx", cardSheet(t))
	if got := sheet.Accounts[0]; sheet.Source != statement.SourceBGCard || got.AccountID != card ||
		got.New != 0 || got.Duplicates != 5 {
		t.Fatalf("Excel import = %s %+v, want the 5 text lines as duplicates in the same card", sheet.Source, got)
	}

	pdf := cardStatementPDF(t)
	r := e.importFile(t, "estado.pdf", pdf)
	if r.Source != statement.SourceBGCardStatement || len(r.Accounts) != 1 {
		t.Fatalf("result = %+v", r)
	}
	got := r.Accounts[0]
	if got.AccountID != card || got.Created || got.Lines != 0 || got.New != 0 || got.NeedsOpening {
		t.Errorf("account result = %+v, want the same card, no lines, nothing more to ask", got)
	}
	// The previous debt is the previous cut's balance, not this one's
	// opening: the statement brings its closing alone.
	if got.Opening != nil || got.Closing == nil || *got.Closing != -1430 {
		t.Errorf("Opening = %v, Closing = %v; want none and -14.30", got.Opening, got.Closing)
	}

	runs, err := e.db.Q().ImportRunsByAccount(ctx, e.user, card, 10)
	if err != nil {
		t.Fatal(err)
	}
	if run := runs[0]; run.Source != "bg_card_statement" || run.LinesSeen != 0 ||
		run.PeriodStart != cutDay || run.PeriodEnd != cutDay {
		t.Errorf("latest run = %+v, want the PDF's, on the cut day", run)
	}
	stmts, err := e.db.Q().StatementsByAccount(ctx, e.user, card)
	if err != nil {
		t.Fatal(err)
	}
	if last := stmts[len(stmts)-1]; last.LineCount != 0 || last.PeriodEnd != cutDay || last.Opening != nil ||
		last.Closing == nil || *last.Closing != -1430 {
		t.Errorf("latest statement = %+v", last)
	}
	// The closing on the cut day is the card's one checkpoint, and its
	// anchor.
	cps, err := e.db.Q().CheckpointsByAccount(ctx, e.user, card)
	if err != nil {
		t.Fatal(err)
	}
	if len(cps) != 1 || cps[0].Source != store.SourceStatementClosing || cps[0].AsOf != cutDay ||
		cps[0].Balance != -1430 || cps[0].BeforeEntryID != nil {
		t.Fatalf("checkpoints = %+v, want one closing of -14.30 on the cut day", cps)
	}
	// The closing is the anchor, so the card's lines are measured from it.
	if a := e.account(t, card); !a.Anchored || a.Balance != -1430 {
		t.Errorf("card = balance %s, anchored %v; want owing 14.30", a.Balance, a.Anchored)
	}
	// Measured back from the cut, the lines open exactly where the PDF says
	// the previous statement left the card.
	rec, err := e.accounts.Reconcile(ctx, e.user, card)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Opening != -10000 {
		t.Errorf("opening = %s, want the PDF's previous debt of 100.00 as -100.00", rec.Opening)
	}

	if again := e.importFile(t, "estado.pdf", pdf); !again.Unchanged {
		t.Error("the same PDF again must report unchanged")
	}
	if cps, _ := e.db.Q().CheckpointsByAccount(ctx, e.user, card); len(cps) != 1 {
		t.Errorf("got %d checkpoints after importing the same PDF twice, want 1", len(cps))
	}

	// A later text export no longer asks for a starting balance.
	later := cardFile + "**** 1111;**** 2222;22/01/2026;23/01/2026;OTRO COMERCIO;R5;Tiendas;-1.00;\r\n"
	if res := e.importFile(t, "tarjeta2.txt", []byte(later)).Accounts[0]; res.NeedsOpening || res.New != 1 {
		t.Errorf("later import = %+v, want 1 new line and no request for an opening", res)
	}
	if a := e.account(t, card); a.Balance != -1530 {
		t.Errorf("balance = %s, want -15.30: the cut's -14.30 and the purchase after it", a.Balance)
	}
}

// The PDF can arrive first: it opens the card, and the lines that follow land
// in it.
func TestACardStatementPDFFirstOpensTheCardTheLinesThenFill(t *testing.T) {
	e := setup(t)
	opened := e.importFile(t, "estado.pdf", cardStatementPDF(t)).Accounts[0]
	if !opened.Created || opened.NeedsOpening {
		t.Fatalf("PDF import = %+v, want a new card that already has its balance", opened)
	}
	lines := e.importFile(t, "tarjeta.txt", []byte(cardFile)).Accounts[0]
	if lines.AccountID != opened.AccountID || lines.Created || lines.NeedsOpening {
		t.Errorf("text import = %+v, want the PDF's card %s", lines, opened.AccountID)
	}
	if a := e.account(t, opened.AccountID); !a.Anchored || a.Balance != -1430 {
		t.Errorf("balance = %s (anchored %v), want -14.30", a.Balance, a.Anchored)
	}
}

// The bank prints the moment of each download on the PDF, so no two downloads
// of one statement share a checksum. A second download is still nothing new;
// another cut is.
func TestDownloadingACardStatementAgainIsNothingNew(t *testing.T) {
	e := setup(t)
	e.importFile(t, "tarjeta.txt", []byte(cardFile))
	first := e.importFile(t, "estado.pdf", cardStatementPDFAt(t, "21-ene-2026 • 10:15 AM", "20-ene-2026"))
	if first.Unchanged {
		t.Fatal("the first statement was taken as already imported")
	}
	again := cardStatementPDFAt(t, "22-ene-2026 • 08:02 PM", "20-ene-2026")
	if bytes.Equal(again, cardStatementPDF(t)) {
		t.Fatal("the fixture is not what this test assumes: the two downloads should differ")
	}
	if r := e.importFile(t, "estado (1).pdf", again); !r.Unchanged {
		t.Errorf("a second download of the same statement = %+v, want unchanged", r)
	}
	cps, err := e.db.Q().CheckpointsByUser(context.Background(), e.user)
	if err != nil {
		t.Fatal(err)
	}
	closings := 0
	for _, cp := range cps {
		if cp.Source == store.SourceStatementClosing {
			closings++
		}
	}
	if closings != 1 {
		t.Errorf("%d closing checkpoints, want 1", closings)
	}
	if r := e.importFile(t, "estado-feb.pdf", cardStatementPDFAt(t, "21-feb-2026 • 09:00 AM", "20-feb-2026")); r.Unchanged {
		t.Error("another cut's statement was taken as already imported")
	}
}

// A statement that prints no lines cannot say a noted purchase is not in it.
// Only once the lines arrive, and none of them is it, is the purchase missing.
func TestAStatementWithoutLinesNeverCallsAPurchaseMissing(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	card := e.importFile(t, "estado.pdf", cardStatementPDF(t)).Accounts[0].AccountID
	noted := e.note(t, card, jan(10), -777, "TIENDA DE PRUEBA")

	missing, err := e.db.Q().MissingCaptures(ctx, e.user)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := missing[noted]; ok {
		t.Error("the statement without lines called the purchase missing")
	}

	e.importFile(t, "tarjeta.txt", []byte(cardFile))
	if missing, err = e.db.Q().MissingCaptures(ctx, e.user); err != nil {
		t.Fatal(err)
	}
	if _, ok := missing[noted]; !ok {
		t.Error("the card's lines reach past the purchase and none is it, yet it is not missing")
	}
}
