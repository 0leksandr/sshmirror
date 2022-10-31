package main

import (
	"github.com/0leksandr/my.go"
	"testing"
)

func TestPath_Equals(t *testing.T) {
	my.Assert(t, Path{}.New("aaa/bbb/ccc").Equals(Path{}.New("aaa/bbb/ccc")))
	for _, path := range []Path{
		Path{}.New("aaa/bbb/ddd"),
		Path{}.New("aaa/ddd/ccc"),
		Path{}.New("ddd/bbb/ccc"),
	} {
		my.Assert(t, !Path{}.New("aaa/bbb/ccc").Equals(path), path)
		my.Assert(t, !path.Equals(Path{}.New("aaa/bbb/ccc")), path)
	}
}
func TestPath_IsParentOf(t *testing.T) {
	for _, path := range []Path{
		Path{}.New("aaa"),
		Path{}.New("aaa/bbb"),
		Path{}.New("aaa/bbb/ccc"),
	} {
		my.Assert(t, path.IsParentOf(Path{}.New("aaa/bbb/ccc")), path)
	}

	for _, path := range []Path{
		Path{}.New("aaa/bbb"),
		Path{}.New("aaa/bbb/ccc"),
	} {
		my.Assert(t, Path{}.New("aaa/bbb").IsParentOf(path), path)
	}

	for _, path := range []Path{
		Path{}.New("ddd"),
		Path{}.New("aaa/ddd"),
		Path{}.New("aaa/bbb/ddd"),
	} {
		my.Assert(t, !path.IsParentOf(Path{}.New("aaa/bbb/ccc")), path)
	}
}
func TestPath_Relates(t *testing.T) {
	for _, paths := range [][2]Path{
		{
			Path{}.New("aaa/bbb/ccc"),
			Path{}.New("aaa/bbb/ccc"),
		},
		{
			Path{}.New("aaa/bbb"),
			Path{}.New("aaa/bbb/ccc"),
		},
		{
			Path{}.New("aaa/bbb/ccc"),
			Path{}.New("aaa/bbb"),
		},
	} {
		my.Assert(t, paths[0].Relates(paths[1]), paths)
	}

	for _, paths := range [][2]Path{
		{
			Path{}.New("aaa/bbb"),
			Path{}.New("aaa/ccc"),
		},
	} {
		my.Assert(t, !paths[0].Relates(paths[1]), paths)
	}
}
func TestPath_Parent(t *testing.T) {
	type TestCase struct {
		original       Path
		expectedParent Filename
	}
	for _, testCase := range []TestCase{
		{Path{}.New("aaa/bbb/ccc"), "aaa/bbb"},
		{Path{}.New("aaa/bbb"), "aaa"},
		{Path{}.New("aaa"), ""},
		{Path{}.New(""), ""},
	} {
		my.AssertEquals(t, testCase.original.Parent(), Path{}.New(testCase.expectedParent), testCase)
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
		Path{}.New("aaa/bbb/ccc"),
		Path{}.New("aaa/bbb"),
		Path{}.New("aaa/ddd"),
		Path{}.New("aaa/ddd/ccc"),
	)
	assertMoved(
		Path{}.New("aaa/bbb/ccc"),
		Path{}.New("aaa"),
		Path{}.New("ddd"),
		Path{}.New("ddd/bbb/ccc"),
	)
	assertMoved(
		Path{}.New("aaa/bbb/ccc"),
		Path{}.New("aaa/bbb/ccc"),
		Path{}.New("ddd"),
		Path{}.New("ddd"),
	)

	assertNotMoved(
		Path{}.New("aaa/bbb/ccc"),
		Path{}.New("aaa/ddd"),
		Path{}.New("eee"),
	)
}
