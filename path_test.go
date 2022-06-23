package main

import (
	"github.com/0leksandr/my.go"
	"testing"
)

func TestPath_Equals(t *testing.T) {
	my.Assert(t, Path{}.New("aaa/bbb/ccc", true).Equals(Path{}.New("aaa/bbb/ccc", true)))
	my.Assert(t, Path{}.New("aaa/bbb/ccc", false).Equals(Path{}.New("aaa/bbb/ccc", false)))
	for _, path := range []Path{
		Path{}.New("aaa/bbb/ccc", true),
		Path{}.New("aaa/bbb/ddd", false),
		Path{}.New("aaa/ddd/ccc", false),
		Path{}.New("ddd/bbb/ccc", false),
	} {
		my.Assert(t, !Path{}.New("aaa/bbb/ccc", false).Equals(path), path)
		my.Assert(t, !path.Equals(Path{}.New("aaa/bbb/ccc", false)), path)
	}
}
func TestPath_IsParentOf(t *testing.T) {
	for _, path := range []Path{
		Path{}.New("aaa", true),
		Path{}.New("aaa/bbb", true),
		Path{}.New("aaa/bbb/ccc", false),
	} {
		my.Assert(t, path.IsParentOf(Path{}.New("aaa/bbb/ccc", false)), path)
	}

	for _, path := range []Path{
		Path{}.New("aaa/bbb", true),
		Path{}.New("aaa/bbb/ccc", true),
		Path{}.New("aaa/bbb/ccc", false),
	} {
		my.Assert(t, Path{}.New("aaa/bbb", true).IsParentOf(path), path)
	}

	for _, path := range []Path{
		Path{}.New("ddd", true),
		Path{}.New("aaa/ddd", true),
		Path{}.New("aaa/bbb/ccc", true),
		Path{}.New("aaa/bbb/ddd", true),
		Path{}.New("aaa/bbb/ccc/ddd", false),
	} {
		my.Assert(t, !path.IsParentOf(Path{}.New("aaa/bbb/ccc", false)), path)
	}

	for _, path := range []Path{
		Path{}.New("aaa", true),
		Path{}.New("aaa/bbb", true),
		Path{}.New("aaa/bbb/ccc", true),
		Path{}.New("aaa/bbb/ccc/ddd", true),
	} {
		my.Assert(t, !Path{}.New("aaa/bbb/ccc", false).IsParentOf(path), path)
	}
}
func TestPath_Relates(t *testing.T) {
	for _, paths := range [][2]Path{
		{
			Path{}.New("aaa/bbb/ccc", false),
			Path{}.New("aaa/bbb/ccc", false),
		},
		{
			Path{}.New("aaa/bbb/ccc", true),
			Path{}.New("aaa/bbb/ccc", true),
		},
		{
			Path{}.New("aaa/bbb", true),
			Path{}.New("aaa/bbb/ccc", false),
		},
		{
			Path{}.New("aaa/bbb", true),
			Path{}.New("aaa/bbb/ccc", true),
		},
		{
			Path{}.New("aaa/bbb/ccc", false),
			Path{}.New("aaa/bbb", true),
		},
		{
			Path{}.New("aaa/bbb/ccc", true),
			Path{}.New("aaa/bbb", true),
		},
	} {
		my.Assert(t, paths[0].Relates(paths[1]), paths)
	}

	for _, paths := range [][2]Path{
		{
			Path{}.New("aaa/bbb/ccc", true),
			Path{}.New("aaa/bbb/ccc", false),
		},
		{
			Path{}.New("aaa/bbb", true),
			Path{}.New("aaa/ccc", true),
		},
	} {
		my.Assert(t, !paths[0].Relates(paths[1]), paths)
	}
}
func TestPath_Move(t *testing.T) {
	assertMoved := func(path, from, to, expected Path) {
		context := []interface{}{path, from, to}
		err := path.Move(from, to)
		my.Assert(t, err == nil, append(context, err)...)
		my.Assert(t, path.Equals(expected), append(context, path)...)
	}
	assertNotMoved := func(path, from, to Path) {
		context := []interface{}{path, from, to}
		originalPath := path
		err := path.Move(from, to)
		my.Assert(t, err != nil, context...)
		my.Assert(t, path.Equals(originalPath), context...)
	}

	assertMoved(
		Path{}.New("aaa/bbb/ccc", false),
		Path{}.New("aaa/bbb", true),
		Path{}.New("aaa/ddd", true),
		Path{}.New("aaa/ddd/ccc", false),
	)
	assertMoved(
		Path{}.New("aaa/bbb/ccc", false),
		Path{}.New("aaa", true),
		Path{}.New("ddd", true),
		Path{}.New("ddd/bbb/ccc", false),
	)
	assertMoved(
		Path{}.New("aaa/bbb/ccc", true),
		Path{}.New("aaa/bbb/ccc", true),
		Path{}.New("ddd", true),
		Path{}.New("ddd", true),
	)

	assertNotMoved(
		Path{}.New("aaa/bbb/ccc", false),
		Path{}.New("aaa/ddd", true),
		Path{}.New("eee", true),
	)
	assertNotMoved(
		Path{}.New("aaa/bbb/ccc", false),
		Path{}.New("aaa/bbb", false),
		Path{}.New("ddd", false),
	)
}
