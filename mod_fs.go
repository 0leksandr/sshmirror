package main

import (
	"os"
)

type Node struct {
	name    string
	parent  *Node
	updated bool
	// MAYBE: speedup/optimize by using sorted list
	children map[string]*Node
}
func (Node) New(name string, parent *Node) *Node {
	return &Node{
		name:     name,
		parent:   parent,
		children: make(map[string]*Node),
	}
}
func (node *Node) Filename() Filename {
	if node.parent == nil {
		return Filename(node.name)
	} else {
		parent := node.parent.Filename()
		if parent != "" { parent += Filename(os.PathSeparator) }
		return parent + Filename(node.name)
	}
}
func (node *Node) Walk(f func(*Node)) {
	f(node)
	for _, child := range node.children { child.Walk(f) }
}
func (node *Node) Die() {
	if node.GetParent() == nil { return } // root
	parent := node.parent
	delete(parent.children, node.name)
	if len(parent.children) == 0 { parent.Die() }
}
func (node *Node) GetChild(name string) *Node {
	if _, not := node.children[name]; !not {
		node.children[name] = Node{}.New(name, node)
	}
	return node.children[name]
}
func (node *Node) Copy(parent *Node) *Node {
	_copy := Node{}.New(node.name, parent)
	if parent != nil { parent.children[node.name] = _copy }
	_copy.updated = node.updated
	for _, childDir := range node.children { _copy.children[childDir.name] = childDir.Copy(_copy) }
	return _copy
}
func (node *Node) ResetChildren() {
	for _, child := range node.children { child.parent = nil }
	node.children = make(map[string]*Node)
}
func (node *Node) GetParent() *Node {
	return node.parent
}
func (node *Node) Updated() bool {
	if node.parent != nil && node.parent.Updated() { return true }
	return node.updated
}
func (node *Node) UpdatedRoot() bool {
	if node.parent != nil && node.parent.Updated() { return false }
	return node.updated
}
func (node *Node) SetUpdated() {
	node.updated = true
}

type ModFS struct {
	root *Node
}
func (ModFS) New() *ModFS {
	return &ModFS{
		root: Node{}.New("", nil),
	}
}
func (modFS *ModFS) Update(path Path) {
	modFS.getNode(path).SetUpdated()
}
func (modFS *ModFS) Delete(path Path) {
	if modFS.nodeExists(path) {
		modFS.getNode(path).Die()
	}
}
func (modFS *ModFS) Move(from Path, to Path) {
	toParent := modFS.getNode(to.Parent())
	nodeFrom := modFS.getNode(from)
	if nodeFrom.Updated() { nodeFrom.SetUpdated() }
	nameTo := modFS.getNode(to).name
	delete(nodeFrom.GetParent().children, nodeFrom.name)
	nodeFrom.parent = toParent
	toParent.children[nameTo] = nodeFrom
	nodeFrom.name = nameTo
}
func (modFS *ModFS) FetchUpdated(flush bool) []Updated {
	updated := make([]Updated, 0)

	modFS.root.Walk(func(node *Node) {
		if node.UpdatedRoot() {
			updated = append(updated, Updated{Path{}.New(node.Filename())})
		}
	})
	if flush {
		modFS.root.ResetChildren()
	}

	return updated
}
func (modFS *ModFS) Copy() *ModFS {
	return &ModFS{
		root: modFS.root.Copy(nil),
	}
}
func (modFS *ModFS) getNode(path Path) *Node {
	node := modFS.root
	for _, part := range path.parts { node = node.GetChild(part) }
	return node
}
func (modFS *ModFS) nodeExists(path Path) bool { // TODO: test
	node := modFS.root
	for _, part := range path.parts {
		if child, ok := node.children[part]; ok {
			node = child
		} else {
			return false
		}
	}
	return true
}
func (modFS *ModFS) String() string { // MAYBE: make it normal
	s := "."
	modFS.root.Walk(func(node *Node) {
		if len(node.children) == 0 {
			s+= "\n" + string(node.Filename())
		}
		if node.Updated() { s += " <" }
	})
	return s
}
