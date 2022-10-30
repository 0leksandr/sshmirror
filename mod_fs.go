package main

import (
	"os"
)

type Node interface {
	GetParent() *Dir
	Filename() Filename // MAYBE: remove
	Path() Path
	Walk(func(Node))
	Suicide() // MAYBE: rename
	Updated() bool
	UpdatedRoot() bool
	SetUpdated()
}
type File struct {
	Node
	name    string // MAYBE: remove
	parent  *Dir
	updated bool
}
func (file *File) GetParent() *Dir {
	return file.parent
}
func (file *File) Filename() Filename {
	parent := file.parent.Filename()
	if parent != "" { parent += Filename(os.PathSeparator) }
	return parent + Filename(file.name)
}
func (file *File) Path() Path {
	return Path{}.New(file.Filename(), false)
}
func (file *File) Walk(f func(Node)) {
	f(file)
}
func (file *File) Suicide() {
	parent := file.parent
	delete(parent.files, file.name)
	if len(parent.files) == 0 && len(parent.dirs) == 0 {
		parent.Suicide()
	}
}
func (file *File) Updated() bool {
	if file.parent != nil && file.parent.Updated() { return true }
	return file.updated
}
func (file *File) UpdatedRoot() bool {
	if file.parent != nil && file.parent.Updated() { return false }
	return file.updated
}
func (file *File) SetUpdated() {
	file.updated = true
}
func (file *File) Copy(parent *Dir) *File {
	return &File{
		name:    file.name,
		parent:  parent,
		updated: file.updated,
	}
}
func (file *File) String() string { // TODO: remove
	return "name:" + file.Filename().Real()
}
type Dir struct {
	File
	// MAYBE: speedup/optimize by using sorted list
	// MAYBE: combine files and dirs
	files map[string]*File
	dirs  map[string]*Dir
}
func (Dir) New(name string, parent *Dir) *Dir {
	return &Dir{
		File:  File{
			name:   name,
			parent: parent,
		},
		files: make(map[string]*File),
		dirs:  make(map[string]*Dir),
	}
}
func (dir *Dir) Filename() Filename {
	if dir.parent == nil {
		return Filename(dir.File.name)
	} else {
		return dir.File.Filename()
	}
}
func (dir *Dir) Path() Path {
	return Path{}.New(dir.Filename(), true)
}
func (dir *Dir) Walk(f func(Node)) {
	f(dir)
	for _, file := range dir.files { file.Walk(f) }
	for _, childDir := range dir.dirs { childDir.Walk(f) }
}
func (dir *Dir) Suicide() {
	if dir.GetParent() == nil { return } // root
	parent := dir.parent
	delete(parent.dirs, dir.name)
	if len(parent.files) == 0 && len(parent.dirs) == 0 {
		parent.Suicide()
	}
}
func (dir *Dir) GetFile(name string) *File {
	if _, not := dir.files[name]; !not {
		dir.files[name] = &File{
			name:    name,
			parent:  dir,
			updated: false,
		}
	}
	return dir.files[name]
}
func (dir *Dir) GetDir(name string) *Dir {
	if _, not := dir.dirs[name]; !not {
		dir.dirs[name] = Dir{}.New(name, dir)
	}
	return dir.dirs[name]
}
func (dir *Dir) Copy(parent *Dir) *Dir {
	_copy := Dir{}.New(dir.File.name, parent)
	if parent != nil { parent.dirs[dir.File.name] = _copy }
	for _, file := range dir.files { _copy.files[file.name] = file.Copy(_copy) }
	for _, childDir := range dir.dirs { _copy.dirs[childDir.File.name] = childDir.Copy(_copy) }
	return _copy
}
func (dir *Dir) resetChildren() {
	dir.files = make(map[string]*File)
	dir.dirs  = make(map[string]*Dir)
}
func (dir *Dir) GetParent() *Dir {
	return dir.File.GetParent()
}
func (dir *Dir) Updated() bool {
	return dir.File.Updated()
}
func (dir *Dir) UpdatedRoot() bool {
	return dir.File.UpdatedRoot()
}
func (dir *Dir) SetUpdated() {
	dir.File.SetUpdated()
}

type ModFS struct {
	root *Dir // MAYBE: separate type for root
}
func (ModFS) New() *ModFS {
	return &ModFS{
		root: Dir{}.New("", nil),
	}
}
func (modFS *ModFS) Update(path Path) {
	modFS.getNode(path).SetUpdated()
}
func (modFS *ModFS) Delete(path Path) {
	if modFS.nodeExists(path) {
		modFS.getNode(path).Suicide()
	}
}
func (modFS *ModFS) Move(from Path, to Filename) { // MAYBE: refactor
	pathTo   := Path{}.New(to, from.isDir)
	toParent := modFS.getDir(pathTo.Parent())
	nodeFrom := modFS.getNode(from)
	if nodeFrom.Updated() { nodeFrom.SetUpdated() }
	if from.isDir {
		dirFrom := modFS.getDir(from)
		nameTo := modFS.getDir(pathTo).name
		delete(dirFrom.GetParent().dirs, dirFrom.name)
		dirFrom.parent = toParent
		toParent.dirs[nameTo] = dirFrom
		dirFrom.name = nameTo
	} else {
		fileFrom := modFS.getFile(from)
		nameTo := modFS.getFile(pathTo).name
		delete(fileFrom.GetParent().files, fileFrom.name)
		fileFrom.parent = toParent
		toParent.files[nameTo] = fileFrom
		fileFrom.name = nameTo
	}
}
func (modFS *ModFS) FetchUpdated(flush bool) []Updated {
	updated := make([]Updated, 0)

	modFS.root.Walk(func(node Node) {
		if node.UpdatedRoot() {
			updated = append(updated, Updated{node.Path()})
		}
	})
	if flush {
		modFS.root.dirs = make(map[string]*Dir)
		modFS.root.files = make(map[string]*File)
	}

	return updated
}
func (modFS *ModFS) Copy() *ModFS {
	return &ModFS{
		root: modFS.root.Copy(nil),
	}
}
func (modFS *ModFS) getFile(path Path) *File {
	if path.isDir { panic("must be a file") } // TODO: remove
	parts := path.parts
	return modFS.getDir(path.Parent()).GetFile(parts[len(parts) - 1])
}
func (modFS *ModFS) getDir(path Path) *Dir {
	dir := modFS.root
	for _, part := range path.parts { dir = dir.GetDir(part) }
	return dir
}
func (modFS *ModFS) getNode(path Path) Node {
	if path.isDir {
		return modFS.getDir(path)
	} else {
		return modFS.getFile(path)
	}
}
func (modFS *ModFS) nodeExists(path Path) bool { // TODO: test
	dir := modFS.root
	last := len(path.parts) - 1
	if last > 0 {
		for _, part := range path.parts[:last] {
			if subDir, ok := dir.dirs[part]; ok {
				dir = subDir
			} else {
				return false
			}
		}
	}

	lastPart := path.parts[last]
	if path.isDir {
		_, hasDir := dir.dirs[lastPart]
		return hasDir
	} else {
		_, hasFile := dir.files[lastPart]
		return hasFile
	}
}
func (modFS *ModFS) String() string { // MAYBE: make it normal
	s := "."
	modFS.root.Walk(func(node Node) {
		switch node.(type) {
			case *File: s+= "\n" + string(node.(*File).Filename())
			case *Dir:
				dir := node.(*Dir)
				if len(dir.files) == 0 || len(dir.dirs) == 0 {
					s+= "\n" + string(dir.Filename()) + "/"
				}
			default: panic("unknown node")
		}
		if node.Updated() { s += " <" }
	})
	return s
}
