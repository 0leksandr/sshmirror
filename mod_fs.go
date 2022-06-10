package main

import (
	"github.com/0leksandr/my.go"
	"os"
	"strings"
)

type Pathname struct {
	Filename
	IsDir bool
}

type NodeStatus interface {
	Modification(Filename) Modification
	ShiftTo(Filename, *ModFS)
}
type StatusUpdated struct {
	NodeStatus
}
type StatusDeleted struct {
	NodeStatus
}
type StatusMovedTo struct {
	NodeStatus
	movedFrom Node
}
type StatusMovedFrom struct {
	NodeStatus
}
func (statusUpdated StatusUpdated) Modification(filename Filename) Modification {
	return Updated{filename}
}
func (statusDeleted StatusDeleted) Modification(filename Filename) Modification {
	return Deleted{filename}
}
func (statusMovedTo StatusMovedTo) Modification(filename Filename) Modification {
	return Moved{statusMovedTo.movedFrom.Filename(), filename}
}
func (statusMovedFrom StatusMovedFrom) Modification(Filename) Modification {
	return nil
}
func (statusUpdated StatusUpdated) ShiftTo(filename Filename, modFS *ModFS) {
	modFS.Update(filename)
}
func (statusDeleted StatusDeleted) ShiftTo(Filename, *ModFS) {
	panic("cannot shift the deleted") // THINK: could it be a valid case?
}
func (statusMovedTo StatusMovedTo) ShiftTo(filename Filename, modFS *ModFS) {
	modFS.Move(
		statusMovedTo.movedFrom.Pathname(),
		filename,
	)
}
func (statusMovedFrom StatusMovedFrom) ShiftTo(Filename, *ModFS) {
	panic("cannot shift the moved") // THINK: same as above?
}

type Node interface {
	GetParent() *Dir
	ResetChanges(*ModFS) // MAYBE: refactor
	Filename() Filename
	Pathname() Pathname
}
type File struct {
	Node
	name   string // MAYBE: remove
	parent *Dir
}
func (file *File) GetParent() *Dir {
	return file.parent
}
func (file *File) ResetChanges(modFS *ModFS) {
	modFS.changes.Del(file)
}
func (file *File) Filename() Filename {
	return file.parent.Filename() + Filename(os.PathSeparator) + Filename(file.name)
}
func (file *File) Pathname() Pathname {
	return Pathname{
		Filename: file.Filename(),
		IsDir:    false,
	}
}
type Dir struct {
	Node
	File
	files map[string]*File // MAYBE: speedup/optimize by using sorted list
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
func (dir *Dir) GetParent() *Dir {
	return dir.Node.GetParent()
}
func (dir *Dir) ResetChanges(modFS *ModFS) {
	modFS.changes.Del(dir)
	for _, file := range dir.files { file.ResetChanges(modFS) }
	for _, _dir := range dir.dirs { _dir.ResetChanges(modFS) }
}
func (dir *Dir) Filename() Filename {
	dirname := Filename(dir.File.name)
	if dir.parent == nil || dir.parent.parent == nil { // THINK: about
		return dirname
	} else {
		return dir.parent.Filename() + Filename(os.PathSeparator) + dirname
	}
}
func (dir *Dir) Pathname() Pathname {
	return Pathname{
		Filename: dir.Filename(),
		IsDir:    true,
	}
}
func (dir *Dir) GetFile(name string) *File {
	if _, not := dir.files[name]; !not {
		dir.files[name] = &File{
			name:   name,
			parent: dir,
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
func (dir *Dir) resetChildren() {
	dir.files = make(map[string]*File)
	dir.dirs  = make(map[string]*Dir)
}

type ModFS struct {
	root    *Dir
	//changes map[Node]NodeStatus // MAYBE: rename
	changes my.OrderedMap // MAYBE: rename
}
func (ModFS) New() *ModFS {
	return &ModFS{
		root:    Dir{}.New("", nil),
		changes: my.OrderedMap{}.New(),
	}
}
func (modFS *ModFS) Update(filename Filename) {
	file := modFS.getFile(filename)
	if nodeStatus, ok1 := modFS.changes.Get(file); ok1 {
		if movedTo, ok2 := nodeStatus.(StatusMovedTo); ok2 { // TODO: something better
			modFS.Delete(Pathname{
				Filename: movedTo.movedFrom.Filename(),
				IsDir:    false,
			})
		}
		modFS.changes.Del(file)
	}
	modFS.changes.Add(file, StatusUpdated{})
}
func (modFS *ModFS) Delete(path Pathname) {
	node := modFS.getNode(path)
	node.ResetChanges(modFS)
	modFS.changes.Add(node, StatusDeleted{})
}
func (modFS *ModFS) Move(from Pathname, to Filename) {
	nodeFrom := modFS.getNode(from)
	nodeTo   := modFS.getNode(Pathname{
		Filename: to,
		IsDir:    from.IsDir,
	})
	if nodeStatus, ok := modFS.changes.Get(nodeFrom); ok {
		nodeStatus.(NodeStatus).ShiftTo(to, modFS)
	}
	modFS.changes.Del(nodeFrom) // TODO: delete?
	modFS.changes.Add(nodeFrom, StatusMovedFrom{})
	nodeTo.ResetChanges(modFS)
	modFS.changes.Add(nodeTo, StatusMovedTo{movedFrom: nodeFrom})
}
func (modFS *ModFS) FlushInPlaceModifications() []Modification {
	modifications := make([]Modification, 0)
	for _, pair := range modFS.changes.Pairs() {
		status := pair.Value.(NodeStatus)
		if _, not := status.(StatusUpdated); !not {
			node := pair.Key.(Node)
			modification := status.Modification(node.Filename())
			if modification != nil {
				modifications = append(modifications, modification)
				modFS.changes.Del(node)
			}
		}
	}
	return modifications
}
func (modFS *ModFS) FlushUpdated() []Updated {
	updated := make([]Updated, 0)
	for _, pair := range modFS.changes.Pairs() {
		status := pair.Value.(NodeStatus)
		if _, ok := status.(StatusUpdated); ok {
			node := pair.Key.(Node)
			updated = append(updated, Updated{node.Filename()})
			modFS.changes.Del(node)
		}
	}
	return updated
}
func (modFS *ModFS) Copy() *ModFS {
	return &ModFS{
		root:    Dir{}.New("", nil),
		changes: modFS.changes.Copy(),
	}
}
func (modFS *ModFS) getDir(name Filename) *Dir {
	return modFS.getDirByParts(modFS.getFilenameParts(name))
}
func (modFS *ModFS) getFile(name Filename) *File {
	parts := modFS.getFilenameParts(name)
	last := len(parts) - 1
	return modFS.getDirByParts(parts[:last]).GetFile(parts[last])
}
func (modFS *ModFS) getNode(path Pathname) Node {
	if path.IsDir {
		return modFS.getDir(path.Filename)
	} else {
		return modFS.getFile(path.Filename)
	}
}
func (modFS *ModFS) getFilenameParts(filename Filename) []string {
	return strings.Split(filename.Real(), string(os.PathSeparator))
}
func (modFS *ModFS) getDirByParts(parts []string) *Dir {
	dir := modFS.root
	for _, part := range parts { dir = dir.GetDir(part) }
	return dir
}
