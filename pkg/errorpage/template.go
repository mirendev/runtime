package errorpage

import (
	"fmt"
	"html/template"
	"strings"
	"text/template/parse"
)

func Parse(src string, logo template.HTML) (*template.Template, error) {
	if strings.TrimSpace(src) == "" {
		return nil, fmt.Errorf("error page template is empty")
	}
	return template.New("ingress-error").Funcs(template.FuncMap{
		"brandLogo": func() template.HTML { return logo },
	}).Parse(src)
}

// ParseApp rejects constructs that let an app-owned template do unbounded work
// without writing output in the shared ingress process.
func ParseApp(src string, logo template.HTML) (*template.Template, error) {
	page, err := Parse(src, logo)
	if err != nil {
		return nil, err
	}
	if len(page.Templates()) != 1 || !bounded(page.Tree.Root) {
		return nil, fmt.Errorf("app error page uses an unsupported action or function; only if, with, brandLogo, comparisons, and/or/not, and len are allowed")
	}
	return page, nil
}

func bounded(node parse.Node) bool {
	switch n := node.(type) {
	case *parse.ListNode:
		if n == nil {
			return true
		}
		for _, child := range n.Nodes {
			if !bounded(child) {
				return false
			}
		}
	case *parse.IfNode:
		return bounded(n.Pipe) && bounded(n.List) && bounded(n.ElseList)
	case *parse.WithNode:
		return bounded(n.Pipe) && bounded(n.List) && bounded(n.ElseList)
	case *parse.ActionNode:
		return bounded(n.Pipe)
	case *parse.PipeNode:
		for _, command := range n.Cmds {
			if !bounded(command) {
				return false
			}
		}
	case *parse.CommandNode:
		for _, arg := range n.Args {
			if ident, ok := arg.(*parse.IdentifierNode); ok {
				switch ident.Ident {
				case "brandLogo", "eq", "ne", "lt", "le", "gt", "ge", "and", "or", "not", "len":
				default:
					return false
				}
			}
			if !bounded(arg) {
				return false
			}
		}
	case *parse.ChainNode:
		return bounded(n.Node)
	case *parse.TemplateNode, *parse.RangeNode:
		return false
	}
	return true
}
