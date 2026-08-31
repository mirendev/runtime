// Package remotewrite decodes the subset of Prometheus Remote Write used by
// runtime integration tests and developer inspection tools.
package remotewrite

import (
	"math"

	"google.golang.org/protobuf/encoding/protowire"
)

type Sample struct {
	Metric      string
	Labels      map[string]string
	Value       float64
	TimestampMS int64
}

func Decode(data []byte) ([]Sample, error) {
	var result []Sample
	for len(data) > 0 {
		number, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, protowire.ParseError(n)
		}
		data = data[n:]
		if number == 1 && wireType == protowire.BytesType {
			series, consumed := protowire.ConsumeBytes(data)
			if consumed < 0 {
				return nil, protowire.ParseError(consumed)
			}
			parsed, err := parseTimeSeries(series)
			if err != nil {
				return nil, err
			}
			result = append(result, parsed...)
			data = data[consumed:]
			continue
		}
		consumed := protowire.ConsumeFieldValue(number, wireType, data)
		if consumed < 0 {
			return nil, protowire.ParseError(consumed)
		}
		data = data[consumed:]
	}
	return result, nil
}

func parseTimeSeries(data []byte) ([]Sample, error) {
	labels := make(map[string]string)
	var samples []Sample
	for len(data) > 0 {
		number, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, protowire.ParseError(n)
		}
		data = data[n:]
		if wireType == protowire.BytesType && (number == 1 || number == 2) {
			value, consumed := protowire.ConsumeBytes(data)
			if consumed < 0 {
				return nil, protowire.ParseError(consumed)
			}
			if number == 1 {
				name, labelValue, err := parseLabel(value)
				if err != nil {
					return nil, err
				}
				labels[name] = labelValue
			} else {
				parsed, err := parseSample(value)
				if err != nil {
					return nil, err
				}
				samples = append(samples, parsed)
			}
			data = data[consumed:]
			continue
		}
		consumed := protowire.ConsumeFieldValue(number, wireType, data)
		if consumed < 0 {
			return nil, protowire.ParseError(consumed)
		}
		data = data[consumed:]
	}
	for i := range samples {
		samples[i].Metric = labels["__name__"]
		samples[i].Labels = labels
	}
	return samples, nil
}

func parseLabel(data []byte) (string, string, error) {
	var name, value string
	for len(data) > 0 {
		number, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return "", "", protowire.ParseError(n)
		}
		data = data[n:]
		if wireType == protowire.BytesType && (number == 1 || number == 2) {
			field, consumed := protowire.ConsumeBytes(data)
			if consumed < 0 {
				return "", "", protowire.ParseError(consumed)
			}
			if number == 1 {
				name = string(field)
			} else {
				value = string(field)
			}
			data = data[consumed:]
			continue
		}
		consumed := protowire.ConsumeFieldValue(number, wireType, data)
		if consumed < 0 {
			return "", "", protowire.ParseError(consumed)
		}
		data = data[consumed:]
	}
	return name, value, nil
}

func parseSample(data []byte) (Sample, error) {
	var result Sample
	for len(data) > 0 {
		number, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return Sample{}, protowire.ParseError(n)
		}
		data = data[n:]
		switch {
		case number == 1 && wireType == protowire.Fixed64Type:
			value, consumed := protowire.ConsumeFixed64(data)
			if consumed < 0 {
				return Sample{}, protowire.ParseError(consumed)
			}
			result.Value = math.Float64frombits(value)
			data = data[consumed:]
		case number == 2 && wireType == protowire.VarintType:
			value, consumed := protowire.ConsumeVarint(data)
			if consumed < 0 {
				return Sample{}, protowire.ParseError(consumed)
			}
			result.TimestampMS = int64(value)
			data = data[consumed:]
		default:
			consumed := protowire.ConsumeFieldValue(number, wireType, data)
			if consumed < 0 {
				return Sample{}, protowire.ParseError(consumed)
			}
			data = data[consumed:]
		}
	}
	return result, nil
}
