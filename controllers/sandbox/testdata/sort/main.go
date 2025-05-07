package main

import (
	"math/rand"
	"slices"
)

func main() {
	var vals []int

	for i := 0; i < 1000000; i++ {
		vals = append(vals, rand.Int())
	}

	go func() {
		for {
			slices.Sort(vals)
		}
	}()

	for {
		slices.Sort(vals)
	}
}
