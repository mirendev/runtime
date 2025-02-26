package qmp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/mitchellh/mapstructure"
)

type Conn struct {
	log  *slog.Logger
	conn io.ReadWriter

	dec *json.Decoder
	enc *json.Encoder

	greeting *GreetingPkt

	events chan *Packet

	execMu     sync.Mutex
	mu         sync.Mutex
	closed     bool
	execWaiter chan *Packet
}

func Open(log *slog.Logger, c io.ReadWriter, events chan *Packet) (*Conn, error) {
	conn := &Conn{
		log:    log,
		conn:   c,
		dec:    json.NewDecoder(c),
		enc:    json.NewEncoder(c),
		events: events,
	}

	conn.dec.UseNumber()

	return conn, nil
}

type GreetingPkt struct {
	Version struct {
		QEMU struct {
			Micro int `json:"micro"`
			Minor int `json:"minor"`
			Major int `json:"major"`
		} `json:"qemu"`
		Package string `json:"package"`
	} `json:"version"`
	Capabilities []string `json:"capabilities"`
}

type ErrorPkt struct {
	Class       string `json:"class"`
	Description string `json:"desc"`
}

type TimestampPkt struct {
	Seconds      int64 `json:"seconds"`
	Microseconds int64 `json:"microseconds"`
}

type Packet struct {
	Greeting *GreetingPkt `json:"QMP"`
	Error    *ErrorPkt    `json:"error"`
	Return   any          `json:"return"`

	Timestamp *TimestampPkt  `json:"timestamp"`
	Event     *string        `json:"event"`
	EventData map[string]any `json:"data"`
}

type ExecutePkt struct {
	Command   string         `json:"execute"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

func (c *Conn) decode(ctx context.Context) error {
	var pkt Packet

	err := c.dec.Decode(&pkt)
	if err != nil {
		return err
	}

	if pkt.Greeting != nil {
		c.greeting = pkt.Greeting
		return nil
	}

	if pkt.Error != nil || pkt.Return != nil {
		c.mu.Lock()
		ch := c.execWaiter
		c.mu.Unlock()

		if ch != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case ch <- &pkt:
				//ok
			}
		}
	}

	if pkt.Event != nil && c.events != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case c.events <- &pkt:
			// ok
		}
	}

	return nil
}

func (c *Conn) Watch(ctx context.Context) error {
	err := c.decode(ctx)
	if err != nil {
		return err
	}

	if c.greeting == nil {
		return fmt.Errorf("missing initial greeting")
	}

	err = c.enc.Encode(ExecutePkt{
		Command: "qmp_capabilities",
	})
	if err != nil {
		return err
	}

	c.log.Info("qmp negotiated")

	go func() {
		for {
			select {
			case <-ctx.Done():
			default:
				// so that we don't wait
			}

			err := c.decode(ctx)
			if err != nil {
				c.mu.Lock()
				c.closed = true
				c.mu.Unlock()
				return
			}
		}
	}()

	return nil
}

var ErrTimeout = errors.New("timeout performing guest sync")

func GuestSync(ctx context.Context, log *slog.Logger, c net.Conn, events chan *Packet) (*Conn, error) {
	log.Info("attempting to sync with guest")

	enc := json.NewEncoder(c)

	err := enc.Encode(ExecutePkt{
		Command: "guest-ping",
	})

	if err != nil {
		return nil, err
	}

	data := make([]byte, 1024)

	type readRes struct {
		x   int
		err error
	}

	ret := make(chan readRes, 1)

	go func() {
		sz, err := c.Read(data)
		log.Info("read some data from guest", "sz", sz, "data", string(data[:sz]))
		// ok, some data! presume it's going to be the whole thing and keep reading
		if err == nil && sz > 0 {
			log.Info("trying again?", "last", data[sz-1] != '\n')
			for data[sz-1] != '\n' {
				n, err := c.Read(data[sz:])
				sz += n
				if err != nil {
					ret <- readRes{sz, err}
				}
			}
		}
		ret <- readRes{sz, err}
	}()

	log.Info("transmitted guest-sync, waiting on response")

	t := time.NewTimer(30 * time.Second)
	defer t.Stop()

	select {
	case <-ret:
		log.Info("guest ping complete")

		conn, err := Open(log, c, events)
		if err != nil {
			return nil, err
		}

		log.Info("performing guest-sync request")
		ts := time.Now().Nanosecond()

		err = conn.WatchAgent(ctx)
		if err != nil {
			return nil, err
		}

		// Wait one second in case there are errant returns to flush
		time.Sleep(time.Second)

		ret, err := conn.Execute(ctx, "guest-sync", map[string]any{
			"id": ts,
		})

		if num, ok := ret.(json.Number); ok {
			i, err := num.Int64()
			if err != nil {
				return nil, err
			}

			if i != int64(ts) {
				return nil, fmt.Errorf("error, unsync'd guest-sync")
			}
		}

		log.Info("performed guest-sync", "id", ts)

		return conn, err
	case <-t.C:
		log.Info("no response to guest-sync, closing connection")
		c.Close()
		return nil, ErrTimeout
	}
}

func (c *Conn) WatchAgent(ctx context.Context) error {
	go func() {
		for {
			select {
			case <-ctx.Done():
			default:
				// so that we don't wait
			}

			err := c.decode(ctx)
			if err != nil {
				c.mu.Lock()
				c.closed = true
				c.mu.Unlock()
				return
			}
		}
	}()

	return nil
}

func (c *Conn) Execute(ctx context.Context, cmd string, args map[string]any) (any, error) {
	c.execMu.Lock()
	defer c.execMu.Unlock()

	ch := make(chan *Packet)

	c.mu.Lock()
	c.execWaiter = ch
	closed := c.closed
	c.mu.Unlock()

	if closed {
		return nil, fmt.Errorf("connection closed")
	}

	defer func() {
		c.mu.Lock()
		c.execWaiter = nil
		c.mu.Unlock()
	}()

	err := c.enc.Encode(&ExecutePkt{
		Command:   cmd,
		Arguments: args,
	})

	if err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case pkt := <-ch:
		if pkt.Error != nil {
			return nil, fmt.Errorf("remote error: %s (%s)", pkt.Error.Description, pkt.Error.Class)
		}

		return pkt.Return, nil
	}
}

func (c *Conn) ExecuteNR(ctx context.Context, cmd string, args map[string]any) error {
	c.execMu.Lock()
	defer c.execMu.Unlock()

	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()

	if closed {
		return fmt.Errorf("connection closed")
	}

	return c.enc.Encode(&ExecutePkt{
		Command:   cmd,
		Arguments: args,
	})
}

type VNCInfo struct {
	Enabled bool           `mapstructure:"enabled"`
	Auth    string         `mapstructure:"auth"`
	Family  string         `mapstructure:"family"`
	Clients map[string]any `mapstructure:"clients"`
	Service string         `mapstructure:"service"`
	Host    string         `mapstructure:"host"`
}

func (c *Conn) QueryVNC(ctx context.Context) (*VNCInfo, error) {
	data, err := c.Execute(ctx, "query-vnc", nil)
	if err != nil {
		return nil, err
	}

	var info VNCInfo
	err = mapstructure.Decode(data, &info)
	if err != nil {
		return nil, err
	}

	return &info, nil
}
