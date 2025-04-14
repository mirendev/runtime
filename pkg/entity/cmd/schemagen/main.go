package main

import (
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"os"
	"strings"

	j "github.com/dave/jennifer/jen"
	"github.com/fxamacker/cbor/v2"
	"golang.org/x/tools/imports"
	"gopkg.in/yaml.v3"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/mapx"
)

var (
	fInput  = flag.String("input", "", "Input file for schema generation")
	fPkg    = flag.String("pkg", "entity", "Package name for generated code")
	fOutput = flag.String("output", "", "output file")
)

const (
	top  = "miren.dev/runtime/pkg/entity"
	topt = "miren.dev/runtime/pkg/entity/types"
	sch  = "miren.dev/runtime/pkg/entity/schema"
)

func main() {
	flag.Parse()

	if *fInput == "" {
		panic("Input file must be specified")
	}

	f, err := os.Open(*fInput)
	if err != nil {
		panic(err)
	}

	defer f.Close()

	var sf schemaFile

	if err := yaml.NewDecoder(f).Decode(&sf); err != nil {
		panic(err)
	}

	var ed entity.EncodedDomain
	ed.Name = sf.Domain
	ed.Version = sf.Version
	ed.Kinds = make(map[string]*entity.EncodedSchema)
	ed.ShortKinds = make(map[string]string)

	jf := j.NewFile(*fPkg)

	var (
		kinds   []string
		structs []string
	)

	usedAttrs := map[string]struct{}{}

	for kind, attrs := range mapx.StableOrder(sf.Kinds) {
		kinds = append(kinds, kind)

		var g gen
		g.usedAttrs = usedAttrs
		g.kind = kind
		g.name = kind // sf.Domain
		g.prefix = sf.Domain + "." + kind
		g.local = toCamal(kind)
		g.sf = &sf
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

			if _, ok := usedAttrs[attr.Attr]; ok {
				panic(fmt.Sprintf("Duplicate attribute name: %s", attr.Attr))
			}

			g.usedAttrs[attr.Attr] = struct{}{}

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

		data, err := cbor.Marshal(ed)
		if err != nil {
			panic(err)
		}

		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		zw.Write(data)
		zw.Flush()

		b.Qual(sch, "RegisterEncodedSchema").Call(
			j.Lit(sf.Domain),
			j.Lit(sf.Version),
			j.Index().Byte().Call(j.Lit(buf.String())),
		)
	})

	var buf bytes.Buffer

	err = jf.Render(&buf)
	if err != nil {
		str := err.Error()
		lines := strings.Split(str, "\n")

		hdr := lines[0]

		var sb strings.Builder

		sb.WriteString(hdr)
		sb.WriteString("\n")

		for i, line := range lines[1:] {
			fmt.Fprintf(&sb, "%d: %s\n", i+1, line)
		}

		fmt.Println(sb.String())
		os.Exit(1)
	}

	code, err := imports.Process("out.go", buf.Bytes(), &imports.Options{})
	if err != nil {
		str := err.Error()
		lines := strings.Split(str, "\n")

		hdr := lines[0]

		var sb strings.Builder

		sb.WriteString(hdr)
		sb.WriteString("\n")

		for i, line := range lines[1:] {
			fmt.Fprintf(&sb, "%d: %s\n", i+1, line)
		}

		fmt.Println(sb.String())
		os.Exit(1)
	}

	if *fOutput == "" {
		fmt.Println(string(code))
	} else {
		err = os.WriteFile(*fOutput, code, 0644)
		if err != nil {
			panic(err)
		}
	}
}

type schemaFile struct {
	Domain  string                 `yaml:"domain"`
	Version string                 `yaml:"version"`
	Major   string                 `yaml:"kind-major"`
	Kinds   map[string]schemaAttrs `yaml:"kinds"`
	//Attrs   map[string]schemaAttr `yaml:"attrs"`
}

type schemaAttrs map[string]*schemaAttr

type schemaAttr struct {
	Type     string   `yaml:"type"`
	Doc      string   `yaml:"doc"`
	Attr     string   `yaml:"attr,omitempty"`     // for attribute name
	Many     bool     `yaml:"many,omitempty"`     // for repeated attributes
	Required bool     `yaml:"required,omitempty"` // for required attributes
	Choices  []string `yaml:"choices,omitempty"`  // for enum attributes
	Indexed  bool     `yaml:"indexed,omitempty"`  // for indexed attributes

	Attrs map[string]*schemaAttr `yaml:"attrs,omitempty"` // for nested attributes
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

type gen struct {
	kind   string
	name   string
	prefix string
	local  string

	usedAttrs map[string]struct{}

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

	subgen []*gen // for nested attributes
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

	g.idents = append(g.idents, j.Id(g.local+fname+"Id").Op("=").Qual(top, "Id").Call(j.Lit(eid)))

	tn := name
	if !attr.Required {
		tn = tn + ",omitempty"
	} else {
		g.ensureAttrs = append(g.ensureAttrs, j.Id(g.local+fname+"Id"))
	}

	tag := map[string]string{
		"json": tn,
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
		} else {
			g.encoders = append(g.encoders,
				j.If(j.Op("!").Qual(top, "Empty").Call(j.Id("o").Dot(fname))).Block(
					j.Id("attrs").Op("=").Append(j.Id("attrs"), j.Qual(top, method).Call(g.Ident(fname), j.Id("o").Dot(fname))),
				),
			)
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
	case "ref":
		g.fields = append(g.fields, j.Id(fname).Qual(top, "Id").Tag(tag))
		simpleDecoder("KindId", "Id")
		simpleEncoder("Ref")
		simpleDecl("Ref")
		simpleField("id")
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
		} else {
			g.encoders = append(g.encoders,
				j.If(j.Id("len").Call(j.Id("o").Dot(fname)).Op("==").Lit(0)).Block(
					j.Id("attrs").Op("=").Append(j.Id("attrs"), j.Qual(top, "Bytes").Call(g.Ident(fname), j.Id("o").Dot(fname))),
				),
			)
		}

	case "label":
		if attr.Many {
			g.fields = append(g.fields, j.Id(fname).Qual(topt, "Labels").Tag(tag))
			g.encoders = append(g.encoders,
				j.For(j.List(j.Op("_"), j.Id("v")).Op(":=").Range().Id("o").Dot(fname)).Block(
					j.Id("attrs").Op("=").Append(j.Id("attrs"), j.Qual(top, "Label").Call(g.Ident(fname), j.Id("v").Dot("Key"), j.Id("v").Dot("Value"))),
				),
			)
		} else {
			g.fields = append(g.fields, j.Id(fname).Qual(topt, "Label").Tag(tag))
			g.encoders = append(g.encoders,
				j.If(j.Op("!").Qual(top, "Empty").Call(j.Id("o").Dot(fname))).Block(
					j.Id("attrs").Op("=").Append(j.Id("attrs"), j.Qual(top, "Label").Call(g.Ident(fname), j.Id("v").Dot("Key"), j.Id("v").Dot("Value")))),
			)
		}
		simpleDecoder("KindLabel", "Label")
		simpleDecl("Label")
		simpleField("label")

	case "enum":
		g.decodeouter = append(g.decodeouter, j.Type().Add(g.NSd(fname)).String())

		g.fields = append(g.fields, j.Id(fname).Add(g.NSd(fname)).Tag(tag))

		fc := map[string]entity.Id{}

		for _, v := range attr.Choices {
			id := name + "." + v

			if _, ok := g.usedAttrs[id]; ok {
				panic(fmt.Sprintf("Duplicate attribute name: %s", id))
			}

			g.usedAttrs[id] = struct{}{}

			g.idents = append(g.idents, j.Add(g.Ident(fname+toCamal(v))).Op("=").Qual(top, "Id").
				Call(j.Lit(g.sf.Domain+"/"+id)))

			fc[v] = entity.Id(g.sf.Domain + "/" + id)
		}

		g.decodeouter = append(g.decodeouter, j.Const().DefsFunc(func(b *j.Group) {
			for _, v := range attr.Choices {
				b.Add(j.Id(strings.ToUpper(v)).Add(g.NSd(fname)).Op("=").Add(j.Lit(name + "." + v)))
			}
		}))

		g.decodeouter = append(g.decodeouter, j.Var().Id(g.name+name+"FromId").Op("=").Map(j.Qual(top, "Id")).Add(g.NSd(fname)).
			ValuesFunc(func(b *j.Group) {
				for _, v := range attr.Choices {
					b.Add(g.Ident(fname + toCamal(v))).Op(":").Id(strings.ToUpper(v))
				}
			}))

		g.decodeouter = append(g.decodeouter, j.Var().Id(g.name+name+"ToId").Op("=").Map(g.NSd(fname)).Qual(top, "Id").
			ValuesFunc(func(b *j.Group) {
				for _, v := range attr.Choices {
					b.Add(j.Id(strings.ToUpper(v)).Op(":").Add(g.Ident(fname + toCamal(v))))
				}
			}))

		d := j.If(
			j.List(j.Id("a"), j.Id("ok")).Op(":=").Id("e").Dot("Get").Call(g.Ident(fname)),
			j.Id("ok").Op("&&").Id("a").Dot("Value").Dot("Kind").Call().Op("==").Qual(top, "KindId"),
		).Block(
			j.Id("o").Dot(fname).Op("=").Id(g.name + name + "FromId").Index(j.Id("a").Dot("Value").Dot("Id").Call()),
		)
		g.decoders = append(g.decoders, d)

		enc := j.If(
			j.List(j.Id("a"), j.Id("ok")).Op(":=").Id(g.name+name+"ToId").Index(j.Id("o").Dot(fname)), j.Id("ok"),
		).Block(
			j.Id("attrs").Op("=").Append(j.Id("attrs"), j.Qual(top, "Ref").Call(g.Ident(fname), j.Id("a"))),
		)
		g.encoders = append(g.encoders, enc)

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

		var args []j.Code

		for _, v := range attr.Choices {
			g.decl = append(g.decl, j.Id("sb").Dot("Singleton").Call(j.Lit(g.sf.Domain+"/"+name+"."+v)))
			args = append(args, g.Ident(fname+toCamal(v)))
		}

		call = append(call, j.Qual(sch, "Choices").Call(args...))

		g.decl = append(g.decl,
			j.Id("sb").Dot("Ref").Call(call...))

		g.ec.Fields = append(g.ec.Fields, &entity.SchemaField{
			Name:       name,
			Type:       "enum",
			Id:         entity.Id(eid),
			Many:       attr.Many,
			EnumValues: fc,
		})
	case "component":
		if attr.Many {
			g.fields = append(g.fields, j.Id(fname).Index().Id(fname).Tag(tag))
		} else {
			g.fields = append(g.fields, j.Id(fname).Id(fname).Tag(tag))
		}

		var sg gen
		sg.usedAttrs = g.usedAttrs
		sg.name = fname
		sg.sf = g.sf
		sg.f = g.f
		sg.local = fname
		sg.prefix = g.prefix + "." + name
		sg.ec = &entity.EncodedSchema{
			Domain:  g.sf.Domain,
			Name:    g.prefix + "." + name,
			Version: g.sf.Version,
		}

		for k, v := range mapx.StableOrder(attr.Attrs) {
			if v.Attr == "" {
				v.Attr = name + "." + k
			}

			if _, ok := g.usedAttrs[v.Attr]; ok {
				panic(fmt.Sprintf("Duplicate attribute name: %s", attr.Attr))
			}

			g.usedAttrs[v.Attr] = struct{}{}

			sg.attr(k, v)
		}

		g.subgen = append(g.subgen, &sg)

		if attr.Many {
			d :=
				j.For(j.List(j.Op("_"), j.Id("a")).Op(":=").Range().Id("e").Dot("GetAll").Call(g.Ident(fname))).Block(
					j.If(
						j.Id("a").Dot("Value").Dot("Kind").Call().Op("==").Qual(top, "KindComponent"),
					).Block(
						j.Var().Id("v").Id(fname),
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
		} else {
			g.encoders = append(g.encoders,
				j.If(j.Op("!").Qual(top, "Empty").Call(j.Id("o").Dot(fname))).Block(
					j.Id("attrs").Op("=").Append(j.Id("attrs"), j.Qual(top, "Component").
						Call(g.Ident(fname), j.Id("o").Dot(fname).Dot("Encode").Call()))),
			)
		}
		simpleDecl("Component")

		g.decl = append(g.decl,
			j.Parens(j.Op("&").Id(fname).Values()).Dot("InitSchema").Call(j.Id("sb").Dot("Builder").Call(j.Lit(name))))

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
	//g.sf.Kinds = append(g.sf.Kinds, strings.ToLower(name))

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

	if g.kind != "" {
		f.Func().
			Params(j.Id("o").Op("*").Id(structName)).Id("Is").
			Params(j.Id("e").Qual(top, "AttrGetter")).Bool().
			BlockFunc(func(b *j.Group) {
				b.Return(j.Qual(top, "Is").Call(j.Id("e"), j.Id("Kind"+toCamal(g.kind))))
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
			if g.kind != "" {
				b.Id("attrs").Op("=").Append(
					j.Id("attrs"),
					j.Qual(top, "Ref").Call(j.Qual(top, "EntityKind"), j.Id("Kind"+toCamal(g.kind))),
				)
			}
			b.Return()
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
