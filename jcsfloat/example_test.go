package jcsfloat_test

import (
	"fmt"
	"log"

	"github.com/lattice-substrate/jcs-schubfach/jcsfloat"
)

func ExampleFormatDouble() {
	s, err := jcsfloat.FormatDouble(3.14)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(s)
	// Output: 3.14
}
