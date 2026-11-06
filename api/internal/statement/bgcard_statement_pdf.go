package statement

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ledongthuc/pdf"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
)

// pdfHeaderWindow is how far into a file its "%PDF-" header may start. PDF
// readers accept a header after some leading bytes, which a download or a
// mail gateway can add, and the file's own offsets count from the header.
const pdfHeaderWindow = 1024

// pdfStart is where data's PDF header starts, or -1 when its first
// pdfHeaderWindow bytes hold none.
func pdfStart(data []byte) int {
	return bytes.Index(data[:min(len(data), pdfHeaderWindow)], []byte("%PDF-"))
}

// parsePDF reads a PDF upload. A BG card's closed statement is the only PDF
// read, and a PDF is one only when its first page has the statement's title
// and summary (cardStatementLayout): the reader names no bank, so any other
// PDF that opens is a format we do not know, never a statement misread. A PDF
// that needs a password or is malformed is an unreadable statement.
func parsePDF(data []byte) (file File, err error) {
	defer func() {
		// The PDF library panics on some malformed files instead of
		// returning an error; such a file is unreadable like any other.
		if r := recover(); r != nil {
			file, err = File{}, fmt.Errorf("statement: the PDF is malformed: %v", r)
		}
	}()
	if start := pdfStart(data); start > 0 {
		data = data[start:]
	}
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return File{}, fmt.Errorf("statement: opening the PDF: %w", err)
	}
	page, ok := cardStatementLayout(pdfRows(r, cardStatementPages))
	if !ok {
		return File{}, fmt.Errorf("%w: a PDF without a card statement's title and summary", ErrUnrecognised)
	}
	return parseBGCardStatement(page)
}

// parseBGCardStatement reads a BG credit card's closed statement, as a PDF,
// for the one thing no other export of the card prints: its balance.
//
// Only the summary is read: the cut date ("Estado de cuenta de 22-mar-2026"),
// the card account ("**** 1234", the first one, under the card's name) and the
// row of values under "Deuda anterior … Deuda al corte". The bank prints debt,
// and a debt D is a balance of -D for the holder: "Deuda al corte $25.00"
// closes at -25.00, and "Deuda al corte -$4.10", money the bank owes the
// holder, closes at +4.10.
//
// The statement has a Closing and no Opening. Its period is the cut day, and
// "Deuda anterior" is the debt at the previous cut, a month earlier: as the
// opening of the cut day it would be a balance on the wrong day. It is read
// only to check that the summary adds up.
//
// The movements the PDF lists are ignored on purpose. They carry no
// reference, so their dedup keys would never match the same movements from
// the text or Excel export, and importing both would count each one twice.
// The statement therefore has no lines, and importing it gives the card a
// closing checkpoint on the cut day, which the lines from those exports are
// then measured against.
//
// A summary with no debt at the cut, or a cut that is not a date, is an
// error, never a guess.
func parseBGCardStatement(page cardStatementPage) (File, error) {
	cut, err := spanishDate(page.cut)
	if err != nil {
		return File{}, err
	}
	account, err := cardStatementAccount(page.rows)
	if err != nil {
		return File{}, err
	}
	summary, err := cardStatementSummary(page.labels, page.values)
	if err != nil {
		return File{}, err
	}
	atCut, err := summary.amount("deuda al corte")
	if err != nil {
		return File{}, err
	}

	st := Statement{
		Identity:    bgCardIdentity(account),
		PeriodStart: cut,
		PeriodEnd:   cut,
		Closing:     cents(-atCut),
	}
	// The summary adds up on its own; a row that does not was misread, or
	// the bank changed what its columns mean.
	previous, errA := summary.amount("deuda anterior")
	purchases, errP := summary.amount("compras y cargos")
	payments, errQ := summary.amount("pagos")
	if errA == nil && errP == nil && errQ == nil && previous-purchases-payments != atCut {
		st.Warnings = append(st.Warnings, fmt.Sprintf(
			"El resumen del estado no cuadra: con una deuda anterior de %s, compras y cargos de %s y pagos de %s, "+
				"la deuda al corte sería %s y el banco muestra %s.",
			previous, purchases, payments, previous-purchases-payments, atCut))
	}
	return File{Source: SourceBGCardStatement, Statements: []Statement{st}}, nil
}

// ContentKey is what identifies the file when its bytes cannot: the bank
// stamps each download of a card statement PDF with the time it was made, so
// two downloads of the same statement never share a checksum. For such a file
// the key is what it says (the card, the cut day and the debt at the cut), and
// importing it again is recognised as nothing new. ok is false for every other
// format, whose bytes are already the same each time.
func (f File) ContentKey() (key string, ok bool) {
	if f.Source != SourceBGCardStatement || len(f.Statements) != 1 {
		return "", false
	}
	st := f.Statements[0]
	if st.Closing == nil {
		return "", false
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%s|%d", f.Source, st.Identity.Institution,
		st.Identity.ExternalNumber, st.PeriodEnd, *st.Closing)))
	return hex.EncodeToString(sum[:]), true
}

// cardStatementPages is how many pages are read: the cut date, the account and
// the summary are on the first, and the rest are movements, which are not.
const cardStatementPages = 1

// cardStatementLabels are the summary's labels, every one of which a closed
// statement's first page prints in one row.
var cardStatementLabels = []string{
	"deuda anterior", "compras y cargos", "pagos", "deuda al corte", "pago minimo", "fecha de pago",
}

// The patterns read text squashed (squash), so they hold however the PDF
// spaced its words.
var (
	cardStatementCutPattern     = regexp.MustCompile(`estadodecuentade(\d{1,2}-[a-z]+\.?-\d{4})`)
	cardStatementAccountPattern = regexp.MustCompile(`\*{4}(\d{4})`)
)

// cardStatementPage is the first page of a closed statement, as far as it is
// read.
type cardStatementPage struct {
	rows []pdfRow
	// cut is the title's date, as printed.
	cut string
	// labels is the summary's row of labels, and values the runs below it.
	labels, values pdfRow
}

// summaryBand is how far apart, in points, the baselines of one row of the
// summary can be: a label or value drawn a little higher or lower than the
// rest is still in its row.
const summaryBand = 4.0

// cardStatementLayout finds what makes a PDF a card's closed statement: the
// "Estado de cuenta de <date>" title and a row with every summary label. ok is
// false for any PDF that lacks either. The values are the first row below the
// labels, with the rows within summaryBand under it; the label row likewise
// takes in the rows within summaryBand of it.
func cardStatementLayout(rows []pdfRow) (page cardStatementPage, ok bool) {
	page.rows = rows
	for _, row := range rows {
		if m := cardStatementCutPattern.FindStringSubmatch(squash(row.String())); m != nil {
			page.cut = m[1]
			break
		}
	}
	if page.cut == "" {
		return page, false
	}
	for i, row := range rows {
		if !row.has("deuda anterior") {
			continue
		}
		from, to := i, i+1
		for from > 0 && rows[from-1].baseline()-row.baseline() <= summaryBand {
			from--
		}
		for to < len(rows) && row.baseline()-rows[to].baseline() <= summaryBand {
			to++
		}
		labels := merge(rows[from:to])
		if !labels.hasAll(cardStatementLabels) {
			continue
		}
		page.labels = labels
		if to < len(rows) {
			end := to + 1
			for end < len(rows) && rows[to].baseline()-rows[end].baseline() <= summaryBand {
				end++
			}
			page.values = merge(rows[to:end])
		}
		return page, true
	}
	return page, false
}

// cardStatementAccount finds the card account's number: the first one the
// page shows, under the card's name. The ones further down head the
// movements of each card on the account.
func cardStatementAccount(rows []pdfRow) (string, error) {
	for _, row := range rows {
		if m := cardStatementAccountPattern.FindStringSubmatch(squash(row.String())); m != nil {
			return "**** " + m[1], nil
		}
	}
	return "", errors.New(`statement: the card statement PDF has no "**** 1234" account`)
}

// cardSummary is the summary's values, as printed, by their label squashed.
type cardSummary map[string]string

// cardStatementSummary pairs each value with the label whose column holds the
// value's centre (pdfRow.column). The order the PDF draws them in is not the
// order they read in, and a value may sit at its label's start, under its
// middle or against the column's right edge.
func cardStatementSummary(labels, values pdfRow) (cardSummary, error) {
	out := cardSummary{}
	for _, value := range values {
		label := labels.column((value.x + value.end) / 2)
		if _, taken := out[squash(label.text)]; taken {
			return nil, fmt.Errorf("statement: the card statement PDF has two values under %q", fold(label.text))
		}
		out[squash(label.text)] = value.text
	}
	return out, nil
}

// amount is the summary's value under label: "$25.00", "-$4.10".
func (s cardSummary) amount(label string) (money.Cents, error) {
	v, ok := s[squash(label)]
	if !ok {
		return 0, fmt.Errorf("statement: the card statement PDF has no value under %q", label)
	}
	c, err := signedFromText(strings.ReplaceAll(v, "$", ""))
	if err != nil {
		return 0, fmt.Errorf("statement: the card statement PDF's %q: %w", label, err)
	}
	return c, nil
}

// squash is s folded and with no spaces at all, for text a PDF may draw with
// its words glued together or spread apart: "Deudaanterior" and
// "Deuda  anterior" both read "deudaanterior".
func squash(s string) string {
	return strings.Join(strings.Fields(fold(s)), "")
}

// spanishMonths are the month names the bank's PDFs print, abbreviated or not.
var spanishMonths = map[string]time.Month{
	"ene": time.January, "enero": time.January,
	"feb": time.February, "febrero": time.February,
	"mar": time.March, "marzo": time.March,
	"abr": time.April, "abril": time.April,
	"may": time.May, "mayo": time.May,
	"jun": time.June, "junio": time.June,
	"jul": time.July, "julio": time.July,
	"ago": time.August, "agosto": time.August,
	"sep": time.September, "sept": time.September, "set": time.September,
	"septiembre": time.September, "setiembre": time.September,
	"oct": time.October, "octubre": time.October,
	"nov": time.November, "noviembre": time.November,
	"dic": time.December, "diciembre": time.December,
}

var spanishDatePattern = regexp.MustCompile(`^(\d{1,2})-([a-z]+)\.?-(\d{4})$`)

// spanishDate reads a date the way the bank's PDFs print it: "22-mar-2026",
// "05-sept-2026".
func spanishDate(s string) (civil.Date, error) {
	m := spanishDatePattern.FindStringSubmatch(fold(s))
	if m == nil {
		return civil.Date{}, fmt.Errorf("statement: %q is not a date", s)
	}
	month, ok := spanishMonths[m[2]]
	if !ok {
		return civil.Date{}, fmt.Errorf("statement: %q has no month we know", s)
	}
	day, _ := strconv.Atoi(m[1])
	year, _ := strconv.Atoi(m[3])
	d := civil.Of(time.Date(year, month, day, 0, 0, 0, 0, time.UTC))
	if d.Day != day {
		return civil.Date{}, fmt.Errorf("statement: %q is not a day of its month", s)
	}
	return d, nil
}

// pdfRun is text a PDF shows in one piece, at one place on its page.
type pdfRun struct {
	// x is where the run starts and end where its last glyph ends, or
	// starts when the font gives no widths.
	x, end, y float64
	text      string
}

// pdfRow is the runs on one line of a page, left to right.
type pdfRow []pdfRun

func (r pdfRow) String() string {
	parts := make([]string, len(r))
	for i, run := range r {
		parts[i] = run.text
	}
	return strings.Join(parts, " ")
}

// baseline is the row's height on the page, from the bottom.
func (r pdfRow) baseline() float64 { return r[0].y }

// has reports whether one of the row's runs reads label, however it is spaced.
func (r pdfRow) has(label string) bool {
	for _, run := range r {
		if squash(run.text) == squash(label) {
			return true
		}
	}
	return false
}

// hasAll reports whether the row has every one of labels.
func (r pdfRow) hasAll(labels []string) bool {
	for _, label := range labels {
		if !r.has(label) {
			return false
		}
	}
	return true
}

// column is the run, of a row of labels, whose column holds x: the one whose
// own span holds it, else the one it falls between the start of and the next
// label's start. Left of every label, the first is the nearest.
func (r pdfRow) column(x float64) pdfRun {
	for _, run := range r {
		if run.x <= x && x <= run.end {
			return run
		}
	}
	for i := len(r) - 1; i >= 0; i-- {
		if r[i].x <= x {
			return r[i]
		}
	}
	return r[0]
}

// merge is the runs of rows as one row, left to right.
func merge(rows []pdfRow) pdfRow {
	var out pdfRow
	for _, row := range rows {
		out = append(out, row...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].x < out[j].x })
	return out
}

// pdfRows reads up to pages pages of a PDF as rows of text, top to bottom.
func pdfRows(r *pdf.Reader, pages int) []pdfRow {
	var out []pdfRow
	for i := 1; i <= min(pages, r.NumPage()); i++ {
		if p := r.Page(i); !p.V.IsNull() {
			out = append(out, pageRows(p)...)
		}
	}
	return out
}

// rowTolerance is how far apart, in points, two runs' baselines can be and
// still be on one row.
const rowTolerance = 1.0

// wordGap is the share of the font size that a gap between two glyphs of one
// run must reach to be read as a space. Some PDFs place each word on its own
// and draw no space glyph between them; a space is about a quarter of the
// font size, and the letters of a word sit much closer.
const wordGap = 0.2

// pageRows groups a page's glyphs into runs and the runs into rows. A glyph
// continues the run before it when it is on the same baseline and starts
// within half its size of where that one ended — or at the same place, when
// the font gives no widths. A gap of wordGap or more within a run is a space.
func pageRows(p pdf.Page) []pdfRow {
	var runs []pdfRun
	var last pdf.Text
	open := false
	for _, g := range p.Content().Text {
		if g.S == "\n" {
			open = false
			continue
		}
		end := last.X + last.W
		if open && math.Abs(g.Y-last.Y) < rowTolerance/2 &&
			(g.X == last.X || math.Abs(g.X-end) < g.FontSize/2) {
			run := &runs[len(runs)-1]
			// Only a glyph with a width says where it ends, and so
			// how wide the gap after it is.
			if last.W > 0 && g.X-end >= wordGap*g.FontSize && g.S != " " && last.S != " " {
				run.text += " "
			}
			run.text += g.S
			run.end = max(run.end, g.X+g.W)
		} else {
			runs = append(runs, pdfRun{x: g.X, end: g.X + g.W, y: g.Y, text: g.S})
			open = true
		}
		last = g
	}
	for i := range runs {
		runs[i].text = strings.TrimSpace(runs[i].text)
	}

	sort.SliceStable(runs, func(i, j int) bool { return runs[i].y > runs[j].y })
	var rows []pdfRow
	for _, run := range runs {
		if run.text == "" {
			continue
		}
		if n := len(rows); n > 0 && rows[n-1][0].y-run.y <= rowTolerance {
			rows[n-1] = append(rows[n-1], run)
			continue
		}
		rows = append(rows, pdfRow{run})
	}
	for _, row := range rows {
		sort.SliceStable(row, func(i, j int) bool { return row[i].x < row[j].x })
	}
	return rows
}
