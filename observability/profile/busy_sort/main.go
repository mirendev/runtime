package main

import (
	"fmt"
	"math/rand"
	"slices"
)

func main() {
	for {
		var vals []int

		for i := 0; i < 1000000; i++ {
			vals = append(vals, rand.Int())
		}

		slices.Sort(vals)
		fmt.Println("sorted")
	}
}
