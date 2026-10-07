package query

// Aggregation resource limits apply equally to local and remote execution.
const (
	MaxAggregateGroups  = maxAggregateGroups
	MaxAggregateValues  = maxAggregateValues
	MaxAggregateMetrics = maxAggregateMetrics
)

// SourceMetadata exposes the engine-derived field metadata needed to build
// transport capability documents without exposing the engine's internal maps.
type SourceMetadata struct {
	Sampled             bool
	SampleFields        []SampleField
	SampleGroupByFields []string
	SampleNumericFields []string
	GroupByFields       []string
	NumericFields       []string
	DefaultInterval     string
	MinimumInterval     string
}

// Metadata returns aggregation and sampling metadata for a request's source.
func Metadata(request MonitorRequest) (SourceMetadata, error) {
	fields, numericFields, err := aggregateFields(request)
	if _, ok := sampledSources[request.Source]; ok {
		groups, numeric := sampledFields(request.Source)
		if request.Using != nil {
			groups = fields
		}
		return SourceMetadata{
			Sampled:             true,
			SampleFields:        snapshotSampleFields(request.Source),
			SampleGroupByFields: groups,
			SampleNumericFields: numeric,
			GroupByFields:       fields,
			NumericFields:       numericFields,
			DefaultInterval:     DefaultSampleInterval.String(),
			MinimumInterval:     MinSampleInterval.String(),
		}, nil
	}
	return SourceMetadata{GroupByFields: fields, NumericFields: numericFields}, err
}
