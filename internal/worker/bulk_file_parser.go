package worker

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"

	"github.com/xuri/excelize/v2"
)

// Read parses a CSV or XLSX reader into rows (including the header row).
// ext must be ".csv" or ".xlsx".
func Read(r io.Reader, ext string) ([][]string, error) {
	switch ext {
	case ".csv":
		return csv.NewReader(r).ReadAll()
	case ".xlsx":
		f, err := excelize.OpenReader(r)
		if err != nil {
			return nil, fmt.Errorf("bulk parser: open xlsx: %w", err)
		}
		defer f.Close()
		sheets := f.GetSheetList()
		if len(sheets) == 0 {
			return nil, fmt.Errorf("bulk parser: xlsx has no sheets")
		}
		return f.GetRows(sheets[0])
	default:
		return nil, fmt.Errorf("bulk parser: unsupported ext %q", ext)
	}
}

// Write serialises rows back to a buffer with the matching content-type.
func Write(rows [][]string, ext string) (*bytes.Buffer, string, error) {
	buf := &bytes.Buffer{}
	switch ext {
	case ".csv":
		w := csv.NewWriter(buf)
		if err := w.WriteAll(rows); err != nil {
			return nil, "", fmt.Errorf("bulk parser: write csv: %w", err)
		}
		return buf, "text/csv", nil
	case ".xlsx":
		f := excelize.NewFile()
		defer f.Close()
		sheet := f.GetSheetName(0)
		for i, row := range rows {
			cell, _ := excelize.CoordinatesToCellName(1, i+1)
			vals := make([]interface{}, len(row))
			for j, v := range row {
				vals[j] = v
			}
			if err := f.SetSheetRow(sheet, cell, &vals); err != nil {
				return nil, "", fmt.Errorf("bulk parser: write xlsx row %d: %w", i, err)
			}
		}
		if _, err := f.WriteTo(buf); err != nil {
			return nil, "", fmt.Errorf("bulk parser: write xlsx: %w", err)
		}
		return buf, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", nil
	default:
		return nil, "", fmt.Errorf("bulk parser: unsupported ext %q", ext)
	}
}
