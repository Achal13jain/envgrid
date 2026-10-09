package formats

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"strings"
)

// CSV is an import and export format only: two columns, key and value,
// with an optional "key,value" header. Content in it is converted into a
// file's own format, so it is never a file's stored format. A line break
// written as CRLF inside a quoted value is read back as LF, which is how
// encoding/csv reads it.
const CSV = "csv"

func parseCSV(src string) ([]KV, error) {
	r := csv.NewReader(strings.NewReader(src))
	r.FieldsPerRecord = -1
	var out []KV
	for line := 1; ; line++ {
		rec, err := r.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, &ParseError{Line: line, Msg: "invalid CSV"}
		}
		if len(rec) != 2 {
			return nil, &ParseError{Line: line, Msg: "expected two columns: key,value"}
		}
		if line == 1 && strings.EqualFold(strings.TrimSpace(rec[0]), "key") && strings.EqualFold(strings.TrimSpace(rec[1]), "value") {
			continue
		}
		out = append(out, KV{Key: strings.TrimSpace(rec[0]), Value: rec[1]})
	}
	return out, nil
}

func exportCSV(kvs []KV) []byte {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"key", "value"})
	for _, kv := range kvs {
		_ = w.Write([]string{kv.Key, kv.Value})
	}
	w.Flush()
	return b.Bytes()
}

// looksLikeCSV is true when every record has two fields and the first reads
// as a key name, which rules out .env and properties lines.
func looksLikeCSV(text string) bool {
	r := csv.NewReader(strings.NewReader(text))
	r.FieldsPerRecord = 2
	recs, err := r.ReadAll()
	if err != nil || len(recs) == 0 {
		return false
	}
	for _, rec := range recs {
		k := strings.TrimSpace(rec[0])
		if k == "" || strings.ContainsAny(k, "= \t:{}[]") {
			return false
		}
	}
	return true
}
