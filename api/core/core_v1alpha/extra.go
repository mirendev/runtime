package core_v1alpha

import (
	"strings"

	entity "miren.dev/runtime/pkg/entity"
)

func MD(ea entity.AttrGetter) Metadata {
	var md Metadata
	md.Decode(ea)
	return md
}

// SystemArtifactPrefix marks an image miren pushes to the cluster registry for
// its own use rather than on behalf of an app.
//
// An artifact's entity name is the tag it was pushed under (see the registry's
// putManifest), so the prefix rides in the tag and needs no schema field. It
// exists because artifact GC archives everything no AppVersion references, and
// a system image belongs to no app: without a way to tell it apart from a
// genuinely orphaned artifact, it would be collected within the hour and its
// blobs deleted underneath the nodes still pulling it.
const SystemArtifactPrefix = "miren-system-"

// IsSystemArtifact reports whether an artifact is one miren pushed for itself,
// and so must survive garbage collection even though no AppVersion points at
// it.
func IsSystemArtifact(id entity.Id) bool {
	name := strings.TrimPrefix(string(id), "artifact/")
	return strings.HasPrefix(name, SystemArtifactPrefix)
}
