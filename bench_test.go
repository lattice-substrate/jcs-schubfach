package schubfach_test

import (
	"math"
	"testing"

	schubfach "github.com/lattice-substrate/jcs-schubfach"
)

func BenchmarkFormatDouble(b *testing.B) {
	cases := []struct {
		name string
		val  float64
	}{
		{"integer", 42},
		{"fraction", 3.14159265358979},
		{"small_fraction", 0.000001},
		{"exponential_large", 1e20},
		{"exponential_small", 1e-7},
		{"subnormal", 5e-324},
		{"negative_zero", math.Copysign(0, -1)},
		{"negative", -273.15},
		{"one", 1},
		{"max_safe_integer", 9007199254740991},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := schubfach.FormatDouble(tc.val); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
