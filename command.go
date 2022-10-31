package main

import (
	"fmt"
)

type RemoteCommander interface {
	MoveCommand(from, to Path) string
	DeleteCommand(path Path) string
}

type UnixCommander struct {
	RemoteCommander
}
func (commander UnixCommander) MoveCommand(from, to Path) string {
	// TODO: delete empty directories of `from`

	mkdirCommand := "true"
	toDir := to.Parent()
	if len(toDir.parts) > 0 { mkdirCommand = commander.MkdirCommand(toDir) }
	return fmt.Sprintf("%s && mv -- %s %s", mkdirCommand, from.original.Escaped(), to.original.Escaped())
}
func (commander UnixCommander) DeleteCommand(path Path) string {
	// --- when aaa/bbb/ccc is being deleted, does it mean that ccc or bbb or aaa is deleted?
	// --- if an empty directory is mistakenly left, it may later lead to errors like "mv: cannot move ...: Directory not empty"

	return fmt.Sprintf("rm -rf -- %s", path.original.Escaped())
}
func (commander UnixCommander) MkdirCommand(dir Path) string {
	return fmt.Sprintf("mkdir -p -- %s", dir.original.Escaped())
}
