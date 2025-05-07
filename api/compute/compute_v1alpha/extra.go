package compute_v1alpha

import (
	entity "miren.dev/runtime/pkg/entity"
)

func Index(kind, node entity.Id) entity.Attr {
	key := Key{
		Kind: kind,
		Node: node,
	}

	return entity.Component(ScheduleKeyId, key.Encode())
}
