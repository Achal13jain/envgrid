package formats

import (
	"fmt"
	"strings"
)

// parseINI reads INI files: [section] headers, key = value (or key: value),
// ';' and '#' comments, and quoted values. Keys inside a section become
// "section.key", like nested JSON keys.
func parseINI(src string) ([]KV, error) {
	var out []KV
	section := ""
	for n, raw := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || line[0] == ';' || line[0] == '#' {
			continue
		}
		if line[0] == '[' {
			end := strings.IndexByte(line, ']')
			if end < 0 {
				return nil, &ParseError{Line: n + 1, Msg: "unclosed [section]"}
			}
			section = strings.TrimSpace(line[1:end])
			continue
		}
		sep := strings.IndexAny(line, "=:")
		if sep <= 0 {
			return nil, &ParseError{Line: n + 1, Msg: "expected key = value"}
		}
		key := strings.TrimSpace(line[:sep])
		val, err := iniValue(strings.TrimSpace(line[sep+1:]))
		if err != nil {
			return nil, &ParseError{Line: n + 1, Msg: err.Error()}
		}
		if section != "" {
			if len(section) > maxKeyLen {
				return nil, &ParseError{Line: n + 1, Msg: "section name over 256 characters"}
			}
			key = section + "." + key
		}
		out = append(out, KV{Key: key, Value: val})
	}
	return out, nil
}

func iniValue(v string) (string, error) {
	if len(v) > 0 && (v[0] == '"' || v[0] == '\'') {
		q := v[0]
		var b strings.Builder
		for i := 1; i < len(v); i++ {
			c := v[i]
			if c == q {
				rest := strings.TrimSpace(v[i+1:])
				if rest != "" && rest[0] != ';' && rest[0] != '#' {
					return "", fmt.Errorf("unexpected characters after closing quote")
				}
				return b.String(), nil
			}
			if c == '\\' && q == '"' && i+1 < len(v) {
				i++
				switch v[i] {
				case 'n':
					b.WriteByte('\n')
				case 'r':
					b.WriteByte('\r')
				case 't':
					b.WriteByte('\t')
				default:
					b.WriteByte(v[i])
				}
				continue
			}
			b.WriteByte(c)
		}
		return "", fmt.Errorf("unterminated quoted value")
	}
	// Unquoted: an inline comment starts at whitespace followed by ; or #.
	for i := 1; i < len(v); i++ {
		if (v[i] == ';' || v[i] == '#') && (v[i-1] == ' ' || v[i-1] == '\t') {
			return strings.TrimSpace(v[:i]), nil
		}
	}
	return v, nil
}

// exportINI writes keys without a dot first, then one [section] per prefix
// (the part before the last dot), in order of first appearance.
func exportINI(kvs []KV) []byte {
	var top []KV
	var order []string
	sections := map[string][]KV{}
	for _, kv := range kvs {
		dot := strings.LastIndexByte(kv.Key, '.')
		if dot <= 0 || dot == len(kv.Key)-1 {
			top = append(top, kv)
			continue
		}
		sec := kv.Key[:dot]
		if _, ok := sections[sec]; !ok {
			order = append(order, sec)
		}
		sections[sec] = append(sections[sec], KV{Key: kv.Key[dot+1:], Value: kv.Value})
	}
	var b strings.Builder
	write := func(list []KV) {
		for _, kv := range list {
			b.WriteString(kv.Key)
			b.WriteString(" = ")
			b.WriteString(iniQuote(kv.Value))
			b.WriteByte('\n')
		}
	}
	write(top)
	for _, sec := range order {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("[" + sec + "]\n")
		write(sections[sec])
	}
	return []byte(b.String())
}

func iniQuote(v string) string {
	if v != "" && strings.TrimSpace(v) == v && !strings.ContainsAny(v, "\"';#\\\n\r\t") {
		return v
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`).Replace(v) + `"`
}

// validINIKey keeps keys writable: no separators, brackets, comment marks or
// empty dotted parts.
func validINIKey(k string) bool {
	if strings.ContainsAny(k, "=:[];#\"'") || strings.HasPrefix(k, ".") || strings.HasSuffix(k, ".") || strings.Contains(k, "..") {
		return false
	}
	for _, part := range strings.Split(k, ".") {
		if strings.TrimSpace(part) != part {
			return false
		}
	}
	return true
}
