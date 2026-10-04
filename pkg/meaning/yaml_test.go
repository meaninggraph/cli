package meaning

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// valueOf parses and renders the value as JSON, which sorts mapping keys.
func valueOf(t *testing.T, text string) string {
	t.Helper()
	node, err := ParseYAML([]byte(text))
	if err != nil {
		t.Fatalf("ParseYAML(%q): %v", text, err)
	}
	out, jsonErr := json.Marshal(node.Value())
	if jsonErr != nil {
		t.Fatal(jsonErr)
	}
	return string(out)
}

// Each accepted case is a file the reference checker's YAML library reads to
// the same value (checked with Node when the cases were written).
func TestParseYAMLAccepts(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("k", 1024)
	cases := []struct{ name, text, want string }{
		{"plain scalars", "a: hello\nb: 840\nc: -3\nd: 1.5\ne: 1e3\nf: .5\ng: 5.\nh: 007\ni: +5\nj: true\nk: False\nl: ~\nm: null\nn:\no: yes\np: 2025-01-01\nq: 1_000\nr: 0b1\ns: NULL\nt: TRUE\nu: 0\nv: -0\n",
			`{"a":"hello","b":840,"c":-3,"d":1.5,"e":1000,"f":0.5,"g":5,"h":7,"i":5,"j":true,"k":false,"l":null,"m":null,"n":null,"o":"yes","p":"2025-01-01","q":"1_000","r":"0b1","s":null,"t":true,"u":0,"v":0}`},
		{"numbers up to 2^53", "a: 9007199254740992\nb: -9007199254740992\nc: 1e308\nd: 0.1e-2\n", `{"a":9007199254740992,"b":-9007199254740992,"c":1e+308,"d":0.001}`},
		{"plain scalars with punctuation", "a: x#y\nb: a:b\nc: http://x/y?z=1\nd: ?x\ne: -x\nf: :x\ng: x &y\nh: 5%\ni: a, b\nj: x] y\nk: a b   \nl: x*y\nm: x!y\n",
			`{"a":"x#y","b":"a:b","c":"http://x/y?z=1","d":"?x","e":"-x","f":":x","g":"x \u0026y","h":"5%","i":"a, b","j":"x] y","k":"a b","l":"x*y","m":"x!y"}`},
		{"comments", "# top\n\na: x # c\nb: \"q\" # c\nc: [x] # c\nd: |  # c\n  t\n# end\ne:    # c\n  f: 1\n  # inner\n", `{"a":"x","b":"q","c":["x"],"d":"t\n","e":{"f":1}}`},
		{"quoted scalars", "a: 'it''s \\n'\nb: \"say \\\"hi\\\" \\\\ \\/\"\nc: ''\nd: \"\"\ne: \"#not a comment\"\n", `{"a":"it's \\n","b":"say \"hi\" \\ /","c":"","d":"","e":"#not a comment"}`},
		{"quoted keys", "\"a b\": 1\n'c': 2\n\"\": 3\n\"d\" : 4\n'e e': 5\n", `{"":3,"a b":1,"c":2,"d":4,"e e":5}`},
		{"keys of every shape", "a b: 1\n-x: 2\n'?': 3\nyes: 4\n1_0: 5\n" + long + ": 6\n", `{"-x":2,"1_0":5,"?":3,"a b":1,"` + long + `":6,"yes":4}`},
		{"block sequences", "a:\n- 1\n- 2\nb:\n  - x\n  -   y: 1\n      z: 2\n  - - p\n    - q\n  -\n    r: 1\n  -\n  - # c\n    s: 2\nc:\n  d:\n    e: 1\n",
			`{"a":[1,2],"b":["x",{"y":1,"z":2},["p","q"],{"r":1},null,{"s":2}],"c":{"d":{"e":1}}}`},
		{"a sequence of mappings in the dash's line", "- a: 1\n  b: 2\n- \"c\": 3\n- 'd'\n- [e]\n- {f: 1}\n- g: |\n    h\n    i\n  j: 2\n", `[{"a":1,"b":2},{"c":3},"d",["e"],{"f":1},{"g":"h\ni\n","j":2}]`},
		{"a root scalar", "hello\n", `"hello"`},
		{"a root sequence", "- a\n- b: 1\n", `["a",{"b":1}]`},
		{"a root flow mapping in JSON style", "{\"a\": [1, 2.5, \"x\", true, null], \"b\": {}, \"c\":1, \"d\" : 2}\n", `{"a":[1,2.5,"x",true,null],"b":{},"c":1,"d":2}`},
		{"a root flow sequence over lines", "[a,\n b,\n]\n", `["a","b"]`},
		{"a document start", "---\na: 1\n", `{"a":1}`},
		{"a document start with a comment", "# c\n--- # c\n\na: 1\n", `{"a":1}`},
		{"a byte order mark", "\xef\xbb\xbfa: 1\n", `{"a":1}`},
		{"a byte order mark and a comment", "\xef\xbb\xbf  # c\n  \na: 1\n", `{"a":1}`},
		{"tabs inside comments", "# a\tb\na: 1 # c\td\nb: x # e\tf\nc: \"q\" #\tg\nd: [x] # h\ti\nf: | # j\tk\n  t\ng: 1\n  # l\tm\n", `{"a":1,"b":"x","c":"q","d":["x"],"f":"t\n","g":1}`},
		{"tabs in text", "a: \"x\ty\"\nb: 'x\ty'\nc: x\ty\n  z\td\nd: |\n  x\ty  \n  z\t  w\n\"k\tk\": 1\n", `{"a":"x\ty","b":"x\ty","c":"x\ty z\td","d":"x\ty  \nz\t  w\n","k\tk":1}`},
		{"tabs in a flow collection's quotes", "a: {\"k\tk\": 'v\tv',\n  l: 1}\n", `{"a":{"k\tk":"v\tv","l":1}}`},
		{"a comment before a block sequence and before a block mapping", "a:\n# c\n  # d\n  - x\n  - y\nb:\n #e\n  c: 1\n", `{"a":["x","y"],"b":{"c":1}}`},
		{"a signed hexadecimal text and an invalid octal one are strings", "a: -0x1F\nb: 0o89\nc: +0x1\n", `{"a":"-0x1F","b":"0o89","c":"+0x1"}`},
		{"blank lines after a block scalar", "a: |\n  x\n\n  \n\nb: 1\n", `{"a":"x\n","b":1}`},
		{"line ends of CRLF", "a: 1\r\nb: |\r\n  x\r\n  y\r\nc: [1,\r\n  2]\r\n", `{"a":1,"b":"x\ny\n","c":[1,2]}`},
		{"no final line break", "a: 1", `{"a":1}`},
		{"flow collections over lines", "a: [x,\n  y,\n  {b: 1,\n   c: 2},\n  ]\nd: {k: v,\n  l: w\n  }\ne: [\n  1\n  ]\n", `{"a":["x","y",{"b":1,"c":2}],"d":{"k":"v","l":"w"},"e":[1]}`},
		{"flow scalars", "a: [x?ref=abc, https://e.com/q?id=5, \"q\", 'r', who?, a:b, -1, ?x, a b , 1e3, true, ~, null, x:y]\nb: {en: who?, ru: [a?, b], \"k\":1, 'k2': 2, n:, m: , o: [], p: {}, q: {r: s}}\n",
			`{"a":["x?ref=abc","https://e.com/q?id=5","q","r","who?","a:b",-1,"?x","a b",1000,true,null,null,"x:y"],"b":{"en":"who?","k":1,"k2":2,"m":null,"n":null,"o":[],"p":{},"q":{"r":"s"},"ru":["a?","b"]}}`},
		{"flow collections closed at the indent of their key", "p:\n  a: [\n    x,\n  ]\n  b: {k: v,\n  }\n  c: [x] # c\nd: [y,\n]\ne:\n- [z,\n]\n- f: {g: h\n    }\n", `{"d":["y"],"e":[["z"],{"f":{"g":"h"}}],"p":{"a":["x"],"b":{"k":"v"},"c":["x"]}}`},
		{"empty flow collections", "a: []\nb: {}\nc: [[], {}]\nd: [ ]\ne: { }\n", `{"a":[],"b":{},"c":[[],{}],"d":[],"e":{}}`},
		{"flow collection then a comment", "a: [x]   # c\nb: {k: v} #c\n", `{"a":["x"],"b":{"k":"v"}}`},
		{"literal and folded block scalars", "a: |\n  x\n   y\n\n  z\n\nb: >\n  a\n  b\n\n  c\n   d\n  e\n\nc: |\n  x\n\n\nd: |-\n  x\n\ne: >-\n  f\n  g\nf: |\ng: >\n\nh: 1\n",
			`{"a":"x\n y\n\nz\n","b":"a b\nc\n d\ne\n","c":"x\n","d":"x","e":"f g","f":"","g":"","h":1}`},
		{"block scalars in sequences, with leading blank lines and a less indented comment", "- |\n  x\n- >\n  y\n- |\n\n  z\n  # text\n# comment\n- >\n  q\n\n", `["x\n","y\n","\nz\n# text\n","q\n"]`},
		{"folded with several blank lines and a more indented line", "a: >\n\n\n  x\n   y\n\n\n  z\n", `{"a":"\n\nx\n y\n\n\nz\n"}`},
		{"a block scalar header followed by a less indented line", "a:\n  b: |\n    x\n  c: 1\n", `{"a":{"b":"x\n","c":1}}`},
		{"a plain scalar continued with a dash", "a: x\n  - y\n  -\n", `{"a":"x - y -"}`},
		{"a sequence entry continued with a dash", "- a\n - b\n", `["a - b"]`},
		{"a plain scalar over lines", "a: one\n  two\n   three\nb: 1\nd:\n  text\n  more\ne: x\n  y #c\nf: 1\n g\n", `{"a":"one two three","b":1,"d":"text more","e":"x y","f":"1 g"}`},
		{"a plain scalar over lines with blank lines", "a: one\n\n  two\n\n\n  three\nb: 1\nc:\n  x\n\n  y\nb2: 2\nd: one\n\nb3: 2\ne: end\n\n", `{"a":"one\ntwo\n\nthree","b":1,"b2":2,"b3":2,"c":"x\ny","d":"one","e":"end"}`},
		{"a plain scalar with blank lines in a sequence and before a comment", "- x\n\n  y\n- z\n\n# c\n- w\n  \n    v # c\n", `["x\ny","z","w\nv"]`},
		{"a value on the next line", "a:\n  \"quoted\"\nb:\n  [x, y]\nc:\n  5\nd:\n  {k: v}\ne:\n  'x'  # c\n", `{"a":"quoted","b":["x","y"],"c":5,"d":{"k":"v"},"e":"x"}`},
		{"trailing spaces and blank lines", "a: 1   \n\n\nb:   2\n\n", `{"a":1,"b":2}`},
		{"empty values", "a:\nb:\n  \nc: # comment\nd: ~\n", `{"a":null,"b":null,"c":null,"d":null}`},
		{"a comment before a colon makes a scalar", "- a #b: c\n- \"d\" # e\n", `["a","d"]`},
		{"a flow key with an escape", "a: {\"a\\u0062\": 1, 'c''d': 2}\n", `{"a":{"ab":1,"c'd":2}}`},
		{"an empty document with a comment", "# only a comment\n", `null`},
		{"unicode", "a: Исполнитель\nb: \"日本語\"\nc: [é, 'ü']\n", `{"a":"Исполнитель","b":"日本語","c":["é","ü"]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := valueOf(t, tc.text); got != tc.want {
				t.Fatalf("value = %s\nwant    %s", got, tc.want)
			}
		})
	}
}

func TestParseYAMLEscapes(t *testing.T) {
	t.Parallel()
	node := parse(t, `a: "\0\a\b\t\n\v\f\r\e\ \"\/\\\N\_\L\P\x41\u00e9\U0001F600\uD83D\uDE00\ud83d\ude00"`+"\n")
	want := "\x00\a\b\t\n\v\f\r\x1b \"/\\\u0085\u00a0\u2028\u2029A\u00e9\U0001F600\U0001F600\U0001F600"
	if got, _ := node.Field("a").Str(); got != want {
		t.Fatalf("a = %q, want %q", got, want)
	}
}

func TestParseYAMLRefuses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, text string
		rule       string
		line       int
		message    string
	}{
		// characters and line ends
		{"invalid UTF-8", "a: 1\nb: \xff\n", RuleYAMLEncoding, 2, "not UTF-8"},
		{"UTF-16 with a byte order mark", "\xff\xfea\x00:\x00", RuleYAMLEncoding, 1, "not UTF-8"},
		{"a NUL character", "a: 1\x00\n", RuleYAMLEncoding, 1, "NUL"},
		{"a lone carriage return", "a: 1\nb: 2\rc: 3\n", RuleYAMLLineEnding, 2, "carriage return"},
		{"a tab as indentation", "a:\n\tb: 1\n", RuleYAMLTab, 2, "not in indentation"},
		{"a tab after a dash in a sequence", "- a\n-\tb\n", RuleYAMLTab, 2, "after a dash"},
		{"a tab after a dash in a mapping", "a: 1\n-\tb\n", RuleYAMLTab, 2, "after a dash"},
		{"a tab after a dash as a block", "a:\n  -\tb\n", RuleYAMLTab, 2, "after a dash"},
		{"a tab after a dash that starts a value", "a: -\tb\n", RuleYAMLTab, 1, "after '-'"},
		{"a tab after a colon", "a:\tb\n", RuleYAMLTab, 1, "after a colon"},
		{"a tab after the colon of a quoted key", "\"a\":\tb\n", RuleYAMLTab, 1, "after a colon"},
		{"a tab between a quoted key and its colon", "\"a\"\t: b\n", RuleYAMLTab, 1, "between a key and its colon"},
		{"a quoted key glued to a value", "\"a\":b\n", RuleYAML, 1, "unexpected text"},
		{"a tab at the end of a line", "a: x\t\n", RuleYAMLTab, 1, "not read"},
		{"a tab at the end of a quoted value", "a: \"x\"\t\n", RuleYAMLTab, 1, "not read"},
		{"a tab before a comment", "a: x\t# c\n", RuleYAMLTab, 1, "before a #"},
		{"a tab at the end of a comment", "a: x # c\t\n", RuleYAMLTab, 1, "not read"},
		{"a tab at the end of a whole-line comment", "# c\t\na: 1\n", RuleYAMLTab, 1, "not read"},
		{"a tab right after the indentation of block text", "a: |\n  \tb\n", RuleYAMLTab, 2, "not read"},
		{"a tab at the end of block text", "a: |\n  b\t\n", RuleYAMLTab, 2, "not read"},
		{"a tab in the indentation of a comment line", "a: 1\n\t# c\nb: 2\n", RuleYAMLTab, 2, "not read"},
		{"a line of a tab", "a: 1\n\t\nb: 2\n", RuleYAMLTab, 2, "not read"},
		{"a line of a tab at the end, without a line break", "a: 1\n\t", RuleYAMLTab, 2, "not read"},
		{"a comment between a key and its plain value", "a:\n# c\n  one\n", RuleYAMLUnsupported, 2, "comment line between a key (or a dash) and the value that starts on a later line (line 3)"},
		{"a comment between a dash and its plain value", "-\n  # c\n  one\n- two\n", RuleYAMLUnsupported, 2, "comment line between a key (or a dash) and the value that starts on a later line (line 3)"},
		{"a comment between a key and its quoted value", "a:\n  # c\n  \"one\"\n", RuleYAMLUnsupported, 2, "comment line between"},
		{"a comment between a key and its flow value", "a:\n  # c\n  [one]\n", RuleYAMLUnsupported, 2, "comment line between"},
		{"a comment between a key and its value is named by its own line", "a: 1\nb:\n\n  # old wording\n  # more\n\n  text\nc: 2\n", RuleYAMLUnsupported, 4, "(line 7)"},
		{"a comment inside a flow collection at the left margin", "a: [x,\n#c\n  y]\n", RuleYAMLUnsupported, 2, "comments inside [ ]"},
		{"a tab in a plain key", "a\tb: 1\n", RuleYAMLTab, 1, "not read"},
		{"a tab at the start of a plain value", "a: \tb\n", RuleYAMLTab, 1, "not read"},
		{"a tab next to a colon inside a plain value", "a: b:\tc\n", RuleYAMLTab, 1, "not read"},
		{"a tab in a flow collection", "a: [b,\tc]\n", RuleYAMLTab, 1, "accepted in quotes only"},
		{"a tab in a flow mapping", "a: {b:\tc}\n", RuleYAMLTab, 1, "accepted in quotes only"},
		{"a tab in the indentation of a flow line", "a: [b,\n\tc]\n", RuleYAML, 2, "must be indented more"},
		{"a line of tabs inside a plain value that continues", "a: b\n\t\n  c\n", RuleYAMLTab, 2, "not read"},
		{"a line that holds a tab inside a block scalar", "a: |\n  b\n  \t\n  c\n", RuleYAMLTab, 3, "not read"},
		{"a tab in the indentation of a comment line after a block scalar", "a: |\n  b\n\t# c\n", RuleYAMLTab, 3, "not read"},
		{"a blank line of a tab after a block scalar and a comment", "a: |\n  b\n# c\n\t\n", RuleYAMLTab, 4, "not read"},
		{"a blank line of a tab after a block scalar and a blank line", "a: |\n  b\n\n\n \t\nc: 1\n", RuleYAMLTab, 5, "not read"},
		{"a line of a tab after a block scalar's text", "a: |\n  b\n\t\n", RuleYAMLTab, 3, "not read"},
		{"a tab before a quoted key's colon in a flow-like line", "a: 1\n\tb: 2\n", RuleYAMLTab, 2, "not read"},
		{"a keep chomping literal", "a: |+\n  b\n", RuleYAMLUnsupported, 1, "keep chomping (|+)"},
		{"a keep chomping folded", "a: >+\n  b\n", RuleYAMLUnsupported, 1, "keep chomping (>+)"},
		{"a keep chomping with an indicator", "a: |+2\n  b\n", RuleYAMLUnsupported, 1, "keep chomping"},
		{"a byte order mark and an indented first line", "\xef\xbb\xbf  a: 1\n  b: 2\n", RuleYAMLEncoding, 1, "byte order mark"},
		{"a byte order mark and a sequence", "\xef\xbb\xbf- a\n- b\n", RuleYAMLEncoding, 1, "byte order mark"},
		{"a flow scalar that continues on the next line", "a: [b,\n  c d\n  e]\n", RuleYAMLUnsupported, 3, "cannot continue on the next line"},
		{"a flow mapping scalar that continues on the next line", "a: {b: c\n  d}\n", RuleYAMLUnsupported, 2, "cannot continue on the next line"},
		{"a bell", "a: x\x07\n", RuleYAMLCharacter, 1, "U+0007"},
		{"DEL", "a: x\x7f\n", RuleYAMLCharacter, 1, "U+007F"},
		{"a C1 control character", "a: x\u0085y\n", RuleYAMLCharacter, 1, "U+0085"},
		{"a line separator", "a: 1\nb: x\u2028y\n", RuleYAMLCharacter, 2, "U+2028"},
		{"a paragraph separator", "a: x\u2029y\n", RuleYAMLCharacter, 1, "U+2029"},
		{"a byte order mark inside the file", "a: 1\nb: x\ufeffy\n", RuleYAMLCharacter, 2, "U+FEFF"},
		{"a noncharacter", "a: x\uffffy\n", RuleYAMLCharacter, 1, "U+FFFF"},
		{"another noncharacter", "a: x\ufffey\n", RuleYAMLCharacter, 1, "U+FFFE"},
		// documents
		{"a directive", "%YAML 1.2\n---\na: 1\n", RuleYAMLDirective, 1, "directive"},
		{"a tag directive", "a: 1\n%TAG ! x\n", RuleYAMLDirective, 2, "directive"},
		{"two documents", "a: 1\n---\nb: 2\n", RuleYAMLDocuments, 2, "document marker"},
		{"two documents, the first marked", "---\na: 1\n---\nb: 2\n", RuleYAMLDocuments, 3, "document marker"},
		{"a document end marker", "a: 1\n...\n", RuleYAMLDocuments, 2, "document marker"},
		{"text on the document start line", "--- a\n", RuleYAMLDocuments, 1, "--- line"},
		// anchors, tags, merge keys, explicit keys
		{"an anchor", "a: &x 1\n", RuleYAMLAnchor, 1, "anchors and aliases"},
		{"an alias", "a: 1\nb: *x\n", RuleYAMLAnchor, 2, "anchors and aliases"},
		{"an anchor on a key", "&a b: 1\n", RuleYAMLAnchor, 1, "anchors and aliases"},
		{"an alias as a key", "*a : 1\n", RuleYAMLAnchor, 1, "anchors and aliases"},
		{"an anchor in a sequence", "- &a x\n", RuleYAMLAnchor, 1, "anchors and aliases"},
		{"an anchor in a flow sequence", "a: [&x 1]\n", RuleYAMLAnchor, 1, "anchors and aliases"},
		{"an alias in a flow mapping", "a: {k: *x}\n", RuleYAMLAnchor, 1, "anchors and aliases"},
		{"an anchor on a flow key", "a: {&k b: 1}\n", RuleYAMLAnchor, 1, "anchors and aliases"},
		{"a merge key", "a: {x: 1}\n<<: {y: 2}\n", RuleYAMLAnchor, 2, "merge keys"},
		{"a quoted merge key", "\"<<\": 1\n", RuleYAMLAnchor, 1, "merge keys"},
		{"a merge key in a flow mapping", "a: {<<: 1}\n", RuleYAMLAnchor, 1, "merge keys"},
		{"a tag", "a: !!str x\n", RuleYAMLTag, 1, "tags"},
		{"a tag on a key", "!!str a: 1\n", RuleYAMLTag, 1, "tags"},
		{"a tag on a collection", "a: !!set {k: ~}\n", RuleYAMLTag, 1, "tags"},
		{"a tag in a flow sequence", "a: [!x y]\n", RuleYAMLTag, 1, "tags"},
		{"a tag on a flow key", "a: {!t k: 1}\n", RuleYAMLTag, 1, "tags"},
		{"an explicit key", "? a\n: 1\n", RuleYAMLUnsupported, 1, "explicit keys"},
		{"an explicit key without text", "?\n: 1\n", RuleYAMLUnsupported, 1, "explicit keys"},
		// keys
		{"a boolean key", "True: 1\n", RuleYAMLKey, 1, "boolean"},
		{"a null key", "null: 1\n", RuleYAMLKey, 1, "null"},
		{"a tilde key", "~: 1\n", RuleYAMLKey, 1, "null"},
		{"a number key", "5: 1\n", RuleYAMLKey, 1, "number"},
		{"an empty key", ": x\n", RuleYAMLKey, 1, "(empty)"},
		{"a number key in a flow mapping", "a: {5: 1}\n", RuleYAMLKey, 1, "number"},
		{"a null key in a flow mapping", "a: {null: 1}\n", RuleYAMLKey, 1, "null"},
		{"an empty key in a flow mapping", "a: {: 1}\n", RuleYAMLKey, 1, "(empty)"},
		{"a key that is too long", strings.Repeat("k", 1025) + ": 1\n", RuleYAMLKey, 1, "1025 characters"},
		{"a quoted key that is too long", "\"" + strings.Repeat("k", 1025) + "\": 1\n", RuleYAMLKey, 1, "1025 characters"},
		{"a flow key that is too long", "a: {" + strings.Repeat("k", 1025) + ": 1}\n", RuleYAMLKey, 1, "1025 characters"},
		{"a repeated key", "a: 1\nb: 2\na: 3\n", RuleYAMLDuplicate, 3, `"a" is repeated`},
		{"a repeated key, once quoted", "a: 1\n\"a\": 2\n", RuleYAMLDuplicate, 2, `"a" is repeated`},
		{"a repeated key in a flow mapping", "a: {k: 1, k: 2}\n", RuleYAMLDuplicate, 1, `"k" is repeated`},
		{"a hexadecimal key is a number", "0x1F: 1\n", RuleYAMLNumber, 1, "hexadecimal"},
		// numbers
		{"a hexadecimal number", "a: 0x1F\n", RuleYAMLNumber, 1, "hexadecimal"},
		{"an octal number", "a: 0o17\n", RuleYAMLNumber, 1, "octal"},
		{"a hexadecimal number in a flow sequence", "a: [0x1F]\n", RuleYAMLNumber, 1, "hexadecimal"},
		{"infinity", "a: .inf\n", RuleYAMLNumber, 1, "finite"},
		{"negative infinity", "a: -.INF\n", RuleYAMLNumber, 1, "finite"},
		{"not a number", "a: .nan\n", RuleYAMLNumber, 1, "finite"},
		{"a float beyond a double", "a: 1e999\n", RuleYAMLNumber, 1, "finite"},
		{"an integer beyond 2^53", "a: 9007199254740993\n", RuleYAMLNumber, 1, "2^53"},
		{"a very large integer", "a: 1" + strings.Repeat("0", 400) + "\n", RuleYAMLNumber, 1, "2^53"},
		{"a negative integer beyond 2^53", "a: -9007199254740993\n", RuleYAMLNumber, 1, "2^53"},
		// escapes
		{"an unknown escape", "a: \"\\q\"\n", RuleYAMLEscape, 1, `\q is not a YAML escape`},
		{"a short hex escape", "a: \"\\x4\"\n", RuleYAMLEscape, 1, "2 hexadecimal digits"},
		{"a non-hex escape", "a: \"\\u12G4\"\n", RuleYAMLEscape, 1, "4 hexadecimal digits"},
		{"a signed escape", "a: \"\\u+123\"\n", RuleYAMLEscape, 1, "4 hexadecimal digits"},
		{"half a surrogate pair", "a: \"\\uD83D\"\n", RuleYAMLEscape, 1, "half of a surrogate pair"},
		{"half a surrogate pair, then text", "a: \"\\uD83Dx\"\n", RuleYAMLEscape, 1, "half of a surrogate pair"},
		{"a high surrogate followed by a short escape", "a: \"\\uD83D\\u12\"\n", RuleYAMLEscape, 1, "half of a surrogate pair"},
		{"a high surrogate followed by a non-surrogate", "a: \"\\uD83D\\u0041\"\n", RuleYAMLEscape, 1, "half of a surrogate pair"},
		{"a low surrogate on its own", "a: \"\\uDE00\"\n", RuleYAMLEscape, 1, "does not name a character"},
		{"a code point beyond Unicode", "a: \"\\U00110000\"\n", RuleYAMLEscape, 1, "does not name a character"},
		// quoted values
		{"a quoted value over two lines", "a: \"x\n  y\"\n", RuleYAMLUnsupported, 1, "must fit on one line"},
		{"an unterminated single-quoted value", "a: 'x\n", RuleYAMLUnsupported, 1, "closing '"},
		{"a trailing backslash", "a: \"x\\\n  y\"\n", RuleYAMLUnsupported, 1, "backslash at the end of a line"},
		{"text after a quoted value", "a: \"x\" y\n", RuleYAML, 1, `unexpected text "y"`},
		{"a comment glued to a quoted value", "a: \"x\"#c\n", RuleYAML, 1, `unexpected text "#c"`},
		{"text after a flow collection", "a: [x] y\n", RuleYAML, 1, `unexpected text "y"`},
		{"a comment glued to a flow collection", "a: [x]#c\n", RuleYAML, 1, `unexpected text "#c"`},
		// flow collections
		{"an unclosed flow collection", "a: [x,\n", RuleYAML, 1, "not closed"},
		{"a nested bracket at the indent of the key", "p:\n  a: [x, [y,\n  ]]\n", RuleYAML, 3, "indented more"},
		{"a closing bracket below the indent of the key", "p:\n    a: [x,\n  ]\n", RuleYAML, 3, "indented more"},
		{"a flow line that is not indented", "a: [x,\ny]\n", RuleYAML, 2, "indented more"},
		{"a comment inside a flow collection", "a: [x, # c\n y]\n", RuleYAMLUnsupported, 1, "comments inside"},
		{"a pair inside a flow sequence", "a: [k: v]\n", RuleYAMLUnsupported, 1, "inside [ ]"},
		{"an empty entry", "a: [,]\n", RuleYAML, 1, `unexpected ','`},
		{"two commas", "a: [x,,y]\n", RuleYAML, 1, `unexpected ','`},
		{"a key without a value", "a: {k}\n", RuleYAMLUnsupported, 1, "an entry without a value is not supported"},
		{"a missing comma in a mapping", "a: {k: 1 j: 2}\n", RuleYAML, 1, "expected a comma or }"},
		{"a missing comma in a sequence", "a: [\"x\" \"y\"]\n", RuleYAML, 1, "expected a comma or ]"},
		{"a key glued to its value", "a: {k:1}\n", RuleYAMLUnsupported, 1, "put a space after a colon"},
		{"a bracket that closes nothing", "a: [x] ]\n", RuleYAML, 1, `unexpected text "]"`},
		{"a block indicator in a flow sequence", "a: [|]\n", RuleYAML, 1, `unexpected '|'`},
		{"a reserved character in a flow sequence", "a: [@x]\n", RuleYAML, 1, `unexpected '@'`},
		{"a dash in a flow sequence", "a: [- x]\n", RuleYAML, 1, "cannot start with '-'"},
		{"a lone dash in a flow mapping", "a: {k: -}\n", RuleYAML, 1, "cannot start with '-'"},
		{"a flow collection as a key", "a: {[x]: 1}\n", RuleYAML, 1, "cannot start a key"},
		{"a block indicator as a flow key", "a: {|: 1}\n", RuleYAML, 1, "cannot start a key"},
		{"a flow collection nested too deep", "a: " + strings.Repeat("[", MaxYAMLDepth+1) + "\n", RuleYAMLLimit, 1, "nested more than"},
		{"a flow mapping nested too deep", "a: " + strings.Repeat("{k: ", MaxYAMLDepth+1) + "\n", RuleYAMLLimit, 1, "nested more than"},
		// block structure
		{"a less indented line inside a mapping", "a:\n  b: 1\n c: 2\n", RuleYAML, 3, "indented"},
		{"a more indented line after a scalar", "a: 1\n  b: 2\n", RuleYAML, 2, "colon followed by a space"},
		{"a sequence entry among mapping entries", "a: 1\n- b\n", RuleYAML, 2, "sequence entry"},
		{"a mapping entry among sequence entries", "- a\nb: 1\n", RuleYAML, 2, "indented differently"},
		{"a more indented line after a compact mapping", "- a: 1\n   b: 2\n", RuleYAML, 2, "colon followed by a space"},
		{"a key without a colon", "a: 1\nb\n", RuleYAML, 2, "expected"},
		{"a flow collection where a key is expected", "a: 1\n[b]: 2\n", RuleYAML, 2, "expected"},
		{"a more indented line after a mapping", "a: 1\nb:\n  c: 2\n   d: 3\n", RuleYAML, 4, "colon followed by a space"},
		{"a more indented key", "a: 1\n  b: 2: 3\n", RuleYAML, 2, "colon followed by a space"},
		{"a mapping value that is a mapping on the same line", "a: b: c\n", RuleYAML, 1, "colon followed by a space"},
		{"a plain value ending in a colon", "a: b:\n", RuleYAML, 1, "colon followed by a space"},
		{"a value that starts with a comma", "a: , x\n", RuleYAML, 1, "cannot start with ','"},
		{"a value that starts with a closing bracket", "a: ] x\n", RuleYAML, 1, "cannot start with ']'"},
		{"a value that starts with a closing brace", "a: } x\n", RuleYAML, 1, "cannot start with '}'"},
		{"a plain value continued with a quote", "a: x\n  \"y\"\n", RuleYAML, 2, "cannot continue with"},
		{"a plain value continued after a comment", "a: x # c\n  y\n", RuleYAML, 2, "indented"},
		{"a plain value, a comment line, and more text", "a: x\n  # c\n  y\n", RuleYAML, 3, "indented"},
		{"a reserved percent", "a: %x\n", RuleYAML, 1, "cannot start with '%'"},
		{"a reserved at", "a: @x\n", RuleYAML, 1, "cannot start with '@'"},
		{"a reserved backtick", "a: `x\n", RuleYAML, 1, "cannot start with '`'"},
		{"a dash and a space as a value", "a: - x\n", RuleYAML, 1, "cannot start a value"},
		{"a lone dash as a value", "a: -\n", RuleYAML, 1, "cannot start a value"},
		{"a question mark and a space as a value", "a: ? x\n", RuleYAML, 1, "cannot start a value"},
		{"a colon and a space as a value", "a: : x\n", RuleYAML, 1, "cannot start a value"},
		{"a block scalar on a line of its own", "a:\n  |\n   x\n", RuleYAMLUnsupported, 2, "must start on the line"},
		{"an indentation indicator", "a: |2\n  x\n", RuleYAMLUnsupported, 1, "indentation indicator"},
		{"an indentation indicator after the chomping indicator", "a: >-1\n  x\n", RuleYAMLUnsupported, 1, "indentation indicator"},
		{"text after a block scalar header", "a: |x\n  y\n", RuleYAML, 1, `unexpected text "x"`},
		{"a comment glued to a block scalar header", "a: |#c\n  y\n", RuleYAML, 1, `unexpected text "#c"`},
		{"spaces on a blank line beyond the indent", "a: |\n  x\n    \n  y\n", RuleYAMLUnsupported, 3, "more spaces"},
		{"spaces on a leading blank line beyond the indent", "a: |\n      \n  x\n", RuleYAMLUnsupported, 2, "more spaces"},
		{"a block nested too deep", nestedMaps(MaxYAMLDepth + 1), RuleYAMLLimit, MaxYAMLDepth + 1, "nested more than"},
		{"a more indented line after a sequence item", "- [a]\n  b\n", RuleYAML, 2, "indented more"},
		{"an unterminated quote in a sequence", "- \"abc\n", RuleYAMLUnsupported, 1, "closing \""},
		{"a comment before the colon of a line that must be a key", "a: 1\nb #c: d\n", RuleYAML, 2, "expected"},
		{"text of several words after a quoted value", "a: \"x\" y z\n", RuleYAML, 1, `unexpected text "y"`},
		{"an unterminated quote in a flow sequence", "a: [\"x]\n", RuleYAMLUnsupported, 1, "closing \""},
		{"a bad escape in a flow sequence", "a: [\"\\q\"]\n", RuleYAMLEscape, 1, `\q is not a YAML escape`},
		{"a bad escape in a flow key", "a: {\"\\q\": 1}\n", RuleYAMLEscape, 1, `\q is not a YAML escape`},
		{"a hexadecimal flow key", "a: {0x1F: 1}\n", RuleYAMLNumber, 1, "hexadecimal"},
		{"a flow collection that is not closed in a sequence", "- [a\n", RuleYAML, 1, "not closed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseYAML([]byte(tc.text))
			if err == nil {
				t.Fatalf("accepted: %s", valueOf(t, tc.text))
			}
			if err.Rule != tc.rule || err.Line != tc.line || !strings.Contains(err.Message, tc.message) {
				t.Fatalf("got rule %q line %d %q\nwant rule %q line %d containing %q", err.Rule, err.Line, err.Message, tc.rule, tc.line, tc.message)
			}
			if err.Error() != err.Message {
				t.Fatal("Error() is the message")
			}
		})
	}
}

func TestParseYAMLLimits(t *testing.T) {
	t.Parallel()
	_, err := ParseYAML([]byte(strings.Repeat("x", MaxFileBytes+1)))
	if err == nil || err.Rule != RuleYAMLLimit {
		t.Fatalf("err = %v", err)
	}
	// 64 levels of nesting are read, the 65th is not.
	if _, err := ParseYAML([]byte(nestedMaps(MaxYAMLDepth))); err != nil {
		t.Fatalf("%d levels: %v", MaxYAMLDepth, err)
	}
	if _, err := ParseYAML([]byte(nestedMaps(MaxYAMLDepth + 1))); err == nil || err.Rule != RuleYAMLLimit {
		t.Fatalf("%d levels: %v", MaxYAMLDepth+1, err)
	}
	// A hostile file of unclosed brackets is refused at once, not parsed to the end.
	if _, err := ParseYAML([]byte("a: " + strings.Repeat("[", 5_000_000))); err == nil || err.Rule != RuleYAMLLimit {
		t.Fatalf("5,000,000 brackets: %v", err)
	}
}

func TestParseYAMLEmptyInputIsNull(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"", "\n\n", "# only a comment\n", "---\n", "--- # nothing\n", "---\n# c\n"} {
		n, err := ParseYAML([]byte(text))
		if err != nil || n.Kind != Null || n.Value() != nil {
			t.Errorf("ParseYAML(%q) = %+v, %v", text, n, err)
		}
	}
}

func TestParseYAMLLinesOfNodes(t *testing.T) {
	t.Parallel()
	doc := parse(t, "# c\na:\n  - x\n  - b: 1\n    c: [2,\n      3]\nd: |\n  t\ne: \"q\"\nf:\n")
	lines := map[string]int{
		"a": 3, "d": 7, "e": 9, "f": 10,
	}
	for key, want := range lines {
		if got := doc.Field(key).Line; got != want {
			t.Errorf("line of %s = %d, want %d", key, got, want)
		}
	}
	if doc.Line != 2 || doc.Field("a").Items[1].Line != 4 || doc.Field("a").Items[1].Field("c").Line != 5 {
		t.Errorf("lines: %d %d %d", doc.Line, doc.Field("a").Items[1].Line, doc.Field("a").Items[1].Field("c").Line)
	}
	var syntax *SyntaxError
	if !errors.As(error(&SyntaxError{}), &syntax) {
		t.Fatal("SyntaxError is an error")
	}
}

// nestedMaps is a block mapping nested the given number of levels deep.
func nestedMaps(levels int) string {
	var sb strings.Builder
	for i := range levels {
		sb.WriteString(strings.Repeat(" ", i) + "a:\n")
	}
	return sb.String()
}

// The tab bookkeeping is one pass: the characters it looks at are counted, for
// documents in which every tab is read and for ones in which the first is not,
// at two sizes. A walk back over the lines for every tab (quadratic: 80,000
// lines of tabs took half a minute) would show as four times the steps for
// twice the lines.
func TestTabsAreCheckedInOnePass(t *testing.T) {
	t.Parallel()
	docs := map[string]func(n int) string{
		"the text of a block scalar": func(n int) string { return "a: |\n" + strings.Repeat("  x\ty\n", n) },
		"comments":                   func(n int) string { return strings.Repeat("# x\ty\n", n) + "a: 1\n" },
		"a quoted scalar":            func(n int) string { return "a: \"" + strings.Repeat("x\ty", n) + "\"\n" },
		"plain scalar lines":         func(n int) string { return "a: x\ty\n" + strings.Repeat("  x\ty\n", n) },
		"lines of tabs":              func(n int) string { return "a: 1\n" + strings.Repeat("\t\n", n) },
		"tab comment lines":          func(n int) string { return "a: |\n  x\n" + strings.Repeat("\t# c\n", n) },
		"tab-ended lines":            func(n int) string { return strings.Repeat("a: 1\t\n", n) },
	}
	for name, doc := range docs {
		_, _, small := parseYAML([]byte(doc(5_000)))
		_, _, large := parseYAML([]byte(doc(10_000)))
		if large > small*5/2+100 || large > 10_000*40 {
			t.Errorf("%s: %d steps for 5,000 lines, %d for 10,000: the work must grow in step with the file", name, small, large)
		}
	}
	// A file at the size limit, all of it lines of tabs, is refused at the first.
	node, err, steps := parseYAML([]byte("a: 1\n" + strings.Repeat("\t\n", (MaxFileBytes-5)/2)))
	if node != nil || err == nil || err.Rule != RuleYAMLTab || err.Line != 2 || steps > 100 {
		t.Fatalf("a file of lines of tabs: %v, %v after %d steps", node, err, steps)
	}
}

// A tab is invisible, so the finding says where it is: the column, counted in
// characters from 1 (a tab is one), in the message and in SyntaxError.Column.
func TestYAMLTabNamesItsColumn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, text   string
		line, column int
	}{
		{"at the end of a line", "a: x\t\n", 1, 5},
		{"at the end of a line with CRLF", "a: x\t\r\n", 1, 5},
		{"after non-ASCII text", "\u00e9: x\t\n", 1, 5},
		{"in indentation", "a:\n  b: 1\n\tc: 2\n", 3, 1},
		{"in the indentation of a comment line", "a: 1\n  \t# c\n", 2, 3},
		{"on a line of its own", "a: 1\n\t\n", 2, 1},
		{"after a dash", "- a\n-\tb\n", 2, 2},
		{"after an indented dash", "a:\n  -\tb\n", 2, 4},
		{"after the dash of a value", "a: -\tb\n", 1, 5},
		{"after a colon", "a:\tb\n", 1, 3},
		{"after the colon of a quoted key", "\"a\":\tb\n", 1, 5},
		{"after the colon of a key on a dash line", "- a:\tb\n", 1, 5},
		{"between a quoted key and its colon", "\"a\"\t: b\n", 1, 4},
		{"before a comment", "a: x\t# c\n", 1, 5},
		{"at the end of a comment", "a: x # c\t\n", 1, 9},
		{"at the end of the comment of a key", "a: # note\t\n", 1, 10},
		{"before the comment of a key", "a:\t# note\n", 1, 3},
		{"in a flow sequence", "a: [b,\tc]\n", 1, 7},
		{"in a plain flow value", "a: [bb\tc]\n", 1, 7},
		{"in a flow collection after non-ASCII text", "\u00e9: [\u00e9\tc]\n", 1, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseYAML([]byte(tc.text))
			if err == nil || err.Rule != RuleYAMLTab || err.Line != tc.line || err.Column != tc.column {
				t.Fatalf("got %+v, want %s at line %d column %d", err, RuleYAMLTab, tc.line, tc.column)
			}
			if want := fmt.Sprintf("column %d: ", tc.column); !strings.HasPrefix(err.Message, want) {
				t.Fatalf("message %q does not begin with %q", err.Message, want)
			}
		})
	}
	// A problem that is not a tab has no column.
	if _, err := ParseYAML([]byte("a: 1\na: 2\n")); err == nil || err.Column != 0 {
		t.Fatalf("got %+v", err)
	}
}

// A comment that follows a key, a dash or the --- marker with nothing else on
// its line may hold tabs between its # and its last character, as the comment
// after a value does (the values are the ones the reference parser reads); a tab
// at the end of the comment, or before its #, is refused as everywhere else.
func TestATabInTheCommentOfAKeyOrDashLineIsRead(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]string{
		"a: # note\tmore\n  b: 1\n":                  `{"a":{"b":1}}`,
		"- # note\tmore\n  one\n":                    `["one"]`,
		"--- # note\tmore\na: 1\n":                   `{"a":1}`,
		"a:   #\tx\n":                                `{"a":null}`,
		"\"a\": # a\t\tb\n  - x\n":                   `{"a":["x"]}`,
		"- a: # c\td\n    b: 1\n- - # e\tf\n  - x\n": `[{"a":{"b":1}},[null,"x"]]`,
		"x:\n- # a\tb\n  v\n":                        `{"x":["v"]}`,
		"a: # k: a\tb\nb: 2\n":                       `{"a":null,"b":2}`,
		"a: # note\tmore\r\n  b: 1\r\n":              `{"a":{"b":1}}`,
		"--- # note\tmore\r\n- x\r\n":                `["x"]`,
		"---   # note\tmore\n":                       `null`,
	} {
		if got := valueOf(t, text); got != want {
			t.Errorf("%q: got %s, want %s", text, got, want)
		}
	}
	for _, text := range []string{"a: # note\t\n  b: 1\n", "- # note\t\n  one\n", "--- # note\t\na: 1\n", "a:\t# note\n  b: 1\n", "-\t# note\n  one\n"} {
		if _, err := ParseYAML([]byte(text)); err == nil || err.Rule != RuleYAMLTab {
			t.Errorf("%q: got %v, want %s", text, err, RuleYAMLTab)
		}
	}
}

// The tab in such a comment changes nothing but the tab: for the heads (a key,
// a dash, the marker, in several places), the comments, the lines that follow
// and the line ends of a generated family (the one the reference parser was
// asked about when the position was accepted), a document is read as the same
// document with spaces in place of the tabs of the comment, or refused for the
// same rule on the same line.
func TestATabInTheCommentOfAKeyOrDashLineChangesNothingElse(t *testing.T) {
	t.Parallel()
	heads := []struct {
		prefix string
		head   func(comment string) string
		column int
	}{
		{"", func(c string) string { return "a: " + c }, 0},
		{"x:\n", func(c string) string { return "  a:  " + c }, 2},
		{"", func(c string) string { return `"a": ` + c }, 0},
		{"", func(c string) string { return "- a: " + c }, 2},
		{"", func(c string) string { return "- " + c }, 0},
		{"x:\n", func(c string) string { return "- " + c }, 0},
		{"", func(c string) string { return "- - " + c }, 2},
		{"", func(c string) string { return "--- " + c }, 0},
		{"# top\n", func(c string) string { return "---   " + c }, 0},
	}
	comments := []string{"# note\tmore", "#\tx", "# \tx", "# a\t\tb", "# a\t: b", "# a\t- b", "# a\t[b", "# a\t\"q", "#\t#"}
	tails := func(o int) []string {
		in, at := strings.Repeat(" ", o+2), strings.Repeat(" ", o)
		return []string{"", in + "b: 1\n", in + "- x\n", in + "v\n", in + "v\n" + in + "w\n", in + "\"v\"\n", in + "[x, y]\n", in + "# k\n" + in + "b: 1\n",
			in + "# k\n" + in + "v\n", "\n" + in + "b: 1\n", in + "# a\tb\n" + in + "b: 1\n", at + "b: 2\n", at + "- x\n", "z: 9\n"}
	}
	n := 0
	for _, eol := range []string{"\n", "\r\n"} {
		for _, h := range heads {
			for _, c := range comments {
				for _, tail := range tails(h.column) {
					doc := strings.ReplaceAll(h.prefix+h.head(c)+"\n"+tail, "\n", eol)
					twin := strings.ReplaceAll(h.prefix+h.head(strings.ReplaceAll(c, "\t", " "))+"\n"+tail, "\n", eol)
					got, gotErr := ParseYAML([]byte(doc))
					want, wantErr := ParseYAML([]byte(twin))
					switch {
					case (gotErr == nil) != (wantErr == nil):
						t.Fatalf("%q: %v, but %q: %v", doc, gotErr, twin, wantErr)
					case gotErr != nil && (gotErr.Rule != wantErr.Rule || gotErr.Line != wantErr.Line):
						t.Fatalf("%q: %v, but %q: %v", doc, gotErr, twin, wantErr)
					case gotErr == nil && !reflect.DeepEqual(got.Value(), want.Value()):
						t.Fatalf("%q reads %v, but %q reads %v", doc, got.Value(), twin, want.Value())
					}
					n++
				}
			}
		}
	}
	if n != 2*9*9*14 {
		t.Fatalf("%d documents", n)
	}
}
