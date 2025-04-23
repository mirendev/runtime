package rpc

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/fxamacker/cbor/v2"
	"miren.dev/runtime/pkg/webtransport"
)

type inlineClient struct {
	log     *slog.Logger
	oid     OID
	ctrl    *controlStream
	session *webtransport.Session
}

func (c *inlineClient) Call(ctx context.Context, method string, args any, ret any) error {
	str, err := c.session.OpenStreamSync(ctx)
	if err != nil {
		return err
	}

	defer str.Close()

	enc := cbor.NewEncoder(str)

	enc.Encode(streamRequest{
		Kind:   "call",
		OID:    c.oid,
		Method: method,
	})

	enc.Encode(args)

	dec := cbor.NewDecoder(str)

	var rr refResponse

	for {
		ts := time.Now().Add(1 * time.Second)
		str.SetReadDeadline(ts)

		err = dec.Decode(&rr)
		if err == nil {
			break
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		if to, ok := err.(interface {
			Timeout() bool
		}); ok && to.Timeout() {
			continue
		}

		return err
	}

	switch rr.Status {
	case "error":
		if rr.Error == "EOF" {
			return io.EOF
		}
		return fmt.Errorf("call error: %s", rr.Error)
	case "ok":
		return dec.Decode(ret)
	default:
		if err := ctx.Err(); err != nil {
			return err
		}

		return fmt.Errorf("unknown response status to %s/%s: %s: %w", c.oid, method, rr.Status, err)
	}
}

func (c *inlineClient) derefOID(ctx context.Context, oid OID) error {
	err := c.ctrl.NoReply(streamRequest{
		Kind: "deref",
		OID:  oid,
	}, nil)

	return err
}
