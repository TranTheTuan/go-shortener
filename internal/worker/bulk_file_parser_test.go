package worker

import (
	"bytes"
	"testing"
)

// roundTrip writes rows then reads them back and asserts equality.
func roundTrip(t *testing.T, rows [][]string, ext string) {
	t.Helper()
	buf, _, err := Write(rows, ext)
	if err != nil {
		t.Fatalf("Write(%s): %v", ext, err)
	}
	got, err := Read(bytes.NewReader(buf.Bytes()), ext)
	if err != nil {
		t.Fatalf("Read(%s): %v", ext, err)
	}
	if len(got) != len(rows) {
		t.Fatalf("row count: want %d, got %d", len(rows), len(got))
	}
	for i, row := range rows {
		if len(got[i]) != len(row) {
			t.Fatalf("row %d col count: want %d, got %d", i, len(row), len(got[i]))
		}
		for j, v := range row {
			if got[i][j] != v {
				t.Errorf("row %d col %d: want %q, got %q", i, j, v, got[i][j])
			}
		}
	}
}

// Note: excelize.GetRows trims trailing empty cells, so all cells must be
// non-empty for a lossless XLSX round-trip.
var testRows = [][]string{
	{"url", "result"},
	{"https://example.com", "http://short/abc"},
	{"https://go.dev", "lỗi xử lý"},
}

func TestRoundTripCSV(t *testing.T)  { roundTrip(t, testRows, ".csv") }
func TestRoundTripXLSX(t *testing.T) { roundTrip(t, testRows, ".xlsx") }

func TestReadUnsupportedExt(t *testing.T) {
	_, err := Read(bytes.NewReader(nil), ".txt")
	if err == nil {
		t.Fatal("want error for unsupported ext")
	}
}

func TestWriteUnsupportedExt(t *testing.T) {
	_, _, err := Write(testRows, ".txt")
	if err == nil {
		t.Fatal("want error for unsupported ext")
	}
}

func TestDeriveResultKey(t *testing.T) {
	cases := []struct {
		key, ext, want string
	}{
		{"uploads/abc123.csv", ".csv", "uploads/abc123-result.csv"},
		{"uploads/abc123.xlsx", ".xlsx", "uploads/abc123-result.xlsx"},
	}
	for _, c := range cases {
		got := deriveResultKey(c.key, c.ext)
		if got != c.want {
			t.Errorf("deriveResultKey(%q, %q) = %q, want %q", c.key, c.ext, got, c.want)
		}
	}
}
