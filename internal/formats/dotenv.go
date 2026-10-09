package formats

import "strings"

// parseDotenv understands comments, an optional "export " prefix, unquoted
// values with trailing " # comments", single and backtick quotes (literal),
// and double quotes with \n \r \t \" \\ \$ escapes. Quoted values may span
// lines. No variable expansion is performed.
func parseDotenv(src string) ([]KV, error) {
	p := dotenvParser{s: strings.ReplaceAll(src, "\r\n", "\n"), line: 1}
	var out []KV
	for p.i < len(p.s) {
		p.skipBlanks()
		if p.i >= len(p.s) {
			break
		}
		switch p.s[p.i] {
		case '\n':
			p.i++
			p.line++
			continue
		case '#':
			p.skipToEOL()
			continue
		}
		start := p.line
		if rest := p.s[p.i:]; strings.HasPrefix(rest, "export ") || strings.HasPrefix(rest, "export\t") {
			p.i += len("export")
			p.skipBlanks()
		}
		j := p.i
		for p.i < len(p.s) && p.s[p.i] != '=' && p.s[p.i] != '\n' {
			p.i++
		}
		if p.i >= len(p.s) || p.s[p.i] != '=' {
			return nil, &ParseError{Line: start, Msg: "expected KEY=value"}
		}
		key := strings.TrimSpace(p.s[j:p.i])
		if !dotenvKey.MatchString(key) {
			return nil, &ParseError{Line: start, Msg: "invalid key name"}
		}
		p.i++ // '='
		p.skipBlanks()
		val, err := p.value()
		if err != nil {
			return nil, err
		}
		out = append(out, KV{Key: key, Value: val})
	}
	return out, nil
}

type dotenvParser struct {
	s    string
	i    int
	line int
}

func (p *dotenvParser) skipBlanks() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t') {
		p.i++
	}
}

// skipToEOL stops on the newline so the main loop counts it.
func (p *dotenvParser) skipToEOL() {
	for p.i < len(p.s) && p.s[p.i] != '\n' {
		p.i++
	}
}

func (p *dotenvParser) value() (string, error) {
	if p.i >= len(p.s) || p.s[p.i] == '\n' {
		return "", nil
	}
	q := p.s[p.i]
	if q != '"' && q != '\'' && q != '`' {
		j := p.i
		for p.i < len(p.s) && p.s[p.i] != '\n' {
			if p.s[p.i] == '#' && p.i > j && (p.s[p.i-1] == ' ' || p.s[p.i-1] == '\t') {
				break
			}
			p.i++
		}
		v := strings.TrimRight(p.s[j:p.i], " \t")
		p.skipToEOL()
		return v, nil
	}

	start := p.line
	p.i++
	var b strings.Builder
	for {
		if p.i >= len(p.s) {
			return "", &ParseError{Line: start, Msg: "unterminated quoted value"}
		}
		c := p.s[p.i]
		if c == q {
			p.i++
			break
		}
		if c == '\\' && q == '"' && p.i+1 < len(p.s) {
			p.i++
			switch e := p.s[p.i]; e {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case '"', '\\', '$':
				b.WriteByte(e)
			default:
				if e == '\n' {
					p.line++
				}
				b.WriteByte('\\')
				b.WriteByte(e)
			}
			p.i++
			continue
		}
		if c == '\n' {
			p.line++
		}
		b.WriteByte(c)
		p.i++
	}
	p.skipBlanks()
	if p.i < len(p.s) && p.s[p.i] != '\n' {
		if p.s[p.i] != '#' {
			return "", &ParseError{Line: p.line, Msg: "unexpected characters after closing quote"}
		}
		p.skipToEOL()
	}
	return b.String(), nil
}

// exportDotenv writes plain values bare, prefers single quotes (literal in
// every dotenv dialect) and falls back to double quotes with escapes.
func exportDotenv(kvs []KV) []byte {
	var b strings.Builder
	for _, kv := range kvs {
		b.WriteString(kv.Key)
		b.WriteByte('=')
		v := kv.Value
		switch {
		case dotenvPlain(v):
			b.WriteString(v)
		case !strings.ContainsAny(v, "'\r"):
			b.WriteByte('\'')
			b.WriteString(v)
			b.WriteByte('\'')
		default:
			b.WriteByte('"')
			for i := 0; i < len(v); i++ {
				switch c := v[i]; c {
				case '\\':
					b.WriteString(`\\`)
				case '"':
					b.WriteString(`\"`)
				case '\r':
					b.WriteString(`\r`)
				default:
					b.WriteByte(c)
				}
			}
			b.WriteByte('"')
		}
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

func dotenvPlain(v string) bool {
	for i := 0; i < len(v); i++ {
		c := v[i]
		alnum := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if !alnum && strings.IndexByte("_-.,:/@+=%~^", c) < 0 {
			return false
		}
	}
	return true
}
