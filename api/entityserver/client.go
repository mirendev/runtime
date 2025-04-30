package entityserver

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
)

type Client struct {
	log       *slog.Logger
	eac       *entityserver_v1alpha.EntityAccessClient
	sessionId string
}

func NewClient(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient) *Client {
	return &Client{
		log: log,
		eac: eac,
	}
}

type SchemaEncoder interface {
	ShortKind() string
	Kind() entity.Id
	Encode() []entity.Attr
	Decode(e entity.AttrGetter)
}

type SchemaEncoderWithId interface {
	SchemaEncoder
	EntityId() entity.Id
}

func (c *Client) Get(ctx context.Context, name string, sc SchemaEncoder) error {
	ret, err := c.eac.Get(ctx, sc.ShortKind()+"/"+name)
	if err != nil {
		return err
	}

	sc.Decode(ret.Entity().Entity())
	return nil
}

func (c *Client) GetById(ctx context.Context, id entity.Id, sc SchemaEncoder) error {
	ret, err := c.eac.Get(ctx, id.String())
	if err != nil {
		return err
	}

	sc.Decode(ret.Entity().Entity())
	return nil
}

type createOp struct {
	labels types.Labels
}

type CreateOptions func(o *createOp)

func WithLabels(labels types.Labels) CreateOptions {
	return func(o *createOp) {
		o.labels = labels
	}
}

func (c *Client) Create(ctx context.Context, name string, sc SchemaEncoder, opts ...CreateOptions) (entity.Id, error) {
	var op createOp
	for _, opt := range opts {
		opt(&op)
	}

	var rpcE entityserver_v1alpha.Entity

	rpcE.SetAttrs(entity.Attrs(
		(&core_v1alpha.Metadata{
			Name:   name,
			Labels: op.labels,
		}).Encode,
		sc.Encode,
		entity.Ident, sc.ShortKind()+"/"+name,
	))

	if c.sessionId != "" {
		pr, err := c.eac.PutSession(ctx, &rpcE, c.sessionId)
		if err != nil {
			return "", err
		}

		return entity.Id(pr.Id()), nil
	}

	pr, err := c.eac.Put(ctx, &rpcE)
	if err != nil {
		return "", err
	}

	return entity.Id(pr.Id()), nil
}

func (c *Client) Update(ctx context.Context, sc SchemaEncoderWithId) error {
	id := sc.EntityId()

	if id == "" {
		return fmt.Errorf("entity id is empty")
	}

	var rpcE entityserver_v1alpha.Entity
	rpcE.SetId(string(id))
	rpcE.SetAttrs(sc.Encode())

	if c.sessionId != "" {
		_, err := c.eac.PutSession(ctx, &rpcE, c.sessionId)
		if err != nil {
			return err
		}

		return nil
	}

	_, err := c.eac.Put(ctx, &rpcE)
	if err != nil {
		return err
	}

	return nil
}

func (c *Client) UpdateAttrs(ctx context.Context, id entity.Id, attrs ...any) error {
	var rpcE2 entityserver_v1alpha.Entity
	rpcE2.SetId(string(id))
	rpcE2.SetAttrs(entity.Attrs(attrs...))

	if c.sessionId != "" {
		_, err := c.eac.PutSession(ctx, &rpcE2, c.sessionId)
		if err != nil {
			return err
		}

		return nil
	}

	_, err := c.eac.Put(ctx, &rpcE2)
	if err != nil {
		return err
	}

	return nil
}

type Session struct {
	c  *Client
	id string

	mu sync.Mutex

	cancel context.CancelFunc
}

const defaultTTL = 60

// Grant creates a new lease with the given TTL
func (c *Client) NewSession(ctx context.Context, usage string) (*Session, *Client, error) {
	ttl := int64(defaultTTL)
	ret, err := c.eac.CreateSession(ctx, ttl, usage)
	if err != nil {
		return nil, nil, err
	}

	sc := &Client{
		eac:       c.eac,
		sessionId: ret.Id(),
	}

	ctx, cancel := context.WithCancel(ctx)

	session := &Session{
		c:  c,
		id: ret.Id(),

		cancel: cancel,
	}

	go func() {
		defer c.log.Debug("session closed", "id", session.id)

		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			session.Revoke(ctx)
		}()

		ticker := time.NewTicker((time.Duration(ttl) * time.Second) / 2)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := session.Ping(ctx); err != nil {
					c.log.Error("failed to ping session", "error", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return session, sc, nil
}

func (l *Session) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.cancel != nil {
		l.cancel()
	}

	if l.id == "" {
		return nil
	}

	_, err := l.c.eac.RevokeSession(context.Background(), l.id)
	if err != nil {
		return err
	}

	l.id = ""
	return nil
}

// Revoke revokes the lease
func (l *Session) Revoke(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	_, err := l.c.eac.RevokeSession(ctx, l.id)
	if err != nil {
		return err
	}

	l.id = ""

	return nil
}

// Assert keeps the lease alive
func (l *Session) Ping(ctx context.Context) error {
	_, err := l.c.eac.PingSession(ctx, l.id)
	if err != nil {
		return err
	}

	return nil
}
