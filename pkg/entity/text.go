package entity

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/fxamacker/cbor/v2"
	lru "github.com/hashicorp/golang-lru/v2"
	"gopkg.in/yaml.v3"
)

type TextFormatter struct {
	store Store

	schemas    *lru.Cache[string, *EncodedDomain]
	kindSchema *lru.Cache[string, *EncodedSchema]
}

func NewTextFormatter(store Store) (*TextFormatter, error) {
	ks, err := lru.New[string, *EncodedSchema](100)
	if err != nil {
		return nil, err
	}

	s, err := lru.New[string, *EncodedDomain](100)
	if err != nil {
		return nil, err
	}

	return &TextFormatter{
		store:      store,
		schemas:    s,
		kindSchema: ks,
	}, nil
}

func (f *TextFormatter) Parse(ctx context.Context, data []byte) (*Entity, error) {
	var sc SchemaValue
	err := yaml.NewDecoder(bytes.NewReader(data)).Decode(&sc)
	if err != nil {
		return nil, err
	}

	domain, kind, ok := strings.Cut(sc.Kind, "/")
	if !ok {
		return nil, fmt.Errorf("invalid kind format")
	}

	schemaId := Id(domain + "/schema." + sc.Version)

	ed, ok := f.schemas.Get(schemaId.String())
	if !ok {
		schema, err := f.store.GetEntity(ctx, Id(domain+"/schema."+sc.Version))
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
	}

	rkind, ok := ed.ShortKinds[kind]
	if !ok {
		return nil, fmt.Errorf("unknown kind: %s", kind)
	}

	es := ed.Kinds[rkind]
	if es == nil {
		return nil, fmt.Errorf("unknown kind: %s", kind)
	}

	attrs, err := NaturalDecode(sc.Spec, es)
	if err != nil {
		return nil, fmt.Errorf("failed to decode entity: %w", err)
	}

	return &Entity{Attrs: attrs}, nil
}

func (f *TextFormatter) resolveKindSchema(ctx context.Context, id Id) (*EncodedSchema, error) {
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

	ed, ok := f.schemas.Get(schemaId.Value.Id().String())
	if !ok {
		schema, err := f.store.GetEntity(ctx, schemaId.Value.Id())
		if err != nil {
			return nil, fmt.Errorf("failed to get schema: %w", err)
		}

		esch, ok := schema.Get(Schema)
		if !ok {
			return nil, fmt.Errorf("missing schema")
		}

		gr, err := gzip.NewReader(bytes.NewReader(esch.Value.Bytes()))
		if err != nil {
			return nil, fmt.Errorf("failed to create gzip reader: %w", err)
		}

		defer gr.Close()

		var ned EncodedDomain
		err = cbor.NewDecoder(gr).Decode(&ned)
		if err != nil {
			return nil, fmt.Errorf("failed to decode schema: %w", err)
		}

		ed = &ned

		f.schemas.Add(schemaId.Value.Id().String(), ed)
	}

	es, ok = ed.Kinds[string(kind.ID)]
	if !ok {
		return nil, fmt.Errorf("missing schema for kind")
	}

	f.kindSchema.Add(id.String(), es)

	return es, nil
}

func (f *TextFormatter) Format(ctx context.Context, ent *Entity) (string, error) {
	var results []any

	for _, kindid := range ent.GetAll(EntityKind) {
		kind, err := f.store.GetEntity(ctx, kindid.Value.Id())
		if err != nil {
			return "", fmt.Errorf("failed to get kind: %w", err)
		}

		schemaId, ok := kind.Get(EntitySchema)
		if !ok {
			return spew.Sdump(ent), nil
		}

		schema, err := f.store.GetEntity(ctx, schemaId.Value.Id())
		if err != nil {
			return "", fmt.Errorf("failed to get schema: %w", err)
		}

		esch, ok := schema.Get(Schema)
		if !ok {
			return "", fmt.Errorf("missing schema")
		}

		var ed EncodedDomain
		gr, err := gzip.NewReader(bytes.NewReader(esch.Value.Bytes()))
		if err != nil {
			return "", fmt.Errorf("failed to create gzip reader: %w", err)
		}

		defer gr.Close()

		err = cbor.NewDecoder(gr).Decode(&ed)
		if err != nil {
			return "", fmt.Errorf("failed to decode schema: %w", err)
		}

		es, ok := ed.Kinds[string(kind.ID)]
		if !ok {
			continue
		}
		m, err := NaturalEncode(ent, es)
		if err != nil {
			return "", fmt.Errorf("failed to encode entity: %w", err)
		}
		results = append(results, m)
	}

	var n yaml.Node
	err := n.Encode(map[string]any{
		"attrs": ent.Attrs,
	})
	if err != nil {
		return "", fmt.Errorf("failed to encode entity: %w", err)
	}

	switch len(results) {
	case 0:
		// ok
	case 1:
		var n2 yaml.Node
		err = n2.Encode(results[0])
		if err != nil {
			return "", fmt.Errorf("failed to encode entity: %w", err)
		}

		n.Content = append(n2.Content, n.Content...)
	default:
		var n2 yaml.Node
		err = n2.Encode(map[string]any{
			"kinds": results,
		})

		n.Content = append(n2.Content, n.Content...)
	}

	var buf bytes.Buffer

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	err = enc.Encode(&n)
	if err != nil {
		return "", fmt.Errorf("failed to encode entity: %w", err)
	}

	return buf.String(), nil
}
