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
	"github.com/davecgh/go-spew/spew"
	"golang.org/x/tools/imports"
	"gopkg.in/yaml.v3"
)

type Generator struct {
	Imports    map[string]Import
	Types      []*DescType
	Interfaces []*DescInterface

	importedGenerators map[string]*Generator

	typeInfo map[string]typeInfo
}

func NewGenerator() (*Generator, error) {
	return &Generator{
		typeInfo:           make(map[string]typeInfo),
		importedGenerators: make(map[string]*Generator),
	}, nil
}

type Import struct {
	Path   string `yaml:"path"`
	Import string `yaml:"import"`
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
	return strings.ToTitle(s[:1]) + s[1:]
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
					gr.Id(capitalize(p.Name)).Op("*").Qual("miren.dev/runtime/pkg/rpc", "Capability").Tag(map[string]string{
						"cbor": fmt.Sprintf("%d,keyasint,omitempty", idx),
						"json": p.Name + ",omitempty",
					})
				} else if p.Type == "bytes" {
					gr.Id(capitalize(p.Name)).Op("*").Index().Byte().Tag(map[string]string{
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
			g.Id("call").Id("*").Qual("miren.dev/runtime/pkg/rpc", "Call")
			g.Id("data").Add(privateArgs)
		})

		for idx, p := range m.Parameters {
			g.readForField(f,
				&DescType{Type: tn + "Args", Generic: t.Generic},
				&DescField{
					Name:  p.Name,
					Type:  p.Type,
					Index: idx,
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
			g.Id("call").Id("*").Qual("miren.dev/runtime/pkg/rpc", "Call")
			g.Id("data").Add(privateResults)
		})

		for idx, p := range m.Results {
			g.writeForField(f,
				&DescType{Type: tn + "Results", Generic: t.Generic},
				&DescField{
					Name:  p.Name,
					Type:  p.Type,
					Index: idx,
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
	name := capitalize(field.Name)
	expName := capitalize(t.Type)

	_, recv := t.addGeneric(expName)

	switch field.Type {
	case "bool":
		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id("Has" + name).Params().Bool().Block(
			j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
		)

		f.Line()

		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id(name).Params().Bool().Block(
			j.If(j.Id("v").Dot("data").Dot(name).Op("==").Nil()).Block(
				j.Return(j.Lit(false)),
			),
			j.Return(j.Op("*").Id("v").Dot("data").Dot(name)),
		)

		f.Line()
	case "uint32", "int32", "uint64", "int64", "float32", "float64":
		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id("Has" + name).Params().Bool().Block(
			j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
		)

		f.Line()

		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id(name).Params().Id(field.Type).Block(
			j.If(j.Id("v").Dot("data").Dot(name).Op("==").Nil()).Block(
				j.Return(j.Lit(0)),
			),
			j.Return(j.Op("*").Id("v").Dot("data").Dot(name)),
		)

		f.Line()

	case "bytes":
		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id("Has" + name).Params().Bool().Block(
			j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
		)

		f.Line()

		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id(name).Params().Index().Byte().Block(
			j.If(j.Id("v").Dot("data").Dot(name).Op("==").Nil()).Block(
				j.Return(j.Nil()),
			),
			j.Return(j.Op("*").Id("v").Dot("data").Dot(name)),
		)

		f.Line()
	case "string":
		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id("Has" + name).Params().Bool().Block(
			j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
		)

		f.Line()

		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id(name).Params().String().Block(
			j.If(j.Id("v").Dot("data").Dot(name).Op("==").Nil()).Block(
				j.Return(j.Lit("")),
			),
			j.Return(j.Op("*").Id("v").Dot("data").Dot(name)),
		)

		f.Line()
	case "list":
		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id("Has" + name).Params().Bool().Block(
			j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
		)

		f.Line()

		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id(name).Params().Index().Id(field.Element).Block(
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
			).Id("Has" + name).Params().Bool().Block(
				j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
			)

			f.Line()

			f.Func().Params(
				j.Id("v").Op("*").Add(recv),
			).Id(name).Params().Op("*").Id(g.deriveType(field.Type, "Client")).Block(
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
			).Id("Has" + name).Params().Bool().Block(
				j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
			)

			f.Line()

			f.Func().Params(
				j.Id("v").Op("*").Add(recv),
			).Id(name).Params().Id(field.Type).Block(
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
			).Id("Has" + name).Params().Bool().Block(
				j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
			)

			f.Line()

			f.Func().Params(
				j.Id("v").Op("*").Add(recv),
			).Id(name).Params().Op("*").Add(g.properType(field.Type)).Block(
				j.Return(j.Id("v").Dot("data").Dot(name)),
			)

		} else {
			f.Func().Params(
				j.Id("v").Op("*").Add(recv),
			).Id("Has" + name).Params().Bool().Block(
				j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
			)

			f.Line()

			f.Func().Params(
				j.Id("v").Op("*").Add(recv),
			).Id(name).Params().Add(g.properType(field.Type)).Block(
				j.Return(j.Op("*").Id("v").Dot("data").Dot(name)),
			)
		}

		f.Line()

	}

	f.Line()
}

func (g *Generator) writeForField(f *j.File, t *DescType, field *DescField) {
	name := capitalize(field.Name)
	expName := capitalize(t.Type)

	_, recv := t.addGeneric(expName)

	switch field.Type {
	case "bool":
		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id("Set" + name).Params(
			j.Id(field.Name).Bool(),
		).Block(
			j.Id("v").Dot("data").Dot(name).Op("=").Op("&").Id(field.Name),
		)

	case "uint32", "int32", "uint64", "int64", "float32", "float64":
		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id("Set" + name).Params(
			j.Id(field.Name).Id(field.Type),
		).Block(
			j.Id("v").Dot("data").Dot(name).Op("=").Op("&").Id(field.Name),
		)

	case "bytes":
		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id("Set"+name).Params(
			j.Id(field.Name).Index().Byte(),
		).Block(
			j.Id("x").Op(":=").Id("slices").Dot("Clone").Call(j.Id(field.Name)),
			j.Id("v").Dot("data").Dot(name).Op("=").Op("&").Id("x"),
		)

	case "string":
		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id("Set" + name).Params(
			j.Id(field.Name).String(),
		).Block(
			j.Id("v").Dot("data").Dot(name).Op("=").Op("&").Id(field.Name),
		)

	case "list":
		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id("Set"+name).Params(
			j.Id(field.Name).Index().Id(field.Element),
		).Block(
			j.Id("x").Op(":=").Id("slices").Dot("Clone").Call(j.Id(field.Name)),
			j.Id("v").Dot("data").Dot(name).Op("=").Op("&").Id("x"),
		)

	default:
		if g.ti(field.Type).isInterface {
			f.Func().Params(
				j.Id("v").Op("*").Add(recv),
			).Id("Set" + name).Params(
				j.Id(field.Name).Id(field.Type),
			).Block(
				j.Id("v").Dot("data").Dot(name).Op("=").Id("v").Dot("call").Dot("NewCapability").CallFunc(func(gr *j.Group) {
					if g.isImported(field.Type) {
						iname, tname := g.splitType(field.Type)
						gr.Qual(g.Imports[iname].Import, "Adapt"+tname).Call(j.Id(field.Name))
					} else {
						gr.Id("Adapt" + field.Type).Call(j.Id(field.Name))
					}
				}),
			)

			f.Line()

			return
		}

		if slices.Contains(t.Generic, field.Type) {
			f.Func().Params(
				j.Id("v").Op("*").Add(recv),
			).Id("Set" + name).Params(
				j.Id(field.Name).Add(g.properType(field.Type)),
			).Block(
				j.Id("v").Dot("data").Dot(name).Op("=").Op("&").Id(field.Name),
			)
			return
		}

		f.Func().Params(
			j.Id("v").Op("*").Add(recv),
		).Id("Set" + name).Params(
			j.Id(field.Name).Op("*").Add(g.properType(field.Type)),
		).Block(
			j.Id("v").Dot("data").Dot(name).Op("=").Id(field.Name),
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
			fieldName := capitalize(field.Name)
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
			gr.Id("U_" + capitalize(field.Name)).Op("*").Add(fieldType).Tag(map[string]string{
				"cbor": fmt.Sprintf("%d,keyasint,omitempty", field.Index),
				"json": field.Name + ",omitempty",
			})
		}
	})
	f.Line()

	// Generate Which method
	f.Func().Params(
		j.Id("v").Op("*").Id(structName),
	).Id("Which").Params().String().BlockFunc(func(g *j.Group) {
		for _, field := range fields {
			g.If(j.Id("v").Dot("U_" + capitalize(field.Name)).Op("!=").Nil()).Block(
				j.Return(j.Lit(field.Name)),
			)
		}
		g.Return(j.Lit(""))
	})
	f.Line()

	// Generate getters and setters
	for _, field := range fields {
		fieldName := capitalize(field.Name)
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
					g.Id("v").Dot("U_" + capitalize(other.Name)).Op("=").Nil()
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

func (g *Generator) generateStruct(f *j.File) error {
	f.ImportName("github.com/fxamacker/cbor/v2", "cbor")
	rpc := "miren.dev/runtime/pkg/rpc"

	for _, t := range g.Types {
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
			for _, field := range t.Fields {
				if field.Type == "list" {
					gr.Id(capitalize(field.Name)).Op("*").Index().Id(field.Element).Tag(map[string]string{
						"cbor": fmt.Sprintf("%d,keyasint,omitempty", field.Index),
						"json": field.Name + ",omitempty",
					})

				} else if field.Type == "union" {
					gr.Id(private(t.Type) + capitalize(field.Name))
				} else {
					typ := j.Op("*").Add(g.properType(field.Type))

					if field.isInterface {
						typ = j.Op("*").Qual(rpc, "Capability")
					}

					gr.Id(capitalize(field.Name)).Add(typ).Tag(map[string]string{
						"cbor": fmt.Sprintf("%d,keyasint,omitempty", field.Index),
						"json": field.Name + ",omitempty",
					})
				}
			}
		})

		f.Line()

		structType, recv := t.addGeneric(expName)

		f.Type().Add(structType).StructFunc(func(g *j.Group) {
			if t.includeCall {
				g.Id("call").Op("*").Qual(rpc, "Call")
			}

			g.Id("data").Add(dataRecv)
		})

		f.Line()

		for _, field := range t.Fields {
			name := capitalize(field.Name)

			switch field.Type {
			case "bool":
				if t.Readable() {
					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id("Has" + name).Params().Bool().Block(
						j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
					)

					f.Line()

					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id(name).Params().Bool().Block(
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
					).Id("Set" + name).Params(
						j.Id(field.Name).Bool(),
					).Block(
						j.Id("v").Dot("data").Dot(name).Op("=").Op("&").Id(field.Name),
					)
				}

			case "uint32", "int32", "uint64", "int64", "float32", "float64":
				if t.Readable() {
					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id("Has" + name).Params().Bool().Block(
						j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
					)

					f.Line()

					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id(name).Params().Add(g.properType(field.Type)).Block(
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
					).Id("Set" + name).Params(
						j.Id(field.Name).Add(g.properType(field.Type)),
					).Block(
						j.Id("v").Dot("data").Dot(name).Op("=").Op("&").Id(field.Name),
					)
				}

			case "bytes":
				if t.Readable() {
					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id("Has" + name).Params().Bool().Block(
						j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
					)

					f.Line()

					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id(name).Params().String().Block(
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
					).Id("Set"+name).Params(
						j.Id(field.Name).String(),
					).Block(
						j.Id("x").Op(":=").Id("slices").Dot("Clone").Call(j.Id(field.Name)),
						j.Id("v").Dot("data").Dot(name).Op("=").Op("&").Id("x"),
					)
				}
			case "string":
				if t.Readable() {
					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id("Has" + name).Params().Bool().Block(
						j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
					)

					f.Line()

					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id(name).Params().String().Block(
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
					).Id("Set" + name).Params(
						j.Id(field.Name).String(),
					).Block(
						j.Id("v").Dot("data").Dot(name).Op("=").Op("&").Id(field.Name),
					)
				}

			case "list":
				if t.Readable() {
					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id("Has" + name).Params().Bool().Block(
						j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
					)

					f.Line()

					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id(name).Params().Index().Id(field.Element).Block(
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
					).Id("Set"+name).Params(
						j.Id(field.Name).Index().Id(field.Element),
					).Block(
						j.Id("x").Op(":=").Id("slices").Dot("Clone").Call(j.Id(field.Name)),
						j.Id("v").Dot("data").Dot(name).Op("=").Op("&").Id("x"),
					)
				}
			case "union":
				f.Func().Params(
					j.Id("v").Op("*").Add(recv),
				).Id(name).Params().Id(capitalize(t.Type) + capitalize(field.Name)).Block(
					j.Return(j.Op("&").Id("v").Dot("data").Dot(private(t.Type) + capitalize(field.Name))),
				)

				f.Line()

			default:
				if g.ti(field.Type).isInterface {
					if t.Readable() {
						f.Func().Params(
							j.Id("v").Op("*").Add(recv),
						).Id("Has" + name).Params().Bool().Block(
							j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Lit("")),
						)

						f.Line()

						f.Func().Params(
							j.Id("v").Op("*").Add(recv),
						).Id(name).Params().Add(g.properType(field.Type)).Block(
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
						).Id("Set" + name).Params(
							j.Id(field.Name).Add(g.properType(field.Type)),
						).Block(
							j.Id("v").Dot("data").Dot(name).Op("=").Id("v").Dot("call").Dot("NewCapability").CallFunc(func(gr *j.Group) {
								if g.isImported(field.Type) {
									iname, tname := g.splitType(field.Type)
									gr.Qual(g.Imports[iname].Import, "Adapt"+tname).Call(j.Id(field.Name))
								} else {
									j.Id("Adapt" + field.Type).Call(j.Id(field.Name))
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
					).Id("Has" + name).Params().Bool().Block(
						j.Return(j.Id("v").Dot("data").Dot(name).Op("!=").Nil()),
					)

					f.Line()

					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id(name).Params().Op("*").Add(g.properType(field.Type)).Block(
						j.Return(j.Id("v").Dot("data").Dot(name)),
					)

					f.Line()
				}

				if t.Writeable() {
					f.Func().Params(
						j.Id("v").Op("*").Add(recv),
					).Id("Set" + name).Params(
						j.Id(field.Name).Op("*").Add(g.properType(field.Type)),
					).Block(
						j.Id("v").Dot("data").Dot(name).Op("=").Id(field.Name),
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
		j.Op("*").Qual(rpc, "Client"),
	)

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
			j.Id("client").Op("*").Qual(rpc, "Client"),
			j.Id("data").Add(i.typeName(private(i.Name)+capitalize(m.Name)+"ResultsData")),
		)

		f.Line()

		for _, p := range m.Results {
			name := capitalize(p.Name)

			if g.ti(p.Type).isInterface {
				f.Func().Params(
					j.Id("v").Op("*").Add(i.typeName(tn + "Results")),
				).Id(name).Params().Id(g.deriveType(p.Type, "Client")).Block(
					j.Return(j.Id(g.deriveType(p.Type, "Client")).Values(
						j.Line().Id("Client").Op(":").Id("v").Dot("client").Dot("NewClient").Call(j.Id("v").Dot("data").Dot(name)),
						j.Line(),
					),
					),
				)
			} else {
				g.readForField(f,
					&DescType{Type: tn + "Results", Generic: i.Generic},
					&DescField{
						Name:  p.Name,
						Type:  p.Type,
						Index: 0,
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
				} else {
					gr.Id(private(p.Name)).Add(g.properType(p.Type))
				}
			}
		}).Params(j.Op("*").Add(i.typeName(tn+"Results")), j.Error()).BlockFunc(func(gr *j.Group) {
			gr.Id("args").Op(":= ").Add(i.typeName(capitalize(i.Name) + capitalize(m.Name) + "Args")).Values()

			for _, p := range m.Parameters {
				if g.ti(p.Type).isInterface {
					gr.Id("args").Dot("data").Dot(capitalize(p.Name)).Op("=").Id("v").Dot("Client").Dot("NewCapability").CallFunc(func(gr *j.Group) {
						if g.isImported(p.Type) {
							iname, tname := g.splitType(p.Type)
							gr.Qual(g.Imports[iname].Import, "Adapt"+tname).Call(j.Id(p.Name))
						} else {
							gr.Id("Adapt" + capitalize(p.Type)).Call(j.Id(private(p.Name)))
						}
						gr.Id(private(p.Name))
					})
				} else if g.ti(p.Type).isMessage {
					gr.Id("args").Dot("data").Dot(capitalize(p.Name)).Op("=").Id(private(p.Name))
				} else {
					gr.Id("args").Dot("data").Dot(capitalize(p.Name)).Op("=").Op("&").Id(private(p.Name))
				}
			}

			gr.Line()

			gr.Var().Id("ret").Add(i.typeName(private(i.Name) + capitalize(m.Name) + "ResultsData"))

			gr.Line()

			gr.Id("err").Op(":=").Id("v").Dot("Client").Dot("Call").Call(
				j.Id("ctx"),
				j.Lit(m.Name),
				j.Op("&").Id("args"),
				j.Op("&").Id("ret"),
			)
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
				j.Op("*").Qual(rpc, "Call"),
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
			j.Id("client").Op("*").Qual(rpc, "Client"),
		)

		for _, m := range i.Method {
			methodName := capitalize(m.Name)

			f.Func().Params(j.Id("_").Add(recv)).Id(methodName).Params(
				j.Id("ctx").Qual("context", "Context"),
				j.Id("state").Op("*").Add(i.typeName(expName+capitalize(m.Name))),
			).Error().Block(
				j.Panic(j.Lit("not implemented")),
			)

			f.Line()
		}

		f.Func().Params(j.Id("t").Add(recv)).Id("CapabilityClient").
			Params().Params(j.Op("*").Qual(rpc, "Client")).Block(
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
						g.Line().Id("Handler").Op(":").Func().Params(
							j.Id("ctx").Qual("context", "Context"),
							j.Id("call").Op("*").Qual(rpc, "Call"),
						).Error().Block(
							j.Return(j.Id("t").Dot(methodName).Call(
								j.Id("ctx"),
								j.Op("&").Add(i.typeName(expName+capitalize(m.Name))).Values(j.Id("Call").Op(":").Id("call")),
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

func (g *Generator) Generate(name string) (string, error) {
	f := j.NewFile(name)

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
		return "", err
	}

	code, err := imports.Process("out.go", buf.Bytes(), &imports.Options{})
	if err != nil {
		spew.Dump(buf.String())
		return "", err
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
	Generic     []string     `yaml:"generic,omitempty"`
	Constraints []string     `yaml:"constraints,omitempty"`

	access      int
	includeCall bool
	result      bool

	dataSize int
	pointers int

	userType *DescType
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
}

type DescMethods struct {
	Name       string           `yaml:"name"`
	Index      int              `yaml:"index"`
	Parameters []*DescParamater `yaml:"parameters"`
	Results    []*DescParamater `yaml:"results"`
}

type DescParamater struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
}
