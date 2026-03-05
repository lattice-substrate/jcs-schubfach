// Package jcs implements RFC 8785 JSON Canonicalization Scheme serialization.
//
// Given a parsed Value tree (from jcstoken), this package produces the exact
// canonical byte sequence specified by RFC 8785. Number formatting uses the
// jcsfloat package (Schubfach-based ECMA-262 Number::toString), and object
// property names are sorted by UTF-16 code-unit order as mandated by
// RFC 8785 section 3.2.3.
package jcs

import (
	"fmt"
	"math"
	"sort"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/lattice-substrate/jcs-schubfach/jcserr"
	"github.com/lattice-substrate/jcs-schubfach/jcsfloat"
	"github.com/lattice-substrate/jcs-schubfach/jcstoken"
)

// --------------------------------------------------------------------------
// Public API
// --------------------------------------------------------------------------

// Canonicalize parses raw JSON bytes and emits the RFC 8785 canonical form.
//
// API-CANON-001: The result is identical to parsing with jcstoken.Parse
// followed by Serialize.
func Canonicalize(input []byte) ([]byte, error) {
	tree, err := jcstoken.Parse(input)
	if err != nil {
		return nil, err //nolint:wrapcheck // REQ:API-CANON-001 pass through jcstoken parse errors unchanged.
	}
	return emit(make([]byte, 0, len(input)), tree, nil)
}

// CanonicalizeWithOptions behaves like Canonicalize but forwards caller-
// supplied options to the parser and the serialization validator.
//
// API-CANON-002: Options are forwarded to ParseWithOptions unchanged.
func CanonicalizeWithOptions(input []byte, opts *jcstoken.Options) ([]byte, error) {
	tree, err := jcstoken.ParseWithOptions(input, opts)
	if err != nil {
		return nil, err //nolint:wrapcheck // REQ:API-CANON-002 pass through jcstoken parse errors unchanged.
	}
	return emit(make([]byte, 0, len(input)), tree, opts)
}

// Serialize encodes a value tree into RFC 8785 canonical JSON bytes.
//
// CANON-ENC-001: Output encoding is UTF-8.
// CANON-WS-001:  No insignificant whitespace is emitted.
func Serialize(v *jcstoken.Value) ([]byte, error) {
	return emit(nil, v, nil)
}

// SerializeWithOptions is like Serialize but validates bounds from opts before
// emitting.
func SerializeWithOptions(v *jcstoken.Value, opts *jcstoken.Options) ([]byte, error) {
	return emit(nil, v, opts)
}

// --------------------------------------------------------------------------
// Internal: top-level emit entry point
// --------------------------------------------------------------------------

func emit(dst []byte, v *jcstoken.Value, opts *jcstoken.Options) ([]byte, error) {
	if v == nil {
		return nil, jcserr.New(jcserr.InternalError, -1, "jcs: nil value")
	}

	lim := buildLimits(opts)
	vs := &validationCounter{}
	if err := checkTree(v, 0, vs, lim); err != nil {
		return nil, err
	}

	return writeValue(dst, v)
}

// --------------------------------------------------------------------------
// Recursive value writer
// --------------------------------------------------------------------------

func writeValue(dst []byte, v *jcstoken.Value) ([]byte, error) {
	switch v.Kind {
	case jcstoken.KindNull:
		// CANON-LIT-001: lowercase null
		return append(dst, "null"...), nil

	case jcstoken.KindBool:
		// CANON-LIT-001: lowercase true / false
		return append(dst, v.Str...), nil

	case jcstoken.KindNumber:
		return writeNumber(dst, v.Num)

	case jcstoken.KindString:
		return writeString(dst, v.Str), nil

	case jcstoken.KindArray:
		return writeArray(dst, v)

	case jcstoken.KindObject:
		return writeObject(dst, v)

	default:
		return nil, jcserr.New(jcserr.InternalError, -1,
			fmt.Sprintf("jcs: unrecognised value kind %d", v.Kind))
	}
}

// --------------------------------------------------------------------------
// Number serialization
// --------------------------------------------------------------------------

func writeNumber(dst []byte, f float64) ([]byte, error) {
	repr, err := jcsfloat.FormatDouble(f)
	if err != nil {
		return nil, jcserr.Wrap(err.Class, -1, "jcs: number serialization error", err)
	}
	return append(dst, repr...), nil
}

// --------------------------------------------------------------------------
// String serialization  (RFC 8785 section 3.2.2.2)
// --------------------------------------------------------------------------
//
// CANON-STR-001: U+0008 -> \b
// CANON-STR-002: U+0009 -> \t
// CANON-STR-003: U+000A -> \n
// CANON-STR-004: U+000C -> \f
// CANON-STR-005: U+000D -> \r
// CANON-STR-006: Other controls U+0000..U+001F -> \u00xx (lowercase hex)
// CANON-STR-007: U+0022 -> \"
// CANON-STR-008: U+005C -> \\
// CANON-STR-009: U+002F solidus is NOT escaped
// CANON-STR-010: Characters > U+001F (except " and \) stay as raw UTF-8
// CANON-STR-011: No Unicode normalization applied
// CANON-STR-012: No BOM

func writeString(dst []byte, s string) []byte {
	dst = append(dst, '"')

	i := 0
	for i < len(s) {
		b := s[i]

		if escaped, ok := escapeOneByte(b); ok {
			dst = append(dst, escaped...)
			i++
			continue
		}

		// Passthrough span: either a single ASCII byte >= 0x20 (not " or \),
		// or a complete multi-byte UTF-8 sequence.
		span := rawSpan(s, i)
		dst = append(dst, s[i:i+span]...)
		i += span
	}

	dst = append(dst, '"')
	return dst
}

// escapeOneByte returns the JCS escape sequence for b if b requires escaping,
// or (nil, false) otherwise.
func escapeOneByte(b byte) ([]byte, bool) {
	switch b {
	case '"': // CANON-STR-007
		return []byte{'\\', '"'}, true
	case '\\': // CANON-STR-008
		return []byte{'\\', '\\'}, true
	case '\b': // CANON-STR-001
		return []byte{'\\', 'b'}, true
	case '\t': // CANON-STR-002
		return []byte{'\\', 't'}, true
	case '\n': // CANON-STR-003
		return []byte{'\\', 'n'}, true
	case '\f': // CANON-STR-004
		return []byte{'\\', 'f'}, true
	case '\r': // CANON-STR-005
		return []byte{'\\', 'r'}, true
	default:
		if b < 0x20 { // CANON-STR-006
			return []byte{'\\', 'u', '0', '0', lowerHex(b >> 4), lowerHex(b & 0x0F)}, true
		}
		return nil, false
	}
}

// lowerHex returns the lowercase hexadecimal digit for a nibble value 0..15.
func lowerHex(nib byte) byte {
	const digits = "0123456789abcdef"
	return digits[nib&0x0F]
}

// rawSpan returns the number of bytes starting at s[pos] that can be copied
// verbatim (either one ASCII byte or a full multi-byte UTF-8 sequence).
func rawSpan(s string, pos int) int {
	lead := s[pos]
	if lead < 0x80 {
		return 1
	}
	seqLen := utf8RuneLen(lead)
	if pos+seqLen > len(s) {
		return len(s) - pos // truncated sequence: copy remaining bytes
	}
	return seqLen
}

// utf8RuneLen returns the byte length of a UTF-8 sequence from its lead byte.
func utf8RuneLen(lead byte) int {
	switch {
	case lead < 0xC0:
		return 1
	case lead < 0xE0:
		return 2
	case lead < 0xF0:
		return 3
	default:
		return 4
	}
}

// --------------------------------------------------------------------------
// Array serialization
// --------------------------------------------------------------------------

func writeArray(dst []byte, v *jcstoken.Value) ([]byte, error) {
	// CANON-SORT-003: array element order is preserved
	dst = append(dst, '[')
	for idx := range v.Elems {
		if idx > 0 {
			dst = append(dst, ',')
		}
		var err error
		dst, err = writeValue(dst, &v.Elems[idx])
		if err != nil {
			return nil, err
		}
	}
	dst = append(dst, ']')
	return dst, nil
}

// --------------------------------------------------------------------------
// Object serialization with UTF-16 code-unit key ordering
// --------------------------------------------------------------------------
//
// CANON-SORT-001: Keys are compared by UTF-16 code-unit value, NOT by raw
//                 UTF-8 byte order.
// CANON-SORT-002: Sorting is applied recursively (handled by writeValue).
// CANON-SORT-004: ASCII fast-path -- for keys consisting solely of U+0000..
//                 U+007F, byte order and UTF-16 code-unit order coincide.
// CANON-SORT-005: Stability of equal-key ordering is irrelevant because
//                 duplicate keys are rejected during validation.

// orderedMember pairs a Member with its optional pre-encoded UTF-16 key for
// sorting.
type orderedMember struct {
	m     jcstoken.Member
	units []uint16 // nil when the key is pure ASCII
}

func writeObject(dst []byte, v *jcstoken.Value) ([]byte, error) {
	entries := make([]orderedMember, len(v.Members))
	for i := range v.Members {
		entries[i].m = v.Members[i]
		if !allASCII(v.Members[i].Key) {
			entries[i].units = utf16.Encode([]rune(v.Members[i].Key))
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return keyLess(&entries[i], &entries[j])
	})

	dst = append(dst, '{')
	for i := range entries {
		if i > 0 {
			dst = append(dst, ',')
		}
		dst = writeString(dst, entries[i].m.Key)
		dst = append(dst, ':')
		var err error
		dst, err = writeValue(dst, &entries[i].m.Value)
		if err != nil {
			return nil, err
		}
	}
	dst = append(dst, '}')
	return dst, nil
}

// allASCII reports whether every byte in s is in [0x00, 0x7F].
func allASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 0x7F {
			return false
		}
	}
	return true
}

// keyLess compares two orderedMember entries using UTF-16 code-unit ordering.
// If both keys are pure ASCII (units == nil), plain string comparison is used
// as an optimisation because byte-value order equals code-unit order for
// codepoints below U+0080.
func keyLess(a, b *orderedMember) bool {
	if a.units == nil && b.units == nil {
		return a.m.Key < b.m.Key
	}
	au := a.units
	if au == nil {
		au = utf16.Encode([]rune(a.m.Key))
	}
	bu := b.units
	if bu == nil {
		bu = utf16.Encode([]rune(b.m.Key))
	}
	return cmpUTF16(au, bu) < 0
}

// cmpUTF16 performs a lexicographic comparison of two UTF-16 code-unit
// sequences, returning -1, 0, or +1.
func cmpUTF16(a, b []uint16) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	default:
		return 0
	}
}

// --------------------------------------------------------------------------
// Validation: pre-serialization tree check
// --------------------------------------------------------------------------

type validationCounter struct {
	total int
}

type limits struct {
	depth    int
	values   int
	members  int
	elems    int
	strBytes int
}

func buildLimits(opts *jcstoken.Options) limits {
	if opts == nil {
		return limits{
			depth:    jcstoken.DefaultMaxDepth,
			values:   jcstoken.DefaultMaxValues,
			members:  jcstoken.DefaultMaxObjectMembers,
			elems:    jcstoken.DefaultMaxArrayElements,
			strBytes: jcstoken.DefaultMaxStringBytes,
		}
	}
	return limits{
		depth:    pick(opts.MaxDepth, jcstoken.DefaultMaxDepth),
		values:   pick(opts.MaxValues, jcstoken.DefaultMaxValues),
		members:  pick(opts.MaxObjectMembers, jcstoken.DefaultMaxObjectMembers),
		elems:    pick(opts.MaxArrayElements, jcstoken.DefaultMaxArrayElements),
		strBytes: pick(opts.MaxStringBytes, jcstoken.DefaultMaxStringBytes),
	}
}

func pick(custom, fallback int) int {
	if custom > 0 {
		return custom
	}
	return fallback
}

//nolint:gocyclo,cyclop,gocognit // REQ:IJSON-DUP-001 spec-bound validation logic is intentionally explicit for requirement traceability.
func checkTree(v *jcstoken.Value, depth int, vc *validationCounter, lim limits) error {
	vc.total++
	if vc.total > lim.values {
		return jcserr.New(jcserr.BoundExceeded, -1,
			fmt.Sprintf("jcs: value count exceeds maximum %d", lim.values))
	}
	if depth > lim.depth {
		return jcserr.New(jcserr.BoundExceeded, -1,
			fmt.Sprintf("jcs: value nesting depth exceeds maximum %d", lim.depth))
	}

	switch v.Kind {
	case jcstoken.KindNull:
		// nothing to validate
		return nil

	case jcstoken.KindBool:
		if v.Str != "true" && v.Str != "false" {
			return jcserr.New(jcserr.InvalidGrammar, -1,
				fmt.Sprintf("jcs: invalid boolean payload %q", v.Str))
		}
		return nil

	case jcstoken.KindNumber:
		if math.IsNaN(v.Num) || math.IsInf(v.Num, 0) {
			return jcserr.New(jcserr.InvalidGrammar, -1,
				"jcs: number is not finite")
		}
		return nil

	case jcstoken.KindString:
		if err := checkString(v.Str, lim.strBytes); err != nil {
			return err
		}
		return nil

	case jcstoken.KindArray:
		if len(v.Elems) > lim.elems {
			return jcserr.New(jcserr.BoundExceeded, -1,
				fmt.Sprintf("jcs: array element count exceeds maximum %d", lim.elems))
		}
		for i := range v.Elems {
			if err := checkTree(&v.Elems[i], depth+1, vc, lim); err != nil {
				return err
			}
		}
		return nil

	case jcstoken.KindObject:
		if len(v.Members) > lim.members {
			return jcserr.New(jcserr.BoundExceeded, -1,
				fmt.Sprintf("jcs: object member count exceeds maximum %d", lim.members))
		}
		keys := make(map[string]struct{}, len(v.Members))
		for i := range v.Members {
			if err := checkString(v.Members[i].Key, lim.strBytes); err != nil {
				return jcserr.Wrap(err.Class, err.Offset, "jcs: invalid object key", err)
			}
			if _, dup := keys[v.Members[i].Key]; dup {
				return jcserr.New(jcserr.DuplicateKey, -1,
					fmt.Sprintf("jcs: duplicate object key %q", v.Members[i].Key))
			}
			keys[v.Members[i].Key] = struct{}{}
			if err := checkTree(&v.Members[i].Value, depth+1, vc, lim); err != nil {
				return err
			}
		}
		return nil

	default:
		return jcserr.New(jcserr.InternalError, -1,
			fmt.Sprintf("jcs: unknown value kind %d", v.Kind))
	}
}

func checkString(s string, maxBytes int) *jcserr.Error {
	if !utf8.ValidString(s) {
		return jcserr.New(jcserr.InvalidUTF8, -1,
			"jcs: string is not valid UTF-8")
	}
	if len(s) > maxBytes {
		return jcserr.New(jcserr.BoundExceeded, -1,
			fmt.Sprintf("jcs: string length exceeds maximum %d bytes", maxBytes))
	}
	for _, r := range s {
		if jcstoken.IsNoncharacter(r) {
			return jcserr.New(jcserr.Noncharacter, -1,
				fmt.Sprintf("jcs: string contains noncharacter U+%04X", r))
		}
		if r >= 0xD800 && r <= 0xDFFF {
			return jcserr.New(jcserr.LoneSurrogate, -1,
				fmt.Sprintf("jcs: string contains surrogate code point U+%04X", r))
		}
	}
	return nil
}
