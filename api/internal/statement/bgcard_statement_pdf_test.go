package statement

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/go-pdf/fpdf"
	"github.com/ledongthuc/pdf"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
)

// pdfSummary is what an invented closed statement prints in its title and
// summary row.
type pdfSummary struct {
	cut                                  string
	previous, purchases, payments, atCut string
}

// owedAndPaid is a statement that opened owing 25.00 and closed with 4.10 in
// the holder's favour: -25.00 - 180.40 + 209.50 = +4.10.
var owedAndPaid = pdfSummary{cut: "22-mar-2026",
	previous: "$25.00", purchases: "-$180.40", payments: "$209.50", atCut: "-$4.10"}

// cardStatementPDF draws a statement laid out like the bank's PDF, with
// invented content: the card's name and account, the title with the cut date,
// the summary's labels with their values on the row below, and two cards'
// movements, which the parser must ignore. password, when set, encrypts it.
func cardStatementPDF(t *testing.T, s pdfSummary, password string) []byte {
	t.Helper()
	doc := fpdf.New("P", "pt", "Letter", "")
	if password != "" {
		doc.SetProtection(fpdf.CnProtectPrint, password, "dueño")
	}
	doc.AddPage()
	tr := doc.UnicodeTranslatorFromDescriptor("")
	text := func(size, x, y float64, s string) {
		doc.SetFont("Helvetica", "", size)
		doc.Text(x, y, tr(s))
	}
	text(8, 504, 35, "02-abr-2026 • 09:30 AM")
	text(12, 20, 65, "TARJETA DE PRUEBA")
	text(10, 20, 84, "**** 1111 null")
	text(10, 20, 105, "Titulo:")
	text(10, 60, 105, "PERSONA DE PRUEBA")
	text(12, 20, 125, "Estado de cuenta de "+s.cut)
	columns := []float64{20, 115, 210, 305, 400, 495}
	labels := []string{"Deuda anterior", "Compras y cargos", "Pagos", "Deuda al corte", "Pago mínimo", "Fecha de pago"}
	values := []string{s.previous, s.purchases, s.payments, s.atCut, "$0.00", "16-abr-2026"}
	for i, x := range columns {
		text(9, x, 146, labels[i])
		text(10, x, 166, values[i])
	}
	text(10, 20, 188, "Cargos y Pagos")
	text(8, 20, 226, "01-mar-2026")
	text(8, 120, 226, "GRACIAS POR SU PAGO - BANCA MOVIL")
	text(8, 370, 226, "Pagos")
	text(8, 561, 226, "$209.50")
	text(10, 20, 348, "Movimientos PERSONA DE PRUEBA • **** 2222")
	text(8, 20, 386, "03-mar-2026")
	text(8, 120, 386, "COMERCIO DE PRUEBA     PANAMA        PA")
	text(8, 370, 386, "Tiendas y almacenes")
	text(8, 563, 386, "-$180.40")
	var buf bytes.Buffer
	if err := doc.Output(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestACardStatementPDFIsTheDebtAtTheCutAsTheHoldersBalance(t *testing.T) {
	file, err := Parse("estado.pdf", cardStatementPDF(t, owedAndPaid, ""))
	if err != nil {
		t.Fatal(err)
	}
	if file.Source != SourceBGCardStatement {
		t.Errorf("Source = %s, want %s", file.Source, SourceBGCardStatement)
	}
	st := only(t, file)
	// The same account the card's text and Excel exports file their lines in,
	// not the additional card further down the page.
	text := only(t, must(Parse("tarjeta.txt", []byte(cardFile))))
	if st.Identity != text.Identity {
		t.Errorf("Identity = %+v, want the text export's %+v", st.Identity, text.Identity)
	}
	if len(st.Lines) != 0 {
		t.Errorf("got %d lines; the PDF's movements have no reference and must be left to the other exports", len(st.Lines))
	}
	cut := civil.Date{Year: 2026, Month: 3, Day: 22}
	if st.PeriodStart != cut || st.PeriodEnd != cut {
		t.Errorf("period = %s..%s, want the cut day", st.PeriodStart, st.PeriodEnd)
	}
	// "Deuda anterior" is the debt at the previous cut: as the opening of
	// this statement's one day it would be a balance on the wrong day.
	if st.Opening != nil {
		t.Errorf("Opening = %s, want none", st.Opening)
	}
	// A debt of -4.10 is 4.10 in the holder's favour.
	if st.Closing == nil || *st.Closing != 410 {
		t.Errorf("Closing = %v, want +4.10", st.Closing)
	}
	if len(st.Warnings) != 0 {
		t.Errorf("Warnings = %q, want none: the summary adds up", st.Warnings)
	}
}

func TestACardStatementWhoseSummaryDoesNotAddUpSaysSo(t *testing.T) {
	misread := owedAndPaid
	misread.payments = "$300.00"
	st := only(t, must(Parse("estado.pdf", cardStatementPDF(t, misread, ""))))
	if len(st.Warnings) != 1 || !strings.Contains(st.Warnings[0], "deuda anterior de 25.00") {
		t.Fatalf("Warnings = %q, want one that names the previous debt", st.Warnings)
	}
	if st.Opening != nil || st.Closing == nil || *st.Closing != 410 {
		t.Errorf("Opening = %v, Closing = %v; the bank's debt at the cut stays as printed", st.Opening, st.Closing)
	}
}

// Two downloads of one statement differ in their bytes, never in what they
// say: the key is the card, the cut and the debt at the cut.
func TestACardStatementsContentKeyIsTheCardTheCutAndTheDebt(t *testing.T) {
	key := func(s pdfSummary) string {
		t.Helper()
		k, ok := must(Parse("estado.pdf", cardStatementPDF(t, s, ""))).ContentKey()
		if !ok {
			t.Fatal("a card statement has no content key")
		}
		return k
	}
	same := key(owedAndPaid)
	if key(owedAndPaid) != same {
		t.Error("the same statement gave two keys")
	}
	otherCut, otherDebt := owedAndPaid, owedAndPaid
	otherCut.cut = "22-abr-2026"
	otherDebt.atCut = "-$5.10"
	if key(otherCut) == same || key(otherDebt) == same {
		t.Error("another cut, or another debt at the cut, gave the same key")
	}
	if _, ok := must(Parse("tarjeta.txt", []byte(cardFile))).ContentKey(); ok {
		t.Error("a text export has a content key; its bytes already are one")
	}
}

func TestACardStatementPDFThatCannotBeReadIsAnErrorNotAPanic(t *testing.T) {
	noDebt := owedAndPaid
	noDebt.atCut = ""
	for name, data := range map[string][]byte{
		"encrypted":     cardStatementPDF(t, owedAndPaid, "secreto"),
		"no debt value": cardStatementPDF(t, noDebt, ""),
		"a bad date":    cardStatementPDF(t, pdfSummary{cut: "31-feb-2026", previous: "$1.00", atCut: "$1.00"}, ""),
		"truncated":     cardStatementPDF(t, owedAndPaid, "")[:600],
		"only a header": []byte("%PDF-1.4\n"),
		// The library panics on a page that restores a state it never saved.
		"a page that crashes the reader": minimalPDF("Q BT /F1 12 Tf (Estado de cuenta) Tj ET"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse("estado.pdf", data)
			if err == nil {
				t.Fatal("Parse accepted it")
			}
			if errors.Is(err, ErrUnrecognised) || errors.Is(err, ErrAccountNotStated) {
				t.Errorf("err = %v; a PDF that cannot be read is an unreadable statement", err)
			}
		})
	}
}

// The reader names no bank, so a PDF is a card's closed statement only when
// its first page says so: the title with the cut and every summary label. Any
// other PDF that opens is a format we do not know, which is what the person
// is told, rather than that their statement could not be read.
func TestAPDFThatIsNotACardStatementIsNotAFormatWeKnow(t *testing.T) {
	blank := fpdf.New("P", "pt", "Letter", "")
	blank.AddPage()
	var blankPDF bytes.Buffer
	if err := blank.Output(&blankPDF); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"a blank page": blankPDF.Bytes(),
		"an invoice": placedPDF([]placed{{20, 65, 12, "Factura 0001"}, {20, 85, 10, "Total"},
			{115, 85, 10, "$25.00"}}),
		"the title and no summary": placedPDF(without(statementPieces(owedAndPaid), cardStatementPieceLabels...)),
		"the summary and no title": placedPDF(without(statementPieces(owedAndPaid), "Estado de cuenta de 22-mar-2026")),
		"a label short":            placedPDF(without(statementPieces(owedAndPaid), "Fecha de pago")),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse("documento.pdf", data)
			if !errors.Is(err, ErrUnrecognised) {
				t.Errorf("err = %v, want ErrUnrecognised", err)
			}
		})
	}
	// The pieces themselves are a statement: it is what each one leaves out.
	if _, err := Parse("estado.pdf", placedPDF(statementPieces(owedAndPaid))); err != nil {
		t.Errorf("the whole page: %v", err)
	}
}

// A PDF reader takes a header that starts a little into the file, after
// bytes a download or a mail gateway put before it; so does this one, within
// the first kilobyte, as readers do.
func TestAPDFWhoseHeaderIsNotItsFirstByteIsStillRead(t *testing.T) {
	statement := cardStatementPDF(t, owedAndPaid, "")
	lead := []byte("\xef\xbb\xbf" + strings.Repeat(" ", 500) + "\r\n")
	st := only(t, must(Parse("estado.pdf", append(lead, statement...))))
	if st.Closing == nil || *st.Closing != 410 {
		t.Errorf("Closing = %v, want +4.10", st.Closing)
	}
	far := append(bytes.Repeat([]byte(" "), pdfHeaderWindow), statement...)
	if _, err := Parse("estado.pdf", far); !errors.Is(err, ErrUnrecognised) {
		t.Errorf("a header past the first kilobyte: err = %v, want ErrUnrecognised", err)
	}
}

// Some PDFs place each word on its own and draw no space glyph between them.
// Words a space's width apart read as words; words drawn closer read glued
// together, and still match.
func TestACardStatementDrawnWordByWordIsStillRead(t *testing.T) {
	var pieces []placed
	for _, p := range statementPieces(owedAndPaid) {
		switch {
		case p.text == "Estado de cuenta de 22-mar-2026", p.text == "Deuda al corte":
			pieces = append(pieces, words(p, 0.05)...)
		case p.text == "**** 1111 null":
			pieces = append(pieces, words(placed{p.x, p.y, p.size, "** ** 1111 null"}, 0.3)...)
		default:
			pieces = append(pieces, words(p, 0.3)...)
		}
	}
	data := placedPDF(pieces)

	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var labels []string
	for _, row := range pdfRows(r, 1) {
		if row.has("deuda anterior") {
			for _, run := range row {
				labels = append(labels, run.text)
			}
		}
	}
	if want := []string{"Deuda anterior", "Compras y cargos", "Pagos", "Deudaalcorte", "Pago mínimo", "Fecha de pago"}; !slices.Equal(labels, want) {
		t.Errorf("labels = %q, want %q", labels, want)
	}

	st := only(t, must(Parse("estado.pdf", data)))
	if st.PeriodEnd != (civil.Date{Year: 2026, Month: 3, Day: 22}) || st.Identity.ExternalNumber != "**** 1111" {
		t.Errorf("cut %s, account %q; want 2026-03-22 and **** 1111", st.PeriodEnd, st.Identity.ExternalNumber)
	}
	if st.Closing == nil || *st.Closing != 410 || len(st.Warnings) != 0 {
		t.Errorf("Closing = %v, Warnings = %q; want +4.10 and a summary that adds up", st.Closing, st.Warnings)
	}
}

// The bank draws each value at its label's start. Drawn against the right
// edge of its column instead, a value is still under the label whose column
// holds it, not the next one it happens to start closer to; and a value drawn
// a little lower than the rest is still in the row.
func TestRightAlignedValuesAreReadUnderTheirOwnLabels(t *testing.T) {
	var pieces []placed
	values := map[string]bool{}
	for _, v := range []string{owedAndPaid.previous, owedAndPaid.purchases, owedAndPaid.payments,
		owedAndPaid.atCut, "$0.00", "16-abr-2026"} {
		values[v] = true
	}
	column := 0
	for _, p := range statementPieces(owedAndPaid) {
		if values[p.text] {
			// Against the column's right edge, 5pt before the next label;
			// every other one 2pt lower.
			p.x = p.x + 90 - width(p)
			p.y += float64(column%2) * 2
			column++
		}
		pieces = append(pieces, p)
	}
	if column != 6 {
		t.Fatalf("moved %d values, want the summary's 6", column)
	}
	st := only(t, must(Parse("estado.pdf", placedPDF(pieces))))
	if st.Closing == nil || *st.Closing != 410 {
		t.Errorf("Closing = %v, want +4.10", st.Closing)
	}
	if len(st.Warnings) != 0 {
		t.Errorf("Warnings = %q, want none: every value under its own label adds up", st.Warnings)
	}
}

// placed is a piece of text a test PDF draws: x from the left and y, its
// baseline, from the top, in points, as fpdf places them.
type placed struct {
	x, y, size float64
	text       string
}

// cardStatementPieceLabels are the summary's labels as the bank prints them.
var cardStatementPieceLabels = []string{
	"Deuda anterior", "Compras y cargos", "Pagos", "Deuda al corte", "Pago mínimo", "Fecha de pago",
}

// statementPieces is cardStatementPDF's first page as pieces for placedPDF,
// each where the bank draws it.
func statementPieces(s pdfSummary) []placed {
	pieces := []placed{
		{20, 65, 12, "TARJETA DE PRUEBA"},
		{20, 84, 10, "**** 1111 null"},
		{20, 125, 12, "Estado de cuenta de " + s.cut},
	}
	values := []string{s.previous, s.purchases, s.payments, s.atCut, "$0.00", "16-abr-2026"}
	for i, label := range cardStatementPieceLabels {
		x := 20 + 95*float64(i)
		pieces = append(pieces, placed{x, 146, 9, label}, placed{x, 166, 10, values[i]})
	}
	return pieces
}

// without is pieces less those that read one of texts.
func without(pieces []placed, texts ...string) []placed {
	var out []placed
	for _, p := range pieces {
		if !slices.Contains(texts, p.text) {
			out = append(out, p)
		}
	}
	return out
}

// words is p drawn word by word, with no space between them: each word starts
// gap times the size after the one before it ends.
func words(p placed, gap float64) []placed {
	var out []placed
	x := p.x
	for _, w := range strings.Fields(p.text) {
		word := placed{x, p.y, p.size, w}
		out = append(out, word)
		x += width(word) + gap*p.size
	}
	return out
}

// width is how wide p is in minimalPDF's font, whose every glyph is half its
// size wide.
func width(p placed) float64 {
	return float64(utf8.RuneCountInString(p.text)) * p.size / 2
}

// placedPDF draws each piece as a text object of its own, in minimalPDF's
// font, on a Letter page.
func placedPDF(pieces []placed) []byte {
	var b strings.Builder
	for _, p := range pieces {
		fmt.Fprintf(&b, "BT /F1 %g Tf %g %g Td (%s) Tj ET\n", p.size, p.x, 792-p.y, winAnsi(p.text))
	}
	return minimalPDF(b.String())
}

// winAnsi is s as the contents of a PDF string in WinAnsiEncoding: an
// accented letter is its Latin-1 byte, written as an octal escape.
func winAnsi(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '(' || r == ')' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x80:
			b.WriteRune(r)
		default:
			fmt.Fprintf(&b, "\\%03o", r)
		}
	}
	return b.String()
}

// minimalPDF is a one-page PDF whose page draws content, with its
// cross-reference table built to match. Its font, F1, gives every glyph a
// width of half its size, so a reader knows where each piece of text ends,
// which fpdf's fonts do not tell it.
func minimalPDF(content string) []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
			"/Resources << /Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica " +
			"/Encoding /WinAnsiEncoding /FirstChar 32 /LastChar 255 /Widths [" +
			strings.Repeat("500 ", 224) + "] >> >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, o := range objects {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}

func TestSpanishDatesAsTheBanksPDFsPrintThem(t *testing.T) {
	for in, want := range map[string]string{
		"22-mar-2026":   "2026-03-22",
		"16-abr-2026":   "2026-04-16",
		"1-ene-2027":    "2027-01-01",
		"05-sept-2026":  "2026-09-05",
		"05-set-2026":   "2026-09-05",
		"05-sep-2026":   "2026-09-05",
		"31-DIC-2026":   "2026-12-31",
		"29-feb-2028":   "2028-02-29",
		"02-ago.-2026":  "2026-08-02",
		"10-abril-2026": "2026-04-10",
	} {
		got, err := spanishDate(in)
		if err != nil || got.String() != want {
			t.Errorf("spanishDate(%q) = %s, %v; want %s", in, got, err, want)
		}
	}
	for _, bad := range []string{"31-feb-2026", "29-feb-2026", "22-mar-26", "22-foo-2026", "22/03/2026", "mar-2026", ""} {
		if d, err := spanishDate(bad); err == nil {
			t.Errorf("spanishDate(%q) = %s, want an error", bad, d)
		}
	}
}
