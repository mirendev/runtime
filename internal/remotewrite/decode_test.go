package remotewrite

import (
	"math"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

func TestDecode(t *testing.T) {
	label := func(name, value string) []byte {
		data := protowire.AppendTag(nil, 1, protowire.BytesType)
		data = protowire.AppendString(data, name)
		data = protowire.AppendTag(data, 2, protowire.BytesType)
		return protowire.AppendString(data, value)
	}

	series := protowire.AppendTag(nil, 1, protowire.BytesType)
	series = protowire.AppendBytes(series, label("__name__", "requests_total"))
	series = protowire.AppendTag(series, 1, protowire.BytesType)
	series = protowire.AppendBytes(series, label("service", "web"))
	sample := protowire.AppendTag(nil, 1, protowire.Fixed64Type)
	sample = protowire.AppendFixed64(sample, math.Float64bits(42))
	sample = protowire.AppendTag(sample, 2, protowire.VarintType)
	sample = protowire.AppendVarint(sample, 1234)
	series = protowire.AppendTag(series, 2, protowire.BytesType)
	series = protowire.AppendBytes(series, sample)
	series = protowire.AppendTag(series, 9, protowire.VarintType)
	series = protowire.AppendVarint(series, 1)
	request := protowire.AppendTag(nil, 1, protowire.BytesType)
	request = protowire.AppendBytes(request, series)

	got, err := Decode(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("decoded %d samples, want 1", len(got))
	}
	if got[0].Metric != "requests_total" || got[0].Labels["service"] != "web" || got[0].Value != 42 || got[0].TimestampMS != 1234 {
		t.Fatalf("decoded sample = %#v", got[0])
	}
}
