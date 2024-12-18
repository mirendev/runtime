package rpc_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/example"
)

type exampleMeter struct {
}

func (m *exampleMeter) ReadTemperature(ctx context.Context, call *example.MeterReadTemperature) error {
	res := call.Results()
	reading := new(example.Reading)

	args := call.Args()

	reading.SetMeter(args.Name())
	reading.SetTemperature(42)

	res.SetReading(reading)

	return nil
}

func (m *exampleMeter) GetSetter(ctx context.Context, call *example.MeterGetSetter) error {
	res := call.Results()
	res.SetSetter(m)
	return nil
}

func (m *exampleMeter) SetTemp(ctx context.Context, call *example.SetTempSetTemp) error {
	args := call.Args()
	res := call.Results()

	res.SetTemp(args.Temp())
	return nil
}

func TestRPC(t *testing.T) {
	t.Run("serves an interface over rpc", func(t *testing.T) {
		r := require.New(t)

		ctx := context.Background()

		s := example.AdaptMeter(&exampleMeter{})

		serv := rpc.NewServer()

		serv.ExposeValue("meter", s)

		go func() {
			err := serv.Serve("localhost:7873")
			r.NoError(err)
		}()

		cs, err := rpc.NewState("localhost:7873")
		r.NoError(err)

		c := cs.NewClient("meter")

		mc := &example.MeterClient{Client: c}

		res, err := mc.ReadTemperature(context.Background(), "test")
		r.NoError(err)

		r.Equal("test", res.Reading().Meter())
		r.Equal(float32(42), res.Reading().Temperature())

		res2, err := mc.GetSetter(context.Background(), "test")
		r.NoError(err)

		res3, err := res2.Setter().SetTemp(ctx, 100)
		r.NoError(err)

		r.Equal(int32(100), res3.Temp())

	})
}
