package formats

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseDotenv(t *testing.T) {
	src := "# leading comment\n" +
		"PLAIN=value\n" +
		"export EXPORTED=yes\n" +
		"  SPACED = padded value   \n" +
		"EMPTY=\n" +
		"INLINE=abc # trailing comment\n" +
		"HASH=abc#not-a-comment\n" +
		"SINGLE='literal \\n $HOME # kept'\n" +
		"DOUBLE=\"line1\\nline2\\t\\\"q\\\" \\\\ \\$\" # comment after quote\n" +
		"MULTI=\"-----BEGIN KEY-----\nabc\ndef\n-----END KEY-----\"\n" +
		"BACKTICK=`it's`\r\n" +
		"\n" +
		"URL=postgres://u:p@h:5432/db?sslmode=disable\n" +
		"DUP=first\n" +
		"DUP=second\n"
	got, err := Parse(Dotenv, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	want := []KV{
		{"PLAIN", "value"},
		{"EXPORTED", "yes"},
		{"SPACED", "padded value"},
		{"EMPTY", ""},
		{"INLINE", "abc"},
		{"HASH", "abc#not-a-comment"},
		{"SINGLE", `literal \n $HOME # kept`},
		{"DOUBLE", "line1\nline2\t\"q\" \\ $"},
		{"MULTI", "-----BEGIN KEY-----\nabc\ndef\n-----END KEY-----"},
		{"BACKTICK", "it's"},
		{"URL", "postgres://u:p@h:5432/db?sslmode=disable"},
		{"DUP", "second"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestParseDotenvErrorsHaveLinesAndNoContent(t *testing.T) {
	cases := map[string]int{
		"A=1\nB=\"unterminated SECRETVALUE\n":    2,
		"A=1\n\nnot a pair SECRETVALUE\n":        3,
		"A=1\nB='x' SECRETVALUE\n":               2,
		"A=1\n9BAD=SECRETVALUE\n":                2,
		"A=\"multi\nline\" trailing SECRETVALUE": 2,
	}
	for src, line := range cases {
		_, err := Parse(Dotenv, []byte(src))
		pe, ok := err.(*ParseError)
		if !ok {
			t.Fatalf("%q: expected ParseError, got %v", src, err)
		}
		if pe.Line != line {
			t.Errorf("%q: line %d, want %d", src, pe.Line, line)
		}
		if strings.Contains(pe.Error(), "SECRETVALUE") {
			t.Errorf("%q: error leaks content: %v", src, pe)
		}
	}
}

func TestParseProperties(t *testing.T) {
	src := "# comment\n" +
		"! also comment\n" +
		"a=1\n" +
		"b : two words  \n" +
		"c three\n" +
		"   d=leading spaces stripped\n" +
		"long = first \\\n" +
		"       second\n" +
		"esc\\ key\\=x=tab\\there\\nnewline\n" +
		"uni=caf\\u00e9 \\uD83D\\uDE00\n" +
		"empty=\n" +
		"bare\n"
	got, err := Parse(Properties, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	want := []KV{
		{"a", "1"},
		{"b", "two words  "},
		{"c", "three"},
		{"d", "leading spaces stripped"},
		{"long", "first second"},
		{"esc key=x", "tab\there\nnewline"},
		{"uni", "café 😀"},
		{"empty", ""},
		{"bare", ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestParseJSONFlattens(t *testing.T) {
	src := `{"z": "last?", "db": {"host": "h", "port": 5432, "ssl": true, "opts": null},
	         "hosts": ["a", "b"], "ratio": 1.50, "empty": {}}`
	got, err := Parse(JSON, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	want := []KV{
		{"z", "last?"}, {"db.host", "h"}, {"db.port", "5432"}, {"db.ssl", "true"},
		{"db.opts", "null"}, {"hosts", `["a","b"]`}, {"ratio", "1.50"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	for _, bad := range []string{`[1,2]`, `{"a":`, `"str"`} {
		if _, err := Parse(JSON, []byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestParseYAMLFlattens(t *testing.T) {
	src := "z: last\n" +
		"db:\n  host: h\n  port: 5432\n  ratio: 1.0\n  quoted: \"yes\"\n" +
		"base: &b\n  x: 1\n" +
		"copy: *b\n" +
		"list: [a, 2]\n" +
		"text: |\n  one\n  two\n"
	got, err := Parse(YAML, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	want := []KV{
		{"z", "last"}, {"db.host", "h"}, {"db.port", "5432"}, {"db.ratio", "1.0"}, {"db.quoted", "yes"},
		{"base.x", "1"}, {"copy.x", "1"}, {"list", `["a",2]`}, {"text", "one\ntwo\n"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	if _, err := Parse(YAML, []byte("- a\n- b\n")); err == nil {
		t.Error("accepted a top-level sequence")
	}
}

func TestExportShapes(t *testing.T) {
	kvs := []KV{{"db.host", "h"}, {"db.port", "5432"}, {"flag", "true"}, {"name", "yes"}, {"list", `["a",1]`}, {"db", "x"}}
	js, _ := Export(JSON, kvs)
	wantJSON := "{\n  \"db.host\": \"h\",\n  \"db.port\": 5432,\n  \"flag\": true,\n  \"name\": \"yes\",\n  \"list\": [\"a\",1],\n  \"db\": \"x\"\n}\n"
	if string(js) != wantJSON {
		t.Errorf("json export:\n%s", js)
	}
	nested, _ := Export(JSON, []KV{{"db.host", "h"}, {"db.port", "1"}})
	if string(nested) != "{\n  \"db\": {\n    \"host\": \"h\",\n    \"port\": 1\n  }\n}\n" {
		t.Errorf("nested json export:\n%s", nested)
	}
	y, _ := Export(YAML, []KV{{"db.host", "h"}, {"db.port", "1"}, {"on", "yes"}, {"empty", ""}})
	if string(y) != "db:\n  host: h\n  port: 1\n\"on\": \"yes\"\nempty: \"\"\n" {
		t.Errorf("yaml export:\n%s", y)
	}
	env, _ := Export(Dotenv, []KV{{"A", "plain"}, {"B", "has space"}, {"C", "it's"}, {"D", ""}})
	if string(env) != "A=plain\nB='has space'\nC=\"it's\"\nD=\n" {
		t.Errorf("dotenv export:\n%s", env)
	}
	if _, err := Export(Dotenv, []KV{{"bad key", "x"}}); err == nil {
		t.Error("dotenv export accepted an invalid key")
	}
	raw, _ := Export(Raw, []KV{{RawKey, "body\n"}})
	if string(raw) != "body\n" {
		t.Errorf("raw export: %q", raw)
	}
}

// Property: for every format, Parse(Export(kvs)) returns kvs.
func TestRoundTripProperty(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for _, format := range []string{Dotenv, JSON, YAML, Properties, INI, CSV} {
		for iter := 0; iter < 400; iter++ {
			kvs := randomKVs(rng, format)
			out, err := Export(format, kvs)
			if err != nil {
				t.Fatalf("%s export: %v", format, err)
			}
			back, err := Parse(format, out)
			if err != nil {
				t.Fatalf("%s parse of own export failed: %v\n%s", format, err, out)
			}
			if !sameKVs(format, kvs, back) {
				t.Fatalf("%s round trip mismatch\nin   %q\nout  %q\nfile:\n%s", format, kvs, back, out)
			}
		}
	}
	raw := []KV{{RawKey, "anything\r\n goes \x00 here"}}
	out, _ := Export(Raw, raw)
	if back, _ := Parse(Raw, out); !reflect.DeepEqual(back, raw) {
		t.Fatal("raw round trip mismatch")
	}
}

// json and yaml group nested keys under their parent, so order may change.
func sameKVs(format string, a, b []KV) bool {
	if format == Dotenv || format == Properties {
		return reflect.DeepEqual(a, b) || (len(a) == 0 && len(b) == 0)
	}
	if len(a) != len(b) {
		return false
	}
	m := map[string]string{}
	for _, kv := range a {
		if format == CSV {
			// encoding/csv reads a quoted CRLF as LF.
			kv.Value = strings.ReplaceAll(kv.Value, "\r\n", "\n")
		}
		m[kv.Key] = kv.Value
	}
	for _, kv := range b {
		if v, ok := m[kv.Key]; !ok || v != kv.Value {
			return false
		}
	}
	return true
}

var valueAtoms = []string{
	"a", "Z", "0", "9", " ", "  ", "\t", "\n", "\r\n", "\r", "'", "\"", "`", "\\", "#", " #", "=", ":", "!", "$", "${X}",
	"-", "[", "]", "{", "}", ",", "é", "日本", "😀", "true", "null", "1.0", "042", "yes", "~", "---", "%", "@", "*", "&", "|", ">",
	"[1,\"a\"]", "-1.5e3", "\\n", "\u00a0", "\x7f",
}

func randomValue(rng *rand.Rand) string {
	if rng.IntN(8) == 0 {
		return []string{"", "true", "5432", "-0.25", "null", `["x",2]`, "yes", "0x1F"}[rng.IntN(8)]
	}
	var b strings.Builder
	for n := rng.IntN(8); n >= 0; n-- {
		b.WriteString(valueAtoms[rng.IntN(len(valueAtoms))])
	}
	return b.String()
}

func randomKey(rng *rand.Rand, format string, i int) string {
	switch format {
	case Dotenv:
		return []string{"API_KEY", "db.host", "x-y", "_P", "K"}[rng.IntN(5)] + "_" + string(rune('A'+i%26)) + strings.Repeat("9", i/26)
	case Properties:
		return []string{"a", "key with space", "k=v", "k:v", "#hash", "!bang", "üñî", "tab\tkey", "x\\y"}[rng.IntN(9)] + string(rune('a'+i))
	default: // json, yaml, ini: exercise nesting and collisions
		parts := []string{"a", "b", "c", "on", "1", "x y", "true"}
		k := parts[rng.IntN(len(parts))]
		for d := rng.IntN(3); d > 0; d-- {
			k += "." + parts[rng.IntN(len(parts))]
		}
		return k
	}
}

func randomKVs(rng *rand.Rand, format string) []KV {
	seen := map[string]bool{}
	var kvs []KV
	for i, n := 0, rng.IntN(10); i < n; i++ {
		k := randomKey(rng, format, i)
		if seen[k] {
			continue
		}
		seen[k] = true
		kvs = append(kvs, KV{Key: k, Value: randomValue(rng)})
	}
	return kvs
}

func TestValidateKey(t *testing.T) {
	ok := [][2]string{{Dotenv, "DATABASE_URL"}, {Dotenv, "_x.y-z"}, {JSON, "db.host"}, {Properties, "server port"}, {YAML, "日本"}}
	for _, c := range ok {
		if err := ValidateKey(c[0], c[1]); err != nil {
			t.Errorf("%v rejected: %v", c, err)
		}
	}
	bad := [][2]string{{Dotenv, "9X"}, {Dotenv, "A B"}, {JSON, ""}, {JSON, " pad"}, {YAML, "a\nb"}, {Raw, "x"}, {JSON, RawKey}}
	for _, c := range bad {
		if err := ValidateKey(c[0], c[1]); err == nil {
			t.Errorf("%v accepted", c)
		}
	}
}

func TestParseINI(t *testing.T) {
	src := "; comment\n" +
		"name = app\n" +
		"[database]\n" +
		"host = db.internal ; inline comment\n" +
		"port: 5432\n" +
		"password = \"p;w#\\\"d\"\n" +
		"\n" +
		"[feature flags]\n" +
		"dark_mode = 'on'\n" +
		"url = http://x/#anchor\n"
	got, err := Parse(INI, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	want := []KV{
		{"name", "app"},
		{"database.host", "db.internal"},
		{"database.port", "5432"},
		{"database.password", `p;w#"d`},
		{"feature flags.dark_mode", "on"},
		{"feature flags.url", "http://x/#anchor"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	out, _ := Export(INI, want)
	if string(out) != "name = app\n\n[database]\nhost = db.internal\nport = 5432\npassword = \"p;w#\\\"d\"\n\n[feature flags]\ndark_mode = on\nurl = \"http://x/#anchor\"\n" {
		t.Errorf("ini export:\n%s", out)
	}
	for _, bad := range []string{"[open\nx=1\n", "no separator here\n", "k = \"unterminated\n"} {
		if _, err := Parse(INI, []byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestDetectINIAndText(t *testing.T) {
	cases := map[string]string{
		"[server]\nport = 8080\nhost = 0.0.0.0\n":                                        INI,
		"# notes\n[a]\nk: v\n":                                                           INI,
		"Remember to rotate the keys every quarter.\nThanks, ops team\n":                 Raw,
		"-----BEGIN CERTIFICATE-----\nMIIBszCCAVmgAwIBAgIU\n-----END CERTIFICATE-----\n": Raw,
	}
	for body, want := range cases {
		if got := Detect("", []byte(body)); got != want {
			t.Errorf("Detect(%.30q) = %s, want %s", body, got, want)
		}
	}
	if got := Detect("settings.cfg", []byte("[x]\na=1\n")); got != INI {
		t.Errorf(".cfg detected as %s", got)
	}
}

func TestCSV(t *testing.T) {
	got, err := Parse(CSV, []byte("key,value\nAPI_URL,https://x/?a=b\nMULTI,\"line1\nline2\"\nQUOTED,\"say \"\"hi\"\"\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []KV{{"API_URL", "https://x/?a=b"}, {"MULTI", "line1\nline2"}, {"QUOTED", `say "hi"`}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
	if _, err := Parse(CSV, []byte("a,b,c\n")); err == nil {
		t.Fatal("three columns accepted")
	}
	for body, want := range map[string]string{
		"API_URL,https://x\nPORT,8080\n": CSV,
		"key,value\nA,1\n":               CSV,
		"A=1,2\nB=3\n":                   Dotenv,
	} {
		if got := Detect("", []byte(body)); got != want {
			t.Errorf("Detect(%q) = %s, want %s", body, got, want)
		}
	}
	if !Convertible(CSV, Dotenv) || Valid(CSV) || !ExportValid(CSV) {
		t.Fatal("csv is a conversion format, not a file format")
	}
}

func TestParseRefusesDeepNesting(t *testing.T) {
	deep := strings.Repeat(`{"":`, 100) + `{"a":"b"}` + strings.Repeat("}", 100)
	if _, err := Parse(JSON, []byte(deep)); err == nil {
		t.Fatal("JSON nested 100 levels was accepted")
	}
	if _, err := Parse(JSON, []byte(`{"a":{"b":{"c":"d"}}}`)); err != nil {
		t.Fatalf("ordinary nesting refused: %v", err)
	}
}

func TestParseRefusesYAMLAliasFanOut(t *testing.T) {
	var b strings.Builder
	b.WriteString("a0: &a0 {}\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&b, "a%d: &a%d {x: *a%d, y: *a%d}\n", i, i, i-1, i-1)
	}
	if _, err := Parse(YAML, []byte(b.String())); err == nil {
		t.Fatal("2^40 alias visits were accepted")
	}
}

func TestParseYAMLDecodesSharedSequenceOnce(t *testing.T) {
	// A 20,000-item anchored sequence reached 512 times through aliases.
	var b strings.Builder
	b.WriteString("s0: &s0 [" + strings.Repeat("1,", 20000) + "1]\n")
	for i := 1; i <= 9; i++ {
		fmt.Fprintf(&b, "s%d: &s%d {x: *s%d, y: *s%d}\n", i, i, i-1, i-1)
	}
	start := time.Now()
	if _, err := Parse(YAML, []byte(b.String())); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("a shared sequence took %v; it should be decoded once", d)
	}
}

func TestParseJSONKeepsLiteralsAndArrays(t *testing.T) {
	kvs, err := Parse(JSON, []byte(`{"a": 1.0, "b": [1, {"c": [2]}], "d": {"e": null, "f": true}, "g": "x"}`))
	if err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprint(kvs)
	if want := `[{a 1.0} {b [1,{"c":[2]}]} {d.e null} {d.f true} {g x}]`; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestParseRefusesTooManyKeys(t *testing.T) {
	var b strings.Builder
	for i := 0; i <= MaxKeys; i++ {
		fmt.Fprintf(&b, "K%d=v\n", i)
	}
	if _, err := Parse(Dotenv, []byte(b.String())); err == nil {
		t.Fatalf("%d keys were accepted", MaxKeys+1)
	}
}
