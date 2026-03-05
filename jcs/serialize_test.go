package jcs_test

import (
	"errors"
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/lattice-substrate/jcs-schubfach/jcs"
	"github.com/lattice-substrate/jcs-schubfach/jcserr"
	"github.com/lattice-substrate/jcs-schubfach/jcstoken"
)

// canon is a test helper: parse then serialize.
func canon(t *testing.T, in string) string {
	t.Helper()
	tree, err := jcstoken.Parse([]byte(in))
	if err != nil {
		t.Fatalf("parse %q: %v", in, err)
	}
	out, err := jcs.Serialize(tree)
	if err != nil {
		t.Fatalf("serialize %q: %v", in, err)
	}
	return string(out)
}

// --------------------------------------------------------------------------
// CANON-WS-001: No insignificant whitespace
// --------------------------------------------------------------------------

func TestSerialize_CANON_WS_001(t *testing.T) {
	got := canon(t, `{ "a" : 1 }`)
	if got != `{"a":1}` {
		t.Fatalf("got %q", got)
	}
}

// --------------------------------------------------------------------------
// CANON-STR-001: U+0008 -> \b
// --------------------------------------------------------------------------

func TestSerialize_CANON_STR_001(t *testing.T) {
	got := canon(t, `"\u0008"`)
	if got != `"\b"` {
		t.Fatalf("got %q want %q", got, `"\b"`)
	}
}

// --------------------------------------------------------------------------
// CANON-STR-002: U+0009 -> \t
// --------------------------------------------------------------------------

func TestSerialize_CANON_STR_002(t *testing.T) {
	got := canon(t, `"\u0009"`)
	if got != `"\t"` {
		t.Fatalf("got %q want %q", got, `"\t"`)
	}
}

// --------------------------------------------------------------------------
// CANON-STR-003: U+000A -> \n
// --------------------------------------------------------------------------

func TestSerialize_CANON_STR_003(t *testing.T) {
	got := canon(t, `"\u000a"`)
	if got != `"\n"` {
		t.Fatalf("got %q want %q", got, `"\n"`)
	}
}

// --------------------------------------------------------------------------
// CANON-STR-004: U+000C -> \f
// --------------------------------------------------------------------------

func TestSerialize_CANON_STR_004(t *testing.T) {
	got := canon(t, `"\u000c"`)
	if got != `"\f"` {
		t.Fatalf("got %q want %q", got, `"\f"`)
	}
}

// --------------------------------------------------------------------------
// CANON-STR-005: U+000D -> \r
// --------------------------------------------------------------------------

func TestSerialize_CANON_STR_005(t *testing.T) {
	got := canon(t, `"\u000d"`)
	if got != `"\r"` {
		t.Fatalf("got %q want %q", got, `"\r"`)
	}
}

// --------------------------------------------------------------------------
// CANON-STR-006: Other control characters -> \u00xx (lowercase)
// --------------------------------------------------------------------------

func TestSerialize_CANON_STR_006(t *testing.T) {
	got := canon(t, `"\u001f"`)
	if got != `"\u001f"` {
		t.Fatalf("got %q", got)
	}
	got = canon(t, `"\u0000"`)
	if got != `"\u0000"` {
		t.Fatalf("got %q", got)
	}
}

// --------------------------------------------------------------------------
// CANON-STR-007: U+0022 -> \"
// --------------------------------------------------------------------------

func TestSerialize_CANON_STR_007(t *testing.T) {
	got := canon(t, `"a\"b"`)
	if got != `"a\"b"` {
		t.Fatalf("got %q", got)
	}
}

// --------------------------------------------------------------------------
// CANON-STR-008: U+005C -> \\
// --------------------------------------------------------------------------

func TestSerialize_CANON_STR_008(t *testing.T) {
	got := canon(t, `"a\\b"`)
	if got != `"a\\b"` {
		t.Fatalf("got %q", got)
	}
}

// --------------------------------------------------------------------------
// CANON-STR-009: Solidus NOT escaped
// --------------------------------------------------------------------------

func TestSerialize_CANON_STR_009(t *testing.T) {
	got := canon(t, `"a\/b"`)
	if got != `"a/b"` {
		t.Fatalf("got %q", got)
	}
}

// --------------------------------------------------------------------------
// CANON-STR-010: Characters above U+001F -> raw UTF-8
// --------------------------------------------------------------------------

func TestSerialize_CANON_STR_010(t *testing.T) {
	// < > & should not be escaped
	if got := canon(t, `"<>&"`); got != `"<>&"` {
		t.Fatalf("got %q", got)
	}
	// Emoji should be raw UTF-8
	got := canon(t, `"\uD83D\uDE00"`)
	if got != `"😀"` {
		t.Fatalf("got %q", got)
	}
}

// --------------------------------------------------------------------------
// CANON-STR-011: No Unicode normalization
// --------------------------------------------------------------------------

func TestSerialize_CANON_STR_011(t *testing.T) {
	nfc := "\u00E9"  // single codepoint
	nfd := "e\u0301" // two codepoints
	v1 := &jcstoken.Value{Kind: jcstoken.KindString, Str: nfc}
	v2 := &jcstoken.Value{Kind: jcstoken.KindString, Str: nfd}
	o1, err := jcs.Serialize(v1)
	if err != nil {
		t.Fatalf("serialize NFC: %v", err)
	}
	o2, err := jcs.Serialize(v2)
	if err != nil {
		t.Fatalf("serialize NFD: %v", err)
	}
	if string(o1) == string(o2) {
		t.Fatal("normalization was applied: NFC and NFD should produce different output")
	}
}

// --------------------------------------------------------------------------
// CANON-STR-012: Strings enclosed in double quotes
// --------------------------------------------------------------------------

func TestSerialize_CANON_STR_012(t *testing.T) {
	if got := canon(t, `""`); got != `""` {
		t.Fatalf("got %q", got)
	}
	if got := canon(t, `"abc"`); got != `"abc"` {
		t.Fatalf("got %q", got)
	}
}

// --------------------------------------------------------------------------
// CANON-SORT-001: UTF-16 code-unit sort order
// --------------------------------------------------------------------------

func TestSerialize_CANON_SORT_001(t *testing.T) {
	// Basic BMP sort
	got := canon(t, `{"z":3,"a":1}`)
	if got != `{"a":1,"z":3}` {
		t.Fatalf("got %q", got)
	}
	// UTF-16 vs UTF-8 divergence: supplementary plane key sorts before
	// BMP private-use key because high surrogate (0xD800) < 0xE000.
	got = canon(t, `{"\uE000":1,"\uD800\uDC00":2}`)
	if got != "{\"𐀀\":2,\"\ue000\":1}" {
		t.Fatalf("got %q", got)
	}
	// Mixed empty/prefix/BMP/supplementary ordering.
	got = canon(t, `{"\uE000":5,"\uD83D\uDE00":4,"\uD800\uDC00":3,"aa":2,"":1}`)
	if got != "{\"\":1,\"aa\":2,\"𐀀\":3,\"😀\":4,\"\ue000\":5}" {
		t.Fatalf("got %q", got)
	}
}

// --------------------------------------------------------------------------
// CANON-SORT-002: Recursive sorting
// --------------------------------------------------------------------------

func TestSerialize_CANON_SORT_002(t *testing.T) {
	got := canon(t, `{"b":[{"z":1,"a":2}],"a":3}`)
	if got != `{"a":3,"b":[{"a":2,"z":1}]}` {
		t.Fatalf("got %q", got)
	}
}

// --------------------------------------------------------------------------
// CANON-SORT-003: Array order preserved
// --------------------------------------------------------------------------

func TestSerialize_CANON_SORT_003(t *testing.T) {
	got := canon(t, `[3,1,2]`)
	if got != `[3,1,2]` {
		t.Fatalf("got %q", got)
	}
}

// --------------------------------------------------------------------------
// CANON-SORT-004: Sorting uses unescaped (decoded) property names
// --------------------------------------------------------------------------

func TestSerialize_CANON_SORT_004(t *testing.T) {
	got := canon(t, `{"\\n":1,"\n":2}`)
	if got != `{"\n":2,"\\n":1}` {
		t.Fatalf("got %q", got)
	}
}

// --------------------------------------------------------------------------
// CANON-SORT-005: Prefix ordering (shorter key first)
// --------------------------------------------------------------------------

func TestSerialize_CANON_SORT_005(t *testing.T) {
	got := canon(t, `{"ab":4,"aa":3,"":1,"a":2}`)
	if got != `{"":1,"a":2,"aa":3,"ab":4}` {
		t.Fatalf("got %q", got)
	}
}

// --------------------------------------------------------------------------
// CANON-LIT-001: Lowercase literals
// --------------------------------------------------------------------------

func TestSerialize_CANON_LIT_001(t *testing.T) {
	if got := canon(t, `true`); got != `true` {
		t.Fatalf("got %q", got)
	}
	if got := canon(t, `false`); got != `false` {
		t.Fatalf("got %q", got)
	}
	if got := canon(t, `null`); got != `null` {
		t.Fatalf("got %q", got)
	}
}

// --------------------------------------------------------------------------
// CANON-ENC-001: Output is UTF-8
// --------------------------------------------------------------------------

func TestSerialize_CANON_ENC_001(t *testing.T) {
	got := canon(t, `{"key":"value"}`)
	if !utf8.ValidString(got) {
		t.Fatal("output is not valid UTF-8")
	}
}

// --------------------------------------------------------------------------
// CANON-ENC-002: No UTF-8 BOM
// --------------------------------------------------------------------------

func TestSerialize_CANON_ENC_002(t *testing.T) {
	got := canon(t, `{"a":1}`)
	if len(got) >= 3 && got[0] == 0xEF && got[1] == 0xBB && got[2] == 0xBF {
		t.Fatalf("unexpected UTF-8 BOM prefix in %q", got)
	}
}

// --------------------------------------------------------------------------
// GEN-GRAM-001: Generator output is valid JSON
// --------------------------------------------------------------------------

func TestSerialize_GEN_GRAM_001(t *testing.T) {
	v, err := jcstoken.Parse([]byte(`{"z":[{"b":"\u0000","a":1e21}],"a":true}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := jcs.Serialize(v)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	if _, err := jcstoken.Parse(out); err != nil {
		t.Fatalf("generated output is not valid JSON grammar: %v", err)
	}
}

// --------------------------------------------------------------------------
// Validation: non-finite numbers rejected
// --------------------------------------------------------------------------

func TestSerializeRejectsNonFiniteNumber(t *testing.T) {
	for _, f := range []float64{math.Inf(1), math.Inf(-1), math.NaN()} {
		v := &jcstoken.Value{Kind: jcstoken.KindNumber, Num: f}
		_, err := jcs.Serialize(v)
		if err == nil {
			t.Fatalf("expected error for %v", f)
		}
		var je *jcserr.Error
		if !errors.As(err, &je) || je.Class != jcserr.InvalidGrammar {
			t.Fatalf("expected INVALID_GRAMMAR for %v, got %v", f, err)
		}
	}
}

// --------------------------------------------------------------------------
// Validation: nil value rejected
// --------------------------------------------------------------------------

func TestSerializeRejectsNilValue(t *testing.T) {
	_, err := jcs.Serialize(nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var je *jcserr.Error
	if !errors.As(err, &je) || je.Class != jcserr.InternalError {
		t.Fatalf("expected INTERNAL_ERROR, got %v", err)
	}
}

// --------------------------------------------------------------------------
// Validation: invalid bool payload rejected
// --------------------------------------------------------------------------

func TestSerializeRejectsInvalidBoolPayload(t *testing.T) {
	v := &jcstoken.Value{Kind: jcstoken.KindBool, Str: "TRUE"}
	_, err := jcs.Serialize(v)
	if err == nil {
		t.Fatal("expected error")
	}
	var je *jcserr.Error
	if !errors.As(err, &je) || je.Class != jcserr.InvalidGrammar {
		t.Fatalf("expected INVALID_GRAMMAR, got %v", err)
	}
}

// --------------------------------------------------------------------------
// Validation: invalid UTF-8 in string payload rejected
// --------------------------------------------------------------------------

func TestSerializeRejectsInvalidUTF8StringPayload(t *testing.T) {
	v := &jcstoken.Value{Kind: jcstoken.KindString, Str: string([]byte{0xff})}
	_, err := jcs.Serialize(v)
	if err == nil {
		t.Fatal("expected error")
	}
	var je *jcserr.Error
	if !errors.As(err, &je) || je.Class != jcserr.InvalidUTF8 {
		t.Fatalf("expected INVALID_UTF8, got %v", err)
	}
}

// --------------------------------------------------------------------------
// Validation: duplicate keys in value tree rejected
// --------------------------------------------------------------------------

func TestSerializeRejectsDuplicateKeysInValueTree(t *testing.T) {
	v := &jcstoken.Value{
		Kind: jcstoken.KindObject,
		Members: []jcstoken.Member{
			{Key: "a", Value: jcstoken.Value{Kind: jcstoken.KindNumber, Num: 1}},
			{Key: "a", Value: jcstoken.Value{Kind: jcstoken.KindNumber, Num: 2}},
		},
	}
	_, err := jcs.Serialize(v)
	if err == nil {
		t.Fatal("expected error")
	}
	var je *jcserr.Error
	if !errors.As(err, &je) || je.Class != jcserr.DuplicateKey {
		t.Fatalf("expected DUPLICATE_KEY, got %v", err)
	}
}

// --------------------------------------------------------------------------
// Negative zero -> "0"
// --------------------------------------------------------------------------

func TestSerializeNegativeZero(t *testing.T) {
	v := &jcstoken.Value{Kind: jcstoken.KindNumber, Num: math.Copysign(0, -1)}
	out, err := jcs.Serialize(v)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	if string(out) != `0` {
		t.Fatalf("got %q", string(out))
	}
}

// --------------------------------------------------------------------------
// Non-object top-level values
// --------------------------------------------------------------------------

func TestSerializeNonObjectTopLevel(t *testing.T) {
	if got := canon(t, `"hello"`); got != `"hello"` {
		t.Fatalf("got %q", got)
	}
	if got := canon(t, `42`); got != `42` {
		t.Fatalf("got %q", got)
	}
}

// --------------------------------------------------------------------------
// API-CANON-001: Canonicalize == Parse + Serialize
// --------------------------------------------------------------------------

func TestCanonicalize_API_CANON_001(t *testing.T) {
	inputs := []string{
		`{"b":2,"a":1}`,
		`{ "z" : true , "a" : null }`,
		`[3, 1, 2]`,
		`"hello"`,
		`42`,
		`true`,
		`null`,
		`{"nested":{"b":2,"a":1},"outer":1}`,
	}
	for _, in := range inputs {
		want := canon(t, in)
		got, err := jcs.Canonicalize([]byte(in))
		if err != nil {
			t.Fatalf("Canonicalize(%q): %v", in, err)
		}
		if string(got) != want {
			t.Fatalf("Canonicalize(%q) = %q, want %q", in, got, want)
		}
	}
}

// --------------------------------------------------------------------------
// API-CANON-002: CanonicalizeWithOptions forwards options
// --------------------------------------------------------------------------

func TestCanonicalizeWithOptions_API_CANON_002(t *testing.T) {
	// Restrict depth to 1 and check rejection.
	deep := []byte(`{"a":{"b":1}}`)
	opts := &jcstoken.Options{MaxDepth: 1}
	_, err := jcs.CanonicalizeWithOptions(deep, opts)
	if err == nil {
		t.Fatal("expected depth error with MaxDepth=1")
	}
	var je *jcserr.Error
	if !errors.As(err, &je) {
		t.Fatalf("expected *jcserr.Error, got %T", err)
	}
	if je.Class != jcserr.BoundExceeded {
		t.Fatalf("expected BoundExceeded, got %s", je.Class)
	}

	// nil options uses defaults.
	got, err := jcs.CanonicalizeWithOptions([]byte(`{"b":1,"a":2}`), nil)
	if err != nil {
		t.Fatalf("CanonicalizeWithOptions with nil opts: %v", err)
	}
	want := canon(t, `{"b":1,"a":2}`)
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	// Depth above default (1000) valid when caller expands bounds.
	const depth = 1200
	deepNesting := []byte(strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth))
	got, err = jcs.CanonicalizeWithOptions(deepNesting, &jcstoken.Options{MaxDepth: 2000})
	if err != nil {
		t.Fatalf("CanonicalizeWithOptions deep input: %v", err)
	}
	if string(got) != string(deepNesting) {
		t.Fatalf("unexpected deep canonical output")
	}

	// Serializer-only bounds honor the provided options.
	v, err := jcstoken.Parse([]byte(`[[[0]]]`))
	if err != nil {
		t.Fatalf("parse for SerializeWithOptions: %v", err)
	}
	_, err = jcs.SerializeWithOptions(v, &jcstoken.Options{MaxDepth: 2})
	if err == nil {
		t.Fatal("expected SerializeWithOptions depth bound error")
	}
	if !errors.As(err, &je) || je.Class != jcserr.BoundExceeded {
		t.Fatalf("expected BoundExceeded, got %v", err)
	}
}

// --------------------------------------------------------------------------
// Noncharacter rejection
// --------------------------------------------------------------------------

func TestSerializeRejectsNoncharacter(t *testing.T) {
	// U+FFFE is a noncharacter
	v := &jcstoken.Value{Kind: jcstoken.KindString, Str: string(rune(0xFFFE))}
	_, err := jcs.Serialize(v)
	if err == nil {
		t.Fatal("expected error for noncharacter")
	}
	var je *jcserr.Error
	if !errors.As(err, &je) || je.Class != jcserr.Noncharacter {
		t.Fatalf("expected NONCHARACTER, got %v", err)
	}
}

// --------------------------------------------------------------------------
// Bounds: value count
// --------------------------------------------------------------------------

func TestSerializeRejectsExcessiveValues(t *testing.T) {
	// Build a small tree but restrict max values to 1
	v := &jcstoken.Value{
		Kind: jcstoken.KindArray,
		Elems: []jcstoken.Value{
			{Kind: jcstoken.KindNull},
			{Kind: jcstoken.KindNull},
		},
	}
	_, err := jcs.SerializeWithOptions(v, &jcstoken.Options{MaxValues: 1})
	if err == nil {
		t.Fatal("expected error")
	}
	var je *jcserr.Error
	if !errors.As(err, &je) || je.Class != jcserr.BoundExceeded {
		t.Fatalf("expected BoundExceeded, got %v", err)
	}
}

// --------------------------------------------------------------------------
// Bounds: string length
// --------------------------------------------------------------------------

func TestSerializeRejectsLongString(t *testing.T) {
	long := strings.Repeat("x", 100)
	v := &jcstoken.Value{Kind: jcstoken.KindString, Str: long}
	_, err := jcs.SerializeWithOptions(v, &jcstoken.Options{MaxStringBytes: 50})
	if err == nil {
		t.Fatal("expected error")
	}
	var je *jcserr.Error
	if !errors.As(err, &je) || je.Class != jcserr.BoundExceeded {
		t.Fatalf("expected BoundExceeded, got %v", err)
	}
}

// --------------------------------------------------------------------------
// Bounds: array element count
// --------------------------------------------------------------------------

func TestSerializeRejectsLargeArray(t *testing.T) {
	elems := make([]jcstoken.Value, 10)
	for i := range elems {
		elems[i] = jcstoken.Value{Kind: jcstoken.KindNull}
	}
	v := &jcstoken.Value{Kind: jcstoken.KindArray, Elems: elems}
	_, err := jcs.SerializeWithOptions(v, &jcstoken.Options{MaxArrayElements: 5})
	if err == nil {
		t.Fatal("expected error")
	}
	var je *jcserr.Error
	if !errors.As(err, &je) || je.Class != jcserr.BoundExceeded {
		t.Fatalf("expected BoundExceeded, got %v", err)
	}
}

// --------------------------------------------------------------------------
// Bounds: object member count
// --------------------------------------------------------------------------

func TestSerializeRejectsLargeObject(t *testing.T) {
	members := make([]jcstoken.Member, 10)
	for i := range members {
		members[i] = jcstoken.Member{
			Key:   strings.Repeat("k", i+1),
			Value: jcstoken.Value{Kind: jcstoken.KindNull},
		}
	}
	v := &jcstoken.Value{Kind: jcstoken.KindObject, Members: members}
	_, err := jcs.SerializeWithOptions(v, &jcstoken.Options{MaxObjectMembers: 5})
	if err == nil {
		t.Fatal("expected error")
	}
	var je *jcserr.Error
	if !errors.As(err, &je) || je.Class != jcserr.BoundExceeded {
		t.Fatalf("expected BoundExceeded, got %v", err)
	}
}

// --------------------------------------------------------------------------
// Unknown Kind rejected
// --------------------------------------------------------------------------

func TestSerializeRejectsUnknownKind(t *testing.T) {
	v := &jcstoken.Value{Kind: jcstoken.Kind(99)}
	_, err := jcs.Serialize(v)
	if err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

// --------------------------------------------------------------------------
// Empty object and array
// --------------------------------------------------------------------------

func TestSerializeEmptyContainers(t *testing.T) {
	if got := canon(t, `{}`); got != `{}` {
		t.Fatalf("got %q", got)
	}
	if got := canon(t, `[]`); got != `[]` {
		t.Fatalf("got %q", got)
	}
}

// --------------------------------------------------------------------------
// Number serialization via jcsfloat
// --------------------------------------------------------------------------

func TestSerializeNumbers(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`0`, `0`},
		{`1`, `1`},
		{`-1`, `-1`},
		{`1.5`, `1.5`},
		{`1e21`, `1e+21`},
		{`0.1`, `0.1`},
		{`1e-7`, `1e-7`},
	}
	for _, tc := range tests {
		got := canon(t, tc.input)
		if got != tc.want {
			t.Errorf("canon(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// --------------------------------------------------------------------------
// Idempotency: serialize(serialize(x)) == serialize(x)
// --------------------------------------------------------------------------

func TestSerializeIdempotent(t *testing.T) {
	inputs := []string{
		`{"b":2,"a":1}`,
		`[1,"hello",null,true,false]`,
		`{"nested":{"z":1,"a":2},"top":3}`,
	}
	for _, in := range inputs {
		first := canon(t, in)
		second := canon(t, first)
		if first != second {
			t.Fatalf("not idempotent: first=%q second=%q", first, second)
		}
	}
}

// --------------------------------------------------------------------------
// All 32 control characters tested
// --------------------------------------------------------------------------

func TestSerializeAllControlCharacters(t *testing.T) {
	shortEscapes := map[byte]string{
		0x08: `\b`,
		0x09: `\t`,
		0x0A: `\n`,
		0x0C: `\f`,
		0x0D: `\r`,
	}
	for i := 0; i < 0x20; i++ {
		v := &jcstoken.Value{Kind: jcstoken.KindString, Str: string(rune(i))}
		out, err := jcs.Serialize(v)
		if err != nil {
			t.Fatalf("serialize U+%04X: %v", i, err)
		}
		s := string(out)
		if esc, ok := shortEscapes[byte(i)]; ok {
			want := `"` + esc + `"`
			if s != want {
				t.Errorf("U+%04X: got %q, want %q", i, s, want)
			}
		} else {
			want := `"` + `\u00` + string(lowerHex(byte(i)>>4)) + string(lowerHex(byte(i)&0x0F)) + `"`
			if s != want {
				t.Errorf("U+%04X: got %q, want %q", i, s, want)
			}
		}
	}
}

func lowerHex(nib byte) byte {
	const digits = "0123456789abcdef"
	return digits[nib&0x0F]
}
