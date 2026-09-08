package lbdmod

import (
	lbdsrc "miren.dev/runtime/third_party/lbd"
)

// SourceVersion reports the miren.dev/lbd version this binary carries.
func SourceVersion() string {
	return lbdsrc.Version()
}

// materializeSource writes the embedded module source into dir, which the
// builder container then mounts. The kernel build system writes its object
// files next to the source, so this has to be a real writable directory rather
// than a read-only mount of something we already have.
func materializeSource(dir string) error {
	return materializeEmbed(lbdsrc.FS, "src", dir)
}
