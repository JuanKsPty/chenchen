package statement

import (
	"strconv"
	"strings"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
)

// bgCardSheetTitle is the title cell of the card's Excel exports, both the
// recent movements and a closed statement's. The account's says "Últimos
// movimientos" instead.
const bgCardSheetTitle = "movimientos de tarjetas de credito"

// bgCardSheetColumns maps the card sheet's headers, folded, to the text
// export's names for the same columns. The sheet writes them with accents,
// trailing spaces and, after "Tarjeta", a tab, and calls the transaction date
// "Fecha transacción" where the text says "Fecha tran".
var bgCardSheetColumns = map[string]string{
	"cuenta":            "Cuenta",
	"tarjeta":           "Tarjeta",
	"fecha transaccion": "Fecha tran",
	"fecha tran":        "Fecha tran",
	"fecha proceso":     "Fecha proceso",
	"descripcion":       "Descripcion",
	"referencia":        "Referencia",
	"categoria":         "Categoria",
	"cargos (db)":       "Cargos (Db)",
	"pagos (cr)":        "Pagos (Cr)",
}

// isBGCardSheet tells a card's sheet from the account's, which is an .xlsx
// too: by its title, or by a header row with every card column. The card's
// sheet also names the account in a "Cuenta : <card> **** 1234" cell, but each
// row carries its own Cuenta, which is what the lines are filed by.
func isBGCardSheet(rows [][]string) bool {
	for _, row := range rows {
		for _, cell := range row {
			if fold(cell) == bgCardSheetTitle {
				return true
			}
		}
	}
	_, col := bgCardSheetHeader(rows)
	return len(col) == len(bgCardColumns)
}

// parseBGCardSheet reads a card's Excel export: the recent movements or a
// closed statement's, which share one layout. Its rows are the text export's
// rows under other headers, so they go through the same bgCardTable and come
// out as the same lines, with the same dedup keys.
func parseBGCardSheet(rows [][]string) (File, error) {
	header, col := bgCardSheetHeader(rows)
	return bgCardTable{
		col:       col,
		rows:      rows[header+1:],
		firstLine: header + 2,
		date:      bgCardSheetDate,
		amount:    sheetAmount,
	}.file()
}

// bgCardSheetHeader finds the header row: the first with every card column,
// or else the one with the most, so that reading it names the column missing.
// Some of its cells span several columns; the empty ones between are ignored.
func bgCardSheetHeader(rows [][]string) (int, map[string]int) {
	header, col := -1, map[string]int{}
	for i, row := range rows {
		if c := bgCardSheetIndex(row); len(c) > len(col) {
			header, col = i, c
		}
		if len(col) == len(bgCardColumns) {
			break
		}
	}
	return header, col
}

// bgCardSheetIndex maps the text export's column names to where row has them.
func bgCardSheetIndex(row []string) map[string]int {
	col := map[string]int{}
	for i, cell := range row {
		if name, ok := bgCardSheetColumns[fold(cell)]; ok {
			if _, seen := col[name]; !seen {
				col[name] = i
			}
		}
	}
	return col
}

// bgCardSheetDate reads a card sheet's date cell. The bank writes it as text,
// "02/11/2026"; a sheet saved again by a spreadsheet program can hold the
// serial number instead.
func bgCardSheetDate(cell string) (civil.Date, error) {
	if d, err := civil.ParseLayout("02/01/2006", cell); err == nil {
		return d, nil
	}
	return excelDate(cell)
}

// sheetAmount reads an amount cell: a number, float noise included, or text
// written the way the text export writes it.
func sheetAmount(cell string) (money.Cents, error) {
	if _, err := strconv.ParseFloat(cell, 64); err == nil {
		return centsFromFloat(cell)
	}
	return signedFromText(cell)
}

// accents maps the accented letters the banks' headers use to plain ones.
var accents = strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u")

// fold reduces a label to what identifies it: words separated by one space,
// no surrounding spaces or tabs, lower case and no accents.
func fold(s string) string {
	return accents.Replace(normalise(s))
}
