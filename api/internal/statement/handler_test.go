package statement

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/JuanKsPty/chenchen/api/internal/logging"
)

func upload(t *testing.T, name string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	fw.Write(data)
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/dry-run", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	NewHandler().Routes().ServeHTTP(rec, req)
	return rec
}

func TestDryRunReportsTheBanksBalances(t *testing.T) {
	rec := upload(t, "estado.csv", bacFile("0.00", "250.75", bacDetail...))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var got FileView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	s := got.Statements[0]
	if got.Source != SourceBACAccount || s.ExternalNumber != "100000001" || s.LineCount != 2 {
		t.Errorf("report = %+v", got)
	}
	if s.Opening == nil || s.Opening.Formatted != "100.00" || s.Closing.Formatted != "250.75" {
		t.Errorf("opening/closing = %+v / %+v", s.Opening, s.Closing)
	}
	if s.PeriodStart.String() != "2026-06-15" || s.Warnings == nil {
		t.Errorf("period start %s, warnings %v", s.PeriodStart, s.Warnings)
	}
}

func TestDryRunRejectsAnUnknownFormat(t *testing.T) {
	rec := upload(t, "otro.csv", []byte("a,b\n1,2"))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", rec.Code)
	}
	code, message := problem(t, rec)
	if code != "unrecognised_format" || !strings.Contains(message, "(.csv, .txt o .xlsx)") {
		t.Errorf("problem = %s %q", code, message)
	}
}

func TestDryRunAsksForTheExportThatNamesTheAccount(t *testing.T) {
	txt := "Fecha tran;Referencia;Descripcion;Cargos (Db);Pagos (Cr);Saldo total\r\n" +
		"01/03/2026;452;YAPPY BG A PERSONA DE PRUEBA;-12.5;;87.5\r\n"
	rec := upload(t, "movimientos.txt", []byte(txt))
	code, message := problem(t, rec)
	if rec.Code != http.StatusUnprocessableEntity || code != "account_not_stated" || !strings.Contains(message, ".xlsx") {
		t.Errorf("status %d, problem = %s %q", rec.Code, code, message)
	}
}

// The parser's own message is technical and in English. The person reads the
// line it points at and what to do, and nothing about the format or the bank.
func TestDryRunNamesTheLineItCannotRead(t *testing.T) {
	broken := "99/99/2026, 1000002, 4A, TRANSFERENCIA DE PRUEBA, 25.00, 0.00, 75.00"
	rec := upload(t, "estado.csv", bacFile("0.00", "250.75", bacDetail[0], broken))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	code, message := problem(t, rec)
	if code != "unreadable_statement" {
		t.Errorf("code = %q, want unreadable_statement", code)
	}
	if !regexp.MustCompile(`^La línea \d+ del archivo `).MatchString(message) {
		t.Errorf("message = %q, want it to name the line", message)
	}
	for _, leak := range []string{"statement", "line", "BAC", "fecha"} {
		if strings.Contains(message, leak) {
			t.Errorf("message = %q, carries the parser's %q", message, leak)
		}
	}
}

// What the person no longer reads is what fixes a parser, so it has to reach a
// log kept at the default level (info), not only a debug one.
func TestDryRunLogsTheParsersOwnMessageAtInfo(t *testing.T) {
	broken := "99/99/2026, 1000002, 4A, TRANSFERENCIA DE PRUEBA, 25.00, 0.00, 75.00"
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "estado.csv")
	if err != nil {
		t.Fatal(err)
	}
	fw.Write(bacFile("0.00", "250.75", bacDetail[0], broken))
	mw.Close()
	var log bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&log, &slog.HandlerOptions{Level: slog.LevelInfo}))
	req := httptest.NewRequest(http.MethodPost, "/dry-run", &body)
	req = req.WithContext(logging.WithLogger(req.Context(), logger))
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	NewHandler().Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if !strings.Contains(log.String(), "level=WARN") || !strings.Contains(log.String(), "99/99/2026") {
		t.Errorf("log = %q, want the parser's message at warn", log.String())
	}
}

func TestDryRunWithoutAFile(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/dry-run", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	NewHandler().Routes().ServeHTTP(rec, req)
	code, message := problem(t, rec)
	if rec.Code != http.StatusBadRequest || code != "upload_too_large_or_malformed" || strings.Contains(message, "multipart") {
		t.Errorf("%d %s %q", rec.Code, code, message)
	}
}

func problem(t *testing.T, rec *httptest.ResponseRecorder) (code, message string) {
	t.Helper()
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %s: %v", rec.Body, err)
	}
	return body.Error.Code, body.Error.Message
}
