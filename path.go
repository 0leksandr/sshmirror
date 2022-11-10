package main

import (
	"errors"
	"os"
	"strings"
)

type Path struct {
	Serializable
	original Filename
	parts    []string
}
func (Path) New(original Filename) Path {
	var parts []string
	switch original {
		case "":                         parts = []string{}
		case Filename(os.PathSeparator): parts = []string{}
		default:                         parts = strings.Split(original.Real(), string(os.PathSeparator))
	}

	return Path{
		original: original,
		parts:    parts,
	}
}
func (path Path) Equals(other Path) bool {
	return path.original == other.original
}
func (path Path) IsParentOf(other Path) bool {
	if len(other.parts) < len(path.parts) {
		return false
	}
	for i, part := range path.parts {
		if part != other.parts[i] {
			return false
		}
	}
	return true
}
func (path Path) Relates(other Path) bool { // MAYBE: rename
	return path.IsParentOf(other) || other.IsParentOf(path)
}
func (path Path) Parent() Path {
	if len(path.parts) == 0 {
		return path
	} else {
		parts := path.parts[:len(path.parts) - 1]
		return Path{}.New(Filename(strings.Join(parts, string(os.PathSeparator))))
	}
}
func (path *Path) Move(from, to Path) error {
	if from.IsParentOf(*path) {
		path.parts = append(to.parts, path.parts[len(from.parts):]...)
		path.original = Filename(strings.Join(path.parts, string(os.PathSeparator)))
		return nil
	} else {
		return errors.New("cannot move")
	}
}
func (path Path) Serialize() Serialized {
	return SerializedString(path.original)
}
func (Path) Deserialize(serialized Serialized) interface{} {
	return Path{}.New(Filename(serialized.(SerializedString)))
}
func (path Path) String() string {
	return "Path{" + string(path.original) + "}"
}
