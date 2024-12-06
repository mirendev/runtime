package profile

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"

	_ "github.com/cilium/ebpf"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/davecgh/go-spew/spew"
	gprofile "github.com/google/pprof/profile"
	"miren.dev/runtime/pkg/perf"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -type stack_key -type arguments perf ebpf/perf_event.c -- -I ebpf/include

const samplesPerSecond = 20

const (
	ipOffset       = 0
	pidOffset      = 8
	cpuOffset      = 12
	tgidOffset     = 16
	nameOffset     = 20
	nameSize       = 16
	ustackSzOffset = 36
	ustackOffset   = 40
	kstackSzOffset = ustackOffset + 128
	kstackOffset   = kstackSzOffset + 4
)

type Stack struct {
	data []byte
}

func (s *Stack) User() []uint64 {
	ustackSize := binary.LittleEndian.Uint32(s.data[ustackSzOffset:])
	ustack := make([]uint64, ustackSize/8)
	for i := 0; i < int(ustackSize); i += 8 {
		ustack[i/8] = binary.LittleEndian.Uint64(s.data[ustackOffset+i:])
	}
	return ustack
}

type Profiler struct {
	attr *perf.Attr
	pid  int

	events []*perf.Event

	stacks []Stack

	ct CallTree

	gpprof gprofile.Profile

	symzer *Symbolizer
}

func NewProfiler(pid int, symzer *Symbolizer) (*Profiler, error) {
	var attr perf.Attr
	attr.SetSampleFreq(samplesPerSecond)
	//attr.SetSamplePeriod(10000000)
	attr.SampleFormat.UserStack = true

	err := perf.CPUClock.Configure(&attr)
	if err != nil {
		return nil, err
	}

	return &Profiler{
		attr:   &attr,
		pid:    pid,
		symzer: symzer,
	}, nil
}

func (p *Profiler) Start(ctx context.Context) error {
	objs := perfObjects{}
	if err := loadPerfObjects(&objs, nil); err != nil {
		return err
	}

	cpus, err := getCPUs()
	if err != nil {
		return err
	}

	var events []*perf.Event

	spew.Dump(cpus)

	for _, cpu := range cpus {
		fmt.Printf("cpu: %d\n", cpu)
		ev, err := perf.Open(p.attr, p.pid, int(cpu), nil)
		if err != nil {
			for _, ev := range events {
				ev.Close()
			}

			return err
		}

		events = append(events, ev)
	}

	fmt.Printf("setting bpf\n")
	for _, ev := range events {
		err := ev.SetBPF(uint32(objs.Profile.FD()))
		if err != nil {
			for _, ev := range events {
				ev.Close()
			}

			return err
		}
	}

	fmt.Println(objs.Events.Type())

	r, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		return err
	}

	go p.readEvents(ctx, r)

	p.events = events

	return nil
}

func (p *Profiler) readEvents(ctx context.Context, r *ringbuf.Reader) {
	go func() {
		<-ctx.Done()
		r.Close()
	}()

	for {
		rec, err := r.Read()
		if err != nil {
			fmt.Printf("error reading: %v\n", err)
			return
		}

		fmt.Println("rec")

		stk := Stack{data: rec.RawSample}

		p.stacks = append(p.stacks, stk)

		p.ct.IngestStack(stk.User(), p.symzer)
	}
}

func (p *Profiler) CallTree() *CallTree {
	return &p.ct
}

func (p *Profiler) Stop() error {
	return nil
}

func (p *Profiler) Stacks() ([]Stack, error) {
	return p.stacks, nil
}

const cpuOnline = "/sys/devices/system/cpu/online"

// Get returns a slice with the online CPUs, for example `[0, 2, 3]`
func getCPUs() ([]uint, error) {
	buf, err := os.ReadFile(cpuOnline)
	if err != nil {
		return nil, err
	}
	return ReadCPURange(string(buf))
}

// loosely based on https://github.com/iovisor/bcc/blob/v0.3.0/src/python/bcc/utils.py#L15
func ReadCPURange(cpuRangeStr string) ([]uint, error) {
	var cpus []uint
	cpuRangeStr = strings.Trim(cpuRangeStr, "\n ")
	for _, cpuRange := range strings.Split(cpuRangeStr, ",") {
		rangeOp := strings.SplitN(cpuRange, "-", 2)
		first, err := strconv.ParseUint(rangeOp[0], 10, 32)
		if err != nil {
			return nil, err
		}
		if len(rangeOp) == 1 {
			cpus = append(cpus, uint(first))
			continue
		}
		last, err := strconv.ParseUint(rangeOp[1], 10, 32)
		if err != nil {
			return nil, err
		}
		for n := first; n <= last; n++ {
			cpus = append(cpus, uint(n))
		}
	}
	return cpus, nil
}
