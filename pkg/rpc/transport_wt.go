package rpc

import (
	"context"
	"errors"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/webtransport-go"
)

// wtStream adapts a *webtransport.Stream to the rpcStream interface.
type wtStream struct {
	*webtransport.Stream
}

func (s wtStream) CancelRead(code uint64) {
	s.Stream.CancelRead(webtransport.StreamErrorCode(code))
}

// wtSession adapts a *webtransport.Session to the rpcSession interface, so the
// WebTransport callstream shares handleCallStream with the message transport.
type wtSession struct {
	sess *webtransport.Session
}

func (s *wtSession) OpenStreamSync(ctx context.Context) (rpcStream, error) {
	str, err := s.sess.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}
	return wtStream{str}, nil
}

func (s *wtSession) AcceptStream(ctx context.Context) (rpcStream, error) {
	str, err := s.sess.AcceptStream(ctx)
	if err != nil {
		return nil, err
	}
	return wtStream{str}, nil
}

func (s *wtSession) Close() error {
	return s.sess.CloseWithError(0, "")
}

// isRetryableTransportError reports whether a transport-level error indicates
// the connection was reset in a way the client should transparently retry.
// This is QUIC-specific; other transports never produce it.
func isRetryableTransportError(err error) bool {
	var ae *quic.ApplicationError
	return errors.As(err, &ae)
}
