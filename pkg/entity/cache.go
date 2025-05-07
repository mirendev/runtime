package entity

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"

	"github.com/fxamacker/cbor/v2"
	lru "github.com/hashicorp/golang-lru/v2"
)

type SchemaCache struct {
	store Store

	schemas    *lru.Cache[string, *EncodedDomain]
	kindSchema *lru.Cache[string, *EncodedSchema]
}

func NewSchemaCache(store Store) (*SchemaCache, error) {
	ks, err := lru.New[string, *EncodedSchema](100)
	if err != nil {
		return nil, err
	}

	s, err := lru.New[string, *EncodedDomain](100)
	if err != nil {
		return nil, err
	}

	return &SchemaCache{
		store:      store,
		schemas:    s,
		kindSchema: ks,
	}, nil
}

func (f *SchemaCache) GetSchema(ctx context.Context, schemaId Id) (*EncodedDomain, error) {
	ed, ok := f.schemas.Get(schemaId.String())
	if ok {
		return ed, nil
	}

	schema, err := f.store.GetEntity(ctx, schemaId)
	if err != nil {
		return nil, fmt.Errorf("failed to get schema: %w", err)
	}

	esch, ok := schema.Get(Schema)
	if !ok {
		return nil, fmt.Errorf("missing schema")
	}

	var ned EncodedDomain
	gr, err := gzip.NewReader(bytes.NewReader(esch.Value.Bytes()))
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}

	defer gr.Close()

	err = cbor.NewDecoder(gr).Decode(&ned)
	if err != nil {
		return nil, fmt.Errorf("failed to decode schema: %w", err)
	}

	ed = &ned

	f.schemas.Add(schemaId.String(), ed)

	return ed, nil
}

func (f *SchemaCache) GetKindSchema(ctx context.Context, id Id) (*EncodedSchema, error) {
	es, ok := f.kindSchema.Get(id.String())
	if ok {
		return es, nil
	}

	kind, err := f.store.GetEntity(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get kind: %w", err)
	}

	schemaId, ok := kind.Get(EntitySchema)
	if !ok {
		return nil, fmt.Errorf("missing schema on kind")
	}

	ed, err := f.GetSchema(ctx, schemaId.Value.Id())
	if err != nil {
		return nil, err
	}

	es, ok = ed.Kinds[string(kind.ID)]
	if !ok {
		return nil, fmt.Errorf("missing schema for kind")
	}

	f.kindSchema.Add(id.String(), es)

	return es, nil
}
