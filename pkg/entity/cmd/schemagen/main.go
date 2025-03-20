package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"strings"

	j "github.com/dave/jennifer/jen"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/tools/imports"
	"gopkg.in/yaml.v3"
	"miren.dev/runtime/pkg/mapx"
)

var (
	fInput  = flag.String("input", "", "Input file for schema generation")
	fPkg    = flag.String("pkg", "entity", "Package name for generated code")
	fOutput = flag.String("output", "", "output file")
)

const (
	top = "miren.dev/runtime/pkg/entity"
	sch = "miren.dev/runtime/pkg/entity/schema"
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

	var g gen
	g.name = sf.Domain
	g.prefix = sf.Domain
	g.sf = &sf
	g.f = j.NewFile(*fPkg)

	g.fields = append(g.fields, j.Id("ID").String().Tag(map[string]string{
		"json": "id",
	}))

	g.decoders = append(g.decoders,
		j.Id("o").Dot("ID").Op("=").Qual(top, "MustGet").Call(j.Id("e"), j.Qual(top, "DBId")).Dot("Value").Dot("String").Call())

	for name, attr := range mapx.StableOrder(sf.Attrs) {
		g.attr(name, attr)
	}

	g.generate()

	g.f.Line()

	g.f.Func().Id("init").Params().BlockFunc(func(b *j.Group) {
		b.Add(j.Qual(sch, "Register").Call(
			j.Lit(sf.Domain),
			j.Lit(sf.Version),
			j.Parens(j.Op("&").Id(g.structName).Values()).Dot("InitSchema"),
		))
	})

	var buf bytes.Buffer

	err = g.f.Render(&buf)
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
	Domain  string                `yaml:"domain"`
	Version string                `yaml:"version"`
	Attrs   map[string]schemaAttr `yaml:"attrs"`
}

type schemaAttr struct {
	Type     string   `yaml:"type"`
	Doc      string   `yaml:"doc"`
	Many     bool     `yaml:"many,omitempty"`     // for repeated attributes
	Required bool     `yaml:"required,omitempty"` // for required attributes
	Choices  []string `yaml:"choices,omitempty"`  // for enum attributes

	Attrs map[string]schemaAttr `yaml:"attrs,omitempty"` // for nested attributes
}

type gen struct {
	name   string
	prefix string
	local  string

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

var caser = cases.Title(language.English)

func (g *gen) attr(name string, attr schemaAttr) {
	fname := caser.String(name)

	g.idents = append(g.idents, j.Id(g.local+fname+"Id").Op("=").Qual(top, "Id").Call(j.Lit(g.prefix+"/"+name)))

	tn := name
	if !attr.Required {
		tn = tn + ",omitempty"
	} else {
		g.ensureAttrs = append(g.ensureAttrs, j.Id(g.local+fname+"Id"))
	}

	tag := map[string]string{
		"json": tn,
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
				j.Id("attrs").Op("=").Append(j.Id("attrs"), j.Qual(top, method).Call(g.Ident(fname), j.Id("o").Dot(fname))),
			)
		}
	}

	simpleDecl := func(method string) {
		var call []j.Code
		call = append(call, j.Lit(name))

		if attr.Doc != "" {
			call = append(call, j.Qual(sch, "Doc").Call(j.Lit(attr.Doc)))
		}

		if attr.Many {
			call = append(call, j.Qual(sch, "Many"))
		}

		if attr.Required {
			call = append(call, j.Qual(sch, "Required"))
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
	case "int":
		g.fields = append(g.fields, j.Id(fname).Int64().Tag(tag))
		simpleDecoder("KindInt64", "Int64")
		simpleEncoder("Int64")
		simpleDecl("Int64")
	case "bool":
		g.fields = append(g.fields, j.Id(fname).Bool().Tag(tag))
		simpleDecoder("KindBool", "Bool")
		simpleEncoder("Bool")
		simpleDecl("Bool")
	case "enum":
		g.decodeouter = append(g.decodeouter, j.Type().Add(g.NSd(fname)).String())

		g.fields = append(g.fields, j.Id(fname).Add(g.NSd(fname)).Tag(tag))

		for _, v := range attr.Choices {
			g.idents = append(g.idents, j.Add(g.Ident(fname+caser.String(v))).Op("=").Qual(top, "Id").
				Call(j.Lit(g.prefix+"/"+name+"."+v)))
		}

		g.decodeouter = append(g.decodeouter, j.Const().DefsFunc(func(b *j.Group) {
			for _, v := range attr.Choices {
				b.Add(j.Id(strings.ToUpper(v)).Add(g.NSd(fname)).Op("=").Add(j.Lit(name + "." + v))) // g.Ident(fname + caser.String(v))))
			}
		}))

		g.decodeouter = append(g.decodeouter, j.Var().Id(name+"FromId").Op("=").Map(j.Qual(top, "Id")).Add(g.NSd(fname)).
			ValuesFunc(func(b *j.Group) {
				for _, v := range attr.Choices {
					b.Add(g.Ident(fname + caser.String(v))).Op(":").Id(strings.ToUpper(v))
				}
			}))

		g.decodeouter = append(g.decodeouter, j.Var().Id(name+"ToId").Op("=").Map(g.NSd(fname)).Qual(top, "Id").
			ValuesFunc(func(b *j.Group) {
				for _, v := range attr.Choices {
					b.Add(j.Id(strings.ToUpper(v)).Op(":").Add(g.Ident(fname + caser.String(v))))
				}
			}))

		/*
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
		*/

		d := j.If(
			j.List(j.Id("a"), j.Id("ok")).Op(":=").Id("e").Dot("Get").Call(g.Ident(fname)),
			j.Id("ok").Op("&&").Id("a").Dot("Value").Dot("Kind").Call().Op("==").Qual(top, "KindId"),
		).Block(
			j.Id("o").Dot(fname).Op("=").Id(name + "FromId").Index(j.Id("a").Dot("Value").Dot("Id").Call()),
		)
		g.decoders = append(g.decoders, d)

		var call []j.Code
		call = append(call, j.Lit(name))

		if attr.Doc != "" {
			call = append(call, j.Qual(sch, "Doc").Call(j.Lit(attr.Doc)))
		}

		if attr.Many {
			call = append(call, j.Qual(sch, "Many"))
		}

		if attr.Required {
			call = append(call, j.Qual(sch, "Required"))
		}

		var args []j.Code

		for _, v := range attr.Choices {
			g.decl = append(g.decl, j.Id("sb").Dot("Singleton").Call(j.Lit(name+"."+v)))
			args = append(args, g.Ident(fname+caser.String(v)))
		}

		call = append(call, j.Qual(sch, "Choices").Call(args...))

		g.decl = append(g.decl,
			j.Id("sb").Dot("Ref").Call(call...))

	case "component":
		if attr.Many {
			g.fields = append(g.fields, j.Id(fname).Index().Id(fname).Tag(tag))
		} else {
			g.fields = append(g.fields, j.Id(fname).Id(fname).Tag(tag))
		}

		var sg gen
		sg.name = fname
		sg.sf = g.sf
		sg.f = g.f
		sg.local = fname
		sg.prefix = g.prefix + "." + name

		for k, v := range mapx.StableOrder(attr.Attrs) {
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
				j.List(j.Id("a"), j.Id("ok")).Op("=").Id("e").Dot("Get").Call(g.Ident(fname)),
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
				j.Id("attrs").Op("=").Append(j.Id("attrs"), j.Qual(top, "Component").
					Call(g.Ident(fname), j.Id("o").Dot(fname).Dot("Encode").Call())),
			)
		}
		simpleDecl("Component")

		g.decl = append(g.decl,
			j.Parens(j.Op("&").Id(fname).Values()).Dot("InitSchema").Call(j.Id("sb").Dot("Builder").Call(j.Lit(name))))
	}
}

func (g *gen) generate() {
	name := g.name

	idx := strings.LastIndex(name, ".")
	if idx != -1 {
		name = name[idx+1:]
	}

	structName := caser.String(name)
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

	f.Func().
		Params(j.Id("o").Op("*").Id(structName)).Id("Encode").
		Params().Params(j.Id("attrs").Index().Qual(top, "Attr")).
		BlockFunc(func(b *j.Group) {
			for _, d := range g.encoders {
				b.Add(d)
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
