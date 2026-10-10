package formats

import (
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
)

// phpFixture mirrors the shape of a real constants class: indentation with
// spaces and tabs, comments of every kind (some containing "const"), strings
// holding // and ;, multi-line arrays, expressions, NULL, define() and a
// heredoc. All values are invented.
const phpFixture = `<?php
namespace App;

use const Other\IMPORTED;

class constants {
    const  PAGE_SIZE = 10;
    const  EDITOR_PAGE_SIZE = 7;

    //temporary...later find way to pass role_id
    const  ADMIN_ID = 2;
    const  MAX_FILESIZE = 104857600; // 100 MB
    const  ALLOWED_TYPES ='*.jpg;*.gif;*.png';
    const DEFAULT_END_TIME = '0000-00-00 00:00:00';
	const TAB_INDENTED = "tabs\tand \"quotes\"";
   // const OLD_LIMIT = 3;
    /* const BLOCK_COMMENTED = 1; */
//    const LIST = array(
//        'Q1' => '01/01',
//        );
    const SITE_URL = "http://localhost/site"; // not a comment: //
    const WIN_PATH = 'C:\Users\Someone\Python\python.exe';
    const ESCAPED = 'it\'s here';
    const NOTHING = NULL;
    const OFF = false;
    const TEN_YEARS = 10 * 365 * 24 * 60 * 60;
    const DURATIONS = '(1800,3000,3600)';
    const RECIPIENTS = ['a@example.com','b@example.com'];
    const REGION_URLS = [
        'GLOBAL' => 'http://localhost/global',
        'USA'    => 'https://us.example.com',
    ];
    public const string TYPED = 'typed';
    final protected const RATIO = 0.5, OTHER = 'second';
    const NOTE = <<<EOT
        first line; with semicolon
        EOT;
    const ODD_SPACING=-1;

    public function f() { return self::PAGE_SIZE + $this->const; }
}
const GLOBAL_LEVEL = 'top';
define('DEFINED_ONE', 'via define');
if (!defined('DEFINED_TWO')) define("DEFINED_TWO", 42);
`

func TestParsePHPConstants(t *testing.T) {
	got, err := Parse(PHP, []byte(phpFixture))
	if err != nil {
		t.Fatal(err)
	}
	want := []KV{
		{"PAGE_SIZE", "10"},
		{"EDITOR_PAGE_SIZE", "7"},
		{"ADMIN_ID", "2"},
		{"MAX_FILESIZE", "104857600"},
		{"ALLOWED_TYPES", "'*.jpg;*.gif;*.png'"},
		{"DEFAULT_END_TIME", "'0000-00-00 00:00:00'"},
		{"TAB_INDENTED", `"tabs\tand \"quotes\""`},
		{"SITE_URL", `"http://localhost/site"`},
		{"WIN_PATH", `'C:\Users\Someone\Python\python.exe'`},
		{"ESCAPED", `'it\'s here'`},
		{"NOTHING", "NULL"},
		{"OFF", "false"},
		{"TEN_YEARS", "10 * 365 * 24 * 60 * 60"},
		{"DURATIONS", "'(1800,3000,3600)'"},
		{"RECIPIENTS", "['a@example.com','b@example.com']"},
		{"REGION_URLS", "[\n        'GLOBAL' => 'http://localhost/global',\n        'USA'    => 'https://us.example.com',\n    ]"},
		{"TYPED", "'typed'"},
		{"RATIO", "0.5"},
		{"OTHER", "'second'"},
		{"NOTE", "<<<EOT\n        first line; with semicolon\n        EOT"},
		{"ODD_SPACING", "-1"},
		{"GLOBAL_LEVEL", "'top'"},
		{"DEFINED_ONE", "'via define'"},
		{"DEFINED_TWO", "42"},
	}
	if !reflect.DeepEqual(got, want) {
		for i := range max(len(got), len(want)) {
			var g, w KV
			if i < len(got) {
				g = got[i]
			}
			if i < len(want) {
				w = want[i]
			}
			if g != w {
				t.Errorf("item %d: got %q, want %q", i, g, w)
			}
		}
	}
}

func TestParsePHPErrors(t *testing.T) {
	cases := map[string]int{
		"<?php\nconst A = 1;\nconst B = 'never closed SECRETVALUE;\n": 3,
		"const A = 1;\nconst B = 2\n":                                 2,
		"const A = (1;\n":                                             1,
		"const A = 1;\n/* unterminated SECRETVALUE":                   2,
		"const = 5;": 1,
	}
	for src, line := range cases {
		_, err := Parse(PHP, []byte(src))
		pe, ok := err.(*ParseError)
		if !ok {
			t.Errorf("%q: expected ParseError, got %v", src, err)
			continue
		}
		if pe.Line != line {
			t.Errorf("%q: line %d, want %d (%s)", src, pe.Line, line, pe.Msg)
		}
		if strings.Contains(pe.Error(), "SECRETVALUE") {
			t.Errorf("%q: error leaks content", src)
		}
	}
}

func TestPHPExportShape(t *testing.T) {
	out, err := Export(PHP, []KV{{"A", "1"}, {"B", "'x'"}, {"C", "5 // five"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "const A = 1;\nconst B = 'x';\nconst C = 5 // five\n;\n" {
		t.Fatalf("got %q", out)
	}
	if _, err := Export(PHP, []KV{{"bad-name", "1"}}); err == nil {
		t.Fatal("invalid constant name exported")
	}
}

func TestValidatePHPValue(t *testing.T) {
	for _, ok := range []string{"1", "'a;b'", `"x"`, "NULL", "['a', 'b']", "10 * 60", "5 // trailing comment", "<<<EOT\nx\nEOT",
		"OTHER_CONST", "self::A . '/x'", "new Foo(1)", "-1.5e3", "PHP_INT_MAX - 1", "array('k' => [1, 2])", "'a' /* note */ . 'b'"} {
		if err := ValidateValue(PHP, ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", " 1", "1; 2", "'unterminated", "(1", "1)", "/* open", "hello world", "'a' 'b'", "1 2", "[1] 2"} {
		if err := ValidateValue(PHP, bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if err := ValidateValue(Dotenv, "anything; at all"); err != nil {
		t.Errorf("dotenv values are free text: %v", err)
	}
}

// Property: PHP files read back exactly as written, in order.
func TestPHPRoundTripProperty(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	for iter := 0; iter < 2000; iter++ {
		var kvs []KV
		for i, n := 0, rng.IntN(8); i < n; i++ {
			kvs = append(kvs, KV{Key: "K" + string(rune('A'+i)), Value: randomPHPValue(rng)})
		}
		out, err := Export(PHP, kvs)
		if err != nil {
			t.Fatal(err)
		}
		back, err := Parse(PHP, out)
		if err != nil {
			t.Fatalf("parse of own export failed: %v\n%s", err, out)
		}
		if !reflect.DeepEqual(back, kvs) && (len(back) != 0 || len(kvs) != 0) {
			t.Fatalf("round trip mismatch\nin  %q\nout %q\nfile:\n%s", kvs, back, out)
		}
		for _, kv := range kvs {
			if err := ValidateValue(PHP, kv.Value); err != nil {
				t.Fatalf("generated value %q rejected: %v", kv.Value, err)
			}
		}
	}
}

func randomPHPValue(rng *rand.Rand) string {
	text := func() string {
		var b strings.Builder
		for n := rng.IntN(6); n >= 0; n-- {
			b.WriteString(valueAtoms[rng.IntN(len(valueAtoms))])
		}
		return b.String()
	}
	single := func() string {
		return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(text()) + "'"
	}
	switch rng.IntN(9) {
	case 0:
		return single()
	case 1:
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`).Replace(text()) + `"`
	case 2:
		return []string{"0", "42", "-1.5", "1e3", "true", "false", "NULL", "null"}[rng.IntN(8)]
	case 3:
		return "[" + single() + ", " + single() + "]"
	case 4:
		return "[\n    'K' => " + single() + ",\n    'L' => 2,\n]"
	case 5:
		return "10 * 365 * (24 + 1)"
	case 6:
		return single() + " // note; with words"
	case 7:
		return "<<<EOT\n" + strings.ReplaceAll(text(), "EOT", "E0T") + "\nEOT"
	default:
		return "array(" + single() + " => [1, 2])"
	}
}

func TestDetect(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"", phpFixture, PHP},
		{"", "    const  WALL_PAGE_SIZE = 10;\n    const  DEFAULT_PAGE_SIZE = 5;\n", PHP},
		{"", `{"a": {"b": 1}}`, JSON},
		{"", "server:\n  port: 8080\nname: app\n", YAML},
		{"", "export DATABASE_URL=postgres://x\nLOG_LEVEL=info\n", Dotenv},
		{"", "DB_HOST = localhost\nDB_PORT=5432\n", Dotenv},
		{"", "server.port=8080\nspring.datasource.url=jdbc:mysql://x\n", Properties},
		{"", "! comment\nkey: value with = sign\nother=1\n", Properties},
		{"", "server {\n  listen 80;\n}\n", Raw},
		{"", "", Raw},
		{"app.env", "A=1\n", Dotenv},
		{"settings.json", "a: 1\nb: 2\n", YAML}, // the name says JSON, the content does not
		{"config.yml", "a: 1\n", YAML},
		{"Constants.PHP", "<?php\nconst A = 1;\n", PHP},
	}
	for _, c := range cases {
		if got := Detect(c.name, []byte(c.body)); got != c.want {
			t.Errorf("Detect(%q, %.40q) = %s, want %s", c.name, c.body, got, c.want)
		}
	}
}

func TestConvertible(t *testing.T) {
	if !Convertible(JSON, Dotenv) || !Convertible(YAML, Properties) {
		t.Error("text formats should convert")
	}
	if Convertible(PHP, Dotenv) || Convertible(Dotenv, PHP) || Convertible(Raw, JSON) {
		t.Error("php and raw must not convert")
	}
}
