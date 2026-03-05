package schubfach

// formatECMA applies the ECMA-262 §6.1.6.1.20 formatting rules (steps 7-10).
//
// digits: significand digit string (k digits)
// n: decimal exponent (number of integer digits in fixed-point view)
func formatECMA(negative bool, digits string, n int) string {
	k := len(digits)

	var buf []byte
	if negative {
		buf = append(buf, '-')
	}

	switch {
	case isIntegerFixed(k, n):
		// ECMA-FMT-004: 1 ≤ n ≤ 21, k ≤ n → integer with trailing zeros
		buf = appendIntegerFixed(buf, digits, k, n)
	case isFractionFixed(n):
		// ECMA-FMT-005: 0 < n ≤ 21, n < k → fixed decimal
		buf = appendFractionFixed(buf, digits, n)
	case isSmallFraction(n):
		// ECMA-FMT-006: -6 < n ≤ 0 → 0.000...digits
		buf = appendSmallFraction(buf, digits, n)
	default:
		// ECMA-FMT-007: exponential notation
		buf = appendExponential(buf, digits, k, n)
	}

	return string(buf)
}

func isIntegerFixed(k, n int) bool {
	return k <= n && n <= 21
}

func isFractionFixed(n int) bool {
	return 0 < n && n <= 21
}

func isSmallFraction(n int) bool {
	return -6 < n && n <= 0
}

func appendIntegerFixed(buf []byte, digits string, k, n int) []byte {
	buf = append(buf, digits...)
	for i := 0; i < n-k; i++ {
		buf = append(buf, '0')
	}
	return buf
}

func appendFractionFixed(buf []byte, digits string, n int) []byte {
	buf = append(buf, digits[:n]...)
	buf = append(buf, '.')
	buf = append(buf, digits[n:]...)
	return buf
}

func appendSmallFraction(buf []byte, digits string, n int) []byte {
	buf = append(buf, '0', '.')
	for i := 0; i < -n; i++ {
		buf = append(buf, '0')
	}
	buf = append(buf, digits...)
	return buf
}

func appendExponential(buf []byte, digits string, k, n int) []byte {
	buf = append(buf, digits[0])
	if k > 1 {
		buf = append(buf, '.')
		buf = append(buf, digits[1:]...)
	}
	buf = append(buf, 'e')
	exp := n - 1
	if exp >= 0 {
		buf = append(buf, '+')
	}
	return appendInt(buf, exp)
}

func appendInt(buf []byte, v int) []byte {
	if v < 0 {
		buf = append(buf, '-')
		v = -v
	}
	if v == 0 {
		return append(buf, '0')
	}
	var tmp [20]byte
	i := len(tmp)
	for v > 0 {
		i--
		tmp[i] = byte('0' + v%10)
		v /= 10
	}
	return append(buf, tmp[i:]...)
}
