package main

import (
	"github.com/0leksandr/my.go"
	"testing"
)

func TestModificationsQueue_Add(t *testing.T) {
	a := Path{}.New("a", false)
	b := Path{}.New("b", false)
	modifications := []Modification{
		Deleted{a},
		Updated{a},
		Moved{a, b},
		Updated{a},
		Deleted{a},
	}
	expectedQueue := &ModificationsQueue{
		inPlace: []InPlaceModification{
			//Deleted{a},

			Deleted{a},
			Moved{a, b},
			Deleted{a},
		},
		updated: []Updated{
			{b},
		},
	}

	queue := ModificationsQueue{}
	for _, modification := range modifications { queue.Add(modification) }
	my.Assert(t, queue.Equals(expectedQueue))
}
