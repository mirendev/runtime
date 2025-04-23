package rpc

import (
	"context"
	"fmt"
	"log/slog"

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
	dec.Decode(&rr)

	switch rr.Status {
	case "error":
		return fmt.Errorf("call error: %s", rr.Error)
	case "ok":
		return dec.Decode(ret)
	default:
		return fmt.Errorf("unknown response status: %s", rr.Status)
	}
}

func (c *inlineClient) derefOID(ctx context.Context, oid OID) error {
	c.log.Info("writing deref to inline")

	err := c.ctrl.NoReply(streamRequest{
		Kind: "deref",
		OID:  oid,
	}, nil)
	if err != nil {
		return err
	}
	/*
		str, err := c.session.OpenStreamSync(ctx)
		if err != nil {
			return err
		}

		defer str.Close()

		enc := cbor.NewEncoder(str)

		err = enc.Encode(streamRequest{
			Kind: "deref",
			OID:  oid,
		})
		if err != nil {
			return err
		}
	*/

	c.log.Info("wrote deref to inline")

	return nil
}
