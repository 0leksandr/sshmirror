package main

import (
	"os"
)

type Node struct {
	Serializable
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
func (node *Node) Serialize() Serialized {
	children := make([]Serialized, 0, len(node.children))
	for _, child := range node.children { children = append(children, child.Serialize()) }

	return SerializedMap{
		"name":     SerializedString(node.name),
		"updated":  SerializedBool(node.updated),
		"children": SerializedList(children),
	}
}
func (*Node) Deserialize(serialized Serialized) interface{} {
	return (&Node{}).deserializeWithParent(serialized, nil)
}
func (*Node) deserializeWithParent(serialized Serialized, parent *Node) *Node {
	serializedMap := serialized.(SerializedMap)
	node := Node{}.New(string(serializedMap["name"].(SerializedString)), parent)
	node.updated = bool(serializedMap["updated"].(SerializedBool))
	for _, childSerialized := range []Serialized(serializedMap["children"].(SerializedList)) {
		childNode := (&Node{}).deserializeWithParent(childSerialized, node)
		node.children[childNode.name] = childNode
	}
	return node
}

type ModFS struct {
	Serializable
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
	nodeFrom := modFS.getNode(from)
	if nodeFrom.Updated() { nodeFrom.SetUpdated() }
	delete(nodeFrom.GetParent().children, nodeFrom.name)
	toParent := modFS.getNode(to.Parent())
	nodeFrom.parent = toParent
	nameTo := modFS.getNode(to).name
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
func (modFS *ModFS) Serialize() Serialized {
	return SerializedMap{
		"root": modFS.root.Serialize(),
	}
}
func (*ModFS) Deserialize(serialized Serialized) interface{} {
	modFS := ModFS{}.New()
	modFS.root = (&Node{}).Deserialize(serialized.(SerializedMap)["root"]).(*Node)
	return modFS
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
