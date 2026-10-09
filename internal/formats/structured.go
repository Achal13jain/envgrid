package formats

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// jsonNumber matches the JSON number grammar.
var jsonNumber = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

// isLiteral reports whether a stored value should be emitted unquoted in
// json and yaml: numbers, true, false and null.
func isLiteral(v string) bool {
	return v == "true" || v == "false" || v == "null" || jsonNumber.MatchString(v)
}

// isArray reports whether v is a compact JSON array, the form imports store
// arrays in. Requiring the compact form keeps export and import exact inverses.
func isArray(v string) bool {
	if !strings.HasPrefix(v, "[") || !json.Valid([]byte(v)) {
		return false
	}
	var buf bytes.Buffer
	return json.Compact(&buf, []byte(v)) == nil && buf.String() == v
}

// JSON

func parseJSON(body []byte) ([]KV, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if trimmed[0] != '{' {
		return nil, &ParseError{Msg: "top level must be a JSON object"}
	}
	if !json.Valid(trimmed) {
		return nil, &ParseError{Msg: "invalid JSON"}
	}
	var out []KV
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	if err := flattenJSON(dec, trimmed, "", 0, &out); err != nil {
		var pe *ParseError
		if errors.As(err, &pe) {
			return nil, pe
		}
		return nil, &ParseError{Msg: "invalid JSON"}
	}
	return out, nil
}

// flattenJSON walks the document once with a single decoder, in document
// order (encoding/json maps would lose it), and emits one KV per leaf. Reading
// each byte once keeps deep nesting from multiplying the work.
func flattenJSON(dec *json.Decoder, raw []byte, prefix string, depth int, out *[]KV) error {
	if len(*out) > MaxKeys || len(prefix) > maxKeyLen || depth > maxDepth {
		return &ParseError{Msg: "too many keys, nesting over 64 levels, or a key name over 256 characters"}
	}
	start := dec.InputOffset()
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch t := tok.(type) {
	case json.Delim:
		if t == '{' {
			for dec.More() {
				k, err := dec.Token()
				if err != nil {
					return err
				}
				if err := flattenJSON(dec, raw, joinKey(prefix, k.(string)), depth+1, out); err != nil {
					return err
				}
			}
			_, err := dec.Token() // the closing brace
			return err
		}
		// An array is kept whole, as compact JSON: skip to its closing bracket.
		for open := 1; open > 0; {
			tok, err := dec.Token()
			if err != nil {
				return err
			}
			if d, ok := tok.(json.Delim); ok {
				if d == '[' || d == '{' {
					open++
				} else {
					open--
				}
			}
		}
		arr := raw[start:dec.InputOffset()]
		var buf bytes.Buffer
		if err := json.Compact(&buf, arr[bytes.IndexByte(arr, '['):]); err != nil {
			return err
		}
		*out = append(*out, KV{Key: prefix, Value: buf.String()})
	case string:
		*out = append(*out, KV{Key: prefix, Value: t})
	case json.Number: // the literal text, so 1.0 stays 1.0
		*out = append(*out, KV{Key: prefix, Value: t.String()})
	case bool:
		*out = append(*out, KV{Key: prefix, Value: strconv.FormatBool(t)})
	default: // null
		*out = append(*out, KV{Key: prefix, Value: "null"})
	}
	return nil
}

func joinKey(prefix, k string) string {
	if prefix == "" {
		return k
	}
	return prefix + "." + k
}

func exportJSON(kvs []KV) []byte {
	var b bytes.Buffer
	writeJSONTree(&b, buildTree(kvs), 1)
	b.WriteByte('\n')
	return b.Bytes()
}

func writeJSONTree(b *bytes.Buffer, t *tree, depth int) {
	if len(t.order) == 0 {
		b.WriteString("{}")
		return
	}
	indent := strings.Repeat("  ", depth)
	b.WriteString("{\n")
	for i, name := range t.order {
		b.WriteString(indent)
		b.Write(jsonString(name))
		b.WriteString(": ")
		if sub, ok := t.sub[name]; ok {
			writeJSONTree(b, sub, depth+1)
		} else if v := t.leaf[name]; isLiteral(v) || isArray(v) {
			b.WriteString(v)
		} else {
			b.Write(jsonString(v))
		}
		if i < len(t.order)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString(strings.Repeat("  ", depth-1))
	b.WriteByte('}')
}

func jsonString(s string) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // encoding a string cannot fail
	return bytes.TrimRight(b.Bytes(), "\n")
}

// tree is an insertion-ordered nesting of dotted keys.
type tree struct {
	order []string
	leaf  map[string]string
	sub   map[string]*tree
}

func newTree() *tree { return &tree{leaf: map[string]string{}, sub: map[string]*tree{}} }

// buildTree nests "a.b.c" under a -> b -> c unless that would collide with
// another key ("a" or "a.b" also exist) or has an empty segment; those keys
// stay literal at the top level. Either way a re-import yields the same keys.
func buildTree(kvs []KV) *tree {
	exists := make(map[string]bool, len(kvs))
	for _, kv := range kvs {
		exists[kv.Key] = true
	}
	root := newTree()
	for _, kv := range kvs {
		segs := strings.Split(kv.Key, ".")
		nest := len(segs) > 1
		for i, s := range segs {
			if s == "" || (i < len(segs)-1 && exists[strings.Join(segs[:i+1], ".")]) {
				nest = false
				break
			}
		}
		if !nest {
			segs = []string{kv.Key}
		}
		t := root
		for _, s := range segs[:len(segs)-1] {
			child, ok := t.sub[s]
			if !ok {
				child = newTree()
				t.sub[s] = child
				t.order = append(t.order, s)
			}
			t = child
		}
		last := segs[len(segs)-1]
		t.leaf[last] = kv.Value
		t.order = append(t.order, last)
	}
	return root
}

// YAML

func parseYAML(body []byte) ([]KV, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return nil, &ParseError{Msg: "invalid YAML"}
	}
	if len(doc.Content) == 0 {
		return nil, nil
	}
	root := resolveAlias(doc.Content[0])
	if root.Kind != yaml.MappingNode {
		return nil, &ParseError{Line: root.Line, Msg: "top level must be a mapping"}
	}
	var out []KV
	visits := 0
	if err := flattenYAML(root, "", 0, &visits, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func resolveAlias(n *yaml.Node) *yaml.Node {
	for n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	return n
}

func flattenYAML(n *yaml.Node, prefix string, depth int, visits *int, out *[]KV) error {
	// Aliases are followed by hand here, so yaml.v3's own alias limits do not
	// apply; these caps bound the result instead. Counting visits stops
	// aliases that fan out to empty mappings, which emit no keys.
	if *visits++; *visits > maxVisits {
		return &ParseError{Line: n.Line, Msg: "aliases expand too far"}
	}
	if len(*out) > MaxKeys || len(prefix) > maxKeyLen || depth > maxDepth {
		return &ParseError{Line: n.Line, Msg: "too many keys, nesting over 64 levels, or a key name over 256 characters"}
	}
	n = resolveAlias(n)
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := resolveAlias(n.Content[i])
			if k.Kind != yaml.ScalarNode {
				return &ParseError{Line: k.Line, Msg: "mapping keys must be scalars"}
			}
			if err := flattenYAML(n.Content[i+1], joinKey(prefix, k.Value), depth+1, visits, out); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		var v any
		if err := n.Decode(&v); err != nil {
			return &ParseError{Line: n.Line, Msg: "unsupported sequence"}
		}
		js, err := json.Marshal(v)
		if err != nil {
			return &ParseError{Line: n.Line, Msg: "sequence cannot be represented as JSON"}
		}
		*out = append(*out, KV{Key: prefix, Value: string(js)})
	case yaml.ScalarNode:
		// The source text, not the resolved type: "1.0" stays "1.0".
		*out = append(*out, KV{Key: prefix, Value: n.Value})
	default:
		return &ParseError{Line: n.Line, Msg: "unsupported YAML node"}
	}
	return nil
}

func exportYAML(kvs []KV) ([]byte, error) {
	root := yamlTree(buildTree(kvs))
	if len(root.Content) == 0 {
		return []byte("{}\n"), nil
	}
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func yamlTree(t *tree) *yaml.Node {
	m := &yaml.Node{Kind: yaml.MappingNode}
	for _, name := range t.order {
		v := yamlValue(t.leaf[name])
		if sub, ok := t.sub[name]; ok {
			v = yamlTree(sub)
		}
		m.Content = append(m.Content, yamlString(name), v)
	}
	return m
}

func yamlValue(v string) *yaml.Node {
	if isLiteral(v) {
		return &yaml.Node{Kind: yaml.ScalarNode, Value: v}
	}
	if isArray(v) {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(v), &doc); err == nil && len(doc.Content) == 1 {
			return doc.Content[0]
		}
	}
	return yamlString(v)
}

// yaml11Special matches strings a YAML 1.1 reader would not treat as text:
// old booleans and sexagesimal numbers.
var yaml11Special = regexp.MustCompile(`^(?i:y|yes|n|no|on|off)$|^[-+]?[0-9][0-9_]*(:[0-5]?[0-9])+(\.[0-9_]*)?$`)

// yamlString picks the style itself: yaml.v3's own choice of literal blocks
// loses leading line breaks and can emit tabs it then refuses to parse.
func yamlString(s string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
	switch {
	case strings.Contains(s, "\n") && literalSafe(s):
		n.Style = yaml.LiteralStyle
	case strings.ContainsAny(s, "\n\r\t") || yaml11Special.MatchString(s):
		n.Style = yaml.DoubleQuotedStyle
	}
	return n
}

// literalSafe allows block literals only for text the emitter round-trips:
// no tabs or carriage returns, no leading whitespace, no trailing spaces.
func literalSafe(s string) bool {
	if strings.ContainsAny(s, "\r\t") || strings.IndexAny(s[:1], " \n") == 0 {
		return false
	}
	for _, line := range strings.Split(s, "\n") {
		if strings.HasSuffix(line, " ") {
			return false
		}
	}
	return true
}
