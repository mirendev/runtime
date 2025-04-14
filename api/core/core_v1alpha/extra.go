package core_v1alpha

import entity "miren.dev/runtime/pkg/entity"

func MD(ea entity.AttrGetter) Metadata {
	var md Metadata
	md.Decode(ea)
	return md
}
