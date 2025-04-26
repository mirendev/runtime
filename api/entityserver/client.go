package entityserver

import (
	"context"
	"fmt"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

type Client struct {
	eac *entityserver_v1alpha.EntityAccessClient
}

func NewClient(eac *entityserver_v1alpha.EntityAccessClient) *Client {
	return &Client{
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

func (c *Client) Create(ctx context.Context, name string, sc SchemaEncoder) (entity.Id, error) {
	var rpcE entityserver_v1alpha.Entity

	rpcE.SetAttrs(entity.Attrs(
		(&core_v1alpha.Metadata{
			Name: name,
		}).Encode,
		sc.Encode,
		entity.Ident, sc.ShortKind()+"/"+name,
	))

	pr, err := c.eac.Put(ctx, &rpcE)
	if err != nil {
		return "", err
	}

	return entity.Id(pr.Id()), nil
}

func (c *Client) CreateNamed(ctx context.Context, md *core_v1alpha.Metadata, name string, attrs ...any) (entity.Id, error) {
	var rpcE entityserver_v1alpha.Entity

	base := entity.Attrs(attrs...)
	base = append(base, entity.Keyword(entity.Ident, name))

	rpcE.SetAttrs(base)

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

	_, err := c.eac.Put(ctx, &rpcE2)
	if err != nil {
		return err
	}

	return nil
}
