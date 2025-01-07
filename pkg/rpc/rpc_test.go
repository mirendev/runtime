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

type exampleUpdate struct {
	gotIt   bool
	reading *example.Reading
}

func (m *exampleUpdate) Update(ctx context.Context, call *example.UpdateReceiverUpdate) error {
	args := call.Args()

	m.reading = args.Reading()

	m.gotIt = true

	return nil
}

type exampleMU struct {
}

func (m *exampleMU) RegisterUpdates(ctx context.Context, call *example.MeterUpdatesRegisterUpdates) error {
	args := call.Args()

	reader := new(example.Reading)

	reader.SetMeter("test")
	reader.SetTemperature(42)

	_, err := args.Recv().Update(ctx, reader)
	return err
}

func TestRPC(t *testing.T) {
	t.Run("serves an interface over rpc", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		s := example.AdaptMeter(&exampleMeter{})

		ss, err := rpc.NewState(ctx, "localhost:7873")
		r.NoError(err)

		serv := ss.Server()

		serv.ExposeValue("meter", s)

		cs, err := rpc.NewState(ctx, "")
		r.NoError(err)

		c := cs.Connect("localhost:7873", "meter")

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

	t.Run("handles passing a local object to a remote object", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		/*
			shutdown, err := rpc.SetupOTelSDK(ctx)
			r.NoError(err)

			defer shutdown(ctx)
		*/

		s := example.AdaptMeterUpdates(&exampleMU{})

		ss, err := rpc.NewState(ctx, "localhost:7874")
		r.NoError(err)

		serv := ss.Server()

		serv.ExposeValue("meter", s)

		cs, err := rpc.NewState(ctx, "")
		r.NoError(err)

		c := cs.Connect("localhost:7874", "meter")

		mc := &example.MeterUpdatesClient{Client: c}

		var up exampleUpdate

		_, err = mc.RegisterUpdates(context.Background(), &up)
		r.NoError(err)

		r.True(up.gotIt)

		r.Equal("test", up.reading.Meter())
		r.Equal(float32(42), up.reading.Temperature())
	})
}
