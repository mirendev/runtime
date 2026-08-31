// Package guardrails holds structural tests that scan the source tree for
// anti-patterns the type system and unit tests can't catch on their own.
package guardrails

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// allowedEnumWraps lists "file:line" sites that are permitted to wrap a
// screaming-case constant in entity.Id(). It should stay empty; add an entry
// only with a comment explaining why the value really is a full id, not an
// enum value.
var allowedEnumWraps = map[string]bool{}

// TestNoEnumValueWrappedInEntityId guards MIR-1425/MIR-1288 at the source level,
// and answers Evan's "we should probably audit uses of entity.Id" on #927.
//
// Ref-typed status attributes take a fully-qualified choice id (e.g.
// SandboxStatusStoppedId = "dev.miren.compute/status.stopped"). The recurring
// bug is wrapping the *short* enum value constant instead —
// entity.Ref(SandboxStatusId, entity.Id(compute_v1alpha.STOPPED)) — which
// yields "stopped", a value no schema.Choices set contains. The runtime
// validator now rejects that on any write it exercises; this scan catches the
// shape everywhere at CI time, including non-write contexts (comparisons) the
// validator never sees.
//
// Two rules, tuned so real enum values trip it and ordinary field accesses
// don't:
//   - A BARE screaming-case ident wrapped in entity.Id() (entity.Id(STOPPED))
//     is an enum value in any context, so it's flagged wherever it appears.
//   - A package-qualified screaming const (entity.Id(compute_v1alpha.STOPPED))
//     is flagged only as the value of an entity.Ref / entity.RefValue. That
//     keeps the common, legitimate entity.Id(x.ID) field conversion (also
//     upper-case) from tripping it outside of a ref.
//
// The correct fully-qualified *Id constants are MixedCase, so they never match.
func TestNoEnumValueWrappedInEntityId(t *testing.T) {
	root := repoRoot(t)

	var violations []string

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "vendor", "testdata", "node_modules", ".git":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			// A file we can't parse isn't this test's problem; the compiler
			// will surface it. Skip rather than fail on unrelated breakage.
			return nil
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if inner, ok := findEnumWrap(call); ok {
				pos := fset.Position(call.Pos())
				rel, _ := filepath.Rel(root, pos.Filename)
				site := rel + ":" + itoa(pos.Line)
				if !allowedEnumWraps[site] {
					violations = append(violations, site+"  ->  "+inner)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking source tree: %v", err)
	}

	if len(violations) > 0 {
		t.Fatalf("wrapping a short enum value in entity.Id() writes an unresolvable "+
			"id (MIR-1288); use the fully-qualified <Kind><Value>Id constant or the "+
			"generated .Encode() instead:\n  %s",
			strings.Join(violations, "\n  "))
	}
}

// TestEnumWrapMatcherDetects proves the scan actually fires on the bug shapes
// and stays quiet on the legitimate forms, so a green
// TestNoEnumValueWrappedInEntityId means "clean", not "matcher broken".
func TestEnumWrapMatcherDetects(t *testing.T) {
	const src = `package p

func f() {
	entity.Ref(SandboxStatusId, entity.Id(compute_v1alpha.STOPPED)) // BAD: pkg enum value in ref
	entity.Ref(SandboxStatusId, entity.Id(DEAD))                    // BAD: bare enum value in ref
	entity.RefValue(entity.Id(RUNNING))                             // BAD: bare enum value via RefValue
	_ = status == entity.Id(PENDING)                                // BAD: bare enum value in a comparison

	entity.Ref(SandboxStatusId, compute_v1alpha.SandboxStatusStoppedId) // ok: full *Id const
	entity.Ref(SandboxStatusId, entity.Id(fullID))                      // ok: dynamic lower-case id
	entity.Ref(SandboxStatusId, "")                                     // ok: clearing
	_ = entity.Id(compute_v1alpha.STOPPED)                              // ok: pkg-qualified outside a ref
	_ = entity.Id(attr.ID)                                              // ok: field access, never flagged
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "snippet.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var hits []string
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if inner, ok := findEnumWrap(call); ok {
				hits = append(hits, inner)
			}
		}
		return true
	})

	want := []string{
		"entity.Id(compute_v1alpha.STOPPED)",
		"entity.Id(DEAD)",
		"entity.Id(RUNNING)",
		"entity.Id(PENDING)",
	}
	if len(hits) != len(want) {
		t.Fatalf("expected %v, got %v", want, hits)
	}
	for i := range want {
		if hits[i] != want[i] {
			t.Fatalf("hit %d: expected %q, got %q (all: %v)", i, want[i], hits[i], hits)
		}
	}
}

// findEnumWrap reports an enum-value-in-entity.Id() misuse for a single call
// expression, if any, returning a printable form of the offending expression.
func findEnumWrap(call *ast.CallExpr) (string, bool) {
	// Rule 1: a bare screaming-case ident wrapped in entity.Id(), anywhere.
	if name, ok := entitySelector(call.Fun); ok && name == "Id" && len(call.Args) == 1 {
		if id, ok := call.Args[0].(*ast.Ident); ok && isScreamingCase(id.Name) {
			return "entity.Id(" + id.Name + ")", true
		}
	}
	// Rule 2: entity.Id(pkg.SCREAMING) as the value of a ref.
	if refArg, ok := refValueArg(call); ok {
		if inner, ok := enumWrapSelector(refArg); ok {
			return "entity.Id(" + inner + ")", true
		}
	}
	return "", false
}

// refValueArg returns the argument that supplies the ref *value* for an
// entity.Ref(id, value) or entity.RefValue(value) call, if this call is one.
func refValueArg(call *ast.CallExpr) (ast.Expr, bool) {
	name, ok := entitySelector(call.Fun)
	if !ok {
		return nil, false
	}
	switch name {
	case "Ref":
		if len(call.Args) == 2 {
			return call.Args[1], true
		}
	case "RefValue":
		if len(call.Args) == 1 {
			return call.Args[0], true
		}
	}
	return nil, false
}

// enumWrapSelector reports whether expr is entity.Id(pkg.SCREAMING), returning
// a printable form of the selector. The bare-ident case is handled globally by
// findEnumWrap's Rule 1, so this only matches package-qualified selectors.
func enumWrapSelector(expr ast.Expr) (string, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	if name, ok := entitySelector(call.Fun); !ok || name != "Id" || len(call.Args) != 1 {
		return "", false
	}
	sel, ok := call.Args[0].(*ast.SelectorExpr)
	if !ok || !isScreamingCase(sel.Sel.Name) {
		return "", false
	}
	if x, ok := sel.X.(*ast.Ident); ok {
		return x.Name + "." + sel.Sel.Name, true
	}
	return sel.Sel.Name, true
}

// entitySelector returns the method name for an `entity.<Name>` selector.
func entitySelector(fun ast.Expr) (string, bool) {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	x, ok := sel.X.(*ast.Ident)
	if !ok || x.Name != "entity" {
		return "", false
	}
	return sel.Sel.Name, true
}

// isScreamingCase reports whether s looks like an enum value constant:
// upper-case letters, digits and underscores only, with at least one letter and
// length > 1 (so "A" or "42" don't trip it).
func isScreamingCase(s string) bool {
	if len(s) < 2 {
		return false
	}
	hasLetter := false
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			hasLetter = true
		case r >= '0' && r <= '9', r == '_':
		default:
			return false
		}
	}
	return hasLetter
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// repoRoot walks up from the test's working directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find go.mod above %s", dir)
		}
		dir = parent
	}
}
