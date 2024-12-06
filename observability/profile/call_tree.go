package profile

import (
	gprofile "github.com/google/pprof/profile"
)

type CallTreeNode struct {
	Value    uint64
	Symbol   *FuncSymbol
	Count    uint64
	Children map[uint64]*CallTreeNode
}

type CallTree struct {
	root map[uint64]*CallTreeNode

	funcs map[uint64]*gprofile.Function
}

func (c *CallTree) IngestStack(stack []uint64, symzer *Symbolizer) {
	if len(stack) == 0 {
		return
	}

	if c.root == nil {
		c.root = make(map[uint64]*CallTreeNode)
	}

	root := stack[len(stack)-1]

	rootFn := root

	var s gprofile.Sample

	sym, _ := symzer.ResolveFunc(root)
	if sym != nil {
		rootFn = sym.Lo

		if gf, ok := c.funcs[root]; ok {
			s.Location = append(s.Location, &gprofile.Location{
				ID:      root,
				Address: root,
				Line: []gprofile.Line{
					{
						Function: gf,
						Line:     1,
					},
				},
			})
		}
	}

	node, ok := c.root[rootFn]
	if !ok {
		node = &CallTreeNode{
			Value:    root,
			Symbol:   sym,
			Count:    1,
			Children: make(map[uint64]*CallTreeNode),
		}
		c.root[root] = node
	} else {
		node.Count++
	}

	for i := len(stack) - 2; i >= 0; i-- {
		child := stack[i]

		sym, _ := symzer.ResolveFunc(child)
		if sym != nil {
			child = sym.Lo
		}

		childNode, ok := node.Children[child]
		if !ok {
			childNode = &CallTreeNode{
				Value:    child,
				Symbol:   sym,
				Count:    1,
				Children: make(map[uint64]*CallTreeNode),
			}
			node.Children[child] = childNode
		} else {
			childNode.Count++
		}
		node = childNode
	}
}
