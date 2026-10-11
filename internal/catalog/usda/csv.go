package usda

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
)

// row is one CSV record addressed by column name.
type row struct {
	file string
	line int
	cols map[string]int
	rec  []string
}

// get returns the trimmed value of a column. The column is guaranteed to exist
// because readCSV checks the required set before yielding any row.
func (r row) get(col string) string { return strings.TrimSpace(r.rec[r.cols[col]]) }

// errAt names the file and line of the record that failed, so the operator can
// find it in a multi-hundred-megabyte file.
func (r row) errAt(format string, args ...any) error {
	return fmt.Errorf("%s line %d: %w", r.file, r.line, fmt.Errorf(format, args...))
}

// readCSV streams name from fsys, checks the header carries every required
// column, and calls fn for each record. Extra columns are ignored, so a newer
// FoodData Central release that adds one does not break the import. The first
// error from fn or from the CSV reader stops the read.
func readCSV(fsys fs.FS, name string, required []string, fn func(row) error) (err error) {
	f, err := fsys.Open(name)
	if err != nil {
		return fmt.Errorf("open %s: %w", name, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			err = errors.Join(err, fmt.Errorf("close %s: %w", name, cerr))
		}
	}()

	r := csv.NewReader(f)
	r.ReuseRecord = false
	header, err := r.Read()
	if err != nil {
		return fmt.Errorf("read %s header: %w", name, err)
	}
	cols := make(map[string]int, len(header))
	for i, h := range header {
		cols[strings.TrimPrefix(strings.TrimSpace(h), "\ufeff")] = i
	}
	for _, c := range required {
		if _, ok := cols[c]; !ok {
			return fmt.Errorf("%s: missing required column %q (is this a FoodData Central CSV download?)", name, c)
		}
	}
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		line, _ := r.FieldPos(0)
		if err := fn(row{file: name, line: line, cols: cols, rec: rec}); err != nil {
			return err
		}
	}
}
