package cloudrpc_test

import (
	"context"
	"errors"

	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/example"
)

// testMeter is the generated example service, used here only as something real
// to call through the relay.
type testMeter struct {
	temp float32
}

func (m *testMeter) ActorState() any { return &m.temp }

func (m *testMeter) ReadTemperature(ctx context.Context, call *example.MeterReadTemperature) error {
	reading := new(example.Reading)
	reading.SetMeter(call.Args().Name())
	reading.SetTemperature(m.temp)

	call.Results().SetReading(reading)
	return nil
}

func (m *testMeter) GetSetter(ctx context.Context, call *example.MeterGetSetter) error {
	call.Results().SetSetter(m)
	return nil
}

func (m *testMeter) ReconstructFromState(is *rpc.InterfaceState) (*rpc.Interface, error) {
	if is.Interface == "SetTemp" {
		return example.AdaptSetTemp(m), nil
	}
	return nil, nil
}

func (m *testMeter) SetTemp(ctx context.Context, call *example.SetTempSetTemp) error {
	args := call.Args()
	m.temp = float32(args.Temp())
	call.Results().SetTemp(args.Temp())
	return nil
}

// blockingMeter parks in ReadTemperature until its context ends, standing in
// for the long-running work a relayed caller can start: a build, a log stream,
// an exec. It reports both that it started and how it was released, which is
// what lets a test tell "the session tore down" from "the handler was ended
// with it."
type blockingMeter struct {
	temp float32

	entered  chan struct{}
	released chan error
}

func (m *blockingMeter) ActorState() any { return &m.temp }

func (m *blockingMeter) ReadTemperature(ctx context.Context, call *example.MeterReadTemperature) error {
	close(m.entered)
	<-ctx.Done()
	m.released <- ctx.Err()
	return ctx.Err()
}

// Unused by the tests that take this meter, but part of the interface.
func (m *blockingMeter) GetSetter(ctx context.Context, call *example.MeterGetSetter) error {
	return errors.New("not used by this fixture")
}

func (m *blockingMeter) ReconstructFromState(is *rpc.InterfaceState) (*rpc.Interface, error) {
	return nil, nil
}
