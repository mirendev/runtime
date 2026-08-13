package cloudrpc_test

import (
	"context"

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
