package main

import (
	"errors"
	"os"
	"strings"
)

type Path struct {
	original Filename
	parts    []string
	isDir    bool
}
func (Path) New(original Filename, isDir bool) Path {
	var parts []string
	switch original {
		case "":                         parts = []string{}
		case Filename(os.PathSeparator): parts = []string{}
		default:                         parts = strings.Split(original.Real(), string(os.PathSeparator))
	}

	return Path{
		original: original,
		parts:    parts,
		isDir:    isDir,
	}
}
func (path Path) Equals(other Path) bool {
	return path.original == other.original && path.isDir == other.isDir
}
func (path Path) IsParentOf(other Path) bool {
	if path.Equals(other) {
		return true
	} else if path.isDir {
		if other.isDir {
			return Path{}.startsWith(other.parts, path.parts)
		} else {
			return Path{}.startsWith(other.parts, path.parts) && path.original != other.original
		}
	} else {
		return false
	}
}
func (path Path) Relates(other Path) bool { // MAYBE: rename
	return path.IsParentOf(other) || other.IsParentOf(path)
}
func (path Path) Parent() Path {
	if len(path.parts) == 0 {
		return path
	} else {
		parts := path.parts[:len(path.parts) - 1]
		return Path{}.New(Filename(strings.Join(parts, string(os.PathSeparator))), true)
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
func (Path) startsWith(big, small []string) bool {
	if len(big) < len(small) { return false }
	for i, partSmall := range small {
		if partSmall != big[i] { return false }
	}
	return true
}
