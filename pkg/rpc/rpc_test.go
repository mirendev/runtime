package rpc_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/example"
	"miren.dev/runtime/pkg/rpc/stream"
)

type exampleMeter struct {
	temp float32
}

func (m *exampleMeter) ReadTemperature(ctx context.Context, call *example.MeterReadTemperature) error {
	res := call.Results()
	reading := new(example.Reading)

	args := call.Args()

	reading.SetMeter(args.Name())
	reading.SetTemperature(m.temp)

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

	m.temp = float32(args.Temp())
	res.SetTemp(args.Temp())
	return nil
}

type exampleUpdate struct {
	gotIt   bool
	reading *example.Reading

	closed bool
}

func (m *exampleUpdate) Update(ctx context.Context, call *example.UpdateReceiverUpdate) error {
	args := call.Args()

	m.reading = args.Reading()

	m.gotIt = true

	return nil
}

func (m *exampleUpdate) Close() error {
	m.closed = true
	return nil
}

type exampleMU struct {
}

func (m *exampleMU) RegisterUpdates(ctx context.Context, call *example.MeterUpdatesRegisterUpdates) error {
	args := call.Args()

	reader := new(example.Reading)

	reader.SetMeter("test")
	reader.SetTemperature(42)

	ur := args.Recv()
	defer ur.Close()

	_, err := ur.Update(ctx, reader)
	return err
}

type exampleAT struct {
}

func (m *exampleAT) Adjust(ctx context.Context, call *example.AdjustTempAdjust) error {
	args := call.Args()

	setter := args.Setter()

	setter.SetTemp(ctx, 72)

	return nil
}

type exampleEmit struct{}

func (m *exampleEmit) Emit(ctx context.Context, call *example.EmitTempsEmit) error {
	args := call.Args()

	emit := args.Emitter()

	emit.Send(ctx, 42.0)
	emit.Send(ctx, 100.0)

	return nil
}

func TestRPC(t *testing.T) {
	t.Run("serves an interface over rpc", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		s := example.AdaptMeter(&exampleMeter{temp: 42})

		ss, err := rpc.NewState(ctx, rpc.WithSkipVerify)
		r.NoError(err)

		serv := ss.Server()

		serv.ExposeValue("meter", s)

		cs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
		r.NoError(err)

		c, err := cs.Connect(ss.ListenAddr(), "meter")
		r.NoError(err)

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

		ss, err := rpc.NewState(ctx, rpc.WithSkipVerify)
		r.NoError(err)

		serv := ss.Server()

		serv.ExposeValue("meter", s)

		cs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
		r.NoError(err)

		c, err := cs.Connect(ss.ListenAddr(), "meter")
		r.NoError(err)

		mc := &example.MeterUpdatesClient{Client: c}

		var up exampleUpdate

		_, err = mc.RegisterUpdates(context.Background(), &up)
		r.NoError(err)

		r.True(up.gotIt)

		r.Equal("test", up.reading.Meter())
		r.Equal(float32(42), up.reading.Temperature())

		r.True(up.closed)
	})

	t.Run("a capability can be passed to a 3rd party", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		var em exampleMeter
		em.temp = 42

		s := example.AdaptMeter(&em)

		ss, err := rpc.NewState(ctx, rpc.WithSkipVerify)
		r.NoError(err)

		serv := ss.Server()

		serv.ExposeValue("meter", s)

		cs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
		r.NoError(err)

		c, err := cs.Connect(ss.ListenAddr(), "meter")
		r.NoError(err)

		mc := &example.MeterClient{Client: c}

		res, err := mc.ReadTemperature(context.Background(), "test")
		r.NoError(err)

		r.Equal("test", res.Reading().Meter())
		r.Equal(float32(42), res.Reading().Temperature())

		res2, err := mc.GetSetter(context.Background(), "test")
		r.NoError(err)

		s2, err := rpc.NewState(ctx, rpc.WithSkipVerify)
		r.NoError(err)

		s2.Server().ExposeValue("adjust", example.AdaptAdjustTemp(&exampleAT{}))

		c2, err := s2.Connect(s2.ListenAddr(), "adjust")
		r.NoError(err)

		ac := &example.AdjustTempClient{Client: c2}

		_, err = ac.Adjust(context.Background(), res2.Setter().Export())

		r.Equal(float32(72), em.temp)
	})

	t.Run("can deal with a stream", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		s := example.AdaptEmitTemps(&exampleEmit{})

		ss, err := rpc.NewState(ctx, rpc.WithSkipVerify)
		r.NoError(err)

		serv := ss.Server()

		serv.ExposeValue("meter", s)

		cs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
		r.NoError(err)

		c, err := cs.Connect(ss.ListenAddr(), "meter")
		r.NoError(err)

		mc := &example.EmitTempsClient{Client: c}

		var vals []float32

		recv := stream.StreamRecv(func(val float32) error {
			vals = append(vals, val)
			return nil
		})

		_, err = mc.Emit(ctx, recv)
		r.NoError(err)

		time.Sleep(time.Second)

		r.Equal([]float32{42, 100}, vals)
	})
}

func BenchmarkRPC(b *testing.B) {
	r := require.New(b)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := example.AdaptMeter(&exampleMeter{temp: 42})

	ss, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	r.NoError(err)

	serv := ss.Server()

	serv.ExposeValue("meter", s)

	cs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	r.NoError(err)

	c, err := cs.Connect(ss.ListenAddr(), "meter")
	r.NoError(err)

	mc := &example.MeterClient{Client: c}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := mc.ReadTemperature(context.Background(), "test")
		r.NoError(err)
	}
}
