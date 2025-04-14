package model

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

type TextFormatter struct {
	sc *entity.SchemaCache
}

func NewTextFormatter(sc *entity.SchemaCache) (*TextFormatter, error) {
	return &TextFormatter{
		sc: sc,
	}, nil
}

type ParsedFile struct {
	Format   string
	Entities []*entity.Entity
}

func (f *TextFormatter) Parse(ctx context.Context, data []byte) (*ParsedFile, error) {
	br := bytes.NewReader(data)

	pf := &ParsedFile{
		Format: "yaml",
	}

	dec := yaml.NewDecoder(br)

	for {
		var sc SchemaValue
		err := dec.Decode(&sc)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			return nil, err
		}

		ent, err := f.decode(ctx, &sc)
		if err != nil {
			return nil, err
		}

		pf.Entities = append(pf.Entities, ent)
	}

	return pf, nil
}

func (f *TextFormatter) decode(ctx context.Context, sc *SchemaValue) (*entity.Entity, error) {
	domain, kind, ok := strings.Cut(sc.Kind, "/")
	if !ok {
		return nil, fmt.Errorf("invalid kind format")
	}

	schemaId := entity.Id(domain + "/schema." + sc.Version)

	ed, err := f.sc.GetSchema(ctx, schemaId)
	if err != nil {
		return nil, fmt.Errorf("failed to get schema: %w", err)
	}

	rkind, ok := ed.ShortKinds[kind]
	if !ok {
		return nil, fmt.Errorf("unknown kind: %s", kind)
	}

	es := ed.Kinds[rkind]
	if es == nil {
		return nil, fmt.Errorf("unknown kind: %s", kind)
	}

	ent, err := NaturalDecode(sc.Spec, es)
	if err != nil {
		return nil, fmt.Errorf("failed to decode entity: %w", err)
	}

	mes, err := f.sc.GetKindSchema(ctx, core_v1alpha.KindMetadata)
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata schema: %w", err)
	}

	ment, err := NaturalDecode(sc.Metadata, mes)
	if err != nil {
		return nil, fmt.Errorf("failed to decode metadata: %w", err)
	}

	ent.ID = entity.Id(sc.Id)

	err = ent.Update(ment.Attrs)
	if err != nil {
		return nil, fmt.Errorf("failed to update entity: %w", err)
	}

	if _, ok := ent.Get(entity.Ident); !ok {
		name, ok := sc.Metadata["name"].(string)
		if sc.Metadata != nil && ok {
			err = ent.Update(entity.Attrs(
				entity.Keyword(entity.Ident, kind+"/"+name),
			))
			if err != nil {
				return nil, fmt.Errorf("failed to set ident: %w", err)
			}
		}
	}

	return ent, nil
}

func (f *TextFormatter) Format(ctx context.Context, ent *entity.Entity) (string, error) {
	var (
		results  []*SchemaValue
		metadata map[string]any
	)

	for _, kindid := range ent.GetAll(entity.EntityKind) {
		es, err := f.sc.GetKindSchema(ctx, kindid.Value.Id())
		if err != nil {
			return "", fmt.Errorf("failed to get kind schema: %w", err)
		}

		if kindid.Value.Id() == core_v1alpha.KindMetadata {
			md, err := naturalEncodeMap(ent, es)
			if err != nil {
				return "", fmt.Errorf("failed to encode entity: %w", err)
			}

			metadata = md
		} else {
			m, err := NaturalEncode(ent, es)
			if err != nil {
				return "", fmt.Errorf("failed to encode entity: %w", err)
			}

			results = append(results, m)
		}
	}

	if metadata != nil && len(results) > 0 {
		results[0].Metadata = metadata
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
