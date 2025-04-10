package compute_v1alpha

import (
	entity "miren.dev/runtime/pkg/entity"
	types "miren.dev/runtime/pkg/entity/types"
)

func Index(kind types.Keyword, node entity.Id) entity.Attr {
	key := Key{
		Kind: kind,
		Node: node,
	}

	return entity.Component(ScheduleKeyId, key.Encode())
}
