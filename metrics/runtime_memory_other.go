//go:build !linux

package metrics

// processResidentBytes has no portable implementation off Linux; the control
// process only runs on Linux in production. On other platforms (e.g. darwin
// dev builds) RSS is simply omitted from the emitted series.
func processResidentBytes() (uint64, bool) {
	return 0, false
}
