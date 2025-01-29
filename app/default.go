package app

import "miren.dev/runtime/defaults"

var DefaultConfiguration = Configuration{}

func init() {
	DefaultConfiguration.SetConcurrency(defaults.Concurrency)
}
