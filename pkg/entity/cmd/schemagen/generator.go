package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"strings"

	j "github.com/dave/jennifer/jen"
	"github.com/fxamacker/cbor/v2"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/mapx"
)

const (
	top  = "miren.dev/runtime/pkg/entity"
	topt = "miren.dev/runtime/pkg/entity/types"
	sch  = "miren.dev/runtime/pkg/entity/schema"
)

type schemaFile struct {
	Domain     string                 `yaml:"domain"`
	Version    string                 `yaml:"version"`
	Major      string                 `yaml:"kind-major"`
	Enums      map[string]schemaEnum  `yaml:"enums"`
	Components map[string]schemaAttrs `yaml:"components"`
	Kinds      map[string]schemaAttrs `yaml:"kinds"`
}

type schemaEnum struct {
	Doc    string   `yaml:"doc,omitempty"`
	Values []string `yaml:"values"`
}

type schemaAttrs map[string]*schemaAttr

type schemaAttr struct {
	Type     string   `yaml:"type"`
	Doc      string   `yaml:"doc"`
	Attr     string   `yaml:"attr,omitempty"`     // for attribute name
	Many     bool     `yaml:"many,omitempty"`     // for repeated attributes
	Required bool     `yaml:"required,omitempty"` // for required attributes
	Choices  []string `yaml:"choices,omitempty"`  // for enum attributes
	Enum     string   `yaml:"enum,omitempty"`     // named enum type
	Encoding string   `yaml:"encoding,omitempty"` // physical enum representation
	Indexed  bool     `yaml:"indexed,omitempty"`  // for indexed attributes
	Session  bool     `yaml:"session,omitempty"`  // for session attributes
	BindTo   string   `yaml:"bind_to,omitempty"`  // for binding to other attributes
	Tags     []string `yaml:"tags,omitempty"`     // for attribute tags

	Attrs map[string]*schemaAttr `yaml:"attrs,omitempty"` // for nested attributes
}

type emptyCheck struct {
	notEmpty j.Code // true when field has a value (used in multi-field if-checks)
	isEmpty  j.Code // true when field is empty (used in single-field return)
}

type gen struct {
	kind        string
	name        string
	prefix      string
	local       string
	isComponent bool // true if generating a standalone component (not an entity kind)

	usedAttrs        map[string]struct{}
	componentSchemas map[string]*entity.EncodedSchema

	ec *entity.EncodedSchema

	structName string
	sf         *schemaFile
	f          *j.File

	idents []j.Code

	ensureAttrs []j.Code // for ensuring attributes are declared

	decl   []j.Code
	fields []j.Code

	decodeouter []j.Code
	decoders    []j.Code
	encoders    []j.Code
	emptyChecks []emptyCheck // per-field emptiness checks

	subgen []*gen // for nested attributes
}

func GenerateSchema(sf *schemaFile, pkg string) (string, error) {
	if err := validateEnums(sf); err != nil {
		return "", err
	}

	var ed entity.EncodedDomain
	ed.Name = sf.Domain
	ed.Version = sf.Version
	ed.Kinds = make(map[string]*entity.EncodedSchema)
	ed.ShortKinds = make(map[string]string)

	jf := j.NewFile(pkg)
	generateEnums(jf, sf)

	var (
		kinds   []string
		structs []string
	)

	usedAttrs := map[string]struct{}{}
	componentSchemas := make(map[string]*entity.EncodedSchema)

	// Generate standalone components first (they may be referenced by kinds)
	for compName, attrs := range mapx.StableOrder(sf.Components) {
		var g gen
		g.usedAttrs = usedAttrs
		g.isComponent = true
		g.name = compName
		g.prefix = sf.Domain + ".component." + compName
		g.local = toCamal(compName)
		g.sf = sf
		g.f = jf
		g.ec = &entity.EncodedSchema{
			Domain:  sf.Domain,
			Name:    sf.Domain + "/component." + compName,
			Version: sf.Version,
		}

		for name, attr := range mapx.StableOrder(attrs) {
			if attr.Attr == "" {
				attr.Attr = "component." + compName + "." + name
			}

			// Use full attribute ID for duplicate checking (includes domain)
			fullAttrId := sf.Domain + "/" + attr.Attr
			if _, ok := usedAttrs[fullAttrId]; ok {
				return "", fmt.Errorf("duplicate attribute name: %s", fullAttrId)
			}

			g.usedAttrs[fullAttrId] = struct{}{}

			g.attr(name, attr)
		}

		g.generate()
		structs = append(structs, g.structName)
		componentSchemas[compName] = g.ec
		g.f.Line()
	}

	for kind, attrs := range mapx.StableOrder(sf.Kinds) {
		kinds = append(kinds, kind)

		var g gen
		g.usedAttrs = usedAttrs
		g.componentSchemas = componentSchemas
		g.kind = kind
		g.name = kind
		g.prefix = sf.Domain + "." + kind
		g.local = toCamal(kind)
		g.sf = sf
		g.f = jf
		g.ec = &entity.EncodedSchema{
			Domain:  sf.Domain,
			Name:    sf.Domain + "/" + kind,
			Version: sf.Version,
		}

		longKind := sf.Domain + "/kind." + kind

		ed.Kinds[longKind] = g.ec
		ed.ShortKinds[kind] = longKind

		g.fields = append(g.fields,
			j.Id("ID").Qual(top, "Id").Tag(map[string]string{
				"json": "id",
			}),
		)

		g.decoders = append(g.decoders,
			j.Id("o").Dot("ID").Op("=").Qual(top, "MustGet").Call(j.Id("e"), j.Qual(top, "DBId")).Dot("Value").Dot("Id").Call())

		for name, attr := range mapx.StableOrder(attrs) {
			if attr.Attr == "" {
				attr.Attr = kind + "." + name
			}

			// Use full attribute ID for duplicate checking (includes domain)
			fullAttrId := sf.Domain + "/" + attr.Attr
			if _, ok := usedAttrs[fullAttrId]; ok {
				return "", fmt.Errorf("duplicate attribute name: %s", fullAttrId)
			}

			g.usedAttrs[fullAttrId] = struct{}{}

			g.attr(name, attr)
		}

		g.generate()

		structs = append(structs, g.structName)

		g.f.Line()
	}

	jf.Var().DefsFunc(func(b *j.Group) {
		for _, kind := range kinds {
			b.Id("Kind"+toCamal(kind)).Op("=").Qual(top, "Id").Call(j.Lit(sf.Domain + "/kind." + kind))
		}

		b.Id("Schema").Op("=").Qual(top, "Id").Call(j.Lit(sf.Domain + "/schema." + sf.Version))
	})

	jf.Func().Id("init").Params().BlockFunc(func(b *j.Group) {
		b.Add(j.Qual(sch, "Register").Call(
			j.Lit(sf.Domain),
			j.Lit(sf.Version),
			j.Func().Params(j.Id("sb").Op("*").Qual(sch, "SchemaBuilder")).
				BlockFunc(func(b *j.Group) {
					for _, sn := range structs {
						b.Parens(j.Op("&").Id(sn).Values()).Dot("InitSchema").Call(j.Id("sb"))
					}
				}),
		))

		// Using Core Deterministic Encoding options for CBOR makes sure this field doesn't generate unruly diffs
		em, err := cbor.CoreDetEncOptions().EncMode()
		if err != nil {
			panic(fmt.Errorf("failed to make cbor encmode: %w", err))
		}
		data, err := em.Marshal(ed)
		if err != nil {
			panic(fmt.Errorf("failed to marshal encoded domain: %w", err))
		}

		compressed, err := compressData(data)
		if err != nil {
			panic(fmt.Errorf("failed to compress encoded domain: %w", err))
		}

		b.Qual(sch, "RegisterEncodedSchema").Call(
			j.Lit(sf.Domain),
			j.Lit(sf.Version),
			j.Index().Byte().Call(j.Lit(string(compressed))),
		)
	})

	var buf bytes.Buffer
	err := jf.Render(&buf)
	if err != nil {
		return "", fmt.Errorf("failed to render generated code: %w", err)
	}

	return buf.String(), nil
}

// compressData compresses the input data using gzip and returns the compressed byte slice.
// Returns an error if any of the gzip operations fail.
func compressData(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)

	if _, err := zw.Write(data); err != nil {
		zw.Close() // Attempt to close even on error
		return nil, fmt.Errorf("failed to write data to gzip writer: %w", err)
	}

	if err := zw.Flush(); err != nil {
		zw.Close() // Attempt to close even on error
		return nil, fmt.Errorf("failed to flush gzip writer: %w", err)
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("failed to close gzip writer: %w", err)
	}

	return buf.Bytes(), nil
}

func toCamal(s string) string {
	var b bytes.Buffer

	upper := true

	for _, c := range s {
		if c == '_' {
			upper = true
			continue
		}

		if upper {
			if c >= 'a' && c <= 'z' {
				b.WriteRune(c - 32)
			} else {
				b.WriteRune(c)
			}
			upper = false
		} else {
			b.WriteRune(c)
		}
	}

	return b.String()
}

const (
	enumEncodingRef     = "ref"
	enumEncodingString  = "string"
	enumEncodingKeyword = "keyword"
)

func validateEnums(sf *schemaFile) error {
	typeNames := make(map[string]string, len(sf.Enums))
	generatedStructs := make(map[string]string, len(sf.Components)+len(sf.Kinds))
	for name := range sf.Components {
		generatedStructs[toCamal(name)] = "component " + name
	}
	for name := range sf.Kinds {
		generatedStructs[toCamal(name)] = "kind " + name
	}

	for name, enum := range sf.Enums {
		if name == "" {
			return fmt.Errorf("enum name must not be empty")
		}

		typeName := toCamal(name)
		if previous, ok := typeNames[typeName]; ok {
			return fmt.Errorf("enum names %q and %q both generate Go type %s", previous, name, typeName)
		}
		if generated, ok := generatedStructs[typeName]; ok {
			return fmt.Errorf("enum %q and %s both generate Go type %s", name, generated, typeName)
		}
		typeNames[typeName] = name

		if len(enum.Values) == 0 {
			return fmt.Errorf("enum %q must declare at least one value", name)
		}

		seen := make(map[string]struct{}, len(enum.Values))
		for _, value := range enum.Values {
			if value == "" {
				return fmt.Errorf("enum %q contains an empty value", name)
			}
			if _, ok := seen[value]; ok {
				return fmt.Errorf("enum %q contains duplicate value %q", name, value)
			}
			seen[value] = struct{}{}
		}
	}

	var validateAttrs func(string, schemaAttrs) error
	validateAttrs = func(path string, attrs schemaAttrs) error {
		for name, attr := range attrs {
			fieldPath := path + "." + name
			if attr.Type == "enum" {
				if attr.Many {
					return fmt.Errorf("enum field %s does not support many", fieldPath)
				}
				if len(attr.Choices) > 0 {
					return fmt.Errorf("enum field %s must use a named enum instead of choices", fieldPath)
				}
				if _, ok := sf.Enums[attr.Enum]; !ok {
					return fmt.Errorf("enum field %s references unknown enum %q", fieldPath, attr.Enum)
				}
				switch attr.Encoding {
				case enumEncodingRef, enumEncodingString:
				case enumEncodingKeyword:
					for _, value := range sf.Enums[attr.Enum].Values {
						keyword := enumID(sf, attr.Enum) + "." + value
						if !entity.ValidKeyword(keyword) {
							return fmt.Errorf("enum %q value %q produces invalid keyword %q for field %s", attr.Enum, value, keyword, fieldPath)
						}
					}
				default:
					return fmt.Errorf("enum field %s has invalid encoding %q", fieldPath, attr.Encoding)
				}
			}

			if len(attr.Attrs) > 0 {
				if err := validateAttrs(fieldPath, attr.Attrs); err != nil {
					return err
				}
			}
		}
		return nil
	}

	for name, attrs := range sf.Components {
		if err := validateAttrs("component."+name, attrs); err != nil {
			return err
		}
	}
	for name, attrs := range sf.Kinds {
		if err := validateAttrs(name, attrs); err != nil {
			return err
		}

	}

	return nil
}

func generateEnums(jf *j.File, sf *schemaFile) {
	for name, enum := range mapx.StableOrder(sf.Enums) {
		typeName := toCamal(name)
		jf.Type().Id(typeName).String()
		jf.Const().DefsFunc(func(group *j.Group) {
			for _, value := range enum.Values {
				group.Id(typeName + toCamal(value)).Id(typeName).Op("=").Lit(value)
			}
		})
		jf.Line()
	}
}

func enumID(sf *schemaFile, name string) string {
	return sf.Domain + "/enum." + name
}

func enumConstant(enumName, value string) string {
	return toCamal(enumName) + toCamal(value)
}

func enumStringMaps(mapName, enumType, enumName string, values []string) []j.Code {
	from := j.Var().Id(mapName + "FromString").Op("=").Map(j.String()).Id(enumType).ValuesFunc(func(group *j.Group) {
		for _, value := range values {
			group.Lit(value).Op(":").Id(enumConstant(enumName, value))
		}
	})
	to := j.Var().Id(mapName + "ToString").Op("=").Map(j.Id(enumType)).String().ValuesFunc(func(group *j.Group) {
		for _, value := range values {
			group.Id(enumConstant(enumName, value)).Op(":").Lit(value)
		}
	})
	return []j.Code{from, to}
}

func enumKeywordMaps(sf *schemaFile, mapName, enumType, enumName string, values []string) []j.Code {
	from := j.Var().Id(mapName + "FromKeyword").Op("=").Map(j.Qual(topt, "Keyword")).Id(enumType).ValuesFunc(func(group *j.Group) {
		for _, value := range values {
			keyword := enumID(sf, enumName) + "." + value
			group.Qual(top, "MustKeyword").Call(j.Lit(keyword)).Op(":").Id(enumConstant(enumName, value))
		}
	})
	to := j.Var().Id(mapName+"ToKeyword").Op("=").Map(j.Id(enumType)).Qual(topt, "Keyword").ValuesFunc(func(group *j.Group) {
		for _, value := range values {
			keyword := enumID(sf, enumName) + "." + value
			group.Id(enumConstant(enumName, value)).Op(":").Qual(top, "MustKeyword").Call(j.Lit(keyword))
		}
	})
	return []j.Code{from, to}
}

func enumDecoder(g *gen, fieldName, mapName, kindName, accessor string) j.Code {
	return j.If(
		j.List(j.Id("a"), j.Id("ok")).Op(":=").Id("e").Dot("Get").Call(g.Ident(fieldName)),
		j.Id("ok").Op("&&").Id("a").Dot("Value").Dot("Kind").Call().Op("==").Qual(top, kindName),
	).Block(
		j.Id("o").Dot(fieldName).Op("=").Id(mapName).Index(j.Id("a").Dot("Value").Dot(accessor).Call()),
	)
}

func enumEncoder(g *gen, fieldName, mapName, constructor string) j.Code {
	return j.If(
		j.List(j.Id("a"), j.Id("ok")).Op(":=").Id(mapName).Index(j.Id("o").Dot(fieldName)), j.Id("ok"),
	).Block(
		j.Id("attrs").Op("=").Append(j.Id("attrs"), j.Qual(top, constructor).Call(g.Ident(fieldName), j.Id("a"))),
	)
}

func (g *gen) Ident(name string) j.Code {
	return j.Id(g.local + name + "Id")
}

func (g *gen) NSd(name string) j.Code {
	return j.Id(g.local + name)
}

func (g *gen) attr(name string, attr *schemaAttr) {
	fname := toCamal(name)

	eid := g.sf.Domain + "/" + attr.Attr

	if attr.BindTo != "" {
		g.idents = append(g.idents, j.Id(g.local+fname+"Id").Op("=").Qual(top, "Id").Call(j.Lit(attr.BindTo)))
	} else {
		g.idents = append(g.idents, j.Id(g.local+fname+"Id").Op("=").Qual(top, "Id").Call(j.Lit(eid)))
	}

	tn := name
	if !attr.Required {
		tn = tn + ",omitempty"
	} else {
		g.ensureAttrs = append(g.ensureAttrs, j.Id(g.local+fname+"Id"))
	}

	jsonTag := tn
	if !attr.Required && !attr.Many &&
		(attr.Type == "time" || attr.Type == "component" || attr.Type == "label" ||
			(attr.Type != "" && g.sf.Components[attr.Type] != nil)) {
		// encoding/json's omitempty option never omits value structs. Keep
		// the generated tag honest without changing the existing output.
		jsonTag = name
	}

	tag := map[string]string{
		"json": jsonTag,
		"cbor": tn,
	}

	simpleDecoder := func(kind, method string) {
		if attr.Many {
			d :=
				j.For(j.List(j.Op("_"), j.Id("a")).Op(":=").Range().Id("e").Dot("GetAll").Call(g.Ident(fname))).Block(
					j.If(
						j.Id("a").Dot("Value").Dot("Kind").Call().Op("==").Qual(top, kind),
					).Block(
						j.Id("o").Dot(fname).Op("=").Append(j.Id("o").Dot(fname), j.Id("a").Dot("Value").Dot(method).Call()),
					),
				)
			g.decoders = append(g.decoders, d)
		} else {
			d := j.If(
				j.List(j.Id("a"), j.Id("ok")).Op(":=").Id("e").Dot("Get").Call(g.Ident(fname)),
				j.Id("ok").Op("&&").Id("a").Dot("Value").Dot("Kind").Call().Op("==").Qual(top, kind),
			).Block(
				j.Id("o").Dot(fname).Op("=").Id("a").Dot("Value").Dot(method).Call(),
			)
			g.decoders = append(g.decoders, d)
		}
	}

	simpleEncoder := func(method string) {
		if attr.Many {
			g.encoders = append(g.encoders,
				j.For(j.List(j.Op("_"), j.Id("v")).Op(":=").Range().Id("o").Dot(fname)).Block(
					j.Id("attrs").Op("=").Append(j.Id("attrs"), j.Qual(top, method).Call(g.Ident(fname), j.Id("v"))),
				),
			)
			g.emptyChecks = append(g.emptyChecks, emptyCheck{
				notEmpty: j.Len(j.Id("o").Dot(fname)).Op("!=").Lit(0),
				isEmpty:  j.Len(j.Id("o").Dot(fname)).Op("==").Lit(0),
			})
		} else {
			// Special handling for required fields and bool type to always encode, even zero values
			// Required int/duration fields need to encode 0 (scale-to-zero, zero duration, etc.)
			// Bool fields always encode false values
			if attr.Required && (attr.Type == "int" || attr.Type == "duration") || attr.Type == "bool" {
				g.encoders = append(g.encoders,
					j.Id("attrs").Op("=").Append(j.Id("attrs"), j.Qual(top, method).Call(g.Ident(fname), j.Id("o").Dot(fname))),
				)
			} else {
				g.encoders = append(g.encoders,
					j.If(j.Op("!").Qual(top, "Empty").Call(j.Id("o").Dot(fname))).Block(
						j.Id("attrs").Op("=").Append(j.Id("attrs"), j.Qual(top, method).Call(g.Ident(fname), j.Id("o").Dot(fname))),
					),
				)
			}
			// All field types (including bool) should be considered in Empty() check
			g.emptyChecks = append(g.emptyChecks, emptyCheck{
				notEmpty: j.Op("!").Qual(top, "Empty").Call(j.Id("o").Dot(fname)),
				isEmpty:  j.Qual(top, "Empty").Call(j.Id("o").Dot(fname)),
			})
		}
	}

	simpleDecl := func(method string) {
		var call []j.Code
		call = append(call, j.Lit(name), j.Lit(eid))

		if attr.Doc != "" {
			call = append(call, j.Qual(sch, "Doc").Call(j.Lit(attr.Doc)))
		}

		if attr.Many {
			call = append(call, j.Qual(sch, "Many"))
		}

		if attr.Required {
			call = append(call, j.Qual(sch, "Required"))
		}

		if attr.Indexed {
			call = append(call, j.Qual(sch, "Indexed"))
		}

		if len(attr.Tags) > 0 {
			var tagArgs []j.Code
			for _, tag := range attr.Tags {
				tagArgs = append(tagArgs, j.Lit(tag))
			}
			call = append(call, j.Qual(sch, "Tags").Call(tagArgs...))
		}

		if len(attr.Choices) > 0 {
			var args []j.Code

			for _, v := range attr.Choices {
				args = append(args, j.Id("sb").Dot("Single").Call(j.Lit(v)))
			}

			call = append(call, j.Qual(sch, "Choices").Call(args...))
		}

		g.decl = append(g.decl,
			j.Id("sb").Dot(method).Call(call...))
	}

	simpleField := func(typ string) {
		g.ec.Fields = append(g.ec.Fields, &entity.SchemaField{
			Name: name,
			Type: typ,
			Id:   entity.Id(eid),
			Many: attr.Many,
		})
	}

	// Check if this is a reference to a standalone component
	if attr.Type != "" && attr.Type != "component" && g.sf.Components[attr.Type] != nil {
		componentName := toCamal(attr.Type)

		// Add field with component type
		if attr.Many {
			g.fields = append(g.fields, j.Id(fname).Index().Id(componentName).Tag(tag))
		} else {
			g.fields = append(g.fields, j.Id(fname).Id(componentName).Tag(tag))
		}

		// Decoder - decode component from entity attribute
		if attr.Many {
			d := j.For(j.List(j.Op("_"), j.Id("a")).Op(":=").Range().Id("e").Dot("GetAll").Call(g.Ident(fname))).Block(
				j.If(
					j.Id("a").Dot("Value").Dot("Kind").Call().Op("==").Qual(top, "KindComponent"),
				).Block(
					j.Var().Id("v").Id(componentName),
					j.Id("v").Dot("Decode").Call(j.Id("a").Dot("Value").Dot("Component").Call()),
					j.Id("o").Dot(fname).Op("=").Append(j.Id("o").Dot(fname), j.Id("v")),
				),
			)
			g.decoders = append(g.decoders, d)
		} else {
			d := j.If(
				j.List(j.Id("a"), j.Id("ok")).Op(":=").Id("e").Dot("Get").Call(g.Ident(fname)),
				j.Id("ok").Op("&&").Id("a").Dot("Value").Dot("Kind").Call().Op("==").Qual(top, "KindComponent"),
			).Block(
				j.Id("o").Dot(fname).Dot("Decode").Call(j.Id("a").Dot("Value").Dot("Component").Call()),
			)
			g.decoders = append(g.decoders, d)
		}

		// Encoder - encode component to entity attribute
		if attr.Many {
			g.encoders = append(g.encoders,
				j.For(j.List(j.Op("_"), j.Id("v")).Op(":=").Range().Id("o").Dot(fname)).Block(
					j.Id("attrs").Op("=").Append(j.Id("attrs"), j.Qual(top, "Component").
						Call(g.Ident(fname), j.Id("v").Dot("Encode").Call())),
				),
			)
			g.emptyChecks = append(g.emptyChecks, emptyCheck{
				notEmpty: j.Len(j.Id("o").Dot(fname)).Op("!=").Lit(0),
				isEmpty:  j.Len(j.Id("o").Dot(fname)).Op("==").Lit(0),
			})
		} else {
			g.encoders = append(g.encoders,
				j.If(j.Op("!").Id("o").Dot(fname).Dot("Empty").Call()).Block(
					j.Id("attrs").Op("=").Append(j.Id("attrs"), j.Qual(top, "Component").
						Call(g.Ident(fname), j.Id("o").Dot(fname).Dot("Encode").Call()))),
			)
			g.emptyChecks = append(g.emptyChecks, emptyCheck{
				notEmpty: j.Op("!").Id("o").Dot(fname).Dot("Empty").Call(),
				isEmpty:  j.Id("o").Dot(fname).Dot("Empty").Call(),
			})
		}

		simpleDecl("Component")

		// Populate Component field with the schema of the referenced component
		g.ec.Fields = append(g.ec.Fields, &entity.SchemaField{
			Name:      name,
			Type:      "component",
			Id:        entity.Id(eid),
			Many:      attr.Many,
			Component: g.componentSchemas[attr.Type],
		})

		return // Early return - handled as component reference
	}

	switch attr.Type {
	case "string":
		if attr.Many {
			g.fields = append(g.fields, j.Id(fname).Index().String().Tag(tag))

		} else {
			g.fields = append(g.fields, j.Id(fname).String().Tag(tag))
		}

		simpleDecoder("KindString", "String")
		simpleEncoder("String")
		simpleDecl("String")
		simpleField("string")
	case "keyword":
		if attr.Many {
			g.fields = append(g.fields, j.Id(fname).Index().Qual(topt, "Keyword").Tag(tag))

		} else {
			g.fields = append(g.fields, j.Id(fname).Qual(topt, "Keyword").Tag(tag))
		}

		simpleDecoder("KindKeyword", "Keyword")
		simpleEncoder("Keyword")
		simpleDecl("Keyword")
		simpleField("keyword")
	case "int":
		g.fields = append(g.fields, j.Id(fname).Int64().Tag(tag))
		simpleDecoder("KindInt64", "Int64")
		simpleEncoder("Int64")
		simpleDecl("Int64")
		simpleField("int")
	case "time":
		g.fields = append(g.fields, j.Id(fname).Qual("time", "Time").Tag(tag))
		simpleDecoder("KindTime", "Time")
		simpleEncoder("Time")
		simpleDecl("Time")
		simpleField("time")
	case "duration":
		if attr.Many {
			g.fields = append(g.fields, j.Id(fname).Index().Qual("time", "Duration").Tag(tag))
		} else {
			g.fields = append(g.fields, j.Id(fname).Qual("time", "Duration").Tag(tag))
		}
		simpleDecoder("KindDuration", "Duration")
		simpleEncoder("Duration")
		simpleDecl("Duration")
		simpleField("duration")
	case "ref":
		if attr.Many {
			g.fields = append(g.fields, j.Id(fname).Index().Qual(top, "Id").Tag(tag))
			g.decoders = append(g.decoders,
				j.For(j.List(j.Op("_"), j.Id("a")).Op(":=").Range().Id("e").Dot("GetAll").Call(g.Ident(fname))).Block(
					j.If(j.Id("a").Dot("Value").Dot("Kind").Call().Op("==").Qual(top, "KindId")).Block(
						j.Id("o").Dot(fname).Op("=").Append(j.Id("o").Dot(fname), j.Id("a").Dot("Value").Dot("Id").Call()),
					),
				),
			)
			g.encoders = append(g.encoders,
				j.For(j.List(j.Op("_"), j.Id("v")).Op(":=").Range().Id("o").Dot(fname)).Block(
					j.Id("attrs").Op("=").Append(j.Id("attrs"), j.Qual(top, "Ref").Call(g.Ident(fname), j.Id("v"))),
				),
			)
			g.emptyChecks = append(g.emptyChecks, emptyCheck{
				notEmpty: j.Len(j.Id("o").Dot(fname)).Op("!=").Lit(0),
				isEmpty:  j.Len(j.Id("o").Dot(fname)).Op("==").Lit(0),
			})
			simpleDecl("Ref")
			simpleField("id")
		} else {
			g.fields = append(g.fields, j.Id(fname).Qual(top, "Id").Tag(tag))
			simpleDecoder("KindId", "Id")
			simpleEncoder("Ref")
			simpleDecl("Ref")
			simpleField("id")
		}
	case "bool":
		g.fields = append(g.fields, j.Id(fname).Bool().Tag(tag))
		simpleDecoder("KindBool", "Bool")
		simpleEncoder("Bool")
		simpleDecl("Bool")
		simpleField("bool")
	case "bytes":
		g.fields = append(g.fields, j.Id(fname).Index().Byte().Tag(tag))
		simpleDecoder("KindBytes", "Bytes")
		simpleDecl("Bytes")
		simpleField("bytes")
		if attr.Many {
			g.encoders = append(g.encoders,
				j.For(j.List(j.Op("_"), j.Id("v")).Op(":=").Range().Id("o").Dot(fname)).Block(
					j.Id("attrs").Op("=").Append(j.Id("attrs"), j.Qual(top, "Bytes").Call(g.Ident(fname), j.Id("v"))),
				),
			)
			g.emptyChecks = append(g.emptyChecks, emptyCheck{
				notEmpty: j.Len(j.Id("o").Dot(fname)).Op("!=").Lit(0),
				isEmpty:  j.Len(j.Id("o").Dot(fname)).Op("==").Lit(0),
			})
		} else {
			g.encoders = append(g.encoders,
				j.If(j.Len(j.Id("o").Dot(fname)).Op(">").Lit(0)).Block(
					j.Id("attrs").Op("=").Append(j.Id("attrs"), j.Qual(top, "Bytes").Call(g.Ident(fname), j.Id("o").Dot(fname))),
				),
			)
			g.emptyChecks = append(g.emptyChecks, emptyCheck{
				notEmpty: j.Len(j.Id("o").Dot(fname)).Op("!=").Lit(0),
				isEmpty:  j.Len(j.Id("o").Dot(fname)).Op("==").Lit(0),
			})
		}

	case "label":
		if attr.Many {
			g.fields = append(g.fields, j.Id(fname).Qual(topt, "Labels").Tag(tag))
			g.encoders = append(g.encoders,
				j.For(j.List(j.Op("_"), j.Id("v")).Op(":=").Range().Id("o").Dot(fname)).Block(
					j.Id("attrs").Op("=").Append(j.Id("attrs"), j.Qual(top, "Label").Call(g.Ident(fname), j.Id("v").Dot("Key"), j.Id("v").Dot("Value"))),
				),
			)
			g.emptyChecks = append(g.emptyChecks, emptyCheck{
				notEmpty: j.Len(j.Id("o").Dot(fname)).Op("!=").Lit(0),
				isEmpty:  j.Len(j.Id("o").Dot(fname)).Op("==").Lit(0),
			})
		} else {
			g.fields = append(g.fields, j.Id(fname).Qual(topt, "Label").Tag(tag))
			g.encoders = append(g.encoders,
				j.If(j.Op("!").Qual(top, "Empty").Call(j.Id("o").Dot(fname))).Block(
					j.Id("attrs").Op("=").Append(j.Id("attrs"), j.Qual(top, "Label").Call(g.Ident(fname), j.Id("o").Dot(fname).Dot("Key"), j.Id("o").Dot(fname).Dot("Value")))),
			)
			g.emptyChecks = append(g.emptyChecks, emptyCheck{
				notEmpty: j.Op("!").Qual(top, "Empty").Call(j.Id("o").Dot(fname)),
				isEmpty:  j.Qual(top, "Empty").Call(j.Id("o").Dot(fname)),
			})
		}
		simpleDecoder("KindLabel", "Label")
		simpleDecl("Label")
		simpleField("label")

	case "enum":
		enum := g.sf.Enums[attr.Enum]
		enumType := toCamal(attr.Enum)
		mapName := g.local + fname

		if attr.Encoding == enumEncodingRef {
			legacyType := g.local + fname
			if legacyType != enumType {
				g.decodeouter = append(g.decodeouter, j.Type().Id(legacyType).Op("=").Id(enumType))
			}
			g.decodeouter = append(g.decodeouter, j.Const().DefsFunc(func(group *j.Group) {
				for _, value := range enum.Values {
					legacyConstant := strings.ToUpper(value)
					if g.isComponent {
						legacyConstant = g.local + legacyConstant
					}
					group.Id(legacyConstant).Id(enumType).Op("=").Id(enumConstant(attr.Enum, value))
				}
			}))
		}

		g.fields = append(g.fields, j.Id(fname).Id(enumType).Tag(tag))

		fc := map[string]entity.Id{}
		var schemaValues []j.Code

		switch attr.Encoding {
		case enumEncodingRef:
			for _, value := range enum.Values {
				// Retain the historical field-local singleton IDs so adopting a named
				// enum does not change existing entity bytes.
				id := name + "." + value
				if g.isComponent {
					id = attr.Attr + "." + value
				}

				fullAttrID := g.sf.Domain + "/" + id
				if _, ok := g.usedAttrs[fullAttrID]; ok {
					panic(fmt.Sprintf("Duplicate attribute name: %s", fullAttrID))
				}
				g.usedAttrs[fullAttrID] = struct{}{}

				g.idents = append(g.idents, j.Add(g.Ident(fname+toCamal(value))).Op("=").Qual(top, "Id").Call(j.Lit(fullAttrID)))
				fc[value] = entity.Id(fullAttrID)
				schemaValues = append(schemaValues, g.Ident(fname+toCamal(value)))
			}

			g.decodeouter = append(g.decodeouter, j.Var().Id(mapName+"FromId").Op("=").Map(j.Qual(top, "Id")).Id(enumType).ValuesFunc(func(b *j.Group) {
				for _, value := range enum.Values {
					b.Add(g.Ident(fname + toCamal(value))).Op(":").Id(enumConstant(attr.Enum, value))
				}
			}))
			g.decodeouter = append(g.decodeouter, j.Var().Id(mapName+"ToId").Op("=").Map(j.Id(enumType)).Qual(top, "Id").ValuesFunc(func(b *j.Group) {
				for _, value := range enum.Values {
					b.Id(enumConstant(attr.Enum, value)).Op(":").Add(g.Ident(fname + toCamal(value)))
				}
			}))

			g.decoders = append(g.decoders, j.If(
				j.List(j.Id("a"), j.Id("ok")).Op(":=").Id("e").Dot("Get").Call(g.Ident(fname)),
				j.Id("ok").Op("&&").Id("a").Dot("Value").Dot("Kind").Call().Op("==").Qual(top, "KindId"),
			).Block(
				j.Id("o").Dot(fname).Op("=").Id(mapName+"FromId").Index(j.Id("a").Dot("Value").Dot("Id").Call()),
			))
			g.encoders = append(g.encoders, j.If(
				j.List(j.Id("a"), j.Id("ok")).Op(":=").Id(mapName+"ToId").Index(j.Id("o").Dot(fname)), j.Id("ok"),
			).Block(
				j.Id("attrs").Op("=").Append(j.Id("attrs"), j.Qual(top, "Ref").Call(g.Ident(fname), j.Id("a"))),
			))

		case enumEncodingString:
			for _, value := range enum.Values {
				schemaValues = append(schemaValues, j.Lit(value))
			}
			g.decodeouter = append(g.decodeouter, enumStringMaps(mapName, enumType, attr.Enum, enum.Values)...)
			g.decoders = append(g.decoders, enumDecoder(g, fname, mapName+"FromString", "KindString", "String"))
			g.encoders = append(g.encoders, enumEncoder(g, fname, mapName+"ToString", "String"))

		case enumEncodingKeyword:
			for _, value := range enum.Values {
				keyword := enumID(g.sf, attr.Enum) + "." + value
				schemaValues = append(schemaValues, j.Qual(top, "MustKeyword").Call(j.Lit(keyword)))
			}
			g.decodeouter = append(g.decodeouter, enumKeywordMaps(g.sf, mapName, enumType, attr.Enum, enum.Values)...)
			g.decoders = append(g.decoders, enumDecoder(g, fname, mapName+"FromKeyword", "KindKeyword", "Keyword"))
			g.encoders = append(g.encoders, enumEncoder(g, fname, mapName+"ToKeyword", "Keyword"))
		}

		g.emptyChecks = append(g.emptyChecks, emptyCheck{
			notEmpty: j.Id("o").Dot(fname).Op("!=").Lit(""),
			isEmpty:  j.Id("o").Dot(fname).Op("==").Lit(""),
		})

		var call []j.Code
		call = append(call, j.Lit(name), j.Lit(eid))

		if attr.Doc != "" {
			call = append(call, j.Qual(sch, "Doc").Call(j.Lit(attr.Doc)))
		}

		if attr.Many {
			call = append(call, j.Qual(sch, "Many"))
		}

		if attr.Required {
			call = append(call, j.Qual(sch, "Required"))
		}

		if attr.Indexed {
			call = append(call, j.Qual(sch, "Indexed"))
		}

		if attr.Session {
			call = append(call, j.Qual(sch, "Session"))
		}

		if len(attr.Tags) > 0 {
			var tagArgs []j.Code
			for _, tag := range attr.Tags {
				tagArgs = append(tagArgs, j.Lit(tag))
			}
			call = append(call, j.Qual(sch, "Tags").Call(tagArgs...))
		}

		if attr.Encoding == enumEncodingRef {
			for _, value := range enum.Values {
				singletonPath := name + "." + value
				if g.isComponent {
					singletonPath = attr.Attr + "." + value
				}
				g.decl = append(g.decl, j.Id("sb").Dot("Singleton").Call(j.Lit(g.sf.Domain+"/"+singletonPath)))
			}
		}

		physicalType := attr.Encoding
		builder := ""
		switch attr.Encoding {
		case enumEncodingRef:
			builder = "Ref"
			call = append(call, j.Qual(sch, "Choices").Call(schemaValues...))
			// Ref-backed enums have always appeared as enums to natural-model
			// consumers, even though their persisted attribute schema is TypeRef.
			physicalType = "enum"
		case enumEncodingString:
			builder = "String"
			call = append(call, j.Qual(sch, "EnumValues").Call(schemaValues...))
		case enumEncodingKeyword:
			builder = "Keyword"
			call = append(call, j.Qual(sch, "EnumValues").Call(schemaValues...))
		}
		g.decl = append(g.decl, j.Id("sb").Dot(builder).Call(call...))

		g.ec.Fields = append(g.ec.Fields, &entity.SchemaField{
			Name:         name,
			Type:         physicalType,
			Id:           entity.Id(eid),
			Many:         attr.Many,
			Enum:         enumID(g.sf, attr.Enum),
			EnumEncoding: attr.Encoding,
			EnumMembers:  enum.Values,
			EnumValues:   fc,
		})
	case "component":
		var sg gen
		sg.usedAttrs = g.usedAttrs
		sg.componentSchemas = g.componentSchemas
		sg.isComponent = g.isComponent // Inherit component context from parent
		sg.sf = g.sf
		sg.f = g.f
		sg.prefix = g.prefix + "." + name

		// Only apply parent prefixing for standalone components (isComponent=true)
		// This avoids breaking changes for existing kinds
		if g.isComponent {
			sg.name = g.local + fname  // Prefixed: e.g., "ConfigSpecNested"
			sg.local = g.local + fname // Prefixed identifiers
		} else {
			sg.name = fname  // Simple: e.g., "Nested" (backward compatibility)
			sg.local = fname // Simple identifiers
		}

		// Use the nested struct's name for the field type
		typeName := sg.name
		if attr.Many {
			g.fields = append(g.fields, j.Id(fname).Index().Id(typeName).Tag(tag))
		} else {
			g.fields = append(g.fields, j.Id(fname).Id(typeName).Tag(tag))
		}
		sg.ec = &entity.EncodedSchema{
			Domain:  g.sf.Domain,
			Name:    g.prefix + "." + name,
			Version: g.sf.Version,
		}

		for k, v := range mapx.StableOrder(attr.Attrs) {
			if v.Attr == "" {
				// For kinds: use simple path (backward compatibility)
				// For components: use full parent path for namespacing
				if g.isComponent {
					v.Attr = attr.Attr + "." + k
				} else {
					v.Attr = name + "." + k
				}
			}

			// Use full attribute ID for duplicate checking (includes domain)
			fullAttrId := g.sf.Domain + "/" + v.Attr
			if _, ok := g.usedAttrs[fullAttrId]; ok {
				panic(fmt.Sprintf("Duplicate attribute name: %s", fullAttrId))
			}

			g.usedAttrs[fullAttrId] = struct{}{}

			sg.attr(k, v)
		}

		g.subgen = append(g.subgen, &sg)

		if attr.Many {
			d :=
				j.For(j.List(j.Op("_"), j.Id("a")).Op(":=").Range().Id("e").Dot("GetAll").Call(g.Ident(fname))).Block(
					j.If(
						j.Id("a").Dot("Value").Dot("Kind").Call().Op("==").Qual(top, "KindComponent"),
					).Block(
						j.Var().Id("v").Id(typeName),
						j.Id("v").Dot("Decode").Call(j.Id("a").Dot("Value").Dot("Component").Call()),
						j.Id("o").Dot(fname).Op("=").Append(j.Id("o").Dot(fname), j.Id("v")),
					),
				)
			g.decoders = append(g.decoders, d)

		} else {
			d := j.If(
				j.List(j.Id("a"), j.Id("ok")).Op(":=").Id("e").Dot("Get").Call(g.Ident(fname)),
				j.Id("ok").Op("&&").Id("a").Dot("Value").Dot("Kind").Call().Op("==").Qual(top, "KindComponent"),
			).Block(
				j.Id("o").Dot(fname).Dot("Decode").Call(j.Id("a").Dot("Value").Dot("Component").Call()),
			)
			g.decoders = append(g.decoders, d)
		}

		if attr.Many {
			g.encoders = append(g.encoders,
				j.For(j.List(j.Op("_"), j.Id("v")).Op(":=").Range().Id("o").Dot(fname)).Block(
					j.Id("attrs").Op("=").Append(j.Id("attrs"), j.Qual(top, "Component").
						Call(g.Ident(fname), j.Id("v").Dot("Encode").Call())),
				),
			)
			g.emptyChecks = append(g.emptyChecks, emptyCheck{
				notEmpty: j.Len(j.Id("o").Dot(fname)).Op("!=").Lit(0),
				isEmpty:  j.Len(j.Id("o").Dot(fname)).Op("==").Lit(0),
			})
		} else {
			g.encoders = append(g.encoders,
				j.If(j.Op("!").Id("o").Dot(fname).Dot("Empty").Call()).Block(
					j.Id("attrs").Op("=").Append(j.Id("attrs"), j.Qual(top, "Component").
						Call(g.Ident(fname), j.Id("o").Dot(fname).Dot("Encode").Call()))),
			)
			g.emptyChecks = append(g.emptyChecks, emptyCheck{
				notEmpty: j.Op("!").Id("o").Dot(fname).Dot("Empty").Call(),
				isEmpty:  j.Id("o").Dot(fname).Dot("Empty").Call(),
			})
		}
		simpleDecl("Component")

		g.decl = append(g.decl,
			j.Parens(j.Op("&").Id(typeName).Values()).Dot("InitSchema").Call(j.Id("sb").Dot("Builder").Call(j.Lit(attr.Attr))))

		g.ec.Fields = append(g.ec.Fields, &entity.SchemaField{
			Name:      name,
			Type:      "component",
			Id:        entity.Id(eid),
			Many:      attr.Many,
			Component: sg.ec,
		})
	default:
		panic(fmt.Sprintf("Unknown attribute type: %s", attr.Type))
	}
}

func (g *gen) generate() {
	name := g.name

	idx := strings.LastIndex(name, ".")
	if idx != -1 {
		name = name[idx+1:]
	}

	g.ec.PrimaryKind = strings.ToLower(name)

	structName := toCamal(name)
	g.structName = structName

	f := g.f

	f.Const().DefsFunc(func(b *j.Group) {
		for _, id := range g.idents {
			b.Add(id)
		}
	})

	// Generate the struct
	g.f.Type().Id(structName).Struct(g.fields...)

	f.Line()

	for _, id := range g.decodeouter {
		f.Add(id)
	}

	f.Func().
		Params(j.Id("o").Op("*").Id(structName)).Id("Decode").
		Params(j.Id("e").Qual(top, "AttrGetter")).
		BlockFunc(func(b *j.Group) {
			for _, d := range g.decoders {
				b.Add(d)
			}
		})

	f.Line()

	// Only generate entity-specific methods for kinds, not standalone components
	if g.kind != "" && !g.isComponent {
		f.Func().
			Params(j.Id("o").Op("*").Id(structName)).Id("Is").
			Params(j.Id("e").Qual(top, "AttrGetter")).Bool().
			BlockFunc(func(b *j.Group) {
				b.Return(j.Qual(top, "Is").Call(j.Id("e"), j.Id("Kind"+toCamal(g.kind))))
			})

		f.Line()

		f.Func().
			Params(j.Id("o").Op("*").Id(structName)).Id("ShortKind").
			Params().String().
			BlockFunc(func(b *j.Group) {
				b.Return(j.Lit(g.kind))
			})

		f.Line()

		f.Func().
			Params(j.Id("o").Op("*").Id(structName)).Id("Kind").
			Params().Qual(top, "Id").
			BlockFunc(func(b *j.Group) {
				b.Return(j.Id("Kind" + toCamal(g.kind)))
			})

		f.Line()

		f.Func().
			Params(j.Id("o").Op("*").Id(structName)).Id("EntityId").
			Params().Params(j.Qual(top, "Id")).
			BlockFunc(func(b *j.Group) {
				b.Return(j.Id("o").Dot("ID"))
			})

		f.Line()

	}

	f.Func().
		Params(j.Id("o").Op("*").Id(structName)).Id("Encode").
		Params().Params(j.Id("attrs").Index().Qual(top, "Attr")).
		BlockFunc(func(b *j.Group) {
			for _, d := range g.encoders {
				b.Add(d)
			}
			// Only append Kind reference for entity kinds, not standalone components
			if g.kind != "" && !g.isComponent {
				b.Id("attrs").Op("=").Append(
					j.Id("attrs"),
					j.Qual(top, "Ref").Call(j.Qual(top, "EntityKind"), j.Id("Kind"+toCamal(g.kind))),
				)
			}
			b.Return()
		})

	f.Line()

	f.Func().
		Params(j.Id("o").Op("*").Id(structName)).Id("Empty").
		Params().Params(j.Bool()).
		BlockFunc(func(b *j.Group) {
			if len(g.emptyChecks) == 1 {
				b.Return(g.emptyChecks[0].isEmpty)
			} else {
				for _, c := range g.emptyChecks {
					b.If(c.notEmpty).Block(j.Return(j.False()))
				}
				b.Return(j.True())
			}
		})

	f.Line()

	f.Func().
		Params(j.Id("o").Op("*").Id(structName)).
		Id("InitSchema").Params(j.Id("sb").Op("*").Qual(sch, "SchemaBuilder")).
		BlockFunc(func(b *j.Group) {
			for _, d := range g.decl {
				b.Add(d)
			}
		})

	f.Line()

	// Generate nested attributes
	for _, sg := range g.subgen {
		sg.generate()
	}
}
