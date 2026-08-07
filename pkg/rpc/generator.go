package rpc

import (
	"bytes"
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	j "github.com/dave/jennifer/jen"
	"github.com/pkg/errors"
	"golang.org/x/tools/imports"
	"gopkg.in/yaml.v3"
)

type Generator struct {
	Imports    map[string]Import
	Types      []*DescType
	Interfaces []*DescInterface

	importedGenerators map[string]*Generator
	importedPackages   map[string][]string

	typeInfo map[string]typeInfo
}

func NewGenerator() (*Generator, error) {
	return &Generator{
		typeInfo:           make(map[string]typeInfo),
		importedGenerators: make(map[string]*Generator),
		importedPackages:   make(map[string][]string),
	}, nil
}

type Import struct {
	Path   string   `yaml:"path"`
	Import string   `yaml:"import"`
	Types  []string `yaml:"types"`
}

func (g *Generator) Read(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}

	var df DescFile

	err = yaml.NewDecoder(f).Decode(&df)
	if err != nil {
		return err
	}

	g.Imports = df.Imports
	g.Types = df.Types
	g.Interfaces = df.Interfaces

	err = g.processImports(path)
	if err != nil {
		return err
	}

	g.populateTypeInfo()

	ut := make(map[string]*DescType)

	for _, t := range g.Types {
		ut[t.Type] = t
	}

	for _, t := range g.Types {
		t.CalculateOffsets(ut)
	}

	return nil
}

func (g *Generator) processImports(src string) error {
	for name, path := range g.Imports {
		if path.Path == "" {
			g.importedPackages[name] = path.Types
		} else {
			relPath := filepath.Join(src, "..", path.Path)

			sg, err := NewGenerator()
			if err != nil {
				return err
			}

			err = sg.Read(relPath)
			if err != nil {
				return err
			}

			g.importedGenerators[name] = sg
		}
	}

	return nil
}

func (g *Generator) ti(name string) typeInfo {
	idx := strings.IndexByte(name, '[')
	if idx != -1 {
		name = name[:idx]
	}

	dot := strings.LastIndexByte(name, '.')
	if dot != -1 {
		imp, ok := g.importedGenerators[name[:dot]]
		if !ok {
			x, ok := g.importedPackages[name[:dot]]
			if ok {
				if slices.Contains(x, name[dot+1:]) {
					return typeInfo{}
				}
			}

			panic("missing import for " + name[:dot])
		}

		return imp.typeInfo[name[dot+1:]]
	}

	return g.typeInfo[name]
}

func (g *Generator) splitType(name string) (string, string) {
	idx := strings.LastIndexByte(name, '.')
	if idx != -1 {
		return name[:idx], name[idx+1:]
	}

	return name, ""
}

func (g *Generator) isImported(name string) bool {
	dot := strings.LastIndexByte(name, '.')

	if dot != -1 {
		_, ok := g.importedGenerators[name[:dot]]
		if !ok {
			panic("missing import for " + name[:dot])
		}

		return true
	}

	return false
}

func capitalize(s string) string {
	return toCamal(s)
}

func toSnake(s string) string {
	var b bytes.Buffer

	for i, c := range s {
		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(c + 32)
		} else {
			b.WriteByte(byte(c))
		}
	}

	return b.String()
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

func private(s string) string {
	return strings.ToLower(s[:1]) + s[1:]
}

func (t *DescInterface) typeName(name string) *j.Statement {
	base := j.Id(name)

	if len(t.Generic) == 0 {
		return base
	}

	return base.TypesFunc(func(gr *j.Group) {
		for _, g := range t.Generic {
			gr.Id(g)
		}
	})
}

func (t *DescInterface) addGeneric(name string) (*j.Statement, *j.Statement) {
	base := j.Id(name)
	if len(t.Generic) == 0 {
		return base, base
	}

	base = base.TypesFunc(func(gr *j.Group) {
		for _, g := range t.Generic {
			gr.Id(g).Id("any")
		}
	})

	recv := j.Id(name)

	recv = recv.TypesFunc(func(gr *j.Group) {
		for _, g := range t.Generic {
			gr.Id(g)
		}
	})

	return base, recv
}

func (t *DescType) addGeneric(name string) (*j.Statement, *j.Statement) {
	base := j.Id(name)
	if len(t.Generic) == 0 {
		return base, base
	}

	base = base.TypesFunc(func(gr *j.Group) {
		for _, g := range t.Generic {
			gr.Id(g).Id("any")
		}
	})

	recv := j.Id(name)

	recv = recv.TypesFunc(func(gr *j.Group) {
		for _, g := range t.Generic {
			gr.Id(g)
		}
	})

	return base, recv
}

func (g *Generator) properType(name string) *j.Statement {
	dot := strings.LastIndexByte(name, '.')

	if dot != -1 {
		imp, ok := g.Imports[name[:dot]]
		if !ok {
			panic("missing import for " + name[:dot])
		}

		return j.Qual(imp.Import, name[dot+1:])
	}

	return j.Id(name)
}

func (g *Generator) mapType(field *DescField) *j.Statement {
	return j.Map(g.properType(field.Key)).Add(g.properType(field.Value))
}

func (g *Generator) deriveType(base, sub string) string {
	bracket := strings.IndexByte(base, '[')

	if bracket == -1 {
		return base + sub
	}

	return base[:bracket] + sub + base[bracket:]
}

func (g *Generator) generateServerStructs(f *j.File, t *DescInterface) error {
	// Generate the Args and Results structs

	f.Comment("Server structs for " + t.Name)

	for _, m := range t.Method {
		ptn := private(t.Name) + capitalize(m.Name)
		tn := capitalize(t.Name) + capitalize(m.Name)

		decl, _ := t.addGeneric(ptn + "ArgsData")

		f.Type().Add(decl).StructFunc(func(gr *j.Group) {
			for idx, p := range m.Parameters {
				if g.ti(p.Type).isInterface {
					gr.Id(toCamal(p.Name)).Op("*").Qual("miren.dev/runtime/pkg/rpc", "Capability").Tag(map[string]string{
						"cbor": fmt.Sprintf("%d,keyasint,omitempty", idx),
						"json": p.Name + ",omitempty",
					})
				} else if p.Type == "bytes" {
					gr.Id(toCamal(p.Name)).Op("*").Index().Byte().Tag(map[string]string{
						"cbor": fmt.Sprintf("%d,keyasint,omitempty", idx),
						"json": p.Name + ",omitempty",
					})
				} else if p.Type == "list" {
					if g.ti(p.Element).isMessage {
						gr.Id(toCamal(p.Name)).Op("*").Index().Op("*").Id(p.Element).Tag(map[string]string{
							"cbor": fmt.Sprintf("%d,keyasint,omitempty", idx),
							"json": p.Name + ",omitempty",
						})
					} else {
						gr.Id(toCamal(p.Name)).Op("*").Index().Add(g.properType(p.Element)).Tag(map[string]string{
							"cbor": fmt.Sprintf("%d,keyasint,omitempty", idx),
							"json": p.Name + ",omitempty",
						})
					}
				} else if p.Type == "map" {
					gr.Id(toCamal(p.Name)).Op("*").Map(g.properType(p.Key)).Add(g.properType(p.Value)).Tag(map[string]string{
						"cbor": fmt.Sprintf("%d,keyasint,omitempty", idx),
						"json": p.Name + ",omitempty",
					})
				} else {
					gr.Id(capitalize(p.Name)).Op("*").Add(g.properType(p.Type)).Tag(map[string]string{
						"cbor": fmt.Sprintf("%d,keyasint,omitempty", idx),
						"json": p.Name + ",omitempty",
					})
				}
			}
		})

		f.Line()

		_, privateArgs := t.addGeneric(ptn + "ArgsData")
		decl, name := t.addGeneric(tn + "Args")

		f.Type().Add(decl).StructFunc(func(g *j.Group) {
			g.Id("call").Qual("miren.dev/runtime/pkg/rpc", "Call")
			g.Id("data").Add(privateArgs)
		})

		for idx, p := range m.Parameters {
			g.readForField(f,
				&DescType{Type: tn + "Args", Generic: t.Generic},
				&DescField{
					Name:    p.Name,
					Type:    p.Type,
					Element: p.Element,
					Key:     p.Key,
					Value:   p.Value,
					Index:   idx,
				},
			)
		}

		f.Line()

		g.generateMarshalers(f, name.GoString())

		f.Line()

		decl, privateResults := t.addGeneric(ptn + "ResultsData")

		f.Type().Add(decl).StructFunc(func(gr *j.Group) {
			for idx, p := range m.Results {
				if g.ti(p.Type).isInterface {
					gr.Id(capitalize(p.Name)).Op("*").Qual("miren.dev/runtime/pkg/rpc", "Capability").Tag(map[string]string{
						"cbor": fmt.Sprintf("%d,keyasint,omitempty", idx),
						"json": p.Name + ",omitempty",
					})
				} else if p.Type == "bytes" {
					gr.Id(capitalize(p.Name)).Op("*").Index().Byte().Tag(map[string]string{
						"cbor": fmt.Sprintf("%d,keyasint,omitempty", idx),
						"json": p.Name + ",omitempty",
					})
				} else if p.Type == "list" {
					if g.ti(p.Element).isMessage {
						gr.Id(capitalize(p.Name)).Op("*").Index().Op("*").Id(p.Element).Tag(map[string]string{
							"cbor": fmt.Sprintf("%d,keyasint,omitempty", idx),
							"json": p.Name + ",omitempty",
						})
					} else {
						gr.Id(capitalize(p.Name)).Op("*").Index().Id(p.Element).Tag(map[string]string{
							"cbor": fmt.Sprintf("%d,keyasint,omitempty", idx),
							"json": p.Name + ",omitempty",
						})
					}
				} else if p.Type == "map" {
					gr.Id(capitalize(p.Name)).Op("*").Map(g.properType(p.Key)).Add(g.properType(p.Value)).Tag(map[string]string{
						"cbor": fmt.Sprintf("%d,keyasint,omitempty", idx),
						"json": p.Name + ",omitempty",
					})
				} else {
					gr.Id(capitalize(p.Name)).Op("*").Add(g.properType(p.Type)).Tag(map[string]string{
						"cbor": fmt.Sprintf("%d,keyasint,omitempty", idx),
						"json": p.Name + ",omitempty",
					})
				}
			}
		})

		f.Line()

		decl, name = t.addGeneric(tn + "Results")

		f.Type().Add(decl).StructFunc(func(g *j.Group) {
			g.Id("call").Qual("miren.dev/runtime/pkg/rpc", "Call")
			g.Id("data").Add(privateResults)
		})

		for idx, p := range m.Results {
			g.writeForField(f,
				&DescType{Type: tn + "Results", Generic: t.Generic},
				&DescField{
					Name:    p.Name,
					Type:    p.Type,
					Element: p.Element,
					Key:     p.Key,
					Value:   p.Value,
					Index:   idx,
				},
			)
		}

		f.Line()

		g.generateMarshalers(f, name.GoString())

		f.Line()
	}

	return nil
}

func (g *Generator) readForField(f *j.File, t *DescType, field *DescField) {
	name := toCamal(field.Name)
	fname := toCamal(name)
	expName := capitalize(t.Type)

	_, recv := t.addGeneric(expName)

	switch field.Type {
	case "bool":
		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id("Has" + fname).Params().Bool().Block(
			j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
		)

		f.Line()

		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id(fname).Params().Bool().Block(
			j.If(j.Id("v").Dot("data").Dot(name).Op("==").Nil()).Block(
				j.Return(j.Lit(false)),
			),
			j.Return(j.Op("*").Id("v").Dot("data").Dot(name)),
		)

		f.Line()
	case "uint32", "int32", "uint64", "int64", "float32", "float64":
		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id("Has" + fname).Params().Bool().Block(
			j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
		)

		f.Line()

		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id(fname).Params().Id(field.Type).Block(
			j.If(j.Id("v").Dot("data").Dot(name).Op("==").Nil()).Block(
				j.Return(j.Lit(0)),
			),
			j.Return(j.Op("*").Id("v").Dot("data").Dot(name)),
		)

		f.Line()

	case "bytes":
		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id("Has" + fname).Params().Bool().Block(
			j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
		)

		f.Line()

		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id(fname).Params().Index().Byte().Block(
			j.If(j.Id("v").Dot("data").Dot(name).Op("==").Nil()).Block(
				j.Return(j.Nil()),
			),
			j.Return(j.Op("*").Id("v").Dot("data").Dot(name)),
		)

		f.Line()
	case "string":
		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id("Has" + fname).Params().Bool().Block(
			j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
		)

		f.Line()

		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id(fname).Params().String().Block(
			j.If(j.Id("v").Dot("data").Dot(name).Op("==").Nil()).Block(
				j.Return(j.Lit("")),
			),
			j.Return(j.Op("*").Id("v").Dot("data").Dot(name)),
		)

		f.Line()
	case "list":
		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id("Has" + fname).Params().Bool().Block(
			j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
		)

		f.Line()

		if g.ti(field.Element).isMessage {
			f.Func().Params(
				j.Id("v").Op("*").Add(recv),
			).Id(fname).Params().Index().Op("*").Id(field.Element).Block(
				j.If(j.Id("v").Dot("data").Dot(name).Op("==").Nil()).Block(
					j.Return(j.Nil()),
				),
				j.Return(j.Op("*").Id("v").Dot("data").Dot(name)),
			)
		} else {
			f.Func().Params(
				j.Id("v").Op("*").Add(recv),
			).Id(fname).Params().Index().Id(field.Element).Block(
				j.If(j.Id("v").Dot("data").Dot(name).Op("==").Nil()).Block(
					j.Return(j.Nil()),
				),
				j.Return(j.Op("*").Id("v").Dot("data").Dot(name)),
			)
		}

		f.Line()
	case "map":
		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id("Has" + fname).Params().Bool().Block(
			j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
		)

		f.Line()

		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id(fname).Params().Add(g.mapType(field)).Block(
			j.If(j.Id("v").Dot("data").Dot(name).Op("==").Nil()).Block(
				j.Return(j.Nil()),
			),
			j.Return(j.Op("*").Id("v").Dot("data").Dot(name)),
		)

		f.Line()
	default:
		if g.ti(field.Type).isInterface {
			f.Func().Params(
				j.Id("v").Op("*").Add(recv),
			).Id("Has" + fname).Params().Bool().Block(
				j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
			)

			f.Line()

			f.Func().Params(
				j.Id("v").Op("*").Add(recv),
			).Id(fname).Params().Op("*").Id(g.deriveType(field.Type, "Client")).Block(
				j.If(j.Id("v").Dot("data").Dot(name).Op("==").Nil()).Block(
					j.Return(j.Nil()),
				),

				j.Return(
					j.Op("&").Id(g.deriveType(field.Type, "Client")).Values(
						j.Id("Client").Op(":").Id("v").Dot("call").Dot("NewClient").Call(
							j.Id("v").Dot("data").Dot(name),
						),
					),
				),
			)

			f.Line()
			return
		}

		if slices.Contains(t.Generic, field.Type) {
			f.Func().Params(
				j.Id("v").Op("*").Add(recv),
			).Id("Has" + fname).Params().Bool().Block(
				j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
			)

			f.Line()

			f.Func().Params(
				j.Id("v").Op("*").Add(recv),
			).Id(fname).Params().Id(field.Type).Block(
				j.If(j.Id("v").Dot("data").Dot(name).Op("==").Nil()).Block(
					j.Return(j.Qual("miren.dev/runtime/pkg/rpc", "Zero").Index(j.Id(field.Type)).Call())),
				j.Return(j.Op("*").Id("v").Dot("data").Dot(name)),
			)

			f.Line()
			return
		}

		if g.ti(field.Type).isMessage {
			f.Func().Params(
				j.Id("v").Op("*").Add(recv),
			).Id("Has" + fname).Params().Bool().Block(
				j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
			)

			f.Line()

			f.Func().Params(
				j.Id("v").Op("*").Add(recv),
			).Id(fname).Params().Op("*").Add(g.properType(field.Type)).Block(
				j.Return(j.Id("v").Dot("data").Dot(name)),
			)

		} else {
			f.Func().Params(
				j.Id("v").Op("*").Add(recv),
			).Id("Has" + fname).Params().Bool().Block(
				j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
			)

			f.Line()

			// Check if the type is nillable (slice or pointer) to determine if we can return nil
			isNillable := strings.HasPrefix(field.Type, "[]") || strings.HasPrefix(field.Type, "*")

			if isNillable {
				f.Func().Params(
					j.Id("v").Op("*").Add(recv),
				).Id(fname).Params().Add(g.properType(field.Type)).Block(
					j.If(j.Id("v").Dot("data").Dot(name).Op("==").Nil()).Block(
						j.Return(j.Nil()),
					),
					j.Return(j.Op("*").Id("v").Dot("data").Dot(name)),
				)
			} else {
				f.Func().Params(
					j.Id("v").Op("*").Add(recv),
				).Id(fname).Params().Add(g.properType(field.Type)).Block(
					j.Return(j.Op("*").Id("v").Dot("data").Dot(name)),
				)
			}
		}

		f.Line()

	}

	f.Line()
}

func (g *Generator) writeForField(f *j.File, t *DescType, field *DescField) {
	name := toCamal(field.Name)
	pname := private(field.Name)
	fname := toCamal(name)
	expName := capitalize(t.Type)

	_, recv := t.addGeneric(expName)

	switch field.Type {
	case "bool":
		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id("Set" + fname).Params(
			j.Id(pname).Bool(),
		).Block(
			j.Id("v").Dot("data").Dot(name).Op("=").Op("&").Id(pname),
		)

	case "uint32", "int32", "uint64", "int64", "float32", "float64":
		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id("Set" + fname).Params(
			j.Id(pname).Id(field.Type),
		).Block(
			j.Id("v").Dot("data").Dot(name).Op("=").Op("&").Id(pname),
		)

	case "bytes":
		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id("Set"+fname).Params(
			j.Id(pname).Index().Byte(),
		).Block(
			j.Id("x").Op(":=").Id("slices").Dot("Clone").Call(j.Id(pname)),
			j.Id("v").Dot("data").Dot(name).Op("=").Op("&").Id("x"),
		)

	case "string":
		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id("Set" + fname).Params(
			j.Id(pname).String(),
		).Block(
			j.Id("v").Dot("data").Dot(name).Op("=").Op("&").Id(pname),
		)

	case "list":
		if g.ti(field.Element).isMessage {
			f.Func().Params(
				j.Id("v").Op("*").Add(recv),
			).Id("Set"+fname).Params(
				j.Id(pname).Index().Op("*").Id(field.Element),
			).Block(
				j.Id("x").Op(":=").Id("slices").Dot("Clone").Call(j.Id(pname)),
				j.Id("v").Dot("data").Dot(name).Op("=").Op("&").Id("x"),
			)
		} else {
			f.Func().Params(
				j.Id("v").Op("*").Add(recv),
			).Id("Set"+fname).Params(
				j.Id(pname).Index().Id(field.Element),
			).Block(
				j.Id("x").Op(":=").Id("slices").Dot("Clone").Call(j.Id(pname)),
				j.Id("v").Dot("data").Dot(name).Op("=").Op("&").Id("x"),
			)
		}

	case "map":
		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id("Set"+fname).Params(
			j.Id(pname).Add(g.mapType(field)),
		).Block(
			j.Id("x").Op(":=").Qual("maps", "Clone").Call(j.Id(pname)),
			j.Id("v").Dot("data").Dot(name).Op("=").Op("&").Id("x"),
		)

	default:
		if g.ti(field.Type).isInterface {
			f.Func().Params(
				j.Id("v").Op("*").Add(recv),
			).Id("Set" + fname).Params(
				j.Id(pname).Id(field.Type),
			).Block(
				j.Id("v").Dot("data").Dot(name).Op("=").Id("v").Dot("call").Dot("NewCapability").CallFunc(func(gr *j.Group) {
					if g.isImported(field.Type) {
						iname, tname := g.splitType(field.Type)
						gr.Qual(g.Imports[iname].Import, "Adapt"+tname).Call(j.Id(pname))
					} else {
						gr.Id("Adapt" + field.Type).Call(j.Id(pname))
					}
				}),
			)

			f.Line()

			return
		}

		if slices.Contains(t.Generic, field.Type) {
			f.Func().Params(
				j.Id("v").Op("*").Add(recv),
			).Id("Set" + fname).Params(
				j.Id(pname).Add(g.properType(field.Type)),
			).Block(
				j.Id("v").Dot("data").Dot(name).Op("=").Op("&").Id(pname),
			)
			return
		}

		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id("Set" + fname).Params(
			j.Id(pname).Op("*").Add(g.properType(field.Type)),
		).Block(
			j.Id("v").Dot("data").Dot(name).Op("=").Id(pname),
		)
	}

	f.Line()
}

// Helper to generate the correct type for a union field
func (g *Generator) typeForUnion(u UnionField) j.Code {
	switch u.Type {
	case "bool", "uint32", "int32", "uint64", "int64", "float32", "float64", "string":
		return j.Id(u.Type)
	case "bytes":
		return j.Index().Byte()
	case "list":
		return j.Index().Id(u.Element)
	default:
		if g.ti(u.Type).isInterface {
			return j.Id(u.Type)
		}
		return j.Op("*").Add(g.properType(u.Type))
	}
}

func (g *Generator) generateUnionInterface(f *j.File, typ, name string, fields []UnionField) {
	interfaceName := capitalize(typ) + capitalize(name)
	f.Type().Id(interfaceName).InterfaceFunc(func(gr *j.Group) {
		gr.Id("Which").Params().String()
		for _, field := range fields {
			fieldName := toCamal(field.Name)
			gr.Id(fieldName).Params().Add(g.typeForUnion(field))
			gr.Id("Set" + fieldName).Params(g.typeForUnion(field))
		}
	})
	f.Line()
}

func (g *Generator) generateUnionStruct(f *j.File, typ, name string, fields []UnionField) {
	structName := private(typ) + capitalize(name)

	// Generate the struct
	f.Type().Id(structName).StructFunc(func(gr *j.Group) {
		for _, field := range fields {
			fieldType := g.typeForUnion(field)
			gr.Id("U_" + toCamal(field.Name)).Op("*").Add(fieldType).Tag(map[string]string{
				"cbor": fmt.Sprintf("%d,keyasint,omitempty", field.Index),
				"json": toSnake(field.Name) + ",omitempty",
			})
		}
	})
	f.Line()

	// Generate Which method
	f.Func().Params(
		j.Id("v").Op("*").Id(structName),
	).Id("Which").Params().String().BlockFunc(func(g *j.Group) {
		for _, field := range fields {
			g.If(j.Id("v").Dot("U_" + toCamal(field.Name)).Op("!=").Nil()).Block(
				j.Return(j.Lit(field.Name)),
			)
		}
		g.Return(j.Lit(""))
	})
	f.Line()

	// Generate getters and setters
	for _, field := range fields {
		fieldName := toCamal(field.Name)
		methodName := fieldName

		fieldName = "U_" + fieldName

		// Getter
		f.Func().Params(
			j.Id("v").Op("*").Id(structName),
		).Id(methodName).Params().Add(g.typeForUnion(field)).Block(
			j.If(j.Id("v").Dot(fieldName).Op("==").Nil()).Block(
				j.Return(g.zeroValue(field)),
			),
			j.Return(j.Op("*").Id("v").Dot(fieldName)),
		)
		f.Line()

		// Setter
		f.Func().Params(
			j.Id("v").Op("*").Id(structName),
		).Id("Set" + methodName).Params(
			j.Id("val").Add(g.typeForUnion(field)),
		).BlockFunc(func(g *j.Group) {
			// Clear all other fields
			for _, other := range fields {
				if other.Name != field.Name {
					g.Id("v").Dot("U_" + toCamal(other.Name)).Op("=").Nil()
				}
			}

			// Set the new value
			if field.Type == "list" {
				g.Id("x").Op(":=").Qual("slices", "Clone").Call(j.Id("val"))
				g.Id("v").Dot(fieldName).Op("=").Op("&").Id("x")

			} else {
				g.Id("v").Dot(fieldName).Op("=").Op("&").Id("val")
			}
		})
		f.Line()
	}
}

func (g *Generator) zeroValue(field UnionField) j.Code {
	switch field.Type {
	case "bool":
		return j.Lit(false)
	case "uint32", "int32", "uint64", "int64":
		return j.Lit(0)
	case "string":
		return j.Lit("")
	case "list":
		return j.Nil()
	default:
		return j.Nil()
	}
}

func (g *Generator) generateCompactStruct(f *j.File, t *DescType) error {
	rpc := "miren.dev/runtime/pkg/rpc"

	// Generate union interfaces and structs first
	for _, field := range t.Fields {
		if field.Type == "union" {
			g.generateUnionInterface(f, t.Type, field.Name, field.Union)
			g.generateUnionStruct(f, t.Type, field.Name, field.Union)
		}
	}

	expName := capitalize(t.Type)

	// Generate data struct with optional type parameter
	dataType, dataRecv := t.addGeneric(private(t.Type) + "Data")

	f.Type().Add(dataType).StructFunc(func(gr *j.Group) {
		gr.Id("_").Struct().Tag(map[string]string{
			"cbor": ",toarray",
		})

		for _, field := range t.Fields {
			switch field.Type {
			case "list":
				if g.ti(field.Element).isMessage {
					gr.Id(toCamal(field.Name)).Index().Id(field.Element).Tag(map[string]string{
						"cbor": fmt.Sprintf("%d,keyasint,omitempty", field.Index),
						"json": toSnake(field.Name) + ",omitempty",
					})
				} else {
					gr.Id(toCamal(field.Name)).Index().Id(field.Element).Tag(map[string]string{
						"cbor": fmt.Sprintf("%d,keyasint,omitempty", field.Index),
						"json": toSnake(field.Name) + ",omitempty",
					})
				}

			case "map":
				gr.Id(toCamal(field.Name)).Add(g.mapType(field)).Tag(map[string]string{
					"cbor": fmt.Sprintf("%d,keyasint,omitempty", field.Index),
					"json": toSnake(field.Name) + ",omitempty",
				})

			case "union":
				gr.Id(private(t.Type) + toCamal(field.Name))
			default:
				typ := g.properType(field.Type)

				if field.isInterface {
					typ = j.Op("*").Qual(rpc, "Capability")
				}

				gr.Id(toCamal(field.Name)).Add(typ).Tag(map[string]string{
					"cbor": fmt.Sprintf("%d,keyasint,omitempty", field.Index),
					"json": toSnake(field.Name) + ",omitempty",
				})
			}
		}
	})

	f.Line()

	structType, recv := t.addGeneric(expName)

	f.Type().Add(structType).StructFunc(func(g *j.Group) {
		if t.includeCall {
			g.Id("call").Qual(rpc, "Call")
		}

		g.Id("data").Add(dataRecv)
	})

	f.Line()

	for _, field := range t.Fields {
		name := toCamal(field.Name)
		pname := private(field.Name)
		fname := toCamal(name)

		switch field.Type {
		case "bool":
			if t.Readable() {
				f.Func().Params(
					j.Id("v").Op("*").Add(recv),
				).Id("Has" + fname).Params().Bool().Block(
					j.Return(j.True()),
				)

				f.Line()

				f.Func().Params(
					j.Id("v").Op("*").Add(recv),
				).Id(fname).Params().Bool().Block(
					j.Return(j.Id("v").Dot("data").Dot(name)),
				)

				f.Line()
			}

			if t.Writeable() {
				f.Func().Params(
					j.Id("v").Op("*").Add(recv),
				).Id("Set" + fname).Params(
					j.Id(pname).Bool(),
				).Block(
					j.Id("v").Dot("data").Dot(name).Op("=").Id(pname),
				)
			}

		case "uint32", "int32", "uint64", "int64", "float32", "float64":
			if t.Readable() {
				f.Func().Params(
					j.Id("v").Op("*").Add(recv),
				).Id("Has" + fname).Params().Bool().Block(
					j.Return(j.True()),
				)

				f.Line()

				f.Func().Params(
					j.Id("v").Op("*").Add(recv),
				).Id(fname).Params().Add(g.properType(field.Type)).Block(
					j.Return(j.Id("v").Dot("data").Dot(name)),
				)

				f.Line()
			}

			if t.Writeable() {
				f.Func().Params(
					j.Id("v").Op("*").Add(recv),
				).Id("Set" + fname).Params(
					j.Id(pname).Add(g.properType(field.Type)),
				).Block(
					j.Id("v").Dot("data").Dot(name).Op("=").Id(pname),
				)
			}

		case "bytes":
			if t.Readable() {
				f.Func().Params(
					j.Id("v").Op("*").Add(recv),
				).Id("Has" + fname).Params().Bool().Block(
					j.Return(j.True()),
				)

				f.Line()

				f.Func().Params(
					j.Id("v").Op("*").Add(recv),
				).Id(fname).Params().String().Block(
					j.Return(j.Id("v").Dot("data").Dot(name)),
				)

				f.Line()
			}

			if t.Writeable() {
				f.Func().Params(
					j.Id("v").Op("*").Add(recv),
				).Id("Set"+fname).Params(
					j.Id(pname).String(),
				).Block(
					j.Id("x").Op(":=").Id("slices").Dot("Clone").Call(j.Id(pname)),
					j.Id("v").Dot("data").Dot(name).Op("=").Id("x"),
				)
			}
		case "string":
			if t.Readable() {
				f.Func().Params(
					j.Id("v").Op("*").Add(recv),
				).Id("Has" + fname).Params().Bool().Block(
					j.Return(j.True()),
				)

				f.Line()

				f.Func().Params(
					j.Id("v").Op("*").Add(recv),
				).Id(fname).Params().String().Block(
					j.Return(j.Id("v").Dot("data").Dot(name)),
				)

				f.Line()
			}

			if t.Writeable() {
				f.Func().Params(
					j.Id("v").Op("*").Add(recv),
				).Id("Set" + fname).Params(
					j.Id(pname).String(),
				).Block(
					j.Id("v").Dot("data").Dot(name).Op("=").Id(pname),
				)
			}

		case "list":
			if t.Readable() {
				f.Func().Params(
					j.Id("v").Op("*").Add(recv),
				).Id("Has" + fname).Params().Bool().Block(
					j.Return(j.True()),
				)

				f.Line()

				if g.ti(field.Element).isMessage {
					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id(fname).Params().Index().Id(field.Element).Block(
						j.Return(j.Id("v").Dot("data").Dot(name)),
					)
				} else {
					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id(fname).Params().Index().Id(field.Element).Block(
						j.Return(j.Id("v").Dot("data").Dot(name)),
					)
				}

				f.Line()
			}

			if t.Writeable() {
				if g.ti(field.Element).isMessage {
					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id("Set"+fname).Params(
						j.Id(pname).Index().Id(field.Element),
					).Block(
						j.Id("x").Op(":=").Id("slices").Dot("Clone").Call(j.Id(pname)),
						j.Id("v").Dot("data").Dot(name).Op("=").Id("x"),
					)
				} else {
					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id("Set"+fname).Params(
						j.Id(pname).Index().Id(field.Element),
					).Block(
						j.Id("x").Op(":=").Id("slices").Dot("Clone").Call(j.Id(pname)),
						j.Id("v").Dot("data").Dot(name).Op("=").Id("x"),
					)
				}
			}
		case "map":
			if t.Readable() {
				f.Func().Params(
					j.Id("v").Op("*").Add(recv),
				).Id("Has" + fname).Params().Bool().Block(
					j.Return(j.True()),
				)

				f.Line()

				f.Func().Params(
					j.Id("v").Op("*").Add(recv),
				).Id(fname).Params().Add(g.mapType(field)).Block(
					j.Return(j.Id("v").Dot("data").Dot(name)),
				)

				f.Line()
			}

			if t.Writeable() {
				f.Func().Params(
					j.Id("v").Op("*").Add(recv),
				).Id("Set" + fname).Params(
					j.Id(pname).Add(g.mapType(field)),
				).Block(
					j.Id("v").Dot("data").Dot(name).Op("=").Qual("maps", "Clone").Call(j.Id(pname)),
				)
			}
		case "union":
			f.Func().Params(
				j.Id("v").Op("*").Add(recv),
			).Id(fname).Params().Id(capitalize(t.Type) + capitalize(name)).Block(
				j.Return(j.Op("&").Id("v").Dot("data").Dot(private(t.Type) + capitalize(name))),
			)

			f.Line()

		default:
			if g.ti(field.Type).isInterface {
				if t.Readable() {
					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id("Has" + fname).Params().Bool().Block(
						j.Return(j.True()),
					)

					f.Line()

					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id(fname).Params().Add(g.properType(field.Type)).Block(
						j.Return(j.Id("v").Dot("data").Dot(name)),
					)

					f.Line()
				}

				if t.Writeable() {
					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id("Set" + fname).Params(
						j.Id(pname).Add(g.properType(field.Type)),
					).Block(
						j.Id("v").Dot("data").Dot(name).Op("=").Id("v").Dot("call").Dot("NewCapability").CallFunc(func(gr *j.Group) {
							if g.isImported(field.Type) {
								iname, tname := g.splitType(field.Type)
								gr.Qual(g.Imports[iname].Import, "Adapt"+tname).Call(j.Id(pname))
							} else {
								j.Id("Adapt" + field.Type).Call(j.Id(pname))
							}
						}),
					)
				}

				f.Line()

				continue
			}

			if t.Readable() {
				f.Func().Params(
					j.Id("v").Op("*").Add(recv),
				).Id("Has" + fname).Params().Bool().Block(
					j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
				)

				f.Line()

				f.Func().Params(
					j.Id("v").Op("*").Add(recv),
				).Id(fname).Params().Op("*").Add(g.properType(field.Type)).Block(
					j.Return(j.Id("v").Dot("data").Dot(name)),
				)

				f.Line()
			}

			if t.Writeable() {
				f.Func().Params(
					j.Id("v").Op("*").Add(recv),
				).Id("Set" + fname).Params(
					j.Id(pname).Op("*").Add(g.properType(field.Type)),
				).Block(
					j.Id("v").Dot("data").Dot(name).Op("=").Id(pname),
				)
			}
		}

		f.Line()
	}

	g.generateMarshalers(f, recv.GoString())
	return nil
}

func (g *Generator) generateStruct(f *j.File) error {
	f.ImportName("github.com/fxamacker/cbor/v2", "cbor")
	rpc := "miren.dev/runtime/pkg/rpc"

	for _, t := range g.Types {
		if t.Compact {
			err := g.generateCompactStruct(f, t)
			if err != nil {
				return err
			}
			continue
		}
		// Generate union interfaces and structs first
		for _, field := range t.Fields {
			if field.Type == "union" {
				g.generateUnionInterface(f, t.Type, field.Name, field.Union)
				g.generateUnionStruct(f, t.Type, field.Name, field.Union)
			}
		}

		expName := capitalize(t.Type)

		// Generate data struct with optional type parameter
		dataType, dataRecv := t.addGeneric(private(t.Type) + "Data")

		f.Type().Add(dataType).StructFunc(func(gr *j.Group) {
			if t.Compact {
				gr.Id("_").Struct().Tag(map[string]string{
					"cbor": ",toarray",
				})
			}

			for _, field := range t.Fields {
				switch field.Type {
				case "list":
					if g.ti(field.Element).isMessage {
						gr.Id(toCamal(field.Name)).Op("*").Index().Op("*").Id(field.Element).Tag(map[string]string{
							"cbor": fmt.Sprintf("%d,keyasint,omitempty", field.Index),
							"json": toSnake(field.Name) + ",omitempty",
						})
					} else {
						gr.Id(toCamal(field.Name)).Op("*").Index().Id(field.Element).Tag(map[string]string{
							"cbor": fmt.Sprintf("%d,keyasint,omitempty", field.Index),
							"json": toSnake(field.Name) + ",omitempty",
						})
					}

				case "map":
					gr.Id(toCamal(field.Name)).Op("*").Add(g.mapType(field)).Tag(map[string]string{
						"cbor": fmt.Sprintf("%d,keyasint,omitempty", field.Index),
						"json": toSnake(field.Name) + ",omitempty",
					})

				case "union":
					gr.Id(private(t.Type) + toCamal(field.Name))
				default:
					typ := j.Op("*").Add(g.properType(field.Type))

					if field.isInterface {
						typ = j.Op("*").Qual(rpc, "Capability")
					}

					gr.Id(toCamal(field.Name)).Add(typ).Tag(map[string]string{
						"cbor": fmt.Sprintf("%d,keyasint,omitempty", field.Index),
						"json": toSnake(field.Name) + ",omitempty",
					})
				}
			}
		})

		f.Line()

		structType, recv := t.addGeneric(expName)

		f.Type().Add(structType).StructFunc(func(g *j.Group) {
			if t.includeCall {
				g.Id("call").Qual(rpc, "Call")
			}

			g.Id("data").Add(dataRecv)
		})

		f.Line()

		for _, field := range t.Fields {
			name := toCamal(field.Name)
			pname := private(field.Name)
			fname := toCamal(name)

			switch field.Type {
			case "bool":
				if t.Readable() {
					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id("Has" + fname).Params().Bool().Block(
						j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
					)

					f.Line()

					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id(fname).Params().Bool().Block(
						j.If(j.Id("v").Dot("data").Dot(name).Op("==").Nil()).Block(
							j.Return(j.Lit(false)),
						),
						j.Return(j.Op("*").Id("v").Dot("data").Dot(name)),
					)

					f.Line()
				}

				if t.Writeable() {
					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id("Set" + fname).Params(
						j.Id(pname).Bool(),
					).Block(
						j.Id("v").Dot("data").Dot(name).Op("=").Op("&").Id(pname),
					)
				}

			case "uint32", "int32", "uint64", "int64", "float32", "float64":
				if t.Readable() {
					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id("Has" + fname).Params().Bool().Block(
						j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
					)

					f.Line()

					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id(fname).Params().Add(g.properType(field.Type)).Block(
						j.If(j.Id("v").Dot("data").Dot(name).Op("==").Nil()).Block(
							j.Return(j.Lit(0)),
						),
						j.Return(j.Op("*").Id("v").Dot("data").Dot(name)),
					)

					f.Line()
				}

				if t.Writeable() {
					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id("Set" + fname).Params(
						j.Id(pname).Add(g.properType(field.Type)),
					).Block(
						j.Id("v").Dot("data").Dot(name).Op("=").Op("&").Id(pname),
					)
				}

			case "bytes":
				if t.Readable() {
					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id("Has" + fname).Params().Bool().Block(
						j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
					)

					f.Line()

					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id(fname).Params().String().Block(
						j.If(j.Id("v").Dot("data").Dot(name).Op("==").Nil()).Block(
							j.Return(j.Nil()),
						),
						j.Return(j.Op("*").Id("v").Dot("data").Dot(name)),
					)

					f.Line()
				}

				if t.Writeable() {
					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id("Set"+fname).Params(
						j.Id(pname).String(),
					).Block(
						j.Id("x").Op(":=").Id("slices").Dot("Clone").Call(j.Id(pname)),
						j.Id("v").Dot("data").Dot(name).Op("=").Op("&").Id("x"),
					)
				}
			case "string":
				if t.Readable() {
					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id("Has" + fname).Params().Bool().Block(
						j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
					)

					f.Line()

					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id(fname).Params().String().Block(
						j.If(j.Id("v").Dot("data").Dot(name).Op("==").Nil()).Block(
							j.Return(j.Lit("")),
						),
						j.Return(j.Op("*").Id("v").Dot("data").Dot(name)),
					)

					f.Line()
				}

				if t.Writeable() {
					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id("Set" + fname).Params(
						j.Id(pname).String(),
					).Block(
						j.Id("v").Dot("data").Dot(name).Op("=").Op("&").Id(pname),
					)
				}

			case "list":
				if t.Readable() {
					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id("Has" + fname).Params().Bool().Block(
						j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
					)

					f.Line()

					if g.ti(field.Element).isMessage {
						f.Func().Params(
							j.Id("v").Op("*").Add(recv),
						).Id(fname).Params().Index().Op("*").Id(field.Element).Block(
							j.If(j.Id("v").Dot("data").Dot(name).Op("==").Nil()).Block(
								j.Return(j.Nil()),
							),
							j.Return(j.Op("*").Id("v").Dot("data").Dot(name)),
						)
					} else {
						f.Func().Params(
							j.Id("v").Op("*").Add(recv),
						).Id(fname).Params().Index().Id(field.Element).Block(
							j.If(j.Id("v").Dot("data").Dot(name).Op("==").Nil()).Block(
								j.Return(j.Nil()),
							),
							j.Return(j.Op("*").Id("v").Dot("data").Dot(name)),
						)
					}

					f.Line()
				}

				if t.Writeable() {
					if g.ti(field.Element).isMessage {
						f.Func().Params(
							j.Id("v").Op("*").Add(recv),
						).Id("Set"+fname).Params(
							j.Id(pname).Index().Op("*").Id(field.Element),
						).Block(
							j.Id("x").Op(":=").Id("slices").Dot("Clone").Call(j.Id(pname)),
							j.Id("v").Dot("data").Dot(name).Op("=").Op("&").Id("x"),
						)
					} else {
						f.Func().Params(
							j.Id("v").Op("*").Add(recv),
						).Id("Set"+fname).Params(
							j.Id(pname).Index().Id(field.Element),
						).Block(
							j.Id("x").Op(":=").Id("slices").Dot("Clone").Call(j.Id(pname)),
							j.Id("v").Dot("data").Dot(name).Op("=").Op("&").Id("x"),
						)
					}
				}
			case "map":
				if t.Readable() {
					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id("Has" + fname).Params().Bool().Block(
						j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
					)

					f.Line()

					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id(fname).Params().Add(g.mapType(field)).Block(
						j.If(j.Id("v").Dot("data").Dot(name).Op("==").Nil()).Block(
							j.Return(j.Nil()),
						),
						j.Return(j.Op("*").Id("v").Dot("data").Dot(name)),
					)

					f.Line()
				}

				if t.Writeable() {
					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id("Set"+fname).Params(
						j.Id(pname).Add(g.mapType(field)),
					).Block(
						j.Id("x").Op(":=").Qual("maps", "Clone").Call(j.Id(pname)),
						j.Id("v").Dot("data").Dot(name).Op("=").Op("&").Id("x"),
					)
				}
			case "union":
				f.Func().Params(
					j.Id("v").Op("*").Add(recv),
				).Id(fname).Params().Id(capitalize(t.Type) + capitalize(name)).Block(
					j.Return(j.Op("&").Id("v").Dot("data").Dot(private(t.Type) + capitalize(name))),
				)

				f.Line()

			default:
				if g.ti(field.Type).isInterface {
					if t.Readable() {
						f.Func().Params(
							j.Id("v").Op("*").Add(recv),
						).Id("Has" + fname).Params().Bool().Block(
							j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Lit("")),
						)

						f.Line()

						f.Func().Params(
							j.Id("v").Op("*").Add(recv),
						).Id(fname).Params().Add(g.properType(field.Type)).Block(
							j.If(j.Id("v").Dot("data").Dot(name).Op("==").Nil()).Block(
								j.Return(j.Nil()),
							),

							j.Return(j.Op("*").Id("v").Dot("data").Dot(name)),
						)

						f.Line()
					}

					if t.Writeable() {
						f.Func().Params(
							j.Id("v").Op("*").Add(recv),
						).Id("Set" + fname).Params(
							j.Id(pname).Add(g.properType(field.Type)),
						).Block(
							j.Id("v").Dot("data").Dot(name).Op("=").Id("v").Dot("call").Dot("NewCapability").CallFunc(func(gr *j.Group) {
								if g.isImported(field.Type) {
									iname, tname := g.splitType(field.Type)
									gr.Qual(g.Imports[iname].Import, "Adapt"+tname).Call(j.Id(pname))
								} else {
									j.Id("Adapt" + field.Type).Call(j.Id(pname))
								}
							}),
						)
					}

					f.Line()

					continue
				}

				if t.Readable() {
					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id("Has" + fname).Params().Bool().Block(
						j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
					)

					f.Line()

					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id(fname).Params().Op("*").Add(g.properType(field.Type)).Block(
						j.Return(j.Id("v").Dot("data").Dot(name)),
					)

					f.Line()
				}

				if t.Writeable() {
					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id("Set" + fname).Params(
						j.Id(pname).Op("*").Add(g.properType(field.Type)),
					).Block(
						j.Id("v").Dot("data").Dot(name).Op("=").Id(pname),
					)
				}
			}

			f.Line()
		}

		g.generateMarshalers(f, recv.GoString())
	}
	return nil
}

func (g *Generator) generateMarshalers(f *j.File, expName string) {
	recv := j.Id(expName)

	f.Func().Params(
		j.Id("v").Op("*").Add(recv),
	).Id("MarshalCBOR").Params().Params(j.Index().Byte(), j.Error()).Block(
		j.Return(j.Qual("github.com/fxamacker/cbor/v2", "Marshal").Call(j.Id("v").Dot("data"))),
	)

	f.Line()

	f.Func().Params(
		j.Id("v").Op("*").Add(recv),
	).Id("UnmarshalCBOR").Params(
		j.Id("data").Index().Byte(),
	).Error().Block(
		j.Return(j.Qual("github.com/fxamacker/cbor/v2", "Unmarshal").Call(j.Id("data"), j.Op("&").Id("v").Dot("data"))),
	)

	f.Line()

	f.Func().Params(
		j.Id("v").Op("*").Add(recv),
	).Id("MarshalJSON").Params().Params(j.Index().Byte(), j.Error()).Block(
		j.Return(j.Qual("encoding/json", "Marshal").Call(j.Id("v").Dot("data"))),
	)

	f.Line()

	f.Func().Params(
		j.Id("v").Op("*").Add(recv),
	).Id("UnmarshalJSON").Params(
		j.Id("data").Index().Byte(),
	).Error().Block(
		j.Return(j.Qual("encoding/json", "Unmarshal").Call(j.Id("data"), j.Op("&").Id("v").Dot("data"))),
	)
}

func (g *Generator) generateClient(f *j.File, i *DescInterface) error {
	rpc := "miren.dev/runtime/pkg/rpc"

	expName := capitalize(i.Name) + "Client"

	clientType, recv := i.addGeneric(expName)

	f.Type().Add(clientType).Struct(
		j.Qual(rpc, "Client"),
	)

	f.Line()

	if len(i.Generic) > 0 {
		f.Func().Id("New" + expName).TypesFunc(func(gr *j.Group) {
			for _, g := range i.Generic {
				gr.Id(g).Any()
			}
		}).Params(j.Id("client").Qual(rpc, "Client")).Op("*").Add(recv).Block(
			j.Return(j.Op("&").Id(expName).TypesFunc(func(gr *j.Group) {
				for _, g := range i.Generic {
					gr.Id(g)
				}
			}).Values(
				j.Id("Client").Op(":").Id("client"),
			)),
		)
	} else {
		f.Func().Id("New" + expName).Params(j.Id("client").Qual(rpc, "Client")).Op("*").Add(recv).Block(
			j.Return(j.Op("&").Id(expName).Values(
				j.Id("Client").Op(":").Id("client"),
			)),
		)
	}

	f.Line()

	f.Func().Params(
		j.Id("c").Add(recv),
	).Id("Export").Params().Add(i.typeName(capitalize(i.Name))).Block(
		j.Return(j.Add(i.typeName("reexport" + capitalize(i.Name))).Values(
			j.Id("client").Op(":").Id("c").Dot("Client"))),
	)

	f.Line()

	for _, m := range i.Method {
		tn := expName + capitalize(m.Name)

		sname, _ := i.addGeneric(tn + "Results")

		f.Type().Add(sname).Struct(
			j.Id("client").Qual(rpc, "Client"),
			j.Id("data").Add(i.typeName(private(i.Name)+capitalize(m.Name)+"ResultsData")),
		)

		f.Line()

		for _, p := range m.Results {
			name := capitalize(p.Name)

			if g.ti(p.Type).isInterface {
				f.Func().Params(
					j.Id("v").Op("*").Add(i.typeName(tn + "Results")),
				).Id(name).Params().Op("*").Id(g.deriveType(p.Type, "Client")).Block(
					j.Return(j.Op("&").Id(g.deriveType(p.Type, "Client")).Values(
						j.Line().Id("Client").Op(":").Id("v").Dot("client").Dot("NewClient").Call(j.Id("v").Dot("data").Dot(name)),
						j.Line(),
					),
					),
				)
			} else {
				g.readForField(f,
					&DescType{Type: tn + "Results", Generic: i.Generic},
					&DescField{
						Name:    p.Name,
						Type:    p.Type,
						Element: p.Element,
						Key:     p.Key,
						Value:   p.Value,
						Index:   0,
					})
			}
			f.Line()
		}

		f.Func().Params(
			j.Id("v").Add(recv),
		).Id(capitalize(m.Name)).ParamsFunc(func(gr *j.Group) {
			gr.Id("ctx").Qual("context", "Context")

			for _, p := range m.Parameters {
				if g.ti(p.Type).isMessage {
					gr.Id(private(p.Name)).Op("*").Add(g.properType(p.Type))
				} else if p.Type == "bytes" {
					gr.Id(private(p.Name)).Index().Byte()
				} else if p.Type == "list" {
					if g.ti(p.Element).isMessage {
						gr.Id(private(p.Name)).Index().Op("*").Add(g.properType(p.Element))
					} else {
						gr.Id(private(p.Name)).Index().Id(p.Element)
					}
				} else if p.Type == "map" {
					gr.Id(private(p.Name)).Map(g.properType(p.Key)).Add(g.properType(p.Value))
				} else {
					gr.Id(private(p.Name)).Add(g.properType(p.Type))
				}
			}
		}).Params(j.Op("*").Add(i.typeName(tn+"Results")), j.Error()).BlockFunc(func(gr *j.Group) {
			gr.Id("args").Op(":= ").Add(i.typeName(capitalize(i.Name) + capitalize(m.Name) + "Args")).Values()

			hasCaps := false
			for _, p := range m.Parameters {
				if g.ti(p.Type).isInterface {
					gr.Id("caps").Op(":=").Map(j.Qual(rpc, "OID")).Op("*").Qual(rpc, "InlineCapability").Values()
					hasCaps = true
					break
				}
			}

			for _, p := range m.Parameters {
				if g.ti(p.Type).isInterface {
					gr.BlockFunc(func(gr *j.Group) {
						gr.List(j.Id("ic"), j.Id("oid"), j.Id("c")).Op(":=").Id("v").Dot("NewInlineCapability").
							CallFunc(func(gr *j.Group) {
								if g.isImported(p.Type) {
									iname, tname := g.splitType(p.Type)
									gr.Qual(g.Imports[iname].Import, "Adapt"+tname).Call(j.Id(p.Name))
								} else {
									gr.Id("Adapt" + capitalize(p.Type)).Call(j.Id(private(p.Name)))
								}
								gr.Id(private(p.Name))
							})
						gr.Id("args").Dot("data").Dot(toCamal(p.Name)).Op("=").Id("c")
						gr.Id("caps").Index(j.Id("oid")).Op("=").Id("ic")
					})
				} else if g.ti(p.Type).isMessage {
					gr.Id("args").Dot("data").Dot(toCamal(p.Name)).Op("=").Id(private(p.Name))
				} else if p.Type == "list" {
					gr.Id("x").Op(":=").Qual("slices", "Clone").Call(j.Id(private(p.Name)))
					gr.Id("args").Dot("data").Dot(toCamal(p.Name)).Op("=").Op("&").Id("x")
				} else if p.Type == "map" {
					gr.Id("x").Op(":=").Qual("maps", "Clone").Call(j.Id(private(p.Name)))
					gr.Id("args").Dot("data").Dot(toCamal(p.Name)).Op("=").Op("&").Id("x")
				} else {
					gr.Id("args").Dot("data").Dot(toCamal(p.Name)).Op("=").Op("&").Id(private(p.Name))
				}
			}

			gr.Line()

			gr.Var().Id("ret").Add(i.typeName(private(i.Name) + capitalize(m.Name) + "ResultsData"))

			gr.Line()

			if hasCaps {
				gr.Id("err").Op(":=").Id("v").Dot("CallWithCaps").Call(
					j.Id("ctx"),
					j.Lit(m.Name),
					j.Op("&").Id("args"),
					j.Op("&").Id("ret"),
					j.Id("caps"),
				)

			} else {
				gr.Id("err").Op(":=").Id("v").Dot("Call").Call(
					j.Id("ctx"),
					j.Lit(m.Name),
					j.Op("&").Id("args"),
					j.Op("&").Id("ret"),
				)
			}
			gr.If(j.Id("err").Op("!=").Nil()).Block(
				j.Return(j.Nil(), j.Id("err")),
			)

			gr.Line()

			gr.Return(j.Op("&").Add(i.typeName(tn+"Results")).Values(
				j.Id("client").Op(":").Id("v").Dot("Client"),
				j.Id("data").Op(":").Id("ret")),
				j.Nil(),
			)
		})

		f.Line()
	}

	return nil
}

func (g *Generator) generateInterfaces(f *j.File) error {
	rpc := "miren.dev/runtime/pkg/rpc"

	for _, i := range g.Interfaces {
		err := g.generateServerStructs(f, i)
		if err != nil {
			return err
		}

		expName := capitalize(i.Name)

		for _, m := range i.Method {

			tn := expName + capitalize(m.Name)

			decl, recv := i.addGeneric(tn)

			f.Type().Add(decl).Struct(
				j.Qual(rpc, "Call"),
				j.Id("args").Add(i.typeName(tn+"Args")),
				j.Id("results").Add(i.typeName(tn+"Results")),
			)

			f.Line()

			f.Func().Params(
				j.Id("t").Op("*").Add(recv),
			).Id("Args").Params().Op("*").Add(i.typeName(tn+"Args")).Block(
				j.Id("args").Op(":=").Op("&").Id("t").Dot("args"),
				j.If(j.Id("args").Dot("call").Op("!=").Nil()).Block(
					j.Return(j.Id("args")),
				),
				j.Id("args").Dot("call").Op("=").Id("t").Dot("Call"),
				j.Id("t").Dot("Call").Dot("Args").Call(j.Id("args")),
				j.Return(j.Id("args")),
			)

			f.Line()

			f.Func().Params(
				j.Id("t").Op("*").Add(recv),
			).Id("Results").Params().Op("*").Add(i.typeName(tn+"Results")).Block(
				j.Id("results").Op(":=").Op("&").Id("t").Dot("results"),
				j.If(j.Id("results").Dot("call").Op("!=").Nil()).Block(
					j.Return(j.Id("results")),
				),
				j.Id("results").Dot("call").Op("=").Id("t").Dot("Call"),
				j.Id("t").Dot("Call").Dot("Results").Call(j.Id("results")),
				j.Return(j.Id("results")),
			)

			f.Line()
		}

		interfaceType, _ := i.addGeneric(expName)

		f.Type().Add(interfaceType).InterfaceFunc(func(g *j.Group) {
			for _, m := range i.Method {
				methodName := capitalize(m.Name)

				g.Id(methodName).Params(
					j.Id("ctx").Qual("context", "Context"),
					j.Id("state").Op("*").Add(i.typeName(expName+capitalize(m.Name))),
				).Error()
			}
		})

		f.Line()

		reexportType, recv := i.addGeneric("reexport" + expName)

		f.Type().Add(reexportType).Struct(
			j.Id("client").Qual(rpc, "Client"),
		)

		for _, m := range i.Method {
			methodName := capitalize(m.Name)

			f.Func().Params(j.Add(recv)).Id(methodName).Params(
				j.Id("ctx").Qual("context", "Context"),
				j.Id("state").Op("*").Add(i.typeName(expName+capitalize(m.Name))),
			).Error().Block(
				j.Panic(j.Lit("not implemented")),
			)

			f.Line()
		}

		f.Func().Params(j.Id("t").Add(recv)).Id("CapabilityClient").
			Params().Params(j.Qual(rpc, "Client")).Block(
			j.Return(j.Id("t").Dot("client")),
		)

		f.Line()

		adaptName, _ := i.addGeneric("Adapt" + expName)

		f.Func().Add(adaptName).Params(
			j.Id("t").Add(i.typeName(expName)),
		).Op("*").Qual(rpc, "Interface").BlockFunc(func(g *j.Group) {
			g.Id("methods").Op(":=").Index().Qual(rpc, "Method").ValuesFunc(func(g *j.Group) {
				for _, m := range i.Method {
					methodName := capitalize(m.Name)

					g.Line().ValuesFunc(func(g *j.Group) {
						g.Line().Id("Name").Op(":").Lit(m.Name)
						g.Line().Id("InterfaceName").Op(":").Lit(i.Name)
						g.Line().Id("Index").Op(":").Lit(m.Index)
						g.Line().Id("Public").Op(":").Lit(m.Public)
						g.Line().Id("Params").Op(":").Index().String().ValuesFunc(func(g *j.Group) {
							for _, p := range m.Parameters {
								g.Lit(p.Name)
							}
						})
						if verb, bare, ok := httpBinding(i, m); ok {
							params := httpPathParams(bare)
							query := httpQueryParams(m, params)
							g.Line().Id("HTTP").Op(":").Op("&").Qual(rpc, "HTTPBinding").ValuesFunc(func(g *j.Group) {
								g.Line().Id("Verb").Op(":").Lit(verb)
								g.Line().Id("Path").Op(":").Lit(bare)
								g.Line().Id("Body").Op(":").Lit(m.HTTP.effectiveBody())
								g.Line().Id("PathParams").Op(":").Index().String().ValuesFunc(func(g *j.Group) {
									for _, p := range params {
										g.Lit(p)
									}
								})
								if len(query) > 0 {
									g.Line().Id("Query").Op(":").Index().Qual(rpc, "HTTPParam").ValuesFunc(func(g *j.Group) {
										for _, q := range query {
											g.Line().Values(
												j.Id("Name").Op(":").Lit(q.Name),
												j.Id("Kind").Op(":").Lit(q.Kind),
											)
										}
										g.Line()
									})
								}
								g.Line()
							})
						}
						g.Line().Id("Handler").Op(":").Func().Params(
							j.Id("ctx").Qual("context", "Context"),
							j.Id("call").Qual(rpc, "Call"),
						).Error().Block(
							j.Return(j.Id("t").Dot(methodName).Call(
								j.Id("ctx"),
								j.Op("&").Add(i.typeName(expName+toCamal(m.Name))).Values(j.Id("Call").Op(":").Id("call")),
							)))
						g.Line()
					})
				}

				g.Line()
			})

			g.Line()

			g.Return(j.Qual(rpc, "NewInterface").Call(j.Id("methods"), j.Id("t")))
		})

		f.Line()

		g.generateClient(f, i)
	}

	return nil
}

func (g *Generator) Generate(pkgName string) (string, error) {
	for _, t := range g.Types {
		err := t.Validate()
		if err != nil {
			return "", err
		}
	}

	if err := g.validateHTTP(); err != nil {
		return "", err
	}

	fmt.Println(pkgName)
	f := j.NewFile(pkgName)

	for name, imp := range g.Imports {
		f.ImportName(imp.Import, name)
	}

	err := g.generateStruct(f)
	if err != nil {
		return "", err
	}

	err = g.generateInterfaces(f)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer

	err = f.Render(&buf)
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

		return "", errors.New(sb.String())
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

		return "", errors.New(sb.String())
	}

	return string(code), nil
}

func (g *Generator) populateTypeInfo() error {
	for _, i := range g.Interfaces {
		name := i.Name
		idx := strings.IndexByte(name, '[')
		if idx != -1 {
			name = name[:idx]
		}
		g.typeInfo[name] = typeInfo{
			isInterface: true,
		}
	}

	for _, t := range g.Types {
		name := t.Type
		idx := strings.IndexByte(name, '[')
		if idx != -1 {
			name = name[:idx]
		}
		g.typeInfo[name] = typeInfo{
			isMessage: true,
		}
	}

	return nil
}

type typeInfo struct {
	isInterface bool
	isMessage   bool
}

type DescFile struct {
	Imports    map[string]Import `yaml:"imports"`
	Types      []*DescType       `yaml:"types"`
	Interfaces []*DescInterface  `yaml:"interfaces"`
}

const (
	TypeRW = iota
	TypeR
	TypeW
)

type DescType struct {
	Type        string       `yaml:"type"`
	Fields      []*DescField `yaml:"fields"`
	Compact     bool         `yaml:"compact,omitempty"`
	Generic     []string     `yaml:"generic,omitempty"`
	Constraints []string     `yaml:"constraints,omitempty"`

	access      int
	includeCall bool

	dataSize int
	pointers int

	userType *DescType
}

func (t *DescType) Validate() error {
	seen := map[int]struct{}{}

	for _, field := range t.Fields {
		if field.Type == "union" {
			for _, u := range field.Union {
				if _, ok := seen[u.Index]; ok {
					return fmt.Errorf("duplicate field index %d in union in type %s", u.Index, t.Type)
				}
				seen[u.Index] = struct{}{}
			}
		} else {
			if _, ok := seen[field.Index]; ok {
				return fmt.Errorf("duplicate field index %d in type %s", field.Index, t.Type)
			}
			seen[field.Index] = struct{}{}
		}

		if field.Type == "list" && field.Element == "" {
			return fmt.Errorf("field %q in type %s: list requires element", field.Name, t.Type)
		}
		if field.Type == "map" {
			if field.Key == "" || field.Value == "" {
				return fmt.Errorf("field %q in type %s: map requires key and value", field.Name, t.Type)
			}
		}
	}

	return nil
}

func (t *DescType) Readable() bool {
	return t.access == TypeR || t.access == TypeRW
}

func (t *DescType) Writeable() bool {
	return t.access == TypeW || t.access == TypeRW
}

var dataFields = map[string]int{
	"bool":    1,
	"int32":   4,
	"uint32":  4,
	"int64":   8,
	"uint64":  8,
	"float32": 4,
	"float64": 8,
}

func (t *DescType) CalculateOffsets(usertypes map[string]*DescType) {
	slices.SortFunc(t.Fields, func(a, b *DescField) int {
		return cmp.Compare(a.Index, b.Index)
	})

	var dataOffset int
	var wordOffset int

	for _, field := range t.Fields {
		align, ok := dataFields[field.Type]
		if !ok {
			continue
		}

		field.dataOffset = dataOffset
		field.wordOffset = wordOffset

		if dataOffset%align != 0 {
			dataOffset += (align - dataOffset%align)
		}

		switch field.Type {
		case "bool":
			dataOffset += 1
		case "uint32", "int32":
			dataOffset += 4
		case "uint64", "int64":
			dataOffset += 8
		case "float32":
			dataOffset += 4
		case "float64":
			dataOffset += 8
		}

		if dataOffset%8 == 0 {
			wordOffset++
		}
	}

	t.dataSize = dataOffset

	if dataOffset%8 != 0 {
		wordOffset++
	}

	// Ok, now do the ones that are pointers
	for _, field := range t.Fields {
		switch field.Type {
		case "string":
			field.wordOffset = wordOffset
			t.pointers++
			wordOffset++
		case "list":
			field.wordOffset = wordOffset
			t.pointers++
			wordOffset++
		case "map":
			field.wordOffset = wordOffset
			t.pointers++
			wordOffset++
		default:
			if ut, ok := usertypes[field.Type]; ok {
				field.wordOffset = wordOffset
				ut.userType = ut
				t.pointers++
				wordOffset++
			}
		}
	}
}

type DescField struct {
	Name  string `yaml:"name"`
	Type  string `yaml:"type"`
	Index int    `yaml:"index"`

	Element string       `yaml:"element"`
	Union   []UnionField `yaml:"union,omitempty"`
	Key     string       `yaml:"key,omitempty"`
	Value   string       `yaml:"value,omitempty"`

	dataOffset int
	wordOffset int

	isInterface bool
}

type UnionField struct {
	Name    string `yaml:"name"`
	Index   int    `yaml:"index"`
	Type    string `yaml:"type"`
	Element string `yaml:"element,omitempty"`
}

type DescInterface struct {
	Name        string         `yaml:"name"`
	Method      []*DescMethods `yaml:"methods"`
	Generic     []string       `yaml:"generic,omitempty"`
	Constraints []string       `yaml:"constraints,omitempty"`
	HTTP        *DescHTTPIface `yaml:"http,omitempty"`
}

// DescHTTPIface holds interface-level REST configuration from the IDL http: block.
type DescHTTPIface struct {
	// Prefix is prepended to every method's path template (e.g. /api/v1).
	Prefix string `yaml:"prefix,omitempty"`
}

type DescMethods struct {
	Name       string           `yaml:"name"`
	Index      int              `yaml:"index"`
	Parameters []*DescParamater `yaml:"parameters"`
	Results    []*DescParamater `yaml:"results"`
	// Public marks this method as accessible without TLS client certificate authentication.
	// Public methods still require capability-level auth (Ed25519 signatures) but allow
	// unauthenticated callers (e.g., for registration flows where the client doesn't have certs yet).
	Public bool            `yaml:"public,omitempty"`
	HTTP   *DescHTTPMethod `yaml:"http,omitempty"`
}

// DescHTTPMethod holds method-level REST configuration from the IDL http: block.
// It accepts two YAML shapes (see UnmarshalYAML): a compact scalar
// "VERB /path/template" for the common case, or a mapping with an explicit verb
// key plus an optional body: override. Exactly one verb field ends up set.
type DescHTTPMethod struct {
	Get    string `yaml:"get,omitempty"`
	Post   string `yaml:"post,omitempty"`
	Put    string `yaml:"put,omitempty"`
	Delete string `yaml:"delete,omitempty"`
	Patch  string `yaml:"patch,omitempty"`
	// Body designates request-body binding: "*" binds the whole JSON body onto
	// the method args; "" means no body and params come from the path and query
	// string. When unset, effectiveBody defaults it from the verb.
	Body string `yaml:"body,omitempty"`
}

// UnmarshalYAML accepts either the compact scalar form ("POST /apps") or the
// mapping form ({post: /apps, body: "*"}). The compact form keeps the common
// case terse and is backward compatible with the pre-existing http: convention
// in the IDL.
func (m *DescHTTPMethod) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		parts := strings.Fields(value.Value)
		if len(parts) != 2 {
			return fmt.Errorf("http: expected \"VERB /path\", got %q", value.Value)
		}
		verb, path := strings.ToUpper(parts[0]), parts[1]
		switch verb {
		case "GET":
			m.Get = path
		case "POST":
			m.Post = path
		case "PUT":
			m.Put = path
		case "DELETE":
			m.Delete = path
		case "PATCH":
			m.Patch = path
		default:
			return fmt.Errorf("http: unknown verb %q", verb)
		}
		return nil
	}

	// Mapping form. Alias the type to avoid recursing into this method.
	type raw DescHTTPMethod
	var r raw
	if err := value.Decode(&r); err != nil {
		return err
	}
	*m = DescHTTPMethod(r)
	return nil
}

// verbPath returns the HTTP verb and the bare path template (without any
// interface prefix) for this method's binding, or ok=false when unset.
func (m *DescHTTPMethod) verbPath() (verb, path string, ok bool) {
	switch {
	case m.Get != "":
		return "GET", m.Get, true
	case m.Post != "":
		return "POST", m.Post, true
	case m.Put != "":
		return "PUT", m.Put, true
	case m.Delete != "":
		return "DELETE", m.Delete, true
	case m.Patch != "":
		return "PATCH", m.Patch, true
	default:
		return "", "", false
	}
}

// effectiveBody resolves the request-body binding, defaulting from the verb when
// unset: body-carrying verbs (POST/PUT/PATCH) bind the whole JSON body, while
// GET/DELETE carry inputs in the path and query string. An explicit body: in
// the IDL always wins.
func (m *DescHTTPMethod) effectiveBody() string {
	if m.Body != "" {
		return m.Body
	}
	switch verb, _, _ := m.verbPath(); verb {
	case "POST", "PUT", "PATCH":
		return "*"
	default:
		return ""
	}
}

// validateHTTP checks every http: annotation in the IDL before any code is
// generated. Each of these mistakes is otherwise silent: the generator happily
// emits a binding and the route misbehaves at runtime, which is a much worse
// place to discover a typo in a path template.
func (g *Generator) validateHTTP() error {
	for _, i := range g.Interfaces {
		for _, m := range i.Method {
			if m.HTTP == nil {
				continue
			}

			if err := g.validateHTTPMethod(m); err != nil {
				return fmt.Errorf("%s.%s: %w", i.Name, m.Name, err)
			}
		}
	}

	return nil
}

func (g *Generator) validateHTTPMethod(m *DescMethods) error {
	var verbs []string
	for _, v := range []struct{ name, path string }{
		{"get", m.HTTP.Get},
		{"post", m.HTTP.Post},
		{"put", m.HTTP.Put},
		{"delete", m.HTTP.Delete},
		{"patch", m.HTTP.Patch},
	} {
		if v.path != "" {
			verbs = append(verbs, v.name)
		}
	}

	if len(verbs) == 0 {
		return fmt.Errorf("http: no verb set")
	}
	if len(verbs) > 1 {
		// verbPath picks the first non-empty field, so the others would be
		// dropped without a word.
		return fmt.Errorf("http: only one verb may be set, got %s", strings.Join(verbs, ", "))
	}

	// The REST gateway binds a request body only when Body is exactly "*". Any
	// other non-empty value counts as "has a body" for query-param generation
	// yet is never decoded, leaving the method with no inputs at all.
	if b := m.HTTP.Body; b != "" && b != "*" {
		return fmt.Errorf("http: body must be \"\" or \"*\", got %q", b)
	}

	// A capability is a live object reference scoped to an RPC connection.
	// There is nothing to hand back over a stateless HTTP request, and restCall
	// panics if a handler tries to mint one, so reject the annotation here
	// rather than fail mid-request.
	for _, p := range m.Parameters {
		if g.ti(p.Type).isInterface {
			return fmt.Errorf("http: parameter %q is a capability, which cannot be expressed over REST", p.Name)
		}
	}
	for _, p := range m.Results {
		if g.ti(p.Type).isInterface {
			return fmt.Errorf("http: result %q is a capability, which cannot be expressed over REST", p.Name)
		}
	}

	// Every {wildcard} must name a declared parameter. Otherwise the generated
	// args struct has no field to receive it and json.Unmarshal drops the path
	// value silently.
	declared := make(map[string]struct{}, len(m.Parameters))
	for _, p := range m.Parameters {
		declared[p.Name] = struct{}{}
	}

	_, bare, _ := m.HTTP.verbPath()
	for _, name := range httpPathParams(bare) {
		if _, ok := declared[name]; !ok {
			return fmt.Errorf("http: path parameter %q in %q has no matching method parameter", name, bare)
		}
	}

	return nil
}

// httpBinding resolves a method's REST binding, returning the HTTP verb and the
// full path template with the interface prefix applied. ok is false when the
// method carries no http: annotation.
func httpBinding(i *DescInterface, m *DescMethods) (verb, path string, ok bool) {
	if m.HTTP == nil {
		return "", "", false
	}

	verb, bare, ok := m.HTTP.verbPath()
	if !ok {
		return "", "", false
	}

	if i.HTTP != nil && i.HTTP.Prefix != "" {
		bare = strings.TrimRight(i.HTTP.Prefix, "/") + "/" + strings.TrimLeft(bare, "/")
	}

	return verb, bare, true
}

// httpQueryParam pairs a parameter name with the REST coercion kind of its type.
type httpQueryParam struct {
	Name string
	Kind string
}

// httpQueryParams returns the parameters bound from the query string: every
// parameter that is not a path wildcard, but only for bodyless bindings. When a
// method carries a request body, its non-path params ride in the JSON body, so
// there is nothing to bind from the query.
func httpQueryParams(m *DescMethods, pathParams []string) []httpQueryParam {
	if m.HTTP == nil || m.HTTP.effectiveBody() != "" {
		return nil
	}

	inPath := make(map[string]struct{}, len(pathParams))
	for _, p := range pathParams {
		inPath[p] = struct{}{}
	}

	var query []httpQueryParam
	for _, p := range m.Parameters {
		if _, ok := inPath[p.Name]; ok {
			continue
		}
		query = append(query, httpQueryParam{Name: p.Name, Kind: httpParamKind(p.Type)})
	}
	return query
}

// httpParamKind maps an IDL scalar type name onto the coercion kind the REST
// gateway uses to turn a raw query string into typed JSON. Non-scalar types
// (messages, lists, maps, interfaces) fall back to "string", which for query
// binding means the raw value is passed through as a JSON string.
func httpParamKind(typeName string) string {
	switch typeName {
	case "bool":
		return "bool"
	case "int", "int8", "int16", "int32", "int64":
		return "int"
	case "uint", "uint8", "uint16", "uint32", "uint64", "byte":
		return "uint"
	case "float32", "float64":
		return "float"
	case "standard.Timestamp":
		return "timestamp"
	default:
		return "string"
	}
}

// httpPathParams extracts the wildcard names ({name}) from a path template, in
// order of appearance.
func httpPathParams(path string) []string {
	var params []string
	for {
		open := strings.IndexByte(path, '{')
		if open == -1 {
			break
		}
		close := strings.IndexByte(path[open:], '}')
		if close == -1 {
			break
		}
		params = append(params, path[open+1:open+close])
		path = path[open+close+1:]
	}
	return params
}

type DescParamater struct {
	Name    string `yaml:"name"`
	Type    string `yaml:"type"`
	Element string `yaml:"element,omitempty"`
	Key     string `yaml:"key,omitempty"`
	Value   string `yaml:"value,omitempty"`
}
