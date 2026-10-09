package formats

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
)

// parseProperties follows java.util.Properties.load: '#' and '!' comments,
// backslash line continuation, key terminated by unescaped '=', ':' or
// whitespace, and \t \n \r \f \uXXXX escapes.
func parseProperties(src string) ([]KV, error) {
	src = strings.ReplaceAll(strings.ReplaceAll(src, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(src, "\n")
	var out []KV
	for n := 0; n < len(lines); n++ {
		start := n + 1
		line := strings.TrimLeft(lines[n], " \t\f")
		if line == "" || line[0] == '#' || line[0] == '!' {
			continue
		}
		// Join continuation lines: an odd number of trailing backslashes.
		if continues(line) && n+1 < len(lines) {
			var b strings.Builder
			for continues(line) && n+1 < len(lines) {
				b.WriteString(line[:len(line)-1])
				n++
				line = strings.TrimLeft(lines[n], " \t\f")
			}
			b.WriteString(line)
			line = b.String()
		}
		if continues(line) {
			line = line[:len(line)-1]
		}

		k := 0
		for k < len(line) {
			c := line[k]
			if c == '\\' {
				k += 2
				continue
			}
			if c == '=' || c == ':' || c == ' ' || c == '\t' || c == '\f' {
				break
			}
			k++
		}
		if k > len(line) {
			k = len(line)
		}
		rawKey, rest := line[:k], line[k:]
		rest = strings.TrimLeft(rest, " \t\f")
		if rest != "" && (rest[0] == '=' || rest[0] == ':') {
			rest = strings.TrimLeft(rest[1:], " \t\f")
		}
		key, err := unescapeProperties(rawKey)
		if err != nil {
			return nil, &ParseError{Line: start, Msg: err.Error()}
		}
		val, err := unescapeProperties(rest)
		if err != nil {
			return nil, &ParseError{Line: start, Msg: err.Error()}
		}
		out = append(out, KV{Key: key, Value: val})
	}
	return out, nil
}

func continues(line string) bool {
	n := 0
	for i := len(line) - 1; i >= 0 && line[i] == '\\'; i-- {
		n++
	}
	return n%2 == 1
}

func unescapeProperties(s string) (string, error) {
	if !strings.Contains(s, `\`) {
		return s, nil
	}
	var units []uint16
	var b strings.Builder
	flush := func() {
		if len(units) > 0 {
			b.WriteString(string(utf16.Decode(units)))
			units = units[:0]
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			flush()
			b.WriteByte(c)
			continue
		}
		i++
		switch e := s[i]; e {
		case 't':
			flush()
			b.WriteByte('\t')
		case 'n':
			flush()
			b.WriteByte('\n')
		case 'r':
			flush()
			b.WriteByte('\r')
		case 'f':
			flush()
			b.WriteByte('\f')
		case 'u':
			if i+4 >= len(s) {
				return "", fmt.Errorf("malformed \\uXXXX escape")
			}
			u, err := strconv.ParseUint(s[i+1:i+5], 16, 16)
			if err != nil {
				return "", fmt.Errorf("malformed \\uXXXX escape")
			}
			units = append(units, uint16(u))
			i += 4
		default:
			flush()
			b.WriteByte(e)
		}
	}
	flush()
	return b.String(), nil
}

// exportProperties writes ASCII-only output (non-ASCII as \uXXXX), which is
// what Properties.store produces and what ISO-8859-1 readers expect.
func exportProperties(kvs []KV) []byte {
	var b strings.Builder
	for _, kv := range kvs {
		writePropertiesEscaped(&b, kv.Key, true)
		b.WriteByte('=')
		writePropertiesEscaped(&b, kv.Value, false)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

func writePropertiesEscaped(b *strings.Builder, s string, isKey bool) {
	for i, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\f':
			b.WriteString(`\f`)
		case '=', ':', '#', '!':
			if isKey || i == 0 {
				b.WriteByte('\\')
			}
			b.WriteRune(r)
		case ' ':
			if isKey || i == 0 {
				b.WriteByte('\\')
			}
			b.WriteByte(' ')
		default:
			if r < 0x20 || r > 0x7e {
				for _, u := range utf16.Encode([]rune{r}) {
					fmt.Fprintf(b, `\u%04X`, u)
				}
			} else {
				b.WriteRune(r)
			}
		}
	}
}
