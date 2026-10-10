package meaning

import (
	"errors"
	"strings"
	"testing"
)

const earlierFile = `# A shop, in the earlier format.
format: meaning/draft-1
id: shop
name: Shop
description: d
models: {shop: shop.modelspec.hcl}
concepts:
  - id: customer   # the people who buy
    kind: entity
    labels: {en: Customer}
    description: A customer.
    bindings:
      - model: modelspec:///shop.Customer
        role: entity
      - model: modelspec:///shop.Customer
        role: identifier
        property: Id    # the key
  - {id: total, kind: attribute, labels: {en: Total}, description: A total., bindings: [{model: 'modelspec:///shop.Order', role: value, property: Total}]}
  - id: order-customer
    kind: "attribute"
    labels: {en: Customer of an order}
    description: Who placed it.
    values-of: customer
    bindings:
      - {model: 'modelspec:///shop.Order', role: 'foreign-key', property: CustomerId}
  - id: status
    kind: entity
    labels: {en: Status}
    description: The states of an order.
    values: [{id: open, labels: {en: Open}}]
  - id: area
    kind: dimension
    labels: {en: Area}
    description: An area.
`

const currentFile = `# A shop, in the earlier format.
format: meaning/draft-2
id: shop
name: Shop
description: d
models: {shop: shop.modelspec.hcl}
concepts:
  - id: customer   # the people who buy
    kind: entity
    labels: {en: Customer}
    description: A customer.
    bindings:
      - model: modelspec:///shop.Customer
        role: instances
      - model: modelspec:///shop.Customer
        role: identifier
        field: Id    # the key
  - {id: total, kind: property, labels: {en: Total}, description: A total., bindings: [{model: 'modelspec:///shop.Order', role: value, field: Total}]}
  - id: order-customer
    kind: "property"
    labels: {en: Customer of an order}
    description: Who placed it.
    values-of: customer
    bindings:
      - {model: 'modelspec:///shop.Order', role: 'reference', field: CustomerId}
  - id: status
    kind: entity
    labels: {en: Status}
    description: The states of an order.
    values: [{id: open, labels: {en: Open}}]
  - id: area
    kind: dimension
    labels: {en: Area}
    description: An area.
`

func TestRewriteDraft2ChangesTheWordsInPlaceAndNothingElse(t *testing.T) {
	t.Parallel()
	got, err := RewriteDraft2([]byte(earlierFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Text) != currentFile {
		t.Fatalf("the rewrite\n%s\nwant\n%s", got.Text, currentFile)
	}
	if got.Format != 1 || got.Kinds != 2 || got.Keys != 3 || got.Roles != 2 || !got.Changed() {
		t.Errorf("counts %+v", got)
	}
	if want := "1 format line, 2 kind attribute, 3 binding key property, 2 role name"; got.Describe() != want {
		t.Errorf("Describe() = %q, want %q", got.Describe(), want)
	}
	// A list of values is listed, with its line, and not changed.
	if len(got.Values) != 1 || got.Values[0] != (ValuesConcept{ID: "status", Kind: "entity", Line: 30}) {
		t.Errorf("values %+v", got.Values)
	}
	// The result is a file of the other format: its first word is read, and the list is the one thing left to edit.
	g := graphOf(t, map[string]string{"/g/a.meaning.yaml": string(got.Text), "/g/shop.modelspec.hcl": "x"}, "")
	if g.Format() != Draft2 {
		t.Errorf("format %q", g.Format())
	}
	// Crlf line ends are kept.
	crlf, err := RewriteDraft2([]byte(strings.ReplaceAll(earlierFile, "\n", "\r\n")))
	if err != nil || string(crlf.Text) != strings.ReplaceAll(currentFile, "\n", "\r\n") {
		t.Errorf("crlf: %v\n%+v", err, crlf)
	}
}

func TestRewriteDraft2LeavesAFileInDraft2AsItIs(t *testing.T) {
	t.Parallel()
	got, err := RewriteDraft2([]byte(currentFile))
	if err != nil || string(got.Text) != currentFile || got.Changed() || got.Describe() != "" || len(got.Values) != 0 {
		t.Errorf("%+v, %v", got, err)
	}
	// A file in the earlier format that holds none of the words only changes its format line.
	got, err = RewriteDraft2([]byte(doc(cn("a", "entity", ""))))
	if err != nil || got.Format != 1 || got.Kinds+got.Keys+got.Roles != 0 || !strings.HasPrefix(string(got.Text), "format: meaning/draft-2\n") {
		t.Errorf("%+v, %v", got, err)
	}
}

func TestRewriteDraft2RefusesWhatItCannotChangeSafely(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ text, want string }{
		"not YAML":            {"a: b\n  c: d\n", "a colon followed by a space"},
		"no format":           {"id: a\n", "says no format that is read"},
		"another format":      {strings.Replace(earlierFile, "draft-1", "draft-3", 1), "says no format that is read"},
		"not a mapping":       {"- a\n", "says no format that is read"},
		"a quoted key":        {strings.Replace(earlierFile, "        property: Id", `        "property": Id`, 1), "line 17 does not hold the binding key property exactly once"},
		"two roles on a line": {strings.Replace(earlierFile, "      - {model: 'modelspec:///shop.Order', role: 'foreign-key', property: CustomerId}", "      - {model: 'modelspec:///shop.Order', role: foreign-key, property: CustomerId, note: 'role: foreign-key'}", 1), "role: foreign-key exactly once"},
		"a word in a value, not the key": {strings.Replace(earlierFile, "  - {id: total, kind: attribute, labels: {en: Total}, description: A total., bindings: [{model: 'modelspec:///shop.Order', role: value, property: Total}]}",
			"  - {id: total, kind: attribute, labels: {en: Total}, description: A total., bindings: [{model: 'modelspec:///shop.Order', role: value, \"property\": Total, note: 'a property: of an order'}]}", 1), "does not hold the data of the file before it"},
		"a kind written over two lines": {strings.Replace(earlierFile, "    kind: \"attribute\"\n", "    kind:\n      attribute\n", 1), "line 21 does not hold kind: attribute exactly once"},
	}
	for name, tc := range tests {
		got, err := RewriteDraft2([]byte(tc.text))
		if err == nil || got != nil || !errors.Is(err, ErrRewrite) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, %v", name, got, err)
		}
	}
}

// What is not a concept or a binding is not touched, and does not stop the rewrite (the schema refuses it later).
func TestRewriteDraft2SkipsWhatIsNoConceptOrBinding(t *testing.T) {
	t.Parallel()
	text := "format: meaning/draft-1\nid: a\nconcepts:\n  - nonsense\n  - {id: a, kind: attribute, bindings: [stray, {role: entity, property: P}]}\n"
	got, err := RewriteDraft2([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if want := "format: meaning/draft-2\nid: a\nconcepts:\n  - nonsense\n  - {id: a, kind: property, bindings: [stray, {role: instances, field: P}]}\n"; string(got.Text) != want {
		t.Errorf("got %q", got.Text)
	}
}
