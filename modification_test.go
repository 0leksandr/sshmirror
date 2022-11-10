package main

import (
	"github.com/0leksandr/my.go"
	"sort"
	"testing"
)

func TestModificationsQueue_Add(t *testing.T) {
	type TestCase struct {
		modifications   []Modification
		expectedUpdated []Updated
	}

	testCases := map[string][]TestCase{
		"updating": {
			{
				[]Modification{
					Updated{Path{}.New("1/2/3")},
				},
				[]Updated{
					{Path{}.New("1/2/3")},
				},
			},
		},
		"deleting": {
			{
				[]Modification{
					Updated{Path{}.New("1/2/3")},
					Deleted{Path{}.New("1/2/3")},
				},
				[]Updated{},
			},
			{
				[]Modification{
					Deleted{Path{}.New("1/2/3")},
					Updated{Path{}.New("1/2/3")},
				},
				[]Updated{
					{Path{}.New("1/2/3")},
				},
			},
			{
				[]Modification{
					Updated{Path{}.New("1/2/3")},
					Deleted{Path{}.New("1/2")},
				},
				[]Updated{},
			},
			{
				[]Modification{
					Updated{Path{}.New("1/2/3")},
					Deleted{Path{}.New("1")},
				},
				[]Updated{},
			},
		},
		"moving": {
			{
				[]Modification{
					Updated{Path{}.New("1/2/3")},
					Moved{Path{}.New("1/2/3"), Path{}.New("1/2/4")},
				},
				[]Updated{
					{Path{}.New("1/2/4")},
				},
			},
			{
				[]Modification{
					Updated{Path{}.New("1/2/3")},
					Moved{Path{}.New("1/2/3"), Path{}.New("a/b/c")},
				},
				[]Updated{
					{Path{}.New("a/b/c")},
				},
			},
			{
				[]Modification{
					Updated{Path{}.New("1/2/3")},
					Moved{Path{}.New("1/2"), Path{}.New("a/b")},
				},
				[]Updated{
					{Path{}.New("a/b/3")},
				},
			},
			{
				[]Modification{
					Updated{Path{}.New("1/2/3")},
					Moved{Path{}.New("1"), Path{}.New("a")},
				},
				[]Updated{
					{Path{}.New("a/2/3")},
				},
			},
		},
	}

	for _, _testCases := range testCases {
		for _, testCase := range _testCases {
			queue := ModificationsQueue{}.New()
			for _, modification := range testCase.modifications { queue.Add(modification) }
			my.AssertEquals(t, queue.fs.FetchUpdated(false), testCase.expectedUpdated)
		}
	}
}
func TestModificationsQueue_Serialize(t *testing.T) {
	for _, modifications := range [][]Modification{
		{
			Updated{Path{}.New("a/b/c")},
			Deleted{Path{}.New("d/e/f")},
			Moved{Path{}.New("g/h/i"), Path{}.New("j/k/l")},
		},
	} {
		queue1 := ModificationsQueue{}.New()
		for _, modification := range modifications { queue1.AtomicAdd(modification) }
		serialized := queue1.Serialize()
		queue2 := (&ModificationsQueue{}).Deserialize(serialized)
		my.AssertEquals(t, queue1, queue2)
		queue3 := (&ModificationsQueue{}).Deserialize(decodeToSerialized(jsonDeserialize(jsonSerialize(serialized))))
		my.AssertEquals(t, queue1, queue3)
	}
}
func TestTransactionalQueue(t *testing.T) {
	type TestCase struct {
		modifications   []Modification
		expectedInPlace []InPlaceModification
		expectedUpdated []Updated
	}

	testCases := func(prefix string) []TestCase {
		path := func(path string) Path {
			return Path{}.New(Filename(path))
		}
		a := path(prefix + "-a")
		b := path(prefix + "-b")
		c := path(prefix + "-c")
		d := path(prefix + "-d")

		return []TestCase{
			{
				[]Modification{Updated{a}},
				[]InPlaceModification{},
				[]Updated{{a}},
			},
			{
				[]Modification{Moved{a, b}},
				[]InPlaceModification{Moved{a, b}},
				[]Updated{},
			},
			{
				[]Modification{Deleted{a}},
				[]InPlaceModification{Deleted{a}},
				[]Updated{},
			},
			{
				[]Modification{
					Updated{a},
					Moved{b, c},
					Deleted{d},
				},
				[]InPlaceModification{
					Moved{b, c},
					Deleted{d},
				},
				[]Updated{{a}},
			},
		}
	}

	sortUpdated := func(updated []Updated) {
		sort.Slice(updated, func(i, j int) bool {
			return updated[i].path.original.Real() < updated[j].path.original.Real()
		})
	}

	assertInPlace := func(actual []InPlaceModification, expected ...TestCase) {
		expectedMerged := make([]InPlaceModification, 0)
		for _, testCase := range expected {
			expectedMerged = append(expectedMerged, testCase.expectedInPlace...)
		}
		my.AssertEquals(t, actual, expectedMerged)
	}
	assertUpdated := func(actual []Updated, expected ...TestCase) {
		expectedMerged := make([]Updated, 0)
		for _, testCase := range expected {
			expectedMerged = append(expectedMerged, testCase.expectedUpdated...)
		}
		sortUpdated(actual)
		sortUpdated(expectedMerged)
		my.AssertEquals(t, actual, expectedMerged)
	}
	assertModifications := func(actual *TransactionalQueue, expected ...TestCase) {
		assertInPlace(actual.GetInPlace(false), expected...)
		assertUpdated(actual.GetUpdated(false), expected...)
	}
	addBatch := func(queue *TransactionalQueue, batch []Modification) {
		for _, modification := range batch {
			queue.AtomicAdd(modification)
		}
	}

	for _, batch1 := range testCases("batch1") {
		for _, batch2 := range testCases("batch2") {
			for _, batch3 := range testCases("batch3") {
				for _, commit := range []bool{true, false} {
					for _, flush := range []bool{true, false} {
						queue := TransactionalQueue{}.New()
						my.Assert(t, queue.IsEmpty())

						addBatch(queue, batch1.modifications)

						queue.Begin()

						addBatch(queue, batch2.modifications)

						assertInPlace(queue.GetInPlace(flush), batch1, batch2)
						assertUpdated(queue.GetUpdated(flush), batch1, batch2)

						addBatch(queue, batch3.modifications)

						if commit {
							queue.Commit()
						} else {
							queue.Rollback()
						}

						if commit && flush {
							assertModifications(queue, batch3)
						} else {
							assertModifications(queue, batch1, batch2, batch3)
						}
					}
				}
			}
		}
	}
}
