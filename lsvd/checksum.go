package lsvd

import (
	"crypto/sha256"

	"github.com/mr-tron/base58"
)

func blkSum(b []byte) string {
	b = b[:BlockSize]

	return rangeSum(b[:BlockSize])
}

func rangeSum(b []byte) string {
	empty := true

	for _, x := range b {
		if x != 0 {
			empty = false
			break
		}
	}

	if empty {
		return "0"
	}

	x := sha256.Sum256(b)
	return base58.Encode(x[:])
}
