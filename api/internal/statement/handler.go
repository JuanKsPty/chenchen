package statement

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/httpx"
	"github.com/JuanKsPty/chenchen/api/internal/logging"
	"github.com/JuanKsPty/chenchen/api/internal/money"
)

// maxUploadSize bounds a statement upload. A year of movements from one bank
// is a few hundred KB. UploadTooLarge names this limit to the person.
const maxUploadSize = 20 << 20

// Handler serves the statement endpoints that need no database.
type Handler struct{}

func NewHandler() *Handler { return &Handler{} }

// Routes returns the /api/imports subrouter.
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Post("/dry-run", h.DryRun)
	return r
}

// DryRun parses an uploaded file and reports what it says, writing nothing.
//
// It exists so a real file can be checked on the real deployment before
// anything depends on the parse being right: identity, period, the bank's own
// balances, and the first line where they stop adding up.
func (h *Handler) DryRun(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		httpx.Fail(w, r, UploadTooLarge().WithCause(err))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.Fail(w, r, MissingFile().WithCause(err))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		httpx.Fail(w, r, UploadUnreadable().WithCause(err))
		return
	}

	parsed, err := Parse(header.Filename, data)
	if err != nil {
		LogParseProblem(r, err)
		httpx.Fail(w, r, ParseProblem(err))
		return
	}
	httpx.JSON(w, r, http.StatusOK, NewFileView(parsed))
}

// The problems an upload can run into, shared by the dry run and the import so
// both say the same thing. The codes are what the interface branches on; the
// messages are for the person, so they name no format, field or bank. What
// actually went wrong is kept as the cause, for the log.

// UploadTooLarge is a body that is over the limit or not a form at all.
func UploadTooLarge() *httpx.Error {
	return httpx.BadRequest("upload_too_large_or_malformed",
		"No pudimos recibir el archivo. Revisa que pese menos de 20 MB y súbelo otra vez.")
}

// MissingFile is a form with no file in it.
func MissingFile() *httpx.Error {
	return httpx.BadRequest("missing_file", "Elige el archivo de tu estado de cuenta.")
}

// UploadUnreadable is a file that arrived but could not be read.
func UploadUnreadable() *httpx.Error {
	return httpx.BadRequest("upload_unreadable", "No pudimos leer el archivo. Elígelo otra vez.")
}

// lineNo finds the line a parser error points at. Every parser names it the
// same way ("line 8"), and it is the one part of a parser's message worth
// passing on.
var lineNo = regexp.MustCompile(`\bline (\d+)\b`)

// ParseProblem turns a Parse failure into what the person reads. The parser's
// own message is technical and in English, so it stays in the log
// (LogParseProblem); the person is told which line, when there is one, and what
// to do about it.
func ParseProblem(err error) *httpx.Error {
	if errors.Is(err, ErrUnrecognised) {
		return httpx.Unprocessable("unrecognised_format",
			"No reconocemos este archivo. Sube el estado de cuenta tal como te lo da tu banco (.csv, .txt o .xlsx).").
			WithCause(err)
	}
	msg := "El archivo tiene un formato que no reconocemos. Descárgalo otra vez de tu banco y súbelo sin abrirlo ni editarlo."
	if m := lineNo.FindStringSubmatch(err.Error()); m != nil {
		msg = fmt.Sprintf("La línea %s del archivo tiene un formato que no reconocemos. "+
			"Descárgalo otra vez de tu banco y súbelo sin abrirlo ni editarlo.", m[1])
	}
	return httpx.Unprocessable("unreadable_statement", msg).WithCause(err)
}

// LogParseProblem records the parser's own message at warn. httpx.Fail logs a
// 4xx's cause only at debug, which the default LOG_LEVEL drops, and this message
// is what it takes to fix a parser against a real statement: nothing else keeps
// it, since a file that cannot be read records no import run.
func LogParseProblem(r *http.Request, err error) {
	logging.FromContext(r.Context()).Warn("statement unreadable", "error", err)
}

// FileView is a parsed file as the API reports it.
type FileView struct {
	Source     Source          `json:"source"`
	Statements []StatementView `json:"statements"`
}

// StatementView summarises one statement: enough to compare with the bank's
// own document at a glance.
type StatementView struct {
	Institution    string          `json:"institution"`
	ExternalNumber string          `json:"external_number"`
	DisplayName    string          `json:"display_name"`
	Class          Class           `json:"class"`
	Type           string          `json:"type"`
	Currency       string          `json:"currency"`
	PeriodStart    civil.Date      `json:"period_start"`
	PeriodEnd      civil.Date      `json:"period_end"`
	Opening        *money.Amount   `json:"opening,omitempty"`
	Closing        *money.Amount   `json:"closing,omitempty"`
	Available      *money.Amount   `json:"available,omitempty"`
	Held           *money.Amount   `json:"held,omitempty"`
	LineCount      int             `json:"line_count"`
	MoneyIn        money.Amount    `json:"money_in"`
	MoneyOut       money.Amount    `json:"money_out"`
	ChainBreak     *chainBreakView `json:"chain_break,omitempty"`
	Warnings       []string        `json:"warnings"`
}

type chainBreakView struct {
	LineNo   int          `json:"line_no"`
	Bank     money.Amount `json:"bank"`
	Computed money.Amount `json:"computed"`
}

// NewFileView builds the report for a parsed file.
func NewFileView(f File) FileView {
	out := FileView{Source: f.Source, Statements: make([]StatementView, 0, len(f.Statements))}
	for _, s := range f.Statements {
		v := StatementView{
			Institution:    s.Identity.Institution,
			ExternalNumber: s.Identity.ExternalNumber,
			DisplayName:    s.Identity.DisplayName,
			Class:          s.Identity.Class,
			Type:           s.Identity.Type,
			Currency:       s.Identity.Currency,
			PeriodStart:    s.PeriodStart,
			PeriodEnd:      s.PeriodEnd,
			Opening:        amount(s.Opening),
			Closing:        amount(s.Closing),
			Available:      amount(s.Available),
			Held:           amount(s.Held),
			LineCount:      len(s.Lines),
			Warnings:       append([]string{}, s.Warnings...),
		}
		var in, outflow money.Cents
		for _, l := range s.Lines {
			if l.Amount > 0 {
				in += l.Amount
			} else {
				outflow -= l.Amount
			}
		}
		v.MoneyIn, v.MoneyOut = in.Amount(), outflow.Amount()
		if b := s.ChainBreak; b != nil {
			v.ChainBreak = &chainBreakView{LineNo: b.LineNo, Bank: b.Bank.Amount(), Computed: b.Computed.Amount()}
		}
		out.Statements = append(out.Statements, v)
	}
	return out
}

func amount(c *money.Cents) *money.Amount {
	if c == nil {
		return nil
	}
	a := c.Amount()
	return &a
}
