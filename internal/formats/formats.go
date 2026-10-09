// Package formats parses and renders the file formats envgrid stores as
// key/value lists: dotenv, json, yaml, properties, ini, php and raw.
//
// Structured formats (json, yaml) are flattened on import: nested objects
// become dotted keys ("db.host") and arrays are stored as compact JSON text.
// Export reverses this. Every value is text; json and yaml exports emit
// numbers, true, false and null without quotes so typed configs survive.
package formats

import (
	"fmt"
	"regexp"
	"strings"
)

// Supported formats.
const (
	Dotenv     = "dotenv"
	JSON       = "json"
	YAML       = "yaml"
	Properties = "properties"
	INI        = "ini"
	PHP        = "php"
	Raw        = "raw"
)

// RawKey is the single implicit key that holds a raw file's whole body.
const RawKey = "__raw__"

// KV is one key and its value.
type KV struct {
	Key   string
	Value string
}

// ParseError reports where parsing failed. It never includes file content,
// because that content is usually secret.
type ParseError struct {
	Line int
	Msg  string
}

func (e *ParseError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("line %d: %s", e.Line, e.Msg)
	}
	return e.Msg
}

// Valid reports whether f is a supported format.
func Valid(f string) bool {
	switch f {
	case Dotenv, JSON, YAML, Properties, INI, PHP, Raw:
		return true
	}
	return false
}

// Label is a format's name for people.
func Label(f string) string {
	switch f {
	case Dotenv:
		return ".env"
	case JSON:
		return "JSON"
	case YAML:
		return "YAML"
	case Properties:
		return ".properties"
	case INI:
		return "INI"
	case PHP:
		return "PHP constants"
	case CSV:
		return "CSV"
	}
	return "plain text"
}

// ExportValid reports whether f can be asked for on export or read on
// import: every file format plus CSV.
func ExportValid(f string) bool { return Valid(f) || f == CSV }

// textual formats hold plain text values, so content in one can be read
// into a file of another. PHP values are PHP expressions and raw files are
// one body, so neither converts.
func textual(f string) bool {
	return f == Dotenv || f == JSON || f == YAML || f == Properties || f == INI || f == CSV
}

// Convertible reports whether content in format from can be imported into a
// file of format to.
func Convertible(from, to string) bool { return textual(from) && textual(to) }

// Ext is the conventional file extension for a format.
func Ext(f string) string {
	switch f {
	case Dotenv:
		return ".env"
	case JSON:
		return ".json"
	case YAML:
		return ".yaml"
	case Properties:
		return ".properties"
	case INI:
		return ".ini"
	case PHP:
		return ".php"
	case CSV:
		return ".csv"
	}
	return ".txt"
}

// ContentType is the media type used when exporting a format.
func ContentType(f string) string {
	switch f {
	case JSON:
		return "application/json; charset=utf-8"
	case YAML:
		return "application/yaml; charset=utf-8"
	case CSV:
		return "text/csv; charset=utf-8"
	}
	return "text/plain; charset=utf-8"
}

// Parse reads body in the given format. Duplicate keys keep their first
// position and their last value, as a shell would.
// MaxKeys caps how many keys one file may parse to, which also bounds how
// long one import holds the database's write lock. Real config files have a
// few hundred keys at most.
const MaxKeys = 2000

// maxDepth caps nesting in JSON and YAML. Empty keys keep the joined name
// short, so the name-length cap alone would not stop deep nesting.
const maxDepth = 64

// maxVisits caps the nodes one YAML walk visits, since aliases can repeat a
// subtree many times over.
const maxVisits = 1 << 16

// maxKeyLen matches ValidateKey; parsers refuse longer joined names early.
const maxKeyLen = 256

func Parse(format string, body []byte) ([]KV, error) {
	var kvs []KV
	var err error
	switch format {
	case Dotenv:
		kvs, err = parseDotenv(string(body))
	case JSON:
		kvs, err = parseJSON(body)
	case YAML:
		kvs, err = parseYAML(body)
	case Properties:
		kvs, err = parseProperties(string(body))
	case INI:
		kvs, err = parseINI(string(body))
	case PHP:
		kvs, err = parsePHP(string(body))
	case CSV:
		kvs, err = parseCSV(string(body))
	case Raw:
		return []KV{{Key: RawKey, Value: string(body)}}, nil
	default:
		return nil, &ParseError{Msg: "unsupported format"}
	}
	if err != nil {
		return nil, err
	}
	if len(kvs) > MaxKeys {
		return nil, &ParseError{Msg: fmt.Sprintf("more than %d keys", MaxKeys)}
	}
	return dedupe(kvs), nil
}

// Export renders kvs in the given format.
func Export(format string, kvs []KV) ([]byte, error) {
	switch format {
	case Dotenv:
		for _, kv := range kvs {
			if !dotenvKey.MatchString(kv.Key) {
				return nil, fmt.Errorf("key %q is not a valid dotenv name", kv.Key)
			}
		}
		return exportDotenv(kvs), nil
	case JSON:
		return exportJSON(kvs), nil
	case YAML:
		return exportYAML(kvs)
	case Properties:
		return exportProperties(kvs), nil
	case CSV:
		return exportCSV(kvs), nil
	case INI:
		for _, kv := range kvs {
			if !validINIKey(kv.Key) {
				return nil, fmt.Errorf("key %q cannot be written to an INI file", kv.Key)
			}
		}
		return exportINI(kvs), nil
	case PHP:
		for _, kv := range kvs {
			if !phpName.MatchString(kv.Key) {
				return nil, fmt.Errorf("key %q is not a valid PHP constant name", kv.Key)
			}
		}
		return exportPHP(kvs), nil
	case Raw:
		for _, kv := range kvs {
			if kv.Key == RawKey {
				return []byte(kv.Value), nil
			}
		}
		return nil, nil
	}
	return nil, fmt.Errorf("unsupported format %q", format)
}

var dotenvKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

// ValidateKey checks a key name for a file of the given format.
func ValidateKey(format, name string) error {
	if name == "" || len(name) > 256 {
		return fmt.Errorf("key name must be 1 to 256 characters")
	}
	if name == RawKey || format == Raw {
		return fmt.Errorf("raw files have a single implicit key")
	}
	if strings.TrimSpace(name) != name {
		return fmt.Errorf("key name must not start or end with whitespace")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("key name must not contain control characters")
		}
	}
	if format == Dotenv && !dotenvKey.MatchString(name) {
		return fmt.Errorf("dotenv keys must start with a letter or underscore and contain only letters, digits, '_', '.' or '-'")
	}
	if format == INI && !validINIKey(name) {
		return fmt.Errorf("INI keys cannot contain = : [ ] ; # or quotes, or empty parts between dots")
	}
	if format == PHP && !phpName.MatchString(name) {
		return fmt.Errorf("PHP constant names must start with a letter or underscore and contain only letters, digits and '_'")
	}
	return nil
}

// ValidateValue checks a value before it is stored in a file of the given
// format. Only PHP restricts values: each must be one PHP expression.
func ValidateValue(format, value string) error {
	if format == PHP {
		return validatePHPValue(value)
	}
	return nil
}

func dedupe(kvs []KV) []KV {
	index := make(map[string]int, len(kvs))
	out := kvs[:0:0]
	for _, kv := range kvs {
		if i, ok := index[kv.Key]; ok {
			out[i].Value = kv.Value
			continue
		}
		index[kv.Key] = len(out)
		out = append(out, kv)
	}
	return out
}
