package sagalock

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/saga"
)

type stepIn struct {
	Seed int `saga:"seed"`
}

type stepOut struct {
	Value int `saga:"value"`
}

type finishIn struct {
	Value int `saga:"value"`
}

type finishOut struct {
	Done bool `saga:"done"`
}

func step(ctx context.Context, in stepIn) (stepOut, error)             { return stepOut{}, nil }
func undoStep(ctx context.Context, in stepIn, out stepOut) error       { return nil }
func finish(ctx context.Context, in finishIn) (finishOut, error)       { return finishOut{}, nil }
func undoFinish(ctx context.Context, in finishIn, out finishOut) error { return nil }

func build(t *testing.T, b *saga.Builder) *saga.Definition {
	t.Helper()
	def, err := b.Build()
	require.NoError(t, err)
	return def
}

func TestCompare(t *testing.T) {
	v1 := build(t, saga.Define("demo").
		Action("step", step).Undo(undoStep).
		Action("finish", finish).Undo(undoFinish))
	lock := map[string]Entry{"demo": EntryFor(v1)}

	t.Run("unchanged definition passes", func(t *testing.T) {
		breaking, stale := Compare(lock, []*saga.Definition{v1})
		assert.Empty(t, breaking)
		assert.Empty(t, stale)
	})

	t.Run("renamed action at the same version is breaking", func(t *testing.T) {
		renamed := build(t, saga.Define("demo").
			Action("begin", step).Undo(undoStep).
			Action("finish", finish).Undo(undoFinish))
		breaking, _ := Compare(lock, []*saga.Definition{renamed})
		require.Len(t, breaking, 1)
		assert.Contains(t, breaking[0], "changed shape without a version bump")
	})

	t.Run("removed action at the same version is breaking", func(t *testing.T) {
		removed := build(t, saga.Define("demo").Action("step", step).Undo(undoStep))
		breaking, _ := Compare(lock, []*saga.Definition{removed})
		assert.Len(t, breaking, 1)
	})

	t.Run("bumped version is stale, not breaking", func(t *testing.T) {
		bumped := build(t, saga.Define("demo").Version(2).ResumesFrom().
			Action("begin", step).Undo(undoStep).
			Action("finish", finish).Undo(undoFinish))
		breaking, stale := Compare(lock, []*saga.Definition{bumped})
		assert.Empty(t, breaking)
		require.Len(t, stale, 1)
		assert.Contains(t, stale[0], "moved to v2")
	})

	t.Run("version going backwards is breaking", func(t *testing.T) {
		ahead := map[string]Entry{"demo": {Version: 3, Actions: EntryFor(v1).Actions}}
		breaking, _ := Compare(ahead, []*saga.Definition{v1})
		require.Len(t, breaking, 1)
		assert.Contains(t, breaking[0], "back to v1")
	})

	t.Run("new and removed definitions are stale", func(t *testing.T) {
		other := build(t, saga.Define("other").Action("step", step).Undo(undoStep))
		_, stale := Compare(lock, []*saga.Definition{other})
		assert.Len(t, stale, 2)
	})
}

// TestEveryDefinitionIsLocked is what keeps the per-package locks honest: a
// definition is only checked if its package's lock test registers it, so this
// reads the source for every saga.Define call and requires its name in the
// lock beside it. A new saga, or one added to a package without adding it to
// that package's lock test, fails here.
func TestEveryDefinitionIsLocked(t *testing.T) {
	root := repoRoot(t)
	defined := findDefinitions(t, root)
	require.NotEmpty(t, defined, "found no saga.Define calls; the scan is broken")

	for dir, names := range defined {
		rel, _ := filepath.Rel(root, dir)
		lock, err := Load(filepath.Join(dir, LockFile))
		require.NoError(t, err)

		for _, name := range names {
			if _, ok := lock[name]; !ok {
				t.Errorf("saga %q is defined in %s but not in its %s. Register it in that package's "+
					"lock test (see pkg/saga/sagalock) and run it with %s=1", name, rel, LockFile, UpdateEnv)
			}
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "no go.mod above the test directory")
		dir = parent
	}
}

// findDefinitions maps each package directory to the saga names it defines.
// A name is a string literal or a package-level string constant; anything
// else fails the test rather than going silently unchecked.
func findDefinitions(t *testing.T, root string) map[string][]string {
	t.Helper()
	const sagaPkg = "miren.dev/runtime/pkg/saga"

	fset := token.NewFileSet()
	byDir := map[string][]*ast.File{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "testdata", "node_modules", "vendor", ".git", ".iso":
				return filepath.SkipDir
			}
			if path == filepath.Join(root, "pkg", "saga") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		byDir[filepath.Dir(path)] = append(byDir[filepath.Dir(path)], file)
		return nil
	})
	require.NoError(t, err)

	defined := map[string][]string{}
	for dir, files := range byDir {
		consts := stringConsts(files)
		for _, file := range files {
			local := importName(file, sagaPkg)
			if local == "" {
				continue
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Define" {
					return true
				}
				if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != local {
					return true
				}

				switch arg := call.Args[0].(type) {
				case *ast.BasicLit:
					name, err := strconv.Unquote(arg.Value)
					require.NoError(t, err)
					defined[dir] = append(defined[dir], name)
				case *ast.Ident:
					name, ok := consts[arg.Name]
					if !ok {
						t.Errorf("%s: saga.Define(%s) names a saga with something other than a string constant",
							fset.Position(call.Pos()), arg.Name)
						return true
					}
					defined[dir] = append(defined[dir], name)
				default:
					t.Errorf("%s: saga.Define needs a string literal or constant name to be lock-checked",
						fset.Position(call.Pos()))
				}
				return true
			})
		}
	}
	return defined
}

func importName(file *ast.File, path string) string {
	for _, imp := range file.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); p == path {
			if imp.Name != nil {
				return imp.Name.Name
			}
			return filepath.Base(path)
		}
	}
	return ""
}

func stringConsts(files []*ast.File) map[string]string {
	consts := map[string]string{}
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, ident := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if v, err := strconv.Unquote(lit.Value); err == nil {
							consts[ident.Name] = v
						}
					}
				}
			}
		}
	}
	return consts
}
