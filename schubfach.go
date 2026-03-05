// Package schubfach implements the ECMAScript Number::toString algorithm for
// IEEE 754 double-precision floating-point values using fixed-width 128-bit
// arithmetic (zero heap allocations for all finite inputs).
//
// The algorithm is specified in ECMA-262 §6.1.6.1.20 (Number::toString).
// The digit generation uses precomputed 128-bit powers of 10, 64×128-bit
// multiply for interval scaling, and shortest-output digit extraction with
// ECMA-262 Note 2 (even-digit) tie-breaking.
//
// FormatDouble is validated against large pinned ECMAScript oracle datasets and
// round-trip fuzzing for finite doubles.
package schubfach

import (
	"math"
	"math/bits"

	"github.com/lattice-substrate/jcs-schubfach/jcserr"
)

// FormatDouble formats an IEEE 754 double-precision value exactly as specified
// by the ECMAScript Number::toString algorithm (ECMA-262, radix 10).
//
// ECMA-FMT-001: NaN returns error.
// ECMA-FMT-002: -0 returns "0".
// ECMA-FMT-003: ±Infinity returns error.
// ECMA-FMT-008: Shortest round-trip representation.
// ECMA-FMT-009: Even-digit tie-breaking.
func FormatDouble(f float64) (string, *jcserr.Error) {
	// ECMA-FMT-001: NaN → error
	if math.IsNaN(f) {
		return "", jcserr.New(jcserr.InvalidGrammar, -1, "NaN is not representable in JSON")
	}
	// ECMA-FMT-002: -0 and +0 → "0"
	if f == 0 {
		return "0", nil
	}
	// ECMA-FMT-003: ±Infinity → error
	if math.IsInf(f, 0) {
		return "", jcserr.New(jcserr.InvalidGrammar, -1, "Infinity is not representable in JSON")
	}

	negative := false
	if f < 0 {
		negative = true
		f = -f
	}

	digits, dp := shortestDecimal(f)
	return formatECMA(negative, digits, dp), nil
}

const (
	mantBits64 = 52
	expBits64  = 11
	bias64     = 1023
	mantMask64 = 1<<mantBits64 - 1
	expMask64  = 1<<expBits64 - 1
)

// shortestDecimal returns the shortest decimal representation of a positive
// finite nonzero float64. Returns (digits, dp) where dp is the decimal point
// position (number of integer-part digits in fixed-point view).
func shortestDecimal(f float64) (string, int) {
	fbits := math.Float64bits(f)
	rawMant := fbits & mantMask64
	rawExp := int(fbits>>mantBits64) & expMask64

	var mant uint64
	var exp int

	if rawExp == 0 {
		// Subnormal
		mant = rawMant
		exp = 1 - bias64 - mantBits64
	} else {
		// Normal
		mant = (1 << mantBits64) | rawMant
		exp = rawExp - bias64 - mantBits64
	}

	// If input is an exact integer with fewer bits than the mantissa,
	// the previous and next integer are not admissible representations.
	if exp <= 0 && bits.TrailingZeros64(mant) >= -exp {
		m := mant >> uint(-exp)
		return formatUint(m), countDecimalDigits(m)
	}

	ml, mc, mu, e2 := computeBounds(mant, exp)
	if e2 == 0 {
		return ryuShortest(ml, mc, mu, true, false, 0)
	}

	// Find 10^q larger than 2^(-e2).
	q := mulByLog2Log10(-e2) + 1

	// Multiply all three bounds by 10^q using 128-bit arithmetic.
	var dl, dc, du uint64
	var dl0, dc0, du0 bool

	dl, _, dl0 = mult128bitPow10(ml, e2, q)
	dc, _, dc0 = mult128bitPow10(mc, e2, q)
	du, e2, du0 = mult128bitPow10(mu, e2, q)

	if e2 >= 0 {
		return "0", 0
	}

	// Exactness checks.
	if q > 55 {
		dl0, dc0, du0 = false, false, false
	}
	if q < 0 && q >= -24 {
		if divisibleByPower5(ml, -q) {
			dl0 = true
		}
		if divisibleByPower5(mc, -q) {
			dc0 = true
		}
		if divisibleByPower5(mu, -q) {
			du0 = true
		}
	}

	// Remove extra binary fraction bits, keeping rounding info.
	extra := uint(-e2)
	extraMask := uint64(1<<extra - 1)

	fracl := dl & extraMask
	fracc := dc & extraMask
	fracu := du & extraMask
	dl >>= extra
	dc >>= extra
	du >>= extra

	// Is 'du' an allowed upper bound?
	uok := !du0 || fracu > 0
	if du0 && fracu == 0 {
		uok = mant&1 == 0
	}
	if !uok {
		du--
	}

	// Should 'dc' be rounded up? (ECMA-FMT-009: even-digit tie-breaking)
	cup := false
	if dc0 {
		cup = fracc > 1<<(extra-1) ||
			(fracc == 1<<(extra-1) && dc&1 == 1)
	} else {
		cup = fracc>>(extra-1) == 1
	}

	// Is 'dl' an allowed lower bound?
	lok := dl0 && fracl == 0 && (mant&1 == 0)
	if !lok {
		dl++
	}

	c0 := dc0 && fracc == 0

	digits, dp := ryuShortest(dl, dc, du, c0, cup, q)
	return digits, dp
}

// computeBounds returns (lower, central, upper) × 2^e2 describing the
// rounding interval for mant × 2^exp.
func computeBounds(mant uint64, exp int) (lower, central, upper uint64, e2 int) {
	if mant != 1<<mantBits64 || exp == 1-bias64-mantBits64 {
		// Regular case (or subnormals)
		return 2*mant - 1, 2 * mant, 2*mant + 1, exp - 1
	}
	// Power-of-2 boundary: predecessor has different exponent.
	return 4*mant - 1, 4 * mant, 4*mant + 2, exp - 2
}

// mulByLog2Log10 returns floor(x * log(2)/log(10)).
func mulByLog2Log10(x int) int {
	return (x * 78913) >> 18
}

// mulByLog10Log2 returns floor(x * log(10)/log(2)).
func mulByLog10Log2(x int) int {
	return (x * 108853) >> 15
}

// mult128bitPow10 multiplies m by 10^q using 128-bit table lookup.
// Returns (result mantissa, result binary exponent, exactness flag).
func mult128bitPow10(m uint64, e2, q int) (uint64, int, bool) {
	if q == 0 {
		return m << 8, e2 - 8, true
	}
	if q < pow10MinExp || pow10MaxExp < q {
		return 0, 0, false
	}
	pow := pow10Table[q-pow10MinExp]
	if q < 0 {
		pow[0]++
	}
	e2 += mulByLog10Log2(q) - 127 + 119

	l1, l0 := bits.Mul64(m, pow[0])
	h1, h0 := bits.Mul64(m, pow[1])
	mid, carry := bits.Add64(l1, h0, 0)
	h1 += carry
	return h1<<9 | mid>>55, e2, mid<<9 == 0 && l0 == 0
}

// divisibleByPower5 reports whether m is divisible by 5^k.
func divisibleByPower5(m uint64, k int) bool {
	if m == 0 {
		return true
	}
	for i := 0; i < k; i++ {
		if m%5 != 0 {
			return false
		}
		m /= 5
	}
	return true
}

// ryuShortest finds the shortest decimal from the interval [dl, du] with
// center dc. Returns (digits, dp) where dp is the decimal point position.
func ryuShortest(dl, dc, du uint64, c0, cup bool, q int) (string, int) {
	// Trim trailing decimal digits while the interval allows.
	trimmed := 0
	cNextDigit := uint64(0)

	for du > 0 {
		l := (dl + 9) / 10
		c := dc / 10
		cdigit := dc % 10
		u := du / 10
		if l > u {
			break
		}
		if l == c+1 && c < u {
			c++
			cdigit = 0
			cup = false
		}
		trimmed++
		c0 = c0 && cNextDigit == 0
		cNextDigit = cdigit
		dl, dc, du = l, c, u
	}

	// Round-to-even tie-breaking (ECMA-FMT-009).
	if trimmed > 0 {
		cup = cNextDigit > 5 ||
			(cNextDigit == 5 && !c0) ||
			(cNextDigit == 5 && c0 && dc&1 == 1)
	}
	if dc < du && cup {
		dc++
	}

	// Render digits.
	var buf [20]byte
	n := 20
	v := dc
	for v > 0 {
		n--
		buf[n] = byte(v%10) + '0'
		v /= 10
	}
	if n == 20 {
		n = 19
		buf[19] = '0'
	}

	ndigits := 20 - n

	// Trim trailing zeros.
	end := 20
	for end > n+1 && buf[end-1] == '0' {
		end--
		trimmed++
	}

	dp := ndigits + trimmed - q
	return string(buf[n:end]), dp
}

// formatUint formats a uint64 as a decimal string.
func formatUint(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	n := 20
	for v > 0 {
		n--
		buf[n] = byte(v%10) + '0'
		v /= 10
	}
	// Trim trailing zeros.
	end := 20
	for end > n+1 && buf[end-1] == '0' {
		end--
	}
	return string(buf[n:end])
}

// countDecimalDigits returns the number of decimal digits in v.
func countDecimalDigits(v uint64) int {
	if v == 0 {
		return 1
	}
	n := 0
	for v > 0 {
		n++
		v /= 10
	}
	return n
}
