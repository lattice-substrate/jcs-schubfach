// Package jcstoken provides a strict JSON tokenizer/parser for RFC 8785 JCS.
//
// It enforces RFC 8259 JSON grammar plus RFC 7493 I-JSON constraints required
// by JCS, including duplicate-key rejection after unescaping and strict string
// scalar validation (no lone surrogates, no noncharacters).
//
// All errors are returned as *jcserr.Error with a populated FailureClass.
package jcstoken

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/lattice-substrate/jcs-schubfach/jcserr"
)

// Default resource bounds for denial-of-service protection.
const (
	DefaultMaxDepth         = 1000
	DefaultMaxInputSize     = 64 * 1024 * 1024
	DefaultMaxValues        = 1_000_000
	DefaultMaxObjectMembers = 250_000
	DefaultMaxArrayElements = 250_000
	DefaultMaxStringBytes   = 8 * 1024 * 1024
	DefaultMaxNumberChars   = 4096
)

// Kind identifies the type of a JSON value.
type Kind int

const (
	// KindNull represents JSON null.
	KindNull Kind = iota
	// KindBool represents JSON true/false.
	KindBool
	// KindNumber represents JSON numbers.
	KindNumber
	// KindString represents JSON strings.
	KindString
	// KindArray represents JSON arrays.
	KindArray
	// KindObject represents JSON objects.
	KindObject
)

// Value represents a parsed JSON value.
type Value struct {
	Kind    Kind
	Str     string   // For KindString: decoded Unicode string; for KindBool: "true"/"false"
	Num     float64  // For KindNumber: IEEE 754 double
	Members []Member // For KindObject: ordered members
	Elems   []Value  // For KindArray: ordered elements
}

// Member is a key-value pair in a JSON object.
type Member struct {
	Key   string
	Value Value
}

// Options controls parser behavior.
type Options struct {
	MaxDepth         int
	MaxInputSize     int
	MaxValues        int
	MaxObjectMembers int
	MaxArrayElements int
	MaxStringBytes   int
	MaxNumberChars   int
}

func pick(val, fallback int) int {
	if val > 0 {
		return val
	}
	return fallback
}

func (o *Options) maxDepth() int {
	if o == nil {
		return DefaultMaxDepth
	}
	return pick(o.MaxDepth, DefaultMaxDepth)
}

func (o *Options) maxInputSize() int {
	if o == nil {
		return DefaultMaxInputSize
	}
	return pick(o.MaxInputSize, DefaultMaxInputSize)
}

func (o *Options) maxValues() int {
	if o == nil {
		return DefaultMaxValues
	}
	return pick(o.MaxValues, DefaultMaxValues)
}

func (o *Options) maxObjectMembers() int {
	if o == nil {
		return DefaultMaxObjectMembers
	}
	return pick(o.MaxObjectMembers, DefaultMaxObjectMembers)
}

func (o *Options) maxArrayElements() int {
	if o == nil {
		return DefaultMaxArrayElements
	}
	return pick(o.MaxArrayElements, DefaultMaxArrayElements)
}

func (o *Options) maxStringBytes() int {
	if o == nil {
		return DefaultMaxStringBytes
	}
	return pick(o.MaxStringBytes, DefaultMaxStringBytes)
}

func (o *Options) maxNumberChars() int {
	if o == nil {
		return DefaultMaxNumberChars
	}
	return pick(o.MaxNumberChars, DefaultMaxNumberChars)
}

// parser holds the state for a single parse pass.
type parser struct {
	src              []byte
	pos              int
	depth            int
	totalValues      int
	maxDepth         int
	maxValues        int
	maxObjectMembers int
	maxArrayElements int
	maxStringBytes   int
	maxNumberChars   int
}

// Parse parses a complete JSON text under RFC 8785's strict input domain.
// PARSE-UTF8-001: Input must be valid UTF-8.
// PARSE-GRAM-008: Trailing non-whitespace content is rejected.
func Parse(data []byte) (*Value, error) {
	return ParseWithOptions(data, nil)
}

// ParseWithOptions is like Parse but accepts configuration options.
func ParseWithOptions(data []byte, opts *Options) (*Value, error) {
	// BOUND-INPUT-001
	limit := opts.maxInputSize()
	if len(data) > limit {
		return nil, jcserr.New(jcserr.BoundExceeded, 0,
			fmt.Sprintf("input size %d exceeds maximum %d", len(data), limit))
	}

	// PARSE-UTF8-001, PARSE-UTF8-002
	if !utf8.Valid(data) {
		return nil, jcserr.New(jcserr.InvalidUTF8, locateInvalidUTF8(data),
			"input is not valid UTF-8")
	}

	p := &parser{
		src:              data,
		maxDepth:         opts.maxDepth(),
		maxValues:        opts.maxValues(),
		maxObjectMembers: opts.maxObjectMembers(),
		maxArrayElements: opts.maxArrayElements(),
		maxStringBytes:   opts.maxStringBytes(),
		maxNumberChars:   opts.maxNumberChars(),
	}

	p.consumeWhitespace()
	val, err := p.value()
	if err != nil {
		return nil, err
	}
	p.consumeWhitespace()

	// PARSE-GRAM-008
	if p.pos != len(p.src) {
		return nil, p.grammarErr("trailing content after JSON value")
	}
	return val, nil
}

// locateInvalidUTF8 finds the byte offset of the first invalid UTF-8 sequence.
func locateInvalidUTF8(data []byte) int {
	i := 0
	for i < len(data) {
		_, sz := utf8.DecodeRune(data[i:])
		if sz == 1 && data[i] >= 0x80 {
			return i
		}
		i += sz
	}
	return 0
}

func (p *parser) grammarErr(msg string) *jcserr.Error {
	return jcserr.New(jcserr.InvalidGrammar, p.pos, msg)
}

func (p *parser) classErr(class jcserr.FailureClass, format string, args ...any) *jcserr.Error {
	return jcserr.New(class, p.pos, fmt.Sprintf(format, args...))
}

func (p *parser) peekByte() (byte, bool) {
	if p.pos >= len(p.src) {
		return 0, false
	}
	return p.src[p.pos], true
}

func (p *parser) readByte() (byte, bool) {
	if p.pos >= len(p.src) {
		return 0, false
	}
	ch := p.src[p.pos]
	p.pos++
	return ch, true
}

func (p *parser) consume(expected byte) *jcserr.Error {
	ch, ok := p.readByte()
	if !ok {
		return p.classErr(jcserr.InvalidGrammar, "unexpected end of input, expected %q", string(expected))
	}
	if ch != expected {
		return p.classErr(jcserr.InvalidGrammar, "expected %q, got %q", string(expected), string(ch))
	}
	return nil
}

// PARSE-GRAM-006: Insignificant whitespace.
func (p *parser) consumeWhitespace() {
	for p.pos < len(p.src) {
		switch p.src[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

// BOUND-DEPTH-001.
func (p *parser) enterContainer() *jcserr.Error {
	p.depth++
	if p.depth > p.maxDepth {
		return p.classErr(jcserr.BoundExceeded,
			"nesting depth %d exceeds maximum %d", p.depth, p.maxDepth)
	}
	return nil
}

func (p *parser) leaveContainer() {
	p.depth--
}

func (p *parser) value() (*Value, error) {
	// BOUND-VALUES-001
	p.totalValues++
	if p.totalValues > p.maxValues {
		return nil, p.classErr(jcserr.BoundExceeded,
			"value count %d exceeds maximum %d", p.totalValues, p.maxValues)
	}

	ch, ok := p.peekByte()
	if !ok {
		return nil, p.grammarErr("unexpected end of input")
	}

	switch ch {
	case '{':
		return p.object()
	case '[':
		return p.array()
	case '"':
		return p.str()
	case 't', 'f':
		return p.boolean()
	case 'n':
		return p.null()
	default:
		return p.number()
	}
}

//nolint:gocyclo,cyclop,gocognit // REQ:IJSON-DUP-001 duplicate-key enforcement keeps this parser path branch-heavy by design.
func (p *parser) object() (*Value, error) {
	if err := p.enterContainer(); err != nil {
		return nil, err
	}
	defer p.leaveContainer()

	if err := p.consume('{'); err != nil {
		return nil, err
	}
	p.consumeWhitespace()

	result := &Value{Kind: KindObject}
	keys := make(map[string]int) // key -> byte offset of first occurrence

	ch, ok := p.peekByte()
	if !ok {
		return nil, p.grammarErr("unexpected end of input in object")
	}
	if ch == '}' {
		p.pos++
		return result, nil
	}

	for {
		p.consumeWhitespace()
		keyOffset := p.pos

		keyVal, err := p.str()
		if err != nil {
			return nil, err
		}
		key := keyVal.Str

		// IJSON-DUP-001, IJSON-DUP-002: duplicate detection after escape decoding
		if prevOff, dup := keys[key]; dup {
			return nil, jcserr.New(jcserr.DuplicateKey, keyOffset,
				fmt.Sprintf("duplicate object key %q (first at byte %d)", key, prevOff))
		}
		keys[key] = keyOffset

		p.consumeWhitespace()
		if err := p.consume(':'); err != nil {
			return nil, err
		}
		p.consumeWhitespace()

		memberVal, err := p.value()
		if err != nil {
			return nil, err
		}

		// BOUND-MEMBERS-001
		if len(result.Members) >= p.maxObjectMembers {
			return nil, p.classErr(jcserr.BoundExceeded,
				"object member count exceeds maximum %d", p.maxObjectMembers)
		}
		result.Members = append(result.Members, Member{Key: key, Value: *memberVal})

		p.consumeWhitespace()
		ch, ok := p.peekByte()
		if !ok {
			return nil, p.grammarErr("unexpected end of input in object")
		}
		if ch == '}' {
			p.pos++
			return result, nil
		}
		if ch == ',' {
			p.pos++
			continue
		}
		return nil, p.classErr(jcserr.InvalidGrammar,
			"expected ',' or '}' in object, got %q", string(ch))
	}
}

//nolint:gocyclo,cyclop // REQ:PARSE-GRAM-005 array grammar parser path is explicit for deterministic error offsets.
func (p *parser) array() (*Value, error) {
	if err := p.enterContainer(); err != nil {
		return nil, err
	}
	defer p.leaveContainer()

	if err := p.consume('['); err != nil {
		return nil, err
	}
	p.consumeWhitespace()

	result := &Value{Kind: KindArray}

	ch, ok := p.peekByte()
	if !ok {
		return nil, p.grammarErr("unexpected end of input in array")
	}
	if ch == ']' {
		p.pos++
		return result, nil
	}

	for {
		p.consumeWhitespace()
		elem, err := p.value()
		if err != nil {
			return nil, err
		}

		// BOUND-ELEMS-001
		if len(result.Elems) >= p.maxArrayElements {
			return nil, p.classErr(jcserr.BoundExceeded,
				"array element count exceeds maximum %d", p.maxArrayElements)
		}
		result.Elems = append(result.Elems, *elem)

		p.consumeWhitespace()
		ch, ok := p.peekByte()
		if !ok {
			return nil, p.grammarErr("unexpected end of input in array")
		}
		if ch == ']' {
			p.pos++
			return result, nil
		}
		if ch == ',' {
			p.pos++
			continue
		}
		return nil, p.classErr(jcserr.InvalidGrammar,
			"expected ',' or ']' in array, got %q", string(ch))
	}
}

// str parses a JSON string with full escape decoding.
// IJSON-SUR-001..003: Surrogate handling.
// IJSON-NONC-001: Noncharacter rejection.
// PARSE-GRAM-004: Unescaped control character rejection.
//
//nolint:gocyclo,cyclop,gocognit // REQ:PARSE-GRAM-004 string decode/validation follows RFC and I-JSON rules with explicit branch points.
func (p *parser) str() (*Value, error) {
	if err := p.consume('"'); err != nil {
		return nil, err
	}

	// Fast path: scan for closing quote over pure printable ASCII (0x20..0x7E)
	// with no backslash escapes. No surrogates, noncharacters, or control chars
	// are possible in this range, so character-level validation is not needed.
	anchor := p.pos
	for p.pos < len(p.src) {
		b := p.src[p.pos]
		if b == '"' {
			raw := p.src[anchor:p.pos]
			if len(raw) > p.maxStringBytes {
				return nil, p.classErr(jcserr.BoundExceeded,
					"string decoded length exceeds maximum %d bytes", p.maxStringBytes)
			}
			p.pos++
			return &Value{Kind: KindString, Str: string(raw)}, nil
		}
		if b < 0x20 || b == '\\' || b >= 0x80 {
			break
		}
		p.pos++
	}

	// General path: handle escapes, multi-byte runes, and control characters.
	buf := make([]byte, 0, p.pos-anchor+32)
	buf = append(buf, p.src[anchor:p.pos]...)

	for {
		if p.pos >= len(p.src) {
			return nil, p.grammarErr("unterminated string")
		}
		b := p.src[p.pos]

		if b == '"' {
			p.pos++
			return &Value{Kind: KindString, Str: string(buf)}, nil
		}

		if b == '\\' {
			escOff := p.pos
			p.pos++
			r, err := p.decodeEscape(escOff)
			if err != nil {
				return nil, err
			}
			if err := checkStringRune(r, escOff); err != nil {
				return nil, err
			}
			var scratch [4]byte
			n := utf8.EncodeRune(scratch[:], r)
			if len(buf)+n > p.maxStringBytes {
				return nil, p.classErr(jcserr.BoundExceeded,
					"string decoded length exceeds maximum %d bytes", p.maxStringBytes)
			}
			buf = append(buf, scratch[:n]...)
			continue
		}

		// PARSE-GRAM-004: reject unescaped control characters
		if b < 0x20 {
			return nil, p.classErr(jcserr.InvalidGrammar,
				"unescaped control character 0x%02X in string", b)
		}

		// Multi-byte UTF-8
		off := p.pos
		r, sz := utf8.DecodeRune(p.src[p.pos:])
		if r == utf8.RuneError && sz <= 1 {
			return nil, p.classErr(jcserr.InvalidUTF8,
				"invalid UTF-8 byte 0x%02X in string", b)
		}
		if err := checkStringRune(r, off); err != nil {
			return nil, err
		}
		if len(buf)+sz > p.maxStringBytes {
			return nil, p.classErr(jcserr.BoundExceeded,
				"string decoded length exceeds maximum %d bytes", p.maxStringBytes)
		}
		buf = append(buf, p.src[p.pos:p.pos+sz]...)
		p.pos += sz
	}
}

// decodeEscape handles the character immediately after a backslash.
func (p *parser) decodeEscape(origin int) (rune, *jcserr.Error) {
	if p.pos >= len(p.src) {
		return 0, jcserr.New(jcserr.InvalidGrammar, origin, "unterminated escape sequence")
	}
	b := p.src[p.pos]
	p.pos++

	if b == 'u' {
		return p.decodeUnicodeEscape(origin)
	}

	// PARSE-GRAM-010: only defined escape characters accepted
	r, ok := simpleEscape(b)
	if !ok {
		return 0, jcserr.New(jcserr.InvalidGrammar, origin,
			fmt.Sprintf("invalid escape character %q", string(b)))
	}
	return r, nil
}

// decodeUnicodeEscape parses \uXXXX (and surrogate pair \uXXXX\uXXXX).
//
//nolint:gocyclo,cyclop // REQ:IJSON-SUR-001 surrogate validation paths are explicit to preserve failure-class semantics.
func (p *parser) decodeUnicodeEscape(origin int) (rune, *jcserr.Error) {
	hi, err := p.readFourHex(origin)
	if err != nil {
		return 0, err
	}

	if !utf16.IsSurrogate(hi) {
		return hi, nil
	}

	// IJSON-SUR-002: lone low surrogate
	if hi >= 0xDC00 {
		return 0, jcserr.New(jcserr.LoneSurrogate, origin,
			fmt.Sprintf("lone low surrogate U+%04X", hi))
	}

	// IJSON-SUR-001: high surrogate must be followed by \uXXXX low surrogate
	if p.pos+1 >= len(p.src) || p.src[p.pos] != '\\' || p.src[p.pos+1] != 'u' {
		return 0, jcserr.New(jcserr.LoneSurrogate, origin,
			fmt.Sprintf("lone high surrogate U+%04X (no following \\u)", hi))
	}
	pairOrigin := p.pos
	p.pos += 2

	lo, err := p.readFourHex(pairOrigin)
	if err != nil {
		return 0, err
	}
	if lo < 0xDC00 || lo > 0xDFFF {
		return 0, jcserr.New(jcserr.LoneSurrogate, pairOrigin,
			fmt.Sprintf("high surrogate U+%04X followed by non-low-surrogate U+%04X", hi, lo))
	}

	// IJSON-SUR-003: valid pair decoded to supplementary-plane scalar
	combined := utf16.DecodeRune(hi, lo)
	if combined == unicode.ReplacementChar {
		return 0, jcserr.New(jcserr.LoneSurrogate, origin,
			fmt.Sprintf("invalid surrogate pair U+%04X U+%04X", hi, lo))
	}
	return combined, nil
}

func simpleEscape(ch byte) (rune, bool) {
	switch ch {
	case '"':
		return '"', true
	case '\\':
		return '\\', true
	case '/':
		return '/', true
	case 'b':
		return '\b', true
	case 'f':
		return '\f', true
	case 'n':
		return '\n', true
	case 'r':
		return '\r', true
	case 't':
		return '\t', true
	default:
		return 0, false
	}
}

// readFourHex reads exactly four hexadecimal digits and returns the rune value.
func (p *parser) readFourHex(origin int) (rune, *jcserr.Error) {
	if p.pos+4 > len(p.src) {
		return 0, jcserr.New(jcserr.InvalidGrammar, origin, "incomplete \\u escape")
	}
	var v rune
	for i := 0; i < 4; i++ {
		d, ok := hexDigit(p.src[p.pos+i])
		if !ok {
			fragment := string(p.src[p.pos : p.pos+4])
			return 0, jcserr.New(jcserr.InvalidGrammar, origin,
				fmt.Sprintf("invalid hex in \\u escape: %q", fragment))
		}
		v = v<<4 | rune(d)
	}
	p.pos += 4
	return v, nil
}

func hexDigit(b byte) (byte, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	default:
		return 0, false
	}
}

// checkStringRune enforces scalar-value policy at a given source offset.
func checkStringRune(r rune, off int) *jcserr.Error {
	if IsNoncharacter(r) {
		return jcserr.New(jcserr.Noncharacter, off,
			fmt.Sprintf("string contains Unicode noncharacter U+%04X", r))
	}
	if r >= 0xD800 && r <= 0xDFFF {
		return jcserr.New(jcserr.LoneSurrogate, off,
			fmt.Sprintf("string contains surrogate code point U+%04X", r))
	}
	return nil
}

// IsNoncharacter returns true if r is a Unicode noncharacter.
// IJSON-NONC-001: U+FDD0..U+FDEF and U+xFFFE, U+xFFFF for planes 0-16.
func IsNoncharacter(r rune) bool {
	if r >= 0xFDD0 && r <= 0xFDEF {
		return true
	}
	if r&0xFFFE == 0xFFFE && r <= 0x10FFFF {
		return true
	}
	return false
}

// number parses a JSON number.
// PARSE-GRAM-001: leading zeros rejected.
// PARSE-GRAM-009: number grammar enforced.
// PROF-NEGZ-001: lexical -0 rejected.
// PROF-OFLOW-001: overflow rejected.
// PROF-UFLOW-001: underflow-to-zero rejected.
func (p *parser) number() (*Value, error) {
	origin := p.pos

	// Optional leading minus sign
	if p.pos < len(p.src) && p.src[p.pos] == '-' {
		p.pos++
	}

	// Integer part
	if err := p.integerDigits(origin); err != nil {
		return nil, err
	}

	// Optional fraction
	if err := p.fractionDigits(origin); err != nil {
		return nil, err
	}

	// Optional exponent
	if err := p.exponentDigits(origin); err != nil {
		return nil, err
	}

	// BOUND-NUMCHARS-001 (final check)
	tokenLen := p.pos - origin
	if tokenLen > p.maxNumberChars {
		return nil, jcserr.New(jcserr.BoundExceeded, origin,
			fmt.Sprintf("number token length %d exceeds maximum %d", tokenLen, p.maxNumberChars))
	}

	raw := string(p.src[origin:p.pos])
	return p.buildNumber(origin, raw)
}

// PARSE-GRAM-001: leading zeros.
func (p *parser) integerDigits(numOrigin int) *jcserr.Error {
	if p.pos >= len(p.src) {
		return p.grammarErr("unexpected end of input in number")
	}

	if p.src[p.pos] == '0' {
		p.pos++
		if p.pos < len(p.src) && isDecDigit(p.src[p.pos]) {
			return p.grammarErr("leading zero in number")
		}
		return nil
	}

	if p.src[p.pos] < '1' || p.src[p.pos] > '9' {
		return p.classErr(jcserr.InvalidGrammar,
			"invalid number character %q", string(p.src[p.pos]))
	}
	for p.pos < len(p.src) && isDecDigit(p.src[p.pos]) {
		p.pos++
		// BOUND-NUMCHARS-001: fail fast during digit scanning
		if p.pos-numOrigin > p.maxNumberChars {
			return jcserr.New(jcserr.BoundExceeded, numOrigin,
				fmt.Sprintf("number token length %d exceeds maximum %d", p.pos-numOrigin, p.maxNumberChars))
		}
	}
	return nil
}

func (p *parser) fractionDigits(numOrigin int) *jcserr.Error {
	if p.pos >= len(p.src) || p.src[p.pos] != '.' {
		return nil
	}
	p.pos++

	if p.pos >= len(p.src) || !isDecDigit(p.src[p.pos]) {
		return p.grammarErr("expected digit after decimal point")
	}
	for p.pos < len(p.src) && isDecDigit(p.src[p.pos]) {
		p.pos++
		if p.pos-numOrigin > p.maxNumberChars {
			return jcserr.New(jcserr.BoundExceeded, numOrigin,
				fmt.Sprintf("number token length %d exceeds maximum %d", p.pos-numOrigin, p.maxNumberChars))
		}
	}
	return nil
}

//nolint:gocyclo,cyclop // REQ:PARSE-GRAM-009 exponent scanner mirrors JSON grammar stages for precise diagnostics.
func (p *parser) exponentDigits(numOrigin int) *jcserr.Error {
	if p.pos >= len(p.src) || (p.src[p.pos] != 'e' && p.src[p.pos] != 'E') {
		return nil
	}
	p.pos++

	if p.pos < len(p.src) && (p.src[p.pos] == '+' || p.src[p.pos] == '-') {
		p.pos++
	}
	if p.pos >= len(p.src) || !isDecDigit(p.src[p.pos]) {
		return p.grammarErr("expected digit in exponent")
	}
	for p.pos < len(p.src) && isDecDigit(p.src[p.pos]) {
		p.pos++
		if p.pos-numOrigin > p.maxNumberChars {
			return jcserr.New(jcserr.BoundExceeded, numOrigin,
				fmt.Sprintf("number token length %d exceeds maximum %d", p.pos-numOrigin, p.maxNumberChars))
		}
	}
	return nil
}

func (p *parser) buildNumber(origin int, raw string) (*Value, error) {
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil && !isRangeError(err) {
		return nil, jcserr.New(jcserr.InvalidGrammar, origin,
			fmt.Sprintf("invalid number: %v", err))
	}

	// PROF-OFLOW-001
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil, jcserr.New(jcserr.NumberOverflow, origin,
			"number overflows IEEE 754 double")
	}

	// PROF-NEGZ-001: lexical negative zero
	if strings.HasPrefix(raw, "-") && allZeroMantissa(raw) {
		return nil, jcserr.New(jcserr.NumberNegZero, origin,
			"negative zero token is not allowed")
	}

	// PROF-UFLOW-001: non-zero underflows to zero
	if f == 0 && !allZeroMantissa(raw) {
		return nil, jcserr.New(jcserr.NumberUnderflow, origin,
			"non-zero number underflows to IEEE 754 zero")
	}

	return &Value{Kind: KindNumber, Num: f}, nil
}

// allZeroMantissa returns true if the raw token has a mantissa of all zeros
// (ignoring sign and exponent). E.g. "-0", "0.0", "0e10" are all zero.
func allZeroMantissa(raw string) bool {
	start := 0
	if len(raw) > 0 && raw[0] == '-' {
		start = 1
	}
	end := len(raw)
	for i := start; i < len(raw); i++ {
		if raw[i] == 'e' || raw[i] == 'E' {
			end = i
			break
		}
	}
	for i := start; i < end; i++ {
		if raw[i] >= '1' && raw[i] <= '9' {
			return false
		}
	}
	return true
}

func isRangeError(err error) bool {
	var ne *strconv.NumError
	if !errors.As(err, &ne) {
		return false
	}
	return errors.Is(ne.Err, strconv.ErrRange)
}

func isDecDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

// PARSE-GRAM-007: invalid literals rejected.
func (p *parser) boolean() (*Value, error) {
	if p.pos+4 <= len(p.src) && string(p.src[p.pos:p.pos+4]) == "true" {
		p.pos += 4
		return &Value{Kind: KindBool, Str: "true"}, nil
	}
	if p.pos+5 <= len(p.src) && string(p.src[p.pos:p.pos+5]) == "false" {
		p.pos += 5
		return &Value{Kind: KindBool, Str: "false"}, nil
	}
	return nil, p.grammarErr("invalid literal")
}

func (p *parser) null() (*Value, error) {
	if p.pos+4 <= len(p.src) && string(p.src[p.pos:p.pos+4]) == "null" {
		p.pos += 4
		return &Value{Kind: KindNull}, nil
	}
	return nil, p.grammarErr("invalid literal")
}
