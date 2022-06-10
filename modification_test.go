package main

import (
	"github.com/0leksandr/my.go"
	"testing"
)

func TestModificationsQueue_Optimize(t *testing.T) {
	for i, testCase := range basicModificationCases() {
		queue := ModificationsQueue{}.New()
		for _, modification := range testCase.expectedModifications {
			Must(queue.Add(modification))
		}
		transferQueue := TransferQueue{
			inPlace: queue.fs.FlushInPlaceModifications(),
			updated: queue.fs.FlushUpdated(),
		}
		my.Assert(t, queue.IsEmpty())
		my.Assert(
			t,
			transferQueue.Equals(testCase.expectedQueue),
			i, testCase, testCase.expectedQueue, transferQueue,
		)
	}
}
