package okf

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestPortableCoreScalars(t *testing.T) {
	for _, test := range []struct{ input, json string }{
		{"TRUE", "true"}, {"True", "true"}, {"FALSE", "false"}, {"False", "false"},
		{"NULL", "null"}, {"Null", "null"}, {"", "null"}, {"~", "null"},
		{"0o17", "15"}, {"-0x2A", "-42"}, {"+0015", "15"}, {"1_234", "1234"},
		{"18446744073709551617", "18446744073709551617"},
		{"1.00000000000000000000000000001", "1.00000000000000000000000000001"},
		{"+.25", "0.25"}, {"001.50e+02", "1.50e+02"}, {"1e999999", "1e999999"},
		{"\"TRUE\"", `"TRUE"`}, {"'0o17'", `"0o17"`}, {"yes", `"yes"`},
		{"off", `"off"`}, {"2026-09-11", `"2026-09-11"`}, {"!!str TRUE", `"TRUE"`},
		{"'it''s literal'", `"it's literal"`},
		{"!!int 0o17", "15"}, {"!!float 3", "3"}, {"!!float .25", "0.25"},
		{"!!bool TRUE", "true"}, {"!!null null", "null"}, {"!!str 12", `"12"`},
		{"!!int 'nope'", `"nope"`}, {"!!float \".inf\"", `".inf"`},
		{"!!seq [one, two]", `["one","two"]`}, {"!!map {a: b}", `{"a":"b"}`},
	} {
		t.Run(test.input, func(t *testing.T) {
			value, err := ParseValue(test.input)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(value)
			if err != nil || string(encoded) != test.json {
				t.Fatalf("got %s, %v; want %s", encoded, err, test.json)
			}
			values, _, err := ParseFrontmatter("type: Probe\nvalue: " + test.input + "\n")
			if err != nil || !reflect.DeepEqual(values["value"], value) {
				t.Fatalf("parser disagreement: %#v %v", values, err)
			}
		})
	}
}

func TestPortableNestedStructuresAndAliases(t *testing.T) {
	values, order, err := ParseFrontmatter("type: Probe\n'quoted key': &shared {text: 'comma, text', items: [TRUE, 0o17, null]}\ncopy: *shared\nempty: {}\nlist: []\n")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(values["copy"], values["quoted key"]) || len(order) != 5 {
		t.Fatalf("bad projection: %#v %v", values, order)
	}
	values["copy"].(map[string]any)["text"] = "changed"
	if values["quoted key"].(map[string]any)["text"] != "comma, text" {
		t.Fatal("aliases share mutable projected values")
	}
}

func TestRejectNonportableFrontmatter(t *testing.T) {
	for _, raw := range []string{
		"type: Probe\ntype: Again", "type: Probe\nobject: {a: 1, a: 2}",
		"type: Probe\n12: numeric key", "type: Probe\n? [one, two]\n: value",
		"type: Probe\nx: !application value", "type: Probe\nx: !!timestamp 2026-09-11",
		"type: Probe\nx: .inf", "type: Probe\nx: -.Inf", "type: Probe\nx: .NaN",
		"type: Probe\nx: &cycle [*cycle]", "type: Probe\nx: *missing", "[one, two]",
		"type: Probe\n---\ntype: Again", "type: Probe\nx: [unfinished", "type: Probe\nx: 'unfinished",
		"type: Probe\nx: !!int nope", "type: Probe\nx: !!float nope", "type: Probe\nx: !!float 0x10",
		"type: Probe\nx: !!bool nope", "type: Probe\nx: !!null nonempty",
		"type: Probe\nx: !!map [a, b]", "type: Probe\nx: !!seq {a: b}",
		"type: Probe\nx: !!map 'text'", "type: Probe\nx: !!str [a]", "type: Probe\nx: !<int> 3",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, _, err := ParseFrontmatter(raw); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestFrontmatterKeyOrderAtProjectionLimit(t *testing.T) {
	for _, fields := range []int{40000, 49998, 49999} {
		t.Run(fmt.Sprint(fields), func(t *testing.T) {
			var raw strings.Builder
			raw.WriteString("type: Probe\n")
			for i := 0; i < fields; i++ {
				fmt.Fprintf(&raw, "k%d: v\n", i)
			}
			values, order, err := ParseFrontmatter(raw.String())
			if fields == 49999 {
				if err == nil || !strings.Contains(err.Error(), "resource limit") {
					t.Fatalf("expected resource error, got %v", err)
				}
				return
			}
			if err != nil || len(values) != fields+1 || len(order) != fields+1 {
				t.Fatalf("keys=%d order=%d error=%v", len(values), len(order), err)
			}
			if order[0] != "type" || order[fields] != fmt.Sprintf("k%d", fields-1) {
				t.Fatal("key order changed")
			}
		})
	}
	_, order, err := ParseFrontmatter("type: Probe\nanchor: &key 'aliased key'\n*key : value\n")
	if err != nil || !reflect.DeepEqual(order, []string{"type", "anchor", "aliased key"}) {
		t.Fatalf("alias key order: %v, %v", order, err)
	}
}

func TestProjectionLimits(t *testing.T) {
	for _, raw := range []string{
		"type: Probe\nx: " + strings.Repeat("a", maxFrontmatterBytes),
		"type: Probe\nx: " + strings.Repeat("[", 130) + "0" + strings.Repeat("]", 130),
		"type: Probe\na: &a [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]\nb: &b [*a,*a,*a,*a,*a,*a,*a,*a,*a,*a]\nc: &c [*b,*b,*b,*b,*b,*b,*b,*b,*b,*b]\nd: &d [*c,*c,*c,*c,*c,*c,*c,*c,*c,*c]\ne: &e [*d,*d,*d,*d,*d,*d,*d,*d,*d,*d]\nf: [*e,*e]",
	} {
		if _, _, err := ParseFrontmatter(raw); err == nil {
			t.Fatal("expected resource failure")
		}
	}
}

func TestPortableValuesSurviveSerialization(t *testing.T) {
	raw := "---\ntype: Probe\nstrings: ['TRUE', '15', 'comma, text', 'null', 'quote\\\"', '']\nnumber: 1.00000000000000000000000000001\nunset:\nempty: {}\nlist: []\nchild: {nested: ['15', null, {empty: {}}]}\n---\n\nBody stays.\n"
	doc, err := ParseConcept("probe", []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	encoded := Serialize(doc)
	roundtrip, err := ParseConcept("probe", encoded)
	if err != nil || !reflect.DeepEqual(doc.Frontmatter, roundtrip.Frontmatter) || doc.Markdown != roundtrip.Markdown {
		t.Fatalf("roundtrip failed: %s\n%#v\n%v", encoded, roundtrip, err)
	}
}

func TestMetadataPatchPreservesNewYAMLSyntax(t *testing.T) {
	source := "---\ntype: Probe\n'quoted key': [\n  'TRUE',\n  'comma, text'\n]\n? 'explicit key'\n: original\nanchor: &name kept\nreference: *name\n# keep this\n---\nBody\n"
	result, err := PatchFrontmatter("probe", []byte(source), map[string]any{"explicit key": "changed", "quoted key": []string{"15", "TRUE"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := ParseConcept("probe", result)
	if err != nil || doc.Frontmatter["explicit key"] != "changed" {
		t.Fatalf("bad patch: %s %v", result, err)
	}
	if !strings.Contains(string(result), "anchor: &name kept\nreference: *name\n# keep this\n---\nBody\n") {
		t.Fatalf("unrelated bytes changed: %s", result)
	}
	if _, err := PatchFrontmatter("probe", []byte(source), nil, []string{"anchor"}); err == nil {
		t.Fatal("deleting a used anchor must fail before writing invalid YAML")
	}
	if _, err := PatchFrontmatter("probe", []byte("---\n{type: Probe, field: original}\n---\n"), map[string]any{"field": "changed"}, nil); err == nil {
		t.Fatal("flow-root metadata edit must fail safely")
	}
}
