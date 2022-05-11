package main

import "fmt"

type RemoteCommander interface {
	MoveCommand(from, to Filename) string
	DeleteCommand(filename Filename) string
}

type UnixCommander struct {
	RemoteCommander
}
func (commander UnixCommander) MoveCommand(from, to Filename) string {
	return fmt.Sprintf("mv -- %s %s", from.Escaped(), to.Escaped())
}
func (commander UnixCommander) DeleteCommand(filename Filename) string {
	return fmt.Sprintf("rm -rf -- %s", filename.Escaped())
}
