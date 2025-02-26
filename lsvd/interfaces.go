package lsvd

import "context"

type ReadExtenter interface {
	ReadExtent(ctx context.Context, ext Extent) (RangeData, error)
}
