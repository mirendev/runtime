//go:build !linux

package ipdiscovery

func linkTypeOf(string) string { return "" }
