package formats

import (
	"regexp"
	"strings"
)

// phpName is a PHP constant name.
var phpName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// parsePHP reads PHP constant declarations: `const NAME = expr;` (class or
// global, with optional visibility, final and type, several per statement)
// and `define('NAME', expr);`. Everything else in the file (the opening tag,
// a class wrapper, comments, other code) is skipped.
//
// Each value is kept as the PHP expression exactly as written: `'29'` stays a
// quoted string and `29` stays a number, so exporting reproduces working PHP.
func parsePHP(src string) ([]KV, error) {
	p := &phpParser{s: src, line: 1}
	var out []KV
	for p.i < len(p.s) && p.err == nil {
		if p.skipNonCode() {
			continue
		}
		c := p.s[p.i]
		if !isIdentStart(c) {
			if c == '\n' {
				p.line++
			}
			p.i++
			continue
		}
		start := p.i
		word := strings.ToLower(p.ident())
		if !p.declarationContext(start) {
			continue
		}
		switch word {
		case "const":
			kvs, err := p.constDecl()
			if err != nil {
				return nil, err
			}
			out = append(out, kvs...)
		case "define":
			kv, ok, err := p.define()
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, kv)
			}
		}
	}
	if p.err != nil {
		return nil, p.err
	}
	return out, nil
}

type phpParser struct {
	s    string
	i    int
	line int
	err  error
}

func isIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func isIdentChar(c byte) bool { return isIdentStart(c) || c >= '0' && c <= '9' }

func (p *phpParser) ident() string {
	start := p.i
	for p.i < len(p.s) && isIdentChar(p.s[p.i]) {
		p.i++
	}
	return p.s[start:p.i]
}

// declarationContext rules out uses such as $const, $obj->define or
// self::const, where the word is not a keyword.
func (p *phpParser) declarationContext(start int) bool {
	j := start - 1
	for j >= 0 && (p.s[j] == ' ' || p.s[j] == '\t') {
		j--
	}
	if j < 0 {
		return true
	}
	if p.s[j] == '$' || p.s[j] == '\\' {
		return false
	}
	return j < 1 || (p.s[j-1:j+1] != "->" && p.s[j-1:j+1] != "::")
}

// skipNonCode steps over a string, comment or heredoc at the cursor and
// reports whether it did. Line numbers are kept up to date.
func (p *phpParser) skipNonCode() bool {
	rest := p.s[p.i:]
	switch {
	case rest[0] == '\'' || rest[0] == '"' || rest[0] == '`':
		p.skipQuoted(rest[0])
	case strings.HasPrefix(rest, "//") || rest[0] == '#':
		for p.i < len(p.s) && p.s[p.i] != '\n' {
			p.i++
		}
	case strings.HasPrefix(rest, "/*"):
		end := strings.Index(rest[2:], "*/")
		if end < 0 {
			p.err = &ParseError{Line: p.line, Msg: "unterminated /* comment"}
			p.i = len(p.s)
			return true
		}
		p.line += strings.Count(rest[:end+4], "\n")
		p.i += end + 4
	case strings.HasPrefix(rest, "<<<"):
		return p.skipHeredoc()
	default:
		return false
	}
	return true
}

func (p *phpParser) skipQuoted(q byte) {
	start := p.line
	p.i++
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case '\\':
			if p.i+1 < len(p.s) && p.s[p.i+1] == '\n' {
				p.line++
			}
			p.i += 2
			continue
		case '\n':
			p.line++
		case q:
			p.i++
			return
		}
		p.i++
	}
	p.err = &ParseError{Line: start, Msg: "unterminated string"}
	p.i = len(p.s)
}

// skipHeredoc handles <<<ID, <<<"ID" and <<<'ID' up to the closing ID,
// which PHP 7.3+ allows to be indented.
func (p *phpParser) skipHeredoc() bool {
	j := p.i + 3
	for j < len(p.s) && (p.s[j] == ' ' || p.s[j] == '\t') {
		j++
	}
	quoted := j < len(p.s) && (p.s[j] == '\'' || p.s[j] == '"')
	if quoted {
		j++
	}
	k := j
	for k < len(p.s) && isIdentChar(p.s[k]) {
		k++
	}
	id := p.s[j:k]
	if quoted && k < len(p.s) {
		k++
	}
	if id == "" || k >= len(p.s) || (p.s[k] != '\n' && p.s[k] != '\r') {
		return false
	}
	start := p.line
	pos := strings.IndexByte(p.s[k:], '\n') + k + 1
	p.line++
	for pos < len(p.s) {
		end := strings.IndexByte(p.s[pos:], '\n')
		lineEnd := len(p.s)
		if end >= 0 {
			lineEnd = pos + end
		}
		body := strings.TrimLeft(p.s[pos:lineEnd], " \t")
		if strings.HasPrefix(body, id) && (len(body) == len(id) || !isIdentChar(body[len(id)])) {
			p.i = lineEnd - len(body) + len(id)
			return true
		}
		if end < 0 {
			break
		}
		pos = lineEnd + 1
		p.line++
	}
	p.err = &ParseError{Line: start, Msg: "unterminated heredoc"}
	p.i = len(p.s)
	return true
}

// skipSpace steps over whitespace and comments, stopping at strings.
func (p *phpParser) skipSpace() {
	for p.i < len(p.s) {
		if c := p.s[p.i]; c != '\'' && c != '"' && c != '`' && p.skipNonCode() {
			if p.err != nil {
				return
			}
			continue
		}
		switch p.s[p.i] {
		case '\n':
			p.line++
		case ' ', '\t', '\r':
		default:
			return
		}
		p.i++
	}
}

// constDecl reads `NAME = expr` items after the const keyword. The name is
// the last identifier before '=', which skips visibility and type.
func (p *phpParser) constDecl() ([]KV, error) {
	var out []KV
	for {
		name := ""
		for {
			p.skipSpace()
			if p.err != nil {
				return nil, p.err
			}
			if p.i >= len(p.s) {
				return nil, &ParseError{Line: p.line, Msg: "const without a value"}
			}
			c := p.s[p.i]
			if c == '=' {
				p.i++
				break
			}
			if c == ';' { // `use const Foo\BAR;` imports, it declares nothing
				p.i++
				return out, nil
			}
			if isIdentStart(c) {
				name = p.ident()
				continue
			}
			p.i++
		}
		if name == "" {
			return nil, &ParseError{Line: p.line, Msg: "const without a name"}
		}
		line := p.line
		val, stop, err := p.expr(";,")
		if err != nil {
			return nil, err
		}
		if val == "" {
			return nil, &ParseError{Line: line, Msg: "const " + name + " has no value"}
		}
		out = append(out, KV{Key: name, Value: val})
		if stop == ';' {
			return out, nil
		}
	}
}

// define reads `('NAME', expr[, flag])`. ok is false for calls this parser
// cannot read statically, such as define($name, ...).
func (p *phpParser) define() (KV, bool, error) {
	p.skipSpace()
	if p.i >= len(p.s) || p.s[p.i] != '(' {
		return KV{}, false, p.err
	}
	p.i++
	p.skipSpace()
	if p.i >= len(p.s) || (p.s[p.i] != '\'' && p.s[p.i] != '"') {
		return KV{}, false, p.err
	}
	start := p.i
	p.skipQuoted(p.s[p.i])
	if p.err != nil {
		return KV{}, false, p.err
	}
	name := p.s[start+1 : p.i-1]
	p.skipSpace()
	if p.i >= len(p.s) || p.s[p.i] != ',' {
		return KV{}, false, &ParseError{Line: p.line, Msg: "define() needs a name and a value"}
	}
	p.i++
	line := p.line
	val, stop, err := p.expr(",)")
	if err != nil {
		return KV{}, false, err
	}
	if val == "" {
		return KV{}, false, &ParseError{Line: line, Msg: "define() without a value"}
	}
	if stop == ',' { // the old case-insensitive flag
		if _, _, err := p.expr(")"); err != nil {
			return KV{}, false, err
		}
	}
	return KV{Key: name, Value: val}, true, nil
}

// expr returns the source text up to the first stop character outside
// brackets, strings and comments, and consumes that character.
func (p *phpParser) expr(stops string) (string, byte, error) {
	start, line, depth := p.i, p.line, 0
	for p.i < len(p.s) {
		if p.skipNonCode() {
			if p.err != nil {
				return "", 0, p.err
			}
			continue
		}
		c := p.s[p.i]
		switch {
		case c == '(' || c == '[' || c == '{':
			depth++
		case (c == ')' || c == ']' || c == '}') && depth > 0:
			depth--
		case depth == 0 && strings.IndexByte(stops, c) >= 0:
			val := strings.TrimSpace(p.s[start:p.i])
			p.i++
			return val, c, nil
		case c == ')' || c == ']' || c == '}':
			return "", 0, &ParseError{Line: p.line, Msg: "unbalanced " + string(c)}
		case c == '\n':
			p.line++
		}
		p.i++
	}
	return "", 0, &ParseError{Line: line, Msg: "missing ';' at the end of a declaration"}
}

// endsInLineComment reports whether a value's last line ends inside a // or
// # comment, which would swallow the ';' written after it.
func endsInLineComment(v string) bool {
	p := &phpParser{s: v, line: 1}
	inComment := false
	for p.i < len(p.s) {
		rest := p.s[p.i:]
		if strings.HasPrefix(rest, "//") || rest[0] == '#' {
			inComment = true
		}
		if p.skipNonCode() {
			continue
		}
		if p.s[p.i] == '\n' {
			inComment = false
		}
		p.i++
	}
	return inComment
}

func exportPHP(kvs []KV) []byte {
	var b strings.Builder
	for _, kv := range kvs {
		v := kv.Value
		if v == "" {
			v = "''"
		}
		b.WriteString("const ")
		b.WriteString(kv.Key)
		b.WriteString(" = ")
		b.WriteString(v)
		if endsInLineComment(v) {
			b.WriteByte('\n')
		}
		b.WriteString(";\n")
	}
	return []byte(b.String())
}

// validatePHPValue accepts a single PHP expression: it must survive being
// written as a declaration and read back unchanged.
func validatePHPValue(v string) error {
	if strings.TrimSpace(v) == "" {
		return &ParseError{Msg: "a PHP value cannot be empty; use '' for an empty string"}
	}
	if strings.TrimSpace(v) != v {
		return &ParseError{Msg: "a PHP value cannot start or end with spaces"}
	}
	kvs, err := parsePHP(string(exportPHP([]KV{{Key: "X", Value: v}})))
	if err != nil {
		return err
	}
	if len(kvs) != 1 || kvs[0].Value != v {
		return &ParseError{Msg: "a PHP value must be one expression, without a ';' outside quotes"}
	}
	if adjacentOperands(v) {
		return &ParseError{Msg: "two values stand next to each other without an operator"}
	}
	return nil
}

// phpKeywords may sit next to an operand (new Foo, $a and $b).
var phpKeywords = map[string]bool{"new": true, "and": true, "or": true, "xor": true, "instanceof": true, "clone": true, "fn": true, "static": true, "print": true}

// adjacentOperands catches the commonest mistake, unquoted text such as
// `hello world`: two operands (words, numbers, strings, closing brackets)
// separated only by whitespace or comments. It is a check, not a parser.
func adjacentOperands(v string) bool {
	p := &phpParser{s: v, line: 1}
	prevOperand := false
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == '\'' || c == '"' || c == '`' || strings.HasPrefix(p.s[p.i:], "<<<") {
			if prevOperand {
				return true
			}
			p.skipNonCode()
			prevOperand = true
			continue
		}
		if p.skipNonCode() { // a comment: does not change what came before
			continue
		}
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			p.i++
		case isIdentStart(c) || c == '$' || c == '\\' || c >= '0' && c <= '9':
			start := p.i
			p.i++
			for p.i < len(p.s) && (isIdentChar(p.s[p.i]) || p.s[p.i] == '\\' || p.s[p.i] == '.' && start < p.i && p.s[start] >= '0' && p.s[start] <= '9') {
				p.i++
			}
			if phpKeywords[strings.ToLower(p.s[start:p.i])] {
				prevOperand = false
				continue
			}
			if prevOperand {
				return true
			}
			prevOperand = true
		case c == ')' || c == ']' || c == '}':
			p.i++
			prevOperand = true
		default:
			p.i++
			prevOperand = false
		}
	}
	return false
}
