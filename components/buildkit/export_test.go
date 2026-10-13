package buildkit

// SetImageForTest points the component at a different daemon image and
// returns a func that restores the default.
func SetImageForTest(ref string) func() {
	prev := buildkitImage
	buildkitImage = ref
	return func() { buildkitImage = prev }
}
