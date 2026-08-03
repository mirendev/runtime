package rpc

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestQUICConfigsPinInitialPacketSize walks the tree and fails on any
// quic.Config literal that omits InitialPacketSize.
//
// This is guarding a bug we have now shipped twice. Omitting the field costs
// nothing on a 1500-MTU path and silently breaks every handshake over a
// 1280-MTU tunnel (Tailscale, WireGuard), because quic-go swallows the
// resulting EMSGSIZE without shrinking the Initial. The first fix pinned it in
// DefaultQUICConfig and missed four call sites that build their own config; one
// of those was `miren cluster add`, which left a user unable to reach a
// tailnet-only cluster while every server-side diagnostic looked healthy.
//
// A grep would do, but nothing runs a grep. If this test becomes an obstacle,
// deleting it is fine; just make sure the thing it is protecting still holds.
func TestQUICConfigsPinInitialPacketSize(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	fset := token.NewFileSet()
	var offenders []string

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Vendored and generated trees are not ours to police.
			switch d.Name() {
			case ".git", ".jj", "vendor", "node_modules", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			// A file we cannot parse is not this test's problem; the build
			// catches it far more legibly.
			return nil
		}

		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			sel, ok := lit.Type.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Config" {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "quic" {
				return true
			}

			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "InitialPacketSize" {
					return true
				}
			}

			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			offenders = append(offenders, rel+":"+strconv.Itoa(fset.Position(lit.Pos()).Line))
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}

	if len(offenders) > 0 {
		t.Errorf("quic.Config literals missing InitialPacketSize (set it to rpc.InitialPacketSize):\n  %s\n\n"+
			"Handshakes over 1280-MTU tunnels (Tailscale, WireGuard) fail silently without it.",
			strings.Join(offenders, "\n  "))
	}
}
