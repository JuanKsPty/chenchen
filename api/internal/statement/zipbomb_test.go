package statement

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"io"
	"runtime"
	"strings"
	"testing"
)

// paddedWorkbook is bgAccountFile's workbook with pad bytes of blank space
// inside its sheet: valid XML that unzips to whatever size it is told, from a
// few hundred KB on the wire. When lie is set, the zip declares the sheet a
// few KB, as a hand-made bomb would to slip under a limit read off the
// headers.
func paddedWorkbook(t *testing.T, pad int, lie bool) []byte {
	t.Helper()
	workbook := bgAccountFile(t, bgFixture)
	src, err := zip.NewReader(bytes.NewReader(workbook), int64(len(workbook)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	for _, f := range src.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(rc)
		if err != nil {
			t.Fatal(err)
		}
		_ = rc.Close()
		if f.Name != "xl/worksheets/sheet1.xml" {
			w, err := z.Create(f.Name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write(body); err != nil {
				t.Fatal(err)
			}
			continue
		}

		head, tail, ok := strings.Cut(string(body), "</worksheet>")
		if !ok {
			t.Fatal("fixture: the sheet has no closing tag")
		}
		var sheet bytes.Buffer
		fw, err := flate.NewWriter(&sheet, flate.BestCompression)
		if err != nil {
			t.Fatal(err)
		}
		size := len(head) + pad + len("</worksheet>") + len(tail)
		write := func(s string) {
			if _, err := fw.Write([]byte(s)); err != nil {
				t.Fatal(err)
			}
		}
		write(head)
		blank := strings.Repeat(" ", 1<<20)
		for left := pad; left > 0; left -= len(blank) {
			write(blank[:min(left, len(blank))])
		}
		write("</worksheet>" + tail)
		if err := fw.Close(); err != nil {
			t.Fatal(err)
		}
		declared := uint64(size)
		if lie {
			declared = 4096
		}
		w, err := z.CreateRaw(&zip.FileHeader{Name: f.Name, Method: zip.Deflate,
			CompressedSize64: uint64(sheet.Len()), UncompressedSize64: declared})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(sheet.Bytes()); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// A workbook that unzips to more than the API can hold is refused before it
// is inflated, and one that lies about its size is read only as far as it
// claims: either way, never a process out of memory.
func TestAZipBombIsCut(t *testing.T) {
	// The padding itself is harmless: the same workbook a little padded reads.
	f, err := Parse("ULTIMOS-MOVIMIENTOS.xlsx", paddedWorkbook(t, 1<<20, false))
	if err != nil || len(only(t, f).Lines) != len(bgFixture) {
		t.Fatalf("a padded workbook = %+v, %v; want it read", f, err)
	}

	for _, lie := range []bool{false, true} {
		bomb := paddedWorkbook(t, 80<<20, lie)
		if len(bomb) > 1<<20 {
			t.Fatalf("fixture: the bomb weighs %d bytes", len(bomb))
		}
		allocated, err := allocatedBy(func() error {
			_, err := Parse("ULTIMOS-MOVIMIENTOS.xlsx", bomb)
			return err
		})
		if !lie && (err == nil || !strings.Contains(err.Error(), "exceeds")) {
			t.Errorf("an 80 MB sheet = %v; want it refused for its size", err)
		}
		if allocated > 16<<20 {
			t.Errorf("lying %v: reading an 80 MB sheet allocated %d MB", lie, allocated>>20)
		}
	}
}

// allocatedBy runs fn and reports how many bytes it allocated.
func allocatedBy(fn func() error) (uint64, error) {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	err := fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc, err
}
