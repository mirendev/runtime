//go:build !linux

package query

import (
	"context"
	"errors"
)

func syscallEvents(context.Context, MonitorRequest, func(Event) error) error {
	return errors.New("eBPF monitoring requires Linux")
}

func packetEvents(context.Context, MonitorRequest, func(Event) error) error {
	return errors.New("eBPF monitoring requires Linux")
}

func diskEvents(context.Context, MonitorRequest, func(Event) error) error {
	return errors.New("eBPF monitoring requires Linux")
}

func tracepointEvents(context.Context, MonitorRequest, func(Event) error) error {
	return errors.New("eBPF monitoring requires Linux")
}

func enrichEventNames(emit func(Event) error) func(Event) error { return emit }
